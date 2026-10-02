package main

// A base project in one go: a Unity project laid out the way VCC / ALCOM lay theirs out (same
// ProjectSettings as VRChat's avatar template, same vpm-manifest.json), with the VRChat SDK and the
// plugins avatar modding needs, the base body the player picked imported, the AI plugins in, Unity opened.
// ALCOM / VCC see it as one of their own afterwards.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	vrcUnity         = "2022.3.22f1" // the Unity VRChat asks for
	vrcUnityRevision = "887be4894c44"
)

// where VRChat publishes its avatar template's settings (a var so the tests can point it elsewhere)
var templateRaw = "https://raw.githubusercontent.com/vrchat-community/template-avatar/main/ProjectSettings/"

func templateBase() string {
	if base := os.Getenv("VRCLIB_VPM_BASE"); base != "" { // tests
		return strings.TrimRight(base, "/") + "/template/"
	}
	return templateRaw
}

// the ProjectSettings files of VRChat's avatar template; the first one is the one that matters
var templateFiles = []string{"ProjectSettings.asset", "AudioManager.asset", "ClusterInputManager.asset", "DynamicsManager.asset",
	"EditorBuildSettings.asset", "EditorSettings.asset", "GraphicsSettings.asset", "InputManager.asset", "MemorySettings.asset",
	"NavMeshAreas.asset", "PackageManagerSettings.asset", "Physics2DSettings.asset", "PresetManager.asset", "QualitySettings.asset",
	"TagManager.asset", "TimeManager.asset", "UnityConnectSettings.asset", "VFXManager.asset", "VersionControlSettings.asset", "XRSettings.asset"}

// Toolchain is what the computer has for making and opening projects.
type Toolchain struct {
	Alcom      string       `json:"alcom"`          // ALCOM.exe, "" when not found
	AlcomSeen  bool         `json:"alcomSeen"`      // its settings exist, so it was used here
	VCC        string       `json:"vcc"`            // CreatorCompanion.exe
	Hub        string       `json:"hub"`            // Unity Hub
	Unity      string       `json:"unity"`          // the editor VRChat asks for, when installed
	UnityWant  string       `json:"unityWant"`      // its version
	Editors    []string     `json:"editors"`        // every Unity version found
	DefaultDir string       `json:"defaultDir"`     // where new projects go by default
	Projects   []string     `json:"alcomProjects"`  // what ALCOM / VCC list
	Bases      []baseChoice `json:"bases"`          // base bodies in the library
	Plugins    []vpmPlugin  `json:"plugins"`        // what can go into a base project
	Templates  bool         `json:"vccTemplates"`   // VCC's own templates are on this computer
	HubInstall string       `json:"hubInstallLink"` // unityhub:// link that installs the right Unity
}

type baseChoice struct {
	Key   string   `json:"key"`
	Name  string   `json:"name"`
	Bases []string `json:"bases"`
	Size  int64    `json:"size"`
}

func vccSetting(key string) string {
	d := vccDir()
	if d == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(d, "settings.json"))
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &m) != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func toolchain(st *Store) Toolchain {
	t := Toolchain{UnityWant: vrcUnity, Alcom: findAlcom(), VCC: findVCC(), Hub: findUnityHub(), Plugins: basePlugins,
		HubInstall: "unityhub://" + vrcUnity + "/" + vrcUnityRevision}
	t.AlcomSeen = t.Alcom != "" || alcomConfigured()
	eds := unityEditors()
	for v := range eds {
		t.Editors = append(t.Editors, v)
	}
	sort.Strings(t.Editors)
	t.Unity = eds[vrcUnity]
	if d := vccDir(); d != "" {
		t.Templates = statOK(filepath.Join(d, "VRCTemplates", "Avatar", "ProjectSettings", "ProjectSettings.asset"))
		if b, err := os.ReadFile(filepath.Join(d, "settings.json")); err == nil {
			var s struct {
				Projects []string `json:"userProjects"`
			}
			if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &s) == nil {
				t.Projects = s.Projects
			}
		}
	}
	t.DefaultDir = vccSetting("defaultProjectPath")
	st.mu.RLock()
	if t.DefaultDir == "" && len(st.Settings.ProjectRoots) > 0 {
		t.DefaultDir = st.Settings.ProjectRoots[0]
	}
	for _, a := range st.Assets {
		if a.Category != "素体" || len(a.Packages) == 0 && len(a.Archives) == 0 {
			continue
		}
		t.Bases = append(t.Bases, baseChoice{Key: a.Key, Name: a.Name, Bases: a.Bases, Size: a.Size})
	}
	st.mu.RUnlock()
	sort.Slice(t.Bases, func(i, j int) bool { return t.Bases[i].Name < t.Bases[j].Name })
	return t
}

