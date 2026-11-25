package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Tools      []string      `json:"tools,omitempty"`
	DurationMs int64         `json:"durationMs"`
	Duration   time.Duration `json:"-"`
	Usage      Usage         `json:"usage,omitempty"`
}

type QuestionOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type QuestionField struct {
	Key         string           `json:"key"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Type        string           `json:"type"`
	Required    bool             `json:"required,omitempty"`
	Placeholder string           `json:"placeholder,omitempty"`
	Default     any              `json:"default,omitempty"`
	Options     []QuestionOption `json:"options,omitempty"`
	Custom      bool             `json:"custom,omitempty"`
}

type QuestionRequest struct {
	ID     string          `json:"id"`
	Title  string          `json:"title,omitempty"`
	Fields []QuestionField `json:"fields"`
}

type QuestionResponse struct {
	Answers   map[string]any `json:"answers,omitempty"`
	Cancelled bool           `json:"cancelled,omitempty"`
	Message   string         `json:"message,omitempty"`
}

type SlashCommand struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	ArgumentHint string `json:"argumentHint,omitempty"`
	Source       string `json:"source"` // "harness" | "skill" | "workflow" | "hub"
}

type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Attachment struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"` // "image" | "file"
	Name          string `json:"name"`
	MimeType      string `json:"mimeType,omitempty"`
	Path          string `json:"path"`
	WorkspacePath string `json:"workspacePath,omitempty"`
}

type RunOptions struct {
	Bot                      config.BotConfig
	Prompt                   string
	Attachments              []Attachment
	SessionID                string
	Ephemeral                bool
	PermissionMode           string
	PlatformInstructionsPath string
	// MCPCommand/MCPEnv describe the bot-hub MCP server for harnesses that
	// take MCP config per process rather than from the workspace.
	MCPCommand   []string
	MCPEnv       map[string]string
	ExtraEnv     map[string]string
	OnText       func(chunk string)
	OnToolUse    func(toolName string)
	OnQuestion   func(ctx context.Context, req QuestionRequest) (QuestionResponse, error)
	OnPermission func(ctx context.Context, req PermissionRequest) (PermissionResponse, error)
}

type PermissionCapabilities struct {
	Modes               []string `json:"modes"`
	InteractiveApproval bool     `json:"interactiveApproval"`
	SupportsAllowAlways bool     `json:"supportsAllowAlways"`
}

// PermissionCapable is optionally implemented by harnesses that support
// non-auto permission modes or interactive tool approvals.
type PermissionCapable interface {
	PermissionCapabilities() PermissionCapabilities
}

// CapabilitiesOf returns the permission capabilities of h, defaulting to auto-only.
func CapabilitiesOf(h Harness) PermissionCapabilities {
	if pc, ok := h.(PermissionCapable); ok {
		caps := pc.PermissionCapabilities()
		if len(caps.Modes) == 0 {
			caps.Modes = []string{config.PermissionModeAuto}
		}
		return caps
	}
	return PermissionCapabilities{Modes: []string{config.PermissionModeAuto}}
}

type PermissionOption struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // "allow_once" | "allow_always" | "reject"
	Label string `json:"label"`
}

type PermissionRequest struct {
	ID       string             `json:"id"`
	ToolName string             `json:"toolName"`
	Kind     string             `json:"kind"` // "shell" | "write" | "read" | "mcp" | "network" | "other"
	Title    string             `json:"title,omitempty"`
	Summary  string             `json:"summary,omitempty"`
	RawInput map[string]any     `json:"rawInput,omitempty"`
	Options  []PermissionOption `json:"options,omitempty"`
}

