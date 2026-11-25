package state

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

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
