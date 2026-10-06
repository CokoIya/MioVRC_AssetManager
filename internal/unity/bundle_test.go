package unity

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
)

// packageOf: a small unitypackage of a product — one prefab under Assets/<name>/.
func packageOf(t *testing.T, name string) []byte {
	t.Helper()
	sum := md5.Sum([]byte(name))
	p := filepath.Join(t.TempDir(), "p.unitypackage")
	makeUnityPackage(t, p, []pkgFile{{guid: hex.EncodeToString(sum[:]), path: "Assets/" + name + "/" + name + ".prefab", body: name}})
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// productZip: a product as a seller packs it.
func productZip(t *testing.T, name string) []byte {
	return testkit.ZipBytes(t, map[string][]byte{name + ".unitypackage": packageOf(t, name), "readme.txt": []byte("x")})
}

func put(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
}

// bundleLibrary: a library with one asset folder, nothing in it yet.
func bundleLibrary(t *testing.T) (*core.Store, string) {
	t.Helper()
	st := testkit.NewStore(t)
	root := t.TempDir()
	st.Settings.Roots, st.Settings.NoWatch, st.Settings.AutoBooth = []string{root}, true, false
	st.Settings.HideZh = true // (no names are sent to the translator: there is no network here)
	t.Cleanup(func() {
		settle()
		DismissImport()
	})
	return st, root
}

// settle waits for what follows an import or a split: the scan — begun a moment after the job's end is told —
// and the look into the unitypackages after it. Both read the data folder, which the next test replaces.
func settle() {
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < 2000 && library.BackgroundBusy(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
}

// cards: the library's cards after a scan — "name" for a product, "name=N" for a collection of N,
// "name=N packed" for one that has to be unpacked to get at its products.
func cards(st *core.Store) string {
	settle()
	library.RunFolderScan(st, &core.Task{})
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

func keyOf(t *testing.T, st *core.Store, name string) string {
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

// ended waits for the job that was started and returns it as it ended.
func ended(t *testing.T) ImportJob {
	t.Helper()
	for i := 0; i < 1000; i++ {
		if j := ImportSnapshot(); j != nil && (j.Stage == "done" || j.Stage == "failed") {
			settle()
			DismissImport()
			return *j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the job did not end")
	return ImportJob{}
}

func marks(st *core.Store, root string) string {
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

func imported(project string) string {
	var out []string
	for _, f := range testkit.ListTree(filepath.Join(project, "Assets")) {
		if !strings.HasSuffix(f, ".meta") {
			out = append(out, f)
		}
	}
	return strings.Join(out, ",")
}

// A card that is a collection of products is not imported: the request is refused with the reason. Said to
// be one asset by the player, it is imported as any other — all of it.
func TestImportRefusesCollection(t *testing.T) {
	st, root := bundleLibrary(t)
	for _, n := range []string{"Sailor Dress", "Twintail", "Cat Ears", "Wolf Tail"} { // (as "2.zip" unpacked into the card's folder)
		put(t, filepath.Join(root, "Pack", "2", n, n+".unitypackage"), packageOf(t, n))
	}
	put(t, filepath.Join(root, "Maid Outfit", "Maid Outfit.unitypackage"), packageOf(t, "Maid Outfit"))
	if got := cards(st); got != "Maid Outfit, Pack=4" {
		t.Fatalf("cards: %s", got)
	}
	proj := newProject(t)
	err := StartImport(st, ImportReq{Key: keyOf(t, st, "Pack"), Project: proj})
	if err == nil || err.Error() != "合集包包含 4 个素材，请先拆分，再分别导入" {
		t.Fatalf("importing the collection: %v", err)
	}
	if j := ImportSnapshot(); j != nil || imported(proj) != "" {
		t.Fatalf("a job began: %+v; in the project: %s", j, imported(proj))
	}
	// the product beside it is imported as ever
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Maid Outfit"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 0 || j.Split || imported(proj) != "Maid Outfit/Maid Outfit.prefab" {
		t.Fatalf("the product: %+v; in the project: %s", j, imported(proj))
	}
	// "this is one asset": imported whole, and not split by the import either
	st.Mu.Lock()
	st.Overrides[core.PathKey(filepath.Join(root, "Pack"))] = "asset"
	st.Mu.Unlock()
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Pack"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || len(j.Imported) != 4 || j.Bundle != 0 {
		t.Fatalf("kept as one asset: %+v", j)
	}
	if got := marks(st, root); got != "pack=asset" {
		t.Errorf("marks: %s", got)
	}
}

// An archive of archives that the player said is one asset: it is imported whole, and what was said goes
// over to the folder the import made of it — the card is not taken for a collection again.
func TestImportOfArchiveKeptAsOneAsset(t *testing.T) {
	st, root := bundleLibrary(t)
	set := testkit.ZipBytes(t, map[string][]byte{"Top.zip": productZip(t, "Top"), "Skirt.zip": productZip(t, "Skirt"), "Boots.zip": productZip(t, "Boots")})
	for _, n := range []string{"Outfit Set", "Other Set"} {
		put(t, filepath.Join(root, n+".zip"), set)
	}
	if got := cards(st); got != "Other Set=3 packed, Outfit Set=3 packed" {
		t.Fatalf("before: %s", got)
	}
	st.Mu.Lock()
	st.Overrides[core.PathKey(filepath.Join(root, "Outfit Set.zip"))] = "asset"
	st.Overrides[core.PathKey(filepath.Join(root, "Other Set.zip"))] = "asset"
	st.Mu.Unlock()
	proj := newProject(t)
	// the archive goes to the Recycle Bin: what was said about it is said about the folder now
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Outfit Set"), Project: proj, Recycle: true}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 0 || len(j.Imported) != 3 {
		t.Fatalf("the job: %+v", j)
	}
	// the archive stays: both are kept as one asset
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Other Set"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 0 || len(j.Imported) != 3 {
		t.Fatalf("the job, archive kept: %+v", j)
	}
	if got := marks(st, root); got != "other set.zip=asset other set=asset outfit set=asset" {
		t.Errorf("marks: %s", got)
	}
	if got := cards(st); got != "Other Set, Outfit Set" {
		t.Errorf("after: %s", got)
	}
}

// Archives that lie side by side in an asset's folder are no archives out of an archive: a tool and the shader
// it needs come like that. The import unpacks both and imports both, as it always did.
func TestImportOfProductWithWhatItNeeds(t *testing.T) {
	st, root := bundleLibrary(t)
	put(t, filepath.Join(root, "Setup Tool", "Setup Tool.zip"), productZip(t, "Setup Tool"))
	put(t, filepath.Join(root, "Setup Tool", "Shader It Needs.zip"), productZip(t, "Shader It Needs"))
	if got := cards(st); got != "Setup Tool" {
		t.Fatalf("before: %s", got)
	}
	proj := newProject(t)
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Setup Tool"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 0 || len(j.Imported) != 2 || j.Unpacked != 2 {
		t.Fatalf("the job: %+v", j)
	}
	if got := imported(proj); got != "Setup Tool/Setup Tool.prefab,Shader It Needs/Shader It Needs.prefab" {
		t.Errorf("in the project: %s", got)
	}
	if got := marks(st, root); got != "" {
		t.Errorf("marks: %s", got)
	}
	if got := cards(st); got != "Setup Tool" {
		t.Errorf("after: %s", got)
	}
}

// What cannot be told before it is unpacked — archives in an archive in an archive here, a rar or a 7z in a
// player's library — is told once the import has unpacked it: nothing is imported, the collection's folder
// levels are marked, the job says why it stopped, and the scan makes a card for each product.
func TestImportSplitsWhatUnpackingShows(t *testing.T) {
	st, root := bundleLibrary(t)
	inner := testkit.ZipBytes(t, map[string][]byte{"Sailor Dress.zip": productZip(t, "Sailor Dress"), "Twintail.zip": productZip(t, "Twintail")})
	put(t, filepath.Join(root, "Mystery", "Outer.zip"), testkit.ZipBytes(t, map[string][]byte{"Inner.zip": inner}))
	if got := cards(st); got != "Mystery" {
		t.Fatalf("before: %s", got)
	}
	proj := newProject(t)
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Mystery"), Project: proj, Recycle: true}); err != nil {
		t.Fatal(err)
	}
	j := ended(t)
	if j.Stage != "failed" || j.Bundle != 2 || j.Err != "该素材是合集包（包含 2 个素材），已拆分为独立的素材卡片，请分别导入" || j.Msg != j.Err || j.Unpacked != 4 {
		t.Fatalf("the job: %+v", j)
	}
	if imported(proj) != "" {
		t.Fatalf("imported all the same: %s", imported(proj))
	}
	if got := marks(st, root); got != "mystery/outer/inner=bundle mystery/outer=bundle mystery=bundle" {
		t.Errorf("marks: %s", got)
	}
	if got := TaskImport.Snapshot().Msg; got != j.Err {
		t.Errorf("the task's line: %s", got)
	}
	if got := cards(st); got != "Sailor Dress, Twintail" {
		t.Fatalf("after: %s", got)
	}
	// each of them is imported from its own card
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Twintail"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || imported(proj) != "Twintail/Twintail.prefab" {
		t.Fatalf("one product: %+v; in the project: %s", j, imported(proj))
	}

	// an asset that is one archive of four products' folders: the folder it unpacks to is the collection
	four := map[string][]byte{}
	for _, n := range []string{"Gothic Coat", "Night Gown", "Summer Bikini", "Bob Hair"} {
		four[n+"/"+n+".unitypackage"] = packageOf(t, n)
	}
	put(t, filepath.Join(root, "Four.zip"), testkit.ZipBytes(t, four))
	if got := cards(st); got != "Four, Sailor Dress, Twintail" {
		t.Fatalf("before: %s", got)
	}
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Four"), Project: proj, Recycle: true}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "failed" || j.Bundle != 4 || !strings.Contains(j.Err, "包含 4 个素材") {
		t.Fatalf("the job: %+v", j)
	}
	if got := cards(st); got != "Bob Hair, Gothic Coat, Night Gown, Sailor Dress, Summer Bikini, Twintail" || imported(proj) != "Twintail/Twintail.prefab" {
		t.Fatalf("after: %s; in the project: %s", got, imported(proj))
	}
}

