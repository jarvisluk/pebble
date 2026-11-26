package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"bot-hub/internal/harness"
	"bot-hub/internal/protocol"
)

const (
	wsReadLimitBytes   = 16 << 20
	wsHelloTimeout     = 10 * time.Second
	wsWriteTimeout     = 5 * time.Second
	wsKeepalivePeriod  = 20 * time.Second
	wsPromptWaitLimit  = 15 * time.Minute
)

// WebSocket JSON frame types between remote nodes and the Primary Hub.
const (
	// Node -> Hub
	FrameHello            = "hello"
	FramePing             = "ping"
	FramePong             = "pong"
	FrameRPCResult        = "rpc_result"
	FrameAgentEvent       = "agent_event"
	FrameAgentApprovalReq = "agent_approval_req"
	FrameAgentQuestionReq = "agent_question_req"

	// Hub -> Node
	FrameHelloAck          = "hello_ack"
	FrameRPCCall           = "rpc_call"
	FrameAgentStart        = "agent_start"
	FrameAgentSend         = "agent_send"
	FrameAgentCancel       = "agent_cancel"
	FrameAgentApprovalResp = "agent_approval_resp"
	FrameAgentQuestionResp = "agent_question_resp"
)

// WSFrame is the JSON envelope sent over /api/nodes/ws.
type WSFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WSHelloPayload is sent by a node immediately after upgrading /api/nodes/ws.
type WSHelloPayload struct {
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
	ActiveTaskIDs     []string          `json:"activeTaskIds,omitempty"`
}

// WSHelloAckPayload is returned by the Hub in response to hello.
type WSHelloAckPayload struct {
	OK              bool     `json:"ok"`
	NodeID          string   `json:"nodeId,omitempty"`
	ProtocolVersion int      `json:"protocolVersion"`
	Capabilities    []string `json:"capabilities,omitempty"`
	GracePeriodSec  int      `json:"gracePeriodSec,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// WSPingPayload is used for ping and pong frames.
type WSPingPayload struct {
	Timestamp int64 `json:"ts,omitempty"`
}

// AgentContextFile carries an optional context file provided by the Hub for a remote task.
type AgentContextFile struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

// AgentStartRequest starts a remote agent turn on a node.
type AgentStartRequest struct {
	TaskID         string             `json:"taskId"`
	BotID          string             `json:"botId"`
	BotName        string             `json:"botName,omitempty"`
	Harness        string             `json:"harness,omitempty"`
	Model          string             `json:"model,omitempty"`
	Agent          string             `json:"agent,omitempty"`
	PermissionMode string             `json:"permissionMode,omitempty"`
	SessionID      string             `json:"sessionId,omitempty"`
	Workdir        string             `json:"workdir,omitempty"`
	WorkspaceID    string             `json:"workspaceId,omitempty"`
	Prompt         string             `json:"prompt"`
	ContextSummary string             `json:"contextSummary,omitempty"`
	ContextFiles   []AgentContextFile `json:"contextFiles,omitempty"`
	MCPHubURL      string             `json:"mcpHubUrl,omitempty"`
	TunnelProxy    string             `json:"tunnelProxy,omitempty"`
	TimeoutSec     int                `json:"timeoutSec,omitempty"`
	ExtraEnv       map[string]string  `json:"extraEnv,omitempty"`
}

// AgentSendRequest delivers a follow-up prompt to an existing remote task.
type AgentSendRequest struct {
	TaskID         string `json:"taskId"`
	Prompt         string `json:"prompt"`
	PermissionMode string `json:"permissionMode,omitempty"`
	TimeoutSec     int    `json:"timeoutSec,omitempty"`
}

// AgentCancelRequest requests cancellation of an active remote task.
type AgentCancelRequest struct {
	TaskID string `json:"taskId"`
	Reason string `json:"reason,omitempty"`
}

const (
	AgentEventChunk   = "chunk"
	AgentEventToolUse = "tool_use"
	AgentEventStatus  = "status"
	AgentEventDone    = "done"
	AgentEventError   = "error"
)

// AgentEvent streams progress and terminal outcomes for a remote task back to the Hub.
type AgentEvent struct {
	TaskID     string         `json:"taskId"`
	Type       string         `json:"type"` // "chunk" | "tool_use" | "status" | "done" | "error"
	Chunk      string         `json:"chunk,omitempty"`
	ToolName   string         `json:"toolName,omitempty"`
	Status     string         `json:"status,omitempty"`
	Text       string         `json:"text,omitempty"`
	SessionID  string         `json:"sessionId,omitempty"`
	Tools      []string       `json:"tools,omitempty"`
	DurationMs int64          `json:"durationMs,omitempty"`
	Usage      *harness.Usage `json:"usage,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// AgentApprovalRequest bridges a harness OnPermission prompt from a node to the Hub.
type AgentApprovalRequest struct {
	RequestID string                    `json:"requestId"`
	TaskID    string                    `json:"taskId"`
	BotID     string                    `json:"botId,omitempty"`
	Request   harness.PermissionRequest `json:"request"`
}

// AgentApprovalResponse carries the Hub's decision back to the node.
type AgentApprovalResponse struct {
	RequestID string                     `json:"requestId"`
	TaskID    string                     `json:"taskId"`
	Response  harness.PermissionResponse `json:"response"`
	Error     string                     `json:"error,omitempty"`
}

// AgentQuestionRequest bridges a harness OnQuestion prompt from a node to the Hub.
type AgentQuestionRequest struct {
	RequestID string                  `json:"requestId"`
	TaskID    string                  `json:"taskId"`
	BotID     string                  `json:"botId,omitempty"`
	Request   harness.QuestionRequest `json:"request"`
}

// AgentQuestionResponse carries the Hub's answers back to the node.
type AgentQuestionResponse struct {
	RequestID string                   `json:"requestId"`
	TaskID    string                   `json:"taskId"`
	Response  harness.QuestionResponse `json:"response"`
	Error     string                   `json:"error,omitempty"`
}

type wsConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func newWSConn(c *websocket.Conn) *wsConn {
	c.SetReadLimit(wsReadLimitBytes)
	return &wsConn{conn: c}
}

func encodeWSFrame(frameType string, payload any) (WSFrame, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return WSFrame{}, err
		}
		raw = b
	}
	return WSFrame{Type: frameType, Payload: raw}, nil
}

