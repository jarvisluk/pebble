package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/protocol"
)

const (
	defaultRemoteAgentTimeoutSec = 300
	maxOutboundQueueSize         = 128
)

type remoteAgentTask struct {
	taskID         string
	bot            config.BotConfig
	harnessKind    string
	workdir        string
	mcpHubURL      string
	extraEnv       map[string]string
	permissionMode string

	turnMu sync.Mutex

	mu           sync.Mutex
	running      bool
	sessionID    string
	activeCancel context.CancelFunc
}

func normalizeCeilingMode(mode string) string {
	trimmed := strings.ToLower(strings.TrimSpace(mode))
	if trimmed == "" {
		return ""
	}
	norm, err := config.NormalizePermissionMode(trimmed)
	if err != nil {
		return config.PermissionModePlan
	}
	return norm
}

func slugifyWorkspaceID(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "workspace"
	}
	return s
}

// NormalizeWorkspaces expands, cleans, deduplicates IDs, and detects git repos
// for configured authorized workspaces on a node.
func NormalizeWorkspaces(in []NodeWorkspace, homeDir string) []NodeWorkspace {
	if len(in) == 0 {
		return nil
	}
	out := make([]NodeWorkspace, 0, len(in))
	usedIDs := make(map[string]int, len(in))
	for _, ws := range in {
		p := expandHome(ws.Path, homeDir)
		if p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		p = filepath.Clean(p)
		name := strings.TrimSpace(ws.Name)
		if name == "" {
			name = filepath.Base(p)
		}
		baseID := strings.TrimSpace(ws.ID)
		if baseID == "" {
			baseID = slugifyWorkspaceID(name)
		}
		id := baseID
		if n, exists := usedIDs[id]; exists {
			for {
				n++
				cand := fmt.Sprintf("%s-%d", baseID, n)
				if usedIDs[cand] == 0 {
					id = cand
					usedIDs[baseID] = n
					break
				}
			}
		}
		usedIDs[id] = 1
		isGit := ws.IsGit
		if !isGit {
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				isGit = true
			}
		}
		out = append(out, NodeWorkspace{
			ID:            id,
			Name:          name,
			Path:          p,
			IsGit:         isGit,
			DefaultBranch: strings.TrimSpace(ws.DefaultBranch),
		})
	}
	return out
}

func (a *Agent) snapshotMaxPermissionMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.maxPermissionMode
}

func (a *Agent) snapshotWorkspaces() []NodeWorkspace {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]NodeWorkspace(nil), a.workspaces...)
}

func (a *Agent) ensureHarnessRegistry() (*harness.Registry, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.harnessRegistry == nil {
		a.harnessRegistry = harness.NewDefaultRegistry(&config.HubConfig{})
		a.ownsRegistry = true
	}
	return a.harnessRegistry, a.ownsRegistry
}

func (a *Agent) probeLocalHarnesses(ctx context.Context) []NodeHarnessInfo {
	if !a.hasPerm(PermAgent) {
		return nil
	}
	reg, owns := a.ensureHarnessRegistry()
	var list []NodeHarnessInfo
	if a.probeHarnesses != nil {
		list = a.probeHarnesses(ctx, reg)
	} else {
		all := reg.All()
		list = make([]NodeHarnessInfo, 0, len(all))
		for _, h := range all {
			if !owns {
				list = append(list, NodeHarnessInfo{
					Kind:                   h.Kind(),
					Label:                  h.Label(),
					Installed:              true,
					AuthStatus:             "ready",
					PermissionCapabilities: harness.CapabilitiesOf(h),
				})
				continue
			}
			installed, _ := harness.DetectHarness(h.Kind())
			if !installed {
				continue
			}
			authStatus, authDetail := harness.DetectHarnessAuthCached(ctx, h.Kind())
			list = append(list, NodeHarnessInfo{
				Kind:                   h.Kind(),
				Label:                  h.Label(),
				Installed:              true,
				AuthStatus:             authStatus,
				AuthDetail:             authDetail,
				PermissionCapabilities: harness.CapabilitiesOf(h),
			})
		}
	}
	a.mu.Lock()
	a.harnesses = append([]NodeHarnessInfo(nil), list...)
	a.mu.Unlock()
	return list
}

func (a *Agent) activeTaskIDs() []string {
	a.tasksMu.Lock()
	defer a.tasksMu.Unlock()
	var ids []string
	for tid, t := range a.tasks {
		t.mu.Lock()
		running := t.running
		t.mu.Unlock()
		if running {
			ids = append(ids, tid)
		}
	}
	sort.Strings(ids)
	return ids
}

