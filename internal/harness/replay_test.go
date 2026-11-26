package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/config"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		stderr    string
		wantKind  ErrorKind
		retryable bool
	}{
		{
			name:     "nil and empty stderr",
			wantKind: ErrorKindNone,
		},
		{
			name:      "http 429 rate limit",
			err:       errors.New("claude failed: API error 429 Too Many Requests"),
			wantKind:  ErrorKindUsageLimit,
			retryable: true,
		},
		{
			name:      "usage limit in stderr",
			err:       errors.New("process exited"),
			stderr:    "Error: You've hit your usage limit; resets at 18:00 UTC",
			wantKind:  ErrorKindUsageLimit,
			retryable: true,
		},
		{
			name:      "insufficient quota",
			err:       errors.New("insufficient_quota: credit balance is too low"),
			wantKind:  ErrorKindUsageLimit,
			retryable: true,
		},
		{
			name:     "auth required 401",
			err:      errors.New("HTTP 401 Unauthorized: invalid API key"),
			wantKind: ErrorKindAuthRequired,
		},
		{
			name:     "login required in stderr",
			err:      errors.New("exit status 1"),
			stderr:   "Not logged in. Please login first.",
			wantKind: ErrorKindAuthRequired,
		},
		{
			name:      "context deadline exceeded",
			err:       context.DeadlineExceeded,
			wantKind:  ErrorKindTimeout,
			retryable: true,
		},
		{
			name:      "timeout text",
			err:       errors.New("bot timed out after 30s"),
			wantKind:  ErrorKindTimeout,
			retryable: true,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			wantKind: ErrorKindCancelled,
		},
		{
			name:     "interrupted text",
			err:      errors.New("bot interrupted"),
			wantKind: ErrorKindCancelled,
		},
		{
			name:     "generic runtime error",
			err:      errors.New("segmentation fault in worker"),
			wantKind: ErrorKindRuntime,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyError(tc.err, tc.stderr)
			if got.Kind != tc.wantKind {
				t.Fatalf("Kind = %q, want %q (msg=%q)", got.Kind, tc.wantKind, got.Message)
			}
			if got.Retryable != tc.retryable {
				t.Fatalf("Retryable = %v, want %v", got.Retryable, tc.retryable)
			}
			if tc.err != nil && !errors.Is(got, tc.err) {
				t.Fatalf("expected Unwrap to preserve original error %v", tc.err)
			}
		})
	}
}

type minimalHarness struct{}

func (minimalHarness) Kind() string                             { return "minimal" }
func (minimalHarness) Label() string                            { return "Minimal" }
func (minimalHarness) RunTurn(context.Context, RunOptions) (*TurnResult, error) {
	return nil, nil
}
func (minimalHarness) Models(context.Context) ([]string, error) { return nil, nil }
func (minimalHarness) Close()                                   {}

func TestHarnessCapabilitiesOf(t *testing.T) {
	cl := NewClaudeHarness()
	defer cl.Close()
	cCaps := HarnessCapabilitiesOf(cl)
	if !cCaps.Streaming || !cCaps.Interrupt || !cCaps.SessionResume || !cCaps.UsageLimitAware {
		t.Fatalf("unexpected Claude capabilities: %+v", cCaps)
	}
	if len(cCaps.PermissionModes) < 4 {
		t.Fatalf("expected all 4 permission modes for Claude, got %v", cCaps.PermissionModes)
	}

	acp := NewCodeBuddyHarness()
	defer acp.Close()
	aCaps := HarnessCapabilitiesOf(acp)
	if !aCaps.Streaming || !aCaps.Interrupt || !aCaps.SessionResume || !aCaps.UsageLimitAware {
		t.Fatalf("unexpected ACP capabilities: %+v", aCaps)
	}

	minCaps := HarnessCapabilitiesOf(minimalHarness{})
	if !minCaps.Streaming || len(minCaps.PermissionModes) != 1 || minCaps.PermissionModes[0] != config.PermissionModeAuto || minCaps.UsageLimitAware {
		t.Fatalf("unexpected minimal fallback capabilities: %+v", minCaps)
	}

	zeroCaps := HarnessCapabilitiesOf(nil)
	if zeroCaps.Streaming || zeroCaps.Interrupt || len(zeroCaps.PermissionModes) != 0 {
		t.Fatalf("expected zero capabilities for nil harness, got %+v", zeroCaps)
	}
}