func (w *wsConn) writeRawFrame(ctx context.Context, frame WSFrame) error {
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return w.conn.Write(wctx, websocket.MessageText, b)
}

func (w *wsConn) writeFrame(ctx context.Context, frameType string, payload any) error {
	frame, err := encodeWSFrame(frameType, payload)
	if err != nil {
		return err
	}
	return w.writeRawFrame(ctx, frame)
}

func (w *wsConn) readFrame(ctx context.Context) (WSFrame, error) {
	_, data, err := w.conn.Read(ctx)
	if err != nil {
		return WSFrame{}, err
	}
	var frame WSFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return WSFrame{}, fmt.Errorf("invalid ws frame: %w", err)
	}
	return frame, nil
}

func (w *wsConn) closeNormal(reason string) {
	_ = w.conn.Close(websocket.StatusNormalClosure, reason)
}

// ServeWS runs the Hub-side lifecycle for an authenticated node WebSocket connection.
func (r *Registry) ServeWS(ctx context.Context, rawConn *websocket.Conn) {
	wsc := newWSConn(rawConn)
	defer rawConn.CloseNow()

	helloCtx, helloCancel := context.WithTimeout(ctx, wsHelloTimeout)
	firstFrame, err := wsc.readFrame(helloCtx)
	helloCancel()
	if err != nil {
		return
	}
	if firstFrame.Type != FrameHello {
		_ = wsc.writeFrame(ctx, FrameHelloAck, WSHelloAckPayload{
			OK:              false,
			ProtocolVersion: protocol.CurrentProtocol,
			Error:           "expected hello frame",
		})
		_ = rawConn.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}

	var hello WSHelloPayload
	if err := json.Unmarshal(firstFrame.Payload, &hello); err != nil {
		_ = wsc.writeFrame(ctx, FrameHelloAck, WSHelloAckPayload{
			OK:              false,
			ProtocolVersion: protocol.CurrentProtocol,
			Error:           "invalid hello payload",
		})
		_ = rawConn.Close(websocket.StatusPolicyViolation, "invalid hello")
		return
	}

	nodeID, prevWS, info, orphanedTasks, hooks, changed, err := r.registerWSNode(hello, wsc)
	if err != nil {
		_ = wsc.writeFrame(ctx, FrameHelloAck, WSHelloAckPayload{
			OK:              false,
			ProtocolVersion: protocol.CurrentProtocol,
			Error:           err.Error(),
		})
		_ = rawConn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	if prevWS != nil && prevWS != wsc {
		prevWS.closeNormal("replaced by new connection")
	}

	r.mu.Lock()
	graceSec := int(r.disconnectGracePeriod.Seconds())
	r.mu.Unlock()

	if err := wsc.writeFrame(ctx, FrameHelloAck, WSHelloAckPayload{
		OK:              true,
		NodeID:          nodeID,
		ProtocolVersion: protocol.CurrentProtocol,
		Capabilities:    protocol.NormalizeCapabilities([]string{protocol.CapNodeWS, protocol.CapNodeAgentExec, protocol.CapNodeWorkspaces}),
		GracePeriodSec:  graceSec,
	}); err != nil {
		r.handleWSDisconnect(nodeID, wsc)
		return
	}

	if changed {
		r.notify()
	}
	for _, task := range orphanedTasks {
		if task.cb.OnEvent != nil {
			task.cb.OnEvent(AgentEvent{
				TaskID: task.taskID,
				Type:   AgentEventError,
				Error:  "remote task lost after node reconnect",
			})
		}
	}
	if hooks.OnNodeReconnected != nil {
		go hooks.OnNodeReconnected(nodeID, info, append([]string(nil), hello.ActiveTaskIDs...))
	}

	defer r.handleWSDisconnect(nodeID, wsc)

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
		frame, err := wsc.readFrame(connCtx)
		if err != nil {
			return
		}
		r.touchWSNode(nodeID, wsc)
		switch frame.Type {
		case FrameHello:
			var upd WSHelloPayload
			if json.Unmarshal(frame.Payload, &upd) == nil && strings.TrimSpace(upd.ID) == nodeID {
				_, _, _, _, _, chg, uerr := r.registerWSNode(upd, wsc)
				if uerr == nil && chg {
					r.notify()
				}
			}
		case FramePing:
			var p WSPingPayload
			_ = json.Unmarshal(frame.Payload, &p)
			_ = wsc.writeFrame(connCtx, FramePong, p)
		case FramePong:
			// lastSeen updated by touchWSNode
		case FrameRPCResult:
			var res Result
			if json.Unmarshal(frame.Payload, &res) == nil {
				if res.NodeID == "" {
					res.NodeID = nodeID
				}
				r.Deliver(res)
			}
		case FrameAgentEvent:
			var evt AgentEvent
			if json.Unmarshal(frame.Payload, &evt) == nil {
				r.handleAgentEvent(nodeID, evt)
			}
		case FrameAgentApprovalReq:
			var req AgentApprovalRequest
			if json.Unmarshal(frame.Payload, &req) == nil {
				go r.handleAgentApprovalReq(nodeID, req)
			}
		case FrameAgentQuestionReq:
			var req AgentQuestionRequest
			if json.Unmarshal(frame.Payload, &req) == nil {
				go r.handleAgentQuestionReq(nodeID, req)
			}
		}
	}
}

