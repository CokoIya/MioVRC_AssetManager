package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

func unityProject(t *testing.T, p string) {
	t.Helper()
	for _, d := range []string{"Assets", "ProjectSettings"} {
		if err := os.MkdirAll(filepath.Join(p, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

// Projects inside folders of their own (a folder per avatar with its versions, backups, a face-tracking copy …)
// are found down to projDepth levels below a project folder, named by their path below it; nothing is looked for
// inside a project, in a project's Library, in hidden folders or behind a link; names two share are told apart.
func TestFindNestedProjects(t *testing.T) {
	root, other := filepath.Join(t.TempDir(), "VRChat"), filepath.Join(t.TempDir(), "Backup")
	for _, p := range []string{"Top", "Manuka第一款/v1", "Manuka第一款/v2_FT", "Manuka第一款/备份/2026-09", "a/b/c/Deep4", "Top/Inner"} {
		unityProject(t, filepath.Join(root, filepath.FromSlash(p)))
	}
	unityProject(t, filepath.Join(root, "a", "b", "c", "d", "TooDeep5"))
	unityProject(t, filepath.Join(root, ".hidden", "P"))
	unityProject(t, filepath.Join(root, "Broken", "Library", "P"))
	unityProject(t, filepath.Join(other, "Manuka第一款", "v1"))
	unityProject(t, filepath.Join(other, "Single"))
	if err := os.Symlink(filepath.Join(root, "Manuka第一款"), filepath.Join(root, "link")); err != nil {
		t.Logf("no symlink here: %v", err)
	}
	got := map[string]string{}
	for _, p := range findProjects([]string{root, other, filepath.Join(other, "Single")}) {
		got[p.Name] = p.Path
	}
	want := map[string]string{
		"Top":                  filepath.Join(root, "Top"),
		"VRChat/Manuka第一款/v1":  filepath.Join(root, "Manuka第一款", "v1"),
		"Backup/Manuka第一款/v1":  filepath.Join(other, "Manuka第一款", "v1"),
		"Manuka第一款/v2_FT":      filepath.Join(root, "Manuka第一款", "v2_FT"),
		"Manuka第一款/备份/2026-09": filepath.Join(root, "Manuka第一款", "备份", "2026-09"),
		"a/b/c/Deep4":          filepath.Join(root, "a", "b", "c", "Deep4"),
		"Single":               filepath.Join(other, "Single"),
	}
	if len(got) != len(want) {
		t.Errorf("found %d: %v", len(got), got)
	}
	for n, p := range want {
		if got[n] != p {
			t.Errorf("%q: %q, want %q (all: %v)", n, got[n], p, got)
		}
	}
}

// A project folder with more folders than are opened (a whole drive, say) is gone through level by level: its own
// level is always looked at, the deepest levels are what is left out — and the folders after it lose nothing.
func TestFindProjectsBudget(t *testing.T) {
	old := projBudget
	projBudget = 40
	defer func() { projBudget = old }()
	big, small := filepath.Join(t.TempDir(), "Drive"), filepath.Join(t.TempDir(), "Unity")
	for i := 0; i < 60; i++ {
		if err := os.MkdirAll(filepath.Join(big, fmt.Sprintf("d%02d", i), "x"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	unityProject(t, filepath.Join(big, "zz_Project"))       // on the folder's own level, after 60 others
	unityProject(t, filepath.Join(big, "d00", "x", "Deep")) // two levels down, past what is opened
	unityProject(t, filepath.Join(small, "Avatar"))         // in the next folder
	unityProject(t, filepath.Join(small, "Kikyo", "v2_FT")) // …and below it
	got := map[string]bool{}
	for _, p := range findProjects([]string{big, small}) {
		got[p.Name] = true
	}
	if !got["zz_Project"] || !got["Avatar"] || !got["Kikyo/v2_FT"] || got["d00/x/Deep"] || len(got) != 3 {
		t.Errorf("found %v", got)
	}
}

// More than 64 projects: each one's use of an asset is still told (a GUID's projects were 64 bits before).
func TestUsageBeyond64Projects(t *testing.T) {
	st := testkit.NewStore(t)
	assets, projRoot := filepath.Join(t.TempDir(), "Assets"), filepath.Join(t.TempDir(), "Unity")
	dress := filepath.Join(assets, "Moon Dress")
	for i, g := range []string{"a", "b", "c", "d"} {
		metaFile(t, filepath.Join(dress, "Moon", "f"+core.Itoa(i)+".prefab"), g)
	}
	uses := map[string]bool{"P003": true, "P070": true, "P099": true}
	for i := 0; i < 100; i++ {
		n := "P" + strings.Repeat("0", 3-len(core.Itoa(i))) + core.Itoa(i)
		p := filepath.Join(projRoot, n)
		unityProject(t, p)
		if uses[n] {
			for j, g := range []string{"a", "b", "c", "d"} {
				metaFile(t, filepath.Join(p, "Assets", "Moon", "f"+core.Itoa(j)+".prefab"), g)
			}
		}
	}
	st.Settings.Roots, st.Settings.ProjectRoots = []string{assets}, []string{projRoot}
	st.Settings.SetupDone, st.Settings.NoWatch, st.Settings.AutoBooth, st.Settings.HideZh = true, true, false, true
	RunFolderScan(st, core.TaskScan)
	RunUsageScan(st, core.TaskUsage)
	if len(st.Projects) != 100 {
		t.Fatalf("projects: %d", len(st.Projects))
	}
	var a *core.Asset
	for _, x := range st.Assets {
		if strings.Contains(x.Name, "Moon") {
			a = x
		}
	}
	if a == nil {
		t.Fatal("no asset")
	}
	got := map[string]string{}
	for _, u := range a.Usage {
		got[u.Project] = u.Status
	}
	if len(got) != 3 || got["P003"] != "used" || got["P070"] != "used" || got["P099"] != "used" {
		t.Errorf("usage: %v", got)
	}
	for _, p := range st.Projects {
		if want := map[bool]int{true: 1, false: 0}[uses[p.Name]]; p.Assets != want {
			t.Errorf("%s uses %d assets, want %d", p.Name, p.Assets, want)
		}
	}
}
