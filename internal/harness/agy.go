package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bot-hub/internal/config"
)

var errLegacyStreamJSON = errors.New("agy emitted legacy stream-json instead of acp")

// AgyHarness runs Antigravity CLI (`agy`). It prefers `agy acp` (Agent Client
// Protocol over stdio) for full per-turn permission modes and interactive tool
// approvals, and falls back to legacy `--output-format stream-json` on older
// binaries that lack ACP support.
type AgyHarness struct {
	binary string

	pool    procPool[*agyProc]
	acpPool procPool[*agyACPProc]

	modelsMu sync.Mutex
	models   []string

	acpProbeMu   sync.Mutex
	acpSupported map[string]bool

	cmdMu       sync.Mutex
	botCommands map[string][]SlashCommand
}

type agyProc struct {
	*lineProc
	model          string
	sessionID      string
	viaMagpie      bool
	permissionMode string
	prevInput      int
	prevOutput     int
	prevCacheRead  int
}

type agyACPProc struct {
	*lineProc
	binary         string
	workspace      string
	viaMagpie      bool
	skipPerms      bool
	agent          string
	sessionID      string
	nextID         atomic.Int64
	availableModes map[string]bool
	appliedMode    string
	appliedModel   string
	turnMu         sync.Mutex
	turnCtx        context.Context
	turnOpts       RunOptions
	permMu         sync.Mutex
	activePerms    map[string]context.CancelFunc
}

type agyEvent struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
	StepUpdate     *struct {
		ConversationID string `json:"conversation_id"`
		StepIndex      int    `json:"step_index"`
		State          string `json:"state"`
		StepType       string `json:"step_type"`
		ToolName       string `json:"tool_name"`
		TextDelta      string `json:"text_delta"`
	} `json:"step_update"`
	Result *struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error"`
		Usage          *struct {
			InputTokens     int `json:"input_tokens"`
			OutputTokens    int `json:"output_tokens"`
			CacheReadTokens int `json:"cache_read_tokens"`
		} `json:"usage"`
	} `json:"result"`
}

func NewAgyHarness() *AgyHarness {
	return &AgyHarness{
		binary:       resolveAgyBin(),
		acpSupported: make(map[string]bool),
		botCommands:  make(map[string][]SlashCommand),
	}
}

func resolveAgyBin() string {
	if v := strings.TrimSpace(os.Getenv("AGY_BIN")); v != "" {
		return v
	}
	if p, ok := findBinary("agy"); ok {
		return p
	}
	return "agy"
}

func (h *AgyHarness) effectiveBinary() string {
	if bin, ok := h.resolveBinary(); ok {
		return bin
	}
	if strings.TrimSpace(h.binary) != "" {
		return h.binary
	}
	return resolveAgyBin()
}

func (h *AgyHarness) resolveBinary() (string, bool) {
	return resolveBinaryFor("AGY_BIN", "agy", h.binary)
}

func (h *AgyHarness) Kind() string  { return "agy" }
func (h *AgyHarness) Label() string { return "Antigravity CLI" }

func (h *AgyHarness) PermissionCapabilities() PermissionCapabilities {
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

func (h *AgyHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "压缩当前会话上下文", Source: "harness"},
		{Name: "plan", Description: "切换计划模式", Source: "harness"},
		{Name: "clear", Description: "清空会话上下文", Source: "harness"},
		{Name: "goal", Description: "开启长程目标执行", Source: "harness"},
		{Name: "schedule", Description: "创建定时任务或提醒", Source: "harness"},
		{Name: "browser", Description: "启用浏览器交互任务", Source: "harness"},
		{Name: "grill-me", Description: "通过问答访谈对齐方案", Source: "harness"},
		{Name: "teamwork", Description: "启动多代理协作", Source: "harness"},
		{Name: "learn", Description: "沉淀本次经验到配置", Source: "harness"},
		{Name: "boost", Description: "启用深度思考与严格验证", Source: "harness"},
		{Name: "plugin", Description: "管理或创建插件", Source: "harness"},
		{Name: "automation", Description: "配置后台自动化任务", Source: "harness"},
		{Name: "help", Description: "查看可用命令帮助", Source: "harness"},
	}

	h.cmdMu.Lock()
	cached := append([]SlashCommand(nil), h.botCommands[bot.ID]...)
	h.cmdMu.Unlock()

	var local []SlashCommand
	if bot.Workspace != "" {
		local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, ".agents", "workflows"), "workflow")...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanSkillDirs(filepath.Join(home, ".gemini", "config", "skills"))...)
		local = append(local, scanSkillDirs(filepath.Join(home, ".gemini", "antigravity", "builtin", "skills"))...)
	}
	local = append(local, scanSharedSkills(bot.Workspace)...)
	return mergeSlashCommands(cached, builtins, local), nil
}

