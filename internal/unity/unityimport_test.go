package unity

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
)

type pkgFile struct {
	guid, path, body string
	dir              bool
}

func makeUnityPackage(t *testing.T, out string, files []pkgFile) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	put := func(name string, b []byte) {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(b)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(b)
	}
	for _, f := range files {
		put("./"+f.guid+"/pathname", []byte(f.path+"\n00"))
		put("./"+f.guid+"/asset.meta", []byte("fileFormatVersion: 2\nguid: "+f.guid+"\n"))
		if !f.dir {
			put("./"+f.guid+"/asset", []byte(f.body))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(out, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func newProject(t *testing.T) string {
	p := t.TempDir()
	for _, d := range []string{"Assets", "ProjectSettings", "Packages"} {
		_ = os.MkdirAll(filepath.Join(p, d), 0755)
	}
	return p
}

func g(n string) string { return strings.Repeat(n, 32)[:32] }

func TestImportUnityPackage(t *testing.T) {
	proj := newProject(t)
	pkg := filepath.Join(t.TempDir(), "Dress.unitypackage")
	makeUnityPackage(t, pkg, []pkgFile{
		{guid: g("a"), path: "Assets/Dress", dir: true},
		{guid: g("b"), path: "Assets/Dress/Dress.prefab", body: "prefab v1"},
		{guid: g("c"), path: "Assets/Dress/Tex/body.png", body: "png"},
		{guid: g("d"), path: "../../evil.txt", body: "x"},
		{guid: g("e"), path: "C:/evil.txt", body: "x"},
		{guid: g("f"), path: "ProjectSettings/x.asset", body: "x"},
	})
	idx := indexProject(proj)
	st, err := importUnityPackage(pkg, idx)
	if err != nil {
		t.Fatal(err)
	}
	if st.New != 2 || st.Skipped != 3 || st.Folders != 1 {
		t.Errorf("stats %+v", st)
	}
	b, _ := os.ReadFile(filepath.Join(proj, "Assets/Dress/Dress.prefab"))
	if string(b) != "prefab v1" {
		t.Errorf("prefab %q", b)
	}
	if library.ReadMetaGUID(filepath.Join(proj, "Assets/Dress/Tex/body.png.meta")) != g("c") {
		t.Error("meta")
	}
	if library.ReadMetaGUID(filepath.Join(proj, "Assets/Dress.meta")) != g("a") {
		t.Error("folder meta")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(proj), "evil.txt")); err == nil {
		t.Error("wrote outside the project")
	}

	// the player moved the prefab; a newer package updates it where it is now
	_ = os.MkdirAll(filepath.Join(proj, "Assets/Mine"), 0755)
	_ = os.Rename(filepath.Join(proj, "Assets/Dress/Dress.prefab"), filepath.Join(proj, "Assets/Mine/Dress.prefab"))
	_ = os.Rename(filepath.Join(proj, "Assets/Dress/Dress.prefab.meta"), filepath.Join(proj, "Assets/Mine/Dress.prefab.meta"))
	// another asset sits where the package's new file wants to go
	_ = os.WriteFile(filepath.Join(proj, "Assets/Dress/Extra.mat"), []byte("theirs"), 0644)
	_ = os.WriteFile(filepath.Join(proj, "Assets/Dress/Extra.mat.meta"), []byte("guid: "+g("9")+"\n"), 0644)
	pkg2 := filepath.Join(t.TempDir(), "Dress_v2.unitypackage")
	makeUnityPackage(t, pkg2, []pkgFile{
		{guid: g("b"), path: "Assets/Dress/Dress.prefab", body: "prefab v2"},
		{guid: g("7"), path: "Assets/Dress/Extra.mat", body: "ours"},
	})
	st2, err := importUnityPackage(pkg2, indexProject(proj))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Assets/Mine/Dress.prefab")); string(b) != "prefab v2" || st2.Updated != 1 {
		t.Errorf("moved prefab not updated: %q %+v", b, st2)
	}
	if _, err := os.Stat(filepath.Join(proj, "Assets/Dress/Dress.prefab")); err == nil {
		t.Error("a second copy was made")
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Assets/Dress/Extra.mat")); string(b) != "theirs" {
		t.Error("overwrote another asset")
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Assets/Dress/Extra 1.mat")); string(b) != "ours" {
		t.Error("conflicting name not numbered")
	}
}

func TestChoosePackages(t *testing.T) {
	st := &core.Store{Settings: core.Settings{Bases: []string{"Kaguya=kaguya|カグヤ", "Plum=plum|プラム", "Chocolat=chocolat"}}}
	pkgs := []string{"/x/Dress_Kaguya.unitypackage", "/x/Dress_Plum.unitypackage", "/x/Dress_Common.unitypackage"}
	c, ask := choosePackages(st, pkgs, []string{"Plum"}, nil)
	if ask || !c[1].Pick || c[0].Pick || !c[2].Pick {
		t.Errorf("project on Plum: %+v ask=%v", c, ask)
	}
	_, ask = choosePackages(st, pkgs, nil, []string{"Kaguya", "Plum"})
	if !ask {
		t.Error("two bases, no project base: should ask")
	}
	c, ask = choosePackages(st, pkgs, nil, []string{"Kaguya"})
	if ask || !c[0].Pick || c[1].Pick {
		t.Errorf("card for Kaguya: %+v %v", c, ask)
	}
	c, ask = choosePackages(st, []string{"/x/Hair.unitypackage"}, []string{"Plum"}, nil)
	if ask || !c[0].Pick {
		t.Errorf("single package: %+v", c)
	}
}

// a unitypackage that bundles files of a package installed in Packages/ (same GUIDs, as a shop's copy of lilToon
// has) leaves the installed package alone, and the result says so
func TestImportKeepsInstalledPackages(t *testing.T) {
	proj := newProject(t)
	lil := filepath.Join(proj, "Packages", "jp.lilxyzw.liltoon", "Shader")
	_ = os.MkdirAll(lil, 0755)
	meta := "fileFormatVersion: 2\nguid: " + g("d") + "\nShaderImporter:\n  new: 1\n"
	_ = os.WriteFile(filepath.Join(lil, "lts.shader"), []byte("lilToon 2.3 (VPM)"), 0644)
	_ = os.WriteFile(filepath.Join(lil, "lts.shader.meta"), []byte(meta), 0644)
	_ = os.WriteFile(filepath.Join(proj, "Packages", "jp.lilxyzw.liltoon", "Shader.meta"), []byte("fileFormatVersion: 2\nguid: "+g("9")+"\nfolderAsset: yes\n"), 0644)
	pkg := filepath.Join(t.TempDir(), "Dress.unitypackage")
	makeUnityPackage(t, pkg, []pkgFile{
		{guid: g("b"), path: "Assets/Dress/Dress.prefab", body: "prefab"},
		{guid: g("9"), path: "Assets/lilToon/Shader", dir: true},
		{guid: g("d"), path: "Assets/lilToon/Shader/lts.shader", body: "lilToon 1.3 (bundled by the shop)"},
		{guid: g("e"), path: "Packages/com.example.tool/Editor/x.cs", body: "// a package of the unitypackage's own"},
	})
	stats, err := importUnityPackage(pkg, indexProject(proj))
	if err != nil {
		t.Fatal(err)
	}
	if stats.New != 1 || stats.Updated != 0 || stats.Kept != 2 || !reflect.DeepEqual(stats.KeptPkgs, []string{"com.example.tool", "jp.lilxyzw.liltoon"}) {
		t.Errorf("stats %+v", stats)
	}
	if b, _ := os.ReadFile(filepath.Join(lil, "lts.shader")); string(b) != "lilToon 2.3 (VPM)" {
		t.Errorf("the installed shader was replaced: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(lil, "lts.shader.meta")); string(b) != meta {
		t.Errorf("its meta was rewritten: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Packages", "jp.lilxyzw.liltoon", "Shader.meta")); !strings.Contains(string(b), "folderAsset") {
		t.Errorf("the folder's meta was rewritten: %q", b)
	}
	if core.StatOK(filepath.Join(proj, "Packages", "com.example.tool")) || core.StatOK(filepath.Join(proj, "Assets", "lilToon", "Shader", "lts.shader")) {
		t.Error("a file went where it should not")
	}
	if !reflect.DeepEqual(stats.Tops, []string{"Assets/Dress"}) {
		t.Errorf("tops %v", stats.Tops)
	}
	if n := keptNote(stats.Kept, stats.KeptPkgs); n != "已跳过 2 个属于已安装插件（Packages）的文件，未覆盖：com.example.tool、jp.lilxyzw.liltoon" || keptNote(0, nil) != "" {
		t.Errorf("note %q", n)
	}
}

// A file the player put into Assets while Unity was closed has no .meta yet: the package's file of that path goes
// next to it under a number, as in Unity (which gives the player's file a .meta when the project opens). So does
// one where a folder of that name is.
func TestImportNumbersFileWithoutMeta(t *testing.T) {
	proj := newProject(t)
	mine := filepath.Join(proj, "Assets", "MyAvatar", "Body.fbx")
	put(t, mine, []byte("the player's own model"))
	_ = os.MkdirAll(filepath.Join(proj, "Assets", "MyAvatar", "Tex.png"), 0755)
	pkg := filepath.Join(t.TempDir(), "Outfit.unitypackage")
	makeUnityPackage(t, pkg, []pkgFile{
		{guid: g("b"), path: "Assets/MyAvatar/Body.fbx", body: "the package's model"},
		{guid: g("c"), path: "Assets/MyAvatar/Tex.png", body: "png"},
	})
	stats, err := importUnityPackage(pkg, indexProject(proj))
	if err != nil {
		t.Fatal(err)
	}
	if stats.New != 2 || stats.Updated != 0 {
		t.Errorf("stats %+v", stats)
	}
	if b, _ := os.ReadFile(mine); string(b) != "the player's own model" || fileThere(mine+".meta") {
		t.Errorf("the player's file: %q", b)
	}
	body1 := filepath.Join(proj, "Assets", "MyAvatar", "Body 1.fbx")
	if b, _ := os.ReadFile(body1); string(b) != "the package's model" || library.ReadMetaGUID(body1+".meta") != g("b") {
		t.Errorf("the package's file: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Assets", "MyAvatar", "Tex 1.png")); string(b) != "png" {
		t.Errorf("next to the folder: %q", b)
	}
	// a newer version updates the package's file where it went, and leaves the player's alone
	makeUnityPackage(t, pkg, []pkgFile{{guid: g("b"), path: "Assets/MyAvatar/Body.fbx", body: "v2"}})
	if stats, err := importUnityPackage(pkg, indexProject(proj)); err != nil || stats.Updated != 1 || stats.New != 0 {
		t.Errorf("again: %+v %v", stats, err)
	}
	if b, _ := os.ReadFile(body1); string(b) != "v2" {
		t.Errorf("the package's file, again: %q", b)
	}
	if b, _ := os.ReadFile(mine); string(b) != "the player's own model" {
		t.Errorf("the player's file, again: %q", b)
	}
}

// An entry for the Assets folder itself: no .meta is written for it (it would be outside Assets, in the project's
// own folder), and none of a file of that name either. What is in Assets is imported as ever.
func TestImportAssetsFolderEntry(t *testing.T) {
	proj := newProject(t)
	pkg := filepath.Join(t.TempDir(), "X.unitypackage")
	makeUnityPackage(t, pkg, []pkgFile{
		{guid: g("a"), path: "Assets", dir: true},
		{guid: g("e"), path: "Assets/", body: "x"},
		{guid: g("c"), path: "Assets/X", dir: true},
		{guid: g("b"), path: "Assets/X/x.txt", body: "x"},
	})
	stats, err := importUnityPackage(pkg, indexProject(proj))
	if err != nil {
		t.Fatal(err)
	}
	if stats.New != 1 || stats.Folders != 1 || stats.Skipped != 2 {
		t.Errorf("stats %+v", stats)
	}
	if got := strings.Join(testkit.ListTree(proj), ","); got != "Assets/X.meta,Assets/X/x.txt,Assets/X/x.txt.meta" {
		t.Errorf("in the project: %s", got)
	}
	if library.ReadMetaGUID(filepath.Join(proj, "Assets", "X.meta")) != g("c") {
		t.Error("folder meta")
	}
}

// Two entries of a package with one path and other GUIDs: the second is numbered, as in Unity, instead of writing
// over the first — on Windows, also where the two differ in case. The same goes for the next package of the import.
func TestImportSamePathTwice(t *testing.T) {
	proj := newProject(t)
	pkg := filepath.Join(t.TempDir(), "Y.unitypackage")
	makeUnityPackage(t, pkg, []pkgFile{
		{guid: g("1"), path: "Assets/Y/mat.mat", body: "first"},
		{guid: g("2"), path: "Assets/Y/mat.mat", body: "second"},
		{guid: g("3"), path: "Assets/Y/Mat.mat", body: "third"},
	})
	idx := indexProject(proj)
	if stats, err := importUnityPackage(pkg, idx); err != nil || stats.New != 3 || stats.Updated != 0 {
		t.Fatalf("stats %+v %v", stats, err)
	}
	next := filepath.Join(t.TempDir(), "Z.unitypackage")
	makeUnityPackage(t, next, []pkgFile{{guid: g("4"), path: "Assets/Y/mat.mat", body: "fourth"}})
	if stats, err := importUnityPackage(next, idx); err != nil || stats.New != 1 || stats.Updated != 0 {
		t.Fatalf("the next package: %+v %v", stats, err)
	}
	for _, w := range []struct{ name, body, guid string }{
		{"mat.mat", "first", g("1")}, {"mat 1.mat", "second", g("2")}, {"Mat 2.mat", "third", g("3")}, {"mat 3.mat", "fourth", g("4")},
	} {
		f := filepath.Join(proj, "Assets", "Y", w.name)
		if b, _ := os.ReadFile(f); string(b) != w.body || library.ReadMetaGUID(f+".meta") != w.guid {
			t.Errorf("%s: %q, guid %s", w.name, b, library.ReadMetaGUID(f+".meta"))
		}
	}
}

// Names Windows takes for another file's: with a dot or a space at the end ("Avatar.prefab." is opened as
// "Avatar.prefab"), and an 8.3 short name ("AVATAR~1.PRE"). Unity makes none of them: such an entry is left out.
func TestCleanPkgPathWindowsNames(t *testing.T) {
	for p, want := range map[string]string{
		"Assets/Avatar.prefab.":         "",
		"Assets/Dress./Dress.prefab":    "",
		"Assets/Dress /Dress.prefab":    "",
		"Assets/AVATAR~1.PRE":           "",
		"Assets/avata~12.pre":           "",
		"Assets/MYAVAT~1/Body.fbx":      "",
		"Assets/衣装~1/Body.fbx":          "",
		"Packages/com.x.y/EDITOR~1/a":   "",
		"Assets":                        "",
		"Assets/":                       "",
		"Assets/Avatar.prefab ":         "Assets/Avatar.prefab", // (the line is trimmed)
		"Assets/Hair~1.prefab":          "Assets/Hair~1.prefab", // no 8.3 name: its extension is longer
		"Assets/LongFolderName~1/a.png": "Assets/LongFolderName~1/a.png",
		"Assets/Hair~Long.fbx":          "Assets/Hair~Long.fbx",
		"Assets/Dress.v1.2/a.png":       "Assets/Dress.v1.2/a.png",
		"Assets/.../a.png":              "",
	} {
		if got := cleanPkgPath(p); got != want {
			t.Errorf("%q: %q, want %q", p, got, want)
		}
	}
	proj := newProject(t)
	put(t, filepath.Join(proj, "Assets", "Avatar.prefab"), []byte("mine"))
	put(t, filepath.Join(proj, "Assets", "Avatar.prefab.meta"), []byte("fileFormatVersion: 2\nguid: "+g("9")+"\n"))
	pkg := filepath.Join(t.TempDir(), "W.unitypackage")
	makeUnityPackage(t, pkg, []pkgFile{{guid: g("c"), path: "Assets/Avatar.prefab.", body: "theirs"}, {guid: g("d"), path: "Assets/AVATAR~1.PRE", body: "theirs"}})
	if stats, err := importUnityPackage(pkg, indexProject(proj)); err != nil || stats.Skipped != 2 || stats.New+stats.Updated != 0 {
		t.Errorf("stats %+v %v", stats, err)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "Assets", "Avatar.prefab")); string(b) != "mine" {
		t.Errorf("the player's prefab: %q", b)
	}
}

// An import that follows a download and cannot begin (here: the folder is no project) while another one waits
// for the player's choice: the one under way keeps its job — the choice reaches it — and why the other could not
// begin is said once it has ended.
func TestImportUnderWayIsNotReplaced(t *testing.T) {
	st, _ := bundleLibrary(t)
	proj := newProject(t)
	both := t.TempDir()
	makeUnityPackage(t, filepath.Join(both, "Dress_Plum.unitypackage"), []pkgFile{{guid: g("c"), path: "Assets/P.prefab", body: "p"}})
	makeUnityPackage(t, filepath.Join(both, "Dress_Chocolat.unitypackage"), []pkgFile{{guid: g("d"), path: "Assets/C.prefab", body: "c"}})
	wait := func(what string, ok func(j *ImportJob) bool) *ImportJob {
		t.Helper()
		for i := 0; i < 1000; i++ {
			if j := ImportSnapshot(); j != nil && ok(j) {
				return j
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s: %+v", what, ImportSnapshot())
		return nil
	}
	if err := StartImport(st, ImportReq{Paths: []string{both}, Project: proj}); err != nil {
		t.Fatal(err)
	}
	w := wait("not asked", func(j *ImportJob) bool { return j.Stage == "choose" })
	StartImportWhenFree(st, ImportReq{Key: "purchase:1", Paths: []string{both}, Project: t.TempDir()})
	time.Sleep(200 * time.Millisecond)
	if j := ImportSnapshot(); j == nil || j.ID != w.ID || j.Stage != "choose" {
		t.Fatalf("the import under way was replaced: %+v", j)
	}
	if err := StartImport(st, ImportReq{Paths: []string{both}, Project: proj}); err != errImportBusy {
		t.Errorf("a second import beside it: %v", err)
	}
	if !ChooseImport([]string{filepath.Join(both, "Dress_Plum.unitypackage")}) {
		t.Fatal("the choice was not taken")
	}
	wait("the import under way did not end", func(j *ImportJob) bool { return j.ID != w.ID || j.Stage == "done" || j.Stage == "failed" })
	if !fileThere(filepath.Join(proj, "Assets", "P.prefab")) || fileThere(filepath.Join(proj, "Assets", "C.prefab")) {
		t.Errorf("in the project: %s", imported(proj))
	}
	f := wait("not said why the other did not begin", func(j *ImportJob) bool { return j.ID != w.ID })
	if f.Stage != "failed" || f.Key != "purchase:1" || f.Err != "该文件夹不是 Unity 工程（未找到 Assets 和 ProjectSettings 文件夹）" {
		t.Errorf("the one that did not begin: %+v", f)
	}
}

// A run that goes wrong ends its job as failed, with what happened, and the next import is not refused as busy.
// (No library to look into: the run panics.)
func TestImportThatPanicsEnds(t *testing.T) {
	st, _ := bundleLibrary(t)
	proj := newProject(t)
	pkg := filepath.Join(t.TempDir(), "Hair.unitypackage")
	makeUnityPackage(t, pkg, []pkgFile{{guid: g("a"), path: "Assets/Hair/Hair.prefab", body: "hair"}})
	if err := StartImport(nil, ImportReq{Paths: []string{pkg}, Project: proj}); err != nil {
		t.Fatal(err)
	}
	j := ended(t)
	if j.Stage != "failed" || !strings.HasPrefix(j.Err, "程序内部错误：") || j.Msg != j.Err || TaskImport.Snapshot().Msg != j.Err {
		t.Fatalf("the job: %+v; the task's line: %s", j, TaskImport.Snapshot().Msg)
	}
	if err := StartImport(st, ImportReq{Paths: []string{pkg}, Project: proj}); err != nil {
		t.Fatalf("the next import: %v", err)
	}
	if j := ended(t); j.Stage != "done" || j.Files != 1 || imported(proj) != "Hair/Hair.prefab" {
		t.Fatalf("the next import: %+v; in the project: %s", j, imported(proj))
	}
}

// The folders an unpack works in (archive: ".x.extracting", ".x.joining", with or without a number of its own)
// are passed over: what is in them is not finished.
func TestFindPackagesSkipsUnpacksUnderWay(t *testing.T) {
	d := t.TempDir()
	for _, p := range []string{"Dress/Dress.unitypackage", ".Hair.extracting/Hair.unitypackage", ".Hair.1a2b3c4d.extracting/Hair/Hair.unitypackage",
		".Coat.joining/Coat.unitypackage", ".Coat.9f8e7d6c.joining/out/Coat.unitypackage"} {
		put(t, filepath.Join(d, filepath.FromSlash(p)), []byte("x"))
	}
	if got := findPackages([]string{d}); len(got) != 1 || got[0] != filepath.Join(d, "Dress", "Dress.unitypackage") {
		t.Errorf("found %v", got)
	}
}

// The files an import is given (what a download brought) are unpacked and nothing else: not the other archives
// next to them, nor a folder of their name that was there before — what the player keeps packed in it stays
// packed and is not recycled. A folder it is given is gone through as ever.
func TestImportUnpacksOnlyTheFilesItIsGiven(t *testing.T) {
	st, _ := bundleLibrary(t)
	item, other := t.TempDir(), t.TempDir()
	put(t, filepath.Join(item, "Hair.zip"), productZip(t, "Hair"))
	put(t, filepath.Join(item, "Hair_old.zip"), productZip(t, "Hair_old")) // an earlier version, kept packed
	put(t, filepath.Join(other, "Coat.zip"), productZip(t, "Coat"))
	proj := newProject(t)
	if err := StartImport(st, ImportReq{Paths: []string{filepath.Join(item, "Hair.zip"), other}, Project: proj, Recycle: true}); err != nil {
		t.Fatal(err)
	}
	j := ended(t)
	if j.Stage != "done" || strings.Join(j.Imported, ",") != "Coat.unitypackage,Hair.unitypackage" || j.Unpacked != 2 || j.Removed != 2 || len(j.Failed) > 0 {
		t.Fatalf("the job: %+v", j)
	}
	if got := strings.Join(testkit.ListTree(item), ","); got != "Hair/Hair.unitypackage,Hair/readme.txt,Hair_old.zip" {
		t.Errorf("next to the download: %s", got)
	}
	// downloaded again where the earlier one was unpacked: that folder is not gone through
	put(t, filepath.Join(item, "Dress", "Dress.unitypackage"), packageOf(t, "Dress"))
	put(t, filepath.Join(item, "Dress", "Dress_PSD.zip"), testkit.ZipBytes(t, map[string][]byte{"body.psd": []byte("psd")}))
	put(t, filepath.Join(item, "Dress.zip"), productZip(t, "Dress"))
	if err := StartImport(st, ImportReq{Paths: []string{filepath.Join(item, "Dress.zip")}, Project: proj, Recycle: true}); err != nil {
		t.Fatal(err)
	}
	ended(t)
	if got := strings.Join(testkit.ListTree(filepath.Join(item, "Dress")), ","); got != "Dress.unitypackage,Dress_PSD.zip" {
		t.Errorf("the earlier folder: %s", got)
	}
}
