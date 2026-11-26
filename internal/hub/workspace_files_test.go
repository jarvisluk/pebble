package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bot-hub/internal/config"
)

func newWorkspaceTestHub(t *testing.T) (*Hub, string, string) {
	t.Helper()
	root := t.TempDir()
	wsDir := filepath.Join(root, "workspaces", "helper")
	sharedDir := filepath.Join(root, "workspaces", "_shared")
	outsideDir := filepath.Join(root, "outside")

	for _, d := range []string{
		filepath.Join(wsDir, "memory"),
		filepath.Join(wsDir, ".git"),
		filepath.Join(wsDir, ".hidden_dir"),
		sharedDir,
		outsideDir,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	if err := os.WriteFile(filepath.Join(wsDir, "AGENTS.md"), []byte("# Helper\nHello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "memory", "notes.md"), []byte("## Notes\n- item"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".env"), []byte("SECRET=123"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".gitignore"), []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "USER.md"), []byte("# Shared User"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sharedDir, filepath.Join(wsDir, "_shared")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outsideDir, "secret.txt"), filepath.Join(wsDir, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "bin.dat"), []byte{0x00, 0x01, 0x02, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "big.txt"), []byte(strings.Repeat("a", 128)), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &Hub{
		subs: make(map[uint64]chan Event),
		lanes: map[string]*botLane{
			"helper": {
				bot: config.BotConfig{
					ID:        "helper",
					Name:      "Helper",
					Workspace: wsDir,
				},
			},
		},
	}
	return h, wsDir, outsideDir
}

func TestListWorkspaceFilesAndSandbox(t *testing.T) {
	h, _, _ := newWorkspaceTestHub(t)

	// Default listing hides dotfiles, .git, .env, and escaping symlinks.
	res, err := h.ListWorkspaceFiles("helper", "", false)
	if err != nil {
		t.Fatalf("ListWorkspaceFiles: %v", err)
	}
	names := make(map[string]WorkspaceFileEntry)
	for _, e := range res.Entries {
		names[e.Name] = e
	}
	if _, ok := names["AGENTS.md"]; !ok {
		t.Fatalf("expected AGENTS.md in entries, got %+v", res.Entries)
	}
	if e, ok := names["_shared"]; !ok || !e.IsDir || !e.IsShared {
		t.Fatalf("expected _shared dir marked shared, got %+v", names["_shared"])
	}
	for _, hidden := range []string{".env", ".gitignore", ".git", ".hidden_dir", "escape.txt"} {
		if _, ok := names[hidden]; ok {
			t.Fatalf("expected %q to be filtered out, got %+v", hidden, res.Entries)
		}
	}

	// showHidden reveals .gitignore and .hidden_dir, but still blocks .env and escape.txt.
	resHidden, err := h.ListWorkspaceFiles("helper", "", true)
	if err != nil {
		t.Fatalf("ListWorkspaceFiles(showHidden): %v", err)
	}
	hiddenNames := make(map[string]bool)
	for _, e := range resHidden.Entries {
		hiddenNames[e.Name] = true
	}
	if !hiddenNames[".gitignore"] || !hiddenNames[".hidden_dir"] {
		t.Fatalf("expected hidden non-secret files when showHidden=true, got %+v", resHidden.Entries)
	}
	if hiddenNames[".env"] || hiddenNames["escape.txt"] {
		t.Fatalf("expected .env and escape.txt to remain blocked, got %+v", resHidden.Entries)
	}

	// _shared traversal works.
	sharedRes, err := h.ListWorkspaceFiles("helper", "_shared", false)
	if err != nil {
		t.Fatalf("ListWorkspaceFiles(_shared): %v", err)
	}
	if len(sharedRes.Entries) != 1 || sharedRes.Entries[0].Name != "USER.md" || !sharedRes.Entries[0].IsShared {
		t.Fatalf("unexpected _shared entries: %+v", sharedRes.Entries)
	}

	// Traversal & symlink escape blocked.
	for _, bad := range []string{"../outside", "memory/../../outside", "/etc/passwd", ".env", "escape.txt"} {
		if _, _, _, err := h.ResolveWorkspacePath("helper", bad); err == nil {
			t.Fatalf("expected error for blocked path %q", bad)
		}
	}
}

func TestReadWorkspaceFileAndOpenStage(t *testing.T) {
	h, _, _ := newWorkspaceTestHub(t)

	md, err := h.ReadWorkspaceFile("helper", "memory/notes.md", 0)
	if err != nil {
		t.Fatalf("ReadWorkspaceFile markdown: %v", err)
	}
	if md.Kind != "markdown" || !strings.Contains(md.Content, "Notes") {
		t.Fatalf("unexpected markdown result: %+v", md)
	}

	bin, err := h.ReadWorkspaceFile("helper", "bin.dat", 0)
	if err != nil {
		t.Fatalf("ReadWorkspaceFile binary: %v", err)
	}
	if bin.Kind != "binary" || bin.Content != "" {
		t.Fatalf("unexpected binary result: %+v", bin)
	}

	trunc, err := h.ReadWorkspaceFile("helper", "big.txt", 32)
	if err != nil {
		t.Fatalf("ReadWorkspaceFile truncated: %v", err)
	}
	if trunc.Kind != "text" || !trunc.Truncated || len(trunc.Content) != 32 {
		t.Fatalf("unexpected truncated result: %+v", trunc)
	}

	subID, ch := h.Subscribe()
	defer h.Unsubscribe(subID)

	st, err := h.OpenBotStage("helper", StageOpenPayload{
		Kind: "files",
		Path: "_shared/USER.md",
	})
	if err != nil {
		t.Fatalf("OpenBotStage: %v", err)
	}
	if st.Path != "_shared/USER.md" || st.Title != "USER.md" {
		t.Fatalf("unexpected stage payload: %+v", st)
	}

	evt := <-ch
	if evt.Type != "stage_open" || evt.BotID != "helper" {
		t.Fatalf("unexpected broadcast event: %+v", evt)
	}

	// OpenBotStage supports kind=file, kind=desktop, kind=browser, autoOpen, and silent flags.
	fileSt, err := h.OpenBotStage("helper", StageOpenPayload{
		Kind:     "file",
		Path:     "memory/notes.md",
		AutoOpen: true,
	})
	if err != nil {
		t.Fatalf("OpenBotStage file: %v", err)
	}
	if fileSt.ID != "file:memory/notes.md" || fileSt.Title != "notes.md" || fileSt.Subtitle != "memory/notes.md" || !fileSt.AutoOpen {
		t.Fatalf("unexpected file stage payload: %+v", fileSt)
	}

	deskSt, err := h.OpenBotStage("helper", StageOpenPayload{
		Kind:   "desktop",
		Silent: true,
	})
	if err != nil {
		t.Fatalf("OpenBotStage desktop: %v", err)
	}
	if deskSt.ID != "desktop" || deskSt.Target != "local" {
		t.Fatalf("unexpected desktop stage payload: %+v", deskSt)
	}

	browserSt, err := h.OpenBotStage("helper", StageOpenPayload{
		Kind:  "browser",
		Title: "Docs",
		URL:   "https://example.com",
	})
	if err != nil {
		t.Fatalf("OpenBotStage browser: %v", err)
	}
	if browserSt.Subtitle != "https://example.com" {
		t.Fatalf("unexpected browser stage payload: %+v", browserSt)
	}

	latest := h.getBotLatestStage("helper")
	if latest == nil || latest.Kind != "browser" || latest.URL != "https://example.com" {
		t.Fatalf("unexpected latest stage cached on hub: %+v", latest)
	}

	statuses, err := h.Status("helper")
	if err != nil || len(statuses) != 1 || statuses[0].LatestStage == nil || statuses[0].LatestStage.Kind != "browser" {
		t.Fatalf("expected BotStatus.LatestStage to be populated, got %+v (err=%v)", statuses, err)
	}
}
