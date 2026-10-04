package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// The tidy tools over the API: a scan, its result, and the Recycle Bin taking only what that result listed.
func TestTidyEndpoints(t *testing.T) {
	root := t.TempDir()
	body := make([]byte, 4096)
	for i := range body {
		body[i] = byte(i * 7)
	}
	a, b := filepath.Join(root, "Alpha Coat", "tex.bin"), filepath.Join(root, "Alpha Coat", "copy", "tex.bin")
	for _, p := range []string{a, b} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	testkit.WriteFile(t, filepath.Join(root, "Alpha Coat", "Alpha Coat.unitypackage"), 100)
	other := filepath.Join(t.TempDir(), "elsewhere.bin")
	_ = os.WriteFile(other, body, 0o644)
	st, _ := scannedLibrary(t, root)
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()

	if r := postJSON(t, srv, "/api/libtools/tidy/recycle", map[string]any{"what": "dup", "paths": []string{a}}); r["ok"] == true {
		t.Fatalf("recycled without a scan: %v", r)
	}
	if r := postJSON(t, srv, "/api/libtools/tidy/scan", map[string]any{"what": "dup", "minSize": 1}); r["ok"] != true {
		t.Fatalf("scan: %v", r)
	}
	var dup map[string]any
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		r := postJSON(t, srv, "/api/libtools/tidy/status", nil)
		part, _ := r["tidy"].(map[string]any)["dup"].(map[string]any)
		if run, _ := part["run"].(map[string]any); run["running"] != true && part["dup"] != nil {
			dup = part["dup"].(map[string]any)
			break
		}
	}
	groups, _ := dup["groups"].([]any)
	if len(groups) != 1 || len(groups[0].(map[string]any)["files"].([]any)) != 2 {
		t.Fatalf("groups: %v", dup)
	}
	seq := dup["seq"]
	// the window has this result already: it is not sent again, until it changes
	if r := postJSON(t, srv, "/api/libtools/tidy/status", map[string]any{"have": map[string]any{"dup": seq}}); r["tidy"].(map[string]any)["dup"].(map[string]any)["dup"] != nil || r["same"].(map[string]any)["dup"] != true {
		t.Fatalf("a result the window has was sent again: %v", r)
	}
	// confirmed without saying which scan was shown: refused
	if r := postJSON(t, srv, "/api/libtools/tidy/recycle", map[string]any{"what": "dup", "paths": []string{b}}); r["ok"] == true {
		t.Fatalf("recycled without the scan the player saw: %v", r)
	}
	// a file outside the result, and the whole group, are refused; nothing is touched
	for _, paths := range [][]string{{other}, {a, b}} {
		if r := postJSON(t, srv, "/api/libtools/tidy/recycle", map[string]any{"what": "dup", "paths": paths, "seq": seq}); r["ok"] == true {
			res, _ := r["result"].(map[string]any)
			if res["removed"] != float64(0) {
				t.Fatalf("recycled %v: %v", paths, r)
			}
		}
	}
	for _, p := range []string{a, b, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s is gone", p)
		}
	}
	r := postJSON(t, srv, "/api/libtools/tidy/recycle", map[string]any{"what": "dup", "paths": []string{b}, "seq": seq})
	if res, _ := r["result"].(map[string]any); r["ok"] != true || res["removed"] != float64(1) || res["freed"] != float64(len(body)) {
		t.Fatalf("recycle: %v", r)
	}
	if _, err := os.Stat(b); err == nil {
		t.Error("the picked copy is still there")
	}
	if _, err := os.Stat(a); err != nil {
		t.Error("the kept copy is gone")
	}
	settle()
}

// exportZip: a library export as another computer (or somebody else) would hand it over.
func exportZip(t *testing.T, lib any, files map[string]string) string {
	t.Helper()
	man, _ := json.Marshal(map[string]any{"app": core.AppName, "kind": "library-export", "format": 1, "version": core.AppVersion})
	lb, _ := json.Marshal(lib)
	all := map[string]string{"manifest.json": string(man), "library.json": string(lb)}
	for n, c := range files {
		all[n] = c
	}
	p := filepath.Join(t.TempDir(), "friend.zip")
	testkit.MakeZip(t, p, all)
	return p
}

