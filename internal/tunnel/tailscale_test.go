package tunnel

import "testing"

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
	clean, tok := ExtractTokenFromLink("tailscale://bothub-mac.ts.net:3789#token=secret%2B1")
	if clean != "tailscale://bothub-mac.ts.net:3789" || tok != "secret+1" {
		t.Fatalf("got clean=%q tok=%q", clean, tok)
	}
}

func TestDefaultTailscaleHostname(t *testing.T) {
	got := DefaultTailscaleHostname("bothub")
	if len(got) == 0 || len(got) > 63 {
		t.Fatalf("unexpected hostname length: %q", got)
	}
	if got != "bothub" && len(got) < 7 {
		t.Fatalf("unexpected hostname: %q", got)
	}
}

func TestSortTailscalePeers(t *testing.T) {
	peers := []TailscalePeer{
		{Hostname: "phone", Online: false, IsBotHub: false},
		{Hostname: "mac-mini", Online: true, IsBotHub: false},
		{Hostname: "bothub-offline", Online: false, IsBotHub: true},
		{Hostname: "bothub-home", Online: true, IsBotHub: true},
		{Hostname: "bothub-alpha", Online: true, IsBotHub: true},
	}
	sortTailscalePeers(peers)
	want := []string{"bothub-alpha", "bothub-home", "mac-mini", "bothub-offline", "phone"}
	for i, w := range want {
		if peers[i].Hostname != w {
			t.Fatalf("idx %d: got %q, want %q", i, peers[i].Hostname, w)
		}
	}
}
