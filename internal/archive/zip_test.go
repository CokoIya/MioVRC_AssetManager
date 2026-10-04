package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
