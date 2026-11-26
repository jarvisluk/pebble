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
	"strings"
)

type BotStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Color       string `json:"color,omitempty"`
	Workspace   string `json:"workspace"`
	Harness     string `json:"harness"`
	Model       string `json:"model,omitempty"`
	// ModelInherited: the model is the hub default for the bot's harness.
	ModelInherited   bool                 `json:"modelInherited"`
	PrivateSessions       bool                 `json:"privateSessions,omitempty"`
	DesktopTarget         string               `json:"desktopTarget,omitempty"`
	BrowserMode           string               `json:"browserMode,omitempty"`
	PermissionMode        string               `json:"permissionMode,omitempty"`
	CoordinatorCanApprove *bool                `json:"coordinatorCanApprove,omitempty"`
	ArchivedSessions      int                  `json:"archivedSessions,omitempty"`
	Busy             bool                 `json:"busy"`
	Queued           int64                `json:"queued"`
	Session          *state.SessionRecord `json:"session,omitempty"`
}

type BotDetail struct {
	BotStatus
	Instructions string `json:"instructions"`
}

type CreateBotRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Avatar          string `json:"avatar,omitempty"`
	Color           string `json:"color,omitempty"`
	Harness         string `json:"harness,omitempty"`
	Model           string `json:"model,omitempty"`
	Instructions    string `json:"instructions,omitempty"`
	PrivateSessions       bool   `json:"privateSessions,omitempty"`
	DesktopTarget         string `json:"desktopTarget,omitempty"`
	BrowserMode           string `json:"browserMode,omitempty"`
	PermissionMode        string `json:"permissionMode,omitempty"`
	CoordinatorCanApprove *bool  `json:"coordinatorCanApprove,omitempty"`
}

// BotUpdate changes a bot's settings; nil fields stay as they are. An empty
// Model means "inherit the hub default".
type BotUpdate struct {
	Name                  *string `json:"name,omitempty"`
	Description           *string `json:"description,omitempty"`
	Avatar                *string `json:"avatar,omitempty"`
	Color                 *string `json:"color,omitempty"`
	Harness               *string `json:"harness,omitempty"`
	Model                 *string `json:"model,omitempty"`
	Instructions          *string `json:"instructions,omitempty"`
	PrivateSessions       *bool   `json:"privateSessions,omitempty"`
	DesktopTarget         *string `json:"desktopTarget,omitempty"`
	BrowserMode           *string `json:"browserMode,omitempty"`
	PermissionMode        *string `json:"permissionMode,omitempty"`
	CoordinatorCanApprove *bool   `json:"coordinatorCanApprove,omitempty"`
}

func (h *Hub) BotName(botID string) string {
	if b, ok := h.GetBot(botID); ok {
		return b.Name
	}
	return botID
}

func (h *Hub) BotWorkspace(botID string) string {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	if lane := h.lanes[botID]; lane != nil {
		return lane.bot.Workspace
	}
	return ""
}

func (h *Hub) Bots() []config.BotConfig {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	out := make([]config.BotConfig, 0, len(h.order))
	for _, id := range h.order {
		out = append(out, h.lanes[id].bot)
	}
	return out
}

func (h *Hub) GetBot(botID string) (config.BotConfig, bool) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	lane, ok := h.lanes[botID]
	if !ok {
		return config.BotConfig{}, false
	}
	return lane.bot, true
}

func (h *Hub) Status(botID string) ([]BotStatus, error) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()

	ids := h.order
	if botID != "" {
		if _, ok := h.lanes[botID]; !ok {
			return nil, fmt.Errorf("bot %q not found", botID)
		}
		ids = []string{botID}
	}

	out := make([]BotStatus, 0, len(ids))
	for _, id := range ids {
		out = append(out, h.statusOfLocked(h.lanes[id]))
	}
	return out, nil
}