// ---------- the job ----------

type NewProjectReq struct {
	Name    string   `json:"name"`
	Parent  string   `json:"parent"`
	Base    string   `json:"base"`    // library asset key of the base body, "" for none
	Plugins []string `json:"plugins"` // package names ticked
	AIKit   bool     `json:"aiKit"`   // the AI plugins in, Unity opened
}

type NewProjectJob struct {
	ID       int64    `json:"id"`
	Stage    string   `json:"stage"` // prepare, repos, packages, settings, base, kit, done, failed
	Msg      string   `json:"msg"`
	Done     int      `json:"done"`
	Total    int      `json:"total"`
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Packages []string `json:"packages,omitempty"`
	Notes    []string `json:"notes,omitempty"`
	Err      string   `json:"err,omitempty"`
	At       int64    `json:"at"`
	cancel   context.CancelFunc
}

var (
	npMu   sync.Mutex
	npJob  *NewProjectJob
	taskNP = &Task{Name: "newproject", Label: "创建基础工程"}
)

func newProjectSnapshot() *NewProjectJob {
	npMu.Lock()
	defer npMu.Unlock()
	if npJob == nil {
		return nil
	}
	c := *npJob
	c.Packages = append([]string(nil), npJob.Packages...)
	c.Notes = append([]string(nil), npJob.Notes...)
	return &c
}

func setNP(f func(j *NewProjectJob)) {
	npMu.Lock()
	if npJob != nil {
		f(npJob)
	}
	npMu.Unlock()
	bumpRev()
}

var reProjName = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

// StartNewProject checks the request and runs the job in the background.
func StartNewProject(st *Store, req NewProjectReq) error {
	name := strings.TrimSpace(req.Name)
	switch {
	case name == "":
		return errors.New("给工程起个名字")
	case reProjName.MatchString(name) || name == "." || name == "..":
		return errors.New("名字里不能有 \\ / : * ? \" < > |")
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		return errors.New("名字不能以点或空格结尾")
	}
	parent := filepath.Clean(strings.Trim(strings.TrimSpace(req.Parent), `"`))
	if parent == "" || parent == "." {
		return errors.New("选一个放工程的文件夹")
	}
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		return errors.New("放工程的文件夹不存在：" + parent)
	}
	path := filepath.Join(parent, name)
	if ents, err := os.ReadDir(path); err == nil && len(ents) > 0 {
		return errors.New("这个文件夹已经有东西了：" + path)
	}
	if req.Base != "" {
		if a := assetByKey(st, req.Base); a == nil || a.Category != "素体" {
			return errors.New("选的素体不在素材库里")
		}
	}
	if impBusy() {
		return errors.New("正在导入另一个素材，等它完成")
	}
	npMu.Lock()
	if npJob != nil && npJob.Stage != "done" && npJob.Stage != "failed" {
		npMu.Unlock()
		return errors.New("正在创建另一个工程")
	}
	ctx, cancel := context.WithCancel(context.Background())
	npJob = &NewProjectJob{ID: time.Now().UnixNano(), Stage: "prepare", Msg: "准备", Path: path, Name: name, At: time.Now().Unix(), cancel: cancel}
	npMu.Unlock()
	bumpRev()
	go run(taskNP, func() {
		defer cancel()
		if err := runNewProject(ctx, st, req, path); err != nil {
			setNP(func(j *NewProjectJob) { j.Stage, j.Err, j.Msg = "failed", err.Error(), err.Error() })
			taskNP.Set(0, 0, err.Error())
			logf("创建工程失败：%v", err)
		}
	})
	return nil
}

func CancelNewProject() {
	npMu.Lock()
	defer npMu.Unlock()
	if npJob != nil && npJob.cancel != nil {
		npJob.cancel()
	}
}

func DismissNewProject() {
	npMu.Lock()
	if npJob != nil && (npJob.Stage == "done" || npJob.Stage == "failed") {
		npJob = nil
	}
	npMu.Unlock()
	bumpRev()
}

func impBusy() bool {
	j := importSnapshot()
	return j != nil && j.Stage != "done" && j.Stage != "failed"
}

