package handler

import (
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

	top := issue("top", "in_review", "") // in review but top-level: not listed
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

	answeredBySuper := issue("answered by super", "in_progress", top)
	comment(answeredBySuper, "agent", worker, me+" v1 or v2?", 1)
	comment(answeredBySuper, "agent", superAgent, "用 v2", 2)

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
	expectChain(inReview, "in_review", child, top)
	expectChain(inReview, "in_review", grandchild, child, top)
	for name, id := range map[string]string{"top-level": top, "closed": closedChild, "foreign": foreign} {
		if _, ok := inReview[id]; ok {
			t.Fatalf("in_review lists the %s issue %s", name, id)
		}
	}

	expectChain(awaiting, "awaiting_reply", asked, top)
	expectChain(awaiting, "awaiting_reply", askedUpper, top)
	expectChain(awaiting, "awaiting_reply", askedAgain, top)
	expectChain(awaiting, "awaiting_reply", deepAsk, child, top)
	for name, id := range map[string]string{
		"answered": answered, "answered by super-agent": answeredBySuper, "cancelled": cancelled,
		"code span": inCode, "@all": atAll, "self mention": selfMention, "deleted": deletedAsk, "foreign": foreign,
	} {
		if _, ok := awaiting[id]; ok {
			t.Fatalf("awaiting_reply lists the %s issue %s", name, id)
		}
	}

	// Closing it drops it from in_review.
	dbfx.Exec(t, `UPDATE issue SET status = 'done' WHERE id = $1`, grandchild)
	testutil.Call(t, testHandler.ListIssueAttention, newRequest(http.MethodGet, "/api/issues/attention", nil)).
		Want(http.StatusOK).JSON(&response)
	if _, ok := index(response.InReview)[grandchild]; ok {
		t.Fatalf("in_review still lists %s after it was closed", grandchild)
	}
}
