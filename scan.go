package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	archiveExt = map[string]bool{".zip": true, ".rar": true, ".7z": true}
	imageExt   = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
	// files that, lying directly inside a folder, prove it is a product folder (not a category folder)
	strongFileExt = map[string]bool{".unitypackage": true, ".fbx": true, ".blend": true, ".psd": true, ".ttf": true,
		".otf": true, ".vrca": true, ".prefab": true, ".clip": true, ".mat": true, ".anim": true, ".controller": true,
		".asset": true, ".shader": true, ".cs": true, ".unity": true}
	// loose files that appear in both product folders and category folders (shop ads, readmes, links)
	weakFileExt = map[string]bool{".url": true, ".pdf": true, ".txt": true, ".md": true, ".jpg": true, ".jpeg": true,
		".png": true, ".webp": true, ".gif": true, ".mp4": true, ".html": true, ".htm": true, ".docx": true}
	// loose files that count as an asset on their own
	looseAssetExt = map[string]bool{".unitypackage": true, ".zip": true, ".rar": true, ".7z": true, ".ttf": true,
		".otf": true, ".fbx": true, ".blend": true, ".vrca": true}
	// texture sources that buyers edit to recolour an outfit
	psdExt = map[string]bool{".psd": true, ".psb": true, ".clip": true, ".sai": true, ".sai2": true, ".kra": true, ".xcf": true, ".mdp": true}
	// Unity / 3D content: an asset with any of these is more than a texture-source pack
	modelExt = map[string]bool{".prefab": true, ".fbx": true, ".blend": true, ".vrm": true, ".unity": true, ".controller": true,
		".anim": true, ".mat": true, ".asset": true, ".shader": true, ".vrca": true}
	skipDirNames = map[string]bool{"library": true, ".git": true, "node_modules": true, "temp": true, "$recycle.bin": true,
		"system volume information": true}

	reBoothID    = regexp.MustCompile(`(?:^|[^0-9])([0-9]{6,8})(?:[^0-9]|$)`)
	reBracketNum = regexp.MustCompile(`[【\[]\s*[0-9]{1,5}\s*[】\]]`)
	reDupSuffix  = regexp.MustCompile(`\s*[\(（][0-9]{1,2}[\)）]\s*$`)
	reYYMMDD     = regexp.MustCompile(`^2[0-9](0[1-9]|1[0-2])(0[1-9]|[12][0-9]|3[01])$`)
	reBoothURL   = regexp.MustCompile(`booth\.pm/(?:[a-z]{2}/)?items/([0-9]{5,9})`)
	reNonKey     = regexp.MustCompile(`[\s_\-\.\(\)（）\[\]【】'"+&,，!！~～・]+`)
	reTrailVer   = regexp.MustCompile(`[\s_\-.]*(v|ver\.?|version)?[\s_\-.]*[0-9]+([._][0-9]+)*[a-z]?$`)
	reGenericKey = regexp.MustCompile(`^(材质|材料|道具|衣服|服装|衣装|头发|发型|配饰|饰品|素体|模型|插件|系统|动作|音效|字体|其他|其它|贴图|纹理|妆容|素材|资源|新建文件夹|新しいフォルダー|newfolder|备份|测试|更新|归总|合集|整合|特典|bonus|dlc|materials?|textures?|psd|fbx|prefabs?|unitypackages?|readme|tools?|hair|clothe?s?|outfits?|accessor(y|ies)|props?|plugins?|others?|misc|assets?|new|update[sd]?|fix(ed)?|最新|修复|版|年|月|日|号)+$`)
	reStructural = regexp.MustCompile(`^(fbx|blend|textures?|tex|materials?|mat|psd|prefabs?|unitypackages?|readme|docs?|documents?|説明書|说明|manual|uv|uvmap|animations?|anim|meshe?s?|models?|images?|画像|samples?|preview|thumbnails?|サムネ|その他|others?|extra|bonus|特典|terms|规约|利用規約|shaders?|scripts?|editor|sounds?|audio|icons?|menu|expressions?|fx|data|source|src|assets|resources|png|tga|masks?|normal(map)?|emission|matcap|spec|liltoon|body|face|costume|kaihen_tips)$`)
	reCollection = regexp.MustCompile(`合集|合辑|全家桶|collection|大全`)
	// .url shortcuts that point at a dependency rather than the product itself
	reDepURL    = regexp.MustCompile(`liltoon|modular|vrcfury|poiyomi|optimizer|gesture ?manager|vrchat|creator ?companion|alcom|unity|ndmf|lilxyzw`)
	depBoothIDs = map[string]bool{"3087170": true} // lilToon
)

