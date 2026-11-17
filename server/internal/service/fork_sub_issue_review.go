package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Fork (zzzpyyy/multica), A rules 4–6 (plan v2.4-A2-r4): a sub-issue is
// reviewed by its parent's owner. Who that is lives only in
// resolveSubIssueReviewer; the review wakeup, the comment hold, the claim's
// owner_is_you and attention all read it.

// Reviewer kinds. yours and self count as the requesting member's review.
const (
	ReviewerYours  = "yours"  // a person: nobody else can review it
	ReviewerMember = "member" // the parent's member assignee (still in the workspace)
	ReviewerAgent  = "agent"  // a wakeable agent that did not deliver it
	ReviewerSelf   = "self"   // the reviewing agent delivered it itself
)

// SubIssueReviewTarget is rule 4's answer for one issue. ID is the member or
// agent for member, agent and self; DelivererID is the child's assignee agent
// (a squad's leader), when it has one.
type SubIssueReviewTarget struct {
	Kind        string
	ID          pgtype.UUID
	DelivererID pgtype.UUID
}

// SubIssueReviewerInput is what rule 4 reads (GetSubIssueReviewerInput,
// ListInReviewReviewerInputs).
type SubIssueReviewerInput struct {
	ParentIssueID         pgtype.UUID
	ParentAssigneeType    string
	ParentAssigneeID      pgtype.UUID
	ParentSquadLeaderID   pgtype.UUID
	ReviewerAgentWakeable bool
	ParentMemberPresent   bool
	ChildAssigneeType     string
	ChildAssigneeID       pgtype.UUID
	ChildSquadLeaderID    pgtype.UUID
}

// ReviewerInputFromRow adapts either query's row.
func ReviewerInputFromRow(parent pgtype.UUID, parentType pgtype.Text, parentID, parentLeader pgtype.UUID, wakeable, present bool, childType pgtype.Text, childID, childLeader pgtype.UUID) SubIssueReviewerInput {
	return SubIssueReviewerInput{
		ParentIssueID: parent, ParentAssigneeType: parentType.String, ParentAssigneeID: parentID,
		ParentSquadLeaderID: parentLeader, ReviewerAgentWakeable: wakeable, ParentMemberPresent: present,
		ChildAssigneeType: childType.String, ChildAssigneeID: childID, ChildSquadLeaderID: childLeader,
	}
}

// ResolveSubIssueReviewer is rule 4 (plan §1). A reviewer is someone else only
// when it is another member still in the workspace, or an agent that can be
// woken (not archived, has a runtime) and did not deliver the sub-issue; a
// squad counts as its leader on both sides. Everything else is the person's.
func ResolveSubIssueReviewer(in SubIssueReviewerInput) SubIssueReviewTarget {
	var out SubIssueReviewTarget
	switch in.ChildAssigneeType {
	case "agent":
		out.DelivererID = in.ChildAssigneeID
	case "squad":
		out.DelivererID = in.ChildSquadLeaderID
	}
	out.Kind = ReviewerYours
	if !in.ParentIssueID.Valid || !in.ParentAssigneeID.Valid {
		return out // top-level, or the parent has no assignee
	}
	switch in.ParentAssigneeType {
	case "member":
		if in.ParentMemberPresent {
			out.Kind, out.ID = ReviewerMember, in.ParentAssigneeID
		}
		return out
	case "agent", "squad":
		reviewer := in.ParentAssigneeID
		if in.ParentAssigneeType == "squad" {
			reviewer = in.ParentSquadLeaderID
		}
		switch {
		case !reviewer.Valid: // a squad whose leader cannot be resolved
		case out.DelivererID.Valid && out.DelivererID == reviewer:
			out.Kind, out.ID = ReviewerSelf, reviewer
		case in.ReviewerAgentWakeable:
			out.Kind, out.ID = ReviewerAgent, reviewer
		}
	}
	return out
}

