package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// An Avatar Explorer folder inside an asset folder: its items are assets, its database, pictures and backups
// are not; what it knows about the items is taken over without touching what the player set here.
func TestAvatarExplorerV1(t *testing.T) {
	root := t.TempDir()
	ae := filepath.Join(root, "Avatar Explorer")
	items := filepath.Join(ae, "Datas", "Items")
	testkit.WriteFile(t, filepath.Join(items, "Kikyo Avatar", "Kikyo.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(items, "Maid Dress", "MaidDress.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(items, "Cat Ears", "CatEars.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(ae, "Datas", "Thumbnail", "maid.png"), 30*1024)
	testkit.WriteFile(t, filepath.Join(ae, "Datas", "AuthorImage", "shop.png"), 30*1024)
	testkit.WriteFile(t, filepath.Join(ae, "Avatar Explorer.exe"), 100)
	_ = os.MkdirAll(filepath.Join(ae, "Backup"), 0755)
	testkit.MakeZip(t, filepath.Join(ae, "Backup", "2026-09-30-21-15-02.zip"), map[string]string{"ItemsData.json": "[]", "Items/Maid Dress/MaidDress_Kikyo.unitypackage": "x"})
	testkit.WriteFile(t, filepath.Join(ae, "Output", "list.csv"), 100)
	// a backup copied elsewhere, a V2 backup folder, and an ordinary asset that only looks like a date
	testkit.MakeZip(t, filepath.Join(root, "2026-08-01-10-00-00.zip"), map[string]string{"ItemsData.json": "[]"})
	testkit.WriteFile(t, filepath.Join(root, "backups", "2026-08-02 10-00-00", "items.json"), 100)
	testkit.WriteFile(t, filepath.Join(root, "backups", "2026-08-02 10-00-00", "commonAvatars.json"), 100)
	testkit.MakeZip(t, filepath.Join(root, "2026-07-01-09-00-00.zip"), map[string]string{"Hat/Hat.unitypackage": "x"})
	testkit.WriteFile(t, filepath.Join(root, "Sailor Dress", "Sailor.unitypackage"), 100)
	db := `[
 {"Title":"【オリジナル3Dモデル】桔梗 Kikyo","AuthorName":"Ponderogen","BoothId":3681787,"ItemPath":"./Datas/Items/Kikyo Avatar","Type":0},
 {"Title":"Maid Dress for Kikyo","AuthorName":"Some Shop","ItemMemo":"买的第一件","BoothId":-1,"ItemPath":"Datas\\Items\\Maid Dress","ImagePath":"./Datas/Thumbnail/maid.png","Type":1,"SupportedAvatar":["./Datas/Items/Kikyo Avatar"],"Tags":["女仆","黑白"]},
 {"Title":"Cat Ears","BoothId":-1,"ItemPath":"` + strings.ReplaceAll(filepath.Join(items, "Cat Ears"), `\`, `\\`) + `","Type":9,"CustomCategory":"耳朵"},
 {"Title":"Gone","BoothId":-1,"ItemPath":"./Datas/Items/Gone","Type":1}
]`
	if err := os.WriteFile(filepath.Join(ae, "Datas", "ItemsData.json"), []byte("\xef\xbb\xbf"+db), 0644); err != nil {
		t.Fatal(err)
	}
	st, byName := scanFor(t, root)
	var names []string
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, want := range []string{"Kikyo Avatar", "Maid Dress", "Cat Ears", "Sailor Dress"} {
		if byName[want] == nil {
			t.Errorf("%q is not an asset: %v", want, names)
		}
	}
	if len(byName) != 5 { // the four and the zip that only has a date for a name
		t.Errorf("assets: %v", names)
	}
	for _, n := range names {
		if l := strings.ToLower(n); strings.Contains(l, "backup") || strings.Contains(l, "thumbnail") || strings.Contains(l, "avatar explorer") || strings.Contains(l, "datas") {
			t.Errorf("%q was taken for an asset", n)
		}
	}
	if got := AEFolders(); len(got) != 1 || core.PathKey(got[0]) != core.PathKey(ae) {
		t.Errorf("found: %v", got)
	}

	pv, err := PreviewAE(st, filepath.Join(ae, "Datas")) // its Datas folder is as good as the folder itself
	if err != nil || pv.Version != 1 || pv.Items != 4 || pv.Matched != 3 || pv.Missing != 1 || pv.Outside != 0 || pv.AddRoot != "" {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	// what the player wrote here stays
	maid := byName["Maid Dress"]
	st.User[maid.Key] = &core.UserData{Notes: "我的备注", Tags: []string{"黑白"}}
	res, err := ApplyAE(st, ae, false)
	if err != nil || res.Matched != 3 || res.Booth != 1 || res.Skipped != 1 || res.Covers != 1 {
		t.Fatalf("apply: %+v %v", res, err)
	}
	u := st.User[maid.Key]
	if u.Notes != "我的备注" || strings.Join(u.Tags, ",") != "黑白,女仆" || u.Name != "Maid Dress for Kikyo" || u.Category != "" {
		t.Errorf("maid dress: %+v", u)
	}
	if !core.ContainsStr(u.Bases, "Kikyo") {
		t.Errorf("the avatar it is for did not become a base body: %v", u.Bases)
	}
	if u.Cover == "" || !core.UnderDir(u.Cover, core.DataDir) || !core.FileExists(u.Cover) {
		t.Errorf("cover: %q", u.Cover)
	}
	if k := st.User[byName["Kikyo Avatar"].Key]; k == nil || k.BoothURL != "https://booth.pm/ja/items/3681787" || k.Name != "" {
		t.Errorf("the avatar: %+v", k)
	}
	if c := st.User[byName["Cat Ears"].Key]; c == nil || !core.ContainsStr(c.Tags, "耳朵") {
		t.Errorf("custom category did not become a tag: %+v", c)
	}
	// a second time changes nothing
	if res, _ := ApplyAE(st, ae, false); res.Matched != 0 {
		t.Errorf("second apply: %+v", res)
	}
	if _, err := ReadAE(root); err == nil {
		t.Error("a folder that is not Avatar Explorer's was read")
	}
}

// V2 keeps its library in a folder of its own, usually outside the asset folders: its item folder can be added.
func TestAvatarExplorerV2(t *testing.T) {
	root, home := t.TempDir(), filepath.Join(t.TempDir(), "Avatar Explorer V2")
	testkit.WriteFile(t, filepath.Join(root, "Sailor Dress", "Sailor.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(home, "items", "Selestia", "Selestia.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(home, "items", "Hoodie", "Hoodie.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(home, "images", "item_thumbnails", "h.png"), 30*1024)
	testkit.WriteFile(t, filepath.Join(home, "backups", "2026-10-01 08-00-00", "items.json"), 100)
	db := `{"Version":3,"Items":[
 {"Id":"a1","Title":"セレスティア Selestia","Author":"Jingo","BoothId":4035411,"ItemPath":"<root>\\Selestia","Category":{"Type":1,"CustomCategory":""}},
 {"Id":"h1","Title":"Hoodie","BoothId":-1,"ItemPath":"<root>/Hoodie","ThumbnailFileName":"h.png","Category":{"Type":2},"SupportedAvatars":["item:a1"],"Tags":["卫衣"],"ItemMemo":"memo"},
 {"Id":"x1","Title":"Elsewhere","BoothId":-1,"ItemPath":"` + strings.ReplaceAll(filepath.Join(root, "Sailor Dress"), `\`, `\\`) + `","Category":{"Type":"Clothing"}}
]}`
	testkit.WriteFile(t, filepath.Join(home, "database", "items.json"), 1)
	if err := os.WriteFile(filepath.Join(home, "database", "items.json"), []byte(db), 0644); err != nil {
		t.Fatal(err)
	}
	st, _ := scanFor(t, root)
	pv, err := PreviewAE(st, home)
	if err != nil || pv.Version != 2 || pv.Items != 3 || pv.Matched != 1 || pv.Outside != 2 || pv.InFolder != 2 || core.PathKey(pv.AddRoot) != core.PathKey(filepath.Join(home, "items")) {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	res, err := ApplyAE(st, home, true)
	if err != nil || res.AddRoot == "" || res.Skipped != 2 {
		t.Fatalf("apply: %+v %v", res, err)
	}
	RunFolderScan(st, &core.Task{}) // the folder that was added is scanned …
	res, err = ApplyAE(st, home, true)
	if err != nil || res.AddRoot != "" || res.Booth != 1 || res.Covers != 1 || res.Skipped != 0 {
		t.Fatalf("after the scan: %+v %v", res, err)
	}
	for _, a := range st.Assets {
		if a.Name == "Hoodie" {
			if u := st.User[a.Key]; u == nil || u.Notes != "memo" || !core.ContainsStr(u.Bases, "Selestia") || !core.ContainsStr(u.Tags, "卫衣") {
				t.Errorf("hoodie: %+v", u)
			}
		}
	}
	// the whole data folder as an asset folder: only its items are assets
	_, byName := scanFor(t, filepath.Dir(home))
	if len(byName) != 2 || byName["Selestia"] == nil || byName["Hoodie"] == nil {
		t.Errorf("assets of the data folder: %v", byName)
	}
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }

// An item is given to the asset at its folder before any item that only lies inside that asset; an item whose
// folder is gone gives nothing to the asset above it; the preview and the import count the same items.
func TestAEAttachesToTheRightAsset(t *testing.T) {
	root := t.TempDir()
	ae := filepath.Join(t.TempDir(), "AE")
	kik := filepath.Join(root, "Kikyo")
	testkit.WriteFile(t, filepath.Join(kik, "Kikyo.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(kik, "Outfits", "DressA", "DressA.zip"), 100)
	sel := filepath.Join(root, "Selestia")
	testkit.WriteFile(t, filepath.Join(sel, "Selestia.unitypackage"), 100)
	moe := filepath.Join(root, "Moe")
	testkit.WriteFile(t, filepath.Join(moe, "Moe.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(moe, "Hats", "Beret", "Beret.zip"), 100)
	// the dress comes first in the file and lies inside the avatar's folder; the avatar is its own item
	db := `[
 {"Title":"Dress A","BoothId":1111111,"ItemPath":` + jsonStr(filepath.Join(kik, "Outfits", "DressA")) + `,"Type":1,"ItemMemo":"dress memo"},
 {"Title":"Kikyo","BoothId":3681787,"ItemPath":` + jsonStr(kik) + `,"Type":0,"ItemMemo":"avatar memo"},
 {"Title":"Old Wig (deleted long ago)","BoothId":2222222,"ItemPath":` + jsonStr(filepath.Join(sel, "OldWig")) + `,"Type":5},
 {"Title":"Beret","BoothId":-1,"ItemPath":` + jsonStr(filepath.Join(moe, "Hats", "Beret")) + `,"Type":4,"ItemMemo":"beret memo"}
]`
	_ = os.MkdirAll(filepath.Join(ae, "Datas"), 0755)
	if err := os.WriteFile(filepath.Join(ae, "Datas", "ItemsData.json"), []byte(db), 0644); err != nil {
		t.Fatal(err)
	}
	st, byName := scanFor(t, root)
	if len(byName) != 3 {
		t.Fatalf("assets: %v", keysOf(byName))
	}
	pv, err := PreviewAE(st, ae)
	if err != nil || pv.Items != 4 || pv.Matched != 3 || pv.Missing != 1 || pv.Outside != 0 {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	res, err := ApplyAE(st, ae, false)
	if err != nil || res.Skipped != pv.Items-pv.Matched || res.Matched != 2 || res.Booth != 1 {
		t.Fatalf("apply: %+v %v (preview %+v)", res, err, pv)
	}
	if u := st.User[byName["Kikyo"].Key]; u == nil || u.BoothURL != "https://booth.pm/ja/items/3681787" || u.Notes != "avatar memo" || u.Category == "衣服" {
		t.Errorf("the avatar took the dress's information: %+v", u)
	}
	if u := st.User[byName["Selestia"].Key]; u != nil && (u.BoothURL != "" || u.Category != "") {
		t.Errorf("an item whose folder is gone gave its information to the asset above it: %+v", u)
	}
	// an item inside an asset no item is at still describes that asset, as before
	if u := st.User[byName["Moe"].Key]; u == nil || u.Notes != "beret memo" {
		t.Errorf("an item inside an asset of its own: %+v", u)
	}
	if res, _ := ApplyAE(st, ae, false); res.Matched != 0 || res.Skipped != 1 {
		t.Errorf("second apply: %+v", res)
	}
}

// A picture path in Avatar Explorer's file is any path: only a picture of this computer is copied into covers/
// (which travels with a library export), and a path on another computer is not even asked for.
func TestAEThumbOnlyPictures(t *testing.T) {
	root := t.TempDir()
	ae := filepath.Join(root, "Free Avatar Pack")
	secret := filepath.Join(t.TempDir(), "id_rsa.txt")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0600); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, filepath.Join(ae, "Datas", "Items", "Hat", "Hat.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(ae, "Datas", "Items", "Wig", "Wig.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(ae, "Datas", "Items", "Shoes", "Shoes.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(ae, "Datas", "Thumbnail", "wig.PNG"), 30*1024)
	db := `[{"Title":"Hat","BoothId":-1,"ItemPath":"./Datas/Items/Hat","ImagePath":` + jsonStr(secret) + `,"Type":4},
 {"Title":"Wig","BoothId":-1,"ItemPath":"./Datas/Items/Wig","ImagePath":"./Datas/Thumbnail/wig.PNG","Type":5},
 {"Title":"Shoes","BoothId":"-1","ItemPath":"./Datas/Items/Shoes","ImagePath":"\\\\evil.example\\share\\shoes.png","Type":1},
 {"Title":"Remote","BoothId":-1,"ItemPath":"\\\\evil.example\\share\\Remote","ImagePath":"//evil.example/share/remote.png","Type":1},
 {"Title":"Remote 2","BoothId":-1,"ItemPath":"//?/UNC/evil.example/share/Remote2","Type":1}]`
	if err := os.WriteFile(filepath.Join(ae, "Datas", "ItemsData.json"), []byte(db), 0644); err != nil {
		t.Fatal(err)
	}
	lib, err := ReadAE(ae)
	if err != nil || len(lib.Items) != 5 {
		t.Fatalf("read: %+v %v", lib, err)
	}
	for _, it := range lib.Items {
		for _, p := range append([]string{it.Thumb}, it.Paths...) {
			if strings.Contains(p, "evil.example") {
				t.Errorf("%s: a path on another computer was kept: %q", it.Title, p)
			}
		}
	}
	if lib.Items[0].Thumb != "" || lib.Items[1].Thumb == "" || len(lib.Items[3].Paths) != 0 {
		t.Errorf("items: %+v", lib.Items)
	}
	// (a network path is the library's own only when the library was read from there)
	if got := aePath(`\\nas\vrc\AE\Datas\Items\Hat`, `\\nas\vrc\AE`); got == "" {
		t.Error("an item in the folder the library is in, on a network share")
	}
	if got := aePath(`\\nas\vrc2\Hat`, `\\nas\vrc`); got != "" {
		t.Errorf("another share: %q", got)
	}
	st, byName := scanFor(t, root)
	res, err := ApplyAE(st, ae, false)
	if err != nil || res.Covers != 1 || res.Skipped != 2 {
		t.Fatalf("apply: %+v %v", res, err)
	}
	if u := st.User[byName["Hat"].Key]; u != nil && u.Cover != "" {
		b, _ := os.ReadFile(u.Cover)
		t.Errorf("a file that is no picture became a cover: %s %q", u.Cover, b)
	}
	if u := st.User[byName["Wig"].Key]; u == nil || !strings.HasSuffix(u.Cover, ".png") || !core.FileExists(u.Cover) {
		t.Errorf("the picture: %+v", u)
	}
	if ents, _ := os.ReadDir(filepath.Join(core.DataDir, "covers", "avatarexplorer")); len(ents) != 1 {
		t.Errorf("covers kept: %d", len(ents))
	}
	// a picture too large to be one is not read: its size is asked for first
	big := filepath.Join(ae, "Datas", "Thumbnail", "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(aeMaxThumb); err != nil { // (sparse: nothing is written)
		t.Fatal(err)
	}
	f.Close()
	if got := aeCopyThumb(big, t.TempDir()); got != "" {
		t.Errorf("a %d MB picture was copied", aeMaxThumb>>20)
	}
}

// One record with a field of another type than expected (a Booth id written as text, a null list, a record
// that is a string) costs that field, not the whole library.
func TestManagerFilesReadLeniently(t *testing.T) {
	ae := t.TempDir()
	_ = os.MkdirAll(filepath.Join(ae, "Datas"), 0755)
	v1 := `[
 {"Title":"A","BoothId":"3681787","ItemPath":"./Datas/Items/A","Type":0,"Tags":null},
 {"Title":"B","BoothId":4035411,"ItemPath":"./Datas/Items/B","Type":"Clothing","Tags":"not a list","SupportedAvatar":["./Datas/Items/A"]},
 "not a record",
 {"Title":123,"BoothId":null,"ItemPath":"./Datas/Items/C","Type":1,"Tags":["x"]}
]`
	if err := os.WriteFile(filepath.Join(ae, "Datas", "ItemsData.json"), []byte(v1), 0644); err != nil {
		t.Fatal(err)
	}
	lib, err := ReadAE(ae)
	if err != nil || len(lib.Items) != 3 {
		t.Fatalf("V1: %+v %v", lib, err)
	}
	if a, b, c := lib.Items[0], lib.Items[1], lib.Items[2]; a.BoothID != 3681787 || b.BoothID != 4035411 || b.Kind != "clothing" || len(b.Supported) != 1 || c.Title != "" || len(c.Paths) != 1 || len(c.Tags) != 1 {
		t.Errorf("V1 items: %+v", lib.Items)
	}
	for _, bad := range []string{`{"Title":"A"}`, `[{"Title":"A",`, `not json`, ``} {
		_ = os.WriteFile(filepath.Join(ae, "Datas", "ItemsData.json"), []byte(bad), 0644)
		if _, err := ReadAE(ae); err == nil {
			t.Errorf("V1 read %q", bad)
		}
	}
	v2home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(v2home, "database"), 0755)
	for _, v2 := range []string{
		`{"Version":"3","Items":[{"Id":"a1","Title":"A","BoothId":"4035411","ItemPath":"<root>/A","Category":{"Type":1}},{"Id":"b1","Title":"B","BoothId":7,"ItemPath":"<root>/B","IsHidden":"yes","Tags":[1,"t"]}]}`,
		`[{"Id":"a1","Title":"A","BoothId":"4035411","ItemPath":"<root>/A","Category":{"Type":1}},{"Id":"b1","Title":"B","BoothId":7,"ItemPath":"<root>/B","Category":"none"}]`,
	} {
		_ = os.WriteFile(filepath.Join(v2home, "database", "items.json"), []byte(v2), 0644)
		lib, err = ReadAE(v2home)
		if err != nil || len(lib.Items) != 2 || lib.Items[0].BoothID != 4035411 || lib.Items[0].Kind != "avatar" || lib.Items[1].BoothID != 7 ||
			core.PathKey(lib.Items[1].Paths[0]) != core.PathKey(filepath.Join(v2home, "items", "B")) {
			t.Errorf("V2: %+v %v", lib, err)
		}
	}
	ka := t.TempDir()
	_ = os.MkdirAll(filepath.Join(ka, "metadata"), 0755)
	meta := `{"version":3,"data":[
 {"id":"` + kaKikyo + `","description":{"name":"Kikyo","creator":"P","imageFilename":null,"tags":[],"memo":null,"boothItemId":"3681787","dependencies":[],"createdAt":"2024-01-01T00:00:00Z","publishedAt":null}},
 {"id":"` + kaMaid + `","description":{"name":"Maid","creator":7,"imageFilename":null,"tags":null,"memo":12,"boothItemId":4444444.0,"dependencies":null,"createdAt":1}}
]}`
	if err := os.WriteFile(filepath.Join(ka, "metadata", "avatars.json"), []byte(meta), 0644); err != nil {
		t.Fatal(err)
	}
	lib, err = ReadKA(ka, "")
	if err != nil || len(lib.Items) != 2 || lib.Items[0].BoothID != 3681787 || lib.Items[1].BoothID != 4444444 || lib.Items[1].Title != "Maid" {
		t.Fatalf("KonoAsset: %+v %v", lib, err)
	}
	for _, bad := range []string{`{"version":3}`, `{"version":3,"data":[`, `"text"`} {
		_ = os.WriteFile(filepath.Join(ka, "metadata", "avatars.json"), []byte(bad), 0644)
		if _, err := ReadKA(ka, ""); err == nil {
			t.Errorf("KonoAsset read %q", bad)
		}
	}
}