func lowerExt(name string) string { return strings.ToLower(filepath.Ext(name)) }

func boothIDFromName(name string) string {
	for _, m := range reBoothID.FindAllStringSubmatch(name, -1) {
		id := m[1]
		if len(id) == 8 && strings.HasPrefix(id, "20") { // looks like a date 20YYMMDD
			continue
		}
		if len(id) == 6 && reYYMMDD.MatchString(id) { // "260811更新"
			continue
		}
		return id
	}
	return ""
}

func stripArchiveExt(name string) string {
	l := strings.ToLower(name)
	for _, e := range []string{".tar.gz", ".zip", ".rar", ".7z", ".unitypackage"} {
		if strings.HasSuffix(l, e) {
			return name[:len(name)-len(e)]
		}
	}
	return name
}

func normKey(s string) string {
	s = reDupSuffix.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = reNonKey.ReplaceAllString(s, "")
	return s
}

func cleanName(n string) string {
	s := reDupSuffix.ReplaceAllString(n, "")
	s = reBracketNum.ReplaceAllString(s, "")
	if id := boothIDFromName(s); id != "" {
		s = strings.Replace(s, id, " ", 1)
	}
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, " _-·.")
	if s == "" {
		return strings.TrimSpace(n)
	}
	return s
}

// isGenericName: names like "材质", "衣服头发", "12月26日更新" that say nothing about the product.
func isGenericName(n string) bool {
	k := normKey(n)
	k = regexp.MustCompile(`[0-9]+`).ReplaceAllString(k, "")
	k = strings.ReplaceAll(k, "version", "")
	k = strings.ReplaceAll(k, "ver", "")
	if k == "" || k == "v" {
		return true
	}
	return reGenericKey.MatchString(k)
}

func isStructuralName(n, parentKey string) bool {
	k := normKey(n)
	if reStructural.MatchString(strings.TrimLeft(strings.ToLower(n), "_ ")) || reStructural.MatchString(k) {
		return true
	}
	return len([]rune(parentKey)) >= 4 && strings.Contains(k, parentKey)
}

// mergeName: identity used to merge the same product across folders, ignoring trailing versions/dates.
func mergeName(raw string) string {
	s := strings.ToLower(cleanName(raw))
	for i := 0; i < 3; i++ {
		t := reTrailVer.ReplaceAllString(s, "")
		if t == s || len([]rune(normKey(t))) < 4 {
			break
		}
		s = t
	}
	return normKey(s)
}

// commonChildPrefix finds a product name shared by most children ("Crimson Rumor_Hair", "Crimson_Rumor_PSD" → "Crimson Rumor").
func commonChildPrefix(names []string) string {
	count := map[string]int{}
	disp := map[string]string{}
	n := 0
	for _, raw := range names {
		c := cleanName(stripArchiveExt(raw))
		if isGenericName(c) || isStructuralName(c, "") {
			continue
		}
		toks := regexp.MustCompile(`[\s_\-.]+`).Split(strings.TrimSpace(c), -1)
		var t []string
		for _, x := range toks {
			if x != "" {
				t = append(t, x)
			}
		}
		if len(t) == 0 {
			continue
		}
		n++
		cands := [][]string{t[:1]}
		if len(t) >= 2 {
			cands = append(cands, t[:2])
		}
		for _, cand := range cands {
			k := strings.ToLower(strings.Join(cand, " "))
			count[k]++
			if _, ok := disp[k]; !ok {
				disp[k] = strings.Join(cand, " ")
			}
		}
	}
	best, bc := "", 0
	for k, c := range count {
		if c < 2 || c*2 < n || len([]rune(k)) < 4 {
			continue
		}
		// prefer the more frequent; on ties the longer prefix
		if c > bc || (c == bc && len(k) > len(best)) {
			best, bc = k, c
		}
	}
	if best == "" {
		return ""
	}
	return disp[best]
}

