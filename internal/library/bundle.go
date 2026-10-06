package library

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"vrclib/internal/archive"
	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/netdisk"
)

// A seller's netdisk share often holds one archive with many archives in it: one for each product, or one for
// each category with the products' archives inside those. Unpacked, that is a folder the scan takes for one
// asset, and importing that asset would put every product into the Unity project. This file tells such a
// collection (合集包) from a product by what lies in its folders, and marks its folder levels as containers
// (the override "bundle"), so that the scan makes a card for each product. The scan's own guess at containers
// (isContainer) is left as it is: nothing here changes how a library without these marks is scanned.

// BundleInfo: what a folder holds when it is a collection of products (合集包).
type BundleInfo struct {
	Containers []string // the folders to scan as containers, outermost first: wrappers and every collection level
	Products   int      // the product entries found under them, those still inside archives too
	Packed     bool     // some of it is still inside archives that have to be unpacked to get at the products
}

// The work for one answer is bounded: the scan asks it of every card that could be a collection.
const (
	bundleDepth   = 6    // folder levels below the top that are looked into
	bundleHolds   = 3    // … and how far down a folder is searched for a package or an archive of its own
	bundleEntries = 2000 // entries of one folder that are read
	bundleReads   = 3000 // folders read
	bundleZips    = 16   // zips looked into
)

var (
	// a name that only says which format or platform a version of a product is for
	reFormatName = regexp.MustCompile(`^(for)?((vrchat|vrc|vrm|unity|quest|pc|android|ios|mmd|blender|physbones?|pb|dynamicbones?|db)(用|版|対応|ver(sion)?)?)+$`)
	// what a name that only says a base body may still hold: "Plum_v2", "Kaguya用", "for Shinano"
	reBaseFiller = regexp.MustCompile(`^(for|用|専用|专用|版|向け|対応|对应|适配|ver(sion)?|v)*$`)
	reDigitRun   = regexp.MustCompile(`[0-9]+`)
)

// Unpacked: what ScanBundle and MarkBundles are told of an unpack that has just run — the folders it made, and
// the archives it made them from: where those were kept, they are no packed things of their own any more.
func Unpacked(done, from []string) map[string]bool {
	made := make(map[string]bool, len(done)+len(from))
	for _, p := range done {
		made[core.PathKey(p)] = true
	}
	for _, p := range from {
		made[core.PathKey(p)] = true
	}
	return made
}

// MarkBundles looks at what is under top and, when it is a collection, marks its folder levels ("bundle"), so
// that the next scan makes a card for each product. A folder the player has decided about ("split", "asset",
// "ignore") keeps that; under a folder kept as one asset nothing is marked. Locks the store itself.
func MarkBundles(st *core.Store, top string, made map[string]bool) BundleInfo {
	st.Mu.RLock()
	defs := naming.ParseBases(st.Settings.Bases)
	ov := make(map[string]string, len(st.Overrides))
	for k, v := range st.Overrides {
		ov[k] = v
	}
	st.Mu.RUnlock()
	if keptWhole(ov, top) {
		return BundleInfo{}
	}
	info := ScanBundle(top, made, defs, ov)
	if len(info.Containers) == 0 {
		return info
	}
	marked, levels := 0, map[int]bool{}
	st.Mu.Lock()
	for _, d := range info.Containers {
		levels[strings.Count(filepath.Clean(d), string(os.PathSeparator))] = true
		if k := core.PathKey(d); st.Overrides[k] == "" {
			st.Overrides[k] = "bundle"
			marked++
		}
	}
	st.Mu.Unlock()
	if marked > 0 {
		_ = st.Save()
	}
	core.Logf("合集包 %s：%d 个素材，%d 层文件夹按容器扫描（新标记 %d 个文件夹）", top, info.Products, len(levels), marked)
	return info
}

// ForgetBundles takes the "bundle" marks below dir away, after dir's own has gone: one 恢复 puts a whole
// collection back into one card. Caller holds st.Mu.
func ForgetBundles(st *core.Store, dir string) {
	under := core.PathKey(dir) + string(os.PathSeparator)
	for k, mode := range st.Overrides {
		if mode == "bundle" && strings.HasPrefix(k, under) {
			delete(st.Overrides, k)
		}
	}
}

