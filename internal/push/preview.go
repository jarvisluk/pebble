package push

import (
	"regexp"
	"strings"
)

var (
	mdFenceRe       = regexp.MustCompile("(?s)(?:```|~~~)[^\n]*\n?.*?(?:```|~~~)")
	mdMathBlockRe   = regexp.MustCompile(`(?s)\$\$(.*?)\$\$`)
	mdImageRe       = regexp.MustCompile(`!\[([^\]]*)\]\([^)\s]+(?:\s+"[^"]*")?\)`)
	mdLinkRe        = regexp.MustCompile(`\[([^\]]+)\]\([^)\s]+(?:\s+"[^"]*")?\)`)
	mdAutoLinkRe    = regexp.MustCompile(`<(https?://[^>\s]+|mailto:[^>\s]+)>`)
	mdBrRe          = regexp.MustCompile(`(?i)<br\s*/?>`)
	mdSafeBlockTag  = regexp.MustCompile(`(?i)</?(?:details|summary)(?:\s+open)?>`)
	mdSafeInlineTag = regexp.MustCompile(`(?i)</?(?:sup|sub|kbd|u|s|del|b|strong|i|em|code|mark)>`)
	mdTableSepRe    = regexp.MustCompile(`^\s*\|?(?:\s*:?-{3,}:?\s*\|)+\s*:?-{3,}:?\s*\|?\s*$`)
	mdHeadingRe     = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	mdQuoteRe       = regexp.MustCompile(`^\s*(?:>\s*)+`)
	mdTaskRe        = regexp.MustCompile(`^\s*[-*+]\s+\[[xX ]\]\s+`)
	mdListRe        = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)
	mdRuleRe        = regexp.MustCompile(`^\s*(?:[-*_]\s*){3,}$`)
	mdFootnoteDefRe = regexp.MustCompile(`^\s*\[\^([^\]]+)\]:\s*`)
	mdFootnoteRefRe = regexp.MustCompile(`\[\^([^\]]+)\]`)
	mdInlineCodeRe  = regexp.MustCompile("`+([^`]+)`+")
	mdBoldItalicRe  = regexp.MustCompile(`\*\*\*([^*\n]+)\*\*\*|___([^_\n]+)___`)
	mdBoldRe        = regexp.MustCompile(`\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
	mdStrikeRe      = regexp.MustCompile(`~~([^~\n]+)~~`)
	mdItalicStarRe  = regexp.MustCompile(`\*([^*\n]+)\*`)
	mdItalicUnderRe = regexp.MustCompile(`(^|[^\pL\pN_])_([^_\n]+?)_([^\pL\pN_]|$)`)
	mdInlineMathRe  = regexp.MustCompile(`\$([^\s$][^$\n]*?[^\s$]|[^\s$])\$`)
	mdEscapeRe      = regexp.MustCompile("\\\\([\\\\`*_{}\\[\\]()#+\\-.!|>~$])")
)

var latexPlainMap = map[string]string{
	"sum": "∑", "prod": "∏", "int": "∫", "oint": "∮",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε",
	"zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ",
	"lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π", "rho": "ρ",
	"sigma": "σ", "tau": "τ", "upsilon": "υ", "phi": "φ", "varphi": "φ", "chi": "χ",
	"psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π",
	"Sigma": "Σ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	"leq": "≤", "le": "≤", "geq": "≥", "ge": "≥", "neq": "≠", "ne": "≠",
	"approx": "≈", "equiv": "≡", "sim": "∼", "simeq": "≃", "propto": "∝",
	"times": "×", "div": "÷", "cdot": "·", "pm": "±", "mp": "∓", "ast": "∗", "star": "⋆",
	"infty": "∞", "partial": "∂", "nabla": "∇", "forall": "∀", "exists": "∃",
	"in": "∈", "notin": "∉", "ni": "∋", "subset": "⊂", "subseteq": "⊆", "supset": "⊃", "supseteq": "⊇",
	"cup": "∪", "cap": "∩", "emptyset": "∅", "varnothing": "∅",
	"to": "→", "rightarrow": "→", "leftarrow": "←", "leftrightarrow": "↔",
	"Rightarrow": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "mapsto": "↦",
	"land": "∧", "lor": "∨", "lnot": "¬", "neg": "¬", "therefore": "∴", "because": "∵",
	"dots": "…", "ldots": "…", "cdots": "⋯", "vdots": "⋮",
	"angle": "∠", "degree": "°", "circ": "∘", "prime": "′",
	"quad": " ", "qquad": " ", ",": " ", ";": " ", ":": " ", "!": "",
}

func readLatexGroup(r []rune, pos int) (string, int) {
	for pos < len(r) && (r[pos] == ' ' || r[pos] == '\t') {
		pos++
	}
	if pos >= len(r) {
		return "", pos
	}
	if r[pos] != '{' {
		if r[pos] == '\\' {
			end := pos + 1
			for end < len(r) && ((r[end] >= 'a' && r[end] <= 'z') || (r[end] >= 'A' && r[end] <= 'Z')) {
				end++
			}
			if end == pos+1 && end < len(r) {
				end++
			}
			return string(r[pos:end]), end
		}
		return string(r[pos]), pos + 1
	}
	depth := 0
	start := pos + 1
	for i := pos; i < len(r); i++ {
		if r[i] == '{' {
			depth++
		} else if r[i] == '}' {
			depth--
			if depth == 0 {
				return string(r[start:i]), i + 1
			}
		}
	}
	return string(r[start:]), len(r)
}

