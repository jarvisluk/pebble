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

	// Simulate an opencode workspace with no per-turn env vars set.
	ws := t.TempDir()
	ocDoc := map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"bot-hub": map[string]any{
					"type":        "local",
					"command":     []string{"/usr/local/bin/bot-hub", "mcp", "--hub-url", api.URL, "--bot-id", "assistant"},
					"environment": map[string]string{"BOT_HUB_API_TOKEN": "tok-ws"},
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
	if _, errOut, code := run("bot", "create", "--id", "writer", "--name", "Writer", "--harness", "agy"); code != 0 {
		t.Fatalf("bot create failed: code=%d err=%s", code, errOut)
	}
	if gotCreate["harness"] != "agy" || gotCreate["id"] != "writer" {
		t.Fatalf("unexpected create payload: %#v", gotCreate)
	}

	// Update only name and instructions-file; description/model/harness must not be sent.
	if _, errOut, code := run("bot", "update", "--name", "Jarvis", "--instructions-file", "AGENTS.md"); code != 0 {
		t.Fatalf("bot update failed: code=%d err=%s", code, errOut)
	}
	if gotPatch["name"] != "Jarvis" || gotPatch["instructions"] != "# Jarvis\nBe concise.\n" {
		t.Fatalf("unexpected patch payload: %#v", gotPatch)
	}
	if _, hasModel := gotPatch["model"]; hasModel {
		t.Fatalf("did not expect model in partial patch: %#v", gotPatch)
	}
	if _, hasDesc := gotPatch["description"]; hasDesc {
		t.Fatalf("did not expect description in partial patch: %#v", gotPatch)
	}

	t.Setenv("BOT_HUB_HOP", "2")
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
			"bot-hub": map[string]any{
				"command": "/usr/local/bin/bot-hub",
				"args":    []string{"mcp", "--hub-url=" + api.URL, "--bot-id=coder"},
				"env":     map[string]string{"BOT_HUB_API_TOKEN": "tok-agy"},
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

	// computer_use role blocks nested desktop task.
	t.Setenv("BOT_HUB_MCP_ROLE", "computer_use")
	var out, errBuf bytes.Buffer
	if code := runCommand(context.Background(), []string{"desktop", "task", "--goal", "nested"}, ws, &out, &errBuf); code == 0 {
		t.Fatalf("expected computer_use role to reject nested desktop task")
	}
}