// A 7z cannot be looked into: its card is a product's until the import has unpacked it.
func TestImportSplitsCollectionIn7z(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z here")
	}
	st, root := bundleLibrary(t)
	src := t.TempDir()
	for _, n := range []string{"Sailor Dress", "Twintail", "Cat Ears"} {
		put(t, filepath.Join(src, n+".zip"), productZip(t, n))
	}
	_ = os.MkdirAll(filepath.Join(root, "Seven"), 0755)
	cmd := exec.Command("7z", "a", "-mx=0", filepath.Join(root, "Seven", "2.7z"), filepath.Join(src, "Sailor Dress.zip"), filepath.Join(src, "Twintail.zip"), filepath.Join(src, "Cat Ears.zip"))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z: %v %s", err, b)
	}
	if got := cards(st); got != "Seven" {
		t.Fatalf("before: %s", got)
	}
	proj := newProject(t)
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Seven"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "failed" || j.Bundle != 3 || imported(proj) != "" {
		t.Fatalf("the job: %+v; in the project: %s", j, imported(proj))
	}
	if got := marks(st, root); got != "seven/2=bundle seven=bundle" {
		t.Errorf("marks: %s", got)
	}
	// (the archives were kept: the 7z beside the folder made of it is no card of its own)
	if got := cards(st); got != "Cat Ears, Sailor Dress, Twintail" {
		t.Fatalf("after: %s", got)
	}
}

