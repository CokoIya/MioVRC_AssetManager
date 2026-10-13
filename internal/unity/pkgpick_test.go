package unity

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

func TestNameVersion(t *testing.T) {
	for _, c := range []struct{ name, ver, series string }{
		{"Kipfel_1.0.3.unitypackage", "1.0.3", "kipfel"},
		{"Plum_v1.0.0.unitypackage", "1.0.0", "plum"},
		{"Plum_v.1.0.1.unitypackage", "1.0.1", "plum"},
		{"RealKissSystemV1.4.0-beta.2.unitypackage", "1.4.0-beta.2", "realkisssystem"},
		{"RealKissSystemWithDPSV1.2.0.unitypackage", "1.2.0", "realkisssystemwithdps"},
		{"VRSuya_Suyasuya_Package_Released_250526.unitypackage", "250526", "vrsuya_suyasuya_package_released"},
		{"kipfel_hakui_ver1.4.2.unitypackage", "1.4.2", "kipfel_hakui"},
		{"CottonCandyTwin_Ver1.0.unitypackage", "1.0", "cottoncandytwin"},
		{"[MA] Attractive Battery v.1.1.unitypackage", "1.1", "[ma]_attractive_battery"},
		{"SmallFireworksFestival ver1.00.unitypackage", "1.00", "smallfireworksfestival"},
		{"ABT_Full_左手_2.2.4.unitypackage", "2.2.4", "abt_full_左手"},
		{"ABT(JP)_L_NoLocomotion_1.3.0.unitypackage", "1.3.0", "abt(jp)_l_nolocomotion"},
		{"TexColorAdjuster.unitypackage", "", "texcoloradjuster"},
		{"ARI_LTT_01.unitypackage", "", "ari_ltt_01"}, // a number, not a version
		{"ABT2.x单独汉化.unitypackage", "", "abt2_x单独汉化"},
		{"Nav2.unitypackage", "", "nav2"},
		{"AsymmetricalHalf-upHair VerMAMEHINATA_Quest.unitypackage", "", "asymmetricalhalf_uphair_vermamehinata_quest"},
	} {
		v, rest := nameVersion(c.name)
		if v.text != c.ver || seriesKey(rest) != c.series {
			t.Errorf("%s: version %q series %q, want %q %q", c.name, v.text, seriesKey(rest), c.ver, c.series)
		}
	}
	newer := func(a, b string) {
		t.Helper()
		va, _ := nameVersion("x_" + a + ".unitypackage")
		vb, _ := nameVersion("x_" + b + ".unitypackage")
		if c, ok := va.cmp(vb); !ok || c != 1 {
			t.Errorf("%s after %s: %d %v", a, b, c, ok)
		}
		if c, ok := vb.cmp(va); !ok || c != -1 {
			t.Errorf("%s before %s: %d %v", b, a, c, ok)
		}
	}
	newer("v1.4.0-beta.2", "v1.3.2")
	newer("v1.4.0", "v1.4.0-beta.2")
	newer("v1.4.0-beta.10", "v1.4.0-beta.9")
	newer("v1.10", "v1.9")
	newer("V1.1", "V1.01")
	newer("2.2.4", "2.1.6")
	newer("1.2", "1.1.9")
	newer("250526", "250106")
	newer("20250526", "241231")
	if _, ok := (pkgVer{date: 20250101}).cmp(pkgVer{parts: []string{"1"}}); ok {
		t.Error("a date and a number compared")
	}
}

// a card's folder with packages; files: name → the files of the package (guid letter → body; "+" ahead of a
// body: another path for the same guid)
func cardPackages(t *testing.T, dir string, pkgs map[string]map[string]string) {
	t.Helper()
	for name, files := range pkgs {
		var fs []pkgFile
		var keys []string
		for gl := range files {
			keys = append(keys, gl)
		}
		sort.Strings(keys)
		for _, gl := range keys {
			body := files[gl]
			p := "Assets/Thing/" + gl + ".prefab"
			if strings.HasPrefix(body, "+") {
				p, body = "Assets/Other/"+gl+".prefab", body[1:]
			}
			fs = append(fs, pkgFile{guid: g(gl), path: p, body: body})
		}
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0755)
		makeUnityPackage(t, filepath.Join(dir, name), fs)
	}
}