// OpenWrappers: the player split a folder that only wraps another one — a download's folder, with the one folder
// its archive unpacked to in it. The split is meant for what lies inside: the folders that only wrap are marked
// as levels too, down to the first one that holds more than a single folder, so that one 「拆分为多个素材」 opens
// something. A folder the player has decided about otherwise is left as it is, and nothing below it is marked.
// Locks the store itself.
func OpenWrappers(st *core.Store, dir string) {
	marked := false
	st.Mu.Lock()
	for i := 0; i < bundleDepth; i++ {
		l := readBundleDir(dir)
		if l == nil || l.project || l.strong || len(l.packages)+len(l.archives) > 0 || len(l.dirs) != 1 {
			break
		}
		dir = filepath.Join(dir, l.dirs[0])
		k := core.PathKey(dir)
		if mode := st.Overrides[k]; mode == "" {
			st.Overrides[k], marked = "bundle", true
		} else if mode != "bundle" && mode != "split" {
			break
		}
	}
	st.Mu.Unlock()
	if marked {
		_ = st.Save()
	}
}

// KeepUnpacked: where an archive the player keeps as one asset ("asset") has been unpacked, the folder made
// of it is kept as one asset too — the decision is about the asset, whatever form it has on the disk — and
// goes with it when the archive is gone. Locks the store itself.
func KeepUnpacked(st *core.Store, done, from []string) {
	changed := false
	st.Mu.Lock()
	for i, d := range done {
		if i >= len(from) || st.Overrides[core.PathKey(from[i])] != "asset" {
			continue
		}
		if k := core.PathKey(d); st.Overrides[k] == "" {
			st.Overrides[k], changed = "asset", true
		}
		if !core.FileExists(from[i]) {
			delete(st.Overrides, core.PathKey(from[i]))
			changed = true
		}
	}
	st.Mu.Unlock()
	if changed {
		_ = st.Save()
	}
}

// keptWhole: the player said that this folder, or one it lies in, is one asset.
func keptWhole(ov map[string]string, p string) bool {
	for k := core.PathKey(p); ; {
		if ov[k] == "asset" {
			return true
		}
		up := filepath.Dir(k)
		if up == k {
			return false
		}
		k = up
	}
}

// ScanBundle looks at what is under top. made: the folders an unpack has just made out of archives (nil when
// not known), as Unpacked puts them together. overrides: what has been decided about folders (Store.Overrides)
// — one kept as an asset is never a level of a collection, nothing is looked for in one the scan skips, and
// one that is scanned as a container already ("bundle", "split") is a level whatever it looks like.
func ScanBundle(top string, made map[string]bool, defs []naming.BaseDef, overrides map[string]string) BundleInfo {
	if mode := overrides[core.PathKey(top)]; mode == "asset" || mode == "ignore" {
		return BundleInfo{}
	}
	s := &bundleScan{made: made, defs: defs, ov: overrides, lists: map[string]*bundleList{}}
	var f bundleFound
	if core.LowerExt(top) == ".zip" && core.FileExists(top) {
		f.packed = s.zipInside(top)
	} else {
		f = s.scan(top, 0, false, false)
	}
	if f.products+f.packed < 2 {
		return BundleInfo{}
	}
	depth := func(p string) int { return strings.Count(filepath.Clean(p), string(os.PathSeparator)) }
	sort.SliceStable(s.containers, func(i, j int) bool { return depth(s.containers[i]) < depth(s.containers[j]) })
	return BundleInfo{Containers: s.containers, Products: f.products + f.packed, Packed: f.packed > 0}
}

type bundleScan struct {
	made       map[string]bool
	defs       []naming.BaseDef
	ov         map[string]string
	lists      map[string]*bundleList // the folders read so far
	reads      int
	zips       int
	containers []string
}

// bundleList: what lies right in a folder, as far as telling a collection goes.
type bundleList struct {
	dirs     []string // its folders
	packages []string // its unitypackages
	archives []string // its archives, every volume of them
	strong   bool     // a model, a prefab, a PSD … lies loose in it: one product's own files
	project  bool     // a Unity project: no asset
}

