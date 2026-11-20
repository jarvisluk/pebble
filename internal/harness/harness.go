package harness

import (
	"context"
	"fmt"
	"sort"
	"time"

	"bot-hub/internal/config"
)

type Usage struct {
	CostUSD         float64 `json:"costUsd,omitempty"`
	InputTokens     int     `json:"inputTokens,omitempty"`
	OutputTokens    int     `json:"outputTokens,omitempty"`
	CacheReadTokens int     `json:"cacheReadTokens,omitempty"`
}

type TurnResult struct {
	BotID      string        `json:"botId"`
	SessionID  string        `json:"sessionId"`
	Text       string        `json:"text"`
	DurationMs int64         `json:"durationMs"`
	Duration   time.Duration `json:"-"`
	Usage      Usage         `json:"usage,omitempty"`
}

type RunOptions struct {
	Bot       config.BotConfig
	Prompt    string
	SessionID string
	// MCPCommand/MCPEnv describe the bot-hub MCP server for harnesses that
	// take MCP config per process rather than from the workspace.
	MCPCommand []string
	MCPEnv     map[string]string
	ExtraEnv   map[string]string
	OnText     func(chunk string)
	OnToolUse  func(toolName string)
}

// Harness runs agent turns. Implementations keep a long-lived connection to
// the agent runtime; RunTurn must honour ctx cancellation as an interrupt.
type Harness interface {
	Kind() string
	Label() string
	RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error)
	// Models lists model IDs the harness can use (best effort, may be empty).
	Models(ctx context.Context) ([]string, error)
	Close()
}

type Registry struct {
	items map[string]Harness
}

func NewDefaultRegistry(cfg *config.HubConfig) *Registry {
	r := &Registry{items: make(map[string]Harness)}
	r.Register(NewOpenCodeHarness(cfg.OpenCodeServerURL))
	r.Register(NewPiHarness())
	r.Register(NewDshHarness())
	r.Register(NewClaudeHarness())
	return r
}

func NewRegistry() *Registry {
	return &Registry{items: make(map[string]Harness)}
}

func (r *Registry) Register(h Harness) {
	r.items[h.Kind()] = h
}

func (r *Registry) Get(kind string) (Harness, error) {
	if kind == "" {
		kind = "opencode"
	}
	h, ok := r.items[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported harness %q", kind)
	}
	return h, nil
}

// harnessOrder is the display order; unknown kinds sort after, by name.
var harnessOrder = map[string]int{"opencode": 1, "pi": 2, "dsh": 3, "claude": 4}

// All returns the registered harnesses in display order.
func (r *Registry) All() []Harness {
	out := make([]Harness, 0, len(r.items))
	for _, h := range r.items {
		out = append(out, h)
	}
	rank := func(k string) int {
		if n, ok := harnessOrder[k]; ok {
			return n
		}
		return len(harnessOrder) + 1
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := rank(out[i].Kind()), rank(out[j].Kind())
		if ri != rj {
			return ri < rj
		}
		return out[i].Kind() < out[j].Kind()
	})
	return out
}

func (r *Registry) Close() {
	for _, h := range r.items {
		h.Close()
	}
}
