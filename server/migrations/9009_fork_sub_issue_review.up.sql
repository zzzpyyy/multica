-- Fork (zzzpyyy/multica), A rules 4–6 (plan v2.4-A2-r4 §2.1, §2.3, §2.4):
-- a sub-issue is reviewed by its parent's owner after the run that delivered
-- it ends. No foreign keys (repo rule): issue and workspace deletion remove
-- these rows in the application deletion graph.

-- One row per sub-issue that has been in review. review_round counts entries
-- into in_review since creation or the last done/cancelled/blocked.
-- delivery_task_id is the first attempt of the retry chain of the run that
-- put it in review (NULL: a person did). settled_at is set once that delivery
-- was handled (a review input recorded, or nothing to do).
CREATE TABLE IF NOT EXISTS fork_sub_issue_review (
 issue_id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL,
 review_round int NOT NULL DEFAULT 0,
 delivery_task_id uuid,
 entered_at timestamptz,
 settled_at timestamptz
);

-- A comment trigger held while the delivering run is still going: the
-- comment came from run source_task_id on child_issue_id or its parent and
-- would wake agent_id, the child's reviewer. Settlement either merges it into
-- the review input ('merged', settled_at set) or releases it ('released',
-- settled_at NULL until the comment was routed again).
CREATE TABLE IF NOT EXISTS fork_deferred_comment_trigger (
 comment_id uuid NOT NULL,
 agent_id uuid NOT NULL,
 source_task_id uuid NOT NULL,
 originator_user_id uuid,
 workspace_id uuid NOT NULL,
 child_issue_id uuid NOT NULL,
 outcome text CHECK (outcome IN ('merged','released')),
 settled_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (comment_id, agent_id)
);

-- The first attempt of a task's retry chain (parent_task_id), at most 10
-- levels up. A run and its automatic retries are one delivery.
CREATE OR REPLACE FUNCTION fork_task_chain_root(task uuid) RETURNS uuid LANGUAGE plpgsql STABLE AS $$
DECLARE root uuid := task; up uuid; depth int := 0;
BEGIN
 WHILE root IS NOT NULL AND depth < 10 LOOP
  SELECT parent_task_id INTO up FROM agent_task_queue WHERE id = root;
  EXIT WHEN up IS NULL;
  root := up;
  depth := depth + 1;
 END LOOP;
 RETURN root;
END $$;

-- Whether a retry chain has ended: every attempt is completed, failed or
-- cancelled. A failed attempt counts once its retry exists or 10 seconds have
-- passed, since an infrastructure retry can be created after the failure.
CREATE OR REPLACE FUNCTION fork_task_chain_ended(root uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 WITH RECURSIVE chain AS (
  SELECT t.id, t.status, t.completed_at, 0 AS depth FROM agent_task_queue t WHERE t.id = root
  UNION ALL
  SELECT t.id, t.status, t.completed_at, chain.depth + 1 FROM agent_task_queue t JOIN chain ON t.parent_task_id = chain.id WHERE chain.depth < 10
 )
 SELECT NOT EXISTS (
  SELECT 1 FROM chain c
  WHERE c.status NOT IN ('completed', 'failed', 'cancelled')
   OR (c.status = 'failed' AND (c.completed_at IS NULL OR c.completed_at > now() - interval '10 seconds')
       AND NOT EXISTS (SELECT 1 FROM agent_task_queue n WHERE n.parent_task_id = c.id)))
$$;

CREATE OR REPLACE FUNCTION fork_record_sub_issue_review() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE root uuid; resets boolean;
BEGIN
 IF NEW.status = 'in_review' AND (TG_OP = 'INSERT' OR OLD.status IS DISTINCT FROM 'in_review') THEN
  root := fork_task_chain_root(NULLIF(current_setting('multica.source_task_id', true), '')::uuid);
  INSERT INTO fork_sub_issue_review AS r (issue_id, workspace_id, review_round, delivery_task_id, entered_at, settled_at)
  VALUES (NEW.id, NEW.workspace_id, 1, root, clock_timestamp(), NULL)
  ON CONFLICT (issue_id) DO UPDATE SET
   review_round = CASE WHEN r.settled_at IS NULL AND r.delivery_task_id IS NOT NULL AND r.delivery_task_id = EXCLUDED.delivery_task_id
                       THEN r.review_round ELSE r.review_round + 1 END,
   entered_at = CASE WHEN r.settled_at IS NULL AND r.delivery_task_id IS NOT NULL AND r.delivery_task_id = EXCLUDED.delivery_task_id
                     THEN r.entered_at ELSE EXCLUDED.entered_at END,
   delivery_task_id = EXCLUDED.delivery_task_id,
   settled_at = NULL,
   workspace_id = EXCLUDED.workspace_id;
  RETURN NULL;
 END IF;
 IF TG_OP = 'UPDATE' AND OLD.status IS NOT DISTINCT FROM NEW.status THEN
  RETURN NULL;
 END IF;
 resets := NEW.status IN ('done', 'cancelled', 'blocked') OR EXISTS (
  SELECT 1 FROM issue_status s WHERE s.workspace_id = NEW.workspace_id AND s.key = NEW.status AND s.category IN ('done', 'closed'));
 IF resets THEN
  UPDATE fork_sub_issue_review SET review_round = 0 WHERE issue_id = NEW.id AND review_round <> 0;
 END IF;
 RETURN NULL;
END $$;

DROP TRIGGER IF EXISTS fork_sub_issue_review_insert ON issue;
CREATE TRIGGER fork_sub_issue_review_insert AFTER INSERT ON issue FOR EACH ROW
 WHEN (NEW.parent_issue_id IS NOT NULL AND NEW.status = 'in_review')
 EXECUTE FUNCTION fork_record_sub_issue_review();

DROP TRIGGER IF EXISTS fork_sub_issue_review_update ON issue;
CREATE TRIGGER fork_sub_issue_review_update AFTER UPDATE OF status ON issue FOR EACH ROW
 WHEN (NEW.parent_issue_id IS NOT NULL AND OLD.status IS DISTINCT FROM NEW.status)
 EXECUTE FUNCTION fork_record_sub_issue_review();

-- The review wakeup is a second platform rule (§2.4).
ALTER TABLE issue_wakeup DROP CONSTRAINT IF EXISTS issue_wakeup_system_rule_check;
ALTER TABLE issue_wakeup ADD CONSTRAINT issue_wakeup_system_rule_check CHECK (system_rule IN ('child_done', 'child_review'));

-- Stock: sub-issues already in review count as their first, settled entry.
-- No wakeup is sent for them (§2.1, Q11).
INSERT INTO fork_sub_issue_review (issue_id, workspace_id, review_round, delivery_task_id, entered_at, settled_at)
SELECT id, workspace_id, 1, NULL, updated_at, clock_timestamp()
FROM issue WHERE parent_issue_id IS NOT NULL AND status = 'in_review'
ON CONFLICT (issue_id) DO NOTHING;
