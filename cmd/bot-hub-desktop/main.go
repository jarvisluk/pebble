// bot-hub-desktop is the desktop app: a window showing the chat page and a
// tray icon. It either runs the hub on this computer ("local") or opens a
// remote bot-hub through a Tailcat link ("remote"). Given arguments, it runs
// the CLI instead, so bots' `<exe> mcp ...` calls work from inside the app.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"

	"bot-hub/internal/cli"
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/state"
	"bot-hub/internal/syncer"
	"bot-hub/internal/tunnel"
	webassets "bot-hub/internal/web"
)

var desktopHarnesses = []struct {
	id    string
	label string
}{
	{"opencode", "OpenCode"},
	{"claude", "Claude Code"},
	{"pi", "Pi"},
	{"dsh", "DeepSeek Harness"},
	{"agy", "Antigravity CLI"},
}

func main() {
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-psn") { // -psn_: old macOS Finder launch arg
		cli.Main(os.Args[1:])
		return
	}
	shellEnv()
	dir, err := dataDir()
	if err != nil {
		log.Fatal(err)
	}
	d := &desktop{dir: dir}
	d.load()
	d.run()
}

type prefs struct {
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

type desktop struct {
	dir          string
	app          *application.App
	win          *application.WebviewWindow
	menu         *application.Menu
	nodeItem     *application.MenuItem
	takeoverItem *application.MenuItem
	pushbackItem *application.MenuItem
	mu           sync.Mutex
	prefs        prefs
	status       string // human-readable status (legacy/failover)
	statusState  string // "" | "starting" | "error"
	statusError  string // raw error detail when statusState == "error"
	stop         func() // stops the running mode
	nodeAgent    *node.Agent
	tsNode       *tunnel.TailscaleNode
}

func (d *desktop) run() {
	d.app = application.New(application.Options{
		Name:        "Pebble",
		Description: "Personal bot hub",
		Assets:      application.AssetOptions{Handler: http.HandlerFunc(d.serveSetup), DisableLogging: true},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		OnShutdown: func() {
			d.halt()
			d.closeTailscale()
		},
	})
	d.win = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Pebble",
		Width:            1100,
		Height:           760,
		MinWidth:         680,
		MinHeight:        520,
		BackgroundColour: application.NewRGB(239, 236, 232),
		URL:              "/desktop-setup",
		JS:               "if(/Mac/.test(navigator.platform)){window.__bothubMacShell=true;document.documentElement.classList.add('mac-app');}",
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHiddenInset,
		},
	})
	d.win.RegisterHook(events.Mac.WindowDidOrderOnScreen, func(*application.WindowEvent) {
		configureMacWindow(d.win.NativeWindow())
	})
	d.win.RegisterHook(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
		configureMacWindow(d.win.NativeWindow())
		if runtime.GOOS == "darwin" {
			d.win.ExecJS("if(/Mac/.test(navigator.platform)){window.__bothubMacShell=true;document.documentElement.classList.add('mac-app');}")
		}
	})
	d.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { d.win.Hide(); e.Cancel() })
	d.app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) { d.win.Show() })

	tray := d.app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	} else {
		tray.SetIcon(icons.SystrayLight)
	}
	menu := d.app.NewMenu()
	menu.Add("打开 Pebble").OnClick(func(*application.Context) { d.win.Show(); d.win.Focus() })
	menu.Add("切换模式…").OnClick(func(*application.Context) { d.reset(); d.win.Show() })
	menu.Add("打开数据目录").OnClick(func(*application.Context) { _ = d.app.Browser.OpenFile(d.dir) })
	d.nodeItem = menu.AddCheckbox("允许主电脑操控本机", d.prefs.AllowNode)
	d.nodeItem.SetHidden(d.prefs.Mode != "remote")
	d.nodeItem.OnClick(func(*application.Context) {
		next := d.nodeItem.Checked()
		d.setLocalNode(&next, nil, nil)
	})
	d.takeoverItem = menu.Add("接管为本机运行（容灾模式）")
	d.takeoverItem.SetHidden(d.prefs.Mode != "remote" || d.prefs.Link == "")
	d.takeoverItem.OnClick(func(*application.Context) { go d.failoverToLocal() })
	d.pushbackItem = menu.Add("回传增量并切回远程主主机")
	d.pushbackItem.SetHidden(d.prefs.Mode != "local" || d.prefs.Link == "")
	d.pushbackItem.OnClick(func(*application.Context) { go d.failbackToRemote() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { d.app.Quit() })
	d.menu = menu
	tray.SetMenu(menu)

	if d.prefs.TailscaleBound {
		go func() {
			_, _ = d.tailscaleStart(context.Background(), "", d.prefs.TailscaleControlURL, false)
		}()
	}

	if d.prefs.Mode != "" {
		go d.start(d.prefs)
	} else {
		// Pre-warm harness auth & Magpie caches while user is on the setup screen
		// so clicking "Run Locally" transitions into onboarding immediately.
		go func() {
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
		}()
	}
	if err := d.app.Run(); err != nil {
		log.Fatal(err)
	}
}

