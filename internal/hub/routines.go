package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

type IsolatedOptions struct {
	Source         string
	Harness        string
	Model          string
	PermissionMode string
	MCPRole        string
	TimeoutSec     int
	ExtraEnv       map[string]string
	OnToolUse      func(toolName string)
}

type RoutinePatch struct {
	Name               *string `json:"name,omitempty"`
	EveryMinutes       *int    `json:"everyMinutes,omitempty"`
	DailyAt            *string `json:"dailyAt,omitempty"`
	Cron               *string `json:"cron,omitempty"`
	WebhookTrigger     *bool   `json:"webhookTrigger,omitempty"`
	WebhookSecret      *string `json:"webhookSecret,omitempty"`
	ClearWebhookSecret *bool   `json:"clearWebhookSecret,omitempty"`
	WebhookURL         *string `json:"webhookUrl,omitempty"`
	Prompt             *string `json:"prompt,omitempty"`
	Model              *string `json:"model,omitempty"`
	MainDelivery       *string `json:"mainDelivery,omitempty"`
	PermissionMode     *string `json:"permissionMode,omitempty"`
	Enabled            *bool   `json:"enabled,omitempty"`
}

// RunIsolated runs a single ephemeral turn for botID without touching or blocking
// the bot's active main session.
func (h *Hub) RunIsolated(ctx context.Context, botID, prompt string, opts IsolatedOptions) (*harness.TurnResult, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	var bot config.BotConfig
	var apiToken string
	var mcpCmd []string
	if ok {
		bot = lane.bot
		apiToken = h.cfg.HTTP.APIToken
		mcpCmd = config.MCPCommand(h.cfg, bot.ID)
	}
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}

	if hk := strings.TrimSpace(opts.Harness); hk != "" && hk != bot.Harness {
		bot.Harness = hk
		if strings.TrimSpace(opts.Model) == "" && bot.ModelInherited {
			h.lanesMu.RLock()
			bot.Model = h.cfg.DefaultModelFor(bot.Harness)
			h.lanesMu.RUnlock()
		}
	}
	if m := strings.TrimSpace(opts.Model); m != "" {
		bot.Model = m
		bot.ModelInherited = false
	}
	if opts.TimeoutSec > 0 {
		bot.TimeoutSec = opts.TimeoutSec
	}
	permMode := bot.EffectivePermissionMode()
	if norm, err := config.NormalizePermissionMode(opts.PermissionMode); err == nil && norm != "" {
		permMode = norm
		bot.PermissionMode = norm
		bot.AutoApprove = nil
	}

	runner, err := h.registry.Get(bot.Harness)
	if err != nil {
		return nil, err
	}

	h.activeEphemeralMu.Lock()
	h.activeEphemeral[bot.Harness]++
	h.activeEphemeralMu.Unlock()
	defer func() {
		h.activeEphemeralMu.Lock()
		if h.activeEphemeral[bot.Harness] > 0 {
			h.activeEphemeral[bot.Harness]--
		}
		h.activeEphemeralMu.Unlock()
		h.flushPendingReload(bot.Harness)
	}()

	platPath := h.platformInstructionsPath()
	effectivePrompt := prompt
	if opts.Source != "memory_dream" && opts.Source != "memory_flush" && !strings.HasPrefix(strings.TrimSpace(prompt), "/") {
		if snap := h.buildMemorySnapshot(ctx, bot, min(h.memorySnapshotMaxChars(), 4000)); snap != "" {
			effectivePrompt = snap + "\n\n" + effectivePrompt
		}
	}
	if bot.Harness == "dsh" && platPath != "" {
		if raw, err := os.ReadFile(platPath); err == nil && len(raw) > 0 {
			effectivePrompt = strings.TrimSpace(string(raw)) + "\n\n" + effectivePrompt
		}
	}

	extraEnv := map[string]string{
		"PEBBLE_URL":    h.cfg.HTTP.BaseURL(),
		"PEBBLE_BOT_ID": bot.ID,
		"PEBBLE_HOP":    "0",
	}
	if h.cfg.ConfigPath != "" {
		extraEnv["PEBBLE_CONFIG"] = h.cfg.ConfigPath
	}
	if h.memoryDisableClaudeAutoMemory() {
		if _, overridden := bot.Env["CLAUDE_CODE_DISABLE_AUTO_MEMORY"]; !overridden {
			extraEnv["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] = "1"
		}
	}
	var mcpEnv map[string]string
	if apiToken != "" {
		extraEnv["PEBBLE_API_TOKEN"] = apiToken
		mcpEnv = map[string]string{"PEBBLE_API_TOKEN": apiToken}
	}
	if role := strings.TrimSpace(opts.MCPRole); role != "" {
		extraEnv["PEBBLE_MCP_ROLE"] = role
		if mcpEnv == nil {
			mcpEnv = make(map[string]string, 1)
		}
		mcpEnv["PEBBLE_MCP_ROLE"] = role
	}
	for k, v := range h.botBrowserEnv(bot) {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}
	for k, v := range opts.ExtraEnv {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	cancelID := h.registerIsolatedCancel(bot.ID, cancel)
	defer func() {
		cancel()
		h.unregisterIsolatedCancel(bot.ID, cancelID)
	}()

	var toolMu sync.Mutex
	var toolOrder []string
	toolCounts := make(map[string]int)

	res, err := runner.RunTurn(runCtx, harness.RunOptions{
		Bot:                      bot,
		Prompt:                   effectivePrompt,
		SessionID:                "",
		Ephemeral:                true,
		PermissionMode:           permMode,
		PlatformInstructionsPath: platPath,
		MCPCommand:               mcpCmd,
		MCPEnv:                   mcpEnv,
		ExtraEnv:                 extraEnv,
		OnToolUse: func(toolName string) {
			if toolName != "" {
				toolMu.Lock()
				if toolCounts[toolName] == 0 {
					toolOrder = append(toolOrder, toolName)
				}
				toolCounts[toolName]++
				toolMu.Unlock()
			}
			if opts.OnToolUse != nil {
				opts.OnToolUse(toolName)
			}
		},
	})
	if err != nil {
		return res, err
	}
	toolMu.Lock()
	res.Tools = formatToolCounts(toolOrder, toolCounts)
	toolMu.Unlock()
	return res, nil
}

// AppendRoutineReceipt writes a routine receipt onto the bot's main timeline
// without waking the main LLM session.
func (h *Hub) AppendRoutineReceipt(botID, routineName, text string, durMs int64, costUSD float64, inTok, outTok int) state.ChatMessage {
	var sessID string
	var turn int
	if h.store != nil {
		if rec, ok := h.store.Get(botID); ok {
			sessID = rec.ID
			turn = rec.TurnCount
		}
	}
	return h.appendMessage(state.ChatMessage{
		BotID:        botID,
		SessionID:    sessID,
		Turn:         turn,
		Role:         "assistant",
		From:         "routine:" + routineName,
		Text:         text,
		DurationMs:   durMs,
		CostUSD:      costUSD,
		InputTokens:  inTok,
		OutputTokens: outTok,
	})
}

func (h *Hub) ListRoutines(botID string) ([]config.ScheduleConfig, error) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	lane, ok := h.lanes[botID]
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	out := make([]config.ScheduleConfig, len(lane.bot.Schedules))
	copy(out, lane.bot.Schedules)
	return out, nil
}

