package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type turnJob struct {
	ctx             context.Context
	prompt          string
	attachments     []harness.Attachment
	opts            SendOptions
	userMsgID       string
	queuedSessionID string
	respCh          chan turnResponse
}

type turnResponse struct {
	res *harness.TurnResult
	err error
}

type botLane struct {
	bot      config.BotConfig
	ch       chan turnJob
	busy     atomic.Bool
	queued   atomic.Int64
	stopCh   chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once

	sessionGen atomic.Uint64

	turnSentCount   atomic.Int64
	turnMu          sync.Mutex
	activeSessionID string
	activeTurn      int
	turnLastMsgID   string

	rotateMu       sync.Mutex
	pendingRotate  bool
	pendingHandoff string

	cancelMu sync.Mutex
	cancel   context.CancelFunc // interrupts the running turn
}

func (l *botLane) stop() {
	l.stopOnce.Do(func() { close(l.stopCh) })
	<-l.stopped
}

func (h *Hub) startLaneLocked(bot config.BotConfig) {
	lane := &botLane{
		bot:     bot,
		ch:      make(chan turnJob, 64),
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
	h.lanes[bot.ID] = lane
	h.order = append(h.order, bot.ID)
	go h.runLane(lane)
}

func (h *Hub) emitStatus(lane *botLane) {
	h.lanesMu.RLock()
	st := h.statusOfLocked(lane)
	h.lanesMu.RUnlock()
	h.broadcast(Event{
		Type:    "bot_status",
		BotID:   lane.bot.ID,
		Payload: st,
	})
}

func (h *Hub) runLane(lane *botLane) {
	defer close(lane.stopped)
	for {
		select {
		case <-lane.stopCh:
			return
		case job := <-lane.ch:
			lane.queued.Add(-1)
			lane.busy.Store(true)
			h.emitStatus(lane)
			ctx, cancel := context.WithCancel(job.ctx)
			lane.cancelMu.Lock()
			lane.cancel = cancel
			lane.cancelMu.Unlock()
			h.lanesMu.RLock()
			bot := lane.bot
			h.lanesMu.RUnlock()
			res, err := h.executeTurn(ctx, lane, bot, job.prompt, job.attachments, job.opts, job.userMsgID, job.queuedSessionID)
			lane.cancelMu.Lock()
			lane.cancel = nil
			lane.cancelMu.Unlock()
			cancel()
			lane.busy.Store(false)
			h.flushPendingReload(bot.Harness)
			h.emitStatus(lane)
			job.respCh <- turnResponse{res: res, err: err}
		}
	}
}

func (h *Hub) platformInstructionsPath() string {
	h.lanesMu.RLock()
	sharedDir := h.cfg.SharedDir
	h.lanesMu.RUnlock()
	if sharedDir == "" {
		return ""
	}
	return filepath.Join(sharedDir, config.PlatformInstructionsFilename)
}

func (h *Hub) buildTurnPrompt(bot config.BotConfig, prompt string, isFirstTurn bool, platPath string, pendingReceipts []state.RoutineRun) string {
	// Slash commands must stay at the start of the prompt so harnesses recognize them.
	if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
		return prompt
	}
	var prefixParts []string
	if isFirstTurn {
		if h.store != nil {
			if prev, ok := h.store.LatestArchivedSession(bot.ID); ok {
				details := []string{fmt.Sprintf("%d turns", prev.TurnCount), "status=" + prev.Status}
				if prev.LastError != "" {
					details = append(details, fmt.Sprintf("lastError=%q", firstLineSummary(prev.LastError, 120)))
				}
				if prev.HandoffNote != "" {
					details = append(details, fmt.Sprintf("handoff=%q", firstLineSummary(prev.HandoffNote, 120)))
				}
				prefixParts = append(prefixParts, fmt.Sprintf(
					"[System Note: New session started. Previous session %s (%s) is archived. Use recall_session if you need prior context.]",
					prev.ID, strings.Join(details, ", "),
				))
			} else if recent, err := h.store.RecentMessagesForHandoff(bot.ID, 8); err == nil && len(recent) > 0 {
				prefixParts = append(prefixParts, formatCrossHostHandoffPrefix(recent))
			}
		}
		if bot.Harness == "dsh" && platPath != "" {
			if raw, err := os.ReadFile(platPath); err == nil && len(raw) > 0 {
				prefixParts = append(prefixParts, strings.TrimSpace(string(raw)))
			}
		}
	}
	if len(pendingReceipts) > 0 {
		prefixParts = append(prefixParts, formatRoutineReceiptsPrefix(pendingReceipts))
	}
	if len(prefixParts) == 0 {
		return prompt
	}
	return strings.Join(prefixParts, "\n\n") + "\n\n" + prompt
}

