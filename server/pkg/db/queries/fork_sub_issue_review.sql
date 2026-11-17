-- Fork (zzzpyyy/multica), A rules 4–6 (plan v2.4-A2-r4 §1, §2): who reviews
-- a sub-issue, and settling a delivery once its run has ended.

-- name: GetSubIssueReviewerInput :one
-- Everything rule 4 needs for one issue, for resolveSubIssueReviewer.
SELECT c.id, c.parent_issue_id,
       p.assignee_type AS parent_assignee_type, p.assignee_id AS parent_assignee_id,
       ps.leader_id AS parent_squad_leader_id,
       (ra.id IS NOT NULL AND ra.archived_at IS NULL AND ra.runtime_id IS NOT NULL)::bool AS reviewer_agent_wakeable,
       (pm.id IS NOT NULL)::bool AS parent_member_present,
       c.assignee_type AS child_assignee_type, c.assignee_id AS child_assignee_id,
       cs.leader_id AS child_squad_leader_id
FROM issue c
LEFT JOIN issue p  ON p.id = c.parent_issue_id AND p.workspace_id = c.workspace_id
LEFT JOIN squad ps ON p.assignee_type = 'squad' AND ps.id = p.assignee_id AND ps.workspace_id = c.workspace_id
LEFT JOIN squad cs ON c.assignee_type = 'squad' AND cs.id = c.assignee_id AND cs.workspace_id = c.workspace_id
LEFT JOIN agent ra ON ra.workspace_id = c.workspace_id AND ra.id = CASE p.assignee_type WHEN 'agent' THEN p.assignee_id
                                                                     WHEN 'squad' THEN ps.leader_id END
LEFT JOIN member pm ON p.assignee_type = 'member' AND pm.workspace_id = c.workspace_id AND pm.user_id = p.assignee_id
WHERE c.id = @issue_id;

-- name: ListInReviewReviewerInputs :many
-- The same inputs for every in-review issue of a workspace (attention).
SELECT c.id, c.parent_issue_id,
       p.assignee_type AS parent_assignee_type, p.assignee_id AS parent_assignee_id,
       ps.leader_id AS parent_squad_leader_id,
       (ra.id IS NOT NULL AND ra.archived_at IS NULL AND ra.runtime_id IS NOT NULL)::bool AS reviewer_agent_wakeable,
       (pm.id IS NOT NULL)::bool AS parent_member_present,
       c.assignee_type AS child_assignee_type, c.assignee_id AS child_assignee_id,
       cs.leader_id AS child_squad_leader_id
FROM issue c
LEFT JOIN issue p  ON p.id = c.parent_issue_id AND p.workspace_id = c.workspace_id
LEFT JOIN squad ps ON p.assignee_type = 'squad' AND ps.id = p.assignee_id AND ps.workspace_id = c.workspace_id
LEFT JOIN squad cs ON c.assignee_type = 'squad' AND cs.id = c.assignee_id AND cs.workspace_id = c.workspace_id
LEFT JOIN agent ra ON ra.workspace_id = c.workspace_id AND ra.id = CASE p.assignee_type WHEN 'agent' THEN p.assignee_id
                                                                     WHEN 'squad' THEN ps.leader_id END
LEFT JOIN member pm ON p.assignee_type = 'member' AND pm.workspace_id = c.workspace_id AND pm.user_id = p.assignee_id
WHERE c.workspace_id = @workspace_id AND c.status = 'in_review'
ORDER BY c.created_at, c.id;

-- name: ListSettleableSubIssueReviews :many
-- Deliveries not handled yet whose run (and every retry) has ended; a person's
-- delivery right away.
SELECT issue_id FROM fork_sub_issue_review
WHERE settled_at IS NULL
  AND (delivery_task_id IS NULL OR fork_task_chain_ended(delivery_task_id))
ORDER BY entered_at, issue_id
LIMIT @max_rows;

-- name: LockSubIssueReview :one
SELECT * FROM fork_sub_issue_review WHERE issue_id = @issue_id AND settled_at IS NULL FOR UPDATE SKIP LOCKED;

-- name: GetSubIssueReview :one
SELECT * FROM fork_sub_issue_review WHERE issue_id = @issue_id;

-- name: SettleSubIssueReview :exec
UPDATE fork_sub_issue_review SET settled_at = clock_timestamp() WHERE issue_id = @issue_id;

-- name: LockDeferredCommentsForChild :many
-- Held comments of a sub-issue not decided yet, with the delivery they belong to.
SELECT d.comment_id, d.agent_id, d.source_task_id, fork_task_chain_root(d.source_task_id)::uuid AS delivery_task_id
FROM fork_deferred_comment_trigger d
WHERE d.child_issue_id = @child_issue_id AND d.outcome IS NULL
ORDER BY d.created_at, d.comment_id
FOR UPDATE;

-- name: ListReleasableDeferredComments :many
-- Held comments whose run has ended but that belong to no delivery waiting to
-- be settled: the run did not deliver the sub-issue. They are released.
SELECT d.comment_id, d.agent_id FROM fork_deferred_comment_trigger d
WHERE d.outcome IS NULL
  AND fork_task_chain_ended(fork_task_chain_root(d.source_task_id))
  AND NOT EXISTS (
      SELECT 1 FROM fork_sub_issue_review r
      WHERE r.issue_id = d.child_issue_id AND r.settled_at IS NULL
        AND r.delivery_task_id = fork_task_chain_root(d.source_task_id))
ORDER BY d.created_at, d.comment_id
LIMIT @max_rows;

-- name: MergeDeferredComments :exec
UPDATE fork_deferred_comment_trigger SET outcome = 'merged', settled_at = clock_timestamp()
WHERE comment_id = ANY(@comment_ids::uuid[]) AND agent_id = @agent_id AND outcome IS NULL;

-- name: ReleaseDeferredComment :exec
-- Released rows keep settled_at NULL until the comment was routed again.
UPDATE fork_deferred_comment_trigger SET outcome = 'released'
WHERE comment_id = @comment_id AND agent_id = @agent_id AND outcome IS NULL;