// CreateRoutine adds a new routine on botID (auto-assigning a unique ID if omitted).
func (h *Hub) CreateRoutine(botID string, raw config.ScheduleConfig) (config.ScheduleConfig, error) {
	return h.saveRoutine(botID, raw, "", true)
}

// UpsertRoutine creates or updates a routine by ID (or by matching name when ID is omitted).
func (h *Hub) UpsertRoutine(botID string, raw config.ScheduleConfig) (config.ScheduleConfig, error) {
	return h.saveRoutine(botID, raw, strings.TrimSpace(raw.ID), false)
}

// UpdateRoutine applies a partial patch to an existing routine.
func (h *Hub) UpdateRoutine(botID, routineID string, patch RoutinePatch) (config.ScheduleConfig, error) {
	routineID = strings.TrimSpace(routineID)
	if routineID == "" {
		return config.ScheduleConfig{}, fmt.Errorf("routine id is required: %w", ErrRoutineNotFound)
	}

	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.RUnlock()
		return config.ScheduleConfig{}, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	var base config.ScheduleConfig
	found := false
	for _, sc := range lane.bot.Schedules {
		if sc.EffectiveID() == routineID {
			base = sc
			found = true
			break
		}
	}
	h.lanesMu.RUnlock()
	if !found {
		return config.ScheduleConfig{}, fmt.Errorf("routine %q not found: %w", routineID, ErrRoutineNotFound)
	}

	if patch.Name != nil {
		base.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.EveryMinutes != nil {
		base.EveryMinutes = *patch.EveryMinutes
	}
	if patch.DailyAt != nil {
		base.DailyAt = strings.TrimSpace(*patch.DailyAt)
	}
	if patch.Cron != nil {
		base.Cron = strings.TrimSpace(*patch.Cron)
	}
	if patch.WebhookTrigger != nil {
		base.WebhookTrigger = *patch.WebhookTrigger
	}
	if patch.ClearWebhookSecret != nil && *patch.ClearWebhookSecret {
		base.WebhookSecret = ""
	} else if patch.WebhookSecret != nil && strings.TrimSpace(*patch.WebhookSecret) != "" {
		base.WebhookSecret = strings.TrimSpace(*patch.WebhookSecret)
	}
	if patch.WebhookURL != nil {
		base.WebhookURL = strings.TrimSpace(*patch.WebhookURL)
	}
	if patch.Prompt != nil {
		base.Prompt = strings.TrimSpace(*patch.Prompt)
	}
	if patch.Model != nil {
		base.Model = strings.TrimSpace(*patch.Model)
	}
	if patch.MainDelivery != nil {
		base.MainDelivery = strings.TrimSpace(*patch.MainDelivery)
	}
	if patch.PermissionMode != nil {
		base.PermissionMode = strings.TrimSpace(*patch.PermissionMode)
	}
	if patch.Enabled != nil {
		v := *patch.Enabled
		base.Enabled = &v
	}
	return h.saveRoutine(botID, base, routineID, false)
}

func (h *Hub) saveRoutine(botID string, raw config.ScheduleConfig, targetID string, createOnly bool) (config.ScheduleConfig, error) {
	clean := cleanScheduleInput(raw)
	if err := clean.Validate(); err != nil {
		return config.ScheduleConfig{}, err
	}

	h.lanesMu.Lock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.Unlock()
		return config.ScheduleConfig{}, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}

	curExpanded := make([]config.ScheduleConfig, len(lane.bot.Schedules))
	copy(curExpanded, lane.bot.Schedules)

	idx := -1
	if createOnly {
		if clean.ID != "" {
			for _, sc := range curExpanded {
				if sc.EffectiveID() == clean.ID {
					h.lanesMu.Unlock()
					return config.ScheduleConfig{}, fmt.Errorf("routine %q already exists", clean.ID)
				}
			}
		}
	} else {
		matchID := strings.TrimSpace(targetID)
		if matchID == "" {
			matchID = clean.ID
		}
		if matchID != "" {
			for i, sc := range curExpanded {
				if sc.EffectiveID() == matchID {
					idx = i
					break
				}
			}
			if targetID != "" && idx == -1 && clean.ID == "" {
				h.lanesMu.Unlock()
				return config.ScheduleConfig{}, fmt.Errorf("routine %q not found: %w", targetID, ErrRoutineNotFound)
			}
		} else {
			for i, sc := range curExpanded {
				if strings.EqualFold(strings.TrimSpace(sc.Name), clean.Name) {
					idx = i
					break
				}
			}
		}
		if idx >= 0 && clean.ID == "" {
			clean.ID = curExpanded[idx].EffectiveID()
		}
	}

	var prevExpanded config.ScheduleConfig
	if idx >= 0 {
		prevExpanded = curExpanded[idx]
	}

	nextRawList := make([]config.ScheduleConfig, len(curExpanded))
	copy(nextRawList, curExpanded)
	if idx >= 0 {
		nextRawList[idx] = clean
	} else {
		nextRawList = append(nextRawList, clean)
		idx = len(nextRawList) - 1
	}

	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(rawCfg *config.HubConfig) error {
			for i := range rawCfg.Bots {
				rb := &rawCfg.Bots[i]
				if rb.ID != botID {
					continue
				}
				normExisting := config.NormalizeSchedules(rb.Schedules)
				diskIdx := -1
				if idx < len(normExisting) && normExisting[idx].EffectiveID() == prevExpanded.EffectiveID() && prevExpanded.EffectiveID() != "" {
					diskIdx = idx
				} else if prevExpanded.EffectiveID() != "" {
					for j, ns := range normExisting {
						if ns.EffectiveID() == prevExpanded.EffectiveID() {
							diskIdx = j
							break
						}
					}
				}
				toDisk := clean
				if diskIdx >= 0 {
					toDisk = preserveRoutineEnvRefs(toDisk, rb.Schedules[diskIdx], prevExpanded)
					if toDisk.ID == "" {
						toDisk.ID = normExisting[diskIdx].EffectiveID()
					}
					rb.Schedules[diskIdx] = toDisk
				} else {
					normAll := config.NormalizeSchedules(append(append([]config.ScheduleConfig(nil), rb.Schedules...), toDisk))
					toDisk.ID = normAll[len(normAll)-1].ID
					rb.Schedules = append(rb.Schedules, toDisk)
				}
				return nil
			}
			return fmt.Errorf("bot %q is not in %s", botID, h.cfg.ConfigPath)
		})
		if err != nil {
			h.lanesMu.Unlock()
			return config.ScheduleConfig{}, fmt.Errorf("save config: %w", err)
		}
	}

	normalized := config.NormalizeSchedules(nextRawList)
	h.setBotSchedulesLocked(lane, normalized)
	saved := normalized[idx]
	h.lanesMu.Unlock()

	h.broadcast(Event{
		Type:    "routine_updated",
		BotID:   botID,
		Payload: map[string]any{"botId": botID, "routine": saved},
	})
	h.notifySchedulesChanged()
	return saved, nil
}

func (h *Hub) DeleteRoutine(botID, routineID string) error {
	routineID = strings.TrimSpace(routineID)
	if routineID == "" {
		return fmt.Errorf("routine %q not found: %w", routineID, ErrRoutineNotFound)
	}

	h.lanesMu.Lock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.Unlock()
		return fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	next := make([]config.ScheduleConfig, 0, len(lane.bot.Schedules))
	found := false
	for _, sc := range lane.bot.Schedules {
		if sc.EffectiveID() == routineID {
			found = true
			continue
		}
		next = append(next, sc)
	}
	if !found {
		h.lanesMu.Unlock()
		return fmt.Errorf("routine %q not found: %w", routineID, ErrRoutineNotFound)
	}

	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(rawCfg *config.HubConfig) error {
			for i := range rawCfg.Bots {
				rb := &rawCfg.Bots[i]
				if rb.ID != botID {
					continue
				}
				normExisting := config.NormalizeSchedules(rb.Schedules)
				filtered := make([]config.ScheduleConfig, 0, len(rb.Schedules))
				for j, sc := range rb.Schedules {
					if j < len(normExisting) && normExisting[j].EffectiveID() == routineID {
						continue
					}
					filtered = append(filtered, sc)
				}
				rb.Schedules = filtered
				return nil
			}
			return fmt.Errorf("bot %q is not in %s", botID, h.cfg.ConfigPath)
		})
		if err != nil {
			h.lanesMu.Unlock()
			return fmt.Errorf("save config: %w", err)
		}
	}

	h.setBotSchedulesLocked(lane, config.NormalizeSchedules(next))
	h.lanesMu.Unlock()

	h.broadcast(Event{
		Type:    "routine_updated",
		BotID:   botID,
		Payload: map[string]any{"botId": botID, "deletedId": routineID},
	})
	h.notifySchedulesChanged()
	return nil
}

