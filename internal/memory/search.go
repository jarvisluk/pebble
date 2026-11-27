package memory

import (
	"context"
	"math"
	"strings"
)

type searchCandidate struct {
	item        Item
	docType     string
	lexScore    float64
	vecScore    float64
	bestChunk   string
	vec         []float32
	accessCount int
	finalScore  float64
}

// Search queries Pebble memory using Tier 0 (unicode61 + trigram FTS5 + short-token LIKE + BM25 +
// recency decay + memory_stats frequency boost) and optional Tier 1 vector cosine + RRF + MMR reranking.
func (s *Store) Search(ctx context.Context, opts SearchOpts) (*ListResponse, error) {
	if s == nil || s.db == nil {
		return &ListResponse{
			Backend:   BackendPebble,
			Installed: true,
			Available: false,
			Error:     "memory database not initialized",
			Items:     []Item{},
		}, nil
	}
	if _, err := s.SyncFromDisk(ctx); err != nil {
		return nil, err
	}

	normScope, err := NormalizeScope(opts.Scope, opts.BotID)
	if err != nil {
		return nil, err
	}
	opts.Scope = normScope
	opts.BotID = strings.TrimSpace(opts.BotID)
	if opts.Limit <= 0 {
		opts.Limit = 30
	}
	if opts.Limit > 100 {
		opts.Limit = 100
	}

	counts, err := s.queryCounts(ctx, opts.Scope, opts.BotID, opts.ExcludedBotIDs)
	if err != nil {
		return nil, err
	}
	magpie := s.DetectMagpieMemory(ctx)
	semantic := s.semanticStatus(ctx)

	query := strings.TrimSpace(opts.Query)
	if query == "" {
		items, err := s.browseDocs(ctx, opts)
		if err != nil {
			return nil, err
		}
		return &ListResponse{
			Backend:   BackendPebble,
			Installed: true,
			Available: true,
			Magpie:    &magpie,
			Semantic:  semantic,
			Counts:    counts,
			Items:     items,
		}, nil
	}

	candidates, vecActive, vecErr := s.retrieveCandidates(ctx, opts, query)
	if vecErr != "" && semantic != nil {
		semantic.Error = vecErr
	}

	ranked := s.rankAndRerank(ctx, candidates, vecActive, opts.Limit)

	// Record SQLite-only access stats for retrieved memory items (never written to Markdown).
	hitIDs := make([]string, 0, len(ranked))
	items := make([]Item, 0, len(ranked))
	for _, c := range ranked {
		it := c.item
		it.Score = math.Round(c.finalScore*10000) / 10000
		it.Snippet = buildSnippet(firstNonEmpty(c.bestChunk, it.Text), query, 240)
		if opts.Compact {
			it.Text = ""
		}
		items = append(items, it)
		hitIDs = append(hitIDs, it.ID)
	}
	s.recordAccess(ctx, hitIDs)

	if opts.IncludeSessions {
		sessLimit := min(max(opts.Limit/2, 5), 10)
		if sessHits := s.searchSessions(ctx, opts, query, sessLimit); len(sessHits) > 0 {
			for i := range sessHits {
				if opts.Compact {
					sessHits[i].Text = ""
				}
			}
			items = append(items, sessHits...)
			if len(items) > opts.Limit {
				items = items[:opts.Limit]
			}
		}
	}

	if items == nil {
		items = []Item{}
	}
	return &ListResponse{
		Backend:   BackendPebble,
		Installed: true,
		Available: true,
		Magpie:    &magpie,
		Semantic:  semantic,
		Counts:    counts,
		Items:     items,
	}, nil
}

func (s *Store) browseDocs(ctx context.Context, opts SearchOpts) ([]Item, error) {
	where, args := buildDocFilterSQL("d", opts)
	sqlStr := `
		SELECT ` + prefixColumns(docSelectCols, "d.") + `
		FROM memory_docs d
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY d.pin DESC, d.updated_at DESC, d.doc_id ASC
		LIMIT ?
	`
	args = append(args, opts.Limit)
	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		it, _, err := itemFromDocRow(rows)
		if err != nil {
			return nil, err
		}
		if !matchesAllTags(it.Tags, opts.Tags) {
			continue
		}
		it.Snippet = buildSnippet(it.Text, "", 240)
		if opts.Compact {
			it.Text = ""
		}
		out = append(out, it)
	}
	if out == nil {
		out = []Item{}
	}
	return out, rows.Err()
}

