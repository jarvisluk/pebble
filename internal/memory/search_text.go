package memory

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func matchesAllTags(itemTags, reqTags []string) bool {
	if len(reqTags) == 0 {
		return true
	}
	have := make(map[string]bool, len(itemTags))
	for _, t := range itemTags {
		have[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(t), "#"))] = true
	}
	for _, want := range reqTags {
		if w := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(want), "#")); w != "" && !have[w] {
			return false
		}
	}
	return true
}

func extractSearchTokens(raw string) []string {
	var tokens []string
	for _, f := range strings.Fields(strings.TrimSpace(raw)) {
		clean := strings.TrimFunc(f, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-' && r != '.' && r != ':'
		})
		if clean != "" {
			tokens = append(tokens, clean)
		}
	}
	return tokens
}

func buildFTSMatchQuery(raw, op string) string {
	tokens := extractSearchTokens(raw)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		parts[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(parts, " "+op+" ")
}

func buildTrigramMatchQuery(raw string) string {
	var parts []string
	for _, t := range extractSearchTokens(raw) {
		if utf8.RuneCountInString(t) >= 3 {
			parts = append(parts, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
		}
	}
	return strings.Join(parts, " AND ")
}

func hasShortToken(raw string) bool {
	for _, t := range extractSearchTokens(raw) {
		if utf8.RuneCountInString(t) < 3 {
			return true
		}
	}
	return false
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func prefixColumns(cols, prefix string) string {
	parts := strings.Split(cols, ",")
	for i := range parts {
		if trimmed := strings.TrimSpace(parts[i]); trimmed != "" {
			parts[i] = prefix + trimmed
		}
	}
	return strings.Join(parts, ", ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func buildSnippet(text, query string, maxRunes int) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if clean == "" {
		return ""
	}
	runes := []rune(clean)
	if len(runes) <= maxRunes {
		return clean
	}
	lowerClean := strings.ToLower(clean)
	for _, tok := range extractSearchTokens(query) {
		if byteIdx := strings.Index(lowerClean, strings.ToLower(tok)); byteIdx >= 0 {
			runeIdx := utf8.RuneCountInString(lowerClean[:byteIdx])
			start := max(0, runeIdx-maxRunes/2)
			end := start + maxRunes
			if end > len(runes) {
				end = len(runes)
				start = max(0, end-maxRunes)
			}
			prefix, suffix := "", ""
			if start > 0 {
				prefix = "..."
			}
			if end < len(runes) {
				suffix = "..."
			}
			return prefix + string(runes[start:end]) + suffix
		}
	}
	return string(runes[:maxRunes-3]) + "..."
}

func textNgrams(s string) map[string]struct{} {
	norm := normalizeForFingerprint(s)
	if norm == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, w := range strings.Fields(norm) {
		runes := []rune(w)
		if hasCJKRunes(runes) {
			if len(runes) == 1 {
				out[string(runes)] = struct{}{}
			}
			for i := 0; i+2 <= len(runes); i++ {
				out[string(runes[i:i+2])] = struct{}{}
			}
		} else {
			out[w] = struct{}{}
		}
	}
	return out
}

func hasCJKRunes(runes []rune) bool {
	for _, r := range runes {
		if (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) {
			return true
		}
	}
	return false
}

func jaccardSets(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}
