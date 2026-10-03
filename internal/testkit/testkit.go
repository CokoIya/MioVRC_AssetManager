package testkit

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"vrclib/internal/core"
)

func NewStore(t *testing.T) *core.Store {
	t.Helper()
	core.DataDir = t.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	st.Settings.Proxy = "direct"
	return st
}

func JPEG(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func MakeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(files[n]))
	}
	_ = zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func ListTree(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			r, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func WriteFile(t *testing.T, p string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, bytes.Repeat([]byte("x"), size), 0644); err != nil {
		t.Fatal(err)
	}
}

func PanDir(name string, kids ...*core.PanFile) *core.PanFile {
	return &core.PanFile{Name: name, Dir: true, Children: kids}
}
func PanFile(name string) *core.PanFile { return &core.PanFile{Name: name, Size: 100} }

func ZipBytes(t *testing.T, files map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, _ := zw.Create(n)
		_, _ = w.Write(files[n])
	}
	_ = zw.Close()
	return buf.Bytes()
}
