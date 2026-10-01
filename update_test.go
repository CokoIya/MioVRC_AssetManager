package main

import "testing"

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.4.1", "1.4.0", true}, {"1.4.0", "1.4.0", false}, {"v1.10.0", "1.9.9", true}, {"1.3", "1.3.1", false},
		{"VRC素材库 2.0", "1.9", true}, {"release", "1.0", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("%s > %s: got %v", c.a, c.b, got)
		}
	}
}
