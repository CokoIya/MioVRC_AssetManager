package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vrclib/internal/core"
)

// The pipeline with an AI (1.7.12). The program does what it can be sure of — the base body into the scene,
// the outfits on, the plugins in place, a draft of the menu by the words in the mesh names — and asks the AI
// once for what takes judgement: the parts of each outfit and hair (which meshes go together, under what
// name), and the names of its menu and its colours. The answer is checked here; with what is wrong in it, the
// AI is asked once more; failing that, the draft is built. After the menu is built, the avatar photographed
// and checked, the AI says once what it sees and what the check-up means. Two or three requests where the AI
// used to work its way through with a request for every step: less to pay, less to go wrong, nothing to get
// lost in, and what it may change is only ever what the plan lets it.

// meshInfo: a mesh of a dressed thing, as the plugin describes it.
type meshInfo struct {
	Path      string    `json:"path"`
	Active    bool      `json:"active"`
	Materials []string  `json:"materials,omitempty"`
	Height    []float64 `json:"height,omitempty"`
	Triangles int       `json:"triangles,omitempty"`
}

func meshPaths(ms []meshInfo) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Path)
	}
	return out
}

// planned: an item of a run's menu plan, and the menu (its root object) it goes under.
type planned struct {
	root string
	item map[string]any
}

// wornInfo: an outfit or a hair the run put on, as far as its menu goes.
type wornInfo struct {
	kind    string // 衣服, 头发
	label   string // its name in the draft
	object  string // its root object below the avatar
	meshes  []meshInfo
	colours []colourInfo // its other colours (outfit items with materialsFrom)
	base    []string     // the category level of its menu
	param   string       // the parameter its outfit items pick on
	first   bool         // it is the default of the parameter
	items   []int        // its items in the draft plan
}

type colourInfo struct {
	Prefab string `json:"materialsFrom"`
	Label  string `json:"name"`
}

// planMax: items one menu page takes (VRChat shows eight).
const planMax = 8

const menuPlanPrompt = `你是「MioVRCA」的菜单整理助手。程序已经把素材穿到玩家的 VRChat 头像上，并按网格名称做了一份菜单草案。你的任务：为下面每件衣服和头发整理它的菜单项——把网格分成玩家能理解的部件开关，给子菜单和配色起简短的名字。只输出 JSON。

每件衣服或头发要有：
- 一个穿上项：kind="outfit"，objects 只写它的根物体（object），不写 materialsFrom。只有一个配色时 label 写「穿上」（头发写「戴上」）。
- 每个配色一项（colours 里的每一个都要有，不多不少）：kind="outfit"，objects 同样只写根物体，materialsFrom 照抄。有配色时，穿上项和配色项的 label 都写配色名（例如 黑色、白色、粉色、原色），不要写 prefab 的英文文件名。
- 部件开关（可以没有）：kind="part"，选中=隐藏。objects 写一组会一起出现的网格。按网格名、材质名和离地高度判断是什么：h=[最低,最高]，单位米，脚在 0 附近；tris 是面数，hidden=true 表示现在隐藏。常用的名字：外套、上衣、裙子、裤子、袜子、鞋子、帽子、手套、头饰、饰品、尾巴、翅膀、包、内衣。
  - 一个开关管一组会一起出现的网格，例如「外套」包括外套本体和它的袖章、里衬；左右成对的放在一起。
  - 大件不要漏；拿不准的小零件归到「饰品」。
  - 衣服的主体（没有它就不成一件衣服的那部分，比如连衣裙本身、身体网格）不做开关。不要做一个包含全部网格的部件。
  - 头发的部件只做发饰、帽子这类，头发本身不做开关；没有这些就不做部件。
  - 同一个网格只放进一个部件。

菜单的位置（path，从主菜单往下写，不含「主菜单」）：
- 开头照抄这件的 category（分类那一层，例如 ["衣服"]）。
- 按模板有「{素材}」这一层时，接着是这件自己的子菜单，名字用它的简短中文名（2～6 个字，例如「水手服」「双马尾」；草案的名字合适就沿用）。穿上项、配色项和部件项都放在这个子菜单里。
- 每个菜单页最多 8 项。穿上项、配色项和部件加起来超过 8 项时，把穿上项和配色项收进下一层「配色」；部件太多就合并成更大的组（各种小零件合成「饰品」）。
- 同一页里的名字不能重复。

只输出这里列出的衣服和头发的 outfit / part 项。配饰、道具、插件、皮肤、一键脱光、素体自带的头发由程序处理，不要输出；parameter、default 也由程序填写，不要输出。objects、materialsFrom 只能用下面给出的路径，不要自己编。

输出格式（只输出这个 JSON，不要解释，不要 Markdown 代码块以外的文字）：
{"items":[{"kind":"outfit","path":["衣服","水手服"],"label":"穿上","objects":["Sailor"]},{"kind":"part","path":["衣服","水手服"],"label":"外套","objects":["Sailor/Jacket","Sailor/Jacket_Sleeve"]}]}`

