package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
)

// AgyHarness keeps one long-lived Antigravity CLI (`agy`) process per bot,
// speaking NDJSON stream-json over stdin/stdout.
type AgyHarness struct {
	binary   string
	pool     procPool[*agyProc]
	modelsMu sync.Mutex
	models   []string
}

type agyProc struct {
	*lineProc
	model         string
	sessionID     string
	viaMagpie     bool
	prevInput     int
	prevOutput    int
	prevCacheRead int
}

type agyEvent struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
	StepUpdate     *struct {
		ConversationID string `json:"conversation_id"`
		StepIndex      int    `json:"step_index"`
		State          string `json:"state"`
		StepType       string `json:"step_type"`
		ToolName       string `json:"tool_name"`
		TextDelta      string `json:"text_delta"`
	} `json:"step_update"`
	Result *struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error"`
		Usage          *struct {
			InputTokens     int `json:"input_tokens"`
			OutputTokens    int `json:"output_tokens"`
			CacheReadTokens int `json:"cache_read_tokens"`
		} `json:"usage"`
	} `json:"result"`
}

func NewAgyHarness() *AgyHarness {
	return &AgyHarness{binary: resolveAgyBin()}
}

func resolveAgyBin() string {
	if v := strings.TrimSpace(os.Getenv("AGY_BIN")); v != "" {
		return v
	}
	if p, ok := findBinary("agy"); ok {
		return p
	}
	return "agy"
}

func (h *AgyHarness) resolveBinary() (string, bool) {
	return resolveBinaryFor("AGY_BIN", "agy", h.binary)
}

func (h *AgyHarness) Kind() string  { return "agy" }
func (h *AgyHarness) Label() string { return "Antigravity CLI" }

func (h *AgyHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "压缩当前会话上下文", Source: "harness"},
		{Name: "plan", Description: "切换计划模式", Source: "harness"},
		{Name: "clear", Description: "清空会话上下文", Source: "harness"},
		{Name: "goal", Description: "开启长程目标执行", Source: "harness"},
		{Name: "schedule", Description: "创建定时任务或提醒", Source: "harness"},
		{Name: "browser", Description: "启用浏览器交互任务", Source: "harness"},
		{Name: "grill-me", Description: "通过问答访谈对齐方案", Source: "harness"},
		{Name: "teamwork", Description: "启动多代理协作", Source: "harness"},
		{Name: "learn", Description: "沉淀本次经验到配置", Source: "harness"},
		{Name: "boost", Description: "启用深度思考与严格验证", Source: "harness"},
		{Name: "plugin", Description: "管理或创建插件", Source: "harness"},
		{Name: "automation", Description: "配置后台自动化任务", Source: "harness"},
		{Name: "help", Description: "查看可用命令帮助", Source: "harness"},
	}
	var local []SlashCommand
	if bot.Workspace != "" {
		local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, ".agents", "workflows"), "workflow")...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanSkillDirs(filepath.Join(home, ".gemini", "config", "skills"))...)
		local = append(local, scanSkillDirs(filepath.Join(home, ".gemini", "antigravity", "builtin", "skills"))...)
	}
	local = append(local, scanSharedSkills(bot.Workspace)...)
	return mergeSlashCommands(builtins, local), nil
}

