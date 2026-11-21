// bot-hub-desktop is the desktop app: a window showing the chat page and a
// tray icon. It either runs the hub on this computer ("local") or opens a
// remote bot-hub through a Tailcat link ("remote"). Given arguments, it runs
// the CLI instead, so bots' `<exe> mcp ...` calls work from inside the app.
package main

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
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
	"bot-hub/internal/tunnel"
)

//go:embed setup.html
var setupHTML []byte

var desktopHarnesses = []struct {
	id    string
	label string
}{
	{"opencode", "OpenCode"},
	{"claude", "Claude Code"},
	{"pi", "Pi"},
	{"dsh", "DeepSeek Harness"},
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
	Mode        string   `json:"mode,omitempty"` // "" (ask) | local | remote
	Link        string   `json:"link,omitempty"`
	RemoteToken string   `json:"remoteToken,omitempty"`
	NodeID      string   `json:"nodeId,omitempty"`
	NodeName    string   `json:"nodeName,omitempty"`
	AllowNode   bool     `json:"allowNode,omitempty"`
	NodePerms   []string `json:"nodePerms"`
}

type desktop struct {
	dir       string
	app       *application.App
	win       *application.WebviewWindow
	menu      *application.Menu
	nodeItem  *application.MenuItem
	mu        sync.Mutex
	prefs     prefs
	status    string // what setup.html shows while starting / after an error
	stop      func() // stops the running mode
	nodeAgent *node.Agent
}

func (d *desktop) run() {
	d.app = application.New(application.Options{
		Name:        "Bot Hub",
		Description: "Personal bot hub",
		Assets:      application.AssetOptions{Handler: http.HandlerFunc(d.serveSetup), DisableLogging: true},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		OnShutdown:  d.halt,
	})
	d.win = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Bot Hub",
		Width:            1100,
		Height:           760,
		MinWidth:         680,
		MinHeight:        520,
		BackgroundColour: application.NewRGB(239, 236, 232),
		URL:              "/",
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
	menu.Add("打开 Bot Hub").OnClick(func(*application.Context) { d.win.Show(); d.win.Focus() })
	menu.Add("切换模式…").OnClick(func(*application.Context) { d.reset(); d.win.Show() })
	menu.Add("打开数据目录").OnClick(func(*application.Context) { _ = d.app.Browser.OpenFile(d.dir) })
	d.nodeItem = menu.AddCheckbox("允许主电脑操控本机", d.prefs.AllowNode)
	d.nodeItem.SetHidden(d.prefs.Mode != "remote")
	d.nodeItem.OnClick(func(*application.Context) {
		next := d.nodeItem.Checked()
		d.setLocalNode(&next, nil, nil)
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { d.app.Quit() })
	d.menu = menu
	tray.SetMenu(menu)

	if d.prefs.Mode != "" {
		go d.start(d.prefs)
	}
	if err := d.app.Run(); err != nil {
		log.Fatal(err)
	}
}

// start runs mode p and points the window at its chat page.
func (d *desktop) start(p prefs) {
	d.halt()
	d.setStatus("正在启动…")
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
		d.setStatus("启动失败：" + err.Error())
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
	d.setStatus("")
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
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		select {
		case err := <-errc:
			return "", fmt.Errorf("本机服务退出：%v", err)
		default:
		}
		if r, err := http.Get(base + "/healthz"); err == nil {
			r.Body.Close()
			return base + "/", nil
		}
	}
	return "", errors.New("本机服务没有响应")
}

func (d *desktop) startRemote(ctx context.Context, rawLink string, done chan struct{}) (string, error) {
	link, linkTok := tunnel.ExtractTokenFromLink(rawLink)
	if _, _, err := tunnel.ParseLink(link); err != nil {
		close(done)
		return "", err
	}
	dial, closeClient, err := tunnel.Connect(ctx, link)
	if err != nil {
		close(done)
		return "", err
	}

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
	tr := &http.Transport{
		DialContext: func(dctx context.Context, _, _ string) (net.Conn, error) { return dial(dctx) },
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
	mode, allow := d.prefs.Mode, d.prefs.AllowNode
	item, menu := d.nodeItem, d.menu
	d.mu.Unlock()
	if item != nil {
		item.SetHidden(mode != "remote")
		item.SetChecked(allow)
	}
	if menu != nil {
		menu.Update()
	}
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
	d.win.SetURL("/")
}

func (d *desktop) setStatus(s string) { d.mu.Lock(); d.status = s; d.mu.Unlock() }

// serveSetup is the in-app page shown before a mode runs (wails:// origin).
func (d *desktop) serveSetup(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/desktop" && r.Method == http.MethodGet:
		d.mu.Lock()
		out := map[string]any{"mode": d.prefs.Mode, "link": d.prefs.Link, "status": d.status, "dataDir": d.dir}
		d.mu.Unlock()
		harnesses := make([]map[string]any, 0, len(desktopHarnesses))
		installedCount := 0
		for _, item := range desktopHarnesses {
			ok, _ := harness.DetectHarness(item.id)
			if ok {
				installedCount++
			}
			harnesses = append(harnesses, map[string]any{
				"id":        item.id,
				"label":     item.label,
				"installed": ok,
			})
		}
		out["harnesses"] = harnesses
		out["installedCount"] = installedCount
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
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(setupHTML)
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
		shell = "/bin/zsh"
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
	curPath := os.Getenv("PATH")
	parts := strings.Split(curPath, string(os.PathListSeparator))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		if p != "" {
			seen[p] = true
		}
	}
	for _, p := range extraBins {
		if !seen[p] {
			parts = append(parts, p)
			seen[p] = true
		}
	}
	os.Setenv("PATH", strings.Join(parts, string(os.PathListSeparator)))
}
