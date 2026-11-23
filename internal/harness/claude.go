package harness

import (
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

// ClaudeHarness keeps one long-lived Claude Code process per bot, talking
// stream-json over stdin/stdout. The process is restarted (resuming the
// session) when the model changes, the session is reset, or it dies.
type ClaudeHarness struct {
	binary   string
	mu       sync.Mutex
	procs    map[string]*claudeProc
	cmdCache map[string][]SlashCommand
}

type claudeProc struct {
	*lineProc
	model     string
	sessionID string
}

type claudeEvent struct {
	Type          string   `json:"type"`
	Subtype       string   `json:"subtype"`
	SessionID     string   `json:"session_id"`
	SlashCommands []string `json:"slash_commands"`
	Skills        []string `json:"skills"`
	Message       *struct {
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
	return &ClaudeHarness{
		binary:   envOr("CLAUDE_BIN", "claude"),
		procs:    make(map[string]*claudeProc),
		cmdCache: make(map[string][]SlashCommand),
	}
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

func (h *ClaudeHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "压缩当前会话上下文", Source: "harness"},
		{Name: "clear", Description: "清空会话历史", Source: "harness"},
		{Name: "cost", Description: "查看本次会话费用统计", Source: "harness"},
		{Name: "context", Description: "查看当前上下文占用", Source: "harness"},
		{Name: "memory", Description: "查看或编辑记忆文件", Source: "harness"},
		{Name: "review", Description: "代码评审", Source: "harness"},
		{Name: "init", Description: "初始化项目 CLAUDE.md", Source: "harness"},
		{Name: "plan", Description: "进入计划模式", Source: "harness"},
		{Name: "mcp", Description: "查看 MCP 服务状态", Source: "harness"},
		{Name: "doctor", Description: "检查运行环境健康状态", Source: "harness"},
	}

	h.mu.Lock()
	cached := append([]SlashCommand(nil), h.cmdCache[bot.ID]...)
	h.mu.Unlock()

	var localCmds, localSkills []SlashCommand
	if bot.Workspace != "" {
		localCmds = append(localCmds, scanMarkdownCommands(filepath.Join(bot.Workspace, ".claude", "commands"), "harness")...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(bot.Workspace, ".claude", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		localCmds = append(localCmds, scanMarkdownCommands(filepath.Join(home, ".claude", "commands"), "harness")...)
		localSkills = append(localSkills, scanSkillDirs(filepath.Join(home, ".claude", "skills"))...)
	}

	return mergeSlashCommands(cached, builtins, localCmds, localSkills), nil
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

	p, err := h.startProc(opts, false)
	if err != nil {
		return nil, err
	}
	h.procs[bot.ID] = p
	return p, nil
}

func (h *ClaudeHarness) startProc(opts RunOptions, ephemeral bool) (*claudeProc, error) {
	bot := opts.Bot

	// Claude Code reads CLAUDE.md; bridge _shared/BOT_HUB.md and @AGENTS.md
	// unless the user already customized CLAUDE.md.
	claudeMD := filepath.Join(bot.Workspace, "CLAUDE.md")
	const wantClaudeMD = "@_shared/BOT_HUB.md\n@AGENTS.md\n"
	if cur, err := os.ReadFile(claudeMD); os.IsNotExist(err) || (err == nil && string(cur) == "@AGENTS.md\n") {
		_ = os.WriteFile(claudeMD, []byte(wantClaudeMD), 0o644)
	}

	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
	if bot.Model != "" {
		args = append(args, "--model", bot.Model)
	}
	if ephemeral {
		args = append(args, "--no-session-persistence")
	} else if opts.SessionID != "" {
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
	return &claudeProc{lineProc: lp, model: bot.Model, sessionID: opts.SessionID}, nil
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

	var (
		p   *claudeProc
		err error
	)
	if opts.Ephemeral {
		p, err = h.startProc(opts, true)
		if err != nil {
			return nil, err
		}
		defer p.kill()
	} else {
		p, err = h.proc(opts)
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

	promptText := AppendAttachmentContext(opts.Prompt, opts.Attachments)
	var userContent any = promptText
	var blocks []map[string]any
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
		blocks = append(blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mt,
				"data":       base64.StdEncoding.EncodeToString(raw),
			},
		})
	}
	if len(blocks) > 0 {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": promptText,
		})
		userContent = blocks
	}

	err = p.send(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": userContent},
	})
	if err != nil {
		dropProc()
		return nil, fmt.Errorf("claude process: %w", err)
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: opts.SessionID}
	var texts []string
	for {
		select {
		case <-ctx.Done():
			// Killing is the interrupt; the next turn resumes the session.
			partial.Text = strings.Join(texts, "\n")
			dropProc()
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)

		case line, ok := <-p.lines:
			if !ok {
				partial.Text = strings.Join(texts, "\n")
				dropProc()
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
			case "system":
				if evt.Subtype == "init" && (len(evt.SlashCommands) > 0 || len(evt.Skills) > 0) {
					var list []SlashCommand
					for _, name := range evt.SlashCommands {
						list = append(list, SlashCommand{Name: name, Source: "harness"})
					}
					for _, name := range evt.Skills {
						list = append(list, SlashCommand{Name: name, Source: "skill"})
					}
					h.mu.Lock()
					if h.cmdCache == nil {
						h.cmdCache = make(map[string][]SlashCommand)
					}
					h.cmdCache[bot.ID] = list
					h.mu.Unlock()
				}
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
					partial.Text = strings.Join(texts, "\n")
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
