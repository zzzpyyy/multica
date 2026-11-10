package main

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

// Fork (D): 9001–9004 apply, roll back to 452's shape, and re-apply.
func TestForkInstanceMigrationsUpDownUpInIsolatedSchema(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "fork_instance_")
	pool := openTestPoolWithSearchPath(t, schema)
	if _, err := pool.Exec(ctx, `
		CREATE TABLE agent_task_queue (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			issue_id UUID, agent_id UUID, comment_thread_id UUID,
			status TEXT NOT NULL, context JSONB
		)`); err != nil {
		t.Fatal(err)
	}
	// 452's index, as the fork migrations find it.
	for _, version := range []string{"452_agent_task_pending_thread_unique"} {
		if err := runMigrations(ctx, pool, runOptions{
			Direction:             "up",
			Files:                 realMigrationFiles(t, []string{version}, "up"),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection("up"),
		}); err != nil {
			t.Fatalf("seed 452: %v", err)
		}
	}

	versions := []string{
		"9001_fork_task_instance",
		"9002_fork_task_batch_index",
		"9003_fork_pending_thread_unique",
		"9004_fork_drop_pending_thread_unique",
	}
	validIndex := func(name string) bool {
		t.Helper()
		var ok bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_index i
				JOIN pg_class c ON c.oid = i.indexrelid
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = $1 AND c.relname = $2 AND i.indisvalid)`, schema, name).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	for _, direction := range []string{"up", "down", "up"} {
		vs := slices.Clone(versions)
		if direction == "down" {
			slices.Reverse(vs)
		}
		if err := runMigrations(ctx, pool, runOptions{
			Direction:             direction,
			Files:                 realMigrationFiles(t, vs, direction),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection(direction),
			Conditions:            conditionsForDirection(direction),
		}); err != nil {
			t.Fatalf("migrate %s: %v", direction, err)
		}
		up := direction == "up"
		var batchTable, batchColumn bool
		if err := pool.QueryRow(ctx, `
			SELECT to_regclass($1) IS NOT NULL,
			       EXISTS (SELECT 1 FROM information_schema.columns
			               WHERE table_schema = $2 AND table_name = 'agent_task_queue' AND column_name = 'batch_id')`,
			schema+".agent_task_batch", schema).Scan(&batchTable, &batchColumn); err != nil {
			t.Fatal(err)
		}
		if batchTable != up || batchColumn != up {
			t.Fatalf("after %s: agent_task_batch=%v batch_id=%v, want %v", direction, batchTable, batchColumn, up)
		}
		if got := validIndex("idx_one_pending_task_per_issue_agent_thread_fork"); got != up {
			t.Fatalf("after %s: fork pending index valid=%v, want %v", direction, got, up)
		}
		if got := validIndex("idx_one_pending_task_per_issue_agent_thread"); got == up {
			t.Fatalf("after %s: 452 pending index valid=%v, want %v", direction, got, !up)
		}
		if got := validIndex("idx_agent_task_queue_batch"); got != up {
			t.Fatalf("after %s: batch index valid=%v, want %v", direction, got, up)
		}
	}
}
