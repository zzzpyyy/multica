package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Fork (PRD A rule 7): GET /api/issues/attention lists in-review sub-issues
// at any depth and issues waiting for the requester's reply, each with its
// parent chain, nearest first.
func TestListIssueAttention(t *testing.T) {
	suffix := time.Now().UnixNano()
	other := dbfx.User(t, fmt.Sprintf("attn other %d", suffix), fmt.Sprintf("attn-other-%d@example.com", suffix))
	dbfx.Member(t, testWorkspaceID, other, "member")
	superAgent := dbfx.Agent(t, fmt.Sprintf("attn super %d", suffix), testRuntimeID, testutil.Cols{"system_key": "super"})
	worker := dbfx.Agent(t, fmt.Sprintf("attn worker %d", suffix), testRuntimeID)

	issue := func(title, status, parent string) string {
		cols := testutil.Cols{"status": status}
		if parent != "" {
			cols["parent_issue_id"] = parent
		}
		return dbfx.Issue(t, fmt.Sprintf("attn %s %d", title, suffix), cols)
	}
	at := func(minutes int) string {
		return time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
	}
	comment := func(issueID, authorType, authorID, content string, minute int, over ...testutil.Cols) {
		cols := testutil.Cols{"author_type": authorType, "author_id": authorID, "created_at": at(minute)}
		for _, o := range over {
			for k, v := range o {
				cols[k] = v
			}
		}
		dbfx.Comment(t, issueID, content, cols)
	}
	me := fmt.Sprintf("[@Me](mention://member/%s)", testUserID)

	top := issue("top", "in_review", "") // top-level in review: listed, no ancestors (SB4a)
	child := issue("child", "in_review", top)
	grandchild := issue("grandchild", "in_review", child)
	closedChild := issue("closed child", "done", top)

	// awaiting_reply cases, all under `top`.
	asked := issue("asked", "in_progress", top)
	comment(asked, "member", other, me+" 用哪个方案？", 1)

	askedUpper := issue("asked upper", "todo", top)
	comment(askedUpper, "agent", worker, fmt.Sprintf("[@Me](mention://member/%s) 要不要加日志？", strings.ToUpper(testUserID)), 1)

	answered := issue("answered", "in_progress", top)
	comment(answered, "member", other, me+" ok?", 1)
	comment(answered, "member", testUserID, "ok", 2)

	// A super-agent forward of your chat answer is your reply (一鸣 11-17);
	// any other super-agent comment on the issue is not (see
	// TestSuperAgentWaitingQuestionsMatchAttention for answers).
	superSession := dbfx.ChatSession(t, superAgent)
	chatTurn := dbfx.Task(t, superAgent, testutil.Cols{"runtime_id": testRuntimeID, "chat_session_id": superSession, "status": "completed"})
	answeredBySuper := issue("answered by super", "in_progress", top)
	comment(answeredBySuper, "agent", worker, me+" v1 or v2?", 1)
	comment(answeredBySuper, "agent", superAgent, "用 v2", 2, testutil.Cols{"source_task_id": chatTurn})
	notAForward := issue("super comment, not a forward", "in_progress", top)
	comment(notAForward, "agent", worker, me+" 要不要加缓存？", 1)
	comment(notAForward, "agent", superAgent, "> super-agent 引用 Me 的原话", 2)

	askedAgain := issue("asked again", "in_progress", top)
	comment(askedAgain, "member", other, me+" first?", 1)
	comment(askedAgain, "member", testUserID, "yes", 2)
	comment(askedAgain, "member", other, me+" second?", 3)

	cancelled := issue("cancelled", "cancelled", top)
	comment(cancelled, "member", other, me+" still?", 1)

	inCode := issue("in code", "in_progress", top)
	comment(inCode, "member", other, "write it as `"+me+"`", 1)

	atAll := issue("at all", "in_progress", top)
	comment(atAll, "member", other, "[@all](mention://all/all) heads up", 1)

	selfMention := issue("self mention", "in_progress", top)
	comment(selfMention, "member", testUserID, me+" note to self", 1)

	deletedAsk := issue("deleted ask", "in_progress", top)
	comment(deletedAsk, "member", other, me+" gone?", 1, testutil.Cols{"deleted_at": at(2)})

	deepAsk := issue("deep ask", "todo", child)
	comment(deepAsk, "member", other, me+" deep?", 1)

	otherWorkspace := dbfx.Workspace(t, fmt.Sprintf("attn ws %d", suffix), fmt.Sprintf("attn-ws-%d", suffix))
	foreignParent := dbfx.Issue(t, fmt.Sprintf("attn foreign %d", suffix), testutil.Cols{"workspace_id": otherWorkspace})
	foreign := dbfx.Issue(t, fmt.Sprintf("attn foreign child %d", suffix), testutil.Cols{
		"workspace_id": otherWorkspace, "status": "in_review", "parent_issue_id": foreignParent,
	})
	dbfx.Comment(t, foreign, me+" elsewhere", testutil.Cols{"workspace_id": otherWorkspace, "author_id": other})

	var response issueAttentionResponse
	testutil.Call(t, testHandler.ListIssueAttention, newRequest(http.MethodGet, "/api/issues/attention", nil)).
		Want(http.StatusOK).JSON(&response)
	index := func(items []issueAttentionItem) map[string][]string {
		out := map[string][]string{}
		for _, item := range items {
			out[item.IssueID] = item.AncestorIDs
		}
		return out
	}
	inReview, awaiting := index(response.InReview), index(response.AwaitingReply)

	expectChain := func(list map[string][]string, name, id string, chain ...string) {
		t.Helper()
		got, ok := list[id]
		if !ok {
			t.Fatalf("%s: %s missing", name, id)
		}
		if fmt.Sprint(got) != fmt.Sprint(chain) {
			t.Fatalf("%s: %s ancestors = %v, want %v", name, id, got, chain)
		}
	}
	expectChain(inReview, "in_review", top)
	expectChain(inReview, "in_review", child, top)
	expectChain(inReview, "in_review", grandchild, child, top)
	for name, id := range map[string]string{"closed": closedChild, "foreign": foreign} {
		if _, ok := inReview[id]; ok {
			t.Fatalf("in_review lists the %s issue %s", name, id)
		}
	}

	expectChain(awaiting, "awaiting_reply", asked, top)
	expectChain(awaiting, "awaiting_reply", askedUpper, top)
	expectChain(awaiting, "awaiting_reply", askedAgain, top)
	expectChain(awaiting, "awaiting_reply", deepAsk, child, top)
	expectChain(awaiting, "awaiting_reply", notAForward, top)
	for name, id := range map[string]string{
		"answered": answered, "answered by super-agent": answeredBySuper, "cancelled": cancelled,
		"code span": inCode, "@all": atAll, "self mention": selfMention, "deleted": deletedAsk, "foreign": foreign,
	} {
		if _, ok := awaiting[id]; ok {
			t.Fatalf("awaiting_reply lists the %s issue %s", name, id)
		}
	}

	// None of these parents has an assignee: in_review_by_others is empty.
	var raw map[string]json.RawMessage
	testutil.Call(t, testHandler.ListIssueAttention, newRequest(http.MethodGet, "/api/issues/attention", nil)).
		Want(http.StatusOK).JSON(&raw)
	if got := string(raw["in_review_by_others"]); got != "[]" {
		t.Fatalf("in_review_by_others = %s, want []", got)
	}
	if got := string(raw["in_review"]); !strings.Contains(got, `"issue_id":"`+top+`","ancestor_ids":[]`) {
		t.Fatalf("top-level in_review item = %s, want an empty ancestor_ids array", got)
	}

	// Closing it drops it from in_review.
	dbfx.Exec(t, `UPDATE issue SET status = 'done' WHERE id = $1`, grandchild)
	testutil.Call(t, testHandler.ListIssueAttention, newRequest(http.MethodGet, "/api/issues/attention", nil)).
		Want(http.StatusOK).JSON(&response)
	if _, ok := index(response.InReview)[grandchild]; ok {
		t.Fatalf("in_review still lists %s after it was closed", grandchild)
	}
}

