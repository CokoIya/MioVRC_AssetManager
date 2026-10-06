package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
)

// bundleCards: the cards as the window gets them — "name" for a product, "name=N" for a collection of N
// products, "name=N packed" for one that has to be unpacked to get at them.
func bundleCards(st *core.Store) string {
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && library.PipelineBusy(); { // the scan a request began
		time.Sleep(5 * time.Millisecond)
	}
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var out []string
	for _, v := range library.AllViews(st) {
		s := v.Name
		if v.Bundle > 0 {
			s += "=" + strconv.Itoa(v.Bundle)
			if v.BundlePacked {
				s += " packed"
			}
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func bundleMarks(st *core.Store, root string) string {
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var out []string
	for k, mode := range st.Overrides {
		rel, _ := filepath.Rel(core.PathKey(root), k)
		out = append(out, filepath.ToSlash(rel)+"="+mode)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func cardKey(t *testing.T, st *core.Store, name string) string {
	t.Helper()
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	for _, v := range library.AllViews(st) {
		if v.Name == name {
			return v.Key
		}
	}
	t.Fatalf("no card named %s", name)
	return ""
}

// A download unpacked long ago that holds a collection, two levels deep, in a folder of its own.
func collectionOnDisk(t *testing.T, root string) {
	t.Helper()
	for _, n := range []string{"衣服/Sailor Dress", "衣服/Maid Outfit", "衣服/Night Gown", "衣服/Summer Bikini", "头发/Twintail_hair", "头发/Bob_hair_v2", "Gothic Coat"} {
		testkit.WriteFile(t, filepath.Join(root, "Share", "2", filepath.FromSlash(n), filepath.Base(n)+".unitypackage"), 100)
	}
	testkit.WriteFile(t, filepath.Join(root, "Wolf Tail", "Wolf Tail.unitypackage"), 100)
}

const splitCards = "Bob_hair_v2, Gothic Coat, Maid Outfit, Night Gown, Sailor Dress, Summer Bikini, Twintail_hair, Wolf Tail"

// 「拆分为 N 个素材」 on a card whose products lie in folders: its levels are marked and the library is scanned,
// nothing is unpacked. One 恢复 on the outermost mark makes it one card again; 「作为单个素材」 ends the matter.
func TestBundleSplit(t *testing.T) {
	root := t.TempDir()
	collectionOnDisk(t, root)
	st, _ := scannedLibrary(t, root)
	st.Settings.HideZh = true // (no names are sent to the translator: there is no network here)
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	if got := bundleCards(st); got != "Share=7, Wolf Tail" {
		t.Fatalf("before: %s", got)
	}
	share, product := cardKey(t, st, "Share"), cardKey(t, st, "Wolf Tail")
	// a collection is not imported, whoever asks
	proj := t.TempDir()
	for _, d := range []string{"Assets", "ProjectSettings"} {
		_ = os.MkdirAll(filepath.Join(proj, d), 0755)
	}
	if r := postJSON(t, srv, "/api/import/start", map[string]any{"key": share, "project": proj}); r["ok"] != false || r["err"] != "合集包包含 7 个素材，请先拆分，再分别导入" {
		t.Fatalf("importing the collection: %v", r)
	}
	if r := postJSON(t, srv, "/api/bundle/split", map[string]any{"key": product}); r["ok"] != false || r["err"] != "该素材不是合集包，无需拆分" {
		t.Errorf("splitting a product: %v", r)
	}
	if r := postJSON(t, srv, "/api/bundle/split", map[string]any{"key": "name:nosuchthing"}); r["ok"] != false || r["err"] != "未找到该素材" {
		t.Errorf("splitting what is not there: %v", r)
	}
	if got := bundleMarks(st, root); got != "" {
		t.Fatalf("marks before the split: %s", got)
	}
	if r := postJSON(t, srv, "/api/bundle/split", map[string]any{"key": share}); r["ok"] != true || r["n"] != float64(7) || r["job"] != nil {
		t.Fatalf("split: %v", r)
	}
	if got := bundleCards(st); got != splitCards {
		t.Fatalf("after the split: %s", got)
	}
	if got := bundleMarks(st, root); got != "share/2/衣服=bundle share/2=bundle share=bundle" {
		t.Errorf("marks: %s", got)
	}
	if unity.ImportSnapshot() != nil {
		t.Error("a job was started for folders that needed no unpacking")
	}
	if saved := core.LoadStore(st.Path).Overrides; len(saved) != 3 {
		t.Errorf("marks on disk: %v", saved)
	}
	// its card is gone: asked again, there is nothing to split
	if r := postJSON(t, srv, "/api/bundle/split", map[string]any{"key": share}); r["ok"] != false {
		t.Errorf("split again: %v", r)
	}
	// 恢复 on a level inside takes that level and what is below it; on the outermost, all of them
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Share", "2", "衣服"), "mode": ""}); r["ok"] != true {
		t.Fatal(r)
	}
	if got := bundleMarks(st, root); got != "share/2=bundle share=bundle" {
		t.Errorf("marks after 恢复 inside: %s", got)
	}
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Share"), "mode": ""}); r["ok"] != true {
		t.Fatal(r)
	}
	if got := bundleMarks(st, root); got != "" {
		t.Errorf("marks after 恢复: %s", got)
	}
	if got := bundleCards(st); got != "Share=7, Wolf Tail" {
		t.Fatalf("after 恢复: %s", got)
	}
	// 「作为单个素材」: no collection any more, at once and after the scan; nothing in it is marked
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Share"), "mode": "asset"}); r["ok"] != true {
		t.Fatal(r)
	}
	st.Mu.RLock()
	now := ""
	for _, v := range library.AllViews(st) {
		now += v.Name + "=" + strconv.Itoa(v.Bundle) + " "
	}
	st.Mu.RUnlock()
	if !strings.Contains(now, "Share=0 ") {
		t.Errorf("at once: %s", now)
	}
	if got := bundleCards(st); got != "Share, Wolf Tail" {
		t.Fatalf("kept as one asset: %s", got)
	}
	if r := postJSON(t, srv, "/api/bundle/split", map[string]any{"key": cardKey(t, st, "Share")}); r["ok"] != false || r["err"] != "该素材不是合集包，无需拆分" {
		t.Errorf("splitting what is kept as one asset: %v", r)
	}
	if got := bundleMarks(st, root); got != "share=asset" {
		t.Errorf("marks: %s", got)
	}
}

// 「拆分为多个素材」 (the override "split") on a card that holds a collection reaches its products in one go:
// the levels inside are marked with it. Where there is no collection inside, it is what it always was.
func TestOverrideSplitReachesCollection(t *testing.T) {
	root := t.TempDir()
	collectionOnDisk(t, root)
	testkit.WriteFile(t, filepath.Join(root, "Moon Dress", "Moon Dress_Kaguya.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Moon Dress", "Moon Dress_Plum.unitypackage"), 100)
	st, _ := scannedLibrary(t, root)
	st.Settings.HideZh = true // (no names are sent to the translator: there is no network here)
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Share"), "mode": "split"}); r["ok"] != true {
		t.Fatal(r)
	}
	if got := bundleMarks(st, root); got != "share/2/衣服=bundle share/2=bundle share=split" {
		t.Errorf("marks: %s", got)
	}
	if got := bundleCards(st); got != strings.Replace(splitCards, "Night Gown", "Moon Dress, Night Gown", 1) {
		t.Fatalf("after the split: %s", got)
	}
	// 恢复 of the player's own split: the folder is one card again, a collection as it was, and the marks the
	// program put below it with that split go too
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Share"), "mode": ""}); r["ok"] != true {
		t.Fatal(r)
	}
	if got := bundleMarks(st, root); got != "" {
		t.Errorf("marks after 恢复: %s", got)
	}
	if got := bundleCards(st); got != "Moon Dress, Share=7, Wolf Tail" {
		t.Fatalf("after 恢复: %s", got)
	}
	// a product split by the player: its two packages become two cards as before, and nothing else is marked
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Moon Dress"), "mode": "split"}); r["ok"] != true {
		t.Fatal(r)
	}
	if got := bundleMarks(st, root); got != "moon dress=split" {
		t.Errorf("marks: %s", got)
	}
	if got := bundleCards(st); got != "Moon Dress_Kaguya, Moon Dress_Plum, Share=7, Wolf Tail" {
		t.Fatalf("a product split: %s", got)
	}
}

