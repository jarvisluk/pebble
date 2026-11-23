package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreSQLiteLifecycleSearchAndRecall(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if !strings.HasSuffix(store.DBPath(), "hub.db") {
		t.Fatalf("unexpected db path: %s", store.DBPath())
	}

	// Turn 1..2 on bot alpha
	s1, err := store.EnsureActiveSession("alpha", "opencode", "magpie/sonnet")
	if err != nil {
		t.Fatalf("EnsureActiveSession: %v", err)
	}
	if _, err := store.AppendMessage(ChatMessage{
		BotID:     "alpha",
		SessionID: s1.ID,
		Turn:      1,
		Role:      "user",
		From:      "web",
		Text:      "排查 Docker 端口 8080 占用情况",
	}); err != nil {
		t.Fatal(err)
	}
	longOut := strings.Repeat("A", 120)
	replyMsg, err := store.AppendMessage(ChatMessage{
		BotID:       "alpha",
		SessionID:   s1.ID,
		Turn:        1,
		Role:        "assistant",
		From:        "bot:alpha",
		Text:        "Docker 服务占用了端口 8080：" + longOut,
		Tools:       []string{"Bash×2", "Read"},
		InputTokens: 4200,
		CostUSD:     0.02,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec1, err := store.RecordTurnDetail(s1.ID, TurnRecordUpdate{
		RuntimeSessionID: "oc_ses_1",
		Harness:          "opencode",
		Model:            "magpie/sonnet",
		PromptTitle:      "排查 Docker 端口 8080 占用情况",
		CostUSD:          0.02,
		InputTokens:      4200,
		IncrementTurn:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec1.SessionID != "oc_ses_1" || rec1.MagpieSession != "opencode:oc_ses_1" || rec1.LastInputTokens != 4200 {
		t.Fatalf("unexpected TurnRecordUpdate result: %+v", rec1)
	}

	// Archive s1 as error
	prev, existed, err := store.ArchiveActive("alpha", "error", "context_length_exceeded", "handoff to next")
	if err != nil || !existed || prev.ID != s1.ID {
		t.Fatalf("ArchiveActive failed: prev=%+v existed=%v err=%v", prev, existed, err)
	}
	if _, ok := store.Get("alpha"); ok {
		t.Fatal("expected no active session after ArchiveActive")
	}

	// Another bot beta
	s2, err := store.EnsureActiveSession("beta", "claude", "opus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessage(ChatMessage{
		BotID:     "beta",
		SessionID: s2.ID,
		Turn:      1,
		Role:      "user",
		From:      "web",
		Text:      "检查数据库迁移脚本",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTurnDetail(s2.ID, TurnRecordUpdate{
		RuntimeSessionID: "cc_uuid_2",
		Harness:          "claude",
		IncrementTurn:    true,
	}); err != nil {
		t.Fatal(err)
	}

	// ListSessions: FTS5 trigram query (>= 3 chars)
	hits, err := store.ListSessions(SessionListOptions{Query: "Docker", Limit: 10})
	if err != nil {
		t.Fatalf("ListSessions trigram: %v", err)
	}
	if len(hits) != 1 || hits[0].SessionID != s1.ID || len(hits[0].Snippets) == 0 {
		t.Fatalf("unexpected trigram search hits: %+v", hits)
	}
	if hits[0].MagpieSession != "opencode:oc_ses_1" {
		t.Fatalf("expected magpieSession opencode:oc_ses_1, got %q", hits[0].MagpieSession)
	}

	// ListSessions: short 2-char Chinese query (< 3 runes) must fall back to LIKE
	shortHits, err := store.ListSessions(SessionListOptions{Query: "端口", Limit: 10})
	if err != nil {
		t.Fatalf("ListSessions short LIKE: %v", err)
	}
	if len(shortHits) != 1 || shortHits[0].SessionID != s1.ID {
		t.Fatalf("expected 1 hit for short query 端口, got %+v", shortHits)
	}

	// ExcludedBotIDs (privateSessions)
	filtered, err := store.ListSessions(SessionListOptions{ExcludedBotIDs: []string{"alpha"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range filtered {
		if h.BotID == "alpha" {
			t.Fatalf("excluded bot alpha returned: %+v", h)
		}
	}

	// RecallSession with "previous" & maxChars truncation
	res, err := store.RecallSession(RecallOptions{
		BotID:     "alpha",
		SessionID: "previous",
		MaxChars:  40,
	})
	if err != nil {
		t.Fatalf("RecallSession previous: %v", err)
	}
	if res.Session.SessionID != s1.ID || res.Session.Status != "error" || len(res.Messages) != 2 {
		t.Fatalf("unexpected recall result: %+v", res)
	}
	if !strings.Contains(res.Messages[1].Text, "[...truncated ") || !strings.Contains(res.Messages[1].Text, replyMsg.ID) {
		t.Fatalf("expected truncation marker with messageId, got %q", res.Messages[1].Text)
	}

	// Single-message full recall bypasses truncation
	fullRes, err := store.RecallSession(RecallOptions{MessageID: replyMsg.ID, MaxChars: 40})
	if err != nil {
		t.Fatalf("RecallSession messageId: %v", err)
	}
	if len(fullRes.Messages) != 1 || !strings.Contains(fullRes.Messages[0].Text, longOut) {
		t.Fatalf("expected untruncated full text, got %+v", fullRes)
	}

	// Recall by runtimeSessionId directly
	byRuntime, err := store.RecallSession(RecallOptions{SessionID: "oc_ses_1"})
	if err != nil || byRuntime.Session.SessionID != s1.ID {
		t.Fatalf("RecallSession by runtimeSessionId failed: res=%+v err=%v", byRuntime, err)
	}
}

func TestLegacyMigrationFromJSONAndJSONL(t *testing.T) {
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, "state")
	msgDir := filepath.Join(stateDir, "messages")
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionsJSON := `{
  "assistant": {
    "sessionId": "ses_current",
    "createdAt": "2026-11-20T10:00:00Z",
    "lastActiveAt": "2026-11-20T11:00:00Z",
    "turnCount": 2,
    "totalCostUsd": 0.05
  }
}`
	if err := os.WriteFile(filepath.Join(stateDir, "sessions.json"), []byte(sessionsJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	jsonl := strings.Join([]string{
		`{"id":"m1","botId":"assistant","role":"user","from":"web","text":"旧会话问题","createdAt":"2026-11-20T09:00:00Z"}`,
		`{"id":"m2","botId":"assistant","role":"assistant","from":"bot:assistant","text":"旧会话回答","sessionId":"ses_old","costUsd":0.01,"createdAt":"2026-11-20T09:00:05Z"}`,
		`{"id":"m3","botId":"assistant","role":"system","from":"system","text":"Session 已重置（旧 Session: ses_old），Workspace 和 memory/ 已保留。","createdAt":"2026-11-20T09:05:00Z"}`,
		`{"id":"m4","botId":"assistant","role":"user","from":"web","text":"新会话第一问","createdAt":"2026-11-20T10:00:00Z"}`,
		`{"id":"m5","botId":"assistant","role":"assistant","from":"bot:assistant","text":"新会话回答","sessionId":"ses_current","costUsd":0.03,"createdAt":"2026-11-20T10:00:05Z"}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(msgDir, "assistant.jsonl"), []byte(jsonl), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(filepath.Join(stateDir, "sessions.json"))
	if err != nil {
		t.Fatalf("NewStore migrate: %v", err)
	}
	defer store.Close()

	// Active session preserved with runtimeSessionId
	act, ok := store.Get("assistant")
	if !ok || act.SessionID != "ses_current" || act.TurnCount != 2 {
		t.Fatalf("unexpected migrated active session: %+v (ok=%v)", act, ok)
	}

	// Archived previous session reconstructed from JSONL
	prev, ok := store.LatestArchivedSession("assistant")
	if !ok || prev.RuntimeSessionID != "ses_old" || prev.Status != "reset" {
		t.Fatalf("unexpected migrated archived session: %+v (ok=%v)", prev, ok)
	}

	// Recall archived session shows m1, m2, m3 properly attached to ses_old
	rec, err := store.RecallSession(RecallOptions{BotID: "assistant", SessionID: "previous"})
	if err != nil {
		t.Fatalf("RecallSession migrated previous: %v", err)
	}
	if len(rec.Messages) != 3 || rec.Messages[0].ID != "m1" || rec.Messages[2].ID != "m3" {
		t.Fatalf("unexpected migrated messages for old session: %+v", rec.Messages)
	}

	// UI message list still returns all 5 messages chronologically
	all, err := store.ListMessages("assistant", 50)
	if err != nil || len(all) != 5 {
		t.Fatalf("expected 5 migrated messages, got %d (err=%v)", len(all), err)
	}
}

func TestRoutineRunsReceiptsAndActiveRecall(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Active session recall via "active"
	sess, err := store.EnsureActiveSession("ops", "opencode", "magpie/flash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessage(ChatMessage{
		BotID: "ops", SessionID: sess.ID, Turn: 1, Role: "user", From: "web", Text: "部署进度如何？",
	}); err != nil {
		t.Fatal(err)
	}
	actRecall, err := store.RecallSession(RecallOptions{BotID: "ops", SessionID: "active"})
	if err != nil {
		t.Fatalf("RecallSession active: %v", err)
	}
	if actRecall.Session.SessionID != sess.ID || len(actRecall.Messages) != 1 {
		t.Fatalf("unexpected active recall: %+v", actRecall)
	}

	// Routine runs + pending receipts lifecycle
	nowLocal := time.Now().In(time.Local)
	r1, err := store.RecordRoutineRun(RoutineRun{
		BotID: "ops", RoutineID: "disk-check", RoutineName: "磁盘巡检",
		Trigger: "schedule", Status: "ok", Delivery: "receipt",
		Output: "磁盘使用率 82%", StartedAt: nowLocal.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordRoutineRun(RoutineRun{
		BotID: "ops", RoutineID: "silent-check", RoutineName: "静默巡检",
		Trigger: "schedule", Status: "silent", Delivery: "receipt", Output: "",
	}); err != nil {
		t.Fatal(err)
	}
	r2, err := store.RecordRoutineRun(RoutineRun{
		BotID: "ops", RoutineID: "err-check", RoutineName: "日志巡检",
		Trigger: "manual", Status: "ok", Delivery: "receipt", Output: "发现 2 条错误",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordRoutineRun(RoutineRun{
		BotID: "ops", RoutineID: "notify-only", RoutineName: "即时告警",
		Trigger: "schedule", Status: "ok", Delivery: "notify", Output: "CPU 99%",
	}); err != nil {
		t.Fatal(err)
	}

	if !store.HasDailyRunOnDate("ops", "disk-check", nowLocal.Format("2006-01-02")) {
		t.Fatal("expected HasDailyRunOnDate=true")
	}
	if store.HasDailyRunOnDate("ops", "err-check", nowLocal.Format("2006-01-02")) {
		t.Fatal("manual run should not count as daily schedule run")
	}

	pending, err := store.ListPendingReceipts("ops", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].ID != r1.ID || pending[1].ID != r2.ID {
		t.Fatalf("expected 2 pending receipts oldest-first, got %+v", pending)
	}

	if err := store.MarkReceiptsInjected([]string{r1.ID}); err != nil {
		t.Fatal(err)
	}
	pending, _ = store.ListPendingReceipts("ops", 5)
	if len(pending) != 1 || pending[0].ID != r2.ID {
		t.Fatalf("expected only r2 pending after marking r1, got %+v", pending)
	}

	if err := store.DismissPendingReceipts("ops"); err != nil {
		t.Fatal(err)
	}
	pending, _ = store.ListPendingReceipts("ops", 5)
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending after dismiss, got %+v", pending)
	}

	runs, err := store.ListRoutineRuns("ops", "", 10)
	if err != nil || len(runs) != 4 {
		t.Fatalf("expected 4 runs, got %d (err=%v)", len(runs), err)
	}
	latest, ok := store.LatestRoutineRun("ops", "disk-check")
	if !ok || latest.ID != r1.ID {
		t.Fatalf("unexpected LatestRoutineRun: %+v ok=%v", latest, ok)
	}
}

func TestStoreMediaInternalAndCollab(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.EnsureActiveSession("alpha", "opencode", "m1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.AppendMessage(ChatMessage{
		BotID: "alpha", SessionID: sess.ID, Turn: 1, Role: "internal", From: "bot:alpha", Text: "raw trace",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessage(ChatMessage{
		BotID: "alpha", SessionID: sess.ID, Turn: 1, Role: "assistant", From: "bot:alpha", Text: "visible",
		Media: []MessageMedia{{ID: "med_1.png", URL: "/api/media/med_1.png", MimeType: "image/png", Size: 123}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessage(ChatMessage{
		BotID: "alpha", SessionID: sess.ID, Turn: 1, Role: "bot_out", From: "bot:alpha", To: "bot:beta", Text: "ping beta",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessage(ChatMessage{
		BotID: "alpha", SessionID: sess.ID, Turn: 1, Role: "bot_in", From: "bot:beta", To: "bot:alpha", Text: "pong alpha",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessage(ChatMessage{
		BotID: "alpha", SessionID: sess.ID, Turn: 1, Role: "bot_out", From: "bot:alpha", To: "bot:gamma", Text: "other peer",
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := store.ListMessages("alpha", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 non-internal messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.Role == "internal" {
			t.Fatalf("internal message leaked into ListMessages: %+v", m)
		}
	}
	if len(msgs[0].Media) != 1 || msgs[0].Media[0].ID != "med_1.png" {
		t.Fatalf("expected media_json roundtrip, got %+v", msgs[0].Media)
	}

	collab, err := store.ListCollabMessages("alpha", "beta", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(collab) != 2 || collab[0].Text != "ping beta" || collab[1].Text != "pong alpha" {
		t.Fatalf("unexpected ListCollabMessages: %+v", collab)
	}
}

func TestUpdateMessageMeta(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess, err := store.EnsureActiveSession("alpha", "opencode", "m1")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.AppendMessage(ChatMessage{
		BotID: "alpha", SessionID: sess.ID, Turn: 1, Role: "assistant", From: "bot:alpha", Text: "reply",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, ok, err := store.UpdateMessageMeta(saved.ID, []string{"bash×3", "edit"}, 4200, 0.0123, 100, 50)
	if err != nil || !ok {
		t.Fatalf("UpdateMessageMeta failed: ok=%v err=%v", ok, err)
	}
	if len(updated.Tools) != 2 || updated.Tools[0] != "bash×3" || updated.DurationMs != 4200 || updated.CostUSD != 0.0123 || updated.InputTokens != 100 || updated.OutputTokens != 50 {
		t.Fatalf("unexpected updated message: %+v", updated)
	}

	msgs, err := store.ListMessages("alpha", 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("ListMessages failed: len=%d err=%v", len(msgs), err)
	}
	if len(msgs[0].Tools) != 2 || msgs[0].Tools[0] != "bash×3" || msgs[0].DurationMs != 4200 || msgs[0].CostUSD != 0.0123 {
		t.Fatalf("expected persisted meta from ListMessages, got %+v", msgs[0])
	}

	if _, missing, err := store.UpdateMessageMeta("nonexistent", []string{"bash"}, 100, 0, 0, 0); err != nil || missing {
		t.Fatalf("expected missing=false err=nil for unknown id, got ok=%v err=%v", missing, err)
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
		BundleID:               "local.bothub.mobile",
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
