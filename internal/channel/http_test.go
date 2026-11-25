package channel

import (
	"bot-hub/internal/config"
	"bot-hub/internal/harness"
	"bot-hub/internal/hub"
	"bot-hub/internal/state"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	cases := []struct {
		name   string
		cfg    config.HTTPConfig
		method string
		path   string
		host   string
		origin string
		auth   string
		want   int
	}{
		{"same-origin ok", config.HTTPConfig{Host: "127.0.0.1"}, "POST", "/api/bots/a/message", "127.0.0.1:3789", "http://127.0.0.1:3789", "", 200},
		{"cross-site POST blocked", config.HTTPConfig{Host: "127.0.0.1"}, "POST", "/api/bots/a/message", "127.0.0.1:3789", "https://evil.example", "", 403},
		{"cross-site webhook blocked", config.HTTPConfig{Host: "127.0.0.1"}, "POST", "/hooks/a", "127.0.0.1:3789", "https://evil.example", "", 403},
		{"webhook any host ok", config.HTTPConfig{Host: "127.0.0.1"}, "POST", "/hooks/a", "tunnel.example", "", "", 200},
		{"dns rebinding blocked", config.HTTPConfig{Host: "127.0.0.1"}, "GET", "/api/bots", "evil.example:3789", "", "", 403},
		{"token missing", config.HTTPConfig{Host: "127.0.0.1", APIToken: "t"}, "GET", "/api/bots", "localhost:3789", "", "", 401},
		{"token ok", config.HTTPConfig{Host: "127.0.0.1", APIToken: "t"}, "GET", "/api/bots", "localhost:3789", "", "Bearer t", 200},
		{"index without token ok", config.HTTPConfig{Host: "127.0.0.1", APIToken: "t"}, "GET", "/", "localhost:3789", "", "", 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &HTTPServer{cfg: c.cfg}
			req := httptest.NewRequest(c.method, c.path, strings.NewReader("{}"))
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.auth != "" {
				req.Header.Set("Authorization", c.auth)
			}
			rec := httptest.NewRecorder()
			s.guard(ok).ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestSessionRecallRotatePrivateAndMemoryRoutes(t *testing.T) {
	tmp := t.TempDir()
	store, err := state.NewStore(filepath.Join(tmp, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})

	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		Bots: []config.BotConfig{
			{ID: "a", Name: "A", Workspace: filepath.Join(tmp, "workspaces", "a"), Harness: "opencode", PrivateSessions: true},
			{ID: "b", Name: "B", Workspace: filepath.Join(tmp, "workspaces", "b"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, nil, reg)
	defer h.Close()

	s1, err := store.EnsureActiveSession("a", "opencode", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessage(state.ChatMessage{
		BotID: "a", SessionID: s1.ID, Turn: 1, Role: "user", From: "web", Text: "排查 Nginx 502 错误",
	}); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	// Rotate bot a's session immediately with handoff note.
	rotReq := httptest.NewRequest(http.MethodPost, "/api/bots/a/rotate", bytes.NewBufferString(`{"handoffNote":"handoff nginx","defer":false}`))
	rotReq.Host = "127.0.0.1:3789"
	rotRec := httptest.NewRecorder()
	mux.ServeHTTP(rotRec, rotReq)
	if rotRec.Code != http.StatusOK || !strings.Contains(rotRec.Body.String(), `"rotated":true`) {
		t.Fatalf("POST /api/bots/a/rotate failed: %d %s", rotRec.Code, rotRec.Body.String())
	}

	// Human can list and recall bot a's session.
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions?query=Nginx", nil)
	listReq.Host = "127.0.0.1:3789"
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), s1.ID) {
		t.Fatalf("GET /api/sessions failed: %d %s", listRec.Code, listRec.Body.String())
	}

	recallReq := httptest.NewRequest(http.MethodGet, "/api/bots/a/sessions/previous", nil)
	recallReq.Host = "127.0.0.1:3789"
	recallRec := httptest.NewRecorder()
	mux.ServeHTTP(recallRec, recallReq)
	if recallRec.Code != http.StatusOK || !strings.Contains(recallRec.Body.String(), "handoff nginx") {
		t.Fatalf("GET /api/bots/a/sessions/previous failed: %d %s", recallRec.Code, recallRec.Body.String())
	}

	// Cross-bot caller b gets 403 on private bot a.
	forbReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+s1.ID+"?callerBotId=b", nil)
	forbReq.Host = "127.0.0.1:3789"
	forbRec := httptest.NewRecorder()
	mux.ServeHTTP(forbRec, forbReq)
	if forbRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for private bot session recall, got %d %s", forbRec.Code, forbRec.Body.String())
	}

	// Memory route gracefully answers even if magpie isn't installed or gateway offline.
	memReq := httptest.NewRequest(http.MethodGet, "/api/memory?scope=global", nil)
	memReq.Host = "127.0.0.1:3789"
	memRec := httptest.NewRecorder()
	mux.ServeHTTP(memRec, memReq)
	if memRec.Code != http.StatusOK || !strings.Contains(memRec.Body.String(), `"items":`) {
		t.Fatalf("GET /api/memory failed: %d %s", memRec.Code, memRec.Body.String())
	}
}

func TestOutboundCollabAndMediaEndpoints(t *testing.T) {
	tmp := t.TempDir()
	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	reg := harness.NewRegistry()
	reg.Register(noopHarness{kind: "opencode"})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "alpha", Name: "Alpha", Workspace: filepath.Join(tmp, "alpha"), Harness: "opencode"},
			{ID: "beta", Name: "Beta", Workspace: filepath.Join(tmp, "beta"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()

	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})
	defer srv.Shutdown(t.Context())
	mux := srv.srv.Handler

	// 1x1 PNG in base64
	b64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	outReq := httptest.NewRequest(http.MethodPost, "/api/bots/alpha/outbound", bytes.NewBufferString(
		`{"text":"look at this","images":[{"dataBase64":"`+b64+`","filename":"dot.png","alt":"dot"}]}`,
	))
	outReq.Host = "127.0.0.1:3789"
	outRec := httptest.NewRecorder()
	mux.ServeHTTP(outRec, outReq)
	if outRec.Code != http.StatusOK {
		t.Fatalf("POST /api/bots/alpha/outbound failed: %d %s", outRec.Code, outRec.Body.String())
	}
	var sent state.ChatMessage
	if err := json.Unmarshal(outRec.Body.Bytes(), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Role != "assistant" || len(sent.Media) != 1 || !strings.HasPrefix(sent.Media[0].URL, "/api/media/") {
		t.Fatalf("unexpected outbound message: %+v", sent)
	}

	// Fetch media
	medReq := httptest.NewRequest(http.MethodGet, sent.Media[0].URL, nil)
	medReq.Host = "127.0.0.1:3789"
	medRec := httptest.NewRecorder()
	mux.ServeHTTP(medRec, medReq)
	if medRec.Code != http.StatusOK || medRec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GET media failed: %d ct=%s", medRec.Code, medRec.Header().Get("Content-Type"))
	}

	// Invalid media ID -> 400
	badReq := httptest.NewRequest(http.MethodGet, "/api/media/..secret.png", nil)
	badReq.Host = "127.0.0.1:3789"
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid media id, got %d", badRec.Code)
	}

	// Collab endpoint
	colReq := httptest.NewRequest(http.MethodGet, "/api/bots/alpha/collab/beta", nil)
	colReq.Host = "127.0.0.1:3789"
	colRec := httptest.NewRecorder()
	mux.ServeHTTP(colRec, colReq)
	if colRec.Code != http.StatusOK || !strings.Contains(colRec.Body.String(), `"messages"`) {
		t.Fatalf("GET collab failed: %d %s", colRec.Code, colRec.Body.String())
	}

	// Outbound file with attachment disposition & sandbox CSP
	fileReq := httptest.NewRequest(http.MethodPost, "/api/bots/alpha/outbound", bytes.NewBufferString(
		`{"text":"report","files":[{"dataBase64":"JVBERi0xLjQK","filename":"report.html"}]}`,
	))
	fileReq.Host = "127.0.0.1:3789"
	fileRec := httptest.NewRecorder()
	mux.ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("POST file outbound failed: %d %s", fileRec.Code, fileRec.Body.String())
	}
	var fileMsg state.ChatMessage
	_ = json.Unmarshal(fileRec.Body.Bytes(), &fileMsg)
	if len(fileMsg.Media) != 1 || fileMsg.Media[0].Kind != "file" {
		t.Fatalf("expected 1 file media, got %+v", fileMsg.Media)
	}

	dlReq := httptest.NewRequest(http.MethodGet, fileMsg.Media[0].URL+"?name=report.html", nil)
	dlReq.Host = "127.0.0.1:3789"
	dlRec := httptest.NewRecorder()
	mux.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusOK ||
		!strings.Contains(dlRec.Header().Get("Content-Disposition"), "attachment") ||
		dlRec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("expected attachment + sandbox CSP for non-image file, got code=%d cd=%q csp=%q",
			dlRec.Code, dlRec.Header().Get("Content-Disposition"), dlRec.Header().Get("Content-Security-Policy"))
	}

	// Ticket-based download when APIToken is enabled
	authSrv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1", APIToken: "sec-tok"})
	defer authSrv.Shutdown(t.Context())
	authMux := authSrv.srv.Handler

	unauthReq := httptest.NewRequest(http.MethodGet, fileMsg.Media[0].URL, nil)
	unauthReq.Host = "127.0.0.1:3789"
	unauthRec := httptest.NewRecorder()
	authMux.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token or ticket, got %d", unauthRec.Code)
	}

	tktReq := httptest.NewRequest(http.MethodPost, "/api/media/"+fileMsg.Media[0].ID+"/ticket", nil)
	tktReq.Host = "127.0.0.1:3789"
	tktReq.Header.Set("Authorization", "Bearer sec-tok")
	tktRec := httptest.NewRecorder()
	authMux.ServeHTTP(tktRec, tktReq)
	if tktRec.Code != http.StatusOK {
		t.Fatalf("POST ticket failed: %d %s", tktRec.Code, tktRec.Body.String())
	}
	var tktBody struct {
		Ticket string `json:"ticket"`
	}
	_ = json.Unmarshal(tktRec.Body.Bytes(), &tktBody)
	if tktBody.Ticket == "" {
		t.Fatalf("expected non-empty ticket")
	}

	// Valid ticket on matching media ID -> 200
	okTktReq := httptest.NewRequest(http.MethodGet, fileMsg.Media[0].URL+"?ticket="+tktBody.Ticket, nil)
	okTktReq.Host = "127.0.0.1:3789"
	okTktRec := httptest.NewRecorder()
	authMux.ServeHTTP(okTktRec, okTktReq)
	if okTktRec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid ticket, got %d", okTktRec.Code)
	}

	// Ticket bound to fileMsg cannot access another media ID -> 401
	wrongTktReq := httptest.NewRequest(http.MethodGet, sent.Media[0].URL+"?ticket="+tktBody.Ticket, nil)
	wrongTktReq.Host = "127.0.0.1:3789"
	wrongTktRec := httptest.NewRecorder()
	authMux.ServeHTTP(wrongTktRec, wrongTktReq)
	if wrongTktRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for mismatched media ticket, got %d", wrongTktRec.Code)
	}
}

