package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vrclib/internal/ai"
	"vrclib/internal/core"
	"vrclib/internal/unity"
)

func registerPipe(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	fail := func(w http.ResponseWriter, err error) {
		core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
	}

	post("/api/pipe/toolchain", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "toolchain": unity.FindToolchain(st)})
	})
	// the project's assets for the station list, with Unity's knowledge when the editor is open
	post("/api/pipe/assets", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := unity.KnownProject(st, str(b, "project"))
		if !ok {
			fail(w, errors.New("该工程不在工程列表中"))
			return
		}
		var fresh bool
		_ = json.Unmarshal(b["fresh"], &fresh)
		list, ok := ai.ProjectAssetsOf(st, p, fresh)
		if !ok {
			fail(w, errors.New("该文件夹不是 Unity 工程（未找到 Assets 和 ProjectSettings 文件夹）"))
			return
		}
		if list == nil {
			list = []ai.ProjAsset{}
		}
		core.WriteJSON(w, map[string]any{"ok": true, "assets": list, "kit": unity.AIKitStatus(p)})
	})
	post("/api/pipe/create", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var req unity.NewProjectReq
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &req)
		if err := unity.StartNewProject(st, req); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "job": unity.NewProjectSnapshot()})
	})
	post("/api/pipe/create/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		unity.CancelNewProject()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/pipe/create/dismiss", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		unity.DismissNewProject()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// ALCOM / VCC / Unity Hub, when this computer has them
	post("/api/pipe/launch", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var exe, name string
		switch str(b, "what") {
		case "alcom":
			exe, name = unity.FindAlcom(), "ALCOM"
		case "vcc":
			exe, name = unity.FindVCC(), "VCC"
		case "hub":
			exe, name = unity.FindUnityHub(), "Unity Hub"
		default:
			fail(w, errors.New("无法识别要打开的程序"))
			return
		}
		if exe == "" {
			fail(w, errors.New("本机未找到 "+name))
			return
		}
		if err := unity.LaunchApp(exe); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
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
			taken = unity.NewProjectTaken(filepath.Join(p, name)) // what a failed attempt left there does not count
		}
		core.WriteJSON(w, map[string]any{"ok": true, "path": p, "taken": taken})
	})
}
