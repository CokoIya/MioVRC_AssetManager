package library

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
)

type guidCacheEntry struct {
	Size  int64    `json:"size"`
	MTime int64    `json:"mtime"`
	GUIDs []string `json:"guids"`
	Err   string   `json:"err,omitempty"`
}

type guidCache struct {
	mu      sync.Mutex
	path    string
	Entries map[string]*guidCacheEntry `json:"entries"`
	dirty   bool
}

func loadGuidCache() *guidCache {
	c := &guidCache{path: filepath.Join(core.DataDir, "guidcache.json"), Entries: map[string]*guidCacheEntry{}}
	if b, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(b, c)
		if c.Entries == nil {
			c.Entries = map[string]*guidCacheEntry{}
		}
	}
	return c
}

func (c *guidCache) save() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := c.path + ".tmp"
	if os.WriteFile(tmp, b, 0644) == nil {
		_ = os.Rename(tmp, c.path)
		c.dirty = false
	}
}

func IsHex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < 32; i++ {
		ch := s[i]
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func readPackageGUIDs(r io.Reader) ([]string, error) {
	gz, err := gzip.NewReader(bufio.NewReaderSize(r, 1<<20))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	set := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return keys(set), err
		}
		n := strings.TrimPrefix(h.Name, "./")
		if i := strings.IndexByte(n, '/'); i >= 0 {
			n = n[:i]
		}
		if IsHex32(n) {
			set[strings.ToLower(n)] = true
		}
	}
	return keys(set), nil
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// guidsOfFile returns GUIDs inside a .unitypackage or inside every .unitypackage in a .zip (cached by size+mtime).
func (c *guidCache) guidsOfFile(path string) []string {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	k := strings.ToLower(path)
	c.mu.Lock()
	if e, ok := c.Entries[k]; ok && e.Size == fi.Size() && e.MTime == fi.ModTime().Unix() {
		c.mu.Unlock()
		return e.GUIDs
	}
	c.mu.Unlock()
	var guids []string
	var rerr error
	if strings.HasSuffix(k, ".zip") {
		zr, err := zip.OpenReader(path)
		if err != nil {
			rerr = err
		} else {
			set := map[string]bool{}
			for _, f := range zr.File {
				if !strings.HasSuffix(strings.ToLower(f.Name), ".unitypackage") {
					continue
				}
				rc, err := f.Open()
				if err != nil {
					continue
				}
				g, _ := readPackageGUIDs(rc)
				rc.Close()
				for _, x := range g {
					set[x] = true
				}
			}
			zr.Close()
			guids = keys(set)
		}
	} else {
		f, err := os.Open(path)
		if err != nil {
			rerr = err
		} else {
			guids, rerr = readPackageGUIDs(f)
			f.Close()
		}
	}
	e := &guidCacheEntry{Size: fi.Size(), MTime: fi.ModTime().Unix(), GUIDs: guids}
	if rerr != nil {
		e.Err = rerr.Error()
	}
	c.mu.Lock()
	c.Entries[k] = e
	c.dirty = true
	c.mu.Unlock()
	return guids
}

func ReadMetaGUID(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 400)
	n, _ := io.ReadFull(f, buf)
	s := string(buf[:n])
	i := strings.Index(s, "guid: ")
	if i < 0 || i+6+32 > len(s) {
		return ""
	}
	g := s[i+6 : i+6+32]
	if IsHex32(g) {
		return strings.ToLower(g)
	}
	return ""
}

// metaGUIDs collects GUIDs of .meta files directly inside the given dirs.
func metaGUIDs(dirs []string) []string {
	set := map[string]bool{}
	for _, d := range dirs {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".meta") {
				if g := ReadMetaGUID(filepath.Join(d, e.Name())); g != "" {
					set[g] = true
				}
			}
		}
	}
	return keys(set)
}

