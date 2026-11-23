package state

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

var (
	// ErrSessionNotFound indicates the requested session does not exist.
	ErrSessionNotFound = errors.New("session not found")

	legacyResetSessionRe = regexp.MustCompile(`旧 Session:\s*([^\s）)]+)`)
)

const defaultRecallMaxChars = 2000

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id                 TEXT PRIMARY KEY,
  bot_id             TEXT NOT NULL,
  runtime_session_id TEXT NOT NULL DEFAULT '',
  harness            TEXT NOT NULL DEFAULT '',
  model              TEXT NOT NULL DEFAULT '',
  status             TEXT NOT NULL DEFAULT 'active',
  title              TEXT NOT NULL DEFAULT '',
  last_error         TEXT NOT NULL DEFAULT '',
  handoff_note       TEXT NOT NULL DEFAULT '',
  turn_count         INTEGER NOT NULL DEFAULT 0,
  total_cost_usd     REAL NOT NULL DEFAULT 0,
  last_input_tokens  INTEGER NOT NULL DEFAULT 0,
  created_at         TEXT NOT NULL,
  last_active_at     TEXT NOT NULL,
  ended_at           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_bot_active ON sessions(bot_id, last_active_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_runtime    ON sessions(runtime_session_id);

CREATE TABLE IF NOT EXISTS messages (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  msg_uid       TEXT NOT NULL UNIQUE,
  session_id    TEXT NOT NULL DEFAULT '',
  bot_id        TEXT NOT NULL,
  turn          INTEGER NOT NULL DEFAULT 0,
  role          TEXT NOT NULL,
  sender        TEXT NOT NULL DEFAULT '',
  recipient     TEXT NOT NULL DEFAULT '',
  text          TEXT NOT NULL,
  media_json    TEXT NOT NULL DEFAULT '[]',
  tools_json    TEXT NOT NULL DEFAULT '[]',
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  cost_usd      REAL NOT NULL DEFAULT 0,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session_turn ON messages(session_id, turn, id);
CREATE INDEX IF NOT EXISTS idx_messages_bot_id       ON messages(bot_id, id DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
  text,
  content='messages',
  content_rowid='id',
  tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, text) VALUES (new.id, new.text);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.id, old.text);
END;

CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE OF text ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.id, old.text);
  INSERT INTO messages_fts(rowid, text) VALUES (new.id, new.text);
END;

CREATE TABLE IF NOT EXISTS routine_runs (
  id            TEXT PRIMARY KEY,
  bot_id        TEXT NOT NULL,
  routine_id    TEXT NOT NULL,
  routine_name  TEXT NOT NULL DEFAULT '',
  trigger       TEXT NOT NULL DEFAULT 'schedule',
  status        TEXT NOT NULL DEFAULT 'ok',
  delivery      TEXT NOT NULL DEFAULT 'receipt',
  model         TEXT NOT NULL DEFAULT '',
  output        TEXT NOT NULL DEFAULT '',
  error         TEXT NOT NULL DEFAULT '',
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  cost_usd      REAL NOT NULL DEFAULT 0,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  started_at    TEXT NOT NULL,
  finished_at   TEXT NOT NULL,
  injected_at   TEXT NOT NULL DEFAULT '',
  slot_key      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_routine_runs_bot_routine ON routine_runs(bot_id, routine_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_routine_runs_pending     ON routine_runs(bot_id, delivery, status, injected_at);
CREATE INDEX IF NOT EXISTS idx_routine_runs_slot_key    ON routine_runs(slot_key);

CREATE TABLE IF NOT EXISTS push_devices (
  device_id                 TEXT PRIMARY KEY,
  platform                  TEXT NOT NULL,
  env                       TEXT NOT NULL DEFAULT 'production',
  bundle_id                 TEXT NOT NULL DEFAULT '',
  push_token                TEXT NOT NULL DEFAULT '',
  live_activity_start_token TEXT NOT NULL DEFAULT '',
  activity_tokens_json      TEXT NOT NULL DEFAULT '{}',
  enabled                   INTEGER NOT NULL DEFAULT 1,
  updated_at                TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_files (
  path       TEXT PRIMARY KEY,
  sha256     TEXT NOT NULL DEFAULT '',
  size       INTEGER NOT NULL DEFAULT 0,
  mtime_unix INTEGER NOT NULL DEFAULT 0,
  deleted    INTEGER NOT NULL DEFAULT 0,
  vc_json    TEXT NOT NULL DEFAULT '{}',
  updated_by TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT ''
);
`

func openSQLite(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create state dir for %s: %w", dbPath, err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	// Single-writer connection prevents intra-process SQLITE_BUSY under concurrent bots.
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("sqlite %s: %w", p, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init sqlite schema: %w", err)
	}
	if err := ensureMessagesMediaColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate messages.media_json: %w", err)
	}
	if err := ensureRoutineRunsSlotKeyColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate routine_runs.slot_key: %w", err)
	}
	return db, nil
}

func ensureRoutineRunsSlotKeyColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(routine_runs)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name, ctyp string
			notnull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "slot_key" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err = db.Exec(`ALTER TABLE routine_runs ADD COLUMN slot_key TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_routine_runs_slot_key ON routine_runs(slot_key)`)
	return err
}

func ensureMessagesMediaColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name, ctyp string
			notnull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "media_json" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE messages ADD COLUMN media_json TEXT NOT NULL DEFAULT '[]'`)
	return err
}

func newSessionID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("ses_%d_%s", time.Now().UTC().UnixMilli(), hex.EncodeToString(b))
}

