package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/webpane"
)

var wishLoopOnce, followLoopOnce sync.Once

// registerStores: the wish list and other shops.
func registerStores(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	fail := func(w http.ResponseWriter, msg string) {
		core.WriteJSON(w, map[string]any{"ok": false, "err": msg})
	}
	// the daily look at the wish list's prices, and at the followed shops' lists, run for as long as the program does
	wishLoopOnce.Do(func() { go booth.WishLoop(st) })
	followLoopOnce.Do(func() { go booth.FollowLoop(st) })

	// what the page needs of the wish list and of Jinxxy, asked for again whenever the state's revision moves
	post("/api/stores/state", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		base := core.JinxxyBase()
		core.WriteJSON(w, map[string]any{"ok": true, "wish": booth.WishSnapshot(st), "follow": booth.FollowSnapshot(), "files": webpane.PaneFiles(),
			"jinxxy": map[string]any{"base": base, "search": core.JinxxySearchURL("")}})
	})
	// an item from a search result or the details panel (id, title, shop, thumb, price), or a pasted address
	post("/api/wish/add", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var seed booth.WishSeed
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &seed)
		if t := strings.TrimSpace(str(b, "text")); t != "" {
			seed = booth.WishSeed{ID: t}
		}
		it, err := booth.WishAdd(st, seed)
		if err != nil {
			fail(w, err.Error())
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "key": it.Key(), "title": it.Title})
	})
	post("/api/wish/remove", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": booth.WishRemove(str(b, "key"))})
	})
	post("/api/wish/note", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if !booth.WishNote(str(b, "key"), str(b, "note")) {
			fail(w, "该商品已不在愿望单中")
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/wish/seen", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		booth.WishSeen()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// the changes the window has announced: key → the time of the change
	post("/api/wish/told", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		at := map[string]int64{}
		_ = json.Unmarshal(b["at"], &at)
		booth.WishTold(at)
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/wish/watch", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var on bool
		_ = json.Unmarshal(b["on"], &on)
		booth.WishSetWatch(on)
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// 「立即检查」: every item not looked at within the hour, one after the other in the background
	post("/api/wish/check", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		n, err := booth.WishStartRound(st)
		if err != nil {
			fail(w, err.Error())
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "n": n})
	})

	// ---------- 1.7.6: following shops ----------
	// a shop from the details panel (sub: its address, name), or a pasted shop or item address
	post("/api/follow/add", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var seed booth.FollowSeed
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &seed)
		s, err := booth.FollowAdd(st, seed)
		if err != nil {
			fail(w, err.Error())
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "sub": s.Sub, "name": s.Name})
	})
	post("/api/follow/remove", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": booth.FollowRemove(str(b, "sub"))})
	})
	post("/api/follow/note", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if !booth.FollowNote(str(b, "sub"), str(b, "note")) {
			fail(w, "已取消关注该店铺")
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/follow/seen", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		booth.FollowSeen()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// 「全部标为已读」: one shop, or every shop without a sub
	post("/api/follow/read", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": booth.FollowRead(str(b, "sub"))})
	})
	// the arrivals the window has announced: "sub:id"
	post("/api/follow/told", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var keys []string
		_ = json.Unmarshal(b["keys"], &keys)
		booth.FollowTold(keys)
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// the daily look: for one shop, or for all of them without a sub
	post("/api/follow/watch", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var on bool
		_ = json.Unmarshal(b["on"], &on)
		core.WriteJSON(w, map[string]any{"ok": booth.FollowSetWatch(str(b, "sub"), on)})
	})
	// 「立即检查」: every shop not looked at within the hour, one after the other in the background
	post("/api/follow/check", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		n, err := booth.FollowStartRound(st)
		if err != nil {
			fail(w, err.Error())
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "n": n})
	})
}
