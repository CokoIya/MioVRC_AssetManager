package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/follow"
	"vrclib/internal/library"
	"vrclib/internal/pandl"
	"vrclib/internal/purchases"
	"vrclib/internal/unity"
)

// panSeen: the netdisk downloads whose end has been told to the update notes already.
var panSeen struct {
	mu   sync.Mutex
	done map[int64]bool
}

// notePanDownloads: a netdisk card that was downloaded (from anywhere in the window) has what the share
// holds now, so its change notice is dealt with.
func notePanDownloads(st *core.Store) {
	jobs := pandl.PanJobsSnapshot()
	panSeen.mu.Lock()
	var fresh []pandl.PanJob
	live := map[int64]bool{}
	for _, j := range jobs {
		live[j.ID] = true
		if j.Stage == "done" && !panSeen.done[j.ID] {
			fresh = append(fresh, j)
		}
	}
	if panSeen.done == nil {
		panSeen.done = map[int64]bool{}
	}
	for id := range panSeen.done {
		if !live[id] {
			delete(panSeen.done, id)
		}
	}
	for _, j := range fresh {
		panSeen.done[j.ID] = true
	}
	panSeen.mu.Unlock()
	for _, j := range fresh {
		library.PanFetched(st, j.Key, j.Paths)
	}
}

