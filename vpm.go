package main

// VPM packages (VRChat's package format, the one VCC and ALCOM use) for the one job this program has with
// them: putting a known set of plugins into a new project. Repository listings come from the same places
// ALCOM / VCC keep theirs (their settings and caches under %LOCALAPPDATA%\VRChatCreatorCompanion), with a
// built-in list of the repositories the plugins live in as the fallback; package zips are taken from ALCOM's
// download cache when it has them, else downloaded.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type vpmRepo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Alt   string `json:"-"`                   // a second address of the same listing
	Local string `json:"localPath,omitempty"` // ALCOM / VCC's cache file of it
}

// The repositories the plugins of a base project live in. VCC / ALCOM's own settings add to these.
var vpmBuiltinRepos = []vpmRepo{
	{ID: "com.vrchat.repos.official", Name: "VRChat 官方", URL: "https://packages.vrchat.com/official?download", Alt: "https://vrchat.github.io/packages/index.json"},
	{ID: "com.vrchat.repos.curated", Name: "VRChat 精选", URL: "https://packages.vrchat.com/curated?download", Alt: "https://vrchat-community.github.io/vpm-listing-curated/index.json"},
	{ID: "dev.nadena.vpm", Name: "bd_（Modular Avatar）", URL: "https://vpm.nadena.dev/vpm.json"},
	{ID: "io.github.lilxyzw.vpm", Name: "lilxyzw（lilToon）", URL: "https://lilxyzw.github.io/vpm-repos/vpm.json"},
	{ID: "com.anatawa12.vpm", Name: "anatawa12（Avatar Optimizer）", URL: "https://vpm.anatawa12.com/vpm.json"},
	{ID: "com.vrcfury.vcc", Name: "VRCFury", URL: "https://vcc.vrcfury.com"},
}

// the cache files ALCOM / VCC keep for the built-in repositories
var vpmCacheNames = map[string]string{
	"com.vrchat.repos.official": "vrc-official.json",
	"com.vrchat.repos.curated":  "vrc-curated.json",
}

// A plugin the player can tick for a new project.
type vpmPlugin struct {
	Pkg     string `json:"pkg"`
	Label   string `json:"label"`
	Note    string `json:"note"`
	Default bool   `json:"default"`
	Fixed   bool   `json:"fixed,omitempty"` // always in
}

var basePlugins = []vpmPlugin{
	{Pkg: "com.vrchat.avatars", Label: "VRChat SDK（Avatars）", Note: "上传头像必需", Default: true, Fixed: true},
	{Pkg: "nadena.dev.modular-avatar", Label: "Modular Avatar", Note: "穿戴、菜单都靠它（会一起装 NDMF）", Default: true},
	{Pkg: "jp.lilxyzw.liltoon", Label: "lilToon", Note: "绝大多数素体和衣服用的着色器", Default: true},
	{Pkg: "com.anatawa12.avatar-optimizer", Label: "Avatar Optimizer（AAO）", Note: "上传时自动优化", Default: true},
	{Pkg: "vrchat.blackstartx.gesture-manager", Label: "Gesture Manager", Note: "在 Unity 里试菜单和手势", Default: true},
	{Pkg: "com.vrcfury.vrcfury", Label: "VRCFury", Note: "很多道具和插件需要", Default: true},
	{Pkg: "lyuma.av3emulator", Label: "Av3Emulator", Note: "更完整的本地模拟（可选）", Default: false},
}

const vpmResolverPkg = "com.vrchat.core.vpm-resolver" // VCC puts it into every project; ALCOM too

type vpmVersion struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	URL     string            `json:"url"`
	SHA     string            `json:"zipSHA256"`
	Unity   string            `json:"unity"`
	Deps    map[string]string `json:"vpmDependencies"`
	Display string            `json:"displayName"`
	repo    string
	sv      semver
}

type vpmListing struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	URL      string `json:"url"`
	Packages map[string]struct {
		Versions map[string]vpmVersion `json:"versions"`
	} `json:"packages"`
}

// vpmIndex: every version of every package the repositories offer, by name then version.
type vpmIndex struct {
	pkgs  map[string]map[string]vpmVersion
	repos []string // where the listings came from, for the report
	stale []string // repositories that could only be read from an old cache
}

