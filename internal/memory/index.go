package memory

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type diskFileMeta struct {
	absPath string
	relPath string
	scope   string
	botID   string
	docType string
	mtime   int64
	size    int64
}

type existingDocState struct {
	docID string
	mtime int64
	size  int64
	sha   string
}

type parsedDoc struct {
	docID         string
	relPath       string
	scope         string
	botID         string
	docType       string
	kind          string
	title         string
	body          string
	why           string
	tags          []string
	status        string
	pin           bool
	confidence    float64
	validFrom     string
	validUntil    string
	supersededBy  string
	sourceSession string
	sourceAuthor  string
	createdAt     string
	updatedAt     string
	mtime         int64
	size          int64
	sha256        string
	chunks        []docChunk
}

type docChunk struct {
	chunkID    string
	chunkIndex int
	heading    string
	startLine  int
	endLine    int
	text       string
	sha256     string
}

type savedEmbedding struct {
	model     string
	dim       int
	vec       []byte
	updatedAt string
	chunkSHA  string
}

// SyncFromDisk performs an incremental scan of workspaces/_shared/memory and workspaces/<bot>/memory,
// checking mtime+size first and sha256 second, and updating SQLite derived tables only for changed files.
// It never writes back to Markdown files.
func (s *Store) SyncFromDisk(ctx context.Context) (SyncStats, error) {
	if s == nil || s.db == nil {
		return SyncStats{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncFromDiskLocked(ctx)
}

func (s *Store) syncFromDiskLocked(ctx context.Context) (SyncStats, error) {
	var stats SyncStats
	files, err := s.discoverMemoryFiles()
	if err != nil {
		return stats, err
	}
	stats.Scanned = len(files)

	existing, err := s.loadExistingDocStates(ctx)
	if err != nil {
		return stats, err
	}

	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		seen[f.relPath] = true
		prev, hasPrev := existing[f.relPath]
		if hasPrev && prev.mtime == f.mtime && prev.size == f.size {
			stats.Unchanged++
			continue
		}

		rawBytes, readErr := os.ReadFile(f.absPath)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				delete(seen, f.relPath)
				continue
			}
			return stats, readErr
		}
		sum := sha256Hex(rawBytes)
		if hasPrev && prev.sha == sum {
			if _, err := s.db.ExecContext(ctx, `UPDATE memory_docs SET mtime_unix = ?, size = ? WHERE doc_id = ?`, f.mtime, f.size, prev.docID); err != nil {
				return stats, err
			}
			stats.Unchanged++
			continue
		}

		doc := parseDiskDoc(f, string(rawBytes), sum)
		if err := s.upsertParsedDocLocked(ctx, doc); err != nil {
			return stats, err
		}
		stats.Indexed++
		stats.Chunks += len(doc.chunks)
	}

	for relPath, prev := range existing {
		if seen[relPath] {
			continue
		}
		if err := s.deleteDocFromIndexLocked(ctx, prev.docID); err != nil {
			return stats, err
		}
		stats.Deleted++
	}
	return stats, nil
}

