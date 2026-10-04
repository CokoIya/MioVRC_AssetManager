package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func TrimErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i+2 < len(s) {
		s = s[i+2:]
	}
	return s
}

// CompactJSON: the same answer without the indentation, so more of it fits.
func CompactJSON(b []byte) []byte {
	var out bytes.Buffer
	if json.Compact(&out, b) != nil {
		return b
	}
	return out.Bytes()
}

func CleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out
}

func CleanPaths(in []string) []string {
	var out []string
	for _, s := range CleanList(in) {
		s = strings.Trim(s, `"`)
		out = append(out, filepath.Clean(s))
	}
	return out
}

var winBad = strings.NewReplacer("<", "", ">", "", ":", "：", "\"", "", "/", "／", "\\", "＼", "|", "｜", "?", "？", "*", "＊")

var (
	// names Windows keeps for devices, whatever the extension ("NUL.zip" cannot be created)
	reWinDevice = regexp.MustCompile(`(?i)^(con|prn|aux|nul|(com|lpt)[0-9¹²³])$`)
	// what a name cut short keeps at its end: its extension, or the two of a volume or a tarball
	reKeepExt = regexp.MustCompile(`(?i)(\.tar\.[a-z0-9]{1,4}|\.part[0-9]+\.rar|\.(zip|7z|rar|tar)\.[0-9]{3}|\.[^.\s]{1,16})$`)
)

// SafeName makes a Windows-safe file or folder name of at most max characters. A longer name is cut
// before its extension, so a download is still recognised as the archive it is.
func SafeName(s string, max int) string {
	s = winBad.Replace(strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, s))
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		ext := []rune(reKeepExt.FindString(s))
		if len(ext)*2 > max {
			ext = nil
		}
		s = strings.TrimRight(string(r[:max-len(ext)]), ". ") + string(ext)
	}
	s = strings.TrimRight(s, ". ")
	stem, _, _ := strings.Cut(s, ".")
	if s == "" || reWinDevice.MatchString(strings.TrimSpace(stem)) {
		return "_" + s
	}
	return s
}

func Truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func StatOK(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func ContainsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func FileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func LowerExt(name string) string { return strings.ToLower(filepath.Ext(name)) }

func StripArchiveExt(name string) string {
	l := strings.ToLower(name)
	for _, e := range []string{".tar.gz", ".zip", ".rar", ".7z", ".unitypackage"} {
		if strings.HasSuffix(l, e) {
			return name[:len(name)-len(e)]
		}
	}
	return name
}

func PathKey(p string) string { return strings.ToLower(filepath.Clean(p)) }

func IsUnityProject(dir string) bool {
	if st, err := os.Stat(filepath.Join(dir, "ProjectSettings")); err == nil && st.IsDir() {
		if st2, err := os.Stat(filepath.Join(dir, "Assets")); err == nil && st2.IsDir() {
			return true
		}
	}
	return false
}

func IsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func UniqStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		k := strings.ToLower(s)
		if s == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func FmtMB(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }
