package channel

import (
	"bot-hub/internal/hub"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (s *HTTPServer) handleQuestions(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []hub.PendingQuestion{}})
		return
	}
	botID := strings.TrimSpace(r.URL.Query().Get("botId"))
	items := s.hub.ListQuestions(botID)
	if items == nil {
		items = []hub.PendingQuestion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleQuestionReply(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing question id"})
		return
	}
	var body struct {
		Answers   map[string]any `json:"answers"`
		Cancelled bool           `json:"cancelled"`
		Message   string         `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	items, err := s.hub.AnswerQuestion(id, body.Answers, body.Cancelled, body.Message)
	if err != nil {
		if errors.Is(err, hub.ErrQuestionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (s *HTTPServer) handleApprovals(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []hub.PendingApproval{}})
		return
	}
	botID := strings.TrimSpace(r.URL.Query().Get("botId"))
	items := s.hub.ListApprovals(botID)
	if items == nil {
		items = []hub.PendingApproval{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleApprovalReply(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "hub unavailable"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing approval id"})
		return
	}
	var body struct {
		Decision string `json:"decision"`
		OptionID string `json:"optionId"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	items, err := s.hub.ReplyApproval(id, body.Decision, body.OptionID, body.Reason)
	if err != nil {
		if errors.Is(err, hub.ErrApprovalNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}
