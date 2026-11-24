package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseAgentStatusLine(t *testing.T) {
	catalog := []string{"mac-mini/claude/claude-fable-5-2", "deepseek/deepseek-chat"}

	cases := []struct {
		name       string
		prefix     string
		raw        string
		wantConn   bool
		wantModel  string
		wantNative string
	}{
		{
			name:      "opencode connected",
			prefix:    "magpie/",
			raw:       "  OpenCode  magpie/claude/claude-opus-5-5  ·  small —  ~/.config/opencode/opencode.json",
			wantConn:  true,
			wantModel: "magpie/claude/claude-opus-5-5",
		},
		{
			name:       "opencode native",
			prefix:     "magpie/",
			raw:        "  OpenCode  openrouter/deepseek/v4  ·  small —  ~/.config/opencode/opencode.json",
			wantConn:   false,
			wantNative: "openrouter/deepseek/v4",
		},
		{
			name:     "opencode unset dash",
			prefix:   "magpie/",
			raw:      "  OpenCode  —  ·  small —",
			wantConn: false,
		},
		{
			name:      "claude connected via catalog",
			prefix:    "",
			raw:       "  Claude Code  mac-mini/claude/claude-fable-5-2  ·  effort —  ~/.claude/settings.json",
			wantConn:  true,
			wantModel: "mac-mini/claude/claude-fable-5-2",
		},
		{
			name:      "claude connected via group",
			prefix:    "",
			raw:       "  Claude Code  group/auto  ·  effort —",
			wantConn:  true,
			wantModel: "group/auto",
		},
		{
			name:       "claude native sonnet",
			prefix:     "",
			raw:        "  Claude Code  sonnet  ·  effort —  ~/.claude/settings.json",
			wantConn:   false,
			wantNative: "sonnet",
		},
		{
			name:     "agy native status placeholder",
			prefix:   "magpie/",
			raw:      "  Antigravity CLI  status  ·  effort —  ~/.gemini/antigravity-cli/settings.json",
			wantConn: false,
		},
		{
			name:       "grok native model",
			prefix:     "magpie/",
			raw:        "  Grok Build  grok-4.7  ·  effort high  ~/.grok/config.toml",
			wantConn:   false,
			wantNative: "grok-4.7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, model, native := parseAgentStatusLine(tc.prefix, tc.raw, catalog)
			if conn != tc.wantConn || model != tc.wantModel || native != tc.wantNative {
				t.Fatalf("got (%v, %q, %q), want (%v, %q, %q)", conn, model, native, tc.wantConn, tc.wantModel, tc.wantNative)
			}
		})
	}
}

func TestParseProvidersOutputAndRedact(t *testing.T) {
	raw := `
  Mac-mini     mac-mini  100.64.0.10:3425    ● sk-1234567890abcdef          168 of 167 models · fetched 8m ago  ← Claude Code, Pi
  Claude Code  claude    api.anthropic.com   ○ switched off               4 of 18 models · fetched Nov 13     magpie provider on claude  ← OpenCode
  Gemini CLI is signed in but not offered: You removed it from magpie.
`
	list := parseProvidersOutput(raw)
	if len(list) != 2 {
		t.Fatalf("got %d providers, want 2: %+v", len(list), list)
	}
	if list[0].ID != "mac-mini" || !list[0].Enabled || strings.Contains(list[0].Status, "1234567890abcd") {
		t.Fatalf("unexpected first provider (or key leaked): %+v", list[0])
	}
	if list[0].UsedBy != "Claude Code, Pi" || !strings.Contains(list[0].ModelsInfo, "168 of 167 models") {
		t.Fatalf("unexpected metadata on provider 0: %+v", list[0])
	}
	if list[1].ID != "claude" || list[1].Enabled || list[1].Endpoint != "api.anthropic.com" {
		t.Fatalf("unexpected second provider: %+v", list[1])
	}
}

func TestParsePresetsOutput(t *testing.T) {
	raw := `
  vendors
  Anthropic   anthropic   magpie provider add anthropic <key>
  DeepSeek    deepseek    magpie provider add deepseek <key>
`
	list := parsePresetsOutput(raw)
	if len(list) != 2 || list[0].ID != "anthropic" || list[1].ID != "deepseek" {
		t.Fatalf("unexpected presets: %+v", list)
	}
}

