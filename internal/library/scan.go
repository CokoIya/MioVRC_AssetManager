package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/core"
	"vrclib/internal/naming"
)

var (
	archiveExt = map[string]bool{".zip": true, ".rar": true, ".7z": true}
	ImageExt   = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
	// loose files that appear in both product folders and category folders (shop ads, readmes, links)
	weakFileExt = map[string]bool{".url": true, ".pdf": true, ".txt": true, ".md": true, ".jpg": true, ".jpeg": true,
		".png": true, ".webp": true, ".gif": true, ".mp4": true, ".html": true, ".htm": true, ".docx": true}
	// Unity / 3D content: an asset with any of these is more than a texture-source pack
	modelExt = map[string]bool{".prefab": true, ".fbx": true, ".blend": true, ".vrm": true, ".unity": true, ".controller": true,
		".anim": true, ".mat": true, ".asset": true, ".shader": true, ".vrca": true}
	skipDirNames = map[string]bool{"library": true, ".git": true, "node_modules": true, "temp": true, "$recycle.bin": true,
		"system volume information": true}
	reTrailVer = regexp.MustCompile(`[\s_\-.]*(v|ver\.?|version)?[\s_\-.]*[0-9]+([._][0-9]+)*[a-z]?$`)
	// .url shortcuts that point at a dependency rather than the product itself
	reDepURL    = regexp.MustCompile(`liltoon|modular|vrcfury|poiyomi|optimizer|gesture ?manager|vrchat|creator ?companion|alcom|unity|ndmf|lilxyzw`)
	depBoothIDs = map[string]bool{"3087170": true} // lilToon
)

// mergeName: identity used to merge the same product across folders, ignoring trailing versions/dates.
func mergeName(raw string) string {
	s := strings.ToLower(naming.CleanName(raw))
	for i := 0; i < 3; i++ {
		t := reTrailVer.ReplaceAllString(s, "")
		if t == s || len([]rune(naming.NormKey(t))) < 4 {
			break
		}
		s = t
	}
	return naming.NormKey(s)
}

type scanCtx struct {
	settings  core.Settings
	overrides map[string]string
	assets    map[string]*core.Asset
	alt       map[string]string // name key → booth key, so id-less copies merge into the booth asset
	order     []string
	warnings  []string
	now       int64
}

// RunFolderScan walks all roots and rebuilds the asset list (keeping usage info from the previous run).
func RunFolderScan(st *core.Store, prog *core.Task) {
	st.Mu.RLock()
	settings := st.Settings
	ov := map[string]string{}
	for k, v := range st.Overrides {
		ov[k] = v
	}
	st.Mu.RUnlock()

	ctx := &scanCtx{settings: settings, overrides: ov, assets: map[string]*core.Asset{}, alt: map[string]string{}, now: time.Now().Unix()}
	for i, root := range settings.Roots {
		prog.Set(i, len(settings.Roots), "正在扫描 "+root)
		fi, err := os.Stat(root)
		if err != nil || !fi.IsDir() {
			ctx.warnings = append(ctx.warnings, "未找到素材文件夹："+root)
			continue
		}
		ctx.scanContainer(root, root, 0, nil)
	}

	// finalize
	list := make([]*core.Asset, 0, len(ctx.order))
	for _, k := range ctx.order {
		a := ctx.assets[k]
		finalizeAsset(a, settings)
		list = append(list, a)
	}

	st.Mu.Lock()
	old := map[string]*core.Asset{}
	for _, a := range st.Assets {
		old[a.Key] = a
	}
	for _, a := range list {
		if fs, ok := st.FirstSeen[a.Key]; ok {
			a.FirstSeen = fs
		} else if a.AltKey != "" && st.FirstSeen[a.AltKey] != 0 {
			a.FirstSeen = st.FirstSeen[a.AltKey]
			st.FirstSeen[a.Key] = a.FirstSeen
		} else {
			// first scan ever: use file mtime so "最近添加" is meaningful; later scans: now
			if st.LastScan == 0 {
				a.FirstSeen = a.MTime
			} else {
				a.FirstSeen = ctx.now
			}
			st.FirstSeen[a.Key] = a.FirstSeen
		}
		if o, ok := old[a.Key]; ok {
			a.Usage = o.Usage
			a.GuidCount = o.GuidCount
		}
		st.UserFor(a) // migrates alt keys
	}
	st.Assets = list
	if st.ScanStart == 0 {
		st.ScanStart = ctx.now
	}
	ApplyPurchases(st)
	st.Warnings = ctx.warnings
	st.LastScan = ctx.now
	st.Mu.Unlock()
	prog.Set(len(settings.Roots), len(settings.Roots), "完成")
}

