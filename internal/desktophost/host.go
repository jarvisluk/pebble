package desktophost

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/state"
	"bot-hub/internal/syncer"
	"bot-hub/internal/tunnel"
)

// Prefs mirrors desktop.json persisted in the data directory.
type Prefs struct {
	Mode                string   `json:"mode,omitempty"` // "" (ask) | local | remote
	Link                string   `json:"link,omitempty"`
	RemoteToken         string   `json:"remoteToken,omitempty"`
	NodeID              string   `json:"nodeId,omitempty"`
	NodeName            string   `json:"nodeName,omitempty"`
	AllowNode           bool     `json:"allowNode,omitempty"`
	NodePerms           []string `json:"nodePerms"`
	TailscaleBound      bool     `json:"tailscaleBound,omitempty"`
	TailscaleControlURL string   `json:"tailscaleControlUrl,omitempty"`
}

// Snapshot represents the live state of the desktop host exported to UI shells.
type Snapshot struct {
	Mode                string   `json:"mode"`
	Link                string   `json:"link"`
	DataDir             string   `json:"dataDir"`
	ActiveURL           string   `json:"activeUrl"`
	ActiveToken         string   `json:"activeToken"`
	Status              string   `json:"status"`
	StatusState         string   `json:"statusState"`
	StatusError         string   `json:"statusError"`
	AllowNode           bool     `json:"allowNode"`
	NodeConnected       bool     `json:"nodeConnected"`
	NodeStatus          string   `json:"nodeStatus"`
	NodeError           string   `json:"nodeError"`
	NodeID              string   `json:"nodeId"`
	NodeName            string   `json:"nodeName"`
	NodePerms           []string `json:"nodePerms"`
	CanFailover         bool     `json:"canFailover"`
	CanFailback         bool     `json:"canFailback"`
	TailscaleBound      bool     `json:"tailscaleBound"`
	TailscaleControlURL string   `json:"tailscaleControlUrl"`
}

// RemoteTarget describes a parsed remote connection link.
type RemoteTarget struct {
	Kind    string // "tailcat" | "tailscale" | "http"
	Link    string // cleaned link without #token=
	Token   string
	Addr    string // host:port for tailscale dialing
	BaseURL string // for Kind == "http" or "tailscale"
}

// Hooks connects the desktop host engine to a UI shell (Wails or Swift daemon).
type Hooks struct {
	ServeLocal func(ctx context.Context, dir string) error
	OnChange   func(Snapshot)
	OnModeURL  func(url string)
	OnReset    func()
	OnOpenURL  func(url string)
}

// Host manages local/remote mode lifecycle, local node agent, failover/failback, and Tailscale.
type Host struct {
	dir   string
	hooks Hooks

	mu          sync.Mutex
	prefs       Prefs
	activeURL   string
	activeToken string
	status      string
	statusState string
	statusError string
	stop        func()
	nodeAgent   *node.Agent
	tsNode      *tunnel.TailscaleNode
}

// New creates a Host rooted at dir with optional lifecycle hooks.
func New(dir string, hooks Hooks) *Host {
	return &Host{dir: dir, hooks: hooks}
}

// Dir returns the host data directory.
func (h *Host) Dir() string {
	return h.dir
}

// Prefs returns a copy of the current preferences.
func (h *Host) Prefs() Prefs {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := h.prefs
	cp.NodePerms = append([]string(nil), h.prefs.NodePerms...)
	return cp
}

