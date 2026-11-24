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
