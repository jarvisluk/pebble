package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/desktop"
)

const (
	maxExecOutputBytes = 512 * 1024
	maxFileReadBytes   = 1024 * 1024
	defaultFileReadCap = 256 * 1024
	maxScreenshotBytes = 4 * 1024 * 1024
)

// AgentOptions configures a remote device node worker.
type AgentOptions struct {
	NodeID      string
	Name        string
	BaseURL     string
	Token       string
	Permissions []string
	Enabled     bool
	HTTPClient  *http.Client
	OnState     func()
}

// AgentStatus describes the live connection state of the local node agent.
type AgentStatus struct {
	Enabled     bool     `json:"enabled"`
	Connected   bool     `json:"connected"`
	Status      string   `json:"status"` // disabled | connecting | connected | waiting_auth | error
	Error       string   `json:"error,omitempty"`
	NodeID      string   `json:"nodeId"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// DefaultIdentity derives a stable node ID and display name from the OS hostname.
func DefaultIdentity() (id, name string) {
	host, _ := os.Hostname()
	name = strings.TrimSpace(host)
	for _, sfx := range []string{".local", ".lan", ".home"} {
		if strings.HasSuffix(strings.ToLower(name), sfx) {
			name = name[:len(name)-len(sfx)]
			break
		}
	}
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	id = strings.Trim(b.String(), "-._")
	if id == "" {
		id = runtime.GOOS + "-node"
	}
	if name == "" {
		name = id
	}
	return id, name
}

// Agent runs on a secondary machine, long-polling the hub for commands and
// executing permitted actions inside the local GUI session.
type Agent struct {
	nodeID   string
	baseURL  string
	hostname string
	homeDir  string
	client   *http.Client
	onState  func()

	mu         sync.Mutex
	enabled    bool
	name       string
	perms      []string
	token      string
	status     string
	errMsg     string
	connected  bool
	loopCancel context.CancelFunc
	loopDone   chan struct{}
	wakeCh     chan struct{}
	closed     bool

	deskMu sync.Mutex
	desk   *desktop.Engine
}

// NewAgent initializes a node Agent and starts polling if opts.Enabled is true.
func NewAgent(opts AgentOptions) *Agent {
	defID, defName := DefaultIdentity()
	id := strings.TrimSpace(opts.NodeID)
	if id == "" || !validNodeIDRe.MatchString(id) {
		id = defID
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = defName
	}
	host, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 40 * time.Second}
	}
	perms := ValidPerms(opts.Permissions)
	a := &Agent{
		nodeID:   id,
		baseURL:  strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/"),
		hostname: host,
		homeDir:  home,
		client:   client,
		onState:  opts.OnState,
		enabled:  opts.Enabled,
		name:     name,
		perms:    perms,
		token:    strings.TrimSpace(opts.Token),
		status:   "disabled",
		wakeCh:   make(chan struct{}, 1),
	}
	if opts.Enabled {
		a.Configure(true, name, perms)
	}
	return a
}

func (a *Agent) notifyState() {
	if a.onState != nil {
		go a.onState()
	}
}

// Status returns a snapshot of the node agent's current state.
func (a *Agent) Status() AgentStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AgentStatus{
		Enabled:     a.enabled,
		Connected:   a.connected,
		Status:      a.status,
		Error:       a.errMsg,
		NodeID:      a.nodeID,
		Name:        a.name,
		Permissions: append([]string(nil), a.perms...),
	}
}

// SetToken updates the bearer token used for hub requests and wakes waiting polls.
func (a *Agent) SetToken(token string) {
	token = strings.TrimSpace(token)
	a.mu.Lock()
	if a.token == token {
		a.mu.Unlock()
		return
	}
	a.token = token
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
	a.mu.Unlock()
}

// Configure updates enablement, display name, and granted permission scopes.
func (a *Agent) Configure(enabled bool, name string, perms []string) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	prevCancel := a.loopCancel
	prevDone := a.loopDone
	a.loopCancel = nil
	a.loopDone = nil

	a.enabled = enabled
	if n := strings.TrimSpace(name); n != "" {
		a.name = n
	}
	if perms != nil {
		a.perms = ValidPerms(perms)
	}
	if prevCancel != nil {
		prevCancel()
	}
	a.mu.Unlock()

	if prevDone != nil {
		<-prevDone
	}

	if !enabled {
		a.sendDisconnect()
		a.mu.Lock()
		a.status = "disabled"
		a.connected = false
		a.errMsg = ""
		a.mu.Unlock()
		a.notifyState()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.mu.Lock()
	if a.closed || !a.enabled {
		a.mu.Unlock()
		cancel()
		return
	}
	a.status = "connecting"
	a.connected = false
	a.errMsg = ""
	a.loopCancel = cancel
	a.loopDone = done
	a.mu.Unlock()

	a.notifyState()
	go a.loop(ctx, done)
}

// Close stops polling and sends a best-effort disconnect notice to the hub.
func (a *Agent) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	wasEnabled := a.enabled || a.connected
	a.enabled = false
	cancel := a.loopCancel
	done := a.loopDone
	a.loopCancel = nil
	a.loopDone = nil
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	if wasEnabled {
		a.sendDisconnect()
	}
	a.deskMu.Lock()
	d := a.desk
	a.desk = nil
	a.deskMu.Unlock()
	if d != nil {
		d.Close()
	}
}

func (a *Agent) desktopEngine() *desktop.Engine {
	a.deskMu.Lock()
	defer a.deskMu.Unlock()
	if a.desk == nil {
		a.desk = desktop.NewEngine()
	}
	return a.desk
}

func (a *Agent) setState(status string, connected bool, errMsg string) {
	a.mu.Lock()
	changed := a.status != status || a.connected != connected || a.errMsg != errMsg
	a.status = status
	a.connected = connected
	a.errMsg = errMsg
	a.mu.Unlock()
	if changed {
		a.notifyState()
	}
}

func (a *Agent) snapshot() (name string, perms []string, token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.name, append([]string(nil), a.perms...), a.token
}

func (a *Agent) hasPerm(perm string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Contains(a.perms, perm)
}

func (a *Agent) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	backoff := 2 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		cmd, statusCode, err := a.pollOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if statusCode == http.StatusUnauthorized {
				a.setState("waiting_auth", false, "等待登录主电脑")
				wait := 2 * time.Second
				select {
				case <-time.After(wait):
				case <-a.wakeCh:
				case <-ctx.Done():
					return
				}
				continue
			}
			a.setState("error", false, err.Error())
			select {
			case <-time.After(backoff):
			case <-a.wakeCh:
				backoff = 2 * time.Second
			case <-ctx.Done():
				return
			}
			if backoff < 8*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second
		a.setState("connected", true, "")
		if cmd != nil {
			c := *cmd
			go a.runCommand(ctx, c)
		}
	}
}

func (a *Agent) pollOnce(ctx context.Context) (*Command, int, error) {
	name, perms, token := a.snapshot()
	payload, _ := json.Marshal(PollRequest{
		ID:          a.nodeID,
		Name:        name,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Hostname:    a.hostname,
		HomeDir:     a.homeDir,
		Permissions: perms,
	})
	reqCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.baseURL+"/api/nodes/poll", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 300 {
		var errObj struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errObj) == nil && errObj.Error != "" {
			return nil, resp.StatusCode, errors.New(errObj.Error)
		}
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var cmd Command
	if err := json.Unmarshal(body, &cmd); err != nil {
		return nil, resp.StatusCode, err
	}
	return &cmd, resp.StatusCode, nil
}

func (a *Agent) sendDisconnect() {
	if a.baseURL == "" {
		return
	}
	_, _, token := a.snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"id": a.nodeID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/nodes/disconnect", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if resp, err := a.client.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (a *Agent) runCommand(parentCtx context.Context, cmd Command) {
	timeout := time.Duration(cmd.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	res := a.execute(ctx, cmd)
	res.CallID = cmd.CallID
	res.NodeID = a.nodeID

	_, _, token := a.snapshot()
	postCtx, postCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer postCancel()
	raw, _ := json.Marshal(res)
	req, err := http.NewRequestWithContext(postCtx, http.MethodPost, a.baseURL+"/api/nodes/result", bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if resp, err := a.client.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (a *Agent) execute(ctx context.Context, cmd Command) Result {
	reqPerm, err := RequiredPerm(cmd.Action)
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	if !a.hasPerm(reqPerm) {
		return Result{OK: false, Error: fmt.Sprintf("本机未开启「%s」权限", PermLabel(reqPerm))}
	}

	switch cmd.Action {
	case "exec":
		return a.execShell(ctx, cmd.Args)
	case "file_read":
		return a.fileRead(cmd.Args)
	case "file_write":
		return a.fileWrite(cmd.Args)
	case "file_list":
		return a.fileList(cmd.Args)
	case "screenshot":
		return a.takeScreenshot(ctx, cmd.Args)
	case "osascript":
		return a.runOSAScript(ctx, cmd.Args)
	case "open":
		return a.openTarget(ctx, cmd.Args)
	case "desktop_status", "desktop_windows", "desktop_focus",
		"desktop_frame", "desktop_input", "desktop_stream_offer",
		"desktop_stream_close", "desktop_stream_set_input":
		return a.execDesktop(ctx, cmd.Action, cmd.Args)
	default:
		return Result{OK: false, Error: fmt.Sprintf("unsupported action %q", cmd.Action)}
	}
}

func (a *Agent) execDesktop(ctx context.Context, action string, raw json.RawMessage) Result {
	eng := a.desktopEngine()
	jsonOut := func(v any, err error) Result {
		if err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		b, merr := json.Marshal(v)
		if merr != nil {
			return Result{OK: false, Error: merr.Error()}
		}
		return Result{OK: true, Output: string(b)}
	}

	switch action {
	case "desktop_status":
		st, err := eng.Status(ctx)
		return jsonOut(st, err)
	case "desktop_windows":
		wins, err := eng.Windows(ctx)
		if wins == nil {
			wins = []desktop.WindowInfo{}
		}
		return jsonOut(map[string]any{"windows": wins}, err)
	case "desktop_focus":
		var args struct {
			App      string `json:"app"`
			WindowID string `json:"windowId"`
		}
		_ = json.Unmarshal(raw, &args)
		if err := eng.Focus(ctx, args.App, args.WindowID); err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		return Result{OK: true, Output: "focused"}
	case "desktop_frame":
		var args struct {
			WindowID string `json:"windowId"`
			MaxDim   int    `json:"maxDim"`
			Quality  int    `json:"quality"`
		}
		_ = json.Unmarshal(raw, &args)
		fr, err := eng.Frame(ctx, args.WindowID, args.MaxDim, args.Quality)
		return jsonOut(fr, err)
	case "desktop_input":
		var ev desktop.UnifiedInput
		if err := json.Unmarshal(raw, &ev); err != nil {
			return Result{OK: false, Error: "invalid input payload"}
		}
		if err := eng.SendInput(ctx, ev); err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		return Result{OK: true, Output: `{"ok":true}`}
	case "desktop_stream_offer":
		var args struct {
			SDP     string `json:"sdp"`
			Target  string `json:"target"`
			FPS     int    `json:"fps"`
			MaxDim  int    `json:"maxDim"`
			Bitrate int    `json:"bitrate"`
		}
		if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.SDP) == "" {
			return Result{OK: false, Error: "sdp is required"}
		}
		res, err := eng.StreamOffer(ctx, args.SDP, args.Target, args.FPS, args.MaxDim, args.Bitrate)
		return jsonOut(res, err)
	case "desktop_stream_close":
		var args struct {
			Session string `json:"session"`
		}
		_ = json.Unmarshal(raw, &args)
		if err := eng.StreamClose(ctx, args.Session); err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		return Result{OK: true, Output: `{"ok":true}`}
	case "desktop_stream_set_input":
		var args struct {
			Enabled bool `json:"enabled"`
		}
		_ = json.Unmarshal(raw, &args)
		if err := eng.SetStreamInput(ctx, args.Enabled); err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		return Result{OK: true, Output: `{"ok":true}`}
	default:
		return Result{OK: false, Error: "unknown desktop action"}
	}
}

func (a *Agent) execShell(ctx context.Context, raw json.RawMessage) Result {
	var args struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Command) == "" {
		return Result{OK: false, Error: "command is required"}
	}
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", args.Command)
	} else {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
			if _, err := os.Stat(shell); err != nil {
				shell = "/bin/sh"
			}
		}
		c = exec.CommandContext(ctx, shell, "-lc", args.Command)
	}
	if cwd := expandHome(args.Cwd, a.homeDir); cwd != "" {
		c.Dir = cwd
	}
	buf := &limitedBuffer{limit: maxExecOutputBytes}
	c.Stdout = buf
	c.Stderr = buf
	err := c.Run()
	out := buf.String()
	if buf.truncated {
		out += "\n[output truncated at 512KB]"
	}
	if err != nil {
		exitCode := 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
			return Result{OK: true, ExitCode: exitCode, Output: out}
		}
		return Result{OK: false, ExitCode: exitCode, Output: out, Error: err.Error()}
	}
	return Result{OK: true, ExitCode: 0, Output: out}
}

func (a *Agent) fileRead(raw json.RawMessage) Result {
	var args struct {
		Path     string `json:"path"`
		MaxBytes int    `json:"maxBytes"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Path) == "" {
		return Result{OK: false, Error: "path is required"}
	}
	p := expandHome(args.Path, a.homeDir)
	capBytes := args.MaxBytes
	if capBytes <= 0 {
		capBytes = defaultFileReadCap
	}
	if capBytes > maxFileReadBytes {
		capBytes = maxFileReadBytes
	}
	f, err := os.Open(p)
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(capBytes)+1))
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	truncated := len(data) > capBytes
	if truncated {
		data = data[:capBytes]
	}
	out := string(data)
	if truncated {
		out += fmt.Sprintf("\n[truncated at %d bytes]", capBytes)
	}
	return Result{OK: true, Output: out}
}