// start runs mode p and points the window at its chat page.
func (d *desktop) start(p prefs) {
	d.halt()
	d.setStatusState("starting", "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	url, err := "", error(nil)
	switch p.Mode {
	case "local":
		url, err = d.startLocal(ctx, done)
	case "remote":
		url, err = d.startRemote(ctx, p.Link, done)
	default:
		err = errors.New("unknown mode")
	}
	if err != nil {
		cancel()
		d.setStatusState("error", err.Error())
		return
	}
	d.mu.Lock()
	d.prefs.Mode = p.Mode
	if p.Mode == "remote" {
		cleanLink, linkTok := tunnel.ExtractTokenFromLink(p.Link)
		if cleanLink != d.prefs.Link && linkTok == "" {
			d.prefs.RemoteToken = ""
		}
		d.prefs.Link = cleanLink
		if linkTok != "" {
			d.prefs.RemoteToken = linkTok
		}
	}
	d.stop = func() { cancel(); <-done }
	d.mu.Unlock()
	d.save()
	d.syncTray()
	d.setStatusState("", "")
	d.win.SetURL(url)
}

func (d *desktop) startLocal(ctx context.Context, done chan struct{}) (string, error) {
	if err := ensureConfig(d.dir); err != nil {
		close(done)
		return "", err
	}
	cfg, _, err := config.Load(d.dir, "")
	if err != nil {
		close(done)
		return "", err
	}
	base := cfg.HTTP.BaseURL()
	errc := make(chan error, 1)
	go func() { errc <- cli.Serve(ctx, d.dir); close(done) }()
	warmClient := &http.Client{Timeout: 3 * time.Second}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		select {
		case err := <-errc:
			return "", fmt.Errorf("本机服务退出：%v", err)
		default:
		}
		if r, err := warmClient.Get(base + "/healthz"); err == nil {
			r.Body.Close()
			if sr, serr := warmClient.Get(base + "/api/settings"); serr == nil {
				sr.Body.Close()
			}
			return base + "/", nil
		}
	}
	return "", errors.New("本机服务没有响应")
}

type remoteTarget struct {
	kind    string // "tailcat" | "tailscale" | "http"
	link    string // cleaned link without #token=
	token   string
	addr    string // host:port for tailscale dialing
	baseURL string // for kind == "http" or "tailscale"
}

func parseRemoteTarget(rawLink string) (remoteTarget, error) {
	link, tok := tunnel.ExtractTokenFromLink(rawLink)
	trimmed := strings.TrimSpace(link)
	if trimmed == "" {
		return remoteTarget{}, errors.New("remote address is required")
	}
	if strings.HasPrefix(trimmed, "tailcat://") {
		if _, _, err := tunnel.ParseLink(trimmed); err != nil {
			return remoteTarget{}, err
		}
		return remoteTarget{kind: "tailcat", link: trimmed, token: tok}, nil
	}
	if strings.HasPrefix(trimmed, "tailscale://") {
		addr, err := tunnel.ParseTailscaleLink(trimmed)
		if err != nil {
			return remoteTarget{}, err
		}
		return remoteTarget{
			kind:    "tailscale",
			link:    trimmed,
			token:   tok,
			addr:    addr,
			baseURL: "http://" + addr,
		}, nil
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		u, err := url.Parse(trimmed)
		if err != nil || u.Host == "" {
			return remoteTarget{}, fmt.Errorf("invalid remote address: %s", trimmed)
		}
		return remoteTarget{
			kind:    "http",
			link:    trimmed,
			token:   tok,
			baseURL: u.Scheme + "://" + u.Host,
		}, nil
	}
	return remoteTarget{}, errors.New("expected tailcat://, tailscale://, or http(s):// address")
}