func TestParseMemorySearchOutputAndTagFilter(t *testing.T) {
	rawText := `[m_ref001] reference, everywhere, #vps #ops, 2026-11-13T19:50+08:00: 示例服务器节点编号为 100001。
[m_abc123] fact, project:dir:/Users/me/repo, #bot:ops #deploy, 2026-11-21T14:00+08:00: 生产端口为 8443
多行补充说明
(looked in: everywhere, project:dir:/Users/me/repo)`

	items := parseMemorySearchOutput(nil, rawText)
	if len(items) != 2 {
		t.Fatalf("got %d memory items, want 2: %+v", len(items), items)
	}
	if items[0].ID != "m_ref001" || items[0].Kind != "reference" || items[0].Scope != "everywhere" || items[0].UpdatedAt != "2026-11-13T19:50+08:00" {
		t.Fatalf("unexpected item 0: %+v", items[0])
	}
	if !hasMemoryTag(items[0].Tags, "vps") || !hasMemoryTag(items[0].Tags, "ops") {
		t.Fatalf("unexpected tags on item 0: %+v", items[0].Tags)
	}
	if items[1].ID != "m_abc123" || !hasMemoryTag(items[1].Tags, "bot:ops") || !strings.Contains(items[1].Text, "多行补充说明") {
		t.Fatalf("unexpected item 1: %+v", items[1])
	}

	empty := parseMemorySearchOutput(nil, `Nothing remembered matches "foo". (looked in: everywhere)`)
	if len(empty) != 0 {
		t.Fatalf("expected 0 items for empty match, got %+v", empty)
	}

	tags := normalizeMemoryTags([]string{"#deploy", "bot:ops", "deploy"}, "ops")
	if len(tags) != 2 || tags[0] != "deploy" || tags[1] != "bot:ops" {
		t.Fatalf("unexpected normalized tags: %+v", tags)
	}
}


func TestMagpieCacheAndInvalidation(t *testing.T) {
	fakeBin := filepath.Join(t.TempDir(), "magpie")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_BIN", fakeBin)
	InvalidateMagpieCache()
	t.Cleanup(InvalidateMagpieCache)

	origOut, origComb := runMagpieOutput, runMagpieCombined
	t.Cleanup(func() {
		runMagpieOutput = origOut
		runMagpieCombined = origComb
	})

	var (
		mu     sync.Mutex
		counts = make(map[string]int)
	)
	runMagpieOutput = func(_ context.Context, args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		mu.Lock()
		counts[key]++
		mu.Unlock()
		switch key {
		case "healthcheck":
			return nil, nil
		case "providers":
			return []byte("  OpenAI  oa  api.openai.com  ● ok  10 models\n"), nil
		case "presets":
			return []byte("  OpenAI  openai  magpie provider add openai <key>\n"), nil
		case "models":
			return []byte("  oa/gpt-5  OpenAI\n"), nil
		case "opencode":
			return []byte("  OpenCode  magpie/oa/gpt-5  ·  fast\n"), nil
		case "claude":
			return []byte("  Claude Code  oa/gpt-5  ·  fast\n"), nil
		default:
			return nil, nil
		}
	}
	runMagpieCombined = func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	ctx := t.Context()
	WarmMagpieCache(ctx, []string{"opencode", "claude"})

	if !MagpieGatewayHealthy(ctx) {
		t.Fatal("expected healthy gateway")
	}
	provs := MagpieListProviders(ctx)
	if len(provs) != 1 || provs[0].ID != "oa" {
		t.Fatalf("unexpected providers: %+v", provs)
	}
	provs[0].Name = "mutated"
	if got := MagpieListProviders(ctx); got[0].Name == "mutated" {
		t.Fatal("expected defensive copy of cached providers")
	}
	_ = MagpiePresets(ctx)
	if st := GetMagpieStatus(ctx, "opencode"); !st.Connected || st.Model != "magpie/oa/gpt-5" {
		t.Fatalf("unexpected opencode status: %+v", st)
	}
	if st := GetMagpieStatus(ctx, "claude"); !st.Connected || st.Model != "oa/gpt-5" {
		t.Fatalf("unexpected claude status: %+v", st)
	}

	mu.Lock()
	for _, k := range []string{"healthcheck", "providers", "presets", "opencode", "claude"} {
		if counts[k] != 1 {
			mu.Unlock()
			t.Fatalf("expected 1 CLI call for %q, got %d", k, counts[k])
		}
	}
	mu.Unlock()

	if _, err := MagpieProviderAction(ctx, MagpieProviderReq{Action: "test", ID: "oa"}); err != nil {
		t.Fatal(err)
	}
	_ = MagpieListProviders(ctx)
	mu.Lock()
	if counts["providers"] != 1 {
		mu.Unlock()
		t.Fatalf("expected test action to preserve providers cache, got %d", counts["providers"])
	}
	mu.Unlock()

	if err := MagpieConnect(ctx, "opencode", "magpie/oa/gpt-5"); err != nil {
		t.Fatal(err)
	}
	_ = MagpieListProviders(ctx)
	_ = GetMagpieStatus(ctx, "opencode")
	mu.Lock()
	if counts["providers"] != 2 || counts["opencode"] != 2 {
		mu.Unlock()
		t.Fatalf("expected invalidation after MagpieConnect, counts=%v", counts)
	}
	mu.Unlock()

	if _, err := MagpieProviderAction(ctx, MagpieProviderReq{Action: "on", ID: "oa"}); err != nil {
		t.Fatal(err)
	}
	_ = MagpieListProviders(ctx)
	mu.Lock()
	if counts["providers"] != 3 {
		mu.Unlock()
		t.Fatalf("expected invalidation after mutating provider action, counts=%v", counts)
	}
	mu.Unlock()
}

