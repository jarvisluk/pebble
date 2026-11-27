package channel

import (
	"bot-hub/internal/config"
	"bot-hub/internal/hub"
	"bot-hub/internal/scheduler"
	"bot-hub/internal/state"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type routineDTO struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	EveryMinutes     int               `json:"everyMinutes,omitempty"`
	DailyAt          string            `json:"dailyAt,omitempty"`
	Cron             string            `json:"cron,omitempty"`
	WebhookTrigger   bool              `json:"webhookTrigger,omitempty"`
	WebhookPath      string            `json:"webhookPath,omitempty"`
	WebhookSecretSet bool              `json:"webhookSecretSet,omitempty"`
	WebhookURL       string            `json:"webhookUrl,omitempty"`
	Prompt           string            `json:"prompt"`
	Model            string            `json:"model,omitempty"`
	MainDelivery     string            `json:"mainDelivery"`
	Enabled          bool              `json:"enabled"`
	Running          bool              `json:"running"`
	NextRunAt        string            `json:"nextRunAt,omitempty"`
	LastRun          *state.RoutineRun `json:"lastRun,omitempty"`
}

func (s *HTTPServer) buildRoutineDTO(botID string, sc config.ScheduleConfig, rt map[string]scheduler.RoutineRuntimeStatus) routineDTO {
	rid := sc.EffectiveID()
	dto := routineDTO{
		ID:               rid,
		Name:             sc.Name,
		EveryMinutes:     sc.EveryMinutes,
		DailyAt:          sc.DailyAt,
		Cron:             sc.Cron,
		WebhookTrigger:   sc.WebhookTrigger,
		WebhookSecretSet: strings.TrimSpace(sc.WebhookSecret) != "",
		WebhookURL:       sc.WebhookURL,
		Prompt:           sc.Prompt,
		Model:            sc.Model,
		MainDelivery:     sc.EffectiveMainDelivery(),
		Enabled:          sc.IsEnabled(),
	}
	if sc.WebhookTrigger {
		dto.WebhookPath = fmt.Sprintf("/hooks/%s/routines/%s", botID, rid)
	}
	if st, ok := rt[rid]; ok {
		dto.Running = st.Running
		dto.NextRunAt = st.NextRunAt
		dto.LastRun = st.LastRun
	} else if last, ok := s.hub.LatestRoutineRun(botID, rid); ok {
		copyLast := last
		dto.LastRun = &copyLast
	}
	return dto
}

func (s *HTTPServer) routineStatusMap(botID string) map[string]scheduler.RoutineRuntimeStatus {
	if sched := s.scheduler(); sched != nil {
		return sched.Status(botID)
	}
	return nil
}

func (s *HTTPServer) handleListRoutines(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	list, err := s.hub.ListRoutines(botID)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	rt := s.routineStatusMap(botID)
	out := make([]routineDTO, 0, len(list))
	for _, sc := range list {
		out = append(out, s.buildRoutineDTO(botID, sc, rt))
	}
	writeJSON(w, http.StatusOK, map[string]any{"routines": out})
}

func (s *HTTPServer) handleCreateRoutine(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	var req config.ScheduleConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	var (
		saved config.ScheduleConfig
		err   error
	)
	if r.URL.Query().Get("upsert") == "1" {
		saved, err = s.hub.UpsertRoutine(botID, req)
	} else {
		saved, err = s.hub.CreateRoutine(botID, req)
	}
	if err != nil {
		writeHubErr(w, err)
		return
	}
	dto := s.buildRoutineDTO(botID, saved, s.routineStatusMap(botID))
	status := http.StatusCreated
	if r.URL.Query().Get("upsert") == "1" {
		status = http.StatusOK
	}
	writeJSON(w, status, dto)
}

func (s *HTTPServer) handleUpdateRoutine(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	routineID := r.PathValue("routineId")
	var patch hub.RoutinePatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	saved, err := s.hub.UpdateRoutine(botID, routineID, patch)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	dto := s.buildRoutineDTO(botID, saved, s.routineStatusMap(botID))
	writeJSON(w, http.StatusOK, dto)
}

