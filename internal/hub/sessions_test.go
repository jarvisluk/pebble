package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type fatalSessionHarness struct {
	mu           sync.Mutex
	calls        int
	prompts      []string
	sessIDs      []string
	flushPrompts []string
	failAll      bool
}

func (f *fatalSessionHarness) Kind() string { return "fataltest" }

func (f *fatalSessionHarness) Label() string { return "FatalTest" }

func (f *fatalSessionHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (f *fatalSessionHarness) Close() {}

func (f *fatalSessionHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	if opts.Ephemeral {
		f.mu.Lock()
		f.flushPrompts = append(f.flushPrompts, opts.Prompt)
		f.mu.Unlock()
		return &harness.TurnResult{BotID: opts.Bot.ID, Text: "NOOP"}, nil
	}
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

func (s *slowRotateHarness) Kind() string { return "slowrot" }

func (s *slowRotateHarness) Label() string { return "SlowRot" }

func (s *slowRotateHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (s *slowRotateHarness) Close() {}

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
