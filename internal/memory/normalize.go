package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// NormalizeScope normalizes scope values ("global", "bot", "all").
func NormalizeScope(scope, botID string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(scope))
	b := strings.TrimSpace(botID)
	switch s {
	case "":
		if b != "" && b != "*" {
			return ScopeBot, nil
		}
		return ScopeGlobal, nil
	case ScopeGlobal:
		if b != "" && b != "*" {
			return ScopeBot, nil
		}
		return ScopeGlobal, nil
	case ScopeBot, "project":
		return ScopeBot, nil
	case ScopeAll:
		return ScopeAll, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidScope, scope)
	}
}

// NormalizeKind normalizes memory kind into one of the canonical values.
func NormalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindProfile:
		return KindProfile
	case KindPreference, "pref":
		return KindPreference
	case KindRule, "policy", "constraint":
		return KindRule
	case KindDecision:
		return KindDecision
	case KindProcedure, "workflow", "runbook", "howto":
		return KindProcedure
	case KindReference, "ref", "link":
		return KindReference
	case KindEpisode, "event", "incident", "log":
		return KindEpisode
	case KindFact, "":
		return KindFact
	default:
		return KindFact
	}
}

// NormalizeStatus normalizes item status.
func NormalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusCandidate:
		return StatusCandidate
	case StatusSuperseded:
		return StatusSuperseded
	case StatusArchived:
		return StatusArchived
	case StatusActive, "":
		return StatusActive
	default:
		return StatusActive
	}
}

// NormalizeAuthor normalizes source_author.
func NormalizeAuthor(author string) string {
	switch strings.ToLower(strings.TrimSpace(author)) {
	case AuthorUser:
		return AuthorUser
	case AuthorDream:
		return AuthorDream
	case AuthorMagpieImport:
		return AuthorMagpieImport
	case AuthorAgent, "":
		return AuthorAgent
	default:
		return AuthorAgent
	}
}

// NormalizeCoexistence normalizes coexistence policy.
func NormalizeCoexistence(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case CoexistencePebbleOnly:
		return CoexistencePebbleOnly
	case CoexistenceDual:
		return CoexistenceDual
	default:
		return CoexistenceAuto
	}
}

// NormalizeTags cleans, deduplicates, and preserves order of tags.
func NormalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		t := strings.TrimSpace(strings.TrimPrefix(raw, "#"))
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// IsRecencyExempt reports whether a memory item should not decay over time.
func IsRecencyExempt(pin bool, kind string) bool {
	if pin {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindRule, KindPreference, KindProfile:
		return true
	default:
		return false
	}
}

// Slugify converts a title or fallback text into a safe ASCII filename slug.
func Slugify(text string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
		if b.Len() >= 36 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "note"
	}
	return slug
}

// DeriveTitle returns an explicit title or derives a concise one from body text.
func DeriveTitle(title, text string) string {
	if t := strings.TrimSpace(title); t != "" {
		return truncateRunes(strings.Join(strings.Fields(t), " "), 96)
	}
	for _, line := range strings.Split(text, "\n") {
		clean := strings.TrimSpace(strings.TrimLeft(line, "#-*•> "))
		if clean == "" {
			continue
		}
		return truncateRunes(strings.Join(strings.Fields(clean), " "), 96)
	}
	return "Untitled memory"
}

// Fingerprint produces a deterministic content hash for dismissal and dedup checks.
func Fingerprint(scope, botID, title, text string) string {
	norm := normalizeForFingerprint(title + " " + text)
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(scope)) + "|" + strings.TrimSpace(botID) + "|" + norm))
	return hex.EncodeToString(h[:16])
}

func normalizeForFingerprint(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "..."
}

func validateBotID(botID string) error {
	id := strings.TrimSpace(botID)
	if id == "" {
		return fmt.Errorf("%w: botId is required for bot scope", ErrInvalidRequest)
	}
	if id == "_shared" || strings.Contains(id, "/") || strings.Contains(id, `\`) || strings.Contains(id, "..") {
		return fmt.Errorf("%w: invalid botId %q", ErrInvalidRequest, botID)
	}
	return nil
}

func (c Config) summary() ConfigSummary {
	maxChars := c.SnapshotMaxChars
	if maxChars <= 0 {
		maxChars = DefaultSnapshotMaxChars
	}
	return ConfigSummary{
		Coexistence:      NormalizeCoexistence(c.Coexistence),
		SnapshotMaxChars: maxChars,
		DreamEnabled:     c.DreamEnabled,
		DreamModel:       strings.TrimSpace(c.DreamModel),
		SemanticEnabled:  c.Semantic.Enabled,
		SemanticBaseURL:  strings.TrimSpace(c.Semantic.BaseURL),
		SemanticModel:    strings.TrimSpace(c.Semantic.Model),
		SemanticHasKey:   strings.TrimSpace(c.Semantic.APIKey) != "",
	}
}

func sliceLines(raw string, startLine, endLine int) (content string, outStart, outEnd, total int, truncated bool) {
	norm := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(norm, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total = len(lines)
	if total == 0 {
		return "", 0, 0, 0, false
	}
	s := startLine
	if s <= 0 {
		s = 1
	}
	e := endLine
	if e <= 0 || e > total {
		e = total
	}
	if s > total {
		return "", s, total, total, false
	}
	if e < s {
		e = s
	}
	truncated = s > 1 || e < total
	return strings.Join(lines[s-1:e], "\n"), s, e, total, truncated
}

func countNonEmptyOrLines(s string) int {
	if s == "" {
		return 0
	}
	norm := strings.ReplaceAll(s, "\r\n", "\n")
	trimmed := strings.TrimSuffix(norm, "\n")
	if trimmed == "" {
		return 0
	}
	return 1 + strings.Count(trimmed, "\n")
}

func randomShortID(numBytes int) string {
	b := make([]byte, numBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func isWithinDir(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel)
}

func isSafeDateString(s string) bool {
	if len(s) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}
