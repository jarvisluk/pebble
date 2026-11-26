package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/desktop"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type DesktopTaskRequest struct {
	BotID      string `json:"botId"`
	Target     string `json:"nodeId,omitempty"`
	Goal       string `json:"goal"`
	App        string `json:"app,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

const defaultDesktopTargetTTL = 30 * time.Minute

// ActiveDesktopTarget describes which desktop is currently active for screen sharing
// and default computer-use tasks.
type ActiveDesktopTarget struct {
	Target    string `json:"target"`
	Name      string `json:"name,omitempty"`
	Temporary bool   `json:"temporary"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

func (h *Hub) findRemoteDesktopNode(target string) (node.NodeInfo, bool) {
	if h.nodes == nil {
		return node.NodeInfo{}, false
	}
	for _, n := range h.nodes.List() {
		if n.ID != target || !n.Connected {
			continue
		}
		for _, p := range n.Permissions {
			if p == node.PermDesktop {
				return n, true
			}
		}
	}
	return node.NodeInfo{}, false
}

func (h *Hub) resetActiveDesktopTargetLocked() ActiveDesktopTarget {
	if h.activeDesktopTimer != nil {
		h.activeDesktopTimer.Stop()
		h.activeDesktopTimer = nil
	}
	h.activeDesktopTarget = "local"
	h.activeDesktopExpires = time.Time{}
	return ActiveDesktopTarget{Target: "local", Temporary: false}
}

func formatVirtualTargetName(hostName, slot string) string {
	if slot == "" {
		return hostName
	}
	if hostName == "" || hostName == "local" {
		return "Virtual Screen · " + slot
	}
	return hostName + " · Virtual Screen " + slot
}

// ActiveDesktopTarget returns the currently active desktop target, falling back to
// "local" if a temporary remote target has expired, disconnected, or lost desktop permission.
func (h *Hub) ActiveDesktopTarget() ActiveDesktopTarget {
	h.desktopTargetMu.Lock()
	cur := strings.TrimSpace(h.activeDesktopTarget)
	exp := h.activeDesktopExpires
	spec := desktop.ParseTarget(cur)
	if spec.IsLocal() && !spec.IsVirtualSlot() {
		h.desktopTargetMu.Unlock()
		return ActiveDesktopTarget{Target: "local", Temporary: false}
	}
	if spec.IsLocal() && spec.IsVirtualSlot() {
		supported := h.localDeskPool != nil && h.localDeskPool.VirtualSupported()
		if supported {
			h.desktopTargetMu.Unlock()
			return ActiveDesktopTarget{
				Target:    spec.Raw,
				Name:      formatVirtualTargetName("local", spec.VirtualSlot),
				Temporary: false,
			}
		}
		snap := h.resetActiveDesktopTargetLocked()
		h.desktopTargetMu.Unlock()
		h.broadcast(Event{Type: "desktop_target", Payload: snap})
		return snap
	}
	expired := !exp.IsZero() && !time.Now().Before(exp)
	h.desktopTargetMu.Unlock()

	if !expired {
		if n, ok := h.findRemoteDesktopNode(spec.NodeID); ok && (!spec.IsVirtualSlot() || n.HasCapability(protocol.CapNodeVirtualSlots)) {
			return ActiveDesktopTarget{
				Target:    spec.Raw,
				Name:      formatVirtualTargetName(n.Name, spec.VirtualSlot),
				Temporary: true,
				ExpiresAt: exp.UTC().Format(time.RFC3339),
			}
		}
	}

	h.desktopTargetMu.Lock()
	changed := h.activeDesktopTarget == cur
	snap := ActiveDesktopTarget{Target: "local", Temporary: false}
	if changed {
		snap = h.resetActiveDesktopTargetLocked()
	} else {
		curNow := strings.TrimSpace(h.activeDesktopTarget)
		specNow := desktop.ParseTarget(curNow)
		if specNow.IsLocal() && specNow.IsVirtualSlot() {
			if h.localDeskPool != nil && h.localDeskPool.VirtualSupported() {
				h.desktopTargetMu.Unlock()
				return ActiveDesktopTarget{
					Target:    specNow.Raw,
					Name:      formatVirtualTargetName("local", specNow.VirtualSlot),
					Temporary: false,
				}
			}
		} else if !specNow.IsLocal() {
			expNow := h.activeDesktopExpires
			h.desktopTargetMu.Unlock()
			if n, ok := h.findRemoteDesktopNode(specNow.NodeID); ok && (!specNow.IsVirtualSlot() || n.HasCapability(protocol.CapNodeVirtualSlots)) {
				return ActiveDesktopTarget{
					Target:    specNow.Raw,
					Name:      formatVirtualTargetName(n.Name, specNow.VirtualSlot),
					Temporary: true,
					ExpiresAt: expNow.UTC().Format(time.RFC3339),
				}
			}
			return ActiveDesktopTarget{Target: "local", Temporary: false}
		}
	}
	h.desktopTargetMu.Unlock()
	if changed {
		h.broadcast(Event{Type: "desktop_target", Payload: snap})
	}
	return snap
}