func (a *Agent) fileWrite(raw json.RawMessage) Result {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Path) == "" {
		return Result{OK: false, Error: "path is required"}
	}
	p := expandHome(args.Path, a.homeDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	if args.Append {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		n, werr := f.WriteString(args.Content)
		cerr := f.Close()
		if werr != nil {
			return Result{OK: false, Error: werr.Error()}
		}
		if cerr != nil {
			return Result{OK: false, Error: cerr.Error()}
		}
		return Result{OK: true, Output: fmt.Sprintf("appended %d bytes to %s", n, p)}
	}
	if err := os.WriteFile(p, []byte(args.Content), 0o644); err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	return Result{OK: true, Output: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), p)}
}

func (a *Agent) fileList(raw json.RawMessage) Result {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Path) == "" {
		return Result{OK: false, Error: "path is required"}
	}
	p := expandHome(args.Path, a.homeDir)
	entries, err := os.ReadDir(p)
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	type item struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Size    int64  `json:"size,omitempty"`
		ModTime string `json:"modTime,omitempty"`
	}
	limit := min(len(entries), 500)
	out := make([]item, 0, limit)
	for _, ent := range entries[:limit] {
		kind := "file"
		if ent.IsDir() {
			kind = "dir"
		} else if ent.Type()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		it := item{Name: ent.Name(), Type: kind}
		if info, err := ent.Info(); err == nil {
			it.Size = info.Size()
			it.ModTime = info.ModTime().UTC().Format(time.RFC3339)
		}
		out = append(out, it)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	text := string(b)
	if len(entries) > limit {
		text += fmt.Sprintf("\n[showing first %d of %d entries]", limit, len(entries))
	}
	return Result{OK: true, Output: text}
}

