package library

import (
	"path/filepath"
	"strings"
	"testing"

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
