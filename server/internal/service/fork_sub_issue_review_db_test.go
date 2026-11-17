package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (A rules 4–6), plan v2.4-A2-r4 §1: who reviews a sub-issue.
func TestResolveSubIssueReviewer(t *testing.T) {
	id := func(n byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{n}, Valid: true} }
	parent, la, ma, m, sqLeader := id(1), id(2), id(3), id(4), id(5)
	child := func(typ string, who pgtype.UUID) SubIssueReviewerInput {
		return SubIssueReviewerInput{ParentIssueID: parent, ChildAssigneeType: typ, ChildAssigneeID: who}
	}
	for _, tc := range []struct {
		name string
		in   SubIssueReviewerInput
		kind string
		who  pgtype.UUID
	}{
		{"top-level", SubIssueReviewerInput{ChildAssigneeType: "agent", ChildAssigneeID: ma}, ReviewerYours, pgtype.UUID{}},
		{"parent has no assignee", child("agent", ma), ReviewerYours, pgtype.UUID{}},
		{"parent is a member", func() SubIssueReviewerInput {
			in := child("agent", ma)
			in.ParentAssigneeType, in.ParentAssigneeID, in.ParentMemberPresent = "member", m, true
			return in
		}(), ReviewerMember, m},
		{"parent member left", func() SubIssueReviewerInput {
			in := child("agent", ma)
			in.ParentAssigneeType, in.ParentAssigneeID = "member", m
			return in
		}(), ReviewerYours, pgtype.UUID{}},
		{"parent agent", func() SubIssueReviewerInput {
			in := child("agent", ma)
			in.ParentAssigneeType, in.ParentAssigneeID, in.ReviewerAgentWakeable = "agent", la, true
			return in
		}(), ReviewerAgent, la},
		{"parent squad: its leader", func() SubIssueReviewerInput {
			in := child("agent", ma)
			in.ParentAssigneeType, in.ParentAssigneeID, in.ParentSquadLeaderID, in.ReviewerAgentWakeable = "squad", id(9), sqLeader, true
			return in
		}(), ReviewerAgent, sqLeader},
		{"reviewer archived or without runtime", func() SubIssueReviewerInput {
			in := child("agent", ma)
			in.ParentAssigneeType, in.ParentAssigneeID = "agent", la
			return in
		}(), ReviewerYours, pgtype.UUID{}},
		{"squad without a leader", func() SubIssueReviewerInput {
			in := child("agent", ma)
			in.ParentAssigneeType, in.ParentAssigneeID, in.ReviewerAgentWakeable = "squad", id(9), true
			return in
		}(), ReviewerYours, pgtype.UUID{}},
		{"self", func() SubIssueReviewerInput {
			in := child("agent", la)
			in.ParentAssigneeType, in.ParentAssigneeID, in.ReviewerAgentWakeable = "agent", la, true
			return in
		}(), ReviewerSelf, la},
		{"self through squads on both sides", func() SubIssueReviewerInput {
			in := SubIssueReviewerInput{ParentIssueID: parent, ChildAssigneeType: "squad", ChildAssigneeID: id(9), ChildSquadLeaderID: sqLeader}
			in.ParentAssigneeType, in.ParentAssigneeID, in.ParentSquadLeaderID, in.ReviewerAgentWakeable = "squad", id(9), sqLeader, true
			return in
		}(), ReviewerSelf, sqLeader},
	} {
		got := ResolveSubIssueReviewer(tc.in)
		if got.Kind != tc.kind || got.ID != tc.who {
			t.Errorf("%s: got %s %v, want %s %v", tc.name, got.Kind, got.ID, tc.kind, tc.who)
		}
	}
}

type reviewFixture struct {
	f                 principalFixture
	s                 *IssueWakeupService
	parent, child     pgtype.UUID
	la, ma, runtimeLA string
}

// newReviewFixture: parent P owned by agent LA, sub-issue K owned by MA.
func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	f, owner := newPrincipalFixture(t)
	if err := f.q.SeedIssueStatusEntries(context.Background(), parseTestUUID(t, f.WorkspaceID)); err != nil {
		t.Fatal(err)
	}
	la := f.privateAgentOwnedBy(t, owner, "LA")
	ma := f.privateAgentOwnedBy(t, owner, "MA")
	parent := f.Issue(t, "review parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": la})
	child := f.Issue(t, "review child", testutil.Cols{"status": "in_progress", "parent_issue_id": parent, "assignee_type": "agent", "assignee_id": ma})
	for _, issue := range []string{parent, child} {
		f.Cleanup(t, "DELETE FROM fork_deferred_comment_trigger WHERE child_issue_id=$1", issue)
		f.Cleanup(t, "DELETE FROM fork_sub_issue_review WHERE issue_id=$1", issue)
		f.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
		f.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
		f.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
		f.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
		f.Cleanup(t, "DELETE FROM issue_child_event WHERE parent_id=$1 OR child_id=$1", issue)
	}
	var rt string
	f.QueryRow(t, "SELECT runtime_id FROM agent WHERE id=$1", la).Scan(&rt)
	return &reviewFixture{f: f, s: &IssueWakeupService{Tasks: f.svc.TaskSvc}, parent: parseTestUUID(t, parent), child: parseTestUUID(t, child), la: la, ma: ma, runtimeLA: rt}
}

