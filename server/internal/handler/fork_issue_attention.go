package handler

import (
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

type issueAttentionResponse struct {
	AwaitingReply []issueAttentionItem `json:"awaiting_reply"`
	InReview      []issueAttentionItem `json:"in_review"`
}

// ListIssueAttention serves GET /api/issues/attention for the requesting
// member, workspace-wide.
//   - in_review: sub-issues (any depth) whose status is in_review. Rules 4–6
//     (who accepts a sub-issue) are not live yet, so every one waits for you.
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
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	ctx := r.Context()

	inReview, err := h.Queries.ListInReviewSubIssueIDs(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues in review")
		return
	}

	terminal, err := h.terminalIssueStatusKeys(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve status categories")
		return
	}
	candidates, err := h.Queries.ListAttentionMentionCandidates(ctx, db.ListAttentionMentionCandidatesParams{
		WorkspaceID:        wsUUID,
		TerminalStatusKeys: terminal,
		MentionPattern:     "%mention://member/" + strings.ToLower(userID) + ")%",
		UserID:             userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list mentions")
		return
	}
	// Latest confirmed mention per issue. Code spans, fenced blocks and
	// non-canonical links do not mention anyone (util.ParseMentions).
	lastMention := map[string]pgtype.Timestamptz{}
	var mentioned []pgtype.UUID
	for _, c := range candidates {
		if !mentionsMember(c.Content, userID) {
			continue
		}
		key := uuidToString(c.IssueID)
		prev, seen := lastMention[key]
		if !seen {
			mentioned = append(mentioned, c.IssueID)
		}
		if !seen || c.CreatedAt.Time.After(prev.Time) {
			lastMention[key] = c.CreatedAt
		}
	}
	var awaiting []pgtype.UUID
	if len(mentioned) > 0 {
		replies, err := h.Queries.ListAttentionReplyTimes(ctx, db.ListAttentionReplyTimesParams{
			IssueIds: mentioned, UserID: userUUID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list replies")
			return
		}
		replied := make(map[string]pgtype.Timestamptz, len(replies))
		for _, reply := range replies {
			replied[uuidToString(reply.IssueID)] = reply.RepliedAt
		}
		for _, id := range mentioned {
			key := uuidToString(id)
			if at, ok := replied[key]; ok && at.Time.After(lastMention[key].Time) {
				continue
			}
			awaiting = append(awaiting, id)
		}
	}

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
		AwaitingReply: items(awaiting),
		InReview:      items(inReview),
	})
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
