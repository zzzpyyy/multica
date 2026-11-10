ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS batch_index,
    DROP COLUMN IF EXISTS batch_id;
DROP TABLE IF EXISTS agent_task_batch;
