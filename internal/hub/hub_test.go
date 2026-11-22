package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
)

type fakeHarness struct {
	delay         time.Duration
	activeByBot   sync.Map
	overlapFound  atomic.Bool
	maxConcurrent atomic.Int32
	currentGlobal atomic.Int32
}

func (f *fakeHarness) Kind() string                             { return "fake" }
func (f *fakeHarness) Label() string                            { return "Fake" }
func (f *fakeHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (f *fakeHarness) Close()                                   {}

func (f *fakeHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	if _, loaded := f.activeByBot.LoadOrStore(opts.Bot.ID, true); loaded {
		f.overlapFound.Store(true)
	}
	defer f.activeByBot.Delete(opts.Bot.ID)

	cur := f.currentGlobal.Add(1)
	for {
		prev := f.maxConcurrent.Load()
		if cur <= prev || f.maxConcurrent.CompareAndSwap(prev, cur) {
			break
		}
	}
	defer f.currentGlobal.Add(-1)

	time.Sleep(f.delay)

	sid := opts.SessionID
	if sid == "" {
		sid = "ses_" + opts.Bot.ID
	}
	return &harness.TurnResult{
		BotID:      opts.Bot.ID,
		SessionID:  sid,
		Text:       "echo:" + opts.Prompt,
		DurationMs: f.delay.Milliseconds(),
		Usage:      harness.Usage{CostUSD: 0.01},
	}, nil
}

func TestHubSerializationConcurrencyCollabAndCreate(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	msgStore, err := state.NewMessageStore(filepath.Join(tmp, "messages"))
	if err != nil {
		t.Fatalf("new message store: %v", err)
	}

	fake := &fakeHarness{delay: 40 * time.Millisecond}
	reg := harness.NewRegistry()
	reg.Register(fake)

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "a", Name: "Bot A", Workspace: filepath.Join(tmp, "workspaces", "a"), Harness: "fake"},
			{ID: "b", Name: "Bot B", Workspace: filepath.Join(tmp, "workspaces", "b"), Harness: "fake"},
		},
	}

	h := New(cfg, store, msgStore, reg)
	defer h.Close()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if _, err := h.Send(context.Background(), "a", "turn1", SendOptions{}); err != nil {
			t.Errorf("a turn1: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := h.Send(context.Background(), "a", "turn2", SendOptions{}); err != nil {
			t.Errorf("a turn2: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := h.Send(context.Background(), "b", "turn1", SendOptions{}); err != nil {
			t.Errorf("b turn1: %v", err)
		}
	}()
	wg.Wait()

	if fake.overlapFound.Load() {
		t.Fatalf("same bot executed concurrently; expected strict per-bot serialization")
	}
	if fake.maxConcurrent.Load() < 2 {
		t.Fatalf("expected different bots to run concurrently, maxConcurrent=%d", fake.maxConcurrent.Load())
	}

	recA, ok := store.Get("a")
	if !ok || recA.SessionID != "ses_a" || recA.TurnCount != 2 {
		t.Fatalf("unexpected session record for a: %+v (ok=%v)", recA, ok)
	}

	// Cross-bot messaging
	askRes, err := h.MessageBot(context.Background(), "a", "b", "please summarize", 0)
	if err != nil {
		t.Fatalf("MessageBot failed: %v", err)
	}
	if askRes.SessionID != "ses_b" {
		t.Fatalf("expected forwarded turn to use ses_b, got %q", askRes.SessionID)
	}

	// Verify both timelines recorded collaboration
	msgsA, _ := h.Messages("a", 50)
	hasOut := false
	for _, m := range msgsA {
		if m.Role == "bot_out" && m.To == "bot:b" {
			hasOut = true
		}
	}
	if !hasOut {
		t.Fatalf("expected bot_out message in sender timeline")
	}

	// Verify hop limit blocks infinite loop
	if _, err := h.MessageBot(context.Background(), "a", "b", "loop", MaxBotHopDepth); err == nil || !strings.Contains(err.Error(), "hop limit") {
		t.Fatalf("expected hop limit error, got %v", err)
	}

	// Dynamic CreateBot
	created, err := h.CreateBot(CreateBotRequest{
		ID:           "writer",
		Harness:      "fake",
		Name:         "Writer Bot",
		Description:  "Writes docs",
		Instructions: "# Writer\nWrite clearly.",
	})
	if err != nil {
		t.Fatalf("CreateBot failed: %v", err)
	}
	if created.ID != "writer" {
		t.Fatalf("unexpected created bot: %+v", created)
	}
	detail, err := h.GetBotDetail("writer")
	if err != nil || !strings.Contains(detail.Instructions, "Write clearly.") {
		t.Fatalf("GetBotDetail failed: %+v, err=%v", detail, err)
	}

	// Reset session
	if _, err := h.HandleInput(context.Background(), "a", "/reset", SendOptions{}); err != nil {
		t.Fatalf("/reset failed: %v", err)
	}
	if _, ok := store.Get("a"); ok {
		t.Fatalf("expected session a to be cleared after /reset")
	}
}

