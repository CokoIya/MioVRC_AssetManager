package unity_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib"
	"vrclib/internal/core"
	"vrclib/internal/unity"
	"vrclib/internal/unity/unitytest"
)

func TestBridgeCall(t *testing.T) {
	proj := t.TempDir()
	if _, err := unity.BridgeCall(context.Background(), proj, "ping", nil, time.Second); err == nil || !strings.Contains(err.Error(), "Unity 插件") {
		t.Errorf("no package: %v", err)
	}
	n := 0
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch cmd {
		case "busy":
			if n++; n < 2 {
				return nil, "Unity 正在编译或导入，请稍后重试"
			}
			return map[string]any{"n": n}, ""
		case "bad":
			return nil, "模型下未找到「X」"
		case "slow":
			time.Sleep(700 * time.Millisecond)
		case "drop":
			return nil, "__drop__"
		}
		return map[string]any{"cmd": cmd}, ""
	})
	raw, err := unity.BridgeCall(context.Background(), proj, "ping", map[string]any{}, 5*time.Second)
	if err != nil || !strings.Contains(string(raw), "ping") {
		t.Fatalf("ping: %s %v", raw, err)
	}
	if raw, err = unity.BridgeCall(context.Background(), proj, "busy", map[string]any{}, 20*time.Second); err != nil || !strings.Contains(string(raw), "2") {
		t.Errorf("busy then done: %s %v", raw, err)
	}
	var be *unity.BridgeError
	if _, err = unity.BridgeCall(context.Background(), proj, "bad", map[string]any{}, 5*time.Second); err == nil || !asBridgeErr(err, &be) {
		t.Errorf("refusal: %v", err)
	}
	if _, err = unity.BridgeCall(context.Background(), proj, "slow", map[string]any{}, 300*time.Millisecond); err == nil || !strings.Contains(err.Error(), "超时") {
		t.Errorf("timeout: %v", err)
	}
	t0 := time.Now()
	if _, err = unity.BridgeCall(context.Background(), proj, "drop", map[string]any{}, time.Minute); err == nil || !strings.Contains(err.Error(), "未响应") || time.Since(t0) > 8*time.Second {
		t.Errorf("dropped request: %v after %v", err, time.Since(t0))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	if _, err = unity.BridgeCall(ctx, proj, "slow", map[string]any{}, time.Minute); err == nil || !strings.Contains(err.Error(), "已停止") {
		t.Errorf("cancelled: %v", err)
	}
	time.Sleep(900 * time.Millisecond)
	// no request of ours is left lying in the folder (answers to the two requests we gave up on may still
	// arrive: the Unity side clears those after ten minutes)
	left, _ := filepath.Glob(filepath.Join(unity.BridgeDir(proj), "req_*"))
	tmp, _ := filepath.Glob(filepath.Join(unity.BridgeDir(proj), "*.tmp"))
	res, _ := filepath.Glob(filepath.Join(unity.BridgeDir(proj), "res_*"))
	if len(left)+len(tmp) > 0 || len(res) > 2 {
		t.Errorf("left behind: %v %v %v", left, tmp, res)
	}
}

func asBridgeErr(err error, target **unity.BridgeError) bool {
	be, ok := err.(*unity.BridgeError)
	if ok {
		*target = be
	}
	return ok
}

