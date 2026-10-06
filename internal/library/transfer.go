package library

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/update"
)

// ---------- 迁移: the library in one zip, and that zip taken in on another computer ----------

const (
	exportFormat = 1 // the layout of the zip
	storeSchema  = 1 // core.Store.Version as this program writes it
	kindExport   = "library-export"
	kindBackup   = "import-backup"

	maxManifest  = 1 << 20
	maxLibrary   = 256 << 20
	maxDataFile  = 64 << 20
	maxDataTotal = 4 << 30
	maxEntries   = 100000
)

// ExportManifest: what a zip is, read before anything else of it is trusted.
type ExportManifest struct {
	App          string   `json:"app"`
	Kind         string   `json:"kind"`
	Format       int      `json:"format"`
	Version      string   `json:"version"` // the program that wrote it
	Exported     int64    `json:"exported"`
	Assets       int      `json:"assets"`
	Roots        []string `json:"roots"`
	ProjectRoots []string `json:"projectRoots"`
	DataDir      string   `json:"dataDir"`
	Files        int      `json:"files"` // data files next to library.json
}

// ---------- paths of another computer ----------

// splitPath: the parts of a path written with either separator ("D:\A\b" → D:, A, b; "/a/b" → "", a, b).
func splitPath(p string) []string {
	p = path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if p == "." {
		return nil
	}
	if p == "/" {
		return []string{""}
	}
	return strings.Split(p, "/")
}

// underRoot: the parts of p below root, when p is root or inside it. Names compare without regard to case,
// as Windows does (the drive letter too).
func underRoot(p, root string) ([]string, bool) {
	ps, rs := splitPath(p), splitPath(root)
	if len(rs) == 0 || len(ps) < len(rs) {
		return nil, false
	}
	for i := range rs {
		if !strings.EqualFold(ps[i], rs[i]) {
			return nil, false
		}
	}
	return ps[len(rs):], true
}

