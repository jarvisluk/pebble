package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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
