package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Frontmatter stores the structured YAML metadata of a Pebble memory item file.
type Frontmatter struct {
	ID            string
	Scope         string
	BotID         string
	Kind          string
	Title         string
	Status        string
	Confidence    float64
	Pin           bool
	Tags          []string
	CreatedAt     string
	UpdatedAt     string
	ValidFrom     string
	ValidUntil    string
	SupersededBy  string
	SourceSession string
	SourceAuthor  string
	Why           string
}

// ParseMarkdownItem parses YAML frontmatter (delimited by ---) and Markdown body.
// If no frontmatter is present, it returns an empty Frontmatter and the full text as body.
func ParseMarkdownItem(raw string) (Frontmatter, string, bool) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return Frontmatter{}, strings.TrimSpace(normalized), false
	}
	rest := normalized[4:]
	endIdx := strings.Index(rest, "\n---\n")
	bodyStart := 0
	fmBlock := ""
	if endIdx >= 0 {
		fmBlock = rest[:endIdx]
		bodyStart = endIdx + 5
	} else if strings.HasSuffix(rest, "\n---") {
		fmBlock = rest[:len(rest)-4]
		bodyStart = len(rest)
	} else {
		return Frontmatter{}, strings.TrimSpace(normalized), false
	}

	fm := parseFrontmatterBlock(fmBlock)
	body := strings.TrimSpace(rest[bodyStart:])
	return fm, body, true
}

func parseFrontmatterBlock(block string) Frontmatter {
	var fm Frontmatter
	fm.Confidence = DefaultConfidence

	lines := strings.Split(block, "\n")
	activeListKey := ""
	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Indented list items (e.g. tags:\n  - foo) or nested Magpie sources blocks.
		if strings.HasPrefix(rawLine, " ") || strings.HasPrefix(rawLine, "\t") {
			if activeListKey == "tags" && strings.HasPrefix(trimmed, "-") {
				val := parseYAMLScalar(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
				if val != "" {
					fm.Tags = append(fm.Tags, val)
				}
			}
			continue
		}
		activeListKey = ""

		colonIdx := strings.IndexByte(rawLine, ':')
		if colonIdx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(rawLine[:colonIdx]))
		rawVal := strings.TrimSpace(rawLine[colonIdx+1:])

		switch key {
		case "id":
			fm.ID = parseYAMLScalar(rawVal)
		case "scope":
			fm.Scope = parseYAMLScalar(rawVal)
		case "bot_id", "botid":
			fm.BotID = parseYAMLScalar(rawVal)
		case "kind":
			fm.Kind = parseYAMLScalar(rawVal)
		case "title":
			fm.Title = parseYAMLScalar(rawVal)
		case "status":
			fm.Status = parseYAMLScalar(rawVal)
		case "confidence":
			if f, err := strconv.ParseFloat(parseYAMLScalar(rawVal), 64); err == nil {
				fm.Confidence = f
			}
		case "pin", "pinned":
			fm.Pin = strings.EqualFold(parseYAMLScalar(rawVal), "true")
		case "tags":
			if rawVal == "" {
				activeListKey = "tags"
			} else {
				fm.Tags = append(fm.Tags, parseYAMLList(rawVal)...)
			}
		case "created_at", "created":
			fm.CreatedAt = parseYAMLScalar(rawVal)
		case "updated_at", "updated":
			fm.UpdatedAt = parseYAMLScalar(rawVal)
		case "valid_from", "validfrom":
			fm.ValidFrom = parseYAMLScalar(rawVal)
		case "valid_until", "validuntil":
			fm.ValidUntil = parseYAMLScalar(rawVal)
		case "superseded_by", "supersededby":
			fm.SupersededBy = parseYAMLScalar(rawVal)
		case "source_session", "sourcesession":
			fm.SourceSession = parseYAMLScalar(rawVal)
		case "source_author", "sourceauthor", "origin":
			fm.SourceAuthor = parseYAMLScalar(rawVal)
		case "why":
			fm.Why = parseYAMLScalar(rawVal)
		default:
			if rawVal == "" {
				activeListKey = key
			}
		}
	}
	fm.Tags = NormalizeTags(fm.Tags)
	return fm
}

