package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type SessionRecord struct {
	ID               string  `json:"id,omitempty"`
	BotID            string  `json:"botId,omitempty"`
	SessionID        string  `json:"sessionId"`
	RuntimeSessionID string  `json:"runtimeSessionId,omitempty"`
	Harness          string  `json:"harness,omitempty"`
	Model            string  `json:"model,omitempty"`
	Status           string  `json:"status,omitempty"`
	Title            string  `json:"title,omitempty"`
	LastError        string  `json:"lastError,omitempty"`
	HandoffNote      string  `json:"handoffNote,omitempty"`
	CreatedAt        string  `json:"createdAt"`
	LastActiveAt     string  `json:"lastActiveAt"`
	EndedAt          string  `json:"endedAt,omitempty"`
	TurnCount        int     `json:"turnCount"`
	TotalCostUSD     float64 `json:"totalCostUsd"`
	LastInputTokens  int     `json:"lastInputTokens,omitempty"`
	MagpieSession    string  `json:"magpieSession,omitempty"`
}

type SearchSnippet struct {
	MessageID string `json:"messageId"`
	Turn      int    `json:"turn"`
	Role      string `json:"role"`
	Excerpt   string `json:"excerpt"`
	CreatedAt string `json:"createdAt"`
}

type SessionSummary struct {
	SessionID        string          `json:"sessionId"`
	RuntimeSessionID string          `json:"runtimeSessionId,omitempty"`
	BotID            string          `json:"botId"`
	Harness          string          `json:"harness,omitempty"`
	Model            string          `json:"model,omitempty"`
	Status           string          `json:"status"`
	Title            string          `json:"title,omitempty"`
	LastError        string          `json:"lastError,omitempty"`
	HandoffNote      string          `json:"handoffNote,omitempty"`
	TurnCount        int             `json:"turnCount"`
	TotalCostUSD     float64         `json:"totalCostUsd"`
	LastInputTokens  int             `json:"lastInputTokens,omitempty"`
	CreatedAt        string          `json:"createdAt"`
	LastActiveAt     string          `json:"lastActiveAt"`
	EndedAt          string          `json:"endedAt,omitempty"`
	MagpieSession    string          `json:"magpieSession,omitempty"`
	Snippets         []SearchSnippet `json:"snippets,omitempty"`
}

type SessionListOptions struct {
	BotID          string
	ExcludedBotIDs []string
	Query          string
	Limit          int
	IncludeActive  bool
}

type RecallOptions struct {
	SessionID string
	BotID     string
	Tail      int
	Offset    int
	Limit     int
	Query     string
	MaxChars  int
	MessageID string
}

type RecallResult struct {
	Session    SessionSummary `json:"session"`
	TotalCount int            `json:"totalCount"`
	Returned   int            `json:"returned"`
	Offset     int            `json:"offset"`
	HasMore    bool           `json:"hasMore"`
	Messages   []ChatMessage  `json:"messages"`
}

type TurnRecordUpdate struct {
	RuntimeSessionID string
	Harness          string
	Model            string
	PromptTitle      string
	CostUSD          float64
	InputTokens      int
	IncrementTurn    bool
}

type RoutineRun struct {
	ID           string  `json:"id"`
	BotID        string  `json:"botId"`
	RoutineID    string  `json:"routineId"`
	RoutineName  string  `json:"routineName,omitempty"`
	Trigger      string  `json:"trigger"`  // "schedule" | "manual"
	Status       string  `json:"status"`   // "ok" | "silent" | "error"
	Delivery     string  `json:"delivery"` // "receipt" | "notify" | "none"
	Model        string  `json:"model,omitempty"`
	Output       string  `json:"output,omitempty"`
	Error        string  `json:"error,omitempty"`
	DurationMs   int64   `json:"durationMs,omitempty"`
	CostUSD      float64 `json:"costUsd,omitempty"`
	InputTokens  int     `json:"inputTokens,omitempty"`
	OutputTokens int     `json:"outputTokens,omitempty"`
	StartedAt    string  `json:"startedAt"`
	FinishedAt   string  `json:"finishedAt"`
	InjectedAt   string  `json:"injectedAt,omitempty"`
}

type Store struct {
	db                 *sql.DB
	dbPath             string
	legacySessionsPath string
	legacyMessagesDir  string
	seq                atomic.Uint64

	closeMu sync.Mutex
	closed  bool
}

const sessionSelectCols = `
  id, bot_id, runtime_session_id, harness, model, status, title,
  last_error, handoff_note, turn_count, total_cost_usd, last_input_tokens,
  created_at, last_active_at, ended_at
`

