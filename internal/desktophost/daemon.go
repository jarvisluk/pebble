package desktophost

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/channel"
	"bot-hub/internal/protocol"
)

// DaemonOptions configures RunDaemon.
type DaemonOptions struct {
	Dir        string
	ServeLocal func(ctx context.Context, dir string) error
	Out        io.Writer
	Stdin      io.Reader
}

type sseClient struct {
	ch chan sseEvent
}

type sseEvent struct {
	name string
	data string
}

type controlServer struct {
	host   *Host
	secret string

	mu      sync.Mutex
	clients map[*sseClient]struct{}
}

func newControlServer(host *Host, secret string) *controlServer {
	return &controlServer{
		host:    host,
		secret:  secret,
		clients: make(map[*sseClient]struct{}),
	}
}

func (s *controlServer) broadcast(name string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ev := sseEvent{name: name, data: string(raw)}
	s.mu.Lock()
	for c := range s.clients {
		select {
		case c.ch <- ev:
		default:
			// Drop oldest event by draining one item so slow readers never stall Host locks.
			select {
			case <-c.ch:
			default:
			}
			select {
			case c.ch <- ev:
			default:
			}
		}
	}
	s.mu.Unlock()
}

func (s *controlServer) addClient() *sseClient {
	c := &sseClient{ch: make(chan sseEvent, 32)}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	return c
}

func (s *controlServer) removeClient(c *sseClient) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
}

func (s *controlServer) authorize(w http.ResponseWriter, r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		http.Error(w, `{"error":"forbidden host"}`, http.StatusForbidden)
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.secret)) != 1 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *controlServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/control/status", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		WriteJSON(w, s.host.Snapshot())
	})

	mux.HandleFunc("/control/events", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		c := s.addClient()
		defer s.removeClient(c)

		if raw, err := json.Marshal(s.host.Snapshot()); err == nil {
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", raw)
			flusher.Flush()
		}

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev := <-c.ch:
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, ev.data)
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	})

	mux.HandleFunc("/control/start", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Mode        string `json:"mode"`
			Link        string `json:"link"`
			RemoteToken string `json:"remoteToken"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		p := s.host.Prefs()
		p.Mode = strings.TrimSpace(req.Mode)
		p.Link = strings.TrimSpace(req.Link)
		p.RemoteToken = strings.TrimSpace(req.RemoteToken)
		go s.host.Start(p)
		WriteJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/control/reset", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		go s.host.Reset()
		WriteJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/control/local-node", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Enabled     *bool    `json:"enabled"`
			Name        *string  `json:"name"`
			Permissions []string `json:"permissions"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		WriteJSON(w, s.host.SetLocalNode(req.Enabled, req.Name, req.Permissions))
	})

	mux.HandleFunc("/control/failover", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		go s.host.FailoverToLocal()
		WriteJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/control/failback", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		go s.host.FailbackToRemote()
		WriteJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/control/tailscale", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		includePeers := r.URL.Query().Get("peers") == "1"
		WriteJSON(w, s.host.TailscaleStatus(r.Context(), includePeers))
	})

	mux.HandleFunc("/control/tailscale/start", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			AuthKey    string `json:"authKey"`
			ControlURL string `json:"controlUrl"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req)
		st, _ := s.host.TailscaleStart(r.Context(), req.AuthKey, req.ControlURL, true)
		WriteJSON(w, st)
	})

	mux.HandleFunc("/control/tailscale/logout", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		WriteJSON(w, s.host.TailscaleLogout(r.Context()))
	})

	return mux
}

func generateControlSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// RunDaemon starts the headless desktop host control API on 127.0.0.1:0,
// writes the handshake JSON line to opts.Out, and stops when ctx cancels or Stdin reaches EOF.
func RunDaemon(ctx context.Context, opts DaemonOptions) error {
	dir := opts.Dir
	if dir == "" {
		var err error
		dir, err = DataDir()
		if err != nil {
			return err
		}
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	secret, err := generateControlSecret()
	if err != nil {
		return err
	}

	var cs *controlServer
	host := New(dir, Hooks{
		ServeLocal: opts.ServeLocal,
		OnChange: func(snap Snapshot) {
			if cs != nil {
				cs.broadcast("status", snap)
			}
		},
		OnOpenURL: func(raw string) {
			if cs != nil {
				cs.broadcast("open_url", map[string]string{"url": raw})
			}
		},
	})
	cs = newControlServer(host, secret)
	host.Load()

	channel.DesktopResetFunc = func() { go host.Reset() }
	channel.OnAPITokenChanged = func(tok string) { host.SetLocalToken(tok) }
	defer func() {
		channel.DesktopResetFunc = nil
		channel.OnAPITokenChanged = nil
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           cs.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	dctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Exit cleanly if the parent GUI app terminates or closes stdin.
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		cancel()
	}()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	handshake := map[string]any{
		"url":             "http://" + ln.Addr().String(),
		"token":           secret,
		"dataDir":         dir,
		"protocolVersion": protocol.CurrentProtocol,
		"capabilities":    protocol.DaemonCapabilities(),
		"version":         protocol.AppVersion,
	}
	rawHS, _ := json.Marshal(handshake)
	if _, err := fmt.Fprintf(out, "%s\n", rawHS); err != nil {
		cancel()
	}

	p := host.Prefs()
	if p.TailscaleBound {
		go func() {
			_, _ = host.TailscaleStart(dctx, "", p.TailscaleControlURL, false)
		}()
	}
	if p.Mode != "" {
		go host.Start(p)
	} else {
		go host.WarmupCaches()
	}

	select {
	case <-dctx.Done():
	case err := <-errCh:
		cancel()
		host.Halt()
		host.CloseTailscale()
		return err
	}

	sctx, scancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer scancel()
	_ = srv.Shutdown(sctx)
	host.Halt()
	host.CloseTailscale()
	return nil
}