// Reindex rebuilds memory_docs, memory_chunks, and both FTS5 tables from disk.
// It intentionally preserves memory_stats and memory_dismissed.
func (s *Store) Reindex(ctx context.Context) (SyncStats, error) {
	if s == nil || s.db == nil {
		return SyncStats{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SyncStats{}, err
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range []string{
		`DELETE FROM memory_fts_uni`,
		`DELETE FROM memory_fts_tri`,
		`DELETE FROM memory_embeddings`,
		`DELETE FROM memory_chunks`,
		`DELETE FROM memory_docs`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return SyncStats{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SyncStats{}, err
	}

	stats, err := s.syncFromDiskLocked(ctx)
	if err != nil {
		return stats, err
	}
	if s.embedClient != nil && s.cfg.Semantic.Enabled {
		_, _ = s.syncEmbeddingsLocked(ctx)
	}
	return stats, nil
}

func (s *Store) syncSingleFileLocked(ctx context.Context, absPath string) error {
	if s.db == nil {
		return nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			rel, relErr := filepath.Rel(s.workspacesDir, absPath)
			if relErr != nil {
				return nil
			}
			var docID string
			if s.db.QueryRowContext(ctx, `SELECT doc_id FROM memory_docs WHERE path = ?`, filepath.ToSlash(rel)).Scan(&docID) == nil {
				return s.deleteDocFromIndexLocked(ctx, docID)
			}
			return nil
		}
		return err
	}
	meta, ok := s.classifyMemoryFile(absPath, info)
	if !ok {
		return nil
	}
	rawBytes, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	doc := parseDiskDoc(meta, string(rawBytes), sha256Hex(rawBytes))
	return s.upsertParsedDocLocked(ctx, doc)
}

func (s *Store) discoverMemoryFiles() ([]diskFileMeta, error) {
	if strings.TrimSpace(s.workspacesDir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(s.workspacesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []diskFileMeta
	for _, ent := range entries {
		if !ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		name := ent.Name()
		memDir := filepath.Join(s.workspacesDir, name, "memory")
		if st, err := os.Stat(memDir); err != nil || !st.IsDir() {
			continue
		}
		scope := ScopeBot
		botID := name
		if name == "_shared" {
			scope = ScopeGlobal
			botID = ""
		}

		idxPath := filepath.Join(memDir, "MEMORY.md")
		if info, err := os.Stat(idxPath); err == nil && !info.IsDir() {
			out = append(out, diskFileMeta{
				absPath: idxPath,
				relPath: filepath.ToSlash(filepath.Join(name, "memory", "MEMORY.md")),
				scope:   scope,
				botID:   botID,
				docType: DocTypeIndex,
				mtime:   info.ModTime().UnixNano(),
				size:    info.Size(),
			})
		}

		for _, spec := range []struct{ sub, docType string }{
			{"items", DocTypeItem},
			{"daily", DocTypeDaily},
			{"dreams", DocTypeDream},
		} {
			subDir := filepath.Join(memDir, spec.sub)
			subEntries, err := os.ReadDir(subDir)
			if err != nil {
				continue
			}
			for _, subEnt := range subEntries {
				if subEnt.IsDir() || strings.HasPrefix(subEnt.Name(), ".") || !strings.HasSuffix(strings.ToLower(subEnt.Name()), ".md") {
					continue
				}
				info, err := subEnt.Info()
				if err != nil {
					continue
				}
				out = append(out, diskFileMeta{
					absPath: filepath.Join(subDir, subEnt.Name()),
					relPath: filepath.ToSlash(filepath.Join(name, "memory", spec.sub, subEnt.Name())),
					scope:   scope,
					botID:   botID,
					docType: spec.docType,
					mtime:   info.ModTime().UnixNano(),
					size:    info.Size(),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].relPath < out[j].relPath })
	return out, nil
}

func (s *Store) classifyMemoryFile(absPath string, info os.FileInfo) (diskFileMeta, bool) {
	rel, err := filepath.Rel(s.workspacesDir, absPath)
	if err != nil {
		return diskFileMeta{}, false
	}
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[1] != "memory" {
		return diskFileMeta{}, false
	}
	scope := ScopeBot
	botID := parts[0]
	if parts[0] == "_shared" {
		scope = ScopeGlobal
		botID = ""
	}
	docType := ""
	switch {
	case len(parts) == 3 && parts[2] == "MEMORY.md":
		docType = DocTypeIndex
	case len(parts) == 4 && parts[2] == "items" && strings.HasSuffix(strings.ToLower(parts[3]), ".md"):
		docType = DocTypeItem
	case len(parts) == 4 && parts[2] == "daily" && strings.HasSuffix(strings.ToLower(parts[3]), ".md"):
		docType = DocTypeDaily
	case len(parts) == 4 && parts[2] == "dreams" && strings.HasSuffix(strings.ToLower(parts[3]), ".md"):
		docType = DocTypeDream
	default:
		return diskFileMeta{}, false
	}
	return diskFileMeta{
		absPath: absPath,
		relPath: rel,
		scope:   scope,
		botID:   botID,
		docType: docType,
		mtime:   info.ModTime().UnixNano(),
		size:    info.Size(),
	}, true
}

func (s *Store) loadExistingDocStates(ctx context.Context) (map[string]existingDocState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc_id, path, mtime_unix, size, sha256 FROM memory_docs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]existingDocState)
	for rows.Next() {
		var path string
		var st existingDocState
		if err := rows.Scan(&st.docID, &path, &st.mtime, &st.size, &st.sha); err != nil {
			return nil, err
		}
		out[path] = st
	}
	return out, rows.Err()
}

// recordAccess updates SQLite-only memory_stats; never touches Markdown files.
func (s *Store) recordAccess(ctx context.Context, docIDs []string) {
	if s == nil || s.db == nil || len(docIDs) == 0 {
		return
	}
	now := s.now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool, len(docIDs))
	for _, id := range docIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO memory_stats(doc_id, access_count, last_accessed_at)
			VALUES (?, 1, ?)
			ON CONFLICT(doc_id) DO UPDATE SET
				access_count = memory_stats.access_count + 1,
				last_accessed_at = excluded.last_accessed_at
		`, id, now)
	}
}
