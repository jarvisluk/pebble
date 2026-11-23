package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectHarnessAndPreference(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })

	// Isolate HOME and AGY_BIN so host machine binaries don't leak.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("AGY_BIN", "")

	// Case 1: None installed.
	lookPathFunc = func(string) (string, error) {
		return "", errors.New("not found")
	}
	for _, kind := range []string{"opencode", "codebuddy", "qoder", "codex", "claude", "trae", "pi", "dsh", "agy"} {
		if ok, p := DetectHarness(kind); ok || p != "" {
			t.Fatalf("expected %s not installed, got ok=%v path=%q", kind, ok, p)
		}
	}
	if got := PreferredInstalledHarness(); got != "" {
		t.Fatalf("PreferredInstalledHarness = %q, want empty", got)
	}

	// Case 2: agy fallback path in ~/.local/bin/agy.
	localBin := filepath.Join(fakeHome, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	agyBin := filepath.Join(localBin, "agy")
	if err := os.WriteFile(agyBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, p := DetectHarness("agy"); !ok || p != agyBin {
		t.Fatalf("DetectHarness(agy) fallback = (%v, %q), want %q", ok, p, agyBin)
	}
	if got := PreferredInstalledHarness(); got != "agy" {
		t.Fatalf("PreferredInstalledHarness = %q, want agy", got)
	}

	// Case 3: Only claude, pi, and agy installed -> claude preferred.
	lookPathFunc = func(bin string) (string, error) {
		switch bin {
		case "claude":
			return "/usr/local/bin/claude", nil
		case "pi":
			return "/usr/local/bin/pi", nil
		default:
			return "", errors.New("not found")
		}
	}
	if ok, p := DetectHarness("claude"); !ok || p != "/usr/local/bin/claude" {
		t.Fatalf("DetectHarness(claude) = (%v, %q)", ok, p)
	}
	if got := PreferredInstalledHarness(); got != "claude" {
		t.Fatalf("PreferredInstalledHarness = %q, want claude", got)
	}

	// Case 4: qoder fallback binary name "qoder" in ~/.qoder/bin/qoder.
	qoderDir := filepath.Join(fakeHome, ".qoder", "bin")
	if err := os.MkdirAll(qoderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	qoderBin := filepath.Join(qoderDir, "qoder")
	if err := os.WriteFile(qoderBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, p := DetectHarness("qoder"); !ok || p != qoderBin {
		t.Fatalf("DetectHarness(qoder) = (%v, %q), want %q", ok, p, qoderBin)
	}
	if got := PreferredInstalledHarness(); got != "qoder" {
		t.Fatalf("PreferredInstalledHarness = %q, want qoder", got)
	}

	// Case 5: opencode fallback path in ~/.opencode/bin/opencode.
	ocDir := filepath.Join(fakeHome, ".opencode", "bin")
	if err := os.MkdirAll(ocDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ocBin := filepath.Join(ocDir, "opencode")
	if err := os.WriteFile(ocBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, p := DetectHarness("opencode"); !ok || p != ocBin {
		t.Fatalf("DetectHarness(opencode) fallback = (%v, %q), want %q", ok, p, ocBin)
	}
	if got := PreferredInstalledHarness(); got != "opencode" {
		t.Fatalf("PreferredInstalledHarness = %q, want opencode", got)
	}
}

func TestDetectHarnessAuth(t *testing.T) {
	origLookPath := lookPathFunc
	origAuthCheck := runAuthCheckFunc
	t.Cleanup(func() {
		lookPathFunc = origLookPath
		runAuthCheckFunc = origAuthCheck
	})

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEBUDDY_API_KEY", "")
	t.Setenv("QODER_API_KEY", "")

	lookPathFunc = func(bin string) (string, error) {
		return "/usr/local/bin/" + bin, nil
	}
	runAuthCheckFunc = func(context.Context, string, ...string) (int, string, error) {
		return 1, "not logged in", errors.New("exit 1")
	}

	// opencode does not require login.
	if st, _ := DetectHarnessAuth(context.Background(), "opencode"); st != "ready" {
		t.Fatalf("opencode auth = %q, want ready", st)
	}

	// codex needs login initially.
	if st, _ := DetectHarnessAuth(context.Background(), "codex"); st != "needs_login" {
		t.Fatalf("codex auth = %q, want needs_login", st)
	}

	// codex becomes ready when ~/.codex/auth.json exists.
	codexDir := filepath.Join(fakeHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(`{"token":"abc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := DetectHarnessAuth(context.Background(), "codex"); st != "ready" {
		t.Fatalf("codex auth with auth.json = %q, want ready", st)
	}

	// codebuddy becomes ready when ~/.codebuddy/auth.json exists.
	if st, _ := DetectHarnessAuth(context.Background(), "codebuddy"); st != "needs_login" {
		t.Fatalf("codebuddy auth = %q, want needs_login", st)
	}
	cbDir := filepath.Join(fakeHome, ".codebuddy")
	if err := os.MkdirAll(cbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cbDir, "auth.json"), []byte(`{"uid":"1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := DetectHarnessAuth(context.Background(), "codebuddy"); st != "ready" {
		t.Fatalf("codebuddy auth with auth.json = %q, want ready", st)
	}

	// claude becomes ready when auth check exits 0.
	runAuthCheckFunc = func(context.Context, string, ...string) (int, string, error) {
		return 0, `{"loggedIn":true}`, nil
	}
	if st, _ := DetectHarnessAuth(context.Background(), "claude"); st != "ready" {
		t.Fatalf("claude auth = %q, want ready", st)
	}
}