func (h *Hub) statusOfLocked(lane *botLane) BotStatus {
	var sessPtr *state.SessionRecord
	archivedCount := 0
	if h.store != nil {
		if rec, ok := h.store.Get(lane.bot.ID); ok {
			recCopy := rec
			sessPtr = &recCopy
		}
		archivedCount = h.store.ArchivedCount(lane.bot.ID)
	}
	return BotStatus{
		ID:               lane.bot.ID,
		Name:             lane.bot.Name,
		Description:      lane.bot.Description,
		Avatar:           lane.bot.Avatar,
		Color:            lane.bot.Color,
		Workspace:        lane.bot.Workspace,
		Harness:          lane.bot.Harness,
		Model:            lane.bot.Model,
		ModelInherited:   lane.bot.ModelInherited,
		PrivateSessions:  lane.bot.PrivateSessions,
		DesktopTarget:         lane.bot.DesktopTarget,
		BrowserMode:           lane.bot.BrowserMode,
		PermissionMode:        lane.bot.EffectivePermissionMode(),
		CoordinatorCanApprove: lane.bot.CoordinatorCanApprove,
		ArchivedSessions:      archivedCount,
		Busy:             lane.busy.Load(),
		Queued:           lane.queued.Load(),
		Session:          sessPtr,
	}
}

func (h *Hub) GetBotDetail(botID string) (*BotDetail, error) {
	sts, err := h.Status(botID)
	if err != nil {
		return nil, err
	}
	st := sts[0]
	raw, _ := os.ReadFile(filepath.Join(st.Workspace, "AGENTS.md"))
	return &BotDetail{
		BotStatus:    st,
		Instructions: string(raw),
	}, nil
}

// CreateBot dynamically provisions a new bot workspace, persists config/bots.json,
// and starts its dedicated serialized worker goroutine immediately.
func (h *Hub) CreateBot(req CreateBotRequest) (*BotStatus, error) {
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	avatar, err := config.NormalizeBotAvatar(req.Avatar)
	if err != nil {
		return nil, err
	}
	req.Avatar = avatar
	color, err := config.NormalizeBotColor(req.Color)
	if err != nil {
		return nil, err
	}
	req.Color = color
	req.Model = strings.TrimSpace(req.Model)
	if err := config.ValidateModelID(req.Model); err != nil {
		return nil, err
	}
	req.Harness = strings.TrimSpace(req.Harness)
	if req.Harness == "" {
		req.Harness = h.DefaultHarness()
	}
	runner, err := h.registry.Get(req.Harness)
	if err != nil {
		return nil, err
	}
	dt, err := config.NormalizeDesktopTarget(req.DesktopTarget)
	if err != nil {
		return nil, err
	}
	req.DesktopTarget = dt
	bm, err := config.NormalizeBrowserMode(req.BrowserMode)
	if err != nil {
		return nil, err
	}
	req.BrowserMode = bm
	pm, err := config.NormalizePermissionMode(req.PermissionMode)
	if err != nil {
		return nil, err
	}
	if pm != "" && !harness.CapabilitiesOf(runner).SupportsMode(pm) {
		return nil, fmt.Errorf("harness %q does not support permission mode %q", req.Harness, pm)
	}
	req.PermissionMode = pm
	req.Instructions = strings.TrimSpace(req.Instructions)

	if err := config.ValidateBotID(req.ID); err != nil {
		return nil, err
	}

	h.lanesMu.Lock()

	if _, exists := h.lanes[req.ID]; exists {
		h.lanesMu.Unlock()
		return nil, fmt.Errorf("bot %q already exists", req.ID)
	}

	bot := config.BotConfig{
		ID:              req.ID,
		Name:            req.Name,
		Description:     req.Description,
		Avatar:          req.Avatar,
		Color:           req.Color,
		Model:           req.Model,
		Harness:         req.Harness,
		PrivateSessions: req.PrivateSessions,
		DesktopTarget:         req.DesktopTarget,
		BrowserMode:           req.BrowserMode,
		PermissionMode:        req.PermissionMode,
		CoordinatorCanApprove: req.CoordinatorCanApprove,
	}
	config.NormalizeBot(&bot, h.cfg)
	if !harness.CapabilitiesOf(runner).SupportsMode(bot.EffectivePermissionMode()) {
		bot.PermissionMode = config.PermissionModeAuto
	}
	if err := config.EnsureWorkspace(&bot, h.cfg, req.Instructions); err != nil {
		h.lanesMu.Unlock()
		return nil, fmt.Errorf("init workspace for %q: %w", req.ID, err)
	}

	h.cfg.Bots = append(h.cfg.Bots, bot)
	if h.cfg.ProjectRoot != "" {
		if _, err := config.SaveLocal(h.cfg); err != nil {
			// Roll back slice entry if persisting fails
			h.cfg.Bots = h.cfg.Bots[:len(h.cfg.Bots)-1]
			h.lanesMu.Unlock()
			return nil, fmt.Errorf("save config: %w", err)
		}
	}

	h.startLaneLocked(bot)
	st := h.statusOfLocked(h.lanes[bot.ID])
	h.lanesMu.Unlock()

	h.broadcast(Event{
		Type:    "bot_created",
		BotID:   bot.ID,
		Payload: st,
	})
	h.notifySchedulesChanged()
	return &st, nil
}

