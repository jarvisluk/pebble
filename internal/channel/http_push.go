package channel

import (
	"bot-hub/internal/state"
	"encoding/json"
	"net/http"
	"strings"
)

func (s *HTTPServer) handlePushStatus(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "mode": "disabled", "deviceCount": 0})
		return
	}
	writeJSON(w, http.StatusOK, s.hub.PushStatus())
}

func (s *HTTPServer) handlePushRegister(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	var body struct {
		DeviceID               string `json:"deviceId"`
		Platform               string `json:"platform"`
		Env                    string `json:"env"`
		BundleID               string `json:"bundleId"`
		PushToken              string `json:"pushToken"`
		LiveActivityStartToken string `json:"liveActivityStartToken"`
		Enabled                *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(body.DeviceID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deviceId is required"})
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	err := s.hub.UpsertPushDevice(state.PushDevice{
		DeviceID:               body.DeviceID,
		Platform:               body.Platform,
		Env:                    body.Env,
		BundleID:               body.BundleID,
		PushToken:              body.PushToken,
		LiveActivityStartToken: body.LiveActivityStartToken,
		Enabled:                enabled,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": s.hub.PushStatus()})
}

func (s *HTTPServer) handlePushActivityToken(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	var body struct {
		DeviceID    string `json:"deviceId"`
		ActivityKey string `json:"activityKey"`
		Token       string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(body.DeviceID) == "" || strings.TrimSpace(body.ActivityKey) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deviceId and activityKey are required"})
		return
	}
	if err := s.hub.SetPushActivityToken(body.DeviceID, body.ActivityKey, body.Token); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handlePushDeleteDevice(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	deviceID := strings.TrimSpace(r.PathValue("deviceId"))
	if deviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "deviceId is required"})
		return
	}
	if err := s.hub.DeletePushDevice(deviceID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
