package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/state"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type remoteTaskBridge struct {
	mu           sync.Mutex
	taskID       string
	bot          config.BotConfig
	permMode     string
	nodeID       string
	sawNodeStart bool
	queue        []node.AgentEvent
	notifyCh     chan struct{}
}

func newRemoteTaskBridge(taskID, nodeID string, bot config.BotConfig, permMode string) *remoteTaskBridge {
	return &remoteTaskBridge{
		taskID:   taskID,
		nodeID:   nodeID,
		bot:      bot,
		permMode: permMode,
		notifyCh: make(chan struct{}, 1),
	}
}

func (b *remoteTaskBridge) setTurnContext(bot config.BotConfig, permMode string) {
	b.mu.Lock()
	b.bot = bot
	b.permMode = permMode
	b.queue = nil
	b.mu.Unlock()
}

func (b *remoteTaskBridge) pushEvent(evt node.AgentEvent) {
	b.mu.Lock()
	if evt.Type == node.AgentEventStatus || evt.Type == node.AgentEventChunk ||
		evt.Type == node.AgentEventToolUse || evt.Type == node.AgentEventDone {
		b.sawNodeStart = true
	}
	b.queue = append(b.queue, evt)
	b.mu.Unlock()
	select {
	case b.notifyCh <- struct{}{}:
	default:
	}
}

func (b *remoteTaskBridge) popEvents() []node.AgentEvent {
	b.mu.Lock()
	if len(b.queue) == 0 {
		b.mu.Unlock()
		return nil
	}
	out := b.queue
	b.queue = nil
	b.mu.Unlock()
	return out
}

func (b *remoteTaskBridge) hasStartedOnNode() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sawNodeStart
}

func (b *remoteTaskBridge) snapshotBot() (config.BotConfig, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bot, b.permMode
}

func isRemoteAgentTask(kind, deviceID string) bool {
	if kind == state.TaskKindDesktop {
		return false
	}
	id := strings.TrimSpace(deviceID)
	return id != "" && !strings.EqualFold(id, "local")
}

func (h *Hub) validateRemoteAgentNode(deviceID string) (node.NodeInfo, error) {
	id := strings.TrimSpace(deviceID)
	if id == "" || strings.EqualFold(id, "local") {
		return node.NodeInfo{}, nil
	}
	if h.nodes == nil {
		return node.NodeInfo{}, fmt.Errorf("remote device %q is unavailable", id)
	}
	info, ok := h.nodes.Get(id)
	if !ok {
		return node.NodeInfo{}, fmt.Errorf("remote device %q not found", id)
	}
	if !info.Connected || info.Reconnecting {
		return node.NodeInfo{}, fmt.Errorf("remote device %q is offline", id)
	}
	if info.Transport != "" && info.Transport != "ws" {
		return node.NodeInfo{}, fmt.Errorf("remote device %q is not connected over WebSocket", id)
	}
	if !slices.Contains(info.Permissions, node.PermAgent) {
		return node.NodeInfo{}, fmt.Errorf("remote device %q does not have %q permission enabled", id, node.PermLabel(node.PermAgent))
	}
	return info, nil
}

func (h *Hub) getOrCreateRemoteBridge(taskID, nodeID string, bot config.BotConfig, permMode string) (*remoteTaskBridge, bool) {
	h.tasksMu.Lock()
	defer h.tasksMu.Unlock()
	if h.remoteTasks == nil {
		h.remoteTasks = make(map[string]*remoteTaskBridge)
	}
	if b, ok := h.remoteTasks[taskID]; ok && b.nodeID == nodeID {
		b.setTurnContext(bot, permMode)
		return b, true
	}
	b := newRemoteTaskBridge(taskID, nodeID, bot, permMode)
	h.remoteTasks[taskID] = b
	return b, false
}

func (h *Hub) removeRemoteBridge(taskID string) {
	h.tasksMu.Lock()
	delete(h.remoteTasks, taskID)
	h.tasksMu.Unlock()
}

