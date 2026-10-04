package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/unity"
)

// hairPartOf: parts of a hair style. The hair itself is not a part, whatever its mesh is called.
func hairPartOf(name string) string {
	g := partOf(name)
	if g != "头饰" {
		return g
	}
	n := strings.ToLower(name)
	if !strings.Contains(n, "hair") && !strings.Contains(n, "髪") && !strings.Contains(n, "头发") {
		return g
	}
	for _, w := range []string{"pin", "tie", "clip", "ribbon", "flower", "band", "acc", "orn", "deco"} {
		if strings.Contains(n, w) {
			return g
		}
	}
	return ""
}

// ownHair: the base body's own hair on the avatar (a mesh of the avatar itself named after hair).
func ownHair(av avatarInfo) string {
	for _, c := range av.Children {
		if c.Kind != "mesh" || (c.Prefab != "" && av.Prefab != "" && !strings.EqualFold(c.Prefab, av.Prefab)) {
			continue
		}
		n := strings.ToLower(c.Name)
		if strings.Contains(n, "hair") || strings.Contains(n, "髪") || strings.Contains(n, "头发") {
			return c.Name
		}
	}
	return ""
}

// groupParts: the part switches of a dressed thing, by the words in its mesh names, at most `room` of them.
func groupParts(meshes []string, partOf func(string) string, room int) (order []string, groups map[string][]string) {
	groups = map[string][]string{}
	for _, m := range meshes {
		g := partOf(baseName(m))
		if g == "" {
			continue
		}
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], m)
	}
	// the whole thing in one group: that is the thing itself, not a part
	for _, g := range order {
		if len(groups[g]) == len(meshes) {
			return nil, map[string][]string{}
		}
	}
	for len(order) > room && len(order) > 1 { // the smallest groups go together as 饰品, small things first
		small := -1
		for i, g := range order {
			if g == "饰品" {
				continue
			}
			if small < 0 || len(groups[g]) < len(groups[order[small]]) || (len(groups[g]) == len(groups[order[small]]) && partRank[g] > partRank[order[small]]) {
				small = i
			}
		}
		if small < 0 {
			break
		}
		g := order[small]
		order = append(order[:small], order[small+1:]...)
		if _, ok := groups["饰品"]; !ok {
			order = append(order, "饰品")
		}
		groups["饰品"] = append(groups["饰品"], groups[g]...)
		delete(groups, g)
	}
	sort.SliceStable(order, func(a, b int) bool { return partRank[order[a]] < partRank[order[b]] })
	return order, groups
}

// assetLabel: the short name a menu calls an asset: the library's, when it is short, else the prefab's.
func assetLabel(a aiAsset, fallback string) string {
	n := strings.TrimSpace(a.Name)
	if n != "" && len([]rune(n)) <= 10 {
		return n
	}
	return fallback
}

// the menu root the plugin makes when a plan names none, and the two limits an avatar is checked against on
// upload (synced parameter bits, PhysBone components)
const (
	defaultMenuRoot = "Avatar Menu"
	budgetMax       = 256
)

// onAvatar: an instance of this prefab is a child of the avatar already.
func onAvatar(av avatarInfo, prefab string) bool {
	for _, c := range av.Children {
		if c.Prefab != "" && strings.EqualFold(c.Prefab, prefab) {
			return true
		}
	}
	return false
}

// ---------- kinds that are not worn: plugins that install themselves, and skins ----------

// the kinds whose prefab goes under the avatar as it is: it brings its own menu, animator and parameters
var pluginKinds = map[string]bool{"表情": true, "光影": true, "互动": true, "面捕": true}

// the order the kinds are worked through, which is the order of the categories in a menu made new
var kindOrder = []string{"表情", "衣服", "头发", "皮肤", "配饰", "道具", "光影", "互动", "面捕"}

const (
	skinParam  = "Skin_Choose"
	stripLabel = "一键脱光"
)

// placed: what the plugin says about a prefab it put under the avatar as it is.
type placed struct {
	Existing       bool     `json:"existing"`
	Changed        bool     `json:"changed"`
	Object         string   `json:"object"`
	Warnings       []string `json:"warnings"`
	VRCFury        bool     `json:"vrcFury"`
	MenuInstallers []string `json:"menuInstallers"`
	PhysBones      int      `json:"physBones"`
	Bits           int      `json:"parameterBits"`
	BitsAdded      int      `json:"parameterBitsAdded"`
	AtLeast        bool     `json:"parameterBitsAtLeast"`
}

// oldPlugin: the project's copy of the Unity plugin does not know what was asked of it yet. Said so that the
// player knows what to do about it.
func oldPlugin(err error) error {
	if err == nil {
		return nil
	}
	if m := err.Error(); strings.Contains(m, "不认识的操作") || strings.Contains(m, "kind 只能是 outfit、part、toggle、strip，") {
		return &refusal{"工程中的 Unity 插件版本较旧，不支持这一类素材。请在「工程」步骤中点击「更新」，待 Unity 重新编译后重试"}
	}
	return err
}

// refusal: something that cannot be done with the project as it is, for a reason the player can act on.
type refusal struct{ msg string }

func (e *refusal) Error() string { return e.msg }

// unitySaidNo: Unity answered, and the answer is a reason the player can act on (not a lost connection).
func unitySaidNo(err error) bool {
	var be *unity.BridgeError
	var re *refusal
	return errors.As(err, &be) || errors.As(err, &re)
}

// folderHolds: what an asset folder of the project has in it, by kind of file. An asset that has no prefab
// to place is told by what it does have.
type folderHolds struct {
	prefabs, materials, textures, anims, controllers, assets, editorScripts int
}

