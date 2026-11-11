-- Fork (zzzpyyy/multica), B: super-agent state (migration 9005).

-- name: ListPendingSuperAgentActions :many
SELECT * FROM super_agent_action
WHERE chat_session_id = $1 AND status = 'pending'
ORDER BY created_at ASC;

-- name: ListSuperAgentQuestionsByStatus :many
SELECT q.id, q.comment_id, q.status, q.created_at, c.issue_id, c.content, i.number AS issue_number
FROM super_agent_question q
JOIN comment c ON c.id = q.comment_id
JOIN issue i ON i.id = c.issue_id
WHERE q.chat_session_id = $1 AND q.status = $2 AND c.deleted_at IS NULL
ORDER BY q.created_at ASC;

-- name: ListRecentSuperAgentPushes :many
SELECT m.content, m.created_at
FROM super_agent_push p
JOIN chat_message m ON m.id = p.chat_message_id
WHERE p.chat_session_id = $1
ORDER BY p.created_at DESC
LIMIT 20;

-- name: GetSuperAgentSessionForMember :one
-- The member's one super-agent conversation in a workspace (rule 1).
SELECT s.* FROM chat_session s
JOIN agent a ON a.id = s.agent_id
WHERE s.workspace_id = @workspace_id AND s.creator_id = @creator_id AND a.system_key = 'super'
ORDER BY s.created_at ASC
LIMIT 1;

-- name: CreateSuperAgentAction :one
INSERT INTO super_agent_action (chat_session_id, chat_message_id, kind, payload, status, result)
VALUES (@chat_session_id, @chat_message_id, @kind, @payload, @status, sqlc.narg(result))
RETURNING *;

-- name: ExpirePendingSuperAgentCards :exec
-- A new card makes the session's older pending cards stale (B7).
UPDATE super_agent_action SET status = 'expired'
WHERE chat_session_id = @chat_session_id AND status = 'pending'
  AND kind IN ('create_issues', 'add_instances') AND id <> @keep_id;

-- name: ListSuperAgentCards :many
SELECT * FROM super_agent_action
WHERE chat_session_id = $1 AND kind IN ('create_issues', 'add_instances')
ORDER BY created_at ASC, id ASC;

-- name: GetSuperAgentAction :one
SELECT * FROM super_agent_action WHERE id = $1;

-- name: TransitionSuperAgentAction :one
-- Compare-and-set: only the caller that finds `from` wins (B3: a double
-- click confirms once).
UPDATE super_agent_action SET status = @to_status
WHERE id = @id AND status = @from_status
RETURNING *;

-- name: FinishSuperAgentAction :one
UPDATE super_agent_action SET status = @status, result = @result
WHERE id = @id
RETURNING *;

-- name: ListPendingSuperAgentDirectActionsForTask :many
-- forward / set_status written by a finished chat turn, not yet executed.
SELECT a.* FROM super_agent_action a
JOIN chat_message m ON m.id = a.chat_message_id
WHERE m.task_id = @task_id AND a.status = 'pending' AND a.kind IN ('forward', 'set_status');

-- name: MarkSuperAgentQuestionsForwarded :exec
UPDATE super_agent_question q SET status = 'forwarded'
FROM comment c
WHERE q.comment_id = c.id AND q.chat_session_id = @chat_session_id
  AND q.status = 'asked' AND c.issue_id = @issue_id;

-- name: ListSuperAgentSessionsInWorkspace :many
-- One super-agent session per member who has opened it (rule 1).
SELECT DISTINCT ON (s.creator_id) s.*
FROM chat_session s
JOIN agent a ON a.id = s.agent_id
WHERE s.workspace_id = $1 AND a.system_key = 'super'
ORDER BY s.creator_id, s.created_at ASC;

-- name: CreateSuperAgentPush :one
-- No row when this session already got this event (F8).
INSERT INTO super_agent_push (chat_session_id, chat_message_id, dedupe_key)
VALUES (@chat_session_id, @chat_message_id, @dedupe_key)
ON CONFLICT (chat_session_id, dedupe_key) DO NOTHING
RETURNING *;

-- name: ListChildIssueStatuses :many
SELECT id, status, stage FROM issue
WHERE parent_issue_id = $1 AND workspace_id = $2;

-- name: CreateSuperAgentQuestion :one
-- No row when this comment is already handled in the session.
INSERT INTO super_agent_question (chat_session_id, comment_id, status)
VALUES (@chat_session_id, @comment_id, @status)
ON CONFLICT (chat_session_id, comment_id) DO NOTHING
RETURNING *;