func planOf(t *testing.T, dir string) pkgPlan {
	t.Helper()
	return planFor(t, dir, "")
}

// planFor: the plan for a project that builds for target ("quest", "pc", "" when that is not known)
func planFor(t *testing.T, dir, target string) pkgPlan {
	t.Helper()
	st := &core.Store{Settings: core.Settings{Bases: core.DefaultSettings().Bases}}
	return planPackages(st, findPackages([]string{dir}), nil, nil, target, nil)
}

func picks(p pkgPlan) string {
	var out []string
	for _, c := range p.choices {
		if c.Pick {
			out = append(out, c.Name)
		}
	}
	return strings.Join(out, ",")
}

func skipList(s []PkgSkip) string {
	var out []string
	for _, x := range s {
		out = append(out, x.Name+"="+x.Why)
	}
	return strings.Join(out, ",")
}

func TestPlanPackages(t *testing.T) {
	five := func(body string) map[string]string {
		return map[string]string{"1": body, "2": body, "3": body, "4": body, "5": body}
	}
	// the versions of a base body: the newest; the same package in another folder once
	d := t.TempDir()
	cardPackages(t, d, map[string]map[string]string{
		"Kipfel_1.0.3.unitypackage": five("a"), "Kipfel_1.1.1.unitypackage": five("b"), "Kipfel_1.2.0.unitypackage": five("c"),
		"copy/Kipfel_1.2.0.unitypackage": five("c"),
	})
	p := planOf(t, d)
	if p.ask || picks(p) != "Kipfel_1.2.0.unitypackage" {
		t.Errorf("versions: ask %v picks %s", p.ask, picks(p))
	}
	if got := skipList(p.skips([]string{filepath.Join(d, "Kipfel_1.2.0.unitypackage")})); got != "Kipfel_1.2.0.unitypackage=dup,Kipfel_1.0.3.unitypackage=old,Kipfel_1.1.1.unitypackage=old" {
		t.Errorf("skips: %s", got)
	}
	// an older version that sorts last by its path does not win any more
	d = t.TempDir()
	cardPackages(t, d, map[string]map[string]string{
		"VRSuya_Package_Released_250526.unitypackage": five("new"), "旧版/VRSuya_Package_Released_250106.unitypackage": five("old"),
	})
	if p := planOf(t, d); p.ask || picks(p) != "VRSuya_Package_Released_250526.unitypackage" {
		t.Errorf("dates: %s", picks(p))
	}
	// the versions of one package and a variant of it that writes the same files: the newest of the one, and
	// the player is asked about the other
	d = t.TempDir()
	cardPackages(t, d, map[string]map[string]string{
		"RealKissSystemV1.2.0.unitypackage": five("12"), "RealKissSystemV1.3.2.unitypackage": five("132"),
		"RealKissSystemV1.4.0-beta.2.unitypackage":    map[string]string{"1": "14", "2": "14", "3": "14", "4": "14", "5": "14", "6": "14"},
		"RealKissSystemWithDPSV1.2.0.unitypackage":    map[string]string{"1": "dps", "2": "dps", "3": "dps", "4": "dps", "5": "dps"},
		"RealKissSystem_Readme_Separate.unitypackage": map[string]string{"e": "readme"},
	})
	p = planOf(t, d)
	if !p.ask || p.why != "overlap" || picks(p) != "RealKissSystemV1.4.0-beta.2.unitypackage,RealKissSystem_Readme_Separate.unitypackage" {
		t.Errorf("variants: ask %v %q picks %s", p.ask, p.why, picks(p))
	}
	for _, c := range p.choices {
		if c.Name == "RealKissSystemWithDPSV1.2.0.unitypackage" && (c.Why != "alt" || !strings.Contains(c.Note, "RealKissSystemV1.4.0-beta.2")) {
			t.Errorf("the variant: %+v", c)
		}
		if c.Name == "RealKissSystemV1.2.0.unitypackage" && (c.Why != "old" || c.Note != "旧版本，已有 1.4.0-beta.2") {
			t.Errorf("the old one: %+v", c)
		}
	}
	// the player ticks an old and the new one: the old one first, so that the new one stays
	got := p.sortPicks([]string{filepath.Join(d, "RealKissSystemV1.4.0-beta.2.unitypackage"), filepath.Join(d, "RealKissSystemV1.2.0.unitypackage")})
	if filepath.Base(got[0]) != "RealKissSystemV1.2.0.unitypackage" {
		t.Errorf("order: %v", got)
	}
	// left and right hand: one of them, the player's language first
	d = t.TempDir()
	cardPackages(t, d, map[string]map[string]string{
		"Tool_Left.unitypackage": five("L"), "Tool_Right.unitypackage": five("R"), "Tool_右手_汉化.unitypackage": five("R"),
	})
	p = planOf(t, d)
	if !p.ask || picks(p) != "Tool_右手_汉化.unitypackage" {
		t.Errorf("hands: ask %v picks %s", p.ask, picks(p))
	}
	// an add-on that writes over some files of another, and a package that shares files as they are: both
	// imported, the add-on after what it changes, nothing asked
	d = t.TempDir()
	base := map[string]string{"1": "body", "2": "x", "3": "x", "4": "x", "5": "x", "6": "x", "7": "x", "8": "x", "9": "x", "a": "fbx"}
	cardPackages(t, d, map[string]map[string]string{
		"A_FaceTracking.unitypackage": {"1": "body with blendshapes", "b": "ft"},
		"Z_Base.unitypackage":         base,
		"M_Colors.unitypackage":       {"2": "x", "3": "x", "c": "red", "d": "blue"},
	})
	p = planOf(t, d)
	if p.ask || picks(p) != "M_Colors.unitypackage,Z_Base.unitypackage,A_FaceTracking.unitypackage" {
		t.Errorf("add-on: ask %v picks %s", p.ask, picks(p))
	}
	if c := p.choices[2]; c.Why != "addon" || !strings.Contains(c.Note, "Z_Base") {
		t.Errorf("the add-on: %+v", c)
	}
	// a PC and a Quest version of one hair: the PC one
	d = t.TempDir()
	cardPackages(t, d, map[string]map[string]string{
		"AsymmetricalHalf-upHair VerMAMEHINATA&Kipfel_PC.unitypackage": five("pc"),
		"AsymmetricalHalf-upHair VerMAMEHINATA_Quest.unitypackage":     {"e": "quest"},
	})
	p = planOf(t, d)
	if p.ask || picks(p) != "AsymmetricalHalf-upHair VerMAMEHINATA&Kipfel_PC.unitypackage" {
		t.Errorf("quest: ask %v picks %s", p.ask, picks(p))
	}
	if got := skipList(p.skips(nil)); got != "AsymmetricalHalf-upHair VerMAMEHINATA_Quest.unitypackage=quest" {
		t.Errorf("quest skipped: %s", got)
	}
	// only Quest versions: kept
	d = t.TempDir()
	cardPackages(t, d, map[string]map[string]string{"Hair_Quest.unitypackage": {"e": "q"}, "Hat_Quest.unitypackage": {"f": "h"}})
	if p := planOf(t, d); p.ask || picks(p) != "Hair_Quest.unitypackage,Hat_Quest.unitypackage" {
		t.Errorf("quest only: %s", picks(p))
	}
}

