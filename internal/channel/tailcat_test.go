package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"bot-hub/internal/node"
	"bot-hub/internal/state"
)

func TestGuardTailcatRequiresToken(t *testing.T) {
	s := &HTTPServer{cfg: config.HTTPConfig{Host: "127.0.0.1", APIToken: "secret"}}
	h := s.guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	do := func(host, token string) int {
		r := httptest.NewRequest("GET", "/api/bots", nil)
		r.Host = host
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r = r.WithContext(context.WithValue(r.Context(), viaTailcatKey, true))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if got := do("phone.local:5555", ""); got != http.StatusUnauthorized {
		t.Fatalf("tunneled without token: %d", got)
	}
	if got := do("phone.local:5555", "secret"); got != http.StatusOK {
		t.Fatalf("tunneled with token (any Host): %d", got)
	}
}

func newTestServer(t *testing.T, rawJSON string) (*HTTPServer, *hub.Hub) {
	t.Helper()
	root := t.TempDir()
	cfgPath := filepath.Join(root, "config", "bots.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(rawJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(root, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := state.NewMessageStore(cfg.MessagesDir)
	if err != nil {
		t.Fatal(err)
	}
	h := hub.New(cfg, store, msgs, harness.NewRegistry())
	t.Cleanup(h.Close)
	return NewHTTPServer(h, cfg.HTTP), h
}

func TestTailcatTokenSyncRevealAndEnvProtection(t *testing.T) {
	srv, h := newTestServer(t, `{"bots":[{"id":"a","name":"A"}]}`)

	// ResetToken generates token, syncs Hub + opencode.json, and reveals once in POST response.
	req := httptest.NewRequest("POST", "/api/tailcat", strings.NewReader(`{"resetToken":true}`))
	req.Host = "127.0.0.1:3789"
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/tailcat status %d: %s", rec.Code, rec.Body.String())
	}
	var setResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &setResp)
	tok, _ := setResp["apiToken"].(string)
	if tok == "" {
		t.Fatalf("expected generated apiToken in POST response: %v", setResp)
	}
	if h.Config().HTTP.APIToken != tok {
		t.Fatalf("hub token not synced: got %q want %q", h.Config().HTTP.APIToken, tok)
	}
	ocBytes, err := os.ReadFile(filepath.Join(h.BotWorkspace("a"), "opencode.json"))
	if err != nil || !strings.Contains(string(ocBytes), tok) {
		t.Fatalf("opencode.json not synced with new token: err=%v body=%s", err, string(ocBytes))
	}

	// Passive GET /api/tailcat must NOT leak apiToken; ?reveal=1 reveals it.
	req = httptest.NewRequest("GET", "/api/tailcat", nil)
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/tailcat status %d", rec.Code)
	}
	var getResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if _, leaked := getResp["apiToken"]; leaked {
		t.Fatalf("passive GET leaked apiToken: %v", getResp)
	}

	req = httptest.NewRequest("GET", "/api/tailcat?reveal=1", nil)
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp["apiToken"] != tok {
		t.Fatalf("reveal=1 did not return apiToken: %v", getResp)
	}

	// Env-ref token must never be revealed or overwritten.
	t.Setenv("MY_ENV_TOK", "env-secret-123")
	srvEnv, _ := newTestServer(t, `{"http":{"apiToken":"${MY_ENV_TOK}"},"bots":[{"id":"a","name":"A"}]}`)

	req = httptest.NewRequest("GET", "/api/tailcat?reveal=1", nil)
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer env-secret-123")
	rec = httptest.NewRecorder()
	srvEnv.srv.Handler.ServeHTTP(rec, req)
	var envGet map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envGet)
	if _, leaked := envGet["apiToken"]; leaked || envGet["tokenEnvRef"] != "${MY_ENV_TOK}" {
		t.Fatalf("env token should stay hidden with tokenEnvRef: %v", envGet)
	}

	req = httptest.NewRequest("POST", "/api/tailcat", strings.NewReader(`{"resetToken":true}`))
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer env-secret-123")
	rec = httptest.NewRecorder()
	srvEnv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when resetting env-backed token, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNodesHTTPHandlers(t *testing.T) {
	srv, h := newTestServer(t, `{"http":{"apiToken":"t-node"},"bots":[{"id":"a","name":"A"}]}`)

	ts := httptest.NewServer(srv.srv.Handler)
	defer ts.Close()

	// Start a simulated poll client that handles screenshot and returns base64 data.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		reqBody := `{"id":"mbp","name":"MacBook","os":"darwin","arch":"arm64","permissions":["files","desktop"]}`
		req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/nodes/poll", strings.NewReader(reqBody))
		req.Header.Set("Authorization", "Bearer t-node")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		var cmd node.Command
		_ = json.NewDecoder(resp.Body).Decode(&cmd)
		resPayload, _ := json.Marshal(node.Result{
			CallID:     cmd.CallID,
			NodeID:     "mbp",
			OK:         true,
			MimeType:   "image/png",
			DataBase64: base64.StdEncoding.EncodeToString([]byte("fakepng")),
			Output:     "captured 7 bytes",
		})
		resReq, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/nodes/result", strings.NewReader(string(resPayload)))
		resReq.Header.Set("Authorization", "Bearer t-node")
		r2, err := http.DefaultClient.Do(resReq)
		if err == nil {
			r2.Body.Close()
		}
	}()

	// Wait until node appears in GET /api/nodes.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := http.NewRequest("GET", ts.URL+"/api/nodes", nil)
		r.Header.Set("Authorization", "Bearer t-node")
		resp, err := http.DefaultClient.Do(r)
		if err == nil {
			var list struct {
				Nodes []node.NodeInfo `json:"nodes"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&list)
			resp.Body.Close()
			if len(list.Nodes) == 1 && list.Nodes[0].Connected {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Call screenshot via POST /api/nodes/_default/call.
	callReq, _ := http.NewRequest("POST", ts.URL+"/api/nodes/_default/call", strings.NewReader(`{"action":"screenshot","botId":"a"}`))
	callReq.Header.Set("Authorization", "Bearer t-node")
	callResp, err := http.DefaultClient.Do(callReq)
	if err != nil {
		t.Fatal(err)
	}
	defer callResp.Body.Close()
	if callResp.StatusCode != http.StatusOK {
		t.Fatalf("call status %d", callResp.StatusCode)
	}
	var got node.Result
	_ = json.NewDecoder(callResp.Body).Decode(&got)
	if !got.OK || got.DataBase64 != "" || !strings.Contains(got.SavedPath, filepath.Join(h.BotWorkspace("a"), "_remote")) {
		t.Fatalf("expected screenshot saved to bot workspace _remote without inline base64, got %+v", got)
	}
	if data, err := os.ReadFile(got.SavedPath); err != nil || string(data) != "fakepng" {
		t.Fatalf("saved screenshot mismatch: err=%v data=%q", err, string(data))
	}

	// Disconnect via POST /api/nodes/disconnect.
	discReq, _ := http.NewRequest("POST", ts.URL+"/api/nodes/disconnect", strings.NewReader(`{"id":"mbp"}`))
	discReq.Header.Set("Authorization", "Bearer t-node")
	discResp, err := http.DefaultClient.Do(discReq)
	if err != nil {
		t.Fatal(err)
	}
	discResp.Body.Close()

	if nodes := h.Nodes().List(); len(nodes) != 1 || nodes[0].Connected {
		t.Fatalf("expected mbp disconnected, got %+v", nodes)
	}
	<-done
}

func TestTailcatTokenCallbackAndSSEReset(t *testing.T) {
	srv, _ := newTestServer(t, `{"bots":[{"id":"a","name":"A"}]}`)

	var gotTokens []string
	OnAPITokenChanged = func(tok string) {
		gotTokens = append(gotTokens, tok)
	}
	t.Cleanup(func() { OnAPITokenChanged = nil })

	// 1. Enabling tailcat when token is empty generates a token and notifies callback.
	req := httptest.NewRequest("POST", "/api/tailcat", strings.NewReader(`{"enabled":true}`))
	req.Host = "127.0.0.1:3789"
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/tailcat enable status %d: %s", rec.Code, rec.Body.String())
	}
	var resp1 map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp1)
	tok1, _ := resp1["apiToken"].(string)
	if tok1 == "" || len(gotTokens) != 1 || gotTokens[0] != tok1 {
		t.Fatalf("expected OnAPITokenChanged(%q), got %v", tok1, gotTokens)
	}

	// Turn off tailcat cleanly so no background tunnel goroutine lingers.
	reqOff := httptest.NewRequest("POST", "/api/tailcat", strings.NewReader(`{"enabled":false}`))
	reqOff.Host = "127.0.0.1:3789"
	reqOff.Header.Set("Authorization", "Bearer "+tok1)
	recOff := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(recOff, reqOff)
	if recOff.Code != http.StatusOK {
		t.Fatalf("POST /api/tailcat disable status %d: %s", recOff.Code, recOff.Body.String())
	}

	// 2. Open SSE stream closes when resetToken triggers notifyTokenChanged.
	ts := httptest.NewServer(srv.srv.Handler)
	defer ts.Close()

	sseCtx, sseCancel := context.WithCancel(context.Background())
	defer sseCancel()
	sseReq, _ := http.NewRequestWithContext(sseCtx, "GET", ts.URL+"/api/events", nil)
	sseReq.Header.Set("Authorization", "Bearer "+tok1)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("GET /api/events error: %v", err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/events status %d", sseResp.StatusCode)
	}

	sseDone := make(chan struct{})
	go func() {
		defer close(sseDone)
		buf := make([]byte, 1024)
		for {
			if _, rerr := sseResp.Body.Read(buf); rerr != nil {
				return
			}
		}
	}()

	resetReq, _ := http.NewRequest("POST", ts.URL+"/api/tailcat", strings.NewReader(`{"resetToken":true}`))
	resetReq.Header.Set("Authorization", "Bearer "+tok1)
	resetRec, err := http.DefaultClient.Do(resetReq)
	if err != nil {
		t.Fatalf("POST /api/tailcat resetToken error: %v", err)
	}
	var resp2 map[string]any
	_ = json.NewDecoder(resetRec.Body).Decode(&resp2)
	resetRec.Body.Close()
	tok2, _ := resp2["apiToken"].(string)
	if tok2 == "" || tok2 == tok1 || len(gotTokens) != 2 || gotTokens[1] != tok2 {
		t.Fatalf("expected rotated token callback (%q -> %q), got %v", tok1, tok2, gotTokens)
	}

	select {
	case <-sseDone:
	case <-time.After(2 * time.Second):
		t.Fatal("expected SSE /api/events stream to close after token rotation")
	}
}
