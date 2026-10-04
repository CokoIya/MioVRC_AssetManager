package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/cloudshare"
	"vrclib/internal/core"
	"vrclib/internal/netdisk"
	"vrclib/internal/testkit"
)

// A Google Drive and a Dropbox card are read with the Baidu shares, on the same daily round, by the reader
// of their own kind; their listings land under their card keys, and what changed is marked the same way.
func TestCloudSharesOnTheDailyRound(t *testing.T) {
	st := panStore(t, "1aaa")
	st.User["gd:1Folder000000000000000000000"] = &core.UserData{ShareURL: "https://drive.google.com/drive/folders/1Folder000000000000000000000"}
	st.User["db:dbox12345678901"] = &core.UserData{ShareURL: "https://www.dropbox.com/s/dbox12345678901/Dress.zip"}
	st.Pan["gd:1Folder000000000000000000000"] = &core.PanListing{Surl: "gd:1Folder000000000000000000000", Fetched: 100, Count: 1, Files: []*core.PanFile{{Name: "old.zip", Size: 1, Ref: "1Old"}}}
	var mu sync.Mutex
	var asked []string
	fetchPanListing = func(st *core.Store, link, pwd string) (*core.PanListing, error) {
		mu.Lock()
		asked = append(asked, "baidu:"+netdisk.ShareSurl(link))
		mu.Unlock()
		return &core.PanListing{Surl: "1aaa", Fetched: time.Now().Unix(), Files: []*core.PanFile{{Name: "a.zip", Size: 5}}, Count: 1}, nil
	}
	cloud := fetchCloudListing
	fetchCloudListing = func(st *core.Store, link string) (*core.PanListing, error) {
		l := cloudshare.Parse(link)
		mu.Lock()
		asked = append(asked, l.Key())
		mu.Unlock()
		if l.Service == cloudshare.Dropbox {
			return nil, errors.New("Dropbox 链接已失效或文件已被删除")
		}
		return &core.PanListing{Surl: l.Key(), Title: "Outfit Pack", Fetched: time.Now().Unix(), Count: 2,
			Files: []*core.PanFile{{Name: "new.zip", Size: 7, Ref: "1New"}, {Name: "old.zip", Size: 1, Ref: "1Old"}}}, nil
	}
	t.Cleanup(func() { fetchCloudListing = cloud })
	st.Mu.RLock()
	due := dueShares(st, time.Now())
	st.Mu.RUnlock()
	if strings.Join(due, ",") != "db:dbox12345678901,gd:1Folder000000000000000000000,pan:1aaa" {
		t.Fatalf("due: %v", due)
	}
	QueuePanFetch(st, due...)
	waitPanIdle(t)
	mu.Lock()
	got := strings.Join(asked, ",")
	mu.Unlock()
	if got != "db:dbox12345678901,gd:1Folder000000000000000000000,baidu:1aaa" {
		t.Errorf("asked: %s", got)
	}
	st.Mu.RLock()
	gd, db, bd := st.Pan["gd:1Folder000000000000000000000"], st.Pan["db:dbox12345678901"], st.Pan["1aaa"]
	st.Mu.RUnlock()
	if gd == nil || gd.Title != "Outfit Pack" || gd.Changed == 0 || strings.Join(gd.Added, ",") != "/new.zip" || len(gd.Removed) != 0 {
		t.Errorf("Drive listing: %+v", gd)
	}
	if db == nil || db.Err == "" || !netdisk.PanErrSettled(db.Err) {
		t.Errorf("Dropbox listing: %+v", db)
	}
	if bd == nil || bd.Count != 1 {
		t.Errorf("Baidu listing: %+v", bd)
	}
	// a share that is gone is not asked for every day
	st.Mu.RLock()
	due = dueShares(st, time.Now().Add(2*24*time.Hour))
	st.Mu.RUnlock()
	if strings.Join(due, ",") != "gd:1Folder000000000000000000000,pan:1aaa" {
		t.Errorf("due after two days: %v", due)
	}
	// the cards: both show as netdisk-only assets, the Drive one named after its folder
	st.Mu.RLock()
	views := AllViews(st)
	st.Mu.RUnlock()
	names := map[string]string{}
	for _, v := range views {
		if v.PanOnly {
			names[v.Key] = v.Name
		}
	}
	if names["gd:1Folder000000000000000000000"] != "Outfit Pack" || names["db:dbox12345678901"] == "" || names["pan:1aaa"] == "" {
		t.Errorf("cards: %v", names)
	}
}

