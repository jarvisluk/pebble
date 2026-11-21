package tunnel

import "testing"

func TestParseLinkRejects(t *testing.T) {
	for _, in := range []string{"", "http://connect/tcx?port=1", "tailcat://connect/tcbad?port=3789", "tailcat://connect/x?port=0", "tailcat://other/x?port=1"} {
		if _, _, err := ParseLink(in); err == nil {
			t.Errorf("ParseLink(%q) accepted", in)
		}
	}
}

func TestExtractTokenFromLink(t *testing.T) {
	clean, tok := ExtractTokenFromLink("  tailcat://connect/tc123?port=3789#token=abc%2B123  ")
	if clean != "tailcat://connect/tc123?port=3789" || tok != "abc+123" {
		t.Fatalf("got clean=%q tok=%q", clean, tok)
	}
	clean2, tok2 := ExtractTokenFromLink("tailcat://connect/tc123?port=3789")
	if clean2 != "tailcat://connect/tc123?port=3789" || tok2 != "" {
		t.Fatalf("without fragment got clean=%q tok=%q", clean2, tok2)
	}
}
