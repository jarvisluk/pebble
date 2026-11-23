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
