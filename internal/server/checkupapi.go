package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"vrclib/internal/ai"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/unity"
)

// registerCheckup: the avatar check-up before upload, and covers made from prefabs.
func registerCheckup(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	num := func(b map[string]json.RawMessage, k string) float64 {
		var f float64
		_ = json.Unmarshal(b[k], &f)
		return f
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
	// what the page shows of a project's check-up: the last one, and whether a new one can be run right now
	last := func(p string) map[string]any {
		unity.AdoptTexFix(p) // a texture fix whose answer never arrived left its record in the project
		res := map[string]any{"ok": true, "record": unity.LastCheckup(p), "can": true}
		if err := unity.CheckupReady(p); err != nil {
			res["can"], res["why"] = false, err.Error()
		}
		return res
	}

	// ---------- the check-up ----------
	post("/api/checkup/run", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if _, err := unity.RunCheckup(ctx, p, str(b, "avatar")); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, last(p))
	})
	post("/api/checkup/last", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if p, ok := project(w, b); ok {
			core.WriteJSON(w, last(p))
		}
	})
	post("/api/checkup/all", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "records": unity.AllCheckups()})
	})

	// ---------- one-click fixes, and the way back ----------
	// the answer carries the fix and the check-up run after it (a check-up that failed after a fix that worked: recheckErr)
	fixed := func(w http.ResponseWriter, p string, key string, what any, recheck error) {
		res := last(p)
		res[key] = what
		if recheck != nil {
			res["recheckErr"] = recheck.Error()
		}
		core.WriteJSON(w, res)
	}
	post("/api/checkup/fix", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		args := map[string]any{"avatar": str(b, "avatar")}
		var paths []string
		_ = json.Unmarshal(b["paths"], &paths)
		if len(paths) > 0 {
			args["paths"] = paths
		} else {
			args["all"] = true
		}
		if max := num(b, "max"); max > 0 {
			args["max"] = max
		}
		if string(b["remove"]) == "true" {
			args["remove"] = true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		fix, _, recheck, err := unity.RunFix(ctx, p, str(b, "kind"), args)
		if err != nil {
			fail(w, err)
			return
		}
		ai.NoteFix(p, fix) // a scene fix is a step 「撤销上一步」 can take back, as one the AI made
		fixed(w, p, "fix", fix, recheck)
	})
	post("/api/checkup/fix/revert", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		rev, _, recheck, err := unity.RevertFix(ctx, p, str(b, "record"), str(b, "avatar"))
		if err != nil {
			fail(w, err)
			return
		}
		fixed(w, p, "revert", rev, recheck)
	})
	// the report card the page drew, kept as a file
	post("/api/checkup/report/save", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		path, err := unity.SaveReport(str(b, "avatar"), str(b, "png"))
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "path": path})
	})

	// ---------- a picture of the player's own as a card's cover ----------
	// picked from anywhere on this computer; a copy is kept in the program's folder (the original is left as it
	// is). reset: the card's own cover again.
	post("/api/cover/own", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		key := str(b, "key")
		var reset bool
		_ = json.Unmarshal(b["reset"], &reset)
		if !knownCard(st, key) {
			fail(w, errors.New("未找到该素材"))
			return
		}
		cover := ""
		if !reset {
			src, err := core.PickFile("选择封面图片", "图片 (*.png;*.jpg;*.jpeg;*.webp;*.gif)", "*.png;*.jpg;*.jpeg;*.webp;*.gif")
			if errors.Is(err, core.ErrPickCancelled) {
				core.WriteJSON(w, map[string]any{"ok": false, "cancelled": true})
				return
			}
			if err != nil {
				fail(w, errors.New("无法打开选择文件的窗口："+core.TrimErr(err)))
				return
			}
			if cover, err = library.CopyOwnCover(src); err != nil {
				fail(w, err)
				return
			}
		}
		st.Mu.Lock()
		u := st.User[key]
		if u == nil {
			u = &core.UserData{}
			st.User[key] = u
		}
		u.Cover, u.Updated = cover, time.Now().Unix()
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true, "cover": cover})
	})

	// ---------- covers from prefabs ----------
	post("/api/cover/status", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "status": ai.CoverStatusOf(st, str(b, "key"))})
	})
	post("/api/cover/generate", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		prefab, err := ai.GenerateCover(ctx, st, str(b, "key"), "")
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "prefab": prefab, "status": ai.CoverStatusOf(st, str(b, "key"))})
	})
	post("/api/cover/remove", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		library.RemoveGeneratedCover(str(b, "key"))
		core.WriteJSON(w, map[string]any{"ok": true, "status": ai.CoverStatusOf(st, str(b, "key"))})
	})
	// every asset of a project that has no cover, one after the other
	post("/api/cover/batch", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		if err := ai.StartCoverBatch(st, p); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "batch": ai.CoverBatchSnapshot()})
	})
	post("/api/cover/batch/status", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "batch": ai.CoverBatchSnapshot()})
	})
	post("/api/cover/batch/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		ai.CancelCoverBatch()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
}

// knownCard: a card of the library — a scanned asset, or one the store keeps the player's data of (a netdisk
// share, a purchase not downloaded yet).
func knownCard(st *core.Store, key string) bool {
	if key == "" {
		return false
	}
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	if st.User[key] != nil {
		return true
	}
	for _, a := range st.Assets {
		if a.Key == key {
			return true
		}
	}
	return strings.HasPrefix(key, "purchase:") && st.Purchases[strings.TrimPrefix(key, "purchase:")] != nil
}
