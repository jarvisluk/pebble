package harness

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// OpenCodeHarness keeps one long-lived `opencode serve` (or a configured
// external server) and drives sessions over its HTTP API, receiving output
// from a single shared SSE event stream.
type OpenCodeHarness struct {
	binary   string
	external string

	mu       sync.Mutex
	baseURL  string
	password string
	cmd      *exec.Cmd
	stopEv   context.CancelFunc
	closed   bool

	subsMu sync.Mutex
	subs   map[string]chan ocEvent

	applied sync.Map // sessionID -> model last applied
}

type ocEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

var ocEventTypes = []string{
	"session.text.delta",
	"session.text.ended",
	"session.tool.input.started",
	"session.step.ended",
	"session.execution.succeeded",
	"session.execution.failed",
	"session.execution.interrupted",
	"permission.asked",
}

func NewOpenCodeHarness(externalURL string) *OpenCodeHarness {
	binary := os.Getenv("OPENCODE_BIN")
	if binary == "" {
		binary = "opencode"
	}
	return &OpenCodeHarness{
		binary:   binary,
		external: strings.TrimRight(externalURL, "/"),
		subs:     make(map[string]chan ocEvent),
	}
}

func (h *OpenCodeHarness) Kind() string  { return "opencode" }
func (h *OpenCodeHarness) Label() string { return "OpenCode" }

// ensure returns a ready server (started on first use) with its event stream connected.
func (h *OpenCodeHarness) ensure(ctx context.Context) (string, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return "", "", errors.New("opencode harness closed")
	}
	if h.baseURL != "" {
		return h.baseURL, h.password, nil
	}

	base, pw := h.external, os.Getenv("OPENCODE_SERVER_PASSWORD")
	var cmd *exec.Cmd
	if base == "" {
		port, err := freePort()
		if err != nil {
			return "", "", err
		}
		pw = randHex(16)
		base = fmt.Sprintf("http://127.0.0.1:%d", port)
		cmd = exec.Command(h.binary, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
		cmd.Env = append(os.Environ(), "OPENCODE_SERVER_PASSWORD="+pw)
		// Own process group so Close also stops the MCP servers it spawns.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return "", "", fmt.Errorf("start opencode serve: %w", err)
		}
		go func() {
			err := cmd.Wait()
			h.mu.Lock()
			if h.cmd == cmd {
				log.Printf("[opencode] server exited: %v", err)
				h.resetLocked()
			}
			h.mu.Unlock()
		}()
	}

	if err := waitHealthy(ctx, base, pw); err != nil {
		if cmd != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return "", "", err
	}

	evCtx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	go h.eventLoop(evCtx, base, pw, ready)
	select {
	case <-ready:
	case <-time.After(15 * time.Second):
		cancel()
		if cmd != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return "", "", errors.New("opencode event stream did not connect")
	}

	h.baseURL, h.password, h.cmd, h.stopEv = base, pw, cmd, cancel
	if cmd != nil {
		log.Printf("[opencode] server ready at %s (pid %d)", base, cmd.Process.Pid)
	}
	return base, pw, nil
}

func (h *OpenCodeHarness) resetLocked() {
	if h.stopEv != nil {
		h.stopEv()
	}
	h.baseURL, h.password, h.cmd, h.stopEv = "", "", nil, nil
}

func (h *OpenCodeHarness) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	if h.cmd != nil {
		_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGTERM)
	}
	h.resetLocked()
}

// Reload terminates the idle opencode serve process (keeping the harness open)
// so subsequent turns restart it with updated CLI/provider configs.
func (h *OpenCodeHarness) Reload() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	if h.cmd != nil && h.cmd.Process != nil {
		_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGTERM)
	}
	h.resetLocked()
	h.applied.Range(func(k, _ any) bool {
		h.applied.Delete(k)
		return true
	})
}

func (h *OpenCodeHarness) eventLoop(ctx context.Context, base, pw string, ready chan struct{}) {
	q := url.Values{}
	for _, t := range ocEventTypes {
		q.Add("types", t)
	}
	var once sync.Once
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/event?"+q.Encode(), nil)
		req.SetBasicAuth(serverUser(), pw)
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
			for sc.Scan() {
				line := sc.Text()
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				var evt ocEvent
				if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &evt) != nil {
					continue
				}
				if evt.Type == "server.connected" {
					once.Do(func() { close(ready) })
					continue
				}
				h.dispatch(evt)
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (h *OpenCodeHarness) dispatch(evt ocEvent) {
	var d struct {
		SessionID string `json:"sessionID"`
	}
	if json.Unmarshal(evt.Data, &d) != nil || d.SessionID == "" {
		return
	}
	h.subsMu.Lock()
	ch := h.subs[d.SessionID]
	h.subsMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- evt:
	default:
		log.Printf("[opencode] dropped %s for %s (consumer lagging)", evt.Type, d.SessionID)
	}
}

func (h *OpenCodeHarness) subscribe(sid string) chan ocEvent {
	ch := make(chan ocEvent, 1024)
	h.subsMu.Lock()
	h.subs[sid] = ch
	h.subsMu.Unlock()
	return ch
}

func (h *OpenCodeHarness) unsubscribe(sid string) {
	h.subsMu.Lock()
	delete(h.subs, sid)
	h.subsMu.Unlock()
}

