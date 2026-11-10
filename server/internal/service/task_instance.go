package service

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (zzzpyyy/multica), D: role instances ("add-instances").

// InstancePriorityBoost is the queue priority of an instance the user asked to
// prioritise. Upstream priorities are 0–4 (chat retries use up to 3), and
// ClaimAgentTask orders priority >= InstancePriorityBoost before everything
// else for the agent (D7, D16).
const InstancePriorityBoost = 10

// nudgeIfAgentHasMoreQueued wakes the daemon again right after a claim when the
// agent still has capacity and queued work. A batch claim takes at most one
// task per agent per poll, and with a WebSocket the next poll can be minutes
// away, so instance 2..k of a batch would otherwise start late (D1).
// running is the agent's active count including the task just claimed.
func (s *TaskService) nudgeIfAgentHasMoreQueued(ctx context.Context, claimed db.AgentTaskQueue, running int64, maxConcurrent int32) {
	if running >= int64(maxConcurrent) {
		return
	}
	more, err := s.Queries.HasQueuedTaskForAgentOnRuntime(ctx, db.HasQueuedTaskForAgentOnRuntimeParams{
		AgentID:   claimed.AgentID,
		RuntimeID: claimed.RuntimeID,
	})
	if err != nil {
		slog.Debug("claim nudge: queued check failed", "agent_id", util.UUIDToString(claimed.AgentID), "error", err)
		return
	}
	if more {
		s.notifyRuntimeMayHaveWork(claimed.RuntimeID, util.UUIDToString(claimed.ID))
	}
}