// bundleEntry: the things of one name in a folder — "X/", "X.zip" and the volumes of one archive are one
// entry, as they are one group to the scan.
type bundleEntry struct {
	name   string // the folder's name, else the file's without what makes it an archive
	dirs   []string
	files  []string
	packed bool // it is, or has with it, an archive that is still packed
}

func (e *bundleEntry) first() string {
	if len(e.dirs) > 0 {
		return e.dirs[0]
	}
	return e.files[0]
}

func (e *bundleEntry) in(set map[string]bool) bool {
	if len(set) == 0 {
		return false
	}
	for _, p := range append(append([]string{}, e.dirs...), e.files...) {
		if set[core.PathKey(p)] {
			return true
		}
	}
	return false
}

// bundleLike: an entry that could be a product of its own.
type bundleLike struct {
	name string
	// an archive that came out of an archive: it lies in what an unpack has just made, and is an archive
	// that is still packed or a folder that unpack made of one
	born   bool
	packed bool // it holds a collection that is still packed: counted there, not as one product
	// the products it stands for where its folder is a level of a collection: one — or, for a folder whose
	// name says nothing and that holds several products (a category with two or three), each of those:
	// the scan takes such a folder for a category by itself
	n int
}

// bundleFound: what a folder turned out to be.
type bundleFound struct {
	container bool
	products  int          // the cards of its products, once it is scanned as a container
	packed    int          // products below it that are still inside archives
	likes     []bundleLike // what could be a product of its own in it, when it is no container
}

func (s *bundleScan) list(dir string) *bundleList {
	if l, ok := s.lists[dir]; ok {
		return l
	}
	var l *bundleList
	if s.reads < bundleReads && !core.Quitting.Load() {
		s.reads++
		l = readBundleDir(dir)
	}
	s.lists[dir] = l
	return l
}

func readBundleDir(dir string) *bundleList {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	ents, _ := f.ReadDir(bundleEntries) // (what was read counts, also when the read ends early)
	f.Close()
	l := &bundleList{}
	assets, settings := false, false
	for _, e := range ents {
		name := e.Name()
		low := strings.ToLower(name)
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "__MACOSX") || strings.HasSuffix(low, ".extracting") {
			continue
		}
		if e.IsDir() { // (a link to a folder is none: it is not followed out of the tree)
			if !skipDirNames[low] {
				l.dirs = append(l.dirs, name)
				assets, settings = assets || low == "assets", settings || low == "projectsettings"
			}
			continue
		}
		switch ext := core.LowerExt(name); {
		case ext == ".unitypackage":
			l.packages = append(l.packages, name)
		case ext == ".zip" || ext == ".rar" || ext == ".7z":
			l.archives = append(l.archives, name)
		case naming.StrongFileExt[ext]:
			l.strong = true
		case len(ext) == 4 && ext[3] >= '0' && ext[3] <= '9' && archive.IsArchiveFile(name): // a volume: ".001", ".z01", ".r00"
			l.archives = append(l.archives, name)
		}
	}
	sort.Strings(l.dirs)
	sort.Strings(l.packages)
	sort.Strings(l.archives)
	l.project = assets && settings
	return l
}

func (s *bundleScan) entries(dir string, l *bundleList) []*bundleEntry {
	byKey := map[string]*bundleEntry{}
	var out []*bundleEntry
	entry := func(stem string) *bundleEntry {
		k := naming.NormKey(stem)
		if k == "" {
			k = strings.ToLower(stem)
		}
		e := byKey[k]
		if e == nil {
			e = &bundleEntry{name: stem}
			byKey[k] = e
			out = append(out, e)
		}
		return e
	}
	for _, n := range l.dirs {
		e := entry(n)
		e.dirs = append(e.dirs, filepath.Join(dir, n))
	}
	for _, n := range l.packages {
		e := entry(core.StripArchiveExt(n))
		e.files = append(e.files, filepath.Join(dir, n))
	}
	for _, n := range l.archives {
		stem, _, _ := archive.VolumeName(n)
		e := entry(stem)
		e.files, e.packed = append(e.files, filepath.Join(dir, n)), true
	}
	return out
}

