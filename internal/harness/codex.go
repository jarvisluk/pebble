package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"bot-hub/internal/config"
)

var errCodexAppServerInit = errors.New("codex app-server initialize failed")

// CodexHarness runs OpenAI Codex CLI. It prefers `codex app-server --stdio` for
// interactive tool approvals and per-turn permission/sandbox policies, and falls
// back cleanly to `codex exec --json` on older binaries.
type CodexHarness struct {
	binary string

	pool               procPool[*codexAppProc]
	probeMu            sync.Mutex
	appServerSupported map[string]bool
}

type codexAppProc struct {
	*lineProc
	binary      string
	workspace   string
	threadID    string
	model       string
	nextID      atomic.Int64
	turnMu      sync.Mutex
	turnCtx     context.Context
	turnOpts    RunOptions
	permMu      sync.Mutex
	activePerms map[string]context.CancelFunc
}

func NewCodexHarness() *CodexHarness {
	return &CodexHarness{
		binary:             envOr("CODEX_BIN", "codex"),
		appServerSupported: make(map[string]bool),
	}
}

func (h *CodexHarness) effectiveBinary() string {
	bin := strings.TrimSpace(h.binary)
	if bin == "" {
		bin = envOr("CODEX_BIN", "codex")
	}
	if bin == "codex" {
		if resolved, ok := findBinary("codex"); ok {
			return resolved
		}
	}
	return bin
}

func (h *CodexHarness) Kind() string  { return "codex" }
func (h *CodexHarness) Label() string { return "Codex" }

func (h *CodexHarness) PermissionCapabilities() PermissionCapabilities {
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

func (h *CodexHarness) Models(context.Context) ([]string, error) {
	return []string{"gpt-5.4", "gpt-5.3-codex", "gpt-5.2-codex", "o3"}, nil
}

func (h *CodexHarness) Close() {
	h.pool.close()
}

// Reload terminates cached codex app-server processes and clears capability
// probes so updated configuration applies on the next turn.
func (h *CodexHarness) Reload() {
	h.pool.clear()
	h.probeMu.Lock()
	h.appServerSupported = make(map[string]bool)
	h.probeMu.Unlock()
}

func (h *CodexHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "Compact current session context", Source: "harness"},
		{Name: "new", Description: "Start a new session", Source: "harness"},
		{Name: "clear", Description: "Clear session history", Source: "harness"},
		{Name: "review", Description: "Review current code changes", Source: "harness"},
		{Name: "plan", Description: "Enter plan mode", Source: "harness"},
		{Name: "model", Description: "View or switch model", Source: "harness"},
		{Name: "diff", Description: "View workspace diff", Source: "harness"},
		{Name: "mention", Description: "Mention files or directories", Source: "harness"},
		{Name: "mcp", Description: "Show MCP server status", Source: "harness"},
		{Name: "init", Description: "Initialize project AGENTS.md", Source: "harness"},
		{Name: "status", Description: "View current session status", Source: "harness"},
		{Name: "help", Description: "Show available commands", Source: "harness"},
	}
	var local []SlashCommand
	if bot.Workspace != "" {
		local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, ".codex", "prompts"), "harness")...)
		local = append(local, scanSkillDirs(filepath.Join(bot.Workspace, ".codex", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanMarkdownCommands(filepath.Join(home, ".codex", "prompts"), "harness")...)
		local = append(local, scanSkillDirs(filepath.Join(home, ".codex", "skills"))...)
	}
	local = append(local, scanSharedSkills(bot.Workspace)...)
	return mergeSlashCommands(builtins, local), nil
}

func (h *CodexHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".codex", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".codex", "agents"))...)
	}
	return mergeAgents(local), nil
}

func (h *CodexHarness) supportsAppServer(bin string) bool {
	h.probeMu.Lock()
	if h.appServerSupported != nil {
		if ok, cached := h.appServerSupported[bin]; cached {
			h.probeMu.Unlock()
			return ok
		}
	}
	h.probeMu.Unlock()

	pctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(pctx, bin, "app-server", "--help").CombinedOutput()
	text := string(out)
	supported := err == nil && (strings.Contains(text, "--stdio") || strings.Contains(text, "Usage: codex app-server"))

	h.setAppServerSupported(bin, supported)
	return supported
}

func (h *CodexHarness) setAppServerSupported(bin string, supported bool) {
	h.probeMu.Lock()
	if h.appServerSupported == nil {
		h.appServerSupported = make(map[string]bool)
	}
	h.appServerSupported[bin] = supported
	h.probeMu.Unlock()
}