type scanCtx struct {
	settings  Settings
	overrides map[string]string
	assets    map[string]*Asset
	alt       map[string]string // name key → booth key, so id-less copies merge into the booth asset
	order     []string
	warnings  []string
	now       int64
}

func pathKey(p string) string { return strings.ToLower(filepath.Clean(p)) }

func isUnityProject(dir string) bool {
	if st, err := os.Stat(filepath.Join(dir, "ProjectSettings")); err == nil && st.IsDir() {
		if st2, err := os.Stat(filepath.Join(dir, "Assets")); err == nil && st2.IsDir() {
			return true
		}
	}
	return false
}

// RunFolderScan walks all roots and rebuilds the asset list (keeping usage info from the previous run).
func RunFolderScan(st *Store, prog *Task) {
	st.mu.RLock()
	settings := st.Settings
	ov := map[string]string{}
	for k, v := range st.Overrides {
		ov[k] = v
	}
	st.mu.RUnlock()

	ctx := &scanCtx{settings: settings, overrides: ov, assets: map[string]*Asset{}, alt: map[string]string{}, now: time.Now().Unix()}
	for i, root := range settings.Roots {
		prog.Set(i, len(settings.Roots), "扫描 "+root)
		fi, err := os.Stat(root)
		if err != nil || !fi.IsDir() {
			ctx.warnings = append(ctx.warnings, "找不到素材文件夹："+root)
			continue
		}
		ctx.scanContainer(root, root, 0, nil)
	}

	// finalize
	list := make([]*Asset, 0, len(ctx.order))
	for _, k := range ctx.order {
		a := ctx.assets[k]
		finalizeAsset(a, settings)
		list = append(list, a)
	}

	st.mu.Lock()
	old := map[string]*Asset{}
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
		st.userFor(a) // migrates alt keys
	}
	st.Assets = list
	if st.ScanStart == 0 {
		st.ScanStart = ctx.now
	}
	applyPurchases(st)
	st.Warnings = ctx.warnings
	st.LastScan = ctx.now
	st.mu.Unlock()
	prog.Set(len(settings.Roots), len(settings.Roots), "完成")
}

type group struct {
	key   string
	dirs  []string
	files []string
}

