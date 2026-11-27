package memory

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
)

func (s *Store) rankAndRerank(ctx context.Context, cands map[string]*searchCandidate, vecActive bool, limit int) []*searchCandidate {
	if len(cands) == 0 {
		return nil
	}

	statsMap := s.loadAccessCounts(ctx, cands)

	var lexList, vecList []*searchCandidate
	for _, c := range cands {
		c.accessCount = statsMap[c.item.ID]
		if c.lexScore > 0 {
			lexList = append(lexList, c)
		}
		if c.vecScore > 0 {
			vecList = append(vecList, c)
		}
	}
	sort.Slice(lexList, func(i, j int) bool {
		if lexList[i].lexScore != lexList[j].lexScore {
			return lexList[i].lexScore > lexList[j].lexScore
		}
		return lexList[i].item.UpdatedAt > lexList[j].item.UpdatedAt
	})
	sort.Slice(vecList, func(i, j int) bool {
		if vecList[i].vecScore != vecList[j].vecScore {
			return vecList[i].vecScore > vecList[j].vecScore
		}
		return vecList[i].item.UpdatedAt > vecList[j].item.UpdatedAt
	})

	const rrfK = 60.0
	rrf := make(map[string]float64, len(cands))
	for i, c := range lexList {
		rrf[c.item.ID] += 1.0 / (rrfK + float64(i+1))
	}
	if vecActive {
		for i, c := range vecList {
			rrf[c.item.ID] += 1.0 / (rrfK + float64(i+1))
		}
	}

	now := s.now().UTC()
	pool := make([]*searchCandidate, 0, len(cands))
	for _, c := range cands {
		base := rrf[c.item.ID]
		if !vecActive && c.lexScore > 0 {
			base *= 1.0 + 0.25*math.Log1p(c.lexScore)
		}
		if vecActive && c.vecScore > 0 {
			base *= 1.0 + 0.15*c.vecScore
		}
		recency := recencyFactor(c.item, now)
		pinBoost := 1.0
		if c.item.Pin {
			pinBoost = 1.20
		}
		freqBoost := math.Min(1.40, 1.0+0.08*math.Log1p(float64(c.accessCount)))
		c.finalScore = base * recency * pinBoost * freqBoost
		pool = append(pool, c)
	}

	sort.Slice(pool, func(i, j int) bool {
		if pool[i].finalScore != pool[j].finalScore {
			return pool[i].finalScore > pool[j].finalScore
		}
		return pool[i].item.UpdatedAt > pool[j].item.UpdatedAt
	})

	if len(pool) <= 1 {
		return pool
	}
	const lambda = 0.75
	maxScore := pool[0].finalScore
	if maxScore <= 0 {
		maxScore = 1
	}

	selected := make([]*searchCandidate, 0, min(limit, len(pool)))
	used := make([]bool, len(pool))
	selected = append(selected, pool[0])
	used[0] = true

	for len(selected) < limit && len(selected) < len(pool) {
		bestIdx := -1
		bestVal := -math.MaxFloat64
		for i, cand := range pool {
			if used[i] {
				continue
			}
			rel := cand.finalScore / maxScore
			maxSim := 0.0
			for _, sel := range selected {
				if sim := candidateSimilarity(cand, sel); sim > maxSim {
					maxSim = sim
				}
			}
			val := lambda*rel - (1.0-lambda)*maxSim
			if val > bestVal {
				bestVal = val
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			break
		}
		used[bestIdx] = true
		selected = append(selected, pool[bestIdx])
	}
	return selected
}

func (s *Store) loadAccessCounts(ctx context.Context, cands map[string]*searchCandidate) map[string]int {
	out := make(map[string]int, len(cands))
	if s.db == nil || len(cands) == 0 {
		return out
	}
	placeholders := make([]string, 0, len(cands))
	args := make([]any, 0, len(cands))
	for id := range cands {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT doc_id, access_count FROM memory_stats WHERE doc_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var cnt int
		if rows.Scan(&id, &cnt) == nil {
			out[id] = cnt
		}
	}
	return out
}

func (s *Store) searchSessions(ctx context.Context, opts SearchOpts, query string, limit int) []Item {
	if s.db == nil || limit <= 0 {
		return nil
	}
	where := []string{"m.text != ''"}
	var args []any

	if opts.Scope == ScopeBot && opts.BotID != "" && opts.BotID != "*" {
		where = append(where, "m.bot_id = ?")
		args = append(args, opts.BotID)
	}
	if len(opts.ExcludedBotIDs) > 0 {
		ph := make([]string, len(opts.ExcludedBotIDs))
		for i, id := range opts.ExcludedBotIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		where = append(where, "m.bot_id NOT IN ("+strings.Join(ph, ",")+")")
	}

	if triQ := buildTrigramMatchQuery(query); triQ != "" && !hasShortToken(query) {
		where = append(where, `m.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)`)
		args = append(args, triQ)
	} else {
		where = append(where, `LOWER(m.text) LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(strings.ToLower(query))+"%")
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, `
		SELECT m.msg_uid, m.session_id, m.bot_id, m.role, m.text, m.created_at
		FROM messages m
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY m.id DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		var msgUID, sessID, botID, role, text, createdAt string
		if rows.Scan(&msgUID, &sessID, &botID, &role, &text, &createdAt) != nil {
			continue
		}
		out = append(out, Item{
			ID:            "session:" + sessID + ":" + msgUID,
			Scope:         ScopeBot,
			BotID:         botID,
			Kind:          KindEpisode,
			Title:         "Session " + sessID + " (" + role + ")",
			Text:          text,
			Snippet:       buildSnippet(text, query, 240),
			Status:        StatusActive,
			SourceSession: sessID,
			Origin:        OriginSession,
			Score:         0.01,
			CreatedAt:     createdAt,
			UpdatedAt:     createdAt,
		})
	}
	return out
}

// FindSimilar returns active items in the same scope/botId with high lexical overlap (>= 0.45).
func (s *Store) FindSimilar(ctx context.Context, scope, botID, title, text string, limit int) ([]Item, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if _, err := s.SyncFromDisk(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	targetGrams := textNgrams(title + " " + text)
	if len(targetGrams) == 0 {
		return nil, nil
	}

	where := []string{"doc_type = ?", "status = ?", "scope = ?"}
	args := []any{DocTypeItem, StatusActive, scope}
	if scope == ScopeBot {
		where = append(where, "bot_id = ?")
		args = append(args, botID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+docSelectCols+`
		FROM memory_docs
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY updated_at DESC
		LIMIT 200
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		item Item
		sim  float64
	}
	var hits []scored
	for rows.Next() {
		it, _, err := itemFromDocRow(rows)
		if err != nil {
			continue
		}
		if sim := jaccardSets(targetGrams, textNgrams(it.Title+" "+it.Text)); sim >= 0.45 {
			it.Score = math.Round(sim*1000) / 1000
			it.Snippet = buildSnippet(it.Text, "", 200)
			hits = append(hits, scored{item: it, sim: sim})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].sim > hits[j].sim })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Item, len(hits))
	for i, h := range hits {
		out[i] = h.item
	}
	return out, nil
}

func (s *Store) queryCounts(ctx context.Context, scope, botID string, excludedBotIDs []string) (Counts, error) {
	var c Counts
	if s.db == nil {
		return c, nil
	}
	where := []string{"1=1"}
	var args []any
	if scope == ScopeGlobal {
		where = append(where, "scope = ?")
		args = append(args, ScopeGlobal)
	} else if scope == ScopeBot && botID != "" && botID != "*" {
		where = append(where, "scope = ? AND bot_id = ?")
		args = append(args, ScopeBot, botID)
	}
	if len(excludedBotIDs) > 0 {
		ph := make([]string, len(excludedBotIDs))
		for i, id := range excludedBotIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		where = append(where, "(scope = 'global' OR bot_id NOT IN ("+strings.Join(ph, ",")+"))")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT doc_type, scope, status, COUNT(*)
		FROM memory_docs
		WHERE `+strings.Join(where, " AND ")+`
		GROUP BY doc_type, scope, status
	`, args...)
	if err != nil {
		return c, err
	}
	defer rows.Close()

	for rows.Next() {
		var docType, sc, st string
		var n int
		if rows.Scan(&docType, &sc, &st, &n) != nil {
			continue
		}
		switch docType {
		case DocTypeDaily:
			c.Daily += n
		case DocTypeDream:
			c.Dream += n
		case DocTypeItem:
			c.Total += n
			switch st {
			case StatusActive:
				c.Active += n
			case StatusCandidate:
				c.Candidate += n
			case StatusSuperseded:
				c.Superseded += n
			case StatusArchived:
				c.Archived += n
			}
			if sc == ScopeGlobal {
				c.Global += n
			} else {
				c.Bot += n
			}
		}
	}
	return c, rows.Err()
}

func buildDocFilterSQL(alias string, opts SearchOpts) ([]string, []any) {
	var where []string
	var args []any

	if opts.IncludeDaily {
		where = append(where, alias+".doc_type IN (?, ?)")
		args = append(args, DocTypeItem, DocTypeDaily)
	} else {
		where = append(where, alias+".doc_type = ?")
		args = append(args, DocTypeItem)
	}

	switch opts.Scope {
	case ScopeGlobal:
		where = append(where, alias+".scope = ?")
		args = append(args, ScopeGlobal)
	case ScopeBot:
		if opts.BotID != "" && opts.BotID != "*" {
			where = append(where, alias+".scope = ? AND "+alias+".bot_id = ?")
			args = append(args, ScopeBot, opts.BotID)
		} else {
			where = append(where, alias+".scope = ?")
			args = append(args, ScopeBot)
		}
	case ScopeAll:
		if opts.BotID != "" && opts.BotID != "*" {
			where = append(where, "("+alias+".scope = ? OR ("+alias+".scope = ? AND "+alias+".bot_id = ?))")
			args = append(args, ScopeGlobal, ScopeBot, opts.BotID)
		}
	}

	if len(opts.ExcludedBotIDs) > 0 {
		ph := make([]string, len(opts.ExcludedBotIDs))
		for i, id := range opts.ExcludedBotIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		where = append(where, "("+alias+".scope = 'global' OR "+alias+".bot_id NOT IN ("+strings.Join(ph, ",")+"))")
	}
	if k := strings.TrimSpace(opts.Kind); k != "" {
		where = append(where, alias+".kind = ?")
		args = append(args, NormalizeKind(k))
	}

	st := strings.ToLower(strings.TrimSpace(opts.Status))
	if st == "" {
		st = StatusActive
	}
	if st != "all" {
		parts := strings.Split(st, ",")
		if len(parts) == 1 {
			where = append(where, alias+".status = ?")
			args = append(args, NormalizeStatus(parts[0]))
		} else {
			var ph []string
			for _, p := range parts {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					ph = append(ph, "?")
					args = append(args, NormalizeStatus(trimmed))
				}
			}
			if len(ph) > 0 {
				where = append(where, alias+".status IN ("+strings.Join(ph, ",")+")")
			}
		}
	}
	return where, args
}

func recencyFactor(it Item, now time.Time) float64 {
	if IsRecencyExempt(it.Pin, it.Kind) {
		return 1.0
	}
	ts, err := time.Parse(time.RFC3339, firstNonEmpty(it.UpdatedAt, it.CreatedAt))
	if err != nil {
		return 0.85
	}
	ageDays := now.Sub(ts).Hours() / 24.0
	if ageDays <= 0 {
		return 1.0
	}
	return 0.5 + 0.5*math.Exp(-ageDays/90.0)
}

func candidateSimilarity(a, b *searchCandidate) float64 {
	if len(a.vec) > 0 && len(a.vec) == len(b.vec) {
		return math.Max(0, CosineSimilarity(a.vec, b.vec))
	}
	return jaccardSets(textNgrams(a.item.Title+" "+a.item.Text), textNgrams(b.item.Title+" "+b.item.Text))
}
