package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	RoutineDeliveryReceipt = "receipt"
	RoutineDeliveryNotify  = "notify"
	RoutineDeliveryNone    = "none"
)

type ScheduleConfig struct {
	ID                   string `json:"id,omitempty"`
	Name                 string `json:"name"`
	EveryMinutes         int    `json:"everyMinutes,omitempty"`
	DailyAt              string `json:"dailyAt,omitempty"` // "HH:MM" local time
	Prompt               string `json:"prompt"`
	Model                string `json:"model,omitempty"`
	MainDelivery         string `json:"mainDelivery,omitempty"` // "receipt" (default) | "notify" | "none"
	NotifyTelegramChatID string `json:"notifyTelegramChatId,omitempty"`
	Enabled              *bool  `json:"enabled,omitempty"`
}

func (s ScheduleConfig) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// EffectiveID returns the routine's ID, or a normalized slug derived from Name.
func (s ScheduleConfig) EffectiveID() string {
	if id := strings.TrimSpace(s.ID); id != "" {
		return id
	}
	return slugifyRoutineID(s.Name)
}

// EffectiveMainDelivery returns the normalized main-session delivery mode ("receipt", "notify", or "none").
func (s ScheduleConfig) EffectiveMainDelivery() string {
	switch strings.ToLower(strings.TrimSpace(s.MainDelivery)) {
	case "", RoutineDeliveryReceipt:
		return RoutineDeliveryReceipt
	case RoutineDeliveryNotify:
		return RoutineDeliveryNotify
	case RoutineDeliveryNone:
		return RoutineDeliveryNone
	default:
		return strings.ToLower(strings.TrimSpace(s.MainDelivery))
	}
}

// Validate checks that the routine has a valid ID/name, trigger, prompt, and delivery mode.
func (s ScheduleConfig) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("routine name is required")
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return errors.New("routine prompt is required")
	}
	if id := strings.TrimSpace(s.ID); id != "" && !botIDRe.MatchString(id) {
		return fmt.Errorf("invalid routine id %q (use letters, digits, - or _)", id)
	}
	if s.EveryMinutes < 0 {
		return errors.New("everyMinutes must be >= 0")
	}
	daily := strings.TrimSpace(s.DailyAt)
	if daily != "" {
		if !isValidDailyAt(daily) {
			return fmt.Errorf("invalid dailyAt %q (expected HH:MM)", s.DailyAt)
		}
	}
	if s.EveryMinutes == 0 && daily == "" {
		return errors.New("either everyMinutes > 0 or dailyAt is required")
	}
	switch strings.ToLower(strings.TrimSpace(s.MainDelivery)) {
	case "", RoutineDeliveryReceipt, RoutineDeliveryNotify, RoutineDeliveryNone:
	default:
		return fmt.Errorf("invalid mainDelivery %q (expected receipt, notify, or none)", s.MainDelivery)
	}
	return nil
}

func isValidDailyAt(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	hh, err1 := strconv.Atoi(v[:2])
	mm, err2 := strconv.Atoi(v[3:])
	return err1 == nil && err2 == nil && hh >= 0 && hh <= 23 && mm >= 0 && mm <= 59
}

