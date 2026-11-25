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
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"bot-hub/internal/config"
)

// OpenCodeHarness keeps one long-lived `opencode serve` (or a configured
// external server) and drives sessions over its HTTP API, receiving output
// from a single shared SSE event stream.
type OpenCodeHarness struct {
	binary      string
	external    string
	httpClient  *http.Client
	modelsCache ttlCache[[]string]

	mu       sync.Mutex
	baseURL  string
	password string
	cmd      *exec.Cmd
	stopEv   context.CancelFunc
	closed   bool

	subsMu sync.Mutex
	subs   map[string]chan ocEvent

	applied      sync.Map // sessionID -> model last applied
	appliedAgent sync.Map // sessionID -> agent last applied
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
	"permission.replied",
	"form.created",
	"form.replied",
	"form.cancelled",
}

func NewOpenCodeHarness(externalURL string) *OpenCodeHarness {
	binary := os.Getenv("OPENCODE_BIN")
	if binary == "" {
		binary = "opencode"
	}
	return &OpenCodeHarness{
		binary:      binary,
		external:    strings.TrimRight(externalURL, "/"),
		httpClient:  &http.Client{Timeout: 60 * time.Second},
		modelsCache: ttlCache[[]string]{ttl: time.Minute},
		subs:        make(map[string]chan ocEvent),
	}
}

func (h *OpenCodeHarness) apiClient(base, pw string) *ocAPI {
	return &ocAPI{base: base, pw: pw, client: h.httpClient}
}

func (h *OpenCodeHarness) Kind() string  { return "opencode" }
func (h *OpenCodeHarness) Label() string { return "OpenCode" }

