package channel

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/scheduler"
	"bot-hub/internal/state"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type routineEchoHarness struct{}

func (routineEchoHarness) Kind() string { return "opencode" }

func (routineEchoHarness) Label() string { return "OpenCode" }

func (routineEchoHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (routineEchoHarness) Close() {}

func (routineEchoHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "rt_ses", Text: "巡检报告：一切正常"}, nil
}

func TestRoutineHTTPRoutes(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(routineEchoHarness{})

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "workspaces", "ops"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	sched := scheduler.New(h)
	h.SetOnSchedulesChanged(sched.Sync)
	sctx, scancel := context.WithCancel(context.Background())
	defer func() {
		scancel()
		sched.Wait()
	}()
	sched.Start(sctx)

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	s.SetScheduler(sched)
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Create routine
	createReq := httptest.NewRequest(http.MethodPost, "/api/bots/ops/routines", bytes.NewBufferString(`{"name":"Disk Check","everyMinutes":15,"prompt":"check df","mainDelivery":"receipt"}`))
	createReq.Host = "127.0.0.1:3789"
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated || !strings.Contains(createRec.Body.String(), `"id":"disk-check"`) {
		t.Fatalf("POST routines failed: %d %s", createRec.Code, createRec.Body.String())
	}

	// Update routine
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/bots/ops/routines/disk-check", bytes.NewBufferString(`{"everyMinutes":30,"mainDelivery":"receipt"}`))
	patchReq.Host = "127.0.0.1:3789"
	patchRec := httptest.NewRecorder()
	mux.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK || !strings.Contains(patchRec.Body.String(), `"everyMinutes":30`) {
		t.Fatalf("PATCH routine failed: %d %s", patchRec.Code, patchRec.Body.String())
	}

	// Run routine synchronously
	runReq := httptest.NewRequest(http.MethodPost, "/api/bots/ops/routines/disk-check/run", nil)
	runReq.Host = "127.0.0.1:3789"
	runRec := httptest.NewRecorder()
	mux.ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusOK || !strings.Contains(runRec.Body.String(), "巡检报告") {
		t.Fatalf("POST run failed: %d %s", runRec.Code, runRec.Body.String())
	}

	// List routines and runs
	listReq := httptest.NewRequest(http.MethodGet, "/api/bots/ops/routines", nil)
	listReq.Host = "127.0.0.1:3789"
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), `"disk-check"`) || !strings.Contains(listRec.Body.String(), `"lastRun"`) {
		t.Fatalf("GET routines failed: %d %s", listRec.Code, listRec.Body.String())
	}

	runsReq := httptest.NewRequest(http.MethodGet, "/api/bots/ops/routine-runs?routineId=disk-check", nil)
	runsReq.Host = "127.0.0.1:3789"
	runsRec := httptest.NewRecorder()
	mux.ServeHTTP(runsRec, runsReq)
	if runsRec.Code != http.StatusOK || !strings.Contains(runsRec.Body.String(), `"runs":[`) {
		t.Fatalf("GET routine-runs failed: %d %s", runsRec.Code, runsRec.Body.String())
	}

	// Delete routine
	delReq := httptest.NewRequest(http.MethodDelete, "/api/bots/ops/routines/disk-check", nil)
	delReq.Host = "127.0.0.1:3789"
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE routine failed: %d %s", delRec.Code, delRec.Body.String())
	}
}

func TestRoutineWebhookEndpointAndSecretRedaction(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(&blockingDesktopHarness{})

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "workspaces", "ops"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	sched := scheduler.New(h)
	h.SetOnSchedulesChanged(sched.Sync)
	sctx, scancel := context.WithCancel(context.Background())
	defer func() {
		scancel()
		sched.Wait()
	}()
	sched.Start(sctx)

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	s.SetScheduler(sched)
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Create cron-only routine -> webhook trigger must return 403.
	if _, err := h.CreateRoutine("ops", config.ScheduleConfig{
		ID:     "cron-only",
		Name:   "Cron Only",
		Cron:   "*/5 * * * *",
		Prompt: "check",
	}); err != nil {
		t.Fatal(err)
	}
	req403 := httptest.NewRequest(http.MethodPost, "/hooks/ops/routines/cron-only", bytes.NewBufferString(`{}`))
	req403.Host = "127.0.0.1:3789"
	rec403 := httptest.NewRecorder()
	mux.ServeHTTP(rec403, req403)
	if rec403.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for routine without webhookTrigger, got %d %s", rec403.Code, rec403.Body.String())
	}

	// Create webhook routine with secret -> GET list must NOT leak secret.
	if _, err := h.CreateRoutine("ops", config.ScheduleConfig{
		ID:             "deploy-hook",
		Name:           "Deploy Hook",
		WebhookTrigger: true,
		WebhookSecret:  "super-secret-123",
		Prompt:         "process deploy",
	}); err != nil {
		t.Fatal(err)
	}
	listReq := httptest.NewRequest(http.MethodGet, "/api/bots/ops/routines", nil)
	listReq.Host = "127.0.0.1:3789"
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || strings.Contains(listRec.Body.String(), "super-secret-123") || !strings.Contains(listRec.Body.String(), `"webhookSecretSet":true`) {
		t.Fatalf("unexpected routine list response (secret leak or missing flag): %d %s", listRec.Code, listRec.Body.String())
	}

	// Wrong secret -> 401.
	req401 := httptest.NewRequest(http.MethodPost, "/hooks/ops/routines/deploy-hook", bytes.NewBufferString(`{"ref":"main"}`))
	req401.Host = "127.0.0.1:3789"
	req401.Header.Set("X-Webhook-Secret", "wrong")
	rec401 := httptest.NewRecorder()
	mux.ServeHTTP(rec401, req401)
	if rec401.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong secret, got %d %s", rec401.Code, rec401.Body.String())
	}

	// Valid secret -> 200.
	req200 := httptest.NewRequest(http.MethodPost, "/hooks/ops/routines/deploy-hook", bytes.NewBufferString(`{"ref":"main"}`))
	req200.Host = "127.0.0.1:3789"
	req200.Header.Set("X-Webhook-Secret", "super-secret-123")
	rec200 := httptest.NewRecorder()
	mux.ServeHTTP(rec200, req200)
	if rec200.Code != http.StatusOK || !strings.Contains(rec200.Body.String(), `"trigger":"webhook"`) {
		t.Fatalf("expected 200 webhook run, got %d %s", rec200.Code, rec200.Body.String())
	}
}
