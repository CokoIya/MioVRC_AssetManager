package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
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

func quickPipeline(ctx context.Context, st *core.Store, s *aiSession, assets []aiAsset, hierarchy string) error {
	p := s.project
	if len(assets) == 0 {
		return errors.New("还没有勾选要装的素材")
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
	if err := step("inspect_avatar", "查看头像和现有菜单", func() (string, error) {
		var err error
		if avs, err = inspect(); err != nil {
			return "", err
		}
		if len(avs) == 0 {
			return "场景里还没有头像", nil
		}
		names := []string{}
		for _, a := range avs {
			names = append(names, a.Name)
		}
		return "头像「" + strings.Join(names, "」「") + "」", nil
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
				return "", errors.New("工程里没有这个文件夹：" + strings.Join(missing, "、"))
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
				return "", errors.New("「" + b.Folder + "」里没有整只头像的 prefab（带 VRC Avatar Descriptor 的）；换一个素体，或者先在 Unity 里打开放着头像的场景")
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
				out += fmt.Sprintf("（共 %d 个整只头像的 prefab，用了它）", len(whole))
			}
			return out, nil
		}); err != nil {
			return err
		}
		if err := step("place_avatar", "把素体「"+strings.TrimSuffix(body.Name, ".prefab")+"」放进新场景", func() (string, error) {
			raw, err := unity.BridgeCall(ctx, p, "place_avatar", map[string]any{"prefab": body.Path}, 3*time.Minute)
			if err != nil {
				return "", err
			}
			var o struct {
				Scene string `json:"scene"`
			}
			_ = json.Unmarshal(raw, &o)
			notes = append(notes, "新建了场景 "+o.Scene+"，素体「"+body.Name+"」已放进去并保存")
			if avs, err = inspect(); err != nil {
				return "", err
			}
			if len(avs) == 0 {
				return "", errors.New("素体放进场景了，但 Unity 没有报告头像；看看 Console 有没有报错")
			}
			return "新场景 " + o.Scene, nil
		}); err != nil {
			return err
		}
	case len(bases) > 0:
		notes = append(notes, "场景里已经有头像「"+avs[0].Name+"」，素体「"+assetLabel(bases[0], baseName(bases[0].Folder))+"」没有重新放置（要换素体的话，先在 Unity 里新建场景）")
	}
	var av avatarInfo
	switch len(avs) {
	case 0:
		return errors.New("打开的场景里没有头像：勾上一个素体让流水线把它放进场景，或者先在 Unity 里打开放着头像的场景")
	case 1:
		av = avs[0]
	default:
		names := []string{}
		for _, a := range avs {
			names = append(names, a.Name)
		}
		return errors.New("场景里有 " + fmt.Sprint(len(avs)) + " 个头像（" + strings.Join(names, "、") + "）：先在 Unity 里只留一个显示的，或者用 AI 模式并说明用哪个")
	}
	aliases := baseAliases(st, av)

	// ---- what the folders hold ----
	var wear []aiAsset
	for _, a := range assets {
		if a.Kind != "素体" {
			wear = append(wear, a)
		}
	}
	if len(wear) == 0 {
		s.add(AIStep{Kind: "say", Text: strings.Join(append(notes, "没有勾选要装的衣服、头发、配饰或道具，流水线到此为止"), "。")})
		return nil
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
			if len(missing) > 0 {
				return "", errors.New("工程里没有这个文件夹：" + strings.Join(missing, "、"))
			}
			return "", errors.New("这些文件夹里没有 prefab")
		}
		out := fmt.Sprintf("%d 个 prefab", len(list))
		if len(missing) > 0 {
			out += "；没找到文件夹 " + strings.Join(missing, "、")
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
	menuRoot := root
	if menuRoot == "" {
		menuRoot = hairRoot
	}

	verbs := map[string]string{"衣服": "穿上", "头发": "戴上", "配饰": "戴上", "道具": "装上"}
	dress := func(kind, label, prefab string, active bool) (object string, meshes []string, err error) {
		err = step("dress", verbs[kind]+"「"+label+"」", func() (string, error) {
			raw, err := unity.BridgeCall(ctx, p, "dress", map[string]any{"avatar": av.Path, "prefab": prefab, "active": active}, 3*time.Minute)
			if err != nil {
				return "", err
			}
			var r struct {
				Existing bool     `json:"existing"`
				Warnings []string `json:"warnings"`
				Outfit   struct {
					Object string `json:"object"`
					Meshes []struct {
						Path string `json:"path"`
					} `json:"meshes"`
				} `json:"outfit"`
			}
			_ = json.Unmarshal(raw, &r)
			object = r.Outfit.Object
			for _, m := range r.Outfit.Meshes {
				meshes = append(meshes, m.Path)
			}
			if !r.Existing {
				s.changed()
			}
			out := fmt.Sprintf("%d 个网格", len(meshes))
			if r.Existing {
				out = "已经在头像上，" + out
			}
			if len(r.Warnings) > 0 {
				warns = append(warns, r.Warnings...)
				out += "。注意：" + strings.Join(r.Warnings, "；")
			}
			return out, nil
		})
		return
	}

	var items []map[string]any
	var done []string
	ownHairDone := false
	for _, kind := range []string{"衣服", "头发", "配饰", "道具"} {
		base, perAsset := menuPath(levels, kind)
		for _, a := range wear {
			if a.Kind != kind {
				continue
			}
			sub := prefabsOf(a)
			if len(sub) == 0 {
				warns = append(warns, "「"+a.Folder+"」里没有 prefab，跳过")
				continue
			}
			outfits, skipped := pickOutfits(sub, aliases)
			if len(outfits) == 0 {
				warns = append(warns, "「"+a.Folder+"」里没有能穿戴的 prefab（都是整只模型），跳过")
				continue
			}
			if len(outfits) > 6 {
				for _, o := range outfits[6:] {
					skipped = append(skipped, o.label)
				}
				outfits = outfits[:6]
			}
			if len(skipped) > 0 {
				notes = append(notes, "「"+assetLabel(a, baseName(a.Folder))+"」里没有穿："+strings.Join(skipped, "、"))
			}
			for i, o := range outfits {
				label := o.label
				if len(outfits) == 1 {
					label = assetLabel(a, o.label)
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
					done = append(done, label)
					// the body's own hair stays choosable: one picker for it, so a new hair can turn it off
					if isHair && !ownHairDone {
						ownHairDone = true
						if own := ownHair(av); own != "" {
							items = append(items, map[string]any{"kind": "outfit", "path": base, "label": "原装头发", "objects": []string{own}, "parameter": pm})
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
						items = append(items, map[string]any{"kind": "outfit", "path": base, "label": label, "objects": []string{object}, "default": first, "parameter": pm})
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
					items = append(items, map[string]any{"kind": "outfit", "path": colourPath, "label": pick, "objects": []string{object}, "default": first, "parameter": pm})
					for _, c := range o.colours {
						items = append(items, map[string]any{"kind": "outfit", "path": colourPath, "label": colourLabel(c, names, aliases), "objects": []string{object}, "materialsFrom": c.Path, "parameter": pm})
					}
					for _, g := range order {
						items = append(items, map[string]any{"kind": "part", "path": where, "label": g, "objects": groups[g]})
					}
				case "配饰":
					object, _, err := dress(kind, label, o.base.Path, true)
					if err != nil {
						return err
					}
					if object == "" {
						continue
					}
					done = append(done, label)
					if len(o.colours) > 0 {
						notes = append(notes, "「"+label+"」还有别的配色（"+strings.Join(names[1:], "、")+"），配饰开关只用了第一个")
					}
					items = append(items, map[string]any{"kind": "toggle", "path": base, "label": label, "objects": []string{object}, "default": true})
				case "道具":
					object, _, err := dress(kind, label, o.base.Path, false)
					if err != nil {
						return err
					}
					if object == "" {
						continue
					}
					done = append(done, label)
					if len(o.colours) > 0 {
						notes = append(notes, "「"+label+"」还有别的配色（"+strings.Join(names[1:], "、")+"），道具开关只用了第一个")
					}
					if perAsset {
						items = append(items, map[string]any{"kind": "toggle", "path": append(append([]string{}, base...), label), "label": "显示", "objects": []string{object}, "default": false})
					} else {
						items = append(items, map[string]any{"kind": "toggle", "path": base, "label": label, "objects": []string{object}, "default": false})
					}
				}
			}
		}
	}
	if len(items) == 0 {
		return errors.New("没有装上任何素材：" + strings.Join(warns, "；"))
	}
	var summary string
	if err := step("build_menu", fmt.Sprintf("生成菜单（%d 项）", len(items)), func() (string, error) {
		args := map[string]any{"avatar": av.Path, "items": items, "parameter": param}
		if menuRoot != "" {
			args["root"] = menuRoot
		}
		raw, err := unity.BridgeCall(ctx, p, "build_menu", args, 5*time.Minute)
		if err != nil {
			return "", err
		}
		var r struct {
			Created  []string `json:"created"`
			Warnings []string `json:"warnings"`
			Icons    int      `json:"icons"`
			Root     string   `json:"root"`
		}
		_ = json.Unmarshal(raw, &r)
		s.changed()
		warns = append(warns, r.Warnings...)
		summary = r.Root
		out := fmt.Sprintf("新建 %d 项，图标 %d 张", len(r.Created), r.Icons)
		if len(r.Created) == 0 {
			out = "菜单已是最新，没有新建的项"
		}
		return out, nil
	}); err != nil {
		return err
	}
	text := "已装上：" + strings.Join(done, "、") + "。菜单在头像下面的「" + summary + "」里，按「" + hierarchy + "」排。"
	if root != "" {
		text += "头像原来就有衣服菜单，新衣服加进了它里面（参数 " + param + "）。"
	}
	if hairRoot != "" {
		text += "头发也加进了原有的头发菜单（参数 " + hairParam + "）。"
	}
	if len(notes) > 0 {
		text += "\n" + strings.Join(core.UniqStrings(notes), "；") + "。"
	}
	if len(warns) > 0 {
		text += "\n注意：" + strings.Join(core.UniqStrings(warns), "；")
	}
	// a look at the result, for the player (an older plugin that cannot photograph is no reason to fail)
	shots := 0
	s.add(AIStep{Kind: "tool", Tool: "look", Text: "给头像拍照", Busy: true})
	if imgs, _, err := takeShots(ctx, p, map[string]any{"avatar": av.Path, "views": []string{"front", "back"}}); err != nil {
		s.finishStep(false, err.Error())
	} else {
		shots = len(imgs)
		s.showImgs(saveShots(imgs))
		s.finishStep(true, "正面和背面：点图片可以放大。这是场景里现在显示着的样子，菜单开关的效果要在 Play 模式里看")
	}
	if shots > 0 {
		text += "\n上面有头像现在的正面和背面截图，先看一眼有没有明显穿模、错位或整块洋红色（材质丢失）。"
	}
	text += "\n部件是按网格名字里的词分的组，分得不对的地方可以让 AI 重新整理，或者在 Unity 里自己调。菜单效果要在 Play 模式里用 Gesture Manager 点一遍；满意就按 Ctrl+S 保存场景，不满意按 Ctrl+Z。"
	s.add(AIStep{Kind: "say", Text: text})
	return nil
}