// SubIssueReviewer resolves rule 4 for one issue as stored now.
func SubIssueReviewer(ctx context.Context, q *db.Queries, child db.Issue) (SubIssueReviewTarget, error) {
	row, err := q.GetSubIssueReviewerInput(ctx, child.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubIssueReviewTarget{Kind: ReviewerYours}, nil
	}
	if err != nil {
		return SubIssueReviewTarget{}, err
	}
	return ResolveSubIssueReviewer(ReviewerInputFromRow(row.ParentIssueID, row.ParentAssigneeType, row.ParentAssigneeID,
		row.ParentSquadLeaderID, row.ReviewerAgentWakeable, row.ParentMemberPresent, row.ChildAssigneeType, row.ChildAssigneeID, row.ChildSquadLeaderID)), nil
}

// SystemRuleChildReview is the review wakeup (plan §2.4): one rule per parent,
// always on, no switch, condition or custom instruction.
const SystemRuleChildReview = "child_review"

// SubIssueReviewEventType is the input a settled delivery records.
const SubIssueReviewEventType = "sub_issue.review_requested"

// SubIssueReviewInstruction is rule 6, the review wakeup's fixed instruction.
const SubIssueReviewInstruction = "Sub-issues of this issue were delivered and wait for your review. Each `sub_issue.review_requested` fact below names one (`sub_issue_id`, `identifier`), says how many times in a row it has come up for review (`review_round`), and carries the comments its delivering run addressed to you. For each one:\n" +
	"0. Read it: `multica issue get <sub_issue_id> --output json`. If it is no longer `in_review`, this delivery already has a result: do nothing for it (no_action).\n" +
	"1. Check its delivery comments and artifacts against the acceptance criteria in its description.\n" +
	"2. Pass: `multica issue status <sub_issue_id> done`. If that closes a stage, advance the next stage in this same run as usual.\n" +
	"3. Fail: first `multica issue status <sub_issue_id> in_progress`, then comment on it with each failed criterion and its evidence, @-mentioning its assignee so they rework it. Never comment first: their run could start while it still reads `in_review`.\n" +
	"4. When `review_round` is 3 or more and it still fails, do not send it back: set it to `blocked` and comment the disagreement (which criterion, what the assignee says) without @-mentioning the assignee."

// SubIssueReviewFact is the receipt payload (plan §2.5).
type SubIssueReviewFact struct {
	SubIssueID        string                  `json:"sub_issue_id"`
	Identifier        string                  `json:"identifier"`
	Title             string                  `json:"title"`
	ReviewRound       int32                   `json:"review_round"`
	EnteredInReviewAt string                  `json:"entered_in_review_at"`
	SourceTaskID      string                  `json:"source_task_id,omitempty"`
	Comments          []SubIssueReviewComment `json:"comments"`
}

type SubIssueReviewComment struct {
	CommentID string `json:"comment_id"`
	IssueID   string `json:"issue_id"`
	Content   string `json:"content"`
}

const subIssueReviewCommentRunes = 2000

// SubIssueReviewEventKey names one delivery: the sub-issue, its round and when
// it entered review. It is built from the stored row on both the writing and
// the checking side, so the formats always agree.
func SubIssueReviewEventKey(r db.ForkSubIssueReview) string {
	return "review:" + util.UUIDToString(r.IssueID) + ":" + strconv.Itoa(int(r.ReviewRound)) + ":" + r.EnteredAt.Time.UTC().Format(time.RFC3339Nano)
}

