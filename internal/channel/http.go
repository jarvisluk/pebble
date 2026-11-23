package channel

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/desktop"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/node"
	"bot-hub/internal/scheduler"
	"bot-hub/internal/state"
	"bot-hub/internal/web"
)

type RoutineScheduler interface {
	Status(botID string) map[string]scheduler.RoutineRuntimeStatus
	RunNow(ctx context.Context, botID, routineID string) (*state.RoutineRun, error)
	TriggerWebhook(ctx context.Context, botID, routineID, payload string) (*state.RoutineRun, error)
}

type HTTPServer struct {
	hub     *hub.Hub
	cfgMu   sync.RWMutex
	cfg     config.HTTPConfig
	srv     *http.Server
	schedMu sync.RWMutex
	sched   RoutineScheduler

	tailcat        tailcatServe
	tailscale      tailscaleServe
	nodesOnce      sync.Once
	fallbackNodes  *node.Registry
	deskOnce       sync.Once
	fallbackDesk   *desktop.Coordinator
	fallbackEngine *desktop.Engine

	mediaTicketsMu sync.Mutex
	mediaTickets   map[string]mediaTicket
}

type mediaTicket struct {
	mediaID   string
	expiresAt time.Time
}

func (s *HTTPServer) SetScheduler(sched RoutineScheduler) {
	s.schedMu.Lock()
	s.sched = sched
	s.schedMu.Unlock()
}

func (s *HTTPServer) scheduler() RoutineScheduler {
	s.schedMu.RLock()
	defer s.schedMu.RUnlock()
	return s.sched
}

func (s *HTTPServer) apiToken() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.APIToken
}

