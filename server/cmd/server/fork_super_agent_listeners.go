package main

import (
	"context"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (zzzpyyy/multica), B: server-side pushes and the answer-on-your-behalf
// entry point. Pushes never start a super-agent run (G5).
func registerForkSuperAgentListeners(bus *events.Bus, tasks *service.TaskService) {
	ctx := context.Background()
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		if changed, _ := payload["status_changed"].(bool); !changed {
			return
		}
		// The payload is a handler.IssueResponse or a map (two writers);
		// re-read the issue by id either way.
		issue, ok := extractIssueFields(payload["issue"])
		if id, err := util.ParseUUID(issue.ID); ok && err == nil {
			tasks.OnIssueStatusChanged(ctx, id)
		}
	})
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		var commentID string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			commentID = c.ID
		case map[string]any:
			commentID, _ = c["id"].(string)
		}
		if id, err := util.ParseUUID(commentID); err == nil {
			tasks.OnCommentCreated(ctx, id)
		}
	})
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		if retryPending, _ := payload["retry_pending"].(bool); retryPending {
			return
		}
		taskID, _ := payload["task_id"].(string)
		if id, err := util.ParseUUID(taskID); err == nil {
			tasks.OnTaskFinalFailure(ctx, id)
		}
	})
}