func slugifyRoutineID(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if isAlnum {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "routine"
	}
	return slug
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
	AutoApprove     *bool              `json:"autoApprove,omitempty"` // default: true
	TimeoutSec      int                `json:"timeoutSec,omitempty"`  // default: 300
	PrivateSessions bool               `json:"privateSessions,omitempty"`
	Env             map[string]string  `json:"env,omitempty"`
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
	DefaultModel        string            `json:"defaultModel,omitempty"`
	DefaultModels       map[string]string `json:"defaultModels,omitempty"`
	OnboardingCompleted bool              `json:"onboardingCompleted,omitempty"`
	DesktopHarness      string            `json:"desktopHarness,omitempty"`
	DesktopModel        string            `json:"desktopModel,omitempty"`
	OpenCodeServerURL   string            `json:"opencodeServerUrl,omitempty"`
	SharedDir         string            `json:"sharedDir,omitempty"`
	StateFile         string            `json:"stateFile,omitempty"`
	MessagesDir       string            `json:"messagesDir,omitempty"`
	HTTP              HTTPConfig        `json:"http,omitempty"`
	Bots              []BotConfig       `json:"bots"`
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

// ContainsEnvRef reports whether s contains a `${VAR}` environment variable reference.
func ContainsEnvRef(s string) bool {
	return envRefRe.MatchString(s)
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

// EnsureStarterConfig creates a minimal config/bots.json in projectRoot with a
// single general-purpose assistant bot if none exists yet.
func EnsureStarterConfig(projectRoot, preferredHarness string) (string, bool, error) {
	path := filepath.Join(projectRoot, "config", "bots.json")
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	hKind := strings.TrimSpace(preferredHarness)
	if hKind == "" {
		hKind = "opencode"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, err
	}
	starter := HubConfig{
		HTTP: HTTPConfig{
			Host: "127.0.0.1",
			Port: 3789,
		},
		Bots: []BotConfig{
			{
				ID:          "assistant",
				Name:        "通用助理",
				Description: "日常问答、信息整理与任务协助",
				Workspace:   "./workspaces/assistant",
				Harness:     hKind,
			},
		},
	}
	raw, err := json.MarshalIndent(starter, "", "  ")
	if err != nil {
		return "", false, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", false, err
	}
	return path, true, nil
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
	cfg.DesktopHarness = strings.TrimSpace(cfg.DesktopHarness)
	cfg.DesktopModel = strings.TrimSpace(expandEnv(cfg.DesktopModel))
	cfg.OpenCodeServerURL = expandEnv(cfg.OpenCodeServerURL)
	cfg.HTTP.WebhookSecret = expandEnv(cfg.HTTP.WebhookSecret)
	cfg.HTTP.APIToken = expandEnv(cfg.HTTP.APIToken)

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
	b.Schedules = NormalizeSchedules(b.Schedules)
}

// NormalizeSchedules expands environment references and normalizes IDs and
// delivery modes across a bot's schedules, preserving unique IDs.
func NormalizeSchedules(in []ScheduleConfig) []ScheduleConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]ScheduleConfig, len(in))
	usedIDs := make(map[string]int, len(in))
	for j, sc := range in {
		sc.Name = strings.TrimSpace(sc.Name)
		sc.DailyAt = strings.TrimSpace(sc.DailyAt)
		sc.Model = strings.TrimSpace(expandEnv(sc.Model))
		sc.NotifyTelegramChatID = strings.TrimSpace(expandEnv(sc.NotifyTelegramChatID))
		sc.MainDelivery = sc.EffectiveMainDelivery()
		baseID := sc.EffectiveID()
		if n, exists := usedIDs[baseID]; exists {
			n++
			usedIDs[baseID] = n
			baseID = fmt.Sprintf("%s-%d", sc.EffectiveID(), n)
			for usedIDs[baseID] > 0 {
				n++
				baseID = fmt.Sprintf("%s-%d", sc.EffectiveID(), n)
			}
		}
		usedIDs[baseID] = 1
		sc.ID = baseID
		out[j] = sc
	}
	return out
}

