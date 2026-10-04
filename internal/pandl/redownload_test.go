package pandl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/netdisk"
	"vrclib/internal/testkit"
)

type bdNode struct {
	fsid  int
	dir   bool
	mtime int64
	data  []byte
}

// fakeBaidu: a share and the player's netdisk, answering like pan.baidu.com does to a logged-in player. Saving
// next to a file of the same name gives "name(1)", as Baidu's does.
type fakeBaidu struct {
	mu        sync.Mutex
	share     map[string]*bdNode // "/sh/Root/a.zip"
	disk      map[string]*bdNode // "/MioVRCA/…"
	clock     int64
	transfers []string
	removed   []string
	lists     int
}

func (f *fakeBaidu) kids(m map[string]*bdNode, dir string) []map[string]any {
	var names []string
	for p := range m {
		if path.Dir(p) == dir {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	out := []map[string]any{}
	for _, p := range names {
		n := m[p]
		d := 0
		if n.dir {
			d = 1
		}
		out = append(out, map[string]any{"fs_id": n.fsid, "server_filename": path.Base(p), "path": p, "isdir": d, "size": len(n.data), "server_mtime": n.mtime})
	}
	return out
}

func (f *fakeBaidu) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = r.ParseForm()
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/api/gettemplatevariable":
		reply(map[string]any{"errno": 0, "result": map[string]any{"bdstoken": "tok", "username": "mio", "loginstate": 1, "is_vip": 0, "is_svip": 1}})
	case "/s/1Share":
		list, _ := json.Marshal(f.kids(f.share, "/sh"))
		fmt.Fprintf(w, `<script id="locals-data" type="application/json">{"share_uk":"3","shareid":4,"title":"","file_list":%s}</script>`, list)
	case "/share/list":
		f.lists++
		reply(map[string]any{"errno": 0, "list": f.kids(f.share, r.Form.Get("dir"))})
	case "/api/list":
		dir := r.Form.Get("dir")
		if n := f.disk[dir]; n == nil || !n.dir {
			reply(map[string]any{"errno": -9})
			return
		}
		reply(map[string]any{"errno": 0, "list": f.kids(f.disk, dir)})
	case "/api/create":
		if f.disk[r.Form.Get("path")] == nil {
			f.disk[r.Form.Get("path")] = &bdNode{dir: true}
		}
		reply(map[string]any{"errno": 0})
	case "/share/transfer":
		var ids []int
		_ = json.Unmarshal([]byte(r.Form.Get("fsidlist")), &ids)
		f.transfers = append(f.transfers, r.Form.Get("fsidlist")+" -> "+r.Form.Get("path"))
		f.clock += 10
		for _, id := range ids {
			for p, n := range f.share {
				if n.fsid != id {
					continue
				}
				dst := r.Form.Get("path") + "/" + path.Base(p)
				if f.disk[dst] != nil {
					dst = strings.TrimSuffix(dst, path.Ext(dst)) + "(1)" + path.Ext(dst)
				}
				for q, m := range f.share { // with what is inside a folder
					if q == p || strings.HasPrefix(q, p+"/") {
						f.disk[dst+strings.TrimPrefix(q, p)] = &bdNode{dir: m.dir, data: m.data, mtime: f.clock}
					}
				}
			}
		}
		reply(map[string]any{"errno": 0})
	case "/api/filemanager":
		var paths []string
		_ = json.Unmarshal([]byte(r.Form.Get("filelist")), &paths)
		if r.Form.Get("opera") != "delete" {
			reply(map[string]any{"errno": 2})
			return
		}
		for _, p := range paths {
			f.removed = append(f.removed, p)
			for q := range f.disk {
				if q == p || strings.HasPrefix(q, p+"/") {
					delete(f.disk, q)
				}
			}
		}
		reply(map[string]any{"errno": 0, "taskid": 77})
	case "/share/taskquery":
		reply(map[string]any{"errno": 0, "status": "success"})
	case "/rest/2.0/pcs/file":
		n := f.disk[r.Form.Get("path")]
		if n == nil {
			w.WriteHeader(404)
			return
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(n.data))
	default:
		reply(map[string]any{"errno": 0})
	}
}

