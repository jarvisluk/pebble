package desktop

import (
	"context"
	"testing"
	"time"
)

func TestEnginePermissionsListAndUnknownRequest(t *testing.T) {
	eng := NewEngine()
	defer eng.Close()

	eng.permMu.Lock()
	eng.srDenied = true
	eng.srChecked = time.Now()
	eng.permMu.Unlock()

	resp, err := eng.Permissions(context.Background())
	if err != nil {
		t.Fatalf("Permissions() error = %v", err)
	}
	if resp == nil || len(resp.Items) == 0 {
		t.Fatalf("expected non-empty permissions list, got %+v", resp)
	}
	eng.permMu.Lock()
	cleared := !eng.srDenied && eng.srChecked.IsZero()
	eng.permMu.Unlock()
	if !cleared {
		t.Fatalf("expected Permissions() to clear cached srDenied/srChecked")
	}

	var hasSR bool
	for _, it := range resp.Items {
		if it.ID == "screen_recording" {
			hasSR = true
		}
		switch it.Status {
		case "granted", "denied", "not_determined", "unsupported":
		default:
			t.Fatalf("unexpected permission status %q for %q", it.Status, it.ID)
		}
	}
	if !hasSR {
		t.Fatalf("expected screen_recording in permissions list")
	}

	if _, err := eng.RequestPermission(context.Background(), "nonexistent_perm", false); err == nil {
		t.Fatalf("expected error for unknown permission id")
	}
}