// Splitting on request: the card's archives are unpacked as an import unpacks them, the collection is marked,
// and the job ends without a project having been asked for. Nothing is removed unless that was asked for.
func TestSplitJob(t *testing.T) {
	st, root := bundleLibrary(t)
	pack := testkit.ZipBytes(t, map[string][]byte{"Sailor Dress.zip": productZip(t, "Sailor Dress"), "Twintail.zip": productZip(t, "Twintail"),
		"Cat Ears.zip": productZip(t, "Cat Ears"), "说明.txt": []byte("x")})
	put(t, filepath.Join(root, "Pack", "2.zip"), pack)
	put(t, filepath.Join(root, "Maid Outfit", "Maid Outfit.zip"), productZip(t, "Maid Outfit"))
	if got := cards(st); got != "Maid Outfit, Pack=3 packed" {
		t.Fatalf("before: %s", got)
	}
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Pack"), Split: true, Project: "anything"}); err != nil {
		t.Fatal(err)
	}
	if label := TaskImport.Snapshot().Label; label != "拆分合集包" {
		for i := 0; i < 200 && TaskImport.Snapshot().Label != "拆分合集包"; i++ {
			time.Sleep(5 * time.Millisecond)
		}
		if label = TaskImport.Snapshot().Label; label != "拆分合集包" {
			t.Errorf("the task is called %q", label)
		}
	}
	j := ended(t)
	if j.Stage != "done" || !j.Split || j.Project != "" || j.Bundle != 3 || j.Msg != "已拆分为 3 个素材" || j.Err != "" || j.Unpacked != 4 || j.Removed != 0 {
		t.Fatalf("the job: %+v", j)
	}
	if got := TaskImport.Snapshot().Msg; got != "完成：已拆分为 3 个素材" {
		t.Errorf("the task's line: %s", got)
	}
	if got := marks(st, root); got != "pack/2=bundle pack=bundle" {
		t.Errorf("marks: %s", got)
	}
	for _, f := range []string{"Pack/2.zip", "Pack/2/Twintail.zip", "Pack/2/Twintail/Twintail.unitypackage", "Pack/2/说明.txt", "Maid Outfit/Maid Outfit.zip"} {
		if !fileThere(filepath.Join(root, filepath.FromSlash(f))) {
			t.Errorf("%s is not there", f)
		}
	}
	if fileThere(filepath.Join(root, "Maid Outfit", "Maid Outfit")) {
		t.Error("another card's archive was unpacked")
	}
	if got := cards(st); got != "Cat Ears, Maid Outfit, Sailor Dress, Twintail" {
		t.Fatalf("after: %s", got)
	}
	// the import that comes after it is called what it is again
	proj := newProject(t)
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Cat Ears"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Split || imported(proj) != "Cat Ears/Cat Ears.prefab" || TaskImport.Snapshot().Label != "导入到 Unity 工程" {
		t.Fatalf("an import after the split: %+v, called %q; in the project: %s", j, TaskImport.Snapshot().Label, imported(proj))
	}

	// with the archives sent to the Recycle Bin, as the import form's box says
	put(t, filepath.Join(root, "More", "3.zip"), testkit.ZipBytes(t, map[string][]byte{"Gothic Coat.zip": productZip(t, "Gothic Coat"), "Night Gown.zip": productZip(t, "Night Gown")}))
	if got := cards(st); !strings.Contains(got, "More=2 packed") {
		t.Fatalf("before: %s", got)
	}
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "More"), Split: true, Recycle: true}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 2 || j.Removed != 3 {
		t.Fatalf("the job: %+v", j)
	}
	if got := strings.Join(testkit.ListTree(filepath.Join(root, "More")), ","); got != "3/Gothic Coat/Gothic Coat.unitypackage,3/Gothic Coat/readme.txt,3/Night Gown/Night Gown.unitypackage,3/Night Gown/readme.txt" {
		t.Errorf("on disk: %s", got)
	}

	// what is no collection after all is left as one card, and the job says so; a card that is not there
	// and one that is not on this disk are refused as for an import
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Maid Outfit"), Split: true}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 0 || j.Msg != "未发现多个素材，未拆分" || j.Unpacked != 1 {
		t.Fatalf("no collection: %+v", j)
	}
	if got := cards(st); got != "Cat Ears, Gothic Coat, Maid Outfit, Night Gown, Sailor Dress, Twintail" {
		t.Fatalf("after: %s", got)
	}
	if err := StartImport(st, ImportReq{Key: "no such card", Split: true}); err == nil || err.Error() != "未找到该素材" {
		t.Errorf("a card that is not there: %v", err)
	}
}

