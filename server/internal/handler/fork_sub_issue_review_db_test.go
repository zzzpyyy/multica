package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (A24,A33,A36): all comment enqueue paths must respect the same
// durable deferral, including a reply routed implicitly back to the leader.
func TestSubIssueReviewCommentDeferral(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	for _, location := range []string{"child", "parent", "implicit"} {
		t.Run(location, func(t *testing.T) {
			leader := dbfx.Agent(t, "review leader "+location, testRuntimeID)
			worker := dbfx.Agent(t, "review worker "+location, testRuntimeID)
			other := dbfx.Agent(t, "review other "+location, testRuntimeID)
			squad := dbfx.Squad(t, "review squad "+location, leader)
			parentID := dbfx.Issue(t, "review parent "+location, testutil.Cols{"status": "in_progress", "assignee_type": "squad", "assignee_id": squad})
			childID := dbfx.Issue(t, "review child "+location, testutil.Cols{"status": "in_progress", "parent_issue_id": parentID, "assignee_type": "agent", "assignee_id": worker})
			leaderRun := dbfx.Task(t, leader, testutil.Cols{"issue_id": parentID, "runtime_id": testRuntimeID, "status": "running", "is_leader_task": true, "squad_id": squad, "originator_user_id": testUserID, "accountable_user_id": testUserID})
			delegation := dbfx.Comment(t, childID, "please rework", testutil.Cols{"author_type": "agent", "author_id": leader, "source_task_id": leaderRun})
			runID := dbfx.Task(t, worker, testutil.Cols{"issue_id": childID, "runtime_id": testRuntimeID, "status": "running", "trigger_comment_id": delegation, "delegated_from_task_id": leaderRun, "squad_id": squad, "originator_user_id": testUserID, "accountable_user_id": testUserID})
			commentIssue := childID
			if location == "parent" {
				commentIssue = parentID
			}
			cols := testutil.Cols{"author_type": "agent", "author_id": worker, "source_task_id": runID}
			content := fmt.Sprintf("delivered [@leader](mention://agent/%s) [@other](mention://agent/%s)", leader, other)
			if location == "implicit" {
				cols["parent_id"] = delegation
				content = "delivered"
			}
			commentID := dbfx.Comment(t, commentIssue, content, cols)
			dbfx.Cleanup(t, `DELETE FROM fork_deferred_comment_trigger WHERE child_issue_id=$1`, childID)
			issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(commentIssue))
			if err != nil {
				t.Fatal(err)
			}
			comment, err := testHandler.Queries.GetComment(ctx, parseUUID(commentID))
			if err != nil {
				t.Fatal(err)
			}
			var pc *db.Comment
			if location == "implicit" {
				p, err := testHandler.Queries.GetComment(ctx, parseUUID(delegation))
				if err != nil {
					t.Fatal(err)
				}
				pc = &p
			}
			triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, content, pc, "agent", worker, commentTriggerComputeOptions{ExcludeTriggerCommentID: comment.ID, AuthoringTaskID: parseUUID(runID), OriginatorUserID: testUserID})
			found := false
			for _, tr := range triggers {
				if tr.Agent.ID == parseUUID(leader) {
					found = true
				}
			}
			if !found {
				t.Fatalf("leader not routed: %+v", triggers)
			}
			result := testHandler.enqueueCommentAgentTriggers(ctx, issue, comment.ID, triggers)
			if got := result[leader]; got.status != DispatchDeferred || got.reason != ReasonDeferred {
				t.Fatalf("review target = %+v", got)
			}
			if location != "implicit" && result[other].status != DispatchQueued {
				t.Fatalf("other target = %+v", result[other])
			}
			assertNoLeaderOnChild := func() {
				t.Helper()
				var n int
				dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, childID, leader).Scan(&n)
				if n != 0 {
					t.Fatalf("leader ran on child: %d", n)
				}
			}
			assertNoLeaderOnChild()
			// Replay with the same target list while still deferred.
			if got := testHandler.enqueueCommentAgentTriggers(ctx, issue, comment.ID, triggers)[leader]; got.status != DispatchDeferred {
				t.Fatalf("replay = %+v", got)
			}
			// Cancellation-survivor replay must go through the exact same gate.
			testHandler.retriggerCancelledTaskSurvivors(ctx, issue, []db.AgentTaskQueue{{AgentID: parseUUID(leader), TriggerCommentID: comment.ID}}, pgtype.UUID{})
			// Simulate a completed leader run that never received this comment.
			lr, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(leaderRun))
			if err != nil {
				t.Fatal(err)
			}
			lr.IssueID = issue.ID
			lr.CreatedAt = pgtype.Timestamptz{Time: comment.CreatedAt.Time.Add(-1e9), Valid: true}
			lr.CoalescedCommentIds = []pgtype.UUID{comment.ID}
			testHandler.reconcileCommentsOnCompletion(ctx, &lr)
			assertNoLeaderOnChild()
			// A merged receipt stays suppressed even after the author ended.
			dbfx.Exec(t, `UPDATE fork_deferred_comment_trigger SET outcome='merged',settled_at=now() WHERE comment_id=$1`, comment.ID)
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, runID)
			if got := testHandler.enqueueCommentAgentTriggers(ctx, issue, comment.ID, triggers)[leader]; got.status != DispatchDeferred {
				t.Fatalf("merged replay = %+v", got)
			}
			assertNoLeaderOnChild()
		})
	}
}

func TestSubIssueReviewCommentRelease(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leader := dbfx.Agent(t, "review release leader", testRuntimeID)
	worker := dbfx.Agent(t, "review release worker", testRuntimeID)
	parentID := dbfx.Issue(t, "review release parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": leader})
	childID := dbfx.Issue(t, "review release child", testutil.Cols{"status": "in_progress", "parent_issue_id": parentID, "assignee_type": "agent", "assignee_id": worker})
	runID := dbfx.Task(t, worker, testutil.Cols{"issue_id": childID, "runtime_id": testRuntimeID, "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	commentID := dbfx.Comment(t, childID, fmt.Sprintf("question [@leader](mention://agent/%s)", leader), testutil.Cols{"author_type": "agent", "author_id": worker, "source_task_id": runID})
	dbfx.Cleanup(t, `DELETE FROM fork_deferred_comment_trigger WHERE child_issue_id=$1`, childID)
	held, err := testHandler.deferSubIssueReviewComment(ctx, parseUUID(commentID), parseUUID(leader))
	if err != nil || !held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, runID)
	if err := testHandler.SettleSubIssueReviews(ctx); err != nil {
		t.Fatal(err)
	}
	var outcome string
	var settled bool
	dbfx.QueryRow(t, `SELECT outcome,settled_at IS NOT NULL FROM fork_deferred_comment_trigger WHERE comment_id=$1 AND agent_id=$2`, commentID, leader).Scan(&outcome, &settled)
	if outcome != "released" || !settled {
		t.Fatalf("release=%s settled=%v", outcome, settled)
	}
	if n := pendingTaskCountForAgentIssue(t, childID, leader); n != 1 {
		t.Fatalf("release runs=%d", n)
	}
	if err := testHandler.SettleSubIssueReviews(ctx); err != nil {
		t.Fatal(err)
	}
	if n := pendingTaskCountForAgentIssue(t, childID, leader); n != 1 {
		t.Fatalf("repeated release runs=%d", n)
	}
}
