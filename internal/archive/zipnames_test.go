package archive

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// The byte groups of the names used below in three Windows code pages (Shift-JIS, GBK, Thai), as Windows
// converts them. Outside Windows there is no conversion, so the tests bring this small one.
var testCodePages = map[uint32]map[string]rune{
	932: {"\x82\xd3": 0x3075, "\x82\xed": 0x308f, "\x83\x4e": 0x30af, "\x83\x58": 0x30b9, "\x83\x60": 0x30c1, "\x83\x65": 0x30c6, "\x83\x83": 0x30e3, "\x90\x4b": 0x5c3b, "\x94\xf6": 0x5c3e, "\xab": 0xff6b, "\xb0": 0xff70, "\xb2": 0xff72, "\xb7": 0xff77, "\xb8": 0xff78, "\xba": 0xff7a, "\xbc": 0xff7c, "\xbd": 0xff7d, "\xc1": 0xff81, "\xc2": 0xff82, "\xc3": 0xff83, "\xc9": 0xff89, "\xcc": 0xff8c, "\xd2": 0xff92, "\xd4": 0xff94, "\xd7": 0xff97, "\xda": 0xff9a, "\xe0\xc5": 0x500f, "\xe2\xca": 0x7c17, "\xf9\xcd": 0xe728, "\xfe": 0xf8f2},
	936: {"\x82\xd3": 0x5086, "\x82\xed": 0x50a2, "\x83\x4e": 0x50cb, "\x83\x58": 0x50d7, "\x83\x60": 0x50e0, "\x83\x65": 0x50e5, "\x83\x83": 0x510d, "\x90\x4b": 0x601f, "\x94\xf6": 0x65dc, "\xb0\xd7": 0x767d, "\xb2\xe2": 0x6d4b, "\xb7\xfe": 0x670d, "\xb8\xfc": 0x66f4, "\xba\xda": 0x9ed1, "\xbd\xc1": 0x6405, "\xc2\xb7": 0x8def, "\xc2\xeb": 0x7801, "\xc3\xb8": 0x9176, "\xc3\xdc": 0x5bc6, "\xc3\xf7": 0x660e, "\xc9\xab": 0x8272, "\xca\xd4": 0x8bd5, "\xcb\xb5": 0x8bf4, "\xcc\xf9": 0x8d34, "\xcd\xbc": 0x56fe, "\xd0\xc2": 0x65b0, "\xd2\xc2": 0x8863, "\xe0\xc5": 0x55ef},
	874: {"\x94": 0x201d, "\xab": 0x0e0b, "\xb0": 0x0e10, "\xb2": 0x0e12, "\xb5": 0x0e15, "\xb7": 0x0e17, "\xb8": 0x0e18, "\xba": 0x0e1a, "\xbd": 0x0e1d, "\xc1": 0x0e21, "\xc2": 0x0e22, "\xc3": 0x0e23, "\xc5": 0x0e25, "\xc9": 0x0e29, "\xca": 0x0e2a, "\xcb": 0x0e2b, "\xd4": 0x0e34, "\xd7": 0x0e37, "\xda": 0x0e3a, "\xe0": 0x0e40, "\xe2": 0x0e42, "\xe4": 0x0e44, "\xf6": 0x0e56, "\xf7": 0x0e57},
}

// File names as zips made on a Chinese, a Japanese and a Thai Windows store them.
const (
	gbkReadme   = "\xcb\xb5\xc3\xf7.txt"                                                     // 说明.txt
	gbkPassword = "\xc3\xdc\xc2\xeb.txt"                                                     // 密码.txt
	gbkUpdate   = "\xb8\xfc\xd0\xc2.txt"                                                     // 更新.txt
	gbkBlack    = "\xba\xda\xc9\xab.png"                                                     // 黑色.png: valid Shift-JIS too (ｺﾚﾉｫ.png)
	gbkWhite    = "\xb0\xd7\xc9\xab.png"                                                     // 白色.png: valid Shift-JIS too (ｰﾗﾉｫ.png)
	gbkTest     = "\xb2\xe2\xca\xd4"                                                         // 测试: valid Shift-JIS too
	gbkRoad     = "\xc2\xb7.txt"                                                             // 路.txt: valid UTF-8 too (·.txt)
	gbkNested   = "\xcc\xf9\xcd\xbc/\xd2\xc2\xb7\xfe.png"                                    // 贴图/衣服.png
	sjisTail    = "\x82\xd3\x82\xed\x82\xd3\x82\xed\x90K\x94\xf6/\x90K\x94\xf6.unitypackage" // ふわふわ尻尾/尻尾.unitypackage
	sjisKanji   = "\x90K\x94\xf6.png"                                                        // 尻尾.png: valid GBK too (rare characters)
	sjisKana    = "\x83e\x83N\x83X\x83`\x83\x83.png"                                         // テクスチャ.png
	sjisHalf    = "\xc3\xb8\xbd\xc1.png"                                                     // ﾃｸｽﾁ.png: as GBK, two everyday characters
	sjisEither  = "\xe0\xc5\x94\xf6.png"                                                     // 倏尾.png: as GBK one everyday and one rare character
	thaiName    = "\xe4.txt"                                                                 // ไ.txt: neither Shift-JIS nor GBK
	badName     = "\xff\xfe.txt"                                                             // no code page at all
)