func (h *Hub) setBotSchedulesLocked(lane *botLane, schedules []config.ScheduleConfig) {
	lane.bot.Schedules = schedules
	for i := range h.cfg.Bots {
		if h.cfg.Bots[i].ID == lane.bot.ID {
			h.cfg.Bots[i].Schedules = schedules
		}
	}
}

func cleanScheduleInput(raw config.ScheduleConfig) config.ScheduleConfig {
	raw.ID = strings.TrimSpace(raw.ID)
	raw.Name = strings.TrimSpace(raw.Name)
	raw.DailyAt = strings.TrimSpace(raw.DailyAt)
	raw.Cron = strings.TrimSpace(raw.Cron)
	raw.WebhookSecret = strings.TrimSpace(raw.WebhookSecret)
	raw.WebhookURL = strings.TrimSpace(raw.WebhookURL)
	raw.Prompt = strings.TrimSpace(raw.Prompt)
	raw.Model = strings.TrimSpace(raw.Model)
	raw.MainDelivery = raw.EffectiveMainDelivery()
	if rpm, err := config.NormalizeRoutinePermissionMode(raw.PermissionMode); err == nil {
		raw.PermissionMode = rpm
	}
	if raw.Enabled == nil {
		v := true
		raw.Enabled = &v
	}
	return raw
}

