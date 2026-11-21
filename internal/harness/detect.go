package harness

import (
	"os"
	"os/exec"
	"path/filepath"
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
	meta := HarnessInstallMeta(kind)
	if meta.Binary == "" {
		return false, ""
	}
	if p, err := lookPathFunc(meta.Binary); err == nil && p != "" {
		return true, p
	}
	if kind == "opencode" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			candidate := filepath.Join(home, ".opencode", "bin", "opencode")
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
	for _, kind := range []string{"opencode", "claude", "pi", "dsh"} {
		if ok, _ := DetectHarness(kind); ok {
			return kind
		}
	}
	return ""
}
