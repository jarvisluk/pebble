package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bot-hub/internal/config"
)

func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ACP_HELPER") != "1" {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg rpcMessage
		if json.Unmarshal([]byte(line), &msg) != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}`+"\n", string(msg.ID))
		case "session/new":
			fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"acp-sess-1"}}`+"\n", string(msg.ID))
		case "session/prompt":
			// Send a tool_call update, request permission, then send message chunks.
			fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","title":"read_file"}}}`)
			fmt.Println(`{"jsonrpc":"2.0","id":999,"method":"session/request_permission","params":{"options":[{"optionId":"opt-allow","kind":"allow_once"}]}}`)
			// Wait for permission response from client.
			if sc.Scan() {
				fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello "}}}}`)
				fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"acp"}}}}`)
				fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}`+"\n", string(msg.ID))
			}
		}
	}
	os.Exit(0)
}

func TestACPHarnessRunTurn(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := NewACPHarness(ACPOptions{
		Kind:      "codebuddy",
		Label:     "CodeBuddy CLI",
		Binaries:  []string{exe},
		Args:      []string{"-test.run=TestACPHelperProcess"},
		EnvPrefix: "CODEBUDDY",
	})
	t.Cleanup(h.Close)

	t.Setenv("CODEBUDDY_BIN", exe)
	t.Setenv("GO_WANT_ACP_HELPER", "1")

	var streamed strings.Builder
	var tools []string
	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "b1", Workspace: t.TempDir()},
		Prompt: "hi",
		OnText: func(c string) { streamed.WriteString(c) },
		OnToolUse: func(name string) {
			tools = append(tools, name)
		},
	})
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if res.SessionID != "acp-sess-1" {
		t.Fatalf("SessionID = %q, want acp-sess-1", res.SessionID)
	}
	if res.Text != "hello acp" || streamed.String() != "hello acp" {
		t.Fatalf("Text = %q, streamed = %q", res.Text, streamed.String())
	}
	if len(tools) != 1 || tools[0] != "read_file" {
		t.Fatalf("tools = %v, want [read_file]", tools)
	}
}

func TestACPHarnessPassesMCPAndIsolatesEphemeral(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "acp.log")
	script := filepath.Join(dir, "mock-acp")
	body := `#!/bin/sh
echo "START $$" >> "` + logFile + `"
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      echo "NEW_SESSION $line" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"acp-ses-1"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewACPHarness(ACPOptions{
		Kind:      "codebuddy",
		Label:     "CodeBuddy CLI",
		Binaries:  []string{script},
		EnvPrefix: "CODEBUDDY",
	})
	defer h.Close()

	bot := config.BotConfig{ID: "b1", Workspace: dir}
	opts := RunOptions{
		Bot:        bot,
		Prompt:     "hi",
		MCPCommand: []string{"/usr/local/bin/pebble", "mcp", "--bot-id", "b1"},
		MCPEnv:     map[string]string{"PEBBLE_API_TOKEN": "tok-acp"},
	}
	if _, err := h.RunTurn(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	opts.Ephemeral = true
	if _, err := h.RunTurn(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	opts.Ephemeral = false
	if _, err := h.RunTurn(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(line, "START ") {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("expected 2 ACP process starts (1 pooled + 1 ephemeral), got %d:\n%s", starts, raw)
	}
	if !strings.Contains(string(raw), `"name":"pebble"`) || !strings.Contains(string(raw), `"PEBBLE_API_TOKEN"`) {
		t.Fatalf("expected session/new to include pebble mcpServers, got:\n%s", raw)
	}
}

func TestACPHarnessListCommandsAndAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".codebuddy", "skills", "cb-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".codebuddy", "skills", "cb-skill", "SKILL.md"), []byte("---\ndescription: CB技能\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, ".codebuddy", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".codebuddy", "agents", "helper.md"), []byte("---\ndescription: 助手\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewCodeBuddyHarness()
	h.captureUpdateCommands("b1", rpcMessage{
		Method: "session/update",
		Params: json.RawMessage(`{"update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"deploy","description":"发布","input":{"hint":"<env>"}}]}}`),
	})

	bot := config.BotConfig{ID: "b1", Workspace: ws}
	cmds, err := h.ListCommands(context.Background(), bot)
	if err != nil {
		t.Fatal(err)
	}
	assertHasCommand(t, cmds, "deploy")
	assertHasCommand(t, cmds, "compact")
	assertHasCommand(t, cmds, "cb-skill")

	agents, err := h.ListAgents(context.Background(), bot)
	if err != nil {
		t.Fatal(err)
	}
	assertHasAgent(t, agents, "helper")
}
