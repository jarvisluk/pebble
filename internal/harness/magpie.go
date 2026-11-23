package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Magpie (https://usemagpie.ai) wires agent CLIs to its local model gateway by
// editing each CLI's own config. bot-hub delegates unified model/key
// management directly to the native magpie CLI.

// magpieAgents maps a harness kind to magpie's agent name and the prefix the
// CLI puts before a magpie model ID (claude talks to the gateway directly).
var magpieAgents = map[string]struct{ agent, prefix string }{
	"opencode": {"opencode", "magpie/"},
	"pi":       {"pi", "magpie/"},
	"dsh":      {"dsh", "magpie/"},
	"claude":   {"claude", ""},
	"agy":      {"agy", "magpie/"},
}

// MagpieInstall tells the UI how to get magpie.
var MagpieInstall = map[string]string{
	"script":   "curl -fsSL https://usemagpie.ai/install.sh | sh",
	"brew":     "brew install --cask magpie",
	"download": "https://usemagpie.ai/download",
	"docs":     "https://usemagpie.ai/docs/start",
}

// MagpieInstalled reports whether the magpie CLI is available.
func MagpieInstalled() bool { return magpieInstalled() }

// MagpieStatus is one harness as magpie sees it.
type MagpieStatus struct {
	Installed   bool   `json:"installed"`             // the magpie CLI is available
	Connected   bool   `json:"connected"`             // the harness's model is routed through magpie
	Model       string `json:"model,omitempty"`       // active magpie model when Connected
	NativeModel string `json:"nativeModel,omitempty"` // harness's native CLI model when !Connected
}

// MagpiePreset describes a built-in provider preset supported by `magpie provider add`.
type MagpiePreset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MagpieProvider describes one provider entry returned by `magpie providers`.
type MagpieProvider struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Endpoint   string `json:"endpoint,omitempty"`
	Status     string `json:"status,omitempty"`
	ModelsInfo string `json:"modelsInfo,omitempty"`
	UsedBy     string `json:"usedBy,omitempty"`
	Enabled    bool   `json:"enabled"`
}

// MagpieProviderReq describes a mutating or diagnostic action on magpie providers.
type MagpieProviderReq struct {
	Action   string `json:"action"`             // add | on | off | rm | refresh | test
	ID       string `json:"id,omitempty"`       // provider ID for on/off/rm/refresh/test
	Preset   string `json:"preset,omitempty"`   // preset ID for add
	Name     string `json:"name,omitempty"`     // custom vendor name for add
	URL      string `json:"url,omitempty"`      // base URL for custom vendor add
	Protocol string `json:"protocol,omitempty"` // openai | anthropic | responses | gemini
	Key      string `json:"key,omitempty"`      // API key (passed via argv only, never logged)
	Models   string `json:"models,omitempty"`   // optional comma-separated models
}

type ttlCache[T any] struct {
	mu      sync.Mutex
	val     T
	fetched time.Time
	ttl     time.Duration
}

func (c *ttlCache[T]) get(load func() (T, bool)) T {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetched.IsZero() && time.Since(c.fetched) < c.ttl {
		return c.val
	}
	val, ok := load()
	if ok {
		c.val = val
		c.fetched = time.Now()
		return val
	}
	if !c.fetched.IsZero() {
		return c.val
	}
	return val
}

func (c *ttlCache[T]) invalidate() {
	c.mu.Lock()
	c.fetched = time.Time{}
	c.mu.Unlock()
}

var (
	catalogCache   = ttlCache[[]string]{ttl: time.Minute}
	healthCache    = ttlCache[bool]{ttl: 30 * time.Second}
	providersCache = ttlCache[[]MagpieProvider]{ttl: 30 * time.Second}
	presetsCache   = ttlCache[[]MagpiePreset]{ttl: 10 * time.Minute}
	dataDirsCache  = ttlCache[[2]string]{ttl: 10 * time.Minute}
	statusCachesMu sync.Mutex
	statusCaches   = make(map[string]*ttlCache[MagpieStatus])

	runMagpieOutput = func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, magpieBin(), args...).Output()
	}
	runMagpieCombined = func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, magpieBin(), args...).CombinedOutput()
	}
)

