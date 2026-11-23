package syncer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/state"
)

func TestShouldSyncPathFilters(t *testing.T) {
	allowed := []string{
		"workspaces/_shared/USER.md",
		"workspaces/assistant/AGENTS.md",
		"workspaces/assistant/memory/notes.md",
		"media/img_123.png",
	}
	for _, p := range allowed {
		if !ShouldSyncPath(p) {
			t.Fatalf("expected path %q to be syncable", p)
		}
	}

	blocked := []string{
		"",
		"/etc/passwd",
		"../secret.txt",
		"workspaces/../.env",
		".env",
		"workspaces/assistant/.env",
		"workspaces/assistant/.env.local",
		"workspaces/assistant/opencode.json",
		"workspaces/assistant/BOT_HUB.md",
		"workspaces/assistant/CLAUDE.md",
		"workspaces/assistant/_shared/USER.md",
		"workspaces/assistant/.git/config",
		"workspaces/assistant/node_modules/pkg/index.js",
		"workspaces/assistant/notes.conflict-macbook.md",
		"workspaces/assistant/data.db",
		"state/hub.db",
		"config/bots.json",
	}
	for _, p := range blocked {
		if ShouldSyncPath(p) {
			t.Fatalf("expected path %q to be blocked", p)
		}
	}
}

