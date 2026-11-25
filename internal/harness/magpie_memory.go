package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MagpieMemoryItem represents one memory record returned by `magpie tool memory_search`.
type MagpieMemoryItem struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
	Text      string   `json:"text"`
}

// MagpieMemorySemanticStatus describes whether Magpie Memory's local semantic embedding search is enabled and ready.
type MagpieMemorySemanticStatus struct {
	Enabled    bool   `json:"enabled"`
	ModelReady bool   `json:"modelReady"`
	IndexReady bool   `json:"indexReady"`
	Active     bool   `json:"active"`
	Model      string `json:"model,omitempty"`
}

// MagpieMemoryList is the response returned for memory queries.
type MagpieMemoryList struct {
	Installed bool                        `json:"installed"`
	Available bool                        `json:"available"`
	Error     string                      `json:"error,omitempty"`
	Semantic  *MagpieMemorySemanticStatus `json:"semantic,omitempty"`
	Items     []MagpieMemoryItem          `json:"items"`
}

// MagpieMemorySaveReq describes a memory create/update request passed to `magpie tool memory_save`.
type MagpieMemorySaveReq struct {
	ID      string   `json:"id,omitempty"`
	Scope   string   `json:"scope,omitempty"` // "global" (default) or "project"
	Project string   `json:"project,omitempty"`
	BotID   string   `json:"botId,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Text    string   `json:"text"`
	Why     string   `json:"why,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

var (
	memoryEntryRe = regexp.MustCompile(`(?s)^\[([^\]]+)\]\s+(.+)$`)
	isoPrefixRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}|$)`)
)

type magpieToolEnvelope struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
}

func runMagpieTool(ctx context.Context, toolName string, args ...string) (*magpieToolEnvelope, string, error) {
	if !magpieInstalled() {
		return nil, "", fmt.Errorf("magpie is not installed")
	}
	cmdArgs := append([]string{"tool", toolName, "--json"}, args...)
	out, err := runMagpieCombined(ctx, cmdArgs...)
	raw := strings.TrimSpace(string(out))

	var env magpieToolEnvelope
	// magpie tool may append a trailing warning line after JSON; extract the first JSON object if present.
	if start := strings.IndexByte(raw, '{'); start >= 0 {
		dec := json.NewDecoder(strings.NewReader(raw[start:]))
		if decErr := dec.Decode(&env); decErr == nil {
			var textParts []string
			for _, c := range env.Content {
				if strings.TrimSpace(c.Text) != "" {
					textParts = append(textParts, c.Text)
				}
			}
			joined := strings.TrimSpace(strings.Join(textParts, "\n"))
			if env.IsError {
				if joined == "" {
					joined = redactSecrets(raw)
				}
				return &env, joined, fmt.Errorf("%s", joined)
			}
			if err != nil {
				return &env, joined, fmt.Errorf("%s", redactSecrets(raw))
			}
			return &env, joined, nil
		}
	}
	if err != nil {
		msg := redactSecrets(raw)
		if msg == "" {
			msg = err.Error()
		}
		return nil, "", fmt.Errorf("magpie tool %s: %s", toolName, msg)
	}
	return &env, raw, nil
}

func botMemoryTag(botID string) string {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return ""
	}
	return "bot:" + botID
}

func normalizeMemoryTags(tags []string, botID string) []string {
	seen := make(map[string]bool, len(tags)+1)
	var out []string
	add := func(t string) {
		t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		out = append(out, t)
	}
	for _, t := range tags {
		add(t)
	}
	if bt := botMemoryTag(botID); bt != "" {
		add(bt)
	}
	return out
}

func hasMemoryTag(tags []string, want string) bool {
	if want == "" {
		return true
	}
	for _, t := range tags {
		if strings.EqualFold(strings.TrimPrefix(t, "#"), want) {
			return true
		}
	}
	return false
}

func parseMemorySearchOutput(env *magpieToolEnvelope, text string) []MagpieMemoryItem {
	if env != nil && len(env.StructuredContent) > 0 {
		var sc struct {
			Items    []MagpieMemoryItem `json:"items"`
			Memories []MagpieMemoryItem `json:"memories"`
		}
		if json.Unmarshal(env.StructuredContent, &sc) == nil {
			if len(sc.Items) > 0 {
				return sc.Items
			}
			if len(sc.Memories) > 0 {
				return sc.Memories
			}
		}
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "Nothing remembered") {
		return []MagpieMemoryItem{}
	}

	lines := strings.Split(trimmed, "\n")
	var entries []string
	for _, line := range lines {
		lineTrim := strings.TrimSpace(line)
		if strings.HasPrefix(lineTrim, "(looked in:") && strings.HasSuffix(lineTrim, ")") {
			continue
		}
		if memoryEntryRe.MatchString(line) {
			entries = append(entries, line)
		} else if len(entries) > 0 {
			entries[len(entries)-1] += "\n" + line
		}
	}

	items := make([]MagpieMemoryItem, 0, len(entries))
	for _, entry := range entries {
		if item, ok := parseMemoryEntryLine(entry); ok {
			items = append(items, item)
		}
	}
	return items
}

func parseMemoryEntryLine(entry string) (MagpieMemoryItem, bool) {
	m := memoryEntryRe.FindStringSubmatch(strings.TrimSpace(entry))
	if len(m) != 3 {
		return MagpieMemoryItem{}, false
	}
	item := MagpieMemoryItem{ID: strings.TrimSpace(m[1])}
	rest := m[2]

	splitIdx := strings.Index(rest, ": ")
	if splitIdx < 0 {
		item.Text = strings.TrimSpace(rest)
		return item, true
	}
	header := rest[:splitIdx]
	item.Text = strings.TrimSpace(rest[splitIdx+2:])

	parts := strings.Split(header, ",")
	for idx, rawPart := range parts {
		p := strings.TrimSpace(rawPart)
		if p == "" {
			continue
		}
		switch {
		case idx == 0:
			item.Kind = p
		case strings.HasPrefix(p, "#"):
			for _, tok := range strings.Fields(p) {
				if tag := strings.TrimPrefix(strings.TrimSpace(tok), "#"); tag != "" {
					item.Tags = append(item.Tags, tag)
				}
			}
		case isoPrefixRe.MatchString(p):
			item.UpdatedAt = p
		case item.Scope == "":
			item.Scope = p
		}
	}
	return item, true
}

const magpieSemanticModelName = "paraphrase-multilingual-MiniLM-L12-v2"

func parseMagpieDataDirs(raw string) (filesDir, cacheDir string) {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "files:"); ok {
			filesDir = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(trimmed, "cache:"); ok {
			cacheDir = strings.TrimSpace(after)
		}
	}
	return filesDir, cacheDir
}

func defaultMagpieDataDirs() (filesDir, cacheDir string) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", ""
	}
	return filepath.Join(home, ".config", "magpie"), filepath.Join(home, ".cache", "magpie")
}

func magpieDataDirs(ctx context.Context) (string, string) {
	defFiles, defCache := defaultMagpieDataDirs()
	if !magpieInstalled() {
		return defFiles, defCache
	}
	pair := dataDirsCache.get(func() ([2]string, bool) {
		out, err := runMagpieOutput(ctx, "data")
		if err != nil {
			return [2]string{defFiles, defCache}, false
		}
		f, c := parseMagpieDataDirs(string(out))
		if f == "" {
			f = defFiles
		}
		if c == "" {
			c = defCache
		}
		return [2]string{f, c}, true
	})
	if pair[0] == "" {
		pair[0] = defFiles
	}
	if pair[1] == "" {
		pair[1] = defCache
	}
	return pair[0], pair[1]
}

func detectMagpieMemorySemanticStatus(filesDir, cacheDir string) *MagpieMemorySemanticStatus {
	st := &MagpieMemorySemanticStatus{}
	if filesDir != "" {
		if data, err := os.ReadFile(filepath.Join(filesDir, "memory-semantic.json")); err == nil {
			var cfg struct {
				On bool `json:"on"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				st.Enabled = cfg.On
			}
		}
	}
	if cacheDir != "" {
		modelDir := filepath.Join(cacheDir, magpieSemanticModelName)
		if matches, err := filepath.Glob(filepath.Join(modelDir, "*", "model_int8.onnx")); err == nil && len(matches) > 0 {
			st.ModelReady = true
		}
		if matches, err := filepath.Glob(filepath.Join(modelDir, "*", "vectors.bin")); err == nil && len(matches) > 0 {
			st.IndexReady = true
		}
	}
	if st.Enabled || st.ModelReady {
		st.Model = magpieSemanticModelName
	}
	st.Active = st.Enabled && st.ModelReady
	return st
}