func codexPolicies(mode string) (approvalPolicy, sandboxMode, sandboxPolicyType string) {
	switch mode {
	case config.PermissionModeEdit:
		return "on-request", "workspace-write", "workspaceWrite"
	case config.PermissionModeAsk:
		return "untrusted", "workspace-write", "workspaceWrite"
	case config.PermissionModePlan:
		return "never", "read-only", "readOnly"
	default:
		return "never", "danger-full-access", "dangerFullAccess"
	}
}

type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
	Item *struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Text    string `json:"text"`
		Command string `json:"command"`
		Tool    string `json:"tool"`
	} `json:"item"`
	Usage *struct {
		InputTokens       int `json:"input_tokens"`
		OutputTokens      int `json:"output_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
	} `json:"usage"`
}

func (h *CodexHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withTurnTimeout(ctx, bot.TimeoutSec, opts)
	defer cancel()

	bin := h.effectiveBinary()
	if h.supportsAppServer(bin) {
		res, fallback, err := h.runAppServerTurn(ctx, bin, opts, started)
		if !fallback {
			return res, err
		}
	}
	return h.runExecTurn(ctx, bin, opts, started)
}

func (h *CodexHarness) appProcFor(ctx context.Context, bin string, opts RunOptions) (*codexAppProc, func(), error) {
	bot := opts.Bot
	return h.pool.acquire(
		bot.ID,
		func(p *codexAppProc) bool {
			return p.binary == bin &&
				p.workspace == bot.Workspace &&
				p.threadID == opts.SessionID
		},
		func() (*codexAppProc, error) {
			return h.startAppServerProc(ctx, bin, opts, false)
		},
	)
}

func (h *CodexHarness) startAppServerProc(ctx context.Context, bin string, opts RunOptions, ephemeral bool) (*codexAppProc, error) {
	bot := opts.Bot
	cmd := exec.Command(bin, "app-server", "--stdio")
	if bot.Workspace != "" {
		cmd.Dir = bot.Workspace
	}
	cmd.Env = mergedEnv(bot.Env, opts.ExtraEnv)

	lp, err := startLineProc(cmd)
	if err != nil {
		return nil, err
	}
	ap := &codexAppProc{
		lineProc:    lp,
		binary:      bin,
		workspace:   bot.Workspace,
		model:       strings.TrimSpace(bot.Model),
		activePerms: make(map[string]context.CancelFunc),
	}

	if err := ap.initialize(ctx); err != nil {
		ap.kill()
		h.setAppServerSupported(bin, false)
		return nil, fmt.Errorf("%w: %v", errCodexAppServerInit, err)
	}
	if err := ap.openThread(ctx, opts, ephemeral); err != nil {
		ap.kill()
		return nil, err
	}
	return ap, nil
}

func (p *codexAppProc) setTurn(ctx context.Context, opts RunOptions) {
	p.turnMu.Lock()
	p.turnCtx = ctx
	p.turnOpts = opts
	p.turnMu.Unlock()
}

func (p *codexAppProc) cancelAllPerms() {
	p.permMu.Lock()
	for id, cancel := range p.activePerms {
		cancel()
		delete(p.activePerms, id)
	}
	p.permMu.Unlock()
}

func (p *codexAppProc) initialize(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := p.call(initCtx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "pebble",
			"version": "1.0.0",
		},
	}, nil)
	if err != nil {
		return err
	}
	return p.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]any{},
	})
}

func (p *codexAppProc) openThread(ctx context.Context, opts RunOptions, ephemeral bool) error {
	sessCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	bot := opts.Bot
	mode := EffectivePermissionMode(opts)
	approvalPolicy, sandboxMode, _ := codexPolicies(mode)

	cwd := bot.Workspace
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	model := strings.TrimSpace(bot.Model)
	if model == "default" {
		model = ""
	}

	var res json.RawMessage
	var err error
	if !ephemeral && opts.SessionID != "" {
		params := map[string]any{
			"threadId":       opts.SessionID,
			"cwd":            cwd,
			"approvalPolicy": approvalPolicy,
			"sandbox":        sandboxMode,
		}
		if model != "" {
			params["model"] = model
		}
		res, err = p.call(sessCtx, "thread/resume", params, nil)
		if err != nil {
			log.Printf("[codex] thread/resume %s failed, starting new thread: %v", opts.SessionID, err)
			res = nil
		} else {
			p.threadID = opts.SessionID
		}
	}
	if res == nil {
		params := map[string]any{
			"cwd":            cwd,
			"approvalPolicy": approvalPolicy,
			"sandbox":        sandboxMode,
			"ephemeral":      ephemeral,
		}
		if model != "" {
			params["model"] = model
		}
		res, err = p.call(sessCtx, "thread/start", params, nil)
		if err != nil {
			return fmt.Errorf("codex thread/start: %w", err)
		}
	}

	var out struct {
		Thread *struct {
			ID string `json:"id"`
		} `json:"thread"`
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(res, &out)
	if out.Thread != nil && out.Thread.ID != "" {
		p.threadID = out.Thread.ID
	} else if out.ThreadID != "" {
		p.threadID = out.ThreadID
	}
	if p.threadID == "" {
		p.threadID = fmt.Sprintf("codex-%d", time.Now().UnixNano())
	}
	return nil
}

func (p *codexAppProc) call(ctx context.Context, method string, params any, onNotify func(rpcMessage)) (json.RawMessage, error) {
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
	wantID := strconv.FormatInt(id, 10)
	for {
		select {
		case <-ctx.Done():
			p.cancelAllPerms()
			return nil, ctx.Err()
		case line, ok := <-p.lines:
			if !ok {
				p.cancelAllPerms()
				return nil, fmt.Errorf("codex app-server exited: %s", p.exitDetail())
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			// Server-to-client request.
			if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
				go p.handleServerRequest(msg)
				continue
			}
			// Notification.
			if msg.Method != "" {
				if onNotify != nil {
					onNotify(msg)
				}
				continue
			}
			// Match response ID (quotes stripped if numeric/string).
			gotID := strings.TrimSpace(string(msg.ID))
			if gotID == wantID {
				if msg.Error != nil {
					return nil, fmt.Errorf("codex %s error (%d): %s", method, msg.Error.Code, msg.Error.Message)
				}
				return msg.Result, nil
			}
		}
	}
}

func (p *codexAppProc) sendInterrupt(turnID string) {
	if !p.alive() || p.threadID == "" || turnID == "" {
		return
	}
	_ = p.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      p.nextID.Add(1),
		"method":  "turn/interrupt",
		"params": map[string]any{
			"threadId": p.threadID,
			"turnId":   turnID,
		},
	})
}

func (p *codexAppProc) drainBrieflyOnInterrupt(onNotify func(rpcMessage), isDone func() bool) {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	for !isDone() {
		select {
		case <-timer.C:
			return
		case line, ok := <-p.lines:
			if !ok {
				return
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			if msg.Method != "" && (len(msg.ID) == 0 || string(msg.ID) == "null") {
				if onNotify != nil {
					onNotify(msg)
				}
			}
		}
	}
}

func (p *codexAppProc) handleServerRequest(msg rpcMessage) {
	rawID := append(json.RawMessage(nil), msg.ID...)
	p.turnMu.Lock()
	turnCtx := p.turnCtx
	turnOpts := p.turnOpts
	p.turnMu.Unlock()
	if turnCtx == nil {
		turnCtx = context.Background()
	}

	reply := func(result any) {
		if p.alive() {
			_ = p.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      rawID,
				"result":  result,
			})
		}
	}

	resolvePerm := func(req PermissionRequest) PermissionResponse {
		key := strings.TrimSpace(string(rawID))
		pCtx, pCancel := context.WithCancel(turnCtx)
		p.permMu.Lock()
		if p.activePerms == nil {
			p.activePerms = make(map[string]context.CancelFunc)
		}
		if prev, exists := p.activePerms[key]; exists {
			prev()
		}
		p.activePerms[key] = pCancel
		p.permMu.Unlock()

		resp := ResolveApprovalDecision(pCtx, turnOpts, req)
		pCancel()

		p.permMu.Lock()
		delete(p.activePerms, key)
		p.permMu.Unlock()
		return resp
	}

	switch msg.Method {
	case "item/commandExecution/requestApproval":
		var params struct {
			ItemID  string `json:"itemId"`
			Command string `json:"command"`
			Cwd     string `json:"cwd"`
			Reason  string `json:"reason"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		summary := strings.TrimSpace(params.Command)
		if summary == "" {
			summary = strings.TrimSpace(params.Reason)
		}
		rawInput := map[string]any{}
		if params.Command != "" {
			rawInput["command"] = params.Command
		}
		if params.Cwd != "" {
			rawInput["cwd"] = params.Cwd
		}
		if params.Reason != "" {
			rawInput["reason"] = params.Reason
		}
		resp := resolvePerm(PermissionRequest{
			ID:       params.ItemID,
			ToolName: "command_execution",
			Kind:     "shell",
			Title:    "command_execution",
			Summary:  truncatePermString(summary, 1000),
			RawInput: rawInput,
			Options:  StandardPermissionOptions(true),
		})
		reply(map[string]any{"decision": codexApprovalDecision(resp.Decision)})

	case "item/fileChange/requestApproval":
		var params struct {
			ItemID    string `json:"itemId"`
			GrantRoot string `json:"grantRoot"`
			Reason    string `json:"reason"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		summary := strings.TrimSpace(params.Reason)
		if summary == "" {
			summary = strings.TrimSpace(params.GrantRoot)
		}
		rawInput := map[string]any{}
		if params.ItemID != "" {
			rawInput["itemId"] = params.ItemID
		}
		if params.GrantRoot != "" {
			rawInput["grantRoot"] = params.GrantRoot
		}
		if params.Reason != "" {
			rawInput["reason"] = params.Reason
		}
		resp := resolvePerm(PermissionRequest{
			ID:       params.ItemID,
			ToolName: "file_change",
			Kind:     "write",
			Title:    "file_change",
			Summary:  truncatePermString(summary, 1000),
			RawInput: rawInput,
			Options:  StandardPermissionOptions(true),
		})
		reply(map[string]any{"decision": codexApprovalDecision(resp.Decision)})

	case "item/permissions/requestApproval":
		var params struct {
			ItemID      string         `json:"itemId"`
			Cwd         string         `json:"cwd"`
			Reason      string         `json:"reason"`
			Permissions map[string]any `json:"permissions"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		rawInput := map[string]any{}
		if params.Cwd != "" {
			rawInput["cwd"] = params.Cwd
		}
		if params.Reason != "" {
			rawInput["reason"] = params.Reason
		}
		if len(params.Permissions) > 0 {
			rawInput["permissions"] = params.Permissions
		}
		summary := strings.TrimSpace(params.Reason)
		if summary == "" {
			summary = strings.TrimSpace(params.Cwd)
		}
		resp := resolvePerm(PermissionRequest{
			ID:       params.ItemID,
			ToolName: "permissions",
			Kind:     "other",
			Title:    "permissions",
			Summary:  truncatePermString(summary, 1000),
			RawInput: rawInput,
			Options:  StandardPermissionOptions(true),
		})
		switch resp.Decision {
		case "allow_always":
			granted := params.Permissions
			if granted == nil {
				granted = map[string]any{}
			}
			reply(map[string]any{"permissions": granted, "scope": "session"})
		case "allow_once":
			granted := params.Permissions
			if granted == nil {
				granted = map[string]any{}
			}
			reply(map[string]any{"permissions": granted, "scope": "turn"})
		default:
			reply(map[string]any{"permissions": map[string]any{}, "scope": "turn"})
		}

	case "item/tool/requestUserInput":
		var params struct {
			Questions []struct {
				ID       string `json:"id"`
				Header   string `json:"header"`
				Question string `json:"question"`
				IsOther  bool   `json:"isOther"`
				Options  []struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				} `json:"options"`
			} `json:"questions"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if turnOpts.OnQuestion == nil || len(params.Questions) == 0 {
			reply(map[string]any{"answers": map[string]any{}})
			return
		}
		fields := make([]QuestionField, 0, len(params.Questions))
		for _, q := range params.Questions {
			var qOpts []QuestionOption
			for _, o := range q.Options {
				qOpts = append(qOpts, QuestionOption{
					Value:       o.Label,
					Label:       o.Label,
					Description: o.Description,
				})
			}
			fType := "text"
			if len(qOpts) > 0 {
				fType = "single"
			}
			title := strings.TrimSpace(q.Header)
			desc := strings.TrimSpace(q.Question)
			if title == "" {
				title = desc
				desc = ""
			}
			fields = append(fields, QuestionField{
				Key:         q.ID,
				Title:       title,
				Description: desc,
				Type:        fType,
				Required:    true,
				Options:     qOpts,
				Custom:      q.IsOther,
			})
		}
		resp, err := turnOpts.OnQuestion(turnCtx, QuestionRequest{
			ID:     strings.TrimSpace(string(rawID)),
			Fields: fields,
		})
		if err != nil || resp.Cancelled {
			reply(map[string]any{"answers": map[string]any{}})
			return
		}
		outAnswers := make(map[string]any, len(params.Questions))
		for _, q := range params.Questions {
			vals := toStringList(resp.Answers[q.ID])
			outAnswers[q.ID] = map[string]any{"answers": vals}
		}
		reply(map[string]any{"answers": outAnswers})

	default:
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
}

func codexApprovalDecision(decision string) string {
	switch decision {
	case "allow_once":
		return "accept"
	case "allow_always":
		return "acceptForSession"
	case "cancelled":
		return "cancel"
	default:
		return "decline"
	}
}

func toStringList(v any) []string {
	switch x := v.(type) {
	case nil:
		return []string{}
	case string:
		if strings.TrimSpace(x) == "" {
			return []string{}
		}
		return []string{x}
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(fmt.Sprint(x)); s != "" {
			return []string{s}
		}
		return []string{}
	}
}

func (h *CodexHarness) runAppServerTurn(ctx context.Context, bin string, opts RunOptions, started time.Time) (res *TurnResult, fallback bool, err error) {
	bot := opts.Bot
	var p *codexAppProc
	if opts.Ephemeral {
		p, err = h.startAppServerProc(ctx, bin, opts, true)
		if err != nil {
			if errors.Is(err, errCodexAppServerInit) {
				return nil, true, nil
			}
			return nil, false, err
		}
		defer p.kill()
	} else {
		var release func()
		p, release, err = h.appProcFor(ctx, bin, opts)
		if err != nil {
			if errors.Is(err, errCodexAppServerInit) {
				return nil, true, nil
			}
			return nil, false, err
		}
		defer release()
	}

	dropProc := func() {
		if opts.Ephemeral {
			p.kill()
		} else {
			h.pool.drop(bot.ID, p)
		}
	}

	p.setTurn(ctx, opts)
	defer func() {
		p.cancelAllPerms()
		p.setTurn(context.Background(), RunOptions{})
	}()

	mode := EffectivePermissionMode(opts)
	approvalPolicy, _, sandboxPolicyType := codexPolicies(mode)

	cwd := bot.Workspace
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	inputItems := []map[string]any{
		{
			"type": "text",
			"text": AppendAttachmentContext(opts.Prompt, opts.Attachments),
		},
	}
	for _, att := range opts.Attachments {
		if att.Kind == "image" && att.Path != "" {
			inputItems = append(inputItems, map[string]any{
				"type": "localImage",
				"path": att.Path,
			})
		}
	}

	turnStartParams := map[string]any{
		"threadId":       p.threadID,
		"input":          inputItems,
		"cwd":            cwd,
		"approvalPolicy": approvalPolicy,
		"sandboxPolicy":  map[string]any{"type": sandboxPolicyType},
	}
	if m := strings.TrimSpace(bot.Model); m != "" && m != "default" {
		turnStartParams["model"] = m
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: p.threadID}
	var (
		textBuf     strings.Builder
		fallbackMsg string
		tools       []string
		usage       Usage
		turnID      string
		turnDone    bool
		turnFailed  string
		interrupted bool
	)

	onNotify := func(msg rpcMessage) {
		switch msg.Method {
		case "item/agentMessage/delta":
			var d struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal(msg.Params, &d) == nil && d.Delta != "" {
				textBuf.WriteString(d.Delta)
				if opts.OnText != nil {
					opts.OnText(d.Delta)
				}
			}
		case "item/started":
			var pld struct {
				Item *struct {
					Type string `json:"type"`
					Tool string `json:"tool"`
				} `json:"item"`
			}
			if json.Unmarshal(msg.Params, &pld) == nil && pld.Item != nil {
				switch pld.Item.Type {
				case "commandExecution":
					tools = append(tools, "shell")
					if opts.OnToolUse != nil {
						opts.OnToolUse("shell")
					}
				case "fileChange":
					tools = append(tools, "file_change")
					if opts.OnToolUse != nil {
						opts.OnToolUse("file_change")
					}
				case "mcpToolCall":
					name := strings.TrimSpace(pld.Item.Tool)
					if name == "" {
						name = "mcp"
					}
					tools = append(tools, name)
					if opts.OnToolUse != nil {
						opts.OnToolUse(name)
					}
				}
			}
		case "item/completed":
			var pld struct {
				Item *struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(msg.Params, &pld) == nil && pld.Item != nil && pld.Item.Type == "agentMessage" {
				if t := strings.TrimSpace(pld.Item.Text); t != "" {
					fallbackMsg = t
				}
			}
		case "thread/tokenUsage/updated":
			var tu struct {
				TokenUsage *struct {
					Last *struct {
						InputTokens       int `json:"inputTokens"`
						CachedInputTokens int `json:"cachedInputTokens"`
						OutputTokens      int `json:"outputTokens"`
					} `json:"last"`
				} `json:"tokenUsage"`
			}
			if json.Unmarshal(msg.Params, &tu) == nil && tu.TokenUsage != nil && tu.TokenUsage.Last != nil {
				usage.InputTokens = tu.TokenUsage.Last.InputTokens
				usage.OutputTokens = tu.TokenUsage.Last.OutputTokens
				usage.CacheReadTokens = tu.TokenUsage.Last.CachedInputTokens
			}
		case "turn/completed":
			var tc struct {
				Turn struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Error  *struct {
						Message string `json:"message"`
					} `json:"error"`
				} `json:"turn"`
			}
			if json.Unmarshal(msg.Params, &tc) == nil {
				if turnID == "" || tc.Turn.ID == "" || tc.Turn.ID == turnID {
					turnDone = true
					switch tc.Turn.Status {
					case "interrupted":
						interrupted = true
					case "failed":
						if tc.Turn.Error != nil && tc.Turn.Error.Message != "" {
							turnFailed = tc.Turn.Error.Message
						} else {
							turnFailed = "turn failed"
						}
					}
				}
			}
		case "error":
			var e struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(msg.Params, &e) == nil && strings.TrimSpace(e.Message) != "" {
				turnFailed = e.Message
			}
		}
	}

	rawStart, err := p.call(ctx, "turn/start", turnStartParams, onNotify)
	if err != nil {
		partial.Text = strings.TrimSpace(textBuf.String())
		partial.Tools = tools
		dropProc()
		if ctx.Err() != nil {
			return partial, false, ctxErr(ctx, bot.ID, bot.TimeoutSec)
		}
		return partial, false, fmt.Errorf("codex failed for bot %q: %w", bot.ID, err)
	}
	var startOut struct {
		Turn *struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(rawStart, &startOut)
	if startOut.Turn != nil {
		turnID = startOut.Turn.ID
	}

	for !turnDone {
		select {
		case <-ctx.Done():
			p.cancelAllPerms()
			p.sendInterrupt(turnID)
			p.drainBrieflyOnInterrupt(onNotify, func() bool { return turnDone })
			partial.Text = strings.TrimSpace(textBuf.String())
			if partial.Text == "" {
				partial.Text = fallbackMsg
			}
			partial.Tools = tools
			dropProc()
			return partial, false, ctxErr(ctx, bot.ID, bot.TimeoutSec)
		case line, ok := <-p.lines:
			if !ok {
				p.cancelAllPerms()
				partial.Text = strings.TrimSpace(textBuf.String())
				if partial.Text == "" {
					partial.Text = fallbackMsg
				}
				partial.Tools = tools
				dropProc()
				return partial, false, fmt.Errorf("codex failed for bot %q: %s", bot.ID, p.exitDetail())
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
				go p.handleServerRequest(msg)
				continue
			}
			if msg.Method != "" {
				onNotify(msg)
			}
		}
	}

	finalText := strings.TrimSpace(textBuf.String())
	if finalText == "" {
		finalText = fallbackMsg
	} else if opts.OnText == nil && fallbackMsg != "" && len(fallbackMsg) > len(finalText) {
		finalText = fallbackMsg
	}
	partial.Tools = tools
	if interrupted {
		partial.Text = finalText
		return partial, false, fmt.Errorf("bot %q interrupted", bot.ID)
	}
	if turnFailed != "" {
		partial.Text = finalText
		return partial, false, fmt.Errorf("codex failed for bot %q: %s", bot.ID, turnFailed)
	}
	res = finish(partial, finalText, usage, started)
	res.Tools = tools
	return res, false, nil
}

func codexExecModeArgs(mode string) []string {
	switch mode {
	case config.PermissionModeEdit:
		return []string{"--sandbox", "workspace-write", "--approve-for-me"}
	case config.PermissionModeAsk:
		return []string{"--sandbox", "workspace-write"}
	case config.PermissionModePlan:
		return []string{"--sandbox", "read-only"}
	default:
		return []string{"--full-auto"}
	}
}

func (h *CodexHarness) runExecTurn(ctx context.Context, bin string, opts RunOptions, started time.Time) (*TurnResult, error) {
	bot := opts.Bot
	mode := EffectivePermissionMode(opts)

	args := []string{"exec"}
	if !opts.Ephemeral && opts.SessionID != "" {
		args = append(args, "resume", opts.SessionID)
	}
	args = append(args, "--json")
	args = append(args, codexExecModeArgs(mode)...)
	if m := strings.TrimSpace(bot.Model); m != "" && m != "default" {
		args = append(args, "--model", m)
	}
	for _, att := range opts.Attachments {
		if att.Kind == "image" && att.Path != "" {
			args = append(args, "--image", att.Path)
		}
	}
	// Read prompt from stdin via "-".
	args = append(args, "-")

	cmd := exec.Command(bin, args...)
	if bot.Workspace != "" {
		cmd.Dir = bot.Workspace
	}
	cmd.Env = mergedEnv(bot.Env, opts.ExtraEnv)
	cmd.Stdin = strings.NewReader(AppendAttachmentContext(opts.Prompt, opts.Attachments))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stderr := &tailWriter{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex: %w", err)
	}

	doneWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-doneWatch:
		}
	}()

	partial := &TurnResult{BotID: bot.ID, SessionID: opts.SessionID}
	var lastText string
	var texts []string
	var tools []string
	var usage Usage
	var errMsg string

	r := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, readErr := r.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var evt codexEvent
			if json.Unmarshal(trimmed, &evt) == nil {
				if evt.ThreadID != "" {
					partial.SessionID = evt.ThreadID
				}
				switch evt.Type {
				case "item.updated", "item.completed":
					if evt.Item != nil {
						switch evt.Item.Type {
						case "agent_message":
							if t := strings.TrimSpace(evt.Item.Text); t != "" {
								if evt.Type == "item.completed" {
									texts = append(texts, t)
								}
								if strings.HasPrefix(evt.Item.Text, lastText) {
									delta := strings.TrimPrefix(evt.Item.Text, lastText)
									if delta != "" && opts.OnText != nil {
										opts.OnText(delta)
									}
								} else if opts.OnText != nil {
									opts.OnText(evt.Item.Text)
								}
								lastText = evt.Item.Text
							}
						case "command_execution":
							tools = append(tools, "shell")
							if opts.OnToolUse != nil {
								opts.OnToolUse("shell")
							}
						case "mcp_tool_call":
							name := evt.Item.Tool
							if name == "" {
								name = "mcp"
							}
							tools = append(tools, name)
							if opts.OnToolUse != nil {
								opts.OnToolUse(name)
							}
						}
					}
				case "turn.completed":
					if evt.Usage != nil {
						usage.InputTokens = evt.Usage.InputTokens
						usage.OutputTokens = evt.Usage.OutputTokens
						usage.CacheReadTokens = evt.Usage.CachedInputTokens
					}
				case "error":
					if evt.Message != "" {
						errMsg = evt.Message
					} else if evt.Error != nil && evt.Error.Message != "" {
						errMsg = evt.Error.Message
					}
				case "turn.failed":
					if evt.Error != nil && evt.Error.Message != "" {
						errMsg = evt.Error.Message
					}
				}
			}
		}
		if readErr != nil {
			break
		}
	}

	waitErr := cmd.Wait()
	close(doneWatch)

	if ctx.Err() != nil {
		partial.Text = strings.Join(texts, "\n")
		partial.Tools = tools
		return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)
	}
	if errMsg != "" {
		partial.Text = strings.Join(texts, "\n")
		partial.Tools = tools
		return partial, fmt.Errorf("codex failed for bot %q: %s", bot.ID, errMsg)
	}
	if waitErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = waitErr.Error()
		}
		partial.Text = strings.Join(texts, "\n")
		partial.Tools = tools
		return partial, fmt.Errorf("codex failed for bot %q: %s", bot.ID, detail)
	}

	finalText := strings.TrimSpace(strings.Join(texts, "\n"))
	if finalText == "" {
		finalText = strings.TrimSpace(lastText)
	}
	res := finish(partial, finalText, usage, started)
	res.Tools = tools
	return res, nil
}