type group struct {
	key    string
	dirs   []string
	files  []string
	covers []string // pictures lying next to it under its name ("X.unitypackage" + "X.png")
}

// siblingCover finds the asset a loose picture belongs to: the one with the same name, or — "X_preview.png",
// "X v1.2.zip" next to "X.jpg" — the one whose name it starts with or that starts with its name. A picture that
// fits two assets equally well belongs to neither.
func siblingCover(imgKey string, groups map[string]*group) *group {
	if g := groups[imgKey]; g != nil {
		return g
	}
	if len([]rune(imgKey)) < 3 {
		return nil
	}
	var best *group
	bestLen, tie := 0, false
	for k, g := range groups {
		n := 0
		switch {
		case len([]rune(k)) >= 4 && strings.HasPrefix(imgKey, k):
			n = len(k)
		case len([]rune(imgKey)) >= 4 && strings.HasPrefix(k, imgKey):
			n = len(imgKey)
		default:
			continue
		}
		if n > bestLen {
			best, bestLen, tie = g, n, false
		} else if n == bestLen {
			tie = true
		}
	}
	if tie {
		return nil
	}
	return best
}

func (c *scanCtx) scanContainer(dir, root string, depth int, hints []string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		c.warnings = append(c.warnings, "无法读取："+dir)
		return
	}
	groups := map[string]*group{}
	var order []string
	var pictures [][2]string // name key, path
	add := func(name string, full string, isDir bool) {
		k := naming.NormKey(core.StripArchiveExt(name))
		if isDir {
			k = naming.NormKey(name)
		}
		g := groups[k]
		if g == nil {
			g = &group{key: k}
			groups[k] = g
			order = append(order, k)
		}
		if isDir {
			g.dirs = append(g.dirs, full)
		} else {
			g.files = append(g.files, full)
		}
	}
	for _, e := range ents {
		name := e.Name()
		full := filepath.Join(dir, name)
		if strings.HasPrefix(name, ".") || strings.HasSuffix(strings.ToLower(name), ".meta") {
			continue
		}
		if e.IsDir() {
			if skipDirNames[strings.ToLower(name)] {
				continue
			}
			add(name, full, true)
		} else {
			if naming.LooseAssetExt[core.LowerExt(name)] || strings.HasSuffix(strings.ToLower(name), ".tar.gz") {
				add(name, full, false)
			} else if ImageExt[core.LowerExt(name)] {
				if fi, err := e.Info(); err == nil && fi.Size() > 2*1024 && fi.Size() < 15*1024*1024 { // named after the asset: small is fine
					pictures = append(pictures, [2]string{naming.NormKey(strings.TrimSuffix(name, filepath.Ext(name))), full})
				}
			}
		}
	}
	for _, p := range pictures {
		if g := siblingCover(p[0], groups); g != nil && len(g.covers) < 4 {
			g.covers = append(g.covers, p[1])
		}
	}
	for _, k := range order {
		g := groups[k]
		if c.overrides[core.PathKey(firstOf(g))] == "ignore" {
			continue
		}
		if len(g.dirs) > 0 {
			d := g.dirs[0]
			mode := c.overrides[core.PathKey(d)]
			if core.IsUnityProject(d) {
				continue
			}
			if mode == "split" || (mode == "" && c.isContainer(d, depth)) {
				c.scanContainer(d, root, depth+1, append(append([]string{}, hints...), filepath.Base(d)))
				// archives grouped with a container folder still become their own asset
				if len(g.files) > 0 {
					c.addAsset(&group{key: g.key, files: g.files}, root, hints)
				}
				continue
			}
		}
		c.addAsset(g, root, hints)
	}
}

func firstOf(g *group) string {
	if len(g.dirs) > 0 {
		return g.dirs[0]
	}
	return g.files[0]
}

