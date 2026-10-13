package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
)

// A base body into a project built on another one: the window is asked to confirm first, and what it sends
// back with "confirmed" starts the import.
func TestImportAskAPI(t *testing.T) {
	st := testkit.NewStore(t)
	proj := filepath.Join(t.TempDir(), "Kipfel")
	for _, d := range []string{"Assets", "ProjectSettings"} {
		_ = os.MkdirAll(filepath.Join(proj, d), 0755)
	}
	lib := t.TempDir()
	testkit.WriteFile(t, filepath.Join(lib, "Plum", "Plum_v1.0.1.unitypackage"), 100)
	st.Assets = []*core.Asset{
		{Key: "path:kipfel", Name: "Kipfel", Category: "素体", Bases: []string{"Kipfel"}, Locations: []core.Location{{Path: filepath.Join(lib, "Kipfel"), Kind: "dir"}},
			Usage: []core.Usage{{Project: "Kipfel", Status: "used"}}},
		{Key: "path:plum", Name: "Plum", Category: "素体", Bases: []string{"Plum"}, Locations: []core.Location{{Path: filepath.Join(lib, "Plum"), Kind: "dir"}}},
	}
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	r := postJSON(t, srv, "/api/import/start", map[string]any{"key": "path:plum", "project": proj})
	if r["ok"] != false || r["ask"] != "「Plum」是 Plum 的素体，目标工程「Kipfel」用的是 Kipfel 素体。仍要导入到这个工程吗？" {
		t.Fatalf("not asked: %v", r)
	}
	if j := unity.ImportSnapshot(); j != nil {
		t.Fatalf("an import began: %+v", j)
	}
	r = postJSON(t, srv, "/api/import/start", map[string]any{"key": "path:plum", "project": proj, "confirmed": true})
	if r["ok"] != true {
		t.Fatalf("confirmed: %v", r)
	}
	for i := 0; i < 500; i++ {
		if j := unity.ImportSnapshot(); j != nil && (j.Stage == "done" || j.Stage == "failed") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	unity.DismissImport()
}
