package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bot-hub/internal/config"
)

// ACPOptions configures a generic ACP (Agent Client Protocol) stdio harness.
type ACPOptions struct {
	Kind            string
	Label           string
	Binaries        []string
	Args            []string
	EnvPrefix       string
	ConfigDir       string
	DefaultModels   []string
	BuiltinCommands []SlashCommand
}

// ACPHarness runs an Agent Client Protocol (JSON-RPC 2.0 over stdio) agent CLI.
type ACPHarness struct {
	opts        ACPOptions
	pool        procPool[*acpProc]
	cmdMu       sync.Mutex
	botCommands map[string][]SlashCommand
}

type acpProc struct {
	*lineProc
	workspace      string
	sessionID      string
	nextID         atomic.Int64
	availableModes map[string]bool
	appliedMode    string
	turnMu         sync.Mutex
	turnCtx        context.Context
	turnOpts       RunOptions
	permMu         sync.Mutex
	activePerms    map[string]context.CancelFunc
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewCodeBuddyHarness() *ACPHarness {
	return NewACPHarness(ACPOptions{
		Kind:          "codebuddy",
		Label:         "CodeBuddy CLI",
		Binaries:      []string{"codebuddy"},
		Args:          []string{"--acp"},
		EnvPrefix:     "CODEBUDDY",
		ConfigDir:     ".codebuddy",
		DefaultModels: []string{"deepseek-v3", "deepseek-r1", "hunyuan-turbos"},
	})
}

func NewQoderHarness() *ACPHarness {
	return NewACPHarness(ACPOptions{
		Kind:          "qoder",
		Label:         "Qoder CLI",
		Binaries:      []string{"qoderclicn", "qoder"},
		Args:          []string{"acp"},
		EnvPrefix:     "QODER",
		ConfigDir:     ".qoder",
		DefaultModels: []string{"auto", "qwen3-coder-plus"},
	})
}

func NewTraeHarness() *ACPHarness {
	return NewACPHarness(ACPOptions{
		Kind:          "trae",
		Label:         "Trae CLI",
		Binaries:      []string{"traecli"},
		Args:          []string{"acp", "serve"},
		EnvPrefix:     "TRAE",
		ConfigDir:     ".trae",
		DefaultModels: []string{"auto"},
	})
}

func NewGrokHarness() *ACPHarness {
	return NewACPHarness(ACPOptions{
		Kind:          "grok",
		Label:         "Grok CLI",
		Binaries:      []string{"grok"},
		Args:          []string{"agent", "stdio"},
		EnvPrefix:     "GROK",
		ConfigDir:     ".grok",
		DefaultModels: []string{"grok-4", "grok-code-fast-1"},
	})
}

func NewACPHarness(opts ACPOptions) *ACPHarness {
	return &ACPHarness{
		opts:        opts,
		botCommands: make(map[string][]SlashCommand),
	}
}

func (h *ACPHarness) Kind() string  { return h.opts.Kind }
func (h *ACPHarness) Label() string { return h.opts.Label }

func (h *ACPHarness) PermissionCapabilities() PermissionCapabilities {
	return PermissionCapabilities{
		Modes: []string{
			config.PermissionModeAuto,
			config.PermissionModeEdit,
			config.PermissionModeAsk,
			config.PermissionModePlan,
		},
		InteractiveApproval: true,
		SupportsAllowAlways: true,
	}
}

func (h *ACPHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := h.opts.BuiltinCommands
	if len(builtins) == 0 {
		builtins = []SlashCommand{
			{Name: "compact", Description: "压缩当前会话上下文", Source: "harness"},
			{Name: "clear", Description: "清空会话历史", Source: "harness"},
			{Name: "plan", Description: "切换计划模式", Source: "harness"},
			{Name: "review", Description: "代码评审", Source: "harness"},
			{Name: "init", Description: "初始化项目配置", Source: "harness"},
			{Name: "help", Description: "查看可用命令帮助", Source: "harness"},
		}
	}

	h.cmdMu.Lock()
	cached := append([]SlashCommand(nil), h.botCommands[bot.ID]...)
	h.cmdMu.Unlock()

	var local []SlashCommand
	if dir := strings.TrimSpace(h.opts.ConfigDir); dir != "" {
		if bot.Workspace != "" {
			local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, dir, "commands"), "harness")...)
			local = append(local, scanSkillDirs(filepath.Join(bot.Workspace, dir, "skills"))...)
		}
		if home, err := os.UserHomeDir(); err == nil {
			local = append(local, scanMarkdownCommands(filepath.Join(home, dir, "commands"), "harness")...)
			local = append(local, scanSkillDirs(filepath.Join(home, dir, "skills"))...)
		}
	}
	local = append(local, scanSharedSkills(bot.Workspace)...)
	return mergeSlashCommands(cached, builtins, local), nil
}

