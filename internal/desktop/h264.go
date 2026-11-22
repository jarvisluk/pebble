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
	"high":   {FPS: 30, MaxDim: 1600, BitrateKbps: 2500},
	"medium": {FPS: 15, MaxDim: 1280, BitrateKbps: 900},
	"low":    {FPS: 8, MaxDim: 960, BitrateKbps: 350},
}

// ResolveVideoPreset normalizes a preset name ("high", "medium", "low") and returns its config.
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
	default:
		return "high", false
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
	for {
		sc1, sc1Len := findStartCode(s.buf, 0)
		if sc1 < 0 {
			// Discard any leading garbage before a potential partial start code.
			if len(s.buf) > 4 {
				s.buf = append([]byte(nil), s.buf[len(s.buf)-4:]...)
			}
			return nil
		}
		sc2, _ := findStartCode(s.buf, sc1+sc1Len)
		if sc2 < 0 {
			if sc1 > 0 {
				s.buf = append([]byte(nil), s.buf[sc1:]...)
			}
			return nil
		}
		nal := s.buf[sc1+sc1Len : sc2]
		if err := s.handleNAL(nal, emit); err != nil {
			return err
		}
		s.buf = append([]byte(nil), s.buf[sc2:]...)
	}
}

// Flush emits any trailing NAL / access unit buffered at EOF.
func (s *AnnexBSplitter) Flush(emit func(H264Chunk) error) error {
	if sc1, sc1Len := findStartCode(s.buf, 0); sc1 >= 0 && sc1+sc1Len < len(s.buf) {
		nal := s.buf[sc1+sc1Len:]
		if err := s.handleNAL(nal, emit); err != nil {
			return err
		}
		s.buf = nil
	}
	return s.flushAU(emit)
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
		if s.auHasSlice {
			if err := s.flushAU(emit); err != nil {
				return err
			}
		}
		if len(s.sps) > 0 {
			s.appendAnnexBNAL(s.sps)
		}
		if len(s.pps) > 0 {
			s.appendAnnexBNAL(s.pps)
		}
		s.appendAnnexBNAL(nal)
		s.auIsKey = true
		s.auHasSlice = true
	case 1: // Non-IDR slice
		if s.auHasSlice {
			if err := s.flushAU(emit); err != nil {
				return err
			}
		}
		s.appendAnnexBNAL(nal)
		s.auIsKey = false
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

// StartH264Stream captures the local desktop with ffmpeg as an Annex-B H.264 stream
// using the given preset ("high" | "medium" | "low") until ctx is cancelled.
func (e *Engine) StartH264Stream(
	ctx context.Context,
	preset string,
	onMeta func(H264Meta) error,
	onChunk func(H264Chunk) error,
) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("需要安装 ffmpeg 以启用 H.264 视频流")
	}
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
	ref, err := e.Frame(ctx, "", cfg.MaxDim, 50)
	if err != nil {
		return err
	}
	w, h := evenDim(ref.ImageWidth), evenDim(ref.ImageHeight)
	if w < 16 || h < 16 {
		return errors.New("invalid desktop frame dimensions")
	}

	encoders := h264EncodersForOS()
	if len(encoders) == 0 {
		return fmt.Errorf("H.264 streaming unsupported on %s", runtime.GOOS)
	}

	var lastErr error
	for _, enc := range encoders {
		emitted := 0
		meta := H264Meta{
			Width:         w,
			Height:        h,
			LogicalX:      ref.LogicalX,
			LogicalY:      ref.LogicalY,
			LogicalWidth:  ref.LogicalWidth,
			LogicalHeight: ref.LogicalHeight,
			FPS:           cfg.FPS,
			BitrateKbps:   cfg.BitrateKbps,
			Preset:        presetName,
			Backend:       "ffmpeg/" + enc,
		}
		if onMeta != nil {
			if err := onMeta(meta); err != nil {
				return err
			}
		}
		args := buildFFmpegH264Args(enc, e.Env(), w, h, cfg)
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
	return errors.New("ffmpeg failed to emit H.264 frames")
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

func buildFFmpegH264Args(encoder string, env []string, w, h int, cfg VideoPresetConfig) []string {
	fps := strconv.Itoa(cfg.FPS)
	gop := strconv.Itoa(cfg.FPS * 2)
	kbps := strconv.Itoa(cfg.BitrateKbps)
	maxKbps := strconv.Itoa((cfg.BitrateKbps * 12) / 10)
	scale := fmt.Sprintf("scale=%d:%d", w, h)

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
	args = append(args, "-vf", scale)
	if encoder == "h264_videotoolbox" {
		args = append(args,
			"-c:v", "h264_videotoolbox",
			"-realtime", "1",
			"-profile:v", "baseline",
			"-pix_fmt", "nv12",
		)
	} else {
		args = append(args,
			"-c:v", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-profile:v", "baseline",
			"-pix_fmt", "yuv420p",
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
