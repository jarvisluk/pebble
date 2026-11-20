package tunnel

import "testing"

func TestParseLinkRejects(t *testing.T) {
	for _, in := range []string{"", "http://connect/tcx?port=1", "tailcat://connect/tcbad?port=3789", "tailcat://connect/x?port=0", "tailcat://other/x?port=1"} {
		if _, _, err := ParseLink(in); err == nil {
			t.Errorf("ParseLink(%q) accepted", in)
		}
	}
}
