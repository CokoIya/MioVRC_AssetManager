package unity

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// a package zip with the given files (package.json added), optionally inside one top folder
func pkgZip(name, version, top string, extra map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{"package.json": `{"name":"` + name + `","version":"` + version + `"}`, "Editor/x.cs": "// " + name}
	for k, v := range extra {
		files[k] = v
	}
	var names []string
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names) // the same bytes every time: a mirror of a version must carry the same hash
	for _, k := range names {
		w, _ := zw.Create(top + k)
		_, _ = w.Write([]byte(files[k]))
	}
	_ = zw.Close()
	return buf.Bytes()
}

type fakeVPM struct {
	srv   *httptest.Server
	zips  map[string][]byte // path → zip
	fails map[string]bool   // paths that answer 500 (to prove a cache was used)
	hits  []string
}

func (f *fakeVPM) ver(name, version string, deps map[string]string, top string) map[string]any {
	path := "/dl/" + name + "-" + version + ".zip"
	z, ok := f.zips[path]
	if !ok {
		z = pkgZip(name, version, top, nil)
		f.zips[path] = z
	}
	sum := sha256.Sum256(z)
	v := map[string]any{"name": name, "version": version, "url": f.srv.URL + path, "zipSHA256": hex.EncodeToString(sum[:]), "unity": "2022.3"}
	if deps != nil {
		v["vpmDependencies"] = deps
	}
	return v
}

