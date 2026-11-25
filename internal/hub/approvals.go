package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// PendingQuestion describes an interactive question card awaiting user input.
type PendingQuestion struct {
	ID        string                  `json:"id"`
	BotID     string                  `json:"botId"`
	BotName   string                  `json:"botName,omitempty"`
	Title     string                  `json:"title,omitempty"`
	Fields    []harness.QuestionField `json:"fields"`
	CreatedAt string                  `json:"createdAt"`
}

type pendingQuestionEntry struct {
	item PendingQuestion
	ch   chan harness.QuestionResponse
}

// PendingApproval describes a pending tool execution approval awaiting user decision.
type PendingApproval struct {
	ID        string                     `json:"id"`
	BotID     string                     `json:"botId"`
	BotName   string                     `json:"botName,omitempty"`
	ToolName  string                     `json:"toolName"`
	Kind      string                     `json:"kind"`
	Title     string                     `json:"title,omitempty"`
	Summary   string                     `json:"summary,omitempty"`
	RawInput  map[string]any             `json:"rawInput,omitempty"`
	Options   []harness.PermissionOption `json:"options,omitempty"`
	Mode      string                     `json:"mode,omitempty"`
	CreatedAt string                     `json:"createdAt"`
}

type pendingApprovalEntry struct {
	item PendingApproval
	ch   chan harness.PermissionResponse
}

func isUserDirectSource(opts SendOptions) bool {
	if opts.FromBotID != "" {
		return false
	}
	src := strings.TrimSpace(opts.Source)
	if src == "webhook" ||
		strings.HasPrefix(src, "routine:") ||
		strings.HasPrefix(src, "schedule:") ||
		strings.HasPrefix(src, "bot:") {
		return false
	}
	return true
}

func isInteractiveQuestionSource(opts SendOptions) bool {
	if opts.FromBotID != "" {
		return false
	}
	src := strings.TrimSpace(opts.Source)
	if src == "webhook" || src == "desktop_task" || src == "cli" ||
		strings.HasPrefix(src, "routine:") ||
		strings.HasPrefix(src, "schedule:") ||
		strings.HasPrefix(src, "bot:") {
		return false
	}
	return true
}

func newQuestionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "q_" + hex.EncodeToString(b)
}

func (h *Hub) listQuestionsLocked(botID string) []PendingQuestion {
	items := make([]PendingQuestion, 0, len(h.questions))
	for _, entry := range h.questions {
		if botID != "" && entry.item.BotID != botID {
			continue
		}
		items = append(items, entry.item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt < items[j].CreatedAt
		}
		return items[i].ID < items[j].ID
	})
	return items
}

// ListQuestions returns all pending interactive question cards, optionally filtered by botID.
func (h *Hub) ListQuestions(botID string) []PendingQuestion {
	h.questionsMu.Lock()
	defer h.questionsMu.Unlock()
	return h.listQuestionsLocked(botID)
}

func (h *Hub) broadcastQuestions(items []PendingQuestion) {
	h.broadcast(Event{
		Type:    "questions",
		Payload: map[string]any{"items": items},
	})
}

// AskQuestion publishes a pending question card to connected clients and blocks until answered or cancelled.
func (h *Hub) AskQuestion(ctx context.Context, bot config.BotConfig, req harness.QuestionRequest) (harness.QuestionResponse, error) {
	id := newQuestionID()
	fields := make([]harness.QuestionField, len(req.Fields))
	copy(fields, req.Fields)
	entry := &pendingQuestionEntry{
		item: PendingQuestion{
			ID:        id,
			BotID:     bot.ID,
			BotName:   bot.Name,
			Title:     req.Title,
			Fields:    fields,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		ch: make(chan harness.QuestionResponse, 1),
	}

	h.questionsMu.Lock()
	h.questions[id] = entry
	items := h.listQuestionsLocked("")
	h.questionsMu.Unlock()
	h.broadcastQuestions(items)

	defer func() {
		h.questionsMu.Lock()
		_, existed := h.questions[id]
		if existed {
			delete(h.questions, id)
			items := h.listQuestionsLocked("")
			h.questionsMu.Unlock()
			h.broadcastQuestions(items)
		} else {
			h.questionsMu.Unlock()
		}
	}()

	select {
	case resp := <-entry.ch:
		return resp, nil
	case <-ctx.Done():
		return harness.QuestionResponse{Cancelled: true}, ctx.Err()
	}
}

// AnswerQuestion delivers a user's response (or cancellation) to a pending question card.
func (h *Hub) AnswerQuestion(id string, answers map[string]any, cancelled bool, message string) ([]PendingQuestion, error) {
	h.questionsMu.Lock()
	entry, ok := h.questions[id]
	if !ok {
		h.questionsMu.Unlock()
		return nil, ErrQuestionNotFound
	}
	delete(h.questions, id)
	items := h.listQuestionsLocked("")
	h.questionsMu.Unlock()

	select {
	case entry.ch <- harness.QuestionResponse{Answers: answers, Cancelled: cancelled, Message: message}:
	default:
	}
	h.broadcastQuestions(items)
	return items, nil
}

const approvalWaitTimeout = 15 * time.Minute

func newApprovalID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "apr_" + hex.EncodeToString(b)
}

