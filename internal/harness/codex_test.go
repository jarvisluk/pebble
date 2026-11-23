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
