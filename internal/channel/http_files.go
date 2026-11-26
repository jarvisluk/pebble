package channel

import (
	"bot-hub/internal/hub"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

func (s *HTTPServer) validMediaTicket(mediaID, ticket string) bool {
	if mediaID == "" || ticket == "" || strings.Contains(mediaID, "/") {
		return false
	}
	now := time.Now()
	s.mediaTicketsMu.Lock()
	defer s.mediaTicketsMu.Unlock()
	if s.mediaTickets == nil {
		return false
	}
	for k, v := range s.mediaTickets {
		if now.After(v.expiresAt) {
			delete(s.mediaTickets, k)
		}
	}
	entry, ok := s.mediaTickets[ticket]
	if !ok || now.After(entry.expiresAt) || entry.mediaID != mediaID {
		return false
	}
	return true
}

func (s *HTTPServer) handleMediaTicket(w http.ResponseWriter, r *http.Request) {
	mediaID := r.PathValue("id")
	if _, _, err := s.hub.ResolveMediaFile(mediaID); err != nil {
		status := http.StatusNotFound
		if strings.Contains(err.Error(), "invalid") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	rb := make([]byte, 16)
	if _, err := rand.Read(rb); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to issue ticket"})
		return
	}
	ticket := hex.EncodeToString(rb)
	now := time.Now()

	s.mediaTicketsMu.Lock()
	if s.mediaTickets == nil {
		s.mediaTickets = make(map[string]mediaTicket)
	}
	for k, v := range s.mediaTickets {
		if now.After(v.expiresAt) {
			delete(s.mediaTickets, k)
		}
	}
	s.mediaTickets[ticket] = mediaTicket{
		mediaID:   mediaID,
		expiresAt: now.Add(60 * time.Second),
	}
	s.mediaTicketsMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":    ticket,
		"expiresIn": 60,
	})
}

func isSafeInlineImageMIME(mimeType string) bool {
	base := strings.TrimSpace(strings.Split(mimeType, ";")[0])
	switch base {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func sanitizeASCIIName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 0x20 && r <= 0x7e && r != '"' && r != '\\' && r != ';' {
			b.WriteRune(r)
		} else if r > 0x7e {
			b.WriteByte('_')
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "download"
	}
	return out
}

func (s *HTTPServer) handleMedia(w http.ResponseWriter, r *http.Request) {
	mediaID := r.PathValue("id")
	filePath, mimeType, err := s.hub.ResolveMediaFile(mediaID)
	if err != nil {
		status := http.StatusNotFound
		if strings.Contains(err.Error(), "invalid") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("download") == "1" || !isSafeInlineImageMIME(mimeType) {
		dlName := strings.TrimSpace(r.URL.Query().Get("name"))
		if dlName == "" {
			dlName = mediaID
		} else {
			dlName = path.Base(dlName)
		}
		asciiName := sanitizeASCIIName(dlName)
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName, url.PathEscape(dlName)))
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	http.ServeFile(w, r, filePath)
}

func (s *HTTPServer) validWorkspaceFileRawRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	const prefix = "/api/bots/"
	const suffix = "/files/raw"
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, suffix) {
		return false
	}
	botID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), suffix)
	if botID == "" || strings.Contains(botID, "/") {
		return false
	}
	ticket := r.URL.Query().Get("ticket")
	relPath := r.URL.Query().Get("path")
	if ticket == "" || relPath == "" {
		return false
	}
	now := time.Now()
	s.fileTicketsMu.Lock()
	defer s.fileTicketsMu.Unlock()
	if s.fileTickets == nil {
		return false
	}
	for k, v := range s.fileTickets {
		if now.After(v.expiresAt) {
			delete(s.fileTickets, k)
		}
	}
	entry, ok := s.fileTickets[ticket]
	if !ok || now.After(entry.expiresAt) {
		return false
	}
	return entry.botID == botID && entry.relPath == relPath
}

func writeWorkspaceFileErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, hub.ErrBotNotFound), errors.Is(err, hub.ErrWorkspaceNotFound):
		status = http.StatusNotFound
	case errors.Is(err, hub.ErrWorkspacePathBlocked):
		status = http.StatusForbidden
	case errors.Is(err, hub.ErrWorkspaceIsDir), errors.Is(err, hub.ErrInvalidStageKind):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func (s *HTTPServer) handleBotFilesList(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	relPath := r.URL.Query().Get("path")
	showHidden := r.URL.Query().Get("showHidden") == "1" || strings.EqualFold(r.URL.Query().Get("showHidden"), "true")
	res, err := s.hub.ListWorkspaceFiles(botID, relPath, showHidden)
	if err != nil {
		writeWorkspaceFileErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleBotFileContent(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	relPath := r.URL.Query().Get("path")
	maxBytes := 0
	if rawMax := r.URL.Query().Get("maxBytes"); rawMax != "" {
		if n, err := strconv.Atoi(rawMax); err == nil && n > 0 {
			maxBytes = n
		}
	}
	res, err := s.hub.ReadWorkspaceFile(botID, relPath, maxBytes)
	if err != nil {
		writeWorkspaceFileErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleBotFileTicket(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	absPath, cleanRel, _, err := s.hub.ResolveWorkspacePath(botID, req.Path)
	if err != nil {
		writeWorkspaceFileErr(w, err)
		return
	}
	if cleanRel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path is a directory"})
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeWorkspaceFileErr(w, hub.ErrWorkspaceNotFound)
		return
	}
	if info.IsDir() {
		writeWorkspaceFileErr(w, hub.ErrWorkspaceIsDir)
		return
	}

	rb := make([]byte, 16)
	if _, err := rand.Read(rb); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to issue ticket"})
		return
	}
	ticket := hex.EncodeToString(rb)
	now := time.Now()

	s.fileTicketsMu.Lock()
	if s.fileTickets == nil {
		s.fileTickets = make(map[string]fileTicket)
	}
	for k, v := range s.fileTickets {
		if now.After(v.expiresAt) {
			delete(s.fileTickets, k)
		}
	}
	s.fileTickets[ticket] = fileTicket{
		botID:     botID,
		relPath:   cleanRel,
		expiresAt: now.Add(60 * time.Second),
	}
	s.fileTicketsMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":    ticket,
		"path":      cleanRel,
		"expiresIn": 60,
	})
}

func (s *HTTPServer) handleBotFileRaw(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	relPath := r.URL.Query().Get("path")
	absPath, cleanRel, _, err := s.hub.ResolveWorkspacePath(botID, relPath)
	if err != nil {
		writeWorkspaceFileErr(w, err)
		return
	}
	if cleanRel == "" {
		writeWorkspaceFileErr(w, hub.ErrWorkspaceIsDir)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeWorkspaceFileErr(w, hub.ErrWorkspaceNotFound)
		return
	}
	if info.IsDir() {
		writeWorkspaceFileErr(w, hub.ErrWorkspaceIsDir)
		return
	}

	meta, err := s.hub.ReadWorkspaceFile(botID, cleanRel, 512)
	mimeType := "application/octet-stream"
	if err == nil && meta.MimeType != "" {
		mimeType = meta.MimeType
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("download") == "1" || !isSafeInlineImageMIME(mimeType) {
		dlName := path.Base(cleanRel)
		asciiName := sanitizeASCIIName(dlName)
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName, url.PathEscape(dlName)))
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	http.ServeFile(w, r, absPath)
}

func (s *HTTPServer) handleBotStageOpen(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var req hub.StageOpenPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	out, err := s.hub.OpenBotStage(botID, req)
	if err != nil {
		writeWorkspaceFileErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 52<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid multipart upload"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "file field is required"})
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, (50<<20)+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "failed to read uploaded file"})
		return
	}
	if len(raw) > 50<<20 {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "file exceeds 50 MiB limit"})
		return
	}

	att, err := s.hub.SaveInboundAttachment(botID, header.Filename, raw)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, att)
}
