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
	"time"
)

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
