package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork (B, plan 2.4 / 3.7): the super-agent ends a reply with at most one
// ```super-action fence. The server strips it, checks it deterministically
// and stores the result in super_agent_action in the same transaction as the
// reply. Cards (create_issues, add_instances) wait for the user's confirm;
// forward / set_status are executed right after the turn commits.

const superActionFence = "```super-action\n"

type superAction struct {
	Type     string          `json:"type"`
	Issues   []superIssueReq `json:"issues"`
	Issue    string          `json:"issue"`
	Agent    string          `json:"agent"`
	Splits   []string        `json:"splits"`
	Priority bool            `json:"priority"`
	Status   string          `json:"status"`
	Sources  []string        `json:"sources"`
	Partial  bool            `json:"partial"`
}

type superIssueReq struct {
	Title            string   `json:"title"`
	Assignee         string   `json:"assignee"`
	Status           string   `json:"status"`
	What             string   `json:"what"`
	Done             string   `json:"done"`
	SourceMessageIDs []string `json:"source_message_ids"`
}

// SuperAssignee is a resolved directory entry.
type SuperAssignee struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SuperIssueItem is one PlanCardIssueItem plus its source messages.
type SuperIssueItem struct {
	Title            string        `json:"title"`
	Assignee         SuperAssignee `json:"assignee"`
	What             string        `json:"what"`
	Done             string        `json:"done"`
	Status           string        `json:"status"`
	WillRun          bool          `json:"will_run"`
	SourceMessageIDs []string      `json:"source_message_ids"`
}

// SuperRef names an issue or agent on a card.
type SuperRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier,omitempty"`
	Title      string `json:"title,omitempty"`
	Name       string `json:"name,omitempty"`
}

// SuperDirectPayload is a forward / set_status action before it runs.
type SuperDirectPayload struct {
	Issue      string   `json:"issue"`
	Status     string   `json:"status,omitempty"`
	MessageIDs []string `json:"message_ids"`
	Text       string   `json:"text"`
}

type superPlan struct {
	kind    string
	payload any
}

func (p *superPlan) isCard() bool { return p.kind == "create_issues" || p.kind == "add_instances" }

// splitSuperAction separates a trailing super-action fence from the visible
// reply. Only a fence at the very end counts; anything else stays visible.
func splitSuperAction(output string) (visible, raw string, ok bool) {
	normalized := strings.ReplaceAll(output, "\r\n", "\n")
	trimmed := strings.TrimRight(normalized, " \t\n")
	if !strings.HasSuffix(trimmed, "\n```") && trimmed != strings.TrimSuffix(superActionFence, "\n")+"```" {
		return output, "", false
	}
	withoutClose := strings.TrimSuffix(trimmed, "```")
	withoutClose = strings.TrimSuffix(withoutClose, "\n")
	start := strings.LastIndex(withoutClose, "\n"+superActionFence)
	switch {
	case start >= 0:
		visible, raw = withoutClose[:start], withoutClose[start+1+len(superActionFence):]
	case strings.HasPrefix(withoutClose, superActionFence):
		raw = strings.TrimPrefix(withoutClose, superActionFence)
	default:
		return output, "", false
	}
	if strings.Contains(raw, "\n```") { // the fence closed earlier: not the trailing block
		return output, "", false
	}
	return strings.TrimRight(visible, " \t\n"), raw, true
}

// superTurn is this chat turn's user input.
type superTurn struct {
	session  db.ChatSession
	messages []db.ChatMessage
	text     string
}

func (s *TaskService) loadSuperTurn(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (superTurn, error) {
	session, err := q.GetChatSession(ctx, task.ChatSessionID)
	if err != nil {
		return superTurn{}, fmt.Errorf("load session: %w", err)
	}
	msgs, err := q.ListChatInputMessages(ctx, chatInputOwnerID(task))
	if err != nil {
		return superTurn{}, fmt.Errorf("load turn input: %w", err)
	}
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		parts = append(parts, m.Content)
	}
	return superTurn{session: session, messages: msgs, text: strings.Join(parts, "\n\n")}, nil
}