// menuPlanRequest: what the AI is given — each outfit and hair with its meshes and its draft items.
func menuPlanRequest(worn []wornInfo, plan []planned, hierarchy string) string {
	type meshOut struct {
		Path   string    `json:"path"`
		Mats   []string  `json:"mats,omitempty"`
		H      []float64 `json:"h,omitempty"`
		Tris   int       `json:"tris,omitempty"`
		Hidden bool      `json:"hidden,omitempty"`
	}
	type outfitOut struct {
		Kind     string           `json:"kind"`
		Name     string           `json:"name"`
		Object   string           `json:"object"`
		Category []string         `json:"category"`
		Colours  []colourInfo     `json:"colours,omitempty"`
		Meshes   []meshOut        `json:"meshes"`
		Draft    []map[string]any `json:"draft"`
	}
	var outs []outfitOut
	for _, w := range worn {
		o := outfitOut{Kind: w.kind, Name: w.label, Object: w.object, Category: w.base, Colours: w.colours}
		for _, m := range w.meshes {
			o.Meshes = append(o.Meshes, meshOut{Path: m.Path, Mats: m.Materials, H: m.Height, Tris: m.Triangles, Hidden: !m.Active})
		}
		for _, i := range w.items {
			it := map[string]any{}
			for _, k := range []string{"kind", "path", "label", "objects", "materialsFrom"} {
				if v, ok := plan[i].item[k]; ok && v != "" {
					it[k] = v
				}
			}
			o.Draft = append(o.Draft, it)
		}
		outs = append(outs, o)
	}
	b, _ := json.Marshal(map[string]any{"outfits": outs})
	return "菜单层级模板：" + zhHierarchy(hierarchy) + "\n下面是这次穿上的衣服和头发（JSON）。draft 是程序按网格名称做的草案，请在它的基础上整理。\n" + string(b)
}

// parsePlanItems: the items of an answer, whatever is written around the JSON.
func parsePlanItems(text string) ([]map[string]any, error) {
	t := strings.TrimSpace(text)
	if i := strings.Index(t, "```"); i >= 0 { // a fenced block: what is in it
		rest := t[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			t = strings.TrimSpace(rest[:j])
		}
	}
	var v struct {
		Items []map[string]any `json:"items"`
	}
	if a, z := strings.IndexByte(t, '{'), strings.LastIndexByte(t, '}'); a >= 0 && z > a && json.Unmarshal([]byte(t[a:z+1]), &v) == nil && len(v.Items) > 0 {
		return v.Items, nil
	}
	var list []map[string]any
	if a, z := strings.IndexByte(t, '['), strings.LastIndexByte(t, ']'); a >= 0 && z > a && json.Unmarshal([]byte(t[a:z+1]), &list) == nil && len(list) > 0 {
		return list, nil
	}
	return nil, errors.New(`回答不是要求的 JSON（{"items":[…]}）`)
}

func anyStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, _ := e.(string)
			out = append(out, strings.TrimSpace(s))
		}
		return out
	case string:
		if x = strings.TrimSpace(x); x != "" {
			return []string{x}
		}
	}
	return nil
}

func hasPrefix(p, base []string) bool {
	if len(p) < len(base) {
		return false
	}
	for i := range base {
		if p[i] != base[i] {
			return false
		}
	}
	return true
}

