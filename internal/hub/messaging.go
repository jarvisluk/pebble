package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"fmt"
	"regexp"
	"strings"
)

type SendOptions struct {
	Source    string
	FromBotID string
	Hop       int
	MediaIDs  []string
	OnText    func(chunk string)
	OnToolUse func(toolName string)
}

var askCmdRe = regexp.MustCompile(`(?s)^/ask\s+([a-zA-Z0-9_-]+)\s+(.+)$`)

var mentionBotRe = regexp.MustCompile(`(?s)^@([A-Za-z0-9_.-]+)\s+(.+)$`)

func (h *Hub) appendMessage(msg state.ChatMessage) state.ChatMessage {
	var (
		saved state.ChatMessage
		err   error
	)
	if h.store != nil {
		saved, err = h.store.AppendMessage(msg)
	} else if h.messages != nil {
		saved, err = h.messages.Append(msg)
	} else {
		return msg
	}
	if err != nil {
		return msg
	}
	h.broadcast(Event{
		Type:    "message",
		BotID:   saved.BotID,
		Payload: saved,
	})
	return saved
}

func (h *Hub) updateMessageMeta(msgID string, tools []string, durationMs int64, costUSD float64, inTok, outTok int) {
	if msgID == "" || h.store == nil {
		return
	}
	updated, ok, err := h.store.UpdateMessageMeta(msgID, tools, durationMs, costUSD, inTok, outTok)
	if err != nil || !ok {
		return
	}
	h.broadcast(Event{
		Type:    "message_updated",
		BotID:   updated.BotID,
		Payload: updated,
	})
}

func (h *Hub) appendInternalMessage(msg state.ChatMessage) state.ChatMessage {
	msg.Role = "internal"
	if h.store != nil {
		if saved, err := h.store.AppendMessage(msg); err == nil {
			return saved
		}
		return msg
	}
	if h.messages != nil {
		if saved, err := h.messages.Append(msg); err == nil {
			return saved
		}
	}
	return msg
}

func (h *Hub) CollabMessages(botID, peerID string, limit int) ([]state.ChatMessage, error) {
	h.lanesMu.RLock()
	_, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found", botID)
	}
	if strings.TrimSpace(peerID) == "" {
		return nil, fmt.Errorf("peerId is required")
	}
	if h.store != nil {
		return h.store.ListCollabMessages(botID, peerID, limit)
	}
	if h.messages == nil {
		return []state.ChatMessage{}, nil
	}
	return h.messages.ListCollab(botID, peerID, limit)
}

func (h *Hub) SendOutboundMessage(botID, text string, images []OutboundImage, files ...OutboundFile) (state.ChatMessage, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	laneBot := config.BotConfig{}
	if ok {
		laneBot = lane.bot
	}
	h.lanesMu.RUnlock()
	if !ok {
		return state.ChatMessage{}, fmt.Errorf("bot %q not found", botID)
	}

	if strings.TrimSpace(text) == "" && len(images) == 0 && len(files) == 0 {
		return state.ChatMessage{}, fmt.Errorf("text, images, or files is required")
	}

	var media []state.MessageMedia
	for _, img := range images {
		m, err := h.SaveOutboundImage(laneBot.Workspace, img)
		if err != nil {
			return state.ChatMessage{}, err
		}
		media = append(media, m)
	}
	for _, f := range files {
		m, err := h.SaveOutboundFile(laneBot.Workspace, f)
		if err != nil {
			return state.ChatMessage{}, err
		}
		media = append(media, m)
	}

	lane.turnMu.Lock()
	sessID := lane.activeSessionID
	turn := lane.activeTurn
	lane.turnMu.Unlock()

	if sessID == "" && h.store != nil {
		if sess, err := h.store.EnsureActiveSession(botID, laneBot.Harness, laneBot.Model); err == nil {
			sessID = sess.ID
			turn = sess.TurnCount
		}
	}

	saved := h.appendMessage(state.ChatMessage{
		BotID:     botID,
		SessionID: sessID,
		Turn:      turn,
		Role:      "assistant",
		From:      "bot:" + botID,
		Text:      text,
		Media:     media,
	})
	lane.turnMu.Lock()
	lane.turnLastMsgID = saved.ID
	lane.turnMu.Unlock()
	lane.turnSentCount.Add(1)
	return saved, nil
}

func (h *Hub) Messages(botID string, limit int) ([]state.ChatMessage, error) {
	h.lanesMu.RLock()
	_, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found", botID)
	}
	if h.store != nil {
		return h.store.ListMessages(botID, limit)
	}
	if h.messages == nil {
		return []state.ChatMessage{}, nil
	}
	return h.messages.List(botID, limit)
}