func statusCacheFor(kind string) *ttlCache[MagpieStatus] {
	statusCachesMu.Lock()
	defer statusCachesMu.Unlock()
	c := statusCaches[kind]
	if c == nil {
		c = &ttlCache[MagpieStatus]{ttl: 30 * time.Second}
		statusCaches[kind] = c
	}
	return c
}

var (
	spaces           = regexp.MustCompile(`\s{2,}`)
	validMagpieIDRe  = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	secretPatternRe  = regexp.MustCompile(`(?i)\b(sk-[a-zA-Z0-9._-]{10,}|tvly-[a-zA-Z0-9._-]{10,}|AIza[0-9A-Za-z_-]{20,})\b`)
	bearerSecretRe   = regexp.MustCompile(`(?i)(Bearer\s+)[a-zA-Z0-9._-]{10,}`)
	defaultPresets   = []MagpiePreset{{ID: "deepseek", Name: "DeepSeek"}, {ID: "anthropic", Name: "Anthropic"}, {ID: "openai", Name: "OpenAI"}, {ID: "openrouter", Name: "OpenRouter"}, {ID: "google", Name: "Google Gemini"}, {ID: "kimi", Name: "Kimi"}}
)

func redactSecrets(s string) string {
	s = secretPatternRe.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) <= 8 {
			return "***"
		}
		return m[:4] + "…" + m[len(m)-4:]
	})
	s = bearerSecretRe.ReplaceAllString(s, "${1}***")
	return s
}

func magpieBin() string {
	if p := strings.TrimSpace(os.Getenv("MAGPIE_BIN")); p != "" {
		return p
	}
	if p, err := exec.LookPath("magpie"); err == nil {
		return p
	}
	if runtime.GOOS == "darwin" {
		const appBin = "/Applications/magpie.app/Contents/MacOS/magpie"
		if info, err := os.Stat(appBin); err == nil && !info.IsDir() {
			return appBin
		}
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			winBin := filepath.Join(local, "magpie", "magpie.exe")
			if info, err := os.Stat(winBin); err == nil && !info.IsDir() {
				return winBin
			}
		}
	}
	return "magpie"
}

func magpieInstalled() bool {
	bin := magpieBin()
	if filepath.IsAbs(bin) {
		info, err := os.Stat(bin)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// InvalidateMagpieCache clears all cached Magpie gateway, provider, preset,
// catalog, and per-harness status snapshots.
func InvalidateMagpieCache() {
	catalogCache.invalidate()
	healthCache.invalidate()
	providersCache.invalidate()
	presetsCache.invalidate()
	dataDirsCache.invalidate()
	statusCachesMu.Lock()
	for _, c := range statusCaches {
		c.invalidate()
	}
	statusCachesMu.Unlock()
}

// WarmMagpieCache populates all Magpie caches concurrently in a single parallel
// wave so interactive settings loads complete immediately.
func WarmMagpieCache(ctx context.Context, kinds []string) {
	if !magpieInstalled() {
		return
	}
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); MagpieGatewayHealthy(ctx) }()
	go func() { defer wg.Done(); MagpieListProviders(ctx) }()
	go func() { defer wg.Done(); MagpiePresets(ctx) }()
	go func() { defer wg.Done(); magpieCatalog(ctx) }()
	for _, k := range kinds {
		kind := k
		if _, ok := magpieAgents[kind]; !ok {
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); GetMagpieStatus(ctx, kind) }()
	}
	wg.Wait()
}

// MagpieGatewayHealthy reports whether magpie's local gateway answers `magpie healthcheck`.
func MagpieGatewayHealthy(ctx context.Context) bool {
	if !magpieInstalled() {
		return false
	}
	return healthCache.get(func() (bool, bool) {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_, err := runMagpieOutput(cctx, "healthcheck")
		if ctx.Err() != nil {
			return false, false
		}
		return err == nil, true
	})
}