type PermissionResponse struct {
	Decision string `json:"decision"` // "allow_once" | "allow_always" | "reject" | "cancelled"
	OptionID string `json:"optionId,omitempty"`
	Reason   string `json:"reason,omitempty"`
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

// CommandLister is optionally implemented by harnesses that expose slash commands or skills.
type CommandLister interface {
	ListCommands(ctx context.Context, bot config.BotConfig) ([]SlashCommand, error)
}

// AgentLister is optionally implemented by harnesses that expose subagents.
type AgentLister interface {
	ListAgents(ctx context.Context, bot config.BotConfig) ([]AgentInfo, error)
}

// Reloader is optionally implemented by harnesses whose cached runtime process
// or catalog should be restarted after CLI model/provider changes.
type Reloader interface {
	Reload()
}

// ParseSlashCommand extracts a leading "/<name> [args]" from the first line of prompt.
func ParseSlashCommand(prompt string) (name, args string, ok bool) {
	trimmed := strings.TrimSpace(prompt)
	if !strings.HasPrefix(trimmed, "/") || len(trimmed) < 2 {
		return "", "", false
	}
	rest := trimmed[1:]
	if rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '/' {
		return "", "", false
	}
	idx := strings.IndexAny(rest, " \t\r\n")
	if idx < 0 {
		return rest, "", true
	}
	return rest[:idx], strings.TrimSpace(rest[idx+1:]), true
}

// AppendAttachmentContext appends workspace paths for attached files so any harness
// can read them directly from disk.
func AppendAttachmentContext(prompt string, atts []Attachment) string {
	trimmed := strings.TrimSpace(prompt)
	if len(atts) == 0 {
		return prompt
	}
	if trimmed == "" {
		trimmed = "Please check the attached file(s)."
	}
	var paths []string
	for _, a := range atts {
		p := a.WorkspacePath
		if p == "" {
			p = a.Path
		}
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return trimmed
	}
	return fmt.Sprintf("%s\n\n[Attached files in workspace: %s]", trimmed, strings.Join(paths, ", "))
}

// parseFrontmatter extracts name and description from YAML frontmatter if present.
func parseFrontmatter(content string) (name, desc string) {
	s := strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(s, "---") {
		return "", ""
	}
	rest := s[3:]
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	} else {
		return "", ""
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", ""
	}
	block := rest[:end]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := strings.Cut(line, ":"); ok {
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(v)
			val = strings.Trim(val, `"'`)
			switch key {
			case "name":
				name = val
			case "description":
				desc = val
			}
		}
	}
	return name, desc
}

const frontmatterHeadBytes = 4096

// readFrontmatterHead reads at most maxBytes from path so command/skill
// scanning never loads full markdown bodies into memory.
func readFrontmatterHead(path string, maxBytes int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// scanMarkdownCommands scans top-level *.md files in dir as slash commands.
func scanMarkdownCommands(dir, source string) []SlashCommand {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []SlashCommand
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".md") {
			continue
		}
		base := strings.TrimSuffix(ent.Name(), filepath.Ext(ent.Name()))
		if base == "" || strings.HasPrefix(base, ".") {
			continue
		}
		var desc string
		if raw, err := readFrontmatterHead(filepath.Join(dir, ent.Name()), frontmatterHeadBytes); err == nil {
			n, d := parseFrontmatter(raw)
			if n != "" {
				base = n
			}
			desc = d
		}
		out = append(out, SlashCommand{
			Name:        base,
			Description: desc,
			Source:      source,
		})
	}
	return out
}

// scanSkillDirs scans */SKILL.md inside dir as skill commands (following symlinks to directories).
func scanSkillDirs(dir string) []SlashCommand {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []SlashCommand
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		isDir := ent.IsDir()
		if !isDir && ent.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, ent.Name())); err == nil {
				isDir = info.IsDir()
			}
		}
		if !isDir {
			continue
		}
		skillPath := filepath.Join(dir, ent.Name(), "SKILL.md")
		raw, err := readFrontmatterHead(skillPath, frontmatterHeadBytes)
		if err != nil {
			continue
		}
		name, desc := parseFrontmatter(raw)
		if name == "" {
			name = ent.Name()
		}
		out = append(out, SlashCommand{
			Name:        name,
			Description: desc,
			Source:      "skill",
		})
	}
	return out
}

