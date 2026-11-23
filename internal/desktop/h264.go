package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// VideoPresetConfig defines target FPS, max dimension, and bitrate for a stream tier.
type VideoPresetConfig struct {
	FPS         int
	MaxDim      int
	BitrateKbps int
}

var videoPresets = map[string]VideoPresetConfig{
	"ultra":  {FPS: 60, MaxDim: 3840, BitrateKbps: 35000},
	"high":   {FPS: 60, MaxDim: 2560, BitrateKbps: 16000},
	"medium": {FPS: 30, MaxDim: 1600, BitrateKbps: 6000},
	"low":    {FPS: 15, MaxDim: 1080, BitrateKbps: 2000},
}

// StreamControl allows live bitrate adaptation and on-demand IDR keyframe insertion.
type StreamControl interface {
	SetBitrate(kbps int)
	ForceKeyframe()
}

// ResolveVideoPreset normalizes a preset name ("ultra", "high", "medium", "low") and returns its config.
func ResolveVideoPreset(name string) (string, VideoPresetConfig) {
	key := strings.ToLower(strings.TrimSpace(name))
	if cfg, ok := videoPresets[key]; ok {
		return key, cfg
	}
	return "high", videoPresets["high"]
}

// StepDownPreset returns the next lower quality tier if available.
func StepDownPreset(cur string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(cur)) {
	case "ultra":
		return "high", true
	case "high", "":
		return "medium", true
	case "medium":
		return "low", true
	default:
		return "low", false
	}
}

// StepUpPreset returns the next higher quality tier if available.
func StepUpPreset(cur string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(cur)) {
	case "low":
		return "medium", true
	case "medium":
		return "high", true
	case "high":
		return "ultra", true
	default:
		return "ultra", false
	}
}

// H264Chunk represents a single Annex-B encoded access unit (keyframe or delta slice).
type H264Chunk struct {
	Data []byte
	Key  bool
}

// H264Meta describes stream geometry and encoding parameters sent before video frames.
type H264Meta struct {
	Width         int     `json:"imageWidth"`
	Height        int     `json:"imageHeight"`
	LogicalX      float64 `json:"logicalX"`
	LogicalY      float64 `json:"logicalY"`
	LogicalWidth  float64 `json:"logicalWidth"`
	LogicalHeight float64 `json:"logicalHeight"`
	FPS           int     `json:"fps"`
	BitrateKbps   int     `json:"bitrateKbps"`
	Preset        string  `json:"preset"`
	Codec         string  `json:"codec"`
	Backend       string  `json:"backend"`
}

// AnnexBSplitter parses a raw H.264 Annex-B byte stream into access-unit chunks,
// prepending cached SPS/PPS to IDR keyframes.
type AnnexBSplitter struct {
	buf       []byte
	sps       []byte
	pps       []byte
	auBuf     []byte
	auIsKey   bool
	auHasSlice bool
}

// Push appends raw Annex-B bytes and invokes emit for each completed access unit.
func (s *AnnexBSplitter) Push(p []byte, emit func(H264Chunk) error) error {
	if len(p) == 0 {
		return nil
	}
	s.buf = append(s.buf, p...)
	offset := 0
	for {
		sc1, sc1Len := findStartCode(s.buf, offset)
		if sc1 < 0 {
			// Preserve up to 4 trailing bytes in case a start code spans chunks.
			if len(s.buf)-offset > 4 {
				offset = len(s.buf) - 4
			}
			break
		}
		sc2, _ := findStartCode(s.buf, sc1+sc1Len)
		if sc2 < 0 {
			offset = sc1
			break
		}
		nal := s.buf[sc1+sc1Len : sc2]
		if err := s.handleNAL(nal, emit); err != nil {
			s.compactBuf(sc2)
			return err
		}
		offset = sc2
	}
	s.compactBuf(offset)
	return nil
}

