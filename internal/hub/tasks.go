package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TaskSpawnRequest configures a new durable BotTask execution under a Bot.
type TaskSpawnRequest struct {
	BotID           string            `json:"botId"`
	Kind            string            `json:"kind,omitempty"`           // "worker" (default) | "routine" | "desktop"
	Title           string            `json:"title,omitempty"`
	Prompt          string            `json:"prompt"`
	DeviceID        string            `json:"deviceId,omitempty"`       // default "local"
	WorkspaceID     string            `json:"workspaceId,omitempty"`
	Isolation       string            `json:"isolation,omitempty"`      // "" | "current" | "worktree" | "temp_dir"
	Harness         string            `json:"harness,omitempty"`
	Model           string            `json:"model,omitempty"`
	PermissionMode  string            `json:"permissionMode,omitempty"`
	Workdir         string            `json:"workdir,omitempty"`
	Delivery        string            `json:"delivery,omitempty"`       // "wake" | "receipt" (default) | "silent"
	DependsOn       []string          `json:"dependsOn,omitempty"`
	ParentTaskID    string            `json:"parentTaskId,omitempty"`
	RoutineID       string            `json:"routineId,omitempty"`
	RoutineName     string            `json:"routineName,omitempty"`
	Trigger         string            `json:"trigger,omitempty"`
	SlotKey         string            `json:"slotKey,omitempty"`
	MCPRole         string            `json:"mcpRole,omitempty"`
	TimeoutSec      int               `json:"timeoutSec,omitempty"`
	ExtraEnv        map[string]string `json:"-"`
	Hop             int               `json:"hop,omitempty"`
	ManagedDelivery bool              `json:"-"`
	OnFinished      func(state.BotTask) `json:"-"`
	OnToolUse       func(toolName string) `json:"-"`
	OnText          func(chunk string)    `json:"-"`
}

type activeTaskRun struct {
	taskID    string
	botID     string
	kind      string
	hop       int
	cancel    context.CancelFunc
	done      chan struct{}
	mu        sync.Mutex
	cancelled bool
	finishing bool
	followUps []string
	req       TaskSpawnRequest
}

func (r *activeTaskRun) isCancelled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled
}

func (h *Hub) notifyTaskWaiters(taskID string) {
	h.tasksMu.Lock()
	waiters := h.taskWaiters[taskID]
	delete(h.taskWaiters, taskID)
	h.tasksMu.Unlock()
	for _, ch := range waiters {
		close(ch)
	}
}