// run inserts a run of MA on the sub-issue.
func (r *reviewFixture) run(t *testing.T, status string, over testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{"issue_id": r.child, "status": status, "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + r.ma + "')"),
		"originator_user_id": r.f.UserID, "accountable_user_id": r.f.UserID}
	if status == "completed" || status == "failed" || status == "cancelled" {
		cols["completed_at"] = testutil.Raw("now() - interval '1 minute'")
	}
	for k, v := range over {
		cols[k] = v
	}
	return r.f.Task(t, r.ma, cols)
}

// setStatus writes the sub-issue's status as run task does (empty: a person).
func (r *reviewFixture) setStatus(t *testing.T, issue pgtype.UUID, status, task string) {
	t.Helper()
	ctx := context.Background()
	tx, err := r.f.svc.TaskSvc.TxStarter.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT set_config('multica.source_task_id',$1,true)", task); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE issue SET status=$2 WHERE id=$1", issue, status); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func (r *reviewFixture) row(t *testing.T) db.ForkSubIssueReview {
	t.Helper()
	row, err := r.f.q.GetSubIssueReview(context.Background(), r.child)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func (r *reviewFixture) end(t *testing.T, task string) {
	t.Helper()
	r.f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now()-interval '1 minute' WHERE id=$1", task)
}

func (r *reviewFixture) settle(t *testing.T) {
	t.Helper()
	if err := r.s.SettleSubIssueReviews(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (r *reviewFixture) inputs(t *testing.T) []SubIssueReviewFact {
	t.Helper()
	rows, err := r.f.svc.TaskSvc.TxStarter.(interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}).Query(context.Background(), `SELECT r.payload FROM issue_wakeup_receipt r JOIN issue_wakeup w ON w.id=r.wakeup_id
		WHERE w.issue_id=$1 AND w.system_rule='child_review' ORDER BY r.created_at`, r.parent)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []SubIssueReviewFact
	for rows.Next() {
		var raw []byte
		var fact SubIssueReviewFact
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(raw, &fact)
		out = append(out, fact)
	}
	return out
}

func (r *reviewFixture) reviewRuns(t *testing.T) []db.AgentTaskQueue {
	t.Helper()
	rows, err := r.f.svc.TaskSvc.TxStarter.(interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}).Query(context.Background(), `SELECT id FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND context->>'wakeup_system'='child_review' ORDER BY created_at`, r.parent, r.la)
	if err != nil {
		t.Fatal(err)
	}
	var ids []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []db.AgentTaskQueue
	for _, id := range ids {
		task, err := r.f.q.GetAgentTask(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, task)
	}
	return out
}

// SB1: the round counts entries into in_review; a retry is the same delivery.
func TestSubIssueReviewRound(t *testing.T) {
	r := newReviewFixture(t)
	r1 := r.run(t, "running", nil)
	r.setStatus(t, r.child, "in_review", r1)
	first := r.row(t)
	if first.ReviewRound != 1 || util.UUIDToString(first.DeliveryTaskID) != r1 || first.SettledAt.Valid {
		t.Fatalf("first delivery = %+v", first)
	}
	r.setStatus(t, r.child, "in_review", r1) // in_review again: not an entry
	if got := r.row(t); got.ReviewRound != 1 || got.EnteredAt != first.EnteredAt {
		t.Fatalf("re-set in_review changed the row: %+v", got)
	}
	// The run fails and its retry delivers again, even after being moved back.
	r.f.Exec(t, "UPDATE agent_task_queue SET status='failed',completed_at=now() WHERE id=$1", r1)
	r2 := r.run(t, "running", testutil.Cols{"parent_task_id": r1})
	r.setStatus(t, r.child, "in_progress", r2)
	r.setStatus(t, r.child, "in_review", r2)
	if got := r.row(t); got.ReviewRound != 1 || util.UUIDToString(got.DeliveryTaskID) != r1 || got.EnteredAt != first.EnteredAt {
		t.Fatalf("retry counted as a new delivery: %+v", got)
	}
	r.end(t, r2)
	r.f.Exec(t, "UPDATE issue SET assignee_type=NULL,assignee_id=NULL WHERE id=$1", r.parent) // settle without a review
	r.settle(t)
	if !r.row(t).SettledAt.Valid {
		t.Fatal("ended delivery not settled")
	}
	// Sent back, then a person delivers: round 2, no delivering run.
	r.setStatus(t, r.child, "in_progress", "")
	r.setStatus(t, r.child, "in_review", "")
	if got := r.row(t); got.ReviewRound != 2 || got.DeliveryTaskID.Valid || got.SettledAt.Valid {
		t.Fatalf("second delivery = %+v", got)
	}
	// Blocked (and done/cancelled) reset the count.
	r.setStatus(t, r.child, "blocked", "")
	if got := r.row(t); got.ReviewRound != 0 {
		t.Fatalf("blocked kept round %d", got.ReviewRound)
	}
	r.setStatus(t, r.child, "in_review", "")
	if got := r.row(t); got.ReviewRound != 1 {
		t.Fatalf("after blocked: round %d", got.ReviewRound)
	}
	// Created directly in review: the first entry.
	direct := parseTestUUID(t, r.f.Issue(t, "direct", testutil.Cols{"status": "in_review", "parent_issue_id": r.parent}))
	r.f.Cleanup(t, "DELETE FROM fork_sub_issue_review WHERE issue_id=$1", direct)
	if got, err := r.f.q.GetSubIssueReview(context.Background(), direct); err != nil || got.ReviewRound != 1 {
		t.Fatalf("created in review: %+v %v", got, err)
	}
}

// SB2: an agent reviewer is woken once the delivering run and its retries end.
func TestSubIssueReviewWakesTheParentAgentAfterTheRun(t *testing.T) {
	r := newReviewFixture(t)
	run := r.run(t, "running", nil)
	r.setStatus(t, r.child, "in_review", run)
	r.settle(t)
	if len(r.inputs(t)) != 0 {
		t.Fatal("recorded an input while the delivering run was still going")
	}
	// A failure with a retry still to come is not the end.
	r.f.Exec(t, "UPDATE agent_task_queue SET status='failed',completed_at=now() WHERE id=$1", run)
	retry := r.run(t, "queued", testutil.Cols{"parent_task_id": run})
	r.settle(t)
	if len(r.inputs(t)) != 0 {
		t.Fatal("recorded an input while a retry was pending")
	}
	r.end(t, retry)
	// The listener and the sweep can settle at once: one input.
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = r.s.SettleSubIssueReviews(context.Background()) }()
	}
	wg.Wait()
	facts := r.inputs(t)
	if len(facts) != 1 || facts[0].ReviewRound != 1 || facts[0].SourceTaskID != run || facts[0].SubIssueID != util.UUIDToString(r.child) {
		t.Fatalf("inputs = %+v", facts)
	}
	runs := r.reviewRuns(t)
	if len(runs) != 1 {
		t.Fatalf("review runs = %d", len(runs))
	}
	note := runs[0].HandoffNote.String
	if !strings.Contains(note, SubIssueReviewInstruction) || !strings.Contains(note, `"review_round":1`) || runs[0].TriggerSummary.String != "Wakeup: sub-issue ready for review" {
		t.Fatalf("review run: summary %q note %q", runs[0].TriggerSummary.String, note)
	}
	if !r.row(t).SettledAt.Valid {
		t.Fatal("delivery not settled")
	}
}

