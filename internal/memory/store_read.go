package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) memoryDir(scope, botID string) (string, error) {
	if scope == ScopeGlobal {
		return filepath.Join(s.workspacesDir, "_shared", "memory"), nil
	}
	if err := validateBotID(botID); err != nil {
		return "", err
	}
	return filepath.Join(s.workspacesDir, strings.TrimSpace(botID), "memory"), nil
}

func (s *Store) indexPathAndLimit(scope, botID string) (absPath, relPath string, maxLines, maxBytes int, err error) {
	normScope, err := NormalizeScope(scope, botID)
	if err != nil {
		return "", "", 0, 0, err
	}
	if normScope == ScopeAll {
		return "", "", 0, 0, fmt.Errorf("%w: index scope must be global or bot", ErrInvalidScope)
	}
	dir, err := s.memoryDir(normScope, botID)
	if err != nil {
		return "", "", 0, 0, err
	}
	absPath = filepath.Join(dir, "MEMORY.md")
	relPath, _ = filepath.Rel(s.workspacesDir, absPath)
	relPath = filepath.ToSlash(relPath)
	if normScope == ScopeGlobal {
		return absPath, relPath, GlobalIndexMaxLines, GlobalIndexMaxBytes, nil
	}
	return absPath, relPath, BotIndexMaxLines, BotIndexMaxBytes, nil
}

// ReadIndex reads global or bot MEMORY.md along with line-count metadata.
func (s *Store) ReadIndex(ctx context.Context, scope, botID string) (*IndexDoc, error) {
	_ = ctx
	absPath, relPath, maxLines, _, err := s.indexPathAndLimit(scope, botID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &IndexDoc{Path: relPath, Content: "", Lines: 0, MaxLines: maxLines}, nil
		}
		return nil, err
	}
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")
	return &IndexDoc{
		Path:     relPath,
		Content:  content,
		Lines:    countNonEmptyOrLines(content),
		MaxLines: maxLines,
	}, nil
}

// WriteIndex validates secrets and line limits, atomically writes MEMORY.md, and syncs SQLite.
func (s *Store) WriteIndex(ctx context.Context, scope, botID, content string) (*IndexDoc, error) {
	if err := CheckSecret("content", content); err != nil {
		return nil, err
	}
	absPath, relPath, maxLines, _, err := s.indexPathAndLimit(scope, botID)
	if err != nil {
		return nil, err
	}
	norm := strings.ReplaceAll(content, "\r\n", "\n")
	if norm != "" && !strings.HasSuffix(norm, "\n") {
		norm += "\n"
	}
	lines := countNonEmptyOrLines(norm)
	if lines > maxLines {
		return nil, fmt.Errorf("%w: %d lines exceeds max %d for %s", ErrIndexTooLarge, lines, maxLines, relPath)
	}
	if err := s.writeAndSyncItem(ctx, absPath, norm); err != nil {
		return nil, err
	}
	return &IndexDoc{Path: relPath, Content: norm, Lines: lines, MaxLines: maxLines}, nil
}

