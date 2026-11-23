package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func generateTestP8PEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestAPNsSenderHeadersJWTAnd410(t *testing.T) {
	var (
		gotPath     string
		gotAuth     string
		gotPushType string
		gotTopic    string
		gotCollapse string
		gotPayload  map[string]any
		respStatus  = http.StatusOK
		respBody    = ""
	)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotPushType = r.Header.Get("apns-push-type")
		gotTopic = r.Header.Get("apns-topic")
		gotCollapse = r.Header.Get("apns-collapse-id")
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(respStatus)
		if respBody != "" {
			_, _ = w.Write([]byte(respBody))
		}
	}))
	defer srv.Close()

	sender, err := NewAPNsSender(APNsSenderOptions{
		KeyPEM:         generateTestP8PEM(t),
		KeyID:          "KEY1234567",
		TeamID:         "TEAM123456",
		DefaultBundle:  "local.bothub.mobile",
		BaseURLProd:    srv.URL,
		BaseURLSandbox: srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewAPNsSender: %v", err)
	}

	err = sender.Send(context.Background(), SendRequest{
		Platform:   "ios",
		Env:        "sandbox",
		PushType:   PushTypeLiveActivity,
		Token:      "deadbeef",
		Priority:   10,
		CollapseID: "bot-alpha",
		Payload: APNSPayload{
			APS: APSBody{
				Event: "update",
				ContentState: &LiveActivityContentState{
					Phase:      "running",
					StatusText: "正在运行 bash",
					StepCount:  2,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/3/device/deadbeef" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "bearer ") || strings.Count(gotAuth, ".") != 2 {
		t.Fatalf("unexpected JWT auth header: %s", gotAuth)
	}
	if gotPushType != "liveactivity" {
		t.Fatalf("unexpected push-type: %s", gotPushType)
	}
	if gotTopic != "local.bothub.mobile.push-type.liveactivity" {
		t.Fatalf("unexpected topic: %s", gotTopic)
	}
	if gotCollapse != "bot-alpha" {
		t.Fatalf("unexpected collapse-id: %s", gotCollapse)
	}

	// Verify 410 / BadDeviceToken maps to ErrInvalidToken.
	respStatus = http.StatusGone
	respBody = `{"reason":"Unregistered"}`
	err = sender.Send(context.Background(), SendRequest{
		Platform: "ios",
		Env:      "production",
		PushType: PushTypeAlert,
		Token:    "expiredtoken",
		Payload:  APNSPayload{APS: APSBody{Alert: &APSAlert{Title: "hi"}}},
	})
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}
