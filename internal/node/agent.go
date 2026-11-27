package node

import (
	"bot-hub/internal/desktop"
	"bot-hub/internal/harness"
	"bot-hub/internal/protocol"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	maxExecOutputBytes = 512 * 1024
	maxFileReadBytes   = 1024 * 1024
	defaultFileReadCap = 256 * 1024
	maxScreenshotBytes = 4 * 1024 * 1024
)

// AgentOptions configures a remote device node worker.
type AgentOptions struct {
	NodeID            string
	Name              string
	BaseURL           string
	Token             string
	Permissions       []string
	Enabled           bool
	HTTPClient        *http.Client
	OnState           func()
	Workspaces        []NodeWorkspace
	MaxPermissionMode string
	HarnessRegistry   *harness.Registry
	DisableWS         bool
	ProbeHarnesses    func(context.Context, *harness.Registry) []NodeHarnessInfo
}

// AgentStatus describes the live connection state of the local node agent.
type AgentStatus struct {
	Enabled           bool              `json:"enabled"`
	Connected         bool              `json:"connected"`
	Status            string            `json:"status"` // disabled | connecting | connected | waiting_auth | error
	Transport         string            `json:"transport,omitempty"`
	Error             string            `json:"error,omitempty"`
	NodeID            string            `json:"nodeId"`
	Name              string            `json:"name"`
	Permissions       []string          `json:"permissions"`
	Harnesses         []NodeHarnessInfo `json:"harnesses,omitempty"`
	Workspaces        []NodeWorkspace   `json:"workspaces,omitempty"`
	MaxPermissionMode string            `json:"maxPermissionMode,omitempty"`
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

// Agent runs on a secondary machine, connecting to the hub over WebSocket
// (with HTTP long-poll fallback) to execute permitted actions and remote agent turns.
type Agent struct {
	nodeID         string
	baseURL        string
	hostname       string
	homeDir        string
	client         *http.Client
	onState        func()
	disableWS      bool
	probeHarnesses func(context.Context, *harness.Registry) []NodeHarnessInfo

	mu                sync.Mutex
	enabled           bool
	name              string
	perms             []string
	token             string
	status            string
	transport         string
	errMsg            string
	connected         bool
	workspaces        []NodeWorkspace
	maxPermissionMode string
	harnesses         []NodeHarnessInfo
	harnessRegistry   *harness.Registry
	ownsRegistry      bool
	loopCancel        context.CancelFunc
	loopDone          chan struct{}
	taskBaseCtx       context.Context
	taskBaseCancel    context.CancelFunc
	wakeCh            chan struct{}
	closed            bool

	wsMu          sync.Mutex
	wsActive      *wsConn
	outboundQueue []WSFrame

	tasksMu sync.Mutex
	tasks   map[string]*remoteAgentTask

	promptsMu        sync.Mutex
	pendingApprovals map[string]chan AgentApprovalResponse
	pendingQuestions map[string]chan AgentQuestionResponse

	deskMu sync.Mutex
	desk   *desktop.EnginePool
}

// NewAgent initializes a node Agent and starts connecting if opts.Enabled is true.
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
	taskCtx, taskCancel := context.WithCancel(context.Background())
	a := &Agent{
		nodeID:            id,
		baseURL:           strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/"),
		hostname:          host,
		homeDir:           home,
		client:            client,
		onState:           opts.OnState,
		disableWS:         opts.DisableWS,
		probeHarnesses:    opts.ProbeHarnesses,
		enabled:           opts.Enabled,
		name:              name,
		perms:             perms,
		token:             strings.TrimSpace(opts.Token),
		status:            "disabled",
		workspaces:        NormalizeWorkspaces(opts.Workspaces, home),
		maxPermissionMode: normalizeCeilingMode(opts.MaxPermissionMode),
		harnessRegistry:   opts.HarnessRegistry,
		taskBaseCtx:       taskCtx,
		taskBaseCancel:    taskCancel,
		wakeCh:            make(chan struct{}, 1),
		tasks:             make(map[string]*remoteAgentTask),
		pendingApprovals:  make(map[string]chan AgentApprovalResponse),
		pendingQuestions:  make(map[string]chan AgentQuestionResponse),
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
		Enabled:           a.enabled,
		Connected:         a.connected,
		Status:            a.status,
		Transport:         a.transport,
		Error:             a.errMsg,
		NodeID:            a.nodeID,
		Name:              a.name,
		Permissions:       append([]string(nil), a.perms...),
		Harnesses:         append([]NodeHarnessInfo(nil), a.harnesses...),
		Workspaces:        append([]NodeWorkspace(nil), a.workspaces...),
		MaxPermissionMode: a.maxPermissionMode,
	}
}

