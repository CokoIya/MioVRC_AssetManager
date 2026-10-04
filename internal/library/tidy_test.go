package library

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

func writeBytes(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
}

// blob: size bytes that differ from another seed's all the way through.
func blob(seed byte, size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = seed + byte(i%251)
	}
	return b
}

func waitTidy(t *testing.T, st *core.Store, what, dl string) TidyPart {
	t.Helper()
	for i := 0; i < 400; i++ {
		v := TidyStatus(st, dl)
		p := v.Dup
		if what == "arc" {
			p = v.Arc
		}
		if !p.Run.Running {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the scan did not end")
	return TidyPart{}
}

func tidyStore(t *testing.T) (st *core.Store, root, dl string) {
	t.Helper()
	st = testkit.NewStore(t)
	root, dl = t.TempDir(), t.TempDir()
	st.Settings.Roots = []string{root}
	st.Settings.DownloadDir = dl
	st.Settings.NoWatch, st.Settings.HideZh = true, true // (no names sent to the translator from a test)
	tidy.mu.Lock()
	tidy.runs, tidy.cache, tidy.adopt, tidy.adoptDup, tidy.adoptArc = nil, nil, 0, false, false
	tidy.mu.Unlock()
	t.Cleanup(func() {
		// the rescan after something went to the Recycle Bin, and the look into the packages that follows it
		for i := 0; i < 400 && BackgroundBusy(); i++ {
			time.Sleep(25 * time.Millisecond)
		}
	})
	return
}

func TestDupScan(t *testing.T) {
	st, root, dl := tidyStore(t)
	same := blob(1, 300<<10)
	writeBytes(t, filepath.Join(root, "Used", "pack.unitypackage"), same)
	writeBytes(t, filepath.Join(root, "Noted", "sub", "pack copy.unitypackage"), same)
	writeBytes(t, filepath.Join(dl, "pack.unitypackage"), same)
	// same size, another middle: told apart by the parts
	mid := append([]byte{}, same...)
	mid[len(mid)/2] ^= 0xff
	writeBytes(t, filepath.Join(root, "Noted", "mid.bin"), mid)
	// same size, same head, middle and tail, one byte elsewhere: told apart only by reading all of it
	deep := append([]byte{}, same...)
	deep[100<<10] ^= 0xff
	writeBytes(t, filepath.Join(root, "Noted", "deep.bin"), deep)
	// a second pair, smaller, both loose
	writeBytes(t, filepath.Join(root, "loose", "a.zip"), blob(9, 70<<10))
	writeBytes(t, filepath.Join(dl, "a (1).zip"), blob(9, 70<<10))
	// a third pair, both inside one folder of one asset: the asset may need each of them
	writeBytes(t, filepath.Join(root, "Noted", "Assets", "A", "tex.png"), blob(5, 66<<10))
	writeBytes(t, filepath.Join(root, "Noted", "Assets", "B", "tex.png"), blob(5, 66<<10))
	// below the size that is compared; inside a Unity project; in a hidden folder; a second name for one file
	writeBytes(t, filepath.Join(root, "Used", "tiny.txt"), []byte("x"))
	writeBytes(t, filepath.Join(root, "Noted", "tiny.txt"), []byte("x"))
	proj := filepath.Join(root, "MyProject")
	_ = os.MkdirAll(filepath.Join(proj, "ProjectSettings"), 0755)
	writeBytes(t, filepath.Join(proj, "Assets", "pack.unitypackage"), same)
	writeBytes(t, filepath.Join(root, ".cache", "pack.unitypackage"), same)
	if err := os.Link(filepath.Join(root, "Used", "pack.unitypackage"), filepath.Join(root, "Used", "hardlink.unitypackage")); err != nil {
		t.Log("no hard links here:", err)
	}
	st.Settings.Roots = append(st.Settings.Roots, filepath.Join(root, "Used"), filepath.Join(t.TempDir(), "unplugged")) // one inside another, one that is not there
	st.Assets = []*core.Asset{
		{Key: "name:used", Name: "Used", Locations: []core.Location{{Path: filepath.Join(root, "Used"), Kind: "dir", Root: root}}, Usage: []core.Usage{{Project: "P", Status: "used"}}},
		{Key: "name:noted", Name: "Noted", Locations: []core.Location{{Path: filepath.Join(root, "Noted"), Kind: "dir", Root: root}}},
	}
	st.User["name:noted"] = &core.UserData{Notes: "mine"}

	if err := TidyStart(st, "dup", dl, 64<<10); err != nil {
		t.Fatal(err)
	}
	p := waitTidy(t, st, "dup", dl)
	if p.Dup == nil || p.Run.Err != "" {
		t.Fatalf("no result: %+v", p.Run)
	}
	r := p.Dup
	if len(r.Groups) != 3 || r.Total != 3 {
		t.Fatalf("groups: %+v", r.Groups)
	}
	g := r.Groups[0] // the bigger waste first
	var names []string
	keep := ""
	for _, f := range g.Files {
		names = append(names, filepath.Base(f.Path))
		if f.Keep {
			keep += filepath.Base(filepath.Dir(f.Path))
		}
	}
	sort.Strings(names)
	// (a hard link is the same file: one of the two names is listed, whichever came first)
	if len(g.Files) != 3 || g.Size != int64(len(same)) || !g.Cross || g.Inner {
		t.Errorf("the big group: %v cross=%v", names, g.Cross)
	}
	if keep != "Used" {
		t.Errorf("kept by suggestion: %q (the copy in the asset a project uses)", keep)
	}
	for _, f := range g.Files {
		if strings.Contains(f.Path, "MyProject") || strings.Contains(f.Path, ".cache") || strings.HasSuffix(f.Path, ".bin") {
			t.Errorf("listed: %s", f.Path)
		}
	}
	if g2 := r.Groups[1]; len(g2.Files) != 2 || g2.Cross || g2.Inner || g2.Size != 70<<10 {
		t.Errorf("the small group: %+v", g2)
	}
	if g3 := r.Groups[2]; len(g3.Files) != 2 || g3.Cross || !g3.Inner || g3.Files[0].Asset != "name:noted" {
		t.Errorf("the group inside one asset folder: %+v", g3)
	}
	if r.Waste != 2*int64(len(same))+70<<10+66<<10 {
		t.Errorf("waste %d", r.Waste)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "unplugged") {
		t.Errorf("warnings: %v", r.Warnings)
	}

	// to the Recycle Bin: never a whole group, nothing the scan did not list, nothing that changed since
	var trash []string
	recycleFiles = func(ps []string) error {
		for _, p := range ps {
			trash = append(trash, p)
			_ = os.Remove(p)
		}
		return nil
	}
	defer func() { recycleFiles = core.RecycleOnly }()
	var all []string
	for _, f := range g.Files {
		all = append(all, f.Path)
	}
	if _, err := TidyRecycle(st, "dup", all, dl, r.Seq); err == nil || !strings.Contains(err.Error(), "至少需保留一份") {
		t.Errorf("all copies of a group: %v", err)
	}
	if _, err := TidyRecycle(st, "dup", []string{filepath.Join(root, "Noted", "deep.bin")}, dl, r.Seq); err == nil {
		t.Error("a file that is no copy was accepted")
	}
	// confirmed against another scan than the one that is kept now: not acted on
	if _, err := TidyRecycle(st, "dup", []string{filepath.Join(dl, "pack.unitypackage")}, dl, r.Seq-1); err != errTidyMoved {
		t.Errorf("a result the player has not seen: %v", err)
	}
	if len(trash) != 0 {
		t.Fatalf("something went: %v", trash)
	}
	inDL, inNoted := filepath.Join(dl, "pack.unitypackage"), filepath.Join(root, "Noted", "sub", "pack copy.unitypackage")
	then := time.Now().Add(-time.Hour)
	_ = os.Chtimes(inNoted, then, then) // touched since the scan
	res, err := TidyRecycle(st, "dup", []string{inDL, inNoted}, dl, r.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || res.Freed != int64(len(same)) || len(res.Failed) != 1 || res.Failed[0].Path != inNoted || strings.Join(trash, ",") != inDL {
		t.Errorf("recycled %+v, trash %v", res, trash)
	}
	// the result goes on without what went, although the library is being scanned again because of it
	p = waitTidy(t, st, "dup", dl)
	if p.Dup == nil || p.Stale || len(p.Dup.Groups) != 3 || len(p.Dup.Groups[0].Files) != 2 || p.Dup.Waste != int64(len(same))+70<<10+66<<10 {
		t.Fatalf("after the first went: %+v", p)
	}
	// a drive without a Recycle Bin: the file stays, and the player is told why
	recycleFiles = func([]string) error { return core.ErrNoRecycleBin }
	loose := filepath.Join(dl, "a (1).zip")
	if res, err := TidyRecycle(st, "dup", []string{loose}, dl, r.Seq); err != nil || res.Removed != 0 || len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Err, "没有回收站") || !core.StatOK(loose) {
		t.Errorf("no Recycle Bin: %+v %v", res, err)
	}
	// once the rescan the clean-up began has ended, the result goes by the library as it is then …
	for i := 0; i < 400 && PipelineBusy(); i++ {
		time.Sleep(25 * time.Millisecond)
	}
	if p := TidyStatus(st, dl).Dup; p.Dup == nil || p.Stale {
		t.Fatalf("after the rescan: %+v", p)
	}
	tidy.mu.Lock()
	if tidy.adopt != 0 {
		t.Error("the clean-up's allowance is still open after its rescan")
	}
	tidy.mu.Unlock()
	// … and a change that is not this tool's own makes it stale
	st.Mu.Lock()
	st.Assets = append(st.Assets, &core.Asset{Key: "name:new", Size: 5})
	st.Mu.Unlock()
	if p := TidyStatus(st, dl).Dup; p.Dup != nil || !p.Stale {
		t.Errorf("the library changed: %+v", p)
	}
}

func TestDupScanCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	writeBytes(t, filepath.Join(root, "a"), blob(1, 1000))
	if _, err := scanDups(ctx, tidyInput{dirs: []string{root}}, 1, func(int64, int64, string) {}); err != context.Canceled {
		t.Errorf("a cancelled scan: %v", err)
	}
}

func TestArchiveScan(t *testing.T) {
	st, root, dl := tidyStore(t)
	old := time.Now().Add(-time.Hour)
	mk := func(p string, files map[string]string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		testkit.MakeZip(t, p, files)
		_ = os.Chtimes(p, old, old)
	}
	big := string(blob(3, 5000))
	// 1. unpacked next to it, under the folder inside the zip
	mk(filepath.Join(root, "Dress.zip"), map[string]string{"Dress v1/a.unitypackage": big, "Dress v1/readme.txt": "hello", "__MACOSX/x": "junk"})
	writeBytes(t, filepath.Join(root, "Dress v1", "a.unitypackage"), []byte(big))
	writeBytes(t, filepath.Join(root, "Dress v1", "readme.txt"), []byte("hello"))
	// 2. a folder of the right name, but not what the zip holds
	mk(filepath.Join(root, "Hair.zip"), map[string]string{"h.unitypackage": big, "tex.png": "12345"})
	writeBytes(t, filepath.Join(root, "Hair", "h.unitypackage"), []byte(big))
	writeBytes(t, filepath.Join(root, "Hair", "tex.png"), []byte("1234")) // another size
	// 3. unpacked into the asset's folder, which is not named after it
	mk(filepath.Join(root, "Shoes_download_v2.zip"), map[string]string{"s.unitypackage": big, "docs/readme.txt": "r"})
	writeBytes(t, filepath.Join(root, "Shoes", "s.unitypackage"), []byte(big))
	writeBytes(t, filepath.Join(root, "Shoes", "docs", "readme.txt"), []byte("r"))
	// 4. not unpacked at all; 5. in the download folder, unpacked; 6. volumes that cannot be looked into here
	mk(filepath.Join(root, "Packed.zip"), map[string]string{"p.unitypackage": big})
	mk(filepath.Join(dl, "9 Item", "Bag.zip"), map[string]string{"bag.unitypackage": big})
	writeBytes(t, filepath.Join(dl, "9 Item", "Bag", "bag.unitypackage"), []byte(big))
	for _, n := range []string{"Vol.part1.rar", "Vol.part2.rar"} {
		writeBytes(t, filepath.Join(root, n), []byte("not really a rar"))
		_ = os.Chtimes(filepath.Join(root, n), old, old)
	}
	// 7. written a moment ago: a download may still be at it
	testkit.MakeZip(t, filepath.Join(root, "Fresh.zip"), map[string]string{"f.txt": "f"})
	writeBytes(t, filepath.Join(root, "Fresh", "f.txt"), []byte("f"))
	st.Assets = []*core.Asset{{Key: "name:shoes", Name: "Shoes", Locations: []core.Location{
		{Path: filepath.Join(root, "Shoes"), Kind: "dir", Root: root}, {Path: filepath.Join(root, "Shoes_download_v2.zip"), Kind: "zip", Root: root}}}}

	if err := TidyStart(st, "arc", dl, 0); err != nil {
		t.Fatal(err)
	}
	p := waitTidy(t, st, "arc", dl)
	if p.Arc == nil {
		t.Fatalf("no result: %+v", p.Run)
	}
	got := map[string]ArcItem{}
	for _, it := range p.Arc.Items {
		got[filepath.Base(it.Main)] = it
	}
	if len(got) != 3 || got["Dress.zip"].Folder != filepath.Join(root, "Dress v1") || got["Shoes_download_v2.zip"].Folder != filepath.Join(root, "Shoes") ||
		got["Bag.zip"].Folder != filepath.Join(dl, "9 Item", "Bag") {
		t.Fatalf("items: %+v", p.Arc.Items)
	}
	if it := got["Dress.zip"]; it.Checked != 2 || it.Entries != 3 || len(it.Parts) != 1 || it.Parts[0].Size != it.Size {
		t.Errorf("Dress.zip: %+v", it)
	}
	if it := got["Shoes_download_v2.zip"]; it.Asset != "name:shoes" || it.AssetName != "Shoes" {
		t.Errorf("Shoes: %+v", it)
	}
	if p.Arc.Archives != 7 || p.Arc.Unreadable != 1 {
		t.Errorf("looked at %d, unreadable %d", p.Arc.Archives, p.Arc.Unreadable)
	}

	var trash []string
	recycleFiles = func(ps []string) error {
		for _, p := range ps {
			trash = append(trash, filepath.Base(p))
			_ = os.Remove(p)
		}
		return nil
	}
	defer func() { recycleFiles = core.RecycleOnly }()
	if _, err := TidyRecycle(st, "arc", []string{filepath.Join(root, "Packed.zip")}, dl, p.Arc.Seq); err == nil {
		t.Error("an archive that is not unpacked was accepted")
	}
	if _, err := TidyRecycle(st, "arc", []string{filepath.Join(root, "Dress.zip")}, dl, p.Arc.Seq+5); err != errTidyMoved || len(trash) != 0 {
		t.Errorf("a result the player has not seen: %v %v", err, trash)
	}
	res, err := TidyRecycle(st, "arc", []string{filepath.Join(root, "Dress.zip"), filepath.Join(dl, "9 Item", "Bag.zip")}, dl, p.Arc.Seq)
	if err != nil || res.Removed != 2 || len(res.Failed) != 0 {
		t.Fatalf("recycled %+v %v", res, err)
	}
	sort.Strings(trash)
	if strings.Join(trash, ",") != "Bag.zip,Dress.zip" || core.StatOK(filepath.Join(root, "Dress.zip")) || !core.StatOK(filepath.Join(root, "Dress v1", "a.unitypackage")) {
		t.Errorf("trash %v", trash)
	}
	if p := waitTidy(t, st, "arc", dl); p.Arc == nil || len(p.Arc.Items) != 1 || p.Arc.Items[0].Main != filepath.Join(root, "Shoes_download_v2.zip") {
		t.Errorf("after: %+v", p)
	}
}

// Every entry of an archive is compared with the files on disk, however many there are: one that is missing
// or has another size is enough to say no.
func TestUnpackedAtChecksEveryEntry(t *testing.T) {
	dir := t.TempDir()
	var entries []archive.Entry
	for i := 0; i < 300; i++ {
		n := "Pack/f" + core.Itoa(i) + ".png"
		entries = append(entries, archive.Entry{Name: n, Size: int64(10 + i)})
		writeBytes(t, filepath.Join(dir, filepath.FromSlash(n)), bytes.Repeat([]byte("x"), 10+i))
	}
	entries = append(entries, archive.Entry{Name: "__MACOSX/Pack/._f1.png", Size: 120}) // (not counted, not looked for)
	s := archive.Set{Main: filepath.Join(dir, "Pack.zip"), Parts: []string{filepath.Join(dir, "Pack.zip")}, Name: "Pack"}
	folder, checked := unpackedAt(s, entries, nil, "")
	if folder != filepath.Join(dir, "Pack") || checked != 300 {
		t.Fatalf("got %q, %d checked", folder, checked)
	}
	if folder, _ := unpackedAt(s, entries, nil, filepath.Join(dir, "Pack")); folder != filepath.Join(dir, "Pack") {
		t.Errorf("asked of the folder it is in: %q", folder)
	}
	if folder, _ := unpackedAt(s, entries, nil, filepath.Join(dir, "Elsewhere")); folder != "" {
		t.Errorf("asked of another folder: %q", folder)
	}
	for _, i := range []int{299, 137, 1} { // the biggest one, and two a sample of 40 would not have looked at
		p := filepath.Join(dir, "Pack", "f"+core.Itoa(i)+".png")
		was, _ := os.ReadFile(p)
		writeBytes(t, p, []byte("short"))
		if folder, _ := unpackedAt(s, entries, nil, ""); folder != "" {
			t.Errorf("accepted with f%d of another size: %s", i, folder)
		}
		_ = os.Remove(p)
		if folder, _ := unpackedAt(s, entries, nil, ""); folder != "" {
			t.Errorf("accepted with f%d missing: %s", i, folder)
		}
		writeBytes(t, p, was)
	}
	if folder, _ := unpackedAt(s, entries, nil, ""); folder == "" {
		t.Error("not found again with every file back")
	}
	if folder, _ := unpackedAt(s, []archive.Entry{{Name: "../../etc/passwd", Size: 1}}, nil, ""); folder != "" {
		t.Errorf("an entry outside the archive's folder: %s", folder)
	}
	// empty files only: their names alone do not say that this is the archive's content
	writeBytes(t, filepath.Join(dir, "Empty", "a.txt"), nil)
	if folder, _ := unpackedAt(archive.Set{Main: filepath.Join(dir, "Empty.zip"), Name: "Empty"}, []archive.Entry{{Name: "a.txt"}}, nil, ""); folder != "" {
		t.Errorf("empty files only: %s", folder)
	}
}

// aged: the file is not "just written" for the archive scan.
func aged(t *testing.T, p string) {
	t.Helper()
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}

// An archive whose unpack lost files is not "already unpacked", wherever in its list the missing ones are.
func TestArchiveScanMissingFiles(t *testing.T) {
	st, root, dl := tidyStore(t)
	files := map[string]string{}
	for i := 0; i < 400; i++ {
		files[fmt.Sprintf("Dress/tex/f%03d.bin", i)] = string(blob(byte(i), 1000))
	}
	zp := filepath.Join(root, "Dress.zip")
	testkit.MakeZip(t, zp, files)
	for n, c := range files {
		writeBytes(t, filepath.Join(root, n), []byte(c))
	}
	aged(t, zp)
	scan := func() *ArcResult {
		t.Helper()
		if err := TidyStart(st, "arc", dl, 0); err != nil {
			t.Fatal(err)
		}
		p := waitTidy(t, st, "arc", dl)
		if p.Arc == nil {
			t.Fatalf("no result: %+v", p.Run)
		}
		return p.Arc
	}
	if r := scan(); len(r.Items) != 1 || r.Items[0].Checked != 400 || r.Items[0].Entries != 400 {
		t.Fatalf("all files there: %+v", r.Items)
	}
	_ = os.Remove(filepath.Join(root, "Dress", "tex", "f217.bin")) // one of 400
	if r := scan(); len(r.Items) != 0 {
		t.Errorf("listed as unpacked with a file missing: %+v", r.Items)
	}
}

// "Dress.zip" and "Dress.7z" in one folder are two archives: the 7z is not listed, or recycled, as a volume
// of the zip that is unpacked.
func TestArchiveScanSameNameArchives(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z here")
	}
	st, root, dl := tidyStore(t)
	recycleFiles = func(ps []string) error { return archive.RemoveFiles(ps) }
	defer func() { recycleFiles = core.RecycleOnly }()
	zp, sz := filepath.Join(root, "Dress.zip"), filepath.Join(root, "Dress.7z")
	testkit.MakeZip(t, zp, map[string]string{"Dress/a.bin": string(blob(1, 5000))})
	writeBytes(t, filepath.Join(root, "Dress", "a.bin"), blob(1, 5000)) // the zip is unpacked
	src := filepath.Join(t.TempDir(), "layers.psd")                     // the 7z (the texture sources) never was
	writeBytes(t, src, blob(9, 90000))
	if out, err := exec.Command("7z", "a", sz, src).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	aged(t, zp)
	aged(t, sz)
	if err := TidyStart(st, "arc", dl, 0); err != nil {
		t.Fatal(err)
	}
	p := waitTidy(t, st, "arc", dl)
	if p.Arc == nil || len(p.Arc.Items) != 1 || p.Arc.Archives != 2 {
		t.Fatalf("scan: %+v", p.Arc)
	}
	it := p.Arc.Items[0]
	if it.Main != zp || len(it.Parts) != 1 || it.Parts[0].Path != zp {
		t.Fatalf("listed: %+v", it)
	}
	res, err := TidyRecycle(st, "arc", []string{zp}, dl, p.Arc.Seq)
	if err != nil || res.Removed != 1 || core.StatOK(zp) || !core.StatOK(sz) {
		t.Errorf("recycle: %+v %v; the zip there: %v, the 7z there: %v", res, err, core.StatOK(zp), core.StatOK(sz))
	}
}