func (r *Registry) registerWSNode(hello WSHelloPayload, wsc *wsConn) (string, *wsConn, NodeInfo, []*agentTaskEntry, NodeLifecycleHooks, bool, error) {
	id := strings.TrimSpace(hello.ID)
	if !validNodeIDRe.MatchString(id) {
		return "", nil, NodeInfo{}, nil, NodeLifecycleHooks{}, false, fmt.Errorf("invalid node id %q", hello.ID)
	}
	name := strings.TrimSpace(hello.Name)
	if name == "" {
		name = id
	}
	perms := ValidPerms(hello.Permissions)
	proto := protocol.NormalizeProtocol(hello.ProtocolVersion)
	caps := protocol.NormalizeCapabilities(append(append([]string(nil), hello.Capabilities...), protocol.CapNodeWS))
	ver := strings.TrimSpace(hello.Version)
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.nodes[id]
	if !ok {
		e = &nodeEntry{cmdCh: make(chan Command, 8), activeTasks: make(map[string]struct{})}
		r.nodes[id] = e
	}
	if e.activeTasks == nil {
		e.activeTasks = make(map[string]struct{})
	}
	wasConnected := isConnectedLocked(e, now)
	wasInGrace := e.inGrace
	prevWS := e.ws

	if e.offlineTimer != nil {
		e.offlineTimer.Stop()
		e.offlineTimer = nil
	}
	if e.graceTimer != nil {
		e.graceTimer.Stop()
		e.graceTimer = nil
	}
	e.inGrace = false
	e.ws = wsc
	e.transport = "ws"

	changed := !ok || !wasConnected || wasInGrace ||
		e.info.Name != name ||
		e.info.OS != hello.OS ||
		e.info.Arch != hello.Arch ||
		e.info.Hostname != hello.Hostname ||
		e.info.HomeDir != hello.HomeDir ||
		e.info.ProtocolVersion != proto ||
		e.info.Version != ver ||
		e.info.MaxPermissionMode != strings.TrimSpace(hello.MaxPermissionMode) ||
		!slices.Equal(e.info.Capabilities, caps) ||
		!slices.Equal(e.info.Permissions, perms) ||
		len(e.info.Harnesses) != len(hello.Harnesses) ||
		len(e.info.Workspaces) != len(hello.Workspaces)

	e.info = NodeInfo{
		ID:                id,
		Name:              name,
		OS:                hello.OS,
		Arch:              hello.Arch,
		Hostname:          hello.Hostname,
		HomeDir:           hello.HomeDir,
		Permissions:       perms,
		ProtocolVersion:   proto,
		Capabilities:      caps,
		Version:           ver,
		Harnesses:         append([]NodeHarnessInfo(nil), hello.Harnesses...),
		Workspaces:        append([]NodeWorkspace(nil), hello.Workspaces...),
		MaxPermissionMode: strings.TrimSpace(hello.MaxPermissionMode),
		ActiveTaskIDs:     append([]string(nil), hello.ActiveTaskIDs...),
	}
	e.connected = true
	e.lastSeen = now

	var orphaned []*agentTaskEntry
	if len(e.activeTasks) > 0 {
		reported := make(map[string]bool, len(hello.ActiveTaskIDs))
		for _, tid := range hello.ActiveTaskIDs {
			reported[tid] = true
		}
		for tid := range e.activeTasks {
			if !reported[tid] {
				delete(e.activeTasks, tid)
				if task, exists := r.agentTasks[tid]; exists && task.running {
					task.running = false
					if task.cancel != nil {
						task.cancel()
					}
					orphaned = append(orphaned, task)
				}
			}
		}
	}

	return id, prevWS, snapshotInfoLocked(e, now), orphaned, r.hooks, changed, nil
}