func (d *desktop) startRemote(ctx context.Context, rawLink string, done chan struct{}) (string, error) {
	target, err := parseRemoteTarget(rawLink)
	if err != nil {
		close(done)
		return "", err
	}

	var (
		tr          *http.Transport
		closeClient func() error
		upstream    *url.URL
	)
	if target.kind == "tailcat" {
		dial, cfn, err := tunnel.Connect(ctx, target.link)
		if err != nil {
			close(done)
			return "", err
		}
		closeClient = cfn
		tr = &http.Transport{
			DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) { return dial(dctx) },
		}
	} else if target.kind == "tailscale" {
		u, err := url.Parse(target.baseURL)
		if err != nil {
			close(done)
			return "", err
		}
		upstream = u
		closeClient = func() error { return nil }
		d.mu.Lock()
		tsNode := d.tsNode
		tsBound := d.prefs.TailscaleBound
		tsCtrl := d.prefs.TailscaleControlURL
		d.mu.Unlock()
		if tsNode == nil && tsBound {
			if _, serr := d.tailscaleStart(ctx, "", tsCtrl, false); serr == nil {
				d.mu.Lock()
				tsNode = d.tsNode
				d.mu.Unlock()
			}
		}
		if tsNode != nil {
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			werr := tsNode.WaitRunning(wctx)
			wcancel()
			if werr == nil {
				dialAddr := target.addr
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
		req, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, target.baseURL+"/healthz", nil)
		resp, err := (&http.Client{Transport: tr}).Do(req)
		cancel()
		if err != nil {
			tr.CloseIdleConnections()
			close(done)
			return "", fmt.Errorf("cannot reach %s: %w", target.baseURL, err)
		}
		resp.Body.Close()
	} else {
		u, err := url.Parse(target.baseURL)
		if err != nil {
			close(done)
			return "", err
		}
		upstream = u
		closeClient = func() error { return nil }
		tr = &http.Transport{}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, target.baseURL+"/healthz", nil)
		resp, err := (&http.Client{Transport: tr}).Do(req)
		cancel()
		if err != nil {
			tr.CloseIdleConnections()
			close(done)
			return "", fmt.Errorf("cannot reach %s: %w", target.baseURL, err)
		}
		resp.Body.Close()
	}
	link := target.link
	linkTok := target.token

	// A fixed local port keeps the login cookie across restarts.
	ln, err := net.Listen("tcp", "127.0.0.1:3790")
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		_ = closeClient()
		close(done)
		return "", err
	}

	localURL := "http://" + ln.Addr().String()
	if tr == nil {
		tr = &http.Transport{}
	}

	d.mu.Lock()
	nodeID, nodeName := d.prefs.NodeID, d.prefs.NodeName
	allowNode, tok := d.prefs.AllowNode, linkTok
	if tok == "" && d.prefs.Link == link {
		tok = d.prefs.RemoteToken
	}
	perms := append([]string(nil), d.prefs.NodePerms...)
	d.mu.Unlock()

	agent := node.NewAgent(node.AgentOptions{
		NodeID:      nodeID,
		Name:        nodeName,
		BaseURL:     localURL,
		Token:       tok,
		Permissions: perms,
		Enabled:     allowNode,
		HTTPClient:  &http.Client{Transport: tr},
	})
	d.mu.Lock()
	d.nodeAgent = agent
	d.mu.Unlock()

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
			if resp.StatusCode == http.StatusOK && resp.Request != nil {
				if resp.Request.URL.Path == "/api/login" {
					for _, c := range resp.Cookies() {
						if c.Name == "bot_hub_token" && c.Value != "" {
							d.setRemoteToken(c.Value)
							break
						}
					}
				} else if strings.HasPrefix(resp.Request.URL.Path, "/api/") {
					if c, err := resp.Request.Cookie("bot_hub_token"); err == nil && c.Value != "" {
						d.setRemoteToken(c.Value)
					}
				}
			}
			return nil
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/local-node", d.handleLocalNode)
	mux.Handle("/", rp)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		defer close(done)
		defer func() {
			d.mu.Lock()
			if d.nodeAgent == agent {
				d.nodeAgent = nil
			}
			d.mu.Unlock()
			agent.Close()
			tr.CloseIdleConnections()
			_ = closeClient()
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
	return localURL + "/", nil
}

