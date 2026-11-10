-- Fork (zzzpyyy/multica), B: super-agent state (plan 3.5). No foreign keys
-- (repo rule): workspace teardown deletes these rows through their chat
-- session, and the super-agent's session cannot be archived or deleted. Unique indexes are built
-- concurrently in 9006-9008.
--
-- super_agent_action: one per assistant message that carried a super-action
-- block. Cards (create_issues, add_instances) start 'pending'; forward and
-- set_status are executed at once and end 'done' or 'failed'.
CREATE TABLE super_agent_action (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id UUID NOT NULL,
    chat_message_id UUID NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('create_issues', 'add_instances', 'forward', 'set_status')),
    payload         JSONB NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pending', 'creating', 'created', 'failed', 'cancelled', 'expired', 'done')),
    result          JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- super_agent_push: a pushed chat message and the event it reported, so an
-- event is pushed once per session (F8).
CREATE TABLE super_agent_push (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id UUID NOT NULL,
    chat_message_id UUID NOT NULL,
    dedupe_key      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- super_agent_question: a comment that @-mentioned the member (from an agent:
-- answered on their behalf or asked; from a member: asked).
CREATE TABLE super_agent_question (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id UUID NOT NULL,
    comment_id      UUID NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('answering', 'answered', 'asked', 'forwarded')),
    task_id         UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The super-agent (agent.system_key = 'super') may
-- only run chat turns (chat_session_id set, including their retries) and
-- answer-on-your-behalf turns (context.type = 'super_answer'). Every enqueue
-- path inserts into this table, so the guard lives here; handlers refuse the
-- same targets earlier with super_agent_not_assignable. A refused insert
-- leaves no row, so no task:failed event and no failure push.
--
-- SQLSTATE MF001: IsSuperAgentRunRefused. Inside a larger transaction the
-- error aborts it; callers must not swallow it there.
CREATE FUNCTION fork_super_agent_run_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.chat_session_id IS NULL
       AND COALESCE(NEW.context->>'type', '') <> 'super_answer'
       AND EXISTS (SELECT 1 FROM agent WHERE id = NEW.agent_id AND system_key = 'super') THEN
        RAISE EXCEPTION USING ERRCODE = 'MF001', MESSAGE = 'super_agent_run_refused';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER fork_super_agent_run_guard
BEFORE INSERT ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION fork_super_agent_run_guard();