func vccDir() string {
	if v := os.Getenv("VRCLIB_VCC_DIR"); v != "" { // tests
		return v
	}
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		return ""
	}
	return filepath.Join(la, "VRChatCreatorCompanion")
}

// vpmRepos: the built-in repositories plus the ones the player added in ALCOM / VCC.
func vpmRepos() []vpmRepo {
	out := append([]vpmRepo{}, vpmBuiltinRepos...)
	if base := os.Getenv("VRCLIB_VPM_BASE"); base != "" { // tests: every built-in repository from one local server
		for i := range out {
			out[i].URL, out[i].Alt = strings.TrimRight(base, "/")+"/"+out[i].ID+".json", ""
		}
	}
	seen := map[string]bool{}
	for _, r := range out {
		seen[repoKey(r.URL)] = true
		if r.Alt != "" {
			seen[repoKey(r.Alt)] = true
		}
	}
	if d := vccDir(); d != "" {
		if b, err := os.ReadFile(filepath.Join(d, "settings.json")); err == nil {
			var s struct {
				UserRepos []vpmRepo `json:"userRepos"`
			}
			if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &s) == nil {
				for _, r := range s.UserRepos {
					if r.URL == "" {
						continue
					}
					if seen[repoKey(r.URL)] { // the built-in one, but now we know its cache file
						for i := range out {
							if repoKey(out[i].URL) == repoKey(r.URL) && out[i].Local == "" {
								out[i].Local = r.Local
							}
						}
						continue
					}
					seen[repoKey(r.URL)] = true
					out = append(out, r)
				}
			}
		}
	}
	return out
}

func repoKey(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	u = strings.TrimSuffix(u, "?download")
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	return u
}

