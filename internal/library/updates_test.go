package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// updReload: as after a restart — what is known comes from updates.json.
func updReload() {
	upd.mu.Lock()
	upd.s = nil
	upd.mu.Unlock()
}

func kaguyaStore(t *testing.T) (*core.Store, *core.Asset) {
	t.Helper()
	st := testkit.NewStore(t)
	root := t.TempDir()
	dir := filepath.Join(root, "9000001 Kaguya", "Kaguya_v1.06")
	_ = os.MkdirAll(dir, 0755)
	a := &core.Asset{Key: "booth:9000001", Name: "Kaguya", RawName: "9000001 Kaguya", BoothID: "9000001", HasDir: true,
		Locations: []core.Location{{Path: dir, Kind: "dir", Size: 100, Root: root}}}
	st.Settings.Roots = []string{root}
	st.Assets = []*core.Asset{a}
	st.Purchases["9000001"] = &core.Purchase{ID: "9000001", Name: "Kaguya", Files: []string{"Kaguya_v1.06.zip", "Kaguya_PSD.zip"}, Downloads: []string{"10", "11"}}
	return st, a
}

func TestBoothUpdateNotes(t *testing.T) {
	st, a := kaguyaStore(t)
	if n := UpdateNotes(st); len(n) != 0 {
		t.Fatalf("a purchase seen for the first time is where it starts: %+v", n[a.Key])
	}
	// the shop puts a later version up: a new download under a new name
	st.Purchases["9000001"] = &core.Purchase{ID: "9000001", Name: "Kaguya", Files: []string{"Kaguya_v1.07.zip", "Kaguya_PSD.zip"}, Downloads: []string{"12", "11"}}
	n := UpdateNotes(st)[a.Key]
	if n == nil || n.Source != "booth" || n.Version != "1.07" || n.Have != "1.06" || strings.Join(n.DLs, ",") != "12" || !n.CanDL || n.Item != "9000001" {
		t.Fatalf("later version: %+v", n)
	}
	if strings.Join(n.Added, ",") != "Kaguya_v1.07.zip" || len(n.Replaced) != 0 {
		t.Errorf("new file: added %v, replaced %v", n.Added, n.Replaced)
	}
	if len(n.Old) != 1 || n.Old[0].Path != a.Locations[0].Path || !n.Old[0].Dir || n.Old[0].Size != 100 {
		t.Errorf("the earlier version's folder: %+v", n.Old)
	}
	if n.At == 0 {
		t.Error("no date")
	}
	at := n.At
	updReload()
	if n = UpdateNotes(st)[a.Key]; n == nil || n.At != at {
		t.Fatalf("after a restart: %+v", n)
	}
	// dealt with: stays so, also after a restart, until something newer still turns up
	AckUpdate(st, a.Key, nil)
	updReload()
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("still flagged after it was dealt with: %+v", n)
	}
	st.Purchases["9000001"] = &core.Purchase{ID: "9000001", Name: "Kaguya", Files: []string{"Kaguya_v1.08.zip", "Kaguya_PSD.zip"}, Downloads: []string{"13", "11"}}
	if n := UpdateNotes(st)[a.Key]; n == nil || n.Version != "1.08" || strings.Join(n.DLs, ",") != "13" {
		t.Fatalf("a newer one after that: %+v", n)
	}
	// the program fetched it (from the purchase's file list, say): nothing left to fetch
	st.Downloaded = map[string]*core.DLRecord{"13": {Item: "9000001", Path: "x"}}
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("flagged though the file was downloaded: %+v", n)
	}
}

func TestBoothReplacedFile(t *testing.T) {
	st := testkit.NewStore(t)
	root := t.TempDir()
	a := &core.Asset{Key: "booth:77", Name: "Dress", RawName: "77 Dress", BoothID: "77", Locations: []core.Location{{Path: filepath.Join(root, "77 Dress"), Kind: "dir", Root: root}}}
	st.Assets = []*core.Asset{a}
	st.Purchases["77"] = &core.Purchase{ID: "77", Files: []string{"Dress.zip", "Readme.txt"}, Downloads: []string{"1", "2"}}
	st.Purchases["88"] = &core.Purchase{ID: "88", Files: []string{"Other.zip"}, Downloads: []string{"5"}} // bought, not on disk
	UpdateNotes(st)
	// the same name under a new id: the shop replaced the file; and one file more
	st.Purchases["77"] = &core.Purchase{ID: "77", Files: []string{"Dress.zip", "Readme.txt", "Dress_Tex.zip"}, Downloads: []string{"9", "2", "10"}}
	st.Purchases["88"] = &core.Purchase{ID: "88", Files: []string{"Other.zip"}, Downloads: []string{"6"}}
	notes := UpdateNotes(st)
	n := notes[a.Key]
	if n == nil || strings.Join(n.Replaced, ",") != "Dress.zip" || strings.Join(n.Added, ",") != "Dress_Tex.zip" || strings.Join(n.DLs, ",") != "9,10" || n.Version != "" || len(n.Old) != 0 {
		t.Fatalf("got %+v", n)
	}
	if len(notes) != 1 {
		t.Errorf("a purchase that is not on disk has nothing to update: %v", notes)
	}
	// that purchase is downloaded later: what it offered at that time is what the player has
	st.Assets = append(st.Assets, &core.Asset{Key: "booth:88", Name: "Other", BoothID: "88", Locations: []core.Location{{Path: filepath.Join(root, "88 Other"), Kind: "dir", Root: root}}})
	if n := UpdateNotes(st)["booth:88"]; n != nil {
		t.Errorf("flagged right after its first download: %+v", n)
	}
	// only a file taken away: nothing newer
	AckUpdate(st, a.Key, nil)
	st.Purchases["77"] = &core.Purchase{ID: "77", Files: []string{"Dress.zip"}, Downloads: []string{"9"}}
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Errorf("a removed download flagged: %+v", n)
	}
}

