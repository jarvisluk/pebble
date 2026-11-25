package channel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"bot-hub/internal/config"
	"bot-hub/internal/protocol"
	"bot-hub/internal/update"
)

func TestUpdateRoutesAndCapability(t *testing.T) {
	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/jarvisluk/pebble/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":     "v9.1.0",
				"name":         "Pebble v9.1.0",
				"body":         "- Added update notifications",
				"html_url":     "https://github.com/jarvisluk/pebble/releases/tag/v9.1.0",
				"published_at": "2026-11-26T00:00:00Z",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ghSrv.Close()

	stateDir := t.TempDir()
	mgr := update.NewManager(update.Options{
		StateDir:       stateDir,
		CurrentVersion: "0.1.0",
		APIBase:        ghSrv.URL,
	})
	s := NewHTTPServer(nil, config.HTTPConfig{Host: "127.0.0.1", APIToken: "tok-1"})
	s.SetUpdater(mgr)
	defer s.Shutdown(t.Context())
	mux := s.srv.Handler

	do := func(method, path, body, auth string) *httptest.ResponseRecorder {
		var rBody *bytes.Buffer
		if body != "" {
			rBody = bytes.NewBufferString(body)
		} else {
			rBody = &bytes.Buffer{}
		}
		req := httptest.NewRequest(method, path, rBody)
		req.Host = "127.0.0.1:3789"
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Auth enforced on update endpoints.
	if rec := do(http.MethodGet, "/api/update", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	// /api/meta includes update.check capability.
	metaRec := do(http.MethodGet, "/api/meta", "", "Bearer tok-1")
	if metaRec.Code != http.StatusOK {
		t.Fatalf("GET /api/meta status = %d", metaRec.Code)
	}
	var info protocol.ServerInfo
	if err := json.Unmarshal(metaRec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if !slices.Contains(info.Capabilities, protocol.CapUpdateCheck) {
		t.Fatalf("expected capability %q in %v", protocol.CapUpdateCheck, info.Capabilities)
	}

	// POST /api/update/check detects newer release.
	chkRec := do(http.MethodPost, "/api/update/check", `{}`, "Bearer tok-1")
	if chkRec.Code != http.StatusOK {
		t.Fatalf("POST /api/update/check failed: %d %s", chkRec.Code, chkRec.Body.String())
	}
	var st update.Status
	if err := json.Unmarshal(chkRec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !st.HasUpdate || st.State != update.StateAvailable || st.Latest == nil || st.Latest.Version != "9.1.0" {
		t.Fatalf("unexpected check status: %+v", st)
	}

	// GET /api/update returns cached status.
	getRec := do(http.MethodGet, "/api/update", "", "Bearer tok-1")
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/update failed: %d", getRec.Code)
	}
	var st2 update.Status
	_ = json.Unmarshal(getRec.Body.Bytes(), &st2)
	if !st2.HasUpdate || st2.Latest == nil || st2.Latest.Version != "9.1.0" {
		t.Fatalf("unexpected GET /api/update status: %+v", st2)
	}

	// PATCH /api/update/config validates and persists config.
	if badRec := do(http.MethodPatch, "/api/update/config", `{"interval":"2m"}`, "Bearer tok-1"); badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad interval, got %d", badRec.Code)
	}
	cfgRec := do(http.MethodPatch, "/api/update/config", `{"autoCheck":false,"interval":"24h","channel":"prerelease"}`, "Bearer tok-1")
	if cfgRec.Code != http.StatusOK {
		t.Fatalf("PATCH /api/update/config failed: %d %s", cfgRec.Code, cfgRec.Body.String())
	}
	var st3 update.Status
	_ = json.Unmarshal(cfgRec.Body.Bytes(), &st3)
	if st3.Config.AutoCheck || st3.Config.Interval != "24h" || st3.Config.Channel != update.ChannelPrerelease {
		t.Fatalf("unexpected updated config: %+v", st3.Config)
	}
}
