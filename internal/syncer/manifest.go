package syncer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/state"
)

const MaxSyncFileSize = 25 * 1024 * 1024 // 25 MiB

// FileEntry describes a single syncable file or deletion tombstone inside workspaces/ or media/.
type FileEntry struct {
	Path        string      `json:"path"`
	Size        int64       `json:"size"`
	ModTimeUnix int64       `json:"modTimeUnix"`
	SHA256      string      `json:"sha256"`
	Deleted     bool        `json:"deleted,omitempty"`
	VC          VectorClock `json:"vc,omitempty"`
	UpdatedBy   string      `json:"updatedBy,omitempty"`
	UpdatedAt   string      `json:"updatedAt,omitempty"`
}

// Manifest is the snapshot of all syncable files on a host.
type Manifest struct {
	HostID      string      `json:"hostId"`
	GeneratedAt string      `json:"generatedAt"`
	Files       []FileEntry `json:"files"`
}

// ShouldSyncPath returns true only for safe relative paths inside workspaces/ or
// media/, excluding secrets, local glue files, databases, git metadata, and conflict copies.
func ShouldSyncPath(relPath string) bool {
	p := strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/")
	if p == "" || strings.HasPrefix(p, "/") {
		return false
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return false
	}
	if clean != "workspaces" && !strings.HasPrefix(clean, "workspaces/") && !strings.HasPrefix(clean, "media/") {
		return false
	}
	if clean == "workspaces" || clean == "media" {
		return false
	}

	parts := strings.Split(clean, "/")
	for i, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
		if seg == ".git" || seg == "node_modules" {
			return false
		}
		if seg == ".env" || strings.HasPrefix(seg, ".env.") {
			return false
		}
		// Skip bot-local _shared symlink (workspaces/<bot>/_shared/...),
		// while allowing real files inside workspaces/_shared/...
		if i == 2 && seg == "_shared" && parts[1] != "_shared" {
			return false
		}
	}

	base := parts[len(parts)-1]
	switch base {
	case "opencode.json", config.PlatformInstructionsFilename, config.LegacyPlatformInstructionsFilename, "CLAUDE.md":
		return false
	}
	if strings.HasSuffix(clean, "/.agents/skills/"+config.PebbleSetupSkillName+"/SKILL.md") ||
		strings.HasSuffix(clean, "/.claude/skills/"+config.PebbleSetupSkillName+"/SKILL.md") {
		return false
	}
	if strings.Contains(base, ".conflict-") {
		return false
	}
	for _, ext := range []string{".db", ".db-wal", ".db-shm"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			return false
		}
	}
	return true
}

