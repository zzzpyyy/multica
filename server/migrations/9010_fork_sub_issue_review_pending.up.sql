-- Fork, A rules 4–6: settlement scans deliveries not handled yet.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_fork_sub_issue_review_pending ON fork_sub_issue_review (delivery_task_id) WHERE settled_at IS NULL;
