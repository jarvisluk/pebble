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
	wsFrameHEVCKey   byte = 0x04
	wsFrameHEVCDelta byte = 0x05
)

type wsClientMsg struct {
	Type        string               `json:"type"`
	Preset      string               `json:"preset,omitempty"`
	BitrateKbps int                  `json:"kbps,omitempty"`
	Event       desktop.UnifiedInput `json:"event,omitempty"`
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

	curPreset := "high"
	reqCodec := desktop.NormalizeVideoCodec(r.URL.Query().Get("codec"))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var (
		mu         sync.Mutex
		streamCtrl desktop.StreamControl
		writeMu    sync.Mutex
	)
	congestionCh := make(chan struct{}, 4)

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

	// Ordered input worker: serialize mouse/keyboard events so keydown/keyup and drags stay in order.
	inputCh := make(chan desktop.UnifiedInput, 128)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-inputCh:
				ictx, icancel := context.WithTimeout(ctx, 4*time.Second)
				_ = s.localDesk().SendInput(ictx, ev)
				icancel()
			}
		}
	}()

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
				select {
				case inputCh <- ev:
				default:
					// Drop ephemeral mouse moves if input worker is saturated.
					if !strings.EqualFold(ev.Kind, "move") {
						select {
						case inputCh <- ev:
						case <-ctx.Done():
							return
						}
					}
				}
			case "keyframe":
				mu.Lock()
				ctrl := streamCtrl
				mu.Unlock()
				if ctrl != nil {
					ctrl.ForceKeyframe()
				}
			case "congestion":
				select {
				case congestionCh <- struct{}{}:
				default:
				}
			case "bitrate":
				if msg.BitrateKbps > 0 {
					mu.Lock()
					ctrl := streamCtrl
					mu.Unlock()
					if ctrl != nil {
						ctrl.SetBitrate(msg.BitrateKbps)
					}
				}
			}
		}
	}()

	var pktBuf []byte

	for ctx.Err() == nil {
		// Drain any pending congestion triggers.
		for {
			select {
			case <-congestionCh:
			default:
				goto drained
			}
		}
	drained:

		mu.Lock()
		preset := curPreset
		sctx, scancel := context.WithCancel(ctx)
		streamCtrl = nil
		mu.Unlock()

		_, baseCfg := desktop.ResolveVideoPreset(preset)
		curBitrateKbps := baseCfg.BitrateKbps
		minBitrateKbps := max(500, baseCfg.BitrateKbps/3)
		activeCodec := reqCodec

		frameCh := make(chan desktop.H264Chunk, 2)
		writeErrCh := make(chan error, 1)
		steppedForCongestion := false

		// Non-blocking writer goroutine: drains frameCh, adapts bitrate smoothly, and avoids blocking encoder.
		writerDone := make(chan struct{})
		go func() {
			defer close(writerDone)
			slowWrites := 0
			lastCongestAt := time.Time{}
			lastSmoothAt := time.Now()
			notifyBitrate := func(kbps int) {
				_ = writeText(map[string]any{
					"type":        "bitrate",
					"preset":      preset,
					"bitrateKbps": kbps,
				})
			}
			applyCongestion := func() bool {
				now := time.Now()
				if !lastCongestAt.IsZero() && now.Sub(lastCongestAt) < 1200*time.Millisecond {
					return false
				}
				lastCongestAt = now
				lastSmoothAt = now
				mu.Lock()
				ctrl := streamCtrl
				mu.Unlock()
				if ctrl != nil && curBitrateKbps > minBitrateKbps {
					curBitrateKbps = max(minBitrateKbps, (curBitrateKbps*7)/10)
					ctrl.SetBitrate(curBitrateKbps)
					notifyBitrate(curBitrateKbps)
					return false
				}
				mu.Lock()
				if next, ok := desktop.StepDownPreset(curPreset); ok {
					curPreset = next
					steppedForCongestion = true
					mu.Unlock()
					scancel()
					return true
				}
				mu.Unlock()
				return false
			}

			for {
				select {
				case <-sctx.Done():
					return
				case <-congestionCh:
					if applyCongestion() {
						return
					}
				case chunk, ok := <-frameCh:
					if !ok {
						return
					}
					select {
					case <-congestionCh:
						if applyCongestion() {
							return
						}
					default:
					}

					needLen := 1 + len(chunk.Data)
					if cap(pktBuf) < needLen {
						pktBuf = make([]byte, needLen)
					} else {
						pktBuf = pktBuf[:needLen]
					}
					if activeCodec == "hevc" {
						if chunk.Key {
							pktBuf[0] = wsFrameHEVCKey
						} else {
							pktBuf[0] = wsFrameHEVCDelta
						}
					} else {
						if chunk.Key {
							pktBuf[0] = wsFrameH264Key
						} else {
							pktBuf[0] = wsFrameH264Delta
						}
					}
					copy(pktBuf[1:], chunk.Data)

					t0 := time.Now()
					writeMu.Lock()
					wctx, wcancel := context.WithTimeout(sctx, 3*time.Second)
					werr := conn.Write(wctx, websocket.MessageBinary, pktBuf)
					wcancel()
					writeMu.Unlock()
					if werr != nil {
						select {
						case writeErrCh <- werr:
						default:
						}
						scancel()
						return
					}

					dur := time.Since(t0)
					if dur > 200*time.Millisecond {
						slowWrites = 3
						lastSmoothAt = time.Now()
					} else if dur > 60*time.Millisecond {
						slowWrites++
						lastSmoothAt = time.Now()
					} else {
						if dur > 20*time.Millisecond {
							lastSmoothAt = time.Now()
						}
						slowWrites = 0
					}

					if slowWrites >= 3 {
						slowWrites = 0
						if applyCongestion() {
							return
						}
					} else if time.Since(lastSmoothAt) >= 5*time.Second {
						mu.Lock()
						ctrl := streamCtrl
						mu.Unlock()
						if ctrl != nil && curBitrateKbps < baseCfg.BitrateKbps {
							curBitrateKbps = min(baseCfg.BitrateKbps, (curBitrateKbps*13)/10)
							ctrl.SetBitrate(curBitrateKbps)
							notifyBitrate(curBitrateKbps)
							lastSmoothAt = time.Now()
						} else if preset != "ultra" && time.Since(lastSmoothAt) >= 8*time.Second {
							mu.Lock()
							if next, ok := desktop.StepUpPreset(curPreset); ok {
								curPreset = next
								steppedForCongestion = true
								mu.Unlock()
								scancel()
								return
							}
							mu.Unlock()
							lastSmoothAt = time.Now()
						}
					}
				}
			}
		}()

		err := s.localDesk().StartVideoStream(
			sctx,
			preset,
			reqCodec,
			func(meta desktop.H264Meta) error {
				if meta.Codec != "" {
					activeCodec = meta.Codec
				}
				return writeText(map[string]any{
					"type":          "meta",
					"codec":         activeCodec,
					"preset":        meta.Preset,
					"auto":          true,
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
			func(ctrl desktop.StreamControl) {
				mu.Lock()
				streamCtrl = ctrl
				mu.Unlock()
			},
			func(chunk desktop.H264Chunk) error {
				if sctx.Err() != nil {
					return sctx.Err()
				}
				select {
				case frameCh <- chunk:
					return nil
				default:
					// Drop stale queued non-key frames so real-time latency never builds up.
					dropped := false
					for {
						select {
						case old := <-frameCh:
							if !old.Key {
								dropped = true
							} else {
								if !chunk.Key {
									select {
									case frameCh <- old:
									default:
									}
									return nil
								}
							}
						default:
							goto enqueued
						}
					}
				enqueued:
					if dropped {
						select {
						case congestionCh <- struct{}{}:
						default:
						}
						if !chunk.Key {
							mu.Lock()
							ctrl := streamCtrl
							mu.Unlock()
							if ctrl != nil {
								ctrl.ForceKeyframe()
							}
						}
					}
					select {
					case frameCh <- chunk:
					default:
					}
					return nil
				}
			},
		)
		scancel()
		<-writerDone
		select {
		case werr := <-writeErrCh:
			if err == nil || errors.Is(err, context.Canceled) {
				err = werr
			}
		default:
		}

		if ctx.Err() != nil {
			break
		}
		// Restart triggered by auto-adaptation.
		if steppedForCongestion {
			continue
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
