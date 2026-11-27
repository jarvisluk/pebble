package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bot-hub/internal/protocol"
	"bot-hub/internal/update"
)

func TestCLIUpdateCommands(t *testing.T) {
	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/jarvisluk/pebble/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v9.9.0",
				"name":     "Pebble v9.9.0",
				"body":     "- Release notes line",
				"html_url": "https://github.com/jarvisluk/pebble/releases/tag/v9.9.0",
			})
		case "/repos/jarvisluk/pebble/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"tag_name":   "v9.9.1-rc.1",
					"prerelease": true,
					"html_url":   "https://github.com/jarvisluk/pebble/releases/tag/v9.9.1-rc.1",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ghSrv.Close()

	stateDir := t.TempDir()
	t.Setenv("PEBBLE_UPDATE_STATE_DIR", stateDir)
	t.Setenv("PEBBLE_UPDATE_API_URL", ghSrv.URL)

	oldVer := protocol.AppVersion
	protocol.AppVersion = "0.1.0"
	defer func() { protocol.AppVersion = oldVer }()

	run := func(args ...string) (string, string, int) {
		var out, errBuf bytes.Buffer
		code := runCommand(context.Background(), args, t.TempDir(), &out, &errBuf)
		return out.String(), errBuf.String(), code
	}

	// Default check -> finds v9.9.0.
	if out, errOut, code := run("update"); code != 0 || !strings.Contains(out, "v9.9.0") {
		t.Fatalf("pebble update failed: code=%d out=%s err=%s", code, out, errOut)
	}

	// JSON check with --channel prerelease.
	out, errOut, code := run("update", "check", "--channel", "prerelease", "--json")
	if code != 0 {
		t.Fatalf("pebble update check --json failed: code=%d err=%s", code, errOut)
	}
	var st update.Status
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("decode json output: %v\n%s", err, out)
	}
	if !st.HasUpdate || st.Latest == nil || st.Latest.Version != "9.9.1-rc.1" {
		t.Fatalf("unexpected status: %+v", st)
	}

	// Configure auto, channel, and proxy.
	if out, errOut, code := run("update", "auto", "off", "24h"); code != 0 || !strings.Contains(out, "autoCheck: off (interval: 24h)") {
		t.Fatalf("update auto failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if out, errOut, code := run("update", "channel", "prerelease"); code != 0 || !strings.Contains(out, "channel: prerelease") {
		t.Fatalf("update channel failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if out, errOut, code := run("update", "proxy", "http://127.0.0.1:7890"); code != 0 || !strings.Contains(out, "http://127.0.0.1:7890") {
		t.Fatalf("update proxy failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if out, errOut, code := run("update", "proxy", "off"); code != 0 || !strings.Contains(out, "proxy: off") {
		t.Fatalf("update proxy off failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if _, _, code := run("update", "auto", "bad"); code == 0 {
		t.Fatal("expected non-zero exit code for invalid auto argument")
	}

	// Up-to-date case.
	protocol.AppVersion = "9.9.0"
	if out, errOut, code := run("update", "check", "--channel", "stable"); code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("expected up to date message: code=%d out=%s err=%s", code, out, errOut)
	}
}