func (h *Hub) buildRemoteContextSummary(bot config.BotConfig) string {
	var parts []string
	if bot.Workspace != "" {
		if raw, err := os.ReadFile(filepath.Join(bot.Workspace, "AGENTS.md")); err == nil {
			if txt := truncateRunesSummary(string(raw), 1500); txt != "" {
				parts = append(parts, "Bot Role (AGENTS.md):\n"+txt)
			}
		}
	}
	if len(parts) == 0 {
		if desc := strings.TrimSpace(bot.Description); desc != "" {
			parts = append(parts, "Bot Role:\n"+desc)
		}
	}
	if userMD := truncateRunesSummary(h.SharedUserMD(), 1000); userMD != "" {
		parts = append(parts, "User Preferences (_shared/USER.md):\n"+userMD)
	}
	return strings.Join(parts, "\n\n")
}

func truncateRunesSummary(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

func normalizeRemoteAgentError(errMsg string) string {
	msg := strings.TrimSpace(errMsg)
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "remote device node disconnected") ||
		strings.Contains(lower, "remote device node offline") ||
		strings.Contains(lower, "remote device offline") {
		return "remote device offline"
	}
	if msg == "" {
		return "remote task failed"
	}
	return msg
}

func (h *Hub) executeRemoteBotTaskTurn(
	ctx context.Context,
	task *state.BotTask,
	bot config.BotConfig,
	permMode string,
	hubURL string,
	extraEnv map[string]string,
	prompt string,
	req TaskSpawnRequest,
	isFollowUp bool,
) (*harness.TurnResult, error) {
	if _, err := h.validateRemoteAgentNode(task.DeviceID); err != nil {
		return nil, err
	}

	bridge, existed := h.getOrCreateRemoteBridge(task.ID, task.DeviceID, bot, permMode)

	turnCtx := ctx
	var turnCancel context.CancelFunc
	if req.TimeoutSec > 0 && (permMode == config.PermissionModeAuto || task.Kind == state.TaskKindRoutine) {
		turnCtx, turnCancel = context.WithTimeout(ctx, time.Duration(req.TimeoutSec)*time.Second)
	} else {
		turnCtx, turnCancel = context.WithCancel(ctx)
	}
	defer turnCancel()

	callbacks := node.AgentTaskCallbacks{
		OnEvent: func(evt node.AgentEvent) {
			bridge.pushEvent(evt)
		},
		OnApproval: func(pCtx context.Context, pReq node.AgentApprovalRequest) (harness.PermissionResponse, error) {
			bCfg, pMode := bridge.snapshotBot()
			return h.RequestApprovalForTask(pCtx, bCfg, task.ID, task.DeviceID, pMode, pReq.Request)
		},
		OnQuestion: func(qCtx context.Context, qReq node.AgentQuestionRequest) (harness.QuestionResponse, error) {
			bCfg, _ := bridge.snapshotBot()
			return h.AskQuestionForTask(qCtx, bCfg, task.ID, task.DeviceID, qReq.Request)
		},
	}

	var dispatchErr error
	if isFollowUp && existed && bridge.hasStartedOnNode() {
		dispatchErr = h.nodes.SendAgentTask(turnCtx, task.DeviceID, node.AgentSendRequest{
			TaskID:         task.ID,
			Prompt:         prompt,
			PermissionMode: permMode,
			TimeoutSec:     bot.TimeoutSec,
		})
	} else {
		dispatchErr = h.nodes.StartAgentTask(turnCtx, task.DeviceID, node.AgentStartRequest{
			TaskID:         task.ID,
			BotID:          bot.ID,
			BotName:        bot.Name,
			Harness:        bot.Harness,
			Model:          bot.Model,
			Agent:          bot.Agent,
			PermissionMode: permMode,
			SessionID:      task.RuntimeSessionID,
			Workdir:        task.Workdir,
			WorkspaceID:    task.WorkspaceID,
			Prompt:         prompt,
			ContextSummary: h.buildRemoteContextSummary(bot),
			MCPHubURL:      hubURL,
			TimeoutSec:     bot.TimeoutSec,
			ExtraEnv:       extraEnv,
		}, callbacks)
	}
	if dispatchErr != nil {
		return nil, dispatchErr
	}

	var (
		chunkBuf   strings.Builder
		toolMu     sync.Mutex
		toolOrder  []string
		toolCounts = make(map[string]int)
	)

	buildPartial := func() *harness.TurnResult {
		toolMu.Lock()
		tools := formatToolCounts(toolOrder, toolCounts)
		toolMu.Unlock()
		return &harness.TurnResult{
			BotID:     task.BotID,
			SessionID: task.RuntimeSessionID,
			Text:      strings.TrimSpace(chunkBuf.String()),
			Tools:     tools,
		}
	}

	for {
		for _, evt := range bridge.popEvents() {
			switch evt.Type {
			case node.AgentEventChunk:
				if evt.Chunk != "" {
					chunkBuf.WriteString(evt.Chunk)
					if h.store != nil {
						_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventChunk, map[string]any{
							"chunk": evt.Chunk,
						})
					}
					h.broadcast(Event{
						Type:  "task_chunk",
						BotID: task.BotID,
						Payload: map[string]any{
							"taskId":   task.ID,
							"botId":    task.BotID,
							"kind":     task.Kind,
							"deviceId": task.DeviceID,
							"chunk":    evt.Chunk,
						},
					})
				}
				if req.OnText != nil {
					req.OnText(evt.Chunk)
				}

			case node.AgentEventToolUse:
				if evt.ToolName != "" {
					toolMu.Lock()
					if toolCounts[evt.ToolName] == 0 {
						toolOrder = append(toolOrder, evt.ToolName)
					}
					toolCounts[evt.ToolName]++
					toolMu.Unlock()
					if h.store != nil {
						_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventToolUse, map[string]any{
							"tool": evt.ToolName,
						})
					}
					h.broadcast(Event{
						Type:  "task_tool_use",
						BotID: task.BotID,
						Payload: map[string]any{
							"taskId":   task.ID,
							"botId":    task.BotID,
							"kind":     task.Kind,
							"deviceId": task.DeviceID,
							"tool":     evt.ToolName,
						},
					})
				}
				if req.OnToolUse != nil {
					req.OnToolUse(evt.ToolName)
				}

			case node.AgentEventStatus:
				st := strings.TrimSpace(evt.Status)
				if st != "" && h.store != nil {
					if cur, ok, err := h.store.GetBotTask(task.ID); err == nil && ok &&
						!state.IsTaskTerminalStatus(cur.Status) &&
						cur.Status != st &&
						cur.Status != state.TaskStatusWaitingApproval &&
						cur.Status != state.TaskStatusWaitingQuestion {
						cur.Status = st
						_ = h.store.UpdateBotTask(cur)
						_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
							"status": st,
						})
						h.broadcast(Event{
							Type:    "task_updated",
							BotID:   task.BotID,
							Payload: cur,
						})
					}
				}

			case node.AgentEventDone:
				toolMu.Lock()
				tools := formatToolCounts(toolOrder, toolCounts)
				toolMu.Unlock()
				text := evt.Text
				if strings.TrimSpace(text) == "" {
					text = strings.TrimSpace(chunkBuf.String())
				}
				if len(evt.Tools) > 0 {
					tools = append([]string(nil), evt.Tools...)
				}
				var usage harness.Usage
				if evt.Usage != nil {
					usage = *evt.Usage
				}
				return &harness.TurnResult{
					BotID:      task.BotID,
					SessionID:  evt.SessionID,
					Text:       text,
					Tools:      tools,
					DurationMs: evt.DurationMs,
					Usage:      usage,
				}, nil

			case node.AgentEventError:
				return buildPartial(), errors.New(normalizeRemoteAgentError(evt.Error))
			}
		}

		select {
		case <-bridge.notifyCh:
		case <-turnCtx.Done():
			if h.nodes != nil {
				cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = h.nodes.CancelAgentTask(cctx, task.DeviceID, task.ID, "task cancelled")
				cancel()
			}
			return buildPartial(), turnCtx.Err()
		}
	}
}

