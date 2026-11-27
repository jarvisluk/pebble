package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/memory"
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// MemoryStatusResponse is the secret-safe status payload returned by GET /api/memory/status.
type MemoryStatusResponse struct {
	Backend      string                 `json:"backend"`
	Config       RedactedMemoryConfig   `json:"config"`
	DocCount     int                    `json:"docCount"`
	ChunkCount   int                    `json:"chunkCount"`
	Counts       memory.Counts          `json:"counts"`
	Semantic     *memory.SemanticStatus `json:"semantic,omitempty"`
	Magpie       *memory.MagpieStatus   `json:"magpie,omitempty"`
	RecentDreams []memory.DreamRecord   `json:"recentDreams,omitempty"`
}

// MemorySaveRequest extends memory.SaveReq with optional supersedes support.
type MemorySaveRequest struct {
	memory.SaveReq
	Supersedes string `json:"supersedes,omitempty"`
}

func resolveWorkspacesDir(cfg *config.HubConfig) string {
	if cfg == nil {
		return ""
	}
	if s := strings.TrimSpace(cfg.SharedDir); s != "" {
		return filepath.Dir(s)
	}
	if r := strings.TrimSpace(cfg.ProjectRoot); r != "" {
		return filepath.Join(r, "workspaces")
	}
	if len(cfg.Bots) > 0 && strings.TrimSpace(cfg.Bots[0].Workspace) != "" {
		return filepath.Dir(strings.TrimSpace(cfg.Bots[0].Workspace))
	}
	return ""
}

func mapMemoryCoexistence(m *config.MemoryConfig) string {
	if m.EffectiveCoexistence() == config.MemoryCoexistenceIsolated {
		return memory.CoexistenceIsolated
	}
	return memory.CoexistenceAuto
}

func buildMemoryEngineConfig(m *config.MemoryConfig) memory.Config {
	emb := m.EffectiveEmbedding()
	return memory.Config{
		Coexistence: mapMemoryCoexistence(m),
		Semantic: memory.EmbeddingConfig{
			Enabled: emb.Enabled,
			BaseURL: emb.BaseURL,
			APIKey:  emb.APIKey,
			Model:   emb.Model,
		},
		SnapshotMaxChars: m.EffectiveSnapshotMaxChars(),
		DreamEnabled:     m.DreamEnabled(),
		DreamModel:       m.EffectiveDreamModel(),
	}
}

func (h *Hub) Memory() *memory.Store {
	return h.memory
}

func (h *Hub) memoryEnabled() bool {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	if h.cfg == nil {
		return true
	}
	return h.cfg.Memory.IsEnabled()
}

func (h *Hub) memoryDisableClaudeAutoMemory() bool {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	if h.cfg == nil {
		return true
	}
	return h.cfg.Memory.ShouldDisableClaudeAutoMemory()
}

func (h *Hub) memorySnapshotMaxChars() int {
	h.lanesMu.RLock()
	defer h.lanesMu.RUnlock()
	if h.cfg == nil {
		return config.DefaultMemorySnapshotMaxChars
	}
	return h.cfg.Memory.EffectiveSnapshotMaxChars()
}

func (h *Hub) buildMemorySnapshot(ctx context.Context, bot config.BotConfig, maxCharsOverride int) string {
	if h.memory == nil || !h.memoryEnabled() {
		return ""
	}
	h.lanesMu.RLock()
	memCfg := h.cfg.Memory
	h.lanesMu.RUnlock()

	maxChars := memCfg.EffectiveSnapshotMaxChars()
	if maxCharsOverride > 0 && maxCharsOverride < maxChars {
		maxChars = maxCharsOverride
	}
	snap, err := h.memory.BuildSnapshot(ctx, memory.SnapshotOpts{
		BotID:           bot.ID,
		Harness:         bot.Harness,
		PrivateSessions: bot.PrivateSessions,
		Coexistence:     mapMemoryCoexistence(memCfg),
		MaxChars:        maxChars,
	})
	if err != nil {
		log.Printf("[memory] build snapshot for %s failed: %v", bot.ID, err)
		return ""
	}
	return snap
}

func ownerBotFromMemoryPath(rawPath string) string {
	p := filepath.ToSlash(strings.TrimSpace(rawPath))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "workspaces/")
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return ""
	}
	head := strings.TrimSpace(parts[0])
	switch head {
	case "", "_shared", "memory", "items", "daily", "dreams", "MEMORY.md", ".", "..":
		return ""
	default:
		return head
	}
}