// SB2: no input for a person's review, a reviewer that cannot run, or self.
func TestSubIssueReviewNoWakeup(t *testing.T) {
	for _, tc := range []string{"no assignee", "member", "archived", "no runtime", "self", "left review"} {
		t.Run(tc, func(t *testing.T) {
			r := newReviewFixture(t)
			switch tc {
			case "no assignee":
				r.f.Exec(t, "UPDATE issue SET assignee_type=NULL,assignee_id=NULL WHERE id=$1", r.parent)
			case "member":
				r.f.Exec(t, "UPDATE issue SET assignee_type='member',assignee_id=$2 WHERE id=$1", r.parent, r.f.UserID)
			case "archived":
				r.f.Exec(t, "UPDATE agent SET archived_at=now() WHERE id=$1", r.la)
			case "no runtime":
				r.f.Exec(t, "UPDATE agent SET runtime_id=NULL WHERE id=$1", r.la)
			case "self":
				r.f.Exec(t, "UPDATE issue SET assignee_id=$2 WHERE id=$1", r.child, r.la)
			}
			run := r.run(t, "running", nil)
			r.setStatus(t, r.child, "in_review", run)
			if tc == "left review" {
				r.setStatus(t, r.child, "done", "")
			}
			r.end(t, run)
			r.settle(t)
			if n := len(r.inputs(t)); n != 0 {
				t.Fatalf("%d inputs", n)
			}
			if !r.row(t).SettledAt.Valid {
				t.Fatal("not settled")
			}
		})
	}
}

