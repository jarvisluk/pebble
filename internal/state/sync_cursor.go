package state

import (
	"database/sql"
	"strings"
)

// SyncMessageRow is the portable row representation of a message in SQLite.
type SyncMessageRow struct {
	RowID        int64   `json:"rowId"`
	MsgUID       string  `json:"msgUid"`
	SessionID    string  `json:"sessionId,omitempty"`
	BotID        string  `json:"botId"`
	Turn         int     `json:"turn,omitempty"`
	Role         string  `json:"role"`
	Sender       string  `json:"sender,omitempty"`
	Recipient    string  `json:"recipient,omitempty"`
	Text         string  `json:"text"`
	MediaJSON    string  `json:"mediaJson,omitempty"`
	StageJSON    string  `json:"stageJson,omitempty"`
	ToolsJSON    string  `json:"toolsJson,omitempty"`
	DurationMs   int64   `json:"durationMs,omitempty"`
	CostUSD      float64 `json:"costUsd,omitempty"`
	InputTokens  int     `json:"inputTokens,omitempty"`
	OutputTokens int     `json:"outputTokens,omitempty"`
	CreatedAt    string  `json:"createdAt"`
}

// SyncDelta carries cursor-based increments for messages and routine_runs.
type SyncDelta struct {
	Messages      []SyncMessageRow `json:"messages,omitempty"`
	NextMsgCursor int64            `json:"nextMsgCursor"`
	RoutineRuns   []RoutineRun     `json:"routineRuns,omitempty"`
	NextRunCursor string           `json:"nextRunCursor,omitempty"`
}

// GetMeta reads a string value from the SQLite meta table.
func (s *Store) GetMeta(key string) string {
	if s == nil || s.db == nil {
		return ""
	}
	var val string
	if err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&val); err != nil {
		return ""
	}
	return val
}