// planSuperAction strips and checks the action block of a super-agent chat
// reply. It returns the reply to store and, when the action stands, what to
// write into super_agent_action.
func (s *TaskService) planSuperAction(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, body string) (string, *superPlan) {
	visible, raw, ok := splitSuperAction(body)
	if !ok {
		return capReply(body, superReplyMaxRunes), nil
	}
	var act superAction
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &act); err != nil {
		return capReply(visible, superReplyMaxRunes), nil // malformed: strip it, do nothing
	}
	turn, err := s.loadSuperTurn(ctx, q, task)
	if err != nil {
		return capReply(visible, superReplyMaxRunes), nil
	}
	switch act.Type {
	case "create_issues":
		return s.planCreateIssues(ctx, q, turn, visible, act)
	case "add_instances":
		return s.planAddInstances(ctx, q, turn, visible, act)
	case "forward", "set_status":
		kind := act.Type
		if strings.Contains(turn.text, "不通过") { // rule 15: "不通过" never changes status
			kind = "forward"
		}
		ids := make([]string, len(turn.messages))
		for i, m := range turn.messages {
			ids[i] = util.UUIDToString(m.ID)
		}
		p := SuperDirectPayload{Issue: strings.TrimSpace(act.Issue), MessageIDs: ids, Text: turn.text}
		if kind == "set_status" {
			p.Status = strings.TrimSpace(act.Status)
		}
		return orDefault(capReply(visible, superReplyMaxRunes), "好的。"), &superPlan{kind: kind, payload: p}
	}
	return capReply(visible, superReplyMaxRunes), nil // answer/ask belong to answer turns; ignored here
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// superReplyMaxRunes is rule 8's limit on a reply outside the plan card.
const superReplyMaxRunes = 100

// cardNote is the text above a plan card. The card shows every field, so
// text that runs past rule 8's 100 characters is the model restating it
// (B13); the server replaces it with the one sentence the card needs.
func cardNote(visible string) string {
	if v := strings.TrimSpace(visible); v != "" && utf8.RuneCountInString(v) <= superReplyMaxRunes {
		return v
	}
	return "请确认下面的计划。"
}

// capReply holds the model's own text to rule 8's limit (B13, B14): whole
// sentences that fit, cut with "…" only when the first sentence alone is too
// long. Server-written notes (not found, refusals) are not passed here.
// ponytail: a cut can drop a trailing question; INSTRUCTIONS.md keeps
// replies short, so this is a backstop that should rarely fire.
func capReply(s string, max int) string {
	s = strings.TrimSpace(s)
	if v, ok := fitSentences(s, max); ok {
		return v
	}
	return string([]rune(s)[:max-1]) + "…"
}

// fitSentences returns s if it fits in max runes, else its longest prefix
// that ends a sentence (。！？!?), however early; ok is false when even the
// first sentence is longer. A line break is not a sentence end: cutting there
// can leave a dangling "：" or a bare list item.
func fitSentences(s string, max int) (string, bool) {
	r := []rune(s)
	if len(r) <= max {
		return s, true
	}
	for i := max - 1; i >= 0; i-- {
		switch r[i] {
		case '。', '！', '？', '!', '?':
			return string(r[:i+1]), true
		}
	}
	return "", false
}

// superDirectory is the workspace directory assignees are matched against
// (exact name; the super-agent itself excluded).
type superDirectory struct {
	squads, agents, members []SuperAssignee
	agentRows               map[string]db.Agent
}

func (s *TaskService) loadSuperDirectory(ctx context.Context, q *db.Queries, ws pgtype.UUID) superDirectory {
	d := superDirectory{agentRows: map[string]db.Agent{}}
	if rows, err := q.ListSquads(ctx, ws); err == nil {
		for _, r := range rows {
			d.squads = append(d.squads, SuperAssignee{Type: "squad", ID: util.UUIDToString(r.ID), Name: r.Name})
		}
	}
	if rows, err := q.ListAgents(ctx, ws); err == nil {
		for _, r := range rows {
			if IsSuperAgent(r) {
				continue
			}
			d.agents = append(d.agents, SuperAssignee{Type: "agent", ID: util.UUIDToString(r.ID), Name: r.Name})
			d.agentRows[util.UUIDToString(r.ID)] = r
		}
	}
	if rows, err := q.ListMembersWithUser(ctx, ws); err == nil {
		for _, r := range rows {
			d.members = append(d.members, SuperAssignee{Type: "member", ID: util.UUIDToString(r.UserID), Name: r.UserName})
		}
	}
	return d
}

