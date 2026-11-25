package node

import (
	"bot-hub/internal/desktop"
	"bot-hub/internal/protocol"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
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
	desk   *desktop.EnginePool
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

func (a *Agent) desktopPool() *desktop.EnginePool {
	a.deskMu.Lock()
	defer a.deskMu.Unlock()
	if a.desk == nil {
		a.desk = desktop.NewEnginePool(desktop.VirtualConfig{})
	}
	return a.desk
}

func (a *Agent) desktopEngine() *desktop.Engine {
	return a.desktopPool().Default()
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
				a.setState("waiting_auth", false, "Waiting to sign in to primary host")
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

func (a *Agent) capabilities() []string {
	caps := []string{
		protocol.CapNodeExecEnv,
		protocol.CapNodeBrowserProfile,
		protocol.CapNodeDesktopPermissions,
		protocol.CapNodeDesktopPower,
	}
	if runtime.GOOS == "linux" && a.desktopPool().VirtualSupported() {
		caps = append(caps, protocol.CapNodeVirtualSlots)
	}
	return protocol.NormalizeCapabilities(caps)
}

func (a *Agent) pollOnce(ctx context.Context) (*Command, int, error) {
	name, perms, token := a.snapshot()
	payload, _ := json.Marshal(PollRequest{
		ID:              a.nodeID,
		Name:            name,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Hostname:        a.hostname,
		HomeDir:         a.homeDir,
		Permissions:     perms,
		ProtocolVersion: protocol.CurrentProtocol,
		Capabilities:    a.capabilities(),
		Version:         protocol.AppVersion,
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
		return Result{OK: false, Error: fmt.Sprintf("%q permission is not enabled on this host", PermLabel(reqPerm))}
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
	case "desktop_status", "desktop_permissions", "desktop_permission_request",
		"desktop_windows", "desktop_focus",
		"desktop_frame", "desktop_input", "desktop_stream_offer",
		"desktop_stream_close", "desktop_stream_set_input",
		"desktop_setup", "desktop_power":
		return a.execDesktop(ctx, cmd.Action, cmd.Args)
	default:
		return Result{OK: false, Error: fmt.Sprintf("unsupported action %q", cmd.Action)}
	}
}
