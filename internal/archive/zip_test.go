package archive

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	// one folder inside: kept as it is
	z1 := filepath.Join(dir, "Kaguya_v1.07.zip")
	testkit.MakeZip(t, z1, map[string]string{"Kaguya/Kaguya.unitypackage": "pkg", "Kaguya/readme.txt": "hi"})
	out, err := extractZip(z1, dir)
	if err != nil || filepath.Base(out) != "Kaguya" {
		t.Fatalf("single folder: %v %v", out, err)
	}
	// loose files: a folder named after the zip; "../" cannot escape
	z2 := filepath.Join(dir, "Dress.zip")
	testkit.MakeZip(t, z2, map[string]string{"Dress.unitypackage": "pkg", "Texture/a.png": "png", "../../evil.txt": "x"})
	out, err = extractZip(z2, dir)
	if err != nil || filepath.Base(out) != "Dress" {
		t.Fatalf("loose files: %v %v", out, err)
	}
	if got := strings.Join(testkit.ListTree(out), ","); got != "Dress.unitypackage,Texture/a.png,evil.txt" {
		t.Errorf("Dress contents: %s", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); err == nil {
		t.Error("zip slip")
	}
	// unpacking again does not write into the first copy
	out2, err := extractZip(z2, dir)
	if err != nil || filepath.Base(out2) != "Dress (2)" {
		t.Errorf("second unpack: %v %v", out2, err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".*extracting")); len(leftovers) > 0 {
		t.Errorf("temp folders left: %v", leftovers)
	}
}

// Zips made on Japanese Windows keep Shift-JIS names without the UTF-8 flag.
func TestShiftJISZip(t *testing.T) {
	dir := t.TempDir()
	sjis := []byte{0x82, 0xd3, 0x82, 0xed, 0x82, 0xd3, 0x82, 0xed, 0x90, 0x4b, 0x94, 0xf6, 0x2f, 0x90, 0x4b, 0x94, 0xf6, 0x2e, 0x75, 0x6e, 0x69, 0x74, 0x79, 0x70, 0x61, 0x63, 0x6b, 0x61, 0x67, 0x65}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: string(sjis), NonUTF8: true, Method: zip.Deflate})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("pkg"))
	_ = zw.Close()
	zp := filepath.Join(dir, "fluffy_tail.zip")
	_ = os.WriteFile(zp, buf.Bytes(), 0644)
	out, err := extractZip(zp, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := core.DecodeCP932(sjis); !ok {
		t.Skip("no code page conversion on this system")
	}
	if filepath.Base(out) != "ふわふわ尻尾" {
		t.Errorf("folder %q", filepath.Base(out))
	}
	if _, err := os.Stat(filepath.Join(out, "尻尾.unitypackage")); err != nil {
		t.Error(err)
	}
}