func (h *Hub) handleRemoteNodeOffline(nodeID string, activeTaskIDs []string) {
	seen := make(map[string]bool, len(activeTaskIDs))
	for _, tid := range activeTaskIDs {
		if tid != "" {
			seen[tid] = true
		}
	}

	h.tasksMu.Lock()
	bridges := make([]*remoteTaskBridge, 0, len(seen))
	for tid, b := range h.remoteTasks {
		if b.nodeID == nodeID || seen[tid] {
			seen[tid] = true
			bridges = append(bridges, b)
		}
	}
	h.tasksMu.Unlock()

	for _, b := range bridges {
		b.pushEvent(node.AgentEvent{
			TaskID: b.taskID,
			Type:   node.AgentEventError,
			Error:  "remote device offline",
		})
	}

	for tid := range seen {
		h.CancelApprovalsByTask(tid)
		h.CancelQuestionsByTask(tid)
		h.tasksMu.Lock()
		_, hasRun := h.taskRuns[tid]
		h.tasksMu.Unlock()
		if !hasRun {
			h.settleOrphanedRemoteTask(tid, "remote device offline")
		}
	}

	if h.store != nil {
		for _, status := range []string{
			state.TaskStatusQueued,
			state.TaskStatusRunning,
			state.TaskStatusWaitingApproval,
			state.TaskStatusWaitingQuestion,
		} {
			tasks, err := h.store.ListBotTasks("", state.TaskListFilter{Status: status, Limit: 200})
			if err != nil {
				continue
			}
			for _, t := range tasks {
				if t.DeviceID != nodeID || !isRemoteAgentTask(t.Kind, t.DeviceID) {
					continue
				}
				h.tasksMu.Lock()
				_, hasRun := h.taskRuns[t.ID]
				h.tasksMu.Unlock()
				if hasRun {
					continue
				}
				h.CancelApprovalsByTask(t.ID)
				h.CancelQuestionsByTask(t.ID)
				h.settleOrphanedRemoteTask(t.ID, "remote device offline")
			}
		}
	}
}

