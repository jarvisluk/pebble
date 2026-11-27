package memory

import (
	"bot-hub/internal/state"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T, cfg Config) (*Store, *state.Store, string) {
	t.Helper()
	wsDir := filepath.Join(t.TempDir(), "workspaces")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := state.NewStore("")
	if err != nil {
		t.Fatalf("state.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if cfg.MagpieConfigDir == "" {
		cfg.MagpieConfigDir = filepath.Join(t.TempDir(), "nonexistent-magpie")
	}
	ms, err := NewStore(wsDir, st, cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return ms, st, wsDir
}

func TestFrontmatterRoundTrip(t *testing.T) {
	origFM := Frontmatter{
		ID:            "mem_abcd1234",
		Scope:         ScopeBot,
		BotID:         "ops-bot",
		Kind:          KindRule,
		Title:         `Deploy rule: never skip "staging" #1`,
		Status:        StatusActive,
		Confidence:    0.95,
		Pin:           true,
		Tags:          []string{"deploy", "staging:check", "中文标签"},
		CreatedAt:     "2026-11-27T08:00:00Z",
		UpdatedAt:     "2026-11-27T09:30:00Z",
		ValidFrom:     "2026-11-27T08:00:00Z",
		ValidUntil:    "",
		SupersededBy:  "",
		SourceSession: "ses_123",
		SourceAuthor:  AuthorUser,
		Why:           "User requested during incident review: avoid outage",
	}
	origBody := "Line one.\n\n## Details\n- Always run canary first.\n- Keep rollback scripts ready."

	rendered := RenderMarkdownItem(origFM, origBody)
	gotFM, gotBody, hasFM := ParseMarkdownItem(rendered)
	if !hasFM {
		t.Fatal("expected frontmatter to be detected")
	}
	if gotFM.ID != origFM.ID || gotFM.Scope != origFM.Scope || gotFM.BotID != origFM.BotID {
		t.Fatalf("identity mismatch: got %+v", gotFM)
	}
	if gotFM.Kind != origFM.Kind || gotFM.Title != origFM.Title || gotFM.Status != origFM.Status {
		t.Fatalf("metadata mismatch: got %+v", gotFM)
	}
	if gotFM.Pin != origFM.Pin || gotFM.Confidence != origFM.Confidence {
		t.Fatalf("pin/confidence mismatch: got %+v", gotFM)
	}
	if gotFM.Why != origFM.Why || gotFM.SourceSession != origFM.SourceSession || gotFM.SourceAuthor != origFM.SourceAuthor {
		t.Fatalf("provenance mismatch: got %+v", gotFM)
	}
	if len(gotFM.Tags) != len(origFM.Tags) || gotFM.Tags[1] != "staging:check" || gotFM.Tags[2] != "中文标签" {
		t.Fatalf("tags round-trip failed: got %#v", gotFM.Tags)
	}
	if gotBody != origBody {
		t.Fatalf("body mismatch:\ngot:  %q\nwant: %q", gotBody, origBody)
	}
}

func TestSecretGuard(t *testing.T) {
	ms, _, _ := newTestStore(t, Config{})
	ctx := context.Background()

	blockedSamples := []struct {
		name string
		text string
	}{
		{"pem private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA"},
		{"openai sk key", "Use key sk-proj-1234567890abcdef1234567890abcdef"},
		{"anthropic key", "Key: sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456"},
		{"github pat", "token ghp_1234567890abcdefghij1234567890abcd"},
		{"google api key", "AIzaSyD-1234567890abcdefghijklmnopqr"},
		{"tavily key", "tvly-prod1234567890abcdef"},
		{"bearer header", "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.abc"},
		{"credential assignment", "api_key = AbCdEf1234567890GhIjKl"},
	}

	for _, tc := range blockedSamples {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ms.Save(ctx, SaveReq{Scope: ScopeGlobal, Text: tc.text})
			if !errors.Is(err, ErrSecretDetected) {
				t.Fatalf("expected ErrSecretDetected for %s, got %v", tc.name, err)
			}
			var secErr *SecretError
			if !errors.As(err, &secErr) || secErr.Code != "secret_detected" {
				t.Fatalf("expected structured *SecretError, got %T %v", err, err)
			}
			// Never echo plaintext secret back in error message.
			if strings.Contains(secErr.Error(), "1234567890") || strings.Contains(secErr.Error(), "AIzaSyD") {
				t.Fatalf("error message leaked secret material: %s", secErr.Error())
			}
		})
	}

	// Placeholders and safe references must be allowed.
	safeSamples := []string{
		"Store API key in ~/.config/bwg/kiwivm.env (permissions 600).",
		"Set api_key = YOUR_API_KEY_HERE before running tests.",
		"Use Authorization: Bearer <token> from $PEBBLE_TOKEN.",
	}
	for _, safe := range safeSamples {
		if _, err := ms.Save(ctx, SaveReq{Scope: ScopeGlobal, Text: safe}); err != nil {
			t.Fatalf("expected safe text to pass SecretGuard, got %v for %q", err, safe)
		}
	}

	// WriteIndex and Patch also enforce SecretGuard.
	if _, err := ms.WriteIndex(ctx, ScopeGlobal, "", "ghp_1234567890abcdefghij1234567890abcd"); !errors.Is(err, ErrSecretDetected) {
		t.Fatalf("expected WriteIndex to reject secret, got %v", err)
	}
}

