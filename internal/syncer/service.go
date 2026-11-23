package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
)

// SyncSummary summarizes a peer-to-peer sync or checkpoint operation.
type SyncSummary struct {
	Direction     string   `json:"direction"` // "pull" | "push"
	PeerURL       string   `json:"peerUrl,omitempty"`
	PortableBots  int      `json:"portableBots"`
	ConfigChanged bool     `json:"configChanged"`
	FilesChecked  int      `json:"filesChecked"`
	FilesSynced   int      `json:"filesSynced"`
	Conflicts     []string `json:"conflicts,omitempty"`
	CompletedAt   string   `json:"completedAt"`
}

// SyncStatus exposes current sync configuration and last run metadata.
type SyncStatus struct {
	HostID          string       `json:"hostId"`
	Role            string       `json:"role"`
	PeerURL         string       `json:"peerUrl,omitempty"`
	GitEnabled      bool         `json:"gitEnabled"`
	WebDAVEnabled   bool         `json:"webdavEnabled"`
	WebDAVPrefix    string       `json:"webdavPrefix,omitempty"`
	WebDAVReadOnly  bool         `json:"webdavReadOnly"`
	LastSync        *SyncSummary `json:"lastSync,omitempty"`
	LastCheckpoint  string       `json:"lastCheckpoint,omitempty"`
	LastCheckpointOK bool        `json:"lastCheckpointOk,omitempty"`
	LastError       string       `json:"lastError,omitempty"`
}

// Syncer coordinates portable config export/import, P2P file sync, and Git checkpoints.
type Syncer struct {
	cfg    *config.HubConfig
	hostID string

	mu               sync.Mutex
	lastSync         *SyncSummary
	lastCheckpoint   string
	lastCheckpointOK bool
	lastError        string
}

// New creates a Syncer bound to cfg.
func New(cfg *config.HubConfig) *Syncer {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "bot-hub"
	}
	return &Syncer{
		cfg:    cfg,
		hostID: host,
	}
}

func (s *Syncer) HostID() string {
	return s.hostID
}

func (s *Syncer) Status() SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := SyncStatus{
		HostID:           s.hostID,
		Role:             s.cfg.Sync.EffectiveRole(),
		GitEnabled:       s.cfg.Sync.GitEnabled(),
		WebDAVEnabled:    s.cfg.Sync.WebDAVEnabled(),
		WebDAVReadOnly:   s.cfg.Sync.WebDAVReadOnly(),
		LastSync:         s.lastSync,
		LastCheckpoint:   s.lastCheckpoint,
		LastCheckpointOK: s.lastCheckpointOK,
		LastError:        s.lastError,
	}
	if s.cfg.Sync != nil {
		st.PeerURL = s.cfg.Sync.PeerURL
		if s.cfg.Sync.WebDAV != nil && s.cfg.Sync.WebDAV.Enabled {
			st.WebDAVPrefix = s.cfg.Sync.WebDAV.EffectivePrefix()
		}
	}
	return st
}

func (s *Syncer) RecordCheckpoint(ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCheckpoint = time.Now().UTC().Format(time.RFC3339)
	s.lastCheckpointOK = ok
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.lastError = ""
	}
}

