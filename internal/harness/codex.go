package harness

import (
	"bot-hub/internal/config"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CodexHarness runs OpenAI Codex CLI. It prefers `codex app-server --stdio` for
// interactive tool approvals and per-turn permission/sandbox policies, and falls
// back cleanly to `codex exec --json` on older binaries.
type CodexHarness struct {
	binary string

	pool               procPool[*codexAppProc]
	probeMu            sync.Mutex
	appServerSupported map[string]bool
}

func NewCodexHarness() *CodexHarness {
	return &CodexHarness{
		binary:             envOr("CODEX_BIN", "codex"),
		appServerSupported: make(map[string]bool),
	}
}

func (h *CodexHarness) effectiveBinary() string {
	bin := strings.TrimSpace(h.binary)
	if bin == "" {
		bin = envOr("CODEX_BIN", "codex")
	}
	if bin == "codex" {
		if resolved, ok := findBinary("codex"); ok {
			return resolved
		}
	}
	return bin
}

func (h *CodexHarness) Kind() string { return "codex" }

func (h *CodexHarness) Label() string { return "Codex" }

func (h *CodexHarness) PermissionCapabilities() PermissionCapabilities {
	return PermissionCapabilities{
		Modes: []string{
			config.PermissionModeAuto,
			config.PermissionModeEdit,
			config.PermissionModeAsk,
			config.PermissionModePlan,
		},
		InteractiveApproval: true,
		SupportsAllowAlways: true,
	}
}

func (h *CodexHarness) Models(context.Context) ([]string, error) {
	return []string{"gpt-5.4", "gpt-5.3-codex", "gpt-5.2-codex", "o3"}, nil
}

func (h *CodexHarness) Close() {
	h.pool.close()
}

// Reload terminates cached codex app-server processes and clears capability
// probes so updated configuration applies on the next turn.
func (h *CodexHarness) Reload() {
	h.pool.clear()
	h.probeMu.Lock()
	h.appServerSupported = make(map[string]bool)
	h.probeMu.Unlock()
}

func (h *CodexHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "Compact current session context", Source: "harness"},
		{Name: "new", Description: "Start a new session", Source: "harness"},
		{Name: "clear", Description: "Clear session history", Source: "harness"},
		{Name: "review", Description: "Review current code changes", Source: "harness"},
		{Name: "plan", Description: "Enter plan mode", Source: "harness"},
		{Name: "model", Description: "View or switch model", Source: "harness"},
		{Name: "diff", Description: "View workspace diff", Source: "harness"},
		{Name: "mention", Description: "Mention files or directories", Source: "harness"},
		{Name: "mcp", Description: "Show MCP server status", Source: "harness"},
		{Name: "init", Description: "Initialize project AGENTS.md", Source: "harness"},
		{Name: "status", Description: "View current session status", Source: "harness"},
		{Name: "help", Description: "Show available commands", Source: "harness"},
	}
	var local []SlashCommand
	if bot.Workspace != "" {
		local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, ".codex", "prompts"), "harness")...)
		local = append(local, scanSkillDirs(filepath.Join(bot.Workspace, ".codex", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanMarkdownCommands(filepath.Join(home, ".codex", "prompts"), "harness")...)
		local = append(local, scanSkillDirs(filepath.Join(home, ".codex", "skills"))...)
	}
	local = append(local, scanSharedSkills(bot.Workspace)...)
	return mergeSlashCommands(builtins, local), nil
}

func (h *CodexHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".codex", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".codex", "agents"))...)
	}
	return mergeAgents(local), nil
}

func (h *CodexHarness) supportsAppServer(bin string) bool {
	h.probeMu.Lock()
	if h.appServerSupported != nil {
		if ok, cached := h.appServerSupported[bin]; cached {
			h.probeMu.Unlock()
			return ok
		}
	}
	h.probeMu.Unlock()

	pctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(pctx, bin, "app-server", "--help").CombinedOutput()
	text := string(out)
	supported := err == nil && (strings.Contains(text, "--stdio") || strings.Contains(text, "Usage: codex app-server"))

	h.setAppServerSupported(bin, supported)
	return supported
}

func (h *CodexHarness) setAppServerSupported(bin string, supported bool) {
	h.probeMu.Lock()
	if h.appServerSupported == nil {
		h.appServerSupported = make(map[string]bool)
	}
	h.appServerSupported[bin] = supported
	h.probeMu.Unlock()
}

func codexPolicies(mode string) (approvalPolicy, sandboxMode, sandboxPolicyType string) {
	switch mode {
	case config.PermissionModeEdit:
		return "on-request", "workspace-write", "workspaceWrite"
	case config.PermissionModeAsk:
		return "untrusted", "workspace-write", "workspaceWrite"
	case config.PermissionModePlan:
		return "never", "read-only", "readOnly"
	default:
		return "never", "danger-full-access", "dangerFullAccess"
	}
}
