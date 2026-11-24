package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/state"
	"bot-hub/internal/syncer"
	"bot-hub/internal/tunnel"
)

func (s *HTTPServer) syncService() *syncer.Syncer {
	s.syncOnce.Do(func() {
		var cfg *config.HubConfig
		var store *state.Store
		if s.hub != nil {
			cfg = s.hub.Config()
			store = s.hub.Store()
		}
		s.syncSvc = syncer.NewWithStore(cfg, store)
	})
	return s.syncSvc
}

func (s *HTTPServer) handleSyncStatus(w http.ResponseWriter, _ *http.Request) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, s.syncService().Status())
}

func (s *HTTPServer) handleSyncPortableGet(w http.ResponseWriter, _ *http.Request) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	svc := s.syncService()
	writeJSON(w, http.StatusOK, config.ExportPortable(s.hub.Config(), svc.HostID()))
}

func (s *HTTPServer) handleSyncPortablePost(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	var bundle config.PortableBundle
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&bundle); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid portable bundle json"})
		return
	}
	cfg := s.hub.Config()
	changed, added, updated, err := config.ApplyPortable(cfg, bundle)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if err := syncer.RebuildWorkspaceGlue(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"changed": changed,
		"added":   added,
		"updated": updated,
	})
}

func (s *HTTPServer) handleSyncManifest(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	includeMedia := r.URL.Query().Get("media") == "1" || strings.EqualFold(r.URL.Query().Get("media"), "true")
	svc := s.syncService()
	m, err := syncer.BuildManifestWithStore(s.hub.Config().ProjectRoot, svc.HostID(), s.hub.Store(), includeMedia)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *HTTPServer) handleSyncFileGet(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	rel := strings.TrimSpace(r.URL.Query().Get("path"))
	data, entry, err := syncer.ReadSyncedFile(s.hub.Config().ProjectRoot, rel)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Sync-SHA256", entry.SHA256)
	w.Header().Set("X-Sync-Mtime", strconv.FormatInt(entry.ModTimeUnix, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *HTTPServer) handleSyncFilePut(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	rel := strings.TrimSpace(r.URL.Query().Get("path"))
	if !syncer.ShouldSyncPath(rel) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path is not syncable"})
		return
	}
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	deleted := r.URL.Query().Get("deleted") == "1" || strings.EqualFold(r.URL.Query().Get("deleted"), "true")
	mtimeSec := int64(0)
	if rawMtime := strings.TrimSpace(r.URL.Query().Get("mtime")); rawMtime != "" {
		if sec, err := strconv.ParseInt(rawMtime, 10, 64); err == nil && sec > 0 {
			mtimeSec = sec
		}
	}
	var data []byte
	if !deleted {
		var rErr error
		data, rErr = io.ReadAll(io.LimitReader(r.Body, syncer.MaxSyncFileSize+1))
		if rErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": rErr.Error()})
			return
		}
		if len(data) > syncer.MaxSyncFileSize {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "file exceeds max sync size"})
			return
		}
	}
	sha := strings.TrimSpace(r.URL.Query().Get("sha256"))
	if !deleted && sha == "" {
		sum := sha256.Sum256(data)
		sha = hex.EncodeToString(sum[:])
	}
	entry := syncer.FileEntry{
		Path:        rel,
		Size:        int64(len(data)),
		ModTimeUnix: mtimeSec,
		SHA256:      sha,
		Deleted:     deleted,
		VC:          syncer.ParseVectorClock(r.URL.Query().Get("vc")),
		UpdatedBy:   host,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	svc := s.syncService()
	_, conflict, err := syncer.ApplyRemoteFileEntry(
		s.hub.Config().ProjectRoot,
		svc.HostID(),
		s.hub.Store(),
		entry,
		func() ([]byte, error) { return data, nil },
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"path":         rel,
		"conflictPath": conflict,
	})
}

