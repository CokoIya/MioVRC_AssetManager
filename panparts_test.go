package main

import (
	"strings"
	"testing"
)

func TestAnalyzePan(t *testing.T) {
	defs := parseBases(defaultSettings().Bases)
	kids := []*PanFile{}
	for _, n := range []string{"AONAMI_chocolat_plum.zip", "AONAMI_manuka.zip", "AONAMI_materials.zip", "AONAMI_milfy_eku.zip",
		"AONAMI_PSD.zip", "AONAMI_rurune.zip", "AONAMI_shinano.zip", "AONAMI_sio.zip"} {
		kids = append(kids, &PanFile{Name: n, Size: 1000})
	}
	l := &PanListing{Title: "8099091", Files: []*PanFile{{Name: "8099091", Dir: true, Children: kids}}}
	info := analyzePan(l, defs)
	if info.name != "AONAMI" {
		t.Errorf("name %q", info.name)
	}
	if id := panBoothID(l, info); id != "8099091" {
		t.Errorf("booth id %q", id)
	}
	got := map[string]string{}
	for _, p := range info.parts {
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
	if info.parts[0].Path != "/8099091/AONAMI_chocolat_plum.zip" {
		t.Errorf("path %q", info.parts[0].Path)
	}
	// a single unrelated archive: no guessing
	l2 := &PanListing{Title: "Moon Dress", Files: []*PanFile{{Name: "Moon Dress.zip"}, {Name: "readme.txt"}}}
	i2 := analyzePan(l2, defs)
	if len(i2.bases) != 0 || i2.parts[1].Kind != "doc" {
		t.Errorf("%+v", i2.parts)
	}
}

func dir(name string, kids ...*PanFile) *PanFile {
	return &PanFile{Name: name, Dir: true, Children: kids}
}
func file(name string) *PanFile { return &PanFile{Name: name, Size: 100} }

func TestSplitPan(t *testing.T) {
	coll := &PanListing{Surl: "1c", Files: []*PanFile{dir("辉夜合集-咸鱼@邀月浮白",
		dir("8562330 辉夜 Kaguya模型", file("Kaguya_v1.06.unitypackage"), file("Kaguya_PSD.zip")),
		dir("头发", dir("Twintail_hair", file("twintail.unitypackage")), file("Bob_hair_v2.zip")),
		dir("妆容", file("Glitter makeup.zip"), file("Natural makeup.zip")),
		dir("衣服", dir("AONAMI", file("AONAMI_kaguya.zip"), file("AONAMI_PSD.zip")), dir("Moon Dress", file("MoonDress_Kaguya.zip"), file("Texture.zip"))),
		dir("饰品", file("Ribbon.unitypackage")),
		file("闲鱼@邀月浮白.jpg"),
	)}}
	items := splitPan(coll)
	var got []string
	for _, it := range items {
		got = append(got, it.Path+" ["+panItemCategory(it.Hints)+"]")
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
	one := &PanListing{Files: []*PanFile{dir("8099091", file("AONAMI_chocolat_plum.zip"), file("AONAMI_manuka.zip"), file("AONAMI_PSD.zip"))}}
	if n := len(splitPan(one)); n > 1 {
		t.Errorf("AONAMI split into %d", n)
	}
	loose := &PanListing{Files: []*PanFile{file("AONAMI_chocolat_plum.zip"), file("AONAMI_manuka.zip"), file("AONAMI_materials.zip")}}
	if n := len(splitPan(loose)); n > 1 {
		t.Errorf("loose AONAMI split into %d", n)
	}
	single := &PanListing{Files: []*PanFile{dir("Moon Dress", file("MoonDress.unitypackage"), file("Texture.zip"), file("readme.txt"))}}
	if n := len(splitPan(single)); n > 1 {
		t.Errorf("single product split into %d", n)
	}
	mixed := &PanListing{Files: []*PanFile{file("HairA.zip"), file("DressB.zip"), file("Necklace.zip")}}
	if n := len(splitPan(mixed)); n != 3 {
		t.Errorf("three products → %d", n)
	}
}
