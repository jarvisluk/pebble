package harness

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Magpie (https://usemagpie.ai) wires agent CLIs to its local model gateway by
// editing each CLI's own config. bot-hub reuses that wiring instead of
// keeping provider settings of its own: it reads magpie's model catalog and
// each harness's state, and can ask magpie to connect a harness.

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

// MagpieInstalled reports whether the magpie CLI is on PATH.
func MagpieInstalled() bool { return magpieInstalled() }

// MagpieStatus is one harness as magpie sees it.
type MagpieStatus struct {
	Installed bool   `json:"installed"` // the magpie CLI is on PATH
	Connected bool   `json:"connected"` // the harness's model is a magpie model
	Model     string `json:"model,omitempty"`
}

var magpie = struct {
	mu      sync.Mutex
	catalog []string
	fetched time.Time
}{}

var spaces = regexp.MustCompile(`\s{2,}`)

func magpieBin() string { return envOr("MAGPIE_BIN", "magpie") }

func magpieInstalled() bool {
	_, err := exec.LookPath(magpieBin())
	return err == nil
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
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	f := spaces.Split(line, -1)
	if len(f) < 2 {
		return st
	}
	model := strings.TrimSpace(f[1])
	if a.prefix != "" {
		// The CLI names magpie's provider, whether or not that model is still listed.
		st.Connected = strings.HasPrefix(model, a.prefix)
	} else {
		for _, c := range magpieCatalog(ctx) {
			if c == model {
				st.Connected = true
				break
			}
		}
	}
	if st.Connected {
		st.Model = model
	}
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
		return fmt.Errorf("a model is required")
	}
	out, err := exec.CommandContext(ctx, magpieBin(), a.agent, id).CombinedOutput()
	if err != nil {
		return fmt.Errorf("magpie %s %s: %s", a.agent, id, strings.TrimSpace(string(out)))
	}
	return nil
}
