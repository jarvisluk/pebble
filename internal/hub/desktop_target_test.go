package hub

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
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

type desktopTestHarness struct {
	mu      sync.Mutex
	prompts []string
	envs    []map[string]string
	delay   time.Duration
	active  atomic.Int32
	maxConc atomic.Int32
}

func (f *desktopTestHarness) Kind() string                             { return "desktest" }
func (f *desktopTestHarness) Label() string                            { return "DeskTest" }
func (f *desktopTestHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (f *desktopTestHarness) Close()                                   {}
func (f *desktopTestHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	cur := f.active.Add(1)
	for {
		prev := f.maxConc.Load()
		if cur <= prev || f.maxConc.CompareAndSwap(prev, cur) {
			break
		}
	}
	f.mu.Lock()
	f.prompts = append(f.prompts, opts.Prompt)
	cp := make(map[string]string, len(opts.ExtraEnv))
	for k, v := range opts.ExtraEnv {
		cp[k] = v
	}
	f.envs = append(f.envs, cp)
	f.mu.Unlock()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.active.Add(-1)
	return &harness.TurnResult{Text: "ok"}, nil
}

func TestPerBotDesktopBindingOfflineProtectionAndConcurrency(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	msgStore, err := state.NewMessageStore(filepath.Join(tmp, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	dh := &desktopTestHarness{delay: 60 * time.Millisecond}
	reg := harness.NewRegistry()
	reg.Register(dh)
	cfg := &config.HubConfig{
		ProjectRoot:    tmp,
		SharedDir:      filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:      filepath.Join(tmp, "sessions.json"),
		MessagesDir:    filepath.Join(tmp, "messages"),
		DesktopHarness: "desktest",
		Bots: []config.BotConfig{
			{ID: "bot-a", Name: "Bot A", Workspace: filepath.Join(tmp, "workspaces", "bot-a"), Harness: "desktest", DesktopTarget: "node-a"},
			{ID: "bot-b", Name: "Bot B", Workspace: filepath.Join(tmp, "workspaces", "bot-b"), Harness: "desktest", DesktopTarget: "node-b"},
		},
	}
	h := New(cfg, store, msgStore, reg)
	defer h.Close()

	// Offline bound target must return ErrBoundDesktopOffline (never silently fall back to local).
	if _, err := h.ResolveBotDesktopTarget("bot-a", ""); !errors.Is(err, ErrBoundDesktopOffline) {
		t.Fatalf("expected ErrBoundDesktopOffline for offline bound node, got %v", err)
	}
	if _, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{BotID: "bot-a", Goal: "open app"}); !errors.Is(err, ErrBoundDesktopOffline) {
		t.Fatalf("expected RunDesktopTask to fail with ErrBoundDesktopOffline, got %v", err)
	}

	// Bring both remote nodes online and verify concurrent execution across different bound screens.
	cancelA := startFakeNodePoll(t, h, "node-a", "Node A", []string{node.PermDesktop})
	defer cancelA()
	cancelB := startFakeNodePoll(t, h, "node-b", "Node B", []string{node.PermDesktop})
	defer cancelB()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{BotID: "bot-a", Goal: "task A"})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{BotID: "bot-b", Goal: "task B"})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent RunDesktopTask failed: %v", e)
		}
	}
	if dh.maxConc.Load() < 2 {
		t.Fatalf("expected bot-a and bot-b on different bound nodes to run concurrently, maxConc=%d", dh.maxConc.Load())
	}

	// Running two tasks on the same bound target concurrently must return ErrDesktopTaskBusy.
	doneFirst := make(chan error, 1)
	go func() {
		_, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{BotID: "bot-a", Goal: "hold"})
		doneFirst <- err
	}()
	time.Sleep(15 * time.Millisecond)
	if _, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{BotID: "bot-a", Goal: "collide"}); !errors.Is(err, ErrDesktopTaskBusy) {
		t.Fatalf("expected ErrDesktopTaskBusy on same bound target, got %v", err)
	}
	if err := <-doneFirst; err != nil {
		t.Fatal(err)
	}
}

func TestPerBotBrowserEnvIsolation(t *testing.T) {
	h := newTestHubForDesktop(t)

	// Default auto mode injects AGENT_BROWSER_SESSION=bot-<id> without profile.
	envAuto := h.botBrowserEnv(config.BotConfig{ID: "alpha"})
	if envAuto["AGENT_BROWSER_SESSION"] != "bot-alpha" {
		t.Fatalf("expected AGENT_BROWSER_SESSION=bot-alpha, got %+v", envAuto)
	}
	if _, hasProf := envAuto["AGENT_BROWSER_PROFILE"]; hasProf {
		t.Fatalf("did not expect AGENT_BROWSER_PROFILE in auto mode: %+v", envAuto)
	}

	// Isolated mode creates and injects AGENT_BROWSER_PROFILE.
	envIso := h.botBrowserEnv(config.BotConfig{ID: "beta", BrowserMode: "isolated"})
	if envIso["AGENT_BROWSER_SESSION"] != "bot-beta" {
		t.Fatalf("expected AGENT_BROWSER_SESSION=bot-beta, got %+v", envIso)
	}
	wantProf := h.BrowserProfileDir("beta")
	if envIso["AGENT_BROWSER_PROFILE"] != wantProf {
		t.Fatalf("expected AGENT_BROWSER_PROFILE=%q, got %q", wantProf, envIso["AGENT_BROWSER_PROFILE"])
	}

	// Explicit user override in bot.Env is preserved (extraEnv does not overwrite).
	envCustom := h.botBrowserEnv(config.BotConfig{
		ID:          "gamma",
		BrowserMode: "isolated",
		Env: map[string]string{
			"AGENT_BROWSER_SESSION": "custom-sess",
			"AGENT_BROWSER_PROFILE": "/tmp/custom-prof",
		},
	})
	if _, exists := envCustom["AGENT_BROWSER_SESSION"]; exists {
		t.Fatalf("expected explicit AGENT_BROWSER_SESSION override to prevent extraEnv overwrite, got %+v", envCustom)
	}
	if _, exists := envCustom["AGENT_BROWSER_PROFILE"]; exists {
		t.Fatalf("expected explicit AGENT_BROWSER_PROFILE override to prevent extraEnv overwrite, got %+v", envCustom)
	}
}

func TestLegacyLinuxNodeRejectsVirtualSlotWithoutFallback(t *testing.T) {
	h := newTestHubForDesktop(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = h.Nodes().Poll(ctx, node.PollRequest{
			ID:          "legacy-linux",
			Name:        "Legacy Linux",
			OS:          "linux",
			Arch:        "amd64",
			Permissions: []string{node.PermDesktop},
		}, 2*time.Second)
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if n, ok := h.Nodes().Get("legacy-linux"); ok && n.Connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := h.SetActiveDesktopTarget("legacy-linux:virtual:assistant", time.Minute); !errors.Is(err, ErrNodeUpgradeRequired) {
		t.Fatalf("expected ErrNodeUpgradeRequired on SetActiveDesktopTarget, got %v", err)
	}
	if _, err := h.ResolveBotDesktopTarget("assistant", "legacy-linux:virtual:assistant"); !errors.Is(err, ErrNodeUpgradeRequired) {
		t.Fatalf("expected ErrNodeUpgradeRequired on ResolveBotDesktopTarget, got %v", err)
	}
}