func (h *Hub) listApprovalsLocked(botID string) []PendingApproval {
	items := make([]PendingApproval, 0, len(h.approvals))
	for _, entry := range h.approvals {
		if botID != "" && entry.item.BotID != botID {
			continue
		}
		items = append(items, entry.item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt < items[j].CreatedAt
		}
		return items[i].ID < items[j].ID
	})
	return items
}

// ListApprovals returns all pending tool approval cards, optionally filtered by botID.
func (h *Hub) ListApprovals(botID string) []PendingApproval {
	h.approvalsMu.Lock()
	defer h.approvalsMu.Unlock()
	return h.listApprovalsLocked(botID)
}

func (h *Hub) broadcastApprovals(items []PendingApproval) {
	h.broadcast(Event{
		Type:    "approvals",
		Payload: map[string]any{"items": items},
	})
}

func approvalAllowKey(req harness.PermissionRequest) string {
	name := strings.ToLower(strings.TrimSpace(req.ToolName))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(req.Kind))
	}
	if name == "" {
		return "tool"
	}
	return name
}

func (h *Hub) isSessionAllowed(botID, key string) bool {
	if botID == "" || key == "" {
		return false
	}
	h.allowlistMu.Lock()
	defer h.allowlistMu.Unlock()
	return h.sessionAllow[botID][key]
}

func (h *Hub) addSessionAllow(botID, key string) {
	if botID == "" || key == "" {
		return
	}
	h.allowlistMu.Lock()
	if h.sessionAllow == nil {
		h.sessionAllow = make(map[string]map[string]bool)
	}
	m := h.sessionAllow[botID]
	if m == nil {
		m = make(map[string]bool)
		h.sessionAllow[botID] = m
	}
	m[key] = true
	h.allowlistMu.Unlock()
}

func (h *Hub) clearSessionAllow(botID string) {
	if botID == "" {
		return
	}
	h.allowlistMu.Lock()
	delete(h.sessionAllow, botID)
	h.allowlistMu.Unlock()
}

// CancelApprovalsByBot cancels and removes all pending approvals for botID.
func (h *Hub) CancelApprovalsByBot(botID string) int {
	h.approvalsMu.Lock()
	var cancelled []*pendingApprovalEntry
	for id, entry := range h.approvals {
		if botID == "" || entry.item.BotID == botID {
			delete(h.approvals, id)
			cancelled = append(cancelled, entry)
		}
	}
	items := h.listApprovalsLocked("")
	h.approvalsMu.Unlock()

	for _, entry := range cancelled {
		select {
		case entry.ch <- harness.PermissionResponse{Decision: "cancelled", Reason: "cancelled"}:
		default:
		}
	}
	if len(cancelled) > 0 {
		h.broadcastApprovals(items)
	}
	return len(cancelled)
}

func (h *Hub) auditApproval(botID string, req harness.PermissionRequest, resp harness.PermissionResponse) {
	if h.store == nil && h.messages == nil {
		return
	}
	tool := strings.TrimSpace(req.ToolName)
	if tool == "" {
		tool = req.Kind
	}
	if tool == "" {
		tool = "tool"
	}
	summary := firstLineSummary(req.Summary, 80)
	var text string
	switch resp.Decision {
	case "allow_once":
		if summary != "" {
			text = fmt.Sprintf("Approved %s: %s", tool, summary)
		} else {
			text = fmt.Sprintf("Approved %s", tool)
		}
	case "allow_always":
		text = fmt.Sprintf("Always allowed for this session: %s", tool)
	case "reject":
		if summary != "" {
			text = fmt.Sprintf("Rejected %s: %s", tool, summary)
		} else {
			text = fmt.Sprintf("Rejected %s", tool)
		}
		if r := firstLineSummary(resp.Reason, 80); r != "" {
			text += fmt.Sprintf(" (%s)", r)
		}
	default:
		return
	}

	var sessID string
	var turn int
	h.lanesMu.RLock()
	lane := h.lanes[botID]
	h.lanesMu.RUnlock()
	if lane != nil {
		lane.turnMu.Lock()
		sessID = lane.activeSessionID
		turn = lane.activeTurn
		lane.turnMu.Unlock()
	}
	if sessID == "" && h.store != nil {
		if rec, ok := h.store.Get(botID); ok {
			sessID = rec.ID
			turn = rec.TurnCount
		}
	}
	h.appendMessage(state.ChatMessage{
		BotID:     botID,
		SessionID: sessID,
		Turn:      turn,
		Role:      "system",
		From:      "system",
		Text:      text,
	})
}

