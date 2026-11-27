package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

type desktopWorkerFakeHarness struct {
	kind    string
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   []harness.RunOptions
}

func (d *desktopWorkerFakeHarness) Kind() string { return d.kind }

func (d *desktopWorkerFakeHarness) Label() string { return d.kind }

func (d *desktopWorkerFakeHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (d *desktopWorkerFakeHarness) Close() {}

func (d *desktopWorkerFakeHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	d.mu.Lock()
	d.calls = append(d.calls, opts)
	started := d.started
	release := d.release
	d.mu.Unlock()

	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return &harness.TurnResult{BotID: opts.Bot.ID}, ctx.Err()
		}
	}
	return &harness.TurnResult{
		BotID:     opts.Bot.ID,
		SessionID: "desk_eph",
		Text:      "clicked submit",
	}, nil
}

func TestRunDesktopTaskOverrideGuardAndInterrupt(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	mainH := &desktopWorkerFakeHarness{kind: "claude"}
	deskH := &desktopWorkerFakeHarness{
		kind:    "opencode",
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	reg := harness.NewRegistry()
	reg.Register(mainH)
	reg.Register(deskH)

	cfg := &config.HubConfig{
		ProjectRoot:    tmp,
		SharedDir:      filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:      filepath.Join(tmp, "sessions.json"),
		DesktopHarness: "opencode",
		DesktopModel:   "anthropic/claude-haiku-4-5",
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "workspaces", "ops"), Harness: "claude", Model: "opus"},
		},
	}
	h := New(cfg, store, nil, reg)
	defer h.Close()

	type taskOut struct {
		res *harness.TurnResult
		err error
	}
	done := make(chan taskOut, 1)
	go func() {
		res, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{
			BotID:  "ops",
			Target: "local",
			Goal:   "open Safari",
			App:    "Safari",
		})
		done <- taskOut{res, err}
	}()
	<-deskH.started

	if activeBot := h.ActiveDesktopTaskBot("local"); activeBot != "ops" {
		t.Fatalf("expected ActiveDesktopTaskBot=ops, got %q", activeBot)
	}

	// Re-entrancy / concurrent task on same target must be rejected.
	if _, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{
		BotID:  "ops",
		Target: "local",
		Goal:   "nested call",
	}); !errors.Is(err, ErrDesktopTaskBusy) {
		t.Fatalf("expected ErrDesktopTaskBusy, got %v", err)
	}

	// Interrupt(botID) must cancel the in-flight isolated desktop worker.
	interrupted, err := h.Interrupt("ops")
	if err != nil || !interrupted {
		t.Fatalf("expected Interrupt=true, got %v err=%v", interrupted, err)
	}
	out := <-done
	if !errors.Is(out.err, context.Canceled) {
		t.Fatalf("expected context.Canceled after Interrupt, got %v", out.err)
	}
	if activeBot := h.ActiveDesktopTaskBot("local"); activeBot != "" {
		t.Fatalf("expected active desktop task cleared after exit, got %q", activeBot)
	}

	// Unblocked run verifies harness, model, and PEBBLE_MCP_ROLE=computer_use.
	deskH.mu.Lock()
	deskH.started = nil
	deskH.release = nil
	deskH.mu.Unlock()

	res, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{
		BotID: "ops",
		Goal:  "click button",
	})
	if err != nil || res.Text != "clicked submit" {
		t.Fatalf("RunDesktopTask failed: res=%+v err=%v", res, err)
	}

	deskH.mu.Lock()
	lastCall := deskH.calls[len(deskH.calls)-1]
	deskH.mu.Unlock()
	if !lastCall.Ephemeral || lastCall.Bot.Harness != "opencode" || lastCall.Bot.Model != "anthropic/claude-haiku-4-5" {
		t.Fatalf("unexpected desktop worker RunOptions: %+v", lastCall)
	}
	if lastCall.ExtraEnv["PEBBLE_MCP_ROLE"] != "computer_use" || lastCall.MCPEnv["PEBBLE_MCP_ROLE"] != "computer_use" {
		t.Fatalf("expected PEBBLE_MCP_ROLE=computer_use in env, got extra=%v mcp=%v", lastCall.ExtraEnv, lastCall.MCPEnv)
	}
}
