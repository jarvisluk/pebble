package channel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
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
		s.fallbackDesk = desktop.NewCoordinator(nil, nil, nil)
	})
	return s.fallbackDesk
}

func (s *HTTPServer) localDesk() *desktop.Engine {
	if s.hub != nil && s.hub.LocalDesktop() != nil {
		return s.hub.LocalDesktop()
	}
	s.deskOnce.Do(func() {
		s.fallbackDesk = desktop.NewCoordinator(nil, nil, nil)
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

	allowLocal := true
	if s.hub != nil {
		allowLocal = s.hub.Config().LocalDesktopAllowed()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"targets":           targets,
		"takeovers":         coord.ListTakeovers(),
		"allowLocalDesktop": allowLocal,
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
	if err := s.desktopCoord().CheckHumanInput(target); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
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
	if err := s.desktopCoord().CheckHumanInput(target); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}

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
	if s.desktopCoord().ScreenState(target).Owner == "agent" {
		if target == "local" {
			_ = s.localDesk().SetStreamInput(ctx, false)
		} else {
			_, _, _ = s.nodes().Call(ctx, target, "desktop_stream_set_input", map[string]any{"enabled": false}, 3*time.Second)
		}
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

func (s *HTTPServer) handleDesktopForceTakeover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
		BotID  string `json:"botId"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	target := s.resolveDesktopTarget(r, body.Target)
	paused := s.desktopCoord().ForceTakeover(target)
	if paused == "" {
		paused = strings.TrimSpace(body.BotID)
	}
	interrupted := false
	if paused != "" && s.hub != nil {
		interrupted, _ = s.hub.Interrupt(paused)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"screen":         s.desktopCoord().ScreenState(target),
		"interruptedBot": paused,
		"interrupted":    interrupted,
	})
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

// handleDesktopMCP executes Bot-initiated `node_desktop` operations uniformly across local and remote nodes,
// updating ScreenLock while automation runs.
func (s *HTTPServer) handleDesktopMCP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID     string   `json:"nodeId"`
		BotID      string   `json:"botId"`
		Op         string   `json:"op"`
		Script     string   `json:"script"`
		Language   string   `json:"language"`
		Target     string   `json:"target"`
		App        string   `json:"app"`
		X          *float64 `json:"x"`
		Y          *float64 `json:"y"`
		DeltaY     float64  `json:"deltaY"`
		Text       string   `json:"text"`
		Key        string   `json:"key"`
		TimeoutSec int      `json:"timeoutSec"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	op := strings.TrimSpace(body.Op)
	if op == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "op is required"})
		return
	}

	target := strings.TrimSpace(body.NodeID)
	if target == "" {
		// If at least one remote node is connected, keep `_default` node resolution unless explicitly "local".
		connectedNodes := 0
		for _, n := range s.nodes().List() {
			if n.Connected {
				connectedNodes++
			}
		}
		if connectedNodes == 0 {
			target = "local"
		}
	}

	isLocal := strings.EqualFold(target, "local")
	if isLocal {
		target = "local"
		if s.hub != nil && !s.hub.Config().LocalDesktopAllowed() {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "本机桌面操控已在「设置 › 远程与设备」中关闭"})
			return
		}
	}

	botName := body.BotID
	if s.hub != nil && body.BotID != "" {
		botName = s.hub.BotName(body.BotID)
	}
	lockTarget := target
	if lockTarget == "" {
		lockTarget = "_default"
	}
	if op != "screenshot" {
		s.desktopCoord().BeginAgentTool(lockTarget, body.BotID, botName, op)
		defer s.desktopCoord().EndAgentTool(lockTarget, body.BotID)
	}

	timeout := time.Duration(body.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	if isLocal {
		switch op {
		case "screenshot":
			fr, err := s.localDesk().Frame(ctx, "", 1920, 80)
			if err != nil {
				writeJSON(w, http.StatusOK, node.Result{NodeID: "local", OK: false, Error: err.Error()})
				return
			}
			res := &node.Result{NodeID: "local", OK: true, MimeType: "image/jpeg", DataBase64: fr.JPEGBase64, Output: "captured local screen"}
			if p, saveErr := s.saveNodeBinary(body.BotID, "local", res); saveErr == nil {
				res.SavedPath = p
				res.DataBase64 = ""
			}
			writeJSON(w, http.StatusOK, res)
			return
		case "osascript":
			out, err := node.RunOSAScript(ctx, body.Script, body.Language)
			if err != nil {
				writeJSON(w, http.StatusOK, node.Result{NodeID: "local", OK: false, ExitCode: 1, Error: err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, node.Result{NodeID: "local", OK: true, Output: out})
			return
		case "open":
			home, _ := os.UserHomeDir()
			if err := node.OpenTarget(ctx, body.Target, body.App, home); err != nil {
				writeJSON(w, http.StatusOK, node.Result{NodeID: "local", OK: false, Error: err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, node.Result{NodeID: "local", OK: true, Output: "opened"})
			return
		case "click", "double_click", "right_click", "type", "key", "scroll":
			err := s.localDesk().SendInput(ctx, desktop.UnifiedInput{
				Kind:   op,
				X:      body.X,
				Y:      body.Y,
				DeltaY: body.DeltaY,
				Text:   body.Text,
				Key:    body.Key,
				App:    body.App,
			})
			if err != nil {
				writeJSON(w, http.StatusOK, node.Result{NodeID: "local", OK: false, Error: err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, node.Result{NodeID: "local", OK: true, Output: "ok"})
			return
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported desktop op " + op})
			return
		}
	}

	var action string
	var args any
	switch op {
	case "screenshot":
		action, args = "screenshot", map[string]any{}
	case "osascript":
		action, args = "osascript", map[string]any{"script": body.Script, "language": body.Language}
	case "open":
		action, args = "open", map[string]any{"target": body.Target, "app": body.App}
	case "click", "double_click", "right_click", "type", "key", "scroll":
		action = "desktop_input"
		args = desktop.UnifiedInput{
			Kind:   op,
			X:      body.X,
			Y:      body.Y,
			DeltaY: body.DeltaY,
			Text:   body.Text,
			Key:    body.Key,
			App:    body.App,
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported desktop op " + op})
		return
	}

	res, resolvedID, err := s.nodes().Call(ctx, target, action, args, timeout)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if res != nil && res.OK && res.DataBase64 != "" {
		if path, saveErr := s.saveNodeBinary(body.BotID, resolvedID, res); saveErr == nil {
			res.SavedPath = path
			res.DataBase64 = ""
		}
	}
	writeJSON(w, http.StatusOK, res)
}