func scanHolds(project, folder string) folderHolds {
	var h folderHolds
	if strings.HasSuffix(strings.ToLower(folder), ".prefab") {
		folder = path.Dir(folder)
	}
	root := filepath.Join(project, filepath.FromSlash(folder))
	_ = filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(d.Name())) {
		case ".prefab":
			h.prefabs++
		case ".mat":
			h.materials++
		case ".png", ".jpg", ".jpeg", ".tga", ".psd", ".tif", ".tiff", ".exr", ".bmp":
			h.textures++
		case ".anim":
			h.anims++
		case ".controller", ".overridecontroller":
			h.controllers++
		case ".asset", ".preset":
			h.assets++
		case ".cs":
			rel, _ := filepath.Rel(root, fp)
			if strings.Contains("/"+strings.ToLower(filepath.ToSlash(rel)), "/editor/") {
				h.editorScripts++
			}
		}
		return nil
	})
	return h
}

func (h folderHolds) String() string {
	var parts []string
	for _, x := range []struct {
		n    int
		unit string
	}{{h.prefabs, "个 prefab"}, {h.materials, "个材质"}, {h.textures, "张贴图"}, {h.anims, "个动画"}, {h.controllers, "个动画控制器"}, {h.assets, "个资产文件（预设等）"}, {h.editorScripts, "个编辑器脚本"}} {
		if x.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", x.n, x.unit))
		}
	}
	if len(parts) == 0 {
		return "未找到可识别的文件"
	}
	return "内有 " + strings.Join(parts, "、")
}

// baseFits: is an asset made for these base bodies (the library's 适配素体) right for the avatar, whose base
// body goes by these names? known is false when either side does not say.
func baseFits(bases, aliases []string) (fits, known bool) {
	if len(bases) == 0 || len(aliases) == 0 {
		return false, false
	}
	for _, b := range bases {
		for _, a := range aliases {
			if strings.EqualFold(strings.TrimSpace(b), strings.TrimSpace(a)) {
				return true, true
			}
		}
	}
	return false, true
}

// libraryBases fills in what the library knows about the rows of a run: the record each folder belongs to
// and the base bodies it is made for. A plugin's or a skin's row also says what its folder holds, which
// decides how it is installed.
func libraryBases(st *core.Store, project string, assets []aiAsset) {
	if len(assets) == 0 || !core.IsUnityProject(project) {
		return
	}
	rows := projectAssets(st, project)
	for i := range assets {
		if pluginKinds[assets[i].Kind] || assets[i].Kind == "皮肤" {
			assets[i].holds = scanHolds(project, assets[i].Folder).String()
		}
		for _, r := range rows {
			if r.Key != "" && under(assets[i].Folder, r.Folder) {
				assets[i].key, assets[i].bases = r.Key, r.Bases
				break
			}
		}
	}
}

// pickPlugin: the one prefab of a plugin's folder to put under the avatar — never a whole avatar; those that
// install themselves when some do; the version for this base body when the package has one per body. With
// several left there is no telling which is meant, and their names are given instead.
func pickPlugin(sub []prefabInfo, aliases []string) (pick *prefabInfo, names []string) {
	var cands []prefabInfo
	for _, x := range sub {
		if !x.WholeAvatar {
			cands = append(cands, x)
		}
	}
	narrow := func(keep func(prefabInfo) bool) {
		var some []prefabInfo
		for _, x := range cands {
			if keep(x) {
				some = append(some, x)
			}
		}
		if len(some) > 0 {
			cands = some
		}
	}
	narrow(func(x prefabInfo) bool { return x.SelfInstalling })
	narrow(func(x prefabInfo) bool {
		low := strings.ToLower(x.Path)
		for _, a := range aliases {
			if a != "" && strings.Contains(low, strings.ToLower(a)) {
				return true
			}
		}
		return false
	})
	if len(cands) == 1 {
		return &cands[0], nil
	}
	for _, x := range cands {
		names = append(names, x.Name)
	}
	return nil, names
}

