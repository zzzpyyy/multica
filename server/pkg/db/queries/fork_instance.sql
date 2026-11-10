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
