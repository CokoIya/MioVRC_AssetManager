package purchases

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
)

func TestDownloadJob(t *testing.T) {
	core.DataDir = t.TempDir()
	lib := t.TempDir()
	var zipBytes bytes.Buffer
	zw := zip.NewWriter(&zipBytes)
	w, _ := zw.Create("MoonDress/MoonDress.unitypackage")
	_, _ = w.Write([]byte("pkg"))
	_ = zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/downloadables/"):
			if !strings.Contains(r.Header.Get("Cookie"), boothSessionCookie+"=good") {
				http.Redirect(w, r, "/users/sign_in", http.StatusFound)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: boothSessionCookie, Value: "good", Path: "/"})
			http.Redirect(w, r, "/cdn/abc/MoonDress_v1.2.zip", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			_, _ = w.Write(zipBytes.Bytes())
		}
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_BOOTH_DL", srv.URL)
	t.Setenv("VRCLIB_BOOTH_BASE", srv.URL)
	st := &core.Store{Path: filepath.Join(core.DataDir, "library.json"), Settings: core.Settings{Roots: []string{lib}}, Purchases: map[string]*core.Purchase{
		"9000003": {ID: "9000003", Name: "【8アバター対応】Moon Dress", Files: []string{"MoonDress_v1.2.zip"}, Downloads: []string{"90000030"}}}}

	// no login saved yet
	if _, _, err := resolveDownload(context.Background(), st, "90000030"); !errors.Is(err, errNeedLogin) {
		t.Fatalf("without a login: %v", err)
	}
	_ = saveBoothSession([]core.SavedCookie{{Name: boothSessionCookie, Value: "bad"}})
	if _, _, err := resolveDownload(context.Background(), st, "90000030"); !errors.Is(err, errNeedLogin) {
		t.Fatalf("expired login: %v", err)
	}
	_ = saveBoothSession([]core.SavedCookie{{Name: boothSessionCookie, Value: "good", Expires: time.Now().Add(time.Hour).Unix()}})
	j := &DLJob{ID: "90000030", Item: "9000003", Name: "MoonDress_v1.2.zip", Status: "running"}
	if err := downloadJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(lib, "9000003 【8アバター対応】Moon Dress") // the item's folder: everything unpacks in there
	if j.Path != want || j.Status != "done" {
		t.Errorf("job: %+v", j)
	}
	if got := strings.Join(testkit.ListTree(filepath.Join(lib, "9000003 【8アバター対応】Moon Dress")), ","); got != "MoonDress/MoonDress.unitypackage" {
		t.Errorf("item folder (zip removed after unpacking): %s", got)
	}
	if naming.BoothIDFromName("9000003 【8アバター対応】Moon Dress") != "9000003" {
		t.Error("the folder name has to carry the item id")
	}
	if st.Downloaded["90000030"] == nil {
		t.Error("not recorded")
	}
}

