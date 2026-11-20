package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"bot-hub/internal/config"
)

func TestGuardTailcatRequiresToken(t *testing.T) {
	s := &HTTPServer{cfg: config.HTTPConfig{Host: "127.0.0.1", APIToken: "secret"}}
	h := s.guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	do := func(host, token string) int {
		r := httptest.NewRequest("GET", "/api/bots", nil)
		r.Host = host
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r = r.WithContext(context.WithValue(r.Context(), viaTailcatKey, true))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if got := do("phone.local:5555", ""); got != http.StatusUnauthorized {
		t.Fatalf("tunneled without token: %d", got)
	}
	if got := do("phone.local:5555", "secret"); got != http.StatusOK {
		t.Fatalf("tunneled with token (any Host): %d", got)
	}
}
