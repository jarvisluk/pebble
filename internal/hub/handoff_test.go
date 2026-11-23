package hub

import (
	"path/filepath"
	"strings"
	"testing"

	"bot-hub/internal/config"
	"bot-hub/internal/state"
)

func TestBuildTurnPromptIncludesCrossHostHandoff(t *testing.T) {
	h := newTestHubForDesktop(t)

	// Simulate messages synced from primary host into standby's SQLite store.
	if _, err := h.store.ImportMessages([]state.SyncMessageRow{
		{
			MsgUID:    "uid_1",
			BotID:     "assistant",
			Role:      "user",
			Text:      "Prepare deployment checklist for Mac mini",
			CreatedAt: "2026-11-24T01:00:00Z",
		},
		{
			MsgUID:    "uid_2",
			BotID:     "assistant",
			Role:      "assistant",
			Text:      "Checklist drafted with 3 verification steps.",
			CreatedAt: "2026-11-24T01:00:04Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	bot := config.BotConfig{
		ID:        "assistant",
		Name:      "Assistant",
		Workspace: filepath.Join(h.cfg.ProjectRoot, "workspaces", "assistant"),
	}
	got := h.buildTurnPrompt(bot, "Continue with step 2", true, "", nil)
	if !strings.Contains(got, "Cross-host session handoff") ||
		!strings.Contains(got, "Prepare deployment checklist for Mac mini") ||
		!strings.Contains(got, "Checklist drafted with 3 verification steps.") ||
		!strings.Contains(got, "Continue with step 2") {
		t.Fatalf("expected cross-host handoff context in prompt, got:\n%s", got)
	}
}
