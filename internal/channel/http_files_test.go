package channel

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	// Open stage endpoint (kind=files backward compatibility + kind=file jump card & latestStage)
	stageRec := doReq(http.MethodPost, "/api/bots/assistant/stage", `{"kind":"files","path":"notes.md"}`, "Bearer secret-token")
	if stageRec.Code != http.StatusOK || !strings.Contains(stageRec.Body.String(), `"kind":"files"`) {
		t.Fatalf("POST stage status = %d, body = %s", stageRec.Code, stageRec.Body.String())
	}

	fileStageRec := doReq(http.MethodPost, "/api/bots/assistant/stage", `{"kind":"file","path":"notes.md","autoOpen":true}`, "Bearer secret-token")
	if fileStageRec.Code != http.StatusOK || !strings.Contains(fileStageRec.Body.String(), `"kind":"file"`) || !strings.Contains(fileStageRec.Body.String(), `"autoOpen":true`) {
		t.Fatalf("POST stage file status = %d, body = %s", fileStageRec.Code, fileStageRec.Body.String())
	}

	botsRec := doReq(http.MethodGet, "/api/bots", "", "Bearer secret-token")
	if botsRec.Code != http.StatusOK || !strings.Contains(botsRec.Body.String(), `"latestStage"`) {
		t.Fatalf("expected latestStage in /api/bots response, got %d: %s", botsRec.Code, botsRec.Body.String())
	}

	msgsRec := doReq(http.MethodGet, "/api/bots/assistant/messages", "", "Bearer secret-token")
	if msgsRec.Code != http.StatusOK || !strings.Contains(msgsRec.Body.String(), `"role":"stage"`) {
		t.Fatalf("expected persisted stage jump card in messages, got %d: %s", msgsRec.Code, msgsRec.Body.String())
	}
}
