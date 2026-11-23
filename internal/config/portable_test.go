package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportAndApplyPortablePreservesLocalRuntime(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config", "bots.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "defaultHarness": "claude",
  "defaultModel": "sonnet",
  "http": { "host": "127.0.0.1", "port": 3789, "apiToken": "${BOT_HUB_API_TOKEN}" },
  "bots": [
    {
      "id": "assistant",
      "name": "Local Assistant",
      "workspace": "./workspaces/assistant",
      "harness": "claude",
      "model": "opus",
      "env": { "SECRET_KEY": "${MY_SECRET}" }
    }
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOT_HUB_API_TOKEN", "tok-123")
	t.Setenv("MY_SECRET", "sec-456")

	localCfg, _, err := Load(tmp, cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	// Build a portable bundle from a remote host with different name, schedule, and a new bot.
	remoteCfg := &HubConfig{
		Bots: []BotConfig{
			{
				ID:          "assistant",
				Name:        "Mac mini Assistant",
				Description: "Synced description",
				Avatar:      "pebble-2",
				Color:       "#7fa14a",
				Harness:     "opencode",
				Model:       "magpie/auto",
				Schedules: []ScheduleConfig{
					{ID: "daily", Name: "Daily", DailyAt: "09:00", Prompt: "Hi"},
				},
			},
			{
				ID:          "researcher",
				Name:        "Researcher",
				Description: "New synced bot",
				Harness:     "dsh",
				Model:       "deepseek-chat",
			},
		},
	}

	bundle := ExportPortable(remoteCfg, "mac-mini")
	if len(bundle.Bots) != 2 {
		t.Fatalf("expected 2 portable bots, got %d", len(bundle.Bots))
	}

	changed, added, updated, err := ApplyPortable(localCfg, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(added) != 1 || added[0] != "researcher" || len(updated) != 1 || updated[0] != "assistant" {
		t.Fatalf("unexpected apply result: changed=%v added=%v updated=%v", changed, added, updated)
	}

	// Verify existing bot kept its local harness, model, and env.
	if localCfg.Bots[0].Harness != "claude" || localCfg.Bots[0].Model != "opus" {
		t.Fatalf("local harness/model overwritten: %+v", localCfg.Bots[0])
	}
	if localCfg.Bots[0].Name != "Mac mini Assistant" || len(localCfg.Bots[0].Schedules) != 1 {
		t.Fatalf("portable fields not updated: %+v", localCfg.Bots[0])
	}

	// Verify new bot inherited local defaults instead of remote harness/model.
	if localCfg.Bots[1].Harness != "claude" || localCfg.Bots[1].Model != "sonnet" {
		t.Fatalf("new bot should inherit local defaults, got harness=%q model=%q", localCfg.Bots[1].Harness, localCfg.Bots[1].Model)
	}

	// Verify ${VAR} references on disk remained unexpanded.
	diskBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	disk := string(diskBytes)
	if !strings.Contains(disk, "${BOT_HUB_API_TOKEN}") || !strings.Contains(disk, "${MY_SECRET}") {
		t.Fatalf("env var references were expanded on disk:\n%s", disk)
	}
}
