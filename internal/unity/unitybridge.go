package unity

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"vrclib"
	"vrclib/internal/core"
	"vrclib/internal/webpane"
)

const (
	PipePkg       = "com.miovrc.pipeline"
	SkillsPkg     = "com.besty.unity-skills"
	SkillsVersion = "2.8.4"
	SkillsZip     = "unitykit/unity-skills-" + SkillsVersion + ".zip"
	KitMarker     = ".miovrca" // in a package folder this program put there
)

func BridgeDir(p string) string { return filepath.Join(p, "UserSettings", "MioVRCA", "bridge") }

// BridgeAlive is the heartbeat the pipeline package writes every two seconds.
type BridgeAlive struct {
	Bridge    string `json:"bridge"`
	Pid       int    `json:"pid"`
	Unity     string `json:"unity"`
	Project   string `json:"project"`
	Compiling bool   `json:"compiling"`
	Playing   bool   `json:"playing"`
	MA        string `json:"ma"`
	SDK       bool   `json:"sdk"`
	Skills    struct {
		Installed bool   `json:"installed"`
		Running   bool   `json:"running"`
		Port      int    `json:"port"`
		Version   string `json:"version"`
		Mode      string `json:"mode"`
		Error     string `json:"error"`
	} `json:"skills"`
}

// ReadBridgeAlive: the heartbeat, when it is fresh (the editor is open and the package is running).
func ReadBridgeAlive(p string) (*BridgeAlive, bool) {
	f := filepath.Join(BridgeDir(p), "alive.json")
	for try := 0; ; try++ {
		fi, err := os.Stat(f)
		if err == nil && time.Since(fi.ModTime()) > 12*time.Second {
			return nil, false // the editor is closed, or stuck
		}
		if err == nil {
			var a BridgeAlive
			if b, err := os.ReadFile(f); err == nil && json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &a) == nil {
				return &a, true
			}
		}
		// not there or half written: it is rewritten every two seconds, so look again in a moment
		if try >= 3 || !core.StatOK(BridgeDir(p)) {
			return nil, false
		}
		time.Sleep(40 * time.Millisecond)
	}
}

// BridgeError is what the Unity side answered: something the player (or the AI) can act on.
type BridgeError struct{ msg string }

func (e *BridgeError) Error() string { return e.msg }

var bridgeSeq atomic.Int64

// waitAlive: the heartbeat, with patience. Unity writes none while it carries out a long step or reloads its
// scripts after an import, so an open editor is given a minute to come back.
func waitAlive(ctx context.Context, p string) error {
	if _, ok := ReadBridgeAlive(p); ok {
		return nil
	}
	if !core.StatOK(filepath.Join(p, "Packages", PipePkg, "package.json")) {
		return errors.New("这个工程还没有装 AI 插件：先点「安装并打开」")
	}
	for until := time.Now().Add(60 * time.Second); ProjectRunning(p) && time.Now().Before(until); {
		select {
		case <-ctx.Done():
			return errors.New("已停止")
		case <-time.After(400 * time.Millisecond):
		}
		if _, ok := ReadBridgeAlive(p); ok {
			return nil
		}
	}
	if !ProjectRunning(p) {
		return errors.New("Unity 没有开着这个工程：先打开它")
	}
	return errors.New("Unity 开着，但插件还没有运行：切到 Unity 窗口等它编译完（右下角转圈结束），有红色报错的话先解决")
}

