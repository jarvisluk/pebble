package channel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"bot-hub/internal/protocol"
	"bot-hub/internal/update"
)

func (s *HTTPServer) updateManager() *update.Manager {
	s.updaterMu.Lock()
	defer s.updaterMu.Unlock()
	if s.updater != nil {
		return s.updater
	}
	var stateDir string
	if s.hub != nil && s.hub.Config() != nil && strings.TrimSpace(s.hub.Config().StateFile) != "" {
		stateDir = filepath.Dir(s.hub.Config().StateFile)
	}
	s.updater = update.NewManager(update.Options{
		StateDir:       stateDir,
		CurrentVersion: protocol.AppVersion,
		OnChange: func(st update.Status) {
			if s.hub != nil {
				s.hub.BroadcastUpdateStatus(st)
			}
		},
	})
	return s.updater
}

func (s *HTTPServer) handleUpdateStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.updateManager().Status())
}

func (s *HTTPServer) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
		Proxy   string `json:"proxy"`
	}
	if r.Body != nil {
		err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body)
		if err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
			return
		}
	}
	if ch := strings.ToLower(strings.TrimSpace(body.Channel)); ch != "" && ch != update.ChannelStable && ch != update.ChannelPrerelease {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid update channel (expected stable or prerelease)"})
		return
	}
	if _, err := update.ValidateProxyURL(body.Proxy); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	st := s.updateManager().CheckNow(ctx, body.Channel, body.Proxy)
	writeJSON(w, http.StatusOK, st)
}

func (s *HTTPServer) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var patch update.ConfigPatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	st, err := s.updateManager().UpdateConfig(patch)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}
