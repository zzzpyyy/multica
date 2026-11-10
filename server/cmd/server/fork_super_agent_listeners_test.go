package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (F3, R2, R10): an agent's @ of a member who has opened the
// super-agent creates no inbox item for that member (neither "mentioned"
// nor the subscriber "new_comment"); a member's @ still does.
func TestAgentMentionSkipsInboxOfSuperAgentUser(t *testing.T) {
	fx := workspaceFixture(t)
	user := fx.User(t, "sa inbox user", "sa-inbox-user@multica.ai")
	fx.Member(t, testWorkspaceID, user, "member")
	rt := fx.Runtime(t, "sa-inbox-rt")
	asker := fx.Agent(t, "sa-inbox-asker", rt)
	sa := fx.Agent(t, "sa-inbox-super", rt, testutil.Cols{"system_key": "super"})
	fx.ChatSession(t, sa, testutil.Cols{"creator_id": user})
	issueID := fx.Issue(t, "sa inbox")
	fx.Exec(t, `INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason) VALUES ($1, 'member', $2, 'manual')`, issueID, user)
	fx.Cleanup(t, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
	fx.Cleanup(t, `DELETE FROM issue_subscriber WHERE issue_id = $1`, issueID)

	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	publish := func(authorType, authorID, content string) {
		commentID := fx.Comment(t, issueID, content, testutil.Cols{"author_type": authorType, "author_id": authorID})
		bus.Publish(events.Event{
			Type: protocol.EventCommentCreated, WorkspaceID: testWorkspaceID, ActorType: authorType, ActorID: authorID,
			Payload: map[string]any{
				"comment":     handler.CommentResponse{ID: commentID, IssueID: issueID, AuthorType: authorType, AuthorID: authorID, Content: content, Type: "comment"},
				"issue_title": "sa inbox", "issue_status": "todo",
			},
		})
	}
	mention := "[@u](mention://member/" + user + ") 接口用 v1 还是 v2？"
	publish("agent", asker, mention)
	if n := fx.Count(t, `SELECT count(*) FROM inbox_item WHERE issue_id=$1 AND recipient_id=$2`, issueID, user); n != 0 {
		t.Fatalf("agent @ reached the member's inbox (%d items)", n)
	}
	publish("member", testUserID, mention)
	if n := fx.Count(t, `SELECT count(*) FROM inbox_item WHERE issue_id=$1 AND recipient_id=$2 AND type='mentioned'`, issueID, user); n != 1 {
		t.Fatalf("member @ inbox items = %d, want 1", n)
	}
}
