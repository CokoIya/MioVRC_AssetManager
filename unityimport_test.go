package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
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
	if readMetaGUID(filepath.Join(proj, "Assets/Dress/Tex/body.png.meta")) != g("c") {
		t.Error("meta")
	}
	if readMetaGUID(filepath.Join(proj, "Assets/Dress.meta")) != g("a") {
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

func TestGroupArchives(t *testing.T) {
	d := t.TempDir()
	names := []string{"A.zip", "B.part1.rar", "B.part2.rar", "C.7z.001", "C.7z.002", "D.zip", "D.z01", "E.part2.rar", "F.rar", "F.r00"}
	var files []string
	for _, n := range names {
		p := filepath.Join(d, n)
		_ = os.WriteFile(p, []byte("x"), 0644)
		files = append(files, p)
	}
	var got []string
	for _, s := range groupArchives(files) {
		got = append(got, filepath.Base(s.Main)+"/"+s.Name+"/"+string(rune('0'+len(s.Parts))))
	}
	sort.Strings(got)
	want := "A.zip/A/1,B.part1.rar/B/2,C.7z.001/C/2,D.zip/D/2,F.rar/F/2"
	if strings.Join(got, ",") != want {
		t.Errorf("got %s", strings.Join(got, ","))
	}
}

func zipBytes(t *testing.T, files map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, _ := zw.Create(n)
		_, _ = w.Write(files[n])
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestUnpackAll(t *testing.T) {
	root := t.TempDir()
	asset := filepath.Join(root, "Kaguya Dress")
	_ = os.MkdirAll(asset, 0755)
	// the main zip holds the package folder and a PSD zip
	psd := zipBytes(t, map[string][]byte{"PSD/body.psd": []byte("psd")})
	main := zipBytes(t, map[string][]byte{"Dress/Dress_Kaguya.unitypackage": []byte("pkg"), "Dress/Dress_PSD.zip": psd})
	_ = os.WriteFile(filepath.Join(asset, "Dress_v1.zip"), main, 0644)
	res := unpackAll([]string{asset}, "", recycleFiles, nil)
	if len(res.Failed) > 0 {
		t.Fatalf("failed: %v", res.Failed)
	}
	for _, p := range []string{"Dress/Dress_Kaguya.unitypackage", "Dress/PSD/body.psd"} {
		if _, err := os.Stat(filepath.Join(asset, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range []string{"Dress_v1.zip", "Dress/Dress_PSD.zip"} {
		if _, err := os.Stat(filepath.Join(asset, p)); err == nil {
			t.Errorf("archive kept: %s", p)
		}
	}
	if res.Removed != 2 {
		t.Errorf("removed %d", res.Removed)
	}
	// unpacked by hand before: left alone
	_ = os.WriteFile(filepath.Join(asset, "Dress.zip"), main, 0644)
	res = unpackAll([]string{asset}, "", recycleFiles, nil)
	if len(res.Done) != 0 || res.Removed != 0 {
		t.Errorf("unpacked again: %+v", res)
	}
}

func TestUnpackWithTool(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z here")
	}
	root := t.TempDir()
	src := filepath.Join(t.TempDir(), "Hair")
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "Hair.unitypackage"), bytes.Repeat([]byte("h"), 300000), 0644)
	// a password-protected 7z split into volumes
	cmd := exec.Command("7z", "a", "-v100k", "-mx=0", "-psecret", "-mhe=on", filepath.Join(root, "Hair.7z"), src)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z: %v %s", err, b)
	}
	sets := findArchives(root)
	if len(sets) != 1 || len(sets[0].Parts) < 3 {
		t.Fatalf("sets %+v", sets)
	}
	if _, err := extractArchive(sets[0], "wrong"); err != errArcPwd {
		t.Errorf("wrong password: %v", err)
	}
	res := unpackAll([]string{root}, "secret", recycleFiles, nil)
	if len(res.Failed) > 0 {
		t.Fatalf("failed %v", res.Failed)
	}
	if _, err := os.Stat(filepath.Join(root, "Hair", "Hair.unitypackage")); err != nil {
		t.Error("not unpacked")
	}
	if left, _ := filepath.Glob(filepath.Join(root, "Hair.7z.*")); len(left) > 0 {
		t.Errorf("volumes left: %v", left)
	}
}

func TestChoosePackages(t *testing.T) {
	st := &Store{Settings: Settings{Bases: []string{"Kaguya=kaguya|カグヤ", "Plum=plum|プラム", "Chocolat=chocolat"}}}
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

// The archive program Windows opens archives with leads to its command-line program.
func TestToolFromExe(t *testing.T) {
	d := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(d, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("x"), 0644)
		return p
	}
	cases := []struct{ gui, cli, kind string }{
		{"7-Zip/7zFM.exe", "7-Zip/7z.exe", "7z"},
		{"Bandizip/Bandizip.exe", "Bandizip/bz.exe", "bz"},
		{"WinRAR/WinRAR.exe", "WinRAR/WinRAR.exe", "winrar"},
		{"HaoZip/HaoZip.exe", "HaoZip/HaoZipC.exe", "haozip"},
	}
	for _, c := range cases {
		gui, cli := mk(c.gui), mk(c.cli)
		tool := toolFromExe(gui)
		if tool == nil || tool.kind != c.kind || tool.exe != cli {
			t.Errorf("%s → %+v", c.gui, tool)
		}
	}
	if toolFromExe(mk("360zip/360zip.exe")) != nil {
		t.Error("360压缩 has no command line to use")
	}
}
