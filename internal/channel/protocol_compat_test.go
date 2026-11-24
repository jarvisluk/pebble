package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/desktop"
	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
)

func TestHealthzRemainsMinimalAndAuthEndpointsExposeProtocol(t *testing.T) {
	s, _ := newTestServer(t, `{
		"http": {"host": "127.0.0.1", "port": 0, "apiToken": "secret-tok"},
		"bots": [{"id": "a", "name": "A", "harness": "opencode"}]
	}`)

	// 1. /healthz stays unauthenticated and returns strictly {"ok":true} without protocol header.
	hReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	hReq.Host = "127.0.0.1:3789"
	hRec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(hRec, hReq)
	if hRec.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", hRec.Code)
	}
	if strings.TrimSpace(hRec.Body.String()) != `{"ok":true}` {
		t.Fatalf("/healthz body = %q, want {\"ok\":true}", hRec.Body.String())
	}
	if got := hRec.Header().Get(protocol.HeaderProtocol); got != "" {
		t.Fatalf("/healthz leaked %s=%q", protocol.HeaderProtocol, got)
	}

	// 2. Unauthenticated /api/meta must return 401 and no protocol header.
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	unauthReq.Host = "127.0.0.1:3789"
	unauthRec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth /api/meta status = %d, want 401", unauthRec.Code)
	}
	if got := unauthRec.Header().Get(protocol.HeaderProtocol); got != "" {
		t.Fatalf("unauth /api/meta leaked %s=%q", protocol.HeaderProtocol, got)
	}

	// 3. Authenticated /api/bots and /api/meta return X-Pebble-Protocol and server metadata.
	botsReq := httptest.NewRequest(http.MethodGet, "/api/bots", nil)
	botsReq.Host = "127.0.0.1:3789"
	botsReq.Header.Set("Authorization", "Bearer secret-tok")
	botsRec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(botsRec, botsReq)
	if botsRec.Code != http.StatusOK {
		t.Fatalf("/api/bots status = %d body=%s", botsRec.Code, botsRec.Body.String())
	}
	if got := botsRec.Header().Get(protocol.HeaderProtocol); got != "2" {
		t.Fatalf("/api/bots %s = %q, want 2", protocol.HeaderProtocol, got)
	}
	var botsResp struct {
		Server protocol.ServerInfo `json:"server"`
	}
	if err := json.Unmarshal(botsRec.Body.Bytes(), &botsResp); err != nil {
		t.Fatal(err)
	}
	if botsResp.Server.ProtocolVersion != protocol.CurrentProtocol ||
		!protocol.HasCapability(botsResp.Server.Capabilities, protocol.CapBotDesktopBinding) {
		t.Fatalf("unexpected server info in /api/bots: %+v", botsResp.Server)
	}

	metaReq := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	metaReq.Host = "127.0.0.1:3789"
	metaReq.Header.Set("Authorization", "Bearer secret-tok")
	metaRec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(metaRec, metaReq)
	if metaRec.Code != http.StatusOK {
		t.Fatalf("/api/meta status = %d body=%s", metaRec.Code, metaRec.Body.String())
	}
	var metaInfo protocol.ServerInfo
	if err := json.Unmarshal(metaRec.Body.Bytes(), &metaInfo); err != nil {
		t.Fatal(err)
	}
	if metaInfo.ProtocolVersion != protocol.CurrentProtocol || len(metaInfo.Capabilities) == 0 {
		t.Fatalf("unexpected /api/meta info: %+v", metaInfo)
	}
}

