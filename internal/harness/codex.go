package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bot-hub/internal/config"
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

func (h *CodexHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "压缩当前会话上下文", Source: "harness"},
		{Name: "new", Description: "开启新会话", Source: "harness"},
		{Name: "clear", Description: "清空会话历史", Source: "harness"},
		{Name: "review", Description: "评审当前代码变更", Source: "harness"},
		{Name: "plan", Description: "进入计划模式", Source: "harness"},
		{Name: "model", Description: "查看或切换模型", Source: "harness"},
		{Name: "diff", Description: "查看工作区变更", Source: "harness"},
		{Name: "mention", Description: "引用文件或目录", Source: "harness"},
		{Name: "mcp", Description: "查看 MCP 服务状态", Source: "harness"},
		{Name: "init", Description: "初始化项目 AGENTS.md", Source: "harness"},
		{Name: "status", Description: "查看当前会话状态", Source: "harness"},
		{Name: "help", Description: "查看可用命令帮助", Source: "harness"},
	}
	var local []SlashCommand
	if bot.Workspace != "" {
		local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, ".codex", "prompts"), "harness")...)
		local = append(local, scanSkillDirs(filepath.Join(bot.Workspace, ".codex", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanMarkdownCommands(filepath.Join(home, ".codex", "prompts"), "harness")...)
		local = append(local, scanSkillDirs(filepath.Join(home, ".codex", "skills"))...)
	}
	local = append(local, scanSharedSkills(bot.Workspace)...)
	return mergeSlashCommands(builtins, local), nil
}

func (h *CodexHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".codex", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".codex", "agents"))...)
	}
	return mergeAgents(local), nil
}

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
	ctx, cancel := withBotTimeout(ctx, bot.TimeoutSec)
	defer cancel()

	bin := h.binary
	if bin == "codex" {
		if resolved, ok := findBinary("codex"); ok {
			bin = resolved
		}
	}

	args := []string{"exec"}
	if !opts.Ephemeral && opts.SessionID != "" {
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