// BridgeCall asks the pipeline package to do one thing and waits for its answer. wait is how long the step
// itself may take once Unity has begun it.
func BridgeCall(ctx context.Context, p, cmd string, args any, wait time.Duration) (json.RawMessage, error) {
	if err := waitAlive(ctx, p); err != nil {
		return nil, err
	}
	if wait <= 0 {
		wait = 90 * time.Second
	}
	dir := BridgeDir(p)
	deadline := time.Now().Add(wait)
	for {
		id := fmt.Sprintf("%013d%03d", time.Now().UnixMilli(), bridgeSeq.Add(1)%1000)
		b, _ := json.Marshal(map[string]any{"id": id, "cmd": cmd, "args": args})
		req, tmp := filepath.Join(dir, "req_"+id+".json"), filepath.Join(dir, "w_"+id+".tmp")
		if err := os.WriteFile(tmp, b, 0644); err != nil {
			return nil, fmt.Errorf("写不了工程的 UserSettings 文件夹：%v", err)
		}
		if err := os.Rename(tmp, req); err != nil {
			_ = os.Remove(tmp)
			return nil, err
		}
		raw, err := waitAnswer(ctx, dir, id, wait)
		_ = os.Remove(filepath.Join(dir, "res_"+id+".json"))
		if err != nil {
			_ = os.Remove(req) // not carried out after we stopped waiting
			return nil, err
		}
		var r struct {
			OK     bool            `json:"ok"`
			Error  string          `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &r) != nil {
			return nil, errors.New("Unity 的回答读不懂")
		}
		if r.OK {
			return r.Result, nil
		}
		// busy compiling: the same request again in a moment
		if strings.Contains(r.Error, "正在编译") && time.Until(deadline) > 4*time.Second {
			select {
			case <-ctx.Done():
				return nil, errors.New("已停止")
			case <-time.After(2 * time.Second):
			}
			continue
		}
		return nil, &BridgeError{r.Error}
	}
}

// How long a request may lie there untouched. Unity drops what is older than two minutes unread, so that
// nothing asked for long ago happens when the editor wakes up.
const bridgePickup = 110 * time.Second

// waitAnswer follows one request: req_<id> lies there until Unity takes it, run_<id> is there while Unity
// works on it, res_<id> is the answer.
func waitAnswer(ctx context.Context, dir, id string, wait time.Duration) ([]byte, error) {
	req, run, res := filepath.Join(dir, "req_"+id+".json"), filepath.Join(dir, "run_"+id+".json"), filepath.Join(dir, "res_"+id+".json")
	stuck := errors.New("Unity 没有回应：它可能正在编译、导入，或者弹了对话框在等你点。切到 Unity 看一下再试")
	sent := time.Now()
	var gone, taken time.Time
	for {
		if b, err := os.ReadFile(res); err == nil && len(b) > 0 {
			return b, nil
		}
		now := time.Now()
		switch {
		case core.StatOK(run): // being worked on: as long as the step is allowed to take
			gone = time.Time{}
			if taken.IsZero() {
				taken = now
			}
			if now.Sub(taken) > wait {
				return nil, errors.New("Unity 这一步做了很久还没有做完：切到 Unity 看看它在忙什么")
			}
		case core.StatOK(req):
			if now.Sub(sent) > bridgePickup {
				return nil, stuck
			}
		case gone.IsZero(): // neither: answered this very moment, or dropped
			gone = now
		case now.Sub(gone) > 3*time.Second:
			return nil, stuck
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("已停止")
		case <-time.After(120 * time.Millisecond):
		}
	}
}

// ---------- the packages ----------

type AIKit struct {
	Pipeline    bool   `json:"pipeline"`              // our package is in the project
	PipelineOld bool   `json:"pipelineOld,omitempty"` // an older copy than the one this program carries
	Skills      string `json:"skills"`                // "": not there; "ours": put there by this program; "own": the project's own
	SkillsVer   string `json:"skillsVer,omitempty"`
	Unity       string `json:"unity,omitempty"`
	Editor      bool   `json:"editor"`  // that Unity version is installed
	Running     bool   `json:"running"` // Unity has the project open
	Alive       bool   `json:"alive"`   // …and our package answers
	Compiling   bool   `json:"compiling,omitempty"`
	Playing     bool   `json:"playing,omitempty"`
	MA          string `json:"ma,omitempty"`
	SDK         bool   `json:"sdk,omitempty"`
	SkillsOn    bool   `json:"skillsOn,omitempty"` // the UnitySkills server is running
	Port        int    `json:"port,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Hint        string `json:"hint,omitempty"` // what the player has to do next, if anything
}

func pkgVersion(dir string) (name, version, author string) {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return
	}
	var m struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Author  any    `json:"author"`
	}
	if json.Unmarshal(b, &m) != nil {
		return
	}
	switch a := m.Author.(type) {
	case string:
		author = a
	case map[string]any:
		author, _ = a["name"].(string)
	}
	return m.Name, m.Version, author
}

func EmbeddedPipelineVersion() string {
	b, err := vrclib.UnityHelper.ReadFile("unityhelper/" + PipePkg + "/package.json")
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(b, &m)
	return m.Version
}

// skillsInProject: whether UnitySkills is in the project, and whose copy it is.
func skillsInProject(p string) (kind, version string) {
	dir := filepath.Join(p, "Packages", SkillsPkg)
	if name, ver, _ := pkgVersion(dir); name == SkillsPkg {
		if core.StatOK(filepath.Join(dir, KitMarker)) {
			return "ours", ver
		}
		return "own", ver
	}
	if b, err := os.ReadFile(filepath.Join(p, "Packages", "manifest.json")); err == nil {
		var m struct {
			Dependencies map[string]string `json:"dependencies"`
		}
		if json.Unmarshal(b, &m) == nil {
			if v, ok := m.Dependencies[SkillsPkg]; ok {
				return "own", v
			}
		}
	}
	return "", ""
}

