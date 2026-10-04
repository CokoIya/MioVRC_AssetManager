package ai

import (
	"reflect"
	"strings"
	"testing"
)

// The menu words of the three languages: a template means the same in each, a plan is written in the
// player's language, and what an avatar's menu already calls a thing is kept.
func TestMenuWords(t *testing.T) {
	seen := map[string]bool{}
	for _, w := range menuWords {
		for i, s := range w {
			if s == "" {
				t.Errorf("%v: no word for language %d", w, i)
			}
		}
		if seen[w[0]] {
			t.Errorf("%s is in the table twice", w[0])
		}
		seen[w[0]] = true
	}
	for zh := range kindCategory {
		if menuWord("en", zh) == zh {
			t.Errorf("category %s has no English word", zh)
		}
	}
	for zh := range partRank {
		if menuWord("en", zh) == zh {
			t.Errorf("part %s has no English word", zh)
		}
	}
	if menuWord("", "衣服") != "衣服" || menuWord("en", "衣服") != "Outfits" || menuWord("ja", "衣服") != "衣装" || menuWord("en", "水手服") != "水手服" {
		t.Error("menuWord")
	}
	if !sameMenuWord("衣服", "Outfits") || !sameMenuWord("衣装", "Outfits") || sameMenuWord("衣服", "头发") || sameMenuWord("穿上", "戴上") || !sameMenuWord("x", "x") {
		t.Error("sameMenuWord")
	}
	if langNote("") != "" || !strings.Contains(langNote("en"), "English") || !strings.Contains(langNote("ja"), "衣服=衣装") {
		t.Error("langNote")
	}
}

func TestHierarchyLanguages(t *testing.T) {
	if hierarchyIn("") != defaultHierarchy {
		t.Fatalf("the Chinese default changed: %q", hierarchyIn(""))
	}
	want := parseHierarchy(defaultHierarchy)
	for _, l := range []string{"en", "ja"} {
		h := hierarchyIn(l)
		if got := parseHierarchy(h); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q reads as %+v, want %+v", l, h, got, want)
		}
		if zhHierarchy(h) != defaultHierarchy {
			t.Errorf("%s: zhHierarchy(%q) = %q", l, h, zhHierarchy(h))
		}
		if localHierarchy(l, defaultHierarchy) != h || localHierarchy("", h) != defaultHierarchy {
			t.Errorf("%s: localHierarchy does not move the default between languages", l)
		}
	}
	if own := "主菜单 > 衣柜 > {素材}"; localHierarchy("en", own) != own {
		t.Error("a template the player wrote was changed")
	}
	// a level written out as a category, in another language: hair goes under its own
	base, per := menuPath(parseHierarchy("Main menu > Outfits > {asset} > {toggles}"), "头发")
	if !per || !reflect.DeepEqual(base, []string{"头发"}) {
		t.Errorf("menuPath = %v, %v", base, per)
	}
}

func TestLocalMenu(t *testing.T) {
	items := []map[string]any{
		{"kind": "outfit", "path": []string{"衣服", "水手服"}, "label": "穿上"},
		{"kind": "part", "path": []string{"衣服", "水手服"}, "label": "外套"},
		{"kind": "strip", "path": []string{"衣服"}, "label": stripLabel},
	}
	get := func(lang string, tree []menuNode) [][]string {
		var out [][]string
		for _, it := range localMenu(lang, tree, "Avatar Menu", items) {
			out = append(out, append(append([]string{}, it["path"].([]string)...), it["label"].(string)))
		}
		return out
	}
	// Chinese on an avatar without a menu: as it always was
	if got, want := get("", nil), [][]string{{"衣服", "水手服", "穿上"}, {"衣服", "水手服", "外套"}, {"衣服", stripLabel}}; !reflect.DeepEqual(got, want) {
		t.Errorf("zh: %v", got)
	}
	if got, want := get("en", nil), [][]string{{"Outfits", "水手服", "Wear"}, {"Outfits", "水手服", "Jacket"}, {"Outfits", "Take all off"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("en: %v", got)
	}
	if got := get("ja", nil); got[0][0] != "衣装" || got[0][2] != "着る" || got[2][1] != "全部脱ぐ" {
		t.Errorf("ja: %v", got)
	}
	// an avatar set up in Chinese, the player now in English: the names it has are used, new ones are English
	zh := []menuNode{{Object: "Avatar Menu", Children: []menuNode{{Label: "衣服", Children: []menuNode{
		{Label: "水手服", Children: []menuNode{{Label: "穿上"}}}, {Label: stripLabel}}}}}}
	if got, want := get("en", zh), [][]string{{"衣服", "水手服", "穿上"}, {"衣服", "水手服", "Jacket"}, {"衣服", stripLabel}}; !reflect.DeepEqual(got, want) {
		t.Errorf("en on a Chinese menu: %v", got)
	}
	// … and the other way round
	en := []menuNode{{Object: "Avatar Menu", Children: []menuNode{{Label: "Outfits", Children: []menuNode{
		{Label: "水手服", Children: []menuNode{{Label: "Wear"}, {Label: "Jacket"}}}}}}}}
	if got, want := get("", en), [][]string{{"Outfits", "水手服", "Wear"}, {"Outfits", "水手服", "Jacket"}, {"Outfits", stripLabel}}; !reflect.DeepEqual(got, want) {
		t.Errorf("zh on an English menu: %v", got)
	}
	if items[0]["label"] != "穿上" || items[0]["path"].([]string)[0] != "衣服" {
		t.Error("the plan given was changed")
	}
	// a plan as the AI writes it
	ai := []map[string]any{{"path": []any{"Outfits"}, "label": "Dress"}}
	if got := localMenu("en", zh, "Avatar Menu", ai)[0]["path"].([]any)[0]; got != "衣服" {
		t.Errorf("an AI plan's path: %v", got)
	}
}