// SetMeta upserts a key/value pair into the SQLite meta table.
func (s *Store) SetMeta(key, value string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// ExportMessagesSince returns up to limit messages with id > afterRowID ordered ascending.
func (s *Store) ExportMessagesSince(afterRowID int64, limit int) ([]SyncMessageRow, int64, error) {
	if s == nil || s.db == nil {
		return nil, afterRowID, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT id, msg_uid, session_id, bot_id, turn, role, sender, recipient,
		       text, media_json, stage_json, tools_json, duration_ms, cost_usd,
		       input_tokens, output_tokens, created_at
		FROM messages
		WHERE id > ?
		ORDER BY id ASC
		LIMIT ?
	`, afterRowID, limit)
	if err != nil {
		return nil, afterRowID, err
	}
	defer rows.Close()

	var out []SyncMessageRow
	next := afterRowID
	for rows.Next() {
		var r SyncMessageRow
		if err := rows.Scan(
			&r.RowID, &r.MsgUID, &r.SessionID, &r.BotID, &r.Turn, &r.Role,
			&r.Sender, &r.Recipient, &r.Text, &r.MediaJSON, &r.StageJSON, &r.ToolsJSON,
			&r.DurationMs, &r.CostUSD, &r.InputTokens, &r.OutputTokens, &r.CreatedAt,
		); err != nil {
			return nil, afterRowID, err
		}
		if r.RowID > next {
			next = r.RowID
		}
		out = append(out, r)
	}
	return out, next, rows.Err()
}

// ImportMessages idempotently inserts synced messages keyed by msg_uid.
func (s *Store) ImportMessages(rows []SyncMessageRow) (int, error) {
	if s == nil || s.db == nil || len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO messages(
			msg_uid, session_id, bot_id, turn, role, sender, recipient,
			text, media_json, stage_json, tools_json, duration_ms, cost_usd,
			input_tokens, output_tokens, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for _, r := range rows {
		uid := strings.TrimSpace(r.MsgUID)
		if uid == "" || strings.TrimSpace(r.BotID) == "" {
			continue
		}
		mediaJSON := r.MediaJSON
		if mediaJSON == "" {
			mediaJSON = "[]"
		}
		toolsJSON := r.ToolsJSON
		if toolsJSON == "" {
			toolsJSON = "[]"
		}
		res, err := stmt.Exec(
			uid, r.SessionID, r.BotID, r.Turn, r.Role, r.Sender, r.Recipient,
			r.Text, mediaJSON, r.StageJSON, toolsJSON, r.DurationMs, r.CostUSD,
			r.InputTokens, r.OutputTokens, r.CreatedAt,
		)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			inserted += int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// ExportRoutineRunsSince returns up to limit routine runs with started_at > afterStartedAt.
func (s *Store) ExportRoutineRunsSince(afterStartedAt string, limit int) ([]RoutineRun, string, error) {
	if s == nil || s.db == nil {
		return nil, afterStartedAt, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT `+routineRunSelectCols+`
		FROM routine_runs
		WHERE started_at > ?
		ORDER BY started_at ASC
		LIMIT ?
	`, afterStartedAt, limit)
	if err != nil {
		return nil, afterStartedAt, err
	}
	defer rows.Close()

	var out []RoutineRun
	next := afterStartedAt
	for rows.Next() {
		r, err := scanRoutineRunRow(rows)
		if err != nil {
			return nil, afterStartedAt, err
		}
		if r.StartedAt > next {
			next = r.StartedAt
		}
		out = append(out, r)
	}
	return out, next, rows.Err()
}

// ImportRoutineRuns idempotently inserts synced routine runs, deduplicating by
// primary key id and optional non-empty slot_key.
func (s *Store) ImportRoutineRuns(runs []RoutineRun) (int, error) {
	if s == nil || s.db == nil || len(runs) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	checkSlotStmt, err := tx.Prepare(`SELECT 1 FROM routine_runs WHERE slot_key = ? LIMIT 1`)
	if err != nil {
		return 0, err
	}
	defer checkSlotStmt.Close()

	insStmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO routine_runs(` + routineRunSelectCols + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer insStmt.Close()

	inserted := 0
	for _, r := range runs {
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.BotID) == "" {
			continue
		}
		if sk := strings.TrimSpace(r.SlotKey); sk != "" {
			var dummy int
			if err := checkSlotStmt.QueryRow(sk).Scan(&dummy); err == nil {
				continue
			} else if err != sql.ErrNoRows {
				return 0, err
			}
		}
		res, err := insStmt.Exec(
			r.ID, r.BotID, r.RoutineID, r.RoutineName, r.Trigger, r.Status, r.Delivery, r.Model,
			r.Output, r.Error, r.DurationMs, r.CostUSD, r.InputTokens, r.OutputTokens,
			r.StartedAt, r.FinishedAt, r.InjectedAt, r.SlotKey,
		)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			inserted += int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// HasRoutineSlotKey reports whether a routine run with slotKey already exists.
func (s *Store) HasRoutineSlotKey(slotKey string) bool {
	if s == nil || s.db == nil || strings.TrimSpace(slotKey) == "" {
		return false
	}
	sk := strings.TrimSpace(slotKey)
	var dummy int
	if err := s.db.QueryRow(`SELECT 1 FROM routine_runs WHERE slot_key = ? LIMIT 1`, sk).Scan(&dummy); err == nil {
		return true
	}
	return s.db.QueryRow(`SELECT 1 FROM bot_tasks WHERE slot_key = ? LIMIT 1`, sk).Scan(&dummy) == nil
}

// RecentMessagesForHandoff returns up to limit recent user/assistant messages for botID in chronological order.
func (s *Store) RecentMessagesForHandoff(botID string, limit int) ([]SyncMessageRow, error) {
	if s == nil || s.db == nil || strings.TrimSpace(botID) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 8
	}
	rows, err := s.db.Query(`
		SELECT id, msg_uid, session_id, bot_id, turn, role, sender, recipient,
		       text, media_json, tools_json, duration_ms, cost_usd,
		       input_tokens, output_tokens, created_at
		FROM messages
		WHERE bot_id = ? AND role IN ('user', 'assistant') AND text != ''
		ORDER BY id DESC
		LIMIT ?
	`, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var desc []SyncMessageRow
	for rows.Next() {
		var r SyncMessageRow
		if err := rows.Scan(
			&r.RowID, &r.MsgUID, &r.SessionID, &r.BotID, &r.Turn, &r.Role,
			&r.Sender, &r.Recipient, &r.Text, &r.MediaJSON, &r.ToolsJSON,
			&r.DurationMs, &r.CostUSD, &r.InputTokens, &r.OutputTokens, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		desc = append(desc, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(desc)-1; i < j; i, j = i+1, j-1 {
		desc[i], desc[j] = desc[j], desc[i]
	}
	return desc, nil
}
