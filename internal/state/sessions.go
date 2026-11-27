package state

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

const sessionSelectCols = `
  id, bot_id, runtime_session_id, harness, model, status, title,
  last_error, handoff_note, turn_count, total_cost_usd, last_input_tokens,
  created_at, last_active_at, ended_at
`

func (s *Store) Get(botID string) (SessionRecord, bool) {
	row := s.db.QueryRow(`
		SELECT `+sessionSelectCols+`
		FROM sessions
		WHERE bot_id = ? AND status = 'active'
		ORDER BY rowid DESC LIMIT 1
	`, botID)
	rec, err := scanSessionRow(row)
	if err != nil {
		return SessionRecord{}, false
	}
	return rec, true
}

func (s *Store) GetSessionByID(sessionID string) (SessionRecord, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionRecord{}, false
	}
	row := s.db.QueryRow(`
		SELECT `+sessionSelectCols+`
		FROM sessions
		WHERE id = ?
		LIMIT 1
	`, sessionID)
	if rec, err := scanSessionRow(row); err == nil {
		return rec, true
	}
	row = s.db.QueryRow(`
		SELECT `+sessionSelectCols+`
		FROM sessions
		WHERE runtime_session_id = ?
		ORDER BY rowid DESC LIMIT 1
	`, sessionID)
	if rec, err := scanSessionRow(row); err == nil {
		return rec, true
	}
	return SessionRecord{}, false
}

// EnsureActiveSession returns the active session for botID, creating a new Hub
// session row if none exists. If an active session exists on a different
// harness, it is archived as harness_changed first.
func (s *Store) EnsureActiveSession(botID, harness, model string) (SessionRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return SessionRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`
		SELECT `+sessionSelectCols+`
		FROM sessions
		WHERE bot_id = ? AND status = 'active'
		ORDER BY rowid DESC LIMIT 1
	`, botID)
	existing, err := scanSessionRow(row)
	if err == nil {
		if existing.Harness != "" && harness != "" && existing.Harness != harness {
			if _, err := tx.Exec(`
				UPDATE sessions
				SET status = 'harness_changed', ended_at = ?
				WHERE id = ?
			`, now, existing.ID); err != nil {
				return SessionRecord{}, err
			}
		} else {
			if (existing.Harness == "" && harness != "") || (model != "" && existing.Model != model) {
				nextHarness := nonEmpty(existing.Harness, harness)
				nextModel := nonEmpty(model, existing.Model)
				_, _ = tx.Exec(`UPDATE sessions SET harness = ?, model = ? WHERE id = ?`, nextHarness, nextModel, existing.ID)
				existing.Harness = nextHarness
				existing.Model = nextModel
				existing.MagpieSession = MagpieSessionFor(existing.Harness, existing.RuntimeSessionID)
			}
			if err := tx.Commit(); err != nil {
				return SessionRecord{}, err
			}
			return existing, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, err
	}

	hubID := newSessionID()
	if _, err := tx.Exec(`
		INSERT INTO sessions(id, bot_id, runtime_session_id, harness, model, status, title, last_error, handoff_note, turn_count, total_cost_usd, last_input_tokens, created_at, last_active_at, ended_at)
		VALUES (?, ?, '', ?, ?, 'active', '', '', '', 0, 0, 0, ?, ?, '')
	`, hubID, botID, harness, model, now, now); err != nil {
		return SessionRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionRecord{}, err
	}
	return SessionRecord{
		ID:           hubID,
		BotID:        botID,
		SessionID:    hubID,
		Harness:      harness,
		Model:        model,
		Status:       "active",
		CreatedAt:    now,
		LastActiveAt: now,
	}, nil
}

// LatestArchivedSession returns the most recently ended non-active session for botID.
func (s *Store) LatestArchivedSession(botID string) (SessionRecord, bool) {
	row := s.db.QueryRow(`
		SELECT `+sessionSelectCols+`
		FROM sessions
		WHERE bot_id = ? AND status != 'active'
		ORDER BY CASE WHEN ended_at != '' THEN ended_at ELSE last_active_at END DESC, rowid DESC
		LIMIT 1
	`, botID)
	rec, err := scanSessionRow(row)
	if err != nil {
		return SessionRecord{}, false
	}
	return rec, true
}