// listingCacheFile: where ALCOM / VCC keep a repository's listing, if they do.
func listingCacheFile(r vpmRepo) string {
	d := vccDir()
	if d == "" {
		return ""
	}
	if r.Local != "" {
		p := filepath.FromSlash(strings.ReplaceAll(r.Local, `\`, "/"))
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(d, p)
	}
	if n := vpmCacheNames[r.ID]; n != "" {
		return filepath.Join(d, "Repos", n)
	}
	if r.ID != "" {
		return filepath.Join(d, "Repos", r.ID+".json")
	}
	return ""
}

func parseListing(b []byte) (*vpmListing, error) {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	var wrapped struct {
		Repo json.RawMessage `json:"repo"`
	}
	if json.Unmarshal(b, &wrapped) == nil && len(wrapped.Repo) > 2 { // ALCOM / VCC cache files wrap the listing
		b = wrapped.Repo
	}
	var l vpmListing
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	if l.Packages == nil {
		return nil, errors.New("不是 VPM 仓库列表")
	}
	return &l, nil
}

var vpmHTTP = &http.Client{Timeout: 60 * time.Second}

var vpmUA = "MioVRCA/" + appVersion + " (VPM client; +https://miovrc.com/vrca/)"

func vpmGet(ctx context.Context, st *Store, u string, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", vpmUA)
	req.Header.Set("Accept", "*/*")
	c := vpmHTTP
	if st != nil {
		c = httpClient(st)
	}
	c = &http.Client{Transport: c.Transport, Timeout: timeout}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// loadIndex reads every repository: fresh from the web when it can, else the cache ALCOM / VCC keep.
// Our own copy of each listing goes to <data>/vpm/<id>.json for next time.
func loadIndex(ctx context.Context, st *Store, progress func(string)) (*vpmIndex, error) {
	idx := &vpmIndex{pkgs: map[string]map[string]vpmVersion{}}
	own := filepath.Join(dataDir, "vpm")
	_ = os.MkdirAll(own, 0755)
	var failed []string
	for _, r := range vpmRepos() {
		if progress != nil {
			progress("读取仓库 " + r.Name)
		}
		ownFile := filepath.Join(own, safeFileName(r.ID+".json"))
		var l *vpmListing
		var err error
		for _, u := range []string{r.URL, r.Alt} {
			if u == "" || l != nil {
				continue
			}
			if b, e := vpmGet(ctx, st, u, 40*time.Second); e == nil {
				if l, err = parseListing(b); err == nil {
					_ = os.WriteFile(ownFile, b, 0644)
				}
			} else {
				err = e
			}
		}
		if l == nil { // the web did not answer: what ALCOM / VCC or we saved last time
			for _, f := range []string{listingCacheFile(r), ownFile} {
				if f == "" {
					continue
				}
				if b, e := os.ReadFile(f); e == nil {
					if l, _ = parseListing(b); l != nil {
						idx.stale = append(idx.stale, r.Name)
						break
					}
				}
			}
		}
		if l == nil {
			failed = append(failed, r.Name+"（"+trimErr(err)+"）")
			continue
		}
		idx.repos = append(idx.repos, r.Name)
		for name, p := range l.Packages {
			m := idx.pkgs[name]
			if m == nil {
				m = map[string]vpmVersion{}
				idx.pkgs[name] = m
			}
			for ver, v := range p.Versions {
				sv, ok := parseSemver(ver)
				if !ok {
					continue
				}
				if _, have := m[ver]; have { // the first repository that offers a version keeps it
					continue
				}
				v.Name, v.Version, v.sv, v.repo = name, ver, sv, r.Name
				m[ver] = v
			}
		}
	}
	if len(idx.pkgs) == 0 {
		return nil, errors.New("一个插件仓库都读不到：" + strings.Join(failed, "；") + "。检查网络（需要代理的话在设置里填上），或者先在 ALCOM / VCC 里刷新一次仓库")
	}
	if len(failed) > 0 {
		idx.stale = append(idx.stale, "读不到："+strings.Join(failed, "；"))
	}
	return idx, nil
}

func safeFileName(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c < 0x20 || strings.ContainsRune(`\/:*?"<>|`, c) {
			c = '_'
		}
		b.WriteRune(c)
	}
	return b.String()
}

// resolve picks a version for every wanted package and everything they need: the highest release that
// satisfies every range asked of it. Prereleases are never picked.
func (idx *vpmIndex) resolve(wanted map[string]string) (map[string]vpmVersion, error) {
	constraints := map[string][]string{}
	for n, r := range wanted {
		constraints[n] = append(constraints[n], r)
	}
	picked := map[string]vpmVersion{}
	for round := 0; round < 30; round++ {
		changed := false
		names := make([]string, 0, len(constraints))
		for n := range constraints {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			best, err := idx.best(n, constraints[n])
			if err != nil {
				return nil, err
			}
			if cur, ok := picked[n]; ok && cur.Version == best.Version {
				continue
			}
			picked[n] = best
			changed = true
			for dep, r := range best.Deps {
				if !containsStr(constraints[dep], r) {
					constraints[dep] = append(constraints[dep], r)
				}
			}
		}
		if !changed {
			return picked, nil
		}
	}
	return nil, errors.New("插件之间的版本要求转不出结果")
}

func (idx *vpmIndex) best(name string, ranges []string) (vpmVersion, error) {
	vers := idx.pkgs[name]
	if len(vers) == 0 {
		return vpmVersion{}, errors.New("仓库里没有 " + name)
	}
	var parsed []verRange
	for _, r := range ranges {
		pr, err := parseRange(r)
		if err != nil {
			return vpmVersion{}, errors.New(name + "：" + err.Error())
		}
		parsed = append(parsed, pr)
	}
	var best *vpmVersion
	for ver := range vers {
		v := vers[ver]
		if v.sv.pre != "" || v.URL == "" {
			continue
		}
		ok := true
		for _, pr := range parsed {
			if !pr.match(v.sv) {
				ok = false
				break
			}
		}
		if ok && (best == nil || v.sv.cmp(best.sv) > 0) {
			b := v
			best = &b
		}
	}
	if best == nil {
		var have []string
		for ver := range vers {
			have = append(have, ver)
		}
		sort.Strings(have)
		if len(have) > 6 {
			have = have[len(have)-6:]
		}
		return vpmVersion{}, fmt.Errorf("%s 没有同时满足 %s 的正式版本（仓库里有 %s）", name, strings.Join(ranges, " 和 "), strings.Join(have, "、"))
	}
	return *best, nil
}

// ---------- the zip of a version ----------

// cachedZip: the package zip as ALCOM downloaded it, or as we did before.
func cachedZip(v vpmVersion) string {
	name := "vrc-get-" + v.Name + "-" + v.Version + ".zip"
	var cands []string
	if d := vccDir(); d != "" {
		cands = append(cands, filepath.Join(d, "Repos", v.Name, name))
	}
	cands = append(cands, filepath.Join(dataDir, "vpm", name))
	for _, p := range cands {
		if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
			if v.SHA == "" || fileSHA256(p) == strings.ToLower(v.SHA) {
				return p
			}
		}
	}
	return ""
}

