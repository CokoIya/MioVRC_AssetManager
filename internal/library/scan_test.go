package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/testkit"
)

func scanFor(t *testing.T, root string) (*core.Store, map[string]*core.Asset) {
	t.Helper()
	core.DataDir = t.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	st.Settings.Roots = []string{root}
	RunFolderScan(st, &core.Task{})
	byName := map[string]*core.Asset{}
	for _, a := range st.Assets {
		byName[a.Name] = a
	}
	return st, byName
}

// A picture lying next to an asset under its name is that asset's cover.
func TestSiblingCovers(t *testing.T) {
	root := t.TempDir()
	big := 20 * 1024
	// one big folder of loose files, the way old downloads pile up
	testkit.WriteFile(t, filepath.Join(root, "Sailor Dress.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Sailor Dress.png"), big)
	testkit.WriteFile(t, filepath.Join(root, "Maid Outfit v1.2.zip"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Maid Outfit.jpg"), big) // the archive's name starts with the picture's
	testkit.WriteFile(t, filepath.Join(root, "Gothic Coat.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Gothic Coat_preview.webp"), big) // the picture's name starts with the asset's
	testkit.WriteFile(t, filepath.Join(root, "Gothic Coat (2).png"), big)      // "(2)" is a copy mark: the same name
	testkit.WriteFile(t, filepath.Join(root, "Cat Ears.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Cat Ears Black.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Cat.png"), big) // fits two assets equally: nobody's
	testkit.WriteFile(t, filepath.Join(root, "Wolf Tail.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Wolf Tail.png"), 100) // too small to be a picture of anything
	testkit.WriteFile(t, filepath.Join(root, "banner.png"), big)    // belongs to nothing
	// a folder with a picture of its own name next to it, and pictures inside
	testkit.WriteFile(t, filepath.Join(root, "Kimono Set", "Kimono.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Kimono Set", "tex", "body_mask.png"), big)
	testkit.WriteFile(t, filepath.Join(root, "Kimono Set.jpg"), big)
	_, as := scanFor(t, root)
	covers := func(name string) []string {
		a := as[name]
		if a == nil {
			t.Fatalf("no asset %q in %v", name, as)
		}
		var out []string
		for _, c := range a.Covers {
			out = append(out, filepath.Base(c))
		}
		return out
	}
	if c := covers("Sailor Dress"); len(c) != 1 || c[0] != "Sailor Dress.png" {
		t.Errorf("same name: %v", c)
	}
	if c := covers("Maid Outfit v1.2"); len(c) != 1 || c[0] != "Maid Outfit.jpg" {
		t.Errorf("shorter picture name: %v", c)
	}
	if c := strings.Join(covers("Gothic Coat"), ","); !strings.Contains(c, "Gothic Coat_preview.webp") || !strings.Contains(c, "Gothic Coat (2).png") {
		t.Errorf("longer picture name and copy mark: %v", c)
	}
	for _, n := range []string{"Cat Ears", "Cat Ears Black", "Wolf Tail"} {
		if c := covers(n); len(c) != 0 {
			t.Errorf("%s got %v", n, c)
		}
	}
	// the picture named after the folder comes before the pictures inside it
	if c := covers("Kimono Set"); len(c) != 2 || c[0] != "Kimono Set.jpg" {
		t.Errorf("folder: %v", c)
	}
	// the pictures did not become assets, and the folder is still one asset
	if len(as) != 7 {
		t.Errorf("%d assets: %v", len(as), as)
	}
}

// A subfolder where separate downloads lie loose, each with its picture, is a pile of assets, not one asset;
// a product folder with several packages stays one.
func TestPileFolder(t *testing.T) {
	root := t.TempDir()
	big := 20 * 1024
	for _, n := range []string{"Sailor Dress", "Maid Outfit", "Gothic Coat"} {
		testkit.WriteFile(t, filepath.Join(root, "旧素材堆", n+".unitypackage"), 100)
	}
	testkit.WriteFile(t, filepath.Join(root, "旧素材堆", "Sailor Dress.png"), big)
	testkit.WriteFile(t, filepath.Join(root, "旧素材堆", "Maid Outfit.jpg"), big)
	// a product: packages for two base bodies and a picture named after what it shows
	testkit.WriteFile(t, filepath.Join(root, "Kaguya Dress", "Dress_Kaguya.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Kaguya Dress", "Dress_Plum.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Kaguya Dress", "Dress_PSD.zip"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Kaguya Dress", "thumbnail.png"), big)
	// a product with one package and its picture, and loose sources: not a pile either
	testkit.WriteFile(t, filepath.Join(root, "Wolf Tail", "Wolf Tail.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Wolf Tail", "Wolf Tail.png"), big)
	testkit.WriteFile(t, filepath.Join(root, "Wolf Tail", "tail.fbx"), 100)
	// packages with their pictures next to a loose model: one product's files
	testkit.WriteFile(t, filepath.Join(root, "Horn Pack", "A Horn.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Horn Pack", "A Horn.png"), big)
	testkit.WriteFile(t, filepath.Join(root, "Horn Pack", "B Horn.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Horn Pack", "B Horn.png"), big)
	testkit.WriteFile(t, filepath.Join(root, "Horn Pack", "horn.fbx"), 100)
	_, as := scanFor(t, root)
	var names []string
	for n := range as {
		names = append(names, n)
	}
	for _, n := range []string{"Sailor Dress", "Maid Outfit", "Gothic Coat", "Kaguya Dress", "Wolf Tail", "Horn Pack"} {
		if as[n] == nil {
			t.Fatalf("no %q among %v", n, names)
		}
	}
	if len(as) != 6 {
		t.Errorf("assets %v", names)
	}
	if c := as["Sailor Dress"].Covers; len(c) != 1 || filepath.Base(c[0]) != "Sailor Dress.png" {
		t.Errorf("pile cover %v", c)
	}
	if h := as["Sailor Dress"].Hints; len(h) != 1 || h[0] != "旧素材堆" {
		t.Errorf("hints %v", h)
	}
	if len(as["Kaguya Dress"].Packages) != 2 || len(as["Horn Pack"].Packages) != 2 {
		t.Errorf("product folders were split")
	}
}

// The base bodies a Booth product says it is for: title, tags, and the 対応アバター part of the description.
func TestBoothBaseText(t *testing.T) {
	defs := naming.ParseBases([]string{"Plum", "Chocolat", "Kikyo=桔梗", "Manuka=マヌカ", "Selestia=セレスティア", "Moe=萌"})
	b := &core.BoothInfo{Name: "【3D衣装】Gothic Coat", Tags: []string{"VRChat", "3D衣装", "マヌカ"},
		Desc: "ゴシックコートです。\n\n▼対応アバター\n・桔梗\n・Plum\n・セレスティア\n\n同ショップの「萌」向け衣装もよろしく！\n\n※Chocolat は非対応です。\n\nShader: lilToon"}
	got := strings.Join(naming.DetectBases(booth.BoothBaseText(b), defs), ",")
	if got != "Plum,Kikyo,Manuka,Selestia" {
		t.Errorf("bases %q from %q", got, booth.BoothBaseText(b))
	}
	if booth.BoothBaseText(nil) != "" {
		t.Error("text out of nothing")
	}
	// a local asset takes them over from its Booth page
	root := t.TempDir()
	testkit.WriteFile(t, filepath.Join(root, "Gothic Coat 1234567", "coat.unitypackage"), 100)
	st, _ := scanFor(t, root)
	st.Settings.Bases = []string{"Plum", "Chocolat", "Kikyo=桔梗", "Manuka=マヌカ", "Selestia=セレスティア", "Moe=萌"}
	b.ID = "1234567"
	st.Booth["1234567"] = b
	v := BuildView(st, st.Assets[0])
	if strings.Join(v.Bases, ",") != got {
		t.Errorf("view bases %v", v.Bases)
	}
}

// A folder that is a link (symbolic link, junction) is not followed, and the window says so, once per
// folder; a link that leads back into the asset folder does not make the scan go round for ever.
func TestLinkedFolderIsNamed(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	testkit.WriteFile(t, filepath.Join(root, "Alpha Coat", "Alpha Coat.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(other, "Beta Hair", "Beta Hair.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(other, "Gamma Prop.unitypackage"), 100)
	if err := os.Symlink(filepath.Join(other, "Beta Hair"), filepath.Join(root, "Beta Hair")); err != nil {
		t.Skip(err)
	}
	_ = os.Symlink(root, filepath.Join(root, "loop"))
	_ = os.Symlink(filepath.Join(other, "Gamma Prop.unitypackage"), filepath.Join(root, "Gamma Prop.unitypackage")) // a linked file is an asset as before
	core.DataDir = t.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	st.Settings.Roots = []string{root, root} // the same folder twice: still one warning per link
	done := make(chan struct{})
	go func() { RunFolderScan(st, &core.Task{}); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the scan does not end")
	}
	var names []string
	for _, a := range st.Assets {
		names = append(names, a.Name)
	}
	if got := strings.Join(names, ","); got != "Alpha Coat,Gamma Prop" {
		t.Errorf("assets %s", got)
	}
	if len(st.Warnings) != 2 || !strings.Contains(st.Warnings[0], "未扫描链接文件夹") || !strings.Contains(st.Warnings[0], filepath.Join(root, "Beta Hair")) ||
		!strings.Contains(st.Warnings[1], filepath.Join(root, "loop")) {
		t.Errorf("warnings %q", st.Warnings)
	}
}

// Quitting during the background refresh: no step is begun, and a scan cut short does not replace the
// library with the part it had got to.
func TestPipelineStopsWhenQuitting(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"Alpha Coat", "Beta Hair", "Gamma Prop"} {
		testkit.WriteFile(t, filepath.Join(root, n, n+".unitypackage"), 100)
	}
	st, as := scanFor(t, root)
	if len(as) != 3 {
		t.Fatalf("assets %v", as)
	}
	last := st.LastScan
	testkit.WriteFile(t, filepath.Join(root, "Delta Tail", "Delta Tail.unitypackage"), 100)
	core.Quitting.Store(true)
	defer core.Quitting.Store(false)
	RunFolderScan(st, &core.Task{})
	if len(st.Assets) != 3 || st.LastScan != last {
		t.Errorf("a scan cut short was kept: %d assets", len(st.Assets))
	}
	start := time.Now()
	if !StartPipeline(st, true, true, true, false, nil) {
		t.Fatal("not started")
	}
	for PipelineBusy() && time.Since(start) < 5*time.Second {
		time.Sleep(5 * time.Millisecond)
	}
	if PipelineBusy() || len(st.Assets) != 3 || core.TaskScan.Snapshot().Running {
		t.Errorf("busy %v after %v, %d assets", PipelineBusy(), time.Since(start), len(st.Assets))
	}
	core.Quitting.Store(false)
	RunFolderScan(st, &core.Task{})
	if len(st.Assets) != 4 {
		t.Errorf("afterwards: %d assets", len(st.Assets))
	}
}

// Another asset manager's folder may be the player's own asset folder (KonoAsset's data folder pointed at it,
// Avatar Explorer unpacked into it): only what the manager keeps for itself is left out, and everything else
// in it stays in the library. A file of the manager's name is not enough to be taken for one.
func TestManagerHomeKeepsOtherAssets(t *testing.T) {
	write := func(p, body string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	const kaID = "1c8b2f4d-7a1f-4e8c-8b0f-2d3e4f5a6b7c"
	type manager struct {
		name  string
		lay   func(home string) // the manager's own files, with one item ("Maid Dress") and bookkeeping that holds files
		own   []string          // names that must not turn up as assets
		found func() []string
	}
	managers := []manager{
		{"KonoAsset", func(home string) {
			write(filepath.Join(home, "metadata", "avatarWearables.json"), `{"version":3,"data":[{"id":"`+kaID+`","description":{"name":"Maid Dress","creator":"S","imageFilename":"m.png","tags":[],"memo":null,"boothItemId":null,"dependencies":[],"createdAt":1},"category":"衣装","supportedAvatars":[]}]}`)
			write(filepath.Join(home, "metadata", "avatars.json"), `{"version":3,"data":[]}`)
			testkit.WriteFile(t, filepath.Join(home, "images", "m.png"), 30*1024)
			testkit.WriteFile(t, filepath.Join(home, "data", kaID, "Maid Dress", "MaidDress.unitypackage"), 100)
		}, []string{"metadata", "images", "data"}, KAFolders},
		{"Avatar Explorer V2", func(home string) {
			write(filepath.Join(home, "database", "items.json"), `{"Version":3,"Items":[{"Id":"m1","Title":"Maid Dress","BoothId":-1,"ItemPath":"<root>/Maid Dress","Category":{"Type":2}}]}`)
			testkit.WriteFile(t, filepath.Join(home, "images", "item_thumbnails", "m.png"), 30*1024)
			testkit.WriteFile(t, filepath.Join(home, "backups", "2026-10-01 08-00-00", "items.json"), 100)
			testkit.WriteFile(t, filepath.Join(home, "backups", "manual", "items.zip"), 100)
			testkit.WriteFile(t, filepath.Join(home, "settings", "runtimeSettings.json"), 2)
			testkit.WriteFile(t, filepath.Join(home, "logs", "app.log"), 100)
			testkit.WriteFile(t, filepath.Join(home, "items", "Maid Dress", "MaidDress.unitypackage"), 100)
		}, []string{"database", "images", "backups", "settings", "logs", "items", "manual"}, AEFolders},
		{"Avatar Explorer V1", func(home string) {
			write(filepath.Join(home, "Datas", "ItemsData.json"), `[{"Title":"Maid Dress","AuthorName":"S","BoothId":-1,"ItemPath":"./Datas/Items/Maid Dress","ImagePath":"./Datas/Thumbnail/m.png","Type":1}]`)
			testkit.WriteFile(t, filepath.Join(home, "Datas", "Thumbnail", "m.png"), 30*1024)
			testkit.WriteFile(t, filepath.Join(home, "Datas", "AuthorImage", "s.png"), 30*1024)
			testkit.WriteFile(t, filepath.Join(home, "Datas", "Temp", "x.zip"), 100)
			testkit.WriteFile(t, filepath.Join(home, "Datas", "Items", "Maid Dress", "MaidDress.unitypackage"), 100)
			_ = os.MkdirAll(filepath.Join(home, "Backup"), 0755)
			testkit.MakeZip(t, filepath.Join(home, "Backup", "2026-09-30-21-15-02.zip"), map[string]string{"ItemsData.json": "[]"})
			testkit.WriteFile(t, filepath.Join(home, "Backup", "about these backups.txt"), 100)
			testkit.WriteFile(t, filepath.Join(home, "Output", "list.csv"), 100)
			testkit.WriteFile(t, filepath.Join(home, "Avatar Explorer.exe"), 100)
		}, []string{"Datas", "Thumbnail", "AuthorImage", "Temp", "Items", "Backup", "Output", "x"}, AEFolders},
	}
	mine := func(dir string) { // what the player keeps there
		testkit.WriteFile(t, filepath.Join(dir, "Sailor Dress", "Sailor.unitypackage"), 100)
		testkit.WriteFile(t, filepath.Join(dir, "Kikyo", "Kikyo.unitypackage"), 100)
		testkit.WriteFile(t, filepath.Join(dir, "Loose Hat.unitypackage"), 100)
	}
	check := func(what string, byName map[string]*core.Asset, m manager, home string) {
		t.Helper()
		for _, n := range []string{"Sailor Dress", "Kikyo", "Loose Hat", "Maid Dress"} {
			if byName[n] == nil {
				t.Errorf("%s: %q is gone from the library: %v", what, n, keysOf(byName))
			}
		}
		if len(byName) != 4 {
			t.Errorf("%s: assets %v", what, keysOf(byName))
		}
		for _, n := range m.own {
			if byName[n] != nil {
				t.Errorf("%s: its own %q was taken for an asset", what, n)
			}
		}
		if got := m.found(); len(got) != 1 || core.PathKey(got[0]) != core.PathKey(home) {
			t.Errorf("%s: found %v", what, got)
		}
	}
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	for _, m := range managers {
		// the asset folder itself is the manager's folder
		root := t.TempDir()
		mine(root)
		st, byName := scanFor(t, root)
		if len(st.Assets) != 3 {
			t.Fatalf("%s: before: %v", m.name, keysOf(byName))
		}
		m.lay(root)
		RunFolderScan(st, &core.Task{})
		byName = map[string]*core.Asset{}
		for _, a := range st.Assets {
			byName[a.Name] = a
		}
		check(m.name+" in the asset folder itself", byName, m, root)
		// … and a folder in it
		root = t.TempDir()
		home := filepath.Join(root, "Manager")
		mine(home)
		m.lay(home)
		_, byName = scanFor(t, root)
		check(m.name+" one folder down", byName, m, home)
	}
	// what the player put into Avatar Explorer V1's Output, or beside its backups, is theirs
	root := t.TempDir()
	managers[2].lay(root)
	testkit.WriteFile(t, filepath.Join(root, "Output", "Exported Hat.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Backup", "My Old Avatar.zip"), 100)
	if _, byName := scanFor(t, root); byName["Maid Dress"] == nil || len(byName) != 3 {
		t.Errorf("Output and Backup with the player's files: %v", keysOf(byName))
	}

	// a file of the manager's name with something else in it: an ordinary folder, as before 1.7.6
	for _, db := range []string{"metadata/avatars.json", "database/items.json", "Datas/ItemsData.json"} {
		for _, body := range []string{`{"version":3,"data":[]}`, `[]`, `[{"id":1,"name":"Sword","rarity":3}]`, `{"items":[{"name":"Sword"}]}`, `not json`, ``} {
			root := t.TempDir()
			mine(root)
			write(filepath.Join(root, filepath.FromSlash(db)), body)
			_, byName := scanFor(t, root)
			if byName["Sailor Dress"] == nil || byName["Kikyo"] == nil || byName["Loose Hat"] == nil {
				t.Errorf("%s holding %q: assets %v", db, body, keysOf(byName))
			}
			if got := append(AEFolders(), KAFolders()...); len(got) != 0 {
				t.Errorf("%s holding %q: taken for a manager's folder: %v", db, body, got)
			}
		}
	}
	// a product that ships such a file stays one asset, with its package
	root = t.TempDir()
	testkit.WriteFile(t, filepath.Join(root, "RPG Item Gimmick", "RPGItems.unitypackage"), 100)
	write(filepath.Join(root, "RPG Item Gimmick", "database", "items.json"), `[{"id":1,"name":"Sword"},{"id":2,"name":"Shield"}]`)
	testkit.WriteFile(t, filepath.Join(root, "RPG Item Gimmick", "images", "sword.png"), 30*1024)
	testkit.WriteFile(t, filepath.Join(root, "Kikyo", "Kikyo.unitypackage"), 100)
	_, byName := scanFor(t, root)
	if a := byName["RPG Item Gimmick"]; a == nil || len(a.Packages) != 1 || len(a.Covers) != 1 || len(byName) != 2 {
		t.Errorf("a product with its own database/items.json: %v", keysOf(byName))
	}
	// an empty library is a manager's when another of its files lies beside it
	for db, beside := range map[string]string{"metadata/avatars.json": "metadata/otherAssets.json", "database/items.json": "database/commonAvatars.json", "Datas/ItemsData.json": "Datas/CommonAvatar.json"} {
		root := t.TempDir()
		mine(root)
		body := `[]`
		if strings.HasPrefix(db, "metadata") {
			body = `{"version":3,"data":[]}`
		}
		write(filepath.Join(root, filepath.FromSlash(db)), body)
		write(filepath.Join(root, filepath.FromSlash(beside)), body)
		_, byName := scanFor(t, root)
		if len(byName) != 3 || byName["Sailor Dress"] == nil || byName["Kikyo"] == nil || byName["Loose Hat"] == nil {
			t.Errorf("an empty %s: assets %v", db, keysOf(byName))
		}
		if got := append(AEFolders(), KAFolders()...); len(got) != 1 {
			t.Errorf("an empty %s: found %v", db, got)
		}
	}
}