func (s *Store) retrieveCandidates(ctx context.Context, opts SearchOpts, query string) (map[string]*searchCandidate, bool, string) {
	where, baseArgs := buildDocFilterSQL("d", opts)
	filterClause := strings.Join(where, " AND ")
	cands := map[string]*searchCandidate{}

	upsert := func(it Item, docType, chunkText string, lexScore float64) {
		if !matchesAllTags(it.Tags, opts.Tags) {
			return
		}
		if cur, ok := cands[it.ID]; ok {
			if lexScore > cur.lexScore {
				cur.lexScore = lexScore
				if chunkText != "" {
					cur.bestChunk = chunkText
				}
			}
			return
		}
		cands[it.ID] = &searchCandidate{
			item:      it,
			docType:   docType,
			lexScore:  lexScore,
			bestChunk: chunkText,
		}
	}

	// 1. Unicode61 FTS5 (AND first, fallback to OR if multi-token AND finds nothing).
	uniHits := 0
	if ftsAnd := buildFTSMatchQuery(query, "AND"); ftsAnd != "" {
		uniHits = s.runFTSSearch(ctx, "memory_fts_uni", ftsAnd, filterClause, baseArgs, 60, upsert)
	}
	if uniHits == 0 {
		if ftsOr := buildFTSMatchQuery(query, "OR"); ftsOr != "" {
			s.runFTSSearch(ctx, "memory_fts_uni", ftsOr, filterClause, baseArgs, 60, upsert)
		}
	}

	// 2. Trigram FTS5 (CJK >= 3 runes, substrings, code identifiers).
	if triQ := buildTrigramMatchQuery(query); triQ != "" {
		s.runFTSSearch(ctx, "memory_fts_tri", triQ, filterClause, baseArgs, 60, upsert)
	}

	// 3. Short-token LIKE fallback (< 3 runes) or zero-hit rescue.
	if hasShortToken(query) || len(cands) == 0 {
		s.runLikeSearch(ctx, query, filterClause, baseArgs, 60, upsert)
	}

	// 4. Optional Tier 1 semantic cosine search.
	vecActive := false
	vecErr := ""
	if s.embedClient != nil && s.embedClient.Ready() {
		qVec, err := s.embedClient.EmbedQuery(ctx, query)
		if err != nil {
			vecErr = err.Error()
		} else if len(qVec) > 0 {
			_, _ = s.SyncEmbeddings(ctx)
			vecActive = s.runVectorScan(ctx, qVec, filterClause, baseArgs, opts.Tags, cands)
		}
	}

	return cands, vecActive, vecErr
}

func (s *Store) runFTSSearch(
	ctx context.Context,
	ftsTable, matchExpr, filterClause string,
	baseArgs []any,
	limit int,
	upsert func(Item, string, string, float64),
) int {
	sqlStr := `
		SELECT ` + prefixColumns(docSelectCols, "d.") + `, c.text, bm25(` + ftsTable + `, 0.0, 0.0, 2.0, 1.5, 1.0)
		FROM ` + ftsTable + ` f
		JOIN memory_chunks c ON c.chunk_id = f.chunk_id
		JOIN memory_docs d ON d.doc_id = f.doc_id
		WHERE ` + ftsTable + ` MATCH ? AND ` + filterClause + `
		ORDER BY bm25(` + ftsTable + `, 0.0, 0.0, 2.0, 1.5, 1.0) ASC
		LIMIT ?
	`
	args := make([]any, 0, len(baseArgs)+2)
	args = append(args, matchExpr)
	args = append(args, baseArgs...)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return 0
	}
	defer rows.Close()

	hits := 0
	for rows.Next() {
		var (
			it        Item
			docType   string
			tagsJSON  string
			pinInt    int
			chunkText string
			rawBM25   float64
		)
		if rows.Scan(
			&it.ID, &it.Path, &it.Scope, &it.BotID, &docType, &it.Kind, &it.Title,
			&it.Text, &it.Why, &tagsJSON, &it.Status, &pinInt, &it.Confidence,
			&it.ValidFrom, &it.ValidUntil, &it.SupersededBy, &it.SourceSession,
			&it.SourceAuthor, &it.CreatedAt, &it.UpdatedAt, &chunkText, &rawBM25,
		) != nil {
			continue
		}
		it.Pin = pinInt != 0
		it.Tags = decodeTagsJSON(tagsJSON)
		it.Origin = docTypeToOrigin(docType)
		score := math.Max(0.05, -rawBM25)
		if ftsTable == "memory_fts_tri" {
			score *= 0.92
		}
		upsert(it, docType, chunkText, score)
		hits++
	}
	return hits
}

