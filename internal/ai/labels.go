package ai

import (
	"strings"

	"vrclib/internal/core"
)

// The words the pipeline writes into an avatar's menu follow the language of the interface (the settings'
// lang: "" Chinese, "en", "ja"). The package works in Chinese throughout; a plan is put into the player's
// language as it leaves for Unity (localMenu). The plugin finds a menu item by its name, so a word the
// avatar's menu already has in another language is kept as it is there: an avatar set up in Chinese goes on
// working after the player changes the language, and the other way round.

// menuLang: the language of the interface (set by RegisterAI; Chinese in tests that do not).
var menuLang = func() string { return "" }

func langIndex(lang string) int {
	switch lang {
	case "en":
		return 1
	case "ja":
		return 2
	}
	return 0
}

// menuWords: Chinese, English, Japanese. The categories of a menu, the parts of an outfit, and the items
// the pipeline names itself.
var menuWords = [][3]string{
	{"衣服", "Outfits", "衣装"}, {"头发", "Hair", "髪型"}, {"皮肤", "Skins", "スキン"}, {"表情", "Expressions", "表情"},
	{"配饰", "Accessories", "アクセサリー"}, {"道具", "Props", "小物"}, {"光影", "Lighting", "ライティング"}, {"互动", "Interactions", "インタラクション"},
	{"外套", "Jacket", "アウター"}, {"上衣", "Top", "トップス"}, {"裙子", "Skirt", "スカート"}, {"裤子", "Pants", "パンツ"},
	{"内衣", "Underwear", "下着"}, {"袜子", "Socks", "靴下"}, {"鞋子", "Shoes", "靴"}, {"帽子", "Hat", "帽子"},
	{"尾巴", "Tail", "しっぽ"}, {"翅膀", "Wings", "羽"}, {"手套", "Gloves", "手袋"}, {"包", "Bag", "バッグ"},
	{"头饰", "Headwear", "頭飾り"}, {"饰品", "Trinkets", "アクセ"},
	{"穿上", "Wear", "着る"}, {"戴上", "Put on", "つける"}, {"显示", "Show", "表示"}, {"配色", "Colors", "カラー"},
	{"原装头发", "Original hair", "元の髪"}, {"原版皮肤", "Original skin", "元のスキン"}, {stripLabel, "Take all off", "全部脱ぐ"},
}

// hierWords: what a menu template is written with, in each language (parseHierarchy reads them all).
var hierWords = [][3]string{
	{"主菜单", "Main menu", "メインメニュー"}, {"{分类}", "{category}", "{カテゴリ}"}, {"{素材}", "{asset}", "{アセット}"}, {"{开关}", "{toggles}", "{トグル}"},
}

// menuWord: a Chinese menu word in that language (itself when it is not one of the pipeline's words).
func menuWord(lang, zh string) string {
	if i := langIndex(lang); i > 0 {
		for _, w := range menuWords {
			if w[0] == zh {
				return w[i]
			}
		}
	}
	return zh
}

// sameMenuWord: two names of one thing, in whichever languages.
func sameMenuWord(a, b string) bool {
	if a == b {
		return true
	}
	for _, w := range menuWords {
		if (w[0] == a || w[1] == a || w[2] == a) && (w[0] == b || w[1] == b || w[2] == b) {
			return true
		}
	}
	return false
}

// hierarchyIn: the default menu template as that language writes it.
func hierarchyIn(lang string) string {
	i := langIndex(lang)
	parts := make([]string, len(hierWords))
	for n, w := range hierWords {
		parts[n] = w[i]
	}
	return strings.Join(parts, " > ")
}

// localHierarchy: a template that is the default of some language, as the player's language writes it; one
// the player wrote stays as it is.
func localHierarchy(lang, h string) string {
	for _, l := range []string{"", "en", "ja"} {
		if strings.TrimSpace(h) == hierarchyIn(l) {
			return hierarchyIn(lang)
		}
	}
	return h
}

// zhHierarchy: a template with the words of the other languages put back into Chinese, which is what the
// AI's instructions explain.
func zhHierarchy(h string) string {
	for _, w := range hierWords {
		h = strings.ReplaceAll(strings.ReplaceAll(h, w[1], w[0]), w[2], w[0])
	}
	return h
}

// langNote: what the AI is told when the interface is not Chinese — the language to answer the player in,
// and the names to give menu items.
func langNote(lang string) string {
	i := langIndex(lang)
	if i == 0 {
		return ""
	}
	name := [3]string{"", "英语（English）", "日语（日本語）"}[i]
	var words []string
	for _, w := range menuWords {
		words = append(words, w[0]+"="+w[i])
	}
	return "\n\n# 语言\n玩家的界面语言是" + name + "。上面说的「用中文」在这里都改为" + name + "：对玩家说的每一句话都用" + name + "写。" +
		"菜单里的名字（label 和子菜单名）也用" + name + "，上面提到的中文菜单名换成：" + strings.Join(words, "、") + "；素材的名字用它的原名或简短的" + name + "名。" +
		"头像上已经有的菜单项和子菜单沿用它们现在的名字，不要因为语言不同另建一份。工具的参数、物体和文件的路径保持原样。"
}

// localMenu puts a plan's names into the player's language, and keeps the names the avatar's menu already
// has: at each level of an item's path, and for the item itself, a name the menu has there under another
// language's word for the same thing is used as it is. tree is the avatar's Modular Avatar menu as inspect
// tells it, root the menu object the plan goes under. The items given are left as they are.
func localMenu(lang string, tree []menuNode, root string, items []map[string]any) []map[string]any {
	var top []menuNode
	for _, n := range tree {
		if n.Object == root || baseName(n.Object) == root || (root == "" && baseName(n.Object) == defaultMenuRoot) {
			top = n.Children
		}
	}
	// the name to use among these menu entries: the one there already, else the word of the language
	pick := func(at []menuNode, zh string) (string, []menuNode) {
		want := menuWord(lang, zh)
		for _, try := range []func(string) bool{func(l string) bool { return l == want }, func(l string) bool { return sameMenuWord(l, zh) }} {
			for _, n := range at {
				if try(n.Label) {
					return n.Label, n.Children
				}
			}
		}
		return want, nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		c := make(map[string]any, len(it))
		for k, v := range it {
			c[k] = v
		}
		at := top
		if p, ok := it["path"].([]string); ok {
			path := make([]string, len(p))
			for i, seg := range p {
				path[i], at = pick(at, seg)
			}
			c["path"] = path
		} else if p, ok := it["path"].([]any); ok { // a plan as the AI wrote it
			path := make([]any, len(p))
			for i, seg := range p {
				s, _ := seg.(string)
				path[i], at = pick(at, s)
			}
			c["path"] = path
		}
		if l, ok := it["label"].(string); ok {
			c["label"], _ = pick(at, l)
		}
		out = append(out, c)
	}
	return out
}

// storeLang: the interface's language as the settings have it.
func storeLang(st *core.Store) string {
	if st == nil {
		return ""
	}
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	return st.Settings.Lang
}
