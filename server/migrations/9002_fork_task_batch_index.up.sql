-- Fork, D: find a batch's instance rows (settlement, GET /issues/{id}/instances).
CREATE INDEX CONCURRENTLY idx_agent_task_queue_batch
    ON agent_task_queue (batch_id)
    WHERE batch_id IS NOT NULL;
