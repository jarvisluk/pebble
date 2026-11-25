package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"bot-hub/internal/config"
	"bot-hub/internal/state"
)

const (
	toolThrottleInterval = 3 * time.Second
	alertMinRunDuration  = 5 * time.Second
	dismissAfterSeconds  = 300
	eventQueueSize       = 256
)

type botRunState struct {
	BotID      string
	BotName    string
	StartedAt  time.Time
	StepCount  int
	LastTool   string
	LastPushAt time.Time
	Busy       bool
}

type Manager struct {
	store  *state.Store
	sender Sender

	mu                 sync.Mutex
	botRuns            map[string]*botRunState
	activeTakeover     map[string]string // target -> takeover id
	knownQuestions     map[string]bool
	knownApprovals     map[string]bool
	activeApprovalBots map[string]bool
	botNames           map[string]string
	eventCh        chan rawEvent
	stopCh         chan struct{}
	stopOnce       sync.Once
}

type rawEvent struct {
	Type    string
	BotID   string
	Payload any
}

func NewManager(cfg *config.HubConfig, store *state.Store) *Manager {
	var sender Sender
	if cfg != nil && cfg.Push.IsEnabled() {
		apnsCfg := cfg.Push.APNs
		if apns, err := NewAPNsSender(APNsSenderOptions{
			KeyPath:       apnsCfg.KeyPath,
			KeyPEM:        apnsCfg.KeyPEM,
			KeyID:         apnsCfg.KeyID,
			TeamID:        apnsCfg.TeamID,
			DefaultBundle: apnsCfg.BundleID,
			DefaultEnv:    apnsCfg.DefaultEnv,
		}); err == nil {
			sender = apns
		} else if !errors.Is(err, ErrNotConfigured) {
			log.Printf("push: apns init error: %v", err)
		} else if relay, err := NewRelaySender(cfg.Push.RelayURL, nil); err == nil {
			sender = relay
		}
	}
	return NewManagerWithSender(store, sender)
}

func NewManagerWithSender(store *state.Store, sender Sender) *Manager {
	m := &Manager{
		store:          store,
		sender:         sender,
		botRuns:            make(map[string]*botRunState),
		activeTakeover:     make(map[string]string),
		knownQuestions:     make(map[string]bool),
		knownApprovals:     make(map[string]bool),
		activeApprovalBots: make(map[string]bool),
		botNames:           make(map[string]string),
		eventCh:        make(chan rawEvent, eventQueueSize),
		stopCh:         make(chan struct{}),
	}
	go m.worker()
	return m
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stopCh) })
}

func (m *Manager) Status() Status {
	mode := "disabled"
	configured := false
	if m != nil && m.sender != nil {
		mode = m.sender.Mode()
		configured = true
	}
	count := 0
	if m != nil && m.store != nil {
		if list, err := m.store.ListPushDevices(); err == nil {
			count = len(list)
		}
	}
	return Status{
		Configured:  configured,
		Mode:        mode,
		DeviceCount: count,
	}
}

func (m *Manager) EnqueueEvent(evtType, botID string, payload any) {
	if m == nil || m.sender == nil {
		return
	}
	select {
	case m.eventCh <- rawEvent{Type: evtType, BotID: botID, Payload: payload}:
	default:
		// Drop under heavy load so Hub.broadcast never blocks.
	}
}

func (m *Manager) worker() {
	for {
		select {
		case <-m.stopCh:
			return
		case evt := <-m.eventCh:
			m.processEvent(evt)
		}
	}
}

func (m *Manager) processEvent(evt rawEvent) {
	switch evt.Type {
	case "bot_status":
		m.handleBotStatus(evt.BotID, evt.Payload)
	case "tool_use":
		m.handleToolUse(evt.BotID, evt.Payload)
	case "message":
		m.handleMessage(evt.BotID, evt.Payload)
	case "takeover":
		m.handleTakeover(evt.Payload)
	case "questions":
		m.handleQuestions(evt.Payload)
	case "approvals":
		m.handleApprovals(evt.Payload)
	}
}