// BotUpdatePreview describes the outcome of validating or dry-running a bot update.
type BotUpdatePreview struct {
	DryRun             bool   `json:"dryRun"`
	Valid              bool   `json:"valid"`
	BotID              string `json:"botId"`
	Harness            string `json:"harness"`
	Model              string `json:"model"`
	ModelInherited     bool   `json:"modelInherited"`
	HarnessChanged     bool   `json:"harnessChanged"`
	SessionWillArchive bool   `json:"sessionWillArchive"`
}

func (h *Hub) normalizeBotUpdate(upd *BotUpdate) error {
	if upd.Harness != nil {
		*upd.Harness = strings.TrimSpace(*upd.Harness)
		if _, err := h.registry.Get(*upd.Harness); err != nil {
			return err
		}
	}
	if upd.Name != nil && strings.TrimSpace(*upd.Name) == "" {
		return errors.New("name is required")
	}
	if upd.Avatar != nil {
		avatar, err := config.NormalizeBotAvatar(*upd.Avatar)
		if err != nil {
			return err
		}
		*upd.Avatar = avatar
	}
	if upd.Color != nil {
		color, err := config.NormalizeBotColor(*upd.Color)
		if err != nil {
			return err
		}
		*upd.Color = color
	}
	if upd.DesktopTarget != nil {
		dt, err := config.NormalizeDesktopTarget(*upd.DesktopTarget)
		if err != nil {
			return err
		}
		*upd.DesktopTarget = dt
	}
	if upd.BrowserMode != nil {
		bm, err := config.NormalizeBrowserMode(*upd.BrowserMode)
		if err != nil {
			return err
		}
		*upd.BrowserMode = bm
	}
	if upd.PermissionMode != nil {
		pm, err := config.NormalizePermissionMode(*upd.PermissionMode)
		if err != nil {
			return err
		}
		*upd.PermissionMode = pm
	}
	if upd.Model != nil {
		*upd.Model = strings.TrimSpace(*upd.Model)
		if err := config.ValidateModelID(*upd.Model); err != nil {
			return err
		}
	}
	return nil
}

