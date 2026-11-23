package state

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
)

// SyncFileRecord tracks the last known vector clock and tombstone state of a synced file.
type SyncFileRecord struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256,omitempty"`
	Size      int64  `json:"size,omitempty"`
	MTimeUnix int64  `json:"mtimeUnix,omitempty"`
	Deleted   bool   `json:"deleted,omitempty"`
	VCJSON    string `json:"vcJson"`
	UpdatedBy string `json:"updatedBy,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// GetOrCreateHostID returns a stable persistent host ID stored in meta ("sync_host_id").
func (s *Store) GetOrCreateHostID(fallback string) string {
	if s == nil || s.db == nil {
		return sanitizeSyncHost(fallback)
	}
	if cur := strings.TrimSpace(s.GetMeta("sync_host_id")); cur != "" {
		return cur
	}
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	id := sanitizeSyncHost(fallback) + "-" + hex.EncodeToString(b)
	_ = s.SetMeta("sync_host_id", id)
	return id
}

func sanitizeSyncHost(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "host"
	}
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	res := strings.Trim(b.String(), "-")
	if res == "" {
		return "host"
	}
	return res
}

// ListSyncFiles returns all tracked sync_files rows ordered by path.
func (s *Store) ListSyncFiles() ([]SyncFileRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT path, sha256, size, mtime_unix, deleted, vc_json, updated_by, updated_at
		FROM sync_files
		ORDER BY path ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SyncFileRecord
	for rows.Next() {
		var r SyncFileRecord
		var del int
		if err := rows.Scan(&r.Path, &r.SHA256, &r.Size, &r.MTimeUnix, &del, &r.VCJSON, &r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Deleted = del != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetSyncFile returns the sync_files row for relPath if present.
func (s *Store) GetSyncFile(relPath string) (SyncFileRecord, bool, error) {
	if s == nil || s.db == nil {
		return SyncFileRecord{}, false, nil
	}
	var r SyncFileRecord
	var del int
	err := s.db.QueryRow(`
		SELECT path, sha256, size, mtime_unix, deleted, vc_json, updated_by, updated_at
		FROM sync_files WHERE path = ?
	`, relPath).Scan(&r.Path, &r.SHA256, &r.Size, &r.MTimeUnix, &del, &r.VCJSON, &r.UpdatedBy, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return SyncFileRecord{}, false, nil
	}
	if err != nil {
		return SyncFileRecord{}, false, err
	}
	r.Deleted = del != 0
	return r, true, nil
}

// UpsertSyncFile inserts or updates a sync_files row.
func (s *Store) UpsertSyncFile(r SyncFileRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	del := 0
	if r.Deleted {
		del = 1
	}
	vc := strings.TrimSpace(r.VCJSON)
	if vc == "" {
		vc = "{}"
	}
	_, err := s.db.Exec(`
		INSERT INTO sync_files(path, sha256, size, mtime_unix, deleted, vc_json, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			sha256     = excluded.sha256,
			size       = excluded.size,
			mtime_unix = excluded.mtime_unix,
			deleted    = excluded.deleted,
			vc_json    = excluded.vc_json,
			updated_by = excluded.updated_by,
			updated_at = excluded.updated_at
	`, r.Path, r.SHA256, r.Size, r.MTimeUnix, del, vc, r.UpdatedBy, r.UpdatedAt)
	return err
}

// PruneDeletedSyncFiles removes tombstones older than olderThan.
func (s *Store) PruneDeletedSyncFiles(olderThan time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	res, err := s.db.Exec(`
		DELETE FROM sync_files
		WHERE deleted = 1 AND updated_at != '' AND updated_at < ?
	`, olderThan.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
