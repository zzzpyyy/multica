package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Fork (D): HTTP contract of POST/GET /api/issues/{id}/instances (plan 3.1).
func TestIssueInstancesHTTP(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	rt := fx.Runtime(t, "inst-http-runtime", testutil.Cols{"provider": "claude", "visibility": "public"})
	tb := fx.Agent(t, "inst-http-TB", rt, testutil.Cols{"max_concurrent_tasks": 2})
	tl := fx.Agent(t, "inst-http-TL", rt, testutil.Cols{"max_concurrent_tasks": 2})
	x := fx.Issue(t, "inst-http X", testutil.Cols{"assignee_type": "agent", "assignee_id": tl, "status": "backlog"})
	fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, x)
	fx.Cleanup(t, `DELETE FROM agent_task_batch WHERE issue_id = $1`, x)

	post := func(body map[string]any, headers map[string]string) *testutil.Response {
		req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+x+"/instances", body), "id", x)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return testutil.Call(t, testHandler.CreateIssueInstances, req)
	}
	code := func(r *testutil.Response) string {
		var e struct {
			Code string `json:"code"`
		}
		r.JSON(&e)
		return e.Code
	}

	if c := code(post(map[string]any{"agent_id": tb, "splits": []string{"a", "b", "c"}}, nil).Want(http.StatusBadRequest)); c != "instance_count_out_of_range" {
		t.Fatalf("k>N code = %q", c)
	}
	if c := code(post(map[string]any{"agent_id": tb, "splits": []string{"a", ""}}, nil).Want(http.StatusBadRequest)); c != "invalid_split" {
		t.Fatalf("empty split code = %q", c)
	}

	var batch struct {
		ID          string `json:"id"`
		Total       int    `json:"total"`
		Priority    bool   `json:"priority"`
		RequestedBy struct {
			Type string `json:"type"`
		} `json:"requested_by"`
		Instances []struct {
			Index  int    `json:"index"`
			Split  string `json:"split"`
			Status string `json:"status"`
			TaskID string `json:"task_id"`
			Queue  *struct {
				Position int    `json:"position"`
				Reason   string `json:"reason"`
			} `json:"queue"`
		} `json:"instances"`
	}
	post(map[string]any{"agent_id": tb, "splits": []string{"a", "b"}, "priority": true}, nil).Want(http.StatusCreated).JSON(&batch)
	if batch.Total != 2 || !batch.Priority || batch.RequestedBy.Type != "member" || len(batch.Instances) != 2 ||
		batch.Instances[1].Split != "b" || batch.Instances[0].Status != "queued" || batch.Instances[0].Queue == nil {
		t.Fatalf("batch = %+v", batch)
	}

	// An agent caller (task identity) cannot ask for priority (D15).
	run := fx.Task(t, tl, testutil.Cols{"runtime_id": rt, "issue_id": x, "status": "running",
		"originator_user_id": testUserID, "accountable_user_id": testUserID, "originator_source": "direct_human"})
	agentHeaders := map[string]string{"X-Agent-ID": tl, "X-Task-ID": run}
	if c := code(post(map[string]any{"agent_id": tb, "splits": []string{"a"}, "priority": true}, agentHeaders).Want(http.StatusForbidden)); c != "priority_requires_member" {
		t.Fatalf("agent priority code = %q", c)
	}
	post(map[string]any{"agent_id": tb, "splits": []string{"d"}}, agentHeaders).Want(http.StatusCreated)

	var list struct {
		Batches []struct {
			ID          string `json:"id"`
			RequestedBy struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"requested_by"`
		} `json:"batches"`
	}
	testutil.Call(t, testHandler.ListIssueInstances, withURLParam(newRequest(http.MethodGet, "/api/issues/"+x+"/instances", nil), "id", x)).
		Want(http.StatusOK).JSON(&list)
	if len(list.Batches) != 2 || list.Batches[0].RequestedBy.Type != "agent" || list.Batches[0].RequestedBy.Name != "inst-http-TL" || list.Batches[1].ID != batch.ID {
		t.Fatalf("list = %+v, want the agent batch first, then %s", list.Batches, batch.ID)
	}
}
