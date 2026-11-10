-- Fork, D: same pending-slot guard as 452, except instance rows (batch_id set).
-- k instances of one agent on one issue are queued at once and must not
-- collide with each other or with the main instance's pending row. Built
-- before 9004 retires the old index, as 452/453 did.
CREATE UNIQUE INDEX CONCURRENTLY idx_one_pending_task_per_issue_agent_thread_fork
    ON agent_task_queue (issue_id, agent_id, COALESCE(comment_thread_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE (status IN ('queued', 'dispatched')
       OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true'))
      AND batch_id IS NULL;