// ValidateCreateBot checks bot creation fields and, when strict is true, verifies
// the target harness is ready and any explicit model is valid for it.
func (h *Hub) ValidateCreateBot(ctx context.Context, req CreateBotRequest, strict bool) error {
	if err := config.ValidateBotID(strings.TrimSpace(req.ID)); err != nil {
		return err
	}
	model := strings.TrimSpace(req.Model)
	if err := config.ValidateModelID(model); err != nil {
		return err
	}
	targetHarness := strings.TrimSpace(req.Harness)
	if targetHarness == "" {
		targetHarness = h.DefaultHarness()
	}
	if _, err := h.registry.Get(targetHarness); err != nil {
		return err
	}
	if strict {
		if err := h.ValidateHarnessReady(ctx, targetHarness); err != nil {
			return err
		}
		if model != "" {
			if err := h.ValidateModelForHarness(ctx, targetHarness, model); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateBotUpdate validates a partial bot update and computes the resulting
// effective harness/model/session archive impact without mutating state.
func (h *Hub) ValidateBotUpdate(ctx context.Context, botID string, upd BotUpdate, strict bool) (*BotUpdatePreview, error) {
	if err := h.normalizeBotUpdate(&upd); err != nil {
		return nil, err
	}
	h.lanesMu.RLock()
	lane, ok := h.lanes[botID]
	var bot config.BotConfig
	if ok {
		bot = lane.bot
	}
	defaultModel, defaultModels := h.cfg.DefaultModel, h.cfg.DefaultModels
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}

	nextHarness := bot.Harness
	harnessChanged := upd.Harness != nil && *upd.Harness != bot.Harness
	if upd.Harness != nil {
		nextHarness = *upd.Harness
	}
	nextModel := bot.Model
	nextInherited := bot.ModelInherited
	if upd.Model != nil {
		nextModel = *upd.Model
		nextInherited = nextModel == ""
	}
	if nextInherited {
		tmpCfg := &config.HubConfig{DefaultModel: defaultModel, DefaultModels: defaultModels}
		nextModel = tmpCfg.DefaultModelFor(nextHarness)
	}

	if strict {
		if upd.Harness != nil {
			if err := h.ValidateHarnessReady(ctx, nextHarness); err != nil {
				return nil, err
			}
		}
		if (upd.Model != nil || harnessChanged) && !nextInherited && nextModel != "" {
			if err := h.ValidateModelForHarness(ctx, nextHarness, nextModel); err != nil {
				return nil, err
			}
		}
	}

	return &BotUpdatePreview{
		DryRun:             true,
		Valid:              true,
		BotID:              botID,
		Harness:            nextHarness,
		Model:              nextModel,
		ModelInherited:     nextInherited,
		HarnessChanged:     harnessChanged,
		SessionWillArchive: harnessChanged,
	}, nil
}

// UpdateBot changes a bot's name, description, harness, model or instructions
// (AGENTS.md), persists it, and applies it from the bot's next turn. Switching
// harness starts a new session, since sessions do not carry across harnesses.
func (h *Hub) UpdateBot(botID string, upd BotUpdate) (*BotStatus, error) {
	if err := h.normalizeBotUpdate(&upd); err != nil {
		return nil, err
	}
	h.lanesMu.Lock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.Unlock()
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	bot := lane.bot
	harnessChanged := upd.Harness != nil && *upd.Harness != bot.Harness
	prevPermMode := bot.EffectivePermissionMode()
	if upd.Name != nil {
		bot.Name = strings.TrimSpace(*upd.Name)
	}
	if upd.Description != nil {
		bot.Description = strings.TrimSpace(*upd.Description)
	}
	if upd.Avatar != nil {
		bot.Avatar = *upd.Avatar
	}
	if upd.Color != nil {
		bot.Color = *upd.Color
	}
	if upd.Harness != nil {
		bot.Harness = *upd.Harness
	}
	if upd.Model != nil {
		bot.Model = *upd.Model
		bot.ModelInherited = bot.Model == ""
	}
	if upd.PrivateSessions != nil {
		bot.PrivateSessions = *upd.PrivateSessions
	}
	if upd.DesktopTarget != nil {
		bot.DesktopTarget = *upd.DesktopTarget
	}
	if upd.BrowserMode != nil {
		bot.BrowserMode = *upd.BrowserMode
	}
	if upd.CoordinatorCanApprove != nil {
		v := *upd.CoordinatorCanApprove
		bot.CoordinatorCanApprove = &v
	}
	if upd.PermissionMode != nil {
		pm := *upd.PermissionMode
		if pm == "" {
			pm = h.cfg.EffectiveDefaultPermissionMode()
		}
		bot.PermissionMode = pm
		bot.AutoApprove = nil
	}
	if runner, err := h.registry.Get(bot.Harness); err == nil {
		caps := harness.CapabilitiesOf(runner)
		effPM := bot.EffectivePermissionMode()
		if !caps.SupportsMode(effPM) {
			if upd.PermissionMode != nil && *upd.PermissionMode != "" {
				h.lanesMu.Unlock()
				return nil, fmt.Errorf("harness %q does not support permission mode %q", bot.Harness, effPM)
			}
			bot.PermissionMode = config.PermissionModeAuto
			bot.AutoApprove = nil
		}
	}
	if bot.ModelInherited {
		bot.Model = h.cfg.DefaultModelFor(bot.Harness)
	}
	permChanged := bot.EffectivePermissionMode() != prevPermMode

	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			for i := range raw.Bots {
				rb := &raw.Bots[i]
				if rb.ID != botID {
					continue
				}
				rb.Name, rb.Description, rb.Harness = bot.Name, bot.Description, bot.Harness
				rb.Avatar, rb.Color = bot.Avatar, bot.Color
				rb.PrivateSessions = bot.PrivateSessions
				rb.DesktopTarget = bot.DesktopTarget
				rb.BrowserMode = bot.BrowserMode
				if upd.CoordinatorCanApprove != nil {
					rb.CoordinatorCanApprove = bot.CoordinatorCanApprove
				}
				if upd.PermissionMode != nil || harnessChanged {
					rb.PermissionMode = bot.PermissionMode
					if upd.PermissionMode != nil || rb.PermissionMode != "" {
						rb.AutoApprove = nil
					}
				}
				if upd.Model != nil || harnessChanged {
					rb.Model = ""
					if !bot.ModelInherited {
						rb.Model = bot.Model
					}
				}
				return nil
			}
			return fmt.Errorf("bot %q is not in %s", botID, h.cfg.ConfigPath)
		})
		if err != nil {
			h.lanesMu.Unlock()
			return nil, fmt.Errorf("save config: %w", err)
		}
	}
	if upd.Instructions != nil {
		if err := os.MkdirAll(bot.Workspace, 0o755); err != nil {
			h.lanesMu.Unlock()
			return nil, fmt.Errorf("create workspace: %w", err)
		}
		content := strings.TrimSpace(*upd.Instructions)
		if content != "" {
			content += "\n"
		}
		if err := writeFileAtomic(filepath.Join(bot.Workspace, "AGENTS.md"), []byte(content), 0o644); err != nil {
			h.lanesMu.Unlock()
			return nil, fmt.Errorf("write AGENTS.md: %w", err)
		}
	}
	lane.bot = bot
	for i := range h.cfg.Bots {
		if h.cfg.Bots[i].ID == botID {
			h.cfg.Bots[i] = bot
		}
	}
	st := h.statusOfLocked(lane)
	h.lanesMu.Unlock()

	if harnessChanged {
		if bot.Harness == "claude" {
			_ = config.EnsureWorkspaceSkill(&bot)
		}
		lane.sessionGen.Add(1)
		if prev, existed, _ := h.store.ArchiveActive(botID, "harness_changed", "", ""); existed {
			h.appendMessage(state.ChatMessage{
				BotID: botID, SessionID: prev.ID, Role: "system", From: "system",
				Text: fmt.Sprintf("Switched to %s and started a new session (previous: %s).", bot.Harness, prev.SessionID),
			})
		}
		st, _ = h.statusOne(botID)
	}
	if harnessChanged || permChanged {
		h.CancelApprovalsByBot(botID)
		h.clearSessionAllow(botID)
	}
	h.broadcast(Event{Type: "bot_status", BotID: botID, Payload: st})
	return &st, nil
}

