package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Fork (B rules 11–13, F1–F9, R10, G5): pushes are written by the server,
// never by a super-agent run. Each is an assistant chat message with no task
// in the member's super-agent session, recorded in super_agent_push under a
// dedupe key so an event reaches a session once.

// Push writes one push into one session; false when that session already
// had it.
func (s *TaskService) Push(ctx context.Context, session db.ChatSession, dedupeKey, content string) (bool, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	msg, err := q.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ID: dbid.NewV7(), ChatSessionID: session.ID, Role: "assistant", Content: content,
	})
	if err != nil {
		return false, fmt.Errorf("push message: %w", err)
	}
	if _, err := q.CreateSuperAgentPush(ctx, db.CreateSuperAgentPushParams{ChatSessionID: session.ID, ChatMessageID: msg.ID, DedupeKey: dedupeKey}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // already pushed; the rollback drops the message
		}
		return false, fmt.Errorf("push record: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	s.publishSuperAgentMessage(session, msg)
	return true, nil
}

// PushToWorkspace pushes a workspace event to every member who has opened
// the super-agent.
func (s *TaskService) PushToWorkspace(ctx context.Context, ws pgtype.UUID, dedupeKey, content string) {
	sessions, err := s.Queries.ListSuperAgentSessionsInWorkspace(ctx, ws)
	if err != nil {
		slog.Warn("super-agent push: list sessions failed", "error", err)
		return
	}
	for _, sess := range sessions {
		if _, err := s.Push(ctx, sess, dedupeKey, content); err != nil {
			slog.Warn("super-agent push failed", "session_id", util.UUIDToString(sess.ID), "key", dedupeKey, "error", err)
		}
	}
}

func (s *TaskService) issueLink(ctx context.Context, issue db.Issue) string {
	prefix := ""
	if ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID); err == nil {
		prefix = ws.IssuePrefix
	}
	return fmt.Sprintf("[%s](mention://issue/%s)", IssueIdentifier(prefix, issue.Number), util.UUIDToString(issue.ID))
}

func (s *TaskService) commentLink(ctx context.Context, issue db.Issue, commentID pgtype.UUID) string {
	slug := ""
	if ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID); err == nil {
		slug = ws.Slug
	}
	return fmt.Sprintf("/%s/issues/%s#comment-%s", slug, util.UUIDToString(issue.ID), util.UUIDToString(commentID))
}

// quote is a comment excerpt for a push: at most 100 characters (rule 8).
func quote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= 100 {
		return s
	}
	return string(r[:100]) + "…"
}

func (s *TaskService) closedCategory(ctx context.Context, ws pgtype.UUID, status string) bool {
	cat, err := issuestatus.CategoryWithError(ctx, s.Queries, ws, status)
	return err == nil && (cat == "done" || cat == "closed")
}

// OnIssueStatusChanged pushes blocked (F2, F6), top-level in_review (F5) and
// stage completion (F1).
func (s *TaskService) OnIssueStatusChanged(ctx context.Context, issueID pgtype.UUID) {
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	link := s.issueLink(ctx, issue)
	switch {
	case issue.Status == "blocked":
		s.PushToWorkspace(ctx, issue.WorkspaceID, fmt.Sprintf("blocked:%s:%d", util.UUIDToString(issue.ID), issue.Revision),
			fmt.Sprintf("%s 阻塞：%s", link, oneLineRunes(issue.Title, 60)))
	case issue.Status == "in_review" && !issue.ParentIssueID.Valid:
		s.PushToWorkspace(ctx, issue.WorkspaceID, fmt.Sprintf("review:%s:%d", util.UUIDToString(issue.ID), issue.Revision),
			fmt.Sprintf("%s 待你验收：%s", link, oneLineRunes(issue.Title, 60)))
	}
	if issue.ParentIssueID.Valid && s.closedCategory(ctx, issue.WorkspaceID, issue.Status) {
		s.pushStageProgress(ctx, issue)
	}
}

// pushStageProgress: the closed child's stage, and every earlier one, is now
// closed (rules 11, F1).
func (s *TaskService) pushStageProgress(ctx context.Context, child db.Issue) {
	parent, err := s.Queries.GetIssue(ctx, child.ParentIssueID)
	if err != nil {
		return
	}
	rows, err := s.Queries.ListChildIssueStatuses(ctx, db.ListChildIssueStatusesParams{ParentIssueID: parent.ID, WorkspaceID: parent.WorkspaceID})
	if err != nil {
		return
	}
	text, ok := stageMessage(child.Stage, func() []stageChild {
		out := make([]stageChild, len(rows))
		for i, r := range rows {
			out[i] = stageChild{stage: r.Stage, closed: s.closedCategory(ctx, parent.WorkspaceID, r.Status)}
		}
		return out
	}())
	if !ok {
		return
	}
	k := "all"
	if child.Stage.Valid {
		k = fmt.Sprint(child.Stage.Int32)
	}
	key := fmt.Sprintf("stage:%s:%s:%s:%d", util.UUIDToString(parent.ID), k, util.UUIDToString(child.ID), child.Revision)
	s.PushToWorkspace(ctx, parent.WorkspaceID, key, s.issueLink(ctx, parent)+" "+text)
}

type stageChild struct {
	stage  pgtype.Int4
	closed bool
}

// stageMessage decides the stage push for a child that just closed in a
// sibling set (which includes it).
func stageMessage(stage pgtype.Int4, children []stageChild) (string, bool) {
	stages := map[int32]bool{} // stage -> all closed
	allClosed, anyStaged := true, false
	for _, c := range children {
		allClosed = allClosed && c.closed
		if !c.stage.Valid {
			continue
		}
		anyStaged = true
		if _, ok := stages[c.stage.Int32]; !ok {
			stages[c.stage.Int32] = true
		}
		stages[c.stage.Int32] = stages[c.stage.Int32] && c.closed
	}
	if !stage.Valid {
		if anyStaged && allClosed {
			return "阶段全部完成", true
		}
		return "", false
	}
	order := make([]int32, 0, len(stages))
	for k := range stages {
		order = append(order, k)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	for _, k := range order {
		if k <= stage.Int32 && !stages[k] {
			return "", false // this stage or an earlier one is still open
		}
		if k > stage.Int32 && !stages[k] {
			return fmt.Sprintf("阶段 %d 完成，阶段 %d 开始", stage.Int32, k), true
		}
	}
	if allClosed {
		return "阶段全部完成", true
	}
	return fmt.Sprintf("阶段 %d 完成", stage.Int32), true
}

// OnTaskFinalFailure pushes "运行失败" (F9); for an answer run, the
// question it could not handle (R9).
func (s *TaskService) OnTaskFinalFailure(ctx context.Context, taskID pgtype.UUID) {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil || !task.IssueID.Valid {
		return
	}
	if q := superAnswerQuestionID(task); q.Valid {
		s.askUser(ctx, q)
		return
	}
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	name := ""
	if a, err := s.Queries.GetAgent(ctx, task.AgentID); err == nil {
		name = a.Name
	}
	s.PushToWorkspace(ctx, issue.WorkspaceID, "failed:"+util.UUIDToString(task.ID),
		fmt.Sprintf("%s 运行失败（%s）：%s", s.issueLink(ctx, issue), name, oneLineRunes(issue.Title, 50)))
}
