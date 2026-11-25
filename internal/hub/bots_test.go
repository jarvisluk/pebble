package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateDeleteBotAndSharedUserMD(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "bots.json")
	initialCfg := `{
  "defaultModel": "global-m",
  "bots": [
    {"id": "a", "name": "Bot A", "description": "Old", "harness": "fake"},
    {"id": "b", "name": "Bot B", "harness": "fake"}
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(initialCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(tmp, cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	msgStore, err := state.NewMessageStore(cfg.MessagesDir)
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(&fakeHarness{})

	h := New(cfg, store, msgStore, reg)
	defer h.Close()

	// Empty name should fail
	empty := "   "
	if _, err := h.UpdateBot("a", BotUpdate{Name: &empty}); err == nil {
		t.Fatal("expected error for empty bot name")
	}

	// Update name, description, and instructions
	newName := "Bot Alpha"
	newDesc := "Updated description"
	newInst := "# Alpha\nFollow strict rules."
	st, err := h.UpdateBot("a", BotUpdate{Name: &newName, Description: &newDesc, Instructions: &newInst})
	if err != nil {
		t.Fatalf("UpdateBot failed: %v", err)
	}
	if st.Name != "Bot Alpha" || st.Description != "Updated description" {
		t.Fatalf("unexpected updated status: %+v", st)
	}
	detail, err := h.GetBotDetail("a")
	if err != nil || detail.Instructions != "# Alpha\nFollow strict rules.\n" {
		t.Fatalf("unexpected instructions: %q, err=%v", detail.Instructions, err)
	}

	// Shared USER.md read/write
	if err := h.SetSharedUserMD("Prefer concise Chinese replies."); err != nil {
		t.Fatalf("SetSharedUserMD failed: %v", err)
	}
	if got := h.SharedUserMD(); got != "Prefer concise Chinese replies.\n" {
		t.Fatalf("SharedUserMD = %q", got)
	}

	// DeleteBot removes b from hub and config while preserving its workspace
	wsB := h.BotWorkspace("b")
	if err := h.DeleteBot("b"); err != nil {
		t.Fatalf("DeleteBot(b) failed: %v", err)
	}
	if _, ok := h.GetBot("b"); ok {
		t.Fatal("bot b still present after delete")
	}
	if _, err := os.Stat(wsB); err != nil {
		t.Fatalf("expected workspace %s to remain on disk: %v", wsB, err)
	}
	rawBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var diskCfg config.HubConfig
	if err := json.Unmarshal(rawBytes, &diskCfg); err != nil {
		t.Fatal(err)
	}
	if len(diskCfg.Bots) != 1 || diskCfg.Bots[0].ID != "a" {
		t.Fatalf("unexpected bots on disk after delete: %+v", diskCfg.Bots)
	}

	// Deleting the last remaining bot must fail
	if err := h.DeleteBot("a"); err == nil {
		t.Fatal("expected error when deleting the last bot")
	}
}

func TestBotAvatarAndColorCreateUpdatePersist(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "bots.json")
	if err := os.WriteFile(cfgPath, []byte(`{"bots":[{"id":"alpha","name":"Alpha"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(tmp, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(&desktopWorkerFakeHarness{kind: "opencode"})
	h := New(cfg, store, nil, reg)
	defer h.Close()

	st, err := h.CreateBot(CreateBotRequest{
		ID:     "beta",
		Name:   "Beta",
		Avatar: "Cat",
		Color:  "#3B82F6",
	})
	if err != nil {
		t.Fatalf("CreateBot failed: %v", err)
	}
	if st.Avatar != "pebble-2" || st.Color != "#3b82f6" {
		t.Fatalf("unexpected created avatar/color: %+v", st)
	}

	if _, err := h.CreateBot(CreateBotRequest{ID: "bad", Name: "Bad", Avatar: "alien"}); err == nil {
		t.Fatal("expected invalid avatar on CreateBot to fail")
	}
	if _, err := h.UpdateBot("beta", BotUpdate{Color: ptr("not-hex")}); err == nil {
		t.Fatal("expected invalid color on UpdateBot to fail")
	}

	upd, err := h.UpdateBot("beta", BotUpdate{Avatar: ptr("pebble-7"), Color: ptr("#10B981")})
	if err != nil {
		t.Fatalf("UpdateBot failed: %v", err)
	}
	if upd.Avatar != "pebble-7" || upd.Color != "#10b981" {
		t.Fatalf("unexpected updated avatar/color: %+v", upd)
	}

	// Reload from disk to verify persistence.
	reloaded, _, err := config.Load(tmp, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var found *config.BotConfig
	for i := range reloaded.Bots {
		if reloaded.Bots[i].ID == "beta" {
			found = &reloaded.Bots[i]
			break
		}
	}
	if found == nil || found.Avatar != "pebble-7" || found.Color != "#10b981" {
		t.Fatalf("expected persisted avatar/color in bots.json, got %+v", found)
	}

	// Resetting to empty restores default.
	cleared, err := h.UpdateBot("beta", BotUpdate{Avatar: ptr(""), Color: ptr("")})
	if err != nil {
		t.Fatalf("UpdateBot clear failed: %v", err)
	}
	if cleared.Avatar != "" || cleared.Color != "" {
		t.Fatalf("expected empty avatar/color after reset, got %+v", cleared)
	}
}

func ptr(s string) *string { return &s }