func (h *OpenCodeHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	if bot.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(bot.TimeoutSec)*time.Second)
		defer cancel()
	}

	base, pw, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	api := &ocAPI{base: base, pw: pw}
	model := modelRef(bot.Model)

	sid := opts.SessionID
	if sid != "" {
		if code, _ := api.do(ctx, http.MethodGet, "/api/session/"+sid, nil, nil); code == http.StatusNotFound {
			sid = ""
		}
	}
	if sid == "" {
		body := map[string]any{
			"title":    "bot:" + bot.ID,
			"location": map[string]string{"directory": bot.Workspace},
		}
		if model != nil {
			body["model"] = model
		}
		if bot.Agent != "" {
			body["agent"] = bot.Agent
		}
		var out struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if _, err := api.do(ctx, http.MethodPost, "/api/session", body, &out); err != nil {
			return nil, fmt.Errorf("create opencode session: %w", err)
		}
		sid = out.Data.ID
		h.applied.Store(sid, bot.Model)
	} else if prev, _ := h.applied.Load(sid); model != nil && prev != bot.Model {
		if _, err := api.do(ctx, http.MethodPost, "/api/session/"+sid+"/model", map[string]any{"model": model}, nil); err != nil {
			return nil, fmt.Errorf("switch model: %w", err)
		}
		h.applied.Store(sid, bot.Model)
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: sid}
	ch := h.subscribe(sid)
	defer h.unsubscribe(sid)

	if _, err := api.do(ctx, http.MethodPost, "/api/session/"+sid+"/prompt", map[string]any{"text": opts.Prompt}, nil); err != nil {
		return partial, fmt.Errorf("send prompt: %w", err)
	}

	var texts []string
	var usage Usage
	for {
		select {
		case <-ctx.Done():
			// Stop the running execution so the session is free for the next turn.
			ictx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = api.do(ictx, http.MethodPost, "/api/session/"+sid+"/interrupt", nil, nil)
			cancel()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return partial, fmt.Errorf("bot %q timed out after %ds", bot.ID, bot.TimeoutSec)
			}
			return partial, fmt.Errorf("bot %q interrupted", bot.ID)

		case evt := <-ch:
			var d struct {
				ID     string `json:"id"`
				Delta  string `json:"delta"`
				Text   string `json:"text"`
				Name   string `json:"name"`
				Cost   float64
				Tokens *struct {
					Input  int `json:"input"`
					Output int `json:"output"`
					Cache  struct {
						Read int `json:"read"`
					} `json:"cache"`
				} `json:"tokens"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(evt.Data, &d)
			switch evt.Type {
			case "session.text.delta":
				if opts.OnText != nil && d.Delta != "" {
					opts.OnText(d.Delta)
				}
			case "session.text.ended":
				if t := strings.TrimSpace(d.Text); t != "" {
					texts = append(texts, t)
				}
			case "session.tool.input.started":
				if opts.OnToolUse != nil {
					opts.OnToolUse(d.Name)
				}
			case "session.step.ended":
				usage.CostUSD += d.Cost
				if d.Tokens != nil {
					usage.InputTokens += d.Tokens.Input
					usage.OutputTokens += d.Tokens.Output
					usage.CacheReadTokens += d.Tokens.Cache.Read
				}
			case "permission.asked":
				decision := "reject"
				if bot.ShouldAutoApprove() {
					decision = "once"
				}
				_, _ = api.do(ctx, http.MethodPost, "/api/session/"+sid+"/permission/"+d.ID+"/reply", map[string]any{"decision": decision}, nil)
			case "session.execution.failed":
				msg := "unknown error"
				if d.Error != nil && d.Error.Message != "" {
					msg = d.Error.Message
				}
				return partial, fmt.Errorf("opencode failed for bot %q: %s", bot.ID, msg)
			case "session.execution.interrupted":
				return partial, fmt.Errorf("bot %q interrupted", bot.ID)
			case "session.execution.succeeded":
				dur := time.Since(started)
				return &TurnResult{
					BotID:      bot.ID,
					SessionID:  sid,
					Text:       strings.Join(texts, "\n"),
					DurationMs: dur.Milliseconds(),
					Duration:   dur,
					Usage:      usage,
				}, nil
			}
		}
	}
}

func (h *OpenCodeHarness) Models(ctx context.Context) ([]string, error) {
	base, pw, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID         string `json:"id"`
			ProviderID string `json:"providerID"`
		} `json:"data"`
	}
	if _, err := (&ocAPI{base: base, pw: pw}).do(ctx, http.MethodGet, "/api/model", nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ProviderID+"/"+m.ID)
	}
	return ids, nil
}

// modelRef splits "provider/model/id" into opencode's {providerID, id}.
func modelRef(model string) map[string]string {
	provider, id, ok := strings.Cut(model, "/")
	if !ok || provider == "" || id == "" {
		return nil
	}
	return map[string]string{"providerID": provider, "id": id}
}

type ocAPI struct {
	base, pw string
}

func (a *ocAPI) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, rd)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(serverUser(), a.pw)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Message != "" {
			return resp.StatusCode, errors.New(e.Message)
		}
		return resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return resp.StatusCode, json.Unmarshal(raw, out)
	}
	return resp.StatusCode, nil
}

func waitHealthy(ctx context.Context, base, pw string) error {
	api := &ocAPI{base: base, pw: pw}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if code, err := api.do(ctx, http.MethodGet, "/api/info", nil, nil); err == nil && code == http.StatusOK {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("opencode server at %s not reachable", base)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func serverUser() string {
	if u := os.Getenv("OPENCODE_SERVER_USERNAME"); u != "" {
		return u
	}
	return "opencode"
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