// MemorySearch searches Pebble native memory while enforcing privateSessions isolation.
func (h *Hub) MemorySearch(ctx context.Context, callerBotID string, opts memory.SearchOpts) (*memory.ListResponse, error) {
	if h.memory == nil {
		return &memory.ListResponse{
			Backend:   memory.BackendPebble,
			Installed: true,
			Available: false,
			Error:     "memory store unavailable",
			Items:     []memory.Item{},
		}, nil
	}
	callerBotID = strings.TrimSpace(callerBotID)
	opts.BotID = strings.TrimSpace(opts.BotID)
	if opts.BotID != "" && opts.BotID != "*" {
		if _, ok := h.GetBot(opts.BotID); !ok {
			return nil, fmt.Errorf("bot %q not found: %w", opts.BotID, ErrBotNotFound)
		}
		if h.isPrivateFor(opts.BotID, callerBotID) {
			return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, opts.BotID)
		}
	}
	opts.ExcludedBotIDs = append(opts.ExcludedBotIDs, h.privateBotIDsExcept(callerBotID)...)
	return h.memory.Search(ctx, opts)
}

// MemoryGet retrieves an L2 memory item or line-sliced memory file with privacy enforcement.
func (h *Hub) MemoryGet(ctx context.Context, callerBotID string, opts memory.GetOpts) (*memory.GetResult, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	callerBotID = strings.TrimSpace(callerBotID)
	opts.BotID = strings.TrimSpace(opts.BotID)
	if opts.BotID != "" {
		if _, ok := h.GetBot(opts.BotID); !ok {
			return nil, fmt.Errorf("bot %q not found: %w", opts.BotID, ErrBotNotFound)
		}
		if h.isPrivateFor(opts.BotID, callerBotID) {
			return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, opts.BotID)
		}
	}
	if owner := ownerBotFromMemoryPath(opts.Path); owner != "" && h.isPrivateFor(owner, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, owner)
	}
	res, err := h.memory.Get(ctx, opts)
	if err != nil {
		return nil, err
	}
	if res.Item != nil && res.Item.BotID != "" && h.isPrivateFor(res.Item.BotID, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, res.Item.BotID)
	}
	if owner := ownerBotFromMemoryPath(res.Path); owner != "" && h.isPrivateFor(owner, callerBotID) {
		return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, owner)
	}
	return res, nil
}

// MemorySave creates or updates a memory item (or daily log) and optionally marks a superseded item.
func (h *Hub) MemorySave(ctx context.Context, callerBotID string, req MemorySaveRequest) (*memory.SaveResult, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	callerBotID = strings.TrimSpace(callerBotID)
	req.BotID = strings.TrimSpace(req.BotID)
	if req.BotID != "" {
		if _, ok := h.GetBot(req.BotID); !ok {
			return nil, fmt.Errorf("bot %q not found: %w", req.BotID, ErrBotNotFound)
		}
		if h.isPrivateFor(req.BotID, callerBotID) {
			return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, req.BotID)
		}
	}
	if id := strings.TrimSpace(req.ID); id != "" {
		existing, err := h.memory.Get(ctx, memory.GetOpts{ID: id})
		if err != nil {
			return nil, err
		}
		if existing.Item != nil && existing.Item.BotID != "" && h.isPrivateFor(existing.Item.BotID, callerBotID) {
			return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, existing.Item.BotID)
		}
	}
	supersedes := strings.TrimSpace(req.Supersedes)
	if supersedes != "" {
		oldItem, err := h.memory.Get(ctx, memory.GetOpts{ID: supersedes})
		if err != nil {
			return nil, err
		}
		if oldItem.Item != nil && oldItem.Item.BotID != "" && h.isPrivateFor(oldItem.Item.BotID, callerBotID) {
			return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, oldItem.Item.BotID)
		}
	}

	saved, err := h.memory.Save(ctx, req.SaveReq)
	if err != nil {
		return nil, err
	}
	if supersedes != "" && saved != nil && saved.Item.ID != "" && saved.Item.ID != supersedes {
		status := memory.StatusSuperseded
		supBy := saved.Item.ID
		validUntil := time.Now().UTC().Format(time.RFC3339)
		_, _ = h.memory.Patch(ctx, supersedes, memory.PatchReq{
			Status:       &status,
			SupersededBy: &supBy,
			ValidUntil:   &validUntil,
		})
	}
	return saved, nil
}

