package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/config"
)

func TestDshDrainsFinalTextChunksToOnText(t *testing.T) {
	tmp := t.TempDir()
	fakeDsh := filepath.Join(tmp, "dsh")
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-dsh-1"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-dsh-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hel"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-dsh-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"lo"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(fakeDsh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &DshHarness{binary: fakeDsh}
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var streamed strings.Builder
	res, err := h.RunTurn(ctx, RunOptions{
		Bot:    config.BotConfig{ID: "dsh-bot", Workspace: tmp},
		Prompt: "hi",
		OnText: func(chunk string) { streamed.WriteString(chunk) },
	})
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}
	if res.Text != "Hello" {
		t.Fatalf("expected res.Text=Hello, got %q", res.Text)
	}
	if streamed.String() != "Hello" {
		t.Fatalf("expected all chunks streamed to OnText, got %q", streamed.String())
	}
}
