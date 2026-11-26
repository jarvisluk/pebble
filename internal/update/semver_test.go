package update

import "testing"

func TestParseAndCompareSemVer(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1},
		{"v0.2.0", "0.2.0", 0},
		{"V1.0.0", "0.9.9", 1},
		{"0.2.0-alpha.1", "0.2.0-beta.1", -1},
		{"0.2.0-beta.1", "0.2.0-rc.1", -1},
		{"0.2.0-rc.1", "0.2.0", -1},
		{"0.2.0-beta.2", "0.2.0-beta.10", -1},
		{"0.2.0-1", "0.2.0-alpha", -1},
		{"0.2.0-alpha", "0.2.0-alpha.1", -1},
		{"1.2.3+build.42", "1.2.3", 0},
	}
	for _, tc := range cases {
		va, okA := ParseVersion(tc.a)
		vb, okB := ParseVersion(tc.b)
		if !okA || !okB {
			t.Fatalf("expected valid parse for %q and %q (okA=%v okB=%v)", tc.a, tc.b, okA, okB)
		}
		if got := Compare(va, vb); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("v0.2.0", "0.1.0") {
		t.Fatal("expected v0.2.0 > 0.1.0")
	}
	if IsNewer("0.1.0", "0.1.0") {
		t.Fatal("expected 0.1.0 == 0.1.0 not newer")
	}
	if IsNewer("0.2.0-beta.1", "0.2.0") {
		t.Fatal("expected prerelease not newer than stable")
	}
	for _, invalid := range []string{"", "dev", "unknown", "0.1", "01.0.0", "1.0.0-"} {
		if IsNewer("1.0.0", invalid) {
			t.Errorf("expected IsNewer(1.0.0, %q) to be false", invalid)
		}
		if IsNewer(invalid, "0.1.0") {
			t.Errorf("expected IsNewer(%q, 0.1.0) to be false", invalid)
		}
	}
}
