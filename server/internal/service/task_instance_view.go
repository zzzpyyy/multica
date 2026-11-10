package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (D): read model for GET /api/issues/{id}/instances (plan 3.2).
// Queue position and reason are computed on read, never stored.

type InstanceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type InstanceRequester struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type InstanceQueue struct {
	Position int    `json:"position"`
	Reason   string `json:"reason"`
	Running  *int64 `json:"running"`
	Limit    *int64 `json:"limit"`
}

type TaskInstance struct {
	Index       int            `json:"index"`
	Split       string         `json:"split"`
	Status      string         `json:"status"`
	TaskID      string         `json:"task_id"`
	TaskIDs     []string       `json:"task_ids"`
	SessionID   *string        `json:"session_id"`
	WorkDir     *string        `json:"work_dir"`
	BranchName  *string        `json:"branch_name"`
	StartedAt   *time.Time     `json:"started_at"`
	CompletedAt *time.Time     `json:"completed_at"`
	Queue       *InstanceQueue `json:"queue"`
}

type TaskInstanceBatch struct {
	ID          string            `json:"id"`
	IssueID     string            `json:"issue_id"`
	Agent       InstanceRef       `json:"agent"`
	Total       int               `json:"total"`
	Priority    bool              `json:"priority"`
	RequestedBy InstanceRequester `json:"requested_by"`
	CreatedAt   time.Time         `json:"created_at"`
	SettledAt   *time.Time        `json:"settled_at"`
	Instances   []TaskInstance    `json:"instances"`
}

// Queue reasons (plan 2.3 "排队位置和原因"), checked in this order.
const (
	QueueReasonRuntimeOffline  = "runtime_offline"
	QueueReasonRoleFull        = "role_full"
	QueueReasonRuntimeFull     = "runtime_full"
	QueueReasonWaitingDispatch = "waiting_dispatch"
)

// InstanceStatus maps a queue row status onto the contract's five values.
func InstanceStatus(taskStatus string) string {
	switch taskStatus {
	case "dispatched", "running", "waiting_local_directory":
		return "running"
	case "completed", "failed", "cancelled":
		return taskStatus
	default: // queued, deferred
		return "queued"
	}
}