// SetActiveDesktopTarget switches the active desktop target to "local", a local virtual slot,
// or a connected remote node (with optional virtual slot). Non-local targets expire after ttl.
func (h *Hub) SetActiveDesktopTarget(target string, ttl time.Duration) (ActiveDesktopTarget, error) {
	spec := desktop.ParseTarget(target)
	if spec.IsLocal() && !spec.IsVirtualSlot() {
		h.desktopTargetMu.Lock()
		snap := h.resetActiveDesktopTargetLocked()
		h.desktopTargetMu.Unlock()
		h.broadcast(Event{Type: "desktop_target", Payload: snap})
		return snap, nil
	}
	if spec.IsLocal() && spec.IsVirtualSlot() {
		if h.localDeskPool == nil || !h.localDeskPool.VirtualSupported() {
			return ActiveDesktopTarget{}, errors.New("this host does not support dedicated virtual screens (Linux only)")
		}
		h.desktopTargetMu.Lock()
		if h.activeDesktopTimer != nil {
			h.activeDesktopTimer.Stop()
			h.activeDesktopTimer = nil
		}
		h.activeDesktopTarget = spec.Raw
		h.activeDesktopExpires = time.Time{}
		h.desktopTargetMu.Unlock()
		snap := ActiveDesktopTarget{
			Target:    spec.Raw,
			Name:      formatVirtualTargetName("local", spec.VirtualSlot),
			Temporary: false,
		}
		h.broadcast(Event{Type: "desktop_target", Payload: snap})
		return snap, nil
	}
	n, ok := h.findRemoteDesktopNode(spec.NodeID)
	if !ok {
		return ActiveDesktopTarget{}, fmt.Errorf("remote desktop target %q unavailable", spec.NodeID)
	}
	if spec.IsVirtualSlot() && !n.HasCapability(protocol.CapNodeVirtualSlots) {
		if n.OS != "linux" {
			return ActiveDesktopTarget{}, fmt.Errorf("remote node %q (%s) does not support virtual screens", spec.NodeID, n.OS)
		}
		return ActiveDesktopTarget{}, fmt.Errorf("remote node %q is outdated and does not support dedicated virtual screens; upgrade the node or bind its main desktop in Bot Details: %w", spec.NodeID, ErrNodeUpgradeRequired)
	}
	if ttl <= 0 {
		ttl = defaultDesktopTargetTTL
	}
	exp := time.Now().Add(ttl)

	h.desktopTargetMu.Lock()
	if h.activeDesktopTimer != nil {
		h.activeDesktopTimer.Stop()
	}
	h.activeDesktopTarget = spec.Raw
	h.activeDesktopExpires = exp
	h.activeDesktopTimer = time.AfterFunc(ttl, h.reconcileActiveDesktopTarget)
	h.desktopTargetMu.Unlock()

	snap := ActiveDesktopTarget{
		Target:    spec.Raw,
		Name:      formatVirtualTargetName(n.Name, spec.VirtualSlot),
		Temporary: true,
		ExpiresAt: exp.UTC().Format(time.RFC3339),
	}
	h.broadcast(Event{Type: "desktop_target", Payload: snap})
	return snap, nil
}

func (h *Hub) reconcileActiveDesktopTarget() {
	h.closeMu.Lock()
	closed := h.closed
	h.closeMu.Unlock()
	if closed {
		return
	}
	_ = h.ActiveDesktopTarget()
}

func (h *Hub) touchActiveDesktopTarget(target string) {
	spec := desktop.ParseTarget(target)
	if spec.IsLocal() {
		return
	}
	h.desktopTargetMu.Lock()
	curSpec := desktop.ParseTarget(h.activeDesktopTarget)
	if curSpec.IsLocal() || curSpec.HostKey() != spec.HostKey() {
		h.desktopTargetMu.Unlock()
		return
	}
	exp := time.Now().Add(defaultDesktopTargetTTL)
	if h.activeDesktopTimer != nil {
		h.activeDesktopTimer.Stop()
	}
	h.activeDesktopExpires = exp
	h.activeDesktopTimer = time.AfterFunc(defaultDesktopTargetTTL, h.reconcileActiveDesktopTarget)
	h.desktopTargetMu.Unlock()

	if n, ok := h.findRemoteDesktopNode(curSpec.NodeID); ok {
		h.broadcast(Event{
			Type: "desktop_target",
			Payload: ActiveDesktopTarget{
				Target:    curSpec.Raw,
				Name:      formatVirtualTargetName(n.Name, curSpec.VirtualSlot),
				Temporary: true,
				ExpiresAt: exp.UTC().Format(time.RFC3339),
			},
		})
	}
}