func TestCRUDIndexJailZeroChurnAndReindex(t *testing.T) {
	ms, st, wsDir := newTestStore(t, Config{})
	ctx := context.Background()

	// Seed read-only _shared/USER.md and forbidden AGENTS.md.
	if err := os.MkdirAll(filepath.Join(wsDir, "_shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	userMDPath := filepath.Join(wsDir, "_shared", "USER.md")
	if err := os.WriteFile(userMDPath, []byte("# User\nPrefers concise Chinese replies.\nLine 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wsDir, "assistant"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "assistant", "AGENTS.md"), []byte("system rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create item.
	res, err := ms.Save(ctx, SaveReq{
		Scope:        ScopeBot,
		BotID:        "assistant",
		Kind:         KindDecision,
		Title:        "SQLite WAL Mode",
		Text:         "Always keep SQLite in WAL mode with busy_timeout=5000.",
		Tags:         []string{"sqlite", "db"},
		Status:       StatusCandidate,
		SourceAuthor: AuthorAgent,
	})
	if err != nil || !res.Created {
		t.Fatalf("Save failed: res=%+v err=%v", res, err)
	}
	itemID := res.Item.ID
	absItemPath := filepath.Join(wsDir, filepath.FromSlash(res.Path))

	// Patch: approve candidate -> active and pin.
	active := StatusActive
	pinTrue := true
	patched, err := ms.Patch(ctx, itemID, PatchReq{Status: &active, Pin: &pinTrue})
	if err != nil || patched.Status != StatusActive || !patched.Pin {
		t.Fatalf("Patch failed: %+v err=%v", patched, err)
	}

	// Patch with empty Title + new Text derives Title from the new Text (not old body).
	emptyTitle := ""
	newText := "SQLite WAL Mode v2\nAlways keep SQLite in WAL mode with busy_timeout=5000."
	patchedTitle, err := ms.Patch(ctx, itemID, PatchReq{Title: &emptyTitle, Text: &newText})
	if err != nil || patchedTitle.Title != "SQLite WAL Mode v2" {
		t.Fatalf("expected title derived from new text, got %+v err=%v", patchedTitle, err)
	}
	patched = patchedTitle

	// Verify zero-churn invariant: reads bump SQLite memory_stats without touching Markdown file.
	infoBefore, err := os.Stat(absItemPath)
	if err != nil {
		t.Fatal(err)
	}
	rawBefore, _ := os.ReadFile(absItemPath)

	got, err := ms.Get(ctx, GetOpts{ID: itemID})
	if err != nil || got.Item == nil || got.Item.ID != itemID {
		t.Fatalf("Get by ID failed: %+v err=%v", got, err)
	}
	if _, err := ms.Search(ctx, SearchOpts{Scope: ScopeBot, BotID: "assistant", Query: "WAL busy_timeout"}); err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	var accessCnt int
	if err := st.DB().QueryRow(`SELECT access_count FROM memory_stats WHERE doc_id = ?`, itemID).Scan(&accessCnt); err != nil || accessCnt < 2 {
		t.Fatalf("expected memory_stats access_count >= 2, got %d err=%v", accessCnt, err)
	}
	infoAfter, _ := os.Stat(absItemPath)
	rawAfter, _ := os.ReadFile(absItemPath)
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) || string(rawAfter) != string(rawBefore) {
		t.Fatal("zero-churn violation: reading memory modified Markdown file on disk")
	}

	// Line-sliced Get on _shared/USER.md works; jail blocks traversal and AGENTS.md.
	userSlice, err := ms.Get(ctx, GetOpts{Path: "_shared/USER.md", StartLine: 1, EndLine: 2})
	if err != nil || !userSlice.Truncated || userSlice.TotalLines != 3 {
		t.Fatalf("Get _shared/USER.md slice unexpected: %+v err=%v", userSlice, err)
	}
	for _, bad := range []string{"../etc/passwd", "/etc/passwd", "assistant/AGENTS.md", "assistant/secret.txt"} {
		if _, err := ms.Get(ctx, GetOpts{Path: bad}); err == nil {
			t.Fatalf("expected path %q to be rejected by jail", bad)
		}
	}

	// ReadIndex / WriteIndex & line limit enforcement.
	idx, err := ms.WriteIndex(ctx, ScopeGlobal, "", "# Global\n- Rule 1\n")
	if err != nil || idx.Lines != 2 || idx.MaxLines != GlobalIndexMaxLines {
		t.Fatalf("WriteIndex unexpected: %+v err=%v", idx, err)
	}
	overLimit := strings.Repeat("- line\n", GlobalIndexMaxLines+5)
	if _, err := ms.WriteIndex(ctx, ScopeGlobal, "", overLimit); !errors.Is(err, ErrIndexTooLarge) {
		t.Fatalf("expected ErrIndexTooLarge, got %v", err)
	}

	// Daily log append.
	dailyRes, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "assistant", Daily: true, Date: "2026-11-27",
		Title: "Migration Check", Text: "Verified SQLite WAL checkpoints.",
	})
	if err != nil || dailyRes.Item.Origin != OriginDaily {
		t.Fatalf("daily Save failed: %+v err=%v", dailyRes, err)
	}

	// Soft Forget with Dismiss stores dismissed fingerprint; Reindex preserves memory_stats & memory_dismissed.
	fg, err := ms.Forget(ctx, itemID, ForgetOpts{Dismiss: true})
	if err != nil || fg.Action != "archived" || !fg.Dismissed {
		t.Fatalf("Forget failed: %+v err=%v", fg, err)
	}
	if !ms.IsDismissed(ctx, ScopeBot, "assistant", patched.Title, patched.Text) {
		t.Fatal("expected dismissed fingerprint to be recorded")
	}

	reStats, err := ms.Reindex(ctx)
	if err != nil || reStats.Indexed < 3 {
		t.Fatalf("Reindex failed: %+v err=%v", reStats, err)
	}
	var preservedAccess int
	if err := st.DB().QueryRow(`SELECT access_count FROM memory_stats WHERE doc_id = ?`, itemID).Scan(&preservedAccess); err != nil || preservedAccess != accessCnt {
		t.Fatalf("Reindex failed to preserve memory_stats: got %d want %d", preservedAccess, accessCnt)
	}
	if !ms.IsDismissed(ctx, ScopeBot, "assistant", patched.Title, patched.Text) {
		t.Fatal("Reindex failed to preserve memory_dismissed")
	}

	// Incremental SyncFromDisk detects unchanged files cleanly.
	inc, err := ms.SyncFromDisk(ctx)
	if err != nil || inc.Indexed != 0 || inc.Unchanged < 3 {
		t.Fatalf("expected incremental SyncFromDisk to skip unchanged files, got %+v err=%v", inc, err)
	}
}

func TestSaveSimilarExistingDetection(t *testing.T) {
	ms, _, _ := newTestStore(t, Config{})
	ctx := context.Background()

	first, err := ms.Save(ctx, SaveReq{
		Scope: ScopeGlobal,
		Kind:  KindRule,
		Title: "Never restart Clash without confirmation",
		Text:  "Always wait for explicit user confirmation before reloading or restarting Clash Verge.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.SimilarExisting) != 0 {
		t.Fatalf("expected no similar items on first save, got %+v", first.SimilarExisting)
	}

	second, err := ms.Save(ctx, SaveReq{
		Scope: ScopeGlobal,
		Kind:  KindRule,
		Title: "Never restart Clash without user confirmation",
		Text:  "Always wait for explicit user confirmation before restarting Clash Verge.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.SimilarExisting) == 0 || second.SimilarExisting[0].ID != first.Item.ID {
		t.Fatalf("expected SimilarExisting to include %s, got %+v", first.Item.ID, second.SimilarExisting)
	}

	// Also verify CJK similarity overlap.
	cjk1, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "ops",
		Title: "数据库备份策略",
		Text:  "每天凌晨三点自动备份数据库到对象存储并保留七天。",
	})
	if err != nil {
		t.Fatal(err)
	}
	cjk2, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "ops",
		Title: "数据库备份策略更新",
		Text:  "每天凌晨三点自动备份数据库到对象存储并保留七天以上。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cjk2.SimilarExisting) == 0 || cjk2.SimilarExisting[0].ID != cjk1.Item.ID {
		t.Fatalf("expected CJK SimilarExisting to include %s, got %+v", cjk1.Item.ID, cjk2.SimilarExisting)
	}
	_ = time.Second
}

