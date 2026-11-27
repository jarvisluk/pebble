package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	detectHarnessFunc     = harness.DetectHarness
	detectHarnessAuthFunc = harness.DetectHarnessAuthCached
)

// HarnessInfo describes one available harness.
type HarnessInfo struct {
	ID             string                         `json:"id"`
	Label          string                         `json:"label"`
	DefaultModel   string                         `json:"defaultModel"`
	Installed      bool                           `json:"installed"`
	BinaryPath     string                         `json:"binaryPath,omitempty"`
	AuthStatus     string                         `json:"authStatus,omitempty"` // "ready" | "needs_login" | "unknown"
	AuthDetail     string                         `json:"authDetail,omitempty"`
	RequiresLogin  bool                           `json:"requiresLogin,omitempty"`
	CanAutoInstall bool                           `json:"canAutoInstall,omitempty"`
	Category       string                         `json:"category,omitempty"` // "recommended" | "advanced"
	Summary        string                         `json:"summary,omitempty"`
	Permissions    harness.PermissionCapabilities `json:"permissions"`
	Install        harness.HarnessInstallInfo     `json:"install"`
}

func (h *Hub) reloadHarness(kind string) {
	r, err := h.registry.Get(kind)
	if err != nil {
		return
	}
	rl, ok := r.(harness.Reloader)
	if !ok {
		return
	}
	if h.isHarnessBusy(kind) {
		h.reloadMu.Lock()
		h.pendingReload[kind] = true
		h.reloadMu.Unlock()
		return
	}
	h.reloadMu.Lock()
	delete(h.pendingReload, kind)
	h.reloadMu.Unlock()
	rl.Reload()
}

func (h *Hub) flushPendingReload(kind string) {
	h.reloadMu.Lock()
	need := h.pendingReload[kind]
	h.reloadMu.Unlock()
	if need {
		h.reloadHarness(kind)
	}
}

func (h *Hub) isHarnessBusy(kind string) bool {
	h.activeEphemeralMu.Lock()
	ephBusy := h.activeEphemeral[kind] > 0
	h.activeEphemeralMu.Unlock()
	if ephBusy {
		return true
	}
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	for _, lane := range h.lanes {
		if lane.bot.Harness == kind && lane.busy.Load() {
			return true
		}
	}
	return false
}

// HarnessKinds returns the registered harness IDs without running external CLI checks.
func (h *Hub) HarnessKinds() []string {
	var kinds []string
	for _, r := range h.registry.All() {
		kinds = append(kinds, r.Kind())
	}
	return kinds
}

// Harnesses lists the registered harnesses with their configured default models
// and live installation/authentication status.
func (h *Hub) Harnesses(ctx context.Context) []HarnessInfo {
	h.lanesMu.RLock()
	var out []HarnessInfo
	for _, r := range h.registry.All() {
		installed, binPath := harness.DetectHarness(r.Kind())
		meta := harness.HarnessInstallMeta(r.Kind())
		authStatus := "unknown"
		if installed && !meta.RequiresLogin {
			authStatus = "ready"
		}
		out = append(out, HarnessInfo{
			ID:             r.Kind(),
			Label:          r.Label(),
			DefaultModel:   h.cfg.DefaultModelFor(r.Kind()),
			Installed:      installed,
			BinaryPath:     binPath,
			AuthStatus:     authStatus,
			RequiresLogin:  meta.RequiresLogin,
			CanAutoInstall: meta.CanAutoInstall,
			Category:       meta.Category,
			Summary:        meta.Summary,
			Permissions:    harness.CapabilitiesOf(r),
			Install:        meta,
		})
	}
	h.lanesMu.RUnlock()
	var wg sync.WaitGroup
	for i := range out {
		if !out[i].Installed {
			continue
		}
		wg.Add(1)
		go func(hi *HarnessInfo) {
			defer wg.Done()
			hi.AuthStatus, hi.AuthDetail = harness.DetectHarnessAuthCached(ctx, hi.ID)
		}(&out[i])
	}
	wg.Wait()
	return out
}

// Models lists the available models reported by the harness's own CLI/runtime.
func (h *Hub) Models(ctx context.Context, kind string) ([]string, error) {
	r, err := h.registry.Get(kind)
	if err != nil {
		return nil, err
	}
	return r.Models(ctx)
}

