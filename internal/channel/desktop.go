package channel

import (
	"bot-hub/internal/desktop"
	"bot-hub/internal/hub"
	"bot-hub/internal/node"
	"bot-hub/internal/protocol"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
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
