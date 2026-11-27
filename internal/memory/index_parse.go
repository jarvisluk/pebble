package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const docSelectCols = `doc_id, path, scope, bot_id, doc_type, kind, title, body, why, tags_json, status, pin, confidence, valid_from, valid_until, superseded_by, source_session, source_author, created_at, updated_at`

func parseDiskDoc(meta diskFileMeta, raw, sum string) parsedDoc {
	mtimeISO := time.Unix(0, meta.mtime).UTC().Format(time.RFC3339)
	doc := parsedDoc{
		relPath:    meta.relPath,
		scope:      meta.scope,
		botID:      meta.botID,
		docType:    meta.docType,
		status:     StatusActive,
		confidence: DefaultConfidence,
		createdAt:  mtimeISO,
		updatedAt:  mtimeISO,
		mtime:      meta.mtime,
		size:       meta.size,
		sha256:     sum,
	}

	bodyOffset := 1
	switch meta.docType {
	case DocTypeItem:
		fm, body, hasFM := ParseMarkdownItem(raw)
		if hasFM {
			bodyOffset = frontmatterBodyStartLine(raw)
			doc.docID = strings.TrimSpace(fm.ID)
			if fm.Scope == ScopeGlobal || fm.Scope == ScopeBot {
				doc.scope = fm.Scope
			}
			if fm.BotID != "" && doc.scope == ScopeBot {
				doc.botID = fm.BotID
			}
			doc.kind = NormalizeKind(fm.Kind)
			doc.title = DeriveTitle(fm.Title, body)
			doc.body = body
			doc.why = strings.TrimSpace(fm.Why)
			doc.tags = NormalizeTags(fm.Tags)
			doc.status = NormalizeStatus(fm.Status)
			doc.pin = fm.Pin
			if fm.Confidence > 0 {
				doc.confidence = fm.Confidence
			}
			doc.validFrom = strings.TrimSpace(fm.ValidFrom)
			doc.validUntil = strings.TrimSpace(fm.ValidUntil)
			doc.supersededBy = strings.TrimSpace(fm.SupersededBy)
			doc.sourceSession = strings.TrimSpace(fm.SourceSession)
			doc.sourceAuthor = NormalizeAuthor(fm.SourceAuthor)
			if fm.CreatedAt != "" {
				doc.createdAt = fm.CreatedAt
			}
			if fm.UpdatedAt != "" {
				doc.updatedAt = fm.UpdatedAt
			}
		} else {
			doc.kind = KindFact
			doc.body = strings.TrimSpace(raw)
			doc.title = DeriveTitle(strings.TrimSuffix(filepath.Base(meta.relPath), ".md"), doc.body)
			doc.sourceAuthor = AuthorUser
		}
		if doc.docID == "" {
			doc.docID = "file_" + sha256Hex([]byte(meta.relPath))[:12]
		}
	case DocTypeIndex:
		doc.docID = indexDocID(meta.scope, meta.botID)
		doc.kind = OriginIndex
		doc.title = "MEMORY.md"
		doc.body = strings.TrimSpace(raw)
		doc.sourceAuthor = AuthorUser
	case DocTypeDaily:
		date := strings.TrimSuffix(filepath.Base(meta.relPath), ".md")
		doc.docID = dailyDocID(meta.scope, meta.botID, date)
		doc.kind = KindEpisode
		doc.title = "Daily log " + date
		doc.body = strings.TrimSpace(raw)
		doc.sourceAuthor = AuthorAgent
	case DocTypeDream:
		date := strings.TrimSuffix(filepath.Base(meta.relPath), ".md")
		doc.docID = dreamDocID(meta.scope, meta.botID, date)
		doc.kind = KindEpisode
		doc.title = "Dream diary " + date
		doc.body = strings.TrimSpace(raw)
		doc.sourceAuthor = AuthorDream
	}

	doc.chunks = chunkMarkdown(doc.docID, doc.body, bodyOffset)
	return doc
}

