package i18ncheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root containing go.mod")
		}
		dir = parent
	}
}

func hasCJK(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF:
			return true
		case r >= 0x3400 && r <= 0x4DBF:
			return true
		case r >= 0x3000 && r <= 0x303F:
			return true
		case r >= 0xFF01 && r <= 0xFF5E:
			return true
		}
	}
	return false
}

// stripCommentsLineByLine removes // and /* ... */ comments while preserving string literals.
// Lines containing "i18n:allow-cjk" are cleared so intentional exceptions are explicitly opt-in.
func stripCommentsLineByLine(src, ext string) []string {
	lines := strings.Split(src, "\n")
	out := make([]string, len(lines))

	supportsSingleQuote := ext != ".swift"
	supportsBacktick := ext == ".go" || ext == ".js" || ext == ".jsx" || ext == ".ts" || ext == ".tsx"
	supportsTripleQuote := ext == ".swift" || ext == ".kt"

	inBlockComment := false
	inBacktick := false
	inTripleQuote := false

	for idx, rawLine := range lines {
		if strings.Contains(rawLine, "i18n:allow-cjk") {
			// Still track multi-line states? Usually single-line opt-in; keep block state accurate only if needed.
			out[idx] = ""
			continue
		}
		var b strings.Builder
		n := len(rawLine)
		inDouble := false
		inSingle := false

		for i := 0; i < n; i++ {
			c := rawLine[i]

			if inBlockComment {
				if c == '*' && i+1 < n && rawLine[i+1] == '/' {
					inBlockComment = false
					i++
				}
				continue
			}

			if inBacktick {
				b.WriteByte(c)
				if c == '`' && (ext == ".go" || (i == 0 || rawLine[i-1] != '\\')) {
					inBacktick = false
				}
				continue
			}

			if inTripleQuote {
				b.WriteByte(c)
				if c == '"' && i+2 < n && rawLine[i+1] == '"' && rawLine[i+2] == '"' {
					b.WriteString(`""`)
					i += 2
					inTripleQuote = false
				}
				continue
			}

			if inDouble {
				b.WriteByte(c)
				if c == '\\' && i+1 < n {
					b.WriteByte(rawLine[i+1])
					i++
					continue
				}
				if c == '"' {
					inDouble = false
				}
				continue
			}

			if inSingle {
				b.WriteByte(c)
				if c == '\\' && i+1 < n {
					b.WriteByte(rawLine[i+1])
					i++
					continue
				}
				if c == '\'' {
					inSingle = false
				}
				continue
			}

			if c == '/' && i+1 < n {
				if rawLine[i+1] == '/' {
					break
				}
				if rawLine[i+1] == '*' {
					inBlockComment = true
					i++
					continue
				}
			}

			if supportsTripleQuote && c == '"' && i+2 < n && rawLine[i+1] == '"' && rawLine[i+2] == '"' {
				b.WriteString(`"""`)
				i += 2
				inTripleQuote = true
				continue
			}

			if c == '"' {
				b.WriteByte(c)
				inDouble = true
				continue
			}
			if supportsSingleQuote && c == '\'' {
				b.WriteByte(c)
				inSingle = true
				continue
			}
			if supportsBacktick && c == '`' {
				b.WriteByte(c)
				inBacktick = true
				continue
			}

			b.WriteByte(c)
		}
		out[idx] = b.String()
	}
	return out
}

