package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// Fork (B, G4, G5): migration 9005's trigger lets the super-agent run only
// chat turns and super_answer turns, whatever path inserts the row.
func TestSuperAgentRunGuard(t *testing.T) {
	f := newInstanceFixture(t, 3)
	ctx := context.Background()
	f.fx.Exec(t, `UPDATE agent SET system_key = 'super' WHERE id = $1`, f.agentID)
	x := f.issue("X")
	session := f.fx.ChatSession(t, f.agentID)

	insert := func(cols string, args ...any) error {
		_, err := f.pool.Exec(ctx, `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority`+cols, args...)
		return err
	}
	if err := insert(`, chat_session_id) VALUES ($1, $2, 'queued', 0, $3)`, f.agentID, f.runtimeID, session); err != nil {
		t.Fatalf("chat turn refused: %v", err)
	}
	if err := insert(`, issue_id, context) VALUES ($1, $2, 'queued', 0, $3, '{"type":"super_answer"}')`, f.agentID, f.runtimeID, x); err != nil {
		t.Fatalf("super_answer turn refused: %v", err)
	}
	f.fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE agent_id = $1`, f.agentID)
	err := insert(`, issue_id) VALUES ($1, $2, 'queued', 0, $3)`, f.agentID, f.runtimeID, x)
	if !IsSuperAgentRunRefused(err) {
		t.Fatalf("issue run for the super-agent: err = %v, want MF001", err)
	}

	// Other agents are untouched.
	other := f.fx.Agent(t, "other", f.runtimeID)
	if _, err := f.pool.Exec(ctx, `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id) VALUES ($1, $2, 'queued', 0, $3)`, other, f.runtimeID, x); err != nil {
		t.Fatalf("ordinary agent refused: %v", err)
	}
	f.fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE agent_id = $1`, other)

	// Inside a transaction the refusal aborts the whole transaction: nothing
	// written before it survives (一鸣, BUNN-28 08:25).
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE issue SET title = 'changed in tx' WHERE id = $1`, x); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id) VALUES ($1, $2, 'queued', 0, $3)`, f.agentID, f.runtimeID, x)
	if !IsSuperAgentRunRefused(err) {
		t.Fatalf("in-tx insert: err = %v, want MF001", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("commit after a refused insert succeeded")
	}
	var title string
	f.fx.QueryRow(t, `SELECT title FROM issue WHERE id = $1`, x).Scan(&title)
	if title == "changed in tx" {
		t.Fatal("half of the refused transaction was kept")
	}
	if n := f.fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND issue_id = $2 AND COALESCE(context->>'type','') <> 'super_answer'`, f.agentID, x); n != 0 {
		t.Fatalf("refused super-agent issue runs present: %d", n)
	}
}

// A run woken by the super-agent's answer that then fails finally must not
// hand coordination back to the super-agent (BUNN-28 v2.2).
func TestDelegatedFailureNeverRecoversToSuperAgent(t *testing.T) {
	f := newInstanceFixture(t, 3)
	ctx := context.Background()
	x := f.issue("X")
	other := f.fx.Agent(t, "worker", f.runtimeID)
	source := f.task(x, "completed", 120, testutil.Cols{"context": testutil.Raw(`'{"type":"super_answer"}'::jsonb`)})
	failed := f.fx.Task(t, other, testutil.Cols{"runtime_id": f.runtimeID, "issue_id": x, "status": "failed",
		"delegated_from_task_id": source, "completed_at": testutil.Raw("now()")})
	load := func() *delegatedFailureRecoveryTarget {
		task, err := f.svc.Queries.GetAgentTask(ctx, util.MustParseUUID(failed))
		if err != nil {
			t.Fatal(err)
		}
		target, err := loadDelegatedFailureRecoveryTarget(ctx, f.svc.Queries, task)
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	if load() == nil {
		t.Fatal("control: an ordinary source agent is not offered recovery")
	}
	f.fx.Exec(t, `UPDATE agent SET system_key = 'super' WHERE id = $1`, f.agentID)
	if load() != nil {
		t.Fatal("recovery target is the super-agent")
	}
}