func moonStore(t *testing.T) (*core.Store, *core.Asset, string) {
	t.Helper()
	st := testkit.NewStore(t)
	root := t.TempDir()
	dir := filepath.Join(root, "Moon Dress")
	_ = os.MkdirAll(dir, 0755)
	a := &core.Asset{Key: "name:moondress", Name: "Moon Dress", RawName: "Moon Dress", HasDir: true, FirstSeen: 5000,
		Locations: []core.Location{{Path: dir, Kind: "dir", Root: root}}}
	st.Assets = []*core.Asset{a}
	st.User["pan:1Share"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1Share", Downloaded: dir, PanGot: []string{"/"}}
	st.Pan = map[string]*core.PanListing{"1Share": {Surl: "1Share", Count: 3, Fetched: 1, Files: []*core.PanFile{
		testkit.PanDir("Root", &core.PanFile{Name: "a.zip", Size: 100}, &core.PanFile{Name: "c.zip", Size: 30}, testkit.PanDir("Tex", &core.PanFile{Name: "t.png", Size: 5}))}}}
	forgetDirs()
	return st, a, dir
}

func TestPanUpdateNotes(t *testing.T) {
	st, a, _ := moonStore(t)
	if n := UpdateNotes(st); len(n) != 0 {
		t.Fatalf("first look: %+v", n[a.Key])
	}
	// the seller replaces a.zip (another size), adds b.zip and a texture, takes c.zip away
	st.Pan["1Share"] = &core.PanListing{Surl: "1Share", Count: 4, Fetched: 2, Changed: 9000, Files: []*core.PanFile{
		testkit.PanDir("Root", &core.PanFile{Name: "a.zip", Size: 120}, &core.PanFile{Name: "b.zip", Size: 50},
			testkit.PanDir("Tex", &core.PanFile{Name: "t.png", Size: 5}, &core.PanFile{Name: "new.png", Size: 7}))}}
	n := UpdateNotes(st)[a.Key]
	if n == nil || n.Source != "pan" || !n.CanDL || n.PanKey != "pan:1Share" {
		t.Fatalf("got %+v", n)
	}
	if strings.Join(n.Added, ",") != "/Root/Tex/new.png,/Root/b.zip" || strings.Join(n.Replaced, ",") != "/Root/a.zip" || strings.Join(n.Removed, ",") != "/Root/c.zip" {
		t.Errorf("added %v, replaced %v, removed %v", n.Added, n.Replaced, n.Removed)
	}
	if strings.Join(n.Paths, ",") != "/Root/Tex/new.png,/Root/b.zip,/Root/a.zip" || n.At != 9000 {
		t.Errorf("what to fetch: %v at %d", n.Paths, n.At)
	}
	// only the replaced file fetched (picked in the card's file list): the rest is still to come
	PanFetched(st, "pan:1Share", []string{"/Root/a.zip"})
	if n = UpdateNotes(st)[a.Key]; n == nil || len(n.Replaced) != 0 || len(n.Added) != 2 {
		t.Fatalf("after a part was fetched: %+v", n)
	}
	PanFetched(st, "pan:1Share", nil)
	updReload()
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("after all of it was fetched: %+v", n)
	}
	// a share that could not be read again says nothing
	st.Pan["1Share"] = &core.PanListing{Surl: "1Share", Err: "分享已失效", Files: st.Pan["1Share"].Files[:0]}
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("a dead share flagged: %+v", n)
	}
}