// failOnceHarness fails its first turn after opencode already created a session.
type failOnceHarness struct {
	calls     atomic.Int32
	gotSecond string
}

func (f *failOnceHarness) Kind() string                             { return "failonce" }
func (f *failOnceHarness) Label() string                            { return "FailOnce" }
func (f *failOnceHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (f *failOnceHarness) Close()                                   {}

func (f *failOnceHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	if f.calls.Add(1) == 1 {
		return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "ses_first"}, context.DeadlineExceeded
	}
	f.gotSecond = opts.SessionID
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: opts.SessionID, Text: "ok"}, nil
}

func TestFailedTurnKeepsSession(t *testing.T) {
	tmp := t.TempDir()
	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	fake := &failOnceHarness{}
	reg := harness.NewRegistry()
	reg.Register(fake)
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots:        []config.BotConfig{{ID: "a", Name: "A", Workspace: filepath.Join(tmp, "a"), Harness: "failonce"}},
	}
	h := New(cfg, store, msgStore, reg)
	defer h.Close()

	if _, err := h.Send(context.Background(), "a", "hi", SendOptions{}); err == nil {
		t.Fatal("expected first turn to fail")
	}
	if _, err := h.Send(context.Background(), "a", "again", SendOptions{}); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if fake.gotSecond != "ses_first" {
		t.Fatalf("second turn session = %q, want ses_first", fake.gotSecond)
	}
}

