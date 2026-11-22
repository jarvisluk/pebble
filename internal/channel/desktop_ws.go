package channel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"bot-hub/internal/desktop"
)

const (
	wsFrameH264Key   byte = 0x02
	wsFrameH264Delta byte = 0x03
)

type wsClientMsg struct {
	Type   string               `json:"type"`
	Preset string               `json:"preset,omitempty"`
	Event  desktop.UnifiedInput `json:"event,omitempty"`
}

func (s *HTTPServer) handleDesktopStreamWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Origin is already verified by s.guard.
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	target := s.resolveDesktopTarget(r, "")
	if target != "local" {
		_ = sendWSError(r.Context(), conn, "", "远端节点暂不支持 H.264 视频流")
		_ = conn.Close(websocket.StatusUnsupportedData, "unsupported target")
		return
	}

	rawPreset := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("preset")))
	autoMode := rawPreset == "" || rawPreset == "auto"
	curPreset, _ := desktop.ResolveVideoPreset(rawPreset)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var (
		mu          sync.Mutex
		streamCancel context.CancelFunc
		writeMu     sync.Mutex
	)
	profileCh := make(chan struct{}, 1)
	triggerRestart := func() {
		mu.Lock()
		c := streamCancel
		mu.Unlock()
		if c != nil {
			c()
		}
		select {
		case profileCh <- struct{}{}:
		default:
		}
	}

	writeText := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		wctx, wcancel := context.WithTimeout(ctx, 3*time.Second)
		defer wcancel()
		return conn.Write(wctx, websocket.MessageText, b)
	}

	// Reader goroutine: handle input events and manual preset switches.
	go func() {
		defer cancel()
		for {
			_, data, rerr := conn.Read(ctx)
			if rerr != nil {
				return
			}
			var msg wsClientMsg
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "input":
				ev := msg.Event
				go func() {
					ictx, icancel := context.WithTimeout(ctx, 4*time.Second)
					defer icancel()
					_ = s.localDesk().SendInput(ictx, ev)
				}()
			case "profile":
				reqP := strings.ToLower(strings.TrimSpace(msg.Preset))
				mu.Lock()
				if reqP == "" || reqP == "auto" {
					autoMode = true
					curPreset = "high"
				} else {
					autoMode = false
					curPreset, _ = desktop.ResolveVideoPreset(reqP)
				}
				mu.Unlock()
				triggerRestart()
			}
		}
	}()

	for ctx.Err() == nil {
		// Drain any pending profile trigger.
		select {
		case <-profileCh:
		default:
		}

		mu.Lock()
		preset := curPreset
		isAuto := autoMode
		sctx, scancel := context.WithCancel(ctx)
		streamCancel = scancel
		mu.Unlock()

		slowWrites := 0
		lastSlowAt := time.Now()
		steppedForCongestion := false

		err := s.localDesk().StartH264Stream(
			sctx,
			preset,
			func(meta desktop.H264Meta) error {
				return writeText(map[string]any{
					"type":          "meta",
					"codec":         "h264",
					"preset":        meta.Preset,
					"auto":          isAuto,
					"fps":           meta.FPS,
					"bitrateKbps":   meta.BitrateKbps,
					"imageWidth":    meta.Width,
					"imageHeight":   meta.Height,
					"logicalX":      meta.LogicalX,
					"logicalY":      meta.LogicalY,
					"logicalWidth":  meta.LogicalWidth,
					"logicalHeight": meta.LogicalHeight,
					"backend":       meta.Backend,
				})
			},
			func(chunk desktop.H264Chunk) error {
				pkt := make([]byte, 1+len(chunk.Data))
				if chunk.Key {
					pkt[0] = wsFrameH264Key
				} else {
					pkt[0] = wsFrameH264Delta
				}
				copy(pkt[1:], chunk.Data)

				t0 := time.Now()
				writeMu.Lock()
				wctx, wcancel := context.WithTimeout(sctx, 3*time.Second)
				werr := conn.Write(wctx, websocket.MessageBinary, pkt)
				wcancel()
				writeMu.Unlock()
				if werr != nil {
					return werr
				}

				if !isAuto {
					return nil
				}
				dur := time.Since(t0)
				if dur > 400*time.Millisecond {
					slowWrites = 4
					lastSlowAt = time.Now()
				} else if dur > 120*time.Millisecond {
					slowWrites++
					lastSlowAt = time.Now()
				} else {
					if dur > 25*time.Millisecond {
						lastSlowAt = time.Now()
					}
					slowWrites = 0
				}

				if slowWrites >= 4 {
					mu.Lock()
					if autoMode {
						if next, ok := desktop.StepDownPreset(curPreset); ok {
							curPreset = next
							steppedForCongestion = true
							mu.Unlock()
							scancel()
							return context.Canceled
						}
					}
					mu.Unlock()
					slowWrites = 0
				} else if preset != "high" && time.Since(lastSlowAt) >= 15*time.Second {
					mu.Lock()
					if autoMode {
						if next, ok := desktop.StepUpPreset(curPreset); ok {
							curPreset = next
							steppedForCongestion = true
							mu.Unlock()
							scancel()
							return context.Canceled
						}
					}
					mu.Unlock()
					lastSlowAt = time.Now()
				}
				return nil
			},
		)
		scancel()

		if ctx.Err() != nil {
			break
		}
		// Restart triggered by profile change or auto-adaptation.
		if steppedForCongestion {
			continue
		}
		select {
		case <-profileCh:
			continue
		default:
		}
		if err != nil {
			code := ""
			if errors.Is(err, desktop.ErrScreenRecordingDenied) {
				code = "screen_recording_denied"
			}
			_ = writeText(map[string]any{
				"type":  "error",
				"code":  code,
				"error": err.Error(),
			})
			_ = conn.Close(websocket.StatusInternalError, "stream failed")
			return
		}
	}
}

func sendWSError(ctx context.Context, conn *websocket.Conn, code, msg string) error {
	b, _ := json.Marshal(map[string]any{
		"type":  "error",
		"code":  code,
		"error": msg,
	})
	wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}
