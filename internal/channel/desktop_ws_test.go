package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDesktopStreamWSAuthAndUpgrade(t *testing.T) {
	srv, _ := newTestServer(t, `{"http":{"apiToken":"ws-secret"},"bots":[{"id":"a","name":"A"}]}`)
	ts := httptest.NewServer(srv.srv.Handler)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/desktop/stream/ws?target=local&preset=auto"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Unauthenticated upgrade must be rejected with 401.
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected unauthenticated WS dial to fail")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Authenticated upgrade must succeed.
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer ws-secret"}},
	})
	if err != nil {
		t.Fatalf("authenticated WS dial failed (resp=%v): %v", resp, err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "test done")
}
