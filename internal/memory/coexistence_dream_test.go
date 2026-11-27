package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedMockMagpieDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "magpie-cfg")
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	libJSON := `{
  "mcp": [
    {
      "name": "magpie-memory",
      "transport": "stdio",
      "command": "/Applications/magpie.app/Contents/MacOS/magpie",
      "args": ["mcp", "memory"],
      "agents": ["claude", "opencode", "codex"]
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "library.json"), []byte(libJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		"m_bot001.md": `---
kind: rule
scope: project:dir:/Users/dev/pebble/workspaces/ops
status: active
origin: agent
tags: [bot:ops, deploy]
why: "Avoid weekend outages"
created: "2026-11-20T10:00:00Z"
updated: "2026-11-21T10:00:00Z"
---

Never push production schema changes on Friday evenings.
`,
		"m_global01.md": `---
kind: preference
scope: global
status: active
origin: user
tags: [style]
created: "2026-11-18T10:00:00Z"
updated: "2026-11-18T10:00:00Z"
---

Prefer short direct replies without filler preamble.
`,
		"m_secret01.md": `---
kind: fact
scope: global
status: active
tags: [bot:ops]
---

Use ghp_1234567890abcdefghij1234567890abcd for CI.
`,
		"m_archived.md": `---
kind: fact
scope: global
status: archived
---

Old archived fact that should be skipped.
`,
		"m_otherproj.md": `---
kind: fact
scope: project:dir:/Users/dev/unrelated
status: active
tags: [unrelated]
---