func newFakeVPM(t *testing.T) *fakeVPM {
	f := &fakeVPM{zips: map[string][]byte{}, fails: map[string]bool{}}
	listings := map[string]any{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits = append(f.hits, r.URL.Path)
		if r.Header.Get("User-Agent") == "" {
			w.WriteHeader(400)
			return
		}
		if f.fails[r.URL.Path] {
			w.WriteHeader(500)
			return
		}
		if z, ok := f.zips[r.URL.Path]; ok {
			_, _ = w.Write(z)
			return
		}
		if l, ok := listings[r.URL.Path]; ok {
			_ = json.NewEncoder(w).Encode(l)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/template/") {
			switch filepath.Base(r.URL.Path) {
			case "ProjectSettings.asset":
				_, _ = w.Write([]byte("%YAML 1.1\nPlayerSettings:\n  productName: World\n  m_ActiveColorSpace: 1\n"))
			case "TagManager.asset":
				_, _ = w.Write([]byte("TagManager: {}\n"))
			default:
				w.WriteHeader(404)
			}
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(f.srv.Close)
	pk := func(name string, versions ...map[string]any) map[string]any {
		vs := map[string]any{}
		for _, v := range versions {
			vs[v["version"].(string)] = v
		}
		return map[string]any{"versions": vs}
	}
	listings["/official.json"] = map[string]any{"name": "official", "id": "com.vrchat.repos.official", "packages": map[string]any{
		"com.vrchat.base":    pk("com.vrchat.base", f.ver("com.vrchat.base", "3.10.5", nil, ""), f.ver("com.vrchat.base", "3.10.4", nil, ""), f.ver("com.vrchat.base", "3.11.0-beta.1", nil, "")),
		"com.vrchat.avatars": pk("com.vrchat.avatars", f.ver("com.vrchat.avatars", "3.10.5", map[string]string{"com.vrchat.base": "3.10.5"}, ""), f.ver("com.vrchat.avatars", "3.11.0-beta.1", map[string]string{"com.vrchat.base": "3.11.0-beta.1"}, "")),
		vpmResolverPkg:       pk(vpmResolverPkg, f.ver(vpmResolverPkg, "0.1.29", nil, "")),
	}}
	listings["/curated.json"] = map[string]any{"name": "curated", "id": "com.vrchat.repos.curated", "packages": map[string]any{
		"vrchat.blackstartx.gesture-manager": pk("gm", f.ver("vrchat.blackstartx.gesture-manager", "3.9.9", map[string]string{"com.vrchat.avatars": ">=3.10.4 < 3.11.X"}, "GestureManager-3.9.9/")),
		"lyuma.av3emulator":                  pk("av3", f.ver("lyuma.av3emulator", "3.4.13", map[string]string{"com.vrchat.avatars": "^3.1.0"}, "")),
	}}
	listings["/nadena.json"] = map[string]any{"name": "bd_", "id": "dev.nadena.vpm", "packages": map[string]any{
		"nadena.dev.ndmf":           pk("ndmf", f.ver("nadena.dev.ndmf", "1.14.8", nil, ""), f.ver("nadena.dev.ndmf", "1.14.7", nil, ""), f.ver("nadena.dev.ndmf", "1.15.0-rc.1", nil, "")),
		"nadena.dev.modular-avatar": pk("ma", f.ver("nadena.dev.modular-avatar", "1.18.7", map[string]string{"nadena.dev.ndmf": ">=1.14.7 <2.0.0-a", "com.vrchat.avatars": ">=3.7.4"}, "")),
	}}
	listings["/lil.json"] = map[string]any{"name": "lil", "id": "io.github.lilxyzw.vpm", "packages": map[string]any{
		"jp.lilxyzw.liltoon": pk("lil", f.ver("jp.lilxyzw.liltoon", "2.3.4", nil, "")),
	}}
	listings["/anatawa.json"] = map[string]any{"name": "anatawa12", "id": "com.anatawa12.vpm", "packages": map[string]any{
		"com.anatawa12.avatar-optimizer": pk("aao", f.ver("com.anatawa12.avatar-optimizer", "1.9.20", map[string]string{"nadena.dev.ndmf": ">=1.8.0 <2.0.0", "com.vrchat.avatars": ">=3.7.0 <3.11.0"}, ""), f.ver("com.anatawa12.avatar-optimizer", "1.9.21-beta.1", nil, "")),
		"nadena.dev.ndmf":                pk("ndmf", f.ver("nadena.dev.ndmf", "1.14.8", nil, "")), // a mirror of the same version: the first repository keeps it
	}}
	listings["/vrcfury.json"] = map[string]any{"name": "VRCFury", "id": "com.vrcfury.vcc", "packages": map[string]any{
		"com.vrcfury.vrcfury": pk("vf", f.ver("com.vrcfury.vrcfury", "1.1430.0", nil, "")),
	}}
	listings["/user.json"] = map[string]any{"name": "user repo", "id": "net.example.vpm", "packages": map[string]any{
		"net.example.thing": pk("thing", f.ver("net.example.thing", "1.0.0", nil, "")),
	}}
	return f
}

func useFakeVPM(t *testing.T, f *fakeVPM) {
	t.Helper()
	old, oldTpl := vpmBuiltinRepos, templateRaw
	vpmBuiltinRepos = []vpmRepo{
		{ID: "com.vrchat.repos.official", Name: "VRChat 官方", URL: f.srv.URL + "/nope.json", Alt: f.srv.URL + "/official.json"},
		{ID: "com.vrchat.repos.curated", Name: "VRChat 精选", URL: f.srv.URL + "/curated.json"},
		{ID: "dev.nadena.vpm", Name: "bd_", URL: f.srv.URL + "/nadena.json"},
		{ID: "io.github.lilxyzw.vpm", Name: "lil", URL: f.srv.URL + "/lil.json"},
		{ID: "com.anatawa12.vpm", Name: "anatawa12", URL: f.srv.URL + "/anatawa.json"},
		{ID: "com.vrcfury.vcc", Name: "VRCFury", URL: f.srv.URL + "/vrcfury.json"},
	}
	templateRaw = f.srv.URL + "/template/"
	t.Cleanup(func() { vpmBuiltinRepos, templateRaw = old, oldTpl })
}

func TestVPMResolve(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeVPM(t)
	useFakeVPM(t, f)
	// ALCOM's settings add a repository and know a cache file; its zip cache holds one package already
	vcc := t.TempDir()
	t.Setenv("VRCLIB_VCC_DIR", vcc)
	_ = os.MkdirAll(filepath.Join(vcc, "Repos", "nadena.dev.ndmf"), 0755)
	ndmf := f.zips["/dl/nadena.dev.ndmf-1.14.8.zip"]
	_ = os.WriteFile(filepath.Join(vcc, "Repos", "nadena.dev.ndmf", "vrc-get-nadena.dev.ndmf-1.14.8.zip"), ndmf, 0644)
	f.fails["/dl/nadena.dev.ndmf-1.14.8.zip"] = true
	settings := map[string]any{"userProjects": []string{`C:\Old\Proj`}, "defaultProjectPath": `D:\VRC`,
		"userRepos": []map[string]any{{"id": "net.example.vpm", "name": "user repo", "url": f.srv.URL + "/user.json", "localPath": "Repos/net.example.vpm.json"},
			{"id": "dev.nadena.vpm", "name": "bd_", "url": f.srv.URL + "/nadena.json", "localPath": "Repos/dev.nadena.vpm.json"}}}
	sb, _ := json.Marshal(settings)
	_ = os.WriteFile(filepath.Join(vcc, "settings.json"), sb, 0644)

	repos := vpmRepos()
	if len(repos) != 7 || repos[6].ID != "net.example.vpm" || repos[2].Local != "Repos/dev.nadena.vpm.json" {
		t.Fatalf("repos %+v", repos)
	}
	idx, err := loadIndex(t.Context(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.repos) != 7 || len(idx.stale) != 0 {
		t.Errorf("index repos %v stale %v", idx.repos, idx.stale)
	}
	if v := idx.pkgs["nadena.dev.ndmf"]["1.14.8"]; v.repo != "bd_" {
		t.Errorf("mirror won: %+v", v)
	}
	wanted := map[string]string{vpmResolverPkg: "*"}
	for _, p := range basePlugins {
		if p.Default {
			wanted[p.Pkg] = "*"
		}
	}
	picked, err := idx.resolve(wanted)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"com.vrchat.avatars": "3.10.5", "com.vrchat.base": "3.10.5", vpmResolverPkg: "0.1.29", "nadena.dev.modular-avatar": "1.18.7",
		"nadena.dev.ndmf": "1.14.8", "jp.lilxyzw.liltoon": "2.3.4", "com.anatawa12.avatar-optimizer": "1.9.20", "vrchat.blackstartx.gesture-manager": "3.9.9", "com.vrcfury.vrcfury": "1.1430.0"}
	if len(picked) != len(want) {
		t.Errorf("picked %d packages, want %d", len(picked), len(want))
	}
	for n, v := range want {
		if picked[n].Version != v {
			t.Errorf("%s: %q, want %s", n, picked[n].Version, v)
		}
	}
	if _, err := idx.resolve(map[string]string{"com.vrchat.avatars": "^4.0.0"}); err == nil || !strings.Contains(err.Error(), "正式版本") {
		t.Errorf("impossible range: %v", err)
	}
	if _, err := idx.resolve(map[string]string{"no.such": "*"}); err == nil || !strings.Contains(err.Error(), "仓库里没有") {
		t.Errorf("unknown package: %v", err)
	}
	// the cached zip is used, a bad download is refused
	zp, err := fetchZip(t.Context(), nil, picked["nadena.dev.ndmf"], nil)
	if err != nil || !strings.Contains(zp, "vrc-get-nadena.dev.ndmf-1.14.8.zip") || !strings.HasPrefix(zp, vcc) {
		t.Errorf("cache: %s %v", zp, err)
	}
	bad := picked["jp.lilxyzw.liltoon"]
	bad.SHA = strings.Repeat("0", 64)
	if _, err := fetchZip(t.Context(), nil, bad, nil); err == nil || !strings.Contains(err.Error(), "校验") {
		t.Errorf("sha: %v", err)
	}
	// the web gone: ALCOM's cache of a listing serves
	_ = os.WriteFile(filepath.Join(vcc, "Repos", "net.example.vpm.json"), []byte(`{"repo":{"name":"user repo","packages":{"net.example.thing":{"versions":{"2.0.0":{"name":"net.example.thing","version":"2.0.0","url":"http://x/y.zip"}}}}},"headers":{}}`), 0644)
	f.fails["/user.json"] = true
	idx, err = loadIndex(t.Context(), nil, nil)
	if err != nil || idx.pkgs["net.example.thing"]["2.0.0"].URL == "" || len(idx.stale) != 1 {
		t.Errorf("stale cache: %v %v", err, idx.stale)
	}
}

func TestNewProject(t *testing.T) {
	st := testkit.NewStore(t)
	f := newFakeVPM(t)
	useFakeVPM(t, f)
	vcc := t.TempDir()
	t.Setenv("VRCLIB_VCC_DIR", vcc)
	_ = os.WriteFile(filepath.Join(vcc, "settings.json"), []byte(`{"userProjects":["C:\\Old"],"defaultProjectPath":"D:\\VRC","userRepos":[]}`), 0644)
	t.Setenv("VRCLIB_UNITY_DIRS", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	EdMu.Lock()
	EdCache = nil
	EdMu.Unlock()
	parent := t.TempDir()
	tc := FindToolchain(st)
	if tc.UnityWant != vrcUnity || tc.DefaultDir != `D:\VRC` || len(tc.Plugins) != len(basePlugins) || len(tc.Projects) != 1 {
		t.Errorf("toolchain %+v", tc)
	}
	for _, c := range []NewProjectReq{{Name: "", Parent: parent}, {Name: "a/b", Parent: parent}, {Name: "ok", Parent: filepath.Join(parent, "nope")}, {Name: "ok", Parent: parent, Base: "no-such-asset"}} {
		if err := StartNewProject(st, c); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
	req := NewProjectReq{Name: "My Avatar", Parent: parent, Plugins: []string{"nadena.dev.modular-avatar", "jp.lilxyzw.liltoon", "../evil"}}
	if err := StartNewProject(st, req); err != nil {
		t.Fatal(err)
	}
	if err := StartNewProject(st, req); err == nil {
		t.Error("a second job started")
	}
	var j *NewProjectJob
	for i := 0; i < 600; i++ {
		if j = NewProjectSnapshot(); j != nil && (j.Stage == "done" || j.Stage == "failed") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if j == nil || j.Stage != "done" {
		t.Fatalf("job %+v", j)
	}
	p := filepath.Join(parent, "My Avatar")
	for _, n := range []string{"com.vrchat.avatars", "com.vrchat.base", vpmResolverPkg, "nadena.dev.modular-avatar", "nadena.dev.ndmf", "jp.lilxyzw.liltoon"} {
		if !core.StatOK(filepath.Join(p, "Packages", n, "package.json")) || !core.StatOK(filepath.Join(p, "Packages", n, "Editor", "x.cs")) {
			t.Errorf("package %s missing", n)
		}
	}
	if core.StatOK(filepath.Join(p, "Packages", "com.anatawa12.avatar-optimizer")) || core.StatOK(filepath.Join(p, "Packages", "evil")) {
		t.Error("a package that was not asked for")
	}
	var vm struct {
		Dependencies map[string]struct{ Version string }
		Locked       map[string]struct {
			Version      string
			Dependencies map[string]string
		}
	}
	b, _ := os.ReadFile(filepath.Join(p, "Packages", "vpm-manifest.json"))
	if json.Unmarshal(b, &vm) != nil || vm.Dependencies["com.vrchat.avatars"].Version != "3.10.5" || vm.Locked["nadena.dev.modular-avatar"].Dependencies["nadena.dev.ndmf"] == "" || len(vm.Locked) != 6 {
		t.Errorf("vpm-manifest: %s", b)
	}
	if _, ok := vm.Dependencies["com.vrchat.base"]; ok {
		t.Error("a dependency listed as a root")
	}
	b, _ = os.ReadFile(filepath.Join(p, "Packages", "manifest.json"))
	if !strings.Contains(string(b), "com.unity.modules.animation") {
		t.Error("manifest.json")
	}
	b, _ = os.ReadFile(filepath.Join(p, "ProjectSettings", "ProjectVersion.txt"))
	if string(b) != "m_EditorVersion: "+vrcUnity+"\nm_EditorVersionWithRevision: "+vrcUnity+" ("+vrcUnityRevision+")\n" {
		t.Errorf("ProjectVersion: %q", b)
	}
	b, _ = os.ReadFile(filepath.Join(p, "ProjectSettings", "ProjectSettings.asset"))
	if !strings.Contains(string(b), "productName: My Avatar") || !strings.Contains(string(b), "m_ActiveColorSpace: 1") || !core.StatOK(filepath.Join(p, "ProjectSettings", "TagManager.asset")) {
		t.Errorf("ProjectSettings: %s", b)
	}
	if !core.StatOK(filepath.Join(p, "Assets", "My Avatar")) {
		t.Error("the player's folder")
	}
	if _, ok := KnownProject(st, p); !ok {
		t.Error("not registered")
	}
	b, _ = os.ReadFile(filepath.Join(vcc, "settings.json"))
	var vs struct {
		Projects []string `json:"userProjects"`
		Default  string   `json:"defaultProjectPath"`
	}
	if json.Unmarshal(b, &vs) != nil || len(vs.Projects) != 2 || vs.Projects[0] != p || vs.Default != `D:\VRC` {
		t.Errorf("VCC list: %s", b)
	}
	if len(j.Notes) == 0 || !strings.Contains(strings.Join(j.Notes, "|"), "官方头像模板") || !strings.Contains(strings.Join(j.Notes, "|"), "没有 Unity") {
		t.Errorf("notes %v", j.Notes)
	}
	DismissNewProject()
	if NewProjectSnapshot() != nil {
		t.Error("not dismissed")
	}
	// the same folder again is refused, a template server that is gone gives the built-in settings
	if err := StartNewProject(st, req); err == nil || !strings.Contains(err.Error(), "已经有东西") {
		t.Errorf("existing: %v", err)
	}
	templateRaw = f.srv.URL + "/gone/"
	_ = os.RemoveAll(filepath.Join(core.DataDir, "vpm", "template-avatar"))
	if err := StartNewProject(st, NewProjectReq{Name: "Second", Parent: parent}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		if j = NewProjectSnapshot(); j != nil && (j.Stage == "done" || j.Stage == "failed") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ = os.ReadFile(filepath.Join(parent, "Second", "ProjectSettings", "ProjectSettings.asset"))
	if j.Stage != "done" || !strings.Contains(string(b), "productName: Second") || !strings.Contains(string(b), "serializedVersion: 26") {
		t.Errorf("fallback settings: %s %s", j.Stage, b)
	}
	DismissNewProject()

	// an optional plugin that cannot be fetched is left out (and what depends on it), the project is still made
	wait := func() *NewProjectJob {
		var j *NewProjectJob
		for i := 0; i < 600; i++ {
			if j = NewProjectSnapshot(); j != nil && (j.Stage == "done" || j.Stage == "failed") {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		return j
	}
	f.fails["/dl/nadena.dev.ndmf-1.14.8.zip"] = true
	_ = os.RemoveAll(filepath.Join(vcc, "Repos", "nadena.dev.ndmf"))
	_ = os.Remove(filepath.Join(core.DataDir, "vpm", "vrc-get-nadena.dev.ndmf-1.14.8.zip"))
	if err := StartNewProject(st, NewProjectReq{Name: "Third", Parent: parent, Plugins: []string{"nadena.dev.modular-avatar", "com.anatawa12.avatar-optimizer", "jp.lilxyzw.liltoon"}}); err != nil {
		t.Fatal(err)
	}
	j = wait()
	notes := strings.Join(j.Notes, "|")
	if j.Stage != "done" || !strings.Contains(notes, "没装 nadena.dev.ndmf") || !strings.Contains(notes, "没装 nadena.dev.modular-avatar") || !strings.Contains(notes, "没装 com.anatawa12.avatar-optimizer") {
		t.Errorf("optional failure: %s %v %v", j.Stage, j.Err, j.Notes)
	}
	if strings.Contains(strings.Join(j.Packages, "|"), "modular-avatar") || !strings.Contains(strings.Join(j.Packages, "|"), "jp.lilxyzw.liltoon") {
		t.Errorf("packages %v", j.Packages)
	}
	if mb, _ := os.ReadFile(filepath.Join(parent, "Third", "Packages", "vpm-manifest.json")); strings.Contains(string(mb), "nadena") || !strings.Contains(string(mb), "liltoon") {
		t.Errorf("manifest %s", mb)
	}
	if core.StatOK(filepath.Join(parent, "Third", "Packages", "nadena.dev.modular-avatar")) {
		t.Error("modular avatar was put in without ndmf")
	}
	DismissNewProject()
	// the SDK itself cannot be fetched: that is the end of it
	delete(f.fails, "/dl/nadena.dev.ndmf-1.14.8.zip")
	f.fails["/dl/com.vrchat.base-3.10.5.zip"] = true
	_ = os.Remove(filepath.Join(core.DataDir, "vpm", "vrc-get-com.vrchat.base-3.10.5.zip"))
	if err := StartNewProject(st, NewProjectReq{Name: "Fourth", Parent: parent}); err != nil {
		t.Fatal(err)
	}
	if j = wait(); j.Stage != "failed" || !strings.Contains(j.Err, "com.vrchat.base") {
		t.Errorf("required failure: %s %q", j.Stage, j.Err)
	}
	DismissNewProject()
}

// With ALCOM's real repository caches at hand (staged from the player's computer), the default plugin set
// resolves against them. Skipped where they are not there.
func TestVPMRealListings(t *testing.T) {
	dir := os.Getenv("VRCLIB_REAL_VCC")
	if dir == "" {
		t.Skip("VRCLIB_REAL_VCC not set")
	}
	core.DataDir = t.TempDir()
	t.Setenv("VRCLIB_VCC_DIR", dir)
	old := vpmBuiltinRepos
	vpmBuiltinRepos = []vpmRepo{{ID: "com.vrchat.repos.official", Name: "VRChat 官方", URL: "http://127.0.0.1:9/x"}, {ID: "com.vrchat.repos.curated", Name: "VRChat 精选", URL: "http://127.0.0.1:9/y"},
		{ID: "dev.nadena.vpm", Name: "bd_", URL: "http://127.0.0.1:9/z"}, {ID: "io.github.lilxyzw.vpm", Name: "lil", URL: "http://127.0.0.1:9/l"},
		{ID: "com.anatawa12.vpm", Name: "anatawa12", URL: "http://127.0.0.1:9/a", Local: "Repos/f4011951-b082-4c9f-887a-61fa3b0d57d2.json"}, {ID: "com.vrcfury.vcc", Name: "VRCFury", URL: "http://127.0.0.1:9/v"}}
	defer func() { vpmBuiltinRepos = old }()
	idx, err := loadIndex(t.Context(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]string{vpmResolverPkg: "*"}
	for _, p := range basePlugins {
		wanted[p.Pkg] = "*"
	}
	picked, err := idx.resolve(wanted)
	if err != nil {
		t.Fatal(err)
	}
	for n, v := range picked {
		t.Logf("%s %s (%s)", n, v.Version, v.repo)
	}
	if picked["com.vrchat.avatars"].Version == "" || picked["nadena.dev.ndmf"].Version == "" || picked["com.vrchat.base"].Version != picked["com.vrchat.avatars"].Version {
		t.Errorf("picked %v", picked)
	}
}