func preserveRoutineEnvRefs(next, curDisk, curExpanded config.ScheduleConfig) config.ScheduleConfig {
	if next.Model == curExpanded.Model && config.ContainsEnvRef(curDisk.Model) {
		next.Model = curDisk.Model
	}
	if next.WebhookSecret == curExpanded.WebhookSecret && config.ContainsEnvRef(curDisk.WebhookSecret) {
		next.WebhookSecret = curDisk.WebhookSecret
	}
	if next.WebhookURL == curExpanded.WebhookURL && config.ContainsEnvRef(curDisk.WebhookURL) {
		next.WebhookURL = curDisk.WebhookURL
	}
	return next
}

func (h *Hub) RecordRoutineRun(run state.RoutineRun) (state.RoutineRun, error) {
	if h.store == nil {
		return run, nil
	}
	return h.store.RecordRoutineRun(run)
}

func (h *Hub) ListRoutineRuns(botID, routineID string, limit int) ([]state.RoutineRun, error) {
	if _, ok := h.GetBot(botID); !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	if h.store == nil {
		return []state.RoutineRun{}, nil
	}
	return h.store.ListRoutineRuns(botID, routineID, limit)
}

func (h *Hub) LatestRoutineRun(botID, routineID string) (state.RoutineRun, bool) {
	if h.store == nil {
		return state.RoutineRun{}, false
	}
	return h.store.LatestRoutineRun(botID, routineID)
}

func (h *Hub) HasDailyRunOnDate(botID, routineID, localDate string) bool {
	if h.store == nil {
		return false
	}
	return h.store.HasDailyRunOnDate(botID, routineID, localDate)
}

func (h *Hub) MarkReceiptsInjected(ids []string) error {
	if h.store == nil {
		return nil
	}
	return h.store.MarkReceiptsInjected(ids)
}

func (h *Hub) BroadcastRoutineRun(run state.RoutineRun) {
	h.broadcast(Event{
		Type:    "routine_run",
		BotID:   run.BotID,
		Payload: run,
	})
}

func (h *Hub) registerIsolatedCancel(botID string, cancel context.CancelFunc) uint64 {
	id := h.isolatedSeq.Add(1)
	h.isolatedCancelsMu.Lock()
	m := h.isolatedCancels[botID]
	if m == nil {
		m = make(map[uint64]context.CancelFunc)
		h.isolatedCancels[botID] = m
	}
	m[id] = cancel
	h.isolatedCancelsMu.Unlock()
	return id
}

func (h *Hub) unregisterIsolatedCancel(botID string, id uint64) {
	h.isolatedCancelsMu.Lock()
	if m := h.isolatedCancels[botID]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(h.isolatedCancels, botID)
		}
	}
	h.isolatedCancelsMu.Unlock()
}

func (h *Hub) cancelIsolatedByBot(botID string) int {
	h.isolatedCancelsMu.Lock()
	m := h.isolatedCancels[botID]
	cancels := make([]context.CancelFunc, 0, len(m))
	for _, fn := range m {
		cancels = append(cancels, fn)
	}
	h.isolatedCancelsMu.Unlock()
	for _, fn := range cancels {
		fn()
	}
	return len(cancels)
}
