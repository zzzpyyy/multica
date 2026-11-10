package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Fork (zzzpyyy/multica), B: the built-in super-agent (plan 2.4, 3.1).

const superAgentMaxConcurrency = 3

type createSuperAgentRequest struct {
	RuntimeID string `json:"runtime_id"`
	Model     string `json:"model"`
}

// superAgentResponse is SuperAgentBootstrap (plan 3.2): the agent plus the
// caller's one session.
type superAgentResponse struct {
	AgentResponse
	Session ChatSessionResponse `json:"session"`
}

// CreateSuperAgent: POST /api/agents/super-agent. Provisions the workspace's
// super-agent on a Claude runtime (201) or returns it (200), with the
// caller's session. Same locking as CreateMikaAgent.
func (h *Handler) CreateSuperAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	var req createSuperAgentRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	agent, created, ok := h.resolveSuperAgent(w, r, workspaceID, userID, req)
	if !ok {
		return
	}
	resp := h.agentToResponse(agent)
	if created {
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	}
	session, err := h.getOrCreateSuperAgentSession(r.Context(), agent, workspaceID, userID)
	if err != nil {
		slog.Warn("super-agent: get-or-create session failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to open the super-agent conversation")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, superAgentResponse{AgentResponse: resp, Session: chatSessionToResponse(session)})
}

func (h *Handler) resolveSuperAgent(w http.ResponseWriter, r *http.Request, workspaceID, userID string, req createSuperAgentRequest) (db.Agent, bool, bool) {
	wsUUID := parseUUID(workspaceID)
	key := pgtype.Text{String: service.SuperAgentSystemKey, Valid: true}
	if existing, err := h.Queries.GetAgentBySystemKey(r.Context(), db.GetAgentBySystemKeyParams{WorkspaceID: wsUUID, SystemKey: key}); err == nil {
		return existing, false, true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to look up the super-agent")
		return db.Agent{}, false, false
	}
	if strings.TrimSpace(req.RuntimeID) == "" {
		writeErrorCode(w, http.StatusConflict, "runtime_required", "the super-agent is not set up yet; choose a Claude runtime")
		return db.Agent{}, false, false
	}
	runtimeID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return db.Agent{}, false, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Agent{}, false, false
	}
	runtime, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if err != nil || uuidToString(runtime.WorkspaceID) != workspaceID {
		writeError(w, http.StatusBadRequest, "runtime not found in this workspace")
		return db.Agent{}, false, false
	}
	if runtime.Provider != "claude" {
		writeErrorCode(w, http.StatusBadRequest, "runtime_not_claude", "the super-agent runs only on a Claude runtime")
		return db.Agent{}, false, false
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "you cannot bind an agent to this runtime")
		return db.Agent{}, false, false
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start agent create transaction")
		return db.Agent{}, false, false
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "super-agent:"+workspaceID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock the super-agent")
		return db.Agent{}, false, false
	}
	if existing, err := qtx.GetAgentBySystemKey(r.Context(), db.GetAgentBySystemKeyParams{WorkspaceID: wsUUID, SystemKey: key}); err == nil {
		return existing, false, true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to look up the super-agent")
		return db.Agent{}, false, false
	}
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock workspace")
		return db.Agent{}, false, false
	}
	runtime, err = qtx.LockAgentRuntimeForAccessChange(r.Context(), runtimeID)
	if err != nil || runtime.WorkspaceID != wsUUID || !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusBadRequest, "runtime not found in this workspace")
		return db.Agent{}, false, false
	}
	model := strings.TrimSpace(req.Model)
	created, err := qtx.CreateSystemUserAgent(r.Context(), db.CreateSystemUserAgentParams{
		WorkspaceID:        wsUUID,
		Name:               service.SuperAgentDefaultName,
		Description:        "和你对话、建任务、跟进进度的唯一对接人。",
		AvatarUrl:          pgtype.Text{String: agentEmojiAvatarPrefix + "🧭", Valid: true},
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeID:          runtime.ID,
		Model:              pgtype.Text{String: model, Valid: model != ""},
		Visibility:         mikaAgentVisibility,
		PermissionMode:     mikaAgentPermissionMode,
		MaxConcurrentTasks: superAgentMaxConcurrency,
		OwnerID:            parseUUID(userID),
		SystemKey:          key,
	})
	if err != nil {
		slog.Warn("create super-agent failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create the super-agent")
		return db.Agent{}, false, false
	}
	if err := replaceInvocationTargetsWithQueries(r.Context(), qtx, created.ID, parseUUID(userID), []targetSpec{
		{targetType: invocationTargetWorkspace, targetID: wsUUID},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent access")
		return db.Agent{}, false, false
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit agent create")
		return db.Agent{}, false, false
	}
	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), created.ID)
		created, _ = h.Queries.GetAgent(r.Context(), created.ID)
	}
	return created, true, true
}

