package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (D3, D10, D14): when every instance of a batch has finished, post one
// summary comment and wake the merge party once.

// OnTaskTerminal is called for task:completed, task:cancelled and a final
// task:failed. Only instance rows matter.
func (s *TaskService) OnTaskTerminal(ctx context.Context, taskID pgtype.UUID) {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil || !task.BatchID.Valid {
		return
	}
	if _, err := s.TrySettleBatch(ctx, task.BatchID); err != nil {
		slog.Warn("instance batch settle failed; the sweep will retry", "batch_id", util.UUIDToString(task.BatchID), "error", err)
	}
}

// SweepUnsettledBatches settles batches whose terminal event was lost (the
// event bus is in-memory). Run by the scheduler every 30 seconds.
func (s *TaskService) SweepUnsettledBatches(ctx context.Context) error {
	ids, err := s.Queries.ListSettleableTaskBatches(ctx, 50)
	if err != nil {
		return fmt.Errorf("list settleable batches: %w", err)
	}
	for _, id := range ids {
		if _, err := s.TrySettleBatch(ctx, id); err != nil {
			slog.Warn("instance batch sweep: settle failed", "batch_id", util.UUIDToString(id), "error", err)
		}
	}
	return nil
}

// TrySettleBatch settles the batch when the latest attempt of every instance
// is terminal. It returns true only for the one call that settled it.
func (s *TaskService) TrySettleBatch(ctx context.Context, batchID pgtype.UUID) (bool, error) {
	batch, err := s.Queries.GetAgentTaskBatch(ctx, batchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load batch: %w", err)
	}
	if batch.SettledAt.Valid {
		return false, nil
	}
	rows, err := s.Queries.ListBatchTasks(ctx, []pgtype.UUID{batchID})
	if err != nil {
		return false, fmt.Errorf("list batch tasks: %w", err)
	}
	// Rows are ordered by (index, created_at): the last row per index is the
	// latest attempt. A retry is created in the same transaction as its
	// parent's failure, so a final failure has no newer row.
	latest := make([]*db.AgentTaskQueue, len(batch.Splits))
	for i := range rows {
		if idx := int(rows[i].BatchIndex.Int16) - 1; idx >= 0 && idx < len(latest) {
			latest[idx] = &rows[i]
		}
	}
	for _, t := range latest {
		if t == nil || !isTerminalStatus(t.Status) {
			return false, nil
		}
	}
	if _, err := s.Queries.SettleAgentTaskBatch(ctx, batchID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // another caller settled it
		}
		return false, fmt.Errorf("settle batch: %w", err)
	}
	// ponytail: settle-then-wake. A crash between the two loses the wake-up
	// but never doubles it, which is what D3 asks for; make it one
	// transaction if lost wake-ups show up in practice.
	s.wakeMergeParty(ctx, batch, latest, rows)
	return true, nil
}

func isTerminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

