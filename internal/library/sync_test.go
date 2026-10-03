package library

import (
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/netdisk"
	"vrclib/internal/testkit"
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
	a := &core.Asset{Name: "Kaguya_v1.06", RawName: "Kaguya_v1.06", Locations: []core.Location{{Path: `B:\dl\Kaguya_v1.06`}}}
	p := &core.Purchase{Files: []string{"Kaguya_v1.07.zip", "Kaguya_PSD.zip"}}
	if v, have := purchaseNewer(a, p); v != "1.07" || have != "1.06" {
		t.Errorf("got %q %q", v, have)
	}
	p2 := &core.Purchase{Files: []string{"Kaguya_v1.06.zip"}}
	if v, _ := purchaseNewer(a, p2); v != "" {
		t.Errorf("same version flagged: %q", v)
	}
	p3 := &core.Purchase{Files: []string{"OtherThing_v2.0.zip"}}
	if v, _ := purchaseNewer(a, p3); v != "" {
		t.Errorf("unrelated file flagged: %q", v)
	}
}

func TestDiffPan(t *testing.T) {
	old := &core.PanListing{Files: []*core.PanFile{testkit.PanDir("A", testkit.PanFile("a1.zip"), testkit.PanFile("a2.zip"))}}
	l := &core.PanListing{Files: []*core.PanFile{testkit.PanDir("A", testkit.PanFile("a1.zip"), testkit.PanFile("a3.zip"))}}
	netdisk.DiffPan(old, l)
	if strings.Join(l.Added, ",") != "/A/a3.zip" || strings.Join(l.Removed, ",") != "/A/a2.zip" || l.Changed == 0 {
		t.Errorf("%v %v %d", l.Added, l.Removed, l.Changed)
	}
	again := &core.PanListing{Files: []*core.PanFile{testkit.PanDir("A", testkit.PanFile("a1.zip"), testkit.PanFile("a3.zip"))}}
	netdisk.DiffPan(l, again)
	if again.Changed != l.Changed || len(again.Added) != 1 {
		t.Error("an unchanged re-read must keep the earlier notice")
	}
	add, rem := shareNews(l, "/A")
	if strings.Join(add, ",") != "/a3.zip" || strings.Join(rem, ",") != "/a2.zip" {
		t.Errorf("news for part: %v %v", add, rem)
	}
}
