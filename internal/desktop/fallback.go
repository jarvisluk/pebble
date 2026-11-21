package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type displayBounds struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

var macBoundsCache struct {
	mu      sync.Mutex
	bounds  displayBounds
	fetched time.Time
}

func fallbackBounds(ctx context.Context) displayBounds {
	switch runtime.GOOS {
	case "darwin":
		macBoundsCache.mu.Lock()
		if time.Since(macBoundsCache.fetched) < 5*time.Second && macBoundsCache.bounds.Width > 0 {
			b := macBoundsCache.bounds
			macBoundsCache.mu.Unlock()
			return b
		}
		macBoundsCache.mu.Unlock()

		script := `ObjC.import('CoreGraphics');
var b = $.CGDisplayBounds($.CGMainDisplayID());
JSON.stringify({x: b.origin.x, y: b.origin.y, width: b.size.width, height: b.size.height});`
		out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output()
		if err == nil {
			var b displayBounds
			if json.Unmarshal(bytes.TrimSpace(out), &b) == nil && b.Width > 0 {
				macBoundsCache.mu.Lock()
				macBoundsCache.bounds = b
				macBoundsCache.fetched = time.Now()
				macBoundsCache.mu.Unlock()
				return b
			}
		}
		return displayBounds{Width: 1440, Height: 900}

	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms; $b=[System.Windows.Forms.SystemInformation]::VirtualScreen; @{x=$b.Left;y=$b.Top;width=$b.Width;height=$b.Height} | ConvertTo-Json -Compress`
		out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
		if err == nil {
			var b displayBounds
			if json.Unmarshal(bytes.TrimSpace(out), &b) == nil && b.Width > 0 {
				return b
			}
		}
		return displayBounds{Width: 1920, Height: 1080}

	default:
		return displayBounds{Width: 1920, Height: 1080}
	}
}

func captureFallbackFrame(ctx context.Context, windowID string, maxDim, quality int) (*FrameResult, error) {
	if maxDim <= 0 {
		maxDim = 1440
	}
	if quality <= 0 {
		quality = 75
	}
	tmpDir, err := os.MkdirTemp("", "bothub-frame-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	switch runtime.GOOS {
	case "darwin":
		rawPath := filepath.Join(tmpDir, "raw.jpg")
		args := []string{"-x", "-t", "jpg"}
		wid := strings.TrimPrefix(strings.TrimSpace(windowID), "w-")
		if n, err := strconv.Atoi(wid); err == nil && n > 0 {
			args = append(args, "-l", strconv.Itoa(n))
		}
		args = append(args, rawPath)
		if out, err := exec.CommandContext(ctx, "/usr/sbin/screencapture", args...).CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return nil, fmt.Errorf("screencapture 失败（请检查系统「屏幕录制」权限）: %s", msg)
		}

		finalPath := rawPath
		if maxDim > 0 {
			scaledPath := filepath.Join(tmpDir, "scaled.jpg")
			sips := exec.CommandContext(ctx, "/usr/bin/sips", "-Z", strconv.Itoa(maxDim), "-s", "format", "jpeg", "-s", "formatOptions", strconv.Itoa(quality), rawPath, "--out", scaledPath)
			if sips.Run() == nil {
				finalPath = scaledPath
			}
		}

		data, err := os.ReadFile(finalPath)
		if err != nil {
			return nil, err
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		bounds := fallbackBounds(ctx)
		return &FrameResult{
			JPEGBase64:    base64.StdEncoding.EncodeToString(data),
			ImageWidth:    cfg.Width,
			ImageHeight:   cfg.Height,
			LogicalX:      bounds.X,
			LogicalY:      bounds.Y,
			LogicalWidth:  bounds.Width,
			LogicalHeight: bounds.Height,
			Backend:       "fallback-screencapture",
			Timestamp:     time.Now().UnixMilli(),
		}, nil

	case "windows":
		jpgPath := filepath.Join(tmpDir, "frame.jpg")
		ps := `
Add-Type -AssemblyName System.Windows.Forms,System.Drawing
$b = [System.Windows.Forms.SystemInformation]::VirtualScreen
$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($b.Left, $b.Top, 0, 0, $bmp.Size)
$g.Dispose()
$maxW = [int]$env:BOTHUB_SHOT_MAXW
if ($maxW -gt 0 -and $bmp.Width -gt $maxW) {
  $h = [int]($bmp.Height * ($maxW / $bmp.Width))
  $scaled = New-Object System.Drawing.Bitmap $bmp, $maxW, $h
  $bmp.Dispose()
  $bmp = $scaled
}
$bmp.Save($env:BOTHUB_SHOT_PATH, [System.Drawing.Imaging.ImageFormat]::Jpeg)
$bmp.Dispose()
@{x=$b.Left;y=$b.Top;width=$b.Width;height=$b.Height} | ConvertTo-Json -Compress
`
		c := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
		c.Env = append(os.Environ(),
			"BOTHUB_SHOT_PATH="+jpgPath,
			"BOTHUB_SHOT_MAXW="+strconv.Itoa(maxDim),
		)
		out, err := c.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("windows capture failed: %s", strings.TrimSpace(string(out)))
		}
		data, err := os.ReadFile(jpgPath)
		if err != nil {
			return nil, err
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		var b displayBounds
		_ = json.Unmarshal(bytes.TrimSpace(out), &b)
		if b.Width <= 0 {
			b = displayBounds{Width: float64(cfg.Width), Height: float64(cfg.Height)}
		}
		return &FrameResult{
			JPEGBase64:    base64.StdEncoding.EncodeToString(data),
			ImageWidth:    cfg.Width,
			ImageHeight:   cfg.Height,
			LogicalX:      b.X,
			LogicalY:      b.Y,
			LogicalWidth:  b.Width,
			LogicalHeight: b.Height,
			Backend:       "fallback-win32",
			Timestamp:     time.Now().UnixMilli(),
		}, nil

	default:
		return nil, fmt.Errorf("desktop capture fallback unsupported on %s without bot-host", runtime.GOOS)
	}
}

func listFallbackWindows(ctx context.Context) ([]WindowInfo, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	script := `const se = Application('System Events');
const out = [];
for (const p of se.processes.whose({backgroundOnly: false})()) {
  const appName = p.name();
  const focused = !!p.frontmost();
  let wins = [];
  try { wins = p.windows(); } catch (e) { continue; }
  for (let i = 0; i < wins.length; i++) {
    try {
      const w = wins[i];
      const title = w.name() || '';
      const pos = w.position();
      const sz = w.size();
      out.push({ id: appName + '#' + i, app: appName, title: String(title), x: pos[0], y: pos[1], width: sz[0], height: sz[1], focused: focused && i === 0 });
    } catch (e) {}
  }
}
JSON.stringify(out);`
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		return nil, err
	}
	var res []WindowInfo
	if err := json.Unmarshal(bytes.TrimSpace(out), &res); err != nil {
		return nil, err
	}
	return res, nil
}

func focusFallbackWindow(ctx context.Context, app, _ string) error {
	app = strings.TrimSpace(app)
	if app == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		script := `function run(argv) { Application(argv[0]).activate(); }`
		return exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script, app).Run()
	default:
		return nil
	}
}

func injectFallbackInput(ctx context.Context, ev UnifiedInput) error {
	if runtime.GOOS == "darwin" {
		return injectMacOSInput(ctx, ev)
	}
	return errors.New("desktop input requires bot-host on this platform")
}

func injectMacOSInput(ctx context.Context, ev UnifiedInput) error {
	kind := strings.ToLower(strings.TrimSpace(ev.Kind))
	x, y := 0.0, 0.0
	if ev.X != nil {
		x = *ev.X
	}
	if ev.Y != nil {
		y = *ev.Y
	}
	if ev.App != "" && kind != "move" {
		_ = focusFallbackWindow(ctx, ev.App, ev.WindowID)
	}

	switch kind {
	case "move", "down", "up", "click", "double_click", "dblclick", "right_click", "rightclick":
		clicks := 1
		btn := strings.ToLower(strings.TrimSpace(ev.Button))
		mode := kind
		if kind == "right_click" || kind == "rightclick" {
			btn = "right"
			mode = "click"
		} else if kind == "double_click" || kind == "dblclick" {
			clicks = 2
			mode = "click"
		}
		if btn == "" {
			btn = "left"
		}
		script := `ObjC.import('ApplicationServices');
function run(argv) {
  var mode = argv[0], x = parseFloat(argv[1]), y = parseFloat(argv[2]), btn = argv[3], clicks = parseInt(argv[4], 10) || 1;
  var src = $.CGEventSourceCreate($.kCGEventSourceStateHIDSystemState);
  var pt = $.CGPointMake(x, y);
  var cgBtn = btn === 'right' ? $.kCGMouseButtonRight : (btn === 'middle' ? $.kCGMouseButtonCenter : $.kCGMouseButtonLeft);
  var downT = btn === 'right' ? $.kCGEventRightMouseDown : (btn === 'middle' ? $.kCGEventOtherMouseDown : $.kCGEventLeftMouseDown);
  var upT = btn === 'right' ? $.kCGEventRightMouseUp : (btn === 'middle' ? $.kCGEventOtherMouseUp : $.kCGEventLeftMouseUp);
  function post(t, st) {
    var e = $.CGEventCreateMouseEvent(src, t, pt, cgBtn);
    if (st > 1) $.CGEventSetIntegerValueField(e, $.kCGMouseEventClickState, st);
    $.CGEventPost($.kCGHIDEventTap, e);
  }
  if (mode === 'move') { post($.kCGEventMouseMoved, 0); return; }
  if (mode === 'down') { post(downT, 1); return; }
  if (mode === 'up') { post(upT, 1); return; }
  for (var i = 1; i <= clicks; i++) { post(downT, i); post(upT, i); }
}`
		return exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script,
			mode, strconv.FormatFloat(x, 'f', 2, 64), strconv.FormatFloat(y, 'f', 2, 64), btn, strconv.Itoa(clicks)).Run()

	case "wheel", "scroll":
		dx := int(-ev.DX)
		dy := int(-ev.DY)
		if dy == 0 && ev.DeltaY != 0 {
			dy = int(ev.DeltaY * 24)
		}
		script := `ObjC.import('ApplicationServices');
function run(argv) {
  var dy = parseInt(argv[0], 10) || 0, dx = parseInt(argv[1], 10) || 0;
  var e = $.CGEventCreateScrollWheelEvent(null, $.kCGScrollEventUnitPixel, 2, dy, dx);
  $.CGEventPost($.kCGHIDEventTap, e);
}`
		return exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script, strconv.Itoa(dy), strconv.Itoa(dx)).Run()

	case "type", "text":
		text := ev.Text
		if text == "" {
			return nil
		}
		// System Events keystroke handles ASCII/IME cleanly via AppleScript without mutating the clipboard.
		script := `on run argv
  tell application "System Events" to keystroke (item 1 of argv)
end run`
		return exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script, text).Run()

	case "key", "keydown", "keyup":
		if kind == "keyup" {
			return nil
		}
		combo := ev.Key
		if combo == "" {
			combo = ev.Code
		}
		_, keyName, mods := parseKeyCombo(combo)
		if ev.Shift {
			mods.Shift = true
		}
		if ev.Ctrl {
			mods.Ctrl = true
		}
		if ev.Alt {
			mods.Alt = true
		}
		if ev.Meta {
			mods.Meta = true
		}
		code, ok := macOSKeyCode(keyName, ev.Code)
		if !ok {
			if len([]rune(keyName)) == 1 && !mods.any() {
				return injectMacOSInput(ctx, UnifiedInput{Kind: "type", Text: keyName})
			}
			return fmt.Errorf("unsupported key %q", combo)
		}
		script := `ObjC.import('ApplicationServices');
function run(argv) {
  var kc = parseInt(argv[0], 10), flags = parseInt(argv[1], 10) || 0;
  var src = $.CGEventSourceCreate($.kCGEventSourceStateHIDSystemState);
  var d = $.CGEventCreateKeyboardEvent(src, kc, true);
  if (flags) $.CGEventSetFlags(d, flags);
  $.CGEventPost($.kCGHIDEventTap, d);
  var u = $.CGEventCreateKeyboardEvent(src, kc, false);
  if (flags) $.CGEventSetFlags(u, flags);
  $.CGEventPost($.kCGHIDEventTap, u);
}`
		var flags int64
		if mods.Shift {
			flags |= 1 << 17 // kCGEventFlagMaskShift
		}
		if mods.Ctrl {
			flags |= 1 << 18 // kCGEventFlagMaskControl
		}
		if mods.Alt {
			flags |= 1 << 19 // kCGEventFlagMaskAlternate
		}
		if mods.Meta {
			flags |= 1 << 20 // kCGEventFlagMaskCommand
		}
		return exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script, strconv.Itoa(code), strconv.FormatInt(flags, 10)).Run()

	default:
		return fmt.Errorf("unsupported input kind %q", ev.Kind)
	}
}

func macOSKeyCode(keyName, webCode string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(keyName)) {
	case "return", "enter":
		return 0x24, true
	case "tab":
		return 0x30, true
	case " ", "space", "spacebar":
		return 0x31, true
	case "backspace":
		return 0x33, true
	case "escape", "esc":
		return 0x35, true
	case "delete", "forwarddelete":
		return 0x75, true
	case "left", "arrowleft":
		return 0x7B, true
	case "right", "arrowright":
		return 0x7C, true
	case "down", "arrowdown":
		return 0x7D, true
	case "up", "arrowup":
		return 0x7E, true
	case "home":
		return 0x73, true
	case "end":
		return 0x77, true
	case "pageup":
		return 0x74, true
	case "pagedown":
		return 0x79, true
	case "a":
		return 0x00, true
	case "s":
		return 0x01, true
	case "d":
		return 0x02, true
	case "f":
		return 0x03, true
	case "h":
		return 0x04, true
	case "g":
		return 0x05, true
	case "z":
		return 0x06, true
	case "x":
		return 0x07, true
	case "c":
		return 0x08, true
	case "v":
		return 0x09, true
	case "b":
		return 0x0B, true
	case "q":
		return 0x0C, true
	case "w":
		return 0x0D, true
	case "e":
		return 0x0E, true
	case "r":
		return 0x0F, true
	case "y":
		return 0x10, true
	case "t":
		return 0x11, true
	case "l":
		return 0x25, true
	case "j":
		return 0x26, true
	case "k":
		return 0x28, true
	case "n":
		return 0x2D, true
	case "m":
		return 0x2E, true
	case "o":
		return 0x1F, true
	case "u":
		return 0x20, true
	case "i":
		return 0x22, true
	case "p":
		return 0x23, true
	}
	if strings.HasPrefix(webCode, "Key") && len(webCode) == 4 {
		return macOSKeyCode(webCode[3:], "")
	}
	return 0, false
}