const messageSelectCols = `
  msg_uid, session_id, bot_id, turn, role, sender, recipient,
  text, media_json, tools_json, duration_ms, cost_usd, input_tokens, output_tokens, created_at
`

// NewStore opens the SQLite state store (hub.db next to filePath when a legacy
// .json path is passed) and imports legacy sessions.json/messages/*.jsonl once.
func NewStore(filePath string) (*Store, error) {
	dbPath := filePath
	var legacySessions, legacyMsgs string
	if strings.HasSuffix(strings.ToLower(filePath), ".json") {
		dir := filepath.Dir(filePath)
		dbPath = filepath.Join(dir, "hub.db")
		legacySessions = filePath
		legacyMsgs = filepath.Join(dir, "messages")
	} else if filePath == "" {
		dbPath = ":memory:"
	}

	db, err := openSQLite(dbPath)
	if err != nil {
		return nil, err
	}

	s := &Store{
		db:                 db,
		dbPath:             dbPath,
		legacySessionsPath: legacySessions,
		legacyMessagesDir:  legacyMsgs,
	}
	if err := s.migrateLegacyIfNeeded(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate legacy state: %w", err)
	}
	return s, nil
}

// DBPath returns the path to the underlying SQLite database file.
func (s *Store) DBPath() string {
	return s.dbPath
}

// Close closes the underlying SQLite database handle.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

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