func (d *desktop) syncTray() {
	d.mu.Lock()
	mode, allow, link := d.prefs.Mode, d.prefs.AllowNode, d.prefs.Link
	item, takeoverItem, pushbackItem, menu := d.nodeItem, d.takeoverItem, d.pushbackItem, d.menu
	d.mu.Unlock()
	if item != nil {
		item.SetHidden(mode != "remote")
		item.SetChecked(allow)
	}
	if takeoverItem != nil {
		takeoverItem.SetHidden(mode != "remote" || link == "")
	}
	if pushbackItem != nil {
		pushbackItem.SetHidden(mode != "local" || link == "")
	}
	if menu != nil {
		menu.Update()
	}
}

func (d *desktop) peerHTTPClient(ctx context.Context, rawLink, fallbackToken string) (*http.Client, string, string, func(), error) {
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

func (d *desktop) failoverToLocal() {
	d.mu.Lock()
	p := d.prefs
	d.mu.Unlock()
	if p.Mode != "remote" || strings.TrimSpace(p.Link) == "" {
		return
	}
	d.setStatus("正在从主电脑同步数据并切换至本机接管…")
	if err := ensureConfig(d.dir); err != nil {
		d.setStatus("初始化本机配置失败：" + err.Error())
		return
	}
	cfg, _, err := config.Load(d.dir, "")
	if err != nil {
		d.setStatus("读取本机配置失败：" + err.Error())
		return
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		d.setStatus("打开本机状态库失败：" + err.Error())
		return
	}
	svc := syncer.NewWithStore(cfg, store)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	cli, peerBase, peerTok, cleanup, dialErr := d.peerHTTPClient(ctx, p.Link, p.RemoteToken)
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
	d.start(p)
}

func (d *desktop) failbackToRemote() {
	d.mu.Lock()
	p := d.prefs
	d.mu.Unlock()
	if p.Mode != "local" || strings.TrimSpace(p.Link) == "" {
		return
	}
	d.setStatus("正在回传接管增量至主电脑…")
	cfg, _, err := config.Load(d.dir, "")
	if err != nil {
		d.setStatus("读取本机配置失败：" + err.Error())
		return
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		d.setStatus("打开本机状态库失败：" + err.Error())
		return
	}
	svc := syncer.NewWithStore(cfg, store)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	cli, peerBase, peerTok, cleanup, err := d.peerHTTPClient(ctx, p.Link, p.RemoteToken)
	if err != nil {
		cancel()
		_ = store.Close()
		d.setStatus("连接主电脑失败，已保留在本机运行：" + err.Error())
		return
	}
	_, pushErr := svc.PushToPeer(ctx, cli, peerBase, peerTok)
	cleanup()
	cancel()
	if pushErr != nil {
		_ = store.Close()
		d.setStatus("回传增量失败，已保留在本机运行：" + pushErr.Error())
		return
	}
	if cfg.Sync.GitEnabled() {
		gctx, gcancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, _ = syncer.CheckpointPush(gctx, cfg, svc.HostID())
		gcancel()
	}
	_ = store.Close()

	p.Mode = "remote"
	d.start(p)
}

func (d *desktop) remoteToken() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.prefs.RemoteToken
}

func (d *desktop) setRemoteToken(tok string) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return
	}
	d.mu.Lock()
	changed := d.prefs.RemoteToken != tok
	if changed {
		d.prefs.RemoteToken = tok
	}
	agent := d.nodeAgent
	d.mu.Unlock()
	if changed {
		d.save()
	}
	if agent != nil {
		agent.SetToken(tok)
	}
}

