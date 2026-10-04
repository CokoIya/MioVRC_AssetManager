package purchases

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// A file the pane's browser downloaded goes into a folder of its own in the download folder, is unpacked like
// a Booth download, and the download folder becomes one of the library's.
func TestAdoptPaneFile(t *testing.T) {
	st := testkit.NewStore(t)
	st.Settings.HideZh = true // (no names sent to the translator from a test)
	// before the folders are removed: the rescan a taken-in file starts still saves into them
	t.Cleanup(settleBackground)
	dl := filepath.Join(t.TempDir(), "downloads")
	st.Settings.DownloadDir = dl
	in := paneIncomingDir(st)
	if in != filepath.Join(dl, ".incoming") {
		t.Fatalf("incoming: %s", in)
	}
	_ = os.MkdirAll(in, 0755)
	src := filepath.Join(in, "527a8247-3c3c-472e-8a03-f074dbb4055a")
	testkit.MakeZip(t, src, map[string]string{"Studded Shorts/StuddedShorts_Plum.unitypackage": "pkg", "Studded Shorts/readme.txt": "hi"})
	final, err := adoptPaneFile(st, src, "Studded Mini Shorts v1.2.zip")
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dl, "Studded Mini Shorts v1.2")
	if final != folder || core.FileExists(src) {
		t.Fatalf("final %s, the browser's copy still there: %v", final, core.FileExists(src))
	}
	found := false
	_ = filepath.WalkDir(folder, func(p string, d os.DirEntry, err error) error {
		found = found || filepath.Base(p) == "StuddedShorts_Plum.unitypackage"
		return nil
	})
	if !found {
		t.Fatalf("not unpacked: %v", testkit.ListTree(dl))
	}
	if core.FileExists(filepath.Join(folder, "Studded Mini Shorts v1.2.zip")) {
		t.Error("the zip was kept though the settings say otherwise")
	}
	st.Mu.RLock()
	inRoots := core.ContainsStr(st.Settings.Roots, dl)
	st.Mu.RUnlock()
	if !inRoots {
		t.Error("the download folder is not one of the library's")
	}
	// not an archive, and one of that name is there already: kept next to it under another name
	for i, want := range []string{"Hat.unitypackage", "Hat (2).unitypackage"} {
		src := filepath.Join(in, "g"+string(rune('0'+i)))
		_ = os.WriteFile(src, []byte("pkg"), 0644)
		final, err := adoptPaneFile(st, src, "Hat.unitypackage")
		if err != nil || final != filepath.Join(dl, "Hat", want) || !core.FileExists(final) {
			t.Fatalf("%d: %v %s", i, err, final)
		}
	}
	// a name with a path in it stays inside the download folder
	src = filepath.Join(in, "g9")
	_ = os.WriteFile(src, []byte("x"), 0644)
	final, err = adoptPaneFile(st, src, `..\..\evil.txt`)
	if err != nil || !core.UnderDir(final, dl) {
		t.Fatalf("%v %s", err, final)
	}
	if _, err := adoptPaneFile(st, filepath.Join(in, "missing"), "x.zip"); err == nil {
		t.Error("a file that is not there was taken in")
	}
}

// The folder a pane download goes into is named after the file, a name the site gave. When the player has a
// folder of that name with archives kept packed, only the file that was just downloaded is unpacked and
// removed.
func TestPaneFileLeavesPlayersArchives(t *testing.T) {
	st := testkit.NewStore(t)
	t.Cleanup(settleBackground)
	lib := t.TempDir() // an asset folder that is also where downloads go
	st.Settings.Roots, st.Settings.DownloadDir, st.Settings.NoWatch, st.Settings.HideZh = []string{lib}, lib, true, true
	mine := filepath.Join(lib, "Moon Dress")
	psd, old := filepath.Join(mine, "original", "MoonDress_PSD.zip"), filepath.Join(mine, "MoonDress_v1.0.zip")
	_ = os.MkdirAll(filepath.Dir(psd), 0755)
	testkit.MakeZip(t, psd, map[string]string{"tex.psd": "layers"})
	testkit.MakeZip(t, old, map[string]string{"MoonDress_v1.0/a.unitypackage": "old"})
	in := paneIncomingDir(st)
	_ = os.MkdirAll(in, 0755)
	src := filepath.Join(in, "527a8247-3c3c-472e-8a03-f074dbb4055a")
	testkit.MakeZip(t, src, map[string]string{"new/Moon.unitypackage": "pkg"})
	if _, err := adoptPaneFile(st, src, "Moon Dress.zip"); err != nil {
		t.Fatal(err)
	}
	want := "Moon Dress/MoonDress_v1.0.zip,Moon Dress/new/Moon.unitypackage,Moon Dress/original/MoonDress_PSD.zip"
	if got := strings.Join(testkit.ListTree(lib), ","); got != want {
		t.Errorf("after the download: %s", got)
	}
}