// Get retrieves an L2 memory item by ID or reads a safely jailed memory file with optional line slicing.
func (s *Store) Get(ctx context.Context, opts GetOpts) (*GetResult, error) {
	if id := strings.TrimSpace(opts.ID); id != "" {
		absPath, relPath, fm, body, err := s.loadItemByID(ctx, id)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(absPath)
		if err != nil {
			return nil, err
		}
		content, startL, endL, totalL, trunc := sliceLines(string(raw), opts.StartLine, opts.EndLine)
		it := itemFromFrontmatter(fm, relPath, body, OriginMemory)
		s.recordAccess(ctx, []string{fm.ID})
		return &GetResult{
			Item:       &it,
			Path:       relPath,
			Content:    content,
			StartLine:  startL,
			EndLine:    endL,
			TotalLines: totalL,
			Truncated:  trunc,
		}, nil
	}

	rawPath := strings.TrimSpace(opts.Path)
	if rawPath == "" {
		return nil, fmt.Errorf("%w: either id or path is required", ErrInvalidRequest)
	}
	absPath, relPath, err := s.resolveSafeReadPath(rawPath, opts.Scope, opts.BotID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, relPath)
		}
		return nil, err
	}
	content, startL, endL, totalL, trunc := sliceLines(string(raw), opts.StartLine, opts.EndLine)

	var itemPtr *Item
	if strings.Contains(relPath, "/memory/items/") {
		if fm, body, hasFM := ParseMarkdownItem(string(raw)); hasFM {
			it := itemFromFrontmatter(fm, relPath, body, OriginMemory)
			itemPtr = &it
			if fm.ID != "" {
				s.recordAccess(ctx, []string{fm.ID})
			}
		}
	} else if s.db != nil {
		var docID string
		if s.db.QueryRowContext(ctx, `SELECT doc_id FROM memory_docs WHERE path = ?`, relPath).Scan(&docID) == nil && docID != "" {
			s.recordAccess(ctx, []string{docID})
		}
	}

	return &GetResult{
		Item:       itemPtr,
		Path:       relPath,
		Content:    content,
		StartLine:  startL,
		EndLine:    endL,
		TotalLines: totalL,
		Truncated:  trunc,
	}, nil
}

// Status returns overall memory engine metrics, semantic/magpie status, and recent dream records.
func (s *Store) Status(ctx context.Context) (*StatusSummary, error) {
	if _, err := s.SyncFromDisk(ctx); err != nil {
		return nil, err
	}
	counts, err := s.queryCounts(ctx, ScopeAll, "", nil)
	if err != nil {
		return nil, err
	}
	docCount := 0
	chunkCount := 0
	if s.db != nil {
		docCount = fmtDocCountQuery(s.db, ctx, "memory_docs")
		chunkCount = fmtDocCountQuery(s.db, ctx, "memory_chunks")
	}
	magpie := s.DetectMagpieMemory(ctx)
	semantic := s.semanticStatus(ctx)
	dreams := s.listRecentDreams(ctx, 10)

	cfg, _ := s.runtimeConfig()
	return &StatusSummary{
		Backend:      BackendPebble,
		Config:       cfg.summary(),
		DocCount:     docCount,
		ChunkCount:   chunkCount,
		Counts:       counts,
		Semantic:     semantic,
		Magpie:       &magpie,
		RecentDreams: dreams,
	}, nil
}