// sepOf: the separator a path is written with (a drive letter or a backslash: Windows).
func sepOf(p string) string {
	if strings.Contains(p, `\`) || (len(p) >= 2 && p[1] == ':') {
		return `\`
	}
	return "/"
}

func baseOf(p string) string {
	s := splitPath(p)
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func joinUnder(root string, rest []string) string {
	sep := sepOf(root)
	out := strings.TrimRight(root, `/\`)
	if len(rest) == 0 {
		if out == "" || strings.HasSuffix(out, ":") {
			return out + sep // "/" or "D:\"
		}
		return out
	}
	return out + sep + strings.Join(rest, sep)
}

// pathMap: folders of the other computer → folders of this one; the longest one a path is under counts.
type pathMap [][2]string

func newPathMap(m map[string]string) pathMap {
	var out pathMap
	for old, to := range m {
		if strings.TrimSpace(old) != "" && strings.TrimSpace(to) != "" {
			out = append(out, [2]string{old, to})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := len(splitPath(out[i][0])), len(splitPath(out[j][0])); a != b {
			return a > b
		}
		return out[i][0] < out[j][0]
	})
	return out
}

func (m pathMap) apply(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	for _, pair := range m {
		if rest, ok := underRoot(p, pair[0]); ok {
			return joinUnder(pair[1], rest), true
		}
	}
	return p, false
}

// rootOf: which of the roots p is under (the longest), "" when none.
func rootOf(p string, roots []string) string {
	best, n := "", -1
	for _, r := range roots {
		if _, ok := underRoot(p, r); ok && len(splitPath(r)) > n {
			best, n = r, len(splitPath(r))
		}
	}
	return best
}

// rootName: what the scan calls an asset folder in a key — its last name, as filepath.Base gives it on the
// computer the path is from: a whole drive or share is "\" there, the top of a Unix disk "/".
func rootName(root string) string {
	s := splitPath(root)
	switch {
	case len(s) == 0:
		return "."
	case len(s) == 1 && s[0] == "":
		return "/"
	case len(s) == 1 && strings.HasSuffix(s[0], ":"), len(s) == 3 && s[0] == "" && strings.HasPrefix(root, `\\`):
		return `\`
	}
	return s[len(s)-1]
}

// pathKeyOf: the key the scan gives an asset that goes by where it is ("path:<folder>/<path below it>").
func pathKeyOf(root, p string) string {
	rest, ok := underRoot(p, root)
	if !ok {
		return ""
	}
	return "path:" + rootName(root) + "/" + strings.ToLower(strings.Join(rest, "/"))
}

// ---------- which files of the data folder travel ----------

var (
	// never: logs, caches, logins and keys (encrypted for one Windows account), what a crash or an update left —
	// and what belongs to one computer, because it says where requests go or where files are on it:
	//   ai.json        the AI services' addresses. The key saved for them (ai-key.dat) stays behind, so an
	//                  imported address would be sent this computer's key
	//   checkups.json  check-ups by the project's path on this computer
	//   pkgcovers.json the unitypackage each preview came from, by its path (looked at again on every scan);
	//                  the previews (covers/unitypackage) are read from the packages anew
	dataSkipFile = map[string]bool{"library.json": true, "library.json.bak": true, "guidcache.json": true, "tidy-cache.json": true,
		"transfer.json": true, "manifest.json": true, "ai.json": true, "checkups.json": true, "pkgcovers.json": true}
	dataSkipDir = map[string]bool{"booth-profile": true, "booth-profile-firefox": true, "web-login": true, "web-xianyu": true, "webview": true, "webview2": true,
		"ebwebview": true, "update": true, "vpm": true, "shots": true, "downloads": true, "covers/thumbs": true, "covers/remote": true,
		"covers/unitypackage": true}
	dataExt = map[string]bool{".json": true, ".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".txt": true, ".md": true, ".csv": true}
	// a short (8.3) name Windows keeps next to a long one: "WEB-LO~1" is web-login, "BOOTH-~1" booth-profile.
	// No name with "~" and a digit is taken, so no folder or file that is left out can be reached under its other name
	reShortName = regexp.MustCompile(`~[0-9]`)
)

// uncPath: a path on another computer ("\\host\share", "//host/share"). Looking at one makes Windows log in
// to that host, so a path of this kind that came out of a zip is never looked at.
func uncPath(p string) bool {
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")
}

// dataFileOK: does a file of the data folder (rel: its path there, "/" between folders) belong in an export?
// The same answer decides what an import may write, so a zip cannot put anything else into the folder.
func dataFileOK(rel string) bool {
	segs := strings.Split(rel, "/")
	if rel == "" || len(segs) > 8 {
		return false
	}
	for i, s := range segs {
		if s == "" || s == "." || s == ".." || strings.HasPrefix(s, ".") || s != core.SafeName(s, 200) || reShortName.MatchString(s) {
			return false
		}
		if i < len(segs)-1 && dataSkipDir[strings.ToLower(strings.Join(segs[:i+1], "/"))] {
			return false
		}
	}
	name := strings.ToLower(segs[len(segs)-1])
	if len(segs) == 1 && (dataSkipFile[name] || strings.HasPrefix(name, kindBackup)) {
		return false
	}
	if strings.Contains(name, ".broken-") || strings.HasSuffix(name, ".tmp") {
		return false
	}
	ext := filepath.Ext(name)
	if len(segs) == 1 {
		return ext == ".json" // next to library.json: the program's own data files (not the exe of a portable copy, not a login)
	}
	return dataExt[ext]
}

// dataFiles: the files of the data folder that travel, as paths below it. avoid: folders that are not
// data (a portable copy may have its asset folders, downloads or projects next to the exe).
func dataFiles(avoid []string) []string {
	var out []string
	root := core.DataDir
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || dataSkipDir[strings.ToLower(rel)] || core.IsUnityProject(p) || strings.Count(rel, "/") >= 7 {
				return filepath.SkipDir
			}
			for _, a := range avoid { // an asset folder, a project or the downloads kept inside the data folder
				if a != "" && core.UnderDir(a, root) && (core.UnderDir(p, a) || core.UnderDir(a, p)) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !d.Type().IsRegular() || !dataFileOK(rel) || len(out) >= maxEntries {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.Size() <= maxDataFile {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func avoidDirs(st *core.Store, dlDir string) []string {
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	out := append(append([]string{dlDir}, st.Settings.Roots...), st.Settings.ProjectRoots...)
	for _, p := range st.Projects {
		out = append(out, p.Path)
	}
	return out
}

// ---------- export ----------

// TransferRun: an export or an import under way, and how the last one ended.
type TransferRun struct {
	Running bool   `json:"running"`
	What    string `json:"what"` // export, import, undo
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Msg     string `json:"msg"`
	Err     string `json:"err,omitempty"`
	Path    string `json:"path,omitempty"` // the zip that was written
	Size    int64  `json:"size,omitempty"`
	Assets  int    `json:"assets,omitempty"`
	Files   int    `json:"files,omitempty"`
}

var transfer struct {
	mu  sync.Mutex
	run TransferRun
}

func transferBegin(what, msg string) bool {
	transfer.mu.Lock()
	defer transfer.mu.Unlock()
	if transfer.run.Running {
		return false
	}
	transfer.run = TransferRun{Running: true, What: what, Msg: msg}
	return true
}

func transferSet(f func(r *TransferRun)) {
	transfer.mu.Lock()
	f(&transfer.run)
	transfer.mu.Unlock()
}

func TransferStatus() TransferRun {
	transfer.mu.Lock()
	defer transfer.mu.Unlock()
	return transfer.run
}

// writeZip writes the library as it is now, the manifest and the given data files into a zip at dst (through
// a temporary file, so a zip that is there is a whole one).
func writeZip(st *core.Store, dst, kind string, files []string, extra map[string][]byte, prog func(i, n int)) (ExportManifest, int64, error) {
	st.Mu.RLock()
	// the library as it leaves this computer: without what is this computer's own. The proxy can carry a
	// password, and none of the three is taken in by an import
	out := struct {
		*core.Store
		Settings  core.Settings `json:"settings"`
		AutoDLDir string        `json:"autoDlDir,omitempty"`
	}{Store: st, Settings: st.Settings}
	out.Settings.Proxy, out.Settings.DownloadDir = "", ""
	lib, err := json.MarshalIndent(&out, "", " ")
	m := ExportManifest{App: core.AppName, Kind: kind, Format: exportFormat, Version: core.AppVersion, Exported: time.Now().Unix(),
		Assets: len(st.Assets), Roots: append([]string{}, st.Settings.Roots...), ProjectRoots: append([]string{}, st.Settings.ProjectRoots...),
		DataDir: core.DataDir, Files: len(files)}
	st.Mu.RUnlock()
	if err != nil {
		return m, 0, err
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return m, 0, err
	}
	fail := func(err error) (ExportManifest, int64, error) {
		f.Close()
		_ = os.Remove(tmp)
		return m, 0, err
	}
	zw := zip.NewWriter(f)
	put := func(name string, b []byte) error {
		w, err := zw.Create(name)
		if err == nil {
			_, err = w.Write(b)
		}
		return err
	}
	mb, _ := json.MarshalIndent(m, "", " ")
	if err := put("manifest.json", mb); err != nil {
		return fail(err)
	}
	if err := put("library.json", lib); err != nil {
		return fail(err)
	}
	for name, b := range extra {
		if err := put(name, b); err != nil {
			return fail(err)
		}
	}
	for i, rel := range files {
		if prog != nil {
			prog(i, len(files))
		}
		src, err := os.Open(filepath.Join(core.DataDir, filepath.FromSlash(rel)))
		if err != nil {
			continue // gone since it was listed
		}
		method := zip.Deflate
		if ext := strings.ToLower(filepath.Ext(rel)); ext != ".json" && dataExt[ext] && ext != ".txt" && ext != ".md" && ext != ".csv" {
			method = zip.Store // pictures are packed already
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "data/" + rel, Method: method, Modified: time.Now()})
		if err == nil {
			_, err = io.Copy(w, src)
		}
		src.Close()
		if err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	fi, _ := f.Stat()
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return m, 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return m, 0, err
	}
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	return m, size, nil
}

// StartExport writes the library and its data files into one zip in dir, in the background.
func StartExport(st *core.Store, dir, dlDir string) error {
	dir = filepath.Clean(strings.Trim(strings.TrimSpace(dir), `"`))
	if !core.IsDir(dir) {
		return errors.New("文件夹不存在：" + dir)
	}
	if !core.DirWritable(dir) {
		return errors.New("无法写入该文件夹：" + dir)
	}
	if !transferBegin("export", "正在整理数据文件") {
		return errors.New("另一项导出或导入正在进行")
	}
	go func() {
		defer core.BumpRev()
		dst := filepath.Join(dir, "MioVRCA-library-"+time.Now().Format("20060102-150405")+".zip")
		files := dataFiles(avoidDirs(st, dlDir))
		m, size, err := writeZip(st, dst, kindExport, files, nil, func(i, n int) {
			transferSet(func(r *TransferRun) { r.Done, r.Total, r.Msg = i, n, "正在写入导出文件" })
		})
		transferSet(func(r *TransferRun) {
			r.Running = false
			if err != nil {
				core.Logf("导出素材库失败: %v", err)
				r.Err, r.Msg = "导出失败："+core.TrimErr(err), "导出失败"
				return
			}
			r.Msg, r.Path, r.Size, r.Assets, r.Files, r.Done, r.Total = "导出完成", dst, size, m.Assets, m.Files, len(files), len(files)
			core.Logf("已导出素材库 → %s（%d 个素材，%d 个数据文件）", dst, m.Assets, m.Files)
		})
	}()
	return nil
}

// ---------- reading a zip that came from somewhere else ----------

type exportZip struct {
	zr       *zip.ReadCloser
	manifest ExportManifest
	store    *core.Store
	data     []*zip.File // the data files it may write, checked
	skipped  int         // entries that are not taken
	size     int64
}

func (z *exportZip) Close() { z.zr.Close() }

func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if int64(f.UncompressedSize64) > limit {
		return nil, errors.New("文件过大")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1)) // what the header says is not taken on trust
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("文件过大")
	}
	return b, nil
}

var errNotExport = errors.New("这不是 MioVRCA 导出的素材库文件")