func (m *Manager) handleBotStatus(botID string, payload any) {
	var st struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		Busy    bool   `json:"busy"`
		IsBusy  bool   `json:"isBusy"`
		Running bool   `json:"running"`
	}
	if !decodePayload(payload, &st) {
		return
	}
	if botID == "" {
		botID = st.ID
	}
	if botID == "" {
		return
	}
	name := strings.TrimSpace(st.Name)
	if name == "" {
		name = botID
	}
	busy := st.Busy || st.IsBusy || st.Running || strings.EqualFold(st.Status, "busy") || strings.EqualFold(st.Status, "running")

	now := time.Now()
	m.mu.Lock()
	m.botNames[botID] = name
	run, exists := m.botRuns[botID]
	if busy {
		if !exists || !run.Busy {
			run = &botRunState{
				BotID:      botID,
				BotName:    name,
				StartedAt:  now,
				StepCount:  0,
				LastPushAt: now,
				Busy:       true,
			}
			m.botRuns[botID] = run
			m.mu.Unlock()
			m.pushBotActivity("bot:"+botID, botID, name, "start", LiveActivityContentState{
				Phase:      "running",
				StatusText: "Thinking…",
				StepCount:  0,
				StartedAt:  float64(now.UnixMilli()) / 1000.0,
			}, "")
			return
		}
		run.BotName = name
		m.mu.Unlock()
		return
	}

	// Transitioned to idle.
	if exists && run.Busy {
		run.Busy = false
		startedAt := run.StartedAt
		steps := run.StepCount
		m.mu.Unlock()
		m.pushBotActivity("bot:"+botID, botID, name, "end", LiveActivityContentState{
			Phase:      "completed",
			StatusText: "Completed",
			StepCount:  steps,
			StartedAt:  float64(startedAt.UnixMilli()) / 1000.0,
			Summary:    "Task completed",
		}, "")
		return
	}
	m.mu.Unlock()
}

func (m *Manager) handleToolUse(botID string, payload any) {
	var p struct {
		BotID string `json:"botId"`
		Tool  string `json:"tool"`
	}
	if !decodePayload(payload, &p) {
		return
	}
	if botID == "" {
		botID = p.BotID
	}
	if botID == "" {
		return
	}
	toolName := strings.TrimSpace(p.Tool)
	if toolName == "" {
		return
	}
	now := time.Now()

	m.mu.Lock()
	name := m.botNames[botID]
	if name == "" {
		name = botID
	}
	run, exists := m.botRuns[botID]
	if !exists {
		run = &botRunState{
			BotID:     botID,
			BotName:   name,
			StartedAt: now,
			Busy:      true,
		}
		m.botRuns[botID] = run
	}
	run.StepCount++
	run.LastTool = toolName
	if now.Sub(run.LastPushAt) < toolThrottleInterval {
		m.mu.Unlock()
		return
	}
	run.LastPushAt = now
	startedAt := run.StartedAt
	steps := run.StepCount
	m.mu.Unlock()

	m.pushBotActivity("bot:"+botID, botID, name, "update", LiveActivityContentState{
		Phase:      "running",
		StatusText: fmt.Sprintf("Running %s", toolName),
		StepCount:  steps,
		StartedAt:  float64(startedAt.UnixMilli()) / 1000.0,
	}, "")
}

func (m *Manager) handleMessage(botID string, payload any) {
	var msg struct {
		BotID string `json:"botId"`
		Role  string `json:"role"`
		From  string `json:"from"`
		Text  string `json:"text"`
	}
	if !decodePayload(payload, &msg) {
		return
	}
	if botID == "" {
		botID = msg.BotID
	}
	if botID == "" || !strings.EqualFold(msg.Role, "assistant") {
		return
	}

	m.mu.Lock()
	name := m.botNames[botID]
	if name == "" {
		name = botID
	}
	run := m.botRuns[botID]
	var dur time.Duration
	if run != nil {
		dur = time.Since(run.StartedAt)
	}
	m.mu.Unlock()

	isRoutine := strings.HasPrefix(msg.From, "schedule") || strings.HasPrefix(msg.From, "webhook")
	if dur < alertMinRunDuration && !isRoutine {
		return
	}
	preview := compactPreview(msg.Text, 120)
	if preview == "" {
		preview = "New reply"
	}
	m.pushAlert(APSAlert{
		Title: name,
		Body:  preview,
	}, "active", "bot-"+botID, "pebble://bot/"+botID, botID, "", "bot_message")
}

