package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (B3, R1, R4, R5, R6): confirming a card creates the issue as the
// super-agent; forward and set_status run after the turn with the user's
// words, waking whoever the user's own comment would wake.

type superTurnFixture struct {
	fx       *testutil.Fixture
	sa       superAgentResponse
	rt       string
	task     string
	messages []string
}

func newSuperTurn(t *testing.T, fx *testutil.Fixture, sa superAgentResponse, rt string, texts ...string) *superTurnFixture {
	t.Helper()
	task := fx.Task(t, sa.ID, testutil.Cols{"runtime_id": rt, "chat_session_id": sa.Session.ID, "status": "completed"})
	f := &superTurnFixture{fx: fx, sa: sa, rt: rt, task: task}
	for _, txt := range texts {
		f.messages = append(f.messages, fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sa.Session.ID, "role": "user", "content": txt, "task_id": task}))
	}
	return f
}

// action records the turn's assistant message and its action row.
func (f *superTurnFixture) action(t *testing.T, kind, status string, payload any) string {
	t.Helper()
	msg := f.fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": f.sa.Session.ID, "role": "assistant", "content": "好的。", "task_id": f.task})
	raw, _ := json.Marshal(payload)
	return f.fx.Insert(t, "super_agent_action", testutil.Cols{
		"chat_session_id": f.sa.Session.ID, "chat_message_id": msg, "kind": kind, "status": status,
		"payload": testutil.Raw("'" + strings.ReplaceAll(string(raw), "'", "''") + "'::jsonb"),
	})
}

func (f *superTurnFixture) run(t *testing.T) {
	t.Helper()
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(f.task))
	if err != nil {
		t.Fatal(err)
	}
	testHandler.runSuperAgentDirectActions(context.Background(), &task)
}

func identOf(t *testing.T, fx *testutil.Fixture, issueID string) string {
	t.Helper()
	var n int32
	var prefix string
	fx.QueryRow(t, `SELECT i.number, w.issue_prefix FROM issue i JOIN workspace w ON w.id=i.workspace_id WHERE i.id=$1`, issueID).Scan(&n, &prefix)
	return fmt.Sprintf("%s-%d", prefix, n)
}

func TestSuperAgentConfirmCreatesIssueAsSuperAgent(t *testing.T) {
	fx, rt, sa := superAgentFixture(t)
	turn := newSuperTurn(t, fx, sa, rt, "给 qa 建个任务：看一下周报，完成标准是周报发出")
	card := turn.action(t, "create_issues", "pending", map[string]any{"items": []map[string]any{{
		"title": "看一下周报", "assignee": map[string]string{"type": "member", "id": testUserID, "name": "me"},
		"what": "看一下周报", "done": "周报发出", "status": "todo", "will_run": false, "source_message_ids": turn.messages,
	}}})
	fx.Cleanup(t, `DELETE FROM issue WHERE creator_id=$1`, sa.ID)
	confirm := func() *testutil.Response {
		req := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/plan-cards/"+card+"/confirm", nil)), "cardId", card)
		return testutil.Call(t, testHandler.ConfirmPlanCard, req)
	}
	var got struct {
		Status        string `json:"status"`
		CreatedIssues []struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
		} `json:"created_issues"`
	}
	confirm().Want(http.StatusOK).JSON(&got)
	if got.Status != "created" || len(got.CreatedIssues) != 1 {
		t.Fatalf("confirm = %+v", got)
	}
	var again struct{ Status string }
	confirm().Want(http.StatusOK).JSON(&again)
	var creatorType, creatorID, status, desc string
	var parent *string
	fx.QueryRow(t, `SELECT creator_type, creator_id::text, status, description, parent_issue_id::text FROM issue WHERE id=$1`, got.CreatedIssues[0].ID).
		Scan(&creatorType, &creatorID, &status, &desc, &parent)
	if creatorType != "agent" || creatorID != sa.ID || status != "todo" || parent != nil {
		t.Fatalf("issue = %s/%s/%s parent %v", creatorType, creatorID, status, parent)
	}
	for _, want := range []string{"给 qa 建个任务：看一下周报，完成标准是周报发出", "/chat?session=" + sa.Session.ID + "&message=" + turn.messages[0], "## 要做什么\n看一下周报", "## 怎样算完成\n周报发出"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description missing %q:\n%s", want, desc)
		}
	}
	if n := fx.Count(t, `SELECT count(*) FROM issue WHERE creator_id=$1`, sa.ID); n != 1 || again.Status != "created" {
		t.Fatalf("issues after double confirm = %d, second status %q (B3)", n, again.Status)
	}
	if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, got.CreatedIssues[0].ID); n != 0 {
		t.Fatalf("member-assigned issue started %d runs (B16)", n)
	}
}

