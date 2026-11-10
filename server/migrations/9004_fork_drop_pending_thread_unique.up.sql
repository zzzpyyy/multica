-- Fork, D: retire 452's index; 9003 replaces it.
DROP INDEX CONCURRENTLY IF EXISTS idx_one_pending_task_per_issue_agent_thread;
