package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/state"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type taskTestHarness struct {
	kind string
	caps harness.PermissionCapabilities
	fn   func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error)
}

func (h *taskTestHarness) Kind() string                             { return h.kind }
func (h *taskTestHarness) Label() string                            { return h.kind }
func (h *taskTestHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (h *taskTestHarness) Close()                                   {}
func (h *taskTestHarness) PermissionCapabilities() harness.PermissionCapabilities {
	return h.caps
}
func (h *taskTestHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return h.fn(ctx, opts)
}

func newTaskTestHub(t *testing.T, th harness.Harness, mutCfg func(*config.HubConfig)) (*Hub, *state.Store) {
	t.Helper()
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	msgStore, err := state.NewMessageStore(filepath.Join(tmp, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(th)
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "workspaces", "ops"), Harness: th.Kind()},
		},
	}
	if mutCfg != nil {
		mutCfg(cfg)
	}
	h := New(cfg, store, msgStore, reg)
	t.Cleanup(func() { h.Close() })
	return h, store
}

func TestBotTaskParallelFanOutAndMainLaneResponsiveness(t *testing.T) {
	mainStarted := make(chan struct{})
	mainRelease := make(chan struct{})
	var workerActive atomic.Int32
	var workerMaxConc atomic.Int32
	allWorkersRunning := make(chan struct{})
	releaseWorkers := make(chan struct{})

	th := &taskTestHarness{
		kind: "taskfake",
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			if opts.ExtraEnv["PEBBLE_TASK_ID"] == "" {
				// Main botLane turn
				select {
				case <-mainStarted:
				default:
					close(mainStarted)
				}
				select {
				case <-mainRelease:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "main_s1", Text: "main done"}, nil
			}
			// Concurrent worker turn
			cur := workerActive.Add(1)
			for {
				prev := workerMaxConc.Load()
				if cur <= prev || workerMaxConc.CompareAndSwap(prev, cur) {
					break
				}
			}
			if cur == 3 {
				select {
				case <-allWorkersRunning:
				default:
					close(allWorkersRunning)
				}
			}
			if opts.OnText != nil {
				opts.OnText("chunk:" + opts.Prompt)
			}
			if opts.OnToolUse != nil {
				opts.OnToolUse("bash")
			}
			select {
			case <-releaseWorkers:
			case <-ctx.Done():
				workerActive.Add(-1)
				return nil, ctx.Err()
			}
			workerActive.Add(-1)
			return &harness.TurnResult{
				BotID:      opts.Bot.ID,
				SessionID:  "wrk_" + opts.ExtraEnv["PEBBLE_TASK_ID"],
				Text:       "worker-ok:" + opts.Prompt,
				DurationMs: 15,
				Usage:      harness.Usage{CostUSD: 0.01, InputTokens: 10, OutputTokens: 5},
			}, nil
		},
	}

	h, _ := newTaskTestHub(t, th, nil)

	// Block main botLane.
	mainErrCh := make(chan error, 1)
	go func() {
		_, err := h.Send(context.Background(), "ops", "main work", SendOptions{})
		mainErrCh <- err
	}()
	<-mainStarted

	// Fan-out 3 concurrent workers on the same bot while botLane is blocked!
	var tasks []*state.BotTask
	for i := 1; i <= 3; i++ {
		tk, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
			BotID:    "ops",
			Title:    fmt.Sprintf("Worker %d", i),
			Prompt:   fmt.Sprintf("job-%d", i),
			Delivery: state.TaskDeliverySilent,
		})
		if err != nil {
			t.Fatalf("SpawnTask %d failed: %v", i, err)
		}
		tasks = append(tasks, tk)
	}

	select {
	case <-allWorkersRunning:
	case <-time.After(3 * time.Second):
		t.Fatalf("expected all 3 workers to run concurrently while botLane is blocked (maxConc=%d)", workerMaxConc.Load())
	}
	if workerMaxConc.Load() != 3 {
		t.Fatalf("expected workerMaxConc=3, got %d", workerMaxConc.Load())
	}

	// Complete workers before releasing main botLane to prove workers do not block on botLane.
	close(releaseWorkers)
	for _, tk := range tasks {
		doneTk, err := h.WaitTask(context.Background(), tk.ID)
		if err != nil || doneTk.Status != state.TaskStatusCompleted {
			t.Fatalf("worker %s failed: %+v err=%v", tk.ID, doneTk, err)
		}
		events, err := h.ListTaskEvents(tk.ID, 0, 50)
		if err != nil || len(events) == 0 {
			t.Fatalf("expected task events for %s, got %+v err=%v", tk.ID, events, err)
		}
	}

	close(mainRelease)
	if err := <-mainErrCh; err != nil {
		t.Fatalf("main turn failed: %v", err)
	}
}