// SpawnTask creates a durable BotTask, persists it, and schedules concurrent execution
// without blocking the Bot's serial main botLane.
func (h *Hub) SpawnTask(ctx context.Context, req TaskSpawnRequest) (*state.BotTask, error) {
	if h.store == nil {
		return nil, errors.New("state store unavailable")
	}
	botID := strings.TrimSpace(req.BotID)
	if botID == "" {
		return nil, fmt.Errorf("botId is required: %w", ErrBotNotFound)
	}
	bot, ok := h.GetBot(botID)
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}

	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = state.TaskKindWorker
	}
	switch kind {
	case state.TaskKindWorker, state.TaskKindRoutine, state.TaskKindDesktop:
	default:
		return nil, fmt.Errorf("invalid task kind %q (expected worker, routine, or desktop)", req.Kind)
	}

	delivery := strings.ToLower(strings.TrimSpace(req.Delivery))
	if delivery == "" {
		delivery = state.TaskDeliveryReceipt
	}
	switch delivery {
	case state.TaskDeliveryWake, state.TaskDeliveryReceipt, state.TaskDeliverySilent:
	default:
		return nil, fmt.Errorf("invalid delivery %q (expected wake, receipt, or silent)", req.Delivery)
	}

	isolation, err := normalizeTaskIsolation(req.Isolation)
	if err != nil {
		return nil, err
	}

	permMode := strings.TrimSpace(req.PermissionMode)
	if permMode != "" {
		norm, err := config.NormalizePermissionMode(permMode)
		if err != nil {
			return nil, err
		}
		permMode = norm
	} else {
		permMode = bot.EffectivePermissionMode()
	}

	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" || strings.EqualFold(deviceID, "local") {
		deviceID = "local"
	}
	remote := isRemoteAgentTask(kind, deviceID)
	if remote {
		if _, err := h.validateRemoteAgentNode(deviceID); err != nil {
			return nil, err
		}
	}

	targetHarness := strings.TrimSpace(req.Harness)
	if targetHarness == "" {
		targetHarness = bot.Harness
	}
	if !remote {
		if _, err := h.registry.Get(targetHarness); err != nil {
			return nil, err
		}
	}
	targetModel := strings.TrimSpace(req.Model)
	if targetModel == "" {
		if targetHarness == bot.Harness {
			targetModel = bot.Model
		} else if bot.ModelInherited {
			h.lanesMu.RLock()
			targetModel = h.cfg.DefaultModelFor(targetHarness)
			h.lanesMu.RUnlock()
		}
	}

	workdir := strings.TrimSpace(req.Workdir)
	if !remote {
		if workdir == "" {
			workdir = bot.Workspace
		} else if !filepath.IsAbs(workdir) && bot.Workspace != "" {
			workdir = filepath.Join(bot.Workspace, workdir)
		}
	}

	var deps []string
	seenDeps := make(map[string]bool, len(req.DependsOn))
	for _, rawDep := range req.DependsOn {
		depID := strings.TrimSpace(rawDep)
		if depID == "" || seenDeps[depID] {
			continue
		}
		if _, found, err := h.store.GetBotTask(depID); err != nil {
			return nil, err
		} else if !found {
			return nil, fmt.Errorf("dependency task %q not found: %w", depID, state.ErrTaskNotFound)
		}
		seenDeps[depID] = true
		deps = append(deps, depID)
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		if strings.TrimSpace(req.RoutineName) != "" {
			title = strings.TrimSpace(req.RoutineName)
		} else {
			title = firstLineSummary(prompt, 80)
		}
	}

	hop := req.Hop
	h.hopsMu.Lock()
	if activeHop, ok := h.hops[botID]; ok && activeHop > hop {
		hop = activeHop
	}
	h.hopsMu.Unlock()
	if parentID := strings.TrimSpace(req.ParentTaskID); parentID != "" {
		h.tasksMu.Lock()
		if pr, ok := h.taskRuns[parentID]; ok && pr.hop > hop {
			hop = pr.hop
		}
		h.tasksMu.Unlock()
	}

	task, err := h.store.CreateBotTask(state.BotTask{
		BotID:          botID,
		Kind:           kind,
		Title:          title,
		Prompt:         prompt,
		Status:         state.TaskStatusQueued,
		DeviceID:       deviceID,
		Harness:        targetHarness,
		Model:          targetModel,
		PermissionMode: permMode,
		Workdir:        workdir,
		WorkspaceID:    strings.TrimSpace(req.WorkspaceID),
		Isolation:      isolation,
		Delivery:       delivery,
		DependsOn:      deps,
		ParentTaskID:   strings.TrimSpace(req.ParentTaskID),
		RoutineID:      strings.TrimSpace(req.RoutineID),
		RoutineName:    strings.TrimSpace(req.RoutineName),
		Trigger:        strings.TrimSpace(req.Trigger),
		SlotKey:        strings.TrimSpace(req.SlotKey),
	})
	if err != nil {
		return nil, err
	}

	if err := h.prepareTaskIsolation(ctx, &task); err != nil {
		task.Status = state.TaskStatusFailed
		task.Error = err.Error()
		task.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		_ = h.store.UpdateBotTask(task)
		return nil, err
	}
	if task.WorktreePath != "" || task.WorktreeBranch != "" {
		_ = h.store.UpdateBotTask(task)
	}

	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
		"status": state.TaskStatusQueued,
	})

	run := h.registerTaskRun(task, hop, req)
	h.broadcast(Event{
		Type:    "task_created",
		BotID:   task.BotID,
		Payload: task,
	})

	h.taskWg.Add(1)
	go h.runBotTask(run, task, prompt, req, false)
	return &task, nil
}

func (h *Hub) registerTaskRun(task state.BotTask, hop int, req TaskSpawnRequest) *activeTaskRun {
	baseCtx := h.taskBaseCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	_, cancel := context.WithCancel(baseCtx)
	run := &activeTaskRun{
		taskID: task.ID,
		botID:  task.BotID,
		kind:   task.Kind,
		hop:    hop,
		cancel: cancel,
		done:   make(chan struct{}),
		req:    req,
	}
	h.tasksMu.Lock()
	if h.taskRuns == nil {
		h.taskRuns = make(map[string]*activeTaskRun)
	}
	h.taskRuns[task.ID] = run
	h.tasksMu.Unlock()
	return run
}