func (d superDirectory) find(name string, kinds ...string) (SuperAssignee, bool) {
	name = strings.TrimSpace(name)
	groups := map[string][]SuperAssignee{"squad": d.squads, "agent": d.agents, "member": d.members}
	for _, k := range kinds {
		for _, a := range groups[k] {
			if a.Name == name {
				return a, true
			}
		}
	}
	return SuperAssignee{}, false
}

func names(xs []SuperAssignee) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.Name
	}
	if len(out) == 0 {
		return "无"
	}
	return strings.Join(out, "、")
}

func (d superDirectory) notFound(missing []string) string {
	return fmt.Sprintf("找不到%s。可选的小队：%s；智能体：%s；成员：%s。",
		"「"+strings.Join(missing, "」「")+"」", names(d.squads), names(d.agents), names(d.members))
}

func (s *TaskService) planCreateIssues(ctx context.Context, q *db.Queries, turn superTurn, visible string, act superAction) (string, *superPlan) {
	dir := s.loadSuperDirectory(ctx, q, turn.session.WorkspaceID)
	if len(act.Issues) == 0 {
		return capReply(visible, superReplyMaxRunes), nil
	}
	var missing []string
	needsDetail := false
	items := make([]SuperIssueItem, 0, len(act.Issues))
	for _, r := range act.Issues {
		who, ok := dir.find(r.Assignee, "squad", "agent", "member")
		if !ok {
			missing = append(missing, strings.TrimSpace(r.Assignee))
			continue
		}
		what, done := strings.TrimSpace(r.What), strings.TrimSpace(r.Done)
		if what == "" || done == "" {
			needsDetail = true
		}
		status := strings.TrimSpace(r.Status)
		if status != "backlog" {
			status = "todo"
		}
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = oneLineRunes(what, 40)
		}
		items = append(items, SuperIssueItem{
			Title: title, Assignee: who, What: what, Done: done, Status: status,
			WillRun:          who.Type != "member" && status != "backlog",
			SourceMessageIDs: s.keepUserMessages(ctx, q, turn, r.SourceMessageIDs),
		})
	}
	if len(missing) > 0 { // B10: the whole reply becomes the not-found note
		return dir.notFound(missing), nil
	}
	if needsDetail { // rule 2, B11/B17
		const ask = "\n\n还需要说明：要做什么 / 怎样算完成。"
		// The model's sentences that fit beside the ask; none fit -> the ask alone.
		note, _ := fitSentences(strings.TrimSpace(visible), superReplyMaxRunes-utf8.RuneCountInString(ask))
		return strings.TrimSpace(note + ask), nil
	}
	return cardNote(visible), &superPlan{kind: "create_issues", payload: map[string]any{"items": items}}
}

// keepUserMessages keeps the ids that are the user's messages in this
// session; with none left, the turn's own messages (plan 2.4 ④).
func (s *TaskService) keepUserMessages(ctx context.Context, q *db.Queries, turn superTurn, ids []string) []string {
	var kept []string
	for _, id := range ids {
		uid, err := util.ParseUUID(strings.TrimSpace(id))
		if err != nil {
			continue
		}
		m, err := q.GetChatMessage(ctx, uid)
		if err == nil && m.ChatSessionID == turn.session.ID && m.Role == "user" {
			kept = append(kept, util.UUIDToString(m.ID))
		}
	}
	if len(kept) == 0 {
		for _, m := range turn.messages {
			kept = append(kept, util.UUIDToString(m.ID))
		}
	}
	return kept
}