// A collection that is grouped with a product as its "other version" — their names say the same — is not
// brought along when that product is imported, and brings nothing along when it is split.
func TestCollectionInAVersionGroup(t *testing.T) {
	st, root := bundleLibrary(t)
	for _, n := range []string{"Top", "Skirt", "Boots", "Ribbon"} {
		put(t, filepath.Join(root, "Moon Dress", "2", n, n+".unitypackage"), packageOf(t, n))
	}
	put(t, filepath.Join(root, "Moon Dress_Kaguya", "Moon Dress_Kaguya.unitypackage"), packageOf(t, "Moon Dress_Kaguya"))
	put(t, filepath.Join(root, "Sun Dress", "2.zip"), testkit.ZipBytes(t, map[string][]byte{"Coat.zip": productZip(t, "Coat"), "Scarf.zip": productZip(t, "Scarf")}))
	put(t, filepath.Join(root, "Sun Dress_Plum", "Sun Dress_Plum.zip"), productZip(t, "Sun Dress_Plum"))
	if got := cards(st); got != "Moon Dress=4, Moon Dress_Kaguya, Sun Dress=2 packed, Sun Dress_Plum" {
		t.Fatalf("before: %s", got)
	}
	st.Mu.RLock()
	groups := map[string]string{}
	for _, v := range library.AllViews(st) {
		groups[v.Name] = v.Group
	}
	st.Mu.RUnlock()
	if groups["Moon Dress"] == "" || groups["Moon Dress"] != groups["Moon Dress_Kaguya"] || groups["Sun Dress"] == "" || groups["Sun Dress"] != groups["Sun Dress_Plum"] {
		t.Fatalf("the cards are not grouped as this test needs them: %v", groups)
	}
	proj := newProject(t)
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Moon Dress_Kaguya"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 0 || len(j.Imported) != 1 || imported(proj) != "Moon Dress_Kaguya/Moon Dress_Kaguya.prefab" {
		t.Fatalf("the product: %+v; in the project: %s", j, imported(proj))
	}
	if got := marks(st, root); got != "" {
		t.Errorf("marks: %s", got)
	}
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Sun Dress"), Split: true}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 2 || j.Unpacked != 3 {
		t.Fatalf("the split: %+v", j)
	}
	if fileThere(filepath.Join(root, "Sun Dress_Plum", "Sun Dress_Plum")) || !fileThere(filepath.Join(root, "Sun Dress_Plum", "Sun Dress_Plum.zip")) {
		t.Error("the other version was unpacked with the collection")
	}
	if got := cards(st); got != "Coat, Moon Dress=4, Moon Dress_Kaguya, Scarf, Sun Dress_Plum" {
		t.Errorf("after: %s", got)
	}
}