func (h *OpenCodeHarness) PermissionCapabilities() PermissionCapabilities {
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

func (h *OpenCodeHarness) resolveBinary() (string, bool) {
	return resolveBinaryFor("OPENCODE_BIN", "opencode", h.binary)
}

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
		bin, ok := h.resolveBinary()
		if !ok {
			bin = h.binary
		}
		cmd = exec.Command(bin, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
		cmd.Env = augmentedEnv(map[string]string{"OPENCODE_SERVER_PASSWORD": pw})
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
	h.modelsCache.invalidate()
	h.applied.Range(func(k, _ any) bool {
		h.applied.Delete(k)
		return true
	})
	h.appliedAgent.Range(func(k, _ any) bool {
		h.appliedAgent.Delete(k)
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
		Form      *struct {
			SessionID string `json:"sessionID"`
		} `json:"form"`
	}
	if json.Unmarshal(evt.Data, &d) != nil {
		return
	}
	sid := d.SessionID
	if sid == "" && d.Form != nil {
		sid = d.Form.SessionID
	}
	if sid == "" {
		return
	}
	h.subsMu.Lock()
	ch := h.subs[sid]
	h.subsMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- evt:
	default:
		log.Printf("[opencode] dropped %s for %s (consumer lagging)", evt.Type, sid)
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
	ctx, cancel := withTurnTimeout(ctx, bot.TimeoutSec, opts)
	defer cancel()

	base, pw, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	api := h.apiClient(base, pw)
	model := modelRef(bot.Model)
	mode := EffectivePermissionMode(opts)
	targetAgent := strings.TrimSpace(bot.Agent)
	if mode == config.PermissionModePlan {
		targetAgent = "plan"
	}

	createSession := func() (string, error) {
		title := "bot:" + bot.ID
		if opts.Ephemeral {
			title = "routine:" + bot.ID
		}
		body := map[string]any{
			"title":    title,
			"location": map[string]string{"directory": bot.Workspace},
		}
		if model != nil {
			body["model"] = model
		}
		if targetAgent != "" {
			body["agent"] = targetAgent
		}
		var out struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if _, err := api.do(ctx, http.MethodPost, "/api/session", body, &out); err != nil {
			return "", fmt.Errorf("create opencode session: %w", err)
		}
		h.applied.Store(out.Data.ID, bot.Model)
		h.appliedAgent.Store(out.Data.ID, targetAgent)
		return out.Data.ID, nil
	}

	sid := opts.SessionID
	reusedKnown := false
	if opts.Ephemeral {
		sid = ""
	} else if sid != "" {
		if _, known := h.applied.Load(sid); known {
			reusedKnown = true
		} else if code, _ := api.do(ctx, http.MethodGet, "/api/session/"+sid, nil, nil); code == http.StatusNotFound {
			sid = ""
		}
	}
	if sid != "" {
		if prev, _ := h.applied.Load(sid); model != nil && prev != bot.Model {
			if code, err := api.do(ctx, http.MethodPost, "/api/session/"+sid+"/model", map[string]any{"model": model}, nil); err != nil {
				if code == http.StatusNotFound {
					h.applied.Delete(sid)
					h.appliedAgent.Delete(sid)
					sid = ""
					reusedKnown = false
				} else {
					return nil, fmt.Errorf("switch model: %w", err)
				}
			} else {
				h.applied.Store(sid, bot.Model)
			}
		}
	}
	if sid != "" {
		prevAgentRaw, hasPrevAgent := h.appliedAgent.Load(sid)
		prevAgent, _ := prevAgentRaw.(string)
		if (hasPrevAgent && prevAgent != targetAgent) || (!hasPrevAgent && targetAgent != "") {
			agentToApply := targetAgent
			if agentToApply == "" && prevAgent == "plan" {
				agentToApply = "build"
			}
			if agentToApply != "" {
				if code, _ := api.do(ctx, http.MethodPost, "/api/session/"+sid+"/agent", map[string]any{"agent": agentToApply}, nil); code == http.StatusNotFound && !reusedKnown {
					// Ignore if server does not support /agent or custom agent name.
				}
			}
			h.appliedAgent.Store(sid, targetAgent)
		}
	}
	if sid == "" {
		var cerr error
		sid, cerr = createSession()
		if cerr != nil {
			return nil, cerr
		}
	}
	if opts.Ephemeral && sid != "" {
		defer func(ephemeralSID string) {
			h.applied.Delete(ephemeralSID)
			h.appliedAgent.Delete(ephemeralSID)
			dctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = api.do(dctx, http.MethodDelete, "/api/session/"+ephemeralSID, nil, nil)
		}(sid)
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: sid}
	ch := h.subscribe(sid)
	defer func() { h.unsubscribe(partial.SessionID) }()

	var ocFiles []map[string]any
	for _, att := range opts.Attachments {
		if att.Path != "" {
			ocFiles = append(ocFiles, map[string]any{
				"uri":  "file://" + att.Path,
				"name": att.Name,
			})
		}
	}

	sendTurn := func(targetSID string) (bool, int, error) {
		if cmdName, cmdArgs, ok := ParseSlashCommand(opts.Prompt); ok {
			if cmdName == "compact" {
				code, err := api.do(ctx, http.MethodPost, "/api/session/"+targetSID+"/compact", map[string]any{}, nil)
				if err != nil {
					return true, code, fmt.Errorf("compact session: %w", err)
				}
				return true, code, nil
			}
			cmdBody := map[string]any{
				"name": cmdName,
				"text": cmdArgs,
			}
			if len(ocFiles) > 0 {
				cmdBody["files"] = ocFiles
			}
			code, err := api.do(ctx, http.MethodPost, "/api/session/"+targetSID+"/command", cmdBody, nil)
			if err != nil {
				if code == http.StatusBadRequest || code == http.StatusNotFound {
					promptBody := map[string]any{"text": AppendAttachmentContext(opts.Prompt, opts.Attachments)}
					if len(ocFiles) > 0 {
						promptBody["files"] = ocFiles
					}
					pcode, perr := api.do(ctx, http.MethodPost, "/api/session/"+targetSID+"/prompt", promptBody, nil)
					if perr != nil {
						return false, pcode, fmt.Errorf("send prompt: %w", perr)
					}
					return false, pcode, nil
				}
				return false, code, fmt.Errorf("send command: %w", err)
			}
			return false, code, nil
		}
		promptBody := map[string]any{"text": AppendAttachmentContext(opts.Prompt, opts.Attachments)}
		if len(ocFiles) > 0 {
			promptBody["files"] = ocFiles
		}
		code, err := api.do(ctx, http.MethodPost, "/api/session/"+targetSID+"/prompt", promptBody, nil)
		if err != nil {
			return false, code, fmt.Errorf("send prompt: %w", err)
		}
		return false, code, nil
	}

	isCompact, code, err := sendTurn(sid)
	if err != nil && code == http.StatusNotFound && reusedKnown {
		h.applied.Delete(sid)
		h.unsubscribe(sid)
		newSID, cerr := createSession()
		if cerr != nil {
			return partial, cerr
		}
		sid = newSID
		partial.SessionID = sid
		ch = h.subscribe(sid)
		isCompact, _, err = sendTurn(sid)
	}
	if err != nil {
		return partial, err
	}

	var texts []string
	var usage Usage
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

	var permMu sync.Mutex
	activePerms := make(map[string]context.CancelFunc)
	cancelAllPerms := func() {
		permMu.Lock()
		for id, cancel := range activePerms {
			cancel()
			delete(activePerms, id)
		}
		permMu.Unlock()
	}
	defer cancelAllPerms()

	cancelForm := func(formID, msg string) {
		path := "/api/session/" + sid + "/form/" + url.PathEscape(formID)
		if msg != "" {
			path += "?message=" + url.QueryEscape(msg)
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccancel()
		_, _ = api.do(cctx, http.MethodDelete, path, nil, nil)
	}

	for {
		select {
		case <-ctx.Done():
			// Stop the running execution so the session is free for the next turn.
			cancelAllForms()
			cancelAllPerms()
			partial.Text = strings.Join(texts, "\n")
			ictx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = api.do(ictx, http.MethodPost, "/api/session/"+sid+"/interrupt", nil, nil)
			cancel()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return partial, fmt.Errorf("bot %q timed out after %ds", bot.ID, bot.TimeoutSec)
			}
			return partial, fmt.Errorf("bot %q interrupted", bot.ID)

		case evt := <-ch:
			var d struct {
				ID         string         `json:"id"`
				RequestID  string         `json:"requestID"`
				Action     string         `json:"action"`
				Permission string         `json:"permission"`
				Resources  []string       `json:"resources"`
				Patterns   []string       `json:"patterns"`
				Metadata   map[string]any `json:"metadata"`
				Message    string         `json:"message"`
				Title      string         `json:"title"`
				Delta      string         `json:"delta"`
				Text       string         `json:"text"`
				Name       string         `json:"name"`
				Cost       float64
				Tokens     *struct {
					Input  int `json:"input"`
					Output int `json:"output"`
					Cache  struct {
						Read int `json:"read"`
					} `json:"cache"`
				} `json:"tokens"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
				Form *struct {
					ID     string `json:"id"`
					Title  string `json:"title"`
					Fields []struct {
						Key         string           `json:"key"`
						Title       string           `json:"title"`
						Description string           `json:"description"`
						Type        string           `json:"type"`
						Required    bool             `json:"required"`
						Hidden      bool             `json:"hidden"`
						Placeholder string           `json:"placeholder"`
						Default     any              `json:"default"`
						Options     []QuestionOption `json:"options"`
						Custom      bool             `json:"custom"`
					} `json:"fields"`
				} `json:"form"`
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
				permID := d.ID
				if permID == "" {
					continue
				}
				action := strings.TrimSpace(d.Action)
				if action == "" {
					action = strings.TrimSpace(d.Permission)
				}
				resources := d.Resources
				if len(resources) == 0 {
					resources = d.Patterns
				}
				rawInput := make(map[string]any, len(d.Metadata)+2)
				for k, v := range d.Metadata {
					rawInput[k] = v
				}
				if len(resources) > 0 {
					rawInput["resources"] = resources
				}
				if strings.TrimSpace(d.Message) != "" {
					rawInput["message"] = d.Message
				}
				toolName := action
				if toolName == "" {
					toolName = "tool"
				}
				summary := strings.TrimSpace(strings.Join(resources, ", "))
				if summary == "" {
					summary = strings.TrimSpace(d.Message)
				}
				if summary == "" {
					summary = strings.TrimSpace(d.Title)
				}
				title := strings.TrimSpace(d.Title)
				if title == "" {
					title = strings.TrimSpace(d.Message)
				}
				if title == "" {
					title = toolName
				}
				req := PermissionRequest{
					ID:       permID,
					ToolName: toolName,
					Kind:     NormalizeToolKind(action, toolName),
					Title:    title,
					Summary:  summary,
					RawInput: rawInput,
					Options:  StandardPermissionOptions(true),
				}
				pCtx, pCancel := context.WithCancel(ctx)
				permMu.Lock()
				if prev, exists := activePerms[permID]; exists {
					prev()
				}
				activePerms[permID] = pCancel
				permMu.Unlock()
				go func() {
					defer pCancel()
					resp := ResolveApprovalDecision(pCtx, opts, req)
					permMu.Lock()
					delete(activePerms, permID)
					permMu.Unlock()
					if pCtx.Err() != nil && ctx.Err() == nil {
						// External permission.replied cancelled our pending prompt.
						return
					}
					decision := "reject"
					switch resp.Decision {
					case "allow_once":
						decision = "once"
					case "allow_always":
						decision = "always"
					}
					body := map[string]any{"decision": decision}
					if decision == "reject" && strings.TrimSpace(resp.Reason) != "" {
						body["message"] = strings.TrimSpace(resp.Reason)
					}
					rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
					_, _ = api.do(rctx, http.MethodPost, "/api/session/"+sid+"/permission/"+url.PathEscape(permID)+"/reply", body, nil)
					rcancel()
				}()
			case "permission.replied":
				targetID := strings.TrimSpace(d.RequestID)
				if targetID == "" {
					targetID = strings.TrimSpace(d.ID)
				}
				if targetID != "" {
					permMu.Lock()
					if cancel, ok := activePerms[targetID]; ok {
						cancel()
						delete(activePerms, targetID)
					}
					permMu.Unlock()
				}
			case "form.created":
				if d.Form == nil || d.Form.ID == "" {
					continue
				}
				fields := make([]QuestionField, 0, len(d.Form.Fields))
				for _, f := range d.Form.Fields {
					if f.Hidden || f.Type == "external" {
						continue
					}
					fields = append(fields, QuestionField{
						Key:         f.Key,
						Title:       f.Title,
						Description: f.Description,
						Type:        f.Type,
						Required:    f.Required,
						Placeholder: f.Placeholder,
						Default:     f.Default,
						Options:     f.Options,
						Custom:      f.Custom,
					})
				}
				if opts.OnQuestion == nil || len(fields) == 0 {
					go cancelForm(d.Form.ID, "non-interactive")
					continue
				}
				qCtx, qCancel := context.WithCancel(ctx)
				formID := d.Form.ID
				formTitle := d.Form.Title
				formMu.Lock()
				if prev, ok := activeForms[formID]; ok {
					prev()
				}
				activeForms[formID] = qCancel
				formMu.Unlock()
				go func() {
					defer qCancel()
					resp, err := opts.OnQuestion(qCtx, QuestionRequest{
						ID:     formID,
						Title:  formTitle,
						Fields: fields,
					})
					formMu.Lock()
					delete(activeForms, formID)
					formMu.Unlock()
					if qCtx.Err() != nil {
						if ctx.Err() != nil {
							cancelForm(formID, "interrupted")
						}
						return
					}
					if err != nil || resp.Cancelled {
						cancelForm(formID, resp.Message)
						return
					}
					_, _ = api.do(ctx, http.MethodPost, "/api/session/"+sid+"/form/"+url.PathEscape(formID)+"/reply", map[string]any{"answer": resp.Answers}, nil)
				}()
			case "form.replied", "form.cancelled":
				if d.ID != "" {
					formMu.Lock()
					if cancel, ok := activeForms[d.ID]; ok {
						cancel()
						delete(activeForms, d.ID)
					}
					formMu.Unlock()
				}
			case "session.execution.failed":
				partial.Text = strings.Join(texts, "\n")
				msg := "unknown error"
				if d.Error != nil && d.Error.Message != "" {
					msg = d.Error.Message
				}
				return partial, fmt.Errorf("opencode failed for bot %q: %s", bot.ID, msg)
			case "session.execution.interrupted":
				partial.Text = strings.Join(texts, "\n")
				return partial, fmt.Errorf("bot %q interrupted", bot.ID)
			case "session.execution.succeeded":
				dur := time.Since(started)
				text := strings.Join(texts, "\n")
				if isCompact && text == "" {
					text = "Session context compacted."
				}
				return &TurnResult{
					BotID:      bot.ID,
					SessionID:  sid,
					Text:       text,
					DurationMs: dur.Milliseconds(),
					Duration:   dur,
					Usage:      usage,
				}, nil
			}
		}
	}
}

func (h *OpenCodeHarness) Models(ctx context.Context) ([]string, error) {
	var fetchErr error
	ids := h.modelsCache.get(func() ([]string, bool) {
		h.mu.Lock()
		serverActive := h.external != "" || h.baseURL != ""
		h.mu.Unlock()

		if !serverActive {
			bin, ok := h.resolveBinary()
			if !ok {
				return nil, false
			}
			fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(fctx, bin, "models")
			cmd.Env = augmentedEnv(nil)
			if out, err := cmd.Output(); err == nil {
				if list := parseSlashModelLines(string(out)); len(list) > 0 {
					return list, true
				}
			}
		}

		base, pw, err := h.ensure(ctx)
		if err != nil {
			if serverActive {
				fetchErr = err
			}
			return nil, false
		}
		var out struct {
			Data []struct {
				ID         string `json:"id"`
				ProviderID string `json:"providerID"`
			} `json:"data"`
		}
		if _, err := h.apiClient(base, pw).do(ctx, http.MethodGet, "/api/model", nil, &out); err != nil {
			if serverActive {
				fetchErr = err
			}
			return nil, false
		}
		list := make([]string, 0, len(out.Data))
		for _, m := range out.Data {
			list = append(list, m.ProviderID+"/"+m.ID)
		}
		return list, len(list) > 0
	})
	if len(ids) == 0 && fetchErr != nil {
		return nil, fetchErr
	}
	return append([]string(nil), ids...), nil
}

func decodeOpenCodeDataList[T any](raw json.RawMessage) ([]T, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var wrapped struct {
		Data []T `json:"data"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Data != nil {
		return wrapped.Data, true
	}
	var arr []T
	if json.Unmarshal(raw, &arr) == nil {
		return arr, true
	}
	return nil, false
}

func (h *OpenCodeHarness) ListCommands(ctx context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "Compact current session context", Source: "harness"},
		{Name: "init", Description: "Initialize project AGENTS.md", Source: "harness"},
		{Name: "review", Description: "Review current code changes", Source: "harness"},
		{Name: "undo", Description: "Undo last turn changes", Source: "harness"},
		{Name: "redo", Description: "Redo undone changes", Source: "harness"},
		{Name: "new", Description: "Start a new session", Source: "harness"},
		{Name: "share", Description: "Share current session", Source: "harness"},
		{Name: "unshare", Description: "Unshare current session", Source: "harness"},
		{Name: "models", Description: "View or switch model", Source: "harness"},
		{Name: "agents", Description: "View or switch agent", Source: "harness"},
		{Name: "mcp", Description: "Show MCP server status", Source: "harness"},
		{Name: "sessions", Description: "List previous sessions", Source: "harness"},
		{Name: "export", Description: "Export current session", Source: "harness"},
		{Name: "help", Description: "Show available commands", Source: "harness"},
	}
	h.mu.Lock()
	ready := h.baseURL != "" || h.external != ""
	h.mu.Unlock()

	var remoteCmds, remoteSkills []SlashCommand
	if ready {
		if base, pw, err := h.ensure(ctx); err == nil {
			api := h.apiClient(base, pw)
			q := ""
			if bot.Workspace != "" {
				q = "?location[directory]=" + url.QueryEscape(bot.Workspace)
			}
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				var raw json.RawMessage
				type ocCmd struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				}
				if _, err := api.do(ctx, http.MethodGet, "/api/command"+q, nil, &raw); err == nil {
					if items, ok := decodeOpenCodeDataList[ocCmd](raw); ok {
						for _, c := range items {
							remoteCmds = append(remoteCmds, SlashCommand{
								Name:        c.Name,
								Description: c.Description,
								Source:      "harness",
							})
						}
					}
				}
			}()
			go func() {
				defer wg.Done()
				var raw json.RawMessage
				type ocSkill struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				}
				if _, err := api.do(ctx, http.MethodGet, "/api/skill"+q, nil, &raw); err == nil {
					if items, ok := decodeOpenCodeDataList[ocSkill](raw); ok {
						for _, s := range items {
							remoteSkills = append(remoteSkills, SlashCommand{
								Name:        s.Name,
								Description: s.Description,
								Source:      "skill",
							})
						}
					}
				}
			}()
			wg.Wait()
		}
	}

	var localCmds, localSkills []SlashCommand
	if bot.Workspace != "" {
		localCmds = append(localCmds, scanMarkdownCommands(filepath.Join(bot.Workspace, ".opencode", "commands"), "harness")...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(bot.Workspace, ".opencode", "skills"))...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(bot.Workspace, ".claude", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		localCmds = append(localCmds, scanMarkdownCommands(filepath.Join(home, ".config", "opencode", "commands"), "harness")...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(home, ".config", "opencode", "skills"))...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(home, ".claude", "skills"))...)
	}
	localSkills = append(localSkills, scanSharedSkills(bot.Workspace)...)

	return mergeSlashCommands(builtins, remoteCmds, remoteSkills, localCmds, localSkills), nil
}

func (h *OpenCodeHarness) ListAgents(ctx context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	h.mu.Lock()
	base, pw := h.baseURL, h.password
	h.mu.Unlock()

	var remote []AgentInfo
	if base != "" {
		q := ""
		if bot.Workspace != "" {
			q = "?location[directory]=" + url.QueryEscape(bot.Workspace)
		}
		var raw json.RawMessage
		type ocAgent struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Hidden      bool   `json:"hidden"`
		}
		if _, err := h.apiClient(base, pw).do(ctx, http.MethodGet, "/api/agent"+q, nil, &raw); err == nil {
			if items, ok := decodeOpenCodeDataList[ocAgent](raw); ok {
				for _, a := range items {
					if a.Hidden || a.Name == "" {
						continue
					}
					remote = append(remote, AgentInfo{Name: a.Name, Description: a.Description})
				}
			}
		}
	}

	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".opencode", "agents"))...)
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".claude", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".config", "opencode", "agents"))...)
		local = append(local, scanAgentsDir(filepath.Join(home, ".claude", "agents"))...)
	}

	return mergeAgents(remote, local), nil
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
	client   *http.Client
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
	client := a.client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
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