// A copy goes to the Recycle Bin only while the copy that stays is still what the scan saw.
func TestDupRecycleChecksKeptCopy(t *testing.T) {
	st, root, dl := tidyStore(t)
	var trashed []string
	recycleFiles = func(ps []string) error {
		trashed = append(trashed, ps...)
		return archive.RemoveFiles(ps)
	}
	defer func() { recycleFiles = core.RecycleOnly }()
	keep, pick := filepath.Join(root, "Dress", "tex.psd"), filepath.Join(dl, "tex.psd")
	st.Assets = []*core.Asset{{Key: "name:dress", Name: "Dress", Locations: []core.Location{{Path: filepath.Join(root, "Dress"), Kind: "dir", Root: root}}}}
	scan := func() *DupResult {
		t.Helper()
		writeBytes(t, keep, blob(3, 300<<10))
		writeBytes(t, pick, blob(3, 300<<10))
		if err := TidyStart(st, "dup", dl, 64<<10); err != nil {
			t.Fatal(err)
		}
		p := waitTidy(t, st, "dup", dl)
		if p.Dup == nil || len(p.Dup.Groups) != 1 {
			t.Fatalf("scan: %+v", p)
		}
		return p.Dup
	}
	stays := func(what, why string, r *DupResult) {
		t.Helper()
		res, err := TidyRecycle(st, "dup", []string{pick}, dl, r.Seq)
		if err != nil || res.Removed != 0 || len(res.Failed) != 1 || res.Failed[0].Err != why || len(trashed) != 0 || !core.StatOK(pick) {
			t.Fatalf("%s: %+v %v, trashed %v", what, res, err, trashed)
		}
	}
	// 1. the copy that was to stay is painted over and saved — the same size, and (within the second, or by a
	// program that puts the date back) the same date
	r := scan()
	fi, _ := os.Stat(keep)
	writeBytes(t, keep, blob(77, 300<<10))
	_ = os.Chtimes(keep, fi.ModTime(), fi.ModTime())
	stays("the kept copy has other content", errDupDiffers, r)
	// 2. … saved at another size; 3. … deleted
	r = scan()
	writeBytes(t, keep, blob(3, 200<<10))
	stays("the kept copy has another size", errKeptChanged, r)
	r = scan()
	_ = os.Remove(keep)
	stays("the kept copy is gone", errKeptChanged, r)
	// 4. the picked copy itself has other content under the same size and date: it is not the spare one
	r = scan()
	fi, _ = os.Stat(pick)
	writeBytes(t, pick, blob(78, 300<<10))
	_ = os.Chtimes(pick, fi.ModTime(), fi.ModTime())
	stays("the picked copy has other content", errDupDiffers, r)
	// 5. nothing changed: the picked copy goes. A second request for the other copy, confirmed against the same
	// result, finds the group gone and takes nothing
	r = scan()
	if res, err := TidyRecycle(st, "dup", []string{pick}, dl, r.Seq); err != nil || res.Removed != 1 || core.StatOK(pick) {
		t.Fatalf("unchanged: %+v %v", res, err)
	}
	if res, err := TidyRecycle(st, "dup", []string{keep}, dl, r.Seq); err == nil || res.Removed != 0 || !core.StatOK(keep) {
		t.Fatalf("the last copy: %+v %v", res, err)
	}
}