// openExport opens a zip and checks it before anything is done with it: that it is one of this program's,
// not from a newer version, and that every file it would write is a data file with a safe name and size.
func openExport(file string) (*exportZip, error) {
	file = filepath.Clean(strings.Trim(strings.TrimSpace(file), `"`))
	fi, err := os.Stat(file)
	if err != nil || fi.IsDir() {
		return nil, errors.New("文件不存在：" + file)
	}
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, errNotExport
	}
	z := &exportZip{zr: zr, size: fi.Size()}
	fail := func(err error) (*exportZip, error) {
		zr.Close()
		return nil, err
	}
	if len(zr.File) > maxEntries {
		return fail(errors.New("导出文件内的文件数量超出限制，已拒绝导入"))
	}
	var mf, lf *zip.File
	var total int64
	for _, f := range zr.File {
		switch {
		case f.Name == "manifest.json":
			mf = f
		case f.Name == "library.json":
			lf = f
		case f.Name == "pending.json":
			// (a backup's own: read by the undo)
		case strings.HasPrefix(f.Name, "data/") && f.Mode().IsRegular() && dataFileOK(strings.TrimPrefix(f.Name, "data/")):
			if f.UncompressedSize64 > maxDataFile {
				return fail(errors.New("导出文件内有超出大小限制的文件（" + f.Name + "），已拒绝导入"))
			}
			total += int64(f.UncompressedSize64)
			z.data = append(z.data, f)
		default:
			if !strings.HasSuffix(f.Name, "/") {
				z.skipped++ // not a data file, an unsafe name ("..", a drive, a link …): never written
			}
		}
	}
	if total > maxDataTotal {
		return fail(errors.New("导出文件解压后的大小超出限制，已拒绝导入"))
	}
	if mf == nil || lf == nil {
		return fail(errNotExport)
	}
	b, err := readEntry(mf, maxManifest)
	if err != nil || json.Unmarshal(b, &z.manifest) != nil || z.manifest.App != core.AppName || (z.manifest.Kind != kindExport && z.manifest.Kind != kindBackup) {
		return fail(errNotExport)
	}
	newer := func(v string) (*exportZip, error) {
		return fail(fmt.Errorf("该文件由更新版本的 MioVRCA（%s）导出，当前版本为 %s。请先将本机的软件更新到相同或更新的版本，再导入", v, core.AppVersion))
	}
	if z.manifest.Format > exportFormat || update.VersionNewer(z.manifest.Version, core.AppVersion) {
		return newer(z.manifest.Version)
	}
	b, err = readEntry(lf, maxLibrary)
	if err != nil {
		return fail(errors.New("导出文件内的素材库数据无法读取（" + err.Error() + "）"))
	}
	var head struct {
		Version int `json:"version"`
	}
	if !bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) || json.Unmarshal(b, &head) != nil {
		return fail(errors.New("导出文件内的素材库数据已损坏，无法导入"))
	}
	if head.Version > storeSchema {
		return newer(z.manifest.Version)
	}
	s := &core.Store{}
	if err := json.Unmarshal(b, s); err != nil {
		return fail(errors.New("导出文件内的素材库数据已损坏，无法导入"))
	}
	fillStore(s)
	z.store = s
	return z, nil
}

// fillStore: the maps a library always has.
func fillStore(s *core.Store) {
	if s.User == nil {
		s.User = map[string]*core.UserData{}
	}
	if s.Booth == nil {
		s.Booth = map[string]*core.BoothInfo{}
	}
	if s.Overrides == nil {
		s.Overrides = map[string]string{}
	}
	if s.FirstSeen == nil {
		s.FirstSeen = map[string]int64{}
	}
	if s.Purchases == nil {
		s.Purchases = map[string]*core.Purchase{}
	}
	if s.Pan == nil {
		s.Pan = map[string]*core.PanListing{}
	}
	if s.BoothMatch == nil {
		s.BoothMatch = map[string]*core.BoothMatch{}
	}
	if s.Trans == nil {
		s.Trans = map[string]string{}
	}
	for k, u := range s.User {
		if u == nil {
			delete(s.User, k)
		}
	}
	kept := s.Assets[:0]
	for _, a := range s.Assets {
		if a != nil && a.Key != "" {
			kept = append(kept, a)
		}
	}
	s.Assets = kept
}

// RootInfo: one folder of the other computer, and where it could be on this one.
type RootInfo struct {
	Path    string `json:"path"`
	Assets  int    `json:"assets"`
	Exists  bool   `json:"exists"`  // the same path is a folder here
	Suggest string `json:"suggest"` // what the mapping starts with ("" = left unmapped)
}

// ImportInfo: what an export holds, shown before anything is taken in.
type ImportInfo struct {
	Path         string     `json:"path"`
	Kind         string     `json:"kind"`
	Version      string     `json:"version"`
	Exported     int64      `json:"exported"`
	Size         int64      `json:"size"`
	Assets       int        `json:"assets"`
	Notes        int        `json:"notes"` // assets with notes, tags or other settings of the player
	Purchases    int        `json:"purchases"`
	Shares       int        `json:"shares"`
	DataFiles    int        `json:"dataFiles"`
	Skipped      int        `json:"skipped"`
	Roots        []RootInfo `json:"roots"`
	ProjectRoots []RootInfo `json:"projectRoots"`
}

// InspectExport reads what an export holds. Nothing is written.
func InspectExport(st *core.Store, file string) (*ImportInfo, error) {
	z, err := openExport(file)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	in := z.store
	info := &ImportInfo{Path: filepath.Clean(strings.Trim(strings.TrimSpace(file), `"`)), Kind: z.manifest.Kind, Version: z.manifest.Version, Exported: z.manifest.Exported,
		Size: z.size, Assets: len(in.Assets), Notes: len(in.User), Purchases: len(in.Purchases), DataFiles: len(z.data), Skipped: z.skipped,
		Roots: []RootInfo{}, ProjectRoots: []RootInfo{}}
	for k := range in.User {
		if core.IsPanShareKey(k) {
			info.Shares++
		}
	}
	st.Mu.RLock()
	mine := append([]string{}, st.Settings.Roots...)
	st.Mu.RUnlock()
	count := map[string]int{}
	for _, a := range in.Assets {
		seen := map[string]bool{}
		for _, l := range a.Locations {
			if r := rootOf(l.Path, in.Settings.Roots); r != "" && !seen[r] {
				seen[r] = true
				count[r]++
			}
		}
	}
	for _, r := range in.Settings.Roots {
		ri := RootInfo{Path: r, Assets: count[r], Exists: !uncPath(r) && core.IsDir(r)}
		if ri.Exists {
			ri.Suggest = r
		} else {
			for _, m := range mine { // a folder of the same name that is in the library here already
				if strings.EqualFold(baseOf(m), baseOf(r)) && core.IsDir(m) {
					ri.Suggest = m
					break
				}
			}
		}
		info.Roots = append(info.Roots, ri)
	}
	for _, r := range in.Settings.ProjectRoots {
		ri := RootInfo{Path: r, Exists: !uncPath(r) && core.IsDir(r)}
		if ri.Exists {
			ri.Suggest = r
		}
		for _, p := range in.Projects {
			if _, ok := underRoot(p.Path, r); ok {
				ri.Assets++ // (projects under it)
			}
		}
		info.ProjectRoots = append(info.ProjectRoots, ri)
	}
	return info, nil
}

// ---------- the library of the other computer, with this computer's paths ----------

// PendingRoot: an asset folder of the other computer that has no folder here yet. Its assets, and what
// the player noted for the ones that go by their path, wait until it is given one.
type PendingRoot struct {
	Root      string                    `json:"root"`
	Assets    []*core.Asset             `json:"assets"`
	User      map[string]*core.UserData `json:"user,omitempty"`
	FirstSeen map[string]int64          `json:"firstSeen,omitempty"`
	Overrides map[string]string         `json:"overrides,omitempty"`
	At        int64                     `json:"at"`
}

func remapList(list []string, m pathMap) []string {
	var out []string
	for _, p := range list {
		if q, ok := m.apply(p); ok {
			out = append(out, q)
		}
	}
	return out
}

