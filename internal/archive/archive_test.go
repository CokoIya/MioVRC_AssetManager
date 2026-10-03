package archive

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

func TestGroupArchives(t *testing.T) {
	d := t.TempDir()
	names := []string{"A.zip", "B.part1.rar", "B.part2.rar", "C.7z.001", "C.7z.002", "D.zip", "D.z01", "E.part2.rar", "F.rar", "F.r00"}
	var files []string
	for _, n := range names {
		p := filepath.Join(d, n)
		_ = os.WriteFile(p, []byte("x"), 0644)
		files = append(files, p)
	}
	var got []string
	for _, s := range groupArchives(files) {
		got = append(got, filepath.Base(s.Main)+"/"+s.Name+"/"+string(rune('0'+len(s.Parts))))
	}
	sort.Strings(got)
	want := "A.zip/A/1,B.part1.rar/B/2,C.7z.001/C/2,D.zip/D/2,F.rar/F/2"
	if strings.Join(got, ",") != want {
		t.Errorf("got %s", strings.Join(got, ","))
	}
}

func TestUnpackAll(t *testing.T) {
	root := t.TempDir()
	asset := filepath.Join(root, "Kaguya Dress")
	_ = os.MkdirAll(asset, 0755)
	// the main zip holds the package folder and a PSD zip
	psd := testkit.ZipBytes(t, map[string][]byte{"PSD/body.psd": []byte("psd")})
	main := testkit.ZipBytes(t, map[string][]byte{"Dress/Dress_Kaguya.unitypackage": []byte("pkg"), "Dress/Dress_PSD.zip": psd})
	_ = os.WriteFile(filepath.Join(asset, "Dress_v1.zip"), main, 0644)
	res := UnpackAll([]string{asset}, "", core.RecycleFiles, nil)
	if len(res.Failed) > 0 {
		t.Fatalf("failed: %v", res.Failed)
	}
	for _, p := range []string{"Dress/Dress_Kaguya.unitypackage", "Dress/PSD/body.psd"} {
		if _, err := os.Stat(filepath.Join(asset, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range []string{"Dress_v1.zip", "Dress/Dress_PSD.zip"} {
		if _, err := os.Stat(filepath.Join(asset, p)); err == nil {
			t.Errorf("archive kept: %s", p)
		}
	}
	if res.Removed != 2 {
		t.Errorf("removed %d", res.Removed)
	}
	// unpacked by hand before: left alone
	_ = os.WriteFile(filepath.Join(asset, "Dress.zip"), main, 0644)
	res = UnpackAll([]string{asset}, "", core.RecycleFiles, nil)
	if len(res.Done) != 0 || res.Removed != 0 {
		t.Errorf("unpacked again: %+v", res)
	}
}

func TestUnpackWithTool(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z here")
	}
	root := t.TempDir()
	src := filepath.Join(t.TempDir(), "Hair")
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "Hair.unitypackage"), bytes.Repeat([]byte("h"), 300000), 0644)
	// a password-protected 7z split into volumes
	cmd := exec.Command("7z", "a", "-v100k", "-mx=0", "-psecret", "-mhe=on", filepath.Join(root, "Hair.7z"), src)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z: %v %s", err, b)
	}
	sets := findArchives(root)
	if len(sets) != 1 || len(sets[0].Parts) < 3 {
		t.Fatalf("sets %+v", sets)
	}
	if _, err := extractArchive(sets[0], "wrong"); err != ErrArcPwd {
		t.Errorf("wrong password: %v", err)
	}
	res := UnpackAll([]string{root}, "secret", core.RecycleFiles, nil)
	if len(res.Failed) > 0 {
		t.Fatalf("failed %v", res.Failed)
	}
	if _, err := os.Stat(filepath.Join(root, "Hair", "Hair.unitypackage")); err != nil {
		t.Error("not unpacked")
	}
	if left, _ := filepath.Glob(filepath.Join(root, "Hair.7z.*")); len(left) > 0 {
		t.Errorf("volumes left: %v", left)
	}
}

// The archive program Windows opens archives with leads to its command-line program.
func TestToolFromExe(t *testing.T) {
	d := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(d, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("x"), 0644)
		return p
	}
	cases := []struct{ gui, cli, kind string }{
		{"7-Zip/7zFM.exe", "7-Zip/7z.exe", "7z"},
		{"Bandizip/Bandizip.exe", "Bandizip/bz.exe", "bz"},
		{"WinRAR/WinRAR.exe", "WinRAR/WinRAR.exe", "winrar"},
		{"HaoZip/HaoZip.exe", "HaoZip/HaoZipC.exe", "haozip"},
	}
	for _, c := range cases {
		gui, cli := mk(c.gui), mk(c.cli)
		tool := toolFromExe(gui)
		if tool == nil || tool.Kind != c.kind || tool.exe != cli {
			t.Errorf("%s → %+v", c.gui, tool)
		}
	}
	if toolFromExe(mk("360zip/360zip.exe")) != nil {
		t.Error("360压缩 has no command line to use")
	}
}
