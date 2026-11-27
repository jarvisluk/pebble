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
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

type blockingDesktopHarness struct {
	started chan struct{}
}

func (b *blockingDesktopHarness) Kind() string { return "opencode" }

func (b *blockingDesktopHarness) Label() string { return "OpenCode" }

func (b *blockingDesktopHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (b *blockingDesktopHarness) Close() {}

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
	putReq := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"desktopHarness":"opencode","desktopModel":"anthropic/claude-haiku-4-5"}`))
	putReq.Host = "127.0.0.1:3789"
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK || !strings.Contains(putRec.Body.String(), `"desktopModel":"anthropic/claude-haiku-4-5"`) {
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

type stubHTTPTestHarness struct{}

func (s *stubHTTPTestHarness) Kind() string { return "stub" }

func (s *stubHTTPTestHarness) Label() string { return "Stub" }

func (s *stubHTTPTestHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (s *stubHTTPTestHarness) Close() {}

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
