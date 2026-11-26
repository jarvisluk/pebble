package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMemorySearchOutputAndTagFilter(t *testing.T) {
	rawText := `[m_ref001] reference, everywhere, #vps #ops, 2026-11-13T19:50+08:00: 示例服务器节点编号为 100001。
[m_abc123] fact, project:dir:/Users/me/repo, #bot:ops #deploy, 2026-11-21T14:00+08:00: 生产端口为 8443
多行补充说明
(looked in: everywhere, project:dir:/Users/me/repo)`

	items := parseMemorySearchOutput(nil, rawText)
	if len(items) != 2 {
		t.Fatalf("got %d memory items, want 2: %+v", len(items), items)
	}
	if items[0].ID != "m_ref001" || items[0].Kind != "reference" || items[0].Scope != "everywhere" || items[0].UpdatedAt != "2026-11-13T19:50+08:00" {
		t.Fatalf("unexpected item 0: %+v", items[0])
	}
	if !hasMemoryTag(items[0].Tags, "vps") || !hasMemoryTag(items[0].Tags, "ops") {
		t.Fatalf("unexpected tags on item 0: %+v", items[0].Tags)
	}
	if items[1].ID != "m_abc123" || !hasMemoryTag(items[1].Tags, "bot:ops") || !strings.Contains(items[1].Text, "多行补充说明") {
		t.Fatalf("unexpected item 1: %+v", items[1])
	}

	empty := parseMemorySearchOutput(nil, `Nothing remembered matches "foo". (looked in: everywhere)`)
	if len(empty) != 0 {
		t.Fatalf("expected 0 items for empty match, got %+v", empty)
	}

	tags := normalizeMemoryTags([]string{"#deploy", "bot:ops", "deploy"}, "ops")
	if len(tags) != 2 || tags[0] != "deploy" || tags[1] != "bot:ops" {
		t.Fatalf("unexpected normalized tags: %+v", tags)
	}
}

func TestParseMagpieDataDirs(t *testing.T) {
	raw := "files: /Users/test/.config/magpie\ncache: /Users/test/.cache/magpie\n"
	files, cache := parseMagpieDataDirs(raw)
	if files != "/Users/test/.config/magpie" || cache != "/Users/test/.cache/magpie" {
		t.Fatalf("unexpected parsed dirs: files=%q cache=%q", files, cache)
	}
}

func TestDetectMagpieMemorySemanticStatus(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		tmp := t.TempDir()
		st := detectMagpieMemorySemanticStatus(filepath.Join(tmp, "files"), filepath.Join(tmp, "cache"))
		if st.Enabled || st.ModelReady || st.IndexReady || st.Active || st.Model != "" {
			t.Fatalf("expected all false for missing config, got %+v", st)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		tmp := t.TempDir()
		files := filepath.Join(tmp, "files")
		cache := filepath.Join(tmp, "cache")
		if err := os.MkdirAll(files, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(files, "memory-semantic.json"), []byte(`{"on":false}`), 0o644); err != nil {
			t.Fatal(err)
		}
		st := detectMagpieMemorySemanticStatus(files, cache)
		if st.Enabled || st.Active {
			t.Fatalf("expected disabled status, got %+v", st)
		}
	})

	t.Run("enabled without model", func(t *testing.T) {
		tmp := t.TempDir()
		files := filepath.Join(tmp, "files")
		cache := filepath.Join(tmp, "cache")
		if err := os.MkdirAll(files, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(files, "memory-semantic.json"), []byte(`{"on":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		st := detectMagpieMemorySemanticStatus(files, cache)
		if !st.Enabled || st.ModelReady || st.IndexReady || st.Active || st.Model != magpieSemanticModelName {
			t.Fatalf("unexpected enabled-without-model status: %+v", st)
		}
	})

	t.Run("enabled and ready", func(t *testing.T) {
		tmp := t.TempDir()
		files := filepath.Join(tmp, "files")
		cache := filepath.Join(tmp, "cache")
		revDir := filepath.Join(cache, magpieSemanticModelName, "abc123")
		if err := os.MkdirAll(files, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(revDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(files, "memory-semantic.json"), []byte(`{"on":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(revDir, "model_int8.onnx"), []byte("onnx"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(revDir, "vectors.bin"), []byte("vec"), 0o644); err != nil {
			t.Fatal(err)
		}
		st := detectMagpieMemorySemanticStatus(files, cache)
		if !st.Enabled || !st.ModelReady || !st.IndexReady || !st.Active || st.Model != magpieSemanticModelName {
			t.Fatalf("expected active ready status, got %+v", st)
		}
	})
}

func TestMagpieMemoryGracefulWhenAbsent(t *testing.T) {
	t.Setenv("MAGPIE_BIN", filepath.Join(t.TempDir(), "missing-magpie"))

	res := MagpieMemorySearch(t.Context(), "global", "", "", "", "", 10)
	if res.Installed || res.Available || len(res.Items) != 0 {
		t.Fatalf("expected unavailable empty memory list when magpie is absent, got %+v", res)
	}
	if _, err := MagpieMemorySave(t.Context(), MagpieMemorySaveReq{Text: "note"}); err == nil {
		t.Fatal("expected error saving memory when magpie is absent")
	}
	if _, err := MagpieMemoryForget(t.Context(), "m_1"); err == nil {
		t.Fatal("expected error forgetting memory when magpie is absent")
	}
}