func (d *desktop) setLocalNode(enabled *bool, name *string, perms []string) map[string]any {
	d.mu.Lock()
	if enabled != nil {
		d.prefs.AllowNode = *enabled
	}
	if name != nil {
		if n := strings.TrimSpace(*name); n != "" {
			d.prefs.NodeName = n
		}
	}
	if perms != nil {
		d.prefs.NodePerms = node.ValidPerms(perms)
	}
	allow := d.prefs.AllowNode
	nodeName := d.prefs.NodeName
	curPerms := append([]string(nil), d.prefs.NodePerms...)
	agent := d.nodeAgent
	d.mu.Unlock()

	d.save()
	d.syncTray()
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
		"nodeId":      d.prefs.NodeID,
		"name":        nodeName,
		"permissions": curPerms,
	}
}

func (d *desktop) handleLocalNode(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
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
		d.mu.Lock()
		agent := d.nodeAgent
		d.mu.Unlock()
		if agent == nil {
			writeJSON(w, map[string]any{"available": false})
			return
		}
		st := agent.Status()
		writeJSON(w, map[string]any{
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
		if tok := d.remoteToken(); tok != "" {
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
		writeJSON(w, d.setLocalNode(req.Enabled, req.Name, req.Permissions))

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (d *desktop) halt() {
	d.mu.Lock()
	stop := d.stop
	d.stop = nil
	d.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (d *desktop) reset() {
	d.halt()
	d.mu.Lock()
	d.prefs.Mode = ""
	d.mu.Unlock()
	d.save()
	d.syncTray()
	d.setStatus("")
	d.win.SetURL("/desktop-setup")
}

func (d *desktop) setStatus(s string) {
	d.mu.Lock()
	d.status = s
	d.statusState = ""
	d.statusError = ""
	d.mu.Unlock()
}

func (d *desktop) setStatusState(state, errMsg string) {
	d.mu.Lock()
	d.status = ""
	d.statusState = state
	d.statusError = errMsg
	d.mu.Unlock()
}

func (d *desktop) closeTailscale() {
	d.mu.Lock()
	n := d.tsNode
	d.tsNode = nil
	d.mu.Unlock()
	if n != nil {
		_ = n.Close()
	}
}

func (d *desktop) tailscaleStart(ctx context.Context, authKey, controlURL string, openBrowser bool) (tunnel.TailscaleState, error) {
	ctrl := strings.TrimSpace(controlURL)
	key := strings.TrimSpace(authKey)

	d.mu.Lock()
	if ctrl == "" {
		ctrl = d.prefs.TailscaleControlURL
	}
	existing := d.tsNode
	needRecreate := existing != nil && (ctrl != existing.Options().ControlURL || key != "")
	if needRecreate {
		d.tsNode = nil
	}
	d.mu.Unlock()

	if needRecreate && existing != nil {
		_ = existing.Close()
		existing = nil
	}

	if existing == nil {
		n := tunnel.NewTailscaleNode(tunnel.TailscaleOptions{
			StateDir:   filepath.Join(d.dir, "tailscale-client"),
			Hostname:   tunnel.DefaultTailscaleHostname("bothub-desktop"),
			ControlURL: ctrl,
			AuthKey:    key,
		})
		if err := n.Start(); err != nil {
			return tunnel.TailscaleState{State: "error", Error: err.Error()}, err
		}
		d.mu.Lock()
		d.tsNode = n
		d.prefs.TailscaleBound = true
		d.prefs.TailscaleControlURL = ctrl
		d.mu.Unlock()
		d.save()
		existing = n
	}

	wctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	st := existing.WaitStateChange(wctx)
	if st.State == "running" {
		st = existing.Status(ctx, true)
	}
	if openBrowser && st.State == "needs_login" && st.AuthURL != "" && d.app != nil {
		_ = d.app.Browser.OpenURL(st.AuthURL)
	}
	return st, nil
}

func (d *desktop) tailscaleStatus(ctx context.Context, includePeers bool) tunnel.TailscaleState {
	d.mu.Lock()
	n := d.tsNode
	d.mu.Unlock()
	if n == nil {
		return tunnel.TailscaleState{State: "disabled"}
	}
	return n.Status(ctx, includePeers)
}

func (d *desktop) tailscaleLogout(ctx context.Context) tunnel.TailscaleState {
	d.mu.Lock()
	n := d.tsNode
	d.tsNode = nil
	d.prefs.TailscaleBound = false
	d.mu.Unlock()
	d.save()
	if n != nil {
		_ = n.Logout(ctx)
	} else {
		_ = os.RemoveAll(filepath.Join(d.dir, "tailscale-client"))
	}
	return tunnel.TailscaleState{State: "disabled"}
}

// serveSetup is the in-app page shown before a mode runs (wails:// origin).
func (d *desktop) serveSetup(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/desktop" && r.Method == http.MethodGet:
		d.mu.Lock()
		out := map[string]any{
			"mode":        d.prefs.Mode,
			"link":        d.prefs.Link,
			"status":      d.status,
			"statusState": d.statusState,
			"statusError": d.statusError,
			"dataDir":     d.dir,
		}
		d.mu.Unlock()
		writeJSON(w, out)
	case r.URL.Path == "/api/desktop/start" && r.Method == http.MethodPost:
		var p prefs
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&p) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		p.Link = strings.TrimSpace(p.Link)
		go d.start(p)
		writeJSON(w, map[string]any{"ok": true})
	case r.URL.Path == "/api/desktop/tailscale" && r.Method == http.MethodGet:
		includePeers := r.URL.Query().Get("peers") == "1"
		writeJSON(w, d.tailscaleStatus(r.Context(), includePeers))
	case r.URL.Path == "/api/desktop/tailscale/start" && r.Method == http.MethodPost:
		var req struct {
			AuthKey    string `json:"authKey"`
			ControlURL string `json:"controlUrl"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req)
		st, _ := d.tailscaleStart(r.Context(), req.AuthKey, req.ControlURL, true)
		writeJSON(w, st)
	case r.URL.Path == "/api/desktop/tailscale/logout" && r.Method == http.MethodPost:
		writeJSON(w, d.tailscaleLogout(r.Context()))
	default:
		distFS := webassets.DistFS()
		clean := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if clean != "" && clean != "." {
			if info, err := fs.Stat(distFS, clean); err == nil && !info.IsDir() {
				http.FileServer(http.FS(distFS)).ServeHTTP(w, r)
				return
			}
		}
		raw, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			http.Error(w, "desktop setup assets missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(raw)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (d *desktop) load() {
	if raw, err := os.ReadFile(filepath.Join(d.dir, "desktop.json")); err == nil {
		_ = json.Unmarshal(raw, &d.prefs)
	}
	defID, defName := node.DefaultIdentity()
	if strings.TrimSpace(d.prefs.NodeID) == "" {
		d.prefs.NodeID = defID
	}
	if strings.TrimSpace(d.prefs.NodeName) == "" {
		d.prefs.NodeName = defName
	}
	if d.prefs.NodePerms == nil {
		d.prefs.NodePerms = []string{node.PermFiles, node.PermExec, node.PermDesktop}
	} else {
		d.prefs.NodePerms = node.ValidPerms(d.prefs.NodePerms)
	}
}

func (d *desktop) save() {
	d.mu.Lock()
	raw, _ := json.MarshalIndent(d.prefs, "", "  ")
	d.mu.Unlock()
	_ = os.WriteFile(filepath.Join(d.dir, "desktop.json"), raw, 0o600) // the link is a secret
}

// dataDir is where the app keeps config/, state/ and workspaces/.
func dataDir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("BOT_HUB_DATA_DIR")); custom != "" {
		return custom, os.MkdirAll(custom, 0o700)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "bot-hub")
	return dir, os.MkdirAll(dir, 0o700)
}

// ensureConfig writes a starter config/bots.json on first local run.
func ensureConfig(dir string) error {
	_, _, err := config.EnsureStarterConfig(dir, harness.PreferredInstalledHarness())
	return err
}

// shellEnv takes PATH and proxy settings from the login shell: an app started
// from Finder or the Dock gets a bare environment, without user-local bins or
// the proxy the DERP relay may need.
func shellEnv() {
	if runtime.GOOS == "windows" {
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		for _, cand := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
			if _, err := os.Stat(cand); err == nil {
				shell = cand
				break
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-ilc", "printf '\\n__ENV__\\n'; env").Output()
	if i := strings.LastIndex(string(out), "__ENV__\n"); err == nil && i >= 0 {
		for _, kv := range strings.Split(string(out[i+len("__ENV__\n"):]), "\n") {
			k, v, ok := strings.Cut(kv, "=")
			if ok && v != "" && (k == "PATH" || strings.HasSuffix(strings.ToLower(k), "_proxy")) {
				os.Setenv(k, v)
			}
		}
	}
	extraBins := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		extraBins = append(extraBins,
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		resBin := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "Resources", "bin"))
		if info, err := os.Stat(resBin); err == nil && info.IsDir() {
			extraBins = append(extraBins, resBin)
		}
	}
	pathVal := os.Getenv("PATH")
	for _, dir := range extraBins {
		pathVal = appendPathIfMissing(pathVal, dir)
	}
	os.Setenv("PATH", pathVal)
	if runtime.GOOS == "darwin" && strings.TrimSpace(os.Getenv("AGENT_BROWSER_EXECUTABLE_PATH")) == "" {
		if p := detectDefaultChromiumExecutable(); p != "" {
			os.Setenv("AGENT_BROWSER_EXECUTABLE_PATH", p)
		}
	}
}

func appendPathIfMissing(pathEnv, dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return pathEnv
	}
	cleanDir := filepath.Clean(dir)
	var parts []string
	found := false
	for _, seg := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		if seg == "" {
			continue
		}
		if filepath.Clean(seg) == cleanDir {
			found = true
		}
		parts = append(parts, seg)
	}
	if !found {
		parts = append(parts, cleanDir)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

var macChromiumApps = map[string]string{
	"com.microsoft.edgemac":        "Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"com.google.chrome":            "Google Chrome.app/Contents/MacOS/Google Chrome",
	"com.google.chrome.canary":     "Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	"com.brave.browser":            "Brave Browser.app/Contents/MacOS/Brave Browser",
	"company.thebrowser.browser":   "Arc.app/Contents/MacOS/Arc",
	"company.thebrowser.dia":       "Dia.app/Contents/MacOS/Dia",
	"com.vivaldi.vivaldi":          "Vivaldi.app/Contents/MacOS/Vivaldi",
	"org.chromium.chromium":        "Chromium.app/Contents/MacOS/Chromium",
}

var macChromiumFallbackBundles = []string{
	"com.microsoft.edgemac",
	"com.google.chrome",
	"com.brave.browser",
}

func detectDefaultChromiumExecutable() string {
	home, _ := os.UserHomeDir()
	var plistJSON []byte
	if home != "" {
		plistPath := filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices", "com.apple.launchservices.secure.plist")
		if _, err := os.Stat(plistPath); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", plistPath).Output(); err == nil {
				plistJSON = out
			}
		}
	}
	return chromiumExecutableFromHandlers(plistJSON, home, func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	})
}

func chromiumExecutableFromHandlers(plistJSON []byte, home string, exists func(string) bool) string {
	resolveBundle := func(bundleID string) string {
		rel, ok := macChromiumApps[strings.ToLower(strings.TrimSpace(bundleID))]
		if !ok {
			return ""
		}
		candidates := []string{filepath.Join("/Applications", rel)}
		if home != "" {
			candidates = append(candidates, filepath.Join(home, "Applications", rel))
		}
		for _, c := range candidates {
			if exists(c) {
				return c
			}
		}
		return ""
	}

	if len(plistJSON) > 0 {
		var doc struct {
			LSHandlers []struct {
				RoleAll     string `json:"LSHandlerRoleAll"`
				URLScheme   string `json:"LSHandlerURLScheme"`
				ContentType string `json:"LSHandlerContentType"`
			} `json:"LSHandlers"`
		}
		if json.Unmarshal(plistJSON, &doc) == nil {
			var httpsBundle, browserBundle string
			for _, h := range doc.LSHandlers {
				switch strings.ToLower(strings.TrimSpace(h.URLScheme)) {
				case "https":
					if httpsBundle == "" {
						httpsBundle = h.RoleAll
					}
				}
				if strings.EqualFold(strings.TrimSpace(h.ContentType), "com.apple.default-app.web-browser") && browserBundle == "" {
					browserBundle = h.RoleAll
				}
			}
			for _, b := range []string{httpsBundle, browserBundle} {
				if p := resolveBundle(b); p != "" {
					return p
				}
			}
		}
	}

	for _, b := range macChromiumFallbackBundles {
		if p := resolveBundle(b); p != "" {
			return p
		}
	}
	return ""
}
