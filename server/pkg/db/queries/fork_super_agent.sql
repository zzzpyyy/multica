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