// 「拆分为多个素材」 on a folder that only wraps another one (a download's folder with the one folder its archive
// unpacked to): what is split is what lies inside — also where nothing says "collection" (three products and
// a readme: too few for the structure alone, and the scan's own guess wants more to go by).
func TestOverrideSplitOpensWrappers(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"Alpha Dress", "Beta Coat", "Gamma Hair"} {
		testkit.WriteFile(t, filepath.Join(root, "Pack", "inner", "2", n, n+".unitypackage"), 100)
	}
	testkit.WriteFile(t, filepath.Join(root, "Pack", "inner", "2", "readme.txt"), 10)
	testkit.WriteFile(t, filepath.Join(root, "Wolf Tail", "Wolf Tail.unitypackage"), 100)
	st, _ := scannedLibrary(t, root)
	st.Settings.HideZh = true // (no names are sent to the translator: there is no network here)
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	if got := bundleCards(st); got != "Pack, Wolf Tail" {
		t.Fatalf("before: %s", got)
	}
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Pack"), "mode": "split"}); r["ok"] != true {
		t.Fatal(r)
	}
	if got := bundleMarks(st, root); got != "pack/inner/2=bundle pack/inner=bundle pack=split" {
		t.Errorf("marks: %s", got)
	}
	if got := bundleCards(st); got != "Alpha Dress, Beta Coat, Gamma Hair, Wolf Tail" {
		t.Fatalf("after the split: %s", got)
	}
	// one 恢复 and it is one card again, with nothing left behind
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Pack"), "mode": ""}); r["ok"] != true {
		t.Fatal(r)
	}
	if got := bundleMarks(st, root); got != "" {
		t.Errorf("marks after 恢复: %s", got)
	}
	if got := bundleCards(st); got != "Pack, Wolf Tail" {
		t.Fatalf("after 恢复: %s", got)
	}
	// a product's own folder is not opened: a split of it is what it always was
	if r := postJSON(t, srv, "/api/override", map[string]any{"path": filepath.Join(root, "Wolf Tail"), "mode": "split"}); r["ok"] != true {
		t.Fatal(r)
	}
	if got := bundleMarks(st, root); got != "wolf tail=split" {
		t.Errorf("marks: %s", got)
	}
}

