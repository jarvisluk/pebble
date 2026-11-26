package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	MCPServerName                      = "pebble"
	LegacyMCPServerName                = "bot-hub"
	PlatformInstructionsFilename       = "PEBBLE.md"
	LegacyPlatformInstructionsFilename = "BOT_HUB.md"
)

// LookupEnv returns the trimmed value of primary when set, falling back to fallback.
func LookupEnv(primary, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(primary)); v != "" {
		return v
	}
	if fallback != "" {
		return strings.TrimSpace(os.Getenv(fallback))
	}
	return ""
}

// PlatformInstructionsText returns the concise hub-wide usage guide written to
// _shared/PEBBLE.md on startup and workspace sync.
func PlatformInstructionsText() string {
	return strings.Join([]string{
		"<!-- Managed by Pebble; overwritten on startup. Put personal global preferences in _shared/USER.md. -->",
		"# Pebble Platform Guide",
		"",
		"- **Workspace Model**: `1 Bot = 1 Workspace = 1 Active Session`. Keep replies concise and direct.",
		"- **User Output Convention**: Your ordinary text output is internal working trace and is hidden from the user. Always call `send_message(text?, images?, files?)` (or `pebble bot send`) to deliver replies, progress updates, local images, or files to the user. If a background task or routine needs no user notification, simply end without calling `send_message`. When replying to another bot via `message_bot`, reply directly in ordinary text (no `send_message` needed).",
		"- **Shared Context & Memory**:",
		"  - Read `_shared/USER.md` for hub-wide user preferences.",
		"  - When Magpie Memory tools (`memory_search` / `memory_save`) are available, store cross-project preferences with `scope=\"global\"` and this bot's durable notes with `scope=\"project\"` and tag `bot:<your-bot-id>` (and pass that tag when searching so sibling workspaces inside a git repo never mix). Otherwise store durable notes in `memory/`.",
		"- **MCP Core Tools (`pebble` MCP, 8 runtime primitives)**:",
		"  - `send_message(text?, images?, files?)`: send visible text, local images, and/or files to the user.",
		"  - `message_bot(botId, message)`: collaborate with another bot.",
		"  - `recall_session(...)`: read sliced messages from `previous` / `active` / `<sessionId>` (keep slices small; fetch single oversized messages by `messageId`). If `pebble session list` returns a `magpieSession`, pass it to `magpie-sessions__read_session` for raw tool transcripts.",
		"  - `desktop_task(goal, ...)` & `request_takeover(reason, ...)`: delegate multi-step GUI tasks to the visible desktop worker, or pause for human QR/2FA/CAPTCHA/payment takeover (omit `nodeId` to use this bot's bound screen `desktopTarget` or the active desktop target).",
		"  - `node_exec(command, ...)` & `node_file(op, path, ...)`: run shell commands or read/write/list files on connected remote nodes.",
		"  - `open_stage(kind, path?, title?, subtitle?, url?, target?, autoOpen?)`: submit a Stage side tab (`file`, `files`, `desktop`, `browser`, `artifact`, `mcp-app`) and post a clickable jump card in chat (`autoOpen` defaults to `false`).",
		"- **Full `pebble` CLI (auto-reads current bot context from env or workspace config)**:",
		"  - **Bots & Self-Edit**: `pebble bot list | get [--id <id>] | create --id <id> --name <name> [--harness opencode|codebuddy|qoder|codex|claude|grok|trae|pi|dsh|agy] [--model <m>] [--force] | update [--id <id>] [--model <m> | --inherit-model] [--harness <h>] [--name ...] [--description ...] [--instructions-file AGENTS.md] [--desktop-target <t>] [--browser-mode auto|isolated|attach] [--dry-run] [--force] | delete --id <id> [--confirm-self] | message <id> <msg> | send [--text \"...\"] [--image <path>] [--file <path>]`.",
		"  - **Settings & Models**: `pebble settings get [--fresh] | models [--harness <h>] | set [--default-harness <h>] [--default-model <m> | --clear-default-model] [--harness-model <h>=<m>] [--clear-harness-model <h>] [--desktop-harness <h> | --clear-desktop-harness] [--desktop-model <m> | --clear-desktop-model] [--shared-user-md \"...\" | --shared-user-md-file <path>] [--dry-run] [--force]`.",
		"  - **Sessions**: `pebble session list [--bot <id|*>] [--query ...] | recall [--id previous|active|<sid>] | rotate [--handoff \"...\"]` (rotate regularly on harnesses without native `billion-context` compression like `agy`, `dsh`).",
		"  - **Routines**: `pebble routine list | upsert --name <n> --prompt <p> [--every-minutes N | --daily-at HH:MM] [--main-delivery receipt|notify|none] | delete --id <r> | run --id <r>` (isolated ephemeral turns with `AGENTS.md` + `memory/`).",
		"  - **Nodes & Desktop**: `pebble node list | exec --command <cmd> | file --op read|write|list --path <p>` and `pebble desktop targets | task --goal <g> | takeover --reason <r>`.",
		"- **Pebble Setup Skill**: Read the workspace `pebble-setup` skill (`.agents/skills/pebble-setup/SKILL.md`) before switching models, changing global settings, or operating Pebble interfaces so pre-flight checks, `--dry-run`, and safety guardrails are followed.",
		"- **Local Desktop & Browser CLI (run locally or via `node_exec`)**:",
		"  - Desktop GUI (`agent-desktop` CLI): operate apps via accessibility-tree `snapshot` + `@ref` instead of blind coordinates.",
		"  - Browser (`agent-browser` CLI):",
		"    1. `AGENT_BROWSER_SESSION` is automatically pre-set per bot (`bot-<id>`), and `AGENT_BROWSER_PROFILE` is pre-set when the bot's browser mode is `isolated`. Always keep `--session <bot-id>` when invoking inside shared-server harnesses (e.g., `opencode`).",
		"    2. In `auto`/`attach` mode, prefer `agent-browser --auto-connect --pin-tab` to attach to the running Chromium browser in an isolated tab (reuses logins without stealing mouse/focus). In `isolated` mode (or if attach fails), use `--headed` when human takeover is needed or default headless mode for background/read-only work.",
		"",
	}, "\n")
}

