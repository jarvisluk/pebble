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

// RebuildIndex deterministically rebuilds global or bot MEMORY.md from active items,
// strictly enforcing line and byte budgets (100 lines / 4KB global, 150 lines / 6KB bot).
func (s *Store) RebuildIndex(ctx context.Context, scope, botID string) (*IndexDoc, error) {
	if _, err := s.SyncFromDisk(ctx); err != nil {
		return nil, err
	}
	absPath, relPath, maxLines, maxBytes, err := s.indexPathAndLimit(scope, botID)
	if err != nil {
		return nil, err
	}
	normScope, _ := NormalizeScope(scope, botID)

	items, err := s.listActiveItemsForIndex(ctx, normScope, botID)
	if err != nil {
		return nil, err
	}
	content := renderCuratedIndex(normScope, botID, items, maxLines, maxBytes)
	if err := s.writeAndSyncItem(ctx, absPath, content); err != nil {
		return nil, err
	}
	return &IndexDoc{
		Path:     relPath,
		Content:  content,
		Lines:    countNonEmptyOrLines(content),
		MaxLines: maxLines,
	}, nil
}

// BuildFlushPrompt constructs a concise Pre-Rotation / Post-Overflow flush prompt
// asking an agent to extract durable notes before session rotation.
func BuildFlushPrompt(botID, reason, recentTranscript string) string {
	var b strings.Builder
	b.WriteString("Session context is about to rotate")
	if strings.TrimSpace(reason) != "" {
		b.WriteString(" (" + strings.TrimSpace(reason) + ")")
	}
	b.WriteString(".\n")
	if strings.TrimSpace(botID) != "" {
		b.WriteString("Bot: " + strings.TrimSpace(botID) + "\n")
	}
	b.WriteString("Extract concise, durable working notes (decisions, open blockers, user preferences, file paths, next steps) to append to today's daily log.\n")
	b.WriteString("Rules: never include plaintext secrets/keys/tokens; reply with NOOP if nothing is worth preserving.\n")
	if strings.TrimSpace(recentTranscript) != "" {
		b.WriteString("\nRecent context:\n")
		b.WriteString(truncateRunes(strings.TrimSpace(recentTranscript), 4000))
		b.WriteByte('\n')
	}
	return b.String()
}

// ApplyFlushNote appends a validated Pre-Rotation / Post-Overflow flush note into daily/YYYY-MM-DD.md.
// Empty notes or NOOP/NONE responses are skipped cleanly.
func (s *Store) ApplyFlushNote(ctx context.Context, scope, botID, note string, now time.Time) (*SaveResult, error) {
	trimmed := strings.TrimSpace(note)
	if trimmed == "" || strings.EqualFold(trimmed, "NOOP") || strings.EqualFold(trimmed, "NONE") {
		return nil, nil
	}
	if now.IsZero() {
		now = s.now()
	}
	return s.Save(ctx, SaveReq{
		Scope:        scope,
		BotID:        botID,
		Title:        "Session Flush",
		Text:         trimmed,
		SourceAuthor: AuthorAgent,
		Daily:        true,
		Date:         now.UTC().Format("2006-01-02"),
	})
}

func dreamNewItemStatus(scope string, confidence float64) string {
	if scope == ScopeGlobal {
		return StatusCandidate
	}
	if confidence > 0 && confidence < 0.6 {
		return StatusCandidate
	}
	return StatusActive
}