func NewHTTPServer(h *hub.Hub, cfg config.HTTPConfig) *HTTPServer {
	s := &HTTPServer{hub: h, cfg: cfg}
	mux := http.NewServeMux()

	// Web UI
	mux.HandleFunc("GET /", s.handleIndex)

	// Health & SSE
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/events", s.handleEvents)

	// Bots API
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/bots", s.handleStatus)
	mux.HandleFunc("POST /api/bots", s.handleCreateBot)
	mux.HandleFunc("GET /api/bots/{id}", s.handleBotDetail)
	mux.HandleFunc("GET /api/bots/{id}/messages", s.handleBotMessages)
	mux.HandleFunc("GET /api/bots/{id}/collab/{peerId}", s.handleBotCollabMessages)
	mux.HandleFunc("POST /api/bots/{id}/message", s.handleMessage)
	mux.HandleFunc("POST /api/bots/{id}/outbound", s.handleOutboundMessage)
	mux.HandleFunc("GET /api/bots/{id}/commands", s.handleBotCommands)
	mux.HandleFunc("GET /api/bots/{id}/mentions", s.handleBotMentions)
	mux.HandleFunc("POST /api/bots/{id}/attachments", s.handleUploadAttachment)
	mux.HandleFunc("POST /api/media/{id}/ticket", s.handleMediaTicket)
	mux.HandleFunc("GET /api/media/{id}", s.handleMedia)
	mux.HandleFunc("POST /api/bots/{id}/reset", s.handleReset)
	mux.HandleFunc("POST /api/bots/{id}/rotate", s.handleRotate)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/bots/{id}/sessions", s.handleListBotSessions)
	mux.HandleFunc("GET /api/sessions/{sessionId}", s.handleRecallSession)
	mux.HandleFunc("GET /api/bots/{id}/sessions/{sessionId}", s.handleRecallBotSession)
	mux.HandleFunc("GET /api/memory", s.handleMemorySearch)
	mux.HandleFunc("POST /api/memory", s.handleMemorySave)
	mux.HandleFunc("DELETE /api/memory/{id}", s.handleMemoryDelete)
	mux.HandleFunc("PATCH /api/bots/{id}", s.handleUpdateBot)
	mux.HandleFunc("DELETE /api/bots/{id}", s.handleDeleteBot)
	mux.HandleFunc("POST /api/bots/{id}/interrupt", s.handleInterrupt)
	mux.HandleFunc("GET /api/questions", s.handleQuestions)
	mux.HandleFunc("POST /api/questions/{id}/reply", s.handleQuestionReply)
	mux.HandleFunc("GET /api/bots/{id}/routines", s.handleListRoutines)
	mux.HandleFunc("POST /api/bots/{id}/routines", s.handleCreateRoutine)
	mux.HandleFunc("PATCH /api/bots/{id}/routines/{routineId}", s.handleUpdateRoutine)
	mux.HandleFunc("DELETE /api/bots/{id}/routines/{routineId}", s.handleDeleteRoutine)
	mux.HandleFunc("POST /api/bots/{id}/routines/{routineId}/run", s.handleRunRoutine)
	mux.HandleFunc("GET /api/bots/{id}/routine-runs", s.handleListRoutineRuns)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	mux.HandleFunc("POST /api/push/register", s.handlePushRegister)
	mux.HandleFunc("POST /api/push/activity-token", s.handlePushActivityToken)
	mux.HandleFunc("DELETE /api/push/devices/{deviceId}", s.handlePushDeleteDevice)
	mux.HandleFunc("GET /api/push/status", s.handlePushStatus)
	mux.HandleFunc("GET /api/models", s.handleModels)
	mux.HandleFunc("POST /api/harnesses/{id}/magpie", s.handleMagpieConnect)
	mux.HandleFunc("POST /api/magpie/providers", s.handleMagpieProvider)
	mux.HandleFunc("POST /api/magpie/open", s.handleMagpieOpen)

	mux.HandleFunc("GET /api/desktop/targets", s.handleDesktopTargets)
	mux.HandleFunc("PUT /api/desktop/target", s.handleDesktopTargetSet)
	mux.HandleFunc("POST /api/desktop/target", s.handleDesktopTargetSet)
	mux.HandleFunc("GET /api/desktop/state", s.handleDesktopState)
	mux.HandleFunc("GET /api/desktop/permissions", s.handleDesktopPermissions)
	mux.HandleFunc("POST /api/desktop/permissions/request", s.handleDesktopPermissionRequest)
	mux.HandleFunc("GET /api/desktop/windows", s.handleDesktopWindows)
	mux.HandleFunc("POST /api/desktop/focus", s.handleDesktopFocus)
	mux.HandleFunc("GET /api/desktop/frame", s.handleDesktopFrame)
	mux.HandleFunc("POST /api/desktop/input", s.handleDesktopInput)
	mux.HandleFunc("POST /api/desktop/stream/offer", s.handleDesktopStreamOffer)
	mux.HandleFunc("POST /api/desktop/stream/close", s.handleDesktopStreamClose)
	mux.HandleFunc("GET /api/desktop/stream/ws", s.handleDesktopStreamWS)
	mux.HandleFunc("POST /api/desktop/setup", s.handleDesktopSetup)
	mux.HandleFunc("POST /api/desktop/power", s.handleDesktopPower)
	mux.HandleFunc("GET /api/desktop/takeovers", s.handleDesktopTakeovers)
	mux.HandleFunc("POST /api/desktop/takeover/request", s.handleDesktopTakeoverRequest)
	mux.HandleFunc("POST /api/desktop/takeover/resolve", s.handleDesktopTakeoverResolve)
	mux.HandleFunc("POST /api/desktop/task", s.handleDesktopTask)

	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/tailcat", s.handleTailcat)
	mux.HandleFunc("POST /api/tailcat", s.handleSetTailcat)
	mux.HandleFunc("GET /api/tailcat/qr.png", s.handleTailcatQR)
	mux.HandleFunc("GET /api/tailscale", s.handleTailscale)
	mux.HandleFunc("POST /api/tailscale", s.handleSetTailscale)
	mux.HandleFunc("GET /api/tailscale/qr.png", s.handleTailscaleQR)

	mux.HandleFunc("GET /api/nodes", s.handleNodes)
	mux.HandleFunc("GET /api/local-node", s.handleLocalNodeStub)
	mux.HandleFunc("POST /api/nodes/poll", s.handleNodePoll)
	mux.HandleFunc("POST /api/nodes/result", s.handleNodeResult)
	mux.HandleFunc("POST /api/nodes/disconnect", s.handleNodeDisconnect)
	mux.HandleFunc("POST /api/nodes/{id}/call", s.handleNodeCall)

	// External Webhooks
	mux.HandleFunc("POST /hooks/{id}", s.handleWebhook)
	mux.HandleFunc("POST /hooks/{id}/routines/{routineId}", s.handleRoutineWebhook)

	s.srv = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           s.guard(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

func (s *HTTPServer) Start() error {
	if !s.cfg.IsLoopback() && s.cfg.APIToken == "" {
		return fmt.Errorf("refusing to listen on %s without http.apiToken (bots run tools with autoApprove)", s.cfg.Host)
	}
	if s.cfg.TailcatEnabled() && s.cfg.APIToken == "" {
		return errors.New("refusing to enable http.tailcat without http.apiToken (bots run tools with autoApprove)")
	}
	if s.cfg.TailscaleEnabled() && s.cfg.APIToken == "" {
		return errors.New("refusing to enable http.tailscale without http.apiToken (bots run tools with autoApprove)")
	}
	s.startTailcat(s.srv.Handler)
	s.startTailscale(s.srv.Handler)
	if s.hub != nil {
		kinds := s.hub.HarnessKinds()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			harness.WarmMagpieCache(ctx, kinds)
		}()
	}
	log.Printf("[http] Web UI & API listening on http://%s", s.srv.Addr)
	err := s.srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.tailcat.shutdown(ctx)
	s.tailscale.shutdown(ctx)
	if s.fallbackEngine != nil {
		s.fallbackEngine.Close()
	}
	return s.srv.Shutdown(ctx)
}

const authCookie = "bot_hub_token"

// guard blocks cross-site requests (CSRF / DNS rebinding) and enforces the API token.
func (s *HTTPServer) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Browsers always send Origin on cross-site POSTs; reject any origin other than ourselves.
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin request rejected"})
				return
			}
		}
		// Webhooks carry their own secret and may arrive through a tunnel with any Host.
		if strings.HasPrefix(r.URL.Path, "/hooks/") || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		tunneled := viaTailcat(r) || viaTailscale(r)
		// DNS-rebinding protection: a loopback server only answers loopback host names.
		// Tunneled requests carry the client's own Host and always need the token below.
		if s.cfg.IsLoopback() && !tunneled {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if host != "127.0.0.1" && host != "localhost" && host != "::1" {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "invalid host"})
				return
			}
		}
		if (s.apiToken() != "" || tunneled) &&
			r.URL.Path != "/" &&
			!strings.HasPrefix(r.URL.Path, "/assets/") &&
			r.URL.Path != "/api/login" &&
			!s.authorized(r) &&
			!(r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/media/") && s.validMediaTicket(strings.TrimPrefix(r.URL.Path, "/api/media/"), r.URL.Query().Get("ticket"))) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) authorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		if c, err := r.Cookie(authCookie); err == nil {
			got = c.Value
		}
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.apiToken())) == 1
}

