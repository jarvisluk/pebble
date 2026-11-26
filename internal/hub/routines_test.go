package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type isolatedTestHarness struct {
	mu          sync.Mutex
	mainStarted chan struct{}
	mainRelease chan struct{}
	calls       []harness.RunOptions
	failNext    atomic.Bool
}

func (i *isolatedTestHarness) Kind() string { return "isotest" }

func (i *isolatedTestHarness) Label() string { return "IsoTest" }

func (i *isolatedTestHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (i *isolatedTestHarness) Close() {}

func (i *isolatedTestHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	i.mu.Lock()
	i.calls = append(i.calls, opts)
	i.mu.Unlock()

	if !opts.Ephemeral && i.mainStarted != nil && i.mainRelease != nil {
		select {
		case <-i.mainStarted:
		default:
			close(i.mainStarted)
		}
		select {
		case <-i.mainRelease:
		case <-ctx.Done():
			return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "main_ses"}, ctx.Err()
		}
	}
	if i.failNext.Swap(false) {
		return nil, errors.New("transient failure")
	}
	sid := opts.SessionID
	if opts.Ephemeral {
		sid = "eph_1"
	} else if sid == "" {
		sid = "main_ses"
	}
	return &harness.TurnResult{
		BotID:     opts.Bot.ID,
		SessionID: sid,
		Text:      "reply:" + opts.Prompt,
	}, nil
}

