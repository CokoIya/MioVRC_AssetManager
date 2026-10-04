package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

const (
	kaKikyo  = "0b7a1e3c-6f0e-4d7b-9a9e-610261e9dc76" // (a digit run that is no Booth item number)
	kaMaid   = "1c8b2f4d-7a1f-4e8c-8b0f-2d3e4f5a6b7c"
	kaEars   = "2d9c3a5e-8b2a-4f9d-9c1a-3e4f5a6b7c8d"
	kaHoodie = "3e0d4b6f-9c3b-4a0e-8d2b-4f5a6b7c8d9e"
	kaWorld  = "4f1e5c7a-0d4c-4b1f-9e3c-5a6b7c8d9e0f"
	kaGone   = "5a2f6d8b-1e5d-4c2a-8f4d-6b7c8d9e0f1a"
)

// writeKA lays out a KonoAsset data folder: its metadata, pictures and the item folders named by id.
func writeKA(t *testing.T, home string) {
	t.Helper()
	testkit.WriteFile(t, filepath.Join(home, "data", kaKikyo, "Kikyo_v1.3.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(home, "data", kaMaid, "MaidDress_Kikyo.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(home, "data", kaMaid, "readme.txt"), 20)
	testkit.WriteFile(t, filepath.Join(home, "data", kaEars, "CatEars.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(home, "data", kaHoodie, "Hoodie.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(home, "data", kaWorld, "Lamp.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(home, "images", "maid.png"), 30*1024)
	testkit.WriteFile(t, filepath.Join(home, "images", "ears.jpg"), 30*1024)
	testkit.WriteFile(t, filepath.Join(home, "metadata", "backups", "avatars_1.json"), 100)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(home, "metadata", name), []byte("\xef\xbb\xbf"+body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("avatars.json", `{"version":3,"data":[
 {"id":"`+kaKikyo+`","description":{"name":"【オリジナル3Dモデル】桔梗 Kikyo","creator":"Ponderogen","imageFilename":null,"tags":[],"memo":null,"boothItemId":3681787,"dependencies":[],"createdAt":1700000000000,"publishedAt":null}}
]}`)
	write("avatarWearables.json", `{"version":3,"data":[
 {"id":"`+kaMaid+`","description":{"name":"Maid Dress for Kikyo","creator":"Some Shop","imageFilename":"maid.png","tags":["女仆","黑白"],"memo":"买的第一件","boothItemId":null,"dependencies":["`+kaKikyo+`"],"createdAt":1700000000000,"publishedAt":null},"category":"衣装","supportedAvatars":["桔梗 Kikyo","Selestia"]},
 {"id":"`+kaEars+`","description":{"name":"Cat Ears","creator":"","imageFilename":"ears.jpg","tags":[],"memo":"","boothItemId":null,"dependencies":[],"createdAt":1700000000000},"category":"ケモミミ","supportedAvatars":[]},
 {"id":"`+kaHoodie+`","description":{"name":"Hoodie","creator":"","imageFilename":null,"tags":["卫衣"],"memo":null,"boothItemId":null,"dependencies":[],"createdAt":1700000000000},"category":"謎カテゴリ","supportedAvatars":["桔梗 Kikyo"]},
 {"id":"`+kaGone+`","description":{"name":"Gone","creator":"","imageFilename":null,"tags":[],"memo":null,"boothItemId":null,"dependencies":[],"createdAt":0},"category":"衣装","supportedAvatars":[]}
]}`)
	write("worldObjects.json", `{"version":3,"data":[
 {"id":"`+kaWorld+`","description":{"name":"Lamp","creator":"","imageFilename":null,"tags":[],"memo":null,"boothItemId":null,"dependencies":[],"createdAt":0},"category":"ライト"}
]}`)
	// an early version wrote the list alone
	write("otherAssets.json", `[]`)
}

// A KonoAsset folder inside an asset folder: its item folders are assets (named by what is in them, not by
// their id), its metadata and pictures are not; what it knows about the items is taken over.
func TestKonoAssetInRoot(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "KonoAsset")
	writeKA(t, home)
	testkit.WriteFile(t, filepath.Join(root, "Sailor Dress", "Sailor.unitypackage"), 100)
	st, byName := scanFor(t, root)
	// an item folder holding one package is named after it; one holding more keeps its id for a name until
	// the import names it
	byID := map[string]*core.Asset{}
	for _, a := range st.Assets {
		byID[a.RawName] = a
		if l := strings.ToLower(a.Name); strings.Contains(l, "metadata") || strings.Contains(l, "images") || strings.Contains(l, "backups") || strings.Contains(l, "konoasset") {
			t.Errorf("%q was taken for an asset", a.Name)
		}
	}
	maid := byID[kaMaid]
	if maid == nil || len(st.Assets) != 6 || byName["Sailor Dress"] == nil || byName["Kikyo_v1.3"] == nil || byName["Hoodie"] == nil || byName["Lamp"] == nil || byName["CatEars"] == nil {
		t.Fatalf("assets: %v", keysOf(byName))
	}
	if !saysNothing(maid.Name) {
		t.Errorf("an id for a name is worth keeping? %q", maid.Name)
	}
	if k := byID[kaKikyo]; k.BoothID != "" || !strings.HasPrefix(k.Key, "name:") {
		t.Errorf("a Booth id read out of an item id: %q %q", k.Key, k.BoothID)
	}
	if got := KAFolders(); len(got) != 1 || core.PathKey(got[0]) != core.PathKey(home) {
		t.Errorf("found: %v", got)
	}
	if got := AEFolders(); len(got) != 0 {
		t.Errorf("taken for Avatar Explorer: %v", got)
	}

	pv, err := PreviewKA(st, home)
	if err != nil || pv.Version != 0 || pv.Items != 6 || pv.Matched != 5 || pv.Missing != 1 || pv.Outside != 0 || pv.AddRoot != "" {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	// what the player wrote here stays
	st.User[maid.Key] = &core.UserData{Notes: "我的备注", Tags: []string{"黑白"}}
	res, err := ApplyKA(st, home, false)
	if err != nil || res.Matched != 5 || res.Booth != 1 || res.Named != 5 || res.Skipped != 1 || res.Covers != 2 || res.Category != 2 || res.Bases != 2 || res.Tags != 3 {
		t.Fatalf("apply: %+v %v", res, err)
	}
	u := st.User[maid.Key]
	if u.Notes != "我的备注" || strings.Join(u.Tags, ",") != "黑白,女仆" || u.Name != "Maid Dress for Kikyo" || u.Category != "" {
		t.Errorf("maid dress: %+v", u) // (the scan already made it 衣服, from the package's name)
	}
	if strings.Join(u.Bases, ",") != "Kikyo,Selestia" {
		t.Errorf("the avatars it is for did not become base bodies: %v", u.Bases)
	}
	if u.Cover == "" || !core.UnderDir(u.Cover, filepath.Join(core.DataDir, "covers", "konoasset")) || !core.FileExists(u.Cover) {
		t.Errorf("cover: %q", u.Cover)
	}
	if v := BuildView(st, maid); v.Name != "Maid Dress for Kikyo" || v.Category != "衣服" {
		t.Errorf("the card: %q %q", v.Name, v.Category)
	}
	if k := st.User[byID[kaKikyo].Key]; k == nil || k.BoothURL != "https://booth.pm/ja/items/3681787" || k.Name != "【オリジナル3Dモデル】桔梗 Kikyo" || k.Category != "" {
		t.Errorf("the avatar: %+v", k) // (its category is what the scan found already)
	}
	// a category the words do not place: the item is still something worn, and the text is kept as a tag
	if h := st.User[byName["Hoodie"].Key]; h == nil || h.Category != "衣服" || strings.Join(h.Tags, ",") != "卫衣,謎カテゴリ" || !core.ContainsStr(h.Bases, "Kikyo") {
		t.Errorf("hoodie: %+v", h)
	}
	// ears: the category word is understood; a world object's category only as a tag, its category as scanned
	if e := st.User[byName["CatEars"].Key]; e == nil || e.Category != "配饰" || len(e.Tags) != 0 || e.Cover == "" || BuildView(st, byName["CatEars"]).Category != "配饰" {
		t.Errorf("ears: %+v", e)
	}
	if l := st.User[byName["Lamp"].Key]; l == nil || l.Category != "" || strings.Join(l.Tags, ",") != "ライト" || l.Name != "Lamp" {
		t.Errorf("lamp: %+v", l)
	}
	// a second time changes nothing
	if res, _ := ApplyKA(st, home, false); res.Matched != 0 {
		t.Errorf("second apply: %+v", res)
	}
	if _, err := ReadKA(root, ""); err == nil {
		t.Error("a folder that is not KonoAsset's was read")
	}
	if _, err := ReadAE(home); err == nil {
		t.Error("KonoAsset's folder read as Avatar Explorer's")
	}
}

func keysOf(m map[string]*core.Asset) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The data folder usually lives outside the asset folders (Documents\KonoAsset): it is found through
// preference.json and can be added; the dependencies go into the memo, in the window's language.
func TestKonoAssetOutside(t *testing.T) {
	root, home := t.TempDir(), filepath.Join(t.TempDir(), "KonoAsset")
	writeKA(t, home)
	testkit.WriteFile(t, filepath.Join(root, "Sailor Dress", "Sailor.unitypackage"), 100)
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("USERPROFILE", t.TempDir())
	pref := filepath.Join(local, "dev.konoasset.app", "preference.json")
	_ = os.MkdirAll(filepath.Dir(pref), 0755)
	if err := os.WriteFile(pref, []byte(`{"version":6,"data":{"dataDirPath":"`+strings.ReplaceAll(home, `\`, `\\`)+`","theme":"system"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if got := kaPreferredDir(pref); got != home {
		t.Errorf("preference: %q", got)
	}
	if err := os.WriteFile(pref, []byte(`{"dataDirPath":"`+strings.ReplaceAll(home, `\`, `\\`)+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if got := kaPreferredDir(pref); got != home {
		t.Errorf("an early preference: %q", got)
	}
	st, _ := scanFor(t, root)
	if got := KAFolders(); len(got) != 1 || core.PathKey(got[0]) != core.PathKey(home) {
		t.Fatalf("found: %v", got)
	}
	pv, err := PreviewKA(st, home)
	if err != nil || pv.Items != 6 || pv.Matched != 0 || pv.Outside != 5 || pv.InFolder != 5 || pv.Missing != 1 || core.PathKey(pv.AddRoot) != core.PathKey(home) {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	st.Settings.Lang = "ja"
	res, err := ApplyKA(st, home, true)
	if err != nil || res.AddRoot == "" || res.Skipped != 6 {
		t.Fatalf("apply: %+v %v", res, err)
	}
	RunFolderScan(st, &core.Task{}) // the folder that was added is scanned: only its items
	if len(st.Assets) != 6 {
		t.Fatalf("assets after the scan: %d", len(st.Assets))
	}
	res, err = ApplyKA(st, home, true)
	if err != nil || res.AddRoot != "" || res.Booth != 1 || res.Covers != 2 || res.Skipped != 1 {
		t.Fatalf("after the scan: %+v %v", res, err)
	}
	for _, a := range st.Assets {
		if a.RawName == kaMaid {
			if u := st.User[a.Key]; u == nil || u.Notes != "买的第一件\n依存アセット：【オリジナル3Dモデル】桔梗 Kikyo" {
				t.Errorf("maid dress: %+v", u)
			}
		}
	}
}

// A KonoAsset item folder the scan takes apart into several assets: the item's information goes to each of
// them (their names stay their own), and the preview counts the item as in the library.
func TestKonoAssetItemInParts(t *testing.T) {
	for name, lay := range map[string]struct {
		files  []string
		assets int
	}{
		"two unpacked folders":   {[]string{"MaidDress_Kikyo/MaidDress_Kikyo.unitypackage", "MaidDress_Selestia/MaidDress_Selestia.unitypackage"}, 2},
		"three unpacked folders": {[]string{"MaidDress_Kikyo/MaidDress_Kikyo.unitypackage", "MaidDress_Selestia/MaidDress_Selestia.unitypackage", "MaidDress_Moe/MaidDress_Moe.unitypackage"}, 3},
		"a folder per avatar":    {[]string{"MaidDress_v1.2/Kikyo/MaidDress_Kikyo.unitypackage", "MaidDress_v1.2/Selestia/MaidDress_Selestia.unitypackage"}, 1},
		"two zips kept":          {[]string{"MaidDress_Kikyo.zip", "MaidDress_Selestia.zip"}, 1},
	} {
		root := t.TempDir()
		home := filepath.Join(root, "KonoAsset")
		for _, f := range lay.files {
			testkit.WriteFile(t, filepath.Join(home, "data", kaMaid, filepath.FromSlash(f)), 100)
		}
		_ = os.MkdirAll(filepath.Join(home, "metadata"), 0755)
		meta := `{"version":3,"data":[{"id":"` + kaMaid + `","description":{"name":"Maid Dress","creator":"Shop","imageFilename":null,"tags":["女仆"],"memo":"m","boothItemId":null,"dependencies":[],"createdAt":1},"category":"衣装","supportedAvatars":[]}]}`
		if err := os.WriteFile(filepath.Join(home, "metadata", "avatarWearables.json"), []byte(meta), 0644); err != nil {
			t.Fatal(err)
		}
		st, byName := scanFor(t, root)
		if len(st.Assets) != lay.assets {
			t.Fatalf("%s: assets %v", name, keysOf(byName))
		}
		pv, err := PreviewKA(st, home)
		if err != nil || pv.Items != 1 || pv.Matched != 1 || pv.Missing != 0 || pv.Outside != 0 {
			t.Errorf("%s: preview %+v %v", name, pv, err)
		}
		res, err := ApplyKA(st, home, false)
		if err != nil || res.Matched != lay.assets || res.Skipped != 0 || res.Tags != lay.assets || res.Notes != lay.assets {
			t.Errorf("%s: apply %+v %v", name, res, err)
		}
		for _, a := range st.Assets {
			u := st.User[a.Key]
			if u == nil || u.Notes != "m" || !core.ContainsStr(u.Tags, "女仆") {
				t.Errorf("%s: %q got %+v", name, a.Name, u)
			} else if lay.assets > 1 && u.Name != "" {
				t.Errorf("%s: the parts all took the item's name: %q", name, u.Name)
			}
		}
	}
}
