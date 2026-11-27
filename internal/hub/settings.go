package hub

import (
	"bot-hub/internal/config"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultHarness returns the hub-wide default harness for new bots.
func (h *Hub) DefaultHarness() string {
	h.lanesMu.RLock()
	explicit := strings.TrimSpace(h.cfg.DefaultHarness)
	h.lanesMu.RUnlock()
	if explicit != "" {
		return explicit
	}
	if installed, _ := detectHarnessFunc("opencode"); installed {
		return "opencode"
	}
	for _, r := range h.registry.All() {
		if installed, _ := detectHarnessFunc(r.Kind()); installed {
			return r.Kind()
		}
	}
	return "opencode"
}

// SetDefaultHarness updates and persists the hub-wide default harness.
func (h *Hub) SetDefaultHarness(harness string) error {
	harness = strings.TrimSpace(harness)
	if harness != "" {
		if _, err := h.registry.Get(harness); err != nil {
			return err
		}
	}
	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	if h.cfg.ConfigPath != "" {
		if err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.DefaultHarness = harness
			return nil
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.DefaultHarness = harness
	return nil
}

// DefaultPermissionMode returns the hub-wide default permission mode for new bots.
func (h *Hub) DefaultPermissionMode() string {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	return h.cfg.EffectiveDefaultPermissionMode()
}

// SetDefaultPermissionMode updates and persists the hub-wide default permission mode.
func (h *Hub) SetDefaultPermissionMode(mode string) error {
	norm, err := config.NormalizePermissionMode(mode)
	if err != nil {
		return err
	}
	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	if h.cfg.ConfigPath != "" {
		if err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.DefaultPermissionMode = norm
			return nil
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.DefaultPermissionMode = norm
	return nil
}

// DefaultModelsSnapshot returns the hub-wide default model and a copy of the
// per-harness overrides safely under the hub lock.
func (h *Hub) DefaultModelsSnapshot() (string, map[string]string) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	m := make(map[string]string, len(h.cfg.DefaultModels))
	for k, v := range h.cfg.DefaultModels {
		m[k] = v
	}
	return h.cfg.DefaultModel, m
}

// SetDefaultModels sets the hub-wide default model and per-harness overrides
// (an empty override is removed), then re-resolves every inheriting bot.
func (h *Hub) SetDefaultModels(defaultModel string, perHarness map[string]string) error {
	defaultModel = strings.TrimSpace(defaultModel)
	if err := config.ValidateModelID(defaultModel); err != nil {
		return err
	}
	clean := map[string]string{}
	for k, v := range perHarness {
		if v = strings.TrimSpace(v); v != "" {
			if _, err := h.registry.Get(k); err != nil {
				return err
			}
			if err := config.ValidateModelID(v); err != nil {
				return err
			}
			clean[k] = v
		}
	}
	h.lanesMu.Lock()
	if h.cfg.ConfigPath != "" {
		err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.DefaultModel = defaultModel
			raw.DefaultModels = clean
			if len(clean) == 0 {
				raw.DefaultModels = nil
			}
			return nil
		})
		if err != nil {
			h.lanesMu.Unlock()
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.DefaultModel = defaultModel
	h.cfg.DefaultModels = clean
	var changedStatuses []BotStatus
	for i := range h.cfg.Bots {
		b := &h.cfg.Bots[i]
		if b.ModelInherited {
			prevModel := b.Model
			b.Model = h.cfg.DefaultModelFor(b.Harness)
			if lane := h.lanes[b.ID]; lane != nil {
				lane.bot.Model = b.Model
				if b.Model != prevModel {
					changedStatuses = append(changedStatuses, h.statusOfLocked(lane))
				}
			}
		}
	}
	h.lanesMu.Unlock()
	for _, st := range changedStatuses {
		h.broadcast(Event{Type: "bot_status", BotID: st.ID, Payload: st})
	}
	return nil
}

// OnboardingCompleted reports whether the first-run unbox wizard has been completed or skipped.
func (h *Hub) OnboardingCompleted() bool {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	return h.cfg.OnboardingCompleted
}

// SetOnboardingCompleted persists whether the first-run unbox wizard has been completed.
func (h *Hub) SetOnboardingCompleted(done bool) error {
	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	if h.cfg.ConfigPath != "" {
		if err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.OnboardingCompleted = done
			return nil
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.OnboardingCompleted = done
	return nil
}

// HasAnyConversation reports whether any configured bot has persisted turns or messages.
func (h *Hub) HasAnyConversation() bool {
	h.lanesMu.RLock()
	ids := append([]string(nil), h.order...)
	h.lanesMu.RUnlock()
	for _, id := range ids {
		if h.store != nil {
			if rec, ok := h.store.Get(id); ok && rec.TurnCount > 0 {
				return true
			}
			if h.store.ArchivedCount(id) > 0 {
				return true
			}
			if msgs, err := h.store.ListMessages(id, 1); err == nil && len(msgs) > 0 {
				return true
			}
		} else if h.messages != nil {
			if msgs, err := h.messages.List(id, 1); err == nil && len(msgs) > 0 {
				return true
			}
		}
	}
	return false
}

// SharedUserMD reads the shared _shared/USER.md memory file across all bots.
func (h *Hub) SharedUserMD() string {
	h.lanesMu.RLock()
	sharedDir := h.cfg.SharedDir
	h.lanesMu.RUnlock()
	if sharedDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(sharedDir, "USER.md"))
	if err != nil {
		return ""
	}
	return string(raw)
}

// SetSharedUserMD writes the shared _shared/USER.md file atomically.
func (h *Hub) SetSharedUserMD(content string) error {
	h.lanesMu.RLock()
	sharedDir := h.cfg.SharedDir
	h.lanesMu.RUnlock()
	if sharedDir == "" {
		return errors.New("sharedDir is not configured")
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return err
	}
	trimmed := strings.TrimRight(content, " \t\r\n")
	if trimmed != "" {
		trimmed += "\n"
	}
	return writeFileAtomic(filepath.Join(sharedDir, "USER.md"), []byte(trimmed), 0o644)
}

// DesktopWorkerSnapshot returns the configured global desktop worker harness and model.
func (h *Hub) DesktopWorkerSnapshot() (string, string) {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	return h.cfg.DesktopHarness, h.cfg.DesktopModel
}

// SetDesktopWorker persists and updates the global computer-use worker harness and model.
func (h *Hub) SetDesktopWorker(harnessKind, model string) error {
	harnessKind = strings.TrimSpace(harnessKind)
	model = strings.TrimSpace(model)
	if err := config.ValidateModelID(model); err != nil {
		return err
	}
	if harnessKind != "" {
		if _, err := h.registry.Get(harnessKind); err != nil {
			return err
		}
	}

	h.lanesMu.Lock()
	defer h.lanesMu.Unlock()
	prevExpandedModel := h.cfg.DesktopModel
	if h.cfg.ConfigPath != "" {
		if err := config.UpdateRaw(h.cfg, func(raw *config.HubConfig) error {
			raw.DesktopHarness = harnessKind
			if model == prevExpandedModel && config.ContainsEnvRef(raw.DesktopModel) {
				return nil
			}
			raw.DesktopModel = model
			return nil
		}); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	h.cfg.DesktopHarness = harnessKind
	h.cfg.DesktopModel = model
	return nil
}
