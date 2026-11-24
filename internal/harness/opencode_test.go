package harness

import (
	"context"
	"encoding/json"
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

	"bot-hub/internal/config"
)

func TestOpenCodeQuestionCardFlow(t *testing.T) {
	var (
		mu       sync.Mutex
		replies  []map[string]any
		cancels  []string
		evCh     = make(chan string, 16)
		prompted = make(chan struct{}, 1)
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/info":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/event":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"server.connected\",\"data\":{}}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			for {
				select {
				case <-r.Context().Done():
					return
				case msg := <-evCh:
					_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
					if flusher != nil {
						flusher.Flush()
					}
				}
			}

		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"ses_1"}}`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_1/prompt":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			select {
			case prompted <- struct{}{}:
			default:
			}

		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_1/form/frm_1/reply":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			replies = append(replies, body)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			evCh <- `{"type":"session.text.ended","data":{"sessionID":"ses_1","text":"选择了茶"}}`
			evCh <- `{"type":"session.execution.succeeded","data":{"sessionID":"ses_1"}}`

		case r.Method == http.MethodDelete && r.URL.Path == "/api/session/ses_1/form/frm_cancel":
			mu.Lock()
			cancels = append(cancels, r.URL.RawQuery)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			evCh <- `{"type":"session.text.ended","data":{"sessionID":"ses_1","text":"已跳过"}}`
			evCh <- `{"type":"session.execution.succeeded","data":{"sessionID":"ses_1"}}`

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	h := NewOpenCodeHarness(srv.URL)
	defer h.Close()

	// 1. Reply flow: form.created with sessionID nested inside data.form.sessionID.
	go func() {
		<-prompted
		evCh <- `{"type":"form.created","data":{"form":{"id":"frm_1","sessionID":"ses_1","title":"Questions","fields":[{"key":"q0","title":"饮品","description":"茶还是咖啡？","type":"string","options":[{"value":"tea","label":"茶"},{"value":"coffee","label":"咖啡"}],"custom":true}]}}}`
	}()

	var gotReq QuestionRequest
	res, err := h.RunTurn(context.Background(), RunOptions{
		Bot:    config.BotConfig{ID: "b1", Workspace: t.TempDir()},
		Prompt: "ask",
		OnQuestion: func(_ context.Context, req QuestionRequest) (QuestionResponse, error) {
			gotReq = req
			return QuestionResponse{Answers: map[string]any{"q0": "tea"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunTurn reply failed: %v", err)
	}
	if res.Text != "选择了茶" {
		t.Fatalf("unexpected text: %q", res.Text)
	}
	if gotReq.ID != "frm_1" || len(gotReq.Fields) != 1 || gotReq.Fields[0].Key != "q0" {
		t.Fatalf("unexpected QuestionRequest: %+v", gotReq)
	}
	mu.Lock()
	if len(replies) != 1 || replies[0]["answer"].(map[string]any)["q0"] != "tea" {
		mu.Unlock()
		t.Fatalf("unexpected reply payload: %+v", replies)
	}
	mu.Unlock()

	// 2. Non-interactive flow (OnQuestion == nil) must DELETE the form immediately.
	go func() {
		<-prompted
		evCh <- `{"type":"form.created","data":{"form":{"id":"frm_cancel","sessionID":"ses_1","title":"Questions","fields":[{"key":"q0","title":"饮品","type":"string"}]}}}`
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res2, err := h.RunTurn(ctx, RunOptions{
		Bot:    config.BotConfig{ID: "b1", Workspace: t.TempDir()},
		Prompt: "ask unattended",
	})
	if err != nil {
		t.Fatalf("RunTurn unattended failed: %v", err)
	}
	if res2.Text != "已跳过" {
		t.Fatalf("unexpected text: %q", res2.Text)
	}
	mu.Lock()
	if len(cancels) != 1 || cancels[0] != "message=non-interactive" {
		mu.Unlock()
		t.Fatalf("expected cancel query message=non-interactive, got %+v", cancels)
	}
	mu.Unlock()
}

func TestOpenCodeSlashCommandAndCompactAndFiles(t *testing.T) {
	evCh := make(chan string, 16)
	var mu sync.Mutex
	var hits []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/info":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/event":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"server.connected\",\"data\":{}}\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			for {
				select {
				case <-r.Context().Done():
					return
				case msg := <-evCh:
					_, _ = w.Write([]byte("data: " + msg + "\n\n"))
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			_, _ = w.Write([]byte(`{"data":{"id":"ses_cmd"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/command":
			_, _ = w.Write([]byte(`{"data":[{"name":"review","description":"Code review"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/skill":
			_, _ = w.Write([]byte(`{"data":[{"name":"pdf","description":"PDF skill"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_cmd/command":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			hits = append(hits, "command:"+fmt.Sprint(body["name"]))
			mu.Unlock()
			if body["name"] == "unknown" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"command not found"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
			evCh <- `{"type":"session.text.ended","data":{"sessionID":"ses_cmd","text":"cmd ok"}}`
			evCh <- `{"type":"session.execution.succeeded","data":{"sessionID":"ses_cmd"}}`
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_cmd/prompt":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			files, _ := body["files"].([]any)
			mu.Lock()
			hits = append(hits, fmt.Sprintf("prompt:%v:files=%d", body["text"], len(files)))
			mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
			evCh <- `{"type":"session.text.ended","data":{"sessionID":"ses_cmd","text":"prompt ok"}}`
			evCh <- `{"type":"session.execution.succeeded","data":{"sessionID":"ses_cmd"}}`
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_cmd/compact":
			mu.Lock()
			hits = append(hits, "compact")
			mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
			evCh <- `{"type":"session.execution.succeeded","data":{"sessionID":"ses_cmd"}}`
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	h := NewOpenCodeHarness(srv.URL)
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot := config.BotConfig{ID: "b1", Workspace: t.TempDir()}

	cmds, err := h.ListCommands(ctx, bot)
	if err != nil || len(cmds) < 3 {
		t.Fatalf("expected compact+review+pdf commands, got %+v err=%v", cmds, err)
	}

	if _, err := h.RunTurn(ctx, RunOptions{Bot: bot, Prompt: "/review src/main.go"}); err != nil {
		t.Fatalf("command RunTurn failed: %v", err)
	}
	if _, err := h.RunTurn(ctx, RunOptions{
		Bot:         bot,
		Prompt:      "/unknown fallback",
		Attachments: []Attachment{{ID: "a1", Kind: "file", Name: "doc.txt", Path: "/tmp/doc.txt", WorkspacePath: ".uploads/doc.txt"}},
	}); err != nil {
		t.Fatalf("fallback RunTurn failed: %v", err)
	}
	resCompact, err := h.RunTurn(ctx, RunOptions{Bot: bot, Prompt: "/compact"})
	if err != nil {
		t.Fatalf("compact RunTurn failed: %v", err)
	}
	if resCompact.Text == "" {
		t.Fatalf("expected non-empty compact result text")
	}

	mu.Lock()
	joined := strings.Join(hits, "|")
	mu.Unlock()
	if !strings.Contains(joined, "command:review") || !strings.Contains(joined, "command:unknown") || !strings.Contains(joined, "files=1") || !strings.Contains(joined, "compact") {
		t.Fatalf("unexpected hit sequence: %s", joined)
	}
}

func TestOpenCodeListCommandsSkipsColdStart(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "started")
	fakeOC := filepath.Join(tmp, "opencode")
	if err := os.WriteFile(fakeOC, []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_BIN", fakeOC)

	h := NewOpenCodeHarness("")
	defer h.Close()

	cmds, err := h.ListCommands(context.Background(), config.BotConfig{ID: "b1", Workspace: tmp})
	if err != nil {
		t.Fatalf("ListCommands failed: %v", err)
	}
	if len(cmds) == 0 || cmds[0].Name != "compact" {
		t.Fatalf("expected builtin compact command, got %+v", cmds)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ListCommands must not cold-start opencode serve when unstarted")
	}
}

func TestOpenCodeSkipsPreflightAndRecoversOn404(t *testing.T) {
	evCh := make(chan string, 16)
	var sessionGetCount atomic.Int32
	var sessionCreateCount atomic.Int32
	var dropFirstSession atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/info":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/event":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"server.connected\",\"data\":{}}\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			for {
				select {
				case <-r.Context().Done():
					return
				case msg := <-evCh:
					_, _ = w.Write([]byte("data: " + msg + "\n\n"))
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			n := sessionCreateCount.Add(1)
			_, _ = fmt.Fprintf(w, `{"data":{"id":"ses_%d"}}`, n)
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_1":
			sessionGetCount.Add(1)
			_, _ = w.Write([]byte(`{"data":{"id":"ses_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_1/prompt":
			if dropFirstSession.Load() {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"session not found"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
			evCh <- `{"type":"session.text.ended","data":{"sessionID":"ses_1","text":"ok1"}}`
			evCh <- `{"type":"session.execution.succeeded","data":{"sessionID":"ses_1"}}`
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_2/prompt":
			_, _ = w.Write([]byte(`{}`))
			evCh <- `{"type":"session.text.ended","data":{"sessionID":"ses_2","text":"recovered"}}`
			evCh <- `{"type":"session.execution.succeeded","data":{"sessionID":"ses_2"}}`
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	h := NewOpenCodeHarness(srv.URL)
	defer h.Close()

	bot := config.BotConfig{ID: "b1", Workspace: t.TempDir()}
	res1, err := h.RunTurn(context.Background(), RunOptions{Bot: bot, Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.RunTurn(context.Background(), RunOptions{Bot: bot, SessionID: res1.SessionID, Prompt: "second"}); err != nil {
		t.Fatal(err)
	}
	if got := sessionGetCount.Load(); got != 0 {
		t.Fatalf("expected 0 preflight GET /api/session calls for known session, got %d", got)
	}

	// Simulate server restarting and forgetting ses_1; known session should recover via 404 -> create ses_2.
	dropFirstSession.Store(true)
	res3, err := h.RunTurn(context.Background(), RunOptions{Bot: bot, SessionID: res1.SessionID, Prompt: "third"})
	if err != nil {
		t.Fatalf("expected 404 recovery to succeed, got %v", err)
	}
	if res3.SessionID != "ses_2" || res3.Text != "recovered" {
		t.Fatalf("unexpected recovery result: %+v", res3)
	}
}
