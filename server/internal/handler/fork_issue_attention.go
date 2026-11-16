package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

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
	// InReviewByOthers stays empty until rules 4–6 ship with the daemon
	// (SB4b); until then every in-review issue waits for the requester.
	InReviewByOthers []issueReviewByOthersItem `json:"in_review_by_others"`
}

// ListIssueAttention serves GET /api/issues/attention for the requesting
// member, workspace-wide.
//   - in_review: issues (top-level and sub-issues at any depth) whose status
//     is in_review. Rules 4–6 (who accepts a sub-issue) are not live yet, so
//     every one waits for you and in_review_by_others is empty (SB4a).
//   - awaiting_reply: open issues whose latest comment @-mentioning you
//     (not by you, not by the super-agent) has no later comment by you or
//     by the super-agent.
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

	inReview, err := h.Queries.ListInReviewIssueIDs(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues in review")
		return
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
		InReviewByOthers: []issueReviewByOthersItem{},
	})
}

// memberAwaitingReply is A rule 7's "待你回复" for one member. It is the one
// judgment of whether an @ still waits for the member: attention's
// awaiting_reply and the super-agent's waiting questions (B rule 14; BUNN-29
// "Nest 批 R1 修复") both read it. An @-mention comment waits while it
// mentions the member directly, is not deleted, its issue is open, and no
// comment by the member or the super-agent on that issue came after it.
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
	listed := map[pgtype.UUID]bool{}
	for _, c := range mentions {
		if at, ok := replied[c.IssueID]; ok && at.Time.After(c.CreatedAt.Time) {
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