// isContainer decides whether a folder is a category folder holding several products.
func (c *scanCtx) isContainer(dir string, depth int) bool {
	if depth >= 4 {
		return false
	}
	name := filepath.Base(dir)
	if naming.BoothIDFromName(name) != "" {
		return false
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	pile := isPile(ents)
	pk := naming.NormKey(naming.CleanName(name))
	items := map[string]bool{}
	dirItems := map[string]bool{}
	archItems := map[string]bool{}
	weak := false
	structural, idChildren, genericChildren := 0, 0, 0
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		var k, base string
		if e.IsDir() {
			if skipDirNames[strings.ToLower(n)] {
				continue
			}
			k, base = naming.NormKey(n), n
			dirItems[k] = true
		} else {
			ext := core.LowerExt(n)
			if naming.StrongFileExt[ext] && !(pile && ext == ".unitypackage") {
				return false
			}
			if archiveExt[ext] || ext == ".unitypackage" || strings.HasSuffix(strings.ToLower(n), ".tar.gz") {
				base = core.StripArchiveExt(n)
				k = naming.NormKey(base)
				archItems[k] = true
			} else {
				if weakFileExt[ext] {
					weak = true
				}
				continue
			}
		}
		if items[k] {
			continue
		}
		items[k] = true
		if naming.BoothIDFromName(base) != "" {
			idChildren++
		}
		if naming.IsCategoryWordName(base) {
			genericChildren++
		}
		if naming.IsStructuralName(base, pk) {
			structural++
		}
	}
	if pile {
		return true
	}
	if len(items) < 2 || structural*2 >= len(items) {
		return false
	}
	evidence := idChildren > 0 || genericChildren >= 2 || naming.IsCategoryWordName(name) || naming.ReCollection.MatchString(strings.ToLower(name))
	if weak || depth >= 3 {
		return evidence
	}
	// without evidence only extracted-but-unpaired subfolders count: "X/" next to "X.zip" inside a product
	// folder is usually a bundled dependency that was unpacked in place
	independent := 0
	for k := range dirItems {
		if !archItems[k] {
			independent++
		}
	}
	return independent >= 2 || evidence
}

// isPile: a folder where separate downloads were dropped loose — packages and archives of different names,
// each with a picture of its own name next to it ("A.unitypackage" + "A.png", "B.zip" + "B.jpg"). A product
// folder does not look like that: its pictures are named after what they show, not after its packages.
func isPile(ents []os.DirEntry) bool {
	assets, pics := map[string]bool{}, map[string]bool{}
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, ".") {
			continue
		}
		ext := core.LowerExt(n)
		switch {
		case ext == ".unitypackage" || archiveExt[ext] || strings.HasSuffix(strings.ToLower(n), ".tar.gz"):
			assets[naming.NormKey(core.StripArchiveExt(n))] = true
		case naming.StrongFileExt[ext]:
			return false // loose models, scenes, sources: one product's own files
		case ImageExt[ext]:
			pics[naming.NormKey(strings.TrimSuffix(n, filepath.Ext(n)))] = true
		}
	}
	paired := 0
	for k := range assets {
		if pics[k] {
			paired++
		}
	}
	return paired >= 2 && (len(assets) >= 3 || paired == len(assets))
}

func kindOf(path string, isDir bool) string {
	if isDir {
		return "dir"
	}
	l := strings.ToLower(path)
	switch {
	case strings.HasSuffix(l, ".zip"):
		return "zip"
	case strings.HasSuffix(l, ".rar"):
		return "rar"
	case strings.HasSuffix(l, ".7z"):
		return "7z"
	case strings.HasSuffix(l, ".unitypackage"):
		return "unitypackage"
	}
	return "file"
}

