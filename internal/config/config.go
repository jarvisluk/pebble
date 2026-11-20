package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type ScheduleConfig struct {
	Name                 string `json:"name"`
	EveryMinutes         int    `json:"everyMinutes,omitempty"`
	DailyAt              string `json:"dailyAt,omitempty"` // "HH:MM" local time
	Prompt               string `json:"prompt"`
	NotifyTelegramChatID string `json:"notifyTelegramChatId,omitempty"`
	Enabled              *bool  `json:"enabled,omitempty"`
}

func (s ScheduleConfig) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

type TelegramBotConfig struct {
	Token          string   `json:"token"`
	AllowedUserIDs []string `json:"allowedUserIds,omitempty"`
	AllowedChatIDs []string `json:"allowedChatIds,omitempty"`
}

type BotConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Workspace   string `json:"workspace"`
	Harness     string `json:"harness,omitempty"` // default: "opencode"
	Model       string `json:"model,omitempty"`
	// ModelInherited is true when Model came from the hub default rather
	// than the bot's own config.
	ModelInherited bool               `json:"-"`
	Agent          string             `json:"agent,omitempty"`
	AutoApprove    *bool              `json:"autoApprove,omitempty"` // default: true
	TimeoutSec     int                `json:"timeoutSec,omitempty"`  // default: 300
	Env            map[string]string  `json:"env,omitempty"`
	Telegram       *TelegramBotConfig `json:"telegram,omitempty"`
	Schedules      []ScheduleConfig   `json:"schedules,omitempty"`
}

func (b BotConfig) ShouldAutoApprove() bool {
	return b.AutoApprove == nil || *b.AutoApprove
}

type HTTPConfig struct {
	Enabled       *bool  `json:"enabled,omitempty"` // default: true
	Host          string `json:"host,omitempty"`    // default: "127.0.0.1"
	Port          int    `json:"port,omitempty"`    // default: 3789
	WebhookSecret string `json:"webhookSecret,omitempty"`
	// APIToken guards the Web UI and /api/*. Required when Host is not loopback.
	APIToken string `json:"apiToken,omitempty"`
	// Tailcat exposes the same server through a Tailcat tunnel (no Tailscale account needed).
	Tailcat *TailcatConfig `json:"tailcat,omitempty"`
}

type TailcatConfig struct {
	Enabled    bool   `json:"enabled"`
	DERPMapURL string `json:"derpMapURL,omitempty"` // optional self-hosted DERP map (https)
}

func (h HTTPConfig) TailcatEnabled() bool { return h.Tailcat != nil && h.Tailcat.Enabled }

