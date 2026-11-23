package hub

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/state"
)

func newTestHubForDesktop(t *testing.T) *Hub {
	t.Helper()
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	msgStore, err := state.NewMessageStore(filepath.Join(tmp, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(&fakeHarness{})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "assistant", Name: "Assistant", Workspace: filepath.Join(tmp, "workspaces", "assistant"), Harness: "fake"},
		},
	}
	h := New(cfg, store, msgStore, reg)
	t.Cleanup(func() { h.Close() })
	return h
}

func startFakeNodePoll(t *testing.T, h *Hub, id, name string, perms []string) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	go func() {
		close(ready)
		_, _ = h.Nodes().Poll(ctx, node.PollRequest{
			ID:          id,
			Name:        name,
			OS:          "darwin",
			Arch:        "arm64",
			Permissions: perms,
		}, 2*time.Second)
	}()
	<-ready
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, n := range h.Nodes().List() {
			if n.ID == id && n.Connected {
				return cancel
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("node %s did not connect in time", id)
	return cancel
}

func TestActiveDesktopTargetLifecycle(t *testing.T) {
	h := newTestHubForDesktop(t)

	// Default stays "local" even when a remote node connects.
	cancelFilesOnly := startFakeNodePoll(t, h, "macbook-ro", "MacBook RO", []string{node.PermFiles, node.PermExec})
	defer cancelFilesOnly()

	if got := h.ActiveDesktopTarget(); got.Target != "local" || got.Temporary {
		t.Fatalf("expected default local target, got %+v", got)
	}

	// Reject switching to a node without desktop permission.
	if _, err := h.SetActiveDesktopTarget("macbook-ro", time.Minute); err == nil {
		t.Fatalf("expected error switching to node without desktop permission")
	}

	// Connect a node with desktop permission and switch to it.
	cancelMB := startFakeNodePoll(t, h, "macbook", "MacBook Pro", []string{node.PermFiles, node.PermExec, node.PermDesktop})
	defer cancelMB()

	snap, err := h.SetActiveDesktopTarget("macbook", 120*time.Millisecond)
	if err != nil {
		t.Fatalf("SetActiveDesktopTarget(macbook) error: %v", err)
	}
	if snap.Target != "macbook" || !snap.Temporary || snap.Name != "MacBook Pro" || snap.ExpiresAt == "" {
		t.Fatalf("unexpected active target snapshot: %+v", snap)
	}

	// Wait for TTL expiry and verify automatic fallback to "local".
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if h.ActiveDesktopTarget().Target == "local" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if got := h.ActiveDesktopTarget(); got.Target != "local" || got.Temporary {
		t.Fatalf("expected fallback to local after TTL, got %+v", got)
	}

	// Switch again and verify Disconnect forces immediate fallback to "local".
	if _, err := h.SetActiveDesktopTarget("macbook", time.Minute); err != nil {
		t.Fatalf("SetActiveDesktopTarget(macbook) error: %v", err)
	}
	cancelMB()
	h.Nodes().Disconnect("macbook")
	if got := h.ActiveDesktopTarget(); got.Target != "local" || got.Temporary {
		t.Fatalf("expected fallback to local after disconnect, got %+v", got)
	}
}
