package hub

import (
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"fmt"
	"strings"
)

type RotateResult struct {
	Rotated   bool   `json:"rotated"`
	Deferred  bool   `json:"deferred"`
	SessionID string `json:"sessionId,omitempty"`
}

func (h *Hub) ResetSession(botID string) (state.SessionRecord, bool, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return state.SessionRecord{}, false, fmt.Errorf("bot %q not found", botID)
	}
	lane.sessionGen.Add(1)
	lane.rotateMu.Lock()
	lane.pendingRotate = false
	lane.pendingHandoff = ""
	lane.rotateMu.Unlock()

	lane.cancelMu.Lock()
	cancel := lane.cancel
	lane.cancelMu.Unlock()
	if cancel != nil {
		cancel()
		h.desktop.CancelTakeoversByBot(botID)
	}
	if h.cancelIsolatedByBot(botID) > 0 {
		h.desktop.CancelTakeoversByBot(botID)
	}
	h.CancelApprovalsByBot(botID)
	h.clearSessionAllow(botID)

	prev, existed, err := h.store.ArchiveActive(botID, "reset", "", "")
	if err == nil {
		_ = h.store.DismissPendingReceipts(botID)
	}
	if err == nil && existed {
		h.appendMessage(state.ChatMessage{
			BotID:     botID,
			SessionID: prev.ID,
			Role:      "system",
			From:      "system",
			Text:      fmt.Sprintf("Session reset (previous: %s). Workspace and memory/ preserved.", prev.SessionID),
		})
		h.emitStatus(lane)
	}
	return prev, existed, err
}

// RotateSession archives the bot's active session immediately or after the in-flight turn completes.
func (h *Hub) RotateSession(botID, handoffNote string, deferUntilTurnEnd bool) (*RotateResult, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	note := strings.TrimSpace(handoffNote)
	if deferUntilTurnEnd && lane.busy.Load() {
		lane.rotateMu.Lock()
		lane.pendingRotate = true
		lane.pendingHandoff = note
		lane.rotateMu.Unlock()
		rec, _ := h.store.Get(botID)
		return &RotateResult{Deferred: true, SessionID: rec.ID}, nil
	}

	lane.sessionGen.Add(1)
	lane.rotateMu.Lock()
	lane.pendingRotate = false
	lane.pendingHandoff = ""
	lane.rotateMu.Unlock()
	h.CancelApprovalsByBot(botID)
	h.clearSessionAllow(botID)

	prev, existed, err := h.store.ArchiveActive(botID, "reset", "", note)
	if err != nil {
		return nil, err
	}
	if h.store != nil {
		_ = h.store.DismissPendingReceipts(botID)
	}
	if existed {
		h.appendMessage(state.ChatMessage{
			BotID:     botID,
			SessionID: prev.ID,
			Role:      "system",
			From:      "system",
			Text:      fmt.Sprintf("Session archived (%s). The next turn will start a new session.", prev.ID),
		})
		h.emitStatus(lane)
	}
	return &RotateResult{Rotated: existed, SessionID: prev.ID}, nil
}

func (h *Hub) privateBotIDsExcept(callerBotID string) []string {
	if callerBotID == "" {
		return nil
	}
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	var excluded []string
	for id, lane := range h.lanes {
		if id != callerBotID && lane.bot.PrivateSessions {
			excluded = append(excluded, id)
		}
	}
	return excluded
}

func (h *Hub) isPrivateFor(targetBotID, callerBotID string) bool {
	if callerBotID == "" || targetBotID == "" || targetBotID == callerBotID {
		return false
	}
	b, ok := h.GetBot(targetBotID)
	return ok && b.PrivateSessions
}

// ListSessions lists archived and active sessions visible to callerBotID.
func (h *Hub) ListSessions(callerBotID string, opts state.SessionListOptions) ([]state.SessionSummary, error) {
	if h.store == nil {
		return []state.SessionSummary{}, nil
	}
	callerBotID = strings.TrimSpace(callerBotID)
	botFilter := strings.TrimSpace(opts.BotID)
	if botFilter == "" && callerBotID != "" {
		botFilter = callerBotID
		opts.BotID = botFilter
	}
	if botFilter != "" && botFilter != "*" && h.isPrivateFor(botFilter, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q sessions are private", ErrForbidden, botFilter)
	}
	opts.ExcludedBotIDs = append(opts.ExcludedBotIDs, h.privateBotIDsExcept(callerBotID)...)
	return h.store.ListSessions(opts)
}

// RecallSession fetches a sliced transcript for a historical or active session.
func (h *Hub) RecallSession(callerBotID string, opts state.RecallOptions) (*state.RecallResult, error) {
	if h.store == nil {
		return nil, state.ErrSessionNotFound
	}
	callerBotID = strings.TrimSpace(callerBotID)
	if opts.BotID == "" && callerBotID != "" && (opts.SessionID == "" || strings.EqualFold(opts.SessionID, "previous")) {
		opts.BotID = callerBotID
	}
	if opts.BotID != "" && opts.BotID != "*" && h.isPrivateFor(opts.BotID, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q sessions are private", ErrForbidden, opts.BotID)
	}
	res, err := h.store.RecallSession(opts)
	if err != nil {
		return nil, err
	}
	if h.isPrivateFor(res.Session.BotID, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q sessions are private", ErrForbidden, res.Session.BotID)
	}
	return res, nil
}

// MemorySearch queries Magpie Memory (global or scoped to a specific bot's workspace + tag).
func (h *Hub) MemorySearch(ctx context.Context, scope, botID, query, kind string, limit int) (harness.MagpieMemoryList, error) {
	projectDir := ""
	botID = strings.TrimSpace(botID)
	if botID != "" {
		bot, ok := h.GetBot(botID)
		if !ok {
			return harness.MagpieMemoryList{}, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
		}
		projectDir = bot.Workspace
		scope = "project"
	}
	return harness.MagpieMemorySearch(ctx, scope, projectDir, botID, query, kind, limit), nil
}

// MemorySave writes or updates a Magpie Memory entry.
func (h *Hub) MemorySave(ctx context.Context, req harness.MagpieMemorySaveReq) (string, error) {
	req.BotID = strings.TrimSpace(req.BotID)
	if req.BotID != "" {
		bot, ok := h.GetBot(req.BotID)
		if !ok {
			return "", fmt.Errorf("bot %q not found: %w", req.BotID, ErrBotNotFound)
		}
		req.Project = bot.Workspace
		req.Scope = "project"
	}
	return harness.MagpieMemorySave(ctx, req)
}

// MemoryForget forgets/deletes a Magpie Memory entry by ID.
func (h *Hub) MemoryForget(ctx context.Context, id string) (string, error) {
	return harness.MagpieMemoryForget(ctx, id)
}
