package channel

import (
	"bot-hub/internal/hub"
	"bot-hub/internal/memory"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func writeMemoryErr(w http.ResponseWriter, err error) {
	var secErr *memory.SecretError
	if errors.As(err, &secErr) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": secErr.Error(),
			"code":  secErr.Code,
			"kind":  secErr.Kind,
			"field": secErr.Field,
		})
		return
	}
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, hub.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, hub.ErrBotNotFound), errors.Is(err, memory.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, memory.ErrSecretDetected),
		errors.Is(err, memory.ErrInvalidScope),
		errors.Is(err, memory.ErrInvalidRequest),
		errors.Is(err, memory.ErrPathOutsideJail),
		errors.Is(err, memory.ErrProtectedFile),
		errors.Is(err, memory.ErrIndexTooLarge):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func parseTagsQuery(raw []string) []string {
	var out []string
	for _, entry := range raw {
		for _, part := range strings.Split(entry, ",") {
			if t := strings.TrimSpace(part); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
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
	callerBotID := strings.TrimSpace(q.Get("callerBotId"))
	res, err := s.hub.MemorySearch(ctx, callerBotID, memory.SearchOpts{
		Scope:           q.Get("scope"),
		BotID:           q.Get("botId"),
		Query:           q.Get("query"),
		Kind:            q.Get("kind"),
		Tags:            parseTagsQuery(q["tags"]),
		Status:          q.Get("status"),
		IncludeSessions: queryBool(r, "includeSessions"),
		IncludeDaily:    queryBool(r, "includeDaily"),
		Compact:         queryBool(r, "compact"),
		Limit:           limit,
	})
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	q := r.URL.Query()
	id := strings.TrimSpace(r.PathValue("id"))
	rawPath := strings.TrimSpace(q.Get("path"))
	if rawPath != "" && (id == "_" || id == "_file" || id == "by-path") {
		id = ""
	}
	startLine, _ := strconv.Atoi(q.Get("startLine"))
	endLine, _ := strconv.Atoi(q.Get("endLine"))
	callerBotID := strings.TrimSpace(q.Get("callerBotId"))

	res, err := s.hub.MemoryGet(ctx, callerBotID, memory.GetOpts{
		ID:        id,
		Path:      rawPath,
		Scope:     q.Get("scope"),
		BotID:     q.Get("botId"),
		StartLine: startLine,
		EndLine:   endLine,
	})
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *HTTPServer) handleMemorySave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID            string   `json:"id,omitempty"`
		Scope         string   `json:"scope,omitempty"`
		BotID         string   `json:"botId,omitempty"`
		CallerBotID   string   `json:"callerBotId,omitempty"`
		Kind          string   `json:"kind,omitempty"`
		Title         string   `json:"title,omitempty"`
		Text          string   `json:"text"`
		Why           string   `json:"why,omitempty"`
		Tags          []string `json:"tags,omitempty"`
		Pin           *bool    `json:"pin,omitempty"`
		Status        string   `json:"status,omitempty"`
		Confidence    *float64 `json:"confidence,omitempty"`
		ValidFrom     string   `json:"validFrom,omitempty"`
		ValidUntil    string   `json:"validUntil,omitempty"`
		Supersedes    string   `json:"supersedes,omitempty"`
		Daily         bool     `json:"daily,omitempty"`
		Date          string   `json:"date,omitempty"`
		SourceSession string   `json:"sourceSession,omitempty"`
		SourceAuthor  string   `json:"sourceAuthor,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	callerBotID := strings.TrimSpace(body.CallerBotID)
	if callerBotID == "" {
		callerBotID = strings.TrimSpace(r.URL.Query().Get("callerBotId"))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	res, err := s.hub.MemorySave(ctx, callerBotID, hub.MemorySaveRequest{
		SaveReq: memory.SaveReq{
			ID:            body.ID,
			Scope:         body.Scope,
			BotID:         body.BotID,
			Kind:          body.Kind,
			Title:         body.Title,
			Text:          body.Text,
			Why:           body.Why,
			Tags:          body.Tags,
			Pin:           body.Pin,
			Status:        body.Status,
			Confidence:    body.Confidence,
			ValidFrom:     body.ValidFrom,
			ValidUntil:    body.ValidUntil,
			Daily:         body.Daily,
			Date:          body.Date,
			SourceSession: body.SourceSession,
			SourceAuthor:  body.SourceAuthor,
		},
		Supersedes: body.Supersedes,
	})
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	msg := fmt.Sprintf("Saved memory %s", res.Item.ID)
	if !res.Created {
		msg = fmt.Sprintf("Updated memory %s", res.Item.ID)
	}
	payload := map[string]any{
		"ok":      true,
		"id":      res.Item.ID,
		"path":    res.Path,
		"message": msg,
		"item":    res.Item,
	}
	if len(res.SimilarExisting) > 0 {
		payload["similarExisting"] = res.SimilarExisting
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *HTTPServer) handleMemoryPatch(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		CallerBotID string `json:"callerBotId,omitempty"`
		memory.PatchReq
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	callerBotID := strings.TrimSpace(body.CallerBotID)
	if callerBotID == "" {
		callerBotID = strings.TrimSpace(r.URL.Query().Get("callerBotId"))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	item, err := s.hub.MemoryPatch(ctx, callerBotID, id, body.PatchReq)
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"item": item,
	})
}

func (s *HTTPServer) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	q := r.URL.Query()
	callerBotID := strings.TrimSpace(q.Get("callerBotId"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	res, err := s.hub.MemoryForget(ctx, callerBotID, id, memory.ForgetOpts{
		HardDelete: queryBool(r, "hard"),
		Dismiss:    queryBool(r, "dismiss"),
		Reason:     q.Get("reason"),
	})
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"id":        res.ID,
		"action":    res.Action,
		"dismissed": res.Dismissed,
		"message":   fmt.Sprintf("Memory %s %s", res.ID, res.Action),
	})
}

func (s *HTTPServer) handleMemoryIndexGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	doc, err := s.hub.MemoryReadIndex(ctx, q.Get("callerBotId"), q.Get("scope"), q.Get("botId"))
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *HTTPServer) handleMemoryIndexPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope       string `json:"scope,omitempty"`
		BotID       string `json:"botId,omitempty"`
		CallerBotID string `json:"callerBotId,omitempty"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	callerBotID := strings.TrimSpace(body.CallerBotID)
	if callerBotID == "" {
		callerBotID = strings.TrimSpace(r.URL.Query().Get("callerBotId"))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	doc, err := s.hub.MemoryWriteIndex(ctx, callerBotID, body.Scope, body.BotID, body.Content)
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"path":     doc.Path,
		"content":  doc.Content,
		"lines":    doc.Lines,
		"maxLines": doc.MaxLines,
	})
}