func (s *Store) newMessageUID() string {
	n := s.seq.Add(1)
	return fmt.Sprintf("msg_%d_%d", time.Now().UTC().UnixMilli(), n)
}

func roundCost(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func firstLineSummary(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	return truncateRunes(s, max)
}

// MagpieSessionFor formats the runtime session reference for magpie-sessions__read_session.
func MagpieSessionFor(harness, runtimeSessionID string) string {
	runtimeSessionID = strings.TrimSpace(runtimeSessionID)
	if runtimeSessionID == "" {
		return ""
	}
	switch harness {
	case "opencode", "claude", "pi":
		return harness + ":" + runtimeSessionID
	default:
		return ""
	}
}

func (s *Store) migrateLegacyIfNeeded() error {
	var done string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'legacy_imported'`).Scan(&done)
	if err == nil && done == "1" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	type legacySession struct {
		SessionID    string  `json:"sessionId"`
		CreatedAt    string  `json:"createdAt"`
		LastActiveAt string  `json:"lastActiveAt"`
		TurnCount    int     `json:"turnCount"`
		TotalCostUSD float64 `json:"totalCostUsd"`
	}
	legacySessions := map[string]legacySession{}
	if s.legacySessionsPath != "" {
		if raw, err := os.ReadFile(s.legacySessionsPath); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
			_ = json.Unmarshal(raw, &legacySessions)
		}
	}

	legacyMsgsByBot := map[string][]ChatMessage{}
	if s.legacyMessagesDir != "" {
		entries, err := os.ReadDir(s.legacyMessagesDir)
		if err == nil {
			for _, ent := range entries {
				if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".jsonl") {
					continue
				}
				botID := strings.TrimSuffix(ent.Name(), ".jsonl")
				msgs := readLegacyJSONL(filepath.Join(s.legacyMessagesDir, ent.Name()), botID)
				if len(msgs) > 0 {
					legacyMsgsByBot[botID] = msgs
				}
			}
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	botSet := map[string]bool{}
	for botID := range legacySessions {
		botSet[botID] = true
	}
	for botID := range legacyMsgsByBot {
		botSet[botID] = true
	}

	botIDs := make([]string, 0, len(botSet))
	for botID := range botSet {
		botIDs = append(botIDs, botID)
	}
	sort.Strings(botIDs)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, botID := range botIDs {
		msgs := legacyMsgsByBot[botID]
		activeRec, hasActive := legacySessions[botID]

		if len(msgs) == 0 && hasActive {
			id := newSessionID()
			createdAt := nonEmpty(activeRec.CreatedAt, now)
			lastAt := nonEmpty(activeRec.LastActiveAt, createdAt)
			if _, err := tx.Exec(`
				INSERT OR IGNORE INTO sessions(id, bot_id, runtime_session_id, harness, model, status, title, turn_count, total_cost_usd, created_at, last_active_at, ended_at)
				VALUES (?, ?, ?, '', '', 'active', '', ?, ?, ?, ?, '')
			`, id, botID, activeRec.SessionID, activeRec.TurnCount, roundCost(activeRec.TotalCostUSD), createdAt, lastAt); err != nil {
				return err
			}
			continue
		}

		segments := splitLegacySegments(msgs)
		activeClaimed := false
		for idx, seg := range segments {
			isLast := idx == len(segments)-1
			runtimeID := seg.runtimeSessionID
			if runtimeID == "" && isLast && hasActive {
				runtimeID = activeRec.SessionID
			}
			hubID := fmt.Sprintf("ses_legacy_%s_%d_%d", botID, time.Now().UTC().UnixMilli(), idx+1)

			status := "superseded"
			lastErr := ""
			hasResetMarker := false
			for _, m := range seg.msgs {
				if m.Role == "system" && strings.HasPrefix(m.Text, "Session 已重置") {
					hasResetMarker = true
				}
				if m.Role == "system" && m.From == "error" {
					lastErr = m.Text
				}
			}

			if isLast && hasActive && !hasResetMarker && (activeRec.SessionID == "" || runtimeID == "" || activeRec.SessionID == runtimeID) {
				status = "active"
				activeClaimed = true
			} else if hasResetMarker {
				status = "reset"
			} else if lastErr != "" {
				status = "error"
			}

			createdAt := seg.msgs[0].CreatedAt
			if createdAt == "" {
				createdAt = now
			}
			lastAt := seg.msgs[len(seg.msgs)-1].CreatedAt
			if lastAt == "" {
				lastAt = createdAt
			}
			endedAt := ""
			if status != "active" {
				endedAt = lastAt
			}

			title := ""
			turn := 0
			var totalCost float64
			for i := range seg.msgs {
				m := &seg.msgs[i]
				if m.Role == "user" || (m.Role == "bot_in" && m.From != "" && !strings.HasPrefix(m.From, "bot:") || m.Role == "bot_in") {
					if m.Role == "user" || (m.Role == "bot_in" && m.To == "") {
						turn++
						if title == "" {
							title = firstLineSummary(m.Text, 120)
						}
					}
				} else if turn == 0 {
					turn = 1
				}
				totalCost += m.CostUSD
			}
			if status == "active" && activeRec.TurnCount > turn {
				turn = activeRec.TurnCount
			}
			if status == "active" && activeRec.TotalCostUSD > totalCost {
				totalCost = activeRec.TotalCostUSD
			}
			if status == "active" && activeRec.CreatedAt != "" {
				createdAt = activeRec.CreatedAt
			}
			if status == "active" && activeRec.LastActiveAt != "" {
				lastAt = activeRec.LastActiveAt
			}

			if _, err := tx.Exec(`
				INSERT OR IGNORE INTO sessions(id, bot_id, runtime_session_id, harness, model, status, title, last_error, handoff_note, turn_count, total_cost_usd, created_at, last_active_at, ended_at)
				VALUES (?, ?, ?, '', '', ?, ?, ?, '', ?, ?, ?, ?, ?)
			`, hubID, botID, runtimeID, status, title, lastErr, turn, roundCost(totalCost), createdAt, lastAt, endedAt); err != nil {
				return err
			}

			curTurn := 0
			for _, m := range seg.msgs {
				if m.Role == "user" || (m.Role == "bot_in" && m.To == "") {
					curTurn++
				} else if curTurn == 0 {
					curTurn = 1
				}
				uid := m.ID
				if uid == "" {
					uid = s.newMessageUID()
				}
				mCreated := nonEmpty(m.CreatedAt, createdAt)
				toolsJSON := "[]"
				if len(m.Tools) > 0 {
					if b, err := json.Marshal(m.Tools); err == nil {
						toolsJSON = string(b)
					}
				}
				if _, err := tx.Exec(`
					INSERT OR IGNORE INTO messages(msg_uid, session_id, bot_id, turn, role, sender, recipient, text, tools_json, duration_ms, cost_usd, input_tokens, output_tokens, created_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, uid, hubID, botID, curTurn, m.Role, m.From, m.To, m.Text, toolsJSON, m.DurationMs, m.CostUSD, m.InputTokens, m.OutputTokens, mCreated); err != nil {
					return err
				}
			}
		}

		if hasActive && !activeClaimed {
			id := newSessionID()
			createdAt := nonEmpty(activeRec.CreatedAt, now)
			lastAt := nonEmpty(activeRec.LastActiveAt, createdAt)
			if _, err := tx.Exec(`
				INSERT OR IGNORE INTO sessions(id, bot_id, runtime_session_id, harness, model, status, title, turn_count, total_cost_usd, created_at, last_active_at, ended_at)
				VALUES (?, ?, ?, '', '', 'active', '', ?, ?, ?, ?, '')
			`, id, botID, activeRec.SessionID, activeRec.TurnCount, roundCost(activeRec.TotalCostUSD), createdAt, lastAt); err != nil {
				return err
			}
		}
	}

	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES ('legacy_imported', '1')`); err != nil {
		return err
	}
	return tx.Commit()
}

type legacySegment struct {
	runtimeSessionID string
	msgs             []ChatMessage
}

func splitLegacySegments(msgs []ChatMessage) []legacySegment {
	var out []legacySegment
	var cur []ChatMessage
	curRuntime := ""

	flush := func() {
		if len(cur) == 0 {
			return
		}
		out = append(out, legacySegment{runtimeSessionID: curRuntime, msgs: cur})
		cur = nil
		curRuntime = ""
	}

	for _, m := range msgs {
		msgRuntime := strings.TrimSpace(m.SessionID)
		isReset := m.Role == "system" && strings.HasPrefix(m.Text, "Session 已重置")
		if isReset && msgRuntime == "" {
			if match := legacyResetSessionRe.FindStringSubmatch(m.Text); len(match) == 2 {
				msgRuntime = strings.TrimSpace(match[1])
			}
		}
		if msgRuntime != "" && curRuntime != "" && msgRuntime != curRuntime {
			// If a new non-reset message brings a brand-new runtimeSessionID after an existing one, start a new segment.
			if !isReset {
				flush()
			}
		}
		cur = append(cur, m)
		if msgRuntime != "" && curRuntime == "" {
			curRuntime = msgRuntime
		}
		if isReset {
			flush()
		}
	}
	flush()
	return out
}

func readLegacyJSONL(path, botID string) []ChatMessage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var all []ChatMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var m ChatMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if m.BotID == "" {
			m.BotID = botID
		}
		all = append(all, m)
	}
	return all
}

func nonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func buildFTSQuery(raw string) string {
	// Wrap each non-empty whitespace token in double quotes so special FTS5 operators don't throw syntax errors.
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		escaped := strings.ReplaceAll(f, `"`, `""`)
		parts = append(parts, `"`+escaped+`"`)
	}
	return strings.Join(parts, " AND ")
}