func TestAIKitInstall(t *testing.T) {
	proj := t.TempDir()
	for _, d := range []string{"Assets", "ProjectSettings", "Packages"} {
		_ = os.MkdirAll(filepath.Join(proj, d), 0755)
	}
	_ = os.WriteFile(filepath.Join(proj, "ProjectSettings", "ProjectVersion.txt"), []byte("m_EditorVersion: 2022.3.22f1\n"), 0644)
	t.Setenv("VRCLIB_UNITY_DIRS", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	unity.EdMu.Lock()
	unity.EdCache = nil
	unity.EdMu.Unlock()
	if k := unity.AIKitStatus(proj); k.Pipeline || k.Skills != "" || k.Hint == "" {
		t.Errorf("before: %+v", k)
	}
	note, err := unity.InstallAIKit(proj)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "无法打开 Unity") { // no editor on this machine
		t.Errorf("note %q", note)
	}
	k := unity.AIKitStatus(proj)
	if !k.Pipeline || k.PipelineOld || k.Skills != "ours" || k.SkillsVer != unity.SkillsVersion || k.Alive {
		t.Errorf("after install: %+v", k)
	}
	for _, f := range []string{
		"Packages/" + unity.PipePkg + "/Editor/Bridge.cs", "Packages/" + unity.PipePkg + "/Editor/MenuBuilder.cs.meta",
		"Packages/" + unity.SkillsPkg + "/package.json", "Packages/" + unity.SkillsPkg + "/Editor/Skills/SkillsHttpServer.cs", "Packages/" + unity.SkillsPkg + "/LICENSE.md",
		"Packages/" + unity.SkillsPkg + "/" + unity.KitMarker, "UserSettings/MioVRCA/bridge/want_skills",
	} {
		if !core.StatOK(filepath.Join(proj, filepath.FromSlash(f))) {
			t.Errorf("missing %s", f)
		}
	}
	if core.StatOK(filepath.Join(proj, "Packages", unity.SkillsPkg, "Tests")) {
		t.Error("UnitySkills' tests were bundled")
	}
	// every file of our package has its .meta (Unity would write new GUIDs otherwise)
	_ = filepath.Walk(filepath.Join(proj, "Packages", unity.PipePkg), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !strings.HasSuffix(p, ".meta") && p != filepath.Join(proj, "Packages", unity.PipePkg) && !core.StatOK(p+".meta") {
			t.Errorf("no .meta for %s", p)
		}
		return nil
	})
	// installing again changes nothing; the cover pictures of the other package survive a removal
	_ = os.WriteFile(filepath.Join(proj, "UserSettings", "MioVRCA", "cover_1.png"), []byte("x"), 0644)
	if _, err := unity.InstallAIKit(proj); err != nil {
		t.Fatal(err)
	}
	if err := unity.RemoveAIKit(proj); err != nil {
		t.Fatal(err)
	}
	if core.StatOK(filepath.Join(proj, "Packages", unity.PipePkg)) || core.StatOK(filepath.Join(proj, "Packages", unity.SkillsPkg)) || core.StatOK(unity.BridgeDir(proj)) ||
		!core.StatOK(filepath.Join(proj, "UserSettings", "MioVRCA", "cover_1.png")) {
		t.Error("removal")
	}
	// a UnitySkills the project brought itself is neither replaced nor removed
	own := filepath.Join(proj, "Packages", unity.SkillsPkg)
	_ = os.MkdirAll(own, 0755)
	_ = os.WriteFile(filepath.Join(own, "package.json"), []byte(`{"name":"com.besty.unity-skills","version":"2.7.0","author":{"name":"Besty"}}`), 0644)
	if note, err = unity.InstallAIKit(proj); err != nil || !strings.Contains(note, "2.7.0") {
		t.Errorf("own copy: %q %v", note, err)
	}
	if k = unity.AIKitStatus(proj); k.Skills != "own" || k.SkillsVer != "2.7.0" {
		t.Errorf("own copy status %+v", k)
	}
	_ = unity.RemoveAIKit(proj)
	if !core.StatOK(filepath.Join(own, "package.json")) {
		t.Error("the project's own UnitySkills was removed")
	}
	_ = os.RemoveAll(own)
	_ = os.WriteFile(filepath.Join(proj, "Packages", "manifest.json"), []byte(`{"dependencies":{"com.besty.unity-skills":"https://github.com/Besty0728/Unity-Skills.git?path=/SkillsForUnity"}}`), 0644)
	if _, err = unity.InstallAIKit(proj); err != nil || core.StatOK(own) {
		t.Errorf("manifest copy: %v", err)
	}
	// somebody else's package under our name is left alone
	_ = unity.RemoveAIKit(proj)
	foreign := filepath.Join(proj, "Packages", unity.PipePkg)
	_ = os.MkdirAll(foreign, 0755)
	_ = os.WriteFile(filepath.Join(foreign, "package.json"), []byte(`{"name":"com.miovrc.pipeline","author":{"name":"else"}}`), 0644)
	if _, err = unity.InstallAIKit(proj); err == nil {
		t.Error("foreign package replaced")
	}
	if err = unity.RemoveAIKit(proj); err == nil || !core.StatOK(foreign) {
		t.Error("foreign package removed")
	}
}

