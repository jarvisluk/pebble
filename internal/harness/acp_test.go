package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"bot-hub/internal/config"
)

func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ACP_HELPER") != "1" {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg rpcMessage
		if json.Unmarshal([]byte(line), &msg) != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}`+"\n", string(msg.ID))
		case "session/new":
			fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"acp-sess-1"}}`+"\n", string(msg.ID))
		case "session/prompt":
			// Send a tool_call update, request permission, then send message chunks.
			fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","title":"read_file"}}}`)
			fmt.Println(`{"jsonrpc":"2.0","id":999,"method":"session/request_permission","params":{"options":[{"optionId":"opt-allow","kind":"allow_once"}]}}`)
			// Wait for permission response from client.
			if sc.Scan() {
				fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello "}}}}`)
				fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"acp"}}}}`)
				fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}`+"\n", string(msg.ID))
			}
		}
	}
	os.Exit(0)
}

func TestACPHarnessRunTurn(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := NewACPHarness(ACPOptions{
		Kind:      "codebuddy",
		Label:     "CodeBuddy CLI",
		Binaries:  []string{exe},
		Args:      []string{"-test.run=TestACPHelperProcess"},
		EnvPrefix: "CODEBUDDY",
	})
	t.Cleanup(h.Close)

	t.Setenv("CODEBUDDY_BIN", exe)
	t.Setenv("GO_WANT_ACP_HELPER", "1")

	var streamed strings.Builder
	var tools []string
	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "b1", Workspace: t.TempDir()},
		Prompt: "hi",
		OnText: func(c string) { streamed.WriteString(c) },
		OnToolUse: func(name string) {
			tools = append(tools, name)
		},
	})
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if res.SessionID != "acp-sess-1" {
		t.Fatalf("SessionID = %q, want acp-sess-1", res.SessionID)
	}
	if res.Text != "hello acp" || streamed.String() != "hello acp" {
		t.Fatalf("Text = %q, streamed = %q", res.Text, streamed.String())
	}
	if len(tools) != 1 || tools[0] != "read_file" {
		t.Fatalf("tools = %v, want [read_file]", tools)
	}
}
