package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"

	"bot-hub/internal/config"
	"bot-hub/internal/tunnel"
)

const viaTailscaleKey ctxKey = 1

// viaTailscale reports whether r arrived through the built-in tsnet listener.
func viaTailscale(r *http.Request) bool { v, _ := r.Context().Value(viaTailscaleKey).(bool); return v }

type tailscaleServe struct {
	mu   sync.Mutex
	node *tunnel.TailscaleNode
	srv  *http.Server
	err  string
}

func (s *HTTPServer) tailscaleStateDir() string {
	if s.hub != nil {
		return filepath.Join(filepath.Dir(s.hub.Config().StateFile), "tsnet")
	}
	return filepath.Join("state", "tsnet")
}

func (s *HTTPServer) startTailscale(handler http.Handler) {
	s.cfgMu.RLock()
	enabled := s.cfg.TailscaleEnabled()
	var tc config.TailscaleConfig
	if s.cfg.Tailscale != nil {
		tc = *s.cfg.Tailscale
	}
	s.cfgMu.RUnlock()

	ts := &s.tailscale
	ts.mu.Lock()
	if !enabled {
		ts.mu.Unlock()
		return
	}
	if ts.node != nil {
		ts.mu.Unlock()
		return
	}
	hostname := strings.TrimSpace(tc.Hostname)
	if hostname == "" {
		hostname = tunnel.DefaultTailscaleHostname("bothub")
	}
	node := tunnel.NewTailscaleNode(tunnel.TailscaleOptions{
		StateDir:   s.tailscaleStateDir(),
		Hostname:   hostname,
		ControlURL: tc.ControlURL,
		AuthKey:    tc.AuthKey,
	})
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, viaTailscaleKey, true)
		},
	}
	ts.node = node
	ts.srv = srv
	ts.err = ""
	ts.mu.Unlock()

	go func() {
		if err := node.Start(); err != nil {
			ts.mu.Lock()
			if ts.node == node {
				ts.err = err.Error()
				ts.node = nil
				ts.srv = nil
			}
			ts.mu.Unlock()
			log.Printf("[tailscale] start: %v", err)
			return
		}
		ln, err := node.Listen(tunnel.Port)
		if err != nil {
			_ = node.Close()
			ts.mu.Lock()
			if ts.node == node {
				ts.err = err.Error()
				ts.node = nil
				ts.srv = nil
			}
			ts.mu.Unlock()
			log.Printf("[tailscale] listen: %v", err)
			return
		}
		log.Printf("[tailscale] listening on :%d as %s", tunnel.Port, hostname)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[tailscale] serve: %v", err)
		}
	}()
}

func (ts *tailscaleServe) shutdown(ctx context.Context) {
	ts.mu.Lock()
	node, srv := ts.node, ts.srv
	ts.node, ts.srv, ts.err = nil, nil, ""
	ts.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(ctx)
	}
	if node != nil {
		_ = node.Close()
	}
}

func (ts *tailscaleServe) logout(ctx context.Context) error {
	ts.mu.Lock()
	node, srv := ts.node, ts.srv
	ts.node, ts.srv, ts.err = nil, nil, ""
	ts.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(ctx)
	}
	if node != nil {
		return node.Logout(ctx)
	}
	return nil
}

func (s *HTTPServer) tailscaleStatus(ctx context.Context, reveal bool) map[string]any {
	s.cfgMu.RLock()
	enabled := s.cfg.TailscaleEnabled()
	var hostname, controlURL string
	if s.cfg.Tailscale != nil {
		hostname = s.cfg.Tailscale.Hostname
		controlURL = s.cfg.Tailscale.ControlURL
	}
	s.cfgMu.RUnlock()

	ts := &s.tailscale
	ts.mu.Lock()
	node := ts.node
	lastErr := ts.err
	ts.mu.Unlock()

	st := tunnel.TailscaleState{
		State:    "disabled",
		Hostname: hostname,
		Error:    lastErr,
	}
	if node != nil {
		st = node.Status(ctx, false)
	} else if lastErr != "" {
		st.State = "error"
	}

	tok := s.apiToken()
	var tokenEnvRef, authKeyEnvRef string
	if s.hub != nil {
		cfgPath := s.hub.Config().ConfigPath
		tokenEnvRef = config.RawAPITokenEnvRef(cfgPath)
		authKeyEnvRef = config.RawTailscaleAuthKeyEnvRef(cfgPath)
	}

	out := map[string]any{
		"enabled":       enabled,
		"state":         st.State,
		"authUrl":       st.AuthURL,
		"hostname":      st.Hostname,
		"dnsName":       st.DNSName,
		"ip":            st.IP,
		"tailnet":       st.Tailnet,
		"link":          st.Link,
		"controlUrl":    controlURL,
		"error":         st.Error,
		"hasToken":      tok != "",
		"tokenEnvRef":   tokenEnvRef,
		"authKeyEnvRef": authKeyEnvRef,
	}
	if reveal && tokenEnvRef == "" && tok != "" {
		out["apiToken"] = tok
	}
	return out
}

func (s *HTTPServer) handleTailscale(w http.ResponseWriter, r *http.Request) {
	reveal := r.URL.Query().Get("reveal") == "1"
	writeJSON(w, http.StatusOK, s.tailscaleStatus(r.Context(), reveal))
}