// IsDismissed reports whether a memory matching (scope, botID, title, text) was previously dismissed.
func (s *Store) IsDismissed(ctx context.Context, scope, botID, title, text string) bool {
	if s == nil || s.db == nil {
		return false
	}
	fp := Fingerprint(scope, botID, title, text)
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM memory_dismissed WHERE fingerprint = ? LIMIT 1`, fp).Scan(&exists)
	return err == nil
}

func (s *Store) loadItemByID(ctx context.Context, id string) (absPath, relPath string, fm Frontmatter, body string, err error) {
	if _, syncErr := s.SyncFromDisk(ctx); syncErr != nil {
		return "", "", Frontmatter{}, "", syncErr
	}
	if s.db != nil {
		var p string
		if qErr := s.db.QueryRowContext(ctx, `SELECT path FROM memory_docs WHERE doc_id = ? LIMIT 1`, id).Scan(&p); qErr == nil && p != "" {
			relPath = p
			absPath = filepath.Join(s.workspacesDir, filepath.FromSlash(p))
		}
	}
	if absPath == "" {
		return "", "", Frontmatter{}, "", fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	raw, readErr := os.ReadFile(absPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", "", Frontmatter{}, "", fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return "", "", Frontmatter{}, "", readErr
	}
	fm, body, _ = ParseMarkdownItem(string(raw))
	if fm.ID == "" {
		fm.ID = id
	}
	if fm.Scope == "" {
		if strings.HasPrefix(relPath, "_shared/") {
			fm.Scope = ScopeGlobal
		} else {
			fm.Scope = ScopeBot
			fm.BotID = strings.Split(relPath, "/")[0]
		}
	}
	return absPath, relPath, fm, body, nil
}

func (s *Store) resolveSafeReadPath(rawPath, scope, botID string) (absPath, relPath string, err error) {
	p := filepath.ToSlash(strings.TrimSpace(rawPath))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "workspaces/")
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\x00") {
		return "", "", ErrPathOutsideJail
	}

	if strings.HasPrefix(p, "memory/") || p == "MEMORY.md" || strings.HasPrefix(p, "items/") || strings.HasPrefix(p, "daily/") || strings.HasPrefix(p, "dreams/") {
		normScope, scopeErr := NormalizeScope(scope, botID)
		if scopeErr == nil && normScope != ScopeAll {
			prefix := "_shared"
			if normScope == ScopeBot {
				if vErr := validateBotID(botID); vErr != nil {
					return "", "", ErrPathOutsideJail
				}
				prefix = strings.TrimSpace(botID)
			}
			if strings.HasPrefix(p, "memory/") {
				p = prefix + "/" + p
			} else {
				p = prefix + "/memory/" + p
			}
		}
	}

	cleaned := filepath.ToSlash(filepath.Clean(p))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", "", ErrPathOutsideJail
	}
	if strings.EqualFold(filepath.Base(cleaned), "AGENTS.md") {
		return "", "", ErrProtectedFile
	}

	allowed := cleaned == "_shared/USER.md"
	if !allowed {
		parts := strings.Split(cleaned, "/")
		if len(parts) >= 3 && parts[1] == "memory" && parts[0] != "" && parts[0] != "." && parts[0] != ".." {
			allowed = true
		}
	}
	if !allowed {
		return "", "", ErrPathOutsideJail
	}

	candidate := filepath.Join(s.workspacesDir, filepath.FromSlash(cleaned))
	absCandidate, err := filepath.Abs(candidate)
	if err != nil || !isWithinDir(s.workspacesDir, absCandidate) {
		return "", "", ErrPathOutsideJail
	}
	if resolved, err := filepath.EvalSymlinks(absCandidate); err == nil {
		rootResolved, rErr := filepath.EvalSymlinks(s.workspacesDir)
		if rErr != nil {
			rootResolved = s.workspacesDir
		}
		if !isWithinDir(rootResolved, resolved) {
			return "", "", ErrPathOutsideJail
		}
	}
	return absCandidate, cleaned, nil
}

func (s *Store) listRecentDreams(ctx context.Context, limit int) []DreamRecord {
	if s.db == nil || limit <= 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT scope, bot_id, path, body, updated_at
		FROM memory_docs
		WHERE doc_type = ?
		ORDER BY updated_at DESC, path DESC
		LIMIT ?
	`, DocTypeDream, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []DreamRecord
	for rows.Next() {
		var rec DreamRecord
		var body string
		if rows.Scan(&rec.Scope, &rec.BotID, &rec.Path, &body, &rec.UpdatedAt) != nil {
			continue
		}
		rec.Date = strings.TrimSuffix(filepath.Base(rec.Path), ".md")
		rec.Summary = DeriveTitle("", body)
		out = append(out, rec)
	}
	return out
}

func itemFromFrontmatter(fm Frontmatter, relPath, body, origin string) Item {
	return Item{
		ID:            fm.ID,
		Path:          relPath,
		Scope:         fm.Scope,
		BotID:         fm.BotID,
		Kind:          NormalizeKind(fm.Kind),
		Title:         DeriveTitle(fm.Title, body),
		Text:          body,
		Snippet:       buildSnippet(body, "", 240),
		Why:           fm.Why,
		Tags:          NormalizeTags(fm.Tags),
		Status:        NormalizeStatus(fm.Status),
		Pin:           fm.Pin,
		Confidence:    fm.Confidence,
		ValidFrom:     fm.ValidFrom,
		ValidUntil:    fm.ValidUntil,
		SupersededBy:  fm.SupersededBy,
		SourceSession: fm.SourceSession,
		SourceAuthor:  NormalizeAuthor(fm.SourceAuthor),
		Origin:        origin,
		CreatedAt:     fm.CreatedAt,
		UpdatedAt:     fm.UpdatedAt,
	}
}
