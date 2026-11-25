package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
)

// PiHarness keeps one long-lived `pi --mode rpc` process per bot. The hub
// picks the session ID, so a restarted process reopens the same session.
type PiHarness struct {
	binary   string
	mu       sync.Mutex
	pool     procPool[*piProc]
	commands map[string][]SlashCommand

	modelsMu sync.Mutex
	models   []string
}

type piProc struct {
	*lineProc
	model     string
	sessionID string
	seq       int
}

type piEvent struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Disposition string `json:"disposition"`
		Summary     string `json:"summary"`
		Commands    []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Source      string `json:"source"`
		} `json:"commands"`
	} `json:"data"`
	Method      string          `json:"method"`
	Title       string          `json:"title"`
	Options     []string        `json:"options"`
	Placeholder string          `json:"placeholder"`
	Prefill     string          `json:"prefill"`
	ToolName    string          `json:"toolName"`
	Aborted     bool            `json:"aborted"`
	Message     json.RawMessage `json:"message"`
	AME         *struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"assistantMessageEvent"`
}

type piEventMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Stop    string          `json:"stopReason"`
	ErrMsg  string          `json:"errorMessage"`
	Usage   *struct {
		Input     int `json:"input"`
		Output    int `json:"output"`
		CacheRead int `json:"cacheRead"`
		Cost      struct {
			Total float64 `json:"total"`
		} `json:"cost"`
	} `json:"usage"`
}

var errPiCommandHandled = errors.New("prompt was handled by a pi command, no run started")

func NewPiHarness() *PiHarness {
	return &PiHarness{
		binary:   envOr("PI_BIN", "pi"),
		commands: make(map[string][]SlashCommand),
	}
}

func (h *PiHarness) Kind() string  { return "pi" }
func (h *PiHarness) Label() string { return "Pi" }

func (h *PiHarness) PermissionCapabilities() PermissionCapabilities {
	return PermissionCapabilities{
		Modes:               []string{config.PermissionModeAuto},
		InteractiveApproval: false,
		SupportsAllowAlways: false,
	}
}

func (h *PiHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "压缩当前会话上下文", Source: "harness"},
		{Name: "new", Description: "开启新会话", Source: "harness"},
		{Name: "clear", Description: "清空当前会话", Source: "harness"},
		{Name: "model", Description: "查看或切换模型", Source: "harness"},
		{Name: "thinking", Description: "调整思考强度", Source: "harness"},
		{Name: "tree", Description: "查看会话分支树", Source: "harness"},
		{Name: "fork", Description: "从当前节点分叉新会话", Source: "harness"},
		{Name: "resume", Description: "恢复历史会话", Source: "harness"},
		{Name: "export", Description: "导出当前会话", Source: "harness"},
		{Name: "session", Description: "查看当前会话信息", Source: "harness"},
		{Name: "reload", Description: "重载扩展与配置", Source: "harness"},
		{Name: "settings", Description: "查看或修改设置", Source: "harness"},
		{Name: "hotkeys", Description: "查看快捷键说明", Source: "harness"},
		{Name: "help", Description: "查看可用命令帮助", Source: "harness"},
	}
	h.mu.Lock()
	cached := append([]SlashCommand(nil), h.commands[bot.ID]...)
	h.mu.Unlock()

	var local []SlashCommand
	if bot.Workspace != "" {
		local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, ".pi", "prompts"), "harness")...)
		local = append(local, scanSkillDirs(filepath.Join(bot.Workspace, ".pi", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanMarkdownCommands(filepath.Join(home, ".pi", "agent", "prompts"), "harness")...)
		local = append(local, scanSkillDirs(filepath.Join(home, ".pi", "agent", "skills"))...)
	}
	local = append(local, scanSharedSkills(bot.Workspace)...)
	return mergeSlashCommands(cached, builtins, local), nil
}

func (h *PiHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".pi", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".pi", "agent", "agents"))...)
	}
	return mergeAgents(local), nil
}

func (h *PiHarness) resolveBinary() (string, bool) {
	return resolveBinaryFor("PI_BIN", "pi", h.binary)
}

// Models lists `pi --list-models` as provider/model IDs (cached on success).
func (h *PiHarness) Models(ctx context.Context) ([]string, error) {
	h.modelsMu.Lock()
	defer h.modelsMu.Unlock()
	if len(h.models) > 0 {
		return append([]string(nil), h.models...), nil
	}
	bin, ok := h.resolveBinary()
	if !ok {
		return nil, nil
	}
	fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(fctx, bin, "--list-models", "--offline")
	cmd.Env = augmentedEnv(nil)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var list []string
	for i, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 2 {
			continue
		}
		list = append(list, f[0]+"/"+f[1])
	}
	if len(list) > 0 {
		h.models = list
	}
	return append([]string(nil), h.models...), nil
}

func (h *PiHarness) Close() {
	h.pool.close()
}

// Reload terminates cached pi RPC processes and clears the cached model list so
// updated provider settings take effect on the next call.
func (h *PiHarness) Reload() {
	h.pool.clear()
	h.modelsMu.Lock()
	h.models = nil
	h.modelsMu.Unlock()
}

func (h *PiHarness) drop(botID string, p *piProc) {
	h.pool.drop(botID, p)
}

func (h *PiHarness) proc(opts RunOptions, sessionID string) (*piProc, func(), error) {
	bot := opts.Bot
	return h.pool.acquire(
		bot.ID,
		func(p *piProc) bool {
			return p.sessionID == sessionID
		},
		func() (*piProc, error) {
			return h.startProc(opts, sessionID, false)
		},
	)
}

func (h *PiHarness) startProc(opts RunOptions, sessionID string, ephemeral bool) (*piProc, error) {
	bot := opts.Bot
	if err := h.writePiMCPConfig(bot.Workspace, opts); err != nil {
		return nil, err
	}
	// --approve trusts the workspace's .pi/mcp.json for this run.
	args := []string{"--mode", "rpc", "--approve"}
	if ephemeral {
		args = append(args, "--no-session")
	} else {
		args = append(args, "--session-id", sessionID)
	}
	if bot.Model != "" {
		args = append(args, "--model", bot.Model)
	}
	if opts.PlatformInstructionsPath != "" {
		if info, err := os.Stat(opts.PlatformInstructionsPath); err == nil && !info.IsDir() {
			args = append(args, "--append-system-prompt", opts.PlatformInstructionsPath)
		}
	}
	cmd := exec.Command(h.binary, args...)
	cmd.Dir = bot.Workspace
	cmd.Env = append(procEnv(cmd, opts), "PWD="+bot.Workspace)
	lp, err := startLineProc(cmd)
	if err != nil {
		return nil, err
	}
	return &piProc{lineProc: lp, model: bot.Model, sessionID: sessionID}, nil
}

// writePiMCPConfig exposes the bot-hub MCP server through the workspace's
// .pi/mcp.json while preserving any user-configured servers. It may hold the
// hub API token, so it is written with 0600 permissions.
func (h *PiHarness) writePiMCPConfig(workspace string, opts RunOptions) error {
	return upsertMCPConfig(filepath.Join(workspace, ".pi", "mcp.json"), opts)
}

func (h *PiHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withBotTimeout(ctx, bot.TimeoutSec)
	defer cancel()
	var (
		sessionID string
		p         *piProc
		err       error
	)
	if opts.Ephemeral {
		p, err = h.startProc(opts, "", true)
		if err != nil {
			return nil, err
		}
		defer p.kill()
	} else {
		sessionID = opts.SessionID
		if sessionID == "" {
			sessionID = newUUID()
		}
		var release func()
		p, release, err = h.proc(opts, sessionID)
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
	partial := &TurnResult{BotID: bot.ID, SessionID: sessionID}

	if bot.Model != "" && bot.Model != p.model {
		provider, id := splitModel(bot.Model)
		if _, err := h.call(ctx, p, map[string]any{"type": "set_model", "provider": provider, "modelId": id}); err != nil {
			return partial, fmt.Errorf("pi set_model %q: %w", bot.Model, err)
		}
		p.model = bot.Model
	}

	h.mu.Lock()
	_, hasCachedCmds := h.commands[bot.ID]
	h.mu.Unlock()
	if !hasCachedCmds && !opts.Ephemeral {
		if evt, err := h.call(ctx, p, map[string]any{"type": "get_commands"}); err == nil && evt != nil {
			var list []SlashCommand
			for _, c := range evt.Data.Commands {
				src := "harness"
				if c.Source == "skill" {
					src = "skill"
				}
				list = append(list, SlashCommand{
					Name:        c.Name,
					Description: c.Description,
					Source:      src,
				})
			}
			h.mu.Lock()
			if h.commands == nil {
				h.commands = make(map[string][]SlashCommand)
			}
			h.commands[bot.ID] = list
			h.mu.Unlock()
		}
	}

	if cmdName, cmdArgs, ok := ParseSlashCommand(opts.Prompt); ok && cmdName == "compact" {
		req := map[string]any{"type": "compact"}
		if cmdArgs != "" {
			req["customInstructions"] = cmdArgs
		}
		evt, err := h.call(ctx, p, req)
		if err != nil {
			return partial, fmt.Errorf("pi compact for bot %q: %w", bot.ID, err)
		}
		text := "会话上下文已压缩。"
		if evt != nil && strings.TrimSpace(evt.Data.Summary) != "" {
			text = strings.TrimSpace(evt.Data.Summary)
		}
		return finish(partial, text, Usage{}, started), nil
	}

	promptText := AppendAttachmentContext(opts.Prompt, opts.Attachments)
	promptReq := map[string]any{"type": "prompt", "message": promptText}
	var piImages []map[string]any
	for _, img := range readImageAttachments(opts.Attachments) {
		piImages = append(piImages, map[string]any{
			"type":     "image",
			"data":     img.DataB64,
			"mimeType": img.MimeType,
		})
	}
	if len(piImages) > 0 {
		promptReq["images"] = piImages
	}

	if _, err := h.call(ctx, p, promptReq); err != nil {
		if errors.Is(err, errPiCommandHandled) {
			return finish(partial, "已执行命令。", Usage{}, started), nil
		}
		return partial, fmt.Errorf("pi failed for bot %q: %w", bot.ID, err)
	}

	var texts []string
	var usage Usage
	var lastErr string
	interrupted := false
	var abortDeadline <-chan time.Time
	var formMu sync.Mutex
	activeForms := make(map[string]context.CancelFunc)
	cancelAllForms := func() {
		formMu.Lock()
		for id, cancel := range activeForms {
			cancel()
			delete(activeForms, id)
		}
		formMu.Unlock()
	}
	defer cancelAllForms()

	for {
		select {
		case <-ctxDone(ctx, interrupted):
			interrupted = true
			cancelAllForms()
			_ = p.send(map[string]any{"type": "abort"})
			abortDeadline = time.After(10 * time.Second)
		case <-abortDeadline:
			partial.Text = strings.Join(texts, "\n")
			dropProc()
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)
		case line, ok := <-p.lines:
			if !ok {
				partial.Text = strings.Join(texts, "\n")
				dropProc()
				return partial, fmt.Errorf("pi failed for bot %q: %s", bot.ID, p.exitDetail())
			}
			var evt piEvent
			if json.Unmarshal(line, &evt) != nil {
				continue
			}
			switch evt.Type {
			case "message_update":
				if evt.AME != nil && evt.AME.Type == "text_delta" && opts.OnText != nil {
					opts.OnText(evt.AME.Delta)
				}
			case "tool_execution_start":
				if opts.OnToolUse != nil {
					opts.OnToolUse(evt.ToolName)
				}
			case "message_end":
				var m piEventMessage
				if len(evt.Message) == 0 || json.Unmarshal(evt.Message, &m) != nil || m.Role != "assistant" {
					continue
				}
				if t := strings.TrimSpace(piText(m.Content)); t != "" {
					texts = append(texts, t)
				}
				if m.Stop == "error" {
					lastErr = m.ErrMsg
				} else if m.Stop != "aborted" {
					lastErr = ""
				}
				if u := m.Usage; u != nil {
					usage.InputTokens += u.Input
					usage.OutputTokens += u.Output
					usage.CacheReadTokens += u.CacheRead
					usage.CostUSD += u.Cost.Total
				}
			case "extension_ui_request":
				switch evt.Method {
				case "select", "confirm", "input", "editor":
					if opts.OnQuestion == nil {
						_ = p.send(map[string]any{"type": "extension_ui_response", "id": evt.ID, "cancelled": true})
						continue
					}
					req, ok := piQuestionRequest(evt)
					if !ok {
						_ = p.send(map[string]any{"type": "extension_ui_response", "id": evt.ID, "cancelled": true})
						continue
					}
					qCtx, qCancel := context.WithCancel(ctx)
					reqID := evt.ID
					method := evt.Method
					formMu.Lock()
					if prev, exists := activeForms[reqID]; exists {
						prev()
					}
					activeForms[reqID] = qCancel
					formMu.Unlock()
					go func() {
						defer qCancel()
						resp, err := opts.OnQuestion(qCtx, req)
						formMu.Lock()
						delete(activeForms, reqID)
						formMu.Unlock()
						if qCtx.Err() != nil && ctx.Err() != nil {
							_ = p.send(map[string]any{"type": "extension_ui_response", "id": reqID, "cancelled": true})
							return
						}
						if err != nil || resp.Cancelled {
							_ = p.send(map[string]any{"type": "extension_ui_response", "id": reqID, "cancelled": true})
							return
						}
						val := resp.Answers["value"]
						if method == "confirm" {
							b, _ := val.(bool)
							_ = p.send(map[string]any{"type": "extension_ui_response", "id": reqID, "confirmed": b})
							return
						}
						s := ""
						if val != nil {
							s = fmt.Sprint(val)
						}
						_ = p.send(map[string]any{"type": "extension_ui_response", "id": reqID, "value": s})
					}()
				}
			case "agent_settled":
				partial.Usage = usage
				if interrupted || evt.Aborted {
					partial.Text = strings.Join(texts, "\n")
					return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)
				}
				if lastErr != "" {
					partial.Text = strings.Join(texts, "\n")
					return partial, fmt.Errorf("pi failed for bot %q: %s", bot.ID, lastErr)
				}
				return finish(partial, strings.Join(texts, "\n"), usage, started), nil
			}
		}
	}
}

// ctxDone returns ctx.Done() until the interrupt has been sent once.
func ctxDone(ctx context.Context, sent bool) <-chan struct{} {
	if sent {
		return nil
	}
	return ctx.Done()
}

// call sends one command and waits for its response, ignoring events that
// arrive meanwhile (none matter before a prompt is accepted).
func (h *PiHarness) call(ctx context.Context, p *piProc, cmd map[string]any) (*piEvent, error) {
	p.seq++
	id := fmt.Sprintf("hub-%d", p.seq)
	cmd["id"] = id
	if err := p.send(cmd); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case line, ok := <-p.lines:
			if !ok {
				return nil, fmt.Errorf("%s", p.exitDetail())
			}
			var evt piEvent
			if json.Unmarshal(line, &evt) != nil || evt.Type != "response" || evt.ID != id {
				continue
			}
			if !evt.Success {
				return nil, fmt.Errorf("%s", evt.Error)
			}
			if evt.Data.Disposition == "handled" {
				return nil, errPiCommandHandled
			}
			return &evt, nil
		}
	}
}

func piText(raw json.RawMessage) string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "")
}

func piQuestionRequest(evt piEvent) (QuestionRequest, bool) {
	if evt.ID == "" {
		return QuestionRequest{}, false
	}
	var msg string
	if len(evt.Message) > 0 {
		_ = json.Unmarshal(evt.Message, &msg)
	}
	field := QuestionField{
		Key:         "value",
		Title:       evt.Title,
		Description: msg,
		Required:    true,
	}
	switch evt.Method {
	case "select":
		field.Type = "string"
		for _, opt := range evt.Options {
			field.Options = append(field.Options, QuestionOption{Value: opt, Label: opt})
		}
	case "confirm":
		field.Type = "boolean"
	case "input", "editor":
		field.Type = "string"
		field.Placeholder = evt.Placeholder
		if evt.Prefill != "" {
			field.Default = evt.Prefill
		}
	default:
		return QuestionRequest{}, false
	}
	return QuestionRequest{
		ID:     evt.ID,
		Title:  evt.Title,
		Fields: []QuestionField{field},
	}, true
}