// ListInstances returns the issue's batches, newest first.
func (s *TaskService) ListInstances(ctx context.Context, issue db.Issue) ([]TaskInstanceBatch, error) {
	batches, err := s.Queries.ListIssueTaskBatches(ctx, db.ListIssueTaskBatchesParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	out := make([]TaskInstanceBatch, 0, len(batches))
	if len(batches) == 0 {
		return out, nil
	}
	ids := make([]pgtype.UUID, len(batches))
	for i, b := range batches {
		ids[i] = b.ID
	}
	rows, err := s.Queries.ListBatchTasks(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list batch tasks: %w", err)
	}
	// attempts[batch][index] oldest → newest.
	attempts := map[string]map[int16][]db.AgentTaskQueue{}
	for _, r := range rows {
		b := util.UUIDToString(r.BatchID)
		if attempts[b] == nil {
			attempts[b] = map[int16][]db.AgentTaskQueue{}
		}
		attempts[b][r.BatchIndex.Int16] = append(attempts[b][r.BatchIndex.Int16], r)
	}

	queues := map[string]*agentQueue{}
	names := map[string]string{}
	for _, b := range batches {
		agentKey := util.UUIDToString(b.AgentID)
		q, ok := queues[agentKey]
		if !ok {
			if q, err = s.loadAgentQueue(ctx, b.AgentID); err != nil {
				return nil, err
			}
			queues[agentKey] = q
		}
		view := TaskInstanceBatch{
			ID:          util.UUIDToString(b.ID),
			IssueID:     util.UUIDToString(b.IssueID),
			Agent:       InstanceRef{ID: agentKey, Name: q.agent.Name},
			Total:       len(b.Splits),
			RequestedBy: InstanceRequester{Type: b.RequestedByType, ID: util.UUIDToString(b.RequestedByID), Name: s.requesterName(ctx, names, b.RequestedByType, b.RequestedByID)},
			CreatedAt:   b.CreatedAt.Time,
			SettledAt:   tsPtr(b.SettledAt),
			Instances:   make([]TaskInstance, 0, len(b.Splits)),
		}
		for i, split := range b.Splits {
			tries := attempts[view.ID][int16(i+1)]
			inst := TaskInstance{Index: i + 1, Split: split, Status: "queued", TaskIDs: []string{}}
			for _, t := range tries {
				inst.TaskIDs = append(inst.TaskIDs, util.UUIDToString(t.ID))
				if t.Priority >= InstancePriorityBoost {
					view.Priority = true
				}
			}
			if n := len(tries); n > 0 {
				last := tries[n-1]
				inst.Status = InstanceStatus(last.Status)
				inst.TaskID = util.UUIDToString(last.ID)
				inst.SessionID = textPtr(last.SessionID)
				inst.WorkDir = textPtr(last.WorkDir)
				inst.BranchName = textPtr(last.BranchName)
				inst.StartedAt = tsPtr(last.StartedAt)
				inst.CompletedAt = tsPtr(last.CompletedAt)
				if inst.Status == "queued" {
					inst.Queue = q.entry(inst.TaskID)
				}
			}
			view.Instances = append(view.Instances, inst)
		}
		out = append(out, view)
	}
	return out, nil
}

// agentQueue is one agent's queue state, read once per request.
type agentQueue struct {
	agent    db.Agent
	position map[string]int // queued task id → 1-based claim position
	queued   int
	reason   string
	running  *int64
	limit    *int64
}

func (q *agentQueue) entry(taskID string) *InstanceQueue {
	pos, ok := q.position[taskID]
	if !ok {
		// ponytail: a deferred retry is not in the claim order yet; show it
		// behind the queued rows rather than computing its promotion time.
		pos = q.queued + 1
	}
	return &InstanceQueue{Position: pos, Reason: q.reason, Running: q.running, Limit: q.limit}
}

func (s *TaskService) loadAgentQueue(ctx context.Context, agentID pgtype.UUID) (*agentQueue, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("load agent: %w", err)
	}
	q := &agentQueue{agent: agent, position: map[string]int{}, reason: QueueReasonWaitingDispatch}
	ids, err := s.Queries.ListAgentQueuedTaskIDsInClaimOrder(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list queue: %w", err)
	}
	for i, id := range ids {
		q.position[util.UUIDToString(id)] = i + 1
	}
	q.queued = len(ids)

	if !agent.RuntimeID.Valid {
		q.reason = QueueReasonRuntimeOffline
		return q, nil
	}
	rt, err := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return nil, fmt.Errorf("load runtime: %w", err)
	}
	seen := rt.LastSeenAt
	if !seen.Valid {
		seen = rt.UpdatedAt
	}
	if rt.Status != "online" || !seen.Valid || time.Since(seen.Time) > RuntimeClaimFreshnessSeconds*time.Second {
		q.reason = QueueReasonRuntimeOffline
		return q, nil
	}
	running, err := s.Queries.CountRunningTasks(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("count running: %w", err)
	}
	if limit := int64(agent.MaxConcurrentTasks); running >= limit {
		q.reason, q.running, q.limit = QueueReasonRoleFull, &running, &limit
		return q, nil
	}
	if rt.MaxConcurrentTasks.Valid {
		active, err := s.Queries.CountActiveTasksByRuntime(ctx, rt.ID)
		if err != nil {
			return nil, fmt.Errorf("count runtime tasks: %w", err)
		}
		if limit := int64(rt.MaxConcurrentTasks.Int32); active >= limit {
			q.reason, q.running, q.limit = QueueReasonRuntimeFull, &active, &limit
		}
	}
	return q, nil
}

func (s *TaskService) requesterName(ctx context.Context, cache map[string]string, typ string, id pgtype.UUID) string {
	key := typ + ":" + util.UUIDToString(id)
	if name, ok := cache[key]; ok {
		return name
	}
	name := ""
	if typ == "agent" {
		if a, err := s.Queries.GetAgent(ctx, id); err == nil {
			name = a.Name
		}
	} else if u, err := s.Queries.GetUser(ctx, id); err == nil {
		name = u.Name
	}
	cache[key] = name
	return name
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
