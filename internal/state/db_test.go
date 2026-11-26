package state

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenSQLiteMigratesLegacyRoutineRunsSlotKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacyDB.Exec(`
CREATE TABLE routine_runs (
  id            TEXT PRIMARY KEY,
  bot_id        TEXT NOT NULL,
  routine_id    TEXT NOT NULL,
  routine_name  TEXT NOT NULL,
  trigger       TEXT NOT NULL DEFAULT 'schedule',
  status        TEXT NOT NULL DEFAULT 'ok',
  delivery      TEXT NOT NULL DEFAULT 'receipt',
  model         TEXT NOT NULL DEFAULT '',
  output        TEXT NOT NULL DEFAULT '',
  error         TEXT NOT NULL DEFAULT '',
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  cost_usd      REAL NOT NULL DEFAULT 0,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  started_at    TEXT NOT NULL,
  finished_at   TEXT NOT NULL,
  injected_at   TEXT NOT NULL DEFAULT ''
);`)
	if err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	_ = legacyDB.Close()

	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("openSQLite failed on legacy routine_runs table: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`PRAGMA table_info(routine_runs)`)
	if err != nil {
		t.Fatal(err)
	}
	hasSlotKey := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctyp string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		if name == "slot_key" {
			hasSlotKey = true
		}
	}
	_ = rows.Close()
	if !hasSlotKey {
		t.Fatalf("expected slot_key column to be migrated onto legacy routine_runs table")
	}

	idxRows, err := db.Query(`PRAGMA index_list(routine_runs)`)
	if err != nil {
		t.Fatal(err)
	}
	hasSlotKeyIdx := false
	for idxRows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			_ = idxRows.Close()
			t.Fatal(err)
		}
		if name == "idx_routine_runs_slot_key" {
			hasSlotKeyIdx = true
		}
	}
	_ = idxRows.Close()
	if !hasSlotKeyIdx {
		t.Fatalf("expected idx_routine_runs_slot_key index to be created")
	}
}

func TestPushDeviceStore(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.UpsertPushDevice(PushDevice{
		DeviceID:               "dev-1",
		Platform:               "ios",
		Env:                    "sandbox",
		BundleID:               "local.pebble.mobile",
		PushToken:              "tok-push-1",
		LiveActivityStartToken: "tok-start-1",
		Enabled:                true,
	}); err != nil {
		t.Fatalf("UpsertPushDevice: %v", err)
	}

	if err := store.SetPushActivityToken("dev-1", "bot:alpha", "tok-act-1"); err != nil {
		t.Fatalf("SetPushActivityToken: %v", err)
	}

	got, ok, err := store.GetPushDevice("dev-1")
	if err != nil || !ok {
		t.Fatalf("GetPushDevice: ok=%v err=%v", ok, err)
	}
	if got.PushToken != "tok-push-1" || got.LiveActivityStartToken != "tok-start-1" || got.ActivityTokens["bot:alpha"] != "tok-act-1" {
		t.Fatalf("unexpected device: %+v", got)
	}

	// Upsert again without tokens preserves existing tokens while updating enabled flag.
	if err := store.UpsertPushDevice(PushDevice{
		DeviceID: "dev-1",
		Platform: "ios",
		Env:      "sandbox",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("UpsertPushDevice preserve: %v", err)
	}
	got2, _, _ := store.GetPushDevice("dev-1")
	if got2.PushToken != "tok-push-1" || got2.ActivityTokens["bot:alpha"] != "tok-act-1" {
		t.Fatalf("expected preserved tokens, got %+v", got2)
	}

	if err := store.ClearInvalidPushToken("dev-1", "activity", "bot:alpha"); err != nil {
		t.Fatalf("ClearInvalidPushToken activity: %v", err)
	}
	if err := store.ClearInvalidPushToken("dev-1", "push", ""); err != nil {
		t.Fatalf("ClearInvalidPushToken push: %v", err)
	}
	got3, _, _ := store.GetPushDevice("dev-1")
	if got3.PushToken != "" || got3.ActivityTokens["bot:alpha"] != "" {
		t.Fatalf("expected cleared tokens, got %+v", got3)
	}

	list, err := store.ListPushDevices()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListPushDevices: len=%d err=%v", len(list), err)
	}

	if err := store.DeletePushDevice("dev-1"); err != nil {
		t.Fatalf("DeletePushDevice: %v", err)
	}
	if _, ok, err := store.GetPushDevice("dev-1"); err != nil || ok {
		t.Fatalf("expected deleted device, got ok=%v err=%v", ok, err)
	}
}

func TestOpenSQLiteMigratesLegacyBotTasksWorktreeColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacyDB.Exec(`
CREATE TABLE bot_tasks (
  id                 TEXT PRIMARY KEY,
  bot_id             TEXT NOT NULL,
  kind               TEXT NOT NULL DEFAULT 'worker',
  title              TEXT NOT NULL DEFAULT '',
  prompt             TEXT NOT NULL DEFAULT '',
  status             TEXT NOT NULL DEFAULT 'queued',
  device_id          TEXT NOT NULL DEFAULT 'local',
  harness            TEXT NOT NULL DEFAULT '',
  model              TEXT NOT NULL DEFAULT '',
  permission_mode    TEXT NOT NULL DEFAULT '',
  workdir            TEXT NOT NULL DEFAULT '',
  delivery           TEXT NOT NULL DEFAULT 'receipt',
  depends_on_json    TEXT NOT NULL DEFAULT '[]',
  parent_task_id     TEXT NOT NULL DEFAULT '',
  routine_id         TEXT NOT NULL DEFAULT '',
  routine_name       TEXT NOT NULL DEFAULT '',
  trigger            TEXT NOT NULL DEFAULT '',
  slot_key           TEXT NOT NULL DEFAULT '',
  runtime_session_id TEXT NOT NULL DEFAULT '',
  output             TEXT NOT NULL DEFAULT '',
  error              TEXT NOT NULL DEFAULT '',
  tools_json         TEXT NOT NULL DEFAULT '[]',
  duration_ms        INTEGER NOT NULL DEFAULT 0,
  cost_usd           REAL NOT NULL DEFAULT 0,
  input_tokens       INTEGER NOT NULL DEFAULT 0,
  output_tokens      INTEGER NOT NULL DEFAULT 0,
  created_at         TEXT NOT NULL,
  started_at         TEXT NOT NULL DEFAULT '',
  finished_at        TEXT NOT NULL DEFAULT '',
  injected_at        TEXT NOT NULL DEFAULT ''
);`)
	if err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	_ = legacyDB.Close()

	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("openSQLite failed on legacy bot_tasks table: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`PRAGMA table_info(bot_tasks)`)
	if err != nil {
		t.Fatal(err)
	}
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctyp string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		cols[name] = true
	}
	_ = rows.Close()
	for _, want := range []string{"workspace_id", "isolation", "worktree_branch", "worktree_path", "checkpoint_ref"} {
		if !cols[want] {
			t.Fatalf("expected migrated column %q on bot_tasks", want)
		}
	}
}
