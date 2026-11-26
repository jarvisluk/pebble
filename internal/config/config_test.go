package config

import (
	"encoding/json"
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
	raw := `{"http":{"webhookSecret":"${T_SECRET}"},"bots":[{"id":"a","workspace":"./workspaces/a","env":{"T_KEY":"${T_TOKEN}"}}]}`
	if err := os.WriteFile(filepath.Join(root, "config", "bots.example.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("T_SECRET", "plain-secret")
	t.Setenv("T_TOKEN", "plain-token")

	cfg, _, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bots[0].Env["T_KEY"] != "plain-token" {
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

func TestLegacyMagpieFieldsIgnoredOnLoad(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"modelAccess":"magpie","magpieWebUrl":"https://magpie.example.com","bots":[{"id":"a"}]}`
	cfgPath := filepath.Join(cfgDir, "bots.json")
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root, cfgPath)
	if err != nil {
		t.Fatalf("expected legacy magpie fields to load cleanly, got error: %v", err)
	}
	if len(cfg.Bots) != 1 || cfg.Bots[0].ID != "a" {
		t.Fatalf("unexpected bots: %+v", cfg.Bots)
	}
}

func TestNormalizeBotDefaultHarness(t *testing.T) {
	root := t.TempDir()
	cfg := &HubConfig{ProjectRoot: root}
	b1 := BotConfig{ID: "a"}
	NormalizeBot(&b1, cfg)
	if b1.Harness != "opencode" {
		t.Fatalf("expected fallback harness opencode, got %q", b1.Harness)
	}
	cfg.DefaultHarness = "agy"
	b2 := BotConfig{ID: "b"}
	NormalizeBot(&b2, cfg)
	if b2.Harness != "agy" {
		t.Fatalf("expected configured default harness agy, got %q", b2.Harness)
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
	pebbleMD, err := os.ReadFile(filepath.Join(cfg.SharedDir, PlatformInstructionsFilename))
	if err != nil {
		t.Fatalf("expected _shared/PEBBLE.md: %v", err)
	}
	if !strings.Contains(string(pebbleMD), "recall_session") || !strings.Contains(string(pebbleMD), "bot:<your-bot-id>") ||
		!strings.Contains(string(pebbleMD), "agent-desktop") || !strings.Contains(string(pebbleMD), "agent-browser") ||
		!strings.Contains(string(pebbleMD), "request_takeover") || !strings.Contains(string(pebbleMD), "pebble bot") ||
		!strings.Contains(string(pebbleMD), "pebble settings") || !strings.Contains(string(pebbleMD), "pebble-setup") ||
		!strings.Contains(string(pebbleMD), "update") || strings.Contains(string(pebbleMD), "node_desktop") {
		t.Fatalf("unexpected PEBBLE.md content:\n%s", pebbleMD)
	}
	skillRaw, err := os.ReadFile(filepath.Join(cfg.Bots[0].Workspace, ".agents", "skills", PebbleSetupSkillName, "SKILL.md"))
	if err != nil {
		t.Fatalf("expected workspace pebble-setup SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillRaw), "name: pebble-setup") {
		t.Fatalf("unexpected skill content:\n%s", skillRaw)
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
	if !strings.Contains(string(ocRaw), `"_shared/PEBBLE.md"`) || !strings.Contains(string(ocRaw), `"pebble"`) {
		t.Fatalf("expected _shared/PEBBLE.md and pebble server in opencode.json:\n%s", ocRaw)
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

func TestScheduleConfigNormalizationAndValidation(t *testing.T) {
	t.Setenv("TEST_ROUTINE_MODEL", "openai/gpt-4o-mini")

	root := t.TempDir()
	cfg := &HubConfig{ProjectRoot: root}
	bot := BotConfig{
		ID: "assistant",
		Schedules: []ScheduleConfig{
			{
				Name:    "Morning Brief",
				DailyAt: "09:00",
				Prompt:  "summarize todo",
				Model:   "${TEST_ROUTINE_MODEL}",
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
	if bot.Schedules[0].Model != "openai/gpt-4o-mini" {
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
		{Name: "a", Cron: "not a cron", Prompt: "x"},
		{Name: "a", WebhookTrigger: true, WebhookURL: "ftp://bad.example/hook", Prompt: "x"},
		{Name: "a", EveryMinutes: 5, MainDelivery: "bogus", Prompt: "x"},
	}
	for i, s := range invalid {
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid case %d (%+v) should have failed Validate", i, s)
		}
	}

	validCronAndWebhook := []ScheduleConfig{
		{Name: "five", Cron: "0 9 * * 1-5", Prompt: "x"},
		{Name: "six-sec", Cron: "*/15 * * * * *", Prompt: "x"},
		{Name: "every-sec", Cron: "@every 30s", Prompt: "x"},
		{Name: "hook-only", WebhookTrigger: true, WebhookURL: "https://hooks.example.com/notify", Prompt: "x"},
	}
	for i, s := range validCronAndWebhook {
		if err := s.Validate(); err != nil {
			t.Fatalf("valid case %d (%+v) unexpectedly failed: %v", i, s, err)
		}
	}
}

func TestDesktopWorkerConfigAndHasDesktopModel(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DESK_MODEL", "anthropic/claude-haiku-4-5")
	cfgPath := filepath.Join(cfgDir, "bots.json")
	if err := os.WriteFile(cfgPath, []byte(`{"desktopHarness":"opencode","desktopModel":"${TEST_DESK_MODEL}","bots":[{"id":"a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DesktopHarness != "opencode" || cfg.DesktopModel != "anthropic/claude-haiku-4-5" {
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

func TestDesktopConfigDefaults(t *testing.T) {
	var d DesktopConfig
	if d.EffectiveMode() != "auto" {
		t.Fatalf("expected default mode auto, got %q", d.EffectiveMode())
	}
	if d.EffectiveResolution() != "1440x900" {
		t.Fatalf("expected default resolution 1440x900, got %q", d.EffectiveResolution())
	}
	if d.EffectiveIdleFreezeMinutes() != 5 || d.EffectiveIdleStopMinutes() != 30 {
		t.Fatalf("unexpected default idle timers: freeze=%d stop=%d", d.EffectiveIdleFreezeMinutes(), d.EffectiveIdleStopMinutes())
	}
	if !d.ShouldStopOnlyWhenNoWindows() {
		t.Fatal("expected ShouldStopOnlyWhenNoWindows=true by default")
	}

	disabled := false
	d = DesktopConfig{
		Mode:                  "virtual",
		Resolution:            "1920x1080",
		IdleFreezeMinutes:     -1,
		IdleStopMinutes:       10,
		StopOnlyWhenNoWindows: &disabled,
	}
	if d.EffectiveMode() != "virtual" || d.EffectiveResolution() != "1920x1080" || d.EffectiveIdleFreezeMinutes() != 0 || d.EffectiveIdleStopMinutes() != 10 || d.ShouldStopOnlyWhenNoWindows() {
		t.Fatalf("unexpected custom DesktopConfig values: %+v", d)
	}
}

func TestBotAvatarAndColorValidation(t *testing.T) {
	for _, av := range []string{"", "blob", "pebble-1", "pebble-7", " CAT ", "sprout"} {
		got, err := NormalizeBotAvatar(av)
		if err != nil {
			t.Fatalf("NormalizeBotAvatar(%q) unexpected error: %v", av, err)
		}
		if av == " CAT " && got != "pebble-2" {
			t.Fatalf("expected pebble-2, got %q", got)
		}
		if av == "sprout" && got != "pebble-7" {
			t.Fatalf("expected pebble-7, got %q", got)
		}
	}
	if _, err := NormalizeBotAvatar("robot"); err == nil {
		t.Fatal("expected invalid avatar to fail")
	}

	for _, c := range []string{"", " #D9774B ", "#10b981"} {
		got, err := NormalizeBotColor(c)
		if err != nil {
			t.Fatalf("NormalizeBotColor(%q) unexpected error: %v", c, err)
		}
		if c == " #D9774B " && got != "#d9774b" {
			t.Fatalf("expected #d9774b, got %q", got)
		}
	}
	for _, bad := range []string{"red", "#fff", "#12345g"} {
		if _, err := NormalizeBotColor(bad); err == nil {
			t.Fatalf("expected invalid color %q to fail", bad)
		}
	}

	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	badCfg := filepath.Join(cfgDir, "bad.json")
	if err := os.WriteFile(badCfg, []byte(`{"bots":[{"id":"a","avatar":"nope"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root, badCfg); err == nil {
		t.Fatal("expected Load to fail on invalid bot avatar")
	}
}

func TestBotDesktopTargetAndBrowserModeValidation(t *testing.T) {
	validTargets := []struct {
		in   string
		want string
	}{
		{"", ""},
		{" LOCAL ", "local"},
		{"virtual:bot1", "virtual:bot1"},
		{"local:virtual:bot_2", "virtual:bot_2"},
		{"macbook-1", "macbook-1"},
		{"linux-node.local:virtual:slot_a", "linux-node.local:virtual:slot_a"},
	}
	for _, tc := range validTargets {
		got, err := NormalizeDesktopTarget(tc.in)
		if err != nil {
			t.Fatalf("NormalizeDesktopTarget(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeDesktopTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"virtual:", "virtual:bad slot", "local:virtual:bad/slot", "node:other", "node:virtual:"} {
		if _, err := NormalizeDesktopTarget(bad); err == nil {
			t.Fatalf("expected NormalizeDesktopTarget(%q) to fail", bad)
		}
	}

	for _, tc := range []struct {
		in   string
		want string
	}{
		{"", ""},
		{"AUTO", ""},
		{" isolated ", "isolated"},
		{"Attach", "attach"},
	} {
		got, err := NormalizeBrowserMode(tc.in)
		if err != nil {
			t.Fatalf("NormalizeBrowserMode(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeBrowserMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := NormalizeBrowserMode("invalid"); err == nil {
		t.Fatal("expected invalid browserMode to fail")
	}

	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "bots.json")
	raw := `{"bots":[{"id":"a","desktopTarget":"local:virtual:a","browserMode":"isolated"}]}`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bots[0].DesktopTarget != "virtual:a" || cfg.Bots[0].BrowserMode != "isolated" || cfg.Bots[0].EffectiveBrowserMode() != "isolated" {
		t.Fatalf("unexpected loaded bot config: %+v", cfg.Bots[0])
	}
}

func TestPermissionModeValidationAndCompatibility(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"", ""},
		{" AUTO ", "auto"},
		{"Edit", "edit"},
		{"ASK", "ask"},
		{"plan", "plan"},
	} {
		got, err := NormalizePermissionMode(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("NormalizePermissionMode(%q) = (%q, %v), want (%q, nil)", tc.in, got, err, tc.want)
		}
	}
	if _, err := NormalizePermissionMode("yolo"); err == nil {
		t.Fatal("expected invalid permissionMode to fail")
	}

	falseVal := false
	trueVal := true
	if got := (BotConfig{}).EffectivePermissionMode(); got != PermissionModeAuto {
		t.Fatalf("default EffectivePermissionMode = %q, want auto", got)
	}
	if got := (BotConfig{AutoApprove: &falseVal}).EffectivePermissionMode(); got != PermissionModeAsk {
		t.Fatalf("legacy autoApprove=false EffectivePermissionMode = %q, want ask", got)
	}
	if !(BotConfig{AutoApprove: &trueVal}).ShouldAutoApprove() {
		t.Fatal("expected autoApprove=true ShouldAutoApprove() == true")
	}
	if (BotConfig{AutoApprove: &falseVal, PermissionMode: PermissionModePlan}).EffectivePermissionMode() != PermissionModePlan {
		t.Fatal("explicit permissionMode must override legacy autoApprove")
	}
	if (BotConfig{PermissionMode: PermissionModeEdit}).ShouldAutoApprove() {
		t.Fatal("edit mode must not return ShouldAutoApprove() == true")
	}

	if _, err := NormalizeRoutinePermissionMode("ask"); err == nil {
		t.Fatal("routine permissionMode should reject ask")
	}
	if got, err := NormalizeRoutinePermissionMode("PLAN"); err != nil || got != PermissionModePlan {
		t.Fatalf("NormalizeRoutinePermissionMode(PLAN) = (%q, %v)", got, err)
	}

	rootPerm := t.TempDir()
	cfgDirPerm := filepath.Join(rootPerm, "config")
	if err := os.MkdirAll(cfgDirPerm, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPathPerm := filepath.Join(cfgDirPerm, "bots.json")
	rawPerm := `{"defaultPermissionMode":"edit","bots":[{"id":"a"},{"id":"b","autoApprove":false},{"id":"c","permissionMode":"plan","schedules":[{"name":"s1","everyMinutes":10,"prompt":"check","permissionMode":"auto"}]}]}`
	if err := os.WriteFile(cfgPathPerm, []byte(rawPerm), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPerm, _, err := Load(rootPerm, cfgPathPerm)
	if err != nil {
		t.Fatal(err)
	}
	if cfgPerm.Bots[0].EffectivePermissionMode() != PermissionModeEdit {
		t.Fatalf("bot a should inherit defaultPermissionMode=edit, got %q", cfgPerm.Bots[0].EffectivePermissionMode())
	}
	if cfgPerm.Bots[1].EffectivePermissionMode() != PermissionModeAsk {
		t.Fatalf("bot b with legacy autoApprove=false should resolve to ask, got %q", cfgPerm.Bots[1].EffectivePermissionMode())
	}
	if cfgPerm.Bots[2].EffectivePermissionMode() != PermissionModePlan || cfgPerm.Bots[2].Schedules[0].PermissionMode != PermissionModeAuto {
		t.Fatalf("bot c config mismatch: %+v", cfgPerm.Bots[2])
	}
}

func TestWorkspaceSkillProvisioningAndValidateModelID(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "bots.json")
	raw := `{"bots":[{"id":"oc","harness":"opencode"},{"id":"cl","harness":"claude"}]}`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root, cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	// Both bots get .agents/skills/pebble-setup/SKILL.md.
	for _, b := range cfg.Bots {
		sharedSkill := filepath.Join(b.Workspace, ".agents", "skills", PebbleSetupSkillName, "SKILL.md")
		data, err := os.ReadFile(sharedSkill)
		if err != nil {
			t.Fatalf("missing shared skill for bot %s: %v", b.ID, err)
		}
		if string(data) != PebbleSetupSkillText() {
			t.Fatalf("skill mismatch for bot %s", b.ID)
		}
	}
	// Only claude bot gets .claude/skills/pebble-setup/SKILL.md on initial provisioning.
	if _, err := os.Stat(filepath.Join(cfg.Bots[1].Workspace, ".claude", "skills", PebbleSetupSkillName, "SKILL.md")); err != nil {
		t.Fatalf("expected .claude skill for claude bot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Bots[0].Workspace, ".claude", "skills", PebbleSetupSkillName, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("did not expect .claude skill for opencode bot, got err=%v", err)
	}

	skillText := PebbleSetupSkillText()
	if !strings.HasPrefix(skillText, "---\nname: pebble-setup\ndescription: ") || strings.Contains(skillText, "node_desktop") {
		t.Fatalf("invalid pebble-setup SKILL.md frontmatter or stale reference")
	}

	for _, valid := range []string{"", "sonnet", "openai/gpt-4o-mini", "anthropic/claude-sonnet-4-5"} {
		if err := ValidateModelID(valid); err != nil {
			t.Fatalf("ValidateModelID(%q) unexpected error: %v", valid, err)
		}
	}
	for _, invalid := range []string{"bad model", "bad\nmodel", "bad\tmodel", "bad\x00model", strings.Repeat("a", 257)} {
		if err := ValidateModelID(invalid); err == nil {
			t.Fatalf("expected ValidateModelID(%q) to fail", invalid)
		}
	}
}

func TestLegacyMigrationLookupEnvAndResolveBinary(t *testing.T) {
	// 1. LookupEnv prefers PEBBLE_* and falls back to BOT_HUB_*.
	t.Setenv("PEBBLE_TEST_KEY", "")
	t.Setenv("BOT_HUB_TEST_KEY", "legacy-val")
	if got := LookupEnv("PEBBLE_TEST_KEY", "BOT_HUB_TEST_KEY"); got != "legacy-val" {
		t.Fatalf("LookupEnv fallback = %q, want legacy-val", got)
	}
	t.Setenv("PEBBLE_TEST_KEY", "new-val")
	if got := LookupEnv("PEBBLE_TEST_KEY", "BOT_HUB_TEST_KEY"); got != "new-val" {
		t.Fatalf("LookupEnv primary = %q, want new-val", got)
	}

	// 2. ResolvePebbleBinary prefers bin/pebble and falls back to bin/bot-hub.
	binRoot := t.TempDir()
	binDir := filepath.Join(binRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyBin := filepath.Join(binDir, "bot-hub")
	if err := os.WriteFile(legacyBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolvePebbleBinary(binRoot); got != legacyBin {
		t.Fatalf("ResolvePebbleBinary fallback = %q, want %q", got, legacyBin)
	}
	pebbleBin := filepath.Join(binDir, "pebble")
	if err := os.WriteFile(pebbleBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolvePebbleBinary(binRoot); got != pebbleBin {
		t.Fatalf("ResolvePebbleBinary primary = %q, want %q", got, pebbleBin)
	}

	// 3. Managed legacy _shared/BOT_HUB.md and opencode.json bot-hub server migrate cleanly.
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	sharedDir := filepath.Join(root, "workspaces", "_shared")
	ws := filepath.Join(root, "workspaces", "ops")
	for _, dir := range []string{cfgDir, sharedDir, ws} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "bots.json"), []byte(`{"http":{"apiToken":"tok-mig"},"bots":[{"id":"ops","name":"Ops"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, LegacyPlatformInstructionsFilename), []byte("<!-- Managed by bot-hub; old -->\n# Bot Hub Platform Guide\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyOC := `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "bot-hub": { "type": "local" },
    "servers": {
      "bot-hub": { "type": "local", "command": ["/old/bot-hub", "mcp"], "environment": {"BOT_HUB_API_TOKEN": "tok-old"} },
      "custom": { "type": "local", "command": ["echo", "keep"] }
    }
  },
  "instructions": ["_shared/BOT_HUB.md", "custom.md"]
}`
	if err := os.WriteFile(filepath.Join(ws, "opencode.json"), []byte(legacyOC), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sharedDir, PlatformInstructionsFilename)); err != nil {
		t.Fatalf("expected _shared/PEBBLE.md created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sharedDir, LegacyPlatformInstructionsFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected managed legacy _shared/BOT_HUB.md cleaned up, got err=%v", err)
	}
	ocRaw, err := os.ReadFile(filepath.Join(ws, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ocDoc struct {
		MCP struct {
			Servers map[string]struct {
				Environment map[string]string `json:"environment"`
			} `json:"servers"`
		} `json:"mcp"`
		Instructions []string `json:"instructions"`
	}
	if err := json.Unmarshal(ocRaw, &ocDoc); err != nil {
		t.Fatal(err)
	}
	if ocDoc.MCP.Servers["bot-hub"].Environment != nil {
		t.Fatalf("expected legacy bot-hub server removed from opencode.json:\n%s", ocRaw)
	}
	if _, hasLegacy := ocDoc.MCP.Servers["bot-hub"]; hasLegacy {
		t.Fatalf("expected legacy bot-hub key deleted from opencode.json:\n%s", ocRaw)
	}
	if ocDoc.MCP.Servers["pebble"].Environment["PEBBLE_API_TOKEN"] != "tok-mig" {
		t.Fatalf("expected pebble server with PEBBLE_API_TOKEN in opencode.json:\n%s", ocRaw)
	}
	if _, hasCustom := ocDoc.MCP.Servers["custom"]; !hasCustom {
		t.Fatalf("expected custom server preserved in opencode.json:\n%s", ocRaw)
	}
	instrJoined := strings.Join(ocDoc.Instructions, ",")
	if strings.Contains(instrJoined, "_shared/BOT_HUB.md") || !strings.Contains(instrJoined, "_shared/PEBBLE.md") || !strings.Contains(instrJoined, "custom.md") {
		t.Fatalf("unexpected migrated instructions: %v", ocDoc.Instructions)
	}
}

