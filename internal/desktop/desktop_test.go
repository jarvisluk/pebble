package desktop

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCoordinatorTakeover(t *testing.T) {
	var mu sync.Mutex
	var takeovers []TakeoverRequest
	var states []ScreenState

	coord := NewCoordinator(
		func(st ScreenState) {
			mu.Lock()
			states = append(states, st)
			mu.Unlock()
		},
		func(list []TakeoverRequest) {
			mu.Lock()
			takeovers = list
			mu.Unlock()
		},
	)

	if st := coord.ScreenState("local"); st.Owner != "idle" {
		t.Fatalf("expected initial idle state, got %+v", st)
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
	if st := coord.ScreenState("local"); st.Owner != "human" || st.BotID != "helper" {
		t.Fatalf("expected human owner during takeover, got %+v", st)
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

	if st := coord.ScreenState("local"); st.Owner != "idle" {
		t.Fatalf("expected idle after resolve, got %+v", st)
	}

	// CancelTakeoversByBot aborts pending takeover cleanly.
	cancelCh := make(chan error, 1)
	go func() {
		_, err := coord.RequestTakeover(context.Background(), "node-b", "ops", "Ops", "2FA", "")
		cancelCh <- err
	}()
	for time.Now().Before(time.Now().Add(2 * time.Second)) {
		if len(coord.ListTakeovers()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	coord.CancelTakeoversByBot("ops")
	select {
	case err := <-cancelCh:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled after CancelTakeoversByBot, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestTakeover did not abort after CancelTakeoversByBot")
	}
	if st := coord.ScreenState("node-b"); st.Owner != "idle" {
		t.Fatalf("expected idle after cancel, got %+v", st)
	}

	mu.Lock()
	_ = takeovers
	_ = states
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

func TestEngineScreenRecordingDeniedCache(t *testing.T) {
	eng := NewEngine()
	eng.recordScreenRecording("denied")
	eng.permMu.Lock()
	if !eng.srDenied || eng.srChecked.IsZero() {
		eng.permMu.Unlock()
		t.Fatalf("expected srDenied=true and srChecked set")
	}
	eng.permMu.Unlock()

	eng.recordScreenRecording("granted")
	eng.permMu.Lock()
	if eng.srDenied {
		eng.permMu.Unlock()
		t.Fatalf("expected srDenied=false after granted")
	}
	eng.permMu.Unlock()
}
