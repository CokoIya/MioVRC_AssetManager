package follow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/pandl"
	"vrclib/internal/purchases"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
)

type fake struct {
	mu       sync.Mutex
	st       *core.Store
	root     string
	dl       []purchases.DLJob
	pan      []pandl.PanJob
	imports  []string // "project: files"
	failProj string
	hold     chan struct{} // the import into holdProj waits here
	holdProj string
	trash    []string
	stopped  int
}

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
}

func project(t *testing.T, st *core.Store, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	for _, d := range []string{"Assets", "ProjectSettings"} {
		_ = os.MkdirAll(filepath.Join(p, d), 0755)
	}
	st.Projects = append(st.Projects, core.ProjectInfo{Name: name, Path: p})
	return p
}

// setup: an asset "Kaguya" (v1.06 on disk) whose purchase offers v1.07, and stand-ins for the download
// queues and the import.
func setup(t *testing.T) (*fake, *core.Store) {
	t.Helper()
	settle() // an earlier part of the same test may still have the library scanned
	st := testkit.NewStore(t)
	f := &fake{st: st, root: t.TempDir()}
	old := filepath.Join(f.root, "9000001 Kaguya", "Kaguya_v1.06")
	write(t, filepath.Join(old, "Kaguya_v1.06.unitypackage"))
	st.Settings.Roots, st.Settings.DownloadDir = []string{f.root}, f.root
	st.Settings.NoWatch, st.Settings.AutoBooth = true, false
	st.Assets = []*core.Asset{{Key: "booth:9000001", Name: "Kaguya", RawName: "9000001 Kaguya", BoothID: "9000001", HasDir: true,
		Locations: []core.Location{{Path: old, Kind: "dir", Size: 1, Root: f.root}}}}
	st.Purchases["9000001"] = &core.Purchase{ID: "9000001", Name: "Kaguya", Files: []string{"Kaguya_v1.06.zip"}, Downloads: []string{"10"}}
	library.UpdateNotes(st)
	st.Purchases["9000001"] = &core.Purchase{ID: "9000001", Name: "Kaguya", Files: []string{"Kaguya_v1.07.zip"}, Downloads: []string{"12"}}

	mu.Lock()
	jobs, running = nil, false
	mu.Unlock()
	queueBooth = func(st *core.Store, item string, ids []string) (int, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, id := range ids {
			f.dl = append(f.dl, purchases.DLJob{ID: id, Item: item, Status: "queued"})
		}
		return len(ids), nil
	}
	boothJobs = func() []purchases.DLJob {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]purchases.DLJob{}, f.dl...)
	}
	cancelBooth = func() { f.mu.Lock(); f.stopped++; f.mu.Unlock() }
	queuePan = func(st *core.Store, key string, paths []string, imp *unity.ImportReq) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pan = append(f.pan, pandl.PanJob{ID: 1, Key: key, Stage: "queued", Paths: paths})
		return nil
	}
	panJobs = func() []pandl.PanJob {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]pandl.PanJob{}, f.pan...)
	}
	cancelPan = func() { f.mu.Lock(); f.stopped++; f.mu.Unlock() }
	importInto = func(ctx context.Context, st *core.Store, req unity.ImportReq, on func(unity.ImportJob)) (unity.ImportJob, error) {
		f.mu.Lock()
		hold := f.hold
		if f.holdProj != filepath.Base(req.Project) {
			hold = nil
		}
		f.mu.Unlock()
		if hold != nil {
			select {
			case <-hold:
			case <-ctx.Done():
				return unity.ImportJob{}, ctx.Err()
			}
		}
		var names []string
		for _, p := range req.Paths {
			names = append(names, filepath.Base(p))
		}
		f.mu.Lock()
		f.imports = append(f.imports, filepath.Base(req.Project)+": "+strings.Join(names, ","))
		bad := f.failProj == filepath.Base(req.Project)
		f.mu.Unlock()
		if bad {
			return unity.ImportJob{Stage: "failed", Err: "无法写入 Assets/x：文件可能被 Unity 占用"}, nil
		}
		return unity.ImportJob{Stage: "done", Imported: names, Files: 7}, nil
	}
	recycle = func(ps []string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.trash = append(f.trash, ps...)
		for _, p := range ps {
			_ = os.RemoveAll(p)
		}
		return nil
	}
	p := poll
	poll = 5 * time.Millisecond
	t.Cleanup(func() {
		settle() // (the stand-ins stay until nothing uses them any more)
		poll = p
		queueBooth, boothJobs, cancelBooth = purchases.QueueDownloads, purchases.DLSnapshot, purchases.CancelDownloads
		queuePan, panJobs, cancelPan = pandl.QueuePanDownload, pandl.PanJobsSnapshot, pandl.CancelPanDownloads
		importInto, recycle = unity.ImportAndWait, core.RecycleOnly
	})
	return f, st
}

