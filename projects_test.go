package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeProject(t *testing.T, ver string) string {
	p := filepath.Join(t.TempDir(), "Plum_FT")
	for _, d := range []string{"Assets", "ProjectSettings", "Packages", "Library"} {
		_ = os.MkdirAll(filepath.Join(p, d), 0755)
	}
	_ = os.WriteFile(filepath.Join(p, "ProjectSettings", "ProjectVersion.txt"), []byte("m_EditorVersion: "+ver+"\nm_EditorVersionWithRevision: "+ver+" (abc)\n"), 0644)
	return p
}

func TestProjectCard(t *testing.T) {
	p := fakeProject(t, "2022.3.22f1")
	if v := projectUnityVersion(p); v != "2022.3.22f1" {
		t.Errorf("version %q", v)
	}
	when := time.Now().Add(-48 * time.Hour)
	f := filepath.Join(p, "Library", "LastSceneManagerSetup.txt")
	_ = os.WriteFile(f, []byte("x"), 0644)
	_ = os.Chtimes(f, when, when)
	if o := projectOpened(p); o != when.Unix() {
		t.Errorf("opened %d, want %d", o, when.Unix())
	}
	if projectRunning(p) {
		t.Error("not open")
	}
	// covers: the newest one
	d := filepath.Join(p, "UserSettings", "MioVRCA")
	_ = os.MkdirAll(d, 0755)
	_ = os.WriteFile(filepath.Join(d, "cover_100.png"), []byte("png"), 0644)
	_ = os.Chtimes(filepath.Join(d, "cover_100.png"), when, when)
	_ = os.WriteFile(filepath.Join(d, "cover_200.png"), []byte("png"), 0644)
	if c, _ := projectCover(p); filepath.Base(c) != "cover_200.png" {
		t.Errorf("cover %q", c)
	}
}

func TestCoverHelper(t *testing.T) {
	p := fakeProject(t, "2022.3.22f1")
	if err := setCoverHelper(p, true); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(p, "Packages", coverPkg)
	for _, f := range []string{"package.json", "package.json.meta", "Editor.meta", "Editor/ProjectCover.cs", "Editor/ProjectCover.cs.meta",
		"Editor/MioVRCA.ProjectCard.Editor.asmdef", "Editor/MioVRCA.ProjectCard.Editor.asmdef.meta"} {
		if !statOK(filepath.Join(dir, filepath.FromSlash(f))) {
			t.Errorf("missing %s", f)
		}
	}
	if statOK(dir + "~") {
		t.Error("temp folder left")
	}
	var m map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "package.json"))
	if json.Unmarshal(b, &m) != nil || m["name"] != coverPkg {
		t.Errorf("package.json %s", b)
	}
	if err := setCoverHelper(p, true); err != nil { // again: replaces our copy
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(p, "UserSettings", "MioVRCA"), 0755)
	_ = os.WriteFile(filepath.Join(p, "UserSettings", "MioVRCA", "cover_1.png"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(p, "UserSettings", "EditorUserSettings.asset"), []byte("keep"), 0644)
	if err := setCoverHelper(p, false); err != nil {
		t.Fatal(err)
	}
	if statOK(dir) || statOK(filepath.Join(p, "UserSettings", "MioVRCA")) || !statOK(filepath.Join(p, "UserSettings", "EditorUserSettings.asset")) {
		t.Error("off: only our package and pictures go")
	}
	// someone else's package of that name is left alone
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"com.miovrc.projectcard","author":{"name":"someone"}}`), 0644)
	if err := setCoverHelper(p, false); err == nil || !statOK(dir) {
		t.Errorf("foreign package: %v", err)
	}
	if !strings.Contains(strings.Join(helperFiles(), " "), "ProjectCover.cs") {
		t.Error("embedded files")
	}
}

func TestUnityEditors(t *testing.T) {
	exe := "Unity"
	if runtime.GOOS == "windows" {
		exe = "Unity.exe"
	}
	hub := t.TempDir()
	_ = os.MkdirAll(filepath.Join(hub, "2022.3.22f1", "Editor"), 0755)
	_ = os.WriteFile(filepath.Join(hub, "2022.3.22f1", "Editor", exe), []byte("x"), 0755)
	_ = os.MkdirAll(filepath.Join(hub, "2019.4.31f1"), 0755) // no editor in it
	t.Setenv("VRCLIB_UNITY_DIRS", hub)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	edMu.Lock()
	edCache = nil
	edMu.Unlock()
	eds := unityEditors()
	if eds["2022.3.22f1"] == "" || eds["2019.4.31f1"] != "" {
		t.Errorf("editors %v", eds)
	}
	// the Hub's list of editors located by hand
	manual := filepath.Join(t.TempDir(), "Unity.exe")
	_ = os.WriteFile(manual, []byte("x"), 0755)
	out := map[string]string{}
	var v any
	_ = json.Unmarshal([]byte(`{"schema_version":"v2","data":[{"version":"2022.3.6f1","location":[`+jsonStr(manual)+`],"manual":true}],
		"unityEditors":[{"path":`+jsonStr(manual)+`,"version":"2019.4.40f1"}]}`), &v)
	findEditors(v, out)
	if out["2022.3.6f1"] != manual || out["2019.4.40f1"] != manual {
		t.Errorf("manual %v", out)
	}
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