// ResolveBotDesktopTarget resolves the desktop target for a bot task or takeover.
// Priority: explicitTarget > bot's bound DesktopTarget > global ActiveDesktopTarget.
// Bound remote targets never fall back silently to local when offline.
func (h *Hub) ResolveBotDesktopTarget(botID, explicitTarget string) (string, error) {
	raw := strings.TrimSpace(explicitTarget)
	boundFromBot := false
	if raw == "" && strings.TrimSpace(botID) != "" {
		if bot, ok := h.GetBot(botID); ok && strings.TrimSpace(bot.DesktopTarget) != "" {
			raw = bot.DesktopTarget
			boundFromBot = true
		}
	}
	if raw == "" {
		return h.ActiveDesktopTarget().Target, nil
	}

	spec := desktop.ParseTarget(raw)
	if spec.IsLocal() {
		if spec.IsVirtualSlot() && (h.localDeskPool == nil || !h.localDeskPool.VirtualSupported()) {
			return "", errors.New("this host does not support dedicated virtual screens (Linux only)")
		}
		return spec.Raw, nil
	}

	n, ok := h.findRemoteDesktopNode(spec.NodeID)
	if !ok {
		if boundFromBot {
			return "", fmt.Errorf("bot %q bound desktop node %q is offline or desktop permission is disabled: %w", botID, spec.NodeID, ErrBoundDesktopOffline)
		}
		return "", fmt.Errorf("remote desktop target %q unavailable: %w", spec.NodeID, ErrBoundDesktopOffline)
	}
	if spec.IsVirtualSlot() && !n.HasCapability(protocol.CapNodeVirtualSlots) {
		if n.OS != "linux" {
			return "", fmt.Errorf("remote node %q (%s) does not support virtual screens", spec.NodeID, n.OS)
		}
		return "", fmt.Errorf("remote node %q is outdated and does not support dedicated virtual screens; upgrade the node or bind its main desktop in Bot Details: %w", spec.NodeID, ErrNodeUpgradeRequired)
	}
	return spec.Raw, nil
}

// BrowserProfileDir returns the persistent isolated browser profile directory for botID.
func (h *Hub) BrowserProfileDir(botID string) string {
	base := ""
	if h.cfg != nil && strings.TrimSpace(h.cfg.StateFile) != "" {
		base = filepath.Dir(h.cfg.StateFile)
	} else if h.cfg != nil && strings.TrimSpace(h.cfg.ProjectRoot) != "" {
		base = filepath.Join(h.cfg.ProjectRoot, "state")
	} else {
		base = os.TempDir()
	}
	return filepath.Join(base, "browsers", botID)
}

func (h *Hub) botBrowserEnv(bot config.BotConfig) map[string]string {
	env := make(map[string]string)
	if strings.TrimSpace(bot.Env["AGENT_BROWSER_SESSION"]) == "" {
		env["AGENT_BROWSER_SESSION"] = "bot-" + bot.ID
	}
	if bot.EffectiveBrowserMode() == "isolated" && strings.TrimSpace(bot.Env["AGENT_BROWSER_PROFILE"]) == "" {
		dir := h.BrowserProfileDir(bot.ID)
		if err := os.MkdirAll(dir, 0o700); err == nil {
			env["AGENT_BROWSER_PROFILE"] = dir
		}
	}
	if strings.TrimSpace(bot.DesktopTarget) != "" && h.localDeskPool != nil {
		spec := desktop.ParseTarget(bot.DesktopTarget)
		if spec.IsLocal() && spec.IsVirtualSlot() {
			for _, kv := range h.localDeskPool.SlotEnv(spec.VirtualSlot) {
				if k, v, ok := strings.Cut(kv, "="); ok && v != "" {
					if _, exists := bot.Env[k]; !exists {
						env[k] = v
					}
				}
			}
		}
	}
	return env
}

// GetBotBrowserInfo returns browser isolation settings and bound remote target details for node_exec forwarding.
func (h *Hub) GetBotBrowserInfo(botID string) (session string, isolated bool, boundNodeID string, boundSlot string) {
	bot, ok := h.GetBot(botID)
	if !ok {
		return "", false, "", ""
	}
	session = strings.TrimSpace(bot.Env["AGENT_BROWSER_SESSION"])
	if session == "" {
		session = "bot-" + bot.ID
	}
	isolated = bot.EffectiveBrowserMode() == "isolated"
	if strings.TrimSpace(bot.DesktopTarget) != "" {
		spec := desktop.ParseTarget(bot.DesktopTarget)
		if !spec.IsLocal() {
			boundNodeID = spec.NodeID
			boundSlot = spec.VirtualSlot
		}
	}
	return session, isolated, boundNodeID, boundSlot
}