// SendToTask delivers a follow-up message to an existing BotTask while preserving
// its runtimeSessionId (queuing onto an active task or re-activating a finished one).
func (h *Hub) SendToTask(_ context.Context, taskID, message string) (*state.BotTask, error) {
	if h.store == nil {
		return nil, errors.New("state store unavailable")
	}
	taskID = strings.TrimSpace(taskID)
	message = strings.TrimSpace(message)
	if taskID == "" {
		return nil, state.ErrTaskNotFound
	}
	if message == "" {
		return nil, errors.New("message is required")
	}

	task, ok, err := h.store.GetBotTask(taskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("task %q not found: %w", taskID, state.ErrTaskNotFound)
	}
	if task.Status == state.TaskStatusCancelled {
		return nil, errors.New("cannot send message to cancelled task")
	}
	if isRemoteAgentTask(task.Kind, task.DeviceID) {
		if _, err := h.validateRemoteAgentNode(task.DeviceID); err != nil {
			return nil, err
		}
	}

	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventMessage, map[string]any{
		"role": "user",
		"text": message,
	})

	var waitDone chan struct{}
	h.tasksMu.Lock()
	if run, active := h.taskRuns[taskID]; active {
		run.mu.Lock()
		if run.cancelled {
			run.mu.Unlock()
			h.tasksMu.Unlock()
			return nil, errors.New("cannot send message to cancelled task")
		}
		if !run.finishing {
			run.followUps = append(run.followUps, message)
			run.mu.Unlock()
			h.tasksMu.Unlock()
			return &task, nil
		}
		waitDone = run.done
		run.mu.Unlock()
	}
	h.tasksMu.Unlock()

	if waitDone != nil {
		<-waitDone
		// Re-read task after previous run finished so we preserve latest RuntimeSessionID/usage.
		latest, found, err := h.store.GetBotTask(taskID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, state.ErrTaskNotFound
		}
		if latest.Status == state.TaskStatusCancelled {
			return nil, errors.New("cannot send message to cancelled task")
		}
		task = latest
	}

	task.Status = state.TaskStatusQueued
	task.Error = ""
	task.FinishedAt = ""
	if err := h.store.UpdateBotTask(task); err != nil {
		return nil, err
	}
	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
		"status": state.TaskStatusQueued,
	})
	h.broadcast(Event{
		Type:    "task_updated",
		BotID:   task.BotID,
		Payload: task,
	})

	hop := 0
	h.hopsMu.Lock()
	if activeHop, ok := h.hops[task.BotID]; ok {
		hop = activeHop
	}
	h.hopsMu.Unlock()

	run := h.registerTaskRun(task, hop, TaskSpawnRequest{
		BotID:          task.BotID,
		Kind:           task.Kind,
		Title:          task.Title,
		Prompt:         message,
		DeviceID:       task.DeviceID,
		WorkspaceID:    task.WorkspaceID,
		Isolation:      task.Isolation,
		Harness:        task.Harness,
		Model:          task.Model,
		PermissionMode: task.PermissionMode,
		Workdir:        task.Workdir,
		Delivery:       task.Delivery,
	})
	h.taskWg.Add(1)
	go h.runBotTask(run, task, message, run.req, true)
	return &task, nil
}

// CancelTask cancels an active or queued BotTask and any pending approvals/questions/takeovers bound to it.
func (h *Hub) CancelTask(taskID string) (*state.BotTask, error) {
	if h.store == nil {
		return nil, errors.New("state store unavailable")
	}
	taskID = strings.TrimSpace(taskID)
	task, ok, err := h.store.GetBotTask(taskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("task %q not found: %w", taskID, state.ErrTaskNotFound)
	}
	if state.IsTaskTerminalStatus(task.Status) {
		return &task, nil
	}

	var cancelFn context.CancelFunc
	h.tasksMu.Lock()
	if run, exists := h.taskRuns[taskID]; exists {
		run.mu.Lock()
		run.cancelled = true
		cancelFn = run.cancel
		run.mu.Unlock()
	}
	h.tasksMu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}
	if isRemoteAgentTask(task.Kind, task.DeviceID) && h.nodes != nil {
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = h.nodes.CancelAgentTask(cctx, task.DeviceID, taskID, "task cancelled")
		ccancel()
		h.removeRemoteBridge(taskID)
	}
	h.CancelApprovalsByTask(taskID)
	h.CancelQuestionsByTask(taskID)
	if h.desktop != nil {
		h.desktop.CancelTakeoversByTask(taskID)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = state.TaskStatusCancelled
	if task.FinishedAt == "" {
		task.FinishedAt = now
	}
	_ = h.store.UpdateBotTask(task)
	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
		"status": state.TaskStatusCancelled,
	})
	h.broadcast(Event{
		Type:    "task_updated",
		BotID:   task.BotID,
		Payload: task,
	})
	h.broadcast(Event{
		Type:    "task_completed",
		BotID:   task.BotID,
		Payload: task,
	})
	h.notifyTaskWaiters(task.ID)
	return &task, nil
}

