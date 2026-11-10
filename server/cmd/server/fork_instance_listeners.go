package main

import (
	"context"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (zzzpyyy/multica), D: settle an instance batch when its last instance
// ends. A failed attempt with a retry pending is not an end. Lost events are
// covered by the instance_batch_settle_sweep job.
func registerForkInstanceListeners(bus *events.Bus, tasks *service.TaskService) {
	ctx := context.Background()
	settle := func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		if e.Type == protocol.EventTaskFailed {
			if retryPending, _ := payload["retry_pending"].(bool); retryPending {
				return
			}
		}
		taskID, _ := payload["task_id"].(string)
		id, err := util.ParseUUID(taskID)
		if err != nil {
			return
		}
		tasks.OnTaskTerminal(ctx, id)
	}
	for _, t := range []string{protocol.EventTaskCompleted, protocol.EventTaskFailed, protocol.EventTaskCancelled} {
		bus.Subscribe(t, settle)
	}
}