func (a *Agent) takeScreenshot(ctx context.Context, raw json.RawMessage) Result {
	var args struct {
		MaxWidth int `json:"maxWidth"`
	}
	_ = json.Unmarshal(raw, &args)
	maxWidth := args.MaxWidth
	if maxWidth <= 0 {
		maxWidth = 1920
	}

	tmpDir, err := os.MkdirTemp("", "bothub-shot-*")
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	defer os.RemoveAll(tmpDir)

	switch runtime.GOOS {
	case "darwin":
		pngPath := filepath.Join(tmpDir, "screen.png")
		capCmd := exec.CommandContext(ctx, "/usr/sbin/screencapture", "-x", "-t", "png", pngPath)
		if out, err := capCmd.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return Result{OK: false, Error: fmt.Sprintf("screencapture 失败（请检查「系统设置 → 隐私与安全性 → 屏幕录制」是否已允许 Bot Hub）: %s", msg)}
		}

		jpgPath := filepath.Join(tmpDir, "screen.jpg")
		sipsCmd := exec.CommandContext(ctx, "/usr/bin/sips", "-Z", strconv.Itoa(maxWidth), "-s", "format", "jpeg", "-s", "formatOptions", "80", pngPath, "--out", jpgPath)
		if err := sipsCmd.Run(); err == nil {
			if data, rerr := os.ReadFile(jpgPath); rerr == nil && len(data) > 0 && len(data) <= maxScreenshotBytes {
				return Result{
					OK:         true,
					MimeType:   "image/jpeg",
					DataBase64: base64.StdEncoding.EncodeToString(data),
					Output:     fmt.Sprintf("captured %d bytes (image/jpeg)", len(data)),
				}
			}
		}

		data, err := os.ReadFile(pngPath)
		if err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		if len(data) > maxScreenshotBytes {
			return Result{OK: false, Error: fmt.Sprintf("screenshot too large (%d bytes)", len(data))}
		}
		return Result{
			OK:         true,
			MimeType:   "image/png",
			DataBase64: base64.StdEncoding.EncodeToString(data),
			Output:     fmt.Sprintf("captured %d bytes (image/png)", len(data)),
		}

	case "windows":
		jpgPath := filepath.Join(tmpDir, "screen.jpg")
		ps := `
Add-Type -AssemblyName System.Windows.Forms,System.Drawing
$b = [System.Windows.Forms.SystemInformation]::VirtualScreen
$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($b.Left, $b.Top, 0, 0, $bmp.Size)
$g.Dispose()
$maxW = [int]$env:BOTHUB_SHOT_MAXW
if ($maxW -gt 0 -and $bmp.Width -gt $maxW) {
  $h = [int]($bmp.Height * ($maxW / $bmp.Width))
  $scaled = New-Object System.Drawing.Bitmap $bmp, $maxW, $h
  $bmp.Dispose()
  $bmp = $scaled
}
$bmp.Save($env:BOTHUB_SHOT_PATH, [System.Drawing.Imaging.ImageFormat]::Jpeg)
$bmp.Dispose()
`
		c := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
		c.Env = append(os.Environ(),
			"BOTHUB_SHOT_PATH="+jpgPath,
			"BOTHUB_SHOT_MAXW="+strconv.Itoa(maxWidth),
		)
		if out, err := c.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return Result{OK: false, Error: fmt.Sprintf("windows screenshot failed: %s", msg)}
		}
		data, err := os.ReadFile(jpgPath)
		if err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		if len(data) > maxScreenshotBytes {
			return Result{OK: false, Error: fmt.Sprintf("screenshot too large (%d bytes)", len(data))}
		}
		return Result{
			OK:         true,
			MimeType:   "image/jpeg",
			DataBase64: base64.StdEncoding.EncodeToString(data),
			Output:     fmt.Sprintf("captured %d bytes (image/jpeg)", len(data)),
		}

	default:
		return Result{OK: false, Error: "screenshot is supported on macOS and Windows nodes"}
	}
}