// SB2/SB3: comments the delivering run held for the reviewer ride along;
// one held for a run that did not deliver is released.
func TestSubIssueReviewMergesHeldComments(t *testing.T) {
	r := newReviewFixture(t)
	run := r.run(t, "running", nil)
	held := r.f.Comment(t, util.UUIDToString(r.parent), fmt.Sprintf("已交付 [@LA](mention://agent/%s)", r.la), testutil.Cols{"author_type": "agent", "author_id": r.ma, "source_task_id": run})
	other := r.run(t, "running", nil)
	stray := r.f.Comment(t, util.UUIDToString(r.child), "question", testutil.Cols{"author_type": "agent", "author_id": r.ma, "source_task_id": other})
	for _, c := range []struct{ comment, task string }{{held, run}, {stray, other}} {
		r.f.Exec(t, `INSERT INTO fork_deferred_comment_trigger(comment_id,agent_id,source_task_id,workspace_id,child_issue_id) VALUES($1,$2,$3,$4,$5)`,
			c.comment, r.la, c.task, r.f.WorkspaceID, r.child)
	}
	r.setStatus(t, r.child, "in_review", run)
	r.end(t, run)
	r.end(t, other)
	r.settle(t)
	facts := r.inputs(t)
	if len(facts) != 1 || len(facts[0].Comments) != 1 || facts[0].Comments[0].CommentID != held || !strings.Contains(facts[0].Comments[0].Content, "已交付") {
		t.Fatalf("inputs = %+v", facts)
	}
	outcome := func(c string) (string, bool) {
		var o pgtype.Text
		var settled bool
		r.f.QueryRow(t, "SELECT outcome, settled_at IS NOT NULL FROM fork_deferred_comment_trigger WHERE comment_id=$1", c).Scan(&o, &settled)
		return o.String, settled
	}
	if o, settled := outcome(held); o != "merged" || !settled {
		t.Fatalf("held: %s %v", o, settled)
	}
	if o, settled := outcome(stray); o != "released" || settled {
		t.Fatalf("stray: %s %v (released rows wait for the handler)", o, settled)
	}
}

// §2.4: a backlog parent keeps the input (child_done keeps consuming its
// hints), and it fires once the parent leaves backlog.
func TestSubIssueReviewKeepsInputsWhileParked(t *testing.T) {
	r := newReviewFixture(t)
	r.f.Exec(t, "UPDATE issue SET status='backlog' WHERE id=$1", r.parent)
	run := r.run(t, "running", nil)
	r.setStatus(t, r.child, "in_review", run)
	r.end(t, run)
	r.settle(t)
	rule, err := r.f.q.GetSystemWakeup(context.Background(), db.GetSystemWakeupParams{IssueID: r.parent, SystemRule: systemRuleText(SystemRuleChildReview)})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := r.s.dispatchSystem(context.Background(), rule); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", rule.ID); n != 1 {
		t.Fatalf("pending inputs while parked = %d", n)
	}
	if len(r.reviewRuns(t)) != 0 {
		t.Fatal("ran on a backlog parent")
	}
	if pending, err := r.s.pendingInputs(context.Background(), []pgtype.UUID{parseTestUUID(t, r.f.WorkspaceID)}, 100); err != nil || pending.OldestReady.Valid || pending.Held != 1 {
		t.Fatalf("parked input reported as backlog: %+v %v", pending, err)
	}
	// child_done in the same backlog still consumes its hints (upstream).
	if err := r.s.ProcessChildEvents(context.Background(), r.parent); err != nil {
		t.Fatal(err)
	}
	if n := r.f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt r JOIN issue_wakeup w ON w.id=r.wakeup_id WHERE w.issue_id=$1 AND w.system_rule='child_done' AND r.processed_at IS NULL", r.parent); n != 0 {
		t.Fatalf("child_done kept %d hints in backlog", n)
	}
	r.f.Exec(t, "UPDATE issue SET status='todo' WHERE id=$1", r.parent)
	if err := r.s.dispatchSystem(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	runs := r.reviewRuns(t)
	if len(runs) != 1 || !strings.Contains(runs[0].HandoffNote.String, `"review_round":1`) {
		t.Fatalf("after backlog: %d runs", len(runs))
	}
}