func (h *Hub) HandleInput(ctx context.Context, botID, rawInput string, opts SendOptions) (*harness.TurnResult, error) {
	input := strings.TrimSpace(rawInput)
	if input == "" && len(opts.MediaIDs) == 0 {
		rec, _ := h.store.Get(botID)
		return &harness.TurnResult{BotID: botID, SessionID: rec.SessionID}, nil
	}

	switch input {
	case "/status":
		sts, err := h.Status(botID)
		if err != nil {
			return nil, err
		}
		st := sts[0]
		sessID := "(not started yet)"
		turns := 0
		cost := 0.0
		if st.Session != nil {
			sessID = st.Session.SessionID
			turns = st.Session.TurnCount
			cost = st.Session.TotalCostUSD
		}
		stateStr := "idle"
		if st.Busy {
			stateStr = "busy"
		}
		modelStr := st.Model
		if modelStr == "" {
			modelStr = "(opencode default)"
		}
		text := fmt.Sprintf(
			"Bot: %s (%s)\nWorkspace: %s\nModel: %s\nSession: %s\nTurns: %d | Cost: $%.4f\nQueue: %s (pending: %d)",
			st.Name, st.ID, st.Workspace, modelStr, sessID, turns, cost, stateStr, st.Queued,
		)
		return &harness.TurnResult{BotID: botID, SessionID: sessID, Text: text}, nil

	case "/reset":
		prev, existed, err := h.ResetSession(botID)
		if err != nil {
			return nil, err
		}
		text := "No active session to reset."
		if existed {
			text = fmt.Sprintf("Session reset (previous: %s). Workspace & memory/ are preserved.", prev.SessionID)
		}
		return &harness.TurnResult{BotID: botID, Text: text}, nil

	case "/bots":
		sts, _ := h.Status("")
		var lines []string
		for _, st := range sts {
			tag := "new"
			if st.Session != nil {
				tag = fmt.Sprintf("turns=%d", st.Session.TurnCount)
			}
			desc := st.Description
			if desc == "" {
				desc = "no description"
			}
			lines = append(lines, fmt.Sprintf("- %s (%s): %s [%s]", st.ID, st.Name, desc, tag))
		}
		rec, _ := h.store.Get(botID)
		return &harness.TurnResult{
			BotID:     botID,
			SessionID: rec.SessionID,
			Text:      strings.Join(lines, "\n"),
		}, nil
	}

	if input == "/mode" || strings.HasPrefix(input, "/mode ") || strings.HasPrefix(input, "/mode\t") {
		arg := strings.TrimSpace(strings.TrimPrefix(input, "/mode"))
		bot, ok := h.GetBot(botID)
		if !ok {
			return nil, fmt.Errorf("bot %q not found", botID)
		}
		runner, err := h.registry.Get(bot.Harness)
		if err != nil {
			return nil, err
		}
		caps := harness.CapabilitiesOf(runner)
		rec, _ := h.store.Get(botID)
		if arg == "" {
			text := fmt.Sprintf("Current permission mode: %s\nAvailable modes: %s", bot.EffectivePermissionMode(), strings.Join(caps.Modes, ", "))
			return &harness.TurnResult{BotID: botID, SessionID: rec.SessionID, Text: text}, nil
		}
		norm, err := config.NormalizePermissionMode(arg)
		if err != nil || norm == "" {
			return nil, fmt.Errorf("invalid permission mode %q (available: %s)", arg, strings.Join(caps.Modes, ", "))
		}
		st, err := h.UpdateBot(botID, BotUpdate{PermissionMode: &norm})
		if err != nil {
			return nil, err
		}
		return &harness.TurnResult{
			BotID:     botID,
			SessionID: rec.SessionID,
			Text:      fmt.Sprintf("Switched %s permission mode to %s.", st.Name, st.PermissionMode),
		}, nil
	}

	if m := askCmdRe.FindStringSubmatch(input); len(m) == 3 {
		targetID := m[1]
		forwarded := m[2]
		res, err := h.MessageBot(ctx, botID, targetID, forwarded, opts.Hop)
		if err != nil {
			return nil, err
		}
		res.Text = fmt.Sprintf("[%s]\n%s", targetID, res.Text)
		return res, nil
	}

	if m := mentionBotRe.FindStringSubmatch(input); len(m) == 3 {
		targetID := m[1]
		forwarded := m[2]
		if targetID != botID {
			if _, exists := h.GetBot(targetID); exists {
				res, err := h.MessageBot(ctx, botID, targetID, forwarded, opts.Hop)
				if err != nil {
					return nil, err
				}
				res.Text = fmt.Sprintf("[%s]\n%s", targetID, res.Text)
				return res, nil
			}
		}
	}

	return h.Send(ctx, botID, input, opts)
}

