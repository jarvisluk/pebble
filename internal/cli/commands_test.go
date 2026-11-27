package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIBotCommandsAndPartialUpdate(t *testing.T) {
	var gotPatch map[string]any
	var gotCreate map[string]any
	var gotMessage map[string]any
	var gotDeletePath string

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-ws" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots":
			_, _ = w.Write([]byte(`{"bots":[{"id":"assistant","name":"Assistant"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots/assistant":
			_, _ = w.Write([]byte(`{"id":"assistant","name":"Assistant","instructions":"# Assistant"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots":
			_ = json.NewDecoder(r.Body).Decode(&gotCreate)
			_, _ = w.Write([]byte(`{"id":"writer","name":"Writer"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/bots/assistant":
			_ = json.NewDecoder(r.Body).Decode(&gotPatch)
			_, _ = w.Write([]byte(`{"id":"assistant","name":"Jarvis"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/bots/writer":
			gotDeletePath = r.URL.Path
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots/writer/message":
			_ = json.NewDecoder(r.Body).Decode(&gotMessage)
			_, _ = w.Write([]byte(`{"botId":"writer","text":"ack"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	// Simulate an opencode workspace with no per-turn env vars set (primary pebble MCP config).
	ws := t.TempDir()
	ocDoc := map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"pebble": map[string]any{
					"type":        "local",
					"command":     []string{"/usr/local/bin/pebble", "mcp", "--hub-url", api.URL, "--bot-id", "assistant"},
					"environment": map[string]string{"PEBBLE_API_TOKEN": "tok-ws"},
				},
			},
		},
	}
	ocBytes, _ := json.Marshal(ocDoc)
	if err := os.WriteFile(filepath.Join(ws, "opencode.json"), ocBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("# Jarvis\nBe concise.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, string, int) {
		var out, errBuf bytes.Buffer
		code := runCommand(context.Background(), args, ws, &out, &errBuf)
		return out.String(), errBuf.String(), code
	}

	if out, errOut, code := run("bot", "list"); code != 0 || !strings.Contains(out, "Assistant") {
		t.Fatalf("bot list failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if out, errOut, code := run("bot", "get"); code != 0 || !strings.Contains(out, "# Assistant") {
		t.Fatalf("bot get failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if _, errOut, code := run("bot", "create", "--id", "writer", "--name", "Writer", "--harness", "agy", "--desktop-target", "virtual:writer", "--browser-mode", "isolated"); code != 0 {
		t.Fatalf("bot create failed: code=%d err=%s", code, errOut)
	}
	if gotCreate["harness"] != "agy" || gotCreate["id"] != "writer" || gotCreate["desktopTarget"] != "virtual:writer" || gotCreate["browserMode"] != "isolated" {
		t.Fatalf("unexpected create payload: %#v", gotCreate)
	}

	// Update only name and instructions-file; description/model/harness must not be sent.
	if _, errOut, code := run("bot", "update", "--name", "Jarvis", "--instructions-file", "AGENTS.md", "--desktop-target", "macbook", "--browser-mode", "attach"); code != 0 {
		t.Fatalf("bot update failed: code=%d err=%s", code, errOut)
	}
	if gotPatch["name"] != "Jarvis" || gotPatch["instructions"] != "# Jarvis\nBe concise.\n" || gotPatch["desktopTarget"] != "macbook" || gotPatch["browserMode"] != "attach" {
		t.Fatalf("unexpected patch payload: %#v", gotPatch)
	}
	if _, hasModel := gotPatch["model"]; hasModel {
		t.Fatalf("did not expect model in partial patch: %#v", gotPatch)
	}
	if _, hasDesc := gotPatch["description"]; hasDesc {
		t.Fatalf("did not expect description in partial patch: %#v", gotPatch)
	}

	t.Setenv("PEBBLE_HOP", "2")
	if out, errOut, code := run("bot", "message", "writer", "hello", "world"); code != 0 || !strings.Contains(out, "ack") {
		t.Fatalf("bot message failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if gotMessage["fromBotId"] != "assistant" || gotMessage["message"] != "hello world" || gotMessage["hop"] != float64(2) {
		t.Fatalf("unexpected message payload: %#v", gotMessage)
	}

	if _, errOut, code := run("bot", "delete", "--id", "writer"); code != 0 || gotDeletePath != "/api/bots/writer" {
		t.Fatalf("bot delete failed: code=%d path=%s err=%s", code, gotDeletePath, errOut)
	}
}

func TestCLISessionRoutineNodeDesktopAndAgyDiscovery(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-agy" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions":
			_, _ = w.Write([]byte(`{"sessions":[{"sessionId":"ses_1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/previous":
			_, _ = w.Write([]byte(`{"session":{"sessionId":"ses_prev"},"messages":[{"text":"old"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots/coder/rotate":
			_, _ = w.Write([]byte(`{"deferred":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots/coder/routines":
			_, _ = w.Write([]byte(`{"routines":[{"id":"r1","name":"Check"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots/coder/routines" && r.URL.Query().Get("upsert") == "1":
			_, _ = w.Write([]byte(`{"id":"r1","name":"Check","enabled":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots/coder/routines/r1/run":
			_, _ = w.Write([]byte(`{"status":"ok","output":"ran"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/bots/coder/routines/r1":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/nodes":
			_, _ = w.Write([]byte(`{"nodes":[{"id":"mac"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/nodes/_default/call":
			_, _ = w.Write([]byte(`{"nodeId":"mac","ok":true,"exitCode":0,"output":"uname-ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/desktop/task":
			_, _ = w.Write([]byte(`{"ok":true,"text":"gui-done"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/desktop/takeover/request":
			_, _ = w.Write([]byte(`{"ok":true,"note":"resumed"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	agyDoc := map[string]any{
		"mcpServers": map[string]any{
			"pebble": map[string]any{
				"command": "/usr/local/bin/pebble",
				"args":    []string{"mcp", "--hub-url=" + api.URL, "--bot-id=coder"},
				"env":     map[string]string{"PEBBLE_API_TOKEN": "tok-agy"},
			},
		},
	}
	b, _ := json.Marshal(agyDoc)
	if err := os.WriteFile(filepath.Join(ws, ".agents", "mcp_config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"session", "list"}, "ses_1"},
		{[]string{"session", "recall", "--tail", "5"}, "old"},
		{[]string{"session", "rotate", "--handoff", "bye"}, `"deferred": true`},
		{[]string{"routine", "list"}, "Check"},
		{[]string{"routine", "upsert", "--name", "Check", "--daily-at", "09:00", "--prompt", "hi"}, `"enabled": true`},
		{[]string{"routine", "run", "--id", "r1"}, "ran"},
		{[]string{"routine", "delete", "--id", "r1"}, `"ok": true`},
		{[]string{"node", "list"}, "mac"},
		{[]string{"node", "exec", "--command", "uname"}, "uname-ok"},
		{[]string{"node", "file", "--op", "read", "--path", "~/a.txt"}, "uname-ok"},
		{[]string{"desktop", "task", "--goal", "open Safari"}, "gui-done"},
		{[]string{"desktop", "takeover", "--reason", "scan QR"}, "resumed"},
	}

	for _, tc := range cases {
		var out, errBuf bytes.Buffer
		code := runCommand(context.Background(), tc.args, ws, &out, &errBuf)
		if code != 0 || !strings.Contains(out.String(), tc.want) {
			t.Fatalf("command %v failed: code=%d want=%q out=%s err=%s", tc.args, code, tc.want, out.String(), errBuf.String())
		}
	}

	// computer_use role blocks nested desktop task (both PEBBLE_MCP_ROLE and legacy BOT_HUB_MCP_ROLE).
	t.Setenv("PEBBLE_MCP_ROLE", "computer_use")
	var out, errBuf bytes.Buffer
	if code := runCommand(context.Background(), []string{"desktop", "task", "--goal", "nested"}, ws, &out, &errBuf); code == 0 {
		t.Fatalf("expected computer_use role to reject nested desktop task")
	}

	// Backward compatibility: legacy BOT_HUB_URL / BOT_HUB_BOT_ID / BOT_HUB_API_TOKEN and servers["bot-hub"] fallback still work.
	legacyWS := t.TempDir()
	legacyOC := `{"mcp":{"servers":{"bot-hub":{"type":"local","command":["/usr/local/bin/bot-hub","mcp","--hub-url","` + api.URL + `","--bot-id","coder"],"environment":{"BOT_HUB_API_TOKEN":"tok-agy"}}}}}`
	if err := os.WriteFile(filepath.Join(legacyWS, "opencode.json"), []byte(legacyOC), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PEBBLE_MCP_ROLE", "")
	var legacyOut, legacyErr bytes.Buffer
	if code := runCommand(context.Background(), []string{"routine", "list"}, legacyWS, &legacyOut, &legacyErr); code != 0 || !strings.Contains(legacyOut.String(), "Check") {
		t.Fatalf("expected legacy bot-hub opencode.json fallback to work, got code=%d out=%s err=%s", code, legacyOut.String(), legacyErr.String())
	}
}

func TestCLISettingsAndGuardrails(t *testing.T) {
	var (
		gotSettingsGetQuery string
		gotModelsQuery      string
		gotSettingsPatch    map[string]any
		gotSettingsQuery    string
		gotBotPatch         map[string]any
		gotBotPatchQuery    string
		gotBotCreateQuery   string
		gotDeletedBotPath   string
	)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-settings" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots/coder":
			_, _ = w.Write([]byte(`{"id":"coder","harness":"claude"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots":
			gotBotCreateQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"id":"newbot","name":"NewBot"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/bots/coder":
			gotBotPatchQuery = r.URL.RawQuery
			gotBotPatch = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&gotBotPatch)
			_, _ = w.Write([]byte(`{"id":"coder","model":"m1"}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/bots/"):
			gotDeletedBotPath = r.URL.Path
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/settings":
			gotSettingsGetQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"defaultHarness":"opencode","defaultModel":"m1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/models":
			gotModelsQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"models":["sonnet","opus"]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/settings":
			gotSettingsQuery = r.URL.RawQuery
			gotSettingsPatch = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&gotSettingsPatch)
			_, _ = w.Write([]byte(`{"dryRun":true,"valid":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/desktop/targets":
			_, _ = w.Write([]byte(`{"targets":[{"id":"local","name":"本机桌面"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	mcpDoc := map[string]any{
		"mcpServers": map[string]any{
			"pebble": map[string]any{
				"command": "/usr/local/bin/pebble",
				"args":    []string{"mcp", "--hub-url=" + api.URL, "--bot-id=coder"},
				"env":     map[string]string{"PEBBLE_API_TOKEN": "tok-settings"},
			},
		},
	}
	b, _ := json.Marshal(mcpDoc)
	if err := os.WriteFile(filepath.Join(ws, ".agents", "mcp_config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	userMDPath := filepath.Join(ws, "user-prefs.md")
	if err := os.WriteFile(userMDPath, []byte("# Prefs\nConcise.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, string, int) {
		var out, errBuf bytes.Buffer
		code := runCommand(context.Background(), args, ws, &out, &errBuf)
		return out.String(), errBuf.String(), code
	}

	// settings get & --fresh
	if out, errOut, code := run("settings", "get"); code != 0 || !strings.Contains(out, `"defaultHarness": "opencode"`) || gotSettingsGetQuery != "" {
		t.Fatalf("settings get failed: code=%d query=%q out=%s err=%s", code, gotSettingsGetQuery, out, errOut)
	}
	if _, errOut, code := run("settings", "get", "--fresh"); code != 0 || gotSettingsGetQuery != "fresh=1" {
		t.Fatalf("settings get --fresh failed: code=%d query=%q err=%s", code, gotSettingsGetQuery, errOut)
	}

	// settings models auto-detects current bot harness ("claude"), plus explicit --harness & --source.
	if out, errOut, code := run("settings", "models"); code != 0 || !strings.Contains(out, `"harness": "claude"`) || !strings.Contains(out, `"sonnet"`) || gotModelsQuery != "harness=claude" {
		t.Fatalf("settings models auto failed: code=%d query=%q out=%s err=%s", code, gotModelsQuery, out, errOut)
	}
	if out, errOut, code := run("settings", "models", "--harness", "opencode", "--source", "magpie"); code != 0 || !strings.Contains(out, `"source": "magpie"`) || gotModelsQuery != "harness=opencode&source=magpie" {
		t.Fatalf("settings models explicit failed: code=%d query=%q out=%s err=%s", code, gotModelsQuery, out, errOut)
	}

	// settings set flags, defaultModelsPatch, --shared-user-md-file, --dry-run, --force.
	if _, errOut, code := run(
		"settings", "set",
		"--default-harness", "opencode",
		"--default-model", "m1",
		"--harness-model", "opencode=m2",
		"--clear-harness-model", "claude",
		"--desktop-harness", "opencode",
		"--desktop-model", "m-fast",
		"--magpie-web-url", "https://magpie.local",
		"--shared-user-md-file", "user-prefs.md",
		"--dry-run",
	); code != 0 {
		t.Fatalf("settings set failed: code=%d err=%s", code, errOut)
	}
	if gotSettingsQuery != "dryRun=1&validate=1" {
		t.Fatalf("unexpected settings query: %q", gotSettingsQuery)
	}
	if gotSettingsPatch["defaultHarness"] != "opencode" || gotSettingsPatch["defaultModel"] != "m1" ||
		gotSettingsPatch["desktopHarness"] != "opencode" || gotSettingsPatch["desktopModel"] != "m-fast" ||
		gotSettingsPatch["magpieWebUrl"] != "https://magpie.local" || gotSettingsPatch["sharedUserMd"] != "# Prefs\nConcise.\n" {
		t.Fatalf("unexpected settings patch payload: %#v", gotSettingsPatch)
	}
	patchMap, _ := gotSettingsPatch["defaultModelsPatch"].(map[string]any)
	if patchMap["opencode"] != "m2" || patchMap["claude"] != "" {
		t.Fatalf("unexpected defaultModelsPatch: %#v", gotSettingsPatch["defaultModelsPatch"])
	}

	// settings set clear flags & --force.
	if _, errOut, code := run(
		"settings", "set",
		"--clear-default-model",
		"--clear-desktop-harness",
		"--clear-desktop-model",
		"--clear-magpie-web-url",
		"--force",
	); code != 0 {
		t.Fatalf("settings set clear failed: code=%d err=%s", code, errOut)
	}
	if gotSettingsQuery != "" || gotSettingsPatch["defaultModel"] != "" || gotSettingsPatch["desktopHarness"] != "" || gotSettingsPatch["desktopModel"] != "" || gotSettingsPatch["magpieWebUrl"] != "" {
		t.Fatalf("unexpected clear settings payload/query: query=%q payload=%#v", gotSettingsQuery, gotSettingsPatch)
	}

	// settings set rejects empty values and conflicting flags.
	badSettingsCases := [][]string{
		{"settings", "set"},
		{"settings", "set", "--default-harness", ""},
		{"settings", "set", "--default-model", ""},
		{"settings", "set", "--default-model", "m1", "--clear-default-model"},
		{"settings", "set", "--harness-model", "opencode="},
		{"settings", "set", "--harness-model", "opencode=m1", "--clear-harness-model", "opencode"},
		{"settings", "set", "--desktop-model", ""},
		{"settings", "set", "--desktop-model", "m1", "--clear-desktop-model"},
		{"settings", "set", "--magpie-web-url", ""},
	}
	for _, tc := range badSettingsCases {
		if _, _, code := run(tc...); code == 0 {
			t.Fatalf("expected %v to fail", tc)
		}
	}

	// bot create validate vs --force.
	if _, errOut, code := run("bot", "create", "--id", "b1", "--name", "B1"); code != 0 || gotBotCreateQuery != "validate=1" {
		t.Fatalf("bot create validate failed: code=%d query=%q err=%s", code, gotBotCreateQuery, errOut)
	}
	if _, errOut, code := run("bot", "create", "--id", "b1", "--name", "B1", "--force"); code != 0 || gotBotCreateQuery != "" {
		t.Fatalf("bot create --force failed: code=%d query=%q err=%s", code, gotBotCreateQuery, errOut)
	}

	// bot update --inherit-model, --dry-run, --force, and conflict guard.
	if _, errOut, code := run("bot", "update", "--inherit-model"); code != 0 || gotBotPatch["model"] != "" || gotBotPatchQuery != "validate=1" {
		t.Fatalf("bot update --inherit-model failed: code=%d query=%q patch=%#v err=%s", code, gotBotPatchQuery, gotBotPatch, errOut)
	}
	if _, errOut, code := run("bot", "update", "--model", "m2", "--dry-run", "--force"); code != 0 || gotBotPatchQuery != "dryRun=1" {
		t.Fatalf("bot update --dry-run --force failed: code=%d query=%q err=%s", code, gotBotPatchQuery, errOut)
	}
	if _, _, code := run("bot", "update", "--inherit-model", "--model", "m1"); code == 0 {
		t.Fatalf("expected --inherit-model with non-empty --model to fail")
	}

	// bot delete self-protection guard.
	if _, errOut, code := run("bot", "delete", "--id", "coder"); code == 0 || !strings.Contains(errOut, "--confirm-self") {
		t.Fatalf("expected self-delete refusal with --confirm-self hint, got code=%d err=%s", code, errOut)
	}
	if _, errOut, code := run("bot", "delete", "--id", "coder", "--confirm-self"); code != 0 || gotDeletedBotPath != "/api/bots/coder" {
		t.Fatalf("expected self-delete with --confirm-self to succeed, got code=%d path=%q err=%s", code, gotDeletedBotPath, errOut)
	}

	// desktop targets.
	if out, errOut, code := run("desktop", "targets"); code != 0 || !strings.Contains(out, `"id": "local"`) {
		t.Fatalf("desktop targets failed: code=%d out=%s err=%s", code, out, errOut)
	}

	// computer_use role blocks mutating bot/settings commands.
	t.Setenv("PEBBLE_MCP_ROLE", "computer_use")
	for _, tc := range [][]string{
		{"bot", "create", "--id", "x", "--name", "X"},
		{"bot", "update", "--name", "X"},
		{"bot", "delete", "--id", "other"},
		{"settings", "set", "--default-model", "m1"},
	} {
		if _, _, code := run(tc...); code == 0 {
			t.Fatalf("expected computer_use role to block %v", tc)
		}
	}
}

func TestCLIMemoryCommands(t *testing.T) {
	var (
		gotSearchQuery string
		gotGetPath     string
		gotGetQuery    string
		gotSaveBody    map[string]any
		gotDeletePath  string
		gotDeleteQuery string
		gotStatusQuery string
		gotDreamBody   map[string]any
		gotImportBody  map[string]any
	)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-mem" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/memory":
			gotSearchQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"backend":"pebble","installed":true,"available":true,"items":[{"id":"mem_1","title":"Note"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/memory/status":
			gotStatusQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"backend":"pebble","docCount":3,"chunkCount":5}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/memory/"):
			gotGetPath = r.URL.Path
			gotGetQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"path":"coder/memory/items/note-1.md","content":"hello"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/memory":
			gotSaveBody = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&gotSaveBody)
			_, _ = w.Write([]byte(`{"ok":true,"id":"mem_2","item":{"id":"mem_2"}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/memory/"):
			gotDeletePath = r.URL.Path
			gotDeleteQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"ok":true,"id":"mem_1","action":"archived"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/memory/dream":
			gotDreamBody = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&gotDreamBody)
			_, _ = w.Write([]byte(`{"ok":true,"results":[{"botId":"coder","summary":"clean"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/memory/reindex":
			_, _ = w.Write([]byte(`{"ok":true,"docsIndexed":4,"chunksIndexed":9}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/memory/import/magpie":
			gotImportBody = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&gotImportBody)
			_, _ = w.Write([]byte(`{"ok":true,"imported":2,"skipped":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	ws := t.TempDir()
	ocDoc := map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"pebble": map[string]any{
					"type":        "local",
					"command":     []string{"/usr/local/bin/pebble", "mcp", "--hub-url", api.URL, "--bot-id", "coder"},
					"environment": map[string]string{"PEBBLE_API_TOKEN": "tok-mem"},
				},
			},
		},
	}
	ocBytes, _ := json.Marshal(ocDoc)
	if err := os.WriteFile(filepath.Join(ws, "opencode.json"), ocBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, string, int) {
		var out, errBuf bytes.Buffer
		code := runCommand(context.Background(), args, ws, &out, &errBuf)
		return out.String(), errBuf.String(), code
	}

	if out, errOut, code := run("memory", "search", "--query", "deploy", "--tags", "ops,ci", "--include-sessions", "--limit", "5"); code != 0 || !strings.Contains(out, `"mem_1"`) {
		t.Fatalf("memory search failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if !strings.Contains(gotSearchQuery, "callerBotId=coder") || !strings.Contains(gotSearchQuery, "includeSessions=1") {
		t.Fatalf("unexpected search query: %q", gotSearchQuery)
	}
	if out, errOut, code := run("memory", "list", "--scope", "bot"); code != 0 || !strings.Contains(out, `"backend": "pebble"`) {
		t.Fatalf("memory list failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if !strings.Contains(gotSearchQuery, "botId=coder") {
		t.Fatalf("expected auto-resolved botId=coder in list query, got %q", gotSearchQuery)
	}
	if out, errOut, code := run("memory", "get", "--id", "mem_1", "--start-line", "1", "--end-line", "10"); code != 0 || !strings.Contains(out, `"hello"`) {
		t.Fatalf("memory get failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if gotGetPath != "/api/memory/mem_1" || !strings.Contains(gotGetQuery, "startLine=1") {
		t.Fatalf("unexpected get request: %q %q", gotGetPath, gotGetQuery)
	}
	if out, errOut, code := run("memory", "save", "--title", "Rule", "--text", "Keep PRs small", "--kind", "rule", "--tags", "git", "--pin", "--supersedes", "mem_0"); code != 0 || !strings.Contains(out, `"mem_2"`) {
		t.Fatalf("memory save failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if gotSaveBody["botId"] != "coder" || gotSaveBody["scope"] != "bot" || gotSaveBody["supersedes"] != "mem_0" || gotSaveBody["pin"] != true {
		t.Fatalf("unexpected save body: %#v", gotSaveBody)
	}
	if out, errOut, code := run("memory", "forget", "--id", "mem_1", "--dismiss", "--reason", "outdated"); code != 0 || !strings.Contains(out, `"archived"`) {
		t.Fatalf("memory forget failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if gotDeletePath != "/api/memory/mem_1" || !strings.Contains(gotDeleteQuery, "dismiss=1") {
		t.Fatalf("unexpected forget request: %q %q", gotDeletePath, gotDeleteQuery)
	}
	if out, errOut, code := run("memory", "status", "--bot", "coder"); code != 0 || !strings.Contains(out, `"docCount": 3`) {
		t.Fatalf("memory status failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if !strings.Contains(gotStatusQuery, "callerBotId=coder") || !strings.Contains(gotStatusQuery, "botId=coder") {
		t.Fatalf("unexpected memory status query: %q", gotStatusQuery)
	}
	if out, errOut, code := run("memory", "dream", "--bot", "coder"); code != 0 || !strings.Contains(out, `"clean"`) {
		t.Fatalf("memory dream failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if gotDreamBody["botId"] != "coder" || gotDreamBody["force"] != true {
		t.Fatalf("unexpected dream body: %#v", gotDreamBody)
	}
	if out, errOut, code := run("memory", "reindex"); code != 0 || !strings.Contains(out, `"docsIndexed": 4`) {
		t.Fatalf("memory reindex failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if out, errOut, code := run("memory", "import-magpie", "--mode", "all", "--dry-run"); code != 0 || !strings.Contains(out, `"imported": 2`) {
		t.Fatalf("memory import-magpie failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if gotImportBody["mode"] != "all" || gotImportBody["dryRun"] != true {
		t.Fatalf("unexpected import body: %#v", gotImportBody)
	}

	// computer_use role blocks mutating memory commands.
	t.Setenv("PEBBLE_MCP_ROLE", "computer_use")
	for _, tc := range [][]string{
		{"memory", "save", "--text", "x"},
		{"memory", "forget", "--id", "mem_1"},
		{"memory", "dream"},
		{"memory", "reindex"},
		{"memory", "import-magpie"},
	} {
		if _, _, code := run(tc...); code == 0 {
			t.Fatalf("expected computer_use role to block %v", tc)
		}
	}
}
