package channel

import (
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

func (s *HTTPServer) handleListBotTasks(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	botID := r.PathValue("id")
	q := r.URL.Query()
	limit := 50
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	tasks, err := s.hub.ListTasks(botID, state.TaskListFilter{
		Kind:   q.Get("kind"),
		Status: q.Get("status"),
		Limit:  limit,
	})
	if err != nil {
		writeHubErr(w, err)
		return
	}
	if tasks == nil {
		tasks = []state.BotTask{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *HTTPServer) handleSpawnTask(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	botID := r.PathValue("id")
	var req hub.TaskSpawnRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	req.BotID = botID

	task, err := s.hub.SpawnTask(r.Context(), req)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *HTTPServer) handleGetTask(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	taskID := r.PathValue("taskId")
	task, err := s.hub.GetTask(taskID)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *HTTPServer) handleListTaskEvents(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	taskID := r.PathValue("taskId")
	q := r.URL.Query()
	var afterSeq int64
	if raw := strings.TrimSpace(q.Get("afterSeq")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid afterSeq"})
			return
		}
		afterSeq = v
	}
	limit := 200
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	events, err := s.hub.ListTaskEvents(taskID, afterSeq, limit)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	if events == nil {
		events = []state.BotTaskEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *HTTPServer) handleSendToTask(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	taskID := r.PathValue("taskId")
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	task, err := s.hub.SendToTask(r.Context(), taskID, body.Message)
	if err != nil {
		if errors.Is(err, state.ErrTaskNotFound) {
			writeHubErr(w, err)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *HTTPServer) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	taskID := r.PathValue("taskId")
	task, err := s.hub.CancelTask(taskID)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}
