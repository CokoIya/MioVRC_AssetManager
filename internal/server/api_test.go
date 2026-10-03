package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
	"vrclib/internal/translate"
)

func postJSON(t *testing.T, srv *httptest.Server, path string, body any) map[string]any {
	t.Helper()
	j, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(j))
	req.Header.Set("X-Token", apiToken)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// scannedLibrary: a library with this folder scanned, and its assets by name.
func scannedLibrary(t *testing.T, root string) (*core.Store, map[string]*core.Asset) {
	t.Helper()
	core.DataDir = t.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	st.Settings.Roots = []string{root}
	library.RunFolderScan(st, &core.Task{})
	byName := map[string]*core.Asset{}
	for _, a := range st.Assets {
		byName[a.Name] = a
	}
	return st, byName
}

// Several cards at once: a category, hidden or not, and "do not list".
func TestBulkEdit(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"Alpha Coat", "Beta Hair", "Gamma Prop"} {
		testkit.WriteFile(t, filepath.Join(root, n, n+".unitypackage"), 100)
	}
	st, as := scannedLibrary(t, root)
	st.User[as["Alpha Coat"].Key] = &core.UserData{Notes: "kept", Tags: []string{"winter"}}
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	defer func() {
		deadline := time.Now().Add(10 * time.Second)
		for library.PipelineBusy() || translate.TransBusy.Load() {
			if time.Now().After(deadline) {
				t.Error("background tasks did not stop before test cleanup")
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	keys := []string{as["Alpha Coat"].Key, as["Beta Hair"].Key, "name:nosuchthing"}
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": keys, "category": "配饰"}); r["ok"] != true || r["n"] != float64(2) {
		t.Fatalf("category: %v", r)
	}
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": keys, "hidden": true}); r["ok"] != true {
		t.Fatalf("hide: %v", r)
	}
	st.Mu.RLock()
	a, b := st.User[as["Alpha Coat"].Key], st.User[as["Beta Hair"].Key]
	if a.Category != "配饰" || !a.Hidden || a.Notes != "kept" || len(a.Tags) != 1 || b == nil || b.Category != "配饰" || !b.Hidden || st.User["name:nosuchthing"] != nil {
		t.Errorf("after bulk: %+v %+v", a, b)
	}
	if u := st.User[as["Gamma Prop"].Key]; u != nil && (u.Hidden || u.Category != "") {
		t.Errorf("an unpicked card changed: %+v", u)
	}
	st.Mu.RUnlock()
	// back to the automatic category, shown again
	postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": keys[:1], "category": "", "hidden": false})
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": keys, "category": "没有的分类"}); r["ok"] != false {
		t.Errorf("an unknown category was taken: %v", r)
	}
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": []string{}}); r["ok"] != false {
		t.Errorf("nothing picked: %v", r)
	}
	st.Mu.RLock()
	if a := st.User[as["Alpha Coat"].Key]; a.Category != "" || a.Hidden {
		t.Errorf("not reset: %+v", a)
	}
	st.Mu.RUnlock()
	// "do not list": the folder is skipped by the next scan
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": []string{as["Gamma Prop"].Key}, "ignore": true}); r["ignored"] != float64(1) {
		t.Fatalf("ignore: %v", r)
	}
	st.Mu.RLock()
	mode := st.Overrides[core.PathKey(filepath.Join(root, "Gamma Prop"))]
	st.Mu.RUnlock()
	if mode != "ignore" {
		t.Errorf("override %q", mode)
	}
}