func findProjects(roots []string) []core.ProjectInfo {
	var out []core.ProjectInfo
	seen := map[string]bool{}
	for _, r := range roots {
		if core.IsUnityProject(r) {
			if !seen[core.PathKey(r)] {
				seen[core.PathKey(r)] = true
				out = append(out, core.ProjectInfo{Name: filepath.Base(r), Path: r})
			}
			continue
		}
		ents, err := os.ReadDir(r)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(r, e.Name())
			if core.IsUnityProject(p) && !seen[core.PathKey(p)] {
				seen[core.PathKey(p)] = true
				out = append(out, core.ProjectInfo{Name: e.Name(), Path: p})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// projectGUIDs returns every GUID in the project plus the subset living under Packages/ (shared libraries such as lilToon).
// projectGUIDs: every GUID of the project, the ones that belong to a package, and for the ones under Assets/
// the folder they sit in (Assets/<shop>/<item>, or Assets/<item>), as an index into folders.
func projectGUIDs(p string) (set, pkg map[string]bool, fold map[string]uint16, folders []string) {
	set = map[string]bool{}
	pkg = map[string]bool{}
	fold = map[string]uint16{}
	folderIdx := map[string]uint16{}
	var mu sync.Mutex
	type job struct {
		path string
		pkg  bool
	}
	paths := make(chan job, 256)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range paths {
				if g := ReadMetaGUID(j.path); g != "" {
					mu.Lock()
					set[g] = true
					if j.pkg {
						pkg[g] = true
					} else if f := assetFolder(p, j.path); f != "" {
						i, ok := folderIdx[f]
						if !ok && len(folders) < 60000 {
							i = uint16(len(folders))
							folderIdx[f] = i
							folders = append(folders, f)
							ok = true
						}
						if ok {
							fold[g] = i
						}
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, sub := range []string{"Assets", "Packages"} {
		root := filepath.Join(p, sub)
		isPkg := sub == "Packages"
		_ = filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				n := strings.ToLower(d.Name())
				if isPkg && (strings.HasPrefix(n, "com.vrchat.") || n == "com.vrcfury.temp" || n == "__generated") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(strings.ToLower(d.Name()), ".meta") {
				paths <- job{fp, isPkg}
			}
			return nil
		})
	}
	close(paths)
	wg.Wait()
	return set, pkg, fold, folders
}

// assetFolder: "Assets/<a>/<b>" (or "Assets/<a>") for a .meta file under the project's Assets.
func assetFolder(project, metaPath string) string {
	rel, err := filepath.Rel(project, metaPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 || parts[0] != "Assets" {
		return ""
	}
	if len(parts) >= 4 {
		return "Assets/" + parts[1] + "/" + parts[2]
	}
	return "Assets/" + parts[1]
}

// RunUsageScan computes which Unity projects use which assets by comparing GUIDs.
func RunUsageScan(st *core.Store, prog *core.Task) {
	st.Mu.RLock()
	roots := append([]string{}, st.Settings.ProjectRoots...)
	assets := append([]*core.Asset{}, st.Assets...)
	st.Mu.RUnlock()

	cache := loadGuidCache()
	projects := findProjects(roots)
	prog.Set(0, len(projects)+len(assets), "索引 Unity 工程")
	guidProj := map[string]uint64{}
	libGuids := map[string]bool{}
	projFold := make([]map[string]uint16, len(projects))
	projDirs := make([][]string, len(projects))
	for i, p := range projects {
		prog.Set(i, len(projects)+len(assets), "索引工程 "+p.Name)
		set, pkg, fold, dirs := projectGUIDs(p.Path)
		projFold[i], projDirs[i] = fold, dirs
		for g := range pkg {
			libGuids[g] = true
		}
		projects[i].Guids = len(set)
		for g := range set {
			guidProj[g] |= 1 << uint(i)
		}
	}

	// per-asset guid sets
	assetGuids := make([][]string, len(assets))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var done int
	var dmu sync.Mutex
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				a := assets[i]
				set := map[string]bool{}
				for _, p := range a.Packages {
					for _, g := range cache.guidsOfFile(p) {
						set[g] = true
					}
				}
				for _, p := range a.Archives {
					for _, g := range cache.guidsOfFile(p) {
						set[g] = true
					}
				}
				for _, g := range metaGUIDs(a.MetaDirs) {
					set[g] = true
				}
				assetGuids[i] = keys(set)
				dmu.Lock()
				done++
				prog.Set(len(projects)+done, len(projects)+len(assets), "分析素材 "+a.Name)
				dmu.Unlock()
			}
		}()
	}
	for i := range assets {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	cache.save()

	// GUIDs shared by 3+ assets (bundled libraries like lilToon) are ignored for matching
	freq := map[string]int{}
	for _, gs := range assetGuids {
		for _, g := range gs {
			freq[g]++
		}
	}
	usage := map[string][]core.Usage{}
	gcount := map[string]int{}
	counts := map[string]int{}
	for i, a := range assets {
		var eligible, own []string
		for _, g := range assetGuids[i] {
			if freq[g] < 3 && g != "00000000000000000000000000000000" {
				eligible = append(eligible, g)
				if !libGuids[g] {
					own = append(own, g)
				}
			}
		}
		// files that are also installed as a package (a bundled lilToon, MA …) say nothing about this asset;
		// unless the asset is mostly such a package itself
		if len(own) >= 5 && len(own)*5 >= len(eligible) {
			eligible = own
		}
		var us []core.Usage
		if len(eligible) > 0 {
			matched := make([]int, len(projects))
			for _, g := range eligible {
				m := guidProj[g]
				for j := range projects {
					if m&(1<<uint(j)) != 0 {
						matched[j]++
					}
				}
			}
			for j, p := range projects {
				if matched[j] < 3 {
					continue
				}
				r := float64(matched[j]) / float64(len(eligible))
				status := ""
				switch {
				case r >= 0.2:
					status = "used"
				case r >= 0.03:
					status = "partial"
				default:
					continue
				}
				// where in the project most of its files sit
				count := map[uint16]int{}
				for _, g := range eligible {
					if fi, ok := projFold[j][g]; ok {
						count[fi]++
					}
				}
				folder, best := "", 0
				for fi, n := range count {
					if n > best || (n == best && projDirs[j][fi] < folder) {
						folder, best = projDirs[j][fi], n
					}
				}
				us = append(us, core.Usage{Project: p.Name, Matched: matched[j], Total: len(eligible), Ratio: r, Status: status, Folder: folder})
				counts[p.Name]++
			}
		}
		usage[a.Key] = us
		gcount[a.Key] = len(assetGuids[i])
	}
	for i := range projects {
		projects[i].Assets = counts[projects[i].Name]
	}

	st.Mu.Lock()
	for _, a := range st.Assets {
		if u, ok := usage[a.Key]; ok {
			a.Usage = u
			a.GuidCount = gcount[a.Key]
		}
	}
	st.Projects = projects
	st.LastUsage = time.Now().Unix()
	st.Mu.Unlock()
	prog.Set(len(projects)+len(assets), len(projects)+len(assets), "完成")
}