func TestRunIsolatedConcurrentAndReceiptInjection(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	ih := &isolatedTestHarness{
		mainStarted: make(chan struct{}),
		mainRelease: make(chan struct{}),
	}
	reg := harness.NewRegistry()
	reg.Register(ih)

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots:        []config.BotConfig{{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "workspaces", "ops"), Harness: "isotest", Model: "main-model"}},
	}
	h := New(cfg, store, nil, reg)
	defer h.Close()

	// Start a blocking main turn.
	mainDone := make(chan error, 1)
	go func() {
		_, err := h.Send(context.Background(), "ops", "main turn", SendOptions{})
		mainDone <- err
	}()
	<-ih.mainStarted

	// RunIsolated must NOT block on main turn and must pass Ephemeral=true, SessionID="", and override model.
	isoRes, err := h.RunIsolated(context.Background(), "ops", "check disk", IsolatedOptions{
		Source: "schedule:disk",
		Model:  "cheap-model",
	})
	if err != nil {
		t.Fatalf("RunIsolated failed while main busy: %v", err)
	}
	if isoRes.SessionID != "eph_1" {
		t.Fatalf("unexpected ephemeral session ID: %q", isoRes.SessionID)
	}

	close(ih.mainRelease)
	if err := <-mainDone; err != nil {
		t.Fatalf("main turn failed: %v", err)
	}
	ih.mainStarted = nil
	ih.mainRelease = nil

	// Active main session must still point to main_ses with 1 turn.
	rec, ok := store.Get("ops")
	if !ok || rec.RuntimeSessionID != "main_ses" || rec.TurnCount != 1 {
		t.Fatalf("main active session corrupted by RunIsolated: %+v ok=%v", rec, ok)
	}

	// Record a pending routine receipt and append to timeline.
	run1, err := h.RecordRoutineRun(state.RoutineRun{
		BotID: "ops", RoutineID: "disk", RoutineName: "磁盘巡检",
		Trigger: "schedule", Status: "ok", Delivery: "receipt",
		Output: "发现 /var 使用率 91%",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.AppendRoutineReceipt("ops", "磁盘巡检", run1.Output, 120, 0.001, 50, 20)

	// Next main turn fails: receipt MUST stay pending.
	ih.failNext.Store(true)
	if _, err := h.Send(context.Background(), "ops", "user ask 1", SendOptions{}); err == nil {
		t.Fatal("expected transient failure on turn")
	}
	pending, _ := store.ListPendingReceipts("ops", 5)
	if len(pending) != 1 || pending[0].ID != run1.ID {
		t.Fatalf("receipt should stay pending after failed turn, got %+v", pending)
	}

	// Next main turn succeeds: receipt is injected and marked injected.
	if _, err := h.Send(context.Background(), "ops", "user ask 2", SendOptions{}); err != nil {
		t.Fatalf("second user turn failed: %v", err)
	}
	pending, _ = store.ListPendingReceipts("ops", 5)
	if len(pending) != 0 {
		t.Fatalf("receipt should be marked injected after succeeded turn, got %+v", pending)
	}

	ih.mu.Lock()
	var lastPrompt string
	var ephCall *harness.RunOptions
	for idx := range ih.calls {
		if ih.calls[idx].Ephemeral {
			c := ih.calls[idx]
			ephCall = &c
		} else {
			lastPrompt = ih.calls[idx].Prompt
		}
	}
	ih.mu.Unlock()
	if ephCall == nil || ephCall.Bot.Model != "cheap-model" || ephCall.SessionID != "" {
		t.Fatalf("unexpected ephemeral RunOptions: %+v", ephCall)
	}
	if !strings.Contains(lastPrompt, "[Background routine receipt") || !strings.Contains(lastPrompt, "发现 /var 使用率 91%") || !strings.Contains(lastPrompt, "user ask 2") {
		t.Fatalf("expected receipt injected into main prompt, got:\n%s", lastPrompt)
	}

	// Pending receipt created before /reset is dismissed and NOT carried into new session.
	if _, err := h.RecordRoutineRun(state.RoutineRun{
		BotID: "ops", RoutineID: "stale", RoutineName: "旧巡检",
		Trigger: "schedule", Status: "ok", Delivery: "receipt", Output: "旧警报",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.ResetSession("ops"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Send(context.Background(), "ops", "fresh turn", SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ih.mu.Lock()
	freshPrompt := ih.calls[len(ih.calls)-1].Prompt
	ih.mu.Unlock()
	if strings.Contains(freshPrompt, "旧警报") {
		t.Fatalf("dismissed receipt leaked into post-reset session:\n%s", freshPrompt)
	}
}

func TestRoutineCRUDPreservesEnvRefsOnDisk(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_R_MODEL", "openai/gpt-4o-mini")
	rawCfg := `{"bots":[{"id":"ops","name":"Ops","schedules":[{"id":"morning","name":"Morning","dailyAt":"09:00","prompt":"check","model":"${TEST_R_MODEL}"}]}]}`
	cfgPath := filepath.Join(cfgDir, "bots.json")
	if err := os.WriteFile(cfgPath, []byte(rawCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, loadedPath, err := config.Load(tmp, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(&fakeHarness{})
	cfg.Bots[0].Harness = "fake"

	h := New(cfg, store, nil, reg)
	defer h.Close()

	var changes atomic.Int32
	h.SetOnSchedulesChanged(func() { changes.Add(1) })

	// Update prompt while keeping expanded model -> must preserve ${TEST_R_MODEL} on disk.
	updated, err := h.UpdateRoutine("ops", "morning", RoutinePatch{
		Prompt: ptrString("updated check"),
		Model:  ptrString("openai/gpt-4o-mini"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Model != "openai/gpt-4o-mini" || updated.Prompt != "updated check" {
		t.Fatalf("unexpected updated routine: %+v", updated)
	}
	diskBytes, _ := os.ReadFile(loadedPath)
	diskStr := string(diskBytes)
	if !strings.Contains(diskStr, "${TEST_R_MODEL}") || strings.Contains(diskStr, "openai/gpt-4o-mini") {
		t.Fatalf("env refs were flattened on disk:\n%s", diskStr)
	}

	// Create second routine and delete it
	created, err := h.CreateRoutine("ops", config.ScheduleConfig{
		Name:         "Disk Watch",
		EveryMinutes: 15,
		Prompt:       "df -h",
	})
	if err != nil || created.ID != "disk-watch" {
		t.Fatalf("CreateRoutine failed: %+v err=%v", created, err)
	}
	if err := h.DeleteRoutine("ops", "disk-watch"); err != nil {
		t.Fatal(err)
	}
	list, _ := h.ListRoutines("ops")
	if len(list) != 1 || list[0].ID != "morning" {
		t.Fatalf("unexpected routine list after delete: %+v", list)
	}
	if changes.Load() != 3 {
		t.Fatalf("expected 3 schedule change notifications, got %d", changes.Load())
	}
}

func ptrString(s string) *string { return &s }
