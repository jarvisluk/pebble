package harness

import (
	"bot-hub/internal/config"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var errLegacyStreamJSON = errors.New("agy emitted legacy stream-json instead of acp")

// AgyHarness runs Antigravity CLI (`agy`). It prefers `agy acp` (Agent Client
// Protocol over stdio) for full per-turn permission modes and interactive tool
// approvals, and falls back to legacy `--output-format stream-json` on older
// binaries that lack ACP support.
type AgyHarness struct {
	binary string

	pool    procPool[*agyProc]
	acpPool procPool[*agyACPProc]

	modelsMu sync.Mutex
	models   []string

	acpProbeMu   sync.Mutex
	acpSupported map[string]bool

	cmdMu       sync.Mutex
	botCommands map[string][]SlashCommand
}

type agyProc struct {
	*lineProc
	model          string
	sessionID      string
	viaMagpie      bool
	permissionMode string
	prevInput      int
	prevOutput     int
	prevCacheRead  int
}

type agyEvent struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
	StepUpdate     *struct {
		ConversationID string `json:"conversation_id"`
		StepIndex      int    `json:"step_index"`
		State          string `json:"state"`
		StepType       string `json:"step_type"`
		ToolName       string `json:"tool_name"`
		TextDelta      string `json:"text_delta"`
	} `json:"step_update"`
	Result *struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error"`
		Usage          *struct {
			InputTokens     int `json:"input_tokens"`
			OutputTokens    int `json:"output_tokens"`
			CacheReadTokens int `json:"cache_read_tokens"`
		} `json:"usage"`
	} `json:"result"`
}

func NewAgyHarness() *AgyHarness {
	return &AgyHarness{
		binary:       resolveAgyBin(),
		acpSupported: make(map[string]bool),
		botCommands:  make(map[string][]SlashCommand),
	}
}

func resolveAgyBin() string {
	if v := strings.TrimSpace(os.Getenv("AGY_BIN")); v != "" {
		return v
	}
	if p, ok := findBinary("agy"); ok {
		return p
	}
	return "agy"
}

func (h *AgyHarness) effectiveBinary() string {
	if bin, ok := h.resolveBinary(); ok {
		return bin
	}
	if strings.TrimSpace(h.binary) != "" {
		return h.binary
	}
	return resolveAgyBin()
}

func (h *AgyHarness) resolveBinary() (string, bool) {
	return resolveBinaryFor("AGY_BIN", "agy", h.binary)
}

func (h *AgyHarness) Kind() string { return "agy" }

func (h *AgyHarness) Label() string { return "Antigravity CLI" }

func (h *AgyHarness) PermissionCapabilities() PermissionCapabilities {
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

func (h *AgyHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "Compact current session context", Source: "harness"},
		{Name: "plan", Description: "Switch to plan mode", Source: "harness"},
		{Name: "clear", Description: "Clear session context", Source: "harness"},
		{Name: "goal", Description: "Start long-running goal execution", Source: "harness"},
		{Name: "schedule", Description: "Create scheduled task or timer", Source: "harness"},
		{Name: "browser", Description: "Run browser interaction task", Source: "harness"},
		{Name: "grill-me", Description: "Align on plan via Q&A interview", Source: "harness"},
		{Name: "teamwork", Description: "Start multi-agent collaboration", Source: "harness"},
		{Name: "learn", Description: "Save lessons to configuration", Source: "harness"},
		{Name: "boost", Description: "Enable deep thinking and verification", Source: "harness"},
		{Name: "plugin", Description: "Manage or create plugins", Source: "harness"},
		{Name: "automation", Description: "Configure background automation", Source: "harness"},
		{Name: "help", Description: "Show available commands", Source: "harness"},
	}

	h.cmdMu.Lock()
	cached := append([]SlashCommand(nil), h.botCommands[bot.ID]...)
	h.cmdMu.Unlock()

	var local []SlashCommand
	if bot.Workspace != "" {
		local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, ".agents", "workflows"), "workflow")...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanSkillDirs(filepath.Join(home, ".gemini", "config", "skills"))...)
		local = append(local, scanSkillDirs(filepath.Join(home, ".gemini", "antigravity", "builtin", "skills"))...)
	}
	local = append(local, scanSharedSkills(bot.Workspace)...)
	return mergeSlashCommands(cached, builtins, local), nil
}

func (h *AgyHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".agents", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".gemini", "config", "agents"))...)
	}
	return mergeAgents(local), nil
}

