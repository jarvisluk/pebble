package push

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

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
