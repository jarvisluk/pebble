package channel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bot-hub/internal/desktop"
	"bot-hub/internal/node"
)

func (s *HTTPServer) desktopCoord() *desktop.Coordinator {
	if s.hub != nil && s.hub.Desktop() != nil {
		return s.hub.Desktop()
	}
	s.deskOnce.Do(func() {
		s.fallbackDesk = desktop.NewCoordinator(nil, nil)
	})
	return s.fallbackDesk
}

func (s *HTTPServer) localDesk() *desktop.Engine {
	if s.hub != nil && s.hub.LocalDesktop() != nil {
		return s.hub.LocalDesktop()
	}
	s.deskOnce.Do(func() {
		s.fallbackDesk = desktop.NewCoordinator(nil, nil)
	})
	if s.fallbackEngine == nil {
		s.fallbackEngine = desktop.NewEngine()
	}
	return s.fallbackEngine
}

func (s *HTTPServer) resolveDesktopTarget(r *http.Request, bodyTarget string) string {
	t := strings.TrimSpace(bodyTarget)
	if t == "" {
		t = strings.TrimSpace(r.URL.Query().Get("target"))
	}
	return desktop.NormalizeTarget(t)
}

func (s *HTTPServer) callRemoteDesktop(ctx context.Context, nodeID, action string, args any, timeout time.Duration, out any) error {
	res, _, err := s.nodes().Call(ctx, nodeID, action, args, timeout)
	if err != nil {
		return err
	}
	if !res.OK {
		if res.Error != "" {
			return errors.New(res.Error)
		}
		return errors.New("remote desktop action failed")
	}
	if out != nil && strings.TrimSpace(res.Output) != "" {
		return json.Unmarshal([]byte(res.Output), out)
	}
	return nil
}

func (s *HTTPServer) handleDesktopTargets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	coord := s.desktopCoord()

	localSt, _ := s.localDesk().Status(ctx)
	targets := []map[string]any{{
		"id":              "local",
		"name":            "本机桌面",
		"local":           true,
		"supported":       localSt == nil || localSt.Supported,
		"webrtcAvailable": localSt != nil && localSt.WebRTCAvailable,
		"backend":         localSt.Backend,
		"screen":          coord.ScreenState("local"),
	}}

	for _, n := range s.nodes().List() {
		if !n.Connected {
			continue
		}
		hasDesk := false
		for _, p := range n.Permissions {
			if p == node.PermDesktop {
				hasDesk = true
				break
			}
		}
		if !hasDesk {
			continue
		}
		targets = append(targets, map[string]any{
			"id":        n.ID,
			"name":      n.Name,
			"local":     false,
			"os":        n.OS,
			"arch":      n.Arch,
			"supported": true,
			"screen":    coord.ScreenState(n.ID),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"targets":   targets,
		"takeovers": coord.ListTakeovers(),
	})
}

func (s *HTTPServer) handleDesktopState(w http.ResponseWriter, r *http.Request) {
	target := s.resolveDesktopTarget(r, "")
	coord := s.desktopCoord()
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	var st desktop.HostStatus
	var err error
	if target == "local" {
		var lp *desktop.HostStatus
		lp, err = s.localDesk().Status(ctx)
		if lp != nil {
			st = *lp
		}
	} else {
		err = s.callRemoteDesktop(ctx, target, "desktop_status", map[string]any{}, 6*time.Second, &st)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     err.Error(),
			"target":    target,
			"screen":    coord.ScreenState(target),
			"takeovers": coord.ListTakeovers(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"target":    target,
		"status":    st,
		"screen":    coord.ScreenState(target),
		"takeovers": coord.ListTakeovers(),
	})
}

