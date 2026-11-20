package hub

import (
	"context"
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