// Fork (A rule 4, SB4b): every in-review issue is in exactly one list, and
// the split is service.ResolveSubIssueReviewer's.
func TestListIssueAttentionByReviewer(t *testing.T) {
	suffix := time.Now().UnixNano()
	other := dbfx.User(t, fmt.Sprintf("rv other %d", suffix), fmt.Sprintf("rv-other-%d@example.com", suffix))
	dbfx.Member(t, testWorkspaceID, other, "member")
	gone := dbfx.User(t, fmt.Sprintf("rv gone %d", suffix), fmt.Sprintf("rv-gone-%d@example.com", suffix))
	la := dbfx.Agent(t, fmt.Sprintf("rv LA %d", suffix), testRuntimeID)
	ma := dbfx.Agent(t, fmt.Sprintf("rv MA %d", suffix), testRuntimeID)
	archived := dbfx.Agent(t, fmt.Sprintf("rv archived %d", suffix), testRuntimeID, testutil.Cols{"archived_at": testutil.Raw("now()")})
	noRuntime := dbfx.Agent(t, fmt.Sprintf("rv no runtime %d", suffix), testRuntimeID)
	dbfx.Exec(t, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, noRuntime)
	sq := dbfx.Squad(t, fmt.Sprintf("rv SQ %d", suffix), la)
	issue := func(title string, cols testutil.Cols) string {
		return dbfx.Issue(t, fmt.Sprintf("rv %s %d", title, suffix), cols)
	}
	parent := func(title, typ, id string) string {
		cols := testutil.Cols{"status": "in_progress"}
		if typ != "" {
			cols["assignee_type"], cols["assignee_id"] = typ, id
		}
		return issue(title, cols)
	}
	child := func(title, parentID, typ, id string) string {
		cols := testutil.Cols{"status": "in_review", "parent_issue_id": parentID}
		if typ != "" {
			cols["assignee_type"], cols["assignee_id"] = typ, id
		}
		return issue(title, cols)
	}
	type want struct{ list, typ, id string }
	cases := map[string]want{}
	add := func(id string, w want) { cases[id] = w }
	add(issue("top", testutil.Cols{"status": "in_review", "assignee_type": "squad", "assignee_id": sq}), want{list: "mine"})
	add(child("unowned parent", parent("p none", "", ""), "agent", ma), want{list: "mine"})
	add(child("parent is me", parent("p me", "member", testUserID), "agent", ma), want{list: "mine"})
	add(child("parent other member", parent("p other", "member", other), "agent", ma), want{"others", "member", other})
	add(child("parent member left", parent("p gone", "member", gone), "agent", ma), want{list: "mine"})
	add(child("parent agent", parent("p LA", "agent", la), "agent", ma), want{"others", "agent", la})
	kSquad := child("parent squad", parent("p SQ", "squad", sq), "agent", ma)
	add(kSquad, want{"others", "agent", la})
	add(child("grandchild", kSquad, "agent", la), want{"others", "agent", ma}) // its parent is kSquad, owned by MA
	add(child("parent archived", parent("p archived", "agent", archived), "agent", ma), want{list: "mine"})
	add(child("parent no runtime", parent("p no runtime", "agent", noRuntime), "agent", ma), want{list: "mine"})
	add(child("self", parent("p self", "agent", la), "agent", la), want{list: "mine"})
	add(child("self via squads", parent("p self squad", "squad", sq), "squad", sq), want{list: "mine"})

	read := func() (map[string]bool, map[string]issueReviewerRef) {
		var response issueAttentionResponse
		testutil.Call(t, testHandler.ListIssueAttention, newRequest(http.MethodGet, "/api/issues/attention", nil)).
			Want(http.StatusOK).JSON(&response)
		mine := map[string]bool{}
		for _, item := range response.InReview {
			mine[item.IssueID] = true
		}
		others := map[string]issueReviewerRef{}
		for _, item := range response.InReviewByOthers {
			if _, dup := others[item.IssueID]; dup {
				t.Fatalf("%s listed twice in in_review_by_others", item.IssueID)
			}
			others[item.IssueID] = item.Reviewer
		}
		return mine, others
	}
	mine, others := read()
	for id, w := range cases {
		reviewer, isOther := others[id]
		if mine[id] == isOther {
			t.Fatalf("%s: in_review=%v in_review_by_others=%v, want exactly one", id, mine[id], isOther)
		}
		switch w.list {
		case "mine":
			if !mine[id] {
				t.Fatalf("%s: want in in_review, got reviewer %+v", id, reviewer)
			}
		case "others":
			if reviewer != (issueReviewerRef{Type: w.typ, ID: w.id}) {
				t.Fatalf("%s: reviewer = %+v, want %s %s", id, reviewer, w.typ, w.id)
			}
		}
	}
	// Invariant over the whole workspace: every in-review issue is in one list.
	rows, err := testPool.Query(context.Background(), `SELECT id FROM issue WHERE workspace_id = $1 AND status = 'in_review'`, testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, isOther := others[id]; mine[id] == isOther {
			t.Fatalf("%s: in_review=%v in_review_by_others=%v", id, mine[id], isOther)
		}
	}
	rows.Close()
}