func assetByKey(st *Store, key string) *Asset {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, a := range st.Assets {
		if a.Key == key {
			return a
		}
	}
	return nil
}

func runNewProject(ctx context.Context, st *Store, req NewProjectReq, path string) error {
	stage := func(s, msg string, done, total int) {
		setNP(func(j *NewProjectJob) { j.Stage, j.Msg, j.Done, j.Total = s, msg, done, total })
		taskNP.Set(done, total, msg)
	}
	note := func(s string) { setNP(func(j *NewProjectJob) { j.Notes = append(j.Notes, s) }) }
	// 1. the folders
	for _, d := range []string{"Assets", "Packages", "ProjectSettings"} {
		if err := os.MkdirAll(filepath.Join(path, d), 0755); err != nil {
			return fmt.Errorf("建不了工程文件夹：%v", err)
		}
	}
	// 2. which packages
	stage("repos", "读取插件仓库", 0, 0)
	idx, err := loadIndex(ctx, st, func(m string) { stage("repos", m, 0, 0) })
	if err != nil {
		return err
	}
	for _, s := range idx.stale {
		note("仓库信息用的是本机缓存：" + s)
	}
	wanted := map[string]string{vpmResolverPkg: "*"}
	for _, p := range basePlugins {
		if p.Fixed || containsStr(req.Plugins, p.Pkg) {
			wanted[p.Pkg] = "*"
		}
	}
	for _, p := range req.Plugins { // anything else the window asked for by name
		if _, known := wanted[p]; !known && validPkgName(p) {
			wanted[p] = "*"
		}
	}
	picked, err := idx.resolve(wanted)
	if err != nil {
		return err
	}
	names := installOrder(picked)
	// what the project cannot do without: the resolver and the SDK (with what they depend on)
	required := map[string]bool{vpmResolverPkg: true}
	for _, p := range basePlugins {
		if p.Fixed {
			required[p.Pkg] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for n := range required {
			for d := range picked[n].Deps {
				if _, ok := picked[d]; ok && !required[d] {
					required[d], changed = true, true
				}
			}
		}
	}
	// 3. in they go, dependencies first; an optional plugin that cannot be fetched is left out with a note
	failed := map[string]string{}
	for i, n := range names {
		v := picked[n]
		for d := range v.Deps {
			if why, bad := failed[d]; bad {
				failed[n] = "它依赖的 " + d + " 没装上（" + why + "）"
				break
			}
		}
		if why, bad := failed[n]; bad {
			if required[n] {
				return errors.New(n + "：" + why)
			}
			note("没装 " + n + "：" + why + "。可以之后在 ALCOM / VCC 里装")
			continue
		}
		stage("packages", "放入 "+n+" "+v.Version, i, len(names))
		zp, err := fetchZip(ctx, st, v, func(m string) { stage("packages", m, i, len(names)) })
		if err == nil {
			if e := extractPackage(zp, path, n); e != nil {
				err = fmt.Errorf("解压失败：%v", e)
			}
		}
		if err != nil {
			if required[n] {
				return err
			}
			failed[n] = err.Error()
			note("没装 " + n + " " + v.Version + "：" + err.Error() + "。可以之后在 ALCOM / VCC 里装")
			continue
		}
		setNP(func(j *NewProjectJob) { j.Packages = append(j.Packages, n+" "+v.Version) })
	}
	for n := range failed {
		delete(picked, n)
		delete(wanted, n)
	}
	if err := writeVpmManifest(path, wanted, picked); err != nil {
		return err
	}
	// 4. the project's own files
	stage("settings", "写工程设置", len(names), len(names))
	how, err := writeProjectSettings(ctx, st, path, req.Name)
	if err != nil {
		return err
	}
	note(how)
	if err := writeUnityManifest(path); err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Join(path, "Assets", safeFileName(req.Name)), 0755) // the player's own folder, VRCAM style
	// 5. known to this program and to ALCOM / VCC
	registerProject(st, path)
	if err := addToVCCList(path); err == nil {
		note("已加进 ALCOM / VCC 的工程列表")
	}
	// 6. the base body
	if req.Base != "" {
		stage("base", "导入素体", 0, 0)
		a := assetByKey(st, req.Base)
		if a == nil {
			return errors.New("素体不在素材库里了")
		}
		if err := StartImport(st, ImportReq{Key: a.Key, Project: path, Recycle: false}); err != nil {
			return errors.New("导入素体没能开始：" + err.Error())
		}
		for {
			j := importSnapshot()
			if j == nil {
				return errors.New("导入素体中断了")
			}
			if j.Stage == "failed" {
				return errors.New("导入素体失败：" + j.Err)
			}
			if j.Stage == "done" {
				note(j.Msg)
				break
			}
			setNP(func(np *NewProjectJob) { np.Msg = j.Msg })
			select {
			case <-ctx.Done():
				return errors.New("已取消")
			case <-time.After(700 * time.Millisecond):
			}
		}
	}
	// 7. the AI plugins and Unity
	if req.AIKit {
		stage("kit", "装 AI 插件并打开 Unity", 0, 0)
		if n, err := installAIKit(path); err != nil {
			note("AI 插件：" + err.Error())
		} else {
			note(n)
		}
	}
	if unityEditors()[vrcUnity] == "" {
		note("这台电脑上没有 Unity " + vrcUnity + "：在 Unity Hub 里装上这个版本再打开工程")
	}
	stage("done", "工程建好了", 1, 1)
	taskNP.Set(1, 1, "工程建好了："+req.Name)
	logf("新工程 %s：%d 个包", path, len(names))
	return nil
}