// BuildManifest scans workspaces/ (and optionally media/) under projectRoot.
func BuildManifest(projectRoot, hostID string, includeMedia bool) (*Manifest, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, errors.New("projectRoot is required")
	}
	roots := []string{"workspaces"}
	if includeMedia {
		roots = append(roots, "media")
	}

	var files []FileEntry
	for _, r := range roots {
		absRoot := filepath.Join(projectRoot, r)
		info, err := os.Stat(absRoot)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			relOS, err := filepath.Rel(projectRoot, p)
			if err != nil {
				return nil
			}
			rel := filepath.ToSlash(relOS)
			if d.IsDir() {
				if rel != r && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".DS_Store") {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if !ShouldSyncPath(rel) {
				return nil
			}
			fi, err := d.Info()
			if err != nil || !fi.Mode().IsRegular() || fi.Size() > MaxSyncFileSize {
				return nil
			}
			sum, err := fileSHA256(p)
			if err != nil {
				return nil
			}
			files = append(files, FileEntry{
				Path:        rel,
				Size:        fi.Size(),
				ModTimeUnix: fi.ModTime().Unix(),
				SHA256:      sum,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return &Manifest{
		HostID:      strings.TrimSpace(hostID),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Files:       files,
	}, nil
}

// BuildManifestWithStore scans disk files and reconciles them against store.sync_files
// so local edits advance the vector clock and deleted files become tombstones.
func BuildManifestWithStore(projectRoot, hostID string, store *state.Store, includeMedia bool) (*Manifest, error) {
	m, err := BuildManifest(projectRoot, hostID, includeMedia)
	if err != nil || store == nil {
		return m, err
	}
	nowStr := time.Now().UTC().Format(time.RFC3339)
	diskMap := make(map[string]FileEntry, len(m.Files))
	for _, f := range m.Files {
		diskMap[f.Path] = f
	}

	tracked, err := store.ListSyncFiles()
	if err != nil {
		return nil, err
	}
	trackedMap := make(map[string]state.SyncFileRecord, len(tracked))
	for _, rec := range tracked {
		trackedMap[rec.Path] = rec
	}

	var out []FileEntry
	for _, f := range m.Files {
		rec, exists := trackedMap[f.Path]
		vc := ParseVectorClock(rec.VCJSON)
		if !exists || rec.Deleted || rec.SHA256 != f.SHA256 {
			vc.Increment(hostID)
			rec = state.SyncFileRecord{
				Path:      f.Path,
				SHA256:    f.SHA256,
				Size:      f.Size,
				MTimeUnix: f.ModTimeUnix,
				Deleted:   false,
				VCJSON:    vc.MarshalJSONString(),
				UpdatedBy: hostID,
				UpdatedAt: nowStr,
			}
			_ = store.UpsertSyncFile(rec)
		}
		f.Deleted = false
		f.VC = vc
		f.UpdatedBy = rec.UpdatedBy
		f.UpdatedAt = rec.UpdatedAt
		out = append(out, f)
	}

	// Emit tombstones for tracked files that are no longer on disk.
	for _, rec := range tracked {
		if _, onDisk := diskMap[rec.Path]; onDisk {
			continue
		}
		if !ShouldSyncPath(rec.Path) {
			continue
		}
		if !includeMedia && strings.HasPrefix(rec.Path, "media/") {
			continue
		}
		vc := ParseVectorClock(rec.VCJSON)
		if !rec.Deleted {
			vc.Increment(hostID)
			rec = state.SyncFileRecord{
				Path:      rec.Path,
				SHA256:    "",
				Size:      0,
				MTimeUnix: time.Now().Unix(),
				Deleted:   true,
				VCJSON:    vc.MarshalJSONString(),
				UpdatedBy: hostID,
				UpdatedAt: nowStr,
			}
			_ = store.UpsertSyncFile(rec)
		}
		out = append(out, FileEntry{
			Path:        rec.Path,
			Size:        0,
			ModTimeUnix: rec.MTimeUnix,
			SHA256:      "",
			Deleted:     true,
			VC:          vc,
			UpdatedBy:   rec.UpdatedBy,
			UpdatedAt:   rec.UpdatedAt,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	m.Files = out
	return m, nil
}

// ApplyRemoteFileEntry merges a remote file or tombstone into projectRoot + store using vector clocks.
func ApplyRemoteFileEntry(
	projectRoot, localHostID string,
	store *state.Store,
	remote FileEntry,
	fetchData func() ([]byte, error),
) (applied bool, conflictPath string, err error) {
	if !ShouldSyncPath(remote.Path) {
		return false, "", fmt.Errorf("path %q is not syncable", remote.Path)
	}
	abs, err := safeJoin(projectRoot, remote.Path)
	if err != nil {
		return false, "", err
	}
	if store == nil {
		if remote.Deleted {
			if rmErr := os.Remove(abs); rmErr == nil || os.IsNotExist(rmErr) {
				return rmErr == nil, "", nil
			}
			return false, "", nil
		}
		data, fErr := fetchData()
		if fErr != nil {
			return false, "", fErr
		}
		cPath, wErr := WriteSyncedFile(projectRoot, remote.Path, remote.UpdatedBy, data, time.Unix(remote.ModTimeUnix, 0))
		return true, cPath, wErr
	}

	rec, exists, err := store.GetSyncFile(remote.Path)
	if err != nil {
		return false, "", err
	}
	localVC := ParseVectorClock(rec.VCJSON)
	remoteVC := remote.VC.Clone()
	if len(remoteVC) == 0 {
		// Backward-compatible fallback if peer didn't send a vector clock.
		remoteVC.Increment(remote.UpdatedBy)
	}

	var ord VCOrder
	if !exists {
		ord = VCBefore
	} else {
		ord = localVC.Compare(remoteVC)
	}

	switch ord {
	case VCEqual, VCAfter:
		return false, "", nil

	case VCBefore:
		if remote.Deleted {
			_ = os.Remove(abs)
			_ = store.UpsertSyncFile(state.SyncFileRecord{
				Path:      remote.Path,
				SHA256:    "",
				Size:      0,
				MTimeUnix: remote.ModTimeUnix,
				Deleted:   true,
				VCJSON:    remoteVC.MarshalJSONString(),
				UpdatedBy: remote.UpdatedBy,
				UpdatedAt: remote.UpdatedAt,
			})
			return true, "", nil
		}
		data, fErr := fetchData()
		if fErr != nil {
			return false, "", fErr
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return false, "", err
		}
		if err := atomicWriteFile(abs, data, 0o644, time.Unix(remote.ModTimeUnix, 0)); err != nil {
			return false, "", err
		}
		_ = store.UpsertSyncFile(state.SyncFileRecord{
			Path:      remote.Path,
			SHA256:    remote.SHA256,
			Size:      int64(len(data)),
			MTimeUnix: remote.ModTimeUnix,
			Deleted:   false,
			VCJSON:    remoteVC.MarshalJSONString(),
			UpdatedBy: remote.UpdatedBy,
			UpdatedAt: remote.UpdatedAt,
		})
		return true, "", nil

	default: // VCConcurrent
		mergedVC := localVC.Merge(remoteVC)
		// Same state on both sides (both deleted or identical content): converge clocks without conflict.
		if (rec.Deleted && remote.Deleted) || (!rec.Deleted && !remote.Deleted && rec.SHA256 != "" && rec.SHA256 == remote.SHA256) {
			rec.VCJSON = mergedVC.MarshalJSONString()
			_ = store.UpsertSyncFile(rec)
			return false, "", nil
		}
		// Delete vs Edit: edit always wins so newly written work is never lost.
		if rec.Deleted && !remote.Deleted {
			data, fErr := fetchData()
			if fErr != nil {
				return false, "", fErr
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return false, "", err
			}
			if err := atomicWriteFile(abs, data, 0o644, time.Unix(remote.ModTimeUnix, 0)); err != nil {
				return false, "", err
			}
			mergedVC.Increment(localHostID)
			_ = store.UpsertSyncFile(state.SyncFileRecord{
				Path:      remote.Path,
				SHA256:    remote.SHA256,
				Size:      int64(len(data)),
				MTimeUnix: remote.ModTimeUnix,
				Deleted:   false,
				VCJSON:    mergedVC.MarshalJSONString(),
				UpdatedBy: remote.UpdatedBy,
				UpdatedAt: remote.UpdatedAt,
			})
			return true, "", nil
		}
		if !rec.Deleted && remote.Deleted {
			mergedVC.Increment(localHostID)
			rec.VCJSON = mergedVC.MarshalJSONString()
			_ = store.UpsertSyncFile(rec)
			return false, "", nil
		}

		// Both sides edited different content -> deterministic winner + .conflict-<loser> copy.
		data, fErr := fetchData()
		if fErr != nil {
			return false, "", fErr
		}
		if remoteWinsConflict(rec, localVC, remote, remoteVC) {
			if localData, rErr := os.ReadFile(abs); rErr == nil {
				conflictRel := conflictRelPath(remote.Path, sanitizeHostTag(rec.UpdatedBy))
				if conflictAbs, cErr := safeJoin(projectRoot, conflictRel); cErr == nil {
					_ = atomicWriteFile(conflictAbs, localData, 0o644, time.Unix(rec.MTimeUnix, 0))
					conflictPath = conflictRel
				}
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return false, "", err
			}
			if err := atomicWriteFile(abs, data, 0o644, time.Unix(remote.ModTimeUnix, 0)); err != nil {
				return false, "", err
			}
			_ = store.UpsertSyncFile(state.SyncFileRecord{
				Path:      remote.Path,
				SHA256:    remote.SHA256,
				Size:      int64(len(data)),
				MTimeUnix: remote.ModTimeUnix,
				Deleted:   false,
				VCJSON:    mergedVC.MarshalJSONString(),
				UpdatedBy: remote.UpdatedBy,
				UpdatedAt: remote.UpdatedAt,
			})
			return true, conflictPath, nil
		}

		// Local wins -> save remote copy as .conflict-<remoteHost> and merge clocks.
		conflictRel := conflictRelPath(remote.Path, sanitizeHostTag(remote.UpdatedBy))
		if conflictAbs, cErr := safeJoin(projectRoot, conflictRel); cErr == nil {
			if err := atomicWriteFile(conflictAbs, data, 0o644, time.Unix(remote.ModTimeUnix, 0)); err != nil {
				return false, "", err
			}
			conflictPath = conflictRel
		}
		rec.VCJSON = mergedVC.MarshalJSONString()
		_ = store.UpsertSyncFile(rec)
		return false, conflictPath, nil
	}
}

func remoteWinsConflict(localRec state.SyncFileRecord, localVC VectorClock, remote FileEntry, remoteVC VectorClock) bool {
	lm, rm := localVC.MaxClock(), remoteVC.MaxClock()
	if rm != lm {
		return rm > lm
	}
	if remote.ModTimeUnix != localRec.MTimeUnix {
		return remote.ModTimeUnix > localRec.MTimeUnix
	}
	return remote.UpdatedBy > localRec.UpdatedBy
}

// ReadSyncedFile safely reads a syncable file under projectRoot.
func ReadSyncedFile(projectRoot, relPath string) ([]byte, FileEntry, error) {
	if !ShouldSyncPath(relPath) {
		return nil, FileEntry{}, fmt.Errorf("path %q is not syncable", relPath)
	}
	abs, err := safeJoin(projectRoot, relPath)
	if err != nil {
		return nil, FileEntry{}, err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return nil, FileEntry{}, err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return nil, FileEntry{}, errors.New("only regular files can be synced")
	}
	if fi.Size() > MaxSyncFileSize {
		return nil, FileEntry{}, fmt.Errorf("file exceeds max sync size (%d bytes)", MaxSyncFileSize)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, FileEntry{}, err
	}
	sum := sha256.Sum256(data)
	return data, FileEntry{
		Path:        path.Clean(strings.ReplaceAll(relPath, "\\", "/")),
		Size:        fi.Size(),
		ModTimeUnix: fi.ModTime().Unix(),
		SHA256:      hex.EncodeToString(sum[:]),
	}, nil
}

// WriteSyncedFile atomically writes data to relPath under projectRoot.
// When local content differs from incoming data, it preserves a .conflict-<host>
// copy using LWW by modification time so no edits are silently lost.
func WriteSyncedFile(projectRoot, relPath, sourceHost string, data []byte, remoteModTime time.Time) (conflictPath string, err error) {
	if !ShouldSyncPath(relPath) {
		return "", fmt.Errorf("path %q is not syncable", relPath)
	}
	if len(data) > MaxSyncFileSize {
		return "", fmt.Errorf("file exceeds max sync size (%d bytes)", MaxSyncFileSize)
	}
	abs, err := safeJoin(projectRoot, relPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}

	inSumBytes := sha256.Sum256(data)
	inSum := hex.EncodeToString(inSumBytes[:])

	if fi, statErr := os.Lstat(abs); statErr == nil && fi.Mode().IsRegular() {
		curSum, sumErr := fileSHA256(abs)
		if sumErr == nil && curSum == inSum {
			return "", nil
		}
		hostTag := sanitizeHostTag(sourceHost)
		if !remoteModTime.IsZero() && fi.ModTime().After(remoteModTime) {
			conflictRel := conflictRelPath(relPath, hostTag)
			conflictAbs, cErr := safeJoin(projectRoot, conflictRel)
			if cErr != nil {
				return "", cErr
			}
			if err := atomicWriteFile(conflictAbs, data, 0o644, remoteModTime); err != nil {
				return "", err
			}
			return conflictRel, nil
		}
		// Remote wins: preserve local copy before overwriting.
		localData, rErr := os.ReadFile(abs)
		if rErr == nil {
			conflictRel := conflictRelPath(relPath, "local")
			if conflictAbs, cErr := safeJoin(projectRoot, conflictRel); cErr == nil {
				_ = atomicWriteFile(conflictAbs, localData, 0o644, fi.ModTime())
				conflictPath = conflictRel
			}
		}
	}

	if err := atomicWriteFile(abs, data, 0o644, remoteModTime); err != nil {
		return "", err
	}
	return conflictPath, nil
}

// RebuildWorkspaceGlue ensures workspaces/_shared exists and every bot workspace
// has a valid _shared -> ../_shared symlink.
func RebuildWorkspaceGlue(cfg *config.HubConfig) error {
	if cfg == nil {
		return nil
	}
	sharedDir := cfg.SharedDir
	if sharedDir == "" && cfg.ProjectRoot != "" {
		sharedDir = filepath.Join(cfg.ProjectRoot, "workspaces", "_shared")
	}
	if sharedDir != "" {
		if err := os.MkdirAll(sharedDir, 0o755); err != nil {
			return err
		}
	}
	for _, b := range cfg.Bots {
		ws := b.Workspace
		if ws == "" && cfg.ProjectRoot != "" {
			ws = filepath.Join(cfg.ProjectRoot, "workspaces", b.ID)
		}
		if ws == "" {
			continue
		}
		if err := os.MkdirAll(ws, 0o755); err != nil {
			return err
		}
		if sharedDir != "" {
			linkPath := filepath.Join(ws, "_shared")
			if _, err := os.Lstat(linkPath); os.IsNotExist(err) {
				relTarget, rErr := filepath.Rel(ws, sharedDir)
				if rErr != nil {
					relTarget = "../_shared"
				}
				_ = os.Symlink(relTarget, linkPath)
			}
		}
		bCopy := b
		bCopy.Workspace = ws
		_ = config.EnsureWorkspaceSkill(&bCopy)
	}
	return nil
}

func fileSHA256(absPath string) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeJoin(root, relPath string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/"))
	if clean == "" || clean == "." || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("invalid relative path %q", relPath)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absTarget := filepath.Join(absRoot, filepath.FromSlash(clean))
	if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %q", relPath)
	}
	return absTarget, nil
}

func atomicWriteFile(absPath string, data []byte, perm os.FileMode, modTime time.Time) error {
	dir := filepath.Dir(absPath)
	tmp, err := os.CreateTemp(dir, ".sync-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, absPath); err != nil {
		return err
	}
	cleanup = false
	if !modTime.IsZero() {
		_ = os.Chtimes(absPath, modTime, modTime)
	}
	return nil
}

func conflictRelPath(relPath, hostTag string) string {
	clean := path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
	dir := path.Dir(clean)
	base := path.Base(clean)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	name := fmt.Sprintf("%s.conflict-%s%s", stem, hostTag, ext)
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

func sanitizeHostTag(host string) string {
	h := strings.TrimSpace(strings.ToLower(host))
	if h == "" {
		return "peer"
	}
	var b strings.Builder
	for _, r := range h {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "peer"
	}
	return s
}