// GetTask returns a single persisted BotTask by ID.
func (h *Hub) GetTask(taskID string) (*state.BotTask, error) {
	if h.store == nil {
		return nil, errors.New("state store unavailable")
	}
	t, ok, err := h.store.GetBotTask(taskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("task %q not found: %w", taskID, state.ErrTaskNotFound)
	}
	return &t, nil
}

// ListTasks returns BotTasks filtered by optional botID, kind, and status.
func (h *Hub) ListTasks(botID string, filter state.TaskListFilter) ([]state.BotTask, error) {
	if strings.TrimSpace(botID) != "" {
		if _, ok := h.GetBot(botID); !ok {
			return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
		}
	}
	if h.store == nil {
		return []state.BotTask{}, nil
	}
	return h.store.ListBotTasks(botID, filter)
}

// ListTaskEvents returns sequenced events for taskID with seq > afterSeq.
func (h *Hub) ListTaskEvents(taskID string, afterSeq int64, limit int) ([]state.BotTaskEvent, error) {
	if h.store == nil {
		return []state.BotTaskEvent{}, nil
	}
	if _, ok, err := h.store.GetBotTask(taskID); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("task %q not found: %w", taskID, state.ErrTaskNotFound)
	}
	return h.store.ListBotTaskEvents(taskID, afterSeq, limit)
}