func (h *ACPHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	var local []AgentInfo
	if dir := strings.TrimSpace(h.opts.ConfigDir); dir != "" {
		if bot.Workspace != "" {
			local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, dir, "agents"))...)
		}
		if home, err := os.UserHomeDir(); err == nil {
			local = append(local, scanAgentsDir(filepath.Join(home, dir, "agents"))...)
		}
	}
	return mergeAgents(local), nil
}

func (h *ACPHarness) handleAvailableCommands(botID string, raw []struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Input       *struct {
		Hint string `json:"hint"`
	} `json:"input"`
}) {
	if botID == "" {
		return
	}
	list := make([]SlashCommand, 0, len(raw))
	for _, c := range raw {
		hint := ""
		if c.Input != nil {
			hint = c.Input.Hint
		}
		list = append(list, SlashCommand{
			Name:         c.Name,
			Description:  c.Description,
			ArgumentHint: hint,
			Source:       "harness",
		})
	}
	h.cmdMu.Lock()
	if h.botCommands == nil {
		h.botCommands = make(map[string][]SlashCommand)
	}
	h.botCommands[botID] = list
	h.cmdMu.Unlock()
}

func (h *ACPHarness) Models(context.Context) ([]string, error) {
	return append([]string(nil), h.opts.DefaultModels...), nil
}

func (h *ACPHarness) Close() {
	h.pool.close()
}

// Reload terminates cached ACP processes so updated settings apply on the next turn.
func (h *ACPHarness) Reload() {
	h.pool.clear()
}

func (h *ACPHarness) resolveBinary() (string, error) {
	if h.opts.EnvPrefix != "" {
		if v := strings.TrimSpace(os.Getenv(h.opts.EnvPrefix + "_BIN")); v != "" {
			return v, nil
		}
	}
	for _, bin := range h.opts.Binaries {
		if p, ok := findBinary(bin); ok {
			return p, nil
		}
	}
	if len(h.opts.Binaries) > 0 {
		return h.opts.Binaries[0], nil
	}
	return "", fmt.Errorf("no binary configured for harness %q", h.opts.Kind)
}

func (h *ACPHarness) getOrStartProc(ctx context.Context, opts RunOptions) (*acpProc, func(), error) {
	bot := opts.Bot
	return h.pool.acquire(
		bot.ID,
		func(p *acpProc) bool {
			return p.workspace == bot.Workspace && (opts.SessionID == "" || p.sessionID == opts.SessionID)
		},
		func() (*acpProc, error) {
			return h.startProc(ctx, opts)
		},
	)
}

