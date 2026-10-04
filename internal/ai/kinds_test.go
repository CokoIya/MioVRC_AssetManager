package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

func touch(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuessKind(t *testing.T) {
	for _, c := range []struct{ name, cat, want string }{
		{"情色发光肌肤材质", "材质", "皮肤"}, {"光泽真皮肤材质", "材质", "皮肤"}, {"丝袜材质 stocking", "材质", "其他"},
		{"PCSS4VRC_v9.6.0", "插件", "光影"}, {"亮度调节 Light Controller", "插件", "光影"},
		{"Panda_shop_sps1.1.4", "插件", "互动"}, {"SPS_V2.6.7", "插件", "互动"}, {"DMCustom_PCS_v1.9.2", "插件", "互动"},
		{"辉夜100种表情动画", "其他", "表情"}, {"FaceTracking_1.0.1_for_Chocolat", "面捕", "面捕"}, {"Chocolat_FT 面部追踪", "其他", "面捕"},
		{"Light", "其他", "光影"}, {"Highlight Shader", "插件", "其他"}, {"Lightning Tail", "配饰", "配饰"},
		{"棉花糖系统", "插件", "其他"}, {"lilToon", "插件", "其他"}, {"Skinny Jeans", "衣服", "衣服"}, {"亲亲音效", "音效", "其他"}, {"水手服", "衣服", "衣服"},
	} {
		if got := guessKind(c.name, c.cat); got != c.want {
			t.Errorf("guessKind(%q, %q) = %q, want %q", c.name, c.cat, got, c.want)
		}
	}
}

// the list offers the new kinds: by the library's category and name, by the folder's name, by what Unity
// says a prefab carries; a skin that is materials only is listed although it has no prefab
func TestProjectAssetsNewKinds(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	touch(t, proj, "ProjectSettings/ProjectVersion.txt", "Assets/nHaruka/PCSS4VRC/SelfLight.prefab", "Assets/Miu/SexyGlow/Materials/Body.mat",
		"Assets/Shop/Mystery/thing.prefab", "Assets/Panda Shop/SPS/Prefab/for Kaguya/SPS for Kaguya.prefab", "Assets/Triturbo/Kaguya_FT/Prefabs/Eye Tracking.prefab")
	pn := filepath.Base(proj)
	st.Assets = []*core.Asset{
		{Key: "s1", Name: "情色发光肌肤材质", Category: "材质", Bases: []string{"Kaguya"}, Usage: []core.Usage{{Project: pn, Status: "used", Folder: "Assets/Miu/SexyGlow"}}},
		{Key: "s2", Name: "丝袜材质", Category: "材质", Usage: []core.Usage{{Project: pn, Status: "used", Folder: "Assets/Tex/Stocking"}}},
		{Key: "f1", Name: "FaceTracking for Kaguya", Category: "面捕", Bases: []string{"Kaguya"}, Usage: []core.Usage{{Project: pn, Status: "used", Folder: "Assets/Triturbo/Kaguya_FT"}}},
	}
	touch(t, proj, "Assets/Tex/Stocking/a.png")
	got := map[string]ProjAsset{}
	for _, a := range projectAssets(st, proj) {
		got[a.Folder] = a
	}
	for folder, kind := range map[string]string{"Assets/nHaruka/PCSS4VRC": "光影", "Assets/Miu/SexyGlow": "皮肤", "Assets/Shop/Mystery": "其他",
		"Assets/Panda Shop/SPS": "互动", "Assets/Triturbo/Kaguya_FT": "面捕"} {
		if a, ok := got[folder]; !ok || a.Kind != kind {
			t.Errorf("%s: %+v, want kind %s", folder, a, kind)
		}
	}
	if a := got["Assets/Miu/SexyGlow"]; a.Prefabs != 0 || !reflect.DeepEqual(a.Bases, []string{"Kaguya"}) {
		t.Errorf("skin row %+v", a)
	}
	if _, ok := got["Assets/Tex/Stocking"]; ok {
		t.Error("a plain texture pack is listed")
	}
	// Unity says the mystery prefab carries face tracking
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch cmd {
		case "prefabs":
			return map[string]any{"prefabs": []any{map[string]any{"path": "Assets/Shop/Mystery/thing.prefab", "faceTracking": true, "selfInstalling": true, "renderers": 0}}}, ""
		case "inspect":
			return map[string]any{"avatars": []any{}}, ""
		}
		return nil, "?"
	})
	for _, a := range projectAssetsLive(st, proj, true) {
		if a.Folder == "Assets/Shop/Mystery" && (a.Kind != "面捕" || !a.FT) {
			t.Errorf("live mystery %+v", a)
		}
	}
}