// MessageBot sends a message from one bot to another with loop-protection hop limits
// and logs the exchange in both bots' chat timelines so users can observe collaboration.
func (h *Hub) MessageBot(ctx context.Context, fromBotID, toBotID, message string, hop int) (*harness.TurnResult, error) {
	if fromBotID == toBotID {
		return nil, fmt.Errorf("bot %q cannot message itself", fromBotID)
	}
	h.hopsMu.Lock()
	if active, ok := h.hops[fromBotID]; ok && active > hop {
		hop = active
	}
	h.hopsMu.Unlock()
	if hop >= MaxBotHopDepth {
		return nil, fmt.Errorf("bot-to-bot hop limit reached (%d); refusing to avoid runaway loop", MaxBotHopDepth)
	}
	if _, ok := h.GetBot(toBotID); !ok {
		return nil, fmt.Errorf("target bot %q not found", toBotID)
	}

	// Record outgoing collaboration message on sender's timeline.
	var fromSessID string
	var fromTurn int
	if fromBotID != "" {
		if _, ok := h.GetBot(fromBotID); ok {
			if h.store != nil {
				if rec, ok := h.store.Get(fromBotID); ok {
					fromSessID = rec.ID
					fromTurn = rec.TurnCount + 1
				}
			}
			h.appendMessage(state.ChatMessage{
				BotID:     fromBotID,
				SessionID: fromSessID,
				Turn:      fromTurn,
				Role:      "bot_out",
				From:      "bot:" + fromBotID,
				To:        "bot:" + toBotID,
				Text:      message,
			})
		}
	}

	wrappedPrompt := fmt.Sprintf("[Message from bot:%s]\n%s", fromBotID, message)
	res, err := h.Send(ctx, toBotID, wrappedPrompt, SendOptions{
		Source:    "bot:" + fromBotID,
		FromBotID: fromBotID,
		Hop:       hop + 1,
	})
	if err != nil {
		return nil, err
	}

	// Record reply back on sender's timeline as well so sender's chat shows the full round-trip.
	if fromBotID != "" {
		if _, ok := h.GetBot(fromBotID); ok {
			h.appendMessage(state.ChatMessage{
				BotID:      fromBotID,
				SessionID:  fromSessID,
				Turn:       fromTurn,
				Role:       "bot_in",
				From:       "bot:" + toBotID,
				To:         "bot:" + fromBotID,
				Text:       res.Text,
				DurationMs: res.DurationMs,
				CostUSD:    res.Usage.CostUSD,
			})
		}
	}

	return res, nil
}

// Send enqueues a turn on the bot's single-lane serialized goroutine.
// Different bots execute concurrently without blocking each other.
func (h *Hub) Send(ctx context.Context, botID, prompt string, opts SendOptions) (*harness.TurnResult, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	laneBot := config.BotConfig{}
	if ok {
		laneBot = lane.bot
	}
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found", botID)
	}

	role := "user"
	from := opts.Source
	to := ""
	if from == "" {
		from = "user"
	}
	if opts.FromBotID != "" {
		role = "bot_in"
		from = "bot:" + opts.FromBotID
		to = "bot:" + botID
	}

	var queuedSessID string
	var queuedTurn int
	if h.store != nil {
		if sess, err := h.store.EnsureActiveSession(botID, laneBot.Harness, laneBot.Model); err == nil {
			queuedSessID = sess.ID
			queuedTurn = sess.TurnCount + 1
		}
	}
	var media []state.MessageMedia
	var atts []harness.Attachment
	if len(opts.MediaIDs) > 0 {
		h.inboundAttsMu.Lock()
		for _, id := range opts.MediaIDs {
			if entry, ok := h.inboundAtts[id]; ok {
				media = append(media, entry.media)
				atts = append(atts, entry.att)
			}
		}
		h.inboundAttsMu.Unlock()
	}

	userMsg := h.appendMessage(state.ChatMessage{
		BotID:     botID,
		SessionID: queuedSessID,
		Turn:      queuedTurn,
		Role:      role,
		From:      from,
		To:        to,
		Text:      prompt,
		Media:     media,
	})

	job := turnJob{
		ctx:             ctx,
		prompt:          prompt,
		attachments:     atts,
		opts:            opts,
		userMsgID:       userMsg.ID,
		queuedSessionID: queuedSessID,
		respCh:          make(chan turnResponse, 1),
	}

	lane.queued.Add(1)
	h.emitStatus(lane)
	select {
	case lane.ch <- job:
	case <-ctx.Done():
		lane.queued.Add(-1)
		h.emitStatus(lane)
		return nil, ctx.Err()
	case <-lane.stopCh:
		lane.queued.Add(-1)
		return nil, fmt.Errorf("hub is shutting down")
	}

	select {
	case resp := <-job.respCh:
		return resp.res, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Interrupt cancels the bot's running turn and any active isolated tasks, if any.
func (h *Hub) Interrupt(botID string) (bool, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	h.lanesMu.RUnlock()
	if !ok {
		return false, fmt.Errorf("bot %q not found", botID)
	}
	lane.cancelMu.Lock()
	cancel := lane.cancel
	lane.cancelMu.Unlock()
	interrupted := false
	if cancel != nil {
		cancel()
		interrupted = true
	}
	if h.cancelIsolatedByBot(botID) > 0 {
		interrupted = true
	}
	if interrupted {
		h.desktop.CancelTakeoversByBot(botID)
		h.CancelApprovalsByBot(botID)
	}
	return interrupted, nil
}
