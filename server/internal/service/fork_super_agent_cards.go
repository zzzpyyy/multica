package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (B, plan 2.4 / 3.1 / 3.2): plan cards — list, confirm, cancel.

// PlanCard is the contract's PlanCard union (kind decides the fields).
type PlanCard struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	ChatSessionID string  `json:"chat_session_id"`
	ChatMessageID string  `json:"chat_message_id"`
	Status        string  `json:"status"`
	Error         *string `json:"error"`
	CreatedAt     string  `json:"created_at"`
	// create_issues
	Items         []SuperIssueItem `json:"items,omitempty"`
	CreatedIssues []SuperRef       `json:"created_issues,omitempty"`
	// add_instances
	Issue    *SuperRef `json:"issue,omitempty"`
	Agent    *SuperRef `json:"agent,omitempty"`
	Splits   []string  `json:"splits,omitempty"`
	Priority *bool     `json:"priority,omitempty"`
	BatchID  *string   `json:"batch_id,omitempty"`
}

type cardPayload struct {
	Items    []SuperIssueItem `json:"items"`
	Issue    *SuperRef        `json:"issue"`
	Agent    *SuperRef        `json:"agent"`
	Splits   []string         `json:"splits"`
	Priority bool             `json:"priority"`
}

type cardResult struct {
	CreatedIssues []SuperRef `json:"created_issues"`
	BatchID       string     `json:"batch_id,omitempty"`
	Error         string     `json:"error,omitempty"`
}

func PlanCardFromRow(a db.SuperAgentAction) PlanCard {
	var p cardPayload
	_ = json.Unmarshal(a.Payload, &p)
	var res cardResult
	if len(a.Result) > 0 {
		_ = json.Unmarshal(a.Result, &res)
	}
	c := PlanCard{
		ID: util.UUIDToString(a.ID), Kind: a.Kind, ChatSessionID: util.UUIDToString(a.ChatSessionID),
		ChatMessageID: util.UUIDToString(a.ChatMessageID), Status: a.Status, CreatedAt: util.TimestampToString(a.CreatedAt),
	}
	if a.Status == "failed" && res.Error != "" {
		c.Error = &res.Error
	}
	switch a.Kind {
	case "create_issues":
		c.Items = p.Items
		c.CreatedIssues = res.CreatedIssues
		if c.CreatedIssues == nil {
			c.CreatedIssues = []SuperRef{}
		}
	case "add_instances":
		c.Issue, c.Agent, c.Splits = p.Issue, p.Agent, p.Splits
		pr := p.Priority
		c.Priority = &pr
		if res.BatchID != "" {
			c.BatchID = &res.BatchID
		}
	}
	return c
}

// CardError is a refused confirm/cancel (plan 3.1).
type CardError struct {
	Status int
	Code   string
	Card   *PlanCard
}

func (e *CardError) Error() string { return e.Code }

// SuperCardDeps are what confirming a card needs from the HTTP layer:
// creating an issue exactly like POST /api/issues does, and checking that the
// confirming member may assign / invoke an agent.
type SuperCardDeps struct {
	CreateIssue func(ctx context.Context, item SuperIssueItem, description string, originTaskID pgtype.UUID) (SuperRef, error)
	CanInvoke   func(db.Agent) bool
}

// ConfirmPlanCard executes a pending card once (B3): a compare-and-set to
// 'creating' decides who runs it; everyone else gets the card's state.
func (s *TaskService) ConfirmPlanCard(ctx context.Context, cardID, memberID pgtype.UUID, deps SuperCardDeps) (PlanCard, error) {
	row, err := s.ownedCard(ctx, cardID, memberID)
	if err != nil {
		return PlanCard{}, err
	}
	won, err := s.Queries.TransitionSuperAgentAction(ctx, db.TransitionSuperAgentActionParams{ID: row.ID, FromStatus: "pending", ToStatus: "creating"})
	if errors.Is(err, pgx.ErrNoRows) {
		cur, gerr := s.Queries.GetSuperAgentAction(ctx, row.ID)
		if gerr != nil {
			return PlanCard{}, gerr
		}
		card := PlanCardFromRow(cur)
		if cur.Status == "cancelled" || cur.Status == "expired" {
			return PlanCard{}, &CardError{Status: http.StatusConflict, Code: "card_closed", Card: &card}
		}
		return card, nil // creating / created / failed: already handled
	}
	if err != nil {
		return PlanCard{}, err
	}
	var p cardPayload
	_ = json.Unmarshal(won.Payload, &p)
	var res cardResult
	var execErr error
	switch won.Kind {
	case "create_issues":
		res.CreatedIssues, execErr = s.confirmCreateIssues(ctx, won, p.Items, deps)
	case "add_instances":
		res.BatchID, execErr = s.confirmAddInstances(ctx, p, memberID, deps)
	default:
		execErr = fmt.Errorf("unknown card kind %q", won.Kind)
	}
	status := "created"
	if execErr != nil {
		status, res.Error = "failed", execErr.Error()
		slog.Warn("super-agent card failed", "card_id", util.UUIDToString(won.ID), "error", execErr)
	}
	if res.CreatedIssues == nil {
		res.CreatedIssues = []SuperRef{}
	}
	resJSON, _ := json.Marshal(res)
	done, err := s.Queries.FinishSuperAgentAction(ctx, db.FinishSuperAgentActionParams{ID: won.ID, Status: status, Result: resJSON})
	if err != nil {
		return PlanCard{}, err
	}
	return PlanCardFromRow(done), nil
}

