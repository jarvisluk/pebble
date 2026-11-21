package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLocalKeepsEnvRefs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"http":{"webhookSecret":"${T_SECRET}"},"bots":[{"id":"a","workspace":"./workspaces/a","telegram":{"token":"${T_TOKEN}","allowedUserIds":["1"]}}]}`
	if err := os.WriteFile(filepath.Join(root, "config", "bots.example.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("T_SECRET", "plain-secret")
	t.Setenv("T_TOKEN", "plain-token")

	cfg, _, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bots[0].Telegram.Token != "plain-token" {
		t.Fatalf("env not expanded in memory")
	}
	b := BotConfig{ID: "b"}
	NormalizeBot(&b, cfg)
	cfg.Bots = append(cfg.Bots, b)

	target, err := SaveLocal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(target)
	s := string(out)
	if strings.Contains(s, "plain-secret") || strings.Contains(s, "plain-token") {
		t.Fatalf("secrets persisted in plaintext:\n%s", s)
	}
	if !strings.Contains(s, "${T_TOKEN}") || !strings.Contains(s, `"id": "b"`) || !strings.Contains(s, "./workspaces/b") {
		t.Fatalf("unexpected saved config:\n%s", s)
	}
}

func TestDefaultModelFor(t *testing.T) {
	cases := []struct {
		name         string
		defaultModel string
		perHarness   map[string]string
		harness      string
		want         string
	}{
		{"empty", "", nil, "opencode", ""},
		{"magpie on opencode", "magpie/mac-mini/auto/sonnet", nil, "opencode", "magpie/mac-mini/auto/sonnet"},
		{"magpie on claude strips prefix", "magpie/mac-mini/auto/sonnet", nil, "claude", "mac-mini/auto/sonnet"},
		{"magpie on dsh", "magpie/mac-mini/auto/sonnet", nil, "dsh", "magpie/mac-mini/auto/sonnet"},
		{"provider/model on opencode", "anthropic/claude-sonnet-4-5", nil, "opencode", "anthropic/claude-sonnet-4-5"},
		{"provider/model on pi", "anthropic/claude-sonnet-4-5", nil, "pi", "anthropic/claude-sonnet-4-5"},
		{"provider/model ignored on claude", "anthropic/claude-sonnet-4-5", nil, "claude", ""},
		{"provider/model ignored on dsh", "anthropic/claude-sonnet-4-5", nil, "dsh", ""},
		{"bare alias on claude", "sonnet", nil, "claude", "sonnet"},
		{"bare alias on dsh", "sonnet", nil, "dsh", "sonnet"},
		{"bare alias on opencode", "sonnet", nil, "opencode", "sonnet"},
		{"per harness override wins", "sonnet", map[string]string{"claude": "opus"}, "claude", "opus"},
	}
	for _, tc := range cases {
		cfg := &HubConfig{DefaultModel: tc.defaultModel, DefaultModels: tc.perHarness}
		if got := cfg.DefaultModelFor(tc.harness); got != tc.want {
			t.Errorf("%s: DefaultModelFor(%q) = %q, want %q", tc.name, tc.harness, got, tc.want)
		}
	}
}

func TestAPITokenHelpers(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, "config", "bots.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"http":{"apiToken":"${TEST_BOT_HUB_TOK}"},"bots":[{"id":"a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_BOT_HUB_TOK", "resolved-secret")

	if ref := RawAPITokenEnvRef(cfgPath); ref != "${TEST_BOT_HUB_TOK}" {
		t.Fatalf("RawAPITokenEnvRef = %q", ref)
	}
	if got := ReadAPIToken(cfgPath); got != "resolved-secret" {
		t.Fatalf("ReadAPIToken = %q", got)
	}

	if err := os.WriteFile(cfgPath, []byte(`{"http":{"apiToken":"literal-tok"},"bots":[{"id":"a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if ref := RawAPITokenEnvRef(cfgPath); ref != "" {
		t.Fatalf("expected empty env ref for literal token, got %q", ref)
	}
	if got := ReadAPIToken(cfgPath); got != "literal-tok" {
		t.Fatalf("ReadAPIToken literal = %q", got)
	}
}

func TestPlatformInstructionsAndOpenCodeSync(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"bots":[{"id":"ops","name":"Ops","privateSessions":true}]}`
	if err := os.WriteFile(filepath.Join(cfgDir, "bots.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Bots[0].PrivateSessions {
		t.Fatal("expected PrivateSessions=true")
	}
	botHubMD, err := os.ReadFile(filepath.Join(cfg.SharedDir, PlatformInstructionsFilename))
	if err != nil {
		t.Fatalf("expected _shared/BOT_HUB.md: %v", err)
	}
	if !strings.Contains(string(botHubMD), "recall_session") || !strings.Contains(string(botHubMD), "bot:<your-bot-id>") ||
		!strings.Contains(string(botHubMD), "agent-desktop") || !strings.Contains(string(botHubMD), "agent-browser") ||
		!strings.Contains(string(botHubMD), "request_takeover") || strings.Contains(string(botHubMD), "node_desktop") {
		t.Fatalf("unexpected BOT_HUB.md content:\n%s", botHubMD)
	}
	agentsMD, err := os.ReadFile(filepath.Join(cfg.Bots[0].Workspace, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(agentsMD), "list_nodes") {
		t.Fatalf("AGENTS.md should not embed platform MCP list:\n%s", agentsMD)
	}
	ocRaw, err := os.ReadFile(filepath.Join(cfg.Bots[0].Workspace, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ocRaw), `"_shared/BOT_HUB.md"`) {
		t.Fatalf("expected _shared/BOT_HUB.md in opencode.json instructions:\n%s", ocRaw)
	}
}

func TestEnsureStarterConfig(t *testing.T) {
	root := t.TempDir()
	path, created, err := EnsureStarterConfig(root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected created=true on first call")
	}
	cfg, loadedPath, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if loadedPath != path {
		t.Fatalf("loadedPath = %q, want %q", loadedPath, path)
	}
	if len(cfg.Bots) != 1 || cfg.Bots[0].ID != "assistant" || cfg.Bots[0].Harness != "claude" {
		t.Fatalf("unexpected starter bots: %+v", cfg.Bots)
	}

	// Idempotent when config/bots.json already exists.
	path2, created2, err := EnsureStarterConfig(root, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if created2 || path2 != path {
		t.Fatalf("second call = (%q, %v), want (%q, false)", path2, created2, path)
	}
}