// remapAsset: the asset with only its places under mapped folders, at their new paths, under the key the
// scan will give it there. nil when none of it is under a mapped folder.
func remapAsset(a *core.Asset, m pathMap, roots []string) *core.Asset {
	c := *a
	c.Locations = nil
	for _, l := range a.Locations {
		p, ok := m.apply(l.Path)
		if !ok {
			continue
		}
		nl := l
		nl.Path = p
		if r, ok := m.apply(l.Root); ok {
			nl.Root = r
		} else if r := rootOf(l.Path, roots); r != "" {
			nl.Root, _ = m.apply(r)
		}
		c.Locations = append(c.Locations, nl)
	}
	if len(c.Locations) == 0 {
		return nil
	}
	c.Key, c.AltKey = remapKey(a.Key, a, m, roots), remapKey(a.AltKey, a, m, roots)
	c.Packages, c.Archives, c.MetaDirs, c.Covers = remapList(a.Packages, m), remapList(a.Archives, m), remapList(a.MetaDirs, m), remapList(a.Covers, m)
	c.PSDs = nil
	for _, f := range a.PSDs {
		if p, ok := m.apply(f.Path); ok {
			f.Path = p
			c.PSDs = append(c.PSDs, f)
		}
	}
	c.HasDir = false
	for _, l := range c.Locations {
		c.HasDir = c.HasDir || l.Kind == "dir"
	}
	return &c
}

// remapKey: a key that goes by the folder's name and the path below it follows the folder to its new name;
// every other key (a Booth id, a name) is the same everywhere.
func remapKey(key string, a *core.Asset, m pathMap, roots []string) string {
	if !strings.HasPrefix(key, "path:") {
		return key
	}
	for _, l := range a.Locations {
		r := l.Root
		if _, ok := underRoot(l.Path, r); !ok || r == "" {
			r = rootOf(l.Path, roots)
		}
		if r == "" || pathKeyOf(r, l.Path) != key {
			continue
		}
		if np, ok := m.apply(l.Path); ok {
			nr, _ := m.apply(r)
			if k := pathKeyOf(nr, np); k != "" {
				return k
			}
		}
	}
	return key
}

// looseKey: the new key for a "path:" key no asset stands for any more, when exactly one folder has the name
// it starts with. where: that folder.
func looseKey(key string, m pathMap, roots []string) (newKey, where string) {
	rest := strings.TrimPrefix(key, "path:")
	base, tail, ok := strings.Cut(rest, "/")
	if !ok {
		return key, ""
	}
	n := 0
	for _, r := range roots {
		if rootName(r) == base {
			where = r
			n++
		}
	}
	if n != 1 {
		return key, ""
	}
	if nr, ok := m.apply(where); ok {
		return "path:" + rootName(nr) + "/" + tail, where
	}
	return key, where
}

type remapped struct {
	store   *core.Store
	pending []PendingRoot
	waiting int // assets that wait for a folder
}

// remapStore turns the library of the other computer into one for this computer. rootMap, projMap: old folder →
// folder here ("" or missing: not mapped). covers: does a cover of that file name exist in this data folder?
// A path of the other computer is only ever turned into one under a folder the player mapped; it is never
// looked for on this computer's disks as it stands (it may name another computer: uncPath).
func remapStore(in *core.Store, rootMap, projMap map[string]string, coverHere func(name string) string) remapped {
	oldRoots := in.Settings.Roots
	m := newPathMap(rootMap)
	pm := newPathMap(projMap)
	all := append(append(pathMap{}, m...), pm...)
	sort.SliceStable(all, func(i, j int) bool { return len(splitPath(all[i][0])) > len(splitPath(all[j][0])) })
	mapped := func(r string) bool { _, ok := m.apply(r); return ok }
	now := time.Now().Unix()
	pend := map[string]*PendingRoot{}
	pendOf := func(r string) *PendingRoot {
		if pend[r] == nil {
			pend[r] = &PendingRoot{Root: r, User: map[string]*core.UserData{}, FirstSeen: map[string]int64{}, Overrides: map[string]string{}, At: now}
		}
		return pend[r]
	}
	out := &core.Store{} // the maps and lists that change are made anew below; the rest is shared with in
	copyStore(out, in)
	res := remapped{}
	keyMap := map[string]string{}
	waits := map[string]string{} // a "path:" key whose folder is not mapped → that folder

	out.Assets = nil
	for _, a := range in.Assets {
		if c := remapAsset(a, m, oldRoots); c != nil {
			keyMap[a.Key] = c.Key
			if a.AltKey != "" {
				keyMap[a.AltKey] = c.AltKey
			}
			out.Assets = append(out.Assets, c)
		}
		// its places under folders without a mapping wait there, as they are
		byRoot := map[string][]core.Location{}
		for _, l := range a.Locations {
			if r := rootOf(l.Path, oldRoots); r != "" && !mapped(r) {
				byRoot[r] = append(byRoot[r], l)
			}
		}
		for r, locs := range byRoot {
			w := *a
			w.Locations = locs
			pendOf(r).Assets = append(pendOf(r).Assets, &w)
			res.waiting++
			if _, here := keyMap[a.Key]; !here && strings.HasPrefix(a.Key, "path:") {
				waits[a.Key] = r
			}
		}
	}
	keyFor := func(k string) (string, string) { // the key here, or the folder it waits for
		if nk, ok := keyMap[k]; ok {
			return nk, ""
		}
		if r, ok := waits[k]; ok {
			return k, r
		}
		if strings.HasPrefix(k, "path:") {
			nk, where := looseKey(k, m, oldRoots)
			if where != "" && !mapped(where) {
				return k, where
			}
			return nk, ""
		}
		return k, ""
	}

	out.User = map[string]*core.UserData{}
	for k, u := range in.User {
		c := *u
		// a cover the player picked: under a mapped folder, or the file of that name in this data folder's
		// covers. Any other path is dropped — the window is given whatever picture a cover names
		if p, ok := all.apply(c.Cover); ok {
			c.Cover = p
		} else {
			c.Cover = coverHere(c.Cover)
		}
		for _, f := range []*string{&c.Downloaded, &c.DownloadDir} {
			if p, ok := m.apply(*f); ok {
				*f = p
			} else {
				*f = "" // not under a folder the player gave a place here: not downloaded, here. (Left as the zip
				// has it, the next download of that card would go into a folder the zip chose.)
			}
		}
		if nk, wait := keyFor(k); wait != "" {
			if _, in := underRoot(u.Cover, wait); in {
				c.Cover = u.Cover // in the folder it waits for: it follows that folder once it is mapped
			}
			pendOf(wait).User[k] = &c
		} else if _, taken := out.User[nk]; !taken || nk == k {
			out.User[nk] = &c
		}
	}
	out.FirstSeen = map[string]int64{}
	for k, t := range in.FirstSeen {
		if nk, wait := keyFor(k); wait != "" {
			pendOf(wait).FirstSeen[k] = t
		} else {
			out.FirstSeen[nk] = t
		}
	}
	out.BoothMatch = map[string]*core.BoothMatch{}
	for k, v := range in.BoothMatch {
		if nk, wait := keyFor(k); wait == "" {
			out.BoothMatch[nk] = v
		}
	}
	out.Overrides = map[string]string{}
	for k, mode := range in.Overrides {
		if p, ok := m.apply(k); ok {
			out.Overrides[core.PathKey(p)] = mode
		} else if r := rootOf(k, oldRoots); r != "" {
			pendOf(r).Overrides[k] = mode
		}
	}
	out.Booth = map[string]*core.BoothInfo{}
	for id, b := range in.Booth {
		c := *b
		c.Cover = coverHere(c.Cover)
		out.Booth[id] = &c
	}
	out.Purchases = map[string]*core.Purchase{}
	for id, p := range in.Purchases {
		c := *p
		c.Cover = coverHere(c.Cover)
		out.Purchases[id] = &c
	}
	out.Downloaded = map[string]*core.DLRecord{}
	for id, r := range in.Downloaded {
		if r == nil {
			continue
		}
		c := *r
		if p, ok := m.apply(c.Path); ok {
			c.Path = p
		} else {
			continue
		}
		out.Downloaded[id] = &c
	}
	out.Settings.Roots = nil
	for _, r := range oldRoots {
		if p, ok := m.apply(r); ok && !containsPath(out.Settings.Roots, p) {
			out.Settings.Roots = append(out.Settings.Roots, p)
		}
	}
	out.Settings.ProjectRoots = nil
	for _, r := range in.Settings.ProjectRoots {
		if p, ok := pm.apply(r); ok && !containsPath(out.Settings.ProjectRoots, p) {
			out.Settings.ProjectRoots = append(out.Settings.ProjectRoots, p)
		}
	}
	for _, f := range []*string{&out.Settings.DownloadDir, &out.AutoDLDir} {
		if p, ok := m.apply(*f); ok {
			*f = p
		} else {
			*f = "" // the download folder of the other computer: chosen anew here
		}
	}
	out.Projects = nil
	for _, p := range in.Projects {
		if np, ok := pm.apply(p.Path); ok {
			p.Path = np
			out.Projects = append(out.Projects, p)
		}
	}
	out.Warnings = nil
	res.store = out
	for _, r := range oldRoots {
		if p := pend[r]; p != nil {
			res.pending = append(res.pending, *p)
		}
	}
	return res
}