func (s *AnnexBSplitter) compactBuf(offset int) {
	if offset <= 0 {
		return
	}
	rem := len(s.buf) - offset
	if rem > 0 {
		copy(s.buf, s.buf[offset:])
	}
	s.buf = s.buf[:rem]
}

// Flush emits any trailing NAL / access unit buffered at EOF.
func (s *AnnexBSplitter) Flush(emit func(H264Chunk) error) error {
	if sc1, sc1Len := findStartCode(s.buf, 0); sc1 >= 0 && sc1+sc1Len < len(s.buf) {
		nal := s.buf[sc1+sc1Len:]
		if err := s.handleNAL(nal, emit); err != nil {
			return err
		}
		s.buf = s.buf[:0]
	}
	return s.flushAU(emit)
}

func isFirstSliceOfFrame(nal []byte) bool {
	// In H.264 slice_header(), first_mb_in_slice is unsigned Exp-Golomb coded.
	// first_mb_in_slice == 0 encodes as a leading '1' bit (MSB 0x80) in byte 1.
	return len(nal) <= 1 || (nal[1]&0x80) != 0
}

func (s *AnnexBSplitter) handleNAL(nal []byte, emit func(H264Chunk) error) error {
	if len(nal) == 0 {
		return nil
	}
	nalType := nal[0] & 0x1F
	switch nalType {
	case 7: // SPS
		if s.auHasSlice {
			if err := s.flushAU(emit); err != nil {
				return err
			}
		}
		s.sps = append([]byte(nil), nal...)
	case 8: // PPS
		if s.auHasSlice {
			if err := s.flushAU(emit); err != nil {
				return err
			}
		}
		s.pps = append([]byte(nil), nal...)
	case 9: // AUD: boundary between access units
		if s.auHasSlice {
			return s.flushAU(emit)
		}
	case 5: // IDR slice
		if s.auHasSlice && isFirstSliceOfFrame(nal) {
			if err := s.flushAU(emit); err != nil {
				return err
			}
		}
		if !s.auHasSlice {
			if len(s.sps) > 0 {
				s.appendAnnexBNAL(s.sps)
			}
			if len(s.pps) > 0 {
				s.appendAnnexBNAL(s.pps)
			}
			s.auIsKey = true
		}
		s.appendAnnexBNAL(nal)
		s.auHasSlice = true
	case 1: // Non-IDR slice
		if s.auHasSlice && isFirstSliceOfFrame(nal) {
			if err := s.flushAU(emit); err != nil {
				return err
			}
		}
		if !s.auHasSlice {
			s.auIsKey = false
		}
		s.appendAnnexBNAL(nal)
		s.auHasSlice = true
	default:
		// SEI or other supplemental NALs attached to current AU if any.
		if s.auHasSlice {
			s.appendAnnexBNAL(nal)
		}
	}
	return nil
}

func (s *AnnexBSplitter) appendAnnexBNAL(nal []byte) {
	s.auBuf = append(s.auBuf, 0x00, 0x00, 0x00, 0x01)
	s.auBuf = append(s.auBuf, nal...)
}

func (s *AnnexBSplitter) flushAU(emit func(H264Chunk) error) error {
	if !s.auHasSlice || len(s.auBuf) == 0 {
		s.auBuf = s.auBuf[:0]
		s.auIsKey = false
		s.auHasSlice = false
		return nil
	}
	out := make([]byte, len(s.auBuf))
	copy(out, s.auBuf)
	key := s.auIsKey
	s.auBuf = s.auBuf[:0]
	s.auIsKey = false
	s.auHasSlice = false
	return emit(H264Chunk{Data: out, Key: key})
}

func findStartCode(b []byte, from int) (idx int, length int) {
	for i := from; i+2 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 {
			if b[i+2] == 1 {
				return i, 3
			}
			if i+3 < len(b) && b[i+2] == 0 && b[i+3] == 1 {
				return i, 4
			}
		}
	}
	return -1, 0
}