func (s *HTTPServer) handleDesktopWindows(w http.ResponseWriter, r *http.Request) {
	target := s.resolveDesktopTarget(r, "")
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	if target == "local" {
		wins, err := s.localDesk().Windows(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if wins == nil {
			wins = []desktop.WindowInfo{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"windows": wins})
		return
	}

	var out struct {
		Windows []desktop.WindowInfo `json:"windows"`
	}
	if err := s.callRemoteDesktop(ctx, target, "desktop_windows", map[string]any{}, 6*time.Second, &out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if out.Windows == nil {
		out.Windows = []desktop.WindowInfo{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleDesktopFocus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target   string `json:"target"`
		App      string `json:"app"`
		WindowID string `json:"windowId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	target := s.resolveDesktopTarget(r, body.Target)
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	var err error
	if target == "local" {
		err = s.localDesk().Focus(ctx, body.App, body.WindowID)
	} else {
		err = s.callRemoteDesktop(ctx, target, "desktop_focus", map[string]any{"app": body.App, "windowId": body.WindowID}, 6*time.Second, nil)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleDesktopFrame(w http.ResponseWriter, r *http.Request) {
	target := s.resolveDesktopTarget(r, "")
	winID := strings.TrimSpace(r.URL.Query().Get("windowId"))
	maxDim, _ := strconv.Atoi(r.URL.Query().Get("maxDim"))
	quality, _ := strconv.Atoi(r.URL.Query().Get("quality"))

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	if target == "local" {
		fr, err := s.localDesk().Frame(ctx, winID, maxDim, quality)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, fr)
		return
	}

	var fr desktop.FrameResult
	err := s.callRemoteDesktop(ctx, target, "desktop_frame", map[string]any{
		"windowId": winID,
		"maxDim":   maxDim,
		"quality":  quality,
	}, 10*time.Second, &fr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, fr)
}

func (s *HTTPServer) handleDesktopInput(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
		desktop.UnifiedInput
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	target := s.resolveDesktopTarget(r, body.Target)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var err error
	if target == "local" {
		err = s.localDesk().SendInput(ctx, body.UnifiedInput)
	} else {
		err = s.callRemoteDesktop(ctx, target, "desktop_input", body.UnifiedInput, 8*time.Second, nil)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleDesktopStreamOffer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target  string `json:"target"`
		SDP     string `json:"sdp"`
		Scope   string `json:"scope"`
		FPS     int    `json:"fps"`
		MaxDim  int    `json:"maxDim"`
		Bitrate int    `json:"bitrate"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&body); err != nil || strings.TrimSpace(body.SDP) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "sdp is required"})
		return
	}
	target := s.resolveDesktopTarget(r, body.Target)
	ctx, cancel := context.WithTimeout(r.Context(), 22*time.Second)
	defer cancel()

	var res desktop.StreamOfferResult
	var err error
	if target == "local" {
		var lp *desktop.StreamOfferResult
		lp, err = s.localDesk().StreamOffer(ctx, body.SDP, body.Scope, body.FPS, body.MaxDim, body.Bitrate)
		if lp != nil {
			res = *lp
		}
	} else {
		err = s.callRemoteDesktop(ctx, target, "desktop_stream_offer", map[string]any{
			"sdp":     body.SDP,
			"target":  body.Scope,
			"fps":     body.FPS,
			"maxDim":  body.MaxDim,
			"bitrate": body.Bitrate,
		}, 20*time.Second, &res)
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleDesktopStreamClose(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target  string `json:"target"`
		Session string `json:"session"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	target := s.resolveDesktopTarget(r, body.Target)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if target == "local" {
		_ = s.localDesk().StreamClose(ctx, body.Session)
	} else {
		_, _, _ = s.nodes().Call(ctx, target, "desktop_stream_close", map[string]any{"session": body.Session}, 5*time.Second)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleDesktopTakeovers(w http.ResponseWriter, _ *http.Request) {
	items := s.desktopCoord().ListTakeovers()
	if items == nil {
		items = []desktop.TakeoverRequest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleDesktopTakeoverRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target     string `json:"target"`
		BotID      string `json:"botId"`
		Reason     string `json:"reason"`
		App        string `json:"app"`
		TimeoutSec int    `json:"timeoutSec"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	timeout := time.Duration(body.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	botName := body.BotID
	if s.hub != nil && body.BotID != "" {
		botName = s.hub.BotName(body.BotID)
	}
	note, err := s.desktopCoord().RequestTakeover(ctx, body.Target, body.BotID, botName, body.Reason, body.App)
	if err != nil {
		writeJSON(w, http.StatusRequestTimeout, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note": note})
}

func (s *HTTPServer) handleDesktopTakeoverResolve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string `json:"id"`
		BotID string `json:"botId"`
		Note  string `json:"note"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body)
	key := strings.TrimSpace(body.ID)
	if key == "" {
		key = strings.TrimSpace(body.BotID)
	}
	ok := s.desktopCoord().ResolveTakeover(key, body.Note)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "items": s.desktopCoord().ListTakeovers()})
}
