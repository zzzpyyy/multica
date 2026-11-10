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