// LiveSubIssueReview reports whether a review input (a
// sub_issue.review_requested payload) is still a delivery the agent should
// review now: the sub-issue is in review under this parent, its stored round
// and entry are the ones the input names, and rule 4 still names this agent.
// A delivery that already got a result, a newer delivery, or a change of
// reviewer makes the input stale.
func LiveSubIssueReview(ctx context.Context, q *db.Queries, parentID, agentID pgtype.UUID, payload []byte) (bool, error) {
	var fact SubIssueReviewFact
	if json.Unmarshal(payload, &fact) != nil {
		return false, nil
	}
	childID, err := util.ParseUUID(fact.SubIssueID)
	if err != nil {
		return false, nil
	}
	child, err := q.GetIssue(ctx, childID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if child.Status != "in_review" || child.ParentIssueID != parentID {
		return false, nil
	}
	row, err := q.GetSubIssueReview(ctx, childID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	key := "review:" + fact.SubIssueID + ":" + strconv.Itoa(int(fact.ReviewRound)) + ":" + fact.EnteredInReviewAt
	if SubIssueReviewEventKey(row) != key {
		return false, nil
	}
	reviewer, err := SubIssueReviewer(ctx, q, child)
	if err != nil {
		return false, err
	}
	return reviewer.Kind == ReviewerAgent && reviewer.ID == agentID, nil
}

// EnsureChildReviewRule returns the parent's review rule, creating it when
// missing. It has no condition: only its inputs make it fire.
func EnsureChildReviewRule(ctx context.Context, tx pgx.Tx, q *db.Queries, parent db.Issue) (db.IssueWakeup, error) {
	rule, err := q.GetSystemWakeup(ctx, db.GetSystemWakeupParams{IssueID: parent.ID, SystemRule: systemRuleText(SystemRuleChildReview)})
	if !errors.Is(err, pgx.ErrNoRows) {
		return rule, err
	}
	rule, err = q.CreateSystemWakeup(ctx, db.CreateSystemWakeupParams{
		ID: dbid.NewV7(), WorkspaceID: parent.WorkspaceID, IssueID: parent.ID,
		EventTypes: []string{SubIssueReviewEventType}, Enabled: true, SystemRule: systemRuleText(SystemRuleChildReview),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return q.GetSystemWakeup(ctx, db.GetSystemWakeupParams{IssueID: parent.ID, SystemRule: systemRuleText(SystemRuleChildReview)})
	}
	return rule, err
}

// SettleSubIssueReviews handles every delivery whose run (and each retry) has
// ended, and every held comment whose run ended without delivering: it records
// the review input for an agent reviewer (merging the held comments), or
// settles with nothing to do and releases them. Terminal task events call it,
// and a 30-second sweep covers lost events and a person's direct change.
// Each delivery settles in its own transaction under a row lock, so the event
// listener and the sweep can run it at once.
func (s *IssueWakeupService) SettleSubIssueReviews(ctx context.Context) error {
	ids, err := s.Tasks.Queries.ListSettleableSubIssueReviews(ctx, 200)
	if err != nil {
		return err
	}
	var errs []error
	var rules []db.IssueWakeup
	for _, id := range ids {
		rule, err := s.settleSubIssueReview(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if rule.ID.Valid {
			rules = append(rules, rule)
		}
	}
	releasable, err := s.Tasks.Queries.ListReleasableDeferredComments(ctx, 200)
	if err != nil {
		errs = append(errs, err)
	}
	for _, d := range releasable {
		if err := s.Tasks.Queries.ReleaseDeferredComment(ctx, db.ReleaseDeferredCommentParams{CommentID: d.CommentID, AgentID: d.AgentID}); err != nil {
			errs = append(errs, err)
		}
	}
	// Fire right away; a rule another writer holds is picked up by the
	// scheduler's input lane within 30 seconds.
	seen := map[pgtype.UUID]bool{}
	for _, rule := range rules {
		if seen[rule.ID] {
			continue
		}
		seen[rule.ID] = true
		if err := s.dispatchSystem(ctx, rule); err != nil {
			slog.Info("sub-issue review: immediate dispatch deferred to the scheduler", "wakeup_id", util.UUIDToString(rule.ID), "error", err)
		}
	}
	return errors.Join(errs...)
}

// settleSubIssueReview settles one delivery and returns the review rule that
// got an input, if any.
func (s *IssueWakeupService) settleSubIssueReview(ctx context.Context, issueID pgtype.UUID) (db.IssueWakeup, error) {
	var none db.IssueWakeup
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return none, err
	}
	defer tx.Rollback(ctx)
	q := s.Tasks.Queries.WithTx(tx)
	row, err := q.LockSubIssueReview(ctx, issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return none, nil // settled meanwhile, or another pass holds it
	}
	if err != nil {
		return none, err
	}
	if row.DeliveryTaskID.Valid {
		var ended bool
		if err = tx.QueryRow(ctx, "SELECT fork_task_chain_ended($1)", row.DeliveryTaskID).Scan(&ended); err != nil {
			return none, err
		}
		if !ended {
			return none, nil
		}
	}
	held, err := q.LockDeferredCommentsForChild(ctx, issueID)
	if err != nil {
		return none, err
	}
	// Only the comments this delivery's run held belong to it; the others
	// wait for their own run to end.
	var mine []db.LockDeferredCommentsForChildRow
	for _, d := range held {
		if row.DeliveryTaskID.Valid && d.DeliveryTaskID == row.DeliveryTaskID {
			mine = append(mine, d)
		}
	}
	release := func(rows []db.LockDeferredCommentsForChildRow) error {
		for _, d := range rows {
			if err := q.ReleaseDeferredComment(ctx, db.ReleaseDeferredCommentParams{CommentID: d.CommentID, AgentID: d.AgentID}); err != nil {
				return err
			}
		}
		return nil
	}
	finish := func(rows []db.LockDeferredCommentsForChildRow) (db.IssueWakeup, error) {
		if err := release(rows); err != nil {
			return none, err
		}
		if err := q.SettleSubIssueReview(ctx, issueID); err != nil {
			return none, err
		}
		return none, tx.Commit(ctx)
	}
	child, err := q.GetIssue(ctx, issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finish(mine)
	}
	if err != nil {
		return none, err
	}
	// The delivery already has a result (or the sub-issue left its parent).
	if child.Status != "in_review" || !child.ParentIssueID.Valid {
		return finish(mine)
	}
	reviewer, err := SubIssueReviewer(ctx, q, child)
	if err != nil {
		return none, err
	}
	if reviewer.Kind != ReviewerAgent {
		return finish(mine)
	}
	parent, err := q.GetIssue(ctx, child.ParentIssueID)
	if err != nil {
		return none, err
	}
	rule, err := EnsureChildReviewRule(ctx, tx, q, parent)
	if err != nil {
		return none, err
	}
	prefix := ""
	if ws, err := q.GetWorkspace(ctx, child.WorkspaceID); err == nil {
		prefix = ws.IssuePrefix
	}
	fact := SubIssueReviewFact{
		SubIssueID: util.UUIDToString(child.ID), Identifier: IssueIdentifier(prefix, child.Number), Title: child.Title,
		ReviewRound: row.ReviewRound, EnteredInReviewAt: row.EnteredAt.Time.UTC().Format(time.RFC3339Nano),
		Comments: []SubIssueReviewComment{},
	}
	if row.DeliveryTaskID.Valid {
		fact.SourceTaskID = util.UUIDToString(row.DeliveryTaskID)
	}
	var merged []pgtype.UUID
	var others []db.LockDeferredCommentsForChildRow
	for _, d := range mine {
		if d.AgentID != reviewer.ID {
			others = append(others, d) // held for an agent that is no longer the reviewer
			continue
		}
		c, err := q.GetComment(ctx, d.CommentID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && c.DeletedAt.Valid) {
			merged = append(merged, d.CommentID) // gone: nothing to deliver
			continue
		}
		if err != nil {
			return none, err
		}
		content := []rune(c.Content)
		if len(content) > subIssueReviewCommentRunes {
			content = content[:subIssueReviewCommentRunes]
		}
		fact.Comments = append(fact.Comments, SubIssueReviewComment{CommentID: util.UUIDToString(c.ID), IssueID: util.UUIDToString(c.IssueID), Content: string(content)})
		merged = append(merged, d.CommentID)
	}
	payload, _ := json.Marshal(fact)
	if _, err = q.RecordWakeupReceipt(ctx, db.RecordWakeupReceiptParams{
		ID: dbid.NewV7(), WakeupID: rule.ID, Revision: rule.Revision, EventKey: SubIssueReviewEventKey(row),
		EventType: SubIssueReviewEventType, Payload: payload,
	}); err != nil {
		return none, err
	}
	if len(merged) > 0 {
		if err = q.MergeDeferredComments(ctx, db.MergeDeferredCommentsParams{CommentIds: merged, AgentID: reviewer.ID}); err != nil {
			return none, err
		}
	}
	if _, err = finish(others); err != nil {
		return none, err
	}
	return rule, nil
}

// systemRuleSpec is what differs between the platform's rules (plan §2.4).
// child_done keeps upstream's behavior.
type systemRuleSpec struct {
	rule string
	// hasCondition: evaluate the rule's condition (child_done's stage check).
	hasCondition bool
	summary      string
	// notifyMember: a member assignee gets an inbox notification.
	notifyMember bool
	// hourlyLimit: pause after wakeupHourlyRunLimit runs in an hour.
	hourlyLimit bool
	// keepInputsWhileParked: a backlog parent keeps the inputs pending; they
	// fire on the first pass after it leaves backlog.
	keepInputsWhileParked bool
	// liveInputs: inputs are checked against current state before they run.
	liveInputs bool
}

var systemRuleSpecs = map[string]systemRuleSpec{
	SystemRuleChildDone:   {rule: SystemRuleChildDone, hasCondition: true, summary: "Wakeup: sub-issues closed", notifyMember: true, hourlyLimit: true},
	SystemRuleChildReview: {rule: SystemRuleChildReview, summary: "Wakeup: sub-issue ready for review", keepInputsWhileParked: true, liveInputs: true},
}

func specForRule(w db.IssueWakeup) systemRuleSpec {
	if spec, ok := systemRuleSpecs[w.SystemRule.String]; ok {
		return spec
	}
	return systemRuleSpecs[SystemRuleChildDone]
}

// instructionFor is the instruction a platform rule's run gets.
func (spec systemRuleSpec) instructionFor(w db.IssueWakeup, settings []byte) string {
	if spec.rule == SystemRuleChildReview {
		return SubIssueReviewInstruction
	}
	return ChildDoneInstruction(w.Instruction, settings)
}

// systemRuleFacts summarizes a firing for the timeline entry.
func (spec systemRuleSpec) facts(receipts []db.IssueWakeupReceipt) map[string]any {
	if spec.rule != SystemRuleChildReview {
		return childDoneFacts(receipts)
	}
	ids := []string{}
	for _, r := range receipts {
		var f SubIssueReviewFact
		if json.Unmarshal(r.Payload, &f) == nil && f.SubIssueID != "" {
			ids = append(ids, f.SubIssueID)
		}
	}
	return map[string]any{"rule": SystemRuleChildReview, "sub_issue_ids": ids}
}

// splitLiveReviewInputs separates review inputs still to be reviewed by the
// agent from stale ones (plan §2.4 "开跑前再核对输入").
func splitLiveReviewInputs(ctx context.Context, q *db.Queries, parentID, agentID pgtype.UUID, receipts []db.IssueWakeupReceipt) (live, stale []db.IssueWakeupReceipt, err error) {
	for _, r := range receipts {
		ok := false
		if r.EventType == SubIssueReviewEventType {
			if ok, err = LiveSubIssueReview(ctx, q, parentID, agentID, r.Payload); err != nil {
				return nil, nil, err
			}
		}
		if ok {
			live = append(live, r)
		} else {
			stale = append(stale, r)
		}
	}
	return live, stale, nil
}

// reviewRunStillLive reports whether a review run waiting to be claimed still
// has at least one live delivery to review; one whose facts all went stale
// is not started.
func reviewRunStillLive(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (bool, error) {
	var stored struct {
		Evidence wakeupEvidence `json:"wakeup_evidence"`
	}
	_ = json.Unmarshal(task.Context, &stored)
	for _, f := range stored.Evidence.Facts {
		if f.EventType != SubIssueReviewEventType {
			continue
		}
		ok, err := LiveSubIssueReview(ctx, q, task.IssueID, task.AgentID, f.Payload)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}
