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

	"bot-hub/internal/config"
)

// ClaudeHarness keeps one long-lived Claude Code process per bot, talking
// stream-json over stdin/stdout. The process is restarted (resuming the
// session) when the model changes, the session is reset, or it dies.
type ClaudeHarness struct {
	binary   string
	mu       sync.Mutex
	pool     procPool[*claudeProc]
	cmdCache map[string][]SlashCommand
}

type claudeProc struct {
	*lineProc
	model          string
	sessionID      string
	permissionMode string
	hasPromptTool  bool
}

type claudeEvent struct {
	Type          string   `json:"type"`
	Subtype       string   `json:"subtype"`
	SessionID     string   `json:"session_id"`
	SlashCommands []string `json:"slash_commands"`
	Skills        []string `json:"skills"`
	RequestID     string   `json:"request_id"`
	Request       *struct {
		Subtype               string         `json:"subtype"`
		ToolName              string         `json:"tool_name"`
		Input                 map[string]any `json:"input"`
		PermissionSuggestions []any          `json:"permission_suggestions"`
		DecisionReason        string         `json:"decision_reason"`
		BlockedPath           string         `json:"blocked_path"`
	} `json:"request"`
	Message *struct {
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

func (h *ClaudeHarness) PermissionCapabilities() PermissionCapabilities {
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

func claudeCLIPermMode(mode string) string {
	switch mode {
	case config.PermissionModeEdit:
		return "acceptEdits"
	case config.PermissionModeAsk:
		return "default"
	case config.PermissionModePlan:
		return "plan"
	default:
		return "bypassPermissions"
	}
}

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
		{Name: "help", Description: "查看可用命令帮助", Source: "harness"},
		{Name: "model", Description: "查看或切换模型", Source: "harness"},
		{Name: "fast", Description: "切换快速输出模式", Source: "harness"},
		{Name: "export", Description: "导出当前会话", Source: "harness"},
		{Name: "permissions", Description: "查看权限配置", Source: "harness"},
		{Name: "agents", Description: "查看或管理 Subagent", Source: "harness"},
		{Name: "skills", Description: "查看已安装 Skill", Source: "harness"},
		{Name: "hooks", Description: "查看钩子配置", Source: "harness"},
		{Name: "usage", Description: "查看订阅额度与用量", Source: "harness"},
		{Name: "rewind", Description: "回退会话到历史节点", Source: "harness"},
		{Name: "rename", Description: "重命名当前会话", Source: "harness"},
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
	localSkills = append(localSkills, scanSharedSkills(bot.Workspace)...)

	return mergeSlashCommands(cached, builtins, localCmds, localSkills), nil
}

func (h *ClaudeHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	builtins := []AgentInfo{
		{Name: "Explore", Description: "快速只读检索代码库"},
		{Name: "Plan", Description: "只读调研与方案设计"},
		{Name: "general-purpose", Description: "通用多步调研与执行代理"},
	}
	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".claude", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".claude", "agents"))...)
	}
	return mergeAgents(local, builtins), nil
}

func (h *ClaudeHarness) Close() {
	h.pool.close()
}

// Reload terminates cached claude processes so updated settings apply on the next turn.
func (h *ClaudeHarness) Reload() {
	h.pool.clear()
}

func (h *ClaudeHarness) proc(opts RunOptions) (*claudeProc, func(), error) {
	bot := opts.Bot
	mode := EffectivePermissionMode(opts)
	needPromptTool := mode != config.PermissionModeAuto
	return h.pool.acquire(
		bot.ID,
		func(p *claudeProc) bool {
			return p.model == bot.Model &&
				p.sessionID == opts.SessionID &&
				p.permissionMode == mode &&
				p.hasPromptTool == needPromptTool
		},
		func() (*claudeProc, error) {
			return h.startProc(opts, false)
		},
	)
}