func TestHTTPCommandsMentionsAndAttachmentUpload(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "a")
	_ = os.MkdirAll(ws, 0o755)
	_ = os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("hi"), 0o644)

	store, _ := state.NewStore(filepath.Join(tmp, "sessions.json"))
	msgStore, _ := state.NewMessageStore(filepath.Join(tmp, "messages"))
	defer store.Close()

	reg := harness.NewRegistry()
	reg.Register(&stubHTTPTestHarness{})

	h := hub.New(&config.HubConfig{
		SharedDir:   filepath.Join(tmp, "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "a", Name: "A", Workspace: ws, Harness: "stub"},
			{ID: "b", Name: "B", Workspace: filepath.Join(tmp, "b"), Harness: "stub"},
		},
	}, store, msgStore, reg)
	defer h.Close()

	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1"})

	// 1. GET /api/bots/a/commands
	cmdReq := httptest.NewRequest(http.MethodGet, "/api/bots/a/commands", nil)
	cmdReq.Host = "127.0.0.1:3789"
	cmdRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(cmdRec, cmdReq)
	if cmdRec.Code != http.StatusOK || !strings.Contains(cmdRec.Body.String(), `"status"`) {
		t.Fatalf("GET commands failed: %d %s", cmdRec.Code, cmdRec.Body.String())
	}

	// 2. GET /api/bots/a/mentions?q=hello
	mReq := httptest.NewRequest(http.MethodGet, "/api/bots/a/mentions?q=hello", nil)
	mReq.Host = "127.0.0.1:3789"
	mRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(mRec, mReq)
	if mRec.Code != http.StatusOK || !strings.Contains(mRec.Body.String(), `"hello.txt"`) {
		t.Fatalf("GET mentions failed: %d %s", mRec.Code, mRec.Body.String())
	}

	// 3. POST /api/bots/a/attachments
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "sample.txt")
	_, _ = fw.Write([]byte("attachment body"))
	_ = mw.Close()

	upReq := httptest.NewRequest(http.MethodPost, "/api/bots/a/attachments", &buf)
	upReq.Host = "127.0.0.1:3789"
	upReq.Header.Set("Origin", "http://127.0.0.1:3789")
	upReq.Header.Set("Content-Type", mw.FormDataContentType())
	upRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(upRec, upReq)
	if upRec.Code != http.StatusOK {
		t.Fatalf("POST attachments failed: %d %s", upRec.Code, upRec.Body.String())
	}
	var upResp struct {
		Media state.MessageMedia `json:"media"`
	}
	_ = json.Unmarshal(upRec.Body.Bytes(), &upResp)
	if upResp.Media.ID == "" {
		t.Fatalf("expected media id, got %s", upRec.Body.String())
	}

	// 4. POST /api/bots/a/message with mediaIds only
	msgReq := httptest.NewRequest(http.MethodPost, "/api/bots/a/message", strings.NewReader(fmt.Sprintf(`{"message":"","mediaIds":["%s"]}`, upResp.Media.ID)))
	msgReq.Host = "127.0.0.1:3789"
	msgReq.Header.Set("Origin", "http://127.0.0.1:3789")
	msgRec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(msgRec, msgReq)
	if msgRec.Code != http.StatusOK {
		t.Fatalf("POST message with mediaIds failed: %d %s", msgRec.Code, msgRec.Body.String())
	}
}

