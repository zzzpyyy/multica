-- Fork (zzzpyyy/multica), D: role instances. Upstream queries the fork changes
-- stay in agent.sql; queries only the fork needs live here.

-- name: HasQueuedTaskForAgentOnRuntime :one
-- After a claim: is there more queued work for this agent? ClaimTasksForRuntimes
-- claims at most one task per agent per poll, so the k instances of a batch
-- would otherwise wait for the daemon's next poll (D1).
SELECT EXISTS (
    SELECT 1 FROM agent_task_queue
    WHERE agent_id = $1 AND runtime_id = $2 AND status = 'queued'
) AS has_queued;

-- name: CreateAgentTaskBatch :one
INSERT INTO agent_task_batch (workspace_id, issue_id, agent_id, splits, requested_by_type, requested_by_id)
VALUES (@workspace_id, @issue_id, @agent_id, @splits::text[], @requested_by_type, @requested_by_id)
RETURNING *;

-- name: CreateInstanceTask :one
-- One instance of a batch. Always a fresh session (D2); the workdir is per
-- task id, so instances never share one. Outside the pending-slot index
-- (batch_id IS NOT NULL), so k rows coexist with the main run's pending row.
-- Fenced against workspace teardown like CreateAgentTask.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, force_fresh_session,
    originator_user_id, accountable_user_id, originator_source, delegated_from_task_id,
    rule_version_id, trigger_evidence_kind, trigger_evidence_ref_id,
    runtime_mcp_overlay, runtime_connected_apps,
    workflow_step, batch_id, batch_index, id
)
SELECT
    @agent_id, @runtime_id, @issue_id, 'queued', @priority, TRUE,
    sqlc.narg(originator_user_id), sqlc.narg(accountable_user_id), sqlc.narg(originator_source),
    sqlc.narg(delegated_from_task_id), sqlc.narg(rule_version_id),
    sqlc.narg(trigger_evidence_kind), sqlc.narg(trigger_evidence_ref_id),
    sqlc.narg(runtime_mcp_overlay), sqlc.narg(runtime_connected_apps),
    (SELECT i.status FROM issue i WHERE i.id = @issue_id),
    @batch_id, @batch_index, @id
WHERE lock_task_owner_rows(@agent_id, @issue_id, @runtime_id)
RETURNING *;

-- name: ListIssueTaskBatches :many
SELECT * FROM agent_task_batch
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at DESC, id DESC;

-- name: ListBatchTasks :many
-- Every attempt of every instance of the given batches, oldest first.
SELECT * FROM agent_task_queue
WHERE batch_id = ANY(@batch_ids::uuid[])
ORDER BY batch_id, batch_index, created_at ASC, id ASC;

-- name: ListAgentQueuedTaskIDsInClaimOrder :many
-- The agent's queued rows in ClaimAgentTask's order (keep the ORDER BY in
-- sync), for "position p in the queue" (D5, D16).
SELECT atq.id FROM agent_task_queue atq
WHERE atq.agent_id = $1 AND atq.status = 'queued'
ORDER BY (atq.priority >= 10) DESC,
         EXISTS (
             SELECT 1 FROM agent_task_queue busy
             WHERE busy.agent_id = atq.agent_id
               AND busy.issue_id = atq.issue_id
               AND busy.status IN ('dispatched', 'running', 'waiting_local_directory')
         ) ASC,
         atq.priority DESC, atq.created_at ASC, atq.id ASC;

-- name: GetAgentTaskBatch :one
SELECT * FROM agent_task_batch WHERE id = $1;
