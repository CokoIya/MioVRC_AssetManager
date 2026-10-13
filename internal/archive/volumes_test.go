package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/testkit"
)

// split writes b as n pieces of one size (the last the rest), named name.001 …
func split(t *testing.T, dir, name string, b []byte, n int) {
	t.Helper()
	size := (len(b) + n - 1) / n
	for i := 0; i < n; i++ {
		end := min((i+1)*size, len(b))
		if err := os.WriteFile(filepath.Join(dir, name+"."+[]string{"001", "002", "003", "004"}[i]), b[i*size:end], 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func noise(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func failedSay(res unpackResult) string {
	var out []string
	for f, e := range res.Failed {
		out = append(out, filepath.Base(f)+"："+e)
	}
	return strings.Join(out, "\n")
}

// Pieces of a zip in the way of 7-Zip: joined and unpacked here (no archive program needed); one missing in
// the middle or at the end is named.
func TestSplitZip(t *testing.T) {
	zb := testkit.ZipBytes(t, map[string][]byte{"Dress/Dress.unitypackage": noise(150000), "Dress/readme.txt": []byte("hi")})
	d := t.TempDir()
	split(t, d, "Dress.zip", zb, 3)
	res := UnpackAll([]string{d}, "", nil, nil)
	if len(res.Done) != 1 || len(res.Failed) != 0 {
		t.Fatalf("done %v failed %s", res.Done, failedSay(res))
	}
	if got := testkit.ListTree(res.Done[0]); strings.Join(got, ",") != "Dress.unitypackage,readme.txt" {
		t.Errorf("unpacked %v", got)
	}
	if left, _ := filepath.Glob(filepath.Join(d, ".Dress.*.joining")); len(left) > 0 {
		t.Errorf("the joined file was left: %v", left)
	}
	// the middle one missing
	d = t.TempDir()
	split(t, d, "Dress.zip", zb, 3)
	_ = os.Remove(filepath.Join(d, "Dress.zip.002"))
	if got := failedSay(UnpackAll([]string{d}, "", nil, nil)); got != "Dress.zip.001：缺少分卷：Dress.zip.002（找到 2 卷），请把全部分卷下载到同一个文件夹后再解压" {
		t.Errorf("middle: %s", got)
	}
	// the last one missing: the pieces there do not end the zip
	d = t.TempDir()
	split(t, d, "Dress.zip", zb, 3)
	_ = os.Remove(filepath.Join(d, "Dress.zip.003"))
	if got := failedSay(UnpackAll([]string{d}, "", nil, nil)); got != "Dress.zip.001：缺少后面的分卷：从 Dress.zip.003 起（找到 2 卷），请把全部分卷下载到同一个文件夹后再解压" {
		t.Errorf("end: %s", got)
	}
	// the first one missing
	d = t.TempDir()
	split(t, d, "Dress.zip", zb, 3)
	_ = os.Remove(filepath.Join(d, "Dress.zip.001"))
	if got := failedSay(UnpackAll([]string{d}, "", nil, nil)); got != "Dress.zip.002：缺少分卷：Dress.zip.001（找到 2 卷），请把全部分卷下载到同一个文件夹后再解压" {
		t.Errorf("first: %s", got)
	}
	// "x.001" with no word of what it is: the first piece says it is a zip
	d = t.TempDir()
	split(t, d, "Bare", zb, 2)
	if res := UnpackAll([]string{d}, "", nil, nil); len(res.Done) != 1 || len(res.Failed) != 0 {
		t.Errorf("bare pieces: %v %s", res.Done, failedSay(res))
	}
}

// Pieces of a unitypackage: joined into a folder of its own; cut short, the next piece is named.
func TestSplitUnitypackage(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := noise(200000)
	_ = tw.WriteHeader(&tar.Header{Name: "0123456789abcdef0123456789abcdef/asset", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	pkg := buf.Bytes()
	d := t.TempDir()
	split(t, d, "Hair.unitypackage", pkg, 2)
	res := UnpackAll([]string{d}, "", nil, nil)
	if len(res.Done) != 1 || len(res.Failed) != 0 {
		t.Fatalf("done %v failed %s", res.Done, failedSay(res))
	}
	if b, _ := os.ReadFile(filepath.Join(res.Done[0], "Hair.unitypackage")); !bytes.Equal(b, pkg) {
		t.Errorf("joined package differs (%d of %d bytes)", len(b), len(pkg))
	}
	d = t.TempDir()
	split(t, d, "Hair.unitypackage", pkg, 2)
	_ = os.Remove(filepath.Join(d, "Hair.unitypackage.002"))
	if got := failedSay(UnpackAll([]string{d}, "", nil, nil)); got != "Hair.unitypackage.001：缺少后面的分卷：从 Hair.unitypackage.002 起（找到 1 卷），请把全部分卷下载到同一个文件夹后再解压" {
		t.Errorf("cut short: %s", got)
	}
}

// A zip spanned the way ZIP does it (x.z01 … x.zip): unpacked from the .zip; its end record says how many pieces
// there are, so a missing one is named, and without the .zip that is what is said.
func TestSpannedZip(t *testing.T) {
	if _, err := exec.LookPath("zip"); err != nil {
		t.Skip("no zip here")
	}
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "a.bin"), noise(300000), 0644)
	_ = os.WriteFile(filepath.Join(src, "b.bin"), noise(200000), 0644)
	make := func() string {
		d := t.TempDir()
		cmd := exec.Command("zip", "-q", "-s", "100k", filepath.Join(d, "Coat.zip"), "a.bin", "b.bin")
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("zip: %v %s", err, out)
		}
		return d
	}
	d := make()
	a := findArchives(d)
	if len(a) != 1 || filepath.Base(a[0].Main) != "Coat.zip" || len(a[0].Parts) != 5 {
		t.Fatalf("sets %+v", a)
	}
	if got := missingVolumes(a[0]); got != "" {
		t.Errorf("whole: %s", got)
	}
	if _, err := exec.LookPath("7z"); err == nil && len(ArchiveTools()) > 0 {
		if res := UnpackAll([]string{d}, "", nil, nil); len(res.Done) != 1 {
			t.Errorf("unpacked: %v %s", res.Done, failedSay(res))
		}
	}
	d = make()
	_ = os.Remove(filepath.Join(d, "Coat.z02"))
	if got := failedSay(UnpackAll([]string{d}, "", nil, nil)); got != "Coat.zip：缺少分卷：Coat.z02（共 5 卷，找到 4 卷），请把全部分卷下载到同一个文件夹后再解压" {
		t.Errorf("a piece: %s", got)
	}
	d = make()
	_ = os.Remove(filepath.Join(d, "Coat.zip"))
	if got := failedSay(UnpackAll([]string{d}, "", nil, nil)); got != "Coat.z01：缺少 Coat.zip：.z01、.z02 这样的分卷要和同名的 .zip 放在同一个文件夹，解压从 .zip 开始（.zip 是最后一卷）" {
		t.Errorf("no .zip: %s", got)
	}
}

// A 7z in pieces: its start header says how long it is, so a missing piece is named wherever it is.
func TestSplit7z(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z here")
	}
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "a.bin"), noise(300000), 0644)
	_ = os.WriteFile(filepath.Join(src, "b.bin"), noise(200000), 0644)
	make := func() string {
		d := t.TempDir()
		cmd := exec.Command("7z", "a", "-v100k", filepath.Join(d, "Bag.7z"), "a.bin", "b.bin")
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("7z: %v %s", err, out)
		}
		return d
	}
	for _, c := range []struct{ gone, want string }{
		{"Bag.7z.005", "Bag.7z.001：缺少分卷：Bag.7z.005（共 5 卷，找到 4 卷），请把全部分卷下载到同一个文件夹后再解压"},
		{"Bag.7z.003", "Bag.7z.001：缺少分卷：Bag.7z.003（共 5 卷，找到 4 卷），请把全部分卷下载到同一个文件夹后再解压"},
		{"Bag.7z.001", "Bag.7z.002：缺少分卷：Bag.7z.001（找到 4 卷），请把全部分卷下载到同一个文件夹后再解压"},
	} {
		d := make()
		_ = os.Remove(filepath.Join(d, c.gone))
		if got := failedSay(UnpackAll([]string{d}, "", nil, nil)); got != c.want {
			t.Errorf("without %s: %s", c.gone, got)
		}
	}
	if d := make(); len(ArchiveTools()) > 0 {
		if res := UnpackAll([]string{d}, "", nil, nil); len(res.Done) != 1 {
			t.Errorf("whole: %v %s", res.Done, failedSay(res))
		}
	}
}

