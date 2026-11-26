package channel

import (
	"bufio"
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type taskHTTPHarness struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	fn      func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error)
}

func (h *taskHTTPHarness) Kind() string                             { return "opencode" }
func (h *taskHTTPHarness) Label() string                            { return "OpenCode" }
func (h *taskHTTPHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (h *taskHTTPHarness) Close()                                   {}
func (h *taskHTTPHarness) PermissionCapabilities() harness.PermissionCapabilities {
	return harness.PermissionCapabilities{
		Modes:               []string{config.PermissionModeAuto, config.PermissionModeAsk},
		InteractiveApproval: true,
		SupportsAllowAlways: true,
	}
}
func (h *taskHTTPHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	if h.fn != nil {
		return h.fn(ctx, opts)
	}
	if opts.OnText != nil {
		opts.OnText("part1:")
	}
	if opts.OnToolUse != nil {
		opts.OnToolUse("read_file")
	}
	return &harness.TurnResult{
		BotID:     opts.Bot.ID,
		SessionID: "http_rt_1",
		Text:      "done:" + opts.Prompt,
	}, nil
}

func TestHTTPTaskLifecycleAndCoordinatorApproval(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	th := &taskHTTPHarness{
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			if opts.Prompt == "wait-approval" && opts.OnPermission != nil {
				resp, err := opts.OnPermission(ctx, harness.PermissionRequest{
					ID:       "p1",
					ToolName: "bash",
					Kind:     "shell",
					Summary:  "ls",
				})
				if err != nil {
					return nil, err
				}
				return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "s_apr", Text: "perm:" + resp.Decision}, nil
			}
			if opts.Prompt == "block-cancel" {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			if opts.OnText != nil {
				opts.OnText("hello-chunk")
			}
			if opts.OnToolUse != nil {
				opts.OnToolUse("grep")
			}
			return &harness.TurnResult{
				BotID:     opts.Bot.ID,
				SessionID: "rt_http_1",
				Text:      "ok:" + opts.Prompt,
			}, nil
		},
	}
	reg.Register(th)

	allowCoord := false
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "ops"), Harness: "opencode", PermissionMode: config.PermissionModeAsk, CoordinatorCanApprove: &allowCoord},
			{ID: "helper", Name: "Helper", Workspace: filepath.Join(tmp, "helper"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer srv.Shutdown(t.Context())
	mux := srv.srv.Handler

	doJSON := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:3789"
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. POST /api/bots/{id}/tasks -> 201 Created
	spawnRec := doJSON(http.MethodPost, "/api/bots/ops/tasks", `{"title":"Inspect","prompt":"check logs","delivery":"silent"}`)
	if spawnRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/bots/ops/tasks failed: %d %s", spawnRec.Code, spawnRec.Body.String())
	}
	var spawned state.BotTask
	_ = json.Unmarshal(spawnRec.Body.Bytes(), &spawned)
	if spawned.ID == "" || spawned.BotID != "ops" {
		t.Fatalf("unexpected spawned task: %+v", spawned)
	}

	// Poll GET /api/tasks/{taskId} until completed.
	deadline := time.Now().Add(2 * time.Second)
	var got state.BotTask
	for time.Now().Before(deadline) {
		getRec := doJSON(http.MethodGet, "/api/tasks/"+spawned.ID, "")
		if getRec.Code != http.StatusOK {
			t.Fatalf("GET /api/tasks/{id} failed: %d", getRec.Code)
		}
		_ = json.Unmarshal(getRec.Body.Bytes(), &got)
		if got.Status == state.TaskStatusCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Status != state.TaskStatusCompleted || got.Output != "ok:check logs" {
		t.Fatalf("expected completed task, got %+v", got)
	}

	// GET /api/tasks/{taskId}/events
	evRec := doJSON(http.MethodGet, "/api/tasks/"+spawned.ID+"/events", "")
	if evRec.Code != http.StatusOK || !strings.Contains(evRec.Body.String(), "hello-chunk") || !strings.Contains(evRec.Body.String(), "grep") {
		t.Fatalf("GET events failed: %d %s", evRec.Code, evRec.Body.String())
	}

	// GET /api/bots/{id}/tasks
	listRec := doJSON(http.MethodGet, "/api/bots/ops/tasks?kind=worker", "")
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), spawned.ID) {
		t.Fatalf("GET bot tasks failed: %d %s", listRec.Code, listRec.Body.String())
	}

	// POST /api/tasks/{taskId}/send
	sendRec := doJSON(http.MethodPost, "/api/tasks/"+spawned.ID+"/send", `{"message":"follow-up"}`)
	if sendRec.Code != http.StatusOK {
		t.Fatalf("POST /api/tasks/{id}/send failed: %d %s", sendRec.Code, sendRec.Body.String())
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = json.Unmarshal(doJSON(http.MethodGet, "/api/tasks/"+spawned.ID, "").Body.Bytes(), &got)
		if got.Status == state.TaskStatusCompleted && got.Output == "ok:follow-up" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Output != "ok:follow-up" {
		t.Fatalf("expected follow-up completion, got %+v", got)
	}

	// POST /api/tasks/{taskId}/cancel
	cRec := doJSON(http.MethodPost, "/api/bots/ops/tasks", `{"prompt":"block-cancel","delivery":"silent"}`)
	var cTask state.BotTask
	_ = json.Unmarshal(cRec.Body.Bytes(), &cTask)
	cancelRec := doJSON(http.MethodPost, "/api/tasks/"+cTask.ID+"/cancel", "")
	if cancelRec.Code != http.StatusOK || !strings.Contains(cancelRec.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("POST cancel failed: %d %s", cancelRec.Code, cancelRec.Body.String())
	}

	// Coordinator approval policy enforcement (403 when disabled, 200 when enabled).
	aprSpawn := doJSON(http.MethodPost, "/api/bots/ops/tasks", `{"prompt":"wait-approval","delivery":"silent"}`)
	var aprTask state.BotTask
	_ = json.Unmarshal(aprSpawn.Body.Bytes(), &aprTask)

	var aprID string
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if items := h.ListApprovals("ops"); len(items) == 1 {
			aprID = items[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if aprID == "" {
		t.Fatal("expected pending approval for wait-approval task")
	}
	forbRec := doJSON(http.MethodPost, "/api/approvals/"+aprID+"/reply", `{"decision":"allow_once","callerBotId":"ops"}`)
	if forbRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when coordinatorCanApprove=false, got %d %s", forbRec.Code, forbRec.Body.String())
	}
	trueVal := true
	if _, err := h.UpdateBot("ops", hub.BotUpdate{CoordinatorCanApprove: &trueVal}); err != nil {
		t.Fatal(err)
	}
	okRec := doJSON(http.MethodPost, "/api/approvals/"+aprID+"/reply", `{"decision":"allow_once","callerBotId":"ops"}`)
	if okRec.Code != http.StatusOK {
		t.Fatalf("expected 200 after enabling coordinatorCanApprove, got %d %s", okRec.Code, okRec.Body.String())
	}

	// Async cross-bot message via POST /api/bots/{id}/message
	asyncRec := doJSON(http.MethodPost, "/api/bots/helper/message", `{"fromBotId":"ops","message":"ping","async":true}`)
	if asyncRec.Code != http.StatusAccepted || !strings.Contains(asyncRec.Body.String(), `"async":true`) {
		t.Fatalf("expected 202 async message response, got %d %s", asyncRec.Code, asyncRec.Body.String())
	}
}

func TestSSEMonotonicSeqAndSinceReplay(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(&taskHTTPHarness{})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "ops"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer srv.Shutdown(t.Context())
	ts := httptest.NewServer(srv.srv.Handler)
	defer ts.Close()

	// Connect initial SSE client and read at least 1 event with id: <seq>.
	ctx1, cancel1 := context.WithCancel(context.Background())
	req1, _ := http.NewRequestWithContext(ctx1, http.MethodGet, ts.URL+"/api/events", nil)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp1.Body.Close()

	// Trigger task creation & completion to produce task_* SSE events.
	b, _ := json.Marshal(hub.TaskSpawnRequest{Prompt: "sse-1", Delivery: state.TaskDeliverySilent})
	postResp, err := http.Post(ts.URL+"/api/bots/ops/tasks", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var t1 state.BotTask
	_ = json.NewDecoder(postResp.Body).Decode(&t1)
	_ = postResp.Body.Close()
	if _, err := h.WaitTask(context.Background(), t1.ID); err != nil {
		t.Fatal(err)
	}

	// Read first non-ready event from stream 1 to capture its seq cursor.
	sc1 := bufio.NewScanner(resp1.Body)
	var firstSeq string
	var curID, curEvent string
	for sc1.Scan() {
		line := sc1.Text()
		if strings.HasPrefix(line, "id: ") {
			curID = strings.TrimPrefix(line, "id: ")
		} else if strings.HasPrefix(line, "event: ") {
			curEvent = strings.TrimPrefix(line, "event: ")
		} else if line == "" && curEvent != "" && curEvent != "ready" && curID != "" {
			firstSeq = curID
			break
		}
	}
	cancel1()
	if firstSeq == "" {
		t.Fatal("expected SSE event with non-empty id: <seq>")
	}

	// While disconnected, emit another task so events occur after firstSeq.
	b2, _ := json.Marshal(hub.TaskSpawnRequest{Prompt: "sse-2", Delivery: state.TaskDeliverySilent})
	postResp2, err := http.Post(ts.URL+"/api/bots/ops/tasks", "application/json", bytes.NewReader(b2))
	if err != nil {
		t.Fatal(err)
	}
	var t2 state.BotTask
	_ = json.NewDecoder(postResp2.Body).Decode(&t2)
	_ = postResp2.Body.Close()
	if _, err := h.WaitTask(context.Background(), t2.ID); err != nil {
		t.Fatal(err)
	}

	// Reconnect with ?since=<firstSeq> and verify missed t2 events are replayed!
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, ts.URL+"/api/events?since="+firstSeq, nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	sc2 := bufio.NewScanner(resp2.Body)
	var replayed []string
	var sawT2Completed bool
	for sc2.Scan() {
		line := sc2.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			replayed = append(replayed, data)
			if strings.Contains(data, t2.ID) && strings.Contains(data, `"task_completed"`) {
				sawT2Completed = true
				break
			}
		}
	}
	if !sawT2Completed {
		t.Fatalf("expected ?since=%s replay to include task_completed for %s, got %v", firstSeq, t2.ID, replayed)
	}
}
