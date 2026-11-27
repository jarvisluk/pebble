package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/protocol"
)

func TestRegistryAndAgentEndToEnd(t *testing.T) {
	var changes atomic.Int32
	reg := NewRegistry(func() { changes.Add(1) })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/nodes/poll", func(w http.ResponseWriter, r *http.Request) {
		var req PollRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cmd, err := reg.Poll(r.Context(), req, 5*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if cmd == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(cmd)
	})
	mux.HandleFunc("POST /api/nodes/result", func(w http.ResponseWriter, r *http.Request) {
		var res Result
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reg.Deliver(res)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/nodes/disconnect", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reg.Disconnect(req.ID)
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	agent := NewAgent(AgentOptions{
		NodeID:      "macbook",
		Name:        "MacBook Pro",
		BaseURL:     srv.URL,
		Permissions: []string{PermFiles, PermExec},
		Enabled:     true,
	})
	defer agent.Close()

	// Wait until the node shows up as connected.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list := reg.List()
		if len(list) == 1 && list[0].Connected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	list := reg.List()
	if len(list) != 1 || !list[0].Connected {
		t.Fatalf("expected 1 connected node, got %+v", list)
	}
	if list[0].ProtocolVersion != protocol.CurrentProtocol || !list[0].HasCapability(protocol.CapNodeExecEnv) || !list[0].HasCapability(protocol.CapNodeDesktopPermissions) {
		t.Fatalf("expected current protocol & capabilities on node, got %+v", list[0])
	}
	for _, act := range []string{"desktop_permissions", "desktop_permission_request"} {
		if perm, err := RequiredPerm(act); err != nil || perm != PermDesktop {
			t.Fatalf("RequiredPerm(%q) = (%q, %v), want (%q, nil)", act, perm, err, PermDesktop)
		}
	}

	// Auto-select single connected node when nodeID is "".
	res, resolvedID, err := reg.Call(context.Background(), "", "exec", map[string]string{"command": "echo hello-node"}, 10*time.Second)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if resolvedID != "macbook" || !res.OK || !strings.Contains(res.Output, "hello-node") {
		t.Fatalf("unexpected exec result: resolved=%q res=%+v", resolvedID, res)
	}

	// File write, read, and list inside TempDir.
	dir := t.TempDir()
	fp := filepath.Join(dir, "sub", "note.txt")
	res, _, err = reg.Call(context.Background(), "", "file_write", map[string]any{"path": fp, "content": "alpha"}, 10*time.Second)
	if err != nil || !res.OK {
		t.Fatalf("file_write failed: err=%v res=%+v", err, res)
	}
	res, _, err = reg.Call(context.Background(), "", "file_read", map[string]any{"path": fp}, 10*time.Second)
	if err != nil || !res.OK || res.Output != "alpha" {
		t.Fatalf("file_read failed: err=%v res=%+v", err, res)
	}
	res, _, err = reg.Call(context.Background(), "", "file_list", map[string]any{"path": filepath.Dir(fp)}, 10*time.Second)
	if err != nil || !res.OK || !strings.Contains(res.Output, "note.txt") {
		t.Fatalf("file_list failed: err=%v res=%+v", err, res)
	}

	// Update permissions live: remove PermExec, keep PermFiles.
	agent.Configure(true, "MacBook Pro", []string{PermFiles})
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list = reg.List()
		if len(list) == 1 && len(list[0].Permissions) == 1 && list[0].Permissions[0] == PermFiles {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, _, err := reg.Call(context.Background(), "macbook", "exec", map[string]string{"command": "pwd"}, 5*time.Second); err == nil {
		t.Fatalf("expected permission denial after removing PermExec")
	}

	// Disable node and confirm it immediately disconnects on the hub.
	agent.Configure(false, "", nil)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		list = reg.List()
		if len(list) == 1 && !list[0].Connected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if list[0].Connected {
		t.Fatalf("expected node disconnected after Configure(false)")
	}
	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("written file missing: %v", err)
	}
	if changes.Load() == 0 {
		t.Fatalf("expected onChange callback to fire")
	}
}

type fakeRemoteHarness struct {
	kind string
	caps harness.PermissionCapabilities
	fn   func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error)

	mu       sync.Mutex
	lastOpts harness.RunOptions
}

