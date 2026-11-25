package desktop

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
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

func TestClassifyScreencaptureError(t *testing.T) {
	err := classifyScreencaptureError("could not create image from display")
	if !errors.Is(err, ErrScreenRecordingDenied) {
		t.Fatalf("expected errors.Is(ErrScreenRecordingDenied), got %v", err)
	}
	if strings.Contains(err.Error(), "失败") {
		t.Fatalf("unexpected CJK in error: %v", err)
	}

	other := classifyScreencaptureError("invalid display id")
	if errors.Is(other, ErrScreenRecordingDenied) {
		t.Fatalf("did not expect ErrScreenRecordingDenied for generic error: %v", other)
	}
}

func TestEngineIsScreenRecordingDeniedNativeFallback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS native screen recording preflight only runs on darwin")
	}
	orig := screenRecordingGrantedFn
	defer func() { screenRecordingGrantedFn = orig }()

	eng := NewEngine()
	eng.host = nil
	screenRecordingGrantedFn = func() (bool, bool) {
		return false, true
	}
	if !eng.isScreenRecordingDenied(context.Background()) {
		t.Fatalf("expected isScreenRecordingDenied=true when native preflight returns denied")
	}
	eng.permMu.Lock()
	cachedDenied := eng.srDenied
	cachedAt := eng.srChecked
	eng.permMu.Unlock()
	if !cachedDenied || cachedAt.IsZero() {
		t.Fatalf("expected srDenied cached true and srChecked set, got denied=%v at=%v", cachedDenied, cachedAt)
	}

	eng.resetPermissionCache()
	screenRecordingGrantedFn = func() (bool, bool) {
		return true, true
	}
	if eng.isScreenRecordingDenied(context.Background()) {
		t.Fatalf("expected isScreenRecordingDenied=false when native preflight returns granted")
	}
}

func TestIsScreenRecordingDeniedError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "sentinel", err: ErrScreenRecordingDenied, want: true},
		{name: "wrapped", err: fmt.Errorf("failed: %w", ErrScreenRecordingDenied), want: true},
		{name: "sck_tcc_declined", err: errors.New("The user declined TCCs for application, window, display capture"), want: true},
		{name: "remote_rpc_zh", err: errors.New("缺少 macOS 屏幕录制权限，请在系统设置中允许"), want: true},
		{name: "network_err", err: errors.New("connection refused"), want: false},
	}
	for _, tc := range cases {
		if got := IsScreenRecordingDeniedError(tc.err); got != tc.want {
			t.Errorf("%s: IsScreenRecordingDeniedError(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}