// RAR volumes say whether another one follows; what archive programs write of a missing volume is read too.
func TestRarVolumesAndToolOutput(t *testing.T) {
	d := t.TempDir()
	w := func(name string, tail []byte) {
		_ = os.WriteFile(filepath.Join(d, name), append(append([]byte("Rar!\x1a\x07\x01\x00"), noise(100)...), tail...), 0644)
	}
	w("Set.part1.rar", []byte{0x1d, 0x77, 0x56, 0x51, 0x03, 0x05, 0x04, 0x01}) // RAR 5, another follows
	w("Set.part2.rar", []byte{0xaa, 0xbb, 0xcc, 0xdd, 0x03, 0x05, 0x04, 0x01})
	a := findArchives(filepath.Join(d, "Set.part1.rar"))
	if got := missingVolumes(a[0]); got != "缺少后面的分卷：从 Set.part3.rar 起（找到 2 卷），请把全部分卷下载到同一个文件夹后再解压" {
		t.Errorf("RAR 5: %s", got)
	}
	w("Set.part2.rar", []byte{0x1d, 0x77, 0x56, 0x51, 0x03, 0x05, 0x04, 0x00}) // the last
	if got := missingVolumes(a[0]); got != "" {
		t.Errorf("RAR 5, all there: %s", got)
	}
	w("Old.rar", []byte{0x12, 0x34, 0x7b, 0x01, 0x40, 0x07, 0x00}) // RAR 4: continues
	w("Old.r00", []byte{0x12, 0x34, 0x7b, 0x01, 0x40, 0x07, 0x00})
	a = findArchives(filepath.Join(d, "Old.rar"))
	if got := missingVolumes(a[0]); got != "缺少后面的分卷：从 Old.r01 起（找到 2 卷），请把全部分卷下载到同一个文件夹后再解压" {
		t.Errorf("RAR 4: %s", got)
	}
	w("Old.r00", []byte{0x12, 0x34, 0x7b, 0x00, 0x40, 0x07, 0x00})
	if got := missingVolumes(a[0]); got != "" {
		t.Errorf("RAR 4, all there: %s", got)
	}
	out := "ERRORS:\nMissing volume : sp.z02\n\nERROR = Missing volume : sp.z02\nVolumes = 5\n"
	set := archiveSet{Main: "/x/sp.zip", Parts: []string{"/x/sp.zip", "/x/sp.z01", "/x/sp.z03"}}
	if got := toolSays(out, set); got != "缺少分卷：sp.z02（找到 3 卷），请把全部分卷下载到同一个文件夹后再解压" {
		t.Errorf("7-Zip's words: %s", got)
	}
	if got := toolSays("Cannot find volume D:\\dl\\Set.part3.rar", archiveSet{Main: "/x/Set.part1.rar", Parts: []string{"/x/Set.part1.rar", "/x/Set.part2.rar"}}); !strings.HasPrefix(got, "缺少分卷：Set.part3.rar（") {
		t.Errorf("WinRAR's words: %s", got)
	}
	if got := toolSays("ERROR: Data Error : a.bin", archiveSet{Main: "/x/a.zip", Parts: []string{"/x/a.zip"}}); !strings.HasPrefix(got, "压缩包数据校验失败") {
		t.Errorf("damaged: %s", got)
	}
}
