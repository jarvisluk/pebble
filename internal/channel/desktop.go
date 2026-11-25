package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bot-hub/internal/desktop"
	"bot-hub/internal/hub"
	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
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

func (s *HTTPServer) localDeskPool() *desktop.EnginePool {
	if s.hub != nil && s.hub.LocalDesktopPool() != nil {
		return s.hub.LocalDesktopPool()
	}
	s.deskOnce.Do(func() {
		s.fallbackDesk = desktop.NewCoordinator(nil, nil)
		s.fallbackEngine = desktop.NewEnginePool(desktop.VirtualConfig{})
	})
	return s.fallbackEngine
}

func (s *HTTPServer) localDesk() *desktop.Engine {
	return s.localDeskPool().Default()
}

func (s *HTTPServer) resolveDesktopTarget(r *http.Request, bodyTarget string) string {
	t := strings.TrimSpace(bodyTarget)
	if t == "" {
		t = strings.TrimSpace(r.URL.Query().Get("target"))
	}
	return desktop.NormalizeTarget(t)
}

func withSlotArg(args map[string]any, slot string) map[string]any {
	if slot == "" {
		return args
	}
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out["slot"] = slot
	return out
}

func withSlotInput(ev desktop.UnifiedInput, slot string) any {
	if slot == "" {
		return ev
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return ev
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ev
	}
	m["slot"] = slot
	return m
}

func writeNodeUpgradeErr(w http.ResponseWriter, status int, msg, reqCap string) {
	payload := map[string]any{
		"error":       msg,
		"code":        protocol.ErrCodeNodeUpgradeRequired,
		"minProtocol": protocol.CurrentProtocol,
	}
	if reqCap != "" {
		payload["requiredCapability"] = reqCap
	}
	writeJSON(w, status, payload)
}

func (s *HTTPServer) checkRemoteDesktopCapability(nodeID, slot, requiredCap string) (status int, msg, reqCap string, ok bool) {
	n, found := s.nodes().Get(nodeID)
	if !found || !n.Connected {
		return 0, "", "", true
	}
	if slot != "" && !n.HasCapability(protocol.CapNodeVirtualSlots) {
		if n.OS != "linux" {
			return http.StatusUnprocessableEntity, fmt.Sprintf("remote node %q (%s) does not support dedicated virtual screens", nodeID, n.OS), protocol.CapNodeVirtualSlots, false
		}
		return http.StatusUnprocessableEntity, fmt.Sprintf("remote node %q is outdated and does not support dedicated virtual screens; upgrade the node or bind its main desktop in Bot Details", nodeID), protocol.CapNodeVirtualSlots, false
	}
	if requiredCap != "" && !n.HasCapability(requiredCap) {
		return http.StatusNotImplemented, fmt.Sprintf("remote node %q is outdated and does not support this desktop action; please upgrade the node", nodeID), requiredCap, false
	}
	return 0, "", "", true
}