func (s *Store) ListSessions(opts SessionListOptions) ([]SessionSummary, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	where := []string{"1=1"}
	var args []any

	if !opts.IncludeActive {
		// Still include active sessions when specifically browsing unless caller filters;
		// wait: default in ListSessions includes both active and archived unless IncludeActive is explicitly false?
		// Actually users/bots may want to see archived + active (with status clearly labeled).
		// Keep IncludeActive meaning "include active sessions too" if true, or let's include active by default!
	}

	botFilter := strings.TrimSpace(opts.BotID)
	if botFilter != "" && botFilter != "*" {
		where = append(where, "s.bot_id = ?")
		args = append(args, botFilter)
	}
	if len(opts.ExcludedBotIDs) > 0 {
		placeholders := make([]string, len(opts.ExcludedBotIDs))
		for i, id := range opts.ExcludedBotIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		where = append(where, "s.bot_id NOT IN ("+strings.Join(placeholders, ",")+")")
	}

	query := strings.TrimSpace(opts.Query)
	if query == "" {
		sqlStr := `
			SELECT ` + prefixCols(sessionSelectCols, "s.") + `
			FROM sessions s
			WHERE ` + strings.Join(where, " AND ") + `
			ORDER BY CASE WHEN s.status = 'active' THEN 1 ELSE 0 END ASC,
			         CASE WHEN s.ended_at != '' THEN s.ended_at ELSE s.last_active_at END DESC,
			         s.rowid DESC
			LIMIT ?
		`
		args = append(args, limit)
		rows, err := s.db.Query(sqlStr, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var out []SessionSummary
		for rows.Next() {
			rec, err := scanSessionRow(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, toSummary(rec))
		}
		if out == nil {
			out = []SessionSummary{}
		}
		return out, rows.Err()
	}

	// Query mode: match session metadata (title/last_error/handoff_note) OR message text via FTS5 (with short-query LIKE fallback).
	matchedMsgCond, msgArgs := s.messageTextCondition(query)
	metaLike := "%" + escapeLike(strings.ToLower(query)) + "%"

	var fullArgs []any
	fullArgs = append(fullArgs, msgArgs...)
	fullArgs = append(fullArgs, whereArgsForSubquery(where, args)...)
	fullArgs = append(fullArgs, metaLike, metaLike, metaLike, limit)

	// Build clean query avoiding CTE column aliasing pitfalls.
	_ = fullArgs
	var qArgs []any
	qArgs = append(qArgs, args...)
	qArgs = append(qArgs, metaLike, metaLike, metaLike)
	qArgs = append(qArgs, msgArgs...)
	qArgs = append(qArgs, limit)

	sqlStr := `
		SELECT DISTINCT ` + prefixCols(sessionSelectCols, "s.") + `
		FROM sessions s
		WHERE ` + strings.Join(where, " AND ") + `
		  AND (
		    LOWER(s.title) LIKE ? ESCAPE '\'
		    OR LOWER(s.last_error) LIKE ? ESCAPE '\'
		    OR LOWER(s.handoff_note) LIKE ? ESCAPE '\'
		    OR EXISTS (
		      SELECT 1 FROM messages m
		      WHERE m.session_id = s.id AND ` + matchedMsgCond + `
		    )
		  )
		ORDER BY s.last_active_at DESC, s.rowid DESC
		LIMIT ?
	`
	rows, err := s.db.Query(sqlStr, qArgs...)
	if err != nil && canUseTrigramFTS(query) {
		// Fallback to LIKE if FTS5 rejects unusual punctuation.
		matchedMsgCond = `LOWER(m.text) LIKE ? ESCAPE '\'`
		qArgs = nil
		qArgs = append(qArgs, args...)
		qArgs = append(qArgs, metaLike, metaLike, metaLike, metaLike, limit)
		sqlStr = `
			SELECT DISTINCT ` + prefixCols(sessionSelectCols, "s.") + `
			FROM sessions s
			WHERE ` + strings.Join(where, " AND ") + `
			  AND (
			    LOWER(s.title) LIKE ? ESCAPE '\'
			    OR LOWER(s.last_error) LIKE ? ESCAPE '\'
			    OR LOWER(s.handoff_note) LIKE ? ESCAPE '\'
			    OR EXISTS (
			      SELECT 1 FROM messages m
			      WHERE m.session_id = s.id AND ` + matchedMsgCond + `
			    )
			  )
			ORDER BY s.last_active_at DESC, s.rowid DESC
			LIMIT ?
		`
		rows, err = s.db.Query(sqlStr, qArgs...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	for rows.Next() {
		rec, err := scanSessionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, toSummary(rec))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		return []SessionSummary{}, nil
	}

	for i := range out {
		snips, _ := s.sessionSnippets(out[i].SessionID, query, 3)
		out[i].Snippets = snips
	}
	return out, nil
}

func whereArgsForSubquery(_ []string, args []any) []any {
	return args
}

func prefixCols(cols, prefix string) string {
	parts := strings.Split(cols, ",")
	for i := range parts {
		trimmed := strings.TrimSpace(parts[i])
		if trimmed != "" {
			parts[i] = prefix + trimmed
		}
	}
	return strings.Join(parts, ", ")
}

func (s *Store) messageTextCondition(query string) (string, []any) {
	if canUseTrigramFTS(query) {
		ftsQ := buildFTSQuery(query)
		if ftsQ != "" {
			return `m.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)`, []any{ftsQ}
		}
	}
	like := "%" + escapeLike(strings.ToLower(strings.TrimSpace(query))) + "%"
	return `LOWER(m.text) LIKE ? ESCAPE '\'`, []any{like}
}

func (s *Store) sessionSnippets(sessionID, query string, max int) ([]SearchSnippet, error) {
	cond, args := s.messageTextCondition(query)
	args = append([]any{sessionID}, args...)
	args = append(args, max)

	rows, err := s.db.Query(`
		SELECT m.msg_uid, m.turn, m.role, m.text, m.created_at
		FROM messages m
		WHERE m.session_id = ? AND `+cond+`
		ORDER BY m.id DESC
		LIMIT ?
	`, args...)
	if err != nil {
		like := "%" + escapeLike(strings.ToLower(strings.TrimSpace(query))) + "%"
		rows, err = s.db.Query(`
			SELECT m.msg_uid, m.turn, m.role, m.text, m.created_at
			FROM messages m
			WHERE m.session_id = ? AND LOWER(m.text) LIKE ? ESCAPE '\'
			ORDER BY m.id DESC
			LIMIT ?
		`, sessionID, like, max)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var snips []SearchSnippet
	for rows.Next() {
		var id, role, text, createdAt string
		var turn int
		if err := rows.Scan(&id, &turn, &role, &text, &createdAt); err != nil {
			continue
		}
		snips = append(snips, SearchSnippet{
			MessageID: id,
			Turn:      turn,
			Role:      role,
			Excerpt:   buildSnippetExcerpt(text, query, 160),
			CreatedAt: createdAt,
		})
	}
	// Reverse to chronological order
	for i, j := 0, len(snips)-1; i < j; i, j = i+1, j-1 {
		snips[i], snips[j] = snips[j], snips[i]
	}
	return snips, rows.Err()
}

func (s *Store) RecallSession(opts RecallOptions) (*RecallResult, error) {
	maxChars := opts.MaxChars
	if maxChars <= 0 {
		maxChars = defaultRecallMaxChars
	}

	// Single-message full fetch bypasses truncation so callers can inspect a clipped message cleanly.
	if msgID := strings.TrimSpace(opts.MessageID); msgID != "" {
		row := s.db.QueryRow(`SELECT `+messageSelectCols+` FROM messages WHERE msg_uid = ? LIMIT 1`, msgID)
		m, err := scanMessageRow(row)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: message %q", ErrSessionNotFound, msgID)
			}
			return nil, err
		}
		var sess SessionRecord
		var found bool
		if m.SessionID != "" {
			sess, found = s.GetSessionByID(m.SessionID)
		}
		if !found {
			sess = SessionRecord{ID: m.SessionID, BotID: m.BotID, Status: "unknown"}
		}
		if opts.BotID != "" && sess.BotID != "" && opts.BotID != sess.BotID {
			return nil, fmt.Errorf("%w: message %q does not belong to bot %q", ErrSessionNotFound, msgID, opts.BotID)
		}
		return &RecallResult{
			Session:    toSummary(sess),
			TotalCount: 1,
			Returned:   1,
			Offset:     0,
			HasMore:    false,
			Messages:   []ChatMessage{m},
		}, nil
	}

	var sess SessionRecord
	var found bool
	sid := strings.TrimSpace(opts.SessionID)
	botID := strings.TrimSpace(opts.BotID)

	if sid == "" || strings.EqualFold(sid, "previous") {
		if botID == "" || botID == "*" {
			return nil, fmt.Errorf("%w: botId is required to locate previous session", ErrSessionNotFound)
		}
		sess, found = s.LatestArchivedSession(botID)
	} else if strings.EqualFold(sid, "active") || strings.EqualFold(sid, "current") {
		if botID == "" || botID == "*" {
			return nil, fmt.Errorf("%w: botId is required to locate active session", ErrSessionNotFound)
		}
		sess, found = s.Get(botID)
	} else {
		row := s.db.QueryRow(`SELECT `+sessionSelectCols+` FROM sessions WHERE id = ? LIMIT 1`, sid)
		if r, err := scanSessionRow(row); err == nil {
			if botID == "" || botID == "*" || r.BotID == botID {
				sess, found = r, true
			}
		}
		if !found {
			q := `SELECT ` + sessionSelectCols + ` FROM sessions WHERE runtime_session_id = ?`
			args := []any{sid}
			if botID != "" && botID != "*" {
				q += ` AND bot_id = ?`
				args = append(args, botID)
			}
			q += ` ORDER BY rowid DESC LIMIT 1`
			if r, err := scanSessionRow(s.db.QueryRow(q, args...)); err == nil {
				sess, found = r, true
			}
		}
	}
	if !found {
		return nil, ErrSessionNotFound
	}

	rows, err := s.db.Query(`
		SELECT `+messageSelectCols+`
		FROM messages
		WHERE session_id = ?
		ORDER BY id ASC
	`, sess.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []ChatMessage
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	pool := all
	if q := strings.TrimSpace(opts.Query); q != "" {
		keep := make([]bool, len(all))
		for i, m := range all {
			matched := containsCaseInsensitive(m.Text, q)
			if !matched && len(m.Tools) > 0 {
				matched = containsCaseInsensitive(strings.Join(m.Tools, " "), q)
			}
			if matched {
				for j := max(0, i-1); j <= min(len(all)-1, i+1); j++ {
					keep[j] = true
				}
			}
		}
		var filtered []ChatMessage
		for i, m := range all {
			if keep[i] {
				filtered = append(filtered, m)
			}
		}
		pool = filtered
	}

	total := len(pool)
	var sliced []ChatMessage
	startOffset := 0
	hasMore := false

	if opts.Offset > 0 || opts.Limit > 0 {
		off := max(0, opts.Offset)
		lim := opts.Limit
		if lim <= 0 {
			lim = 20
		}
		if lim > 100 {
			lim = 100
		}
		if off < total {
			end := min(total, off+lim)
			sliced = append(sliced, pool[off:end]...)
			startOffset = off
			hasMore = end < total
		} else {
			startOffset = total
			sliced = []ChatMessage{}
		}
	} else {
		tail := opts.Tail
		if tail <= 0 {
			tail = 20
		}
		if tail > 100 {
			tail = 100
		}
		if total > tail {
			startOffset = total - tail
			hasMore = true
		}
		sliced = append(sliced, pool[startOffset:]...)
	}

	for i := range sliced {
		truncateMessageText(&sliced[i], maxChars)
	}
	if sliced == nil {
		sliced = []ChatMessage{}
	}

	return &RecallResult{
		Session:    toSummary(sess),
		TotalCount: total,
		Returned:   len(sliced),
		Offset:     startOffset,
		HasMore:    hasMore,
		Messages:   sliced,
	}, nil
}

const routineRunSelectCols = `
  id, bot_id, routine_id, routine_name, trigger, status, delivery, model,
  output, error, duration_ms, cost_usd, input_tokens, output_tokens,
  started_at, finished_at, injected_at
`

func scanRoutineRunRow(scanner interface{ Scan(...any) error }) (RoutineRun, error) {
	var r RoutineRun
	err := scanner.Scan(
		&r.ID,
		&r.BotID,
		&r.RoutineID,
		&r.RoutineName,
		&r.Trigger,
		&r.Status,
		&r.Delivery,
		&r.Model,
		&r.Output,
		&r.Error,
		&r.DurationMs,
		&r.CostUSD,
		&r.InputTokens,
		&r.OutputTokens,
		&r.StartedAt,
		&r.FinishedAt,
		&r.InjectedAt,
	)
	return r, err
}

func (s *Store) RecordRoutineRun(run RoutineRun) (RoutineRun, error) {
	if run.ID == "" {
		run.ID = fmt.Sprintf("run_%d_%d", time.Now().UTC().UnixMilli(), s.seq.Add(1))
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if run.StartedAt == "" {
		run.StartedAt = now
	}
	if run.FinishedAt == "" {
		run.FinishedAt = now
	}
	if run.Trigger == "" {
		run.Trigger = "schedule"
	}
	if run.Status == "" {
		run.Status = "ok"
	}
	if run.Delivery == "" {
		run.Delivery = "receipt"
	}
	run.CostUSD = roundCost(run.CostUSD)

	if _, err := s.db.Exec(`
		INSERT INTO routine_runs(`+routineRunSelectCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.BotID, run.RoutineID, run.RoutineName, run.Trigger, run.Status, run.Delivery, run.Model,
		run.Output, run.Error, run.DurationMs, run.CostUSD, run.InputTokens, run.OutputTokens,
		run.StartedAt, run.FinishedAt, run.InjectedAt); err != nil {
		return run, err
	}
	return run, nil
}

func (s *Store) ListRoutineRuns(botID, routineID string, limit int) ([]RoutineRun, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	where := []string{"bot_id = ?"}
	args := []any{botID}
	if strings.TrimSpace(routineID) != "" {
		where = append(where, "routine_id = ?")
		args = append(args, strings.TrimSpace(routineID))
	}
	args = append(args, limit)
	rows, err := s.db.Query(`
		SELECT `+routineRunSelectCols+`
		FROM routine_runs
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY started_at DESC, rowid DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RoutineRun{}
	for rows.Next() {
		r, err := scanRoutineRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) LatestRoutineRun(botID, routineID string) (RoutineRun, bool) {
	row := s.db.QueryRow(`
		SELECT `+routineRunSelectCols+`
		FROM routine_runs
		WHERE bot_id = ? AND routine_id = ?
		ORDER BY started_at DESC, rowid DESC
		LIMIT 1
	`, botID, routineID)
	r, err := scanRoutineRunRow(row)
	if err != nil {
		return RoutineRun{}, false
	}
	return r, true
}

func (s *Store) HasDailyRunOnDate(botID, routineID, localDate string) bool {
	rows, err := s.db.Query(`
		SELECT started_at
		FROM routine_runs
		WHERE bot_id = ? AND routine_id = ? AND trigger = 'schedule'
		ORDER BY started_at DESC, rowid DESC
		LIMIT 10
	`, botID, routineID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var startedAt string
		if err := rows.Scan(&startedAt); err != nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
			if t.In(time.Local).Format("2006-01-02") == localDate {
				return true
			}
		} else if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
			if t.In(time.Local).Format("2006-01-02") == localDate {
				return true
			}
		}
	}
	return false
}

func (s *Store) ListPendingReceipts(botID string, limit int) ([]RoutineRun, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query(`
		SELECT `+routineRunSelectCols+`
		FROM (
			SELECT `+routineRunSelectCols+`, rowid AS _rid
			FROM routine_runs
			WHERE bot_id = ? AND delivery = 'receipt' AND status = 'ok' AND output != '' AND injected_at = ''
			ORDER BY _rid DESC
			LIMIT ?
		) sub
		ORDER BY _rid ASC
	`, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RoutineRun{}
	for rows.Next() {
		r, err := scanRoutineRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) MarkReceiptsInjected(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, now)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.Exec(`UPDATE routine_runs SET injected_at = ? WHERE injected_at = '' AND id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

func (s *Store) DismissPendingReceipts(botID string) error {
	_, err := s.db.Exec(`
		UPDATE routine_runs
		SET injected_at = 'dismissed'
		WHERE bot_id = ? AND delivery = 'receipt' AND status = 'ok' AND injected_at = ''
	`, botID)
	return err
}

// Keep unused os/json imports clean if not referenced directly in store.go.
var (
	_ = os.ErrNotExist
	_ = json.Marshal
)