func (s *TaskService) wakeMergeParty(ctx context.Context, batch db.AgentTaskBatch, latest []*db.AgentTaskQueue, rows []db.AgentTaskQueue) {
	log := slog.With("batch_id", util.UUIDToString(batch.ID), "issue_id", util.UUIDToString(batch.IssueID))
	issue, err := s.Queries.GetIssue(ctx, batch.IssueID)
	if err != nil {
		log.Warn("instance batch settled but its issue is gone", "error", err)
		return
	}
	slug := ""
	if ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID); err == nil {
		slug = ws.Slug
	}
	// Comments from every attempt of an instance, by instance index.
	indexOf := make(map[pgtype.UUID]int16, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	for i, r := range rows {
		indexOf[r.ID], ids[i] = r.BatchIndex.Int16, r.ID
	}
	comments, err := s.Queries.ListCommentsBySourceTasks(ctx, db.ListCommentsBySourceTasksParams{IssueID: issue.ID, TaskIds: ids})
	if err != nil {
		log.Warn("instance batch summary: list comments failed", "error", err)
	}
	links := map[int16][]string{}
	for _, c := range comments {
		idx := indexOf[c.SourceTaskID]
		links[idx] = append(links[idx], fmt.Sprintf("/%s/issues/%s#comment-%s", slug, util.UUIDToString(issue.ID), util.UUIDToString(c.ID)))
	}
	content := batchSummary(batch, latest, links)

	created, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: dbid.NewV7(), IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		AuthorType: "system", AuthorID: pgtype.UUID{Valid: true},
		Content: content, Type: "system",
	})
	if err != nil {
		log.Error("instance batch summary comment failed; merge party not woken", "error", err)
		return
	}
	comment := created.Comment()
	if s.Bus != nil {
		s.Bus.Publish(events.Event{
			Type: protocol.EventCommentCreated, WorkspaceID: util.UUIDToString(issue.WorkspaceID), ActorType: "system",
			Payload: map[string]any{
				"comment": commentEventFields(comment), "issue_title": issue.Title,
				"issue_status": issue.Status, "issue_revision": created.IssueRevision,
			},
		})
	}

	agentID, squadID, isLeader := s.mergeParty(ctx, batch, issue)
	if !agentID.Valid {
		log.Info("instance batch settled; no merge agent (member or no assignee)")
		return
	}
	actor := batch.RequestedByID
	if batch.RequestedByType == "agent" {
		actor = latest[0].OriginatorUserID
	}
	if _, err := s.enqueueMentionTask(ctx, issue, agentID, comment.ID, isLeader, squadID, false, "", actor, pgtype.UUID{}, OriginDerived); err != nil {
		log.Warn("instance batch settled; merge party enqueue failed", "agent_id", util.UUIDToString(agentID), "error", err)
		return
	}
	log.Info("instance batch settled; merge party woken", "agent_id", util.UUIDToString(agentID))
}

// mergeParty: an agent-requested batch wakes the requester; a member-requested
// one wakes the assignee, or the leader of the assigned squad; a member or no
// assignee wakes nobody (plan 2.3, D3, D14).
func (s *TaskService) mergeParty(ctx context.Context, batch db.AgentTaskBatch, issue db.Issue) (agentID, squadID pgtype.UUID, isLeader bool) {
	var squad *db.Squad
	if issue.AssigneeType.String == "squad" && issue.AssigneeID.Valid {
		if sq, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID}); err == nil {
			squad = &sq
		}
	}
	if batch.RequestedByType == "agent" {
		agentID = batch.RequestedByID
	} else if issue.AssigneeType.String == "agent" {
		agentID = issue.AssigneeID
	} else if squad != nil {
		agentID = squad.LeaderID
	}
	if squad != nil && agentID.Valid && squad.LeaderID == agentID {
		return agentID, squad.ID, true
	}
	return agentID, pgtype.UUID{}, false
}

var instanceStatusLabel = map[string]string{"completed": "已完成", "failed": "失败", "cancelled": "已取消"}

func batchSummary(batch db.AgentTaskBatch, latest []*db.AgentTaskQueue, links map[int16][]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "加开的 %d 个实例都已结束：\n\n", len(batch.Splits))
	for i, t := range latest {
		split := []rune(strings.ReplaceAll(batch.Splits[i], "\n", " "))
		if len(split) > 80 {
			split = append(split[:80], '…')
		}
		fmt.Fprintf(&b, "- 实例 %d/%d「%s」：%s", i+1, len(batch.Splits), string(split), instanceStatusLabel[t.Status])
		if l := links[int16(i+1)]; len(l) > 0 {
			parts := make([]string, len(l))
			for j, u := range l {
				parts[j] = fmt.Sprintf("[评论 %d](%s)", j+1, u)
			}
			b.WriteString("，" + strings.Join(parts, "、"))
		} else {
			b.WriteString("，没有评论")
		}
		if t.BranchName.Valid && t.BranchName.String != "" {
			fmt.Fprintf(&b, "，分支 `%s`", t.BranchName.String)
		}
		b.WriteString("\n")
	}
	return b.String()
}
