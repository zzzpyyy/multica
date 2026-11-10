package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Fork (zzzpyyy/multica), D: role instances (plan 3.1).
//   POST /api/issues/{id}/instances  add k instances of an agent
//   GET  /api/issues/{id}/instances  batches, instances, queue position/reason

type createInstancesRequest struct {
	AgentID  string   `json:"agent_id"`
	Splits   []string `json:"splits"`
	Priority bool     `json:"priority"`
}

func writeInstanceError(w http.ResponseWriter, err error) bool {
	var ie *service.InstanceError
	if !errors.As(err, &ie) {
		return false
	}
	if ie.Code == "" {
		writeError(w, ie.Status, ie.Msg)
	} else {
		writeErrorCode(w, ie.Status, ie.Code, ie.Msg)
	}
	return true
}

func (h *Handler) CreateIssueInstances(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req createInstancesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	in := service.InstanceRequest{
		Issue:    issue,
		Agent:    agent,
		Splits:   req.Splits,
		Priority: req.Priority,
	}
	if actorType == "agent" {
		// The task token vouches for X-Task-ID (resolveActor). The run must be
		// live and belong to the calling agent.
		taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID"))
		if err != nil {
			writeError(w, http.StatusForbidden, "agent request without a task")
			return
		}
		task, err := h.Queries.GetAgentTask(r.Context(), taskID)
		if err != nil || uuidToString(task.AgentID) != actorID || isTerminalTaskStatus(task.Status) {
			writeError(w, http.StatusForbidden, "agent request without a live task")
			return
		}
		in.CallerTask = &task
	} else {
		in.MemberID = parseUUID(actorID)
	}
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	in.CanInvoke = func(a db.Agent) bool {
		return h.canInvokeAgent(r.Context(), a, actorType, actorID, originator, workspaceID)
	}

	batch, _, err := h.TaskService.CreateInstances(r.Context(), in)
	if err != nil {
		if writeInstanceError(w, err) {
			return
		}
		slog.Error("create instances failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create instances")
		return
	}
	views, err := h.TaskService.ListInstances(r.Context(), issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load instances")
		return
	}
	for _, v := range views {
		if v.ID == uuidToString(batch.ID) {
			writeJSON(w, http.StatusCreated, v)
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "created batch not found")
}

func (h *Handler) ListIssueInstances(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	views, err := h.TaskService.ListInstances(r.Context(), issue)
	if err != nil {
		slog.Error("list instances failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load instances")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": views})
}

// refuseInstanceOwnershipWrite answers 403 instance_run_readonly when an
// instance run tries to change its issue's status, assignee or parent; those
// belong to the main run and the assignee (D4). fields are the JSON keys the
// request sent.
func (h *Handler) refuseInstanceOwnershipWrite(w http.ResponseWriter, r *http.Request, issue db.Issue, fields map[string]json.RawMessage) bool {
	if !instanceOwnershipFields(fields) {
		return false
	}
	actorType, _ := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	run := h.agentRunOnIssue(r, actorType, issue)
	if run == nil || !run.BatchID.Valid {
		return false
	}
	if w != nil {
		writeErrorCode(w, http.StatusForbidden, "instance_run_readonly",
			"an instance run cannot change the issue's status, assignee or parent; post your result as a comment")
	}
	return true
}

func instanceOwnershipFields(fields map[string]json.RawMessage) bool {
	for _, k := range []string{"status", "assignee_type", "assignee_id", "parent_issue_id"} {
		if _, ok := fields[k]; ok {
			return true
		}
	}
	return false
}

func (h *Handler) isInstanceRun(ctx context.Context, taskID pgtype.UUID) bool {
	if !taskID.Valid {
		return false
	}
	task, err := h.Queries.GetAgentTask(ctx, taskID)
	return err == nil && task.BatchID.Valid
}

// InstanceClaim tells the fork's daemon which instance of which batch it is
// running (claim response field `instance`, plan 2.3 / 3.5).
type InstanceClaim struct {
	BatchID  string              `json:"batch_id"`
	Index    int                 `json:"index"`
	Total    int                 `json:"total"`
	Split    string              `json:"split"`
	Siblings []InstanceClaimPeer `json:"siblings"`
}

type InstanceClaimPeer struct {
	Index int    `json:"index"`
	Split string `json:"split"`
}

// ForkDaemonRequiredReason is the failure reason of an instance row claimed by
// a daemon that did not advertise multica-fork-v1.
const ForkDaemonRequiredReason = "fork_daemon_required"

// applyInstanceClaim fills resp.Instance for an instance row, or fails the
// task when the claiming daemon is not the fork's: an upstream daemon would
// ignore the split and work the whole issue.
func (h *Handler) applyInstanceClaim(r *http.Request, task *db.AgentTaskQueue, resp *AgentTaskResponse) *claimBuildFailure {
	if !task.BatchID.Valid {
		return nil
	}
	if !requestHasClientCapability(r, protocol.DaemonCapabilityForkV1) {
		return h.failClaimedTaskBeforeLaunch(r.Context(), task,
			"This run is one instance of an add-instances batch and needs the fork's daemon (multica-fork-v1); the runtime that claimed it runs an upstream daemon.",
			taskfailure.Reason(ForkDaemonRequiredReason), ForkDaemonRequiredReason, http.StatusConflict, "fork daemon required")
	}
	batch, err := h.Queries.GetAgentTaskBatch(r.Context(), task.BatchID)
	if err != nil {
		return h.rejectClaimSourceLoad(r.Context(), task, err, "agent_task_batch", uuidToString(task.BatchID))
	}
	idx := int(task.BatchIndex.Int16)
	if idx < 1 || idx > len(batch.Splits) {
		return h.failClaimedTaskBeforeLaunch(r.Context(), task, "Instance index is outside its batch.",
			taskfailure.ReasonInvalidTaskIdentity, "instance_index_invalid", http.StatusConflict, "instance index invalid")
	}
	claim := &InstanceClaim{BatchID: uuidToString(batch.ID), Index: idx, Total: len(batch.Splits), Split: batch.Splits[idx-1], Siblings: []InstanceClaimPeer{}}
	for i, sp := range batch.Splits {
		if i+1 != idx {
			claim.Siblings = append(claim.Siblings, InstanceClaimPeer{Index: i + 1, Split: sp})
		}
	}
	resp.Instance = claim
	return nil
}
