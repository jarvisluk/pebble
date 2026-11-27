package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/scheduler"
	"bot-hub/internal/state"
)

func TestGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	cases := []struct {
		name   string
		cfg    config.HTTPConfig
		method string
		path   string
		host   string
		origin string
		auth   string
		want   int
	}{
		{"same-origin ok", config.HTTPConfig{Host: "127.0.0.1"}, "POST", "/api/bots/a/message", "127.0.0.1:3789", "http://127.0.0.1:3789", "", 200},
		{"cross-site POST blocked", config.HTTPConfig{Host: "127.0.0.1"}, "POST", "/api/bots/a/message", "127.0.0.1:3789", "https://evil.example", "", 403},
		{"cross-site webhook blocked", config.HTTPConfig{Host: "127.0.0.1"}, "POST", "/hooks/a", "127.0.0.1:3789", "https://evil.example", "", 403},
		{"webhook any host ok", config.HTTPConfig{Host: "127.0.0.1"}, "POST", "/hooks/a", "tunnel.example", "", "", 200},
		{"dns rebinding blocked", config.HTTPConfig{Host: "127.0.0.1"}, "GET", "/api/bots", "evil.example:3789", "", "", 403},
		{"token missing", config.HTTPConfig{Host: "127.0.0.1", APIToken: "t"}, "GET", "/api/bots", "localhost:3789", "", "", 401},
		{"token ok", config.HTTPConfig{Host: "127.0.0.1", APIToken: "t"}, "GET", "/api/bots", "localhost:3789", "", "Bearer t", 200},
		{"index without token ok", config.HTTPConfig{Host: "127.0.0.1", APIToken: "t"}, "GET", "/", "localhost:3789", "", "", 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &HTTPServer{cfg: c.cfg}
			req := httptest.NewRequest(c.method, c.path, strings.NewReader("{}"))
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.auth != "" {
				req.Header.Set("Authorization", c.auth)
			}
			rec := httptest.NewRecorder()
			s.guard(ok).ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestDesktopTakeoverRoutes(t *testing.T) {
	s := NewHTTPServer(nil, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Blocking takeover/request resumes when takeover/resolve hits.
	done := make(chan int, 1)
	go func() {
		trReq := httptest.NewRequest(http.MethodPost, "/api/desktop/takeover/request", bytes.NewBufferString(`{"target":"local","botId":"ops","reason":"扫描二维码"}`))
		trReq.Host = "127.0.0.1:3789"
		trRec := httptest.NewRecorder()
		mux.ServeHTTP(trRec, trReq)
		done <- trRec.Code
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.desktopCoord().ListTakeovers()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	list := s.desktopCoord().ListTakeovers()
	if len(list) != 1 {
		t.Fatalf("expected 1 active takeover, got %d", len(list))
	}
	if st := s.desktopCoord().ScreenState("local"); st.Owner != "human" {
		t.Fatalf("expected human owner during takeover, got %+v", st)
	}

	resReq := httptest.NewRequest(http.MethodPost, "/api/desktop/takeover/resolve", bytes.NewBufferString(`{"id":"`+list[0].ID+`","note":"ok-done"}`))
	resReq.Host = "127.0.0.1:3789"
	resRec := httptest.NewRecorder()
	mux.ServeHTTP(resRec, resReq)
	if resRec.Code != http.StatusOK {
		t.Fatalf("takeover resolve failed: %d", resRec.Code)
	}
	var resBody struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(resRec.Body.Bytes(), &resBody)
	if !resBody.OK {
		t.Fatalf("expected ok=true from resolve")
	}
	if code := <-done; code != http.StatusOK {
		t.Fatalf("expected takeover/request 200 after resolve, got %d", code)
	}
	if st := s.desktopCoord().ScreenState("local"); st.Owner != "idle" {
		t.Fatalf("expected idle owner after resolve, got %+v", st)
	}
}

type noopHarness struct{ kind string }

func (n noopHarness) Kind() string                                            { return n.kind }
func (n noopHarness) Label() string                                           { return n.kind }
func (n noopHarness) RunTurn(context.Context, harness.RunOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{}, nil
}
func (n noopHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (n noopHarness) Close()                                   {}

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
		DefaultModels: map[string]string{"claude": "claude-sonnet"},
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

func TestSessionRecallRotatePrivateAndMemoryRoutes(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "a", Name: "A", Workspace: filepath.Join(tmp, "workspaces", "a"), Harness: "opencode", PrivateSessions: true},
			{ID: "b", Name: "B", Workspace: filepath.Join(tmp, "workspaces", "b"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	s1, err := store.EnsureActiveSession("a", "opencode", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessage(state.ChatMessage{
		BotID: "a", SessionID: s1.ID, Turn: 1, Role: "user", From: "web", Text: "排查 Nginx 502 错误",
	}); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Rotate bot a's session immediately with handoff note.
	rotReq := httptest.NewRequest(http.MethodPost, "/api/bots/a/rotate", bytes.NewBufferString(`{"handoffNote":"handoff nginx","defer":false}`))
	rotReq.Host = "127.0.0.1:3789"
	rotRec := httptest.NewRecorder()
	mux.ServeHTTP(rotRec, rotReq)
	if rotRec.Code != http.StatusOK || !strings.Contains(rotRec.Body.String(), `"rotated":true`) {
		t.Fatalf("POST /api/bots/a/rotate failed: %d %s", rotRec.Code, rotRec.Body.String())
	}

	// Human can list and recall bot a's session.
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions?query=Nginx", nil)
	listReq.Host = "127.0.0.1:3789"
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), s1.ID) {
		t.Fatalf("GET /api/sessions failed: %d %s", listRec.Code, listRec.Body.String())
	}

	recallReq := httptest.NewRequest(http.MethodGet, "/api/bots/a/sessions/previous", nil)
	recallReq.Host = "127.0.0.1:3789"
	recallRec := httptest.NewRecorder()
	mux.ServeHTTP(recallRec, recallReq)
	if recallRec.Code != http.StatusOK || !strings.Contains(recallRec.Body.String(), "handoff nginx") {
		t.Fatalf("GET /api/bots/a/sessions/previous failed: %d %s", recallRec.Code, recallRec.Body.String())
	}

	// Cross-bot caller b gets 403 on private bot a.
	forbReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+s1.ID+"?callerBotId=b", nil)
	forbReq.Host = "127.0.0.1:3789"
	forbRec := httptest.NewRecorder()
	mux.ServeHTTP(forbRec, forbReq)
	if forbRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for private bot session recall, got %d %s", forbRec.Code, forbRec.Body.String())
	}

	// Memory route gracefully answers even if magpie isn't installed or gateway offline.
	memReq := httptest.NewRequest(http.MethodGet, "/api/memory?scope=global", nil)
	memReq.Host = "127.0.0.1:3789"
	memRec := httptest.NewRecorder()
	mux.ServeHTTP(memRec, memReq)
	if memRec.Code != http.StatusOK || !strings.Contains(memRec.Body.String(), `"items":`) {
		t.Fatalf("GET /api/memory failed: %d %s", memRec.Code, memRec.Body.String())
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

	// Disallowed magpie as harness id returns 400.
	badMagpie := httptest.NewRequest(http.MethodPost, "/api/harnesses/magpie/install", nil)
	badMagpie.Host = "127.0.0.1:3789"
	badMagpieRec := httptest.NewRecorder()
	mux.ServeHTTP(badMagpieRec, badMagpie)
	if badMagpieRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for magpie via harness route, got %d", badMagpieRec.Code)
	}

	// GET install status for opencode returns idle by default.
	getInst := httptest.NewRequest(http.MethodGet, "/api/harnesses/opencode/install", nil)
	getInst.Host = "127.0.0.1:3789"
	getInstRec := httptest.NewRecorder()
	mux.ServeHTTP(getInstRec, getInst)
	if getInstRec.Code != http.StatusOK {
		t.Fatalf("GET install status failed: %d %s", getInstRec.Code, getInstRec.Body.String())
	}

	// GET magpie install status returns 200.
	getMp := httptest.NewRequest(http.MethodGet, "/api/magpie/install", nil)
	getMp.Host = "127.0.0.1:3789"
	getMpRec := httptest.NewRecorder()
	mux.ServeHTTP(getMpRec, getMp)
	if getMpRec.Code != http.StatusOK {
		t.Fatalf("GET magpie install status failed: %d %s", getMpRec.Code, getMpRec.Body.String())
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

type routineEchoHarness struct{}

func (routineEchoHarness) Kind() string                             { return "opencode" }
func (routineEchoHarness) Label() string                            { return "OpenCode" }
func (routineEchoHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (routineEchoHarness) Close()                                   {}
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

type blockingDesktopHarness struct {
	started chan struct{}
}

func (b *blockingDesktopHarness) Kind() string                             { return "opencode" }
func (b *blockingDesktopHarness) Label() string                            { return "OpenCode" }
func (b *blockingDesktopHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (b *blockingDesktopHarness) Close()                                   {}
func (b *blockingDesktopHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	if b.started != nil {
		select {
		case <-b.started:
		default:
			close(b.started)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "dt_1", Text: "desktop done"}, nil
}

func TestDesktopTaskAndSettingsRoutes(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	bh := &blockingDesktopHarness{}
	reg := harness.NewRegistry()
	reg.Register(bh)

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

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// PUT /api/settings persists desktopHarness & desktopModel
	putReq := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"desktopHarness":"opencode","desktopModel":"magpie/mac-mini/auto/flash"}`))
	putReq.Host = "127.0.0.1:3789"
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK || !strings.Contains(putRec.Body.String(), `"desktopModel":"magpie/mac-mini/auto/flash"`) {
		t.Fatalf("PUT /api/settings desktop failed: %d %s", putRec.Code, putRec.Body.String())
	}

	// POST /api/desktop/task succeeds
	taskReq := httptest.NewRequest(http.MethodPost, "/api/desktop/task", bytes.NewBufferString(`{"botId":"ops","nodeId":"local","goal":"open Settings"}`))
	taskReq.Host = "127.0.0.1:3789"
	taskRec := httptest.NewRecorder()
	mux.ServeHTTP(taskRec, taskReq)
	if taskRec.Code != http.StatusOK || !strings.Contains(taskRec.Body.String(), `"desktop done"`) {
		t.Fatalf("POST /api/desktop/task failed: %d %s", taskRec.Code, taskRec.Body.String())
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

func TestDesktopSetupAndPowerRoutes(t *testing.T) {
	srv := NewHTTPServer(nil, config.HTTPConfig{Host: "127.0.0.1", Port: 3789, APIToken: "tok-123"})
	mux := srv.srv.Handler

	// Unauthorized request should return 401.
	unauth := httptest.NewRequest(http.MethodPost, "/api/desktop/power", bytes.NewBufferString(`{"action":"wake"}`))
	unauth.Host = "127.0.0.1:3789"
	unauthRec := httptest.NewRecorder()
	mux.ServeHTTP(unauthRec, unauth)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", unauthRec.Code)
	}

	// Missing action should return 400.
	bad := httptest.NewRequest(http.MethodPost, "/api/desktop/power", bytes.NewBufferString(`{}`))
	bad.Host = "127.0.0.1:3789"
	bad.Header.Set("Authorization", "Bearer tok-123")
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing action, got %d %s", badRec.Code, badRec.Body.String())
	}

	// Setup on non-linux returns 400 with structured SetupResult.
	setupReq := httptest.NewRequest(http.MethodPost, "/api/desktop/setup", bytes.NewBufferString(`{"target":"local"}`))
	setupReq.Host = "127.0.0.1:3789"
	setupReq.Header.Set("Authorization", "Bearer tok-123")
	setupRec := httptest.NewRecorder()
	mux.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusBadRequest && setupRec.Code != http.StatusOK {
		t.Fatalf("unexpected setup status %d: %s", setupRec.Code, setupRec.Body.String())
	}
}

func TestQuestionsHTTPAPI(t *testing.T) {
	tmp := t.TempDir()
	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "ops"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	type askOut struct {
		resp harness.QuestionResponse
		err  error
	}
	askDone := make(chan askOut, 1)
	go func() {
		resp, err := h.AskQuestion(context.Background(), cfg.Bots[0], harness.QuestionRequest{
			ID:    "frm_http",
			Title: "Pick",
			Fields: []harness.QuestionField{
				{Key: "q0", Title: "Drink", Type: "string"},
			},
		})
		askDone <- askOut{resp, err}
	}()

	var qID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		listReq := httptest.NewRequest(http.MethodGet, "/api/questions?botId=ops", nil)
		listReq.Host = "127.0.0.1:3789"
		listRec := httptest.NewRecorder()
		mux.ServeHTTP(listRec, listReq)
		if listRec.Code != http.StatusOK {
			t.Fatalf("GET /api/questions failed: %d", listRec.Code)
		}
		var body struct {
			Items []hub.PendingQuestion `json:"items"`
		}
		_ = json.Unmarshal(listRec.Body.Bytes(), &body)
		if len(body.Items) == 1 {
			qID = body.Items[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if qID == "" {
		t.Fatalf("expected 1 pending question via GET /api/questions")
	}

	replyReq := httptest.NewRequest(http.MethodPost, "/api/questions/"+qID+"/reply", bytes.NewBufferString(`{"answers":{"q0":"coffee"}}`))
	replyReq.Host = "127.0.0.1:3789"
	replyRec := httptest.NewRecorder()
	mux.ServeHTTP(replyRec, replyReq)
	if replyRec.Code != http.StatusOK {
		t.Fatalf("POST /api/questions/{id}/reply failed: %d %s", replyRec.Code, replyRec.Body.String())
	}

	out := <-askDone
	if out.err != nil || out.resp.Cancelled || out.resp.Answers["q0"] != "coffee" {
		t.Fatalf("unexpected AskQuestion result: %+v err=%v", out.resp, out.err)
	}

	// Missing ID -> 404.
	missingReq := httptest.NewRequest(http.MethodPost, "/api/questions/"+qID+"/reply", bytes.NewBufferString(`{"cancelled":true}`))
	missingReq.Host = "127.0.0.1:3789"
	missingRec := httptest.NewRecorder()
	mux.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for settled question, got %d", missingRec.Code)
	}
}

func TestOutboundCollabAndMediaEndpoints(t *testing.T) {
	tmp := t.TempDir()
	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "alpha", Name: "Alpha", Workspace: filepath.Join(tmp, "alpha"), Harness: "opencode"},
			{ID: "beta", Name: "Beta", Workspace: filepath.Join(tmp, "beta"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()

	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer srv.Shutdown(t.Context())
	mux := srv.srv.Handler

	// 1x1 PNG in base64
	b64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	outReq := httptest.NewRequest(http.MethodPost, "/api/bots/alpha/outbound", bytes.NewBufferString(
		`{"text":"look at this","images":[{"dataBase64":"`+b64+`","filename":"dot.png","alt":"dot"}]}`,
	))
	outReq.Host = "127.0.0.1:3789"
	outRec := httptest.NewRecorder()
	mux.ServeHTTP(outRec, outReq)
	if outRec.Code != http.StatusOK {
		t.Fatalf("POST /api/bots/alpha/outbound failed: %d %s", outRec.Code, outRec.Body.String())
	}
	var sent state.ChatMessage
	if err := json.Unmarshal(outRec.Body.Bytes(), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Role != "assistant" || len(sent.Media) != 1 || !strings.HasPrefix(sent.Media[0].URL, "/api/media/") {
		t.Fatalf("unexpected outbound message: %+v", sent)
	}

	// Fetch media
	medReq := httptest.NewRequest(http.MethodGet, sent.Media[0].URL, nil)
	medReq.Host = "127.0.0.1:3789"
	medRec := httptest.NewRecorder()
	mux.ServeHTTP(medRec, medReq)
	if medRec.Code != http.StatusOK || medRec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GET media failed: %d ct=%s", medRec.Code, medRec.Header().Get("Content-Type"))
	}

	// Invalid media ID -> 400
	badReq := httptest.NewRequest(http.MethodGet, "/api/media/..secret.png", nil)
	badReq.Host = "127.0.0.1:3789"
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid media id, got %d", badRec.Code)
	}

	// Collab endpoint
	colReq := httptest.NewRequest(http.MethodGet, "/api/bots/alpha/collab/beta", nil)
	colReq.Host = "127.0.0.1:3789"
	colRec := httptest.NewRecorder()
	mux.ServeHTTP(colRec, colReq)
	if colRec.Code != http.StatusOK || !strings.Contains(colRec.Body.String(), `"messages"`) {
		t.Fatalf("GET collab failed: %d %s", colRec.Code, colRec.Body.String())
	}

	// Outbound file with attachment disposition & sandbox CSP
	fileReq := httptest.NewRequest(http.MethodPost, "/api/bots/alpha/outbound", bytes.NewBufferString(
		`{"text":"report","files":[{"dataBase64":"JVBERi0xLjQK","filename":"report.html"}]}`,
	))
	fileReq.Host = "127.0.0.1:3789"
	fileRec := httptest.NewRecorder()
	mux.ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("POST file outbound failed: %d %s", fileRec.Code, fileRec.Body.String())
	}
	var fileMsg state.ChatMessage
	_ = json.Unmarshal(fileRec.Body.Bytes(), &fileMsg)
	if len(fileMsg.Media) != 1 || fileMsg.Media[0].Kind != "file" {
		t.Fatalf("expected 1 file media, got %+v", fileMsg.Media)
	}

	dlReq := httptest.NewRequest(http.MethodGet, fileMsg.Media[0].URL+"?name=report.html", nil)
	dlReq.Host = "127.0.0.1:3789"
	dlRec := httptest.NewRecorder()
	mux.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusOK ||
		!strings.Contains(dlRec.Header().Get("Content-Disposition"), "attachment") ||
		dlRec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("expected attachment + sandbox CSP for non-image file, got code=%d cd=%q csp=%q",
			dlRec.Code, dlRec.Header().Get("Content-Disposition"), dlRec.Header().Get("Content-Security-Policy"))
	}

	// Ticket-based download when APIToken is enabled
	authSrv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1", APIToken: "sec-tok"})
	defer authSrv.Shutdown(t.Context())
	authMux := authSrv.srv.Handler

	unauthReq := httptest.NewRequest(http.MethodGet, fileMsg.Media[0].URL, nil)
	unauthReq.Host = "127.0.0.1:3789"
	unauthRec := httptest.NewRecorder()
	authMux.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token or ticket, got %d", unauthRec.Code)
	}

	tktReq := httptest.NewRequest(http.MethodPost, "/api/media/"+fileMsg.Media[0].ID+"/ticket", nil)
	tktReq.Host = "127.0.0.1:3789"
	tktReq.Header.Set("Authorization", "Bearer sec-tok")
	tktRec := httptest.NewRecorder()
	authMux.ServeHTTP(tktRec, tktReq)
	if tktRec.Code != http.StatusOK {
		t.Fatalf("POST ticket failed: %d %s", tktRec.Code, tktRec.Body.String())
	}
	var tktBody struct {
		Ticket string `json:"ticket"`
	}
	_ = json.Unmarshal(tktRec.Body.Bytes(), &tktBody)
	if tktBody.Ticket == "" {
		t.Fatalf("expected non-empty ticket")
	}

	// Valid ticket on matching media ID -> 200
	okTktReq := httptest.NewRequest(http.MethodGet, fileMsg.Media[0].URL+"?ticket="+tktBody.Ticket, nil)
	okTktReq.Host = "127.0.0.1:3789"
	okTktRec := httptest.NewRecorder()
	authMux.ServeHTTP(okTktRec, okTktReq)
	if okTktRec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid ticket, got %d", okTktRec.Code)
	}

	// Ticket bound to fileMsg cannot access another media ID -> 401
	wrongTktReq := httptest.NewRequest(http.MethodGet, sent.Media[0].URL+"?ticket="+tktBody.Ticket, nil)
	wrongTktReq.Host = "127.0.0.1:3789"
	wrongTktRec := httptest.NewRecorder()
	authMux.ServeHTTP(wrongTktRec, wrongTktReq)
	if wrongTktRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for mismatched media ticket, got %d", wrongTktRec.Code)
	}
}

func TestHTTPCommandsMentionsAndAttachmentUpload(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "a")
	_ = os.MkdirAll(ws, 0o755)
	_ = os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("hi"), 0o644)

	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	defer store.Close()

	reg := harness.NewRegistry()
	reg.Register(&stubHTTPTestHarness{})

	h := hub.New(&config.HubConfig{
		SharedDir:   filepath.Join(tmp, "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "a", Name: "A", Workspace: ws, Harness: "stub"},
			{ID: "b", Name: "B", Workspace: filepath.Join(tmp, "b"), Harness: "stub"},
		},
	}, store, msgStore, reg)
	defer h.Close()

	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})

	// 1. GET /api/bots/a/commands
	cmdReq := httptest.NewRequest(http.MethodGet, "/api/bots/a/commands", nil)
	cmdReq.Host = "127.0.0.1:3789"
	cmdRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(cmdRec, cmdReq)
	if cmdRec.Code != http.StatusOK || !strings.Contains(cmdRec.Body.String(), `"status"`) {
		t.Fatalf("GET commands failed: %d %s", cmdRec.Code, cmdRec.Body.String())
	}

	// 2. GET /api/bots/a/mentions?q=hello
	mReq := httptest.NewRequest(http.MethodGet, "/api/bots/a/mentions?q=hello", nil)
	mReq.Host = "127.0.0.1:3789"
	mRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(mRec, mReq)
	if mRec.Code != http.StatusOK || !strings.Contains(mRec.Body.String(), `"hello.txt"`) {
		t.Fatalf("GET mentions failed: %d %s", mRec.Code, mRec.Body.String())
	}

	// 3. POST /api/bots/a/attachments
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "sample.txt")
	_, _ = fw.Write([]byte("attachment body"))
	_ = mw.Close()

	upReq := httptest.NewRequest(http.MethodPost, "/api/bots/a/attachments", &buf)
	upReq.Host = "127.0.0.1:3789"
	upReq.Header.Set("Origin", "http://127.0.0.1:3789")
	upReq.Header.Set("Content-Type", mw.FormDataContentType())
	upRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(upRec, upReq)
	if upRec.Code != http.StatusOK {
		t.Fatalf("POST attachments failed: %d %s", upRec.Code, upRec.Body.String())
	}
	var upResp struct {
		Media state.MessageMedia `json:"media"`
	}
	_ = json.Unmarshal(upRec.Body.Bytes(), &upResp)
	if upResp.Media.ID == "" {
		t.Fatalf("expected media id, got %s", upRec.Body.String())
	}

	// 4. POST /api/bots/a/message with mediaIds only
	msgReq := httptest.NewRequest(http.MethodPost, "/api/bots/a/message", strings.NewReader(fmt.Sprintf(`{"message":"","mediaIds":["%s"]}`, upResp.Media.ID)))
	msgReq.Host = "127.0.0.1:3789"
	msgReq.Header.Set("Origin", "http://127.0.0.1:3789")
	msgRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(msgRec, msgReq)
	if msgRec.Code != http.StatusOK {
		t.Fatalf("POST message with mediaIds failed: %d %s", msgRec.Code, msgRec.Body.String())
	}
}

type stubHTTPTestHarness struct{}

func (s *stubHTTPTestHarness) Kind() string                                     { return "stub" }
func (s *stubHTTPTestHarness) Label() string                                    { return "Stub" }
func (s *stubHTTPTestHarness) Models(context.Context) ([]string, error)         { return nil, nil }
func (s *stubHTTPTestHarness) Close()                                           {}
func (s *stubHTTPTestHarness) RunTurn(_ context.Context, o harness.RunOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{BotID: o.Bot.ID, SessionID: "s1", Text: "ok"}, nil
}

func TestDesktopPermissionsRoutes(t *testing.T) {
	srv := NewHTTPServer(nil, config.HTTPConfig{Host: "127.0.0.1", Port: 3789, APIToken: "secret-token"})

	getReq := httptest.NewRequest(http.MethodGet, "/api/desktop/permissions?target=local", nil)
	getReq.Host = "127.0.0.1:3789"
	getReq.Header.Set("Authorization", "Bearer secret-token")
	getRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/desktop/permissions status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var listResp struct {
		Target string `json:"target"`
		Items  []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode permissions response: %v", err)
	}
	if listResp.Target != "local" || len(listResp.Items) == 0 {
		t.Fatalf("unexpected permissions payload: %+v", listResp)
	}

	badReq := httptest.NewRequest(http.MethodPost, "/api/desktop/permissions/request", strings.NewReader(`{}`))
	badReq.Host = "127.0.0.1:3789"
	badReq.Header.Set("Origin", "http://127.0.0.1:3789")
	badReq.Header.Set("Authorization", "Bearer secret-token")
	badReq.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when permission missing, got %d", badRec.Code)
	}
}

func TestHTTPPushEndpoints(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	msgStore, err := state.NewMessageStore(filepath.Join(tmp, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "assistant", Name: "Pebble", Workspace: filepath.Join(tmp, "assistant"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()
	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1", Port: 3789, APIToken: "secret-token"})
	defer srv.Shutdown(t.Context())

	doReq := func(method, path, body string) *httptest.ResponseRecorder {
		var rdr *strings.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		} else {
			rdr = strings.NewReader("")
		}
		req := httptest.NewRequest(method, path, rdr)
		req.Host = "127.0.0.1:3789"
		req.Header.Set("Origin", "http://127.0.0.1:3789")
		req.Header.Set("Authorization", "Bearer secret-token")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := doReq(http.MethodPost, "/api/push/register", `{"deviceId":"ios-1","platform":"ios","env":"sandbox","bundleId":"local.pebble.mobile","pushToken":"pt-1","liveActivityStartToken":"st-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST /api/push/register status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if rec := doReq(http.MethodPost, "/api/push/activity-token", `{"deviceId":"ios-1","activityKey":"bot:assistant","token":"at-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST /api/push/activity-token status = %d, body = %s", rec.Code, rec.Body.String())
	}

	statusRec := doReq(http.MethodGet, "/api/push/status", "")
	if statusRec.Code != http.StatusOK {
		t.Fatalf("GET /api/push/status status = %d, body = %s", statusRec.Code, statusRec.Body.String())
	}
	var st struct {
		Configured  bool   `json:"configured"`
		Mode        string `json:"mode"`
		DeviceCount int    `json:"deviceCount"`
	}
	if err := json.Unmarshal(statusRec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Mode != "disabled" || st.DeviceCount != 1 {
		t.Fatalf("unexpected push status: %+v", st)
	}

	if rec := doReq(http.MethodDelete, "/api/push/devices/ios-1", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/push/devices/ios-1 status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestWorkspaceFilesAndStageRoutes(t *testing.T) {
	tmp := t.TempDir()
	wsDir := filepath.Join(tmp, "workspaces", "assistant")
	sharedDir := filepath.Join(tmp, "workspaces", "_shared")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "notes.md"), []byte("# Hello\nWorld"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".env"), []byte("TOKEN=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sharedDir, filepath.Join(wsDir, "_shared")); err != nil {
		t.Fatal(err)
	}

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
	reg.Register(noopHarness{kind: "opencode"})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   sharedDir,
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "assistant", Name: "Pebble", Workspace: wsDir, Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()
	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1", Port: 3789, APIToken: "secret-token"})
	defer srv.Shutdown(t.Context())

	doReq := func(method, path, body, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:3789"
		req.Header.Set("Origin", "http://127.0.0.1:3789")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(rec, req)
		return rec
	}

	// List files
	listRec := doReq(http.MethodGet, "/api/bots/assistant/files", "", "Bearer secret-token")
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "notes.md") {
		t.Fatalf("GET files status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), ".env") {
		t.Fatalf("expected .env to be excluded from listing: %s", listRec.Body.String())
	}

	// Blocked path returns 403
	envRec := doReq(http.MethodGet, "/api/bots/assistant/files/content?path=.env", "", "Bearer secret-token")
	if envRec.Code != http.StatusForbidden {
		t.Fatalf("GET .env content status = %d, want 403", envRec.Code)
	}

	// Read markdown preview
	mdRec := doReq(http.MethodGet, "/api/bots/assistant/files/content?path=notes.md", "", "Bearer secret-token")
	if mdRec.Code != http.StatusOK || !strings.Contains(mdRec.Body.String(), `"kind":"markdown"`) {
		t.Fatalf("GET notes.md content status = %d, body = %s", mdRec.Code, mdRec.Body.String())
	}

	// Issue ticket & fetch raw without Bearer token
	tkRec := doReq(http.MethodPost, "/api/bots/assistant/files/ticket", `{"path":"notes.md"}`, "Bearer secret-token")
	if tkRec.Code != http.StatusOK {
		t.Fatalf("POST ticket status = %d, body = %s", tkRec.Code, tkRec.Body.String())
	}
	var tk struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(tkRec.Body.Bytes(), &tk); err != nil || tk.Ticket == "" {
		t.Fatalf("invalid ticket response: %s", tkRec.Body.String())
	}

	rawRec := doReq(http.MethodGet, "/api/bots/assistant/files/raw?path=notes.md&ticket="+tk.Ticket, "", "")
	if rawRec.Code != http.StatusOK || rawRec.Body.String() != "# Hello\nWorld" {
		t.Fatalf("GET raw with ticket status = %d, body = %s", rawRec.Code, rawRec.Body.String())
	}
	if rawRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header")
	}
	if !strings.Contains(rawRec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("expected attachment Content-Disposition for markdown raw, got %q", rawRec.Header().Get("Content-Disposition"))
	}

	// Ticket mismatch rejected
	badRaw := doReq(http.MethodGet, "/api/bots/assistant/files/raw?path=.env&ticket="+tk.Ticket, "", "")
	if badRaw.Code != http.StatusUnauthorized {
		t.Fatalf("GET raw mismatched path status = %d, want 401", badRaw.Code)
	}

	// Open stage endpoint
	stageRec := doReq(http.MethodPost, "/api/bots/assistant/stage", `{"kind":"files","path":"notes.md"}`, "Bearer secret-token")
	if stageRec.Code != http.StatusOK || !strings.Contains(stageRec.Body.String(), `"kind":"files"`) {
		t.Fatalf("POST stage status = %d, body = %s", stageRec.Code, stageRec.Body.String())
	}
}

func TestApprovalsAndDefaultPermissionModeHTTPRoutes(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspaces", "assistant")
	_ = os.MkdirAll(ws, 0o755)
	_ = os.MkdirAll(filepath.Join(tmp, "shared"), 0o755)
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "assistant", Name: "Pebble", Workspace: ws, Harness: "opencode", PermissionMode: config.PermissionModeAsk},
		},
	}
	h := hub.New(cfg, store, nil, reg)
	defer h.Close()
	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1", Port: 3789, APIToken: "secret-token"})
	defer srv.Shutdown(t.Context())

	doReq := func(method, path, body, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:3789"
		req.Header.Set("Origin", "http://127.0.0.1:3789")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(rec, req)
		return rec
	}

	// Unauthorized access blocked.
	if rec := doReq(http.MethodGet, "/api/approvals", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/approvals without auth = %d, want 401", rec.Code)
	}

	// Start pending approval in background.
	bot, _ := h.GetBot("assistant")
	respCh := make(chan harness.PermissionResponse, 1)
	go func() {
		r, _ := h.RequestApproval(t.Context(), bot, config.PermissionModeAsk, harness.PermissionRequest{
			ID:       "r1",
			ToolName: "bash",
			Kind:     "shell",
			Summary:  "ls -la",
		})
		respCh <- r
	}()

	var aprID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := doReq(http.MethodGet, "/api/approvals?botId=assistant", "", "Bearer secret-token")
		if rec.Code == http.StatusOK {
			var doc struct {
				Items []hub.PendingApproval `json:"items"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &doc)
			if len(doc.Items) == 1 {
				aprID = doc.Items[0].ID
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if aprID == "" {
		t.Fatal("timed out waiting for pending approval over HTTP")
	}

	// Invalid decision -> 400.
	if bad := doReq(http.MethodPost, "/api/approvals/"+aprID+"/reply", `{"decision":"bogus"}`, "Bearer secret-token"); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid decision status = %d, want 400", bad.Code)
	}
	// Unknown ID -> 404.
	if nf := doReq(http.MethodPost, "/api/approvals/apr_missing/reply", `{"decision":"allow_once"}`, "Bearer secret-token"); nf.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want 404", nf.Code)
	}
	// Valid reply -> 200 and unblocks RequestApproval.
	if okRec := doReq(http.MethodPost, "/api/approvals/"+aprID+"/reply", `{"decision":"allow_once"}`, "Bearer secret-token"); okRec.Code != http.StatusOK {
		t.Fatalf("POST reply status = %d body=%s", okRec.Code, okRec.Body.String())
	}
	gotResp := <-respCh
	if gotResp.Decision != "allow_once" {
		t.Fatalf("expected allow_once, got %+v", gotResp)
	}

	// GET/PUT /api/settings defaultPermissionMode.
	if putBad := doReq(http.MethodPut, "/api/settings", `{"defaultPermissionMode":"bogus"}`, "Bearer secret-token"); putBad.Code != http.StatusBadRequest {
		t.Fatalf("invalid defaultPermissionMode status = %d, want 400", putBad.Code)
	}
	if putOK := doReq(http.MethodPut, "/api/settings", `{"defaultPermissionMode":"edit"}`, "Bearer secret-token"); putOK.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings status = %d body=%s", putOK.Code, putOK.Body.String())
	}
	getSettings := doReq(http.MethodGet, "/api/settings", "", "Bearer secret-token")
	if !strings.Contains(getSettings.Body.String(), `"defaultPermissionMode":"edit"`) {
		t.Fatalf("GET /api/settings missing defaultPermissionMode: %s", getSettings.Body.String())
	}
}

type catalogNoopHarness struct {
	kind   string
	models []string
}

func (c catalogNoopHarness) Kind() string  { return c.kind }
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
	// Isolate from host Magpie and PATH so DetectHarness("opencode") and catalog models are deterministic.
	t.Setenv("MAGPIE_BIN", filepath.Join(tmp, "missing-magpie"))
	harness.InvalidateMagpieCache()
	t.Cleanup(harness.InvalidateMagpieCache)

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
	harness.InvalidateMagpieCache()
	t.Cleanup(harness.InvalidateMagpieCache)
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