func AIKitStatus(p string) AIKit {
	k := AIKit{Unity: projectUnityVersion(p), Running: ProjectRunning(p)}
	k.Editor = k.Unity != "" && unityEditors()[k.Unity] != ""
	dir := filepath.Join(p, "Packages", PipePkg)
	if name, ver, author := pkgVersion(dir); name == PipePkg && author == "MioVRC" {
		k.Pipeline = true
		k.PipelineOld = ver != EmbeddedPipelineVersion()
	}
	k.Skills, k.SkillsVer = skillsInProject(p)
	if a, ok := ReadBridgeAlive(p); ok {
		k.Alive, k.Running = true, true
		k.Compiling, k.Playing, k.MA, k.SDK = a.Compiling, a.Playing, a.MA, a.SDK
		k.SkillsOn, k.Port, k.Mode = a.Skills.Running, a.Skills.Port, a.Skills.Mode
		if a.Bridge != "" && a.Bridge != EmbeddedPipelineVersion() {
			k.PipelineOld = true
		}
	}
	if !k.SkillsOn {
		if port := registryPort(p); port > 0 {
			k.SkillsOn, k.Port = true, port
		}
	}
	switch {
	case !k.Pipeline:
		k.Hint = "还没有给这个工程装 AI 插件"
	case !k.Running && k.Unity == "":
		k.Hint = "Unity 还没有开着这个工程：从 Unity Hub 或 VCC 打开它"
	case !k.Running && !k.Editor:
		k.Hint = fmt.Sprintf("这台电脑上没找到 Unity %s：从 Unity Hub 或 VCC 打开这个工程", k.Unity)
	case !k.Running:
		k.Hint = "Unity 还没有开着这个工程"
	case !k.Alive:
		k.Hint = "Unity 正在启动或编译：切到 Unity 窗口，等右下角转圈结束。一直连不上的话，看 Console 里有没有红色报错"
	case !k.SDK:
		k.Hint = "这个工程没有 VRChat SDK（Avatars）：先用 VCC / ALCOM 装上"
	case k.MA == "":
		k.Hint = "这个工程没有 Modular Avatar：先用 VCC / ALCOM 装上，穿戴和菜单都靠它"
	case k.Compiling:
		k.Hint = "Unity 正在编译或导入，等它忙完"
	case k.Playing:
		k.Hint = "Unity 在 Play 模式里：先退出 Play，再让 AI 改场景"
	}
	return k
}

// registryPort: UnitySkills lists its running editors in ~/.unity_skills/registry.json, by project path.
func registryPort(p string) int {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	b, err := os.ReadFile(filepath.Join(home, ".unity_skills", "registry.json"))
	if err != nil {
		return 0
	}
	var reg map[string]struct {
		Path       string `json:"path"`
		Port       int    `json:"port"`
		LastActive int64  `json:"last_active"`
	}
	if json.Unmarshal(b, &reg) != nil {
		return 0
	}
	for k, v := range reg {
		path := v.Path
		if path == "" {
			path = k
		}
		if core.PathKey(path) == core.PathKey(p) && v.Port > 0 && time.Now().Unix()-v.LastActive < 120 && skillsHealthy(v.Port) {
			return v.Port
		}
	}
	return 0
}

func skillsHealthy(port int) bool {
	r, err := webpane.LocalHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	defer r.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<16))
	return r.StatusCode == 200
}