// WaitTask blocks until taskID reaches a terminal status or ctx is cancelled.
func (h *Hub) WaitTask(ctx context.Context, taskID string) (*state.BotTask, error) {
	if h.store == nil {
		return nil, errors.New("state store unavailable")
	}
	for {
		ch := make(chan struct{})
		h.tasksMu.Lock()
		if h.taskWaiters == nil {
			h.taskWaiters = make(map[string][]chan struct{})
		}
		h.taskWaiters[taskID] = append(h.taskWaiters[taskID], ch)
		h.tasksMu.Unlock()

		t, ok, err := h.store.GetBotTask(taskID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("task %q not found: %w", taskID, state.ErrTaskNotFound)
		}
		if state.IsTaskTerminalStatus(t.Status) {
			return &t, nil
		}

		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (h *Hub) cancelTasksByBot(botID string) int {
	h.tasksMu.Lock()
	var ids []string
	for id, run := range h.taskRuns {
		if run.botID == botID {
			ids = append(ids, id)
		}
	}
	h.tasksMu.Unlock()
	for _, id := range ids {
		_, _ = h.CancelTask(id)
	}
	return len(ids)
}

func (h *Hub) setTaskWaitingState(taskID, botID, waitingStatus, eventType string, payload any) {
	if h.store == nil || taskID == "" {
		return
	}
	t, ok, err := h.store.GetBotTask(taskID)
	if err != nil || !ok || state.IsTaskTerminalStatus(t.Status) {
		return
	}
	t.Status = waitingStatus
	_ = h.store.UpdateBotTask(t)
	if eventType != "" {
		_, _ = h.store.AppendBotTaskEvent(taskID, botID, eventType, payload)
	}
	_, _ = h.store.AppendBotTaskEvent(taskID, botID, state.TaskEventStatus, map[string]any{
		"status": waitingStatus,
	})
	h.broadcast(Event{
		Type:    "task_updated",
		BotID:   botID,
		Payload: t,
	})
}

func (h *Hub) restoreTaskRunningState(taskID, botID, fromWaitingStatus string) {
	if h.store == nil || taskID == "" {
		return
	}
	t, ok, err := h.store.GetBotTask(taskID)
	if err != nil || !ok || t.Status != fromWaitingStatus {
		return
	}
	t.Status = state.TaskStatusRunning
	_ = h.store.UpdateBotTask(t)
	_, _ = h.store.AppendBotTaskEvent(taskID, botID, state.TaskEventStatus, map[string]any{
		"status": state.TaskStatusRunning,
	})
	h.broadcast(Event{
		Type:    "task_updated",
		BotID:   botID,
		Payload: t,
	})
}

// NotifyTaskTakeoverStarted transitions a desktop BotTask into waiting_takeover and logs an event.
func (h *Hub) NotifyTaskTakeoverStarted(taskID, target string) {
	if h.store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	if t, ok, err := h.store.GetBotTask(taskID); err == nil && ok {
		h.setTaskWaitingState(taskID, t.BotID, state.TaskStatusWaitingTakeover, state.TaskEventTakeover, map[string]any{
			"target": target,
			"state":  "requested",
		})
	}
}

// NotifyTaskTakeoverEnded restores a desktop BotTask from waiting_takeover back to running.
func (h *Hub) NotifyTaskTakeoverEnded(taskID string) {
	if h.store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	if t, ok, err := h.store.GetBotTask(taskID); err == nil && ok {
		h.restoreTaskRunningState(taskID, t.BotID, state.TaskStatusWaitingTakeover)
	}
}

func (h *Hub) waitForTaskDependencies(ctx context.Context, task state.BotTask) error {
	for _, depID := range task.DependsOn {
		for {
			ch := make(chan struct{})
			h.tasksMu.Lock()
			if h.taskWaiters == nil {
				h.taskWaiters = make(map[string][]chan struct{})
			}
			h.taskWaiters[depID] = append(h.taskWaiters[depID], ch)
			h.tasksMu.Unlock()

			dep, ok, err := h.store.GetBotTask(depID)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("dependency task %s not found", depID)
			}
			switch dep.Status {
			case state.TaskStatusCompleted:
				goto nextDep
			case state.TaskStatusFailed, state.TaskStatusCancelled:
				return fmt.Errorf("dependency task %s %s", depID, dep.Status)
			}

			select {
			case <-ch:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	nextDep:
	}
	return nil
}

func (h *Hub) runBotTask(run *activeTaskRun, task state.BotTask, initialPrompt string, req TaskSpawnRequest, isFollowUp bool) {
	defer func() {
		h.tasksMu.Lock()
		if cur, ok := h.taskRuns[task.ID]; ok && cur == run {
			delete(h.taskRuns, task.ID)
		}
		h.tasksMu.Unlock()
		close(run.done)
		h.notifyTaskWaiters(task.ID)
		h.taskWg.Done()
	}()

	baseCtx := h.taskBaseCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	taskCtx, cancel := context.WithCancel(baseCtx)
	run.mu.Lock()
	if run.cancelled {
		run.mu.Unlock()
		cancel()
		return
	}
	oldCancel := run.cancel
	run.cancel = cancel
	run.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	defer cancel()

	if !isFollowUp && len(task.DependsOn) > 0 {
		if err := h.waitForTaskDependencies(taskCtx, task); err != nil {
			if run.isCancelled() || errors.Is(err, context.Canceled) {
				if latest, ok, _ := h.store.GetBotTask(task.ID); ok && latest.Status == state.TaskStatusCancelled {
					if req.OnFinished != nil {
						req.OnFinished(latest)
					}
					return
				}
				h.finalizeTaskCancelled(&task, req)
				return
			}
			h.finalizeTaskFailed(&task, err.Error(), req, run.hop)
			return
		}
	}

	prompt := initialPrompt
	turnFollowUp := isFollowUp
	for {
		if run.isCancelled() || taskCtx.Err() != nil {
			h.finalizeTaskCancelled(&task, req)
			return
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		task.Status = state.TaskStatusRunning
		if task.StartedAt == "" {
			task.StartedAt = now
		}
		_ = h.store.UpdateBotTask(task)
		_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
			"status": state.TaskStatusRunning,
		})
		h.broadcast(Event{
			Type:    "task_updated",
			BotID:   task.BotID,
			Payload: task,
		})

		started := time.Now()
		res, err := h.executeBotTaskTurn(taskCtx, &task, prompt, req, run.hop, turnFollowUp)
		elapsedMs := time.Since(started).Milliseconds()

		if res != nil {
			if res.SessionID != "" {
				task.RuntimeSessionID = res.SessionID
			}
			if len(res.Tools) > 0 {
				task.Tools = append(task.Tools, res.Tools...)
			}
			dur := res.DurationMs
			if dur <= 0 {
				dur = elapsedMs
			}
			task.DurationMs += dur
			task.CostUSD = roundCost(task.CostUSD + res.Usage.CostUSD)
			task.InputTokens += res.Usage.InputTokens
			task.OutputTokens += res.Usage.OutputTokens
			if strings.TrimSpace(res.Text) != "" || err == nil {
				task.Output = res.Text
			}
		} else if err != nil {
			task.DurationMs += elapsedMs
		}

		if run.isCancelled() || errors.Is(taskCtx.Err(), context.Canceled) {
			if latest, ok, _ := h.store.GetBotTask(task.ID); ok && latest.Status == state.TaskStatusCancelled {
				task.Status = state.TaskStatusCancelled
				if task.FinishedAt == "" {
					task.FinishedAt = latest.FinishedAt
				}
				_ = h.store.UpdateBotTask(task)
				if req.OnFinished != nil {
					req.OnFinished(task)
				}
				return
			}
			h.finalizeTaskCancelled(&task, req)
			return
		}

		if err != nil {
			h.finalizeTaskFailed(&task, err.Error(), req, run.hop)
			return
		}

		if res != nil {
			_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventMessage, map[string]any{
				"role":             "assistant",
				"text":             res.Text,
				"runtimeSessionId": task.RuntimeSessionID,
			})
		}

		run.mu.Lock()
		if len(run.followUps) > 0 {
			prompt = run.followUps[0]
			run.followUps = run.followUps[1:]
			turnFollowUp = true
			run.mu.Unlock()
			_ = h.store.UpdateBotTask(task)
			continue
		}
		run.finishing = true
		run.mu.Unlock()
		break
	}

	task.Status = state.TaskStatusCompleted
	task.Error = ""
	task.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_ = h.store.UpdateBotTask(task)
	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
		"status": state.TaskStatusCompleted,
	})
	h.broadcast(Event{
		Type:    "task_completed",
		BotID:   task.BotID,
		Payload: task,
	})
	if !req.ManagedDelivery {
		h.deliverTaskResult(task, run.hop)
	}
	if req.OnFinished != nil {
		req.OnFinished(task)
	}
}

