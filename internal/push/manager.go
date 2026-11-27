package push

import (
	"bot-hub/internal/config"
	"bot-hub/internal/state"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
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
	eventCh            chan rawEvent
	stopCh             chan struct{}
	stopOnce           sync.Once
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
		store:              store,
		sender:             sender,
		botRuns:            make(map[string]*botRunState),
		activeTakeover:     make(map[string]string),
		knownQuestions:     make(map[string]bool),
		knownApprovals:     make(map[string]bool),
		activeApprovalBots: make(map[string]bool),
		botNames:           make(map[string]string),
		eventCh:            make(chan rawEvent, eventQueueSize),
		stopCh:             make(chan struct{}),
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