func containsPath(list []string, p string) bool {
	for _, x := range list {
		if core.PathKey(x) == core.PathKey(p) {
			return true
		}
	}
	return false
}

// ---------- transfer.json: what waits, and the backup the last import made ----------

type transferState struct {
	Pending    []PendingRoot `json:"pending,omitempty"`
	Backup     string        `json:"backup,omitempty"` // of the library before the last import
	BackupAt   int64         `json:"backupAt,omitempty"`
	ImportFrom string        `json:"importFrom,omitempty"`
	ImportMode string        `json:"importMode,omitempty"`
	// the data files that import wrote where there was none (paths below the data folder): the backup has
	// nothing to put in their place, so its undo removes them
	Created []string `json:"created,omitempty"`
}

var transferMu sync.Mutex // transfer.json is read and written under it

func transferFile() string { return filepath.Join(core.DataDir, "transfer.json") }

func loadTransfer() *transferState {
	s := &transferState{}
	if b, err := os.ReadFile(transferFile()); err == nil {
		_ = json.Unmarshal(b, s)
	}
	return s
}

func saveTransfer(s *transferState) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeFileAtomic(transferFile(), b)
}

// PendingView: a folder of the other computer that waits for a folder here.
type PendingView struct {
	Root   string `json:"root"`
	Assets int    `json:"assets"`
	Notes  int    `json:"notes"`
	At     int64  `json:"at"`
}

type BackupView struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	At   int64  `json:"at"`
}

// TransferView: what the settings show about moving the library.
type TransferView struct {
	Run     TransferRun   `json:"run"`
	Pending []PendingView `json:"pending"`
	Undo    *BackupView   `json:"undo,omitempty"` // the last import can be undone from this backup
	From    string        `json:"from,omitempty"`
	Mode    string        `json:"mode,omitempty"`
	Backups []BackupView  `json:"backups"`
}

func TransferState() TransferView {
	transferMu.Lock()
	s := loadTransfer()
	transferMu.Unlock()
	v := TransferView{Run: TransferStatus(), Pending: []PendingView{}, Backups: []BackupView{}, From: s.ImportFrom, Mode: s.ImportMode}
	for _, p := range s.Pending {
		v.Pending = append(v.Pending, PendingView{Root: p.Root, Assets: len(p.Assets), Notes: len(p.User), At: p.At})
	}
	if fi, err := os.Stat(s.Backup); s.Backup != "" && err == nil {
		v.Undo = &BackupView{Path: s.Backup, Size: fi.Size(), At: s.BackupAt}
	}
	if ents, err := os.ReadDir(core.DataDir); err == nil {
		for _, e := range ents {
			if !e.IsDir() && strings.HasPrefix(e.Name(), kindBackup+"-") && strings.HasSuffix(e.Name(), ".zip") {
				if fi, err := e.Info(); err == nil {
					v.Backups = append(v.Backups, BackupView{Path: filepath.Join(core.DataDir, e.Name()), Size: fi.Size(), At: fi.ModTime().Unix()})
				}
			}
		}
	}
	sort.Slice(v.Backups, func(i, j int) bool { return v.Backups[i].At > v.Backups[j].At })
	return v
}

// ---------- import ----------

type ImportOptions struct {
	Path     string            `json:"path"`
	Roots    map[string]string `json:"roots"`    // asset folder there → folder here ("" = wait)
	Projects map[string]string `json:"projects"` // project folder there → folder here ("" = leave out)
	Mode     string            `json:"mode"`     // "merge" or "replace"
}

type ImportResult struct {
	Mode      string   `json:"mode"`
	Backup    string   `json:"backup"`
	Assets    int      `json:"assets"`    // assets taken into the library
	Waiting   int      `json:"waiting"`   // assets that wait for a folder
	Pending   []string `json:"pending"`   // the folders they wait for
	Notes     int      `json:"notes"`     // assets with the player's notes and settings
	DataFiles int      `json:"dataFiles"` // data files written
	KeptFiles int      `json:"keptFiles"` // merge: data files that were here already and stayed
	Skipped   int      `json:"skipped"`   // entries of the zip that were not taken
}

// ImportBusy: is something under way that an import must not run into (set by the server: scans, downloads)?
var ImportBusy = func() bool { return PipelineBusy() || core.Downloading.Load() > 0 }

// An import and its undo replace pkgcovers.json: the run that reads covers out of unitypackages is cancelled
// first and waited for (it starts again with the scan that follows).
const pkgStillBusy = "正在提取封面，尚未停止，请稍后重试"

var pkgStopWait = 30 * time.Second

// dataReloaders: called after an import or an undo replaced files in the data folder.
var dataReloaders []func()

// OnDataImported: other parts of the program that keep a data file in memory hear here when the files in
// the data folder were replaced (an import, or its undo), to read theirs again.
func OnDataImported(f func()) { dataReloaders = append(dataReloaders, f) }

// writeDataFile writes one data file of an import (tests put a failing disk here).
var writeDataFile = writeFileAtomic

// holdData keeps the data files this package holds in memory (updates.json, gencovers.json) from being
// written while an import or its undo replaces them: a save that came in between would put the old state
// back over the file that was just taken in. release lets go of them, to be read again at their next use.
func holdData() (release func()) {
	upd.mu.Lock()
	genMu.Lock()
	return func() {
		upd.s, genAll = nil, nil
		genMu.Unlock()
		upd.mu.Unlock()
	}
}

// reloadData: files in the data folder were replaced — everybody who keeps one in memory reads it again.
func reloadData() {
	for _, f := range dataReloaders {
		f()
	}
}

// verifyData reads every data file of the zip once, before any of them is written: a damaged zip (a wrong
// checksum, an entry cut short or bigger than it says) is refused while nothing has been touched.
func verifyData(z *exportZip) error {
	var total int64
	for _, f := range z.data {
		rel := strings.TrimPrefix(f.Name, "data/")
		rc, err := f.Open()
		if err != nil {
			return errors.New("导出文件内的「" + rel + "」无法读取（" + err.Error() + "）")
		}
		n, err := io.Copy(io.Discard, io.LimitReader(rc, maxDataFile+1)) // (the checksum is compared at the end of the entry)
		rc.Close()
		if err != nil {
			return errors.New("导出文件内的「" + rel + "」无法读取（" + err.Error() + "）")
		}
		if n > maxDataFile {
			return errors.New("导出文件内有超出大小限制的文件（" + f.Name + "），已拒绝导入")
		}
		if total += n; total > maxDataTotal {
			return errors.New("导出文件解压后的大小超出限制，已拒绝导入")
		}
	}
	return nil
}