func TestSuperAgentForwardAndSetStatus(t *testing.T) {
	fx, rt, sa := superAgentFixture(t)
	worker := fx.Agent(t, "sa-worker", rt, testutil.Cols{"max_concurrent_tasks": 3})
	k := fx.Issue(t, "sa forward target", testutil.Cols{"assignee_type": "agent", "assignee_id": worker, "status": "in_review"})
	fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, k)
	ask := fx.Comment(t, k, "接口用 v1 还是 v2？", testutil.Cols{"author_type": "agent", "author_id": worker})
	fx.Insert(t, "super_agent_question", testutil.Cols{"chat_session_id": sa.Session.ID, "comment_id": ask, "status": "asked"})
	ident := identOf(t, fx, k)

	// R1: one question waiting, no issue named → forwarded there.
	turn := newSuperTurn(t, fx, sa, rt, "用 v2")
	turn.action(t, "forward", "pending", map[string]any{"issue": "", "message_ids": turn.messages, "text": "用 v2"})
	turn.run(t)
	var content, author string
	fx.QueryRow(t, `SELECT content, author_id::text FROM comment WHERE issue_id=$1 AND content LIKE '%用 v2%'`, k).Scan(&content, &author)
	if author != sa.ID || !strings.Contains(content, "来自 super-agent 对话") || !strings.Contains(content, "message="+turn.messages[0]) {
		t.Fatalf("forward comment = %q by %s", content, author)
	}
	if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, k, worker); n != 1 {
		t.Fatalf("assignee runs after forward = %d, want 1", n)
	}
	if n := fx.Count(t, `SELECT count(*) FROM super_agent_question WHERE comment_id=$1 AND status='forwarded'`, ask); n != 1 {
		t.Fatal("question not marked forwarded")
	}
	if n := fx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id=$1 AND task_id IS NULL AND content LIKE '已转到%'`, sa.Session.ID); n != 1 {
		t.Fatal("no 已转到 note in the conversation")
	}

	// R4: two questions waiting and no issue named → nothing forwarded.
	k2 := fx.Issue(t, "sa second question")
	for _, issue := range []string{k, k2} {
		c := fx.Comment(t, issue, "要不要分页？", testutil.Cols{"author_type": "agent", "author_id": worker})
		fx.Insert(t, "super_agent_question", testutil.Cols{"chat_session_id": sa.Session.ID, "comment_id": c, "status": "asked"})
	}
	before := fx.Count(t, `SELECT count(*) FROM comment WHERE author_id=$1`, sa.ID)
	turn = newSuperTurn(t, fx, sa, rt, "可以")
	turn.action(t, "forward", "pending", map[string]any{"issue": "", "message_ids": turn.messages, "text": "可以"})
	turn.run(t)
	if after := fx.Count(t, `SELECT count(*) FROM comment WHERE author_id=$1`, sa.ID); after != before {
		t.Fatalf("ambiguous forward posted %d comments", after-before)
	}

	// R5 / R11: set_status only with the identifier in the user's words.
	turn = newSuperTurn(t, fx, sa, rt, "通过了")
	turn.action(t, "set_status", "pending", map[string]any{"issue": ident, "status": "done", "message_ids": turn.messages, "text": "通过了"})
	turn.run(t)
	var st string
	fx.QueryRow(t, `SELECT status FROM issue WHERE id=$1`, k).Scan(&st)
	if st != "in_review" {
		t.Fatalf("status changed to %s without the identifier in the message", st)
	}
	// The activity log is written from issue:updated (cmd/server), so check
	// the event's actor here.
	var actorType, actorID string
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		if p, ok := e.Payload.(map[string]any); ok && p["status_changed"] == true {
			actorType, actorID = e.ActorType, e.ActorID
		}
	})
	turn = newSuperTurn(t, fx, sa, rt, ident+" 验收通过")
	turn.action(t, "set_status", "pending", map[string]any{"issue": ident, "status": "done", "message_ids": turn.messages, "text": ident + " 验收通过"})
	turn.run(t)
	fx.QueryRow(t, `SELECT status FROM issue WHERE id=$1`, k).Scan(&st)
	if st != "done" {
		t.Fatalf("status = %s, want done", st)
	}
	if actorType != "agent" || actorID != sa.ID {
		t.Fatalf("status change actor = %s %s, want the super-agent (G4)", actorType, actorID)
	}
}