func (s *TaskService) planAddInstances(ctx context.Context, q *db.Queries, turn superTurn, visible string, act superAction) (string, *superPlan) {
	refuse := func(why string) (string, *superPlan) { return "不能加开：" + why, nil }
	issue, ok := ResolveIssueIdentifier(ctx, q, turn.session.WorkspaceID, act.Issue)
	if !ok {
		return refuse(fmt.Sprintf("找不到任务「%s」。", strings.TrimSpace(act.Issue)))
	}
	dir := s.loadSuperDirectory(ctx, q, turn.session.WorkspaceID)
	who, ok := dir.find(act.Agent, "agent")
	if !ok {
		return refuse(fmt.Sprintf("找不到智能体「%s」。可选：%s。", strings.TrimSpace(act.Agent), names(dir.agents)))
	}
	agent := dir.agentRows[who.ID]
	splits := make([]string, 0, len(act.Splits))
	for _, sp := range act.Splits {
		if sp = strings.TrimSpace(sp); sp == "" {
			return refuse("每个实例都要有分工说明。")
		}
		splits = append(splits, sp)
	}
	if n := int(agent.MaxConcurrentTasks); len(splits) < 1 || len(splits) > n {
		return refuse(fmt.Sprintf("%s 一次最多 %d 个实例，这次是 %d 个。", agent.Name, n, len(splits)))
	}
	if err := s.CheckInstanceAdmission(ctx, issue, agent); err != nil {
		return refuse(err.Error())
	}
	priority := act.Priority && strings.Contains(turn.text, "优先") // only the user's own words can ask for it
	prefix := ""
	if ws, err := q.GetWorkspace(ctx, issue.WorkspaceID); err == nil {
		prefix = ws.IssuePrefix
	}
	return cardNote(visible), &superPlan{kind: "add_instances", payload: map[string]any{
		"issue":    SuperRef{ID: util.UUIDToString(issue.ID), Identifier: IssueIdentifier(prefix, issue.Number), Title: issue.Title},
		"agent":    SuperRef{ID: who.ID, Name: who.Name},
		"splits":   splits,
		"priority": priority,
	}}
}

var issueIdentifierRe = regexp.MustCompile(`(?i)^\s*([a-z][a-z0-9]*)-(\d+)\s*$`)

// ResolveIssueIdentifier finds "BUNN-12" (case-insensitive) in the workspace.
func ResolveIssueIdentifier(ctx context.Context, q *db.Queries, ws pgtype.UUID, identifier string) (db.Issue, bool) {
	m := issueIdentifierRe.FindStringSubmatch(identifier)
	if m == nil {
		return db.Issue{}, false
	}
	w, err := q.GetWorkspace(ctx, ws)
	if err != nil || !strings.EqualFold(w.IssuePrefix, m[1]) {
		return db.Issue{}, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return db.Issue{}, false
	}
	issue, err := q.GetIssueByNumber(ctx, db.GetIssueByNumberParams{WorkspaceID: ws, Number: int32(n)})
	return issue, err == nil
}

// MentionsIdentifier reports whether text names the issue, e.g. "BUNN-12"
// but not "BUNN-123".
func MentionsIdentifier(text, identifier string) bool {
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])` + regexp.QuoteMeta(identifier) + `($|[^0-9])`)
	return re.MatchString(text)
}

func oneLineRunes(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// writeSuperAction stores the plan for the assistant message just written;
// a new card expires the session's older pending ones (B7).
func writeSuperAction(ctx context.Context, q *db.Queries, msg db.ChatMessage, plan *superPlan) error {
	payload, err := json.Marshal(plan.payload)
	if err != nil {
		return err
	}
	row, err := q.CreateSuperAgentAction(ctx, db.CreateSuperAgentActionParams{
		ChatSessionID: msg.ChatSessionID, ChatMessageID: msg.ID, Kind: plan.kind, Payload: payload, Status: "pending",
	})
	if err != nil {
		return fmt.Errorf("write super-agent action: %w", err)
	}
	if plan.isCard() {
		return q.ExpirePendingSuperAgentCards(ctx, db.ExpirePendingSuperAgentCardsParams{ChatSessionID: msg.ChatSessionID, KeepID: row.ID})
	}
	return nil
}
