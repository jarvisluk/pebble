package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/memory"
	"bot-hub/internal/state"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryCaptureHarness struct {
	mu       sync.Mutex
	calls    []harness.RunOptions
	dreamOut string
	flushOut string
}

func (m *memoryCaptureHarness) Kind() string                             { return "memcap" }
func (m *memoryCaptureHarness) Label() string                            { return "MemCap" }
func (m *memoryCaptureHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (m *memoryCaptureHarness) Close()                                   {}
func (m *memoryCaptureHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, opts)
	dreamOut := m.dreamOut
	flushOut := m.flushOut
	m.mu.Unlock()

	if opts.Ephemeral {
		if strings.Contains(opts.Prompt, "Dream memory consolidation engine") {
			return &harness.TurnResult{BotID: opts.Bot.ID, Text: dreamOut}, nil
		}
		if strings.Contains(opts.Prompt, "Session context is about to rotate") {
			return &harness.TurnResult{BotID: opts.Bot.ID, Text: flushOut}, nil
		}
		return &harness.TurnResult{BotID: opts.Bot.ID, Text: "isolated-ok"}, nil
	}
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "rt_mem_1", Text: "ok"}, nil
}

func newMemoryTestHub(t *testing.T, capH *memoryCaptureHarness, bots []config.BotConfig) (*Hub, string) {
	t.Helper()
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config", "bots.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"memory":{"embedding":{"enabled":true,"baseUrl":"https://embed.local/v1","apiKey":"${TEST_HUB_EMBED_KEY}","model":"emb-1"}},"bots":[{"id":"a"},{"id":"b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_HUB_EMBED_KEY", "sk-live-secret")

	store, err := state.NewStore(filepath.Join(tmp, "state", "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(capH)

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		ConfigPath:  cfgPath,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "state", "sessions.json"),
		Memory: &config.MemoryConfig{
			Embedding: &config.MemoryEmbedConfig{
				Enabled: true,
				BaseURL: "https://embed.local/v1",
				APIKey:  "sk-live-secret",
				Model:   "emb-1",
			},
		},
		Bots: bots,
	}
	for i := range cfg.Bots {
		if cfg.Bots[i].Workspace == "" {
			cfg.Bots[i].Workspace = filepath.Join(tmp, "workspaces", cfg.Bots[i].ID)
		}
		_ = os.MkdirAll(filepath.Join(cfg.Bots[i].Workspace, "memory", "items"), 0o755)
	}
	_ = os.MkdirAll(filepath.Join(cfg.SharedDir, "memory", "items"), 0o755)

	h := New(cfg, store, nil, reg)
	t.Cleanup(func() { h.Close() })
	return h, cfgPath
}

func TestFrozenSnapshotFirstTurnOnlyClaudeEnvAndRoutineRecursionGuard(t *testing.T) {
	capH := &memoryCaptureHarness{}
	h, _ := newMemoryTestHub(t, capH, []config.BotConfig{
		{ID: "a", Name: "A", Harness: "memcap"},
		{ID: "b", Name: "B", Harness: "memcap", Env: map[string]string{"CLAUDE_CODE_DISABLE_AUTO_MEMORY": "0"}},
	})

	if err := h.SetSharedUserMD("# User\nPrefers terse Go code.\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.MemorySave(context.Background(), "", MemorySaveRequest{
		SaveReq: memory.SaveReq{Scope: memory.ScopeBot, BotID: "a", Kind: memory.KindRule, Title: "Deploy rule", Text: "Always run make test"},
	}); err != nil {
		t.Fatal(err)
	}

	// Turn 1 gets Frozen Snapshot + CLAUDE_CODE_DISABLE_AUTO_MEMORY=1.
	if _, err := h.Send(context.Background(), "a", "hello turn 1", SendOptions{}); err != nil {
		t.Fatal(err)
	}
	// Turn 2 in same session MUST NOT get Frozen Snapshot (preserves prompt cache).
	if _, err := h.Send(context.Background(), "a", "follow up turn 2", SendOptions{}); err != nil {
		t.Fatal(err)
	}
	// Bot b overrides CLAUDE_CODE_DISABLE_AUTO_MEMORY via bot.Env -> extraEnv must not overwrite it.
	if _, err := h.Send(context.Background(), "b", "hello b", SendOptions{}); err != nil {
		t.Fatal(err)
	}
	// Normal routine gets compact Frozen Snapshot; memory_dream and memory_flush skip snapshot.
	if _, err := h.RunIsolated(context.Background(), "a", "daily check", IsolatedOptions{Source: "routine:daily"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RunIsolated(context.Background(), "a", "dream prompt", IsolatedOptions{Source: "memory_dream"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RunIsolated(context.Background(), "a", "flush prompt", IsolatedOptions{Source: "memory_flush"}); err != nil {
		t.Fatal(err)
	}

	capH.mu.Lock()
	defer capH.mu.Unlock()
	if len(capH.calls) != 6 {
		t.Fatalf("expected 6 harness calls, got %d", len(capH.calls))
	}
	if !strings.Contains(capH.calls[0].Prompt, "<pebble_memory_snapshot") || !strings.Contains(capH.calls[0].Prompt, "Prefers terse Go code.") {
		t.Fatalf("expected Frozen Snapshot on Turn 1, got:\n%s", capH.calls[0].Prompt)
	}
	if capH.calls[0].ExtraEnv["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] != "1" {
		t.Fatalf("expected CLAUDE_CODE_DISABLE_AUTO_MEMORY=1 on bot a, got %v", capH.calls[0].ExtraEnv)
	}
	if strings.Contains(capH.calls[1].Prompt, "<pebble_memory_snapshot") {
		t.Fatalf("Turn 2 must NOT re-inject Frozen Snapshot:\n%s", capH.calls[1].Prompt)
	}
	if _, exists := capH.calls[2].ExtraEnv["CLAUDE_CODE_DISABLE_AUTO_MEMORY"]; exists {
		t.Fatalf("expected bot.Env override to prevent extraEnv CLAUDE_CODE_DISABLE_AUTO_MEMORY on bot b: %v", capH.calls[2].ExtraEnv)
	}
	if !strings.Contains(capH.calls[3].Prompt, "<pebble_memory_snapshot") || capH.calls[3].ExtraEnv["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] != "1" {
		t.Fatalf("expected compact snapshot + auto-memory disable on normal routine:\n%s", capH.calls[3].Prompt)
	}
	if strings.Contains(capH.calls[4].Prompt, "<pebble_memory_snapshot") {
		t.Fatalf("memory_dream must skip snapshot injection:\n%s", capH.calls[4].Prompt)
	}
	if strings.Contains(capH.calls[5].Prompt, "<pebble_memory_snapshot") {
		t.Fatalf("memory_flush must skip snapshot injection:\n%s", capH.calls[5].Prompt)
	}
}

func TestPrivateBotMemoryIsolationAndSettingsPersistence(t *testing.T) {
	capH := &memoryCaptureHarness{}
	h, cfgPath := newMemoryTestHub(t, capH, []config.BotConfig{
		{ID: "a", Name: "Private A", Harness: "memcap", PrivateSessions: true},
		{ID: "b", Name: "Public B", Harness: "memcap"},
	})

	saved, err := h.MemorySave(context.Background(), "a", MemorySaveRequest{
		SaveReq: memory.SaveReq{Scope: memory.ScopeBot, BotID: "a", Title: "Secret note", Text: "private launch date"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.MemoryWriteIndex(context.Background(), "a", memory.ScopeBot, "a", "# Index\n- Private\n"); err != nil {
		t.Fatal(err)
	}

	// Cross-bot caller "b" is forbidden on private bot "a".
	if _, err := h.MemorySearch(context.Background(), "b", memory.SearchOpts{Scope: memory.ScopeBot, BotID: "a"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on search, got %v", err)
	}
	if _, err := h.MemoryGet(context.Background(), "b", memory.GetOpts{ID: saved.Item.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on get by id, got %v", err)
	}
	if _, err := h.MemoryGet(context.Background(), "b", memory.GetOpts{Path: "a/memory/MEMORY.md"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on get by path, got %v", err)
	}
	if _, err := h.MemoryGet(context.Background(), "b", memory.GetOpts{Path: "b/../a/memory/MEMORY.md"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on traversal get by path, got %v", err)
	}
	var statCount int
	_ = h.store.DB().QueryRow(`SELECT COUNT(*) FROM memory_stats`).Scan(&statCount)
	if statCount != 0 {
		t.Fatalf("expected traversal blocked before reading/recording memory_stats, got %d rows", statCount)
	}
	newTitle := "Hijacked"
	if _, err := h.MemoryPatch(context.Background(), "b", saved.Item.ID, memory.PatchReq{Title: &newTitle}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on patch, got %v", err)
	}
	if _, err := h.MemoryForget(context.Background(), "b", saved.Item.ID, memory.ForgetOpts{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on forget, got %v", err)
	}
	if _, err := h.MemoryReadIndex(context.Background(), "b", memory.ScopeBot, "a"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on read index, got %v", err)
	}
	if _, err := h.MemoryWriteIndex(context.Background(), "b", memory.ScopeBot, "a", "# bad"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on write index, got %v", err)
	}

	// Global/all search from "b" excludes private bot "a" silently.
	allForB, err := h.MemorySearch(context.Background(), "b", memory.SearchOpts{Scope: memory.ScopeAll, Query: "launch"})
	if err != nil {
		t.Fatal(err)
	}
	if len(allForB.Items) != 0 {
		t.Fatalf("private bot memory leaked into scope=all search for b: %+v", allForB.Items)
	}

	// Private bot Dream attempting scope="global" ADD must stay inside private bot "a" and never touch _shared/memory.
	capH.mu.Lock()
	capH.dreamOut = `{"summary":"private secret dream summary","operations":[{"action":"ADD","scope":"global","kind":"fact","title":"Private secret","text":"never share globally"}]}`
	capH.mu.Unlock()
	dreamRes, err := h.RunMemoryDream(context.Background(), "a", true)
	if err != nil || len(dreamRes) != 1 || dreamRes[0].Error != "" || dreamRes[0].Applied == nil || dreamRes[0].Applied.Added != 1 {
		t.Fatalf("expected private bot dream to succeed within bot scope, got %+v err=%v", dreamRes, err)
	}
	globalList, err := h.MemorySearch(context.Background(), "", memory.SearchOpts{Scope: memory.ScopeGlobal, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(globalList.Items) != 0 {
		t.Fatalf("private bot Dream leaked item into global scope: %+v", globalList.Items)
	}
	botAList, err := h.MemorySearch(context.Background(), "a", memory.SearchOpts{Scope: memory.ScopeBot, BotID: "a", Query: "never share globally"})
	if err != nil || len(botAList.Items) != 1 {
		t.Fatalf("expected sanitized private dream item stored in bot a scope, got %+v err=%v", botAList, err)
	}

	// MemoryStatus enforces privateSessions isolation for cross-bot callers.
	if _, err := h.MemoryStatus(context.Background(), "b", "a"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on MemoryStatus(b, a), got %v", err)
	}
	stForB, err := h.MemoryStatus(context.Background(), "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if stForB.Counts.Active != 0 || stForB.Counts.Bot != 0 {
		t.Fatalf("expected private bot a excluded from aggregate counts for caller b, got %+v", stForB.Counts)
	}
	for _, rec := range stForB.RecentDreams {
		if rec.BotID == "a" || strings.Contains(rec.Summary, "private secret dream summary") {
			t.Fatalf("private bot a dream leaked in MemoryStatus for caller b: %+v", rec)
		}
	}
	stForHuman, err := h.MemoryStatus(context.Background(), "", "")
	if err != nil || stForHuman.Counts.Active < 2 || len(stForHuman.RecentDreams) == 0 {
		t.Fatalf("expected human caller to see full status, got %+v err=%v", stForHuman, err)
	}

	// Settings persistence: preserve ${TEST_HUB_EMBED_KEY} on redacted/empty apiKey and fire schedule sync.
	var synced atomic.Int32
	h.SetOnSchedulesChanged(func() { synced.Add(1) })
	redacted := "********"
	dailyAt := "04:00"
	if err := h.SetMemoryConfig(MemoryConfigPatch{
		Dream:     &MemoryDreamConfigPatch{DailyAt: &dailyAt},
		Embedding: &MemoryEmbedConfigPatch{APIKey: &redacted},
	}); err != nil {
		t.Fatal(err)
	}
	if synced.Load() != 1 {
		t.Fatalf("expected SetMemoryConfig to notify schedules changed, got %d", synced.Load())
	}
	redCfg := h.MemoryConfig()
	if redCfg.Dream.DailyAt != "04:00" || !redCfg.Embedding.HasAPIKey || redCfg.Embedding.APIKeyEnvRef != "${TEST_HUB_EMBED_KEY}" {
		t.Fatalf("unexpected redacted memory config: %+v", redCfg)
	}
	disk, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(disk), "sk-live-secret") || !strings.Contains(string(disk), "${TEST_HUB_EMBED_KEY}") {
		t.Fatalf("expected ${TEST_HUB_EMBED_KEY} preserved on disk:\n%s", string(disk))
	}
}

func TestMemoryDreamAndSessionRotateFlushWiring(t *testing.T) {
	capH := &memoryCaptureHarness{
		dreamOut: `{"summary":"consolidated rule","operations":[{"action":"ADD","kind":"rule","title":"Use Go stdlib","text":"Prefer Go stdlib over heavy deps"}]}`,
		flushOut: "- Decided to ship Pebble native memory\n- Next: verify dream timer",
	}
	h, _ := newMemoryTestHub(t, capH, []config.BotConfig{
		{ID: "a", Name: "A", Harness: "memcap"},
	})

	// Configure dream model & minNewMessages=4.
	dreamModel := "cheap-dream-model"
	minMsgs := 4
	if err := h.SetMemoryConfig(MemoryConfigPatch{
		Dream: &MemoryDreamConfigPatch{Model: &dreamModel, MinNewMessages: &minMsgs},
	}); err != nil {
		t.Fatal(err)
	}

	// Below minNewMessages -> non-forced Dream skips.
	resSkip, err := h.RunMemoryDream(context.Background(), "a", false)
	if err != nil || len(resSkip) != 1 || !resSkip[0].Skipped {
		t.Fatalf("expected non-forced dream skip below minNewMessages, got %+v err=%v", resSkip, err)
	}

	// Run 3 turns (= 6 messages >= 4) so both Dream and RotateSession(>=3 turns) qualify.
	for i := 0; i < 3; i++ {
		if _, err := h.Send(context.Background(), "a", "work turn message", SendOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	// Non-forced Dream now executes, applies ADD, writes diary + MEMORY.md, and advances cursor.
	resRun, err := h.RunMemoryDream(context.Background(), "a", false)
	if err != nil || len(resRun) != 1 || resRun[0].Skipped || resRun[0].Error != "" || resRun[0].Applied == nil || resRun[0].Applied.Added != 1 {
		t.Fatalf("unexpected dream run result: %+v err=%v", resRun, err)
	}
	if resRun[0].DiaryPath == "" || resRun[0].IndexPath == "" {
		t.Fatalf("expected diary and index paths from dream: %+v", resRun[0])
	}
	// Second non-forced Dream immediately skips because cursor advanced!
	resAfter, err := h.RunMemoryDream(context.Background(), "a", false)
	if err != nil || len(resAfter) != 1 || !resAfter[0].Skipped {
		t.Fatalf("expected second dream run to skip after cursor update, got %+v err=%v", resAfter, err)
	}

	// Verify dream model was passed to isolated harness call.
	capH.mu.Lock()
	foundDreamModel := false
	for _, c := range capH.calls {
		if c.Ephemeral && strings.Contains(c.Prompt, "Dream memory consolidation engine") && c.Bot.Model == "cheap-dream-model" {
			foundDreamModel = true
		}
	}
	capH.mu.Unlock()
	if !foundDreamModel {
		t.Fatal("expected Dream run to pass configured dream.Model to RunIsolated")
	}

	// RotateSession without handoff note on TurnCount>=3 triggers async flush into memory/daily/YYYY-MM-DD.md.
	if _, err := h.RotateSession("a", "", false); err != nil {
		t.Fatal(err)
	}
	dailyPath := filepath.Join(h.memory.WorkspacesDir(), "a", "memory", "daily", time.Now().UTC().Format("2006-01-02")+".md")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(dailyPath); err == nil && strings.Contains(string(raw), "Decided to ship Pebble native memory") {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	raw, _ := os.ReadFile(dailyPath)
	t.Fatalf("expected async session flush note in %s, got %q", dailyPath, string(raw))
}