func TestPickPlugin(t *testing.T) {
	al := []string{"Kaguya", "kaguya", "辉夜"}
	sps := []prefabInfo{{Path: "Assets/SPS/for Plum/SPS for Plum.prefab", Name: "SPS for Plum", SelfInstalling: true}, {Path: "Assets/SPS/for Kaguya/SPS for Kaguya.prefab", Name: "SPS for Kaguya", SelfInstalling: true},
		{Path: "Assets/SPS/full.prefab", Name: "full", WholeAvatar: true}}
	if p, _ := pickPlugin(sps, al); p == nil || p.Name != "SPS for Kaguya" {
		t.Errorf("version for the base body: %+v", p)
	}
	if p, names := pickPlugin(sps, nil); p != nil || len(names) != 2 {
		t.Errorf("no base body known: %+v %v", p, names)
	}
	pcs := []prefabInfo{{Path: "Assets/PCS/PCS MA Prefab.prefab", Name: "PCS MA Prefab", SelfInstalling: true}, {Path: "Assets/PCS/PCS VF Prefab.prefab", Name: "PCS VF Prefab", SelfInstalling: true},
		{Path: "Assets/PCS/Heart.prefab", Name: "Heart"}}
	if p, names := pickPlugin(pcs, al); p != nil || !reflect.DeepEqual(names, []string{"PCS MA Prefab", "PCS VF Prefab"}) {
		t.Errorf("two that install themselves: %+v %v", p, names)
	}
	if p, _ := pickPlugin(pcs[2:], al); p == nil || p.Name != "Heart" {
		t.Errorf("a single prefab: %+v", p)
	}
	if p, names := pickPlugin(nil, al); p != nil || names != nil {
		t.Errorf("nothing: %+v %v", p, names)
	}
}

func TestHasPCSS(t *testing.T) {
	if !hasPCSS(avatarInfo{Children: []avatarChild{{Name: "Body"}, {Name: "SelfLight", Components: []string{"PCSS4VRC_NDMF"}}}}) || !hasPCSS(avatarInfo{Children: []avatarChild{{Name: "PCSS_Setup_for_MA"}}}) ||
		hasPCSS(avatarInfo{Children: []avatarChild{{Name: "Body", Components: []string{"SkinnedMeshRenderer"}}}}) {
		t.Error("hasPCSS")
	}
}

func TestHasStrip(t *testing.T) {
	marked := avatarInfo{MaMenu: []menuNode{{Object: "M", Children: []menuNode{{Label: "全脱", Auto: true, Strip: "Clothtoggle"}}}}}
	if !hasStrip(marked, "Clothtoggle") || hasStrip(marked, "Hair_Choose") {
		t.Error("marked strip")
	}
	// made by hand: a switch of its own that turns every outfit of the parameter off
	outfits := []menuNode{{Label: "A", Parameter: "Clothtoggle", Toggles: []string{"A=on", "B=off"}}, {Label: "B", Parameter: "Clothtoggle", Toggles: []string{"B=on", "A=off"}}}
	part := menuNode{Label: "外套", Auto: true, Toggles: []string{"A/Coat=off"}}
	old := avatarInfo{MaMenu: []menuNode{{Object: "M", Children: append(append([]menuNode{}, outfits...), part, menuNode{Label: "R18 全关", Auto: true, Toggles: []string{"A=off", "B=off", "Underwear=off"}})}}}
	if !hasStrip(old, "Clothtoggle") || hasStrip(old, "Hair_Choose") {
		t.Error("a strip made by hand")
	}
	if hasStrip(avatarInfo{MaMenu: append(append([]menuNode{}, outfits...), part, menuNode{Label: "只关 A", Auto: true, Toggles: []string{"A=off"}})}, "Clothtoggle") {
		t.Error("a switch that hides one outfit passes for a strip")
	}
	// one outfit: only its name tells it from a switch that hides that outfit
	one := []menuNode{outfits[0], {Label: "隐藏", Auto: true, Toggles: []string{"A=off"}}}
	if hasStrip(avatarInfo{MaMenu: one}, "Clothtoggle") || !hasStrip(avatarInfo{MaMenu: []menuNode{outfits[0], {Label: "一键脱光", Auto: true, Toggles: []string{"A=off"}}}}, "Clothtoggle") {
		t.Error("a single outfit")
	}
}

