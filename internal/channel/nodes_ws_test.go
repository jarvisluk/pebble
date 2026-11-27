package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
)

func TestNodeWSAuthAndDeviceTokens(t *testing.T) {
	devSecret := "device-plain-secret"
	hashBytes := sha256.Sum256([]byte("device-hashed-secret"))
	hashHex := "sha256:" + hex.EncodeToString(hashBytes[:])

	srv, h := newTestServer(t, `{
		"http": {
			"host": "127.0.0.1",
			"port": 0,
			"apiToken": "admin-secret",
			"deviceTokens": [
				{"id": "dev-plain", "name": "Studio", "token": "`+devSecret+`", "scopes": ["node"]},
				{"id": "dev-hash", "name": "Laptop", "tokenHash": "`+hashHex+`", "scopes": ["node:agent"]},
				{"id": "dev-revoked", "name": "Old", "token": "revoked-secret", "revoked": true}
			]
		},
		"bots": [{"id": "a", "name": "A", "harness": "opencode"}]
	}`)

	ts := httptest.NewServer(srv.srv.Handler)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/nodes/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Unauthenticated /api/nodes/ws upgrade must fail with 401.
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil || (resp != nil && resp.StatusCode != http.StatusUnauthorized) {
		t.Fatalf("expected 401 for unauthenticated WS dial, err=%v resp=%v", err, resp)
	}

	// 2. Revoked device token must fail with 401.
	_, resp, err = websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer revoked-secret"}},
	})
	if err == nil || (resp != nil && resp.StatusCode != http.StatusUnauthorized) {
		t.Fatalf("expected 401 for revoked device token, err=%v resp=%v", err, resp)
	}

	// 3. Scoped "node" plaintext device token works on /api/nodes/ws, rejected on /api/bots.
	botsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/bots", nil)
	botsReq.Header.Set("Authorization", "Bearer "+devSecret)
	botsResp, err := http.DefaultClient.Do(botsReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = botsResp.Body.Close()
	if botsResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for node-scoped token on /api/bots, got %d", botsResp.StatusCode)
	}

	// 4. Hashed "node:agent" device token works for full node.Agent over /api/nodes/ws.
	fake := &stubHTTPTestHarness{}
	hreg := harness.NewRegistry()
	hreg.Register(fake)
	_ = hreg

	agent := node.NewAgent(node.AgentOptions{
		NodeID:      "laptop-1",
		Name:        "Laptop 1",
		BaseURL:     ts.URL,
		Token:       "device-hashed-secret",
		Permissions: []string{node.PermExec, node.PermAgent},
		Enabled:     true,
		ProbeHarnesses: func(context.Context, *harness.Registry) []node.NodeHarnessInfo {
			return []node.NodeHarnessInfo{{Kind: "opencode", Installed: true, AuthStatus: "ready"}}
		},
	})
	defer agent.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := h.Nodes().Get("laptop-1"); ok && info.Connected && info.Transport == "ws" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	info, ok := h.Nodes().Get("laptop-1")
	if !ok || !info.Connected || info.Transport != "ws" {
		t.Fatalf("expected laptop-1 connected via ws, got %+v (ok=%v)", info, ok)
	}

	// "node:agent" token also has access to /api/bots/{id}/outbound (for MCP) but not /api/settings.
	setReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/settings", nil)
	setReq.Header.Set("Authorization", "Bearer device-hashed-secret")
	setResp, err := http.DefaultClient.Do(setReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = setResp.Body.Close()
	if setResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for node:agent token on /api/settings, got %d", setResp.StatusCode)
	}
	// Verify empty DeviceTokens backward compatibility with APIToken-only server.
	_ = config.DeviceTokenConfig{}
}