-- name: GetSuperAgentQuestion :one
SELECT * FROM super_agent_question WHERE id = $1;

-- name: SetSuperAgentQuestionTask :exec
UPDATE super_agent_question SET task_id = @task_id WHERE id = @id;

-- name: SetSuperAgentQuestionStatus :one
-- Only an answering question moves on, so a late or replayed outcome cannot
-- reopen one that was already answered or asked.
UPDATE super_agent_question SET status = @status
WHERE id = @id AND status = 'answering'
RETURNING *;

-- name: CreateSuperAnswerTask :one
-- The super-agent's answer-on-your-behalf run for one question (rule 17-20).
-- Fresh session; context.type lets fork_super_agent_run_guard admit it. A
-- pending run in the same thread wins: no row, the sweep retries later.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, trigger_comment_id, force_fresh_session, context,
    originator_user_id, accountable_user_id, originator_source, delegated_from_task_id,
    rule_version_id, trigger_evidence_kind, trigger_evidence_ref_id, id
)
SELECT
    @agent_id, @runtime_id, @issue_id, 'queued', 2, @trigger_comment_id, TRUE,
    jsonb_build_object('type', 'super_answer', 'question_id', @question_id::text),
    sqlc.narg(originator_user_id), sqlc.narg(accountable_user_id), sqlc.narg(originator_source),
    sqlc.narg(delegated_from_task_id), sqlc.narg(rule_version_id),
    sqlc.narg(trigger_evidence_kind), sqlc.narg(trigger_evidence_ref_id), @id
WHERE lock_task_owner_rows(@agent_id, @issue_id, @runtime_id)
ON CONFLICT (issue_id, agent_id, (COALESCE(comment_thread_id, '00000000-0000-0000-0000-000000000000'::uuid))) WHERE (status IN ('queued', 'dispatched')
       OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true'))
      AND batch_id IS NULL
DO NOTHING
RETURNING *;

-- name: ListStuckSuperAgentQuestions :many
-- Answering for over a minute with no run in flight: the enqueue lost a
-- race, the event was lost, or the run ended without an outcome. Answering
-- for over 4 minutes is overdue whatever its run is doing (retrying, stuck,
-- never claimed): with the 30 s sweep it is pushed within rule 20's 5.
SELECT q.*,
       (SELECT count(*) FROM agent_task_queue t WHERE t.context->>'question_id' = q.id::text)::int AS attempts,
       (q.created_at < now() - interval '4 minutes')::bool AS overdue
FROM super_agent_question q
WHERE q.status = 'answering'
  AND q.created_at < now() - interval '1 minute'
  AND (q.created_at < now() - interval '4 minutes' OR NOT EXISTS (
      SELECT 1 FROM agent_task_queue t
      WHERE t.context->>'question_id' = q.id::text
        AND t.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  ))
ORDER BY q.created_at ASC
LIMIT 50;

-- name: ListIssueTreeIDs :many
-- Every issue in the tree the given issue belongs to: up to its top-level
-- issue, then down (rule 18).
WITH RECURSIVE up AS (
    SELECT i.id, i.parent_issue_id, 0 AS depth FROM issue i WHERE i.id = @issue_id
    UNION ALL
    SELECT p.id, p.parent_issue_id, up.depth + 1 FROM issue p JOIN up ON p.id = up.parent_issue_id
    WHERE up.depth < 32
), root AS (
    SELECT id FROM up ORDER BY depth DESC LIMIT 1
), down AS (
    SELECT i.id, 0 AS depth FROM issue i WHERE i.id = (SELECT id FROM root)
    UNION ALL
    SELECT c.id, down.depth + 1 FROM issue c JOIN down ON c.parent_issue_id = down.id
    WHERE down.depth < 32
)
SELECT DISTINCT id FROM down;

-- name: ListMemberCommentsInIssues :many
SELECT id, issue_id, content, created_at FROM comment
WHERE author_type = 'member' AND author_id = @author_id
  AND issue_id = ANY(@issue_ids::uuid[]) AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 200;

-- name: ListUserMessagesInSession :many
SELECT id, content, created_at FROM chat_message
WHERE chat_session_id = $1 AND role = 'user'
ORDER BY created_at DESC
LIMIT 500;

-- name: ListSuperAgentMessageIssueLinks :many
-- Which issue each forwarded or task-creating action tied the user's messages
-- to (rule 18: a message belongs to the issue it was forwarded to or written
-- into).
SELECT kind, payload, result FROM super_agent_action
WHERE chat_session_id = $1
  AND ((kind = 'forward' AND status = 'done') OR (kind = 'create_issues' AND status IN ('created', 'failed')));
