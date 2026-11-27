package channel

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type noopHarness struct{ kind string }

func (n noopHarness) Kind() string { return n.kind }

func (n noopHarness) Label() string { return n.kind }

func (n noopHarness) RunTurn(context.Context, harness.RunOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{}, nil
}

func (n noopHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (n noopHarness) Close() {}

func TestSettingsPreserveDefaultsAndDeleteBotRoute(t *testing.T) {
	tmp := t.TempDir()
	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})
	reg.Register(noopHarness{kind: "claude"})

	cfg := &config.HubConfig{
		ProjectRoot:    tmp,
		SharedDir:      filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:      filepath.Join(tmp, "sessions.json"),
		MessagesDir:    filepath.Join(tmp, "messages"),
		DefaultHarness: "claude",
		DefaultModel:   "global-model",
		DefaultModels:  map[string]string{"claude": "claude-sonnet"},
		Bots: []config.BotConfig{
			{ID: "a", Name: "A", Workspace: filepath.Join(tmp, "a"), Harness: "opencode"},
			{ID: "b", Name: "B", Workspace: filepath.Join(tmp, "b"), Harness: "claude"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Partial PUT /api/settings (only sharedUserMd) must preserve defaultModel & defaultModels.
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"sharedUserMd":"# User\nNotes"}`))
	req.Host = "127.0.0.1:3789"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings failed: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		DefaultHarness string            `json:"defaultHarness"`
		DefaultModel   string            `json:"defaultModel"`
		DefaultModels  map[string]string `json:"defaultModels"`
		SharedUserMD   string            `json:"sharedUserMd"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DefaultHarness != "claude" || got.DefaultModel != "global-model" || got.DefaultModels["claude"] != "claude-sonnet" {
		t.Fatalf("expected defaults preserved, got harness=%q model=%q models=%v", got.DefaultHarness, got.DefaultModel, got.DefaultModels)
	}
	if got.SharedUserMD != "# User\nNotes\n" {
		t.Fatalf("unexpected sharedUserMd: %q", got.SharedUserMD)
	}

	// DELETE /api/bots/b succeeds, then DELETE /api/bots/a fails (last bot), and DELETE missing returns 404.
	delB := httptest.NewRequest(http.MethodDelete, "/api/bots/b", nil)
	delB.Host = "127.0.0.1:3789"
	recB := httptest.NewRecorder()
	mux.ServeHTTP(recB, delB)
	if recB.Code != http.StatusOK {
		t.Fatalf("DELETE /api/bots/b failed: %d %s", recB.Code, recB.Body.String())
	}

	delA := httptest.NewRequest(http.MethodDelete, "/api/bots/a", nil)
	delA.Host = "127.0.0.1:3789"
	recA := httptest.NewRecorder()
	mux.ServeHTTP(recA, delA)
	if recA.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 deleting last bot, got %d %s", recA.Code, recA.Body.String())
	}

	delMissing := httptest.NewRequest(http.MethodDelete, "/api/bots/missing", nil)
	delMissing.Host = "127.0.0.1:3789"
	recM := httptest.NewRecorder()
	mux.ServeHTTP(recM, delMissing)
	if recM.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting missing bot, got %d", recM.Code)
	}
}

func TestOnboardingSettingsRoute(t *testing.T) {
	tmp := t.TempDir()
	if _, _, err := config.EnsureStarterConfig(tmp, "opencode"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(tmp, "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})

	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Fresh hub needs setup and includes harness install metadata.
	getReq := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	getReq.Host = "127.0.0.1:3789"
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings failed: %d %s", getRec.Code, getRec.Body.String())
	}
	var res struct {
		Harnesses []struct {
			ID        string                     `json:"id"`
			Installed bool                       `json:"installed"`
			Install   harness.HarnessInstallInfo `json:"install"`
		} `json:"harnesses"`
		Onboarding struct {
			Completed  bool     `json:"completed"`
			NeedsSetup bool     `json:"needsSetup"`
			Installed  []string `json:"installedHarnesses"`
		} `json:"onboarding"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Onboarding.NeedsSetup || res.Onboarding.Completed {
		t.Fatalf("expected needsSetup=true, completed=false; got %+v", res.Onboarding)
	}
	if res.Onboarding.Installed == nil {
		t.Fatal("expected non-nil installedHarnesses slice")
	}
	if len(res.Harnesses) != 1 || res.Harnesses[0].Install.Command == "" {
		t.Fatalf("expected harness install metadata, got %+v", res.Harnesses)
	}

	// Completing onboarding persists onboardingCompleted=true and clears needsSetup.
	putReq := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"onboardingCompleted":true}`))
	putReq.Host = "127.0.0.1:3789"
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings failed: %d %s", putRec.Code, putRec.Body.String())
	}
	if err := json.Unmarshal(putRec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Onboarding.Completed || res.Onboarding.NeedsSetup {
		t.Fatalf("expected completed=true, needsSetup=false after PUT; got %+v", res.Onboarding)
	}
}