func (s *HTTPServer) handleSetTailscale(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled    *bool   `json:"enabled"`
		Hostname   *string `json:"hostname"`
		ControlURL *string `json:"controlUrl"`
		AuthKey    *string `json:"authKey"`
		Logout     bool    `json:"logout"`
		APIToken   string  `json:"apiToken"`
		ResetToken bool    `json:"resetToken"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	req.APIToken = strings.TrimSpace(req.APIToken)

	var cfgPath string
	if s.hub != nil {
		cfgPath = s.hub.Config().ConfigPath
	}
	if (req.ResetToken || req.APIToken != "") && cfgPath != "" {
		if envRef := config.RawAPITokenEnvRef(cfgPath); envRef != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("当前口令来自环境变量 %s，请修改 .env 文件", envRef)})
			return
		}
	}
	if req.AuthKey != nil && strings.TrimSpace(*req.AuthKey) != "" && cfgPath != "" {
		if envRef := config.RawTailscaleAuthKeyEnvRef(cfgPath); envRef != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("当前 Auth Key 来自环境变量 %s，请修改 .env 文件", envRef)})
			return
		}
	}

	s.cfgMu.RLock()
	curEnabled := s.cfg.TailscaleEnabled()
	prevToken := s.cfg.APIToken
	var prevTS config.TailscaleConfig
	if s.cfg.Tailscale != nil {
		prevTS = *s.cfg.Tailscale
	}
	s.cfgMu.RUnlock()

	enable := curEnabled
	if req.Logout {
		enable = false
	} else if req.Enabled != nil {
		enable = *req.Enabled
	}

	nextTS := prevTS
	nextTS.Enabled = enable
	if req.Hostname != nil {
		nextTS.Hostname = strings.TrimSpace(*req.Hostname)
	}
	if req.ControlURL != nil {
		nextTS.ControlURL = strings.TrimSpace(*req.ControlURL)
	}
	if req.Logout {
		nextTS.AuthKey = ""
	} else if req.AuthKey != nil {
		ak := strings.TrimSpace(*req.AuthKey)
		if ak != "" {
			nextTS.AuthKey = ak
		}
	}

	token := prevToken
	if req.ResetToken {
		var err error
		token, err = newAPIToken()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	} else if req.APIToken != "" {
		token = req.APIToken
	} else if enable && token == "" {
		var err error
		token, err = newAPIToken()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}

	if s.hub != nil {
		err := config.UpdateRaw(s.hub.Config(), func(raw *config.HubConfig) error {
			if raw.HTTP.Tailscale == nil {
				raw.HTTP.Tailscale = &config.TailscaleConfig{}
			}
			raw.HTTP.Tailscale.Enabled = enable
			if req.Hostname != nil {
				raw.HTTP.Tailscale.Hostname = nextTS.Hostname
			}
			if req.ControlURL != nil {
				raw.HTTP.Tailscale.ControlURL = nextTS.ControlURL
			}
			if req.Logout {
				if config.RawTailscaleAuthKeyEnvRef(cfgPath) == "" {
					raw.HTTP.Tailscale.AuthKey = ""
				}
			} else if req.AuthKey != nil && strings.TrimSpace(*req.AuthKey) != "" {
				raw.HTTP.Tailscale.AuthKey = nextTS.AuthKey
			}
			if token != prevToken {
				raw.HTTP.APIToken = token
			}
			return nil
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": fmt.Sprintf("save config: %v", err)})
			return
		}
	}

	s.cfgMu.Lock()
	s.cfg.Tailscale = &nextTS
	s.cfg.APIToken = token
	s.cfgMu.Unlock()

	if token != prevToken && s.hub != nil {
		s.hub.SetAPIToken(token)
	}

	if token != "" && (token != prevToken || enable) {
		http.SetCookie(w, &http.Cookie{
			Name:     authCookie,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   r.TLS != nil,
			MaxAge:   30 * 24 * 3600,
		})
	}

	if req.Logout {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_ = s.tailscale.logout(ctx)
	} else if !enable {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		s.tailscale.shutdown(ctx)
	} else if s.srv != nil {
		// Restart if hostname/controlURL/authKey changed while running.
		if req.Hostname != nil || req.ControlURL != nil || (req.AuthKey != nil && strings.TrimSpace(*req.AuthKey) != "") {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			s.tailscale.shutdown(ctx)
			cancel()
		}
		s.startTailscale(s.srv.Handler)
		s.tailscale.mu.Lock()
		node := s.tailscale.node
		s.tailscale.mu.Unlock()
		if node != nil {
			wctx, wcancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
			_ = node.WaitStateChange(wctx)
			wcancel()
		}
	}

	writeJSON(w, http.StatusOK, s.tailscaleStatus(r.Context(), token != prevToken))
}

func (s *HTTPServer) handleTailscaleQR(w http.ResponseWriter, r *http.Request) {
	s.tailscale.mu.Lock()
	node := s.tailscale.node
	s.tailscale.mu.Unlock()
	if node == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "tailscale not ready"})
		return
	}
	st := node.Status(r.Context(), false)
	if st.State != "running" || st.Link == "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "tailscale not ready"})
		return
	}
	qrData := st.Link
	if tok := s.apiToken(); tok != "" {
		qrData = st.Link + "#token=" + url.QueryEscape(tok)
	}
	code, err := qr.Encode(qrData, qr.M)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	code.Scale = 6
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(code.PNG())
}