func getMagpieMemorySemanticStatus(ctx context.Context) *MagpieMemorySemanticStatus {
	filesDir, cacheDir := magpieDataDirs(ctx)
	return detectMagpieMemorySemanticStatus(filesDir, cacheDir)
}

// MagpieMemorySearch queries memories via `magpie tool memory_search`.
// When botID is non-empty, it scopes to projectDir AND enforces tag `bot:<botID>`
// so multiple bots sharing a git repo root remain isolated.
func MagpieMemorySearch(ctx context.Context, scope, projectDir, botID, query, kind string, limit int) MagpieMemoryList {
	if !magpieInstalled() {
		return MagpieMemoryList{Installed: false, Available: false, Items: []MagpieMemoryItem{}}
	}
	semantic := getMagpieMemorySemanticStatus(ctx)
	scope = strings.TrimSpace(scope)
	botID = strings.TrimSpace(botID)
	if botID != "" {
		scope = "project"
		if strings.TrimSpace(projectDir) == "" {
			return MagpieMemoryList{Installed: true, Available: true, Semantic: semantic, Items: []MagpieMemoryItem{}}
		}
		if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
			return MagpieMemoryList{Installed: true, Available: true, Semantic: semantic, Items: []MagpieMemoryItem{}}
		}
	} else if scope != "all" {
		scope = "global"
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 50 {
		limit = 50
	}

	args := []string{"--scope", scope, "--limit", strconv.Itoa(limit)}
	if scope == "project" && strings.TrimSpace(projectDir) != "" {
		args = append(args, "--project", projectDir)
	}
	if q := strings.TrimSpace(query); q != "" {
		args = append(args, "--query", q)
	}
	if k := strings.TrimSpace(kind); k != "" {
		args = append(args, "--kind", k)
	}
	botTag := botMemoryTag(botID)
	if botTag != "" {
		args = append(args, "--tags", botTag)
	}

	env, text, err := runMagpieTool(ctx, "memory_search", args...)
	if err != nil {
		return MagpieMemoryList{
			Installed: true,
			Available: false,
			Error:     err.Error(),
			Semantic:  semantic,
			Items:     []MagpieMemoryItem{},
		}
	}
	items := parseMemorySearchOutput(env, text)
	if botTag != "" {
		filtered := make([]MagpieMemoryItem, 0, len(items))
		for _, it := range items {
			if hasMemoryTag(it.Tags, botTag) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	return MagpieMemoryList{
		Installed: true,
		Available: true,
		Semantic:  semantic,
		Items:     items,
	}
}

// MagpieMemorySave saves or updates a memory via `magpie tool memory_save`.
func MagpieMemorySave(ctx context.Context, req MagpieMemorySaveReq) (string, error) {
	if !magpieInstalled() {
		return "", fmt.Errorf("magpie is not installed")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return "", fmt.Errorf("memory content cannot be empty")
	}

	scope := strings.TrimSpace(req.Scope)
	botID := strings.TrimSpace(req.BotID)
	if botID != "" {
		scope = "project"
		if strings.TrimSpace(req.Project) == "" {
			return "", fmt.Errorf("bot workspace is required")
		}
		if err := os.MkdirAll(req.Project, 0o755); err != nil {
			return "", err
		}
	}
	if scope == "" {
		scope = "global"
	}

	args := []string{"--text", text, "--scope", scope}
	if scope == "project" && strings.TrimSpace(req.Project) != "" {
		args = append(args, "--project", req.Project)
	}
	if id := strings.TrimSpace(req.ID); id != "" {
		args = append(args, "--id", id)
	}
	if k := strings.TrimSpace(req.Kind); k != "" {
		args = append(args, "--kind", k)
	}
	if w := strings.TrimSpace(req.Why); w != "" {
		args = append(args, "--why", w)
	}
	for _, t := range normalizeMemoryTags(req.Tags, botID) {
		args = append(args, "--tags", t)
	}

	_, msg, err := runMagpieTool(ctx, "memory_save", args...)
	if err != nil {
		return "", err
	}
	return msg, nil
}

// MagpieMemoryForget removes/forgets a memory via `magpie tool memory_forget`.
func MagpieMemoryForget(ctx context.Context, id string) (string, error) {
	if !magpieInstalled() {
		return "", fmt.Errorf("magpie is not installed")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("memory id is required")
	}
	_, msg, err := runMagpieTool(ctx, "memory_forget", "--id", id)
	if err != nil {
		return "", err
	}
	return msg, nil
}
