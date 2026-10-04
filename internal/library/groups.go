package library

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

// ---------- base bodies named "For_X" ----------

// ---------- grouping ----------

var (
	reForStrip   = regexp.MustCompile(`(?i)(^|[^a-z])for[ _\-]+[a-z0-9]+`)
	reJaForStrip = regexp.MustCompile(`[A-Za-z]+[ _]?対応`)
	reVariantTok = regexp.MustCompile(`(^|[^a-z])(psd|psb|clip|textures?|tex|sources?|src|quest|pc|android|common|full|set)([^a-z]|$)`)
	reVerAny     = regexp.MustCompile(`(?i)(v|ver\.?|version)\s*[0-9]+([._][0-9]+)*[a-z]?|[0-9]+([._][0-9]+)+`)
)

// stemOf is a product name with base bodies, "for X", versions and "PSD"/"Texture" words removed:
// the downloads of one product share it. "" when too little is left to be meaningful.
func stemOf(name string, defs []naming.BaseDef, extra []string) string {
	s := strings.ToLower(naming.CleanName(name))
	s = reForStrip.ReplaceAllString(s, "$1 ")
	s = reJaForStrip.ReplaceAllString(s, " ")
	for _, d := range defs {
		for i, re := range d.ASCII {
			if strings.Contains(s, d.Words[i]) {
				s = re.ReplaceAllString(s, "$1 $2")
			}
		}
		for _, o := range d.Others {
			s = strings.ReplaceAll(s, o, " ")
		}
	}
	for _, b := range extra {
		s = wordRe(b).ReplaceAllString(s, "$1 $2")
	}
	for i := 0; i < 2; i++ { // overlapping words ("_psd_tex_")
		s = reVariantTok.ReplaceAllString(s, "$1 $3")
	}
	s = reVerAny.ReplaceAllString(s, " ")
	k := naming.NormKey(s)
	if len([]rune(k)) < 5 || naming.IsMostlyDigits(k) || naming.ReGenericKey.MatchString(k) {
		return ""
	}
	return k
}

func wordRe(w string) *regexp.Regexp {
	key := "w\x00" + strings.ToLower(w)
	if v, ok := naming.DefsCache.Load(key); ok {
		return v.(*regexp.Regexp)
	}
	re := regexp.MustCompile(`(^|[^a-z])` + regexp.QuoteMeta(strings.ToLower(w)) + `([^a-z]|$)`)
	naming.DefsCache.Store(key, re)
	return re
}

type groupAgg struct {
	booth map[string]bool
	shops map[string]bool
	cats  map[string]bool // categories of the non-PSD members
}

func (g *groupAgg) merge(o *groupAgg) {
	for k := range o.booth {
		g.booth[k] = true
	}
	for k := range o.shops {
		g.shops[k] = true
	}
	for k := range o.cats {
		g.cats[k] = true
	}
}

func compatible(a, b *groupAgg) bool {
	count := func(x, y map[string]bool) int {
		n := len(x)
		for k := range y {
			if !x[k] {
				n++
			}
		}
		return n
	}
	return count(a.booth, b.booth) <= 1 && count(a.shops, b.shops) <= 1 && count(a.cats, b.cats) <= 1
}

func viewShop(v *AssetView) string {
	if v.Booth != nil && v.Booth.Shop != "" {
		return strings.ToLower(v.Booth.Shop)
	}
	if v.Purchase != nil && v.Purchase.Shop != "" {
		return strings.ToLower(v.Purchase.Shop)
	}
	return ""
}