// settle waits until no update and no scan is under way.
func settle() {
	for i := 0; i < 400 && (Active() || library.PipelineBusy()); i++ {
		time.Sleep(25 * time.Millisecond)
	}
}

// arrive: the download ends — the new version is unpacked next to the old one.
func (f *fake) arrive(t *testing.T) string {
	t.Helper()
	folder := filepath.Join(f.root, "9000001 Kaguya")
	write(t, filepath.Join(folder, "Kaguya_v1.07", "Kaguya_v1.07.unitypackage"))
	f.mu.Lock()
	for i := range f.dl {
		f.dl[i].Status, f.dl[i].Path = "done", folder
	}
	f.mu.Unlock()
	return folder
}

func waitJob(t *testing.T, id int64, ok func(Job) bool) Job {
	t.Helper()
	var last Job
	for i := 0; i < 600; i++ {
		for _, j := range Snapshot() {
			if j.ID == id {
				last = j
				if ok(j) {
					return j
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the job did not get there: %+v", last)
	return last
}

func ended(j Job) bool { return over(j.Stage) }

// downloading: the job has put its download into the queue (the stage alone is there a moment earlier).
func downloading(j Job) bool { return j.Stage == "download" && j.Msg != "排队中" }

func TestUpdateIntoProjects(t *testing.T) {
	f, st := setup(t)
	a, b, c := project(t, st, "ProjA"), project(t, st, "ProjB"), project(t, st, "ProjC")
	f.failProj = "ProjB"
	if _, err := Start(st, "booth:9000001", []string{filepath.Join(t.TempDir(), "NotAProject")}, false); err == nil {
		t.Fatal("a project that is not in the list was accepted")
	}
	if _, err := Start(st, "name:nothing", nil, false); err == nil {
		t.Fatal("an asset without an update was accepted")
	}
	j, err := Start(st, "booth:9000001", []string{a, b, c, a}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Projects) != 3 || !j.Recycle || j.Note.Version != "1.07" {
		t.Fatalf("queued: %+v", j)
	}
	if _, err := Start(st, "booth:9000001", nil, false); err == nil {
		t.Error("the same asset was queued twice")
	}
	waitJob(t, j.ID, downloading)
	f.mu.Lock()
	asked := len(f.dl) == 1 && f.dl[0].ID == "12" && f.dl[0].Item == "9000001"
	f.mu.Unlock()
	if !asked {
		t.Fatalf("what was downloaded: %+v", f.dl)
	}
	folder := f.arrive(t)
	got := waitJob(t, j.ID, ended)
	if got.Stage != "done" || got.Folder != folder || len(got.Packages) != 1 || filepath.Base(got.Packages[0]) != "Kaguya_v1.07.unitypackage" {
		t.Fatalf("ended: %+v", got)
	}
	// one after another, in order; the one that failed did not stop the next; only the new package went in
	f.mu.Lock()
	imports := strings.Join(f.imports, " | ")
	trash := len(f.trash)
	f.mu.Unlock()
	if imports != "ProjA: Kaguya_v1.07.unitypackage | ProjB: Kaguya_v1.07.unitypackage | ProjC: Kaguya_v1.07.unitypackage" {
		t.Errorf("imports: %s", imports)
	}
	st1, st2, st3 := got.Projects[0], got.Projects[1], got.Projects[2]
	if st1.Status != "done" || st1.Files != 7 || st1.Pkgs != 1 || st2.Status != "failed" || !strings.Contains(st2.Msg, "占用") || st3.Status != "done" {
		t.Errorf("projects: %+v", got.Projects)
	}
	// a project failed: the earlier version stays, whatever the player ticked
	if trash != 0 || len(got.Recycled) != 0 || !core.StatOK(filepath.Join(folder, "Kaguya_v1.06")) {
		t.Errorf("the old version went although a project failed: %v", f.trash)
	}
	// the asset has the new files: no update any more
	if n := library.UpdateNotes(st)["booth:9000001"]; n != nil {
		t.Errorf("still flagged: %+v", n)
	}
	// once more for the project that failed: no download, only that project
	f.failProj = ""
	r, err := Retry(st, j.ID)
	if err != nil || len(r.Projects) != 1 || r.Projects[0].Name != "ProjB" {
		t.Fatalf("retry: %+v %v", r, err)
	}
	got = waitJob(t, r.ID, ended)
	f.mu.Lock()
	n, dls := len(f.imports), len(f.dl)
	f.mu.Unlock()
	if got.Stage != "done" || got.Projects[0].Status != "done" || n != 4 || dls != 1 {
		t.Errorf("after the retry: %+v, %d imports, %d downloads", got, n, dls)
	}
	if len(Snapshot()) != 1 {
		t.Errorf("jobs listed for the asset: %d", len(Snapshot()))
	}
}

func TestUpdateRecyclesOldVersion(t *testing.T) {
	f, st := setup(t)
	a := project(t, st, "ProjA")
	j, err := Start(st, "booth:9000001", []string{a}, true)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, j.ID, downloading)
	folder := f.arrive(t)
	got := waitJob(t, j.ID, ended)
	old := filepath.Join(folder, "Kaguya_v1.06")
	if got.Stage != "done" || len(got.Recycled) != 1 || got.Recycled[0].Path != old || got.RecycleErr != "" || core.StatOK(old) || !core.StatOK(filepath.Join(folder, "Kaguya_v1.07")) {
		t.Fatalf("ended: %+v", got)
	}

	// kept unless asked: the same without the tick
	f, st = setup(t)
	j, _ = Start(st, "booth:9000001", nil, false)
	waitJob(t, j.ID, downloading)
	folder = f.arrive(t)
	got = waitJob(t, j.ID, ended)
	if got.Stage != "done" || len(f.trash) != 0 || !core.StatOK(filepath.Join(folder, "Kaguya_v1.06")) || len(got.Projects) != 0 {
		t.Fatalf("download only: %+v, trash %v", got, f.trash)
	}

	// the new files landed inside the old version's folder: it is not moved
	f, st = setup(t)
	j, _ = Start(st, "booth:9000001", nil, true)
	waitJob(t, j.ID, downloading)
	inOld := filepath.Join(f.root, "9000001 Kaguya", "Kaguya_v1.06")
	write(t, filepath.Join(inOld, "new", "Kaguya_v1.07.unitypackage"))
	f.mu.Lock()
	f.dl[0].Status, f.dl[0].Path = "done", filepath.Join(inOld, "new")
	f.mu.Unlock()
	got = waitJob(t, j.ID, ended)
	if got.Stage != "done" || len(f.trash) != 0 || got.RecycleErr == "" || !core.StatOK(inOld) {
		t.Fatalf("new files inside the old folder: %+v", got)
	}
}

func TestUpdateCancelAndFailure(t *testing.T) {
	// the download fails: no project is touched, the asset stays flagged
	f, st := setup(t)
	a, b := project(t, st, "ProjA"), project(t, st, "ProjB")
	j, _ := Start(st, "booth:9000001", []string{a, b}, false)
	waitJob(t, j.ID, downloading)
	f.mu.Lock()
	f.dl[0].Status, f.dl[0].Err = "failed", "Booth 上未找到该文件（可能已删除）"
	f.mu.Unlock()
	got := waitJob(t, j.ID, ended)
	if got.Stage != "failed" || !strings.Contains(got.Err, "未找到该文件") || got.Projects[0].Status != "skipped" || len(f.imports) != 0 {
		t.Fatalf("failed download: %+v", got)
	}
	if library.UpdateNotes(st)["booth:9000001"] == nil {
		t.Error("marked up to date although nothing was downloaded")
	}

	// waiting for a login is said, and goes on once the download does
	f, st = setup(t)
	j, _ = Start(st, "booth:9000001", nil, false)
	waitJob(t, j.ID, downloading)
	f.mu.Lock()
	f.dl[0].Status = "login"
	f.mu.Unlock()
	waitJob(t, j.ID, func(j Job) bool { return j.Login == "booth" })
	f.arrive(t)
	if got := waitJob(t, j.ID, ended); got.Stage != "done" || got.Login != "" {
		t.Fatalf("after the login: %+v", got)
	}

	// cancelled while downloading: the download is stopped (nothing else is downloading), no import
	f, st = setup(t)
	a = project(t, st, "ProjA")
	j, _ = Start(st, "booth:9000001", []string{a}, false)
	waitJob(t, j.ID, downloading)
	Cancel(j.ID)
	got = waitJob(t, j.ID, ended)
	if got.Stage != "cancelled" || got.Projects[0].Status != "cancelled" || f.stopped != 1 || len(f.imports) != 0 {
		t.Fatalf("cancelled download: %+v stopped=%d", got, f.stopped)
	}

	// … with another download under way that one is left alone
	f, st = setup(t)
	f.dl = []purchases.DLJob{{ID: "777", Item: "other", Status: "running"}}
	j, _ = Start(st, "booth:9000001", nil, false)
	waitJob(t, j.ID, downloading)
	Cancel(j.ID)
	if got = waitJob(t, j.ID, ended); got.Stage != "cancelled" || f.stopped != 0 {
		t.Fatalf("cancelled next to another download: %+v stopped=%d", got, f.stopped)
	}

	// cancelled between two projects: the first is done, the second never starts
	f, st = setup(t)
	a, b = project(t, st, "ProjA"), project(t, st, "ProjB")
	j, _ = Start(st, "booth:9000001", []string{a, b}, true)
	waitJob(t, j.ID, downloading)
	f.mu.Lock()
	f.hold, f.holdProj = make(chan struct{}), "ProjB"
	f.mu.Unlock()
	f.arrive(t)
	waitJob(t, j.ID, func(j Job) bool { return j.Projects[1].Status == "running" })
	Cancel(j.ID)
	got = waitJob(t, j.ID, ended)
	if got.Stage != "cancelled" || got.Projects[0].Status != "done" || got.Projects[1].Status != "cancelled" || len(f.trash) != 0 {
		t.Fatalf("cancelled between projects: %+v", got)
	}
	// a second job waits behind the first, and one that waits can be cancelled before it starts
	f, st = setup(t)
	st.Assets = append(st.Assets, &core.Asset{Key: "booth:2", Name: "Second", BoothID: "2", Locations: []core.Location{{Path: filepath.Join(f.root, "2 Second"), Kind: "dir", Root: f.root}}})
	st.Purchases["2"] = &core.Purchase{ID: "2", Files: []string{"S.zip"}, Downloads: []string{"20"}}
	library.UpdateNotes(st)
	st.Purchases["2"] = &core.Purchase{ID: "2", Files: []string{"S.zip"}, Downloads: []string{"21"}}
	j, _ = Start(st, "booth:9000001", nil, false)
	j2, err := Start(st, "booth:2", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, j.ID, downloading)
	if q := waitJob(t, j2.ID, func(Job) bool { return true }); q.Stage != "queued" {
		t.Fatalf("the second job: %+v", q)
	}
	Cancel(j2.ID)
	f.arrive(t)
	waitJob(t, j.ID, ended)
	if q := waitJob(t, j2.ID, ended); q.Stage != "cancelled" {
		t.Fatalf("the second job after being cancelled: %+v", q)
	}
	Dismiss(j2.ID)
	if len(Snapshot()) != 1 {
		t.Errorf("after closing its result: %d jobs", len(Snapshot()))
	}
}

func TestUpdateFromNetdisk(t *testing.T) {
	f, st := setup(t)
	dir := filepath.Join(f.root, "Moon Dress")
	write(t, filepath.Join(dir, "Moon.unitypackage"))
	st.Assets = []*core.Asset{{Key: "name:moondress", Name: "Moon Dress", HasDir: true, FirstSeen: 10, Locations: []core.Location{{Path: dir, Kind: "dir", Root: f.root}}}}
	st.User["pan:1Share"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1Share", Downloaded: dir, PanGot: []string{"/"}}
	st.Pan = map[string]*core.PanListing{"1Share": {Surl: "1Share", Fetched: 1, Files: []*core.PanFile{{Name: "Moon.unitypackage", Size: 1}}}}
	library.UpdateNotes(st)
	st.Pan["1Share"] = &core.PanListing{Surl: "1Share", Fetched: 2, Changed: 99, Files: []*core.PanFile{{Name: "Moon.unitypackage", Size: 2}, {Name: "Moon_Tex.unitypackage", Size: 5}}}
	a := project(t, st, "ProjA")
	j, err := Start(st, "name:moondress", []string{a}, false)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, j.ID, downloading)
	f.mu.Lock()
	if len(f.pan) != 1 || f.pan[0].Key != "pan:1Share" || strings.Join(f.pan[0].Paths, ",") != "/Moon_Tex.unitypackage,/Moon.unitypackage" {
		t.Errorf("asked of the netdisk: %+v", f.pan)
	}
	f.pan[0].Stage = "login"
	f.mu.Unlock()
	waitJob(t, j.ID, func(j Job) bool { return j.Login == "baidu" })
	// the replaced file is written again, the new one arrives
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "Moon.unitypackage"), []byte("xx"), 0644); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "Moon_Tex.unitypackage"))
	f.mu.Lock()
	f.pan[0].Stage, f.pan[0].Dir = "done", dir
	f.mu.Unlock()
	got := waitJob(t, j.ID, ended)
	if got.Stage != "done" || strings.Join(f.imports, "|") != "ProjA: Moon.unitypackage,Moon_Tex.unitypackage" {
		t.Fatalf("ended: %+v, imports %v", got, f.imports)
	}
	if n := library.UpdateNotes(st)["name:moondress"]; n != nil {
		t.Errorf("still flagged: %+v", n)
	}

	// a share linked by hand cannot be fetched by the program
	st.Assets = append(st.Assets, &core.Asset{Key: "name:hand", Name: "Hand", FirstSeen: 10, Locations: []core.Location{{Path: filepath.Join(f.root, "Hand"), Kind: "dir", Root: f.root}}})
	st.User["name:hand"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1Hand"}
	st.Pan["1Hand"] = &core.PanListing{Surl: "1Hand", Fetched: 1, Files: []*core.PanFile{{Name: "x.zip", Size: 10}}}
	library.UpdateNotes(st)
	st.Pan["1Hand"] = &core.PanListing{Surl: "1Hand", Fetched: 2, Changed: 500, Files: []*core.PanFile{{Name: "x.zip", Size: 11}}}
	if _, err := Start(st, "name:hand", nil, false); err == nil || !strings.Contains(err.Error(), "手动关联") {
		t.Errorf("a share linked by hand: %v", err)
	}
}