func formatCrossHostHandoffPrefix(recent []state.SyncMessageRow) string {
	var lines []string
	lines = append(lines, "[System Note: Cross-host session handoff — recent conversation synced from another host:]")
	for _, m := range recent {
		lines = append(lines, fmt.Sprintf("- %s: %s", m.Role, firstLineSummary(m.Text, 240)))
	}
	return strings.Join(lines, "\n")
}

func formatRoutineReceiptsPrefix(receipts []state.RoutineRun) string {
	var lines []string
	lines = append(lines, "[Background routine receipt — results from earlier background checks, for context only; no need to repeat]")
	for _, r := range receipts {
		name := r.RoutineName
		if name == "" {
			name = r.RoutineID
		}
		ts := r.FinishedAt
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			ts = parsed.In(time.Local).Format("01-02 15:04")
		} else if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			ts = parsed.In(time.Local).Format("01-02 15:04")
		}
		excerpt := truncateRunes(strings.TrimSpace(r.Output), 1500)
		lines = append(lines, fmt.Sprintf("- [%s @ %s]: %s", name, ts, excerpt))
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func formatToolCounts(order []string, counts map[string]int) []string {
	if len(order) == 0 {
		return nil
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		if n := counts[name]; n > 1 {
			out = append(out, fmt.Sprintf("%s×%d", name, n))
		} else {
			out = append(out, name)
		}
	}
	return out
}

func isFatalSessionError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "timed out") || strings.Contains(s, "interrupted") {
		return false
	}
	patterns := []string{
		"context_length_exceeded",
		"context length exceeded",
		"maximum context length",
		"context window",
		"prompt is too long",
		"input is too long",
		"too many tokens",
		"session not found",
		"no conversation found",
		"invalid session",
		"failed to resume",
		"unable to resume",
	}
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	if strings.Contains(s, "tool_use") && strings.Contains(s, "tool_result") {
		return true
	}
	return false
}