func TestMagpieCatalogDoesNotCacheEmptyOutput(t *testing.T) {
	fakeBin := filepath.Join(t.TempDir(), "magpie")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_BIN", fakeBin)
	InvalidateMagpieCache()
	t.Cleanup(InvalidateMagpieCache)

	origOut := runMagpieOutput
	t.Cleanup(func() { runMagpieOutput = origOut })

	calls := 0
	runMagpieOutput = func(_ context.Context, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "models" {
			calls++
			if calls == 1 {
				return []byte("  Acer\n"), nil
			}
			return []byte("  acer/gpt-5  OpenAI\n"), nil
		}
		return nil, nil
	}

	ctx := t.Context()
	if got := MagpieModels(ctx, "opencode"); len(got) != 0 {
		t.Fatalf("expected empty models on first transient empty output, got %v", got)
	}
	got := MagpieModels(ctx, "opencode")
	if len(got) != 1 || got[0] != "magpie/acer/gpt-5" {
		t.Fatalf("expected second call to retry and populate catalog, got %v (calls=%d)", got, calls)
	}
	if calls != 2 {
		t.Fatalf("expected 2 CLI calls when first returned empty, got %d", calls)
	}
	_ = MagpieModels(ctx, "opencode")
	if calls != 2 {
		t.Fatalf("expected non-empty catalog to be cached on third call, got calls=%d", calls)
	}
}

func TestParseMagpieDataDirs(t *testing.T) {
	raw := "files: /Users/test/.config/magpie\ncache: /Users/test/.cache/magpie\n"
	files, cache := parseMagpieDataDirs(raw)
	if files != "/Users/test/.config/magpie" || cache != "/Users/test/.cache/magpie" {
		t.Fatalf("unexpected parsed dirs: files=%q cache=%q", files, cache)
	}
}

func TestDetectMagpieMemorySemanticStatus(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		tmp := t.TempDir()
		st := detectMagpieMemorySemanticStatus(filepath.Join(tmp, "files"), filepath.Join(tmp, "cache"))
		if st.Enabled || st.ModelReady || st.IndexReady || st.Active || st.Model != "" {
			t.Fatalf("expected all false for missing config, got %+v", st)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		tmp := t.TempDir()
		files := filepath.Join(tmp, "files")
		cache := filepath.Join(tmp, "cache")
		if err := os.MkdirAll(files, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(files, "memory-semantic.json"), []byte(`{"on":false}`), 0o644); err != nil {
			t.Fatal(err)
		}
		st := detectMagpieMemorySemanticStatus(files, cache)
		if st.Enabled || st.Active {
			t.Fatalf("expected disabled status, got %+v", st)
		}
	})

	t.Run("enabled without model", func(t *testing.T) {
		tmp := t.TempDir()
		files := filepath.Join(tmp, "files")
		cache := filepath.Join(tmp, "cache")
		if err := os.MkdirAll(files, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(files, "memory-semantic.json"), []byte(`{"on":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		st := detectMagpieMemorySemanticStatus(files, cache)
		if !st.Enabled || st.ModelReady || st.IndexReady || st.Active || st.Model != magpieSemanticModelName {
			t.Fatalf("unexpected enabled-without-model status: %+v", st)
		}
	})

	t.Run("enabled and ready", func(t *testing.T) {
		tmp := t.TempDir()
		files := filepath.Join(tmp, "files")
		cache := filepath.Join(tmp, "cache")
		revDir := filepath.Join(cache, magpieSemanticModelName, "abc123")
		if err := os.MkdirAll(files, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(revDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(files, "memory-semantic.json"), []byte(`{"on":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(revDir, "model_int8.onnx"), []byte("onnx"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(revDir, "vectors.bin"), []byte("vec"), 0o644); err != nil {
			t.Fatal(err)
		}
		st := detectMagpieMemorySemanticStatus(files, cache)
		if !st.Enabled || !st.ModelReady || !st.IndexReady || !st.Active || st.Model != magpieSemanticModelName {
			t.Fatalf("expected active ready status, got %+v", st)
		}
	})
}
