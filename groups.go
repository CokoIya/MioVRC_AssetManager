package main

// Same product, several downloads: an outfit sold with one package per base body
// ("HYPERTECH_EXO_FRAME_For_Milltina", "…_For_Rurune", …) or with a separate PSD pack. The library
// shows those as one card; filtering by a base body brings up the matching download.
//
// Also: base bodies named only as "For_X" in a file name, and style tags for outfits.

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ---------- base bodies named "For_X" ----------

var (
	reForBase   = regexp.MustCompile(`(?i)(?:^|[^a-z])for[ _\-]+([a-z]{3,15})(?:[^a-z]|$)`)
	reForBaseJa = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Za-z]{3,15})[ _]?対応`)
	// words that follow "for" without being a base body
	forStop = map[string]bool{}
)

func init() {
	for _, w := range strings.Fields(`vrchat vrc unity quest android ios pc mac liltoon poiyomi modular modularavatar ma avatar avatars
		all any common free blender mobile sdk vcc alcom test beta mmd vroid psd texture textures preview booth the your you
		men women girls boys girl boy male female use sale each every multi several various standard basic unitypackage fbx vrm
		pcss face tracking facetracking body hair windows version ver update new mesh mmd4 cluster resonite neos chilloutvr cvr
		lite light dark black white red blue pink green version2 more other others`) {
		forStop[w] = true
	}
}

// canonBase turns an extracted word into a base name: the table's name when it is a known base,
// otherwise the word with ordinary capitalisation ("LUMINA" → "Lumina").
func canonBase(w string, defs []baseDef) string {
	if hit := detectBases(w, defs); len(hit) > 0 {
		return hit[0]
	}
	if strings.ToUpper(w) == w || strings.ToLower(w) == w {
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		return string(r)
	}
	return w
}

// forBases finds base bodies written as "For_Milfy" / "Milfy対応" that the base table does not know.
func forBases(text string, defs []baseDef) []string {
	var out []string
	add := func(w string) {
		if forStop[strings.ToLower(w)] || isMostlyDigits(w) {
			return
		}
		out = append(out, canonBase(w, defs))
	}
	for _, m := range reForBase.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range reForBaseJa.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	return uniqStrings(out)
}

// ---------- grouping ----------

var (
	reForStrip   = regexp.MustCompile(`(?i)(^|[^a-z])for[ _\-]+[a-z0-9]+`)
	reJaForStrip = regexp.MustCompile(`[A-Za-z]+[ _]?対応`)
	reVariantTok = regexp.MustCompile(`(^|[^a-z])(psd|psb|clip|textures?|tex|sources?|src|quest|pc|android|common|full|set)([^a-z]|$)`)
	reVerAny     = regexp.MustCompile(`(?i)(v|ver\.?|version)\s*[0-9]+([._][0-9]+)*[a-z]?|[0-9]+([._][0-9]+)+`)
)

// stemOf is a product name with base bodies, "for X", versions and "PSD"/"Texture" words removed:
// the downloads of one product share it. "" when too little is left to be meaningful.
func stemOf(name string, defs []baseDef, extra []string) string {
	s := strings.ToLower(cleanName(name))
	s = reForStrip.ReplaceAllString(s, "$1 ")
	s = reJaForStrip.ReplaceAllString(s, " ")
	for _, d := range defs {
		for i, re := range d.ascii {
			if strings.Contains(s, d.words[i]) {
				s = re.ReplaceAllString(s, "$1 $2")
			}
		}
		for _, o := range d.others {
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
	k := normKey(s)
	if len([]rune(k)) < 5 || isMostlyDigits(k) || reGenericKey.MatchString(k) {
		return ""
	}
	return k
}

func wordRe(w string) *regexp.Regexp {
	key := "w\x00" + strings.ToLower(w)
	if v, ok := defsCache.Load(key); ok {
		return v.(*regexp.Regexp)
	}
	re := regexp.MustCompile(`(^|[^a-z])` + regexp.QuoteMeta(strings.ToLower(w)) + `([^a-z]|$)`)
	defsCache.Store(key, re)
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
func groupViews(st *Store, views []AssetView) {
	defs := parseBases(st.Settings.Bases)
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
		s := stemOf(v.AutoName, defs, v.AutoBases)
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
		gname := groupName(names)
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
		rs[i] = []rune(cleanName(n))
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
	if p := trim(common(false)); len([]rune(normKey(p))) >= 4 {
		return p
	}
	if p := trim(common(true)); len([]rune(normKey(p))) >= 4 && !isMostlyDigits(p) {
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
		return strings.Join(v.Bases[:3], " / ") + " 等 " + itoa(n) + " 个"
	}
	rest := cleanName(v.AutoName)
	if gname != "" {
		if i := strings.Index(strings.ToLower(rest), strings.ToLower(gname)); i >= 0 {
			rest = rest[:i] + rest[i+len(gname):]
		}
	}
	rest = strings.Trim(reVerAny.ReplaceAllString(rest, ""), " _-.·")
	if r := []rune(rest); len(r) > 0 && len(r) <= 24 {
		return rest
	}
	return ""
}

// ---------- style tags for outfits ----------

// defaultStyles: display name = Booth tags / words that put an outfit in it.
var defaultStyles = []string{
	"JK=JK|制服|セーラー|セーラー服|ブレザー|学生服|学校|スクール|school|uniform|sailor",
	"Sexy=セクシー|sexy|バニー|bunny|ボンデージ|bondage|レオタード|leotard|ランジェリー|lingerie|下着|ハイレグ|性感",
	"H=R-18|R18|NSFW|18禁|成人向け",
	"可爱=かわいい|可愛い|カワイイ|キュート|cute|kawaii|ゆめかわ|ロリータ|lolita|フリル|甘ロリ",
	"女仆=メイド|maid",
	"成熟=大人|お姉さん|エレガント|elegant|上品|オフィス|スーツ|suit|秘書|OL",
	"泳装=水着|swimsuit|swimwear|bikini|ビキニ|スク水",
	"和风=和服|着物|浴衣|和風|kimono|yukata|巫女|袴",
	"中华=チャイナ|チャイナドレス|旗袍|qipao|漢服|汉服",
	"哥特=ゴシック|gothic|ゴスロリ|地雷|量産型|パンク|punk",
	"休闲=カジュアル|casual|パーカー|hoodie|ストリート|street|スポーツ|sports|ジャージ",
	"科幻=サイバー|cyber|サイバーパンク|cyberpunk|SF|メカ|mecha|テック|アーマー|armor|戦闘服",
	"睡衣=パジャマ|pajama|pyjama|ルームウェア|ナイトウェア|ネグリジェ",
}

const adultStyle = "H"

// styleDefs parses the style table. Unlike base bodies, a one-letter name ("H") is not used as a
// search word by itself.
func styleDefs(list []string) []baseDef {
	key := "s\x00" + strings.Join(list, "\n")
	if v, ok := defsCache.Load(key); ok {
		return v.([]baseDef)
	}
	out := styleDefsNow(list)
	defsCache.Store(key, out)
	return out
}

func styleDefsNow(list []string) []baseDef {
	var out []baseDef
	for _, item := range list {
		item = strings.TrimSpace(item)
		name, aliases := item, ""
		if i := strings.Index(item, "="); i >= 0 {
			name, aliases = strings.TrimSpace(item[:i]), item[i+1:]
		}
		if name == "" {
			continue
		}
		bd := baseDef{name: name}
		words := strings.Split(aliases, "|")
		if len([]rune(name)) >= 2 {
			words = append(words, name)
		}
		for _, w := range words {
			w = strings.TrimSpace(strings.ToLower(w))
			if w == "" {
				continue
			}
			if isASCII(w) {
				bd.words = append(bd.words, w)
				bd.ascii = append(bd.ascii, regexp.MustCompile(`(^|[^a-z0-9])`+regexp.QuoteMeta(w)+`([^a-z0-9]|$)`))
			} else {
				bd.others = append(bd.others, w)
			}
		}
		out = append(out, bd)
	}
	return out
}

func styleNames(list []string) []string {
	var out []string
	for _, d := range styleDefs(list) {
		out = append(out, d.name)
	}
	return out
}

// autoStyles: style tags for an outfit from its Booth tags and names (not the description, which
// mentions everything it goes well with).
func autoStyles(v *AssetView, defs []baseDef) []string {
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
	hit := map[string]bool{}
	for _, n := range detectBases(strings.Join(parts, " | "), defs) {
		hit[n] = true
	}
	if adult {
		hit[adultStyle] = true
	}
	var out []string
	for _, d := range defs { // in the table's order
		if hit[d.name] {
			out = append(out, d.name)
			hit[d.name] = false
		}
	}
	return out
}