func TestWriteSyncedFileConflictPreservation(t *testing.T) {
	tmp := t.TempDir()
	rel := "workspaces/assistant/memory/todo.md"
	abs := filepath.Join(tmp, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("local-newer"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = os.Chtimes(abs, now, now)

	// Incoming is older -> keep local main, save remote as .conflict-mac-mini
	conflict, err := WriteSyncedFile(tmp, rel, "mac-mini", []byte("remote-older"), now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if conflict != "workspaces/assistant/memory/todo.conflict-mac-mini.md" {
		t.Fatalf("unexpected conflict path: %q", conflict)
	}
	gotMain, _ := os.ReadFile(abs)
	if string(gotMain) != "local-newer" {
		t.Fatalf("expected local-newer to remain main, got %q", gotMain)
	}
	gotConflict, _ := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(conflict)))
	if string(gotConflict) != "remote-older" {
		t.Fatalf("expected remote-older in conflict file, got %q", gotConflict)
	}
}

func TestPeerPullAndPushSync(t *testing.T) {
	primaryDir := t.TempDir()
	standbyDir := t.TempDir()

	primaryCfg := &config.HubConfig{
		ProjectRoot:    primaryDir,
		SharedDir:      filepath.Join(primaryDir, "workspaces", "_shared"),
		DefaultHarness: "opencode",
		Bots: []config.BotConfig{
			{ID: "assistant", Name: "Mini Assistant", Workspace: filepath.Join(primaryDir, "workspaces", "assistant"), Harness: "opencode", Model: "m1"},
		},
	}
	standbyCfg := &config.HubConfig{
		ProjectRoot:    standbyDir,
		SharedDir:      filepath.Join(standbyDir, "workspaces", "_shared"),
		DefaultHarness: "claude",
		DefaultModel:   "sonnet",
		Bots:           []config.BotConfig{},
	}

	if err := os.MkdirAll(filepath.Join(primaryDir, "workspaces", "_shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(primaryDir, "workspaces", "_shared", "USER.md"), []byte("user prefs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(primaryDir, "workspaces", "assistant"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(primaryDir, "workspaces", "assistant", "AGENTS.md"), []byte("be helpful"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Secret and glue files must NOT be synced.
	_ = os.WriteFile(filepath.Join(primaryDir, "workspaces", "assistant", "opencode.json"), []byte("{}"), 0o644)
	_ = os.WriteFile(filepath.Join(primaryDir, "workspaces", "assistant", ".env"), []byte("SECRET=1"), 0o600)

	mux := http.NewServeMux()
	primarySvc := New(primaryCfg)
	mux.HandleFunc("GET /api/sync/portable", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(config.ExportPortable(primaryCfg, "mac-mini"))
	})
	mux.HandleFunc("GET /api/sync/manifest", func(w http.ResponseWriter, _ *http.Request) {
		m, _ := BuildManifest(primaryDir, "mac-mini", true)
		_ = json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("GET /api/sync/file", func(w http.ResponseWriter, r *http.Request) {
		data, _, err := ReadSyncedFile(primaryDir, r.URL.Query().Get("path"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	_ = primarySvc

	srv := httptest.NewServer(mux)
	defer srv.Close()

	standbySvc := New(standbyCfg)
	summary, err := standbySvc.PullFromPeer(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.FilesSynced != 2 || summary.PortableBots != 1 {
		t.Fatalf("unexpected sync summary: %+v", summary)
	}
	if len(standbyCfg.Bots) != 1 || standbyCfg.Bots[0].Harness != "claude" || standbyCfg.Bots[0].Model != "sonnet" {
		t.Fatalf("expected standby bot to inherit local harness/model, got %+v", standbyCfg.Bots)
	}
	if _, err := os.Stat(filepath.Join(standbyDir, "workspaces", "assistant", "AGENTS.md")); err != nil {
		t.Fatalf("expected AGENTS.md synced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(standbyDir, "workspaces", "assistant", "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("opencode.json should not be synced")
	}
	if _, err := os.Stat(filepath.Join(standbyDir, "workspaces", "assistant", ".env")); !os.IsNotExist(err) {
		t.Fatalf(".env should not be synced")
	}
}

func TestGitCheckpointPushAndPull(t *testing.T) {
	ctx := context.Background()
	bareDir := t.TempDir()
	if _, err := runGit(ctx, bareDir, "init", "--bare", "-b", "main"); err != nil {
		t.Skipf("git bare init failed: %v", err)
	}

	primaryDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(primaryDir, "workspaces", "_shared"), 0o755)
	_ = os.WriteFile(filepath.Join(primaryDir, "workspaces", "_shared", "USER.md"), []byte("shared rules"), 0o644)
	_ = os.WriteFile(filepath.Join(primaryDir, ".env"), []byte("API_KEY=secret"), 0o600)
	_ = os.MkdirAll(filepath.Join(primaryDir, "state"), 0o755)
	_ = os.WriteFile(filepath.Join(primaryDir, "state", "hub.db"), []byte("sqlite"), 0o600)

	primaryCfg := &config.HubConfig{
		ProjectRoot: primaryDir,
		Sync: &config.SyncConfig{
			Role: "primary",
			Git:  &config.SyncGitConfig{Enabled: true, RepoURL: bareDir, Branch: "main"},
		},
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops Bot", Harness: "opencode", Model: "m1"},
		},
	}

	pushRes, err := CheckpointPush(ctx, primaryCfg, "mac-mini")
	if err != nil {
		t.Fatalf("CheckpointPush failed: %v", err)
	}
	if !pushRes.Committed || !pushRes.Pushed {
		t.Fatalf("expected committed and pushed, got %+v", pushRes)
	}

	// Verify .env and state/hub.db were NOT tracked in git.
	lsFiles, err := runGit(ctx, primaryDir, "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lsFiles, ".env") || strings.Contains(lsFiles, "hub.db") {
		t.Fatalf("sensitive files leaked into git: %s", lsFiles)
	}

	// Second machine pulls from bare repo.
	standbyDir := t.TempDir()
	standbyCfg := &config.HubConfig{
		ProjectRoot:    standbyDir,
		DefaultHarness: "claude",
		DefaultModel:   "sonnet",
		Sync: &config.SyncConfig{
			Role: "standby",
			Git:  &config.SyncGitConfig{Enabled: true, RepoURL: bareDir, Branch: "main"},
		},
	}
	pullRes, err := CheckpointPull(ctx, standbyCfg)
	if err != nil {
		t.Fatalf("CheckpointPull failed: %v", err)
	}
	if !pullRes.Pulled || !pullRes.ConfigChange || len(pullRes.AddedBots) != 1 || pullRes.AddedBots[0] != "ops" {
		t.Fatalf("unexpected pull result: %+v", pullRes)
	}
	gotUser, err := os.ReadFile(filepath.Join(standbyDir, "workspaces", "_shared", "USER.md"))
	if err != nil || string(gotUser) != "shared rules" {
		t.Fatalf("expected USER.md restored, got %q (%v)", gotUser, err)
	}
}

func TestWebDAVReadOnlyAndPropfind(t *testing.T) {
	wsRoot := t.TempDir()
	_ = os.MkdirAll(filepath.Join(wsRoot, "assistant"), 0o755)
	_ = os.WriteFile(filepath.Join(wsRoot, "assistant", "AGENTS.md"), []byte("hello dav"), 0o644)
	_ = os.WriteFile(filepath.Join(wsRoot, "assistant", ".env"), []byte("SECRET=1"), 0o600)

	h := NewWebDAVHandler(wsRoot, true)

	// PROPFIND on /assistant/
	req := httptest.NewRequest("PROPFIND", "/assistant/", nil)
	req.Header.Set("Depth", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AGENTS.md") || strings.Contains(rec.Body.String(), ".env") {
		t.Fatalf("unexpected PROPFIND body:\n%s", rec.Body.String())
	}

	// GET allowed file
	req = httptest.NewRequest(http.MethodGet, "/assistant/AGENTS.md", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello dav" {
		t.Fatalf("GET AGENTS.md status=%d body=%q", rec.Code, rec.Body.String())
	}

	// PUT blocked in read-only mode
	req = httptest.NewRequest(http.MethodPut, "/assistant/new.txt", strings.NewReader("nope"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on read-only PUT, got %d", rec.Code)
	}
}

func TestBidirectionalDeltaSyncWithStores(t *testing.T) {
	primaryDir := t.TempDir()
	standbyDir := t.TempDir()

	pStore, err := state.NewStore(filepath.Join(primaryDir, "state", "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer pStore.Close()
	sStore, err := state.NewStore(filepath.Join(standbyDir, "state", "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer sStore.Close()

	_, _ = pStore.AppendMessage(state.ChatMessage{
		ID:        "p_msg_1",
		BotID:     "assistant",
		Role:      "user",
		Text:      "from primary",
		CreatedAt: "2026-11-24T01:00:00Z",
	})
	_, _ = pStore.RecordRoutineRun(state.RoutineRun{
		ID:         "p_run_1",
		BotID:      "assistant",
		RoutineID:  "check",
		StartedAt:  "2026-11-24T01:01:00Z",
		FinishedAt: "2026-11-24T01:01:01Z",
	})

	primaryCfg := &config.HubConfig{
		ProjectRoot:    primaryDir,
		SharedDir:      filepath.Join(primaryDir, "workspaces", "_shared"),
		DefaultHarness: "opencode",
		Bots: []config.BotConfig{
			{ID: "assistant", Name: "Assistant", Workspace: filepath.Join(primaryDir, "workspaces", "assistant")},
		},
	}
	standbyCfg := &config.HubConfig{
		ProjectRoot:    standbyDir,
		SharedDir:      filepath.Join(standbyDir, "workspaces", "_shared"),
		DefaultHarness: "claude",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sync/portable", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(config.ExportPortable(primaryCfg, "mac-mini"))
	})
	mux.HandleFunc("POST /api/sync/portable", func(w http.ResponseWriter, r *http.Request) {
		var b config.PortableBundle
		_ = json.NewDecoder(r.Body).Decode(&b)
		changed, _, _, _ := config.ApplyPortable(primaryCfg, b)
		_ = json.NewEncoder(w).Encode(map[string]any{"changed": changed})
	})
	mux.HandleFunc("GET /api/sync/manifest", func(w http.ResponseWriter, _ *http.Request) {
		m, _ := BuildManifest(primaryDir, "mac-mini", true)
		_ = json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("GET /api/sync/file", func(w http.ResponseWriter, r *http.Request) {
		data, _, err := ReadSyncedFile(primaryDir, r.URL.Query().Get("path"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	mux.HandleFunc("PUT /api/sync/file", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/sync/delta", func(w http.ResponseWriter, r *http.Request) {
		afterMsg, _ := strconv.ParseInt(r.URL.Query().Get("afterMsgId"), 10, 64)
		afterRun := r.URL.Query().Get("afterRunAt")
		msgs, nextMsg, _ := pStore.ExportMessagesSince(afterMsg, 500)
		runs, nextRun, _ := pStore.ExportRoutineRunsSince(afterRun, 500)
		_ = json.NewEncoder(w).Encode(state.SyncDelta{
			Messages:      msgs,
			NextMsgCursor: nextMsg,
			RoutineRuns:   runs,
			NextRunCursor: nextRun,
		})
	})
	mux.HandleFunc("POST /api/sync/delta", func(w http.ResponseWriter, r *http.Request) {
		var d state.SyncDelta
		_ = json.NewDecoder(r.Body).Decode(&d)
		mIns, _ := pStore.ImportMessages(d.Messages)
		rIns, _ := pStore.ImportRoutineRuns(d.RoutineRuns)
		_ = json.NewEncoder(w).Encode(map[string]any{"messagesInserted": mIns, "routineRunsInserted": rIns})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	standbySvc := NewWithStore(standbyCfg, sStore)
	pullSum, err := standbySvc.PullFromPeer(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if pullSum.MessagesSynced != 1 || pullSum.RoutineRunsSynced != 1 {
		t.Fatalf("expected 1 msg and 1 run pulled, got %+v", pullSum)
	}

	// Standby creates a new message while taken over, then pushes back to primary.
	_, _ = sStore.AppendMessage(state.ChatMessage{
		ID:        "s_msg_2",
		BotID:     "assistant",
		Role:      "user",
		Text:      "from standby takeover",
		CreatedAt: "2026-11-24T01:05:00Z",
	})
	pushSum, err := standbySvc.PushToPeer(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if pushSum.MessagesSynced != 1 {
		t.Fatalf("expected 1 new message pushed back, got %+v", pushSum)
	}

	// Second push should be a no-op (cursor advanced).
	pushSum2, err := standbySvc.PushToPeer(context.Background(), srv.Client(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if pushSum2.MessagesSynced != 0 || pushSum2.RoutineRunsSynced != 0 {
		t.Fatalf("expected 0 synced on second push, got %+v", pushSum2)
	}
}
