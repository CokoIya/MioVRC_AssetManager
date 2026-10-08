package pandl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/cloudshare"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/netdisk"
	"vrclib/internal/purchases"
	"vrclib/internal/testkit"
)

// fakeCloud: drive.google.com (a shared folder with two files, through its share pages) and www.dropbox.com
// (one file) as one server. cut > 0: the connection is dropped after that many bytes of the first request
// for a file, so the next request has to continue it.
type fakeCloud struct {
	mu    sync.Mutex
	files map[string][]byte // Drive id → bytes
	names map[string]string // Drive id → name
	cut   int
	cuts  map[string]bool
	hits  map[string]int
	quota bool
}

func (f *fakeCloud) serve(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"e-`+name+`"`)
	from := 0
	if rg := r.Header.Get("Range"); rg != "" {
		_, _ = fmt.Sscanf(rg, "bytes=%d-", &from)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
		w.Header().Set("Content-Length", fmt.Sprint(len(data)-from))
		w.WriteHeader(206)
	} else {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	}
	body := data[from:]
	if f.cut > 0 && !f.cuts[name] && from == 0 && len(body) > f.cut {
		f.cuts[name] = true
		_, _ = w.Write(body[:f.cut])
		w.(http.Flusher).Flush()
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		c.Close()
		return
	}
	_, _ = w.Write(body)
}

func (f *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits[r.URL.Path]++
	q := r.URL.Query()
	switch {
	case r.URL.Path == "/embeddedfolderview":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Outfit Pack - Google Drive</title></head><body>`)
		for id, name := range f.names {
			fmt.Fprintf(w, `<div class="flip-entry"><a href="https://drive.google.com/file/d/%s/view?usp=drive_web"><div class="flip-entry-title">%s</div><div class="flip-entry-last-modified"><div>Oct 1</div></div></a></div>`, id, name)
		}
		fmt.Fprint(w, `</body></html>`)
	case r.URL.Path == "/uc":
		data, ok := f.files[q.Get("id")]
		switch {
		case !ok:
			w.WriteHeader(404)
		case f.quota:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<html><body><p class="uc-error-subcaption">Too many users have viewed or downloaded this file recently.</p></body></html>`)
		default:
			f.serve(w, r, f.names[q.Get("id")], data)
		}
	case strings.HasPrefix(r.URL.Path, "/s/dbox12345678901/") && q.Get("dl") == "1":
		f.serve(w, r, "Dress.zip", f.files["db"])
	default:
		w.WriteHeader(404)
	}
}

func newFakeCloud(t *testing.T) (*fakeCloud, *core.Store) {
	t.Helper()
	zipA := testkit.ZipBytes(t, map[string][]byte{"Outfit/outfit.unitypackage": bytes.Repeat([]byte("U"), 4000), "Outfit/readme.txt": []byte("hi")})
	f := &fakeCloud{files: map[string][]byte{"1FileOutfit00000000000000000": zipA, "1FileTexture0000000000000000": bytes.Repeat([]byte("P"), 3000), "db": testkit.ZipBytes(t, map[string][]byte{"Dress/dress.unitypackage": bytes.Repeat([]byte("D"), 2500)})},
		names: map[string]string{"1FileOutfit00000000000000000": "Outfit.zip", "1FileTexture0000000000000000": "Texture.png"}, cuts: map[string]bool{}, hits: map[string]int{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_GDRIVE_BASE", srv.URL)
	t.Setenv("VRCLIB_GDRIVE_API", srv.URL)
	t.Setenv("VRCLIB_GDRIVE_KEY", "")
	t.Setenv("VRCLIB_DROPBOX_BASE", srv.URL)
	t.Setenv("VRCLIB_BROWSER", "")
	st := testkit.NewStore(t)
	st.Settings.DownloadDir = t.TempDir()
	st.Settings.Roots = []string{st.Settings.DownloadDir}
	st.Settings.AutoBooth = false
	st.Settings.HideZh = true // (no translation of names behind the tests: it would go out to the net)
	st.User["gd:1Folder000000000000000000000"] = &core.UserData{ShareURL: "https://drive.google.com/drive/folders/1Folder000000000000000000000"}
	st.User["db:dbox12345678901"] = &core.UserData{ShareURL: "https://www.dropbox.com/s/dbox12345678901/Dress.zip"}
	pause, stall := cloudPause, purchases.StallAfter
	cloudPause, purchases.StallAfter = time.Millisecond, 500*time.Millisecond
	t.Cleanup(func() {
		cloudPause, purchases.StallAfter = pause, stall
		settleLibrary()
	})
	return f, st
}

// settleLibrary waits for what follows a download to end: the scan, and the look into unitypackages after it
// (both read the data folder, which the next test replaces).
func settleLibrary() {
	// (the translation of new names follows the scan, and reads the data folder too: until all of it is
	// over, three looks in a row)
	for i, idle := 0, 0; i < 400 && idle < 3; i++ {
		if library.BackgroundBusy() {
			idle = 0
		} else {
			idle++
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// A Drive folder card, its listing not read yet: read, fetched (one connection dropped on the way and
// continued where it stopped), unpacked, taken into the library.
func TestCloudDownloadDriveFolder(t *testing.T) {
	f, st := newFakeCloud(t)
	f.cut = 100
	j := &PanJob{ID: 1, Key: "gd:1Folder000000000000000000000", Title: netdisk.SharePlaceholder("gd:1Folder000000000000000000000"), Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "done" || j.Title != "Outfit Pack" || j.FileN != 2 || j.Total != int64(len(f.files["1FileOutfit00000000000000000"])+3000) || j.Done != j.Total {
		t.Errorf("job: %+v", *j)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "Outfit/outfit.unitypackage,Outfit/readme.txt,Texture.png" {
		t.Errorf("on disk: %s", got)
	}
	if filepath.Base(j.Dir) != "Outfit Pack" {
		t.Errorf("folder: %s", j.Dir)
	}
	st.Mu.RLock()
	u, l := st.User[j.Key], st.Pan["gd:1Folder000000000000000000000"]
	st.Mu.RUnlock()
	if u == nil || u.Downloaded != j.Dir || u.DownloadDir != "" || len(u.PanGot) != 1 || u.PanGot[0] != "/" {
		t.Errorf("user data: %+v", u)
	}
	if l == nil || l.Count != 2 || l.Title != "Outfit Pack" {
		t.Errorf("listing kept: %+v", l)
	}
	f.mu.Lock()
	uc, cuts := f.hits["/uc"], len(f.cuts)
	f.mu.Unlock()
	if cuts != 2 || uc != 4 { // each file: one request cut short, one that continued it
		t.Errorf("%d requests for 2 files, %d cut", uc, cuts)
	}
	if n, _ := filepath.Glob(filepath.Join(j.Dir, "*.part")); len(n) != 0 {
		t.Errorf("part files left: %v", n)
	}
}

// More of a card downloaded into the folder it has: only the files this download brought are unpacked and
// removed. Archives the player keeps packed in that folder stay as they are.
func TestCloudDownloadLeavesOtherArchives(t *testing.T) {
	_, st := newFakeCloud(t)
	dir := filepath.Join(st.Settings.DownloadDir, "Outfit Pack")
	for _, n := range []string{"Mine.zip", "keep/Old_v1.zip"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, n)), 0755)
		testkit.MakeZip(t, filepath.Join(dir, n), map[string]string{"x/a.txt": "the player's"})
	}
	st.User["gd:1Folder000000000000000000000"].Downloaded = dir
	j := &PanJob{ID: 1, Key: "gd:1Folder000000000000000000000", Title: "Outfit Pack", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(testkit.ListTree(dir), ","); j.Dir != dir || got != "Mine.zip,Outfit/outfit.unitypackage,Outfit/readme.txt,Texture.png,keep/Old_v1.zip" {
		t.Errorf("in %s: %s", j.Dir, got)
	}
}

// Only one file picked; the other is not asked for. A second pick of the same file fetches it again.
func TestCloudDownloadPicked(t *testing.T) {
	f, st := newFakeCloud(t)
	st.Settings.NoExtract = true
	j := &PanJob{ID: 1, Key: "gd:1Folder000000000000000000000", Title: "Outfit Pack", Stage: "save", Paths: []string{"/Texture.png"}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "Texture.png" || j.FileN != 1 {
		t.Errorf("picked: %s (%d files)", got, j.FileN)
	}
	f.mu.Lock()
	uc := f.hits["/uc"]
	f.mu.Unlock()
	if uc != 1 {
		t.Errorf("%d requests for one file", uc)
	}
	j2 := &PanJob{ID: 2, Key: "gd:1Folder000000000000000000000", Title: "Outfit Pack", Stage: "save", Paths: []string{"/Outfit.zip"}}
	if err := runPanJob(context.Background(), st, j2); err != nil {
		t.Fatal(err)
	}
	if j2.Dir != j.Dir || strings.Join(testkit.ListTree(j.Dir), ",") != "Outfit.zip,Texture.png" {
		t.Errorf("the second pick went to %s: %s", j2.Dir, testkit.ListTree(j2.Dir))
	}
	st.Mu.RLock()
	got := st.User[j.Key].PanGot
	st.Mu.RUnlock()
	if strings.Join(got, ",") != "/Outfit.zip,/Texture.png" {
		t.Errorf("recorded as got: %v", got)
	}
}

// A Dropbox link: one file, fetched with dl=1, unpacked.
func TestCloudDownloadDropbox(t *testing.T) {
	_, st := newFakeCloud(t)
	j := &PanJob{ID: 1, Key: "db:dbox12345678901", Title: netdisk.SharePlaceholder("db:dbox12345678901"), Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "done" || j.Title != "Dress" || strings.Join(testkit.ListTree(j.Dir), ",") != "Dress/dress.unitypackage" {
		t.Errorf("job %+v: %s", *j, testkit.ListTree(j.Dir))
	}
}

// What the service refuses for a reason of its own ends the job with that reason, after one request; a
// throttled file is said to be throttled.
func TestCloudDownloadRefused(t *testing.T) {
	f, st := newFakeCloud(t)
	f.quota = true
	j := &PanJob{ID: 1, Key: "gd:1Folder000000000000000000000", Title: "Outfit Pack", Stage: "save"}
	err := runPanJob(context.Background(), st, j)
	if !errors.Is(err, cloudshare.ErrGDQuota) {
		t.Errorf("throttled: %v", err)
	}
	f.mu.Lock()
	uc := f.hits["/uc"]
	f.mu.Unlock()
	if uc != 1 {
		t.Errorf("%d requests after a refusal", uc)
	}
	if n, _ := filepath.Glob(filepath.Join(j.Dir, "*.part")); len(n) != 0 {
		t.Errorf("part files left: %v", n)
	}
	// a web page where the file should be is never saved as the file
	open := cloudOpen
	cloudOpen = func(ctx context.Context, c *http.Client, src cloudshare.Source, from int64, ifRange string) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/octet-stream"}}, ContentLength: -1,
			Body: io.NopCloser(strings.NewReader("<!DOCTYPE html><html><body>maintenance</body></html>"))}, nil
	}
	defer func() { cloudOpen = open }()
	f.quota = false
	j = &PanJob{ID: 2, Key: "gd:1Folder000000000000000000000", Title: "Outfit Pack", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); !errors.Is(err, errCloudPage) {
		t.Errorf("a page: %v", err)
	}
	if entries, _ := os.ReadDir(j.Dir); len(entries) != 0 {
		t.Errorf("saved anyway: %v", entries)
	}
}

// Cancel while a connection is being retried: the job stops at once, the part file goes.
func TestCloudDownloadCancelled(t *testing.T) {
	_, st := newFakeCloud(t)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := dead.URL
	dead.Close()
	t.Setenv("VRCLIB_DROPBOX_BASE", addr)
	ctx, cancel := context.WithCancel(context.Background())
	j := &PanJob{ID: 1, Key: "db:dbox12345678901", Title: "Dress", Stage: "save"}
	done := make(chan error, 1)
	go func() { done <- runPanJob(ctx, st, j) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errPanCancelled) && !cloudshare.Temporary(err) {
			t.Errorf("cancelled: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("still trying after cancel")
	}
}

// Through the queue: the job counts as a download from being queued until it is over, exactly once.
func TestCloudDownloadQueued(t *testing.T) {
	_, st := newFakeCloud(t)
	st.Settings.NoExtract = true
	before := core.Downloading.Load()
	if err := QueuePanDownload(st, "db:dbox12345678901", nil, nil); err != nil {
		t.Fatal(err)
	}
	if core.Downloading.Load() != before+1 {
		t.Errorf("counted %d, want %d", core.Downloading.Load(), before+1)
	}
	var j *PanJob
	for i := 0; i < 400 && j == nil; i++ {
		time.Sleep(25 * time.Millisecond)
		for _, s := range PanJobsSnapshot() {
			if s.Key == "db:dbox12345678901" && panOver(s.Stage) {
				s := s
				j = &s
			}
		}
	}
	if j == nil || j.Stage != "done" {
		t.Fatalf("job: %+v", j)
	}
	if core.Downloading.Load() != before {
		t.Errorf("still counted: %d", core.Downloading.Load())
	}
	if err := QueuePanDownload(st, "gd:nolink", nil, nil); err == nil {
		t.Error("a card without a link was queued")
	}
	DismissPanJob("db:dbox12345678901")
}

// ---------- a Drive folder read from its share pages, entry by entry ----------

type driveEntry struct {
	id, name string
	data     []byte
	ctype    string
	status   int    // of a page answered instead of the file
	page     string // that page
	hold     chan struct{}
}

// pageDrive: drive.google.com with one shared folder (driveFolder): its entries in order, sub-folders by id.
type pageDrive struct {
	mu      sync.Mutex
	entries []driveEntry
	sub     map[string][]driveEntry
	subFail map[string]int // a sub-folder's listing answers with this status
	subHold chan struct{}  // a sub-folder's listing waits for this (or for the request to be given up)
	hits    map[string]int
}

func (d *pageDrive) find(id string) *driveEntry {
	for i := range d.entries {
		if d.entries[i].id == id {
			return &d.entries[i]
		}
	}
	for _, s := range d.sub {
		for i := range s {
			if s[i].id == id {
				return &s[i]
			}
		}
	}
	return nil
}

func (d *pageDrive) count(what string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hits[what]
}

func (d *pageDrive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	q := r.URL.Query()
	id := q.Get("id")
	d.hits[r.URL.Path+"?"+id]++
	switch r.URL.Path {
	case "/embeddedfolderview":
		list := d.entries
		if s, ok := d.sub[id]; ok {
			if st := d.subFail[id]; st != 0 {
				d.mu.Unlock()
				w.WriteHeader(st)
				return
			}
			if hold := d.subHold; hold != nil {
				d.mu.Unlock()
				select {
				case <-hold:
				case <-r.Context().Done():
				}
				d.mu.Lock()
			}
			list = s
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Pack - Google Drive</title></head><body>`)
		for _, e := range list {
			href := "https://drive.google.com/file/d/" + e.id + "/view?usp=drive_web"
			if _, ok := d.sub[e.id]; ok {
				href = "https://drive.google.com/drive/folders/" + e.id
			}
			fmt.Fprintf(w, `<div class="flip-entry"><a href="%s"><div class="flip-entry-title">%s</div></a></div>`, href, e.name)
		}
		fmt.Fprint(w, `</body></html>`)
		d.mu.Unlock()
	case "/uc":
		e := d.find(id)
		if e == nil {
			d.mu.Unlock()
			w.WriteHeader(404)
			return
		}
		f := *e
		d.mu.Unlock()
		if f.page != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(f.status)
			fmt.Fprint(w, f.page)
			return
		}
		ct := f.ctype
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", `attachment; filename="`+f.name+`"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(f.data)))
		if f.hold != nil { // half of it, then the rest when the test says so
			time.Sleep(600 * time.Millisecond)
			_, _ = w.Write(f.data[:len(f.data)/2])
			w.(http.Flusher).Flush()
			select {
			case <-f.hold:
			case <-r.Context().Done():
			}
			_, _ = w.Write(f.data[len(f.data)/2:])
			return
		}
		_, _ = w.Write(f.data)
	default:
		d.mu.Unlock()
		w.WriteHeader(404)
	}
}