func TestBotTaskDependsOnDAGOrderingAndFailurePropagation(t *testing.T) {
	var mu sync.Mutex
	var order []string
	th := &taskTestHarness{
		kind: "dagfake",
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			mu.Lock()
			order = append(order, opts.Prompt)
			mu.Unlock()
			if opts.Prompt == "step-fail" {
				return nil, errors.New("boom")
			}
			return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "s", Text: "ok:" + opts.Prompt}, nil
		},
	}
	h, _ := newTaskTestHub(t, th, nil)

	a, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Prompt: "step-A", Delivery: state.TaskDeliverySilent})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Prompt: "step-B", Delivery: state.TaskDeliverySilent})
	if err != nil {
		t.Fatal(err)
	}
	c, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:     "ops",
		Prompt:    "step-C",
		DependsOn: []string{a.ID, b.ID},
		Delivery:  state.TaskDeliverySilent,
	})
	if err != nil {
		t.Fatal(err)
	}

	doneC, err := h.WaitTask(context.Background(), c.ID)
	if err != nil || doneC.Status != state.TaskStatusCompleted {
		t.Fatalf("expected C completed, got %+v err=%v", doneC, err)
	}
	mu.Lock()
	if len(order) != 3 || order[2] != "step-C" {
		mu.Unlock()
		t.Fatalf("expected step-C to run last after A and B, got order=%v", order)
	}
	mu.Unlock()

	// Downstream of failed task fails with clear error.
	f, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Prompt: "step-fail", Delivery: state.TaskDeliverySilent})
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:     "ops",
		Prompt:    "step-D",
		DependsOn: []string{f.ID},
		Delivery:  state.TaskDeliverySilent,
	})
	if err != nil {
		t.Fatal(err)
	}
	doneD, err := h.WaitTask(context.Background(), d.ID)
	if err != nil || doneD.Status != state.TaskStatusFailed || !strings.Contains(doneD.Error, f.ID) {
		t.Fatalf("expected downstream D to fail due to %s, got %+v err=%v", f.ID, doneD, err)
	}
}

