package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bot-hub/internal/config"
)

func TestAgyHarnessRunTurnAndWorkspaceFiles(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(filepath.Join(ws, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Existing third-party server should be preserved while legacy bot-hub server/rule migrate.
	if err := os.MkdirAll(filepath.Join(ws, ".agents", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".agents", "mcp_config.json"), []byte(`{"mcpServers":{"keep":{"command":"echo"},"bot-hub":{"command":"/old/bot-hub"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".agents", "rules", "bot-hub.md"), []byte("legacy rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	instPath := filepath.Join(tmp, "PEBBLE.md")
	if err := os.WriteFile(instPath, []byte("# Pebble Platform Guide\n- Stay focused.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeAgy := filepath.Join(tmp, "agy")
	script := `#!/bin/sh
if [ "$1" = "models" ]; then
  echo "Fetching available models..."
  echo "gemini-4-neon-medium	Gemini 4 Neon (Medium)"
  echo "claude-sonnet-5-5-medium	Claude Sonnet 5.5 (Medium)"
  exit 0
fi
echo '{"event":"init","conversation_id":"conv-agy-1","init":{"cwd":"."}}'
turn=0
while IFS= read -r line; do
  turn=$((turn + 1))
  if [ "$turn" = "1" ]; then
    echo '{"event":"step_update","step_update":{"conversation_id":"conv-agy-1","step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"run_command"}}'
    echo '{"event":"step_update","step_update":{"conversation_id":"conv-agy-1","step_index":1,"state":"DONE","step_type":"tool","tool_name":"run_command"}}'
    echo '{"event":"step_update","step_update":{"conversation_id":"conv-agy-1","step_index":2,"state":"ACTIVE","step_type":"agent_response","text_delta":"Hel"}}'
    echo '{"event":"step_update","step_update":{"conversation_id":"conv-agy-1","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"lo"}}'
    echo '{"event":"result","result":{"conversation_id":"conv-agy-1","status":"SUCCESS","response":"Hello","usage":{"input_tokens":100,"output_tokens":20,"cache_read_tokens":50}}}'
  else
    echo '{"event":"step_update","step_update":{"conversation_id":"conv-agy-1","step_index":3,"state":"DONE","step_type":"agent_response","text_delta":"World"}}'
    echo '{"event":"result","result":{"conversation_id":"conv-agy-1","status":"SUCCESS","response":"World","usage":{"input_tokens":130,"output_tokens":35,"cache_read_tokens":120}}}'
  fi
done
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGY_BIN", fakeAgy)

	h := NewAgyHarness()
	t.Cleanup(h.Close)

	models, err := h.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "gemini-4-neon-medium" || models[1] != "claude-sonnet-5-5-medium" {
		t.Fatalf("unexpected models: %+v", models)
	}

	var streamed strings.Builder
	var tools []string
	res1, err := h.RunTurn(context.Background(), RunOptions{
		Bot:                      config.BotConfig{ID: "dev", Workspace: ws, Model: "gemini-4-neon-medium"},
		Prompt:                   "hi",
		PlatformInstructionsPath: instPath,
		MCPCommand:               []string{"/usr/local/bin/pebble", "mcp", "--bot-id", "dev"},
		MCPEnv:                   map[string]string{"PEBBLE_API_TOKEN": "tok-123"},
		OnText:                   func(c string) { streamed.WriteString(c) },
		OnToolUse:                func(name string) { tools = append(tools, name) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res1.SessionID != "conv-agy-1" || res1.Text != "Hello" || streamed.String() != "Hello" {
		t.Fatalf("unexpected turn 1 result: %+v streamed=%q", res1, streamed.String())
	}
	if len(tools) != 1 || tools[0] != "run_command" {
		t.Fatalf("expected tool run_command once, got %+v", tools)
	}
	if res1.Usage.InputTokens != 100 || res1.Usage.OutputTokens != 20 || res1.Usage.CacheReadTokens != 50 {
		t.Fatalf("unexpected turn 1 usage: %+v", res1.Usage)
	}

	// Second turn reuses the same process and computes delta usage from cumulative counters.
	res2, err := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "dev", Workspace: ws, Model: "gemini-4-neon-medium"},
		SessionID: res1.SessionID,
		Prompt:    "next",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Text != "World" || res2.Usage.InputTokens != 30 || res2.Usage.OutputTokens != 15 || res2.Usage.CacheReadTokens != 70 {
		t.Fatalf("unexpected turn 2 result: %+v", res2)
	}

	// Verify .agents/mcp_config.json and .agents/rules/pebble.md (and legacy bot-hub removal)
	mcpRaw, err := os.ReadFile(filepath.Join(ws, ".agents", "mcp_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(ws, ".agents", "mcp_config.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions on mcp_config.json, got %v (err=%v)", info.Mode().Perm(), err)
	}
	var mcpDoc map[string]map[string]any
	if err := json.Unmarshal(mcpRaw, &mcpDoc); err != nil {
		t.Fatal(err)
	}
	if mcpDoc["mcpServers"]["keep"] == nil || mcpDoc["mcpServers"]["pebble"] == nil || mcpDoc["mcpServers"]["bot-hub"] != nil {
		t.Fatalf("unexpected mcp_config.json: %s", mcpRaw)
	}

	ruleRaw, err := os.ReadFile(filepath.Join(ws, ".agents", "rules", "pebble.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ruleRaw), "trigger: always_on") || !strings.Contains(string(ruleRaw), "Stay focused.") {
		t.Fatalf("unexpected rule file:\n%s", ruleRaw)
	}
	if _, err := os.Stat(filepath.Join(ws, ".agents", "rules", "bot-hub.md")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy .agents/rules/bot-hub.md removed, got err=%v", err)
	}
}

func TestAgyModelsStripsSpinner(t *testing.T) {
	tmp := t.TempDir()
	fakeAgy := filepath.Join(tmp, "agy")
	script := "#!/bin/sh\n" +
		"printf '\\r⠋ Fetching available models...\\r⠙ Fetching available models...\\rgemini-4-neon-high             Gemini 4 Neon (High)\\n'\n" +
		"printf 'claude-opus-5-5-high           Claude Opus 5.5 (High)\\n'\n"
	if err := os.WriteFile(fakeAgy, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &AgyHarness{binary: fakeAgy}
	models, err := h.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "gemini-4-neon-high" || models[1] != "claude-opus-5-5-high" {
		t.Fatalf("unexpected parsed agy models: %#v", models)
	}
}

func TestAgyEphemeralDoesNotReuseOrPollutePool(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	pidLog := filepath.Join(tmp, "pids.log")
	fakeAgy := filepath.Join(tmp, "agy")
	script := `#!/bin/sh
if [ "$1" = "acp" ] && [ "$2" = "--help" ]; then
  exit 1
fi
echo "$$:$*" >> ` + pidLog + `
while IFS= read -r line; do
  echo '{"event":"result","result":{"conversation_id":"conv-1","status":"SUCCESS","response":"ok"}}'
done
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &AgyHarness{binary: fakeAgy}
	defer h.Close()

	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "dev", Workspace: ws},
		SessionID: "conv-1",
		Prompt:    "main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "dev", Workspace: ws},
		SessionID: "conv-1",
		Ephemeral: true,
		Prompt:    "routine",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "dev", Workspace: ws},
		SessionID: "conv-1",
		Prompt:    "main2",
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(pidLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 process launches (1 persistent + 1 ephemeral), got %d:\n%s", len(lines), raw)
	}
	if !strings.Contains(lines[0], "--conversation conv-1") {
		t.Fatalf("expected persistent launch to include --conversation, got %q", lines[0])
	}
	if strings.Contains(lines[1], "--conversation") {
		t.Fatalf("expected ephemeral launch to omit --conversation, got %q", lines[1])
	}
}


func TestAgyACPPermissionModesAndApproval(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(tmp, "acp-events.log")
	pidLog := filepath.Join(tmp, "acp-pids.log")
	fakeAgy := filepath.Join(tmp, "agy")

	script := `#!/bin/sh
if [ "$1" = "acp" ] && [ "$2" = "--help" ]; then
  echo "Start the Agent Client Protocol (ACP) stdio server"
  exit 0
fi
echo "PID:$$ ARGS:$*" >> "` + pidLog + `"
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      echo "RESUME:$line" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"not found"}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      echo "NEW:$line" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"agy-acp-1","modes":{"currentModeId":"default","availableModes":[{"id":"default"},{"id":"accept-edits"},{"id":"auto"},{"id":"plan"}]},"models":{"currentModelId":"gemini-4-neon-medium"}}}\n' "$id"
      ;;
    *'"method":"session/set_mode"'*)
      echo "SET_MODE:$line" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      echo "SET_MODEL:$line" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/cancel"'*)
      echo "CANCEL:$line" >> "` + logFile + `"
      ;;
    *'"method":"session/prompt"'*)
      echo "PROMPT:$line" >> "` + logFile + `"
      if printf '%s' "$line" | grep -q "trigger_perm"; then
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"audit","description":"Audit code"}]}}}\n'
        printf '{"jsonrpc":"2.0","id":"perm-str-1","method":"session/request_permission","params":{"sessionId":"agy-acp-1","options":[{"optionId":"allow","kind":"allow_once","name":"Allow"},{"optionId":"deny","kind":"reject_once","name":"Deny"}],"toolCall":{"toolCallId":"tc-1","name":"run_command","kind":"execute","title":"Run shell","args":{"command":"echo ok","api_key":"secret-val"}}}}\n'
        IFS= read -r permResp
        echo "PERM_RESP:$permResp" >> "` + logFile + `"
      fi
      if printf '%s' "$line" | grep -q "trigger_hang"; then
        printf '{"jsonrpc":"2.0","id":"perm-hang","method":"session/request_permission","params":{"sessionId":"agy-acp-1","options":[{"optionId":"allow","kind":"allow_once","name":"Allow"}],"toolCall":{"toolCallId":"tc-2","name":"run_command","kind":"execute","args":{"command":"sleep 10"}}}}\n'
        while IFS= read -r extra; do
          echo "AFTER_HANG:$extra" >> "` + logFile + `"
          if printf '%s' "$extra" | grep -q '"method":"session/cancel"'; then
            printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"cancelled"}}\n' "$id"
            exit 0
          fi
        done
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","kind":"execute","title":"run_command"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"acp ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":12,"outputTokens":8,"cachedReadTokens":4}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewAgyHarness()
	h.binary = fakeAgy
	defer h.Close()

	caps := h.PermissionCapabilities()
	if !caps.InteractiveApproval || !caps.SupportsAllowAlways || len(caps.Modes) != 4 {
		t.Fatalf("unexpected PermissionCapabilities: %+v", caps)
	}

	// Create tiny 1x1 PNG attachment to verify ACP image block generation.
	imgPath := filepath.Join(tmp, "tiny.png")
	if err := os.WriteFile(imgPath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Turn 1: edit mode
	res1, err := h.RunTurn(context.Background(), RunOptions{
		Bot: config.BotConfig{
			ID:             "dev",
			Workspace:      ws,
			Model:          "gemini-4-neon-medium",
			PermissionMode: config.PermissionModeEdit,
		},
		Prompt:      "first",
		Attachments: []Attachment{{ID: "img1", Kind: "image", Name: "tiny.png", MimeType: "image/png", Path: imgPath}},
	})
	if err != nil {
		t.Fatalf("turn 1 error: %v", err)
	}
	if res1.SessionID != "agy-acp-1" || res1.Usage.InputTokens != 12 || res1.Usage.OutputTokens != 8 || res1.Usage.CacheReadTokens != 4 {
		t.Fatalf("unexpected turn 1 result: %+v", res1)
	}

	// Turn 2: switch to ask mode (hot set_mode without restarting process) and trigger permission.
	var gotReq PermissionRequest
	res2, err := h.RunTurn(context.Background(), RunOptions{
		Bot: config.BotConfig{
			ID:             "dev",
			Workspace:      ws,
			Model:          "gemini-4-neon-medium",
			PermissionMode: config.PermissionModeAsk,
		},
		SessionID: res1.SessionID,
		Prompt:    "trigger_perm",
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			gotReq = req
			return PermissionResponse{Decision: "allow_once"}, nil
		},
	})
	if err != nil {
		t.Fatalf("turn 2 error: %v", err)
	}
	if res2.Text != "acp ok" || gotReq.Kind != "shell" || gotReq.RawInput["api_key"] != "[REDACTED]" {
		t.Fatalf("unexpected turn 2 result/perm: res=%+v req=%+v", res2, gotReq)
	}

	// Turn 3: switch to plan mode (still reusing same pooled ACP process).
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot: config.BotConfig{
			ID:             "dev",
			Workspace:      ws,
			Model:          "gemini-4-neon-medium",
			PermissionMode: config.PermissionModePlan,
		},
		SessionID: res1.SessionID,
		Prompt:    "plan step",
	}); err != nil {
		t.Fatalf("turn 3 error: %v", err)
	}

	// Verify available_commands_update cached into ListCommands.
	cmds, err := h.ListCommands(context.Background(), config.BotConfig{ID: "dev", Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	assertHasCommand(t, cmds, "audit")

	pidRaw, err := os.ReadFile(pidLog)
	if err != nil {
		t.Fatal(err)
	}
	pidLines := strings.Split(strings.TrimSpace(string(pidRaw)), "\n")
	if len(pidLines) != 1 {
		t.Fatalf("expected 1 pooled ACP process across edit->ask->plan switches, got %d:\n%s", len(pidLines), pidRaw)
	}
	if strings.Contains(pidLines[0], "--dangerously-skip-permissions") {
		t.Fatalf("non-auto ACP launch must not pass --dangerously-skip-permissions: %s", pidLines[0])
	}

	evtRaw, _ := os.ReadFile(logFile)
	evtStr := string(evtRaw)
	for _, want := range []string{
		`"type":"image"`,
		`"modeId":"accept-edits"`,
		`"modeId":"default"`,
		`"modeId":"plan"`,
		`"id":"perm-str-1"`,
		`"optionId":"allow"`,
	} {
		if !strings.Contains(evtStr, want) {
			t.Fatalf("expected %q in ACP log, got:\n%s", want, evtStr)
		}
	}

	// Turn 4: Interrupt during pending permission sends session/cancel and returns interrupted.
	turnCtx, cancelTurn := context.WithCancel(context.Background())
	permStarted := make(chan struct{})
	go func() {
		<-permStarted
		cancelTurn()
	}()
	_, intErr := h.RunTurn(turnCtx, RunOptions{
		Bot: config.BotConfig{
			ID:             "dev",
			Workspace:      ws,
			PermissionMode: config.PermissionModeAsk,
		},
		SessionID: res1.SessionID,
		Prompt:    "trigger_hang",
		OnPermission: func(pCtx context.Context, _ PermissionRequest) (PermissionResponse, error) {
			close(permStarted)
			<-pCtx.Done()
			return PermissionResponse{Decision: "cancelled"}, pCtx.Err()
		},
	})
	if intErr == nil || !strings.Contains(intErr.Error(), "interrupted") {
		t.Fatalf("expected interrupted error, got %v", intErr)
	}
}

func TestAgyACPSetsModelAndIsolatesEphemeral(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeAgy := filepath.Join(tmp, "agy")
	logFile := filepath.Join(tmp, "acp.log")

	agyScript := `#!/bin/sh
if [ "$1" = "acp" ] && [ "$2" = "--help" ]; then
  echo "Agent Client Protocol"
  exit 0
fi
echo "LAUNCH:$*" >> "` + logFile + `"
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      echo "NEW" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"acp-1"}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      echo "RESUME" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"acp-1"}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      echo "SET_MODEL:$line" >> "` + logFile + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"via acp"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(fakeAgy, []byte(agyScript), 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewAgyHarness()
	h.binary = fakeAgy
	defer h.Close()

	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot: config.BotConfig{
			ID:             "dev",
			Workspace:      ws,
			Model:          "claude-opus",
			PermissionMode: config.PermissionModeAuto,
		},
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("acp RunTurn failed: %v", err)
	}
	if res.Text != "via acp" {
		t.Fatalf("unexpected text: %q", res.Text)
	}

	// Ephemeral turn launches separately without session/resume.
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot: config.BotConfig{
			ID:             "dev",
			Workspace:      ws,
			Model:          "claude-opus",
			PermissionMode: config.PermissionModeAuto,
		},
		SessionID: "acp-1",
		Ephemeral: true,
		Prompt:    "routine",
	}); err != nil {
		t.Fatalf("ephemeral Turn failed: %v", err)
	}

	raw, _ := os.ReadFile(logFile)
	s := string(raw)
	if !strings.Contains(s, "SET_MODEL:") {
		t.Fatalf("expected session/set_model to be called:\n%s", s)
	}
	if !strings.Contains(s, "LAUNCH:acp --dangerously-skip-permissions") {
		t.Fatalf("expected direct agy acp launch with --dangerously-skip-permissions:\n%s", s)
	}
	if strings.Contains(s, "RESUME") {
		t.Fatalf("ephemeral turn must not call session/resume:\n%s", s)
	}
}