Unrelated project fact without bot tag.
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(memDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestMagpieDetectImportAndCoexistenceSnapshot(t *testing.T) {
	magpieDir := seedMockMagpieDir(t)
	ms, _, wsDir := newTestStore(t, Config{
		Coexistence:     CoexistenceAuto,
		MagpieConfigDir: magpieDir,
	})
	ctx := context.Background()

	// 1. DetectMagpieMemory
	det := ms.DetectMagpieMemory(ctx)
	if !det.Installed || !det.HasLegacyItems || det.LegacyCount != 4 {
		t.Fatalf("unexpected Magpie detection: %+v", det)
	}
	if !det.HarnessHasMagpieMemory("claude") || !det.HarnessHasMagpieMemory("opencode") || det.HarnessHasMagpieMemory("pi") {
		t.Fatalf("unexpected HarnessHasMagpieMemory results for %+v", det.InjectedHarnesses)
	}

	// 2. Import bots_only mode migrates #bot:ops, skips global & unrelated, blocks secret.
	impBots, err := ms.ImportFromMagpie(ctx, ImportOpts{Mode: "bots_only"})
	if err != nil {
		t.Fatalf("ImportFromMagpie bots_only failed: %v", err)
	}
	if impBots.Imported != 1 || impBots.BotCounts["ops"] != 1 || impBots.GlobalCount != 0 || impBots.Failed != 1 {
		t.Fatalf("unexpected bots_only import result: %+v", impBots)
	}

	// 3. Re-running with Mode="all" skips already-imported bot item (idempotent) and imports global active item.
	impAll, err := ms.ImportFromMagpie(ctx, ImportOpts{Mode: "all"})
	if err != nil {
		t.Fatalf("ImportFromMagpie all failed: %v", err)
	}
	if impAll.Imported != 1 || impAll.GlobalCount != 1 || impAll.Skipped < 2 {
		t.Fatalf("unexpected all import result: %+v", impAll)
	}
	if impAll.Items[0].SourceAuthor != AuthorMagpieImport {
		t.Fatalf("expected source_author=magpie_import, got %q", impAll.Items[0].SourceAuthor)
	}

	// 4. Seed a native Pebble global item, USER.md, global/bot MEMORY.md, and recent + stale daily logs.
	nativeGlobal, err := ms.Save(ctx, SaveReq{
		Scope: ScopeGlobal, Kind: KindRule,
		Title: "Native Pebble Team Rule", Text: "Always keep tests green before merging.",
		SourceAuthor: AuthorUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.RebuildIndex(ctx, ScopeGlobal, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.RebuildIndex(ctx, ScopeBot, "ops"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "_shared", "USER.md"), []byte("# User\nTimezone: UTC+8\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 27, 15, 0, 0, 0, time.UTC)
	ms.clock = func() time.Time { return now }
	if _, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "ops", Daily: true, Date: "2026-11-27",
		Title: "Canary Rollout", Text: "Rolled out v2.1 to canary.",
	}); err != nil {
		t.Fatal(err)
	}
	// Stale daily log (> 48h old) must not appear in 48h daily headings.
	if _, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "ops", Daily: true, Date: "2026-11-20",
		Title: "Ancient Task", Text: "Very old task.",
	}); err != nil {
		t.Fatal(err)
	}

	// 5. Snapshot with Coexistence="auto" + Magpie-injected harness ("claude"):
	//    - MUST include USER.md, Native Pebble global item, bot MEMORY.md, recent 48h heading
	//    - MUST OMIT global magpie_import item ("Prefer short direct replies...")
	//    - MUST append 2-line single-source boundary directive & privateSessions notice
	snapAuto, err := ms.BuildSnapshot(ctx, SnapshotOpts{
		BotID:           "ops",
		Harness:         "claude",
		PrivateSessions: true,
		Coexistence:     CoexistenceAuto,
		MaxChars:        4000,
		Now:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(snapAuto, `<pebble_memory_snapshot kind="reference-data">`) || !strings.HasSuffix(snapAuto, `</pebble_memory_snapshot>`) {
		t.Fatalf("snapshot missing wrapper tags:\n%s", snapAuto)
	}
	if !strings.Contains(snapAuto, "Timezone: UTC+8") || !strings.Contains(snapAuto, nativeGlobal.Item.Title) {
		t.Fatalf("snapshot missing USER.md or native global item:\n%s", snapAuto)
	}
	if strings.Contains(snapAuto, "Prefer short direct replies") {
		t.Fatalf("expected global magpie_import item to be omitted under auto coexistence:\n%s", snapAuto)
	}
	if !strings.Contains(snapAuto, "Canary Rollout") || strings.Contains(snapAuto, "Ancient Task") {
		t.Fatalf("unexpected 48h daily headings in snapshot:\n%s", snapAuto)
	}
	if !strings.Contains(snapAuto, "save_memory") || !strings.Contains(snapAuto, "privateSessions=true") {
		t.Fatalf("expected coexistence boundary directive in snapshot:\n%s", snapAuto)
	}

	// Deterministic check: calling twice with same inputs yields byte-for-byte identical snapshot.
	snapAuto2, _ := ms.BuildSnapshot(ctx, SnapshotOpts{
		BotID: "ops", Harness: "claude", PrivateSessions: true, Coexistence: CoexistenceAuto, MaxChars: 4000, Now: now,
	})
	if snapAuto != snapAuto2 {
		t.Fatal("expected BuildSnapshot output to be deterministic")
	}

	// Under isolated coexistence (canonical mode shared with Web/Apple/Android/Hub),
	// imported global items are NOT omitted even on a Magpie-injected harness like "claude",
	// and StatusSummary reports coexistence="isolated".
	if got := NormalizeCoexistence("isolated"); got != CoexistenceIsolated {
		t.Fatalf("NormalizeCoexistence(\"isolated\") = %q, want %q", got, CoexistenceIsolated)
	}
	ms.UpdateConfig(Config{Coexistence: CoexistenceIsolated, MagpieConfigDir: magpieDir})
	stIso, err := ms.Status(ctx)
	if err != nil || stIso.Config.Coexistence != CoexistenceIsolated {
		t.Fatalf("expected Status.Config.Coexistence=%q, got %+v err=%v", CoexistenceIsolated, stIso, err)
	}
	snapIsolated, err := ms.BuildSnapshot(ctx, SnapshotOpts{
		BotID: "ops", Harness: "claude", Coexistence: CoexistenceIsolated, MaxChars: 4000, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapIsolated, "Prefer short direct replies") {
		t.Fatalf("expected imported global item under isolated mode:\n%s", snapIsolated)
	}
	if strings.Contains(snapIsolated, "treat ambient Magpie memory as read-only") {
		t.Fatalf("unexpected auto coexistence directive under isolated mode:\n%s", snapIsolated)
	}

	// Legacy pebble_only alias remains compatible.
	snapPebbleOnly, err := ms.BuildSnapshot(ctx, SnapshotOpts{
		BotID: "ops", Harness: "pi", Coexistence: CoexistencePebbleOnly, MaxChars: 4000, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapPebbleOnly, "Prefer short direct replies") {
		t.Fatalf("expected imported global item under pebble_only mode:\n%s", snapPebbleOnly)
	}
}

func TestDreamConsolidationGuardsCursorAndFlush(t *testing.T) {
	ms, _, _ := newTestStore(t, Config{})
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 23, 0, 0, 0, time.UTC)
	ms.clock = func() time.Time { return now }

	// Seed a pinned item (must not be mutated by Dream), a replaceable item, and a dismissed item.
	pinTrue := true
	pinned, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "dev", Kind: KindRule, Pin: &pinTrue,
		Title: "Never force push main", Text: "Force pushing main is strictly forbidden.",
	})
	if err != nil {
		t.Fatal(err)
	}
	oldFact, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "dev", Kind: KindFact,
		Title: "API Port", Text: "Local server runs on port 3000.",
	})
	if err != nil {
		t.Fatal(err)
	}
	toDismiss, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "dev", Kind: KindPreference,
		Title: "Use Tabs", Text: "Use tabs instead of spaces in Python.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Forget(ctx, toDismiss.Item.ID, ForgetOpts{Dismiss: true}); err != nil {
		t.Fatal(err)
	}

	// BuildDreamPrompt & ParseDreamPlan (with markdown JSON fence).
	prompt := BuildDreamPrompt(DreamPromptInput{
		Scope:          ScopeBot,
		BotID:          "dev",
		ExistingItems:  []Item{pinned.Item, oldFact.Item},
		DailyLogs:      []string{"Switched local server port to 3789."},
		DismissedHints: []string{"Use Tabs"},
	})
	if !strings.Contains(prompt, "[PINNED]") || !strings.Contains(prompt, "Dismissed Hints") {
		t.Fatalf("BuildDreamPrompt missing expected sections:\n%s", prompt)
	}

	rawPlan := "Here is the consolidation plan:\n```json\n" + `{
  "summary": "Updated API port and queued global preference candidate.",
  "operations": [
    {"action": "SUPERSEDE", "id": "` + oldFact.Item.ID + `", "kind": "fact", "title": "API Port", "text": "Local server runs on port 3789.", "reason": "port changed"},
    {"action": "ADD", "scope": "global", "kind": "preference", "title": "Concise changelog", "text": "Keep release changelogs concise and grouped by area."},
    {"action": "ADD", "kind": "preference", "title": "Use Tabs", "text": "Use tabs instead of spaces in Python."},
    {"action": "ARCHIVE", "id": "` + pinned.Item.ID + `", "reason": "attempting to archive pinned"},
    {"action": "ADD", "kind": "fact", "title": "Leaked Token", "text": "Use sk-proj-1234567890abcdef1234567890abcdef"},
    {"action": "NOOP"}
  ]
}` + "\n```"

	plan, err := ParseDreamPlan(rawPlan)
	if err != nil {
		t.Fatalf("ParseDreamPlan failed: %v", err)
	}

	applied, err := ms.ApplyDreamPlan(ctx, ScopeBot, "dev", plan, now)
	if err != nil {
		t.Fatalf("ApplyDreamPlan failed: %v", err)
	}
	if applied.Added != 1 || applied.Superseded != 1 || applied.Blocked != 3 || applied.Skipped != 1 {
		t.Fatalf("unexpected DreamApplyResult: %+v", applied)
	}

	// Verify pinned item stayed active.
	pinnedAfter, err := ms.Get(ctx, GetOpts{ID: pinned.Item.ID})
	if err != nil || pinnedAfter.Item.Status != StatusActive {
		t.Fatalf("expected pinned item to remain active, got %+v err=%v", pinnedAfter, err)
	}

	// Verify old fact was marked superseded with superseded_by pointing to replacement.
	oldAfter, err := ms.Get(ctx, GetOpts{ID: oldFact.Item.ID})
	if err != nil || oldAfter.Item.Status != StatusSuperseded || oldAfter.Item.SupersededBy == "" {
		t.Fatalf("expected old fact to be superseded, got %+v err=%v", oldAfter, err)
	}

	// Verify global inferred item was saved as status=candidate and source_author=dream.
	candList, err := ms.Search(ctx, SearchOpts{Scope: ScopeGlobal, Status: StatusCandidate})
	if err != nil || len(candList.Items) != 1 || candList.Items[0].SourceAuthor != AuthorDream {
		t.Fatalf("expected 1 global candidate item from dream, got %+v err=%v", candList, err)
	}

	// Verify MEMORY.md rebuilt and dreams/2026-11-27.md written.
	idx, err := ms.ReadIndex(ctx, ScopeBot, "dev")
	if err != nil || idx.Lines == 0 || idx.Lines > BotIndexMaxLines || !strings.Contains(idx.Content, "API Port") {
		t.Fatalf("unexpected rebuilt bot MEMORY.md: %+v err=%v", idx, err)
	}
	diary, err := ms.Get(ctx, GetOpts{Path: applied.DiaryPath})
	if err != nil || !strings.Contains(diary.Content, "Updated API port") {
		t.Fatalf("unexpected dream audit diary: %+v err=%v", diary, err)
	}

	// DreamCursor round-trip via state.Store meta.
	wantCur := DreamCursor{LastRunAt: now.Format(time.RFC3339), LastMessageRowID: 42, LastDailyDate: "2026-11-27", RunCount: 1}
	if err := ms.SetDreamCursor(ScopeBot, "dev", wantCur); err != nil {
		t.Fatalf("SetDreamCursor failed: %v", err)
	}
	gotCur := ms.GetDreamCursor(ScopeBot, "dev")
	if gotCur != wantCur {
		t.Fatalf("DreamCursor mismatch: got %+v want %+v", gotCur, wantCur)
	}

	// Pre-Rotation / Post-Overflow Flush helpers.
	flushPrompt := BuildFlushPrompt("dev", "context_overflow", "User decided to switch from REST to gRPC.")
	if !strings.Contains(flushPrompt, "context_overflow") || !strings.Contains(flushPrompt, "NOOP") {
		t.Fatalf("unexpected flush prompt: %s", flushPrompt)
	}
	if noopRes, err := ms.ApplyFlushNote(ctx, ScopeBot, "dev", "NOOP", now); err != nil || noopRes != nil {
		t.Fatalf("expected NOOP flush note to be skipped: res=%+v err=%v", noopRes, err)
	}
	flushRes, err := ms.ApplyFlushNote(ctx, ScopeBot, "dev", "Decided to keep REST for external API and gRPC internally.", now)
	if err != nil || flushRes == nil || flushRes.Item.Origin != OriginDaily {
		t.Fatalf("ApplyFlushNote failed: res=%+v err=%v", flushRes, err)
	}
}

