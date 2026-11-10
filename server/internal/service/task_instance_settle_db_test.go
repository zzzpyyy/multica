package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// Fork (D3, D10, D14): one summary comment and exactly one merge-party run
// per batch, only after every instance's latest attempt is terminal.

func (f *instanceFixture) mergeRuns(issueID, agentID string) int {
	return f.fx.Count(f.t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND batch_id IS NULL`, issueID, agentID)
}

func (f *instanceFixture) finish(taskID, status string) {
	f.fx.Exec(f.t, `UPDATE agent_task_queue SET status=$2, completed_at=now() WHERE id=$1`, taskID, status)
}

func TestBatchSettlesOnceAndWakesAssignee(t *testing.T) {
	f := newInstanceFixture(t, 3)
	ctx := context.Background()
	tl := f.fx.Agent(t, "TL", f.runtimeID, testutil.Cols{"max_concurrent_tasks": 3})
	x := f.fx.Issue(t, "X", testutil.Cols{"assignee_type": "agent", "assignee_id": tl, "status": "backlog"})
	f.fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, x)
	f.fx.Cleanup(t, `DELETE FROM comment WHERE issue_id=$1`, x)
	b := f.batch(x, 3)
	i1 := f.instance(x, b, 1, "running", 60, nil)
	i2 := f.instance(x, b, 2, "running", 60, nil)
	i3 := f.instance(x, b, 3, "running", 60, nil)
	f.fx.Comment(t, x, "完成：x1", testutil.Cols{"author_type": "agent", "author_id": f.agentID, "source_task_id": i1})

	f.finish(i1, "completed")
	f.finish(i2, "cancelled") // D10
	f.svc.OnTaskTerminal(ctx, util.MustParseUUID(i2))
	if n := f.mergeRuns(x, tl); n != 0 {
		t.Fatalf("merge party woken with an instance still running: %d runs", n)
	}

	// A failed attempt with a retry queued is not the end.
	f.finish(i3, "failed")
	retry := f.instance(x, b, 3, "queued", 0, testutil.Cols{"branch_name": "agent/tb/abc123"})
	f.svc.OnTaskTerminal(ctx, util.MustParseUUID(i3))
	if n := f.mergeRuns(x, tl); n != 0 {
		t.Fatalf("merge party woken while a retry is queued: %d", n)
	}

	f.finish(retry, "completed")
	var wg sync.WaitGroup
	wins := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := f.svc.TrySettleBatch(ctx, util.MustParseUUID(b))
			if err != nil {
				t.Error(err)
			}
			wins <- ok
		}()
	}
	wg.Wait()
	close(wins)
	won := 0
	for ok := range wins {
		if ok {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("settle winners = %d, want exactly 1", won)
	}
	if n := f.mergeRuns(x, tl); n != 1 {
		t.Fatalf("merge runs = %d, want exactly 1", n)
	}
	f.svc.OnTaskTerminal(ctx, util.MustParseUUID(retry))
	if err := f.svc.SweepUnsettledBatches(ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.mergeRuns(x, tl); n != 1 {
		t.Fatalf("merge runs after replays = %d, want 1", n)
	}

	var content, triggerContent string
	f.fx.QueryRow(t, `SELECT content FROM comment WHERE issue_id=$1 AND author_type='system'`, x).Scan(&content)
	f.fx.QueryRow(t, `SELECT c.content FROM agent_task_queue q JOIN comment c ON c.id=q.trigger_comment_id
		WHERE q.issue_id=$1 AND q.agent_id=$2 AND q.batch_id IS NULL`, x, tl).Scan(&triggerContent)
	if triggerContent != content {
		t.Fatal("merge run is not triggered by the summary comment")
	}
	for _, want := range []string{"实例 1/3", "已完成", "实例 2/3", "已取消", "实例 3/3", "#comment-", "agent/tb/abc123"} {
		if !strings.Contains(content, want) {
			t.Errorf("summary missing %q:\n%s", want, content)
		}
	}
}

func TestBatchMergeParty(t *testing.T) {
	f := newInstanceFixture(t, 3)
	ctx := context.Background()
	leader := f.fx.Agent(t, "Leader", f.runtimeID, testutil.Cols{"max_concurrent_tasks": 3})
	tl := f.fx.Agent(t, "TL", f.runtimeID, testutil.Cols{"max_concurrent_tasks": 3})
	squad := f.fx.Squad(t, "S", leader)

	run := func(name string, assignee testutil.Cols, requestedBy testutil.Cols) (string, string) {
		cols := testutil.Cols{"status": "backlog"}
		for k, v := range assignee {
			cols[k] = v
		}
		x := f.fx.Issue(t, name, cols)
		f.fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, x)
		f.fx.Cleanup(t, `DELETE FROM comment WHERE issue_id=$1`, x)
		b := f.batch(x, 1)
		if requestedBy != nil {
			f.fx.Exec(t, `UPDATE agent_task_batch SET requested_by_type=$2, requested_by_id=$3 WHERE id=$1`, b, requestedBy["type"], requestedBy["id"])
		}
		i := f.instance(x, b, 1, "completed", 10, testutil.Cols{"originator_user_id": f.userID, "accountable_user_id": f.userID, "originator_source": "direct_human"})
		f.svc.OnTaskTerminal(ctx, util.MustParseUUID(i))
		return x, b
	}

	x, _ := run("squad", testutil.Cols{"assignee_type": "squad", "assignee_id": squad}, nil)
	if n := f.mergeRuns(x, leader); n != 1 {
		t.Fatalf("squad assignee: leader runs = %d, want 1", n)
	}
	var isLeader bool
	f.fx.QueryRow(t, `SELECT is_leader_task FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, x, leader).Scan(&isLeader)
	if !isLeader {
		t.Fatal("squad leader merge run is not a leader task")
	}

	x, _ = run("agent requested", testutil.Cols{"assignee_type": "squad", "assignee_id": squad}, testutil.Cols{"type": "agent", "id": tl})
	if a, b := f.mergeRuns(x, tl), f.mergeRuns(x, leader); a != 1 || b != 0 {
		t.Fatalf("agent-requested: requester runs %d, leader runs %d; want 1, 0", a, b)
	}

	x, _ = run("member assignee", testutil.Cols{"assignee_type": "member", "assignee_id": f.userID}, nil)
	if n := f.fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND batch_id IS NULL`, x); n != 0 {
		t.Fatalf("member assignee: %d runs, want 0", n)
	}
	if n := f.fx.Count(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND author_type='system'`, x); n != 1 {
		t.Fatalf("member assignee: %d summary comments, want 1", n)
	}
}