func fileSHA256(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// fetchZip gets the version's zip into our cache (or finds ALCOM's) and returns its path.
func fetchZip(ctx context.Context, st *Store, v vpmVersion, progress func(string)) (string, error) {
	if p := cachedZip(v); p != "" {
		return p, nil
	}
	if progress != nil {
		progress("下载 " + v.Name + " " + v.Version)
	}
	b, err := vpmGet(ctx, st, v.URL, 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("下载 %s %s 失败：%v", v.Name, v.Version, trimErr(err))
	}
	if v.SHA != "" {
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != strings.ToLower(v.SHA) {
			return "", fmt.Errorf("%s %s 下载下来的文件校验不对（可能被代理改了，或者仓库信息过期）", v.Name, v.Version)
		}
	}
	if _, err := zip.NewReader(bytes.NewReader(b), int64(len(b))); err != nil {
		return "", fmt.Errorf("%s %s 下载下来的不是 zip", v.Name, v.Version)
	}
	dir := filepath.Join(dataDir, "vpm")
	_ = os.MkdirAll(dir, 0755)
	p := filepath.Join(dir, "vrc-get-"+v.Name+"-"+v.Version+".zip")
	if err := os.WriteFile(p+".tmp", b, 0644); err != nil {
		return "", err
	}
	return p, os.Rename(p+".tmp", p)
}

// extractPackage puts the zip's files into Packages/<name>. A zip whose package.json sits inside one top
// folder (a source archive) has that folder stripped.
func extractPackage(zipPath, project, name string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	prefix := ""
	rootHas := false
	for _, f := range zr.File {
		if f.Name == "package.json" {
			rootHas = true
			break
		}
	}
	if !rootHas {
		for _, f := range zr.File {
			if i := strings.Index(f.Name, "/"); i > 0 && f.Name[i+1:] == "package.json" {
				prefix = f.Name[:i+1]
				break
			}
		}
		if prefix == "" {
			return errors.New(name + " 的压缩包里没有 package.json")
		}
	}
	return putPackage(project, name, func(tmp string) error {
		for _, f := range zr.File {
			rel := strings.TrimPrefix(f.Name, prefix)
			if rel == "" || (prefix != "" && !strings.HasPrefix(f.Name, prefix)) {
				continue
			}
			if strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
				continue
			}
			dst := filepath.Join(tmp, filepath.FromSlash(rel))
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
		return nil
	})
}

// writeVpmManifest: Packages/vpm-manifest.json the way VCC / ALCOM write it, so they take the project over
// from here without complaint.
func writeVpmManifest(project string, roots map[string]string, picked map[string]vpmVersion) error {
	type dep struct {
		Version string `json:"version"`
	}
	type lock struct {
		Version string            `json:"version"`
		Deps    map[string]string `json:"dependencies"`
	}
	m := struct {
		Dependencies map[string]dep  `json:"dependencies"`
		Locked       map[string]lock `json:"locked"`
	}{map[string]dep{}, map[string]lock{}}
	for n := range roots {
		if v, ok := picked[n]; ok {
			m.Dependencies[n] = dep{v.Version}
		}
	}
	for n, v := range picked {
		deps := v.Deps
		if deps == nil {
			deps = map[string]string{}
		}
		m.Locked[n] = lock{v.Version, deps}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(filepath.Join(project, "Packages", "vpm-manifest.json"), b, 0644)
}