func (s *Store) listActiveItemsForIndex(ctx context.Context, scope, botID string) ([]Item, error) {
	if s.db == nil {
		return nil, nil
	}
	where := []string{"doc_type = ?", "scope = ?", "status = ?"}
	args := []any{DocTypeItem, scope, StatusActive}
	if scope == ScopeBot {
		where = append(where, "bot_id = ?")
		args = append(args, botID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+docSelectCols+`
		FROM memory_docs
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY pin DESC, updated_at DESC, doc_id ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		it, _, err := itemFromDocRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func renderCuratedIndex(scope, botID string, items []Item, maxLines, maxBytes int) string {
	header := "# Global Memory Index"
	if scope == ScopeBot {
		header = "# Memory Index (" + botID + ")"
	}
	lines := []string{header, ""}

	kindOrder := map[string]int{
		KindRule: 1, KindPreference: 2, KindProfile: 3, KindDecision: 4,
		KindProcedure: 5, KindFact: 6, KindReference: 7, KindEpisode: 8,
	}
	sorted := append([]Item(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Pin != sorted[j].Pin {
			return sorted[i].Pin
		}
		ki, kj := kindOrder[sorted[i].Kind], kindOrder[sorted[j].Kind]
		if ki != kj {
			return ki < kj
		}
		if sorted[i].UpdatedAt != sorted[j].UpdatedAt {
			return sorted[i].UpdatedAt > sorted[j].UpdatedAt
		}
		return sorted[i].ID < sorted[j].ID
	})

	usedBytes := len(header) + 2
	for _, it := range sorted {
		if len(lines) >= maxLines {
			break
		}
		pinPrefix := ""
		if it.Pin {
			pinPrefix = "[pinned] "
		}
		bullet := fmt.Sprintf("- %s[%s] %s (`items/%s`, id: %s)", pinPrefix, it.Kind, truncateRunes(it.Title, 84), filepath.Base(it.Path), it.ID)
		if maxBytes > 0 && usedBytes+len(bullet)+1 > maxBytes {
			break
		}
		lines = append(lines, bullet)
		usedBytes += len(bullet) + 1
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func (s *Store) appendDreamDiary(
	ctx context.Context,
	scope, botID, summary string,
	ops []string,
	res *DreamApplyResult,
	now time.Time,
) (string, error) {
	memDir, err := s.memoryDir(scope, botID)
	if err != nil {
		return "", err
	}
	dateStr := now.Format("2006-01-02")
	absPath := filepath.Join(memDir, "dreams", dateStr+".md")
	relPath, _ := filepath.Rel(s.workspacesDir, absPath)
	relPath = filepath.ToSlash(relPath)

	var entry strings.Builder
	entry.WriteString(fmt.Sprintf("## Dream Run %s\n\n", now.Format("15:04:05Z")))
	if strings.TrimSpace(summary) != "" {
		entry.WriteString("Summary: " + strings.TrimSpace(summary) + "\n\n")
	}
	entry.WriteString(fmt.Sprintf("Stats: added=%d updated=%d superseded=%d archived=%d skipped=%d blocked=%d\n",
		res.Added, res.Updated, res.Superseded, res.Archived, res.Skipped, res.Blocked))
	if len(ops) > 0 {
		entry.WriteString("\nOperations:\n" + strings.Join(ops, "\n") + "\n")
	}
	if len(res.Warnings) > 0 {
		entry.WriteString("\nWarnings:\n")
		for _, w := range res.Warnings {
			entry.WriteString("- " + w + "\n")
		}
	}

	s.mu.Lock()
	var existing string
	if raw, err := os.ReadFile(absPath); err == nil {
		existing = strings.ReplaceAll(string(raw), "\r\n", "\n")
	}
	next := fmt.Sprintf("# Dream Audit Diary %s\n\n%s", dateStr, entry.String())
	if strings.TrimSpace(existing) != "" {
		next = strings.TrimRight(existing, "\n") + "\n\n" + entry.String()
	}
	wErr := AtomicWriteFile(absPath, []byte(next), 0o644)
	if wErr == nil {
		wErr = s.syncSingleFileLocked(ctx, absPath)
	}
	s.mu.Unlock()
	if wErr != nil {
		return "", wErr
	}
	return relPath, nil
}

func extractJSONPayload(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		rest := s[idx+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			s = strings.TrimSpace(rest[:end])
		}
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return strings.TrimSpace(s[start : end+1])
	}
	return s
}

func mergeTags(base, extra []string) []string {
	merged := make([]string, 0, len(base)+len(extra))
	merged = append(merged, base...)
	merged = append(merged, extra...)
	return NormalizeTags(merged)
}
