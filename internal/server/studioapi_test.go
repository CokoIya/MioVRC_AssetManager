package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
)

// the card's 「摄影棚」: the package goes in and Unity is asked for the studio; the details take it out again
func TestStudioAPI(t *testing.T) {
	// no Unity on this computer: the request must not start one
	t.Setenv("VRCLIB_UNITY_DIRS", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramW6432", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	reset := func() {
		unity.EdMu.Lock()
		unity.EdCache = nil
		unity.EdMu.Unlock()
	}
	reset()
	t.Cleanup(reset)

	st := testkit.NewStore(t)
	proj := filepath.Join(t.TempDir(), "Kaguya")
	for _, d := range []string{"Assets", "Packages", "ProjectSettings"} {
		_ = os.MkdirAll(filepath.Join(proj, d), 0755)
	}
	_ = os.WriteFile(filepath.Join(proj, "ProjectSettings", "ProjectVersion.txt"), []byte("m_EditorVersion: 2022.3.22f1\n"), 0644)
	st.Projects = []core.ProjectInfo{{Name: filepath.Base(proj), Path: proj}}
	st.Settings.Lang = "en"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()

	if r := postJSON(t, srv, "/api/project/studio", map[string]any{"path": "/nowhere"}); r["ok"] != false || !strings.Contains(r["err"].(string), "不在工程列表中") {
		t.Errorf("unknown project: %v", r)
	}
	pkg := filepath.Join(proj, "Packages", unity.StudioPkg)
	r := postJSON(t, srv, "/api/project/studio", map[string]any{"path": proj}) // "on" left out: on
	if note, _ := r["note"].(string); r["ok"] != true || note == "" || r["running"] != false {
		t.Errorf("open: %v", r)
	}
	b, err := os.ReadFile(filepath.Join(unity.StudioDataDir(proj), "open.json"))
	var req struct {
		At   int64  `json:"at"`
		Lang string `json:"lang"`
	}
	if err != nil || json.Unmarshal(b, &req) != nil || req.At == 0 || req.Lang != "en" {
		t.Errorf("request %s %v", b, err)
	}
	if !unity.StudioInstalled(proj) {
		t.Error("package not installed")
	}
	if r := postJSON(t, srv, "/api/projects", map[string]any{}); !strings.Contains(fmt.Sprint(r["projects"]), "studio:true") {
		t.Errorf("card: %v", r)
	}
	// not while the studio is open in Unity: it keeps its running file fresh
	running := filepath.Join(unity.StudioDataDir(proj), "running")
	_ = os.WriteFile(running, []byte("1"), 0644)
	if r := postJSON(t, srv, "/api/project/studio", map[string]any{"path": proj, "on": false}); r["ok"] != false || !strings.Contains(fmt.Sprint(r["err"]), "退出 Play 模式") || !unity.StudioInstalled(proj) {
		t.Errorf("removed while open: %v", r)
	}
	_ = os.Remove(running) // the studio closed
	if r := postJSON(t, srv, "/api/project/studio", map[string]any{"path": proj, "on": false}); r["ok"] != true {
		t.Errorf("remove: %v", r)
	}
	if core.StatOK(pkg) || core.StatOK(unity.StudioDataDir(proj)) {
		t.Error("package or its settings left after removal")
	}
}