func TestSkillsREST(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.String())
		switch {
		case r.URL.Path == "/health":
			_, _ = io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/skills/recommend":
			_, _ = io.WriteString(w, `{"results":[{"name":"scene_load","description":"Load an existing scene","category":"Scene","telemetry":{"calls":9},
			 "schema":{"parameters":[{"name":"scenePath","type":"string","required":true,"defaultValue":null}],"riskLevel":"high","readOnly":false}}]}`)
		case r.URL.Path == "/skill/gameobject_find":
			b, _ := io.ReadAll(r.Body)
			_, _ = io.WriteString(w, `{"status":"success","result":{"echo":`+string(b)+`}}`)
		case r.URL.Path == "/skill/asset_delete":
			_, _ = io.WriteString(w, `{"status":"error","error":"MODE_FORBIDDEN","message":"not allowed in Auto mode"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(interface{ String() string }).String()
	port = port[strings.LastIndex(port, ":")+1:]
	if unity.SkillsPort(proj) != 0 {
		t.Error("a port without a registry")
	}
	if _, _, err := unity.SkillsCall(context.Background(), proj, "gameobject_find", nil); err == nil || !strings.Contains(err.Error(), "UnitySkills") {
		t.Errorf("not installed: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(home, ".unity_skills"), 0755)
	reg := fmt.Sprintf(`{%q:{"id":"x","path":%q,"port":%s,"pid":1,"last_active":%d},"C:\\Other":{"path":"C:\\Other","port":1,"last_active":%d}}`, proj, proj, port, time.Now().Unix(), time.Now().Unix())
	_ = os.WriteFile(filepath.Join(home, ".unity_skills", "registry.json"), []byte(reg), 0644)
	if p := unity.SkillsPort(proj); fmt.Sprint(p) != port {
		t.Fatalf("port %d, want %s", p, port)
	}
	out, err := unity.SkillsFind(context.Background(), proj, "load a scene", 3)
	if err != nil || !strings.Contains(out, `"scene_load"`) || !strings.Contains(out, `"scenePath"`) || strings.Contains(out, "telemetry") {
		t.Errorf("find: %s %v", out, err)
	}
	out, ok, err := unity.SkillsCall(context.Background(), proj, "gameobject_find", map[string]any{"name": "Body"})
	if err != nil || !ok || !strings.Contains(out, `"name":"Body"`) {
		t.Errorf("call: %s %v %v", out, ok, err)
	}
	if out, ok, err = unity.SkillsCall(context.Background(), proj, "asset_delete", map[string]any{}); err != nil || ok || !strings.Contains(out, "MODE_FORBIDDEN") {
		t.Errorf("forbidden: %s %v %v", out, ok, err)
	}
	if _, _, err = unity.SkillsCall(context.Background(), proj, "../health", nil); err == nil {
		t.Error("a path as a skill name")
	}
	if !strings.Contains(strings.Join(got, "\n"), "intent=load+a+scene") {
		t.Errorf("requests %v", got)
	}
}

func TestClip(t *testing.T) {
	s := strings.Repeat("好", 100)
	c := string(unity.Clip([]byte(s), 100))
	if !strings.HasPrefix(c, strings.Repeat("好", 33)+"…") || strings.ContainsRune(c, '\uFFFD') {
		t.Errorf("clip %q", c)
	}
	if string(unity.Clip([]byte("abc"), 10)) != "abc" {
		t.Error("short text changed")
	}
}

// The heartbeat's version is how the program notices an old copy of the package in a project.
func TestPipelineVersion(t *testing.T) {
	b, err := vrclib.UnityHelper.ReadFile("unityhelper/" + unity.PipePkg + "/Editor/Bridge.cs")
	if err != nil {
		t.Fatal(err)
	}
	v := unity.EmbeddedPipelineVersion()
	if v == "" || !strings.Contains(string(b), `public const string Version = "`+v+`";`) {
		t.Errorf("package.json says %q, Bridge.cs does not", v)
	}
	// every command the program sends is one the package knows
	for _, cmd := range []string{"ping", "inspect", "inspect_object", "prefabs", "refresh", "dress", "build_menu", "icons", "place_avatar", "snapshot", "undo", "select"} {
		if !strings.Contains(string(b), `"`+cmd+`"`) {
			t.Errorf("Bridge.cs has no %q", cmd)
		}
	}
	if _, err := vrclib.UnityKit.ReadFile(unity.SkillsZip); err != nil {
		t.Error(err)
	}
	if lic, err := vrclib.UnityKit.ReadFile("unitykit/UnitySkills-LICENSE.txt"); err != nil || !strings.Contains(string(lic), "MIT License") {
		t.Error("UnitySkills licence text is not bundled")
	}
}
