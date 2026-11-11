package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (B2–B11, B15–B18, D13, D16, R4–R6): the action block is checked by the
// server, never trusted.

type superFixture struct {
	*instanceFixture
	session string
	task    db.AgentTaskQueue
}

func newSuperFixture(t *testing.T) *superFixture {
	f := newInstanceFixture(t, 3)
	f.fx.Exec(t, `UPDATE agent_runtime SET provider='claude' WHERE id=$1`, f.runtimeID)
	sa := f.fx.Agent(t, "super-agent", f.runtimeID, testutil.Cols{"system_key": "super"})
	f.fx.Agent(t, "尤雨溪", f.runtimeID, testutil.Cols{"description": "前端", "max_concurrent_tasks": 3})
	session := f.fx.ChatSession(t, sa, testutil.Cols{"creator_id": f.userID})
	f.fx.Cleanup(t, `DELETE FROM super_agent_action WHERE chat_session_id=$1`, session)
	f.fx.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id=$1`, session)
	return &superFixture{instanceFixture: f, session: session}
}

// turn queues a chat turn whose input is the given user messages.
func (f *superFixture) turn(texts ...string) db.AgentTaskQueue {
	f.t.Helper()
	ctx := context.Background()
	var sa string
	f.fx.QueryRow(f.t, `SELECT agent_id FROM chat_session WHERE id=$1`, f.session).Scan(&sa)
	id := f.fx.Task(f.t, sa, testutil.Cols{"runtime_id": f.runtimeID, "chat_session_id": f.session, "status": "running"})
	f.fx.Exec(f.t, `UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1`, id)
	for _, txt := range texts {
		f.fx.Insert(f.t, "chat_message", testutil.Cols{"chat_session_id": f.session, "role": "user", "content": txt, "task_id": id})
	}
	task, err := f.svc.Queries.GetAgentTask(ctx, util.MustParseUUID(id))
	if err != nil {
		f.t.Fatal(err)
	}
	f.task = task
	return task
}

func fence(js string) string { return "好的。\n\n```super-action\n" + js + "\n```\n" }

func (f *superFixture) plan(body string) (string, *superPlan) {
	return f.svc.planSuperAction(context.Background(), f.svc.Queries, f.task, body)
}

func TestSplitSuperAction(t *testing.T) {
	for _, tc := range []struct {
		in, visible string
		ok          bool
	}{
		{"hi\n\n```super-action\n{\"type\":\"forward\"}\n```", "hi", true},
		{"```super-action\n{}\n```  \n", "", true},
		{"hi\n```super-action\n{}\n```\nmore text", "hi\n```super-action\n{}\n```\nmore text", false},
		{"see\n```super-action\n{}\n```\nthen\n```go\nx\n```", "see\n```super-action\n{}\n```\nthen\n```go\nx\n```", false},
		{"no fence", "no fence", false},
	} {
		visible, _, ok := splitSuperAction(tc.in)
		if ok != tc.ok || (ok && visible != tc.visible) || (!ok && visible != tc.in) {
			t.Errorf("splitSuperAction(%q) = %q, %v", tc.in, visible, ok)
		}
	}
}

