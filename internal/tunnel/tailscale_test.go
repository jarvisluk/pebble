package tunnel

import (
	"strings"
	"testing"
)

func TestParseTailscaleLink(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"tailscale://mac-mini.tail123.ts.net:3789", "mac-mini.tail123.ts.net:3789", true},
		{"tailscale://mac-mini.tail123.ts.net", "mac-mini.tail123.ts.net:3789", true},
		{"tailscale://100.64.0.10:8080", "100.64.0.10:8080", true},
		{"tailscale://[fd7a:115c:a1e0::1]:3789", "[fd7a:115c:a1e0::1]:3789", true},
		{"", "", false},
		{"http://100.64.0.10:3789", "", false},
		{"tailscale://", "", false},
		{"tailscale://mac-mini:0", "", false},
		{"tailscale://mac-mini:99999", "", false},
	}
	for _, tc := range cases {
		got, err := ParseTailscaleLink(tc.in)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Errorf("ParseTailscaleLink(%q) = (%q, %v), want (%q, nil)", tc.in, got, err, tc.want)
			}
		} else if err == nil {
			t.Errorf("ParseTailscaleLink(%q) expected error, got %q", tc.in, got)
		}
	}
}

func TestExtractTokenFromTailscaleLink(t *testing.T) {
	clean, tok := ExtractTokenFromLink("tailscale://pebble-mac.ts.net:3789#token=secret%2B1")
	if clean != "tailscale://pebble-mac.ts.net:3789" || tok != "secret+1" {
		t.Fatalf("got clean=%q tok=%q", clean, tok)
	}
}

func TestDefaultTailscaleHostname(t *testing.T) {
	got := DefaultTailscaleHostname("")
	if len(got) == 0 || len(got) > 63 {
		t.Fatalf("unexpected hostname length: %q", got)
	}
	if got != "pebble" && !strings.HasPrefix(got, "pebble-") {
		t.Fatalf("expected default hostname to start with pebble, got %q", got)
	}
}

func TestIsPebbleHostname(t *testing.T) {
	for _, match := range []string{"pebble", "pebble-mac-mini", "bothub", "bothub-mac"} {
		if !isPebbleHostname(match) {
			t.Fatalf("expected isPebbleHostname(%q) = true", match)
		}
	}
	for _, miss := range []string{"", "iphone", "pebblex", "bothubx"} {
		if isPebbleHostname(miss) {
			t.Fatalf("expected isPebbleHostname(%q) = false", miss)
		}
	}
}

func TestSortTailscalePeers(t *testing.T) {
	peers := []TailscalePeer{
		{Hostname: "phone", Online: false, IsBotHub: false},
		{Hostname: "mac-mini", Online: true, IsBotHub: false},
		{Hostname: "pebble-offline", Online: false, IsBotHub: true},
		{Hostname: "pebble-home", Online: true, IsBotHub: true},
		{Hostname: "pebble-alpha", Online: true, IsBotHub: true},
	}
	sortTailscalePeers(peers)
	want := []string{"pebble-alpha", "pebble-home", "mac-mini", "pebble-offline", "phone"}
	for i, w := range want {
		if peers[i].Hostname != w {
			t.Fatalf("idx %d: got %q, want %q", i, peers[i].Hostname, w)
		}
	}
}