// magpieCatalog lists the gateway's model IDs (cached for a minute).
func magpieCatalog(ctx context.Context) []string {
	if !magpieInstalled() {
		return nil
	}
	ids := catalogCache.get(func() ([]string, bool) {
		out, err := runMagpieOutput(ctx, "models")
		if err != nil {
			return nil, false
		}
		var parsed []string
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) > 0 && strings.Contains(f[0], "/") && !strings.Contains(f[0], "://") {
				parsed = append(parsed, f[0])
			}
		}
		return parsed, true
	})
	return append([]string(nil), ids...)
}

// MagpieModels returns magpie's catalog as model IDs for the given harness.
func MagpieModels(ctx context.Context, kind string) []string {
	a, ok := magpieAgents[kind]
	if !ok {
		return nil
	}
	ids := magpieCatalog(ctx)
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = a.prefix + id
	}
	return out
}

func parseAgentStatusLine(prefix, raw string, catalog []string) (connected bool, model, nativeModel string) {
	line, _, _ := strings.Cut(strings.TrimSpace(raw), "\n")
	f := spaces.Split(line, -1)
	if len(f) < 2 {
		return false, "", ""
	}
	val := strings.TrimSpace(f[1])
	if prefix != "" {
		connected = strings.HasPrefix(val, prefix)
	} else {
		if strings.HasPrefix(val, "group/") {
			connected = true
		} else {
			for _, c := range catalog {
				if c == val {
					connected = true
					break
				}
			}
		}
	}
	if connected {
		return true, val, ""
	}
	if val != "" && val != "—" && val != "-" && !strings.HasPrefix(val, "·") &&
		!strings.EqualFold(val, "status") && !strings.Contains(val, " ") {
		return false, "", val
	}
	return false, "", ""
}

// GetMagpieStatus reads `magpie <agent>`: "  Name  <model>  ·  …  <config path>".
func GetMagpieStatus(ctx context.Context, kind string) MagpieStatus {
	a, ok := magpieAgents[kind]
	if !ok || !magpieInstalled() {
		return MagpieStatus{}
	}
	return statusCacheFor(kind).get(func() (MagpieStatus, bool) {
		st := MagpieStatus{Installed: true}
		out, err := runMagpieOutput(ctx, a.agent)
		if err != nil {
			return st, false
		}
		var catalog []string
		if a.prefix == "" {
			catalog = magpieCatalog(ctx)
		}
		st.Connected, st.Model, st.NativeModel = parseAgentStatusLine(a.prefix, string(out), catalog)
		return st, true
	})
}

// MagpieConnect runs `magpie <agent> <model>`, which points the harness's CLI
// (its global config) at magpie's gateway with that model.
func MagpieConnect(ctx context.Context, kind, model string) error {
	a, ok := magpieAgents[kind]
	if !ok {
		return fmt.Errorf("magpie does not manage harness %q", kind)
	}
	if !magpieInstalled() {
		return fmt.Errorf("magpie is not installed")
	}
	id := strings.TrimPrefix(strings.TrimSpace(model), "magpie/")
	if id == "" {
		return fmt.Errorf("请先选择或填写要接入的模型")
	}
	out, err := runMagpieCombined(ctx, a.agent, id)
	if err != nil {
		return fmt.Errorf("magpie %s %s: %s", a.agent, id, redactSecrets(strings.TrimSpace(string(out))))
	}
	InvalidateMagpieCache()
	return nil
}