// the plugin kinds and skins without an AI: what can be placed is placed as it is and its menu brought into
// the category; what needs the player is said, one line each, and nothing is guessed
func TestQuickPipelinePlugins(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	touch(t, proj, "ProjectSettings/ProjectVersion.txt",
		"Assets/nHaruka/Light/LightControl.prefab", "Assets/nHaruka/Light/Editor/Setup.cs",
		"Assets/Panda Shop/SPS/Prefab/for Kaguya/SPS for Kaguya.prefab", "Assets/Panda Shop/SPS/Prefab/for Plum/SPS for Plum.prefab",
		"Assets/Triturbo/Kaguya_FT/Prefabs/Eye Tracking.prefab", "Assets/Triturbo/Kaguya_FT/Prefabs/Mouth Tracking.prefab", "Assets/Triturbo/Kaguya_FT/Editor/FaceTrackingEditor.cs",
		"Assets/Triturbo/Plum_FT/Prefabs/Eye Tracking.prefab",
		"Assets/Faces/Pack/smile.anim", "Assets/Faces/Pack/cry.anim", "Assets/Faces/Pack/Face.controller",
		"Assets/Miu/SexyGlow/Presets/Body.asset", "Assets/Miu/SexyGlow/Tex/a.png", "Assets/Miu/SexyGlow/Tex/b.png", "Assets/Miu/SexyGlow/Materials/Body_SexyGlow.mat",
		"Assets/Skins/Tan/Kaguya_Tan.prefab", "Assets/PCS/PCS MA Prefab.prefab", "Assets/PCS/PCS VF Prefab.prefab", "Assets/Broken/gimmick.prefab")
	pn := filepath.Base(proj)
	st.Assets = []*core.Asset{
		{Key: "ft-plum", Name: "FaceTracking for Plum", Category: "面捕", Bases: []string{"Plum"}, Usage: []core.Usage{{Project: pn, Status: "used", Folder: "Assets/Triturbo/Plum_FT"}}},
		{Key: "ft-kaguya", Name: "FaceTracking for Kaguya", Category: "面捕", Bases: []string{"Kaguya"}, Usage: []core.Usage{{Project: pn, Status: "used", Folder: "Assets/Triturbo/Kaguya_FT"}}},
	}
	u := &sceneUnity{live: true, bits: 120, extra: []prefabInfo{
		{Path: "Assets/nHaruka/Light/LightControl.prefab", Name: "LightControl", SelfInstalling: true, MenuInstallers: 1},
		{Path: "Assets/Panda Shop/SPS/Prefab/for Kaguya/SPS for Kaguya.prefab", Name: "SPS for Kaguya", SelfInstalling: true, VRCFury: true, Renderers: 1},
		{Path: "Assets/Panda Shop/SPS/Prefab/for Plum/SPS for Plum.prefab", Name: "SPS for Plum", SelfInstalling: true, VRCFury: true, Renderers: 1},
		{Path: "Assets/Triturbo/Kaguya_FT/Prefabs/Eye Tracking.prefab", Name: "Eye Tracking", SelfInstalling: true, FaceTracking: true},
		{Path: "Assets/Triturbo/Kaguya_FT/Prefabs/Mouth Tracking.prefab", Name: "Mouth Tracking", SelfInstalling: true, FaceTracking: true},
		{Path: "Assets/Triturbo/Plum_FT/Prefabs/Eye Tracking.prefab", Name: "Eye Tracking", SelfInstalling: true, FaceTracking: true},
		{Path: "Assets/Skins/Tan/Kaguya_Tan.prefab", Name: "Kaguya_Tan", WholeAvatar: true, Renderers: 3, MeshNames: []string{"Body", "Hair", "Body_b"}},
		{Path: "Assets/PCS/PCS MA Prefab.prefab", Name: "PCS MA Prefab", SelfInstalling: true, MenuInstallers: 1},
		{Path: "Assets/PCS/PCS VF Prefab.prefab", Name: "PCS VF Prefab", SelfInstalling: true, VRCFury: true},
		{Path: "Assets/Broken/gimmick.prefab", Name: "gimmick", MissingScripts: 3},
	}}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	req := aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{
		{Folder: "Assets/Triturbo/Plum_FT", Kind: "面捕", Name: "Plum 面捕"},
		{Folder: "Assets/Triturbo/Kaguya_FT", Kind: "面捕", Name: "辉夜面捕"},
		{Folder: "Assets/Panda Shop/SPS", Kind: "互动", Name: "SPS"},
		{Folder: "Assets/PCS", Kind: "互动", Name: "PCS"},
		{Folder: "Assets/Broken", Kind: "互动", Name: "坏掉的插件"},
		{Folder: "Assets/nHaruka/Light", Kind: "光影", Name: "灯光控制"},
		{Folder: "Assets/Skins/Tan", Kind: "皮肤", Name: "小麦色"},
		{Folder: "Assets/Miu/SexyGlow", Kind: "皮肤", Name: "发光肌肤"},
		{Folder: "Assets/Faces/Pack", Kind: "表情", Name: "表情包"},
	}}
	if err := startAIRun(st, req); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	last := steps[len(steps)-1]
	if last.Kind != "say" {
		t.Fatalf("steps %+v", steps)
	}
	if !strings.Contains(steps[0].Text, "- 面捕：Assets/Triturbo/Plum_FT（Plum 面捕）；适配素体：Plum；内有 1 个 prefab") ||
		!strings.Contains(steps[0].Text, "- 面捕：Assets/Triturbo/Kaguya_FT（辉夜面捕）；适配素体：Kaguya；内有 2 个 prefab、1 个编辑器脚本") {
		t.Errorf("the task does not say what the library knows: %s", steps[0].Text)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	var placed []string
	for _, p := range u.places {
		placed = append(placed, filepath.Base(fmt.Sprint(p["prefab"])))
	}
	if want := []string{"LightControl.prefab", "SPS for Kaguya.prefab"}; !reflect.DeepEqual(placed, want) {
		t.Errorf("placed %v, want %v", placed, want)
	}
	if len(u.dressed) != 0 {
		t.Errorf("a plugin was dressed: %v", u.dressed)
	}
	if len(u.menus) != 1 {
		t.Fatalf("%d menus", len(u.menus))
	}
	var rows []string
	for _, it := range u.menus[0]["items"].([]any) {
		o := it.(map[string]any)
		row := fmt.Sprint(o["kind"], " ", strings.Join(asStrings(o["path"]), "/"), ":", o["label"], " ", asStrings(o["objects"]))
		if p, _ := o["parameter"].(string); p != "" {
			row += " @" + p
		}
		if d, _ := o["default"].(bool); d {
			row += " *"
		}
		if m, _ := o["materialsFrom"].(string); m != "" {
			row += " <" + filepath.Base(m)
		}
		rows = append(rows, row)
	}
	want := []string{"skin 皮肤:原版皮肤 [] @Skin_Choose *", "skin 皮肤:小麦色 [Body] @Skin_Choose <Kaguya_Tan.prefab", "install 光影:灯光控制 [LightControl]"}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("menu rows:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
	for _, w := range []string{
		"已放置插件：灯光控制、SPS", "已添加皮肤切换项：小麦色", "需手动完成：",
		"「Plum 面捕」未安装：它为「Plum」制作，当前模型的素体为「Kaguya」", "不能跨素体使用",
		"「辉夜面捕」需使用其自带的安装工具安装", "TriturboFT > 素体名 FT", "Apply Face Tracking Addon",
		"「SPS」的菜单由 VRCFury 在构建时生成，流水线未移动", "Move Menu Item",
		"「PCS」未放置：其中有多个可放置的 prefab（PCS MA Prefab、PCS VF Prefab）", "类别选择「互动」",
		"「坏掉的插件」未放置：prefab「gimmick」中有 3 个缺失的脚本",
		"「发光肌肤」未生成皮肤菜单：其中没有带身体网格的 prefab（内有 1 个材质、2 张贴图、1 个资产文件（预设等））",
		"「表情包」未安装：本次同时选择了面捕",
	} {
		if !strings.Contains(last.Text, w) {
			t.Errorf("summary lacks %q:\n%s", w, last.Text)
		}
	}
	if strings.Contains(last.Text, "一键脱光") {
		t.Errorf("a strip without an outfit menu: %s", last.Text)
	}
	// two changes to take back: each placed plugin is a step, the menu another
	if _, _, changes := aiSessionFor(proj).snapshot(); changes != 3 {
		t.Errorf("%d changes counted", changes)
	}
}

// nothing the line can do by itself is not an error: it says what the folder holds and what is left to do
func TestQuickPipelineNothingToPlace(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	touch(t, proj, "Assets/Miu/SexyGlow/Tex/a.png", "Assets/Faces/Pack/smile.anim")
	u := &sceneUnity{live: true, ft: true}
	u.extra = nil
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		if cmd == "prefabs" {
			return map[string]any{"prefabs": []any{}, "total": 0}, ""
		}
		return u.answer(cmd, args)
	})
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{{Folder: "Assets/Miu/SexyGlow", Kind: "皮肤"}, {Folder: "Assets/Faces/Pack", Kind: "表情", Name: "表情包"}}}); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	last := steps[len(steps)-1]
	if last.Kind != "say" || !strings.Contains(last.Text, "流水线未改动模型") || !strings.Contains(last.Text, "内有 1 张贴图") || !strings.Contains(last.Text, "「表情包」未安装：模型上已有面捕") {
		t.Fatalf("last step %+v", last)
	}
	// without face tracking the expression pack is looked at: animations only, nothing to place
	u.mu.Lock()
	u.ft = false
	u.mu.Unlock()
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{{Folder: "Assets/Faces/Pack", Kind: "表情", Name: "表情包"}}}); err != nil {
		t.Fatal(err)
	}
	steps = waitRun(t, aiSessionFor(proj))
	if last := steps[len(steps)-1]; last.Kind != "say" || !strings.Contains(last.Text, "「表情包」未放置：其中没有可放置的 prefab（内有 1 个动画）") || !strings.Contains(last.Text, "FX 动画控制器") {
		t.Errorf("expression pack: %+v", last)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.menus)+len(u.places)+len(u.dressed) != 0 {
		t.Errorf("the scene was touched: %v", u.calls)
	}
	// an outfit folder with nothing in it is still the error it was
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{{Folder: "Assets/Empty", Kind: "衣服"}}}); err != nil {
		t.Fatal(err)
	}
	u.mu.Unlock()
	steps = waitRun(t, aiSessionFor(proj))
	u.mu.Lock()
	if last := steps[len(steps)-1]; last.Kind != "error" || !strings.Contains(last.Text, "没有 prefab") {
		t.Errorf("empty outfit folder: %+v", last)
	}
}

