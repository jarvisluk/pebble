package main

import (
	"bytes"
	"encoding/json"
	"image/png"
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

func TestTrayIconAssets(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		size int
	}{
		{name: "tray_mac_template.png", data: trayMacTemplateIcon, size: 44},
		{name: "tray_color.png", data: trayColorIcon, size: 64},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img, err := png.Decode(bytes.NewReader(tc.data))
			if err != nil {
				t.Fatalf("decode %s: %v", tc.name, err)
			}
			b := img.Bounds()
			if b.Dx() != tc.size || b.Dy() != tc.size {
				t.Fatalf("%s bounds = %dx%d, want %dx%d", tc.name, b.Dx(), b.Dy(), tc.size, tc.size)
			}
			// Corners must be transparent.
			for _, pt := range [][2]int{{0, 0}, {tc.size - 1, 0}, {0, tc.size - 1}, {tc.size - 1, tc.size - 1}} {
				_, _, _, a := img.At(pt[0], pt[1]).RGBA()
				if a != 0 {
					t.Fatalf("%s corner (%d,%d) alpha = %d, want 0", tc.name, pt[0], pt[1], a)
				}
			}
			// Interior must contain opaque/non-transparent pixels.
			nonTransparent := 0
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					_, _, _, a := img.At(x, y).RGBA()
					if a > 0 {
						nonTransparent++
					}
				}
			}
			if nonTransparent == 0 {
				t.Fatalf("%s has no non-transparent pixels", tc.name)
			}
		})
	}
}