// mergeMenuPlan checks the AI's items against what the run put on and, when nothing is wrong, puts them in
// the place of each outfit's draft items: its own items come where its first one was, the other items of
// the plan (accessories, props, plugins, skins, 一键脱光, the body's own hair) stay as and where they were.
// The parameters and the defaults are the draft's. errs: what is wrong, said to the AI.
func mergeMenuPlan(items []map[string]any, worn []wornInfo, plan []planned) (merged []planned, n int, errs []string) {
	owner := map[string]int{}  // a root object → its outfit
	meshOf := map[string]int{} // a mesh → its outfit
	for i, w := range worn {
		owner[w.object] = i
		for _, m := range w.meshes {
			meshOf[m.Path] = i
		}
	}
	mine := make([][]map[string]any, len(worn))
	bases := make([]bool, len(worn))
	colours := make([]map[string]int, len(worn))
	inPart := make([]map[string]string, len(worn))
	for i := range worn {
		colours[i], inPart[i] = map[string]int{}, map[string]string{}
	}
	bad := func(i int, label, f string, a ...any) {
		who := fmt.Sprintf("第 %d 项", i+1)
		if label != "" {
			who += "「" + label + "」"
		}
		errs = append(errs, who+"："+fmt.Sprintf(f, a...))
	}
	for i, it := range items {
		kind, _ := it["kind"].(string)
		label, _ := it["label"].(string)
		label = strings.TrimSpace(label)
		path := anyStrings(it["path"])
		objects := anyStrings(it["objects"])
		from, _ := it["materialsFrom"].(string)
		from = strings.TrimSpace(from)
		if kind != "outfit" && kind != "part" {
			bad(i, label, "kind 只能是 outfit 或 part（其他菜单项由程序处理）")
			continue
		}
		if label == "" {
			bad(i, "", "缺少 label")
			continue
		}
		if len([]rune(label)) > 16 {
			bad(i, label, "label 太长（最多 16 个字）")
			continue
		}
		if len(objects) == 0 {
			bad(i, label, "缺少 objects")
			continue
		}
		w := -1
		switch kind {
		case "outfit":
			if k, ok := owner[objects[0]]; ok && len(objects) == 1 {
				w = k
			} else {
				bad(i, label, "outfit 项的 objects 只能写一件衣服或头发的根物体（object）")
				continue
			}
			if from == "" {
				if bases[w] {
					bad(i, label, "「%s」有两个不带 materialsFrom 的穿上项", worn[w].label)
					continue
				}
				bases[w] = true
			} else {
				known := false
				for _, c := range worn[w].colours {
					known = known || c.Prefab == from
				}
				if !known {
					bad(i, label, "materialsFrom「%s」不是「%s」的配色", from, worn[w].label)
					continue
				}
				if colours[w][from]++; colours[w][from] > 1 {
					bad(i, label, "「%s」的配色「%s」出现了两次", worn[w].label, from)
					continue
				}
			}
		case "part":
			if from != "" {
				bad(i, label, "part 项不能写 materialsFrom")
				continue
			}
			ok := true
			for _, o := range objects {
				k, known := meshOf[o]
				switch {
				case !known:
					bad(i, label, "objects 里的「%s」不是列出的网格", o)
					ok = false
				case w >= 0 && k != w:
					bad(i, label, "一个部件里的网格分属几件衣服")
					ok = false
				case inPart[k][o] != "":
					bad(i, label, "网格「%s」已经放在部件「%s」里", o, inPart[k][o])
					ok = false
				default:
					w = k
					inPart[k][o] = label
				}
				if !ok {
					break
				}
			}
			if !ok {
				continue
			}
			if len(objects) >= len(worn[w].meshes) {
				bad(i, label, "这个部件包含了「%s」的全部网格：那是它本身，不做开关", worn[w].label)
				continue
			}
		}
		if !hasPrefix(path, worn[w].base) || len(path) > len(worn[w].base)+2 {
			bad(i, label, "path 要以 %s 开头，最多再往下两层", fmt.Sprint(worn[w].base))
			continue
		}
		empty := false
		for _, seg := range path {
			empty = empty || seg == ""
		}
		if empty {
			bad(i, label, "path 里有空的名字")
			continue
		}
		c := map[string]any{"kind": kind, "path": path, "label": label, "objects": objects}
		if kind == "outfit" {
			c["parameter"] = worn[w].param
			if from != "" {
				c["materialsFrom"] = from
			} else if worn[w].first {
				c["default"] = true
			}
		}
		mine[w] = append(mine[w], c)
	}
	for w, x := range worn {
		if !bases[w] {
			errs = append(errs, "「"+x.label+"」缺少不带 materialsFrom 的穿上项")
		}
		for _, c := range x.colours {
			if colours[w][c.Prefab] == 0 {
				errs = append(errs, "「"+x.label+"」缺少配色「"+c.Label+"」（materialsFrom "+c.Prefab+"）的项")
			}
		}
	}
	if len(errs) > 0 {
		return nil, 0, errs
	}
	// the outfit items first, then its parts: the order of a menu that is made new
	of := map[int]int{}
	for w, x := range worn {
		for _, i := range x.items {
			of[i] = w
		}
	}
	put := map[int]bool{}
	for i, p := range plan {
		w, theirs := of[i]
		if !theirs {
			merged = append(merged, p)
			continue
		}
		if put[w] {
			continue
		}
		put[w] = true
		for _, kind := range []string{"outfit", "part"} {
			for _, it := range mine[w] {
				if it["kind"] == kind {
					merged = append(merged, planned{p.root, it})
					n++
				}
			}
		}
	}
	// a page takes eight; and a name only once
	count, names := map[string]int{}, map[string]bool{}
	for _, p := range merged {
		page := p.root + "/" + strings.Join(anyStrings(p.item["path"]), "/")
		count[page]++
		l, _ := p.item["label"].(string)
		if names[page+"\x00"+l] {
			errs = append(errs, "菜单「"+strings.Join(anyStrings(p.item["path"]), " > ")+"」中有两项都叫「"+l+"」")
		}
		names[page+"\x00"+l] = true
	}
	for page, k := range count {
		if k > planMax {
			_, at, _ := strings.Cut(page, "/")
			errs = append(errs, fmt.Sprintf("菜单「%s」有 %d 项，超过 %d 项", strings.ReplaceAll(at, "/", " > "), k, planMax))
		}
	}
	if len(errs) > 0 {
		return nil, 0, errs
	}
	return merged, n, nil
}