// A project that builds for Quest (Android): of a PC and a Quest version, the Quest one, and the PC one is said
// to be left out. For a project that builds for PC, or one nothing is known of, the PC one as before.
func TestPlanPackagesForQuestProject(t *testing.T) {
	five := func(body string) map[string]string {
		return map[string]string{"1": body, "2": body, "3": body, "4": body, "5": body}
	}
	const pc, quest = "AsymmetricalHalf-upHair VerMAMEHINATA&Kipfel_PC.unitypackage", "AsymmetricalHalf-upHair VerMAMEHINATA_Quest.unitypackage"
	d := t.TempDir()
	cardPackages(t, d, map[string]map[string]string{pc: five("pc"), quest: {"e": "quest"}, "Hat.unitypackage": {"f": "hat"}})
	p := planFor(t, d, "quest")
	if p.ask || picks(p) != quest+",Hat.unitypackage" {
		t.Errorf("quest: ask %v picks %s", p.ask, picks(p))
	}
	for _, c := range p.choices {
		if c.Name == pc && (c.Why != "pc" || c.Note != "PC 版本，工程的目标平台为 Android（Quest）") {
			t.Errorf("the PC version: %+v", c)
		}
	}
	if got := skipList(p.skips(nil)); got != pc+"=pc" {
		t.Errorf("quest, skipped: %s", got)
	}
	if got := skipText(PkgSkip{Name: pc, Why: "pc"}); got != pc+"（PC 版本）" {
		t.Errorf("for the log: %s", got)
	}
	for _, target := range []string{"pc", ""} {
		if p := planFor(t, d, target); p.ask || picks(p) != pc+",Hat.unitypackage" || skipList(p.skips(nil)) != quest+"=quest" {
			t.Errorf("%q: ask %v picks %s skipped %s", target, p.ask, picks(p), skipList(p.skips(nil)))
		}
	}
	// only PC versions: kept
	d = t.TempDir()
	cardPackages(t, d, map[string]map[string]string{"Hair_PC.unitypackage": {"e": "h"}, "Hat_PC.unitypackage": {"f": "h"}})
	if p := planFor(t, d, "quest"); p.ask || picks(p) != "Hair_PC.unitypackage,Hat_PC.unitypackage" {
		t.Errorf("PC only: %s", picks(p))
	}
	// where the player picked, a PC version that was not ticked is the player's choice, as a Quest one is
	asked := pkgPlan{ask: true, choices: []PkgChoice{{Path: "/x/" + pc, Name: pc, Why: "pc"}, {Path: "/x/" + quest, Name: quest, Pick: true}}}
	if got := asked.skips([]string{"/x/" + quest}); len(got) != 0 {
		t.Errorf("asked, skipped: %v", got)
	}
}