func TestUpdateDeleteBotAndSharedUserMD(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "bots.json")
	initialCfg := `{
  "defaultModel": "global-m",
  "bots": [
    {"id": "a", "name": "Bot A", "description": "Old", "harness": "fake"},
    {"id": "b", "name": "Bot B", "harness": "fake"}
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(initialCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(tmp, cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	msgStore, err := state.NewMessageStore(cfg.MessagesDir)
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(&fakeHarness{})

	h := New(cfg, store, msgStore, reg)
	defer h.Close()

	// Empty name should fail
	empty := "   "
	if _, err := h.UpdateBot("a", BotUpdate{Name: &empty}); err == nil {
		t.Fatal("expected error for empty bot name")
	}

	// Update name, description, and instructions
	newName := "Bot Alpha"
	newDesc := "Updated description"
	newInst := "# Alpha\nFollow strict rules."
	st, err := h.UpdateBot("a", BotUpdate{Name: &newName, Description: &newDesc, Instructions: &newInst})
	if err != nil {
		t.Fatalf("UpdateBot failed: %v", err)
	}
	if st.Name != "Bot Alpha" || st.Description != "Updated description" {
		t.Fatalf("unexpected updated status: %+v", st)
	}
	detail, err := h.GetBotDetail("a")
	if err != nil || detail.Instructions != "# Alpha\nFollow strict rules.\n" {
		t.Fatalf("unexpected instructions: %q, err=%v", detail.Instructions, err)
	}

	// Shared USER.md read/write
	if err := h.SetSharedUserMD("Prefer concise Chinese replies."); err != nil {
		t.Fatalf("SetSharedUserMD failed: %v", err)
	}
	if got := h.SharedUserMD(); got != "Prefer concise Chinese replies.\n" {
		t.Fatalf("SharedUserMD = %q", got)
	}

	// DeleteBot removes b from hub and config while preserving its workspace
	wsB := h.BotWorkspace("b")
	if err := h.DeleteBot("b"); err != nil {
		t.Fatalf("DeleteBot(b) failed: %v", err)
	}
	if _, ok := h.GetBot("b"); ok {
		t.Fatal("bot b still present after delete")
	}
	if _, err := os.Stat(wsB); err != nil {
		t.Fatalf("expected workspace %s to remain on disk: %v", wsB, err)
	}
	rawBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var diskCfg config.HubConfig
	if err := json.Unmarshal(rawBytes, &diskCfg); err != nil {
		t.Fatal(err)
	}
	if len(diskCfg.Bots) != 1 || diskCfg.Bots[0].ID != "a" {
		t.Fatalf("unexpected bots on disk after delete: %+v", diskCfg.Bots)
	}

	// Deleting the last remaining bot must fail
	if err := h.DeleteBot("a"); err == nil {
		t.Fatal("expected error when deleting the last bot")
	}
}

type fatalSessionHarness struct {
	mu      sync.Mutex
	calls   int
	prompts []string
	sessIDs []string
	failAll bool
}

func (f *fatalSessionHarness) Kind() string                             { return "fataltest" }
func (f *fatalSessionHarness) Label() string                            { return "FatalTest" }
func (f *fatalSessionHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (f *fatalSessionHarness) Close()                                   {}

func (f *fatalSessionHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	f.mu.Lock()
	f.calls++
	idx := f.calls
	f.prompts = append(f.prompts, opts.Prompt)
	f.sessIDs = append(f.sessIDs, opts.SessionID)
	failAll := f.failAll
	f.mu.Unlock()

	if opts.OnToolUse != nil {
		opts.OnToolUse("Bash")
	}
	if idx == 1 {
		return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "rt_ses_1", Text: "first ok"}, nil
	}
	if idx == 2 || failAll {
		return &harness.TurnResult{
			BotID:     opts.Bot.ID,
			SessionID: opts.SessionID,
			Text:      "partial analysis before overflow",
		}, errors.New("API error: prompt is too long (context_length_exceeded)")
	}
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "rt_ses_2", Text: "recovered answer"}, nil
}

func TestFatalErrorAutoArchiveRetryAndRecall(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	fh := &fatalSessionHarness{}
	reg := harness.NewRegistry()
	reg.Register(fh)

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "a", Name: "Bot A", Workspace: filepath.Join(tmp, "workspaces", "a"), Harness: "fataltest", PrivateSessions: true},
			{ID: "b", Name: "Bot B", Workspace: filepath.Join(tmp, "workspaces", "b"), Harness: "fataltest"},
		},
	}
	h := New(cfg, store, nil, reg)
	defer h.Close()

	if _, err := h.Send(context.Background(), "a", "setup context", SendOptions{}); err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	// Turn 2 hits fatal error on rt_ses_1, auto-archives, and transparently retries once on a fresh session!
	res, err := h.Send(context.Background(), "a", "continue task", SendOptions{})
	if err != nil {
		t.Fatalf("expected transparent retry to succeed, got err=%v", err)
	}
	if res.Text != "recovered answer" || res.SessionID != "rt_ses_2" {
		t.Fatalf("unexpected retry result: %+v", res)
	}
	fh.mu.Lock()
	if fh.calls != 3 || fh.sessIDs[1] != "rt_ses_1" || fh.sessIDs[2] != "" {
		t.Fatalf("unexpected harness calls: calls=%d sessIDs=%v", fh.calls, fh.sessIDs)
	}
	if !strings.Contains(fh.prompts[2], "Use recall_session if you need prior context") || !strings.Contains(fh.prompts[2], "status=error") {
		t.Fatalf("expected breadcrumb in retried turn 1 prompt, got:\n%s", fh.prompts[2])
	}
	fh.mu.Unlock()

	// UI messages should have only 2 user bubbles (no duplicate from retry).
	msgs, err := h.Messages("a", 50)
	if err != nil {
		t.Fatal(err)
	}
	userCount := 0
	for _, m := range msgs {
		if m.Role == "user" {
			userCount++
		}
	}
	if userCount != 2 {
		t.Fatalf("expected 2 user messages in timeline, got %d", userCount)
	}

	// Cross-bot caller "b" must be blocked by PrivateSessions=true on "a".
	if _, err := h.ListSessions("b", state.SessionListOptions{BotID: "a"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden listing private bot a from b, got %v", err)
	}
	if _, err := h.RecallSession("b", state.RecallOptions{BotID: "a", SessionID: "previous"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden recalling private bot a from b, got %v", err)
	}
	// Global search from "b" omits "a" silently.
	globalForB, err := h.ListSessions("b", state.SessionListOptions{BotID: "*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range globalForB {
		if s.BotID == "a" {
			t.Fatalf("private bot a leaked into global search for b: %+v", s)
		}
	}
	// Human caller ("") and self ("a") can recall previous archived error session.
	prevRecall, err := h.RecallSession("a", state.RecallOptions{SessionID: "previous"})
	if err != nil {
		t.Fatalf("self recall previous failed: %v", err)
	}
	if prevRecall.Session.Status != "error" || !strings.Contains(prevRecall.Session.LastError, "context_length_exceeded") {
		t.Fatalf("unexpected archived error session metadata: %+v", prevRecall.Session)
	}
	hasPartial := false
	for _, m := range prevRecall.Messages {
		if strings.Contains(m.Text, "partial analysis before overflow") {
			hasPartial = true
		}
	}
	if !hasPartial {
		t.Fatalf("expected partial text saved in archived error session: %+v", prevRecall.Messages)
	}
}

type slowRotateHarness struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
	prompts []string
	mu      sync.Mutex
}

func (s *slowRotateHarness) Kind() string                             { return "slowrot" }
func (s *slowRotateHarness) Label() string                            { return "SlowRot" }
func (s *slowRotateHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (s *slowRotateHarness) Close()                                   {}

func (s *slowRotateHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	n := s.calls.Add(1)
	s.mu.Lock()
	s.prompts = append(s.prompts, opts.Prompt)
	s.mu.Unlock()
	if n == 1 && s.started != nil {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "rt_1"}, ctx.Err()
		}
	}
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "rt_" + opts.Bot.ID, Text: "done"}, nil
}

func TestDeferredRotateAndMidFlightResetIsolation(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	sh := &slowRotateHarness{started: make(chan struct{}), release: make(chan struct{})}
	reg := harness.NewRegistry()
	reg.Register(sh)

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots:        []config.BotConfig{{ID: "a", Name: "A", Workspace: filepath.Join(tmp, "workspaces", "a"), Harness: "slowrot"}},
	}
	h := New(cfg, store, nil, reg)
	defer h.Close()

	done := make(chan error, 1)
	go func() {
		_, err := h.Send(context.Background(), "a", "turn one", SendOptions{})
		done <- err
	}()
	<-sh.started

	rot, err := h.RotateSession("a", "context got noisy; resume auth refactor", true)
	if err != nil || !rot.Deferred {
		t.Fatalf("expected deferred rotate while busy, got %+v err=%v", rot, err)
	}
	close(sh.release)
	if err := <-done; err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if _, ok := store.Get("a"); ok {
		t.Fatal("expected active session to be archived right after deferred rotate turn finished")
	}

	// Next turn starts fresh session and receives handoffNote in breadcrumb.
	if _, err := h.Send(context.Background(), "a", "turn two", SendOptions{}); err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	sh.mu.Lock()
	p2 := sh.prompts[1]
	sh.mu.Unlock()
	if !strings.Contains(p2, "resume auth refactor") {
		t.Fatalf("expected handoff note in new session turn 1 prompt, got:\n%s", p2)
	}
}

type isolatedTestHarness struct {
	mu          sync.Mutex
	mainStarted chan struct{}
	mainRelease chan struct{}
	calls       []harness.RunOptions
	failNext    atomic.Bool
}

func (i *isolatedTestHarness) Kind() string                             { return "isotest" }
func (i *isolatedTestHarness) Label() string                            { return "IsoTest" }
func (i *isolatedTestHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (i *isolatedTestHarness) Close()                                   {}

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
	if !strings.Contains(lastPrompt, "[后台定时任务回执") || !strings.Contains(lastPrompt, "发现 /var 使用率 91%") || !strings.Contains(lastPrompt, "user ask 2") {
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
	t.Setenv("TEST_R_MODEL", "magpie/flash")
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
		Model:  ptrString("magpie/flash"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Model != "magpie/flash" || updated.Prompt != "updated check" {
		t.Fatalf("unexpected updated routine: %+v", updated)
	}
	diskBytes, _ := os.ReadFile(loadedPath)
	diskStr := string(diskBytes)
	if !strings.Contains(diskStr, "${TEST_R_MODEL}") || strings.Contains(diskStr, "magpie/flash") {
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

type desktopWorkerFakeHarness struct {
	kind    string
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   []harness.RunOptions
}

func (d *desktopWorkerFakeHarness) Kind() string                             { return d.kind }
func (d *desktopWorkerFakeHarness) Label() string                            { return d.kind }
func (d *desktopWorkerFakeHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (d *desktopWorkerFakeHarness) Close()                                   {}

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
		DesktopModel:   "magpie/mac-mini/auto/flash",
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

	// Unblocked run verifies harness, model, and BOT_HUB_MCP_ROLE=computer_use.
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
	if !lastCall.Ephemeral || lastCall.Bot.Harness != "opencode" || lastCall.Bot.Model != "magpie/mac-mini/auto/flash" {
		t.Fatalf("unexpected desktop worker RunOptions: %+v", lastCall)
	}
	if lastCall.ExtraEnv["BOT_HUB_MCP_ROLE"] != "computer_use" || lastCall.MCPEnv["BOT_HUB_MCP_ROLE"] != "computer_use" {
		t.Fatalf("expected BOT_HUB_MCP_ROLE=computer_use in env, got extra=%v mcp=%v", lastCall.ExtraEnv, lastCall.MCPEnv)
	}
}

func TestBotAvatarAndColorCreateUpdatePersist(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "bots.json")
	if err := os.WriteFile(cfgPath, []byte(`{"bots":[{"id":"alpha","name":"Alpha"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(tmp, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(&desktopWorkerFakeHarness{kind: "opencode"})
	h := New(cfg, store, nil, reg)
	defer h.Close()

	st, err := h.CreateBot(CreateBotRequest{
		ID:     "beta",
		Name:   "Beta",
		Avatar: "Cat",
		Color:  "#3B82F6",
	})
	if err != nil {
		t.Fatalf("CreateBot failed: %v", err)
	}
	if st.Avatar != "cat" || st.Color != "#3b82f6" {
		t.Fatalf("unexpected created avatar/color: %+v", st)
	}

	if _, err := h.CreateBot(CreateBotRequest{ID: "bad", Name: "Bad", Avatar: "alien"}); err == nil {
		t.Fatal("expected invalid avatar on CreateBot to fail")
	}
	if _, err := h.UpdateBot("beta", BotUpdate{Color: ptr("not-hex")}); err == nil {
		t.Fatal("expected invalid color on UpdateBot to fail")
	}

	upd, err := h.UpdateBot("beta", BotUpdate{Avatar: ptr("sprout"), Color: ptr("#10B981")})
	if err != nil {
		t.Fatalf("UpdateBot failed: %v", err)
	}
	if upd.Avatar != "sprout" || upd.Color != "#10b981" {
		t.Fatalf("unexpected updated avatar/color: %+v", upd)
	}

	// Reload from disk to verify persistence.
	reloaded, _, err := config.Load(tmp, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var found *config.BotConfig
	for i := range reloaded.Bots {
		if reloaded.Bots[i].ID == "beta" {
			found = &reloaded.Bots[i]
			break
		}
	}
	if found == nil || found.Avatar != "sprout" || found.Color != "#10b981" {
		t.Fatalf("expected persisted avatar/color in bots.json, got %+v", found)
	}

	// Resetting to empty restores default.
	cleared, err := h.UpdateBot("beta", BotUpdate{Avatar: ptr(""), Color: ptr("")})
	if err != nil {
		t.Fatalf("UpdateBot clear failed: %v", err)
	}
	if cleared.Avatar != "" || cleared.Color != "" {
		t.Fatalf("expected empty avatar/color after reset, got %+v", cleared)
	}
}

func ptr(s string) *string { return &s }

type questionTestHarness struct {
	run func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error)
}

func (q *questionTestHarness) Kind() string                             { return "opencode" }
func (q *questionTestHarness) Label() string                            { return "OpenCode" }
func (q *questionTestHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (q *questionTestHarness) Close()                                   {}
func (q *questionTestHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return q.run(ctx, opts)
}

func TestHubQuestionLifecycleAndUnattendedBypass(t *testing.T) {
	tmp := t.TempDir()
	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))

	var mu sync.Mutex
	var sawOnQuestion []bool
	reg := harness.NewRegistry()
	reg.Register(&questionTestHarness{
		run: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			mu.Lock()
			sawOnQuestion = append(sawOnQuestion, opts.OnQuestion != nil)
			mu.Unlock()
			if opts.OnQuestion != nil {
				resp, err := opts.OnQuestion(ctx, harness.QuestionRequest{
					ID:    "frm_1",
					Title: "Confirm",
					Fields: []harness.QuestionField{
						{Key: "q0", Title: "Drink", Type: "string"},
					},
				})
				if err != nil {
					return nil, err
				}
				return &harness.TurnResult{Text: fmt.Sprint(resp.Answers["q0"])}, nil
			}
			return &harness.TurnResult{Text: "unattended"}, nil
		},
	})

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "_shared"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "ops"), Harness: "opencode"},
		},
	}
	h := New(cfg, store, msgStore, reg)
	defer h.Close()

	subID, evCh := h.Subscribe()
	defer h.Unsubscribe(subID)

	type out struct {
		res *harness.TurnResult
		err error
	}
	done := make(chan out, 1)
	go func() {
		r, err := h.Send(context.Background(), "ops", "choose", SendOptions{Source: "user"})
		done <- out{r, err}
	}()

	var qID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items := h.ListQuestions("ops")
		if len(items) == 1 {
			qID = items[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if qID == "" {
		t.Fatalf("expected pending question in hub")
	}

	items, err := h.AnswerQuestion(qID, map[string]any{"q0": "tea"}, false, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("AnswerQuestion failed: items=%+v err=%v", items, err)
	}
	if _, err := h.AnswerQuestion(qID, nil, false, ""); !errors.Is(err, ErrQuestionNotFound) {
		t.Fatalf("expected ErrQuestionNotFound on second answer, got %v", err)
	}

	res := <-done
	if res.err != nil || res.res.Text != "tea" {
		t.Fatalf("unexpected interactive turn result: %+v err=%v", res.res, res.err)
	}

	sawQuestionsEvent := false
	drainDeadline := time.After(500 * time.Millisecond)
drain:
	for {
		select {
		case ev := <-evCh:
			if ev.Type == "questions" {
				sawQuestionsEvent = true
			}
		case <-drainDeadline:
			break drain
		}
	}
	if !sawQuestionsEvent {
		t.Fatalf("expected questions SSE event to be broadcast")
	}

	// Non-user sources (e.g. routine) must not receive OnQuestion.
	res2, err := h.Send(context.Background(), "ops", "cron", SendOptions{Source: "routine:morning"})
	if err != nil || res2.Text != "unattended" {
		t.Fatalf("unexpected unattended turn result: %+v err=%v", res2, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sawOnQuestion) != 2 || !sawOnQuestion[0] || sawOnQuestion[1] {
		t.Fatalf("expected OnQuestion=[true, false], got %v", sawOnQuestion)
	}
}