func (h *ACPHarness) startProc(ctx context.Context, opts RunOptions) (*acpProc, error) {
	bot := opts.Bot
	bin, err := h.resolveBinary()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, h.opts.Args...)
	if bot.Workspace != "" {
		cmd.Dir = bot.Workspace
	}
	cmd.Env = mergedEnv(bot.Env, opts.ExtraEnv)

	lp, err := startLineProc(cmd)
	if err != nil {
		return nil, err
	}
	ap := &acpProc{
		lineProc:       lp,
		workspace:      bot.Workspace,
		availableModes: make(map[string]bool),
		activePerms:    make(map[string]context.CancelFunc),
	}

	if err := ap.initialize(ctx); err != nil {
		ap.kill()
		return nil, err
	}
	sid, modes, err := ap.newSession(ctx, bot.Workspace, opts, func(msg rpcMessage) {
		h.captureUpdateCommands(bot.ID, msg)
	})
	if err != nil {
		ap.kill()
		return nil, err
	}
	ap.sessionID = sid
	ap.availableModes = modes
	return ap, nil
}

func (h *ACPHarness) dropProc(botID string, p *acpProc) {
	h.pool.drop(botID, p)
}

func (p *acpProc) setTurn(ctx context.Context, opts RunOptions) {
	p.turnMu.Lock()
	p.turnCtx = ctx
	p.turnOpts = opts
	p.turnMu.Unlock()
}

func (p *acpProc) cancelAllPerms() {
	p.permMu.Lock()
	for id, cancel := range p.activePerms {
		cancel()
		delete(p.activePerms, id)
	}
	p.permMu.Unlock()
}

func (p *acpProc) call(ctx context.Context, method string, params any, onMsg func(rpcMessage)) (json.RawMessage, error) {
	id := p.nextID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := p.send(req); err != nil {
		return nil, err
	}
	wantID := fmt.Sprintf("%d", id)
	for {
		select {
		case <-ctx.Done():
			p.cancelAllPerms()
			if method == "session/prompt" && p.sessionID != "" && p.alive() {
				_ = p.send(map[string]any{
					"jsonrpc": "2.0",
					"method":  "session/cancel",
					"params":  map[string]any{"sessionId": p.sessionID},
				})
				drainCancelAck(p.lines, wantID, 150*time.Millisecond)
			}
			return nil, ctx.Err()
		case line, ok := <-p.lines:
			if !ok {
				p.cancelAllPerms()
				return nil, fmt.Errorf("acp process exited: %s", p.exitDetail())
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			// Handle server-to-client requests (e.g. session/request_permission).
			if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
				go p.handleServerRequest(msg)
				continue
			}
			// Handle notifications.
			if msg.Method != "" {
				if onMsg != nil {
					onMsg(msg)
				}
				continue
			}
			// Match response by ID.
			gotID := strings.TrimSpace(string(msg.ID))
			if gotID == wantID {
				if msg.Error != nil {
					return nil, fmt.Errorf("acp %s error (%d): %s", method, msg.Error.Code, msg.Error.Message)
				}
				return msg.Result, nil
			}
		}
	}
}

func (p *acpProc) handleServerRequest(msg rpcMessage) {
	rawID := append(json.RawMessage(nil), msg.ID...)
	if msg.Method == "session/request_permission" {
		p.turnMu.Lock()
		turnCtx := p.turnCtx
		turnOpts := p.turnOpts
		p.turnMu.Unlock()
		if turnCtx == nil {
			turnCtx = context.Background()
		}

		_, req, acpOpts := buildACPPermissionRequest(msg.Params)
		key := strings.TrimSpace(string(rawID))
		pCtx, pCancel := context.WithCancel(turnCtx)
		p.permMu.Lock()
		if p.activePerms == nil {
			p.activePerms = make(map[string]context.CancelFunc)
		}
		if prev, ok := p.activePerms[key]; ok {
			prev()
		}
		p.activePerms[key] = pCancel
		p.permMu.Unlock()

		resp := ResolveApprovalDecision(pCtx, turnOpts, req)
		pCancel()

		p.permMu.Lock()
		delete(p.activePerms, key)
		p.permMu.Unlock()

		outcome := buildACPPermissionOutcome(resp, acpOpts)
		if p.alive() {
			_ = p.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      rawID,
				"result": map[string]any{
					"outcome": outcome,
				},
			})
		}
		return
	}
	// Unsupported server request: respond with method not found so server does not hang.
	if p.alive() {
		_ = p.send(map[string]any{
			"jsonrpc": "2.0",
			"id":      rawID,
			"error": map[string]any{
				"code":    -32601,
				"message": "method not supported",
			},
		})
	}
}

type acpPermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

func buildACPPermissionRequest(raw json.RawMessage) (string, PermissionRequest, []acpPermissionOption) {
	var params struct {
		SessionID string                `json:"sessionId"`
		Options   []acpPermissionOption `json:"options"`
		ToolCall  *struct {
			ToolCallID string         `json:"toolCallId"`
			Name       string         `json:"name"`
			ToolName   string         `json:"toolName"`
			Title      string         `json:"title"`
			Kind       string         `json:"kind"`
			Args       map[string]any `json:"args"`
			RawInput   map[string]any `json:"rawInput"`
		} `json:"toolCall"`
	}
	_ = json.Unmarshal(raw, &params)

	var toolName, title, rawKind string
	var rawInput map[string]any
	var callID string
	if params.ToolCall != nil {
		callID = params.ToolCall.ToolCallID
		toolName = strings.TrimSpace(params.ToolCall.Name)
		if toolName == "" {
			toolName = strings.TrimSpace(params.ToolCall.ToolName)
		}
		title = strings.TrimSpace(params.ToolCall.Title)
		if toolName == "" {
			toolName = title
		}
		rawKind = params.ToolCall.Kind
		if len(params.ToolCall.RawInput) > 0 {
			rawInput = params.ToolCall.RawInput
		} else if len(params.ToolCall.Args) > 0 {
			rawInput = params.ToolCall.Args
		}
	}
	if toolName == "" {
		toolName = "tool"
	}

	kind := NormalizeToolKind(rawKind, toolName)
	sumTitle, summary := SummarizeToolCall(toolName, rawInput)
	if title == "" {
		title = sumTitle
	}
	if summary == "" && title != "" && title != toolName {
		summary = title
	}

	var opts []PermissionOption
	hasAllowOnce := false
	hasAllowAlways := false
	hasReject := false
	for _, o := range params.Options {
		switch o.Kind {
		case "allow_once":
			hasAllowOnce = true
			lbl := strings.TrimSpace(o.Name)
			if lbl == "" {
				lbl = "Allow once"
			}
			opts = append(opts, PermissionOption{ID: o.OptionID, Kind: "allow_once", Label: lbl})
		case "allow_always":
			hasAllowAlways = true
			lbl := strings.TrimSpace(o.Name)
			if lbl == "" {
				lbl = "Allow for session"
			}
			opts = append(opts, PermissionOption{ID: o.OptionID, Kind: "allow_always", Label: lbl})
		case "reject_once", "reject_always":
			hasReject = true
			lbl := strings.TrimSpace(o.Name)
			if lbl == "" {
				lbl = "Deny"
			}
			opts = append(opts, PermissionOption{ID: o.OptionID, Kind: "reject", Label: lbl})
		}
	}
	if !hasAllowOnce && len(opts) == 0 {
		opts = StandardPermissionOptions(true)
	} else {
		if !hasAllowAlways {
			opts = append(opts, PermissionOption{ID: "session_allow", Kind: "allow_always", Label: "Allow for session"})
		}
		if !hasReject {
			opts = append(opts, PermissionOption{ID: "reject", Kind: "reject", Label: "Deny"})
		}
	}

	return params.SessionID, PermissionRequest{
		ID:       callID,
		ToolName: toolName,
		Kind:     kind,
		Title:    title,
		Summary:  summary,
		RawInput: rawInput,
		Options:  opts,
	}, params.Options
}