// The import job: what is left out is said; what the player has to decide waits for the player.
func TestImportPlan(t *testing.T) {
	st, root := bundleLibrary(t)
	card := filepath.Join(root, "Kipfel")
	five := func(body string) map[string]string {
		return map[string]string{"1": body, "2": body, "3": body, "4": body, "5": body}
	}
	cardPackages(t, card, map[string]map[string]string{
		"Kipfel_1.0.3.unitypackage": five("a"), "Kipfel_1.2.0.unitypackage": five("c"), "backup/Kipfel_1.2.0.unitypackage": five("c"),
	})
	proj := newProject(t)
	if got := cards(st); got != "Kipfel" {
		t.Fatalf("cards: %s", got)
	}
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Kipfel"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	j := ended(t)
	if j.Stage != "done" || strings.Join(j.Imported, ",") != "Kipfel_1.2.0.unitypackage" || len(j.Skipped) != 2 ||
		!strings.Contains(j.Msg, "2 个旧版本、重复或会互相覆盖的 unitypackage 未导入") {
		t.Fatalf("the job: %+v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Assets/Thing/1.prefab")); string(b) != "c" {
		t.Errorf("in the project: %q", b)
	}

	// left and right: the player is asked, and what the player ticks is imported
	hands := filepath.Join(root, "Hands")
	cardPackages(t, hands, map[string]map[string]string{"Hands_Left.unitypackage": five("L"), "Hands_Right.unitypackage": five("R")})
	if got := cards(st); got != "Hands, Kipfel" {
		t.Fatalf("cards: %s", got)
	}
	if err := StartImport(st, ImportReq{Key: keyOf(t, st, "Hands"), Project: proj}); err != nil {
		t.Fatal(err)
	}
	var w *ImportJob
	for i := 0; i < 500; i++ {
		if w = ImportSnapshot(); w != nil && w.Stage == "choose" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if w == nil || w.Stage != "choose" || w.Why != "overlap" || len(w.Choices) != 2 {
		t.Fatalf("not asked: %+v", w)
	}
	var right string
	for _, c := range w.Choices {
		if c.Name == "Hands_Right.unitypackage" {
			right = c.Path
		}
	}
	ChooseImport([]string{right})
	j = ended(t)
	if j.Stage != "done" || strings.Join(j.Imported, ",") != "Hands_Right.unitypackage" || fmt.Sprint(len(j.Skipped)) != "0" {
		t.Fatalf("after the choice: %+v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Assets/Thing/1.prefab")); string(b) != "R" {
		t.Errorf("in the project: %q", b)
	}
}

// An import into a project that builds for Quest: the Unity plugin's last heartbeat says so (written while Unity
// was open, an hour ago here), and of a PC and a Quest version the Quest one is imported.
func TestImportIntoQuestProject(t *testing.T) {
	st, _ := bundleLibrary(t)
	d := t.TempDir()
	cardPackages(t, d, map[string]map[string]string{"Hair_PC.unitypackage": {"1": "pc", "2": "pc"}, "Hair_Quest.unitypackage": {"1": "quest"}})
	proj := newProject(t)
	alive := filepath.Join(BridgeDir(proj), "alive.json")
	put(t, alive, []byte(`{"bridge":"1.4.1","unity":"2022.3.22f1","target":"quest"}`))
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(alive, old, old)
	if err := StartImport(st, ImportReq{Paths: []string{d}, Project: proj}); err != nil {
		t.Fatal(err)
	}
	j := ended(t)
	if j.Stage != "done" || strings.Join(j.Imported, ",") != "Hair_Quest.unitypackage" || skipList(j.Skipped) != "Hair_PC.unitypackage=pc" {
		t.Fatalf("the job: %+v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Assets/Thing/1.prefab")); string(b) != "quest" || fileThere(filepath.Join(proj, "Assets/Thing/2.prefab")) {
		t.Errorf("in the project: %q", b)
	}
	// a project that builds for PC: the PC one, as ever
	pcProj := newProject(t)
	put(t, filepath.Join(BridgeDir(pcProj), "alive.json"), []byte(`{"target":"pc"}`))
	if err := StartImport(st, ImportReq{Paths: []string{d}, Project: pcProj}); err != nil {
		t.Fatal(err)
	}
	if j := ended(t); j.Stage != "done" || strings.Join(j.Imported, ",") != "Hair_PC.unitypackage" || skipList(j.Skipped) != "Hair_Quest.unitypackage=quest" {
		t.Fatalf("into the PC project: %+v", j)
	}
}

// A base body into a project built on another one is asked about first; into one with none yet, or its own, not.
func TestImportAsk(t *testing.T) {
	st := testkit.NewStore(t)
	loc := func(n string) []core.Location { return []core.Location{{Path: "/lib/" + n, Kind: "dir"}} }
	st.Assets = []*core.Asset{
		{Key: "k", Name: "Kipfel", Category: "素体", Bases: []string{"Kipfel"}, Locations: loc("Kipfel"), Usage: []core.Usage{{Project: "Kipfel", Status: "used"}}},
		{Key: "p", Name: "Plum", Category: "素体", Bases: []string{"Plum"}, Locations: loc("Plum")},
		{Key: "d", Name: "Dress", Category: "衣服", Bases: []string{"Plum"}, Locations: loc("Dress")},
	}
	if q := ImportAsk(st, "p", "/x/Kipfel"); q != "「Plum」是 Plum 的素体，目标工程「Kipfel」用的是 Kipfel 素体。仍要导入到这个工程吗？" {
		t.Errorf("into another's project: %q", q)
	}
	for _, c := range [][2]string{{"k", "/x/Kipfel"}, {"p", "/x/New"}, {"d", "/x/Kipfel"}} {
		if q := ImportAsk(st, c[0], c[1]); q != "" {
			t.Errorf("%v: %q", c, q)
		}
	}
}
