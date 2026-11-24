package desktophost

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
)

// DataDir returns the root directory where the desktop host keeps config/, state/, and workspaces/.
func DataDir() (string, error) {
	if custom := config.LookupEnv("PEBBLE_DATA_DIR", "BOT_HUB_DATA_DIR"); custom != "" {
		return custom, os.MkdirAll(custom, 0o700)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := resolveDefaultDataDir(base)
	return dir, os.MkdirAll(dir, 0o700)
}

func resolveDefaultDataDir(base string) string {
	pebbleDir := filepath.Join(base, "pebble")
	if info, err := os.Stat(pebbleDir); err == nil && info.IsDir() {
		return pebbleDir
	}
	legacyDir := filepath.Join(base, "bot-hub")
	if info, err := os.Stat(legacyDir); err == nil && info.IsDir() {
		return legacyDir
	}
	return pebbleDir
}

// EnsureConfig writes a starter config/bots.json on first local run.
func EnsureConfig(dir string) error {
	_, _, err := config.EnsureStarterConfig(dir, harness.PreferredInstalledHarness())
	return err
}

// ShellEnv inherits PATH and proxy settings from the login shell because a GUI app
// started from Finder or Dock gets a bare environment without user-local bins or proxies.
func ShellEnv() {
	if runtime.GOOS == "windows" {
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		for _, cand := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
			if _, err := os.Stat(cand); err == nil {
				shell = cand
				break
			}
		}
	}
	if shell != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, shell, "-ilc", "printf '\\n__ENV__\\n'; env").Output()
		cancel()
		if i := strings.LastIndex(string(out), "__ENV__\n"); err == nil && i >= 0 {
			for _, kv := range strings.Split(string(out[i+len("__ENV__\n"):]), "\n") {
				k, v, ok := strings.Cut(kv, "=")
				if ok && v != "" && (k == "PATH" || strings.HasSuffix(strings.ToLower(k), "_proxy")) {
					os.Setenv(k, v)
				}
			}
		}
	}
	extraBins := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		extraBins = append(extraBins,
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if filepath.Base(exeDir) == "bin" && strings.HasSuffix(filepath.Clean(exeDir), filepath.Join("Resources", "bin")) {
			extraBins = append(extraBins, exeDir)
		}
		resBin := filepath.Clean(filepath.Join(exeDir, "..", "Resources", "bin"))
		if info, err := os.Stat(resBin); err == nil && info.IsDir() {
			extraBins = append(extraBins, resBin)
		}
	}
	pathVal := os.Getenv("PATH")
	for _, dir := range extraBins {
		pathVal = AppendPathIfMissing(pathVal, dir)
	}
	os.Setenv("PATH", pathVal)
	if runtime.GOOS == "darwin" && strings.TrimSpace(os.Getenv("AGENT_BROWSER_EXECUTABLE_PATH")) == "" {
		if p := DetectDefaultChromiumExecutable(); p != "" {
			os.Setenv("AGENT_BROWSER_EXECUTABLE_PATH", p)
		}
	}
}

// AppendPathIfMissing appends dir to pathEnv if not already present.
func AppendPathIfMissing(pathEnv, dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return pathEnv
	}
	cleanDir := filepath.Clean(dir)
	var parts []string
	found := false
	for _, seg := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		if seg == "" {
			continue
		}
		if filepath.Clean(seg) == cleanDir {
			found = true
		}
		parts = append(parts, seg)
	}
	if !found {
		parts = append(parts, cleanDir)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

var macChromiumApps = map[string]string{
	"com.microsoft.edgemac":      "Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"com.google.chrome":          "Google Chrome.app/Contents/MacOS/Google Chrome",
	"com.google.chrome.canary":   "Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	"com.brave.browser":          "Brave Browser.app/Contents/MacOS/Brave Browser",
	"company.thebrowser.browser": "Arc.app/Contents/MacOS/Arc",
	"company.thebrowser.dia":     "Dia.app/Contents/MacOS/Dia",
	"com.vivaldi.vivaldi":        "Vivaldi.app/Contents/MacOS/Vivaldi",
	"org.chromium.chromium":      "Chromium.app/Contents/MacOS/Chromium",
}

var macChromiumFallbackBundles = []string{
	"com.microsoft.edgemac",
	"com.google.chrome",
	"com.brave.browser",
}

// DetectDefaultChromiumExecutable resolves the user's default Chromium-based browser executable on macOS.
func DetectDefaultChromiumExecutable() string {
	home, _ := os.UserHomeDir()
	var plistJSON []byte
	if home != "" {
		plistPath := filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices", "com.apple.launchservices.secure.plist")
		if _, err := os.Stat(plistPath); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", plistPath).Output(); err == nil {
				plistJSON = out
			}
		}
	}
	return ChromiumExecutableFromHandlers(plistJSON, home, func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	})
}

// ChromiumExecutableFromHandlers selects a Chromium browser binary from LaunchServices JSON data.
func ChromiumExecutableFromHandlers(plistJSON []byte, home string, exists func(string) bool) string {
	resolveBundle := func(bundleID string) string {
		rel, ok := macChromiumApps[strings.ToLower(strings.TrimSpace(bundleID))]
		if !ok {
			return ""
		}
		candidates := []string{filepath.Join("/Applications", rel)}
		if home != "" {
			candidates = append(candidates, filepath.Join(home, "Applications", rel))
		}
		for _, c := range candidates {
			if exists(c) {
				return c
			}
		}
		return ""
	}

	if len(plistJSON) > 0 {
		var doc struct {
			LSHandlers []struct {
				RoleAll     string `json:"LSHandlerRoleAll"`
				URLScheme   string `json:"LSHandlerURLScheme"`
				ContentType string `json:"LSHandlerContentType"`
			} `json:"LSHandlers"`
		}
		if json.Unmarshal(plistJSON, &doc) == nil {
			var httpsBundle, browserBundle string
			for _, h := range doc.LSHandlers {
				switch strings.ToLower(strings.TrimSpace(h.URLScheme)) {
				case "https":
					if httpsBundle == "" {
						httpsBundle = h.RoleAll
					}
				}
				if strings.EqualFold(strings.TrimSpace(h.ContentType), "com.apple.default-app.web-browser") && browserBundle == "" {
					browserBundle = h.RoleAll
				}
			}
			for _, b := range []string{httpsBundle, browserBundle} {
				if p := resolveBundle(b); p != "" {
					return p
				}
			}
		}
	}

	for _, b := range macChromiumFallbackBundles {
		if p := resolveBundle(b); p != "" {
			return p
		}
	}
	return ""
}
