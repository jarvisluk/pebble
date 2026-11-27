package memory

import (
	"bot-hub/internal/state"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store manages Pebble's file-first Markdown memory SoT and its derived SQLite index.
type Store struct {
	workspacesDir string
	stateStore    *state.Store
	db            *sql.DB

	cfgMu       sync.RWMutex
	cfg         Config
	embedClient *EmbeddingClient

	mu          sync.Mutex
	magpieCache *magpieDetectCache
	clock       func() time.Time
}

// NewStore creates a memory engine backed by workspacesDir (Markdown SoT) and stateStore (derived SQLite index).
func NewStore(workspacesDir string, st *state.Store, cfg Config) (*Store, error) {
	wsDir := strings.TrimSpace(workspacesDir)
	if wsDir == "" {
		return nil, fmt.Errorf("%w: workspacesDir is required", ErrInvalidRequest)
	}
	absWS, err := filepath.Abs(wsDir)
	if err != nil {
		return nil, err
	}
	var db *sql.DB
	if st != nil {
		db = st.DB()
	}
	s := &Store{
		workspacesDir: absWS,
		stateStore:    st,
		db:            db,
		cfg:           cfg,
		magpieCache:   newMagpieDetectCache(30 * time.Second),
		clock:         time.Now,
	}
	if cfg.Semantic.Enabled && strings.TrimSpace(cfg.Semantic.BaseURL) != "" {
		s.embedClient = NewEmbeddingClient(cfg.Semantic)
	}
	return s, nil
}

// WorkspacesDir returns the absolute workspaces root managed by this Store.
func (s *Store) WorkspacesDir() string {
	if s == nil {
		return ""
	}
	return s.workspacesDir
}

// UpdateConfig refreshes runtime memory settings (such as semantic embedding or coexistence config).
func (s *Store) UpdateConfig(cfg Config) {
	if s == nil {
		return
	}
	var nextClient *EmbeddingClient
	if cfg.Semantic.Enabled && strings.TrimSpace(cfg.Semantic.BaseURL) != "" {
		nextClient = NewEmbeddingClient(cfg.Semantic)
	}
	s.cfgMu.Lock()
	s.cfg = cfg
	s.embedClient = nextClient
	s.cfgMu.Unlock()
	if s.magpieCache != nil {
		s.magpieCache.invalidate()
	}
}

func (s *Store) runtimeConfig() (Config, *EmbeddingClient) {
	if s == nil {
		return Config{}, nil
	}
	s.cfgMu.RLock()
	cfg := s.cfg
	client := s.embedClient
	s.cfgMu.RUnlock()
	return cfg, client
}

func (s *Store) now() time.Time {
	if s != nil && s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

// Save creates or updates a durable memory item, or appends a daily working log entry.
func (s *Store) Save(ctx context.Context, req SaveReq) (*SaveResult, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, fmt.Errorf("%w: memory text cannot be empty", ErrInvalidRequest)
	}
	if err := CheckFields(map[string]string{
		"title": req.Title,
		"text":  text,
		"why":   req.Why,
		"tags":  strings.Join(req.Tags, " "),
	}); err != nil {
		return nil, err
	}

	scope, err := NormalizeScope(req.Scope, req.BotID)
	if err != nil {
		return nil, err
	}
	if scope == ScopeAll {
		return nil, fmt.Errorf("%w: save scope must be global or bot", ErrInvalidScope)
	}
	botID := strings.TrimSpace(req.BotID)
	if scope == ScopeGlobal {
		botID = ""
	} else if err := validateBotID(botID); err != nil {
		return nil, err
	}

	if req.Daily {
		return s.appendDailyEntry(ctx, scope, botID, req)
	}
	if id := strings.TrimSpace(req.ID); id != "" {
		return s.updateExistingItem(ctx, id, req)
	}

	// Pre-write similarity check before creating a new item.
	title := DeriveTitle(req.Title, text)
	similar, _ := s.FindSimilar(ctx, scope, botID, title, text, 5)

	nowISO := s.now().UTC().Format(time.RFC3339)
	shortID := randomShortID(4)
	id := "mem_" + shortID
	memDir, err := s.memoryDir(scope, botID)
	if err != nil {
		return nil, err
	}
	fileName := fmt.Sprintf("%s-%s.md", Slugify(title), shortID)
	absPath := filepath.Join(memDir, "items", fileName)
	relPath, _ := filepath.Rel(s.workspacesDir, absPath)
	relPath = filepath.ToSlash(relPath)

	conf := DefaultConfidence
	if req.Confidence != nil && *req.Confidence > 0 {
		conf = *req.Confidence
	}
	pin := req.Pin != nil && *req.Pin
	fm := Frontmatter{
		ID:            id,
		Scope:         scope,
		BotID:         botID,
		Kind:          NormalizeKind(req.Kind),
		Title:         title,
		Status:        NormalizeStatus(req.Status),
		Confidence:    conf,
		Pin:           pin,
		Tags:          NormalizeTags(req.Tags),
		CreatedAt:     nowISO,
		UpdatedAt:     nowISO,
		ValidFrom:     strings.TrimSpace(req.ValidFrom),
		ValidUntil:    strings.TrimSpace(req.ValidUntil),
		SourceSession: strings.TrimSpace(req.SourceSession),
		SourceAuthor:  NormalizeAuthor(req.SourceAuthor),
		Why:           strings.TrimSpace(req.Why),
	}

	if err := s.writeAndSyncItem(ctx, absPath, RenderMarkdownItem(fm, text)); err != nil {
		return nil, err
	}
	return &SaveResult{
		Item:            itemFromFrontmatter(fm, relPath, text, OriginMemory),
		Created:         true,
		Path:            relPath,
		SimilarExisting: similar,
	}, nil
}

func (s *Store) appendDailyEntry(ctx context.Context, scope, botID string, req SaveReq) (*SaveResult, error) {
	memDir, err := s.memoryDir(scope, botID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	dateStr := strings.TrimSpace(req.Date)
	if dateStr == "" {
		dateStr = now.Format("2006-01-02")
	}
	if !isSafeDateString(dateStr) {
		return nil, fmt.Errorf("%w: invalid daily date %q", ErrInvalidRequest, dateStr)
	}
	absPath := filepath.Join(memDir, "daily", dateStr+".md")
	relPath, _ := filepath.Rel(s.workspacesDir, absPath)
	relPath = filepath.ToSlash(relPath)

	heading := now.Format("15:04Z")
	if t := strings.TrimSpace(req.Title); t != "" {
		heading += " " + t
	}
	var entry strings.Builder
	entry.WriteString("## " + heading + "\n\n" + strings.TrimSpace(req.Text) + "\n")
	if tags := NormalizeTags(req.Tags); len(tags) > 0 {
		entry.WriteString("\nTags: #" + strings.Join(tags, " #") + "\n")
	}

	s.mu.Lock()
	var existing string
	if raw, err := os.ReadFile(absPath); err == nil {
		existing = strings.ReplaceAll(string(raw), "\r\n", "\n")
	}
	next := fmt.Sprintf("# Daily Log %s\n\n%s", dateStr, entry.String())
	if strings.TrimSpace(existing) != "" {
		next = strings.TrimRight(existing, "\n") + "\n\n" + entry.String()
	}
	wErr := AtomicWriteFile(absPath, []byte(next), 0o644)
	if wErr == nil {
		wErr = s.syncSingleFileLocked(ctx, absPath)
	}
	s.mu.Unlock()
	if wErr != nil {
		return nil, wErr
	}

	nowISO := now.Format(time.RFC3339)
	return &SaveResult{
		Item: Item{
			ID:           dailyDocID(scope, botID, dateStr),
			Path:         relPath,
			Scope:        scope,
			BotID:        botID,
			Kind:         KindEpisode,
			Title:        "Daily log " + dateStr,
			Text:         strings.TrimSpace(req.Text),
			Snippet:      buildSnippet(req.Text, "", 240),
			Tags:         NormalizeTags(req.Tags),
			Status:       StatusActive,
			SourceAuthor: NormalizeAuthor(req.SourceAuthor),
			Origin:       OriginDaily,
			CreatedAt:    nowISO,
			UpdatedAt:    nowISO,
		},
		Created: strings.TrimSpace(existing) == "",
		Path:    relPath,
	}, nil
}

func (s *Store) updateExistingItem(ctx context.Context, id string, req SaveReq) (*SaveResult, error) {
	absPath, relPath, fm, oldBody, err := s.loadItemByID(ctx, id)
	if err != nil {
		return nil, err
	}
	text := firstNonEmpty(strings.TrimSpace(req.Text), oldBody)
	if strings.TrimSpace(req.Title) != "" {
		fm.Title = DeriveTitle(req.Title, text)
	} else if fm.Title == "" {
		fm.Title = DeriveTitle("", text)
	}
	if strings.TrimSpace(req.Kind) != "" {
		fm.Kind = NormalizeKind(req.Kind)
	}
	if strings.TrimSpace(req.Why) != "" {
		fm.Why = strings.TrimSpace(req.Why)
	}
	if req.Tags != nil {
		fm.Tags = NormalizeTags(req.Tags)
	}
	if strings.TrimSpace(req.Status) != "" {
		fm.Status = NormalizeStatus(req.Status)
	}
	if req.Pin != nil {
		fm.Pin = *req.Pin
	}
	if req.Confidence != nil && *req.Confidence > 0 {
		fm.Confidence = *req.Confidence
	}
	if strings.TrimSpace(req.ValidFrom) != "" {
		fm.ValidFrom = strings.TrimSpace(req.ValidFrom)
	}
	if strings.TrimSpace(req.ValidUntil) != "" {
		fm.ValidUntil = strings.TrimSpace(req.ValidUntil)
	}
	if strings.TrimSpace(req.SourceSession) != "" {
		fm.SourceSession = strings.TrimSpace(req.SourceSession)
	}
	if strings.TrimSpace(req.SourceAuthor) != "" {
		fm.SourceAuthor = NormalizeAuthor(req.SourceAuthor)
	}
	fm.UpdatedAt = s.now().UTC().Format(time.RFC3339)

	if err := s.writeAndSyncItem(ctx, absPath, RenderMarkdownItem(fm, text)); err != nil {
		return nil, err
	}
	return &SaveResult{
		Item:    itemFromFrontmatter(fm, relPath, text, OriginMemory),
		Created: false,
		Path:    relPath,
	}, nil
}

// Patch selectively updates fields on an existing memory item (e.g. approving candidate -> active).
func (s *Store) Patch(ctx context.Context, id string, req PatchReq) (*Item, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidRequest)
	}
	checkMap := map[string]string{}
	if req.Title != nil {
		checkMap["title"] = *req.Title
	}
	if req.Text != nil {
		checkMap["text"] = *req.Text
	}
	if req.Why != nil {
		checkMap["why"] = *req.Why
	}
	if req.Tags != nil {
		checkMap["tags"] = strings.Join(*req.Tags, " ")
	}
	if err := CheckFields(checkMap); err != nil {
		return nil, err
	}

	absPath, relPath, fm, body, err := s.loadItemByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Text != nil {
		body = strings.TrimSpace(*req.Text)
		if body == "" {
			return nil, fmt.Errorf("%w: memory text cannot be empty", ErrInvalidRequest)
		}
	}
	if req.Title != nil {
		fm.Title = DeriveTitle(*req.Title, body)
	}
	if req.Kind != nil {
		fm.Kind = NormalizeKind(*req.Kind)
	}
	if req.Tags != nil {
		fm.Tags = NormalizeTags(*req.Tags)
	}
	if req.Why != nil {
		fm.Why = strings.TrimSpace(*req.Why)
	}
	if req.Pin != nil {
		fm.Pin = *req.Pin
	}
	if req.Status != nil {
		fm.Status = NormalizeStatus(*req.Status)
	}
	if req.Confidence != nil && *req.Confidence > 0 {
		fm.Confidence = *req.Confidence
	}
	if req.ValidFrom != nil {
		fm.ValidFrom = strings.TrimSpace(*req.ValidFrom)
	}
	if req.ValidUntil != nil {
		fm.ValidUntil = strings.TrimSpace(*req.ValidUntil)
	}
	if req.SupersededBy != nil {
		fm.SupersededBy = strings.TrimSpace(*req.SupersededBy)
	}
	fm.UpdatedAt = s.now().UTC().Format(time.RFC3339)

	if err := s.writeAndSyncItem(ctx, absPath, RenderMarkdownItem(fm, body)); err != nil {
		return nil, err
	}
	it := itemFromFrontmatter(fm, relPath, body, OriginMemory)
	return &it, nil
}

