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

// ImportFromMagpie migrates Magpie Memory markdown files (~/.config/magpie/memory/m_*.md) directly
// into Pebble's file-first Markdown SoT with idempotent deduplication and SecretGuard validation.
func (s *Store) ImportFromMagpie(ctx context.Context, opts ImportOpts) (*ImportResult, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: store is nil", ErrInvalidRequest)
	}
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		mode = "bots_only"
	}
	if mode != "bots_only" && mode != "all" {
		return nil, fmt.Errorf("%w: import mode must be bots_only or all", ErrInvalidRequest)
	}
	filterBot := strings.TrimSpace(opts.BotID)

	magpieDir := strings.TrimSpace(opts.MagpieDir)
	if magpieDir == "" {
		cfg, _ := s.runtimeConfig()
		magpieDir = strings.TrimSpace(cfg.MagpieConfigDir)
	}
	if magpieDir == "" {
		magpieDir = DefaultMagpieConfigDir()
	}
	memDir := filepath.Join(magpieDir, "memory")

	res := &ImportResult{
		BotCounts: map[string]int{},
	}

	entries, err := os.ReadDir(memDir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}

	// Ensure our SQLite index is current before checking idempotent duplicates.
	if _, err := s.SyncFromDisk(ctx); err != nil {
		return res, err
	}

	existingIDs, existingFPs := s.loadImportDedupSets(ctx)

	var fileNames []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if strings.HasPrefix(name, "m_") && strings.HasSuffix(strings.ToLower(name), ".md") {
			fileNames = append(fileNames, name)
		}
	}
	sort.Strings(fileNames)

	nowISO := s.now().UTC().Format(time.RFC3339)

	for _, name := range fileNames {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		rawBytes, readErr := os.ReadFile(filepath.Join(memDir, name))
		if readErr != nil {
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, readErr))
			continue
		}
		fm, body, hasFM := ParseMarkdownItem(string(rawBytes))
		if !hasFM || strings.TrimSpace(body) == "" {
			res.Skipped++
			continue
		}

		origStatus := strings.ToLower(strings.TrimSpace(fm.Status))
		if origStatus == StatusArchived || origStatus == StatusSuperseded || origStatus == "deleted" {
			res.Skipped++
			continue
		}

		targetScope, targetBot, cleanTags, ok := routeMagpieRecord(fm, mode, filterBot)
		if !ok {
			res.Skipped++
			continue
		}

		title := DeriveTitle(fm.Title, body)
		if err := CheckFields(map[string]string{
			"title": title,
			"text":  body,
			"why":   fm.Why,
			"tags":  strings.Join(cleanTags, " "),
		}); err != nil {
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}

		origID := strings.TrimSuffix(name, filepath.Ext(name))
		detID := "imp_" + strings.TrimPrefix(origID, "m_")
		fp := Fingerprint(targetScope, targetBot, title, body)
		if existingIDs[detID] || existingFPs[fp] {
			res.Skipped++
			continue
		}

		createdAt := firstNonEmpty(fm.CreatedAt, nowISO)
		updatedAt := firstNonEmpty(fm.UpdatedAt, createdAt)
		status := StatusActive
		if origStatus == StatusCandidate {
			status = StatusCandidate
		}

		outFM := Frontmatter{
			ID:           detID,
			Scope:        targetScope,
			BotID:        targetBot,
			Kind:         NormalizeKind(fm.Kind),
			Title:        title,
			Status:       status,
			Confidence:   DefaultConfidence,
			Pin:          fm.Pin,
			Tags:         cleanTags,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
			SourceAuthor: AuthorMagpieImport,
			Why:          strings.TrimSpace(fm.Why),
		}

		targetDir, dirErr := s.memoryDir(targetScope, targetBot)
		if dirErr != nil {
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, dirErr))
			continue
		}
		shortSuffix := strings.TrimPrefix(detID, "imp_")
		if len(shortSuffix) > 12 {
			shortSuffix = shortSuffix[:12]
		}
		fileName := fmt.Sprintf("%s-%s.md", Slugify(title), shortSuffix)
		absPath := filepath.Join(targetDir, "items", fileName)
		relPath, _ := filepath.Rel(s.workspacesDir, absPath)
		relPath = filepath.ToSlash(relPath)

		if !opts.DryRun {
			rendered := RenderMarkdownItem(outFM, body)
			s.mu.Lock()
			wErr := AtomicWriteFile(absPath, []byte(rendered), 0o644)
			if wErr == nil {
				wErr = s.syncSingleFileLocked(ctx, absPath)
			}
			s.mu.Unlock()
			if wErr != nil {
				res.Failed++
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, wErr))
				continue
			}
		}

		existingIDs[detID] = true
		existingFPs[fp] = true
		res.Imported++
		if targetScope == ScopeGlobal {
			res.GlobalCount++
		} else {
			res.BotCounts[targetBot]++
		}
		res.Items = append(res.Items, itemFromFrontmatter(outFM, relPath, body, OriginMemory))
	}

	if len(res.BotCounts) == 0 {
		res.BotCounts = nil
	}
	return res, nil
}

func routeMagpieRecord(fm Frontmatter, mode, filterBot string) (scope, botID string, cleanTags []string, ok bool) {
	extractedBot := ""
	remaining := make([]string, 0, len(fm.Tags))
	for _, rawTag := range fm.Tags {
		t := strings.TrimSpace(strings.TrimPrefix(rawTag, "#"))
		if t == "" {
			continue
		}
		if len(t) > 4 && strings.EqualFold(t[:4], "bot:") {
			cand := strings.TrimSpace(t[4:])
			if extractedBot == "" && validateBotID(cand) == nil {
				extractedBot = cand
				continue
			}
		}
		remaining = append(remaining, t)
	}

	if extractedBot != "" {
		if filterBot != "" && !strings.EqualFold(extractedBot, filterBot) {
			return "", "", nil, false
		}
		return ScopeBot, extractedBot, NormalizeTags(remaining), true
	}

	if mode == "all" && filterBot == "" {
		origScope := strings.ToLower(strings.TrimSpace(fm.Scope))
		origStatus := strings.ToLower(strings.TrimSpace(fm.Status))
		if origScope == ScopeGlobal && (origStatus == "" || origStatus == StatusActive) {
			return ScopeGlobal, "", NormalizeTags(remaining), true
		}
	}
	return "", "", nil, false
}

func (s *Store) loadImportDedupSets(ctx context.Context) (ids map[string]bool, fps map[string]bool) {
	ids = map[string]bool{}
	fps = map[string]bool{}
	if s.db == nil {
		return ids, fps
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT doc_id, scope, bot_id, title, body
		FROM memory_docs
		WHERE doc_type = ?
	`, DocTypeItem)
	if err != nil {
		return ids, fps
	}
	defer rows.Close()
	for rows.Next() {
		var id, sc, bid, title, body string
		if rows.Scan(&id, &sc, &bid, &title, &body) != nil {
			continue
		}
		ids[id] = true
		fps[Fingerprint(sc, bid, title, body)] = true
	}
	return ids, fps
}
