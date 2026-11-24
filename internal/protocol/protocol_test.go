package protocol

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestNormalizeProtocolAndParseClientMeta(t *testing.T) {
	if got := NormalizeProtocol(0); got != 1 {
		t.Fatalf("NormalizeProtocol(0) = %d, want 1", got)
	}
	if got := NormalizeProtocol(-3); got != 1 {
		t.Fatalf("NormalizeProtocol(-3) = %d, want 1", got)
	}
	if got := NormalizeProtocol(2); got != 2 {
		t.Fatalf("NormalizeProtocol(2) = %d, want 2", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/bots", nil)
	if meta := ParseClientMeta(req); meta.ProtocolVersion != 1 {
		t.Fatalf("expected legacy default protocol 1, got %+v", meta)
	}

	req.Header.Set(HeaderProtocol, "2")
	req.Header.Set(HeaderClient, "apple")
	req.Header.Set(HeaderVersion, "1.0.0")
	meta := ParseClientMeta(req)
	if meta.ProtocolVersion != 2 || meta.Client != "apple" || meta.Version != "1.0.0" {
		t.Fatalf("unexpected parsed meta: %+v", meta)
	}

	qReq := httptest.NewRequest(http.MethodGet, "/api/desktop/stream/ws?proto=2", nil)
	if qMeta := ParseClientMeta(qReq); qMeta.ProtocolVersion != 2 {
		t.Fatalf("expected query proto=2, got %+v", qMeta)
	}

	badReq := httptest.NewRequest(http.MethodGet, "/api/bots", nil)
	badReq.Header.Set(HeaderProtocol, "not-a-number")
	if bMeta := ParseClientMeta(badReq); bMeta.ProtocolVersion != 1 {
		t.Fatalf("expected invalid header fallback to 1, got %+v", bMeta)
	}
}

func TestCapabilityNormalizationAndBuilders(t *testing.T) {
	norm := NormalizeCapabilities([]string{" b ", "", "a", "b", "c"})
	if !slices.Equal(norm, []string{"a", "b", "c"}) {
		t.Fatalf("unexpected NormalizeCapabilities: %v", norm)
	}
	if !HasCapability(norm, "b") || HasCapability(norm, "d") || HasCapability(norm, "") {
		t.Fatalf("unexpected HasCapability results for %v", norm)
	}

	noVirt := ServerCapabilities(false)
	if HasCapability(noVirt, CapDesktopVirtualSlots) {
		t.Fatalf("did not expect virtual slots capability when disabled: %v", noVirt)
	}
	if !HasCapability(noVirt, CapBotDesktopBinding) || !HasCapability(noVirt, CapDesktopWSHEVC) {
		t.Fatalf("expected core server capabilities in %v", noVirt)
	}

	withVirt := ServerCapabilities(true)
	if !HasCapability(withVirt, CapDesktopVirtualSlots) {
		t.Fatalf("expected virtual slots capability when enabled: %v", withVirt)
	}

	daemonCaps := DaemonCapabilities()
	for _, want := range []string{CapDaemonFailover, CapDaemonLocalNode, CapDaemonTailscale} {
		if !HasCapability(daemonCaps, want) {
			t.Fatalf("missing daemon capability %q in %v", want, daemonCaps)
		}
	}
}

func TestWriteErrorPayload(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", &UpgradeError{
		Code:               ErrCodeNodeUpgradeRequired,
		Message:            "upgrade node",
		RequiredCapability: CapNodeVirtualSlots,
		MinProtocol:        CurrentProtocol,
	})
	payload := WriteErrorPayload(wrapped)
	if payload["error"] != "upgrade node" || payload["code"] != ErrCodeNodeUpgradeRequired ||
		payload["requiredCapability"] != CapNodeVirtualSlots || payload["minProtocol"] != CurrentProtocol {
		t.Fatalf("unexpected error payload: %+v", payload)
	}
}
