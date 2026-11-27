// Package node implements remote device nodes that connect to the hub over
// the authenticated Tailcat HTTP channel so bots can run files, shell, and
// desktop actions inside a secondary machine's GUI session.
package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/protocol"
)

const (
	PermFiles   = "files"
	PermExec    = "exec"
	PermDesktop = "desktop"
	PermAgent   = "agent"

	defaultDisconnectGracePeriod = 30 * time.Second
)

var validNodeIDRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ValidPerms normalizes and filters permissions into canonical order.
func ValidPerms(in []string) []string {
	has := make(map[string]bool, len(in))
	for _, p := range in {
		has[strings.TrimSpace(p)] = true
	}
	var out []string
	for _, p := range []string{PermFiles, PermExec, PermDesktop, PermAgent} {
		if has[p] {
			out = append(out, p)
		}
	}
	return out
}

// PermLabel returns a concise label for a permission scope.
func PermLabel(p string) string {
	switch p {
	case PermFiles:
		return "File Access"
	case PermExec:
		return "Run Commands"
	case PermDesktop:
		return "Desktop & Automation"
	case PermAgent:
		return "Run Agents"
	default:
		return p
	}
}

// RequiredPerm maps a node command action to its required permission scope.
func RequiredPerm(action string) (string, error) {
	switch action {
	case "file_read", "file_write", "file_list":
		return PermFiles, nil
	case "exec":
		return PermExec, nil
	case "screenshot", "osascript", "open",
		"desktop_status", "desktop_permissions", "desktop_permission_request",
		"desktop_windows", "desktop_focus",
		"desktop_frame", "desktop_input", "desktop_stream_offer",
		"desktop_stream_close", "desktop_stream_set_input",
		"desktop_setup", "desktop_power":
		return PermDesktop, nil
	case "agent_start", "agent_send", "agent_cancel":
		return PermAgent, nil
	default:
		return "", fmt.Errorf("unsupported node action %q", action)
	}
}

func permissionRank(mode string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case config.PermissionModePlan:
		return 0, true
	case config.PermissionModeAsk:
		return 1, true
	case config.PermissionModeEdit:
		return 2, true
	case config.PermissionModeAuto:
		return 3, true
	default:
		return 0, false
	}
}

// IntersectPermissionMode returns the stricter (less permissive) of requested
// and ceiling, enforcing downward-only permission mode intersection.
func IntersectPermissionMode(requested, ceiling string) string {
	req := strings.ToLower(strings.TrimSpace(requested))
	if req == "" {
		req = config.PermissionModeAuto
	}
	ceil := strings.ToLower(strings.TrimSpace(ceiling))
	if ceil == "" {
		ceil = config.PermissionModeAuto
	}
	rRank, rOK := permissionRank(req)
	if !rOK {
		req = config.PermissionModeAsk
		rRank = 1
	}
	cRank, cOK := permissionRank(ceil)
	if !cOK {
		ceil = config.PermissionModePlan
		cRank = 0
	}
	if cRank < rRank {
		return ceil
	}
	return req
}

// NodeHarnessInfo describes an agent harness installed on a remote node.
type NodeHarnessInfo struct {
	Kind                   string                         `json:"kind"`
	Label                  string                         `json:"label,omitempty"`
	Installed              bool                           `json:"installed"`
	AuthStatus             string                         `json:"authStatus,omitempty"` // "ready" | "needs_login" | "unknown"
	AuthDetail             string                         `json:"authDetail,omitempty"`
	PermissionCapabilities harness.PermissionCapabilities `json:"permissionCapabilities,omitempty"`
}

// NodeWorkspace describes a user-authorized directory or git repository on a remote node.
type NodeWorkspace struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	IsGit         bool   `json:"isGit,omitempty"`
	DefaultBranch string `json:"defaultBranch,omitempty"`
}

