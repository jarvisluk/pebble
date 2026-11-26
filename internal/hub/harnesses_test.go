package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type permCapHarness struct {
	kind string
	caps harness.PermissionCapabilities
	fn   func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error)
}

func (p *permCapHarness) Kind() string { return p.kind }

func (p *permCapHarness) Label() string { return p.kind }

func (p *permCapHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (p *permCapHarness) Close() {}

func (p *permCapHarness) PermissionCapabilities() harness.PermissionCapabilities { return p.caps }

func (p *permCapHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return p.fn(ctx, opts)
}

type modelCatalogHarness struct {
	kind   string
	models []string
}

func (m *modelCatalogHarness) Kind() string { return m.kind }

func (m *modelCatalogHarness) Label() string { return m.kind }

func (m *modelCatalogHarness) Models(context.Context) ([]string, error) {
	return append([]string(nil), m.models...), nil
}

func (m *modelCatalogHarness) Close() {}

func (m *modelCatalogHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "s1", Text: "ok"}, nil
}

func TestSetDefaultModelsBroadcastsPreviewAndValidation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MAGPIE_BIN", filepath.Join(tmp, "missing-magpie"))
	harness.InvalidateMagpieCache()
	t.Cleanup(harness.InvalidateMagpieCache)

	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := harness.NewRegistry()
	reg.Register(&modelCatalogHarness{kind: "opencode", models: []string{"m1", "m2"}})
	reg.Register(&modelCatalogHarness{kind: "claude", models: []string{"sonnet", "opus"}})
	reg.Register(&modelCatalogHarness{kind: "codex", models: nil})

	cfg := &config.HubConfig{
		ProjectRoot:    tmp,
		SharedDir:      filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:      filepath.Join(tmp, "sessions.json"),
		DefaultHarness: "opencode",
		DefaultModel:   "m1",
		Bots: []config.BotConfig{
			{ID: "a", Name: "A", Workspace: filepath.Join(tmp, "workspaces", "a"), Harness: "opencode", Model: "m1", ModelInherited: true},
			{ID: "b", Name: "B", Workspace: filepath.Join(tmp, "workspaces", "b"), Harness: "claude", Model: "", ModelInherited: true},
			{ID: "c", Name: "C", Workspace: filepath.Join(tmp, "workspaces", "c"), Harness: "opencode", Model: "m1", ModelInherited: false},
		},
	}
	h := New(cfg, store, nil, reg)
	defer h.Close()

	// PreviewDefaultModelsImpact includes only inheriting bots whose resolved model changes.
	affected := h.PreviewDefaultModelsImpact("m2", map[string]string{"claude": "sonnet"})
	if len(affected) != 2 || affected[0] != "a" || affected[1] != "b" {
		t.Fatalf("expected affected=[a b], got %v", affected)
	}

	// SetDefaultModels broadcasts bot_status for inheriting bots whose effective model changed.
	subID, evCh := h.Subscribe()
	defer h.Unsubscribe(subID)
	if err := h.SetDefaultModels("m2", map[string]string{"claude": "sonnet"}); err != nil {
		t.Fatalf("SetDefaultModels failed: %v", err)
	}
	gotStatus := map[string]string{}
	deadline := time.After(2 * time.Second)
	for len(gotStatus) < 2 {
		select {
		case ev := <-evCh:
			if ev.Type == "bot_status" {
				if st, ok := ev.Payload.(BotStatus); ok {
					gotStatus[st.ID] = st.Model
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for bot_status events, got %+v", gotStatus)
		}
	}
	if gotStatus["a"] != "m2" || gotStatus["b"] != "sonnet" {
		t.Fatalf("unexpected bot_status models: %+v", gotStatus)
	}
	if botC, ok := h.GetBot("c"); !ok || botC.Model != "m1" || botC.ModelInherited {
		t.Fatalf("expected pinned bot c untouched, got %+v", botC)
	}

	// ValidateHarnessReady with stubbed detector.
	origDetect, origAuth := detectHarnessFunc, detectHarnessAuthFunc
	t.Cleanup(func() {
		detectHarnessFunc = origDetect
		detectHarnessAuthFunc = origAuth
	})
	detectHarnessFunc = func(kind string) (bool, string) {
		return kind != "codex", "/usr/bin/" + kind
	}
	detectHarnessAuthFunc = func(_ context.Context, kind string) (string, string) {
		if kind == "claude" {
			return "needs_login", "需登录"
		}
		return "ready", ""
	}
	ctx := context.Background()
	if err := h.ValidateHarnessReady(ctx, "codex"); err == nil {
		t.Fatal("expected uninstalled codex to fail ValidateHarnessReady")
	}
	if err := h.ValidateHarnessReady(ctx, "claude"); err == nil {
		t.Fatal("expected needs_login claude to fail ValidateHarnessReady")
	}
	if err := h.ValidateHarnessReady(ctx, "opencode"); err != nil {
		t.Fatalf("expected opencode ready, got %v", err)
	}

	// ValidateModelForHarness & ValidateGlobalDefaultModel checks.
	if err := h.ValidateModelForHarness(ctx, "opencode", "m2"); err != nil {
		t.Fatalf("expected m2 valid for opencode: %v", err)
	}
	if err := h.ValidateModelForHarness(ctx, "opencode", "nonexistent"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected nonexistent model error with --force hint, got %v", err)
	}
	if err := h.ValidateModelForHarness(ctx, "codex", "magpie/gpt-5.4"); err == nil {
		t.Fatal("expected magpie/ prefix rejected on codex")
	}
	if err := h.ValidateGlobalDefaultModel(ctx, "claude", "anthropic/claude-sonnet"); err == nil {
		t.Fatal("expected provider/model rejected for defaultHarness=claude")
	}
	if err := h.ValidateGlobalDefaultModel(ctx, "claude", "magpie/sonnet"); err != nil {
		t.Fatalf("expected magpie/sonnet accepted for defaultHarness=claude (stripped to sonnet): %v", err)
	}

	// ValidateBotUpdate rejects switching to a harness that requires login, then succeeds once ready,
	// while still rejecting carrying incompatible explicit model across harness switch.
	claudeStr := "claude"
	emptyStr := ""
	if _, err := h.ValidateBotUpdate(ctx, "c", BotUpdate{Harness: &claudeStr, Model: &emptyStr}, true); err == nil {
		t.Fatal("expected switching c to claude to fail when claude requires login")
	}
	detectHarnessAuthFunc = func(_ context.Context, kind string) (string, string) {
		return "ready", ""
	}
	if _, err := h.ValidateBotUpdate(ctx, "c", BotUpdate{Harness: &claudeStr}, true); err == nil {
		t.Fatal("expected switching c (explicit m1) to claude without updating/inheriting model to fail strict validation")
	}
	preview, err := h.ValidateBotUpdate(ctx, "c", BotUpdate{Harness: &claudeStr, Model: &emptyStr}, true)
	if err != nil {
		t.Fatalf("expected switching c to claude with inherit-model to pass: %v", err)
	}
	if !preview.DryRun || !preview.Valid || !preview.HarnessChanged || !preview.SessionWillArchive || !preview.ModelInherited || preview.Model != "sonnet" {
		t.Fatalf("unexpected bot update preview: %+v", preview)
	}

	// UpdateBot switching harness to claude provisions .claude/skills/pebble-setup/SKILL.md.
	if _, err := h.UpdateBot("c", BotUpdate{Harness: &claudeStr, Model: &emptyStr}); err != nil {
		t.Fatalf("UpdateBot to claude failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "workspaces", "c", ".claude", "skills", config.PebbleSetupSkillName, "SKILL.md")); err != nil {
		t.Fatalf("expected .claude skill after switching harness to claude: %v", err)
	}

	// ValidateModelID enforced on CreateBot, UpdateBot, SetDefaultModels, SetDesktopWorker.
	badModel := "bad model\nline"
	if _, err := h.CreateBot(CreateBotRequest{ID: "d", Name: "D", Harness: "opencode", Model: badModel}); err == nil {
		t.Fatal("expected CreateBot to reject whitespace/newline model ID")
	}
	if _, err := h.UpdateBot("a", BotUpdate{Model: &badModel}); err == nil {
		t.Fatal("expected UpdateBot to reject whitespace/newline model ID")
	}
	if err := h.SetDefaultModels(badModel, nil); err == nil {
		t.Fatal("expected SetDefaultModels to reject whitespace/newline model ID")
	}
	if err := h.SetDesktopWorker("opencode", badModel); err == nil {
		t.Fatal("expected SetDesktopWorker to reject whitespace/newline model ID")
	}
}

func TestModelsWithSourceMagpieVsNative(t *testing.T) {
	tmp := t.TempDir()
	fakeMagpie := filepath.Join(tmp, "magpie")
	script := `#!/bin/sh
case "$1" in
  models)
    printf '  acer/auto/claude-opus-5-5   Claude Opus 5.5\n'
    printf '  acer/deepseek/v4            DeepSeek V4\n'
    ;;
  opencode)
    printf '  OpenCode  magpie/acer/auto/claude-opus-5-5  ·  small\n'
    ;;
  codex)
    printf '  Codex  codex/gpt-6-luna  ·  effort\n'
    ;;
  claude)
    printf '  Claude Code  sonnet  ·  effort\n'
    ;;
  *)
    printf '  Other  status  ·  effort\n'
    ;;
esac
`
	if err := os.WriteFile(fakeMagpie, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_BIN", fakeMagpie)
	harness.InvalidateMagpieCache()
	t.Cleanup(harness.InvalidateMagpieCache)

	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	defer store.Close()

	reg := harness.NewRegistry()
	reg.Register(harness.NewOpenCodeHarness(""))
	reg.Register(harness.NewCodexHarness())
	reg.Register(harness.NewClaudeHarness())

	h := New(&config.HubConfig{
		SharedDir:   filepath.Join(tmp, "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots:        []config.BotConfig{{ID: "b1", Name: "B1", Workspace: filepath.Join(tmp, "ws"), Harness: "codex"}},
	}, store, msgStore, reg)
	defer h.Close()

	ctx := context.Background()

	// Connected harness returns Magpie models with its prefix.
	ocModels, err := h.ModelsWithSource(ctx, "opencode", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ocModels) != 2 || ocModels[0] != "magpie/acer/auto/claude-opus-5-5" {
		t.Fatalf("unexpected connected opencode models: %#v", ocModels)
	}

	// Non-Magpie harness prepends its active NativeModel ahead of stock CLI models.
	codexModels, err := h.ModelsWithSource(ctx, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(codexModels) < 2 || codexModels[0] != "codex/gpt-6-luna" || codexModels[1] != "gpt-5.4" {
		t.Fatalf("unexpected native codex models: %#v", codexModels)
	}

	// NativeModel already present in stock list should not be duplicated.
	claudeModels, err := h.ModelsWithSource(ctx, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	sonnetCount := 0
	for _, m := range claudeModels {
		if m == "sonnet" {
			sonnetCount++
		}
	}
	if sonnetCount != 1 {
		t.Fatalf("expected sonnet exactly once in native claude models, got %#v", claudeModels)
	}

	// source=magpie returns Magpie catalog even for non-connected harness.
	codexMagpie, err := h.ModelsWithSource(ctx, "codex", "magpie")
	if err != nil {
		t.Fatal(err)
	}
	if len(codexMagpie) != 2 || codexMagpie[0] != "acer/auto/claude-opus-5-5" {
		t.Fatalf("unexpected codex source=magpie models: %#v", codexMagpie)
	}
}