// §2.4: an input whose delivery already has a result is dropped (no_action),
// and a queued review run whose deliveries all went stale is not claimed.
func TestSubIssueReviewDropsStaleInputs(t *testing.T) {
	r := newReviewFixture(t)
	r.f.Exec(t, "UPDATE issue SET status='backlog' WHERE id=$1", r.parent)
	run := r.run(t, "running", nil)
	r.setStatus(t, r.child, "in_review", run)
	r.end(t, run)
	r.settle(t)
	r.setStatus(t, r.child, "done", "") // a person accepted it meanwhile
	r.f.Exec(t, "UPDATE issue SET status='todo' WHERE id=$1", r.parent)
	rule, _ := r.f.q.GetSystemWakeup(context.Background(), db.GetSystemWakeupParams{IssueID: r.parent, SystemRule: systemRuleText(SystemRuleChildReview)})
	if err := r.s.dispatchSystem(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	if len(r.reviewRuns(t)) != 0 {
		t.Fatal("stale input started a run")
	}
	if n := r.f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", rule.ID); n != 0 {
		t.Fatalf("stale input left pending: %d", n)
	}
	if n := r.f.Count(t, "SELECT count(*) FROM activity_log WHERE issue_id=$1 AND details->>'outcome'='no_action'", r.parent); n != 1 {
		t.Fatalf("no_action entries = %d", n)
	}

	// A queued review run whose delivery got a result before it was claimed.
	r2 := newReviewFixture(t)
	run2 := r2.run(t, "running", nil)
	r2.setStatus(t, r2.child, "in_review", run2)
	r2.end(t, run2)
	r2.settle(t)
	runs := r2.reviewRuns(t)
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
	if err := r2.s.CheckClaim(context.Background(), runs[0]); err != nil {
		t.Fatalf("live review run refused: %v", err)
	}
	r2.setStatus(t, r2.child, "in_progress", "")
	if err := r2.s.CheckClaim(context.Background(), runs[0]); err == nil {
		t.Fatal("stale review run claimable")
	}
}

// A29, §2.4: a second delivery joins the queued review run; no hourly pause.
func TestSubIssueReviewMergesIntoTheQueuedRunAndNeverPauses(t *testing.T) {
	r := newReviewFixture(t)
	k2 := parseTestUUID(t, r.f.Issue(t, "review child 2", testutil.Cols{"status": "in_progress", "parent_issue_id": r.parent, "assignee_type": "agent", "assignee_id": r.ma}))
	r.f.Cleanup(t, "DELETE FROM fork_sub_issue_review WHERE issue_id=$1", k2)
	first := r.run(t, "running", nil)
	r.setStatus(t, r.child, "in_review", first)
	r.end(t, first)
	r.settle(t)
	second := r.f.Task(t, r.ma, testutil.Cols{"issue_id": k2, "status": "running", "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + r.ma + "')"),
		"originator_user_id": r.f.UserID, "accountable_user_id": r.f.UserID})
	r.setStatus(t, k2, "in_review", second)
	r.end(t, second)
	r.settle(t)
	runs := r.reviewRuns(t)
	if len(runs) != 1 || strings.Count(runs[0].HandoffNote.String, "\n"+SubIssueReviewEventType+" {") != 2 {
		t.Fatalf("runs = %d, note %q", len(runs), runs[0].HandoffNote.String)
	}
	// Twelve review runs this hour already: the next delivery still runs.
	rule, _ := r.f.q.GetSystemWakeup(context.Background(), db.GetSystemWakeupParams{IssueID: r.parent, SystemRule: systemRuleText(SystemRuleChildReview)})
	r.f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", runs[0].ID)
	for range wakeupHourlyRunLimit {
		r.f.Task(t, r.la, testutil.Cols{"issue_id": r.parent, "status": "completed", "runtime_id": r.runtimeLA, "completed_at": testutil.Raw("now()"),
			"context": fmt.Sprintf(`{"wakeup_id":"%s","wakeup_system":"child_review"}`, util.UUIDToString(rule.ID))})
	}
	r.setStatus(t, r.child, "in_progress", "")
	third := r.run(t, "running", nil)
	r.setStatus(t, r.child, "in_review", third)
	r.end(t, third)
	r.settle(t)
	got, _ := r.f.q.LocklessWakeup(context.Background(), rule.ID)
	if got.PausedReason.Valid || !got.Enabled {
		t.Fatalf("review rule paused: %+v", got.PausedReason)
	}
	if n := r.f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status='queued' AND context->>'wakeup_system'='child_review'", r.parent); n != 1 {
		t.Fatalf("queued review runs = %d", n)
	}
}
