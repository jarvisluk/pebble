package harness

import (
	"bytes"
	"context"
	"encoding/base64"
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
	mcpMu    sync.Mutex
	procs    map[string]*piProc
	commands map[string][]SlashCommand

	modelsOnce sync.Once
	models     []string
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
		procs:    make(map[string]*piProc),
		commands: make(map[string][]SlashCommand),
	}
}

func (h *PiHarness) Kind() string  { return "pi" }
func (h *PiHarness) Label() string { return "Pi" }

func (h *PiHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "压缩当前会话上下文", Source: "harness"},
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
	return mergeSlashCommands(cached, builtins, local), nil
}

// Models lists `pi --list-models` as provider/model IDs (cached).
func (h *PiHarness) Models(ctx context.Context) ([]string, error) {
	h.modelsOnce.Do(func() {
		out, err := exec.CommandContext(ctx, h.binary, "--list-models", "--offline").Output()
		if err != nil {
			return
		}
		for i, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if i == 0 || len(f) < 2 {
				continue
			}
			h.models = append(h.models, f[0]+"/"+f[1])
		}
	})
	return h.models, nil
}

func (h *PiHarness) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, p := range h.procs {
		p.kill()
		delete(h.procs, id)
	}
}

// Reload terminates cached pi RPC processes and clears the cached model list so
// updated provider settings take effect on the next call.
func (h *PiHarness) Reload() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, p := range h.procs {
		p.kill()
		delete(h.procs, id)
	}
	h.modelsOnce = sync.Once{}
	h.models = nil
}

func (h *PiHarness) drop(botID string, p *piProc) {
	h.mu.Lock()
	if h.procs[botID] == p {
		delete(h.procs, botID)
	}
	h.mu.Unlock()
	p.kill()
}

func (h *PiHarness) proc(opts RunOptions, sessionID string) (*piProc, error) {
	bot := opts.Bot
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.procs[bot.ID]; p != nil {
		if p.alive() && p.sessionID == sessionID {
			return p, nil
		}
		p.kill()
		delete(h.procs, bot.ID)
	}

	p, err := h.startProc(opts, sessionID, false)
	if err != nil {
		return nil, err
	}
	h.procs[bot.ID] = p
	return p, nil
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
	if len(opts.MCPCommand) == 0 {
		return nil
	}
	h.mcpMu.Lock()
	defer h.mcpMu.Unlock()

	dir := filepath.Join(workspace, ".pi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "mcp.json")
	doc := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &doc)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	server := map[string]any{"command": opts.MCPCommand[0], "args": opts.MCPCommand[1:]}
	if len(opts.MCPEnv) > 0 {
		server["env"] = opts.MCPEnv
	}
	servers["bot-hub"] = server
	doc["mcpServers"] = servers
	b, _ := json.MarshalIndent(doc, "", "  ")
	want := append(b, '\n')
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, want) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, want, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (h *PiHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	if bot.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(bot.TimeoutSec)*time.Second)
		defer cancel()
	}
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
		p, err = h.proc(opts, sessionID)
		if err != nil {
			return nil, err
		}
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
	for _, att := range opts.Attachments {
		if att.Kind != "image" || att.Path == "" {
			continue
		}
		raw, err := os.ReadFile(att.Path)
		if err != nil || len(raw) == 0 {
			continue
		}
		mt := att.MimeType
		if mt == "" {
			mt = "image/png"
		}
		piImages = append(piImages, map[string]any{
			"type":     "image",
			"data":     base64.StdEncoding.EncodeToString(raw),
			"mimeType": mt,
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
