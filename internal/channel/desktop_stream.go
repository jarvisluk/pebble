package channel

import (
	"bot-hub/internal/desktop"
	"bot-hub/internal/hub"
	"bot-hub/internal/protocol"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *HTTPServer) handleDesktopFrame(w http.ResponseWriter, r *http.Request) {
	target := s.resolveDesktopTarget(r, "")
	spec := desktop.ParseTarget(target)
	winID := strings.TrimSpace(r.URL.Query().Get("windowId"))
	maxDim, _ := strconv.Atoi(r.URL.Query().Get("maxDim"))
	quality, _ := strconv.Atoi(r.URL.Query().Get("quality"))

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	if spec.IsLocal() {
		eng, err := s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		fr, err := eng.Frame(ctx, winID, maxDim, quality)
		if err != nil {
			status := http.StatusBadRequest
			payload := map[string]any{"error": err.Error()}
			if desktop.IsScreenRecordingDeniedError(err) {
				status = http.StatusForbidden
				payload["code"] = "screen_recording_denied"
			}
			writeJSON(w, status, payload)
			return
		}
		writeJSON(w, http.StatusOK, fr)
		return
	}

	if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, ""); !ok {
		writeNodeUpgradeErr(w, status, msg, reqCap)
		return
	}
	var fr desktop.FrameResult
	err := s.callRemoteDesktop(ctx, spec.NodeID, "desktop_frame", withSlotArg(map[string]any{
		"windowId": winID,
		"maxDim":   maxDim,
		"quality":  quality,
	}, spec.VirtualSlot), 10*time.Second, &fr)
	if err != nil {
		status := http.StatusBadRequest
		payload := map[string]any{"error": err.Error()}
		if desktop.IsScreenRecordingDeniedError(err) {
			status = http.StatusForbidden
			payload["code"] = "screen_recording_denied"
		}
		writeJSON(w, status, payload)
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
	spec := desktop.ParseTarget(target)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var err error
	if spec.IsLocal() {
		var eng *desktop.Engine
		eng, err = s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err == nil {
			err = eng.SendInput(ctx, body.UnifiedInput)
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, ""); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		err = s.callRemoteDesktop(ctx, spec.NodeID, "desktop_input", withSlotInput(body.UnifiedInput, spec.VirtualSlot), 8*time.Second, nil)
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
	spec := desktop.ParseTarget(target)
	ctx, cancel := context.WithTimeout(r.Context(), 22*time.Second)
	defer cancel()

	var res desktop.StreamOfferResult
	var err error
	if spec.IsLocal() {
		var eng *desktop.Engine
		eng, err = s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err == nil {
			var lp *desktop.StreamOfferResult
			lp, err = eng.StreamOffer(ctx, body.SDP, body.Scope, body.FPS, body.MaxDim, body.Bitrate)
			if lp != nil {
				res = *lp
			}
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, ""); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		err = s.callRemoteDesktop(ctx, spec.NodeID, "desktop_stream_offer", withSlotArg(map[string]any{
			"sdp":     body.SDP,
			"target":  body.Scope,
			"fps":     body.FPS,
			"maxDim":  body.MaxDim,
			"bitrate": body.Bitrate,
		}, spec.VirtualSlot), 20*time.Second, &res)
	}
	if err != nil {
		status := http.StatusServiceUnavailable
		payload := map[string]any{"error": err.Error()}
		if desktop.IsScreenRecordingDeniedError(err) {
			status = http.StatusForbidden
			payload["code"] = "screen_recording_denied"
		}
		writeJSON(w, status, payload)
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
	spec := desktop.ParseTarget(target)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if spec.IsLocal() {
		if eng, err := s.localDeskPool().ForSlot(spec.VirtualSlot); err == nil {
			_ = eng.StreamClose(ctx, body.Session)
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, ""); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		_, _, _ = s.nodes().Call(ctx, spec.NodeID, "desktop_stream_close", withSlotArg(map[string]any{"session": body.Session}, spec.VirtualSlot), 5*time.Second)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleDesktopSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	target := s.resolveDesktopTarget(r, body.Target)
	spec := desktop.ParseTarget(target)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	var res desktop.SetupResult
	if spec.IsLocal() {
		eng, err := s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		rp, err := eng.Setup(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if rp != nil {
			res = *rp
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, protocol.CapNodeDesktopPower); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		if err := s.callRemoteDesktop(ctx, spec.NodeID, "desktop_setup", withSlotArg(map[string]any{}, spec.VirtualSlot), 5*time.Minute, &res); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if !res.OK {
		writeJSON(w, http.StatusBadRequest, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleDesktopPower(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || strings.TrimSpace(body.Action) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "action is required"})
		return
	}
	target := s.resolveDesktopTarget(r, body.Target)
	spec := desktop.ParseTarget(target)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var st desktop.HostStatus
	if spec.IsLocal() {
		eng, err := s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		sp, err := eng.Power(ctx, body.Action)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if sp != nil {
			st = *sp
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, protocol.CapNodeDesktopPower); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		if err := s.callRemoteDesktop(ctx, spec.NodeID, "desktop_power", withSlotArg(map[string]any{"action": body.Action}, spec.VirtualSlot), 12*time.Second, &st); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"target": target,
		"status": st,
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
		TaskID     string `json:"taskId"`
		DeviceID   string `json:"deviceId"`
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
		if strings.TrimSpace(body.Target) == "" {
			if resolved, rerr := s.hub.ResolveBotDesktopTarget(body.BotID, ""); rerr == nil && resolved != "" {
				body.Target = resolved
			}
		}
	}
	if s.hub != nil && strings.TrimSpace(body.TaskID) != "" {
		s.hub.NotifyTaskTakeoverStarted(body.TaskID, body.Target)
		defer s.hub.NotifyTaskTakeoverEnded(body.TaskID)
	}
	note, err := s.desktopCoord().RequestTakeoverWithTask(ctx, body.Target, body.BotID, botName, body.Reason, body.App, body.TaskID, body.DeviceID)
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

func (s *HTTPServer) handleDesktopTask(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	var body struct {
		BotID      string `json:"botId"`
		NodeID     string `json:"nodeId"`
		Goal       string `json:"goal"`
		App        string `json:"app"`
		TimeoutSec int    `json:"timeoutSec"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	res, err := s.hub.RunDesktopTask(r.Context(), hub.DesktopTaskRequest{
		BotID:      body.BotID,
		Target:     body.NodeID,
		Goal:       body.Goal,
		App:        body.App,
		TimeoutSec: body.TimeoutSec,
	})
	if err != nil {
		if errors.Is(err, hub.ErrNodeUpgradeRequired) {
			writeNodeUpgradeErr(w, http.StatusUnprocessableEntity, err.Error(), protocol.CapNodeVirtualSlots)
			return
		}
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, hub.ErrBotNotFound):
			status = http.StatusNotFound
		case errors.Is(err, hub.ErrDesktopTaskBusy):
			status = http.StatusConflict
		case errors.Is(err, hub.ErrLocalDesktopDisabled):
			status = http.StatusForbidden
		case errors.Is(err, hub.ErrBoundDesktopOffline):
			status = http.StatusServiceUnavailable
		case strings.Contains(err.Error(), "goal is required"):
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"botId":      res.BotID,
		"sessionId":  res.SessionID,
		"text":       res.Text,
		"tools":      res.Tools,
		"durationMs": res.DurationMs,
		"usage":      res.Usage,
	})
}