func (h *ClaudeHarness) startProc(opts RunOptions, ephemeral bool) (*claudeProc, error) {
	bot := opts.Bot
	mode := EffectivePermissionMode(opts)
	needPromptTool := mode != config.PermissionModeAuto

	// Claude Code reads CLAUDE.md; bridge _shared/PEBBLE.md and @AGENTS.md
	// unless the user already customized CLAUDE.md (auto-migrating legacy BOT_HUB.md bridge).
	claudeMD := filepath.Join(bot.Workspace, "CLAUDE.md")
	const (
		wantClaudeMD   = "@_shared/" + config.PlatformInstructionsFilename + "\n@AGENTS.md\n"
		legacyClaudeMD = "@_shared/" + config.LegacyPlatformInstructionsFilename + "\n@AGENTS.md\n"
	)
	if cur, err := os.ReadFile(claudeMD); os.IsNotExist(err) || (err == nil && (string(cur) == "@AGENTS.md\n" || string(cur) == legacyClaudeMD)) {
		_ = os.WriteFile(claudeMD, []byte(wantClaudeMD), 0o644)
	}
	_ = config.EnsureClaudeWorkspaceSkill(bot.Workspace)

	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
	if bot.Model != "" {
		args = append(args, "--model", bot.Model)
	}
	if ephemeral {
		args = append(args, "--no-session-persistence")
	} else if opts.SessionID != "" {
		args = append(args, "--resume", opts.SessionID)
	}
	args = append(args, "--permission-mode", claudeCLIPermMode(mode))
	if needPromptTool {
		args = append(args, "--permission-prompt-tool", "stdio")
	}
	if len(opts.MCPCommand) > 0 {
		mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
			config.MCPServerName: map[string]any{"command": opts.MCPCommand[0], "args": opts.MCPCommand[1:], "env": opts.MCPEnv},
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
	return &claudeProc{
		lineProc:       lp,
		model:          bot.Model,
		sessionID:      opts.SessionID,
		permissionMode: mode,
		hasPromptTool:  needPromptTool,
	}, nil
}

func (h *ClaudeHarness) drop(botID string, p *claudeProc) {
	h.pool.drop(botID, p)
}

func (h *ClaudeHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withTurnTimeout(ctx, bot.TimeoutSec, opts)
	defer cancel()

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
		var release func()
		p, release, err = h.proc(opts)
		if err != nil {
			return nil, err
		}
		defer release()
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
	for _, img := range readImageAttachments(opts.Attachments) {
		blocks = append(blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": img.MimeType,
				"data":       img.DataB64,
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
	var permMu sync.Mutex
	activePerms := make(map[string]context.CancelFunc)
	cancelAllPerms := func() {
		permMu.Lock()
		for id, c := range activePerms {
			c()
			delete(activePerms, id)
		}
		permMu.Unlock()
	}
	defer cancelAllPerms()

	for {
		select {
		case <-ctx.Done():
			// Killing is the interrupt; the next turn resumes the session.
			cancelAllPerms()
			partial.Text = strings.Join(texts, "\n")
			dropProc()
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)

		case line, ok := <-p.lines:
			if !ok {
				cancelAllPerms()
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
			case "control_request":
				if evt.RequestID == "" || evt.Request == nil {
					continue
				}
				if evt.Request.Subtype != "can_use_tool" {
					_ = p.send(map[string]any{
						"type": "control_response",
						"response": map[string]any{
							"subtype":    "error",
							"request_id": evt.RequestID,
							"error":      fmt.Sprintf("unsupported control_request subtype %q", evt.Request.Subtype),
						},
					})
					continue
				}
				reqID := evt.RequestID
				toolName := strings.TrimSpace(evt.Request.ToolName)
				if toolName == "" {
					toolName = "tool"
				}
				origInput := evt.Request.Input
				suggestions := evt.Request.PermissionSuggestions
				title, summary := SummarizeToolCall(toolName, origInput)
				if summary == "" {
					summary = strings.TrimSpace(evt.Request.BlockedPath)
				}
				if summary == "" {
					summary = strings.TrimSpace(evt.Request.DecisionReason)
				}
				permReq := PermissionRequest{
					ID:       reqID,
					ToolName: toolName,
					Kind:     ClassifyToolKind(toolName),
					Title:    title,
					Summary:  summary,
					RawInput: origInput,
					Options:  StandardPermissionOptions(true),
				}
				pCtx, pCancel := context.WithCancel(ctx)
				permMu.Lock()
				if prev, exists := activePerms[reqID]; exists {
					prev()
				}
				activePerms[reqID] = pCancel
				permMu.Unlock()
				go func() {
					defer pCancel()
					resp := ResolveApprovalDecision(pCtx, opts, permReq)
					permMu.Lock()
					delete(activePerms, reqID)
					permMu.Unlock()
					if !p.alive() {
						return
					}
					if resp.Decision == "allow_once" || resp.Decision == "allow_always" {
						updatedInput := origInput
						if updatedInput == nil {
							updatedInput = map[string]any{}
						}
						allowBody := map[string]any{
							"behavior":     "allow",
							"updatedInput": updatedInput,
						}
						if resp.Decision == "allow_always" && len(suggestions) > 0 {
							allowBody["updatedPermissions"] = suggestions
						}
						_ = p.send(map[string]any{
							"type": "control_response",
							"response": map[string]any{
								"subtype":    "success",
								"request_id": reqID,
								"response":   allowBody,
							},
						})
						return
					}
					reason := strings.TrimSpace(resp.Reason)
					if reason == "" {
						reason = "User denied permission"
					}
					_ = p.send(map[string]any{
						"type": "control_response",
						"response": map[string]any{
							"subtype":    "success",
							"request_id": reqID,
							"response": map[string]any{
								"behavior": "deny",
								"message":  reason,
							},
						},
					})
				}()
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