// StartH264Stream captures the local desktop as an Annex-B H.264 stream until ctx is cancelled.
func (e *Engine) StartH264Stream(
	ctx context.Context,
	preset string,
	onMeta func(H264Meta) error,
	onChunk func(H264Chunk) error,
) error {
	return e.StartVideoStream(ctx, preset, "h264", onMeta, nil, onChunk)
}

// NormalizeVideoCodec normalizes a requested codec name ("hevc" or "h264").
func NormalizeVideoCodec(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hevc", "h265", "h.265":
		return "hevc"
	default:
		return "h264"
	}
}

// StartVideoStream captures the local desktop as an Annex-B H.264 or HEVC stream,
// preferring native ScreenCaptureKit + VideoToolbox on macOS and falling back to ffmpeg.
func (e *Engine) StartVideoStream(
	ctx context.Context,
	preset string,
	codec string,
	onMeta func(H264Meta) error,
	onReady func(StreamControl),
	onChunk func(H264Chunk) error,
) error {
	if err := e.prepareActive(ctx); err != nil {
		return err
	}
	if e.isScreenRecordingDenied(ctx) {
		return ErrScreenRecordingDenied
	}
	e.wakeMacDisplay(false)
	e.acquireStreamLease()
	defer e.releaseStreamLease()

	presetName, cfg := ResolveVideoPreset(preset)
	codecName := NormalizeVideoCodec(codec)
	srcW, srcH, bounds, ok := mainDisplayMetrics(ctx)
	if !ok {
		st, err := e.Status(ctx)
		if err != nil {
			return err
		}
		bounds = displayBounds{
			X:      st.LogicalX,
			Y:      st.LogicalY,
			Width:  st.LogicalWidth,
			Height: st.LogicalHeight,
		}
		if bounds.Width <= 0 || bounds.Height <= 0 {
			bounds = fallbackBounds(ctx)
		}
		srcW = int(bounds.Width)
		srcH = int(bounds.Height)
	}
	w, h := fitDimensions(srcW, srcH, cfg.MaxDim)
	if w < 16 || h < 16 {
		return errors.New("invalid desktop frame dimensions")
	}

	if nativeCaptureAvailable() {
		meta := H264Meta{
			Width:         w,
			Height:        h,
			LogicalX:      bounds.X,
			LogicalY:      bounds.Y,
			LogicalWidth:  bounds.Width,
			LogicalHeight: bounds.Height,
			FPS:           cfg.FPS,
			BitrateKbps:   cfg.BitrateKbps,
			Preset:        presetName,
			Codec:         codecName,
			Backend:       "sck/" + codecName,
		}
		if onMeta != nil {
			if err := onMeta(meta); err != nil {
				return err
			}
		}
		emitted := 0
		err := startNativeVideoStream(ctx, w, h, cfg.FPS, cfg.BitrateKbps, codecName, onReady, func(c H264Chunk) error {
			emitted++
			if onChunk != nil {
				return onChunk(c)
			}
			return nil
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if emitted > 0 || err == nil {
			return err
		}
		// If ScreenCaptureKit reports permission denied or no display, propagate instead of running ffmpeg.
		if strings.Contains(strings.ToLower(err.Error()), "declined") || strings.Contains(strings.ToLower(err.Error()), "permission") {
			return err
		}
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("需要安装 ffmpeg 以启用视频流")
	}

	encoders := h264EncodersForOS()
	if len(encoders) == 0 {
		return fmt.Errorf("video streaming unsupported on %s", runtime.GOOS)
	}

	var lastErr error
	for _, enc := range encoders {
		emitted := 0
		meta := H264Meta{
			Width:         w,
			Height:        h,
			LogicalX:      bounds.X,
			LogicalY:      bounds.Y,
			LogicalWidth:  bounds.Width,
			LogicalHeight: bounds.Height,
			FPS:           cfg.FPS,
			BitrateKbps:   cfg.BitrateKbps,
			Preset:        presetName,
			Codec:         "h264",
			Backend:       "ffmpeg/" + enc,
		}
		if onMeta != nil {
			if err := onMeta(meta); err != nil {
				return err
			}
		}
		args := buildFFmpegH264Args(enc, e.Env(), srcW, srcH, w, h, cfg)
		err := runFFmpegH264(ctx, ffmpegPath, e.Env(), args, func(c H264Chunk) error {
			emitted++
			if onChunk != nil {
				return onChunk(c)
			}
			return nil
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if emitted > 0 || err == nil {
			return err
		}
		lastErr = err
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("ffmpeg failed to emit video frames")
}

func fitDimensions(srcW, srcH, maxDim int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return 0, 0
	}
	w, h := srcW, srcH
	if maxDim > 0 && (w > maxDim || h > maxDim) {
		if w >= h {
			h = (h * maxDim) / w
			w = maxDim
		} else {
			w = (w * maxDim) / h
			h = maxDim
		}
	}
	return evenDim(w), evenDim(h)
}

func evenDim(v int) int {
	if v <= 0 {
		return 0
	}
	if v%2 != 0 {
		return v - 1
	}
	return v
}

func h264EncodersForOS() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"h264_videotoolbox", "libx264"}
	case "linux":
		return []string{"libx264"}
	default:
		return nil
	}
}

func buildFFmpegH264Args(encoder string, env []string, srcW, srcH, w, h int, cfg VideoPresetConfig) []string {
	fps := strconv.Itoa(cfg.FPS)
	gop := strconv.Itoa(cfg.FPS * 2)
	kbps := strconv.Itoa(cfg.BitrateKbps)
	maxKbps := strconv.Itoa((cfg.BitrateKbps * 15) / 10)

	var args []string
	args = append(args, "-loglevel", "error", "-nostdin")
	switch runtime.GOOS {
	case "darwin":
		args = append(args, "-f", "avfoundation", "-framerate", fps, "-capture_cursor", "1", "-i", "Capture screen 0:none")
	case "linux":
		disp := envValue(env, "DISPLAY")
		if disp == "" {
			disp = ":0"
		}
		args = append(args, "-f", "x11grab", "-framerate", fps, "-i", disp)
	}
	if w > 0 && h > 0 && (w != evenDim(srcW) || h != evenDim(srcH)) {
		args = append(args, "-vf", fmt.Sprintf("scale=%d:%d:flags=fast_bilinear", w, h))
	}
	if encoder == "h264_videotoolbox" {
		args = append(args,
			"-c:v", "h264_videotoolbox",
			"-realtime", "1",
			"-prio_speed", "1",
			"-profile:v", "high",
			"-bf", "0",
			"-pix_fmt", "nv12",
		)
	} else {
		args = append(args,
			"-c:v", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-profile:v", "high",
			"-bf", "0",
			"-pix_fmt", "yuv420p",
			"-threads", "0",
			"-x264-params", "repeat-headers=1",
		)
	}
	args = append(args,
		"-g", gop,
		"-b:v", kbps+"k",
		"-maxrate", maxKbps+"k",
		"-bufsize", kbps+"k",
		"-f", "h264",
		"pipe:1",
	)
	return args
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func runFFmpegH264(ctx context.Context, ffmpegPath string, env, args []string, emit func(H264Chunk) error) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return err
	}

	splitter := &AnnexBSplitter{}
	buf := make([]byte, 32*1024)
	var readErr error
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			if perr := splitter.Push(buf[:n], emit); perr != nil {
				readErr = perr
				break
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				readErr = rerr
			}
			break
		}
	}
	if readErr == nil {
		_ = splitter.Flush(emit)
	}

	if cmd.Process != nil && readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-time.After(2 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		waitErr = <-waitDone
	}

	if readErr != nil {
		return readErr
	}
	if waitErr != nil && ctx.Err() == nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg != "" {
			return fmt.Errorf("ffmpeg: %s", msg)
		}
		return waitErr
	}
	return ctx.Err()
}