func (r *Registry) touchWSNode(nodeID string, wsc *wsConn) {
	r.mu.Lock()
	if e, ok := r.nodes[nodeID]; ok && e.ws == wsc {
		e.lastSeen = time.Now()
	}
	r.mu.Unlock()
}

func (r *Registry) handleWSDisconnect(nodeID string, wsc *wsConn) {
	r.mu.Lock()
	e, ok := r.nodes[nodeID]
	if !ok || e.ws != wsc {
		r.mu.Unlock()
		return
	}
	e.ws = nil

	type pendingFail struct {
		callID string
		ch     chan Result
	}
	var waiters []pendingFail
	for callID, nid := range r.pendingNode {
		if nid != nodeID {
			continue
		}
		if ch, ok := r.pending[callID]; ok {
			waiters = append(waiters, pendingFail{callID: callID, ch: ch})
			delete(r.pending, callID)
		}
		delete(r.pendingNode, callID)
	}

	grace := r.disconnectGracePeriod
	hasActiveTasks := len(e.activeTasks) > 0
	var lostTasks []*agentTaskEntry
	var offlineHook func(string, []string)
	var notifyChange bool

	if hasActiveTasks && grace > 0 {
		e.inGrace = true
		e.connected = true
		if e.graceTimer != nil {
			e.graceTimer.Stop()
		}
		e.graceTimer = time.AfterFunc(grace, func() {
			r.expireGrace(nodeID, e)
		})
		notifyChange = true
	} else {
		e.inGrace = false
		if e.activePolls == 0 {
			notifyChange = e.connected
			e.connected = false
		}
		lostTasks, offlineHook = r.failNodeTasksLocked(e)
	}
	r.mu.Unlock()

	for _, w := range waiters {
		select {
		case w.ch <- Result{CallID: w.callID, NodeID: nodeID, OK: false, Error: "remote device node disconnected"}:
		default:
		}
	}
	r.notifyLostTasks(nodeID, lostTasks, offlineHook, "remote device node disconnected")
	if notifyChange {
		r.notify()
	}
}

