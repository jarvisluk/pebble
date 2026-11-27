package channel

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type settingsBody struct {
	DefaultHarness        *string           `json:"defaultHarness,omitempty"`
	DefaultPermissionMode *string           `json:"defaultPermissionMode,omitempty"`
	DefaultModel          *string           `json:"defaultModel,omitempty"`
	DefaultModels         map[string]string `json:"defaultModels,omitempty"`
	DefaultModelsPatch    map[string]string `json:"defaultModelsPatch,omitempty"`
	DesktopHarness        *string           `json:"desktopHarness,omitempty"`
	DesktopModel          *string           `json:"desktopModel,omitempty"`
	SharedUserMD          *string                `json:"sharedUserMd,omitempty"`
	MagpieWebURL          *string                `json:"magpieWebUrl,omitempty"`
	OnboardingCompleted   *bool                  `json:"onboardingCompleted,omitempty"`
	Memory                *hub.MemoryConfigPatch `json:"memory,omitempty"`
}

// DesktopResetFunc is registered by the native desktop host (bot-hub-desktop)
// so the onboarding wizard can navigate back to the mode selector page.
var DesktopResetFunc func()

// OnAPITokenChanged is registered by the native desktop host so local-mode
// clients immediately receive newly generated or rotated API tokens.
var OnAPITokenChanged func(token string)

func (s *HTTPServer) handleDesktopReset(w http.ResponseWriter, r *http.Request) {
	if DesktopResetFunc == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "desktop host unavailable"})
		return
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "loopback only"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	DesktopResetFunc()
}