func (s *HTTPServer) handleMemoryStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	q := r.URL.Query()
	st, err := s.hub.MemoryStatus(ctx, q.Get("callerBotId"), q.Get("botId"))
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *HTTPServer) handleMemoryDream(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BotID string `json:"botId,omitempty"`
		Force *bool  `json:"force,omitempty"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)
	force := true
	if body.Force != nil {
		force = *body.Force
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	results, err := s.hub.RunMemoryDream(ctx, body.BotID, force)
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	resp := map[string]any{
		"ok":      true,
		"results": results,
	}
	if len(results) == 1 {
		resp["botId"] = results[0].BotID
		resp["path"] = results[0].DiaryPath
		resp["summary"] = results[0].Summary
		if results[0].Error != "" {
			resp["error"] = results[0].Error
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) handleMemoryReindex(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	stats, err := s.hub.MemoryReindex(ctx)
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"docsIndexed":   stats.Indexed,
		"chunksIndexed": stats.Chunks,
		"scanned":       stats.Scanned,
		"deleted":       stats.Deleted,
	})
}

func (s *HTTPServer) handleMemoryImportMagpie(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode   string `json:"mode,omitempty"`
		BotID  string `json:"botId,omitempty"`
		DryRun bool   `json:"dryRun,omitempty"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Minute)
	defer cancel()

	res, err := s.hub.MemoryImportMagpie(ctx, memory.ImportOpts{
		Mode:   body.Mode,
		BotID:  body.BotID,
		DryRun: body.DryRun,
	})
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	resp := map[string]any{
		"ok":          true,
		"imported":    res.Imported,
		"skipped":     res.Skipped,
		"failed":      res.Failed,
		"globalCount": res.GlobalCount,
	}
	if len(res.BotCounts) > 0 {
		resp["botCounts"] = res.BotCounts
	}
	if len(res.Errors) > 0 {
		resp["errors"] = res.Errors
	}
	writeJSON(w, http.StatusOK, resp)
}
