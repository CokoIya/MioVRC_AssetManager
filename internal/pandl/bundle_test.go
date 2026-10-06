package pandl

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
)

// collectionZip: what a seller packs — one archive with an archive for each product in it.
func collectionZip(t *testing.T, products ...string) []byte {
	inner := map[string][]byte{"说明.txt": []byte("readme")}
	for _, p := range products {
		inner[p+".zip"] = testkit.ZipBytes(t, map[string][]byte{p + ".unitypackage": []byte("package of " + p), "readme.txt": []byte("x")})
	}
	return testkit.ZipBytes(t, inner)
}

func unityProject(t *testing.T) string {
	p := t.TempDir()
	for _, d := range []string{"Assets", "ProjectSettings", "Packages"} {
		_ = os.MkdirAll(filepath.Join(p, d), 0755)
	}
	return p
}

// importStarted: did an import for this card begin within a moment? It is waited for and put away, so the
// next test starts without one.
func importStarted(t *testing.T, key string) bool {
	t.Helper()
	var j *unity.ImportJob
	for i := 0; i < 60 && j == nil; i++ {
		time.Sleep(10 * time.Millisecond)
		j = unity.ImportSnapshot()
	}
	if j == nil {
		return false
	}
	waitUntil(t, "the import to end", func() bool {
		s := unity.ImportSnapshot()
		if s != nil && s.Stage == "choose" { // packages for several base bodies: nobody is there to pick
			unity.DismissImport()
		}
		return s == nil || s.Stage == "done" || s.Stage == "failed"
	})
	unity.DismissImport()
	return j.Key == key
}