func buildACPPermissionOutcome(resp PermissionResponse, options []acpPermissionOption) map[string]any {
	if resp.Decision == "cancelled" {
		return map[string]any{"outcome": "cancelled"}
	}

	// Honour an explicit protocol optionId when valid (excluding synthetic session_allow).
	if resp.OptionID != "" && resp.OptionID != "session_allow" {
		for _, opt := range options {
			if opt.OptionID == resp.OptionID {
				return map[string]any{
					"outcome":  "selected",
					"optionId": opt.OptionID,
				}
			}
		}
	}

	findByKinds := func(kinds ...string) string {
		for _, want := range kinds {
			for _, opt := range options {
				if opt.Kind == want {
					return opt.OptionID
				}
			}
		}
		return ""
	}

	var selected string
	switch resp.Decision {
	case "allow_always":
		selected = findByKinds("allow_always", "allow_once")
		if selected == "" && len(options) > 0 {
			selected = options[0].OptionID
		}
	case "allow_once":
		selected = findByKinds("allow_once", "allow_always")
		if selected == "" && len(options) > 0 {
			selected = options[0].OptionID
		}
	default:
		selected = findByKinds("reject_once", "reject_always")
	}
	if selected == "" {
		return map[string]any{"outcome": "cancelled"}
	}
	return map[string]any{
		"outcome":  "selected",
		"optionId": selected,
	}
}

func (p *acpProc) initialize(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := p.call(initCtx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name":    config.MCPServerName,
			"version": "1.0.0",
		},
	}, nil)
	return err
}

func (p *acpProc) newSession(ctx context.Context, workspace string, opts RunOptions, onMsg func(rpcMessage)) (string, map[string]bool, error) {
	sessCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cwd := workspace
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	res, err := p.call(sessCtx, "session/new", map[string]any{
		"cwd":        cwd,
		"mcpServers": acpMCPServers(opts),
	}, onMsg)
	if err != nil {
		return "", nil, err
	}
	var out struct {
		SessionID string `json:"sessionId"`
		Modes     *struct {
			AvailableModes []struct {
				ID string `json:"id"`
			} `json:"availableModes"`
		} `json:"modes"`
	}
	_ = json.Unmarshal(res, &out)
	modes := make(map[string]bool)
	if out.Modes != nil {
		for _, m := range out.Modes.AvailableModes {
			if m.ID != "" {
				modes[m.ID] = true
			}
		}
	}
	sid := out.SessionID
	if sid == "" {
		sid = fmt.Sprintf("acp-%d", time.Now().UnixNano())
	}
	return sid, modes, nil
}

func pickACPMode(available map[string]bool, permMode string) string {
	if len(available) == 0 {
		return ""
	}
	var candidates []string
	switch permMode {
	case config.PermissionModeAuto:
		candidates = []string{"bypassPermissions", "bypass", "yolo", "auto"}
	case config.PermissionModeEdit:
		candidates = []string{"accept-edits", "acceptEdits", "edit"}
	case config.PermissionModeAsk:
		candidates = []string{"default", "ask", "manual"}
	case config.PermissionModePlan:
		candidates = []string{"plan"}
	}
	for _, c := range candidates {
		if available[c] {
			return c
		}
	}
	return ""
}