func (c *scanCtx) addAsset(g *group, root string, hints []string) {
	primary := firstOf(g)
	rawName := filepath.Base(primary)
	if len(g.dirs) == 0 {
		rawName = core.StripArchiveExt(rawName)
	}
	a := &core.Asset{RawName: rawName, Hints: hints, Covers: append([]string{}, g.covers...)}
	ids := map[string]int{}
	var names, topNames []string
	names = append(names, rawName)
	for _, d := range g.dirs {
		a.HasDir = true
		info := walkAsset(d)
		a.Size += info.size
		a.Files += info.files
		a.Packages = append(a.Packages, info.packages...)
		a.Archives = append(a.Archives, info.archives...)
		a.OtherArchives += info.otherArc
		a.Covers = append(a.Covers, info.covers...)
		a.MetaDirs = append(a.MetaDirs, info.metaDirs...)
		a.PSDs = append(a.PSDs, info.psds...)
		a.PSDCount += info.psdCount
		a.ModelFiles += info.models
		if info.mtime > a.MTime {
			a.MTime = info.mtime
		}
		for id, n := range info.urlIDs {
			ids[id] += n
		}
		names = append(names, info.wrapperNames...)
		if topNames == nil {
			topNames = info.topNames
		}
		a.Locations = append(a.Locations, core.Location{Path: d, Kind: "dir", Size: info.size, Root: root})
	}
	for _, f := range g.files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		k := kindOf(f, false)
		a.Locations = append(a.Locations, core.Location{Path: f, Kind: k, Size: fi.Size(), Root: root})
		if !a.HasDir {
			a.Size += fi.Size()
			a.Files++
		}
		if fi.ModTime().Unix() > a.MTime {
			a.MTime = fi.ModTime().Unix()
		}
		if k != "zip" && archive.IsArchiveFile(f) {
			a.OtherArchives++
		}
		switch k {
		case "unitypackage":
			a.Packages = append(a.Packages, f)
		case "zip":
			a.Archives = append(a.Archives, f)
			if !a.HasDir {
				zl := zipListing(f)
				a.PSDCount += zl.psds
				a.PSDInZip += zl.psds
				a.ModelFiles += zl.models
				a.ZipPackages += zl.packages
			}
		}
	}
	// booth id: folder/file names first (outer → inner), then .url shortcuts (display/fetch only, never identity)
	nameID := ""
	for _, n := range names {
		if id := naming.BoothIDFromName(n); id != "" {
			nameID = id
			break
		}
	}
	a.BoothID = nameID
	if a.BoothID == "" && len(ids) == 1 {
		for id := range ids {
			a.BoothID = id
			a.BoothFromURL = true
		}
	}
	a.Name = naming.CleanName(rawName)
	// wrapper folders like "4016/7825319/…" or "材质/_Material_X.zip": prefer the inner, more descriptive name
	if (naming.IsMostlyDigits(a.Name) || naming.IsGenericName(a.Name)) && len(names) > 1 {
		for _, n := range names[1:] {
			if cn := naming.CleanName(n); !naming.IsMostlyDigits(cn) && !naming.IsGenericName(cn) {
				a.Name = cn
				break
			}
		}
	}
	// a generic folder name ("衣服头发") whose children share a product name
	if a.BoothID == "" && naming.IsGenericName(a.Name) && len(topNames) > 0 {
		if p := naming.CommonChildPrefix(topNames); p != "" {
			a.Name = p + "（" + a.Name + "）"
		}
	}
	keySrc := rawName
	if naming.IsGenericName(naming.CleanName(rawName)) && !naming.IsGenericName(a.Name) && !strings.Contains(a.Name, "（") {
		keySrc = a.Name // "材质/_Material_X.zip" is identified by the inner name
	}
	nk := mergeName(keySrc)
	nameKey := "name:" + nk
	if (core.IsASCII(nk) && len(nk) < 6) || len([]rune(nk)) < 3 || naming.IsGenericName(naming.CleanName(keySrc)) {
		rel, err := filepath.Rel(root, primary)
		if err != nil {
			rel = primary
		}
		nameKey = "path:" + filepath.Base(root) + "/" + strings.ToLower(filepath.ToSlash(rel))
	}
	if nameID != "" {
		a.Key = "booth:" + nameID
		a.AltKey = nameKey
	} else {
		a.Key = nameKey
	}
	// merge with an asset of the same identity found in another place
	if ex, ok := c.assets[a.Key]; ok {
		mergeAsset(ex, a)
		return
	}
	if nameID == "" {
		if bk, ok := c.alt[nameKey]; ok {
			if ex, ok := c.assets[bk]; ok {
				mergeAsset(ex, a)
				return
			}
		}
	}
	if nameID != "" {
		if ex, ok := c.assets[nameKey]; ok && !strings.HasPrefix(ex.Key, "booth:") {
			// same product seen first without an id → upgrade it
			delete(c.assets, nameKey)
			for i, k := range c.order {
				if k == nameKey {
					c.order[i] = a.Key
				}
			}
			ex.Key, ex.AltKey, ex.BoothID = a.Key, nameKey, nameID
			mergeAsset(ex, a)
			c.assets[a.Key] = ex
			c.alt[nameKey] = a.Key
			return
		}
		if !strings.HasPrefix(nameKey, "path:") {
			c.alt[nameKey] = a.Key
		}
	}
	c.assets[a.Key] = a
	c.order = append(c.order, a.Key)
}

