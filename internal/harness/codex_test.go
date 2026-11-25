package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bot-hub/internal/config"
)

func TestCodexHarnessRunTurn(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "mock-codex")
	argsLog := filepath.Join(dir, "args.txt")
	body := `#!/bin/sh
echo "$*" >> "` + argsLog + `"
cat >/dev/null
echo '{"type":"thread.started","thread_id":"th-123"}'
echo '{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"ls"}}'
echo '{"type":"item.updated","item":{"id":"i2","type":"agent_message","text":"hello "}}'
echo '{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":"hello codex"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5,"cached_input_tokens":2}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &CodexHarness{binary: script}
	var streamed strings.Builder
	var tools []string
	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "b1", Workspace: dir, Model: "gpt-5.4"},
		Prompt:    "ping",
		SessionID: "th-prev",
		OnText:    func(c string) { streamed.WriteString(c) },
		OnToolUse: func(n string) { tools = append(tools, n) },
	})
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if res.SessionID != "th-123" {
		t.Fatalf("SessionID = %q, want th-123", res.SessionID)
	}
	if res.Text != "hello codex" {
		t.Fatalf("Text = %q, want 'hello codex'", res.Text)
	}
	if streamed.String() != "hello codex" {
		t.Fatalf("streamed = %q, want 'hello codex'", streamed.String())
	}
	if len(tools) != 1 || tools[0] != "shell" {
		t.Fatalf("tools = %v, want [shell]", tools)
	}
	if res.Usage.InputTokens != 10 || res.Usage.OutputTokens != 5 || res.Usage.CacheReadTokens != 2 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	rawArgs, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(rawArgs), "exec resume th-prev --json --full-auto --model gpt-5.4 -") {
		t.Fatalf("unexpected args: %q", string(rawArgs))
	}
}

func TestCodexEphemeralAndBotEnv(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "mock-codex")
	argsLog := filepath.Join(dir, "args.txt")
	envLog := filepath.Join(dir, "env.txt")
	body := `#!/bin/sh
echo "$*" >> "` + argsLog + `"
echo "$BOT_ONLY_VAR" > "` + envLog + `"
cat >/dev/null
echo '{"type":"thread.started","thread_id":"th-eph"}'
echo '{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"done"}}'
echo '{"type":"turn.completed"}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &CodexHarness{binary: script}
	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot: config.BotConfig{
			ID:        "b1",
			Workspace: dir,
			Env:       map[string]string{"BOT_ONLY_VAR": "from-bot"},
		},
		Prompt:    "ping",
		SessionID: "th-prev",
		Ephemeral: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "done" {
		t.Fatalf("Text = %q, want done", res.Text)
	}
	rawArgs, _ := os.ReadFile(argsLog)
	if strings.Contains(string(rawArgs), "resume") {
		t.Fatalf("ephemeral codex run must not resume session, got %q", string(rawArgs))
	}
	rawEnv, _ := os.ReadFile(envLog)
	if strings.TrimSpace(string(rawEnv)) != "from-bot" {
		t.Fatalf("expected BOT_ONLY_VAR=from-bot, got %q", string(rawEnv))
	}
}

func TestCodexListCommandsAndAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := t.TempDir()

	if err := os.MkdirAll(filepath.Join(ws, ".codex", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".codex", "prompts", "audit.md"), []byte("---\ndescription: 安全审计\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, ".codex", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".codex", "agents", "rev.toml"), []byte("name = \"reviewer\"\ndescription = \"评审\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewCodexHarness()
	bot := config.BotConfig{ID: "b1", Workspace: ws}
	cmds, err := h.ListCommands(context.Background(), bot)
	if err != nil {
		t.Fatal(err)
	}
	assertHasCommand(t, cmds, "compact")
	assertHasCommand(t, cmds, "review")
	assertHasCommand(t, cmds, "audit")

	agents, err := h.ListAgents(context.Background(), bot)
	if err != nil {
		t.Fatal(err)
	}
	assertHasAgent(t, agents, "reviewer")
}


func TestCodexAppServerApprovalAndModes(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideDir := filepath.Join(dir, "outside")
	_ = os.MkdirAll(outsideDir, 0o755)

	script := filepath.Join(dir, "mock-codex")
	logFile := filepath.Join(dir, "app.log")
	pidLog := filepath.Join(dir, "pids.log")

	body := `#!/bin/sh
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  echo "Usage: codex app-server --stdio"
  exit 0
fi
echo "PID:$$" >> "` + pidLog + `"
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"id":%s,"result":{"userAgent":"codex-test"}}\n' "$id"
      ;;
    *'"method":"initialized"'*)
      echo "INITIALIZED" >> "` + logFile + `"
      ;;
    *'"method":"thread/start"'*)
      echo "THREAD_START:$line" >> "` + logFile + `"
      printf '{"id":%s,"result":{"thread":{"id":"th-app-1"}}}\n' "$id"
      ;;
    *'"method":"turn/start"'*)
      echo "TURN_START:$line" >> "` + logFile + `"
      printf '{"id":%s,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}\n' "$id"
      if printf '%s' "$line" | grep -q "cmd_approve"; then
        printf '{"id":"srv-cmd","method":"item/commandExecution/requestApproval","params":{"itemId":"i1","command":"uname -a","cwd":"` + ws + `"}}\n'
        IFS= read -r a1
        echo "CMD_DECISION:$a1" >> "` + logFile + `"
        printf '{"id":"srv-perm","method":"item/permissions/requestApproval","params":{"itemId":"i2","cwd":"` + ws + `","reason":"net access","permissions":{"network":true}}}\n'
        IFS= read -r a2
        echo "PERM_DECISION:$a2" >> "` + logFile + `"
        printf '{"id":"srv-ui","method":"item/tool/requestUserInput","params":{"questions":[{"id":"q1","header":"Env","question":"Select env","options":[{"label":"prod","description":"Production"}]}]}}\n'
        IFS= read -r a3
        echo "UI_ANS:$a3" >> "` + logFile + `"
      elif printf '%s' "$line" | grep -q "file_outside"; then
        printf '{"id":"srv-fc","method":"item/fileChange/requestApproval","params":{"itemId":"f1","grantRoot":"` + outsideDir + `","reason":"write outside workspace"}}\n'
        IFS= read -r a1
        echo "FC_DECISION:$a1" >> "` + logFile + `"
      elif printf '%s' "$line" | grep -q "hang_turn"; then
        printf '{"id":"srv-hang","method":"item/commandExecution/requestApproval","params":{"itemId":"h1","command":"sleep 10"}}\n'
        while IFS= read -r extra; do
          echo "AFTER_HANG:$extra" >> "` + logFile + `"
          if printf '%s' "$extra" | grep -q '"method":"turn/interrupt"'; then
            printf '{"method":"turn/completed","params":{"threadId":"th-app-1","turn":{"id":"turn-1","status":"interrupted"}}}\n'
            break
          fi
        done
        continue
      fi
      printf '{"method":"item/started","params":{"threadId":"th-app-1","turnId":"turn-1","item":{"id":"i1","type":"commandExecution","command":"uname -a"}}}\n'
      printf '{"method":"item/agentMessage/delta","params":{"threadId":"th-app-1","turnId":"turn-1","itemId":"m1","delta":"hello "}}\n'
      printf '{"method":"item/agentMessage/delta","params":{"threadId":"th-app-1","turnId":"turn-1","itemId":"m1","delta":"app-server"}}\n'
      printf '{"method":"thread/tokenUsage/updated","params":{"threadId":"th-app-1","turnId":"turn-1","tokenUsage":{"last":{"inputTokens":20,"cachedInputTokens":5,"outputTokens":7}}}}\n'
      printf '{"method":"turn/completed","params":{"threadId":"th-app-1","turnId":"turn-1","turn":{"id":"turn-1","status":"completed"}}}\n'
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewCodexHarness()
	h.binary = script
	defer h.Close()

	// Turn 1: ask mode -> command approval (allow_once), permissions approval (allow_always -> scope session), requestUserInput
	res1, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "cx1", Workspace: ws, Model: "gpt-5.4", PermissionMode: config.PermissionModeAsk},
		Prompt: "cmd_approve",
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			if req.ToolName == "permissions" {
				return PermissionResponse{Decision: "allow_always"}, nil
			}
			return PermissionResponse{Decision: "allow_once"}, nil
		},
		OnQuestion: func(_ context.Context, req QuestionRequest) (QuestionResponse, error) {
			return QuestionResponse{Answers: map[string]any{"q1": "prod"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Turn 1 error: %v", err)
	}
	if res1.SessionID != "th-app-1" || res1.Text != "hello app-server" {
		t.Fatalf("unexpected Turn 1 result: %+v", res1)
	}
	if res1.Usage.InputTokens != 20 || res1.Usage.OutputTokens != 7 || res1.Usage.CacheReadTokens != 5 {
		t.Fatalf("unexpected Turn 1 usage: %+v", res1.Usage)
	}

	// Turn 2: edit mode (reuses same pooled process!) with fileChange grantRoot outside workspace -> MUST prompt user!
	var promptedOutside bool
	res2, err := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "cx1", Workspace: ws, PermissionMode: config.PermissionModeEdit},
		SessionID: res1.SessionID,
		Prompt:    "file_outside",
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			promptedOutside = true
			return PermissionResponse{Decision: "reject"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Turn 2 error: %v", err)
	}
	if !promptedOutside {
		t.Fatal("expected edit mode file_change with outside grantRoot to prompt user")
	}
	if res2.SessionID != "th-app-1" {
		t.Fatalf("unexpected Turn 2 session: %+v", res2)
	}

	// Turn 3: plan mode (still reuses same process!).
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "cx1", Workspace: ws, PermissionMode: config.PermissionModePlan},
		SessionID: res1.SessionID,
		Prompt:    "read only plan",
	}); err != nil {
		t.Fatalf("Turn 3 error: %v", err)
	}

	pidRaw, _ := os.ReadFile(pidLog)
	if lines := strings.Split(strings.TrimSpace(string(pidRaw)), "\n"); len(lines) != 1 {
		t.Fatalf("expected 1 pooled codex app-server across ask->edit->plan, got %d:\n%s", len(lines), pidRaw)
	}

	// Turn 4: interrupt during approval sends turn/interrupt.
	ctx, cancel := context.WithCancel(context.Background())
	permStarted := make(chan struct{})
	go func() {
		<-permStarted
		cancel()
	}()
	_, intErr := h.RunTurn(ctx, RunOptions{
		Bot:       config.BotConfig{ID: "cx1", Workspace: ws, PermissionMode: config.PermissionModeAsk},
		SessionID: res1.SessionID,
		Prompt:    "hang_turn",
		OnPermission: func(pCtx context.Context, _ PermissionRequest) (PermissionResponse, error) {
			close(permStarted)
			<-pCtx.Done()
			return PermissionResponse{Decision: "cancelled"}, pCtx.Err()
		},
	})
	if intErr == nil || !strings.Contains(intErr.Error(), "interrupted") {
		t.Fatalf("expected interrupted error, got %v", intErr)
	}

	logRaw, _ := os.ReadFile(logFile)
	sLog := string(logRaw)
	for _, want := range []string{
		"INITIALIZED",
		`CMD_DECISION:{"id":"srv-cmd","jsonrpc":"2.0","result":{"decision":"accept"}}`,
		`"scope":"session"`,
		`"answers":{"q1":{"answers":["prod"]}}`,
		`FC_DECISION:{"id":"srv-fc","jsonrpc":"2.0","result":{"decision":"decline"}}`,
		`"approvalPolicy":"on-request"`,
		`"sandboxPolicy":{"type":"workspaceWrite"}`,
		`"approvalPolicy":"never"`,
		`"sandboxPolicy":{"type":"readOnly"}`,
		`"method":"turn/interrupt"`,
	} {
		if !strings.Contains(sLog, want) {
			t.Fatalf("app-server log missing %q:\n%s", want, sLog)
		}
	}
}

func TestCodexExecFallbackModes(t *testing.T) {
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	script := filepath.Join(dir, "old-codex")
	body := `#!/bin/sh
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  echo "unknown subcommand" >&2
  exit 2
fi
echo "$*" >> "` + argsLog + `"
cat >/dev/null
echo '{"type":"thread.started","thread_id":"th-old"}'
echo '{"type":"item.completed","item":{"id":"m1","type":"agent_message","text":"exec ok"}}'
echo '{"type":"turn.completed"}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewCodexHarness()
	h.binary = script
	defer h.Close()

	for _, mode := range []string{
		config.PermissionModeAuto,
		config.PermissionModeEdit,
		config.PermissionModeAsk,
		config.PermissionModePlan,
	} {
		if _, err := h.RunTurn(context.Background(), RunOptions{
			Bot:    config.BotConfig{ID: "cx-old", Workspace: dir, PermissionMode: mode},
			Prompt: "hi",
		}); err != nil {
			t.Fatalf("mode %s fallback failed: %v", mode, err)
		}
	}

	raw, _ := os.ReadFile(argsLog)
	s := string(raw)
	for _, want := range []string{
		"exec --json --full-auto -",
		"exec --json --sandbox workspace-write --approve-for-me -",
		"exec --json --sandbox workspace-write -",
		"exec --json --sandbox read-only -",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("exec fallback args missing %q:\n%s", want, s)
		}
	}
}
