package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (zzzpyyy/multica), D: role instances ("add-instances").

// InstancePriorityBoost is the queue priority of an instance the user asked to
// prioritise. Upstream priorities are 0–4 (chat retries use up to 3), and
// ClaimAgentTask orders priority >= InstancePriorityBoost before everything
// else for the agent (D7, D16).
const InstancePriorityBoost = 10

// SuperAgentSystemKey marks the built-in super-agent (agent.system_key). It
// never runs instances (rule 7).
const SuperAgentSystemKey = "super"

// InstanceError is a rejected add-instances request. Status and Code are the
// HTTP contract (plan 3.1); the CLI and plan cards show Msg.
type InstanceError struct {
	Status int
	Code   string
	Msg    string
}

func (e *InstanceError) Error() string { return e.Msg }

func instanceErr(status int, code, format string, args ...any) *InstanceError {
	return &InstanceError{Status: status, Code: code, Msg: fmt.Sprintf(format, args...)}
}

// InstanceRequest asks for len(Splits) instances of Agent on Issue. Exactly
// one requester is set: MemberID (a user, CLI with a personal token or a
// confirmed plan card) or CallerTask (an agent's run, CLI with a task token).
type InstanceRequest struct {
	Issue      db.Issue
	Agent      db.Agent
	Splits     []string
	Priority   bool
	MemberID   pgtype.UUID
	CallerTask *db.AgentTaskQueue
	// CanInvoke is the handler's invoke-permission check for Agent.
	CanInvoke func(db.Agent) bool
}

