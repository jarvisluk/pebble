package desktop

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// HostStatus describes the desktop capture/input capabilities and primary bounds of a machine.
type HostStatus struct {
	Platform        string   `json:"platform"`
	Supported       bool     `json:"supported"`
	WebRTCAvailable bool     `json:"webrtcAvailable"`
	Backend         string   `json:"backend"`
	ScreenRecording string   `json:"screenRecording,omitempty"`
	Accessibility   string   `json:"accessibility,omitempty"`
	LogicalX        float64  `json:"logicalX"`
	LogicalY        float64  `json:"logicalY"`
	LogicalWidth    float64  `json:"logicalWidth"`
	LogicalHeight   float64  `json:"logicalHeight"`
	Virtual         bool     `json:"virtual,omitempty"`
	PowerState      string   `json:"powerState,omitempty"`
	KeepAwake       bool     `json:"keepAwake,omitempty"`
	Installable     bool     `json:"installable,omitempty"`
	MissingDeps     []string `json:"missingDeps,omitempty"`
	InstallCommand  string   `json:"installCommand,omitempty"`
	DisplayServer   string   `json:"displayServer,omitempty"`
}

// WindowInfo describes an open desktop window.
type WindowInfo struct {
	ID      string  `json:"id"`
	App     string  `json:"app"`
	Title   string  `json:"title"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Focused bool    `json:"focused"`
}

// FrameResult is a single compressed screen frame with logical mapping metadata.
type FrameResult struct {
	JPEGBase64    string  `json:"jpegBase64"`
	ImageWidth    int     `json:"imageWidth"`
	ImageHeight   int     `json:"imageHeight"`
	LogicalX      float64 `json:"logicalX"`
	LogicalY      float64 `json:"logicalY"`
	LogicalWidth  float64 `json:"logicalWidth"`
	LogicalHeight float64 `json:"logicalHeight"`
	Backend       string  `json:"backend"`
	Timestamp     int64   `json:"timestamp"`
}

// StreamOfferResult returns the WebRTC SDP answer and session ID from bot-host.
type StreamOfferResult struct {
	SDP     string `json:"sdp"`
	Session string `json:"session"`
	Codec   string `json:"codec,omitempty"`
	Backend string `json:"backend,omitempty"`
}

// UnifiedInput represents a mouse, scroll, keyboard, or text event.
type UnifiedInput struct {
	Kind     string   `json:"kind"` // move, down, up, click, double_click, right_click, wheel, scroll, key, keydown, keyup, type, text
	X        *float64 `json:"x,omitempty"`
	Y        *float64 `json:"y,omitempty"`
	Button   string   `json:"button,omitempty"`
	DX       float64  `json:"dx,omitempty"`
	DY       float64  `json:"dy,omitempty"`
	DeltaY   float64  `json:"deltaY,omitempty"`
	Key      string   `json:"key,omitempty"`
	Code     string   `json:"code,omitempty"`
	Shift    bool     `json:"shift,omitempty"`
	Ctrl     bool     `json:"ctrl,omitempty"`
	Alt      bool     `json:"alt,omitempty"`
	Meta     bool     `json:"meta,omitempty"`
	Text     string   `json:"text,omitempty"`
	WindowID string   `json:"windowId,omitempty"`
	App      string   `json:"app,omitempty"`
}

// ErrScreenRecordingDenied indicates macOS Screen Recording permission is not granted.
var ErrScreenRecordingDenied = errors.New("macOS Screen Recording permission is denied; allow Pebble in System Settings -> Privacy & Security -> Screen & System Audio Recording and retry")

var screenRecordingGrantedFn = nativeScreenRecordingGranted

// IsScreenRecordingDeniedError reports whether err indicates macOS Screen Recording permission denial.
func IsScreenRecordingDeniedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrScreenRecordingDenied) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "screen_recording_denied") ||
		strings.Contains(msg, "declined tccs") ||
		strings.Contains(msg, "user declined") ||
		strings.Contains(msg, "屏幕录制权限") || // i18n:allow-cjk
		strings.Contains(msg, "screen recording permission") {
		return true
	}
	return strings.Contains(msg, "screen recording") &&
		(strings.Contains(msg, "denied") || strings.Contains(msg, "declined"))
}

// Engine drives local desktop capture, WebRTC streaming, window inspection, and input injection,
// preferring opencode-remote-desktop's native `bot-host` sidecar with OS fallback when absent.
type Engine struct {
	host         *botHostClient
	vm           *virtualManager
	wakeMu       sync.Mutex
	lastWake     time.Time
	permMu       sync.Mutex
	srDenied     bool
	srChecked    time.Time
	streamMu         sync.Mutex
	streamLease      func()
	streamLeaseCount int
	webrtcMu         sync.Mutex
	webrtcSessions   map[string]*nativeWebRTCSession
}

func NewEngine() *Engine {
	return NewEngineWithConfig(VirtualConfig{})
}

func NewEngineWithConfig(cfg VirtualConfig) *Engine {
	return NewEngineWithBackend(cfg, nil)
}

func NewEngineWithBackend(cfg VirtualConfig, backend virtualBackend) *Engine {
	vm := newVirtualManager(cfg, backend)
	host := newBotHostClient()
	host.envFn = vm.Env
	return &Engine{
		host: host,
		vm:   vm,
	}
}

func (e *Engine) VirtualSupported() bool {
	if e == nil || e.vm == nil {
		return false
	}
	return e.vm.backend.Probe().VirtualSupported
}

func (e *Engine) VirtualStatus() VirtualStatus {
	if e == nil || e.vm == nil {
		return VirtualStatus{}
	}
	return e.vm.Status()
}

func (e *Engine) recordScreenRecording(state string) {
	if state == "" {
		return
	}
	e.permMu.Lock()
	e.srDenied = strings.EqualFold(state, "denied")
	e.srChecked = time.Now()
	e.permMu.Unlock()
}

func (e *Engine) isScreenRecordingDenied(ctx context.Context) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	e.permMu.Lock()
	if !e.srChecked.IsZero() && time.Since(e.srChecked) < 2*time.Second {
		denied := e.srDenied
		e.permMu.Unlock()
		return denied
	}
	e.permMu.Unlock()

	if e.host != nil && e.host.Available() {
		var hs struct {
			ScreenRecording string `json:"screenRecording"`
		}
		if err := e.host.Request(ctx, "host.status", map[string]any{}, &hs); err == nil && hs.ScreenRecording != "" {
			e.recordScreenRecording(hs.ScreenRecording)
			return strings.EqualFold(hs.ScreenRecording, "denied")
		}
	}

	if granted, supported := screenRecordingGrantedFn(); supported {
		if granted {
			e.recordScreenRecording("granted")
			return false
		}
		e.recordScreenRecording("denied")
		return true
	}
	return false
}

// wakeMacDisplay wakes the macOS display via caffeinate so ScreenCaptureKit has an active display
// and VideoToolbox/screen captures do not see an empty display list or black frames while sleeping.
func (e *Engine) wakeMacDisplay(wait bool) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	now := time.Now()
	e.wakeMu.Lock()
	shouldRun := wait || e.lastWake.IsZero() || now.Sub(e.lastWake) > 3500*time.Millisecond
	if shouldRun {
		e.lastWake = now
	}
	e.wakeMu.Unlock()
	if !shouldRun {
		return false
	}
	cmd := exec.Command("/usr/bin/caffeinate", "-u", "-t", "5")
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { _ = cmd.Wait() }()
	if wait {
		time.Sleep(450 * time.Millisecond)
	}
	return true
}

func (e *Engine) Close() {
	if e == nil {
		return
	}
	e.closePionSession("")
	e.releaseStreamLease()
	if e.host != nil {
		e.host.Close()
	}
	if e.vm != nil {
		e.vm.Close()
	}
}

func (e *Engine) SetHasTakeover(fn func() bool) {
	if e != nil && e.vm != nil {
		e.vm.SetHasTakeover(fn)
	}
}

func (e *Engine) AcquireTaskLease() func() {
	if e != nil && e.vm != nil {
		return e.vm.AcquireLease()
	}
	return func() {}
}

func (e *Engine) PrepareForTask(ctx context.Context) error {
	return e.prepareActive(ctx)
}

func (e *Engine) Env() []string {
	if e != nil && e.vm != nil {
		return e.vm.Env()
	}
	return nil
}

func (e *Engine) Power(ctx context.Context, action string) (*HostStatus, error) {
	if e == nil || e.vm == nil {
		return nil, errors.New("desktop engine unavailable")
	}
	if strings.EqualFold(strings.TrimSpace(action), "stop") || strings.EqualFold(strings.TrimSpace(action), "freeze") {
		e.releaseStreamLease()
		if e.host != nil {
			_ = e.host.Request(ctx, "stream.close", map[string]any{}, nil)
		}
	}
	if err := e.vm.Power(ctx, action); err != nil {
		return nil, err
	}
	return e.Status(ctx)
}

func (e *Engine) Setup(ctx context.Context) (*SetupResult, error) {
	if e == nil || e.vm == nil {
		return nil, errors.New("desktop engine unavailable")
	}
	return e.vm.Setup(ctx)
}

func (e *Engine) prepareActive(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if e.vm != nil {
		usedVirtual, err := e.vm.EnsureAwake(ctx)
		if err != nil {
			return err
		}
		if usedVirtual {
			return nil
		}
	}
	if runtime.GOOS == "linux" {
		wakeLinuxNativeDisplay()
	}
	return nil
}

func (e *Engine) acquireStreamLease() {
	if e == nil || e.vm == nil {
		return
	}
	e.streamMu.Lock()
	defer e.streamMu.Unlock()
	if e.streamLeaseCount == 0 {
		e.streamLease = e.vm.AcquireLease()
	}
	e.streamLeaseCount++
}

func (e *Engine) releaseStreamLease() {
	if e == nil {
		return
	}
	e.streamMu.Lock()
	var rel func()
	if e.streamLeaseCount > 0 {
		e.streamLeaseCount--
	}
	if e.streamLeaseCount == 0 {
		rel = e.streamLease
		e.streamLease = nil
	}
	e.streamMu.Unlock()
	if rel != nil {
		rel()
	}
}

func (e *Engine) Status(ctx context.Context) (*HostStatus, error) {
	b := fallbackBounds(ctx)
	var vst VirtualStatus
	if e.vm != nil {
		vst = e.vm.Status()
	}
	_, hasFFmpeg := exec.LookPath("ffmpeg")
	nativeStreamReady := nativeCaptureAvailable() || hasFFmpeg == nil
	linuxSupported := runtime.GOOS == "linux" && (e.host.Available() || (vst.UsingNative && hasAgentDesktop()) || (vst.Virtual && vst.VirtualReady && hasAgentDesktop()))
	st := &HostStatus{
		Platform:        runtime.GOOS,
		Supported:       runtime.GOOS == "darwin" || runtime.GOOS == "windows" || linuxSupported,
		WebRTCAvailable: e.host.Available() || nativeStreamReady,
		Backend:         "fallback",
		LogicalX:        b.X,
		LogicalY:        b.Y,
		LogicalWidth:    b.Width,
		LogicalHeight:   b.Height,
		Virtual:         vst.Virtual,
		PowerState:      string(vst.PowerState),
		KeepAwake:       vst.KeepAwake,
		Installable:     vst.Installable,
		MissingDeps:     vst.MissingDeps,
		InstallCommand:  vst.InstallCommand,
		DisplayServer:   vst.DisplayServer,
	}
	if nativeCaptureAvailable() {
		st.Backend = "native (ScreenCaptureKit)"
	} else if hasFFmpeg == nil {
		st.Backend = "native (ffmpeg)"
	}
	if runtime.GOOS == "linux" && hasAgentDesktop() && !e.host.Available() {
		st.Backend = "agent-desktop"
	}
	if e.host.Available() {
		type hostStatusPayload struct {
			Platform        string `json:"platform"`
			Supported       bool   `json:"supported"`
			Accessibility   string `json:"accessibility"`
			ScreenRecording string `json:"screenRecording"`
			Backend         string `json:"backend"`
			StreamBackend   string `json:"streamBackend"`
			Displays        []struct {
				X         float64 `json:"x"`
				Y         float64 `json:"y"`
				Width     float64 `json:"width"`
				Height    float64 `json:"height"`
				IsPrimary bool    `json:"isPrimary"`
			} `json:"displays"`
		}
		var hs hostStatusPayload
		err := e.host.Request(ctx, "host.status", map[string]any{}, &hs)
		if err == nil {
			e.recordScreenRecording(hs.ScreenRecording)
		}
		if ((err == nil && len(hs.Displays) == 0 && !strings.EqualFold(hs.ScreenRecording, "denied")) || err != nil) && runtime.GOOS == "darwin" {
			if e.wakeMacDisplay(true) {
				var hsRetry hostStatusPayload
				if rerr := e.host.Request(ctx, "host.status", map[string]any{}, &hsRetry); rerr == nil {
					hs = hsRetry
					err = nil
					e.recordScreenRecording(hs.ScreenRecording)
				}
			}
		}
		if err == nil {
			st.Supported = hs.Supported
			st.WebRTCAvailable = hs.StreamBackend != ""
			st.Backend = "bot-host (" + hs.Backend + ")"
			st.ScreenRecording = hs.ScreenRecording
			st.Accessibility = hs.Accessibility
			for _, d := range hs.Displays {
				if d.IsPrimary || st.LogicalWidth == 0 {
					st.LogicalX, st.LogicalY = d.X, d.Y
					st.LogicalWidth, st.LogicalHeight = d.Width, d.Height
					if d.IsPrimary {
						break
					}
				}
			}
		}
	}
	if runtime.GOOS == "darwin" && (st.ScreenRecording == "" || st.Accessibility == "") {
		for _, p := range platformPermissions(ctx) {
			if p.ID == "screen_recording" && st.ScreenRecording == "" && p.Status != "unsupported" {
				st.ScreenRecording = p.Status
			} else if p.ID == "accessibility" && st.Accessibility == "" && p.Status != "unsupported" {
				st.Accessibility = p.Status
			}
		}
	}
	return st, nil
}

func (e *Engine) Windows(ctx context.Context) ([]WindowInfo, error) {
	if err := e.prepareActive(ctx); err != nil {
		return []WindowInfo{}, err
	}
	if e.host.Available() {
		if e.isScreenRecordingDenied(ctx) {
			return []WindowInfo{}, nil
		}
		var list []WindowInfo
		if err := e.host.Request(ctx, "window.list", map[string]any{}, &list); err != nil {
			return []WindowInfo{}, err
		}
		return list, nil
	}
	return listFallbackWindows(ctx, e.Env())
}

func (e *Engine) Focus(ctx context.Context, app, windowID string) error {
	if err := e.prepareActive(ctx); err != nil {
		return err
	}
	return focusFallbackWindow(ctx, e.Env(), app, windowID)
}

func (e *Engine) Frame(ctx context.Context, windowID string, maxDim, quality int) (*FrameResult, error) {
	if err := e.prepareActive(ctx); err != nil {
		return nil, err
	}
	if e.isScreenRecordingDenied(ctx) {
		return nil, ErrScreenRecordingDenied
	}
	if maxDim <= 0 {
		maxDim = 1440
	}
	if quality <= 0 {
		quality = 75
	}
	e.wakeMacDisplay(false)
	if e.host.Available() {
		params := map[string]any{
			"maxDim":  maxDim,
			"quality": quality,
		}
		if windowID != "" {
			params["target"] = windowID
		}
		type rawFramePayload struct {
			DataBase64     string  `json:"dataBase64"`
			Width          int     `json:"width"`
			Height         int     `json:"height"`
			LogicalWidth   float64 `json:"logicalWidth"`
			LogicalHeight  float64 `json:"logicalHeight"`
			LogicalOriginX float64 `json:"logicalOriginX"`
			LogicalOriginY float64 `json:"logicalOriginY"`
			Backend        string  `json:"backend"`
		}
		var raw rawFramePayload
		err := e.host.Request(ctx, "screen.capture", params, &raw)
		if (err != nil || raw.DataBase64 == "") && runtime.GOOS == "darwin" {
			if e.wakeMacDisplay(true) {
				var rawRetry rawFramePayload
				if rerr := e.host.Request(ctx, "screen.capture", params, &rawRetry); rerr == nil && rawRetry.DataBase64 != "" {
					raw = rawRetry
					err = nil
				}
			}
		}
		if err == nil && raw.DataBase64 != "" {
			return &FrameResult{
				JPEGBase64:    raw.DataBase64,
				ImageWidth:    raw.Width,
				ImageHeight:   raw.Height,
				LogicalX:      raw.LogicalOriginX,
				LogicalY:      raw.LogicalOriginY,
				LogicalWidth:  raw.LogicalWidth,
				LogicalHeight: raw.LogicalHeight,
				Backend:       "bot-host/" + raw.Backend,
				Timestamp:     time.Now().UnixMilli(),
			}, nil
		}
	}
	return captureFallbackFrame(ctx, e.Env(), windowID, maxDim, quality)
}

func (e *Engine) SendInput(ctx context.Context, ev UnifiedInput) error {
	if err := e.prepareActive(ctx); err != nil {
		return err
	}
	if e.host.Available() {
		events, ok := toBotHostEvents(ev)
		if ok {
			if ev.App != "" && strings.ToLower(ev.Kind) != "move" {
				_ = focusFallbackWindow(ctx, e.Env(), ev.App, ev.WindowID)
			}
			var firstErr error
			for _, item := range events {
				if err := e.host.Request(ctx, "input.inject", item, nil); err != nil {
					firstErr = err
					break
				}
			}
			if firstErr == nil {
				return nil
			}
		}
	}
	return injectFallbackInput(ctx, e.Env(), ev)
}

func (e *Engine) StreamOffer(ctx context.Context, sdp, target string, fps, maxDim, bitrate int) (*StreamOfferResult, error) {
	if fps <= 0 {
		fps = 60
	}
	if maxDim <= 0 {
		maxDim = 3840
	}
	if bitrate <= 0 {
		bitrate = 35_000_000
	}
	// Prefer embedded Pion WebRTC + native ScreenCaptureKit/ffmpeg for full-screen streams.
	if strings.TrimSpace(target) == "" || !e.host.Available() {
		return e.startPionWebRTC(ctx, sdp, fps, maxDim, bitrate)
	}
	if err := e.prepareActive(ctx); err != nil {
		return nil, err
	}
	if e.isScreenRecordingDenied(ctx) {
		return nil, ErrScreenRecordingDenied
	}
	e.wakeMacDisplay(false)
	params := map[string]any{
		"sdp":     sdp,
		"fps":     fps,
		"maxDim":  maxDim,
		"bitrate": bitrate,
	}
	if strings.TrimSpace(target) != "" {
		params["target"] = target
	}
	var res StreamOfferResult
	err := e.host.Request(ctx, "stream.offer", params, &res)
	if err != nil && runtime.GOOS == "darwin" {
		if e.wakeMacDisplay(true) {
			err = e.host.Request(ctx, "stream.offer", params, &res)
		}
	}
	if err != nil {
		return nil, err
	}
	e.acquireStreamLease()
	return &res, nil
}

func (e *Engine) StreamClose(ctx context.Context, session string) error {
	if strings.HasPrefix(session, "rtc_") {
		e.closePionSession(session)
		return nil
	}
	if session == "" {
		e.closePionSession("")
	}
	e.releaseStreamLease()
	if !e.host.Available() {
		return nil
	}
	params := map[string]any{}
	if session != "" {
		params["session"] = session
	}
	return e.host.Request(ctx, "stream.close", params, nil)
}

func (e *Engine) SetStreamInput(ctx context.Context, enabled bool) error {
	if !e.host.Available() {
		return nil
	}
	return e.host.Request(ctx, "stream.setInput", map[string]any{"enabled": enabled}, nil)
}

type keyModifiers struct {
	Shift, Ctrl, Alt, Meta bool
}

func (m keyModifiers) any() bool { return m.Shift || m.Ctrl || m.Alt || m.Meta }

func parseKeyCombo(combo string) (parts []string, mainKey string, mods keyModifiers) {
	for _, raw := range strings.Split(combo, "+") {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		switch strings.ToLower(p) {
		case "cmd", "command", "meta", "super", "win":
			mods.Meta = true
		case "ctrl", "control":
			mods.Ctrl = true
		case "alt", "option", "opt":
			mods.Alt = true
		case "shift":
			mods.Shift = true
		default:
			mainKey = p
		}
		parts = append(parts, p)
	}
	return parts, mainKey, mods
}

func normalizeWebKey(k string) (key, code string) {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "return", "enter":
		return "Enter", "Enter"
	case "esc", "escape":
		return "Escape", "Escape"
	case "tab":
		return "Tab", "Tab"
	case "space", "spacebar", " ":
		return " ", "Space"
	case "backspace":
		return "Backspace", "Backspace"
	case "delete", "del":
		return "Delete", "Delete"
	case "up", "arrowup":
		return "ArrowUp", "ArrowUp"
	case "down", "arrowdown":
		return "ArrowDown", "ArrowDown"
	case "left", "arrowleft":
		return "ArrowLeft", "ArrowLeft"
	case "right", "arrowright":
		return "ArrowRight", "ArrowRight"
	case "home":
		return "Home", "Home"
	case "end":
		return "End", "End"
	case "pageup":
		return "PageUp", "PageUp"
	case "pagedown":
		return "PageDown", "PageDown"
	}
	if len(k) == 1 {
		ch := strings.ToUpper(k)
		if ch[0] >= 'A' && ch[0] <= 'Z' {
			return strings.ToLower(k), "Key" + ch
		}
		if ch[0] >= '0' && ch[0] <= '9' {
			return k, "Digit" + k
		}
	}
	return k, k
}

// toBotHostEvents translates high-level UI/MCP inputs into `bot-host`'s RemoteInputEvent payloads.
func toBotHostEvents(ev UnifiedInput) ([]map[string]any, bool) {
	kind := strings.ToLower(strings.TrimSpace(ev.Kind))
	makeMouse := func(t, btn string) map[string]any {
		m := map[string]any{"type": t}
		if ev.X != nil {
			m["x"] = *ev.X
		}
		if ev.Y != nil {
			m["y"] = *ev.Y
		}
		if btn != "" {
			m["button"] = btn
		}
		return m
	}

	switch kind {
	case "move", "down", "up":
		btn := ev.Button
		if btn == "" && kind != "move" {
			btn = "left"
		}
		return []map[string]any{makeMouse(kind, btn)}, true
	case "click":
		btn := ev.Button
		if btn == "" {
			btn = "left"
		}
		return []map[string]any{makeMouse("down", btn), makeMouse("up", btn)}, true
	case "double_click", "dblclick":
		return []map[string]any{
			makeMouse("down", "left"), makeMouse("up", "left"),
			makeMouse("down", "left"), makeMouse("up", "left"),
		}, true
	case "right_click", "rightclick":
		return []map[string]any{makeMouse("down", "right"), makeMouse("up", "right")}, true
	case "wheel", "scroll":
		dy := ev.DY
		dx := ev.DX
		if dy == 0 && ev.DeltaY != 0 {
			dy = -ev.DeltaY * 24
		}
		return []map[string]any{{"type": "wheel", "dx": dx, "dy": dy}}, true
	case "type", "text":
		return []map[string]any{{"type": "text", "text": ev.Text}}, true
	case "keydown", "keyup":
		k, code := normalizeWebKey(ev.Key)
		if ev.Code != "" {
			code = ev.Code
		}
		return []map[string]any{{
			"type":  kind,
			"key":   k,
			"code":  code,
			"shift": ev.Shift,
			"ctrl":  ev.Ctrl,
			"alt":   ev.Alt,
			"meta":  ev.Meta,
		}}, true
	case "key":
		_, mainKey, mods := parseKeyCombo(ev.Key)
		if mainKey == "" && ev.Code == "" {
			return nil, false
		}
		shift := mods.Shift || ev.Shift
		ctrl := mods.Ctrl || ev.Ctrl
		alt := mods.Alt || ev.Alt
		meta := mods.Meta || ev.Meta
		k, code := normalizeWebKey(mainKey)
		if ev.Code != "" {
			code = ev.Code
		}
		base := map[string]any{
			"key":   k,
			"code":  code,
			"shift": shift,
			"ctrl":  ctrl,
			"alt":   alt,
			"meta":  meta,
		}
		down := cloneMap(base)
		down["type"] = "keydown"
		up := cloneMap(base)
		up["type"] = "keyup"
		return []map[string]any{down, up}, true
	default:
		return nil, false
	}
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func NormalizeTarget(t string) string {
	return ParseTarget(t).Raw
}

func FormatScreenError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("desktop action failed: %v", err)
}