// NodeInfo describes a connected or recently seen remote device node.
type NodeInfo struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	OS                string            `json:"os"`
	Arch              string            `json:"arch"`
	Hostname          string            `json:"hostname"`
	HomeDir           string            `json:"homeDir,omitempty"`
	Permissions       []string          `json:"permissions"`
	ProtocolVersion   int               `json:"protocolVersion"`
	Capabilities      []string          `json:"capabilities,omitempty"`
	Version           string            `json:"version,omitempty"`
	Connected         bool              `json:"connected"`
	Reconnecting      bool              `json:"reconnecting,omitempty"`
	Transport         string            `json:"transport,omitempty"` // "ws" | "poll"
	LastSeenAt        string            `json:"lastSeenAt,omitempty"`
	Harnesses         []NodeHarnessInfo `json:"harnesses,omitempty"`
	Workspaces        []NodeWorkspace   `json:"workspaces,omitempty"`
	MaxPermissionMode string            `json:"maxPermissionMode,omitempty"`
	ActiveTaskIDs     []string          `json:"activeTaskIds,omitempty"`
}

// HasCapability reports whether the node advertised capability capName.
func (n NodeInfo) HasCapability(capName string) bool {
	return protocol.HasCapability(n.Capabilities, capName)
}

// IsOutdated reports whether the node's protocol generation trails CurrentProtocol.
func (n NodeInfo) IsOutdated() bool {
	return protocol.NormalizeProtocol(n.ProtocolVersion) < protocol.CurrentProtocol
}

// PollRequest is sent by a remote node when long-polling for commands.
type PollRequest struct {
	ID                string            `json:"id"`
	Name              string            `json:"name,omitempty"`
	OS                string            `json:"os,omitempty"`
	Arch              string            `json:"arch,omitempty"`
	Hostname          string            `json:"hostname,omitempty"`
	HomeDir           string            `json:"homeDir,omitempty"`
	Permissions       []string          `json:"permissions,omitempty"`
	ProtocolVersion   int               `json:"protocolVersion,omitempty"`
	Capabilities      []string          `json:"capabilities,omitempty"`
	Version           string            `json:"version,omitempty"`
	Harnesses         []NodeHarnessInfo `json:"harnesses,omitempty"`
	Workspaces        []NodeWorkspace   `json:"workspaces,omitempty"`
	MaxPermissionMode string            `json:"maxPermissionMode,omitempty"`
}

// Command is a single RPC invocation dispatched from the hub to a node.
type Command struct {
	CallID     string          `json:"callId"`
	Action     string          `json:"action"`
	Args       json.RawMessage `json:"args,omitempty"`
	TimeoutSec int             `json:"timeoutSec,omitempty"`
}

// Result is returned by a remote node for a Command.
type Result struct {
	CallID     string `json:"callId"`
	NodeID     string `json:"nodeId"`
	OK         bool   `json:"ok"`
	Output     string `json:"output,omitempty"`
	DataBase64 string `json:"dataBase64,omitempty"`
	MimeType   string `json:"mimeType,omitempty"`
	ExitCode   int    `json:"exitCode,omitempty"`
	Error      string `json:"error,omitempty"`
	SavedPath  string `json:"savedPath,omitempty"`
}

type nodeEntry struct {
	info         NodeInfo
	lastSeen     time.Time
	activePolls  int
	connected    bool
	inGrace      bool
	transport    string // "ws" | "poll"
	cmdCh        chan Command
	ws           *wsConn
	offlineTimer *time.Timer
	graceTimer   *time.Timer
	activeTasks  map[string]struct{}
}

// NodeLifecycleHooks receives asynchronous notifications when a WS node
// connects/reconnects or becomes permanently offline after its grace period.
type NodeLifecycleHooks struct {
	OnNodeReconnected func(nodeID string, info NodeInfo, activeTaskIDs []string)
	OnNodeOffline     func(nodeID string, activeTaskIDs []string)
}