// planOutcome: how the AI's plan went — first: it held as it came; fixed: after being told what was wrong;
// failed: it did not hold (the draft is built); error: no answer.
type planOutcome string

const (
	planFirst  planOutcome = "first"
	planFixed  planOutcome = "fixed"
	planFailed planOutcome = "failed"
	planError  planOutcome = "error"
)

// askMenuPlan asks the AI for the menus of what a run put on, as menuPlanPrompt says, once more with what was
// wrong. merged is the plan to build (the draft when the AI's does not hold); n the AI's items in it; errs what
// was wrong the last time (or the service's error).
func askMenuPlan(ctx context.Context, c *aiClient, worn []wornInfo, plan []planned, hierarchy string) (merged []planned, n int, how planOutcome, errs []string) {
	turns := []aiTurn{{Role: "user", Text: menuPlanRequest(worn, plan, hierarchy)}}
	system := menuPlanPrompt + langNote(menuLang())
	for try := 0; try < 2; try++ {
		out, err := c.chat(ctx, system, turns, nil)
		if err != nil {
			return plan, 0, planError, []string{err.Error()}
		}
		items, perr := parsePlanItems(out.Text)
		if perr == nil {
			m, k, bad := mergeMenuPlan(items, worn, plan)
			if len(bad) == 0 {
				return m, k, map[bool]planOutcome{true: planFirst, false: planFixed}[try == 0], nil
			}
			errs = bad
		} else {
			errs = []string{perr.Error()}
		}
		core.Logf("AI 的菜单方案有 %d 处问题：%s", len(errs), strings.Join(errs, "；"))
		shown := errs
		if len(shown) > 12 {
			shown = append(append([]string{}, shown[:12]...), fmt.Sprintf("……共 %d 处", len(errs)))
		}
		turns = append(turns, out, aiTurn{Role: "user", Text: "方案有这些问题，请改正后重新输出完整的 JSON：\n- " + strings.Join(shown, "\n- ")})
	}
	return plan, 0, planFailed, errs
}