// A library export taken in with "replace" does not re-point the AI service: the key saved on this computer
// is only ever sent to the address it was saved for.
func TestImportDoesNotRedirectAIKey(t *testing.T) {
	var mu sync.Mutex
	gotAuth := map[string]string{}
	mk := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			gotAuth[name] = r.Header.Get("Authorization")
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
		}))
	}
	good, evil := mk("good"), mk("evil")
	defer good.Close()
	defer evil.Close()
	root := t.TempDir()
	testkit.WriteFile(t, filepath.Join(root, "Alpha Coat", "Alpha Coat.unitypackage"), 100)
	st, _ := scannedLibrary(t, root)
	st.Settings.Proxy, st.Settings.HideZh = "direct", true
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	if r := postJSON(t, srv, "/api/ai/save", map[string]any{"provider": "openai", "baseUrl": good.URL, "model": "m1", "key": "sk-PLAYER-SECRET-12345"}); r["ok"] != true {
		t.Fatalf("save: %v", r)
	}
	aij, _ := json.Marshal(map[string]any{"provider": "openai", "profiles": map[string]any{"openai": map[string]any{"baseUrl": evil.URL, "model": "m1"}}})
	zp := exportZip(t, map[string]any{"version": 1, "settings": map[string]any{"roots": []string{}}}, map[string]string{"data/ai.json": string(aij), "data/AI~1.json": string(aij)})
	if r := postJSON(t, srv, "/api/libtools/import/apply", map[string]any{"path": zp, "mode": "replace"}); r["ok"] != true {
		t.Fatalf("import: %v", r)
	}
	settle()
	cfg := postJSON(t, srv, "/api/ai/config", nil)["ai"].(map[string]any)
	prof := cfg["profiles"].(map[string]any)["openai"].(map[string]any)
	if prof["baseUrl"] != good.URL {
		t.Errorf("the AI address after the import: %v, want this computer's %s", prof["baseUrl"], good.URL)
	}
	if r := postJSON(t, srv, "/api/ai/models", map[string]any{"provider": "openai", "baseUrl": prof["baseUrl"], "model": "m1"}); r["ok"] != true {
		t.Errorf("the model list of the saved service: %v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotAuth["evil"] != "" || gotAuth["good"] != "Bearer sk-PLAYER-SECRET-12345" {
		t.Errorf("the saved key went to: %q", gotAuth)
	}
}

// A replace over the API: this computer's proxy, download folder and update check stay; recipes that are not
// valid are not listed; and the undo takes away what the import created.
func TestImportSettingsRecipesAndUndo(t *testing.T) {
	root := t.TempDir()
	testkit.WriteFile(t, filepath.Join(root, "Alpha Coat", "Alpha Coat.unitypackage"), 100)
	st, _ := scannedLibrary(t, root)
	st.Settings.Proxy, st.Settings.NoWatch, st.Settings.HideZh = "", true, true // the player never set a proxy
	_ = st.Save()
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	elsewhere := t.TempDir() // any folder that exists on this computer
	const good = `{"format":"miovrca-recipe","schema":1,"name":"friend's","base":{"prefab":"Assets/Base/Base.prefab"},"assets":[{"name":"a","prefab":"Assets/Shop/a.prefab","kind":"衣服","object":"a","url":"https://booth.pm/ja/items/1"}],"menus":[]}`
	const evil = `{"format":"miovrca-recipe","schema":1,"name":"x","base":{"prefab":"Packages/../../x.prefab"},"assets":[{"name":"a","prefab":"../../outside.prefab","kind":"衣服","object":"a","url":"javascript:alert(1)"}],"menus":[]}`
	zp := exportZip(t, map[string]any{"version": 1, "settings": map[string]any{"roots": []string{}, "proxy": "http://203.0.113.9:8080", "downloadDir": elsewhere, "noUpdateCheck": true}},
		map[string]string{"data/wishlist.json": `{"items":[]}`, "data/recipes/zz-evil-recipe.json": evil, "data/recipes/zz-good-recipe.json": good})
	if r := postJSON(t, srv, "/api/libtools/import/apply", map[string]any{"path": zp, "mode": "replace"}); r["ok"] != true {
		t.Fatalf("import: %v", r)
	}
	settle()
	st.Mu.RLock()
	if st.Settings.Proxy != "" || st.Settings.DownloadDir != "" || st.Settings.NoUpdateCheck || len(st.Assets) != 0 {
		t.Errorf("after the replace: proxy %q, download folder %q, no update check %v, %d assets", st.Settings.Proxy, st.Settings.DownloadDir, st.Settings.NoUpdateCheck, len(st.Assets))
	}
	st.Mu.RUnlock()
	list, _ := postJSON(t, srv, "/api/recipe/list", nil)["recipes"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != "zz-good-recipe" {
		t.Errorf("recipes after the import: %v", list)
	}
	if r := postJSON(t, srv, "/api/libtools/import/undo", nil); r["ok"] != true {
		t.Fatalf("undo: %v", r)
	}
	settle()
	for _, f := range []string{"wishlist.json", "recipes/zz-evil-recipe.json", "recipes/zz-good-recipe.json", "recipes"} {
		if _, err := os.Stat(filepath.Join(core.DataDir, filepath.FromSlash(f))); err == nil {
			t.Errorf("after the undo, %s written by the import is still in the data folder", f)
		}
	}
	st.Mu.RLock()
	if len(st.Assets) != 1 || st.Settings.Proxy != "" {
		t.Errorf("after the undo: %d assets, proxy %q", len(st.Assets), st.Settings.Proxy)
	}
	st.Mu.RUnlock()
	if list, _ := postJSON(t, srv, "/api/recipe/list", nil)["recipes"].([]any); len(list) != 0 {
		t.Errorf("recipes after the undo: %v", list)
	}
}
