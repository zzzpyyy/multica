package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
	atYou := "[@qa](mention://member/" + testUserID + ") "
	ask := fx.Comment(t, k, atYou+"接口用 v1 还是 v2？", testutil.Cols{"author_type": "agent", "author_id": worker})
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
		c := fx.Comment(t, issue, atYou+"要不要分页？", testutil.Cols{"author_type": "agent", "author_id": worker})
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

// R2 / R7 / R12: an answer quotes the member's words under the question and
// wakes the agent that asked; a source outside the tree turns into ask.
func TestSuperAnswerOutcome(t *testing.T) {
	fx, rt, sa := superAgentFixture(t)
	ctx := context.Background()
	asker := fx.Agent(t, "sa-asker", rt, testutil.Cols{"max_concurrent_tasks": 3})
	t1 := fx.Issue(t, "sa T1")
	k := fx.Issue(t, "sa K8", testutil.Cols{"parent_issue_id": t1, "assignee_type": "agent", "assignee_id": asker})
	other := fx.Issue(t, "sa T2")
	fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, k)
	said := fx.Comment(t, t1, "fork 仓库放 GitHub")
	notHere := fx.Comment(t, other, "放 iCode")

	run := func(question, sources string) (string, string) {
		t.Helper()
		ask := fx.Comment(t, k, question, testutil.Cols{"author_type": "agent", "author_id": asker})
		q := fx.Insert(t, "super_agent_question", testutil.Cols{"chat_session_id": sa.Session.ID, "comment_id": ask, "status": "answering"})
		id := fx.Task(t, sa.ID, testutil.Cols{"runtime_id": rt, "issue_id": k, "status": "completed", "trigger_comment_id": ask,
			"context": testutil.Raw(`jsonb_build_object('type','super_answer','question_id','` + q + `')`)})
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(id))
		if err != nil {
			t.Fatal(err)
		}
		testHandler.runSuperAnswer(ctx, &task, "```super-action\n{\"type\":\"answer\",\"sources\":["+sources+"]}\n```")
		var status string
		fx.QueryRow(t, `SELECT status FROM super_agent_question WHERE id=$1`, q).Scan(&status)
		return ask, status
	}

	ask, status := run("fork 仓库放 GitHub 还是 iCode？", `"c:`+said+`"`)
	var content string
	fx.QueryRow(t, `SELECT content FROM comment WHERE parent_id=$1 AND author_id=$2`, ask, sa.ID).Scan(&content)
	if status != "answered" || !strings.Contains(content, "super-agent 引用") || !strings.Contains(content, "fork 仓库放 GitHub") || !strings.Contains(content, "#comment-"+said) {
		t.Fatalf("answer: status %s, reply %q", status, content)
	}
	if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, k, asker); n != 1 {
		t.Fatalf("asking agent runs after the answer = %d, want 1", n)
	}

	ask, status = run("仓库公开还是私有？", `"c:`+notHere+`"`)
	if status != "asked" || fx.Count(t, `SELECT count(*) FROM comment WHERE parent_id=$1`, ask) != 0 {
		t.Fatalf("out-of-tree source: status %s, replies %d", status, fx.Count(t, `SELECT count(*) FROM comment WHERE parent_id=$1`, ask))
	}
}

