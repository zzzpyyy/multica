package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Fork (A rule 4, plan v2.4-A2-r4 §3): owner_is_you only when this run's agent
// owns both the sub-issue and its parent (a squad as its leader).
func TestClaimParentOwnerIsYou(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	n := time.Now().UnixNano()
	la := dbfx.Agent(t, fmt.Sprintf("owner LA %d", n), testRuntimeID)
	ma := dbfx.Agent(t, fmt.Sprintf("owner MA %d", n), testRuntimeID)
	sq := dbfx.Squad(t, fmt.Sprintf("owner SQ %d", n), la)
	issue := func(title string, cols testutil.Cols) string {
		return dbfx.Issue(t, fmt.Sprintf("owner %s %d", title, n), cols)
	}
	for _, tc := range []struct {
		name                 string
		parentType, parentID string
		childType, childID   string
		runAgent             string
		want                 bool
	}{
		{"both squad, leader runs", "squad", sq, "squad", sq, la, true},
		{"both LA, LA runs", "agent", la, "agent", la, la, true},
		{"parent squad, child MA, leader runs (mentioned)", "squad", sq, "agent", ma, la, false},
		{"parent LA, child MA, MA runs", "agent", la, "agent", ma, ma, false},
		{"both LA, another agent runs", "agent", la, "agent", la, ma, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := issue("parent "+tc.name, testutil.Cols{"status": "in_progress", "assignee_type": tc.parentType, "assignee_id": tc.parentID})
			child := issue("child "+tc.name, testutil.Cols{"status": "in_progress", "parent_issue_id": parent, "assignee_type": tc.childType, "assignee_id": tc.childID})
			taskID := dbfx.Task(t, tc.runAgent, testutil.Cols{"issue_id": child, "runtime_id": testRuntimeID, "status": "dispatched"})
			task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			c, err := testHandler.Queries.GetIssue(ctx, parseUUID(child))
			if err != nil {
				t.Fatal(err)
			}
			got, _ := testHandler.loadClaimParentIssue(ctx, &task, c)
			if got == nil || got.OwnerIsYou != tc.want {
				t.Fatalf("owner_is_you = %+v, want %v", got, tc.want)
			}
		})
	}
}
