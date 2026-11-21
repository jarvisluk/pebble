package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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

var magpie = struct {
	mu      sync.Mutex
	catalog []string
	fetched time.Time
}{}

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

func invalidateMagpieCache() {
	magpie.mu.Lock()
	magpie.fetched = time.Time{}
	magpie.mu.Unlock()
}

// MagpieGatewayHealthy reports whether magpie's local gateway answers `magpie healthcheck`.
func MagpieGatewayHealthy(ctx context.Context) bool {
	if !magpieInstalled() {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return exec.CommandContext(cctx, magpieBin(), "healthcheck").Run() == nil
}

// magpieCatalog lists the gateway's model IDs (cached for a minute).
func magpieCatalog(ctx context.Context) []string {
	magpie.mu.Lock()
	defer magpie.mu.Unlock()
	if time.Since(magpie.fetched) < time.Minute {
		return magpie.catalog
	}
	if !magpieInstalled() {
		return nil
	}
	out, err := exec.CommandContext(ctx, magpieBin(), "models").Output()
	if err != nil {
		return magpie.catalog
	}
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && strings.Contains(f[0], "/") && !strings.Contains(f[0], "://") {
			ids = append(ids, f[0])
		}
	}
	magpie.catalog, magpie.fetched = ids, time.Now()
	return ids
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
	if val != "" && val != "—" && val != "-" && !strings.HasPrefix(val, "·") {
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
	st := MagpieStatus{Installed: true}
	out, err := exec.CommandContext(ctx, magpieBin(), a.agent).Output()
	if err != nil {
		return st
	}
	var catalog []string
	if a.prefix == "" {
		catalog = magpieCatalog(ctx)
	}
	st.Connected, st.Model, st.NativeModel = parseAgentStatusLine(a.prefix, string(out), catalog)
	return st
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
	out, err := exec.CommandContext(ctx, magpieBin(), a.agent, id).CombinedOutput()
	if err != nil {
		return fmt.Errorf("magpie %s %s: %s", a.agent, id, redactSecrets(strings.TrimSpace(string(out))))
	}
	invalidateMagpieCache()
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
	out, err := exec.CommandContext(ctx, magpieBin(), a.agent, "default").CombinedOutput()
	if err != nil {
		return fmt.Errorf("magpie %s default: %s", a.agent, redactSecrets(strings.TrimSpace(string(out))))
	}
	invalidateMagpieCache()
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
	out, err := exec.CommandContext(ctx, magpieBin(), "providers").Output()
	if err != nil {
		return nil
	}
	return parseProvidersOutput(string(out))
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
	out, err := exec.CommandContext(ctx, magpieBin(), "presets").Output()
	if err == nil {
		if list := parsePresetsOutput(string(out)); len(list) > 0 {
			return list
		}
	}
	return append([]MagpiePreset(nil), defaultPresets...)
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

	out, err := exec.CommandContext(ctx, magpieBin(), args...).CombinedOutput()
	cleaned := redactSecrets(strings.TrimSpace(string(out)))
	if err != nil {
		if cleaned != "" {
			return "", fmt.Errorf("%s", cleaned)
		}
		return "", fmt.Errorf("magpie provider %s: %w", action, err)
	}
	if mutating {
		invalidateMagpieCache()
	}
	return cleaned, nil
}
