package memory

import (
	"errors"
	"fmt"
	"time"
)

const (
	BackendPebble = "pebble"

	ScopeGlobal = "global"
	ScopeBot    = "bot"
	ScopeAll    = "all"

	DocTypeItem  = "item"
	DocTypeIndex = "index"
	DocTypeDaily = "daily"
	DocTypeDream = "dream"

	KindProfile    = "profile"
	KindPreference = "preference"
	KindRule       = "rule"
	KindFact       = "fact"
	KindDecision   = "decision"
	KindProcedure  = "procedure"
	KindReference  = "reference"
	KindEpisode    = "episode"

	StatusActive     = "active"
	StatusCandidate  = "candidate"
	StatusSuperseded = "superseded"
	StatusArchived   = "archived"

	AuthorUser         = "user"
	AuthorAgent        = "agent"
	AuthorDream        = "dream"
	AuthorMagpieImport = "magpie_import"

	OriginMemory  = "memory"
	OriginDaily   = "daily"
	OriginDream   = "dream"
	OriginIndex   = "index"
	OriginSession = "session"

	CoexistenceAuto       = "auto"
	CoexistenceIsolated   = "isolated"
	CoexistencePebbleOnly = "pebble_only"
	CoexistenceDual       = "dual"

	GlobalIndexMaxLines = 100
	GlobalIndexMaxBytes = 4096
	BotIndexMaxLines    = 150
	BotIndexMaxBytes    = 6144

	DefaultSnapshotMaxChars = 6000
	DefaultConfidence       = 0.8
)

var (
	ErrNotFound        = errors.New("memory item not found")
	ErrSecretDetected  = errors.New("secret detected in memory content")
	ErrInvalidScope    = errors.New("invalid memory scope")
	ErrInvalidRequest  = errors.New("invalid memory request")
	ErrPathOutsideJail = errors.New("path is outside allowed memory directories")
	ErrProtectedFile   = errors.New("protected workspace file cannot be modified")
	ErrIndexTooLarge   = errors.New("memory index exceeds allowed line limit")
)