// SetWorkspaces updates the authorized workspaces advertised by this node.
func (a *Agent) SetWorkspaces(ws []NodeWorkspace) {
	norm := NormalizeWorkspaces(ws, a.homeDir)
	a.mu.Lock()
	a.workspaces = norm
	connected := a.connected
	a.mu.Unlock()
	if connected {
		a.pushLiveHello()
	}
}

// SetMaxPermissionMode updates the device-side permission mode ceiling.
func (a *Agent) SetMaxPermissionMode(mode string) {
	norm := normalizeCeilingMode(mode)
	a.mu.Lock()
	a.maxPermissionMode = norm
	connected := a.connected
	a.mu.Unlock()
	if connected {
		a.pushLiveHello()
	}
}

func (a *Agent) pushLiveHello() {
	a.wsMu.Lock()
	wsc := a.wsActive
	a.wsMu.Unlock()
	if wsc == nil {
		return
	}
	hello := a.buildHelloPayload(context.Background())
	_ = wsc.writeFrame(context.Background(), FrameHello, hello)
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
		a.cancelAllTasks("node agent disabled")
		a.closeActiveWS("node disabled")
		a.sendDisconnect()
		a.mu.Lock()
		a.status = "disabled"
		a.transport = ""
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
	if a.taskBaseCtx == nil || a.taskBaseCtx.Err() != nil {
		a.taskBaseCtx, a.taskBaseCancel = context.WithCancel(context.Background())
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

// Close stops connections, cancels active remote agent tasks, and sends a
// best-effort disconnect notice to the hub.
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
	taskCancel := a.taskBaseCancel
	a.loopCancel = nil
	a.loopDone = nil
	a.taskBaseCancel = nil
	reg := a.harnessRegistry
	ownsReg := a.ownsRegistry
	a.harnessRegistry = nil
	a.ownsRegistry = false
	a.mu.Unlock()

	a.cancelAllTasks("node agent closed")
	if taskCancel != nil {
		taskCancel()
	}
	a.closeActiveWS("node closing")
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	if wasEnabled {
		a.sendDisconnect()
	}
	if ownsReg && reg != nil {
		reg.Close()
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

func (a *Agent) setTransport(tr string) {
	a.mu.Lock()
	a.transport = tr
	a.mu.Unlock()
}

func (a *Agent) closeActiveWS(reason string) {
	a.wsMu.Lock()
	wsc := a.wsActive
	a.wsActive = nil
	a.wsMu.Unlock()
	if wsc != nil {
		wsc.closeNormal(reason)
	}
}

func (a *Agent) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	if a.disableWS {
		a.runPollLoop(ctx)
		return
	}
	backoff := 2 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		wsUnsupported, statusCode, err := a.runWSOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if wsUnsupported {
			a.runPollLoop(ctx)
			return
		}
		if statusCode == http.StatusUnauthorized {
			a.setState("waiting_auth", false, "Waiting to sign in to primary host")
			select {
			case <-time.After(2 * time.Second):
			case <-a.wakeCh:
			case <-ctx.Done():
				return
			}
			continue
		}
		if err != nil {
			a.setState("error", false, err.Error())
		} else {
			a.setState("connecting", false, "")
			backoff = 2 * time.Second
		}
		select {
		case <-time.After(backoff):
		case <-a.wakeCh:
			backoff = 2 * time.Second
		case <-ctx.Done():
			return
		}
		if err != nil && backoff < 8*time.Second {
			backoff *= 2
		}
	}
}

func (a *Agent) runPollLoop(ctx context.Context) {
	a.setTransport("poll")
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

func (a *Agent) wsEndpointURL() (string, error) {
	u, err := url.Parse(a.baseURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported base URL scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/nodes/ws"
	return u.String(), nil
}

func (a *Agent) runWSOnce(ctx context.Context) (wsUnsupported bool, statusCode int, err error) {
	wsURL, err := a.wsEndpointURL()
	if err != nil {
		return false, 0, err
	}
	_, _, token := a.snapshot()
	hdr := http.Header{}
	if token != "" {
		hdr.Set("Authorization", "Bearer "+token)
	}
	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	rawConn, resp, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPClient: a.client,
		HTTPHeader: hdr,
	})
	dialCancel()
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		if code == http.StatusNotFound || code == http.StatusMethodNotAllowed || code == http.StatusNotImplemented {
			return true, code, err
		}
		return false, code, err
	}
	defer rawConn.CloseNow()

	wsc := newWSConn(rawConn)
	hello := a.buildHelloPayload(ctx)
	if err := wsc.writeFrame(ctx, FrameHello, hello); err != nil {
		return false, 0, err
	}

	ackCtx, ackCancel := context.WithTimeout(ctx, wsHelloTimeout)
	ackFrame, err := wsc.readFrame(ackCtx)
	ackCancel()
	if err != nil {
		return false, 0, err
	}
	if ackFrame.Type != FrameHelloAck {
		return false, 0, fmt.Errorf("expected %s, got %s", FrameHelloAck, ackFrame.Type)
	}
	var ack WSHelloAckPayload
	if err := json.Unmarshal(ackFrame.Payload, &ack); err != nil {
		return false, 0, err
	}
	if !ack.OK {
		if ack.Error == "" {
			ack.Error = "node registration rejected"
		}
		return false, 0, errors.New(ack.Error)
	}

	a.wsMu.Lock()
	a.wsActive = wsc
	queued := append([]WSFrame(nil), a.outboundQueue...)
	a.outboundQueue = nil
	a.wsMu.Unlock()

	defer func() {
		a.wsMu.Lock()
		if a.wsActive == wsc {
			a.wsActive = nil
		}
		a.wsMu.Unlock()
	}()

	a.setTransport("ws")
	a.setState("connected", true, "")

	for _, f := range queued {
		if werr := wsc.writeRawFrame(ctx, f); werr != nil {
			a.enqueueOutbound(f)
			return false, 0, werr
		}
	}

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	go func() {
		ticker := time.NewTicker(wsKeepalivePeriod)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				_ = wsc.writeFrame(connCtx, FramePing, WSPingPayload{Timestamp: time.Now().UnixMilli()})
			}
		}
	}()

	for {
		frame, rerr := wsc.readFrame(connCtx)
		if rerr != nil {
			return false, 0, rerr
		}
		switch frame.Type {
		case FramePing:
			var p WSPingPayload
			_ = json.Unmarshal(frame.Payload, &p)
			_ = wsc.writeFrame(connCtx, FramePong, p)
		case FramePong:
		case FrameRPCCall:
			var cmd Command
			if json.Unmarshal(frame.Payload, &cmd) == nil {
				go a.runWSCommand(connCtx, wsc, cmd)
			}
		case FrameAgentStart:
			var req AgentStartRequest
			if json.Unmarshal(frame.Payload, &req) == nil {
				go a.handleAgentStart(req)
			}
		case FrameAgentSend:
			var req AgentSendRequest
			if json.Unmarshal(frame.Payload, &req) == nil {
				go a.handleAgentSend(req)
			}
		case FrameAgentCancel:
			var req AgentCancelRequest
			if json.Unmarshal(frame.Payload, &req) == nil {
				a.handleAgentCancel(req)
			}
		case FrameAgentApprovalResp:
			var resp AgentApprovalResponse
			if json.Unmarshal(frame.Payload, &resp) == nil {
				a.deliverApprovalResponse(resp)
			}
		case FrameAgentQuestionResp:
			var resp AgentQuestionResponse
			if json.Unmarshal(frame.Payload, &resp) == nil {
				a.deliverQuestionResponse(resp)
			}
		}
	}
}

func (a *Agent) runWSCommand(parentCtx context.Context, wsc *wsConn, cmd Command) {
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
	_ = wsc.writeFrame(parentCtx, FrameRPCResult, res)
}

func (a *Agent) capabilities() []string {
	caps := []string{
		protocol.CapNodeExecEnv,
		protocol.CapNodeBrowserProfile,
		protocol.CapNodeDesktopPermissions,
		protocol.CapNodeDesktopPower,
	}
	if !a.disableWS {
		caps = append(caps, protocol.CapNodeWS)
	}
	if a.hasPerm(PermAgent) {
		caps = append(caps, protocol.CapNodeAgentExec, protocol.CapNodeWorkspaces)
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
