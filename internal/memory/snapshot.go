package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	snapshotOpenTag  = `<pebble_memory_snapshot kind="reference-data">`
	snapshotCloseTag = `</pebble_memory_snapshot>`
)

// BuildSnapshot renders a deterministic, prompt-cache-friendly memory snapshot for session startup.
// It combines _shared/USER.md, global MEMORY.md, bot MEMORY.md, recent 48h daily headings, and
// Magpie coexistence boundary guidance when applicable.
func (s *Store) BuildSnapshot(ctx context.Context, opts SnapshotOpts) (string, error) {
	if s == nil {
		return "", nil
	}
	if s.db != nil {
		_, _ = s.SyncFromDisk(ctx)
	}

	cfg, _ := s.runtimeConfig()
	maxChars := opts.MaxChars
	if maxChars <= 0 {
		maxChars = cfg.SnapshotMaxChars
	}
	if maxChars <= 0 {
		maxChars = DefaultSnapshotMaxChars
	}
	now := opts.Now
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()

	botID := strings.TrimSpace(opts.BotID)
	coexistMode := NormalizeCoexistence(firstNonEmpty(opts.Coexistence, cfg.Coexistence))
	harnessHasMagpie := s.HarnessHasMagpieMemory(ctx, opts.Harness)
	coexistActive := coexistMode == CoexistenceAuto && harnessHasMagpie

	// Allocate deterministic per-section character budgets.
	userBudget := max(400, maxChars/5)
	globalBudget := max(600, maxChars*3/10)
	botBudget := max(800, maxChars*3/10)
	dailyBudget := max(400, maxChars/6)

	var sections []string

	// 1. User Profile (_shared/USER.md) - read-only.
	if userMD := s.readSharedUserMD(); userMD != "" {
		sections = append(sections, "## User Profile\n"+truncateSnapshotSection(userMD, userBudget))
	}

	// 2. Global Memory (_shared/memory/MEMORY.md, or fallback summary of active global items).
	globalSummary := s.renderGlobalSnapshotSection(ctx, coexistActive)
	if globalSummary != "" {
		sections = append(sections, "## Global Memory\n"+truncateSnapshotSection(globalSummary, globalBudget))
	}

	// 3. Bot Memory (<bot>/memory/MEMORY.md, or fallback summary of active bot items).
	if botID != "" && validateBotID(botID) == nil {
		botSummary := s.renderBotSnapshotSection(ctx, botID)
		if botSummary != "" {
			sections = append(sections, fmt.Sprintf("## Bot Memory (%s)\n%s", botID, truncateSnapshotSection(botSummary, botBudget)))
		}
	}

	// 4. Recent 48h Daily Log Headings.
	dailyHeadings := s.collectRecentDailyHeadings(botID, now)
	if dailyHeadings != "" {
		sections = append(sections, "## Recent Daily Logs (48h)\n"+truncateSnapshotSection(dailyHeadings, dailyBudget))
	}

	// 5. Magpie single-source coexistence directive (concise 2-line directive).
	var boundarySection string
	if coexistActive {
		boundarySection = buildCoexistenceDirective(opts.PrivateSessions)
	} else if opts.PrivateSessions && harnessHasMagpie {
		boundarySection = "## Memory Boundary\n- This bot has privateSessions enabled: store all memories exclusively in Pebble and never write to external `magpie-memory`."
	}

	if len(sections) == 0 && boundarySection == "" {
		return "", nil
	}

	// Enforce total MaxChars budget while always preserving the boundary directive and closing tags.
	wrapperOverhead := len([]rune(snapshotOpenTag)) + len([]rune(snapshotCloseTag)) + 4
	boundaryReserve := 0
	if boundarySection != "" {
		boundaryReserve = len([]rune(boundarySection)) + 2
	}
	bodyBudget := maxChars - wrapperOverhead - boundaryReserve
	if bodyBudget < 0 {
		bodyBudget = 0
	}

	body := strings.Join(sections, "\n\n")
	if bodyBudget > 0 && len([]rune(body)) > bodyBudget {
		body = truncateSnapshotSection(body, bodyBudget)
	} else if bodyBudget == 0 {
		body = ""
	}

	var parts []string
	if strings.TrimSpace(body) != "" {
		parts = append(parts, strings.TrimSpace(body))
	}
	if boundarySection != "" {
		parts = append(parts, boundarySection)
	}

	return snapshotOpenTag + "\n" + strings.Join(parts, "\n\n") + "\n" + snapshotCloseTag, nil
}

func (s *Store) readSharedUserMD() string {
	p := filepath.Join(s.workspacesDir, "_shared", "USER.md")
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(raw), "\r\n", "\n"))
}

func (s *Store) renderGlobalSnapshotSection(ctx context.Context, omitMagpieImport bool) string {
	idxDoc, err := s.ReadIndex(ctx, ScopeGlobal, "")
	if err == nil && strings.TrimSpace(idxDoc.Content) != "" {
		content := strings.TrimSpace(idxDoc.Content)
		if omitMagpieImport {
			content = s.filterMagpieImportedLines(ctx, content)
		}
		if strings.TrimSpace(content) != "" {
			return strings.TrimSpace(content)
		}
	}
	// Fallback: summarize active global items when MEMORY.md is absent or empty.
	return s.summarizeActiveItemsForSnapshot(ctx, ScopeGlobal, "", omitMagpieImport)
}

func (s *Store) renderBotSnapshotSection(ctx context.Context, botID string) string {
	idxDoc, err := s.ReadIndex(ctx, ScopeBot, botID)
	if err == nil && strings.TrimSpace(idxDoc.Content) != "" {
		return strings.TrimSpace(idxDoc.Content)
	}
	return s.summarizeActiveItemsForSnapshot(ctx, ScopeBot, botID, false)
}

