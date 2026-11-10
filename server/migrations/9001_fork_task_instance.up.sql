-- Fork (zzzpyyy/multica), D: role instances. One row per add-instances
-- request ("batch"); each instance is an agent_task_queue row carrying
-- batch_id and batch_index (1-based; splits[batch_index] is its split).
--
-- settled_at is set exactly once, when every instance's latest attempt is
-- terminal and the merge party has been woken.
--
-- No foreign keys (repo rule): DeleteIssue and workspace teardown delete the
-- batches; agent_task_queue.batch_id is a plain reference. Numbered from 9001
-- so upstream's own sequence never collides with fork migrations.
CREATE TABLE agent_task_batch (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    issue_id          UUID NOT NULL,
    agent_id          UUID NOT NULL,
    splits            TEXT[] NOT NULL CHECK (cardinality(splits) >= 1),
    requested_by_type TEXT NOT NULL CHECK (requested_by_type IN ('member', 'agent')),
    requested_by_id   UUID NOT NULL,
    settled_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Nullable without a default: a metadata-only change on the queue table.
ALTER TABLE agent_task_queue
    ADD COLUMN batch_id UUID,
    ADD COLUMN batch_index SMALLINT;