// fakeCodePages puts the test conversion in, on a system whose own code page is ansi.
func fakeCodePages(t *testing.T, ansi uint32) {
	t.Helper()
	oldDecode, oldANSI := decodeCP, ansiCP
	t.Cleanup(func() { decodeCP, ansiCP = oldDecode, oldANSI })
	ansiCP = func() uint32 { return ansi }
	decodeCP = func(cp uint32, b []byte) (string, bool) {
		table := testCodePages[cp]
		var sb strings.Builder
		for i := 0; i < len(b); {
			if b[i] < 0x80 {
				sb.WriteByte(b[i])
				i++
			} else if r, ok := table[string(b[i:i+1])]; ok {
				sb.WriteRune(r)
				i++
			} else if r, ok := table[string(b[i:min(i+2, len(b))])]; ok {
				sb.WriteRune(r)
				i += 2
			} else {
				return "", false // strict, as MB_ERR_INVALID_CHARS is
			}
		}
		return sb.String(), true
	}
}

// legacyZip: a zip whose names are stored as given, without the UTF-8 flag.
func legacyZip(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, NonUTF8: true, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(n))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipFiles(t *testing.T, b []byte) []*zip.File {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	return zr.File
}

// The code page is decided once for the archive: by which one reads every name, then by what the text
// looks like, then by the system's own.
func TestZipCodePage(t *testing.T) {
	cases := []struct {
		what  string
		ansi  uint32
		names []string
		want  uint32
		fails bool
	}{
		{"plain names", 936, []string{"Dress/readme.txt", "a~b.png"}, 0, false},
		{"UTF-8 without the flag", 936, []string{"日本語/テクスチャ.png"}, cpUTF8, false},
		{"Chinese, some of it not valid Shift-JIS", 932, []string{gbkReadme, gbkPassword, gbkUpdate, gbkBlack, gbkWhite}, cpGBK, false},
		{"Chinese that is valid Shift-JIS, on a Japanese system", 932, []string{gbkTest, gbkBlack}, cpGBK, false},
		{"Chinese, one name of it valid UTF-8", 936, []string{gbkRoad, gbkReadme}, cpGBK, false},
		{"Japanese with kana, on a Chinese system", 936, []string{sjisTail}, cpSJIS, false},
		{"Japanese in kanji only", 936, []string{sjisKanji}, cpSJIS, false},
		{"Japanese in katakana", 936, []string{sjisKana, "readme.txt"}, cpSJIS, false},
		{"half-width katakana alone is not Japanese", 932, []string{sjisHalf}, cpGBK, false},
		{"half-width katakana next to full-width", 936, []string{sjisHalf, sjisKana}, cpSJIS, false},
		{"nothing to tell them apart, on a Japanese system", 932, []string{sjisEither}, cpSJIS, false},
		{"nothing to tell them apart, on a Chinese system", 936, []string{sjisEither}, cpGBK, false},
		{"nothing to tell them apart, elsewhere", 1252, []string{sjisEither}, cpGBK, false},
		{"neither, on a system that reads it", 874, []string{thaiName}, 874, false},
		{"neither, on a Chinese system", 936, []string{thaiName}, 0, true},
		{"no code page at all", 936, []string{gbkReadme, badName}, 0, true},
	}
	for _, c := range cases {
		fakeCodePages(t, c.ansi)
		cp, ok := zipCodePage(zipFiles(t, legacyZip(t, c.names...)))
		if cp != c.want || ok == c.fails {
			t.Errorf("%s: code page %d (ok %v), want %d", c.what, cp, ok, c.want)
		}
	}
}

