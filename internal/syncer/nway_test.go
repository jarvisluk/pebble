package syncer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/state"
)

type testSyncNode struct {
	id   string
	dir  string
	cfg  *config.HubConfig
	st   *state.Store
	svc  *Syncer
	srv  *httptest.Server
	reqs int
}

func newTestSyncNode(t *testing.T, id string) *testSyncNode {
	t.Helper()
	dir := t.TempDir()
	st, err := state.NewStore(filepath.Join(dir, "state", "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SetMeta("sync_host_id", id)
	cfg := &config.HubConfig{
		ProjectRoot:    dir,
		SharedDir:      filepath.Join(dir, "workspaces", "_shared"),
		DefaultHarness: "opencode",
		Bots: []config.BotConfig{
			{ID: "assistant", Name: "Assistant", Workspace: filepath.Join(dir, "workspaces", "assistant")},
		},
	}
	_ = os.MkdirAll(filepath.Join(dir, "workspaces", "assistant", "memory"), 0o755)
	svc := NewWithStore(cfg, st)
	n := &testSyncNode{id: id, dir: dir, cfg: cfg, st: st, svc: svc}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sync/portable", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(config.ExportPortable(n.cfg, n.svc.HostID()))
	})
	mux.HandleFunc("POST /api/sync/portable", func(w http.ResponseWriter, r *http.Request) {
		var b config.PortableBundle
		_ = json.NewDecoder(r.Body).Decode(&b)
		changed, _, _, _ := config.ApplyPortable(n.cfg, b)
		_ = json.NewEncoder(w).Encode(map[string]any{"changed": changed})
	})
	mux.HandleFunc("GET /api/sync/manifest", func(w http.ResponseWriter, _ *http.Request) {
		m, _ := BuildManifestWithStore(n.dir, n.svc.HostID(), n.st, true)
		_ = json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("GET /api/sync/file", func(w http.ResponseWriter, r *http.Request) {
		data, _, err := ReadSyncedFile(n.dir, r.URL.Query().Get("path"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /api/sync/delta", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(state.SyncDelta{})
	})
	mux.HandleFunc("POST /api/sync/delta", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"messagesInserted": 0, "routineRunsInserted": 0})
	})

	n.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		n.srv.Close()
		_ = n.st.Close()
	})
	return n
}

func (n *testSyncNode) writeFile(rel, content string) {
	abs := filepath.Join(n.dir, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte(content), 0o644)
}