func (a *Agent) buildHelloPayload(ctx context.Context) WSHelloPayload {
	name, perms, _ := a.snapshot()
	var harnesses []NodeHarnessInfo
	var workspaces []NodeWorkspace
	if a.hasPerm(PermAgent) {
		harnesses = a.probeLocalHarnesses(ctx)
		workspaces = a.snapshotWorkspaces()
	}
	return WSHelloPayload{
		ID:                a.nodeID,
		Name:              name,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		Hostname:          a.hostname,
		HomeDir:           a.homeDir,
		Permissions:       perms,
		ProtocolVersion:   protocol.CurrentProtocol,
		Capabilities:      a.capabilities(),
		Version:           protocol.AppVersion,
		Harnesses:         harnesses,
		Workspaces:        workspaces,
		MaxPermissionMode: a.snapshotMaxPermissionMode(),
		ActiveTaskIDs:     a.activeTaskIDs(),
	}
}

func (a *Agent) taskBaseContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.taskBaseCtx == nil {
		return context.Background()
	}
	return a.taskBaseCtx
}

func (a *Agent) enqueueOutbound(f WSFrame) {
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	if len(a.outboundQueue) >= maxOutboundQueueSize {
		copy(a.outboundQueue, a.outboundQueue[1:])
		a.outboundQueue[len(a.outboundQueue)-1] = f
		return
	}
	a.outboundQueue = append(a.outboundQueue, f)
}

func (a *Agent) sendAgentWSFrame(frameType string, payload any, queueIfOffline bool) {
	frame, err := encodeWSFrame(frameType, payload)
	if err != nil {
		return
	}
	a.wsMu.Lock()
	wsc := a.wsActive
	a.wsMu.Unlock()
	if wsc == nil {
		if queueIfOffline {
			a.enqueueOutbound(frame)
		}
		return
	}
	if err := wsc.writeRawFrame(context.Background(), frame); err != nil && queueIfOffline {
		a.enqueueOutbound(frame)
	}
}

func (a *Agent) sendAgentError(taskID, msg string) {
	a.sendAgentWSFrame(FrameAgentEvent, AgentEvent{
		TaskID: taskID,
		Type:   AgentEventError,
		Error:  msg,
	}, true)
}

func sanitizeTaskSegment(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), ".")
	if s == "" {
		return "task"
	}
	return s
}

func evalExistingSymlinks(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	cur := abs
	var suffix []string
	for {
		resolved, rerr := filepath.EvalSymlinks(cur)
		if rerr == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(abs), nil
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func isWithinRoot(root, candidate string) bool {
	realRoot, err1 := evalExistingSymlinks(root)
	realCand, err2 := evalExistingSymlinks(candidate)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, realCand)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func hasDotDotSegment(p string) bool {
	for _, seg := range strings.FieldsFunc(filepath.ToSlash(p), func(r rune) bool { return r == '/' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

func (a *Agent) resolveTaskWorkdir(workspaceID, workdir, taskID string) (string, error) {
	workspaces := a.snapshotWorkspaces()
	wsID := strings.TrimSpace(workspaceID)
	rawDir := strings.TrimSpace(workdir)

	if wsID != "" {
		var matched *NodeWorkspace
		for i := range workspaces {
			if workspaces[i].ID == wsID {
				matched = &workspaces[i]
				break
			}
		}
		if matched == nil {
			return "", fmt.Errorf("workspace %q is not authorized on this node", wsID)
		}
		if rawDir == "" {
			return matched.Path, nil
		}
		cand := expandHome(rawDir, a.homeDir)
		if !filepath.IsAbs(cand) {
			cand = filepath.Join(matched.Path, cand)
		}
		cand = filepath.Clean(cand)
		if !isWithinRoot(matched.Path, cand) {
			return "", fmt.Errorf("workdir %q escapes authorized workspace %q", workdir, wsID)
		}
		return cand, nil
	}

	if rawDir != "" {
		if hasDotDotSegment(rawDir) {
			return "", fmt.Errorf("workdir %q contains invalid path traversal segment", workdir)
		}
		cand := expandHome(rawDir, a.homeDir)
		if !filepath.IsAbs(cand) {
			return "", fmt.Errorf("workdir %q must be an absolute path when workspaceId is omitted", workdir)
		}
		cand = filepath.Clean(cand)
		if len(workspaces) > 0 {
			for _, ws := range workspaces {
				if isWithinRoot(ws.Path, cand) {
					return cand, nil
				}
			}
			return "", fmt.Errorf("workdir %q is outside authorized workspaces", workdir)
		}
		return cand, nil
	}

	scratch := filepath.Join(os.TempDir(), "pebble-node-tasks", sanitizeTaskSegment(taskID))
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return "", err
	}
	return scratch, nil
}

func materializeContextFiles(taskID string, files []AgentContextFile) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	baseDir := filepath.Join(os.TempDir(), "pebble-node-context", sanitizeTaskSegment(taskID))
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}
	written := make([]string, 0, len(files))
	for _, cf := range files {
		rel := strings.TrimSpace(cf.Path)
		if rel == "" {
			continue
		}
		if filepath.IsAbs(rel) || hasDotDotSegment(rel) {
			return nil, fmt.Errorf("invalid context file path %q", cf.Path)
		}
		target := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(rel)))
		if !isWithinRoot(baseDir, target) {
			return nil, fmt.Errorf("context file path %q escapes context directory", cf.Path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(cf.Content), 0o600); err != nil {
			return nil, err
		}
		written = append(written, target)
	}
	return written, nil
}

func buildRemotePrompt(prompt, contextSummary string, contextPaths []string) string {
	var parts []string
	if sum := strings.TrimSpace(contextSummary); sum != "" {
		parts = append(parts, "[Context Summary]\n"+sum)
	}
	if len(contextPaths) > 0 {
		parts = append(parts, "[Context Files]\n"+strings.Join(contextPaths, "\n"))
	}
	if p := strings.TrimSpace(prompt); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, "\n\n")
}