// MagpieDisconnect runs `magpie <agent> default`, restoring the harness's CLI to its native config.
func MagpieDisconnect(ctx context.Context, kind string) error {
	a, ok := magpieAgents[kind]
	if !ok {
		return fmt.Errorf("magpie does not manage harness %q", kind)
	}
	if !magpieInstalled() {
		return fmt.Errorf("magpie is not installed")
	}
	out, err := runMagpieCombined(ctx, a.agent, "default")
	if err != nil {
		return fmt.Errorf("magpie %s default: %s", a.agent, redactSecrets(strings.TrimSpace(string(out))))
	}
	InvalidateMagpieCache()
	return nil
}

// MagpieOpen opens magpie's own window on this machine, where providers and
// models are managed.
func MagpieOpen() error {
	if !magpieInstalled() {
		return fmt.Errorf("magpie is not installed")
	}
	cmd := exec.Command(magpieBin())
	if runtime.GOOS == "darwin" {
		if info, err := os.Stat("/Applications/magpie.app"); err == nil && info.IsDir() {
			cmd = exec.Command("open", "/Applications/magpie.app")
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func parseProvidersOutput(raw string) []MagpieProvider {
	var out []MagpieProvider
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		cols := spaces.Split(trimmed, -1)
		statusIdx := -1
		for i, c := range cols {
			if strings.HasPrefix(c, "●") || strings.HasPrefix(c, "○") {
				statusIdx = i
				break
			}
		}
		if statusIdx < 2 {
			continue
		}
		p := MagpieProvider{
			Name:    cols[0],
			ID:      cols[statusIdx-1],
			Enabled: strings.HasPrefix(cols[statusIdx], "●"),
			Status:  redactSecrets(strings.TrimSpace(strings.TrimLeft(cols[statusIdx], "●○ "))),
		}
		if statusIdx >= 3 {
			p.ID = cols[statusIdx-2]
			p.Endpoint = cols[statusIdx-1]
		}
		var modelParts []string
		for _, extra := range cols[statusIdx+1:] {
			extra = strings.TrimSpace(extra)
			if extra == "" || strings.HasPrefix(extra, "magpie provider ") {
				continue
			}
			if strings.HasPrefix(extra, "←") {
				p.UsedBy = strings.TrimSpace(strings.TrimPrefix(extra, "←"))
			} else {
				modelParts = append(modelParts, extra)
			}
		}
		p.ModelsInfo = strings.Join(modelParts, " · ")
		out = append(out, p)
	}
	return out
}

// MagpieListProviders returns the configured providers from `magpie providers`.
func MagpieListProviders(ctx context.Context) []MagpieProvider {
	if !magpieInstalled() {
		return nil
	}
	list := providersCache.get(func() ([]MagpieProvider, bool) {
		out, err := runMagpieOutput(ctx, "providers")
		if err != nil {
			return nil, false
		}
		return parseProvidersOutput(string(out)), true
	})
	if list == nil {
		return nil
	}
	return append([]MagpieProvider(nil), list...)
}

func parsePresetsOutput(raw string) []MagpiePreset {
	var out []MagpiePreset
	seen := make(map[string]bool)
	for _, line := range strings.Split(raw, "\n") {
		if !strings.Contains(line, "magpie provider add ") {
			continue
		}
		cols := spaces.Split(strings.TrimSpace(line), -1)
		if len(cols) < 2 {
			continue
		}
		id := strings.TrimSpace(cols[1])
		name := strings.TrimSpace(cols[0])
		if id == "" || seen[id] || !validMagpieIDRe.MatchString(id) {
			continue
		}
		seen[id] = true
		out = append(out, MagpiePreset{ID: id, Name: name})
	}
	return out
}

// MagpiePresets returns vendor presets supported by `magpie provider add`.
func MagpiePresets(ctx context.Context) []MagpiePreset {
	if !magpieInstalled() {
		return nil
	}
	list := presetsCache.get(func() ([]MagpiePreset, bool) {
		out, err := runMagpieOutput(ctx, "presets")
		if err == nil {
			if parsed := parsePresetsOutput(string(out)); len(parsed) > 0 {
				return parsed, true
			}
		}
		if ctx.Err() != nil {
			return nil, false
		}
		return append([]MagpiePreset(nil), defaultPresets...), true
	})
	return append([]MagpiePreset(nil), list...)
}

// MagpieProviderAction runs a native `magpie provider ...` management command.
func MagpieProviderAction(ctx context.Context, req MagpieProviderReq) (string, error) {
	if !magpieInstalled() {
		return "", fmt.Errorf("magpie 未安装")
	}
	action := strings.TrimSpace(req.Action)
	var args []string
	mutating := true

	switch action {
	case "add":
		preset := strings.TrimSpace(req.Preset)
		name := strings.TrimSpace(req.Name)
		key := strings.TrimSpace(req.Key)
		url := strings.TrimSpace(req.URL)
		if preset != "" {
			if !validMagpieIDRe.MatchString(preset) {
				return "", fmt.Errorf("无效预设 ID: %q", preset)
			}
			args = append(args, "provider", "add", preset)
			if key != "" {
				args = append(args, key)
			}
			if name != "" {
				args = append(args, "name="+name)
			}
		} else {
			if name == "" {
				return "", fmt.Errorf("请填写名称")
			}
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				return "", fmt.Errorf("接口地址需以 http:// 或 https:// 开头")
			}
			protoField := "url="
			switch strings.TrimSpace(req.Protocol) {
			case "anthropic":
				protoField = "anthropic="
			case "responses":
				protoField = "responses="
			case "gemini":
				protoField = "gemini="
			}
			args = append(args, "provider", "add", name, protoField+url)
			if key != "" {
				args = append(args, "key="+key)
			}
			if m := strings.TrimSpace(req.Models); m != "" {
				args = append(args, "models="+m)
			}
		}

	case "on", "off", "rm", "test", "refresh":
		id := strings.TrimSpace(req.ID)
		if id == "" || !validMagpieIDRe.MatchString(id) {
			return "", fmt.Errorf("无效 provider ID: %q", req.ID)
		}
		sub := action
		if action == "refresh" {
			sub = "models"
		}
		if action == "test" {
			mutating = false
		}
		args = []string{"provider", sub, id}

	default:
		return "", fmt.Errorf("unsupported magpie provider action %q", action)
	}

	out, err := runMagpieCombined(ctx, args...)
	cleaned := redactSecrets(strings.TrimSpace(string(out)))
	if err != nil {
		if cleaned != "" {
			return "", fmt.Errorf("%s", cleaned)
		}
		return "", fmt.Errorf("magpie provider %s: %w", action, err)
	}
	if mutating {
		InvalidateMagpieCache()
	}
	return cleaned, nil
}

// MagpieMemoryItem represents one memory record returned by `magpie tool memory_search`.
type MagpieMemoryItem struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
	Text      string   `json:"text"`
}

