package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	SlotKey      string  `json:"slotKey,omitempty"`
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

// Keep unused os/json imports clean if not referenced directly in store.go.
var (
	_ = os.ErrNotExist
	_ = json.Marshal
)
