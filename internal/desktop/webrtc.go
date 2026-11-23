package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type nativeWebRTCSession struct {
	id     string
	pc     *webrtc.PeerConnection
	cancel context.CancelFunc
	mu     sync.Mutex
	ctrl   StreamControl
	closed bool
}

func (s *nativeWebRTCSession) setControl(c StreamControl) {
	s.mu.Lock()
	s.ctrl = c
	s.mu.Unlock()
}

func (s *nativeWebRTCSession) getControl() StreamControl {
	s.mu.Lock()
	c := s.ctrl
	s.mu.Unlock()
	return c
}

func (s *nativeWebRTCSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cancel := s.cancel
	pc := s.pc
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if pc != nil {
		_ = pc.Close()
	}
}

func newWebRTCSessionID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "rtc_" + hex.EncodeToString(b[:])
}

func (e *Engine) startPionWebRTC(
	ctx context.Context,
	sdpOffer string,
	fps, maxDim, bitrate int,
) (*StreamOfferResult, error) {
	if err := e.prepareActive(ctx); err != nil {
		return nil, err
	}
	if e.isScreenRecordingDenied(ctx) {
		return nil, ErrScreenRecordingDenied
	}
	e.wakeMacDisplay(false)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"bot-hub-desktop",
	)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}

	rtpSender, err := pc.AddTrack(track)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}

	sctx, scancel := context.WithCancel(context.Background())
	id := newWebRTCSessionID()
	sess := &nativeWebRTCSession{
		id:     id,
		pc:     pc,
		cancel: scancel,
	}

	e.webrtcMu.Lock()
	if e.webrtcSessions == nil {
		e.webrtcSessions = make(map[string]*nativeWebRTCSession)
	}
	e.webrtcSessions[id] = sess
	e.webrtcMu.Unlock()

	cleanup := func() {
		e.webrtcMu.Lock()
		delete(e.webrtcSessions, id)
		e.webrtcMu.Unlock()
		sess.close()
	}

	// Adaptive bitrate state for WebRTC (2 Mbps ~ 35 Mbps, starting at 20 Mbps).
	var abrMu sync.Mutex
	curKbps := 20000
	if bitrate > 0 {
		curKbps = min(35000, max(2000, bitrate/1000))
	}
	const minKbps = 2000
	const maxKbps = 35000
	lastDownAt := time.Time{}
	lastUpAt := time.Now()
	pliCount := 0
	pliWindowAt := time.Now()

	stepDown := func() {
		abrMu.Lock()
		now := time.Now()
		if !lastDownAt.IsZero() && now.Sub(lastDownAt) < 2*time.Second {
			abrMu.Unlock()
			return
		}
		lastDownAt = now
		lastUpAt = now
		next := max(minKbps, (curKbps*7)/10)
		changed := next != curKbps
		curKbps = next
		abrMu.Unlock()
		if changed {
			if ctrl := sess.getControl(); ctrl != nil {
				ctrl.SetBitrate(next)
			}
		}
	}

	stepUp := func() {
		abrMu.Lock()
		now := time.Now()
		if now.Sub(lastUpAt) < 6*time.Second {
			abrMu.Unlock()
			return
		}
		lastUpAt = now
		next := min(maxKbps, (curKbps*5)/4)
		changed := next != curKbps
		curKbps = next
		abrMu.Unlock()
		if changed {
			if ctrl := sess.getControl(); ctrl != nil {
				ctrl.SetBitrate(next)
			}
		}
	}

	// Read RTCP feedback (PLI/FIR/ReceiverReport) for immediate IDR and loss-based ABR.
	go func() {
		buf := make([]byte, 1500)
		for {
			n, _, rerr := rtpSender.Read(buf)
			if rerr != nil {
				return
			}
			pkts, perr := rtcp.Unmarshal(buf[:n])
			if perr != nil {
				continue
			}
			for _, pkt := range pkts {
				switch p := pkt.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					if ctrl := sess.getControl(); ctrl != nil {
						ctrl.ForceKeyframe()
					}
					abrMu.Lock()
					now := time.Now()
					if now.Sub(pliWindowAt) > 3*time.Second {
						pliWindowAt = now
						pliCount = 1
					} else {
						pliCount++
					}
					burst := pliCount >= 2
					abrMu.Unlock()
					if burst {
						stepDown()
					}
				case *rtcp.ReceiverReport:
					for _, r := range p.Reports {
						if r.FractionLost > 5 { // > ~2% packet loss
							stepDown()
							break
						}
					}
				}
			}
		}
	}()

	// Handle DataChannel input, client RTT/loss telemetry, and keyframe requests.
	inputCh := make(chan UnifiedInput, 128)
	go func() {
		for {
			select {
			case <-sctx.Done():
				return
			case ev := <-inputCh:
				ictx, icancel := context.WithTimeout(sctx, 4*time.Second)
				_ = e.SendInput(ictx, ev)
				icancel()
			}
		}
	}()

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if msg.IsString {
				var raw struct {
					Type    string   `json:"type"`
					RTTMs   float64  `json:"rttMs"`
					LossPct float64  `json:"lossPct"`
					X       *float64 `json:"x"`
					Y       *float64 `json:"y"`
					Button  string   `json:"button"`
					DX      float64  `json:"dx"`
					DY      float64  `json:"dy"`
					DeltaY  float64  `json:"deltaY"`
					Key     string   `json:"key"`
					Code    string   `json:"code"`
					Shift   bool     `json:"shift"`
					Ctrl    bool     `json:"ctrl"`
					Alt     bool     `json:"alt"`
					Meta    bool     `json:"meta"`
					Text    string   `json:"text"`
				}
				if json.Unmarshal(msg.Data, &raw) != nil {
					return
				}
				switch strings.ToLower(raw.Type) {
				case "keyframe":
					if ctrl := sess.getControl(); ctrl != nil {
						ctrl.ForceKeyframe()
					}
					return
				case "stats":
					if raw.LossPct > 1.5 || raw.RTTMs > 120 {
						stepDown()
					} else if raw.LossPct <= 0.2 && raw.RTTMs > 0 && raw.RTTMs < 70 {
						stepUp()
					} else {
						abrMu.Lock()
						lastUpAt = time.Now()
						abrMu.Unlock()
					}
					return
				}
				ev := UnifiedInput{
					Kind:   raw.Type,
					X:      raw.X,
					Y:      raw.Y,
					Button: raw.Button,
					DX:     raw.DX,
					DY:     raw.DY,
					DeltaY: raw.DeltaY,
					Key:    raw.Key,
					Code:   raw.Code,
					Shift:  raw.Shift,
					Ctrl:   raw.Ctrl,
					Alt:    raw.Alt,
					Meta:   raw.Meta,
					Text:   raw.Text,
				}
				select {
				case inputCh <- ev:
				default:
					if !strings.EqualFold(ev.Kind, "move") {
						select {
						case inputCh <- ev:
						case <-sctx.Done():
						}
					}
				}
			}
		})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			cleanup()
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdpOffer,
	}); err != nil {
		cleanup()
		return nil, err
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		cleanup()
		return nil, err
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		cleanup()
		return nil, err
	}
	select {
	case <-gatherComplete:
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	}

	preset := presetFromParams(fps, maxDim, bitrate)
	frameDur := time.Second / time.Duration(max(1, fps))
	if frameDur <= 0 {
		frameDur = time.Second / 60
	}

	backendName := "pion/ffmpeg"
	if nativeCaptureAvailable() {
		backendName = "pion/sck"
	}

	go func() {
		defer cleanup()
		_ = e.StartVideoStream(
			sctx,
			preset,
			"h264",
			func(m H264Meta) error {
				if m.FPS > 0 {
					frameDur = time.Second / time.Duration(m.FPS)
				}
				return nil
			},
			func(ctrl StreamControl) {
				sess.setControl(ctrl)
				if bitrate > 0 {
					ctrl.SetBitrate(max(500, bitrate/1000))
				}
			},
			func(chunk H264Chunk) error {
				if sctx.Err() != nil {
					return sctx.Err()
				}
				return track.WriteSample(media.Sample{
					Data:     chunk.Data,
					Duration: frameDur,
				})
			},
		)
	}()

	localDesc := pc.LocalDescription()
	if localDesc == nil {
		cleanup()
		return nil, errors.New("webrtc local description unavailable")
	}
	return &StreamOfferResult{
		SDP:     localDesc.SDP,
		Session: id,
		Codec:   "h264",
		Backend: backendName,
	}, nil
}

func presetFromParams(fps, maxDim, bitrate int) string {
	switch {
	case maxDim >= 3000 || bitrate >= 25_000_000:
		return "ultra"
	case maxDim >= 2000 || fps >= 45 || bitrate >= 12_000_000:
		return "high"
	case maxDim >= 1200 || fps >= 24:
		return "medium"
	default:
		return "low"
	}
}

func (e *Engine) closePionSession(session string) {
	e.webrtcMu.Lock()
	if session == "" {
		all := make([]*nativeWebRTCSession, 0, len(e.webrtcSessions))
		for k, s := range e.webrtcSessions {
			all = append(all, s)
			delete(e.webrtcSessions, k)
		}
		e.webrtcMu.Unlock()
		for _, s := range all {
			s.close()
		}
		return
	}
	s := e.webrtcSessions[session]
	delete(e.webrtcSessions, session)
	e.webrtcMu.Unlock()
	if s != nil {
		s.close()
	}
}
