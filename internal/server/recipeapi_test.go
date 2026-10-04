package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
)

// the recipe routes: a file is looked at, taken in, listed, renamed, written out again and removed; what is
// asked for by a name that is not a recipe's gets an answer, not a file
func TestRecipeRoutes(t *testing.T) {
	apiToken = "t0ken"
	st, srv := guarded(t)
	_ = st
	file := map[string]any{"format": "miovrca-recipe", "schema": 1, "name": "朋友的方案", "base": map[string]any{"name": "Kaguya"},
		"assets": []any{map[string]any{"name": "水手服", "boothId": "1234567", "prefab": "Assets/Shop/Sailor/Sailor.prefab", "kind": "衣服", "object": "Sailor"}},
		"menus":  []any{map[string]any{"items": []any{map[string]any{"kind": "outfit", "path": []string{"衣服"}, "label": "水手服", "objects": []string{"Sailor"}}}}}}
	text, _ := json.Marshal(file)
	post := func(path string, body any) map[string]any { return postJSON(t, srv, path, body) }

	if r := post("/api/recipe/import", map[string]any{"text": string(text)}); r["ok"] != true || len(post("/api/recipe/list", map[string]any{})["recipes"].([]any)) != 0 {
		t.Fatalf("looking at a file: %v", r)
	} else if needs := r["needs"].([]any); len(needs) != 1 || needs[0].(map[string]any)["link"] != "https://booth.pm/ja/items/1234567" {
		t.Errorf("needs %v", needs)
	}
	r := post("/api/recipe/import", map[string]any{"text": string(text), "save": true})
	id, _ := r["recipe"].(map[string]any)["id"].(string)
	if r["ok"] != true || id == "" {
		t.Fatalf("import: %v", r)
	}
	list := post("/api/recipe/list", map[string]any{})["recipes"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["name"] != "朋友的方案" || list[0].(map[string]any)["imported"] != true {
		t.Fatalf("list %v", list)
	}
	if r := post("/api/recipe/rename", map[string]any{"id": id, "name": "改过的名字", "note": "备注"}); r["ok"] != true {
		t.Errorf("rename: %v", r)
	}
	if r := post("/api/recipe/get", map[string]any{"id": id}); r["ok"] != true || r["recipe"].(map[string]any)["name"] != "改过的名字" {
		t.Errorf("get: %v", r)
	}
	dir := t.TempDir()
	r = post("/api/recipe/export", map[string]any{"id": id, "dir": dir})
	out, _ := r["file"].(string)
	if b, err := os.ReadFile(out); r["ok"] != true || err != nil || filepath.Dir(out) != dir || !strings.HasSuffix(out, ".miovrca-recipe.json") || strings.Contains(string(b), id) {
		t.Errorf("export: %v %v", r, err)
	}
	for _, bad := range []map[string]any{{"id": id, "dir": ""}, {"id": id, "dir": "relative/dir"}, {"id": "../library", "dir": dir}} {
		if r := post("/api/recipe/export", bad); r["ok"] != false {
			t.Errorf("export %v: %v", bad, r)
		}
	}
	// a project that is not in the list is not looked into
	for _, path := range []string{"/api/recipe/preview", "/api/recipe/apply", "/api/recipe/save"} {
		if r := post(path, map[string]any{"id": id, "project": t.TempDir(), "name": "x"}); r["ok"] != false || !strings.Contains(r["err"].(string), "不在工程列表中") {
			t.Errorf("%s on an unknown project: %v", path, r)
		}
	}
	for _, bad := range []string{"", "..", "../library", "nope"} {
		for _, path := range []string{"/api/recipe/get", "/api/recipe/delete", "/api/recipe/rename"} {
			if r := post(path, map[string]any{"id": bad, "name": "x"}); r["ok"] != false {
				t.Errorf("%s %q: %v", path, bad, r)
			}
		}
	}
	if !core.FileExists(filepath.Join(core.DataDir, "recipes", id+".json")) {
		t.Fatal("the recipe's file is gone")
	}
	if r := post("/api/recipe/import", map[string]any{"text": `{"format":"miovrca-recipe","schema":1,"name":"x","base":{},"assets":[{"name":"a","prefab":"Assets/../../x.prefab","kind":"衣服","object":"a"}],"menus":[]}`, "save": true}); r["ok"] != false {
		t.Errorf("a path out of the project: %v", r)
	}
	if r := post("/api/recipe/delete", map[string]any{"id": id}); r["ok"] != true || len(post("/api/recipe/list", map[string]any{})["recipes"].([]any)) != 0 {
		t.Errorf("delete: %v", r)
	}
}