// the plugin of an older version does not know "place": said as what it is, the run goes on
func TestQuickPipelineOldPlugin(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{live: true, extra: []prefabInfo{{Path: "Assets/Light/Light.prefab", Name: "Light"}}}
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		if cmd == "place" {
			return nil, "不认识的操作：place"
		}
		return u.answer(cmd, args)
	})
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{{Folder: "Assets/Light", Kind: "光影", Name: "灯光"}}}); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	if last := steps[len(steps)-1]; last.Kind != "say" || !strings.Contains(last.Text, "「灯光」未放置：工程中的 Unity 插件版本较旧") || !strings.Contains(last.Text, "点击「更新」") {
		t.Errorf("last step %+v", last)
	}
}

// the AI's tools: place_prefab goes to the plugin's "place", and the new item kinds are in the schema
func TestPlacePrefabTool(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{live: true, extra: []prefabInfo{{Path: "Assets/Light/Light.prefab", Name: "Light", SelfInstalling: true, MenuInstallers: 1}}}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	s := aiSessionFor(proj)
	content, short, ok := s.runTool(t.Context(), "place_prefab", map[string]any{"prefab": "Assets/Light/Light.prefab"})
	if !ok || !strings.Contains(short, "「Light」已放置") || !strings.Contains(content, `"menuInstallers":["Light"]`) {
		t.Fatalf("place_prefab: %v %q %s", ok, short, content)
	}
	if _, short, _ = s.runTool(t.Context(), "place_prefab", map[string]any{"prefab": "Assets/Light/Light.prefab"}); !strings.Contains(short, "已在模型上") {
		t.Errorf("second time: %q", short)
	}
	if _, _, n := s.snapshot(); n != 1 {
		t.Errorf("%d changes", n)
	}
	var schema string
	for _, tool := range aiTools {
		if tool.Name == "build_menu" {
			schema = fmt.Sprint(tool.Params)
		}
	}
	for _, w := range []string{"skin", "install", "materials"} {
		if !strings.Contains(schema, w) {
			t.Errorf("build_menu's schema lacks %q", w)
		}
	}
	prompt := aiSystemPrompt(proj, "")
	for _, w := range []string{"place_prefab", "Skin_Choose", "TriturboFT", "kind=\"install\"", "一键脱光"} {
		if !strings.Contains(prompt, w) {
			t.Errorf("the prompt lacks %q", w)
		}
	}
	if title := toolTitle("place_prefab", map[string]any{"prefab": "Assets/Light/Light.prefab"}); title != "放置「Light」" {
		t.Errorf("title %q", title)
	}
}
