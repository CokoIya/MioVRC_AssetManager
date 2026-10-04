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

// Only the volumes of one scheme are one archive; archives that merely share a name are each their own.
func TestGroupArchivesByScheme(t *testing.T) {
	d := t.TempDir()
	names := []string{
		// the real volume sets
		"Z.zip", "Z.z01", "Z.z02", "R.rar", "R.r00", "R.r01", "P.part1.rar", "P.part02.rar", "S.7z.001", "S.7z.002", "N.zip.001", "N.zip.002",
		// one name, several archives
		"Dress.zip", "Dress.7z", "Dress.rar", "Dress.part1.rar", "Dress.part2.rar", "Dress.7z.001", "Dress.7z.002", "Dress.zip.001",
		"Coat.zip", "Coat.r00", "Hat.rar", "Hat.z01", "Bag.7z", "Bag.z01", "Bag.r00",
	}
	var files []string
	for _, n := range names {
		p := filepath.Join(d, n)
		_ = os.WriteFile(p, []byte("x"), 0644)
		files = append(files, p)
	}
	got := map[string]string{}
	for _, s := range groupArchives(files) {
		var parts []string
		for _, p := range s.Parts {
			parts = append(parts, filepath.Base(p))
		}
		sort.Strings(parts)
		got[filepath.Base(s.Main)] = s.Name + ":" + strings.Join(parts, "+")
		if rp := readParts(s); len(rp) != len(s.Parts) {
			t.Errorf("%s: read %v of %v", s.Main, rp, s.Parts)
		}
	}
	want := map[string]string{
		"Z.zip": "Z:Z.z01+Z.z02+Z.zip", "R.rar": "R:R.r00+R.r01+R.rar", "P.part1.rar": "P:P.part02.rar+P.part1.rar",
		"S.7z.001": "S:S.7z.001+S.7z.002", "N.zip.001": "N:N.zip.001+N.zip.002",
		"Dress.zip": "Dress:Dress.zip", "Dress.7z": "Dress:Dress.7z", "Dress.rar": "Dress:Dress.rar",
		"Dress.part1.rar": "Dress:Dress.part1.rar+Dress.part2.rar", "Dress.7z.001": "Dress:Dress.7z.001+Dress.7z.002", "Dress.zip.001": "Dress:Dress.zip.001",
		"Coat.zip": "Coat:Coat.zip", "Hat.rar": "Hat:Hat.rar", "Bag.7z": "Bag:Bag.7z", // (a lone .r00 / .z01 has no archive to open it)
	}
	for m, w := range want {
		if got[m] != w {
			t.Errorf("%s: got %q, want %q", m, got[m], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("sets: %v", got)
	}
	// one file asked for: its own set, not the others of its name
	for file, n := range map[string]int{"Dress.zip": 1, "Dress.7z": 1, "Dress.part2.rar": 2, "Z.z01": 3, "Coat.r00": 0} {
		sets := findArchives(filepath.Join(d, file))
		if n == 0 && len(sets) == 0 {
			continue
		}
		if len(sets) != 1 || len(sets[0].Parts) != n {
			t.Errorf("%s alone: %+v", file, sets)
		}
	}
	// a name, cut where the set's name ends: what is put in there keeps a volume a volume of its set
	for name, want := range map[string][3]string{"Dress.part1.rar": {"Dress", ".part1.rar", "dress|part"}, "Dress.ZIP.001": {"Dress", ".ZIP.001", "dress|num.zip"},
		"Dress.z01": {"Dress", ".z01", "dress|zip"}, "Dress.zip": {"Dress", ".zip", "dress|zip"}, "Dress.r00": {"Dress", ".r00", "dress|rar"}, "Dress.7z": {"Dress", ".7z", "dress|7z"},
		"Dress v1.2.unitypackage": {"Dress v1.2", ".unitypackage", "dress v1.2.unitypackage|"}, "README": {"README", "", "readme|"}} {
		if stem, rest, key := VolumeName(name); stem != want[0] || rest != want[1] || key != want[2] {
			t.Errorf("VolumeName(%q) = %q %q %q", name, stem, rest, key)
		}
	}
	// a set somebody put together by hand: what is not a volume of its first file is not among what was read
	odd := archiveSet{Main: filepath.Join(d, "Dress.zip"), Parts: []string{filepath.Join(d, "Dress.zip"), filepath.Join(d, "Dress.7z"), filepath.Join(d, "Dress.rar"), filepath.Join(d, "Z.z01")}}
	if rp := readParts(odd); len(rp) != 1 || rp[0] != odd.Main {
		t.Errorf("read of a mixed set: %v", rp)
	}
}

// "Dress.zip" and "Dress.7z" in one folder: each is unpacked, and none is removed without having been.
func TestUnpackAllSameNameArchives(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z here")
	}
	dir := t.TempDir()
	testkit.MakeZip(t, filepath.Join(dir, "Dress.zip"), map[string]string{"Dress/a.bin": "aaaa"})
	src := filepath.Join(t.TempDir(), "layers.psd")
	_ = os.WriteFile(src, []byte("layers"), 0644)
	if out, err := exec.Command("7z", "a", filepath.Join(dir, "Dress.7z"), src).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var removed []string
	res := UnpackAll([]string{dir}, "", func(p []string) error { removed = append(removed, p...); return RemoveFiles(p) }, nil)
	if len(res.Failed) > 0 || len(res.Done) != 2 || res.Removed != 2 || len(removed) != 2 {
		t.Fatalf("done %v failed %v removed %v", res.Done, res.Failed, removed)
	}
	left := strings.Join(testkit.ListTree(dir), " ")
	if !strings.Contains(left, "layers.psd") || !strings.Contains(left, "a.bin") || strings.Contains(left, "Dress.7z") || strings.Contains(left, "Dress.zip") {
		t.Errorf("left: %s", left)
	}
	// the 7z cannot be unpacked (no program for it here): it stays, whatever happens to the zip of its name
	dir = t.TempDir()
	testkit.MakeZip(t, filepath.Join(dir, "Dress.zip"), map[string]string{"Dress/a.bin": "aaaa"})
	_ = os.WriteFile(filepath.Join(dir, "Dress.7z"), []byte("not really a 7z"), 0644)
	res = UnpackAll([]string{dir}, "", RemoveFiles, nil)
	if _, err := os.Stat(filepath.Join(dir, "Dress.7z")); err != nil || res.Removed != 1 || len(res.Failed) != 1 {
		t.Errorf("the archive that failed: %v, removed %d, failed %v", err, res.Removed, res.Failed)
	}
}

// UnpackFiles takes the files it is given and what was in them: nothing else in their folder, and no folder
// that was there before.
func TestUnpackFilesLeavesTheRest(t *testing.T) {
	dir := t.TempDir()
	mine := map[string]string{"Old_v1.zip": "Old_v1/a.unitypackage", "original/PSD.zip": "tex.psd", "New/inner.zip": "x.txt"}
	for n, e := range mine {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, n)), 0755)
		testkit.MakeZip(t, filepath.Join(dir, n), map[string]string{e: "player"})
	}
	psd := testkit.ZipBytes(t, map[string][]byte{"PSD/body.psd": []byte("psd")})
	got := filepath.Join(dir, "New_v2.zip")
	_ = os.WriteFile(got, testkit.ZipBytes(t, map[string][]byte{"New_v2/New.unitypackage": []byte("pkg"), "New_v2/New_PSD.zip": psd}), 0644)
	again := filepath.Join(dir, "New.zip") // its folder is there already: left packed, and the folder is not gone through
	testkit.MakeZip(t, again, map[string]string{"New/b.txt": "b"})
	res := UnpackFiles([]string{got, again, dir}, "", RemoveFiles, nil)
	if len(res.Failed) > 0 || res.Removed != 2 {
		t.Fatalf("failed %v, removed %d, done %v", res.Failed, res.Removed, res.Done)
	}
	for n := range mine {
		if !core.FileExists(filepath.Join(dir, n)) {
			t.Errorf("the player's %s is gone", n)
		}
	}
	for _, p := range []string{"New_v2/New.unitypackage", "New_v2/PSD/body.psd", "New.zip"} {
		if !core.StatOK(filepath.Join(dir, p)) {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range []string{"New_v2.zip", "New_v2/New_PSD.zip", "Old_v1", "original/PSD", "New/inner"} {
		if core.StatOK(filepath.Join(dir, p)) {
			t.Errorf("%s should not be there", p)
		}
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