func TestDreamCrossBotAndCrossScopeIsolation(t *testing.T) {
	ms, _, _ := newTestStore(t, Config{})
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 23, 30, 0, 0, time.UTC)

	botBItem, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "botB", Kind: KindFact,
		Title: "BotB Secret Plan", Text: "Original BotB note.",
	})
	if err != nil {
		t.Fatal(err)
	}
	globalItem, err := ms.Save(ctx, SaveReq{
		Scope: ScopeGlobal, Kind: KindFact,
		Title: "Global Note", Text: "Original global note.",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Dream scoped to botA attempts to ADD into botB and UPDATE/SUPERSEDE/ARCHIVE botB & global items.
	maliciousPlan := &DreamPlan{
		Summary: "Attempt cross-scope mutations",
		Operations: []DreamOp{
			{Action: DreamActionAdd, Scope: ScopeBot, BotID: "botB", Kind: KindFact, Title: "Injected", Text: "Should be blocked."},
			{Action: DreamActionUpdate, ID: botBItem.Item.ID, Text: "Tampered BotB note."},
			{Action: DreamActionSupersede, ID: botBItem.Item.ID, Text: "Superseded BotB note."},
			{Action: DreamActionArchive, ID: botBItem.Item.ID},
			{Action: DreamActionUpdate, ID: globalItem.Item.ID, Text: "Tampered global note."},
			{Action: DreamActionArchive, ID: globalItem.Item.ID},
		},
	}

	res, err := ms.ApplyDreamPlan(ctx, ScopeBot, "botA", maliciousPlan, now)
	if err != nil {
		t.Fatalf("ApplyDreamPlan failed: %v", err)
	}
	if res.Added != 0 || res.Updated != 0 || res.Superseded != 0 || res.Archived != 0 || res.Blocked != 6 {
		t.Fatalf("expected all 6 cross-scope ops blocked, got %+v", res)
	}

	// Verify botB and global items remain untouched and botB has only its 1 original item.
	gotB, err := ms.Get(ctx, GetOpts{ID: botBItem.Item.ID})
	if err != nil || gotB.Item.Text != "Original BotB note." || gotB.Item.Status != StatusActive {
		t.Fatalf("botB item was mutated: %+v err=%v", gotB, err)
	}
	gotG, err := ms.Get(ctx, GetOpts{ID: globalItem.Item.ID})
	if err != nil || gotG.Item.Text != "Original global note." || gotG.Item.Status != StatusActive {
		t.Fatalf("global item was mutated: %+v err=%v", gotG, err)
	}
	listB, err := ms.Search(ctx, SearchOpts{Scope: ScopeBot, BotID: "botB", Status: "all"})
	if err != nil || len(listB.Items) != 1 {
		t.Fatalf("expected botB to still have exactly 1 item, got %+v err=%v", listB, err)
	}

	// Global Dream run cannot ADD into a bot or mutate a bot item.
	globalPlan := &DreamPlan{
		Summary: "Global dream trying to mutate botB",
		Operations: []DreamOp{
			{Action: DreamActionAdd, Scope: ScopeBot, BotID: "botB", Kind: KindFact, Text: "Blocked bot write."},
			{Action: DreamActionUpdate, ID: botBItem.Item.ID, Text: "Blocked bot update."},
		},
	}
	resG, err := ms.ApplyDreamPlan(ctx, ScopeGlobal, "", globalPlan, now)
	if err != nil {
		t.Fatalf("global ApplyDreamPlan failed: %v", err)
	}
	if resG.Blocked != 2 || resG.Added != 0 || resG.Updated != 0 {
		t.Fatalf("expected global dream cross-bot ops blocked, got %+v", resG)
	}
}
