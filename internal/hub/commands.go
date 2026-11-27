package hub

import (
	"bot-hub/internal/harness"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type MentionCandidate struct {
	Kind        string `json:"kind"` // "file" | "bot" | "agent"
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// ListBotCommands aggregates Hub built-in commands with commands/skills exposed by the bot's harness.
func (h *Hub) ListBotCommands(ctx context.Context, botID string) ([]harness.SlashCommand, error) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return nil, ErrBotNotFound
	}

	hubBuiltins := []harness.SlashCommand{
		{Name: "status", Description: "Show current session status and cost", Source: "hub"},
		{Name: "bots", Description: "List all bots", Source: "hub"},
		{Name: "mode", Description: "View or switch permission mode", ArgumentHint: "[auto|edit|ask|plan]", Source: "hub"},
		{Name: "ask", Description: "Delegate a task to another bot", ArgumentHint: "<bot> <message>", Source: "hub"},
		{Name: "reset", Description: "Archive current session and start fresh", Source: "hub"},
	}
	hubSet := map[string]harness.SlashCommand{}
	for _, c := range hubBuiltins {
		hubSet[c.Name] = c
	}

	var harnessCmds []harness.SlashCommand
	if runner, err := h.registry.Get(bot.Harness); err == nil {
		if lister, ok := runner.(harness.CommandLister); ok {
			lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if list, err := lister.ListCommands(lctx, bot); err == nil {
				harnessCmds = list
			}
		}
	}

	seen := make(map[string]bool)
	out := make([]harness.SlashCommand, 0, len(harnessCmds)+len(hubBuiltins))
	for _, c := range harnessCmds {
		name := strings.TrimPrefix(strings.TrimSpace(c.Name), "/")
		if name == "" || seen[name] {
			continue
		}
		if _, isHub := hubSet[name]; isHub {
			continue
		}
		c.Name = name
		seen[name] = true
		out = append(out, c)
	}
	for _, c := range hubBuiltins {
		if !seen[c.Name] {
			seen[c.Name] = true
			out = append(out, c)
		}
	}
	return out, nil
}

var skipMentionDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".uploads":     true,
	"__pycache__":  true,
	".next":        true,
	"dist":         true,
	"build":        true,
	".venv":        true,
}

func isWithinRoot(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func scanWorkspaceMentionFiles(workspace string) []string {
	if workspace == "" {
		return nil
	}
	wsReal, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		wsReal = filepath.Clean(workspace)
	}
	var sharedReal string
	if s, err := filepath.EvalSymlinks(filepath.Join(workspace, "_shared")); err == nil {
		sharedReal = s
	}

	const maxEntries = 2000
	const maxDepth = 6

	var files []string
	var walkDir func(absDir, relPrefix string, depth int, allowedRoot string)
	walkDir = func(absDir, relPrefix string, depth int, allowedRoot string) {
		if depth > maxDepth || len(files) >= maxEntries {
			return
		}
		entries, err := os.ReadDir(absDir)
		if err != nil {
			return
		}
		for _, ent := range entries {
			if len(files) >= maxEntries {
				return
			}
			name := ent.Name()
			if skipMentionDirs[name] {
				continue
			}
			relPath := name
			if relPrefix != "" {
				relPath = relPrefix + "/" + name
			}
			fullPath := filepath.Join(absDir, name)

			isDir := ent.IsDir()
			entryRoot := allowedRoot
			if ent.Type()&fs.ModeSymlink != 0 {
				if depth == 0 && name == "_shared" && sharedReal != "" {
					info, err := os.Stat(fullPath)
					if err != nil || !info.IsDir() {
						continue
					}
					isDir = true
					entryRoot = sharedReal
				} else {
					resolved, err := filepath.EvalSymlinks(fullPath)
					if err != nil || !isWithinRoot(allowedRoot, resolved) {
						continue
					}
					info, err := os.Stat(resolved)
					if err != nil {
						continue
					}
					isDir = info.IsDir()
				}
			}

			if isDir {
				walkDir(fullPath, relPath, depth+1, entryRoot)
			} else {
				files = append(files, relPath)
			}
		}
	}

	walkDir(workspace, "", 0, wsReal)
	return files
}

// ListBotMentions returns @-mention candidates (other bots, harness agents, and workspace files).
func (h *Hub) ListBotMentions(ctx context.Context, botID, query string, limit int) ([]MentionCandidate, error) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return nil, ErrBotNotFound
	}
	if limit <= 0 {
		limit = 30
	}
	q := strings.ToLower(strings.TrimSpace(query))

	type scored struct {
		item  MentionCandidate
		score int
	}
	var all []scored
	matchScore := func(value, label string) (int, bool) {
		if q == "" {
			return 0, true
		}
		lv := strings.ToLower(value)
		ll := strings.ToLower(label)
		base := strings.ToLower(filepath.Base(value))
		switch {
		case base == q || lv == q || ll == q:
			return 4, true
		case strings.HasPrefix(base, q) || strings.HasPrefix(ll, q):
			return 3, true
		case strings.HasPrefix(lv, q):
			return 2, true
		case strings.Contains(lv, q) || strings.Contains(ll, q):
			return 1, true
		default:
			return 0, false
		}
	}

	// 1. Other bots in the hub.
	h.lanesMu.RLock()
	for _, id := range h.order {
		if id == botID {
			continue
		}
		lane := h.lanes[id]
		if lane == nil {
			continue
		}
		b := lane.bot
		if sc, ok := matchScore(b.ID, b.Name); ok {
			all = append(all, scored{
				item: MentionCandidate{
					Kind:        "bot",
					Value:       b.ID,
					Label:       b.Name,
					Description: b.Description,
				},
				score: sc + 10,
			})
		}
	}
	h.lanesMu.RUnlock()

	// 2. Harness agents (if supported).
	if runner, err := h.registry.Get(bot.Harness); err == nil {
		if al, ok := runner.(harness.AgentLister); ok {
			actx, cancel := context.WithTimeout(ctx, 2*time.Second)
			agents, err := al.ListAgents(actx, bot)
			cancel()
			if err == nil {
				for _, a := range agents {
					if sc, ok := matchScore(a.Name, a.Name); ok {
						all = append(all, scored{
							item: MentionCandidate{
								Kind:        "agent",
								Value:       a.Name,
								Label:       a.Name,
								Description: a.Description,
							},
							score: sc + 8,
						})
					}
				}
			}
		}
	}

	// 3. Workspace files.
	for _, rel := range scanWorkspaceMentionFiles(bot.Workspace) {
		if sc, ok := matchScore(rel, filepath.Base(rel)); ok {
			all = append(all, scored{
				item: MentionCandidate{
					Kind:        "file",
					Value:       rel,
					Label:       filepath.Base(rel),
					Description: rel,
				},
				score: sc,
			})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].item.Value < all[j].item.Value
	})

	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]MentionCandidate, 0, len(all))
	for _, s := range all {
		out = append(out, s.item)
	}
	return out, nil
}
