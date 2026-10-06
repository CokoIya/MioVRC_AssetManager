package library

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/testkit"
)

// lay puts files under root: "a/b.unitypackage" a small file, "a/b/" a folder, and "x.zip<a.zip,b/c.txt" a
// zip that holds those entries.
func lay(t testing.TB, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if name, inside, ok := strings.Cut(p, "<"); ok {
			rawZip(t, filepath.Join(root, filepath.FromSlash(name)), true, strings.Split(inside, ",")...)
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// rawZip: a zip with these entries; flagged false: their names are stored without the mark that says UTF-8.
func rawZip(t testing.TB, file string, flagged bool, names ...string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, NonUTF8: !flagged, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("x"))
	}
	_ = zw.Close()
	_ = os.MkdirAll(filepath.Dir(file), 0755)
	if err := os.WriteFile(file, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

// products: a folder for each name, with its package in it.
func products(names ...string) (out []string) {
	for _, n := range names {
		out = append(out, n+"/"+filepath.Base(n)+".unitypackage")
	}
	return out
}

// madeAt: what an unpack that has just made these folders under root (from these archives, where given as
// "folder<archive") tells about it.
func madeAt(root string, rels ...string) map[string]bool {
	var done, from []string
	for _, r := range rels {
		dir, arc, kept := strings.Cut(r, "<")
		done = append(done, filepath.Join(root, filepath.FromSlash(dir)))
		if kept {
			from = append(from, filepath.Join(root, filepath.FromSlash(arc)))
		}
	}
	return Unpacked(done, from)
}

// bundleSays: what was found, as one line — the containers below top ("." is top itself), the number of
// products, and "packed" when some are still inside archives.
func bundleSays(top string, b BundleInfo) string {
	var cs []string
	for _, c := range b.Containers {
		rel, _ := filepath.Rel(top, c)
		cs = append(cs, filepath.ToSlash(rel))
	}
	out := fmt.Sprintf("%s | %d", strings.Join(cs, " "), b.Products)
	if b.Packed {
		out += " packed"
	}
	return out
}

var testBases = naming.ParseBases(core.DefaultSettings().Bases)

const noBundle = " | 0"

func TestScanBundle(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		made  []string // the folders an unpack has just made (nil: not known)
		want  string
	}{
		// ---- the player's report: the share's one archive "2", unpacked into the download folder ----
		{"the download: 2/ → categories → products, just unpacked",
			append(products("2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/头发/Twintail_hair", "2/头发/Bob_hair_v2", "2/Gothic Coat"),
				"2/说明.txt", "2/预览.png", "2/衣服/readme.txt", "2/衣服/Sailor Dress/readme.txt", "2/衣服/Sailor Dress/thumb.png", "2/衣服/Sailor Dress/Textures/skirt.png"),
			[]string{"2", "2/衣服", "2/头发", "2/Gothic Coat", "2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/头发/Twintail_hair", "2/头发/Bob_hair_v2"},
			". 2 2/头发 2/衣服 | 5"},
		{"… the products straight in 2/",
			append(products("2/Sailor Dress", "2/Maid Outfit"), "2/店铺说明.txt", "2/cover.jpg"),
			[]string{"2", "2/Sailor Dress", "2/Maid Outfit"}, ". 2 | 2"},
		{"… unpacked long ago: a level is told by four products, not by two — whose category is no level, but counted as the two",
			append(products("2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/衣服/Gothic Coat", "2/衣服/Night Gown", "2/头发/Twintail_hair", "2/头发/Bob_hair_v2", "2/Wolf Tail"), "2/说明.txt", "2/预览.png"),
			nil, ". 2 2/衣服 | 7"},
		{"… unpacked long ago, two products in each category: nothing tells it from one product",
			products("2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/头发/Twintail_hair", "2/头发/Bob_hair_v2"), nil, noBundle},
		{"… a category archive with one product in it counts as that product",
			products("2/衣服/Sailor Dress", "2/头发/Twintail_hair", "2/饰品/Cat Ears"),
			[]string{"2", "2/衣服", "2/头发", "2/饰品", "2/衣服/Sailor Dress", "2/头发/Twintail_hair", "2/饰品/Cat Ears"}, ". 2 | 3"},
		{"… numbered archives, a product with its PSD pack in each",
			[]string{"2/1/Sailor Dress.unitypackage", "2/1/PSD.zip", "2/1/readme.txt", "2/2/Maid Outfit.unitypackage", "2/3/Gothic Coat/Gothic Coat.unitypackage"},
			[]string{"2", "2/1", "2/2", "2/3"}, ". 2 | 3"},
		{"… numbered folders that are the versions of one product",
			[]string{"Dress/1.0/Dress.unitypackage", "Dress/1.1/Dress.unitypackage"}, []string{"Dress", "Dress/1.0", "Dress/1.1"}, noBundle},
		{"two levels of collections",
			products("春季合集/衣服/Sailor Dress", "春季合集/衣服/Maid Outfit", "春季合集/头发/Twintail_hair", "春季合集/头发/Bob_hair_v2", "夏季合集/Bikini Set", "夏季合集/Straw Hat"),
			[]string{"春季合集", "春季合集/衣服", "春季合集/头发", "夏季合集", "春季合集/衣服/Sailor Dress", "春季合集/衣服/Maid Outfit", "春季合集/头发/Twintail_hair", "春季合集/头发/Bob_hair_v2", "夏季合集/Bikini Set", "夏季合集/Straw Hat"},
			". 夏季合集 春季合集 春季合集/头发 春季合集/衣服 | 6"},
		{"a wrapper chain", products("a/b/c/Sailor Dress", "a/b/c/Maid Outfit", "a/b/c/Gothic Coat", "a/b/c/Night Gown"), nil, ". a a/b a/b/c | 4"},
		{"its own name says collection", products("辉夜合集/Sailor Dress", "辉夜合集/Maid Outfit"), nil, ". 辉夜合集 | 2"},
		{"… but a name alone is not enough", products("辉夜合集/Sailor Dress"), nil, noBundle},
		{"the collection is named after the base body its products are for",
			products("Kaguya/Kaguya_Sailor Dress", "Kaguya/Kaguya_Maid Outfit"), []string{"Kaguya", "Kaguya/Kaguya_Sailor Dress", "Kaguya/Kaguya_Maid Outfit"}, ". Kaguya | 2"},

		// ---- one product: never a collection ----
		{"one product: an archive for each base body, and its PSD pack", []string{"Dress_Kaguya.zip", "Dress_Plum.zip", "PSD.zip", "readme.txt"}, nil, noBundle},
		{"… unpacked", products("Dress_Kaguya", "Dress_Plum", "PSD/Dress_PSD"), []string{"Dress_Kaguya", "Dress_Plum", "PSD"}, noBundle},
		{"one product: a folder for each base body", products("Kaguya/Dress", "Plum/Dress", "Plum_v2/Dress", "for Shinano/Dress"), []string{"Kaguya", "Plum", "Plum_v2", "for Shinano"}, noBundle},
		{"one product: the base body in front of its name", products("Kaguya_Dress", "Plum_Dress", "Manuka_Dress", "Shinano_Dress"), []string{"Kaguya_Dress", "Plum_Dress", "Manuka_Dress", "Shinano_Dress"}, noBundle},
		{"one product: an archive for each format", []string{"Unity.zip", "VRM.zip", "FBX.zip", "PC版.zip", "Quest対応.zip", "VRChat用.zip", "PhysBone.zip", "DynamicBone_ver.zip", "Blender.zip"}, nil, noBundle},
		{"one product: Textures, Prefab and PSD folders",
			[]string{"Dress.unitypackage", "Textures/tex.zip", "Prefab/Dress.unitypackage", "PSD/Dress_PSD.zip", "Textures_v2/tex2.zip", "FBX 2.0/fbx.zip", "特典/Bonus_Ribbon.unitypackage", "Manual/guide.zip", "素材/tools.unitypackage"},
			[]string{"Textures", "Prefab", "PSD", "Textures_v2", "FBX 2.0", "特典", "Manual", "素材"}, noBundle},
		{"one product: four shaders in its Shaders folder, four bonus packs in 特典, three packages for Unity",
			[]string{"Avatar.unitypackage", "Shaders/lilToon.unitypackage", "Shaders/Poiyomi.unitypackage", "Shaders/UTS2.unitypackage", "Shaders/Arktoon.unitypackage",
				"特典/Ribbon.zip", "特典/Socks.zip", "特典/Glasses.zip", "特典/Choker.zip", "Unity/Core.unitypackage", "Unity/Physics.unitypackage", "Unity/Menu.unitypackage"}, nil, noBundle},
		{"… the same out of its archive, the packs in it unpacked too", append(products("2/Textures/Red Set", "2/Textures/Blue Set"), "2/Avatar.unitypackage"), []string{"2", "2/Textures/Red Set", "2/Textures/Blue Set"}, noBundle},
		{"one product: archives named after its folder", []string{"MaidDress/Red MaidDress.zip", "MaidDress/Blue MaidDress.zip", "MaidDress/Black MaidDress.zip", "MaidDress/White MaidDress.zip"}, nil, noBundle},
		{"one product: a model lies loose among its packages", append(products("Sailor Dress", "Maid Outfit", "Gothic Coat", "Night Gown"), "body.fbx"), nil, noBundle},
		{"one product: its folder is named by a Booth item number (one purchase), whatever was unpacked into it",
			[]string{"8562330 Full Set/Sailor Dress.zip", "8562330 Full Set/Twintail.zip", "8562330 Full Set/Cat Ears.zip", "8562330 Full Set/Wolf Tail.zip"}, []string{"8562330 Full Set"}, noBundle},
		{"one product: it and its texture sources and a bonus, out of one archive", []string{"2/Sailor Dress.zip", "2/改変用素材.zip", "2/おまけ衣装.zip", "2/Kaguya.zip"}, []string{"2"}, noBundle},
		{"one product: the thing itself and what comes with it", []string{"本体.zip", "特典.zip", "贴图.zip", "说明书.zip"}, nil, noBundle},
		{"three unrelated packages: not enough to tell", []string{"Sailor Dress.unitypackage", "Twintail.unitypackage", "Cat Ears.unitypackage"}, nil, noBundle},
		{"… with a PSD pack, a folder named by a base body and a texture pack: still three", []string{"Sailor Dress.unitypackage", "Twintail.unitypackage", "Cat Ears.unitypackage", "PSD.zip", "Kaguya/x.unitypackage", "Twintail Textures.zip"}, nil, noBundle},
		{"… two of them in two versions each: still three", products("Sailor Dress_v1", "Sailor Dress_v2", "Twin Tail_v1", "Twin Tail_v2", "Cat Ears"), nil, noBundle},
		{"four unrelated packages: a pile of products", []string{"Sailor Dress.unitypackage", "Twintail.unitypackage", "Cat Ears.unitypackage", "Wolf Tail.unitypackage"}, nil, ". | 4"},
		{"four packages of one product", []string{"Sailor Dress_A.unitypackage", "Sailor Dress_B.unitypackage", "Sailor Dress_C.unitypackage", "Sailor Dress Quest.unitypackage"}, nil, noBundle},
		{"two products in two versions each are two products, not four",
			products("FaceTracking_1_0_1_for_Kaguya", "FaceTracking_1_1_1_for_Kaguya", "TriturboFramework-v1_0_20", "TriturboFramework-v1_0_22"), nil, noBundle},
		{"… four products, one of them in two versions", products("Sailor Dress_v1", "Sailor Dress_v2", "Twintail", "Cat Ears", "Wolf Tail"), nil, ". | 5"},

		// ---- the same base body in front of different products (archives out of the archive "2", as below) ----
		{"products for one base body, named by it", []string{"2/【Kaguya】Sailor Dress.zip", "2/【Kaguya】Twin Hair.zip", "2/Kaguya Night Gown.zip"}, []string{"2"}, ". 2 | 3"},
		{"… with nothing but the base body shared", []string{"2/Kaguya A.zip", "2/Kaguya B.zip", "2/Kaguya C.zip"}, []string{"2"}, ". 2 | 3"},
		{"a number in front of all of them is no product name", []string{"2/2024.05 Sailor Dress.zip", "2/2024.05 Twin Hair.zip"}, []string{"2"}, ". 2 | 2"},

		// ---- still packed ----
		{"a packed zip of zips", []string{"2.zip<Sailor Dress.zip,Twintail.zip,Cat Ears.rar,说明.txt,PSD.zip", "预览.png"}, nil, " | 3 packed"},
		{"… with a folder that wraps everything in it", []string{"2.zip<合集/Sailor Dress.zip,合集/Twintail.zip,合集/说明.txt,__MACOSX/合集/._x"}, nil, " | 2 packed"},
		{"… in a folder of its own, below the download folder", []string{"2/2.zip<Sailor Dress.zip,Twintail.zip"}, nil, " | 2 packed"},
		{"a packed zip of one product's archives", []string{"Dress.zip<Dress_Kaguya.zip,Dress_Plum.zip,PSD.zip"}, nil, noBundle},
		{"a packed zip with a model at its top", []string{"Dress.zip<Outfit.zip,Shoes.zip,body.fbx"}, nil, noBundle},
		{"a packed zip of one archive", []string{"Dress.zip<Inner.zip"}, nil, noBundle},
		{"a packed rar: one product", []string{"2.rar"}, nil, noBundle},
		{"two packed archives that came out of an archive: two products", []string{"2/Sailor Dress.rar", "2/Twintail.7z", "2/说明.txt"}, []string{"2"}, ". 2 | 2"},
		{"… in a folder below the one that came out of it", []string{"2/新建文件夹/Sailor Dress.rar", "2/新建文件夹/Twintail.7z"}, []string{"2"}, ". 2 2/新建文件夹 | 2"},
		{"… side by side in a folder that was there: a product and what it needs come like that too", []string{"Setup Tool.rar", "Shader It Needs.7z", "说明.txt"}, nil, noBundle},
		{"… the same just unpacked there (an import of that product)", products("Setup Tool", "Shader It Needs"), []string{"Setup Tool", "Shader It Needs"}, noBundle},
		{"… three of them, long there", []string{"Blink Pack.zip", "Face Animation.zip", "Hand Signs.zip"}, nil, noBundle},
		{"volumes of one archive are one entry", []string{"2/Sailor Dress.part1.rar", "2/Sailor Dress.part2.rar", "2/Twintail.zip.001", "2/Twintail.zip.002"}, []string{"2"}, ". 2 | 2"},
		{"… of one archive only", []string{"2/Sailor Dress.part1.rar", "2/Sailor Dress.part2.rar", "2/Sailor Dress.part3.rar"}, []string{"2"}, noBundle},
		{"archives that did not unpack: each is one product", append(products("2/Sailor Dress"), "2/Twintail.rar", "2/Cat Ears.7z"), []string{"2", "2/Sailor Dress"}, ". 2 | 3"},
		{"a product among the unpacked ones holds a collection that is still packed",
			append(products("2/Sailor Dress", "2/Maid Outfit"), "2/More/more.zip<Twintail.zip,Cat Ears.zip"), []string{"2", "2/Sailor Dress", "2/Maid Outfit", "2/More"}, ". 2 | 4 packed"},

		// ---- archives kept beside what they were unpacked to (保留压缩包) ----
		{"kept archives, each with its folder: two products out of the archive",
			append(products("2/Sailor Dress", "2/Twintail"), "2.zip", "2/Sailor Dress.zip", "2/Twintail.zip"), []string{"2<2.zip", "2/Sailor Dress<2/Sailor Dress.zip", "2/Twintail<2/Twintail.zip"}, ". 2 | 2"},
		{"… long there, in a folder of their own: a tool and what it needs, unpacked in place", append(products("Setup Tool", "Shader It Needs"), "Setup Tool.zip", "Shader It Needs.zip"), nil, noBundle},
		{"a kept archive and the folder it made, named otherwise: one product", append(products("2/Sailor"), "2/水手服.rar"), []string{"2", "2/Sailor<2/水手服.rar"}, noBundle},
		{"… two of them: two products, not four", append(products("2/Sailor", "2/Twin"), "2/水手服.rar", "2/双马尾.rar"), []string{"2", "2/Sailor<2/水手服.rar", "2/Twin<2/双马尾.rar"}, ". 2 | 2"},

		// ---- bounds ----
		{"six levels down is looked at", products("1/2/3/4/5/6/Sailor Dress", "1/2/3/4/5/6/Maid Outfit", "1/2/3/4/5/6/Gothic Coat", "1/2/3/4/5/6/Night Gown"), nil, ". 1 1/2 1/2/3 1/2/3/4 1/2/3/4/5 1/2/3/4/5/6 | 4"},
		{"seven levels down is not", products("1/2/3/4/5/6/7/Sailor Dress", "1/2/3/4/5/6/7/Maid Outfit", "1/2/3/4/5/6/7/Gothic Coat", "1/2/3/4/5/6/7/Night Gown"), nil, noBundle},
		{"a package four folders down does not make its folder a product", []string{"Sailor Dress/a/b/c/x.unitypackage", "Maid Outfit/a/b/c/x.unitypackage", "Gothic Coat/a/b/c/x.unitypackage", "Night Gown/a/b/c/x.unitypackage"}, nil, noBundle},
		{"… three folders down does", []string{"Sailor Dress/a/b/x.unitypackage", "Maid Outfit/a/b/x.unitypackage", "Gothic Coat/a/b/x.unitypackage", "Night Gown/a/b/x.unitypackage"}, nil, ". | 4"},
		{"what is not an asset is not looked at", append(products("Sailor Dress", "Maid Outfit", "Gothic Coat", ".git/Night Gown", "__MACOSX/Wolf Tail", ".Cat Ears.extracting/Cat Ears", "node_modules/Twintail"), "Bob Hair.unitypackage.meta", "x.meta"), nil, noBundle},
		{"a Unity project among the products is none of them", append(products("Sailor Dress", "Maid Outfit", "Gothic Coat", "MyAvatar/Assets/x", "MyAvatar/ProjectSettings/y"), "MyAvatar/Export.unitypackage"), nil, noBundle},
	}
	for _, c := range cases {
		top := t.TempDir()
		lay(t, top, c.files...)
		var made map[string]bool
		if c.made != nil {
			made = madeAt(top, c.made...)
		}
		if got := bundleSays(top, ScanBundle(top, made, testBases, nil)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A zip is looked into as it is: by itself, and with names stored without the mark that says UTF-8.
func TestScanBundlePackedZip(t *testing.T) {
	top := t.TempDir()
	lay(t, top, "2.zip<Sailor Dress.zip,Twintail.zip,Cat Ears.zip")
	if got := bundleSays(top, ScanBundle(filepath.Join(top, "2.zip"), nil, testBases, nil)); got != " | 3 packed" {
		t.Errorf("the zip itself: %q", got)
	}
	// kept when it was unpacked (保留压缩包), its folder beside it — named as the zip, or as the one folder
	// in it: nothing is packed any more, what it holds has its cards there
	lay(t, top, "kept/2.zip<Sailor Dress.zip,Twintail.zip", "kept/2/x.txt", "kept/3.zip<Pack/Sailor Dress.zip,Pack/Twintail.zip")
	if got := bundleSays(top, ScanBundle(filepath.Join(top, "kept", "2.zip"), nil, testBases, nil)); got != noBundle {
		t.Errorf("a zip with its folder beside it: %q", got)
	}
	if got := bundleSays(top, ScanBundle(filepath.Join(top, "kept", "3.zip"), nil, testBases, nil)); got != " | 2 packed" {
		t.Errorf("a zip that wraps its archives in a folder: %q", got)
	}
	lay(t, top, "kept/Pack/x.txt")
	if got := bundleSays(top, ScanBundle(filepath.Join(top, "kept", "3.zip"), nil, testBases, nil)); got != noBundle {
		t.Errorf("… with that folder beside it: %q", got)
	}
	// UTF-8 names without the mark (zips made on macOS)
	rawZip(t, filepath.Join(top, "mac", "合集.zip"), false, "水手服套装.zip", "双马尾假发.zip", "说明.txt")
	if got := bundleSays(top, ScanBundle(filepath.Join(top, "mac"), nil, testBases, nil)); got != " | 2 packed" {
		t.Errorf("UTF-8 names without the mark: %q", got)
	}
	// names in a code page this system cannot read (GBK bytes, outside Windows): not looked into, one product
	rawZip(t, filepath.Join(top, "gbk", "2.zip"), false, "\xcb\xae\xca\xd6\xb7\xfe.zip", "\xcb\xab\xc2\xed\xce\xb2.zip")
	if got := bundleSays(top, ScanBundle(filepath.Join(top, "gbk"), nil, testBases, nil)); got != noBundle {
		t.Errorf("names that cannot be read: %q", got)
	}
	// not a zip at all, and a file that is no archive
	lay(t, top, "bad/2.zip", "note/2.txt")
	for _, p := range []string{"bad", "bad/2.zip", "note/2.txt", "nothing here"} {
		if got := bundleSays(top, ScanBundle(filepath.Join(top, filepath.FromSlash(p)), nil, testBases, nil)); got != noBundle {
			t.Errorf("%s: %q", p, got)
		}
	}
}

// What the player decided about a folder is kept: one kept as an asset is never a level of a collection,
// and nothing is looked for in what the scan is told to skip.
func TestScanBundleOverrides(t *testing.T) {
	top := t.TempDir()
	lay(t, top, products("2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/Gothic Coat", "2/Night Gown", "2/Wolf Tail")...)
	made := madeAt(top, "2", "2/衣服", "2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/Gothic Coat", "2/Night Gown", "2/Wolf Tail")
	key := func(rel string) string { return core.PathKey(filepath.Join(top, filepath.FromSlash(rel))) }
	for _, c := range []struct {
		name string
		ov   map[string]string
		want string
	}{
		{"none", nil, ". 2 2/衣服 | 5"},
		{"the top is kept as one asset", map[string]string{key("."): "asset"}, noBundle},
		{"the top is skipped", map[string]string{key("."): "ignore"}, noBundle},
		{"a level is kept as one asset: one entry, and its name says no product", map[string]string{key("2/衣服"): "asset"}, ". 2 | 3"},
		{"a product is kept as one asset: still a product", map[string]string{key("2/Gothic Coat"): "asset"}, ". 2 2/衣服 | 5"},
		{"products are skipped", map[string]string{key("2/Gothic Coat"): "ignore", key("2/Night Gown"): "ignore"}, ". 2 2/衣服 | 3"},
		{"the collection inside is skipped", map[string]string{key("2"): "ignore"}, noBundle},
		{"marks of the program and splits of the player change nothing", map[string]string{key("2"): "bundle", key("2/衣服"): "split"}, ". 2 2/衣服 | 5"},
	} {
		if got := bundleSays(top, ScanBundle(top, made, testBases, c.ov)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// a folder named as a product names its own ("Shaders") is not looked into — unless it is scanned as a
	// container already: then it is a level, and so is what it lies in
	own := t.TempDir()
	lay(t, own, append(products("Avatar/Shaders/lilToon", "Avatar/Shaders/Poiyomi", "Avatar/Shaders/UTS2", "Avatar/Shaders/Arktoon"), "Avatar/Avatar.unitypackage")...)
	shaders := core.PathKey(filepath.Join(own, "Avatar", "Shaders"))
	for mode, want := range map[string]string{"": noBundle, "split": ". Avatar Avatar/Shaders | 4", "bundle": ". Avatar Avatar/Shaders | 4"} {
		if got := bundleSays(own, ScanBundle(own, nil, testBases, map[string]string{shaders: mode})); got != want {
			t.Errorf("a product's Shaders folder marked %q: got %q, want %q", mode, got, want)
		}
	}
	// a folder that is scanned as a container already is a level of a collection whatever it looks like: two
	// products, unpacked long ago — and everything in it that holds a package is a card
	two := t.TempDir()
	lay(t, two, append(products("Pack/Sailor Dress", "Pack/Maid Outfit"), "Pack/PSD/Dress_PSD.zip", "Pack/readme/x.txt")...)
	pack := core.PathKey(filepath.Join(two, "Pack"))
	for mode, want := range map[string]string{"": noBundle, "bundle": ". Pack | 3", "split": ". Pack | 3", "asset": noBundle} {
		if got := bundleSays(two, ScanBundle(two, nil, testBases, map[string]string{pack: mode})); got != want {
			t.Errorf("a folder of two products marked %q: got %q, want %q", mode, got, want)
		}
	}
}

// The work is bounded: a folder is read up to a number of entries, and a tree up to a number of folders.
func TestScanBundleBounds(t *testing.T) {
	word := func(i int) string { // five letters, different each time and no word of any list
		b := []byte("qaaaa")
		for k := 4; k > 0; k, i = k-1, i/26 {
			b[k] += byte(i % 26)
		}
		return string(b)
	}
	top := t.TempDir()
	for i := 0; i < bundleEntries+60; i++ {
		lay(t, top, "pile/"+word(i)+".zip")
	}
	if b := ScanBundle(filepath.Join(top, "pile"), nil, testBases, nil); b.Products != bundleEntries || len(b.Containers) != 1 {
		t.Errorf("a folder of %d archives: %d products, containers %v", bundleEntries+60, b.Products, b.Containers)
	}
	// more folders than are read for one answer: it ends, and says what it found until then
	var files []string
	for c := 0; c < 34; c++ {
		for p := 0; p < 100; p++ {
			files = append(files, fmt.Sprintf("big/%s/%s/x.unitypackage", word(c), word(1000+c*100+p)))
		}
	}
	lay(t, top, files...)
	s := &bundleScan{defs: testBases, lists: map[string]*bundleList{}}
	f := s.scan(filepath.Join(top, "big"), 0, false, false)
	if s.reads != bundleReads || !f.container || f.products < 2000 || f.products >= 3400 {
		t.Errorf("a tree of 3400 product folders: %d folders read, container %v, %d products", s.reads, f.container, f.products)
	}
}

func TestMarkBundles(t *testing.T) {
	st := testkit.NewStore(t)
	root := t.TempDir()
	local := filepath.Join(root, "2")
	lay(t, local, products("2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/头发/Twintail_hair", "2/头发/Bob_hair_v2", "2/Gothic Coat")...)
	made := madeAt(local, "2", "2/衣服", "2/头发", "2/Gothic Coat", "2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/头发/Twintail_hair", "2/头发/Bob_hair_v2")
	key := func(rel string) string { return core.PathKey(filepath.Join(local, filepath.FromSlash(rel))) }
	marks := func(s *core.Store) string {
		s.Mu.RLock()
		defer s.Mu.RUnlock()
		var out []string
		for _, k := range core.SortedKeys(s.Overrides) {
			rel, _ := filepath.Rel(core.PathKey(local), k)
			out = append(out, filepath.ToSlash(rel)+"="+s.Overrides[k])
		}
		return strings.Join(out, " ")
	}
	st.Overrides[key("2/头发")] = "split" // the player's own: stays
	b := MarkBundles(st, local, made)
	if got := bundleSays(local, b); got != ". 2 2/头发 2/衣服 | 5" {
		t.Fatalf("found: %q", got)
	}
	const want = ".=bundle 2=bundle 2/头发=split 2/衣服=bundle"
	if got := marks(st); got != want {
		t.Errorf("marks: %q, want %q", got, want)
	}
	if got := marks(core.LoadStore(st.Path)); got != want {
		t.Errorf("marks on disk: %q", got)
	}
	// asked again (more of the share downloaded into the folder, an import of it): the same answer, and without
	// the unpack's word too — the marks say what it is
	for _, m := range []map[string]bool{made, nil} {
		if b := MarkBundles(st, local, m); b.Products != 5 || len(b.Containers) != 4 || marks(st) != want {
			t.Errorf("asked again: %+v, marks %q", b, marks(st))
		}
	}
	// one 恢复 on the outermost mark: the marks below go with it, the player's own stays
	st.Mu.Lock()
	delete(st.Overrides, key("."))
	ForgetBundles(st, local)
	st.Mu.Unlock()
	if got := marks(st); got != "2/头发=split" {
		t.Errorf("after 恢复: %q", got)
	}
	// a folder the player keeps as one asset, or one above it: nothing is marked in it
	for _, kept := range []string{local, root} {
		st.Mu.Lock()
		st.Overrides = map[string]string{core.PathKey(kept): "asset"}
		st.Mu.Unlock()
		if b := MarkBundles(st, local, made); b.Products != 0 || len(st.Overrides) != 1 {
			t.Errorf("under %s kept as one asset: %+v, marks %v", kept, b, st.Overrides)
		}
	}
	// no collection: nothing marked, nothing found
	st.Overrides = map[string]string{}
	one := filepath.Join(root, "Dress")
	lay(t, one, "Dress_Kaguya.zip", "Dress_Plum.zip", "PSD.zip")
	if b := MarkBundles(st, one, nil); b.Products != 0 || len(b.Containers) != 0 || len(st.Overrides) != 0 {
		t.Errorf("one product: %+v, marks %v", b, st.Overrides)
	}
	// a collection that is still packed: told, but there is no folder level to mark
	packed := filepath.Join(root, "packed")
	lay(t, packed, "2.zip<Sailor Dress.zip,Twintail.zip")
	if b := MarkBundles(st, packed, map[string]bool{}); b.Products != 2 || !b.Packed || len(b.Containers) != 0 || len(st.Overrides) != 0 {
		t.Errorf("still packed: %+v, marks %v", b, st.Overrides)
	}
}

// The look at a card for a collection, next to the walk through its files the scan does anyway: what a scan of
// a library of product folders pays for it (each: two packages, a zip, a texture folder, a readme).
func BenchmarkBundleLook(b *testing.B) {
	root := b.TempDir()
	const n = 3000
	dirs := make([]string, n)
	for i := range dirs {
		dirs[i] = filepath.Join(root, fmt.Sprintf("Outfit %04d", i))
		lay(b, dirs[i], "Outfit_Kaguya.unitypackage", "Outfit_Plum.unitypackage", "PSD.zip", "Textures/a.png", "Textures/b.png", "readme.txt")
	}
	b.Run("walk", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, d := range dirs {
				walkAsset(d)
			}
		}
	})
	b.Run("bundle", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, d := range dirs {
				if ScanBundle(d, nil, testBases, nil).Products != 0 {
					b.Fatal("a product taken for a collection")
				}
			}
		}
	})
}

// The cards: one that holds a collection says so (how many products, and whether they have to be unpacked
// first) until the player keeps it as one asset; split, every product's card is the netdisk card's.
func TestBundleCards(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "2")
	lay(t, local, append(products("2/衣服/Sailor Dress", "2/衣服/Maid Outfit", "2/衣服/Night Gown", "2/衣服/Summer Bikini", "2/Gothic Coat"), "2/说明.txt")...)
	lay(t, root, append(products("Wolf Tail"), "Pack.zip<Sailor Dress.zip,Twintail.zip", "Kept/3.zip<Sailor Dress.zip,Twintail.zip,Cat Ears.zip", "Kept/说明.txt",
		"Dress.zip<Dress_Kaguya.zip,Dress_Plum.zip", "Coat/Coat_Kaguya.unitypackage", "Coat/Coat_Plum.unitypackage", "Coat/PSD.zip")...)
	st, _ := scanFor(t, root)
	st.User["pan:1Share"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1Share", Downloaded: local}
	views := func() map[string]AssetView {
		st.Mu.RLock()
		defer st.Mu.RUnlock()
		out := map[string]AssetView{}
		for _, v := range AllViews(st) {
			if !v.PanOnly {
				out[v.Name] = v
			}
		}
		return out
	}
	vs := views()
	for name, want := range map[string]string{"2": "5 false", "Pack": "2 true", "Kept": "3 true", "Dress": "0 false", "Coat": "0 false", "Wolf Tail": "0 false"} {
		v, ok := vs[name]
		if got := fmt.Sprint(v.Bundle, v.BundlePacked); !ok || got != want {
			t.Errorf("%s: a collection of %s, want %s (cards: %v)", name, got, want, core.SortedKeys(vs))
		}
	}
	if vs["2"].FromPan != "pan:1Share" || vs["Wolf Tail"].FromPan != "" {
		t.Errorf("the download's card: from %q; the one beside it: from %q", vs["2"].FromPan, vs["Wolf Tail"].FromPan)
	}
	b, _ := json.Marshal(vs["2"])
	if !strings.Contains(string(b), `"bundle":5`) || strings.Contains(string(b), "bundlePacked") {
		t.Errorf("as the window gets it: %s", b)
	}
	if b, _ := json.Marshal(vs["Wolf Tail"]); strings.Contains(string(b), "bundle") {
		t.Errorf("a product's card carries the mark: %s", b)
	}
	// kept as one asset: said at once, and by the scan that follows
	st.Mu.Lock()
	st.Overrides[core.PathKey(local)] = "asset"
	st.Overrides[core.PathKey(filepath.Join(root, "Pack.zip"))] = "asset"
	st.Mu.Unlock()
	if vs = views(); vs["2"].Bundle != 0 || vs["Pack"].Bundle != 0 || vs["Pack"].BundlePacked || vs["Kept"].Bundle != 3 {
		t.Errorf("kept as one asset: %d, %d; the other: %d", vs["2"].Bundle, vs["Pack"].Bundle, vs["Kept"].Bundle)
	}
	RunFolderScan(st, &core.Task{})
	for _, a := range st.Assets {
		if want := map[string]int{"Kept": 3}[a.Name]; a.Bundle != want {
			t.Errorf("after the rescan %s is a collection of %d, want %d", a.Name, a.Bundle, want)
		}
	}
	// split: the cards of its products are the download's
	st.Mu.Lock()
	delete(st.Overrides, core.PathKey(local))
	st.Mu.Unlock()
	if b := MarkBundles(st, local, nil); b.Products != 5 {
		t.Fatalf("marked: %+v", b)
	}
	RunFolderScan(st, &core.Task{})
	vs = views()
	for _, name := range []string{"Sailor Dress", "Maid Outfit", "Night Gown", "Summer Bikini", "Gothic Coat"} {
		if v, ok := vs[name]; !ok || v.FromPan != "pan:1Share" || v.Bundle != 0 || v.User.ShareURL == "" {
			t.Errorf("%s: there %v, from %q, a collection of %d", name, ok, v.FromPan, v.Bundle)
		}
	}
	if _, whole := vs["2"]; whole || vs["Wolf Tail"].FromPan != "" || len(vs) != 10 {
		t.Errorf("cards after the split: %v", core.SortedKeys(vs))
	}
}
