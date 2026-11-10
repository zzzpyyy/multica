package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (D2–D4): the instance claim payload, the fork-daemon gate, instance
// runs being read-only on their issue, and their comments waking nobody.

func instanceClaimFixture(t *testing.T) (fx *testutil.Fixture, agentID, issueID, batchID string) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx = testutil.New(testPool, testWorkspaceID, testUserID)
	// Own agent: other tests rebind the workspace's shared agents.
	agentID = fx.Agent(t, "inst-claim-TB", testRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	issueID = fx.Issue(t, "instance claim")
	batchID = fx.Insert(t, "agent_task_batch", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issueID, "agent_id": agentID,
		"splits":            testutil.Raw(`ARRAY['home page','list page','detail page']`),
		"requested_by_type": "member", "requested_by_id": testUserID,
	})
	return fx, agentID, issueID, batchID
}

func TestInstanceClaimPayloadAndForkGate(t *testing.T) {
	fx, agentID, issueID, batchID := instanceClaimFixture(t)
	runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(testRuntimeID))
	if err != nil {
		t.Fatal(err)
	}
	claim := func(capabilities string) (string, *AgentTaskResponse, *claimBuildFailure) {
		id := fx.Task(t, agentID, testutil.Cols{
			"issue_id": issueID, "runtime_id": testRuntimeID, "status": "dispatched",
			"dispatched_at": time.Now(), "batch_id": batchID, "batch_index": 2, "force_fresh_session": true,
		})
		task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
		if err != nil {
			t.Fatal(err)
		}
		req := newOwnerDaemonRequest("POST", "/claim", nil)
		req.Header.Set("X-Client-Capabilities", capabilities)
		resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &task, runtime, testRuntimeID, testWorkspaceID)
		return id, &resp, failure
	}

	id, _, failure := claim(protocol.DaemonCapabilityLocalWorktreeV1)
	if failure == nil || failure.outcome != ForkDaemonRequiredReason {
		t.Fatalf("upstream daemon claim failure = %+v, want %s", failure, ForkDaemonRequiredReason)
	}
	var status, reason string
	dbfx.QueryRow(t, `SELECT status, COALESCE(failure_reason,'') FROM agent_task_queue WHERE id=$1`, id).Scan(&status, &reason)
	if status != "failed" || reason != ForkDaemonRequiredReason {
		t.Fatalf("upstream-claimed instance = %s / %s, want failed / %s", status, reason, ForkDaemonRequiredReason)
	}

	_, resp, failure := claim(protocol.DaemonCapabilityLocalWorktreeV1 + "," + protocol.DaemonCapabilityForkV1)
	if failure != nil {
		t.Fatalf("fork daemon claim failed: %+v", failure)
	}
	in := resp.Instance
	if in == nil || in.Index != 2 || in.Total != 3 || in.Split != "list page" || len(in.Siblings) != 2 ||
		in.Siblings[0].Split != "home page" || in.Siblings[1].Index != 3 || in.BatchID != batchID {
		t.Fatalf("instance payload = %+v", in)
	}
	if resp.PriorSessionID != "" {
		t.Fatalf("instance resumed session %q", resp.PriorSessionID)
	}
}

func TestInstanceRunCannotChangeIssueOwnership(t *testing.T) {
	fx, agentID, issueID, batchID := instanceClaimFixture(t)
	instance := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "running",
		"batch_id": batchID, "batch_index": 1})
	normal := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "running"})
	put := func(taskID string, body map[string]any) *testutil.Response {
		req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, body), "id", issueID)
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
		return testutil.Call(t, testHandler.UpdateIssue, req)
	}
	for _, body := range []map[string]any{
		{"status": "done"}, {"assignee_type": "member", "assignee_id": testUserID}, {"parent_issue_id": nil},
	} {
		var e struct {
			Code string `json:"code"`
		}
		put(instance, body).Want(http.StatusForbidden).JSON(&e)
		if e.Code != "instance_run_readonly" {
			t.Fatalf("%v: code = %q", body, e.Code)
		}
	}
	put(instance, map[string]any{"title": "renamed by instance"}).Want(http.StatusOK)
	put(normal, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
	var st string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id=$1`, issueID).Scan(&st)
	if st != "in_progress" {
		t.Fatalf("status = %s, want in_progress from the normal run", st)
	}
}

func TestInstanceCommentWakesNobody(t *testing.T) {
	fx, agentID, _, batchID := instanceClaimFixture(t)
	leader := fx.Agent(t, "inst-leader", testRuntimeID)
	squad := fx.Squad(t, "inst-squad", leader)
	issueID := fx.Issue(t, "instance squad issue", testutil.Cols{"assignee_type": "squad", "assignee_id": squad})
	fx.Exec(t, `UPDATE agent_task_batch SET issue_id=$1 WHERE id=$2`, issueID, batchID)
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	instance := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "running",
		"batch_id": batchID, "batch_index": 1})
	normal := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "running"})

	route := func(taskID string) int {
		triggers, _ := testHandler.computeCommentAgentTriggers(context.Background(), issue, "done: home page", nil, "agent", agentID,
			commentTriggerComputeOptions{AuthoringTaskID: parseUUID(taskID), OriginatorUserID: testUserID})
		return len(triggers)
	}
	if n := route(normal); n != 1 {
		t.Fatalf("normal agent comment on a squad issue triggers %d, want the leader (1)", n)
	}
	if n := route(instance); n != 0 {
		t.Fatalf("instance comment triggers %d, want 0", n)
	}
}