func TestPlanCreateIssues(t *testing.T) {
	f := newSuperFixture(t)
	f.turn("给尤雨溪建个任务：看板支持暗色模式，完成标准是系统切到深色时看板跟着变深色，先别跑")

	// B2: a well-formed card.
	visible, plan := f.plan(fence(`{"type":"create_issues","issues":[{"title":"看板暗色模式","assignee":"尤雨溪","status":"backlog","what":"看板支持暗色模式","done":"切到深色时看板变深色"}]}`))
	if plan == nil || plan.kind != "create_issues" || visible != "好的。" {
		t.Fatalf("plan = %+v, visible %q", plan, visible)
	}
	items := plan.payload.(map[string]any)["items"].([]SuperIssueItem)
	if len(items) != 1 || items[0].Assignee.Name != "尤雨溪" || items[0].Status != "backlog" || items[0].WillRun || len(items[0].SourceMessageIDs) != 1 {
		t.Fatalf("item = %+v", items)
	}

	// B13: text over 100 characters above a card is the card restated; the
	// server keeps one sentence. Short text stays as the model wrote it.
	long := "请确认这两个任务：\n\n" + strings.Repeat("- 标题：排查接口超时\n- 负责人：彦宏\n- 状态：backlog\n", 4)
	visible, plan = f.plan(strings.Replace(fence(`{"type":"create_issues","issues":[{"title":"看板暗色模式","assignee":"尤雨溪","status":"backlog","what":"看板支持暗色模式","done":"切到深色时看板变深色"}]}`), "好的。", long, 1))
	if plan == nil || visible != "请确认下面的计划。" {
		t.Fatalf("long card text: plan %v, reply %q", plan != nil, visible)
	}

	// B13/B14: a reply without a card keeps the sentences that fit in 100.
	chatty := strings.Repeat("我负责和你把需求聊清楚，然后出计划卡片。", 6) + "还有别的吗？" // 126 runes
	visible, plan = f.plan(chatty)
	if plan != nil || len([]rune(visible)) > 100 || !strings.HasSuffix(visible, "。") || !strings.HasPrefix(chatty, visible) {
		t.Fatalf("long reply: %d runes %q", len([]rune(visible)), visible)
	}
	if got := capReply(strings.Repeat("长", 150), 100); len([]rune(got)) != 100 || !strings.HasSuffix(got, "…") {
		t.Fatalf("no sentence end: %d runes", len([]rune(got)))
	}
	visible, _ = f.plan(fence(`{"type":"create_issues","issues":[{"title":"优化看板","assignee":"尤雨溪","what":"优化看板","done":""}]}`))
	if strings.Contains(visible, "好的。") == false || len([]rune(visible)) > 100 {
		t.Fatalf("ask reply: %q", visible)
	}
	visible, _ = f.plan(strings.Replace(fence(`{"type":"create_issues","issues":[{"title":"优化看板","assignee":"尤雨溪","what":"优化看板","done":""}]}`), "好的。", chatty, 1))
	if n := len([]rune(visible)); n > 100 || !strings.HasSuffix(visible, "还需要说明：要做什么 / 怎样算完成。") {
		t.Fatalf("long ask reply: %d runes %q", n, visible)
	}

	// B10: an unknown assignee replaces the reply with the directory; no card.
	visible, plan = f.plan(fence(`{"type":"create_issues","issues":[{"title":"周报","assignee":"张三","what":"整理周报","done":"发出"}]}`))
	if plan != nil || !strings.Contains(visible, "找不到「张三」") || !strings.Contains(visible, "尤雨溪") || strings.Contains(visible, "super-agent") {
		t.Fatalf("unknown assignee: plan %v, reply %q", plan, visible)
	}

	// B11 / B17: missing what or done → no card, ask.
	visible, plan = f.plan(fence(`{"type":"create_issues","issues":[{"title":"优化看板","assignee":"尤雨溪","what":"优化看板","done":""}]}`))
	if plan != nil || !strings.Contains(visible, "怎样算完成") {
		t.Fatalf("missing done: plan %v, reply %q", plan, visible)
	}

	// The super-agent is not a valid assignee even by its exact name.
	_, plan = f.plan(fence(`{"type":"create_issues","issues":[{"title":"x","assignee":"super-agent","what":"a","done":"b"}]}`))
	if plan != nil {
		t.Fatal("super-agent accepted as assignee")
	}

	// B4 / B16: will_run follows assignee kind and status.
	_, plan = f.plan(fence(`{"type":"create_issues","issues":[{"title":"a","assignee":"尤雨溪","status":"todo","what":"a","done":"b"}]}`))
	if !plan.payload.(map[string]any)["items"].([]SuperIssueItem)[0].WillRun {
		t.Fatal("todo agent task not marked will_run")
	}
}

func TestPlanDirectActions(t *testing.T) {
	f := newSuperFixture(t)
	f.turn("BUNN-3 不通过：缺少错误码说明")
	_, plan := f.plan(fence(`{"type":"set_status","issue":"BUNN-3","status":"done"}`))
	if plan == nil || plan.kind != "forward" {
		t.Fatalf("不通过 must become a forward (R6), got %+v", plan)
	}
	f.turn("BUNN-3 验收通过")
	_, plan = f.plan(fence(`{"type":"set_status","issue":"BUNN-3","status":"done"}`))
	if plan == nil || plan.kind != "set_status" || plan.payload.(SuperDirectPayload).Status != "done" {
		t.Fatalf("set_status plan = %+v", plan)
	}
}

func TestPlanAddInstances(t *testing.T) {
	f := newSuperFixture(t)
	x := f.issue("X")
	var number int32
	f.fx.QueryRow(t, `SELECT number FROM issue WHERE id=$1`, x).Scan(&number)
	f.fx.Exec(t, `UPDATE workspace SET issue_prefix='SAT' WHERE id=$1`, f.fx.WorkspaceID)
	ident := IssueIdentifier("SAT", number)

	f.turn("在 " + ident + " 上加开 2 个 尤雨溪，分别做 d 和 e")
	reply, plan := f.plan(fence(`{"type":"add_instances","issue":"` + ident + `","agent":"尤雨溪","splits":["d","e"],"priority":true}`))
	if plan == nil || plan.kind != "add_instances" {
		t.Fatalf("plan = %+v, reply %q", plan, reply)
	}
	if plan.payload.(map[string]any)["priority"].(bool) {
		t.Fatal("priority set without the user saying 优先 (D16)")
	}
	f.turn("在 " + ident + " 上优先加开 1 个 尤雨溪，做 f")
	_, plan = f.plan(fence(`{"type":"add_instances","issue":"` + ident + `","agent":"尤雨溪","splits":["f"],"priority":true}`))
	if plan == nil || !plan.payload.(map[string]any)["priority"].(bool) {
		t.Fatal("priority dropped although the user said 优先")
	}
	visible, plan := f.plan(fence(`{"type":"add_instances","issue":"` + ident + `","agent":"尤雨溪","splits":["1","2","3","4"]}`))
	if plan != nil || !strings.Contains(visible, "最多 3 个") {
		t.Fatalf("k>N: plan %v reply %q", plan, visible)
	}
}

