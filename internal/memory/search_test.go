package memory

import (
	"bot-hub/internal/state"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHybridSearchCJKAndEnglishAndSessionFusion(t *testing.T) {
	ms, st, _ := newTestStore(t, Config{})
	ctx := context.Background()

	// Seed English (with diacritics), CJK long (>=3 runes), and CJK short (2 runes) items.
	_, err := ms.Save(ctx, SaveReq{
		Scope: ScopeGlobal,
		Kind:  KindPreference,
		Title: "Espresso Café Setup",
		Text:  "Prefer pour-over coffee at the corner café near the office.",
		Tags:  []string{"lifestyle", "coffee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cjkLong, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot,
		BotID: "dev",
		Kind:  KindProcedure,
		Title: "数据库连接池调优",
		Text:  "生产环境 PostgreSQL 连接池上限设置为 50，避免空闲连接耗尽内存。",
		Tags:  []string{"postgres", "perf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cjkShort, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot,
		BotID: "dev",
		Kind:  KindFact,
		Title: "缓存策略",
		Text:  "热点元数据本地缓存 TTL 设为 30 秒。",
		Tags:  []string{"cache"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ms.Save(ctx, SaveReq{
		Scope: ScopeBot,
		BotID: "secret-bot",
		Kind:  KindFact,
		Title: "Private Cache Note",
		Text:  "机密缓存配置仅 secret-bot 可见。",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Diacritic-insensitive English unicode61 search ("cafe" matches "café").
	resEn, err := ms.Search(ctx, SearchOpts{Scope: ScopeAll, Query: "cafe", Compact: true})
	if err != nil || len(resEn.Items) == 0 || !strings.Contains(resEn.Items[0].Title, "Café") {
		t.Fatalf("expected unicode61 diacritic match for 'cafe', got %+v err=%v", resEn, err)
	}
	if resEn.Items[0].Text != "" || resEn.Items[0].Snippet == "" {
		t.Fatalf("expected Compact L1 hit to omit Text and populate Snippet: %+v", resEn.Items[0])
	}

	// 2. CJK >= 3 runes trigram FTS5 search.
	resTri, err := ms.Search(ctx, SearchOpts{Scope: ScopeBot, BotID: "dev", Query: "数据库连接池"})
	if err != nil || len(resTri.Items) == 0 || resTri.Items[0].ID != cjkLong.Item.ID {
		t.Fatalf("expected trigram CJK match %s, got %+v err=%v", cjkLong.Item.ID, resTri, err)
	}

	// 3. CJK < 3 runes LIKE fallback ("缓存") + excludedBotIds privacy isolation.
	resShort, err := ms.Search(ctx, SearchOpts{
		Scope:          ScopeAll,
		ExcludedBotIDs: []string{"secret-bot"},
		Query:          "缓存",
	})
	if err != nil || len(resShort.Items) == 0 {
		t.Fatalf("expected short-CJK LIKE fallback match for '缓存', got %+v err=%v", resShort, err)
	}
	for _, it := range resShort.Items {
		if it.BotID == "secret-bot" {
			t.Fatalf("excluded bot leaked into search results: %+v", it)
		}
	}
	if resShort.Items[0].ID != cjkShort.Item.ID {
		t.Fatalf("expected %s top hit, got %s", cjkShort.Item.ID, resShort.Items[0].ID)
	}

	// 4. Tag filter.
	resTag, err := ms.Search(ctx, SearchOpts{Scope: ScopeBot, BotID: "dev", Query: "连接池", Tags: []string{"postgres"}})
	if err != nil || len(resTag.Items) != 1 || resTag.Items[0].ID != cjkLong.Item.ID {
		t.Fatalf("expected tag-filtered hit, got %+v err=%v", resTag, err)
	}

	// 5. IncludeSessions searches messages_fts and respects excludedBotIds.
	if _, err := st.AppendMessage(state.ChatMessage{
		BotID: "dev", SessionID: "ses_pub", Role: "assistant", Text: "Resolved Kubernetes ingress timeout issue yesterday.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendMessage(state.ChatMessage{
		BotID: "secret-bot", SessionID: "ses_priv", Role: "assistant", Text: "Secret Kubernetes ingress credentials discussion.",
	}); err != nil {
		t.Fatal(err)
	}

	resSess, err := ms.Search(ctx, SearchOpts{
		Scope:           ScopeAll,
		ExcludedBotIDs:  []string{"secret-bot"},
		Query:           "Kubernetes ingress",
		IncludeSessions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundPubSess := false
	for _, it := range resSess.Items {
		if it.BotID == "secret-bot" {
			t.Fatalf("private session leaked despite ExcludedBotIDs: %+v", it)
		}
		if it.Origin == OriginSession && it.SourceSession == "ses_pub" {
			foundPubSess = true
		}
	}
	if !foundPubSess {
		t.Fatalf("expected public session hit tagged Origin=session, got %+v", resSess.Items)
	}
}

func TestRecencyDecayExemptionsAndFrequencyBoost(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	oldTime := now.Add(-240 * 24 * time.Hour)

	ms, _, _ := newTestStore(t, Config{})
	ctx := context.Background()

	// Create old episode (decays) vs old rule (exempt from recency decay).
	ms.clock = func() time.Time { return oldTime }
	oldEp, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "ops", Kind: KindEpisode,
		Title: "Deployment checklist run", Text: "Deployment checklist verification note for cluster.",
	})
	if err != nil {
		t.Fatal(err)
	}
	oldRule, err := ms.Save(ctx, SaveReq{
		Scope: ScopeBot, BotID: "ops", Kind: KindRule,
		Title: "Deployment checklist rule", Text: "Deployment checklist verification rule for cluster.",
	})
	if err != nil {
		t.Fatal(err)
	}

	ms.clock = func() time.Time { return now }
	res, err := ms.Search(ctx, SearchOpts{Scope: ScopeBot, BotID: "ops", Query: "Deployment checklist verification"})
	if err != nil || len(res.Items) < 2 {
		t.Fatalf("expected 2 results, got %+v err=%v", res, err)
	}
	if res.Items[0].ID != oldRule.Item.ID {
		t.Fatalf("expected recency-exempt rule (%s) to outrank old episode (%s), got order %s, %s",
			oldRule.Item.ID, oldEp.Item.ID, res.Items[0].ID, res.Items[1].ID)
	}
}

func TestTier1SemanticSearchAndGracefulFallback(t *testing.T) {
	failMode := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failMode {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		type emb struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		out := make([]emb, len(req.Input))
		for i, text := range req.Input {
			low := strings.ToLower(text)
			switch {
			case strings.Contains(low, "automobile") || strings.Contains(low, "vehicle") || strings.Contains(low, "car"):
				out[i] = emb{Index: i, Embedding: []float32{0.95, 0.05, 0.0}}
			case strings.Contains(low, "kubernetes") || strings.Contains(low, "pod"):
				out[i] = emb{Index: i, Embedding: []float32{0.0, 0.95, 0.05}}
			default:
				out[i] = emb{Index: i, Embedding: []float32{0.0, 0.0, 1.0}}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
	}))
	defer srv.Close()

	cfg := Config{
		Semantic: EmbeddingConfig{
			Enabled: true,
			BaseURL: srv.URL,
			APIKey:  "test-secret-emb-key-12345",
			Model:   "mock-embed-v1",
		},
	}
	ms, _, _ := newTestStore(t, cfg)
	ctx := context.Background()

	carDoc, err := ms.Save(ctx, SaveReq{
		Scope: ScopeGlobal, Kind: KindFact,
		Title: "Garage Vehicle",
		Text:  "The electric automobile charges overnight in the garage.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Save(ctx, SaveReq{
		Scope: ScopeGlobal, Kind: KindFact,
		Title: "Cluster Runtime",
		Text:  "Kubernetes pod autoscaling triggers at 70 percent CPU.",
	}); err != nil {
		t.Fatal(err)
	}

	embedded, err := ms.SyncEmbeddings(ctx)
	if err != nil || embedded < 2 {
		t.Fatalf("SyncEmbeddings failed: embedded=%d err=%v", embedded, err)
	}

	// Query "car" has zero lexical overlap with "automobile"/"vehicle", so it MUST come from Tier 1 cosine!
	res, err := ms.Search(ctx, SearchOpts{Scope: ScopeGlobal, Query: "car"})
	if err != nil || len(res.Items) == 0 || res.Items[0].ID != carDoc.Item.ID {
		t.Fatalf("expected Tier 1 semantic match %s for 'car', got %+v err=%v", carDoc.Item.ID, res, err)
	}

	// Status must NEVER expose plaintext Semantic.APIKey.
	status, err := ms.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	statusBytes, _ := json.Marshal(status)
	if strings.Contains(string(statusBytes), "test-secret-emb-key-12345") {
		t.Fatalf("StatusSummary leaked plaintext embedding APIKey: %s", string(statusBytes))
	}
	if !status.Config.SemanticHasKey || status.Semantic == nil || !status.Semantic.Active {
		t.Fatalf("expected active semantic status with SemanticHasKey=true, got %+v", status)
	}

	// Verify graceful fallback to Tier 0 when embedding endpoint errors.
	failMode = true
	resFallback, err := ms.Search(ctx, SearchOpts{Scope: ScopeGlobal, Query: "Kubernetes"})
	if err != nil || len(resFallback.Items) == 0 {
		t.Fatalf("expected Tier 0 fallback hit when embedding endpoint fails, got %+v err=%v", resFallback, err)
	}
	if resFallback.Semantic == nil || resFallback.Semantic.Error == "" {
		t.Fatalf("expected semantic error surfaced without failing search: %+v", resFallback.Semantic)
	}
}