func (h *Hub) finalizeTaskCancelled(task *state.BotTask, req TaskSpawnRequest) {
	h.removeRemoteBridge(task.ID)
	task.Status = state.TaskStatusCancelled
	if task.FinishedAt == "" {
		task.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_ = h.store.UpdateBotTask(*task)
	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
		"status": state.TaskStatusCancelled,
	})
	h.broadcast(Event{
		Type:    "task_updated",
		BotID:   task.BotID,
		Payload: *task,
	})
	h.broadcast(Event{
		Type:    "task_completed",
		BotID:   task.BotID,
		Payload: *task,
	})
	if req.OnFinished != nil {
		req.OnFinished(*task)
	}
}

func (h *Hub) finalizeTaskFailed(task *state.BotTask, errMsg string, req TaskSpawnRequest, hop int) {
	h.removeRemoteBridge(task.ID)
	task.Status = state.TaskStatusFailed
	task.Error = errMsg
	task.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_ = h.store.UpdateBotTask(*task)
	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventError, map[string]any{
		"error": errMsg,
	})
	_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventStatus, map[string]any{
		"status": state.TaskStatusFailed,
		"error":  errMsg,
	})
	h.broadcast(Event{
		Type:    "task_completed",
		BotID:   task.BotID,
		Payload: *task,
	})
	if !req.ManagedDelivery {
		h.deliverTaskResult(*task, hop)
	}
	if req.OnFinished != nil {
		req.OnFinished(*task)
	}
}

