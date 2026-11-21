package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HarnessInstallInfo describes how to obtain and install a supported agent runtime.
type HarnessInstallInfo struct {
	Binary  string `json:"binary"`
	Command string `json:"command"`
	URL     string `json:"url"`
}

var lookPathFunc = exec.LookPath

var harnessInstallMeta = map[string]HarnessInstallInfo{
	"opencode": {
		Binary:  "opencode",
		Command: "curl -fsSL https://opencode.ai/install | bash",
		URL:     "https://opencode.ai",
	},
	"claude": {
		Binary:  "claude",
		Command: "npm install -g @anthropic-ai/claude-code",
		URL:     "https://docs.anthropic.com/en/docs/claude-code",
	},
	"pi": {
		Binary:  "pi",
		Command: "npm install -g @mariozechner/pi-coding-agent",
		URL:     "https://github.com/badlogic/pi-mono",
	},
	"dsh": {
		Binary:  "dsh",
		Command: "查看 DeepSeek Harness 安装说明",
		URL:     "https://github.com/deepseek-ai/deepseek-harness",
	},
	"agy": {
		Binary:  "agy",
		Command: "查看 Antigravity CLI 安装说明",
	},
}

// HarnessInstallMeta returns install guidance metadata for a harness kind.
func HarnessInstallMeta(kind string) HarnessInstallInfo {
	if info, ok := harnessInstallMeta[kind]; ok {
		return info
	}
	return HarnessInstallInfo{Binary: kind}
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
	if meta.Binary == "" {
		return false, ""
	}
	if p, err := lookPathFunc(meta.Binary); err == nil && p != "" {
		return true, p
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		var candidate string
		switch kind {
		case "opencode":
			candidate = filepath.Join(home, ".opencode", "bin", "opencode")
		case "agy":
			candidate = filepath.Join(home, ".local", "bin", "agy")
		}
		if candidate != "" {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return true, candidate
			}
		}
	}
	return false, ""
}

// PreferredInstalledHarness returns the first installed harness in default priority order,
// or empty string when none is installed.
func PreferredInstalledHarness() string {
	for _, kind := range []string{"opencode", "claude", "pi", "dsh", "agy"} {
		if ok, _ := DetectHarness(kind); ok {
			return kind
		}
	}
	return ""
}
