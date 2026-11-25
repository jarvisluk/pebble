// Package update checks GitHub Releases for newer Pebble versions and manages
// update check preferences and background polling state.
package update

import (
	"strconv"
	"strings"
)

// Version represents a parsed SemVer 2.0.0 version (major.minor.patch[-prerelease]).
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease []string
	Normalized string
}

// ParseVersion parses a SemVer string, tolerating an optional leading "v" or "V"
// and stripping any "+build" metadata.
func ParseVersion(raw string) (Version, bool) {
	s := strings.TrimSpace(raw)
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	if s == "" {
		return Version{}, false
	}
	if plus := strings.IndexByte(s, '+'); plus >= 0 {
		s = s[:plus]
	}
	core := s
	var preStr string
	if dash := strings.IndexByte(s, '-'); dash >= 0 {
		core = s[:dash]
		preStr = s[dash+1:]
		if preStr == "" {
			return Version{}, false
		}
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	maj, ok1 := parseNonNegInt(parts[0])
	min, ok2 := parseNonNegInt(parts[1])
	pat, ok3 := parseNonNegInt(parts[2])
	if !ok1 || !ok2 || !ok3 {
		return Version{}, false
	}

	var pre []string
	if preStr != "" {
		pre = strings.Split(preStr, ".")
		for _, id := range pre {
			if !isValidPrereleaseID(id) {
				return Version{}, false
			}
		}
	}

	norm := strconv.Itoa(maj) + "." + strconv.Itoa(min) + "." + strconv.Itoa(pat)
	if preStr != "" {
		norm += "-" + preStr
	}
	return Version{
		Major:      maj,
		Minor:      min,
		Patch:      pat,
		Prerelease: pre,
		Normalized: norm,
	}, true
}

func parseNonNegInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func isValidPrereleaseID(id string) bool {
	if id == "" {
		return false
	}
	allDigits := true
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= '0' && c <= '9' {
			continue
		}
		allDigits = false
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' {
			continue
		}
		return false
	}
	if allDigits && len(id) > 1 && id[0] == '0' {
		return false
	}
	return true
}

func isNumericID(id string) (int, bool) {
	if id == "" {
		return 0, false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(id)
	return n, err == nil
}

// Compare compares two SemVer 2.0.0 versions, returning -1, 0, or 1.
func Compare(a, b Version) int {
	if a.Major != b.Major {
		return cmpInt(a.Major, b.Major)
	}
	if a.Minor != b.Minor {
		return cmpInt(a.Minor, b.Minor)
	}
	if a.Patch != b.Patch {
		return cmpInt(a.Patch, b.Patch)
	}
	aPre, bPre := len(a.Prerelease) > 0, len(b.Prerelease) > 0
	if !aPre && !bPre {
		return 0
	}
	if !aPre && bPre {
		return 1
	}
	if aPre && !bPre {
		return -1
	}

	limit := len(a.Prerelease)
	if len(b.Prerelease) < limit {
		limit = len(b.Prerelease)
	}
	for i := 0; i < limit; i++ {
		ai, bi := a.Prerelease[i], b.Prerelease[i]
		if ai == bi {
			continue
		}
		an, aNum := isNumericID(ai)
		bn, bNum := isNumericID(bi)
		switch {
		case aNum && bNum:
			return cmpInt(an, bn)
		case aNum && !bNum:
			return -1
		case !aNum && bNum:
			return 1
		case ai < bi:
			return -1
		default:
			return 1
		}
	}
	return cmpInt(len(a.Prerelease), len(b.Prerelease))
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// IsNewer reports whether candidate parses as a strictly higher SemVer than current.
// Unparseable versions (such as "dev" or empty strings) return false safely.
func IsNewer(candidate, current string) bool {
	cand, ok1 := ParseVersion(candidate)
	cur, ok2 := ParseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	return Compare(cand, cur) > 0
}
