package netdisk

import (
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/testkit"
)

func TestAnalyzePan(t *testing.T) {
	defs := naming.ParseBases(core.DefaultSettings().Bases)
	kids := []*core.PanFile{}
	for _, n := range []string{"AONAMI_chocolat_plum.zip", "AONAMI_manuka.zip", "AONAMI_materials.zip", "AONAMI_milfy_eku.zip",
		"AONAMI_PSD.zip", "AONAMI_rurune.zip", "AONAMI_shinano.zip", "AONAMI_sio.zip"} {
		kids = append(kids, &core.PanFile{Name: n, Size: 1000})
	}
	l := &core.PanListing{Title: "8099091", Files: []*core.PanFile{{Name: "8099091", Dir: true, Children: kids}}}
	info := AnalyzePan(l, defs)
	if info.Name != "AONAMI" {
		t.Errorf("name %q", info.Name)
	}
	if id := PanBoothID(l, info); id != "8099091" {
		t.Errorf("booth id %q", id)
	}
	got := map[string]string{}
	for _, p := range info.Parts {
		got[p.Name] = p.Kind + ":" + strings.Join(p.Bases, ",")
	}
	want := map[string]string{
		"AONAMI_chocolat_plum.zip": "variant:Plum,Chocolat", "AONAMI_manuka.zip": "variant:Manuka", "AONAMI_materials.zip": "material:",
		"AONAMI_milfy_eku.zip": "variant:Milfy,Eku", "AONAMI_PSD.zip": "psd:", "AONAMI_rurune.zip": "variant:Rurune",
		"AONAMI_shinano.zip": "variant:Shinano", "AONAMI_sio.zip": "variant:Sio",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %q want %q", k, got[k], w)
		}
	}
	if info.Parts[0].Path != "/8099091/AONAMI_chocolat_plum.zip" {
		t.Errorf("path %q", info.Parts[0].Path)
	}
	// a single unrelated archive: no guessing
	l2 := &core.PanListing{Title: "Moon Dress", Files: []*core.PanFile{{Name: "Moon Dress.zip"}, {Name: "readme.txt"}}}
	i2 := AnalyzePan(l2, defs)
	if len(i2.Bases) != 0 || i2.Parts[1].Kind != "doc" {
		t.Errorf("%+v", i2.Parts)
	}
}

func TestSplitPan(t *testing.T) {
	coll := &core.PanListing{Surl: "1c", Files: []*core.PanFile{testkit.PanDir("辉夜合集-咸鱼@邀月浮白",
		testkit.PanDir("8562330 辉夜 Kaguya模型", testkit.PanFile("Kaguya_v1.06.unitypackage"), testkit.PanFile("Kaguya_PSD.zip")),
		testkit.PanDir("头发", testkit.PanDir("Twintail_hair", testkit.PanFile("twintail.unitypackage")), testkit.PanFile("Bob_hair_v2.zip")),
		testkit.PanDir("妆容", testkit.PanFile("Glitter makeup.zip"), testkit.PanFile("Natural makeup.zip")),
		testkit.PanDir("衣服", testkit.PanDir("AONAMI", testkit.PanFile("AONAMI_kaguya.zip"), testkit.PanFile("AONAMI_PSD.zip")), testkit.PanDir("Moon Dress", testkit.PanFile("MoonDress_Kaguya.zip"), testkit.PanFile("Texture.zip"))),
		testkit.PanDir("饰品", testkit.PanFile("Ribbon.unitypackage")),
		testkit.PanFile("闲鱼@邀月浮白.jpg"),
	)}}
	items := SplitPan(coll)
	var got []string
	for _, it := range items {
		got = append(got, it.Path+" ["+PanItemCategory(it.Hints)+"]")
	}
	want := []string{
		"/辉夜合集-咸鱼@邀月浮白/8562330 辉夜 Kaguya模型 []",
		"/辉夜合集-咸鱼@邀月浮白/头发/Twintail_hair [头发]",
		"/辉夜合集-咸鱼@邀月浮白/头发/Bob_hair_v2.zip [头发]",
		"/辉夜合集-咸鱼@邀月浮白/妆容/Glitter makeup.zip [材质]",
		"/辉夜合集-咸鱼@邀月浮白/妆容/Natural makeup.zip [材质]",
		"/辉夜合集-咸鱼@邀月浮白/衣服/AONAMI [衣服]",
		"/辉夜合集-咸鱼@邀月浮白/衣服/Moon Dress [衣服]",
		"/辉夜合集-咸鱼@邀月浮白/饰品/Ribbon.unitypackage [配饰]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s", strings.Join(got, "\n"))
	}
	// one product with a download per base body: not split
	one := &core.PanListing{Files: []*core.PanFile{testkit.PanDir("8099091", testkit.PanFile("AONAMI_chocolat_plum.zip"), testkit.PanFile("AONAMI_manuka.zip"), testkit.PanFile("AONAMI_PSD.zip"))}}
	if n := len(SplitPan(one)); n > 1 {
		t.Errorf("AONAMI split into %d", n)
	}
	loose := &core.PanListing{Files: []*core.PanFile{testkit.PanFile("AONAMI_chocolat_plum.zip"), testkit.PanFile("AONAMI_manuka.zip"), testkit.PanFile("AONAMI_materials.zip")}}
	if n := len(SplitPan(loose)); n > 1 {
		t.Errorf("loose AONAMI split into %d", n)
	}
	single := &core.PanListing{Files: []*core.PanFile{testkit.PanDir("Moon Dress", testkit.PanFile("MoonDress.unitypackage"), testkit.PanFile("Texture.zip"), testkit.PanFile("readme.txt"))}}
	if n := len(SplitPan(single)); n > 1 {
		t.Errorf("single product split into %d", n)
	}
	mixed := &core.PanListing{Files: []*core.PanFile{testkit.PanFile("HairA.zip"), testkit.PanFile("DressB.zip"), testkit.PanFile("Necklace.zip")}}
	if n := len(SplitPan(mixed)); n != 3 {
		t.Errorf("three products → %d", n)
	}
}