// IsLoopback reports whether the server only listens on a loopback address.
func (h HTTPConfig) IsLoopback() bool {
	switch h.Host {
	case "", "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func (h HTTPConfig) IsEnabled() bool {
	return h.Enabled == nil || *h.Enabled
}

func (h HTTPConfig) BaseURL() string {
	host := h.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := h.Port
	if port == 0 {
		port = 3789
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

type HubConfig struct {
	ProjectRoot string `json:"-"`
	ConfigPath  string `json:"-"`
	// DefaultModel applies to opencode and pi bots (they share provider/model
	// IDs); DefaultModels overrides it per harness.
	DefaultModel  string            `json:"defaultModel,omitempty"`
	DefaultModels map[string]string `json:"defaultModels,omitempty"`
	// ModelAccess picks who wires the harness CLIs to models: "magpie"
	// (default) reuses magpie's gateway and catalog; "self" leaves each
	// CLI's own provider config alone and turns the magpie integration off.
	ModelAccess       string      `json:"modelAccess,omitempty"`
	OpenCodeServerURL string      `json:"opencodeServerUrl,omitempty"`
	SharedDir         string      `json:"sharedDir,omitempty"`
	StateFile         string      `json:"stateFile,omitempty"`
	MessagesDir       string      `json:"messagesDir,omitempty"`
	HTTP              HTTPConfig  `json:"http,omitempty"`
	Bots              []BotConfig `json:"bots"`
}

var (
	envRefRe = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)
	botIDRe  = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

func ValidateBotID(id string) error {
	if !botIDRe.MatchString(id) {
		return fmt.Errorf("invalid bot id %q (use letters, digits, - or _)", id)
	}
	if id == "_shared" {
		return fmt.Errorf("bot id %q is reserved", id)
	}
	return nil
}

func expandEnv(s string) string {
	return envRefRe.ReplaceAllStringFunc(s, func(m string) string {
		key := m[2 : len(m)-1]
		return os.Getenv(key)
	})
}

func loadDotEnv(projectRoot string) {
	p := filepath.Join(projectRoot, ".env")
	f, err := os.Open(p)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
}

func ResolvePath(projectRoot, explicit string) (string, error) {
	if explicit != "" {
		if filepath.IsAbs(explicit) {
			return explicit, nil
		}
		return filepath.Join(projectRoot, explicit), nil
	}
	local := filepath.Join(projectRoot, "config", "bots.json")
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}
	example := filepath.Join(projectRoot, "config", "bots.example.json")
	if _, err := os.Stat(example); err == nil {
		return example, nil
	}
	return "", fmt.Errorf("no config file found; create %s from config/bots.example.json", local)
}

func Load(projectRoot, explicitPath string) (*HubConfig, string, error) {
	loadDotEnv(projectRoot)

	cfgPath, err := ResolvePath(projectRoot, explicitPath)
	if err != nil {
		return nil, "", err
	}

	rawBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, cfgPath, fmt.Errorf("read config %s: %w", cfgPath, err)
	}

	var cfg HubConfig
	if err := json.Unmarshal(rawBytes, &cfg); err != nil {
		return nil, cfgPath, fmt.Errorf("parse config %s: %w", cfgPath, err)
	}

	cfg.ProjectRoot = projectRoot
	cfg.ConfigPath = cfgPath
	cfg.DefaultModel = expandEnv(cfg.DefaultModel)
	cfg.OpenCodeServerURL = expandEnv(cfg.OpenCodeServerURL)
	cfg.HTTP.WebhookSecret = expandEnv(cfg.HTTP.WebhookSecret)
	cfg.HTTP.APIToken = expandEnv(cfg.HTTP.APIToken)
	if err := ValidateModelAccess(cfg.ModelAccess); err != nil {
		return nil, cfgPath, err
	}

	if cfg.SharedDir == "" {
		cfg.SharedDir = "./workspaces/_shared"
	}
	cfg.SharedDir = absFrom(projectRoot, cfg.SharedDir)

	if cfg.StateFile == "" {
		cfg.StateFile = "./state/sessions.json"
	}
	cfg.StateFile = absFrom(projectRoot, cfg.StateFile)

	if cfg.MessagesDir == "" {
		cfg.MessagesDir = "./state/messages"
	}
	cfg.MessagesDir = absFrom(projectRoot, cfg.MessagesDir)

	if cfg.HTTP.Host == "" {
		cfg.HTTP.Host = "127.0.0.1"
	}
	if cfg.HTTP.Port == 0 {
		cfg.HTTP.Port = 3789
	}

	seen := make(map[string]struct{}, len(cfg.Bots))
	for i := range cfg.Bots {
		b := &cfg.Bots[i]
		if err := ValidateBotID(b.ID); err != nil {
			return nil, cfgPath, err
		}
		if _, dup := seen[b.ID]; dup {
			return nil, cfgPath, fmt.Errorf("duplicate bot id %q", b.ID)
		}
		seen[b.ID] = struct{}{}

		NormalizeBot(b, &cfg)
		if err := EnsureWorkspace(b, &cfg, ""); err != nil {
			return nil, cfgPath, fmt.Errorf("init workspace for bot %q: %w", b.ID, err)
		}
	}

	if len(cfg.Bots) == 0 {
		return nil, cfgPath, errors.New("config has no bots")
	}

	return &cfg, cfgPath, nil
}

func NormalizeBot(b *BotConfig, cfg *HubConfig) {
	if b.Name == "" {
		b.Name = b.ID
	}
	if b.Harness == "" {
		b.Harness = "opencode"
	}
	b.Model = expandEnv(b.Model)
	b.ModelInherited = b.Model == ""
	if b.ModelInherited {
		b.Model = cfg.DefaultModelFor(b.Harness)
	}
	if b.TimeoutSec <= 0 {
		b.TimeoutSec = 300
	}
	if b.Workspace == "" {
		b.Workspace = filepath.Join("./workspaces", b.ID)
	}
	b.Workspace = absFrom(cfg.ProjectRoot, b.Workspace)

	for k, v := range b.Env {
		b.Env[k] = expandEnv(v)
	}
	if b.Telegram != nil {
		b.Telegram.Token = expandEnv(b.Telegram.Token)
		for j := range b.Telegram.AllowedUserIDs {
			b.Telegram.AllowedUserIDs[j] = expandEnv(b.Telegram.AllowedUserIDs[j])
		}
		for j := range b.Telegram.AllowedChatIDs {
			b.Telegram.AllowedChatIDs[j] = expandEnv(b.Telegram.AllowedChatIDs[j])
		}
	}
}

// ValidateModelAccess accepts "" (= magpie), "magpie" or "self".
func ValidateModelAccess(mode string) error {
	switch mode {
	case "", "magpie", "self":
		return nil
	}
	return fmt.Errorf("modelAccess must be \"magpie\" or \"self\", got %q", mode)
}

// UseMagpie reports whether harnesses get their models through magpie.
func (c *HubConfig) UseMagpie() bool { return c.ModelAccess != "self" }

// DefaultModelFor returns the model a bot of this harness inherits; empty
// means the harness's own default.
func (c *HubConfig) DefaultModelFor(harness string) string {
	if m := c.DefaultModels[harness]; m != "" {
		return expandEnv(m)
	}
	// A magpie model works in every harness magpie wires up (claude takes
	// the bare gateway ID); any other default only fits opencode and pi.
	if strings.HasPrefix(c.DefaultModel, "magpie/") {
		if harness == "claude" {
			return strings.TrimPrefix(c.DefaultModel, "magpie/")
		}
		return c.DefaultModel
	}
	switch harness {
	case "", "opencode", "pi":
		return c.DefaultModel
	}
	return ""
}

func absFrom(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

func EnsureWorkspace(bot *BotConfig, cfg *HubConfig, customInstructions string) error {
	if err := os.MkdirAll(cfg.SharedDir, 0o755); err != nil {
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
			"## Workspace & Memory Rules",
			"- This directory is your dedicated workspace (1 Bot = 1 Workspace = 1 Session).",
			"- Read `_shared/USER.md` for global user preferences and shared context.",
			"- Persist important decisions, ongoing tasks, and facts into `memory/` so you can recover even if the session is reset.",
			"- You have `bot-hub` MCP tools (`list_bots`, `get_bot`, `create_bot`, `message_bot`) to inspect bots, create new specialized bots, and collaborate with other bots.",
			"- Keep replies concise and direct.",
			"",
		}, "\n")
		if err := os.WriteFile(agentsMD, []byte(content), 0o644); err != nil {
			return err
		}
	}

	// Write workspace-local opencode.json so the bot automatically gets bot-hub MCP tools
	// (list_bots, get_bot, create_bot, message_bot) without clobbering global Magpie MCPs.
	if err := ensureOpenCodeMCPConfig(bot, cfg); err != nil {
		return err
	}
	return nil
}