func (m *Manager) handleTakeover(payload any) {
	var p struct {
		Items []struct {
			ID     string `json:"id"`
			Target string `json:"target"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	if !decodePayload(payload, &p) {
		return
	}

	current := make(map[string]string, len(p.Items))
	for _, item := range p.Items {
		target := strings.TrimSpace(item.Target)
		if target == "" {
			target = "local"
		}
		current[target] = item.ID

		m.mu.Lock()
		prevID := m.activeTakeover[target]
		m.activeTakeover[target] = item.ID
		m.mu.Unlock()

		if prevID != item.ID {
			reason := strings.TrimSpace(item.Reason)
			if reason == "" {
				reason = "Manual action required"
			}
			deepLink := "pebble://desktop?target=" + target
			m.pushAlert(APSAlert{
				Title:    "Remote desktop takeover needed",
				Subtitle: target,
				Body:     reason,
			}, "time-sensitive", "takeover-"+target, deepLink, "", target, "takeover")

			now := time.Now()
			m.pushDesktopActivity("desktop:"+target, target, "start", LiveActivityContentState{
				Phase:          "takeover",
				StatusText:     "Waiting for takeover",
				StartedAt:      float64(now.UnixMilli()) / 1000.0,
				TakeoverReason: reason,
			})
		}
	}

	m.mu.Lock()
	var cleared []string
	for target := range m.activeTakeover {
		if _, ok := current[target]; !ok {
			cleared = append(cleared, target)
			delete(m.activeTakeover, target)
		}
	}
	m.mu.Unlock()

	for _, target := range cleared {
		now := time.Now()
		m.pushDesktopActivity("desktop:"+target, target, "end", LiveActivityContentState{
			Phase:      "completed",
			StatusText: "Takeover completed",
			StartedAt:  float64(now.UnixMilli()) / 1000.0,
			Summary:    "Automation resumed",
		})
	}
}

func (m *Manager) handleQuestions(payload any) {
	var p struct {
		Items []struct {
			ID      string `json:"id"`
			BotID   string `json:"botId"`
			BotName string `json:"botName"`
			Title   string `json:"title"`
			Prompt  string `json:"prompt"`
		} `json:"items"`
	}
	if !decodePayload(payload, &p) {
		return
	}
	current := make(map[string]bool, len(p.Items))
	for _, item := range p.Items {
		current[item.ID] = true
		m.mu.Lock()
		seen := m.knownQuestions[item.ID]
		m.knownQuestions[item.ID] = true
		name := m.botNames[item.BotID]
		m.mu.Unlock()
		if seen {
			continue
		}
		if name == "" {
			name = strings.TrimSpace(item.BotName)
		}
		if name == "" {
			name = item.BotID
		}
		if name == "" {
			name = "Bot"
		}
		body := compactPreview(item.Title, 120)
		if body == "" {
			body = compactPreview(item.Prompt, 120)
		}
		if body == "" {
			body = "Waiting for your reply"
		}
		m.pushAlert(APSAlert{
			Title: name + " is waiting for a reply",
			Body:  body,
		}, "active", "question-"+item.BotID, "pebble://bot/"+item.BotID, item.BotID, "", "question")
	}

	m.mu.Lock()
	for id := range m.knownQuestions {
		if !current[id] {
			delete(m.knownQuestions, id)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) handleApprovals(payload any) {
	var p struct {
		Items []struct {
			ID       string `json:"id"`
			BotID    string `json:"botId"`
			BotName  string `json:"botName"`
			ToolName string `json:"toolName"`
			Title    string `json:"title"`
			Summary  string `json:"summary"`
		} `json:"items"`
	}
	if !decodePayload(payload, &p) {
		return
	}
	currentIDs := make(map[string]bool, len(p.Items))
	currentBots := make(map[string]bool)
	for _, item := range p.Items {
		currentIDs[item.ID] = true
		if item.BotID != "" {
			currentBots[item.BotID] = true
		}

		m.mu.Lock()
		seen := m.knownApprovals[item.ID]
		m.knownApprovals[item.ID] = true
		name := m.botNames[item.BotID]
		run := m.botRuns[item.BotID]
		var startedAt time.Time
		var steps int
		if run != nil {
			startedAt = run.StartedAt
			steps = run.StepCount
		}
		m.mu.Unlock()
		if seen {
			continue
		}
		if name == "" {
			name = strings.TrimSpace(item.BotName)
		}
		if name == "" {
			name = item.BotID
		}
		if name == "" {
			name = "Bot"
		}
		label := strings.TrimSpace(item.ToolName)
		if label == "" {
			label = strings.TrimSpace(item.Title)
		}
		summary := compactPreview(item.Summary, 100)
		body := label
		if summary != "" {
			if body != "" {
				body += ": " + summary
			} else {
				body = summary
			}
		}
		if body == "" {
			body = "Approval required to run action"
		}

		m.pushAlert(APSAlert{
			Title: name + " needs approval",
			Body:  body,
		}, "time-sensitive", "approval-"+item.BotID, "pebble://bot/"+item.BotID, item.BotID, "", "approval")

		if item.BotID != "" {
			if startedAt.IsZero() {
				startedAt = time.Now()
			}
			m.mu.Lock()
			m.activeApprovalBots[item.BotID] = true
			m.mu.Unlock()
			m.pushBotActivity("bot:"+item.BotID, item.BotID, name, "update", LiveActivityContentState{
				Phase:      "approval",
				StatusText: "Approval required",
				StepCount:  steps,
				StartedAt:  float64(startedAt.UnixMilli()) / 1000.0,
				Summary:    body,
			}, "")
		}
	}

	type restoreItem struct {
		botID     string
		name      string
		startedAt time.Time
		steps     int
	}
	var toRestore []restoreItem
	m.mu.Lock()
	for id := range m.knownApprovals {
		if !currentIDs[id] {
			delete(m.knownApprovals, id)
		}
	}
	for botID := range m.activeApprovalBots {
		if !currentBots[botID] {
			delete(m.activeApprovalBots, botID)
			if run := m.botRuns[botID]; run != nil && run.Busy {
				name := m.botNames[botID]
				if name == "" {
					name = botID
				}
				toRestore = append(toRestore, restoreItem{
					botID:     botID,
					name:      name,
					startedAt: run.StartedAt,
					steps:     run.StepCount,
				})
			}
		}
	}
	m.mu.Unlock()

	for _, r := range toRestore {
		m.pushBotActivity("bot:"+r.botID, r.botID, r.name, "update", LiveActivityContentState{
			Phase:      "running",
			StatusText: "Running",
			StepCount:  r.steps,
			StartedAt:  float64(r.startedAt.UnixMilli()) / 1000.0,
		}, "")
	}
}

func (m *Manager) pushAlert(alert APSAlert, level, collapseID, deepLink, botID, target, kind string) {
	devices, err := m.store.ListPushDevices()
	if err != nil {
		return
	}
	payload := APNSPayload{
		APS: APSBody{
			Alert:             &alert,
			Sound:             "default",
			InterruptionLevel: level,
		},
		DeepLink: deepLink,
		BotID:    botID,
		Target:   target,
		Kind:     kind,
	}
	for _, d := range devices {
		if !d.Enabled || d.PushToken == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := m.sender.Send(ctx, SendRequest{
			Platform:   d.Platform,
			Env:        d.Env,
			BundleID:   d.BundleID,
			PushType:   PushTypeAlert,
			Token:      d.PushToken,
			Priority:   10,
			CollapseID: collapseID,
			Payload:    payload,
		})
		cancel()
		if errors.Is(err, ErrInvalidToken) {
			_ = m.store.ClearInvalidPushToken(d.DeviceID, "push", "")
		}
	}
}

func (m *Manager) pushBotActivity(activityKey, botID, botName, event string, stateVal LiveActivityContentState, _ string) {
	m.pushLiveActivity(activityKey, LiveActivityAttributes{
		Kind:    "bot",
		BotID:   botID,
		BotName: botName,
	}, event, stateVal)
}

func (m *Manager) pushDesktopActivity(activityKey, target, event string, stateVal LiveActivityContentState) {
	m.pushLiveActivity(activityKey, LiveActivityAttributes{
		Kind:   "desktop",
		Target: target,
	}, event, stateVal)
}

func (m *Manager) pushLiveActivity(activityKey string, attrs LiveActivityAttributes, event string, stateVal LiveActivityContentState) {
	devices, err := m.store.ListPushDevices()
	if err != nil {
		return
	}
	nowUnix := time.Now().Unix()
	for _, d := range devices {
		if !d.Enabled || d.Platform != "ios" {
			continue
		}
		actToken := d.ActivityTokens[activityKey]
		sendEvent := event
		token := actToken
		tokenField := "activity"

		if token == "" {
			if event == "end" || d.LiveActivityStartToken == "" {
				continue
			}
			// Push-to-Start when no active activity token is registered yet.
			sendEvent = "start"
			token = d.LiveActivityStartToken
			tokenField = "start"
		}

		aps := APSBody{
			Event:        sendEvent,
			ContentState: &stateVal,
			Timestamp:    nowUnix,
		}
		if sendEvent == "start" {
			aps.AttributesType = "PebbleActivityAttributes"
			aps.Attributes = &attrs
		}
		if sendEvent == "end" {
			aps.DismissalDate = nowUnix + dismissAfterSeconds
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := m.sender.Send(ctx, SendRequest{
			Platform: d.Platform,
			Env:      d.Env,
			BundleID: d.BundleID,
			PushType: PushTypeLiveActivity,
			Token:    token,
			Priority: 10,
			Payload: APNSPayload{
				APS:    aps,
				BotID:  attrs.BotID,
				Target: attrs.Target,
				Kind:   attrs.Kind,
			},
		})
		cancel()
		if errors.Is(err, ErrInvalidToken) {
			_ = m.store.ClearInvalidPushToken(d.DeviceID, tokenField, activityKey)
		}
	}
}

func decodePayload(v any, dst any) bool {
	if v == nil {
		return false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, dst) == nil
}

var (
	mdFenceRe       = regexp.MustCompile("(?s)(?:```|~~~)[^\n]*\n?.*?(?:```|~~~)")
	mdMathBlockRe   = regexp.MustCompile(`(?s)\$\$(.*?)\$\$`)
	mdImageRe       = regexp.MustCompile(`!\[([^\]]*)\]\([^)\s]+(?:\s+"[^"]*")?\)`)
	mdLinkRe        = regexp.MustCompile(`\[([^\]]+)\]\([^)\s]+(?:\s+"[^"]*")?\)`)
	mdAutoLinkRe    = regexp.MustCompile(`<(https?://[^>\s]+|mailto:[^>\s]+)>`)
	mdBrRe          = regexp.MustCompile(`(?i)<br\s*/?>`)
	mdSafeBlockTag  = regexp.MustCompile(`(?i)</?(?:details|summary)(?:\s+open)?>`)
	mdSafeInlineTag = regexp.MustCompile(`(?i)</?(?:sup|sub|kbd|u|s|del|b|strong|i|em|code|mark)>`)
	mdTableSepRe    = regexp.MustCompile(`^\s*\|?(?:\s*:?-{3,}:?\s*\|)+\s*:?-{3,}:?\s*\|?\s*$`)
	mdHeadingRe     = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	mdQuoteRe       = regexp.MustCompile(`^\s*(?:>\s*)+`)
	mdTaskRe        = regexp.MustCompile(`^\s*[-*+]\s+\[[xX ]\]\s+`)
	mdListRe        = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)
	mdRuleRe        = regexp.MustCompile(`^\s*(?:[-*_]\s*){3,}$`)
	mdFootnoteDefRe = regexp.MustCompile(`^\s*\[\^([^\]]+)\]:\s*`)
	mdFootnoteRefRe = regexp.MustCompile(`\[\^([^\]]+)\]`)
	mdInlineCodeRe  = regexp.MustCompile("`+([^`]+)`+")
	mdBoldItalicRe  = regexp.MustCompile(`\*\*\*([^*\n]+)\*\*\*|___([^_\n]+)___`)
	mdBoldRe        = regexp.MustCompile(`\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
	mdStrikeRe      = regexp.MustCompile(`~~([^~\n]+)~~`)
	mdItalicStarRe  = regexp.MustCompile(`\*([^*\n]+)\*`)
	mdItalicUnderRe = regexp.MustCompile(`(^|[^\pL\pN_])_([^_\n]+?)_([^\pL\pN_]|$)`)
	mdInlineMathRe  = regexp.MustCompile(`\$([^\s$][^$\n]*?[^\s$]|[^\s$])\$`)
	mdEscapeRe      = regexp.MustCompile("\\\\([\\\\`*_{}\\[\\]()#+\\-.!|>~$])")
)

var latexPlainMap = map[string]string{
	"sum": "∑", "prod": "∏", "int": "∫", "oint": "∮",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε",
	"zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ",
	"lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π", "rho": "ρ",
	"sigma": "σ", "tau": "τ", "upsilon": "υ", "phi": "φ", "varphi": "φ", "chi": "χ",
	"psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π",
	"Sigma": "Σ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	"leq": "≤", "le": "≤", "geq": "≥", "ge": "≥", "neq": "≠", "ne": "≠",
	"approx": "≈", "equiv": "≡", "sim": "∼", "simeq": "≃", "propto": "∝",
	"times": "×", "div": "÷", "cdot": "·", "pm": "±", "mp": "∓", "ast": "∗", "star": "⋆",
	"infty": "∞", "partial": "∂", "nabla": "∇", "forall": "∀", "exists": "∃",
	"in": "∈", "notin": "∉", "ni": "∋", "subset": "⊂", "subseteq": "⊆", "supset": "⊃", "supseteq": "⊇",
	"cup": "∪", "cap": "∩", "emptyset": "∅", "varnothing": "∅",
	"to": "→", "rightarrow": "→", "leftarrow": "←", "leftrightarrow": "↔",
	"Rightarrow": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "mapsto": "↦",
	"land": "∧", "lor": "∨", "lnot": "¬", "neg": "¬", "therefore": "∴", "because": "∵",
	"dots": "…", "ldots": "…", "cdots": "⋯", "vdots": "⋮",
	"angle": "∠", "degree": "°", "circ": "∘", "prime": "′",
	"quad": " ", "qquad": " ", ",": " ", ";": " ", ":": " ", "!": "",
}

func readLatexGroup(r []rune, pos int) (string, int) {
	for pos < len(r) && (r[pos] == ' ' || r[pos] == '\t') {
		pos++
	}
	if pos >= len(r) {
		return "", pos
	}
	if r[pos] != '{' {
		if r[pos] == '\\' {
			end := pos + 1
			for end < len(r) && ((r[end] >= 'a' && r[end] <= 'z') || (r[end] >= 'A' && r[end] <= 'Z')) {
				end++
			}
			if end == pos+1 && end < len(r) {
				end++
			}
			return string(r[pos:end]), end
		}
		return string(r[pos]), pos + 1
	}
	depth := 0
	start := pos + 1
	for i := pos; i < len(r); i++ {
		if r[i] == '{' {
			depth++
		} else if r[i] == '}' {
			depth--
			if depth == 0 {
				return string(r[start:i]), i + 1
			}
		}
	}
	return string(r[start:]), len(r)
}

func wrapMathArgIfComplex(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len([]rune(s)) == 1 || strings.ContainsAny(s, " \t+-*/=<>") == false {
		return s
	}
	return "(" + s + ")"
}

func latexToPlain(expr string) string {
	r := []rune(strings.TrimSpace(expr))
	var b strings.Builder
	for i := 0; i < len(r); {
		ch := r[i]
		if ch == '\\' {
			if i+1 < len(r) && r[i+1] == '\\' {
				b.WriteByte(' ')
				i += 2
				continue
			}
			if i+1 < len(r) && !((r[i+1] >= 'a' && r[i+1] <= 'z') || (r[i+1] >= 'A' && r[i+1] <= 'Z')) {
				if mapped, ok := latexPlainMap[string(r[i+1])]; ok {
					b.WriteString(mapped)
				} else {
					b.WriteRune(r[i+1])
				}
				i += 2
				continue
			}
			end := i + 1
			for end < len(r) && ((r[end] >= 'a' && r[end] <= 'z') || (r[end] >= 'A' && r[end] <= 'Z')) {
				end++
			}
			cmd := string(r[i+1 : end])
			i = end
			switch cmd {
			case "left", "right", "big", "Big", "bigg", "Bigg":
				continue
			case "frac", "dfrac", "tfrac":
				num, next1 := readLatexGroup(r, i)
				den, next2 := readLatexGroup(r, next1)
				i = next2
				b.WriteString(wrapMathArgIfComplex(latexToPlain(num)))
				b.WriteByte('/')
				b.WriteString(wrapMathArgIfComplex(latexToPlain(den)))
			case "sqrt":
				arg, next := readLatexGroup(r, i)
				i = next
				plainArg := strings.TrimSpace(latexToPlain(arg))
				b.WriteRune('√')
				if len([]rune(plainArg)) <= 1 {
					b.WriteString(plainArg)
				} else {
					b.WriteByte('(')
					b.WriteString(plainArg)
					b.WriteByte(')')
				}
			case "text", "mathrm", "mathbf", "mathit", "mathsf", "mathtt", "boldsymbol", "operatorname", "hat", "bar", "vec", "tilde":
				arg, next := readLatexGroup(r, i)
				i = next
				b.WriteString(latexToPlain(arg))
			default:
				if mapped, ok := latexPlainMap[cmd]; ok {
					b.WriteString(mapped)
				} else {
					b.WriteString(cmd)
				}
			}
			continue
		}
		if ch == '^' || ch == '_' {
			arg, next := readLatexGroup(r, i+1)
			i = next
			b.WriteRune(ch)
			b.WriteString(latexToPlain(arg))
			continue
		}
		if ch == '{' || ch == '}' {
			i++
			continue
		}
		b.WriteRune(ch)
		i++
	}
	return strings.TrimSpace(b.String())
}

func stripMarkdownForPreview(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = mdFenceRe.ReplaceAllString(s, " [Code] ")
	s = mdMathBlockRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m, "$$"), "$$"))
		if inner == "" {
			return " [Formula] "
		}
		return " " + latexToPlain(inner) + " "
	})

	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if mdTableSepRe.MatchString(trimmed) || mdRuleRe.MatchString(trimmed) {
			continue
		}
		line = mdFootnoteDefRe.ReplaceAllString(line, "[$1] ")
		line = mdQuoteRe.ReplaceAllString(line, "")
		line = mdHeadingRe.ReplaceAllString(line, "")
		line = mdTaskRe.ReplaceAllString(line, "")
		line = mdListRe.ReplaceAllString(line, "")
		if strings.HasPrefix(strings.TrimSpace(line), "|") && strings.HasSuffix(strings.TrimSpace(line), "|") {
			line = strings.ReplaceAll(line, `\|`, "¦")
			cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
			cleaned := make([]string, 0, len(cells))
			for _, c := range cells {
				c = strings.TrimSpace(strings.ReplaceAll(c, "¦", "|"))
				if c != "" {
					cleaned = append(cleaned, c)
				}
			}
			line = strings.Join(cleaned, " · ")
		}
		kept = append(kept, line)
	}
	s = strings.Join(kept, " ")
	s = mdBrRe.ReplaceAllString(s, " ")
	s = mdSafeBlockTag.ReplaceAllString(s, " ")
	s = mdSafeInlineTag.ReplaceAllString(s, "")
	s = mdImageRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdImageRe.FindStringSubmatch(m)
		if len(sub) > 1 && strings.TrimSpace(sub[1]) != "" {
			return "[Image: " + strings.TrimSpace(sub[1]) + "]"
		}
		return "[Image]"
	})
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = mdAutoLinkRe.ReplaceAllString(s, "$1")
	s = mdFootnoteRefRe.ReplaceAllString(s, "[$1]")
	s = mdInlineCodeRe.ReplaceAllString(s, "$1")
	s = mdBoldItalicRe.ReplaceAllString(s, "$1$2")
	s = mdBoldRe.ReplaceAllString(s, "$1$2")
	s = mdStrikeRe.ReplaceAllString(s, "$1")
	s = mdItalicStarRe.ReplaceAllString(s, "$1")
	s = mdItalicUnderRe.ReplaceAllString(s, "$1$2$3")
	s = mdInlineMathRe.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) <= 2 {
			return m
		}
		return latexToPlain(m[1 : len(m)-1])
	})
	s = mdEscapeRe.ReplaceAllString(s, "$1")
	return s
}

func compactPreview(s string, maxRunes int) string {
	s = stripMarkdownForPreview(s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}