// EnsurePlatformInstructions writes the managed _shared/PEBBLE.md file atomically
// and cleans up a previously managed legacy _shared/BOT_HUB.md if present.
func EnsurePlatformInstructions(sharedDir string) (string, error) {
	if strings.TrimSpace(sharedDir) == "" {
		return "", errors.New("sharedDir is empty")
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(sharedDir, PlatformInstructionsFilename)
	want := PlatformInstructionsText()
	changed := true
	if cur, err := os.ReadFile(target); err == nil && string(cur) == want {
		changed = false
	}
	if changed {
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, []byte(want), 0o644); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
	}
	legacyTarget := filepath.Join(sharedDir, LegacyPlatformInstructionsFilename)
	if raw, err := os.ReadFile(legacyTarget); err == nil {
		trimmed := strings.TrimSpace(string(raw))
		if strings.HasPrefix(trimmed, "<!-- Managed by bot-hub;") || strings.HasPrefix(trimmed, "# Bot Hub Platform Guide") {
			_ = os.Remove(legacyTarget)
		}
	}
	return target, nil
}

func EnsureWorkspace(bot *BotConfig, cfg *HubConfig, customInstructions string) error {
	if _, err := EnsurePlatformInstructions(cfg.SharedDir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(bot.Workspace, "memory"), 0o755); err != nil {
		return err
	}

	sharedLink := filepath.Join(bot.Workspace, "_shared")
	if _, err := os.Lstat(sharedLink); os.IsNotExist(err) {
		_ = os.Symlink(cfg.SharedDir, sharedLink)
	}

	agentsMD := filepath.Join(bot.Workspace, "AGENTS.md")
	if customInstructions != "" {
		if err := os.WriteFile(agentsMD, []byte(strings.TrimSpace(customInstructions)+"\n"), 0o644); err != nil {
			return err
		}
	} else if _, err := os.Stat(agentsMD); os.IsNotExist(err) {
		desc := bot.Description
		if desc == "" {
			desc = "You are a persistent personal AI bot."
		}
		content := strings.Join([]string{
			"# " + bot.Name,
			"",
			desc,
			"",
			"## Role & Focus",
			"- Stay focused on your assigned responsibilities in this workspace.",
			"- Follow platform conventions in `_shared/" + PlatformInstructionsFilename + "` and shared user preferences in `_shared/USER.md`.",
			"",
		}, "\n")
		if err := os.WriteFile(agentsMD, []byte(content), 0o644); err != nil {
			return err
		}
	}

	// Write workspace-local opencode.json so the bot automatically gets pebble MCP tools
	// and _shared/PEBBLE.md instructions without touching global configs.
	if err := ensureOpenCodeMCPConfig(bot, cfg); err != nil {
		return err
	}
	if err := EnsureWorkspaceSkill(bot); err != nil {
		return err
	}
	return nil
}