func (r *Registry) expireGrace(nodeID string, targetEntry *nodeEntry) {
	r.mu.Lock()
	e, ok := r.nodes[nodeID]
	if !ok || e != targetEntry || !e.inGrace || e.ws != nil {
		r.mu.Unlock()
		return
	}
	e.inGrace = false
	e.graceTimer = nil
	e.connected = false
	lostTasks, offlineHook := r.failNodeTasksLocked(e)
	r.mu.Unlock()

	r.notifyLostTasks(nodeID, lostTasks, offlineHook, "remote device node offline (disconnect grace period expired)")
	r.notify()
}

func (r *Registry) sendWSFrameToNode(ctx context.Context, nodeID, frameType string, payload any) error {
	r.mu.Lock()
	e, ok := r.nodes[nodeID]
	var wsc *wsConn
	if ok {
		wsc = e.ws
	}
	r.mu.Unlock()
	if wsc == nil {
		return fmt.Errorf("node %q is not connected via WebSocket", nodeID)
	}
	return wsc.writeFrame(ctx, frameType, payload)
}

// StartAgentTask dispatches an agent_start command to nodeID (or the sole
// connected node when nodeID is empty) over WebSocket.
func (r *Registry) StartAgentTask(ctx context.Context, nodeID string, req AgentStartRequest, cb AgentTaskCallbacks) error {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return errors.New("taskId is required")
	}
	req.TaskID = taskID

	targetID, e, err := r.resolveConnectedNodeLocked(nodeID, PermAgent)
	if err != nil {
		return err
	}
	if e.ws == nil {
		r.mu.Unlock()
		if e.inGrace {
			return fmt.Errorf("node %q is temporarily disconnected", targetID)
		}
		return fmt.Errorf("node %q does not have an active WebSocket connection", targetID)
	}
	if e.activeTasks == nil {
		e.activeTasks = make(map[string]struct{})
	}
	taskCtx, taskCancel := context.WithCancel(context.Background())
	entry := &agentTaskEntry{
		nodeID:  targetID,
		taskID:  taskID,
		cb:      cb,
		running: true,
		ctx:     taskCtx,
		cancel:  taskCancel,
	}
	r.agentTasks[taskID] = entry
	e.activeTasks[taskID] = struct{}{}
	wsc := e.ws
	r.mu.Unlock()

	if err := wsc.writeFrame(ctx, FrameAgentStart, req); err != nil {
		r.mu.Lock()
		if cur, ok := r.agentTasks[taskID]; ok && cur == entry {
			cur.running = false
			delete(r.agentTasks, taskID)
		}
		delete(e.activeTasks, taskID)
		r.mu.Unlock()
		taskCancel()
		return err
	}
	return nil
}

// SendAgentTask sends a follow-up prompt to an existing remote agent task.
func (r *Registry) SendAgentTask(ctx context.Context, nodeID string, req AgentSendRequest) error {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return errors.New("taskId is required")
	}
	req.TaskID = taskID

	r.mu.Lock()
	task := r.agentTasks[taskID]
	targetID := strings.TrimSpace(nodeID)
	if targetID == "" && task != nil {
		targetID = task.nodeID
	}
	r.mu.Unlock()

	resolvedID, e, err := r.resolveConnectedNodeLocked(targetID, PermAgent)
	if err != nil {
		return err
	}
	if e.ws == nil {
		r.mu.Unlock()
		return fmt.Errorf("node %q does not have an active WebSocket connection", resolvedID)
	}
	if e.activeTasks == nil {
		e.activeTasks = make(map[string]struct{})
	}
	if task != nil {
		if task.cancel != nil {
			task.cancel()
		}
		taskCtx, taskCancel := context.WithCancel(context.Background())
		task.ctx = taskCtx
		task.cancel = taskCancel
		task.running = true
		e.activeTasks[taskID] = struct{}{}
	}
	wsc := e.ws
	r.mu.Unlock()

	return wsc.writeFrame(ctx, FrameAgentSend, req)
}