// Snapshot returns a thread-safe snapshot of the current desktop host state.
func (h *Host) Snapshot() Snapshot {
	h.mu.Lock()
	p := h.prefs
	perms := append([]string(nil), h.prefs.NodePerms...)
	activeURL := h.activeURL
	activeToken := h.activeToken
	status := h.status
	statusState := h.statusState
	statusError := h.statusError
	agent := h.nodeAgent
	h.mu.Unlock()

	nodeConnected := false
	nodeStatus := "disabled"
	nodeError := ""
	if agent != nil {
		st := agent.Status()
		nodeConnected = st.Connected
		nodeStatus = st.Status
		nodeError = st.Error
		if st.NodeID != "" {
			p.NodeID = st.NodeID
		}
		if st.Name != "" {
			p.NodeName = st.Name
		}
		if st.Permissions != nil {
			perms = append([]string(nil), st.Permissions...)
		}
	} else if p.AllowNode {
		nodeStatus = "idle"
	}

	linkTrimmed := strings.TrimSpace(p.Link)
	return Snapshot{
		Mode:                p.Mode,
		Link:                p.Link,
		DataDir:             h.dir,
		ActiveURL:           activeURL,
		ActiveToken:         activeToken,
		Status:              status,
		StatusState:         statusState,
		StatusError:         statusError,
		AllowNode:           p.AllowNode,
		NodeConnected:       nodeConnected,
		NodeStatus:          nodeStatus,
		NodeError:           nodeError,
		NodeID:              p.NodeID,
		NodeName:            p.NodeName,
		NodePerms:           perms,
		CanFailover:         p.Mode == "remote" && linkTrimmed != "",
		CanFailback:         p.Mode == "local" && linkTrimmed != "",
		TailscaleBound:      p.TailscaleBound,
		TailscaleControlURL: p.TailscaleControlURL,
	}
}

func (h *Host) notify() {
	if h.hooks.OnChange != nil {
		h.hooks.OnChange(h.Snapshot())
	}
}

// Load reads desktop.json and fills default node identity/permissions.
func (h *Host) Load() {
	h.mu.Lock()
	if raw, err := os.ReadFile(filepath.Join(h.dir, "desktop.json")); err == nil {
		_ = json.Unmarshal(raw, &h.prefs)
	}
	defID, defName := node.DefaultIdentity()
	if strings.TrimSpace(h.prefs.NodeID) == "" {
		h.prefs.NodeID = defID
	}
	if strings.TrimSpace(h.prefs.NodeName) == "" {
		h.prefs.NodeName = defName
	}
	if h.prefs.NodePerms == nil {
		h.prefs.NodePerms = []string{node.PermFiles, node.PermExec, node.PermDesktop}
	} else {
		h.prefs.NodePerms = node.ValidPerms(h.prefs.NodePerms)
	}
	h.mu.Unlock()
}

// Save writes desktop.json with 0600 permissions.
func (h *Host) Save() {
	h.mu.Lock()
	raw, _ := json.MarshalIndent(h.prefs, "", "  ")
	h.mu.Unlock()
	_ = os.WriteFile(filepath.Join(h.dir, "desktop.json"), raw, 0o600)
}

