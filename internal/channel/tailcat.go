package channel

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"rsc.io/qr"

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
	t := &s.tailcat
	if !s.cfg.TailcatEnabled() {
		t.state = "disabled"
		return
	}
	t.state = "connecting"
	t.srv = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, viaTailcatKey, true)
		},
	}
	go func() {
		identity := filepath.Join(filepath.Dir(s.hub.Config().StateFile), "tailcat-identity.json")
		ln, link, stop, err := tunnel.Listen(context.Background(), identity, s.cfg.Tailcat.DERPMapURL)
		t.mu.Lock()
		if err != nil {
			t.state, t.err = "error", err.Error()
			t.mu.Unlock()
			log.Printf("[tailcat] %v", err)
			return
		}
		t.state, t.link, t.stop = "ready", link, stop
		t.mu.Unlock()
		log.Printf("[tailcat] ready; open the Web UI (远程访问) for the link / QR code")
		if err := t.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[tailcat] serve: %v", err)
		}
	}()
}

func (t *tailcatServe) shutdown(ctx context.Context) {
	if t.srv != nil {
		_ = t.srv.Shutdown(ctx)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stop != nil {
		t.stop()
	}
}

func (s *HTTPServer) handleTailcat(w http.ResponseWriter, _ *http.Request) {
	t := &s.tailcat
	t.mu.Lock()
	defer t.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"state": t.state, "link": t.link, "error": t.err})
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
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	code.Scale = 6
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(code.PNG())
}
