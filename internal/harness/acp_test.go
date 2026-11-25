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
      echo "INITIALIZE $line" >> "` + logFile + `"
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
	rawStr := string(raw)
	if !strings.Contains(rawStr, `INITIALIZE `) || !strings.Contains(rawStr, `"clientInfo":{"name":"pebble"`) {
		t.Fatalf("expected initialize clientInfo.name=pebble, got:\n%s", raw)
	}
	if !strings.Contains(rawStr, `"name":"pebble"`) || strings.Contains(rawStr, `"name":"bot-hub"`) || !strings.Contains(rawStr, `"PEBBLE_API_TOKEN"`) {
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


func TestACPPermissionModesAndCancellation(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "acp-perm.log")
	script := filepath.Join(dir, "mock-acp")
	body := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"acp-p1","modes":{"availableModes":[{"id":"default"},{"id":"accept-edits"},{"id":"plan"}]}}}\n' "$id"
      ;;
    *'"method":"session/set_mode"'*)
      echo "MODE:$line" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/cancel"'*)
      echo "CANCEL:$line" >> "` + logFile + `"
      ;;
    *'"method":"session/prompt"'*)
      if printf '%s' "$line" | grep -q "allow_always_syn"; then
        printf '{"jsonrpc":"2.0","id":"str-1","method":"session/request_permission","params":{"sessionId":"acp-p1","options":[{"optionId":"opt-once","kind":"allow_once","name":"Allow"},{"optionId":"opt-deny","kind":"reject_once","name":"Deny"}],"toolCall":{"toolCallId":"tc-1","name":"Bash","kind":"execute","args":{"command":"date"}}}}\n'
        IFS= read -r resp
        echo "RESP:$resp" >> "` + logFile + `"
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":"str-hang","method":"session/request_permission","params":{"sessionId":"acp-p1","options":[{"optionId":"opt-once","kind":"allow_once","name":"Allow"}],"toolCall":{"toolCallId":"tc-2","name":"Bash","args":{"command":"sleep 10"}}}}\n'
        while IFS= read -r extra; do
          case "$extra" in
            *'"method":"session/cancel"'*)
              echo "CANCEL:$extra" >> "` + logFile + `"
              printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"cancelled"}}\n' "$id"
              break
              ;;
          esac
        done
      fi
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewACPHarness(ACPOptions{Kind: "codebuddy", Label: "CodeBuddy", Binaries: []string{script}, EnvPrefix: "CB_TEST"})
	defer h.Close()

	// Synthetic session_allow option chosen by user -> must map back to real protocol optionId "opt-once".
	var sawSynthetic bool
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "b1", Workspace: dir, PermissionMode: config.PermissionModeEdit},
		Prompt: "allow_always_syn",
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			for _, o := range req.Options {
				if o.ID == "session_allow" && o.Kind == "allow_always" {
					sawSynthetic = true
					return PermissionResponse{Decision: "allow_always", OptionID: o.ID}, nil
				}
			}
			t.Fatalf("missing synthetic session_allow option: %+v", req.Options)
			return PermissionResponse{}, nil
		},
	}); err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if !sawSynthetic {
		t.Fatal("did not see synthetic session_allow")
	}

	// Cancel turn -> sends session/cancel and cancels OnPermission.
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	go func() {
		<-entered
		cancel()
	}()
	_, err := h.RunTurn(ctx, RunOptions{
		Bot:    config.BotConfig{ID: "b1", Workspace: dir, PermissionMode: config.PermissionModeAsk},
		Prompt: "hang",
		OnPermission: func(pCtx context.Context, _ PermissionRequest) (PermissionResponse, error) {
			close(entered)
			<-pCtx.Done()
			return PermissionResponse{Decision: "cancelled"}, pCtx.Err()
		},
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("expected interrupted error, got %v", err)
	}

	raw, _ := os.ReadFile(logFile)
	s := string(raw)
	for _, want := range []string{
		`"modeId":"accept-edits"`,
		`"id":"str-1"`,
		`"optionId":"opt-once"`,
		`"modeId":"default"`,
		`CANCEL:`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("ACP log missing %q:\n%s", want, s)
		}
	}
}
