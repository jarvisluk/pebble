package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bot-hub/internal/config"
)

func TestScanSkillDirsFollowsSymlinksAndSharedSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ws := t.TempDir()
	realSkill := filepath.Join(t.TempDir(), "my-skill")
	if err := os.MkdirAll(realSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realSkill, "SKILL.md"), []byte("---\nname: linked-skill\ndescription: 软链技能\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sharedDir := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realSkill, filepath.Join(sharedDir, "my-skill")); err != nil {
		t.Fatal(err)
	}

	got := scanSharedSkills(ws)
	if len(got) != 1 || got[0].Name != "linked-skill" || got[0].Source != "skill" || got[0].Description != "软链技能" {
		t.Fatalf("unexpected shared symlinked skills: %+v", got)
	}
}

func TestScanAgentsDirMarkdownAndTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Explore.md"), []byte("---\nname: Explore\ndescription: 代码检索\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reviewer.toml"), []byte("name = \"grok-reviewer\"\ndescription = \"代码评审员\"\n[model]\nname = \"ignored\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.md"), []byte("---\nname: hidden\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := scanAgentsDir(dir)
	if len(got) != 2 {
		t.Fatalf("expected 2 agents, got %+v", got)
	}
	byName := map[string]string{}
	for _, a := range got {
		byName[a.Name] = a.Description
	}
	if byName["Explore"] != "代码检索" || byName["grok-reviewer"] != "代码评审员" {
		t.Fatalf("unexpected agents parsed: %+v", byName)
	}
}

func TestHarnessesListCommandsAndAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := t.TempDir()

	if err := os.MkdirAll(filepath.Join(ws, ".agents", "skills", "shared-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".agents", "skills", "shared-skill", "SKILL.md"), []byte("---\ndescription: 共享技能\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, ".claude", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".claude", "agents", "custom.md"), []byte("---\ndescription: 自定义代理\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	bot := config.BotConfig{ID: "b1", Workspace: ws}

	claude := NewClaudeHarness()
	defer claude.Close()
	cCmds, err := claude.ListCommands(ctx, bot)
	if err != nil {
		t.Fatal(err)
	}
	assertHasCommand(t, cCmds, "help")
	assertHasCommand(t, cCmds, "shared-skill")
	cAgents, err := claude.ListAgents(ctx, bot)
	if err != nil {
		t.Fatal(err)
	}
	assertHasAgent(t, cAgents, "Explore")
	assertHasAgent(t, cAgents, "custom")

	agy := NewAgyHarness()
	aCmds, err := agy.ListCommands(ctx, bot)
	if err != nil {
		t.Fatal(err)
	}
	assertHasCommand(t, aCmds, "goal")
	assertHasCommand(t, aCmds, "shared-skill")

	dsh := NewDshHarness()
	dCmds, err := dsh.ListCommands(ctx, bot)
	if err != nil {
		t.Fatal(err)
	}
	assertHasCommand(t, dCmds, "compact")
	assertHasCommand(t, dCmds, "shared-skill")
}

func assertHasCommand(t *testing.T, cmds []SlashCommand, name string) {
	t.Helper()
	for _, c := range cmds {
		if c.Name == name {
			return
		}
	}
	t.Fatalf("expected command %q in %+v", name, cmds)
}

func assertHasAgent(t *testing.T, agents []AgentInfo, name string) {
	t.Helper()
	for _, a := range agents {
		if a.Name == name {
			return
		}
	}
	t.Fatalf("expected agent %q in %+v", name, agents)
}

func TestEnsureWorkspacePebbleSetupSkillDiscoveredByHarnesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	cfg := &config.HubConfig{
		ProjectRoot: root,
		SharedDir:   filepath.Join(root, "workspaces", "_shared"),
	}

	ocBot := config.BotConfig{ID: "oc", Name: "OC", Workspace: filepath.Join(root, "workspaces", "oc"), Harness: "opencode"}
	clBot := config.BotConfig{ID: "cl", Name: "CL", Workspace: filepath.Join(root, "workspaces", "cl"), Harness: "claude"}
	if err := config.EnsureWorkspace(&ocBot, cfg, ""); err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureWorkspace(&clBot, cfg, ""); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	listers := []struct {
		name string
		h    CommandLister
		bot  config.BotConfig
	}{
		{"claude", NewClaudeHarness(), clBot},
		{"agy", NewAgyHarness(), ocBot},
		{"dsh", NewDshHarness(), ocBot},
		{"opencode", NewOpenCodeHarness(""), ocBot},
		{"pi", NewPiHarness(), ocBot},
		{"codex", NewCodexHarness(), ocBot},
		{"codebuddy", NewCodeBuddyHarness(), ocBot},
	}

	for _, tc := range listers {
		cmds, err := tc.h.ListCommands(ctx, tc.bot)
		if err != nil {
			t.Fatalf("%s ListCommands failed: %v", tc.name, err)
		}
		var found *SlashCommand
		for i := range cmds {
			if cmds[i].Name == config.PebbleSetupSkillName {
				found = &cmds[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("%s did not discover %q skill in %+v", tc.name, config.PebbleSetupSkillName, cmds)
		}
		if found.Source != "skill" || found.Description == "" {
			t.Fatalf("%s unexpected skill entry: %+v", tc.name, *found)
		}
	}

	wsCustom := filepath.Join(root, "workspaces", "custom-claude")
	if err := config.EnsureClaudeWorkspaceSkill(wsCustom); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wsCustom, ".claude", "skills", config.PebbleSetupSkillName, "SKILL.md")); err != nil {
		t.Fatalf("expected EnsureClaudeWorkspaceSkill to create SKILL.md: %v", err)
	}
}

