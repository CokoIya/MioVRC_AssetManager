package library

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/core"
)

// ---------- 整理: identical files, and archives that are unpacked already ----------

// DupFile is one copy of a file that is there more than once.
type DupFile struct {
	Path      string `json:"path"`
	Root      string `json:"root"`
	MTime     int64  `json:"mtime"`
	Asset     string `json:"asset,omitempty"` // the asset it sits in (its key)
	AssetName string `json:"assetName,omitempty"`
	Used      bool   `json:"used,omitempty"`  // … which a project uses
	Noted     bool   `json:"noted,omitempty"` // … which the player wrote notes or tags for
	In        string `json:"in,omitempty"`    // the asset's folder it lies in
	Keep      bool   `json:"keep,omitempty"`  // the copy suggested to stay
}

type DupGroup struct {
	Size  int64     `json:"size"`
	Files []DupFile `json:"files"`
	Cross bool      `json:"cross,omitempty"` // the copies belong to different assets
	Inner bool      `json:"inner,omitempty"` // copies lie in one folder of one asset: the asset may need each of them
}

type DupResult struct {
	Groups   []DupGroup `json:"groups"`
	Files    int        `json:"files"`   // files looked at
	Total    int        `json:"total"`   // groups found (Groups holds the biggest of them)
	Waste    int64      `json:"waste"`   // what would be freed with one copy of each left
	MinSize  int64      `json:"minSize"` // smaller files were not compared
	Warnings []string   `json:"warnings,omitempty"`
	At       int64      `json:"at"`
	Seq      int64      `json:"seq"` // which scan this is (the window keeps a result it has, and confirms against it)
}

type ArcPart struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

// ArcItem is an archive (all its volumes) whose contents are on disk already.
type ArcItem struct {
	Main      string    `json:"main"`
	Parts     []ArcPart `json:"parts"`
	Size      int64     `json:"size"`
	Root      string    `json:"root"`
	Folder    string    `json:"folder"`  // where its contents are
	Checked   int       `json:"checked"` // entries compared with the files there (name and size): all but the junk an archive program adds
	Entries   int       `json:"entries"` // files in the archive
	Asset     string    `json:"asset,omitempty"`
	AssetName string    `json:"assetName,omitempty"`
}

type ArcResult struct {
	Items      []ArcItem `json:"items"`
	Archives   int       `json:"archives"`   // archives looked at
	Unreadable int       `json:"unreadable"` // could not be looked into (rar / 7z without 7-Zip, encrypted): left alone
	Free       int64     `json:"free"`
	Warnings   []string  `json:"warnings,omitempty"`
	At         int64     `json:"at"`
	Seq        int64     `json:"seq"`
}

