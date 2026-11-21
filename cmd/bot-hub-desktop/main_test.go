package main

import (
	"path/filepath"
	"testing"
)

func TestAppendPathIfMissing(t *testing.T) {
	got := appendPathIfMissing("/usr/bin:/bin", "/App/Contents/Resources/bin")
	want := "/usr/bin:/bin:/App/Contents/Resources/bin"
	if got != want {
		t.Fatalf("appendPathIfMissing = %q, want %q", got, want)
	}
	// Duplicate with trailing slash should not append again.
	dup := appendPathIfMissing("/usr/bin:/App/Contents/Resources/bin:/bin", "/App/Contents/Resources/bin/")
	if dup != "/usr/bin:/App/Contents/Resources/bin:/bin" {
		t.Fatalf("expected deduplicated PATH, got %q", dup)
	}
}

func TestChromiumExecutableFromHandlers(t *testing.T) {
	edge := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	arc := "/Users/u/Applications/Arc.app/Contents/MacOS/Arc"

	// 1. Default https browser is Chrome -> selects Chrome even if Edge is installed.
	plistChrome := []byte(`{"LSHandlers":[{"LSHandlerURLScheme":"https","LSHandlerRoleAll":"com.google.chrome"}]}`)
	got := chromiumExecutableFromHandlers(plistChrome, "/Users/u", func(p string) bool {
		return p == edge || p == chrome
	})
	if got != chrome {
		t.Fatalf("expected Chrome, got %q", got)
	}

	// 2. Default browser in ~/Applications (Arc) is found.
	plistArc := []byte(`{"LSHandlers":[{"LSHandlerContentType":"com.apple.default-app.web-browser","LSHandlerRoleAll":"company.thebrowser.browser"}]}`)
	got = chromiumExecutableFromHandlers(plistArc, "/Users/u", func(p string) bool {
		return p == filepath.Clean(arc) || p == chrome
	})
	if got != arc {
		t.Fatalf("expected Arc from ~/Applications, got %q", got)
	}

	// 3. Default is Safari -> falls back to installed Edge/Chrome.
	plistSafari := []byte(`{"LSHandlers":[{"LSHandlerURLScheme":"https","LSHandlerRoleAll":"com.apple.safari"}]}`)
	got = chromiumExecutableFromHandlers(plistSafari, "/Users/u", func(p string) bool {
		return p == chrome
	})
	if got != chrome {
		t.Fatalf("expected fallback to Chrome when Safari is default, got %q", got)
	}

	// 4. Missing plist + no Chromium installed -> empty string.
	got = chromiumExecutableFromHandlers(nil, "/Users/u", func(string) bool { return false })
	if got != "" {
		t.Fatalf("expected empty when no Chromium installed, got %q", got)
	}
}
