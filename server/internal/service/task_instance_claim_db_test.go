package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (D): DB-backed checks for instance claiming (migrations 9001–9004 and
// the ClaimAgentTask changes). Skips without DATABASE_URL, like the package's
// other DB tests.

type instanceFixture struct {
	t         *testing.T
	svc       *TaskService
	fx        *testutil.Fixture
	pool      *pgxpool.Pool
	wakeups   *recordingWakeup
	userID    string
	agentID   string
	runtimeID string
}

type recordingWakeup struct {
	mu    sync.Mutex
	calls int
}

func (w *recordingWakeup) NotifyTaskAvailable(string, string) {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
}

func (w *recordingWakeup) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func newInstanceFixture(t *testing.T, maxConcurrent int) *instanceFixture {
	t.Helper()
	pool := sharedTestPool(t)
	boot := testutil.New(pool, "", "")
	n := time.Now().UnixNano()
	userID := boot.User(t, fmt.Sprintf("inst-%d", n), fmt.Sprintf("inst-%d@example.com", n))
	wsID := boot.Workspace(t, fmt.Sprintf("inst-%d", n), fmt.Sprintf("inst-%d", n))
	fx := testutil.New(pool, wsID, userID)
	fx.Member(t, wsID, userID, "owner")
	runtimeID := fx.Runtime(t, "inst-runtime", testutil.Cols{"visibility": "public"})
	agentID := fx.Agent(t, "TB", runtimeID, testutil.Cols{"max_concurrent_tasks": maxConcurrent})
	w := &recordingWakeup{}
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	svc.Wakeup = w
	return &instanceFixture{t: t, svc: svc, fx: fx, pool: pool, wakeups: w, userID: userID, agentID: agentID, runtimeID: runtimeID}
}

func (f *instanceFixture) issue(title string) string {
	return f.fx.Issue(f.t, title)
}

func (f *instanceFixture) batch(issueID string, k int) string {
	f.t.Helper()
	return f.fx.Insert(f.t, "agent_task_batch", testutil.Cols{
		"workspace_id":      f.fx.WorkspaceID,
		"issue_id":          issueID,
		"agent_id":          f.agentID,
		"splits":            testutil.Raw(fmt.Sprintf("array_fill('x'::text, ARRAY[%d])", k)),
		"requested_by_type": "member",
		"requested_by_id":   f.userID,
	})
}

// task inserts a queue row for the fixture agent; ageSec orders created_at.
func (f *instanceFixture) task(issueID, status string, ageSec int, over testutil.Cols) string {
	f.t.Helper()
	cols := testutil.Cols{
		"runtime_id": f.runtimeID,
		"issue_id":   issueID,
		"status":     status,
		"created_at": testutil.Raw(fmt.Sprintf("now() - interval '%d seconds'", ageSec)),
	}
	if status == "running" {
		cols["dispatched_at"] = testutil.Raw("now() - interval '1 minute'")
		cols["started_at"] = testutil.Raw("now() - interval '1 minute'")
	}
	for k, v := range over {
		cols[k] = v
	}
	return f.fx.Task(f.t, f.agentID, cols)
}

func (f *instanceFixture) instance(issueID, batchID string, index int, status string, ageSec int, over testutil.Cols) string {
	f.t.Helper()
	cols := testutil.Cols{"batch_id": batchID, "batch_index": index, "force_fresh_session": true}
	for k, v := range over {
		cols[k] = v
	}
	return f.task(issueID, status, ageSec, cols)
}

func (f *instanceFixture) claim() *db.AgentTaskQueue {
	f.t.Helper()
	got, err := f.svc.claimTask(context.Background(), util.MustParseUUID(f.agentID), util.MustParseUUID(f.runtimeID), claimScope{claimPath: claimPathRuntime})
	if err != nil {
		f.t.Fatalf("claimTask: %v", err)
	}
	return got
}

func (f *instanceFixture) status(taskID string) string {
	f.t.Helper()
	var s string
	f.fx.QueryRow(f.t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&s)
	return s
}

func idOf(task *db.AgentTaskQueue) string {
	if task == nil {
		return "<none>"
	}
	return util.UUIDToString(task.ID)
}

// D1, D12: instances run beside the main run and each other; a second normal
// run on the same issue still waits for the main run.
func TestInstanceClaimRunsBesideMainRun(t *testing.T) {
	f := newInstanceFixture(t, 5)
	x := f.issue("X")
	main := f.task(x, "running", 300, nil)
	b := f.batch(x, 3)
	want := map[string]bool{}
	for i := 1; i <= 3; i++ {
		want[f.instance(x, b, i, "queued", 100-i, nil)] = true
	}
	normal := f.task(x, "queued", 200, nil)

	for i := 0; i < 3; i++ {
		got := f.claim()
		if got == nil || !want[idOf(got)] {
			t.Fatalf("claim %d = %s, want one of the 3 instances", i+1, idOf(got))
		}
		delete(want, idOf(got))
	}
	if got := f.claim(); got != nil {
		t.Fatalf("4th claim = %s, want none: the normal row must wait for the main run", idOf(got))
	}
	if s := f.status(normal); s != "queued" {
		t.Fatalf("normal row status = %s, want queued", s)
	}
	if s := f.status(main); s != "running" {
		t.Fatalf("main run status = %s, want running (no preemption)", s)
	}

	// Running instances do not hold the normal slot.
	f.fx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, main)
	if got := f.claim(); idOf(got) != normal {
		t.Fatalf("claim after main completed = %s, want the normal row %s", idOf(got), normal)
	}
}

