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
	"testing"
	"time"
)

type questionTestHarness struct {
	run func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error)
}

func (q *questionTestHarness) Kind() string { return "opencode" }

func (q *questionTestHarness) Label() string { return "OpenCode" }

func (q *questionTestHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (q *questionTestHarness) Close() {}

func (q *questionTestHarness) RunTurn(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	return q.run(ctx, opts)
}

func TestHubQuestionLifecycleAndUnattendedBypass(t *testing.T) {
	tmp := t.TempDir()
	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))

	var mu sync.Mutex
	var sawOnQuestion []bool
	reg := harness.NewRegistry()
	reg.Register(&questionTestHarness{
		run: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			mu.Lock()
			sawOnQuestion = append(sawOnQuestion, opts.OnQuestion != nil)
			mu.Unlock()
			if opts.OnQuestion != nil {
				resp, err := opts.OnQuestion(ctx, harness.QuestionRequest{
					ID:    "frm_1",
					Title: "Confirm",
					Fields: []harness.QuestionField{
						{Key: "q0", Title: "Drink", Type: "string"},
					},
				})
				if err != nil {
					return nil, err
				}
				return &harness.TurnResult{Text: fmt.Sprint(resp.Answers["q0"])}, nil
			}
			return &harness.TurnResult{Text: "unattended"}, nil
		},
	})

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "_shared"),
		Bots: []config.BotConfig{
			{ID: "ops", Name: "Ops", Workspace: filepath.Join(tmp, "ops"), Harness: "opencode"},
		},
	}
	h := New(cfg, store, msgStore, reg)
	defer h.Close()

	subID, evCh := h.Subscribe()
	defer h.Unsubscribe(subID)

	type out struct {
		res *harness.TurnResult
		err error
	}
	done := make(chan out, 1)
	go func() {
		r, err := h.Send(context.Background(), "ops", "choose", SendOptions{Source: "user"})
		done <- out{r, err}
	}()

	var qID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items := h.ListQuestions("ops")
		if len(items) == 1 {
			qID = items[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if qID == "" {
		t.Fatalf("expected pending question in hub")
	}

	items, err := h.AnswerQuestion(qID, map[string]any{"q0": "tea"}, false, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("AnswerQuestion failed: items=%+v err=%v", items, err)
	}
	if _, err := h.AnswerQuestion(qID, nil, false, ""); !errors.Is(err, ErrQuestionNotFound) {
		t.Fatalf("expected ErrQuestionNotFound on second answer, got %v", err)
	}

	res := <-done
	if res.err != nil || res.res.Text != "tea" {
		t.Fatalf("unexpected interactive turn result: %+v err=%v", res.res, res.err)
	}

	sawQuestionsEvent := false
	drainDeadline := time.After(500 * time.Millisecond)
drain:
	for {
		select {
		case ev := <-evCh:
			if ev.Type == "questions" {
				sawQuestionsEvent = true
			}
		case <-drainDeadline:
			break drain
		}
	}
	if !sawQuestionsEvent {
		t.Fatalf("expected questions SSE event to be broadcast")
	}

	// Non-user sources (e.g. routine) must not receive OnQuestion.
	res2, err := h.Send(context.Background(), "ops", "cron", SendOptions{Source: "routine:morning"})
	if err != nil || res2.Text != "unattended" {
		t.Fatalf("unexpected unattended turn result: %+v err=%v", res2, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sawOnQuestion) != 2 || !sawOnQuestion[0] || sawOnQuestion[1] {
		t.Fatalf("expected OnQuestion=[true, false], got %v", sawOnQuestion)
	}
}

type envCaptureHarness struct {
	mu       sync.Mutex
	lastOpts harness.RunOptions
}

func (e *envCaptureHarness) Kind() string { return "fake" }

func (e *envCaptureHarness) Label() string { return "Fake" }

func (e *envCaptureHarness) Models(context.Context) ([]string, error) { return nil, nil }

func (e *envCaptureHarness) Close() {}

func (e *envCaptureHarness) RunTurn(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
	e.mu.Lock()
	e.lastOpts = opts
	e.mu.Unlock()
	return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "ses-env", Text: "ok"}, nil
}

func TestPermissionModeApprovalLifecycleAndSessionAllow(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	msgStore, err := state.NewMessageStore(filepath.Join(tmp, "messages"))
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var unattendedSawNilPerm bool
	var lastDecisions []string

	fullHarness := &permCapHarness{
		kind: "capfull",
		caps: harness.PermissionCapabilities{
			Modes:               []string{config.PermissionModeAuto, config.PermissionModeEdit, config.PermissionModeAsk, config.PermissionModePlan},
			InteractiveApproval: true,
			SupportsAllowAlways: true,
		},
		fn: func(ctx context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			if opts.Ephemeral {
				mu.Lock()
				unattendedSawNilPerm = opts.OnPermission == nil
				mu.Unlock()
				dec := harness.ResolveApprovalDecision(ctx, opts, harness.PermissionRequest{
					ToolName: "bash",
					Kind:     "shell",
					RawInput: map[string]any{"command": "rm -rf /tmp/x"},
				})
				return &harness.TurnResult{BotID: opts.Bot.ID, Text: dec.Decision}, nil
			}
			if opts.OnPermission == nil {
				return &harness.TurnResult{BotID: opts.Bot.ID, Text: "no-perm-cb"}, nil
			}
			resp, err := opts.OnPermission(ctx, harness.PermissionRequest{
				ID:       "r1",
				ToolName: "bash",
				Kind:     "shell",
				Title:    "bash",
				Summary:  "echo hi",
				RawInput: map[string]any{"command": "echo hi", "secret": "s3cr3t"},
				Options: []harness.PermissionOption{
					{ID: "real_once", Kind: "allow_once", Label: "Allow once"},
					{ID: "session_allow", Kind: "allow_always", Label: "Allow for session"},
					{ID: "deny", Kind: "reject", Label: "Deny"},
				},
			})
			if err != nil {
				return nil, err
			}
			mu.Lock()
			lastDecisions = append(lastDecisions, resp.Decision+":"+resp.OptionID+":"+resp.Reason)
			mu.Unlock()
			return &harness.TurnResult{BotID: opts.Bot.ID, SessionID: "s1", Text: resp.Decision}, nil
		},
	}

	autoOnlyHarness := &permCapHarness{
		kind: "capauto",
		caps: harness.PermissionCapabilities{Modes: []string{config.PermissionModeAuto}},
		fn: func(_ context.Context, opts harness.RunOptions) (*harness.TurnResult, error) {
			return &harness.TurnResult{BotID: opts.Bot.ID, Text: "auto"}, nil
		},
	}

	reg := harness.NewRegistry()
	reg.Register(fullHarness)
	reg.Register(autoOnlyHarness)

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "dev", Name: "Dev", Workspace: filepath.Join(tmp, "workspaces", "dev"), Harness: "capfull", PermissionMode: config.PermissionModeAsk},
		},
	}
	h := New(cfg, store, msgStore, reg)
	defer h.Close()

	// 1. /mode command queries & updates mode.
	resQ, err := h.HandleInput(context.Background(), "dev", "/mode", SendOptions{Source: "web"})
	if err != nil || !strings.Contains(resQ.Text, "Current permission mode: ask") {
		t.Fatalf("/mode query failed: res=%+v err=%v", resQ, err)
	}
	if _, err := h.HandleInput(context.Background(), "dev", "/mode invalid", SendOptions{Source: "web"}); err == nil {
		t.Fatal("expected /mode invalid to fail")
	}

	// 2. Interactive approval + ReplyApproval("allow_always") + audit message + sessionAllow cache hit.
	done1 := make(chan *harness.TurnResult, 1)
	go func() {
		r, err := h.HandleInput(context.Background(), "dev", "run tool 1", SendOptions{Source: "web"})
		if err != nil {
			t.Errorf("turn 1 error: %v", err)
		}
		done1 <- r
	}()

	var apr PendingApproval
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		list := h.ListApprovals("dev")
		if len(list) == 1 {
			apr = list[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if apr.ID == "" || apr.RawInput["secret"] != "[REDACTED]" {
		t.Fatalf("expected redacted pending approval, got %+v", apr)
	}
	if _, err := h.ReplyApproval(apr.ID, "allow_always", "session_allow", ""); err != nil {
		t.Fatalf("ReplyApproval failed: %v", err)
	}
	<-done1

	// Subsequent call for same tool must auto-allow immediately using real protocol optionId ("real_once", NOT "session_allow")!
	res2, err := h.HandleInput(context.Background(), "dev", "run tool 2", SendOptions{Source: "web"})
	if err != nil || res2.Text != "allow_once" {
		t.Fatalf("expected sessionAllow hit -> allow_once, got %+v err=%v", res2, err)
	}
	mu.Lock()
	if len(lastDecisions) < 2 || lastDecisions[1] != "allow_once:real_once:" {
		mu.Unlock()
		t.Fatalf("expected second decision to select real_once (not session_allow), got %+v", lastDecisions)
	}
	mu.Unlock()

	// 3. ResetSession clears sessionAllow -> prompts again, then ReplyApproval("reject", reason).
	if _, _, err := h.ResetSession("dev"); err != nil {
		t.Fatal(err)
	}
	done3 := make(chan error, 1)
	go func() {
		_, err := h.HandleInput(context.Background(), "dev", "run tool 3", SendOptions{Source: "web"})
		done3 <- err
	}()
	var apr3 PendingApproval
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if list := h.ListApprovals("dev"); len(list) == 1 {
			apr3 = list[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if apr3.ID == "" {
		t.Fatal("expected prompt after ResetSession cleared sessionAllow")
	}
	if _, err := h.ReplyApproval(apr3.ID, "reject", "deny", "危险命令"); err != nil {
		t.Fatal(err)
	}
	if err := <-done3; err != nil {
		t.Fatal(err)
	}

	msgs, _ := h.Messages("dev", 20)
	var sawAuditAllow, sawAuditDeny bool
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Text, "Always allowed for this session: bash") {
			sawAuditAllow = true
		}
		if m.Role == "system" && strings.Contains(m.Text, "Rejected bash") && strings.Contains(m.Text, "危险命令") {
			sawAuditDeny = true
		}
	}
	if !sawAuditAllow || !sawAuditDeny {
		t.Fatalf("missing audit messages in %+v", msgs)
	}

	// 4. Unattended RunIsolated with permissionMode="ask" sets OnPermission=nil and auto-denies.
	isoRes, err := h.RunIsolated(context.Background(), "dev", "routine", IsolatedOptions{
		Source:         "routine",
		PermissionMode: config.PermissionModeAsk,
	})
	if err != nil || isoRes.Text != "reject" {
		t.Fatalf("expected unattended ask to auto-deny, got res=%+v err=%v", isoRes, err)
	}
	mu.Lock()
	if !unattendedSawNilPerm {
		mu.Unlock()
		t.Fatal("expected OnPermission == nil for unattended routine")
	}
	mu.Unlock()

	// 5. Switching harness to autoOnly auto-falls back permissionMode to "auto"; setting unsupported mode explicitly fails.
	capAutoKind := "capauto"
	askMode := config.PermissionModeAsk
	st, err := h.UpdateBot("dev", BotUpdate{Harness: &capAutoKind})
	if err != nil || st.PermissionMode != config.PermissionModeAuto {
		t.Fatalf("expected auto fallback when switching harness, got st=%+v err=%v", st, err)
	}
	if _, err := h.UpdateBot("dev", BotUpdate{PermissionMode: &askMode}); err == nil {
		t.Fatal("expected explicit unsupported permission mode on capauto to fail")
	}
}
