package unity

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/naming"
)

// ---------- the project ----------

type projIndex struct {
	root   string
	byGUID map[string]string // guid → path inside the project ("Assets/a/b.prefab")
	byPath map[string]string // lower-case path → guid
}

func indexProject(root string) *projIndex {
	idx := &projIndex{root: root, byGUID: map[string]string{}, byPath: map[string]string{}}
	for _, sub := range []string{"Assets", "Packages"} {
		base := filepath.Join(root, sub)
		_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".meta") {
				return nil
			}
			g := library.ReadMetaGUID(p)
			if g == "" {
				return nil
			}
			rel, err := filepath.Rel(root, strings.TrimSuffix(p, filepath.Ext(p)))
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			idx.byGUID[g] = rel
			idx.byPath[strings.ToLower(rel)] = g
			return nil
		})
	}
	return idx
}

// ---------- the package ----------

type pkgItem struct {
	guid     string
	path     string // as written in the package
	meta     []byte
	hasAsset bool
}

func pkgEntry(name string) (guid, part string) {
	name = strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "./")
	i := strings.IndexByte(name, '/')
	if i <= 0 {
		return "", ""
	}
	g := strings.ToLower(name[:i])
	if !library.IsHex32(g) {
		return "", ""
	}
	return g, name[i+1:]
}

func openPackage(pkg string) (*tar.Reader, func(), error) {
	f, err := os.Open(pkg)
	if err != nil {
		return nil, nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, errors.New("不是有效的 unitypackage")
	}
	return tar.NewReader(gz), func() { gz.Close(); f.Close() }, nil
}

// readPackageItems: the first pass — names, meta files, which entries carry a file.
func readPackageItems(pkg string) (map[string]*pkgItem, error) {
	tr, closeFn, err := openPackage(pkg)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	items := map[string]*pkgItem{}
	get := func(g string) *pkgItem {
		if items[g] == nil {
			items[g] = &pkgItem{guid: g}
		}
		return items[g]
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("unitypackage 读取不完整（文件可能已损坏）")
		}
		g, part := pkgEntry(h.Name)
		if g == "" {
			continue
		}
		switch part {
		case "pathname":
			b, _ := io.ReadAll(io.LimitReader(tr, 4096))
			line := strings.SplitN(strings.ReplaceAll(string(b), "\r", ""), "\n", 2)[0]
			get(g).path = strings.TrimSpace(line)
		case "asset.meta":
			b, _ := io.ReadAll(io.LimitReader(tr, 4<<20))
			get(g).meta = b
		case "asset":
			get(g).hasAsset = true
		}
	}
	return items, nil
}

// cleanPkgPath: a package path that is safe to write ("" when it is not).
func cleanPkgPath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return ""
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return ""
		}
	}
	p = path.Clean(p)
	top := strings.SplitN(p, "/", 2)[0]
	if top != "Assets" && top != "Packages" {
		return ""
	}
	if top == "Packages" && !strings.Contains(strings.TrimPrefix(p, "Packages/"), "/") {
		return "" // Packages/manifest.json and the like belong to the project
	}
	return p
}

type importStats struct {
	New, Updated, Folders, Skipped int
	Tops                           []string
	Kept                           int      // files of an installed package (Packages/) the unitypackage would have replaced
	KeptPkgs                       []string // … and the packages they belong to
}

