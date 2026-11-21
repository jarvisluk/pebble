package desktop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCoordinatorScreenLockAndTakeover(t *testing.T) {
	var mu sync.Mutex
	var inputStates []bool
	var takeovers []TakeoverRequest

	coord := NewCoordinator(
		nil,
		func(list []TakeoverRequest) {
			mu.Lock()
			takeovers = list
			mu.Unlock()
		},
		func(_ string, enabled bool) {
			mu.Lock()
			inputStates = append(inputStates, enabled)
			mu.Unlock()
		},
	)
	coord.grace = 40 * time.Millisecond

	// Agent acquires screen -> human input locked.
	coord.BeginAgentTool("local", "ops", "Ops", "click")
	if st := coord.ScreenState("local"); st.Owner != "agent" || st.BotID != "ops" {
		t.Fatalf("expected agent lock, got %+v", st)
	}
	if err := coord.CheckHumanInput("local"); !errors.Is(err, ErrAgentBusy) {
		t.Fatalf("expected ErrAgentBusy while agent owns screen, got %v", err)
	}

	// EndAgentTool releases after grace window.
	coord.EndAgentTool("local", "ops")
	time.Sleep(80 * time.Millisecond)
	if st := coord.ScreenState("local"); st.Owner != "idle" {
		t.Fatalf("expected idle after grace window, got %+v", st)
	}
	if err := coord.CheckHumanInput("local"); err != nil {
		t.Fatalf("expected human input allowed after grace, got %v", err)
	}

	// ForceTakeover returns active botID and unlocks input.
	coord.BeginAgentTool("node-b", "ops", "Ops", "type")
	paused := coord.ForceTakeover("node-b")
	if paused != "ops" {
		t.Fatalf("ForceTakeover paused=%q, want ops", paused)
	}
	if st := coord.ScreenState("node-b"); st.Owner != "human" {
		t.Fatalf("expected human after ForceTakeover, got %+v", st)
	}

	// RequestTakeover blocks until ResolveTakeover wakes it.
	resCh := make(chan string, 1)
	go func() {
		note, err := coord.RequestTakeover(context.Background(), "local", "helper", "Helper", "需要扫码登录", "WeChat")
		if err != nil {
			resCh <- "ERR:" + err.Error()
			return
		}
		resCh <- note
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(coord.ListTakeovers()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	list := coord.ListTakeovers()
	if len(list) != 1 || list[0].Reason != "需要扫码登录" {
		t.Fatalf("expected pending takeover, got %+v", list)
	}
	if !coord.ResolveTakeover(list[0].ID, "已扫码完成") {
		t.Fatalf("expected ResolveTakeover to return true")
	}

	select {
	case got := <-resCh:
		if got != "已扫码完成" {
			t.Fatalf("got takeover note %q, want 已扫码完成", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestTakeover did not unblock after ResolveTakeover")
	}

	mu.Lock()
	_ = takeovers
	if len(inputStates) == 0 {
		t.Fatalf("expected SetInput callbacks to fire")
	}
	mu.Unlock()
}

func TestToBotHostEvents(t *testing.T) {
	x, y := 120.5, 240.5
	evs, ok := toBotHostEvents(UnifiedInput{Kind: "click", X: &x, Y: &y})
	if !ok || len(evs) != 2 || evs[0]["type"] != "down" || evs[1]["type"] != "up" {
		t.Fatalf("unexpected click expansion: ok=%v evs=%+v", ok, evs)
	}
	kevs, ok := toBotHostEvents(UnifiedInput{Kind: "key", Key: "cmd+c"})
	if !ok || len(kevs) != 2 || kevs[0]["code"] != "KeyC" || kevs[0]["meta"] != true {
		t.Fatalf("unexpected key combo mapping: ok=%v evs=%+v", ok, kevs)
	}
}
