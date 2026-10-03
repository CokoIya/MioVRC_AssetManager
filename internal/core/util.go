package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// SafeName makes a Windows-safe file or folder name.
func SafeName(s string, max int) string {
	s = winBad.Replace(strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, s))
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	s = strings.TrimRight(s, ". ")
	switch strings.ToUpper(s) {
	case "", "CON", "PRN", "AUX", "NUL", "COM1", "LPT1":
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
