package desktophost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bot-hub/internal/protocol"
)

func TestAppendPathIfMissing(t *testing.T) {
	got := AppendPathIfMissing("/usr/bin:/bin", "/App/Contents/Resources/bin")
	want := "/usr/bin:/bin:/App/Contents/Resources/bin"
	if got != want {
		t.Fatalf("AppendPathIfMissing = %q, want %q", got, want)
	}
	dup := AppendPathIfMissing("/usr/bin:/App/Contents/Resources/bin:/bin", "/App/Contents/Resources/bin/")
	if dup != "/usr/bin:/App/Contents/Resources/bin:/bin" {
		t.Fatalf("expected deduplicated PATH, got %q", dup)
	}
}

func TestChromiumExecutableFromHandlers(t *testing.T) {
	edge := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	arc := "/Users/u/Applications/Arc.app/Contents/MacOS/Arc"

	plistChrome := []byte(`{"LSHandlers":[{"LSHandlerURLScheme":"https","LSHandlerRoleAll":"com.google.chrome"}]}`)
	got := ChromiumExecutableFromHandlers(plistChrome, "/Users/u", func(p string) bool {
		return p == edge || p == chrome
	})
	if got != chrome {
		t.Fatalf("expected Chrome, got %q", got)
	}

	plistArc := []byte(`{"LSHandlers":[{"LSHandlerContentType":"com.apple.default-app.web-browser","LSHandlerRoleAll":"company.thebrowser.browser"}]}`)
	got = ChromiumExecutableFromHandlers(plistArc, "/Users/u", func(p string) bool {
		return p == filepath.Clean(arc) || p == chrome
	})
	if got != arc {
		t.Fatalf("expected Arc from ~/Applications, got %q", got)
	}

	plistSafari := []byte(`{"LSHandlers":[{"LSHandlerURLScheme":"https","LSHandlerRoleAll":"com.apple.safari"}]}`)
	got = ChromiumExecutableFromHandlers(plistSafari, "/Users/u", func(p string) bool {
		return p == chrome
	})
	if got != chrome {
		t.Fatalf("expected fallback to Chrome when Safari is default, got %q", got)
	}

	got = ChromiumExecutableFromHandlers(nil, "/Users/u", func(string) bool { return false })
	if got != "" {
		t.Fatalf("expected empty when no Chromium installed, got %q", got)
	}
}

func TestParseRemoteTarget(t *testing.T) {
	ts, err := ParseRemoteTarget("tailscale://mac-mini.ts.net#token=sec123")
	if err != nil {
		t.Fatalf("tailscale parse error: %v", err)
	}
	if ts.Kind != "tailscale" || ts.Addr != "mac-mini.ts.net:3789" || ts.BaseURL != "http://mac-mini.ts.net:3789" || ts.Token != "sec123" {
		t.Fatalf("unexpected tailscale target: %+v", ts)
	}

	httpTarget, err := ParseRemoteTarget("https://100.64.0.2:8443#token=abc")
	if err != nil {
		t.Fatalf("https parse error: %v", err)
	}
	if httpTarget.Kind != "http" || httpTarget.BaseURL != "https://100.64.0.2:8443" || httpTarget.Token != "abc" {
		t.Fatalf("unexpected https target: %+v", httpTarget)
	}

	if _, err := ParseRemoteTarget("invalid-link"); err == nil {
		t.Fatal("expected error for invalid remote link")
	}
}

