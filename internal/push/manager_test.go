package push

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"bot-hub/internal/state"
)

type mockSender struct {
	mu   sync.Mutex
	reqs []SendRequest
	err  error
}

func (m *mockSender) Mode() string { return "direct" }

func (m *mockSender) Send(_ context.Context, req SendRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, req)
	return m.err
}

func (m *mockSender) list() []SendRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SendRequest, len(m.reqs))
	copy(out, m.reqs)
	return out
}

func TestManagerLiveActivityThrottleAndTakeover(t *testing.T) {
	store, err := state.NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.UpsertPushDevice(state.PushDevice{
		DeviceID:               "iphone-1",
		Platform:               "ios",
		Env:                    "sandbox",
		BundleID:               "local.bothub.mobile",
		PushToken:              "push-tok",
		LiveActivityStartToken: "start-tok",
		Enabled:                true,
	}); err != nil {
		t.Fatal(err)
	}

	sender := &mockSender{}
	mgr := NewManagerWithSender(store, sender)
	defer mgr.Close()

	// 1. Bot becomes busy -> Push-to-Start live activity.
	mgr.processEvent(rawEvent{
		Type:  "bot_status",
		BotID: "alpha",
		Payload: map[string]any{
			"id":     "alpha",
			"name":   "Alpha",
			"status": "busy",
		},
	})
	reqs := sender.list()
	if len(reqs) != 1 || reqs[0].PushType != PushTypeLiveActivity || reqs[0].Token != "start-tok" {
		t.Fatalf("expected push-to-start liveactivity, got %+v", reqs)
	}

	// Register activity token for bot:alpha, then burst two tool_use events -> both throttled within 3s.
	_ = store.SetPushActivityToken("iphone-1", "bot:alpha", "act-tok")
	mgr.processEvent(rawEvent{Type: "tool_use", BotID: "alpha", Payload: map[string]any{"botId": "alpha", "tool": "bash"}})
	mgr.processEvent(rawEvent{Type: "tool_use", BotID: "alpha", Payload: map[string]any{"botId": "alpha", "tool": "read"}})
	if len(sender.list()) != 1 {
		t.Fatalf("expected tool_use within 3s to be throttled, got %d requests", len(sender.list()))
	}

	// 2. Bot returns idle -> end live activity.
	mgr.processEvent(rawEvent{
		Type:  "bot_status",
		BotID: "alpha",
		Payload: map[string]any{
			"id":     "alpha",
			"name":   "Alpha",
			"status": "idle",
		},
	})
	reqs = sender.list()
	if len(reqs) != 2 || reqs[1].Token != "act-tok" {
		t.Fatalf("expected end liveactivity on act-tok, got %+v", reqs)
	}
	endPayload, _ := reqs[1].Payload.(APNSPayload)
	if endPayload.APS.Event != "end" || endPayload.APS.ContentState.StepCount != 2 {
		t.Fatalf("unexpected end payload: %+v", endPayload.APS)
	}

	// 3. Takeover request -> time-sensitive alert + desktop liveactivity start.
	mgr.processEvent(rawEvent{
		Type: "takeover",
		Payload: map[string]any{
			"items": []map[string]any{
				{"id": "tk-1", "target": "local", "reason": "需要扫码登录"},
			},
		},
	})
	reqs = sender.list()
	if len(reqs) != 4 {
		t.Fatalf("expected 4 total requests after takeover, got %d", len(reqs))
	}
	alertPayload, _ := reqs[2].Payload.(APNSPayload)
	if alertPayload.APS.InterruptionLevel != "time-sensitive" || alertPayload.DeepLink != "bothub://desktop?target=local" {
		t.Fatalf("unexpected takeover alert payload: %+v", alertPayload)
	}
}