func closeBotBrowserSession(botID string) {
	if strings.TrimSpace(botID) == "" {
		return
	}
	bin, err := exec.LookPath("agent-browser")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, bin, "--session", "bot-"+botID, "close").Run()
}

// ActiveDesktopTaskBot returns the bot ID currently running a computer-use worker on target, if any.
func (h *Hub) ActiveDesktopTaskBot(target string) string {
	t := desktop.NormalizeTarget(target)
	h.desktopTaskMu.Lock()
	defer h.desktopTaskMu.Unlock()
	if id := h.activeDesktopTasks[t]; id != "" {
		return id
	}
	if t != "local" && t != "" {
		return h.activeDesktopTasks["_default"]
	}
	return ""
}

// RunDesktopTask executes a multi-step GUI / visible-browser goal in an isolated
// ephemeral worker using the configured lightweight desktop model/harness.
func (h *Hub) RunDesktopTask(ctx context.Context, req DesktopTaskRequest) (*harness.TurnResult, error) {
	botID := strings.TrimSpace(req.BotID)
	if botID == "" {
		return nil, fmt.Errorf("botId is required: %w", ErrBotNotFound)
	}
	if _, ok := h.GetBot(botID); !ok {
		return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
	}
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		return nil, errors.New("goal is required")
	}

	target, err := h.ResolveBotDesktopTarget(botID, req.Target)
	if err != nil {
		return nil, err
	}
	spec := desktop.ParseTarget(target)
	if !spec.IsLocal() {
		h.touchActiveDesktopTarget(spec.HostKey())
	}

	h.desktopTaskMu.Lock()
	if holder := h.activeDesktopTasks[target]; holder != "" {
		h.desktopTaskMu.Unlock()
		return nil, fmt.Errorf("%w (%s, held by bot %q)", ErrDesktopTaskBusy, target, holder)
	}
	h.activeDesktopTasks[target] = botID
	h.desktopTaskMu.Unlock()
	defer func() {
		h.desktopTaskMu.Lock()
		if h.activeDesktopTasks[target] == botID {
			delete(h.activeDesktopTasks, target)
		}
		h.desktopTaskMu.Unlock()
	}()

	var taskEnv map[string]string
	if spec.IsLocal() && h.localDeskPool != nil {
		eng, err := h.localDeskPool.ForSlot(spec.VirtualSlot)
		if err != nil {
			return nil, err
		}
		release := eng.AcquireTaskLease()
		defer release()
		if err := eng.PrepareForTask(ctx); err != nil && spec.IsVirtualSlot() {
			return nil, err
		}
		if envs := eng.Env(); len(envs) > 0 {
			taskEnv = make(map[string]string, len(envs))
			for _, kv := range envs {
				if k, v, ok := strings.Cut(kv, "="); ok {
					taskEnv[k] = v
				}
			}
		}
	}

	deskHarness, deskModel := h.DesktopWorkerSnapshot()
	var lines []string
	lines = append(lines,
		"[Computer Use Worker]",
		fmt.Sprintf("Target desktop: %s", target),
	)
	if app := strings.TrimSpace(req.App); app != "" {
		lines = append(lines, fmt.Sprintf("Target app: %s", app))
	}
	lines = append(lines,
		"Rules:",
		"- Operate ONLY on the visible desktop via `node_desktop` (never launch headless browsers or hidden background UI sessions) so the user can observe and take over at any moment.",
		"- Capture a screenshot with `node_desktop(op=\"screenshot\")` and inspect it before clicking/typing; verify the final state visually after acting.",
		"- If login/QR/2FA/CAPTCHA/payment confirmation is required, call `request_takeover(reason=...)` and wait for the human to finish.",
		"- Never call `desktop_task` recursively.",
		"- Return a concise factual summary of what was done and observed.",
		"",
		"Goal:",
		goal,
	)

	return h.RunIsolated(ctx, botID, strings.Join(lines, "\n"), IsolatedOptions{
		Source:         "desktop_task",
		Harness:        deskHarness,
		Model:          deskModel,
		PermissionMode: config.PermissionModeAuto,
		MCPRole:        "computer_use",
		TimeoutSec:     req.TimeoutSec,
		ExtraEnv:       taskEnv,
		OnToolUse: func(toolName string) {
			if toolName == "" {
				return
			}
			h.broadcast(Event{
				Type:  "tool_use",
				BotID: botID,
				Payload: map[string]any{
					"botId": botID,
					"tool":  "desktop:" + toolName,
				},
			})
		},
	})
}