// CancelAgentTask cancels an active remote agent task on nodeID.
func (r *Registry) CancelAgentTask(ctx context.Context, nodeID, taskID, reason string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("taskId is required")
	}

	r.mu.Lock()
	task := r.agentTasks[taskID]
	targetID := strings.TrimSpace(nodeID)
	if targetID == "" && task != nil {
		targetID = task.nodeID
	}
	e := r.nodes[targetID]
	var wsc *wsConn
	if e != nil {
		wsc = e.ws
	}
	if wsc == nil {
		// Node already offline/in-grace: clean up locally so callers unblock.
		var notifyErr bool
		if e != nil {
			delete(e.activeTasks, taskID)
		}
		if task != nil && task.running {
			task.running = false
			if task.cancel != nil {
				task.cancel()
			}
			notifyErr = true
		}
		r.mu.Unlock()
		if notifyErr && task.cb.OnEvent != nil {
			msg := strings.TrimSpace(reason)
			if msg == "" {
				msg = "task cancelled"
			}
			task.cb.OnEvent(AgentEvent{TaskID: taskID, Type: AgentEventError, Error: msg})
		}
		return nil
	}
	r.mu.Unlock()

	return wsc.writeFrame(ctx, FrameAgentCancel, AgentCancelRequest{
		TaskID: taskID,
		Reason: reason,
	})
}

func (r *Registry) handleAgentEvent(nodeID string, evt AgentEvent) {
	r.mu.Lock()
	task := r.agentTasks[evt.TaskID]
	isTerminal := evt.Type == AgentEventDone || evt.Type == AgentEventError
	if isTerminal {
		if e, ok := r.nodes[nodeID]; ok {
			delete(e.activeTasks, evt.TaskID)
		}
		if task != nil {
			task.running = false
			if task.cancel != nil {
				task.cancel()
			}
		}
	}
	var onEvent func(AgentEvent)
	if task != nil {
		onEvent = task.cb.OnEvent
	}
	r.mu.Unlock()

	if onEvent != nil {
		onEvent(evt)
	}
}

func (r *Registry) handleAgentApprovalReq(nodeID string, req AgentApprovalRequest) {
	r.mu.Lock()
	task := r.agentTasks[req.TaskID]
	var handler func(context.Context, AgentApprovalRequest) (harness.PermissionResponse, error)
	var taskCtx context.Context
	if task != nil {
		handler = task.cb.OnApproval
		taskCtx = task.ctx
	}
	r.mu.Unlock()

	respMsg := AgentApprovalResponse{
		RequestID: req.RequestID,
		TaskID:    req.TaskID,
	}
	if handler == nil {
		respMsg.Response = harness.PermissionResponse{Decision: "reject", Reason: "approval handler unavailable"}
	} else {
		if taskCtx == nil {
			taskCtx = context.Background()
		}
		pctx, cancel := context.WithTimeout(taskCtx, wsPromptWaitLimit)
		resp, err := handler(pctx, req)
		cancel()
		if err != nil {
			respMsg.Error = err.Error()
			respMsg.Response = harness.PermissionResponse{Decision: "cancelled", Reason: err.Error()}
		} else {
			respMsg.Response = resp
		}
	}
	_ = r.sendWSFrameToNode(context.Background(), nodeID, FrameAgentApprovalResp, respMsg)
}

func (r *Registry) handleAgentQuestionReq(nodeID string, req AgentQuestionRequest) {
	r.mu.Lock()
	task := r.agentTasks[req.TaskID]
	var handler func(context.Context, AgentQuestionRequest) (harness.QuestionResponse, error)
	var taskCtx context.Context
	if task != nil {
		handler = task.cb.OnQuestion
		taskCtx = task.ctx
	}
	r.mu.Unlock()

	respMsg := AgentQuestionResponse{
		RequestID: req.RequestID,
		TaskID:    req.TaskID,
	}
	if handler == nil {
		respMsg.Response = harness.QuestionResponse{Cancelled: true, Message: "question handler unavailable"}
	} else {
		if taskCtx == nil {
			taskCtx = context.Background()
		}
		qctx, cancel := context.WithTimeout(taskCtx, wsPromptWaitLimit)
		resp, err := handler(qctx, req)
		cancel()
		if err != nil {
			respMsg.Error = err.Error()
			respMsg.Response = harness.QuestionResponse{Cancelled: true, Message: err.Error()}
		} else {
			respMsg.Response = resp
		}
	}
	_ = r.sendWSFrameToNode(context.Background(), nodeID, FrameAgentQuestionResp, respMsg)
}