// registerLibTools: library tools: updates carried into projects, tidying, export and import.
func registerLibTools(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	fail := func(w http.ResponseWriter, err error) {
		core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
	}
	jobID := func(b map[string]json.RawMessage) int64 {
		id, _ := strconv.ParseInt(str(b, "id"), 10, 64)
		return id
	}
	dlDir := func() string {
		st.Mu.RLock()
		defer st.Mu.RUnlock()
		return purchases.DownloadDir(st)
	}
	// an import replaces the library under everything else: not while something writes into it
	library.ImportBusy = func() bool {
		if library.PipelineBusy() || core.Downloading.Load() > 0 || follow.Active() {
			return true
		}
		j := unity.ImportSnapshot()
		return j != nil && j.Stage != "done" && j.Stage != "failed"
	}

	// ---------- updates ----------
	post("/api/libtools/updates", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		notePanDownloads(st)
		core.WriteJSON(w, map[string]any{"ok": true, "notes": library.UpdateNotes(st), "jobs": follow.Snapshot()})
	})
	post("/api/libtools/update/start", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var projects []string
		_ = json.Unmarshal(b["projects"], &projects)
		var recycle bool
		_ = json.Unmarshal(b["recycle"], &recycle)
		j, err := follow.Start(st, str(b, "key"), projects, recycle)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "job": j})
	})
	post("/api/libtools/update/retry", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		j, err := follow.Retry(st, jobID(b))
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "job": j})
	})
	post("/api/libtools/update/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		follow.Cancel(jobID(b))
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/libtools/update/dismiss", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		follow.Dismiss(jobID(b))
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// "已是最新": the player has the new files already, or does not want them
	post("/api/libtools/update/seen", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		key := str(b, "key")
		if key == "" {
			fail(w, errors.New("未找到该素材"))
			return
		}
		library.AckUpdate(st, key, nil)
		core.WriteJSON(w, map[string]any{"ok": true})
	})

	// ---------- tidying ----------
	// have: the scans whose results the window holds already (a result can be large, and the window asks
	// again every moment while another scan runs)
	post("/api/libtools/tidy/status", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		d := dlDir()
		v := library.TidyStatus(st, d)
		var have struct{ Dup, Arc int64 }
		_ = json.Unmarshal(b["have"], &have)
		same := map[string]bool{}
		if v.Dup.Dup != nil && have.Dup != 0 && v.Dup.Dup.Seq == have.Dup {
			v.Dup.Dup, same["dup"] = nil, true
		}
		if v.Arc.Arc != nil && have.Arc != 0 && v.Arc.Arc.Seq == have.Arc {
			v.Arc.Arc, same["arc"] = nil, true
		}
		core.WriteJSON(w, map[string]any{"ok": true, "tidy": v, "dlDir": d, "same": same})
	})
	post("/api/libtools/tidy/scan", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var minSize int64
		_ = json.Unmarshal(b["minSize"], &minSize)
		if err := library.TidyStart(st, str(b, "what"), dlDir(), minSize); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/libtools/tidy/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		library.TidyCancel(str(b, "what"))
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/libtools/tidy/recycle", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var paths []string
		_ = json.Unmarshal(b["paths"], &paths)
		var seq int64
		_ = json.Unmarshal(b["seq"], &seq)
		res, err := library.TidyRecycle(st, str(b, "what"), paths, dlDir(), seq)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "result": res})
	})

	// ---------- moving the library to another computer ----------
	post("/api/libtools/transfer/state", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "transfer": library.TransferState()})
	})
	post("/api/libtools/export", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if err := library.StartExport(st, str(b, "dir"), dlDir()); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/libtools/pickfile", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, err := core.PickFile("选择导出的素材库文件", "MioVRCA 导出文件 (*.zip)", "*.zip")
		switch {
		case errors.Is(err, core.ErrPickCancelled):
			core.WriteJSON(w, map[string]any{"ok": false, "cancelled": true})
		case err != nil:
			core.WriteJSON(w, map[string]any{"ok": false, "err": "无法打开文件选择窗口，请直接粘贴文件路径：" + err.Error()})
		default:
			core.WriteJSON(w, map[string]any{"ok": true, "path": p})
		}
	})
	post("/api/libtools/import/inspect", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		info, err := library.InspectExport(st, str(b, "path"))
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "info": info})
	})
	post("/api/libtools/import/apply", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var opt library.ImportOptions
		raw, _ := json.Marshal(b)
		if err := json.Unmarshal(raw, &opt); err != nil {
			fail(w, errors.New(badBody("导入选项", err)))
			return
		}
		res, err := library.ImportLibrary(st, opt)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "result": res})
	})
	post("/api/libtools/import/undo", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if err := library.UndoImport(st); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/libtools/import/maproot", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		n, err := library.MapPending(st, str(b, "root"), str(b, "to"))
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "assets": n})
	})
	post("/api/libtools/import/droproot", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if err := library.DropPending(str(b, "root")); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// ---------- another asset manager's library (Avatar Explorer, KonoAsset) ----------
	type manager struct {
		name    string
		folders func() []string
		preview func(*core.Store, string) (*library.AEPreview, error)
		apply   func(*core.Store, string, bool) (*library.AEResult, error)
	}
	for _, m := range []manager{{"ae", library.AEFolders, library.PreviewAE, library.ApplyAE}, {"ka", library.KAFolders, library.PreviewKA, library.ApplyKA}} {
		m := m
		post("/api/libtools/"+m.name+"/find", func(w http.ResponseWriter, b map[string]json.RawMessage) {
			found := []*library.AEPreview{}
			for _, d := range m.folders() {
				if pv, err := m.preview(st, d); err == nil {
					found = append(found, pv)
				}
			}
			core.WriteJSON(w, map[string]any{"ok": true, "found": found})
		})
		post("/api/libtools/"+m.name+"/preview", func(w http.ResponseWriter, b map[string]json.RawMessage) {
			pv, err := m.preview(st, str(b, "dir"))
			if err != nil {
				fail(w, err)
				return
			}
			core.WriteJSON(w, map[string]any{"ok": true, "preview": pv})
		})
		post("/api/libtools/"+m.name+"/apply", func(w http.ResponseWriter, b map[string]json.RawMessage) {
			if library.PipelineBusy() {
				fail(w, errors.New("正在扫描素材库，请在其完成后重试"))
				return
			}
			var addRoot bool
			_ = json.Unmarshal(b["addRoot"], &addRoot)
			dir := str(b, "dir")
			res, err := m.apply(st, dir, addRoot)
			if err != nil {
				fail(w, err)
				return
			}
			st.Mu.RLock()
			auto := st.Settings.AutoBooth
			st.Mu.RUnlock()
			switch {
			case res.AddRoot != "":
				// its asset folder was added: scan it, then take over what is known about the assets in it
				go func() {
					library.StartPipeline(st, true, true, false, false, nil)
					for i := 0; i < 3600 && library.PipelineBusy(); i++ {
						time.Sleep(500 * time.Millisecond)
					}
					if core.Quitting.Load() {
						return
					}
					if _, err := m.apply(st, dir, false); err == nil {
						library.StartPipeline(st, false, false, auto, false, nil)
					}
				}()
			case res.Booth > 0:
				library.StartPipeline(st, false, false, auto, false, nil) // the Booth pages of the newly linked ones
			}
			core.WriteJSON(w, map[string]any{"ok": true, "result": res})
		})
	}
	// ---------- covers from the previews inside unitypackages ----------
	post("/api/libtools/pkgcover/status", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "status": library.PkgCoverStatusOf(st, str(b, "key")), "batch": library.PkgBatchSnapshot()})
	})
	post("/api/libtools/pkgcover/extract", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		key := str(b, "key")
		c, err := library.ExtractPkgCover(st, key)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "cover": c, "status": library.PkgCoverStatusOf(st, key)})
	})
	post("/api/libtools/pkgcover/remove", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		key := str(b, "key")
		library.RemovePkgCover(st, key)
		core.WriteJSON(w, map[string]any{"ok": true, "status": library.PkgCoverStatusOf(st, key)})
	})
	// every asset of the library that shows no picture, one after the other
	post("/api/libtools/pkgcover/batch", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if err := library.StartPkgBatch(st); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "batch": library.PkgBatchSnapshot()})
	})
	post("/api/libtools/pkgcover/batch/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		library.CancelPkgBatch()
		core.WriteJSON(w, map[string]any{"ok": true, "batch": library.PkgBatchSnapshot()})
	})
}
