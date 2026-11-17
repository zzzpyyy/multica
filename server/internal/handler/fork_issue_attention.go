package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (zzzpyyy/multica), PRD A rule 7: the one-line progress counts two
// things below a card at any depth that the existing endpoints cannot give
// (`/children` is one level, `child-progress` has no in-review count, and the
// inbox skips agent @-mentions routed to the super-agent). Plan v2.3-A r3, 2.2.

type issueAttentionItem struct {
	IssueID     string   `json:"issue_id"`
	AncestorIDs []string `json:"ancestor_ids"`
}

// issueReviewByOthersItem is an in-review sub-issue someone else reviews
// (rule 4). Plan v2.4-A2 4.1.
type issueReviewByOthersItem struct {
	IssueID  string           `json:"issue_id"`
	Reviewer issueReviewerRef `json:"reviewer"`
}

type issueReviewerRef struct {
	Type string `json:"type"` // member | agent
	ID   string `json:"id"`
}

type issueAttentionResponse struct {
	AwaitingReply []issueAttentionItem `json:"awaiting_reply"`
	InReview      []issueAttentionItem `json:"in_review"`
	// InReviewByOthers: in-review sub-issues another member or a wakeable
	// agent reviews (rule 4, SB4b).
	InReviewByOthers []issueReviewByOthersItem `json:"in_review_by_others"`
}

// ListIssueAttention serves GET /api/issues/attention for the requesting
// member, workspace-wide.
//   - in_review / in_review_by_others: every issue whose status is in_review
//     is in exactly one of them. Rule 4 (service.ResolveSubIssueReviewer, the
//     one implementation) decides: top-level issues and sub-issues nobody else
//     can review wait for you; those another member or a wakeable agent that
//     did not deliver them reviews list that reviewer (SB4b).
//   - awaiting_reply: open issues with at least one @ of you still waiting
//     for your reply (loadMemberAwaitingReply).
func (h *Handler) ListIssueAttention(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	if _, err := util.ParseUUID(userID); err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	ctx := r.Context()

	reviewRows, err := h.Queries.ListInReviewReviewerInputs(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues in review")
		return
	}
	userUUID := parseUUID(userID)
	var inReview []pgtype.UUID
	byOthers := []issueReviewByOthersItem{}
	for _, row := range reviewRows {
		reviewer := service.ResolveSubIssueReviewer(service.ReviewerInputFromRow(row.ParentIssueID, row.ParentAssigneeType,
			row.ParentAssigneeID, row.ParentSquadLeaderID, row.ReviewerAgentWakeable, row.ParentMemberPresent,
			row.ChildAssigneeType, row.ChildAssigneeID, row.ChildSquadLeaderID))
		switch {
		case reviewer.Kind == service.ReviewerAgent:
			byOthers = append(byOthers, issueReviewByOthersItem{IssueID: uuidToString(row.ID), Reviewer: issueReviewerRef{Type: "agent", ID: uuidToString(reviewer.ID)}})
		case reviewer.Kind == service.ReviewerMember && reviewer.ID != userUUID:
			byOthers = append(byOthers, issueReviewByOthersItem{IssueID: uuidToString(row.ID), Reviewer: issueReviewerRef{Type: "member", ID: uuidToString(reviewer.ID)}})
		default: // yours, self, or the parent's member is you
			inReview = append(inReview, row.ID)
		}
	}

	reply, err := h.loadMemberAwaitingReply(ctx, wsUUID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues awaiting your reply")
		return
	}
	awaiting := reply.issues

	ancestors := map[string][]string{}
	if ids := append(append([]pgtype.UUID(nil), inReview...), awaiting...); len(ids) > 0 {
		rows, err := h.Queries.ListIssueAncestors(ctx, db.ListIssueAncestorsParams{WorkspaceID: wsUUID, IssueIds: ids})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list parent chains")
			return
		}
		for _, row := range rows { // ordered by issue, then depth: nearest first
			key := uuidToString(row.IssueID)
			ancestors[key] = append(ancestors[key], uuidToString(row.AncestorID))
		}
	}
	items := func(ids []pgtype.UUID) []issueAttentionItem {
		out := make([]issueAttentionItem, len(ids))
		for i, id := range ids {
			key := uuidToString(id)
			chain := ancestors[key]
			if chain == nil {
				chain = []string{}
			}
			out[i] = issueAttentionItem{IssueID: key, AncestorIDs: chain}
		}
		return out
	}
	writeJSON(w, http.StatusOK, issueAttentionResponse{
		AwaitingReply:    items(awaiting),
		InReview:         items(inReview),
		InReviewByOthers: byOthers,
	})
}