func (h *AgyHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".agents", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".gemini", "config", "agents"))...)
	}
	return mergeAgents(local), nil
}

func (h *AgyHarness) captureAvailableCommands(botID string, raw []struct {
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

// Models lists model slugs from `agy models` (cached on success).
func (h *AgyHarness) Models(ctx context.Context) ([]string, error) {
	h.modelsMu.Lock()
	defer h.modelsMu.Unlock()
	if len(h.models) > 0 {
		return append([]string(nil), h.models...), nil
	}
	bin, ok := h.resolveBinary()
	if !ok {
		return nil, nil
	}
	fctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(fctx, bin, "models")
	cmd.Env = augmentedEnv(nil)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var list []string
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.LastIndexByte(line, '\r'); i >= 0 {
			line = line[i+1:]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Fetching ") {
			continue
		}
		f := strings.Fields(trimmed)
		if len(f) == 0 || strings.Contains(f[0], ":") {
			continue
		}
		list = append(list, f[0])
	}
	if len(list) > 0 {
		h.models = list
	}
	return append([]string(nil), h.models...), nil
}

func (h *AgyHarness) Close() {
	h.pool.close()
	h.acpPool.close()
}

// Reload terminates cached agy processes and clears the cached model and
// protocol capability state so updated provider or Magpie settings apply cleanly.
func (h *AgyHarness) Reload() {
	h.pool.clear()
	h.acpPool.clear()
	h.modelsMu.Lock()
	h.models = nil
	h.modelsMu.Unlock()
	h.acpProbeMu.Lock()
	h.acpSupported = make(map[string]bool)
	h.acpProbeMu.Unlock()
}

func (h *AgyHarness) viaMagpie(ctx context.Context, model string) bool {
	if !magpieInstalled() {
		return false
	}
	st := GetMagpieStatus(ctx, "agy")
	return st.Connected && (model == "" || strings.HasPrefix(model, "magpie/"))
}

func (h *AgyHarness) supportsACP(bin string) bool {
	h.acpProbeMu.Lock()
	if h.acpSupported != nil {
		if ok, cached := h.acpSupported[bin]; cached {
			h.acpProbeMu.Unlock()
			return ok
		}
	}
	h.acpProbeMu.Unlock()

	pctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(pctx, bin, "acp", "--help").CombinedOutput()
	text := string(out)
	supported := err == nil && (strings.Contains(text, "Agent Client Protocol") || strings.Contains(text, "agy acp"))

	h.setACPSupported(bin, supported)
	return supported
}

func (h *AgyHarness) setACPSupported(bin string, supported bool) {
	h.acpProbeMu.Lock()
	if h.acpSupported == nil {
		h.acpSupported = make(map[string]bool)
	}
	h.acpSupported[bin] = supported
	h.acpProbeMu.Unlock()
}

// writeAgyWorkspaceFiles configures workspace-local .agents/mcp_config.json and
// .agents/rules/pebble.md for Antigravity CLI without touching global configs.
func writeAgyWorkspaceFiles(workspace string, opts RunOptions) error {
	agentsDir := filepath.Join(workspace, ".agents")
	if err := upsertMCPConfig(filepath.Join(agentsDir, "mcp_config.json"), opts); err != nil {
		return err
	}

	if opts.PlatformInstructionsPath != "" {
		raw, err := os.ReadFile(opts.PlatformInstructionsPath)
		if err == nil && len(raw) > 0 {
			rulesDir := filepath.Join(agentsDir, "rules")
			if err := os.MkdirAll(rulesDir, 0o755); err != nil {
				return err
			}
			const header = "---\ntrigger: always_on\ndescription: \"Pebble workspace, MCP, and session management guide\"\n---\n\n"
			content := header + strings.TrimSpace(string(raw)) + "\n"
			rulePath := filepath.Join(rulesDir, "pebble.md")
			if cur, err := os.ReadFile(rulePath); err != nil || string(cur) != content {
				if err := os.WriteFile(rulePath, []byte(content), 0o644); err != nil {
					return err
				}
			}
			_ = os.Remove(filepath.Join(rulesDir, "bot-hub.md"))
		}
	}
	return nil
}

func (h *AgyHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withTurnTimeout(ctx, bot.TimeoutSec, opts)
	defer cancel()

	bin := h.effectiveBinary()
	if h.supportsACP(bin) {
		res, fallback, err := h.runACPTurn(ctx, bin, opts, started)
		if !fallback {
			return res, err
		}
	}
	return h.runStreamTurn(ctx, bin, opts, started)
}

func (h *AgyHarness) acpProcFor(ctx context.Context, bin string, opts RunOptions, viaMagpie, skipPerms bool) (*agyACPProc, func(), error) {
	bot := opts.Bot
	return h.acpPool.acquire(
		bot.ID,
		func(p *agyACPProc) bool {
			return p.binary == bin &&
				p.workspace == bot.Workspace &&
				p.viaMagpie == viaMagpie &&
				p.skipPerms == skipPerms &&
				p.agent == bot.Agent &&
				p.sessionID == opts.SessionID
		},
		func() (*agyACPProc, error) {
			return h.startACPProc(ctx, bin, opts, viaMagpie, skipPerms)
		},
	)
}

func (h *AgyHarness) startACPProc(ctx context.Context, bin string, opts RunOptions, viaMagpie, skipPerms bool) (*agyACPProc, error) {
	bot := opts.Bot
	if err := writeAgyWorkspaceFiles(bot.Workspace, opts); err != nil {
		return nil, err
	}

	args := []string{"acp"}
	if skipPerms {
		args = append(args, "--dangerously-skip-permissions")
	}
	if strings.TrimSpace(bot.Agent) != "" {
		args = append(args, "--agent", strings.TrimSpace(bot.Agent))
	}

	var cmd *exec.Cmd
	if viaMagpie {
		runArgs := append([]string{"agy", "run", "--"}, args...)
		cmd = exec.Command(magpieBin(), runArgs...)
	} else {
		cmd = exec.Command(bin, args...)
	}
	cmd.Dir = bot.Workspace
	cmd.Env = procEnv(cmd, opts)

	lp, err := startLineProc(cmd)
	if err != nil {
		return nil, err
	}
	ap := &agyACPProc{
		lineProc:       lp,
		binary:         bin,
		workspace:      bot.Workspace,
		viaMagpie:      viaMagpie,
		skipPerms:      skipPerms,
		agent:          bot.Agent,
		availableModes: make(map[string]bool),
		activePerms:    make(map[string]context.CancelFunc),
	}

	if err := ap.initialize(ctx); err != nil {
		ap.kill()
		if errors.Is(err, errLegacyStreamJSON) {
			h.setACPSupported(bin, false)
		}
		return nil, err
	}
	sid, modes, curMode, curModel, err := ap.openSession(ctx, bot.Workspace, opts, func(msg rpcMessage) {
		h.captureACPNotification(bot.ID, msg)
	})
	if err != nil {
		ap.kill()
		return nil, err
	}
	ap.sessionID = sid
	ap.availableModes = modes
	ap.appliedMode = curMode
	ap.appliedModel = curModel
	return ap, nil
}

func (h *AgyHarness) captureACPNotification(botID string, msg rpcMessage) {
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
		h.captureAvailableCommands(botID, params.Update.AvailableCommands)
	}
}

