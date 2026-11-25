//go:build linux

package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func hasAgentDesktop() bool {
	_, err := exec.LookPath("agent-desktop")
	return err == nil
}

func runAgentDesktop(ctx context.Context, extraEnv []string, args ...string) ([]byte, error) {
	bin, err := exec.LookPath("agent-desktop")
	if err != nil {
		return nil, errors.New("agent-desktop CLI is not installed; install or configure bot-host first")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if len(extraEnv) > 0 {
		cmd.Env = mergeEnv(os.Environ(), extraEnv)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return out, errors.New(msg)
	}
	return out, nil
}

func captureLinuxFallbackFrame(ctx context.Context, extraEnv []string, windowID string, maxDim, quality int) (*FrameResult, error) {
	if maxDim <= 0 {
		maxDim = 1440
	}
	if quality <= 0 || quality > 100 {
		quality = 75
	}
	tmpDir, err := os.MkdirTemp("", "bothub-linux-frame-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	pngPath := filepath.Join(tmpDir, "frame.png")
	args := []string{"screenshot"}
	if wid := strings.TrimSpace(windowID); wid != "" {
		args = append(args, "--window-id", wid)
	} else {
		args = append(args, "--screen", "0")
	}
	args = append(args, pngPath)

	if _, err := runAgentDesktop(ctx, extraEnv, args...); err != nil {
		return nil, fmt.Errorf("agent-desktop screenshot failed: %w", err)
	}

	raw, err := os.ReadFile(pngPath)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot image: %w", err)
	}

	origBounds := img.Bounds()
	origW, origH := origBounds.Dx(), origBounds.Dy()
	scaled := scaleImageToMaxDim(img, maxDim)
	sb := scaled.Bounds()

	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, scaled, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}

	return &FrameResult{
		JPEGBase64:    base64.StdEncoding.EncodeToString(jpegBuf.Bytes()),
		ImageWidth:    sb.Dx(),
		ImageHeight:   sb.Dy(),
		LogicalX:      0,
		LogicalY:      0,
		LogicalWidth:  float64(origW),
		LogicalHeight: float64(origH),
		Backend:       "agent-desktop",
		Timestamp:     time.Now().UnixMilli(),
	}, nil
}

func scaleImageToMaxDim(src image.Image, maxDim int) image.Image {
	if maxDim <= 0 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim || w <= 0 || h <= 0 {
		return src
	}
	var newW, newH int
	if w >= h {
		newW = maxDim
		newH = int(math.Round(float64(h) * float64(maxDim) / float64(w)))
	} else {
		newH = maxDim
		newW = int(math.Round(float64(w) * float64(maxDim) / float64(h)))
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		sy := b.Min.Y + y*h/newH
		for x := 0; x < newW; x++ {
			sx := b.Min.X + x*w/newW
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func listLinuxFallbackWindows(ctx context.Context, extraEnv []string) ([]WindowInfo, error) {
	out, err := runAgentDesktop(ctx, extraEnv, "list-windows")
	if err != nil {
		return nil, err
	}
	return parseAgentDesktopWindows(out)
}

func focusLinuxFallbackWindow(ctx context.Context, extraEnv []string, app, windowID string) error {
	wid := strings.TrimSpace(windowID)
	app = strings.TrimSpace(app)
	if wid == "" && app == "" {
		return nil
	}
	args := []string{"focus-window"}
	if wid != "" {
		args = append(args, "--window-id", wid)
	} else {
		args = append(args, "--app", app)
	}
	_, err := runAgentDesktop(ctx, extraEnv, args...)
	return err
}

func injectLinuxFallbackInput(ctx context.Context, extraEnv []string, ev UnifiedInput) error {
	kind := strings.ToLower(strings.TrimSpace(ev.Kind))
	x, y := 0, 0
	if ev.X != nil {
		x = int(math.Round(*ev.X))
	}
	if ev.Y != nil {
		y = int(math.Round(*ev.Y))
	}
	xyArg := fmt.Sprintf("--xy=%d,%d", x, y)

	switch kind {
	case "move":
		_, err := runAgentDesktop(ctx, extraEnv, "--headed", "mouse-move", xyArg)
		return err
	case "click", "down":
		btn := strings.ToLower(strings.TrimSpace(ev.Button))
		if btn == "" {
			btn = "left"
		}
		_, err := runAgentDesktop(ctx, extraEnv, "--headed", "mouse-click", xyArg, "--button", btn, "--count", "1")
		return err
	case "double_click":
		_, err := runAgentDesktop(ctx, extraEnv, "--headed", "mouse-click", xyArg, "--button", "left", "--count", "2")
		return err
	case "right_click":
		_, err := runAgentDesktop(ctx, extraEnv, "--headed", "mouse-click", xyArg, "--button", "right", "--count", "1")
		return err
	case "up":
		return nil
	case "wheel", "scroll":
		dy := ev.DY
		if dy == 0 {
			dy = ev.DeltaY
		}
		dxLines := int(math.Round(ev.DX / 40))
		dyLines := int(math.Round(-dy / 40))
		if dxLines == 0 && dyLines == 0 {
			if dy > 0 {
				dyLines = -3
			} else if dy < 0 {
				dyLines = 3
			}
		}
		_, err := runAgentDesktop(ctx, extraEnv,
			"--headed", "mouse-wheel",
			"--x="+strconv.Itoa(x),
			"--y="+strconv.Itoa(y),
			"--dx="+strconv.Itoa(dxLines),
			"--dy="+strconv.Itoa(dyLines),
		)
		return err
	case "type", "text":
		txt := ev.Text
		if txt == "" {
			return nil
		}
		if _, err := runAgentDesktop(ctx, extraEnv, "clipboard-set", txt); err != nil {
			return err
		}
		_, err := runAgentDesktop(ctx, extraEnv, "--headed", "press", "ctrl+v")
		return err
	case "key", "keydown":
		combo := buildLinuxKeyCombo(ev)
		if combo == "" {
			return nil
		}
		_, err := runAgentDesktop(ctx, extraEnv, "--headed", "press", combo)
		return err
	case "keyup":
		return nil
	default:
		return fmt.Errorf("unsupported input event kind %q", ev.Kind)
	}
}

func buildLinuxKeyCombo(ev UnifiedInput) string {
	var parts []string
	if ev.Ctrl {
		parts = append(parts, "ctrl")
	}
	if ev.Alt {
		parts = append(parts, "alt")
	}
	if ev.Shift {
		parts = append(parts, "shift")
	}
	if ev.Meta {
		parts = append(parts, "super")
	}
	key := strings.TrimSpace(ev.Key)
	if key == "" {
		key = strings.TrimSpace(ev.Code)
	}
	if key == "" {
		return ""
	}
	if strings.Contains(key, "+") {
		return strings.ToLower(key)
	}
	switch strings.ToLower(key) {
	case "enter":
		key = "return"
	case " ":
		key = "space"
	case "arrowup":
		key = "up"
	case "arrowdown":
		key = "down"
	case "arrowleft":
		key = "left"
	case "arrowright":
		key = "right"
	}
	if len(parts) > 0 {
		return strings.Join(append(parts, strings.ToLower(key)), "+")
	}
	return strings.ToLower(key)
}