// MagpieMemorySemanticStatus describes whether Magpie Memory's local semantic embedding search is enabled and ready.
type MagpieMemorySemanticStatus struct {
	Enabled    bool   `json:"enabled"`
	ModelReady bool   `json:"modelReady"`
	IndexReady bool   `json:"indexReady"`
	Active     bool   `json:"active"`
	Model      string `json:"model,omitempty"`
}

// MagpieMemoryList is the response returned for memory queries.
type MagpieMemoryList struct {
	Installed bool                        `json:"installed"`
	Available bool                        `json:"available"`
	Error     string                      `json:"error,omitempty"`
	Semantic  *MagpieMemorySemanticStatus `json:"semantic,omitempty"`
	Items     []MagpieMemoryItem          `json:"items"`
}

// MagpieMemorySaveReq describes a memory create/update request passed to `magpie tool memory_save`.
type MagpieMemorySaveReq struct {
	ID      string   `json:"id,omitempty"`
	Scope   string   `json:"scope,omitempty"` // "global" (default) or "project"
	Project string   `json:"project,omitempty"`
	BotID   string   `json:"botId,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Text    string   `json:"text"`
	Why     string   `json:"why,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

var (
	memoryEntryRe = regexp.MustCompile(`(?s)^\[([^\]]+)\]\s+(.+)$`)
	isoPrefixRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}|$)`)
)

type magpieToolEnvelope struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
}

func runMagpieTool(ctx context.Context, toolName string, args ...string) (*magpieToolEnvelope, string, error) {
	if !magpieInstalled() {
		return nil, "", fmt.Errorf("magpie 未安装")
	}
	cmdArgs := append([]string{"tool", toolName, "--json"}, args...)
	out, err := runMagpieCombined(ctx, cmdArgs...)
	raw := strings.TrimSpace(string(out))

	var env magpieToolEnvelope
	// magpie tool may append a trailing warning line after JSON; extract the first JSON object if present.
	if start := strings.IndexByte(raw, '{'); start >= 0 {
		dec := json.NewDecoder(strings.NewReader(raw[start:]))
		if decErr := dec.Decode(&env); decErr == nil {
			var textParts []string
			for _, c := range env.Content {
				if strings.TrimSpace(c.Text) != "" {
					textParts = append(textParts, c.Text)
				}
			}
			joined := strings.TrimSpace(strings.Join(textParts, "\n"))
			if env.IsError {
				if joined == "" {
					joined = redactSecrets(raw)
				}
				return &env, joined, fmt.Errorf("%s", joined)
			}
			if err != nil {
				return &env, joined, fmt.Errorf("%s", redactSecrets(raw))
			}
			return &env, joined, nil
		}
	}
	if err != nil {
		msg := redactSecrets(raw)
		if msg == "" {
			msg = err.Error()
		}
		return nil, "", fmt.Errorf("magpie tool %s: %s", toolName, msg)
	}
	return &env, raw, nil
}

func botMemoryTag(botID string) string {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return ""
	}
	return "bot:" + botID
}

func normalizeMemoryTags(tags []string, botID string) []string {
	seen := make(map[string]bool, len(tags)+1)
	var out []string
	add := func(t string) {
		t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		out = append(out, t)
	}
	for _, t := range tags {
		add(t)
	}
	if bt := botMemoryTag(botID); bt != "" {
		add(bt)
	}
	return out
}

func hasMemoryTag(tags []string, want string) bool {
	if want == "" {
		return true
	}
	for _, t := range tags {
		if strings.EqualFold(strings.TrimPrefix(t, "#"), want) {
			return true
		}
	}
	return false
}

func parseMemorySearchOutput(env *magpieToolEnvelope, text string) []MagpieMemoryItem {
	if env != nil && len(env.StructuredContent) > 0 {
		var sc struct {
			Items    []MagpieMemoryItem `json:"items"`
			Memories []MagpieMemoryItem `json:"memories"`
		}
		if json.Unmarshal(env.StructuredContent, &sc) == nil {
			if len(sc.Items) > 0 {
				return sc.Items
			}
			if len(sc.Memories) > 0 {
				return sc.Memories
			}
		}
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "Nothing remembered") {
		return []MagpieMemoryItem{}
	}

	lines := strings.Split(trimmed, "\n")
	var entries []string
	for _, line := range lines {
		lineTrim := strings.TrimSpace(line)
		if strings.HasPrefix(lineTrim, "(looked in:") && strings.HasSuffix(lineTrim, ")") {
			continue
		}
		if memoryEntryRe.MatchString(line) {
			entries = append(entries, line)
		} else if len(entries) > 0 {
			entries[len(entries)-1] += "\n" + line
		}
	}

	items := make([]MagpieMemoryItem, 0, len(entries))
	for _, entry := range entries {
		if item, ok := parseMemoryEntryLine(entry); ok {
			items = append(items, item)
		}
	}
	return items
}

func parseMemoryEntryLine(entry string) (MagpieMemoryItem, bool) {
	m := memoryEntryRe.FindStringSubmatch(strings.TrimSpace(entry))
	if len(m) != 3 {
		return MagpieMemoryItem{}, false
	}
	item := MagpieMemoryItem{ID: strings.TrimSpace(m[1])}
	rest := m[2]

	splitIdx := strings.Index(rest, ": ")
	if splitIdx < 0 {
		item.Text = strings.TrimSpace(rest)
		return item, true
	}
	header := rest[:splitIdx]
	item.Text = strings.TrimSpace(rest[splitIdx+2:])

	parts := strings.Split(header, ",")
	for idx, rawPart := range parts {
		p := strings.TrimSpace(rawPart)
		if p == "" {
			continue
		}
		switch {
		case idx == 0:
			item.Kind = p
		case strings.HasPrefix(p, "#"):
			for _, tok := range strings.Fields(p) {
				if tag := strings.TrimPrefix(strings.TrimSpace(tok), "#"); tag != "" {
					item.Tags = append(item.Tags, tag)
				}
			}
		case isoPrefixRe.MatchString(p):
			item.UpdatedAt = p
		case item.Scope == "":
			item.Scope = p
		}
	}
	return item, true
}

const magpieSemanticModelName = "paraphrase-multilingual-MiniLM-L12-v2"

func parseMagpieDataDirs(raw string) (filesDir, cacheDir string) {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "files:"); ok {
			filesDir = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(trimmed, "cache:"); ok {
			cacheDir = strings.TrimSpace(after)
		}
	}
	return filesDir, cacheDir
}

func defaultMagpieDataDirs() (filesDir, cacheDir string) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", ""
	}
	return filepath.Join(home, ".config", "magpie"), filepath.Join(home, ".cache", "magpie")
}

func magpieDataDirs(ctx context.Context) (string, string) {
	defFiles, defCache := defaultMagpieDataDirs()
	if !magpieInstalled() {
		return defFiles, defCache
	}
	pair := dataDirsCache.get(func() ([2]string, bool) {
		out, err := runMagpieOutput(ctx, "data")
		if err != nil {
			return [2]string{defFiles, defCache}, false
		}
		f, c := parseMagpieDataDirs(string(out))
		if f == "" {
			f = defFiles
		}
		if c == "" {
			c = defCache
		}
		return [2]string{f, c}, true
	})
	if pair[0] == "" {
		pair[0] = defFiles
	}
	if pair[1] == "" {
		pair[1] = defCache
	}
	return pair[0], pair[1]
}

func detectMagpieMemorySemanticStatus(filesDir, cacheDir string) *MagpieMemorySemanticStatus {
	st := &MagpieMemorySemanticStatus{}
	if filesDir != "" {
		if data, err := os.ReadFile(filepath.Join(filesDir, "memory-semantic.json")); err == nil {
			var cfg struct {
				On bool `json:"on"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				st.Enabled = cfg.On
			}
		}
	}
	if cacheDir != "" {
		modelDir := filepath.Join(cacheDir, magpieSemanticModelName)
		if matches, err := filepath.Glob(filepath.Join(modelDir, "*", "model_int8.onnx")); err == nil && len(matches) > 0 {
			st.ModelReady = true
		}
		if matches, err := filepath.Glob(filepath.Join(modelDir, "*", "vectors.bin")); err == nil && len(matches) > 0 {
			st.IndexReady = true
		}
	}
	if st.Enabled || st.ModelReady {
		st.Model = magpieSemanticModelName
	}
	st.Active = st.Enabled && st.ModelReady
	return st
}

