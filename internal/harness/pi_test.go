package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
