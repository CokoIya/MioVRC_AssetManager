package library

import (
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

func testStore() *core.Store {
	st := &core.Store{Settings: core.DefaultSettings()}
	return st
}

func TestStems(t *testing.T) {
	defs := naming.ParseBases(core.DefaultSettings().Bases)
	same := [][]string{
		{"HYPERTECH_EXO_FRAME_For_Milltina_v1.01", "HYPERTECH_EXO_FRAME_For_Milfy_v1.01", "HYPERTECH_EXO_FRAME_For_Chocolat_v1.01"},
		{"ventus_school_PSD", "ventus_school_Plum"},
		{"Crimson Rumor_Kaguya", "Crimson_Rumor_Texture"},
	}
	for _, g := range same {
		s0 := stemOf(g[0], defs, naming.ForBases(g[0], defs))
		for _, n := range g[1:] {
			if s := stemOf(n, defs, naming.ForBases(n, defs)); s != s0 || s == "" {
				t.Errorf("%q→%q vs %q→%q", g[0], s0, n, s)
			}
		}
	}
	for _, n := range []string{"Plum", "Plum_v1.0.1", "PSD", "Kaguya_PSD", "衣服"} {
		if s := stemOf(n, defs, nil); s != "" {
			t.Errorf("%q should have no stem, got %q", n, s)
		}
	}
}

func view(key, name, cat, booth, shop string, bases ...string) AssetView {
	v := AssetView{Key: key, Name: name, AutoName: name, RawName: name, Category: cat, BoothID: booth, Bases: bases, AutoBases: bases}
	if shop != "" {
		v.Booth = &core.BoothInfo{ID: booth, Shop: shop, Name: "商品 " + booth}
	}
	return v
}

func TestGroupViews(t *testing.T) {
	st := testStore()
	vs := []AssetView{
		view("a", "HYPERTECH_EXO_FRAME_For_Milltina_v1.01", "衣服", "111", "CLONE", "Milltina"),
		view("b", "HYPERTECH_EXO_FRAME_For_Milfy_v1.01", "衣服", "111", "CLONE", "Milfy"),
		view("c", "HYPERTECH_EXO_FRAME_For_Rurune_v1.01", "衣服", "", "", "Rurune"), // no booth yet: joins by name
		view("d", "ventus_school_PSD", "衣服", "222", "ventus"),
		view("e", "ventus_school_Plum", "衣服", "222", "ventus", "Plum"),
		view("f", "Plum_Gradation", "材质", "333", "shopA", "Plum"),
		view("g", "Kikyo_Gradation", "材质", "444", "shopB", "Kikyo"), // different products: stay apart
		view("h", "Plum", "素体", "", ""),
	}
	vs[3].PSD = true
	groupViews(st, vs)
	g := func(i int) string { return vs[i].Group }
	if g(0) == "" || g(0) != g(1) || g(0) != g(2) {
		t.Errorf("hypertech not grouped: %q %q %q", g(0), g(1), g(2))
	}
	if vs[0].GroupName != "HYPERTECH_EXO_FRAME" {
		t.Errorf("group name %q", vs[0].GroupName)
	}
	if g(3) == "" || g(3) != g(4) || vs[3].Variant != "PSD" || vs[4].Variant != "Plum" {
		t.Errorf("ventus: %q %q %q %q", g(3), g(4), vs[3].Variant, vs[4].Variant)
	}
	if g(5) != "" || g(6) != "" || g(7) != "" {
		t.Errorf("should stay single: %q %q %q", g(5), g(6), g(7))
	}
	if vs[1].Variant != "Milfy" {
		t.Errorf("variant %q", vs[1].Variant)
	}
}

func TestStyles(t *testing.T) {
	defs := styleDefs(core.DefaultSettings().Styles)
	v := AssetView{Category: "衣服", AutoName: "Moon Dress", Booth: &core.BoothInfo{Name: "【8アバター対応】セーラー服", Tags: []string{"制服", "かわいい"}, Adult: true}}
	got := strings.Join(autoStyles(&v, defs), ",")
	if got != "JK,H,可爱" {
		t.Errorf("styles %q", got)
	}
	v2 := AssetView{Category: "衣服", AutoName: "Type_H_hoodie"}
	if got := strings.Join(autoStyles(&v2, defs), ","); got != "休闲" {
		t.Errorf("one-letter style name must not match by itself: %q", got)
	}
	v3 := AssetView{Category: "头发", AutoName: "メイド hair"}
	if len(autoStyles(&v3, defs)) != 0 {
		t.Error("only outfits get styles")
	}
}

func TestGroupName(t *testing.T) {
	cases := map[string][]string{
		"ventus_school":       {"ventus_school_PSD", "ventus_school_Plum"},
		"HYPERTECH_EXO_FRAME": {"HYPERTECH_EXO_FRAME_For_Milltina_v1.01", "HYPERTECH_EXO_FRAME_For_Milfy_v1.01"},
		"Sheer_Veil_Dress":    {"Sheer_Veil_Dress", "Sheer_Veil_Dress_chocolat"},
		"Crimson Dress":       {"Plum_Crimson Dress", "Kikyo_Crimson Dress"},
	}
	for want, names := range cases {
		if got := groupName(names); got != want {
			t.Errorf("%v: got %q want %q", names, got, want)
		}
	}
}

// The group's name is cut out of a download's own name where it is in that name, not where it is in the
// lower-cased copy: letters whose lower case is longer ("Ⱥ" → "ⱥ") moved the place, up to a crash.
func TestVariantLabelLetterCase(t *testing.T) {
	for _, c := range []struct{ name, group, want string }{
		{"ȺȺȺȺȺȺ x", "X", "ȺȺȺȺȺȺ"},
		{"ȺȺȺȺ Dress Kaguya", "dress", "ȺȺȺȺ  Kaguya"},
		{"Summer DRESS Quest", "Summer Dress", "Quest"},
		{"İpek Dress black", "dress", "İpek  black"},
		{"Dress", "Dress", ""},
		{"Dress Long", "", "Dress Long"},
	} {
		v := AssetView{AutoName: c.name}
		if got := variantLabel(&v, c.group); got != c.want {
			t.Errorf("variantLabel(%q, %q) = %q, want %q", c.name, c.group, got, c.want)
		}
	}
	for _, c := range []struct {
		s, sub string
		at, n  int
	}{
		{"Summer DRESS v2", "dress", 7, 5}, {"ȺȺ ⱥx", "Ⱥx", 5, 4}, {"abc", "x", -1, 0}, {"abc", "", -1, 0}, {"ab", "abc", -1, 0}, {"衣服Dress", "dress", 6, 5},
	} {
		if at, n := indexFold(c.s, c.sub); at != c.at || n != c.n {
			t.Errorf("indexFold(%q, %q) = %d, %d", c.s, c.sub, at, n)
		}
	}
}