// putPackage writes a package into the project's Packages folder in one step: next to it under a name Unity
// skips ("~"), then moved into place.
func putPackage(p, pkg string, write func(tmp string) error) error {
	pkgs := filepath.Join(p, "Packages")
	if err := os.MkdirAll(pkgs, 0755); err != nil {
		return err
	}
	dir := filepath.Join(pkgs, pkg)
	tmp, old := dir+"~", dir+".old~"
	_ = os.RemoveAll(tmp)
	_ = os.RemoveAll(old)
	if err := write(tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	// the copy that is there steps aside whole (or not at all, when Unity holds a file in it)
	had := core.StatOK(dir)
	if had {
		if err := os.Rename(dir, old); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if had {
			_ = os.Rename(old, dir)
		}
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

func writeEmbeddedPackage(pkg string) func(tmp string) error {
	return func(tmp string) error {
		root := "unityhelper/" + pkg
		return fs.WalkDir(vrclib.UnityHelper, root, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			dst := filepath.Join(tmp, filepath.FromSlash(strings.TrimPrefix(fp, root)))
			if d.IsDir() {
				return os.MkdirAll(dst, 0755)
			}
			b, err := vrclib.UnityHelper.ReadFile(fp)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, b, 0644)
		})
	}
}

func writeSkillsPackage(tmp string) error {
	b, err := vrclib.UnityKit.ReadFile(SkillsZip)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		name := filepath.FromSlash(f.Name)
		if strings.Contains(f.Name, "..") || filepath.IsAbs(name) {
			continue
		}
		dst := filepath.Join(tmp, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(dst)
		if err == nil {
			_, err = io.Copy(w, rc)
			if cerr := w.Close(); err == nil {
				err = cerr
			}
		}
		rc.Close()
		if err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(tmp, KitMarker), []byte("MioVRCA 放进来的 UnitySkills "+SkillsVersion+"（MIT，github.com/Besty0728/Unity-Skills）。在软件里点「移除 AI 插件」会删掉这个文件夹。\n"), 0644)
}

// InstallAIKit puts both packages into the project (a UnitySkills the project already has is left alone),
// asks for the UnitySkills server to be started, and opens the project in Unity when it is not open.
func InstallAIKit(p string) (note string, err error) {
	dir := filepath.Join(p, "Packages", PipePkg)
	if name, _, author := pkgVersion(dir); core.StatOK(dir) && !(name == PipePkg && author == "MioVRC") {
		return "", errors.New("工程的 Packages 里已经有一个 " + PipePkg + "，不是本软件放的，没有动它")
	}
	if err := putPackage(p, PipePkg, writeEmbeddedPackage(PipePkg)); err != nil {
		return "", fmt.Errorf("没能放进流水线插件：%v（Unity 正占用文件的话，关掉 Unity 再试）", err)
	}
	kind, ver := skillsInProject(p)
	switch {
	case kind == "own":
		note = "工程里已经有 UnitySkills " + ver + "，用它自己的"
	case kind == "ours" && ver == SkillsVersion:
	default:
		if err := putPackage(p, SkillsPkg, writeSkillsPackage); err != nil {
			return "", fmt.Errorf("没能放进 UnitySkills：%v（Unity 正占用文件的话，关掉 Unity 再试）", err)
		}
	}
	if err := os.MkdirAll(BridgeDir(p), 0755); err == nil {
		_ = os.WriteFile(filepath.Join(BridgeDir(p), "want_skills"), []byte("1"), 0644)
	}
	core.Logf("AI 插件已放进 %s", p)
	if ProjectRunning(p) {
		if _, ok := ReadBridgeAlive(p); ok { // an older copy is running: have Unity pick the new files up
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				_, _ = BridgeCall(ctx, p, "refresh", map[string]any{}, 15*time.Second)
			}()
			return joinNote(note, "Unity 开着：它会自己重新编译，稍等就连上"), nil
		}
		return joinNote(note, "Unity 开着：切到 Unity 窗口，它会导入插件并编译，完成后这里自动连上"), nil
	}
	if err := OpenInUnity(p); err != nil {
		return joinNote(note, "插件装好了，但没能打开 Unity："+err.Error()), nil
	}
	return joinNote(note, "正在打开 Unity。第一次要导入插件并编译，一两分钟后这里自动连上"), nil
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "。" + b
}

// RemoveAIKit takes out what installAIKit put in. The project's own UnitySkills stays.
func RemoveAIKit(p string) error {
	dir := filepath.Join(p, "Packages", PipePkg)
	if core.StatOK(dir) {
		if name, _, author := pkgVersion(dir); !(name == PipePkg && author == "MioVRC") {
			return errors.New("工程里的 " + PipePkg + " 不是本软件放的，没有动它")
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("没能删掉流水线插件：%v（关掉 Unity 再试）", err)
		}
	}
	sk := filepath.Join(p, "Packages", SkillsPkg)
	if core.StatOK(filepath.Join(sk, KitMarker)) {
		if err := os.RemoveAll(sk); err != nil {
			return fmt.Errorf("没能删掉 UnitySkills：%v（关掉 Unity 再试）", err)
		}
	}
	_ = os.RemoveAll(BridgeDir(p))
	_ = os.RemoveAll(filepath.Join(p, "UserSettings", "MioVRCA", "shots")) // the plugin's own pictures, normally empty
	_ = os.Remove(filepath.Join(p, "UserSettings", "MioVRCA"))             // only when nothing else (cover pictures) is in it
	core.Logf("AI 插件已从 %s 移除", p)
	return nil
}

// ---------- UnitySkills over REST ----------