// boothServer stands in for Booth's downloads: each downloadable id leads to a file of the given name.
func boothServer(t *testing.T, files map[string][2]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/downloadables/"):
			id := strings.TrimPrefix(r.URL.Path, "/downloadables/")
			http.Redirect(w, r, "/cdn/"+id+"/"+files[id][0], http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			_, _ = w.Write([]byte(files[strings.Split(r.URL.Path, "/")[2]][1]))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_BOOTH_DL", srv.URL)
	t.Setenv("VRCLIB_BOOTH_BASE", srv.URL)
	_ = saveBoothSession([]core.SavedCookie{{Name: boothSessionCookie, Value: "good", Expires: time.Now().Add(time.Hour).Unix()}})
}

func settlePipeline(t *testing.T) { t.Cleanup(settleBackground) }

// A download into an item folder that is there already (an update, found by the Booth id in the folder's
// name) unpacks and removes only what it brought: the archives the player keeps packed there stay.
func TestDownloadLeavesPackedArchivesInItemFolder(t *testing.T) {
	core.DataDir = t.TempDir()
	lib := t.TempDir()
	boothServer(t, map[string][2]string{"90000031": {"MoonDress_v1.1.zip", string(testkit.ZipBytes(t, map[string][]byte{"MoonDress_v1.1/MoonDress.unitypackage": []byte("NEW VERSION")}))}})
	st := &core.Store{Path: filepath.Join(core.DataDir, "library.json"), Settings: core.Settings{Roots: []string{lib}, NoWatch: true, Proxy: "direct"}, Purchases: map[string]*core.Purchase{
		"9000003": {ID: "9000003", Name: "Moon Dress", Files: []string{"MoonDress_v1.1.zip"}, Downloads: []string{"90000031"}}}}
	settlePipeline(t)
	folder := filepath.Join(lib, "9000003 月のドレス")
	mine := map[string][]byte{}
	for n, e := range map[string]string{"MoonDress_v1.0.zip": "MoonDress_v1.0/MoonDress.unitypackage", "MoonDress_PSD.zip": "tex.psd", "original/Extra.zip": "extra.txt"} {
		p := filepath.Join(folder, filepath.FromSlash(n))
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		testkit.MakeZip(t, p, map[string]string{e: "OLD VERSION"})
		mine[p], _ = os.ReadFile(p)
	}
	j := &DLJob{ID: "90000031", Item: "9000003", Name: "MoonDress_v1.1.zip", Status: "running"}
	if err := downloadJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	for p, was := range mine {
		if now, err := os.ReadFile(p); err != nil || !bytes.Equal(now, was) {
			t.Errorf("the player's %s was touched (%v)", filepath.Base(p), err)
		}
	}
	got := strings.Join(testkit.ListTree(folder), ",")
	if got != "MoonDress_PSD.zip,MoonDress_v1.0.zip,MoonDress_v1.1/MoonDress.unitypackage,original/Extra.zip" {
		t.Errorf("item folder: %s", got) // the new zip unpacked and removed, nothing else unpacked
	}
	if j.Path != folder || j.Status != "done" {
		t.Errorf("job: %+v", j)
	}
}

// "下载并导入": the import that follows is given what this round of downloads brought — the folder its archive was
// unpacked to, or the archive itself where it stays packed — and not the item's folder, where it would unpack
// (and put in the Recycle Bin) what the player keeps packed there: an earlier version, a download kept packed.
func TestImportAfterDownloadTakesWhatTheDownloadBrought(t *testing.T) {
	newZip := testkit.ZipBytes(t, map[string][]byte{"MoonDress_v1.1/MoonDress.unitypackage": []byte("NEW VERSION")})
	var got []unity.ImportReq
	start := startImport
	startImport = func(st *core.Store, req unity.ImportReq) { got = append(got, req) }
	t.Cleanup(func() { startImport = start })
	run := func(noExtract, keepZip bool) (folder string, paths []string) {
		t.Helper()
		core.DataDir = t.TempDir()
		lib := t.TempDir()
		boothServer(t, map[string][2]string{"90000031": {"MoonDress_v1.1.zip", string(newZip)}})
		st := &core.Store{Path: filepath.Join(core.DataDir, "library.json"), Settings: core.Settings{Roots: []string{lib}, NoExtract: noExtract, KeepZip: keepZip, NoWatch: true, Proxy: "direct"},
			Purchases: map[string]*core.Purchase{"9000003": {ID: "9000003", Name: "Moon Dress", Files: []string{"MoonDress_v1.1.zip"}, Downloads: []string{"90000031"}}}}
		settlePipeline(t)
		folder = filepath.Join(lib, "9000003 Moon Dress")
		_ = os.MkdirAll(folder, 0755)
		testkit.MakeZip(t, filepath.Join(folder, "MoonDress_v1.0.zip"), map[string]string{"MoonDress_v1.0/MoonDress.unitypackage": "OLD VERSION"})
		testkit.MakeZip(t, filepath.Join(folder, "MoonDress_PSD.zip"), map[string]string{"tex.psd": "kept packed"})
		got = nil
		SetPendingImport("9000003", unity.ImportReq{Project: "P", Recycle: true})
		j := &DLJob{ID: "90000031", Item: "9000003", Name: "MoonDress_v1.1.zip", Status: "running"}
		if err := downloadJob(context.Background(), st, j); err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Key != "purchase:9000003" || got[0].Project != "P" || !got[0].Recycle {
			t.Fatalf("import: %+v", got)
		}
		return folder, got[0].Paths
	}
	for _, c := range []struct {
		name               string
		noExtract, keepZip bool
		want               string
	}{
		{"unpacked", false, false, "MoonDress_v1.1"},
		{"kept packed (「不自动解压」): the import unpacks it", true, false, "MoonDress_v1.1.zip"},
		{"unpacked, the archive kept (解压后保留压缩包): the folder stands for it", false, true, "MoonDress_v1.1"},
	} {
		folder, paths := run(c.noExtract, c.keepZip)
		if len(paths) != 1 || paths[0] != filepath.Join(folder, c.want) {
			t.Errorf("%s: import of %v", c.name, paths)
		}
	}
	// nothing of the downloads there any more: the item's folder, as before
	folder := t.TempDir()
	got = nil
	SetPendingImport("9000004", unity.ImportReq{Project: "P"})
	keepForImport(&DLJob{Item: "9000004"}, []string{filepath.Join(folder, "gone.zip")})
	importAfterDownload(nil, &DLJob{Item: "9000004"}, folder)
	if len(got) != 1 || len(got[0].Paths) != 1 || got[0].Paths[0] != folder {
		t.Errorf("nothing there: %+v", got)
	}
	// a round put off by another file of the item queued meanwhile: both rounds are imported
	a, b := filepath.Join(folder, "A"), filepath.Join(folder, "B.unitypackage")
	_ = os.Mkdir(a, 0755)
	_ = os.WriteFile(b, []byte("pkg"), 0644)
	got = nil
	SetPendingImport("9000004", unity.ImportReq{Project: "P"})
	keepForImport(&DLJob{Item: "9000004"}, []string{a})
	keepForImport(&DLJob{Item: "9000004"}, []string{b, a})
	keepForImport(&DLJob{Item: "9000005"}, []string{b}) // (no import waits for that one)
	importAfterDownload(nil, &DLJob{Item: "9000004"}, folder)
	if len(got) != 1 || strings.Join(got[0].Paths, ",") != a+","+b {
		t.Errorf("two rounds: %+v", got)
	}
	pendMu.Lock()
	_, kept := pendImports["9000005"]
	pendMu.Unlock()
	if kept {
		t.Error("an import nobody asked for")
	}
}

// What a round brought, for the import: the outermost folders its archives were unpacked to, and the files no such
// folder stands for (the volumes of an archive that was unpacked and kept are not among them).
func TestBroughtPaths(t *testing.T) {
	d := t.TempDir()
	for _, n := range []string{"Tex.7z.001", "Tex.7z.002", "Hair.zip", "readme.txt", "Locked.zip"} {
		testkit.WriteFile(t, filepath.Join(d, n), 10)
	}
	for _, n := range []string{"Tex/PSD", "Hair"} {
		_ = os.MkdirAll(filepath.Join(d, n), 0755)
	}
	files := []string{"Tex.7z.001", "Tex.7z.002", "Hair.zip", "readme.txt", "Locked.zip", "Gone.zip"}
	for i, n := range files {
		files[i] = filepath.Join(d, n)
	}
	in := func(n ...string) []string {
		for i := range n {
			n[i] = filepath.Join(d, n[i])
		}
		return n
	}
	got := broughtPaths(files, in("Tex", "Tex/PSD", "Hair"), in("Tex.7z.001", "Tex/PSD.zip", "Hair.zip"))
	if want := in("Tex", "Hair", "readme.txt", "Locked.zip"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v", got)
	}
}

// A download never replaces a file that is there: with 「不自动解压」 an update's file of the same name goes
// next to the earlier one.
func TestDownloadNeverReplacesAFile(t *testing.T) {
	core.DataDir = t.TempDir()
	lib := t.TempDir()
	newZip := testkit.ZipBytes(t, map[string][]byte{"MoonDress/MoonDress.unitypackage": []byte("NEW VERSION")})
	boothServer(t, map[string][2]string{"90000031": {"MoonDress.zip", string(newZip)}})
	st := &core.Store{Path: filepath.Join(core.DataDir, "library.json"), Settings: core.Settings{Roots: []string{lib}, NoExtract: true, NoWatch: true, Proxy: "direct"}, Purchases: map[string]*core.Purchase{
		"9000003": {ID: "9000003", Name: "Moon Dress", Files: []string{"MoonDress.zip"}, Downloads: []string{"90000031"}}}}
	settlePipeline(t)
	folder := filepath.Join(lib, "9000003 Moon Dress")
	old := filepath.Join(folder, "MoonDress.zip")
	_ = os.MkdirAll(folder, 0755)
	testkit.MakeZip(t, old, map[string]string{"MoonDress/MoonDress.unitypackage": "OLD VERSION"})
	oldBytes, _ := os.ReadFile(old)
	for i, want := range []string{"MoonDress (2).zip", "MoonDress (3).zip"} {
		j := &DLJob{ID: "90000031", Item: "9000003", Name: "MoonDress.zip", Status: "running"}
		if err := downloadJob(context.Background(), st, j); err != nil {
			t.Fatal(err)
		}
		if now, _ := os.ReadFile(old); !bytes.Equal(now, oldBytes) {
			t.Fatalf("%d: the earlier version's archive was replaced", i)
		}
		if b, _ := os.ReadFile(filepath.Join(folder, want)); !bytes.Equal(b, newZip) || j.Path != filepath.Join(folder, want) {
			t.Fatalf("%d: the new file is not next to it: %v, job at %s", i, testkit.ListTree(folder), j.Path)
		}
	}
	// with unpacking on: the earlier archive still stays; the new one is unpacked (next to it) and removed
	st.Settings.NoExtract = false
	_ = os.Remove(filepath.Join(folder, "MoonDress (2).zip"))
	_ = os.Remove(filepath.Join(folder, "MoonDress (3).zip"))
	j := &DLJob{ID: "90000031", Item: "9000003", Name: "MoonDress.zip", Status: "running"}
	if err := downloadJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(testkit.ListTree(folder), ","); got != "MoonDress.zip,MoonDress/MoonDress.unitypackage" {
		t.Errorf("item folder: %s", got)
	}
	if now, _ := os.ReadFile(old); !bytes.Equal(now, oldBytes) {
		t.Error("the earlier version's archive was replaced")
	}
	if b, _ := os.ReadFile(filepath.Join(folder, "MoonDress", "MoonDress.unitypackage")); string(b) != "NEW VERSION" {
		t.Errorf("unpacked: %q", b)
	}
}

// A first download of an item with several files, into a folder of its own: as before, all of them are
// unpacked once the last one is here (volumes together), and the archives are removed.
func TestItemFilesUnpackedTogether(t *testing.T) {
	emptyQueue(t)
	st := testkit.NewStore(t)
	settlePipeline(t)
	lib := t.TempDir()
	st.Settings.Roots, st.Settings.NoWatch, st.Settings.HideZh = []string{lib}, true, true
	files := map[string][2]string{
		"70000011": {"Hair_A.zip", string(testkit.ZipBytes(t, map[string][]byte{"Hair_A/a.unitypackage": []byte("a")}))},
		"70000012": {"Hair_B.zip", string(testkit.ZipBytes(t, map[string][]byte{"b.unitypackage": []byte("b"), "PSD.zip": testkit.ZipBytes(t, map[string][]byte{"PSD/b.psd": []byte("psd")})}))},
		"70000013": {"readme.txt", "hello"}, // the last file is no archive: the ones before it are unpacked all the same
	}
	want := "Hair_A/a.unitypackage,Hair_B/PSD/b.psd,Hair_B/b.unitypackage,readme.txt"
	p := &core.Purchase{ID: "7000001", Name: "Hair", Files: []string{"Hair_A.zip", "Hair_B.zip"}, Downloads: []string{"70000011", "70000012"}}
	if _, err := exec.LookPath("7z"); err == nil { // … and a 7z in two volumes
		src := filepath.Join(t.TempDir(), "Tex")
		_ = os.MkdirAll(src, 0755)
		_ = os.WriteFile(filepath.Join(src, "tex.bin"), bytes.Repeat([]byte("t"), 150000), 0644)
		vol := filepath.Join(t.TempDir(), "Tex.7z")
		if b, err := exec.Command("7z", "a", "-v100k", "-mx=0", vol, src).CombinedOutput(); err != nil {
			t.Fatalf("7z: %v %s", err, b)
		}
		for i, id := range []string{"70000014", "70000015"} {
			b, err := os.ReadFile(fmt.Sprintf("%s.%03d", vol, i+1))
			if err != nil {
				t.Fatal(err)
			}
			files[id] = [2]string{fmt.Sprintf("Tex.7z.%03d", i+1), string(b)}
			p.Files, p.Downloads = append(p.Files, files[id][0]), append(p.Downloads, id)
		}
		want = "Hair_A/a.unitypackage,Hair_B/PSD/b.psd,Hair_B/b.unitypackage,Tex/tex.bin,readme.txt"
	}
	p.Files, p.Downloads = append(p.Files, "readme.txt"), append(p.Downloads, "70000013")
	st.Purchases[p.ID] = p
	boothServer(t, files)
	if n, err := QueueDownloads(st, p.ID, nil); err != nil || n != len(p.Downloads) {
		t.Fatalf("queued %d: %v", n, err)
	}
	waitFor(t, "the downloads", func() bool {
		for _, j := range DLSnapshot() {
			if j.Status != "done" && j.Status != "failed" {
				return false
			}
		}
		return true
	})
	for _, j := range DLSnapshot() {
		if j.Status != "done" {
			t.Fatalf("job %+v", j)
		}
	}
	if got := strings.Join(testkit.ListTree(filepath.Join(lib, "7000001 Hair")), ","); got != want {
		t.Errorf("item folder: %s", got)
	}
}

// Volumes downloaded again while earlier ones of the same names are still there: the old files stay, and the
// new set gets one name for all its volumes, so it can still be opened.
func TestRedownloadKeepsVolumesTogether(t *testing.T) {
	emptyQueue(t)
	st := testkit.NewStore(t)
	settlePipeline(t)
	lib := t.TempDir()
	st.Settings.Roots, st.Settings.NoWatch, st.Settings.NoExtract, st.Settings.HideZh = []string{lib}, true, true, true
	names := []string{"Big.part1.rar", "Big.part2.rar", "Big.zip.001", "Big.zip.002", "Big.zip", "Big.z01", "readme.txt"}
	files := map[string][2]string{}
	p := &core.Purchase{ID: "7000002", Name: "Big"}
	for i, n := range names {
		id := fmt.Sprintf("7000002%d", i)
		files[id] = [2]string{n, "NEW " + n}
		p.Files, p.Downloads = append(p.Files, n), append(p.Downloads, id)
	}
	st.Purchases[p.ID] = p
	boothServer(t, files)
	folder := filepath.Join(lib, "7000002 Big")
	_ = os.MkdirAll(folder, 0755)
	old := []string{"Big.part2.rar", "Big.zip.001", "Big.z01", "readme.txt"} // what is left of an earlier download: not every volume
	for _, n := range old {
		_ = os.WriteFile(filepath.Join(folder, n), []byte("OLD "+n), 0644)
	}
	if n, err := QueueDownloads(st, p.ID, nil); err != nil || n != len(names) {
		t.Fatalf("queued %d: %v", n, err)
	}
	waitFor(t, "the downloads", func() bool {
		for _, j := range DLSnapshot() {
			if j.Status != "done" && j.Status != "failed" {
				return false
			}
		}
		return true
	})
	for _, n := range old {
		if b, _ := os.ReadFile(filepath.Join(folder, n)); string(b) != "OLD "+n {
			t.Errorf("the earlier %s was replaced: %q", n, b)
		}
	}
	for n, was := range map[string]string{"Big (2).part1.rar": "Big.part1.rar", "Big (2).part2.rar": "Big.part2.rar", "Big (2).zip.001": "Big.zip.001", "Big (2).zip.002": "Big.zip.002",
		"Big (2).zip": "Big.zip", "Big (2).z01": "Big.z01", "readme (2).txt": "readme.txt"} {
		if b, _ := os.ReadFile(filepath.Join(folder, n)); string(b) != "NEW "+was {
			t.Errorf("%s: %q", n, b)
		}
	}
	if got := testkit.ListTree(folder); len(got) != len(old)+len(names) {
		t.Errorf("item folder: %v", got)
	}
	// once more (the files of the round before are kept packed, and are not this round's): again one name a set
	if n, err := QueueDownloads(st, p.ID, nil); err != nil || n != len(names) {
		t.Fatalf("queued again %d: %v", n, err)
	}
	waitFor(t, "the downloads", func() bool {
		for _, j := range DLSnapshot() {
			if j.Status != "done" && j.Status != "failed" {
				return false
			}
		}
		return true
	})
	for _, n := range []string{"Big (3).part1.rar", "Big (3).part2.rar", "Big (3).zip.001", "Big (3).zip.002", "Big (3).zip", "Big (3).z01", "readme (3).txt"} {
		if b, _ := os.ReadFile(filepath.Join(folder, n)); !strings.HasPrefix(string(b), "NEW ") {
			t.Errorf("second round, %s: %q", n, b)
		}
	}
	if got := testkit.ListTree(folder); len(got) != len(old)+2*len(names) {
		t.Errorf("item folder after the second round: %v", got)
	}
}

func TestSessionFile(t *testing.T) {
	core.DataDir = t.TempDir()
	cs := []core.SavedCookie{{Name: boothSessionCookie, Value: "abc", Expires: time.Now().Add(time.Hour).Unix()}, {Name: "old", Value: "x", Expires: 1}}
	if err := saveBoothSession(cs); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(sessionFile())
	if runtime.GOOS == "windows" && bytes.Contains(raw, []byte("abc")) {
		t.Error("the login is stored readable")
	}
	got := LoadBoothSession()
	if len(got) != 1 || got[0].Value != "abc" {
		t.Errorf("loaded %+v", got)
	}
	if h := cookieHeader(got); h != "adult=t; "+boothSessionCookie+"=abc" {
		t.Errorf("header %q", h)
	}
}

// A set whose first volume came in a round of its own (「缺少后面的分卷」, and the player fetched the rest later):
// once the set is whole it is unpacked, and every volume this program downloaded goes with it — the earlier one
// too, which is no file of the player's.
func TestEarlierVolumeOfTheSetGoesWithIt(t *testing.T) {
	core.DataDir = t.TempDir()
	lib := t.TempDir()
	zip := testkit.ZipBytes(t, map[string][]byte{"Coat/Coat.unitypackage": bytes.Repeat([]byte("c"), 4000)})
	half := len(zip) / 2
	boothServer(t, map[string][2]string{
		"80000011": {"Coat.zip.001", string(zip[:half])},
		"80000012": {"Coat.zip.002", string(zip[half:])},
	})
	st := &core.Store{Path: filepath.Join(core.DataDir, "library.json"), Settings: core.Settings{Roots: []string{lib}, NoWatch: true, Proxy: "direct", HideZh: true}, Purchases: map[string]*core.Purchase{
		"8000001": {ID: "8000001", Name: "Coat", Files: []string{"Coat.zip.001", "Coat.zip.002"}, Downloads: []string{"80000011", "80000012"}}}}
	settlePipeline(t)
	folder := filepath.Join(lib, "8000001 Coat")
	for _, id := range []string{"80000011", "80000012"} { // one round each
		j := &DLJob{ID: id, Item: "8000001", Status: "running"}
		if err := downloadJob(context.Background(), st, j); err != nil {
			t.Fatal(err)
		}
		if id == "80000011" {
			if got := strings.Join(testkit.ListTree(folder), ","); got != "Coat.zip.001" {
				t.Fatalf("after the first volume: %s", got)
			}
		}
	}
	if got := strings.Join(testkit.ListTree(folder), ","); got != "Coat/Coat.unitypackage" {
		t.Errorf("item folder: %s", got)
	}
}
