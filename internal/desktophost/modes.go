package desktophost

import (
	"bot-hub/internal/config"
	"bot-hub/internal/node"
	"bot-hub/internal/state"
	"bot-hub/internal/syncer"
	"bot-hub/internal/tunnel"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Start runs mode p ("local" or "remote") and updates the active URL/token.
func (h *Host) Start(p Prefs) {
	h.Halt()
	h.setStatusState("starting", "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var (
		modeURL string
		tok     string
		err     error
	)
	switch p.Mode {
	case "local":
		modeURL, tok, err = h.startLocal(ctx, done)
	case "remote":
		modeURL, tok, err = h.startRemote(ctx, p.Link, p.RemoteToken, done)
	default:
		err = errors.New("unknown mode")
	}
	if err != nil {
		cancel()
		h.setStatusState("error", err.Error())
		return
	}

	h.mu.Lock()
	h.prefs.Mode = p.Mode
	if p.Mode == "remote" {
		cleanLink, linkTok := tunnel.ExtractTokenFromLink(p.Link)
		if cleanLink != h.prefs.Link && linkTok == "" && strings.TrimSpace(p.RemoteToken) == "" {
			h.prefs.RemoteToken = ""
		}
		h.prefs.Link = cleanLink
		if tok != "" {
			h.prefs.RemoteToken = tok
		}
	}
	h.activeURL = strings.TrimRight(modeURL, "/")
	h.activeToken = tok
	h.stop = func() { cancel(); <-done }
	h.mu.Unlock()

	h.Save()
	h.setStatusState("", "")
	if h.hooks.OnModeURL != nil {
		h.hooks.OnModeURL(modeURL)
	}
}

func (h *Host) startLocal(ctx context.Context, done chan struct{}) (string, string, error) {
	if h.hooks.ServeLocal == nil {
		close(done)
		return "", "", errors.New("local server handler not configured")
	}
	if err := EnsureConfig(h.dir); err != nil {
		close(done)
		return "", "", err
	}
	cfg, _, err := config.Load(h.dir, "")
	if err != nil {
		close(done)
		return "", "", err
	}
	base := cfg.HTTP.BaseURL()
	errc := make(chan error, 1)
	go func() { errc <- h.hooks.ServeLocal(ctx, h.dir); close(done) }()
	warmClient := &http.Client{Timeout: 3 * time.Second}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		select {
		case err := <-errc:
			return "", "", fmt.Errorf("local service exited: %v", err)
		default:
		}
		if req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/meta", nil); rerr == nil {
			if cfg.HTTP.APIToken != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.HTTP.APIToken)
			}
			if r, err := warmClient.Do(req); err == nil {
				pidHdr := strings.TrimSpace(r.Header.Get("X-Pebble-PID"))
				r.Body.Close()
				if r.StatusCode == http.StatusOK && (pidHdr == "" || pidHdr == strconv.Itoa(os.Getpid())) {
					if sreq, serr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/settings", nil); serr == nil {
						if cfg.HTTP.APIToken != "" {
							sreq.Header.Set("Authorization", "Bearer "+cfg.HTTP.APIToken)
						}
						if sr, serr := warmClient.Do(sreq); serr == nil {
							sr.Body.Close()
						}
					}
					return base + "/", cfg.HTTP.APIToken, nil
				}
			}
		}
	}
	return "", "", errors.New("local service did not respond")
}

// ParseRemoteTarget parses tailcat://, tailscale://, or http(s):// addresses.
func ParseRemoteTarget(rawLink string) (RemoteTarget, error) {
	link, tok := tunnel.ExtractTokenFromLink(rawLink)
	trimmed := strings.TrimSpace(link)
	if trimmed == "" {
		return RemoteTarget{}, errors.New("remote address is required")
	}
	if strings.HasPrefix(trimmed, "tailcat://") {
		if _, _, err := tunnel.ParseLink(trimmed); err != nil {
			return RemoteTarget{}, err
		}
		return RemoteTarget{Kind: "tailcat", Link: trimmed, Token: tok}, nil
	}
	if strings.HasPrefix(trimmed, "tailscale://") {
		addr, err := tunnel.ParseTailscaleLink(trimmed)
		if err != nil {
			return RemoteTarget{}, err
		}
		return RemoteTarget{
			Kind:    "tailscale",
			Link:    trimmed,
			Token:   tok,
			Addr:    addr,
			BaseURL: "http://" + addr,
		}, nil
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		u, err := url.Parse(trimmed)
		if err != nil || u.Host == "" {
			return RemoteTarget{}, fmt.Errorf("invalid remote address: %s", trimmed)
		}
		return RemoteTarget{
			Kind:    "http",
			Link:    trimmed,
			Token:   tok,
			BaseURL: u.Scheme + "://" + u.Host,
		}, nil
	}
	return RemoteTarget{}, errors.New("expected tailcat://, tailscale://, or http(s):// address")
}

