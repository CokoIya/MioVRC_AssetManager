//go:build !windows

package core

import (
	"hash/crc32"
	"os"
	"strings"
)

// Outside Windows (development and tests) the "clipboard" is the file VRCLIB_CLIP_FILE names: what it holds is
// the text, and its number changes with it. A file of that name with ".busy" after it: another program is
// holding the clipboard; with ".back": this program's window is not the one in front.

func clipFile() ([]byte, bool) {
	name := os.Getenv("VRCLIB_CLIP_FILE")
	if name == "" {
		return nil, false
	}
	b, err := os.ReadFile(name)
	return b, err == nil
}

func clipFlag(ext string) bool {
	name := os.Getenv("VRCLIB_CLIP_FILE")
	if name == "" {
		return false
	}
	_, err := os.Stat(name + ext)
	return err == nil
}

func AppInFront() bool { return !clipFlag(".back") }

func Elevated() bool { return false }

func ClipboardSeq() uint32 {
	b, ok := clipFile()
	if !ok {
		return 0
	}
	return crc32.ChecksumIEEE(b) | 1
}

func ClipboardText() (text string, ok, busy bool) {
	if clipFlag(".busy") {
		return "", false, true
	}
	b, ok := clipFile()
	if !ok || len([]rune(string(b))) > ClipMaxChars {
		return "", false, false
	}
	return strings.TrimSuffix(string(b), "\x00"), true, false
}