// MCPCommand is the stdio command that exposes bot-hub's tools to a bot's agent.
func MCPCommand(cfg *HubConfig, botID string) []string {
	return []string{resolveBotHubBinary(cfg.ProjectRoot), "mcp", "--hub-url", cfg.HTTP.BaseURL(), "--bot-id", botID}
}

func ensureOpenCodeMCPConfig(bot *BotConfig, cfg *HubConfig) error {
	ocPath := filepath.Join(bot.Workspace, "opencode.json")

	doc := map[string]any{
		"$schema": "https://opencode.ai/config.json",
	}
	if raw, err := os.ReadFile(ocPath); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &doc)
	}

	// opencode v2 schema: mcp.servers.<name>. Drop the pre-v2 top-level entry if present.
	mcpMap, _ := doc["mcp"].(map[string]any)
	if mcpMap == nil {
		mcpMap = map[string]any{}
	}
	delete(mcpMap, "bot-hub")
	servers, _ := mcpMap["servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	server := map[string]any{
		"type":    "local",
		"command": MCPCommand(cfg, bot.ID),
	}
	// opencode spawns MCP servers from its shared service process, so per-turn env
	// vars never reach them; the token has to live in the config itself.
	if cfg.HTTP.APIToken != "" {
		server["environment"] = map[string]string{"BOT_HUB_API_TOKEN": cfg.HTTP.APIToken}
	}
	servers["bot-hub"] = server
	mcpMap["servers"] = servers
	doc["mcp"] = mcpMap

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

