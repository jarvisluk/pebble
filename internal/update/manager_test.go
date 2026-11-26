package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestManagerCheckConfigAndPersistence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/jarvisluk/pebble/releases/latest" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":     "v0.5.0",
				"name":         "v0.5.0",
				"body":         "New release body",
				"html_url":     "https://github.com/jarvisluk/pebble/releases/tag/v0.5.0",
				"published_at": "2026-11-26T01:00:00Z",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	stateDir := t.TempDir()
	var mu sync.Mutex
	var events []Status
	mgr := NewManager(Options{
		StateDir:       stateDir,
		CurrentVersion: "0.1.0",
		APIBase:        srv.URL,
		InitialDelay:   10 * time.Millisecond,
		OnChange: func(s Status) {
			mu.Lock()
			events = append(events, s)
			mu.Unlock()
		},
	})

	st := mgr.CheckNow(context.Background(), "", "")
	if st.State != StateAvailable || !st.HasUpdate || st.Latest == nil || st.Latest.Version != "0.5.0" {
		t.Fatalf("unexpected check status: %+v", st)
	}

	autoOff := false
	iv1h := "1h"
	updated, err := mgr.UpdateConfig(ConfigPatch{AutoCheck: &autoOff, Interval: &iv1h})
	if err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}
	if updated.Config.AutoCheck || updated.Config.Interval != "1h" {
		t.Fatalf("unexpected updated config: %+v", updated.Config)
	}

	badIv := "5m"
	if _, err := mgr.UpdateConfig(ConfigPatch{Interval: &badIv}); err == nil {
		t.Fatal("expected error for invalid interval 5m")
	}

	// Reload from disk and verify config + cached latest persisted.
	mgr2 := NewManager(Options{
		StateDir:       stateDir,
		CurrentVersion: "0.1.0",
		APIBase:        srv.URL,
	})
	st2 := mgr2.Status()
	if st2.Config.AutoCheck || st2.Config.Interval != "1h" {
		t.Fatalf("persisted config mismatch: %+v", st2.Config)
	}
	if !st2.HasUpdate || st2.Latest == nil || st2.Latest.Version != "0.5.0" {
		t.Fatalf("persisted latest mismatch: %+v", st2)
	}

	mu.Lock()
	evCount := len(events)
	mu.Unlock()
	if evCount < 2 {
		t.Fatalf("expected OnChange callbacks, got %d", evCount)
	}
}

func TestManagerBackgroundStartAndStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v0.2.0",
			"html_url": "https://github.com/jarvisluk/pebble/releases/tag/v0.2.0",
		})
	}))
	defer srv.Close()

	done := make(chan Status, 4)
	mgr := NewManager(Options{
		StateDir:       t.TempDir(),
		CurrentVersion: "0.1.0",
		APIBase:        srv.URL,
		InitialDelay:   10 * time.Millisecond,
		OnChange: func(s Status) {
			if s.State == StateAvailable {
				select {
				case done <- s:
				default:
				}
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	mgr.Start(ctx)
	select {
	case s := <-done:
		if s.Latest == nil || s.Latest.Version != "0.2.0" {
			t.Fatalf("unexpected background status: %+v", s)
		}
	case <-time.After(2 * time.Second):
		cancel()
		mgr.Wait()
		t.Fatal("timed out waiting for background update check")
	}
	cancel()
	mgr.Wait()
}
