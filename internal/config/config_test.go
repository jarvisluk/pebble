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
	if !strings.Contains(string(botHubMD), "recall_session") || !strings.Contains(string(botHubMD), "bot:<your-bot-id>") {
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

func TestScheduleConfigNormalizationAndValidation(t *testing.T) {
	t.Setenv("TEST_TG_CHAT", "123456789")
	t.Setenv("TEST_ROUTINE_MODEL", "magpie/mac-mini/auto/flash")

	root := t.TempDir()
	cfg := &HubConfig{ProjectRoot: root}
	bot := BotConfig{
		ID: "assistant",
		Schedules: []ScheduleConfig{
			{
				Name:                 "Morning Brief",
				DailyAt:              "09:00",
				Prompt:               "summarize todo",
				Model:                "${TEST_ROUTINE_MODEL}",
				NotifyTelegramChatID: "${TEST_TG_CHAT}",
			},
			{
				Name:         "晨间简报",
				EveryMinutes: 30,
				Prompt:       "check inbox",
				MainDelivery: "notify",
			},
			{
				Name:         "晚间简报",
				EveryMinutes: 60,
				Prompt:       "evening check",
				MainDelivery: "none",
			},
		},
	}
	NormalizeBot(&bot, cfg)

	if bot.Schedules[0].ID != "morning-brief" {
		t.Fatalf("expected slugified ID morning-brief, got %q", bot.Schedules[0].ID)
	}
	if bot.Schedules[0].NotifyTelegramChatID != "123456789" {
		t.Fatalf("expected expanded NotifyTelegramChatID 123456789, got %q", bot.Schedules[0].NotifyTelegramChatID)
	}
	if bot.Schedules[0].Model != "magpie/mac-mini/auto/flash" {
		t.Fatalf("expected expanded Model, got %q", bot.Schedules[0].Model)
	}
	if bot.Schedules[0].MainDelivery != RoutineDeliveryReceipt {
		t.Fatalf("expected default MainDelivery receipt, got %q", bot.Schedules[0].MainDelivery)
	}
	if bot.Schedules[1].ID != "routine" || bot.Schedules[2].ID != "routine-2" {
		t.Fatalf("expected deduplicated fallback IDs routine and routine-2, got %q and %q", bot.Schedules[1].ID, bot.Schedules[2].ID)
	}
	for i, s := range bot.Schedules {
		if err := s.Validate(); err != nil {
			t.Fatalf("schedule %d unexpectedly invalid: %v", i, err)
		}
	}

	invalid := []ScheduleConfig{
		{Name: "", EveryMinutes: 10, Prompt: "x"},
		{Name: "a", EveryMinutes: 10, Prompt: ""},
		{ID: "bad id!", Name: "a", EveryMinutes: 10, Prompt: "x"},
		{Name: "a", Prompt: "x"},
		{Name: "a", DailyAt: "25:00", Prompt: "x"},
		{Name: "a", EveryMinutes: 5, MainDelivery: "bogus", Prompt: "x"},
	}
	for i, s := range invalid {
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid case %d (%+v) should have failed Validate", i, s)
		}
	}
}

func TestDesktopWorkerConfigAndHasDesktopModel(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DESK_MODEL", "magpie/mac-mini/auto/flash")
	cfgPath := filepath.Join(cfgDir, "bots.json")
	if err := os.WriteFile(cfgPath, []byte(`{"desktopHarness":"opencode","desktopModel":"${TEST_DESK_MODEL}","bots":[{"id":"a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DesktopHarness != "opencode" || cfg.DesktopModel != "magpie/mac-mini/auto/flash" {
		t.Fatalf("unexpected desktop worker config: harness=%q model=%q", cfg.DesktopHarness, cfg.DesktopModel)
	}
	if !HasDesktopModel(cfgPath) {
		t.Fatal("expected HasDesktopModel=true")
	}

	emptyPath := filepath.Join(cfgDir, "empty.json")
	if err := os.WriteFile(emptyPath, []byte(`{"bots":[{"id":"a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if HasDesktopModel(emptyPath) {
		t.Fatal("expected HasDesktopModel=false when unset")
	}
}