// TidyRun: a scan under way, or how the last one ended.
type TidyRun struct {
	Running bool   `json:"running"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Msg     string `json:"msg"`
	Err     string `json:"err,omitempty"`

	cancel context.CancelFunc
}

type tidyCache struct {
	Dup   *DupResult `json:"dup,omitempty"`
	DupFP string     `json:"dupFp,omitempty"`
	Arc   *ArcResult `json:"arc,omitempty"`
	ArcFP string     `json:"arcFp,omitempty"`
}

var tidy struct {
	mu    sync.Mutex
	runs  map[string]*TidyRun
	file  string
	cache *tidyCache
	adopt int64 // until then (or the end of the rescan) a change of the library is this tool's own doing
	// … and these results, good when it began, stay
	adoptDup, adoptArc bool
}

const (
	dupListMax    = 500      // groups kept in a result, the biggest
	dupDefaultMin = 1 << 20  // files below this are not compared unless the player asks
	partHash      = 64 << 10 // head, middle and tail of a file, before all of it is read
)

// recycleFiles: the Recycle Bin, and nothing past it (tests put their own in).
var recycleFiles = core.RecycleOnly

func tidyFile() string { return filepath.Join(core.DataDir, "tidy-cache.json") }

// tidyCacheLocked: the results kept for this data folder. Caller holds tidy.mu.
func tidyCacheLocked() *tidyCache {
	if tidy.cache != nil && tidy.file == tidyFile() {
		return tidy.cache
	}
	c := &tidyCache{}
	if b, err := os.ReadFile(tidyFile()); err == nil {
		_ = json.Unmarshal(b, c)
	}
	tidy.cache, tidy.file = c, tidyFile()
	return c
}

func tidySaveLocked() {
	if tidy.cache == nil {
		return
	}
	if b, err := json.Marshal(tidy.cache); err == nil {
		if err := writeFileAtomic(tidy.file, b); err != nil {
			core.Logf("tidy-cache.json 保存失败: %v", err)
		}
	}
}

// libFingerprint: the library as the results saw it — its folders and every asset's size and date.
// Caller holds st.Mu.
func libFingerprint(st *core.Store, dirs []string) string {
	h := sha1.New()
	for _, d := range dirs {
		io.WriteString(h, core.PathKey(d)+"\n")
	}
	for _, a := range st.Assets {
		io.WriteString(h, a.Key+"|"+strconv.FormatInt(a.Size, 36)+"|"+strconv.Itoa(a.Files)+"|"+strconv.FormatInt(a.MTime, 36)+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)[:10])
}

// tidyDirs: where the tools look — the asset folders and the download folder, each once (a folder inside
// another one is walked with it). Caller holds st.Mu.
func tidyDirs(st *core.Store, dlDir string) []string {
	var out []string
	all := append(append([]string{}, st.Settings.Roots...), dlDir)
	for i, d := range all {
		if strings.TrimSpace(d) == "" {
			continue
		}
		inside := false
		for j, o := range all {
			if i != j && strings.TrimSpace(o) != "" && core.UnderDir(d, o) && (core.PathKey(d) != core.PathKey(o) || j < i) {
				inside = true
			}
		}
		if !inside {
			out = append(out, filepath.Clean(d))
		}
	}
	return out
}

// assetPlace: what a file's folder says about it.
type assetPlace struct {
	key, name   string
	used, noted bool
	dirs        []string
}

// assetPlaces: every place of every asset, by lower-case path. Caller holds st.Mu.
func assetPlaces(st *core.Store) map[string]*assetPlace {
	out := map[string]*assetPlace{}
	for _, a := range st.Assets {
		p := &assetPlace{key: a.Key, name: a.Name}
		for _, u := range a.Usage {
			if u.Status == "used" {
				p.used = true
			}
		}
		if u := st.User[a.Key]; u != nil {
			p.noted = strings.TrimSpace(u.Notes) != "" || len(u.Tags) > 0
			if u.Name != "" {
				p.name = u.Name
			}
		}
		for _, l := range a.Locations {
			out[core.PathKey(l.Path)] = p
			if l.Kind == "dir" {
				p.dirs = append(p.dirs, l.Path)
			}
		}
	}
	return out
}

func placeOf(places map[string]*assetPlace, file string) *assetPlace {
	a, _ := placeIn(places, file)
	return a
}

// placeIn: the asset a file belongs to, and the asset's folder it lies in ("" when the file is the place itself).
func placeIn(places map[string]*assetPlace, file string) (*assetPlace, string) {
	own := core.PathKey(file)
	for p, up := own, 0; up < 40; up++ {
		if a := places[p]; a != nil {
			if p == own {
				return a, ""
			}
			return a, p
		}
		q := filepath.Dir(p)
		if q == p {
			break
		}
		p = q
	}
	return nil, ""
}

type tidyInput struct {
	dirs     []string
	projects []string
	places   map[string]*assetPlace
	fp       string
}

func tidyInputOf(st *core.Store, dlDir string) tidyInput {
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	in := tidyInput{dirs: tidyDirs(st, dlDir), places: assetPlaces(st)}
	for _, p := range st.Projects {
		in.projects = append(in.projects, p.Path)
	}
	in.fp = libFingerprint(st, in.dirs)
	return in
}

// TidyStart begins a scan in the background: "dup" (identical files) or "arc" (archives unpacked already).
// dlDir: the download folder, looked through with the asset folders.
func TidyStart(st *core.Store, what, dlDir string, minSize int64) error {
	if what != "dup" && what != "arc" {
		return errors.New("无法识别的扫描类型")
	}
	in := tidyInputOf(st, dlDir)
	if len(in.dirs) == 0 {
		return errors.New("尚未添加素材文件夹")
	}
	if minSize <= 0 {
		minSize = dupDefaultMin
	}
	tidy.mu.Lock()
	if tidy.runs == nil {
		tidy.runs = map[string]*TidyRun{}
	}
	if r := tidy.runs[what]; r != nil && r.Running {
		tidy.mu.Unlock()
		return errors.New("正在扫描")
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &TidyRun{Running: true, Msg: "正在读取文件列表", cancel: cancel}
	tidy.runs[what] = run
	if what == "dup" { // a new result goes by the library it saw, not by an earlier clean-up
		tidy.adoptDup = false
	} else {
		tidy.adoptArc = false
	}
	tidy.mu.Unlock()
	prog := func(done, total int64, msg string) {
		tidy.mu.Lock()
		run.Done, run.Total, run.Msg = done, total, msg
		tidy.mu.Unlock()
	}
	go func() {
		var err error
		var dup *DupResult
		var arc *ArcResult
		func() {
			defer func() {
				if r := recover(); r != nil {
					core.Logf("整理扫描出错: %v", r)
					err = errors.New("扫描出错，详见 library.log")
				}
			}()
			if what == "dup" {
				dup, err = scanDups(ctx, in, minSize, prog)
			} else {
				arc, err = scanArchives(ctx, in, prog)
			}
		}()
		cancel()
		tidy.mu.Lock()
		run.Running = false
		switch {
		case errors.Is(err, context.Canceled):
			run.Msg = "已取消"
		case err != nil:
			run.Err, run.Msg = err.Error(), err.Error()
		default:
			c := tidyCacheLocked()
			if what == "dup" {
				c.Dup, c.DupFP = dup, in.fp
			} else {
				c.Arc, c.ArcFP = arc, in.fp
			}
			tidySaveLocked()
			run.Msg = "扫描完成"
		}
		tidy.mu.Unlock()
		core.BumpRev()
	}()
	return nil
}

// TidyCancel stops a scan at its next file.
func TidyCancel(what string) {
	tidy.mu.Lock()
	if r := tidy.runs[what]; r != nil && r.Running && r.cancel != nil {
		r.cancel()
	}
	tidy.mu.Unlock()
}

// TidyPart: one tool's scan and what it found. Stale: the library has changed since, the result is gone.
type TidyPart struct {
	Run   TidyRun    `json:"run"`
	Dup   *DupResult `json:"dup,omitempty"`
	Arc   *ArcResult `json:"arc,omitempty"`
	Stale bool       `json:"stale,omitempty"`
}

type TidyView struct {
	Dup TidyPart `json:"dup"`
	Arc TidyPart `json:"arc"`
}

// TidyStatus: the scans and their results, as long as the library is what it was when they were made.
func TidyStatus(st *core.Store, dlDir string) TidyView {
	busy := PipelineBusy() // (asked first: when no scan runs, the library read next is the one after it)
	st.Mu.RLock()
	fp := libFingerprint(st, tidyDirs(st, dlDir))
	st.Mu.RUnlock()
	tidy.mu.Lock()
	defer tidy.mu.Unlock()
	c := tidyCacheLocked()
	var v TidyView
	if r := tidy.runs["dup"]; r != nil {
		v.Dup.Run = TidyRun{Running: r.Running, Done: r.Done, Total: r.Total, Msg: r.Msg, Err: r.Err}
	}
	if r := tidy.runs["arc"]; r != nil {
		v.Arc.Run = TidyRun{Running: r.Running, Done: r.Done, Total: r.Total, Msg: r.Msg, Err: r.Err}
	}
	// what this tool moved to the Recycle Bin changes the library too: what is left of its results stays,
	// and once the rescan after it has ended they go by the library as it is then
	if time.Now().Unix() < tidy.adopt {
		if tidy.adoptDup && c.Dup != nil {
			c.DupFP = fp
		}
		if tidy.adoptArc && c.Arc != nil {
			c.ArcFP = fp
		}
		if !busy {
			tidy.adopt = 0
			tidySaveLocked()
		}
	}
	if c.Dup != nil {
		if c.DupFP == fp {
			v.Dup.Dup = c.Dup
		} else {
			v.Dup.Stale = true
		}
	}
	if c.Arc != nil {
		if c.ArcFP == fp {
			v.Arc.Arc = c.Arc
		} else {
			v.Arc.Stale = true
		}
	}
	return v
}

// ---------- walking ----------

type walked struct {
	path, root  string
	size, mtime int64
}

// tidyWalk hands every regular file under dirs to each. Hidden folders, Unity's own, the program's data
// folder and whole Unity projects are passed by; links are not followed; a folder that cannot be read is
// noted and the walk goes on.
func tidyWalk(ctx context.Context, dirs []string, each func(walked), warn func(string)) error {
	data := core.PathKey(core.DataDir)
	seen := map[string]bool{}
	for _, root := range dirs {
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			warn("未找到文件夹：" + root + "（如为移动硬盘，请连接后重新扫描）")
			continue
		}
		if inUnityProject(root) {
			warn("位于 Unity 工程内，未扫描：" + root)
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return filepath.SkipAll
			}
			if err != nil {
				if d != nil && d.IsDir() {
					warn("无法读取：" + p)
				}
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				k := core.PathKey(p)
				if seen[k] {
					return filepath.SkipDir
				}
				seen[k] = true
				if p != root && (strings.HasPrefix(name, ".") || skipDirNames[strings.ToLower(name)]) {
					return filepath.SkipDir
				}
				if (core.DataDir != "" && k == data) || core.IsUnityProject(p) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") || isPartialDownload(name) {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			each(walked{path: p, root: root, size: fi.Size(), mtime: fi.ModTime().Unix()})
			return nil
		})
		if err != nil {
			warn("无法读取：" + root)
		}
		if ctx.Err() != nil {
			return context.Canceled
		}
	}
	return nil
}

// inUnityProject: is dir a Unity project, or somewhere inside one?
func inUnityProject(dir string) bool {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if core.IsUnityProject(d) {
			return true
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}

// warner collects what could not be read: the first few by name, the rest as a count.
type warner struct {
	list []string
	more int
}

func (w *warner) add(s string) {
	if len(w.list) < 8 {
		w.list = append(w.list, s)
	} else {
		w.more++
	}
}

func (w *warner) all() []string {
	out := core.UniqStrings(w.list)
	if w.more > 0 {
		out = append(out, fmt.Sprintf("另有 %d 处无法读取", w.more))
	}
	return out
}

// ---------- identical files ----------

// partialSum: the head, middle and tail of a file (all of a small one). whole: that was the whole file.
func partialSum(ctx context.Context, f walked) (sum string, whole bool, err error) {
	fh, err := os.Open(f.path)
	if err != nil {
		return "", false, err
	}
	defer fh.Close()
	h := sha1.New()
	if f.size <= 3*partHash {
		if _, err := io.Copy(h, fh); err != nil {
			return "", false, err
		}
		return hex.EncodeToString(h.Sum(nil)), true, nil
	}
	buf := make([]byte, partHash)
	for _, off := range []int64{0, f.size/2 - partHash/2, f.size - partHash} {
		if ctx.Err() != nil {
			return "", false, context.Canceled
		}
		if _, err := fh.ReadAt(buf, off); err != nil {
			return "", false, err
		}
		h.Write(buf)
	}
	return hex.EncodeToString(h.Sum(nil)), false, nil
}

func fullSum(ctx context.Context, p string, step func(n int64)) (string, error) {
	fh, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if ctx.Err() != nil {
			return "", context.Canceled
		}
		n, err := fh.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			step(int64(n))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// scanDups finds files with the same content: same size first, then the same head, middle and tail, and
// only what is still alike is read in full.
func scanDups(ctx context.Context, in tidyInput, minSize int64, prog func(done, total int64, msg string)) (*DupResult, error) {
	var w warner
	var files []walked
	n := 0
	err := tidyWalk(ctx, in.dirs, func(f walked) {
		n++
		if n%2000 == 0 {
			prog(int64(n), 0, "正在读取文件列表")
		}
		if f.size >= minSize && !strings.EqualFold(filepath.Ext(f.path), ".meta") {
			files = append(files, f)
		}
	}, w.add)
	if err != nil {
		return nil, err
	}
	bySize := map[int64][]int{}
	for i, f := range files {
		bySize[f.size] = append(bySize[f.size], i)
	}
	var cand []int
	for _, idx := range bySize {
		if len(idx) > 1 {
			cand = append(cand, idx...)
		}
	}
	sort.Ints(cand)
	// 1. the parts
	type pkey struct {
		size int64
		sum  string
	}
	byPart := map[pkey][]int{}
	wholeSum := map[int]bool{}
	for k, i := range cand {
		if k%50 == 0 {
			prog(int64(k), int64(len(cand)), "正在比较文件")
		}
		sum, whole, err := partialSum(ctx, files[i])
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		if err != nil {
			w.add("无法读取：" + files[i].path)
			continue
		}
		byPart[pkey{files[i].size, sum}] = append(byPart[pkey{files[i].size, sum}], i)
		wholeSum[i] = whole
	}
	// 2. all of what is still alike
	var total, done int64
	var todo [][]int
	for _, idx := range byPart {
		if len(idx) < 2 {
			continue
		}
		idx = dropSameFile(files, idx)
		if len(idx) < 2 {
			continue
		}
		todo = append(todo, idx)
		if !wholeSum[idx[0]] {
			total += files[idx[0]].size * int64(len(idx))
		}
	}
	sort.Slice(todo, func(a, b int) bool { return todo[a][0] < todo[b][0] })
	res := &DupResult{Files: n, MinSize: minSize, At: time.Now().Unix(), Seq: time.Now().UnixMilli(), Groups: []DupGroup{}}
	last := time.Time{}
	for _, idx := range todo {
		groups := map[string][]int{"": idx}
		if !wholeSum[idx[0]] {
			groups = map[string][]int{}
			for _, i := range idx {
				sum, err := fullSum(ctx, files[i].path, func(n int64) {
					done += n
					if time.Since(last) > 300*time.Millisecond {
						last = time.Now()
						prog(done, total, "正在核对内容相同的文件")
					}
				})
				if errors.Is(err, context.Canceled) {
					return nil, err
				}
				if err != nil {
					w.add("无法读取：" + files[i].path)
					continue
				}
				groups[sum] = append(groups[sum], i)
			}
		}
		for _, same := range groups {
			if len(same) < 2 {
				continue
			}
			g := DupGroup{Size: files[same[0]].size}
			for _, i := range same {
				f := files[i]
				df := DupFile{Path: f.path, Root: f.root, MTime: f.mtime}
				if a, dir := placeIn(in.places, f.path); a != nil {
					df.Asset, df.AssetName, df.Used, df.Noted, df.In = a.key, a.name, a.used, a.noted, dir
				}
				g.Files = append(g.Files, df)
			}
			suggestKeep(&g)
			res.Groups = append(res.Groups, g)
		}
	}
	sort.SliceStable(res.Groups, func(a, b int) bool {
		wa, wb := res.Groups[a].Size*int64(len(res.Groups[a].Files)-1), res.Groups[b].Size*int64(len(res.Groups[b].Files)-1)
		if wa != wb {
			return wa > wb
		}
		return res.Groups[a].Files[0].Path < res.Groups[b].Files[0].Path
	})
	res.Total = len(res.Groups)
	for _, g := range res.Groups {
		res.Waste += g.Size * int64(len(g.Files)-1)
	}
	if len(res.Groups) > dupListMax {
		res.Groups = res.Groups[:dupListMax]
	}
	res.Warnings = w.all()
	return res, nil
}

// dropSameFile: two names for one file (hard links) are one copy; removing one frees nothing.
func dropSameFile(files []walked, idx []int) []int {
	if len(idx) > 200 {
		return idx
	}
	var infos []os.FileInfo
	var out []int
	for _, i := range idx {
		fi, err := os.Stat(files[i].path)
		if err != nil {
			continue
		}
		dup := false
		for _, o := range infos {
			if os.SameFile(o, fi) {
				dup = true
				break
			}
		}
		if !dup {
			infos = append(infos, fi)
			out = append(out, i)
		}
	}
	return out
}

// suggestKeep marks the one copy that should stay: in an asset a project uses, else in one with the
// player's notes, else in any asset; among equals the oldest, then the shortest path. It also says whether
// the copies belong to different assets, or lie together in one folder of an asset.
func suggestKeep(g *DupGroup) {
	rank := func(f DupFile) int {
		switch {
		case f.Used:
			return 3
		case f.Noted:
			return 2
		case f.Asset != "":
			return 1
		}
		return 0
	}
	best := 0
	assets, dirs := map[string]bool{}, map[string]bool{}
	g.Inner = false
	for i, f := range g.Files {
		g.Files[i].Keep = false
		if f.Asset != "" {
			assets[f.Asset] = true
		}
		if f.In != "" {
			g.Inner = g.Inner || dirs[f.In]
			dirs[f.In] = true
		}
		b := g.Files[best]
		switch ri, rb := rank(f), rank(b); {
		case ri != rb:
			if ri > rb {
				best = i
			}
		case f.MTime != b.MTime:
			if f.MTime < b.MTime {
				best = i
			}
		case len(f.Path) != len(b.Path):
			if len(f.Path) < len(b.Path) {
				best = i
			}
		case f.Path < b.Path:
			best = i
		}
	}
	g.Files[best].Keep = true
	g.Cross = len(assets) > 1
}

// ---------- archives that are unpacked already ----------

func skipEntry(name string) bool {
	l := strings.ToLower(name)
	return strings.HasPrefix(l, "__macosx/") || strings.HasSuffix(l, ".ds_store") || strings.HasSuffix(l, "thumbs.db") || strings.HasSuffix(l, "desktop.ini")
}

// unpackedAt finds where an archive's contents are on disk: next to it (the folder it unpacks to, or loose
// beside it) or in one of the asset's folders. Going by the name is not enough: every one of its entries must
// be there under the same name and with the same size (one Lstat each). checked: how many were compared.
// only: the one folder to look in ("" = any of them) — when the answer of an earlier look is asked again.
func unpackedAt(s archive.Set, entries []archive.Entry, assetDirs []string, only string) (folder string, checked int) {
	var files []archive.Entry
	tops := map[string]bool{}
	nested := true
	var bytes int64
	for _, e := range entries {
		rel := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(e.Name, "\\", "/")), "/")
		if rel == "" || skipEntry(rel) {
			continue
		}
		segs := strings.Split(rel, "/")
		tops[segs[0]] = true
		if len(segs) == 1 {
			nested = false
		}
		files = append(files, archive.Entry{Name: rel, Size: e.Size})
		bytes += e.Size
	}
	if len(files) == 0 || bytes == 0 {
		return "", 0 // nothing, or empty files only: nothing to tell by
	}
	strip := ""
	if len(tops) == 1 && nested {
		for t := range tops {
			strip = t + "/"
		}
	}
	parent := filepath.Dir(s.Main)
	type cand struct{ dir, strip string }
	var cands []cand
	if strip != "" {
		cands = append(cands, cand{filepath.Join(parent, core.SafeName(strings.TrimSuffix(strip, "/"), 120)), strip})
		cands = append(cands, cand{filepath.Join(parent, core.SafeName(s.Name, 120)), strip})
	}
	cands = append(cands, cand{filepath.Join(parent, core.SafeName(s.Name, 120)), ""}, cand{parent, ""})
	for _, d := range assetDirs {
		cands = append(cands, cand{d, ""})
		if strip != "" {
			cands = append(cands, cand{d, strip})
		}
	}
	at := func(dir, rel string, size int64) bool {
		segs := strings.Split(rel, "/")
		safe := make([]string, len(segs))
		for i := range segs {
			safe[i] = core.SafeName(segs[i], 200)
		}
		for _, names := range [][]string{safe, segs} {
			if fi, err := os.Lstat(filepath.Join(append([]string{dir}, names...)...)); err == nil && fi.Mode().IsRegular() && fi.Size() == size {
				return true
			}
		}
		return false
	}
	for _, c := range cands {
		if only != "" && core.PathKey(c.dir) != core.PathKey(only) {
			continue
		}
		if fi, err := os.Stat(c.dir); err != nil || !fi.IsDir() {
			continue
		}
		ok := true
		for _, e := range files {
			if !at(c.dir, strings.TrimPrefix(e.Name, c.strip), e.Size) {
				ok = false
				break
			}
		}
		if ok {
			return c.dir, len(files)
		}
	}
	return "", 0
}

func scanArchives(ctx context.Context, in tidyInput, prog func(done, total int64, msg string)) (*ArcResult, error) {
	var w warner
	var files []string
	info := map[string]walked{}
	n := 0
	err := tidyWalk(ctx, in.dirs, func(f walked) {
		n++
		if n%2000 == 0 {
			prog(int64(n), 0, "正在读取文件列表")
		}
		if archive.IsArchiveFile(f.path) {
			files = append(files, f.path)
			info[f.path] = f
		}
	}, w.add)
	if err != nil {
		return nil, err
	}
	sets := archive.Sets(files)
	res := &ArcResult{Archives: len(sets), At: time.Now().Unix(), Seq: time.Now().UnixMilli(), Items: []ArcItem{}}
	now := time.Now().Unix()
	for i, s := range sets {
		if ctx.Err() != nil {
			return nil, context.Canceled
		}
		prog(int64(i), int64(len(sets)), "正在核对 "+filepath.Base(s.Main))
		if now-info[s.Main].mtime < 60 {
			continue // just written: a download or an unpack may still be at it
		}
		entries, err := archive.ListEntries(s)
		if err != nil {
			res.Unreadable++
			continue
		}
		a := placeOf(in.places, s.Main)
		var dirs []string
		if a != nil {
			dirs = a.dirs
		}
		folder, checked := unpackedAt(s, entries, dirs, "")
		if folder == "" {
			continue
		}
		it := ArcItem{Main: s.Main, Size: s.Size, Root: info[s.Main].root, Folder: folder, Checked: checked, Entries: len(entries)}
		for _, p := range s.Parts {
			it.Parts = append(it.Parts, ArcPart{Path: p, Size: info[p].size, MTime: info[p].mtime})
		}
		if a != nil {
			it.Asset, it.AssetName = a.key, a.name
		}
		res.Items = append(res.Items, it)
		res.Free += s.Size
	}
	sort.SliceStable(res.Items, func(a, b int) bool { return res.Items[a].Size > res.Items[b].Size })
	res.Warnings = w.all()
	return res, nil
}

// ---------- to the Recycle Bin ----------

// CheckRemovable: may this file or folder go to the Recycle Bin? Only what lies inside one of the allowed
// folders (the asset folders, the download folder) and is none of them; nothing that holds an asset folder,
// a Unity project or a path in keep; nothing inside a Unity project; no link.
func CheckRemovable(p string, allowed, projects, keep []string) error {
	p = filepath.Clean(p)
	fi, err := os.Lstat(p)
	if err != nil {
		return errors.New("文件已不存在")
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("这是一个链接，未移动")
	}
	inside := false
	for _, r := range allowed {
		if r != "" && core.UnderDir(p, r) && core.PathKey(p) != core.PathKey(r) {
			inside = true
		}
	}
	if !inside {
		return errors.New("不在素材文件夹或下载文件夹内，未移动")
	}
	for _, list := range [][]string{allowed, projects, keep} {
		for _, r := range list {
			if r != "" && core.UnderDir(r, p) {
				return errors.New("其中包含素材文件夹、Unity 工程或刚下载的文件，未移动")
			}
		}
	}
	for _, r := range projects {
		if r != "" && core.UnderDir(p, r) && core.IsUnityProject(r) {
			return errors.New("位于 Unity 工程内，未移动")
		}
	}
	if fi.IsDir() && core.IsUnityProject(p) {
		return errors.New("这是一个 Unity 工程，未移动")
	}
	if inUnityProject(filepath.Dir(p)) { // (also when an asset folder itself lies inside a project)
		return errors.New("位于 Unity 工程内，未移动")
	}
	return nil
}

var errTidyMoved = errors.New("扫描结果已更新，请重新勾选并确认")

// TidyFail: a file that stayed where it is, and why.
type TidyFail struct {
	Path string `json:"path"`
	Err  string `json:"err"`
}

type TidyRecycled struct {
	Removed int        `json:"removed"`
	Freed   int64      `json:"freed"`
	Failed  []TidyFail `json:"failed,omitempty"`
}

// tidyRecycleMu: one clean-up at a time. A second request reads the result the first one has left, so two of
// them cannot each take a different copy of a group and leave none.
var tidyRecycleMu sync.Mutex

const (
	errKeptChanged  = "要保留的那一份在扫描后已有变化或已不存在，为避免移走仅剩的一份，未移动"
	errUnpackedGone = "已解压的文件在扫描后已有变化或已不存在，为避免移走仅剩的一份，未移动"
	errDupDiffers   = "该文件与要保留的那一份在扫描后已不再相同，未移动"
)

// sameFileNow: is p still the regular file the scan saw (size and date)?
func sameFileNow(p string, size, mtime int64) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Size() == size && fi.ModTime().Unix() == mtime
}

// keptCopy: a copy of the group that is not picked and is still the file the scan saw. The result goes by
// the library's state, which only moves with a rescan — the copy that was to stay may have been edited or
// deleted since. sum: its head, middle and tail as they are now, which a picked copy has to match.
func keptCopy(g DupGroup, picked map[string]bool) (sum string, ok bool) {
	for _, f := range g.Files {
		if picked[f.Path] || !sameFileNow(f.Path, g.Size, f.MTime) {
			continue
		}
		if sum, _, err := partialSum(context.Background(), walked{path: f.Path, size: g.Size}); err == nil {
			return sum, true
		}
	}
	return "", false
}

// stillUnpacked: are the archive's contents still where the scan found them? Asked again in full right before
// the archive goes: the folder may have been deleted, or files in it changed, since the scan.
func stillUnpacked(it ArcItem) bool {
	var files []string
	for _, p := range it.Parts {
		files = append(files, p.Path)
	}
	sets := archive.Sets(files)
	if it.Folder == "" || len(sets) != 1 || sets[0].Main != it.Main || len(sets[0].Parts) != len(it.Parts) {
		return false
	}
	entries, err := archive.ListEntries(sets[0])
	if err != nil {
		return false
	}
	folder, _ := unpackedAt(sets[0], entries, []string{it.Folder}, it.Folder)
	return folder != ""
}

// TidyRecycle moves what the player picked from a scan's result to the Recycle Bin: copies of identical
// files (never all of a group), or archives with all their volumes. Only what the result lists is taken,
// and only while it is the file the scan saw (same size and date) and what makes it spare is still there:
// an unpicked copy of the same content, or the archive's unpacked files. seq: the scan whose result the
// player was shown and confirmed; another one is not acted on unseen.
func TidyRecycle(st *core.Store, what string, picks []string, dlDir string, seq int64) (TidyRecycled, error) {
	tidyRecycleMu.Lock()
	defer tidyRecycleMu.Unlock()
	var out TidyRecycled
	view := TidyStatus(st, dlDir)
	in := tidyInputOf(st, dlDir)
	st.Mu.RLock()
	projects := append(append([]string{}, in.projects...), st.Settings.ProjectRoots...)
	st.Mu.RUnlock()
	picked := map[string]bool{}
	for _, p := range picks {
		picked[p] = true
	}
	var todo []ArcPart
	want := map[string]string{} // a picked copy → the head, middle and tail of the copy that stays
	switch what {
	case "dup":
		if view.Dup.Dup == nil {
			return out, errors.New("扫描结果已失效，请重新扫描")
		}
		if view.Dup.Dup.Seq != seq {
			return out, errTidyMoved
		}
		for _, g := range view.Dup.Dup.Groups {
			n := 0
			for _, f := range g.Files {
				if picked[f.Path] {
					n++
				}
			}
			if n == 0 {
				continue
			}
			if n >= len(g.Files) {
				return out, errors.New("每组相同的文件至少需保留一份")
			}
			sum, kept := keptCopy(g, picked)
			for _, f := range g.Files {
				if !picked[f.Path] {
					continue
				}
				if !kept {
					out.Failed = append(out.Failed, TidyFail{f.Path, errKeptChanged})
					continue
				}
				want[f.Path] = sum
				todo = append(todo, ArcPart{Path: f.Path, Size: g.Size, MTime: f.MTime})
			}
		}
	case "arc":
		if view.Arc.Arc == nil {
			return out, errors.New("扫描结果已失效，请重新扫描")
		}
		if view.Arc.Arc.Seq != seq {
			return out, errTidyMoved
		}
		for _, it := range view.Arc.Arc.Items {
			if !picked[it.Main] {
				continue
			}
			// an archive goes whole or not at all: every volume must be the file the scan saw, and what was
			// unpacked from them must still be there
			why := ""
			for _, f := range it.Parts {
				if err := CheckRemovable(f.Path, in.dirs, projects, nil); err != nil {
					why = err.Error()
				} else if !sameFileNow(f.Path, f.Size, f.MTime) {
					why = "文件在扫描后已有变化，未移动"
				}
			}
			if why == "" && !stillUnpacked(it) {
				why = errUnpackedGone
			}
			if why != "" {
				out.Failed = append(out.Failed, TidyFail{it.Main, why})
				continue
			}
			todo = append(todo, it.Parts...)
		}
	default:
		return out, errors.New("无法识别的操作")
	}
	if len(todo) == 0 {
		if len(out.Failed) > 0 {
			return out, nil // (picked from the result, and none of it may go: said file by file)
		}
		return out, errors.New("所选文件不在扫描结果中，请重新扫描")
	}
	var ok []ArcPart
	for _, f := range todo {
		err := CheckRemovable(f.Path, in.dirs, projects, nil)
		if err == nil && !sameFileNow(f.Path, f.Size, f.MTime) {
			err = errors.New("文件在扫描后已有变化，未移动")
		}
		if sum, cmp := want[f.Path]; err == nil && cmp {
			if now, _, e := partialSum(context.Background(), walked{path: f.Path, size: f.Size}); e != nil || now != sum {
				err = errors.New(errDupDiffers) // (one of the two was changed under its size and date)
			}
		}
		if err != nil {
			out.Failed = append(out.Failed, TidyFail{f.Path, err.Error()})
			continue
		}
		ok = append(ok, f)
	}
	gone := map[string]bool{}
	for i := 0; i < len(ok); i += 30 {
		batch := ok[i:min(i+30, len(ok))]
		paths := make([]string, len(batch))
		for k, f := range batch {
			paths[k] = f.Path
		}
		why := map[string]error{}
		if err := recycleFiles(paths); err != nil { // one of them is held open, say: the others still go
			for _, p := range paths {
				if _, e := os.Lstat(p); e == nil {
					why[p] = recycleFiles([]string{p})
				}
			}
		}
		for _, f := range batch {
			if _, e := os.Lstat(f.Path); e == nil {
				msg := "无法移至回收站（文件可能正被占用，或路径过长）"
				if errors.Is(why[f.Path], core.ErrNoRecycleBin) {
					msg = "所在磁盘没有回收站（移动硬盘、U 盘或网络位置），为避免永久删除，未移动"
				}
				out.Failed = append(out.Failed, TidyFail{f.Path, msg})
				continue
			}
			gone[f.Path] = true
			out.Removed++
			out.Freed += f.Size
			core.Logf("整理：已移至回收站 %s", f.Path)
		}
	}
	if len(gone) == 0 {
		return out, nil
	}
	StartPipeline(st, true, true, false, false, nil) // (begun before the results are kept: no moment in between)
	// the result without what has gone
	tidy.mu.Lock()
	c := tidyCacheLocked()
	if what == "dup" && c.Dup != nil {
		kept := c.Dup.Groups[:0]
		for _, g := range c.Dup.Groups {
			var fs []DupFile
			for _, f := range g.Files {
				if !gone[f.Path] {
					fs = append(fs, f)
				}
			}
			if len(fs) != len(g.Files) {
				c.Dup.Waste -= g.Size * int64(len(g.Files)-len(fs))
			}
			if len(fs) < 2 {
				if len(fs) != len(g.Files) {
					c.Dup.Total--
				}
				continue
			}
			g.Files = fs
			suggestKeep(&g)
			kept = append(kept, g)
		}
		c.Dup.Groups = kept
	}
	if what == "arc" && c.Arc != nil {
		kept := c.Arc.Items[:0]
		for _, it := range c.Arc.Items {
			left := false
			for _, p := range it.Parts {
				left = left || !gone[p.Path]
			}
			if left && gone[it.Main] {
				left = false // its first volume is gone: no archive any more
			}
			if left {
				kept = append(kept, it)
			} else {
				c.Arc.Free -= it.Size
			}
		}
		c.Arc.Items = kept
	}
	tidy.adopt = time.Now().Unix() + 600
	tidy.adoptDup, tidy.adoptArc = view.Dup.Dup != nil, view.Arc.Arc != nil
	tidySaveLocked()
	tidy.mu.Unlock()
	core.BumpRev()
	return out, nil
}