func resolveNodePebbleBinary() string {
	if exe, err := os.Executable(); err == nil && exe != "" && !strings.Contains(exe, "go-build") && !strings.HasSuffix(exe, ".test") {
		return exe
	}
	if p, err := exec.LookPath("pebble"); err == nil && p != "" {
		return p
	}
	return "pebble"
}

func (a *Agent) handleAgentStart(req AgentStartRequest) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return
	}
	if !a.hasPerm(PermAgent) {
		a.sendAgentError(taskID, fmt.Sprintf("%q permission is not enabled on this host", PermLabel(PermAgent)))
		return
	}
	workdir, err := a.resolveTaskWorkdir(req.WorkspaceID, req.Workdir, taskID)
	if err != nil {
		a.sendAgentError(taskID, err.Error())
		return
	}
	reg, _ := a.ensureHarnessRegistry()
	runner, err := reg.Get(strings.TrimSpace(req.Harness))
	if err != nil {
		a.sendAgentError(taskID, err.Error())
		return
	}
	contextPaths, err := materializeContextFiles(taskID, req.ContextFiles)
	if err != nil {
		a.sendAgentError(taskID, err.Error())
		return
	}
	effectiveMode := IntersectPermissionMode(req.PermissionMode, a.snapshotMaxPermissionMode())
	mcpHubURL := strings.TrimRight(strings.TrimSpace(req.MCPHubURL), "/")
	if mcpHubURL == "" {
		mcpHubURL = strings.TrimRight(strings.TrimSpace(req.TunnelProxy), "/")
	}
	if mcpHubURL == "" {
		mcpHubURL = a.baseURL
	}

	bot := config.BotConfig{
		ID:             strings.TrimSpace(req.BotID),
		Name:           strings.TrimSpace(req.BotName),
		Workspace:      workdir,
		Harness:        runner.Kind(),
		Model:          strings.TrimSpace(req.Model),
		Agent:          strings.TrimSpace(req.Agent),
		PermissionMode: effectiveMode,
		TimeoutSec:     req.TimeoutSec,
	}
	if bot.ID == "" {
		bot.ID = "remote-task"
	}
	if bot.Name == "" {
		bot.Name = bot.ID
	}

	task := &remoteAgentTask{
		taskID:         taskID,
		bot:            bot,
		harnessKind:    runner.Kind(),
		workdir:        workdir,
		mcpHubURL:      mcpHubURL,
		extraEnv:       req.ExtraEnv,
		permissionMode: effectiveMode,
		sessionID:      strings.TrimSpace(req.SessionID),
	}

	a.tasksMu.Lock()
	a.tasks[taskID] = task
	a.tasksMu.Unlock()

	prompt := buildRemotePrompt(req.Prompt, req.ContextSummary, contextPaths)
	a.executeAgentTurn(task, runner, prompt, effectiveMode, req.TimeoutSec)
}

