package channel

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type dreamHTTPTestHarness struct{}

func (dreamHTTPTestHarness) Kind() string                             { return "dreamhttp" }
func (dreamHTTPTestHarness) Label() string                            { return "DreamHTTP" }
func (dreamHTTPTestHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (dreamHTTPTestHarness) Close()                                   {}
func (dreamHTTPTestHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{
		BotID: opts.Bot.ID,
		Text:  `{"summary":"http dream ok","operations":[{"action":"NOOP"}]}`,
	}, nil
}

func TestHTTPMemoryEndpointsAndSettingsRedaction(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config", "bots.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	rawCfg := `{
  "memory": {
    "coexistence": "auto",
    "embedding": {
      "enabled": true,
      "baseUrl": "https://api.example.com/v1",
      "apiKey": "${HTTP_TEST_EMBED_KEY}",
      "model": "text-embedding-3-small"
    }
  },
  "bots": [
    {"id": "a", "name": "Private A", "harness": "dreamhttp", "privateSessions": true},
    {"id": "b", "name": "Public B", "harness": "dreamhttp"}
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(rawCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_TEST_EMBED_KEY", "sk-http-real-secret-999")

	cfg, _, err := config.Load(tmp, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.NewStore(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(dreamHTTPTestHarness{})
	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer srv.Shutdown(t.Context())
	mux := srv.srv.Handler

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Host = "127.0.0.1:3789"
		req.Header.Set("Origin", "http://127.0.0.1:3789")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. SecretGuard blocks plaintext secret on POST /api/memory.
	secRec := do(http.MethodPost, "/api/memory", `{"scope":"bot","botId":"a","text":"my key is ghp_1234567890abcdefghijklmnopqrstuvwxyz"}`)
	if secRec.Code != http.StatusBadRequest || !strings.Contains(secRec.Body.String(), `"secret_detected"`) {
		t.Fatalf("expected 400 secret_detected, got %d %s", secRec.Code, secRec.Body.String())
	}

	// 2. POST /api/memory creates bot item; second save with supersedes marks first superseded.
	save1 := do(http.MethodPost, "/api/memory", `{"scope":"bot","botId":"a","kind":"rule","title":"Deploy rule","text":"Run tests before deploy","tags":["ops","ci"],"pin":true}`)
	if save1.Code != http.StatusOK {
		t.Fatalf("POST /api/memory failed: %d %s", save1.Code, save1.Body.String())
	}
	var saved1 struct {
		OK   bool   `json:"ok"`
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	_ = json.Unmarshal(save1.Body.Bytes(), &saved1)
	if !saved1.OK || saved1.ID == "" || saved1.Path == "" {
		t.Fatalf("unexpected save1 response: %s", save1.Body.String())
	}

	save2 := do(http.MethodPost, "/api/memory", `{"scope":"bot","botId":"a","kind":"rule","title":"Deploy rule v2","text":"Run full race tests before deploy","supersedes":"`+saved1.ID+`"}`)
	if save2.Code != http.StatusOK {
		t.Fatalf("POST /api/memory supersedes failed: %d %s", save2.Code, save2.Body.String())
	}
	var saved2 struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(save2.Body.Bytes(), &saved2)

	// 3. GET /api/memory/{id} verifies old item is superseded; private bot 403 for caller b.
	getOld := do(http.MethodGet, "/api/memory/"+saved1.ID, "")
	if getOld.Code != http.StatusOK || !strings.Contains(getOld.Body.String(), `"status":"superseded"`) {
		t.Fatalf("expected superseded status on old item: %d %s", getOld.Code, getOld.Body.String())
	}
	forbGet := do(http.MethodGet, "/api/memory/"+saved2.ID+"?callerBotId=b", "")
	if forbGet.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for caller b reading private bot a, got %d %s", forbGet.Code, forbGet.Body.String())
	}
	forbSearch := do(http.MethodGet, "/api/memory?scope=bot&botId=a&callerBotId=b", "")
	if forbSearch.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for caller b searching private bot a, got %d %s", forbSearch.Code, forbSearch.Body.String())
	}

	// 4. PATCH /api/memory/{id} & GET /api/memory?query=...
	patchRec := do(http.MethodPatch, "/api/memory/"+saved2.ID, `{"title":"Deploy rule final","pin":true}`)
	if patchRec.Code != http.StatusOK || !strings.Contains(patchRec.Body.String(), "Deploy rule final") {
		t.Fatalf("PATCH /api/memory/{id} failed: %d %s", patchRec.Code, patchRec.Body.String())
	}
	searchRec := do(http.MethodGet, "/api/memory?scope=bot&botId=a&query=race&compact=1", "")
	if searchRec.Code != http.StatusOK || !strings.Contains(searchRec.Body.String(), `"backend":"pebble"`) || !strings.Contains(searchRec.Body.String(), saved2.ID) {
		t.Fatalf("GET /api/memory search failed: %d %s", searchRec.Code, searchRec.Body.String())
	}

	// 5. PUT & GET /api/memory/index + line-sliced GET /api/memory/_?path=...
	putIdx := do(http.MethodPut, "/api/memory/index", `{"scope":"global","content":"# Global\n- Line 2\n- Line 3\n"}`)
	if putIdx.Code != http.StatusOK || !strings.Contains(putIdx.Body.String(), `"lines":3`) || !strings.Contains(putIdx.Body.String(), `"content":`) {
		t.Fatalf("PUT /api/memory/index failed: %d %s", putIdx.Code, putIdx.Body.String())
	}
	getIdx := do(http.MethodGet, "/api/memory/index?scope=global", "")
	if getIdx.Code != http.StatusOK || !strings.Contains(getIdx.Body.String(), "Line 2") {
		t.Fatalf("GET /api/memory/index failed: %d %s", getIdx.Code, getIdx.Body.String())
	}
	sliceRec := do(http.MethodGet, "/api/memory/_?path=_shared/memory/MEMORY.md&startLine=2&endLine=2", "")
	if sliceRec.Code != http.StatusOK || !strings.Contains(sliceRec.Body.String(), "Line 2") || strings.Contains(sliceRec.Body.String(), "Line 3") {
		t.Fatalf("GET /api/memory/_ line slice failed: %d %s", sliceRec.Code, sliceRec.Body.String())
	}

	// 6. GET /api/memory/status & GET /api/settings never expose plaintext apiKey!
	stRec := do(http.MethodGet, "/api/memory/status", "")
	if stRec.Code != http.StatusOK || strings.Contains(stRec.Body.String(), "sk-http-real-secret-999") ||
		!strings.Contains(stRec.Body.String(), `"backend":"pebble"`) || !strings.Contains(stRec.Body.String(), `"hasApiKey":true`) {
		t.Fatalf("GET /api/memory/status failed or leaked secret: %d %s", stRec.Code, stRec.Body.String())
	}
	setRec := do(http.MethodGet, "/api/settings", "")
	if setRec.Code != http.StatusOK || strings.Contains(setRec.Body.String(), "sk-http-real-secret-999") ||
		!strings.Contains(setRec.Body.String(), `"memory":`) {
		t.Fatalf("GET /api/settings failed or leaked secret: %d %s", setRec.Code, setRec.Body.String())
	}

	// 7. PUT /api/settings updates memory coexistence & dream without clobbering ${HTTP_TEST_EMBED_KEY}.
	putSet := do(http.MethodPut, "/api/settings", `{"memory":{"coexistence":"isolated","dream":{"dailyAt":"04:30"},"embedding":{"enabled":true,"apiKey":"***"}}}`)
	if putSet.Code != http.StatusOK || !strings.Contains(putSet.Body.String(), `"coexistence":"isolated"`) || !strings.Contains(putSet.Body.String(), `"dailyAt":"04:30"`) {
		t.Fatalf("PUT /api/settings memory failed: %d %s", putSet.Code, putSet.Body.String())
	}
	diskCfg, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(diskCfg), "sk-http-real-secret-999") || !strings.Contains(string(diskCfg), "${HTTP_TEST_EMBED_KEY}") {
		t.Fatalf("expected ${HTTP_TEST_EMBED_KEY} preserved on disk:\n%s", string(diskCfg))
	}

	// 8. POST /api/memory/dream, privacy on GET /api/memory/status, POST /api/memory/reindex, POST /api/memory/import/magpie, DELETE /api/memory/{id}.
	dreamRec := do(http.MethodPost, "/api/memory/dream", `{"botId":"a"}`)
	if dreamRec.Code != http.StatusOK || !strings.Contains(dreamRec.Body.String(), `"http dream ok"`) {
		t.Fatalf("POST /api/memory/dream failed: %d %s", dreamRec.Code, dreamRec.Body.String())
	}
	forbSt := do(http.MethodGet, "/api/memory/status?botId=a&callerBotId=b", "")
	if forbSt.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for caller b fetching private bot a status, got %d %s", forbSt.Code, forbSt.Body.String())
	}
	filtSt := do(http.MethodGet, "/api/memory/status?callerBotId=b", "")
	if filtSt.Code != http.StatusOK || strings.Contains(filtSt.Body.String(), "http dream ok") || strings.Contains(filtSt.Body.String(), `"botId":"a"`) {
		t.Fatalf("private bot a dream leaked into unfiltered status for caller b: %d %s", filtSt.Code, filtSt.Body.String())
	}
	reindexRec := do(http.MethodPost, "/api/memory/reindex", `{}`)
	if reindexRec.Code != http.StatusOK || !strings.Contains(reindexRec.Body.String(), `"docsIndexed":`) || !strings.Contains(reindexRec.Body.String(), `"chunksIndexed":`) {
		t.Fatalf("POST /api/memory/reindex failed: %d %s", reindexRec.Code, reindexRec.Body.String())
	}
	impRec := do(http.MethodPost, "/api/memory/import/magpie", `{"mode":"bots_only","botId":"a"}`)
	if impRec.Code != http.StatusOK || !strings.Contains(impRec.Body.String(), `"ok":true`) {
		t.Fatalf("POST /api/memory/import/magpie failed: %d %s", impRec.Code, impRec.Body.String())
	}
	delRec := do(http.MethodDelete, "/api/memory/"+saved2.ID+"?dismiss=1&reason=done", "")
	if delRec.Code != http.StatusOK || !strings.Contains(delRec.Body.String(), `"dismissed":true`) {
		t.Fatalf("DELETE /api/memory/{id} failed: %d %s", delRec.Code, delRec.Body.String())
	}
}
