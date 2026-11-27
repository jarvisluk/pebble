package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/memory"
	"bot-hub/internal/state"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DreamRunResult describes the outcome of running Dream consolidation for a single bot.
type DreamRunResult struct {
	BotID      string                   `json:"botId"`
	Skipped    bool                     `json:"skipped,omitempty"`
	Reason     string                   `json:"reason,omitempty"`
	Summary    string                   `json:"summary,omitempty"`
	DiaryPath  string                   `json:"diaryPath,omitempty"`
	IndexPath  string                   `json:"indexPath,omitempty"`
	IndexLines int                      `json:"indexLines,omitempty"`
	Applied    *memory.DreamApplyResult `json:"applied,omitempty"`
	Error      string                   `json:"error,omitempty"`
}

// RunMemoryDream runs Dream consolidation for one bot (if botID != "") or all configured bots.
func (h *Hub) RunMemoryDream(ctx context.Context, botID string, force bool) ([]DreamRunResult, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	botID = strings.TrimSpace(botID)
	var targets []config.BotConfig
	if botID != "" {
		b, ok := h.GetBot(botID)
		if !ok {
			return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
		}
		targets = []config.BotConfig{b}
	} else {
		targets = h.Bots()
	}

	h.lanesMu.RLock()
	memCfg := h.cfg.Memory
	h.lanesMu.RUnlock()
	dreamModel := memCfg.EffectiveDreamModel()
	minNew := memCfg.EffectiveDreamMinNewMessages()

	results := make([]DreamRunResult, 0, len(targets))
	for _, bot := range targets {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		results = append(results, h.runDreamForBot(ctx, bot, dreamModel, minNew, force))
	}
	return results, nil
}

func (h *Hub) runDreamForBot(ctx context.Context, bot config.BotConfig, dreamModel string, minNew int, force bool) DreamRunResult {
	res := DreamRunResult{BotID: bot.ID}
	h.dreamMu.Lock()
	if h.dreamActive[bot.ID] {
		h.dreamMu.Unlock()
		res.Skipped = true
		res.Reason = "dream already running"
		return res
	}
	h.dreamActive[bot.ID] = true
	h.dreamMu.Unlock()
	defer func() {
		h.dreamMu.Lock()
		delete(h.dreamActive, bot.ID)
		h.dreamMu.Unlock()
	}()

	cur := h.memory.GetDreamCursor(memory.ScopeBot, bot.ID)
	excerpts, maxRowID, lastSessID, msgCount := h.collectDreamMessages(bot.ID, cur.LastMessageRowID)
	dailyLogs, latestDaily := h.collectRecentDailyLogs(bot.ID, cur.LastDailyDate)

	if !force && msgCount < minNew {
		res.Skipped = true
		res.Reason = fmt.Sprintf("only %d new messages (min %d)", msgCount, minNew)
		return res
	}

	var existingItems []memory.Item
	if list, err := h.memory.Search(ctx, memory.SearchOpts{
		Scope:  memory.ScopeBot,
		BotID:  bot.ID,
		Status: memory.StatusActive,
		Limit:  100,
	}); err == nil && list != nil {
		existingItems = list.Items
	}
	dismissed := h.collectDismissedHints(bot.ID)

	prompt := memory.BuildDreamPrompt(memory.DreamPromptInput{
		Scope:           memory.ScopeBot,
		BotID:           bot.ID,
		ExistingItems:   existingItems,
		DailyLogs:       dailyLogs,
		SessionExcerpts: excerpts,
		DismissedHints:  dismissed,
		MaxIndexLines:   memory.BotIndexMaxLines,
	})

	turnRes, err := h.RunIsolated(ctx, bot.ID, prompt, IsolatedOptions{
		Source:         "memory_dream",
		Model:          dreamModel,
		PermissionMode: config.PermissionModePlan,
		TimeoutSec:     180,
	})
	if err != nil {
		res.Error = err.Error()
		return res
	}
	plan, err := memory.ParseDreamPlan(turnRes.Text)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	now := time.Now().UTC()
	applied, err := h.memory.ApplyDreamPlan(ctx, memory.ScopeBot, bot.ID, plan, now)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	nextCur := cur
	nextCur.LastRunAt = now.Format(time.RFC3339)
	if maxRowID > nextCur.LastMessageRowID {
		nextCur.LastMessageRowID = maxRowID
	}
	if lastSessID != "" {
		nextCur.LastSessionID = lastSessID
	}
	if latestDaily != "" {
		nextCur.LastDailyDate = latestDaily
	}
	nextCur.RunCount++
	_ = h.memory.SetDreamCursor(memory.ScopeBot, bot.ID, nextCur)

	res.Summary = plan.Summary
	res.DiaryPath = applied.DiaryPath
	res.IndexPath = applied.IndexPath
	res.IndexLines = applied.IndexLines
	res.Applied = applied
	return res
}