// DefaultModelFor returns the model a bot of this harness inherits; empty
// means the harness's own default.
func (c *HubConfig) DefaultModelFor(harness string) string {
	if m := c.DefaultModels[harness]; m != "" {
		return expandEnv(m)
	}
	if c.DefaultModel == "" {
		return ""
	}
	// A magpie model works in every harness magpie wires up (claude takes
	// the bare gateway ID); a bare ID/alias works across all harnesses;
	// provider/model only fits opencode and pi.
	if strings.HasPrefix(c.DefaultModel, "magpie/") {
		if harness == "claude" {
			return strings.TrimPrefix(c.DefaultModel, "magpie/")
		}
		return c.DefaultModel
	}
	if !strings.Contains(c.DefaultModel, "/") {
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

const PlatformInstructionsFilename = "BOT_HUB.md"

// PlatformInstructionsText returns the concise hub-wide usage guide written to
// _shared/BOT_HUB.md on startup and workspace sync.
func PlatformInstructionsText() string {
	return strings.Join([]string{
		"<!-- Managed by bot-hub; overwritten on startup. Put personal global preferences in _shared/USER.md. -->",
		"# Bot Hub Platform Guide",
		"",
		"- **Workspace Model**: `1 Bot = 1 Workspace = 1 Active Session`. Keep replies concise and direct.",
		"- **Shared Context & Memory**:",
		"  - Read `_shared/USER.md` for hub-wide user preferences.",
		"  - When Magpie Memory tools (`memory_search` / `memory_save`) are available, store cross-project preferences with `scope=\"global\"` and this bot's durable notes with `scope=\"project\"` and tag `bot:<your-bot-id>` (and pass that tag when searching so sibling workspaces inside a git repo never mix). Otherwise store durable notes in `memory/`.",
		"- **MCP Core Tools (`bot-hub` MCP, 6 runtime primitives)**:",
		"  - `message_bot(botId, message)`: collaborate with another bot.",
		"  - `recall_session(...)`: read sliced messages from `previous` / `active` / `<sessionId>` (keep slices small; fetch single oversized messages by `messageId`). If `bot-hub session list` returns a `magpieSession`, pass it to `magpie-sessions__read_session` for raw tool transcripts.",
		"  - `desktop_task(goal, ...)` & `request_takeover(reason, ...)`: delegate multi-step GUI tasks to the visible desktop worker, or pause for human QR/2FA/CAPTCHA/payment takeover.",
		"  - `node_exec(command, ...)` & `node_file(op, path, ...)`: run shell commands or read/write/list files on connected remote nodes.",
		"- **Full `bot-hub` CLI (auto-reads current bot context from env or workspace config)**:",
		"  - **Bots & Self-Edit**: `bot-hub bot list | get [--id <id>] | create --id <id> --name <name> [--harness opencode|pi|dsh|claude|agy] | update [--id <id>] [--name ...] [--description ...] [--instructions-file AGENTS.md] | delete --id <id> | message <id> <msg>`.",
		"  - **Sessions**: `bot-hub session list [--bot <id|*>] [--query ...] | recall [--id previous|active|<sid>] | rotate [--handoff \"...\"]` (rotate regularly on harnesses without native `billion-context` compression like `agy`, `dsh`).",
		"  - **Routines**: `bot-hub routine list | upsert --name <n> --prompt <p> [--every-minutes N | --daily-at HH:MM] [--main-delivery receipt|notify|none] | delete --id <r> | run --id <r>` (isolated ephemeral turns with `AGENTS.md` + `memory/`).",
		"  - **Nodes & Desktop**: `bot-hub node list | exec --command <cmd> | file --op read|write|list --path <p>` and `bot-hub desktop task --goal <g> | takeover --reason <r>`.",
		"- **Local Desktop & Browser CLI (run locally or via `node_exec`)**:",
		"  - Desktop GUI (`agent-desktop` CLI): operate apps via accessibility-tree `snapshot` + `@ref` instead of blind coordinates.",
		"  - Browser (`agent-browser` CLI):",
		"    1. Prefer `agent-browser --auto-connect --pin-tab` to attach to the running Chromium browser in an isolated tab (reuses logins without stealing mouse/focus).",
		"    2. If attach fails, use default headless mode for background/read-only work, or `--headed --profile ~/.bot-hub/browser/<bot-id>` when login or human visual takeover is needed.",
		"    3. Pass `--session <bot-id>` to isolate state across bots.",
		"",
	}, "\n")
}

// EnsurePlatformInstructions writes the managed _shared/BOT_HUB.md file atomically.
func EnsurePlatformInstructions(sharedDir string) (string, error) {
	if strings.TrimSpace(sharedDir) == "" {
		return "", errors.New("sharedDir is empty")
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(sharedDir, PlatformInstructionsFilename)
	want := PlatformInstructionsText()
	if cur, err := os.ReadFile(target); err == nil && string(cur) == want {
		return target, nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(want), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
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
			"- Follow platform conventions in `_shared/BOT_HUB.md` and shared user preferences in `_shared/USER.md`.",
			"",
		}, "\n")
		if err := os.WriteFile(agentsMD, []byte(content), 0o644); err != nil {
			return err
		}
	}

	// Write workspace-local opencode.json so the bot automatically gets bot-hub MCP tools
	// and _shared/BOT_HUB.md instructions without clobbering global Magpie config.
	if err := ensureOpenCodeMCPConfig(bot, cfg); err != nil {
		return err
	}
	return nil
}

// MCPCommand is the stdio command that exposes bot-hub's tools to a bot's agent.
func MCPCommand(cfg *HubConfig, botID string) []string {
	cmd := []string{ResolveBotHubBinary(cfg.ProjectRoot), "mcp", "--hub-url", cfg.HTTP.BaseURL(), "--bot-id", botID}
	if cfg.ConfigPath != "" {
		cmd = append(cmd, "--config", cfg.ConfigPath)
	}
	return cmd
}

type rawTokenDoc struct {
	HTTP struct {
		APIToken string `json:"apiToken"`
	} `json:"http"`
}

func readRawAPIToken(configPath string) string {
	if configPath == "" {
		return ""
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var doc rawTokenDoc
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return strings.TrimSpace(doc.HTTP.APIToken)
}

// RawAPITokenEnvRef returns the raw http.apiToken string if it contains an
// unexpanded `${VAR}` environment reference, or empty string otherwise.
func RawAPITokenEnvRef(configPath string) string {
	tok := readRawAPIToken(configPath)
	if envRefRe.MatchString(tok) {
		return tok
	}
	return ""
}

// ReadAPIToken reads and expands http.apiToken from configPath (loading any
// sibling .env file from its project root).
func ReadAPIToken(configPath string) string {
	tok := readRawAPIToken(configPath)
	if tok == "" {
		return ""
	}
	loadDotEnv(filepath.Dir(filepath.Dir(configPath)))
	return strings.TrimSpace(expandEnv(tok))
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

	const platInstr = "_shared/" + PlatformInstructionsFilename
	var nextInstr []string
	seenInstr := map[string]bool{}
	if rawList, ok := doc["instructions"].([]any); ok {
		for _, item := range rawList {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" && !seenInstr[s] {
				seenInstr[s] = true
				nextInstr = append(nextInstr, s)
			}
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

func ResolveBotHubBinary(projectRoot string) string {
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
