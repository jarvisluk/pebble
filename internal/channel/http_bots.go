package channel

import (
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *HTTPServer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	sts, err := s.hub.Status("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bots":   sts,
		"server": s.serverInfo(),
	})
}

func (s *HTTPServer) handleCreateBot(w http.ResponseWriter, r *http.Request) {
	var req hub.CreateBotRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if queryBool(r, "validate") {
		vctx, vcancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer vcancel()
		if err := s.hub.ValidateCreateBot(vctx, req, true); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	st, err := s.hub.CreateBot(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, st)
}

func (s *HTTPServer) handleBotDetail(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	detail, err := s.hub.GetBotDetail(botID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *HTTPServer) handleBotMessages(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	limit := 200
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	msgs, err := s.hub.Messages(botID, limit)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *HTTPServer) handleBotCollabMessages(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	peerID := r.PathValue("peerId")
	limit := 200
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	msgs, err := s.hub.CollabMessages(botID, peerID, limit)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

type outboundMessageReq struct {
	Text   string              `json:"text"`
	Images []hub.OutboundImage `json:"images,omitempty"`
	Files  []hub.OutboundFile  `json:"files,omitempty"`
}

func (s *HTTPServer) handleOutboundMessage(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var req outboundMessageReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	msg, err := s.hub.SendOutboundMessage(botID, req.Text, req.Images, req.Files...)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

type messageReq struct {
	Message   string   `json:"message"`
	FromBotID string   `json:"fromBotId,omitempty"`
	Hop       int      `json:"hop,omitempty"`
	Source    string   `json:"source,omitempty"`
	MediaIDs  []string `json:"mediaIds,omitempty"`
}

func (s *HTTPServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var req messageReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" && len(req.MediaIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "message or mediaIds is required"})
		return
	}

	if req.FromBotID != "" {
		res, err := s.hub.MessageBot(r.Context(), req.FromBotID, botID, req.Message, req.Hop)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	src := req.Source
	if src == "" {
		src = "web"
	}
	res, err := s.hub.HandleInput(r.Context(), botID, req.Message, hub.SendOptions{
		Source:   src,
		MediaIDs: req.MediaIDs,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleBotCommands(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	cmds, err := s.hub.ListBotCommands(r.Context(), botID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	if cmds == nil {
		cmds = []harness.SlashCommand{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": cmds})
}

func (s *HTTPServer) handleBotMentions(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	q := r.URL.Query().Get("q")
	limit := 30
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if n, err := strconv.Atoi(rawLimit); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	items, err := s.hub.ListBotMentions(r.Context(), botID, q, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	if items == nil {
		items = []hub.MentionCandidate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleUpdateBot(w http.ResponseWriter, r *http.Request) {
	var upd hub.BotUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&upd); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	botID := r.PathValue("id")
	validate := queryBool(r, "validate")
	dryRun := queryBool(r, "dryRun")
	if validate || dryRun {
		vctx, vcancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer vcancel()
		preview, err := s.hub.ValidateBotUpdate(vctx, botID, upd, validate)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, hub.ErrBotNotFound) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		if dryRun {
			writeJSON(w, http.StatusOK, preview)
			return
		}
	}
	st, err := s.hub.UpdateBot(botID, upd)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *HTTPServer) handleDeleteBot(w http.ResponseWriter, r *http.Request) {
	if err := s.hub.DeleteBot(r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, hub.ErrBotNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	ok, err := s.hub.Interrupt(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interrupted": ok})
}

func (s *HTTPServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebhookSecret != "" {
		got := r.Header.Get("X-Webhook-Secret")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.WebhookSecret)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid webhook secret"})
			return
		}
	}

	botID := r.PathValue("id")
	var payload map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json payload"})
		return
	}
	pretty, _ := json.MarshalIndent(payload, "", "  ")
	prompt := fmt.Sprintf("[Webhook Event]\n```json\n%s\n```", string(pretty))

	res, err := s.hub.Send(r.Context(), botID, prompt, hub.SendOptions{Source: "webhook"})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}
