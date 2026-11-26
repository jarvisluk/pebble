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
	Magpie         harness.MagpieStatus           `json:"magpie"`
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

// Harnesses lists the registered harnesses with the model their bots
// inherit and how magpie sees them.
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
	magpieOK := harness.MagpieInstalled()
	var wg sync.WaitGroup
	for i := range out {
		if !out[i].Installed {
			out[i].Magpie = harness.MagpieStatus{Installed: magpieOK}
			continue
		}
		wg.Add(1)
		go func(hi *HarnessInfo) {
			defer wg.Done()
			if magpieOK {
				hi.Magpie = harness.GetMagpieStatus(ctx, hi.ID)
			}
			hi.AuthStatus, hi.AuthDetail = harness.DetectHarnessAuthCached(ctx, hi.ID)
		}(&out[i])
	}
	wg.Wait()
	return out
}

// Models lists the models a harness offers: magpie's catalog (when installed)
// merged with the harness's own models.
func (h *Hub) Models(ctx context.Context, kind string) ([]string, error) {
	return h.ModelsWithSource(ctx, kind, "")
}

// ModelsWithSource returns the available models for kind.
// When source == "magpie", it returns Magpie gateway models for wiring that harness.
// Otherwise it returns Magpie models only when kind is connected to Magpie, and
// native CLI models when kind is running in native mode.
func (h *Hub) ModelsWithSource(ctx context.Context, kind, source string) ([]string, error) {
	r, err := h.registry.Get(kind)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(source), "magpie") {
		if !harness.MagpieInstalled() {
			return []string{}, nil
		}
		return harness.MagpieModels(ctx, kind), nil
	}
	var nativeModel string
	if harness.MagpieInstalled() {
		st := harness.GetMagpieStatus(ctx, kind)
		if st.Connected {
			if models := harness.MagpieModels(ctx, kind); len(models) > 0 {
				return models, nil
			}
		}
		nativeModel = strings.TrimSpace(st.NativeModel)
	}
	models, err := r.Models(ctx)
	if err != nil {
		if nativeModel != "" {
			return []string{nativeModel}, nil
		}
		return nil, err
	}
	if nativeModel != "" {
		found := false
		for _, m := range models {
			if m == nativeModel {
				found = true
				break
			}
		}
		if !found {
			out := make([]string, 0, len(models)+1)
			out = append(out, nativeModel)
			out = append(out, models...)
			models = out
		}
	}
	return models, nil
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

// MagpieConnect asks magpie to point a harness's CLI at its gateway, using
// the given model or the harness's default, and reloads the harness when idle.
func (h *Hub) MagpieConnect(ctx context.Context, kind, model string) error {
	if _, err := h.registry.Get(kind); err != nil {
		return err
	}
	h.lanesMu.RLock()
	if strings.TrimSpace(model) == "" {
		model = h.cfg.DefaultModelFor(kind)
	}
	h.lanesMu.RUnlock()
	if err := harness.MagpieConnect(ctx, kind, model); err != nil {
		return err
	}
	h.reloadHarness(kind)
	return nil
}

// MagpieDisconnect asks magpie to restore a harness's CLI to its native config
// (`magpie <agent> default`), and reloads the harness when idle.
func (h *Hub) MagpieDisconnect(ctx context.Context, kind string) error {
	if _, err := h.registry.Get(kind); err != nil {
		return err
	}
	if err := harness.MagpieDisconnect(ctx, kind); err != nil {
		return err
	}
	h.reloadHarness(kind)
	return nil
}

// MagpieProviderAction invokes `magpie provider ...` and schedules harness reloads
// when provider definitions change.
func (h *Hub) MagpieProviderAction(ctx context.Context, req harness.MagpieProviderReq) (string, error) {
	out, err := harness.MagpieProviderAction(ctx, req)
	if err != nil {
		return "", err
	}
	if req.Action != "test" {
		for _, r := range h.registry.All() {
			h.reloadHarness(r.Kind())
		}
	}
	return out, nil
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
	models, err := h.ModelsWithSource(ctx, kind, "")
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
	if (kind == "claude" || kind == "codex") && strings.HasPrefix(model, "magpie/") {
		return fmt.Errorf("harness %s expects bare model IDs without the 'magpie/' prefix (got %q)", kind, model)
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