// extractData writes the zip's data files into the data folder. overwrite false: a file that is here stays.
// created: the files written where there was none before (paths below the data folder).
func extractData(z *exportZip, overwrite bool) (written, kept int, created []string, err error) {
	var total int64
	for _, f := range z.data {
		rel := strings.TrimPrefix(f.Name, "data/")
		dst := filepath.Join(core.DataDir, filepath.FromSlash(rel))
		if !core.UnderDir(dst, core.DataDir) || !dataFileOK(rel) {
			continue
		}
		// no folder on the way may be a link out of the data folder
		link := false
		for d := filepath.Dir(dst); core.UnderDir(d, core.DataDir) && core.PathKey(d) != core.PathKey(core.DataDir); d = filepath.Dir(d) {
			if fi, err := os.Lstat(d); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				link = true
			}
		}
		isNew := true
		if fi, err := os.Lstat(dst); err == nil {
			isNew = false
			if !fi.Mode().IsRegular() {
				link = true
			} else if !overwrite {
				kept++
				continue
			}
		}
		if link {
			continue
		}
		b, err := readEntry(f, maxDataFile)
		if err != nil {
			return written, kept, created, errors.New("导出文件内的「" + rel + "」无法读取（" + err.Error() + "）")
		}
		if total += int64(len(b)); total > maxDataTotal {
			return written, kept, created, errors.New("导出文件解压后的大小超出限制，已停止导入")
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return written, kept, created, errors.New("无法写入数据文件「" + rel + "」（" + core.TrimErr(err) + "）")
		}
		if err := writeDataFile(dst, b); err != nil {
			dropEmptyDirs(filepath.Dir(dst)) // (the folder made for it a moment ago)
			return written, kept, created, errors.New("无法写入数据文件「" + rel + "」（" + core.TrimErr(err) + "）")
		}
		if isNew {
			created = append(created, rel)
		}
		written++
	}
	return written, kept, created, nil
}

// removeCreated takes away the data files an import wrote where there was none, and the folders that are
// empty without them. Only what an import may write is ever removed.
func removeCreated(created []string) {
	for _, rel := range created {
		p := filepath.Join(core.DataDir, filepath.FromSlash(rel))
		if !dataFileOK(rel) || !core.UnderDir(p, core.DataDir) {
			continue
		}
		if fi, err := os.Lstat(p); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(p); err != nil {
			core.Logf("撤销导入：未能删除导入时新建的 %s: %v", rel, err)
			continue
		}
		dropEmptyDirs(filepath.Dir(p))
	}
}

// dropEmptyDirs removes dir and the folders above it, up to the data folder, as far as they are empty.
func dropEmptyDirs(dir string) {
	for d := dir; core.UnderDir(d, core.DataDir) && core.PathKey(d) != core.PathKey(core.DataDir); d = filepath.Dir(d) {
		if os.Remove(d) != nil { // (only an empty folder goes)
			break
		}
	}
}

// restoreData puts the data files back as the backup has them and removes the ones written since where there
// was none: the data folder as it was when the backup was made.
func restoreData(backup string, created []string) error {
	z, err := openExport(backup)
	if err != nil {
		return err
	}
	defer z.Close()
	if z.manifest.Kind != kindBackup {
		return errors.New("备份文件无法读取")
	}
	_, _, _, err = extractData(z, true)
	removeCreated(created)
	return err
}

// coverFinder: a cover path of the other computer → the file of that name in this data folder's covers.
func coverFinder() func(string) string {
	return func(old string) string {
		name := baseOf(old)
		if old == "" || name == "" || name != core.SafeName(name, 200) {
			return ""
		}
		p := filepath.Join(core.CoversDir(), name)
		if core.FileExists(p) {
			return p
		}
		return ""
	}
}

// copyStore: the library's content from src into dst (the lists and maps themselves, not copies of them).
// What belongs to an installation is left as dst has it: the update it was told about, the notes it has
// shown, how it saves.
func copyStore(dst, src *core.Store) {
	dst.Settings, dst.Assets, dst.User, dst.Booth, dst.Overrides = src.Settings, src.Assets, src.User, src.Booth, src.Overrides
	dst.Projects, dst.FirstSeen, dst.LastScan, dst.LastUsage, dst.Warnings = src.Projects, src.FirstSeen, src.LastScan, src.LastUsage, nil
	dst.Purchases, dst.PurchaseSync, dst.GumroadSync = src.Purchases, src.PurchaseSync, src.GumroadSync
	dst.Pan, dst.BoothMatch, dst.Trans, dst.ScanStart = src.Pan, src.BoothMatch, src.Trans, src.ScanStart
	dst.Downloaded, dst.AutoDLDir = src.Downloaded, src.AutoDLDir
}

// adoptStore puts the content of src into the running library. What is this computer's own stays as it is
// here, whatever the zip says: the proxy all requests go through, where downloads go (the folder becomes an
// asset folder), whether updates are looked for and which one was skipped, how the window opens, the
// language, how 闲鱼's pages open. Caller holds st.Mu.
func adoptStore(st, src *core.Store) {
	mine, auto := st.Settings, st.AutoDLDir
	copyStore(st, src)
	st.Settings.Proxy, st.Settings.DownloadDir, st.AutoDLDir = mine.Proxy, mine.DownloadDir, auto
	st.Settings.NoUpdateCheck, st.Settings.SkipVersion = mine.NoUpdateCheck, mine.SkipVersion
	st.Settings.WindowMode, st.Settings.Lang = mine.WindowMode, mine.Lang
	st.Settings.XyExternal, st.Settings.XyNoticed, st.Settings.NoXyClip = mine.XyExternal, mine.XyNoticed, mine.NoXyClip
}

func mergeUser(cur, in *core.UserData) {
	if cur.Name == "" {
		cur.Name = in.Name
	}
	if cur.Category == "" {
		cur.Category = in.Category
	}
	if cur.Bases == nil {
		cur.Bases = in.Bases
	}
	cur.Tags = core.CleanList(append(append([]string{}, cur.Tags...), in.Tags...))
	if strings.TrimSpace(cur.Notes) == "" {
		cur.Notes = in.Notes
	}
	if cur.ShareURL == "" {
		cur.ShareURL, cur.SharePwd = in.ShareURL, in.SharePwd
	}
	if cur.PanPath == "" {
		cur.PanPath = in.PanPath
	}
	if cur.BoothURL == "" && !cur.NoBooth {
		cur.BoothURL, cur.NoBooth = in.BoothURL, in.NoBooth
	}
	if cur.NameZh == "" {
		cur.NameZh = in.NameZh
	}
	if cur.Cover == "" {
		cur.Cover = in.Cover
	}
	cur.Fav = cur.Fav || in.Fav
	if !cur.StylesSet && in.StylesSet {
		cur.Styles, cur.StylesSet = in.Styles, true
	}
	if cur.Downloaded == "" && cur.DownloadDir == "" && in.Downloaded != "" {
		cur.Downloaded, cur.PanCopy, cur.PanSaved, cur.PanGot = in.Downloaded, in.PanCopy, in.PanSaved, in.PanGot
	}
	cur.Updated = max(cur.Updated, in.Updated)
}