// PullFromPeer pulls portable bot config and changed workspace/media files from peerBaseURL.
func (s *Syncer) PullFromPeer(ctx context.Context, client *http.Client, peerBaseURL, peerToken string) (SyncSummary, error) {
	if s.cfg == nil {
		return SyncSummary{}, errors.New("syncer config is nil")
	}
	base, tok, cli := s.resolvePeer(client, peerBaseURL, peerToken)
	if base == "" {
		return SyncSummary{}, errors.New("peerUrl is required")
	}

	var bundle config.PortableBundle
	if err := doJSONRequest(ctx, cli, http.MethodGet, base+"/api/sync/portable", tok, nil, &bundle); err != nil {
		s.recordErr(err)
		return SyncSummary{}, fmt.Errorf("fetch peer portable config: %w", err)
	}
	changed, _, _, err := config.ApplyPortable(s.cfg, bundle)
	if err != nil {
		s.recordErr(err)
		return SyncSummary{}, fmt.Errorf("apply portable config: %w", err)
	}
	if err := RebuildWorkspaceGlue(s.cfg); err != nil {
		s.recordErr(err)
		return SyncSummary{}, err
	}

	var remoteManifest Manifest
	if err := doJSONRequest(ctx, cli, http.MethodGet, base+"/api/sync/manifest?media=1", tok, nil, &remoteManifest); err != nil {
		s.recordErr(err)
		return SyncSummary{}, fmt.Errorf("fetch peer manifest: %w", err)
	}
	localManifest, err := BuildManifest(s.cfg.ProjectRoot, s.hostID, true)
	if err != nil {
		s.recordErr(err)
		return SyncSummary{}, err
	}
	localMap := make(map[string]FileEntry, len(localManifest.Files))
	for _, f := range localManifest.Files {
		localMap[f.Path] = f
	}

	var synced int
	var conflicts []string
	for _, rf := range remoteManifest.Files {
		if lf, ok := localMap[rf.Path]; ok && lf.SHA256 == rf.SHA256 {
			continue
		}
		data, err := fetchPeerFile(ctx, cli, base, tok, rf.Path)
		if err != nil {
			s.recordErr(err)
			return SyncSummary{}, fmt.Errorf("fetch %s: %w", rf.Path, err)
		}
		modTime := time.Unix(rf.ModTimeUnix, 0)
		conflict, err := WriteSyncedFile(s.cfg.ProjectRoot, rf.Path, remoteManifest.HostID, data, modTime)
		if err != nil {
			s.recordErr(err)
			return SyncSummary{}, fmt.Errorf("write %s: %w", rf.Path, err)
		}
		synced++
		if conflict != "" {
			conflicts = append(conflicts, conflict)
		}
	}

	summary := SyncSummary{
		Direction:     "pull",
		PeerURL:       base,
		PortableBots:  len(bundle.Bots),
		ConfigChanged: changed,
		FilesChecked:  len(remoteManifest.Files),
		FilesSynced:   synced,
		Conflicts:     conflicts,
		CompletedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	s.recordSync(summary)
	return summary, nil
}

// PushToPeer pushes local portable bot config and changed files to peerBaseURL.
func (s *Syncer) PushToPeer(ctx context.Context, client *http.Client, peerBaseURL, peerToken string) (SyncSummary, error) {
	if s.cfg == nil {
		return SyncSummary{}, errors.New("syncer config is nil")
	}
	base, tok, cli := s.resolvePeer(client, peerBaseURL, peerToken)
	if base == "" {
		return SyncSummary{}, errors.New("peerUrl is required")
	}

	bundle := config.ExportPortable(s.cfg, s.hostID)
	var applyResp struct {
		Changed bool `json:"changed"`
	}
	if err := doJSONRequest(ctx, cli, http.MethodPost, base+"/api/sync/portable", tok, bundle, &applyResp); err != nil {
		s.recordErr(err)
		return SyncSummary{}, fmt.Errorf("push portable config: %w", err)
	}

	localManifest, err := BuildManifest(s.cfg.ProjectRoot, s.hostID, true)
	if err != nil {
		s.recordErr(err)
		return SyncSummary{}, err
	}
	var remoteManifest Manifest
	if err := doJSONRequest(ctx, cli, http.MethodGet, base+"/api/sync/manifest?media=1", tok, nil, &remoteManifest); err != nil {
		s.recordErr(err)
		return SyncSummary{}, fmt.Errorf("fetch peer manifest: %w", err)
	}
	remoteMap := make(map[string]FileEntry, len(remoteManifest.Files))
	for _, f := range remoteManifest.Files {
		remoteMap[f.Path] = f
	}

	var synced int
	var conflicts []string
	for _, lf := range localManifest.Files {
		if rf, ok := remoteMap[lf.Path]; ok && rf.SHA256 == lf.SHA256 {
			continue
		}
		data, entry, err := ReadSyncedFile(s.cfg.ProjectRoot, lf.Path)
		if err != nil {
			continue
		}
		conflict, err := pushPeerFile(ctx, cli, base, tok, s.hostID, entry, data)
		if err != nil {
			s.recordErr(err)
			return SyncSummary{}, fmt.Errorf("push %s: %w", lf.Path, err)
		}
		synced++
		if conflict != "" {
			conflicts = append(conflicts, conflict)
		}
	}

	summary := SyncSummary{
		Direction:     "push",
		PeerURL:       base,
		PortableBots:  len(bundle.Bots),
		ConfigChanged: applyResp.Changed,
		FilesChecked:  len(localManifest.Files),
		FilesSynced:   synced,
		Conflicts:     conflicts,
		CompletedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	s.recordSync(summary)
	return summary, nil
}

func (s *Syncer) resolvePeer(client *http.Client, peerBaseURL, peerToken string) (string, string, *http.Client) {
	base := strings.TrimRight(strings.TrimSpace(peerBaseURL), "/")
	tok := strings.TrimSpace(peerToken)
	if s.cfg != nil && s.cfg.Sync != nil {
		if base == "" {
			base = s.cfg.Sync.PeerURL
		}
		if tok == "" {
			tok = s.cfg.Sync.PeerToken
		}
	}
	cli := client
	if cli == nil {
		cli = &http.Client{Timeout: 30 * time.Second}
	}
	return base, tok, cli
}

func (s *Syncer) recordSync(summary SyncSummary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSync = &summary
	s.lastError = ""
}

func (s *Syncer) recordErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.lastError = err.Error()
	}
}

func doJSONRequest(ctx context.Context, cli *http.Client, method, targetURL, token string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func fetchPeerFile(ctx context.Context, cli *http.Client, base, token, relPath string) ([]byte, error) {
	u := base + "/api/sync/file?path=" + url.QueryEscape(relPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, MaxSyncFileSize+1))
}

func pushPeerFile(ctx context.Context, cli *http.Client, base, token, hostID string, entry FileEntry, data []byte) (string, error) {
	q := url.Values{}
	q.Set("path", entry.Path)
	q.Set("host", hostID)
	q.Set("mtime", strconv.FormatInt(entry.ModTimeUnix, 10))
	u := base + "/api/sync/file?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		ConflictPath string `json:"conflictPath"`
	}
	_ = json.Unmarshal(raw, &out)
	return out.ConflictPath, nil
}