const driveFolder = "gd:1PageFolder000000000000000000"

func newPageDrive(t *testing.T, entries ...driveEntry) (*pageDrive, *core.Store) {
	t.Helper()
	d := &pageDrive{entries: entries, sub: map[string][]driveEntry{}, subFail: map[string]int{}, hits: map[string]int{}}
	srv := httptest.NewServer(d)
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_GDRIVE_BASE", srv.URL)
	t.Setenv("VRCLIB_GDRIVE_API", srv.URL)
	t.Setenv("VRCLIB_GDRIVE_KEY", "")
	t.Setenv("VRCLIB_DROPBOX_BASE", srv.URL)
	t.Setenv("VRCLIB_BROWSER", "")
	st := testkit.NewStore(t)
	st.Settings.DownloadDir = t.TempDir()
	st.Settings.Roots = []string{st.Settings.DownloadDir}
	st.Settings.AutoBooth = false
	st.Settings.HideZh = true // (no translation of names behind the tests: it would go out to the net)
	st.Settings.NoExtract = true
	st.User[driveFolder] = &core.UserData{ShareURL: "https://drive.google.com/drive/folders/1PageFolder000000000000000000"}
	pause, stall := cloudPause, purchases.StallAfter
	cloudPause, purchases.StallAfter = time.Millisecond, 2*time.Second
	t.Cleanup(func() {
		cloudPause, purchases.StallAfter = pause, stall
		settleLibrary()
	})
	return d, st
}