func TestReplayACPStreamFixtures(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	wireLog := filepath.Join(tmp, "acp-wire.log")
	script := filepath.Join(tmp, "replay-acp")

	fixtureScript := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"audit","description":"Audit codebase","input":{"hint":"<path>"}}]}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"acp-replay-1","modes":{"availableModes":[{"id":"default"},{"id":"accept-edits"},{"id":"plan"}]}}}\n' "$id"
      ;;
    *'"method":"session/set_mode"'*)
      echo "SET_MODE:$line" >> "` + wireLog + `"
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      if printf '%s' "$line" | grep -q "replay_ok"; then
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","title":"read_file","kind":"read"}}}\n'
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolName":"grep","kind":"read"}}}\n'
        printf '{"jsonrpc":"2.0","id":"perm-1","method":"session/request_permission","params":{"sessionId":"acp-replay-1","options":[{"optionId":"opt-once","kind":"allow_once","name":"Allow once"},{"optionId":"opt-deny","kind":"reject_once","name":"Deny"}],"toolCall":{"toolCallId":"tc-1","name":"Bash","kind":"execute","rawInput":{"command":"go test ./..."}}}}\n'
        IFS= read -r permResp
        echo "PERM_RESP:$permResp" >> "` + wireLog + `"
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Part 1. "}}}}\n'
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":[{"type":"text","text":"Part 2."}]}}}\n'
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      elif printf '%s' "$line" | grep -q "replay_rate_limit"; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"429 Too Many Requests: rate limit exceeded"}}\n' "$id"
      elif printf '%s' "$line" | grep -q "replay_cancel"; then
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"partial-acp"}}}}\n'
        printf '{"jsonrpc":"2.0","id":"perm-hang","method":"session/request_permission","params":{"sessionId":"acp-replay-1","options":[{"optionId":"opt-once","kind":"allow_once","name":"Allow"}],"toolCall":{"toolCallId":"tc-2","name":"Bash","rawInput":{"command":"sleep 30"}}}}\n'
        while IFS= read -r extra; do
          case "$extra" in
            *'"method":"session/cancel"'*)
              echo "CANCEL:$extra" >> "` + wireLog + `"
              printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"cancelled"}}\n' "$id"
              break
              ;;
          esac
        done
      fi
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(fixtureScript), 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewACPHarness(ACPOptions{
		Kind:      "codebuddy",
		Label:     "CodeBuddy CLI",
		Binaries:  []string{script},
		EnvPrefix: "CODEBUDDY",
	})
	defer h.Close()

	// 1. Streaming chunks, tool calls, permission prompt, and available commands replay.
	var chunks []string
	var tools []string
	var permSeen PermissionRequest
	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot:            config.BotConfig{ID: "r1", Workspace: ws, PermissionMode: config.PermissionModeAsk},
		Prompt:         "replay_ok",
		PermissionMode: config.PermissionModeAsk,
		OnText:         func(c string) { chunks = append(chunks, c) },
		OnToolUse:      func(tool string) { tools = append(tools, tool) },
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			permSeen = req
			return PermissionResponse{Decision: "allow_once"}, nil
		},
	})
	if err != nil {
		t.Fatalf("ACP replay_ok failed: %v", err)
	}
	if res.SessionID != "acp-replay-1" || res.Text != "Part 1. Part 2." {
		t.Fatalf("unexpected ACP TurnResult: %+v", res)
	}
	if strings.Join(chunks, "") != "Part 1. Part 2." {
		t.Fatalf("unexpected chunks: %v", chunks)
	}
	if len(tools) != 2 || tools[0] != "read_file" || tools[1] != "grep" {
		t.Fatalf("unexpected tools: %v", tools)
	}
	if permSeen.ToolName != "Bash" || permSeen.Kind != "shell" {
		t.Fatalf("unexpected permission request: %+v", permSeen)
	}
	cmds, err := h.ListCommands(context.Background(), config.BotConfig{ID: "r1", Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	assertHasCommand(t, cmds, "audit")

	// 2. Cancellation & terminal settlement replay.
	cancelCtx, cancel := context.WithCancel(context.Background())
	permStarted := make(chan struct{})
	permCancelled := make(chan struct{})
	go func() {
		<-permStarted
		cancel()
	}()
	partialRes, cancelErr := h.RunTurn(cancelCtx, RunOptions{
		Bot:            config.BotConfig{ID: "r1", Workspace: ws, PermissionMode: config.PermissionModeAsk},
		Prompt:         "replay_cancel",
		PermissionMode: config.PermissionModeAsk,
		OnPermission: func(pCtx context.Context, _ PermissionRequest) (PermissionResponse, error) {
			close(permStarted)
			<-pCtx.Done()
			close(permCancelled)
			return PermissionResponse{Decision: "cancelled"}, pCtx.Err()
		},
	})
	if cancelErr == nil || ClassifyError(cancelErr, "").Kind != ErrorKindCancelled {
		t.Fatalf("expected cancelled classification, got err=%v classified=%+v", cancelErr, ClassifyError(cancelErr, ""))
	}
	if partialRes == nil || partialRes.Text != "partial-acp" {
		t.Fatalf("expected partial text 'partial-acp', got %+v", partialRes)
	}
	select {
	case <-permCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("permission prompt context was not cancelled")
	}

	// 3. ACP JSON-RPC error classified as usage_limit.
	_, rlErr := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "r1", Workspace: ws},
		Prompt:    "replay_rate_limit",
		Ephemeral: true,
	})
	classified := ClassifyError(rlErr, "")
	if classified.Kind != ErrorKindUsageLimit || !classified.Retryable {
		t.Fatalf("expected usage_limit retryable error, got %+v (err=%v)", classified, rlErr)
	}

	wireRaw, _ := os.ReadFile(wireLog)
	wire := string(wireRaw)
	if !strings.Contains(wire, `"optionId":"opt-once"`) || !strings.Contains(wire, `"method":"session/cancel"`) {
		t.Fatalf("expected wire log to contain selected optionId and session/cancel:\n%s", wire)
	}
}