func (h *Hub) executeBotTaskTurn(ctx context.Context, task *state.BotTask, prompt string, req TaskSpawnRequest, hop int, isFollowUp bool) (*harness.TurnResult, error) {
	h.lanesMu.RLock()
	lane, ok := h.lanes[task.BotID]
	var bot config.BotConfig
	var apiToken string
	var hubURL string
	var cfgPath string
	var mcpCmd []string
	if ok {
		bot = lane.bot
		apiToken = h.cfg.HTTP.APIToken
		hubURL = h.cfg.HTTP.BaseURL()
		cfgPath = h.cfg.ConfigPath
		mcpCmd = config.MCPCommand(h.cfg, bot.ID)
	}
	h.lanesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bot %q not found: %w", task.BotID, ErrBotNotFound)
	}

	if hk := strings.TrimSpace(task.Harness); hk != "" && hk != bot.Harness {
		bot.Harness = hk
		if strings.TrimSpace(task.Model) == "" && bot.ModelInherited {
			h.lanesMu.RLock()
			bot.Model = h.cfg.DefaultModelFor(bot.Harness)
			h.lanesMu.RUnlock()
		}
	}
	if m := strings.TrimSpace(task.Model); m != "" {
		bot.Model = m
		bot.ModelInherited = false
	}
	if req.TimeoutSec > 0 {
		bot.TimeoutSec = req.TimeoutSec
	}
	permMode := bot.EffectivePermissionMode()
	if norm, err := config.NormalizePermissionMode(task.PermissionMode); err == nil && norm != "" {
		permMode = norm
		bot.PermissionMode = norm
		bot.AutoApprove = nil
	}

	if isRemoteAgentTask(task.Kind, task.DeviceID) {
		remoteEnv := make(map[string]string, len(req.ExtraEnv)+2)
		remoteEnv["PEBBLE_HOP"] = strconv.Itoa(hop)
		if role := strings.TrimSpace(req.MCPRole); role != "" {
			remoteEnv["PEBBLE_MCP_ROLE"] = role
		}
		for k, v := range req.ExtraEnv {
			if k != "" && v != "" {
				remoteEnv[k] = v
			}
		}
		return h.executeRemoteBotTaskTurn(ctx, task, bot, permMode, hubURL, remoteEnv, prompt, req, isFollowUp)
	}

	bot.Workspace = h.effectiveTaskExecDir(*task, bot.Workspace)

	runner, err := h.registry.Get(bot.Harness)
	if err != nil {
		return nil, err
	}

	h.activeEphemeralMu.Lock()
	h.activeEphemeral[bot.Harness]++
	h.activeEphemeralMu.Unlock()
	defer func() {
		h.activeEphemeralMu.Lock()
		if h.activeEphemeral[bot.Harness] > 0 {
			h.activeEphemeral[bot.Harness]--
		}
		h.activeEphemeralMu.Unlock()
		h.flushPendingReload(bot.Harness)
	}()

	platPath := h.platformInstructionsPath()
	effectivePrompt := prompt
	if bot.Harness == "dsh" && platPath != "" {
		if raw, err := os.ReadFile(platPath); err == nil && len(raw) > 0 {
			effectivePrompt = strings.TrimSpace(string(raw)) + "\n\n" + prompt
		}
	}

	extraEnv := map[string]string{
		"PEBBLE_URL":       hubURL,
		"PEBBLE_BOT_ID":    bot.ID,
		"PEBBLE_HOP":       strconv.Itoa(hop),
		"PEBBLE_TASK_ID":   task.ID,
		"PEBBLE_DEVICE_ID": task.DeviceID,
	}
	if cfgPath != "" {
		extraEnv["PEBBLE_CONFIG"] = cfgPath
	}
	var mcpEnv map[string]string
	if apiToken != "" {
		extraEnv["PEBBLE_API_TOKEN"] = apiToken
		mcpEnv = map[string]string{"PEBBLE_API_TOKEN": apiToken}
	}
	if role := strings.TrimSpace(req.MCPRole); role != "" {
		extraEnv["PEBBLE_MCP_ROLE"] = role
		if mcpEnv == nil {
			mcpEnv = make(map[string]string, 1)
		}
		mcpEnv["PEBBLE_MCP_ROLE"] = role
	}
	if mcpEnv == nil {
		mcpEnv = make(map[string]string, 2)
	}
	mcpEnv["PEBBLE_TASK_ID"] = task.ID
	mcpEnv["PEBBLE_DEVICE_ID"] = task.DeviceID
	if h.canCoordinatorApprove(bot.ID) {
		extraEnv["PEBBLE_COORDINATOR_CAN_APPROVE"] = "1"
		mcpEnv["PEBBLE_COORDINATOR_CAN_APPROVE"] = "1"
	}
	for k, v := range h.botBrowserEnv(bot) {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}
	for k, v := range req.ExtraEnv {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}

	var toolMu sync.Mutex
	var toolOrder []string
	toolCounts := make(map[string]int)

	ephemeral := task.Kind != state.TaskKindWorker
	sessionID := ""
	if !ephemeral {
		sessionID = task.RuntimeSessionID
	}

	var onQuestion func(context.Context, harness.QuestionRequest) (harness.QuestionResponse, error)
	var onPermission func(context.Context, harness.PermissionRequest) (harness.PermissionResponse, error)
	if task.Kind != state.TaskKindRoutine {
		onQuestion = func(qCtx context.Context, qReq harness.QuestionRequest) (harness.QuestionResponse, error) {
			return h.AskQuestionForTask(qCtx, bot, task.ID, task.DeviceID, qReq)
		}
		onPermission = func(pCtx context.Context, pReq harness.PermissionRequest) (harness.PermissionResponse, error) {
			return h.RequestApprovalForTask(pCtx, bot, task.ID, task.DeviceID, permMode, pReq)
		}
	}

	turnCtx := ctx
	var turnCancel context.CancelFunc
	if req.TimeoutSec > 0 && (permMode == config.PermissionModeAuto || task.Kind == state.TaskKindRoutine) {
		turnCtx, turnCancel = context.WithTimeout(ctx, time.Duration(req.TimeoutSec)*time.Second)
	} else {
		turnCtx, turnCancel = context.WithCancel(ctx)
	}
	defer turnCancel()

	res, err := runner.RunTurn(turnCtx, harness.RunOptions{
		Bot:                      bot,
		Prompt:                   effectivePrompt,
		SessionID:                sessionID,
		Ephemeral:                ephemeral,
		PermissionMode:           permMode,
		PlatformInstructionsPath: platPath,
		MCPCommand:               mcpCmd,
		MCPEnv:                   mcpEnv,
		ExtraEnv:                 extraEnv,
		OnText: func(chunk string) {
			if chunk != "" {
				_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventChunk, map[string]any{
					"chunk": chunk,
				})
				h.broadcast(Event{
					Type:  "task_chunk",
					BotID: task.BotID,
					Payload: map[string]any{
						"taskId": task.ID,
						"botId":  task.BotID,
						"kind":   task.Kind,
						"chunk":  chunk,
					},
				})
			}
			if req.OnText != nil {
				req.OnText(chunk)
			}
		},
		OnQuestion:   onQuestion,
		OnPermission: onPermission,
		OnToolUse: func(toolName string) {
			if toolName != "" {
				toolMu.Lock()
				if toolCounts[toolName] == 0 {
					toolOrder = append(toolOrder, toolName)
				}
				toolCounts[toolName]++
				toolMu.Unlock()
				_, _ = h.store.AppendBotTaskEvent(task.ID, task.BotID, state.TaskEventToolUse, map[string]any{
					"tool": toolName,
				})
				h.broadcast(Event{
					Type:  "task_tool_use",
					BotID: task.BotID,
					Payload: map[string]any{
						"taskId": task.ID,
						"botId":  task.BotID,
						"kind":   task.Kind,
						"tool":   toolName,
					},
				})
			}
			if req.OnToolUse != nil {
				req.OnToolUse(toolName)
			}
		},
	})
	toolMu.Lock()
	tools := formatToolCounts(toolOrder, toolCounts)
	toolMu.Unlock()
	if res != nil && len(res.Tools) == 0 && len(tools) > 0 {
		res.Tools = tools
	}
	_ = h.captureTaskCheckpoint(context.Background(), task)
	return res, err
}