// An archive listed as unpacked goes only while its unpacked files are still there.
func TestArcRecycleChecksFolder(t *testing.T) {
	st, root, dl := tidyStore(t)
	recycleFiles = func(ps []string) error { return archive.RemoveFiles(ps) }
	defer func() { recycleFiles = core.RecycleOnly }()
	files := map[string]string{}
	for i := 0; i < 5; i++ {
		files[fmt.Sprintf("Dress/f%d.bin", i)] = string(blob(byte(i), 2000+i))
	}
	zp := filepath.Join(root, "Dress.zip")
	scan := func() *ArcResult {
		t.Helper()
		testkit.MakeZip(t, zp, files)
		for n, c := range files {
			writeBytes(t, filepath.Join(root, n), []byte(c))
		}
		aged(t, zp)
		if err := TidyStart(st, "arc", dl, 0); err != nil {
			t.Fatal(err)
		}
		p := waitTidy(t, st, "arc", dl)
		if p.Arc == nil || len(p.Arc.Items) != 1 {
			t.Fatalf("scan: %+v", p.Arc)
		}
		return p.Arc
	}
	stays := func(what string, r *ArcResult) {
		t.Helper()
		res, err := TidyRecycle(st, "arc", []string{zp}, dl, r.Seq)
		if err != nil || res.Removed != 0 || len(res.Failed) != 1 || res.Failed[0].Err != errUnpackedGone || !core.StatOK(zp) {
			t.Fatalf("%s: %+v %v", what, res, err)
		}
	}
	r := scan()
	if err := os.RemoveAll(filepath.Join(root, "Dress")); err != nil { // deleted in Explorer; no rescan has run yet
		t.Fatal(err)
	}
	stays("the unpacked folder is gone", r)
	r = scan()
	_ = os.Remove(filepath.Join(root, "Dress", "f3.bin"))
	stays("one unpacked file is gone", r)
	r = scan()
	writeBytes(t, filepath.Join(root, "Dress", "f2.bin"), []byte("cut short"))
	stays("one unpacked file has another size", r)
	r = scan()
	if res, err := TidyRecycle(st, "arc", []string{zp}, dl, r.Seq); err != nil || res.Removed != 1 || core.StatOK(zp) {
		t.Fatalf("unchanged: %+v %v", res, err)
	}
}