// scanSharedSkills scans the cross-tool .agents/skills directories in workspace and $HOME.
func scanSharedSkills(workspace string) []SlashCommand {
	var out []SlashCommand
	if workspace != "" {
		out = append(out, scanSkillDirs(filepath.Join(workspace, ".agents", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, scanSkillDirs(filepath.Join(home, ".agents", "skills"))...)
	}
	return out
}

// parseTOMLTopFields extracts top-level name and description string fields before any [section].
func parseTOMLTopFields(content string) (name, desc string) {
	s := strings.TrimPrefix(content, "\ufeff")
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(v)
			if i := strings.Index(val, "#"); i >= 0 && !strings.HasPrefix(val, `"`) && !strings.HasPrefix(val, `'`) {
				val = strings.TrimSpace(val[:i])
			}
			val = strings.Trim(val, `"'`)
			switch key {
			case "name":
				name = val
			case "description":
				desc = val
			}
		}
	}
	return name, desc
}

// scanAgentsDir scans top-level *.md and *.toml agent definitions in dir.
func scanAgentsDir(dir string) []AgentInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []AgentInfo
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		isDir := ent.IsDir()
		if !isDir && ent.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".md" && ext != ".toml" {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base == "" {
			continue
		}
		var parsedName, desc string
		if raw, err := readFrontmatterHead(filepath.Join(dir, name), frontmatterHeadBytes); err == nil {
			if ext == ".md" {
				parsedName, desc = parseFrontmatter(raw)
			} else {
				parsedName, desc = parseTOMLTopFields(raw)
			}
			if parsedName != "" {
				base = parsedName
			}
		}
		out = append(out, AgentInfo{
			Name:        base,
			Description: desc,
		})
	}
	return out
}

func mergeAgents(groups ...[]AgentInfo) []AgentInfo {
	seen := make(map[string]AgentInfo)
	var order []string
	for _, g := range groups {
		for _, a := range g {
			name := strings.TrimSpace(a.Name)
			if name == "" {
				continue
			}
			a.Name = name
			if prev, exists := seen[name]; !exists {
				seen[name] = a
				order = append(order, name)
			} else if prev.Description == "" && a.Description != "" {
				seen[name] = a
			}
		}
	}
	out := make([]AgentInfo, 0, len(order))
	for _, name := range order {
		out = append(out, seen[name])
	}
	return out
}

func mergeSlashCommands(groups ...[]SlashCommand) []SlashCommand {
	seen := make(map[string]SlashCommand)
	var order []string
	for _, g := range groups {
		for _, c := range g {
			name := strings.TrimPrefix(strings.TrimSpace(c.Name), "/")
			if name == "" {
				continue
			}
			c.Name = name
			if prev, exists := seen[name]; !exists {
				seen[name] = c
				order = append(order, name)
			} else if prev.Description == "" && c.Description != "" {
				seen[name] = c
			}
		}
	}
	out := make([]SlashCommand, 0, len(order))
	for _, name := range order {
		out = append(out, seen[name])
	}
	return out
}

type Registry struct {
	items map[string]Harness
}

func NewDefaultRegistry(cfg *config.HubConfig) *Registry {
	r := &Registry{items: make(map[string]Harness)}
	r.Register(NewOpenCodeHarness(cfg.OpenCodeServerURL))
	r.Register(NewCodeBuddyHarness())
	r.Register(NewQoderHarness())
	r.Register(NewCodexHarness())
	r.Register(NewClaudeHarness())
	r.Register(NewGrokHarness())
	r.Register(NewTraeHarness())
	r.Register(NewPiHarness())
	r.Register(NewDshHarness())
	r.Register(NewAgyHarness())
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
var harnessOrder = map[string]int{
	"opencode":  1,
	"codebuddy": 2,
	"qoder":     3,
	"codex":     4,
	"claude":    5,
	"grok":      6,
	"trae":      7,
	"pi":        8,
	"dsh":       9,
	"agy":       10,
}

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
