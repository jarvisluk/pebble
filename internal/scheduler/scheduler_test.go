package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
)

type fakeRoutineHarness struct {
	mu      sync.Mutex
	reply   string
	blockCh chan struct{}
	started chan struct{}
	calls   []harness.RunOptions
}

func (f *fakeRoutineHarness) Kind() string                             { return "rtfake" }
func (f *fakeRoutineHarness) Label() string                            { return "RTFake" }
func (f *fakeRoutineHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (f *fakeRoutineHarness) Close()                                   {}

func (f *fakeRoutineHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, opts)
	reply := f.reply
	block := f.blockCh
	started := f.started
	f.mu.Unlock()

	if opts.Ephemeral && started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if opts.Ephemeral && block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if !opts.Ephemeral {
		reply = "main-ack:" + opts.Prompt
	}
	sid := "eph"
	if !opts.Ephemeral {
		sid = "main_1"
	}
	return &harness.TurnResult{
		BotID:      opts.Bot.ID,
		SessionID:  sid,
		Text:       reply,
		DurationMs: 12,
		Usage:      harness.Usage{CostUSD: 0.002, InputTokens: 100, OutputTokens: 20},
	}, nil
}

func newTestSchedulerHub(t *testing.T, fh *fakeRoutineHarness) (*hub.Hub, *state.Store) {
	t.Helper()
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(fh)
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "workspaces", "ops"), Harness: "rtfake"},
		},
	}
	h := hub.New(cfg, store, nil, reg)
	t.Cleanup(h.Close)
	return h, store
}

func TestSchedulerSilentReceiptNotifyNoneAndOverlap(t *testing.T) {
	fh := &fakeRoutineHarness{reply: "`NO_REPLY`"}
	h, store := newTestSchedulerHub(t, fh)
	s := New(h)
	h.SetOnSchedulesChanged(s.Sync)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		s.Wait()
	}()
	s.Start(ctx)

	// 1. Add routine via Hub (hot-reload arms it)
	enabled := true
	r1, err := h.CreateRoutine("ops", config.ScheduleConfig{
		ID:           "disk",
		Name:         "Disk Check",
		EveryMinutes: 30,
		Prompt:       "check df",
		MainDelivery: "receipt",
		Enabled:      &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	stMap := s.Status("ops")
	if stMap[r1.ID].NextRunAt == "" {
		t.Fatalf("expected armed job with NextRunAt after CreateRoutine, got %+v", stMap)
	}

	// 2. NO_REPLY -> status=silent, no timeline message, no main turn
	res, err := s.RunNow(ctx, "ops", "disk")
	if err != nil || res.Status != "silent" {
		t.Fatalf("expected silent run, got %+v err=%v", res, err)
	}
	if msgs, _ := h.Messages("ops", 10); len(msgs) != 0 {
		t.Fatalf("expected 0 timeline messages for NO_REPLY, got %+v", msgs)
	}

	// 3. Real output with receipt delivery -> timeline message, NO main session wakeup yet
	fh.mu.Lock()
	fh.reply = "告警：根分区 92%"
	fh.mu.Unlock()
	res, err = s.RunNow(ctx, "ops", "disk")
	if err != nil || res.Status != "ok" {
		t.Fatalf("expected ok run, got %+v err=%v", res, err)
	}
	msgs, _ := h.Messages("ops", 10)
	if len(msgs) != 1 || msgs[0].From != "routine:Disk Check" {
		t.Fatalf("expected timeline receipt message, got %+v", msgs)
	}
	if _, ok := store.Get("ops"); ok {
		t.Fatal("receipt delivery must not start or wake main session")
	}
	pending, _ := store.ListPendingReceipts("ops", 5)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending receipt, got %d", len(pending))
	}

	// 4. Notify delivery -> triggers main session turn directly
	if _, err := h.UpdateRoutine("ops", "disk", hub.RoutinePatch{
		MainDelivery: strPtr("notify"),
	}); err != nil {
		t.Fatal(err)
	}
	res, err = s.RunNow(ctx, "ops", "disk")
	if err != nil || res.Status != "ok" {
		t.Fatalf("notify run failed: %+v err=%v", res, err)
	}
	if rec, ok := store.Get("ops"); !ok || rec.TurnCount != 1 {
		t.Fatalf("expected main session to receive 1 turn for notify delivery, got %+v ok=%v", rec, ok)
	}

	// 5. None delivery -> does not write timeline or main session
	if _, err := h.UpdateRoutine("ops", "disk", hub.RoutinePatch{
		MainDelivery: strPtr("none"),
	}); err != nil {
		t.Fatal(err)
	}
	msgsBefore, _ := h.Messages("ops", 50)
	if _, err := s.RunNow(ctx, "ops", "disk"); err != nil {
		t.Fatal(err)
	}
	msgsAfter, _ := h.Messages("ops", 50)
	if len(msgsAfter) != len(msgsBefore) {
		t.Fatalf("none delivery should not write main timeline: before=%d after=%d", len(msgsBefore), len(msgsAfter))
	}

	// 6. Single-flight overlap prevention
	fh.mu.Lock()
	fh.blockCh = make(chan struct{})
	fh.started = make(chan struct{})
	fh.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := s.RunNow(ctx, "ops", "disk")
		done <- err
	}()
	<-fh.started

	if _, err := s.RunNow(ctx, "ops", "disk"); err != ErrRoutineAlreadyRunning {
		t.Fatalf("expected ErrRoutineAlreadyRunning on concurrent trigger, got %v", err)
	}
	if !s.Status("ops")["disk"].Running {
		t.Fatal("expected Running=true while executing")
	}
	close(fh.blockCh)
	if err := <-done; err != nil {
		t.Fatalf("blocked run failed: %v", err)
	}

	// 7. Disable & delete hot-unarms job
	dis := false
	if _, err := h.UpdateRoutine("ops", "disk", hub.RoutinePatch{Enabled: &dis}); err != nil {
		t.Fatal(err)
	}
	if s.Status("ops")["disk"].NextRunAt != "" {
		t.Fatalf("disabled routine should not have NextRunAt: %+v", s.Status("ops")["disk"])
	}
	if err := h.DeleteRoutine("ops", "disk"); err != nil {
		t.Fatal(err)
	}
	if len(s.Status("ops")) != 0 {
		t.Fatalf("expected 0 statuses after delete, got %+v", s.Status("ops"))
	}
}