// MemoryPatch selectively updates an existing memory item while enforcing privacy isolation.
func (h *Hub) MemoryPatch(ctx context.Context, callerBotID, id string, req memory.PatchReq) (*memory.Item, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	existing, err := h.memory.Get(ctx, memory.GetOpts{ID: id})
	if err != nil {
		return nil, err
	}
	if existing.Item != nil && existing.Item.BotID != "" && h.isPrivateFor(existing.Item.BotID, strings.TrimSpace(callerBotID)) {
		return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, existing.Item.BotID)
	}
	return h.memory.Patch(ctx, id, req)
}

// MemoryForget soft-archives or hard-deletes a memory item while enforcing privacy isolation.
func (h *Hub) MemoryForget(ctx context.Context, callerBotID, id string, opts memory.ForgetOpts) (*memory.ForgetResult, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	existing, err := h.memory.Get(ctx, memory.GetOpts{ID: id})
	if err != nil {
		return nil, err
	}
	if existing.Item != nil && existing.Item.BotID != "" && h.isPrivateFor(existing.Item.BotID, strings.TrimSpace(callerBotID)) {
		return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, existing.Item.BotID)
	}
	return h.memory.Forget(ctx, id, opts)
}

// MemoryReadIndex reads global or bot MEMORY.md with privacy enforcement.
func (h *Hub) MemoryReadIndex(ctx context.Context, callerBotID, scope, botID string) (*memory.IndexDoc, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	botID = strings.TrimSpace(botID)
	if botID != "" {
		if _, ok := h.GetBot(botID); !ok {
			return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
		}
		if h.isPrivateFor(botID, strings.TrimSpace(callerBotID)) {
			return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, botID)
		}
	}
	return h.memory.ReadIndex(ctx, scope, botID)
}

// MemoryWriteIndex updates global or bot MEMORY.md with privacy enforcement.
func (h *Hub) MemoryWriteIndex(ctx context.Context, callerBotID, scope, botID, content string) (*memory.IndexDoc, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	botID = strings.TrimSpace(botID)
	if botID != "" {
		if _, ok := h.GetBot(botID); !ok {
			return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
		}
		if h.isPrivateFor(botID, strings.TrimSpace(callerBotID)) {
			return nil, fmt.Errorf("%w: bot %q memory is private", ErrForbidden, botID)
		}
	}
	return h.memory.WriteIndex(ctx, scope, botID, content)
}

// MemoryStatus returns memory index metrics, redacted config, coexistence state, and recent dreams.
func (h *Hub) MemoryStatus(ctx context.Context, botID string) (*MemoryStatusResponse, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	botID = strings.TrimSpace(botID)
	if botID != "" {
		if _, ok := h.GetBot(botID); !ok {
			return nil, fmt.Errorf("bot %q not found: %w", botID, ErrBotNotFound)
		}
	}
	st, err := h.memory.Status(ctx)
	if err != nil {
		return nil, err
	}
	counts := st.Counts
	recent := st.RecentDreams
	if botID != "" {
		if list, err := h.memory.Search(ctx, memory.SearchOpts{
			Scope: memory.ScopeBot,
			BotID: botID,
			Limit: 1,
		}); err == nil && list != nil {
			counts = list.Counts
		}
		filtered := make([]memory.DreamRecord, 0, len(recent))
		for _, rec := range recent {
			if rec.BotID == botID {
				filtered = append(filtered, rec)
			}
		}
		recent = filtered
	}
	return &MemoryStatusResponse{
		Backend:      st.Backend,
		Config:       h.MemoryConfig(),
		DocCount:     st.DocCount,
		ChunkCount:   st.ChunkCount,
		Counts:       counts,
		Semantic:     st.Semantic,
		Magpie:       st.Magpie,
		RecentDreams: recent,
	}, nil
}

// MemoryReindex rebuilds the SQLite derived index from Markdown files on disk.
func (h *Hub) MemoryReindex(ctx context.Context) (memory.SyncStats, error) {
	if h.memory == nil {
		return memory.SyncStats{}, errors.New("memory store unavailable")
	}
	return h.memory.Reindex(ctx)
}

// MemoryImportMagpie imports legacy Magpie memory files directly into Pebble's Markdown SoT.
func (h *Hub) MemoryImportMagpie(ctx context.Context, opts memory.ImportOpts) (*memory.ImportResult, error) {
	if h.memory == nil {
		return nil, errors.New("memory store unavailable")
	}
	opts.BotID = strings.TrimSpace(opts.BotID)
	if opts.BotID != "" {
		if _, ok := h.GetBot(opts.BotID); !ok {
			return nil, fmt.Errorf("bot %q not found: %w", opts.BotID, ErrBotNotFound)
		}
	}
	return h.memory.ImportFromMagpie(ctx, opts)
}