func (s *HTTPServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if r.URL.Query().Get("fresh") == "1" {
		harness.InvalidateMagpieCache()
		harness.InvalidateAuthCache()
	}
	defaultModel, defaultModels := s.hub.DefaultModelsSnapshot()
	deskHarness, deskModel := s.hub.DesktopWorkerSnapshot()
	installed := harness.MagpieInstalled()
	var (
		healthy   bool
		providers []harness.MagpieProvider
		presets   []harness.MagpiePreset
		harnesses []hub.HarnessInfo
		wg        sync.WaitGroup
	)
	wg.Add(1)
	go func() { defer wg.Done(); harnesses = s.hub.Harnesses(ctx) }()
	if installed {
		wg.Add(3)
		go func() { defer wg.Done(); healthy = harness.MagpieGatewayHealthy(ctx) }()
		go func() { defer wg.Done(); providers = harness.MagpieListProviders(ctx) }()
		go func() { defer wg.Done(); presets = harness.MagpiePresets(ctx) }()
	}
	wg.Wait()
	if providers == nil {
		providers = []harness.MagpieProvider{}
	}
	if presets == nil {
		presets = []harness.MagpiePreset{}
	}
	installedIDs := []string{}
	for _, hi := range harnesses {
		if hi.Installed {
			installedIDs = append(installedIDs, hi.ID)
		}
	}
	completed := s.hub.OnboardingCompleted()
	hasConversation := s.hub.HasAnyConversation()
	writeJSON(w, http.StatusOK, map[string]any{
		"magpie": map[string]any{
			"installed": installed,
			"healthy":   healthy,
			"install":   harness.MagpieInstall,
			"providers": providers,
			"presets":   presets,
			"webUrl":    s.hub.MagpieWebURL(),
		},
		"desktopHarness":        deskHarness,
		"desktopModel":          deskModel,
		"defaultHarness":        s.hub.DefaultHarness(),
		"defaultPermissionMode": s.hub.DefaultPermissionMode(),
		"defaultModel":          defaultModel,
		"defaultModels":         defaultModels,
		"sharedUserMd":          s.hub.SharedUserMD(),
		"memory":                s.hub.MemoryConfig(),
		"harnesses":             harnesses,
		"canResetDesktopMode":   DesktopResetFunc != nil,
		"onboarding": map[string]any{
			"completed":          completed,
			"needsSetup":         !completed && !hasConversation,
			"installedHarnesses": installedIDs,
		},
	})
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *HTTPServer) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if body.DefaultModels != nil && body.DefaultModelsPatch != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cannot specify both defaultModels and defaultModelsPatch"})
		return
	}

	curDefaultHarness := s.hub.DefaultHarness()
	nextDefaultHarness := curDefaultHarness
	if body.DefaultHarness != nil {
		trimmed := strings.TrimSpace(*body.DefaultHarness)
		if trimmed != "" {
			if err := s.hub.ValidateHarnessKey(trimmed); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			nextDefaultHarness = trimmed
		} else {
			nextDefaultHarness = "opencode"
		}
	}

	curDefaultModel, curPerHarness := s.hub.DefaultModelsSnapshot()
	nextDefaultModel := curDefaultModel
	if body.DefaultModel != nil {
		nextDefaultModel = strings.TrimSpace(*body.DefaultModel)
		if err := config.ValidateModelID(nextDefaultModel); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}

	nextPerHarness := make(map[string]string, len(curPerHarness))
	for k, v := range curPerHarness {
		nextPerHarness[k] = v
	}
	if body.DefaultModels != nil {
		nextPerHarness = make(map[string]string, len(body.DefaultModels))
		for _, k := range sortedMapKeys(body.DefaultModels) {
			v := strings.TrimSpace(body.DefaultModels[k])
			if v == "" {
				continue
			}
			if err := s.hub.ValidateHarnessKey(k); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if err := config.ValidateModelID(v); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			nextPerHarness[k] = v
		}
	} else if body.DefaultModelsPatch != nil {
		for _, rawK := range sortedMapKeys(body.DefaultModelsPatch) {
			k := strings.TrimSpace(rawK)
			if err := s.hub.ValidateHarnessKey(k); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			v := strings.TrimSpace(body.DefaultModelsPatch[rawK])
			if err := config.ValidateModelID(v); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if v == "" {
				delete(nextPerHarness, k)
			} else {
				nextPerHarness[k] = v
			}
		}
	}

	curDeskHarness, curDeskModel := s.hub.DesktopWorkerSnapshot()
	nextDeskHarness, nextDeskModel := curDeskHarness, curDeskModel
	if body.DesktopHarness != nil {
		nextDeskHarness = strings.TrimSpace(*body.DesktopHarness)
		if nextDeskHarness != "" {
			if err := s.hub.ValidateHarnessKey(nextDeskHarness); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
		}
	}
	if body.DesktopModel != nil {
		nextDeskModel = strings.TrimSpace(*body.DesktopModel)
		if err := config.ValidateModelID(nextDeskModel); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.MagpieWebURL != nil {
		if _, err := hub.ValidateMagpieWebURL(*body.MagpieWebURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.Memory != nil {
		if err := s.hub.ValidateMemoryConfigPatch(*body.Memory); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}

	if queryBool(r, "validate") {
		vctx, vcancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer vcancel()
		if body.DefaultHarness != nil && strings.TrimSpace(*body.DefaultHarness) != "" {
			if err := s.hub.ValidateHarnessReady(vctx, nextDefaultHarness); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
		}
		if body.DefaultModel != nil && nextDefaultModel != "" {
			if err := s.hub.ValidateGlobalDefaultModel(vctx, nextDefaultHarness, nextDefaultModel); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
		}
		if body.DefaultModelsPatch != nil {
			for _, rawK := range sortedMapKeys(body.DefaultModelsPatch) {
				k := strings.TrimSpace(rawK)
				v := strings.TrimSpace(body.DefaultModelsPatch[rawK])
				if v == "" {
					continue
				}
				if err := s.hub.ValidateHarnessReady(vctx, k); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				if err := s.hub.ValidateModelForHarness(vctx, k, v); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
			}
		} else if body.DefaultModels != nil {
			for _, k := range sortedMapKeys(nextPerHarness) {
				v := nextPerHarness[k]
				if v == "" {
					continue
				}
				if err := s.hub.ValidateHarnessReady(vctx, k); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				if err := s.hub.ValidateModelForHarness(vctx, k, v); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
			}
		}
		if body.DesktopHarness != nil && nextDeskHarness != "" {
			if err := s.hub.ValidateHarnessReady(vctx, nextDeskHarness); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
		}
		desktopTargetChanged := nextDeskHarness != curDeskHarness || (nextDeskHarness == "" && nextDefaultHarness != curDefaultHarness)
		if (body.DesktopModel != nil || desktopTargetChanged) && nextDeskModel != "" {
			target := nextDeskHarness
			if target == "" {
				target = nextDefaultHarness
			}
			if err := s.hub.ValidateModelForHarness(vctx, target, nextDeskModel); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
		}
	}

	if queryBool(r, "dryRun") {
		writeJSON(w, http.StatusOK, map[string]any{
			"dryRun":         true,
			"valid":          true,
			"defaultHarness": nextDefaultHarness,
			"defaultModel":   nextDefaultModel,
			"defaultModels":  nextPerHarness,
			"desktopHarness": nextDeskHarness,
			"desktopModel":   nextDeskModel,
			"affectedBots":   s.hub.PreviewDefaultModelsImpact(nextDefaultModel, nextPerHarness),
		})
		return
	}

	if body.DesktopHarness != nil || body.DesktopModel != nil {
		if err := s.hub.SetDesktopWorker(nextDeskHarness, nextDeskModel); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.DefaultHarness != nil {
		if err := s.hub.SetDefaultHarness(*body.DefaultHarness); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.DefaultPermissionMode != nil {
		if err := s.hub.SetDefaultPermissionMode(*body.DefaultPermissionMode); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.DefaultModel != nil || body.DefaultModels != nil || body.DefaultModelsPatch != nil {
		if err := s.hub.SetDefaultModels(nextDefaultModel, nextPerHarness); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.SharedUserMD != nil {
		if err := s.hub.SetSharedUserMD(*body.SharedUserMD); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.MagpieWebURL != nil {
		if err := s.hub.SetMagpieWebURL(*body.MagpieWebURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.OnboardingCompleted != nil {
		if err := s.hub.SetOnboardingCompleted(*body.OnboardingCompleted); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.Memory != nil {
		if err := s.hub.SetMemoryConfig(*body.Memory); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	s.handleSettings(w, r)
}

func (s *HTTPServer) handleMagpieConnect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"` // connect (default) | disconnect
		Model  string `json:"model"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	kind := r.PathValue("id")
	var err error
	if strings.EqualFold(strings.TrimSpace(body.Action), "disconnect") {
		err = s.hub.MagpieDisconnect(ctx, kind)
	} else {
		err = s.hub.MagpieConnect(ctx, kind, body.Model)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.handleSettings(w, r)
}

func (s *HTTPServer) handleMagpieOpen(w http.ResponseWriter, r *http.Request) {
	if err := harness.MagpieOpen(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleHarnessInstallStart(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.PathValue("id"))
	if kind == "" || kind == "magpie" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid harness id"})
		return
	}
	task, err := harness.StartInstall(kind)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *HTTPServer) handleHarnessInstallStatus(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.PathValue("id"))
	if kind == "" || kind == "magpie" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid harness id"})
		return
	}
	writeJSON(w, http.StatusOK, harness.GetInstallStatus(kind))
}

func (s *HTTPServer) handleHarnessLoginStart(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.PathValue("id"))
	if kind == "" || kind == "magpie" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid harness id"})
		return
	}
	task, err := harness.StartLogin(kind)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *HTTPServer) handleHarnessLoginStatus(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.PathValue("id"))
	if kind == "" || kind == "magpie" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid harness id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, harness.GetLoginStatus(ctx, kind))
}

func (s *HTTPServer) handleMagpieInstallStart(w http.ResponseWriter, r *http.Request) {
	task, err := harness.StartInstall("magpie")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *HTTPServer) handleMagpieInstallStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, harness.GetInstallStatus("magpie"))
}

func (s *HTTPServer) handleMagpieProvider(w http.ResponseWriter, r *http.Request) {
	var req harness.MagpieProviderReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := s.hub.MagpieProviderAction(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	providers := harness.MagpieListProviders(ctx)
	if providers == nil {
		providers = []harness.MagpieProvider{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"output":    out,
		"providers": providers,
	})
}

func (s *HTTPServer) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	q := r.URL.Query()
	models, err := s.hub.ModelsWithSource(ctx, q.Get("harness"), q.Get("source"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if models == nil {
		models = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}
