package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
				Name:        "Pebble",
				Description: "General assistant for Q&A, organizing info, and everyday tasks",
				Color:       "#7aaa98",
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
	dpm, err := NormalizePermissionMode(cfg.DefaultPermissionMode)
	if err != nil {
		return nil, cfgPath, err
	}
	cfg.DefaultPermissionMode = dpm
	cfg.DefaultModel = expandEnv(cfg.DefaultModel)
	cfg.DesktopHarness = strings.TrimSpace(cfg.DesktopHarness)
	cfg.DesktopModel = strings.TrimSpace(expandEnv(cfg.DesktopModel))
	cfg.MagpieWebURL = strings.TrimSpace(expandEnv(cfg.MagpieWebURL))
	cfg.OpenCodeServerURL = expandEnv(cfg.OpenCodeServerURL)
	cfg.HTTP.WebhookSecret = expandEnv(cfg.HTTP.WebhookSecret)
	cfg.HTTP.APIToken = expandEnv(cfg.HTTP.APIToken)
	for i := range cfg.HTTP.DeviceTokens {
		dt := &cfg.HTTP.DeviceTokens[i]
		dt.ID = strings.TrimSpace(dt.ID)
		dt.Name = strings.TrimSpace(dt.Name)
		dt.Token = strings.TrimSpace(expandEnv(dt.Token))
		dt.TokenHash = strings.TrimSpace(dt.TokenHash)
	}
	if cfg.Node != nil {
		cfg.Node.ID = strings.TrimSpace(cfg.Node.ID)
		cfg.Node.Name = strings.TrimSpace(cfg.Node.Name)
		mpm, err := NormalizePermissionMode(cfg.Node.MaxPermissionMode)
		if err != nil {
			return nil, cfgPath, fmt.Errorf("node maxPermissionMode: %w", err)
		}
		cfg.Node.MaxPermissionMode = mpm
		for i := range cfg.Node.Workspaces {
			ws := &cfg.Node.Workspaces[i]
			ws.ID = strings.TrimSpace(ws.ID)
			ws.Name = strings.TrimSpace(ws.Name)
			ws.Path = strings.TrimSpace(expandEnv(ws.Path))
			ws.DefaultBranch = strings.TrimSpace(ws.DefaultBranch)
			if ws.Path != "" && !filepath.IsAbs(ws.Path) && !strings.HasPrefix(ws.Path, "~/") && ws.Path != "~" {
				ws.Path = absFrom(projectRoot, ws.Path)
			}
		}
	}
	if cfg.HTTP.Tailscale != nil {
		cfg.HTTP.Tailscale.Hostname = strings.TrimSpace(cfg.HTTP.Tailscale.Hostname)
		cfg.HTTP.Tailscale.ControlURL = strings.TrimSpace(expandEnv(cfg.HTTP.Tailscale.ControlURL))
		cfg.HTTP.Tailscale.AuthKey = strings.TrimSpace(expandEnv(cfg.HTTP.Tailscale.AuthKey))
	}
	if cfg.Sync != nil {
		cfg.Sync.PeerURL = strings.TrimRight(strings.TrimSpace(expandEnv(cfg.Sync.PeerURL)), "/")
		cfg.Sync.PeerToken = strings.TrimSpace(expandEnv(cfg.Sync.PeerToken))
		for i := range cfg.Sync.Peers {
			cfg.Sync.Peers[i].URL = strings.TrimRight(strings.TrimSpace(expandEnv(cfg.Sync.Peers[i].URL)), "/")
			cfg.Sync.Peers[i].Token = strings.TrimSpace(expandEnv(cfg.Sync.Peers[i].Token))
		}
		if cfg.Sync.Git != nil {
			cfg.Sync.Git.RepoURL = strings.TrimSpace(expandEnv(cfg.Sync.Git.RepoURL))
			cfg.Sync.Git.Branch = strings.TrimSpace(cfg.Sync.Git.Branch)
		}
	}
	cfg.Push.RelayURL = strings.TrimSpace(expandEnv(cfg.Push.RelayURL))
	cfg.Push.APNs.KeyPath = strings.TrimSpace(expandEnv(cfg.Push.APNs.KeyPath))
	if cfg.Push.APNs.KeyPath != "" && !filepath.IsAbs(cfg.Push.APNs.KeyPath) {
		cfg.Push.APNs.KeyPath = absFrom(projectRoot, cfg.Push.APNs.KeyPath)
	}
	cfg.Push.APNs.KeyPEM = strings.TrimSpace(expandEnv(cfg.Push.APNs.KeyPEM))
	cfg.Push.APNs.KeyID = strings.TrimSpace(expandEnv(cfg.Push.APNs.KeyID))
	cfg.Push.APNs.TeamID = strings.TrimSpace(expandEnv(cfg.Push.APNs.TeamID))
	cfg.Push.APNs.BundleID = strings.TrimSpace(expandEnv(cfg.Push.APNs.BundleID))
	cfg.Push.APNs.DefaultEnv = strings.ToLower(strings.TrimSpace(expandEnv(cfg.Push.APNs.DefaultEnv)))

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

		avatar, err := NormalizeBotAvatar(b.Avatar)
		if err != nil {
			return nil, cfgPath, fmt.Errorf("bot %q: %w", b.ID, err)
		}
		b.Avatar = avatar
		color, err := NormalizeBotColor(b.Color)
		if err != nil {
			return nil, cfgPath, fmt.Errorf("bot %q: %w", b.ID, err)
		}
		b.Color = color
		dt, err := NormalizeDesktopTarget(b.DesktopTarget)
		if err != nil {
			return nil, cfgPath, fmt.Errorf("bot %q: %w", b.ID, err)
		}
		b.DesktopTarget = dt
		bm, err := NormalizeBrowserMode(b.BrowserMode)
		if err != nil {
			return nil, cfgPath, fmt.Errorf("bot %q: %w", b.ID, err)
		}
		b.BrowserMode = bm
		pm, err := NormalizePermissionMode(b.PermissionMode)
		if err != nil {
			return nil, cfgPath, fmt.Errorf("bot %q: %w", b.ID, err)
		}
		b.PermissionMode = pm
		for j := range b.Schedules {
			rpm, err := NormalizeRoutinePermissionMode(b.Schedules[j].PermissionMode)
			if err != nil {
				return nil, cfgPath, fmt.Errorf("bot %q schedule %q: %w", b.ID, b.Schedules[j].Name, err)
			}
			b.Schedules[j].PermissionMode = rpm
		}

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

func absFrom(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

type rawTokenDoc struct {
	HTTP struct {
		APIToken  string `json:"apiToken"`
		Tailscale *struct {
			AuthKey string `json:"authKey"`
		} `json:"tailscale"`
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

// RawTailscaleAuthKeyEnvRef returns the raw http.tailscale.authKey string if it
// contains an unexpanded `${VAR}` environment reference, or empty string otherwise.
func RawTailscaleAuthKeyEnvRef(configPath string) string {
	if configPath == "" {
		return ""
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var doc rawTokenDoc
	if json.Unmarshal(raw, &doc) != nil || doc.HTTP.Tailscale == nil {
		return ""
	}
	ak := strings.TrimSpace(doc.HTTP.Tailscale.AuthKey)
	if envRefRe.MatchString(ak) {
		return ak
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