func (s *TaskService) CancelPlanCard(ctx context.Context, cardID, memberID pgtype.UUID) (PlanCard, error) {
	row, err := s.ownedCard(ctx, cardID, memberID)
	if err != nil {
		return PlanCard{}, err
	}
	done, err := s.Queries.TransitionSuperAgentAction(ctx, db.TransitionSuperAgentActionParams{ID: row.ID, FromStatus: "pending", ToStatus: "cancelled"})
	if errors.Is(err, pgx.ErrNoRows) {
		cur, gerr := s.Queries.GetSuperAgentAction(ctx, row.ID)
		if gerr != nil {
			return PlanCard{}, gerr
		}
		card := PlanCardFromRow(cur)
		if cur.Status == "cancelled" {
			return card, nil
		}
		return PlanCard{}, &CardError{Status: http.StatusConflict, Code: "card_closed", Card: &card}
	}
	if err != nil {
		return PlanCard{}, err
	}
	return PlanCardFromRow(done), nil
}

func (s *TaskService) ownedCard(ctx context.Context, cardID, memberID pgtype.UUID) (db.SuperAgentAction, error) {
	row, err := s.Queries.GetSuperAgentAction(ctx, cardID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.Kind != "create_issues" && row.Kind != "add_instances") {
		return db.SuperAgentAction{}, &CardError{Status: http.StatusNotFound, Code: "not_found"}
	}
	if err != nil {
		return db.SuperAgentAction{}, err
	}
	session, err := s.Queries.GetChatSession(ctx, row.ChatSessionID)
	if err != nil {
		return db.SuperAgentAction{}, &CardError{Status: http.StatusNotFound, Code: "not_found"}
	}
	if session.CreatorID != memberID {
		return db.SuperAgentAction{}, &CardError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return row, nil
}

func (s *TaskService) confirmCreateIssues(ctx context.Context, card db.SuperAgentAction, items []SuperIssueItem, deps SuperCardDeps) ([]SuperRef, error) {
	msg, err := s.Queries.GetChatMessage(ctx, card.ChatMessageID)
	if err != nil {
		return nil, fmt.Errorf("load card message: %w", err)
	}
	session, err := s.Queries.GetChatSession(ctx, card.ChatSessionID)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	slug := ""
	if ws, err := s.Queries.GetWorkspace(ctx, session.WorkspaceID); err == nil {
		slug = ws.Slug
	}
	userName := ""
	if u, err := s.Queries.GetUser(ctx, session.CreatorID); err == nil {
		userName = u.Name
	}
	created := []SuperRef{}
	for _, item := range items {
		desc := s.superIssueDescription(ctx, slug, userName, session.ID, item)
		ref, err := deps.CreateIssue(ctx, item, desc, msg.TaskID)
		if err != nil {
			return created, fmt.Errorf("create %q: %w", item.Title, err)
		}
		created = append(created, ref)
	}
	return created, nil
}

// superIssueDescription is the issue body template (rule 6, plan 2.4): the
// user's messages verbatim, each with a link, then what and done.
func (s *TaskService) superIssueDescription(ctx context.Context, slug, userName string, session pgtype.UUID, item SuperIssueItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s 的原话\n\n", userName)
	for _, id := range item.SourceMessageIDs {
		uid, err := util.ParseUUID(id)
		if err != nil {
			continue
		}
		m, err := s.Queries.GetChatMessage(ctx, uid)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s\n（[对话消息](%s)）\n\n", m.Content, ChatMessageLink(slug, session, m.ID))
	}
	fmt.Fprintf(&b, "## 要做什么\n%s\n\n## 怎样算完成\n%s\n", item.What, item.Done)
	return b.String()
}

// ChatMessageLink is the contract's chat message link (plan 3.6).
func ChatMessageLink(slug string, session, message pgtype.UUID) string {
	return fmt.Sprintf("/%s/chat?session=%s&message=%s", slug, util.UUIDToString(session), util.UUIDToString(message))
}

func (s *TaskService) confirmAddInstances(ctx context.Context, p cardPayload, memberID pgtype.UUID, deps SuperCardDeps) (string, error) {
	if p.Issue == nil || p.Agent == nil {
		return "", errors.New("card has no issue or agent")
	}
	issue, err := s.Queries.GetIssue(ctx, util.MustParseUUID(p.Issue.ID))
	if err != nil {
		return "", fmt.Errorf("load issue: %w", err)
	}
	agent, err := s.Queries.GetAgent(ctx, util.MustParseUUID(p.Agent.ID))
	if err != nil {
		return "", fmt.Errorf("load agent: %w", err)
	}
	batch, _, err := s.CreateInstances(ctx, InstanceRequest{
		Issue: issue, Agent: agent, Splits: p.Splits, Priority: p.Priority, MemberID: memberID, CanInvoke: deps.CanInvoke,
	})
	if err != nil {
		return "", err
	}
	return util.UUIDToString(batch.ID), nil
}

// MarshalJSON emits exactly the contract's fields for the card's kind
// (plan 3.2), with nulls where the contract has them.
func (c PlanCard) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"id": c.ID, "kind": c.Kind, "chat_session_id": c.ChatSessionID, "chat_message_id": c.ChatMessageID,
		"status": c.Status, "error": c.Error, "created_at": c.CreatedAt,
	}
	switch c.Kind {
	case "create_issues":
		items, created := c.Items, c.CreatedIssues
		if items == nil {
			items = []SuperIssueItem{}
		}
		if created == nil {
			created = []SuperRef{}
		}
		out["items"], out["created_issues"] = items, created
	case "add_instances":
		out["issue"], out["agent"], out["splits"], out["batch_id"] = c.Issue, c.Agent, c.Splits, c.BatchID
		out["priority"] = c.Priority != nil && *c.Priority
	}
	return json.Marshal(out)
}
