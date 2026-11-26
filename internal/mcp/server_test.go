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
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"message_bot","arguments":{"botId":"researcher","message":"hi"}}}`,
		"",
	}, "\n")

	var out bytes.Buffer
	srv := NewServer(api.URL, "assistant")
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON-RPC responses, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"name":"pebble"`) {
		t.Fatalf("expected serverInfo.name=pebble in initialize response:\n%s", lines[0])
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
	want := map[string]bool{
		"send_message":     true,
		"message_bot":      true,
		"node_exec":        true,
		"node_file":        true,
		"desktop_task":     true,
		"request_takeover": true,
		"recall_session":   true,
		"open_stage":       true,
		"task_spawn":       true,
		"task_status":      true,
		"task_read":        true,
		"task_send":        true,
		"task_cancel":      true,
	}
	if len(listResp.Result.Tools) != len(want) {
		t.Fatalf("expected %d core tools, got %d: %+v", len(want), len(listResp.Result.Tools), listResp.Result.Tools)
	}
	for _, tool := range listResp.Result.Tools {
		if !want[tool.Name] {
			t.Fatalf("unexpected tool in MCP list: %q", tool.Name)
		}
	}
	if !strings.Contains(lines[2], "Reply from bot:researcher") {
		t.Fatalf("expected message_bot reply, got %s", lines[2])
	}
}

func TestMCPRecallSessionTool(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/sessions/previous" {
			if r.URL.Query().Get("callerBotId") != "assistant" || r.URL.Query().Get("botId") != "assistant" || r.URL.Query().Get("tail") != "5" {
				http.Error(w, `{"error":"bad recall_session query"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"session":{"sessionId":"ses_prev"},"messages":[{"id":"m1","text":"old context"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer api.Close()

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"recall_session","arguments":{"tail":5}}}` + "\n"

	var out bytes.Buffer
	srv := NewServer(api.URL, "assistant")
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}
	if !strings.Contains(out.String(), "old context") {
		t.Fatalf("unexpected recall_session output:\n%s", out.String())
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

	t.Setenv("PEBBLE_API_TOKEN", "tok-v1")
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"node_exec","arguments":{"command":"uname"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"node_file","arguments":{"op":"read","path":"~/a.txt"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"request_takeover","arguments":{"reason":"请扫码登录"}}}`,
		"",
	}, "\n")

	var out bytes.Buffer
	srv := NewServerWithConfig(api.URL, "assistant", cfgPath)
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 responses, got %d:\n%s", len(lines), out.String())
	}
	if authCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 401 retry before caching fresh token, got %d", authCalls.Load())
	}
	if !strings.Contains(lines[0], "ok-exec") ||
		!strings.Contains(lines[1], "file-body") || !strings.Contains(lines[2], "人类已扫码完成") {
		t.Fatalf("unexpected node/takeover tool outputs:\n%s", out.String())
	}
}

func TestMCPDesktopTaskAndRoleFiltering(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config", "bots.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"desktopModel":"magpie/flash","bots":[{"id":"assistant"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/desktop/task" {
			_, _ = w.Write([]byte(`{"ok":true,"text":"opened Safari and logged in"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer api.Close()

	// 1. Main role exposes desktop_task.
	mainInput := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"desktop_task","arguments":{"goal":"open Safari"}}}`,
		"",
	}, "\n")
	var mainOut bytes.Buffer
	mainSrv := NewServerWithConfig(api.URL, "assistant", cfgPath)
	if err := mainSrv.ServeStdio(context.Background(), strings.NewReader(mainInput), &mainOut); err != nil {
		t.Fatal(err)
	}
	mainLines := strings.Split(strings.TrimSpace(mainOut.String()), "\n")
	if !strings.Contains(mainLines[0], `"desktop_task"`) {
		t.Fatalf("expected desktop_task visible:\n%s", mainLines[0])
	}
	if !strings.Contains(mainLines[1], "opened Safari and logged in") {
		t.Fatalf("expected desktop_task output, got %s", mainLines[1])
	}

	// 2. computer_use role exposes 3 tools and blocks nested desktop_task (test PEBBLE_MCP_ROLE + legacy BOT_HUB_MCP_ROLE).
	t.Setenv("PEBBLE_MCP_ROLE", "computer_use")
	cuInput := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"desktop_task","arguments":{"goal":"nested"}}}`,
		"",
	}, "\n")
	var cuOut bytes.Buffer
	cuSrv := NewServerWithConfig(api.URL, "assistant", cfgPath)
	if err := cuSrv.ServeStdio(context.Background(), strings.NewReader(cuInput), &cuOut); err != nil {
		t.Fatal(err)
	}
	cuLines := strings.Split(strings.TrimSpace(cuOut.String()), "\n")
	var cuList struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(cuLines[0]), &cuList); err != nil {
		t.Fatal(err)
	}
	if len(cuList.Result.Tools) != 3 {
		t.Fatalf("expected 3 tools for computer_use role, got %d: %+v", len(cuList.Result.Tools), cuList.Result.Tools)
	}
	if strings.Contains(cuLines[0], `"desktop_task"`) || !strings.Contains(cuLines[0], `"node_exec"`) {
		t.Fatalf("expected computer_use role to expose node_exec and hide desktop_task:\n%s", cuLines[0])
	}
	if !strings.Contains(cuLines[1], `"isError":true`) {
		t.Fatalf("expected nested desktop_task rejected, got %s", cuLines[1])
	}

	t.Setenv("PEBBLE_MCP_ROLE", "")
	t.Setenv("BOT_HUB_MCP_ROLE", "computer_use")
	legacySrv := NewServerWithConfig(api.URL, "assistant", cfgPath)
	if legacySrv.role != "computer_use" {
		t.Fatalf("expected legacy BOT_HUB_MCP_ROLE fallback, got %q", legacySrv.role)
	}
}