// holds: is there a unitypackage or an archive in dir, at most levels folders down?
func (s *bundleScan) holds(dir string, levels int) bool {
	l := s.list(dir)
	if l == nil || l.project {
		return false
	}
	if len(l.packages)+len(l.archives) > 0 {
		return true
	}
	if levels > 1 {
		for _, d := range l.dirs {
			if s.holds(filepath.Join(dir, d), levels-1) {
				return true
			}
		}
	}
	return false
}

// scan reads one folder. It is a container — a level of a collection — when
//
//	C  one of its folders is a container (a product does not hold collections; a folder that only wraps a
//	   container is the one-entry case of this);
//	A  it lies in what an unpack has just made, and at least two of its entries that could be products of
//	   their own are archives — still packed, or made into folders by that unpack — and say different
//	   products: an archive that held the archives of several products. (Archives that lie side by side in
//	   a folder that was there before say less: a product and what it needs come like that, too.)
//	B  at least four different products are among the entries that could be products of their own (the
//	   structure alone, for what was unpacked long ago);
//	E  its own name says collection (合集, 全家桶 …) and at least two entries could be products of their own.
//
// An entry could be a product of its own when it is or holds a unitypackage or an archive, and its name does
// not say "a part of a product" (partOf). A folder with a model, a prefab or a PSD lying loose in it is one
// product's own, and so is one named by a Booth item number: neither is a container, whatever else it holds.
// Nor is a folder that is named as products name their own folders ("Textures", "Shaders", "特典", "Unity"):
// what lies in it is the product's, and it is not looked into.
//
// A folder that is scanned as a container already — marked at an earlier download, or split by the player —
// is one without any of this, and everything in it that holds a package or an archive is a card of its own.
//
// named: the caller wants to know what could be a product in it also when it is no container (likes).
// fresh: dir lies in a folder an unpack has just made.
func (s *bundleScan) scan(dir string, depth int, named, fresh bool) bundleFound {
	var f bundleFound
	l := s.list(dir)
	mode := s.mode(dir)
	fresh = fresh || (len(s.made) > 0 && s.made[core.PathKey(dir)])
	marked := mode == "bundle" || mode == "split"
	if l == nil || l.project || (!marked && (l.strong || naming.BoothIDFromName(filepath.Base(dir)) != "")) {
		return f
	}
	var ents []*bundleEntry
	for _, e := range s.entries(dir, l) {
		// (an archive this unpack made a folder of another name from: the folder stands for it)
		if s.mode(e.first()) != "ignore" && (len(e.dirs) > 0 || !e.in(s.made)) {
			ents = append(ents, e)
		}
	}
	// a zip that is all the folder holds is looked into: the one packed archive of a collection
	if len(ents) == 1 && len(ents[0].dirs) == 0 && len(ents[0].files) == 1 && core.LowerExt(ents[0].files[0]) == ".zip" {
		f.packed += s.zipInside(ents[0].files[0])
	}
	// what is or holds a package or an archive: only that can be a product, or a level of the collection
	var cands []bundleLike
	born := 0
	for _, e := range ents {
		c := bundleLike{name: e.name, born: fresh && (e.packed || e.in(s.made)), n: 1}
		holds := len(e.files) > 0
		if len(e.dirs) > 0 {
			d := e.dirs[0]
			holds = s.holds(d, bundleHolds) || holds
			// (a folder a product has of its own — "Shaders" with four shaders in it — is no level of a
			// collection, unless it is scanned as a container already)
			if mode := s.mode(d); mode != "asset" && depth < bundleDepth && (holds || s.wraps(d)) && (mode != "" || !s.ownFolder(e.name)) {
				plain := s.plain(e.name)
				sub := s.scan(d, depth+1, plain, fresh)
				f.packed += sub.packed
				if sub.container {
					f.container, f.products = true, f.products+sub.products
					continue
				}
				c.packed = sub.packed >= 2
				// a folder whose name says nothing ("3", "衣服") goes by the one product it holds
				if plain && len(sub.likes) == 1 {
					c.name, c.born = sub.likes[0].name, c.born || sub.likes[0].born
				} else if plain && s.different(likeNames(sub.likes), 2) {
					c.n = len(sub.likes)
				}
			}
		}
		if holds {
			cands = append(cands, c)
			if c.born {
				born++
			}
		}
	}
	// The names are read only where they can decide something (reading them is most of the work): not in a
	// product's folder with a package, a PSD pack and nothing else.
	collection := len(cands) >= 2 && naming.ReCollection.MatchString(strings.ToLower(filepath.Base(dir)))
	if !marked && !f.container && !named && born < 2 && len(cands) < 4 && !collection {
		return f
	}
	likes, categories := cands, 0
	if !marked {
		parent := s.nameKey(filepath.Base(dir))
		likes = nil
		for _, c := range cands {
			if !s.partOf(c.name, parent) {
				likes = append(likes, c)
			} else if c.n > 1 {
				categories += c.n // no product by its name, and no level by the rules: counted, where dir is one
			}
		}
	}
	var fromArchives []string
	for _, x := range likes {
		if x.born {
			fromArchives = append(fromArchives, x.name)
		}
	}
	names := likeNames(likes)
	f.container = marked || f.container || s.different(fromArchives, 2) || s.different(names, 4) || (collection && len(names) >= 2)
	if !f.container {
		f.likes = likes
		return f
	}
	f.products += categories
	for _, x := range likes {
		if !x.packed {
			f.products += x.n
		}
	}
	s.containers = append(s.containers, dir)
	return f
}

