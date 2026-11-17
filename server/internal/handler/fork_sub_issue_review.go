package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (A rules 4–6): every comment route meets in
// enqueueCommentAgentTriggers, including completion reconciliation and replay
// after an edit/delete. Hold just the reviewer's trigger until the delivering
// run ends; other targets on the same comment are unaffected.
func (h *Handler) deferSubIssueReviewComment(ctx context.Context, commentID, targetID pgtype.UUID) (bool, error) {
	if !commentID.Valid || !targetID.Valid {
		return false, nil
	}
	// A merged input must NEVER be replayed, including when the source run
	// has since ended. A released input must never be deferred again.
	var outcome pgtype.Text
	err := h.DB.QueryRow(ctx, `SELECT outcome FROM fork_deferred_comment_trigger WHERE comment_id=$1 AND agent_id=$2`, commentID, targetID).Scan(&outcome)
	if err == nil {
		return outcome.String != "released", nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	comment, err := h.Queries.GetComment(ctx, commentID)
	if err != nil {
		return false, err
	}
	if comment.AuthorType != "agent" || !comment.SourceTaskID.Valid || comment.DeletedAt.Valid {
		return false, nil
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	// Serialize with the source's terminal transition. Once it can finish,
	// the durable deferred row already exists for the settlement listener.
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1 FOR SHARE`, comment.SourceTaskID).Scan(&status); err != nil {
		return false, err
	}
	if status == "completed" || status == "failed" || status == "cancelled" {
		return false, nil
	}
	q := h.Queries.WithTx(tx)
	run, err := q.GetAgentTask(ctx, comment.SourceTaskID)
	if err != nil {
		return false, err
	}
	if !run.IssueID.Valid || run.AgentID != comment.AuthorID {
		return false, nil
	}
	child, err := q.GetIssue(ctx, run.IssueID)
	if err != nil {
		return false, err
	}
	if !child.ParentIssueID.Valid || comment.WorkspaceID != child.WorkspaceID || (comment.IssueID != child.ID && comment.IssueID != child.ParentIssueID) {
		return false, nil
	}
	reviewer, err := service.SubIssueReviewer(ctx, q, child)
	if err != nil {
		return false, err
	}
	if reviewer.Kind != service.ReviewerAgent || reviewer.ID != targetID {
		return false, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO fork_deferred_comment_trigger
		(comment_id,agent_id,source_task_id,originator_user_id,workspace_id,child_issue_id)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (comment_id,agent_id) DO NOTHING`,
		commentID, targetID, run.ID, run.OriginatorUserID, child.WorkspaceID, child.ID)
	if err != nil {
		return false, err
	}
	if err = tx.QueryRow(ctx, `SELECT outcome FROM fork_deferred_comment_trigger WHERE comment_id=$1 AND agent_id=$2`, commentID, targetID).Scan(&outcome); err != nil {
		return false, err
	}
	return outcome.String != "released", tx.Commit(ctx)
}

// SettleSubIssueReviews is shared by terminal-task events and the 30-second
// sweep. The service first commits review inputs / release decisions; only
// then may the handler re-enter the ordinary comment routing path.
func (h *Handler) SettleSubIssueReviews(ctx context.Context) error {
	if err := (&service.IssueWakeupService{Tasks: h.TaskService}).SettleSubIssueReviews(ctx); err != nil {
		return err
	}
	rows, err := h.DB.Query(ctx, `SELECT comment_id,agent_id FROM fork_deferred_comment_trigger
		WHERE outcome='released' AND settled_at IS NULL ORDER BY comment_id,agent_id LIMIT 100`)
	if err != nil {
		return err
	}
	type target struct{ comment, agent pgtype.UUID }
	var targets []target
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.comment, &t.agent); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var errs []error
	for _, t := range targets {
		if err := h.releaseSubIssueReviewComment(ctx, t.comment, t.agent); err != nil {
			errs = append(errs, fmt.Errorf("release review comment %s: %w", uuidToString(t.comment), err))
		}
	}
	return errors.Join(errs...)
}

func (h *Handler) releaseSubIssueReviewComment(ctx context.Context, commentID, agentID pgtype.UUID) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked pgtype.UUID
	err = tx.QueryRow(ctx, `SELECT comment_id FROM fork_deferred_comment_trigger
		WHERE comment_id=$1 AND agent_id=$2 AND outcome='released' AND settled_at IS NULL
		FOR UPDATE SKIP LOCKED`, commentID, agentID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	finish := func() error {
		if _, err := tx.Exec(ctx, `UPDATE fork_deferred_comment_trigger SET settled_at=clock_timestamp()
			WHERE comment_id=$1 AND agent_id=$2`, commentID, agentID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	// If enqueue committed before a crash, the next sweep recognizes its
	// durable input rather than replaying a completed run a second time.
	var covered bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_task_queue WHERE agent_id=$2
		AND ($1=ANY(delivered_comment_ids) OR (status NOT IN ('completed','failed','cancelled')
		AND (trigger_comment_id=$1 OR $1=ANY(coalesced_comment_ids)))))`, commentID, agentID).Scan(&covered); err != nil {
		return err
	}
	if covered {
		return finish()
	}
	comment, err := h.Queries.GetComment(ctx, commentID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (comment.DeletedAt.Valid || isNoteComment(comment.Content))) {
		return finish()
	}
	if err != nil {
		return err
	}
	issue, err := h.Queries.GetIssue(ctx, comment.IssueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finish()
	}
	if err != nil {
		return err
	}
	var parent *db.Comment
	if comment.ParentID.Valid {
		p, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: comment.ParentID, WorkspaceID: issue.WorkspaceID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			parent = &p
		}
	}
	originator := h.TaskService.ResolveOriginatorFromTriggerComment(ctx, issue.WorkspaceID, comment.ID)
	triggers, _ := h.computeCommentAgentTriggers(ctx, issue, comment.Content, parent, comment.AuthorType, uuidToString(comment.AuthorID), commentTriggerComputeOptions{
		ExcludeTriggerCommentID: comment.ID, AuthoringTaskID: comment.SourceTaskID, OriginatorUserID: uuidToString(originator),
	})
	for _, trigger := range triggers {
		if trigger.Agent.ID != agentID {
			continue
		}
		result := h.enqueueCommentAgentTriggers(ctx, issue, comment.ID, []commentAgentTrigger{trigger})[uuidToString(agentID)]
		if result.status == DispatchBlocked {
			return fmt.Errorf("enqueue blocked: %s", result.reason)
		}
	}
	return finish()
}

func logDeferredReviewError(commentID pgtype.UUID, err error) {
	slog.Warn("defer sub-issue review comment failed; not dispatching reviewer", "comment_id", uuidToString(commentID), "error", err)
}