func TestConcurrentUpdateConfigAndReads(t *testing.T) {
	ms, _, _ := newTestStore(t, Config{Coexistence: CoexistenceAuto})
	ctx := context.Background()

	if _, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "ops", Kind: KindFact,
		Title: "Concurrency Check", Text: "Ensure runtime config updates do not race with reads.",
	}); err != nil {
		t.Fatal(err)
	}

	baseMagpieDir := ms.cfg.MagpieConfigDir
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			mode := CoexistenceAuto
			enabled := false
			if i%2 == 1 {
				mode = CoexistenceIsolated
				enabled = true
			}
			ms.UpdateConfig(Config{
				Coexistence:      mode,
				SnapshotMaxChars: 3000 + i,
				MagpieConfigDir:  baseMagpieDir,
				Semantic: EmbeddingConfig{
					Enabled: enabled,
					BaseURL: "",
					Model:   "mock-model",
				},
			})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			_, _ = ms.Search(ctx, SearchOpts{Scope: ScopeBot, BotID: "ops", Query: "Concurrency"})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			_, _ = ms.BuildSnapshot(ctx, SnapshotOpts{BotID: "ops", Harness: "claude"})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			_, _ = ms.Status(ctx)
			_ = ms.DetectMagpieMemory(ctx)
		}
	}()

	wg.Wait()
}