func TestBotTaskDeliveriesWakeReceiptSilentAndHopGuard(t *testing.T) {
	var mu sync.Mutex
	var mainPrompts []string
	th := &taskTestHarness{
		kind: "delfake",
		fn: func(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			if opts.ExtraEnv["PEBBLE_TASK_ID"] == "" {
				mu.Lock()
				mainPrompts = append(mainPrompts, opts.Prompt)
				mu.Unlock()
				return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "main_1", Text: "main-ack"}, nil
			}
			return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "tsk_1", Text: "result:" + opts.Prompt}, nil
		},
	}
	h, store := newTaskTestHub(t, th, nil)

	// 1. Silent: persisted in bot_tasks only; no timeline messages, no main turn.
	sTask, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Prompt: "silent-job", Delivery: state.TaskDeliverySilent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WaitTask(context.Background(), sTask.ID); err != nil {
		t.Fatal(err)
	}
	if msgs, _ := h.Messages("ops", 20); len(msgs) != 0 {
		t.Fatalf("silent delivery should not append timeline messages, got %+v", msgs)
	}

	// 2. Receipt: writes receipt card to timeline without waking main LLM, then injected on next main turn.
	rTask, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Title: "Disk Audit", Prompt: "receipt-job", Delivery: state.TaskDeliveryReceipt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WaitTask(context.Background(), rTask.ID); err != nil {
		t.Fatal(err)
	}
	msgs, _ := h.Messages("ops", 20)
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].From, "task:") || msgs[0].Text != "result:receipt-job" {
		t.Fatalf("expected receipt message on timeline, got %+v", msgs)
	}
	if _, ok := store.Get("ops"); ok {
		t.Fatal("receipt delivery must not wake main session")
	}

	if _, err := h.Send(context.Background(), "ops", "user question", SendOptions{Source: "web"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(mainPrompts) != 1 || !strings.Contains(mainPrompts[0], "result:receipt-job") {
		mu.Unlock()
		t.Fatalf("expected receipt injected into next main prompt, got %+v", mainPrompts)
	}
	mu.Unlock()
	if gotR, _ := h.GetTask(rTask.ID); gotR.InjectedAt == "" {
		t.Fatalf("expected receipt task injected_at marked after main turn")
	}

	// 3. Wake: triggers turn on botLane asynchronously.
	wTask, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Title: "Wake Job", Prompt: "wake-job", Delivery: state.TaskDeliveryWake})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WaitTask(context.Background(), wTask.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	woke := false
	for time.Now().Before(deadline) {
		mu.Lock()
		if len(mainPrompts) >= 2 {
			woke = strings.Contains(mainPrompts[1], "result:wake-job")
			mu.Unlock()
			if woke {
				break
			}
		} else {
			mu.Unlock()
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !woke {
		t.Fatalf("expected wake delivery to trigger main botLane turn, mainPrompts=%+v", mainPrompts)
	}

	// 4. Hop limit safety: task spawned at MaxBotHopDepth must NOT wake main botLane.
	beforeCount := len(mainPrompts)
	hTask, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Title: "Deep Wake", Prompt: "deep-job", Delivery: state.TaskDeliveryWake, Hop: MaxBotHopDepth})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WaitTask(context.Background(), hTask.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	afterCount := len(mainPrompts)
	mu.Unlock()
	if afterCount != beforeCount {
		t.Fatalf("expected wake at MaxBotHopDepth to be suppressed, mainPrompts grew from %d to %d", beforeCount, afterCount)
	}
}

func TestBotTaskSendFollowUpCancelAndStartupSettlement(t *testing.T) {
	var mu sync.Mutex
	var seenSessions []string
	var blockCancel chan struct{}
	var cancelStarted chan struct{}
	turnN := 0

	th := &taskTestHarness{
		kind: "sessfake",
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			if opts.Prompt == "hang-for-cancel" {
				if cancelStarted != nil {
					select {
					case <-cancelStarted:
					default:
						close(cancelStarted)
					}
				}
				select {
				case <-blockCancel:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			mu.Lock()
			turnN++
			seenSessions = append(seenSessions, opts.SessionID)
			sid := opts.SessionID
			if sid == "" {
				sid = fmt.Sprintf("rt_task_%d", turnN)
			}
			mu.Unlock()
			return &harness.TurnResult{
				BotID:     opts.Bot.ID,
				SessionID: sid,
				Text:      "ans:" + opts.Prompt,
			}, nil
		},
	}

	h, store := newTaskTestHub(t, th, nil)

	// SendToTask on completed task preserves runtimeSessionId.
	t1, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Prompt: "turn-1", Delivery: state.TaskDeliverySilent})
	if err != nil {
		t.Fatal(err)
	}
	done1, err := h.WaitTask(context.Background(), t1.ID)
	if err != nil || done1.RuntimeSessionID != "rt_task_1" {
		t.Fatalf("unexpected first completion: %+v err=%v", done1, err)
	}

	if _, err := h.SendToTask(context.Background(), t1.ID, "turn-2"); err != nil {
		t.Fatalf("SendToTask failed: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cur, _ := h.GetTask(t1.ID)
		if cur != nil && cur.Status == state.TaskStatusCompleted && cur.Output == "ans:turn-2" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	done2, _ := h.GetTask(t1.ID)
	if done2.Output != "ans:turn-2" || done2.RuntimeSessionID != "rt_task_1" {
		t.Fatalf("expected follow-up to preserve rt_task_1, got %+v", done2)
	}
	mu.Lock()
	if len(seenSessions) != 2 || seenSessions[0] != "" || seenSessions[1] != "rt_task_1" {
		mu.Unlock()
		t.Fatalf("expected SessionID sequence ['', 'rt_task_1'], got %v", seenSessions)
	}
	mu.Unlock()

	// CancelTask interrupts running task and rejects subsequent SendToTask.
	blockCancel = make(chan struct{})
	cancelStarted = make(chan struct{})
	tCancel, err := h.SpawnTask(context.Background(), TaskSpawnRequest{BotID: "ops", Prompt: "hang-for-cancel", Delivery: state.TaskDeliverySilent})
	if err != nil {
		t.Fatal(err)
	}
	<-cancelStarted
	cancelled, err := h.CancelTask(tCancel.ID)
	if err != nil || cancelled.Status != state.TaskStatusCancelled {
		t.Fatalf("CancelTask failed: %+v err=%v", cancelled, err)
	}
	if _, err := h.SendToTask(context.Background(), tCancel.ID, "nope"); err == nil {
		t.Fatal("expected SendToTask on cancelled task to fail")
	}

	// Startup terminal settlement settles leftover non-terminal tasks on Hub.New.
	tmp := t.TempDir()
	s2, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{
		state.TaskStatusQueued,
		state.TaskStatusRunning,
		state.TaskStatusWaitingApproval,
		state.TaskStatusWaitingQuestion,
		state.TaskStatusWaitingTakeover,
	} {
		if _, err := s2.CreateBotTask(state.BotTask{
			BotID:  "ops",
			Prompt: "stale-" + st,
			Status: st,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s2.CreateBotTask(state.BotTask{
		BotID:      "ops",
		Prompt:     "already-done",
		Status:     state.TaskStatusCompleted,
		FinishedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	reg2 := harness.NewRegistry()
	reg2.Register(th)
	h2 := New(&config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots:        []config.BotConfig{{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "ops"), Harness: th.Kind()}},
	}, s2, nil, reg2)
	defer h2.Close()

	settledList, err := h2.ListTasks("ops", state.TaskListFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	failedCount := 0
	completedCount := 0
	for _, item := range settledList {
		switch item.Status {
		case state.TaskStatusFailed:
			failedCount++
			if item.Error != "interrupted by hub restart" || item.FinishedAt == "" {
				t.Fatalf("unexpected settled task: %+v", item)
			}
		case state.TaskStatusCompleted:
			completedCount++
		default:
			t.Fatalf("expected no non-terminal tasks after Hub.New, got %+v", item)
		}
	}
	if failedCount != 5 || completedCount != 1 {
		t.Fatalf("expected 5 failed and 1 completed after startup settlement, got failed=%d completed=%d", failedCount, completedCount)
	}
	_ = store
}

func TestBotTaskApprovalsQuestionsAndCoordinatorRace(t *testing.T) {
	th := &taskTestHarness{
		kind: "aprfake",
		caps: harness.PermissionCapabilities{
			Modes:               []string{config.PermissionModeAuto, config.PermissionModeAsk},
			InteractiveApproval: true,
			SupportsAllowAlways: true,
		},
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			if opts.OnQuestion == nil || opts.OnPermission == nil {
				return nil, errors.New("expected OnQuestion and OnPermission on worker task")
			}
			qResp, err := opts.OnQuestion(ctx, harness.QuestionRequest{
				ID:     "q_1",
				Title:  "Pick env",
				Fields: []harness.QuestionField{{Key: "env", Title: "Env", Type: "string"}},
			})
			if err != nil {
				return nil, err
			}
			pResp, err := opts.OnPermission(ctx, harness.PermissionRequest{
				ID:       "p_1",
				ToolName: "bash",
				Kind:     "shell",
				Summary:  "echo hi",
			})
			if err != nil {
				return nil, err
			}
			return &harness.TurnResult{
				BotID:     opts.Bot.ID,
				SessionID: "apr_s",
				Text:      fmt.Sprintf("%v:%s", qResp.Answers["env"], pResp.Decision),
			}, nil
		},
	}

	allowCoord := true
	h, _ := newTaskTestHub(t, th, func(cfg *config.HubConfig) {
		cfg.Bots[0].PermissionMode = config.PermissionModeAsk
		cfg.Bots[0].CoordinatorCanApprove = &allowCoord
	})

	tk, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:    "ops",
		Kind:     state.TaskKindDesktop,
		Prompt:   "need input and approval",
		DeviceID: "mac-mini",
		Delivery: state.TaskDeliverySilent,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Wait for PendingQuestion bound to taskId & deviceId and task status=waiting_question.
	var q PendingQuestion
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if qs := h.ListQuestions("ops"); len(qs) == 1 {
			q = qs[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if q.ID == "" || q.TaskID != tk.ID || q.DeviceID != "mac-mini" {
		t.Fatalf("expected question bound to taskId=%s deviceId=mac-mini, got %+v", tk.ID, q)
	}
	if cur, _ := h.GetTask(tk.ID); cur.Status != state.TaskStatusWaitingQuestion {
		t.Fatalf("expected waiting_question status, got %q", cur.Status)
	}
	if _, err := h.AnswerQuestion(q.ID, map[string]any{"env": "prod"}, false, ""); err != nil {
		t.Fatal(err)
	}

	// 2. Wait for PendingApproval bound to taskId & deviceId and task status=waiting_approval.
	var apr PendingApproval
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if aprs := h.ListApprovals("ops"); len(aprs) == 1 {
			apr = aprs[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if apr.ID == "" || apr.TaskID != tk.ID || apr.DeviceID != "mac-mini" {
		t.Fatalf("expected approval bound to taskId=%s deviceId=mac-mini, got %+v", tk.ID, apr)
	}
	if cur, _ := h.GetTask(tk.ID); cur.Status != state.TaskStatusWaitingApproval {
		t.Fatalf("expected waiting_approval status, got %q", cur.Status)
	}

	// Coordinator approves; second (human) answer loses race with ErrApprovalNotFound.
	if _, err := h.ReplyApprovalAsCoordinator("ops", apr.ID, "allow_once", "", ""); err != nil {
		t.Fatalf("ReplyApprovalAsCoordinator failed: %v", err)
	}
	if _, err := h.ReplyApproval(apr.ID, "reject", "", "too late"); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("expected ErrApprovalNotFound for human losing race, got %v", err)
	}

	done, err := h.WaitTask(context.Background(), tk.ID)
	if err != nil || done.Status != state.TaskStatusCompleted || done.Output != "prod:allow_once" {
		t.Fatalf("unexpected task completion: %+v err=%v", done, err)
	}
}

func TestBotTaskDesktopUnificationAndTakeover(t *testing.T) {
	var seenTaskID, seenDeviceID string
	var hRef *Hub
	th := &taskTestHarness{
		kind: "deskuni",
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			seenTaskID = opts.ExtraEnv["PEBBLE_TASK_ID"]
			seenDeviceID = opts.ExtraEnv["PEBBLE_DEVICE_ID"]
			if opts.OnText != nil {
				opts.OnText("observing screen")
			}
			if opts.OnToolUse != nil {
				opts.OnToolUse("node_desktop")
			}
			// Simulate takeover bound to taskId.
			hRef.NotifyTaskTakeoverStarted(seenTaskID, "local")
			tk, _ := hRef.GetTask(seenTaskID)
			if tk == nil || tk.Status != state.TaskStatusWaitingTakeover {
				return nil, fmt.Errorf("expected waiting_takeover, got %+v", tk)
			}
			hRef.NotifyTaskTakeoverEnded(seenTaskID)
			tkAfter, _ := hRef.GetTask(seenTaskID)
			if tkAfter == nil || tkAfter.Status != state.TaskStatusRunning {
				return nil, fmt.Errorf("expected running after takeover end, got %+v", tkAfter)
			}
			return &harness.TurnResult{
				BotID:     opts.Bot.ID,
				SessionID: "desk_uni_1",
				Text:      "desktop goal done",
			}, nil
		},
	}

	h, _ := newTaskTestHub(t, th, func(cfg *config.HubConfig) {
		cfg.DesktopHarness = "deskuni"
	})
	hRef = h

	res, err := h.RunDesktopTask(context.Background(), DesktopTaskRequest{
		BotID:  "ops",
		Target: "local",
		Goal:   "click login",
		App:    "Safari",
	})
	if err != nil || res.Text != "desktop goal done" {
		t.Fatalf("RunDesktopTask failed: res=%+v err=%v", res, err)
	}
	if seenTaskID == "" || seenDeviceID != "local" {
		t.Fatalf("expected PEBBLE_TASK_ID and PEBBLE_DEVICE_ID=local, got task=%q dev=%q", seenTaskID, seenDeviceID)
	}

	tasks, err := h.ListTasks("ops", state.TaskListFilter{Kind: state.TaskKindDesktop})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected 1 desktop task persisted, got %+v err=%v", tasks, err)
	}
	if tasks[0].ID != seenTaskID || tasks[0].Status != state.TaskStatusCompleted || tasks[0].DeviceID != "local" {
		t.Fatalf("unexpected desktop task record: %+v", tasks[0])
	}
	events, err := h.ListTaskEvents(seenTaskID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var sawChunk, sawTool, sawTakeover bool
	for _, ev := range events {
		switch ev.EventType {
		case state.TaskEventChunk:
			sawChunk = true
		case state.TaskEventToolUse:
			sawTool = true
		case state.TaskEventTakeover:
			sawTakeover = true
		}
	}
	if !sawChunk || !sawTool || !sawTakeover {
		t.Fatalf("expected chunk, tool_use, and takeover events, got %+v", events)
	}
}
