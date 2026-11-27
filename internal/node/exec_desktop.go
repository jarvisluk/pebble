package node

import (
	"bot-hub/internal/desktop"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func (a *Agent) execDesktop(ctx context.Context, action string, raw json.RawMessage) Result {
	var slotArgs struct {
		Slot string `json:"slot"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &slotArgs)
	}
	eng, err := a.desktopPool().ForSlot(slotArgs.Slot)
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	jsonOut := func(v any, err error) Result {
		if err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		b, merr := json.Marshal(v)
		if merr != nil {
			return Result{OK: false, Error: merr.Error()}
		}
		return Result{OK: true, Output: string(b)}
	}

	switch action {
	case "desktop_status":
		st, err := eng.Status(ctx)
		return jsonOut(st, err)
	case "desktop_permissions":
		res, err := eng.Permissions(ctx)
		return jsonOut(res, err)
	case "desktop_permission_request":
		var args struct {
			Permission   string `json:"permission"`
			OpenSettings bool   `json:"openSettings"`
		}
		if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Permission) == "" {
			return Result{OK: false, Error: "permission is required"}
		}
		res, err := eng.RequestPermission(ctx, args.Permission, args.OpenSettings)
		return jsonOut(res, err)
	case "desktop_windows":
		wins, err := eng.Windows(ctx)
		if wins == nil {
			wins = []desktop.WindowInfo{}
		}
		return jsonOut(map[string]any{"windows": wins}, err)
	case "desktop_focus":
		var args struct {
			App      string `json:"app"`
			WindowID string `json:"windowId"`
		}
		_ = json.Unmarshal(raw, &args)
		if err := eng.Focus(ctx, args.App, args.WindowID); err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		return Result{OK: true, Output: "focused"}
	case "desktop_frame":
		var args struct {
			WindowID string `json:"windowId"`
			MaxDim   int    `json:"maxDim"`
			Quality  int    `json:"quality"`
		}
		_ = json.Unmarshal(raw, &args)
		fr, err := eng.Frame(ctx, args.WindowID, args.MaxDim, args.Quality)
		return jsonOut(fr, err)
	case "desktop_input":
		var ev desktop.UnifiedInput
		if err := json.Unmarshal(raw, &ev); err != nil {
			return Result{OK: false, Error: "invalid input payload"}
		}
		if err := eng.SendInput(ctx, ev); err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		return Result{OK: true, Output: `{"ok":true}`}
	case "desktop_stream_offer":
		var args struct {
			SDP     string `json:"sdp"`
			Target  string `json:"target"`
			FPS     int    `json:"fps"`
			MaxDim  int    `json:"maxDim"`
			Bitrate int    `json:"bitrate"`
		}
		if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.SDP) == "" {
			return Result{OK: false, Error: "sdp is required"}
		}
		res, err := eng.StreamOffer(ctx, args.SDP, args.Target, args.FPS, args.MaxDim, args.Bitrate)
		return jsonOut(res, err)
	case "desktop_stream_close":
		var args struct {
			Session string `json:"session"`
		}
		_ = json.Unmarshal(raw, &args)
		if err := eng.StreamClose(ctx, args.Session); err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		return Result{OK: true, Output: `{"ok":true}`}
	case "desktop_stream_set_input":
		var args struct {
			Enabled bool `json:"enabled"`
		}
		_ = json.Unmarshal(raw, &args)
		if err := eng.SetStreamInput(ctx, args.Enabled); err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		return Result{OK: true, Output: `{"ok":true}`}
	case "desktop_setup":
		res, err := eng.Setup(ctx)
		return jsonOut(res, err)
	case "desktop_power":
		var args struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(raw, &args)
		st, err := eng.Power(ctx, args.Action)
		return jsonOut(st, err)
	default:
		return Result{OK: false, Error: "unknown desktop action"}
	}
}

func (a *Agent) takeScreenshot(ctx context.Context, raw json.RawMessage) Result {
	var args struct {
		MaxWidth int `json:"maxWidth"`
	}
	_ = json.Unmarshal(raw, &args)
	maxWidth := args.MaxWidth
	if maxWidth <= 0 {
		maxWidth = 1920
	}

	tmpDir, err := os.MkdirTemp("", "pebble-shot-*")
	if err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	defer os.RemoveAll(tmpDir)

	switch runtime.GOOS {
	case "darwin":
		pngPath := filepath.Join(tmpDir, "screen.png")
		capCmd := exec.CommandContext(ctx, "/usr/sbin/screencapture", "-x", "-t", "png", pngPath)
		if out, err := capCmd.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return Result{OK: false, Error: fmt.Sprintf("screencapture failed (check System Settings -> Privacy & Security -> Screen Recording for Pebble): %s", msg)}
		}

		jpgPath := filepath.Join(tmpDir, "screen.jpg")
		sipsCmd := exec.CommandContext(ctx, "/usr/bin/sips", "-Z", strconv.Itoa(maxWidth), "-s", "format", "jpeg", "-s", "formatOptions", "80", pngPath, "--out", jpgPath)
		if err := sipsCmd.Run(); err == nil {
			if data, rerr := os.ReadFile(jpgPath); rerr == nil && len(data) > 0 && len(data) <= maxScreenshotBytes {
				return Result{
					OK:         true,
					MimeType:   "image/jpeg",
					DataBase64: base64.StdEncoding.EncodeToString(data),
					Output:     fmt.Sprintf("captured %d bytes (image/jpeg)", len(data)),
				}
			}
		}

		data, err := os.ReadFile(pngPath)
		if err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		if len(data) > maxScreenshotBytes {
			return Result{OK: false, Error: fmt.Sprintf("screenshot too large (%d bytes)", len(data))}
		}
		return Result{
			OK:         true,
			MimeType:   "image/png",
			DataBase64: base64.StdEncoding.EncodeToString(data),
			Output:     fmt.Sprintf("captured %d bytes (image/png)", len(data)),
		}

	case "windows":
		jpgPath := filepath.Join(tmpDir, "screen.jpg")
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
`
		c := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
		c.Env = append(os.Environ(),
			"BOTHUB_SHOT_PATH="+jpgPath,
			"BOTHUB_SHOT_MAXW="+strconv.Itoa(maxWidth),
		)
		if out, err := c.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return Result{OK: false, Error: fmt.Sprintf("windows screenshot failed: %s", msg)}
		}
		data, err := os.ReadFile(jpgPath)
		if err != nil {
			return Result{OK: false, Error: err.Error()}
		}
		if len(data) > maxScreenshotBytes {
			return Result{OK: false, Error: fmt.Sprintf("screenshot too large (%d bytes)", len(data))}
		}
		return Result{
			OK:         true,
			MimeType:   "image/jpeg",
			DataBase64: base64.StdEncoding.EncodeToString(data),
			Output:     fmt.Sprintf("captured %d bytes (image/jpeg)", len(data)),
		}

	case "linux":
		fr, err := a.desktopEngine().Frame(ctx, "", maxWidth, 80)
		if err != nil {
			return Result{OK: false, Error: fmt.Sprintf("linux screenshot failed: %v", err)}
		}
		return Result{
			OK:         true,
			MimeType:   "image/jpeg",
			DataBase64: fr.JPEGBase64,
			Output:     fmt.Sprintf("captured %dx%d (image/jpeg)", fr.ImageWidth, fr.ImageHeight),
		}

	default:
		return Result{OK: false, Error: "screenshot is supported on macOS, Windows, and Linux nodes"}
	}
}

