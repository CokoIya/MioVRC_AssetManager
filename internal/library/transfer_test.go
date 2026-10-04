package library

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

func TestPathsOfAnotherComputer(t *testing.T) {
	under := []struct {
		p, root string
		rest    string
		ok      bool
	}{
		{`D:\VRC\Assets\Dress\a.unitypackage`, `D:\VRC\Assets`, "Dress/a.unitypackage", true},
		{`d:\vrc\assets\Dress`, `D:\VRC\Assets\`, "Dress", true}, // drive letter and names in another case, a trailing separator
		{`D:/VRC/Assets/Dress`, `D:\VRC\Assets`, "Dress", true},  // either separator
		{`D:\VRC\Assets`, `D:\VRC\Assets`, "", true},
		{`D:\VRC\AssetsOld\x`, `D:\VRC\Assets`, "", false}, // a name that only starts the same
		{`E:\VRC\Assets\x`, `D:\VRC\Assets`, "", false},
		{`/home/u/assets/x/y`, `/home/u/assets`, "x/y", true},
		{`\\nas\share\assets\x`, `\\NAS\share\assets`, "x", true},
		{`D:\x`, `D:\`, "x", true},
		{`x`, ``, "", false},
	}
	for _, c := range under {
		rest, ok := underRoot(c.p, c.root)
		if ok != c.ok || strings.Join(rest, "/") != c.rest {
			t.Errorf("underRoot(%q, %q) = %v, %v", c.p, c.root, rest, ok)
		}
	}
	m := newPathMap(map[string]string{`D:\VRC\Assets`: `F:\Lib\My Assets`, `D:\VRC\Assets\Sub`: `/mnt/sub`, `E:\Other`: "", `G:\`: `H:\`})
	apply := []struct{ p, want string }{
		{`d:\vrc\assets\衣服\Dress\a.unitypackage`, `F:\Lib\My Assets\衣服\Dress\a.unitypackage`}, // onto a Windows folder: backslashes
		{`D:\VRC\Assets`, `F:\Lib\My Assets`},
		{`D:\VRC\Assets\Sub\x\y.zip`, `/mnt/sub/x/y.zip`}, // the longer folder counts; onto a folder written with "/": slashes
		{`D:/VRC/Assets/Sub`, `/mnt/sub`},
		{`G:\a\b`, `H:\a\b`},
		{`G:\`, `H:\`},
	}
	for _, c := range apply {
		if got, ok := m.apply(c.p); !ok || got != c.want {
			t.Errorf("apply(%q) = %q, %v; want %q", c.p, got, ok, c.want)
		}
	}
	for _, p := range []string{`E:\Other\x`, `C:\Users\me`, ``, `D:\VRC\AssetsOld`} {
		if got, ok := m.apply(p); ok {
			t.Errorf("apply(%q) = %q: not under a mapped folder", p, got)
		}
	}
	if k := pathKeyOf(`D:\VRC\Assets`, `d:\vrc\assets\衣服\材质\X.zip`); k != "path:Assets/衣服/材质/x.zip" {
		t.Errorf("pathKeyOf: %q", k)
	}
	// a whole drive or a share as the asset folder: the scan calls it "\" on Windows ("/" at the top of a Unix disk)
	for root, want := range map[string]string{`D:\`: `\`, `D:`: `\`, `\\nas\share`: `\`, `\\nas\share\lib`: "lib", "/": "/", `D:\VRC\Assets\`: "Assets", "/home/u/assets": "assets"} {
		if got := rootName(root); got != want {
			t.Errorf("rootName(%q) = %q, want %q", root, got, want)
		}
	}
	if k := pathKeyOf(`D:\`, `d:\衣服\X.zip`); k != `path:\/衣服/x.zip` {
		t.Errorf("pathKeyOf under a drive: %q", k)
	}
	// … and its assets' keys follow it into a folder, and back out of one
	drive := &core.Asset{Key: `path:\/衣服/x.zip`, Locations: []core.Location{{Path: `D:\衣服\X.zip`, Kind: "zip", Root: `D:\`}}}
	if k := remapKey(drive.Key, drive, newPathMap(map[string]string{`D:\`: `F:\Lib`}), []string{`D:\`}); k != "path:Lib/衣服/x.zip" {
		t.Errorf("from a drive into a folder: %q", k)
	}
	inFolder := &core.Asset{Key: "path:Lib/衣服/x.zip", Locations: []core.Location{{Path: `F:\Lib\衣服\X.zip`, Kind: "zip", Root: `F:\Lib`}}}
	if k := remapKey(inFolder.Key, inFolder, newPathMap(map[string]string{`F:\Lib`: `G:\`}), []string{`F:\Lib`}); k != `path:\/衣服/x.zip` {
		t.Errorf("from a folder onto a drive: %q", k)
	}
	if k, where := looseKey(`path:\/gone/thing`, newPathMap(map[string]string{`D:\`: `F:\Lib`}), []string{`D:\`, `E:\Other`}); k != "path:Lib/gone/thing" || where != `D:\` {
		t.Errorf("a key without an asset, under a drive: %q %q", k, where)
	}
	if baseOf(`D:\VRC\Assets\`) != "Assets" || baseOf("/a/b") != "b" || sepOf(`D:\x`) != `\` || sepOf("/x") != "/" || sepOf("D:") != `\` {
		t.Error("baseOf / sepOf")
	}
}

// winLibrary: a library as a Windows computer wrote it.
func winLibrary() *core.Store {
	st := &core.Store{}
	fillStore(st)
	st.Settings.Roots = []string{`D:\VRC\Assets`, `E:\Other`}
	st.Settings.ProjectRoots = []string{`D:\Unity`}
	st.Settings.DownloadDir = `D:\VRC\Assets\dl`
	loc := func(root, rel, kind string) core.Location {
		return core.Location{Path: root + `\` + rel, Kind: kind, Root: root}
	}
	st.Assets = []*core.Asset{
		{Key: "booth:111", AltKey: "name:moondress", Name: "Moon Dress", Locations: []core.Location{loc(`D:\VRC\Assets`, `111 Moon Dress`, "dir")},
			Packages: []string{`D:\VRC\Assets\111 Moon Dress\Moon.unitypackage`}, Covers: []string{`D:\VRC\Assets\111 Moon Dress\cover.png`},
			MetaDirs: []string{`D:\VRC\Assets\111 Moon Dress\Assets`}, PSDs: []core.PSDFile{{Path: `D:\VRC\Assets\111 Moon Dress\tex.psd`}},
			Usage: []core.Usage{{Project: "Avatar", Status: "used", Matched: 9, Total: 10}}, GuidCount: 10, HasDir: true},
		// goes by where it is: the folder's name is part of its key. The drive letter is written small here
		{Key: "path:Assets/衣服/材质/x.zip", Name: "材质", Locations: []core.Location{{Path: `d:\vrc\assets\衣服\材质\X.zip`, Kind: "zip", Root: `D:\VRC\Assets`}}},
		// in both folders: the part in the folder without a mapping waits
		{Key: "name:sharedhair", Name: "Shared Hair", Locations: []core.Location{loc(`D:\VRC\Assets`, `Shared Hair`, "dir"), loc(`E:\Other`, `Shared Hair.zip`, "zip")},
			Archives: []string{`E:\Other\Shared Hair.zip`}, HasDir: true},
		{Key: "name:onlyother", Name: "Only Other", Locations: []core.Location{loc(`E:\Other`, `Only Other`, "dir")}, HasDir: true,
			Usage: []core.Usage{{Project: "Avatar", Status: "partial"}}},
		{Key: "path:Other/道具/a", Name: "道具", Locations: []core.Location{loc(`E:\Other`, `道具\A`, "dir")}, HasDir: true},
	}
	st.User = map[string]*core.UserData{
		"booth:111":               {Notes: "my favourite", Tags: []string{"red"}, Cover: `D:\VRC\Assets\111 Moon Dress\cover.png`},
		"path:Assets/衣服/材质/x.zip": {Notes: "textures", Category: "材质"},
		"name:onlyother":          {Notes: "waits by name"},
		"path:Other/道具/a":         {Notes: "waits by path", Cover: `E:\Other\道具\A\c.png`},
		"path:Assets/gone/thing":  {Tags: []string{"loose"}},
		"pan:1Share":              {ShareURL: "https://pan.baidu.com/s/1Share", Downloaded: `D:\VRC\Assets\dl\Share`, DownloadDir: `C:\tmp\half`},
	}
	st.FirstSeen = map[string]int64{"booth:111": 100, "path:Assets/衣服/材质/x.zip": 200, "path:Other/道具/a": 300}
	st.Overrides = map[string]string{`d:\vrc\assets\衣服`: "split", `e:\other\junk`: "ignore", `c:\elsewhere`: "ignore"}
	st.Booth["111"] = &core.BoothInfo{ID: "111", Name: "Moon Dress", Cover: `C:\Users\me\AppData\Local\MioVRC_AssetManager\covers\booth_111.jpg`}
	st.Purchases["111"] = &core.Purchase{ID: "111", Cover: `libdata\covers\purchase_111.jpg`}
	st.Downloaded = map[string]*core.DLRecord{"1": {Item: "111", Path: `D:\VRC\Assets\dl\111 Moon Dress`}, "2": {Item: "x", Path: `C:\nowhere\x`}}
	st.Projects = []core.ProjectInfo{{Name: "Avatar", Path: `D:\Unity\Avatar`}}
	return st
}

func TestRemapStore(t *testing.T) {
	in := winLibrary()
	covers := func(old string) string {
		if n := baseOf(old); n == "booth_111.jpg" {
			return "/data/covers/" + n
		}
		return ""
	}
	rm := remapStore(in, map[string]string{`D:\VRC\Assets`: `F:\Lib\Stuff`, `E:\Other`: ""}, map[string]string{`D:\Unity`: `F:\Unity`}, covers)
	out := rm.store
	byKey := map[string]*core.Asset{}
	var keys []string
	for _, a := range out.Assets {
		byKey[a.Key] = a
		keys = append(keys, a.Key)
	}
	sort.Strings(keys)
	if strings.Join(keys, " ") != "booth:111 name:sharedhair path:Stuff/衣服/材质/x.zip" {
		t.Fatalf("assets here: %v", keys)
	}
	a := byKey["booth:111"]
	if a.Locations[0].Path != `F:\Lib\Stuff\111 Moon Dress` || a.Locations[0].Root != `F:\Lib\Stuff` || a.Packages[0] != `F:\Lib\Stuff\111 Moon Dress\Moon.unitypackage` ||
		a.Covers[0] != `F:\Lib\Stuff\111 Moon Dress\cover.png` || a.MetaDirs[0] != `F:\Lib\Stuff\111 Moon Dress\Assets` || a.PSDs[0].Path != `F:\Lib\Stuff\111 Moon Dress\tex.psd` {
		t.Errorf("paths of booth:111: %+v", a)
	}
	if len(a.Usage) != 1 || a.Usage[0].Project != "Avatar" || a.GuidCount != 10 || a.AltKey != "name:moondress" {
		t.Errorf("usage did not follow: %+v", a)
	}
	// the asset that goes by its path: a new key under the new folder's name, the path below it as it was
	p := byKey["path:Stuff/衣服/材质/x.zip"]
	if p.Locations[0].Path != `F:\Lib\Stuff\衣服\材质\X.zip` || p.Locations[0].Root != `F:\Lib\Stuff` {
		t.Errorf("path asset: %+v", p.Locations)
	}
	if h := byKey["name:sharedhair"]; len(h.Locations) != 1 || h.Locations[0].Path != `F:\Lib\Stuff\Shared Hair` || len(h.Archives) != 0 || !h.HasDir {
		t.Errorf("the asset in both folders: %+v", h)
	}
	// what the player wrote follows the keys
	u := out.User
	if u["booth:111"] == nil || u["booth:111"].Notes != "my favourite" || u["booth:111"].Cover != `F:\Lib\Stuff\111 Moon Dress\cover.png` {
		t.Errorf("notes of booth:111: %+v", u["booth:111"])
	}
	if x := u["path:Stuff/衣服/材质/x.zip"]; x == nil || x.Notes != "textures" || u["path:Assets/衣服/材质/x.zip"] != nil {
		t.Errorf("notes of the path asset did not move to its new key: %v", core.SortedKeys(u))
	}
	if x := u["path:Stuff/gone/thing"]; x == nil || x.Tags[0] != "loose" {
		t.Errorf("notes without an asset: %v", core.SortedKeys(u))
	}
	if u["name:onlyother"] == nil || u["path:Other/道具/a"] != nil {
		t.Errorf("a name key is the same everywhere and waits in the library; a path key waits with its folder: %v", core.SortedKeys(u))
	}
	if x := u["pan:1Share"]; x.Downloaded != `F:\Lib\Stuff\dl\Share` || x.DownloadDir != "" {
		t.Errorf("netdisk card: %+v", x)
	}
	if out.FirstSeen["path:Stuff/衣服/材质/x.zip"] != 200 || out.FirstSeen["booth:111"] != 100 || len(out.FirstSeen) != 2 {
		t.Errorf("first seen: %v", out.FirstSeen)
	}
	if len(out.Overrides) != 1 || out.Overrides[core.PathKey(`F:\Lib\Stuff\衣服`)] != "split" {
		t.Errorf("overrides: %v", out.Overrides)
	}
	if out.Booth["111"].Cover != "/data/covers/booth_111.jpg" || out.Purchases["111"].Cover != "" {
		t.Errorf("covers: %q %q", out.Booth["111"].Cover, out.Purchases["111"].Cover)
	}
	if len(out.Downloaded) != 1 || out.Downloaded["1"].Path != `F:\Lib\Stuff\dl\111 Moon Dress` {
		t.Errorf("downloaded: %+v", out.Downloaded)
	}
	s := out.Settings
	if strings.Join(s.Roots, "|") != `F:\Lib\Stuff` || strings.Join(s.ProjectRoots, "|") != `F:\Unity` || s.DownloadDir != `F:\Lib\Stuff\dl` {
		t.Errorf("settings: %+v", s)
	}
	if len(out.Projects) != 1 || out.Projects[0].Path != `F:\Unity\Avatar` {
		t.Errorf("projects: %+v", out.Projects)
	}
	// the folder without a mapping: its assets, and what goes by their path, wait
	if len(rm.pending) != 1 || rm.pending[0].Root != `E:\Other` || rm.waiting != 3 {
		t.Fatalf("pending: %+v (waiting %d)", rm.pending, rm.waiting)
	}
	pe := rm.pending[0]
	var pk []string
	for _, a := range pe.Assets {
		pk = append(pk, a.Key)
	}
	sort.Strings(pk)
	if strings.Join(pk, " ") != "name:onlyother name:sharedhair path:Other/道具/a" || pe.User["path:Other/道具/a"] == nil || pe.FirstSeen["path:Other/道具/a"] != 300 ||
		pe.Overrides[`e:\other\junk`] != "ignore" {
		t.Errorf("what waits: %v %+v", pk, pe)
	}
	// the library that was read is not changed by any of it
	if in.Assets[0].Locations[0].Path != `D:\VRC\Assets\111 Moon Dress` || in.User["booth:111"].Cover != `D:\VRC\Assets\111 Moon Dress\cover.png` || len(in.Settings.Roots) != 2 {
		t.Error("the input was changed")
	}

	// onto a folder of the same name, on this kind of system: the path keys stay, the separators change
	rm = remapStore(winLibrary(), map[string]string{`D:\VRC\Assets`: `/mnt/big/Assets`, `E:\Other`: `/mnt/Other2`}, nil, covers)
	byKey = map[string]*core.Asset{}
	for _, a := range rm.store.Assets {
		byKey[a.Key] = a
	}
	if x := byKey["path:Assets/衣服/材质/x.zip"]; x == nil || x.Locations[0].Path != "/mnt/big/Assets/衣服/材质/X.zip" {
		t.Errorf("same folder name: %v", rm.store.Assets)
	}
	if x := byKey["path:Other2/道具/a"]; x == nil || x.Locations[0].Path != "/mnt/Other2/道具/A" || rm.store.User["path:Other2/道具/a"] == nil ||
		rm.store.User["path:Other2/道具/a"].Cover != "/mnt/Other2/道具/A/c.png" {
		t.Errorf("second folder: %v %v", rm.store.Assets, core.SortedKeys(rm.store.User))
	}
	if h := byKey["name:sharedhair"]; len(h.Locations) != 2 || h.Archives[0] != "/mnt/Other2/Shared Hair.zip" || len(rm.pending) != 0 || len(rm.store.Projects) != 0 {
		t.Errorf("both mapped: %+v, pending %v", h, rm.pending)
	}
}

// metaFile: a file of an unpacked Unity asset with its .meta (which is what usage is told by).
func metaFile(t *testing.T, p, guid string) {
	t.Helper()
	writeBytes(t, p, []byte("x"))
	writeBytes(t, p+".meta", []byte("fileFormatVersion: 2\nguid: "+strings.Repeat(guid, 32)[:32]+"\n"))
}

// libOnDisk: a library as the scan made it, of real folders: an asset with a Booth id that a project uses,
// one that goes by its path (a name too short to go by), one in a second folder; a cover, the player's
// notes, and data files of other parts of the program.
func libOnDisk(t *testing.T) (st *core.Store, rootA, rootB, projRoot string) {
	t.Helper()
	st = testkit.NewStore(t)
	rootA, rootB, projRoot = filepath.Join(t.TempDir(), "Assets"), filepath.Join(t.TempDir(), "Extra"), filepath.Join(t.TempDir(), "Unity")
	moon := filepath.Join(rootA, "9000111 Moon Dress")
	for i, g := range []string{"a", "b", "c", "d"} {
		metaFile(t, filepath.Join(moon, "Moon", "f"+core.Itoa(i)+".prefab"), g)
		metaFile(t, filepath.Join(projRoot, "Avatar", "Assets", "Moon", "f"+core.Itoa(i)+".prefab"), g)
	}
	_ = os.MkdirAll(filepath.Join(projRoot, "Avatar", "ProjectSettings"), 0755)
	writeBytes(t, filepath.Join(moon, "cover.png"), testkit.JPEG(4, 4, color.RGBA{1, 2, 3, 255}))
	writeBytes(t, filepath.Join(rootA, "ab", "x.unitypackage"), []byte("pkg"))
	writeBytes(t, filepath.Join(rootB, "Only Extra", "e.unitypackage"), []byte("pkg"))
	st.Settings.Roots, st.Settings.ProjectRoots = []string{rootA, rootB}, []string{projRoot}
	st.Settings.SetupDone, st.Settings.NoWatch, st.Settings.AutoBooth = true, true, false
	st.Settings.HideZh = true // (no names sent to the translator from a test)
	RunFolderScan(st, core.TaskScan)
	RunUsageScan(st, core.TaskUsage)
	keys := map[string]*core.Asset{}
	for _, a := range st.Assets {
		keys[a.Key] = a
	}
	if len(keys) != 3 || keys["booth:9000111"] == nil || keys["path:Assets/ab"] == nil || keys["name:onlyextra"] == nil || len(keys["booth:9000111"].Usage) != 1 {
		t.Fatalf("the scan: %v, usage %+v", core.SortedKeys(keys), keys["booth:9000111"])
	}
	st.User["booth:9000111"] = &core.UserData{Notes: "my favourite", Tags: []string{"red"}, Cover: filepath.Join(moon, "cover.png")}
	st.User["path:Assets/ab"] = &core.UserData{Notes: "short name"}
	st.Overrides[core.PathKey(filepath.Join(rootA, "nothing here"))] = "ignore"
	cover := filepath.Join(core.CoversDir(), "booth_9000111.jpg")
	writeBytes(t, cover, testkit.JPEG(8, 8, color.RGBA{9, 9, 9, 255}))
	st.Booth["9000111"] = &core.BoothInfo{ID: "9000111", Name: "Moon Dress", Cover: cover}
	// what must not travel
	writeBytes(t, filepath.Join(core.CoversDir(), "thumbs", "t.jpg"), []byte("thumb"))
	writeBytes(t, filepath.Join(core.DataDir, "booth-session.dat"), []byte("secret"))
	writeBytes(t, filepath.Join(core.DataDir, "ai-key.dat"), []byte("secret"))
	writeBytes(t, filepath.Join(core.DataDir, "library.log"), []byte("log"))
	writeBytes(t, filepath.Join(core.DataDir, "web-login", "Cookies.json"), []byte("{}"))
	writeBytes(t, filepath.Join(core.DataDir, "MioVRCA.exe"), []byte("MZ"))
	// (where this computer's AI requests go, its check-ups by project path, the previews of its packages by their path)
	writeBytes(t, filepath.Join(core.DataDir, "ai.json"), []byte(`{"provider":"x"}`))
	writeBytes(t, filepath.Join(core.DataDir, "checkups.json"), []byte(`{"projects":{}}`))
	writeBytes(t, filepath.Join(core.DataDir, "pkgcovers.json"), []byte(`{"covers":{}}`))
	writeBytes(t, filepath.Join(core.CoversDir(), "unitypackage", "p.png"), []byte("png"))
	// … and what must: the data files of other parts of the program
	writeBytes(t, filepath.Join(core.DataDir, "recipes.json"), []byte(`{"recipes":[1]}`))
	writeBytes(t, filepath.Join(core.DataDir, "notes.json"), []byte(`{"notes":"x"}`))
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 400 && (BackgroundBusy() || TransferStatus().Running); i++ {
			time.Sleep(25 * time.Millisecond)
		}
	})
	return
}

func exportNow(t *testing.T, st *core.Store) string {
	t.Helper()
	dir := t.TempDir()
	if err := StartExport(st, dir, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400 && TransferStatus().Running; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	r := TransferStatus()
	if r.Err != "" || r.Path == "" || !core.FileExists(r.Path) {
		t.Fatalf("export: %+v", r)
	}
	return r.Path
}

func zipNamesOf(t *testing.T, p string) []string {
	t.Helper()
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var out []string
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

func waitIdle() {
	for i := 0; i < 400 && BackgroundBusy(); i++ {
		time.Sleep(25 * time.Millisecond)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	st, rootA, rootB, projRoot := libOnDisk(t)
	exp := exportNow(t, st)
	if got := strings.Join(zipNamesOf(t, exp), " "); got != "data/covers/booth_9000111.jpg data/notes.json data/recipes.json library.json manifest.json" {
		t.Fatalf("in the export: %s", got)
	}
	info, err := InspectExport(st, exp)
	if err != nil {
		t.Fatal(err)
	}
	if info.Assets != 3 || info.Version != core.AppVersion || len(info.Roots) != 2 || info.Roots[0].Assets != 2 || !info.Roots[0].Exists || info.Roots[0].Suggest != rootA ||
		info.DataFiles != 3 || len(info.ProjectRoots) != 1 || info.ProjectRoots[0].Assets != 1 {
		t.Fatalf("summary: %+v", info)
	}

	// "another computer": a new data folder; the first asset folder and the projects under other names, the
	// second asset folder not there
	newA, newProj := filepath.Join(t.TempDir(), "MyLib"), filepath.Join(t.TempDir(), "Projects")
	if err := os.Rename(rootA, newA); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(projRoot, newProj); err != nil {
		t.Fatal(err)
	}
	extraKept := filepath.Join(t.TempDir(), "ExtraHere")
	if err := os.Rename(rootB, extraKept); err != nil {
		t.Fatal(err)
	}
	core.DataDir = t.TempDir()
	forgetDirs()
	st2 := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	st2.Settings.SetupDone, st2.Settings.NoWatch, st2.Settings.AutoBooth, st2.Settings.Proxy = true, true, false, "direct"
	st2.Settings.HideZh = true
	keepRoot := filepath.Join(t.TempDir(), "Here")
	writeBytes(t, filepath.Join(keepRoot, "Local Thing", "l.unitypackage"), []byte("pkg"))
	st2.Settings.Roots = []string{keepRoot}
	RunFolderScan(st2, core.TaskScan)
	st2.User["name:localthing"] = &core.UserData{Notes: "was here"}
	st2.User["booth:9000111"] = &core.UserData{Tags: []string{"blue"}}
	writeBytes(t, filepath.Join(core.DataDir, "recipes.json"), []byte(`{"recipes":["mine"]}`))
	_ = st2.Save()

	info, err = InspectExport(st2, exp)
	if err != nil || info.Roots[0].Exists || info.Roots[0].Suggest != "" {
		t.Fatalf("summary on the other computer: %+v %v", info, err)
	}
	if _, err := ImportLibrary(st2, ImportOptions{Path: exp, Roots: map[string]string{rootA: filepath.Join(newA, "nope")}, Mode: "merge"}); err == nil {
		t.Fatal("a folder that is not there was accepted")
	}
	if _, err := ImportLibrary(st2, ImportOptions{Path: exp, Roots: map[string]string{rootA: newA}, Mode: ""}); err == nil {
		t.Fatal("no mode was accepted")
	}
	keysOf := func() map[string]*core.Asset {
		m := map[string]*core.Asset{}
		for _, a := range st2.Assets {
			m[a.Key] = a
		}
		return m
	}

	// a run that reads covers out of unitypackages is under way: an import (and its undo) replaces the file
	// it writes, so it is cancelled and waited for — and when it does not stop, nothing is imported
	batch := func(stops bool) {
		pkgJobMu.Lock()
		pkgJob = PkgBatch{Running: true}
		pkgJobMu.Unlock()
		if stops {
			go func() {
				for {
					pkgJobMu.Lock()
					if pkgJob.cancel {
						pkgJob.Running = false
						pkgJobMu.Unlock()
						return
					}
					pkgJobMu.Unlock()
					time.Sleep(2 * time.Millisecond)
				}
			}()
		}
	}
	wait := pkgStopWait
	pkgStopWait = 60 * time.Millisecond
	batch(false)
	had := len(st2.Assets)
	_, err = ImportLibrary(st2, ImportOptions{Path: exp, Roots: map[string]string{rootA: newA}, Projects: map[string]string{projRoot: newProj}, Mode: "merge"})
	if err == nil || err.Error() != pkgStillBusy || TransferStatus().Running || len(st2.Assets) != had {
		t.Fatalf("an import under a run that does not stop: %v (running %v, assets %d → %d)", err, TransferStatus().Running, had, len(st2.Assets))
	}
	pkgStopWait = wait
	batch(true)

	// 1. merge: what is here stays, the rest is added; the folder that is not here waits
	res, err := ImportLibrary(st2, ImportOptions{Path: exp, Roots: map[string]string{rootA: newA}, Projects: map[string]string{projRoot: newProj}, Mode: "merge"})
	if err != nil {
		t.Fatal(err)
	}
	if snap := PkgBatchSnapshot(); snap.Running || !snap.cancel {
		t.Errorf("the run was not cancelled for the import: %+v", snap)
	}
	if res.Assets != 2 || res.Waiting != 1 || strings.Join(res.Pending, "|") != rootB || !core.FileExists(res.Backup) || !strings.HasPrefix(filepath.Base(res.Backup), "import-backup-") ||
		filepath.Dir(res.Backup) != core.DataDir || res.KeptFiles != 1 || res.DataFiles != 2 {
		t.Fatalf("merge result: %+v", res)
	}
	waitIdle() // the rescan: the folders are looked through under their new paths, the projects under theirs
	st2.Mu.RLock()
	keys := keysOf()
	// the asset that goes by its path has a new key, after the new folder's name; nothing is there twice
	if len(keys) != 3 || keys["name:localthing"] == nil || keys["booth:9000111"] == nil || keys["path:MyLib/ab"] == nil {
		t.Errorf("assets after the merge: %v", core.SortedKeys(keys))
	}
	if a := keys["booth:9000111"]; a != nil && (len(a.Usage) != 1 || a.Usage[0].Project != "Avatar" || a.Usage[0].Status != "used" || a.Locations[0].Root != newA) {
		t.Errorf("usage or folder of the Booth asset: %+v", a)
	}
	if u := st2.User["booth:9000111"]; u == nil || u.Notes != "my favourite" || strings.Join(u.Tags, ",") != "blue,red" || u.Cover != filepath.Join(newA, "9000111 Moon Dress", "cover.png") {
		t.Errorf("notes of the Booth asset: %+v", u)
	}
	if u := st2.User["path:MyLib/ab"]; u == nil || u.Notes != "short name" || st2.User["path:Assets/ab"] != nil {
		t.Errorf("notes of the path asset: %v", core.SortedKeys(st2.User))
	}
	if st2.User["name:localthing"].Notes != "was here" || st2.Overrides[core.PathKey(filepath.Join(newA, "nothing here"))] != "ignore" {
		t.Errorf("what was here, or the override: %v", st2.Overrides)
	}
	if c := st2.Booth["9000111"].Cover; c != filepath.Join(core.CoversDir(), "booth_9000111.jpg") || !core.FileExists(c) {
		t.Errorf("Booth cover: %q", c)
	}
	if strings.Join(st2.Settings.Roots, "|") != keepRoot+"|"+newA || strings.Join(st2.Settings.ProjectRoots, "|") != newProj ||
		len(st2.Projects) != 1 || st2.Projects[0].Path != filepath.Join(newProj, "Avatar") {
		t.Errorf("folders: %v %v %+v", st2.Settings.Roots, st2.Settings.ProjectRoots, st2.Projects)
	}
	st2.Mu.RUnlock()
	if b, _ := os.ReadFile(filepath.Join(core.DataDir, "recipes.json")); string(b) != `{"recipes":["mine"]}` {
		t.Errorf("merge replaced a data file that was here: %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(core.DataDir, "notes.json")); string(b) != `{"notes":"x"}` {
		t.Errorf("a data file that was not here did not come: %s", b)
	}
	for _, n := range []string{"booth-session.dat", "ai-key.dat", "MioVRCA.exe", "web-login", "ai.json", "checkups.json", "pkgcovers.json", "covers/unitypackage"} {
		if core.StatOK(filepath.Join(core.DataDir, n)) {
			t.Errorf("%s travelled", n)
		}
	}
	tv := TransferState()
	if len(tv.Pending) != 1 || tv.Pending[0].Root != rootB || tv.Pending[0].Assets != 1 || tv.Undo == nil || tv.Undo.Path != res.Backup || len(tv.Backups) != 1 {
		t.Fatalf("state: %+v", tv)
	}

	// 2. the waiting folder gets a folder
	if _, err := MapPending(st2, rootB, filepath.Join(extraKept, "nope")); err == nil {
		t.Error("a folder that is not there was accepted")
	}
	if n, err := MapPending(st2, rootB, extraKept); err != nil || n != 1 {
		t.Fatalf("MapPending: %d %v", n, err)
	}
	waitIdle()
	st2.Mu.RLock()
	keys = keysOf()
	roots := strings.Join(st2.Settings.Roots, "|")
	st2.Mu.RUnlock()
	if len(keys) != 4 || keys["name:onlyextra"] == nil || keys["name:onlyextra"].Locations[0].Path != filepath.Join(extraKept, "Only Extra") || !strings.HasSuffix(roots, extraKept) || len(TransferState().Pending) != 0 {
		t.Errorf("after mapping the folder: %v roots=%s", core.SortedKeys(keys), roots)
	}

	// 3. undo: the library as it was before the import
	batch(true)
	if err := UndoImport(st2); err != nil {
		t.Fatal(err)
	}
	if snap := PkgBatchSnapshot(); snap.Running || !snap.cancel {
		t.Errorf("the run was not cancelled for the undo: %+v", snap)
	}
	waitIdle()
	st2.Mu.RLock()
	if len(st2.Assets) != 1 || st2.Assets[0].Key != "name:localthing" || strings.Join(st2.Settings.Roots, "|") != keepRoot || st2.User["booth:9000111"].Notes != "" ||
		strings.Join(st2.User["booth:9000111"].Tags, ",") != "blue" || len(st2.Overrides) != 0 || len(st2.Projects) != 0 {
		t.Errorf("after the undo: %d assets, roots %v, user %+v", len(st2.Assets), st2.Settings.Roots, st2.User["booth:9000111"])
	}
	st2.Mu.RUnlock()
	if TransferState().Undo != nil {
		t.Error("the undo can be done twice")
	}
	// … and the data folder too: what the import wrote where there was nothing is gone again
	for _, n := range []string{"notes.json", "covers/booth_9000111.jpg", "covers"} {
		if core.StatOK(filepath.Join(core.DataDir, filepath.FromSlash(n))) {
			t.Errorf("after the undo, %s (written by the import) is still there", n)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(core.DataDir, "recipes.json")); string(b) != `{"recipes":["mine"]}` {
		t.Errorf("after the undo, the data file that was here: %s", b)
	}
	if err := UndoImport(st2); err == nil {
		t.Error("a second undo did something")
	}

	// 4. replace: the library is the export's; this computer's own data file gives way and is in the backup
	res, err = ImportLibrary(st2, ImportOptions{Path: exp, Roots: map[string]string{rootA: newA, rootB: extraKept}, Projects: map[string]string{projRoot: newProj}, Mode: "replace"})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle()
	st2.Mu.RLock()
	keys = keysOf()
	if len(keys) != 3 || keys["name:localthing"] != nil || keys["name:onlyextra"] == nil || keys["path:MyLib/ab"] == nil || strings.Join(st2.Settings.Roots, "|") != newA+"|"+extraKept ||
		st2.User["name:localthing"] != nil || strings.Join(st2.User["booth:9000111"].Tags, ",") != "red" || st2.Settings.Proxy != "direct" || len(keys["booth:9000111"].Usage) != 1 {
		t.Errorf("after replace: %v roots %v", core.SortedKeys(keys), st2.Settings.Roots)
	}
	st2.Mu.RUnlock()
	if b, _ := os.ReadFile(filepath.Join(core.DataDir, "recipes.json")); string(b) != `{"recipes":[1]}` {
		t.Errorf("replace kept the data file that was here: %s", b)
	}
	// (only the file that was here is in the backup: the others have nothing to be put back)
	if got := strings.Join(zipNamesOf(t, res.Backup), " "); got != "data/recipes.json library.json manifest.json pending.json" {
		t.Errorf("in the backup: %s", got)
	}
	onDisk := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	if len(onDisk.Assets) != 3 || onDisk.User["booth:9000111"] == nil {
		t.Errorf("library.json after the import: %d assets", len(onDisk.Assets))
	}
	if err := UndoImport(st2); err != nil {
		t.Fatal(err)
	}
	waitIdle()
	if b, _ := os.ReadFile(filepath.Join(core.DataDir, "recipes.json")); string(b) != `{"recipes":["mine"]}` {
		t.Errorf("undo did not bring the data file back: %s", b)
	}
	if core.StatOK(filepath.Join(core.DataDir, "notes.json")) || core.StatOK(filepath.Join(core.CoversDir(), "booth_9000111.jpg")) {
		t.Error("undo left the files the import had created")
	}
}

// craftZip: a zip made by hand, as a careless or hostile hand would.
func craftZip(t *testing.T, manifest any, library string, extra map[string][]byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(n string, b []byte) {
		w, _ := zw.Create(n)
		_, _ = w.Write(b)
	}
	if manifest != nil {
		b, _ := json.Marshal(manifest)
		put("manifest.json", b)
	}
	if library != "" {
		put("library.json", []byte(library))
	}
	names := core.SortedKeys(extra)
	for _, n := range names {
		put(n, extra[n])
	}
	_ = zw.Close()
	p := filepath.Join(t.TempDir(), "x.zip")
	if err := os.WriteFile(p, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportUntrustedZip(t *testing.T) {
	st := testkit.NewStore(t)
	st.Settings.NoWatch, st.Settings.AutoBooth = true, false
	t.Cleanup(waitIdle)
	good := ExportManifest{App: core.AppName, Kind: kindExport, Format: exportFormat, Version: core.AppVersion}
	lib := `{"version":1,"settings":{"roots":[]},"assets":[],"user":{"booth:1":{"notes":"n"}}}`
	outside := filepath.Join(filepath.Dir(core.DataDir), "evil.json")

	// not ours at all
	for name, p := range map[string]string{
		"a plain zip":           craftZip(t, nil, "", map[string][]byte{"readme.txt": []byte("hi")}),
		"another program's":     craftZip(t, map[string]any{"app": "Other", "kind": kindExport}, lib, nil),
		"no library":            craftZip(t, good, "", nil),
		"a library that is not": craftZip(t, good, `[1,2,3]`, nil),
	} {
		if _, err := InspectExport(st, p); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	notZip := filepath.Join(t.TempDir(), "x.zip")
	_ = os.WriteFile(notZip, []byte("not a zip"), 0644)
	if _, err := InspectExport(st, notZip); err != errNotExport {
		t.Errorf("not a zip: %v", err)
	}

	// from a newer version: refused, and said why
	newer := good
	newer.Version = "99.0.0"
	for name, p := range map[string]string{
		"a newer program":        craftZip(t, newer, lib, nil),
		"a newer format":         craftZip(t, ExportManifest{App: core.AppName, Kind: kindExport, Format: exportFormat + 1, Version: core.AppVersion}, lib, nil),
		"a newer library schema": craftZip(t, good, `{"version":2,"assets":[]}`, nil),
	} {
		_, err := ImportLibrary(st, ImportOptions{Path: p, Mode: "replace"})
		if err == nil || !strings.Contains(err.Error(), "更新版本") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(st.User) != 0 {
		t.Fatal("something was taken in")
	}

	// names that lead out of the data folder, files that are not data: never written, the rest is taken
	p := craftZip(t, good, lib, map[string][]byte{
		"data/../evil.json":             []byte("x"),
		"data/covers/../../evil.json":   []byte("x"),
		"../evil.json":                  []byte("x"),
		"/abs/evil.json":                []byte("x"),
		`data/..\evil.json`:             []byte("x"),
		`data/C:\evil.json`:             []byte("x"),
		"data/booth-session.dat":        []byte("stolen login"),
		"data/ai-key.dat":               []byte("key"),
		"data/MioVRCA.exe":              []byte("MZ"),
		"data/library.json":             []byte("{}"),
		"data/web-login/x.json":         []byte("{}"),
		"data/covers/thumbs/x.jpg":      []byte("x"),
		"data/.hidden.json":             []byte("{}"),
		"data/covers/NUL.jpg":           []byte("x"),
		"data/sub/script.js":            []byte("x"),
		"data/covers/purchase_1.jpg":    []byte("picture"),
		"data/wishlist.json":            []byte(`{"w":1}`),
		"data/recipes/first/steps.json": []byte(`{}`),
	})
	info, err := InspectExport(st, p)
	if err != nil || info.DataFiles != 3 || info.Skipped != 15 {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	res, err := ImportLibrary(st, ImportOptions{Path: p, Mode: "replace"})
	if err != nil || res.DataFiles != 3 {
		t.Fatalf("import: %+v %v", res, err)
	}
	if core.StatOK(outside) || core.StatOK(filepath.Join(core.DataDir, "evil.json")) || core.StatOK(filepath.Join(core.DataDir, "booth-session.dat")) ||
		core.StatOK(filepath.Join(core.DataDir, "MioVRCA.exe")) || core.StatOK(filepath.Join(core.DataDir, "sub")) {
		t.Error("a file that must not be written was written")
	}
	var got []string
	for _, f := range testkit.ListTree(core.DataDir) {
		if !strings.HasPrefix(f, "import-backup-") && !strings.HasPrefix(f, "library.") && f != "transfer.json" {
			got = append(got, f)
		}
	}
	if strings.Join(got, " ") != "covers/purchase_1.jpg recipes/first/steps.json wishlist.json" {
		t.Errorf("written: %v", got)
	}
	st.Mu.RLock()
	if st.User["booth:1"] == nil || st.User["booth:1"].Notes != "n" {
		t.Error("the library was not taken in")
	}
	st.Mu.RUnlock()

	// an entry bigger than it says: stopped at the limit, nothing taken in
	big := craftZip(t, good, `{"version":1,"user":{"booth:2":{"notes":"big"}}}`, map[string][]byte{"data/huge.json": bytes.Repeat([]byte(" "), maxDataFile+10)})
	if _, err := ImportLibrary(st, ImportOptions{Path: big, Mode: "merge"}); err == nil || !strings.Contains(err.Error(), "大小限制") {
		t.Errorf("an oversized entry: %v", err)
	}
	st.Mu.RLock()
	if st.User["booth:2"] != nil {
		t.Error("taken in although refused")
	}
	st.Mu.RUnlock()
}

func TestExportLeavesOutWhatIsNotData(t *testing.T) {
	st := testkit.NewStore(t)
	// a portable copy: asset folder, project and downloads inside the program's folder
	root := filepath.Join(core.DataDir, "MyAssets")
	writeBytes(t, filepath.Join(root, "Dress", "tex.png"), []byte("png"))
	writeBytes(t, filepath.Join(root, "Dress", "notes.json"), []byte("{}"))
	writeBytes(t, filepath.Join(core.DataDir, "Proj", "ProjectSettings", "x.json"), []byte("{}"))
	_ = os.MkdirAll(filepath.Join(core.DataDir, "Proj", "Assets"), 0755)
	writeBytes(t, filepath.Join(core.DataDir, "downloads", "a.json"), []byte("{}"))
	writeBytes(t, filepath.Join(core.DataDir, "covers", "booth_1.jpg"), []byte("jpg"))
	writeBytes(t, filepath.Join(core.DataDir, "checkups", "a", "shot.png"), []byte("png"))
	writeBytes(t, filepath.Join(core.DataDir, "import-backup-20260101-000000.zip"), []byte("zip"))
	writeBytes(t, filepath.Join(core.DataDir, "library.json.broken-20260101"), []byte("{"))
	writeBytes(t, filepath.Join(core.DataDir, "updates.json"), []byte("{}"))
	writeBytes(t, filepath.Join(core.DataDir, "tidy-cache.json"), []byte("{}"))
	st.Settings.Roots = []string{root}
	got := strings.Join(dataFiles(avoidDirs(st, filepath.Join(core.DataDir, "downloads"))), " ")
	if got != "checkups/a/shot.png covers/booth_1.jpg updates.json" {
		t.Errorf("data files: %s", got)
	}
}

// What is this computer's own does not leave it: where its AI requests go, what goes by a path on it, and the
// proxy (which can carry a password).
func TestExportLeavesOutMachineData(t *testing.T) {
	st := testkit.NewStore(t)
	st.Settings.Proxy, st.Settings.DownloadDir, st.AutoDLDir = "http://me:PROXYPASS@10.0.0.1:8080", filepath.Join(t.TempDir(), "dl-here"), filepath.Join(t.TempDir(), "auto-here")
	st.Settings.Roots, st.Settings.HideZh = []string{t.TempDir()}, true
	for _, f := range []string{"ai.json", "ai-key.dat", "booth-session.dat", "gumroad-session.dat", "baidu-session.dat", "web-session.dat", "library.log",
		"booth-debug.html", "pan-debug.txt", "cloud-debug.txt", "wishlist.json", "follows.json", "updates.json", "gencovers.json", "checkups.json", "pkgcovers.json",
		"transfer.json", "tidy-cache.json", "guidcache.json", "web-login/Default/Preferences.json", "webview/EBWebView/Local State.json", "booth-profile/x.json",
		"booth-profile-firefox/cookies.json", "covers/a.jpg", "covers/generated/g.png", "covers/thumbs/a.jpg", "covers/unitypackage/p.png", "recipes/r.json",
		"reports/x.png", "vpm/vrc-official.json", "update/x.json", "shots/s.png", "import-backup-1.zip", "library.json.broken-20260101-000000", "follows.json.broken",
		"exe.stamp", "notes.txt", "covers/SHORT~1.jpg"} {
		writeBytes(t, filepath.Join(core.DataDir, filepath.FromSlash(f)), []byte("SECRET"))
	}
	exp := exportNow(t, st)
	want := "data/covers/a.jpg data/covers/generated/g.png data/follows.json data/gencovers.json data/recipes/r.json data/reports/x.png data/updates.json data/wishlist.json library.json manifest.json"
	if got := strings.Join(zipNamesOf(t, exp), " "); got != want {
		t.Errorf("in the export:\n %s\nwant\n %s", got, want)
	}
	z, err := openExport(exp)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, f := range z.zr.File {
		if b, _ := readEntry(f, maxLibrary); bytes.Contains(b, []byte("PROXYPASS")) || bytes.Contains(b, []byte("dl-here")) || bytes.Contains(b, []byte("auto-here")) {
			t.Errorf("%s holds the proxy or the download folder", f.Name)
		}
	}
	if s := z.store.Settings; s.Proxy != "" || s.DownloadDir != "" || z.store.AutoDLDir != "" || !s.HideZh || len(s.Roots) != 1 {
		t.Errorf("settings in the export: %+v, autoDlDir %q", s, z.store.AutoDLDir)
	}
	// the library that was exported is as it was
	if st.Settings.Proxy == "" || st.Settings.DownloadDir == "" || st.AutoDLDir == "" {
		t.Error("the export changed the running library")
	}
	// a short name Windows keeps for a long one never passes for a data file
	for rel, want := range map[string]bool{"WEB-LO~1/Cookies.json": false, "BOOTH-~1/x.json": false, "covers/UNITYP~1/p.png": false, "AI~1.json": false,
		"covers/a~b.jpg": true, "covers/a.jpg": true, "wishlist.json": true, "ai.json": false, "checkups.json": false, "pkgcovers.json": false, "covers/unitypackage/p.png": false} {
		if dataFileOK(rel) != want {
			t.Errorf("dataFileOK(%q) = %v", rel, !want)
		}
	}
}

// A replace takes the library from the zip, not this computer's own settings; and no path of the zip is
// looked for on this computer unless the player mapped the folder it is under.
func TestImportKeepsMachineSettings(t *testing.T) {
	st := testkit.NewStore(t)
	t.Cleanup(waitIdle)
	here := t.TempDir() // folders and a picture that exist on this computer, none of them the library's
	pic := filepath.Join(here, "private.png")
	writeBytes(t, pic, []byte("png"))
	writeBytes(t, filepath.Join(core.CoversDir(), "user_1.jpg"), []byte("jpg"))
	mine := core.Settings{Proxy: "", DownloadDir: "", NoUpdateCheck: false, WindowMode: "tab", Lang: "ja", SkipVersion: "1.7.5", NoWatch: true}
	st.Settings, st.AutoDLDir = mine, filepath.Join(here, "MioVRCdownload")
	lib, _ := json.Marshal(map[string]any{"version": 1, "autoDlDir": here,
		"settings": map[string]any{"roots": []string{`\\evil.example\share`, "//evil.example/share2", here}, "proxy": "http://203.0.113.9:8080", "downloadDir": here, "noUpdateCheck": true,
			"windowMode": "", "lang": "en", "skipVersion": "9.9.9", "hideZh": true, "styles": []string{"甜美=sweet"}, "noWatch": true},
		"user": map[string]any{
			"booth:1":  map[string]any{"notes": "n1", "cover": pic},                                    // a picture of this computer the zip names
			"booth:2":  map[string]any{"notes": "n2", "cover": `\\evil.example\share\c.png`},           // … of another computer
			"booth:3":  map[string]any{"notes": "n3", "cover": `C:\Users\x\AppData\covers\user_1.jpg`}, // the data folder's own: by its name
			"pan:1Abc": map[string]any{"shareUrl": "https://pan.baidu.com/s/1Abc", "downloaded": here, "downloadDir": `\\evil.example\share\half`},
		},
		"downloaded": map[string]any{"1": map[string]any{"item": "1", "path": here}, "2": map[string]any{"item": "2", "path": `\\evil.example\share\x`}}})
	zp := craftZip(t, ExportManifest{App: core.AppName, Kind: kindExport, Format: exportFormat, Version: core.AppVersion}, string(lib), nil)

	info, err := InspectExport(st, zp)
	if err != nil || len(info.Roots) != 3 {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	// ("//evil.example/share2" is a folder that exists on a system where "//" is "/": it is still not looked at)
	for _, r := range info.Roots[:2] {
		if r.Exists || r.Suggest != "" {
			t.Errorf("a path on another computer was looked at: %+v", r)
		}
	}
	if !uncPath(`\\host\share`) || !uncPath("//host/share") || uncPath(`D:\x`) || uncPath("/mnt/x") {
		t.Error("uncPath")
	}
	if _, err := ImportLibrary(st, ImportOptions{Path: zp, Mode: "replace"}); err != nil { // (no folder mapped)
		t.Fatal(err)
	}
	check := func(when string) {
		t.Helper()
		st.Mu.RLock()
		defer st.Mu.RUnlock()
		s := st.Settings
		if s.Proxy != mine.Proxy || s.DownloadDir != mine.DownloadDir || s.NoUpdateCheck != mine.NoUpdateCheck || s.WindowMode != mine.WindowMode || s.Lang != mine.Lang ||
			s.SkipVersion != mine.SkipVersion || st.AutoDLDir != filepath.Join(here, "MioVRCdownload") {
			t.Errorf("%s: this computer's settings changed: %+v, autoDlDir %q", when, s, st.AutoDLDir)
		}
	}
	check("after the replace")
	st.Mu.RLock()
	if !st.Settings.HideZh || len(st.Settings.Styles) != 1 || len(st.Settings.Roots) != 0 || !st.Settings.SetupDone {
		t.Errorf("the library's own settings did not come: %+v", st.Settings)
	}
	u := st.User
	if u["booth:1"] == nil || u["booth:1"].Notes != "n1" || u["booth:1"].Cover != "" || u["booth:2"].Cover != "" || u["booth:3"].Cover != filepath.Join(core.CoversDir(), "user_1.jpg") {
		t.Errorf("covers: %q %q %q", u["booth:1"].Cover, u["booth:2"].Cover, u["booth:3"].Cover)
	}
	// "downloaded to <a folder that exists here>": only under a folder the player mapped
	if p := u["pan:1Abc"]; p == nil || p.Downloaded != "" || p.DownloadDir != "" || len(st.Downloaded) != 0 {
		t.Errorf("download places taken from the zip: %+v %v", p, st.Downloaded)
	}
	st.Mu.RUnlock()
	st.Mu.Lock()
	st.Settings.Proxy, mine.Proxy = "http://127.0.0.1:7890", "http://127.0.0.1:7890" // changed after the import: an undo is not what changes it back
	st.Mu.Unlock()
	waitIdle()
	if err := UndoImport(st); err != nil {
		t.Fatal(err)
	}
	check("after the undo")
	st.Mu.RLock()
	if st.Settings.HideZh || len(st.User) != 0 {
		t.Errorf("the undo did not bring the library back: %+v", st.Settings)
	}
	st.Mu.RUnlock()
}

// A zip that is damaged is refused before anything is written; an import that fails while it writes puts the
// data files back by itself, and where it cannot, the undo is there.
func TestImportThatFails(t *testing.T) {
	st := testkit.NewStore(t)
	st.Settings.NoWatch = true
	t.Cleanup(waitIdle)
	reloads := 0
	n := len(dataReloaders)
	OnDataImported(func() { reloads++ })
	defer func() { dataReloaders = dataReloaders[:n]; writeDataFile = writeFileAtomic }()
	const mineW, mineF = `{"ver":1,"items":["mine"]}`, `{"ver":1,"shops":["mine"]}`
	reset := func() {
		writeBytes(t, filepath.Join(core.DataDir, "follows.json"), []byte(mineF))
		writeBytes(t, filepath.Join(core.DataDir, "wishlist.json"), []byte(mineW))
		_ = os.RemoveAll(filepath.Join(core.DataDir, "recipes"))
		_ = os.Remove(filepath.Join(core.DataDir, "aaa-new.json"))
	}
	intact := func(when string) {
		t.Helper()
		f, _ := os.ReadFile(filepath.Join(core.DataDir, "follows.json"))
		w, _ := os.ReadFile(filepath.Join(core.DataDir, "wishlist.json"))
		if string(f) != mineF || string(w) != mineW || core.StatOK(filepath.Join(core.DataDir, "recipes")) || core.StatOK(filepath.Join(core.DataDir, "aaa-new.json")) {
			t.Errorf("%s: the data folder is not as it was: %v\n%s\n%s", when, testkit.ListTree(core.DataDir), f, w)
		}
	}
	backups := func() int { return len(TransferState().Backups) }
	good := ExportManifest{App: core.AppName, Kind: kindExport, Format: exportFormat, Version: core.AppVersion}
	lib := `{"version":1,"settings":{"roots":[]},"user":{"booth:1":{"notes":"theirs"}}}`
	files := map[string][]byte{"data/aaa-new.json": []byte(`{"new":1}`), "data/follows.json": []byte(`{"ver":1,"shops":["theirs"]}`),
		"data/recipes/r1.json": []byte(`{}`), "data/wishlist.json": []byte(`{"ver":1,"items":["theirs"],"pad":"DAMAGED-HERE-DAMAGED-HERE"}`)}

	// 1. one byte of the zip is wrong (a bad USB stick, a broken transfer): refused, nothing written, no backup made
	reset()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mb, _ := json.Marshal(good)
	for _, e := range [][2]string{{"manifest.json", string(mb)}, {"library.json", lib}, {"data/follows.json", string(files["data/follows.json"])}, {"data/wishlist.json", string(files["data/wishlist.json"])}} {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: e[0], Method: zip.Store})
		_, _ = w.Write([]byte(e[1]))
	}
	_ = zw.Close()
	b := buf.Bytes()
	b[bytes.Index(b, []byte("DAMAGED-HERE"))] ^= 0xff
	bad := filepath.Join(t.TempDir(), "damaged.zip")
	writeBytes(t, bad, b)
	if _, err := ImportLibrary(st, ImportOptions{Path: bad, Mode: "replace"}); err == nil || !strings.Contains(err.Error(), "wishlist.json") {
		t.Fatalf("a damaged zip: %v", err)
	}
	intact("a damaged zip")
	if tv := TransferState(); tv.Undo != nil || backups() != 0 || len(st.User) != 0 || tv.Run.Running {
		t.Errorf("a damaged zip left something: %+v", tv)
	}

	// 2. the disk refuses a file half way (full, or a scanner holding it): what was written is put back
	zp := craftZip(t, good, lib, files)
	calls := 0
	writeDataFile = func(file string, b []byte) error {
		if calls++; calls == 3 { // aaa-new.json and follows.json are written, recipes/r1.json is not
			return errors.New("disk full")
		}
		return writeFileAtomic(file, b)
	}
	_, err := ImportLibrary(st, ImportOptions{Path: zp, Mode: "replace"})
	if err == nil || !strings.Contains(err.Error(), "recipes/r1.json") || !strings.Contains(err.Error(), "已恢复为导入前的状态") {
		t.Fatalf("a write that failed: %v", err)
	}
	intact("a write that failed")
	if tv := TransferState(); tv.Undo != nil || backups() != 1 || len(st.User) != 0 || reloads != 1 || tv.Run.Running || tv.Run.Err == "" {
		t.Errorf("after a write that failed: %+v, %d reloads, user %v", tv, reloads, st.User)
	}

	// 3. … and putting it back fails too: the undo is offered, and does it
	calls = 0
	writeDataFile = func(file string, b []byte) error {
		if calls++; calls >= 3 {
			return errors.New("disk full")
		}
		return writeFileAtomic(file, b)
	}
	_, err = ImportLibrary(st, ImportOptions{Path: zp, Mode: "replace"})
	if err == nil || !strings.Contains(err.Error(), "撤销上次导入") {
		t.Fatalf("a restore that failed: %v", err)
	}
	if f, _ := os.ReadFile(filepath.Join(core.DataDir, "follows.json")); string(f) == mineF {
		t.Fatal("the test did not get the data folder half written")
	}
	tv := TransferState()
	if tv.Undo == nil || reloads != 2 || len(st.User) != 0 {
		t.Fatalf("no undo after a failed restore: %+v, %d reloads", tv, reloads)
	}
	writeDataFile = writeFileAtomic
	waitIdle()
	if err := UndoImport(st); err != nil {
		t.Fatal(err)
	}
	waitIdle()
	intact("the undo of a half-written import")
	if TransferState().Undo != nil || reloads != 3 {
		t.Errorf("after the undo: %+v, %d reloads", TransferState(), reloads)
	}

	// 4. an import that goes well, then its undo: what it created is gone again
	if _, err := ImportLibrary(st, ImportOptions{Path: zp, Mode: "replace"}); err != nil {
		t.Fatal(err)
	}
	if f, _ := os.ReadFile(filepath.Join(core.DataDir, "follows.json")); string(f) != string(files["data/follows.json"]) || !core.StatOK(filepath.Join(core.DataDir, "recipes", "r1.json")) ||
		!core.StatOK(filepath.Join(core.DataDir, "aaa-new.json")) || st.User["booth:1"] == nil {
		t.Fatalf("the import: %v", testkit.ListTree(core.DataDir))
	}
	waitIdle()
	if err := UndoImport(st); err != nil {
		t.Fatal(err)
	}
	intact("the undo")
	if len(st.User) != 0 {
		t.Error("the undo left the library")
	}
}

// A save of a data file that comes in while an import replaces it waits, and then works on the file that was
// taken in — not on what it had in memory before.
func TestImportHoldsDataFiles(t *testing.T) {
	st := testkit.NewStore(t)
	st.Settings.NoWatch = true
	t.Cleanup(waitIdle)
	defer func() { writeDataFile = writeFileAtomic }()
	writeBytes(t, updFile(), []byte(`{"ver":{"old-asset":"1.0"}}`))
	upd.mu.Lock()
	upd.s = nil
	if updLoadLocked().Ver["old-asset"] != "1.0" { // in memory, as after any look at the updates
		t.Fatal("not loaded")
	}
	upd.mu.Unlock()
	saved := make(chan bool, 1)
	started := false
	writeDataFile = func(file string, b []byte) error {
		if !started { // while the import writes: a background save of updates.json
			started = true
			begun := make(chan bool)
			go func() {
				close(begun)
				upd.mu.Lock()
				s := updLoadLocked()
				s.Since["saved-meanwhile"] = 7
				upd.dirty = true
				updSaveLocked()
				upd.mu.Unlock()
				saved <- true
			}()
			<-begun
			time.Sleep(30 * time.Millisecond)
		}
		return writeFileAtomic(file, b)
	}
	zp := craftZip(t, ExportManifest{App: core.AppName, Kind: kindExport, Format: exportFormat, Version: core.AppVersion}, `{"version":1,"settings":{"roots":[]}}`,
		map[string][]byte{"data/updates.json": []byte(`{"ver":{"imported-asset":"2.0"}}`), "data/zzz.json": []byte(`{}`)})
	if _, err := ImportLibrary(st, ImportOptions{Path: zp, Mode: "replace"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-saved:
	case <-time.After(5 * time.Second):
		t.Fatal("the save never ran")
	}
	b, _ := os.ReadFile(updFile())
	if !bytes.Contains(b, []byte("imported-asset")) || bytes.Contains(b, []byte("old-asset")) || !bytes.Contains(b, []byte("saved-meanwhile")) {
		t.Errorf("updates.json after the import and the save: %s", b)
	}
}