// D6, D7, D8: a freed slot goes to an issue with nothing running before
// another instance on a busy issue, unless that instance is prioritised; the
// running instances are never touched.
func TestInstanceClaimOrder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		priority int
		wantX    bool
	}{
		{"other issue first", 0, false},
		{"prioritised instance first", InstancePriorityBoost, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInstanceFixture(t, 3)
			x, z := f.issue("X"), f.issue("Z")
			bx := f.batch(x, 3)
			running := []string{
				f.instance(x, bx, 1, "running", 600, nil),
				f.instance(x, bx, 2, "running", 600, nil),
				f.instance(x, bx, 3, "running", 600, nil),
			}
			startedBefore := map[string]time.Time{}
			for _, id := range running {
				var at time.Time
				f.fx.QueryRow(t, `SELECT started_at FROM agent_task_queue WHERE id = $1`, id).Scan(&at)
				startedBefore[id] = at
			}

			bx2 := f.batch(x, 1)
			x4 := f.instance(x, bx2, 1, "queued", 20, testutil.Cols{"priority": tc.priority})
			bz := f.batch(z, 1)
			z1 := f.instance(z, bz, 1, "queued", 10, nil)

			if got := f.claim(); got != nil {
				t.Fatalf("claim with 3/3 running = %s, want none", idOf(got))
			}
			f.fx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, running[2])

			want, other := z1, x4
			if tc.wantX {
				want, other = x4, z1
			}
			if got := f.claim(); idOf(got) != want {
				t.Fatalf("freed slot went to %s, want %s", idOf(got), want)
			}
			if s := f.status(other); s != "queued" {
				t.Fatalf("other request status = %s, want queued", s)
			}
			for _, id := range running[:2] {
				var s string
				var started time.Time
				f.fx.QueryRow(t, `SELECT status, started_at FROM agent_task_queue WHERE id = $1`, id).Scan(&s, &started)
				if s != "running" || !started.Equal(startedBefore[id]) {
					t.Fatalf("running instance %s became %s / started %v, want untouched", id, s, started)
				}
			}
		})
	}
}

// With no instances anywhere the claim order is upstream's: priority, then age.
func TestClaimOrderWithoutInstancesIsUpstream(t *testing.T) {
	f := newInstanceFixture(t, 5)
	a, b, c := f.issue("A"), f.issue("B"), f.issue("C")
	oldLow := f.task(a, "queued", 30, testutil.Cols{"priority": 1})
	newHigh := f.task(b, "queued", 10, testutil.Cols{"priority": 3})
	oldHigh := f.task(c, "queued", 20, testutil.Cols{"priority": 3})
	for i, want := range []string{oldHigh, newHigh, oldLow} {
		if got := f.claim(); idOf(got) != want {
			t.Fatalf("claim %d = %s, want %s", i+1, idOf(got), want)
		}
	}
}