func (c *scanCtx) scanContainer(dir, root string, depth int, hints []string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		c.warnings = append(c.warnings, "无法读取："+dir)
		return
	}
	groups := map[string]*group{}
	var order []string
	add := func(name string, full string, isDir bool) {
		k := normKey(stripArchiveExt(name))
		if isDir {
			k = normKey(name)
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
			if looseAssetExt[lowerExt(name)] || strings.HasSuffix(strings.ToLower(name), ".tar.gz") {
				add(name, full, false)
			}
		}
	}
	for _, k := range order {
		g := groups[k]
		if c.overrides[pathKey(firstOf(g))] == "ignore" {
			continue
		}
		if len(g.dirs) > 0 {
			d := g.dirs[0]
			mode := c.overrides[pathKey(d)]
			if isUnityProject(d) {
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
	if boothIDFromName(name) != "" {
		return false
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	pk := normKey(cleanName(name))
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
			k, base = normKey(n), n
			dirItems[k] = true
		} else {
			ext := lowerExt(n)
			if strongFileExt[ext] {
				return false
			}
			if archiveExt[ext] || strings.HasSuffix(strings.ToLower(n), ".tar.gz") {
				base = stripArchiveExt(n)
				k = normKey(base)
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
		if boothIDFromName(base) != "" {
			idChildren++
		}
		if isCategoryWordName(base) {
			genericChildren++
		}
		if isStructuralName(base, pk) {
			structural++
		}
	}
	if len(items) < 2 || structural*2 >= len(items) {
		return false
	}
	evidence := idChildren > 0 || genericChildren >= 2 || isCategoryWordName(name) || reCollection.MatchString(strings.ToLower(name))
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

// isCategoryWordName: a folder named only by category words ("衣服", "头发", "更新归总"), not just digits.
func isCategoryWordName(n string) bool {
	k := regexp.MustCompile(`[0-9]+`).ReplaceAllString(normKey(n), "")
	return k != "" && reGenericKey.MatchString(k)
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
		rawName = stripArchiveExt(rawName)
	}
	a := &Asset{RawName: rawName, Hints: hints}
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
		a.Locations = append(a.Locations, Location{Path: d, Kind: "dir", Size: info.size, Root: root})
	}
	for _, f := range g.files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		k := kindOf(f, false)
		a.Locations = append(a.Locations, Location{Path: f, Kind: k, Size: fi.Size(), Root: root})
		if !a.HasDir {
			a.Size += fi.Size()
			a.Files++
		}
		if fi.ModTime().Unix() > a.MTime {
			a.MTime = fi.ModTime().Unix()
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
		if id := boothIDFromName(n); id != "" {
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
	a.Name = cleanName(rawName)
	// wrapper folders like "4016/7825319/…" or "材质/_Material_X.zip": prefer the inner, more descriptive name
	if (isMostlyDigits(a.Name) || isGenericName(a.Name)) && len(names) > 1 {
		for _, n := range names[1:] {
			if cn := cleanName(n); !isMostlyDigits(cn) && !isGenericName(cn) {
				a.Name = cn
				break
			}
		}
	}
	// a generic folder name ("衣服头发") whose children share a product name
	if a.BoothID == "" && isGenericName(a.Name) && len(topNames) > 0 {
		if p := commonChildPrefix(topNames); p != "" {
			a.Name = p + "（" + a.Name + "）"
		}
	}
	keySrc := rawName
	if isGenericName(cleanName(rawName)) && !isGenericName(a.Name) && !strings.Contains(a.Name, "（") {
		keySrc = a.Name // "材质/_Material_X.zip" is identified by the inner name
	}
	nk := mergeName(keySrc)
	nameKey := "name:" + nk
	if (isASCII(nk) && len(nk) < 6) || len([]rune(nk)) < 3 || isGenericName(cleanName(keySrc)) {
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

func isMostlyDigits(s string) bool {
	d := 0
	n := 0
	for _, r := range s {
		if r == ' ' || r == '_' || r == '-' {
			continue
		}
		n++
		if r >= '0' && r <= '9' {
			d++
		}
	}
	return n == 0 || d*2 >= n
}

func mergeAsset(dst, src *Asset) {
	dst.Locations = append(dst.Locations, src.Locations...)
	dst.Packages = append(dst.Packages, src.Packages...)
	dst.Archives = append(dst.Archives, src.Archives...)
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
	covers       []string
	metaDirs     []string
	urlIDs       map[string]int
	wrapperNames []string
	topNames     []string
	psds         []PSDFile
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
				groups[normKey(n)] = true
				onlyDir = n
			} else if archiveExt[lowerExt(n)] || strings.HasSuffix(ln, ".unitypackage") || strings.HasSuffix(ln, ".tar.gz") {
				groups[normKey(stripArchiveExt(n))] = true
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
		info.wrapperNames = append(info.wrapperNames, stripArchiveExt(onlyFile))
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
		ext := lowerExt(name)
		depth := strings.Count(filepath.Clean(p), string(os.PathSeparator)) - baseDepth
		switch {
		case ext == ".unitypackage":
			info.packages = append(info.packages, p)
		case ext == ".zip":
			info.archives = append(info.archives, p)
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
				for _, m := range reBoothURL.FindAllStringSubmatch(string(b), -1) {
					if !depBoothIDs[m[1]] {
						info.urlIDs[m[1]]++
					}
				}
			}
		case psdExt[ext]:
			info.psdCount++
			if len(info.psds) < 40 {
				pf := PSDFile{Path: p, Size: fi.Size()}
				if ext == ".psd" || ext == ".psb" {
					pf.W, pf.H = psdDims(p)
				}
				info.psds = append(info.psds, pf)
			}
		case modelExt[ext]:
			info.models++
		case imageExt[ext] && depth <= 3 && fi.Size() > 8*1024 && fi.Size() < 15*1024*1024:
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

type catRule struct {
	cat string
	re  *regexp.Regexp
}

var catRules = []catRule{
	{"面捕", regexp.MustCompile(`facetracking|face[ _-]?tracking|面捕|triturbo|面部追踪`)},
	{"字体", regexp.MustCompile(`\.ttf|\.otf|字体|font`)},
	{"插件", regexp.MustCompile(`pcss|插件|tool|ツール|系统|system|toolkit|(^|[^a-z])sps([^a-z]|$)|(^|[^a-z])dps|(^|[^a-z])pcs([^a-z]|$)|marshmallow|棉花糖|light ?controller|亮度|计数|counter|手势|gesture|icon ?generator|亲吻|kiss|碰撞|modular|かんたん|(^|[^a-z])erp|insertial|bulge|framework`)},
	{"动作", regexp.MustCompile(`pose|姿势|动作|motion|emote|(^|[^a-z])afk|待机|ポーズ|モーション`)},
	{"音效", regexp.MustCompile(`音效|sound|(^|[^a-z])se([^a-z]|$)|ボイス|voice|音声`)},
	{"头发", regexp.MustCompile(`hair|头发|发型|髪|ヘア|twin[ _-]?tails?|pony[ _-]?tails?|ツインテ|ポニテ|braids?([^a-z]|$)|(^|[^a-z])bob([^a-z]|$)`)},
	{"材质", regexp.MustCompile(`材质|material|texture|テクスチャ|skin|肌|丝袜|stocking|gradation|emission|mask|makeup|妆容|メイク|blush|psd|shader`)},
	{"衣服", regexp.MustCompile(`衣服|cloth|dress|school|sailor|(^|[^a-z])jk([^a-z]|$)|outfit|衣装|uniform|制服|skirt|コート|パーカー|wear|水着|bikini|lingerie|内衣|dress|衣装|ドレス|ワンピ|ジャケット|セーター|下着|ランジェリー|ブーツ|ソックス|タイツ|ニーハイ|コスチューム|outfit`)},
	{"配饰", regexp.MustCompile(`(^|[^a-z])(gloves?|rings?|earrings?|ears?|hats?|ribbons?|chokers?|necklaces?|glasses|tails?|wings?|halo|accessor[a-z]*)([^a-z]|$)|手套|耳环|アクセ|帽|眼镜|尾巴|翅膀|耳朵|饰品|配饰|尻尾|しっぽ|ケモミミ|リボン|ピアス|ネックレス|チョーカー|メガネ|帽子|ヘアピン|髪飾り|ブレスレット`)},
	{"道具", regexp.MustCompile(`道具|(^|[^a-z])(props?|guns?|rifles?|weapons?|pets?|items?)([^a-z]|$)|枪|狙击|小猪|叠叠乐|武器|宠物`)},
	{"素体", regexp.MustCompile(`素体|オリジナル3dモデル|avatar ?base|^(plum|chocolat|chiffon|lime|kaguya|manuka|karin|shinano|rusk|mamehinata|lasyusha|kikyo|selestia|airi|maya|uzuki|rindo|milltina|mizuki)([ _-]?v?[0-9][0-9.]*)?$|模型$|オリジナル3d`)},
}

var boothCatMap = map[string]string{
	"3Dキャラクター": "素体", "3D衣装": "衣服", "3D装飾品": "配饰", "3D小道具": "道具", "3Dテクスチャ": "材质",
	"3Dツール・システム": "插件", "3Dモーション・アニメーション": "动作", "3D環境・ワールド": "其他", "3Dモデル（その他）": "其他",
	"ボイス・ASMR": "音效", "素材（音声）": "音效", "フォント": "字体", "ソフトウェア": "插件",
}

var bracketTags = []struct{ cat, words string }{
	{"面捕", "面捕"}, {"字体", "字体"}, {"插件", "插件|系统|工具|ツール"}, {"动作", "动作|姿势"}, {"音效", "音效"},
	{"头发", "头发|发型|髪型|ヘア"}, {"衣服", "衣服|服装|衣装"}, {"配饰", "配饰|饰品|アクセ"}, {"道具", "道具"},
	{"材质", "材质|妆容|纹理|贴图|メイク"}, {"素体", "素体|模型"},
}
var reBracketTag = regexp.MustCompile(`[【\[]([^】\]]{1,12})[】\]]`)

// bracketCategory reads an explicit tag such as "【衣服】" or "【插件ERP】" from a name.
func bracketCategory(name string) string {
	for _, m := range reBracketTag.FindAllStringSubmatch(name, -1) {
		t := strings.ToLower(m[1])
		for _, bt := range bracketTags {
			for _, w := range strings.Split(bt.words, "|") {
				if strings.Contains(t, w) {
					return bt.cat
				}
			}
		}
	}
	return ""
}

var Categories = []string{"素体", "衣服", "头发", "配饰", "道具", "材质", "面捕", "插件", "动作", "音效", "字体", "其他"}

func classify(text string) string {
	t := strings.ToLower(text)
	for _, r := range catRules {
		if r.re.MatchString(t) {
			return r.cat
		}
	}
	return "其他"
}

type baseDef struct {
	name   string
	ascii  []*regexp.Regexp
	words  []string // ascii[i] matches words[i] as a whole word; checked with Contains first
	others []string
}

// parsed tables are cached: they are read for every card on every refresh
var defsCache sync.Map

func parseBases(list []string) []baseDef {
	key := "b\x00" + strings.Join(list, "\n")
	if v, ok := defsCache.Load(key); ok {
		return v.([]baseDef)
	}
	out := parseBasesNow(list)
	defsCache.Store(key, out)
	return out
}

func parseBasesNow(list []string) []baseDef {
	var out []baseDef
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, aliases := item, item
		if i := strings.Index(item, "="); i > 0 {
			name, aliases = strings.TrimSpace(item[:i]), item[i+1:]
		}
		bd := baseDef{name: name}
		for _, al := range strings.Split(aliases+"|"+name, "|") {
			al = strings.TrimSpace(strings.ToLower(al))
			if al == "" {
				continue
			}
			if isASCII(al) {
				bd.words = append(bd.words, al)
				bd.ascii = append(bd.ascii, regexp.MustCompile(`(^|[^a-z])`+regexp.QuoteMeta(al)+`([^a-z]|$)`))
			} else {
				bd.others = append(bd.others, al)
			}
		}
		out = append(out, bd)
	}
	return out
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func detectBases(text string, defs []baseDef) []string {
	t := strings.ToLower(text)
	var out []string
	for _, d := range defs {
		hit := false
		for i, re := range d.ascii {
			if strings.Contains(t, d.words[i]) && re.MatchString(t) {
				hit = true
				break
			}
		}
		if !hit {
			for _, o := range d.others {
				if strings.Contains(t, o) {
					hit = true
					break
				}
			}
		}
		if hit {
			out = append(out, d.name)
		}
	}
	return out
}

func assetText(a *Asset) string {
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

func finalizeAsset(a *Asset, s Settings) {
	a.Hints = uniqStrings(a.Hints)
	a.Packages = uniqStrings(a.Packages)
	a.Archives = uniqStrings(a.Archives)
	a.Covers = uniqStrings(a.Covers)
	a.MetaDirs = uniqStrings(a.MetaDirs)
	text := assetText(a)
	// a lone font file
	if len(a.Locations) == 1 {
		if e := lowerExt(a.Locations[0].Path); e == ".ttf" || e == ".otf" {
			a.Category = "字体"
		}
	}
	if a.Category == "" {
		a.Category = bracketCategory(a.RawName)
	}
	if a.Category == "" {
		// own name first, then the nearest category folder it sits in ("衣服/侠客服"), then everything else
		a.Category = classify(a.Name)
		if a.Category == "其他" {
			for i := len(a.Hints) - 1; i >= 0; i-- {
				if isCategoryWordName(a.Hints[i]) {
					if c := classify(a.Hints[i]); c != "其他" {
						a.Category = c
						break
					}
				}
			}
		}
		if a.Category == "其他" {
			a.Category = classify(text)
		}
	}
	a.Bases = detectBases(text, parseBases(s.Bases))
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

func uniqStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		k := strings.ToLower(s)
		if s == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}