// A collection that is still packed — one archive with the products' archives in it: the split is a job that
// unpacks it first, without a project.
func TestBundleSplitPacked(t *testing.T) {
	root := t.TempDir()
	product := func(n string) []byte {
		return testkit.ZipBytes(t, map[string][]byte{n + ".unitypackage": []byte("package of " + n), "readme.txt": []byte("x")})
	}
	pack := testkit.ZipBytes(t, map[string][]byte{"Sailor Dress.zip": product("Sailor Dress"), "Twintail.zip": product("Twintail"), "说明.txt": []byte("x")})
	_ = os.MkdirAll(filepath.Join(root, "Share"), 0755)
	if err := os.WriteFile(filepath.Join(root, "Share", "2.zip"), pack, 0644); err != nil {
		t.Fatal(err)
	}
	st, _ := scannedLibrary(t, root)
	st.Settings.HideZh, st.Settings.NoWatch, st.Settings.AutoBooth = true, true, false
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	t.Cleanup(unity.DismissImport)
	if got := bundleCards(st); got != "Share=2 packed" {
		t.Fatalf("before: %s", got)
	}
	if r := postJSON(t, srv, "/api/bundle/split", map[string]any{"key": cardKey(t, st, "Share"), "recycle": true}); r["ok"] != true || r["job"] != true || r["n"] != nil {
		t.Fatalf("split: %v", r)
	}
	var j *unity.ImportJob
	for i := 0; i < 1000; i++ {
		if j = unity.ImportSnapshot(); j != nil && (j.Stage == "done" || j.Stage == "failed") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if j == nil || j.Stage != "done" || !j.Split || j.Bundle != 2 || j.Msg != "已拆分为 2 个素材" || j.Project != "" {
		t.Fatalf("the job: %+v", j)
	}
	// the window is told of the job as of an import, and puts it away
	if job, _ := postJSON(t, srv, "/api/progress", nil)["importJob"].(map[string]any); job["split"] != true || job["bundle"] != float64(2) || job["project"] != "" {
		t.Errorf("the job as the window gets it: %v", job)
	}
	if r := postJSON(t, srv, "/api/import/dismiss", nil); r["ok"] != true || unity.ImportSnapshot() != nil {
		t.Errorf("dismiss: %v", r)
	}
	if got := bundleCards(st); got != "Sailor Dress, Twintail" {
		t.Fatalf("after: %s", got)
	}
	if got := bundleMarks(st, root); got != "share/2=bundle share=bundle" {
		t.Errorf("marks: %s", got)
	}
	if got := strings.Join(testkit.ListTree(root), ","); got != "Share/2/Sailor Dress/Sailor Dress.unitypackage,Share/2/Sailor Dress/readme.txt,Share/2/Twintail/Twintail.unitypackage,Share/2/Twintail/readme.txt,Share/2/说明.txt" {
		t.Errorf("on disk: %s", got)
	}
}