// Rule 20: a question still answering after 4 minutes is pushed even while
// its answer run is still running (retrying, stuck); the run is cancelled,
// and an answer it returns afterwards writes nothing.
func TestOverdueQuestionPushedWhileRunStillRunning(t *testing.T) {
	fx, rt, sa := superAgentFixture(t)
	ctx := context.Background()
	asker := fx.Agent(t, "sa-slow-asker", rt)
	t1 := fx.Issue(t, "sa slow T1")
	k := fx.Issue(t, "sa slow K", testutil.Cols{"parent_issue_id": t1, "assignee_type": "agent", "assignee_id": asker})
	fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, k)
	fx.Cleanup(t, `DELETE FROM super_agent_push WHERE chat_session_id=$1`, sa.Session.ID)
	said := fx.Comment(t, t1, "用 v2")
	ask := fx.Comment(t, k, "接口用 v1 还是 v2？", testutil.Cols{"author_type": "agent", "author_id": asker})
	q := fx.Insert(t, "super_agent_question", testutil.Cols{"chat_session_id": sa.Session.ID, "comment_id": ask, "status": "answering",
		"created_at": testutil.Raw(`now() - interval '4 minutes 10 seconds'`)})
	fx.Cleanup(t, `DELETE FROM super_agent_question WHERE id=$1`, q)
	id := fx.Task(t, sa.ID, testutil.Cols{"runtime_id": rt, "issue_id": k, "status": "running", "started_at": time.Now(), "trigger_comment_id": ask,
		"context": testutil.Raw(`jsonb_build_object('type','super_answer','question_id','` + q + `')`)})
	fx.Exec(t, `UPDATE super_agent_question SET task_id=$2 WHERE id=$1`, q, id)

	if err := testHandler.TaskService.SweepSuperAgentQuestions(ctx); err != nil {
		t.Fatal(err)
	}
	var status, taskStatus string
	fx.QueryRow(t, `SELECT status FROM super_agent_question WHERE id=$1`, q).Scan(&status)
	fx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, id).Scan(&taskStatus)
	if status != "asked" || taskStatus != "cancelled" {
		t.Fatalf("after the sweep: question %s, run %s; want asked, cancelled", status, taskStatus)
	}
	if n := fx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id=$1 AND task_id IS NULL AND content LIKE '%接口用 v1 还是 v2%'`, sa.Session.ID); n != 1 {
		t.Fatalf("question pushes = %d, want 1", n)
	}

	// The run comes back late with an answer: one outcome per question.
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	testHandler.runSuperAnswer(ctx, &task, "```super-action\n{\"type\":\"answer\",\"sources\":[\"c:"+said+"\"]}\n```")
	if n := fx.Count(t, `SELECT count(*) FROM comment WHERE parent_id=$1`, ask); n != 0 {
		t.Fatalf("late answer wrote %d replies", n)
	}
	if err := testHandler.TaskService.SweepSuperAgentQuestions(ctx); err != nil { // a second sweep pushes nothing new
		t.Fatal(err)
	}
	if n := fx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id=$1 AND task_id IS NULL AND content LIKE '%接口用 v1 还是 v2%'`, sa.Session.ID); n != 1 {
		t.Fatalf("question pushes after a second sweep = %d, want 1", n)
	}

	// Never claimed (daemon offline, no free slot): pushed the same way.
	ask2 := fx.Comment(t, k, "要不要支持分页？", testutil.Cols{"author_type": "agent", "author_id": asker})
	q2 := fx.Insert(t, "super_agent_question", testutil.Cols{"chat_session_id": sa.Session.ID, "comment_id": ask2, "status": "answering",
		"created_at": testutil.Raw(`now() - interval '4 minutes 10 seconds'`)})
	fx.Cleanup(t, `DELETE FROM super_agent_question WHERE id=$1`, q2)
	id2 := fx.Task(t, sa.ID, testutil.Cols{"runtime_id": rt, "issue_id": k, "status": "queued", "trigger_comment_id": ask2,
		"context": testutil.Raw(`jsonb_build_object('type','super_answer','question_id','` + q2 + `')`)})
	fx.Exec(t, `UPDATE super_agent_question SET task_id=$2 WHERE id=$1`, q2, id2)
	if err := testHandler.TaskService.SweepSuperAgentQuestions(ctx); err != nil {
		t.Fatal(err)
	}
	fx.QueryRow(t, `SELECT status FROM super_agent_question WHERE id=$1`, q2).Scan(&status)
	fx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, id2).Scan(&taskStatus)
	if status != "asked" || taskStatus != "cancelled" ||
		fx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id=$1 AND task_id IS NULL AND content LIKE '%要不要支持分页%'`, sa.Session.ID) != 1 {
		t.Fatalf("queued answer run: question %s, run %s", status, taskStatus)
	}
}

