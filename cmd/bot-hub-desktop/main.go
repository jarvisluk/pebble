// bot-hub-desktop is the desktop app: a window showing the chat page and a
// tray icon. It either runs the hub on this computer ("local") or opens a
// remote bot-hub through a Tailcat link ("remote"). Given arguments, it runs
// the CLI instead, so bots' `<exe> mcp ...` calls work from inside the app.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	"bot-hub/internal/tunnel"
)

//go:embed setup.html
var setupHTML []byte

//go:embed default-config.json
var defaultConfig []byte

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
	Mode string `json:"mode,omitempty"` // "" (ask) | local | remote
	Link string `json:"link,omitempty"`
}

type desktop struct {
	dir    string
	app    *application.App
	win    *application.WebviewWindow
	mu     sync.Mutex
	prefs  prefs
	status string // what setup.html shows while starting / after an error
	stop   func() // stops the running mode
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
		Title: "Bot Hub", Width: 1100, Height: 760, URL: "/",
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
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { d.app.Quit() })
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
	d.prefs, d.stop = p, func() { cancel(); <-done }
	d.mu.Unlock()
	d.save()
	d.setStatus("")
	d.win.SetURL(url)
}

func (d *desktop) startLocal(ctx context.Context, done chan struct{}) (string, error) {
	if _, err := exec.LookPath("opencode"); err != nil {
		close(done)
		return "", errors.New("没有找到 opencode，请先安装 OpenCode")
	}
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

func (d *desktop) startRemote(ctx context.Context, link string, done chan struct{}) (string, error) {
	if _, _, err := tunnel.ParseLink(link); err != nil {
		close(done)
		return "", err
	}
	ready := make(chan string, 1)
	errc := make(chan error, 1)
	// A fixed local port per link keeps the login cookie across restarts.
	go func() {
		errc <- tunnel.Forward(ctx, link, "127.0.0.1:3790", func(u string) { ready <- u })
		close(done)
	}()
	select {
	case u := <-ready:
		return u + "/", nil
	case err := <-errc:
		if err == nil {
			err = errors.New("连接已关闭")
		}
		return "", err
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
		_, err := exec.LookPath("opencode")
		out["opencode"] = err == nil
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
	path := filepath.Join(dir, "config", "bots.json")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, defaultConfig, 0o600)
}

// shellEnv takes PATH and proxy settings from the login shell: an app started
// from Finder or the Dock gets a bare environment, without ~/.opencode/bin or
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
	if home, err := os.UserHomeDir(); err == nil {
		os.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Join(home, ".opencode", "bin"))
	}
}