func TestSetupInstallAndLoginRoutes(t *testing.T) {
	tmp := t.TempDir()
	if _, _, err := config.EnsureStarterConfig(tmp, "opencode"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(tmp, "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})

	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Disallowed install target returns 400.
	badInstall := httptest.NewRequest(http.MethodPost, "/api/harnesses/evil/install", nil)
	badInstall.Host = "127.0.0.1:3789"
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badInstall)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad install target, got %d", badRec.Code)
	}

	// GET install status for opencode returns idle by default.
	getInst := httptest.NewRequest(http.MethodGet, "/api/harnesses/opencode/install", nil)
	getInst.Host = "127.0.0.1:3789"
	getInstRec := httptest.NewRecorder()
	mux.ServeHTTP(getInstRec, getInst)
	if getInstRec.Code != http.StatusOK {
		t.Fatalf("GET install status failed: %d %s", getInstRec.Code, getInstRec.Body.String())
	}

	// POST login for opencode (does not require login) returns 400.
	badLogin := httptest.NewRequest(http.MethodPost, "/api/harnesses/opencode/login", nil)
	badLogin.Host = "127.0.0.1:3789"
	badLoginRec := httptest.NewRecorder()
	mux.ServeHTTP(badLoginRec, badLogin)
	if badLoginRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for login on opencode, got %d", badLoginRec.Code)
	}
}

type catalogNoopHarness struct {
	kind   string
	models []string
}

func (c catalogNoopHarness) Kind() string { return c.kind }

func (c catalogNoopHarness) Label() string { return c.kind }

func (c catalogNoopHarness) RunTurn(context.Context, harness.RunOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{}, nil
}

func (c catalogNoopHarness) Models(context.Context) ([]string, error) {
	return append([]string(nil), c.models...), nil
}

func (c catalogNoopHarness) Close() {}

