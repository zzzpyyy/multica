package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (B, plan 3.1): plan cards.
//   GET  /api/chat/sessions/{sessionId}/plan-cards
//   POST /api/chat/plan-cards/{cardId}/confirm
//   POST /api/chat/plan-cards/{cardId}/cancel

func (h *Handler) ListPlanCards(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	session, ok := h.loadChatSessionForUser(w, r, userID, ctxWorkspaceID(r.Context()), chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListSuperAgentCards(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load plan cards")
		return
	}
	cards := make([]service.PlanCard, len(rows))
	for i, row := range rows {
		cards[i] = service.PlanCardFromRow(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

func (h *Handler) ConfirmPlanCard(w http.ResponseWriter, r *http.Request) {
	h.planCardAction(w, r, true)
}

func (h *Handler) CancelPlanCard(w http.ResponseWriter, r *http.Request) {
	h.planCardAction(w, r, false)
}

func (h *Handler) planCardAction(w http.ResponseWriter, r *http.Request, confirm bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	cardID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "cardId"), "card id")
	if !ok {
		return
	}
	member := parseUUID(userID)
	var card service.PlanCard
	var err error
	if confirm {
		card, err = h.TaskService.ConfirmPlanCard(r.Context(), cardID, member, h.superCardDeps(r, userID))
	} else {
		card, err = h.TaskService.CancelPlanCard(r.Context(), cardID, member)
	}
	var ce *service.CardError
	switch {
	case errors.As(err, &ce) && ce.Card != nil:
		writeJSON(w, ce.Status, map[string]any{"error": "the card is closed", "code": ce.Code, "card": ce.Card})
	case errors.As(err, &ce):
		writeError(w, ce.Status, ce.Code)
	case err != nil:
		slog.Error("plan card action failed", "card_id", uuidToString(cardID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update the plan card")
	default:
		writeJSON(w, http.StatusOK, card)
	}
}

// superCardDeps creates card issues as the super-agent (origin agent_create,
// the chat turn as origin) after the same assignee check POST /api/issues
// runs for the confirming member.
func (h *Handler) superCardDeps(r *http.Request, userID string) service.SuperCardDeps {
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID := parseUUID(workspaceID)
	return service.SuperCardDeps{
		CanInvoke: func(a db.Agent) bool {
			return h.canInvokeAgent(r.Context(), a, "member", userID, userID, workspaceID)
		},
		CreateIssue: func(ctx context.Context, item service.SuperIssueItem, description string, originTaskID pgtype.UUID) (service.SuperRef, error) {
			assigneeType := pgtype.Text{String: item.Assignee.Type, Valid: true}
			assigneeID := parseUUID(item.Assignee.ID)
			if status, msg := h.validateAssigneePair(ctx, r, workspaceID, assigneeType, assigneeID); status != 0 {
				return service.SuperRef{}, errors.New(msg)
			}
			sa, err := h.Queries.GetAgentBySystemKey(ctx, db.GetAgentBySystemKeyParams{
				WorkspaceID: wsUUID, SystemKey: pgtype.Text{String: service.SuperAgentSystemKey, Valid: true},
			})
			if err != nil {
				return service.SuperRef{}, err
			}
			prefix := h.getIssuePrefix(ctx, wsUUID)
			res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
				WorkspaceID:  wsUUID,
				Title:        item.Title,
				Description:  pgtype.Text{String: description, Valid: true},
				Status:       item.Status,
				Priority:     "none",
				AssigneeType: assigneeType,
				AssigneeID:   assigneeID,
				CreatorType:  "agent",
				CreatorID:    sa.ID,
				OriginType:   pgtype.Text{String: "agent_create", Valid: originTaskID.Valid},
				OriginID:     originTaskID,
			}, service.IssueCreateOpts{
				ActorID: uuidToString(sa.ID),
				BroadcastPayload: func(issue db.Issue, _ []db.Attachment, labels []db.IssueLabel) map[string]any {
					payload := issueToResponse(issue, prefix)
					h.fillStatusCategory(ctx, wsUUID, &payload)
					lr := labelsToResponse(labels)
					payload.Labels = &lr
					return map[string]any{"issue": payload}
				},
			})
			if err != nil {
				return service.SuperRef{}, err
			}
			return service.SuperRef{ID: uuidToString(res.Issue.ID), Identifier: service.IssueIdentifier(prefix, res.Issue.Number), Title: res.Issue.Title}, nil
		},
	}
}
