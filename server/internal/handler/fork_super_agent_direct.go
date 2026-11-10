package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (B, plan 2.4): forward and set_status run right after the chat turn
// that asked for them has committed. The super-agent is the author/actor;
// who gets woken is computed as if the user had posted (B rule 14, G4).

func (h *Handler) runSuperAgentDirectActions(ctx context.Context, task *db.AgentTaskQueue) {
	if !task.ChatSessionID.Valid {
		return
	}
	actions, err := h.Queries.ListPendingSuperAgentDirectActionsForTask(ctx, task.ID)
	if err != nil || len(actions) == 0 {
		return
	}
	session, err := h.Queries.GetChatSession(ctx, task.ChatSessionID)
	if err != nil {
		return
	}
	for _, a := range actions {
		won, err := h.Queries.TransitionSuperAgentAction(ctx, db.TransitionSuperAgentActionParams{ID: a.ID, FromStatus: "pending", ToStatus: "creating"})
		if err != nil {
			continue // another completion replay took it
		}
		var p service.SuperDirectPayload
		_ = json.Unmarshal(won.Payload, &p)
		var note string
		var result map[string]any
		switch won.Kind {
		case "forward":
			note, result = h.superForward(ctx, task, session, p)
		case "set_status":
			note, result = h.superSetStatus(ctx, task, session, p)
		}
		status := "done"
		if result["error"] != nil {
			status = "failed"
		}
		res, _ := json.Marshal(result)
		if _, err := h.Queries.FinishSuperAgentAction(ctx, db.FinishSuperAgentActionParams{ID: won.ID, Status: status, Result: res}); err != nil {
			slog.Warn("super-agent action: record result failed", "action_id", uuidToString(won.ID), "error", err)
		}
		if note != "" {
			h.postSuperAgentNote(ctx, session, note)
		}
	}
}

func (h *Handler) superIssueRef(ctx context.Context, issue db.Issue) string {
	return fmt.Sprintf("[%s](mention://issue/%s)", service.IssueIdentifier(h.getIssuePrefix(ctx, issue.WorkspaceID), issue.Number), uuidToString(issue.ID))
}

// superForward posts the user's words on the one issue they are for (R1, R4).
func (h *Handler) superForward(ctx context.Context, task *db.AgentTaskQueue, session db.ChatSession, p service.SuperDirectPayload) (string, map[string]any) {
	var issue db.Issue
	found := false
	if p.Issue != "" && service.MentionsIdentifier(p.Text, p.Issue) {
		issue, found = service.ResolveIssueIdentifier(ctx, h.Queries, session.WorkspaceID, p.Issue)
	}
	if !found {
		asked, err := h.Queries.ListSuperAgentQuestionsByStatus(ctx, db.ListSuperAgentQuestionsByStatusParams{ChatSessionID: session.ID, Status: "asked"})
		issues := map[pgtype.UUID]bool{}
		for _, q := range asked {
			issues[q.IssueID] = true
		}
		if err == nil && len(issues) == 1 {
			for id := range issues {
				issue, err = h.Queries.GetIssue(ctx, id)
				found = err == nil
			}
		}
	}
	if !found {
		return "没有转达：请说明是哪个任务。", map[string]any{"error": "target_unclear"}
	}
	sa, err := h.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return "", map[string]any{"error": err.Error()}
	}
	slug := ""
	if ws, err := h.Queries.GetWorkspace(ctx, session.WorkspaceID); err == nil {
		slug = ws.Slug
	}
	link := ""
	if n := len(p.MessageIDs); n > 0 {
		link = service.ChatMessageLink(slug, session.ID, parseUUID(p.MessageIDs[n-1]))
	}
	content := fmt.Sprintf("> 来自 super-agent 对话 · [查看原消息](%s)\n\n%s", link, p.Text)
	comment, err := h.createSuperAgentComment(ctx, issue, sa, task.ID, content)
	if err != nil {
		return "转达失败，请稍后再说一次。", map[string]any{"error": err.Error()}
	}
	// Woken as if the user had written it: the assignee, or a squad's leader.
	userID := uuidToString(session.CreatorID)
	h.triggerTasksForComment(ctx, issue, comment, nil, "member", userID, userID, nil, commentSteerRequest{})
	if err := h.Queries.MarkSuperAgentQuestionsForwarded(ctx, db.MarkSuperAgentQuestionsForwardedParams{ChatSessionID: session.ID, IssueID: issue.ID}); err != nil {
		slog.Warn("super-agent forward: mark questions failed", "error", err)
	}
	return "已转到 " + h.superIssueRef(ctx, issue), map[string]any{
		"issue_id": uuidToString(issue.ID), "comment_id": uuidToString(comment.ID), "message_ids": p.MessageIDs,
	}
}