func scanTreeForCJK(t *testing.T, root string, relDirs []string, exts map[string]bool, skipPath func(rel string) bool) []string {
	t.Helper()
	var violations []string
	for _, rd := range relDirs {
		absDir := filepath.Join(root, rd)
		if _, err := os.Stat(absDir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				base := d.Name()
				if base == ".git" || base == "node_modules" || base == "dist" || base == "build" || base == ".build" || base == "DerivedData" || base == ".gradle" {
					return filepath.SkipDir
				}
				if skipPath != nil && skipPath(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if !exts[ext] {
				return nil
			}
			if skipPath != nil && skipPath(rel) {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			stripped := stripCommentsLineByLine(string(raw), ext)
			for i, line := range stripped {
				if hasCJK(line) {
					snippet := strings.TrimSpace(line)
					if len(snippet) > 140 {
						snippet = snippet[:140] + "..."
					}
					violations = append(violations, rel+":"+itoa(i+1)+": "+snippet)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", rd, err)
		}
	}
	return violations
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestNoBackendCJKLeaks(t *testing.T) {
	root := repoRoot(t)
	violations := scanTreeForCJK(
		t,
		root,
		[]string{"cmd", "internal"},
		map[string]bool{".go": true},
		func(rel string) bool {
			return strings.HasSuffix(rel, "_test.go") ||
				rel == "internal/web/dist" ||
				strings.HasPrefix(rel, "internal/web/dist/")
		},
	)
	if len(violations) > 0 {
		t.Fatalf("found hardcoded CJK in Go backend (use English or mark intentional legacy migration lines with i18n:allow-cjk):\n%s", strings.Join(violations, "\n"))
	}
}

func TestNoClientCodeCJKLeaks(t *testing.T) {
	root := repoRoot(t)
	violations := scanTreeForCJK(
		t,
		root,
		[]string{"apps/apple", "apps/android/app/src/main", "web/src"},
		map[string]bool{".swift": true, ".kt": true, ".java": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true},
		func(rel string) bool {
			return strings.Contains(rel, "Tests/") ||
				strings.HasSuffix(rel, "Tests.swift") ||
				strings.Contains(rel, ".test.") ||
				rel == "apps/android/app/src/main/java/ai/pebble/android/i18n/StringsZh.kt" ||
				rel == "apps/android/app/src/main/java/ai/pebble/android/i18n/StringsEn.kt" ||
				rel == "web/src/lib/i18n/zh-CN.ts" ||
				rel == "web/src/lib/i18n/en.ts"
		},
	)
	if len(violations) > 0 {
		t.Fatalf("found hardcoded CJK in client UI/logic code (move to i18n dictionaries or mark intentional language names with i18n:allow-cjk):\n%s", strings.Join(violations, "\n"))
	}
}

func TestEnglishResourcesNoCJK(t *testing.T) {
	root := repoRoot(t)
	candidates := []string{
		"apps/apple/Pebble/strings-en.json",
		"apps/apple/Pebble/en.lproj/InfoPlist.strings",
		"apps/apple/Pebble/en.lproj/Localizable.strings",
		"apps/apple/PebbleLiveActivity/en.lproj/InfoPlist.strings",
		"apps/apple/PebbleLiveActivity/en.lproj/Localizable.strings",
		"apps/apple/Info.plist",
		"apps/apple/Info-macOS.plist",
		"apps/apple/PebbleLiveActivity/Info.plist",
		"build/darwin/Info.plist",
		"build/darwin/en.lproj/InfoPlist.strings",
		"apps/android/app/src/main/res/values/strings.xml",
	}

	var violations []string
	for _, rel := range candidates {
		abs := filepath.Join(root, rel)
		raw, err := os.ReadFile(abs)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if hasCJK(line) {
				violations = append(violations, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}

	// Check Android enStrings block.
	ktPath := filepath.Join(root, "apps/android/app/src/main/java/ai/pebble/android/i18n/StringsEn.kt")
	ktRaw, err := os.ReadFile(ktPath)
	if err != nil {
		t.Fatalf("read StringsEn.kt: %v", err)
	}
	ktEnBlock, ktStartLine := extractBlock(string(ktRaw), `internal val enStrings: Map<String, String> = mapOf(`, "\n)")
	if ktEnBlock == "" {
		t.Fatal("failed to locate enStrings block in StringsEn.kt")
	}
	for i, line := range strings.Split(ktEnBlock, "\n") {
		if hasCJK(line) {
			violations = append(violations, "StringsEn.kt:"+itoa(ktStartLine+i)+": "+strings.TrimSpace(line))
		}
	}

	// Check Web en block.
	tsPath := filepath.Join(root, "web/src/lib/i18n/en.ts")
	tsRaw, err := os.ReadFile(tsPath)
	if err != nil {
		t.Fatalf("read web/src/lib/i18n/en.ts: %v", err)
	}
	tsEnBlock, tsStartLine := extractBlock(string(tsRaw), "export const en: Record<string, string> = {", "\n};")
	if tsEnBlock == "" {
		t.Fatal("failed to locate en block in web/src/lib/i18n/en.ts")
	}
	for i, line := range strings.Split(tsEnBlock, "\n") {
		if hasCJK(line) {
			violations = append(violations, "web/src/lib/i18n/en.ts:"+itoa(tsStartLine+i)+": "+strings.TrimSpace(line))
		}
	}

	if len(violations) > 0 {
		t.Fatalf("found CJK characters inside English resources:\n%s", strings.Join(violations, "\n"))
	}
}

func TestI18nKeyParity(t *testing.T) {
	root := repoRoot(t)

	// 1. Apple JSON dictionaries.
	zhApple := readJSONKeys(t, filepath.Join(root, "apps/apple/Pebble/strings-zh.json"))
	enApple := readJSONKeys(t, filepath.Join(root, "apps/apple/Pebble/strings-en.json"))
	assertSameKeys(t, "Apple strings-zh.json", zhApple, "Apple strings-en.json", enApple)

	// 2. Android StringsZh.kt / StringsEn.kt.
	zhKtRawBytes, err := os.ReadFile(filepath.Join(root, "apps/android/app/src/main/java/ai/pebble/android/i18n/StringsZh.kt"))
	if err != nil {
		t.Fatalf("read StringsZh.kt: %v", err)
	}
	enKtRawBytes, err := os.ReadFile(filepath.Join(root, "apps/android/app/src/main/java/ai/pebble/android/i18n/StringsEn.kt"))
	if err != nil {
		t.Fatalf("read StringsEn.kt: %v", err)
	}
	zhKtBlock, _ := extractBlock(string(zhKtRawBytes), `internal val zhStrings: Map<String, String> = mapOf(`, "\n)")
	enKtBlock, _ := extractBlock(string(enKtRawBytes), `internal val enStrings: Map<String, String> = mapOf(`, "\n)")
	if zhKtBlock == "" || enKtBlock == "" {
		t.Fatal("failed to extract zhStrings/enStrings from StringsZh.kt/StringsEn.kt")
	}
	ktKeyRe := regexp.MustCompile(`"([^"]+)"\s+to\s+"`)
	zhKt := extractRegexKeys(ktKeyRe, zhKtBlock)
	enKt := extractRegexKeys(ktKeyRe, enKtBlock)
	assertSameKeys(t, "Android zhStrings", zhKt, "Android enStrings", enKt)

	// 3. Web i18n/zh-CN.ts & i18n/en.ts.
	zhTsRawBytes, err := os.ReadFile(filepath.Join(root, "web/src/lib/i18n/zh-CN.ts"))
	if err != nil {
		t.Fatalf("read web/src/lib/i18n/zh-CN.ts: %v", err)
	}
	enTsRawBytes, err := os.ReadFile(filepath.Join(root, "web/src/lib/i18n/en.ts"))
	if err != nil {
		t.Fatalf("read web/src/lib/i18n/en.ts: %v", err)
	}
	zhTsBlock, _ := extractBlock(string(zhTsRawBytes), "export const zhCN: Record<string, string> = {", "\n};")
	enTsBlock, _ := extractBlock(string(enTsRawBytes), "export const en: Record<string, string> = {", "\n};")
	if zhTsBlock == "" || enTsBlock == "" {
		t.Fatal("failed to extract zhCN/en blocks from web/src/lib/i18n/")
	}
	tsKeyRe := regexp.MustCompile(`(?m)^\s+([A-Za-z0-9_]+):`)
	zhTs := extractRegexKeys(tsKeyRe, zhTsBlock)
	enTs := extractRegexKeys(tsKeyRe, enTsBlock)
	assertSameKeys(t, "Web zh-CN", zhTs, "Web en", enTs)
}

func extractBlock(src, startMarker, endMarker string) (string, int) {
	startIdx := strings.Index(src, startMarker)
	if startIdx < 0 {
		return "", 0
	}
	bodyStart := startIdx + len(startMarker)
	endRel := strings.Index(src[bodyStart:], endMarker)
	if endRel < 0 {
		return "", 0
	}
	startLine := 1 + strings.Count(src[:bodyStart], "\n")
	return src[bodyStart : bodyStart+endRel], startLine
}

func readJSONKeys(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func extractRegexKeys(re *regexp.Regexp, block string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		if len(m) >= 2 {
			out[m[1]] = true
		}
	}
	return out
}

func assertSameKeys(t *testing.T, nameA string, a map[string]bool, nameB string, b map[string]bool) {
	t.Helper()
	var onlyA, onlyB []string
	for k := range a {
		if !b[k] {
			onlyA = append(onlyA, k)
		}
	}
	for k := range b {
		if !a[k] {
			onlyB = append(onlyB, k)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	if len(onlyA) > 0 || len(onlyB) > 0 {
		t.Fatalf("key mismatch between %s and %s:\n  only in %s: %v\n  only in %s: %v",
			nameA, nameB, nameA, onlyA, nameB, onlyB)
	}
}
