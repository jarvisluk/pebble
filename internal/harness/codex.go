package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// CodexHarness runs OpenAI Codex CLI in non-interactive JSONL mode (`codex exec --json --full-auto`).
type CodexHarness struct {
	binary string
}

func NewCodexHarness() *CodexHarness {
	return &CodexHarness{
		binary: envOr("CODEX_BIN", "codex"),
	}
}

func (h *CodexHarness) Kind() string  { return "codex" }
func (h *CodexHarness) Label() string { return "Codex" }

func (h *CodexHarness) Models(context.Context) ([]string, error) {
	return []string{"gpt-5.4", "gpt-5.3-codex", "gpt-5.2-codex", "o3"}, nil
}

func (h *CodexHarness) Close() {}

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

	bin := h.binary
	if bin == "codex" {
		if resolved, ok := findBinary("codex"); ok {
			bin = resolved
		}
	}

	args := []string{"exec"}
	if opts.SessionID != "" {
		args = append(args, "resume", opts.SessionID)
	}
	args = append(args, "--json", "--full-auto")
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
	cmd.Env = augmentedEnv(opts.ExtraEnv)
	cmd.Stdin = strings.NewReader(opts.Prompt)
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
		trimmed := strings.TrimSpace(string(line))
		if trimmed != "" {
			var evt codexEvent
			if json.Unmarshal([]byte(trimmed), &evt) == nil {
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