// AgentTaskCallbacks receives streaming events and interactive prompts from a remote agent task.
type AgentTaskCallbacks struct {
	OnEvent    func(evt AgentEvent)
	OnApproval func(ctx context.Context, req AgentApprovalRequest) (harness.PermissionResponse, error)
	OnQuestion func(ctx context.Context, req AgentQuestionRequest) (harness.QuestionResponse, error)
}

// NodeAgentDispatcher is the Hub-side interface for dispatching and controlling
// remote agent tasks on connected device nodes.
type NodeAgentDispatcher interface {
	StartAgentTask(ctx context.Context, nodeID string, req AgentStartRequest, cb AgentTaskCallbacks) error
	SendAgentTask(ctx context.Context, nodeID string, req AgentSendRequest) error
	CancelAgentTask(ctx context.Context, nodeID, taskID, reason string) error
}

type agentTaskEntry struct {
	nodeID  string
	taskID  string
	cb      AgentTaskCallbacks
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

var _ NodeAgentDispatcher = (*Registry)(nil)

// Registry tracks remote device nodes connected to the hub and routes RPC calls
// and remote agent tasks.
type Registry struct {
	mu                    sync.Mutex
	nodes                 map[string]*nodeEntry
	pending               map[string]chan Result
	pendingNode           map[string]string
	agentTasks            map[string]*agentTaskEntry
	disconnectGracePeriod time.Duration
	hooks                 NodeLifecycleHooks
	onChange              func()
}

// NewRegistry creates a Registry. onChange is invoked asynchronously outside
// locks whenever a node connects, disconnects, or updates its permissions.
func NewRegistry(onChange func()) *Registry {
	return &Registry{
		nodes:                 make(map[string]*nodeEntry),
		pending:               make(map[string]chan Result),
		pendingNode:           make(map[string]string),
		agentTasks:            make(map[string]*agentTaskEntry),
		disconnectGracePeriod: defaultDisconnectGracePeriod,
		onChange:              onChange,
	}
}

// SetDisconnectGracePeriod configures how long a WS-disconnected node with
// active remote agent tasks remains in grace before being declared offline.
func (r *Registry) SetDisconnectGracePeriod(d time.Duration) {
	if d < 0 {
		d = 0
	}
	r.mu.Lock()
	r.disconnectGracePeriod = d
	r.mu.Unlock()
}

// SetLifecycleHooks installs callbacks for WS node reconnect and offline reconciliation.
func (r *Registry) SetLifecycleHooks(hooks NodeLifecycleHooks) {
	r.mu.Lock()
	r.hooks = hooks
	r.mu.Unlock()
}

func (r *Registry) notify() {
	if r.onChange != nil {
		go r.onChange()
	}
}

func isConnectedLocked(e *nodeEntry, now time.Time) bool {
	if !e.connected {
		return false
	}
	if e.ws != nil || e.inGrace {
		return true
	}
	return e.activePolls > 0 || now.Sub(e.lastSeen) < 15*time.Second
}

func snapshotInfoLocked(e *nodeEntry, now time.Time) NodeInfo {
	info := e.info
	info.Permissions = append([]string(nil), e.info.Permissions...)
	info.Capabilities = append([]string(nil), e.info.Capabilities...)
	info.Harnesses = append([]NodeHarnessInfo(nil), e.info.Harnesses...)
	info.Workspaces = append([]NodeWorkspace(nil), e.info.Workspaces...)
	if len(e.activeTasks) > 0 {
		ids := make([]string, 0, len(e.activeTasks))
		for tid := range e.activeTasks {
			ids = append(ids, tid)
		}
		sort.Strings(ids)
		info.ActiveTaskIDs = ids
	} else {
		info.ActiveTaskIDs = append([]string(nil), e.info.ActiveTaskIDs...)
	}
	info.ProtocolVersion = protocol.NormalizeProtocol(e.info.ProtocolVersion)
	info.Connected = isConnectedLocked(e, now)
	info.Reconnecting = e.inGrace
	info.Transport = e.transport
	if !e.lastSeen.IsZero() {
		info.LastSeenAt = e.lastSeen.UTC().Format(time.RFC3339)
	}
	return info
}

// List returns all known remote nodes sorted by ID.
func (r *Registry) List() []NodeInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	out := make([]NodeInfo, 0, len(r.nodes))
	for _, e := range r.nodes {
		out = append(out, snapshotInfoLocked(e, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns a snapshot of nodeID if registered.
func (r *Registry) Get(nodeID string) (NodeInfo, bool) {
	id := strings.TrimSpace(nodeID)
	if id == "" {
		return NodeInfo{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.nodes[id]
	if !ok {
		return NodeInfo{}, false
	}
	return snapshotInfoLocked(e, time.Now()), true
}

// Poll registers/refreshes a node and waits up to wait for a pending command.
// Returning (nil, nil) indicates the long-poll timed out cleanly with no work.
func (r *Registry) Poll(ctx context.Context, req PollRequest, wait time.Duration) (*Command, error) {
	id := strings.TrimSpace(req.ID)
	if !validNodeIDRe.MatchString(id) {
		return nil, fmt.Errorf("invalid node id %q", req.ID)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = id
	}
	perms := ValidPerms(req.Permissions)
	proto := protocol.NormalizeProtocol(req.ProtocolVersion)
	caps := protocol.NormalizeCapabilities(req.Capabilities)
	ver := strings.TrimSpace(req.Version)
	now := time.Now()

	r.mu.Lock()
	e, ok := r.nodes[id]
	if !ok {
		e = &nodeEntry{cmdCh: make(chan Command, 8), activeTasks: make(map[string]struct{})}
		r.nodes[id] = e
	}
	wasConnected := isConnectedLocked(e, now)
	changed := !ok || !wasConnected ||
		e.info.Name != name ||
		e.info.OS != req.OS ||
		e.info.Arch != req.Arch ||
		e.info.Hostname != req.Hostname ||
		e.info.HomeDir != req.HomeDir ||
		e.info.ProtocolVersion != proto ||
		e.info.Version != ver ||
		e.info.MaxPermissionMode != req.MaxPermissionMode ||
		!slices.Equal(e.info.Capabilities, caps) ||
		!slices.Equal(e.info.Permissions, perms)

	if e.offlineTimer != nil {
		e.offlineTimer.Stop()
		e.offlineTimer = nil
	}
	if e.graceTimer != nil {
		e.graceTimer.Stop()
		e.graceTimer = nil
	}
	e.inGrace = false
	e.transport = "poll"
	e.info = NodeInfo{
		ID:                id,
		Name:              name,
		OS:                req.OS,
		Arch:              req.Arch,
		Hostname:          req.Hostname,
		HomeDir:           req.HomeDir,
		Permissions:       perms,
		ProtocolVersion:   proto,
		Capabilities:      caps,
		Version:           ver,
		Harnesses:         append([]NodeHarnessInfo(nil), req.Harnesses...),
		Workspaces:        append([]NodeWorkspace(nil), req.Workspaces...),
		MaxPermissionMode: strings.TrimSpace(req.MaxPermissionMode),
	}
	e.connected = true
	e.lastSeen = now
	e.activePolls++
	cmdCh := e.cmdCh
	r.mu.Unlock()

	if changed {
		r.notify()
	}

	defer func() {
		r.mu.Lock()
		e.activePolls--
		e.lastSeen = time.Now()
		if e.activePolls <= 0 && e.connected && e.ws == nil && !e.inGrace {
			if e.offlineTimer != nil {
				e.offlineTimer.Stop()
			}
			e.offlineTimer = time.AfterFunc(4*time.Second, func() {
				r.mu.Lock()
				cur := r.nodes[id]
				if cur != e || cur.activePolls > 0 || cur.ws != nil || cur.inGrace || !cur.connected || time.Since(cur.lastSeen) < 3500*time.Millisecond {
					r.mu.Unlock()
					return
				}
				cur.connected = false
				cur.offlineTimer = nil
				r.mu.Unlock()
				r.notify()
			})
		}
		r.mu.Unlock()
	}()

	if wait <= 0 {
		wait = 25 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case cmd := <-cmdCh:
		return &cmd, nil
	case <-timer.C:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Disconnect marks a node offline immediately and fails any queued commands and active agent tasks.
func (r *Registry) Disconnect(nodeID string) {
	id := strings.TrimSpace(nodeID)
	if id == "" {
		return
	}
	r.mu.Lock()
	e, ok := r.nodes[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	if e.offlineTimer != nil {
		e.offlineTimer.Stop()
		e.offlineTimer = nil
	}
	if e.graceTimer != nil {
		e.graceTimer.Stop()
		e.graceTimer = nil
	}
	wasConnected := e.connected
	e.connected = false
	e.inGrace = false
	ws := e.ws
	e.ws = nil

	type pendingFail struct {
		callID string
		ch     chan Result
	}
	var waiters []pendingFail
	for {
		select {
		case cmd := <-e.cmdCh:
			if ch, ok := r.pending[cmd.CallID]; ok {
				waiters = append(waiters, pendingFail{callID: cmd.CallID, ch: ch})
				delete(r.pending, cmd.CallID)
				delete(r.pendingNode, cmd.CallID)
			}
		default:
			goto drained
		}
	}
drained:
	for callID, nid := range r.pendingNode {
		if nid != id {
			continue
		}
		if ch, ok := r.pending[callID]; ok {
			waiters = append(waiters, pendingFail{callID: callID, ch: ch})
			delete(r.pending, callID)
		}
		delete(r.pendingNode, callID)
	}

	lostTasks, offlineHook := r.failNodeTasksLocked(e)
	r.mu.Unlock()

	if ws != nil {
		ws.closeNormal("node disconnected")
	}
	for _, w := range waiters {
		select {
		case w.ch <- Result{CallID: w.callID, NodeID: id, OK: false, Error: "remote device node disconnected"}:
		default:
		}
	}
	r.notifyLostTasks(id, lostTasks, offlineHook, "remote device node disconnected")
	if wasConnected {
		r.notify()
	}
}

func (r *Registry) failNodeTasksLocked(e *nodeEntry) ([]*agentTaskEntry, func(string, []string)) {
	if len(e.activeTasks) == 0 {
		return nil, nil
	}
	lost := make([]*agentTaskEntry, 0, len(e.activeTasks))
	for tid := range e.activeTasks {
		if task, ok := r.agentTasks[tid]; ok && task.running {
			task.running = false
			if task.cancel != nil {
				task.cancel()
			}
			lost = append(lost, task)
		}
		delete(e.activeTasks, tid)
	}
	return lost, r.hooks.OnNodeOffline
}

func (r *Registry) notifyLostTasks(nodeID string, lost []*agentTaskEntry, offlineHook func(string, []string), reason string) {
	if len(lost) == 0 {
		return
	}
	lostIDs := make([]string, 0, len(lost))
	for _, task := range lost {
		lostIDs = append(lostIDs, task.taskID)
		if task.cb.OnEvent != nil {
			task.cb.OnEvent(AgentEvent{
				TaskID: task.taskID,
				Type:   AgentEventError,
				Error:  reason,
			})
		}
	}
	sort.Strings(lostIDs)
	if offlineHook != nil {
		go offlineHook(nodeID, lostIDs)
	}
}

// Call dispatches an action to nodeID (or auto-selects the sole connected node
// when nodeID is empty) and waits for its result.
func (r *Registry) Call(ctx context.Context, nodeID, action string, args any, timeout time.Duration) (*Result, string, error) {
	reqPerm, err := RequiredPerm(action)
	if err != nil {
		return nil, "", err
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}

	var rawArgs json.RawMessage
	if args != nil {
		switch v := args.(type) {
		case json.RawMessage:
			rawArgs = v
		case []byte:
			rawArgs = v
		default:
			b, err := json.Marshal(args)
			if err != nil {
				return nil, "", err
			}
			rawArgs = b
		}
	}

	targetID, e, err := r.resolveConnectedNodeLocked(nodeID, reqPerm)
	if err != nil {
		return nil, targetID, err
	}

	callID := newCallID()
	resCh := make(chan Result, 1)
	r.pending[callID] = resCh
	r.pendingNode[callID] = targetID
	cmdCh := e.cmdCh
	ws := e.ws
	inGrace := e.inGrace
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.pending, callID)
		delete(r.pendingNode, callID)
		r.mu.Unlock()
	}()

	cmd := Command{
		CallID:     callID,
		Action:     action,
		Args:       rawArgs,
		TimeoutSec: int(timeout.Seconds()),
	}

	if ws != nil {
		if werr := ws.writeFrame(ctx, FrameRPCCall, cmd); werr != nil {
			return nil, targetID, fmt.Errorf("failed to send rpc_call to node %q: %w", targetID, werr)
		}
	} else if inGrace {
		return nil, targetID, fmt.Errorf("node %q is temporarily disconnected", targetID)
	} else {
		enqTimer := time.NewTimer(5 * time.Second)
		select {
		case cmdCh <- cmd:
			enqTimer.Stop()
		case <-enqTimer.C:
			return nil, targetID, fmt.Errorf("node %q command queue is full", targetID)
		case <-ctx.Done():
			enqTimer.Stop()
			return nil, targetID, ctx.Err()
		}
	}

	waitTimer := time.NewTimer(timeout)
	defer waitTimer.Stop()

	select {
	case res := <-resCh:
		if res.NodeID == "" {
			res.NodeID = targetID
		}
		return &res, targetID, nil
	case <-waitTimer.C:
		return nil, targetID, fmt.Errorf("node %q timed out (%s)", targetID, timeout)
	case <-ctx.Done():
		return nil, targetID, ctx.Err()
	}
}

// resolveConnectedNodeLocked acquires r.mu and returns the resolved targetID and nodeEntry
// with r.mu STILL HELD on success (and unlocked on error).
func (r *Registry) resolveConnectedNodeLocked(nodeID, reqPerm string) (string, *nodeEntry, error) {
	targetID := strings.TrimSpace(nodeID)
	r.mu.Lock()
	now := time.Now()
	if targetID == "" {
		var connected []string
		for id, e := range r.nodes {
			if isConnectedLocked(e, now) {
				connected = append(connected, id)
			}
		}
		sort.Strings(connected)
		switch len(connected) {
		case 0:
			r.mu.Unlock()
			return "", nil, errors.New("no connected remote device nodes (enable \"Allow Primary Host to Control This Device\" in Pebble Settings -> Connection & Devices on the other computer)")
		case 1:
			targetID = connected[0]
		default:
			r.mu.Unlock()
			return "", nil, fmt.Errorf("multiple devices connected (%s); specify nodeId", strings.Join(connected, ", "))
		}
	}

	e, ok := r.nodes[targetID]
	if !ok || !isConnectedLocked(e, now) {
		r.mu.Unlock()
		return targetID, nil, fmt.Errorf("node %q is not connected", targetID)
	}
	if reqPerm != "" && !slices.Contains(e.info.Permissions, reqPerm) {
		r.mu.Unlock()
		return targetID, nil, fmt.Errorf("node %q does not have %q permission enabled", targetID, PermLabel(reqPerm))
	}
	return targetID, e, nil
}

// Deliver supplies a command Result from a node back to its waiting Call.
func (r *Registry) Deliver(res Result) bool {
	if res.CallID == "" {
		return false
	}
	r.mu.Lock()
	ch, ok := r.pending[res.CallID]
	if ok {
		delete(r.pending, res.CallID)
		delete(r.pendingNode, res.CallID)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- res:
		return true
	default:
		return false
	}
}

func newCallID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
