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
	// Existing third-party server should be preserved.
	if err := os.WriteFile(filepath.Join(ws, ".agents", "mcp_config.json"), []byte(`{"mcpServers":{"keep":{"command":"echo"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	instPath := filepath.Join(tmp, "BOT_HUB.md")
	if err := os.WriteFile(instPath, []byte("# Bot Hub Guide\n- Stay focused.\n"), 0o644); err != nil {
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
	t.Setenv("MAGPIE_BIN", filepath.Join(tmp, "missing-magpie"))

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
		MCPCommand:               []string{"/usr/local/bin/bot-hub", "mcp", "--bot-id", "dev"},
		MCPEnv:                   map[string]string{"BOT_HUB_API_TOKEN": "tok-123"},
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

	// Verify .agents/mcp_config.json and .agents/rules/bot-hub.md
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
	if mcpDoc["mcpServers"]["keep"] == nil || mcpDoc["mcpServers"]["bot-hub"] == nil {
		t.Fatalf("unexpected mcp_config.json: %s", mcpRaw)
	}

	ruleRaw, err := os.ReadFile(filepath.Join(ws, ".agents", "rules", "bot-hub.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ruleRaw), "trigger: always_on") || !strings.Contains(string(ruleRaw), "Stay focused.") {
		t.Fatalf("unexpected rule file:\n%s", ruleRaw)
	}
}