// WarmupCaches pre-warms harness auth and Magpie caches while on the setup screen.
func (h *Host) WarmupCaches() {
	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	magpieOK := harness.MagpieInstalled()
	if magpieOK {
		wg.Add(3)
		go func() { defer wg.Done(); _ = harness.MagpieGatewayHealthy(wctx) }()
		go func() { defer wg.Done(); _ = harness.MagpieListProviders(wctx) }()
		go func() { defer wg.Done(); _ = harness.MagpiePresets(wctx) }()
	}
	for _, id := range []string{"opencode", "codebuddy", "qoder", "codex", "claude", "grok", "trae", "pi", "dsh", "agy"} {
		if ok, _ := harness.DetectHarness(id); ok {
			k := id
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = harness.DetectHarnessAuthCached(wctx, k)
				if magpieOK {
					_ = harness.GetMagpieStatus(wctx, k)
				}
			}()
		}
	}
	wg.Wait()
}

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
			return "", "", fmt.Errorf("本机服务退出：%v", err)
		default:
		}
		if r, err := warmClient.Get(base + "/healthz"); err == nil {
			r.Body.Close()
			if sr, serr := warmClient.Get(base + "/api/settings"); serr == nil {
				sr.Body.Close()
			}
			return base + "/", cfg.HTTP.APIToken, nil
		}
	}
	return "", "", errors.New("本机服务没有响应")
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
	h.setStatus("正在从主电脑同步数据并切换至本机接管…")
	if err := EnsureConfig(h.dir); err != nil {
		h.setStatus("初始化本机配置失败：" + err.Error())
		return
	}
	cfg, _, err := config.Load(h.dir, "")
	if err != nil {
		h.setStatus("读取本机配置失败：" + err.Error())
		return
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		h.setStatus("打开本机状态库失败：" + err.Error())
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
	h.setStatus("正在回传接管增量至主电脑…")
	cfg, _, err := config.Load(h.dir, "")
	if err != nil {
		h.setStatus("读取本机配置失败：" + err.Error())
		return
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		h.setStatus("打开本机状态库失败：" + err.Error())
		return
	}
	svc := syncer.NewWithStore(cfg, store)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	cli, peerBase, peerTok, cleanup, err := h.peerHTTPClient(ctx, p.Link, p.RemoteToken)
	if err != nil {
		cancel()
		_ = store.Close()
		h.setStatus("连接主电脑失败，已保留在本机运行：" + err.Error())
		return
	}
	_, pushErr := svc.PushToPeer(ctx, cli, peerBase, peerTok)
	cleanup()
	cancel()
	if pushErr != nil {
		_ = store.Close()
		h.setStatus("回传增量失败，已保留在本机运行：" + pushErr.Error())
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

// SetLocalNode updates local node settings and reconfigures the live node agent if running.
func (h *Host) SetLocalNode(enabled *bool, name *string, perms []string) map[string]any {
	h.mu.Lock()
	if enabled != nil {
		h.prefs.AllowNode = *enabled
	}
	if name != nil {
		if n := strings.TrimSpace(*name); n != "" {
			h.prefs.NodeName = n
		}
	}
	if perms != nil {
		h.prefs.NodePerms = node.ValidPerms(perms)
	}
	allow := h.prefs.AllowNode
	nodeID := h.prefs.NodeID
	nodeName := h.prefs.NodeName
	curPerms := append([]string(nil), h.prefs.NodePerms...)
	agent := h.nodeAgent
	h.mu.Unlock()

	h.Save()
	defer h.notify()
	if agent != nil {
		agent.Configure(allow, nodeName, curPerms)
		st := agent.Status()
		return map[string]any{
			"available":   true,
			"enabled":     st.Enabled,
			"connected":   st.Connected,
			"status":      st.Status,
			"error":       st.Error,
			"nodeId":      st.NodeID,
			"name":        st.Name,
			"permissions": st.Permissions,
		}
	}
	return map[string]any{
		"available":   false,
		"enabled":     allow,
		"nodeId":      nodeID,
		"name":        nodeName,
		"permissions": curPerms,
	}
}

// HandleLocalNode serves GET/POST /api/local-node on the remote proxy.
func (h *Host) HandleLocalNode(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h2, _, err := net.SplitHostPort(host); err == nil {
		host = h2
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		http.Error(w, `{"error":"invalid host"}`, http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			http.Error(w, `{"error":"cross-origin request rejected"}`, http.StatusForbidden)
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		h.mu.Lock()
		agent := h.nodeAgent
		h.mu.Unlock()
		if agent == nil {
			WriteJSON(w, map[string]any{"available": false})
			return
		}
		st := agent.Status()
		WriteJSON(w, map[string]any{
			"available":   true,
			"enabled":     st.Enabled,
			"connected":   st.Connected,
			"status":      st.Status,
			"error":       st.Error,
			"nodeId":      st.NodeID,
			"name":        st.Name,
			"permissions": st.Permissions,
		})

	case http.MethodPost:
		if tok := h.remoteToken(); tok != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got == "" {
				if c, err := r.Cookie("bot_hub_token"); err == nil {
					got = c.Value
				}
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(tok)) != 1 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		var req struct {
			Enabled     *bool    `json:"enabled"`
			Name        *string  `json:"name"`
			Permissions []string `json:"permissions"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		WriteJSON(w, h.SetLocalNode(req.Enabled, req.Name, req.Permissions))

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// Halt stops the active local server or remote proxy.
func (h *Host) Halt() {
	h.mu.Lock()
	stop := h.stop
	h.stop = nil
	h.activeURL = ""
	h.activeToken = ""
	h.mu.Unlock()
	if stop != nil {
		stop()
	}
	h.notify()
}

// Reset stops any active mode and returns the host to mode selection.
func (h *Host) Reset() {
	h.Halt()
	h.mu.Lock()
	h.prefs.Mode = ""
	h.activeURL = ""
	h.activeToken = ""
	h.status = ""
	h.statusState = ""
	h.statusError = ""
	h.mu.Unlock()
	h.Save()
	h.notify()
	if h.hooks.OnReset != nil {
		h.hooks.OnReset()
	}
}

func (h *Host) setStatus(s string) {
	h.mu.Lock()
	h.status = s
	h.statusState = ""
	h.statusError = ""
	h.mu.Unlock()
	h.notify()
}

func (h *Host) setStatusState(state, errMsg string) {
	h.mu.Lock()
	h.status = ""
	h.statusState = state
	h.statusError = errMsg
	h.mu.Unlock()
	h.notify()
}

// CloseTailscale shuts down the embedded Tailscale node if active.
func (h *Host) CloseTailscale() {
	h.mu.Lock()
	n := h.tsNode
	h.tsNode = nil
	h.mu.Unlock()
	if n != nil {
		_ = n.Close()
	}
}

// TailscaleStart starts or reconfigures the embedded Tailscale node.
func (h *Host) TailscaleStart(ctx context.Context, authKey, controlURL string, openBrowser bool) (tunnel.TailscaleState, error) {
	ctrl := strings.TrimSpace(controlURL)
	key := strings.TrimSpace(authKey)

	h.mu.Lock()
	if ctrl == "" {
		ctrl = h.prefs.TailscaleControlURL
	}
	existing := h.tsNode
	needRecreate := existing != nil && (ctrl != existing.Options().ControlURL || key != "")
	if needRecreate {
		h.tsNode = nil
	}
	h.mu.Unlock()

	if needRecreate && existing != nil {
		_ = existing.Close()
		existing = nil
	}

	if existing == nil {
		n := tunnel.NewTailscaleNode(tunnel.TailscaleOptions{
			StateDir:   filepath.Join(h.dir, "tailscale-client"),
			Hostname:   tunnel.DefaultTailscaleHostname("bothub-desktop"),
			ControlURL: ctrl,
			AuthKey:    key,
		})
		if err := n.Start(); err != nil {
			return tunnel.TailscaleState{State: "error", Error: err.Error()}, err
		}
		h.mu.Lock()
		h.tsNode = n
		h.prefs.TailscaleBound = true
		h.prefs.TailscaleControlURL = ctrl
		h.mu.Unlock()
		h.Save()
		h.notify()
		existing = n
	}

	wctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	st := existing.WaitStateChange(wctx)
	if st.State == "running" {
		st = existing.Status(ctx, true)
	}
	if openBrowser && st.State == "needs_login" && st.AuthURL != "" && h.hooks.OnOpenURL != nil {
		h.hooks.OnOpenURL(st.AuthURL)
	}
	return st, nil
}

// TailscaleStatus returns the current embedded Tailscale node status.
func (h *Host) TailscaleStatus(ctx context.Context, includePeers bool) tunnel.TailscaleState {
	h.mu.Lock()
	n := h.tsNode
	h.mu.Unlock()
	if n == nil {
		return tunnel.TailscaleState{State: "disabled"}
	}
	return n.Status(ctx, includePeers)
}

// TailscaleLogout logs out and removes the embedded Tailscale client state.
func (h *Host) TailscaleLogout(ctx context.Context) tunnel.TailscaleState {
	h.mu.Lock()
	n := h.tsNode
	h.tsNode = nil
	h.prefs.TailscaleBound = false
	h.mu.Unlock()
	h.Save()
	h.notify()
	if n != nil {
		_ = n.Logout(ctx)
	} else {
		_ = os.RemoveAll(filepath.Join(h.dir, "tailscale-client"))
	}
	return tunnel.TailscaleState{State: "disabled"}
}

// WriteJSON writes v as JSON with HTTP 200.
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