// Forget soft-archives (default) or hard-deletes a memory item, optionally recording a dismissal fingerprint.
func (s *Store) Forget(ctx context.Context, id string, opts ForgetOpts) (*ForgetResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidRequest)
	}
	absPath, relPath, fm, body, err := s.loadItemByID(ctx, id)
	if err != nil {
		return nil, err
	}

	nowISO := s.now().UTC().Format(time.RFC3339)
	if opts.Dismiss && s.db != nil {
		fp := Fingerprint(fm.Scope, fm.BotID, fm.Title, body)
		summary := DeriveTitle(fm.Title, body)
		s.mu.Lock()
		_, _ = s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO memory_dismissed(fingerprint, scope, bot_id, summary, dismissed_at)
			VALUES (?, ?, ?, ?, ?)
		`, fp, fm.Scope, fm.BotID, summary, nowISO)
		s.mu.Unlock()
	}

	if opts.HardDelete {
		s.mu.Lock()
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			s.mu.Unlock()
			return nil, err
		}
		if s.db != nil {
			_ = s.deleteDocFromIndexLocked(ctx, fm.ID)
			_, _ = s.db.ExecContext(ctx, `DELETE FROM memory_stats WHERE doc_id = ?`, fm.ID)
		}
		s.mu.Unlock()
		return &ForgetResult{ID: fm.ID, Action: "deleted", Dismissed: opts.Dismiss, Path: relPath}, nil
	}

	fm.Status = StatusArchived
	if fm.ValidUntil == "" {
		fm.ValidUntil = nowISO
	}
	if strings.TrimSpace(opts.SupersededBy) != "" {
		fm.SupersededBy = strings.TrimSpace(opts.SupersededBy)
	}
	fm.UpdatedAt = nowISO

	if err := s.writeAndSyncItem(ctx, absPath, RenderMarkdownItem(fm, body)); err != nil {
		return nil, err
	}
	return &ForgetResult{ID: fm.ID, Action: "archived", Dismissed: opts.Dismiss, Path: relPath}, nil
}

func (s *Store) writeAndSyncItem(ctx context.Context, absPath, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := AtomicWriteFile(absPath, []byte(content), 0o644); err != nil {
		return err
	}
	return s.syncSingleFileLocked(ctx, absPath)
}