// The folder a Drive card was downloaded into: its card carries the share, and the change notice of the
// next read says what is new, like a Baidu share's.
func TestCloudUpdateNotes(t *testing.T) {
	st := testkit.NewStore(t)
	root := t.TempDir()
	dir := filepath.Join(root, "Outfit Pack")
	_ = os.MkdirAll(dir, 0755)
	a := &core.Asset{Key: "dir:" + dir, Name: "Outfit Pack", RawName: "Outfit Pack", HasDir: true, Locations: []core.Location{{Path: dir, Kind: "dir", Size: 100, Root: root}}}
	st.Settings.Roots = []string{root}
	st.Assets = []*core.Asset{a}
	key := "gd:1Folder000000000000000000000"
	st.User[key] = &core.UserData{ShareURL: "https://drive.google.com/drive/folders/1Folder000000000000000000000", Downloaded: dir, PanGot: []string{"/"}}
	st.Pan[key] = &core.PanListing{Surl: key, Count: 1, Fetched: 1, Files: []*core.PanFile{{Name: "a.zip", Size: 100, Ref: "1A"}}}
	st.Mu.RLock()
	views := AllViews(st)
	st.Mu.RUnlock()
	var v *AssetView
	for i := range views {
		if views[i].Key == a.Key {
			v = &views[i]
		}
	}
	if v == nil || v.FromPan != key || v.User.ShareURL == "" || v.Pan == nil {
		t.Fatalf("the folder's card: %+v", v)
	}
	if n := UpdateNotes(st); len(n) != 0 {
		t.Fatalf("first look: %+v", n[a.Key])
	}
	st.Pan[key] = &core.PanListing{Surl: key, Count: 2, Fetched: 2, Changed: 9000, Files: []*core.PanFile{{Name: "a.zip", Size: 100, Ref: "1A"}, {Name: "b.zip", Size: 50, Ref: "1B"}}}
	n := UpdateNotes(st)[a.Key]
	if n == nil || n.Source != "pan" || !n.CanDL || n.PanKey != key || strings.Join(n.Added, ",") != "/b.zip" {
		t.Fatalf("got %+v", n)
	}
	PanFetched(st, key, nil)
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("after it was fetched: %+v", n)
	}
}

// A link field that holds a Baidu link and a Drive link (a seller's "百度 … / 海外 …" line) is one share: the
// Baidu one, read by the Baidu reader and kept under its id — not a Drive listing under the Baidu key.
func TestFetchListingOfBothLinks(t *testing.T) {
	st := panStore(t)
	link := "https://pan.baidu.com/s/1bbbCCC 提取码 ab12 海外 https://drive.google.com/drive/folders/1Folder000000000000000000000"
	st.User["d:/lib/dress"] = &core.UserData{ShareURL: link}
	st.User["d:/lib/hair"] = &core.UserData{ShareURL: "海外 https://drive.google.com/drive/folders/1Folder000000000000000000000 国内 https://pan.baidu.com/s/1dddEEE"}
	var mu sync.Mutex
	var asked []string
	fetchPanListing = func(st *core.Store, link, pwd string) (*core.PanListing, error) {
		mu.Lock()
		asked = append(asked, "baidu:"+netdisk.ShareSurl(link))
		mu.Unlock()
		return &core.PanListing{Surl: netdisk.ShareSurl(link), Fetched: time.Now().Unix(), Files: []*core.PanFile{{Name: "a.zip", Size: 5}}, Count: 1}, nil
	}
	cloud := fetchCloudListing
	fetchCloudListing = func(st *core.Store, link string) (*core.PanListing, error) {
		l := cloudshare.Parse(link)
		mu.Lock()
		asked = append(asked, l.Key())
		mu.Unlock()
		return &core.PanListing{Surl: l.Key(), Title: "Drive folder", Fetched: time.Now().Unix(), Count: 1, Files: []*core.PanFile{{Name: "g.zip", Size: 7, Ref: "1New"}}}, nil
	}
	t.Cleanup(func() { fetchCloudListing = cloud })
	QueuePanFetch(st, "d:/lib/dress", "d:/lib/hair")
	waitPanIdle(t)
	mu.Lock()
	got := strings.Join(asked, ",")
	mu.Unlock()
	if got != "baidu:1bbbCCC,baidu:1dddEEE" || netdisk.ShareID(link) != "1bbbCCC" {
		t.Errorf("asked %s for the share %s", got, netdisk.ShareID(link))
	}
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	for k, l := range st.Pan {
		if l.Surl != k || cloudshare.IsCloudKey(l.Surl) {
			t.Errorf("st.Pan[%q] holds the listing of %q (%s)", k, l.Surl, l.Title)
		}
	}
	if len(st.Pan) != 2 {
		t.Errorf("%d listings kept", len(st.Pan))
	}
}
