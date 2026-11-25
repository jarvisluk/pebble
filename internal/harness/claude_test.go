package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bot-hub/internal/config"
)

func TestClaudePermissionModesAndControlRequests(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	argsLog := filepath.Join(tmp, "claude-args.log")
	respLog := filepath.Join(tmp, "claude-resp.log")
	fakeClaude := filepath.Join(tmp, "claude")

	script := `#!/bin/sh
echo "ARGS:$*" >> "` + argsLog + `"
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      if printf '%s' "$line" | grep -q "case_allow_once"; then
        printf '{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls -la","secret":"s3cr3t"}}}\n'
        IFS= read -r cres
        echo "CRES1:$cres" >> "` + respLog + `"
      elif printf '%s' "$line" | grep -q "case_allow_always"; then
        printf '{"type":"control_request","request_id":"req-2","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"git status"},"permission_suggestions":[{"type":"addRules","behavior":"allow","toolName":"Bash"}]}}\n'
        IFS= read -r cres
        echo "CRES2:$cres" >> "` + respLog + `"
      elif printf '%s' "$line" | grep -q "case_deny"; then
        printf '{"type":"control_request","request_id":"req-3","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"rm -rf /"}}}\n'
        IFS= read -r cres
        echo "CRES3:$cres" >> "` + respLog + `"
      elif printf '%s' "$line" | grep -q "case_plan_write"; then
        printf '{"type":"control_request","request_id":"req-4","request":{"subtype":"can_use_tool","tool_name":"Edit","input":{"file_path":"` + ws + `/a.txt"}}}\n'
        IFS= read -r cres
        echo "CRES4:$cres" >> "` + respLog + `"
      elif printf '%s' "$line" | grep -q "case_edit_inside"; then
        printf '{"type":"control_request","request_id":"req-5","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"` + ws + `/b.txt"}}}\n'
        IFS= read -r cres
        echo "CRES5:$cres" >> "` + respLog + `"
      fi
      printf '{"type":"result","session_id":"claude-s1","is_error":false,"result":"done","usage":{"input_tokens":5,"output_tokens":3}}\n'
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

	// 1. auto mode uses bypassPermissions and omits --permission-prompt-tool stdio.
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "c1", Workspace: ws, PermissionMode: config.PermissionModeAuto},
		Prompt: "plain",
	}); err != nil {
		t.Fatalf("auto RunTurn failed: %v", err)
	}

	// 2. ask mode: allow_once + redaction check.
	var gotReq PermissionRequest
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "c1", Workspace: ws, PermissionMode: config.PermissionModeAsk},
		Prompt: "case_allow_once",
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			gotReq = req
			return PermissionResponse{Decision: "allow_once"}, nil
		},
	}); err != nil {
		t.Fatalf("allow_once RunTurn failed: %v", err)
	}
	if gotReq.RawInput["secret"] != "[REDACTED]" {
		t.Fatalf("expected secret redacted, got %+v", gotReq.RawInput)
	}

	// 3. ask mode: allow_always passes updatedPermissions.
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "c1", Workspace: ws, PermissionMode: config.PermissionModeAsk},
		Prompt: "case_allow_always",
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			return PermissionResponse{Decision: "allow_always"}, nil
		},
	}); err != nil {
		t.Fatalf("allow_always RunTurn failed: %v", err)
	}

	// 4. ask mode: unattended (OnPermission == nil) auto-denies.
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "c1", Workspace: ws, PermissionMode: config.PermissionModeAsk},
		Prompt: "case_deny",
	}); err != nil {
		t.Fatalf("unattended deny RunTurn failed: %v", err)
	}

	// 5. plan mode: Edit rejected without prompting.
	called := false
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "c1", Workspace: ws, PermissionMode: config.PermissionModePlan},
		Prompt: "case_plan_write",
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			called = true
			return PermissionResponse{Decision: "allow_once"}, nil
		},
	}); err != nil {
		t.Fatalf("plan RunTurn failed: %v", err)
	}
	if called {
		t.Fatal("expected plan mode to auto-deny Edit without invoking OnPermission")
	}

	// 6. edit mode: workspace Write auto-allowed without prompting.
	if _, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "c1", Workspace: ws, PermissionMode: config.PermissionModeEdit},
		Prompt: "case_edit_inside",
		OnPermission: func(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
			t.Fatal("edit mode should auto-allow workspace write")
			return PermissionResponse{}, nil
		},
	}); err != nil {
		t.Fatalf("edit RunTurn failed: %v", err)
	}

	rawArgs, _ := os.ReadFile(argsLog)
	sArgs := string(rawArgs)
	for _, want := range []string{
		"--permission-mode bypassPermissions",
		"--permission-mode default --permission-prompt-tool stdio",
		"--permission-mode plan --permission-prompt-tool stdio",
		"--permission-mode acceptEdits --permission-prompt-tool stdio",
	} {
		if !strings.Contains(sArgs, want) {
			t.Fatalf("expected args to contain %q:\n%s", want, sArgs)
		}
	}

	rawResp, _ := os.ReadFile(respLog)
	respLines := strings.Split(strings.TrimSpace(string(rawResp)), "\n")
	if len(respLines) != 5 {
		t.Fatalf("expected 5 control_responses, got %d:\n%s", len(respLines), rawResp)
	}
	assertLine := func(idx int, prefix string, wants ...string) {
		t.Helper()
		if !strings.HasPrefix(respLines[idx], prefix) {
			t.Fatalf("line %d = %q, want prefix %q", idx, respLines[idx], prefix)
		}
		for _, w := range wants {
			if !strings.Contains(respLines[idx], w) {
				t.Fatalf("line %d missing %q: %s", idx, w, respLines[idx])
			}
		}
	}
	assertLine(0, "CRES1:", `"request_id":"req-1"`, `"behavior":"allow"`, `"secret":"s3cr3t"`)
	assertLine(1, "CRES2:", `"request_id":"req-2"`, `"behavior":"allow"`, `"updatedPermissions":[{"behavior":"allow","toolName":"Bash","type":"addRules"}]`)
	assertLine(2, "CRES3:", `"request_id":"req-3"`, `"behavior":"deny"`)
	assertLine(3, "CRES4:", `"request_id":"req-4"`, `"behavior":"deny"`)
	assertLine(4, "CRES5:", `"request_id":"req-5"`, `"behavior":"allow"`)
}

func TestClaudeInterruptCancelsPendingApproval(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	_ = os.MkdirAll(ws, 0o755)
	fakeClaude := filepath.Join(tmp, "claude")
	script := `#!/bin/sh
while IFS= read -r line; do
  printf '{"type":"control_request","request_id":"req-hang","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"sleep 10"}}}\n'
  sleep 10
done
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &ClaudeHarness{binary: fakeClaude, cmdCache: make(map[string][]SlashCommand)}
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	unblocked := make(chan struct{})
	permStarted := make(chan struct{})
	go func() {
		<-permStarted
		cancel()
	}()
	_, err := h.RunTurn(ctx, RunOptions{
		Bot:    config.BotConfig{ID: "c2", Workspace: ws, PermissionMode: config.PermissionModeAsk},
		Prompt: "hang",
		OnPermission: func(pCtx context.Context, _ PermissionRequest) (PermissionResponse, error) {
			close(permStarted)
			<-pCtx.Done()
			close(unblocked)
			return PermissionResponse{Decision: "cancelled"}, pCtx.Err()
		},
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("expected interrupted error, got %v", err)
	}
	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("OnPermission was not cancelled on turn interrupt")
	}
}