func (h *AgyHarness) captureAvailableCommands(botID string, raw []struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Input       *struct {
		Hint string `json:"hint"`
	} `json:"input"`
}) {
	if botID == "" {
		return
	}
	list := make([]SlashCommand, 0, len(raw))
	for _, c := range raw {
		hint := ""
		if c.Input != nil {
			hint = c.Input.Hint
		}
		list = append(list, SlashCommand{
			Name:         c.Name,
			Description:  c.Description,
			ArgumentHint: hint,
			Source:       "harness",
		})
	}
	h.cmdMu.Lock()
	if h.botCommands == nil {
		h.botCommands = make(map[string][]SlashCommand)
	}
	h.botCommands[botID] = list
	h.cmdMu.Unlock()
}

// Models lists model slugs from `agy models` (cached on success).
func (h *AgyHarness) Models(ctx context.Context) ([]string, error) {
	h.modelsMu.Lock()
	defer h.modelsMu.Unlock()
	if len(h.models) > 0 {
		return append([]string(nil), h.models...), nil
	}
	bin, ok := h.resolveBinary()
	if !ok {
		return nil, nil
	}
	fctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(fctx, bin, "models")
	cmd.Env = augmentedEnv(nil)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var list []string
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.LastIndexByte(line, '\r'); i >= 0 {
			line = line[i+1:]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Fetching ") {
			continue
		}
		f := strings.Fields(trimmed)
		if len(f) == 0 || strings.Contains(f[0], ":") {
			continue
		}
		list = append(list, f[0])
	}
	if len(list) > 0 {
		h.models = list
	}
	return append([]string(nil), h.models...), nil
}

func (h *AgyHarness) Close() {
	h.pool.close()
	h.acpPool.close()
}

// Reload terminates cached agy processes and clears the cached model and
// protocol capability state so updated provider or Magpie settings apply cleanly.
func (h *AgyHarness) Reload() {
	h.pool.clear()
	h.acpPool.clear()
	h.modelsMu.Lock()
	h.models = nil
	h.modelsMu.Unlock()
	h.acpProbeMu.Lock()
	h.acpSupported = make(map[string]bool)
	h.acpProbeMu.Unlock()
}

func (h *AgyHarness) viaMagpie(ctx context.Context, model string) bool {
	if !magpieInstalled() {
		return false
	}
	st := GetMagpieStatus(ctx, "agy")
	return st.Connected && (model == "" || strings.HasPrefix(model, "magpie/"))
}

func (h *AgyHarness) supportsACP(bin string) bool {
	h.acpProbeMu.Lock()
	if h.acpSupported != nil {
		if ok, cached := h.acpSupported[bin]; cached {
			h.acpProbeMu.Unlock()
			return ok
		}
	}
	h.acpProbeMu.Unlock()

	pctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(pctx, bin, "acp", "--help").CombinedOutput()
	text := string(out)
	supported := err == nil && (strings.Contains(text, "Agent Client Protocol") || strings.Contains(text, "agy acp"))

	h.setACPSupported(bin, supported)
	return supported
}

func (h *AgyHarness) setACPSupported(bin string, supported bool) {
	h.acpProbeMu.Lock()
	if h.acpSupported == nil {
		h.acpSupported = make(map[string]bool)
	}
	h.acpSupported[bin] = supported
	h.acpProbeMu.Unlock()
}

// writeAgyWorkspaceFiles configures workspace-local .agents/mcp_config.json and
// .agents/rules/pebble.md for Antigravity CLI without touching global configs.
func writeAgyWorkspaceFiles(workspace string, opts RunOptions) error {
	agentsDir := filepath.Join(workspace, ".agents")
	if err := upsertMCPConfig(filepath.Join(agentsDir, "mcp_config.json"), opts); err != nil {
		return err
	}

	if opts.PlatformInstructionsPath != "" {
		raw, err := os.ReadFile(opts.PlatformInstructionsPath)
		if err == nil && len(raw) > 0 {
			rulesDir := filepath.Join(agentsDir, "rules")
			if err := os.MkdirAll(rulesDir, 0o755); err != nil {
				return err
			}
			const header = "---\ntrigger: always_on\ndescription: \"Pebble workspace, MCP, and session management guide\"\n---\n\n"
			content := header + strings.TrimSpace(string(raw)) + "\n"
			rulePath := filepath.Join(rulesDir, "pebble.md")
			if cur, err := os.ReadFile(rulePath); err != nil || string(cur) != content {
				if err := os.WriteFile(rulePath, []byte(content), 0o644); err != nil {
					return err
				}
			}
			_ = os.Remove(filepath.Join(rulesDir, "bot-hub.md"))
		}
	}
	return nil
}

func (h *AgyHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withTurnTimeout(ctx, bot.TimeoutSec, opts)
	defer cancel()

	bin := h.effectiveBinary()
	if h.supportsACP(bin) {
		res, fallback, err := h.runACPTurn(ctx, bin, opts, started)
		if !fallback {
			return res, err
		}
	}
	return h.runStreamTurn(ctx, bin, opts, started)
}
