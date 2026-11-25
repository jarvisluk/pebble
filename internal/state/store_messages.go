package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const messageSelectCols = `
  msg_uid, session_id, bot_id, turn, role, sender, recipient,
  text, media_json, tools_json, duration_ms, cost_usd, input_tokens, output_tokens, created_at
`

func (s *Store) AppendMessage(msg ChatMessage) (ChatMessage, error) {
	if msg.ID == "" {
		msg.ID = s.newMessageUID()
	}
	if msg.CreatedAt == "" {
		msg.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	mediaJSON := "[]"
	if len(msg.Media) > 0 {
		if b, err := json.Marshal(msg.Media); err == nil {
			mediaJSON = string(b)
		}
	}
	toolsJSON := "[]"
	if len(msg.Tools) > 0 {
		if b, err := json.Marshal(msg.Tools); err == nil {
			toolsJSON = string(b)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return msg, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO messages(msg_uid, session_id, bot_id, turn, role, sender, recipient, text, media_json, tools_json, duration_ms, cost_usd, input_tokens, output_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, msg.ID, msg.SessionID, msg.BotID, msg.Turn, msg.Role, msg.From, msg.To, msg.Text, mediaJSON, toolsJSON, msg.DurationMs, msg.CostUSD, msg.InputTokens, msg.OutputTokens, msg.CreatedAt); err != nil {
		return msg, err
	}

	if msg.SessionID != "" && (msg.Role == "user" || (msg.Role == "bot_in" && msg.To == "")) {
		summary := firstLineSummary(msg.Text, 120)
		if summary != "" {
			_, _ = tx.Exec(`
				UPDATE sessions
				SET title = CASE WHEN title = '' THEN ? ELSE title END,
				    last_active_at = ?
				WHERE id = ?
			`, summary, msg.CreatedAt, msg.SessionID)
		}
	}

	if err := tx.Commit(); err != nil {
		return msg, err
	}
	return msg, nil
}

// ReassignMessageSession updates a queued user/bot_in message's session_id and turn
// when its actual execution moves to a different session (e.g. mid-queue reset or error retry).
func (s *Store) ReassignMessageSession(msgID, sessionID string, turn int) error {
	if msgID == "" || sessionID == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var text, createdAt string
	err = tx.QueryRow(`SELECT text, created_at FROM messages WHERE msg_uid = ? LIMIT 1`, msgID).Scan(&text, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := tx.Exec(`UPDATE messages SET session_id = ?, turn = ? WHERE msg_uid = ?`, sessionID, turn, msgID); err != nil {
		return err
	}
	if summary := firstLineSummary(text, 120); summary != "" {
		_, _ = tx.Exec(`
			UPDATE sessions
			SET title = CASE WHEN title = '' THEN ? ELSE title END,
			    last_active_at = ?
			WHERE id = ?
		`, summary, createdAt, sessionID)
	}
	return tx.Commit()
}

// UpdateMessageMeta updates the turn-level execution metadata (tools, duration,
// cost, and token counts) on an already-persisted message (e.g. sent via send_message).
func (s *Store) UpdateMessageMeta(msgID string, tools []string, durationMs int64, costUSD float64, inTok, outTok int) (ChatMessage, bool, error) {
	if msgID == "" {
		return ChatMessage{}, false, nil
	}
	toolsJSON := "[]"
	if len(tools) > 0 {
		if b, err := json.Marshal(tools); err == nil {
			toolsJSON = string(b)
		}
	}
	res, err := s.db.Exec(`
		UPDATE messages
		SET tools_json = ?, duration_ms = ?, cost_usd = ?, input_tokens = ?, output_tokens = ?
		WHERE msg_uid = ?
	`, toolsJSON, durationMs, costUSD, inTok, outTok, msgID)
	if err != nil {
		return ChatMessage{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ChatMessage{}, false, err
	}
	if affected == 0 {
		return ChatMessage{}, false, nil
	}
	row := s.db.QueryRow(`SELECT `+messageSelectCols+` FROM messages WHERE msg_uid = ? LIMIT 1`, msgID)
	msg, err := scanMessageRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ChatMessage{}, false, nil
		}
		return ChatMessage{}, false, err
	}
	return msg, true, nil
}

func (s *Store) ListMessages(botID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(`
		SELECT `+messageSelectCols+`
		FROM (
			SELECT `+messageSelectCols+`, id AS _row_id
			FROM messages
			WHERE bot_id = ? AND role != 'internal'
			ORDER BY id DESC
			LIMIT ?
		) sub
		ORDER BY _row_id ASC
	`, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ChatMessage{}
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ListCollabMessages(botID, peerID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	peerTag := "bot:" + peerID
	rows, err := s.db.Query(`
		SELECT `+messageSelectCols+`
		FROM (
			SELECT `+messageSelectCols+`, id AS _row_id
			FROM messages
			WHERE bot_id = ? AND role IN ('bot_out', 'bot_in') AND (sender = ? OR recipient = ?)
			ORDER BY id DESC
			LIMIT ?
		) sub
		ORDER BY _row_id ASC
	`, botID, peerTag, peerTag, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ChatMessage{}
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
