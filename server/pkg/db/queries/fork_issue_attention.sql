-- Fork (zzzpyyy/multica), A rule 7: what GET /api/issues/attention counts.

-- name: ListInReviewIssueIDs :many
-- "待你验收" before rules 4–6 ship (plan v2.4-A2 SB4a): every issue in review,
-- top-level ones included (their done is left to the member).
SELECT id FROM issue
WHERE workspace_id = @workspace_id
  AND status = 'in_review';

-- name: ListAttentionMentionCandidates :many
-- Comments on open issues that may @ the member: a superset the handler
-- confirms with util.ParseMentions. ILIKE because mention UUIDs are
-- case-insensitive. The member's own and the super-agent's comments never
-- ask for a reply.
-- ponytail: scans the open issues' comments per request; record mentions
-- at write time if a workspace reaches tens of thousands of comments.
SELECT c.id, c.issue_id, c.content, c.created_at
FROM comment c
JOIN issue i ON i.id = c.issue_id
WHERE i.workspace_id = @workspace_id
  AND c.workspace_id = @workspace_id
  AND NOT (i.status = ANY(@terminal_status_keys::text[]))
  AND c.deleted_at IS NULL
  AND c.author_type IN ('member', 'agent')
  AND c.content ILIKE @mention_pattern::text
  AND NOT (c.author_type = 'member' AND c.author_id = @user_id)
  AND NOT (c.author_type = 'agent' AND c.author_id IN (
        SELECT a.id FROM agent a WHERE a.workspace_id = @workspace_id AND a.system_key = 'super'));

-- name: ListAttentionReplyTimes :many
-- The latest comment on each issue that replies for the member and so
-- answers every @ before it (一鸣 11-17): their own, or a super-agent forward
-- of their chat answer (top-level, written by a turn of their own session).
-- An answer on their behalf answers only the @ it sits under: see
-- ListAttentionAnsweredMentions.
SELECT c.issue_id, MAX(c.created_at)::timestamptz AS replied_at
FROM comment c
WHERE c.issue_id = ANY(@issue_ids::uuid[])
  AND c.deleted_at IS NULL
  AND ((c.author_type = 'member' AND c.author_id = @user_id)
    OR (c.author_type = 'agent' AND c.parent_id IS NULL AND c.author_id IN (
          SELECT a.id FROM agent a WHERE a.workspace_id = @workspace_id AND a.system_key = 'super')
        AND EXISTS (
          SELECT 1 FROM agent_task_queue t JOIN chat_session s ON s.id = t.chat_session_id
          WHERE t.id = c.source_task_id AND s.creator_id = @user_id)))
GROUP BY c.issue_id;

-- name: ListAttentionAnsweredMentions :many
-- @ comments the super-agent fully answered for the member: the member's
-- question is answered (a partial answer leaves it asked, rule 19) and the
-- answer sits under it. runSuperAnswer settles the question before it writes
-- the reply, so the reply's comment:created event already sees it answered.
SELECT q.comment_id
FROM super_agent_question q
JOIN chat_session s ON s.id = q.chat_session_id
WHERE q.comment_id = ANY(@comment_ids::uuid[])
  AND q.status = 'answered'
  AND s.creator_id = @user_id AND s.workspace_id = @workspace_id
  AND EXISTS (
      SELECT 1 FROM comment r
      WHERE r.parent_id = q.comment_id AND r.deleted_at IS NULL AND r.author_type = 'agent'
        AND r.author_id = s.agent_id);

-- name: ListIssueAncestors :many
-- Parent chains, nearest first. Capped at 32 levels so a cycle in the data
-- cannot loop.
WITH RECURSIVE chain AS (
    SELECT i.id AS issue_id, p.id AS ancestor_id, p.parent_issue_id, 1 AS depth
    FROM issue i
    JOIN issue p ON p.id = i.parent_issue_id
    WHERE i.workspace_id = @workspace_id
      AND p.workspace_id = @workspace_id
      AND i.id = ANY(@issue_ids::uuid[])
    UNION ALL
    SELECT chain.issue_id, p.id, p.parent_issue_id, chain.depth + 1
    FROM chain
    JOIN issue p ON p.id = chain.parent_issue_id
    WHERE p.workspace_id = @workspace_id AND chain.depth < 32
)
SELECT issue_id::uuid AS issue_id, ancestor_id::uuid AS ancestor_id, depth::int AS depth
FROM chain
ORDER BY issue_id, depth;
