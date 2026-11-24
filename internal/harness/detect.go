package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HarnessInstallInfo describes how to obtain and install a supported agent runtime.
type HarnessInstallInfo struct {
	Binary         string   `json:"binary"`
	Binaries       []string `json:"binaries,omitempty"`
	Command        string   `json:"command"`
	URL            string   `json:"url"`
	CanAutoInstall bool     `json:"canAutoInstall,omitempty"`
	RequiresLogin  bool     `json:"requiresLogin,omitempty"`
	Category       string   `json:"category,omitempty"` // "recommended" | "advanced"
	Summary        string   `json:"summary,omitempty"`
}

var (
	lookPathFunc     = exec.LookPath
	runAuthCheckFunc = defaultRunAuthCheck
	extraBinDirsFunc = defaultExtraBinDirs
)

func defaultRunAuthCheck(ctx context.Context, bin string, args ...string) (int, string, error) {
	cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Env = augmentedEnv(nil)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out), err
		}
		return 1, string(out), err
	}
	return 0, string(out), nil
}

var harnessInstallMeta = map[string]HarnessInstallInfo{
	"opencode": {
		Binary:         "opencode",
		Command:        "curl -fsSL https://opencode.ai/install | bash",
		URL:            "https://opencode.ai",
		CanAutoInstall: true,
		RequiresLogin:  false,
		Category:       "recommended",
		Summary:        "免登录 · 内置免费模型",
	},
	"codebuddy": {
		Binary:         "codebuddy",
		Command:        "npm install -g @tencent-ai/codebuddy-code",
		URL:            "https://www.codebuddy.ai",
		CanAutoInstall: true,
		RequiresLogin:  true,
		Category:       "recommended",
		Summary:        "微信扫码 · 国内直连",
	},
	"qoder": {
		Binary:         "qoderclicn",
		Binaries:       []string{"qoderclicn", "qoder"},
		Command:        "curl -fsSL https://qoder.com.cn/install | bash",
		URL:            "https://qoder.com",
		CanAutoInstall: true,
		RequiresLogin:  true,
		Category:       "recommended",
		Summary:        "国内直连 · 免 Node 依赖",
	},
	"codex": {
		Binary:         "codex",
		Command:        "npm install -g @openai/codex",
		URL:            "https://developers.openai.com/codex/cli",
		CanAutoInstall: true,
		RequiresLogin:  true,
		Category:       "recommended",
		Summary:        "ChatGPT 订阅",
	},
	"claude": {
		Binary:         "claude",
		Command:        "curl -fsSL https://claude.ai/install.sh | bash",
		URL:            "https://docs.anthropic.com/en/docs/claude-code",
		CanAutoInstall: true,
		RequiresLogin:  true,
		Category:       "recommended",
		Summary:        "Claude 订阅",
	},
	"grok": {
		Binary:         "grok",
		Command:        "curl -fsSL https://x.ai/cli/install.sh | bash",
		URL:            "https://x.ai",
		CanAutoInstall: true,
		RequiresLogin:  true,
		Category:       "recommended",
		Summary:        "SuperGrok / X 订阅",
	},
	"trae": {
		Binary:         "traecli",
		Command:        "查看 Trae CLI 安装说明",
		URL:            "https://www.trae.cn",
		CanAutoInstall: false,
		RequiresLogin:  false,
		Category:       "advanced",
		Summary:        "字节 Trae ACP",
	},
	"pi": {
		Binary:         "pi",
		Command:        "npm install -g @mariozechner/pi-coding-agent",
		URL:            "https://github.com/badlogic/pi-mono",
		CanAutoInstall: true,
		RequiresLogin:  false,
		Category:       "advanced",
		Summary:        "极客轻量 Agent",
	},
	"dsh": {
		Binary:         "dsh",
		Command:        "查看 DeepSeek Harness 安装说明",
		URL:            "https://github.com/deepseek-ai/deepseek-harness",
		CanAutoInstall: false,
		RequiresLogin:  false,
		Category:       "advanced",
		Summary:        "DeepSeek CLI",
	},
	"agy": {
		Binary:         "agy",
		Command:        "查看 Antigravity CLI 安装说明",
		CanAutoInstall: false,
		RequiresLogin:  false,
		Category:       "advanced",
		Summary:        "Antigravity CLI",
	},
}

// HarnessInstallMeta returns install guidance metadata for a harness kind.
func HarnessInstallMeta(kind string) HarnessInstallInfo {
	if info, ok := harnessInstallMeta[kind]; ok {
		return info
	}
	return HarnessInstallInfo{Binary: kind, Category: "advanced"}
}

func defaultExtraBinDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".qoder", "bin"),
			filepath.Join(home, ".grok", "bin"),
		)
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	return dirs
}

func findBinary(bin string) (string, bool) {
	if bin == "" {
		return "", false
	}
	if p, err := lookPathFunc(bin); err == nil && p != "" {
		return p, true
	}
	for _, dir := range extraBinDirsFunc() {
		candidate := filepath.Join(dir, bin)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// resolveBinaryFor resolves a harness binary honoring an optional env override
// (which rejects fallback to host PATH if explicitly set to a missing path),
// an explicit configured/test path, and finally findBinary(defaultName).
func resolveBinaryFor(envKey, defaultName, configured string) (string, bool) {
	if envKey != "" {
		if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
			if info, err := os.Stat(v); err == nil && !info.IsDir() {
				return v, true
			}
			if p, ok := findBinary(v); ok {
				return p, true
			}
			return "", false
		}
	}
	if configured != "" && configured != defaultName {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, true
		}
		if !filepath.IsAbs(configured) {
			if p, ok := findBinary(configured); ok {
				return p, true
			}
		}
	}
	return findBinary(defaultName)
}

