package state

import (
	"path/filepath"
	"testing"
)

func TestSyncCursorMessagesAndRoutineRuns(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewStore(filepath.Join(dir, "d1", "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()
	s2, err := NewStore(filepath.Join(dir, "d2", "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// Seed s1 with messages and routine runs.
	if _, err := s1.AppendMessage(ChatMessage{
		ID:        "msg_1",
		BotID:     "assistant",
		Role:      "user",
		Text:      "Check server health",
		CreatedAt: "2026-11-24T01:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.AppendMessage(ChatMessage{
		ID:        "msg_2",
		BotID:     "assistant",
		Role:      "assistant",
		Text:      "All services are green.",
		CreatedAt: "2026-11-24T01:00:05Z",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := s1.RecordRoutineRun(RoutineRun{
		ID:         "run_1",
		BotID:      "assistant",
		RoutineID:  "morning",
		StartedAt:  "2026-11-24T01:01:00Z",
		FinishedAt: "2026-11-24T01:01:02Z",
		SlotKey:    "assistant:morning:2026-11-24T01:01",
	}); err != nil {
		t.Fatal(err)
	}

	msgs, nextMsg, err := s1.ExportMessagesSince(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || nextMsg <= 0 {
		t.Fatalf("expected 2 exported messages, got %d (next=%d)", len(msgs), nextMsg)
	}
	runs, nextRun, err := s1.ExportRoutineRunsSince("", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || nextRun == "" {
		t.Fatalf("expected 1 exported routine run, got %d", len(runs))
	}

	// Import into s2 and verify idempotency.
	insM, err := s2.ImportMessages(msgs)
	if err != nil || insM != 2 {
		t.Fatalf("expected 2 imported messages, got %d (%v)", insM, err)
	}
	insM2, err := s2.ImportMessages(msgs)
	if err != nil || insM2 != 0 {
		t.Fatalf("expected 0 duplicate messages on re-import, got %d (%v)", insM2, err)
	}

	insR, err := s2.ImportRoutineRuns(runs)
	if err != nil || insR != 1 {
		t.Fatalf("expected 1 imported routine run, got %d (%v)", insR, err)
	}
	// Different ID but same slot_key must be deduplicated.
	dupSlotRun := runs[0]
	dupSlotRun.ID = "run_from_standby"
	insR2, err := s2.ImportRoutineRuns([]RoutineRun{dupSlotRun})
	if err != nil || insR2 != 0 {
		t.Fatalf("expected slot_key duplicate skipped, got %d (%v)", insR2, err)
	}

	// RecentMessagesForHandoff returns chronological order.
	handoff, err := s2.RecentMessagesForHandoff("assistant", 8)
	if err != nil || len(handoff) != 2 {
		t.Fatalf("expected 2 handoff messages, got %d (%v)", len(handoff), err)
	}
	if handoff[0].Role != "user" || handoff[1].Role != "assistant" {
		t.Fatalf("unexpected handoff order: %+v", handoff)
	}

	// Meta persistence check.
	if err := s2.SetMeta("sync_cursor:test", "42"); err != nil {
		t.Fatal(err)
	}
	if got := s2.GetMeta("sync_cursor:test"); got != "42" {
		t.Fatalf("expected meta 42, got %q", got)
	}
}