// refineMenu: askMenuPlan for a run, as a step of its record. The plan is the AI's when it holds, else the
// draft as it was.
func (s *aiSession) refineMenu(ctx context.Context, c *aiClient, worn []wornInfo, plan []planned, hierarchy string) []planned {
	s.add(AIStep{Kind: "tool", Tool: "plan", Text: "AI 整理菜单方案", Busy: true})
	saveEvalSample(s.project, worn, plan, hierarchy)
	merged, n, how, errs := askMenuPlan(ctx, c, worn, plan, hierarchy)
	switch {
	case how == planFirst:
		s.finishStep(true, fmt.Sprintf("已采用 AI 的方案（%d 项）", n))
		core.Logf("AI 菜单方案：直接采用（%d 项）", n)
	case how == planFixed:
		s.finishStep(true, fmt.Sprintf("已采用 AI 修改后的方案（%d 项）", n))
		core.Logf("AI 菜单方案：修改一次后采用（%d 项）", n)
	case ctx.Err() != nil:
		s.finishStep(false, "已停止")
	case how == planError:
		s.finishStep(false, "AI 未给出方案（"+errs[0]+"），已使用按网格名称分组的方案")
	case len(errs) == 1 && strings.HasPrefix(errs[0], "回答不是要求的 JSON"):
		s.finishStep(false, "AI 的回答不是菜单方案，已使用按网格名称分组的方案")
	default:
		s.finishStep(false, fmt.Sprintf("AI 的方案有 %d 处问题，未采用，已使用按网格名称分组的方案", len(errs)))
	}
	return merged
}

const reviewPrompt = `你是「MioVRCA」里的 AI 改模助手。程序刚在玩家的 Unity 工程里给 VRChat 头像装好了素材、生成了菜单。下面是这次的记录、上传前体检的结果和截图。用简短的中文分点告诉玩家（不超过 10 行）：
- 截图里看到的问题：穿模、错位、整块洋红色（材质丢失）或纯白（贴图没挂上）、衣服没穿上。只说你确实看到的；你没有看到画面时，如实说这次没看图，请玩家自己看记录里的截图。正面图里头像的左手在画面右边。
- 体检里「需处理」的每一项：是什么问题、不处理会怎样、该怎么办；「建议优化」挑要紧的说。体检给的是构建前的数值，上传时 Modular Avatar、AAO、VRCFury 还会改动它们；整体评级照结果里的写法说。
- 记录里「需手动完成」「注意」的事项不用重复原文，需要时补充怎么做。
不要重复列出装了什么、菜单是什么样，程序会在你的话后面列出。不要说「没问题」「完美」：菜单开关的效果、骨骼物理、能不能上传都还没有确认。`

// reviewRun: what the AI says about the run, with its pictures and its check-up ("" when it says nothing).
func (s *aiSession) reviewRun(ctx context.Context, c *aiClient, record string, imgs []aiImage, checkup string) string {
	s.add(AIStep{Kind: "tool", Tool: "review", Text: "AI 检查结果", Busy: true})
	text := "这次的记录：\n" + record
	if checkup != "" {
		text += "\n\n上传前体检：\n" + checkup
	}
	var attach []aiImage
	if len(imgs) > 0 {
		said, at, _ := s.present(ctx, imgs, "看有没有穿模、错位、材质丢失、衣服没穿上")
		attach = at
		text += "\n\n截图（正面、背面）：" + said
	} else {
		text += "\n\n这次没有截图：你没有看到画面。"
	}
	out, err := c.chat(ctx, reviewPrompt+langNote(menuLang()), []aiTurn{{Role: "user", Text: text, Images: attach}}, nil)
	if err != nil {
		var he *aiHTTPError
		if len(attach) > 0 && errors.As(err, &he) && he.noPictures() { // the pictures were the trouble: once without them
			rememberSees(seesKey(c), false)
			out, err = c.chat(ctx, reviewPrompt+langNote(menuLang()), []aiTurn{{Role: "user", Text: text + "\n（这个模型不接受图片：你没有看到画面。）"}}, nil)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			s.finishStep(false, "已停止")
		} else {
			s.finishStep(false, "AI 未返回（"+err.Error()+"）")
		}
		return ""
	}
	s.finishStep(true, "")
	return strings.TrimSpace(out.Text)
}