func firstLineSummary(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	r := []rune(s)
	if len(r) <= max || max <= 0 {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func (h *Hub) executeTurn(ctx context.Context, lane *botLane, bot config.BotConfig, prompt string, attachments []harness.Attachment, opts SendOptions, userMsgID, queuedSessionID string) (*harness.TurnResult, error) {
	runner, err := h.registry.Get(bot.Harness)
	if err != nil {
		return nil, err
	}

	h.lanesMu.RLock()
	apiToken := h.cfg.HTTP.APIToken
	hubURL := h.cfg.HTTP.BaseURL()
	cfgPath := h.cfg.ConfigPath
	mcpCmd := config.MCPCommand(h.cfg, bot.ID)
	h.lanesMu.RUnlock()

	platPath := h.platformInstructionsPath()
	extraEnv := map[string]string{
		"PEBBLE_URL":    hubURL,
		"PEBBLE_BOT_ID": bot.ID,
		"PEBBLE_HOP":    strconv.Itoa(opts.Hop),
	}
	if cfgPath != "" {
		extraEnv["PEBBLE_CONFIG"] = cfgPath
	}
	if apiToken != "" {
		extraEnv["PEBBLE_API_TOKEN"] = apiToken
	}
	for k, v := range h.botBrowserEnv(bot) {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}

	h.hopsMu.Lock()
	h.hops[bot.ID] = opts.Hop
	h.hopsMu.Unlock()
	defer func() {
		h.hopsMu.Lock()
		delete(h.hops, bot.ID)
		h.hopsMu.Unlock()
	}()

	var mcpEnv map[string]string
	if apiToken != "" {
		mcpEnv = map[string]string{"PEBBLE_API_TOKEN": apiToken}
	}

	for attempt := 0; attempt < 2; attempt++ {
		gen := lane.sessionGen.Load()
		var sess state.SessionRecord
		if h.store != nil {
			var sErr error
			sess, sErr = h.store.EnsureActiveSession(bot.ID, bot.Harness, bot.Model)
			if sErr != nil {
				return nil, sErr
			}
			if userMsgID != "" && sess.ID != queuedSessionID {
				_ = h.store.ReassignMessageSession(userMsgID, sess.ID, sess.TurnCount+1)
				queuedSessionID = sess.ID
			}
		}
		turnNum := sess.TurnCount + 1
		var pendingReceipts []state.RoutineRun
		if h.store != nil {
			pendingReceipts, _ = h.store.ListPendingReceipts(bot.ID, 5)
		}
		effectivePrompt := h.buildTurnPrompt(bot, prompt, sess.TurnCount == 0, platPath, pendingReceipts)

		var toolMu sync.Mutex
		var toolOrder []string
		toolCounts := make(map[string]int)

		var onQuestion func(context.Context, harness.QuestionRequest) (harness.QuestionResponse, error)
		var onPermission func(context.Context, harness.PermissionRequest) (harness.PermissionResponse, error)
		if isInteractiveQuestionSource(opts) {
			onQuestion = func(qCtx context.Context, req harness.QuestionRequest) (harness.QuestionResponse, error) {
				return h.AskQuestion(qCtx, bot, req)
			}
			onPermission = func(pCtx context.Context, req harness.PermissionRequest) (harness.PermissionResponse, error) {
				return h.RequestApproval(pCtx, bot, bot.EffectivePermissionMode(), req)
			}
		}

		lane.turnSentCount.Store(0)
		lane.turnMu.Lock()
		lane.activeSessionID = sess.ID
		lane.activeTurn = turnNum
		lane.turnLastMsgID = ""
		lane.turnMu.Unlock()

		res, err := runner.RunTurn(ctx, harness.RunOptions{
			Bot:                      bot,
			Prompt:                   effectivePrompt,
			Attachments:              attachments,
			SessionID:                sess.RuntimeSessionID,
			PermissionMode:           bot.EffectivePermissionMode(),
			PlatformInstructionsPath: platPath,
			MCPCommand:               mcpCmd,
			MCPEnv:                   mcpEnv,
			ExtraEnv:                 extraEnv,
			OnText:                   opts.OnText,
			OnQuestion:               onQuestion,
			OnPermission:             onPermission,
			OnToolUse: func(toolName string) {
				if toolName != "" {
					toolMu.Lock()
					if toolCounts[toolName] == 0 {
						toolOrder = append(toolOrder, toolName)
					}
					toolCounts[toolName]++
					toolMu.Unlock()
				}
				h.broadcast(Event{
					Type:  "tool_use",
					BotID: bot.ID,
					Payload: map[string]any{
						"botId": bot.ID,
						"tool":  toolName,
					},
				})
				if opts.OnToolUse != nil {
					opts.OnToolUse(toolName)
				}
			},
		})

		lane.turnMu.Lock()
		lane.activeSessionID = ""
		lane.activeTurn = 0
		lane.turnMu.Unlock()

		toolMu.Lock()
		tools := formatToolCounts(toolOrder, toolCounts)
		toolMu.Unlock()

		if err != nil {
			resetMidFlight := gen != lane.sessionGen.Load()
			if resetMidFlight {
				if res != nil && res.SessionID != "" && h.store != nil {
					_, _ = h.store.RecordTurnDetail(sess.ID, state.TurnRecordUpdate{
						RuntimeSessionID: res.SessionID,
						Harness:          bot.Harness,
						Model:            bot.Model,
						PromptTitle:      prompt,
					})
				}
				if errors.Is(ctx.Err(), context.Canceled) {
					h.appendMessage(state.ChatMessage{
						BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
						Role: "system", From: "system", Text: "Stopped",
					})
				}
				return nil, err
			}

			if errors.Is(ctx.Err(), context.Canceled) {
				if res != nil && res.SessionID != "" && res.SessionID != sess.RuntimeSessionID && h.store != nil {
					_, _ = h.store.RecordTurnDetail(sess.ID, state.TurnRecordUpdate{
						RuntimeSessionID: res.SessionID,
						Harness:          bot.Harness,
						Model:            bot.Model,
						PromptTitle:      prompt,
					})
				}
				if res != nil && strings.TrimSpace(res.Text) != "" {
					h.appendInternalMessage(state.ChatMessage{
						BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
						From: "bot:" + bot.ID, Text: res.Text,
					})
				}
				h.appendMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
					Role: "system", From: "system", Text: "Stopped",
				})
				return nil, err
			}

			// D2-A: Fatal session corruption/overflow on an existing session -> archive & retry once cleanly.
			if attempt == 0 && h.store != nil && sess.RuntimeSessionID != "" && isFatalSessionError(err) {
				if res != nil && strings.TrimSpace(res.Text) != "" {
					h.appendInternalMessage(state.ChatMessage{
						BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
						From: "bot:" + bot.ID, Text: res.Text,
					})
				}
				h.appendMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
					Role: "system", From: "error",
					Text: fmt.Sprintf("Execution failed (%s): %v", firstLineSummary(prompt, 80), err),
				})
				_, _, _ = h.store.ArchiveSessionByID(sess.ID, "error", err.Error(), "")
				h.appendMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum,
					Role: "system", From: "system",
					Text: fmt.Sprintf("Session error detected; archived previous session (%s) and retrying in a new session.", sess.ID),
				})
				h.emitStatus(lane)
				continue
			}

			if res != nil && res.SessionID != "" && res.SessionID != sess.RuntimeSessionID && h.store != nil {
				_, _ = h.store.RecordTurnDetail(sess.ID, state.TurnRecordUpdate{
					RuntimeSessionID: res.SessionID,
					Harness:          bot.Harness,
					Model:            bot.Model,
					PromptTitle:      prompt,
				})
			}
			if res != nil && strings.TrimSpace(res.Text) != "" {
				h.appendInternalMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
					From: "bot:" + bot.ID, Text: res.Text,
				})
			}
			h.appendMessage(state.ChatMessage{
				BotID: bot.ID, SessionID: sess.ID, Turn: turnNum, Tools: tools,
				Role: "system", From: "error", Text: fmt.Sprintf("Execution failed: %v", err),
			})
			return nil, err
		}

		if h.store != nil && sess.ID != "" {
			if _, saveErr := h.store.RecordTurnDetail(sess.ID, state.TurnRecordUpdate{
				RuntimeSessionID: res.SessionID,
				Harness:          bot.Harness,
				Model:            bot.Model,
				PromptTitle:      prompt,
				CostUSD:          res.Usage.CostUSD,
				InputTokens:      res.Usage.InputTokens,
				IncrementTurn:    true,
			}); saveErr != nil {
				return res, fmt.Errorf("turn succeeded but failed to persist session: %w", saveErr)
			}
		}
		if h.store != nil && len(pendingReceipts) > 0 {
			receiptIDs := make([]string, 0, len(pendingReceipts))
			for _, pr := range pendingReceipts {
				receiptIDs = append(receiptIDs, pr.ID)
			}
			_ = h.store.MarkReceiptsInjected(receiptIDs)
		}

		res.Tools = tools
		if opts.FromBotID != "" {
			if strings.TrimSpace(res.Text) != "" {
				h.appendMessage(state.ChatMessage{
					BotID:        bot.ID,
					SessionID:    sess.ID,
					Turn:         turnNum,
					Role:         "bot_out",
					From:         "bot:" + bot.ID,
					To:           "bot:" + opts.FromBotID,
					Text:         res.Text,
					Tools:        tools,
					DurationMs:   res.DurationMs,
					CostUSD:      res.Usage.CostUSD,
					InputTokens:  res.Usage.InputTokens,
					OutputTokens: res.Usage.OutputTokens,
				})
			}
		} else {
			if strings.TrimSpace(res.Text) != "" {
				h.appendInternalMessage(state.ChatMessage{
					BotID:        bot.ID,
					SessionID:    sess.ID,
					Turn:         turnNum,
					From:         "bot:" + bot.ID,
					Text:         res.Text,
					Tools:        tools,
					DurationMs:   res.DurationMs,
					CostUSD:      res.Usage.CostUSD,
					InputTokens:  res.Usage.InputTokens,
					OutputTokens: res.Usage.OutputTokens,
				})
			}
			if lane.turnSentCount.Load() > 0 {
				lane.turnMu.Lock()
				lastMsgID := lane.turnLastMsgID
				lane.turnMu.Unlock()
				h.updateMessageMeta(lastMsgID, tools, res.DurationMs, res.Usage.CostUSD, res.Usage.InputTokens, res.Usage.OutputTokens)
			} else if strings.TrimSpace(res.Text) != "" && isUserDirectSource(opts) {
				h.appendMessage(state.ChatMessage{
					BotID:        bot.ID,
					SessionID:    sess.ID,
					Turn:         turnNum,
					Role:         "assistant",
					From:         "bot:" + bot.ID,
					Text:         res.Text,
					Tools:        tools,
					DurationMs:   res.DurationMs,
					CostUSD:      res.Usage.CostUSD,
					InputTokens:  res.Usage.InputTokens,
					OutputTokens: res.Usage.OutputTokens,
				})
			}
		}

		lane.rotateMu.Lock()
		doRotate := lane.pendingRotate
		handoff := lane.pendingHandoff
		lane.pendingRotate = false
		lane.pendingHandoff = ""
		lane.rotateMu.Unlock()

		if doRotate && h.store != nil && gen == lane.sessionGen.Load() {
			if _, archived, _ := h.store.ArchiveSessionByID(sess.ID, "reset", "", handoff); archived {
				h.clearSessionAllow(bot.ID)
				_ = h.store.DismissPendingReceipts(bot.ID)
				h.appendMessage(state.ChatMessage{
					BotID: bot.ID, SessionID: sess.ID, Turn: turnNum,
					Role: "system", From: "system",
					Text: fmt.Sprintf("Session archived (%s). The next turn will start a new session.", sess.ID),
				})
			}
		}
		return res, nil
	}
	return nil, fmt.Errorf("bot %q exhausted retries", bot.ID)
}