func (a *Agent) runOSAScript(ctx context.Context, raw json.RawMessage) Result {
	var args struct {
		Script   string `json:"script"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Script) == "" {
		return Result{OK: false, Error: "script is required"}
	}
	out, err := RunOSAScript(ctx, args.Script, args.Language)
	if err != nil {
		return Result{OK: false, ExitCode: 1, Error: err.Error()}
	}
	return Result{OK: true, Output: out}
}

// RunOSAScript runs an AppleScript or JXA snippet on macOS inside the current GUI session.
func RunOSAScript(ctx context.Context, script, language string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("osascript is only supported on macOS")
	}
	var cmdArgs []string
	if strings.EqualFold(language, "JavaScript") || strings.EqualFold(language, "jxa") {
		cmdArgs = append(cmdArgs, "-l", "JavaScript")
	}
	cmdArgs = append(cmdArgs, "-")
	c := exec.CommandContext(ctx, "/usr/bin/osascript", cmdArgs...)
	c.Stdin = strings.NewReader(script)
	buf := &limitedBuffer{limit: maxExecOutputBytes}
	c.Stdout = buf
	c.Stderr = buf
	if err := c.Run(); err != nil {
		out := strings.TrimSpace(buf.String())
		if out == "" {
			out = err.Error()
		}
		return "", errors.New(out)
	}
	return strings.TrimRight(buf.String(), "\r\n"), nil
}

func (a *Agent) openTarget(ctx context.Context, raw json.RawMessage) Result {
	var args struct {
		Target string `json:"target"`
		App    string `json:"app"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{OK: false, Error: "invalid open args"}
	}
	if err := OpenTarget(ctx, args.Target, args.App, a.homeDir); err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	return Result{OK: true, Output: "opened"}
}

