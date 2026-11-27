package memory

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultEmbeddingTimeout = 15 * time.Second
	embeddingBatchSize      = 32
	queryCacheMaxEntries    = 64
	queryCacheTTL           = 5 * time.Minute
)

type cachedQueryVec struct {
	vec       []float32
	expiresAt time.Time
}

// EmbeddingClient calls an OpenAI-compatible /v1/embeddings endpoint using stdlib net/http.
type EmbeddingClient struct {
	cfg        EmbeddingConfig
	endpoint   string
	httpClient *http.Client

	cacheMu sync.Mutex
	cache   map[string]cachedQueryVec
}

// NewEmbeddingClient constructs a Tier 1 embedding client from cfg.
func NewEmbeddingClient(cfg EmbeddingConfig) *EmbeddingClient {
	timeout := defaultEmbeddingTimeout
	if cfg.TimeoutSec > 0 {
		timeout = time.Duration(cfg.TimeoutSec) * time.Second
	}
	return &EmbeddingClient{
		cfg:        cfg,
		endpoint:   normalizeEmbeddingEndpoint(cfg.BaseURL),
		httpClient: &http.Client{Timeout: timeout},
		cache:      make(map[string]cachedQueryVec),
	}
}

func normalizeEmbeddingEndpoint(baseURL string) string {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u == "" {
		return ""
	}
	if strings.HasSuffix(u, "/embeddings") {
		return u
	}
	if strings.HasSuffix(u, "/v1") {
		return u + "/embeddings"
	}
	return u + "/v1/embeddings"
}

// Model returns the configured embedding model identifier.
func (c *EmbeddingClient) Model() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.cfg.Model)
}

// Ready reports whether the client has the minimum configuration required to run.
func (c *EmbeddingClient) Ready() bool {
	return c != nil && c.cfg.Enabled && c.endpoint != "" && c.Model() != ""
}

// Embed computes float32 embeddings for each input string in order.
func (c *EmbeddingClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if !c.Ready() {
		return nil, fmt.Errorf("embedding client is not configured")
	}
	if len(inputs) == 0 {
		return nil, nil
	}

	payload := map[string]any{
		"model": c.Model(),
		"input": inputs,
	}
	if c.cfg.Dimensions > 0 {
		payload["dimensions"] = c.cfg.Dimensions
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(c.cfg.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding endpoint returned HTTP %d", resp.StatusCode)
	}

	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(parsed.Data) != len(inputs) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(parsed.Data), len(inputs))
	}

	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// EmbedQuery embeds a single search query with a short in-memory TTL cache.
func (c *EmbeddingClient) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	key := c.Model() + "|" + strings.TrimSpace(query)
	now := time.Now()

	c.cacheMu.Lock()
	if hit, ok := c.cache[key]; ok && now.Before(hit.expiresAt) {
		c.cacheMu.Unlock()
		return hit.vec, nil
	}
	c.cacheMu.Unlock()

	vecs, err := c.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	c.cacheMu.Lock()
	if len(c.cache) >= queryCacheMaxEntries {
		for k, v := range c.cache {
			if now.After(v.expiresAt) {
				delete(c.cache, k)
			}
		}
		if len(c.cache) >= queryCacheMaxEntries {
			c.cache = make(map[string]cachedQueryVec)
		}
	}
	c.cache[key] = cachedQueryVec{vec: vecs[0], expiresAt: now.Add(queryCacheTTL)}
	c.cacheMu.Unlock()
	return vecs[0], nil
}

// EncodeFloat32s serializes a float32 slice into little-endian bytes for SQLite BLOB storage.
func EncodeFloat32s(vec []float32) []byte {
	if len(vec) == 0 {
		return nil
	}
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// DecodeFloat32s deserializes little-endian bytes into a float32 slice.
func DecodeFloat32s(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// CosineSimilarity computes cosine similarity between two equal-length float32 vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// SyncEmbeddings backfills missing embeddings for indexed chunks when Tier 1 semantic search is enabled.
func (s *Store) SyncEmbeddings(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncEmbeddingsLocked(ctx)
}

func (s *Store) syncEmbeddingsLocked(ctx context.Context) (int, error) {
	_, embedClient := s.runtimeConfig()
	if embedClient == nil || !embedClient.Ready() || s.db == nil {
		return 0, nil
	}
	model := embedClient.Model()

	type pendingChunk struct {
		chunkID string
		text    string
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.chunk_id, d.title, c.heading, c.text
		FROM memory_chunks c
		JOIN memory_docs d ON d.doc_id = c.doc_id
		LEFT JOIN memory_embeddings e ON e.chunk_id = c.chunk_id AND e.model = ?
		WHERE e.chunk_id IS NULL AND c.text != ''
		ORDER BY c.chunk_id ASC
	`, model)
	if err != nil {
		return 0, err
	}
	var pending []pendingChunk
	for rows.Next() {
		var cid, title, heading, text string
		if scanErr := rows.Scan(&cid, &title, &heading, &text); scanErr != nil {
			_ = rows.Close()
			return 0, scanErr
		}
		combined := strings.TrimSpace(strings.Join([]string{title, heading, text}, "\n"))
		pending = append(pending, pendingChunk{chunkID: cid, text: combined})
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	embedded := 0
	nowISO := s.now().UTC().Format(time.RFC3339)
	for start := 0; start < len(pending); start += embeddingBatchSize {
		if err := ctx.Err(); err != nil {
			return embedded, err
		}
		end := start + embeddingBatchSize
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[start:end]
		inputs := make([]string, len(batch))
		for i, p := range batch {
			inputs[i] = p.text
		}
		vecs, err := embedClient.Embed(ctx, inputs)
		if err != nil {
			return embedded, err
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return embedded, err
		}
		for i, vec := range vecs {
			if len(vec) == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO memory_embeddings(chunk_id, model, dim, vec, updated_at)
				VALUES (?, ?, ?, ?, ?)
			`, batch[i].chunkID, model, len(vec), EncodeFloat32s(vec), nowISO); err != nil {
				_ = tx.Rollback()
				return embedded, err
			}
			embedded++
		}
		if err := tx.Commit(); err != nil {
			return embedded, err
		}
	}
	return embedded, nil
}

func (s *Store) semanticStatus(ctx context.Context) *SemanticStatus {
	cfg, embedClient := s.runtimeConfig()
	st := &SemanticStatus{
		Enabled: cfg.Semantic.Enabled,
		Model:   strings.TrimSpace(cfg.Semantic.Model),
	}
	if embedClient != nil && embedClient.Ready() {
		st.ModelReady = true
	}
	if s.db != nil {
		st.TotalChunks = fmtDocCountQuery(s.db, ctx, "memory_chunks")
		if st.Model != "" {
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE model = ?`, st.Model).Scan(&st.EmbeddedChunks)
		} else {
			st.EmbeddedChunks = fmtDocCountQuery(s.db, ctx, "memory_embeddings")
		}
		st.IndexReady = st.EmbeddedChunks > 0
	}
	st.Active = st.Enabled && st.ModelReady && (st.IndexReady || st.TotalChunks == 0)
	return st
}
