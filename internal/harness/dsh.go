package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// DshHarness drives DeepSeek Harness through one long-lived
// `dsh --profile acp` process (Agent Client Protocol over stdio). Every bot
// is one ACP session on that connection.
type DshHarness struct {
	binary  string
	startMu sync.Mutex // serializes process start + initialize

	mu      sync.Mutex
	proc    *lineProc
	seq     int
	pending map[int]chan acpResponse
	subs    map[string]chan acpUpdate // sessionID → turn listener
	open    map[string]*dshSession    // sessions live on the current proc
	models  []string
}

type dshSession struct {
	model       string
	autoApprove bool
}

type acpMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type acpResponse struct {
	Result json.RawMessage
	Err    error
}

type acpUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`
	Content       struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Title string `json:"title"`
}

type acpConfigOption struct {
	ID      string `json:"id"`
	Options []struct {
		Options []struct {
			Value string `json:"value"`
		} `json:"options"`
	} `json:"options"`
}

func NewDshHarness() *DshHarness {
	return &DshHarness{binary: envOr("DSH_BIN", "dsh")}
}

func (h *DshHarness) Kind() string  { return "dsh" }
func (h *DshHarness) Label() string { return "DeepSeek Harness" }

// Models returns the model choices dsh advertised on the last session it
// opened, or its stock routes before any session exists.
func (h *DshHarness) Models(context.Context) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.models) > 0 {
		return h.models, nil
	}
	return []string{"deepseek-account/deepseek-flash", "deepseek-account/deepseek-v4-pro", "deepseek-official/deepseek-v4-flash"}, nil
}

func (h *DshHarness) Close() {
	h.mu.Lock()
	p := h.proc
	h.proc = nil
	h.mu.Unlock()
	if p != nil {
		p.kill()
	}
}

// Reload terminates the shared dsh ACP process so the next turn picks up updated configs.
func (h *DshHarness) Reload() {
	h.mu.Lock()
	p := h.proc
	h.proc = nil
	h.open = make(map[string]*dshSession)
	h.models = nil
	h.mu.Unlock()
	if p != nil {
		p.kill()
	}
}

// ensure starts the ACP server if needed and returns it.
func (h *DshHarness) ensure(ctx context.Context) (*lineProc, error) {
	h.startMu.Lock()
	defer h.startMu.Unlock()
	h.mu.Lock()
	if h.proc != nil && h.proc.alive() {
		p := h.proc
		h.mu.Unlock()
		return p, nil
	}
	cmd := exec.Command(h.binary, "--profile", "acp")
	p, err := startLineProc(cmd)
	if err != nil {
		h.mu.Unlock()
		return nil, err
	}
	h.proc = p
	h.pending = make(map[int]chan acpResponse)
	h.subs = make(map[string]chan acpUpdate)
	h.open = make(map[string]*dshSession)
	h.mu.Unlock()
	go h.readLoop(p)

	if _, err := h.request(ctx, p, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}); err != nil {
		p.kill()
		return nil, fmt.Errorf("dsh acp initialize: %w", err)
	}
	return p, nil
}

func (h *DshHarness) readLoop(p *lineProc) {
	for line := range p.lines {
		var msg acpMessage
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		switch {
		case msg.Method == "session/update":
			var params struct {
				SessionID string    `json:"sessionId"`
				Update    acpUpdate `json:"update"`
			}
			if json.Unmarshal(msg.Params, &params) != nil {
				continue
			}
			h.mu.Lock()
			ch := h.subs[params.SessionID]
			h.mu.Unlock()
			if ch != nil {
				select {
				case ch <- params.Update:
				default:
				}
			}
		case msg.Method == "session/request_permission":
			h.answerPermission(p, msg)
		case msg.Method != "":
			// Unsupported client method (fs, terminal…): refuse.
			if len(msg.ID) > 0 {
				_ = p.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "method not supported"}})
			}
		default:
			var id int
			if json.Unmarshal(msg.ID, &id) != nil {
				continue
			}
			h.mu.Lock()
			ch := h.pending[id]
			delete(h.pending, id)
			h.mu.Unlock()
			if ch == nil {
				continue
			}
			res := acpResponse{Result: msg.Result}
			if msg.Error != nil {
				res.Err = fmt.Errorf("%s", msg.Error.Message)
			}
			ch <- res
		}
	}
	// Process exited: fail every waiter.
	h.mu.Lock()
	for id, ch := range h.pending {
		ch <- acpResponse{Err: fmt.Errorf("dsh exited: %s", p.exitDetail())}
		delete(h.pending, id)
	}
	if h.proc == p {
		h.proc = nil
	}
	h.mu.Unlock()
}

func (h *DshHarness) answerPermission(p *lineProc, msg acpMessage) {
	var params struct {
		SessionID string `json:"sessionId"`
		Options   []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	h.mu.Lock()
	allow := h.open[params.SessionID] != nil && h.open[params.SessionID].autoApprove
	h.mu.Unlock()
	want := "reject_once"
	if allow {
		want = "allow_once"
	}
	outcome := map[string]any{"outcome": "cancelled"}
	for _, o := range params.Options {
		if o.Kind == want {
			outcome = map[string]any{"outcome": "selected", "optionId": o.OptionID}
			break
		}
	}
	_ = p.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]any{"outcome": outcome}})
}

func (h *DshHarness) request(ctx context.Context, p *lineProc, method string, params any) (json.RawMessage, error) {
	ch := make(chan acpResponse, 1)
	h.mu.Lock()
	h.seq++
	id := h.seq
	h.pending[id] = ch
	h.mu.Unlock()
	if err := p.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case res := <-ch:
		return res.Result, res.Err
	case <-ctx.Done():
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return nil, ctx.Err()
	}
}

// openSession resumes the bot's persisted session on this connection, or
// starts a new one when there is none (or it can no longer be resumed).
func (h *DshHarness) openSession(ctx context.Context, p *lineProc, opts RunOptions) (string, error) {
	sid := opts.SessionID
	h.mu.Lock()
	s := h.open[sid]
	h.mu.Unlock()
	if sid != "" && s != nil {
		s.autoApprove = opts.Bot.ShouldAutoApprove()
		return sid, nil
	}
	params := map[string]any{"cwd": opts.Bot.Workspace, "mcpServers": acpMCPServers(opts)}
	var raw json.RawMessage
	var err error
	if sid != "" {
		params["sessionId"] = sid
		raw, err = h.request(ctx, p, "session/resume", params)
		if err != nil {
			log.Printf("[dsh] resume %s for bot %s failed, starting a new session: %v", sid, opts.Bot.ID, err)
			delete(params, "sessionId")
			sid = ""
		}
	}
	if sid == "" {
		raw, err = h.request(ctx, p, "session/new", params)
		if err != nil {
			return "", fmt.Errorf("dsh session/new: %w", err)
		}
	}
	var res struct {
		SessionID     string            `json:"sessionId"`
		ConfigOptions []acpConfigOption `json:"configOptions"`
	}
	_ = json.Unmarshal(raw, &res)
	if res.SessionID != "" {
		sid = res.SessionID
	}
	h.mu.Lock()
	h.open[sid] = &dshSession{autoApprove: opts.Bot.ShouldAutoApprove()}
	if m := acpModels(res.ConfigOptions); len(m) > 0 {
		h.models = m
	}
	h.mu.Unlock()
	return sid, nil
}

func acpMCPServers(opts RunOptions) []any {
	if len(opts.MCPCommand) == 0 {
		return []any{}
	}
	env := []map[string]string{}
	for k, v := range opts.MCPEnv {
		env = append(env, map[string]string{"name": k, "value": v})
	}
	return []any{map[string]any{"name": "bot-hub", "command": opts.MCPCommand[0], "args": opts.MCPCommand[1:], "env": env}}
}

// acpModels turns dsh's `["provider","model"]` option values into
// provider/model IDs.
func acpModels(opts []acpConfigOption) []string {
	var out []string
	for _, o := range opts {
		if o.ID != "model" {
			continue
		}
		for _, g := range o.Options {
			for _, v := range g.Options {
				var pm []string
				if json.Unmarshal([]byte(v.Value), &pm) == nil && len(pm) == 2 {
					out = append(out, pm[0]+"/"+pm[1])
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func (h *DshHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	if bot.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(bot.TimeoutSec)*time.Second)
		defer cancel()
	}
	p, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	sid, err := h.openSession(ctx, p, opts)
	if err != nil {
		return nil, err
	}
	partial := &TurnResult{BotID: bot.ID, SessionID: sid}

	h.mu.Lock()
	s := h.open[sid]
	h.mu.Unlock()
	if bot.Model != "" && s.model != bot.Model {
		provider, id := splitModel(bot.Model)
		value, _ := json.Marshal([]string{provider, id})
		if _, err := h.request(ctx, p, "session/set_config_option", map[string]any{"sessionId": sid, "configId": "model", "value": string(value)}); err != nil {
			return partial, fmt.Errorf("dsh model %q: %w", bot.Model, err)
		}
		s.model = bot.Model
	}

	updates := make(chan acpUpdate, 1024)
	h.mu.Lock()
	h.subs[sid] = updates
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.subs[sid] == updates {
			delete(h.subs, sid)
		}
		h.mu.Unlock()
	}()

	type promptResult struct {
		raw json.RawMessage
		err error
	}
	done := make(chan promptResult, 1)
	go func() {
		// Not tied to ctx: an interrupt is session/cancel, and the prompt
		// still answers (with stopReason "cancelled").
		raw, err := h.request(context.Background(), p, "session/prompt", map[string]any{
			"sessionId": sid,
			"prompt":    []map[string]string{{"type": "text", "text": opts.Prompt}},
		})
		done <- promptResult{raw, err}
	}()

	var texts strings.Builder
	cancelled := false
	var cancelDeadline <-chan time.Time
	for {
		select {
		case <-ctxDone(ctx, cancelled):
			cancelled = true
			_ = p.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
			cancelDeadline = time.After(15 * time.Second)
		case <-cancelDeadline:
			partial.Text = strings.TrimSpace(texts.String())
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)
		case u := <-updates:
			switch u.SessionUpdate {
			case "agent_message_chunk":
				if u.Content.Type == "text" {
					texts.WriteString(u.Content.Text)
					if opts.OnText != nil {
						opts.OnText(u.Content.Text)
					}
				}
			case "tool_call":
				if opts.OnToolUse != nil {
					opts.OnToolUse(u.Title)
				}
			}
		case r := <-done:
			// Drain updates delivered just before the response.
			for drained := false; !drained; {
				select {
				case u := <-updates:
					if u.SessionUpdate == "agent_message_chunk" && u.Content.Type == "text" {
						texts.WriteString(u.Content.Text)
					}
				default:
					drained = true
				}
			}
			if r.err != nil {
				partial.Text = strings.TrimSpace(texts.String())
				return partial, fmt.Errorf("dsh failed for bot %q: %w", bot.ID, r.err)
			}
			var res struct {
				StopReason string `json:"stopReason"`
			}
			_ = json.Unmarshal(r.raw, &res)
			if cancelled || res.StopReason == "cancelled" {
				partial.Text = strings.TrimSpace(texts.String())
				return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)
			}
			text := strings.TrimSpace(texts.String())
			if res.StopReason == "refusal" && text == "" {
				return partial, fmt.Errorf("dsh refused the prompt for bot %q", bot.ID)
			}
			return finish(partial, text, Usage{}, started), nil
		}
	}
}