func (s *Store) upsertParsedDocLocked(ctx context.Context, doc parsedDoc) error {
	preserved := map[string]savedEmbedding{}
	if rows, err := s.db.QueryContext(ctx, `
		SELECT e.chunk_id, e.model, e.dim, e.vec, e.updated_at, c.sha256
		FROM memory_embeddings e
		JOIN memory_chunks c ON c.chunk_id = e.chunk_id
		WHERE c.doc_id = ?
	`, doc.docID); err == nil {
		for rows.Next() {
			var cid string
			var se savedEmbedding
			if rows.Scan(&cid, &se.model, &se.dim, &se.vec, &se.updatedAt, &se.chunkSHA) == nil {
				preserved[cid] = se
			}
		}
		_ = rows.Close()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var oldDocID string
	if tx.QueryRowContext(ctx, `SELECT doc_id FROM memory_docs WHERE path = ? AND doc_id != ?`, doc.relPath, doc.docID).Scan(&oldDocID) == nil && oldDocID != "" {
		if err := deleteDocTx(ctx, tx, oldDocID); err != nil {
			return err
		}
	}

	tagsJSON := "[]"
	if len(doc.tags) > 0 {
		if raw, err := json.Marshal(doc.tags); err == nil {
			tagsJSON = string(raw)
		}
	}
	pinInt := 0
	if doc.pin {
		pinInt = 1
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_docs(
			doc_id, path, scope, bot_id, doc_type, kind, title, body, why, tags_json,
			status, pin, confidence, valid_from, valid_until, superseded_by,
			source_session, source_author, created_at, updated_at, mtime_unix, size, sha256
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(doc_id) DO UPDATE SET
			path = excluded.path, scope = excluded.scope, bot_id = excluded.bot_id,
			doc_type = excluded.doc_type, kind = excluded.kind, title = excluded.title,
			body = excluded.body, why = excluded.why, tags_json = excluded.tags_json,
			status = excluded.status, pin = excluded.pin, confidence = excluded.confidence,
			valid_from = excluded.valid_from, valid_until = excluded.valid_until,
			superseded_by = excluded.superseded_by, source_session = excluded.source_session,
			source_author = excluded.source_author, created_at = excluded.created_at,
			updated_at = excluded.updated_at, mtime_unix = excluded.mtime_unix,
			size = excluded.size, sha256 = excluded.sha256
	`,
		doc.docID, doc.relPath, doc.scope, doc.botID, doc.docType, doc.kind,
		doc.title, doc.body, doc.why, tagsJSON, doc.status, pinInt, doc.confidence,
		doc.validFrom, doc.validUntil, doc.supersededBy, doc.sourceSession,
		doc.sourceAuthor, doc.createdAt, doc.updatedAt, doc.mtime, doc.size, doc.sha256,
	); err != nil {
		return err
	}

	for _, delSQL := range []string{
		`DELETE FROM memory_fts_uni WHERE doc_id = ?`,
		`DELETE FROM memory_fts_tri WHERE doc_id = ?`,
		`DELETE FROM memory_chunks WHERE doc_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, delSQL, doc.docID); err != nil {
			return err
		}
	}

	tagsText := strings.Join(doc.tags, " ")
	for _, ch := range doc.chunks {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_chunks(chunk_id, doc_id, chunk_index, heading, start_line, end_line, text, sha256)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, ch.chunkID, doc.docID, ch.chunkIndex, ch.heading, ch.startLine, ch.endLine, ch.text, ch.sha256); err != nil {
			return err
		}
		ftsText := ch.text
		if ch.heading != "" && !strings.Contains(ftsText, ch.heading) {
			ftsText = ch.heading + "\n" + ftsText
		}
		if doc.why != "" && ch.chunkIndex == 0 {
			ftsText += "\n" + doc.why
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_fts_uni(chunk_id, doc_id, title, tags, text) VALUES (?, ?, ?, ?, ?)`,
			ch.chunkID, doc.docID, doc.title, tagsText, ftsText); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_fts_tri(chunk_id, doc_id, title, tags, text) VALUES (?, ?, ?, ?, ?)`,
			ch.chunkID, doc.docID, doc.title, tagsText, ftsText); err != nil {
			return err
		}
		if prevEmb, ok := preserved[ch.chunkID]; ok && prevEmb.chunkSHA == ch.sha256 {
			_, _ = tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO memory_embeddings(chunk_id, model, dim, vec, updated_at)
				VALUES (?, ?, ?, ?, ?)
			`, ch.chunkID, prevEmb.model, prevEmb.dim, prevEmb.vec, prevEmb.updatedAt)
		}
	}
	return tx.Commit()
}

func (s *Store) deleteDocFromIndexLocked(ctx context.Context, docID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := deleteDocTx(ctx, tx, docID); err != nil {
		return err
	}
	return tx.Commit()
}

func deleteDocTx(ctx context.Context, tx *sql.Tx, docID string) error {
	for _, stmt := range []string{
		`DELETE FROM memory_fts_uni WHERE doc_id = ?`,
		`DELETE FROM memory_fts_tri WHERE doc_id = ?`,
		`DELETE FROM memory_chunks WHERE doc_id = ?`,
		`DELETE FROM memory_docs WHERE doc_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, docID); err != nil {
			return err
		}
	}
	return nil
}

