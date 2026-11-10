package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
