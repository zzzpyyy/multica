package service

import (
	"context"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (D): CreateInstances validation and the GET /instances read model.

func (f *instanceFixture) loadIssue(id string) db.Issue {
	f.t.Helper()
	issue, err := f.svc.Queries.GetIssue(context.Background(), util.MustParseUUID(id))
	if err != nil {
		f.t.Fatal(err)
	}
	return issue
}

func (f *instanceFixture) loadAgent(id string) db.Agent {
	f.t.Helper()
	a, err := f.svc.Queries.GetAgent(context.Background(), util.MustParseUUID(id))
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

func (f *instanceFixture) claudeRuntime() {
	f.fx.Exec(f.t, `UPDATE agent_runtime SET provider = 'claude' WHERE id = $1`, f.runtimeID)
}

func wantInstanceErr(t *testing.T, err error, status int, code string) {
	t.Helper()
	var ie *InstanceError
	if !errors.As(err, &ie) || ie.Status != status || ie.Code != code {
		t.Fatalf("err = %v, want %d %q", err, status, code)
	}
}

func (f *instanceFixture) memberReq(issueID string, splits ...string) InstanceRequest {
	return InstanceRequest{
		Issue: f.loadIssue(issueID), Agent: f.loadAgent(f.agentID), Splits: splits,
		MemberID: util.MustParseUUID(f.userID),
	}
}

func (f *instanceFixture) activeRuns(issueID string) int {
	return f.fx.Count(f.t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued','dispatched','running')`, issueID)
}

// D11: k outside 1..N or an empty split is refused and nothing is queued.
func TestCreateInstancesValidation(t *testing.T) {
	f := newInstanceFixture(t, 3)
	f.claudeRuntime()
	x := f.issue("X")
	ctx := context.Background()

	_, _, err := f.svc.CreateInstances(ctx, f.memberReq(x, "a", "b", "c", "d"))
	wantInstanceErr(t, err, 400, "instance_count_out_of_range")
	_, _, err = f.svc.CreateInstances(ctx, f.memberReq(x))
	wantInstanceErr(t, err, 400, "instance_count_out_of_range")
	_, _, err = f.svc.CreateInstances(ctx, f.memberReq(x, "a", "  "))
	wantInstanceErr(t, err, 400, "invalid_split")
	if n := f.activeRuns(x); n != 0 {
		t.Fatalf("runs after refused requests = %d, want 0", n)
	}

	batch, tasks, err := f.svc.CreateInstances(ctx, f.memberReq(x, " a ", "b", "c"))
	if err != nil {
		t.Fatalf("CreateInstances: %v", err)
	}
	f.fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE batch_id = $1`, util.UUIDToString(batch.ID))
	f.fx.Cleanup(t, `DELETE FROM agent_task_batch WHERE id = $1`, util.UUIDToString(batch.ID))
	if len(tasks) != 3 || batch.Splits[0] != "a" || batch.RequestedByType != "member" {
		t.Fatalf("batch = %+v, %d tasks", batch, len(tasks))
	}
	for i, task := range tasks {
		if task.BatchIndex.Int16 != int16(i+1) || !task.ForceFreshSession || task.Status != "queued" || task.Priority >= InstancePriorityBoost {
			t.Fatalf("task %d = index %v fresh %v status %s priority %d", i, task.BatchIndex, task.ForceFreshSession, task.Status, task.Priority)
		}
		if util.UUIDToString(task.OriginatorUserID) != f.userID {
			t.Fatalf("task %d originator = %s, want the member", i, util.UUIDToString(task.OriginatorUserID))
		}
	}
}

// P2 and the super-agent: refused before anything is queued.
func TestInstanceAdmission(t *testing.T) {
	f := newInstanceFixture(t, 3)
	x := f.issue("X")
	ctx := context.Background()

	_, _, err := f.svc.CreateInstances(ctx, f.memberReq(x, "a"))
	wantInstanceErr(t, err, 400, "runtime_unsupported") // fixture runtime is not Claude

	f.claudeRuntime()
	f.fx.Exec(t, `UPDATE agent_runtime SET daemon_id = 'daemon-1' WHERE id = $1`, f.runtimeID)
	project := f.fx.Project(t, "P")
	f.fx.Exec(t, `UPDATE issue SET project_id = $1 WHERE id = $2`, project, x)
	res := f.fx.Insert(t, "project_resource", testutil.Cols{
		"workspace_id": f.fx.WorkspaceID, "project_id": project, "resource_type": "local_directory",
		"resource_ref": testutil.Raw(`'{"local_path":"/src/app","daemon_id":"daemon-1"}'::jsonb`),
	})
	_, _, err = f.svc.CreateInstances(ctx, f.memberReq(x, "a"))
	wantInstanceErr(t, err, 400, "runtime_unsupported")

	f.fx.Exec(t, `UPDATE project_resource SET resource_ref = '{"local_path":"/src/app","daemon_id":"daemon-1","execution_mode":"worktree"}'::jsonb WHERE id = $1`, res)
	if err := f.svc.CheckInstanceAdmission(ctx, f.loadIssue(x), f.loadAgent(f.agentID)); err != nil {
		t.Fatalf("worktree local directory refused: %v", err)
	}

	f.fx.Exec(t, `UPDATE agent SET system_key = 'super' WHERE id = $1`, f.agentID)
	_, _, err = f.svc.CreateInstances(ctx, f.memberReq(x, "a"))
	wantInstanceErr(t, err, 400, "super_agent_not_assignable")
	if n := f.activeRuns(x); n != 0 {
		t.Fatalf("runs after refused requests = %d, want 0", n)
	}
}

// D14, D15: an agent caller must own the issue and cannot set priority.
func TestCreateInstancesFromAgent(t *testing.T) {
	f := newInstanceFixture(t, 3)
	f.claudeRuntime()
	ctx := context.Background()
	leader := f.fx.Agent(t, "TL", f.runtimeID, testutil.Cols{"max_concurrent_tasks": 3})
	x := f.fx.Issue(t, "X", testutil.Cols{"assignee_type": "agent", "assignee_id": leader, "status": "backlog"})
	other := f.fx.Issue(t, "Y")
	caller := f.fx.Task(t, leader, testutil.Cols{"runtime_id": f.runtimeID, "issue_id": x, "status": "running",
		"originator_user_id": f.userID, "accountable_user_id": f.userID, "originator_source": "direct_human"})
	task, err := f.svc.Queries.GetAgentTask(ctx, util.MustParseUUID(caller))
	if err != nil {
		t.Fatal(err)
	}
	req := func(issueID string, priority bool) InstanceRequest {
		return InstanceRequest{Issue: f.loadIssue(issueID), Agent: f.loadAgent(f.agentID), Splits: []string{"a", "b"}, Priority: priority, CallerTask: &task}
	}

	_, _, err = f.svc.CreateInstances(ctx, req(x, true))
	wantInstanceErr(t, err, 403, "priority_requires_member")
	_, _, err = f.svc.CreateInstances(ctx, req(other, false))
	wantInstanceErr(t, err, 403, "not_issue_owner")

	batch, tasks, err := f.svc.CreateInstances(ctx, req(x, false))
	if err != nil {
		t.Fatalf("CreateInstances from owner agent: %v", err)
	}
	f.fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE batch_id = $1`, util.UUIDToString(batch.ID))
	f.fx.Cleanup(t, `DELETE FROM agent_task_batch WHERE id = $1`, util.UUIDToString(batch.ID))
	if batch.RequestedByType != "agent" || util.UUIDToString(batch.RequestedByID) != leader {
		t.Fatalf("requested_by = %s %s, want agent %s", batch.RequestedByType, util.UUIDToString(batch.RequestedByID), leader)
	}
	if util.UUIDToString(tasks[0].DelegatedFromTaskID) != caller || util.UUIDToString(tasks[0].OriginatorUserID) != f.userID {
		t.Fatalf("attribution = delegated %s originator %s", util.UUIDToString(tasks[0].DelegatedFromTaskID), util.UUIDToString(tasks[0].OriginatorUserID))
	}
}

// D5, D9, D16: position follows the claim order; the reason names the cap.
func TestListInstancesQueue(t *testing.T) {
	f := newInstanceFixture(t, 3)
	f.claudeRuntime()
	ctx := context.Background()
	x, y, z := f.issue("X"), f.issue("Y"), f.issue("Z")
	by := f.batch(y, 1)
	f.instance(y, by, 1, "running", 600, testutil.Cols{"session_id": "s-y", "work_dir": "/w/y"})
	bx := f.batch(x, 3)
	f.instance(x, bx, 1, "running", 300, nil)
	f.instance(x, bx, 2, "running", 300, nil)
	x3 := f.instance(x, bx, 3, "queued", 300, nil)
	bz := f.batch(z, 1)
	z1 := f.instance(z, bz, 1, "queued", 200, nil)

	views, err := f.svc.ListInstances(ctx, f.loadIssue(x))
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Total != 3 || views[0].Agent.Name != "TB" || views[0].RequestedBy.Type != "member" {
		t.Fatalf("views = %+v", views)
	}
	inst := views[0].Instances
	if inst[0].Status != "running" || inst[2].Status != "queued" || inst[2].TaskID != x3 || len(inst[2].TaskIDs) != 1 {
		t.Fatalf("instances = %+v", inst)
	}
	q := inst[2].Queue
	// Z has nothing running, so it is ahead of X's third instance (D6).
	if q == nil || q.Position != 2 || q.Reason != QueueReasonRoleFull || *q.Running != 3 || *q.Limit != 3 {
		t.Fatalf("queue = %+v, want position 2, role_full 3/3", q)
	}
	zv, _ := f.svc.ListInstances(ctx, f.loadIssue(z))
	if zv[0].Instances[0].TaskID != z1 || zv[0].Instances[0].Queue.Position != 1 {
		t.Fatalf("Z queue = %+v", zv[0].Instances[0].Queue)
	}

	// D9: below the role cap but at the runtime cap → runtime_full.
	f.fx.Exec(t, `UPDATE agent SET max_concurrent_tasks = 5 WHERE id = $1`, f.agentID)
	f.fx.Exec(t, `UPDATE agent_runtime SET max_concurrent_tasks = 3 WHERE id = $1`, f.runtimeID)
	views, _ = f.svc.ListInstances(ctx, f.loadIssue(x))
	if q := views[0].Instances[2].Queue; q.Reason != QueueReasonRuntimeFull || *q.Limit != 3 {
		t.Fatalf("queue = %+v, want runtime_full", q)
	}
	f.fx.Exec(t, `UPDATE agent_runtime SET max_concurrent_tasks = NULL WHERE id = $1`, f.runtimeID)
	views, _ = f.svc.ListInstances(ctx, f.loadIssue(x))
	if q := views[0].Instances[2].Queue; q.Reason != QueueReasonWaitingDispatch || q.Limit != nil {
		t.Fatalf("queue = %+v, want waiting_dispatch", q)
	}
	f.fx.Exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, f.runtimeID)
	views, _ = f.svc.ListInstances(ctx, f.loadIssue(x))
	if q := views[0].Instances[2].Queue; q.Reason != QueueReasonRuntimeOffline {
		t.Fatalf("queue = %+v, want runtime_offline", q)
	}

	// D16: a prioritised instance is first and the batch says so.
	f.fx.Exec(t, `UPDATE agent_runtime SET status = 'online' WHERE id = $1`, f.runtimeID)
	f.fx.Exec(t, `UPDATE agent_task_queue SET priority = $1 WHERE id = $2`, InstancePriorityBoost, x3)
	views, _ = f.svc.ListInstances(ctx, f.loadIssue(x))
	if !views[0].Priority || views[0].Instances[2].Queue.Position != 1 {
		t.Fatalf("priority=%v position=%d, want true / 1", views[0].Priority, views[0].Instances[2].Queue.Position)
	}
	yv, _ := f.svc.ListInstances(ctx, f.loadIssue(y))
	if s := yv[0].Instances[0]; s.SessionID == nil || *s.SessionID != "s-y" || s.Queue != nil {
		t.Fatalf("Y instance = %+v", s)
	}
}
