package memory

import (
	"fmt"
	"regexp"
	"strings"
)

type secretRule struct {
	kind string
	re   *regexp.Regexp
}

var secretRules = []secretRule{
	{
		kind: "private_key",
		re:   regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
	},
	{
		kind: "anthropic_key",
		re:   regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{16,}\b`),
	},
	{
		kind: "openai_key",
		re:   regexp.MustCompile(`\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{20,}\b`),
	},
	{
		kind: "stripe_key",
		re:   regexp.MustCompile(`\b(?:sk|rk)_live_[A-Za-z0-9]{16,}\b`),
	},
	{
		kind: "github_token",
		re:   regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b`),
	},
	{
		kind: "github_pat",
		re:   regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	},
	{
		kind: "google_api_key",
		re:   regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{25,}\b`),
	},
	{
		kind: "tavily_key",
		re:   regexp.MustCompile(`\btvly-[A-Za-z0-9_-]{16,}\b`),
	},
	{
		kind: "aws_access_key",
		re:   regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	},
	{
		kind: "slack_token",
		re:   regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{12,}\b`),
	},
	{
		kind: "telegram_token",
		re:   regexp.MustCompile(`\b[0-9]{8,12}:[A-Za-z0-9_-]{30,}\b`),
	},
	{
		kind: "bearer_token",
		re:   regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{20,}\b`),
	},
	{
		kind: "credential_assignment",
		re:   regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret|private[_-]?key|password|passwd)\s*[:=]\s*["']?([A-Za-z0-9._~+/=-]{16,})["']?`),
	},
}

var secretPlaceholders = []string{
	"redacted",
	"example",
	"placeholder",
	"changeme",
	"your_api_key",
	"your-api-key",
	"your_token",
	"your-token",
	"replace_me",
	"xxxxxxxx",
	"00000000",
}

// ContainsSecret inspects text for plaintext credentials or private keys.
// It returns the matched secret kind and true if a secret pattern is detected.
func ContainsSecret(text string) (string, bool) {
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	for _, rule := range secretRules {
		matches := rule.re.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			candidate := m[0]
			if len(m) > 1 && m[1] != "" {
				candidate = m[1]
			}
			if isPlaceholderValue(candidate) {
				continue
			}
			if rule.kind == "credential_assignment" && !looksLikeSecretToken(candidate) {
				continue
			}
			return rule.kind, true
		}
	}
	return "", false
}

// CheckSecret validates a single text field and returns a structured *SecretError on violation.
func CheckSecret(field, text string) error {
	kind, ok := ContainsSecret(text)
	if !ok {
		return nil
	}
	msg := fmt.Sprintf("refused to store plaintext secret (%s) in memory", kind)
	if strings.TrimSpace(field) != "" {
		msg = fmt.Sprintf("refused to store plaintext secret (%s) in memory field %q", kind, field)
	}
	return &SecretError{
		Code:    "secret_detected",
		Kind:    kind,
		Field:   field,
		Message: msg,
	}
}

// CheckFields validates a map of named fields with SecretGuard.
func CheckFields(fields map[string]string) error {
	for name, val := range fields {
		if err := CheckSecret(name, val); err != nil {
			return err
		}
	}
	return nil
}

func isPlaceholderValue(val string) bool {
	low := strings.ToLower(strings.TrimSpace(val))
	if low == "" {
		return true
	}
	if strings.HasPrefix(low, "$") || strings.HasPrefix(low, "<") || strings.HasPrefix(low, "{{") {
		return true
	}
	for _, p := range secretPlaceholders {
		if strings.Contains(low, p) {
			return true
		}
	}
	// Reject repeated single-character strings like "****************" or "aaaaaaaaaaaaaaaa".
	if len(low) >= 8 {
		first := low[0]
		allSame := true
		for i := 1; i < len(low); i++ {
			if low[i] != first {
				allSame = false
				break
			}
		}
		if allSame {
			return true
		}
	}
	return false
}

func looksLikeSecretToken(val string) bool {
	if len(val) < 16 {
		return false
	}
	// Paths or env variable names with only uppercase + underscores are usually references, not raw secrets.
	if strings.HasPrefix(val, "/") || strings.HasPrefix(val, "./") || strings.HasPrefix(val, "~/") {
		return false
	}
	var hasLower, hasUpper, hasDigit int
	for _, r := range val {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = 1
		case r >= 'A' && r <= 'Z':
			hasUpper = 1
		case r >= '0' && r <= '9':
			hasDigit = 1
		}
	}
	// Require at least two character classes or length >= 24 to minimize false positives on prose/identifiers.
	return (hasLower+hasUpper+hasDigit) >= 2 || len(val) >= 24
}