func TestHTTPPushEndpoints(t *testing.T) {
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
	reg.Register(noopHarness{kind: "opencode"})
	cfg := &config.HubConfig{
		ProjectRoot: tmp,
		SharedDir:   filepath.Join(tmp, "workspaces", "_shared"),
		StateFile:   filepath.Join(tmp, "sessions.json"),
		MessagesDir: filepath.Join(tmp, "messages"),
		Bots: []config.BotConfig{
			{ID: "assistant", Name: "Pebble", Workspace: filepath.Join(tmp, "assistant"), Harness: "opencode"},
		},
	}
	h := hub.New(cfg, store, msgStore, reg)
	defer h.Close()
	srv := NewHTTPServer(h, config.HTTPConfig{Host: "127.0.0.1", Port: 3789, APIToken: "secret-token"})
	defer srv.Shutdown(t.Context())

	doReq := func(method, path, body string) *httptest.ResponseRecorder {
		var rdr *strings.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		} else {
			rdr = strings.NewReader("")
		}
		req := httptest.NewRequest(method, path, rdr)
		req.Host = "127.0.0.1:3789"
		req.Header.Set("Origin", "http://127.0.0.1:3789")
		req.Header.Set("Authorization", "Bearer secret-token")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := doReq(http.MethodPost, "/api/push/register", `{"deviceId":"ios-1","platform":"ios","env":"sandbox","bundleId":"local.pebble.mobile","pushToken":"pt-1","liveActivityStartToken":"st-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST /api/push/register status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if rec := doReq(http.MethodPost, "/api/push/activity-token", `{"deviceId":"ios-1","activityKey":"bot:assistant","token":"at-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST /api/push/activity-token status = %d, body = %s", rec.Code, rec.Body.String())
	}

	statusRec := doReq(http.MethodGet, "/api/push/status", "")
	if statusRec.Code != http.StatusOK {
		t.Fatalf("GET /api/push/status status = %d, body = %s", statusRec.Code, statusRec.Body.String())
	}
	var st struct {
		Configured  bool   `json:"configured"`
		Mode        string `json:"mode"`
		DeviceCount int    `json:"deviceCount"`
	}
	if err := json.Unmarshal(statusRec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Mode != "disabled" || st.DeviceCount != 1 {
		t.Fatalf("unexpected push status: %+v", st)
	}

	if rec := doReq(http.MethodDelete, "/api/push/devices/ios-1", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/push/devices/ios-1 status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