// OpenTarget opens a file, URL, or application inside the current GUI session.
func OpenTarget(ctx context.Context, target, app, homeDir string) error {
	target = strings.TrimSpace(target)
	app = strings.TrimSpace(app)
	if target == "" && app == "" {
		return errors.New("target or app is required")
	}
	if target != "" && !strings.Contains(target, "://") {
		target = expandHome(target, homeDir)
	}

	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		var cmdArgs []string
		if app != "" {
			cmdArgs = append(cmdArgs, "-a", app)
		}
		if target != "" {
			cmdArgs = append(cmdArgs, target)
		}
		c = exec.CommandContext(ctx, "/usr/bin/open", cmdArgs...)
	case "windows":
		cmdArgs := []string{"/c", "start", ""}
		if app != "" {
			cmdArgs = append(cmdArgs, app)
		}
		if target != "" {
			cmdArgs = append(cmdArgs, target)
		}
		c = exec.CommandContext(ctx, "cmd", cmdArgs...)
	default:
		return errors.New("open is supported on macOS and Windows")
	}

	if out, err := c.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

func expandHome(p, home string) string {
	p = strings.TrimSpace(p)
	if p == "" || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rem := b.limit - b.buf.Len(); rem > 0 {
		if len(p) > rem {
			_, _ = b.buf.Write(p[:rem])
			b.truncated = true
		} else {
			_, _ = b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