func TestClaudeHarnessMigratesLegacyClaudeMD(t *testing.T) {
	tmp := t.TempDir()
	argLog := filepath.Join(tmp, "claude-args.log")
	fakeClaude := filepath.Join(tmp, "fake-claude.sh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"" + argLog + "\"\nIFS= read -r _line\nprintf '{\"type\":\"result\",\"session_id\":\"cl-1\",\"result\":\"ok\"}\\n'\n"
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_BIN", fakeClaude)

	h := NewClaudeHarness()
	defer h.Close()

	// 1. Legacy CLAUDE.md bridge migrates to @_shared/PEBBLE.md and passes "pebble" in --mcp-config.
	wsLegacy := filepath.Join(tmp, "ws-legacy")
	if err := os.MkdirAll(wsLegacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsLegacy, "CLAUDE.md"), []byte("@_shared/BOT_HUB.md\n@AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot:        config.BotConfig{ID: "cl1", Workspace: wsLegacy, Harness: "claude"},
		Prompt:     "hi",
		Ephemeral:  true,
		MCPCommand: []string{"/usr/local/bin/pebble", "mcp", "--bot-id", "cl1"},
		MCPEnv:     map[string]string{"PEBBLE_API_TOKEN": "tok-cl"},
	})
	if err != nil || res.Text != "ok" {
		t.Fatalf("RunTurn wsLegacy failed: res=%+v err=%v", res, err)
	}
	migrated, err := os.ReadFile(filepath.Join(wsLegacy, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(migrated) != "@_shared/PEBBLE.md\n@AGENTS.md\n" {
		t.Fatalf("expected CLAUDE.md migrated, got %q", migrated)
	}
	if _, err := os.Stat(filepath.Join(wsLegacy, ".claude", "skills", config.PebbleSetupSkillName, "SKILL.md")); err != nil {
		t.Fatalf("expected Claude pebble-setup skill ensured: %v", err)
	}
	argsRaw, err := os.ReadFile(argLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(argsRaw), `"pebble"`) || strings.Contains(string(argsRaw), `"bot-hub"`) {
		t.Fatalf("expected --mcp-config to register pebble MCP server, got:\n%s", argsRaw)
	}

	// 2. Custom CLAUDE.md must never be overwritten.
	wsCustom := filepath.Join(tmp, "ws-custom")
	if err := os.MkdirAll(wsCustom, 0o755); err != nil {
		t.Fatal(err)
	}
	const customContent = "# My Custom Instructions\nDo not overwrite.\n"
	if err := os.WriteFile(filepath.Join(wsCustom, "CLAUDE.md"), []byte(customContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "cl2", Workspace: wsCustom, Harness: "claude"},
		Prompt:    "hi",
		Ephemeral: true,
	}); err != nil {
		t.Fatal(err)
	}
	gotCustom, err := os.ReadFile(filepath.Join(wsCustom, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCustom) != customContent {
		t.Fatalf("expected custom CLAUDE.md untouched, got %q", gotCustom)
	}
}
