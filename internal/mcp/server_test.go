package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMCPStdioToolsFlow(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"bots":[{"id":"assistant","name":"Assistant"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots/researcher/message":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"botId":"researcher","sessionId":"ses_r","text":"done"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_bots","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"message_bot","arguments":{"botId":"researcher","message":"hi"}}}`,
		"",
	}, "\n")

	var out bytes.Buffer
	srv := NewServer(api.URL, "assistant")
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 JSON-RPC responses, got %d:\n%s", len(lines), out.String())
	}

	var listResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatalf("parse tools/list: %v", err)
	}
	if len(listResp.Result.Tools) != 11 {
		t.Fatalf("expected 11 tools, got %d", len(listResp.Result.Tools))
	}
	if !strings.Contains(lines[3], "Reply from bot:researcher") {
		t.Fatalf("expected message_bot reply, got %s", lines[3])
	}
}

func TestMCPSessionRecallAndRotateTools(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions":
			if r.URL.Query().Get("callerBotId") != "assistant" || r.URL.Query().Get("botId") != "*" || r.URL.Query().Get("query") != "docker" {
				http.Error(w, `{"error":"bad list_sessions query"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"sessions":[{"sessionId":"ses_1","botId":"ops","status":"error"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/previous":
			if r.URL.Query().Get("callerBotId") != "assistant" || r.URL.Query().Get("botId") != "assistant" || r.URL.Query().Get("tail") != "5" {
				http.Error(w, `{"error":"bad recall_session query"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"session":{"sessionId":"ses_prev"},"messages":[{"id":"m1","text":"old context"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots/assistant/rotate":
			var body struct {
				HandoffNote string `json:"handoffNote"`
				Defer       bool   `json:"defer"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !body.Defer || body.HandoffNote != "clean handoff" {
				http.Error(w, `{"error":"bad rotate payload"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"deferred":true,"sessionId":"ses_cur"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sessions","arguments":{"botId":"*","query":"docker"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"recall_session","arguments":{"tail":5}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rotate_session","arguments":{"handoffNote":"clean handoff"}}}`,
		"",
	}, "\n")

	var out bytes.Buffer
	srv := NewServer(api.URL, "assistant")
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 responses, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "ses_1") || !strings.Contains(lines[1], "old context") || !strings.Contains(lines[2], `"deferred": true`) && !strings.Contains(lines[2], `\"deferred\": true`) {
		t.Fatalf("unexpected session/rotate outputs:\n%s", out.String())
	}
}

func TestMCPNodeToolsAndTokenRotation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config", "bots.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"http":{"apiToken":"tok-v2"},"bots":[{"id":"assistant"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var authCalls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-v2" {
			authCalls.Add(1)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/nodes":
			_, _ = w.Write([]byte(`{"nodes":[{"id":"macbook","connected":true,"permissions":["files","exec","desktop"]}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/nodes/_default/call":
			var body struct {
				Action string `json:"action"`
				BotID  string `json:"botId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body.Action {
			case "exec":
				_, _ = w.Write([]byte(`{"callId":"1","nodeId":"macbook","ok":true,"exitCode":0,"output":"ok-exec"}`))
			case "file_read":
				_, _ = w.Write([]byte(`{"callId":"2","nodeId":"macbook","ok":true,"output":"file-body"}`))
			default:
				http.NotFound(w, r)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/desktop/takeover/request":
			_, _ = w.Write([]byte(`{"ok":true,"note":"人类已扫码完成"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	t.Setenv("BOT_HUB_API_TOKEN", "tok-v1")
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_nodes","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"node_exec","arguments":{"command":"uname"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"node_file","arguments":{"op":"read","path":"~/a.txt"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"request_takeover","arguments":{"reason":"请扫码登录"}}}`,
		"",
	}, "\n")

	var out bytes.Buffer
	srv := NewServerWithConfig(api.URL, "assistant", cfgPath)
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 responses, got %d:\n%s", len(lines), out.String())
	}
	if authCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 401 retry before caching fresh token, got %d", authCalls.Load())
	}
	if !strings.Contains(lines[0], "macbook") || !strings.Contains(lines[1], "ok-exec") ||
		!strings.Contains(lines[2], "file-body") || !strings.Contains(lines[3], "人类已扫码完成") {
		t.Fatalf("unexpected node/takeover tool outputs:\n%s", out.String())
	}
}
