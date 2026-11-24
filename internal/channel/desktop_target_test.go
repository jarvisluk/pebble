package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/hub"
	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
)

func TestDesktopTargetHTTP(t *testing.T) {
	s, h := newTestServer(t, `{
		"http": {"host": "127.0.0.1", "port": 0},
		"bots": [{"id": "a", "name": "A", "harness": "opencode"}]
	}`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = h.Nodes().Poll(ctx, node.PollRequest{
			ID:          "macbook",
			Name:        "MacBook Pro",
			OS:          "darwin",
			Arch:        "arm64",
			Permissions: []string{node.PermDesktop},
		}, 2*time.Second)
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var connected bool
		for _, n := range h.Nodes().List() {
			if n.ID == "macbook" && n.Connected {
				connected = true
				break
			}
		}
		if connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 1. GET /api/desktop/targets returns activeTarget = local
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/desktop/targets", nil)
	s.handleDesktopTargets(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/desktop/targets status=%d body=%s", rec.Code, rec.Body.String())
	}
	var targetsResp struct {
		ActiveTarget hub.ActiveDesktopTarget `json:"activeTarget"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &targetsResp); err != nil {
		t.Fatal(err)
	}
	if targetsResp.ActiveTarget.Target != "local" || targetsResp.ActiveTarget.Temporary {
		t.Fatalf("expected initial local target, got %+v", targetsResp.ActiveTarget)
	}

	// 2. PUT /api/desktop/target switches to macbook
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/desktop/target", strings.NewReader(`{"target":"macbook","ttlMinutes":15}`))
	s.handleDesktopTargetSet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/desktop/target status=%d body=%s", rec.Code, rec.Body.String())
	}
	var setResp struct {
		ActiveTarget hub.ActiveDesktopTarget `json:"activeTarget"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setResp); err != nil {
		t.Fatal(err)
	}
	if setResp.ActiveTarget.Target != "macbook" || !setResp.ActiveTarget.Temporary {
		t.Fatalf("expected temporary macbook target, got %+v", setResp.ActiveTarget)
	}

	// 3. PUT /api/desktop/target switches back to local
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/desktop/target", strings.NewReader(`{"target":"local"}`))
	s.handleDesktopTargetSet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/desktop/target local status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setResp); err != nil {
		t.Fatal(err)
	}
	if setResp.ActiveTarget.Target != "local" || setResp.ActiveTarget.Temporary {
		t.Fatalf("expected local target, got %+v", setResp.ActiveTarget)
	}
}

func TestBotBindingHTTPAndNodeExecForwarding(t *testing.T) {
	s, h := newTestServer(t, `{
		"http": {"host": "127.0.0.1", "port": 0},
		"bots": [{"id": "a", "name": "A", "harness": "opencode"}]
	}`)

	// 1. PATCH /api/bots/{id} updates desktopTarget and browserMode.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/bots/a", strings.NewReader(`{"desktopTarget":"linux-box","browserMode":"isolated"}`))
	req.SetPathValue("id", "a")
	s.handleUpdateBot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /api/bots/a status=%d body=%s", rec.Code, rec.Body.String())
	}
	var botResp struct {
		DesktopTarget string `json:"desktopTarget"`
		BrowserMode   string `json:"browserMode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &botResp); err != nil {
		t.Fatal(err)
	}
	if botResp.DesktopTarget != "linux-box" || botResp.BrowserMode != "isolated" {
		t.Fatalf("unexpected updated bot fields: %+v", botResp)
	}

	// 2. POST /api/desktop/task returns 503 when bound remote node is offline.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/desktop/task", strings.NewReader(`{"botId":"a","goal":"check"}`))
	s.handleDesktopTask(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for offline bound node, got status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 3. Connect linux-box node with desktop + exec perms, verify GET /api/desktop/targets reports virtualSupported
	// and node_exec forwards AGENT_BROWSER_SESSION & browserProfileBotId.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotCmdCh := make(chan node.Command, 1)
	go func() {
		cmd, err := h.Nodes().Poll(ctx, node.PollRequest{
			ID:              "linux-box",
			Name:            "Linux Box",
			OS:              "linux",
			Arch:            "amd64",
			Permissions:     []string{node.PermExec, node.PermDesktop},
			ProtocolVersion: protocol.CurrentProtocol,
			Capabilities: []string{
				protocol.CapNodeExecEnv,
				protocol.CapNodeBrowserProfile,
				protocol.CapNodeVirtualSlots,
			},
		}, 2*time.Second)
		if err == nil && cmd != nil {
			gotCmdCh <- *cmd
			_ = h.Nodes().Deliver(node.Result{CallID: cmd.CallID, NodeID: "linux-box", OK: true, Output: "done"})
		}
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var connected bool
		for _, n := range h.Nodes().List() {
			if n.ID == "linux-box" && n.Connected {
				connected = true
				break
			}
		}
		if connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/desktop/targets", nil)
	s.handleDesktopTargets(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"virtualSupported":true`) {
		t.Fatalf("expected virtualSupported=true for linux node, body=%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/nodes/_default/call", strings.NewReader(`{"botId":"a","action":"exec","args":{"command":"agent-browser get url"}}`))
	req.SetPathValue("id", "_default")
	s.handleNodeCall(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleNodeCall status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case cmd := <-gotCmdCh:
		var args struct {
			Env                 map[string]string `json:"env"`
			BrowserProfileBotID string            `json:"browserProfileBotId"`
		}
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			t.Fatal(err)
		}
		if args.Env["AGENT_BROWSER_SESSION"] != "bot-a" || args.BrowserProfileBotID != "a" {
			t.Fatalf("unexpected forwarded exec args: %+v", args)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for node exec command")
	}
}