func TestNoNewPackages(t *testing.T) {
	// the new version brings no unitypackage (textures only, say): the projects are told so, nothing is imported
	f, st := setup(t)
	a := project(t, st, "ProjA")
	j, _ := Start(st, "booth:9000001", []string{a}, false)
	waitJob(t, j.ID, downloading)
	folder := filepath.Join(f.root, "9000001 Kaguya")
	write(t, filepath.Join(folder, "Kaguya_v1.07", "tex.png"))
	f.mu.Lock()
	f.dl[0].Status, f.dl[0].Path = "done", folder
	f.mu.Unlock()
	got := waitJob(t, j.ID, ended)
	if got.Stage != "done" || got.Projects[0].Status != "skipped" || len(f.imports) != 0 {
		t.Fatalf("ended: %+v", got)
	}
}

func TestReplacedArchiveIsUnpackedAgain(t *testing.T) {
	// the seller replaced Dress.zip under its name: the download leaves it packed, because the folder of the
	// earlier one is there. The update unpacks it next to that folder and imports what is inside
	f, st := setup(t)
	a := project(t, st, "ProjA")
	folder := filepath.Join(f.root, "9000001 Kaguya")
	write(t, filepath.Join(folder, "Dress", "Dress.unitypackage")) // the earlier one, unpacked
	j, _ := Start(st, "booth:9000001", []string{a}, false)
	waitJob(t, j.ID, downloading)
	testkit.MakeZip(t, filepath.Join(folder, "Dress.zip"), map[string]string{"Dress/Dress.unitypackage": "the new one", "Dress/readme.txt": "r"})
	f.mu.Lock()
	f.dl[0].Status, f.dl[0].Path = "done", folder
	f.mu.Unlock()
	got := waitJob(t, j.ID, ended)
	again := filepath.Join(folder, "Dress (2)", "Dress.unitypackage")
	if got.Stage != "done" || len(got.Packages) != 1 || got.Packages[0] != again || strings.Join(f.imports, "|") != "ProjA: Dress.unitypackage" {
		t.Fatalf("ended: %+v, imports %v", got, f.imports)
	}
	if b, _ := os.ReadFile(again); string(b) != "the new one" {
		t.Errorf("the new folder: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(folder, "Dress", "Dress.unitypackage")); string(b) != "x" {
		t.Errorf("the earlier folder was touched: %q", b)
	}
	if core.StatOK(filepath.Join(folder, "Dress.zip")) || strings.Join(f.trash, "|") != filepath.Join(folder, "Dress.zip") {
		t.Errorf("the archive should have gone to the Recycle Bin once unpacked (archives are not kept): %v", f.trash)
	}
}

// Two new archives that share a name ("Dress.zip", "Dress.7z") are two archives: the one that could not be
// unpacked does not go to the Recycle Bin as a "volume" of the one that was.
func TestReplacedArchivesOfOneName(t *testing.T) {
	f, st := setup(t)
	a := project(t, st, "ProjA")
	folder := filepath.Join(f.root, "9000001 Kaguya")
	write(t, filepath.Join(folder, "Dress", "Dress.unitypackage")) // the earlier one, unpacked
	j, _ := Start(st, "booth:9000001", []string{a}, false)
	waitJob(t, j.ID, downloading)
	testkit.MakeZip(t, filepath.Join(folder, "Dress.zip"), map[string]string{"Dress/Dress.unitypackage": "the new one"})
	other := filepath.Join(folder, "Dress.7z") // the texture sources, in a format that cannot be unpacked here
	if err := os.WriteFile(other, []byte("not really a 7z"), 0644); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.dl[0].Status, f.dl[0].Path = "done", folder
	f.mu.Unlock()
	got := waitJob(t, j.ID, ended)
	if got.Stage != "done" {
		t.Fatalf("ended: %+v", got)
	}
	if !core.StatOK(other) || strings.Join(f.trash, "|") != filepath.Join(folder, "Dress.zip") {
		t.Errorf("the 7z there: %v; to the Recycle Bin: %v", core.StatOK(other), f.trash)
	}
	if b, _ := os.ReadFile(filepath.Join(folder, "Dress (2)", "Dress.unitypackage")); string(b) != "the new one" {
		t.Errorf("the zip was not unpacked next to the earlier folder: %q", b)
	}
}