// SecretError is a structured error returned when SecretGuard blocks a write.
type SecretError struct {
	Code    string `json:"code"`
	Kind    string `json:"kind"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e *SecretError) Error() string {
	if e == nil {
		return ErrSecretDetected.Error()
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("secret detected (%s)", e.Kind)
}

func (e *SecretError) Unwrap() error {
	return ErrSecretDetected
}

// EmbeddingConfig configures optional Tier 1 OpenAI-compatible embeddings.
type EmbeddingConfig struct {
	Enabled    bool   `json:"enabled"`
	BaseURL    string `json:"baseUrl,omitempty"`
	APIKey     string `json:"apiKey,omitempty"`
	Model      string `json:"model,omitempty"`
	Dimensions int    `json:"dimensions,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

// Config configures the Pebble memory engine.
type Config struct {
	Coexistence      string          `json:"coexistence,omitempty"`
	Semantic         EmbeddingConfig `json:"semantic"`
	SnapshotMaxChars int             `json:"snapshotMaxChars,omitempty"`
	DreamEnabled     bool            `json:"dreamEnabled,omitempty"`
	DreamModel       string          `json:"dreamModel,omitempty"`
	MagpieConfigDir  string          `json:"-"`
}

// ConfigSummary is a secret-safe view of Config exposed in StatusSummary.
type ConfigSummary struct {
	Coexistence      string `json:"coexistence"`
	SnapshotMaxChars int    `json:"snapshotMaxChars"`
	DreamEnabled     bool   `json:"dreamEnabled"`
	DreamModel       string `json:"dreamModel,omitempty"`
	SemanticEnabled  bool   `json:"semanticEnabled"`
	SemanticBaseURL  string `json:"semanticBaseUrl,omitempty"`
	SemanticModel    string `json:"semanticModel,omitempty"`
	SemanticHasKey   bool   `json:"semanticHasKey,omitempty"`
}

// SemanticStatus describes Tier 1 embedding search availability.
type SemanticStatus struct {
	Enabled        bool   `json:"enabled"`
	ModelReady     bool   `json:"modelReady"`
	IndexReady     bool   `json:"indexReady"`
	Active         bool   `json:"active"`
	Model          string `json:"model,omitempty"`
	EmbeddedChunks int    `json:"embeddedChunks,omitempty"`
	TotalChunks    int    `json:"totalChunks,omitempty"`
	Error          string `json:"error,omitempty"`
}

// MagpieStatus describes local Magpie Memory coexistence state.
type MagpieStatus struct {
	Installed         bool     `json:"installed"`
	ConfigDir         string   `json:"configDir,omitempty"`
	HasLegacyItems    bool     `json:"hasLegacyItems"`
	LegacyCount       int      `json:"legacyCount"`
	InjectedHarnesses []string `json:"injectedHarnesses,omitempty"`
}

// Counts aggregates memory item totals by status and scope.
type Counts struct {
	Total      int `json:"total"`
	Active     int `json:"active"`
	Candidate  int `json:"candidate"`
	Superseded int `json:"superseded"`
	Archived   int `json:"archived"`
	Global     int `json:"global"`
	Bot        int `json:"bot"`
	Daily      int `json:"daily,omitempty"`
	Dream      int `json:"dream,omitempty"`
}

// Item is the canonical memory item DTO matching the shared HTTP contract.
type Item struct {
	ID            string   `json:"id"`
	Path          string   `json:"path,omitempty"`
	Scope         string   `json:"scope"`
	BotID         string   `json:"botId,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Title         string   `json:"title,omitempty"`
	Text          string   `json:"text,omitempty"`
	Snippet       string   `json:"snippet,omitempty"`
	Why           string   `json:"why,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Status        string   `json:"status,omitempty"`
	Pin           bool     `json:"pin,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	ValidFrom     string   `json:"validFrom,omitempty"`
	ValidUntil    string   `json:"validUntil,omitempty"`
	SupersededBy  string   `json:"supersededBy,omitempty"`
	SourceSession string   `json:"sourceSession,omitempty"`
	SourceAuthor  string   `json:"sourceAuthor,omitempty"`
	Origin        string   `json:"origin,omitempty"`
	Score         float64  `json:"score,omitempty"`
	CreatedAt     string   `json:"createdAt,omitempty"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
}

// ListResponse is returned by Search and list queries.
type ListResponse struct {
	Backend   string          `json:"backend"`
	Installed bool            `json:"installed"`
	Available bool            `json:"available"`
	Error     string          `json:"error,omitempty"`
	Magpie    *MagpieStatus   `json:"magpie,omitempty"`
	Semantic  *SemanticStatus `json:"semantic,omitempty"`
	Counts    Counts          `json:"counts"`
	Items     []Item          `json:"items"`
}

// IndexDoc describes a curated MEMORY.md document.
type IndexDoc struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Lines    int    `json:"lines"`
	MaxLines int    `json:"maxLines"`
}

