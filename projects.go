package main

// The 工程 page: one card per Unity project, with its cover, Unity version, when it was last open and how many
// library assets it uses, and a button that opens it in the right Unity version. Managing packages and avatars
// inside a project is left to VCC / ALCOM.
//
// The cover is taken inside Unity by a small editor package the player can put into a project
// (unityhelper/com.miovrc.projectcard): it saves UserSettings/MioVRCA/cover_<time>.png.

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed unityhelper
var unityHelper embed.FS

const coverPkg = "com.miovrc.projectcard"

type ProjectCard struct {
	ProjectInfo
	Unity   string `json:"unity,omitempty"`   // editor version the project is on
	Opened  int64  `json:"opened,omitempty"`  // last time Unity had it open
	Running bool   `json:"running,omitempty"` // open in Unity now
	Cover   string `json:"cover,omitempty"`   // the newest picture the cover package took
	CoverAt int64  `json:"coverAt,omitempty"`
	Helper  bool   `json:"helper,omitempty"` // the cover package is in the project
	Editor  bool   `json:"editor"`           // its Unity version is installed on this computer
}

func projectCards(st *Store) []ProjectCard {
	st.mu.RLock()
	ps := append([]ProjectInfo{}, st.Projects...)
	st.mu.RUnlock()
	eds := unityEditors()
	out := make([]ProjectCard, 0, len(ps))
	for _, p := range ps {
		c := ProjectCard{ProjectInfo: p, Unity: projectUnityVersion(p.Path), Opened: projectOpened(p.Path), Running: projectRunning(p.Path)}
		c.Cover, c.CoverAt = projectCover(p.Path)
		c.Helper = statOK(filepath.Join(p.Path, "Packages", coverPkg, "package.json"))
		c.Editor = c.Unity != "" && eds[c.Unity] != ""
		out = append(out, c)
	}
	return out
}

// knownProject: a project on the 工程 page (the buttons act only on those).
func knownProject(st *Store, p string) (string, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, pr := range st.Projects {
		if pathKey(pr.Path) == pathKey(p) {
			return pr.Path, true
		}
	}
	return "", false
}

func projectUnityVersion(p string) string {
	f, err := os.Open(filepath.Join(p, "ProjectSettings", "ProjectVersion.txt"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "m_EditorVersion:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// projectOpened: Unity writes these while the project is open and when it closes.
func projectOpened(p string) int64 {
	var t int64
	for _, f := range []string{"Library/LastSceneManagerSetup.txt", "UserSettings/EditorUserSettings.asset",
		"Library/EditorUserBuildSettings.asset", "Temp/UnityLockfile"} {
		if fi, err := os.Stat(filepath.Join(p, filepath.FromSlash(f))); err == nil && fi.ModTime().Unix() > t {
			t = fi.ModTime().Unix()
		}
	}
	return t
}

// projectRunning: Unity keeps Temp/UnityLockfile open while the project is open; one left by a crash can be opened.
func projectRunning(p string) bool {
	lock := filepath.Join(p, "Temp", "UnityLockfile")
	if !statOK(lock) {
		return false
	}
	if runtime.GOOS != "windows" {
		return true
	}
	f, err := os.OpenFile(lock, os.O_RDWR, 0)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	f.Close()
	return false
}

func projectCover(p string) (string, int64) {
	ms, _ := filepath.Glob(filepath.Join(p, "UserSettings", "MioVRCA", "cover_*.png"))
	best, at := "", int64(0)
	for _, m := range ms {
		if fi, err := os.Stat(m); err == nil && fi.Size() > 0 && fi.ModTime().Unix() >= at {
			best, at = m, fi.ModTime().Unix()
		}
	}
	return best, at
}

// ---------- Unity editors on this computer ----------

var (
	edMu    sync.Mutex
	edCache map[string]string
	edAt    time.Time
)

// unityEditors: installed editors by version → Unity.exe (Unity Hub's install folders and the editors it was
// pointed at, and the ones VCC / ALCOM know).
func unityEditors() map[string]string {
	edMu.Lock()
	defer edMu.Unlock()
	if edCache != nil && time.Since(edAt) < 30*time.Second {
		return edCache
	}
	out := map[string]string{}
	exe := "Unity.exe"
	if runtime.GOOS != "windows" {
		exe = "Unity"
	}
	inDir := func(dir string) { // <dir>\<version>\Editor\Unity.exe
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if p := filepath.Join(dir, e.Name(), "Editor", exe); e.IsDir() && out[e.Name()] == "" && statOK(p) {
				out[e.Name()] = p
			}
		}
	}
	var dirs []string
	if v := os.Getenv("VRCLIB_UNITY_DIRS"); v != "" { // tests
		dirs = append(dirs, filepath.SplitList(v)...)
	}
	appData := os.Getenv("APPDATA")
	if appData != "" {
		if b, err := os.ReadFile(filepath.Join(appData, "UnityHub", "secondaryInstallPath.json")); err == nil {
			var s string
			if json.Unmarshal(bytes.TrimSpace(b), &s) == nil && s != "" {
				dirs = append(dirs, s)
			}
		}
	}
	for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
		if pf := os.Getenv(env); pf != "" {
			dirs = append(dirs, filepath.Join(pf, "Unity", "Hub", "Editor"))
		}
	}
	for _, d := range dirs {
		inDir(d)
	}
	// editors added to the Hub by hand, and the ones VCC / ALCOM use
	var files []string
	if appData != "" {
		files = append(files, filepath.Join(appData, "UnityHub", "editors-v2.json"), filepath.Join(appData, "UnityHub", "editors.json"))
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		files = append(files, filepath.Join(la, "VRChatCreatorCompanion", "settings.json"), filepath.Join(la, "VRChatCreatorCompanion", "vrc-get-settings.json"))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var v any
		if json.Unmarshal(b, &v) == nil {
			findEditors(v, out)
		}
	}
	edCache, edAt = out, time.Now()
	return out
}

// findEditors: objects like {"version": "2022.3.22f1", "location": ["…\Unity.exe"]} or {"path": …, "version": …}.
func findEditors(v any, out map[string]string) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			findEditors(e, out)
		}
	case map[string]any:
		ver, exe := "", ""
		for k, e := range x {
			switch strings.ToLower(k) {
			case "version":
				ver, _ = e.(string)
			case "location", "path":
				switch l := e.(type) {
				case string:
					exe = l
				case []any:
					if len(l) > 0 {
						exe, _ = l[0].(string)
					}
				}
			}
		}
		if ver != "" && exe != "" && strings.EqualFold(path.Base(filepath.ToSlash(exe)), "unity.exe") && out[ver] == "" && statOK(exe) {
			out[ver] = exe
		}
		for _, e := range x {
			findEditors(e, out)
		}
	}
}