// DeleteBot stops the bot's serial lane and removes it from config, keeping its
// workspace directory and chat history files intact on disk.
func (h *Hub) DeleteBot(botID string) error {
	h.lanesMu.Lock()
	lane, ok := h.lanes[botID]
	if !ok {
		h.lanesMu.Unlock()
		return fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	if len(h.order) <= 1 {
		h.lanesMu.Unlock()
		return errors.New("at least one bot is required")
	}
	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			filtered := make([]config.BotConfig, 0, len(raw.Bots))
			for _, rb := range raw.Bots {
				if rb.ID != botID {
					filtered = append(filtered, rb)
				}
			}
			if len(filtered) == len(raw.Bots) {
				return fmt.Errorf("bot %q is not in %s", botID, h.cfg.ConfigPath)
			}
			raw.Bots = filtered
			return nil
		})
		if err != nil {
			h.lanesMu.Unlock()
			return fmt.Errorf("save config: %w", err)
		}
	}

	delete(h.lanes, botID)
	nextOrder := make([]string, 0, len(h.order)-1)
	for _, id := range h.order {
		if id != botID {
			nextOrder = append(nextOrder, id)
		}
	}
	h.order = nextOrder
	nextBots := make([]config.BotConfig, 0, len(h.cfg.Bots))
	for _, b := range h.cfg.Bots {
		if b.ID != botID {
			nextBots = append(nextBots, b)
		}
	}
	h.cfg.Bots = nextBots
	h.lanesMu.Unlock()

	lane.cancelMu.Lock()
	cancel := lane.cancel
	lane.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	h.cancelIsolatedByBot(botID)
	h.cancelTasksByBot(botID)
	if h.desktop != nil {
		h.desktop.CancelTakeoversByBot(botID)
	}
	h.CancelApprovalsByBot(botID)
	h.clearSessionAllow(botID)
	if h.localDeskPool != nil {
		h.localDeskPool.RemoveSlot(botID)
	}
	go closeBotBrowserSession(botID)
	lane.stop()

	if h.store != nil {
		_, _, _ = h.store.Reset(botID)
	}
	h.broadcast(Event{
		Type:    "bot_deleted",
		BotID:   botID,
		Payload: map[string]any{"id": botID},
	})
	h.notifySchedulesChanged()
	return nil
}

func (h *Hub) statusOne(botID string) (BotStatus, error) {
	sts, err := h.Status(botID)
	if err != nil {
		return BotStatus{}, err
	}
	return sts[0], nil
}