func (h *Host) startRemote(ctx context.Context, rawLink, overrideToken string, done chan struct{}) (string, string, error) {
	target, err := ParseRemoteTarget(rawLink)
	if err != nil {
		close(done)
		return "", "", err
	}

	var (
		tr          *http.Transport
		closeClient func() error
		upstream    *url.URL
	)
	if target.Kind == "tailcat" {
		dial, cfn, err := tunnel.Connect(ctx, target.Link)
		if err != nil {
			close(done)
			return "", "", err
		}
		closeClient = cfn
		tr = &http.Transport{
			DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) { return dial(dctx) },
		}
	} else if target.Kind == "tailscale" {
		u, err := url.Parse(target.BaseURL)
		if err != nil {
			close(done)
			return "", "", err
		}
		upstream = u
		closeClient = func() error { return nil }
		h.mu.Lock()
		tsNode := h.tsNode
		tsBound := h.prefs.TailscaleBound
		tsCtrl := h.prefs.TailscaleControlURL
		h.mu.Unlock()
		if tsNode == nil && tsBound {
			if _, serr := h.TailscaleStart(ctx, "", tsCtrl, false); serr == nil {
				h.mu.Lock()
				tsNode = h.tsNode
				h.mu.Unlock()
			}
		}
		if tsNode != nil {
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			werr := tsNode.WaitRunning(wctx)
			wcancel()
			if werr == nil {
				dialAddr := target.Addr
				tr = &http.Transport{
					DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) {
						return tsNode.Dial(dctx, dialAddr)
					},
				}
			}
		}
		if tr == nil {
			tr = &http.Transport{}
		}
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		req, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, target.BaseURL+"/healthz", nil)
		resp, err := (&http.Client{Transport: tr}).Do(req)
		cancel()
		if err != nil {
			tr.CloseIdleConnections()
			close(done)
			return "", "", fmt.Errorf("cannot reach %s: %w", target.BaseURL, err)
		}
		resp.Body.Close()
	} else {
		u, err := url.Parse(target.BaseURL)
		if err != nil {
			close(done)
			return "", "", err
		}
		upstream = u
		closeClient = func() error { return nil }
		tr = &http.Transport{}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, target.BaseURL+"/healthz", nil)
		resp, err := (&http.Client{Transport: tr}).Do(req)
		cancel()
		if err != nil {
			tr.CloseIdleConnections()
			close(done)
			return "", "", fmt.Errorf("cannot reach %s: %w", target.BaseURL, err)
		}
		resp.Body.Close()
	}

	link := target.Link
	tok := strings.TrimSpace(target.Token)
	if tok == "" {
		tok = strings.TrimSpace(overrideToken)
	}

	// Keep a fixed local port so cookies/endpoints stay stable across restarts.
	ln, err := net.Listen("tcp", "127.0.0.1:3790")
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		_ = closeClient()
		close(done)
		return "", "", err
	}

	localURL := "http://" + ln.Addr().String()
	if tr == nil {
		tr = &http.Transport{}
	}

	h.mu.Lock()
	nodeID, nodeName := h.prefs.NodeID, h.prefs.NodeName
	allowNode := h.prefs.AllowNode
	if tok == "" && h.prefs.Link == link {
		tok = h.prefs.RemoteToken
	}
	perms := append([]string(nil), h.prefs.NodePerms...)
	h.mu.Unlock()

	agent := node.NewAgent(node.AgentOptions{
		NodeID:      nodeID,
		Name:        nodeName,
		BaseURL:     localURL,
		Token:       tok,
		Permissions: perms,
		Enabled:     allowNode,
		HTTPClient:  &http.Client{Transport: tr},
		OnState:     func() { h.notify() },
	})
	h.mu.Lock()
	h.nodeAgent = agent
	h.mu.Unlock()

	rp := &httputil.ReverseProxy{
		Transport:     tr,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			if upstream != nil {
				pr.Out.URL.Scheme = upstream.Scheme
				pr.Out.URL.Host = upstream.Host
				pr.Out.Host = upstream.Host
				return
			}
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = pr.In.Host
			pr.Out.Host = pr.In.Host
		},
		ModifyResponse: func(resp *http.Response) error {
			h.syncRemoteTokenFromResponse(resp)
			return nil
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/local-node", h.HandleLocalNode)
	mux.Handle("/", rp)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		defer close(done)
		defer func() {
			h.mu.Lock()
			if h.nodeAgent == agent {
				h.nodeAgent = nil
			}
			h.mu.Unlock()
			agent.Close()
			tr.CloseIdleConnections()
			_ = closeClient()
			h.notify()
		}()
		<-ctx.Done()
		sctx, scancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer scancel()
		_ = srv.Shutdown(sctx)
	}()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[remote-proxy] %v", err)
		}
	}()
	return localURL + "/", tok, nil
}