// B3, B6, B7: confirm runs once however many clicks race; a newer card
// expires the older one; cancel is final.
func TestPlanCardLifecycle(t *testing.T) {
	f := newSuperFixture(t)
	ctx := context.Background()
	member := util.MustParseUUID(f.userID)
	write := func(js string) db.SuperAgentAction {
		t.Helper()
		f.turn("给尤雨溪建个任务：a，完成标准 b，先别跑")
		_, plan := f.plan(fence(js))
		msg := f.fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": f.session, "role": "assistant", "content": "好的。", "task_id": util.UUIDToString(f.task.ID)})
		m, err := f.svc.Queries.GetChatMessage(ctx, util.MustParseUUID(msg))
		if err != nil {
			t.Fatal(err)
		}
		if err := writeSuperAction(ctx, f.svc.Queries, m, plan); err != nil {
			t.Fatal(err)
		}
		var id pgtype.UUID
		f.fx.QueryRow(t, `SELECT id FROM super_agent_action WHERE chat_message_id=$1`, msg).Scan(&id)
		a, _ := f.svc.Queries.GetSuperAgentAction(ctx, id)
		return a
	}
	card := `{"type":"create_issues","issues":[{"title":"t","assignee":"尤雨溪","status":"backlog","what":"a","done":"b"}]}`
	old := write(card)
	newer := write(card)
	if a, _ := f.svc.Queries.GetSuperAgentAction(ctx, old.ID); a.Status != "expired" {
		t.Fatalf("older card status = %s, want expired (B7)", a.Status)
	}
	var mu sync.Mutex
	created := 0
	deps := SuperCardDeps{CreateIssue: func(ctx context.Context, item SuperIssueItem, desc string, origin pgtype.UUID) (SuperRef, error) {
		mu.Lock()
		created++
		mu.Unlock()
		if !strings.Contains(desc, "## 要做什么\na") || !strings.Contains(desc, "/chat?session=") || !strings.Contains(desc, "给尤雨溪建个任务") {
			t.Errorf("description = %q", desc)
		}
		return SuperRef{ID: "00000000-0000-0000-0000-000000000001", Identifier: "T-1"}, nil
	}}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.svc.ConfirmPlanCard(ctx, newer.ID, member, deps); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("issues created = %d, want 1 (B3)", created)
	}
	got, _ := f.svc.Queries.GetSuperAgentAction(ctx, newer.ID)
	card2 := PlanCardFromRow(got)
	if got.Status != "created" || len(card2.CreatedIssues) != 1 || card2.CreatedIssues[0].Identifier != "T-1" {
		t.Fatalf("confirmed card = %+v", card2)
	}
	if _, err := f.svc.ConfirmPlanCard(ctx, old.ID, member, deps); err == nil {
		t.Fatal("confirming an expired card succeeded")
	}
	third := write(card)
	if c, err := f.svc.CancelPlanCard(ctx, third.ID, member); err != nil || c.Status != "cancelled" {
		t.Fatalf("cancel = %+v, %v", c, err)
	}
	if _, err := f.svc.ConfirmPlanCard(ctx, third.ID, member, deps); err == nil || created != 1 {
		t.Fatal("cancelled card was confirmed (B6)")
	}
	stranger := util.MustParseUUID(f.fx.User(t, "stranger", "stranger-"+util.UUIDToString(third.ID)+"@example.com"))
	if _, err := f.svc.ConfirmPlanCard(ctx, third.ID, stranger, deps); err == nil {
		t.Fatal("another member confirmed the card")
	}
}

func TestPlanCardJSONShape(t *testing.T) {
	pending := PlanCard{ID: "c", Kind: "create_issues", Status: "pending"}
	b, _ := json.Marshal(pending)
	for _, want := range []string{`"created_issues":[]`, `"items":[]`, `"error":null`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("create_issues card %s lacks %s", b, want)
		}
	}
	b, _ = json.Marshal(PlanCard{ID: "c", Kind: "add_instances", Status: "pending", Splits: []string{"a"}})
	for _, want := range []string{`"batch_id":null`, `"priority":false`, `"splits":["a"]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("add_instances card %s lacks %s", b, want)
		}
	}
	if strings.Contains(string(b), "created_issues") {
		t.Errorf("add_instances card carries created_issues: %s", b)
	}
}
