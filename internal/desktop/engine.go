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
	Platform        string  `json:"platform"`
	Supported       bool    `json:"supported"`
	WebRTCAvailable bool    `json:"webrtcAvailable"`
	Backend         string  `json:"backend"`
	ScreenRecording string  `json:"screenRecording,omitempty"`
	Accessibility   string  `json:"accessibility,omitempty"`
	LogicalX        float64 `json:"logicalX"`
	LogicalY        float64 `json:"logicalY"`
	LogicalWidth    float64 `json:"logicalWidth"`
	LogicalHeight   float64 `json:"logicalHeight"`
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

// Engine drives local desktop capture, WebRTC streaming, window inspection, and input injection,
// preferring opencode-remote-desktop's native `bot-host` sidecar with OS fallback when absent.
type Engine struct {
	host     *botHostClient
	wakeMu   sync.Mutex
	lastWake time.Time
}

func NewEngine() *Engine {
	return &Engine{host: newBotHostClient()}
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
	if e != nil && e.host != nil {
		e.host.Close()
	}
}

func (e *Engine) Status(ctx context.Context) (*HostStatus, error) {
	b := fallbackBounds(ctx)
	st := &HostStatus{
		Platform:        runtime.GOOS,
		Supported:       runtime.GOOS == "darwin" || runtime.GOOS == "windows" || e.host.Available(),
		WebRTCAvailable: e.host.Available(),
		Backend:         "fallback",
		LogicalX:        b.X,
		LogicalY:        b.Y,
		LogicalWidth:    b.Width,
		LogicalHeight:   b.Height,
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
		if (err == nil && len(hs.Displays) == 0 && runtime.GOOS == "darwin") || (err != nil && runtime.GOOS == "darwin") {
			if e.wakeMacDisplay(true) {
				var hsRetry hostStatusPayload
				if rerr := e.host.Request(ctx, "host.status", map[string]any{}, &hsRetry); rerr == nil {
					hs = hsRetry
					err = nil
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
	return st, nil
}

func (e *Engine) Windows(ctx context.Context) ([]WindowInfo, error) {
	if e.host.Available() {
		var list []WindowInfo
		if err := e.host.Request(ctx, "window.list", map[string]any{}, &list); err == nil {
			return list, nil
		}
	}
	return listFallbackWindows(ctx)
}

func (e *Engine) Focus(ctx context.Context, app, windowID string) error {
	return focusFallbackWindow(ctx, app, windowID)
}

func (e *Engine) Frame(ctx context.Context, windowID string, maxDim, quality int) (*FrameResult, error) {
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
	return captureFallbackFrame(ctx, windowID, maxDim, quality)
}

func (e *Engine) SendInput(ctx context.Context, ev UnifiedInput) error {
	if e.host.Available() {
		events, ok := toBotHostEvents(ev)
		if ok {
			if ev.App != "" && strings.ToLower(ev.Kind) != "move" {
				_ = focusFallbackWindow(ctx, ev.App, ev.WindowID)
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
	return injectFallbackInput(ctx, ev)
}

func (e *Engine) StreamOffer(ctx context.Context, sdp, target string, fps, maxDim, bitrate int) (*StreamOfferResult, error) {
	if !e.host.Available() {
		return nil, errors.New("bot-host unavailable; using JPEG stream")
	}
	e.wakeMacDisplay(false)
	if fps <= 0 {
		fps = 30
	}
	if maxDim <= 0 {
		maxDim = 1600
	}
	if bitrate <= 0 {
		bitrate = 3_000_000
	}
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
	return &res, nil
}

func (e *Engine) StreamClose(ctx context.Context, session string) error {
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
	t = strings.TrimSpace(t)
	if t == "" {
		return "local"
	}
	return t
}

func FormatScreenError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("桌面操作失败: %v", err)
}