func newFakeBaidu(t *testing.T) (*fakeBaidu, *core.Store) {
	t.Helper()
	f := &fakeBaidu{share: map[string]*bdNode{}, disk: map[string]*bdNode{"/": {dir: true}}, clock: 100}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_PAN_BASE", srv.URL)
	t.Setenv("VRCLIB_PCS_BASE", srv.URL)
	t.Setenv("VRCLIB_DEFAULT_BROWSER", "")
	t.Setenv("VRCLIB_BROWSER", "")
	st := testkit.NewStore(t)
	st.Settings.DownloadDir = t.TempDir()
	st.Settings.Roots = []string{st.Settings.DownloadDir}
	st.Settings.NoExtract, st.Settings.AutoBooth = true, false
	st.User["pan:1Share"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1Share"}
	bdLoaded, bdCache = false, nil
	if err := SaveBaiduSession(&baiduSession{Cookies: []core.SavedCookie{{Name: "BDUSS", Value: "ok"}, {Name: "STOKEN", Value: "ok"}}}); err != nil {
		t.Fatal(err)
	}
	poll := panPoll
	panPoll = 5 * time.Millisecond
	t.Cleanup(func() {
		panPoll = poll
		settleLibrary() // the scan that follows a download
		bdLoaded, bdCache = false, nil
	})
	return f, st
}

func (f *fakeBaidu) take() (transfers, removed string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	transfers, removed = strings.Join(f.transfers, "; "), strings.Join(f.removed, "; ")
	f.transfers, f.removed = nil, nil
	return
}

// A card downloaded whole; then the seller replaces a file under the same name (same size, even) and adds one
// inside the folder. Downloading again brings both: the netdisk copy is brought up to the share, and the file
// on this disk is the new one.
func TestRedownloadAfterShareUpdate(t *testing.T) {
	f, st := newFakeBaidu(t)
	old, changed, added := bytes.Repeat([]byte("A"), 100), bytes.Repeat([]byte("B"), 100), bytes.Repeat([]byte("C"), 50)
	f.share["/sh/Root"] = &bdNode{fsid: 1, dir: true, mtime: 10}
	f.share["/sh/Root/a.zip"] = &bdNode{fsid: 2, data: old, mtime: 10}
	f.share["/sh/Root/Tex"] = &bdNode{fsid: 5, dir: true, mtime: 10}
	f.share["/sh/Root/Tex/t.png"] = &bdNode{fsid: 6, data: []byte("png"), mtime: 10}
	run := func() *PanJob {
		t.Helper()
		j := &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save"}
		if err := runPanJob(context.Background(), st, j); err != nil {
			t.Fatal(err)
		}
		return j
	}
	j := run()
	local := j.Dir
	if tr, rm := f.take(); tr != "[1] -> /MioVRCA/Root" || rm != "" {
		t.Fatalf("first download: transfers %q, removed %q", tr, rm)
	}
	if got := strings.Join(testkit.ListTree(local), ","); got != "Tex/t.png,a.zip" {
		t.Fatalf("first download: %s", got)
	}
	st.Mu.RLock()
	saved := st.User["pan:1Share"].PanSaved
	st.Mu.RUnlock()
	if len(saved) != 1 || saved[0] != "/" {
		t.Fatalf("recorded as saved: %v", saved)
	}

	// nothing changed: nothing is saved again, nothing removed
	run()
	if tr, rm := f.take(); tr != "" || rm != "" {
		t.Errorf("an unchanged share: transfers %q, removed %q", tr, rm)
	}

	f.mu.Lock()
	f.clock += 100
	f.share["/sh/Root/a.zip"] = &bdNode{fsid: 7, data: changed, mtime: f.clock}
	f.share["/sh/Root/b.zip"] = &bdNode{fsid: 3, data: added, mtime: f.clock}
	f.share["/sh/Root/Tex/new.png"] = &bdNode{fsid: 8, data: []byte("new"), mtime: f.clock}
	f.mu.Unlock()
	j = run()
	tr, rm := f.take()
	if rm != "/MioVRCA/Root/Root/a.zip" || !strings.Contains(tr, "[8] -> /MioVRCA/Root/Root/Tex") || !strings.Contains(tr, "[7,3] -> /MioVRCA/Root/Root") {
		t.Errorf("after the update: transfers %q, removed %q", tr, rm)
	}
	f.mu.Lock()
	var disk []string
	for p, n := range f.disk {
		if !n.dir {
			disk = append(disk, p)
		}
	}
	sort.Strings(disk)
	copyOK := bytes.Equal(f.disk["/MioVRCA/Root/Root/a.zip"].data, changed)
	f.mu.Unlock()
	if strings.Join(disk, ",") != "/MioVRCA/Root/Root/Tex/new.png,/MioVRCA/Root/Root/Tex/t.png,/MioVRCA/Root/Root/a.zip,/MioVRCA/Root/Root/b.zip" || !copyOK {
		t.Errorf("the netdisk copy: %v (a.zip is the new file: %v)", disk, copyOK)
	}
	if j.Dir != local || strings.Join(testkit.ListTree(local), ",") != "Tex/new.png,Tex/t.png,a.zip,b.zip" {
		t.Errorf("on this disk: %s in %s", testkit.ListTree(j.Dir), j.Dir)
	}
	if a, _ := os.ReadFile(filepath.Join(local, "a.zip")); !bytes.Equal(a, changed) {
		t.Errorf("a.zip on this disk is still the old file (%.8s…)", a)
	}
	if b, _ := os.ReadFile(filepath.Join(local, "b.zip")); !bytes.Equal(b, added) {
		t.Errorf("b.zip: %d bytes", len(b))
	}
}

// Only the replaced file ticked in the card's file list: it is saved and downloaded again too.
func TestRedownloadPickedFile(t *testing.T) {
	f, st := newFakeBaidu(t)
	f.share["/sh/Root"] = &bdNode{fsid: 1, dir: true, mtime: 10}
	f.share["/sh/Root/a.zip"] = &bdNode{fsid: 2, data: bytes.Repeat([]byte("A"), 100), mtime: 10}
	if err := runPanJob(context.Background(), st, &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save"}); err != nil {
		t.Fatal(err)
	}
	f.take()
	bigger := bytes.Repeat([]byte("B"), 999)
	f.mu.Lock()
	f.share["/sh/Root/a.zip"] = &bdNode{fsid: 7, data: bigger, mtime: 10} // no date to go by: the size tells
	f.mu.Unlock()
	j := &PanJob{ID: 2, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save", Paths: []string{"/Root/a.zip"}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if tr, rm := f.take(); tr != "[7] -> /MioVRCA/Root/Root" || rm != "/MioVRCA/Root/Root/a.zip" {
		t.Errorf("transfers %q, removed %q", tr, rm)
	}
	if a, _ := os.ReadFile(filepath.Join(j.Dir, "a.zip")); !bytes.Equal(a, bigger) {
		t.Errorf("a.zip on this disk: %d bytes", len(a))
	}
}

func TestPanStale(t *testing.T) {
	raw := func(dir, size, mtime string) netdisk.PanRaw {
		return netdisk.PanRaw{IsDir: json.Number(dir), Size: json.Number(size), Mtime: json.Number(mtime)}
	}
	ent := func(dir, size, mtime string) bdEntry {
		return bdEntry{IsDir: json.Number(dir), Size: json.Number(size), Mtime: json.Number(mtime)}
	}
	for _, c := range []struct {
		it   netdisk.PanRaw
		e    bdEntry
		want bool
	}{
		{raw("0", "100", "10"), ent("0", "100", "20"), false}, // the copy was made after the share's file
		{raw("0", "100", "10"), ent("0", "100", "10"), false},
		{raw("0", "100", "30"), ent("0", "100", "20"), true}, // changed in the share since
		{raw("0", "101", "10"), ent("0", "100", "20"), true},
		{raw("0", "100", ""), ent("0", "100", ""), false}, // no dates: the size is all there is
		{raw("1", "0", "30"), ent("1", "0", "20"), false}, // folders are looked into instead
		{raw("1", "0", "10"), ent("0", "5", "20"), true},
	} {
		if got := panStale(c.it, c.e); got != c.want {
			t.Errorf("share %+v, copy %+v: stale = %v", c.it, c.e, got)
		}
	}
	b := &bdClient{ctx: context.Background()}
	for _, p := range []string{"/MioVRCA", "/MioVRCA/Dress", "/other/Dress/a.zip", "/MioVRCA/Dress/../../x", "/"} {
		if err := b.remove([]string{p}); err == nil || !strings.Contains(err.Error(), "MioVRCA") {
			t.Errorf("%s would have been removed: %v", p, err)
		}
	}
}