// getOrCreateSuperAgentSession returns the member's single super-agent
// session. Archived sessions are not skipped: the session cannot be archived
// or deleted (superAgentSessionLocked), so the oldest one is the one.
func (h *Handler) getOrCreateSuperAgentSession(ctx context.Context, agent db.Agent, workspaceID, userID string) (db.ChatSession, error) {
	wsUUID, creator := parseUUID(workspaceID), parseUUID(userID)
	if s, err := h.Queries.GetSuperAgentSessionForMember(ctx, db.GetSuperAgentSessionForMemberParams{WorkspaceID: wsUUID, CreatorID: creator}); err == nil {
		return s, nil
	}
	return h.getOrCreateMikaSession(ctx, agent, workspaceID, userID, "super-agent")
}

// superAgentSessionLocked answers 409 super_agent_session_locked when a
// request would create a second super-agent session or archive/delete the
// one there is (rule 1; plan 3.1).
func (h *Handler) superAgentSessionLocked(w http.ResponseWriter, ctx context.Context, agentID pgtype.UUID) bool {
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || !service.IsSuperAgent(agent) {
		return false
	}
	writeErrorCode(w, http.StatusConflict, "super_agent_session_locked", "the super-agent has exactly one conversation; it cannot be created again, archived or deleted")
	return true
}

// refuseSuperAgentAssignee answers 400 super_agent_not_assignable (plan 3.1).
func refuseSuperAgentAssignee(w http.ResponseWriter, agent db.Agent) bool {
	if !service.IsSuperAgent(agent) {
		return false
	}
	writeError(w, http.StatusBadRequest, superAgentNotAssignableMsg)
	return true
}

// applySuperAgentClaim gates and shapes a claimed super-agent task: only the
// fork's daemon may run it, it runs with no tools, and it gets the
// product-owned instructions instead of anything saved on the agent row.
func (h *Handler) applySuperAgentClaim(r *http.Request, task *db.AgentTaskQueue, resp *AgentTaskResponse) *claimBuildFailure {
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil || !service.IsSuperAgent(agent) {
		return nil
	}
	if !requestHasClientCapability(r, protocol.DaemonCapabilityForkV1) {
		return h.failClaimedTaskBeforeLaunch(r.Context(), task,
			"The super-agent needs the fork's daemon (multica-fork-v1) so it runs without tools; the runtime that claimed this task runs an upstream daemon.",
			taskfailure.Reason(ForkDaemonRequiredReason), ForkDaemonRequiredReason, http.StatusConflict, "fork daemon required")
	}
	return nil
}

// stripSuperAgentTools runs last in the claim builder, after every field that
// could carry a tool has been filled in.
func stripSuperAgentTools(resp *AgentTaskResponse) {
	resp.NoTools = true
	resp.RemoteMCPConnections = nil
	resp.PluginHookTools = nil
	if resp.Agent != nil {
		resp.Agent.CustomArgs = nil
		resp.Agent.McpConfig = json.RawMessage(`{}`)
		resp.Agent.Instructions = service.SuperAgentInstructions()
		resp.Agent.Skills = nil
		resp.Agent.SkillRefs = nil
	}
}

func (h *Handler) isSuperAgentTask(ctx context.Context, task *db.AgentTaskQueue) bool {
	agent, err := h.Queries.GetAgent(ctx, task.AgentID)
	return err == nil && service.IsSuperAgent(agent)
}

const superAgentNotAssignableMsg = "the super-agent cannot be assigned work; talk to it in its conversation instead"

// refuseSuperAgentByID is refuseSuperAgentAssignee for a raw agent id; an
// unparseable or unknown id is left to the caller's own validation.
func (h *Handler) refuseSuperAgentByID(w http.ResponseWriter, ctx context.Context, agentID string) bool {
	id := pgtype.UUID{}
	if err := id.Scan(strings.TrimSpace(agentID)); err != nil || !id.Valid {
		return false
	}
	agent, err := h.Queries.GetAgent(ctx, id)
	return err == nil && refuseSuperAgentAssignee(w, agent)
}