func TestMCPSendMessageTool(t *testing.T) {
	var gotBody map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/bots/assistant/outbound" {
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				http.Error(w, `{"error":"bad json"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"id":"msg_1","media":[{"id":"m1"},{"id":"m2"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer api.Close()

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_message","arguments":{"text":"hello","images":["a.png",{"path":"b.png","alt":"diagram"}],"files":["report.pdf",{"path":"data.csv","name":"metrics.csv"}]}}}` + "\n"
	var out bytes.Buffer
	srv := NewServer(api.URL, "assistant")
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}
	if !strings.Contains(out.String(), "Message sent to user (id=msg_1, attachments=2)") {
		t.Fatalf("unexpected send_message output:\n%s", out.String())
	}
	imgs, ok := gotBody["images"].([]any)
	if !ok || len(imgs) != 2 {
		t.Fatalf("expected 2 images forwarded, got %#v", gotBody["images"])
	}
	files, ok := gotBody["files"].([]any)
	if !ok || len(files) != 2 {
		t.Fatalf("expected 2 files forwarded, got %#v", gotBody["files"])
	}
}

func TestMCPTaskToolsAndCoordinatorApproval(t *testing.T) {
	var gotSpawn, gotSend, gotApprove map[string]any
	var gotAsyncMsg map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots/assistant/tasks":
			_ = json.NewDecoder(r.Body).Decode(&gotSpawn)
			_, _ = w.Write([]byte(`{"id":"tsk_1","botId":"assistant","kind":"worker","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks/tsk_1":
			_, _ = w.Write([]byte(`{"id":"tsk_1","botId":"assistant","status":"completed","output":"done"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks/tsk_1/events":
			_, _ = w.Write([]byte(`{"events":[{"seq":1,"taskId":"tsk_1","eventType":"status"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots/assistant/tasks":
			_, _ = w.Write([]byte(`{"tasks":[{"id":"tsk_1","status":"completed"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/tasks/tsk_1/send":
			_ = json.NewDecoder(r.Body).Decode(&gotSend)
			_, _ = w.Write([]byte(`{"id":"tsk_1","status":"queued"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/tasks/tsk_1/cancel":
			_, _ = w.Write([]byte(`{"id":"tsk_1","status":"cancelled"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/approvals/apr_1/reply":
			_ = json.NewDecoder(r.Body).Decode(&gotApprove)
			_, _ = w.Write([]byte(`{"ok":true,"items":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/bots/researcher/message":
			_ = json.NewDecoder(r.Body).Decode(&gotAsyncMsg)
			_, _ = w.Write([]byte(`{"async":true,"task":{"id":"tsk_msg_1","status":"queued"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	t.Setenv("PEBBLE_COORDINATOR_CAN_APPROVE", "1")
	t.Setenv("PEBBLE_TASK_ID", "tsk_parent")
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"task_spawn","arguments":{"prompt":"audit repo","delivery":"wake"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"task_status","arguments":{"taskId":"tsk_1"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"task_read","arguments":{"taskId":"tsk_1","afterSeq":0}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"task_send","arguments":{"taskId":"tsk_1","message":"more detail"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"task_cancel","arguments":{"taskId":"tsk_1"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"task_approve","arguments":{"approvalId":"apr_1","decision":"allow_once"}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"message_bot","arguments":{"botId":"researcher","message":"bg check","async":true}}}`,
		"",
	}, "\n")

	var out bytes.Buffer
	srv := NewServer(api.URL, "assistant")
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 8 {
		t.Fatalf("expected 8 responses, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"task_approve"`) {
		t.Fatalf("expected task_approve exposed when PEBBLE_COORDINATOR_CAN_APPROVE=1:\n%s", lines[0])
	}
	if gotSpawn["prompt"] != "audit repo" || gotSpawn["delivery"] != "wake" || gotSpawn["parentTaskId"] != "tsk_parent" {
		t.Fatalf("unexpected task_spawn payload: %#v", gotSpawn)
	}
	var readResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &readResp); err != nil || len(readResp.Result.Content) == 0 ||
		!strings.Contains(readResp.Result.Content[0].Text, `"events"`) || !strings.Contains(readResp.Result.Content[0].Text, `"tsk_1"`) {
		t.Fatalf("unexpected task_read output: %s", lines[3])
	}
	if gotSend["message"] != "more detail" {
		t.Fatalf("unexpected task_send payload: %#v", gotSend)
	}
	if !strings.Contains(lines[5], "cancelled") {
		t.Fatalf("unexpected task_cancel output: %s", lines[5])
	}
	if gotApprove["decision"] != "allow_once" || gotApprove["callerBotId"] != "assistant" {
		t.Fatalf("unexpected task_approve payload: %#v", gotApprove)
	}
	if gotAsyncMsg["async"] != true {
		t.Fatalf("unexpected async message_bot payload: %#v", gotAsyncMsg)
	}
}