func likeNames(likes []bundleLike) []string {
	names := make([]string, len(likes))
	for i, x := range likes {
		names[i] = x.name
	}
	return names
}

// wraps: the folder holds one folder and nothing else that counts — a wrapper is looked through however
// deep the first package lies.
func (s *bundleScan) wraps(dir string) bool {
	l := s.list(dir)
	return l != nil && len(l.packages)+len(l.archives) == 0 && len(l.dirs) == 1
}

// mode: what has been decided about a folder or a file ("" when nothing).
func (s *bundleScan) mode(p string) string {
	if len(s.ov) == 0 {
		return ""
	}
	return s.ov[core.PathKey(p)]
}

// zipInside looks into a zip without unpacking it (its central directory only): when its top level — inside
// a folder that wraps everything, too — holds the archives of several products, it is a collection that is
// still packed, and those are its products. 0 when it is none, or when it cannot be read; a rar or a 7z
// never can, and is one product each. 0 also for a zip whose folder lies beside it (the archive was kept
// when it was unpacked): what it holds is looked at there.
func (s *bundleScan) zipInside(zipPath string) int {
	if s.zips >= bundleZips || core.Quitting.Load() {
		return 0
	}
	s.zips++
	files, err := archive.ZipEntries(zipPath)
	if err != nil {
		return 0
	}
	var names []string
	for _, f := range files {
		n := path.Clean("/" + f.Name)[1:]
		if n != "" && !strings.HasPrefix(n, "__MACOSX") && !strings.HasPrefix(path.Base(n), ".") {
			names = append(names, n)
		}
	}
	stem := core.StripArchiveExt(filepath.Base(zipPath))
	beside := []string{stem} // the folder an unpack makes of it: named as the zip, or as the one folder in it
	parent := s.nameKey(stem)
	for i := 0; i < 3 && len(names) > 0; i++ { // a folder that wraps everything
		top, _, _ := strings.Cut(names[0], "/")
		wraps := true
		for _, n := range names {
			wraps = wraps && strings.HasPrefix(n, top+"/")
		}
		if !wraps {
			break
		}
		for j := range names {
			names[j] = names[j][len(top)+1:]
		}
		if k := s.nameKey(top); k != "" {
			parent = k
		}
		if i == 0 {
			beside = append(beside, top)
		}
	}
	for _, n := range beside {
		if core.IsDir(filepath.Join(filepath.Dir(zipPath), core.SafeName(n, 120))) {
			return 0
		}
	}
	seen := map[string]bool{}
	var arcs []string
	for _, n := range names {
		if strings.Contains(n, "/") {
			continue
		}
		switch ext := core.LowerExt(n); {
		case archive.IsArchiveFile(n):
			stem, _, _ := archive.VolumeName(n)
			if k := naming.NormKey(stem); !seen[k] {
				seen[k] = true
				if !s.partOf(stem, parent) {
					arcs = append(arcs, stem)
				}
			}
		case naming.StrongFileExt[ext] && ext != ".unitypackage":
			return 0 // one product's own files
		}
	}
	if s.different(arcs, 2) {
		return len(arcs)
	}
	return 0
}

