package channel

import (
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *HTTPServer) handleReset(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	prev, existed, err := s.hub.ResetSession(botID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reset":           existed,
		"previousSession": prev.SessionID,
		"sessionId":       prev.ID,
	})
}

func (s *HTTPServer) handleRotate(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var body struct {
		HandoffNote string `json:"handoffNote"`
		Defer       bool   `json:"defer"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)
	res, err := s.hub.RotateSession(botID, body.HandoffNote, body.Defer)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	s.serveSessionList(w, r, r.URL.Query().Get("botId"))
}

func (s *HTTPServer) handleListBotSessions(w http.ResponseWriter, r *http.Request) {
	s.serveSessionList(w, r, r.PathValue("id"))
}

func (s *HTTPServer) serveSessionList(w http.ResponseWriter, r *http.Request, botID string) {
	q := r.URL.Query()
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	callerBotID := strings.TrimSpace(q.Get("callerBotId"))
	list, err := s.hub.ListSessions(callerBotID, state.SessionListOptions{
		BotID:         botID,
		Query:         q.Get("query"),
		Limit:         limit,
		IncludeActive: q.Get("includeActive") != "false",
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrForbidden) {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (s *HTTPServer) handleRecallSession(w http.ResponseWriter, r *http.Request) {
	s.serveSessionRecall(w, r, r.URL.Query().Get("botId"), r.PathValue("sessionId"))
}

func (s *HTTPServer) handleRecallBotSession(w http.ResponseWriter, r *http.Request) {
	s.serveSessionRecall(w, r, r.PathValue("id"), r.PathValue("sessionId"))
}

func (s *HTTPServer) serveSessionRecall(w http.ResponseWriter, r *http.Request, botID, sessionID string) {
	q := r.URL.Query()
	parseInt := func(name string) int {
		if v := q.Get(name); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
		return 0
	}
	callerBotID := strings.TrimSpace(q.Get("callerBotId"))
	res, err := s.hub.RecallSession(callerBotID, state.RecallOptions{
		SessionID: sessionID,
		BotID:     botID,
		Tail:      parseInt("tail"),
		Offset:    parseInt("offset"),
		Limit:     parseInt("limit"),
		Query:     q.Get("query"),
		MaxChars:  parseInt("maxChars"),
		MessageID: q.Get("messageId"),
	})
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, hub.ErrForbidden):
			status = http.StatusForbidden
		case errors.Is(err, state.ErrSessionNotFound):
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	q := r.URL.Query()
	limit := 30
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	res, err := s.hub.MemorySearch(ctx, q.Get("scope"), q.Get("botId"), q.Get("query"), q.Get("kind"), limit)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleMemorySave(w http.ResponseWriter, r *http.Request) {
	var req harness.MagpieMemorySaveReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	msg, err := s.hub.MemorySave(ctx, req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *HTTPServer) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	msg, err := s.hub.MemoryForget(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}