// superSetStatus changes status only when the user's own words name the
// issue (R5, R11). "不通过" never reaches here: planning turned it into a
// forward.
func (h *Handler) superSetStatus(ctx context.Context, task *db.AgentTaskQueue, session db.ChatSession, p service.SuperDirectPayload) (string, map[string]any) {
	if p.Issue == "" || !service.MentionsIdentifier(p.Text, p.Issue) {
		return "没有改状态：请在消息里写明任务编号。", map[string]any{"error": "issue_not_in_message"}
	}
	issue, ok := service.ResolveIssueIdentifier(ctx, h.Queries, session.WorkspaceID, p.Issue)
	if !ok {
		return fmt.Sprintf("没有改状态：找不到 %s。", p.Issue), map[string]any{"error": "issue_not_found"}
	}
	// ponytail: the status write goes through UpdateIssue itself, in process,
	// with the super-agent as the actor (resolveActor's in-process path), so
	// validation, activity, events and wakeups are exactly a user's. Extract
	// UpdateIssue's core when a second in-process caller needs it.
	body, _ := json.Marshal(map[string]any{"status": p.Status})
	req := httptest.NewRequest(http.MethodPut, "/api/issues/"+uuidToString(issue.ID), bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuidToString(session.CreatorID))
	req.Header.Set("X-Workspace-ID", uuidToString(session.WorkspaceID))
	req.Header.Set("X-Agent-ID", uuidToString(task.AgentID))
	req.Header.Set("X-Task-ID", uuidToString(task.ID))
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidToString(issue.ID))
	h.UpdateIssue(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
	if rec.Code != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		return fmt.Sprintf("没有改状态：%s", e.Error), map[string]any{"error": e.Error, "status_code": rec.Code}
	}
	return fmt.Sprintf("已把 %s 置为 %s", h.superIssueRef(ctx, issue), p.Status), map[string]any{"issue_id": uuidToString(issue.ID), "status": p.Status}
}

// createSuperAgentComment writes a top-level comment authored by the
// super-agent. source_task_id is the chat turn, so runs it starts are
// attributed to the user who sent that turn.
func (h *Handler) createSuperAgentComment(ctx context.Context, issue db.Issue, sa db.Agent, sourceTask pgtype.UUID, content string) (db.Comment, error) {
	created, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: dbid.NewV7(), IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		AuthorType: "agent", AuthorID: sa.ID, Content: content, Type: "comment", SourceTaskID: sourceTask,
	})
	if err != nil {
		return db.Comment{}, err
	}
	comment := created.Comment()
	resp := commentToResponse(comment, nil, nil)
	resp.IssueRevision = created.IssueRevision
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "agent", uuidToString(sa.ID), map[string]any{
		"comment": resp, "issue_title": issue.Title, "issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id": uuidToPtr(issue.AssigneeID), "issue_status": issue.Status, "issue_revision": created.IssueRevision,
	})
	return comment, nil
}

// postSuperAgentNote writes an assistant message with no task (the server's
// own words in the conversation: "已转到 …", pushes) and announces it.
func (h *Handler) postSuperAgentNote(ctx context.Context, session db.ChatSession, content string) {
	if _, err := h.TaskService.PostSuperAgentMessage(ctx, session, content); err != nil {
		slog.Warn("super-agent note failed", "session_id", uuidToString(session.ID), "error", err)
	}
}