func (s *Store) runLikeSearch(
	ctx context.Context,
	query, filterClause string,
	baseArgs []any,
	limit int,
	upsert func(Item, string, string, float64),
) {
	tokens := extractSearchTokens(query)
	if len(tokens) == 0 {
		return
	}
	conds := make([]string, 0, len(tokens))
	args := make([]any, 0, len(baseArgs)+len(tokens)*3+1)
	args = append(args, baseArgs...)
	for _, tok := range tokens {
		like := "%" + escapeLike(strings.ToLower(tok)) + "%"
		conds = append(conds, `(LOWER(d.title) LIKE ? ESCAPE '\' OR LOWER(d.tags_json) LIKE ? ESCAPE '\' OR LOWER(d.body) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	args = append(args, limit)

	sqlStr := `
		SELECT ` + prefixColumns(docSelectCols, "d.") + `
		FROM memory_docs d
		WHERE ` + filterClause + ` AND (` + strings.Join(conds, " AND ") + `)
		ORDER BY d.updated_at DESC
		LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		it, docType, err := itemFromDocRow(rows)
		if err != nil {
			continue
		}
		score := 0.8
		lowTitle := strings.ToLower(it.Title)
		for _, tok := range tokens {
			if strings.Contains(lowTitle, strings.ToLower(tok)) {
				score += 0.6
			}
		}
		upsert(it, docType, it.Text, score)
	}
}

func (s *Store) runVectorScan(
	ctx context.Context,
	queryVec []float32,
	filterClause string,
	baseArgs []any,
	reqTags []string,
	cands map[string]*searchCandidate,
) bool {
	model := s.embedClient.Model()
	args := make([]any, 0, len(baseArgs)+1)
	args = append(args, model)
	args = append(args, baseArgs...)

	sqlStr := `
		SELECT ` + prefixColumns(docSelectCols, "d.") + `, c.text, e.vec
		FROM memory_embeddings e
		JOIN memory_chunks c ON c.chunk_id = e.chunk_id
		JOIN memory_docs d ON d.doc_id = c.doc_id
		WHERE e.model = ? AND ` + filterClause + `
	`
	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return false
	}
	defer rows.Close()

	scanned := false
	for rows.Next() {
		var (
			it        Item
			docType   string
			tagsJSON  string
			pinInt    int
			chunkText string
			vecBlob   []byte
		)
		if rows.Scan(
			&it.ID, &it.Path, &it.Scope, &it.BotID, &docType, &it.Kind, &it.Title,
			&it.Text, &it.Why, &tagsJSON, &it.Status, &pinInt, &it.Confidence,
			&it.ValidFrom, &it.ValidUntil, &it.SupersededBy, &it.SourceSession,
			&it.SourceAuthor, &it.CreatedAt, &it.UpdatedAt, &chunkText, &vecBlob,
		) != nil {
			continue
		}
		it.Pin = pinInt != 0
		it.Tags = decodeTagsJSON(tagsJSON)
		if !matchesAllTags(it.Tags, reqTags) {
			continue
		}
		it.Origin = docTypeToOrigin(docType)
		vec := DecodeFloat32s(vecBlob)
		if len(vec) != len(queryVec) {
			continue
		}
		scanned = true
		sim := CosineSimilarity(queryVec, vec)
		if sim < 0.15 {
			continue
		}
		if cur, ok := cands[it.ID]; ok {
			if sim > cur.vecScore {
				cur.vecScore = sim
				cur.vec = vec
				if cur.bestChunk == "" {
					cur.bestChunk = chunkText
				}
			}
		} else {
			cands[it.ID] = &searchCandidate{
				item: it, docType: docType, vecScore: sim, bestChunk: chunkText, vec: vec,
			}
		}
	}
	return scanned
}