// mergeStore adds what src has to the running library. What both have stays as it is here; the player's
// notes are filled in where they are empty here, tags are put together.
func mergeStore(st, src *core.Store) {
	for _, r := range src.Settings.Roots {
		if !containsPath(st.Settings.Roots, r) {
			st.Settings.Roots = append(st.Settings.Roots, r)
		}
	}
	for _, r := range src.Settings.ProjectRoots {
		if !containsPath(st.Settings.ProjectRoots, r) {
			st.Settings.ProjectRoots = append(st.Settings.ProjectRoots, r)
		}
	}
	have := map[string]bool{}
	for _, a := range st.Assets {
		have[a.Key] = true
		if a.AltKey != "" {
			have[a.AltKey] = true
		}
	}
	for _, a := range src.Assets {
		if !have[a.Key] && (a.AltKey == "" || !have[a.AltKey]) {
			have[a.Key] = true
			st.Assets = append(st.Assets, a)
		}
	}
	for k, u := range src.User {
		if cur := st.User[k]; cur != nil {
			mergeUser(cur, u)
		} else {
			st.User[k] = u
		}
	}
	for k, t := range src.FirstSeen {
		if cur, ok := st.FirstSeen[k]; !ok || (t > 0 && t < cur) {
			st.FirstSeen[k] = t
		}
	}
	for k, v := range src.Overrides {
		if _, ok := st.Overrides[k]; !ok {
			st.Overrides[k] = v
		}
	}
	for k, v := range src.Booth {
		if st.Booth[k] == nil {
			st.Booth[k] = v
		}
	}
	for k, v := range src.Purchases {
		if st.Purchases[k] == nil {
			st.Purchases[k] = v
		}
	}
	if st.Pan == nil {
		st.Pan = map[string]*core.PanListing{}
	}
	for k, v := range src.Pan {
		if st.Pan[k] == nil {
			st.Pan[k] = v
		}
	}
	if st.BoothMatch == nil {
		st.BoothMatch = map[string]*core.BoothMatch{}
	}
	for k, v := range src.BoothMatch {
		if st.BoothMatch[k] == nil {
			st.BoothMatch[k] = v
		}
	}
	if st.Trans == nil {
		st.Trans = map[string]string{}
	}
	for k, v := range src.Trans {
		if _, ok := st.Trans[k]; !ok {
			st.Trans[k] = v
		}
	}
	if st.Downloaded == nil {
		st.Downloaded = map[string]*core.DLRecord{}
	}
	for k, v := range src.Downloaded {
		if st.Downloaded[k] == nil {
			st.Downloaded[k] = v
		}
	}
	for _, p := range src.Projects {
		known := false
		for _, o := range st.Projects {
			known = known || core.PathKey(o.Path) == core.PathKey(p.Path)
		}
		if !known {
			st.Projects = append(st.Projects, p)
		}
	}
	if st.PurchaseSync == 0 {
		st.PurchaseSync = src.PurchaseSync
	}
	if st.GumroadSync == 0 {
		st.GumroadSync = src.GumroadSync
	}
	if st.ScanStart == 0 {
		st.ScanStart = src.ScanStart
	}
}

// backupNow writes the library as it is, with the data files an import may replace, into a dated zip in the
// data folder.
func backupNow(st *core.Store, incoming []*zip.File) (string, error) {
	var files []string
	for _, f := range incoming {
		rel := strings.TrimPrefix(f.Name, "data/")
		if core.FileExists(filepath.Join(core.DataDir, filepath.FromSlash(rel))) {
			files = append(files, rel)
		}
	}
	dst := filepath.Join(core.DataDir, kindBackup+"-"+time.Now().Format("20060102-150405")+".zip")
	for i := 2; core.StatOK(dst); i++ {
		dst = filepath.Join(core.DataDir, fmt.Sprintf("%s-%s-%d.zip", kindBackup, time.Now().Format("20060102-150405"), i))
	}
	// what waits for a folder goes along, next to library.json
	transferMu.Lock()
	pend, _ := json.Marshal(loadTransfer().Pending)
	transferMu.Unlock()
	if _, _, err := writeZip(st, dst, kindBackup, files, map[string][]byte{"pending.json": pend}, nil); err != nil {
		return "", err
	}
	return dst, nil
}

