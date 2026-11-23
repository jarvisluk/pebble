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
	binary string
	mu     sync.Mutex
	procs  map[string]*agyProc

	modelsOnce sync.Once
	models     []string
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
	return &AgyHarness{binary: resolveAgyBin(), procs: make(map[string]*agyProc)}
}

func resolveAgyBin() string {
	if v := strings.TrimSpace(os.Getenv("AGY_BIN")); v != "" {
		return v
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		cand := filepath.Join(home, ".local", "bin", "agy")
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}
	return "agy"
}

func (h *AgyHarness) Kind() string  { return "agy" }
func (h *AgyHarness) Label() string { return "Antigravity CLI" }

func (h *AgyHarness) ListCommands(_ context.Context, bot config.BotConfig) ([]SlashCommand, error) {
	builtins := []SlashCommand{
		{Name: "compact", Description: "压缩当前会话上下文", Source: "harness"},
		{Name: "plan", Description: "切换计划模式", Source: "harness"},
		{Name: "clear", Description: "清空会话上下文", Source: "harness"},
	}
	var local []SlashCommand
	if bot.Workspace != "" {
		local = append(local, scanMarkdownCommands(filepath.Join(bot.Workspace, ".agents", "workflows"), "workflow")...)
		local = append(local, scanSkillDirs(filepath.Join(bot.Workspace, ".agents", "skills"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local = append(local, scanSkillDirs(filepath.Join(home, ".gemini", "config", "skills"))...)
	}
	return mergeSlashCommands(builtins, local), nil
}

// Models lists model slugs from `agy models` (cached).
func (h *AgyHarness) Models(ctx context.Context) ([]string, error) {
	h.modelsOnce.Do(func() {
		out, err := exec.CommandContext(ctx, h.binary, "models").Output()
		if err != nil {
			return
		}
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
			h.models = append(h.models, f[0])
		}
	})
	return append([]string(nil), h.models...), nil
}

func (h *AgyHarness) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, p := range h.procs {
		p.kill()
		delete(h.procs, id)
	}
}

// Reload terminates cached agy processes and clears the cached model list so
// updated provider or Magpie settings apply on the next turn.
func (h *AgyHarness) Reload() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, p := range h.procs {
		p.kill()
		delete(h.procs, id)
	}
	h.modelsOnce = sync.Once{}
	h.models = nil
}

func (h *AgyHarness) drop(botID string, p *agyProc) {
	h.mu.Lock()
	if h.procs[botID] == p {
		delete(h.procs, botID)
	}
	h.mu.Unlock()
	p.kill()
}

func (h *AgyHarness) proc(ctx context.Context, opts RunOptions) (*agyProc, error) {
	bot := opts.Bot
	viaMagpie := false
	if magpieInstalled() {
		st := GetMagpieStatus(ctx, "agy")
		viaMagpie = st.Connected && (bot.Model == "" || strings.HasPrefix(bot.Model, "magpie/"))
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.procs[bot.ID]; p != nil {
		if p.alive() && p.model == bot.Model && p.sessionID == opts.SessionID && p.viaMagpie == viaMagpie {
			return p, nil
		}
		p.kill()
		delete(h.procs, bot.ID)
	}

	if err := writeAgyWorkspaceFiles(bot.Workspace, opts); err != nil {
		return nil, err
	}

	args := []string{"--input-format", "stream-json", "--output-format", "stream-json"}
	if bot.Model != "" {
		args = append(args, "--model", bot.Model)
	}
	if opts.SessionID != "" {
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
	p := &agyProc{lineProc: lp, model: bot.Model, sessionID: opts.SessionID, viaMagpie: viaMagpie}
	h.procs[bot.ID] = p
	return p, nil
}

// writeAgyWorkspaceFiles configures workspace-local .agents/mcp_config.json and
// .agents/rules/bot-hub.md for Antigravity CLI without touching global configs.
func writeAgyWorkspaceFiles(workspace string, opts RunOptions) error {
	agentsDir := filepath.Join(workspace, ".agents")
	if len(opts.MCPCommand) > 0 {
		if err := os.MkdirAll(agentsDir, 0o755); err != nil {
			return err
		}
		mcpPath := filepath.Join(agentsDir, "mcp_config.json")
		doc := map[string]any{}
		if raw, err := os.ReadFile(mcpPath); err == nil {
			_ = json.Unmarshal(raw, &doc)
		}
		servers, _ := doc["mcpServers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
		}
		entry := map[string]any{
			"command": opts.MCPCommand[0],
			"args":    opts.MCPCommand[1:],
		}
		if len(opts.MCPEnv) > 0 {
			entry["env"] = opts.MCPEnv
		}
		servers["bot-hub"] = entry
		doc["mcpServers"] = servers
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(mcpPath, append(b, '\n'), 0o600); err != nil {
			return err
		}
		if err := os.Chmod(mcpPath, 0o600); err != nil {
			return err
		}
	}

	if opts.PlatformInstructionsPath != "" {
		raw, err := os.ReadFile(opts.PlatformInstructionsPath)
		if err == nil && len(raw) > 0 {
			rulesDir := filepath.Join(agentsDir, "rules")
			if err := os.MkdirAll(rulesDir, 0o755); err != nil {
				return err
			}
			const header = "---\ntrigger: always_on\ndescription: \"Bot Hub workspace, MCP, and session management guide\"\n---\n\n"
			content := header + strings.TrimSpace(string(raw)) + "\n"
			rulePath := filepath.Join(rulesDir, "bot-hub.md")
			if cur, err := os.ReadFile(rulePath); err != nil || string(cur) != content {
				if err := os.WriteFile(rulePath, []byte(content), 0o644); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (h *AgyHarness) RunTurn(ctx context.Context, opts RunOptions) (*TurnResult, error) {
	started := time.Now()
	bot := opts.Bot
	if bot.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(bot.TimeoutSec)*time.Second)
		defer cancel()
	}

	p, err := h.proc(ctx, opts)
	if err != nil {
		return nil, err
	}
	err = p.send(map[string]any{
		"event":   "user",
		"message": map[string]any{"content": AppendAttachmentContext(opts.Prompt, opts.Attachments)},
	})
	if err != nil {
		h.drop(bot.ID, p)
		return nil, fmt.Errorf("agy process: %w", err)
	}

	partial := &TurnResult{BotID: bot.ID, SessionID: opts.SessionID}
	var textBuf strings.Builder
	seenToolSteps := make(map[int]bool)

	for {
		select {
		case <-ctx.Done():
			partial.Text = strings.TrimSpace(textBuf.String())
			h.drop(bot.ID, p)
			return partial, ctxErr(ctx, bot.ID, bot.TimeoutSec)

		case line, ok := <-p.lines:
			if !ok {
				partial.Text = strings.TrimSpace(textBuf.String())
				h.drop(bot.ID, p)
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
