package memory

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type magpieDetectCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	expiresAt time.Time
	cached    MagpieStatus
	valid     bool
}

func newMagpieDetectCache(ttl time.Duration) *magpieDetectCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &magpieDetectCache{ttl: ttl}
}

func (c *magpieDetectCache) get(fetch func() MagpieStatus) MagpieStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.valid && now.Before(c.expiresAt) {
		return cloneMagpieStatus(c.cached)
	}
	st := fetch()
	c.cached = st
	c.expiresAt = now.Add(c.ttl)
	c.valid = true
	return cloneMagpieStatus(st)
}

func (c *magpieDetectCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = false
}

func cloneMagpieStatus(st MagpieStatus) MagpieStatus {
	if len(st.InjectedHarnesses) > 0 {
		cp := make([]string, len(st.InjectedHarnesses))
		copy(cp, st.InjectedHarnesses)
		st.InjectedHarnesses = cp
	}
	return st
}

// DefaultMagpieConfigDir returns ~/.config/magpie.
func DefaultMagpieConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".config", "magpie")
}

// DetectMagpieMemory inspects the local Magpie Memory installation with TTL caching.
func (s *Store) DetectMagpieMemory(ctx context.Context) MagpieStatus {
	_ = ctx
	dir := ""
	explicitDir := false
	if s != nil {
		dir = strings.TrimSpace(s.cfg.MagpieConfigDir)
		explicitDir = dir != ""
	}
	if dir == "" {
		dir = DefaultMagpieConfigDir()
	}
	if s != nil && s.magpieCache != nil {
		return s.magpieCache.get(func() MagpieStatus {
			return detectMagpieMemoryDir(dir, explicitDir)
		})
	}
	return detectMagpieMemoryDir(dir, explicitDir)
}

// HarnessHasMagpieMemory reports whether Magpie injects magpie-memory MCP into harnessKind.
func (s *Store) HarnessHasMagpieMemory(ctx context.Context, harnessKind string) bool {
	return s.DetectMagpieMemory(ctx).HarnessHasMagpieMemory(harnessKind)
}

// DetectMagpieMemory detects local Magpie Memory state in the default ~/.config/magpie directory.
func DetectMagpieMemory(ctx context.Context) MagpieStatus {
	_ = ctx
	return detectMagpieMemoryDir(DefaultMagpieConfigDir(), false)
}

// DetectMagpieMemoryInDir detects Magpie Memory state in an explicit config directory.
func DetectMagpieMemoryInDir(configDir string) MagpieStatus {
	return detectMagpieMemoryDir(strings.TrimSpace(configDir), true)
}

// HarnessHasMagpieMemory reports whether harnessKind is among Magpie's injected harnesses.
func (m MagpieStatus) HarnessHasMagpieMemory(harnessKind string) bool {
	if !m.Installed || len(m.InjectedHarnesses) == 0 {
		return false
	}
	want := harnessAliases(harnessKind)
	if len(want) == 0 {
		return false
	}
	for _, h := range m.InjectedHarnesses {
		norm := strings.ToLower(strings.TrimSpace(h))
		for _, alias := range want {
			if norm == alias {
				return true
			}
		}
	}
	return false
}

func detectMagpieMemoryDir(configDir string, explicitDir bool) MagpieStatus {
	st := MagpieStatus{ConfigDir: configDir}
	if configDir != "" {
		if info, err := os.Stat(configDir); err == nil && info.IsDir() {
			st.Installed = true
		}
	}
	if !st.Installed && !explicitDir {
		if _, err := exec.LookPath("magpie"); err == nil {
			st.Installed = true
		}
	}
	if !st.Installed || configDir == "" {
		return st
	}

	st.InjectedHarnesses = parseMagpieLibraryHarnesses(filepath.Join(configDir, "library.json"))
	count := countMagpieLegacyItems(filepath.Join(configDir, "memory"))
	st.LegacyCount = count
	st.HasLegacyItems = count > 0
	return st
}

func parseMagpieLibraryHarnesses(libraryPath string) []string {
	raw, err := os.ReadFile(libraryPath)
	if err != nil {
		return nil
	}
	var lib struct {
		MCP []struct {
			Name    string   `json:"name"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Agents  []string `json:"agents"`
			Hidden  bool     `json:"hidden"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &lib); err != nil {
		return nil
	}

	seen := map[string]bool{}
	var out []string
	for _, entry := range lib.MCP {
		if !isMagpieMemoryMCP(entry.Name, entry.Args) {
			continue
		}
		for _, a := range entry.Agents {
			norm := strings.ToLower(strings.TrimSpace(a))
			if norm == "" || seen[norm] {
				continue
			}
			seen[norm] = true
			out = append(out, norm)
		}
	}
	sort.Strings(out)
	return out
}

func isMagpieMemoryMCP(name string, args []string) bool {
	if strings.EqualFold(strings.TrimSpace(name), "magpie-memory") {
		return true
	}
	hasMCP := false
	hasMem := false
	for _, a := range args {
		low := strings.ToLower(strings.TrimSpace(a))
		if low == "mcp" {
			hasMCP = true
		}
		if low == "memory" {
			hasMem = true
		}
	}
	return hasMCP && hasMem
}

func countMagpieLegacyItems(memDir string) int {
	if memDir == "" {
		return 0
	}
	if idxCount, ok := countMagpieIndexJSON(filepath.Join(memDir, "index.json")); ok {
		return idxCount
	}
	entries, err := os.ReadDir(memDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".md") {
			continue
		}
		if strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(memDir, ent.Name()))
		if err != nil {
			continue
		}
		fm, body, _ := ParseMarkdownItem(string(raw))
		if strings.TrimSpace(body) == "" {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(fm.Status))
		if st == StatusArchived || st == StatusSuperseded || st == "deleted" {
			continue
		}
		count++
	}
	return count
}

func countMagpieIndexJSON(indexPath string) (int, bool) {
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return 0, false
	}
	var idx struct {
		Files map[string]struct {
			Memory struct {
				Status string `json:"status"`
				Text   string `json:"text"`
			} `json:"memory"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil || idx.Files == nil {
		return 0, false
	}
	count := 0
	for _, f := range idx.Files {
		st := strings.ToLower(strings.TrimSpace(f.Memory.Status))
		if st == StatusArchived || st == StatusSuperseded || st == "deleted" {
			continue
		}
		if strings.TrimSpace(f.Memory.Text) == "" {
			continue
		}
		count++
	}
	return count, true
}

func harnessAliases(harnessKind string) []string {
	k := strings.ToLower(strings.TrimSpace(harnessKind))
	switch k {
	case "":
		return nil
	case "claude", "claude-code", "claude_code":
		return []string{"claude", "claude-code"}
	case "antigravity", "agy":
		return []string{"antigravity", "agy"}
	default:
		return []string{k}
	}
}
