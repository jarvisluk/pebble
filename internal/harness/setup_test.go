package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallWhitelistValidation(t *testing.T) {
	if _, err := StartInstall("unknown-agent"); err == nil {
		t.Fatal("expected error for unsupported install target")
	}
	if _, err := StartLogin("opencode"); err == nil {
		t.Fatal("expected error for login on opencode")
	}
}

func TestStartLoginExtractsURLAndVerifiesAuth(t *testing.T) {
	origLookPath := lookPathFunc
	origOpen := openBrowserFunc
	origAuthCheck := runAuthCheckFunc
	t.Cleanup(func() {
		lookPathFunc = origLookPath
		openBrowserFunc = origOpen
		runAuthCheckFunc = origAuthCheck
		setupMu.Lock()
		delete(setupTasks, taskKey("codex", "login"))
		setupMu.Unlock()
	})

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("OPENAI_API_KEY", "")

	binDir := filepath.Join(fakeHome, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mockCodex := filepath.Join(binDir, "codex")
	script := `#!/bin/sh
echo "Open https://auth.openai.com/device?code=XYZ to continue"
sleep 5
`
	if err := os.WriteFile(mockCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	lookPathFunc = func(bin string) (string, error) {
		if bin == "codex" {
			return mockCodex, nil
		}
		return "", os.ErrNotExist
	}
	runAuthCheckFunc = func(context.Context, string, ...string) (int, string, error) {
		return 1, "not logged in", os.ErrNotExist
	}

	var openedURL string
	openBrowserFunc = func(u string) error {
		openedURL = u
		return nil
	}

	task, err := StartLogin("codex")
	if err != nil {
		t.Fatalf("StartLogin error: %v", err)
	}
	if task.State != "running" {
		t.Fatalf("initial state = %q, want running", task.State)
	}

	// Wait for URL extraction.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := GetLoginStatus(context.Background(), "codex")
		if st.LoginURL != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := GetLoginStatus(context.Background(), "codex")
	if st.LoginURL != "https://auth.openai.com/device?code=XYZ" {
		t.Fatalf("LoginURL = %q", st.LoginURL)
	}
	if openedURL != st.LoginURL {
		t.Fatalf("openedURL = %q, want %q", openedURL, st.LoginURL)
	}

	// Simulate auth completion by writing ~/.codex/auth.json.
	codexDir := filepath.Join(fakeHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(`{"token":"ok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	st = GetLoginStatus(context.Background(), "codex")
	if st.State != "done" || st.AuthStatus != "ready" {
		t.Fatalf("expected done+ready after auth.json, got %+v", st)
	}
}