func (h *AgyHarness) ListAgents(_ context.Context, bot config.BotConfig) ([]AgentInfo, error) {
	var local []AgentInfo
	if bot.Workspace != "" {
		local = append(local, scanAgentsDir(filepath.Join(bot.Workspace, ".agents", "agents"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanAgentsDir(filepath.Join(home, ".gemini", "config", "agents"))...)
	}
	return mergeAgents(local), nil
}

// Models lists model slugs from `agy models` (cached on success).
func (h *AgyHarness) Models(ctx context.Context) ([]string, error) {
	h.modelsMu.Lock()
	defer h.modelsMu.Unlock()
	if len(h.models) > 0 {
		return append([]string(nil), h.models...), nil
	}
	bin, ok := h.resolveBinary()
	if !ok {
		return nil, nil
	}
	fctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(fctx, bin, "models")
	cmd.Env = augmentedEnv(nil)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var list []string
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.LastIndexByte(line, '\r'); i >= 0 {
			line = line[i+1:]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Fetching ") {
			continue
		}
		f := strings.Fields(trimmed)
		if len(f) == 0 || strings.Contains(f[0], ":") {
			continue
		}
		list = append(list, f[0])
	}
	if len(list) > 0 {
		h.models = list
	}
	return append([]string(nil), h.models...), nil
}

func (h *AgyHarness) Close() {
	h.pool.close()
}

// Reload terminates cached agy processes and clears the cached model list so
// updated provider or Magpie settings apply on the next turn.
func (h *AgyHarness) Reload() {
	h.pool.clear()
	h.modelsMu.Lock()
	h.models = nil
	h.modelsMu.Unlock()
}

func (h *AgyHarness) drop(botID string, p *agyProc) {
	h.pool.drop(botID, p)
}

func (h *AgyHarness) viaMagpie(ctx context.Context, model string) bool {
	if !magpieInstalled() {
		return false
	}
	st := GetMagpieStatus(ctx, "agy")
	return st.Connected && (model == "" || strings.HasPrefix(model, "magpie/"))
}

func (h *AgyHarness) proc(ctx context.Context, opts RunOptions) (*agyProc, func(), error) {
	bot := opts.Bot
	viaMagpie := h.viaMagpie(ctx, bot.Model)
	return h.pool.acquire(
		bot.ID,
		func(p *agyProc) bool {
			return p.model == bot.Model && p.sessionID == opts.SessionID && p.viaMagpie == viaMagpie
		},
		func() (*agyProc, error) {
			return h.startProc(opts, viaMagpie, false)
		},
	)
}

func (h *AgyHarness) startProc(opts RunOptions, viaMagpie, ephemeral bool) (*agyProc, error) {
	bot := opts.Bot
	if err := writeAgyWorkspaceFiles(bot.Workspace, opts); err != nil {
		return nil, err
	}

	args := []string{"--input-format", "stream-json", "--output-format", "stream-json"}
	if bot.Model != "" {
		args = append(args, "--model", bot.Model)
	}
	if !ephemeral && opts.SessionID != "" {
		args = append(args, "--conversation", opts.SessionID)
	}
	if bot.ShouldAutoApprove() {
		args = append(args, "--dangerously-skip-permissions")
	}

	var cmd *exec.Cmd
	if viaMagpie {
		runArgs := append([]string{"agy", "run", "--"}, args...)
		cmd = exec.Command(magpieBin(), runArgs...)
	} else {
		cmd = exec.Command(h.binary, args...)
	}
	cmd.Dir = bot.Workspace
	cmd.Env = procEnv(cmd, opts)

	lp, err := startLineProc(cmd)
	if err != nil {
		return nil, err
	}
	return &agyProc{lineProc: lp, model: bot.Model, sessionID: opts.SessionID, viaMagpie: viaMagpie}, nil
}

// writeAgyWorkspaceFiles configures workspace-local .agents/mcp_config.json and
// .agents/rules/pebble.md for Antigravity CLI without touching global configs.
func writeAgyWorkspaceFiles(workspace string, opts RunOptions) error {
	agentsDir := filepath.Join(workspace, ".agents")
	if err := upsertMCPConfig(filepath.Join(agentsDir, "mcp_config.json"), opts); err != nil {
		return err
	}

	if opts.PlatformInstructionsPath != "" {
		raw, err := os.ReadFile(opts.PlatformInstructionsPath)
		if err == nil && len(raw) > 0 {
			rulesDir := filepath.Join(agentsDir, "rules")
			if err := os.MkdirAll(rulesDir, 0o755); err != nil {
				return err
			}
			const header = "---\ntrigger: always_on\ndescription: \"Pebble workspace, MCP, and session management guide\"\n---\n\n"
			content := header + strings.TrimSpace(string(raw)) + "\n"
			rulePath := filepath.Join(rulesDir, "pebble.md")
			if cur, err := os.ReadFile(rulePath); err != nil || string(cur) != content {
				if err := os.WriteFile(rulePath, []byte(content), 0o644); err != nil {
					return err
				}
			}
			_ = os.Remove(filepath.Join(rulesDir, "bot-hub.md"))
		}
	}
	return nil
}

func (h *AgyHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	ctx, cancel := withBotTimeout(ctx, bot.TimeoutSec)
	defer cancel()

	var (
		p   *agyProc
		err error
	)
	if opts.Ephemeral {
		p, err = h.startProc(opts, h.viaMagpie(ctx, bot.Model), true)
		if err != nil {
			return nil, err
		}
		defer p.kill()
	} else {
		var release func()
		p, release, err = h.proc(ctx, opts)
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

	err = p.send(map[string]any{
		"event":   "user",
		"message": map[string]any{"content": AppendAttachmentContext(opts.Prompt, opts.Attachments)},
	})
	if err != nil {
		dropProc()
		return nil, fmt.Errorf("agy process: %w", err)
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: opts.SessionID}
	var textBuf strings.Builder
	seenToolSteps := make(map[int]bool)

	for {
		select {
		case <-ctx.Done():
			partial.Text = strings.TrimSpace(textBuf.String())
			dropProc()
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)

		case line, ok := <-p.lines:
			if !ok {
				partial.Text = strings.TrimSpace(textBuf.String())
				dropProc()
				return partial, fmt.Errorf("agy failed for bot %q: %s", bot.ID, p.exitDetail())
			}
			var evt agyEvent
			if json.Unmarshal(line, &evt) != nil {
				continue
			}
			if evt.ConversationID != "" {
				partial.SessionID = evt.ConversationID
				p.sessionID = evt.ConversationID
			}
			switch evt.Event {
			case "step_update":
				su := evt.StepUpdate
				if su == nil {
					continue
				}
				if su.ConversationID != "" {
					partial.SessionID = su.ConversationID
					p.sessionID = su.ConversationID
				}
				switch su.StepType {
				case "agent_response":
					if su.TextDelta != "" {
						textBuf.WriteString(su.TextDelta)
						if opts.OnText != nil {
							opts.OnText(su.TextDelta)
						}
					}
				case "tool":
					if su.ToolName != "" && !seenToolSteps[su.StepIndex] {
						seenToolSteps[su.StepIndex] = true
						if opts.OnToolUse != nil {
							opts.OnToolUse(su.ToolName)
						}
					}
				}
			case "result":
				res := evt.Result
				if res == nil {
					continue
				}
				if res.ConversationID != "" {
					partial.SessionID = res.ConversationID
					p.sessionID = res.ConversationID
				}
				if res.Status != "" && res.Status != "SUCCESS" {
					msg := strings.TrimSpace(res.Error)
					if msg == "" {
						msg = res.Status
					}
					partial.Text = strings.TrimSpace(textBuf.String())
					return partial, fmt.Errorf("agy failed for bot %q: %s", bot.ID, msg)
				}
				text := strings.TrimSpace(res.Response)
				if text == "" {
					text = strings.TrimSpace(textBuf.String())
				}
				var usage Usage
				if res.Usage != nil {
					usage.InputTokens = deltaCounter(res.Usage.InputTokens, &p.prevInput)
					usage.OutputTokens = deltaCounter(res.Usage.OutputTokens, &p.prevOutput)
					usage.CacheReadTokens = deltaCounter(res.Usage.CacheReadTokens, &p.prevCacheRead)
				}
				return finish(partial, text, usage, started), nil
			}
		}
	}
}

func deltaCounter(cum int, prev *int) int {
	if cum < *prev {
		*prev = cum
		return cum
	}
	d := cum - *prev
	*prev = cum
	return d
}
