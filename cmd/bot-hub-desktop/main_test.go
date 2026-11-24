package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bot-hub/internal/desktophost"
)

func newTestDesktop(t *testing.T) *desktop {
	t.Helper()
	dir := t.TempDir()
	h := desktophost.New(dir, desktophost.Hooks{})
	h.Load()
	return &desktop{dir: dir, host: h}
}

func TestServeSetupAssetsAndAPI(t *testing.T) {
	d := newTestDesktop(t)

	reqPage := httptest.NewRequest(http.MethodGet, "/desktop-setup", nil)
	recPage := httptest.NewRecorder()
	d.serveSetup(recPage, reqPage)
	if recPage.Code != http.StatusOK {
		t.Fatalf("GET /desktop-setup status = %d, want 200", recPage.Code)
	}
	if ct := recPage.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET /desktop-setup Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(recPage.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("GET /desktop-setup expected React index.html root mount")
	}

	reqAPI := httptest.NewRequest(http.MethodGet, "/api/desktop", nil)
	recAPI := httptest.NewRecorder()
	d.serveSetup(recAPI, reqAPI)
	if recAPI.Code != http.StatusOK {
		t.Fatalf("GET /api/desktop status = %d, want 200", recAPI.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recAPI.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /api/desktop json: %v", err)
	}
	if body["dataDir"] != d.dir {
		t.Fatalf("dataDir = %v, want %q", body["dataDir"], d.dir)
	}

	reqTS := httptest.NewRequest(http.MethodGet, "/api/desktop/tailscale", nil)
	recTS := httptest.NewRecorder()
	d.serveSetup(recTS, reqTS)
	if recTS.Code != http.StatusOK {
		t.Fatalf("GET /api/desktop/tailscale status = %d, want 200", recTS.Code)
	}
	if !strings.Contains(recTS.Body.String(), `"state":"disabled"`) {
		t.Fatalf("unexpected tailscale status body: %s", recTS.Body.String())
	}
}