// installedPackage: the package under Packages/ a path belongs to. What VCC / ALCOM installed there is never
// written over by a unitypackage (a shop's bundled copy of lilToon must not replace the installed one).
func installedPackage(paths ...string) (pkg string, ok bool) {
	for _, p := range paths {
		segs := strings.Split(strings.ReplaceAll(p, `\`, "/"), "/")
		if len(segs) >= 2 && strings.EqualFold(segs[0], "Packages") {
			return segs[1], true
		}
	}
	return "", false
}

// target: where the item goes in the project (Unity keeps an asset it already has where it is).
func (idx *projIndex) target(it *pkgItem, isDir bool) string {
	if old := idx.byGUID[it.guid]; old != "" {
		return old
	}
	p := cleanPkgPath(it.path)
	if p == "" {
		return ""
	}
	if g, ok := idx.byPath[strings.ToLower(p)]; ok && g != it.guid && !isDir {
		// another asset has this name: Unity would add a number
		ext := path.Ext(p)
		stem := strings.TrimSuffix(p, ext)
		for i := 1; ; i++ {
			q := fmt.Sprintf("%s %d%s", stem, i, ext)
			if _, taken := idx.byPath[strings.ToLower(q)]; !taken {
				if _, err := os.Stat(filepath.Join(idx.root, filepath.FromSlash(q))); os.IsNotExist(err) {
					return q
				}
			}
		}
	}
	return p
}

func writeMeta(full string, it *pkgItem) error {
	meta := it.meta
	if len(meta) == 0 {
		meta = []byte("fileFormatVersion: 2\nguid: " + it.guid + "\n")
	}
	return os.WriteFile(full+".meta", meta, 0644)
}

// importUnityPackage writes the package's files into the project.
func importUnityPackage(pkg string, idx *projIndex) (importStats, error) {
	var stats importStats
	items, err := readPackageItems(pkg)
	if err != nil {
		return stats, err
	}
	dest := map[string]string{}
	tops := map[string]bool{}
	kept := map[string]bool{}
	// folders first (shortest paths first), so their .meta keep the package's GUIDs
	var folders []*pkgItem
	for _, it := range items {
		if !it.hasAsset {
			folders = append(folders, it)
		}
	}
	sort.Slice(folders, func(i, j int) bool { return len(folders[i].path) < len(folders[j].path) })
	for _, it := range folders {
		rel := idx.target(it, true)
		if rel == "" {
			stats.Skipped++
			continue
		}
		if _, ok := installedPackage(rel, it.path); ok {
			continue
		}
		full := filepath.Join(idx.root, filepath.FromSlash(rel))
		if err := os.MkdirAll(full, 0755); err != nil {
			return stats, fmt.Errorf("无法创建文件夹 %s：%v", rel, err)
		}
		if g := idx.byPath[strings.ToLower(rel)]; g == "" || g == it.guid {
			if err := writeMeta(full, it); err != nil {
				return stats, err
			}
			idx.byGUID[it.guid], idx.byPath[strings.ToLower(rel)] = rel, it.guid
		}
		stats.Folders++
	}
	for _, it := range items {
		if !it.hasAsset {
			continue
		}
		rel := idx.target(it, false)
		if rel == "" {
			stats.Skipped++
			continue
		}
		if pkg, ok := installedPackage(rel, it.path); ok { // where it is now, or where the package wants it
			stats.Kept++
			kept[pkg] = true
			continue
		}
		dest[it.guid] = rel
		segs := strings.Split(rel, "/")
		if len(segs) > 2 {
			tops[segs[0]+"/"+segs[1]] = true
		} else {
			tops[rel] = true
		}
	}
	// second pass: the files themselves
	tr, closeFn, err := openPackage(pkg)
	if err != nil {
		return stats, err
	}
	defer closeFn()
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return stats, errors.New("unitypackage 读取不完整（文件可能已损坏）")
		}
		g, part := pkgEntry(h.Name)
		if part != "asset" || dest[g] == "" {
			continue
		}
		rel := dest[g]
		full := filepath.Join(idx.root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return stats, fmt.Errorf("无法创建文件夹：%v", err)
		}
		_, existed := os.Stat(full)
		tmp := full + ".mioimport~" // Unity skips names ending in "~" while it is being written
		w, err := os.Create(tmp)
		if err != nil {
			return stats, fmt.Errorf("无法写入 %s：%v", rel, err)
		}
		_, cerr := io.Copy(w, tr)
		if err := w.Close(); cerr == nil {
			cerr = err
		}
		if cerr != nil {
			_ = os.Remove(tmp)
			return stats, fmt.Errorf("无法写入 %s：%v", rel, cerr)
		}
		if err := os.Rename(tmp, full); err != nil { // replaces the old file in one step
			_ = os.Remove(tmp)
			return stats, fmt.Errorf("无法写入 %s：%v（文件可能被 Unity 占用）", rel, err)
		}
		if err := writeMeta(full, items[g]); err != nil {
			return stats, err
		}
		idx.byGUID[g], idx.byPath[strings.ToLower(rel)] = rel, g
		if existed == nil {
			stats.Updated++
		} else {
			stats.New++
		}
	}
	for t := range tops {
		stats.Tops = append(stats.Tops, t)
	}
	sort.Strings(stats.Tops)
	for p := range kept {
		stats.KeptPkgs = append(stats.KeptPkgs, p)
	}
	sort.Strings(stats.KeptPkgs)
	return stats, nil
}

// keptNote: what the result says about files that were left alone ("" when there were none).
func keptNote(n int, pkgs []string) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("已跳过 %d 个属于已安装插件（Packages）的文件，未覆盖：%s", n, strings.Join(pkgs, "、"))
}

// ---------- the import job ----------

type PkgChoice struct {
	Path  string   `json:"path"`
	Name  string   `json:"name"`
	Size  int64    `json:"size"`
	Bases []string `json:"bases,omitempty"`
	Pick  bool     `json:"pick"`
}

type ImportJob struct {
	ID       int64       `json:"id"`
	Key      string      `json:"key"`
	Project  string      `json:"project"`
	Stage    string      `json:"stage"` // unpack, choose, import, done, failed
	Msg      string      `json:"msg"`
	Done     int         `json:"done"`
	Total    int         `json:"total"`
	Choices  []PkgChoice `json:"choices,omitempty"`
	Failed   []string    `json:"failed,omitempty"` // archives that did not unpack
	Unpacked int         `json:"unpacked,omitempty"`
	Removed  int         `json:"removed,omitempty"`
	Imported []string    `json:"imported,omitempty"`
	Files    int         `json:"files,omitempty"`
	Tops     []string    `json:"tops,omitempty"`
	Kept     int         `json:"kept,omitempty"`     // files of installed packages that were not written over
	KeptPkgs []string    `json:"keptPkgs,omitempty"` // the packages they belong to
	Bundle   int         `json:"bundle,omitempty"`   // a collection of products (合集包): split into this many cards, not imported
	Split    bool        `json:"split,omitempty"`    // splitting was all that was asked for: there is no project
	Err      string      `json:"err,omitempty"`
	At       int64       `json:"at"`

	places  []string
	pwd     string
	recycle bool
	chosen  chan []string
}

var (
	impMu      sync.Mutex
	impJob     *ImportJob
	TaskImport = &core.Task{Name: "import", Label: labelImport}
)

// what the task is called while it runs: it imports, or it only splits a collection
const (
	labelImport = "导入到 Unity 工程"
	labelSplit  = "拆分合集包"
)

func ImportSnapshot() *ImportJob {
	impMu.Lock()
	defer impMu.Unlock()
	if impJob == nil {
		return nil
	}
	c := *impJob
	c.Choices = append([]PkgChoice(nil), impJob.Choices...)
	return &c
}

// setImp changes the job; the window reloads everything only when the stage changes
// (progress comes with /api/progress).
func setImp(f func(j *ImportJob)) {
	impMu.Lock()
	stage := ""
	changed := false
	if impJob != nil {
		stage = impJob.Stage
		f(impJob)
		changed = impJob.Stage != stage
	}
	impMu.Unlock()
	if changed {
		core.BumpRev()
	}
}

type ImportReq struct {
	Key     string   `json:"key"`
	Paths   []string `json:"paths"` // instead of an asset: folders that were just downloaded
	Project string   `json:"project"`
	Pwd     string   `json:"pwd"`
	Recycle bool     `json:"recycle"`
	// no import: the asset is a collection of products (合集包) — it is unpacked as for an import and split
	// into a card for each product. No project is needed.
	Split bool `json:"split"`
}

// importPlaces: what an asset brings along — its own files, and the downloads of the same product
// that are not for another base body (PSD and texture packs, common parts). A collection of products
// (合集包) is not imported: it brings nothing along, and is refused until it has been split — split: that is
// what it is asked for, and only its own files are wanted.
func importPlaces(st *core.Store, key string, split bool) (places []string, bases []string, err error) {
	st.Mu.RLock()
	views := library.AllViews(st)
	st.Mu.RUnlock()
	var a *library.AssetView
	for i := range views {
		if views[i].Key == key {
			a = &views[i]
		}
	}
	if a == nil {
		return nil, nil, errors.New("未找到该素材")
	}
	if a.Virtual || (a.PanOnly && len(a.Locations) == 0) {
		return nil, nil, errors.New("该素材尚未下载到本地")
	}
	if a.Bundle >= 2 && !split {
		return nil, nil, fmt.Errorf("合集包包含 %d 个素材，请先拆分，再分别导入", a.Bundle)
	}
	add := func(v *library.AssetView) {
		for _, l := range v.Locations {
			if !core.ContainsStr(places, l.Path) {
				places = append(places, l.Path)
			}
		}
	}
	add(a)
	if a.Group != "" && !split {
		for i := range views {
			v := &views[i]
			if v.Key == a.Key || v.Group != a.Group || v.Virtual || v.PanOnly || v.Bundle >= 2 {
				continue
			}
			if v.PSD || len(v.Bases) == 0 || len(a.Bases) == 0 || overlap(v.Bases, a.Bases) {
				add(v)
			}
		}
	}
	return places, a.Bases, nil
}

func overlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}

// projectBases: the base bodies a project is built on (素体 assets it uses).
func projectBases(st *core.Store, project string) []string {
	name := filepath.Base(project)
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var out []string
	for _, a := range st.Assets {
		if a.Category != "素体" {
			continue
		}
		for _, u := range a.Usage {
			if u.Project == name && u.Status == "used" {
				out = append(out, a.Bases...)
			}
		}
	}
	return core.UniqStrings(out)
}

func findPackages(places []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range places {
		_ = filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && strings.HasSuffix(d.Name(), ".extracting") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(fp), ".unitypackage") && !seen[strings.ToLower(fp)] {
				seen[strings.ToLower(fp)] = true
				out = append(out, fp)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// choosePackages decides which packages to import. ask: the player has to pick.
func choosePackages(st *core.Store, pkgs []string, projBases, assetBases []string) (choices []PkgChoice, ask bool) {
	st.Mu.RLock()
	defs := naming.ParseBases(st.Settings.Bases)
	st.Mu.RUnlock()
	want, sure := projBases, len(projBases) > 0
	if !sure {
		want = assetBases
	}
	marked := map[string]bool{}
	for _, p := range pkgs {
		name := filepath.Base(p)
		bs := core.UniqStrings(append(naming.DetectBases(name, defs), naming.ForBases(name, defs)...))
		c := PkgChoice{Path: p, Name: name, Bases: bs}
		if st, err := os.Stat(p); err == nil {
			c.Size = st.Size()
		}
		for _, b := range bs {
			marked[strings.ToLower(b)] = true
		}
		choices = append(choices, c)
	}
	picked := 0
	for i := range choices {
		c := &choices[i]
		switch {
		case len(c.Bases) == 0 || len(marked) <= 1:
			c.Pick = true // common parts, or nothing to choose between
		case overlap(c.Bases, want):
			c.Pick = true
			picked++
		}
	}
	if len(marked) > 1 && (!sure && len(want) != 1 || picked == 0) {
		return choices, true // packages for several base bodies and no way to tell which one
	}
	return choices, false
}

// StartImport begins the import in the background — or, for req.Split, the split of a collection.
func StartImport(st *core.Store, req ImportReq) error {
	label := labelImport
	if req.Split {
		req.Project, label = "", labelSplit // nothing is imported
	} else if !core.IsUnityProject(req.Project) {
		return errors.New("该文件夹不是 Unity 工程（未找到 Assets 和 ProjectSettings 文件夹）")
	}
	places, bases := req.Paths, []string(nil)
	switch {
	case req.Key != "" && len(req.Paths) == 0:
		var err error
		if places, bases, err = importPlaces(st, req.Key, req.Split); err != nil {
			return err
		}
	case req.Key != "": // just downloaded: the card it came from says which base bodies it is for
		st.Mu.RLock()
		for _, v := range library.AllViews(st) {
			if v.Key == req.Key {
				bases = v.Bases
			}
		}
		st.Mu.RUnlock()
	}
	if len(places) == 0 {
		return errors.New("没有可导入的文件")
	}
	impMu.Lock()
	if impJob != nil && impJob.Stage != "done" && impJob.Stage != "failed" {
		impMu.Unlock()
		return errImportBusy
	}
	impJob = &ImportJob{ID: time.Now().UnixNano(), Key: req.Key, Project: req.Project, Split: req.Split, Stage: "unpack", At: time.Now().Unix(),
		places: places, pwd: req.Pwd, recycle: req.Recycle, chosen: make(chan []string, 1)}
	impMu.Unlock()
	core.BumpRev()
	go func() {
		TaskImport.Relabel(label)
		core.RunTask(TaskImport, func() { runImport(st, bases) })
	}()
	return nil
}

// StartImportWhenFree: an import that follows a download; it waits for an import already running.
func StartImportWhenFree(st *core.Store, req ImportReq) {
	go func() {
		for i := 0; i < 1800; i++ {
			err := StartImport(st, req)
			if err == nil {
				return
			}
			if err != errImportBusy {
				core.Logf("下载后导入没能开始：%v", err)
				impMu.Lock()
				impJob = &ImportJob{ID: time.Now().UnixNano(), Key: req.Key, Project: req.Project, Stage: "failed", Err: err.Error(), Msg: err.Error(), At: time.Now().Unix()}
				impMu.Unlock()
				core.BumpRev()
				return
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

var errImportBusy = errors.New("另一个素材正在导入，请等待导入完成后重试")

// ChooseImport: the player's pick when several base bodies were possible (nil = cancel).
func ChooseImport(paths []string) bool {
	impMu.Lock()
	defer impMu.Unlock()
	if impJob == nil || impJob.Stage != "choose" {
		return false
	}
	select {
	case impJob.chosen <- paths:
	default:
	}
	return true
}

// AfterImport: told what was just imported into a project (the pipeline page remembers it; set in ai.go).
var AfterImport = func(project, key string, tops []string) {}

// DismissImport: the player closed the result (or cancelled the choice).
func DismissImport() {
	impMu.Lock()
	if impJob != nil {
		switch impJob.Stage {
		case "choose":
			select {
			case impJob.chosen <- nil:
			default:
			}
		case "done", "failed":
			impJob = nil
		}
	}
	impMu.Unlock()
	core.BumpRev()
}

// bundleTops: the folders that are asked whether they hold a collection once everything is unpacked — the
// places that are folders, and the folders made out of places that were archives (the outermost of them:
// what was unpacked inside a folder is looked at with it).
func bundleTops(places, made []string) []string {
	var tops []string
	for _, p := range places {
		if core.IsDir(p) {
			tops = append(tops, p)
		}
	}
	made = append([]string(nil), made...)
	sort.Slice(made, func(i, k int) bool { return len(made[i]) < len(made[k]) }) // a folder before what is in it
	for _, d := range made {
		inside := false
		for _, t := range tops {
			inside = inside || core.UnderDir(d, t)
		}
		if !inside && core.IsDir(d) {
			tops = append(tops, d)
		}
	}
	return tops
}

func runImport(st *core.Store, assetBases []string) {
	j := ImportSnapshot()
	fail := func(msg string) {
		setImp(func(j *ImportJob) { j.Stage, j.Err, j.Msg = "failed", msg, msg })
		TaskImport.Set(0, 0, msg)
		if j.Split {
			core.Logf("拆分失败：%s", msg)
		} else {
			core.Logf("导入失败：%s", msg)
		}
	}
	// 1. unpack everything, archives inside archives too
	TaskImport.Set(0, 0, "正在解压")
	impMu.Lock()
	places, pwd, recycle, chosen := impJob.places, impJob.pwd, impJob.recycle, impJob.chosen
	impMu.Unlock()
	var remove func([]string) error
	if recycle {
		// the player's own files: to the Recycle Bin, never deleted for good — on a drive without one
		// (a removable or network drive) they stay where they are
		remove = core.RecycleOnly
	}
	res := archive.UnpackAll(places, pwd, remove, func(name string, i, n int) {
		setImp(func(j *ImportJob) { j.Msg, j.Done, j.Total = "正在解压 "+name, i, n })
		TaskImport.Set(i, n, "正在解压 "+name)
	})
	var failed []string
	for a, e := range res.Failed {
		failed = append(failed, filepath.Base(a)+"："+e)
	}
	sort.Strings(failed)
	setImp(func(j *ImportJob) { j.Failed, j.Unpacked, j.Removed = failed, len(res.Done), res.Removed })
	// the places that are left (an archive that was a place itself may be gone now)
	var look []string
	for _, p := range append(places, res.Done...) {
		if _, err := os.Stat(p); err == nil {
			look = append(look, p)
		}
	}
	pwdNeeded := false
	for _, e := range res.Failed {
		pwdNeeded = pwdNeeded || e == archive.ErrArcPwd.Error()
	}
	// A collection of products (合集包) is never imported whole, and what has been unpacked shows one for
	// certain — a rar cannot be looked into before. Its folder levels are marked, the scan makes a card for
	// each product, and those are imported one by one.
	library.KeepUnpacked(st, res.Done, res.From) // (not what the player said is one asset)
	unpacked := library.Unpacked(res.Done, res.From)
	products := 0
	for _, top := range bundleTops(places, res.Done) {
		if b := library.MarkBundles(st, top, unpacked); len(b.Containers) > 0 {
			products += b.Products
		}
	}
	// (the scan is begun before the end is told: the window waits for it before it looks for the new cards)
	if j.Split {
		library.StartPipeline(st, true, true, false, false, nil)
		switch {
		case products >= 2:
			msg := fmt.Sprintf("已拆分为 %d 个素材", products)
			if len(failed) > 0 {
				msg += "；部分压缩包解压失败"
			}
			setImp(func(j *ImportJob) { j.Stage, j.Msg, j.Bundle = "done", msg, products })
			TaskImport.Set(1, 1, "完成："+msg)
		case pwdNeeded && pwd == "":
			fail("压缩包已加密，请在「解压密码」中填写密码后重新拆分")
		case pwdNeeded:
			fail("解压密码错误，请更换密码后重试（密码通常位于商品说明、卖家消息或压缩包旁的文本文件中）")
		default: // it looked like a collection while it was packed, and is none
			msg := "未发现多个素材，未拆分"
			if len(failed) > 0 {
				msg += "（有压缩包解压失败：" + failed[0] + "）"
			}
			setImp(func(j *ImportJob) { j.Stage, j.Msg = "done", msg })
			TaskImport.Set(0, 0, msg)
		}
		return
	}
	if products >= 2 {
		msg := fmt.Sprintf("该素材是合集包（包含 %d 个素材），已拆分为独立的素材卡片，请分别导入", products)
		library.StartPipeline(st, true, true, false, false, nil)
		setImp(func(j *ImportJob) { j.Stage, j.Err, j.Msg, j.Bundle = "failed", msg, msg, products })
		TaskImport.Set(0, 0, msg)
		core.Logf("未导入 %s：%s", j.Key, msg)
		return
	}
	pkgs := findPackages(look)
	if len(pkgs) == 0 {
		msg := "未找到 unitypackage"
		switch {
		case pwdNeeded && pwd == "":
			msg = "压缩包已加密，请在「解压密码」中填写密码后点击「一键导入」"
		case pwdNeeded:
			msg = "解压密码错误，请更换密码后重试（密码通常位于商品说明、卖家消息或压缩包旁的文本文件中）"
		case len(failed) > 0:
			msg += "（有压缩包解压失败：" + failed[0] + "）"
		}
		fail(msg)
		return
	}
	// 2. which packages
	choices, ask := choosePackages(st, pkgs, projectBases(st, j.Project), assetBases)
	var pick []string
	if ask {
		setImp(func(j *ImportJob) { j.Stage, j.Choices, j.Msg = "choose", choices, "选择要导入的 unitypackage" })
		TaskImport.Set(0, 0, "选择要导入的 unitypackage")
		select {
		case pick = <-chosen:
		case <-time.After(30 * time.Minute):
		}
		if len(pick) == 0 {
			setImp(func(j *ImportJob) { j.Stage, j.Msg = "failed", "已取消" })
			TaskImport.Set(0, 0, "已取消")
			return
		}
	} else {
		for _, c := range choices {
			if c.Pick {
				pick = append(pick, c.Path)
			}
		}
		setImp(func(j *ImportJob) { j.Choices = choices })
	}
	// 3. import
	setImp(func(j *ImportJob) { j.Stage, j.Msg = "import", "正在读取工程" })
	TaskImport.Set(0, len(pick), "正在读取工程")
	idx := indexProject(j.Project)
	var files, kept int
	var tops, done, keptPkgs []string
	for i, p := range pick {
		name := filepath.Base(p)
		setImp(func(j *ImportJob) { j.Msg, j.Done, j.Total = "正在导入 "+name, i, len(pick) })
		TaskImport.Set(i, len(pick), "正在导入 "+name)
		stats, err := importUnityPackage(p, idx)
		if err != nil {
			fail(name + "：" + err.Error())
			return
		}
		files += stats.New + stats.Updated
		kept += stats.Kept
		tops = core.UniqStrings(append(tops, stats.Tops...))
		keptPkgs = core.UniqStrings(append(keptPkgs, stats.KeptPkgs...))
		done = append(done, name)
		core.Logf("导入 %s → %s：新 %d，更新 %d，未覆盖 Packages 中的 %d", name, j.Project, stats.New, stats.Updated, stats.Kept)
	}
	sort.Strings(keptPkgs)
	msg := fmt.Sprintf("已导入 %d 个 unitypackage，共 %d 个文件", len(done), files)
	if n := keptNote(kept, keptPkgs); n != "" {
		msg += "；" + n
	}
	setImp(func(j *ImportJob) {
		j.Stage, j.Msg, j.Imported, j.Files, j.Tops, j.Done, j.Total = "done", msg, done, files, tops, len(pick), len(pick)
		j.Kept, j.KeptPkgs = kept, keptPkgs
	})
	TaskImport.Set(1, 1, msg+"，目标工程："+filepath.Base(j.Project))
	AfterImport(j.Project, j.Key, tops) // the pipeline page starts from what was just imported
	// the library sees the unpacked folders, and the project now uses the asset
	library.StartPipeline(st, true, true, false, false, nil)
}