// DetectHarness checks whether the CLI binary for kind is installed on this machine.
func DetectHarness(kind string) (installed bool, binPath string) {
	if kind == "agy" {
		if v := strings.TrimSpace(os.Getenv("AGY_BIN")); v != "" {
			if info, err := os.Stat(v); err == nil && !info.IsDir() {
				return true, v
			}
		}
	}
	meta := HarnessInstallMeta(kind)
	candidates := meta.Binaries
	if len(candidates) == 0 && meta.Binary != "" {
		candidates = []string{meta.Binary}
	}
	for _, bin := range candidates {
		if p, ok := findBinary(bin); ok {
			return true, p
		}
	}
	return false, ""
}

// PreferredInstalledHarness returns the first installed harness in default priority order,
// or empty string when none is installed.
func PreferredInstalledHarness() string {
	for _, kind := range []string{"opencode", "codebuddy", "qoder", "codex", "claude", "grok", "trae", "pi", "dsh", "agy"} {
		if ok, _ := DetectHarness(kind); ok {
			return kind
		}
	}
	return ""
}

type authCacheEntry struct {
	status  string
	detail  string
	fetched time.Time
}

var (
	authCacheMu sync.Mutex
	authCache   = make(map[string]authCacheEntry)
)

const authCacheTTL = 30 * time.Second

// InvalidateAuthCache clears cached DetectHarnessAuth results.
func InvalidateAuthCache() {
	authCacheMu.Lock()
	clear(authCache)
	authCacheMu.Unlock()
}

// DetectHarnessAuthCached returns a briefly cached auth status for kind so
// repeated /api/settings loads stay fast without spawning CLI subprocesses.
func DetectHarnessAuthCached(ctx context.Context, kind string) (string, string) {
	authCacheMu.Lock()
	if ent, ok := authCache[kind]; ok && time.Since(ent.fetched) < authCacheTTL {
		authCacheMu.Unlock()
		return ent.status, ent.detail
	}
	authCacheMu.Unlock()

	status, detail := DetectHarnessAuth(ctx, kind)

	authCacheMu.Lock()
	authCache[kind] = authCacheEntry{status: status, detail: detail, fetched: time.Now()}
	authCacheMu.Unlock()
	return status, detail
}

// DetectHarnessAuth verifies whether an installed harness is authenticated and ready to run.
// Returns "ready", "needs_login", or "unknown".
func DetectHarnessAuth(ctx context.Context, kind string) (status string, detail string) {
	meta := HarnessInstallMeta(kind)
	if !meta.RequiresLogin {
		return "ready", ""
	}
	installed, binPath := DetectHarness(kind)
	if !installed {
		return "unknown", "not installed"
	}

	home, _ := os.UserHomeDir()
	switch kind {
	case "claude":
		if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" {
			return "ready", "API Key"
		}
		if home != "" && nonEmptyFile(filepath.Join(home, ".claude", ".credentials.json")) {
			return "ready", "已登录"
		}
		if GetMagpieStatus(ctx, "claude").Connected {
			return "ready", "Magpie"
		}
		if code, out, err := runAuthCheckFunc(ctx, binPath, "auth", "status"); err == nil && code == 0 {
			low := strings.ToLower(out)
			if !strings.Contains(low, "not logged in") && !strings.Contains(low, `"loggedin":false`) && !strings.Contains(low, `"loggedIn": false`) {
				return "ready", "已登录"
			}
		}
		return "needs_login", "需登录 Claude 账号"

	case "codex":
		if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "" {
			return "ready", "API Key"
		}
		if home != "" && nonEmptyFile(filepath.Join(home, ".codex", "auth.json")) {
			return "ready", "已登录"
		}
		if GetMagpieStatus(ctx, "codex").Connected {
			return "ready", "Magpie"
		}
		if code, out, err := runAuthCheckFunc(ctx, binPath, "login", "status"); err == nil && code == 0 {
			low := strings.ToLower(out)
			if !strings.Contains(low, "not logged in") {
				return "ready", "已登录"
			}
		}
		return "needs_login", "需登录 ChatGPT 账号"

	case "codebuddy":
		if strings.TrimSpace(os.Getenv("CODEBUDDY_API_KEY")) != "" {
			return "ready", "API Key"
		}
		if home != "" && dirHasAuthFile(filepath.Join(home, ".codebuddy")) {
			return "ready", "已登录"
		}
		return "needs_login", "需微信或账号登录"

	case "qoder":
		if strings.TrimSpace(os.Getenv("QODER_API_KEY")) != "" {
			return "ready", "API Key"
		}
		if home != "" && dirHasAuthFile(filepath.Join(home, ".qoder")) {
			return "ready", "已登录"
		}
		if code, out, err := runAuthCheckFunc(ctx, binPath, "auth", "status"); err == nil && code == 0 {
			low := strings.ToLower(out)
			if !strings.Contains(low, "not logged in") && !strings.Contains(low, "unauthenticated") {
				return "ready", "已登录"
			}
		}
		return "needs_login", "需登录 Qoder 账号"

	case "grok":
		if strings.TrimSpace(os.Getenv("XAI_API_KEY")) != "" {
			return "ready", "API Key"
		}
		if home != "" && nonEmptyFile(filepath.Join(home, ".grok", "auth.json")) {
			return "ready", "已登录"
		}
		return "needs_login", "需登录 xAI / X 账号"

	default:
		return "ready", ""
	}
}

func nonEmptyFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 2
}

func dirHasAuthFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.Contains(name, "auth") || strings.Contains(name, "cred") || strings.Contains(name, "token") || strings.Contains(name, "session") {
			if nonEmptyFile(filepath.Join(dir, e.Name())) {
				return true
			}
		}
	}
	return false
}