func resolveBotHubBinary(projectRoot string) string {
	// Prefer compiled bin/bot-hub when running via `go run` so the path stays stable across restarts.
	candidate := filepath.Join(projectRoot, "bin", "bot-hub")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	if exe, err := os.Executable(); err == nil && !strings.Contains(exe, "go-build") {
		return exe
	}
	return candidate
}

// SaveLocal writes the current config into config/bots.json (never overwriting bots.example.json).
func SaveLocal(cfg *HubConfig) (string, error) {
	target := cfg.ConfigPath
	if target == "" || strings.HasSuffix(target, "bots.example.json") {
		target = filepath.Join(cfg.ProjectRoot, "config", "bots.json")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}

	// Re-read the on-disk config without env expansion so `${VAR}` references
	// (tokens, secrets) stay as references instead of being persisted in plaintext.
	// Existing bots keep their raw on-disk form; only newly created bots are appended.
	if raw, err := os.ReadFile(cfg.ConfigPath); err == nil {
		var onDisk HubConfig
		if err := json.Unmarshal(raw, &onDisk); err == nil {
			rawByID := make(map[string]BotConfig, len(onDisk.Bots))
			for _, b := range onDisk.Bots {
				rawByID[b.ID] = b
			}
			onDisk.Bots = onDisk.Bots[:0]
			for _, b := range cfg.Bots {
				if rb, ok := rawByID[b.ID]; ok {
					onDisk.Bots = append(onDisk.Bots, rb)
					continue
				}
				if rel, err := filepath.Rel(cfg.ProjectRoot, b.Workspace); err == nil && !strings.HasPrefix(rel, "..") {
					b.Workspace = "./" + filepath.ToSlash(rel)
				}
				if b.ModelInherited {
					b.Model = ""
				}
				onDisk.Bots = append(onDisk.Bots, b)
			}
			return writeConfig(cfg, target, onDisk)
		}
	}

	// Fallback (no readable source file): store clean relative paths where possible.
	toSave := *cfg
	toSave.Bots = make([]BotConfig, len(cfg.Bots))
	for i, b := range cfg.Bots {
		bc := b
		if rel, err := filepath.Rel(cfg.ProjectRoot, b.Workspace); err == nil && !strings.HasPrefix(rel, "..") {
			bc.Workspace = "./" + filepath.ToSlash(rel)
		}
		if bc.ModelInherited {
			bc.Model = ""
		}
		toSave.Bots[i] = bc
	}
	if rel, err := filepath.Rel(cfg.ProjectRoot, cfg.SharedDir); err == nil && !strings.HasPrefix(rel, "..") {
		toSave.SharedDir = "./" + filepath.ToSlash(rel)
	}
	if rel, err := filepath.Rel(cfg.ProjectRoot, cfg.StateFile); err == nil && !strings.HasPrefix(rel, "..") {
		toSave.StateFile = "./" + filepath.ToSlash(rel)
	}
	if rel, err := filepath.Rel(cfg.ProjectRoot, cfg.MessagesDir); err == nil && !strings.HasPrefix(rel, "..") {
		toSave.MessagesDir = "./" + filepath.ToSlash(rel)
	}

	return writeConfig(cfg, target, toSave)
}

// UpdateRaw applies fn to the on-disk config as written (`${VAR}`
// references unexpanded) and saves it to config/bots.json.
func UpdateRaw(cfg *HubConfig, fn func(raw *HubConfig) error) error {
	raw, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return err
	}
	var onDisk HubConfig
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		return err
	}
	if err := fn(&onDisk); err != nil {
		return err
	}
	target := cfg.ConfigPath
	if strings.HasSuffix(target, "bots.example.json") {
		target = filepath.Join(cfg.ProjectRoot, "config", "bots.json")
	}
	_, err = writeConfig(cfg, target, onDisk)
	return err
}

func writeConfig(cfg *HubConfig, target string, toSave HubConfig) (string, error) {
	raw, err := json.MarshalIndent(toSave, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		return "", err
	}
	cfg.ConfigPath = target
	return target, nil
}
