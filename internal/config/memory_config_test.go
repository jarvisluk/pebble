package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryConfigDefaultsValidationAndEnvRefs(t *testing.T) {
	var nilMem *MemoryConfig
	if !nilMem.IsEnabled() || nilMem.EffectiveCoexistence() != MemoryCoexistenceAuto ||
		nilMem.EffectiveSnapshotMaxChars() != DefaultMemorySnapshotMaxChars ||
		!nilMem.ShouldDisableClaudeAutoMemory() || !nilMem.DreamEnabled() ||
		nilMem.EffectiveDreamDailyAt() != DefaultMemoryDreamDailyAt ||
		nilMem.EffectiveDreamMinNewMessages() != DefaultMemoryDreamMinMessages {
		t.Fatalf("unexpected nil MemoryConfig defaults")
	}

	invalidCases := []*MemoryConfig{
		{Coexistence: "bogus"},
		{SnapshotMaxChars: -1},
		{Dream: &MemoryDreamConfig{DailyAt: "25:99"}},
		{Dream: &MemoryDreamConfig{MinNewMessages: -2}},
		{Dream: &MemoryDreamConfig{Model: "bad model with space"}},
		{Embedding: &MemoryEmbedConfig{BaseURL: "ftp://invalid.local"}},
		{Embedding: &MemoryEmbedConfig{Model: "bad model"}},
	}
	for i, tc := range invalidCases {
		if err := ValidateMemoryConfig(tc); err == nil {
			t.Fatalf("case %d (%+v): expected validation error", i, tc)
		}
	}

	root := t.TempDir()
	cfgPath := filepath.Join(root, "config", "bots.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "memory": {
    "coexistence": "isolated",
    "snapshotMaxChars": 8000,
    "dream": {
      "dailyAt": "04:15",
      "model": "${TEST_DREAM_MODEL}",
      "minNewMessages": 6
    },
    "embedding": {
      "enabled": true,
      "baseUrl": "https://api.example.com/v1",
      "apiKey": "${TEST_EMBED_KEY}",
      "model": "text-embedding-3-small"
    }
  },
  "bots": [{"id": "a"}]
}`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DREAM_MODEL", "magpie/fast")
	t.Setenv("TEST_EMBED_KEY", "sk-secret-embed-val")

	cfg, _, err := Load(root, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Memory == nil || cfg.Memory.EffectiveCoexistence() != MemoryCoexistenceIsolated ||
		cfg.Memory.EffectiveDreamModel() != "magpie/fast" ||
		cfg.Memory.EffectiveEmbedding().APIKey != "sk-secret-embed-val" {
		t.Fatalf("unexpected loaded memory config: %+v", cfg.Memory)
	}
	if ref := RawMemoryEmbedAPIKeyEnvRef(cfgPath); ref != "${TEST_EMBED_KEY}" {
		t.Fatalf("RawMemoryEmbedAPIKeyEnvRef = %q, want ${TEST_EMBED_KEY}", ref)
	}

	// SaveLocal preserves ${TEST_EMBED_KEY} and never writes plaintext secret.
	target, err := SaveLocal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	disk, _ := os.ReadFile(target)
	if strings.Contains(string(disk), "sk-secret-embed-val") || !strings.Contains(string(disk), "${TEST_EMBED_KEY}") {
		t.Fatalf("expected env ref preserved on SaveLocal:\n%s", string(disk))
	}

	// Portable export strips APIKey; ApplyPortable applies non-secret fields while preserving local APIKey env ref.
	remoteCfg := &HubConfig{
		Memory: &MemoryConfig{
			Coexistence:      MemoryCoexistenceAuto,
			SnapshotMaxChars: 9500,
			Dream:            &MemoryDreamConfig{DailyAt: "02:45", MinNewMessages: 5},
			Embedding:        &MemoryEmbedConfig{Enabled: true, BaseURL: "https://embed.remote/v1", APIKey: "remote-secret-must-strip", Model: "embed-v2"},
		},
		Bots: cfg.Bots,
	}
	bundle := ExportPortable(remoteCfg, "remote-host")
	if bundle.Memory == nil || bundle.Memory.Embedding == nil || bundle.Memory.Embedding.APIKey != "" {
		t.Fatalf("expected ExportPortable to strip embedding.apiKey, got %+v", bundle.Memory)
	}

	changed, _, _, err := ApplyPortable(cfg, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected ApplyPortable to report changed=true for memory update")
	}
	if cfg.Memory.EffectiveSnapshotMaxChars() != 9500 || cfg.Memory.EffectiveEmbedding().BaseURL != "https://embed.remote/v1" ||
		cfg.Memory.EffectiveEmbedding().APIKey != "sk-secret-embed-val" {
		t.Fatalf("unexpected in-memory config after ApplyPortable: %+v", cfg.Memory)
	}
	diskAfter, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(diskAfter), "sk-secret-embed-val") || strings.Contains(string(diskAfter), "remote-secret-must-strip") ||
		!strings.Contains(string(diskAfter), "${TEST_EMBED_KEY}") {
		t.Fatalf("expected local ${TEST_EMBED_KEY} preserved on disk after ApplyPortable:\n%s", string(diskAfter))
	}
}