// memberAwaitingReply is A rule 7's "待你回复" for one member, judged per @
// (一鸣 11-17, BUNN-29 "Nest 批 R1 修复"). It is the one judgment of whether
// an @ still waits for the member: attention's awaiting_reply and the
// super-agent's waiting questions (B rule 14) both read it. An @ waits while
//   - it mentions the member directly, is not deleted, and its issue is open;
//   - no comment by the member, and no super-agent forward of their answer,
//     came after it on that issue (another member's comment does not count);
//   - the super-agent has not fully answered it (a partial answer leaves it).
type memberAwaitingReply struct {
	issues   []pgtype.UUID   // issues with at least one waiting @
	comments map[string]bool // waiting @ comment ids
}

func (h *Handler) loadMemberAwaitingReply(ctx context.Context, wsUUID pgtype.UUID, userID string) (memberAwaitingReply, error) {
	out := memberAwaitingReply{comments: map[string]bool{}}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return out, err
	}
	terminal, err := h.terminalIssueStatusKeys(ctx, wsUUID)
	if err != nil {
		return out, err
	}
	candidates, err := h.Queries.ListAttentionMentionCandidates(ctx, db.ListAttentionMentionCandidatesParams{
		WorkspaceID:        wsUUID,
		TerminalStatusKeys: terminal,
		MentionPattern:     "%mention://member/" + strings.ToLower(userID) + ")%",
		UserID:             userUUID,
	})
	if err != nil {
		return out, err
	}
	// Code spans, fenced blocks and non-canonical links do not mention
	// anyone (util.ParseMentions).
	var mentions []db.ListAttentionMentionCandidatesRow
	seen := map[pgtype.UUID]bool{}
	var issueIDs []pgtype.UUID
	for _, c := range candidates {
		if !mentionsMember(c.Content, userID) {
			continue
		}
		mentions = append(mentions, c)
		if !seen[c.IssueID] {
			seen[c.IssueID] = true
			issueIDs = append(issueIDs, c.IssueID)
		}
	}
	if len(mentions) == 0 {
		return out, nil
	}
	replies, err := h.Queries.ListAttentionReplyTimes(ctx, db.ListAttentionReplyTimesParams{
		IssueIds: issueIDs, UserID: userUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		return out, err
	}
	replied := make(map[pgtype.UUID]pgtype.Timestamptz, len(replies))
	for _, reply := range replies {
		replied[reply.IssueID] = reply.RepliedAt
	}
	ids := make([]pgtype.UUID, len(mentions))
	for i, c := range mentions {
		ids[i] = c.ID
	}
	answeredRows, err := h.Queries.ListAttentionAnsweredMentions(ctx, db.ListAttentionAnsweredMentionsParams{
		CommentIds: ids, UserID: userUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		return out, err
	}
	answered := make(map[pgtype.UUID]bool, len(answeredRows))
	for _, id := range answeredRows {
		answered[id] = true
	}
	listed := map[pgtype.UUID]bool{}
	for _, c := range mentions {
		if at, ok := replied[c.IssueID]; ok && at.Time.After(c.CreatedAt.Time) {
			continue
		}
		if answered[c.ID] {
			continue
		}
		out.comments[uuidToString(c.ID)] = true
		if !listed[c.IssueID] {
			listed[c.IssueID] = true
			out.issues = append(out.issues, c.IssueID)
		}
	}
	return out, nil
}

// mentionsMember: content @-mentions this member directly (mention://member/<id>).
// @all and squad mentions do not count (plan v2.3-A r3, 待确认 ③).
func mentionsMember(content, userID string) bool {
	for _, m := range util.ParseMentions(content) {
		if m.Type == "member" && strings.EqualFold(m.ID, userID) {
			return true
		}
	}
	return false
}
