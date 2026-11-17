-- Fork, A rules 4–6: settlement finds the comments a run's delivery held.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_fork_deferred_comment_trigger_pending ON fork_deferred_comment_trigger (source_task_id) WHERE settled_at IS NULL;