// A zip made on a Chinese Windows: every file comes out, under its Chinese name.
func TestExtractZipGBK(t *testing.T) {
	fakeCodePages(t, 936)
	dir := t.TempDir()
	zp := filepath.Join(dir, "pack.zip")
	_ = os.WriteFile(zp, legacyZip(t, gbkReadme, gbkPassword, gbkUpdate, gbkBlack, gbkWhite, gbkRoad, gbkNested), 0644)
	if needs, cp := zipNeedsTool(zp); needs || cp != cpGBK {
		t.Errorf("needs a program %v, code page %d", needs, cp)
	}
	out, err := extractZip(zp, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "密码.txt,更新.txt,白色.png,说明.txt,贴图/衣服.png,路.txt,黑色.png"
	if got := strings.Join(testkit.ListTree(out), ","); got != want {
		t.Errorf("unpacked %s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "黑色.png")); string(b) != gbkBlack {
		t.Errorf("黑色.png holds %q", b)
	}
}

// Zips made on Japanese Windows keep Shift-JIS names without the UTF-8 flag.
func TestShiftJISZip(t *testing.T) {
	fakeCodePages(t, 936)
	dir := t.TempDir()
	zp := filepath.Join(dir, "fluffy_tail.zip")
	_ = os.WriteFile(zp, legacyZip(t, sjisTail), 0644)
	out, err := extractZip(zp, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(out) != "ふわふわ尻尾" {
		t.Errorf("folder %q", filepath.Base(out))
	}
	if _, err := os.Stat(filepath.Join(out, "尻尾.unitypackage")); err != nil {
		t.Error(err)
	}
	// unpacked before: found again under its Japanese name
	if got := alreadyUnpacked(archiveSet{Main: zp, Parts: []string{zp}, Name: "fluffy_tail"}); got != out {
		t.Errorf("already unpacked: %q", got)
	}
}

func nothingUnpacked(t *testing.T, dir string, zips ...string) {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != strings.Join(zips, ",") {
		t.Errorf("in the folder: %v, want only %v", names, zips)
	}
}

// Two entries that would be written under one name: the unpacking stops with nothing written, and the
// archive is kept, instead of one file replacing the other without a word.
func TestExtractZipSameName(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "pack.zip")
	// "?" cannot be in a file name on Windows and becomes "？"
	_ = os.WriteFile(zp, legacyZip(t, "tex/a?.png", "tex/a？.png", "readme.txt"), 0644)
	if _, err := extractZip(zp, dir); err == nil || !strings.Contains(err.Error(), "重名") {
		t.Fatalf("err %v", err)
	}
	nothingUnpacked(t, dir, "pack.zip")
	removed := 0
	res := UnpackAll([]string{dir}, "", func(p []string) error { removed += len(p); return core.RecycleFiles(p) }, nil)
	if len(res.Done) != 0 || removed != 0 || !strings.Contains(res.Failed[zp], "重名") {
		t.Errorf("%+v, %d archives removed", res, removed)
	}
	nothingUnpacked(t, dir, "pack.zip")
}

// Names no code page reads: no U+FFFD names, no raw bytes — the archive stays packed.
func TestExtractZipUnreadableNames(t *testing.T) {
	fakeCodePages(t, 936)
	dir := t.TempDir()
	zp := filepath.Join(dir, "pack.zip")
	_ = os.WriteFile(zp, legacyZip(t, gbkReadme, badName), 0644)
	if _, err := extractZip(zp, dir); err != errZipNames {
		t.Fatalf("err %v", err)
	}
	res := UnpackAll([]string{dir}, "", core.RecycleFiles, nil)
	if len(res.Done) != 0 || res.Failed[zp] == "" {
		t.Errorf("%+v", res)
	}
	nothingUnpacked(t, dir, "pack.zip")
}

// The archive program is told the code page of the names, and never to replace a file it has just written.
func TestArcToolArgs(t *testing.T) {
	for _, c := range []struct {
		kind string
		cp   uint32
		want string
	}{
		{"7z", cpGBK, "x -y -aou -bd -oout -p -mcp=936 -- a.zip"},
		{"7z", cpSJIS, "x -y -aou -bd -oout -p -mcp=932 -- a.zip"},
		{"7z", 0, "x -y -aou -bd -oout -p -- a.zip"},
		{"bz", cpGBK, "x -y -o:out -cp:936 a.zip"},
		{"bz", 0, "x -y -o:out a.zip"},
	} {
		if got := strings.Join(arcToolArgs(arcTool{Kind: c.kind}, "a.zip", "out", "", c.cp), " "); got != c.want {
			t.Errorf("%s, code page %d: %s", c.kind, c.cp, got)
		}
	}
	p, err := exec.LookPath("7z")
	if err != nil {
		t.Skip("no 7z here")
	}
	// two entries of one name: both are there afterwards
	dir := t.TempDir()
	zp := filepath.Join(dir, "twice.zip")
	_ = os.WriteFile(zp, legacyZip(t, "a.txt", "b.txt", "a.txt"), 0644)
	out := filepath.Join(dir, "out")
	_ = os.MkdirAll(out, 0755)
	if err := runArcTool(arcTool{Kind: "7z", exe: p, Name: "7-Zip"}, zp, out, "", 0); err != nil {
		t.Fatal(err)
	}
	if got := testkit.ListTree(out); len(got) != 3 {
		t.Errorf("unpacked %v", got)
	}
}
