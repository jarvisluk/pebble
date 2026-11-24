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


func TestDshConcurrentApprovalDoesNotBlockReadLoop(t *testing.T) {
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
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-%s"}}\n' "$id" "$id"
      ;;
    *'"method":"session/prompt"'*)
      if printf '%s' "$line" | grep -q "botA_slow"; then
        sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
        printf '{"jsonrpc":"2.0","id":9001,"method":"session/request_permission","params":{"sessionId":"%s","options":[{"optionId":"ok","kind":"allow_once"}],"toolCall":{"toolCallId":"tc-a","name":"Bash","args":{"command":"echo a"}}}}\n' "$sid"
        # Save prompt request id for later completion without blocking shell loop cleanly in python/subshell? Wait for permission reply, then answer!
        printf '%s:%s\n' "$id" "$sid" > "` + tmp + `/pending_a"
      elif printf '%s' "$line" | grep -q "botB_fast"; then
        sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"B-done"}}}}\n' "$sid"
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      fi
      ;;
    *'"id":9001'*)
      meta=$(cat "` + tmp + `/pending_a")
      pid_a=${meta%%:*}
      sid_a=${meta#*:}
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"A-done"}}}}\n' "$sid_a"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$pid_a"
      ;;
  esac
done
`
	if err := os.WriteFile(fakeDsh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &DshHarness{binary: fakeDsh}
	defer h.Close()

	enteredA := make(chan struct{})
	releaseA := make(chan struct{})
	resACh := make(chan string, 1)
	errACh := make(chan error, 1)

	go func() {
		res, err := h.RunTurn(context.Background(), RunOptions{
			Bot:    config.BotConfig{ID: "bot-a", Workspace: tmp, PermissionMode: config.PermissionModeAsk},
			Prompt: "botA_slow",
			OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
				close(enteredA)
				<-releaseA
				return PermissionResponse{Decision: "allow_once"}, nil
			},
		})
		if res != nil {
			resACh <- res.Text
		} else {
			resACh <- ""
		}
		errACh <- err
	}()

	<-enteredA

	// While bot A is blocked waiting for human approval on shared dsh process, bot B MUST finish quickly!
	ctxB, cancelB := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelB()
	resB, errB := h.RunTurn(ctxB, RunOptions{
		Bot:    config.BotConfig{ID: "bot-b", Workspace: tmp, PermissionMode: config.PermissionModeAuto},
		Prompt: "botB_fast",
	})
	if errB != nil || resB.Text != "B-done" {
		t.Fatalf("bot B blocked or failed while bot A awaited approval: res=%+v err=%v", resB, errB)
	}

	close(releaseA)
	if errA := <-errACh; errA != nil {
		t.Fatalf("bot A failed: %v", errA)
	}
	if got := <-resACh; got != "A-done" {
		t.Fatalf("bot A text = %q, want A-done", got)
	}
}