func (h *Hub) deliverTaskResult(task state.BotTask, hop int) {
	label := strings.TrimSpace(task.Title)
	if label == "" {
		label = task.ID
	}

	switch task.Delivery {
	case state.TaskDeliverySilent:
		return

	case state.TaskDeliveryReceipt:
		var text string
		if task.Status == state.TaskStatusCompleted {
			text = strings.TrimSpace(task.Output)
		} else if task.Status == state.TaskStatusFailed {
			text = fmt.Sprintf("Task failed: %s", task.Error)
		}
		if text == "" {
			return
		}
		var sessID string
		var turn int
		if h.store != nil {
			if rec, ok := h.store.Get(task.BotID); ok {
				sessID = rec.ID
				turn = rec.TurnCount
			}
		}
		h.appendMessage(state.ChatMessage{
			BotID:        task.BotID,
			SessionID:    sessID,
			Turn:         turn,
			Role:         "assistant",
			From:         "task:" + label,
			Text:         text,
			Tools:        task.Tools,
			DurationMs:   task.DurationMs,
			CostUSD:      task.CostUSD,
			InputTokens:  task.InputTokens,
			OutputTokens: task.OutputTokens,
		})

	case state.TaskDeliveryWake:
		var summary string
		if task.Status == state.TaskStatusCompleted {
			body := strings.TrimSpace(task.Output)
			if body == "" {
				body = "(no output)"
			}
			summary = fmt.Sprintf("[Task completed: %s (id=%s, kind=%s)]\n%s", label, task.ID, task.Kind, body)
		} else {
			summary = fmt.Sprintf("[Task failed: %s (id=%s, kind=%s)]\nError: %s", label, task.ID, task.Kind, task.Error)
			if body := strings.TrimSpace(task.Output); body != "" {
				summary += "\nOutput:\n" + body
			}
		}

		if hop >= MaxBotHopDepth {
			log.Printf("[tasks] hop limit reached (%d) waking bot=%s from task=%s; recording receipt instead", MaxBotHopDepth, task.BotID, task.ID)
			var sessID string
			var turn int
			if h.store != nil {
				if rec, ok := h.store.Get(task.BotID); ok {
					sessID = rec.ID
					turn = rec.TurnCount
				}
			}
			h.appendMessage(state.ChatMessage{
				BotID:     task.BotID,
				SessionID: sessID,
				Turn:      turn,
				Role:      "system",
				From:      "system",
				Text:      summary,
			})
			return
		}

		h.taskWg.Add(1)
		go func(t state.BotTask, msg string, nextHop int) {
			defer h.taskWg.Done()
			ctx := h.taskBaseCtx
			if ctx == nil {
				ctx = context.Background()
			}
			if _, err := h.Send(ctx, t.BotID, msg, SendOptions{
				Source: "task:" + t.ID,
				Hop:    nextHop,
			}); err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Printf("[tasks] wake bot=%s for task=%s failed: %v", t.BotID, t.ID, err)
				}
				return
			}
			if h.store != nil {
				_ = h.store.MarkReceiptsInjected([]string{t.ID})
			}
		}(task, summary, hop+1)
	}
}

func roundCost(v float64) float64 {
	return float64(int64(v*1e6+0.5)) / 1e6
}
