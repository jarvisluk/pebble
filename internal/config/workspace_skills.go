package config

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

const PebbleSetupSkillName = "pebble-setup"

//go:embed skills/pebble-setup/SKILL.md
var pebbleSetupSkillMD string

// PebbleSetupSkillText returns the managed SKILL.md content describing Pebble's
// interfaces, model switching, global settings, and safety guardrails.
func PebbleSetupSkillText() string {
	return pebbleSetupSkillMD
}

func writeSkillFileIfChanged(target, content string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if cur, err := os.ReadFile(target); err == nil && string(cur) == content {
		return nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// EnsureClaudeWorkspaceSkill writes .claude/skills/pebble-setup/SKILL.md into
// workspace so Claude Code discovers the skill locally inside the bot workspace.
func EnsureClaudeWorkspaceSkill(workspace string) error {
	ws := strings.TrimSpace(workspace)
	if ws == "" {
		return nil
	}
	target := filepath.Join(ws, ".claude", "skills", PebbleSetupSkillName, "SKILL.md")
	return writeSkillFileIfChanged(target, PebbleSetupSkillText())
}

// EnsureWorkspaceSkill writes the managed pebble-setup skill into the bot's
// cross-tool .agents/skills directory (and .claude/skills when running Claude Code).
func EnsureWorkspaceSkill(bot *BotConfig) error {
	if bot == nil || strings.TrimSpace(bot.Workspace) == "" {
		return nil
	}
	content := PebbleSetupSkillText()
	sharedTarget := filepath.Join(bot.Workspace, ".agents", "skills", PebbleSetupSkillName, "SKILL.md")
	if err := writeSkillFileIfChanged(sharedTarget, content); err != nil {
		return err
	}
	if bot.Harness == "claude" {
		if err := EnsureClaudeWorkspaceSkill(bot.Workspace); err != nil {
			return err
		}
	}
	return nil
}