// syncRemoteTokenFromResponse adopts a browser login or cookie-authenticated token
// without letting a stale host cookie overwrite an explicit Authorization Bearer token.
func (h *Host) syncRemoteTokenFromResponse(resp *http.Response) {
	if resp == nil || resp.StatusCode != http.StatusOK || resp.Request == nil {
		return
	}
	if resp.Request.URL.Path == "/api/login" {
		for _, c := range resp.Cookies() {
			if c.Name == "bot_hub_token" && c.Value != "" {
				h.setRemoteToken(c.Value)
				break
			}
		}
		return
	}
	if strings.HasPrefix(resp.Request.URL.Path, "/api/") &&
		strings.TrimSpace(resp.Request.Header.Get("Authorization")) == "" {
		if c, err := resp.Request.Cookie("bot_hub_token"); err == nil && c.Value != "" {
			h.setRemoteToken(c.Value)
		}
	}
}

func (h *Host) peerHTTPClient(ctx context.Context, rawLink, fallbackToken string) (*http.Client, string, string, func(), error) {
	link, linkTok := tunnel.ExtractTokenFromLink(rawLink)
	tok := strings.TrimSpace(linkTok)
	if tok == "" {
		tok = strings.TrimSpace(fallbackToken)
	}
	if strings.HasPrefix(link, "tailcat://") {
		dial, closeClient, err := tunnel.Connect(ctx, link)
		if err != nil {
			return nil, "", "", nil, err
		}
		cli := &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) { return dial(dctx) },
			},
		}
		return cli, "http://tailcat.local", tok, func() { _ = closeClient() }, nil
	}
	return &http.Client{Timeout: 15 * time.Second}, strings.TrimRight(link, "/"), tok, func() {}, nil
}

// FailoverToLocal pulls state from the remote primary and switches to local mode.
func (h *Host) FailoverToLocal() {
	h.mu.Lock()
	p := h.prefs
	h.mu.Unlock()
	if p.Mode != "remote" || strings.TrimSpace(p.Link) == "" {
		return
	}
	h.setStatus("Syncing data from primary host and switching to local failover…")
	if err := EnsureConfig(h.dir); err != nil {
		h.setStatus("Failed to initialize local config: " + err.Error())
		return
	}
	cfg, _, err := config.Load(h.dir, "")
	if err != nil {
		h.setStatus("Failed to load local config: " + err.Error())
		return
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		h.setStatus("Failed to open local state DB: " + err.Error())
		return
	}
	svc := syncer.NewWithStore(cfg, store)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	cli, peerBase, peerTok, cleanup, dialErr := h.peerHTTPClient(ctx, p.Link, p.RemoteToken)
	pulled := false
	if dialErr == nil {
		defer cleanup()
		if _, err := svc.PullFromPeer(ctx, cli, peerBase, peerTok); err == nil {
			pulled = true
		}
	}
	cancel()
	if !pulled && cfg.Sync.GitEnabled() {
		gctx, gcancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, _ = syncer.CheckpointPull(gctx, cfg)
		gcancel()
	}
	_ = store.Close()

	p.Mode = "local"
	h.Start(p)
}

// FailbackToRemote pushes local delta to the remote primary and switches back to remote mode.
func (h *Host) FailbackToRemote() {
	h.mu.Lock()
	p := h.prefs
	h.mu.Unlock()
	if p.Mode != "local" || strings.TrimSpace(p.Link) == "" {
		return
	}
	h.setStatus("Syncing local changes back to primary host…")
	cfg, _, err := config.Load(h.dir, "")
	if err != nil {
		h.setStatus("Failed to load local config: " + err.Error())
		return
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		h.setStatus("Failed to open local state DB: " + err.Error())
		return
	}
	svc := syncer.NewWithStore(cfg, store)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	cli, peerBase, peerTok, cleanup, err := h.peerHTTPClient(ctx, p.Link, p.RemoteToken)
	if err != nil {
		cancel()
		_ = store.Close()
		h.setStatus("Failed to reach primary host; staying in local mode: " + err.Error())
		return
	}
	_, pushErr := svc.PushToPeer(ctx, cli, peerBase, peerTok)
	cleanup()
	cancel()
	if pushErr != nil {
		_ = store.Close()
		h.setStatus("Failed to sync changes; staying in local mode: " + pushErr.Error())
		return
	}
	if cfg.Sync.GitEnabled() {
		gctx, gcancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, _ = syncer.CheckpointPush(gctx, cfg, svc.HostID())
		gcancel()
	}
	_ = store.Close()

	p.Mode = "remote"
	h.Start(p)
}

func (h *Host) remoteToken() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.prefs.RemoteToken
}

func (h *Host) setRemoteToken(tok string) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return
	}
	h.mu.Lock()
	changed := h.prefs.RemoteToken != tok
	if changed {
		h.prefs.RemoteToken = tok
	}
	if h.prefs.Mode == "remote" {
		h.activeToken = tok
	}
	agent := h.nodeAgent
	h.mu.Unlock()
	if changed {
		h.Save()
		h.notify()
	}
	if agent != nil {
		agent.SetToken(tok)
	}
}