// MCPCommand is the stdio command that exposes Pebble's tools to a bot's agent.
func MCPCommand(cfg *HubConfig, botID string) []string {
	cmd := []string{ResolvePebbleBinary(cfg.ProjectRoot), "mcp", "--hub-url", cfg.HTTP.BaseURL(), "--bot-id", botID}
	if cfg.ConfigPath != "" {
		cmd = append(cmd, "--config", cfg.ConfigPath)
	}
	return cmd
}

// HasDesktopModel reports whether configPath configures a non-empty desktopModel.
func HasDesktopModel(configPath string) bool {
	if configPath == "" {
		return false
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	var doc struct {
		DesktopModel string `json:"desktopModel"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	if ContainsEnvRef(doc.DesktopModel) {
		loadDotEnv(filepath.Dir(filepath.Dir(configPath)))
	}
	return strings.TrimSpace(expandEnv(doc.DesktopModel)) != ""
}

func ensureOpenCodeMCPConfig(bot *BotConfig, cfg *HubConfig) error {
	ocPath := filepath.Join(bot.Workspace, "opencode.json")

	doc := map[string]any{
		"$schema": "https://opencode.ai/config.json",
	}
	if raw, err := os.ReadFile(ocPath); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &doc)
	}

	// opencode v2 schema: mcp.servers.<name>. Drop pre-v2 top-level and legacy server keys.
	mcpMap, _ := doc["mcp"].(map[string]any)
	if mcpMap == nil {
		mcpMap = map[string]any{}
	}
	delete(mcpMap, LegacyMCPServerName)
	delete(mcpMap, MCPServerName)
	servers, _ := mcpMap["servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	delete(servers, LegacyMCPServerName)
	server := map[string]any{
		"type":    "local",
		"command": MCPCommand(cfg, bot.ID),
	}
	// opencode spawns MCP servers from its shared service process, so per-turn env
	// vars never reach them; the token has to live in the config itself.
	if cfg.HTTP.APIToken != "" {
		server["environment"] = map[string]string{"PEBBLE_API_TOKEN": cfg.HTTP.APIToken}
	}
	servers[MCPServerName] = server
	mcpMap["servers"] = servers
	doc["mcp"] = mcpMap

	const platInstr = "_shared/" + PlatformInstructionsFilename
	const legacyPlatInstr = "_shared/" + LegacyPlatformInstructionsFilename
	var nextInstr []string
	seenInstr := map[string]bool{}
	if rawList, ok := doc["instructions"].([]any); ok {
		for _, item := range rawList {
			s, ok := item.(string)
			if !ok {
				continue
			}
			trimmed := strings.TrimSpace(s)
			if trimmed == "" || trimmed == legacyPlatInstr || seenInstr[s] {
				continue
			}
			seenInstr[s] = true
			nextInstr = append(nextInstr, s)
		}
	}
	if !seenInstr[platInstr] {
		nextInstr = append(nextInstr, platInstr)
	}
	doc["instructions"] = nextInstr

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.WriteFile(ocPath, out, 0o600); err != nil {
		return err
	}
	return os.Chmod(ocPath, 0o600) // WriteFile keeps the mode of an existing file
}

// ResolvePebbleBinary returns the preferred path to the pebble CLI binary.
func ResolvePebbleBinary(projectRoot string) string {
	// Prefer compiled bin/pebble (or legacy bin/bot-hub) when running via `go run`.
	for _, name := range []string{"pebble", "bot-hub"} {
		candidate := filepath.Join(projectRoot, "bin", name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if exe, err := os.Executable(); err == nil && !strings.Contains(exe, "go-build") {
		return exe
	}
	return filepath.Join(projectRoot, "bin", "pebble")
}

// ResolveBotHubBinary is kept as a compatibility alias for ResolvePebbleBinary.
func ResolveBotHubBinary(projectRoot string) string {
	return ResolvePebbleBinary(projectRoot)
}