func TestNextDailyTimeAndDuplicateProtection(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 11, 21, 9, 0, 15, 0, loc)
	// Within the 09:00 minute and hasn't run today -> fire immediately.
	if got := nextDailyTime(now, "09:00", false); !got.Equal(now) {
		t.Fatalf("expected immediate fire in 09:00 minute when ranToday=false, got %v", got)
	}
	// Already ran today -> schedule tomorrow 09:00.
	wantTomorrow := time.Date(2026, 11, 22, 9, 0, 0, 0, loc)
	if got := nextDailyTime(now, "09:00", true); !got.Equal(wantTomorrow) {
		t.Fatalf("expected tomorrow 09:00 when ranToday=true, got %v", got)
	}
	// Earlier in the day -> today 09:00.
	morning := time.Date(2026, 11, 21, 8, 30, 0, 0, loc)
	wantToday := time.Date(2026, 11, 21, 9, 0, 0, 0, loc)
	if got := nextDailyTime(morning, "09:00", false); !got.Equal(wantToday) {
		t.Fatalf("expected today 09:00, got %v", got)
	}
}

func strPtr(s string) *string { return &s }

var _ = atomic.Bool{}

func TestSchedulerCronSecondsAndWebhooks(t *testing.T) {
	var received []map[string]any
	var recMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		recMu.Lock()
		received = append(received, body)
		recMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fh := &fakeRoutineHarness{reply: "NO_REPLY"}
	h, _ := newTestSchedulerHub(t, fh)
	s := New(h)
	h.SetOnSchedulesChanged(s.Sync)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		s.Wait()
	}()
	s.Start(ctx)

	// Webhook-only routine with outbound webhookUrl: NO_REPLY must NOT push outbound webhook.
	if _, err := h.CreateRoutine("ops", config.ScheduleConfig{
		ID:             "hook-job",
		Name:           "Hook Job",
		WebhookTrigger: true,
		WebhookURL:     srv.URL,
		Prompt:         "handle event",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := s.TriggerWebhook(ctx, "ops", "hook-job", `{"ok":true}`)
	if err != nil || res.Status != "silent" {
		t.Fatalf("expected silent webhook run, got %+v err=%v", res, err)
	}
	recMu.Lock()
	if len(received) != 0 {
		t.Fatalf("expected 0 outbound webhooks on NO_REPLY, got %d", len(received))
	}
	recMu.Unlock()

	// Real output -> pushes outbound webhook and injects payload in prompt.
	fh.mu.Lock()
	fh.reply = "alert handled"
	fh.mu.Unlock()
	res, err = s.TriggerWebhook(ctx, "ops", "hook-job", `{"event":"deploy"}`)
	if err != nil || res.Status != "ok" {
		t.Fatalf("expected ok webhook run, got %+v err=%v", res, err)
	}
	fh.mu.Lock()
	lastCall := fh.calls[len(fh.calls)-1]
	fh.mu.Unlock()
	if !strings.Contains(lastCall.Prompt, "[Webhook Payload]") || !strings.Contains(lastCall.Prompt, `{"event":"deploy"}`) {
		t.Fatalf("expected webhook payload in prompt, got:\n%s", lastCall.Prompt)
	}
	recMu.Lock()
	if len(received) != 1 || received[0]["event"] != "routine.completed" || received[0]["output"] != "alert handled" {
		t.Fatalf("unexpected outbound webhook payload: %+v", received)
	}
	recMu.Unlock()

	// Second-level cron (@every 1s) arms and computes NextRunAt.
	if _, err := h.CreateRoutine("ops", config.ScheduleConfig{
		ID:     "sec-cron",
		Name:   "Sec Cron",
		Cron:   "@every 1s",
		Prompt: "ping",
	}); err != nil {
		t.Fatal(err)
	}
	st := s.Status("ops")["sec-cron"]
	if st.NextRunAt == "" {
		t.Fatalf("expected NextRunAt for second-level cron, got %+v", st)
	}
}