func TestAgyStreamJSONFallback(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	argsLog := filepath.Join(tmp, "args.log")
	fakeAgy := filepath.Join(tmp, "agy")
	// Binary pretends to support ACP on --help, then emits stream-json init line so harness falls back.
	script := `#!/bin/sh
if [ "$1" = "acp" ] && [ "$2" = "--help" ]; then
  echo "agy acp"
  exit 0
fi
echo "ARGS:$*" >> "` + argsLog + `"
echo '{"event":"init","conversation_id":"fallback-1"}'
while IFS= read -r line; do
  echo '{"event":"result","result":{"conversation_id":"fallback-1","status":"SUCCESS","response":"fallback ok"}}'
done
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewAgyHarness()
	h.binary = fakeAgy
	defer h.Close()

	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot: config.BotConfig{
			ID:             "dev",
			Workspace:      ws,
			PermissionMode: config.PermissionModeEdit,
		},
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("fallback RunTurn failed: %v", err)
	}
	if res.Text != "fallback ok" || res.SessionID != "fallback-1" {
		t.Fatalf("unexpected fallback result: %+v", res)
	}
	raw, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(raw), "--mode accept-edits") {
		t.Fatalf("expected fallback stream-json to pass --mode accept-edits, got:\n%s", raw)
	}
}
