package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"bot-hub/internal/protocol"
)

func TestWriteVersion(t *testing.T) {
	origVer, origCommit, origDate := protocol.AppVersion, protocol.BuildCommit, protocol.BuildDate
	t.Cleanup(func() {
		protocol.AppVersion = origVer
		protocol.BuildCommit = origCommit
		protocol.BuildDate = origDate
	})

	protocol.AppVersion = "0.2.0-rc.1"
	protocol.BuildCommit = ""
	protocol.BuildDate = ""

	var buf bytes.Buffer
	if err := writeVersion(&buf, false); err != nil {
		t.Fatalf("writeVersion plain: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "pebble 0.2.0-rc.1" {
		t.Fatalf("plain without metadata = %q, want %q", got, "pebble 0.2.0-rc.1")
	}

	protocol.BuildCommit = "abc1234"
	protocol.BuildDate = "2026-11-26T00:00:00Z"

	buf.Reset()
	if err := writeVersion(&buf, false); err != nil {
		t.Fatalf("writeVersion plain with metadata: %v", err)
	}
	wantPlain := "pebble 0.2.0-rc.1 (abc1234, built 2026-11-26T00:00:00Z)"
	if got := strings.TrimSpace(buf.String()); got != wantPlain {
		t.Fatalf("plain with metadata = %q, want %q", got, wantPlain)
	}

	buf.Reset()
	if err := writeVersion(&buf, true); err != nil {
		t.Fatalf("writeVersion json: %v", err)
	}
	var payload struct {
		Version         string `json:"version"`
		Commit          string `json:"commit"`
		BuildDate       string `json:"buildDate"`
		ProtocolVersion int    `json:"protocolVersion"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if payload.Version != "0.2.0-rc.1" || payload.Commit != "abc1234" ||
		payload.BuildDate != "2026-11-26T00:00:00Z" || payload.ProtocolVersion != protocol.CurrentProtocol {
		t.Fatalf("unexpected json payload: %+v", payload)
	}
}
