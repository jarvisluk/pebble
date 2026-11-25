package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCheckerStableAndETag(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if r.URL.Path != "/repos/jarvisluk/pebble/releases/latest" {
			http.NotFound(w, r)
			return
		}
		if n > 1 && r.Header.Get("If-None-Match") == `"etag-v1"` {
			w.Header().Set("ETag", `"etag-v1"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"etag-v1"`)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v0.3.0",
			"name":         "Pebble v0.3.0",
			"body":         "## Highlights\n- Release notes",
			"html_url":     "https://github.com/jarvisluk/pebble/releases/tag/v0.3.0",
			"published_at": "2026-11-26T00:00:00Z",
		})
	}))
	defer srv.Close()

	c := &Checker{APIBase: srv.URL, CurrentVersion: "0.1.0"}
	info, etag, err := c.Check(context.Background(), ChannelStable, "", "", nil)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if info == nil || info.Version != "0.3.0" || info.Tag != "v0.3.0" {
		t.Fatalf("unexpected release info: %+v", info)
	}
	if etag != `"etag-v1"` {
		t.Fatalf("unexpected etag: %q", etag)
	}

	// Second call hits 304 Not Modified and returns cached info.
	info2, etag2, err := c.Check(context.Background(), ChannelStable, "", etag, info)
	if err != nil {
		t.Fatalf("Check 304 failed: %v", err)
	}
	if info2 == nil || info2.Version != "0.3.0" || etag2 != `"etag-v1"` {
		t.Fatalf("unexpected 304 result: %+v etag=%q", info2, etag2)
	}
}

func TestCheckerPrereleaseAnd404AndRateLimit(t *testing.T) {
	mode := "prerelease"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "404":
			http.NotFound(w, r)
		case "ratelimit":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
		case "prerelease":
			if r.URL.Path != "/repos/jarvisluk/pebble/releases" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"tag_name": "v0.4.0-rc.1", "draft": true, "prerelease": true},
				{"tag_name": "v0.2.0-beta.2", "draft": false, "prerelease": true, "html_url": "https://example/beta2"},
				{"tag_name": "v0.2.0-beta.10", "draft": false, "prerelease": true, "html_url": "https://example/beta10"},
				{"tag_name": "v0.1.0", "draft": false, "prerelease": false},
			})
		}
	}))
	defer srv.Close()

	c := &Checker{APIBase: srv.URL, CurrentVersion: "0.1.0"}
	info, _, err := c.Check(context.Background(), ChannelPrerelease, "", "", nil)
	if err != nil {
		t.Fatalf("prerelease check failed: %v", err)
	}
	if info == nil || info.Version != "0.2.0-beta.10" || !info.Prerelease {
		t.Fatalf("expected v0.2.0-beta.10 (ignoring draft), got %+v", info)
	}

	mode = "404"
	info, _, err = c.Check(context.Background(), ChannelStable, "", "", nil)
	if err != nil || info != nil {
		t.Fatalf("expected (nil, nil) on 404, got info=%+v err=%v", info, err)
	}

	mode = "ratelimit"
	_, _, err = c.Check(context.Background(), ChannelStable, "", "", nil)
	if ErrorCodeOf(err) != ErrCodeRateLimited {
		t.Fatalf("expected ErrCodeRateLimited, got %q (%v)", ErrorCodeOf(err), err)
	}

	if _, _, err := c.Check(context.Background(), ChannelStable, "ftp://bad", "", nil); ErrorCodeOf(err) != ErrCodeInvalidProxy {
		t.Fatalf("expected ErrCodeInvalidProxy, got %q (%v)", ErrorCodeOf(err), err)
	}
}