func TestControlServerEndpointsAndAuth(t *testing.T) {
	dir := t.TempDir()
	host := New(dir, Hooks{})
	host.Load()
	cs := newControlServer(host, "test-secret")
	h := cs.handler()

	// Unauthorized without bearer token
	reqUnauth := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9000/control/status", nil)
	recUnauth := httptest.NewRecorder()
	h.ServeHTTP(recUnauth, reqUnauth)
	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d, want 401", recUnauth.Code)
	}

	// Reject non-loopback Host
	reqHost := httptest.NewRequest(http.MethodGet, "http://evil.example.com/control/status", nil)
	reqHost.Header.Set("Authorization", "Bearer test-secret")
	recHost := httptest.NewRecorder()
	h.ServeHTTP(recHost, reqHost)
	if recHost.Code != http.StatusForbidden {
		t.Fatalf("non-loopback host status = %d, want 403", recHost.Code)
	}

	// Authorized status
	reqStatus := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9000/control/status", nil)
	reqStatus.Header.Set("Authorization", "Bearer test-secret")
	recStatus := httptest.NewRecorder()
	h.ServeHTTP(recStatus, reqStatus)
	if recStatus.Code != http.StatusOK {
		t.Fatalf("GET /control/status = %d, want 200", recStatus.Code)
	}
	var snap Snapshot
	if err := json.Unmarshal(recStatus.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snap.DataDir != dir || snap.Mode != "" || snap.DaemonProtocol != protocol.CurrentProtocol {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if len(protocol.DaemonCapabilities()) == 0 {
		t.Fatal("expected daemon capabilities")
	}

	// Update local-node prefs
	reqNode := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9000/control/local-node", strings.NewReader(`{"enabled":true,"name":"MacBook"}`))
	reqNode.Header.Set("Authorization", "Bearer test-secret")
	recNode := httptest.NewRecorder()
	h.ServeHTTP(recNode, reqNode)
	if recNode.Code != http.StatusOK {
		t.Fatalf("POST /control/local-node = %d, want 200", recNode.Code)
	}
	if !host.Prefs().AllowNode || host.Prefs().NodeName != "MacBook" {
		t.Fatalf("local-node prefs not updated: %+v", host.Prefs())
	}

	// Tailscale status when disabled
	reqTS := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9000/control/tailscale", nil)
	reqTS.Header.Set("Authorization", "Bearer test-secret")
	recTS := httptest.NewRecorder()
	h.ServeHTTP(recTS, reqTS)
	if recTS.Code != http.StatusOK || !strings.Contains(recTS.Body.String(), `"state":"disabled"`) {
		t.Fatalf("unexpected tailscale status: %d %s", recTS.Code, recTS.Body.String())
	}

	// Reset
	reqReset := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9000/control/reset", nil)
	reqReset.Header.Set("Authorization", "Bearer test-secret")
	recReset := httptest.NewRecorder()
	h.ServeHTTP(recReset, reqReset)
	if recReset.Code != http.StatusOK {
		t.Fatalf("POST /control/reset = %d, want 200", recReset.Code)
	}
}

func TestSyncRemoteTokenFromResponse(t *testing.T) {
	dir := t.TempDir()
	host := New(dir, Hooks{})
	host.Load()
	host.setRemoteToken("remote-token")

	// 1. Explicit Authorization header must ignore a stale 127.0.0.1 cookie.
	reqBearer := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3790/api/bots", nil)
	reqBearer.Header.Set("Authorization", "Bearer remote-token")
	reqBearer.AddCookie(&http.Cookie{Name: "bot_hub_token", Value: "stale-local-token"})
	host.syncRemoteTokenFromResponse(&http.Response{
		StatusCode: http.StatusOK,
		Request:    reqBearer,
	})
	if got := host.Prefs().RemoteToken; got != "remote-token" {
		t.Fatalf("RemoteToken overwritten with Bearer present: got %q, want %q", got, "remote-token")
	}

	// 2. Cookie-only API call adopts the cookie token.
	reqCookie := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3790/api/bots", nil)
	reqCookie.AddCookie(&http.Cookie{Name: "bot_hub_token", Value: "cookie-token"})
	host.syncRemoteTokenFromResponse(&http.Response{
		StatusCode: http.StatusOK,
		Request:    reqCookie,
	})
	if got := host.Prefs().RemoteToken; got != "cookie-token" {
		t.Fatalf("RemoteToken from cookie = %q, want %q", got, "cookie-token")
	}

	// 3. /api/login Set-Cookie adopts the new login token even if Authorization was absent.
	reqLogin := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3790/api/login", nil)
	host.syncRemoteTokenFromResponse(&http.Response{
		StatusCode: http.StatusOK,
		Request:    reqLogin,
		Header: http.Header{
			"Set-Cookie": []string{(&http.Cookie{Name: "bot_hub_token", Value: "login-token"}).String()},
		},
	})
	if got := host.Prefs().RemoteToken; got != "login-token" {
		t.Fatalf("RemoteToken from /api/login = %q, want %q", got, "login-token")
	}
}

func TestDataDirResolutionAndLegacyFallback(t *testing.T) {
	// 1. Fresh install -> <base>/pebble.
	freshBase := t.TempDir()
	if got, want := resolveDefaultDataDir(freshBase), filepath.Join(freshBase, "pebble"); got != want {
		t.Fatalf("fresh resolveDefaultDataDir = %q, want %q", got, want)
	}

	// 2. Existing legacy <base>/bot-hub without <base>/pebble -> keeps <base>/bot-hub.
	legacyBase := t.TempDir()
	legacyDir := filepath.Join(legacyBase, "bot-hub")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := resolveDefaultDataDir(legacyBase); got != legacyDir {
		t.Fatalf("legacy resolveDefaultDataDir = %q, want %q", got, legacyDir)
	}

	// 3. When both exist, prefers <base>/pebble.
	pebbleDir := filepath.Join(legacyBase, "pebble")
	if err := os.MkdirAll(pebbleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := resolveDefaultDataDir(legacyBase); got != pebbleDir {
		t.Fatalf("both-exist resolveDefaultDataDir = %q, want %q", got, pebbleDir)
	}

	// 4. Env overrides: PEBBLE_DATA_DIR beats BOT_HUB_DATA_DIR, and BOT_HUB_DATA_DIR works as fallback.
	customLegacy := filepath.Join(t.TempDir(), "custom-legacy")
	customPebble := filepath.Join(t.TempDir(), "custom-pebble")
	t.Setenv("PEBBLE_DATA_DIR", "")
	t.Setenv("BOT_HUB_DATA_DIR", customLegacy)
	got, err := DataDir()
	if err != nil || got != customLegacy {
		t.Fatalf("DataDir fallback = (%q, %v), want %q", got, err, customLegacy)
	}
	t.Setenv("PEBBLE_DATA_DIR", customPebble)
	got, err = DataDir()
	if err != nil || got != customPebble {
		t.Fatalf("DataDir primary = (%q, %v), want %q", got, err, customPebble)
	}
}