func frontmatterBodyStartLine(raw string) int {
	norm := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(norm, "---\n") {
		return 1
	}
	endIdx := strings.Index(norm[4:], "\n---\n")
	if endIdx < 0 {
		return 1
	}
	return 1 + strings.Count(norm[:4+endIdx+5], "\n")
}

func chunkMarkdown(docID, body string, startLineOffset int) []docChunk {
	const maxChunkRunes = 800
	if startLineOffset <= 0 {
		startLineOffset = 1
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return []docChunk{{
			chunkID: docID + "#0", chunkIndex: 0, startLine: startLineOffset, endLine: startLineOffset,
			text: "", sha256: sha256Hex(nil),
		}}
	}

	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var chunks []docChunk
	var buf []string
	curHeading := ""
	curStart := startLineOffset
	curRunes := 0

	flush := func(endLine int) {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		if text == "" {
			buf = nil
			curRunes = 0
			return
		}
		idx := len(chunks)
		chunks = append(chunks, docChunk{
			chunkID:    docID + "#" + strconv.Itoa(idx),
			chunkIndex: idx,
			heading:    curHeading,
			startLine:  curStart,
			endLine:    endLine,
			text:       text,
			sha256:     sha256Hex([]byte(curHeading + "\n" + text)),
		})
		buf = nil
		curRunes = 0
	}

	for i, line := range lines {
		lineNo := startLineOffset + i
		trimmedLine := strings.TrimSpace(line)
		isHeading := strings.HasPrefix(trimmedLine, "#") && len(trimmedLine) > 1 && (trimmedLine[1] == ' ' || trimmedLine[1] == '#')
		if isHeading {
			if len(buf) > 0 {
				flush(lineNo - 1)
			}
			curHeading = strings.TrimSpace(strings.TrimLeft(trimmedLine, "# "))
			curStart = lineNo
			buf = append(buf, line)
			curRunes += len([]rune(line))
			continue
		}
		lineRunes := len([]rune(line))
		if len(buf) > 0 && curRunes+lineRunes+1 > maxChunkRunes && trimmedLine == "" {
			flush(lineNo - 1)
			curStart = lineNo + 1
			continue
		}
		if len(buf) > 0 && curRunes+lineRunes+1 > maxChunkRunes+200 {
			flush(lineNo - 1)
			curStart = lineNo
		}
		if len(buf) == 0 {
			curStart = lineNo
		}
		buf = append(buf, line)
		curRunes += lineRunes + 1
	}
	if len(buf) > 0 {
		flush(startLineOffset + len(lines) - 1)
	}
	if len(chunks) == 0 {
		chunks = append(chunks, docChunk{
			chunkID: docID + "#0", chunkIndex: 0, startLine: startLineOffset,
			endLine: startLineOffset + len(lines) - 1, text: trimmed, sha256: sha256Hex([]byte(trimmed)),
		})
	}
	return chunks
}

func indexDocID(scope, botID string) string {
	if scope == ScopeGlobal {
		return "index:global"
	}
	return "index:bot:" + botID
}

func dailyDocID(scope, botID, date string) string {
	if scope == ScopeGlobal {
		return "daily:global:" + date
	}
	return "daily:bot:" + botID + ":" + date
}

func dreamDocID(scope, botID, date string) string {
	if scope == ScopeGlobal {
		return "dream:global:" + date
	}
	return "dream:bot:" + botID + ":" + date
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func decodeTagsJSON(raw string) []string {
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

func docTypeToOrigin(docType string) string {
	switch docType {
	case DocTypeDaily:
		return OriginDaily
	case DocTypeDream:
		return OriginDream
	case DocTypeIndex:
		return OriginIndex
	default:
		return OriginMemory
	}
}

func itemFromDocRow(scanner interface{ Scan(...any) error }) (Item, string, error) {
	var (
		it       Item
		docType  string
		tagsJSON string
		pinInt   int
	)
	if err := scanner.Scan(
		&it.ID, &it.Path, &it.Scope, &it.BotID, &docType, &it.Kind, &it.Title,
		&it.Text, &it.Why, &tagsJSON, &it.Status, &pinInt, &it.Confidence,
		&it.ValidFrom, &it.ValidUntil, &it.SupersededBy, &it.SourceSession,
		&it.SourceAuthor, &it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return Item{}, "", err
	}
	it.Pin = pinInt != 0
	it.Tags = decodeTagsJSON(tagsJSON)
	it.Origin = docTypeToOrigin(docType)
	return it, docType, nil
}

func fmtDocCountQuery(db *sql.DB, ctx context.Context, table string) int {
	var n int
	_ = db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&n)
	return n
}