func (p *agyACPProc) setTurn(ctx context.Context, opts RunOptions) {
	p.turnMu.Lock()
	p.turnCtx = ctx
	p.turnOpts = opts
	p.turnMu.Unlock()
}

func (p *agyACPProc) cancelAllPerms() {
	p.permMu.Lock()
	for id, cancel := range p.activePerms {
		cancel()
		delete(p.activePerms, id)
	}
	p.permMu.Unlock()
}

func (p *agyACPProc) call(ctx context.Context, method string, params any, onMsg func(rpcMessage)) (json.RawMessage, error) {
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
				return nil, fmt.Errorf("agy acp process exited: %s", p.exitDetail())
			}
			if method == "initialize" {
				var probe struct {
					Event string `json:"event"`
				}
				if json.Unmarshal(line, &probe) == nil && probe.Event != "" {
					return nil, errLegacyStreamJSON
				}
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			// Server-to-client request (string or integer ID).
			if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
				go p.handleServerRequest(msg)
				continue
			}
			if msg.Method != "" {
				if onMsg != nil {
					onMsg(msg)
				}
				continue
			}
			if strings.TrimSpace(string(msg.ID)) == wantID {
				if msg.Error != nil {
					return nil, fmt.Errorf("agy acp %s error (%d): %s", method, msg.Error.Code, msg.Error.Message)
				}
				return msg.Result, nil
			}
		}
	}
}

