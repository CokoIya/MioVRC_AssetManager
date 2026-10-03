package semver

import (
	"testing"
)

func TestSemverRanges(t *testing.T) {
	for _, c := range []struct {
		rng, ver string
		want     bool
	}{
		{"3.10.5", "3.10.5", true}, {"3.10.5", "3.10.6", false},
		{"3.1.x", "3.1.11", true}, {"3.1.x", "3.2.0", false},
		{"3.x.x", "3.10.5", true}, {"3.x.x", "4.0.0", false},
		{"^1.14", "1.14.8", true}, {"^1.14", "1.20.0", true}, {"^1.14", "2.0.0", false}, {"^1.14", "1.13.9", false},
		{"^0.1.3", "0.1.9", true}, {"^0.1.3", "0.2.0", false},
		{"~3.2.0", "3.2.9", true}, {"~3.2.0", "3.3.0", false},
		{">=1.8.0 <2.0.0", "1.14.8", true}, {">=1.8.0 <2.0.0", "2.0.0", false}, {">=1.8.0 <2.0.0", "1.7.9", false},
		{">=1.14.7 <2.0.0-a", "1.14.8", true}, {">=1.14.7 <2.0.0-a", "2.0.0", false}, {">=1.14.7 <2.0.0-a", "1.14.7", true},
		{">=1.8.0-alpha.3 <2.0.0", "1.8.0", true},
		{">=3.10.4 < 3.11.X", "3.10.5", true}, {">=3.10.4 < 3.11.X", "3.11.0", false}, {">=3.10.4 < 3.11.X", "3.10.3", false},
		{"3.2 - 3.10", "3.10.5", true}, {"3.2 - 3.10", "3.11.0", false}, {"3.2 - 3.10", "3.1.9", false},
		{"3.2.x || 3.3.x", "3.3.1", true}, {"3.2.x || 3.3.x", "3.4.0", false},
		{"^1.10.0 || ^2.0.0", "2.3.0", true}, {"^1.10.0 || ^2.0.0", "3.0.0", false},
		{"^3.1.x", "3.9.0", true}, {"*", "9.9.9", true}, {">=3.7.4", "3.10.5", true}, {">=3.7.4", "3.7.3", false},
		{"<=3.11.x", "3.11.9", true}, {"<=3.11.x", "3.12.0", false},
	} {
		r, err := ParseRange(c.rng)
		if err != nil {
			t.Errorf("parseRange(%q): %v", c.rng, err)
			continue
		}
		v, ok := ParseSemver(c.ver)
		if !ok {
			t.Fatalf("parseSemver(%q)", c.ver)
		}
		if got := r.Match(v); got != c.want {
			t.Errorf("%q matches %q = %v, want %v", c.rng, c.ver, got, c.want)
		}
	}
	for _, c := range [][2]string{{"1.0.0-alpha", "1.0.0"}, {"1.0.0-alpha.1", "1.0.0-alpha.2"}, {"1.0.0-alpha.9", "1.0.0-beta"}, {"1.9.20-beta.2", "1.9.20"}, {"3.9.9", "3.10.0"}} {
		a, _ := ParseSemver(c[0])
		b, _ := ParseSemver(c[1])
		if a.Cmp(b) >= 0 || b.Cmp(a) <= 0 {
			t.Errorf("%s < %s", c[0], c[1])
		}
	}
	if _, err := ParseRange("what"); err == nil {
		t.Error("nonsense accepted")
	}
}