func TestReplayClaudeStreamJSONFixtures(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	respLog := filepath.Join(tmp, "claude-replay-resp.log")
	fakeClaude := filepath.Join(tmp, "claude")

	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      if printf '%s' "$line" | grep -q "replay_success"; then
        printf '%s\n' '{"type":"system","subtype":"init","session_id":"cl-replay-1","slash_commands":["compact","review"],"skills":["deploy-skill"]}'
        printf '%s\n' '{"type":"assistant","session_id":"cl-replay-1","message":{"content":[{"type":"tool_use","name":"Read"},{"type":"text","text":"Checking files..."}]}}'
        printf '%s\n' '{"type":"control_request","request_id":"cr-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test ./..."}}}'
        IFS= read -r cres
        echo "CRES:$cres" >> "` + respLog + `"
        printf '%s\n' '{"type":"assistant","session_id":"cl-replay-1","message":{"content":[{"type":"text","text":"All checks green."}]}}'
        printf '%s\n' '{"type":"result","subtype":"success","session_id":"cl-replay-1","is_error":false,"result":"Checking files...\nAll checks green.","total_cost_usd":0.0125,"usage":{"input_tokens":120,"output_tokens":45,"cache_read_input_tokens":80}}'
      elif printf '%s' "$line" | grep -q "replay_usage_limit"; then
        printf '%s\n' '{"type":"result","subtype":"error","session_id":"cl-replay-2","is_error":true,"result":"You have hit your usage limit; please retry later"}'
      fi
      ;;
  esac
done
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &ClaudeHarness{
		binary:   fakeClaude,
		cmdCache: make(map[string][]SlashCommand),
	}
	defer h.Close()

	var chunks []string
	var tools []string
	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot:            config.BotConfig{ID: "c-replay", Workspace: ws, PermissionMode: config.PermissionModeAsk},
		Prompt:         "replay_success",
		PermissionMode: config.PermissionModeAsk,
		OnText:         func(c string) { chunks = append(chunks, c) },
		OnToolUse:      func(name string) { tools = append(tools, name) },
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			if req.ToolName != "Bash" {
				t.Errorf("unexpected permission tool: %+v", req)
			}
			return PermissionResponse{Decision: "allow_once"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Claude replay_success failed: %v", err)
	}
	if res.SessionID != "cl-replay-1" || res.Usage.CostUSD != 0.0125 || res.Usage.InputTokens != 120 || res.Usage.OutputTokens != 45 || res.Usage.CacheReadTokens != 80 {
		t.Fatalf("unexpected Claude replay TurnResult: %+v", res)
	}
	if len(chunks) != 2 || len(tools) != 1 || tools[0] != "Read" {
		t.Fatalf("unexpected Claude stream callbacks: chunks=%v tools=%v", chunks, tools)
	}
	cmds, err := h.ListCommands(context.Background(), config.BotConfig{ID: "c-replay", Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	assertHasCommand(t, cmds, "deploy-skill")

	// Usage limit error result replay.
	_, ulErr := h.RunTurn(context.Background(), RunOptions{
		Bot:       config.BotConfig{ID: "c-replay", Workspace: ws},
		Prompt:    "replay_usage_limit",
		Ephemeral: true,
	})
	classified := ClassifyError(ulErr, "")
	if classified.Kind != ErrorKindUsageLimit || !classified.Retryable {
		t.Fatalf("expected usage_limit retryable error, got %+v (err=%v)", classified, ulErr)
	}
}