func synthesizePermissionsFromStatus(target string, st desktop.HostStatus, fallbackOS string) desktop.PermissionsResponse {
	plat := strings.TrimSpace(st.Platform)
	if plat == "" {
		plat = fallbackOS
	}
	items := make([]desktop.PermissionItem, 0, 2)
	if sr := strings.TrimSpace(st.ScreenRecording); sr != "" && sr != "unsupported" {
		items = append(items, desktop.PermissionItem{
			ID:              "screen_recording",
			Name:            "Screen Recording",
			Description:     "Capture the host desktop screen for live remote desktop preview.",
			Status:          sr,
			CanOpenSettings: plat == "darwin",
		})
	}
	if ax := strings.TrimSpace(st.Accessibility); ax != "" && ax != "unsupported" {
		items = append(items, desktop.PermissionItem{
			ID:              "accessibility",
			Name:            "Accessibility",
			Description:     "Inject remote mouse, scroll, and keyboard input and inspect desktop windows.",
			Status:          ax,
			CanOpenSettings: plat == "darwin",
		})
	}
	return desktop.PermissionsResponse{
		Target:   target,
		Platform: plat,
		Items:    items,
	}
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

	pool := s.localDeskPool()
	localSt, _ := pool.Default().Status(ctx)
	backend := ""
	if localSt != nil {
		backend = localSt.Backend
	}
	localVirtualSupported := pool.VirtualSupported()
	localEntry := map[string]any{
		"id":               "local",
		"name":             "Host Desktop",
		"local":            true,
		"supported":        localSt == nil || localSt.Supported,
		"webrtcAvailable":  localSt != nil && localSt.WebRTCAvailable,
		"backend":          backend,
		"virtualSupported": localVirtualSupported,
		"screen":           coord.ScreenState("local"),
	}
	if localSt != nil {
		if localSt.Virtual {
			localEntry["virtual"] = true
		}
		if localSt.PowerState != "" {
			localEntry["powerState"] = localSt.PowerState
		}
		if localSt.Installable {
			localEntry["installable"] = true
		}
	}
	targets := []map[string]any{localEntry}
	seenTargets := map[string]bool{"local": true}
	remoteNodes := make(map[string]node.NodeInfo)

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
		remoteNodes[n.ID] = n
		seenTargets[n.ID] = true
		entry := map[string]any{
			"id":               n.ID,
			"name":             n.Name,
			"local":            false,
			"os":               n.OS,
			"arch":             n.Arch,
			"supported":        true,
			"virtualSupported": n.HasCapability(protocol.CapNodeVirtualSlots),
			"protocolVersion":  n.ProtocolVersion,
			"screen":           coord.ScreenState(n.ID),
		}
		if n.IsOutdated() {
			entry["outdated"] = true
		}
		targets = append(targets, entry)
	}

	if localVirtualSupported {
		for _, slot := range pool.ListSlots() {
			if seenTargets[slot.Target] {
				continue
			}
			seenTargets[slot.Target] = true
			targets = append(targets, map[string]any{
				"id":         slot.Target,
				"name":       "Virtual Screen · " + slot.Slot,
				"local":      true,
				"hostId":     "local",
				"slot":       slot.Slot,
				"virtual":    true,
				"supported":  true,
				"powerState": string(slot.PowerState),
				"screen":     coord.ScreenState(slot.Target),
			})
		}
	}

	if s.hub != nil {
		if bots, err := s.hub.Status(""); err == nil {
			for _, b := range bots {
				if strings.TrimSpace(b.DesktopTarget) == "" {
					continue
				}
				spec := desktop.ParseTarget(b.DesktopTarget)
				if !spec.IsVirtualSlot() || seenTargets[spec.Raw] {
					continue
				}
				if spec.IsLocal() {
					if !localVirtualSupported {
						continue
					}
					seenTargets[spec.Raw] = true
					targets = append(targets, map[string]any{
						"id":        spec.Raw,
						"name":      "Virtual Screen · " + spec.VirtualSlot,
						"local":     true,
						"hostId":    "local",
						"slot":      spec.VirtualSlot,
						"virtual":   true,
						"supported": true,
						"screen":    coord.ScreenState(spec.Raw),
					})
				} else if n, ok := remoteNodes[spec.NodeID]; ok && n.HasCapability(protocol.CapNodeVirtualSlots) {
					seenTargets[spec.Raw] = true
					targets = append(targets, map[string]any{
						"id":        spec.Raw,
						"name":      n.Name + " · Virtual Screen " + spec.VirtualSlot,
						"local":     false,
						"hostId":    n.ID,
						"slot":      spec.VirtualSlot,
						"os":        n.OS,
						"arch":      n.Arch,
						"virtual":   true,
						"supported": true,
						"screen":    coord.ScreenState(spec.Raw),
					})
				}
			}
		}
	}

	activeTarget := hub.ActiveDesktopTarget{Target: "local", Temporary: false}
	if s.hub != nil {
		activeTarget = s.hub.ActiveDesktopTarget()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"targets":      targets,
		"activeTarget": activeTarget,
		"takeovers":    coord.ListTakeovers(),
	})
}

