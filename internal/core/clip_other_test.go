//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// (the stand-in for the clipboard outside Windows: a file)
func TestClipboardStandIn(t *testing.T) {
	t.Setenv("VRCLIB_CLIP_FILE", "")
	if ClipboardSeq() != 0 {
		t.Error("a number without a clipboard")
	}
	if _, ok, _ := ClipboardText(); ok {
		t.Error("text without a clipboard")
	}
	f := filepath.Join(t.TempDir(), "clip.txt")
	t.Setenv("VRCLIB_CLIP_FILE", f)
	_ = os.WriteFile(f, []byte("one"), 0644)
	a := ClipboardSeq()
	if s, ok, _ := ClipboardText(); a == 0 || !ok || s != "one" {
		t.Errorf("read %q %v (number %d)", s, ok, a)
	}
	_ = os.WriteFile(f, []byte("two"), 0644)
	if b := ClipboardSeq(); b == 0 || b == a {
		t.Errorf("the number did not move: %d → %d", a, b)
	}
	_ = os.WriteFile(f, []byte(strings.Repeat("x", ClipMaxChars+1)), 0644)
	if _, ok, _ := ClipboardText(); ok {
		t.Error("a long text was read")
	}
	_ = os.WriteFile(f+".busy", nil, 0644)
	if _, ok, busy := ClipboardText(); ok || !busy {
		t.Error("a clipboard held by another program was read")
	}
}