func (a *Agent) handleAgentSend(req AgentSendRequest) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return
	}
	if !a.hasPerm(PermAgent) {
		a.sendAgentError(taskID, fmt.Sprintf("%q permission is not enabled on this host", PermLabel(PermAgent)))
		return
	}
	a.tasksMu.Lock()
	task := a.tasks[taskID]
	a.tasksMu.Unlock()
	if task == nil {
		a.sendAgentError(taskID, fmt.Sprintf("unknown remote task %q", taskID))
		return
	}
	reg, _ := a.ensureHarnessRegistry()
	runner, err := reg.Get(task.harnessKind)
	if err != nil {
		a.sendAgentError(taskID, err.Error())
		return
	}
	reqMode := strings.TrimSpace(req.PermissionMode)
	if reqMode == "" {
		reqMode = task.permissionMode
	}
	effectiveMode := IntersectPermissionMode(reqMode, a.snapshotMaxPermissionMode())
	a.executeAgentTurn(task, runner, req.Prompt, effectiveMode, req.TimeoutSec)
}

func (a *Agent) handleAgentCancel(req AgentCancelRequest) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return
	}
	a.tasksMu.Lock()
	task := a.tasks[taskID]
	a.tasksMu.Unlock()
	if task == nil {
		return
	}
	task.mu.Lock()
	cancel := task.activeCancel
	task.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Agent) cancelAllTasks(_ string) {
	a.tasksMu.Lock()
	tasks := make([]*remoteAgentTask, 0, len(a.tasks))
	for _, t := range a.tasks {
		tasks = append(tasks, t)
	}
	a.tasksMu.Unlock()

	for _, t := range tasks {
		t.mu.Lock()
		cancel := t.activeCancel
		t.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func (a *Agent) executeAgentTurn(task *remoteAgentTask, runner harness.Harness, prompt, effectiveMode string, timeoutSec int) {
	task.turnMu.Lock()
	defer task.turnMu.Unlock()

	if timeoutSec <= 0 {
		timeoutSec = task.bot.TimeoutSec
	}
	if timeoutSec <= 0 {
		timeoutSec = defaultRemoteAgentTimeoutSec
	}
	baseCtx := a.taskBaseContext()
	if baseCtx.Err() != nil {
		a.sendAgentError(task.taskID, "node agent stopped")
		return
	}
	turnCtx, cancel := context.WithTimeout(baseCtx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	task.mu.Lock()
	task.running = true
	task.activeCancel = cancel
	sessionID := task.sessionID
	task.mu.Unlock()

	defer func() {
		task.mu.Lock()
		task.running = false
		task.activeCancel = nil
		task.mu.Unlock()
	}()

	_, _, token := a.snapshot()
	pebbleBin := resolveNodePebbleBinary()
	var mcpCmd []string
	if task.mcpHubURL != "" {
		mcpCmd = []string{pebbleBin, "mcp", "--hub-url", task.mcpHubURL, "--bot-id", task.bot.ID}
	}
	extraEnv := make(map[string]string, len(task.extraEnv)+4)
	for k, v := range task.extraEnv {
		if k != "" && v != "" {
			extraEnv[k] = v
		}
	}
	if task.mcpHubURL != "" {
		extraEnv["PEBBLE_URL"] = task.mcpHubURL
	}
	extraEnv["PEBBLE_BOT_ID"] = task.bot.ID
	extraEnv["PEBBLE_TASK_ID"] = task.taskID
	if token != "" {
		extraEnv["PEBBLE_API_TOKEN"] = token
	}

	mcpEnv := map[string]string{
		"PEBBLE_BOT_ID":  task.bot.ID,
		"PEBBLE_TASK_ID": task.taskID,
	}
	if task.mcpHubURL != "" {
		mcpEnv["PEBBLE_URL"] = task.mcpHubURL
	}
	if token != "" {
		mcpEnv["PEBBLE_API_TOKEN"] = token
	}

	botCopy := task.bot
	botCopy.PermissionMode = effectiveMode

	a.sendAgentWSFrame(FrameAgentEvent, AgentEvent{
		TaskID: task.taskID,
		Type:   AgentEventStatus,
		Status: "running",
	}, false)

	res, err := runner.RunTurn(turnCtx, harness.RunOptions{
		Bot:            botCopy,
		Prompt:         prompt,
		SessionID:      sessionID,
		PermissionMode: effectiveMode,
		MCPCommand:     mcpCmd,
		MCPEnv:         mcpEnv,
		ExtraEnv:       extraEnv,
		OnText: func(chunk string) {
			if chunk == "" {
				return
			}
			a.sendAgentWSFrame(FrameAgentEvent, AgentEvent{
				TaskID: task.taskID,
				Type:   AgentEventChunk,
				Chunk:  chunk,
			}, false)
		},
		OnToolUse: func(toolName string) {
			if toolName == "" {
				return
			}
			a.sendAgentWSFrame(FrameAgentEvent, AgentEvent{
				TaskID:   task.taskID,
				Type:     AgentEventToolUse,
				ToolName: toolName,
			}, false)
		},
		OnPermission: func(pCtx context.Context, req harness.PermissionRequest) (harness.PermissionResponse, error) {
			return a.bridgeApproval(pCtx, task.taskID, botCopy.ID, req)
		},
		OnQuestion: func(qCtx context.Context, req harness.QuestionRequest) (harness.QuestionResponse, error) {
			return a.bridgeQuestion(qCtx, task.taskID, botCopy.ID, req)
		},
	})
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.Canceled) {
			msg = "task cancelled"
		}
		a.sendAgentError(task.taskID, msg)
		return
	}

	var doneEvt AgentEvent
	doneEvt.TaskID = task.taskID
	doneEvt.Type = AgentEventDone
	if res != nil {
		if res.SessionID != "" {
			task.mu.Lock()
			task.sessionID = res.SessionID
			task.mu.Unlock()
		}
		doneEvt.Text = res.Text
		doneEvt.SessionID = res.SessionID
		doneEvt.Tools = append([]string(nil), res.Tools...)
		doneEvt.DurationMs = res.DurationMs
		u := res.Usage
		doneEvt.Usage = &u
	}
	a.sendAgentWSFrame(FrameAgentEvent, doneEvt, true)
}

func (a *Agent) bridgeApproval(ctx context.Context, taskID, botID string, req harness.PermissionRequest) (harness.PermissionResponse, error) {
	reqID := newCallID()
	ch := make(chan AgentApprovalResponse, 1)
	a.promptsMu.Lock()
	a.pendingApprovals[reqID] = ch
	a.promptsMu.Unlock()

	defer func() {
		a.promptsMu.Lock()
		delete(a.pendingApprovals, reqID)
		a.promptsMu.Unlock()
	}()

	a.sendAgentWSFrame(FrameAgentApprovalReq, AgentApprovalRequest{
		RequestID: reqID,
		TaskID:    taskID,
		BotID:     botID,
		Request:   req,
	}, true)

	select {
	case resp := <-ch:
		if resp.Error != "" && resp.Response.Decision == "" {
			return harness.PermissionResponse{Decision: "reject", Reason: resp.Error}, nil
		}
		return resp.Response, nil
	case <-ctx.Done():
		return harness.PermissionResponse{Decision: "cancelled", Reason: "cancelled"}, ctx.Err()
	}
}

func (a *Agent) deliverApprovalResponse(resp AgentApprovalResponse) {
	a.promptsMu.Lock()
	ch, ok := a.pendingApprovals[resp.RequestID]
	if ok {
		delete(a.pendingApprovals, resp.RequestID)
	}
	a.promptsMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

func (a *Agent) bridgeQuestion(ctx context.Context, taskID, botID string, req harness.QuestionRequest) (harness.QuestionResponse, error) {
	reqID := newCallID()
	ch := make(chan AgentQuestionResponse, 1)
	a.promptsMu.Lock()
	a.pendingQuestions[reqID] = ch
	a.promptsMu.Unlock()

	defer func() {
		a.promptsMu.Lock()
		delete(a.pendingQuestions, reqID)
		a.promptsMu.Unlock()
	}()

	a.sendAgentWSFrame(FrameAgentQuestionReq, AgentQuestionRequest{
		RequestID: reqID,
		TaskID:    taskID,
		BotID:     botID,
		Request:   req,
	}, true)

	select {
	case resp := <-ch:
		if resp.Error != "" && !resp.Response.Cancelled && len(resp.Response.Answers) == 0 {
			return harness.QuestionResponse{Cancelled: true, Message: resp.Error}, nil
		}
		return resp.Response, nil
	case <-ctx.Done():
		return harness.QuestionResponse{Cancelled: true, Message: "cancelled"}, ctx.Err()
	}
}

func (a *Agent) deliverQuestionResponse(resp AgentQuestionResponse) {
	a.promptsMu.Lock()
	ch, ok := a.pendingQuestions[resp.RequestID]
	if ok {
		delete(a.pendingQuestions, resp.RequestID)
	}
	a.promptsMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

// Keep json referenced when needed by future extensions.
var _ = json.Marshal