// R1, R4 (BUNN-31 Nest batch; BUNN-29 "Nest 批 R1 修复", 一鸣 11-17):
// whether an @ still waits for the member is one judgment, made per @. The
// same data goes through both real code paths, the super-agent's waiting
// questions and GET /api/issues/attention, and the two must agree.
func TestSuperAgentWaitingQuestionsMatchAttention(t *testing.T) {
	fx, rt, sa := superAgentFixture(t)
	ctx := context.Background()
	worker := fx.Agent(t, "sa-open-worker", rt, testutil.Cols{"max_concurrent_tasks": 3})
	other := fx.User(t, "sa open other", "sa-open-other-"+sa.ID[:8]+"@multica.ai")
	fx.Member(t, testWorkspaceID, other, "member")
	atYou := "[@qa](mention://member/" + testUserID + ") "
	t1 := fx.Issue(t, "sa open T1")
	said := fx.Comment(t, t1, "用 v2") // the member's words in the tree: what answers quote
	ago := func(seconds int) testutil.Cols {
		return testutil.Cols{"created_at": testutil.Raw(fmt.Sprintf("now() - interval '%d seconds'", seconds))}
	}
	newIssue := func(title string) string {
		issue := fx.Issue(t, title, testutil.Cols{"parent_issue_id": t1, "assignee_type": "agent", "assignee_id": worker})
		fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issue)
		return issue
	}
	askOn := func(issue, authorType, authorID, question, status string, secondsAgo int) string {
		cols := ago(secondsAgo)
		cols["author_type"], cols["author_id"] = authorType, authorID
		comment := fx.Comment(t, issue, atYou+question, cols)
		fx.Insert(t, "super_agent_question", testutil.Cols{"chat_session_id": sa.Session.ID, "comment_id": comment, "status": status})
		return comment
	}
	ask := func(title, question string) (string, string) {
		issue := newIssue(title)
		return issue, askOn(issue, "agent", worker, question, "asked", 120)
	}
	// answer runs the real answer-run outcome for an agent's question.
	answer := func(issue, question string, partial bool) {
		t.Helper()
		var q string
		fx.QueryRow(t, `SELECT id FROM super_agent_question WHERE comment_id=$1`, question).Scan(&q)
		fx.Exec(t, `UPDATE super_agent_question SET status='answering' WHERE id=$1`, q)
		id := fx.Task(t, sa.ID, testutil.Cols{"runtime_id": rt, "issue_id": issue, "status": "completed", "trigger_comment_id": question,
			"context": testutil.Raw(`jsonb_build_object('type','super_answer','question_id','` + q + `')`)})
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(id))
		if err != nil {
			t.Fatal(err)
		}
		testHandler.runSuperAnswer(ctx, &task, fmt.Sprintf("```super-action\n{\"type\":\"answer\",\"sources\":[\"c:%s\"],\"partial\":%v}\n```", said, partial))
	}

	k4, _ := ask("open K4", "接口用 v1 还是 v2？")
	replied, _ := ask("replied R1", "用哪个方案？")
	fx.Comment(t, replied, "用方案 A", ago(60)) // the member answered on the issue, not in the chat
	done, _ := ask("done", "要不要加缓存？")
	fx.Exec(t, `UPDATE issue SET status='done' WHERE id=$1`, done)
	cancelled, _ := ask("cancelled G2", "要不要加日志？")
	fx.Exec(t, `UPDATE issue SET status='cancelled' WHERE id=$1`, cancelled)
	edited, editedComment := ask("edited E", "要不要分页？")
	fx.Exec(t, `UPDATE comment SET content='要不要分页？' WHERE id=$1`, editedComment)
	deleted, deletedComment := ask("deleted D", "要不要写迁移？")
	fx.Exec(t, `UPDATE comment SET deleted_at=now() WHERE id=$1`, deletedComment)
	otherReplied, _ := ask("other member replied O", "超时设多少秒？")
	fx.Comment(t, otherReplied, "我觉得 10 秒", testutil.Cols{"author_id": other, "created_at": testutil.Raw(`now() - interval '1 minute'`)})
	forwarded, _ := ask("forwarded F", "要不要加监控？")
	turn := newSuperTurn(t, fx, sa, rt, identOf(t, fx, forwarded)+" 要加")
	turn.action(t, "forward", "pending", map[string]any{"issue": identOf(t, fx, forwarded), "message_ids": turn.messages, "text": identOf(t, fx, forwarded) + " 要加"})
	turn.run(t)
	answeredFully, fullQuestion := ask("fully answered A", "接口用 v1 还是 v2？")
	answer(answeredFully, fullQuestion, false)
	answeredPartly, partQuestion := ask("partly answered P", "接口用 v1 还是 v2？超时多少？")
	answer(answeredPartly, partQuestion, true)
	// BUNN-55: another member asks first; later the assignee asks a second
	// question there and the super-agent answers that one fully.
	y := newIssue("BUNN-55 Y")
	askOn(y, "member", other, "要不要加日志？", "asked", 120)
	taQuestion := askOn(y, "agent", worker, "接口用 v1 还是 v2？", "asked", 90)
	answer(y, taQuestion, false)

	waitingInChat := func() map[string]bool {
		t.Helper()
		qs, err := testHandler.openSuperAgentQuestions(ctx, chatSessionOf(t, sa))
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, q := range qs {
			out[uuidToString(q.IssueID)] = true
		}
		return out
	}
	waitingInAttention := func() map[string]bool {
		t.Helper()
		var response issueAttentionResponse
		testutil.Call(t, testHandler.ListIssueAttention, newRequest(http.MethodGet, "/api/issues/attention", nil)).
			Want(http.StatusOK).JSON(&response)
		out := map[string]bool{}
		for _, item := range response.AwaitingReply {
			out[item.IssueID] = true
		}
		return out
	}
	want := map[string]bool{
		k4: true, otherReplied: true, answeredPartly: true, y: true,
		replied: false, done: false, cancelled: false, edited: false, deleted: false, forwarded: false, answeredFully: false,
	}
	chat, attention := waitingInChat(), waitingInAttention()
	for issue, w := range want {
		if chat[issue] != w || attention[issue] != w {
			var title string
			fx.QueryRow(t, `SELECT title FROM issue WHERE id=$1`, issue).Scan(&title)
			t.Fatalf("%s: waiting in chat %v, in attention %v; want %v", title, chat[issue], attention[issue], w)
		}
	}

	// Several really waiting: an answer that names no issue is not forwarded.
	before := fx.Count(t, `SELECT count(*) FROM comment WHERE author_id=$1`, sa.ID)
	turn = newSuperTurn(t, fx, sa, rt, "可以")
	turn.action(t, "forward", "pending", map[string]any{"issue": "", "message_ids": turn.messages, "text": "可以"})
	turn.run(t)
	if after := fx.Count(t, `SELECT count(*) FROM comment WHERE author_id=$1`, sa.ID); after != before {
		t.Fatalf("forward with several questions waiting posted %d comments", after-before)
	}

	// The R1 scenario: R1 answered on the issue, G2 cancelled, only K4 left.
	for _, issue := range []string{otherReplied, answeredPartly, y} {
		fx.Exec(t, `UPDATE issue SET status='cancelled' WHERE id=$1`, issue)
	}
	turn = newSuperTurn(t, fx, sa, rt, "用 v2")
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(turn.task))
	if err != nil {
		t.Fatal(err)
	}
	block, err := testHandler.superAgentContext(ctx, &task)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(block, "## 等用户回答的问题")
	if at < 0 || !strings.Contains(block[at:], identOf(t, fx, k4)) || strings.Contains(block[at:], identOf(t, fx, replied)+"：") {
		t.Fatalf("turn context waiting section:\n%s", block)
	}
	turn.action(t, "forward", "pending", map[string]any{"issue": "", "message_ids": turn.messages, "text": "用 v2"})
	turn.run(t)
	if n := fx.Count(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND author_id=$2 AND content LIKE '%用 v2%'`, k4, sa.ID); n != 1 {
		t.Fatalf("forward comments on K4 = %d, want 1", n)
	}
}

func chatSessionOf(t *testing.T, sa superAgentResponse) db.ChatSession {
	t.Helper()
	session, err := testHandler.Queries.GetChatSession(context.Background(), parseUUID(sa.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	return session
}
