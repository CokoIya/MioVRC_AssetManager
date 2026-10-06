package server

import (
	"encoding/json"
	"net/http"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/unity"
)

// registerBundle: a card that holds a collection of products (合集包) is split into a card for each.
func registerBundle(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	fail := func(w http.ResponseWriter, msg string) {
		core.WriteJSON(w, map[string]any{"ok": false, "err": msg})
	}
	// Where every product can be got at as the card lies on the disk, its folder levels are marked and the
	// library scanned: {ok, n}. Where some of it is still inside archives, those are unpacked first — a job,
	// shown as an import is ({ok, job}); recycle and pwd are what the import form says.
	post("/api/bundle/split", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var req struct {
			Key     string `json:"key"`
			Pwd     string `json:"pwd"`
			Recycle bool   `json:"recycle"`
		}
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &req)
		st.Mu.RLock()
		var card *library.AssetView
		views := library.AllViews(st)
		for i := range views {
			if views[i].Key == req.Key {
				card = &views[i]
			}
		}
		st.Mu.RUnlock()
		switch {
		case card == nil:
			fail(w, "未找到该素材")
			return
		case card.Bundle < 2:
			fail(w, "该素材不是合集包，无需拆分")
			return
		}
		if card.BundlePacked {
			if err := unity.StartImport(st, unity.ImportReq{Key: req.Key, Split: true, Pwd: req.Pwd, Recycle: req.Recycle}); err != nil {
				fail(w, err.Error())
				return
			}
			core.WriteJSON(w, map[string]any{"ok": true, "job": true})
			return
		}
		n := 0
		for _, l := range card.Locations {
			if info := library.MarkBundles(st, l.Path, nil); len(info.Containers) > 0 {
				n += info.Products
			}
		}
		// (scanned also when nothing was marked: the folder is not what it was when the card was made)
		library.StartPipeline(st, true, true, false, false, nil)
		if n < 2 {
			fail(w, "未发现多个素材，未拆分")
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "n": n})
	})
}