// RenderMarkdownItem renders a Pebble memory item to deterministic YAML frontmatter + Markdown body.
func RenderMarkdownItem(fm Frontmatter, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	writeYAMLField(&b, "id", fm.ID)
	writeYAMLField(&b, "scope", fm.Scope)
	if fm.BotID != "" {
		writeYAMLField(&b, "bot_id", fm.BotID)
	}
	writeYAMLField(&b, "kind", NormalizeKind(fm.Kind))
	writeYAMLField(&b, "title", fm.Title)
	writeYAMLField(&b, "status", NormalizeStatus(fm.Status))
	conf := fm.Confidence
	if conf <= 0 {
		conf = DefaultConfidence
	}
	b.WriteString(fmt.Sprintf("confidence: %s\n", strconv.FormatFloat(conf, 'f', -1, 64)))
	if fm.Pin {
		b.WriteString("pin: true\n")
	} else {
		b.WriteString("pin: false\n")
	}
	tags := NormalizeTags(fm.Tags)
	if len(tags) > 0 {
		quoted := make([]string, len(tags))
		for i, t := range tags {
			quoted[i] = formatYAMLScalar(t)
		}
		b.WriteString("tags: [" + strings.Join(quoted, ", ") + "]\n")
	} else {
		b.WriteString("tags: []\n")
	}
	if fm.CreatedAt != "" {
		writeYAMLField(&b, "created_at", fm.CreatedAt)
	}
	if fm.UpdatedAt != "" {
		writeYAMLField(&b, "updated_at", fm.UpdatedAt)
	}
	if fm.ValidFrom != "" {
		writeYAMLField(&b, "valid_from", fm.ValidFrom)
	}
	if fm.ValidUntil != "" {
		writeYAMLField(&b, "valid_until", fm.ValidUntil)
	}
	if fm.SupersededBy != "" {
		writeYAMLField(&b, "superseded_by", fm.SupersededBy)
	}
	if fm.SourceSession != "" {
		writeYAMLField(&b, "source_session", fm.SourceSession)
	}
	if fm.SourceAuthor != "" {
		writeYAMLField(&b, "source_author", NormalizeAuthor(fm.SourceAuthor))
	}
	if fm.Why != "" {
		writeYAMLField(&b, "why", fm.Why)
	}
	b.WriteString("---\n\n")
	trimmed := strings.TrimSpace(body)
	if trimmed != "" {
		b.WriteString(trimmed)
		b.WriteByte('\n')
	}
	return b.String()
}

func writeYAMLField(b *strings.Builder, key, val string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(formatYAMLScalar(val))
	b.WriteByte('\n')
}

func formatYAMLScalar(s string) string {
	if s == "" {
		return `""`
	}
	needsQuote := strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") ||
		strings.ContainsAny(s, ":#[]{}\n\r\t\"'&*!|>%@`?,") ||
		strings.EqualFold(s, "true") || strings.EqualFold(s, "false") ||
		strings.EqualFold(s, "null") || strings.EqualFold(s, "~")
	if !needsQuote {
		// Quote strings that look like numbers or ISO timestamps so parsers keep them strings.
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			needsQuote = true
		}
	}
	if !needsQuote {
		return s
	}
	return strconv.Quote(s)
}

func parseYAMLScalar(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s == "null" || s == "~" {
		return ""
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if unq, err := strconv.Unquote(s); err == nil {
			return unq
		}
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

func parseYAMLList(raw string) []string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		val := parseYAMLScalar(s)
		if val == "" {
			return nil
		}
		return []string{val}
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	inDouble := false
	inSingle := false
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case inDouble:
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(inner) {
				i++
				cur.WriteByte(inner[i])
			} else if c == '"' {
				inDouble = false
			}
		case inSingle:
			cur.WriteByte(c)
			if c == '\'' {
				if i+1 < len(inner) && inner[i+1] == '\'' {
					i++
					cur.WriteByte(inner[i])
				} else {
					inSingle = false
				}
			}
		case c == '"':
			inDouble = true
			cur.WriteByte(c)
		case c == '\'':
			inSingle = true
			cur.WriteByte(c)
		case c == ',':
			if v := parseYAMLScalar(cur.String()); v != "" {
				out = append(out, v)
			}
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if v := parseYAMLScalar(cur.String()); v != "" {
		out = append(out, v)
	}
	return out
}

// AtomicWriteFile writes data to path atomically via a same-directory temporary file and rename.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pebble-mem-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
