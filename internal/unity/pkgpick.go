package unity

import (
	"fmt"
	"hash/maphash"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

// Which of a card's unitypackages are imported (1.7.13). A product's folder often holds more than one version
// of the same thing: the versions of a package ("Kipfel_1.0.3" … "_1.2.0"), the same package twice (a copy in
// another folder), a PC and a Quest version, the versions for the left and the right hand, with and without a
// feature, a translated copy. Imported one after another, each writes over what the one before wrote — Unity
// keeps an asset it has where it is, under the first one's name — and the last one decides: an older version,
// the other hand. So:
//   - a package that is there twice (name and size) is imported once;
//   - of the versions of one package (its name without the version), the newest;
//   - of a package's PC and Quest versions, the PC one — the Quest one for a project that builds for Quest
//     (Android, iOS);
//   - packages that write other things into most of the same files are alternatives: the best one is ticked
//     and the player is asked; one that writes over a few files of another (an add-on, a face tracking that
//     replaces the model) comes after it.
// What is left out is said in the result (and the log), so that it can be imported by hand.

// pkgVer: the version a package's name gives.
type pkgVer struct {
	date  int      // yyyymmdd, for a date ("Released_250526")
	parts []string // the numbers, for a version ("1", "4", "0")
	pre   string   // "beta.2"
	text  string   // as written ("1.4.0-beta.2", "250526")
}

func (v pkgVer) known() bool { return v.date > 0 || len(v.parts) > 0 }

var (
	// "v1.2", "Ver1.0", "ver.1.4.2", "V1.4.0-beta.2", "v.1.0.1", "Version 2", "v2"; glued to a word only with a
	// dot in it ("SystemV1.2.0", not "Nav2")
	reVerTag = regexp.MustCompile(`(?i)(^|[^a-z0-9]|[a-z])(v(?:er(?:sion)?)?[ ._]?)(\d+(?:\.\d+)*)((?:[-_. ]?(?:alpha|beta|rc|pre|preview)[._-]?\d*)?)`)
	// "_1.0.3", " 2.2.4", "(1.1)"
	reVerDots = regexp.MustCompile(`(?i)(^|[\s_\-(\[【])(\d+(?:\.\d+)+)((?:[-_.]?(?:alpha|beta|rc|pre|preview)[._-]?\d*)?)`)
	// "_250526", "-20250526"
	reVerDate = regexp.MustCompile(`(^|[\s_\-(\[【])(\d{6}|\d{8})($|[\s_\-)\]】])`)
	reSepRun  = regexp.MustCompile(`[\s_\-.]+`)
)

// nameVersion: the version in a package's name (the last one written in it), and the name without it.
func nameVersion(name string) (v pkgVer, rest string) {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	at, end := -1, -1
	pick := func(start, stop int, ver pkgVer) {
		if start > at {
			at, end, v = start, stop, ver
		}
	}
	for _, m := range reVerTag.FindAllStringSubmatchIndex(stem, -1) {
		lead := stem[m[2]:m[3]]
		nums := stem[m[6]:m[7]]
		if lead != "" && unicode.IsLetter(rune(lead[0])) && !strings.Contains(nums, ".") {
			continue // "Nav2": a name, not a version
		}
		pick(m[4], m[1], pkgVer{parts: strings.Split(nums, "."), pre: preOf(stem[m[8]:m[9]]), text: strings.TrimLeft(stem[m[6]:m[1]], " ")})
	}
	for _, m := range reVerDots.FindAllStringSubmatchIndex(stem, -1) {
		pick(m[4], m[1], pkgVer{parts: strings.Split(stem[m[4]:m[5]], "."), pre: preOf(stem[m[6]:m[7]]), text: stem[m[4]:m[1]]})
	}
	for _, m := range reVerDate.FindAllStringSubmatchIndex(stem, -1) {
		if d := dateOf(stem[m[4]:m[5]]); d > 0 {
			pick(m[4], m[5], pkgVer{date: d, text: stem[m[4]:m[5]]})
		}
	}
	if at < 0 {
		return pkgVer{}, stem
	}
	return v, stem[:at] + stem[end:]
}

func preOf(s string) string {
	return strings.ToLower(strings.Trim(s, "-_. "))
}

// dateOf: "250526" → 20250526, "20250526" → 20250526; 0 when it is no date.
func dateOf(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	if len(s) == 6 {
		n += 20000000
	}
	y, m, d := n/10000, n/100%100, n%100
	if y < 2000 || y > 2099 || m < 1 || m > 12 || d < 1 || d > 31 {
		return 0
	}
	return n
}

// cmp: -1, 0, 1; ok is false when the two cannot be compared (a date and a number, or one is unknown).
func (a pkgVer) cmp(b pkgVer) (int, bool) {
	switch {
	case a.date > 0 && b.date > 0:
		return sign(a.date - b.date), true
	case len(a.parts) == 0 || len(b.parts) == 0:
		return 0, false
	}
	for i := 0; i < max(len(a.parts), len(b.parts)); i++ {
		x, y := "0", "0"
		if i < len(a.parts) {
			x = a.parts[i]
		}
		if i < len(b.parts) {
			y = b.parts[i]
		}
		if c := numCmp(x, y, i > 0); c != 0 {
			return c, true
		}
	}
	switch {
	case a.pre == b.pre:
		return 0, true
	case a.pre == "": // a release comes after its prereleases
		return 1, true
	case b.pre == "":
		return -1, true
	}
	return sign(strings.Compare(naturalKey(a.pre), naturalKey(b.pre))), true
}

// numCmp compares two parts of a version: as numbers ("1.10" after "1.9"), and — after the first part, where
// one is written with a leading zero ("1.01", "1.1") — as decimals.
func numCmp(x, y string, frac bool) int {
	if frac && x != y && (strings.HasPrefix(x, "0") || strings.HasPrefix(y, "0")) {
		for len(x) < len(y) {
			x += "0"
		}
		for len(y) < len(x) {
			y += "0"
		}
		return sign(strings.Compare(x, y))
	}
	a, _ := strconv.Atoi(x)
	b, _ := strconv.Atoi(y)
	return sign(a - b)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

var reDigits = regexp.MustCompile(`\d+`)

// naturalKey: numbers padded, so that "beta.10" sorts after "beta.9".
func naturalKey(s string) string {
	return reDigits.ReplaceAllStringFunc(s, func(d string) string { return fmt.Sprintf("%012s", d) })
}

// seriesKey: the name of a package without its version — the versions of one package share it.
func seriesKey(rest string) string {
	return strings.Trim(reSepRun.ReplaceAllString(strings.ToLower(rest), "_"), "_")
}

var (
	platformWords = map[string]bool{"pc": true, "quest": true, "android": true, "mobile": true, "ios": true, "windows": true, "win": true}
	questWords    = map[string]bool{"quest": true, "android": true, "mobile": true, "ios": true}
	fillerWords   = map[string]bool{"ver": true, "version": true, "for": true, "and": true, "versions": true, "用": true, "版": true, "対応": true}
	reWordSplit   = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// pkgWords: the words of a package's name without its version, its platform and the base bodies it names —
// what tells a PC and a Quest version of one package apart is no more than those.
func pkgWords(rest string, defs []naming.BaseDef) (words []string, quest bool) {
	var b strings.Builder
	prev := rune(0)
	for _, r := range rest { // "VerMAMEHINATA" → "Ver MAMEHINATA", "HairPC" → "Hair PC"
		if unicode.IsUpper(r) && unicode.IsLower(prev) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	for _, w := range reWordSplit.Split(strings.ToLower(b.String()), -1) {
		switch {
		case w == "" || fillerWords[w]:
		case questWords[w]:
			quest = true
		case platformWords[w]:
		case isBaseWord(w, defs):
		default:
			words = append(words, w)
		}
	}
	return words, quest
}

// isBaseWord: the word is a base body's name as a whole (not a word that has one in it).
func isBaseWord(w string, defs []naming.BaseDef) bool {
	for _, d := range defs {
		if strings.EqualFold(d.Name, w) {
			return true
		}
		for _, x := range d.Words {
			if x == w {
				return true
			}
		}
		for _, x := range d.Others {
			if x == w {
				return true
			}
		}
	}
	return false
}

// pkgInfo: what is known of one package of a card.
type pkgInfo struct {
	ver    pkgVer
	series string
	words  []string
	quest  bool
	lang   string // "zh", "ja", "en" when the name says it is a translation into that language
	digest *pkgDigest
}

var (
	reLangZh = regexp.MustCompile(`(?i)汉化|漢化|中文|简体|簡体|繁体|繁體|(^|[^a-z])(chs|cht|cn|zh)([^a-z]|$)`)
	reLangJa = regexp.MustCompile(`(?i)日本語|(^|[^a-z])(jp|jpn|ja)([^a-z]|$)`)
	reLangEn = regexp.MustCompile(`(?i)english|(^|[^a-z])(en|eng)([^a-z]|$)`)
)

func pkgLang(name string) string {
	switch {
	case reLangZh.MatchString(name):
		return "zh"
	case reLangJa.MatchString(name):
		return "ja"
	case reLangEn.MatchString(name):
		return "en"
	}
	return ""
}

// ---------- what the packages hold ----------

// pkgDigest: the files of a package, by GUID — where each goes, and a hash of what it is.
type pkgDigest struct {
	paths  map[string]string
	assets map[string]uint64
}

// digestPackage reads a package once through (nothing is kept but the paths and the hashes).
func digestPackage(p string, seed maphash.Seed) (*pkgDigest, error) {
	tr, closeFn, err := openPackage(p)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	d := &pkgDigest{paths: map[string]string{}, assets: map[string]uint64{}}
	var h maphash.Hash
	h.SetSeed(seed)
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		g, part := pkgEntry(hd.Name)
		switch {
		case g == "":
		case part == "pathname":
			b, _ := io.ReadAll(io.LimitReader(tr, 4096))
			line := strings.SplitN(strings.ReplaceAll(string(b), "\r", ""), "\n", 2)[0]
			d.paths[g] = strings.ToLower(strings.TrimSpace(line))
		case part == "asset":
			h.Reset()
			if _, err := io.Copy(&h, tr); err != nil {
				return nil, err
			}
			d.assets[g] = h.Sum64()
		}
	}
	return d, nil
}

// files whose difference makes two packages different things, rather than the same thing with a script of
// another version in it
var weightyExt = map[string]bool{".prefab": true, ".controller": true, ".overridecontroller": true, ".anim": true,
	".asset": true, ".unity": true, ".fbx": true, ".mat": true, ".blend": true, ".obj": true}

// overlap: what b would write over of what a wrote. shared: the GUIDs both have; differ: those of them that
// are another file, or at another place; weighty: … of those, prefabs, controllers, animations, models.
func (a *pkgDigest) overlap(b *pkgDigest) (shared, differ, weighty int) {
	for g, pa := range a.paths {
		pb, ok := b.paths[g]
		if !ok {
			continue
		}
		shared++
		ha, aok := a.assets[g]
		hb, bok := b.assets[g]
		if pa != pb || aok != bok || ha != hb {
			differ++
			if weightyExt[path.Ext(pa)] || weightyExt[path.Ext(pb)] {
				weighty++
			}
		}
	}
	return
}

// ---------- the plan ----------

// PkgSkip: a package of the card that was not imported, and why: "dup" (there twice), "old" (an older version
// of one that was), "quest" (the Quest version of one that was), "pc" (the PC version of one that was, for a
// project that builds for Quest), "alt" (written over by the one that was).
type PkgSkip struct {
	Name string `json:"name"`
	Why  string `json:"why"`
	Of   string `json:"of,omitempty"` // the package it gave way to
}

// pkgPlan: what of a card's packages is imported, or put to the player.
type pkgPlan struct {
	choices []PkgChoice // in the order they are imported
	ask     bool
	why     string // what the player is asked: "bases" (which base body), "overlap" (which of packages that write the same files)
	skipped []PkgSkip
	info    map[string]*pkgInfo // by path
}

// planPackages decides which packages to import. target: the platform the project builds for (ProjectTarget),
// "quest" or "pc" ("" when it is not known). progress is told how far the reading of packages is (that only
// happens when two or more are left that could write over each other).
func planPackages(st *core.Store, pkgs []string, projBases, assetBases []string, target string, progress func(i, n int)) pkgPlan {
	st.Mu.RLock()
	defs := naming.ParseBases(st.Settings.Bases)
	lang := st.Settings.Lang
	st.Mu.RUnlock()
	if lang != "en" && lang != "ja" {
		lang = "zh"
	}
	plan := pkgPlan{info: map[string]*pkgInfo{}}
	// 1. a package that is there twice
	var uniq []string
	seen := map[string]string{}
	for _, p := range pkgs {
		k := strings.ToLower(filepath.Base(p))
		if fi, err := os.Stat(p); err == nil {
			k += "|" + strconv.FormatInt(fi.Size(), 10)
		}
		if first, ok := seen[k]; ok {
			plan.skipped = append(plan.skipped, PkgSkip{Name: filepath.Base(p), Why: "dup", Of: first})
			continue
		}
		seen[k] = p
		uniq = append(uniq, p)
	}
	for _, p := range uniq {
		v, rest := nameVersion(filepath.Base(p))
		words, quest := pkgWords(rest, defs)
		plan.info[p] = &pkgInfo{ver: v, series: seriesKey(rest), words: words, quest: quest, lang: pkgLang(filepath.Base(p))}
	}
	// 2. the base bodies (as before)
	choices, ask := choosePackages(st, uniq, projBases, assetBases)
	plan.choices, plan.ask = choices, ask
	if ask {
		plan.why = "bases"
	}
	// 3. the versions of one package: the newest
	bySeries := map[string][]int{}
	for i, c := range plan.choices {
		if c.Pick {
			k := plan.info[c.Path].series
			bySeries[k] = append(bySeries[k], i)
		}
	}
	for _, idx := range bySeries {
		if len(idx) < 2 {
			continue
		}
		best := -1
		comparable := true
		for _, i := range idx {
			v := plan.info[plan.choices[i].Path].ver
			if !v.known() {
				comparable = false
				break
			}
			if best >= 0 {
				c, ok := v.cmp(plan.info[plan.choices[best].Path].ver)
				if !ok {
					comparable = false
					break
				}
				if c > 0 {
					best = i
				}
			} else {
				best = i
			}
		}
		if !comparable {
			continue // an unknown version among them: what they hold decides (4)
		}
		bv := plan.info[plan.choices[best].Path].ver
		for _, i := range idx {
			c := &plan.choices[i]
			if cv, _ := plan.info[c.Path].ver.cmp(bv); cv == 0 {
				continue // the same version twice (another size): what they hold decides
			}
			c.Pick, c.Why, c.Note = false, "old", "旧版本，已有 "+bv.text
		}
	}
	// … and of a PC and a Quest version, the PC one — the Quest one where the project builds for Quest
	for i := range plan.choices {
		c := &plan.choices[i]
		ci := plan.info[c.Path]
		if !c.Pick {
			continue
		}
		for j, o := range plan.choices {
			oi := plan.info[o.Path]
			if j == i || !o.Pick {
				continue
			}
			if target != "quest" && questOf(ci, oi) {
				c.Pick, c.Why, c.Note = false, "quest", "Quest 版本，已有 PC 版本"
				break
			}
			if target == "quest" && questOf(oi, ci) {
				c.Pick, c.Why, c.Note = false, "pc", "PC 版本，工程的目标平台为 Android（Quest）"
				break
			}
		}
	}
	if plan.ask {
		plan.order()
		return plan
	}
	// 4. what is left: packages that write different things into the same files
	var picked []int
	var total int64
	for i, c := range plan.choices {
		if c.Pick {
			picked = append(picked, i)
			total += c.Size
		}
	}
	if len(picked) < 2 || total > 4<<30 { // (four gigabytes of packages are not read twice)
		plan.order()
		return plan
	}
	seed := maphash.MakeSeed()
	for n, i := range picked {
		if progress != nil {
			progress(n, len(picked))
		}
		d, err := digestPackage(plan.choices[i].Path, seed)
		if err != nil {
			plan.order() // the import itself says what is wrong with it
			return plan
		}
		plan.info[plan.choices[i].Path].digest = d
	}
	// the best first: the newest version, the player's language, then the name
	rank := append([]int{}, picked...)
	sort.SliceStable(rank, func(x, y int) bool {
		a, b := plan.info[plan.choices[rank[x]].Path], plan.info[plan.choices[rank[y]].Path]
		if c, ok := a.ver.cmp(b.ver); ok && c != 0 {
			return c > 0
		}
		if a.ver.known() != b.ver.known() {
			return a.ver.known()
		}
		if (a.lang == lang) != (b.lang == lang) {
			return a.lang == lang
		}
		return strings.ToLower(plan.choices[rank[x]].Name) < strings.ToLower(plan.choices[rank[y]].Name)
	})
	var kept []int
	for _, i := range rank {
		di := plan.info[plan.choices[i].Path].digest
		gaveWay, same := -1, false
		for _, k := range kept {
			dk := plan.info[plan.choices[k].Path].digest
			shared, differ, weighty := dk.overlap(di)
			if differ == 0 && shared > 0 && shared == len(dk.paths) && shared == len(di.paths) {
				gaveWay, same = k, true // the same package under another name
				break
			}
			if differ == 0 || (weighty == 0 && differ < 3) {
				continue // the same files, or a script of another version: no harm
			}
			if shared*10 >= len(dk.paths)*6 && shared*10 >= len(di.paths)*6 {
				gaveWay = k // most of both: one or the other
				break
			}
		}
		if gaveWay < 0 {
			kept = append(kept, i)
			continue
		}
		c := &plan.choices[i]
		if same {
			c.Pick, c.Why, c.Note = false, "dup", "与「"+plan.choices[gaveWay].Name+"」内容相同"
			continue
		}
		c.Pick, c.Why, c.Note = false, "alt", "与「"+plan.choices[gaveWay].Name+"」写入同一批文件，只导入一个"
		plan.ask, plan.why = true, "overlap"
	}
	// an add-on that writes over some files of another comes after it
	for _, i := range kept {
		di := plan.info[plan.choices[i].Path].digest
		for _, k := range kept {
			dk := plan.info[plan.choices[k].Path].digest
			if k == i || len(dk.paths) <= len(di.paths) {
				continue
			}
			if _, differ, weighty := dk.overlap(di); differ > 0 && (weighty > 0 || differ >= 3) {
				c := &plan.choices[i]
				c.Why, c.Note = "addon", fmt.Sprintf("会改写「%s」的 %d 个文件，在它之后导入", plan.choices[k].Name, differ)
				break
			}
		}
	}
	plan.order()
	return plan
}

// questOf: q is the Quest version of the package p is the PC version of (its name says no more than p's, but
// for the platform).
func questOf(q, p *pkgInfo) bool {
	return q.quest && !p.quest && len(q.words) > 0 && subset(q.words, p.words)
}

func subset(a, b []string) bool {
	for _, x := range a {
		if !core.ContainsStr(b, x) {
			return false
		}
	}
	return true
}

// order puts the choices in the order they are imported: the versions of one package oldest first (so that
// the newest of those ticked is what stays), a package before the add-ons that write over some of its files,
// the rest by name.
func (plan *pkgPlan) order() {
	files := func(c PkgChoice) int {
		if in := plan.info[c.Path]; in != nil && in.digest != nil {
			return len(in.digest.paths)
		}
		return 0
	}
	series := func(c PkgChoice) string {
		if in := plan.info[c.Path]; in != nil {
			return in.series
		}
		return strings.ToLower(c.Name)
	}
	sort.SliceStable(plan.choices, func(x, y int) bool {
		a, b := plan.choices[x], plan.choices[y]
		if (a.Why == "addon") != (b.Why == "addon") {
			return b.Why == "addon"
		}
		if a.Why == "addon" && files(a) != files(b) {
			return files(a) > files(b)
		}
		sa, sb := series(a), series(b)
		if sa != sb {
			return sa < sb
		}
		c, ok := plan.info[a.Path].ver.cmp(plan.info[b.Path].ver)
		return ok && c < 0
	})
}

// skips: what of the card was not imported, given what was — the packages that were there twice, and where
// nothing was asked, the older versions and the Quest versions (the PC versions, for a project that builds for
// Quest; not the versions for other base bodies, which are left out as they always were). Where the player
// picked, what was in the list is the player's choice.
func (plan *pkgPlan) skips(picked []string) []PkgSkip {
	out := append([]PkgSkip{}, plan.skipped...)
	for _, c := range plan.choices {
		if core.ContainsStr(picked, c.Path) || !(c.Why == "dup" || (!plan.ask && (c.Why == "old" || c.Why == "quest" || c.Why == "pc"))) {
			continue
		}
		out = append(out, PkgSkip{Name: c.Name, Why: c.Why})
	}
	return out
}

// sortPicks: the packages the player picked, in the order of the plan.
func (plan *pkgPlan) sortPicks(paths []string) []string {
	at := map[string]int{}
	for i, c := range plan.choices {
		at[c.Path] = i
	}
	out := append([]string{}, paths...)
	sort.SliceStable(out, func(x, y int) bool { return at[out[x]] < at[out[y]] })
	return out
}

// skipText: what was not imported, for the log.
func skipText(s PkgSkip) string {
	switch s.Why {
	case "dup":
		return s.Name + "（重复）"
	case "old":
		return s.Name + "（旧版本）"
	case "quest":
		return s.Name + "（Quest 版本）"
	case "pc":
		return s.Name + "（PC 版本）"
	}
	return s.Name + "（与其他包写入同一批文件）"
}
