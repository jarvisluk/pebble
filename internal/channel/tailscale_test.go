package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"bot-hub/internal/config"
)

func TestGuardTailscaleRequiresToken(t *testing.T) {
	s := &HTTPServer{cfg: config.HTTPConfig{Host: "127.0.0.1", APIToken: "secret"}}
	h := s.guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	do := func(host, token string) int {
		r := httptest.NewRequest("GET", "/api/bots", nil)
		r.Host = host
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r = r.WithContext(context.WithValue(r.Context(), viaTailscaleKey, true))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if got := do("bothub-mac.ts.net:3789", ""); got != http.StatusUnauthorized {
		t.Fatalf("tailscale without token: %d", got)
	}
	if got := do("bothub-mac.ts.net:3789", "secret"); got != http.StatusOK {
		t.Fatalf("tailscale with token (any Host): %d", got)
	}
}

func TestTailscaleStartRefusesWithoutToken(t *testing.T) {
	s := &HTTPServer{cfg: config.HTTPConfig{
		Host:      "127.0.0.1",
		Tailscale: &config.TailscaleConfig{Enabled: true},
	}}
	if err := s.Start(); err == nil {
		t.Fatal("expected Start to fail when tailscale enabled without apiToken")
	}
}

func TestTailscaleStatusAndConfigRedaction(t *testing.T) {
	t.Setenv("TS_TEST_AUTHKEY", "tskey-auth-secret")
	srv, h := newTestServer(t, `{
		"http": {
			"apiToken": "tok-123",
			"tailscale": {
				"enabled": false,
				"hostname": "bothub-test",
				"controlUrl": "https://headscale.example.com",
				"authKey": "${TS_TEST_AUTHKEY}"
			}
		},
		"bots": [{"id": "a", "name": "A"}]
	}`)

	// Passive GET /api/tailscale must never leak apiToken or authKey.
	req := httptest.NewRequest("GET", "/api/tailscale", nil)
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer tok-123")
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/tailscale status %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, leaked := got["apiToken"]; leaked {
		t.Fatalf("passive GET leaked apiToken: %v", got)
	}
	if _, leaked := got["authKey"]; leaked {
		t.Fatalf("GET leaked authKey: %v", got)
	}
	if got["authKeyEnvRef"] != "${TS_TEST_AUTHKEY}" {
		t.Fatalf("expected authKeyEnvRef, got %v", got["authKeyEnvRef"])
	}
	if got["controlUrl"] != "https://headscale.example.com" {
		t.Fatalf("expected controlUrl, got %v", got["controlUrl"])
	}

	// Overwriting env-backed authKey via POST must be rejected.
	req = httptest.NewRequest("POST", "/api/tailscale", strings.NewReader(`{"authKey":"tskey-other"}`))
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer tok-123")
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when overwriting env authKey, got %d: %s", rec.Code, rec.Body.String())
	}

	// Logout disables tailscale and preserves env-backed authKey in config.
	req = httptest.NewRequest("POST", "/api/tailscale", strings.NewReader(`{"logout":true}`))
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer tok-123")
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/tailscale logout status %d: %s", rec.Code, rec.Body.String())
	}
	rawCfg, err := os.ReadFile(h.Config().ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawCfg), "${TS_TEST_AUTHKEY}") {
		t.Fatalf("expected env ref preserved in config: %s", string(rawCfg))
	}
}

func TestTailscaleTokenChangeCallback(t *testing.T) {
	srv, h := newTestServer(t, `{"http":{"apiToken":"initial-tok"},"bots":[{"id":"a","name":"A"}]}`)

	var got []string
	OnAPITokenChanged = func(tok string) {
		got = append(got, tok)
	}
	t.Cleanup(func() { OnAPITokenChanged = nil })

	// Explicitly setting a new apiToken via /api/tailscale notifies OnAPITokenChanged.
	req := httptest.NewRequest("POST", "/api/tailscale", strings.NewReader(`{"apiToken":"custom-ts-tok"}`))
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer initial-tok")
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/tailscale custom apiToken status %d: %s", rec.Code, rec.Body.String())
	}
	if len(got) != 1 || got[0] != "custom-ts-tok" {
		t.Fatalf("expected callback [custom-ts-tok], got %v", got)
	}

	// Resetting token via /api/tailscale also generates a token and notifies callback.
	req = httptest.NewRequest("POST", "/api/tailscale", strings.NewReader(`{"resetToken":true}`))
	req.Host = "127.0.0.1:3789"
	req.Header.Set("Authorization", "Bearer custom-ts-tok")
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/tailscale resetToken status %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	rotated, _ := resp["apiToken"].(string)
	if rotated == "" || rotated == "custom-ts-tok" || len(got) != 2 || got[1] != rotated {
		t.Fatalf("expected rotated token callback (%q), got %v", rotated, got)
	}
	if h.Config().HTTP.APIToken != rotated {
		t.Fatalf("hub APIToken = %q, want %q", h.Config().HTTP.APIToken, rotated)
	}
}