// nameKey: a folder's name as a key, for telling the entries that are named after it ("Dress/Dress_PSD.zip":
// a part of it). "" when the name says nothing of a product: a number, a category word, only a base body.
func (s *bundleScan) nameKey(name string) string {
	n := naming.CleanName(name)
	if naming.IsGenericName(n) || s.onlyBases(n) {
		return ""
	}
	return naming.NormKey(n)
}

// partOf: does the name say "a part of a product", or nothing at all, rather than a product of its own? A
// generic word or a number, a folder of the product's structure or one named after the folder it lies in
// (parent), a PSD, texture, readme or bonus pack, a bare format or platform ("Unity", "PC版", "VRM"), or only a
// base body's name ("Kaguya", "Plum_v2", "for Shinano": one product's versions for several base bodies).
func (s *bundleScan) partOf(name, parent string) bool {
	n := naming.CleanName(name)
	if naming.IsGenericName(n) || netdisk.PanPartKind(name) != "variant" || s.onlyBases(n) || s.ownFolder(name) {
		return true
	}
	return naming.IsStructuralName(n, parent) // named after the folder it lies in
}

// ownFolder: the name is one that products give their own folders and packs — a word of their structure
// ("Textures", "Prefab", "Shaders", "特典", also "Textures_v2"), or a bare format or platform ("Unity", "PC版").
func (s *bundleScan) ownFolder(name string) bool {
	n := naming.CleanName(name)
	bare := reTrailVer.ReplaceAllString(strings.ToLower(n), "")
	return naming.IsStructuralName(n, "") || (bare != "" && naming.IsStructuralName(bare, "")) ||
		reFormatName.MatchString(reDigitRun.ReplaceAllString(naming.NormKey(n), ""))
}

// plain: the name says nothing of what it names — a number, a category word, "新建文件夹" — and nothing of
// being a part either.
func (s *bundleScan) plain(name string) bool {
	n := naming.CleanName(name)
	return naming.IsGenericName(n) && !naming.IsStructuralName(n, "") && netdisk.PanPartKind(name) == "variant"
}

func (s *bundleScan) onlyBases(n string) bool {
	low := strings.ToLower(n)
	rest := withoutBases(low, s.defs)
	return rest != low && reBaseFiller.MatchString(reDigitRun.ReplaceAllString(naming.NormKey(rest), ""))
}

// productKey: what a name says of the product, without the base bodies, the versions and the words for a
// kind of download: the downloads of one product share it.
func (s *bundleScan) productKey(name string) string {
	if k := stemOf(name, s.defs, nil); k != "" {
		return k
	}
	return reDigitRun.ReplaceAllString(naming.NormKey(withoutBases(strings.ToLower(naming.CleanName(name)), s.defs)), "")
}

// different: do these names say at least n products? The versions of one product, and its downloads for
// several base bodies ("Kaguya_Dress", "Plum_Dress"), are one. And they say one product when most of them
// share a product's name ("Dress_Kaguya", "Dress_Plum", "Dress_PSD") — which a base body in front of all of
// them ("Kaguya A", "Kaguya B"), a number or a category word is not.
func (s *bundleScan) different(names []string, n int) bool {
	if len(names) < n {
		return false
	}
	keys := map[string]bool{}
	for _, name := range names {
		keys[s.productKey(name)] = true
	}
	if len(keys) < n {
		return false
	}
	shared := naming.CommonChildPrefix(names)
	if shared == "" {
		return true
	}
	rest := strings.TrimSpace(withoutBases(strings.ToLower(shared), s.defs))
	return naming.IsMostlyDigits(rest) || naming.IsGenericName(rest)
}
