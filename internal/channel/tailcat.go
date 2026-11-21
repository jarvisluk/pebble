package channel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

type ctxKey int

const viaTailcatKey ctxKey = 0

// viaTailcat reports whether r arrived through the Tailcat tunnel.
func viaTailcat(r *http.Request) bool { v, _ := r.Context().Value(viaTailcatKey).(bool); return v }

// tailcatServe serves the same handler as the loopback server on a Tailcat
// listener. Requests arriving there are marked so guard always demands the token.
type tailcatServe struct {
	mu    sync.Mutex
	state string // disabled | connecting | ready | error
	link  string
	err   string
	srv   *http.Server
	stop  func()
}

func (s *HTTPServer) startTailcat(handler http.Handler) {
	s.cfgMu.RLock()
	enabled := s.cfg.TailcatEnabled()
	var derpMapURL string
	if s.cfg.Tailcat != nil {
		derpMapURL = s.cfg.Tailcat.DERPMapURL
	}
	s.cfgMu.RUnlock()

	t := &s.tailcat
	t.mu.Lock()
	if !enabled {
		t.state = "disabled"
		t.mu.Unlock()
		return
	}
	if t.state == "connecting" || t.state == "ready" {
		t.mu.Unlock()
		return
	}
	t.state, t.err, t.link = "connecting", "", ""
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, viaTailcatKey, true)
		},
	}
	t.srv = srv
	t.mu.Unlock()

	go func() {
		identity := filepath.Join(filepath.Dir(s.hub.Config().StateFile), "tailcat-identity.json")
		ln, link, stop, err := tunnel.Listen(context.Background(), identity, derpMapURL)
		t.mu.Lock()
		if t.state != "connecting" || t.srv != srv {
			t.mu.Unlock()
			if stop != nil {
				stop()
			}
			return
		}
		if err != nil {
			t.state, t.err, t.srv = "error", err.Error(), nil
			t.mu.Unlock()
			log.Printf("[tailcat] %v", err)
			return
		}
		t.state, t.link, t.stop = "ready", link, stop
		t.mu.Unlock()
		log.Printf("[tailcat] ready; open the Web UI (远程访问) for the link / QR code")
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[tailcat] serve: %v", err)
		}
	}()
}

func (t *tailcatServe) shutdown(ctx context.Context) {
	t.mu.Lock()
	srv, stop := t.srv, t.stop
	t.srv, t.stop = nil, nil
	t.state, t.link, t.err = "disabled", "", ""
	t.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(ctx)
	}
	if stop != nil {
		stop()
	}
}

func newAPIToken() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *HTTPServer) tailcatStatus(reveal bool) map[string]any {
	t := &s.tailcat
	t.mu.Lock()
	state, link, errMsg := t.state, t.link, t.err
	t.mu.Unlock()
	if state == "" {
		state = "disabled"
	}
	tok := s.apiToken()
	envRef := ""
	if s.hub != nil {
		envRef = config.RawAPITokenEnvRef(s.hub.Config().ConfigPath)
	}
	out := map[string]any{
		"state":       state,
		"link":        link,
		"error":       errMsg,
		"hasToken":    tok != "",
		"tokenEnvRef": envRef,
	}
	if reveal && envRef == "" && tok != "" {
		out["apiToken"] = tok
	}
	return out
}

func (s *HTTPServer) handleTailcat(w http.ResponseWriter, r *http.Request) {
	reveal := r.URL.Query().Get("reveal") == "1"
	writeJSON(w, http.StatusOK, s.tailcatStatus(reveal))
}

func (s *HTTPServer) handleSetTailcat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled    *bool  `json:"enabled"`
		APIToken   string `json:"apiToken"`
		ResetToken bool   `json:"resetToken"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	req.APIToken = strings.TrimSpace(req.APIToken)

	if (req.ResetToken || req.APIToken != "") && s.hub != nil {
		if envRef := config.RawAPITokenEnvRef(s.hub.Config().ConfigPath); envRef != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("当前口令来自环境变量 %s，请修改 .env 文件", envRef)})
			return
		}
	}

	s.cfgMu.RLock()
	curEnabled := s.cfg.TailcatEnabled()
	prevToken := s.cfg.APIToken
	s.cfgMu.RUnlock()

	enable := curEnabled
	if req.Enabled != nil {
		enable = *req.Enabled
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

	err := config.UpdateRaw(s.hub.Config(), func(raw *config.HubConfig) error {
		if req.Enabled != nil {
			if enable {
				if raw.HTTP.Tailcat == nil {
					raw.HTTP.Tailcat = &config.TailcatConfig{}
				}
				raw.HTTP.Tailcat.Enabled = true
			} else if raw.HTTP.Tailcat != nil {
				raw.HTTP.Tailcat.Enabled = false
			}
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

	s.cfgMu.Lock()
	if req.Enabled != nil {
		if enable {
			if s.cfg.Tailcat == nil {
				s.cfg.Tailcat = &config.TailcatConfig{}
			}
			s.cfg.Tailcat.Enabled = true
		} else if s.cfg.Tailcat != nil {
			s.cfg.Tailcat.Enabled = false
		}
	}
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
	if req.Enabled != nil {
		if enable {
			if s.srv != nil {
				s.startTailcat(s.srv.Handler)
			}
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			s.tailcat.shutdown(ctx)
		}
	}

	writeJSON(w, http.StatusOK, s.tailcatStatus(token != prevToken))
}

func (s *HTTPServer) handleTailcatQR(w http.ResponseWriter, _ *http.Request) {
	t := &s.tailcat
	t.mu.Lock()
	link := t.link
	t.mu.Unlock()
	if link == "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "tailcat not ready"})
		return
	}
	qrData := link
	if tok := s.apiToken(); tok != "" {
		qrData = link + "#token=" + url.QueryEscape(tok)
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