func marksUnder(st *core.Store, dir string) string {
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var out []string
	for k, mode := range st.Overrides {
		rel, _ := filepath.Rel(core.PathKey(dir), k)
		out = append(out, filepath.ToSlash(rel)+"="+mode)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func cardNames(st *core.Store) string {
	settleLibrary()
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var out []string
	for _, a := range st.Assets {
		out = append(out, a.Name)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// A Baidu share that is one archive of several products' archives: unpacked, its folder levels are marked, the
// note says so, and the import that was asked for with the download does not happen.
func TestPanDownloadOfCollection(t *testing.T) {
	f, st := newFakeBaidu(t)
	st.Settings.NoExtract = false
	f.share["/sh/2.zip"] = &bdNode{fsid: 2, data: collectionZip(t, "Sailor Dress", "Twintail", "Cat Ears"), mtime: 10}
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save", imp: &unity.ImportReq{Project: unityProject(t), Recycle: true}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "2/Cat Ears/Cat Ears.unitypackage,2/Cat Ears/readme.txt,2/Sailor Dress/Sailor Dress.unitypackage,2/Sailor Dress/readme.txt,2/Twintail/Twintail.unitypackage,2/Twintail/readme.txt,2/说明.txt" {
		t.Fatalf("on disk: %s", got)
	}
	if got := marksUnder(st, j.Dir); got != ".=bundle 2=bundle" {
		t.Errorf("marks: %q", got)
	}
	if j.Stage != "done" || !strings.HasSuffix(j.Msg, "；合集包，已拆分为 3 个素材；合集包不会整包导入，请在各素材卡片上分别导入") {
		t.Errorf("job: %s %q", j.Stage, j.Msg)
	}
	if importStarted(t, j.Key) {
		t.Error("the collection was imported whole")
	}
	if got := cardNames(st); got != "Cat Ears,Sailor Dress,Twintail" {
		t.Errorf("cards: %s", got)
	}
	st.Mu.RLock()
	saved := core.LoadStore(st.Path).Overrides
	st.Mu.RUnlock()
	if len(saved) != 2 {
		t.Errorf("marks on disk: %v", saved)
	}

	// without an import asked for, the note says only that it was split
	_ = os.RemoveAll(j.Dir)
	st.Mu.Lock()
	st.Overrides = map[string]string{}
	st.Mu.Unlock()
	j = &PanJob{ID: 2, Key: "pan:1Share", Title: "2", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(j.Msg, "）；合集包，已拆分为 3 个素材") || marksUnder(st, j.Dir) != ".=bundle 2=bundle" {
		t.Errorf("job: %q, marks %q", j.Msg, marksUnder(st, j.Dir))
	}
	settleLibrary()
}

// The same share with automatic unpacking off: nothing can be marked inside an archive — the note says that
// it is a collection, and it is not imported whole either. The card of its folder says it too.
func TestPanDownloadOfPackedCollection(t *testing.T) {
	f, st := newFakeBaidu(t) // (NoExtract is on there)
	f.share["/sh/2.zip"] = &bdNode{fsid: 2, data: collectionZip(t, "Sailor Dress", "Twintail"), mtime: 10}
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save", imp: &unity.ImportReq{Project: unityProject(t), Recycle: true}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "2.zip" {
		t.Fatalf("on disk: %s", got)
	}
	if !strings.HasSuffix(j.Msg, "）；合集包，拆分后可分别导入") || marksUnder(st, j.Dir) != "" {
		t.Errorf("job: %q, marks %q", j.Msg, marksUnder(st, j.Dir))
	}
	if importStarted(t, j.Key) {
		t.Error("the packed collection was imported whole")
	}
	if got := cardNames(st); got != "2" {
		t.Fatalf("cards: %s", got)
	}
	st.Mu.RLock()
	a := st.Assets[0]
	st.Mu.RUnlock()
	if a.Bundle != 2 || !a.BundlePacked {
		t.Errorf("the folder's card: a collection of %d, packed %v", a.Bundle, a.BundlePacked)
	}
}

// One product, as before: no marks, nothing about a collection in the note, and the import begins.
func TestPanDownloadOfOneProduct(t *testing.T) {
	f, st := newFakeBaidu(t)
	st.Settings.NoExtract = false
	dress := testkit.ZipBytes(t, map[string][]byte{"Dress_Kaguya.zip": testkit.ZipBytes(t, map[string][]byte{"Dress_Kaguya.unitypackage": []byte("k")}),
		"Dress_Plum.zip": testkit.ZipBytes(t, map[string][]byte{"Dress_Plum.unitypackage": []byte("p")}), "PSD.zip": testkit.ZipBytes(t, map[string][]byte{"dress.psd": []byte("psd")})})
	f.share["/sh/Moon Dress.zip"] = &bdNode{fsid: 2, data: dress, mtime: 10}
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save", imp: &unity.ImportReq{Project: unityProject(t), Recycle: true}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(j.Msg, "合集") || !strings.HasPrefix(j.Msg, "已下载 1 个文件（") || strings.Contains(j.Msg, "；") || marksUnder(st, j.Dir) != "" {
		t.Errorf("job: %q, marks %q", j.Msg, marksUnder(st, j.Dir))
	}
	if !importStarted(t, j.Key) {
		t.Error("the import that was asked for did not begin")
	}
	if got := cardNames(st); got != "Moon Dress" {
		t.Errorf("cards: %s", got)
	}
}

// A share of two archives side by side — a tool and the shader it needs — is no archive of archives: what
// its listing says of it stands, and it ends as it always did.
func TestPanDownloadOfArchivesSideBySide(t *testing.T) {
	f, st := newFakeBaidu(t)
	st.Settings.NoExtract = false
	product := func(n string) []byte {
		return testkit.ZipBytes(t, map[string][]byte{n + ".unitypackage": []byte("package of " + n)})
	}
	f.share["/sh/Setup Tool.zip"] = &bdNode{fsid: 2, data: product("Setup Tool"), mtime: 10}
	f.share["/sh/Shader It Needs.zip"] = &bdNode{fsid: 3, data: product("Shader It Needs"), mtime: 10}
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "Setup Tool", Stage: "save", imp: &unity.ImportReq{Project: unityProject(t), Recycle: true}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "Setup Tool/Setup Tool.unitypackage,Shader It Needs/Shader It Needs.unitypackage" {
		t.Fatalf("on disk: %s", got)
	}
	if strings.Contains(j.Msg, "合集") || marksUnder(st, j.Dir) != "" {
		t.Errorf("job: %q, marks %q", j.Msg, marksUnder(st, j.Dir))
	}
	if !importStarted(t, j.Key) {
		t.Error("the import that was asked for did not begin")
	}
}

// A Dropbox link to such an archive ends the same way; a Drive folder of one product as it always did.
func TestCloudDownloadOfCollection(t *testing.T) {
	f, st := newFakeCloud(t)
	f.files["db"] = collectionZip(t, "Sailor Suit", "Twintail") // (it comes as "Dress.zip")
	const key = "db:dbox12345678901"
	j := &PanJob{ID: 1, Key: key, Title: "Pack", Stage: "save", imp: &unity.ImportReq{Project: unityProject(t), Recycle: true}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := marksUnder(st, j.Dir); got != ".=bundle dress=bundle" {
		t.Errorf("marks: %q (on disk: %s)", got, testkit.ListTree(j.Dir))
	}
	if !strings.HasSuffix(j.Msg, "；合集包，已拆分为 2 个素材；合集包不会整包导入，请在各素材卡片上分别导入") {
		t.Errorf("job: %q", j.Msg)
	}
	if importStarted(t, key) {
		t.Error("the collection was imported whole")
	}
	if got := cardNames(st); got != "Sailor Suit,Twintail" {
		t.Errorf("cards: %s", got)
	}
	st.Mu.RLock()
	views := 0
	for _, a := range st.Assets {
		if core.UnderDir(a.Locations[0].Path, j.Dir) {
			views++
		}
	}
	st.Mu.RUnlock()
	if views != 2 {
		t.Errorf("%d of the cards are in the download's folder", views)
	}

	// one product from a Drive folder: no marks, and its import begins
	const drive = "gd:1Folder000000000000000000000"
	st.Mu.Lock()
	st.Overrides = map[string]string{}
	st.Mu.Unlock()
	j = &PanJob{ID: 2, Key: drive, Title: "Outfit Pack", Stage: "save", imp: &unity.ImportReq{Project: unityProject(t), Recycle: true}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(j.Msg, "合集") || marksUnder(st, j.Dir) != "" {
		t.Errorf("one product: %q, marks %q", j.Msg, marksUnder(st, j.Dir))
	}
	if !importStarted(t, drive) {
		t.Error("the import that was asked for did not begin")
	}
	// with unpacking off: told, not imported, nothing marked
	st.Mu.Lock()
	st.Settings.NoExtract = true
	delete(st.User, key)
	st.User[key] = &core.UserData{ShareURL: "https://www.dropbox.com/s/dbox12345678901/Dress.zip"}
	st.Mu.Unlock()
	_ = os.RemoveAll(filepath.Join(st.Settings.DownloadDir, "Pack"))
	j = &PanJob{ID: 3, Key: key, Title: "Pack", Stage: "save", imp: &unity.ImportReq{Project: unityProject(t), Recycle: true}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(j.Msg, "）；合集包，拆分后可分别导入") || marksUnder(st, j.Dir) != "" {
		t.Errorf("packed: %q, marks %q", j.Msg, marksUnder(st, j.Dir))
	}
	if importStarted(t, key) {
		t.Error("the packed collection was imported whole")
	}
}

// The share the player reported: one archive ("2"), in it an archive for each category with the products'
// archives inside, a product's archive beside them, a readme and a picture; a product brings its PSD pack as
// an archive of its own. Every level is unpacked and marked, each product is one card in its category, with the
// share it came from — and the PSD pack stays with its product.
func TestPanDownloadOfNestedCollection(t *testing.T) {
	f, st := newFakeBaidu(t)
	st.Settings.NoExtract = false
	product := func(name string, psd bool) []byte {
		files := map[string][]byte{name + ".unitypackage": []byte("package of " + name), "readme.txt": []byte("x")}
		if psd {
			files[name+"_PSD.zip"] = testkit.ZipBytes(t, map[string][]byte{"body.psd": []byte("8BPS")})
		}
		return testkit.ZipBytes(t, files)
	}
	category := func(products ...string) []byte {
		inner := map[string][]byte{}
		for i, p := range products {
			inner[p+".zip"] = product(p, i == 0)
		}
		return testkit.ZipBytes(t, inner)
	}
	f.share["/sh/2.zip"] = &bdNode{fsid: 2, mtime: 10, data: testkit.ZipBytes(t, map[string][]byte{
		"衣服合集.zip":         category("Sailor Dress", "Maid Outfit", "Night Gown"),
		"头发.zip":           category("Twintail Hair", "Bob Hair"),
		"Gothic Dress.zip": product("Gothic Dress", false),
		"说明.txt":           []byte("readme"),
		"预览.jpg":           make([]byte, 9000),
	})}
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save", imp: &unity.ImportReq{Project: unityProject(t), Recycle: true}}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if got := marksUnder(st, j.Dir); got != ".=bundle 2/头发=bundle 2/衣服合集=bundle 2=bundle" {
		t.Errorf("marks: %q", got)
	}
	if j.Stage != "done" || !strings.Contains(j.Msg, "；合集包，已拆分为 6 个素材；合集包不会整包导入") {
		t.Errorf("job: %s %q", j.Stage, j.Msg)
	}
	if importStarted(t, j.Key) {
		t.Error("the collection was imported whole")
	}
	if got := cardNames(st); got != "Bob Hair,Gothic Dress,Maid Outfit,Night Gown,Sailor Dress,Twintail Hair" {
		t.Errorf("cards: %s", got)
	}
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	for _, v := range library.AllViews(st) {
		if v.PanOnly {
			t.Errorf("the share's own card is still there: %s", v.Name)
			continue
		}
		want := map[string]string{"Sailor Dress": "衣服", "Maid Outfit": "衣服", "Night Gown": "衣服", "Twintail Hair": "头发", "Bob Hair": "头发", "Gothic Dress": "衣服"}[v.Name]
		if v.Category != want || v.FromPan != "pan:1Share" || v.Bundle != 0 {
			t.Errorf("%s: category %s (want %s), from %q, bundle %d", v.Name, v.Category, want, v.FromPan, v.Bundle)
		}
		if v.Name == "Sailor Dress" && v.Archives != 1 { // its PSD pack came along, still packed (the fourth level)
			t.Errorf("Sailor Dress: %d archives with it", v.Archives)
		}
	}
}
