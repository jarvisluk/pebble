package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/config"
)

func TestWritePiMCPConfigPreservesExistingServers(t *testing.T) {
	ws := t.TempDir()
	piDir := filepath.Join(ws, ".pi")
	if err := os.MkdirAll(piDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(piDir, "mcp.json")
	if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{"keep":{"command":"echo","args":["hi"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &PiHarness{}
	err := h.writePiMCPConfig(ws, RunOptions{
		MCPCommand: []string{"/usr/local/bin/bot-hub", "mcp", "--bot-id", "pi-bot"},
		MCPEnv:     map[string]string{"BOT_HUB_API_TOKEN": "tok-pi"},
	})
	if err != nil {
		t.Fatalf("writePiMCPConfig failed: %v", err)
	}

	info, err := os.Stat(mcpPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions on .pi/mcp.json, got %v (err=%v)", info.Mode().Perm(), err)
	}
	raw, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.MCPServers["keep"] == nil || doc.MCPServers["bot-hub"] == nil {
		t.Fatalf("expected both keep and bot-hub in .pi/mcp.json: %s", raw)
	}
}

func TestPiExtensionUIRequestQuestionFlow(t *testing.T) {
	dir := t.TempDir()
	outLog := filepath.Join(dir, "stdin.log")
	script := filepath.Join(dir, "fake-pi.sh")
	sh := `#!/bin/sh
while IFS= read -r line; do
  printf '%s\n' "$line" >> "` + outLog + `"
  case "$line" in
    *'"type":"prompt"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
      printf '{"type":"response","id":"%s","command":"prompt","success":true}\n' "$id"
      printf '{"type":"extension_ui_request","id":"ui-1","method":"select","title":"Pick drink","options":["tea","coffee"]}\n'
      ;;
    *'"type":"extension_ui_response"'*)
      if printf '%s' "$line" | grep -q '"id":"ui-1"'; then
        printf '{"type":"extension_ui_request","id":"ui-2","method":"confirm","title":"Sure?","message":"Confirm tea?"}\n'
      elif printf '%s' "$line" | grep -q '"confirmed":true'; then
        printf '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"confirmed tea"}]}}\n'
        printf '{"type":"agent_settled"}\n'
      fi
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PI_BIN", script)
	h := NewPiHarness()
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var seen []QuestionRequest
	res, err := h.RunTurn(ctx, RunOptions{
		Bot:       config.BotConfig{ID: "pi-bot", Workspace: dir},
		Ephemeral: true,
		Prompt:    "hi",
		OnQuestion: func(_ context.Context, req QuestionRequest) (QuestionResponse, error) {
			seen = append(seen, req)
			if len(seen) == 1 {
				return QuestionResponse{Answers: map[string]any{"value": "tea"}}, nil
			}
			return QuestionResponse{Answers: map[string]any{"value": true}}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}
	if res.Text != "confirmed tea" {
		t.Fatalf("unexpected text: %q", res.Text)
	}
	if len(seen) != 2 || seen[0].Fields[0].Type != "string" || len(seen[0].Fields[0].Options) != 2 || seen[1].Fields[0].Type != "boolean" {
		t.Fatalf("unexpected questions seen: %+v", seen)
	}
	logged, _ := os.ReadFile(outLog)
	if !strings.Contains(string(logged), `"value":"tea"`) || !strings.Contains(string(logged), `"confirmed":true`) {
		t.Fatalf("expected extension_ui_response in stdin log, got:\n%s", logged)
	}
}

func TestPiGetCommandsAndCompact(t *testing.T) {
	dir := t.TempDir()
	outLog := filepath.Join(dir, "stdin.log")
	script := filepath.Join(dir, "fake-pi.sh")
	sh := fmt.Sprintf(`#!/bin/sh
LOG=%q
while IFS= read -r line; do
  printf '%%s\n' "$line" >> "$LOG"
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *'"type":"get_commands"'*)
      printf '{"type":"response","id":"%%s","command":"get_commands","success":true,"data":{"commands":[{"name":"hello","description":"say hi","source":"extension"}]}}\n' "$id"
      ;;
    *'"type":"compact"'*)
      printf '{"type":"response","id":"%%s","command":"compact","success":true,"data":{"summary":"compacted summary"}}\n' "$id"
      ;;
  esac
done
`, outLog)
	if err := os.WriteFile(script, []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &PiHarness{binary: script, commands: make(map[string][]SlashCommand)}
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot := config.BotConfig{ID: "pi-cmd", Workspace: dir}

	res, err := h.RunTurn(ctx, RunOptions{
		Bot:    bot,
		Prompt: "/compact keep decisions",
	})
	if err != nil {
		t.Fatalf("compact RunTurn failed: %v", err)
	}
	if res.Text != "compacted summary" {
		t.Fatalf("unexpected compact text: %q", res.Text)
	}

	cmds, err := h.ListCommands(ctx, bot)
	if err != nil {
		t.Fatal(err)
	}
	var foundHello bool
	for _, c := range cmds {
		if c.Name == "hello" {
			foundHello = true
		}
	}
	if !foundHello {
		t.Fatalf("expected cached get_commands result, got %+v", cmds)
	}
}
