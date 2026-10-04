package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"vrclib/internal/ai"
	"vrclib/internal/core"
	"vrclib/internal/unity"
)

// registerRecipes: outfitting recipes — a pipeline run saved, applied to a project, exported and imported.
func registerRecipes(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	fail := func(w http.ResponseWriter, err error) {
		core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
	}
	project := func(w http.ResponseWriter, b map[string]json.RawMessage) (string, bool) {
		p, ok := unity.KnownProject(st, str(b, "project"))
		if !ok {
			fail(w, errors.New("该工程不在工程列表中"))
		}
		return p, ok
	}

	// the saved recipes; with a project, also whether its last run can be saved as one
	post("/api/recipe/list", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		res := map[string]any{"ok": true, "recipes": ai.ListRecipes()}
		if p, ok := unity.KnownProject(st, str(b, "project")); ok {
			res["ready"] = ai.RecipeReady(p)
		}
		core.WriteJSON(w, res)
	})
	post("/api/recipe/get", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		r, err := ai.LoadRecipe(str(b, "id"))
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "recipe": r, "needs": ai.RecipeNeeds(st, r)})
	})
	post("/api/recipe/save", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		r, err := ai.SaveRecipe(st, p, str(b, "name"), str(b, "note"))
		if err != nil {
			fail(w, err)
			return
		}
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true, "recipe": r})
	})
	post("/api/recipe/rename", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		r, err := ai.RenameRecipe(str(b, "id"), str(b, "name"), str(b, "note"))
		if err != nil {
			fail(w, err)
			return
		}
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true, "recipe": r})
	})
	post("/api/recipe/delete", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if err := ai.DeleteRecipe(str(b, "id")); err != nil {
			fail(w, err)
			return
		}
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// what applying it to the project would do: nothing is changed
	post("/api/recipe/preview", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		pv, err := ai.PreviewRecipe(st, str(b, "id"), p)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "preview": pv})
	})
	post("/api/recipe/apply", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		if err := ai.StartRecipeRun(st, p, str(b, "id")); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// one file in a folder the player picked
	post("/api/recipe/export", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		dir := filepath.Clean(strings.Trim(strings.TrimSpace(str(b, "dir")), `"`))
		if dir == "" || dir == "." || !filepath.IsAbs(dir) {
			fail(w, errors.New("请选择导出位置"))
			return
		}
		file, err := ai.ExportRecipe(str(b, "id"), dir)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "file": file})
	})
	// a file somebody else made: looked at first (save=false), then taken in
	post("/api/recipe/import", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var save bool
		_ = json.Unmarshal(b["save"], &save)
		var text string
		if raw, ok := b["text"]; !ok || json.Unmarshal(raw, &text) != nil || strings.TrimSpace(text) == "" {
			fail(w, errors.New("无法读取该文件：文件为空，或不是文本文件"))
			return
		}
		r, err := ai.ImportRecipe([]byte(text), save)
		if err != nil {
			fail(w, err)
			return
		}
		if save {
			core.BumpRev()
		}
		core.WriteJSON(w, map[string]any{"ok": true, "recipe": r, "needs": ai.RecipeNeeds(st, r)})
	})
}
