package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLocalKeepsEnvRefs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"http":{"webhookSecret":"${T_SECRET}"},"bots":[{"id":"a","workspace":"./workspaces/a","telegram":{"token":"${T_TOKEN}","allowedUserIds":["1"]}}]}`
	if err := os.WriteFile(filepath.Join(root, "config", "bots.example.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("T_SECRET", "plain-secret")
	t.Setenv("T_TOKEN", "plain-token")

	cfg, _, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bots[0].Telegram.Token != "plain-token" {
		t.Fatalf("env not expanded in memory")
	}
	b := BotConfig{ID: "b"}
	NormalizeBot(&b, cfg)
	cfg.Bots = append(cfg.Bots, b)

	target, err := SaveLocal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(target)
	s := string(out)
	if strings.Contains(s, "plain-secret") || strings.Contains(s, "plain-token") {
		t.Fatalf("secrets persisted in plaintext:\n%s", s)
	}
	if !strings.Contains(s, "${T_TOKEN}") || !strings.Contains(s, `"id": "b"`) || !strings.Contains(s, "./workspaces/b") {
		t.Fatalf("unexpected saved config:\n%s", s)
	}
}
