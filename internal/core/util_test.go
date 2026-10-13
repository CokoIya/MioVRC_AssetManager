package core

import (
	"strings"
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

// Device names are not usable as file names on Windows, with or without an extension.
func TestSafeNameDevices(t *testing.T) {
	for in, want := range map[string]string{
		"NUL.zip": "_NUL.zip", "CON.unitypackage": "_CON.unitypackage", "COM3": "_COM3", "aux.txt": "_aux.txt",
		"lpt9.tar.gz": "_lpt9.tar.gz", "nul .txt": "_nul .txt", "Console.zip": "Console.zip", "COM10.zip": "COM10.zip",
		"a.": "a", "..": "_", "": "_", "x/../../y.zip": "x／..／..／y.zip", `..\..\y.zip`: "..＼..＼y.zip",
	} {
		if got := SafeName(in, 120); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A name that is too long is cut before its extension.
func TestSafeNameKeepsExtension(t *testing.T) {
	long := strings.Repeat("あ", 125)
	for ext, kept := range map[string]string{
		".zip": ".zip", ".unitypackage": ".unitypackage", ".tar.gz": ".tar.gz", ".part1.rar": ".part1.rar",
		".7z.001": ".7z.001", ".ZIP": ".ZIP", " v1.2.zip": ".zip", "": "",
	} {
		got := SafeName(long+ext, 120)
		if n := len([]rune(got)); n != 120 || !strings.HasSuffix(got, kept) || !strings.HasPrefix(got, "あああ") {
			t.Errorf("SafeName(…%s): %d characters, ends %q", ext, n, got[len(got)-min(len(got), 16):])
		}
	}
	// nothing to keep when the limit is tiny or the name is short enough
	if got := SafeName("abcdefgh.unitypackage", 5); got != "abcde" {
		t.Errorf("tiny limit: %q", got)
	}
	if got := SafeName("Dress. .zip", 9); got != "Dress.zip" {
		t.Errorf("cut at a dot: %q", got)
	}
	if got := SafeName("Kaguya v1.07", 120); got != "Kaguya v1.07" {
		t.Errorf("short name: %q", got)
	}
}

// Whoever calls OpenURL — a route that checked the address, or a page of a built-in browser (闲鱼's, given to the
// default browser) — nothing it hands to Windows can end the address on the command line it is put into.
func TestOpenURLRefusesWhatEndsTheAddress(t *testing.T) {
	for _, u := range []string{"", `https://www.goofish.com/search?q=" --new-window "https://evil.example/`, "https://booth.pm/?q=a b", "https://booth.pm/x\t",
		"https://booth.pm/\r\n--x", "https://booth.pm/\x00", "https://booth.pm/\u3000x", `https://booth.pm\@evil.example/`, `https://booth.pm/x\`} {
		if err := OpenURL(u); err == nil {
			t.Errorf("%q was handed over", u)
		}
	}
	for u, want := range map[string]string{"https://www.goofish.com/item/it's": "https://www.goofish.com/item/it%27s", "mailto:a@b.example?body=it's": "mailto:a@b.example?body=it%27s",
		"unityhub://2022.3.22f1/887be4894c44": "unityhub://2022.3.22f1/887be4894c44", "https://booth.pm/ja/search/%E8%A1%A3": "https://booth.pm/ja/search/%E8%A1%A3"} {
		if got, err := shellAddress(u); err != nil || got != want {
			t.Errorf("%q → %q %v, want %q", u, got, err, want)
		}
	}
}