func (p *agyACPProc) handleServerRequest(msg rpcMessage) {
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

func (p *agyACPProc) initialize(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := p.call(initCtx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name":    "pebble",
			"version": "1.0.0",
		},
	}, nil)
	return err
}

func (p *agyACPProc) openSession(ctx context.Context, workspace string, opts RunOptions, onMsg func(rpcMessage)) (sid string, modes map[string]bool, currentMode, currentModel string, err error) {
	sessCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cwd := workspace
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	mcpServers := acpMCPServers(opts)

	var res json.RawMessage
	if !opts.Ephemeral && opts.SessionID != "" {
		res, err = p.call(sessCtx, "session/resume", map[string]any{
			"sessionId":  opts.SessionID,
			"cwd":        cwd,
			"mcpServers": mcpServers,
		}, onMsg)
		if err != nil {
			log.Printf("[agy] acp session/resume %s failed, creating new session: %v", opts.SessionID, err)
			res = nil
		} else {
			sid = opts.SessionID
		}
	}
	if res == nil {
		res, err = p.call(sessCtx, "session/new", map[string]any{
			"cwd":        cwd,
			"mcpServers": mcpServers,
		}, onMsg)
		if err != nil {
			return "", nil, "", "", err
		}
	}

	var out struct {
		SessionID string `json:"sessionId"`
		Modes     *struct {
			CurrentModeID  string `json:"currentModeId"`
			AvailableModes []struct {
				ID string `json:"id"`
			} `json:"availableModes"`
		} `json:"modes"`
		Models *struct {
			CurrentModelID string `json:"currentModelId"`
		} `json:"models"`
	}
	_ = json.Unmarshal(res, &out)
	if out.SessionID != "" {
		sid = out.SessionID
	}
	if sid == "" {
		sid = fmt.Sprintf("agy-acp-%d", time.Now().UnixNano())
	}
	modes = make(map[string]bool)
	if out.Modes != nil {
		currentMode = strings.TrimSpace(out.Modes.CurrentModeID)
		for _, m := range out.Modes.AvailableModes {
			if m.ID != "" {
				modes[m.ID] = true
			}
		}
	}
	if out.Models != nil {
		currentModel = strings.TrimSpace(out.Models.CurrentModelID)
	}
	return sid, modes, currentMode, currentModel, nil
}

