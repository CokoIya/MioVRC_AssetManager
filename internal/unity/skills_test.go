package unity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
)

// UnitySkills in a new project: by its README's link when this computer's git reaches GitHub (Unity fetches
// it), else the copy the program carries — with the reason in the note.
func TestInstallSkills(t *testing.T) {
	old := gitReaches
	t.Cleanup(func() { gitReaches = old })
	newProj := func() string {
		p := t.TempDir()
		_ = os.MkdirAll(filepath.Join(p, "Packages"), 0755)
		if err := writeUnityManifest(p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	gitReaches = func(ctx context.Context, repo string) (bool, string) {
		if repo != "https://github.com/Besty0728/Unity-Skills.git" {
			t.Errorf("asked of %s", repo)
		}
		return true, ""
	}
	p := newProj()
	n := installSkills(context.Background(), p)
	var m struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	b, _ := os.ReadFile(filepath.Join(p, "Packages", "manifest.json"))
	if err := json.Unmarshal(b, &m); err != nil || m.Dependencies[SkillsPkg] != "https://github.com/Besty0728/Unity-Skills.git?path=/SkillsForUnity" || m.Dependencies["com.unity.modules.animation"] != "1.0.0" {
		t.Errorf("manifest: %s", b)
	}
	if !strings.Contains(n, "通过 Git 安装") || core.StatOK(filepath.Join(p, "Packages", SkillsPkg)) {
		t.Errorf("by git: %q", n)
	}
	if kind, ver := skillsInProject(p); kind != "own" || ver != "Git" {
		t.Errorf("before Unity fetched it: %s %s", kind, ver)
	}
	cache := filepath.Join(p, "Library", "PackageCache", SkillsPkg+"@1a2b3c")
	_ = os.MkdirAll(cache, 0755)
	_ = os.WriteFile(filepath.Join(cache, "package.json"), []byte(`{"name":"com.besty.unity-skills","version":"2.9.0"}`), 0644)
	if kind, ver := skillsInProject(p); kind != "own" || ver != "2.9.0" {
		t.Errorf("fetched: %s %s", kind, ver)
	}

	for _, why := range []string{"本机未安装 Git", "Git 无法连接 GitHub"} {
		gitReaches = func(context.Context, string) (bool, string) { return false, why }
		p = newProj()
		n = installSkills(context.Background(), p)
		if n != "UnitySkills："+why+"，已安装软件自带的 "+SkillsVersion {
			t.Errorf("without git: %q", n)
		}
		if kind, ver := skillsInProject(p); kind != "ours" || ver != SkillsVersion {
			t.Errorf("the program's copy: %s %s", kind, ver)
		}
		b, _ = os.ReadFile(filepath.Join(p, "Packages", "manifest.json"))
		if strings.Contains(string(b), SkillsPkg) {
			t.Errorf("a link Unity cannot fetch went into the manifest: %s", b)
		}
	}
}

// The new-project window offers it, ticked; the VPM repositories are never asked for it.
func TestSkillsOffered(t *testing.T) {
	found := false
	for _, p := range gitPlugins {
		if p.Pkg == SkillsPkg && p.Default && p.Git {
			found = true
		}
	}
	for _, p := range basePlugins {
		if p.Pkg == SkillsPkg {
			t.Error("UnitySkills among the VPM plugins")
		}
	}
	if !found || !isGitPlugin(SkillsPkg) || isGitPlugin("nadena.dev.modular-avatar") {
		t.Error("UnitySkills is not offered as a ticked git plugin")
	}
}

// want_skills holds until when the program wants the UnitySkills server: a use moves the time on (never back, and
// not with every request), the end of a run brings it down to the grace; written in one step, and only where the
// plugin has its folder.
func TestWantSkills(t *testing.T) {
	p := t.TempDir()
	f := filepath.Join(BridgeDir(p), "want_skills")
	WantSkills(p, SkillsLease)
	if core.StatOK(BridgeDir(p)) {
		t.Fatal("written where the plugin has no folder")
	}
	_ = os.MkdirAll(BridgeDir(p), 0755)
	now := time.Now().Unix()
	for _, c := range []struct {
		was  string // "-": no file
		want int64  // seconds from now
	}{
		{"-", 180}, {"1", 180}, {"x", 180}, // none, or what an older program wrote
		{fmt.Sprint(now + 100), 100},   // more than half of the lease is left: not written
		{fmt.Sprint(now + 80), 180},    // less: moved on
		{fmt.Sprint(now + 1000), 1000}, // a later time stays
	} {
		_ = os.Remove(f)
		if c.was != "-" {
			_ = os.WriteFile(f, []byte(c.was), 0644)
		}
		now = time.Now().Unix()
		WantSkills(p, SkillsLease)
		if got := wantedUntil(p) - now; got < c.want-1 || got > c.want {
			t.Errorf("WantSkills after %q: %d s from now, want %d", c.was, got, c.want)
		}
	}
	for _, c := range []struct {
		was  string
		want int64
	}{{fmt.Sprint(now + 1000), 120}, {fmt.Sprint(now + 60), 60}, {"1", 1 - now}, {"-", -now}} {
		_ = os.Remove(f)
		if c.was != "-" {
			_ = os.WriteFile(f, []byte(c.was), 0644)
		}
		now = time.Now().Unix()
		SkillsDone(p, 2*time.Minute)
		if got := wantedUntil(p) - now; got < c.want-1 || got > c.want {
			t.Errorf("SkillsDone after %q: %d s from now, want %d", c.was, got, c.want)
		}
	}
	if core.StatOK(f) {
		t.Error("SkillsDone wrote a time where there was none")
	}
	if left, _ := filepath.Glob(filepath.Join(BridgeDir(p), "*.tmp")); len(left) > 0 {
		t.Errorf("left behind: %v", left)
	}
}

// A request to UnitySkills asks for the server and gives the plugin a while to start it, while the editor is open,
// has UnitySkills loaded and is not compiling; otherwise, or when it does not come up, it says so as before.
func TestSkillsRequestWaitsForServer(t *testing.T) {
	p := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"status":"ok"}`) }))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	request := func(ctx context.Context) (time.Duration, error) {
		t0 := time.Now()
		b, status, err := SkillsRequest(ctx, p, "GET", "/health", nil)
		if err == nil && (status != 200 || !strings.Contains(string(b), "ok")) {
			t.Errorf("answer %d %s", status, b)
		}
		return time.Since(t0), err
	}
	if _, err := request(context.Background()); err == nil || !strings.Contains(err.Error(), "未安装 UnitySkills") {
		t.Errorf("not installed: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(p, "Packages", SkillsPkg), 0755)
	_ = os.WriteFile(filepath.Join(p, "Packages", SkillsPkg, "package.json"), []byte(`{"name":"com.besty.unity-skills","version":"2.8.4"}`), 0644)
	_ = os.MkdirAll(BridgeDir(p), 0755)
	alive := func(state string) {
		_ = os.WriteFile(filepath.Join(BridgeDir(p), "alive.json"), []byte(`{"bridge":"1","pid":1,`+state+`}`), 0644)
	}
	// the plugin: it starts the server once the program wants it
	alive(`"skills":{"installed":true,"running":false}`)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for until := time.Now().Add(5 * time.Second); wantedUntil(p) < time.Now().Unix()+60 && time.Now().Before(until); {
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(600 * time.Millisecond)
		alive(fmt.Sprintf(`"skills":{"installed":true,"running":true,"port":%d}`, port))
	}()
	took, err := request(context.Background())
	<-done
	if err != nil || took < 500*time.Millisecond {
		t.Errorf("started for the request: %v after %v", err, took)
	}
	// what cannot start it: not answered at once
	stale := func() {
		alive(`"skills":{"installed":true,"running":false}`)
		old := time.Now().Add(-time.Minute)
		_ = os.Chtimes(filepath.Join(BridgeDir(p), "alive.json"), old, old)
	}
	for what, set := range map[string]func(){
		"compiling":               func() { alive(`"compiling":true,"skills":{"installed":true,"running":false}`) },
		"no UnitySkills in Unity": func() { alive(`"skills":{"installed":false}`) },
		"the editor is closed":    stale,
	} {
		set()
		if took, err := request(context.Background()); err == nil || !strings.Contains(err.Error(), "UnitySkills 服务未启动") || took > time.Second {
			t.Errorf("%s: %v after %v", what, err, took)
		}
	}
	// stopped while it waits
	alive(`"skills":{"installed":true,"running":false}`)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	if took, err := request(ctx); err == nil || err.Error() != "已停止" || took > 2*time.Second {
		t.Errorf("stopped: %v after %v", err, took)
	}
	// it does not come up
	old := skillsStartWait
	skillsStartWait = time.Second
	t.Cleanup(func() { skillsStartWait = old })
	if took, err := request(context.Background()); err == nil || !strings.Contains(err.Error(), "UnitySkills 服务未启动") || took < time.Second {
		t.Errorf("not started: %v after %v", err, took)
	}
}

// putPackage replaces the folder of one of the program's own packages only when the program put it there;
// another package's (a VPM package being updated) as before.
func TestPutPackageOnlyOverItsOwn(t *testing.T) {
	p := t.TempDir()
	write := func(tmp string) error {
		_ = os.MkdirAll(tmp, 0755)
		return os.WriteFile(filepath.Join(tmp, "new.txt"), []byte("new"), 0644)
	}
	for _, c := range []struct {
		pkg, packageJSON string
		marker, replaced bool
	}{
		{SkillsPkg, `{"name":"com.besty.unity-skills"}`, false, false},
		{SkillsPkg, `{"name":"com.besty.unity-skills"}`, true, true},
		{PipePkg, `{"name":"com.miovrc.pipeline","author":{"name":"someone"}}`, false, false},
		{PipePkg, `{"name":"com.miovrc.pipeline","author":{"name":"MioVRC"}}`, false, true},
		{"com.miovrc.projectcard", ``, false, false},
		{"nadena.dev.modular-avatar", `{"name":"nadena.dev.modular-avatar"}`, false, true},
	} {
		dir := filepath.Join(p, "Packages", c.pkg)
		_ = os.RemoveAll(dir)
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0644)
		if c.packageJSON != "" {
			_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(c.packageJSON), 0644)
		}
		if c.marker {
			_ = os.WriteFile(filepath.Join(dir, KitMarker), []byte("x"), 0644)
		}
		err := putPackage(p, c.pkg, write)
		if replaced := core.StatOK(filepath.Join(dir, "new.txt")) && !core.StatOK(filepath.Join(dir, "old.txt")); replaced != c.replaced || (err == nil) != c.replaced {
			t.Errorf("%s %s marker=%v: replaced=%v, %v", c.pkg, c.packageJSON, c.marker, replaced, err)
		}
		if left, _ := filepath.Glob(dir + ".*~"); len(left) > 0 {
			t.Errorf("left behind: %v", left)
		}
	}
}