func TestSettingsPatchDryRunValidationAndBotRoutes(t *testing.T) {
	tmp := t.TempDir()
	binDir := t.TempDir()
	for _, binName := range []string{"opencode", "claude"} {
		fakeBin := filepath.Join(binDir, binName)
		if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	harness.InvalidateAuthCache()
	t.Cleanup(harness.InvalidateAuthCache)
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	msgStore, err := state.NewMessageStore(filepath.Join(tmp, "messages"))
	if err != nil {
		t.Fatal(err)
	}

	reg := harness.NewRegistry()
	reg.Register(catalogNoopHarness{kind: "opencode", models: []string{"m1", "m2"}})
	reg.Register(catalogNoopHarness{kind: "claude", models: []string{"sonnet", "opus"}})
	reg.Register(catalogNoopHarness{kind: "trae", models: []string{"auto"}}) // not installed on PATH

	cfg := &config.HubConfig{
		ProjectRoot:    tmp,
		SharedDir:      filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:      filepath.Join(tmp, "sessions.json"),
		MessagesDir:    filepath.Join(tmp, "messages"),
		DefaultHarness: "opencode",
		DefaultModel:   "m1",
		Bots: []config.BotConfig{
			{ID: "a", Name: "A", Workspace: filepath.Join(tmp, "workspaces", "a"), Harness: "opencode", Model: "m1", ModelInherited: true},
			{ID: "b", Name: "B", Workspace: filepath.Join(tmp, "workspaces", "b"), Harness: "opencode", Model: "m1", ModelInherited: false},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1", Port: 3789})
	defer s.Shutdown(t.Context())

	doReq := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:3789"
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		s.srv.Handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. Conflicting defaultModels + defaultModelsPatch rejected.
	if rec := doReq(http.MethodPut, "/api/settings", `{"defaultModels":{"opencode":"m1"},"defaultModelsPatch":{"claude":"sonnet"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for conflicting defaultModels and defaultModelsPatch, got %d %s", rec.Code, rec.Body.String())
	}

	// 2. PATCH /api/settings merges defaultModelsPatch without wiping sibling harnesses, and "" clears one key.
	if rec := doReq(http.MethodPatch, "/api/settings", `{"defaultModelsPatch":{"opencode":"m1","claude":"sonnet"}}`); rec.Code != http.StatusOK {
		t.Fatalf("initial PATCH /api/settings failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPatch, "/api/settings", `{"defaultModelsPatch":{"opencode":"m2"}}`); rec.Code != http.StatusOK {
		t.Fatalf("merge PATCH /api/settings failed: %d %s", rec.Code, rec.Body.String())
	}
	_, per := h.DefaultModelsSnapshot()
	if per["opencode"] != "m2" || per["claude"] != "sonnet" {
		t.Fatalf("expected opencode=m2 and claude=sonnet preserved, got %v", per)
	}
	if rec := doReq(http.MethodPatch, "/api/settings", `{"defaultModelsPatch":{"claude":""}}`); rec.Code != http.StatusOK {
		t.Fatalf("clear PATCH /api/settings failed: %d %s", rec.Code, rec.Body.String())
	}
	_, per = h.DefaultModelsSnapshot()
	if _, hasClaude := per["claude"]; hasClaude || per["opencode"] != "m2" {
		t.Fatalf("expected claude cleared and opencode=m2 preserved, got %v", per)
	}

	// 3. Invalid validation must fail BEFORE mutating any other setting (e.g. sharedUserMd).
	if rec := doReq(http.MethodPatch, "/api/settings?validate=1", `{"defaultModel":"bad-model","sharedUserMd":"should-not-save"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid defaultModel, got %d %s", rec.Code, rec.Body.String())
	}
	if got := h.SharedUserMD(); got != "" {
		t.Fatalf("expected sharedUserMd untouched on validation failure, got %q", got)
	}

	// 4. Dry-run preview returns affectedBots without mutating state.
	dryRec := doReq(http.MethodPatch, "/api/settings?validate=1&dryRun=1", `{"defaultModelsPatch":{"opencode":"m1"}}`)
	if dryRec.Code != http.StatusOK {
		t.Fatalf("dryRun PATCH /api/settings failed: %d %s", dryRec.Code, dryRec.Body.String())
	}
	var dryOut struct {
		DryRun       bool     `json:"dryRun"`
		Valid        bool     `json:"valid"`
		AffectedBots []string `json:"affectedBots"`
	}
	if err := json.Unmarshal(dryRec.Body.Bytes(), &dryOut); err != nil {
		t.Fatal(err)
	}
	if !dryOut.DryRun || !dryOut.Valid || len(dryOut.AffectedBots) != 1 || dryOut.AffectedBots[0] != "a" {
		t.Fatalf("unexpected dryRun settings output: %+v", dryOut)
	}
	if _, perNow := h.DefaultModelsSnapshot(); perNow["opencode"] != "m2" {
		t.Fatalf("expected dryRun not to mutate defaultModels, got %v", perNow)
	}

	// 4b. Switching desktopHarness (or fallback defaultHarness) validates existing desktopModel under validate=1.
	if rec := doReq(http.MethodPatch, "/api/settings", `{"desktopHarness":"claude","desktopModel":"sonnet"}`); rec.Code != http.StatusOK {
		t.Fatalf("setup desktopHarness=claude failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPatch, "/api/settings?validate=1", `{"desktopHarness":"opencode"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when switching desktopHarness to opencode with stale desktopModel=sonnet, got %d %s", rec.Code, rec.Body.String())
	}
	if dh, dm := h.DesktopWorkerSnapshot(); dh != "claude" || dm != "sonnet" {
		t.Fatalf("expected desktop worker unchanged on failed switch, got (%q, %q)", dh, dm)
	}
	if rec := doReq(http.MethodPatch, "/api/settings?validate=1", `{"desktopHarness":"opencode","desktopModel":""}`); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when switching desktopHarness=opencode with cleared desktopModel, got %d %s", rec.Code, rec.Body.String())
	}
	if dh, dm := h.DesktopWorkerSnapshot(); dh != "opencode" || dm != "" {
		t.Fatalf("expected desktop worker updated to (opencode, \"\"), got (%q, %q)", dh, dm)
	}

	// 5. Bot update validate=1 & dryRun=1.
	if rec := doReq(http.MethodPatch, "/api/bots/a?validate=1", `{"model":"bad-model"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad model on bot update, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPatch, "/api/bots/a?validate=1", `{"model":"m2"}`); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid model on bot update, got %d %s", rec.Code, rec.Body.String())
	}
	botDryRec := doReq(http.MethodPatch, "/api/bots/b?dryRun=1", `{"harness":"claude","model":""}`)
	if botDryRec.Code != http.StatusOK {
		t.Fatalf("dryRun PATCH /api/bots/b failed: %d %s", botDryRec.Code, botDryRec.Body.String())
	}
	var botDry hub.BotUpdatePreview
	if err := json.Unmarshal(botDryRec.Body.Bytes(), &botDry); err != nil {
		t.Fatal(err)
	}
	if !botDry.DryRun || !botDry.Valid || !botDry.HarnessChanged || !botDry.SessionWillArchive {
		t.Fatalf("unexpected bot dryRun output: %+v", botDry)
	}
	if botB, _ := h.GetBot("b"); botB.Harness != "opencode" {
		t.Fatalf("expected dryRun not to mutate bot b harness, got %q", botB.Harness)
	}

	// 6. Bot create validate=1 checks uninstalled harness and model catalog.
	if rec := doReq(http.MethodPost, "/api/bots?validate=1", `{"id":"c1","name":"C1","harness":"trae"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for uninstalled trae on create?validate=1, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPost, "/api/bots?validate=1", `{"id":"c2","name":"C2","harness":"opencode","model":"bad-model"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad model on create?validate=1, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPost, "/api/bots?validate=1", `{"id":"c3","name":"C3","harness":"opencode","model":"m1"}`); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid create?validate=1, got %d %s", rec.Code, rec.Body.String())
	}

	// 7. PUT /api/settings without ?validate=1 still rejects switching defaultHarness to an uninstalled harness,
	// while allowing switching to an installed harness.
	if rec := doReq(http.MethodPut, "/api/settings", `{"defaultHarness":"trae"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for uninstalled defaultHarness trae without validate=1, got %d %s", rec.Code, rec.Body.String())
	}
	if got := h.DefaultHarness(); got != "opencode" {
		t.Fatalf("expected defaultHarness preserved as opencode, got %q", got)
	}
	if rec := doReq(http.MethodPut, "/api/settings", `{"defaultHarness":"claude"}`); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for installed defaultHarness claude without validate=1, got %d %s", rec.Code, rec.Body.String())
	}
	if got := h.DefaultHarness(); got != "claude" {
		t.Fatalf("expected defaultHarness updated to claude, got %q", got)
	}
}

func TestSettingsDefaultHarnessFallbackToInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MAGPIE_BIN", filepath.Join(tmp, "missing-magpie"))
	harness.InvalidateAuthCache()
	t.Cleanup(harness.InvalidateAuthCache)
	t.Cleanup(hub.SetHarnessDetectorForTest(func(kind string) (bool, string) {
		if kind == "claude" {
			return true, "/usr/local/bin/claude"
		}
		return false, ""
	}))

	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	msgStore, err := state.NewMessageStore(filepath.Join(tmp, "messages"))
	if err != nil {
		t.Fatal(err)
	}

	reg := harness.NewRegistry()
	reg.Register(catalogNoopHarness{kind: "opencode", models: []string{"m1"}})
	reg.Register(catalogNoopHarness{kind: "claude", models: []string{"sonnet"}})

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "a", Name: "A", Workspace: filepath.Join(tmp, "workspaces", "a"), Harness: "claude"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1", Port: 3789})
	defer s.Shutdown(t.Context())

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Host = "127.0.0.1:3789"
	rec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings failed: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		DefaultHarness string `json:"defaultHarness"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DefaultHarness != "claude" {
		t.Fatalf("expected defaultHarness fallback to installed claude, got %q", got.DefaultHarness)
	}
}
