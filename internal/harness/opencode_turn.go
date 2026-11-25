package harness

import (
	"bot-hub/internal/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

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

// modelRef splits "provider/model/id" into opencode's {providerID, id}.
func modelRef(model string) map[string]string {
	provider, id, ok := strings.Cut(model, "/")
	if !ok || provider == "" || id == "" {
		return nil
	}
	return map[string]string{"providerID": provider, "id": id}
}