func TestPanUpdateFirstLookAndParts(t *testing.T) {
	// a change noticed before this version kept lists: the share's own notice, which does not tell new from replaced
	st, a, _ := moonStore(t)
	st.Pan["1Share"].Changed, st.Pan["1Share"].Added, st.Pan["1Share"].Removed = 9000, []string{"/Root/a.zip"}, []string{"/Root/old.zip"}
	n := UpdateNotes(st)[a.Key]
	if n == nil || strings.Join(n.Changed, ",") != "/Root/a.zip" || len(n.Added)+len(n.Replaced) != 0 || strings.Join(n.Removed, ",") != "/Root/old.zip" {
		t.Fatalf("got %+v", n)
	}
	AckUpdate(st, a.Key, nil)
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("after 已是最新: %+v", n)
	}
	st.Mu.RLock()
	seen := st.User[a.Key] != nil && st.User[a.Key].PanSeen > 0
	st.Mu.RUnlock()
	if !seen {
		t.Error("the asset's own mark was not set")
	}

	// downloaded after the notice: nothing to say
	st, a, _ = moonStore(t)
	st.Pan["1Share"].Changed, st.Pan["1Share"].Added = 4000, []string{"/Root/a.zip"}
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("a change from before the download: %+v", n)
	}

	// only /Root/Tex was downloaded: a change elsewhere in the share is not this asset's
	st, a, _ = moonStore(t)
	st.User["pan:1Share"].PanGot = []string{"/Root/Tex"}
	UpdateNotes(st)
	st.Pan["1Share"] = &core.PanListing{Surl: "1Share", Fetched: 2, Changed: 9000, Files: []*core.PanFile{
		testkit.PanDir("Root", &core.PanFile{Name: "a.zip", Size: 999}, &core.PanFile{Name: "c.zip", Size: 30}, testkit.PanDir("Tex", &core.PanFile{Name: "t.png", Size: 5}))}}
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("a part that was never downloaded: %+v", n)
	}
	st.Pan["1Share"] = &core.PanListing{Surl: "1Share", Fetched: 3, Changed: 9500, Files: []*core.PanFile{
		testkit.PanDir("Root", &core.PanFile{Name: "a.zip", Size: 999}, &core.PanFile{Name: "c.zip", Size: 30}, testkit.PanDir("Tex", &core.PanFile{Name: "t.png", Size: 6}))}}
	if n := UpdateNotes(st)[a.Key]; n == nil || strings.Join(n.Replaced, ",") != "/Root/Tex/t.png" {
		t.Fatalf("the downloaded part changed: %+v", n)
	}
}

func TestPanUpdateLinkedByHand(t *testing.T) {
	st := testkit.NewStore(t)
	root := t.TempDir()
	a := &core.Asset{Key: "name:handdress", Name: "Hand Dress", FirstSeen: 100, Locations: []core.Location{{Path: filepath.Join(root, "Hand Dress"), Kind: "dir", Root: root}}}
	st.Assets = []*core.Asset{a}
	st.User[a.Key] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1Hand"}
	st.Pan = map[string]*core.PanListing{"1Hand": {Surl: "1Hand", Fetched: 1, Files: []*core.PanFile{{Name: "x.zip", Size: 10}}}}
	UpdateNotes(st)
	st.Pan["1Hand"] = &core.PanListing{Surl: "1Hand", Fetched: 2, Changed: 500, Files: []*core.PanFile{{Name: "x.zip", Size: 10}, {Name: "y.zip", Size: 20}}}
	n := UpdateNotes(st)[a.Key]
	if n == nil || n.CanDL || strings.Join(n.Added, ",") != "/y.zip" {
		t.Fatalf("got %+v", n)
	}
	AckUpdate(st, a.Key, nil)
	if n := UpdateNotes(st)[a.Key]; n != nil {
		t.Fatalf("after 已是最新: %+v", n)
	}
}

// A large library: the notes are worked out anew at every reload of the window.
func BenchmarkUpdateNotes(b *testing.B) {
	st := bigStore(b, 6000)
	st.Mu.Lock()
	for id, p := range st.Purchases { // every other purchase has a later file by now
		if len(id)%2 == 0 || id[len(id)-2] == '1' {
			p.Files, p.Downloads = append(p.Files, p.Name+" v9.9.zip"), append(p.Downloads, id+"9")
		}
	}
	st.Mu.Unlock()
	UpdateNotes(st)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		UpdateNotes(st)
	}
}

// writeFileAtomic: the file is whole or as it was; a swap that keeps being refused gives up and leaves no
// temporary file behind.
func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.json")
	for _, c := range []string{"one", "two"} {
		if err := writeFileAtomic(file, []byte(c)); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(file); string(b) != c {
			t.Fatalf("read back %q", b)
		}
	}
	held := filepath.Join(dir, "held.json") // something the swap cannot replace: a folder with a file in it
	if err := os.MkdirAll(filepath.Join(held, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := writeFileAtomic(held, []byte("x")); err == nil {
		t.Error("a swap that cannot be done was reported as done")
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Error("the swap was not tried again")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}