// RequestApproval publishes a pending tool approval card to connected clients and blocks until resolved or cancelled.
func (h *Hub) RequestApproval(ctx context.Context, bot config.BotConfig, mode string, req harness.PermissionRequest) (harness.PermissionResponse, error) {
	allowKey := approvalAllowKey(req)
	if h.isSessionAllowed(bot.ID, allowKey) {
		var allowAlwaysID, fallbackID string
		for _, opt := range req.Options {
			if opt.Kind == "allow_once" && opt.ID != "session_allow" {
				return harness.PermissionResponse{Decision: "allow_once", OptionID: opt.ID}, nil
			}
			if opt.Kind == "allow_always" && opt.ID != "session_allow" && allowAlwaysID == "" {
				allowAlwaysID = opt.ID
			}
			if (opt.Kind == "allow_once" || opt.Kind == "allow_always") && fallbackID == "" {
				fallbackID = opt.ID
			}
		}
		if allowAlwaysID != "" {
			return harness.PermissionResponse{Decision: "allow_once", OptionID: allowAlwaysID}, nil
		}
		return harness.PermissionResponse{Decision: "allow_once", OptionID: fallbackID}, nil
	}

	id := newApprovalID()
	opts := make([]harness.PermissionOption, len(req.Options))
	copy(opts, req.Options)
	if len(opts) == 0 {
		opts = harness.StandardPermissionOptions(true)
	}
	kind := req.Kind
	if kind == "" {
		kind = harness.ClassifyToolKind(req.ToolName)
	}
	entry := &pendingApprovalEntry{
		item: PendingApproval{
			ID:        id,
			BotID:     bot.ID,
			BotName:   bot.Name,
			ToolName:  req.ToolName,
			Kind:      kind,
			Title:     req.Title,
			Summary:   req.Summary,
			RawInput:  harness.SanitizePermissionInput(req.RawInput),
			Options:   opts,
			Mode:      mode,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		ch: make(chan harness.PermissionResponse, 1),
	}

	h.approvalsMu.Lock()
	h.approvals[id] = entry
	items := h.listApprovalsLocked("")
	h.approvalsMu.Unlock()
	h.broadcastApprovals(items)

	defer func() {
		h.approvalsMu.Lock()
		_, existed := h.approvals[id]
		if existed {
			delete(h.approvals, id)
			items := h.listApprovalsLocked("")
			h.approvalsMu.Unlock()
			h.broadcastApprovals(items)
		} else {
			h.approvalsMu.Unlock()
		}
	}()

	timer := time.NewTimer(approvalWaitTimeout)
	defer timer.Stop()

	select {
	case resp := <-entry.ch:
		if resp.Decision == "allow_always" {
			h.addSessionAllow(bot.ID, allowKey)
		}
		if resp.Decision != "cancelled" {
			h.auditApproval(bot.ID, req, resp)
		}
		return resp, nil
	case <-timer.C:
		resp := harness.PermissionResponse{Decision: "reject", Reason: "approval timed out"}
		h.auditApproval(bot.ID, req, resp)
		return resp, nil
	case <-ctx.Done():
		return harness.PermissionResponse{Decision: "cancelled"}, ctx.Err()
	}
}

// ReplyApproval delivers a user's decision to a pending tool approval card.
func (h *Hub) ReplyApproval(id, decision, optionID, reason string) ([]PendingApproval, error) {
	decision = strings.ToLower(strings.TrimSpace(decision))
	switch decision {
	case "allow_once", "allow_always", "reject":
	default:
		return nil, fmt.Errorf("invalid decision %q (expected allow_once, allow_always, or reject)", decision)
	}

	h.approvalsMu.Lock()
	entry, ok := h.approvals[id]
	if !ok {
		h.approvalsMu.Unlock()
		return nil, ErrApprovalNotFound
	}
	delete(h.approvals, id)
	items := h.listApprovalsLocked("")
	h.approvalsMu.Unlock()

	select {
	case entry.ch <- harness.PermissionResponse{
		Decision: decision,
		OptionID: strings.TrimSpace(optionID),
		Reason:   strings.TrimSpace(reason),
	}:
	default:
	}
	h.broadcastApprovals(items)
	return items, nil
}