// ImportLibrary takes an export in: the current library is backed up first, then the export's folders are
// turned into this computer's and its content replaces the library or is added to it.
func ImportLibrary(st *core.Store, opt ImportOptions) (*ImportResult, error) {
	if opt.Mode != "merge" && opt.Mode != "replace" {
		return nil, errors.New("请选择导入方式")
	}
	if ImportBusy() {
		return nil, errors.New("正在扫描或下载，请在其完成后再导入")
	}
	z, err := openExport(opt.Path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	in := z.store
	// the mapping: only folders the export has, only onto folders that are here
	rootMap, projMap := map[string]string{}, map[string]string{}
	check := func(olds []string, given, into map[string]string, what string) error {
		for _, old := range olds {
			to := strings.Trim(strings.TrimSpace(given[old]), `"`)
			if to == "" {
				continue
			}
			to = filepath.Clean(to)
			if !core.IsDir(to) {
				return errors.New(what + "不存在：" + to)
			}
			into[old] = to
		}
		return nil
	}
	if err := check(in.Settings.Roots, opt.Roots, rootMap, "素材文件夹"); err != nil {
		return nil, err
	}
	if err := check(in.Settings.ProjectRoots, opt.Projects, projMap, "工程文件夹"); err != nil {
		return nil, err
	}
	if !transferBegin("import", "正在备份当前素材库") {
		return nil, errors.New("另一项导出或导入正在进行")
	}
	done := func(msg string, err error) {
		transferSet(func(r *TransferRun) {
			r.Running, r.Msg = false, msg
			if err != nil {
				r.Err = err.Error()
			}
		})
	}
	if !StopPkgBatch(pkgStopWait) {
		err := errors.New(pkgStillBusy)
		done("导入失败", err)
		return nil, err
	}
	// 1. the whole zip is read once: nothing is written from one that is damaged
	if err := verifyData(z); err != nil {
		core.Logf("导入的文件未通过校验: %v", err)
		done("导入失败", err)
		return nil, err
	}
	// 2. the backup, and where it is: from here on the import can be undone, however it ends
	var was transferState // what the last import left to be undone
	backup, err := backupNow(st, z.data)
	if err == nil {
		transferMu.Lock()
		ts := loadTransfer()
		was = *ts
		ts.Backup, ts.BackupAt, ts.ImportFrom, ts.ImportMode, ts.Created = backup, time.Now().Unix(), filepath.Base(opt.Path), opt.Mode, nil
		if err = saveTransfer(ts); err != nil {
			_ = saveTransfer(&was)
			_ = os.Remove(backup)
		}
		transferMu.Unlock()
	}
	if err != nil {
		core.Logf("导入前备份失败: %v", err)
		err = errors.New("无法备份当前素材库，已取消导入（" + core.TrimErr(err) + "）")
		done("导入失败", err)
		return nil, err
	}
	// 3. the data files
	transferSet(func(r *TransferRun) { r.Msg = "正在写入数据文件" })
	res := &ImportResult{Mode: opt.Mode, Backup: backup, Skipped: z.skipped, Pending: []string{}}
	release := holdData()
	var created []string
	res.DataFiles, res.KeptFiles, created, err = extractData(z, opt.Mode == "replace")
	if err != nil {
		// some files are written already: back to how they were, at once. The library itself is untouched
		core.Logf("导入数据文件失败: %v", err)
		rerr := restoreData(backup, created)
		release()
		reloadData()
		transferMu.Lock()
		ts := loadTransfer()
		if rerr == nil { // nothing of this import is left to undo
			ts.Backup, ts.BackupAt, ts.ImportFrom, ts.ImportMode, ts.Created = was.Backup, was.BackupAt, was.ImportFrom, was.ImportMode, was.Created
			err = errors.New(err.Error() + "。数据文件已恢复为导入前的状态，素材库未改动")
		} else {
			core.Logf("导入失败后未能恢复数据文件: %v", rerr)
			ts.Created = created
			err = errors.New(err.Error() + "。部分数据文件已被替换且未能自动恢复，请在设置的「备份与待映射文件夹」中点击「撤销上次导入」")
		}
		if serr := saveTransfer(ts); serr != nil {
			core.Logf("transfer.json 保存失败: %v", serr)
		}
		transferMu.Unlock()
		core.BumpRev()
		done("导入失败", err)
		return nil, err
	}
	release()
	// 4. the library
	rm := remapStore(in, rootMap, projMap, coverFinder())
	res.Assets, res.Waiting, res.Notes = len(rm.store.Assets), rm.waiting, len(rm.store.User)
	for _, p := range rm.pending {
		res.Pending = append(res.Pending, p.Root)
	}
	transferMu.Lock()
	ts := loadTransfer()
	st.Mu.Lock()
	if opt.Mode == "replace" {
		adoptStore(st, rm.store)
		ts.Pending = rm.pending
	} else {
		mergeStore(st, rm.store)
		for _, p := range rm.pending {
			kept := ts.Pending[:0]
			for _, o := range ts.Pending {
				if o.Root != p.Root {
					kept = append(kept, o)
				}
			}
			ts.Pending = append(kept, p)
		}
	}
	st.Settings.SetupDone = true // (taken in from the first-run screen: there is a library now)
	st.Mu.Unlock()
	ts.Created = created
	err = saveTransfer(ts)
	transferMu.Unlock()
	if err != nil {
		core.Logf("transfer.json 保存失败: %v", err)
	}
	afterImport(st)
	core.Logf("已导入素材库（%s）：%d 个素材，%d 个等待映射，备份 %s", opt.Mode, res.Assets, res.Waiting, backup)
	done("导入完成", nil)
	return res, nil
}

// afterImport: the library was replaced under the running program — saved, shown, looked through again.
func afterImport(st *core.Store) {
	core.ProxyChanged()
	forgetDirs()
	_ = st.Save()
	reloadData()
	core.BumpRev()
	st.Mu.RLock()
	auto := st.Settings.AutoBooth
	st.Mu.RUnlock()
	StartPipeline(st, true, true, auto, false, nil)
}

// UndoImport puts the library back as the backup of the last import has it.
func UndoImport(st *core.Store) error {
	if ImportBusy() {
		return errors.New("正在扫描或下载，请在其完成后再撤销")
	}
	transferMu.Lock()
	ts := loadTransfer()
	transferMu.Unlock()
	backup := ts.Backup
	if backup == "" || !core.FileExists(backup) {
		return errors.New("未找到上次导入前的备份")
	}
	z, err := openExport(backup)
	if err != nil {
		return errors.New("备份文件无法读取：" + err.Error())
	}
	defer z.Close()
	if z.manifest.Kind != kindBackup {
		return errors.New("备份文件无法读取")
	}
	if !transferBegin("undo", "正在恢复导入前的素材库") {
		return errors.New("另一项导出或导入正在进行")
	}
	if !StopPkgBatch(pkgStopWait) {
		err = errors.New(pkgStillBusy)
	} else if err = verifyData(z); err == nil {
		release := holdData()
		_, _, _, err = extractData(z, true)
		if err == nil {
			removeCreated(ts.Created) // what the import wrote where there was nothing
		}
		release()
		if err != nil {
			reloadData() // (some files are back already)
		}
	}
	if err == nil {
		st.Mu.Lock()
		adoptStore(st, z.store)
		st.Mu.Unlock()
		transferMu.Lock()
		ts = loadTransfer()
		ts.Pending = pendingInBackup(z)
		ts.Backup, ts.BackupAt, ts.ImportFrom, ts.ImportMode, ts.Created = "", 0, "", "", nil
		_ = saveTransfer(ts)
		transferMu.Unlock()
		afterImport(st)
		core.Logf("已撤销导入，素材库恢复自 %s", backup)
	}
	transferSet(func(r *TransferRun) {
		r.Running, r.Msg = false, "已恢复"
		if err != nil {
			r.Err, r.Msg = err.Error(), "恢复失败"
		}
	})
	return err
}

// pendingInBackup: the folders that waited when the backup was made (kept in the zip next to library.json).
func pendingInBackup(z *exportZip) []PendingRoot {
	for _, f := range z.zr.File {
		if f.Name == "pending.json" {
			if b, err := readEntry(f, maxLibrary); err == nil {
				var p []PendingRoot
				if json.Unmarshal(b, &p) == nil {
					return p
				}
			}
		}
	}
	return nil
}

// MapPending gives a waiting folder of the other computer a folder here: its assets, with the notes and
// adjustments that go by their path, come into the library under the new folder.
func MapPending(st *core.Store, oldRoot, newRoot string) (int, error) {
	newRoot = filepath.Clean(strings.Trim(strings.TrimSpace(newRoot), `"`))
	if !core.IsDir(newRoot) {
		return 0, errors.New("文件夹不存在：" + newRoot)
	}
	if ImportBusy() {
		return 0, errors.New("正在扫描或下载，请稍后重试")
	}
	transferMu.Lock()
	defer transferMu.Unlock()
	ts := loadTransfer()
	var p *PendingRoot
	kept := ts.Pending[:0]
	for i := range ts.Pending {
		if ts.Pending[i].Root == oldRoot && p == nil {
			c := ts.Pending[i]
			p = &c
			continue
		}
		kept = append(kept, ts.Pending[i])
	}
	if p == nil {
		return 0, errors.New("未找到等待映射的文件夹")
	}
	ts.Pending = kept
	// a library of that one folder, turned into this computer's and added
	src := &core.Store{Assets: p.Assets, User: p.User, FirstSeen: p.FirstSeen, Overrides: p.Overrides}
	src.Settings.Roots = []string{oldRoot}
	fillStore(src)
	rm := remapStore(src, map[string]string{oldRoot: newRoot}, nil, coverFinder())
	m := newPathMap(map[string]string{oldRoot: newRoot})
	st.Mu.Lock()
	mergeStore(st, rm.store)
	// what was taken in earlier and still points into the old folder
	for _, u := range st.User {
		for _, f := range []*string{&u.Cover, &u.Downloaded} {
			if q, ok := m.apply(*f); ok {
				*f = q
			}
		}
	}
	for _, r := range st.Downloaded {
		if q, ok := m.apply(r.Path); ok {
			r.Path = q
		}
	}
	st.Mu.Unlock()
	if err := saveTransfer(ts); err != nil {
		core.Logf("transfer.json 保存失败: %v", err)
	}
	afterImport(st)
	return len(rm.store.Assets), nil
}

// DropPending: the player does not want a waiting folder's assets after all.
func DropPending(oldRoot string) error {
	transferMu.Lock()
	defer transferMu.Unlock()
	ts := loadTransfer()
	kept := ts.Pending[:0]
	for _, p := range ts.Pending {
		if p.Root != oldRoot {
			kept = append(kept, p)
		}
	}
	ts.Pending = kept
	return saveTransfer(ts)
}
