package main

import (
	"strings"
	"testing"
)

func TestSplitVer(t *testing.T) {
	cases := map[string][2]string{
		"Kaguya_v1.07.zip":                                 {"kaguya", "1.07"},
		"Kaguya_v1.06":                                     {"kaguya", "1.06"},
		"FaceTracking_1.0.1_for_Chocolat_1.01":             {"facetrackingforchocolat101", "1.0.1"},
		"HYPERTECH_EXO_FRAME_For_Milfy_v1.01.unitypackage": {"hypertechexoframeformilfy", "1.01"},
		"Moon Dress.zip":                                   {"", ""},
	}
	for in, want := range cases {
		s, v := splitVer(in)
		if s != want[0] || v != want[1] {
			t.Errorf("%s: got (%s, %s)", in, s, v)
		}
	}
}

func TestPurchaseNewer(t *testing.T) {
	a := &Asset{Name: "Kaguya_v1.06", RawName: "Kaguya_v1.06", Locations: []Location{{Path: `B:\dl\Kaguya_v1.06`}}}
	p := &Purchase{Files: []string{"Kaguya_v1.07.zip", "Kaguya_PSD.zip"}}
	if v, have := purchaseNewer(a, p); v != "1.07" || have != "1.06" {
		t.Errorf("got %q %q", v, have)
	}
	p2 := &Purchase{Files: []string{"Kaguya_v1.06.zip"}}
	if v, _ := purchaseNewer(a, p2); v != "" {
		t.Errorf("same version flagged: %q", v)
	}
	p3 := &Purchase{Files: []string{"OtherThing_v2.0.zip"}}
	if v, _ := purchaseNewer(a, p3); v != "" {
		t.Errorf("unrelated file flagged: %q", v)
	}
}

func TestDiffPan(t *testing.T) {
	old := &PanListing{Files: []*PanFile{dir("A", file("a1.zip"), file("a2.zip"))}}
	l := &PanListing{Files: []*PanFile{dir("A", file("a1.zip"), file("a3.zip"))}}
	diffPan(old, l)
	if strings.Join(l.Added, ",") != "/A/a3.zip" || strings.Join(l.Removed, ",") != "/A/a2.zip" || l.Changed == 0 {
		t.Errorf("%v %v %d", l.Added, l.Removed, l.Changed)
	}
	again := &PanListing{Files: []*PanFile{dir("A", file("a1.zip"), file("a3.zip"))}}
	diffPan(l, again)
	if again.Changed != l.Changed || len(again.Added) != 1 {
		t.Error("an unchanged re-read must keep the earlier notice")
	}
	add, rem := shareNews(l, "/A")
	if strings.Join(add, ",") != "/a3.zip" || strings.Join(rem, ",") != "/a2.zip" {
		t.Errorf("news for part: %v %v", add, rem)
	}
}

func TestBoothChanges(t *testing.T) {
	old := &BoothInfo{Name: "X", Price: "¥ 1,000", Desc: "a\nb", Images: []string{"1"}, Ver: 4}
	bi := &BoothInfo{Name: "X", Price: "¥ 1,200", Desc: "a\nb\nv1.07 更新", Images: []string{"1"}, Ver: 4}
	boothChanges(old, bi)
	if bi.Changed == 0 || bi.ChangeNote != "价格 ¥ 1,000 → ¥ 1,200，商品说明改了" {
		t.Errorf("%q", bi.ChangeNote)
	}
	same := &BoothInfo{Name: "X", Price: "¥ 1,000", Desc: "a  b", Images: []string{"1"}, Ver: 4}
	boothChanges(old, same)
	if same.Changed != 0 {
		t.Error("whitespace-only change flagged")
	}
}
