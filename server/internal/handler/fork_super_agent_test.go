package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (B): super-agent bootstrap, its one session, and every way it could
// be given an ordinary run.

func superAgentFixture(t *testing.T) (fx *testutil.Fixture, claudeRT string, sa superAgentResponse) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx = testutil.New(testPool, testWorkspaceID, testUserID)
	claudeRT = fx.Runtime(t, "sa-claude", testutil.Cols{"provider": "claude", "visibility": "public", "daemon_id": "sa-daemon-" + t.Name()})
	codexRT := fx.Runtime(t, "sa-codex", testutil.Cols{"provider": "codex", "visibility": "public"})
	// Registered after the runtimes so it runs first: the agent row holds them.
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id=$1 AND system_key='super')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id=$1 AND system_key='super')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_invocation_target WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id=$1 AND system_key='super')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id=$1 AND system_key='super'`, testWorkspaceID)
	})

	post := func(body any) *testutil.Response {
		return testutil.Call(t, testHandler.CreateSuperAgent, withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/agents/super-agent", body)))
	}
	code := func(r *testutil.Response) string {
		var e struct {
			Code string `json:"code"`
		}
		r.JSON(&e)
		return e.Code
	}
	if c := code(post(map[string]any{}).Want(http.StatusConflict)); c != "runtime_required" {
		t.Fatalf("no runtime: code %q", c)
	}
	if c := code(post(map[string]any{"runtime_id": codexRT}).Want(http.StatusBadRequest)); c != "runtime_not_claude" {
		t.Fatalf("codex runtime: code %q", c)
	}
	post(map[string]any{"runtime_id": claudeRT}).Want(http.StatusCreated).JSON(&sa)
	var again superAgentResponse
	post(map[string]any{}).Want(http.StatusOK).JSON(&again)
	if sa.SystemKey != service.SuperAgentSystemKey || again.ID != sa.ID || again.Session.ID != sa.Session.ID || sa.Session.ID == "" {
		t.Fatalf("bootstrap = %s/%s, again = %s/%s", sa.ID, sa.Session.ID, again.ID, again.Session.ID)
	}
	return fx, claudeRT, sa
}

func errCode(r *testutil.Response) string {
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &e)
	return e.Code
}

func TestSuperAgentSessionLocked(t *testing.T) {
	_, _, sa := superAgentFixture(t)
	r := testutil.Call(t, testHandler.CreateChatSession, withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", map[string]any{"agent_id": sa.ID})))
	if r.Code != http.StatusConflict || errCode(r) != "super_agent_session_locked" {
		t.Fatalf("second session: %d %s", r.Code, r.Body.String())
	}
	archive := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodPatch, "/api/chat/sessions/"+sa.Session.ID+"/archive", map[string]any{"archived": true})), "sessionId", sa.Session.ID)
	if r := testutil.Call(t, testHandler.SetChatSessionArchived, archive); r.Code != http.StatusConflict || errCode(r) != "super_agent_session_locked" {
		t.Fatalf("archive: %d %s", r.Code, r.Body.String())
	}
	del := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodDelete, "/api/chat/sessions/"+sa.Session.ID, nil)), "sessionId", sa.Session.ID)
	if r := testutil.Call(t, testHandler.DeleteChatSession, del); r.Code != http.StatusConflict || errCode(r) != "super_agent_session_locked" {
		t.Fatalf("delete: %d %s", r.Code, r.Body.String())
	}
}

func TestSuperAgentNotAssignable(t *testing.T) {
	fx, _, sa := superAgentFixture(t)
	r := testutil.Call(t, testHandler.CreateIssue, withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title": "assign to super-agent", "assignee_type": "agent", "assignee_id": sa.ID, "status": "todo",
	})))
	if r.Code != http.StatusBadRequest || errCode(r) != "super_agent_not_assignable" {
		t.Fatalf("create issue assigned to super-agent: %d %s", r.Code, r.Body.String())
	}
	squad := fx.Squad(t, "sa-squad", fx.Agent(t, "sa-leader", testRuntimeID))
	r = testutil.Call(t, testHandler.AddSquadMember, withURLParam(withChatTestWorkspaceCtx(t,
		newRequest(http.MethodPost, "/api/squads/"+squad+"/members", map[string]any{"member_type": "agent", "member_id": sa.ID})), "id", squad))
	if r.Code != http.StatusBadRequest || errCode(r) != "super_agent_not_assignable" {
		t.Fatalf("add squad member: %d %s", r.Code, r.Body.String())
	}
	x := fx.Issue(t, "sa wakeup target")
	r = testutil.Call(t, testHandler.CreateIssueWakeup, withURLParam(withChatTestWorkspaceCtx(t,
		newRequest(http.MethodPost, "/api/issues/"+x+"/wakeups", map[string]any{"agent_id": sa.ID})), "id", x))
	if r.Code != http.StatusBadRequest || errCode(r) != "super_agent_not_assignable" {
		t.Fatalf("wakeup: %d %s", r.Code, r.Body.String())
	}
}

// A reply to a comment the super-agent wrote must not start a super-agent
// run (一鸣's counterexample, BUNN-28 07:36).
func TestReplyToSuperAgentCommentStartsNoRun(t *testing.T) {
	fx, _, sa := superAgentFixture(t)
	x := fx.Issue(t, "sa reply target")
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(x))
	if err != nil {
		t.Fatal(err)
	}
	forward := fx.Comment(t, x, "> 来自 super-agent 对话\n用 v2", testutil.Cols{"author_type": "agent", "author_id": sa.ID})
	parent, err := testHandler.Queries.GetComment(context.Background(), parseUUID(forward))
	if err != nil {
		t.Fatal(err)
	}
	reply := fx.Comment(t, x, "收到", testutil.Cols{"parent_id": forward})
	triggers, _ := testHandler.computeCommentAgentTriggers(context.Background(), issue, "收到", &parent, "member", testUserID,
		commentTriggerComputeOptions{ExcludeTriggerCommentID: parseUUID(reply), OriginatorUserID: testUserID})
	for _, tr := range triggers {
		if uuidToString(tr.Agent.ID) == sa.ID {
			t.Fatal("reply to the super-agent's comment triggers the super-agent")
		}
	}
	mention := "[@super-agent](mention://agent/" + sa.ID + ") 看一下"
	triggers, _ = testHandler.computeCommentAgentTriggers(context.Background(), issue, mention, nil, "member", testUserID,
		commentTriggerComputeOptions{OriginatorUserID: testUserID})
	if len(triggers) != 0 {
		t.Fatalf("@super-agent triggers %d runs", len(triggers))
	}
}

// Claimed super-agent work: refused to an upstream daemon; for the fork's
// daemon every tool-bearing field is emptied and the product prompt is used.
func TestSuperAgentClaimHasNoTools(t *testing.T) {
	fx, claudeRT, sa := superAgentFixture(t)
	fx.Exec(t, `UPDATE agent SET custom_args='["--tools","Bash"]'::jsonb, mcp_config='{"mcpServers":{"x":{"command":"sh"}}}'::jsonb, instructions='saved notes' WHERE id=$1`, sa.ID)
	runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(claudeRT))
	if err != nil {
		t.Fatal(err)
	}
	claim := func(caps string) (string, AgentTaskResponse, *claimBuildFailure) {
		id := fx.Task(t, sa.ID, testutil.Cols{"runtime_id": claudeRT, "chat_session_id": sa.Session.ID, "status": "dispatched", "dispatched_at": time.Now()})
		task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
		if err != nil {
			t.Fatal(err)
		}
		req := newOwnerDaemonRequest("POST", "/claim", nil)
		req.Header.Set("X-Client-Capabilities", caps)
		resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &task, runtime, claudeRT, testWorkspaceID)
		return id, resp, failure
	}
	if _, _, failure := claim(protocol.DaemonCapabilityLocalWorktreeV1); failure == nil || failure.outcome != ForkDaemonRequiredReason {
		t.Fatalf("upstream daemon: failure = %+v", failure)
	}
	_, resp, failure := claim(protocol.DaemonCapabilityForkV1)
	if failure != nil {
		t.Fatalf("fork daemon: %+v", failure)
	}
	if !resp.NoTools || resp.Agent == nil || resp.Agent.CustomArgs != nil || string(resp.Agent.McpConfig) != "{}" ||
		resp.RemoteMCPConnections != nil || resp.PluginHookTools != nil {
		t.Fatalf("claim keeps tools: no_tools=%v custom=%v mcp=%s remote=%v plugin=%v", resp.NoTools, resp.Agent.CustomArgs, resp.Agent.McpConfig, resp.RemoteMCPConnections, resp.PluginHookTools)
	}
	if resp.Agent.Instructions != service.SuperAgentInstructions() || strings.Contains(resp.Agent.Instructions, "saved notes") {
		t.Fatal("claim does not carry the product-owned instructions")
	}

	// B18: each user message reaches the model with its id, so it can cite
	// earlier turns in source_message_ids.
	id := fx.Task(t, sa.ID, testutil.Cols{"runtime_id": claudeRT, "chat_session_id": sa.Session.ID, "status": "dispatched", "dispatched_at": time.Now()})
	fx.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, id)
	msgID := fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sa.Session.ID, "role": "user", "content": "过期的标红", "task_id": id})
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	req := newOwnerDaemonRequest("POST", "/claim", nil)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityForkV1)
	resp, _, _, _, _, failure = testHandler.buildClaimedTaskResponse(req, &task, runtime, claudeRT, testWorkspaceID)
	if failure != nil || !strings.Contains(resp.ChatMessage, "（消息 id="+msgID+"）\n过期的标红") {
		t.Fatalf("turn input lacks the message id: failure=%+v msg=%q", failure, resp.ChatMessage)
	}
}
