package unity

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/library"
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