// An encrypted collection: the split says that a password is needed, and that the one given is wrong.
func TestSplitJobPassword(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z here")
	}
	st, root := bundleLibrary(t)
	src := t.TempDir()
	for _, n := range []string{"Sailor Dress", "Twintail"} {
		put(t, filepath.Join(src, n+".zip"), productZip(t, n))
	}
	_ = os.MkdirAll(filepath.Join(root, "Locked"), 0755)
	cmd := exec.Command("7z", "a", "-mx=0", "-psecret", "-mhe=on", filepath.Join(root, "Locked", "2.7z"), filepath.Join(src, "Sailor Dress.zip"), filepath.Join(src, "Twintail.zip"))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z: %v %s", err, b)
	}
	if got := cards(st); got != "Locked" {
		t.Fatalf("before: %s", got)
	}
	key := keyOf(t, st, "Locked")
	for pwd, want := range map[string]string{"": "压缩包已加密，请在「解压密码」中填写密码后重新拆分",
		"wrong": "解压密码错误，请更换密码后重试（密码通常位于商品说明、卖家消息或压缩包旁的文本文件中）"} {
		if err := StartImport(st, ImportReq{Key: key, Split: true, Pwd: pwd}); err != nil {
			t.Fatal(err)
		}
		if j := ended(t); j.Stage != "failed" || j.Err != want || j.Bundle != 0 || len(j.Failed) != 1 {
			t.Errorf("password %q: %+v", pwd, j)
		}
	}
	if err := StartImport(st, ImportReq{Key: key, Split: true, Pwd: "secret"}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || j.Bundle != 2 || j.Msg != "已拆分为 2 个素材" {
		t.Fatalf("with the password: %+v", j)
	}
	if got := cards(st); got != "Sailor Dress, Twintail" {
		t.Fatalf("after: %s", got)
	}
}