func (h *Hub) collectDreamMessages(botID string, sinceRowID int64) (excerpts []string, maxRowID int64, lastSessionID string, count int) {
	if h.store == nil || h.store.DB() == nil {
		return nil, sinceRowID, "", 0
	}
	rows, err := h.store.DB().Query(`
		SELECT id, session_id, role, text, created_at
		FROM messages
		WHERE bot_id = ? AND id > ? AND role IN ('user', 'assistant', 'bot_in', 'bot_out')
		ORDER BY id ASC
		LIMIT 200
	`, botID, sinceRowID)
	if err != nil {
		return nil, sinceRowID, "", 0
	}
	defer rows.Close()

	maxRowID = sinceRowID
	var lines []string
	for rows.Next() {
		var rowID int64
		var sessID, role, text, createdAt string
		if rows.Scan(&rowID, &sessID, &role, &text, &createdAt) != nil {
			continue
		}
		if rowID > maxRowID {
			maxRowID = rowID
		}
		if sessID != "" {
			lastSessionID = sessID
		}
		count++
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", createdAt, role, truncateRunes(trimmed, 400)))
	}
	if len(lines) > 0 {
		excerpts = append(excerpts, strings.Join(lines, "\n"))
	}
	return excerpts, maxRowID, lastSessionID, count
}

func (h *Hub) collectRecentDailyLogs(botID, sinceDate string) (logs []string, latestDate string) {
	if h.memory == nil {
		return nil, sinceDate
	}
	dir := filepath.Join(h.memory.WorkspacesDir(), botID, "memory", "daily")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, sinceDate
	}
	var names []string
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".md") {
			continue
		}
		names = append(names, ent.Name())
	}
	sort.Strings(names)
	if len(names) > 7 {
		names = names[len(names)-7:]
	}
	latestDate = sinceDate
	for _, name := range names {
		d := strings.TrimSuffix(name, filepath.Ext(name))
		if sinceDate != "" && d < sinceDate {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || strings.TrimSpace(string(raw)) == "" {
			continue
		}
		logs = append(logs, truncateRunes(strings.TrimSpace(string(raw)), 2000))
		if d > latestDate {
			latestDate = d
		}
	}
	return logs, latestDate
}

func (h *Hub) collectDismissedHints(botID string) []string {
	if h.store == nil || h.store.DB() == nil {
		return nil
	}
	rows, err := h.store.DB().Query(`
		SELECT summary
		FROM memory_dismissed
		WHERE (scope = 'bot' AND bot_id = ?) OR scope = 'global'
		ORDER BY dismissed_at DESC
		LIMIT 50
	`, botID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var hints []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil && strings.TrimSpace(s) != "" {
			hints = append(hints, strings.TrimSpace(s))
		}
	}
	return hints
}

func (h *Hub) triggerSessionFlushAsync(botID, sessionID, reason string) {
	if h.memory == nil || h.store == nil || !h.memoryEnabled() {
		return
	}
	h.closeMu.Lock()
	if h.closed || h.bgCtx == nil {
		h.closeMu.Unlock()
		return
	}
	h.bgWG.Add(1)
	bgCtx := h.bgCtx
	h.closeMu.Unlock()

	go func() {
		defer h.bgWG.Done()
		ctx, cancel := context.WithTimeout(bgCtx, 60*time.Second)
		defer cancel()

		recalled, err := h.store.RecallSession(state.RecallOptions{
			SessionID: sessionID,
			BotID:     botID,
			Tail:      24,
			MaxChars:  1000,
		})
		if err != nil || recalled == nil || len(recalled.Messages) == 0 {
			return
		}
		var lines []string
		for _, m := range recalled.Messages {
			if m.Role == "system" {
				continue
			}
			txt := strings.TrimSpace(m.Text)
			if txt == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s: %s", m.Role, truncateRunes(txt, 500)))
		}
		if len(lines) == 0 {
			return
		}

		h.lanesMu.RLock()
		dreamModel := h.cfg.Memory.EffectiveDreamModel()
		h.lanesMu.RUnlock()

		prompt := memory.BuildFlushPrompt(botID, reason, strings.Join(lines, "\n"))
		turnRes, err := h.RunIsolated(ctx, botID, prompt, IsolatedOptions{
			Source:         "memory_flush",
			Model:          dreamModel,
			PermissionMode: config.PermissionModePlan,
			TimeoutSec:     60,
		})
		if err != nil || turnRes == nil {
			if err != nil && ctx.Err() == nil {
				log.Printf("[memory] session flush for %s (%s) failed: %v", botID, reason, err)
			}
			return
		}
		if _, err := h.memory.ApplyFlushNote(ctx, memory.ScopeBot, botID, turnRes.Text, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("[memory] apply flush note for %s (%s) failed: %v", botID, reason, err)
		}
	}()
}