func (n *testSyncNode) readFile(rel string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(n.dir, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (n *testSyncNode) hasConflictFiles(relDir string) []string {
	absDir := filepath.Join(n.dir, filepath.FromSlash(relDir))
	entries, _ := os.ReadDir(absDir)
	var out []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".conflict-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestFourNodeChainFastForwardZeroConflicts(t *testing.T) {
	ctx := context.Background()
	a := newTestSyncNode(t, "node-a")
	b := newTestSyncNode(t, "node-b")
	c := newTestSyncNode(t, "node-c")
	d := newTestSyncNode(t, "node-d")

	rel := "workspaces/assistant/memory/plan.md"
	a.writeFile(rel, "v1")
	if _, err := b.svc.PullFromPeer(ctx, b.srv.Client(), a.srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.PullFromPeer(ctx, c.srv.Client(), b.srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.svc.PullFromPeer(ctx, d.srv.Client(), c.srv.URL, ""); err != nil {
		t.Fatal(err)
	}

	// Only A updates plan.md -> B -> C -> D must fast-forward with ZERO .conflict files.
	a.writeFile(rel, "v2-from-a")
	for _, pair := range [][2]*testSyncNode{{b, a}, {c, b}, {d, c}} {
		sum, err := pair[0].svc.PullFromPeer(ctx, pair[0].srv.Client(), pair[1].srv.URL, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(sum.Conflicts) != 0 {
			t.Fatalf("unexpected conflict during chain fast-forward on %s: %v", pair[0].id, sum.Conflicts)
		}
		if got, ok := pair[0].readFile(rel); !ok || got != "v2-from-a" {
			t.Fatalf("%s expected v2-from-a, got %q (%v)", pair[0].id, got, ok)
		}
		if cf := pair[0].hasConflictFiles("workspaces/assistant/memory"); len(cf) != 0 {
			t.Fatalf("%s generated spurious conflict files: %v", pair[0].id, cf)
		}
	}
}

func TestThreeNodeDeleteTombstoneNoResurrection(t *testing.T) {
	ctx := context.Background()
	a := newTestSyncNode(t, "node-a")
	b := newTestSyncNode(t, "node-b")
	c := newTestSyncNode(t, "node-c")

	rel := "workspaces/assistant/memory/temp.md"
	a.writeFile(rel, "initial")
	_, _ = b.svc.PullFromPeer(ctx, b.srv.Client(), a.srv.URL, "")
	_, _ = c.svc.PullFromPeer(ctx, c.srv.Client(), b.srv.URL, "")

	// A deletes temp.md; B syncs from A; C was offline and still has temp.md.
	_ = os.Remove(filepath.Join(a.dir, filepath.FromSlash(rel)))
	if _, err := b.svc.PullFromPeer(ctx, b.srv.Client(), a.srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.readFile(rel); ok {
		t.Fatalf("expected B to delete %s via tombstone", rel)
	}

	// C comes online and syncs from B -> C must delete temp.md and NOT resurrect it.
	if _, err := c.svc.PullFromPeer(ctx, c.srv.Client(), b.srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.readFile(rel); ok {
		t.Fatalf("expected C to delete %s via tombstone", rel)
	}
	// B pulls from C -> still deleted!
	if _, err := b.svc.PullFromPeer(ctx, b.srv.Client(), c.srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.readFile(rel); ok {
		t.Fatalf("deleted file resurrected on B")
	}
}

func TestFourNodeConcurrentConflictConvergesWithoutSecondaryConflicts(t *testing.T) {
	ctx := context.Background()
	a := newTestSyncNode(t, "node-a")
	b := newTestSyncNode(t, "node-b")
	c := newTestSyncNode(t, "node-c")
	d := newTestSyncNode(t, "node-d")

	rel := "workspaces/assistant/memory/shared.md"
	a.writeFile(rel, "base")
	_, _ = b.svc.PullFromPeer(ctx, b.srv.Client(), a.srv.URL, "")
	_, _ = c.svc.PullFromPeer(ctx, c.srv.Client(), b.srv.URL, "")
	_, _ = d.svc.PullFromPeer(ctx, d.srv.Client(), c.srv.URL, "")

	// A and C concurrently edit shared.md while disconnected.
	a.writeFile(rel, "edit-by-a")
	c.writeFile(rel, "edit-by-c")
	// Refresh their manifests so both advance their local vector clocks.
	_, _ = BuildManifestWithStore(a.dir, a.id, a.st, true)
	_, _ = BuildManifestWithStore(c.dir, c.id, c.st, true)

	// B pulls from A (fast-forward), then pulls from C (true concurrent conflict!).
	if _, err := b.svc.PullFromPeer(ctx, b.srv.Client(), a.srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	sumC, err := b.svc.PullFromPeer(ctx, b.srv.Client(), c.srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sumC.Conflicts) != 1 {
		t.Fatalf("expected exactly 1 conflict on B, got %v", sumC.Conflicts)
	}

	// Now D pulls from B -> D must fast-forward the resolved winner with ZERO new conflicts.
	sumD, err := d.svc.PullFromPeer(ctx, d.srv.Client(), b.srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sumD.Conflicts) != 0 {
		t.Fatalf("expected 0 secondary conflicts on D, got %v", sumD.Conflicts)
	}
}

func TestDeleteVsEditConflictPreservesEdit(t *testing.T) {
	ctx := context.Background()
	a := newTestSyncNode(t, "node-a")
	b := newTestSyncNode(t, "node-b")

	rel := "workspaces/assistant/memory/keep.md"
	a.writeFile(rel, "base")
	_, _ = b.svc.PullFromPeer(ctx, b.srv.Client(), a.srv.URL, "")

	// A deletes keep.md, B concurrently edits keep.md.
	_ = os.Remove(filepath.Join(a.dir, filepath.FromSlash(rel)))
	b.writeFile(rel, "important-new-edit")
	_, _ = BuildManifestWithStore(a.dir, a.id, a.st, true)
	_, _ = BuildManifestWithStore(b.dir, b.id, b.st, true)

	// A pulls from B -> edit wins over delete.
	if _, err := a.svc.PullFromPeer(ctx, a.srv.Client(), b.srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	if got, ok := a.readFile(rel); !ok || got != "important-new-edit" {
		t.Fatalf("expected edit to win over delete, got %q (%v)", got, ok)
	}
}

func TestPortableSchedulesMergeByID(t *testing.T) {
	localCfg := &config.HubConfig{
		Bots: []config.BotConfig{
			{
				ID:   "assistant",
				Name: "Assistant",
				Schedules: []config.ScheduleConfig{
					{ID: "local-only", Name: "Local", EveryMinutes: 10, Prompt: "local"},
					{ID: "shared", Name: "Shared", EveryMinutes: 5, Prompt: "old"},
				},
			},
		},
	}
	bundle := config.PortableBundle{
		Version: 1,
		Bots: []config.PortableBotConfig{
			{
				ID:   "assistant",
				Name: "Assistant",
				Schedules: []config.ScheduleConfig{
					{ID: "shared", Name: "Shared", EveryMinutes: 5, Prompt: "new"},
					{ID: "remote-only", Name: "Remote", DailyAt: "09:00", Prompt: "remote"},
				},
			},
		},
	}
	changed, _, _, err := config.ApplyPortable(localCfg, bundle)
	if err != nil || !changed {
		t.Fatalf("expected changed=true, err=%v", err)
	}
	if len(localCfg.Bots[0].Schedules) != 3 {
		t.Fatalf("expected 3 merged schedules, got %d: %+v", len(localCfg.Bots[0].Schedules), localCfg.Bots[0].Schedules)
	}
}

func TestGitCheckpointPullAutoResolvesRebaseConflict(t *testing.T) {
	ctx := context.Background()
	bareDir := t.TempDir()
	if _, err := runGit(ctx, bareDir, "init", "--bare", "-b", "main"); err != nil {
		t.Skipf("git bare init failed: %v", err)
	}

	dir1 := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir1, "workspaces", "_shared"), 0o755)
	_ = os.WriteFile(filepath.Join(dir1, "workspaces", "_shared", "USER.md"), []byte("v1"), 0o644)
	cfg1 := &config.HubConfig{
		ProjectRoot: dir1,
		Sync:        &config.SyncConfig{Git: &config.SyncGitConfig{Enabled: true, RepoURL: bareDir, Branch: "main"}},
	}
	if _, err := CheckpointPush(ctx, cfg1, "host1"); err != nil {
		t.Fatal(err)
	}

	dir2 := t.TempDir()
	cfg2 := &config.HubConfig{
		ProjectRoot: dir2,
		Sync:        &config.SyncConfig{Git: &config.SyncGitConfig{Enabled: true, RepoURL: bareDir, Branch: "main"}},
	}
	if _, err := CheckpointPull(ctx, cfg2); err != nil {
		t.Fatal(err)
	}

	// Host 1 modifies USER.md and pushes; Host 2 concurrently modifies USER.md and pulls.
	_ = os.WriteFile(filepath.Join(dir1, "workspaces", "_shared", "USER.md"), []byte("from-host1"), 0o644)
	if _, err := CheckpointPush(ctx, cfg1, "host1"); err != nil {
		t.Fatal(err)
	}

	_ = os.WriteFile(filepath.Join(dir2, "workspaces", "_shared", "USER.md"), []byte("from-host2"), 0o644)
	_, _ = runGit(ctx, dir2, "add", "-A")
	_, _ = runGit(ctx, dir2, "commit", "-m", "local host2 edit")

	pullRes, err := CheckpointPull(ctx, cfg2)
	if err != nil {
		t.Fatalf("expected auto-resolved git pull, got error: %v", err)
	}
	if !pullRes.Pulled {
		t.Fatalf("expected Pulled=true")
	}
	if _, err := os.Stat(filepath.Join(dir2, "workspaces", "_shared", "USER.conflict-checkpoint.md")); err != nil {
		entries, _ := os.ReadDir(filepath.Join(dir2, "workspaces", "_shared"))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		mainData, _ := os.ReadFile(filepath.Join(dir2, "workspaces", "_shared", "USER.md"))
		t.Fatalf("expected USER.conflict-checkpoint.md created: %v (dir entries=%v, USER.md=%q)", err, names, string(mainData))
	}
	_ = time.Now()
}