// 9003/9004: many pending instances share an (issue, agent, thread) slot with
// one pending normal run; two pending normal runs still collide.
func TestPendingSlotIndexExcludesInstances(t *testing.T) {
	f := newInstanceFixture(t, 5)
	x := f.issue("X")
	b := f.batch(x, 2)
	f.instance(x, b, 1, "queued", 3, nil)
	f.instance(x, b, 2, "queued", 2, nil)
	f.task(x, "queued", 1, nil)

	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority) VALUES ($1, $2, $3, 'queued', 0)`,
		f.agentID, f.runtimeID, x)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "idx_one_pending_task_per_issue_agent_thread_fork" {
		t.Fatalf("second normal pending row: err = %v, want unique violation on the fork index", err)
	}
	if !isDuplicatePendingTaskErr(err) {
		t.Fatal("isDuplicatePendingTaskErr does not recognise the fork index")
	}
}

// D2: an instance retry keeps its batch identity and never resumes a session,
// and is not blocked by a pending normal run.
func TestRetryOfInstanceStaysAnInstance(t *testing.T) {
	f := newInstanceFixture(t, 5)
	x := f.issue("X")
	b := f.batch(x, 2)
	failed := f.instance(x, b, 2, "failed", 60, testutil.Cols{"session_id": "inst-session", "work_dir": "/tmp/inst2", "completed_at": testutil.Raw("now()")})
	f.task(x, "queued", 1, nil) // pending normal run on the same slot

	q := db.New(f.pool)
	retry, err := q.CreateRetryTask(context.Background(), db.CreateRetryTaskParams{ID: util.MustParseUUID(failed)})
	if err != nil {
		t.Fatalf("CreateRetryTask: %v", err)
	}
	f.fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id = $1`, util.UUIDToString(retry.ID))
	if util.UUIDToString(retry.BatchID) != b || retry.BatchIndex.Int16 != 2 || !retry.ForceFreshSession {
		t.Fatalf("retry batch=%s index=%v fresh=%v, want %s / 2 / true", util.UUIDToString(retry.BatchID), retry.BatchIndex, retry.ForceFreshSession, b)
	}

	ok, err := hasRunnableSuccessor(context.Background(), q, db.AgentTaskQueue{IssueID: util.MustParseUUID(x), AgentID: util.MustParseUUID(f.agentID), BatchID: util.MustParseUUID(b)})
	if err != nil || ok {
		t.Fatalf("hasRunnableSuccessor(instance) = %v, %v; want false", ok, err)
	}
}

// A normal retry still loses to a pending normal run (upstream behaviour).
func TestRetryOfNormalRunStillYieldsToPendingRun(t *testing.T) {
	f := newInstanceFixture(t, 5)
	x := f.issue("X")
	failed := f.task(x, "failed", 60, testutil.Cols{"completed_at": testutil.Raw("now()")})
	f.task(x, "queued", 1, nil)
	_, err := db.New(f.pool).CreateRetryTask(context.Background(), db.CreateRetryTaskParams{ID: util.MustParseUUID(failed)})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CreateRetryTask = %v, want no row (slot taken)", err)
	}
}

// D2: the main run never resumes an instance's session.
func TestLastTaskSessionSkipsInstances(t *testing.T) {
	f := newInstanceFixture(t, 5)
	x := f.issue("X")
	f.task(x, "completed", 600, testutil.Cols{"session_id": "main-session", "started_at": testutil.Raw("now() - interval '10 minutes'"), "completed_at": testutil.Raw("now() - interval '9 minutes'")})
	b := f.batch(x, 1)
	f.instance(x, b, 1, "completed", 60, testutil.Cols{"session_id": "inst-session", "started_at": testutil.Raw("now() - interval '1 minute'"), "completed_at": testutil.Raw("now()")})

	got, err := db.New(f.pool).GetLastTaskSession(context.Background(), db.GetLastTaskSessionParams{
		AgentID: util.MustParseUUID(f.agentID), IssueID: util.MustParseUUID(x),
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession: %v", err)
	}
	if got.SessionID.String != "main-session" {
		t.Fatalf("resumed session = %q, want main-session", got.SessionID.String)
	}
}

// D1: after a claim the daemon is woken again while the agent has capacity
// and more queued work, and not when it is full.
func TestClaimNudgesForMoreQueuedWork(t *testing.T) {
	for _, tc := range []struct {
		max       int
		wantNudge bool
	}{{3, true}, {1, false}} {
		t.Run(fmt.Sprintf("max=%d", tc.max), func(t *testing.T) {
			f := newInstanceFixture(t, tc.max)
			x := f.issue("X")
			b := f.batch(x, 2)
			f.instance(x, b, 1, "queued", 2, nil)
			f.instance(x, b, 2, "queued", 1, nil)
			before := f.wakeups.count()
			if f.claim() == nil {
				t.Fatal("no claim")
			}
			if got := f.wakeups.count() > before; got != tc.wantNudge {
				t.Fatalf("nudged = %v, want %v", got, tc.wantNudge)
			}
		})
	}
}

// agent_task_batch has no FK (repo rule), so deleting the issue deletes it.
func TestDeleteIssueDeletesInstanceBatches(t *testing.T) {
	f := newInstanceFixture(t, 1)
	x := f.issue("X")
	f.batch(x, 2)
	if err := db.New(f.pool).DeleteIssue(context.Background(), db.DeleteIssueParams{
		ID: util.MustParseUUID(x), WorkspaceID: util.MustParseUUID(f.fx.WorkspaceID),
	}); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if n := f.fx.Count(t, `SELECT count(*) FROM agent_task_batch WHERE issue_id = $1`, x); n != 0 {
		t.Fatalf("batches left after DeleteIssue = %d, want 0", n)
	}
}