// groupViews marks downloads of the same product with a shared Group id, a GroupName and a
// per-download Variant label.
func groupViews(st *core.Store, views []AssetView) {
	defs := naming.ParseBases(st.Settings.Bases)
	n := len(views)
	parent := make([]int, n)
	aggs := make([]*groupAgg, n)
	for i := range views {
		parent[i] = i
		v := &views[i]
		ag := &groupAgg{booth: map[string]bool{}, shops: map[string]bool{}, cats: map[string]bool{}}
		if v.BoothID != "" {
			ag.booth[v.BoothID] = true
		}
		if s := viewShop(v); s != "" {
			ag.shops[s] = true
		}
		if !v.PSD {
			ag.cats[v.Category] = true
		}
		aggs[i] = ag
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(i, j int, check bool) {
		ri, rj := find(i), find(j)
		if ri == rj || (check && !compatible(aggs[ri], aggs[rj])) {
			return
		}
		parent[rj] = ri
		aggs[ri].merge(aggs[rj])
	}
	skip := func(v *AssetView) bool { return v.User.NoGroup || v.SplitInto > 0 }
	// one Booth product
	byBooth := map[string]int{}
	for i := range views {
		v := &views[i]
		if v.BoothID == "" || skip(v) {
			continue
		}
		if j, ok := byBooth[v.BoothID]; ok {
			union(j, i, false)
		} else {
			byBooth[v.BoothID] = i
		}
	}
	// the same name once base bodies / PSD / versions are taken out
	byStem := map[string][]int{}
	var stems []string
	for i := range views {
		v := &views[i]
		if skip(v) {
			continue
		}
		s := stemMemo.get(memoKey{defs: defsID(defs), name: v.AutoName + "\x00" + strings.Join(v.AutoBases, "\x00")}, func() string {
			return stemOf(v.AutoName, defs, v.AutoBases)
		})
		if s == "" {
			continue
		}
		if _, ok := byStem[s]; !ok {
			stems = append(stems, s)
		}
		byStem[s] = append(byStem[s], i)
	}
	for _, s := range stems {
		idx := byStem[s]
		for k := 1; k < len(idx); k++ {
			union(idx[0], idx[k], true)
		}
	}
	members := map[int][]int{}
	for i := range views {
		r := find(i)
		members[r] = append(members[r], i)
	}
	for _, m := range members {
		if len(m) < 2 {
			continue
		}
		keys := make([]string, len(m))
		names := make([]string, len(m))
		for k, i := range m {
			keys[k] = views[i].Key
			names[k] = views[i].AutoName
		}
		sort.Strings(keys)
		gid := "g:" + keys[0]
		gname := groupMemo.get(memoKey{name: strings.Join(names, "\x00")}, func() string { return groupName(names) })
		if gname == "" {
			for _, i := range m {
				if b := views[i].Booth; b != nil && b.Name != "" {
					gname = b.Name
					break
				}
			}
		}
		// a PSD pack files under its product's category ("衣服", not "材质")
		cats := map[string]int{}
		mainCat := ""
		for _, i := range m {
			if v := &views[i]; !v.PSD {
				cats[v.Category]++
				if mainCat == "" || cats[v.Category] > cats[mainCat] {
					mainCat = v.Category
				}
			}
		}
		for _, i := range m {
			v := &views[i]
			v.Group, v.GroupName = gid, gname
			v.Variant = variantLabel(v, gname)
			if v.PSD && mainCat != "" && v.User.Category == "" {
				v.Category = mainCat
			}
		}
	}
}

// groupName: what the downloads' names have in common ("HYPERTECH_EXO_FRAME_For_Milltina_v1.01",
// "HYPERTECH_EXO_FRAME_For_Rurune_v1.01" → "HYPERTECH_EXO_FRAME").
func groupName(names []string) string {
	trim := func(s string) string {
		s = strings.Trim(s, " _-.·")
		for {
			l := strings.ToLower(s)
			if strings.HasSuffix(l, " for") || strings.HasSuffix(l, "_for") || strings.HasSuffix(l, "-for") {
				s = strings.Trim(s[:len(s)-4], " _-.·")
				continue
			}
			if strings.HasPrefix(l, "for ") || strings.HasPrefix(l, "for_") || strings.HasPrefix(l, "for-") {
				s = strings.Trim(s[4:], " _-.·")
				continue
			}
			return s
		}
	}
	rs := make([][]rune, len(names))
	for i, n := range names {
		rs[i] = []rune(naming.CleanName(n))
	}
	common := func(rev bool) string {
		first := rs[0]
		k := 0
	outer:
		for ; k < len(first); k++ {
			for _, r := range rs[1:] {
				if k >= len(r) {
					break outer
				}
				a, b := first[k], r[k]
				if rev {
					a, b = first[len(first)-1-k], r[len(r)-1-k]
				}
				if unicode.ToLower(a) != unicode.ToLower(b) {
					break outer
				}
			}
		}
		// do not stop inside a word ("ventus_school_PSD" / "ventus_school_Plum" → "ventus_school_", not "…_P")
		word := func(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }
		at := func(r []rune, i int) rune {
			if rev {
				return r[len(r)-1-i]
			}
			return r[i]
		}
		for k > 0 && word(at(first, k-1)) {
			split := false
			for _, r := range rs {
				if k < len(r) && word(at(r, k)) {
					split = true
				}
			}
			if !split {
				break
			}
			k--
		}
		if rev {
			return string(first[len(first)-k:])
		}
		return string(first[:k])
	}
	if p := trim(common(false)); len([]rune(naming.NormKey(p))) >= 4 {
		return p
	}
	if p := trim(common(true)); len([]rune(naming.NormKey(p))) >= 4 && !naming.IsMostlyDigits(p) {
		return p
	}
	return ""
}

// variantLabel says what one download of a product is for: its base bodies, "PSD", or the rest
// of its name.
func variantLabel(v *AssetView, gname string) string {
	if v.PSD {
		return "PSD"
	}
	if n := len(v.Bases); n > 0 {
		if n <= 3 {
			return strings.Join(v.Bases, " / ")
		}
		return strings.Join(v.Bases[:3], " / ") + " 等 " + core.Itoa(n) + " 个"
	}
	return restMemo.get(memoKey{name: v.AutoName + "\x00" + gname}, func() string {
		rest := naming.CleanName(v.AutoName)
		if gname != "" {
			if i, n := indexFold(rest, gname); i >= 0 {
				rest = rest[:i] + rest[i+n:]
			}
		}
		rest = strings.Trim(reVerAny.ReplaceAllString(rest, ""), " _-.·")
		if r := []rune(rest); len(r) > 0 && len(r) <= 24 {
			return rest
		}
		return ""
	})
}

// indexFold: where sub is in s whatever the letter case, and how many bytes of s it takes there; -1 when it
// is not. (A place found in the lower-cased text is not a place in the text itself: "Ⱥ" and "ⱥ" differ in
// length.)
func indexFold(s, sub string) (int, int) {
	for i := range s {
		rest, matched := s[i:], true
		for _, want := range sub {
			r, size := utf8.DecodeRuneInString(rest)
			if size == 0 || unicode.ToLower(r) != unicode.ToLower(want) {
				matched = false
				break
			}
			rest = rest[size:]
		}
		if matched && sub != "" {
			return i, len(s) - i - len(rest)
		}
	}
	return -1, 0
}

// ---------- style tags for outfits ----------

// styleDefs parses the style table. Unlike base bodies, a one-letter name ("H") is not used as a
// search word by itself.
func styleDefs(list []string) []naming.BaseDef {
	if p := lastStyles.Load(); p != nil && slices.Equal(p.list, list) {
		return p.defs // asked for once per card, and nearly always the same table
	}
	key := "s\x00" + strings.Join(list, "\n")
	v, ok := naming.DefsCache.Load(key)
	if !ok {
		v, _ = naming.DefsCache.LoadOrStore(key, styleDefsNow(list))
	}
	out := v.([]naming.BaseDef)
	lastStyles.Store(&parsedStyles{slices.Clone(list), out})
	return out
}

var lastStyles atomic.Pointer[parsedStyles]

type parsedStyles struct {
	list []string
	defs []naming.BaseDef
}

func styleDefsNow(list []string) []naming.BaseDef {
	var out []naming.BaseDef
	for _, item := range list {
		item = strings.TrimSpace(item)
		name, aliases := item, ""
		if i := strings.Index(item, "="); i >= 0 {
			name, aliases = strings.TrimSpace(item[:i]), item[i+1:]
		}
		if name == "" {
			continue
		}
		bd := naming.BaseDef{Name: name}
		words := strings.Split(aliases, "|")
		if len([]rune(name)) >= 2 {
			words = append(words, name)
		}
		for _, w := range words {
			w = strings.TrimSpace(strings.ToLower(w))
			if w == "" {
				continue
			}
			if core.IsASCII(w) {
				bd.Words = append(bd.Words, w)
				bd.ASCII = append(bd.ASCII, regexp.MustCompile(`(^|[^a-z0-9])`+regexp.QuoteMeta(w)+`([^a-z0-9]|$)`))
			} else {
				bd.Others = append(bd.Others, w)
			}
		}
		out = append(out, bd)
	}
	return out
}

func StyleNames(list []string) []string {
	var out []string
	for _, d := range styleDefs(list) {
		out = append(out, d.Name)
	}
	return out
}

// autoStyles: style tags for an outfit from its Booth tags and names (not the description, which
// mentions everything it goes well with).
func autoStyles(v *AssetView, defs []naming.BaseDef) []string {
	if v.Category != "衣服" {
		return nil
	}
	parts := []string{v.AutoName, v.RawName}
	adult := false
	if v.Booth != nil {
		parts = append(parts, v.Booth.Name)
		parts = append(parts, v.Booth.Tags...)
		adult = v.Booth.Adult
	}
	if v.Purchase != nil {
		parts = append(parts, v.Purchase.Name)
	}
	text := strings.Join(parts, " | ")
	// the list is shared by every card of the same text: read, never changed
	return styleMemo.get(memoKey{defsID(defs), adult, text}, func() []string {
		hit := map[string]bool{}
		for _, n := range naming.DetectBases(text, defs) {
			hit[n] = true
		}
		if adult {
			hit[naming.AdultStyle] = true
		}
		var out []string
		for _, d := range defs { // in the table's order
			if hit[d.Name] {
				out = append(out, d.Name)
				hit[d.Name] = false
			}
		}
		return out
	})
}