func (s *HTTPServer) handleDesktopTargetSet(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	var body struct {
		Target     string `json:"target"`
		TTLMinutes int    `json:"ttlMinutes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	var ttl time.Duration
	if body.TTLMinutes > 0 {
		ttl = time.Duration(body.TTLMinutes) * time.Minute
	}
	snap, err := s.hub.SetActiveDesktopTarget(body.Target, ttl)
	if err != nil {
		if errors.Is(err, hub.ErrNodeUpgradeRequired) {
			writeNodeUpgradeErr(w, http.StatusUnprocessableEntity, err.Error(), protocol.CapNodeVirtualSlots)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activeTarget": snap})
}

func (s *HTTPServer) handleDesktopState(w http.ResponseWriter, r *http.Request) {
	target := s.resolveDesktopTarget(r, "")
	spec := desktop.ParseTarget(target)
	coord := s.desktopCoord()
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	var st desktop.HostStatus
	var err error
	if spec.IsLocal() {
		var eng *desktop.Engine
		eng, err = s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err == nil {
			var lp *desktop.HostStatus
			lp, err = eng.Status(ctx)
			if lp != nil {
				st = *lp
			}
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, ""); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		err = s.callRemoteDesktop(ctx, spec.NodeID, "desktop_status", withSlotArg(map[string]any{}, spec.VirtualSlot), 6*time.Second, &st)
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

func (s *HTTPServer) handleDesktopPermissions(w http.ResponseWriter, r *http.Request) {
	target := s.resolveDesktopTarget(r, "")
	spec := desktop.ParseTarget(target)
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	var resp desktop.PermissionsResponse
	if spec.IsLocal() {
		eng, err := s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		lp, err := eng.Permissions(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if lp != nil {
			resp = *lp
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, ""); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		if n, found := s.nodes().Get(spec.NodeID); found && n.Connected && !n.HasCapability(protocol.CapNodeDesktopPermissions) {
			var st desktop.HostStatus
			if err := s.callRemoteDesktop(ctx, spec.NodeID, "desktop_status", withSlotArg(map[string]any{}, spec.VirtualSlot), 6*time.Second, &st); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			resp = synthesizePermissionsFromStatus(target, st, n.OS)
		} else if err := s.callRemoteDesktop(ctx, spec.NodeID, "desktop_permissions", withSlotArg(map[string]any{}, spec.VirtualSlot), 6*time.Second, &resp); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	resp.Target = target
	if resp.Items == nil {
		resp.Items = []desktop.PermissionItem{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) handleDesktopPermissionRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target       string `json:"target"`
		Permission   string `json:"permission"`
		OpenSettings bool   `json:"openSettings"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || strings.TrimSpace(body.Permission) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "permission is required"})
		return
	}
	target := s.resolveDesktopTarget(r, body.Target)
	spec := desktop.ParseTarget(target)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var res desktop.PermissionRequestResult
	if spec.IsLocal() {
		eng, err := s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		rp, err := eng.RequestPermission(ctx, body.Permission, body.OpenSettings)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if rp != nil {
			res = *rp
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, protocol.CapNodeDesktopPermissions); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		if err := s.callRemoteDesktop(ctx, spec.NodeID, "desktop_permission_request", withSlotArg(map[string]any{
			"permission":   body.Permission,
			"openSettings": body.OpenSettings,
		}, spec.VirtualSlot), 10*time.Second, &res); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if res.Items == nil {
		res.Items = []desktop.PermissionItem{}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleDesktopWindows(w http.ResponseWriter, r *http.Request) {
	target := s.resolveDesktopTarget(r, "")
	spec := desktop.ParseTarget(target)
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	if spec.IsLocal() {
		eng, err := s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		wins, err := eng.Windows(ctx)
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

	if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, ""); !ok {
		writeNodeUpgradeErr(w, status, msg, reqCap)
		return
	}
	var out struct {
		Windows []desktop.WindowInfo `json:"windows"`
	}
	if err := s.callRemoteDesktop(ctx, spec.NodeID, "desktop_windows", withSlotArg(map[string]any{}, spec.VirtualSlot), 6*time.Second, &out); err != nil {
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
	spec := desktop.ParseTarget(target)
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	var err error
	if spec.IsLocal() {
		var eng *desktop.Engine
		eng, err = s.localDeskPool().ForSlot(spec.VirtualSlot)
		if err == nil {
			err = eng.Focus(ctx, body.App, body.WindowID)
		}
	} else {
		if status, msg, reqCap, ok := s.checkRemoteDesktopCapability(spec.NodeID, spec.VirtualSlot, ""); !ok {
			writeNodeUpgradeErr(w, status, msg, reqCap)
			return
		}
		err = s.callRemoteDesktop(ctx, spec.NodeID, "desktop_focus", withSlotArg(map[string]any{"app": body.App, "windowId": body.WindowID}, spec.VirtualSlot), 6*time.Second, nil)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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
			if errors.Is(err, desktop.ErrScreenRecordingDenied) {
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
		if errors.Is(err, desktop.ErrScreenRecordingDenied) {
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