// CheckInstanceAdmission rejects targets whose instances could not run in
// isolation (P2) or must not run at all (super-agent).
func (s *TaskService) CheckInstanceAdmission(ctx context.Context, issue db.Issue, agent db.Agent) error {
	if agent.SystemKey.Valid && agent.SystemKey.String == SuperAgentSystemKey {
		return instanceErr(http.StatusBadRequest, "super_agent_not_assignable", "super-agent cannot run instances")
	}
	if agent.ArchivedAt.Valid {
		return instanceErr(http.StatusBadRequest, "", "agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return instanceErr(http.StatusBadRequest, "runtime_unsupported", "agent has no runtime")
	}
	rt, err := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return fmt.Errorf("load runtime: %w", err)
	}
	if rt.Provider != "claude" {
		return instanceErr(http.StatusBadRequest, "runtime_unsupported",
			"runtime is not Claude: %s runs on %q, where instances of one issue would queue one after another", agent.Name, rt.Provider)
	}
	if issue.ProjectID.Valid && rt.DaemonID.Valid {
		resources, err := s.Queries.ListProjectResourcesInWorkspace(ctx, db.ListProjectResourcesInWorkspaceParams{
			ProjectID: issue.ProjectID, WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return fmt.Errorf("load project resources: %w", err)
		}
		for _, res := range resources {
			if res.ResourceType != "local_directory" {
				continue
			}
			var ref struct {
				LocalPath     string `json:"local_path"`
				DaemonID      string `json:"daemon_id"`
				ExecutionMode string `json:"execution_mode"`
			}
			if json.Unmarshal(res.ResourceRef, &ref) != nil || ref.DaemonID != rt.DaemonID.String {
				continue
			}
			if ref.ExecutionMode != "worktree" {
				return instanceErr(http.StatusBadRequest, "runtime_unsupported",
					"project uses a local directory without worktree mode: instances would share %q and queue on it", ref.LocalPath)
			}
		}
	}
	return nil
}

// CreateInstances validates and enqueues one batch (plan 2.3, 3.1).
func (s *TaskService) CreateInstances(ctx context.Context, req InstanceRequest) (db.AgentTaskBatch, []db.AgentTaskQueue, error) {
	agent, issue := req.Agent, req.Issue
	if agent.SystemKey.Valid && agent.SystemKey.String == SuperAgentSystemKey {
		return db.AgentTaskBatch{}, nil, instanceErr(http.StatusBadRequest, "super_agent_not_assignable", "super-agent cannot run instances")
	}
	n := int(agent.MaxConcurrentTasks)
	if k := len(req.Splits); k < 1 || k > n {
		return db.AgentTaskBatch{}, nil, instanceErr(http.StatusBadRequest, "instance_count_out_of_range",
			"instance count %d is out of range: %s allows 1 to %d (max_concurrent_tasks)", k, agent.Name, n)
	}
	splits := make([]string, len(req.Splits))
	for i, sp := range req.Splits {
		splits[i] = strings.TrimSpace(sp)
		if splits[i] == "" {
			return db.AgentTaskBatch{}, nil, instanceErr(http.StatusBadRequest, "invalid_split", "split %d is empty; every instance needs a split", i+1)
		}
	}

	var attr attribution.Result
	requestedByType, requestedByID := "member", req.MemberID
	if req.CallerTask != nil {
		if req.Priority {
			return db.AgentTaskBatch{}, nil, instanceErr(http.StatusForbidden, "priority_requires_member", "only a request from a user can set priority")
		}
		owner, err := s.isIssueOwnerAgent(ctx, issue, req.CallerTask.AgentID)
		if err != nil {
			return db.AgentTaskBatch{}, nil, err
		}
		if !owner {
			return db.AgentTaskBatch{}, nil, instanceErr(http.StatusForbidden, "not_issue_owner", "only the issue's assignee agent or its squad leader can add instances")
		}
		requestedByType, requestedByID = "agent", req.CallerTask.AgentID
		attr = attribution.ClassifyComment(attribution.CommentFacts{
			AuthorType:        "agent",
			SourceTaskID:      req.CallerTask.ID,
			ParentOriginator:  req.CallerTask.OriginatorUserID,
			ParentAccountable: req.CallerTask.AccountableUserID,
		}, attribution.SourceDelegation)
		attr.EvidenceKind, attr.EvidenceRefID = attribution.EvidenceIssueAssignment, issue.ID
	} else {
		attr = attribution.ClassifyDirect(attribution.DirectFacts{IssueID: issue.ID, ActorUserID: req.MemberID})
	}
	if req.CanInvoke != nil && !req.CanInvoke(agent) {
		return db.AgentTaskBatch{}, nil, instanceErr(http.StatusForbidden, "", "you do not have permission to assign work to this agent")
	}
	if err := s.CheckInstanceAdmission(ctx, issue, agent); err != nil {
		return db.AgentTaskBatch{}, nil, err
	}
	attr, err := s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		return db.AgentTaskBatch{}, nil, instanceErr(http.StatusForbidden, "", "this workspace refuses runs without an accountable member")
	}

	priority := priorityToInt(issue.Priority)
	if req.Priority {
		priority = InstancePriorityBoost
	}
	overlay := s.buildRuntimeMCPOverlay(ctx, attr.UserID, agent)
	source, delegatedFrom, evidenceKind, evidenceRef := attributionCreateParams(attr)

	var batch db.AgentTaskBatch
	tasks := make([]db.AgentTaskQueue, 0, len(splits))
	err = s.runInTx(ctx, func(q *db.Queries) error {
		var err error
		batch, err = q.CreateAgentTaskBatch(ctx, db.CreateAgentTaskBatchParams{
			WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AgentID: agent.ID,
			Splits: splits, RequestedByType: requestedByType, RequestedByID: requestedByID,
		})
		if err != nil {
			return fmt.Errorf("create batch: %w", err)
		}
		for i := range splits {
			task, err := q.CreateInstanceTask(ctx, db.CreateInstanceTaskParams{
				AgentID: agent.ID, RuntimeID: agent.RuntimeID, IssueID: issue.ID, Priority: priority,
				OriginatorUserID: attr.UserID, AccountableUserID: attr.AccountableUserID,
				OriginatorSource: source, DelegatedFromTaskID: delegatedFrom, RuleVersionID: attr.RuleVersionID,
				TriggerEvidenceKind: evidenceKind, TriggerEvidenceRefID: evidenceRef,
				RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps,
				BatchID: batch.ID, BatchIndex: pgtype.Int2{Int16: int16(i + 1), Valid: true},
				ID: dbid.NewV7(),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return instanceErr(http.StatusNotFound, "", "issue or agent no longer exists")
			}
			if err != nil {
				return fmt.Errorf("create instance %d: %w", i+1, err)
			}
			tasks = append(tasks, task)
		}
		return nil
	})
	if err != nil {
		return db.AgentTaskBatch{}, nil, err
	}
	slog.Info("instances enqueued", "batch_id", util.UUIDToString(batch.ID), "issue_id", util.UUIDToString(issue.ID),
		"agent_id", util.UUIDToString(agent.ID), "count", len(tasks), "priority", req.Priority, "requested_by", requestedByType)
	s.RecordHumanDelegation(ctx, attr)
	for _, t := range tasks {
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, t)
		s.NotifyTaskEnqueued(ctx, t)
	}
	return batch, tasks, nil
}

// isIssueOwnerAgent: the agent is the issue's assignee, or leads the squad
// that is (D14).
func (s *TaskService) isIssueOwnerAgent(ctx context.Context, issue db.Issue, agentID pgtype.UUID) (bool, error) {
	if !issue.AssigneeID.Valid {
		return false, nil
	}
	switch issue.AssigneeType.String {
	case "agent":
		return issue.AssigneeID == agentID, nil
	case "squad":
		squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("load squad: %w", err)
		}
		return squad.LeaderID == agentID, nil
	}
	return false, nil
}

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
