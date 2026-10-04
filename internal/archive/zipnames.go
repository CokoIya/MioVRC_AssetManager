package archive

import (
	"archive/zip"
	"errors"
	"strings"
	"unicode/utf8"

	"vrclib/internal/core"
)

// Windows converts the names and knows this system's own code page; tests put their own in.
var (
	decodeCP = core.DecodeCodePage
	ansiCP   = core.ANSICodePage
)

const (
	cpSJIS = 932   // zips made on a Japanese Windows
	cpGBK  = 936   // … on a Chinese one
	cpUTF8 = 65001 // UTF-8 names without the flag that says so (macOS)
)

var errZipNames = errors.New("无法识别压缩包内文件名的编码，为避免文件名错乱或文件相互覆盖，已停止解压并保留压缩包，请使用解压软件手动解压")

// legacyName: a name stored without the UTF-8 flag that is more than ASCII, so in whatever code page the
// computer that made the zip was using.
func legacyName(f *zip.File) bool { return f.NonUTF8 && !core.IsASCII(f.Name) }

// zipCodePage decides how such names are to be read, once for the whole archive (name by name, half of a
// Chinese zip reads as Japanese): 0 when there are none, else a code page. False: nothing reads all of them.
func zipCodePage(files []*zip.File) (uint32, bool) {
	var names []string
	isUTF8 := true
	for _, f := range files {
		if legacyName(f) {
			names = append(names, f.Name)
			isUTF8 = isUTF8 && utf8.ValidString(f.Name)
		}
	}
	switch {
	case len(names) == 0:
		return 0, true
	case isUTF8:
		return cpUTF8, true
	}
	return pickCodePage(names)
}

func decodesAll(cp uint32, names []string) bool {
	for _, n := range names {
		if _, ok := decodeCP(cp, []byte(n)); !ok {
			return false
		}
	}
	return true
}

// pickCodePage: Shift-JIS or GBK. A code page in which one of the names is not valid is out. When both
// read everything the text decides, and when the text says nothing, this system's own code page, then GBK.
func pickCodePage(names []string) (uint32, bool) {
	sjisOK, gbkOK := decodesAll(cpSJIS, names), decodesAll(cpGBK, names)
	switch {
	case sjisOK && !gbkOK:
		return cpSJIS, true
	case gbkOK && !sjisOK:
		return cpGBK, true
	case !sjisOK:
		if cp := ansiCP(); cp != 0 && cp != cpSJIS && cp != cpGBK && cp != cpUTF8 && decodesAll(cp, names) {
			return cp, true // another Windows altogether (Korean, Traditional Chinese …), read on one like it
		}
		return 0, false
	}
	sjis, gbk := 0, 0
	for _, n := range names {
		s, _ := decodeCP(cpSJIS, []byte(n))
		sjis += sjisEvidence(s)
		gbk += gbkEvidence(n)
	}
	switch {
	case sjis > gbk:
		return cpSJIS, true
	case gbk > sjis || ansiCP() != cpSJIS:
		return cpGBK, true
	}
	return cpSJIS, true
}

// sjisEvidence, on a name read as Shift-JIS: hiragana and full-width katakana are only there in Japanese
// text; half-width katakana is what the bytes of Chinese characters look like read this way.
func sjisEvidence(s string) int {
	e := 0
	for _, r := range s {
		switch {
		case (r >= 0x3041 && r <= 0x3096) || (r >= 0x30a1 && r <= 0x30fa) || r == 0x30fc:
			e += 2
		case r >= 0xff61 && r <= 0xff9f:
			e--
		}
	}
	return e
}

// gbkEvidence, on the bytes: the everyday Chinese characters (GB2312: first byte B0–F7, second A1–FE) speak
// for GBK; the rare ones of its extension areas, where Japanese text lands read as GBK, speak against.
func gbkEvidence(n string) int {
	e := 0
	for i := 0; i+1 < len(n); i++ {
		lead, trail := n[i], n[i+1]
		if lead < 0x81 {
			continue
		}
		i++
		switch {
		case lead >= 0xb0 && lead <= 0xf7 && trail >= 0xa1:
			e++
		case lead >= 0xa1 && lead <= 0xa9 && trail >= 0xa1:
			// punctuation, kana, Greek …: either
		default:
			e--
		}
	}
	return e
}

// zipNames: the name of every entry as its author saw it, with "/" between folders. An error when the
// names cannot be read: U+FFFD names or raw bytes would make different files end up under one name.
func zipNames(files []*zip.File) ([]string, error) {
	cp, ok := zipCodePage(files)
	if !ok {
		return nil, errZipNames
	}
	out := make([]string, len(files))
	for i, f := range files {
		n := f.Name
		if cp != 0 && cp != cpUTF8 && legacyName(f) {
			if n, ok = decodeCP(cp, []byte(f.Name)); !ok {
				return nil, errZipNames
			}
		}
		out[i] = strings.ReplaceAll(n, "\\", "/")
	}
	return out, nil
}
