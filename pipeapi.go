package main

// The 流水线 page's requests: what tools this computer has, what a project holds, a new base project.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func registerPipe(st *Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	fail := func(w http.ResponseWriter, err error) { writeJSON(w, map[string]any{"ok": false, "err": err.Error()}) }

	post("/api/pipe/toolchain", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		writeJSON(w, map[string]any{"ok": true, "toolchain": toolchain(st)})
	})
	// the project's assets for the station list, with Unity's knowledge when the editor is open
	post("/api/pipe/assets", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := knownProject(st, str(b, "project"))
		if !ok {
			fail(w, errors.New("不在工程列表里"))
			return
		}
		var fresh bool
		_ = json.Unmarshal(b["fresh"], &fresh)
		list, ok := projectAssetsOf(st, p, fresh)
		if !ok {
			fail(w, errors.New("这不是 Unity 工程（找不到 Assets 和 ProjectSettings 文件夹）"))
			return
		}
		if list == nil {
			list = []ProjAsset{}
		}
		writeJSON(w, map[string]any{"ok": true, "assets": list, "kit": aiKitStatus(p)})
	})
	post("/api/pipe/create", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var req NewProjectReq
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &req)
		if err := StartNewProject(st, req); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "job": newProjectSnapshot()})
	})
	post("/api/pipe/create/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		CancelNewProject()
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/pipe/create/dismiss", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		DismissNewProject()
		writeJSON(w, map[string]any{"ok": true})
	})
	// ALCOM / VCC / Unity Hub, when this computer has them
	post("/api/pipe/launch", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var exe, name string
		switch str(b, "what") {
		case "alcom":
			exe, name = findAlcom(), "ALCOM"
		case "vcc":
			exe, name = findVCC(), "VCC"
		case "hub":
			exe, name = findUnityHub(), "Unity Hub"
		default:
			fail(w, errors.New("不认识要打开的程序"))
			return
		}
		if exe == "" {
			fail(w, errors.New("这台电脑上没找到 "+name))
			return
		}
		if err := launchApp(exe); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	// a folder for a new project, typed or picked: is it a place a project can go?
	post("/api/pipe/parent", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p := filepath.Clean(strings.Trim(strings.TrimSpace(str(b, "path")), `"`))
		if fi, err := os.Stat(p); p == "" || p == "." || err != nil || !fi.IsDir() {
			fail(w, errors.New("文件夹不存在："+p))
			return
		}
		taken := false
		if name := strings.TrimSpace(str(b, "name")); name != "" {
			if ents, err := os.ReadDir(filepath.Join(p, name)); err == nil && len(ents) > 0 {
				taken = true
			}
		}
		writeJSON(w, map[string]any{"ok": true, "path": p, "taken": taken})
	})
}