// DreamRecord summarizes a recent Dream audit diary file.
type DreamRecord struct {
	Scope     string `json:"scope"`
	BotID     string `json:"botId,omitempty"`
	Date      string `json:"date"`
	Path      string `json:"path"`
	Summary   string `json:"summary,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// StatusSummary summarizes overall memory health and stats.
type StatusSummary struct {
	Backend      string          `json:"backend"`
	Config       ConfigSummary   `json:"config"`
	DocCount     int             `json:"docCount"`
	ChunkCount   int             `json:"chunkCount"`
	Counts       Counts          `json:"counts"`
	Semantic     *SemanticStatus `json:"semantic,omitempty"`
	Magpie       *MagpieStatus   `json:"magpie,omitempty"`
	RecentDreams []DreamRecord   `json:"recentDreams,omitempty"`
}

// SearchOpts filters and configures memory retrieval.
type SearchOpts struct {
	Scope           string
	BotID           string
	ExcludedBotIDs  []string
	Query           string
	Kind            string
	Tags            []string
	Status          string
	IncludeSessions bool
	IncludeDaily    bool
	Compact         bool
	Limit           int
}

// GetOpts selects an L2 memory item by ID or a line-sliced file by workspace-relative path.
type GetOpts struct {
	ID        string
	Path      string
	Scope     string
	BotID     string
	StartLine int
	EndLine   int
}

// GetResult returns the L2 item or line-sliced file content.
type GetResult struct {
	Item       *Item  `json:"item,omitempty"`
	Path       string `json:"path"`
	Content    string `json:"content"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	TotalLines int    `json:"totalLines"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// SaveReq creates/updates a durable item or appends to a daily working log.
type SaveReq struct {
	ID            string   `json:"id,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	BotID         string   `json:"botId,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Title         string   `json:"title,omitempty"`
	Text          string   `json:"text"`
	Why           string   `json:"why,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Status        string   `json:"status,omitempty"`
	Pin           *bool    `json:"pin,omitempty"`
	Confidence    *float64 `json:"confidence,omitempty"`
	ValidFrom     string   `json:"validFrom,omitempty"`
	ValidUntil    string   `json:"validUntil,omitempty"`
	SourceSession string   `json:"sourceSession,omitempty"`
	SourceAuthor  string   `json:"sourceAuthor,omitempty"`
	Daily         bool     `json:"daily,omitempty"`
	Date          string   `json:"date,omitempty"`
}

// SaveResult describes the saved memory record and any high-overlap existing items.
type SaveResult struct {
	Item            Item   `json:"item"`
	Created         bool   `json:"created"`
	Path            string `json:"path"`
	SimilarExisting []Item `json:"similarExisting,omitempty"`
}

// PatchReq updates mutable metadata and body fields of an existing memory item.
type PatchReq struct {
	Title        *string   `json:"title,omitempty"`
	Text         *string   `json:"text,omitempty"`
	Kind         *string   `json:"kind,omitempty"`
	Tags         *[]string `json:"tags,omitempty"`
	Why          *string   `json:"why,omitempty"`
	Pin          *bool     `json:"pin,omitempty"`
	Status       *string   `json:"status,omitempty"`
	Confidence   *float64  `json:"confidence,omitempty"`
	ValidFrom    *string   `json:"validFrom,omitempty"`
	ValidUntil   *string   `json:"validUntil,omitempty"`
	SupersededBy *string   `json:"supersededBy,omitempty"`
}

// ForgetOpts configures soft-archival, hard deletion, and dismissal tracking.
type ForgetOpts struct {
	HardDelete   bool   `json:"hardDelete,omitempty"`
	Dismiss      bool   `json:"dismiss,omitempty"`
	Reason       string `json:"reason,omitempty"`
	SupersededBy string `json:"supersededBy,omitempty"`
}

// ForgetResult reports the outcome of a Forget operation.
type ForgetResult struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Dismissed bool   `json:"dismissed,omitempty"`
	Path      string `json:"path,omitempty"`
}

// SyncStats reports how many files were scanned, updated, or pruned during sync.
type SyncStats struct {
	Scanned   int `json:"scanned"`
	Indexed   int `json:"indexed"`
	Unchanged int `json:"unchanged"`
	Deleted   int `json:"deleted"`
	Chunks    int `json:"chunks"`
}

// SnapshotOpts controls frozen session snapshot rendering.
type SnapshotOpts struct {
	BotID           string
	Harness         string
	PrivateSessions bool
	Coexistence     string
	MaxChars        int
	Now             time.Time
}

// ImportOpts configures importing memories from Magpie Memory markdown files.
type ImportOpts struct {
	Mode      string `json:"mode,omitempty"` // "bots_only" (default) | "all"
	BotID     string `json:"botId,omitempty"`
	DryRun    bool   `json:"dryRun,omitempty"`
	MagpieDir string `json:"-"`
}

// ImportResult summarizes a Magpie Memory import run.
type ImportResult struct {
	Imported    int            `json:"imported"`
	Skipped     int            `json:"skipped"`
	Failed      int            `json:"failed"`
	GlobalCount int            `json:"globalCount"`
	BotCounts   map[string]int `json:"botCounts,omitempty"`
	Errors      []string       `json:"errors,omitempty"`
	Items       []Item         `json:"items,omitempty"`
}