func (h *Hub) handleRemoteNodeReconnected(nodeID string, info node.NodeInfo, activeTaskIDs []string) {
	log.Printf("[tasks] remote node %s (%s) reconnected (activeTaskIds=%v)", nodeID, info.Name, activeTaskIDs)
	if h.store == nil {
		return
	}
	activeSet := make(map[string]bool, len(activeTaskIDs))
	for _, tid := range activeTaskIDs {
		if tid != "" {
			activeSet[tid] = true
		}
	}
	for _, status := range []string{
		state.TaskStatusQueued,
		state.TaskStatusRunning,
		state.TaskStatusWaitingApproval,
		state.TaskStatusWaitingQuestion,
	} {
		tasks, err := h.store.ListBotTasks("", state.TaskListFilter{Status: status, Limit: 200})
		if err != nil {
			continue
		}
		for _, t := range tasks {
			if t.DeviceID != nodeID || !isRemoteAgentTask(t.Kind, t.DeviceID) || activeSet[t.ID] {
				continue
			}
			h.tasksMu.Lock()
			_, hasRun := h.taskRuns[t.ID]
			h.tasksMu.Unlock()
			if hasRun {
				continue
			}
			h.settleOrphanedRemoteTask(t.ID, "remote task lost after node reconnect")
		}
	}
}

func (h *Hub) settleOrphanedRemoteTask(taskID, errMsg string) {
	if h.store == nil {
		return
	}
	t, ok, err := h.store.GetBotTask(taskID)
	if err != nil || !ok || state.IsTaskTerminalStatus(t.Status) {
		return
	}
	t.Status = state.TaskStatusFailed
	t.Error = errMsg
	if t.FinishedAt == "" {
		t.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_ = h.store.UpdateBotTask(t)
	_, _ = h.store.AppendBotTaskEvent(t.ID, t.BotID, state.TaskEventError, map[string]any{
		"error": errMsg,
	})
	_, _ = h.store.AppendBotTaskEvent(t.ID, t.BotID, state.TaskEventStatus, map[string]any{
		"status": state.TaskStatusFailed,
		"error":  errMsg,
	})
	h.broadcast(Event{
		Type:    "task_completed",
		BotID:   t.BotID,
		Payload: t,
	})
	h.deliverTaskResult(t, 0)
	h.notifyTaskWaiters(t.ID)
	h.removeRemoteBridge(t.ID)
}