func TestLegacyRemoteNodeFallbacksAndUpgradeProtection(t *testing.T) {
	s, h := newTestServer(t, `{
		"http": {"host": "127.0.0.1", "port": 0},
		"bots": [{"id": "a", "name": "A", "harness": "opencode", "browserMode": "isolated"}]
	}`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for ctx.Err() == nil {
			cmd, err := h.Nodes().Poll(ctx, node.PollRequest{
				ID:          "legacy-linux",
				Name:        "Legacy Linux",
				OS:          "linux",
				Arch:        "amd64",
				Permissions: []string{node.PermExec, node.PermDesktop},
				// Omit ProtocolVersion & Capabilities to simulate legacy node.
			}, 2*time.Second)
			if err != nil || cmd == nil {
				continue
			}
			switch cmd.Action {
			case "desktop_status":
				raw, _ := json.Marshal(desktop.HostStatus{
					Platform:        "linux",
					Supported:       true,
					ScreenRecording: "granted",
					Accessibility:   "denied",
				})
				_ = h.Nodes().Deliver(node.Result{CallID: cmd.CallID, NodeID: "legacy-linux", OK: true, Output: string(raw)})
			case "exec":
				_ = h.Nodes().Deliver(node.Result{CallID: cmd.CallID, NodeID: "legacy-linux", OK: true, Output: string(cmd.Args)})
			default:
				_ = h.Nodes().Deliver(node.Result{CallID: cmd.CallID, NodeID: "legacy-linux", OK: false, Error: "unsupported action"})
			}
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n, ok := h.Nodes().Get("legacy-linux"); ok && n.Connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	n, ok := h.Nodes().Get("legacy-linux")
	if !ok || n.ProtocolVersion != 1 || !n.IsOutdated() {
		t.Fatalf("expected normalized legacy node protocol=1 outdated=true, got %+v (ok=%v)", n, ok)
	}

	// 1. GET /api/desktop/targets reports virtualSupported=false & outdated=true.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/desktop/targets", nil)
	s.handleDesktopTargets(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/desktop/targets status=%d body=%s", rec.Code, rec.Body.String())
	}
	var targetsResp struct {
		Targets []struct {
			ID               string `json:"id"`
			VirtualSupported bool   `json:"virtualSupported"`
			ProtocolVersion  int    `json:"protocolVersion"`
			Outdated         bool   `json:"outdated"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &targetsResp); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, tr := range targetsResp.Targets {
		if tr.ID == "legacy-linux" {
			found = true
			if tr.VirtualSupported || tr.ProtocolVersion != 1 || !tr.Outdated {
				t.Fatalf("unexpected legacy target entry: %+v", tr)
			}
		}
	}
	if !found {
		t.Fatalf("legacy-linux missing from targets: %+v", targetsResp.Targets)
	}

	// 2. Switching active target to legacy-linux:virtual:a fails closed with 422 node_upgrade_required.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/desktop/target", strings.NewReader(`{"target":"legacy-linux:virtual:a"}`))
	s.handleDesktopTargetSet(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT virtual target on legacy node = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), protocol.ErrCodeNodeUpgradeRequired) {
		t.Fatalf("expected %s in body=%s", protocol.ErrCodeNodeUpgradeRequired, rec.Body.String())
	}

	// 3. GET /api/desktop/permissions falls back to desktop_status transparently.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/desktop/permissions?target=legacy-linux", nil)
	s.handleDesktopPermissions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET permissions fallback status=%d body=%s", rec.Code, rec.Body.String())
	}
	var permResp desktop.PermissionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &permResp); err != nil {
		t.Fatal(err)
	}
	if permResp.Platform != "linux" || len(permResp.Items) != 2 ||
		permResp.Items[0].ID != "screen_recording" || permResp.Items[0].Status != "granted" ||
		permResp.Items[1].ID != "accessibility" || permResp.Items[1].Status != "denied" {
		t.Fatalf("unexpected synthesized permissions: %+v", permResp)
	}

	// 4. POST /api/desktop/power fails fast with 501 node_upgrade_required.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/desktop/power", strings.NewReader(`{"target":"legacy-linux","action":"wake"}`))
	s.handleDesktopPower(rec, req)
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), protocol.ErrCodeNodeUpgradeRequired) {
		t.Fatalf("POST power on legacy node = %d body=%s", rec.Code, rec.Body.String())
	}

	// 5. Ordinary node_exec succeeds on legacy node without forwarding browserProfileBotId.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/nodes/legacy-linux/call", strings.NewReader(`{"botId":"a","action":"exec","args":{"command":"echo ok"}}`))
	req.SetPathValue("id", "legacy-linux")
	s.handleNodeCall(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("node_exec on legacy node = %d body=%s", rec.Code, rec.Body.String())
	}
	var execRes node.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &execRes); err != nil {
		t.Fatal(err)
	}
	if !execRes.OK || strings.Contains(execRes.Output, "browserProfileBotId") {
		t.Fatalf("unexpected exec result on legacy node: %+v", execRes)
	}
}
