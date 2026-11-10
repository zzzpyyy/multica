package service

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (zzzpyyy/multica), B: the built-in super-agent. It talks to the user,
// proposes plans and answers on their behalf; it never runs tools. The server
// executes every action it proposes (plan 1.1, 2.4).

// SuperAgentSystemKey marks the super-agent (agent.system_key).
const SuperAgentSystemKey = "super"

// SuperAgentDefaultName is the name it is provisioned with.
const SuperAgentDefaultName = "super-agent"

// SuperAnswerContextType marks an answer-on-your-behalf run
// (agent_task_queue.context.type), the only issue run it may have.
const SuperAnswerContextType = "super_answer"

//go:embed builtin_agents/super/INSTRUCTIONS.md
var superAgentInstructions string

// SuperAgentInstructions is the product-owned prompt; the agent row's own
// instructions are not used.
func SuperAgentInstructions() string {
	return strings.TrimRight(superAgentInstructions, "\n")
}

func IsSuperAgent(a db.Agent) bool {
	return a.SystemKey.Valid && a.SystemKey.String == SuperAgentSystemKey
}

// IsSuperAgentRunRefused reports the fork_super_agent_run_guard trigger
// (migration 9005) refusing a super-agent run. Outside a larger transaction
// it means "not enqueued"; inside one the transaction is already aborted.
func IsSuperAgentRunRefused(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "MF001"
}

// ErrSuperAgentNotAssignable is returned by paths that would give the
// super-agent an ordinary run (assignment, squad membership, wakeups, reruns).
var ErrSuperAgentNotAssignable = errors.New("super_agent_not_assignable")

func (s *TaskService) isSuperAgentTask(ctx context.Context, task db.AgentTaskQueue) bool {
	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	return err == nil && IsSuperAgent(agent)
}
