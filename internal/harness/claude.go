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
)

// ClaudeHarness keeps one long-lived Claude Code process per bot, talking
// stream-json over stdin/stdout. The process is restarted (resuming the
// session) when the model changes, the session is reset, or it dies.
type ClaudeHarness struct {
	binary string
	mu     sync.Mutex
	procs  map[string]*claudeProc
}

type claudeProc struct {
	*lineProc
	model     string
	sessionID string
}

type claudeEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Message   *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Name string `json:"name"`
		} `json:"content"`
	} `json:"message"`
	Result    string  `json:"result"`
	IsError   bool    `json:"is_error"`
	TotalCost float64 `json:"total_cost_usd"`
	Usage     *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		CacheRead    int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func NewClaudeHarness() *ClaudeHarness {
	return &ClaudeHarness{binary: envOr("CLAUDE_BIN", "claude"), procs: make(map[string]*claudeProc)}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (h *ClaudeHarness) Kind() string  { return "claude" }
func (h *ClaudeHarness) Label() string { return "Claude Code" }

// Models returns Claude Code's aliases; any model ID the CLI accepts
// (e.g. one routed through a gateway) can also be typed in.
func (h *ClaudeHarness) Models(context.Context) ([]string, error) {
	return []string{"sonnet", "opus", "haiku"}, nil
}

func (h *ClaudeHarness) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, p := range h.procs {
		p.kill()
		delete(h.procs, id)
	}
}

// Reload terminates cached claude processes so updated settings apply on the next turn.
func (h *ClaudeHarness) Reload() {
	h.Close()
}

func (h *ClaudeHarness) proc(opts RunOptions) (*claudeProc, error) {
	bot := opts.Bot
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.procs[bot.ID]; p != nil {
		if p.alive() && p.model == bot.Model && p.sessionID == opts.SessionID {
			return p, nil
		}
		p.kill()
		delete(h.procs, bot.ID)
	}

	// Claude Code reads CLAUDE.md; the workspace instructions live in AGENTS.md.
	claudeMD := filepath.Join(bot.Workspace, "CLAUDE.md")
	if _, err := os.Stat(claudeMD); os.IsNotExist(err) {
		_ = os.WriteFile(claudeMD, []byte("@AGENTS.md\n"), 0o644)
	}

	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
	if bot.Model != "" {
		args = append(args, "--model", bot.Model)
	}
	if opts.SessionID != "" {
		args = append(args, "--resume", opts.SessionID)
	}
	if bot.ShouldAutoApprove() {
		args = append(args, "--permission-mode", "bypassPermissions")
	}
	if len(opts.MCPCommand) > 0 {
		mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
			"bot-hub": map[string]any{"command": opts.MCPCommand[0], "args": opts.MCPCommand[1:], "env": opts.MCPEnv},
		}})
		args = append(args, "--mcp-config", string(mcp))
	}

	cmd := exec.Command(h.binary, args...)
	cmd.Dir = bot.Workspace
	// Drop a parent Claude Code session's variables (bot-hub may run inside
	// one): they override the gateway settings magpie writes to
	// ~/.claude/settings.json. A bot's own env still applies.
	cmd.Env = nil
	for _, kv := range cmd.Environ() {
		if !strings.HasPrefix(kv, "ANTHROPIC_") && !strings.HasPrefix(kv, "CLAUDECODE=") &&
			!strings.HasPrefix(kv, "CLAUDE_CODE_") && !strings.HasPrefix(kv, "CLAUDE_AGENT_SDK_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = procEnv(cmd, opts)
	lp, err := startLineProc(cmd)
	if err != nil {
		return nil, err
	}
	p := &claudeProc{lineProc: lp, model: bot.Model, sessionID: opts.SessionID}
	h.procs[bot.ID] = p
	return p, nil
}

func (h *ClaudeHarness) drop(botID string, p *claudeProc) {
	h.mu.Lock()
	if h.procs[botID] == p {
		delete(h.procs, botID)
	}
	h.mu.Unlock()
	p.kill()
}

func (h *ClaudeHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	if bot.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(bot.TimeoutSec)*time.Second)
		defer cancel()
	}

	p, err := h.proc(opts)
	if err != nil {
		return nil, err
	}
	err = p.send(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": opts.Prompt},
	})
	if err != nil {
		h.drop(bot.ID, p)
		return nil, fmt.Errorf("claude process: %w", err)
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: opts.SessionID}
	var texts []string
	for {
		select {
		case <-ctx.Done():
			// Killing is the interrupt; the next turn resumes the session.
			h.drop(bot.ID, p)
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)

		case line, ok := <-p.lines:
			if !ok {
				h.drop(bot.ID, p)
				return partial, fmt.Errorf("claude failed for bot %q: %s", bot.ID, p.exitDetail())
			}
			var evt claudeEvent
			if json.Unmarshal(line, &evt) != nil {
				continue
			}
			if evt.SessionID != "" {
				partial.SessionID = evt.SessionID
				p.sessionID = evt.SessionID
			}
			switch evt.Type {
			case "assistant":
				if evt.Message == nil {
					continue
				}
				for _, c := range evt.Message.Content {
					switch c.Type {
					case "text":
						if t := strings.TrimSpace(c.Text); t != "" {
							texts = append(texts, t)
							if opts.OnText != nil {
								opts.OnText(c.Text)
							}
						}
					case "tool_use":
						if opts.OnToolUse != nil {
							opts.OnToolUse(c.Name)
						}
					}
				}
			case "result":
				if evt.IsError {
					return partial, fmt.Errorf("claude failed for bot %q: %s", bot.ID, evt.Result)
				}
				text := strings.TrimSpace(evt.Result)
				if text == "" {
					text = strings.Join(texts, "\n")
				}
				usage := Usage{CostUSD: evt.TotalCost}
				if evt.Usage != nil {
					usage.InputTokens = evt.Usage.InputTokens
					usage.OutputTokens = evt.Usage.OutputTokens
					usage.CacheReadTokens = evt.Usage.CacheRead
				}
				return finish(partial, text, usage, started), nil
			}
		}
	}
}

func ctxErr(ctx context.Context, botID string, timeoutSec int) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("bot %q timed out after %ds", botID, timeoutSec)
	}
	return fmt.Errorf("bot %q interrupted", botID)
}

func finish(partial *TurnResult, text string, usage Usage, started time.Time) *TurnResult {
	dur := time.Since(started)
	return &TurnResult{
		BotID:      partial.BotID,
		SessionID:  partial.SessionID,
		Text:       text,
		DurationMs: dur.Milliseconds(),
		Duration:   dur,
		Usage:      usage,
	}
}