func canUseTrigramFTS(query string) bool {
	fields := strings.Fields(strings.TrimSpace(query))
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		if utf8.RuneCountInString(f) < 3 {
			return false
		}
	}
	return true
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func decodeToolsJSON(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

func scanSessionRow(scanner interface{ Scan(...any) error }) (SessionRecord, error) {
	var rec SessionRecord
	var hubID, runtimeID string
	err := scanner.Scan(
		&hubID,
		&rec.BotID,
		&runtimeID,
		&rec.Harness,
		&rec.Model,
		&rec.Status,
		&rec.Title,
		&rec.LastError,
		&rec.HandoffNote,
		&rec.TurnCount,
		&rec.TotalCostUSD,
		&rec.LastInputTokens,
		&rec.CreatedAt,
		&rec.LastActiveAt,
		&rec.EndedAt,
	)
	if err != nil {
		return SessionRecord{}, err
	}
	rec.ID = hubID
	rec.RuntimeSessionID = runtimeID
	rec.SessionID = nonEmpty(runtimeID, hubID)
	rec.MagpieSession = MagpieSessionFor(rec.Harness, runtimeID)
	return rec, nil
}

func toSummary(rec SessionRecord) SessionSummary {
	return SessionSummary{
		SessionID:        rec.ID,
		RuntimeSessionID: rec.RuntimeSessionID,
		BotID:            rec.BotID,
		Harness:          rec.Harness,
		Model:            rec.Model,
		Status:           rec.Status,
		Title:            rec.Title,
		LastError:        rec.LastError,
		HandoffNote:      rec.HandoffNote,
		TurnCount:        rec.TurnCount,
		TotalCostUSD:     rec.TotalCostUSD,
		LastInputTokens:  rec.LastInputTokens,
		CreatedAt:        rec.CreatedAt,
		LastActiveAt:     rec.LastActiveAt,
		EndedAt:          rec.EndedAt,
		MagpieSession:    rec.MagpieSession,
	}
}

func scanMessageRow(scanner interface{ Scan(...any) error }) (ChatMessage, error) {
	var m ChatMessage
	var mediaJSON string
	var toolsJSON string
	err := scanner.Scan(
		&m.ID,
		&m.SessionID,
		&m.BotID,
		&m.Turn,
		&m.Role,
		&m.From,
		&m.To,
		&m.Text,
		&mediaJSON,
		&toolsJSON,
		&m.DurationMs,
		&m.CostUSD,
		&m.InputTokens,
		&m.OutputTokens,
		&m.CreatedAt,
	)
	if err != nil {
		return ChatMessage{}, err
	}
	m.Media = decodeMediaJSON(mediaJSON)
	m.Tools = decodeToolsJSON(toolsJSON)
	return m, nil
}

func decodeMediaJSON(raw string) []MessageMedia {
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	var out []MessageMedia
	if err := json.Unmarshal([]byte(raw), &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func isValidMsgRowID(id string) (int64, bool) {
	if !strings.HasPrefix(id, "row_") {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(id, "row_"), 10, 64)
	return n, err == nil
}

func containsCaseInsensitive(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func buildSnippetExcerpt(text, query string, maxRunes int) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if clean == "" {
		return ""
	}
	runes := []rune(clean)
	if len(runes) <= maxRunes {
		return clean
	}
	lowerClean := strings.ToLower(clean)
	lowerQ := strings.ToLower(strings.TrimSpace(query))
	if lowerQ != "" {
		if byteIdx := strings.Index(lowerClean, lowerQ); byteIdx >= 0 {
			runeIdx := utf8.RuneCountInString(lowerClean[:byteIdx])
			half := maxRunes / 2
			start := runeIdx - half
			if start < 0 {
				start = 0
			}
			end := start + maxRunes
			if end > len(runes) {
				end = len(runes)
				start = end - maxRunes
				if start < 0 {
					start = 0
				}
			}
			prefix := ""
			suffix := ""
			if start > 0 {
				prefix = "…"
			}
			if end < len(runes) {
				suffix = "…"
			}
			return prefix + string(runes[start:end]) + suffix
		}
	}
	return string(runes[:maxRunes-1]) + "…"
}

func truncateMessageText(m *ChatMessage, maxChars int) {
	if maxChars <= 0 {
		return
	}
	r := []rune(m.Text)
	if len(r) <= maxChars {
		return
	}
	omitted := len(r) - maxChars
	m.Text = string(r[:maxChars]) + fmt.Sprintf("\n[...truncated %d chars, messageId=%s]", omitted, m.ID)
}
