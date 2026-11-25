package harness

import (
	"bot-hub/internal/config"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func (h *AgyHarness) drop(botID string, p *agyProc) {
	h.pool.drop(botID, p)
}

func (h *AgyHarness) proc(ctx context.Context, bin string, opts RunOptions) (*agyProc, func(), error) {
	bot := opts.Bot
	viaMagpie := h.viaMagpie(ctx, bot.Model)
	mode := EffectivePermissionMode(opts)
	return h.pool.acquire(
		bot.ID,
		func(p *agyProc) bool {
			return p.model == bot.Model &&
				p.sessionID == opts.SessionID &&
				p.viaMagpie == viaMagpie &&
				p.permissionMode == mode
		},
		func() (*agyProc, error) {
			return h.startProc(bin, opts, viaMagpie, false)
		},
	)
}

func agyStreamModeArgs(mode string) []string {
	switch mode {
	case config.PermissionModeEdit:
		return []string{"--mode", "accept-edits"}
	case config.PermissionModePlan:
		return []string{"--mode", "plan"}
	case config.PermissionModeAsk:
		return []string{"--mode", "auto"}
	default:
		return []string{"--dangerously-skip-permissions"}
	}
}

func (h *AgyHarness) startProc(bin string, opts RunOptions, viaMagpie, ephemeral bool) (*agyProc, error) {
	bot := opts.Bot
	mode := EffectivePermissionMode(opts)
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
	args = append(args, agyStreamModeArgs(mode)...)

	var cmd *exec.Cmd
	if viaMagpie {
		runArgs := append([]string{"agy", "run", "--"}, args...)
		cmd = exec.Command(magpieBin(), runArgs...)
	} else {
		cmd = exec.Command(bin, args...)
	}
	cmd.Dir = bot.Workspace
	cmd.Env = procEnv(cmd, opts)

	lp, err := startLineProc(cmd)
	if err != nil {
		return nil, err
	}
	return &agyProc{
		lineProc:       lp,
		model:          bot.Model,
		sessionID:      opts.SessionID,
		viaMagpie:      viaMagpie,
		permissionMode: mode,
	}, nil
}

func (h *AgyHarness) runStreamTurn(ctx context.Context, bin string, opts RunOptions, started time.Time) (*TurnResult, error) {
	bot := opts.Bot
	var (
		p   *agyProc
		err error
	)
	if opts.Ephemeral {
		p, err = h.startProc(bin, opts, h.viaMagpie(ctx, bot.Model), true)
		if err != nil {
			return nil, err
		}
		defer p.kill()
	} else {
		var release func()
		p, release, err = h.proc(ctx, bin, opts)
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
