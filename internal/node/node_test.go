package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bot-hub/internal/protocol"
)

func TestRegistryAndAgentEndToEnd(t *testing.T) {
	var changes atomic.Int32
	reg := NewRegistry(func() { changes.Add(1) })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/nodes/poll", func(w http.ResponseWriter, r *http.Request) {
		var req PollRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cmd, err := reg.Poll(r.Context(), req, 5*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if cmd == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(cmd)
	})
	mux.HandleFunc("POST /api/nodes/result", func(w http.ResponseWriter, r *http.Request) {
		var res Result
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reg.Deliver(res)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/nodes/disconnect", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reg.Disconnect(req.ID)
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	agent := NewAgent(AgentOptions{
		NodeID:      "macbook",
		Name:        "MacBook Pro",
		BaseURL:     srv.URL,
		Permissions: []string{PermFiles, PermExec},
		Enabled:     true,
	})
	defer agent.Close()

	// Wait until the node shows up as connected.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list := reg.List()
		if len(list) == 1 && list[0].Connected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	list := reg.List()
	if len(list) != 1 || !list[0].Connected {
		t.Fatalf("expected 1 connected node, got %+v", list)
	}
	if list[0].ProtocolVersion != protocol.CurrentProtocol || !list[0].HasCapability(protocol.CapNodeExecEnv) || !list[0].HasCapability(protocol.CapNodeDesktopPermissions) {
		t.Fatalf("expected current protocol & capabilities on node, got %+v", list[0])
	}
	for _, act := range []string{"desktop_permissions", "desktop_permission_request"} {
		if perm, err := RequiredPerm(act); err != nil || perm != PermDesktop {
			t.Fatalf("RequiredPerm(%q) = (%q, %v), want (%q, nil)", act, perm, err, PermDesktop)
		}
	}

	// Auto-select single connected node when nodeID is "".
	res, resolvedID, err := reg.Call(context.Background(), "", "exec", map[string]string{"command": "echo hello-node"}, 10*time.Second)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if resolvedID != "macbook" || !res.OK || !strings.Contains(res.Output, "hello-node") {
		t.Fatalf("unexpected exec result: resolved=%q res=%+v", resolvedID, res)
	}

	// File write, read, and list inside TempDir.
	dir := t.TempDir()
	fp := filepath.Join(dir, "sub", "note.txt")
	res, _, err = reg.Call(context.Background(), "", "file_write", map[string]any{"path": fp, "content": "alpha"}, 10*time.Second)
	if err != nil || !res.OK {
		t.Fatalf("file_write failed: err=%v res=%+v", err, res)
	}
	res, _, err = reg.Call(context.Background(), "", "file_read", map[string]any{"path": fp}, 10*time.Second)
	if err != nil || !res.OK || res.Output != "alpha" {
		t.Fatalf("file_read failed: err=%v res=%+v", err, res)
	}
	res, _, err = reg.Call(context.Background(), "", "file_list", map[string]any{"path": filepath.Dir(fp)}, 10*time.Second)
	if err != nil || !res.OK || !strings.Contains(res.Output, "note.txt") {
		t.Fatalf("file_list failed: err=%v res=%+v", err, res)
	}

	// Update permissions live: remove PermExec, keep PermFiles.
	agent.Configure(true, "MacBook Pro", []string{PermFiles})
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list = reg.List()
		if len(list) == 1 && len(list[0].Permissions) == 1 && list[0].Permissions[0] == PermFiles {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, _, err := reg.Call(context.Background(), "macbook", "exec", map[string]string{"command": "pwd"}, 5*time.Second); err == nil {
		t.Fatalf("expected permission denial after removing PermExec")
	}

	// Disable node and confirm it immediately disconnects on the hub.
	agent.Configure(false, "", nil)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		list = reg.List()
		if len(list) == 1 && !list[0].Connected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if list[0].Connected {
		t.Fatalf("expected node disconnected after Configure(false)")
	}
	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("written file missing: %v", err)
	}
	if changes.Load() == 0 {
		t.Fatalf("expected onChange callback to fire")
	}
}