// hasStrip: the avatar has a "take everything off" already — one the plugin marked for this parameter, or
// one made by hand, known the way the plugin knows it: a switch of its own that turns off every outfit of
// the parameter and nothing on; with a single outfit that is also what a part switch looks like, so then
// its name has to say it.
func hasStrip(av avatarInfo, param string) bool {
	worn := map[string]bool{} // the objects the parameter's outfits turn on
	var all []menuNode
	var walk func(n menuNode)
	walk = func(n menuNode) {
		all = append(all, n)
		if n.Parameter == param && !n.Auto {
			for _, t := range n.Toggles {
				if o, ok := strings.CutSuffix(t, "=on"); ok {
					worn[o] = true
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, n := range av.MaMenu {
		walk(n)
	}
	for _, n := range all {
		if n.Strip != "" {
			if n.Strip == param {
				return true
			}
			continue
		}
		if !n.Auto || len(n.Toggles) == 0 || len(worn) == 0 {
			continue
		}
		off := map[string]bool{}
		for _, t := range n.Toggles {
			if o, ok := strings.CutSuffix(t, "=off"); ok {
				off[o] = true
			}
		}
		covers := len(off) == len(n.Toggles)
		for o := range worn {
			covers = covers && off[o]
		}
		low := strings.ToLower(n.Label)
		if covers && (len(worn) >= 2 || strings.Contains(low, "脱") || strings.Contains(low, "裸") || strings.Contains(low, "strip")) {
			return true
		}
	}
	return false
}

// paramInUse: some menu item of the avatar is on this parameter.
func paramInUse(av avatarInfo, param string) bool {
	found := false
	var walk func(n menuNode)
	walk = func(n menuNode) {
		found = found || (n.Parameter == param && !n.Auto)
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, n := range av.MaMenu {
		walk(n)
	}
	return found
}

// hasPCSS: PCSS4VRC is on the avatar. At build time it swaps the materials the renderers have for their
// _pcss copies, and only those: a material a menu item puts on later is not converted.
func hasPCSS(av avatarInfo) bool {
	for _, c := range av.Children {
		if strings.Contains(strings.ToLower(c.Name+" "+strings.Join(c.Components, " ")), "pcss") {
			return true
		}
	}
	return false
}

// bodyMeshes: the avatar's own meshes a skin changes the materials of (not its hair).
func bodyMeshes(av avatarInfo) []string {
	var out []string
	for _, c := range av.Children {
		if c.Kind != "mesh" || (c.Prefab != "" && av.Prefab != "" && !strings.EqualFold(c.Prefab, av.Prefab)) {
			continue
		}
		if n := strings.ToLower(c.Name); strings.Contains(n, "hair") || strings.Contains(n, "髪") || strings.Contains(n, "头发") {
			continue
		}
		out = append(out, c.Name)
	}
	return out
}

func quickPipeline(ctx context.Context, st *core.Store, s *aiSession, assets []aiAsset, hierarchy string) error {
	p := s.project
	if len(assets) == 0 {
		return errors.New("未勾选要装配的素材")
	}
	step := func(tool, title string, f func() (string, error)) error {
		s.add(AIStep{Kind: "tool", Tool: tool, Text: title, Busy: true})
		out, err := f()
		if err != nil {
			s.finishStep(false, err.Error())
			return err
		}
		s.finishStep(true, out)
		return nil
	}
	levels := parseHierarchy(hierarchy)
	var notes, warns []string

	// ---- the avatar, placed from a base body when the scene has none ----
	inspect := func() ([]avatarInfo, error) {
		raw, err := unity.BridgeCall(ctx, p, "inspect", map[string]any{}, 0)
		if err != nil {
			return nil, err
		}
		var o struct {
			Avatars []avatarInfo `json:"avatars"`
		}
		_ = json.Unmarshal(raw, &o)
		var act []avatarInfo
		for _, a := range o.Avatars {
			if a.Active {
				act = append(act, a)
			}
		}
		if len(act) == 0 {
			act = o.Avatars
		}
		return act, nil
	}
	var avs []avatarInfo
	if err := step("inspect_avatar", "查看模型和现有菜单", func() (string, error) {
		var err error
		if avs, err = inspect(); err != nil {
			return "", err
		}
		if len(avs) == 0 {
			return "场景中暂无模型", nil
		}
		names := []string{}
		for _, a := range avs {
			names = append(names, a.Name)
		}
		return "模型「" + strings.Join(names, "」「") + "」", nil
	}); err != nil {
		return err
	}
	var bases []aiAsset
	for _, a := range assets {
		if a.Kind == "素体" {
			bases = append(bases, a)
		}
	}
	listPrefabs := func(dirs []string) ([]prefabInfo, []string, error) {
		get := func() ([]prefabInfo, []string, error) {
			raw, err := unity.BridgeCall(ctx, p, "prefabs", map[string]any{"folders": dirs, "limit": 300}, 0)
			if err != nil {
				return nil, nil, err
			}
			var o struct {
				Prefabs  []prefabInfo `json:"prefabs"`
				NotFound []string     `json:"notFound"`
			}
			_ = json.Unmarshal(raw, &o)
			return o.Prefabs, o.NotFound, nil
		}
		list, missing, err := get()
		if err == nil && (len(missing) > 0 || len(list) == 0) { // just imported: Unity has not read the files yet
			if _, e := unity.BridgeCall(ctx, p, "refresh", map[string]any{}, 5*time.Minute); e == nil {
				list, missing, err = get()
			}
		}
		return list, missing, err
	}
	folderDir := func(f string) string {
		if strings.HasSuffix(strings.ToLower(f), ".prefab") {
			return path.Dir(f)
		}
		return f
	}
	switch {
	case len(avs) == 0 && len(bases) > 0:
		b := bases[0]
		var body prefabInfo
		if err := step("list_prefabs", "查找素体 prefab："+b.Folder, func() (string, error) {
			list, missing, err := listPrefabs([]string{folderDir(b.Folder)})
			if err != nil {
				return "", err
			}
			if len(missing) > 0 {
				return "", errors.New("未找到文件夹：" + strings.Join(missing, "、"))
			}
			var whole []prefabInfo
			for _, x := range list {
				if !x.WholeAvatar || !under(x.Path, folderDir(b.Folder)) {
					continue
				}
				if strings.HasSuffix(strings.ToLower(b.Folder), ".prefab") && !strings.EqualFold(x.Path, b.Folder) {
					continue
				}
				whole = append(whole, x)
			}
			if len(whole) == 0 {
				return "", errors.New("「" + b.Folder + "」中没有完整模型的 prefab（需带 VRC Avatar Descriptor），请更换素体，或先在 Unity 中打开包含模型的场景")
			}
			// the plain PC version first: not a Quest/lite/face-tracking variant, then the fewest words in its name
			sort.SliceStable(whole, func(i, j int) bool {
				odd := func(x prefabInfo) int {
					n := strings.ToLower(x.Name)
					o := 0
					for _, w := range []string{"quest", "android", "lite", "low", "facetrack", "face track", "ft_", "_ft", "mobile", "fallback"} {
						if strings.Contains(n, w) {
							o++
						}
					}
					return o
				}
				if odd(whole[i]) != odd(whole[j]) {
					return odd(whole[i]) < odd(whole[j])
				}
				return len(whole[i].Name) < len(whole[j].Name)
			})
			body = whole[0]
			out := "素体 prefab「" + body.Name + "」"
			if len(whole) > 1 {
				out += fmt.Sprintf("（共 %d 个完整模型的 prefab，已选用此项）", len(whole))
			}
			return out, nil
		}); err != nil {
			return err
		}
		if err := step("place_avatar", "将素体「"+strings.TrimSuffix(body.Name, ".prefab")+"」放入新场景", func() (string, error) {
			raw, err := unity.BridgeCall(ctx, p, "place_avatar", map[string]any{"prefab": body.Path}, 3*time.Minute)
			if err != nil {
				return "", err
			}
			var o struct {
				Scene string `json:"scene"`
			}
			_ = json.Unmarshal(raw, &o)
			notes = append(notes, "已新建场景 "+o.Scene+"，素体「"+body.Name+"」已放入并保存")
			if avs, err = inspect(); err != nil {
				return "", err
			}
			if len(avs) == 0 {
				return "", errors.New("素体已放入场景，但 Unity 未检测到模型，请查看 Console 是否有报错")
			}
			return "新场景 " + o.Scene, nil
		}); err != nil {
			return err
		}
	case len(bases) > 0:
		notes = append(notes, "场景中已有模型「"+avs[0].Name+"」，素体「"+assetLabel(bases[0], baseName(bases[0].Folder))+"」未重新放置（如需更换素体，请先在 Unity 中新建场景）")
	}
	var av avatarInfo
	switch len(avs) {
	case 0:
		return errors.New("当前场景中没有模型：请勾选一个素体由流水线放入场景，或先在 Unity 中打开包含模型的场景")
	case 1:
		av = avs[0]
	default:
		names := []string{}
		for _, a := range avs {
			names = append(names, a.Name)
		}
		return errors.New("场景中有 " + fmt.Sprint(len(avs)) + " 个模型（" + strings.Join(names, "、") + "）：请在 Unity 中仅保留一个处于显示状态，或取消勾选「不使用 AI」并说明要处理哪一个")
	}
	aliases := baseAliases(st, av)
	s.recAvatar(av.Name, av.Prefab)
	bodyName := av.Name // what the avatar's base body is called, in messages
	if len(aliases) > 0 {
		bodyName = aliases[0]
	}

	// ---- what the folders hold ----
	var wear []aiAsset
	for _, a := range assets {
		if a.Kind != "素体" {
			wear = append(wear, a)
		}
	}
	if len(wear) == 0 {
		s.add(AIStep{Kind: "say", Text: strings.Join(append(notes, "除素体外未勾选其他素材，流水线已结束。"), "。")})
		return nil
	}
	worn, ftPicked := false, false // something of this run is worn (an outfit, hair …); face tracking is among its rows
	for _, a := range wear {
		worn = worn || !pluginKinds[a.Kind] && a.Kind != "皮肤"
		ftPicked = ftPicked || a.Kind == "面捕"
	}
	var list []prefabInfo
	var dirs []string
	for _, a := range wear {
		dirs = append(dirs, folderDir(a.Folder))
	}
	dirs = core.UniqStrings(dirs)
	if err := step("list_prefabs", "查找 prefab："+strings.Join(dirs, "、"), func() (string, error) {
		var missing []string
		var err error
		if list, missing, err = listPrefabs(dirs); err != nil {
			return "", err
		}
		if len(list) == 0 {
			if len(missing) > 0 && (worn || len(missing) == len(dirs)) {
				return "", errors.New("未找到文件夹：" + strings.Join(missing, "、"))
			}
			if worn {
				return "", errors.New("所选文件夹中没有 prefab")
			}
			// a skin or a plugin may well have none: each says below what it holds instead
		}
		out := fmt.Sprintf("%d 个 prefab", len(list))
		if len(missing) > 0 {
			out += "；未找到文件夹 " + strings.Join(missing, "、")
		}
		return out, nil
	}); err != nil {
		return err
	}
	prefabsOf := func(a aiAsset) []prefabInfo {
		var out []prefabInfo
		for _, x := range list {
			if strings.HasSuffix(strings.ToLower(a.Folder), ".prefab") {
				if strings.EqualFold(x.Path, a.Folder) {
					out = append(out, x)
				}
			} else if under(x.Path, a.Folder) {
				out = append(out, x)
			}
		}
		return out
	}

	// ---- the menus the avatar has, and what becomes a default ----
	root, param := outfitMenu(av)
	hairRoot, hairParam := hairMenu(av)
	if param == "" {
		param = "Clothtoggle"
	}
	if hairParam == "" {
		hairParam = "Hair_Choose"
	}
	// the first outfit becomes the default only on an avatar that has no outfit menu and no default yet
	outfitDefault := root == "" && !wearsByDefault(av, param)
	hairDefault := hairRoot == "" && !wearsByDefault(av, hairParam)
	// each kind goes under the menu the avatar has for it: hair under the hair menu when there is one,
	// everything else under the outfit menu (or the plugin's own)
	rootOf := func(kind string) string {
		r := root
		if kind == "头发" && hairRoot != "" {
			r = hairRoot
		}
		if r == "" {
			r = defaultMenuRoot
		}
		return r
	}

	bones := 0 // PhysBones on the avatar, as the last step told
	verbs := map[string]string{"衣服": "装配", "头发": "装配", "配饰": "装配", "道具": "装配"}
	dress := func(kind, label, prefab string, active bool) (object string, meshes []string, err error) {
		err = step("dress", verbs[kind]+"「"+label+"」", func() (string, error) {
			args := map[string]any{"avatar": av.Path, "prefab": prefab}
			// shown or hidden is said only for what this run puts on: an outfit that is on the avatar already
			// keeps the state it has (a second run must not hide the default outfit)
			if !onAvatar(av, prefab) {
				args["active"] = active
			}
			raw, err := unity.BridgeCall(ctx, p, "dress", args, 3*time.Minute)
			if err != nil {
				return "", err
			}
			var r struct {
				Existing  bool     `json:"existing"`
				Changed   *bool    `json:"changed"`
				Warnings  []string `json:"warnings"`
				PhysBones int      `json:"physBones"`
				Outfit    struct {
					Object string `json:"object"`
					Meshes []struct {
						Path string `json:"path"`
					} `json:"meshes"`
				} `json:"outfit"`
			}
			_ = json.Unmarshal(raw, &r)
			object, bones = r.Outfit.Object, max(bones, r.PhysBones)
			for _, m := range r.Outfit.Meshes {
				meshes = append(meshes, m.Path)
			}
			changed := r.Changed != nil && *r.Changed || r.Changed == nil && !r.Existing // an older plugin does not say
			if changed {
				s.changed(false)
			}
			if !r.Existing {
				s.did(func(d *runDone) { d.dressed = append(d.dressed, label) })
			}
			rec := recStep{Prefab: prefab, Object: object}
			if v, ok := args["active"].(bool); ok {
				rec.Active = &v
			}
			s.recStep(rec, changed)
			out := fmt.Sprintf("%d 个网格", len(meshes))
			if r.Existing {
				out = "已在模型上，" + out
			}
			if len(r.Warnings) > 0 {
				warns = append(warns, r.Warnings...)
				out += "。注意：" + strings.Join(r.Warnings, "；")
			}
			return out, nil
		})
		return
	}

	type planned struct {
		root string
		item map[string]any
	}
	var plan []planned
	var done []string
	made := map[string]bool{}   // kinds that got menu items
	labels := map[string]bool{} // menu names in use, by kind
	// named: the menu name of what was just put on. Two assets that would be called the same (two prefabs both
	// named Dress) must not share one menu entry: the later one goes by its object's name, which is its own.
	named := func(kind, label, object string) string {
		for n, try := 2, baseName(object); labels[kind+"/"+label]; n++ {
			if label = try; labels[kind+"/"+label] {
				label = fmt.Sprintf("%s (%d)", try, n)
			}
		}
		labels[kind+"/"+label] = true
		return label
	}
	ownHairDone := false
	var put []string // plugins put under the avatar as they are
	var skins []string
	var todo []string // what is left for the player to do by hand, one line each
	bitsNow := 0      // synced parameter bits, as the last step that counted them told
	ftOn := av.FT     // face tracking is on the avatar (it was, or this run put it there)
	nameOf := func(a aiAsset) string {
		return assetLabel(a, strings.TrimSuffix(baseName(a.Folder), ".prefab"))
	}
	madeFor := func(a aiAsset) string { return "「" + strings.Join(a.bases, "、") + "」" }

	// ---- a plugin: its prefab goes under the avatar as it is, its own menu into the category's ----
	plugin := func(kind string, a aiAsset, add func(map[string]any), base []string) error {
		label := nameOf(a)
		sub := prefabsOf(a)
		holds := scanHolds(p, a.Folder)
		isPrefab := strings.HasSuffix(strings.ToLower(a.Folder), ".prefab")
		there := false // one of its prefabs is on the avatar already
		for _, x := range sub {
			there = there || onAvatar(av, x.Path)
		}
		fits, known := baseFits(a.bases, aliases)
		switch {
		case kind == "表情" && (ftOn || ftPicked):
			todo = append(todo, "「"+label+"」未安装："+map[bool]string{true: "模型上已有面捕", false: "本次同时选择了面捕"}[ftOn]+"，面捕会取代手势表情，按约定面捕模型不再制作表情菜单")
			return nil
		case kind == "面捕" && ftOn && !there:
			notes = append(notes, "模型上已有面捕，「"+label+"」未重复安装")
			return nil
		case known && !fits && kind == "面捕":
			todo = append(todo, "「"+label+"」未安装：它为"+madeFor(a)+"制作，当前模型的素体为「"+bodyName+"」。面捕依赖素体的脸部网格和形态键，不能跨素体使用")
			return nil
		case known && !fits && kind == "表情":
			todo = append(todo, "「"+label+"」未安装：它为"+madeFor(a)+"制作，当前模型的素体为「"+bodyName+"」。表情动画写入的是该素体脸部的形态键，不能跨素体使用")
			return nil
		case known && !fits:
			warns = append(warns, "「"+label+"」为"+madeFor(a)+"制作，当前模型的素体为「"+bodyName+"」，位置或尺寸可能不匹配，请在 Unity 中检查")
		case !known && kind == "面捕":
			notes = append(notes, "未能核对「"+label+"」与模型的素体是否一致（素材库未标注适配素体，或未识别出模型的素体），请自行确认面捕对应的素体及其版本")
		}
		// face tracking that comes with an installer of its own has to add shape keys to the face mesh: its
		// prefabs alone do nothing
		if kind == "面捕" && holds.editorScripts > 0 && !isPrefab && !there {
			how := "在 Unity 菜单栏中打开该面捕自带的安装窗口"
			if strings.Contains(strings.ToLower(a.Folder), "triturbo") {
				how += "（TriturboFT > 素体名 FT）"
			}
			todo = append(todo, "「"+label+"」需使用其自带的安装工具安装，流水线未改动模型：它会为脸部网格补充面捕形态键，直接放置 prefab 不会生效。步骤："+how+
				"，将模型拖入 Avatar 栏，选择眼部和嘴部的精度后点击安装（Apply Face Tracking Addon）。安装前请确认素体版本在该面捕支持的范围内；精度越高占用的同步参数越多（高精度的眼部加嘴部约 160 位，上限 256 位）")
			return nil
		}
		pick, names := pickPlugin(sub, aliases)
		if pick == nil && len(names) == 0 {
			line := "「" + label + "」未放置：其中没有可放置的 prefab（" + holds.String() + "）。"
			switch {
			case kind == "表情":
				line += "表情需要将动画接入模型 FX 动画控制器的手势层或表情轮盘，涉及修改控制器，流水线不自动处理，请按素材说明在 Unity 中设置"
			case holds.editorScripts > 0:
				line += "该素材带有编辑器脚本，通常在 Unity 菜单栏中有自己的安装窗口，请按素材说明安装"
			default:
				line += "请按素材说明在 Unity 中安装"
			}
			todo = append(todo, line)
			return nil
		}
		if pick == nil {
			if len(names) > 5 {
				names = append(names[:5], fmt.Sprintf("等 %d 个", len(names)))
			}
			todo = append(todo, "「"+label+"」未放置：其中有多个可放置的 prefab（"+strings.Join(names, "、")+"），无法判断应使用哪一个。请在「素材」步骤的手动添加路径中填写要放置的 prefab 路径，类别选择「"+kind+"」后重新运行；素材带有安装说明时以说明为准")
			return nil
		}
		if pick.MissingScripts > 0 {
			todo = append(todo, fmt.Sprintf("「%s」未放置：prefab「%s」中有 %d 个缺失的脚本，它依赖的插件尚未安装到工程中。请先按素材说明安装所需插件（常见的有 Modular Avatar、VRCFury 或素材自带的框架包）后重新运行", label, pick.Name, pick.MissingScripts))
			return nil
		}
		var r placed
		err := step("place_prefab", "放置「"+label+"」", func() (string, error) {
			raw, err := unity.BridgeCall(ctx, p, "place", map[string]any{"avatar": av.Path, "prefab": pick.Path}, 3*time.Minute)
			if err != nil {
				return "", oldPlugin(err)
			}
			_ = json.Unmarshal(raw, &r)
			if r.Changed {
				s.changed(false)
				s.did(func(d *runDone) { d.dressed = append(d.dressed, label) })
			}
			s.recStep(recStep{Prefab: pick.Path, Object: r.Object, Place: true}, r.Changed)
			bones, bitsNow = max(bones, r.PhysBones), r.Bits
			out := "已放置，保持原名和原有结构"
			if r.Existing {
				out = "已在模型上"
			}
			if len(r.Warnings) > 0 {
				warns = append(warns, r.Warnings...)
				out += "。注意：" + strings.Join(r.Warnings, "；")
			}
			return out, nil
		})
		if err != nil {
			if !unitySaidNo(err) {
				return err
			}
			todo = append(todo, "「"+label+"」未放置："+err.Error()) // Unity's reason is the player's to act on; the rest of the run goes on
			return nil
		}
		if r.Object == "" {
			return nil
		}
		put = append(put, label)
		if kind == "面捕" {
			ftOn = true
			line := "面捕「" + label + "」已放置，其菜单按约定保留在主菜单的「Face Tracking」，未移入分类。还需确认：手势表情与面捕冲突，面捕模型通常不保留 FX 中的手势表情层，需在 Unity 中自行处理；游戏内需开启 OSC 并运行面捕软件"
			if r.Bits > 0 {
				line += fmt.Sprintf("；同步参数现预计%s %d / %d 位", map[bool]string{true: "至少", false: ""}[r.AtLeast], r.Bits, budgetMax)
			}
			todo = append(todo, line)
			return nil
		}
		if len(r.MenuInstallers) == 0 {
			if r.VRCFury {
				todo = append(todo, "「"+label+"」的菜单由 VRCFury 在构建时生成，流水线未移动。如需放入「"+kindCategory[kind]+"」，请在 Unity 中为它的 VRCFury 组件添加 Move Menu Item，或修改该插件自带的菜单路径设置")
			} else {
				notes = append(notes, "「"+label+"」没有自带的菜单安装器（MA Menu Installer），未生成菜单项")
			}
			return nil
		}
		add(map[string]any{"kind": "install", "path": base, "label": label, "objects": []string{r.Object}})
		return nil
	}

	// ---- a skin: another set of materials for the body, picked in the menu ----
	skinMenu := paramInUse(av, skinParam)
	skin := func(a aiAsset, add func(map[string]any), base []string) {
		label := nameOf(a)
		if fits, known := baseFits(a.bases, aliases); known && !fits {
			todo = append(todo, "「"+label+"」未使用：它为"+madeFor(a)+"制作，当前模型的素体为「"+bodyName+"」。皮肤贴图按素体的 UV 绘制，不能跨素体使用")
			return
		}
		body := bodyMeshes(av)
		// a prefab with meshes named as the body's carries the skin's materials on them
		var src []prefabInfo
		for _, x := range prefabsOf(a) {
			shared := false
			for _, m := range x.MeshNames {
				shared = shared || core.ContainsStr(body, m)
			}
			if shared {
				src = append(src, x)
			}
		}
		if len(src) == 0 {
			holds := scanHolds(p, a.Folder)
			line := "「" + label + "」未生成皮肤菜单：其中没有带身体网格的 prefab（" + holds.String() + "），流水线无法判断材质对应哪个网格。请在 Unity 中复制素体原版的身体、脸部材质，套用该皮肤的贴图或 lilToon 预设后替换到对应的材质槽"
			if holds.materials > 0 {
				line += "；如素材已带有现成材质，也可使用 AI 运行流水线，并在对话中说明哪个材质用于哪个网格，由 AI 生成皮肤切换项"
			}
			todo = append(todo, line)
			return
		}
		if len(aliases) > 0 {
			var mine []prefabInfo
			for _, x := range src {
				for _, al := range aliases {
					if strings.Contains(strings.ToLower(x.Path), strings.ToLower(al)) {
						mine = append(mine, x)
						break
					}
				}
			}
			if len(mine) > 0 {
				src = mine
			}
		}
		if len(src) > 6 {
			notes = append(notes, fmt.Sprintf("「%s」中有 %d 个皮肤 prefab，仅使用前 6 个", label, len(src)))
			src = src[:6]
		}
		// the look the scene has stays choosable
		if !skinMenu {
			skinMenu = true
			add(map[string]any{"kind": "skin", "path": base, "label": "原版皮肤", "default": true, "parameter": skinParam})
		}
		for _, x := range src {
			l := label
			if len(src) > 1 {
				l = tidyName(x.Name, aliases)
			}
			l = named("皮肤", l, x.Name)
			add(map[string]any{"kind": "skin", "path": base, "label": l, "objects": body, "materialsFrom": x.Path, "parameter": skinParam})
			skins = append(skins, l)
		}
	}

	for _, kind := range kindOrder {
		base, perAsset := menuPath(levels, kind)
		add := func(it map[string]any) {
			plan = append(plan, planned{rootOf(kind), it})
			made[kind] = true
		}
		for _, a := range wear {
			if a.Kind != kind {
				continue
			}
			if pluginKinds[kind] {
				if err := plugin(kind, a, add, base); err != nil {
					return err
				}
				continue
			}
			if kind == "皮肤" {
				skin(a, add, base)
				continue
			}
			sub := prefabsOf(a)
			if len(sub) == 0 {
				warns = append(warns, "「"+a.Folder+"」中没有 prefab，已跳过")
				continue
			}
			outfits, skipped := pickOutfits(sub, aliases)
			if len(outfits) == 0 {
				warns = append(warns, "「"+a.Folder+"」中没有可装配的 prefab（均为完整模型），已跳过")
				continue
			}
			if len(outfits) > 6 {
				for _, o := range outfits[6:] {
					skipped = append(skipped, o.label)
				}
				outfits = outfits[:6]
			}
			if len(skipped) > 0 {
				notes = append(notes, "「"+assetLabel(a, baseName(a.Folder))+"」中未装配："+strings.Join(skipped, "、"))
			}
			for i, o := range outfits {
				label := o.label
				if len(outfits) == 1 {
					label = assetLabel(a, o.label)
				}
				if len(o.others) > 0 {
					warns = append(warns, "「"+label+"」有多个素体版本（"+strings.Join(append([]string{o.version}, o.others...), "、")+"），未能从路径判断哪个适用于「"+av.Name+
						"」，已装配 "+o.version+" 版本；如不合身，请只勾选对应版本的 prefab 后重试")
				}
				names := []string{o.base.Name}
				for _, c := range o.colours {
					names = append(names, c.Name)
				}
				switch kind {
				case "衣服", "头发":
					pm, isHair := param, kind == "头发"
					first := i == 0 && outfitDefault
					if isHair {
						pm = hairParam
						first = i == 0 && hairDefault
						if first {
							hairDefault = false
						}
					} else if first {
						outfitDefault = false
					}
					object, meshes, err := dress(kind, label, o.base.Path, first)
					if err != nil {
						return err
					}
					if object == "" {
						continue
					}
					label = named(kind, label, object)
					done = append(done, label)
					// the body's own hair stays choosable: one picker for it, so a new hair can turn it off
					if isHair && !ownHairDone {
						ownHairDone = true
						if own := ownHair(av); own != "" {
							add(map[string]any{"kind": "outfit", "path": base, "label": "原装头发", "objects": []string{own}, "parameter": pm})
						}
					}
					partOf := partOf
					if isHair {
						partOf = hairPartOf
					}
					order, groups := groupParts(meshes, partOf, 8)
					// hair with one colour and nothing to switch sits on the category level itself
					own := perAsset && (!isHair || len(o.colours) > 0 || len(order) > 0)
					if !own {
						add(map[string]any{"kind": "outfit", "path": base, "label": label, "objects": []string{object}, "default": first, "parameter": pm})
						continue
					}
					where := append(append([]string{}, base...), label)
					pickers := 1 + len(o.colours)
					colourPath := where
					if pickers+len(order) > 8 && pickers > 1 {
						colourPath, pickers = append(append([]string{}, where...), "配色"), 1
					}
					if pickers+len(order) > 8 {
						order, groups = groupParts(meshes, partOf, 8-pickers)
					}
					pick := "穿上"
					if isHair {
						pick = "戴上"
					}
					if len(o.colours) > 0 {
						pick = colourLabel(o.base, names, aliases)
					}
					add(map[string]any{"kind": "outfit", "path": colourPath, "label": pick, "objects": []string{object}, "default": first, "parameter": pm})
					for _, c := range o.colours {
						add(map[string]any{"kind": "outfit", "path": colourPath, "label": colourLabel(c, names, aliases), "objects": []string{object}, "materialsFrom": c.Path, "parameter": pm})
					}
					for _, g := range order {
						add(map[string]any{"kind": "part", "path": where, "label": g, "objects": groups[g]})
					}
				case "配饰":
					object, _, err := dress(kind, label, o.base.Path, true)
					if err != nil {
						return err
					}
					if object == "" {
						continue
					}
					label = named(kind, label, object)
					done = append(done, label)
					if len(o.colours) > 0 {
						notes = append(notes, "「"+label+"」另有其他配色（"+strings.Join(names[1:], "、")+"），配饰开关仅使用第一个")
					}
					add(map[string]any{"kind": "toggle", "path": base, "label": label, "objects": []string{object}, "default": true})
				case "道具":
					object, _, err := dress(kind, label, o.base.Path, false)
					if err != nil {
						return err
					}
					if object == "" {
						continue
					}
					label = named(kind, label, object)
					done = append(done, label)
					if len(o.colours) > 0 {
						notes = append(notes, "「"+label+"」另有其他配色（"+strings.Join(names[1:], "、")+"），道具开关仅使用第一个")
					}
					if perAsset {
						add(map[string]any{"kind": "toggle", "path": append(append([]string{}, base...), label), "label": "显示", "objects": []string{object}, "default": false})
					} else {
						add(map[string]any{"kind": "toggle", "path": base, "label": label, "objects": []string{object}, "default": false})
					}
				}
			}
		}
		// "take everything off" closes an outfit menu: last on the category's level, after every outfit, on a
		// switch of its own. One the avatar has already learns the new outfits by itself.
		if kind == "衣服" && made[kind] && !hasStrip(av, param) {
			add(map[string]any{"kind": "strip", "path": base, "label": stripLabel, "parameter": param})
			notes = append(notes, "已在衣服菜单末尾添加「"+stripLabel+"」：它只脱下该菜单中的衣服，素体自带的内衣和原装衣服不受其控制（如需一并隐藏，可在 Unity 中将对应物体加入该菜单项的 MA Object Toggle 并取消勾选）")
		}
	}
	say := func(text string) {
		if len(notes) > 0 {
			text += "\n" + strings.Join(core.UniqStrings(notes), "；") + "。"
		}
		if len(todo) > 0 {
			text += "\n\n需手动完成：\n- " + strings.Join(core.UniqStrings(todo), "\n- ")
		}
		if len(warns) > 0 {
			text += "\n\n注意：" + strings.Join(core.UniqStrings(warns), "；")
		}
		s.add(AIStep{Kind: "say", Text: text})
	}
	for _, pl := range plan {
		if m, _ := pl.item["materialsFrom"].(string); m != "" && hasPCSS(av) {
			warns = append(warns, "模型上装有 PCSS4VRC：它在构建时只将网格上现有的材质换成带阴影的 _pcss 版本，配色项和皮肤项替换的材质不会被转换，切换后可能没有 PCSS 阴影。可在 Unity 中将对应 MA Material Setter 的目标材质改为同目录下的 _pcss 版本")
			break
		}
	}
	if len(plan) == 0 && len(put) == 0 {
		if len(todo) == 0 {
			return errors.New("未装配任何素材：" + strings.Join(warns, "；"))
		}
		// nothing the line can do by itself: said plainly, with what is left to do
		say("流水线未改动模型。")
		return nil
	}
	// one plan per menu root, in the order their first items come
	var roots, where []string
	byRoot := map[string][]map[string]any{}
	for _, pl := range plan {
		if _, ok := byRoot[pl.root]; !ok {
			roots = append(roots, pl.root)
		}
		byRoot[pl.root] = append(byRoot[pl.root], pl.item)
	}
	bits := bitsNow // synced parameter bits, as the plugin estimates them after the last step
	for _, r := range roots {
		items := localMenu(menuLang(), av.MaMenu, r, byRoot[r]) // in the player's language, by the names the menu has already
		title := fmt.Sprintf("生成菜单（%d 项）", len(items))
		if len(roots) > 1 {
			title = fmt.Sprintf("生成菜单「%s」（%d 项）", r, len(items))
		}
		if err := step("build_menu", title, func() (string, error) {
			args := map[string]any{"avatar": av.Path, "items": items, "parameter": param}
			if r != defaultMenuRoot {
				args["root"] = r
			}
			raw, err := unity.BridgeCall(ctx, p, "build_menu", args, 5*time.Minute)
			if err != nil {
				return "", oldPlugin(err)
			}
			var res struct {
				Created  []string `json:"created"`
				Warnings []string `json:"warnings"`
				Icons    int      `json:"icons"`
				Root     string   `json:"root"`
				Changed  *bool    `json:"changed"`
				Bits     int      `json:"parameterBits"`
			}
			_ = json.Unmarshal(raw, &res)
			changed := res.Changed == nil || *res.Changed // an older plugin does not say: counted, as before
			if changed {
				s.changed(false)
				s.did(func(d *runDone) { d.menus++ })
			}
			root, _ := args["root"].(string)
			s.recMenu(root, param, items, changed)
			warns = append(warns, res.Warnings...)
			where, bits = append(where, res.Root), res.Bits
			out := fmt.Sprintf("新建 %d 项，图标 %d 张", len(res.Created), res.Icons)
			switch {
			case res.Changed != nil && !*res.Changed:
				out = "菜单已是最新，未作改动"
			case len(res.Created) == 0 && res.Changed != nil:
				out = "已更新现有菜单项，无新建项"
			case len(res.Created) == 0:
				out = "菜单已是最新，无新建项"
			}
			return out, nil
		}); err != nil {
			return err
		}
	}
	// the two limits an upload fails on: said here too when the plugin's own warnings did not
	said := strings.Join(warns, "\n")
	if bones > budgetMax && !strings.Contains(said, "PhysBone") {
		warns = append(warns, fmt.Sprintf("模型上共有 %d 个 PhysBone，超过 %d 个的上限，上传时将校验失败（隐藏的物体同样计入）", bones, budgetMax))
	}
	if bits > budgetMax && !strings.Contains(said, "同步参数") {
		warns = append(warns, fmt.Sprintf("同步参数预计 %d / %d 位，超出上限将导致上传失败，可减少部件开关的数量", bits, budgetMax))
	}
	text := ""
	if len(done) > 0 {
		text += "已装配：" + strings.Join(done, "、") + "。"
	}
	if len(skins) > 0 {
		text += "已添加皮肤切换项：" + strings.Join(skins, "、") + "（与「原版皮肤」互斥）。"
	}
	if len(put) > 0 {
		text += "已放置插件：" + strings.Join(put, "、") + "（保持原名和原有结构，未添加开关）。"
	}
	if len(where) > 0 {
		text += "菜单位于模型下的「" + strings.Join(core.UniqStrings(where), "」「") + "」，按「" + hierarchy + "」排列。"
	}
	if root != "" && made["衣服"] {
		text += "模型已有衣服菜单，新衣服已加入其中（参数 " + param + "）。"
	}
	if hairRoot != "" && made["头发"] {
		text += "头发已加入原有的头发菜单（参数 " + hairParam + "）。"
	}
	// a look at the result, for the player (an older plugin that cannot photograph is no reason to fail)
	shots := 0
	s.add(AIStep{Kind: "tool", Tool: "look", Text: "为模型截图", Busy: true})
	if imgs, _, err := takeShots(ctx, p, map[string]any{"avatar": av.Path, "views": []string{"front", "back"}}); err != nil {
		s.finishStep(false, err.Error())
	} else {
		shots = len(imgs)
		s.showImgs(saveShots(imgs))
		s.finishStep(true, "正面和背面，点击图片可放大。截图为场景当前显示的状态，菜单开关的效果需在 Play 模式中查看")
	}
	if shots > 0 {
		text += "\n上方为模型当前的正面和背面截图，请检查是否有明显穿模、错位或整块洋红色（材质丢失）。"
	}
	if made["衣服"] || made["头发"] {
		text += "\n部件按网格名称中的关键词分组，分组有误时可让 AI 重新整理，或在 Unity 中手动调整。"
	}
	text += "\n菜单效果需在 Play 模式中用 Gesture Manager 逐项测试；确认无误后按 Ctrl+S 保存场景，如需回退按 Ctrl+Z。"
	say(text)
	return nil
}