// SkillsPort: where the project's UnitySkills server listens, 0 when it is not running.
func SkillsPort(p string) int {
	if a, ok := ReadBridgeAlive(p); ok && a.Skills.Running && a.Skills.Port > 0 {
		return a.Skills.Port
	}
	return registryPort(p)
}

var skillsHTTP = &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{Proxy: nil}}

func SkillsRequest(ctx context.Context, p, method, path string, body any) ([]byte, int, error) {
	port := SkillsPort(p)
	if port == 0 {
		if kind, _ := skillsInProject(p); kind == "" {
			return nil, 0, errors.New("这个工程没有装 UnitySkills：先点「安装并打开」")
		}
		return nil, 0, errors.New("UnitySkills 的服务没有运行：在 Unity 里打开 Window > UnitySkills，点 Start Server")
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), rd)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := skillsHTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, errors.New("已停止")
		}
		return nil, 0, errors.New("连不上 Unity 里的 UnitySkills（它可能正在重新编译）：等几秒再试")
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 24<<20)) // a screenshot can come back inside the answer
	return b, resp.StatusCode, nil
}

// SkillsFind: the skills that fit an intent, with their parameters, as short as it can be said.
func SkillsFind(ctx context.Context, p, intent string, top int) (string, error) {
	if top <= 0 || top > 12 {
		top = 6
	}
	b, _, err := SkillsRequest(ctx, p, "GET", "/skills/recommend?includeSchema=true&topN="+fmt.Sprint(top)+"&intent="+url.QueryEscape(intent), nil)
	if err != nil {
		return "", err
	}
	var r struct {
		Results []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
			Schema      struct {
				Parameters []struct {
					Name     string `json:"name"`
					Type     string `json:"type"`
					Required bool   `json:"required"`
					Default  any    `json:"defaultValue"`
				} `json:"parameters"`
				RiskLevel string `json:"riskLevel"`
				ReadOnly  bool   `json:"readOnly"`
			} `json:"schema"`
		} `json:"results"`
	}
	if json.Unmarshal(b, &r) != nil {
		return string(Clip(b, 6000)), nil
	}
	type par struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Required bool   `json:"required,omitempty"`
		Default  any    `json:"default,omitempty"`
	}
	type sk struct {
		Name     string `json:"name"`
		Desc     string `json:"description"`
		Params   []par  `json:"parameters"`
		Risk     string `json:"risk,omitempty"`
		ReadOnly bool   `json:"readOnly,omitempty"`
	}
	out := []sk{}
	for _, x := range r.Results {
		s := sk{Name: x.Name, Desc: x.Description, Risk: x.Schema.RiskLevel, ReadOnly: x.Schema.ReadOnly, Params: []par{}}
		for _, q := range x.Schema.Parameters {
			s.Params = append(s.Params, par{Name: q.Name, Type: q.Type, Required: q.Required, Default: q.Default})
		}
		out = append(out, s)
	}
	j, _ := json.Marshal(out)
	return string(j), nil
}

// what a UnitySkills answer may take of the conversation, compacted first (pictures in it do not count:
// they are taken out and go along as pictures)
const SkillAnswerMax = 30000

// SkillsCall runs one UnitySkills skill and returns what it answered.
func SkillsCall(ctx context.Context, p, name string, args map[string]any) (string, bool, error) {
	b, ok, err := SkillsCallRaw(ctx, p, name, args)
	if err != nil {
		return "", false, err
	}
	return string(Clip(core.CompactJSON(b), SkillAnswerMax)), ok, nil
}

// SkillsCallRaw: the answer whole, as UnitySkills sent it.
func SkillsCallRaw(ctx context.Context, p, name string, args map[string]any) ([]byte, bool, error) {
	if !validSkillName(name) {
		return nil, false, errors.New("skill 名字不对：" + name)
	}
	if args == nil {
		args = map[string]any{}
	}
	b, status, err := SkillsRequest(ctx, p, "POST", "/skill/"+name, args)
	if err != nil {
		return nil, false, err
	}
	ok := status/100 == 2
	var r struct {
		Status string `json:"status"`
		Error  any    `json:"error"`
	}
	if json.Unmarshal(b, &r) == nil && (r.Status == "error" || (r.Error != nil && r.Status != "success")) {
		ok = false
	}
	return b, ok, nil
}

func validSkillName(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

func Clip(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	cut := n
	for cut > 0 && b[cut]&0xC0 == 0x80 { // not in the middle of a character
		cut--
	}
	return append(append([]byte{}, b[:cut]...), []byte(fmt.Sprintf("…（太长，后面 %d 字节没有给出；把查询范围收小一点）", len(b)-cut))...)
}