// WarmModelsCache pre-populates model lists for installed harnesses in parallel
// so slow CLIs are ready before Settings opens.
func (h *Hub) WarmModelsCache(ctx context.Context) {
	var wg sync.WaitGroup
	for _, r := range h.registry.All() {
		kind := r.Kind()
		if installed, _ := harness.DetectHarness(kind); !installed {
			continue
		}
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			_, _ = h.Models(ctx, k)
		}(kind)
	}
	wg.Wait()
}

// ValidateHarnessKey reports whether kind is a registered harness ID.
func (h *Hub) ValidateHarnessKey(kind string) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return errors.New("harness ID is required")
	}
	_, err := h.registry.Get(kind)
	return err
}

// ValidateHarnessReady checks that kind is registered, installed, and authenticated.
func (h *Hub) ValidateHarnessReady(ctx context.Context, kind string) error {
	kind = strings.TrimSpace(kind)
	if _, err := h.registry.Get(kind); err != nil {
		return err
	}
	installed, _ := detectHarnessFunc(kind)
	if !installed {
		return fmt.Errorf("harness %q is not installed", kind)
	}
	status, detail := detectHarnessAuthFunc(ctx, kind)
	if status == "needs_login" {
		if detail != "" {
			return fmt.Errorf("harness %q requires login (%s)", kind, detail)
		}
		return fmt.Errorf("harness %q requires login", kind)
	}
	return nil
}

// ValidateModelForHarness checks model syntax and, when the harness exposes a
// non-empty model catalog, verifies exact membership.
func (h *Hub) ValidateModelForHarness(ctx context.Context, kind, model string) error {
	model = strings.TrimSpace(model)
	if err := config.ValidateModelID(model); err != nil {
		return err
	}
	if model == "" {
		return nil
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = h.DefaultHarness()
	}
	if _, err := h.registry.Get(kind); err != nil {
		return err
	}
	models, err := h.Models(ctx, kind)
	if err != nil {
		return err
	}
	if len(models) > 0 {
		for _, m := range models {
			if m == model {
				return nil
			}
		}
		limit := 15
		if len(models) < limit {
			limit = len(models)
		}
		sample := strings.Join(models[:limit], ", ")
		if len(models) > limit {
			sample += fmt.Sprintf(" ... (total %d)", len(models))
		}
		return fmt.Errorf("model %q is not in %s's available model list [%s]; run `pebble settings models --harness %s` or pass --force for a custom model ID", model, kind, sample, kind)
	}
	return nil
}

// ValidateGlobalDefaultModel checks that defaultModel applies to defaultHarness
// and passes ValidateModelForHarness for the resolved model ID.
func (h *Hub) ValidateGlobalDefaultModel(ctx context.Context, defaultHarness, defaultModel string) error {
	defaultModel = strings.TrimSpace(defaultModel)
	if err := config.ValidateModelID(defaultModel); err != nil {
		return err
	}
	if defaultModel == "" {
		return nil
	}
	defaultHarness = strings.TrimSpace(defaultHarness)
	if defaultHarness == "" {
		defaultHarness = h.DefaultHarness()
	}
	effective := (&config.HubConfig{DefaultModel: defaultModel}).DefaultModelFor(defaultHarness)
	if effective == "" {
		return fmt.Errorf("defaultModel %q does not apply to defaultHarness %q; set a compatible model or use --harness-model", defaultModel, defaultHarness)
	}
	return h.ValidateModelForHarness(ctx, defaultHarness, effective)
}

// PreviewDefaultModelsImpact returns the IDs of bots inheriting the hub default
// whose effective resolved model would change under nextDefaultModel/nextPerHarness.
func (h *Hub) PreviewDefaultModelsImpact(nextDefaultModel string, nextPerHarness map[string]string) []string {
	tmpCfg := &config.HubConfig{
		DefaultModel:  strings.TrimSpace(nextDefaultModel),
		DefaultModels: nextPerHarness,
	}
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	affected := make([]string, 0)
	for _, id := range h.order {
		lane := h.lanes[id]
		if lane == nil || !lane.bot.ModelInherited {
			continue
		}
		next := tmpCfg.DefaultModelFor(lane.bot.Harness)
		if next != lane.bot.Model {
			affected = append(affected, lane.bot.ID)
		}
	}
	return affected
}
