package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Fork (B rules 17–20): the answer-on-your-behalf run. The server builds its
// whole prompt (server_prompt) and executes its outcome; the run itself can
// only say answer (with sources from the candidates) or ask.

func (h *Handler) applySuperAnswerPrompt(r *http.Request, task *db.AgentTaskQueue, resp *AgentTaskResponse) *claimBuildFailure {
	if _, ok := h.TaskService.SuperAnswerQuestion(r.Context(), *task); !ok {
		return nil
	}
	prompt, err := h.TaskService.BuildSuperAnswerPrompt(r.Context(), *task)
	if err != nil {
		// A final failure pushes the question to the member (R9).
		return h.failClaimedTaskBeforeLaunch(r.Context(), task, "Could not assemble the answer prompt: "+err.Error(),
			taskfailure.ReasonInvalidTaskIdentity, "error_super_answer_prompt", http.StatusConflict, "super answer prompt failed")
	}
	resp.ServerPrompt = prompt
	return nil
}

// runSuperAnswer executes a finished answer run (CompleteTask, after commit).
func (h *Handler) runSuperAnswer(ctx context.Context, task *db.AgentTaskQueue, output string) {
	q, ok := h.TaskService.SuperAnswerQuestion(ctx, *task)
	if !ok || q.Status != "answering" {
		return
	}
	out := service.ParseSuperAnswer(output)
	if !out.Answer {
		h.TaskService.AskUser(ctx, q.ID)
		return
	}
	question, err := h.Queries.GetComment(ctx, q.CommentID)
	if err != nil {
		h.TaskService.AskUser(ctx, q.ID)
		return
	}
	issue, err := h.Queries.GetIssue(ctx, question.IssueID)
	if err != nil {
		return
	}
	session, err := h.Queries.GetChatSession(ctx, q.ChatSessionID)
	if err != nil {
		return
	}
	cands, err := h.TaskService.SuperAnswerCandidates(ctx, session, issue)
	if err != nil {
		h.TaskService.AskUser(ctx, q.ID)
		return
	}
	byID := make(map[string]service.SuperCandidate, len(cands))
	for _, c := range cands {
		byID[c.ID] = c
	}
	var picked []service.SuperCandidate
	for _, id := range out.Sources {
		c, ok := byID[strings.TrimSpace(id)]
		if !ok { // not the member's words in this tree: never quoted (R12)
			h.TaskService.AskUser(ctx, q.ID)
			return
		}
		picked = append(picked, c)
	}
	settled, ok := h.TaskService.ClaimSuperAnswer(ctx, q, out.Partial)
	if !ok {
		return // a replayed completion
	}
	user := ""
	if u, err := h.Queries.GetUser(ctx, session.CreatorID); err == nil {
		user = u.Name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "> super-agent 引用 %s 的原话\n", user)
	for _, c := range picked {
		fmt.Fprintf(&b, "\n%s\n（[原话出处](%s)）\n", c.Text, c.Link)
	}
	sa, err := h.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return
	}
	reply, err := h.createSuperAgentCommentWithParent(ctx, issue, sa, task.ID, question.ID, b.String())
	if err != nil {
		slog.Warn("super-agent answer comment failed; asking the member", "question_id", uuidToString(q.ID), "error", err)
		h.TaskService.PushQuestion(ctx, settled)
		return
	}
	// Woken as if the member replied: the agent that asked (R2).
	userID := uuidToString(session.CreatorID)
	h.triggerTasksForComment(ctx, issue, reply, &question, "member", userID, userID, nil, commentSteerRequest{})
	if out.Partial {
		h.TaskService.PushQuestion(ctx, settled) // the rest is the member's to answer (rule 19)
	}
}
