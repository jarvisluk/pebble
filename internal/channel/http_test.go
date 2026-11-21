package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestTelegramFailsClosed(t *testing.T) {
	p := &telegramPoller{allowedUID: map[string]struct{}{}, allowedCID: map[string]struct{}{}}
	if p.isAllowed(&tgMessage{}) {
		t.Fatal("empty allowlist must deny")
	}
}

func TestDesktopLockAndTakeoverRoutes(t *testing.T) {
	s := NewHTTPServer(nil, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Lock screen by agent and confirm /api/desktop/input returns 409 Conflict.
	s.desktopCoord().BeginAgentTool("local", "ops", "Ops", "click")
	inReq := httptest.NewRequest(http.MethodPost, "/api/desktop/input", bytes.NewBufferString(`{"target":"local","kind":"click","x":10,"y":10}`))
	inReq.Host = "127.0.0.1:3789"
	inRec := httptest.NewRecorder()
	mux.ServeHTTP(inRec, inReq)
	if inRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 while agent owns screen, got %d (%s)", inRec.Code, inRec.Body.String())
	}

	// Force takeover unlocks screen lock.
	ftReq := httptest.NewRequest(http.MethodPost, "/api/desktop/force-takeover", bytes.NewBufferString(`{"target":"local"}`))
	ftReq.Host = "127.0.0.1:3789"
	ftRec := httptest.NewRecorder()
	mux.ServeHTTP(ftRec, ftReq)
	if ftRec.Code != http.StatusOK {
		t.Fatalf("force-takeover failed: %d %s", ftRec.Code, ftRec.Body.String())
	}
	if st := s.desktopCoord().ScreenState("local"); st.Owner != "human" {
		t.Fatalf("expected human owner after force-takeover, got %+v", st)
	}

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
		ProjectRoot:   tmp,
		SharedDir:     filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:     filepath.Join(tmp, "sessions.json"),
		MessagesDir:   filepath.Join(tmp, "messages"),
		DefaultModel:  "global-model",
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

	// Partial PUT /api/settings (only allowLocalDesktop + sharedUserMd) must preserve defaultModel & defaultModels.
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"allowLocalDesktop":false,"sharedUserMd":"# User\nNotes"}`))
	req.Host = "127.0.0.1:3789"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings failed: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		AllowLocalDesktop bool              `json:"allowLocalDesktop"`
		DefaultModel      string            `json:"defaultModel"`
		DefaultModels     map[string]string `json:"defaultModels"`
		SharedUserMD      string            `json:"sharedUserMd"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AllowLocalDesktop {
		t.Fatal("expected allowLocalDesktop=false")
	}
	if got.DefaultModel != "global-model" || got.DefaultModels["claude"] != "claude-sonnet" {
		t.Fatalf("expected defaults preserved, got model=%q models=%v", got.DefaultModel, got.DefaultModels)
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

	sched := scheduler.New(h, nil)
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
