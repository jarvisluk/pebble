package hub

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/node"
	"bot-hub/internal/state"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func startTestNodeWSServer(t *testing.T, h *Hub, onWS func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/nodes/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		if onWS != nil {
			onWS(conn)
		}
		h.Nodes().ServeWS(r.Context(), conn)
	})
	mux.HandleFunc("POST /api/nodes/disconnect", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func waitForHubCondition(t *testing.T, timeout time.Duration, fn func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

func TestRemoteBotTaskLifecycleApprovalsFollowUpAndCancel(t *testing.T) {
	mainWakeCh := make(chan string, 4)
	localHarness := &taskTestHarness{
		kind: "remotefake",
		fn: func(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			mainWakeCh <- opts.Prompt
			return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "main_s1", Text: "woke"}, nil
		},
	}

	h, _ := newTaskTestHub(t, localHarness, func(cfg *config.HubConfig) {
		cfg.HTTP = config.HTTPConfig{Host: "127.0.0.1", Port: 3799}
		cfg.Bots[0].PermissionMode = config.PermissionModeAuto
	})

	// Seed bot role AGENTS.md and _shared/USER.md to verify ContextSummary forwarding.
	if err := os.MkdirAll(h.cfg.Bots[0].Workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.cfg.Bots[0].Workspace, "AGENTS.md"), []byte("Role: Ops specialist for remote builds"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.cfg.SharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.cfg.SharedDir, "USER.md"), []byte("Prefer concise outputs"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Subscribe Hub SSE stream to verify task_chunk / task_tool_use / task_updated / task_completed events.
	subID, subCh := h.Subscribe()
	defer h.Unsubscribe(subID)

	// 1. Validation: unknown / offline / no-PermAgent nodes must fail SpawnTask.
	if _, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:    "ops",
		Prompt:   "hi",
		DeviceID: "missing-node",
	}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing node error, got %v", err)
	}

	srv := startTestNodeWSServer(t, h, nil)

	noAgentNode := node.NewAgent(node.AgentOptions{
		NodeID:      "files-only",
		BaseURL:     srv.URL,
		Permissions: []string{node.PermFiles},
		Enabled:     true,
	})
	defer noAgentNode.Close()
	waitForHubCondition(t, 5*time.Second, func() bool {
		info, ok := h.Nodes().Get("files-only")
		return ok && info.Connected
	}, "files-only connected")

	if _, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:    "ops",
		Prompt:   "hi",
		DeviceID: "files-only",
	}); err == nil || !strings.Contains(err.Error(), node.PermLabel(node.PermAgent)) {
		t.Fatalf("expected PermAgent validation error, got %v", err)
	}

	// 2. Connect full WS agent node with authorized workspace and MaxPermissionMode="ask".
	remoteRepo := t.TempDir()
	remoteSub := filepath.Join(remoteRepo, "sub")
	if err := os.MkdirAll(remoteSub, 0o755); err != nil {
		t.Fatal(err)
	}

	var (
		mu              sync.Mutex
		seenOpts        []harness.RunOptions
		turnCount       atomic.Int32
		blockCancelTurn = make(chan struct{})
		cancelStarted   = make(chan struct{})
	)
	remoteFake := &taskTestHarness{
		kind: "remotefake",
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			n := turnCount.Add(1)
			mu.Lock()
			seenOpts = append(seenOpts, opts)
			mu.Unlock()

			if strings.Contains(opts.Prompt, "hang-remote-cancel") {
				close(cancelStarted)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			if n == 1 {
				if opts.OnText != nil {
					opts.OnText("remote-chunk-1 ")
				}
				if opts.OnToolUse != nil {
					opts.OnToolUse("read_file")
				}
				qResp, err := opts.OnQuestion(ctx, harness.QuestionRequest{
					ID:     "rq1",
					Title:  "Pick target",
					Fields: []harness.QuestionField{{Key: "target", Title: "Target", Type: "string"}},
				})
				if err != nil {
					return nil, err
				}
				pResp, err := opts.OnPermission(ctx, harness.PermissionRequest{
					ID:       "rp1",
					ToolName: "bash",
					Kind:     "shell",
					Summary:  "make test",
				})
				if err != nil {
					return nil, err
				}
				return &harness.TurnResult{
					BotID:     opts.Bot.ID,
					SessionID: "remote-rt-1",
					Text:      fmt.Sprintf("done:%v:%s", qResp.Answers["target"], pResp.Decision),
					Usage:     harness.Usage{CostUSD: 0.02, InputTokens: 20, OutputTokens: 10},
				}, nil
			}
			return &harness.TurnResult{
				BotID:     opts.Bot.ID,
				SessionID: opts.SessionID,
				Text:      "followup:" + opts.Prompt,
			}, nil
		},
	}
	remoteReg := harness.NewRegistry()
	remoteReg.Register(remoteFake)

	agent := node.NewAgent(node.AgentOptions{
		NodeID:            "mac-studio",
		Name:              "Mac Studio",
		BaseURL:           srv.URL,
		Token:             "node-secret",
		Permissions:       []string{node.PermAgent, node.PermFiles},
		Enabled:           true,
		MaxPermissionMode: config.PermissionModeAsk,
		Workspaces: []node.NodeWorkspace{
			{ID: "repo", Name: "Repo", Path: remoteRepo},
		},
		HarnessRegistry: remoteReg,
	})
	defer agent.Close()

	waitForHubCondition(t, 5*time.Second, func() bool {
		info, ok := h.Nodes().Get("mac-studio")
		return ok && info.Connected && info.Transport == "ws"
	}, "mac-studio ws connected")

	// 3. Spawn remote task with WorkspaceID + Delivery="wake".
	tk, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:       "ops",
		Title:       "Remote Build",
		Prompt:      "run remote suite",
		DeviceID:    "mac-studio",
		WorkspaceID: "repo",
		Workdir:     "sub",
		Delivery:    state.TaskDeliveryWake,
	})
	if err != nil {
		t.Fatalf("SpawnTask remote failed: %v", err)
	}

	// Answer bridged question.
	var pq PendingQuestion
	waitForHubCondition(t, 5*time.Second, func() bool {
		qs := h.ListQuestions("ops")
		if len(qs) == 1 {
			pq = qs[0]
			return true
		}
		return false
	}, "remote pending question")
	if pq.TaskID != tk.ID || pq.DeviceID != "mac-studio" {
		t.Fatalf("unexpected question binding: %+v", pq)
	}
	if cur, _ := h.GetTask(tk.ID); cur.Status != state.TaskStatusWaitingQuestion {
		t.Fatalf("expected waiting_question, got %q", cur.Status)
	}
	if _, err := h.AnswerQuestion(pq.ID, map[string]any{"target": "staging"}, false, ""); err != nil {
		t.Fatal(err)
	}

	// Approve bridged permission.
	var pa PendingApproval
	waitForHubCondition(t, 5*time.Second, func() bool {
		aprs := h.ListApprovals("ops")
		if len(aprs) == 1 {
			pa = aprs[0]
			return true
		}
		return false
	}, "remote pending approval")
	if pa.TaskID != tk.ID || pa.DeviceID != "mac-studio" {
		t.Fatalf("unexpected approval binding: %+v", pa)
	}
	if cur, _ := h.GetTask(tk.ID); cur.Status != state.TaskStatusWaitingApproval {
		t.Fatalf("expected waiting_approval, got %q", cur.Status)
	}
	if _, err := h.ReplyApproval(pa.ID, "allow_once", "", ""); err != nil {
		t.Fatal(err)
	}

	doneTk, err := h.WaitTask(context.Background(), tk.ID)
	if err != nil || doneTk.Status != state.TaskStatusCompleted {
		t.Fatalf("remote task failed: %+v err=%v", doneTk, err)
	}
	if doneTk.Output != "done:staging:allow_once" || doneTk.RuntimeSessionID != "remote-rt-1" {
		t.Fatalf("unexpected remote task output/session: %+v", doneTk)
	}

	// Verify ContextSummary and MCPHubURL were sent to remote node.
	mu.Lock()
	firstOpts := seenOpts[0]
	mu.Unlock()
	if !strings.Contains(firstOpts.Prompt, "Ops specialist for remote builds") || !strings.Contains(firstOpts.Prompt, "Prefer concise outputs") {
		t.Fatalf("expected ContextSummary in remote prompt, got:\n%s", firstOpts.Prompt)
	}
	if firstOpts.ExtraEnv["PEBBLE_URL"] != "http://127.0.0.1:3799" {
		t.Fatalf("expected MCPHubURL propagated to remote PEBBLE_URL, got %v", firstOpts.ExtraEnv)
	}

	// Verify wake delivery fired.
	select {
	case wakePrompt := <-mainWakeCh:
		if !strings.Contains(wakePrompt, "done:staging:allow_once") {
			t.Fatalf("unexpected wake prompt: %q", wakePrompt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected wake delivery after remote task completion")
	}

	// Verify bot_task_events and SSE broadcast recorded chunk, tool_use, status, completed.
	evts, err := h.ListTaskEvents(tk.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var sawChunkEvt, sawToolEvt bool
	for _, ev := range evts {
		if ev.EventType == state.TaskEventChunk {
			sawChunkEvt = true
		}
		if ev.EventType == state.TaskEventToolUse {
			sawToolEvt = true
		}
	}
	if !sawChunkEvt || !sawToolEvt {
		t.Fatalf("expected chunk and tool_use in bot_task_events, got %+v", evts)
	}
	var sawSSEChunk, sawSSETool, sawSSECompleted bool
drainSSE:
	for {
		select {
		case ev := <-subCh:
			switch ev.Type {
			case "task_chunk":
				sawSSEChunk = true
			case "task_tool_use":
				sawSSETool = true
			case "task_completed":
				sawSSECompleted = true
			}
		default:
			break drainSSE
		}
	}
	if !sawSSEChunk || !sawSSETool || !sawSSECompleted {
		t.Fatalf("expected task_chunk, task_tool_use, task_completed SSE events (got chunk=%v tool=%v done=%v)", sawSSEChunk, sawSSETool, sawSSECompleted)
	}

	// 4. Follow-up SendToTask preserves RuntimeSessionID via SendAgentTask.
	if _, err := h.SendToTask(context.Background(), tk.ID, "second step"); err != nil {
		t.Fatalf("SendToTask remote failed: %v", err)
	}
	waitForHubCondition(t, 5*time.Second, func() bool {
		cur, err := h.GetTask(tk.ID)
		return err == nil && cur.Status == state.TaskStatusCompleted && cur.Output == "followup:second step"
	}, "remote follow-up completion")
	mu.Lock()
	if len(seenOpts) < 2 || seenOpts[1].SessionID != "remote-rt-1" {
		mu.Unlock()
		t.Fatalf("expected follow-up SessionID remote-rt-1, got %+v", seenOpts)
	}
	mu.Unlock()

	// 5. CancelTask cancels active remote task.
	_ = blockCancelTurn
	cTask, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:    "ops",
		Prompt:   "hang-remote-cancel",
		DeviceID: "mac-studio",
		Delivery: state.TaskDeliverySilent,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-cancelStarted
	cancelled, err := h.CancelTask(cTask.ID)
	if err != nil || cancelled.Status != state.TaskStatusCancelled {
		t.Fatalf("CancelTask remote failed: %+v err=%v", cancelled, err)
	}
}

func TestRemoteBotTaskOfflineSettlementAndReconnect(t *testing.T) {
	wakeCh := make(chan string, 4)
	localHarness := &taskTestHarness{
		kind: "offlinefake",
		fn: func(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			wakeCh <- opts.Prompt
			return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "m1", Text: "ack"}, nil
		},
	}

	h, store := newTaskTestHub(t, localHarness, nil)
	h.Nodes().SetDisconnectGracePeriod(150 * time.Millisecond)

	var (
		connMu sync.Mutex
		lastWS *websocket.Conn
	)
	srv := startTestNodeWSServer(t, h, func(c *websocket.Conn) {
		connMu.Lock()
		lastWS = c
		connMu.Unlock()
	})

	startedTurn := make(chan struct{})
	remoteReg := harness.NewRegistry()
	remoteReg.Register(&taskTestHarness{
		kind: "offlinefake",
		fn: func(ctx context.Context, _ harness.RunOptions) (*harness.TurnResult, error) {
			close(startedTurn)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	agent := node.NewAgent(node.AgentOptions{
		NodeID:          "flaky-mac",
		BaseURL:         srv.URL,
		Permissions:     []string{node.PermAgent},
		Enabled:         true,
		HarnessRegistry: remoteReg,
	})
	defer agent.Close()

	waitForHubCondition(t, 5*time.Second, func() bool {
		info, ok := h.Nodes().Get("flaky-mac")
		return ok && info.Connected
	}, "flaky-mac connected")

	tk, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:    "ops",
		Title:    "Flaky Job",
		Prompt:   "wait",
		DeviceID: "flaky-mac",
		Delivery: state.TaskDeliveryWake,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-startedTurn

	// Abruptly close server-side WS and stop agent so grace expires -> OnNodeOffline settles task + triggers wake!
	agent.Close()
	connMu.Lock()
	ws := lastWS
	connMu.Unlock()
	if ws != nil {
		_ = ws.Close(websocket.StatusNormalClosure, "drop")
	}

	doneTk, err := h.WaitTask(context.Background(), tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if doneTk.Status != state.TaskStatusFailed || !strings.Contains(doneTk.Error, "remote device offline") {
		t.Fatalf("expected failed remote device offline, got %+v", doneTk)
	}

	select {
	case msg := <-wakeCh:
		if !strings.Contains(msg, "remote device offline") {
			t.Fatalf("expected wake prompt with offline error, got %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected wake trigger when remote device went offline")
	}

	// Verify OnNodeReconnected reconciles dangling DB tasks not reported in activeTaskIds.
	dangling, err := store.CreateBotTask(state.BotTask{
		BotID:    "ops",
		Kind:     state.TaskKindWorker,
		Prompt:   "orphaned-db",
		Status:   state.TaskStatusRunning,
		DeviceID: "reconn-node",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.handleRemoteNodeReconnected("reconn-node", node.NodeInfo{ID: "reconn-node", Name: "Reconn"}, nil)
	reconciled, err := h.GetTask(dangling.ID)
	if err != nil || reconciled.Status != state.TaskStatusFailed || !strings.Contains(reconciled.Error, "lost after node reconnect") {
		t.Fatalf("expected reconciled failed task, got %+v err=%v", reconciled, err)
	}
}

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	ctx := context.Background()
	if _, err := runGit(ctx, repo, nil, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, repo, nil, "config", "user.name", "Test User"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, repo, nil, "config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, repo, nil, "add", "README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, repo, nil, "commit", "--no-verify", "-m", "chore: initial commit"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestBotTaskWorktreeIsolationCheckpointDiffAndRollback(t *testing.T) {
	repo := initTestGitRepo(t)
	origHead, err := runGit(context.Background(), repo, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	th := &taskTestHarness{
		kind: "wtfake",
		fn: func(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			if opts.Prompt == "temp-dir-job" {
				if err := os.WriteFile(filepath.Join(opts.Bot.Workspace, "scratch.txt"), []byte("tmp\n"), 0o644); err != nil {
					return nil, err
				}
				return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "s-tmp", Text: "temp-ok"}, nil
			}
			// Modify tracked file and add untracked file inside isolated worktree.
			if err := os.WriteFile(filepath.Join(opts.Bot.Workspace, "README.md"), []byte("hello\nmodified in worktree\n"), 0o644); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(opts.Bot.Workspace, "new_file.txt"), []byte("brand new\n"), 0o644); err != nil {
				return nil, err
			}
			return &harness.TurnResult{
				BotID:     opts.Bot.ID,
				SessionID: "s-wt",
				Text:      "wt-done:" + opts.Bot.Workspace,
			}, nil
		},
	}

	h, _ := newTaskTestHub(t, th, func(cfg *config.HubConfig) {
		cfg.Bots[0].Workspace = repo
	})

	// 1. Worktree isolation + hidden checkpoint + diff + in-place rollback.
	tk, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:     "ops",
		Prompt:    "edit in worktree",
		Isolation: state.TaskIsolationWorktree,
		Delivery:  state.TaskDeliverySilent,
	})
	if err != nil {
		t.Fatalf("SpawnTask worktree failed: %v", err)
	}
	wantBranch := "pebble/task-" + tk.ID
	if tk.WorktreeBranch != wantBranch || !strings.Contains(tk.WorktreePath, filepath.Join("worktrees", tk.ID)) {
		t.Fatalf("unexpected worktree metadata: %+v", tk)
	}

	doneTk, err := h.WaitTask(context.Background(), tk.ID)
	if err != nil || doneTk.Status != state.TaskStatusCompleted {
		t.Fatalf("worktree task failed: %+v err=%v", doneTk, err)
	}
	if doneTk.CheckpointRef != "refs/pebble/checkpoints/"+tk.ID {
		t.Fatalf("expected CheckpointRef, got %+v", doneTk)
	}

	// Original repo must remain clean and on original HEAD!
	curHead, _ := runGit(context.Background(), repo, nil, "rev-parse", "HEAD")
	origStatus, _ := runGit(context.Background(), repo, nil, "status", "--porcelain")
	if curHead != origHead || origStatus != "" {
		t.Fatalf("expected original repo untouched (head=%s vs %s, status=%q)", curHead, origHead, origStatus)
	}

	// Worktree branch HEAD must stay untouched (checkpoint did not commit onto user branch),
	// and worktree working tree still has uncommitted changes!
	wtHead, _ := runGit(context.Background(), doneTk.WorktreePath, nil, "rev-parse", "HEAD")
	wtStatus, _ := runGit(context.Background(), doneTk.WorktreePath, nil, "status", "--porcelain")
	if wtHead != origHead || !strings.Contains(wtStatus, "README.md") || !strings.Contains(wtStatus, "new_file.txt") {
		t.Fatalf("expected worktree index/HEAD untouched with tracked+untracked changes, head=%s status=%q", wtHead, wtStatus)
	}

	// GetTaskDiff must show both tracked modification and untracked file.
	diff, err := h.GetTaskDiff(context.Background(), tk.ID)
	if err != nil {
		t.Fatalf("GetTaskDiff failed: %v", err)
	}
	if diff.Branch != wantBranch || diff.CheckpointRef != doneTk.CheckpointRef || len(diff.Files) != 2 {
		t.Fatalf("unexpected TaskDiffResult: %+v", diff)
	}
	if !strings.Contains(diff.Patch, "modified in worktree") || !strings.Contains(diff.Patch, "brand new") {
		t.Fatalf("expected patch to include tracked & untracked changes:\n%s", diff.Patch)
	}

	// Rollback without removing worktree restores clean state.
	rb1, err := h.RollbackTask(context.Background(), tk.ID, false)
	if err != nil || !rb1.RolledBack || rb1.RemovedWorktree {
		t.Fatalf("RollbackTask(removeWorktree=false) failed: %+v err=%v", rb1, err)
	}
	wtStatusAfter, _ := runGit(context.Background(), doneTk.WorktreePath, nil, "status", "--porcelain")
	if wtStatusAfter != "" {
		t.Fatalf("expected clean worktree after rollback, got %q", wtStatusAfter)
	}

	// 2. Rollback with removeWorktree=true removes worktree directory and branch.
	tk2, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:     "ops",
		Prompt:    "edit again",
		Isolation: state.TaskIsolationWorktree,
		Delivery:  state.TaskDeliverySilent,
	})
	if err != nil {
		t.Fatal(err)
	}
	done2, err := h.WaitTask(context.Background(), tk2.ID)
	if err != nil || done2.Status != state.TaskStatusCompleted {
		t.Fatalf("second worktree task failed: %+v err=%v", done2, err)
	}
	rb2, err := h.RollbackTask(context.Background(), tk2.ID, true)
	if err != nil || !rb2.RemovedWorktree {
		t.Fatalf("RollbackTask(removeWorktree=true) failed: %+v err=%v", rb2, err)
	}
	if _, statErr := os.Stat(done2.WorktreePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected worktree dir removed: %s", done2.WorktreePath)
	}
	if _, bErr := runGit(context.Background(), repo, nil, "rev-parse", "--verify", done2.WorktreeBranch); bErr == nil {
		t.Fatalf("expected worktree branch %s removed", done2.WorktreeBranch)
	}

	// 3. TempDir isolation creates isolated directory under <stateDir>/task-dirs/<taskId> and cleans up.
	tkTmp, err := h.SpawnTask(context.Background(), TaskSpawnRequest{
		BotID:     "ops",
		Prompt:    "temp-dir-job",
		Isolation: state.TaskIsolationTempDir,
		Delivery:  state.TaskDeliverySilent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tkTmp.WorktreePath, filepath.Join("task-dirs", tkTmp.ID)) {
		t.Fatalf("expected task-dirs path, got %q", tkTmp.WorktreePath)
	}
	if _, err := h.WaitTask(context.Background(), tkTmp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tkTmp.WorktreePath, "scratch.txt")); err != nil {
		t.Fatalf("expected scratch.txt inside temp_dir: %v", err)
	}
	if _, err := h.RollbackTask(context.Background(), tkTmp.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tkTmp.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected temp_dir removed after rollback")
	}

	// Verify errors.Is works cleanly with ErrTaskNotFound.
	if _, err := h.GetTaskDiff(context.Background(), "nonexistent"); !errors.Is(err, state.ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}
