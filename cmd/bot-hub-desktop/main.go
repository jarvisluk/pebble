// bot-hub-desktop is the desktop app: a window showing the chat page and a
// tray icon. It either runs the hub on this computer ("local") or opens a
// remote bot-hub through a Tailcat link ("remote"). Given arguments, it runs
// the CLI instead, so bots' `<exe> mcp ...` calls work from inside the app.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"bot-hub/internal/channel"
	"bot-hub/internal/cli"
	"bot-hub/internal/desktophost"
	webassets "bot-hub/internal/web"
)

//go:embed assets/tray_mac_template.png
var trayMacTemplateIcon []byte

//go:embed assets/tray_color.png
var trayColorIcon []byte

func main() {
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-psn") { // -psn_: old macOS Finder launch arg
		cli.Main(os.Args[1:])
		return
	}
	desktophost.ShellEnv()
	dir, err := desktophost.DataDir()
	if err != nil {
		log.Fatal(err)
	}
	d := &desktop{dir: dir}
	d.host = desktophost.New(dir, desktophost.Hooks{
		ServeLocal: cli.Serve,
		OnChange:   func(snap desktophost.Snapshot) { d.syncTrayFromSnapshot(snap) },
		OnModeURL:  func(u string) { d.setWindowURL(u) },
		OnReset:    func() { d.setWindowURL("/desktop-setup") },
		OnOpenURL: func(u string) {
			if d.app != nil {
				_ = d.app.Browser.OpenURL(u)
			}
		},
	})
	d.host.Load()
	channel.DesktopResetFunc = func() { go d.host.Reset() }
	channel.OnAPITokenChanged = func(tok string) { d.host.SetLocalToken(tok) }
	d.run()
}

type desktop struct {
	dir          string
	host         *desktophost.Host
	app          *application.App
	win          *application.WebviewWindow
	menu         *application.Menu
	nodeItem     *application.MenuItem
	takeoverItem *application.MenuItem
	pushbackItem *application.MenuItem
}

func (d *desktop) setWindowURL(u string) {
	if d.win != nil {
		d.win.SetURL(u)
	}
}

func (d *desktop) run() {
	d.app = application.New(application.Options{
		Name:        "Pebble",
		Description: "Personal bot hub",
		Assets:      application.AssetOptions{Handler: http.HandlerFunc(d.serveSetup), DisableLogging: true},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		OnShutdown: func() {
			d.host.Halt()
			d.host.CloseTailscale()
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

	snap := d.host.Snapshot()
	tray := d.app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayMacTemplateIcon)
	} else {
		tray.SetIcon(trayColorIcon)
	}
	menu := d.app.NewMenu()
	menu.Add("打开 Pebble").OnClick(func(*application.Context) { d.win.Show(); d.win.Focus() })
	menu.Add("切换模式…").OnClick(func(*application.Context) { d.host.Reset(); d.win.Show() })
	menu.Add("打开数据目录").OnClick(func(*application.Context) { _ = d.app.Browser.OpenFile(d.dir) })
	d.nodeItem = menu.AddCheckbox("允许主电脑操控本机", snap.AllowNode)
	d.nodeItem.SetHidden(snap.Mode != "remote")
	d.nodeItem.OnClick(func(*application.Context) {
		next := d.nodeItem.Checked()
		d.host.SetLocalNode(&next, nil, nil)
	})
	d.takeoverItem = menu.Add("接管为本机运行（容灾模式）")
	d.takeoverItem.SetHidden(!snap.CanFailover)
	d.takeoverItem.OnClick(func(*application.Context) { go d.host.FailoverToLocal() })
	d.pushbackItem = menu.Add("回传增量并切回远程主主机")
	d.pushbackItem.SetHidden(!snap.CanFailback)
	d.pushbackItem.OnClick(func(*application.Context) { go d.host.FailbackToRemote() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { d.app.Quit() })
	d.menu = menu
	tray.SetMenu(menu)

	p := d.host.Prefs()
	if p.TailscaleBound {
		go func() {
			_, _ = d.host.TailscaleStart(context.Background(), "", p.TailscaleControlURL, false)
		}()
	}
	if p.Mode != "" {
		go d.host.Start(p)
	} else {
		go d.host.WarmupCaches()
	}

	if err := d.app.Run(); err != nil {
		log.Fatal(err)
	}
}

func (d *desktop) syncTrayFromSnapshot(snap desktophost.Snapshot) {
	if d.nodeItem != nil {
		d.nodeItem.SetHidden(snap.Mode != "remote")
		d.nodeItem.SetChecked(snap.AllowNode)
	}
	if d.takeoverItem != nil {
		d.takeoverItem.SetHidden(!snap.CanFailover)
	}
	if d.pushbackItem != nil {
		d.pushbackItem.SetHidden(!snap.CanFailback)
	}
	if d.menu != nil {
		d.menu.Update()
	}
}

// serveSetup is the in-app page shown before a mode runs (wails:// origin).
func (d *desktop) serveSetup(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/desktop" && r.Method == http.MethodGet:
		snap := d.host.Snapshot()
		desktophost.WriteJSON(w, map[string]any{
			"mode":        snap.Mode,
			"link":        snap.Link,
			"status":      snap.Status,
			"statusState": snap.StatusState,
			"statusError": snap.StatusError,
			"dataDir":     snap.DataDir,
		})
	case r.URL.Path == "/api/desktop/start" && r.Method == http.MethodPost:
		var req struct {
			Mode        string `json:"mode"`
			Link        string `json:"link"`
			RemoteToken string `json:"remoteToken"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		p := d.host.Prefs()
		p.Mode = strings.TrimSpace(req.Mode)
		p.Link = strings.TrimSpace(req.Link)
		if tok := strings.TrimSpace(req.RemoteToken); tok != "" {
			p.RemoteToken = tok
		}
		go d.host.Start(p)
		desktophost.WriteJSON(w, map[string]any{"ok": true})
	case r.URL.Path == "/api/desktop/tailscale" && r.Method == http.MethodGet:
		includePeers := r.URL.Query().Get("peers") == "1"
		desktophost.WriteJSON(w, d.host.TailscaleStatus(r.Context(), includePeers))
	case r.URL.Path == "/api/desktop/tailscale/start" && r.Method == http.MethodPost:
		var req struct {
			AuthKey    string `json:"authKey"`
			ControlURL string `json:"controlUrl"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req)
		st, _ := d.host.TailscaleStart(r.Context(), req.AuthKey, req.ControlURL, true)
		desktophost.WriteJSON(w, st)
	case r.URL.Path == "/api/desktop/tailscale/logout" && r.Method == http.MethodPost:
		desktophost.WriteJSON(w, d.host.TailscaleLogout(r.Context()))
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
