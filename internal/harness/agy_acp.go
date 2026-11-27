package harness

import (
	"bot-hub/internal/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type agyACPProc struct {
	*lineProc
	binary         string
	workspace      string
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

func (h *AgyHarness) acpProcFor(ctx context.Context, bin string, opts RunOptions, skipPerms bool) (*agyACPProc, func(), error) {
	bot := opts.Bot
	return h.acpPool.acquire(
		bot.ID,
		func(p *agyACPProc) bool {
			return p.binary == bin &&
				p.workspace == bot.Workspace &&
				p.skipPerms == skipPerms &&
				p.agent == bot.Agent &&
				p.sessionID == opts.SessionID
		},
		func() (*agyACPProc, error) {
			return h.startACPProc(ctx, bin, opts, skipPerms)
		},
	)
}

func (h *AgyHarness) startACPProc(ctx context.Context, bin string, opts RunOptions, skipPerms bool) (*agyACPProc, error) {
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

	cmd := exec.Command(bin, args...)
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
	skipPerms := mode == config.PermissionModeAuto

	var p *agyACPProc
	if opts.Ephemeral {
		p, err = h.startACPProc(ctx, bin, opts, skipPerms)
		if err != nil {
			if errors.Is(err, errLegacyStreamJSON) {
				return nil, true, nil
			}
			return nil, false, err
		}
		defer p.kill()
	} else {
		var release func()
		p, release, err = h.acpProcFor(ctx, bin, opts, skipPerms)
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