func tree(dir string) string { return strings.Join(testkit.ListTree(dir), ",") }

func driveJob(id int64) *PanJob {
	return &PanJob{ID: id, Key: driveFolder, Title: "Pack", Stage: "save"}
}

// readShare reads the folder's listing as the daily round does, and waits for it.
func readShare(t *testing.T, st *core.Store) *core.PanListing {
	t.Helper()
	library.QueuePanFetch(st, driveFolder)
	for i := 0; ; i++ {
		time.Sleep(20 * time.Millisecond)
		netdisk.PanMu.Lock()
		idle := !netdisk.PanActive && len(netdisk.PanQueue) == 0
		netdisk.PanMu.Unlock()
		if idle {
			break
		}
		if i > 500 {
			t.Fatal("the share reader did not finish")
		}
	}
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	l := *st.Pan[driveFolder]
	return &l
}

// A file of the share that is a web page itself (README.html, sent as text/html like any page) is fetched as
// the file it is, and the files after it too.
func TestCloudHTMLFileInFolder(t *testing.T) {
	page := []byte("<!DOCTYPE html><html><body>terms of use</body></html>")
	_, st := newPageDrive(t,
		driveEntry{id: "1FileA0000000000000000000000", name: "A.unitypackage", data: bytes.Repeat([]byte("A"), 2000)},
		driveEntry{id: "1FileR0000000000000000000000", name: "README.html", data: page, ctype: "text/html; charset=utf-8"},
		driveEntry{id: "1FileZ0000000000000000000000", name: "Z.unitypackage", data: bytes.Repeat([]byte("Z"), 2000)})
	j := driveJob(1)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := tree(j.Dir); got != "A.unitypackage,README.html,Z.unitypackage" {
		t.Errorf("on disk: %s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(j.Dir, "README.html")); !bytes.Equal(b, page) {
		t.Errorf("README.html: %q", b)
	}
}

// Two entries of one name in a folder (Drive allows it), and one that differs only in case: each is kept,
// the later ones under a numbered name.
func TestCloudDuplicateNames(t *testing.T) {
	_, st := newPageDrive(t,
		driveEntry{id: "1FileA0000000000000000000000", name: "Texture.png", data: bytes.Repeat([]byte("1"), 1000)},
		driveEntry{id: "1FileB0000000000000000000000", name: "Texture.png", data: bytes.Repeat([]byte("2"), 3000)},
		driveEntry{id: "1FileC0000000000000000000000", name: "texture.PNG", data: bytes.Repeat([]byte("3"), 500)})
	j := driveJob(1)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := tree(j.Dir); got != "Texture (2).png,Texture.png,texture (3).PNG" {
		t.Fatalf("on disk: %s", got)
	}
	for name, want := range map[string]string{"Texture.png": strings.Repeat("1", 1000), "Texture (2).png": strings.Repeat("2", 3000), "texture (3).PNG": strings.Repeat("3", 500)} {
		if b, _ := os.ReadFile(filepath.Join(j.Dir, name)); string(b) != want {
			t.Errorf("%s: %d bytes of %q", name, len(b), string(b[:min(1, len(b))]))
		}
	}
	if j.Done != 4500 || j.Total != 4500 {
		t.Errorf("counted %d of %d", j.Done, j.Total)
	}
	// again into the same folder: the same names
	if err := runPanJob(context.Background(), st, driveJob(2)); err != nil {
		t.Fatal(err)
	}
	if got := tree(j.Dir); got != "Texture (2).png,Texture.png,texture (3).PNG" {
		t.Errorf("after a second download: %s", got)
	}
}

// Google's "too many users" page says the same whatever status it comes with: one request, and the player is
// told the file is throttled — not that the share is private, not "try later" after three tries.
func TestCloudQuotaPageStatus(t *testing.T) {
	quota := `<html><body><p class="uc-error-caption">Sorry, you can't view or download this file at this time.</p><p class="uc-error-subcaption">Too many users have viewed or downloaded this file recently. Please try accessing the file again later.</p></body></html>`
	for _, status := range []int{200, 403, 429} {
		d, st := newPageDrive(t, driveEntry{id: "1FileA0000000000000000000000", name: "A.zip", page: quota, status: status})
		err := runPanJob(context.Background(), st, driveJob(1))
		if n := d.count("/uc?1FileA0000000000000000000000"); !errors.Is(err, cloudshare.ErrGDQuota) || n != 1 {
			t.Errorf("the quota page with HTTP %d: %v after %d requests", status, err, n)
		}
	}
}

// A sub-folder that cannot be read for a moment: the listing read before stays the share's (no change
// notice, now or at the next read), and a download fetches all of it.
func TestCloudSubFolderUnreadable(t *testing.T) {
	const sub = "1SubFolder000000000000000000"
	d, st := newPageDrive(t,
		driveEntry{id: sub, name: "Textures"},
		driveEntry{id: "1FileA0000000000000000000000", name: "A.unitypackage", data: bytes.Repeat([]byte("A"), 2000)})
	d.sub[sub] = []driveEntry{{id: "1FileT0000000000000000000000", name: "T.png", data: bytes.Repeat([]byte("T"), 500)}}
	fail := func(status int) {
		d.mu.Lock()
		d.subFail[sub] = status
		d.mu.Unlock()
	}
	// a card that was never read: its download does not make do with half of the share
	fail(429)
	j := driveJob(1)
	err := runPanJob(context.Background(), st, j)
	st.Mu.RLock()
	l, u := st.Pan[driveFolder], st.User[driveFolder]
	st.Mu.RUnlock()
	if err == nil || !cloudshare.Temporary(err) || l != nil || len(u.PanGot) != 0 || j.Dir != "" {
		t.Fatalf("download of a share that cannot be read in full: err %v, listing %+v, got %v, folder %q", err, l, u.PanGot, j.Dir)
	}
	fail(0)
	if l := readShare(t, st); l.Count != 2 || l.Truncated || l.Err != "" {
		t.Fatalf("first read: %+v", l)
	}
	fail(429)
	if l := readShare(t, st); l.Count != 2 || l.Truncated || l.Err == "" || l.Changed != 0 || len(l.Removed) != 0 {
		t.Fatalf("read with the sub-folder answering 429: %+v", l)
	}
	j = driveJob(2)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	st.Mu.RLock()
	got := strings.Join(st.User[driveFolder].PanGot, ",")
	st.Mu.RUnlock()
	if tree(j.Dir) != "A.unitypackage,Textures/T.png" || got != "/" || strings.Contains(j.Msg, "未完全列出") {
		t.Errorf("download: %s on disk, got %q, %q", tree(j.Dir), got, j.Msg)
	}
	fail(0)
	if l := readShare(t, st); l.Count != 2 || l.Changed != 0 || len(l.Added) != 0 || l.Err != "" {
		t.Errorf("next read: %+v", l)
	}
}

// A listing that stops short of the share (too deep, too many entries): the download says so, and records as
// downloaded what was listed in full — not all of the share.
func TestCloudDownloadOfCutListing(t *testing.T) {
	_, st := newPageDrive(t,
		driveEntry{id: "1FileA0000000000000000000000", name: "A.unitypackage", data: bytes.Repeat([]byte("A"), 2000)},
		driveEntry{id: "1FileD0000000000000000000000", name: "d.png", data: bytes.Repeat([]byte("D"), 300)},
		driveEntry{id: "1FileT0000000000000000000000", name: "T.png", data: bytes.Repeat([]byte("T"), 500)})
	cut := func() *core.PanListing {
		return &core.PanListing{Surl: driveFolder, Title: "Pack", Fetched: time.Now().Unix(), Count: 3, Truncated: true, Files: []*core.PanFile{
			{Name: "Done", Dir: true, Children: []*core.PanFile{{Name: "d.png", Ref: "1FileD0000000000000000000000"}}},
			{Name: "Textures", Dir: true, Children: []*core.PanFile{{Name: "T.png", Ref: "1FileT0000000000000000000000"}, {Name: "More", Dir: true, Partial: true}}},
			{Name: "A.unitypackage", Ref: "1FileA0000000000000000000000"}}}
	}
	st.Mu.Lock()
	st.Pan[driveFolder] = cut()
	st.Mu.Unlock()
	j := driveJob(1)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	st.Mu.RLock()
	got := strings.Join(st.User[driveFolder].PanGot, ",")
	st.Mu.RUnlock()
	if j.Stage != "done" || !strings.Contains(j.Msg, "分享未完全列出，部分文件未下载") || got != "/A.unitypackage,/Done,/Textures/T.png" || tree(j.Dir) != "A.unitypackage,Done/d.png,Textures/T.png" {
		t.Errorf("all of a cut listing: %s %q, got %q, on disk %s", j.Stage, j.Msg, got, tree(j.Dir))
	}
	// picked parts: a folder listed in full is all there, a cut one is not
	panGot := func() string {
		st.Mu.RLock()
		defer st.Mu.RUnlock()
		return strings.Join(st.User[driveFolder].PanGot, ",")
	}
	st.Mu.Lock()
	st.User[driveFolder].PanGot, st.User[driveFolder].Downloaded = nil, ""
	st.Mu.Unlock()
	j = &PanJob{ID: 2, Key: driveFolder, Title: "Pack", Stage: "save", Paths: []string{"/Done"}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := panGot(); got != "/Done" || strings.Contains(j.Msg, "未完全列出") {
		t.Errorf("a whole folder picked: got %q, %q", got, j.Msg)
	}
	j = &PanJob{ID: 3, Key: driveFolder, Title: "Pack", Stage: "save", Paths: []string{"/Textures"}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := panGot(); got != "/Done,/Textures/T.png" || !strings.Contains(j.Msg, "分享未完全列出，部分文件未下载") {
		t.Errorf("a cut folder picked: got %q, %q", got, j.Msg)
	}
	// which parts count
	for _, c := range []struct {
		l    *core.PanListing
		want string
		all  bool
	}{
		{&core.PanListing{Files: []*core.PanFile{{Name: "a.zip"}, {Name: "D", Dir: true, Children: []*core.PanFile{{Name: "b.zip"}}}}}, "/", true},
		{&core.PanListing{Truncated: true, Files: []*core.PanFile{{Name: "a.zip"}, {Name: "D", Dir: true, Children: []*core.PanFile{{Name: "b.zip"}}}}}, "/a.zip,/D", false}, // cut at the top
		{&core.PanListing{Files: []*core.PanFile{{Name: "D", Dir: true, Partial: true}}}, "", false},
	} {
		parts, all := cloudListed("", c.l.Files, c.l.Truncated)
		if strings.Join(parts, ",") != c.want || all != c.all {
			t.Errorf("listed parts %v (all %v), want %s", parts, all, c.want)
		}
	}
}

// The seller took a file away and put it there again (another id, the same place) after the listing was read:
// the share is read once more and the file fetched by its place. A file that is really gone ends the job.
func TestCloudFileReplacedSinceListing(t *testing.T) {
	d, st := newPageDrive(t,
		driveEntry{id: "1FileA0000000000000000000000", name: "A.unitypackage", data: bytes.Repeat([]byte("A"), 2000)},
		driveEntry{id: "1FileC0000000000000000000000", name: "C.unitypackage", data: bytes.Repeat([]byte("C"), 100)})
	j := driveJob(1)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(j.Dir)
	d.mu.Lock()
	d.entries = []driveEntry{{id: "1FileNEW00000000000000000000", name: "A.unitypackage", data: bytes.Repeat([]byte("B"), 2500)},
		{id: "1FileNEWC0000000000000000000", name: "C.unitypackage", data: bytes.Repeat([]byte("c"), 150)}}
	d.mu.Unlock()
	lists := d.count("/embeddedfolderview?1PageFolder000000000000000000")
	j = driveJob(2)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatalf("a file replaced since the listing was read: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(j.Dir, "A.unitypackage"))
	c, _ := os.ReadFile(filepath.Join(j.Dir, "C.unitypackage"))
	if string(a) != strings.Repeat("B", 2500) || string(c) != strings.Repeat("c", 150) {
		t.Errorf("fetched %d and %d bytes", len(a), len(c))
	}
	if n := d.count("/embeddedfolderview?1PageFolder000000000000000000") - lists; n != 1 || d.count("/uc?1FileC0000000000000000000000") != 1 {
		t.Errorf("the share was read %d times; the old id of the second file was asked %d times after the first download", n, d.count("/uc?1FileC0000000000000000000000")-1)
	}
	st.Mu.RLock()
	ref := st.Pan[driveFolder].Files[0].Ref
	st.Mu.RUnlock()
	if ref != "1FileNEW00000000000000000000" || j.Done != 2650 {
		t.Errorf("listing kept with %s; counted %d", ref, j.Done)
	}
	// gone from the share altogether
	_ = os.RemoveAll(j.Dir)
	d.mu.Lock()
	d.entries = d.entries[1:]
	d.mu.Unlock()
	if err := runPanJob(context.Background(), st, driveJob(3)); !cloudshare.Gone(err) {
		t.Errorf("a file that is gone: %v", err)
	}
}

// A listing without sizes cannot say whether the file in the folder is the one the share holds now: the
// server's answer does. Replaced in place, it is fetched again; unchanged, it is left alone.
func TestCloudChangedFileFetchedAgain(t *testing.T) {
	const id = "1FileA0000000000000000000000"
	d, st := newPageDrive(t, driveEntry{id: id, name: "A.unitypackage", data: bytes.Repeat([]byte("A"), 2000)})
	j := driveJob(1)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(j.Dir, "A.unitypackage")
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	_ = os.Chtimes(file, old, old)
	before := d.count("/uc?" + id)
	j = driveJob(2)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(file); fi == nil || !fi.ModTime().Equal(old) || d.count("/uc?"+id)-before != 1 || j.Done != 2000 || j.Total != 2000 {
		t.Errorf("an unchanged file: written again, or %d requests; counted %d of %d", d.count("/uc?"+id)-before, j.Done, j.Total)
	}
	d.mu.Lock()
	d.entries[0].data = bytes.Repeat([]byte("B"), 2500)
	d.mu.Unlock()
	j = driveJob(3)
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != strings.Repeat("B", 2500) || j.Stage != "done" {
		t.Errorf("a file replaced in place: %d bytes of %q on disk", len(b), string(b[:1]))
	}
	// a server that does not say the length: fetched again
	open := cloudOpen
	var opened atomic.Int32
	cloudOpen = func(ctx context.Context, c *http.Client, src cloudshare.Source, from int64, ifRange string) (*http.Response, error) {
		opened.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/octet-stream"}}, ContentLength: -1,
			Body: io.NopCloser(strings.NewReader(strings.Repeat("C", 2500)))}, nil
	}
	defer func() { cloudOpen = open }()
	if err := runPanJob(context.Background(), st, driveJob(4)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != strings.Repeat("C", 2500) || opened.Load() != 1 {
		t.Errorf("no length said: %q… on disk after %d requests", string(b[:1]), opened.Load())
	}
}

// A Baidu job that needs a login waits for it; the Drive job behind it does not — it runs, and is not
// "already in the queue" afterwards.
func TestBaiduLoginDoesNotParkCloudJobs(t *testing.T) {
	_, st := newPageDrive(t, driveEntry{id: "1FileA0000000000000000000000", name: "A.unitypackage", data: bytes.Repeat([]byte("A"), 2000)})
	st.User["pan:1abcdefg"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1abcdefg", SharePwd: "abcd"}
	st.User["pan:1hijklmn"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1hijklmn"}
	run := runPan
	gate := make(chan struct{})
	runPan = func(ctx context.Context, st *core.Store, j *PanJob) error {
		if strings.HasPrefix(j.Key, "pan:") {
			<-gate
			return ErrBaiduLogin
		}
		return run(ctx, st, j)
	}
	defer func() { runPan = run }()
	for _, k := range []string{"pan:1abcdefg", driveFolder, "pan:1hijklmn"} {
		if err := QueuePanDownload(st, k, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	close(gate)
	stage := map[string]string{}
	for i := 0; i < 400 && !panOver(stage[driveFolder]); i++ {
		time.Sleep(25 * time.Millisecond)
		for _, s := range PanJobsSnapshot() {
			stage[s.Key] = s.Stage
		}
	}
	if stage["pan:1abcdefg"] != "login" || stage["pan:1hijklmn"] != "login" || stage[driveFolder] != "done" {
		t.Errorf("stages: %v", stage)
	}
	if err := QueuePanDownload(st, driveFolder, nil, nil); err != nil {
		t.Errorf("the Drive card queued again: %v", err)
	}
	for i := 0; i < 400; i++ {
		panDLMu.Lock()
		running := panDLRunning
		panDLMu.Unlock()
		if !running {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	CancelPanDownloads()
	for _, k := range []string{"pan:1abcdefg", "pan:1hijklmn", driveFolder} {
		DismissPanJob(k)
	}
}

// A download while the share is read again and the window asks for the cards (for the race detector).
func TestCloudDownloadWhileShareIsRead(t *testing.T) {
	d, st := newPageDrive(t,
		driveEntry{id: "1SubFolder000000000000000000", name: "Textures"},
		driveEntry{id: "1FileA0000000000000000000000", name: "A.unitypackage", data: bytes.Repeat([]byte("A"), 200000)},
		driveEntry{id: "1FileB0000000000000000000000", name: "B.unitypackage", data: bytes.Repeat([]byte("B"), 200000)})
	d.sub["1SubFolder000000000000000000"] = []driveEntry{{id: "1FileT0000000000000000000000", name: "T.png", data: bytes.Repeat([]byte("T"), 50000)}}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			library.QueuePanFetch(st, driveFolder)
			st.Mu.RLock()
			_ = library.AllViews(st)
			st.Mu.RUnlock()
			_ = PanJobsSnapshot()
			time.Sleep(2 * time.Millisecond)
		}
	}()
	for i := 0; i < 3; i++ {
		j := driveJob(int64(i))
		panDLMu.Lock()
		panJobs = append(panJobs, j)
		panDLMu.Unlock()
		if err := runPanJob(context.Background(), st, j); err != nil {
			t.Error(err)
		}
		if got := tree(j.Dir); got != "A.unitypackage,B.unitypackage,Textures/T.png" {
			t.Errorf("on disk: %s", got)
		}
		_ = os.RemoveAll(j.Dir)
	}
	close(stop)
	wg.Wait()
	readShare(t, st)
	panDLMu.Lock()
	keepPanLocked(func(*PanJob) bool { return true })
	panDLMu.Unlock()
}

// The finished part file cannot take the file's name (a folder is in the way here; on Windows also a file that
// is open, or being scanned): what was downloaded stays, and the next try gives it its name without asking
// the server again.
func TestCloudRenameFailureKeepsPart(t *testing.T) {
	data := bytes.Repeat([]byte("Z"), 300000)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_DROPBOX_BASE", srv.URL)
	st := testkit.NewStore(t)
	dst := filepath.Join(t.TempDir(), "Pack.zip")
	_ = os.MkdirAll(filepath.Join(dst, "x"), 0755)
	c := cloudshare.Client(st)
	src := cloudshare.Source{Service: cloudshare.Dropbox, Link: "https://www.dropbox.com/s/abcdefgh12345/Pack.zip"}
	err := fetchCloudFile(context.Background(), c, src, int64(len(data)), dst, func(int64, int64) {})
	if !purchases.IsLocalErr(err) || !strings.Contains(err.Error(), "无法写入「") || partSize(dst+".part") != int64(len(data)) || hits.Load() != 1 {
		t.Fatalf("a name that is taken: %v; part file of %d bytes after %d requests", err, partSize(dst+".part"), hits.Load())
	}
	_ = os.RemoveAll(dst)
	var at int64
	if err := fetchCloudFile(context.Background(), c, src, int64(len(data)), dst, func(pos, size int64) { at = pos }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); !bytes.Equal(b, data) || hits.Load() != 1 || partSize(dst+".part") != 0 || at != int64(len(data)) {
		t.Errorf("the next try: %d bytes after %d requests, part file of %d bytes, counted %d", len(b), hits.Load(), partSize(dst+".part"), at)
	}
	// what the disk cannot hold still goes: it only takes up the room that is missing
	free := purchases.DiskFree
	purchases.DiskFree = func(string) uint64 { return 10 }
	defer func() { purchases.DiskFree = free }()
	dst2 := filepath.Join(t.TempDir(), "Big.zip")
	if err := fetchCloudFile(context.Background(), c, src, int64(len(data)), dst2, func(int64, int64) {}); !purchases.IsLocalErr(err) || core.StatOK(dst2+".part") {
		t.Errorf("a full disk: %v, part file kept: %v", err, core.StatOK(dst2+".part"))
	}
}

// A source that cannot be continued (a Dropbox folder's zip: no length, no ranges) and breaks once: the bytes
// of the try that was thrown away are not counted with the file's.
func TestCloudRestartCountedOnce(t *testing.T) {
	data := testkit.ZipBytes(t, map[string][]byte{"Hair/hair.unitypackage": bytes.Repeat([]byte("H"), 400000)})
	var cut atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="Hair.zip"`)
		if r.Header.Get("Range") == "bytes=0-0" { // the probe
			w.(http.Flusher).Flush()
			_, _ = w.Write(data[:1])
			return
		}
		w.(http.Flusher).Flush() // chunked, no Content-Length
		if cut.CompareAndSwap(false, true) {
			_, _ = w.Write(data[:len(data)*3/4])
			w.(http.Flusher).Flush()
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			c.Close()
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_DROPBOX_BASE", srv.URL)
	t.Setenv("VRCLIB_BROWSER", "")
	st := testkit.NewStore(t)
	st.Settings.DownloadDir = t.TempDir()
	st.Settings.Roots = []string{st.Settings.DownloadDir}
	st.Settings.AutoBooth, st.Settings.NoExtract = false, true
	st.User["db:abcdefgh12345"] = &core.UserData{ShareURL: "https://www.dropbox.com/scl/fo/abcdefgh12345/h?rlkey=k1"}
	pause := cloudPause
	cloudPause = time.Millisecond
	defer func() {
		cloudPause = pause
		settleLibrary()
	}()
	j := &PanJob{ID: 1, Key: "db:abcdefgh12345", Title: "Hair", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	if b, _ := os.ReadFile(filepath.Join(j.Dir, "Hair.zip")); !bytes.Equal(b, data) || j.Done != size || j.Total != size || !strings.Contains(j.Msg, "（"+fmtBytes(size)+"）") || !cut.Load() {
		t.Errorf("%d bytes on disk of %d; counted %d of %d: %q", len(b), size, j.Done, j.Total, j.Msg)
	}
}

// A listing without sizes: the bar goes by the number of files, not by the bytes of the files started so far
// (which made it run to the end once for every file).
func TestCloudProgressByFiles(t *testing.T) {
	hold := make(chan struct{})
	_, st := newPageDrive(t,
		driveEntry{id: "1FileA0000000000000000000000", name: "A.unitypackage", data: bytes.Repeat([]byte("A"), 5000)},
		driveEntry{id: "1FileB0000000000000000000000", name: "B.unitypackage", data: bytes.Repeat([]byte("B"), 2000), hold: hold})
	j := driveJob(1)
	done := make(chan error, 1)
	go func() { done <- runPanJob(context.Background(), st, j) }()
	var pct, files int
	var byCount bool
	for i := 0; i < 400; i++ {
		time.Sleep(10 * time.Millisecond)
		panDLMu.Lock()
		pct, files, byCount = j.Pct, j.Files, j.ByCount
		panDLMu.Unlock()
		if files == 1 && pct >= 50 {
			break
		}
	}
	close(hold)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// the first of two files is there, the second at its beginning: past the half, well before the end
	if !byCount || files != 1 || pct < 50 || pct > 80 {
		t.Errorf("in the second of two files: %d%% (by files: %v, %d done)", pct, byCount, files)
	}
	if j.Done != 7000 || j.Total != 7000 {
		t.Errorf("counted %d of %d", j.Done, j.Total)
	}
}

// Cancel while the share's folders are still being read on the way to a download: the read ends with it.
func TestCloudCancelWhileListing(t *testing.T) {
	d, st := newPageDrive(t, driveEntry{id: "1SubFolder000000000000000000", name: "Textures"})
	d.sub["1SubFolder000000000000000000"] = []driveEntry{{id: "1FileT0000000000000000000000", name: "T.png", data: []byte("T")}}
	d.subHold = make(chan struct{})
	defer close(d.subHold)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runPanJob(ctx, st, driveJob(1)) }()
	for i := 0; i < 400 && d.count("/embeddedfolderview?1SubFolder000000000000000000") == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the job went on after cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("still reading the share after cancel")
	}
}

// A Drive name may hold a slash: a picked entry is found by its path as the file list has it.
func TestCloudNodeByPath(t *testing.T) {
	fs := []*core.PanFile{
		{Name: "v1/v2 textures.zip", Ref: "a"},
		{Name: "v1", Dir: true, Children: []*core.PanFile{{Name: "body.png", Ref: "b"}, {Name: "A/B", Dir: true, Children: []*core.PanFile{{Name: "c/d.png", Ref: "c"}}}}},
		{Name: "top.zip", Ref: "d"}}
	for p, want := range map[string]string{"/v1/v2 textures.zip": "a|", "/v1/body.png": "b|/v1", "/v1/A/B/c/d.png": "c|/v1/A/B", "/top.zip": "d|", "/v1/A/B": "|/v1", "/v1/nope.png": "nil", "/nope": "nil"} {
		n, parent := cloudNode(fs, p)
		got := "nil"
		if n != nil {
			got = n.Ref + "|" + parent
		}
		if got != want {
			t.Errorf("%s: %s, want %s", p, got, want)
		}
	}
}
