package naming

import (
	"regexp"
	"sort"
	"strings"

	"vrclib/internal/core"
)

// What names a part or a variant of one product rather than a product: its colours, its eyes, its materials,
// a type, a number. A product's folder often holds those as folders or packages of their own ("Eye_Blue/",
// "Color_02.unitypackage", "黒/", "目テクスチャ/", "预制体/"); they are no products to be cards of their own.
// Words that also name a category of a library ("材质", "贴图", "模型", "Textures") are not among them: a folder
// of that name may be a category, and what it holds decides (library/scan.go).
var (
	partWordsASCII = map[string]bool{}
	partWordsCJK   []string // longest first: they are taken out of a name one after another
	reCamel        = regexp.MustCompile(`([a-z])([A-Z])`)
	reLetterDigit  = regexp.MustCompile(`([A-Za-z])([0-9])|([0-9])([A-Za-z])`)
	rePartSep      = regexp.MustCompile(`[\s_\-.()（）\[\]【】+&・,，、~～!！'"]+`)
)

func init() {
	for _, w := range strings.Fields(`eye eyes iris pupil pupils highlight highlights
		color colors colour colours col variation variations variant variants ver version type types pattern patterns
		option options opt preset presets alt sub extra
		mat mats material materials tex texture textures mask masks normal normals emission matcap uv
		prefab prefabs package packages unitypackage unitypackages anim animation animations shader shaders
		script scripts icon icons image images img
		black white red blue green pink purple yellow orange brown gray grey silver gold navy beige cream mint
		lavender violet cyan aqua dark light pastel khaki wine rose ivory coral teal indigo magenta peach
		bordeaux burgundy crimson scarlet azure monochrome mono`) {
		partWordsASCII[w] = true
	}
	partWordsCJK = strings.Fields(`テクスチャー テクスチャ マテリアル プレハブ パッケージ アニメーション シェーダー スクリプト アイコン
		バリエーション オプション プリセット パターン タイプ カラー 色違い 色変え 差分 配色 瞳 目 眼 アイ 画像 柄
		黒色 白色 水色 桃色 黒 白 赤 青 緑 桃 紫 黄 茶 灰 銀 金 紺
		ブラック ホワイト レッド ブルー グリーン ピンク パープル イエロー オレンジ ブラウン グレー シルバー ゴールド
		ネイビー ベージュ ミント ラベンダー クリーム
		预制体 预设 着色器 脚本 动画 图标 颜色 配色 款式 款 眼睛 眼瞳 瞳孔 美瞳
		黑色 红色 蓝色 绿色 粉色 紫色 黄色 灰色 藏青 米白 米色 黑 红 蓝 绿 粉 橙 棕 银 青 色`)
	sort.SliceStable(partWordsCJK, func(i, j int) bool { return len(partWordsCJK[i]) > len(partWordsCJK[j]) })
}

// IsPartName: does the name say only "a part or a variant of a product" — a colour, the eyes, the materials, a
// type, with numbers and single letters ("Eye_Blue", "EyeBlue", "Color02", "Type A", "黒", "目テクスチャ",
// "预制体", "粉色")? Not when anything else is in it ("Black Cat Hoodie", "白丝"), and not for a number alone.
func IsPartName(n string) bool {
	s := CleanName(core.StripArchiveExt(n))
	s = reCamel.ReplaceAllString(s, "$1 $2")
	s = reLetterDigit.ReplaceAllString(s, "$1$3 $2$4")
	s = strings.ToLower(s)
	word := false
	for _, w := range partWordsCJK {
		if strings.Contains(s, w) {
			s, word = strings.ReplaceAll(s, w, " "), true
		}
	}
	for _, t := range rePartSep.Split(s, -1) {
		switch {
		case t == "":
		case partWordsASCII[t]:
			word = true
		case isDigits(t) || (len(t) == 1 && t[0] >= 'a' && t[0] <= 'z'):
		default:
			return false
		}
	}
	return word
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