func validPkgName(s string) bool {
	if s == "" || len(s) > 120 {
		return false
	}
	for _, c := range s {
		if !(c == '.' || c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// installOrder: the packages with what they depend on first, alphabetical among equals.
func installOrder(picked map[string]vpmVersion) []string {
	rest := make([]string, 0, len(picked))
	for n := range picked {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	var out []string
	done := map[string]bool{}
	for len(rest) > 0 {
		var next []string
		progressed := false
		for _, n := range rest {
			ready := true
			for d := range picked[n].Deps {
				if _, in := picked[d]; in && !done[d] {
					ready = false
					break
				}
			}
			if ready {
				out = append(out, n)
				done[n], progressed = true, true
			} else {
				next = append(next, n)
			}
		}
		if !progressed { // a cycle: take them as they are
			out = append(out, next...)
			break
		}
		rest = next
	}
	return out
}

func registerProject(st *Store, p string) {
	st.mu.Lock()
	known := false
	for _, pr := range st.Projects {
		if pathKey(pr.Path) == pathKey(p) {
			known = true
		}
	}
	if !known {
		st.Projects = append(st.Projects, ProjectInfo{Name: filepath.Base(p), Path: p})
		sort.Slice(st.Projects, func(i, j int) bool { return st.Projects[i].Name < st.Projects[j].Name })
		inRoot := false
		for _, r := range st.Settings.ProjectRoots {
			if strings.HasPrefix(pathKey(p), pathKey(r)+string(filepath.Separator)) {
				inRoot = true
			}
		}
		if !inRoot {
			st.Settings.ProjectRoots = cleanPaths(append(st.Settings.ProjectRoots, p))
		}
	}
	st.mu.Unlock()
	if !known {
		_ = st.Save()
		bumpRev()
	}
}

// addToVCCList puts the project into ALCOM / VCC's settings.json, so their project list shows it next time.
func addToVCCList(p string) error {
	d := vccDir()
	if d == "" {
		return errors.New("no vcc")
	}
	f := filepath.Join(d, "settings.json")
	b, err := os.ReadFile(f)
	if err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &m); err != nil {
		return err
	}
	var list []string
	_ = json.Unmarshal(m["userProjects"], &list)
	for _, x := range list {
		if pathKey(x) == pathKey(p) {
			return nil
		}
	}
	list = append([]string{p}, list...)
	m["userProjects"], _ = json.Marshal(list)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(f+".tmp", out, 0644); err != nil {
		return err
	}
	return os.Rename(f+".tmp", f)
}

// ---------- ProjectSettings ----------

// writeProjectSettings fills ProjectSettings/: VCC's own avatar template when it is on this computer, else
// VRChat's template from its repository (kept for next time), else a small set of our own. Says which.
func writeProjectSettings(ctx context.Context, st *Store, project, name string) (string, error) {
	dir := filepath.Join(project, "ProjectSettings")
	how := ""
	if d := vccDir(); d != "" {
		src := filepath.Join(d, "VRCTemplates", "Avatar", "ProjectSettings")
		if ents, err := os.ReadDir(src); err == nil {
			n := 0
			for _, e := range ents {
				if e.IsDir() || e.Name() == "ProjectVersion.txt" {
					continue
				}
				if b, err := os.ReadFile(filepath.Join(src, e.Name())); err == nil && os.WriteFile(filepath.Join(dir, e.Name()), b, 0644) == nil {
					n++
				}
			}
			if n > 0 && statOK(filepath.Join(dir, "ProjectSettings.asset")) {
				how = "工程设置来自 VCC 的头像模板"
			}
		}
	}
	if how == "" {
		cache := filepath.Join(dataDir, "vpm", "template-avatar")
		_ = os.MkdirAll(cache, 0755)
		got := 0
		for _, f := range templateFiles {
			b, err := os.ReadFile(filepath.Join(cache, f))
			if err != nil {
				if b, err = vpmGet(ctx, st, templateBase()+f, 30*time.Second); err != nil {
					if f == "ProjectSettings.asset" {
						break
					}
					continue
				}
				_ = os.WriteFile(filepath.Join(cache, f), b, 0644)
			}
			if os.WriteFile(filepath.Join(dir, f), b, 0644) == nil {
				got++
			}
		}
		if got > 0 && statOK(filepath.Join(dir, "ProjectSettings.asset")) {
			how = "工程设置来自 VRChat 官方头像模板"
		}
	}
	if how == "" {
		if err := os.WriteFile(filepath.Join(dir, "ProjectSettings.asset"), []byte(minimalProjectSettings(name)), 0644); err != nil {
			return "", err
		}
		how = "拿不到 VRChat 的模板，工程设置用了最少的一份（线性颜色空间等）；VRChat SDK 打开时会补齐其余"
	}
	// the product name is the project's
	if b, err := os.ReadFile(filepath.Join(dir, "ProjectSettings.asset")); err == nil {
		s := regexp.MustCompile(`(?m)^(\s*productName:).*$`).ReplaceAllString(string(b), "${1} "+name)
		_ = os.WriteFile(filepath.Join(dir, "ProjectSettings.asset"), []byte(s), 0644)
	}
	ver := "m_EditorVersion: " + vrcUnity + "\nm_EditorVersionWithRevision: " + vrcUnity + " (" + vrcUnityRevision + ")\n"
	return how, os.WriteFile(filepath.Join(dir, "ProjectVersion.txt"), []byte(ver), 0644)
}

func minimalProjectSettings(name string) string {
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return "%YAML 1.1\n%TAG !u! tag:unity3d.com,2011:\n--- !u!129 &1\nPlayerSettings:\n  m_ObjectHideFlags: 0\n  serializedVersion: 26\n" +
		"  productGUID: " + hex.EncodeToString(id) + "\n  companyName: DefaultCompany\n  productName: " + name + "\n" +
		"  defaultScreenWidth: 1024\n  defaultScreenHeight: 768\n  m_ActiveColorSpace: 1\n  gpuSkinning: 1\n" +
		"  apiCompatibilityLevelPerPlatform:\n    Standalone: 6\n  apiCompatibilityLevel: 6\n"
}

// writeUnityManifest: Packages/manifest.json with the packages a Unity 2022.3 project starts with (what the
// avatar template lists). The VPM packages are embedded in Packages/ and need no entry.
func writeUnityManifest(project string) error {
	deps := map[string]string{
		"com.unity.ide.rider": "3.0.26", "com.unity.ide.visualstudio": "2.0.22", "com.unity.ide.vscode": "1.2.5",
		"com.unity.test-framework": "1.1.29", "com.unity.textmeshpro": "2.1.6", "com.unity.timeline": "1.7.5", "com.unity.ugui": "1.0.0",
	}
	for _, m := range []string{"ai", "androidjni", "animation", "assetbundle", "audio", "cloth", "director", "imageconversion", "imgui",
		"jsonserialize", "particlesystem", "physics", "physics2d", "screencapture", "terrain", "terrainphysics", "tilemap", "ui", "uielements",
		"umbra", "unityanalytics", "unitywebrequest", "unitywebrequestassetbundle", "unitywebrequestaudio", "unitywebrequesttexture",
		"unitywebrequestwww", "vehicles", "video", "vr", "wind", "xr"} {
		deps["com.unity.modules."+m] = "1.0.0"
	}
	b, _ := json.MarshalIndent(map[string]any{"dependencies": deps}, "", "  ")
	return os.WriteFile(filepath.Join(project, "Packages", "manifest.json"), b, 0644)
}