func mergeAsset(dst, src *core.Asset) {
	dst.Locations = append(dst.Locations, src.Locations...)
	dst.Packages = append(dst.Packages, src.Packages...)
	dst.Archives = append(dst.Archives, src.Archives...)
	dst.OtherArchives += src.OtherArchives
	dst.Covers = append(dst.Covers, src.Covers...)
	dst.MetaDirs = append(dst.MetaDirs, src.MetaDirs...)
	dst.Hints = append(dst.Hints, src.Hints...)
	if src.HasDir && !dst.HasDir {
		dst.HasDir = true
		dst.Name = src.Name
		dst.RawName = src.RawName
	}
	if dst.Size < src.Size {
		dst.Size = src.Size
	}
	// the same product seen twice (folder + its zip): keep the fuller picture, do not add up
	if len(dst.PSDs) == 0 {
		dst.PSDs = src.PSDs
	}
	dst.PSDCount = max(dst.PSDCount, src.PSDCount)
	dst.PSDInZip = max(dst.PSDInZip, src.PSDInZip)
	dst.ModelFiles = max(dst.ModelFiles, src.ModelFiles)
	dst.ZipPackages = max(dst.ZipPackages, src.ZipPackages)
	if dst.Files < src.Files {
		dst.Files = src.Files
	}
	if src.MTime > dst.MTime {
		dst.MTime = src.MTime
	}
	if dst.BoothID == "" {
		dst.BoothID = src.BoothID
		dst.BoothFromURL = src.BoothFromURL
	} else if dst.BoothFromURL && src.BoothID != "" && !src.BoothFromURL {
		dst.BoothID, dst.BoothFromURL = src.BoothID, false
	}
}

type walkInfo struct {
	size         int64
	files        int
	mtime        int64
	packages     []string
	archives     []string
	otherArc     int // rar, 7z and split volumes: only unpacked for an import
	covers       []string
	metaDirs     []string
	urlIDs       map[string]int
	wrapperNames []string
	topNames     []string
	psds         []core.PSDFile
	psdCount     int
	models       int
}

func walkAsset(root string) walkInfo {
	info := walkInfo{urlIDs: map[string]int{}}
	// wrapper chain: a folder holding exactly one item (a subfolder, an archive, or a folder+archive pair) and nothing else
	cur := root
	for i := 0; i < 4; i++ {
		ents, err := os.ReadDir(cur)
		if err != nil {
			break
		}
		groups := map[string]bool{}
		var onlyDir, onlyFile string
		others := 0
		info.topNames = info.topNames[:0]
		for _, e := range ents {
			n := e.Name()
			ln := strings.ToLower(n)
			if strings.HasPrefix(n, ".") || strings.HasSuffix(ln, ".meta") {
				continue
			}
			info.topNames = append(info.topNames, n)
			if e.IsDir() {
				groups[naming.NormKey(n)] = true
				onlyDir = n
			} else if archiveExt[core.LowerExt(n)] || strings.HasSuffix(ln, ".unitypackage") || strings.HasSuffix(ln, ".tar.gz") {
				groups[naming.NormKey(core.StripArchiveExt(n))] = true
				onlyFile = n
			} else {
				others++
			}
		}
		if len(groups) != 1 || others != 0 {
			break
		}
		if onlyDir != "" {
			info.wrapperNames = append(info.wrapperNames, onlyDir)
			cur = filepath.Join(cur, onlyDir)
			continue
		}
		info.wrapperNames = append(info.wrapperNames, core.StripArchiveExt(onlyFile))
		break
	}
	type coverCand struct {
		path  string
		score int
	}
	var cands []coverCand
	metaSeen := map[string]bool{}
	baseDepth := strings.Count(filepath.Clean(root), string(os.PathSeparator))
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && skipDirNames[strings.ToLower(d.Name())] {
				return filepath.SkipDir
			}
			return nil
		}
		if info.files > 60000 {
			return filepath.SkipAll
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		info.files++
		info.size += fi.Size()
		if t := fi.ModTime().Unix(); t > info.mtime {
			info.mtime = t
		}
		name := d.Name()
		ext := core.LowerExt(name)
		depth := strings.Count(filepath.Clean(p), string(os.PathSeparator)) - baseDepth
		switch {
		case ext == ".unitypackage":
			info.packages = append(info.packages, p)
		case ext == ".zip":
			info.archives = append(info.archives, p)
		case archive.IsArchiveFile(name):
			info.otherArc++
		case ext == ".meta":
			dir := filepath.Dir(p)
			if !metaSeen[dir] {
				metaSeen[dir] = true
				if len(info.metaDirs) < 200 {
					info.metaDirs = append(info.metaDirs, dir)
				}
			}
		case ext == ".url" && depth <= 4 && fi.Size() < 8192 && !reDepURL.MatchString(strings.ToLower(name)):
			if b, err := os.ReadFile(p); err == nil {
				for _, m := range naming.ReBoothURL.FindAllStringSubmatch(string(b), -1) {
					if !depBoothIDs[m[1]] {
						info.urlIDs[m[1]]++
					}
				}
			}
		case naming.PSDExt[ext]:
			info.psdCount++
			if len(info.psds) < 40 {
				pf := core.PSDFile{Path: p, Size: fi.Size()}
				if ext == ".psd" || ext == ".psb" {
					pf.W, pf.H = psdDims(p)
				}
				info.psds = append(info.psds, pf)
			}
		case modelExt[ext]:
			info.models++
		case ImageExt[ext] && depth <= 3 && fi.Size() > 8*1024 && fi.Size() < 15*1024*1024:
			cands = append(cands, coverCand{p, coverScore(name, depth, fi.Size())})
		}
		return nil
	})
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	for i, c := range cands {
		if i >= 6 {
			break
		}
		info.covers = append(info.covers, c.path)
	}
	return info
}