func TestCheckRemovable(t *testing.T) {
	root, dl, other := t.TempDir(), t.TempDir(), t.TempDir()
	proj := filepath.Join(root, "Proj")
	_ = os.MkdirAll(filepath.Join(proj, "ProjectSettings"), 0755)
	writeBytes(t, filepath.Join(proj, "Assets", "a.zip"), []byte("x"))
	writeBytes(t, filepath.Join(root, "Dress", "a.zip"), []byte("x"))
	writeBytes(t, filepath.Join(root, "Dress", "new", "b.zip"), []byte("x"))
	writeBytes(t, filepath.Join(other, "a.zip"), []byte("x"))
	_ = os.MkdirAll(filepath.Join(root, "Nested", "dl"), 0755)
	allowed := []string{root, dl, filepath.Join(root, "Nested", "dl")}
	cases := []struct {
		p    string
		keep []string
		ok   bool
	}{
		{filepath.Join(root, "Dress", "a.zip"), nil, true},
		{filepath.Join(root, "Dress"), nil, true},
		{filepath.Join(root, "Dress"), []string{filepath.Join(root, "Dress", "new")}, false}, // holds what was just downloaded
		{root, nil, false},                                   // an asset folder itself
		{filepath.Join(root, "Nested"), nil, false},          // holds the download folder
		{filepath.Join(other, "a.zip"), nil, false},          // outside the library
		{filepath.Join(proj, "Assets", "a.zip"), nil, false}, // inside a Unity project
		{proj, nil, false},
		{filepath.Join(root, "Dress", "..", "..", filepath.Base(other), "a.zip"), nil, false},
		{filepath.Join(root, "gone.zip"), nil, false},
	}
	for _, c := range cases {
		if err := CheckRemovable(c.p, allowed, nil, c.keep); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.p, err)
		}
	}
	if err := CheckRemovable(filepath.Join(root, "Dress"), allowed, []string{filepath.Join(root, "Dress", "new")}, nil); err == nil {
		t.Error("a folder holding a project folder was accepted")
	}
	// an asset folder that itself lies inside a Unity project: its files are the project's
	inProj := filepath.Join(proj, "Assets", "Imports")
	writeBytes(t, filepath.Join(inProj, "Hat", "hat.zip"), []byte("x"))
	if err := CheckRemovable(filepath.Join(inProj, "Hat", "hat.zip"), []string{inProj}, nil, nil); err == nil {
		t.Error("a file of an asset folder inside a Unity project was accepted")
	}
	var seen, warned []string
	_ = tidyWalk(context.Background(), []string{inProj, filepath.Join(root, "Dress")}, func(f walked) { seen = append(seen, f.path) }, func(w string) { warned = append(warned, w) })
	if len(seen) != 2 || len(warned) != 1 || !strings.Contains(warned[0], "Unity 工程") {
		t.Errorf("walked %v, warned %v", seen, warned)
	}
	link := filepath.Join(root, "link")
	if os.Symlink(other, link) == nil {
		if err := CheckRemovable(link, allowed, nil, nil); err == nil {
			t.Error("a link was accepted")
		}
	}
}
