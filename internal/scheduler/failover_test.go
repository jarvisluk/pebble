package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
)

type fakeSchedHarness struct{}

func (f *fakeSchedHarness) Kind() string                             { return "fake" }
func (f *fakeSchedHarness) Label() string                            { return "Fake" }
func (f *fakeSchedHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (f *fakeSchedHarness) Close()                                   {}
func (f *fakeSchedHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{
		BotID:     opts.Bot.ID,
		SessionID: "ses_sched",
		Text:      "ok",
	}, nil
}

func newTestFailoverHub(t *testing.T, role string, timeoutSec int) *hub.Hub {
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
	reg.Register(&fakeSchedHarness{})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Sync: &config.SyncConfig{
			Role:               role,
			FailoverTimeoutSec: timeoutSec,
		},
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "workspaces", "ops"), Harness: "fake"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	t.Cleanup(func() { h.Close() })
	return h
}

func TestStandbyFailoverPolicyAndSlotDedupe(t *testing.T) {
	h := newTestFailoverHub(t, "standby", 60)
	s := New(h)

	now := time.Date(2026, 11, 24, 2, 0, 0, 0, time.UTC)
	s.nowFn = func() time.Time { return now }

	primaryAlive := true
	s.SetPrimaryProbe(func(context.Context) bool { return primaryAlive })

	autoSched := config.ScheduleConfig{
		ID:             "check",
		Name:           "Health Check",
		EveryMinutes:   5,
		Prompt:         "check",
		FailoverPolicy: config.RoutineFailoverAuto,
	}
	primaryOnlySched := config.ScheduleConfig{
		ID:             "billing",
		Name:           "Billing Report",
		EveryMinutes:   5,
		Prompt:         "report",
		FailoverPolicy: config.RoutineFailoverPrimaryOnly,
	}

	// 1. Primary is alive -> standby skips auto routine.
	run, err := s.execute(context.Background(), "ops", autoSched, "schedule", "")
	if err != nil || run != nil {
		t.Fatalf("expected skip when primary alive, got run=%+v err=%v", run, err)
	}

	// 2. Primary goes offline, but elapsed < 60s -> standby still waits.
	primaryAlive = false
	now = now.Add(30 * time.Second)
	run, err = s.execute(context.Background(), "ops", autoSched, "schedule", "")
	if err != nil || run != nil {
		t.Fatalf("expected wait before failover timeout, got run=%+v err=%v", run, err)
	}

	// 3. Elapsed >= 60s -> primary_only still skipped, auto takes over.
	now = now.Add(35 * time.Second)
	run, err = s.execute(context.Background(), "ops", primaryOnlySched, "schedule", "")
	if err != nil || run != nil {
		t.Fatalf("expected primary_only skipped on standby, got run=%+v err=%v", run, err)
	}

	run, err = s.execute(context.Background(), "ops", autoSched, "schedule", "")
	if err != nil || run == nil || run.SlotKey == "" {
		t.Fatalf("expected standby to take over auto routine with slotKey, got run=%+v err=%v", run, err)
	}

	// 4. Same slot minute already recorded -> deduplicated.
	run2, err := s.execute(context.Background(), "ops", autoSched, "schedule", "")
	if err != nil || run2 != nil {
		t.Fatalf("expected duplicate slot skipped, got run=%+v err=%v", run2, err)
	}
}

func TestThreeNodePriorityLadderFailover(t *testing.T) {
	h := newTestFailoverHub(t, "standby", 60)
	h.Config().Sync.Priority = 30
	h.Config().Sync.Peers = []config.SyncPeerConfig{
		{ID: "mac-mini", URL: "http://p10", Priority: 10},
		{ID: "macbook", URL: "http://p20", Priority: 20},
	}

	s := New(h)
	now := time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)
	s.nowFn = func() time.Time { return now }

	alive := map[string]bool{"http://p10": false, "http://p20": true}
	s.SetPeerProbe(func(_ context.Context, peerURL string) bool {
		return alive[peerURL]
	})

	sched := config.ScheduleConfig{
		ID:             "sync-check",
		Name:           "Sync Check",
		EveryMinutes:   5,
		Prompt:         "check",
		FailoverPolicy: config.RoutineFailoverAuto,
	}

	// P10 is down, but P20 is alive -> P30 must NOT run!
	run, err := s.executeWithSlot(context.Background(), "ops", sched, "schedule", "", now)
	if err != nil || run != nil {
		t.Fatalf("expected P30 to defer to P20, got run=%+v err=%v", run, err)
	}

	// Both P10 and P20 go down; after timeout, P30 takes over.
	alive["http://p20"] = false
	_, _ = s.executeWithSlot(context.Background(), "ops", sched, "schedule", "", now)
	now = now.Add(65 * time.Second)
	run, err = s.executeWithSlot(context.Background(), "ops", sched, "schedule", "", now)
	if err != nil || run == nil {
		t.Fatalf("expected P30 to take over when P10 and P20 are down, got run=%+v err=%v", run, err)
	}
}
