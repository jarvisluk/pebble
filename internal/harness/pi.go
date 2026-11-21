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
	"time"
)

// PiHarness keeps one long-lived `pi --mode rpc` process per bot. The hub
// picks the session ID, so a restarted process reopens the same session.
type PiHarness struct {
	binary string
	mu     sync.Mutex
	procs  map[string]*piProc

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
	} `json:"data"`
	Method   string `json:"method"`
	ToolName string `json:"toolName"`
	Aborted  bool   `json:"aborted"`
	Message  *struct {
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
	} `json:"message"`
	AME *struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"assistantMessageEvent"`
}

func NewPiHarness() *PiHarness {
	return &PiHarness{binary: envOr("PI_BIN", "pi"), procs: make(map[string]*piProc)}
}

func (h *PiHarness) Kind() string  { return "pi" }
func (h *PiHarness) Label() string { return "Pi" }

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

	if err := writePiMCPConfig(bot.Workspace, opts); err != nil {
		return nil, err
	}
	// --approve trusts the workspace's .pi/mcp.json for this run.
	args := []string{"--mode", "rpc", "--approve", "--session-id", sessionID}
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
	p := &piProc{lineProc: lp, model: bot.Model, sessionID: sessionID}
	h.procs[bot.ID] = p
	return p, nil
}

// writePiMCPConfig exposes the bot-hub MCP server through the workspace's
// .pi/mcp.json. It may hold the hub API token, so it is private.
func writePiMCPConfig(workspace string, opts RunOptions) error {
	if len(opts.MCPCommand) == 0 {
		return nil
	}
	dir := filepath.Join(workspace, ".pi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	server := map[string]any{"command": opts.MCPCommand[0], "args": opts.MCPCommand[1:]}
	if len(opts.MCPEnv) > 0 {
		server["env"] = opts.MCPEnv
	}
	b, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"bot-hub": server}}, "", "  ")
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (h *PiHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	if bot.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(bot.TimeoutSec)*time.Second)
		defer cancel()
	}
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = newUUID()
	}
	p, err := h.proc(opts, sessionID)
	if err != nil {
		return nil, err
	}
	partial := &TurnResult{BotID: bot.ID, SessionID: sessionID}

	if bot.Model != "" && bot.Model != p.model {
		provider, id := splitModel(bot.Model)
		if _, err := h.call(ctx, p, map[string]any{"type": "set_model", "provider": provider, "modelId": id}); err != nil {
			return partial, fmt.Errorf("pi set_model %q: %w", bot.Model, err)
		}
		p.model = bot.Model
	}

	if _, err := h.call(ctx, p, map[string]any{"type": "prompt", "message": opts.Prompt}); err != nil {
		return partial, fmt.Errorf("pi failed for bot %q: %w", bot.ID, err)
	}

	var texts []string
	var usage Usage
	var lastErr string
	interrupted := false
	var abortDeadline <-chan time.Time
	for {
		select {
		case <-ctxDone(ctx, interrupted):
			interrupted = true
			_ = p.send(map[string]any{"type": "abort"})
			abortDeadline = time.After(10 * time.Second)
		case <-abortDeadline:
			partial.Text = strings.Join(texts, "\n")
			h.drop(bot.ID, p)
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)
		case line, ok := <-p.lines:
			if !ok {
				partial.Text = strings.Join(texts, "\n")
				h.drop(bot.ID, p)
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
				m := evt.Message
				if m == nil || m.Role != "assistant" {
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
				// No UI here: dismiss dialogs so extensions never block a turn.
				switch evt.Method {
				case "select", "confirm", "input", "editor":
					_ = p.send(map[string]any{"type": "extension_ui_response", "id": evt.ID, "cancelled": true})
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
				return nil, fmt.Errorf("prompt was handled by a pi command, no run started")
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
