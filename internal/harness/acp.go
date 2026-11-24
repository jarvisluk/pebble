package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// ACPOptions configures a generic ACP (Agent Client Protocol) stdio harness.
type ACPOptions struct {
	Kind          string
	Label         string
	Binaries      []string
	Args          []string
	EnvPrefix     string
	DefaultModels []string
}

// ACPHarness runs an Agent Client Protocol (JSON-RPC 2.0 over stdio) agent CLI.
type ACPHarness struct {
	opts ACPOptions
	pool procPool[*acpProc]
}

type acpProc struct {
	*lineProc
	workspace   string
	sessionID   string
	nextID      atomic.Int64
	autoApprove atomic.Bool
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
		DefaultModels: []string{"grok-4", "grok-code-fast-1"},
	})
}

func NewACPHarness(opts ACPOptions) *ACPHarness {
	return &ACPHarness{opts: opts}
}

func (h *ACPHarness) Kind() string  { return h.opts.Kind }
func (h *ACPHarness) Label() string { return h.opts.Label }

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
		lineProc:  lp,
		workspace: bot.Workspace,
	}

	if err := ap.initialize(ctx); err != nil {
		ap.kill()
		return nil, err
	}
	sid, err := ap.newSession(ctx, bot.Workspace, opts)
	if err != nil {
		ap.kill()
		return nil, err
	}
	ap.sessionID = sid
	return ap, nil
}

func (h *ACPHarness) dropProc(botID string, p *acpProc) {
	h.pool.drop(botID, p)
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
			return nil, ctx.Err()
		case line, ok := <-p.lines:
			if !ok {
				return nil, fmt.Errorf("acp process exited: %s", p.exitDetail())
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			// Handle server-to-client requests (e.g. session/request_permission).
			if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
				p.handleServerRequest(msg)
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
	if msg.Method == "session/request_permission" {
		var req struct {
			Options []struct {
				OptionID string `json:"optionId"`
				Kind     string `json:"kind"`
			} `json:"options"`
		}
		_ = json.Unmarshal(msg.Params, &req)
		allow := p.autoApprove.Load()
		selected := ""
		for _, opt := range req.Options {
			if allow && (opt.Kind == "allow_once" || opt.Kind == "allow_always") {
				selected = opt.OptionID
				break
			}
			if !allow && (opt.Kind == "reject_once" || opt.Kind == "reject_always") {
				selected = opt.OptionID
				break
			}
		}
		if allow && selected == "" && len(req.Options) > 0 {
			selected = req.Options[0].OptionID
		}
		outcome := map[string]any{"outcome": "cancelled"}
		if selected != "" {
			outcome = map[string]any{
				"outcome":  "selected",
				"optionId": selected,
			}
		}
		_ = p.send(map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(msg.ID),
			"result": map[string]any{
				"outcome": outcome,
			},
		})
		return
	}
	// Unsupported server request: respond with method not found so server does not hang.
	_ = p.send(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(msg.ID),
		"error": map[string]any{
			"code":    -32601,
			"message": "method not supported",
		},
	})
}

func (p *acpProc) initialize(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := p.call(initCtx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name":    "bot-hub",
			"version": "1.0.0",
		},
	}, nil)
	return err
}

func (p *acpProc) newSession(ctx context.Context, workspace string, opts RunOptions) (string, error) {
	sessCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cwd := workspace
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	res, err := p.call(sessCtx, "session/new", map[string]any{
		"cwd":        cwd,
		"mcpServers": acpMCPServers(opts),
	}, nil)
	if err != nil {
		return "", err
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(res, &out) == nil && out.SessionID != "" {
		return out.SessionID, nil
	}
	return fmt.Sprintf("acp-%d", time.Now().UnixNano()), nil
}

func (h *ACPHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withBotTimeout(ctx, bot.TimeoutSec)
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
	p.autoApprove.Store(bot.ShouldAutoApprove())

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
				SessionUpdate string `json:"sessionUpdate"`
				Title         string `json:"title"`
				Kind          string `json:"kind"`
				ToolName      string `json:"toolName"`
				Content       json.RawMessage `json:"content"`
			} `json:"update"`
		}
		if json.Unmarshal(msg.Params, &params) != nil {
			return
		}
		switch params.Update.SessionUpdate {
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

var _ Harness = (*ACPHarness)(nil)