func agyTargetACPMode(available map[string]bool, permMode string) string {
	if target := pickACPMode(available, permMode); target != "" {
		return target
	}
	switch permMode {
	case config.PermissionModeEdit:
		return "accept-edits"
	case config.PermissionModeAsk:
		return "default"
	case config.PermissionModePlan:
		return "plan"
	case config.PermissionModeAuto:
		return "auto"
	default:
		return ""
	}
}

func (h *AgyHarness) runACPTurn(ctx context.Context, bin string, opts RunOptions, started time.Time) (res *TurnResult, fallback bool, err error) {
	bot := opts.Bot
	mode := EffectivePermissionMode(opts)
	viaMagpie := h.viaMagpie(ctx, bot.Model)
	skipPerms := mode == config.PermissionModeAuto

	var p *agyACPProc
	if opts.Ephemeral {
		p, err = h.startACPProc(ctx, bin, opts, viaMagpie, skipPerms)
		if err != nil {
			if errors.Is(err, errLegacyStreamJSON) {
				return nil, true, nil
			}
			return nil, false, err
		}
		defer p.kill()
	} else {
		var release func()
		p, release, err = h.acpProcFor(ctx, bin, opts, viaMagpie, skipPerms)
		if err != nil {
			if errors.Is(err, errLegacyStreamJSON) {
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
			h.acpPool.drop(bot.ID, p)
		}
	}

	p.setTurn(ctx, opts)
	defer func() {
		p.cancelAllPerms()
		p.setTurn(context.Background(), RunOptions{})
	}()

	if !skipPerms {
		targetMode := agyTargetACPMode(p.availableModes, mode)
		if targetMode != "" && p.appliedMode != targetMode {
			mCtx, mCancel := context.WithTimeout(ctx, 5*time.Second)
			_, mErr := p.call(mCtx, "session/set_mode", map[string]any{
				"sessionId": p.sessionID,
				"modeId":    targetMode,
			}, nil)
			mCancel()
			if mErr != nil {
				dropProc()
				return nil, false, fmt.Errorf("agy set mode %q: %w", targetMode, mErr)
			}
			p.appliedMode = targetMode
		}
	}

	modelID := strings.TrimSpace(bot.Model)
	if modelID != "" && p.appliedModel != modelID {
		if viaMagpie && strings.HasPrefix(modelID, "magpie/") {
			p.appliedModel = modelID
		} else {
			mCtx, mCancel := context.WithTimeout(ctx, 5*time.Second)
			_, mErr := p.call(mCtx, "session/set_model", map[string]any{
				"sessionId": p.sessionID,
				"modelId":   modelID,
			}, nil)
			mCancel()
			if mErr != nil {
				cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
				_, cErr := p.call(cCtx, "session/set_config_option", map[string]any{
					"sessionId": p.sessionID,
					"configId":  "model",
					"value":     modelID,
				}, nil)
				cCancel()
				if cErr != nil {
					if !strings.Contains(mErr.Error(), "-32601") && !strings.Contains(cErr.Error(), "-32601") {
						dropProc()
						return nil, false, fmt.Errorf("agy set model %q: %w", modelID, mErr)
					}
				}
			}
			p.appliedModel = modelID
		}
	}

	var promptBlocks []map[string]any
	for _, img := range readImageAttachments(opts.Attachments) {
		promptBlocks = append(promptBlocks, map[string]any{
			"type":     "image",
			"data":     img.DataB64,
			"mimeType": img.MimeType,
		})
	}
	promptBlocks = append(promptBlocks, map[string]any{
		"type": "text",
		"text": AppendAttachmentContext(opts.Prompt, opts.Attachments),
	})

	var textBuf strings.Builder
	var tools []string
	rawRes, err := p.call(ctx, "session/prompt", map[string]any{
		"sessionId": p.sessionID,
		"prompt":    promptBlocks,
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
			h.captureAvailableCommands(bot.ID, params.Update.AvailableCommands)
		case "agent_message_chunk":
			if chunk := extractACPChunkText(params.Update.Content); chunk != "" {
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
			BotID:     bot.ID,
			SessionID: p.sessionID,
			Text:      strings.TrimSpace(textBuf.String()),
			Tools:     tools,
		}
		dropProc()
		if ctx.Err() != nil {
			return partial, false, ctxErr(ctx, bot.ID, bot.TimeoutSec)
		}
		return partial, false, fmt.Errorf("agy failed for bot %q: %w", bot.ID, err)
	}

	var promptOut struct {
		StopReason string `json:"stopReason"`
		Usage      *struct {
			InputTokens      int `json:"inputTokens"`
			OutputTokens     int `json:"outputTokens"`
			CachedReadTokens int `json:"cachedReadTokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(rawRes, &promptOut)
	if promptOut.StopReason == "cancelled" {
		partial := &TurnResult{
			BotID:     bot.ID,
			SessionID: p.sessionID,
			Text:      strings.TrimSpace(textBuf.String()),
			Tools:     tools,
		}
		return partial, false, ctxErr(ctx, bot.ID, bot.TimeoutSec)
	}
	var usage Usage
	if promptOut.Usage != nil {
		usage.InputTokens = promptOut.Usage.InputTokens
		usage.OutputTokens = promptOut.Usage.OutputTokens
		usage.CacheReadTokens = promptOut.Usage.CachedReadTokens
	}
	outRes := finish(&TurnResult{
		BotID:     bot.ID,
		SessionID: p.sessionID,
	}, strings.TrimSpace(textBuf.String()), usage, started)
	outRes.Tools = tools
	return outRes, false, nil
}

func (h *AgyHarness) drop(botID string, p *agyProc) {
	h.pool.drop(botID, p)
}

func (h *AgyHarness) proc(ctx context.Context, bin string, opts RunOptions) (*agyProc, func(), error) {
	bot := opts.Bot
	viaMagpie := h.viaMagpie(ctx, bot.Model)
	mode := EffectivePermissionMode(opts)
	return h.pool.acquire(
		bot.ID,
		func(p *agyProc) bool {
			return p.model == bot.Model &&
				p.sessionID == opts.SessionID &&
				p.viaMagpie == viaMagpie &&
				p.permissionMode == mode
		},
		func() (*agyProc, error) {
			return h.startProc(bin, opts, viaMagpie, false)
		},
	)
}

func agyStreamModeArgs(mode string) []string {
	switch mode {
	case config.PermissionModeEdit:
		return []string{"--mode", "accept-edits"}
	case config.PermissionModePlan:
		return []string{"--mode", "plan"}
	case config.PermissionModeAsk:
		return []string{"--mode", "auto"}
	default:
		return []string{"--dangerously-skip-permissions"}
	}
}

func (h *AgyHarness) startProc(bin string, opts RunOptions, viaMagpie, ephemeral bool) (*agyProc, error) {
	bot := opts.Bot
	mode := EffectivePermissionMode(opts)
	if err := writeAgyWorkspaceFiles(bot.Workspace, opts); err != nil {
		return nil, err
	}

	args := []string{"--input-format", "stream-json", "--output-format", "stream-json"}
	if bot.Model != "" {
		args = append(args, "--model", bot.Model)
	}
	if !ephemeral && opts.SessionID != "" {
		args = append(args, "--conversation", opts.SessionID)
	}
	args = append(args, agyStreamModeArgs(mode)...)

	var cmd *exec.Cmd
	if viaMagpie {
		runArgs := append([]string{"agy", "run", "--"}, args...)
		cmd = exec.Command(magpieBin(), runArgs...)
	} else {
		cmd = exec.Command(bin, args...)
	}
	cmd.Dir = bot.Workspace
	cmd.Env = procEnv(cmd, opts)

	lp, err := startLineProc(cmd)
	if err != nil {
		return nil, err
	}
	return &agyProc{
		lineProc:       lp,
		model:          bot.Model,
		sessionID:      opts.SessionID,
		viaMagpie:      viaMagpie,
		permissionMode: mode,
	}, nil
}

func (h *AgyHarness) runStreamTurn(ctx context.Context, bin string, opts RunOptions, started time.Time) (*TurnResult, error) {
	bot := opts.Bot
	var (
		p   *agyProc
		err error
	)
	if opts.Ephemeral {
		p, err = h.startProc(bin, opts, h.viaMagpie(ctx, bot.Model), true)
		if err != nil {
			return nil, err
		}
		defer p.kill()
	} else {
		var release func()
		p, release, err = h.proc(ctx, bin, opts)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	dropProc := func() {
		if opts.Ephemeral {
			p.kill()
		} else {
			h.drop(bot.ID, p)
		}
	}

	err = p.send(map[string]any{
		"event":   "user",
		"message": map[string]any{"content": AppendAttachmentContext(opts.Prompt, opts.Attachments)},
	})
	if err != nil {
		dropProc()
		return nil, fmt.Errorf("agy process: %w", err)
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: opts.SessionID}
	var textBuf strings.Builder
	seenToolSteps := make(map[int]bool)

	for {
		select {
		case <-ctx.Done():
			partial.Text = strings.TrimSpace(textBuf.String())
			dropProc()
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)

		case line, ok := <-p.lines:
			if !ok {
				partial.Text = strings.TrimSpace(textBuf.String())
				dropProc()
				return partial, fmt.Errorf("agy failed for bot %q: %s", bot.ID, p.exitDetail())
			}
			var evt agyEvent
			if json.Unmarshal(line, &evt) != nil {
				continue
			}
			if evt.ConversationID != "" {
				partial.SessionID = evt.ConversationID
				p.sessionID = evt.ConversationID
			}
			switch evt.Event {
			case "step_update":
				su := evt.StepUpdate
				if su == nil {
					continue
				}
				if su.ConversationID != "" {
					partial.SessionID = su.ConversationID
					p.sessionID = su.ConversationID
				}
				switch su.StepType {
				case "agent_response":
					if su.TextDelta != "" {
						textBuf.WriteString(su.TextDelta)
						if opts.OnText != nil {
							opts.OnText(su.TextDelta)
						}
					}
				case "tool":
					if su.ToolName != "" && !seenToolSteps[su.StepIndex] {
						seenToolSteps[su.StepIndex] = true
						if opts.OnToolUse != nil {
							opts.OnToolUse(su.ToolName)
						}
					}
				}
			case "result":
				res := evt.Result
				if res == nil {
					continue
				}
				if res.ConversationID != "" {
					partial.SessionID = res.ConversationID
					p.sessionID = res.ConversationID
				}
				if res.Status != "" && res.Status != "SUCCESS" {
					msg := strings.TrimSpace(res.Error)
					if msg == "" {
						msg = res.Status
					}
					partial.Text = strings.TrimSpace(textBuf.String())
					return partial, fmt.Errorf("agy failed for bot %q: %s", bot.ID, msg)
				}
				text := strings.TrimSpace(res.Response)
				if text == "" {
					text = strings.TrimSpace(textBuf.String())
				}
				var usage Usage
				if res.Usage != nil {
					usage.InputTokens = deltaCounter(res.Usage.InputTokens, &p.prevInput)
					usage.OutputTokens = deltaCounter(res.Usage.OutputTokens, &p.prevOutput)
					usage.CacheReadTokens = deltaCounter(res.Usage.CacheReadTokens, &p.prevCacheRead)
				}
				return finish(partial, text, usage, started), nil
			}
		}
	}
}

func deltaCounter(cum int, prev *int) int {
	if cum < *prev {
		*prev = cum
		return cum
	}
	d := cum - *prev
	*prev = cum
	return d
}
