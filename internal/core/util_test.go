package core

import (
	"testing"
)

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"9000003 【8アバター対応】Moon Dress": "9000003 【8アバター対応】Moon Dress",
		"a/b:c*d?": "a／b：c＊d？",
		"name. ":   "name",
		"CON":      "_CON",
	} {
		if got := SafeName(in, 70); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