func (s *Store) summarizeActiveItemsForSnapshot(ctx context.Context, scope, botID string, omitMagpieImport bool) string {
	if s.db == nil {
		return ""
	}
	where := []string{"doc_type = ?", "scope = ?", "status = ?"}
	args := []any{DocTypeItem, scope, StatusActive}
	if scope == ScopeBot {
		where = append(where, "bot_id = ?")
		args = append(args, botID)
	}
	if omitMagpieImport {
		where = append(where, "source_author != ?")
		args = append(args, AuthorMagpieImport)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT doc_id, kind, title, pin
		FROM memory_docs
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY pin DESC, updated_at DESC, doc_id ASC
		LIMIT 30
	`, args...)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var id, kind, title string
		var pin int
		if rows.Scan(&id, &kind, &title, &pin) != nil {
			continue
		}
		prefix := "- "
		if pin != 0 {
			prefix = "- [pinned] "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s (%s)", prefix, kind, title, id))
	}
	return strings.Join(lines, "\n")
}

func (s *Store) filterMagpieImportedLines(ctx context.Context, content string) string {
	if s.db == nil || strings.TrimSpace(content) == "" {
		return content
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT doc_id, path, title
		FROM memory_docs
		WHERE doc_type = ? AND scope = ? AND source_author = ?
	`, DocTypeItem, ScopeGlobal, AuthorMagpieImport)
	if err != nil {
		return content
	}
	defer rows.Close()

	var needles []string
	for rows.Next() {
		var id, relPath, title string
		if rows.Scan(&id, &relPath, &title) != nil {
			continue
		}
		if id != "" {
			needles = append(needles, strings.ToLower(id))
		}
		if base := filepath.Base(relPath); base != "" && base != "." {
			needles = append(needles, strings.ToLower(base))
		}
		if t := strings.TrimSpace(title); len([]rune(t)) >= 6 {
			needles = append(needles, strings.ToLower(t))
		}
	}
	if len(needles) == 0 {
		return content
	}

	rawLines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			kept = append(kept, line)
			continue
		}
		low := strings.ToLower(line)
		matched := false
		for _, n := range needles {
			if strings.Contains(low, n) {
				matched = true
				break
			}
		}
		if !matched {
			kept = append(kept, line)
		}
	}
	return pruneEmptyMarkdownSections(kept)
}

func pruneEmptyMarkdownSections(lines []string) string {
	var out []string
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") {
			// Check if any non-empty, non-heading content follows before next heading.
			hasContent := false
			for j := i + 1; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if t == "" {
					continue
				}
				if strings.HasPrefix(t, "#") {
					break
				}
				hasContent = true
				break
			}
			if !hasContent {
				continue
			}
		}
		out = append(out, lines[i])
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func (s *Store) collectRecentDailyHeadings(botID string, now time.Time) string {
	// Include today, yesterday, and 2 days ago to reliably span a rolling 48h window across timezones.
	dateSet := []string{
		now.Format("2006-01-02"),
		now.Add(-24 * time.Hour).Format("2006-01-02"),
		now.Add(-48 * time.Hour).Format("2006-01-02"),
	}
	cutoff := now.Add(-48 * time.Hour)

	type target struct {
		label string
		dir   string
	}
	var targets []target
	if botID != "" && validateBotID(botID) == nil {
		targets = append(targets, target{
			label: botID,
			dir:   filepath.Join(s.workspacesDir, botID, "memory", "daily"),
		})
	}
	targets = append(targets, target{
		label: "global",
		dir:   filepath.Join(s.workspacesDir, "_shared", "memory", "daily"),
	})

	var blocks []string
	for _, tgt := range targets {
		sortDates := append([]string(nil), dateSet...)
		sort.Strings(sortDates)
		for i := len(sortDates) - 1; i >= 0; i-- {
			d := sortDates[i]
			fp := filepath.Join(tgt.dir, d+".md")
			info, err := os.Stat(fp)
			if err != nil || info.IsDir() {
				continue
			}
			// Keep file if its date is today/yesterday OR its mtime is within 48h.
			if d == dateSet[2] && info.ModTime().UTC().Before(cutoff) {
				continue
			}
			raw, err := os.ReadFile(fp)
			if err != nil {
				continue
			}
			headings := extractMarkdownHeadings(string(raw))
			if len(headings) == 0 {
				continue
			}
			blocks = append(blocks, fmt.Sprintf("- %s (%s): %s", d, tgt.label, strings.Join(headings, " | ")))
		}
	}
	return strings.Join(blocks, "\n")
}

func extractMarkdownHeadings(raw string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
			h := strings.TrimSpace(strings.TrimLeft(trimmed, "# "))
			if h != "" {
				out = append(out, h)
			}
		}
	}
	return out
}

func buildCoexistenceDirective(privateSessions bool) string {
	line1 := "- Use Pebble `save_memory`/`recall_memory` for bot and team memory; treat ambient Magpie memory as read-only and never double-write."
	line2 := "- Keep bot-scoped notes in Pebble; private bots must never write to external `magpie-memory`."
	if privateSessions {
		line2 = "- This bot has `privateSessions=true`: never write any note or summary to external `magpie-memory`."
	}
	return "## Memory Boundary\n" + line1 + "\n" + line2
}

func truncateSnapshotSection(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	const marker = "\n...[truncated]"
	cut := maxRunes - len([]rune(marker))
	if cut <= 0 {
		return string(runes[:maxRunes])
	}
	trimmed := string(runes[:cut])
	if nl := strings.LastIndex(trimmed, "\n"); nl > cut/2 {
		trimmed = trimmed[:nl]
	}
	return strings.TrimRight(trimmed, " \t\n") + marker
}