func (s *HTTPServer) handleDeleteRoutine(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	routineID := r.PathValue("routineId")
	if err := s.hub.DeleteRoutine(botID, routineID); err != nil {
		writeHubErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleRunRoutine(w http.ResponseWriter, r *http.Request) {
	sched := s.scheduler()
	if sched == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "scheduler is not running"})
		return
	}
	botID := r.PathValue("id")
	routineID := r.PathValue("routineId")
	if r.URL.Query().Get("async") == "1" {
		list, err := s.hub.ListRoutines(botID)
		if err != nil {
			writeHubErr(w, err)
			return
		}
		found := false
		for _, sc := range list {
			if sc.EffectiveID() == routineID {
				found = true
				break
			}
		}
		if !found {
			writeHubErr(w, fmt.Errorf("routine %q not found: %w", routineID, hub.ErrRoutineNotFound))
			return
		}
		if rt := sched.Status(botID); rt[routineID].Running {
			writeHubErr(w, scheduler.ErrRoutineAlreadyRunning)
			return
		}
		go func() {
			_, _ = sched.RunNow(context.Background(), botID, routineID)
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "botId": botID, "routineId": routineID})
		return
	}
	run, err := sched.RunNow(r.Context(), botID, routineID)
	if err != nil {
		if errors.Is(err, hub.ErrBotNotFound) || errors.Is(err, hub.ErrRoutineNotFound) || errors.Is(err, scheduler.ErrRoutineAlreadyRunning) {
			writeHubErr(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "run": run})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *HTTPServer) handleListRoutineRuns(w http.ResponseWriter, r *http.Request) {
	botID := r.PathValue("id")
	q := r.URL.Query()
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := s.hub.ListRoutineRuns(botID, q.Get("routineId"), limit)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *HTTPServer) handleRoutineWebhook(w http.ResponseWriter, r *http.Request) {
	sched := s.scheduler()
	if sched == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "scheduler is not running"})
		return
	}
	botID := r.PathValue("id")
	routineID := r.PathValue("routineId")

	list, err := s.hub.ListRoutines(botID)
	if err != nil {
		writeHubErr(w, err)
		return
	}
	var target *config.ScheduleConfig
	for i := range list {
		if list[i].EffectiveID() == routineID {
			sc := list[i]
			target = &sc
			break
		}
	}
	if target == nil {
		writeHubErr(w, fmt.Errorf("routine %q not found: %w", routineID, hub.ErrRoutineNotFound))
		return
	}
	if !target.IsEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "routine is disabled"})
		return
	}
	if !target.WebhookTrigger {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "webhook trigger is not enabled for this routine"})
		return
	}

	expectedSecret := strings.TrimSpace(target.WebhookSecret)
	if expectedSecret == "" {
		expectedSecret = s.cfg.WebhookSecret
	}
	if expectedSecret != "" {
		got := r.Header.Get("X-Webhook-Secret")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(expectedSecret)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid webhook secret"})
			return
		}
	}

	rawBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "failed to read webhook payload"})
		return
	}
	payloadText := ""
	if trimmed := bytes.TrimSpace(rawBody); len(trimmed) > 0 {
		var formatted bytes.Buffer
		if json.Indent(&formatted, trimmed, "", "  ") == nil {
			payloadText = "```json\n" + formatted.String() + "\n```"
		} else {
			payloadText = string(trimmed)
		}
	}

	if r.URL.Query().Get("async") == "1" {
		if rt := sched.Status(botID); rt[routineID].Running {
			writeHubErr(w, scheduler.ErrRoutineAlreadyRunning)
			return
		}
		go func() {
			_, _ = sched.TriggerWebhook(context.Background(), botID, routineID, payloadText)
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "botId": botID, "routineId": routineID})
		return
	}

	run, err := sched.TriggerWebhook(r.Context(), botID, routineID, payloadText)
	if err != nil {
		if errors.Is(err, hub.ErrBotNotFound) || errors.Is(err, hub.ErrRoutineNotFound) || errors.Is(err, scheduler.ErrRoutineAlreadyRunning) {
			writeHubErr(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "run": run})
		return
	}
	writeJSON(w, http.StatusOK, run)
}