func (s *HTTPServer) handleSyncGitCheckpoint(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	svc := s.syncService()
	res, err := syncer.CheckpointPush(r.Context(), s.hub.Config(), svc.HostID())
	svc.RecordCheckpoint(err == nil, err)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleSyncGitPull(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	res, err := syncer.CheckpointPull(r.Context(), s.hub.Config())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleSyncDeltaGet(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil || s.hub.Store() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store unavailable"})
		return
	}
	var afterMsgID int64
	if raw := strings.TrimSpace(r.URL.Query().Get("afterMsgId")); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
			afterMsgID = n
		}
	}
	afterRunAt := strings.TrimSpace(r.URL.Query().Get("afterRunAt"))
	limit := 500
	if rawLim := strings.TrimSpace(r.URL.Query().Get("limit")); rawLim != "" {
		if n, err := strconv.Atoi(rawLim); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	store := s.hub.Store()
	msgs, nextMsg, err := store.ExportMessagesSince(afterMsgID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	runs, nextRun, err := store.ExportRoutineRunsSince(afterRunAt, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, state.SyncDelta{
		Messages:      msgs,
		NextMsgCursor: nextMsg,
		RoutineRuns:   runs,
		NextRunCursor: nextRun,
	})
}

func (s *HTTPServer) handleSyncDeltaPost(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil || s.hub.Store() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store unavailable"})
		return
	}
	var delta state.SyncDelta
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&delta); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid delta json"})
		return
	}
	store := s.hub.Store()
	msgIns, err := store.ImportMessages(delta.Messages)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	runIns, err := store.ImportRoutineRuns(delta.RoutineRuns)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"messagesInserted":    msgIns,
		"routineRunsInserted": runIns,
	})
}

func (s *HTTPServer) handleSyncPeerPull(w http.ResponseWriter, r *http.Request) {
	s.handleSyncPeer(w, r, true)
}

func (s *HTTPServer) handleSyncPeerPush(w http.ResponseWriter, r *http.Request) {
	s.handleSyncPeer(w, r, false)
}

func (s *HTTPServer) handleSyncPeer(w http.ResponseWriter, r *http.Request, pull bool) {
	if s.hub == nil || s.hub.Config() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	var body struct {
		PeerURL   string `json:"peerUrl"`
		PeerToken string `json:"peerToken"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body)

	peerURL := strings.TrimSpace(body.PeerURL)
	peerToken := strings.TrimSpace(body.PeerToken)
	if peerURL == "" && s.hub.Config().Sync != nil {
		peerURL = s.hub.Config().Sync.PeerURL
	}
	if peerToken == "" && s.hub.Config().Sync != nil {
		peerToken = s.hub.Config().Sync.PeerToken
	}

	cli := &http.Client{Timeout: 30 * time.Second}
	if strings.HasPrefix(peerURL, "tailcat://") {
		link, linkTok := tunnel.ExtractTokenFromLink(peerURL)
		if peerToken == "" {
			peerToken = linkTok
		}
		dial, closeClient, err := tunnel.Connect(r.Context(), link)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		defer func() { _ = closeClient() }()
		cli = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) { return dial(dctx) },
			},
		}
		peerURL = "http://tailcat.local"
	}

	svc := s.syncService()
	var (
		summary syncer.SyncSummary
		err     error
	)
	if pull {
		summary, err = svc.PullFromPeer(r.Context(), cli, peerURL, peerToken)
	} else {
		summary, err = svc.PushToPeer(r.Context(), cli, peerURL, peerToken)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *HTTPServer) mountWebDAVIfEnabled(mux *http.ServeMux) {
	if s.hub == nil || s.hub.Config() == nil {
		return
	}
	cfg := s.hub.Config()
	if !cfg.Sync.WebDAVEnabled() {
		return
	}
	prefix := cfg.Sync.WebDAV.EffectivePrefix()
	wsRoot := filepath.Join(cfg.ProjectRoot, "workspaces")
	dav := syncer.NewWebDAVHandler(wsRoot, cfg.Sync.WebDAVReadOnly())
	mux.Handle(prefix+"/", http.StripPrefix(prefix, dav))
	mux.Handle(prefix, http.StripPrefix(prefix, dav))
}