func getMagpieMemorySemanticStatus(ctx context.Context) *MagpieMemorySemanticStatus {
	filesDir, cacheDir := magpieDataDirs(ctx)
	return detectMagpieMemorySemanticStatus(filesDir, cacheDir)
}

// MagpieMemorySearch queries memories via `magpie tool memory_search`.
// When botID is non-empty, it scopes to projectDir AND enforces tag `bot:<botID>`
// so multiple bots sharing a git repo root remain isolated.
func MagpieMemorySearch(ctx context.Context, scope, projectDir, botID, query, kind string, limit int) MagpieMemoryList {
	if !magpieInstalled() {
		return MagpieMemoryList{Installed: false, Available: false, Items: []MagpieMemoryItem{}}
	}
	semantic := getMagpieMemorySemanticStatus(ctx)
	scope = strings.TrimSpace(scope)
	botID = strings.TrimSpace(botID)
	if botID != "" {
		scope = "project"
		if strings.TrimSpace(projectDir) == "" {
			return MagpieMemoryList{Installed: true, Available: true, Semantic: semantic, Items: []MagpieMemoryItem{}}
		}
		if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
			return MagpieMemoryList{Installed: true, Available: true, Semantic: semantic, Items: []MagpieMemoryItem{}}
		}
	} else if scope != "all" {
		scope = "global"
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 50 {
		limit = 50
	}

	args := []string{"--scope", scope, "--limit", strconv.Itoa(limit)}
	if scope == "project" && strings.TrimSpace(projectDir) != "" {
		args = append(args, "--project", projectDir)
	}
	if q := strings.TrimSpace(query); q != "" {
		args = append(args, "--query", q)
	}
	if k := strings.TrimSpace(kind); k != "" {
		args = append(args, "--kind", k)
	}
	botTag := botMemoryTag(botID)
	if botTag != "" {
		args = append(args, "--tags", botTag)
	}

	env, text, err := runMagpieTool(ctx, "memory_search", args...)
	if err != nil {
		return MagpieMemoryList{
			Installed: true,
			Available: false,
			Error:     err.Error(),
			Semantic:  semantic,
			Items:     []MagpieMemoryItem{},
		}
	}
	items := parseMemorySearchOutput(env, text)
	if botTag != "" {
		filtered := make([]MagpieMemoryItem, 0, len(items))
		for _, it := range items {
			if hasMemoryTag(it.Tags, botTag) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	return MagpieMemoryList{
		Installed: true,
		Available: true,
		Semantic:  semantic,
		Items:     items,
	}
}

// MagpieMemorySave saves or updates a memory via `magpie tool memory_save`.
func MagpieMemorySave(ctx context.Context, req MagpieMemorySaveReq) (string, error) {
	if !magpieInstalled() {
		return "", fmt.Errorf("magpie 未安装")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return "", fmt.Errorf("记忆内容不能为空")
	}

	scope := strings.TrimSpace(req.Scope)
	botID := strings.TrimSpace(req.BotID)
	if botID != "" {
		scope = "project"
		if strings.TrimSpace(req.Project) == "" {
			return "", fmt.Errorf("缺少 Bot 工作目录")
		}
		if err := os.MkdirAll(req.Project, 0o755); err != nil {
			return "", err
		}
	}
	if scope == "" {
		scope = "global"
	}

	args := []string{"--text", text, "--scope", scope}
	if scope == "project" && strings.TrimSpace(req.Project) != "" {
		args = append(args, "--project", req.Project)
	}
	if id := strings.TrimSpace(req.ID); id != "" {
		args = append(args, "--id", id)
	}
	if k := strings.TrimSpace(req.Kind); k != "" {
		args = append(args, "--kind", k)
	}
	if w := strings.TrimSpace(req.Why); w != "" {
		args = append(args, "--why", w)
	}
	for _, t := range normalizeMemoryTags(req.Tags, botID) {
		args = append(args, "--tags", t)
	}

	_, msg, err := runMagpieTool(ctx, "memory_save", args...)
	if err != nil {
		return "", err
	}
	return msg, nil
}

// MagpieMemoryForget removes/forgets a memory via `magpie tool memory_forget`.
func MagpieMemoryForget(ctx context.Context, id string) (string, error) {
	if !magpieInstalled() {
		return "", fmt.Errorf("magpie 未安装")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("memory id is required")
	}
	_, msg, err := runMagpieTool(ctx, "memory_forget", "--id", id)
	if err != nil {
		return "", err
	}
	return msg, nil
}
