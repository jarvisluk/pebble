package harness

import (
	"bot-hub/internal/config"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
	Item *struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Text    string `json:"text"`
		Command string `json:"command"`
		Tool    string `json:"tool"`
	} `json:"item"`
	Usage *struct {
		InputTokens       int `json:"input_tokens"`
		OutputTokens      int `json:"output_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
	} `json:"usage"`
}

func (h *CodexHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withTurnTimeout(ctx, bot.TimeoutSec, opts)
	defer cancel()

	bin := h.effectiveBinary()
	if h.supportsAppServer(bin) {
		res, fallback, err := h.runAppServerTurn(ctx, bin, opts, started)
		if !fallback {
			return res, err
		}
	}
	return h.runExecTurn(ctx, bin, opts, started)
}

func (h *CodexHarness) runAppServerTurn(ctx context.Context, bin string, opts RunOptions, started time.Time) (res *TurnResult, fallback bool, err error) {
	bot := opts.Bot
	var p *codexAppProc
	if opts.Ephemeral {
		p, err = h.startAppServerProc(ctx, bin, opts, true)
		if err != nil {
			if errors.Is(err, errCodexAppServerInit) {
				return nil, true, nil
			}
			return nil, false, err
		}
		defer p.kill()
	} else {
		var release func()
		p, release, err = h.appProcFor(ctx, bin, opts)
		if err != nil {
			if errors.Is(err, errCodexAppServerInit) {
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
			h.pool.drop(bot.ID, p)
		}
	}

	p.setTurn(ctx, opts)
	defer func() {
		p.cancelAllPerms()
		p.setTurn(context.Background(), RunOptions{})
	}()

	mode := EffectivePermissionMode(opts)
	approvalPolicy, _, sandboxPolicyType := codexPolicies(mode)

	cwd := bot.Workspace
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	inputItems := []map[string]any{
		{
			"type": "text",
			"text": AppendAttachmentContext(opts.Prompt, opts.Attachments),
		},
	}
	for _, att := range opts.Attachments {
		if att.Kind == "image" && att.Path != "" {
			inputItems = append(inputItems, map[string]any{
				"type": "localImage",
				"path": att.Path,
			})
		}
	}

	turnStartParams := map[string]any{
		"threadId":       p.threadID,
		"input":          inputItems,
		"cwd":            cwd,
		"approvalPolicy": approvalPolicy,
		"sandboxPolicy":  map[string]any{"type": sandboxPolicyType},
	}
	if m := strings.TrimSpace(bot.Model); m != "" && m != "default" {
		turnStartParams["model"] = m
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: p.threadID}
	var (
		textBuf     strings.Builder
		fallbackMsg string
		tools       []string
		usage       Usage
		turnID      string
		turnDone    bool
		turnFailed  string
		interrupted bool
	)

	onNotify := func(msg rpcMessage) {
		switch msg.Method {
		case "item/agentMessage/delta":
			var d struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal(msg.Params, &d) == nil && d.Delta != "" {
				textBuf.WriteString(d.Delta)
				if opts.OnText != nil {
					opts.OnText(d.Delta)
				}
			}
		case "item/started":
			var pld struct {
				Item *struct {
					Type string `json:"type"`
					Tool string `json:"tool"`
				} `json:"item"`
			}
			if json.Unmarshal(msg.Params, &pld) == nil && pld.Item != nil {
				switch pld.Item.Type {
				case "commandExecution":
					tools = append(tools, "shell")
					if opts.OnToolUse != nil {
						opts.OnToolUse("shell")
					}
				case "fileChange":
					tools = append(tools, "file_change")
					if opts.OnToolUse != nil {
						opts.OnToolUse("file_change")
					}
				case "mcpToolCall":
					name := strings.TrimSpace(pld.Item.Tool)
					if name == "" {
						name = "mcp"
					}
					tools = append(tools, name)
					if opts.OnToolUse != nil {
						opts.OnToolUse(name)
					}
				}
			}
		case "item/completed":
			var pld struct {
				Item *struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(msg.Params, &pld) == nil && pld.Item != nil && pld.Item.Type == "agentMessage" {
				if t := strings.TrimSpace(pld.Item.Text); t != "" {
					fallbackMsg = t
				}
			}
		case "thread/tokenUsage/updated":
			var tu struct {
				TokenUsage *struct {
					Last *struct {
						InputTokens       int `json:"inputTokens"`
						CachedInputTokens int `json:"cachedInputTokens"`
						OutputTokens      int `json:"outputTokens"`
					} `json:"last"`
				} `json:"tokenUsage"`
			}
			if json.Unmarshal(msg.Params, &tu) == nil && tu.TokenUsage != nil && tu.TokenUsage.Last != nil {
				usage.InputTokens = tu.TokenUsage.Last.InputTokens
				usage.OutputTokens = tu.TokenUsage.Last.OutputTokens
				usage.CacheReadTokens = tu.TokenUsage.Last.CachedInputTokens
			}
		case "turn/completed":
			var tc struct {
				Turn struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Error  *struct {
						Message string `json:"message"`
					} `json:"error"`
				} `json:"turn"`
			}
			if json.Unmarshal(msg.Params, &tc) == nil {
				if turnID == "" || tc.Turn.ID == "" || tc.Turn.ID == turnID {
					turnDone = true
					switch tc.Turn.Status {
					case "interrupted":
						interrupted = true
					case "failed":
						if tc.Turn.Error != nil && tc.Turn.Error.Message != "" {
							turnFailed = tc.Turn.Error.Message
						} else {
							turnFailed = "turn failed"
						}
					}
				}
			}
		case "error":
			var e struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(msg.Params, &e) == nil && strings.TrimSpace(e.Message) != "" {
				turnFailed = e.Message
			}
		}
	}

	rawStart, err := p.call(ctx, "turn/start", turnStartParams, onNotify)
	if err != nil {
		partial.Text = strings.TrimSpace(textBuf.String())
		partial.Tools = tools
		dropProc()
		if ctx.Err() != nil {
			return partial, false, ctxErr(ctx, bot.ID, bot.TimeoutSec)
		}
		return partial, false, fmt.Errorf("codex failed for bot %q: %w", bot.ID, err)
	}
	var startOut struct {
		Turn *struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(rawStart, &startOut)
	if startOut.Turn != nil {
		turnID = startOut.Turn.ID
	}

	for !turnDone {
		select {
		case <-ctx.Done():
			p.cancelAllPerms()
			p.sendInterrupt(turnID)
			p.drainBrieflyOnInterrupt(onNotify, func() bool { return turnDone })
			partial.Text = strings.TrimSpace(textBuf.String())
			if partial.Text == "" {
				partial.Text = fallbackMsg
			}
			partial.Tools = tools
			dropProc()
			return partial, false, ctxErr(ctx, bot.ID, bot.TimeoutSec)
		case line, ok := <-p.lines:
			if !ok {
				p.cancelAllPerms()
				partial.Text = strings.TrimSpace(textBuf.String())
				if partial.Text == "" {
					partial.Text = fallbackMsg
				}
				partial.Tools = tools
				dropProc()
				return partial, false, fmt.Errorf("codex failed for bot %q: %s", bot.ID, p.exitDetail())
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
				go p.handleServerRequest(msg)
				continue
			}
			if msg.Method != "" {
				onNotify(msg)
			}
		}
	}

	finalText := strings.TrimSpace(textBuf.String())
	if finalText == "" {
		finalText = fallbackMsg
	} else if opts.OnText == nil && fallbackMsg != "" && len(fallbackMsg) > len(finalText) {
		finalText = fallbackMsg
	}
	partial.Tools = tools
	if interrupted {
		partial.Text = finalText
		return partial, false, fmt.Errorf("bot %q interrupted", bot.ID)
	}
	if turnFailed != "" {
		partial.Text = finalText
		return partial, false, fmt.Errorf("codex failed for bot %q: %s", bot.ID, turnFailed)
	}
	res = finish(partial, finalText, usage, started)
	res.Tools = tools
	return res, false, nil
}

func codexExecModeArgs(mode string) []string {
	switch mode {
	case config.PermissionModeEdit:
		return []string{"--sandbox", "workspace-write", "--approve-for-me"}
	case config.PermissionModeAsk:
		return []string{"--sandbox", "workspace-write"}
	case config.PermissionModePlan:
		return []string{"--sandbox", "read-only"}
	default:
		return []string{"--full-auto"}
	}
}

func (h *CodexHarness) runExecTurn(ctx context.Context, bin string, opts RunOptions, started time.Time) (*TurnResult, error) {
	bot := opts.Bot
	mode := EffectivePermissionMode(opts)

	args := []string{"exec"}
	if !opts.Ephemeral && opts.SessionID != "" {
		args = append(args, "resume", opts.SessionID)
	}
	args = append(args, "--json")
	args = append(args, codexExecModeArgs(mode)...)
	if m := strings.TrimSpace(bot.Model); m != "" && m != "default" {
		args = append(args, "--model", m)
	}
	for _, att := range opts.Attachments {
		if att.Kind == "image" && att.Path != "" {
			args = append(args, "--image", att.Path)
		}
	}
	// Read prompt from stdin via "-".
	args = append(args, "-")

	cmd := exec.Command(bin, args...)
	if bot.Workspace != "" {
		cmd.Dir = bot.Workspace
	}
	cmd.Env = mergedEnv(bot.Env, opts.ExtraEnv)
	cmd.Stdin = strings.NewReader(AppendAttachmentContext(opts.Prompt, opts.Attachments))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stderr := &tailWriter{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex: %w", err)
	}

	doneWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-doneWatch:
		}
	}()

	partial := &TurnResult{BotID: bot.ID, SessionID: opts.SessionID}
	var lastText string
	var texts []string
	var tools []string
	var usage Usage
	var errMsg string

	r := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, readErr := r.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var evt codexEvent
			if json.Unmarshal(trimmed, &evt) == nil {
				if evt.ThreadID != "" {
					partial.SessionID = evt.ThreadID
				}
				switch evt.Type {
				case "item.updated", "item.completed":
					if evt.Item != nil {
						switch evt.Item.Type {
						case "agent_message":
							if t := strings.TrimSpace(evt.Item.Text); t != "" {
								if evt.Type == "item.completed" {
									texts = append(texts, t)
								}
								if strings.HasPrefix(evt.Item.Text, lastText) {
									delta := strings.TrimPrefix(evt.Item.Text, lastText)
									if delta != "" && opts.OnText != nil {
										opts.OnText(delta)
									}
								} else if opts.OnText != nil {
									opts.OnText(evt.Item.Text)
								}
								lastText = evt.Item.Text
							}
						case "command_execution":
							tools = append(tools, "shell")
							if opts.OnToolUse != nil {
								opts.OnToolUse("shell")
							}
						case "mcp_tool_call":
							name := evt.Item.Tool
							if name == "" {
								name = "mcp"
							}
							tools = append(tools, name)
							if opts.OnToolUse != nil {
								opts.OnToolUse(name)
							}
						}
					}
				case "turn.completed":
					if evt.Usage != nil {
						usage.InputTokens = evt.Usage.InputTokens
						usage.OutputTokens = evt.Usage.OutputTokens
						usage.CacheReadTokens = evt.Usage.CachedInputTokens
					}
				case "error":
					if evt.Message != "" {
						errMsg = evt.Message
					} else if evt.Error != nil && evt.Error.Message != "" {
						errMsg = evt.Error.Message
					}
				case "turn.failed":
					if evt.Error != nil && evt.Error.Message != "" {
						errMsg = evt.Error.Message
					}
				}
			}
		}
		if readErr != nil {
			break
		}
	}

	waitErr := cmd.Wait()
	close(doneWatch)

	if ctx.Err() != nil {
		partial.Text = strings.Join(texts, "\n")
		partial.Tools = tools
		return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)
	}
	if errMsg != "" {
		partial.Text = strings.Join(texts, "\n")
		partial.Tools = tools
		return partial, fmt.Errorf("codex failed for bot %q: %s", bot.ID, errMsg)
	}
	if waitErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = waitErr.Error()
		}
		partial.Text = strings.Join(texts, "\n")
		partial.Tools = tools
		return partial, fmt.Errorf("codex failed for bot %q: %s", bot.ID, detail)
	}

	finalText := strings.TrimSpace(strings.Join(texts, "\n"))
	if finalText == "" {
		finalText = strings.TrimSpace(lastText)
	}
	res := finish(partial, finalText, usage, started)
	res.Tools = tools
	return res, nil
}