func wrapMathArgIfComplex(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len([]rune(s)) == 1 || strings.ContainsAny(s, " \t+-*/=<>") == false {
		return s
	}
	return "(" + s + ")"
}

func latexToPlain(expr string) string {
	r := []rune(strings.TrimSpace(expr))
	var b strings.Builder
	for i := 0; i < len(r); {
		ch := r[i]
		if ch == '\\' {
			if i+1 < len(r) && r[i+1] == '\\' {
				b.WriteByte(' ')
				i += 2
				continue
			}
			if i+1 < len(r) && !((r[i+1] >= 'a' && r[i+1] <= 'z') || (r[i+1] >= 'A' && r[i+1] <= 'Z')) {
				if mapped, ok := latexPlainMap[string(r[i+1])]; ok {
					b.WriteString(mapped)
				} else {
					b.WriteRune(r[i+1])
				}
				i += 2
				continue
			}
			end := i + 1
			for end < len(r) && ((r[end] >= 'a' && r[end] <= 'z') || (r[end] >= 'A' && r[end] <= 'Z')) {
				end++
			}
			cmd := string(r[i+1 : end])
			i = end
			switch cmd {
			case "left", "right", "big", "Big", "bigg", "Bigg":
				continue
			case "frac", "dfrac", "tfrac":
				num, next1 := readLatexGroup(r, i)
				den, next2 := readLatexGroup(r, next1)
				i = next2
				b.WriteString(wrapMathArgIfComplex(latexToPlain(num)))
				b.WriteByte('/')
				b.WriteString(wrapMathArgIfComplex(latexToPlain(den)))
			case "sqrt":
				arg, next := readLatexGroup(r, i)
				i = next
				plainArg := strings.TrimSpace(latexToPlain(arg))
				b.WriteRune('√')
				if len([]rune(plainArg)) <= 1 {
					b.WriteString(plainArg)
				} else {
					b.WriteByte('(')
					b.WriteString(plainArg)
					b.WriteByte(')')
				}
			case "text", "mathrm", "mathbf", "mathit", "mathsf", "mathtt", "boldsymbol", "operatorname", "hat", "bar", "vec", "tilde":
				arg, next := readLatexGroup(r, i)
				i = next
				b.WriteString(latexToPlain(arg))
			default:
				if mapped, ok := latexPlainMap[cmd]; ok {
					b.WriteString(mapped)
				} else {
					b.WriteString(cmd)
				}
			}
			continue
		}
		if ch == '^' || ch == '_' {
			arg, next := readLatexGroup(r, i+1)
			i = next
			b.WriteRune(ch)
			b.WriteString(latexToPlain(arg))
			continue
		}
		if ch == '{' || ch == '}' {
			i++
			continue
		}
		b.WriteRune(ch)
		i++
	}
	return strings.TrimSpace(b.String())
}

func stripMarkdownForPreview(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = mdFenceRe.ReplaceAllString(s, " [Code] ")
	s = mdMathBlockRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m, "$$"), "$$"))
		if inner == "" {
			return " [Formula] "
		}
		return " " + latexToPlain(inner) + " "
	})

	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if mdTableSepRe.MatchString(trimmed) || mdRuleRe.MatchString(trimmed) {
			continue
		}
		line = mdFootnoteDefRe.ReplaceAllString(line, "[$1] ")
		line = mdQuoteRe.ReplaceAllString(line, "")
		line = mdHeadingRe.ReplaceAllString(line, "")
		line = mdTaskRe.ReplaceAllString(line, "")
		line = mdListRe.ReplaceAllString(line, "")
		if strings.HasPrefix(strings.TrimSpace(line), "|") && strings.HasSuffix(strings.TrimSpace(line), "|") {
			line = strings.ReplaceAll(line, `\|`, "¦")
			cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
			cleaned := make([]string, 0, len(cells))
			for _, c := range cells {
				c = strings.TrimSpace(strings.ReplaceAll(c, "¦", "|"))
				if c != "" {
					cleaned = append(cleaned, c)
				}
			}
			line = strings.Join(cleaned, " · ")
		}
		kept = append(kept, line)
	}
	s = strings.Join(kept, " ")
	s = mdBrRe.ReplaceAllString(s, " ")
	s = mdSafeBlockTag.ReplaceAllString(s, " ")
	s = mdSafeInlineTag.ReplaceAllString(s, "")
	s = mdImageRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdImageRe.FindStringSubmatch(m)
		if len(sub) > 1 && strings.TrimSpace(sub[1]) != "" {
			return "[Image: " + strings.TrimSpace(sub[1]) + "]"
		}
		return "[Image]"
	})
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = mdAutoLinkRe.ReplaceAllString(s, "$1")
	s = mdFootnoteRefRe.ReplaceAllString(s, "[$1]")
	s = mdInlineCodeRe.ReplaceAllString(s, "$1")
	s = mdBoldItalicRe.ReplaceAllString(s, "$1$2")
	s = mdBoldRe.ReplaceAllString(s, "$1$2")
	s = mdStrikeRe.ReplaceAllString(s, "$1")
	s = mdItalicStarRe.ReplaceAllString(s, "$1")
	s = mdItalicUnderRe.ReplaceAllString(s, "$1$2$3")
	s = mdInlineMathRe.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) <= 2 {
			return m
		}
		return latexToPlain(m[1 : len(m)-1])
	})
	s = mdEscapeRe.ReplaceAllString(s, "$1")
	return s
}

func compactPreview(s string, maxRunes int) string {
	s = stripMarkdownForPreview(s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}