func coverScore(name string, depth int, size int64) int {
	l := strings.ToLower(name)
	s := 100 - depth*20
	for _, w := range []string{"thumb", "サムネ", "cover", "封面", "main", "メイン", "top", "商品", "sample", "preview", "预览", "icon", "image", "_base"} {
		if strings.Contains(l, w) {
			s += 40
			break
		}
	}
	for _, w := range []string{"mask", "normal", "_n.", "matcap", "rim", "shadow", "alpha", "emission", "uv", "tex", "_m.", "body", "face", "hair", "eye", "skin", "color", "gradation", "spec"} {
		if strings.Contains(l, w) {
			s -= 60
			break
		}
	}
	if size > 200*1024 {
		s += 10
	}
	return s
}

// ---------- classification ----------

var Categories = []string{"素体", "衣服", "头发", "配饰", "道具", "材质", "面捕", "插件", "动作", "音效", "字体", "其他"}

func assetText(a *core.Asset) string {
	var b strings.Builder
	b.WriteString(a.RawName)
	for _, h := range a.Hints {
		b.WriteString(" ")
		b.WriteString(h)
	}
	for _, p := range a.Packages {
		b.WriteString(" ")
		b.WriteString(filepath.Base(p))
	}
	for _, l := range a.Locations {
		b.WriteString(" ")
		b.WriteString(filepath.Base(l.Path))
	}
	return b.String()
}

func finalizeAsset(a *core.Asset, s core.Settings) {
	a.Hints = core.UniqStrings(a.Hints)
	a.Packages = core.UniqStrings(a.Packages)
	a.Archives = core.UniqStrings(a.Archives)
	a.Covers = core.UniqStrings(a.Covers)
	a.MetaDirs = core.UniqStrings(a.MetaDirs)
	text := assetText(a)
	// a lone font file
	if len(a.Locations) == 1 {
		if e := core.LowerExt(a.Locations[0].Path); e == ".ttf" || e == ".otf" {
			a.Category = "字体"
		}
	}
	if a.Category == "" {
		a.Category = naming.BracketCategory(a.RawName)
	}
	if a.Category == "" {
		// own name first, then the nearest category folder it sits in ("衣服/侠客服"), then everything else
		a.Category = naming.Classify(a.Name)
		if a.Category == "其他" {
			for i := len(a.Hints) - 1; i >= 0; i-- {
				if naming.IsCategoryWordName(a.Hints[i]) {
					if c := naming.Classify(a.Hints[i]); c != "其他" {
						a.Category = c
						break
					}
				}
			}
		}
		if a.Category == "其他" {
			a.Category = naming.Classify(text)
		}
	}
	a.Bases = naming.DetectBases(text, naming.ParseBases(s.Bases))
	// primary location first: extracted folder, then archives
	sort.SliceStable(a.Locations, func(i, j int) bool {
		return kindRank(a.Locations[i].Kind) < kindRank(a.Locations[j].Kind)
	})
}

func kindRank(k string) int {
	switch k {
	case "dir":
		return 0
	case "unitypackage":
		return 1
	case "zip", "rar", "7z":
		return 2
	}
	return 3
}
