package channel

import (
	"bot-hub/internal/config"
	"bot-hub/internal/desktop"
	"bot-hub/internal/hub"
	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
	"bot-hub/internal/scheduler"
	"bot-hub/internal/state"
	"bot-hub/internal/syncer"
	"bot-hub/internal/update"
	"bot-hub/internal/web"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RoutineScheduler interface {
	Status(botID string) map[string]scheduler.RoutineRuntimeStatus
	RunNow(ctx context.Context, botID, routineID string) (*state.RoutineRun, error)
	TriggerWebhook(ctx context.Context, botID, routineID, payload string) (*state.RoutineRun, error)
}

type HTTPServer struct {
	hub       *hub.Hub
	cfgMu     sync.RWMutex
	cfg       config.HTTPConfig
	srv       *http.Server
	schedMu   sync.RWMutex
	sched     RoutineScheduler
	updaterMu sync.RWMutex
	updater   *update.Manager

	tailcat        tailcatServe
	tailscale      tailscaleServe
	nodesOnce      sync.Once
	fallbackNodes  *node.Registry
	deskOnce       sync.Once
	fallbackDesk   *desktop.Coordinator
	fallbackEngine *desktop.EnginePool
	syncOnce       sync.Once
	syncSvc        *syncer.Syncer

	mediaTicketsMu sync.Mutex
	mediaTickets   map[string]mediaTicket

	fileTicketsMu sync.Mutex
	fileTickets   map[string]fileTicket

	sseResetMu sync.Mutex
	sseReset   chan struct{}
}

type mediaTicket struct {
	mediaID   string
	expiresAt time.Time
}

type fileTicket struct {
	botID     string
	relPath   string
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

func (s *HTTPServer) SetUpdater(u *update.Manager) {
	s.updaterMu.Lock()
	s.updater = u
	s.updaterMu.Unlock()
}

func (s *HTTPServer) apiToken() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.APIToken
}

func (s *HTTPServer) sseResetChan() <-chan struct{} {
	s.sseResetMu.Lock()
	defer s.sseResetMu.Unlock()
	if s.sseReset == nil {
		s.sseReset = make(chan struct{})
	}
	return s.sseReset
}

func (s *HTTPServer) notifyTokenChanged(token string) {
	s.sseResetMu.Lock()
	if s.sseReset != nil {
		close(s.sseReset)
	}
	s.sseReset = make(chan struct{})
	s.sseResetMu.Unlock()
	if fn := OnAPITokenChanged; fn != nil {
		fn(token)
	}
}

func NewHTTPServer(h *hub.Hub, cfg config.HTTPConfig) *HTTPServer {
	s := &HTTPServer{hub: h, cfg: cfg, sseReset: make(chan struct{})}
	mux := http.NewServeMux()

	// Web UI
	mux.HandleFunc("GET /", s.handleIndex)

	// Health, Meta & SSE
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/meta", s.handleMeta)
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
	mux.HandleFunc("GET /api/bots/{id}/files", s.handleBotFilesList)
	mux.HandleFunc("GET /api/bots/{id}/files/content", s.handleBotFileContent)
	mux.HandleFunc("POST /api/bots/{id}/files/ticket", s.handleBotFileTicket)
	mux.HandleFunc("GET /api/bots/{id}/files/raw", s.handleBotFileRaw)
	mux.HandleFunc("POST /api/bots/{id}/stage", s.handleBotStageOpen)
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
	mux.HandleFunc("GET /api/approvals", s.handleApprovals)
	mux.HandleFunc("POST /api/approvals/{id}/reply", s.handleApprovalReply)
	mux.HandleFunc("GET /api/bots/{id}/routines", s.handleListRoutines)
	mux.HandleFunc("POST /api/bots/{id}/routines", s.handleCreateRoutine)
	mux.HandleFunc("PATCH /api/bots/{id}/routines/{routineId}", s.handleUpdateRoutine)
	mux.HandleFunc("DELETE /api/bots/{id}/routines/{routineId}", s.handleDeleteRoutine)
	mux.HandleFunc("POST /api/bots/{id}/routines/{routineId}/run", s.handleRunRoutine)
	mux.HandleFunc("GET /api/bots/{id}/routine-runs", s.handleListRoutineRuns)
	mux.HandleFunc("GET /api/bots/{id}/tasks", s.handleListBotTasks)
	mux.HandleFunc("POST /api/bots/{id}/tasks", s.handleSpawnTask)
	mux.HandleFunc("GET /api/tasks/{taskId}", s.handleGetTask)
	mux.HandleFunc("GET /api/tasks/{taskId}/events", s.handleListTaskEvents)
	mux.HandleFunc("POST /api/tasks/{taskId}/send", s.handleSendToTask)
	mux.HandleFunc("POST /api/tasks/{taskId}/cancel", s.handleCancelTask)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	mux.HandleFunc("PATCH /api/settings", s.handlePutSettings)
	mux.HandleFunc("GET /api/update", s.handleUpdateStatus)
	mux.HandleFunc("POST /api/update/check", s.handleUpdateCheck)
	mux.HandleFunc("PUT /api/update/config", s.handleUpdateConfig)
	mux.HandleFunc("PATCH /api/update/config", s.handleUpdateConfig)
	mux.HandleFunc("POST /api/desktop/reset", s.handleDesktopReset)
	mux.HandleFunc("POST /api/push/register", s.handlePushRegister)
	mux.HandleFunc("POST /api/push/activity-token", s.handlePushActivityToken)
	mux.HandleFunc("DELETE /api/push/devices/{deviceId}", s.handlePushDeleteDevice)
	mux.HandleFunc("GET /api/push/status", s.handlePushStatus)
	mux.HandleFunc("GET /api/models", s.handleModels)
	mux.HandleFunc("POST /api/harnesses/{id}/install", s.handleHarnessInstallStart)
	mux.HandleFunc("GET /api/harnesses/{id}/install", s.handleHarnessInstallStatus)
	mux.HandleFunc("POST /api/harnesses/{id}/login", s.handleHarnessLoginStart)
	mux.HandleFunc("GET /api/harnesses/{id}/login", s.handleHarnessLoginStatus)

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

	// Multi-host sync & optional WebDAV
	mux.HandleFunc("GET /api/sync/status", s.handleSyncStatus)
	mux.HandleFunc("GET /api/sync/portable", s.handleSyncPortableGet)
	mux.HandleFunc("POST /api/sync/portable", s.handleSyncPortablePost)
	mux.HandleFunc("GET /api/sync/manifest", s.handleSyncManifest)
	mux.HandleFunc("GET /api/sync/file", s.handleSyncFileGet)
	mux.HandleFunc("PUT /api/sync/file", s.handleSyncFilePut)
	mux.HandleFunc("GET /api/sync/delta", s.handleSyncDeltaGet)
	mux.HandleFunc("POST /api/sync/delta", s.handleSyncDeltaPost)
	mux.HandleFunc("POST /api/sync/peer/pull", s.handleSyncPeerPull)
	mux.HandleFunc("POST /api/sync/peer/push", s.handleSyncPeerPush)
	mux.HandleFunc("POST /api/sync/git/checkpoint", s.handleSyncGitCheckpoint)
	mux.HandleFunc("POST /api/sync/git/pull", s.handleSyncGitPull)
	s.mountWebDAVIfEnabled(mux)

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
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s.hub.WarmModelsCache(ctx)
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
			!(r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/media/") && s.validMediaTicket(strings.TrimPrefix(r.URL.Path, "/api/media/"), r.URL.Query().Get("ticket"))) &&
			!(r.Method == http.MethodGet && s.validWorkspaceFileRawRequest(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set(protocol.HeaderProtocol, strconv.Itoa(protocol.CurrentProtocol))
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
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid token"})
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

	replay := false
	var since uint64
	if r.URL.Query().Has("since") {
		rawSince := strings.TrimSpace(r.URL.Query().Get("since"))
		if rawSince == "" {
			replay = true
		} else if v, err := strconv.ParseUint(rawSince, 10, 64); err == nil {
			since = v
			replay = true
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid since cursor"})
			return
		}
	} else if lastID := strings.TrimSpace(r.Header.Get("Last-Event-ID")); lastID != "" {
		if v, err := strconv.ParseUint(lastID, 10, 64); err == nil {
			since = v
			replay = true
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	resetCh := s.sseResetChan()
	subID, ch, missed := s.hub.SubscribeSince(since, replay)
	defer s.hub.Unsubscribe(subID)

	// Send an initial ping so clients mark connected immediately.
	fmt.Fprintf(w, "event: ready\ndata: {}\n\n")
	for _, evt := range missed {
		raw, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		if evt.Seq > 0 {
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.Seq, evt.Type, raw)
		} else {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, raw)
		}
	}
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-resetCh:
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
			if evt.Seq > 0 {
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.Seq, evt.Type, raw)
			} else {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, raw)
			}
			flusher.Flush()
		}
	}
}

func (s *HTTPServer) serverInfo() protocol.ServerInfo {
	virtualSupported := false
	if pool := s.localDeskPool(); pool != nil {
		virtualSupported = pool.VirtualSupported()
	}
	return protocol.BuildServerInfo(virtualSupported)
}

func (s *HTTPServer) handleMeta(w http.ResponseWriter, r *http.Request) {
	if !viaTailcat(r) && !viaTailscale(r) {
		w.Header().Set("X-Pebble-PID", strconv.Itoa(os.Getpid()))
	}
	writeJSON(w, http.StatusOK, s.serverInfo())
}

func queryBool(r *http.Request, name string) bool {
	if r == nil || r.URL == nil {
		return false
	}
	v := strings.TrimSpace(r.URL.Query().Get(name))
	return v == "1" || strings.EqualFold(v, "true")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeHubErr(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, hub.ErrBotNotFound), errors.Is(err, hub.ErrRoutineNotFound), errors.Is(err, state.ErrTaskNotFound):
		status = http.StatusNotFound
	case errors.Is(err, hub.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, scheduler.ErrRoutineAlreadyRunning):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}
