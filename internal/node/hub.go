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

	"bot-hub/internal/protocol"
)

const (
	PermFiles   = "files"
	PermExec    = "exec"
	PermDesktop = "desktop"
)

var validNodeIDRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ValidPerms normalizes and filters permissions into canonical order.
func ValidPerms(in []string) []string {
	has := make(map[string]bool, len(in))
	for _, p := range in {
		has[strings.TrimSpace(p)] = true
	}
	var out []string
	for _, p := range []string{PermFiles, PermExec, PermDesktop} {
		if has[p] {
			out = append(out, p)
		}
	}
	return out
}

// PermLabel returns a concise Chinese label for a permission scope.
func PermLabel(p string) string {
	switch p {
	case PermFiles:
		return "文件读写"
	case PermExec:
		return "执行命令"
	case PermDesktop:
		return "桌面与自动化"
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
	default:
		return "", fmt.Errorf("unsupported node action %q", action)
	}
}

// NodeInfo describes a connected or recently seen remote device node.
type NodeInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
	Hostname        string   `json:"hostname"`
	HomeDir         string   `json:"homeDir,omitempty"`
	Permissions     []string `json:"permissions"`
	ProtocolVersion int      `json:"protocolVersion"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Version         string   `json:"version,omitempty"`
	Connected       bool     `json:"connected"`
	LastSeenAt      string   `json:"lastSeenAt,omitempty"`
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
	ID              string   `json:"id"`
	Name            string   `json:"name,omitempty"`
	OS              string   `json:"os,omitempty"`
	Arch            string   `json:"arch,omitempty"`
	Hostname        string   `json:"hostname,omitempty"`
	HomeDir         string   `json:"homeDir,omitempty"`
	Permissions     []string `json:"permissions,omitempty"`
	ProtocolVersion int      `json:"protocolVersion,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Version         string   `json:"version,omitempty"`
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
	cmdCh        chan Command
	offlineTimer *time.Timer
}

// Registry tracks remote device nodes connected to the hub and routes RPC calls.
type Registry struct {
	mu       sync.Mutex
	nodes    map[string]*nodeEntry
	pending  map[string]chan Result
	onChange func()
}

// NewRegistry creates a Registry. onChange is invoked asynchronously outside
// locks whenever a node connects, disconnects, or updates its permissions.
func NewRegistry(onChange func()) *Registry {
	return &Registry{
		nodes:    make(map[string]*nodeEntry),
		pending:  make(map[string]chan Result),
		onChange: onChange,
	}
}

func (r *Registry) notify() {
	if r.onChange != nil {
		go r.onChange()
	}
}

func isConnectedLocked(e *nodeEntry, now time.Time) bool {
	return e.connected && (e.activePolls > 0 || now.Sub(e.lastSeen) < 15*time.Second)
}

func snapshotInfoLocked(e *nodeEntry, now time.Time) NodeInfo {
	info := e.info
	info.Permissions = append([]string(nil), e.info.Permissions...)
	info.Capabilities = append([]string(nil), e.info.Capabilities...)
	info.ProtocolVersion = protocol.NormalizeProtocol(e.info.ProtocolVersion)
	info.Connected = isConnectedLocked(e, now)
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
		e = &nodeEntry{cmdCh: make(chan Command, 8)}
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
		!slices.Equal(e.info.Capabilities, caps) ||
		!slices.Equal(e.info.Permissions, perms)

	if e.offlineTimer != nil {
		e.offlineTimer.Stop()
		e.offlineTimer = nil
	}
	e.info = NodeInfo{
		ID:              id,
		Name:            name,
		OS:              req.OS,
		Arch:            req.Arch,
		Hostname:        req.Hostname,
		HomeDir:         req.HomeDir,
		Permissions:     perms,
		ProtocolVersion: proto,
		Capabilities:    caps,
		Version:         ver,
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
		if e.activePolls <= 0 && e.connected {
			if e.offlineTimer != nil {
				e.offlineTimer.Stop()
			}
			e.offlineTimer = time.AfterFunc(4*time.Second, func() {
				r.mu.Lock()
				cur := r.nodes[id]
				if cur != e || cur.activePolls > 0 || !cur.connected || time.Since(cur.lastSeen) < 3500*time.Millisecond {
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

// Disconnect marks a node offline immediately and fails any queued commands.
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
	wasConnected := e.connected
	e.connected = false
	var dropped []Command
	for {
		select {
		case cmd := <-e.cmdCh:
			dropped = append(dropped, cmd)
		default:
			goto drained
		}
	}
drained:
	var waiters []chan Result
	for _, cmd := range dropped {
		if ch, ok := r.pending[cmd.CallID]; ok {
			waiters = append(waiters, ch)
			delete(r.pending, cmd.CallID)
		}
	}
	r.mu.Unlock()

	for i, ch := range waiters {
		ch <- Result{CallID: dropped[i].CallID, NodeID: id, OK: false, Error: "远端设备节点已断开"}
	}
	if wasConnected {
		r.notify()
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
			return nil, "", errors.New("当前没有已连接的远端设备节点（请在另一台电脑的 Bot Hub「设置 › 远程与设备」中开启「允许主电脑操控本机」）")
		case 1:
			targetID = connected[0]
		default:
			r.mu.Unlock()
			return nil, "", fmt.Errorf("已连接多台设备 (%s)，请指定 nodeId", strings.Join(connected, ", "))
		}
	}

	e, ok := r.nodes[targetID]
	if !ok || !isConnectedLocked(e, now) {
		r.mu.Unlock()
		return nil, targetID, fmt.Errorf("节点 %q 当前未连接", targetID)
	}
	if !slices.Contains(e.info.Permissions, reqPerm) {
		r.mu.Unlock()
		return nil, targetID, fmt.Errorf("节点 %q 未开启「%s」权限", targetID, PermLabel(reqPerm))
	}

	callID := newCallID()
	resCh := make(chan Result, 1)
	r.pending[callID] = resCh
	cmdCh := e.cmdCh
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.pending, callID)
		r.mu.Unlock()
	}()

	cmd := Command{
		CallID:     callID,
		Action:     action,
		Args:       rawArgs,
		TimeoutSec: int(timeout.Seconds()),
	}

	enqTimer := time.NewTimer(5 * time.Second)
	select {
	case cmdCh <- cmd:
		enqTimer.Stop()
	case <-enqTimer.C:
		return nil, targetID, fmt.Errorf("节点 %q 指令队列已满", targetID)
	case <-ctx.Done():
		enqTimer.Stop()
		return nil, targetID, ctx.Err()
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
		return nil, targetID, fmt.Errorf("节点 %q 执行超时 (%s)", targetID, timeout)
	case <-ctx.Done():
		return nil, targetID, ctx.Err()
	}
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
