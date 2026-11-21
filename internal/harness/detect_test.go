package harness

import (
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
	for _, kind := range []string{"opencode", "claude", "pi", "dsh", "agy"} {
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

	// Case 3: opencode fallback path in ~/.opencode/bin/opencode.
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
