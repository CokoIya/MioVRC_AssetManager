package naming

import (
	"regexp"
	"strings"

	"vrclib/internal/core"
)

const AdultStyle = "H"

var RePSDName = regexp.MustCompile(`(^|[^a-z])(psd|psb|clip)([^a-z]|$)|テクスチャ素材|改変用素材|texture ?source`)

var (
	// files that, lying directly inside a folder, prove it is a product folder (not a category folder)
	StrongFileExt = map[string]bool{".unitypackage": true, ".fbx": true, ".blend": true, ".psd": true, ".ttf": true,
		".otf": true, ".vrca": true, ".prefab": true, ".clip": true, ".mat": true, ".anim": true, ".controller": true,
		".asset": true, ".shader": true, ".cs": true, ".unity": true}
	// loose files that count as an asset on their own
	LooseAssetExt = map[string]bool{".unitypackage": true, ".zip": true, ".rar": true, ".7z": true, ".ttf": true,
		".otf": true, ".fbx": true, ".blend": true, ".vrca": true}
	// texture sources that buyers edit to recolour an outfit
	PSDExt       = map[string]bool{".psd": true, ".psb": true, ".clip": true, ".sai": true, ".sai2": true, ".kra": true, ".xcf": true, ".mdp": true}
	reBoothID    = regexp.MustCompile(`(?:^|[^0-9])([0-9]{6,8})(?:[^0-9]|$)`)
	ReBracketNum = regexp.MustCompile(`[【\[]\s*[0-9]{1,5}\s*[】\]]`)
	reDupSuffix  = regexp.MustCompile(`\s*[\(（][0-9]{1,2}[\)）]\s*$`)
	reYYMMDD     = regexp.MustCompile(`^2[0-9](0[1-9]|1[0-2])(0[1-9]|[12][0-9]|3[01])$`)
	ReBoothURL   = regexp.MustCompile(`booth\.pm/(?:[a-z]{2}/)?items/([0-9]{5,9})`)
	reNonKey     = regexp.MustCompile(`[\s_\-\.\(\)（）\[\]【】'"+&,，!！~～・]+`)
	ReGenericKey = regexp.MustCompile(`^(材质|材料|道具|衣服|服装|衣装|头发|发型|配饰|饰品|素体|模型|插件|系统|动作|音效|字体|其他|其它|贴图|纹理|妆容|素材|资源|新建文件夹|新しいフォルダー|newfolder|备份|测试|更新|归总|合集|整合|特典|bonus|dlc|materials?|textures?|psd|fbx|prefabs?|unitypackages?|readme|tools?|hair|clothe?s?|outfits?|accessor(y|ies)|props?|plugins?|others?|misc|assets?|new|update[sd]?|fix(ed)?|最新|修复|版|年|月|日|号)+$`)
	ReStructural = regexp.MustCompile(`^(fbx|blend|textures?|tex|materials?|mat|psd|prefabs?|unitypackages?|readme|docs?|documents?|説明書|说明|manual|uv|uvmap|animations?|anim|meshe?s?|models?|images?|画像|samples?|preview|thumbnails?|サムネ|その他|others?|extra|bonus|特典|terms|规约|利用規約|shaders?|scripts?|editor|sounds?|audio|icons?|menu|expressions?|fx|data|source|src|assets|resources|png|tga|masks?|normal(map)?|emission|matcap|spec|liltoon|body|face|costume|kaihen_tips)$`)
	ReCollection = regexp.MustCompile(`合集|合辑|全家桶|collection|大全`)
	// compiled once: these are used for every card, every time the window asks for the library
	reDigits  = regexp.MustCompile(`[0-9]+`)
	reNameSep = regexp.MustCompile(`[\s_\-.]+`)
)

func BoothIDFromName(name string) string {
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

func NormKey(s string) string {
	s = reDupSuffix.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = reNonKey.ReplaceAllString(s, "")
	return s
}

func CleanName(n string) string {
	s := reDupSuffix.ReplaceAllString(n, "")
	s = ReBracketNum.ReplaceAllString(s, "")
	if id := BoothIDFromName(s); id != "" {
		s = strings.Replace(s, id, " ", 1)
	}
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, " _-·.")
	if s == "" {
		return strings.TrimSpace(n)
	}
	return s
}

// IsGenericName: names like "材质", "衣服头发", "12月26日更新" that say nothing about the product.
func IsGenericName(n string) bool {
	k := NormKey(n)
	k = reDigits.ReplaceAllString(k, "")
	k = strings.ReplaceAll(k, "version", "")
	k = strings.ReplaceAll(k, "ver", "")
	if k == "" || k == "v" {
		return true
	}
	return ReGenericKey.MatchString(k)
}

func IsStructuralName(n, parentKey string) bool {
	k := NormKey(n)
	if ReStructural.MatchString(strings.TrimLeft(strings.ToLower(n), "_ ")) || ReStructural.MatchString(k) {
		return true
	}
	return len([]rune(parentKey)) >= 4 && strings.Contains(k, parentKey)
}

// CommonChildPrefix finds a product name shared by most children ("Crimson Rumor_Hair", "Crimson_Rumor_PSD" → "Crimson Rumor").
func CommonChildPrefix(names []string) string {
	count := map[string]int{}
	disp := map[string]string{}
	n := 0
	for _, raw := range names {
		c := CleanName(core.StripArchiveExt(raw))
		if IsGenericName(c) || IsStructuralName(c, "") {
			continue
		}
		toks := reNameSep.Split(strings.TrimSpace(c), -1)
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

// IsCategoryWordName: a folder named only by category words ("衣服", "头发", "更新归总"), not just digits.
func IsCategoryWordName(n string) bool {
	k := reDigits.ReplaceAllString(NormKey(n), "")
	return k != "" && ReGenericKey.MatchString(k)
}

func IsMostlyDigits(s string) bool {
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

var BoothCatMap = map[string]string{
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

// BracketCategory reads an explicit tag such as "【衣服】" or "【插件ERP】" from a name.
func BracketCategory(name string) string {
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

func Classify(text string) string {
	t := strings.ToLower(text)
	for _, r := range catRules {
		if r.re.MatchString(t) {
			return r.cat
		}
	}
	return "其他"
}