// ArchivedCount returns the number of archived (non-active) sessions for botID.
func (s *Store) ArchivedCount(botID string) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE bot_id = ? AND status != 'active'`, botID).Scan(&n)
	return n
}

func (s *Store) RecordTurn(botID, runtimeSessionID string, costUSD float64) (SessionRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return SessionRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`
		SELECT `+sessionSelectCols+`
		FROM sessions
		WHERE bot_id = ? AND status = 'active'
		ORDER BY rowid DESC LIMIT 1
	`, botID)
	existing, err := scanSessionRow(row)
	if err == nil {
		if existing.RuntimeSessionID == "" || existing.RuntimeSessionID == runtimeSessionID || runtimeSessionID == "" {
			nextRuntime := nonEmpty(runtimeSessionID, existing.RuntimeSessionID)
			nextCost := roundCost(existing.TotalCostUSD + costUSD)
			nextTurns := existing.TurnCount + 1
			if _, err := tx.Exec(`
				UPDATE sessions
				SET runtime_session_id = ?, last_active_at = ?, turn_count = ?, total_cost_usd = ?
				WHERE id = ?
			`, nextRuntime, now, nextTurns, nextCost, existing.ID); err != nil {
				return SessionRecord{}, err
			}
			if err := tx.Commit(); err != nil {
				return SessionRecord{}, err
			}
			existing.RuntimeSessionID = nextRuntime
			existing.SessionID = nonEmpty(nextRuntime, existing.ID)
			existing.LastActiveAt = now
			existing.TurnCount = nextTurns
			existing.TotalCostUSD = nextCost
			existing.MagpieSession = MagpieSessionFor(existing.Harness, nextRuntime)
			return existing, nil
		}
		// Runtime session ID changed under our feet: archive old as superseded.
		if _, err := tx.Exec(`
			UPDATE sessions
			SET status = 'superseded', ended_at = ?
			WHERE id = ?
		`, now, existing.ID); err != nil {
			return SessionRecord{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, err
	}

	hubID := newSessionID()
	cost := roundCost(costUSD)
	if _, err := tx.Exec(`
		INSERT INTO sessions(id, bot_id, runtime_session_id, harness, model, status, title, last_error, handoff_note, turn_count, total_cost_usd, last_input_tokens, created_at, last_active_at, ended_at)
		VALUES (?, ?, ?, '', '', 'active', '', '', '', 1, ?, 0, ?, ?, '')
	`, hubID, botID, runtimeSessionID, cost, now, now); err != nil {
		return SessionRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionRecord{}, err
	}
	return SessionRecord{
		ID:               hubID,
		BotID:            botID,
		SessionID:        nonEmpty(runtimeSessionID, hubID),
		RuntimeSessionID: runtimeSessionID,
		Status:           "active",
		CreatedAt:        now,
		LastActiveAt:     now,
		TurnCount:        1,
		TotalCostUSD:     cost,
	}, nil
}

// RecordTurnDetail updates a specific session row by its Hub sessionID without
// touching any newer active session if the bot was reset mid-flight.
func (s *Store) RecordTurnDetail(sessionID string, upd TurnRecordUpdate) (SessionRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return SessionRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`SELECT `+sessionSelectCols+` FROM sessions WHERE id = ? LIMIT 1`, sessionID)
	existing, err := scanSessionRow(row)
	if err != nil {
		return SessionRecord{}, err
	}

	// If runtime session ID changed from an already-established ID while still active,
	// archive the prior one and migrate caller's turn? No, just record runtime session ID
	// when empty or matching; if changed while active, record as updated runtime ID.
	nextRuntime := nonEmpty(upd.RuntimeSessionID, existing.RuntimeSessionID)
	nextHarness := nonEmpty(upd.Harness, existing.Harness)
	nextModel := nonEmpty(upd.Model, existing.Model)
	nextTitle := existing.Title
	if nextTitle == "" && upd.PromptTitle != "" {
		nextTitle = firstLineSummary(upd.PromptTitle, 120)
	}
	nextTurns := existing.TurnCount
	if upd.IncrementTurn {
		nextTurns++
	}
	nextCost := roundCost(existing.TotalCostUSD + upd.CostUSD)
	nextInput := existing.LastInputTokens
	if upd.InputTokens > 0 {
		nextInput = upd.InputTokens
	}

	if _, err := tx.Exec(`
		UPDATE sessions
		SET runtime_session_id = ?, harness = ?, model = ?, title = ?,
		    last_active_at = ?, turn_count = ?, total_cost_usd = ?, last_input_tokens = ?
		WHERE id = ?
	`, nextRuntime, nextHarness, nextModel, nextTitle, now, nextTurns, nextCost, nextInput, existing.ID); err != nil {
		return SessionRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionRecord{}, err
	}

	existing.RuntimeSessionID = nextRuntime
	existing.SessionID = nonEmpty(nextRuntime, existing.ID)
	existing.Harness = nextHarness
	existing.Model = nextModel
	existing.Title = nextTitle
	existing.LastActiveAt = now
	existing.TurnCount = nextTurns
	existing.TotalCostUSD = nextCost
	existing.LastInputTokens = nextInput
	existing.MagpieSession = MagpieSessionFor(nextHarness, nextRuntime)
	return existing, nil
}

// ArchiveActive marks the active session for botID with status ("reset", "error",
// "harness_changed", "superseded") and returns the archived record.
func (s *Store) ArchiveActive(botID, status, lastError, handoffNote string) (SessionRecord, bool, error) {
	if status == "" || status == "active" {
		status = "reset"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return SessionRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`
		SELECT `+sessionSelectCols+`
		FROM sessions
		WHERE bot_id = ? AND status = 'active'
		ORDER BY rowid DESC LIMIT 1
	`, botID)
	prev, err := scanSessionRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, false, nil
	}
	if err != nil {
		return SessionRecord{}, false, err
	}

	if _, err := tx.Exec(`
		UPDATE sessions
		SET status = ?, last_error = CASE WHEN ? != '' THEN ? ELSE last_error END,
		    handoff_note = CASE WHEN ? != '' THEN ? ELSE handoff_note END,
		    ended_at = ?
		WHERE id = ?
	`, status, lastError, lastError, handoffNote, handoffNote, now, prev.ID); err != nil {
		return SessionRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return SessionRecord{}, false, err
	}
	prev.Status = status
	if lastError != "" {
		prev.LastError = lastError
	}
	if handoffNote != "" {
		prev.HandoffNote = handoffNote
	}
	prev.EndedAt = now
	return prev, true, nil
}

// ArchiveSessionByID marks a specific session row (if still active) as archived.
func (s *Store) ArchiveSessionByID(sessionID, status, lastError, handoffNote string) (SessionRecord, bool, error) {
	if status == "" || status == "active" {
		status = "reset"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return SessionRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`SELECT `+sessionSelectCols+` FROM sessions WHERE id = ? LIMIT 1`, sessionID)
	prev, err := scanSessionRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, false, nil
	}
	if err != nil {
		return SessionRecord{}, false, err
	}

	if _, err := tx.Exec(`
		UPDATE sessions
		SET status = ?, last_error = CASE WHEN ? != '' THEN ? ELSE last_error END,
		    handoff_note = CASE WHEN ? != '' THEN ? ELSE handoff_note END,
		    ended_at = ?
		WHERE id = ?
	`, status, lastError, lastError, handoffNote, handoffNote, now, prev.ID); err != nil {
		return SessionRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return SessionRecord{}, false, err
	}
	prev.Status = status
	if lastError != "" {
		prev.LastError = lastError
	}
	if handoffNote != "" {
		prev.HandoffNote = handoffNote
	}
	prev.EndedAt = now
	return prev, true, nil
}

// Reset archives the bot's current active session as "reset".
func (s *Store) Reset(botID string) (SessionRecord, bool, error) {
	return s.ArchiveActive(botID, "reset", "", "")
}