func (f *fakeRemoteHarness) Kind() string  { return f.kind }
func (f *fakeRemoteHarness) Label() string { return "Fake " + f.kind }
func (f *fakeRemoteHarness) PermissionCapabilities() harness.PermissionCapabilities {
	return f.caps
}
func (f *fakeRemoteHarness) Models(context.Context) ([]string, error) { return []string{"fake-1"}, nil }
func (f *fakeRemoteHarness) Close()                                   {}
func (f *fakeRemoteHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	f.mu.Lock()
	f.lastOpts = opts
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(ctx, opts)
	}
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "sess-default", Text: "ok"}, nil
}

func newWSTestServer(t *testing.T, reg *Registry) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/nodes/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		reg.ServeWS(r.Context(), conn)
	})
	mux.HandleFunc("POST /api/nodes/disconnect", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reg.Disconnect(req.ID)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func waitForNodeCondition(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func TestIntersectPermissionMode(t *testing.T) {
	cases := []struct {
		req  string
		ceil string
		want string
	}{
		{"auto", "auto", "auto"},
		{"auto", "edit", "edit"},
		{"auto", "ask", "ask"},
		{"auto", "plan", "plan"},
		{"edit", "ask", "ask"},
		{"ask", "edit", "ask"},
		{"plan", "auto", "plan"},
		{"edit", "plan", "plan"},
		{"", "ask", "ask"},
		{"auto", "", "auto"},
		{"auto", "invalid", "plan"},
	}
	for _, tc := range cases {
		if got := IntersectPermissionMode(tc.req, tc.ceil); got != tc.want {
			t.Errorf("IntersectPermissionMode(%q, %q) = %q, want %q", tc.req, tc.ceil, got, tc.want)
		}
	}
}

func TestWSHandshakeStreamingApprovalsAndCancel(t *testing.T) {
	reg := NewRegistry(nil)
	srv := newWSTestServer(t, reg)

	wsRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wsRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(wsRoot, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var turnCount atomic.Int32
	var secondTurnSession string
	fake := &fakeRemoteHarness{
		kind: "opencode",
		caps: harness.PermissionCapabilities{
			Modes:               []string{config.PermissionModeAuto, config.PermissionModeEdit, config.PermissionModeAsk, config.PermissionModePlan},
			InteractiveApproval: true,
			SupportsAllowAlways: true,
		},
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			n := turnCount.Add(1)
			if n == 1 {
				if opts.OnText != nil {
					opts.OnText("hello ")
				}
				if opts.OnToolUse != nil {
					opts.OnToolUse("read_file")
				}
				permResp, err := opts.OnPermission(ctx, harness.PermissionRequest{
					ID:       "p1",
					ToolName: "bash",
					Kind:     "shell",
					Summary:  "run echo",
				})
				if err != nil {
					return nil, err
				}
				qResp, err := opts.OnQuestion(ctx, harness.QuestionRequest{
					ID:    "q1",
					Title: "Pick one",
					Fields: []harness.QuestionField{{
						Key:   "env",
						Title: "Env",
						Type:  "text",
					}},
				})
				if err != nil {
					return nil, err
				}
				if opts.OnText != nil {
					opts.OnText(permResp.Decision + ":" + qResp.Answers["env"].(string))
				}
				return &harness.TurnResult{
					BotID:     opts.Bot.ID,
					SessionID: "remote-sess-1",
					Text:      "hello " + permResp.Decision + ":" + qResp.Answers["env"].(string),
					Tools:     []string{"read_file"},
				}, nil
			}
			if n == 2 {
				secondTurnSession = opts.SessionID
				return &harness.TurnResult{
					BotID:     opts.Bot.ID,
					SessionID: opts.SessionID,
					Text:      "follow-up:" + opts.Prompt,
				}, nil
			}
			// Third turn blocks until cancelled.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	hreg := harness.NewRegistry()
	hreg.Register(fake)

	agent := NewAgent(AgentOptions{
		NodeID:            "studio-1",
		Name:              "Mac Studio",
		BaseURL:           srv.URL,
		Token:             "node-tok",
		Permissions:       []string{PermFiles, PermExec, PermAgent},
		Enabled:           true,
		Workspaces:        []NodeWorkspace{{ID: "repo", Name: "Repo", Path: wsRoot}},
		MaxPermissionMode: config.PermissionModeAsk,
		HarnessRegistry:   hreg,
	})
	defer agent.Close()

	waitForNodeCondition(t, 5*time.Second, func() bool {
		info, ok := reg.Get("studio-1")
		return ok && info.Connected && info.Transport == "ws"
	}, "ws node registration")

	info, _ := reg.Get("studio-1")
	if !info.HasCapability(protocol.CapNodeWS) || !info.HasCapability(protocol.CapNodeAgentExec) || !info.HasCapability(protocol.CapNodeWorkspaces) {
		t.Fatalf("missing expected WS/agent/workspace capabilities: %+v", info.Capabilities)
	}
	if info.MaxPermissionMode != config.PermissionModeAsk {
		t.Fatalf("MaxPermissionMode = %q, want ask", info.MaxPermissionMode)
	}
	if len(info.Harnesses) != 1 || info.Harnesses[0].Kind != "opencode" || !info.Harnesses[0].Installed {
		t.Fatalf("unexpected advertised harnesses: %+v", info.Harnesses)
	}
	if len(info.Workspaces) != 1 || info.Workspaces[0].ID != "repo" || !info.Workspaces[0].IsGit {
		t.Fatalf("unexpected advertised workspaces: %+v", info.Workspaces)
	}

	// Short RPC over WS still works.
	rpcRes, _, err := reg.Call(context.Background(), "studio-1", "exec", map[string]string{"command": "echo ws-rpc"}, 5*time.Second)
	if err != nil || !rpcRes.OK || !strings.Contains(rpcRes.Output, "ws-rpc") {
		t.Fatalf("WS rpc_call failed: err=%v res=%+v", err, rpcRes)
	}

	// Streamed turn + Permission ceiling (auto -> ask) + Approval/Question round-trip + MCP env wiring.
	var (
		evMu   sync.Mutex
		events []AgentEvent
		doneCh = make(chan AgentEvent, 1)
	)
	err = reg.StartAgentTask(context.Background(), "studio-1", AgentStartRequest{
		TaskID:         "task-1",
		BotID:          "coder",
		BotName:        "Coder",
		Harness:        "opencode",
		PermissionMode: config.PermissionModeAuto, // must be clamped to "ask"
		WorkspaceID:    "repo",
		Workdir:        "sub",
		Prompt:         "inspect repo",
		ContextSummary: "Parent summary",
		ContextFiles:   []AgentContextFile{{Path: "notes/ctx.md", Content: "context body"}},
		MCPHubURL:      srv.URL,
	}, AgentTaskCallbacks{
		OnEvent: func(evt AgentEvent) {
			evMu.Lock()
			events = append(events, evt)
			evMu.Unlock()
			if evt.Type == AgentEventDone || evt.Type == AgentEventError {
				select {
				case doneCh <- evt:
				default:
				}
			}
		},
		OnApproval: func(_ context.Context, req AgentApprovalRequest) (harness.PermissionResponse, error) {
			if req.TaskID != "task-1" || req.Request.ToolName != "bash" {
				t.Errorf("unexpected approval req: %+v", req)
			}
			return harness.PermissionResponse{Decision: "allow_once", OptionID: "allow_once"}, nil
		},
		OnQuestion: func(_ context.Context, req AgentQuestionRequest) (harness.QuestionResponse, error) {
			if req.TaskID != "task-1" || req.Request.ID != "q1" {
				t.Errorf("unexpected question req: %+v", req)
			}
			return harness.QuestionResponse{Answers: map[string]any{"env": "prod"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartAgentTask failed: %v", err)
	}

	select {
	case doneEvt := <-doneCh:
		if doneEvt.Type != AgentEventDone || doneEvt.Text != "hello allow_once:prod" || doneEvt.SessionID != "remote-sess-1" {
			t.Fatalf("unexpected done event: %+v", doneEvt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent done event")
	}

	fake.mu.Lock()
	lastOpts := fake.lastOpts
	fake.mu.Unlock()
	if lastOpts.PermissionMode != config.PermissionModeAsk || lastOpts.Bot.PermissionMode != config.PermissionModeAsk {
		t.Fatalf("expected downward-clamped permissionMode ask, got opts=%q bot=%q", lastOpts.PermissionMode, lastOpts.Bot.PermissionMode)
	}
	if lastOpts.Bot.Workspace != subDir {
		t.Fatalf("expected resolved workdir %q, got %q", subDir, lastOpts.Bot.Workspace)
	}
	if lastOpts.ExtraEnv["PEBBLE_TASK_ID"] != "task-1" || lastOpts.ExtraEnv["PEBBLE_API_TOKEN"] != "node-tok" ||
		lastOpts.MCPEnv["PEBBLE_TASK_ID"] != "task-1" || !slices.Contains(lastOpts.MCPCommand, "mcp") {
		t.Fatalf("unexpected MCP/env wiring: cmd=%v extra=%v mcp=%v", lastOpts.MCPCommand, lastOpts.ExtraEnv, lastOpts.MCPEnv)
	}
	if !strings.Contains(lastOpts.Prompt, "Parent summary") || !strings.Contains(lastOpts.Prompt, "ctx.md") {
		t.Fatalf("expected context summary/files in prompt, got:\n%s", lastOpts.Prompt)
	}

	evMu.Lock()
	var sawChunk, sawTool bool
	for _, ev := range events {
		if ev.Type == AgentEventChunk {
			sawChunk = true
		}
		if ev.Type == AgentEventToolUse && ev.ToolName == "read_file" {
			sawTool = true
		}
	}
	evMu.Unlock()
	if !sawChunk || !sawTool {
		t.Fatalf("expected chunk and tool_use events, got %+v", events)
	}

	// Path traversal outside authorized workspace must fail.
	errCh := make(chan AgentEvent, 1)
	if err := reg.StartAgentTask(context.Background(), "studio-1", AgentStartRequest{
		TaskID:      "task-escape",
		BotID:       "coder",
		Harness:     "opencode",
		WorkspaceID: "repo",
		Workdir:     "../outside",
		Prompt:      "nope",
	}, AgentTaskCallbacks{
		OnEvent: func(evt AgentEvent) {
			if evt.Type == AgentEventError {
				select {
				case errCh <- evt:
				default:
				}
			}
		},
	}); err != nil {
		t.Fatalf("StartAgentTask dispatch failed: %v", err)
	}
	select {
	case ev := <-errCh:
		if !strings.Contains(ev.Error, "escapes authorized workspace") {
			t.Fatalf("expected path traversal error, got %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for path traversal error event")
	}

	// Follow-up agent_send reuses task-1 sessionId.
	if err := reg.SendAgentTask(context.Background(), "studio-1", AgentSendRequest{
		TaskID: "task-1",
		Prompt: "continue",
	}); err != nil {
		t.Fatalf("SendAgentTask failed: %v", err)
	}
	select {
	case doneEvt := <-doneCh:
		if doneEvt.Type != AgentEventDone || doneEvt.Text != "follow-up:continue" {
			t.Fatalf("unexpected follow-up done event: %+v", doneEvt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for follow-up done event")
	}
	if secondTurnSession != "remote-sess-1" {
		t.Fatalf("expected second turn to reuse remote-sess-1, got %q", secondTurnSession)
	}

	// agent_cancel interrupts an in-flight turn cleanly.
	cancelCh := make(chan AgentEvent, 1)
	doneCh2 := make(chan AgentEvent, 1)
	if err := reg.StartAgentTask(context.Background(), "studio-1", AgentStartRequest{
		TaskID:  "task-cancel",
		BotID:   "coder",
		Harness: "opencode",
		Prompt:  "wait forever",
	}, AgentTaskCallbacks{
		OnEvent: func(evt AgentEvent) {
			if evt.Type == AgentEventError {
				select {
				case cancelCh <- evt:
				default:
				}
			}
			if evt.Type == AgentEventDone {
				select {
				case doneCh2 <- evt:
				default:
				}
			}
		},
	}); err != nil {
		t.Fatalf("StartAgentTask cancel test failed: %v", err)
	}
	waitForNodeCondition(t, 3*time.Second, func() bool {
		return turnCount.Load() >= 3
	}, "third turn to enter RunTurn")
	if err := reg.CancelAgentTask(context.Background(), "studio-1", "task-cancel", "stop now"); err != nil {
		t.Fatalf("CancelAgentTask failed: %v", err)
	}
	select {
	case ev := <-cancelCh:
		if !strings.Contains(ev.Error, "cancelled") {
			t.Fatalf("expected cancelled error, got %+v", ev)
		}
	case ev := <-doneCh2:
		t.Fatalf("expected error on cancel, got done: %+v", ev)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled event")
	}
}

func TestPermAgentDenial(t *testing.T) {
	reg := NewRegistry(nil)
	srv := newWSTestServer(t, reg)

	agent := NewAgent(AgentOptions{
		NodeID:      "no-agent",
		BaseURL:     srv.URL,
		Permissions: []string{PermFiles, PermExec}, // PermAgent omitted
		Enabled:     true,
	})
	defer agent.Close()

	waitForNodeCondition(t, 5*time.Second, func() bool {
		info, ok := reg.Get("no-agent")
		return ok && info.Connected
	}, "no-agent connected")

	err := reg.StartAgentTask(context.Background(), "no-agent", AgentStartRequest{
		TaskID: "t1",
		BotID:  "b1",
		Prompt: "hi",
	}, AgentTaskCallbacks{})
	if err == nil || !strings.Contains(err.Error(), PermLabel(PermAgent)) {
		t.Fatalf("expected PermAgent denial error, got %v", err)
	}
}

func TestWSDisconnectGracePeriodReconnectAndExpiry(t *testing.T) {
	reg := NewRegistry(nil)
	reg.SetDisconnectGracePeriod(300 * time.Millisecond)

	reconnCh := make(chan []string, 4)
	offlineCh := make(chan []string, 4)
	reg.SetLifecycleHooks(NodeLifecycleHooks{
		OnNodeReconnected: func(_ string, _ NodeInfo, activeTaskIDs []string) {
			reconnCh <- activeTaskIDs
		},
		OnNodeOffline: func(_ string, activeTaskIDs []string) {
			offlineCh <- activeTaskIDs
		},
	})

	// Hold server-side WS connections so the test can drop a connection abruptly.
	var (
		connMu    sync.Mutex
		lastWS    *websocket.Conn
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/nodes/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		connMu.Lock()
		lastWS = conn
		connMu.Unlock()
		reg.ServeWS(r.Context(), conn)
	})
	mux.HandleFunc("POST /api/nodes/disconnect", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	unblockTurn := make(chan struct{})
	fake := &fakeRemoteHarness{
		kind: "opencode",
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			select {
			case <-unblockTurn:
				return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "s-grace", Text: "resumed-ok"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	hreg := harness.NewRegistry()
	hreg.Register(fake)

	agent := NewAgent(AgentOptions{
		NodeID:          "grace-node",
		BaseURL:         srv.URL,
		Permissions:     []string{PermAgent},
		Enabled:         true,
		HarnessRegistry: hreg,
	})
	defer agent.Close()

	waitForNodeCondition(t, 5*time.Second, func() bool {
		info, ok := reg.Get("grace-node")
		return ok && info.Connected
	}, "grace-node connected")
	<-reconnCh // initial hello

	// Part 1: Drop connection while task is active -> enters grace, reconnects, and task finishes cleanly.
	doneCh := make(chan AgentEvent, 1)
	if err := reg.StartAgentTask(context.Background(), "grace-node", AgentStartRequest{
		TaskID:  "grace-task-1",
		BotID:   "b1",
		Harness: "opencode",
		Prompt:  "run",
	}, AgentTaskCallbacks{
		OnEvent: func(evt AgentEvent) {
			if evt.Type == AgentEventDone || evt.Type == AgentEventError {
				select {
				case doneCh <- evt:
				default:
				}
			}
		},
	}); err != nil {
		t.Fatal(err)
	}
	waitForNodeCondition(t, 3*time.Second, func() bool {
		return len(agent.activeTaskIDs()) == 1
	}, "task running on node")

	connMu.Lock()
	firstConn := lastWS
	connMu.Unlock()
	_ = firstConn.Close(websocket.StatusInternalError, "simulated drop")

	waitForNodeCondition(t, 2*time.Second, func() bool {
		info, ok := reg.Get("grace-node")
		return ok && info.Connected && info.Reconnecting
	}, "grace-node in grace period")

	// Wake agent immediately so it reconnects well before the 300ms grace period expires.
	select {
	case agent.wakeCh <- struct{}{}:
	default:
	}

	select {
	case ids := <-reconnCh:
		if !slices.Contains(ids, "grace-task-1") {
			t.Fatalf("expected reconnect hello to report grace-task-1, got %v", ids)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for node reconnect")
	}

	close(unblockTurn)
	select {
	case ev := <-doneCh:
		if ev.Type != AgentEventDone || ev.Text != "resumed-ok" {
			t.Fatalf("expected task to finish after reconnect, got %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for resumed task completion")
	}

	// Part 2: Drop connection while task is active and DO NOT reconnect -> grace expires -> OnNodeOffline fires.
	reg.SetDisconnectGracePeriod(120 * time.Millisecond)
	unblock2 := make(chan struct{})
	defer close(unblock2)
	fake.mu.Lock()
	fake.fn = func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
		select {
		case <-unblock2:
			return &harness.TurnResult{BotID: opts.Bot.ID}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	fake.mu.Unlock()

	errCh2 := make(chan AgentEvent, 1)
	if err := reg.StartAgentTask(context.Background(), "grace-node", AgentStartRequest{
		TaskID:  "grace-task-2",
		BotID:   "b1",
		Harness: "opencode",
		Prompt:  "run",
	}, AgentTaskCallbacks{
		OnEvent: func(evt AgentEvent) {
			if evt.Type == AgentEventError {
				select {
				case errCh2 <- evt:
				default:
				}
			}
		},
	}); err != nil {
		t.Fatal(err)
	}
	waitForNodeCondition(t, 3*time.Second, func() bool {
		return len(agent.activeTaskIDs()) == 1
	}, "second task running on node")

	// Close agent cleanly? No, disable reconnect by closing loop via Configure? Wait: Configure(false) sends disconnect!
	// Instead, point agent to dead server or close server after closing ws? Closing srv shuts down new dials cleanly!
	connMu.Lock()
	secondConn := lastWS
	connMu.Unlock()
	srv.CloseClientConnections()
	_ = secondConn.CloseNow()
	srv.Close()

	select {
	case lostIDs := <-offlineCh:
		if !slices.Contains(lostIDs, "grace-task-2") {
			t.Fatalf("expected OnNodeOffline with grace-task-2, got %v", lostIDs)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OnNodeOffline after grace expiry")
	}

	select {
	case ev := <-errCh2:
		if ev.Type != AgentEventError {
			t.Fatalf("expected error event on grace expiry, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task error event after grace expiry")
	}

	if info, _ := reg.Get("grace-node"); info.Connected {
		t.Fatalf("expected grace-node offline after grace expiry, got %+v", info)
	}
}