func (s *HTTPServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	token := s.apiToken()
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "token 不正确"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   30 * 24 * 3600,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	dist := web.DistFS()
	clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if clean == "" || clean == "." {
		data, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "web ui not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	if info, err := fs.Stat(dist, clean); err == nil && !info.IsDir() {
		if strings.HasPrefix(clean, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServer(http.FS(dist)).ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	subID, ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(subID)

	// Send an initial ping so clients mark connected immediately.
	fmt.Fprintf(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case evt, ok := <-ch:
			if !ok {
				return
			}
			raw, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, raw)
			flusher.Flush()
		}
	}
}

func (s *HTTPServer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	sts, err := s.hub.Status("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": sts})
}

func (s *HTTPServer) handleCreateBot(w http.ResponseWriter, r *http.Request) {
	var req hub.CreateBotRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	st, err := s.hub.CreateBot(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, st)
}

func (s *HTTPServer) handleBotDetail(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	detail, err := s.hub.GetBotDetail(botID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *HTTPServer) handleBotMessages(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	limit := 200
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	msgs, err := s.hub.Messages(botID, limit)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *HTTPServer) handleBotCollabMessages(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	peerID := r.PathValue("peerId")
	limit := 200
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	msgs, err := s.hub.CollabMessages(botID, peerID, limit)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

type outboundMessageReq struct {
	Text   string              `json:"text"`
	Images []hub.OutboundImage `json:"images,omitempty"`
	Files  []hub.OutboundFile  `json:"files,omitempty"`
}

func (s *HTTPServer) handleOutboundMessage(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var req outboundMessageReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	msg, err := s.hub.SendOutboundMessage(botID, req.Text, req.Images, req.Files...)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

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

type messageReq struct {
	Message   string   `json:"message"`
	FromBotID string   `json:"fromBotId,omitempty"`
	Hop       int      `json:"hop,omitempty"`
	Source    string   `json:"source,omitempty"`
	MediaIDs  []string `json:"mediaIds,omitempty"`
}

func (s *HTTPServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var req messageReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" && len(req.MediaIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "message or mediaIds is required"})
		return
	}

	if req.FromBotID != "" {
		res, err := s.hub.MessageBot(r.Context(), req.FromBotID, botID, req.Message, req.Hop)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	src := req.Source
	if src == "" {
		src = "web"
	}
	res, err := s.hub.HandleInput(r.Context(), botID, req.Message, hub.SendOptions{
		Source:   src,
		MediaIDs: req.MediaIDs,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleBotCommands(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	cmds, err := s.hub.ListBotCommands(r.Context(), botID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	if cmds == nil {
		cmds = []harness.SlashCommand{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": cmds})
}

func (s *HTTPServer) handleBotMentions(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	q := r.URL.Query().Get("q")
	limit := 30
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if n, err := strconv.Atoi(rawLimit); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	items, err := s.hub.ListBotMentions(r.Context(), botID, q, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	if items == nil {
		items = []hub.MentionCandidate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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

func (s *HTTPServer) handleUpdateBot(w http.ResponseWriter, r *http.Request) {
	var upd hub.BotUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&upd); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	st, err := s.hub.UpdateBot(r.PathValue("id"), upd)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *HTTPServer) handleDeleteBot(w http.ResponseWriter, r *http.Request) {
	if err := s.hub.DeleteBot(r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	ok, err := s.hub.Interrupt(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interrupted": ok})
}

type settingsBody struct {
	DefaultHarness      *string           `json:"defaultHarness,omitempty"`
	DefaultModel        *string           `json:"defaultModel,omitempty"`
	DefaultModels       map[string]string `json:"defaultModels,omitempty"`
	DesktopHarness      *string           `json:"desktopHarness,omitempty"`
	DesktopModel        *string           `json:"desktopModel,omitempty"`
	SharedUserMD        *string           `json:"sharedUserMd,omitempty"`
	MagpieWebURL        *string           `json:"magpieWebUrl,omitempty"`
	OnboardingCompleted *bool             `json:"onboardingCompleted,omitempty"`
}

func (s *HTTPServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if r.URL.Query().Get("fresh") == "1" {
		harness.InvalidateMagpieCache()
	}
	defaultModel, defaultModels := s.hub.DefaultModelsSnapshot()
	deskHarness, deskModel := s.hub.DesktopWorkerSnapshot()
	installed := harness.MagpieInstalled()
	var (
		healthy   bool
		providers []harness.MagpieProvider
		presets   []harness.MagpiePreset
		harnesses []hub.HarnessInfo
		wg        sync.WaitGroup
	)
	wg.Add(1)
	go func() { defer wg.Done(); harnesses = s.hub.Harnesses(ctx) }()
	if installed {
		wg.Add(3)
		go func() { defer wg.Done(); healthy = harness.MagpieGatewayHealthy(ctx) }()
		go func() { defer wg.Done(); providers = harness.MagpieListProviders(ctx) }()
		go func() { defer wg.Done(); presets = harness.MagpiePresets(ctx) }()
	}
	wg.Wait()
	if providers == nil {
		providers = []harness.MagpieProvider{}
	}
	if presets == nil {
		presets = []harness.MagpiePreset{}
	}
	installedIDs := []string{}
	for _, hi := range harnesses {
		if hi.Installed {
			installedIDs = append(installedIDs, hi.ID)
		}
	}
	completed := s.hub.OnboardingCompleted()
	hasConversation := s.hub.HasAnyConversation()
	writeJSON(w, http.StatusOK, map[string]any{
		"magpie": map[string]any{
			"installed": installed,
			"healthy":   healthy,
			"install":   harness.MagpieInstall,
			"providers": providers,
			"presets":   presets,
			"webUrl":    s.hub.MagpieWebURL(),
		},
		"desktopHarness": deskHarness,
		"desktopModel":   deskModel,
		"defaultHarness": s.hub.DefaultHarness(),
		"defaultModel":   defaultModel,
		"defaultModels":  defaultModels,
		"sharedUserMd":   s.hub.SharedUserMD(),
		"harnesses":      harnesses,
		"onboarding": map[string]any{
			"completed":          completed,
			"needsSetup":         !completed && !hasConversation,
			"installedHarnesses": installedIDs,
		},
	})
}

func (s *HTTPServer) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if body.DesktopHarness != nil || body.DesktopModel != nil {
		curHarness, curModel := s.hub.DesktopWorkerSnapshot()
		if body.DesktopHarness != nil {
			curHarness = *body.DesktopHarness
		}
		if body.DesktopModel != nil {
			curModel = *body.DesktopModel
		}
		if err := s.hub.SetDesktopWorker(curHarness, curModel); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.DefaultHarness != nil {
		if err := s.hub.SetDefaultHarness(*body.DefaultHarness); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.DefaultModel != nil || body.DefaultModels != nil {
		curDefault, curPerHarness := s.hub.DefaultModelsSnapshot()
		if body.DefaultModel != nil {
			curDefault = *body.DefaultModel
		}
		if body.DefaultModels != nil {
			curPerHarness = body.DefaultModels
		}
		if err := s.hub.SetDefaultModels(curDefault, curPerHarness); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.SharedUserMD != nil {
		if err := s.hub.SetSharedUserMD(*body.SharedUserMD); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.MagpieWebURL != nil {
		if err := s.hub.SetMagpieWebURL(*body.MagpieWebURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if body.OnboardingCompleted != nil {
		if err := s.hub.SetOnboardingCompleted(*body.OnboardingCompleted); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	s.handleSettings(w, r)
}

func (s *HTTPServer) handleMagpieConnect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"` // connect (default) | disconnect
		Model  string `json:"model"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	kind := r.PathValue("id")
	var err error
	if strings.EqualFold(strings.TrimSpace(body.Action), "disconnect") {
		err = s.hub.MagpieDisconnect(ctx, kind)
	} else {
		err = s.hub.MagpieConnect(ctx, kind, body.Model)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.handleSettings(w, r)
}

func (s *HTTPServer) handleMagpieOpen(w http.ResponseWriter, r *http.Request) {
	if err := harness.MagpieOpen(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleMagpieProvider(w http.ResponseWriter, r *http.Request) {
	var req harness.MagpieProviderReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := s.hub.MagpieProviderAction(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	providers := harness.MagpieListProviders(ctx)
	if providers == nil {
		providers = []harness.MagpieProvider{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"output":    out,
		"providers": providers,
	})
}

func (s *HTTPServer) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	q := r.URL.Query()
	models, err := s.hub.ModelsWithSource(ctx, q.Get("harness"), q.Get("source"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if models == nil {
		models = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *HTTPServer) handleReset(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	prev, existed, err := s.hub.ResetSession(botID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reset":           existed,
		"previousSession": prev.SessionID,
		"sessionId":       prev.ID,
	})
}

func (s *HTTPServer) handleRotate(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var body struct {
		HandoffNote string `json:"handoffNote"`
		Defer       bool   `json:"defer"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)
	res, err := s.hub.RotateSession(botID, body.HandoffNote, body.Defer)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	s.serveSessionList(w, r, r.URL.Query().Get("botId"))
}

func (s *HTTPServer) handleListBotSessions(w http.ResponseWriter, r *http.Request) {
	s.serveSessionList(w, r, r.PathValue("id"))
}

func (s *HTTPServer) serveSessionList(w http.ResponseWriter, r *http.Request, botID string) {
	q := r.URL.Query()
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	callerBotID := strings.TrimSpace(q.Get("callerBotId"))
	list, err := s.hub.ListSessions(callerBotID, state.SessionListOptions{
		BotID:         botID,
		Query:         q.Get("query"),
		Limit:         limit,
		IncludeActive: q.Get("includeActive") != "false",
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrForbidden) {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (s *HTTPServer) handleRecallSession(w http.ResponseWriter, r *http.Request) {
	s.serveSessionRecall(w, r, r.URL.Query().Get("botId"), r.PathValue("sessionId"))
}

func (s *HTTPServer) handleRecallBotSession(w http.ResponseWriter, r *http.Request) {
	s.serveSessionRecall(w, r, r.PathValue("id"), r.PathValue("sessionId"))
}

func (s *HTTPServer) serveSessionRecall(w http.ResponseWriter, r *http.Request, botID, sessionID string) {
	q := r.URL.Query()
	parseInt := func(name string) int {
		if v := q.Get(name); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
		return 0
	}
	callerBotID := strings.TrimSpace(q.Get("callerBotId"))
	res, err := s.hub.RecallSession(callerBotID, state.RecallOptions{
		SessionID: sessionID,
		BotID:     botID,
		Tail:      parseInt("tail"),
		Offset:    parseInt("offset"),
		Limit:     parseInt("limit"),
		Query:     q.Get("query"),
		MaxChars:  parseInt("maxChars"),
		MessageID: q.Get("messageId"),
	})
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, hub.ErrForbidden):
			status = http.StatusForbidden
		case errors.Is(err, state.ErrSessionNotFound):
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	q := r.URL.Query()
	limit := 30
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	res, err := s.hub.MemorySearch(ctx, q.Get("scope"), q.Get("botId"), q.Get("query"), q.Get("kind"), limit)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleMemorySave(w http.ResponseWriter, r *http.Request) {
	var req harness.MagpieMemorySaveReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	msg, err := s.hub.MemorySave(ctx, req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *HTTPServer) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	msg, err := s.hub.MemoryForget(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *HTTPServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebhookSecret != "" {
		got := r.Header.Get("X-Webhook-Secret")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.WebhookSecret)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid webhook secret"})
			return
		}
	}

	botID := r.PathValue("id")
	var payload map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json payload"})
		return
	}
	pretty, _ := json.MarshalIndent(payload, "", "  ")
	prompt := fmt.Sprintf("[Webhook Event]\n```json\n%s\n```", string(pretty))

	res, err := s.hub.Send(r.Context(), botID, prompt, hub.SendOptions{Source: "webhook"})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type routineDTO struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	EveryMinutes     int               `json:"everyMinutes,omitempty"`
	DailyAt          string            `json:"dailyAt,omitempty"`
	Cron             string            `json:"cron,omitempty"`
	WebhookTrigger   bool              `json:"webhookTrigger,omitempty"`
	WebhookPath      string            `json:"webhookPath,omitempty"`
	WebhookSecretSet bool              `json:"webhookSecretSet,omitempty"`
	WebhookURL       string            `json:"webhookUrl,omitempty"`
	Prompt           string            `json:"prompt"`
	Model            string            `json:"model,omitempty"`
	MainDelivery     string            `json:"mainDelivery"`
	Enabled          bool              `json:"enabled"`
	Running          bool              `json:"running"`
	NextRunAt        string            `json:"nextRunAt,omitempty"`
	LastRun          *state.RoutineRun `json:"lastRun,omitempty"`
}

func (s *HTTPServer) buildRoutineDTO(botID string, sc config.ScheduleConfig, rt map[string]scheduler.RoutineRuntimeStatus) routineDTO {
	rid := sc.EffectiveID()
	dto := routineDTO{
		ID:               rid,
		Name:             sc.Name,
		EveryMinutes:     sc.EveryMinutes,
		DailyAt:          sc.DailyAt,
		Cron:             sc.Cron,
		WebhookTrigger:   sc.WebhookTrigger,
		WebhookSecretSet: strings.TrimSpace(sc.WebhookSecret) != "",
		WebhookURL:       sc.WebhookURL,
		Prompt:           sc.Prompt,
		Model:            sc.Model,
		MainDelivery:     sc.EffectiveMainDelivery(),
		Enabled:          sc.IsEnabled(),
	}
	if sc.WebhookTrigger {
		dto.WebhookPath = fmt.Sprintf("/hooks/%s/routines/%s", botID, rid)
	}
	if st, ok := rt[rid]; ok {
		dto.Running = st.Running
		dto.NextRunAt = st.NextRunAt
		dto.LastRun = st.LastRun
	} else if last, ok := s.hub.LatestRoutineRun(botID, rid); ok {
		copyLast := last
		dto.LastRun = &copyLast
	}
	return dto
}

func (s *HTTPServer) routineStatusMap(botID string) map[string]scheduler.RoutineRuntimeStatus {
	if sched := s.scheduler(); sched != nil {
		return sched.Status(botID)
	}
	return nil
}

func writeHubErr(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, hub.ErrBotNotFound), errors.Is(err, hub.ErrRoutineNotFound):
		status = http.StatusNotFound
	case errors.Is(err, scheduler.ErrRoutineAlreadyRunning):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func (s *HTTPServer) handleListRoutines(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	list, err := s.hub.ListRoutines(botID)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	rt := s.routineStatusMap(botID)
	out := make([]routineDTO, 0, len(list))
	for _, sc := range list {
		out = append(out, s.buildRoutineDTO(botID, sc, rt))
	}
	writeJSON(w, http.StatusOK, map[string]any{"routines": out})
}

func (s *HTTPServer) handleCreateRoutine(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var req config.ScheduleConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	var (
		saved config.ScheduleConfig
		err   error
	)
	if r.URL.Query().Get("upsert") == "1" {
		saved, err = s.hub.UpsertRoutine(botID, req)
	} else {
		saved, err = s.hub.CreateRoutine(botID, req)
	}
	if err != nil {
		writeHubErr(w, err)
		return
	}
	dto := s.buildRoutineDTO(botID, saved, s.routineStatusMap(botID))
	status := http.StatusCreated
	if r.URL.Query().Get("upsert") == "1" {
		status = http.StatusOK
	}
	writeJSON(w, status, dto)
}

func (s *HTTPServer) handleUpdateRoutine(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	routineID := r.PathValue("routineId")
	var patch hub.RoutinePatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	saved, err := s.hub.UpdateRoutine(botID, routineID, patch)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	dto := s.buildRoutineDTO(botID, saved, s.routineStatusMap(botID))
	writeJSON(w, http.StatusOK, dto)
}

func (s *HTTPServer) handleDeleteRoutine(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	routineID := r.PathValue("routineId")
	if err := s.hub.DeleteRoutine(botID, routineID); err != nil {
		writeHubErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleRunRoutine(w http.ResponseWriter, r *http.Request) {
	sched := s.scheduler()
	if sched == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "scheduler is not running"})
		return
	}
	botID := r.PathValue("id")
	routineID := r.PathValue("routineId")
	if r.URL.Query().Get("async") == "1" {
		list, err := s.hub.ListRoutines(botID)
		if err != nil {
			writeHubErr(w, err)
			return
		}
		found := false
		for _, sc := range list {
			if sc.EffectiveID() == routineID {
				found = true
				break
			}
		}
		if !found {
			writeHubErr(w, fmt.Errorf("routine %q not found: %w", routineID, hub.ErrRoutineNotFound))
			return
		}
		if rt := sched.Status(botID); rt[routineID].Running {
			writeHubErr(w, scheduler.ErrRoutineAlreadyRunning)
			return
		}
		go func() {
			_, _ = sched.RunNow(context.Background(), botID, routineID)
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "botId": botID, "routineId": routineID})
		return
	}
	run, err := sched.RunNow(r.Context(), botID, routineID)
	if err != nil {
		if errors.Is(err, hub.ErrBotNotFound) || errors.Is(err, hub.ErrRoutineNotFound) || errors.Is(err, scheduler.ErrRoutineAlreadyRunning) {
			writeHubErr(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "run": run})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *HTTPServer) handleListRoutineRuns(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	q := r.URL.Query()
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := s.hub.ListRoutineRuns(botID, q.Get("routineId"), limit)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *HTTPServer) handleRoutineWebhook(w http.ResponseWriter, r *http.Request) {
	sched := s.scheduler()
	if sched == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "scheduler is not running"})
		return
	}
	botID := r.PathValue("id")
	routineID := r.PathValue("routineId")

	list, err := s.hub.ListRoutines(botID)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	var target *config.ScheduleConfig
	for i := range list {
		if list[i].EffectiveID() == routineID {
			sc := list[i]
			target = &sc
			break
		}
	}
	if target == nil {
		writeHubErr(w, fmt.Errorf("routine %q not found: %w", routineID, hub.ErrRoutineNotFound))
		return
	}
	if !target.IsEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "routine is disabled"})
		return
	}
	if !target.WebhookTrigger {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "webhook trigger is not enabled for this routine"})
		return
	}

	expectedSecret := strings.TrimSpace(target.WebhookSecret)
	if expectedSecret == "" {
		expectedSecret = s.cfg.WebhookSecret
	}
	if expectedSecret != "" {
		got := r.Header.Get("X-Webhook-Secret")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(expectedSecret)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid webhook secret"})
			return
		}
	}

	rawBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "failed to read webhook payload"})
		return
	}
	payloadText := ""
	if trimmed := bytes.TrimSpace(rawBody); len(trimmed) > 0 {
		var formatted bytes.Buffer
		if json.Indent(&formatted, trimmed, "", "  ") == nil {
			payloadText = "```json\n" + formatted.String() + "\n```"
		} else {
			payloadText = string(trimmed)
		}
	}

	if r.URL.Query().Get("async") == "1" {
		if rt := sched.Status(botID); rt[routineID].Running {
			writeHubErr(w, scheduler.ErrRoutineAlreadyRunning)
			return
		}
		go func() {
			_, _ = sched.TriggerWebhook(context.Background(), botID, routineID, payloadText)
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "botId": botID, "routineId": routineID})
		return
	}

	run, err := sched.TriggerWebhook(r.Context(), botID, routineID, payloadText)
	if err != nil {
		if errors.Is(err, hub.ErrBotNotFound) || errors.Is(err, hub.ErrRoutineNotFound) || errors.Is(err, scheduler.ErrRoutineAlreadyRunning) {
			writeHubErr(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "run": run})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *HTTPServer) handleQuestions(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []hub.PendingQuestion{}})
		return
	}
	botID := strings.TrimSpace(r.URL.Query().Get("botId"))
	items := s.hub.ListQuestions(botID)
	if items == nil {
		items = []hub.PendingQuestion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleQuestionReply(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing question id"})
		return
	}
	var body struct {
		Answers   map[string]any `json:"answers"`
		Cancelled bool           `json:"cancelled"`
		Message   string         `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	items, err := s.hub.AnswerQuestion(id, body.Answers, body.Cancelled, body.Message)
	if err != nil {
		if errors.Is(err, hub.ErrQuestionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (s *HTTPServer) handlePushStatus(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "mode": "disabled", "deviceCount": 0})
		return
	}
	writeJSON(w, http.StatusOK, s.hub.PushStatus())
}

func (s *HTTPServer) handlePushRegister(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	var body struct {
		DeviceID               string `json:"deviceId"`
		Platform               string `json:"platform"`
		Env                    string `json:"env"`
		BundleID               string `json:"bundleId"`
		PushToken              string `json:"pushToken"`
		LiveActivityStartToken string `json:"liveActivityStartToken"`
		Enabled                *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(body.DeviceID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deviceId is required"})
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	err := s.hub.UpsertPushDevice(state.PushDevice{
		DeviceID:               body.DeviceID,
		Platform:               body.Platform,
		Env:                    body.Env,
		BundleID:               body.BundleID,
		PushToken:              body.PushToken,
		LiveActivityStartToken: body.LiveActivityStartToken,
		Enabled:                enabled,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": s.hub.PushStatus()})
}

func (s *HTTPServer) handlePushActivityToken(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	var body struct {
		DeviceID    string `json:"deviceId"`
		ActivityKey string `json:"activityKey"`
		Token       string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(body.DeviceID) == "" || strings.TrimSpace(body.ActivityKey) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deviceId and activityKey are required"})
		return
	}
	if err := s.hub.SetPushActivityToken(body.DeviceID, body.ActivityKey, body.Token); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handlePushDeleteDevice(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	deviceID := strings.TrimSpace(r.PathValue("deviceId"))
	if deviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deviceId is required"})
		return
	}
	if err := s.hub.DeletePushDevice(deviceID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