func (h *ACPHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withTurnTimeout(ctx, bot.TimeoutSec, opts)
	defer cancel()

	var (
		p   *acpProc
		err error
	)
	if opts.Ephemeral {
		p, err = h.startProc(ctx, opts)
		if err != nil {
			return nil, err
		}
		defer p.kill()
	} else {
		var release func()
		p, release, err = h.getOrStartProc(ctx, opts)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	dropProc := func() {
		if opts.Ephemeral {
			p.kill()
		} else {
			h.dropProc(bot.ID, p)
		}
	}

	mode := EffectivePermissionMode(opts)
	p.setTurn(ctx, opts)
	defer func() {
		p.cancelAllPerms()
		p.setTurn(context.Background(), RunOptions{})
	}()

	if targetMode := pickACPMode(p.availableModes, mode); targetMode != "" && p.appliedMode != targetMode {
		mCtx, mCancel := context.WithTimeout(ctx, 5*time.Second)
		_, mErr := p.call(mCtx, "session/set_mode", map[string]any{
			"sessionId": p.sessionID,
			"modeId":    targetMode,
		}, nil)
		mCancel()
		if mErr == nil {
			p.appliedMode = targetMode
		}
	}

	promptText := AppendAttachmentContext(opts.Prompt, opts.Attachments)
	var textBuf strings.Builder
	var tools []string

	_, err = p.call(ctx, "session/prompt", map[string]any{
		"sessionId": p.sessionID,
		"prompt": []map[string]any{
			{
				"type": "text",
				"text": promptText,
			},
		},
	}, func(msg rpcMessage) {
		if msg.Method != "session/update" {
			return
		}
		var params struct {
			Update struct {
				SessionUpdate     string          `json:"sessionUpdate"`
				Title             string          `json:"title"`
				Kind              string          `json:"kind"`
				ToolName          string          `json:"toolName"`
				Content           json.RawMessage `json:"content"`
				AvailableCommands []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Input       *struct {
						Hint string `json:"hint"`
					} `json:"input"`
				} `json:"availableCommands"`
			} `json:"update"`
		}
		if json.Unmarshal(msg.Params, &params) != nil {
			return
		}
		switch params.Update.SessionUpdate {
		case "available_commands_update":
			h.handleAvailableCommands(bot.ID, params.Update.AvailableCommands)
		case "agent_message_chunk":
			chunk := extractACPChunkText(params.Update.Content)
			if chunk != "" {
				textBuf.WriteString(chunk)
				if opts.OnText != nil {
					opts.OnText(chunk)
				}
			}
		case "tool_call", "tool_call_update":
			toolName := strings.TrimSpace(params.Update.Title)
			if toolName == "" {
				toolName = strings.TrimSpace(params.Update.ToolName)
			}
			if toolName == "" {
				toolName = strings.TrimSpace(params.Update.Kind)
			}
			if toolName != "" {
				tools = append(tools, toolName)
				if opts.OnToolUse != nil {
					opts.OnToolUse(toolName)
				}
			}
		}
	})
	if err != nil {
		partial := &TurnResult{
			BotID:     opts.Bot.ID,
			SessionID: p.sessionID,
			Text:      strings.TrimSpace(textBuf.String()),
			Tools:     tools,
		}
		dropProc()
		if ctx.Err() != nil {
			return partial, ctxErr(ctx, opts.Bot.ID, opts.Bot.TimeoutSec)
		}
		return partial, fmt.Errorf("%s failed for bot %q: %w", h.opts.Kind, opts.Bot.ID, err)
	}

	res := finish(&TurnResult{
		BotID:     opts.Bot.ID,
		SessionID: p.sessionID,
	}, strings.TrimSpace(textBuf.String()), Usage{}, started)
	res.Tools = tools
	return res, nil
}

func drainCancelAck(lines <-chan []byte, wantID string, maxWait time.Duration) {
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			if msg.Method == "" && strings.TrimSpace(string(msg.ID)) == wantID {
				return
			}
		}
	}
}

func extractACPChunkText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var single struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &single) == nil && single.Text != "" {
		return single.Text
	}
	var list []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &list) == nil {
		var sb strings.Builder
		for _, item := range list {
			if item.Text != "" {
				sb.WriteString(item.Text)
			}
		}
		return sb.String()
	}
	return ""
}

func (h *ACPHarness) captureUpdateCommands(botID string, msg rpcMessage) {
	if msg.Method != "session/update" {
		return
	}
	var params struct {
		Update struct {
			SessionUpdate     string `json:"sessionUpdate"`
			AvailableCommands []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Input       *struct {
					Hint string `json:"hint"`
				} `json:"input"`
			} `json:"availableCommands"`
		} `json:"update"`
	}
	if json.Unmarshal(msg.Params, &params) == nil && params.Update.SessionUpdate == "available_commands_update" {
		h.handleAvailableCommands(botID, params.Update.AvailableCommands)
	}
}

var _ Harness = (*ACPHarness)(nil)
