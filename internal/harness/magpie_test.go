package harness

import (
	"strings"
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