// openInUnity starts the project in the Unity version it is on.
func openInUnity(p string) error {
	if projectRunning(p) {
		return errors.New("这个工程已经在 Unity 里打开了")
	}
	ver := projectUnityVersion(p)
	if ver == "" {
		return errors.New("读不到这个工程用的 Unity 版本（ProjectSettings/ProjectVersion.txt）")
	}
	exe := unityEditors()[ver]
	if exe == "" {
		return fmt.Errorf("这台电脑上没找到 Unity %s：在 Unity Hub 里装上这个版本，或者从 Unity Hub、VCC 打开", ver)
	}
	cmd := exec.Command(exe, "-projectPath", p)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Unity 没能启动：%v", err)
	}
	logf("打开工程 %s（Unity %s）", p, ver)
	return cmd.Process.Release()
}

// ---------- the cover package ----------

// setCoverHelper puts the cover package into the project, or takes it (and the pictures it took) out again.
func setCoverHelper(p string, on bool) error {
	dir := filepath.Join(p, "Packages", coverPkg)
	ours := func() bool {
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		var m struct {
			Name   string `json:"name"`
			Author struct {
				Name string `json:"name"`
			} `json:"author"`
		}
		return err == nil && json.Unmarshal(b, &m) == nil && m.Name == coverPkg && m.Author.Name == "MioVRC"
	}
	if statOK(dir) && !ours() {
		return errors.New("工程的 Packages 里已经有一个 " + coverPkg + "，不是本软件放的，没有动它")
	}
	if !on {
		if statOK(dir) {
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("没能删掉封面插件：%v（Unity 开着的话，关掉再试）", err)
			}
		}
		// its pictures; what else is in the folder (the AI assistant's) stays
		ms, _ := filepath.Glob(filepath.Join(p, "UserSettings", "MioVRCA", "cover_*.png"))
		for _, m := range ms {
			_ = os.Remove(m)
		}
		_ = os.Remove(filepath.Join(p, "UserSettings", "MioVRCA"))
		logf("工程封面插件已移除：%s", p)
		return nil
	}
	if err := putPackage(p, coverPkg, writeEmbeddedPackage(coverPkg)); err != nil {
		return fmt.Errorf("没能放进封面插件：%v", err)
	}
	logf("工程封面插件已放进 %s", p)
	return nil
}

// helperFiles: the package's files, for the tests.
func helperFiles() []string {
	var out []string
	_ = fs.WalkDir(unityHelper, "unityhelper", func(fp string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, fp)
		}
		return nil
	})
	sort.Strings(out)
	return out
}