func (a *Agent) runOSAScript(ctx context.Context, raw json.RawMessage) Result {
	var args struct {
		Script   string `json:"script"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Script) == "" {
		return Result{OK: false, Error: "script is required"}
	}
	out, err := RunOSAScript(ctx, args.Script, args.Language)
	if err != nil {
		return Result{OK: false, ExitCode: 1, Error: err.Error()}
	}
	return Result{OK: true, Output: out}
}

// RunOSAScript runs an AppleScript or JXA snippet on macOS inside the current GUI session.
func RunOSAScript(ctx context.Context, script, language string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("osascript is only supported on macOS")
	}
	var cmdArgs []string
	if strings.EqualFold(language, "JavaScript") || strings.EqualFold(language, "jxa") {
		cmdArgs = append(cmdArgs, "-l", "JavaScript")
	}
	cmdArgs = append(cmdArgs, "-")
	c := exec.CommandContext(ctx, "/usr/bin/osascript", cmdArgs...)
	c.Stdin = strings.NewReader(script)
	buf := &limitedBuffer{limit: maxExecOutputBytes}
	c.Stdout = buf
	c.Stderr = buf
	if err := c.Run(); err != nil {
		out := strings.TrimSpace(buf.String())
		if out == "" {
			out = err.Error()
		}
		return "", errors.New(out)
	}
	return strings.TrimRight(buf.String(), "\r\n"), nil
}

func (a *Agent) openTarget(ctx context.Context, raw json.RawMessage) Result {
	var args struct {
		Target string `json:"target"`
		App    string `json:"app"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{OK: false, Error: "invalid open args"}
	}
	if err := OpenTarget(ctx, args.Target, args.App, a.homeDir); err != nil {
		return Result{OK: false, Error: err.Error()}
	}
	return Result{OK: true, Output: "opened"}
}

// OpenTarget opens a file, URL, or application inside the current GUI session.
func OpenTarget(ctx context.Context, target, app, homeDir string) error {
	target = strings.TrimSpace(target)
	app = strings.TrimSpace(app)
	if target == "" && app == "" {
		return errors.New("target or app is required")
	}
	if target != "" && !strings.Contains(target, "://") {
		target = expandHome(target, homeDir)
	}

	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		var cmdArgs []string
		if app != "" {
			cmdArgs = append(cmdArgs, "-a", app)
		}
		if target != "" {
			cmdArgs = append(cmdArgs, target)
		}
		c = exec.CommandContext(ctx, "/usr/bin/open", cmdArgs...)
	case "windows":
		cmdArgs := []string{"/c", "start", ""}
		if app != "" {
			cmdArgs = append(cmdArgs, app)
		}
		if target != "" {
			cmdArgs = append(cmdArgs, target)
		}
		c = exec.CommandContext(ctx, "cmd", cmdArgs...)
	case "linux":
		if target != "" {
			c = exec.CommandContext(ctx, "xdg-open", target)
		} else {
			c = exec.CommandContext(ctx, app)
		}
	default:
		return errors.New("open is supported on macOS, Windows, and Linux")
	}

	if out, err := c.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}
