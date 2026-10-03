package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

func TestNormBase(t *testing.T) {
	for _, c := range [][3]string{
		{"openai", "https://api.deepseek.com", "https://api.deepseek.com/v1"},
		{"openai", "https://api.deepseek.com/", "https://api.deepseek.com/v1"},
		{"openai", "https://api.openai.com/v1/", "https://api.openai.com/v1"},
		{"openai", "https://relay.example.com/v1/chat/completions", "https://relay.example.com/v1"},
		{"openai", "http://localhost:11434/v1", "http://localhost:11434/v1"},
		{"openai", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4"},
		{"claude", "https://api.anthropic.com", "https://api.anthropic.com"},
		{"claude", "https://api.anthropic.com/v1/messages", "https://api.anthropic.com"},
		{"claude", "https://relay.example.com/claude/v1", "https://relay.example.com/claude"},
		{"openai", "not a url", ""},
	} {
		if got := normBase(c[0], c[1]); got != c[2] {
			t.Errorf("normBase(%s, %s) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestAIConfigAndKeys(t *testing.T) {
	core.DataDir = t.TempDir()
	v := aiView()
	if v.Ready || v.Provider != "" || v.Hierarchy != defaultHierarchy || len(v.Providers) != 6 {
		t.Fatalf("fresh view %+v", v)
	}
	if v.Profiles["deepseek"].BaseURL != "https://api.deepseek.com" || v.Profiles["deepseek"].Model != "deepseek-flash" {
		t.Errorf("deepseek defaults %+v", v.Profiles["deepseek"])
	}
	key := "sk-test-1234567890abcd"
	if err := aiSave("deepseek", "", "", &key, nil); err != nil {
		t.Fatal(err)
	}
	v = aiView()
	if !v.Ready || v.Keys["deepseek"] != "…abcd" || v.Provider != "deepseek" {
		t.Errorf("after save %+v", v)
	}
	// the key file does not hold the key in a form the window is given, and the config file not at all
	if b, _ := os.ReadFile(aiConfigFile()); strings.Contains(string(b), "sk-test") {
		t.Error("key in ai.json")
	}
	if b, _ := json.Marshal(v); strings.Contains(string(b), "sk-test") {
		t.Error("key in the view")
	}
	// another provider keeps its own address, model and key; the first one's stay
	k2 := "relay-key-000011112222"
	if err := aiSave("openai", "https://relay.example.com/v1", "gpt-x", &k2, nil); err != nil {
		t.Fatal(err)
	}
	if err := aiSave("openai", "https://relay.example.com/v1", "gpt-y", nil, nil); err != nil { // key untouched
		t.Fatal(err)
	}
	v = aiView()
	if v.Keys["deepseek"] != "…abcd" || v.Keys["openai"] != "…2222" || v.Profiles["openai"].Model != "gpt-y" || v.Provider != "openai" {
		t.Errorf("two providers %+v", v)
	}
	empty := ""
	_ = aiSave("openai", "https://relay.example.com/v1", "gpt-y", &empty, nil)
	if v = aiView(); v.Keys["openai"] != "" || v.Ready {
		t.Errorf("key removed %+v", v)
	}
	if err := aiSave("openai", "relay.example.com", "m", nil, nil); err == nil {
		t.Error("address without scheme accepted")
	}
	h := "主菜单 > 换装 > {衣服} > {开关}"
	_ = aiSave("", "", "", nil, &h)
	if v = aiView(); v.Hierarchy != h || v.Provider != "openai" {
		t.Errorf("hierarchy %+v", v)
	}
	aiRemember(`C:\Proj\A`, "booth:1", []string{"Assets/Shop"})
	if got := aiView().Recent[core.PathKey(`C:\Proj\A`)]; len(got) != 1 || got[0] != "Assets/Shop" {
		t.Errorf("recent %v", got)
	}
	if got := loadAIConfig().Imported[core.PathKey(`C:\Proj\A`)]["Assets/Shop"]; got != "booth:1" {
		t.Errorf("imported %q", got)
	}
}

func TestAIErrText(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		want   []string
	}{
		{401, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error"}}`, []string{"API Key 不正确", "Incorrect API key"}},
		{402, `{"error":{"message":"Insufficient Balance"}}`, []string{"余额"}},
		{404, `{"error":{"type":"not_found_error","message":"model: nope"}}`, []string{"模型名称不正确", "model: nope"}},
		{429, `{"error":"rate limited"}`, []string{"过于频繁", "rate limited"}},
		{502, `<html>bad gateway</html>`, []string{"稍后重试"}},
	} {
		got := aiErrText("X", c.status, []byte(c.body))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%d: %q lacks %q", c.status, got, w)
			}
		}
		if strings.Contains(got, "<html>") {
			t.Errorf("html in %q", got)
		}
	}
}

func TestHierarchyLevels(t *testing.T) {
	for _, c := range []struct {
		in   string
		base []string
		per  bool
	}{
		{"主菜单 > 衣服 > {衣服} > {开关}", []string{"衣服"}, true},
		{"主菜单->换装->服装->{衣服}->{开关}", []string{"换装", "服装"}, true},
		{"主菜单 > {衣服} > {开关}", nil, true},
		{"主菜单 > 衣服 > {开关}", []string{"衣服"}, false},
		{"(主菜单->生成衣服层级->衣服1层级->衣服相关开关或层级)", []string{"衣服"}, true},
		{"衣柜 / {outfit} / {开关}", []string{"衣柜"}, true},
		{"", nil, false},
	} {
		base, per := hierarchyLevels(c.in)
		if !reflect.DeepEqual(base, c.base) || per != c.per {
			t.Errorf("%q → %v %v, want %v %v", c.in, base, per, c.base, c.per)
		}
	}
}

func TestPartOf(t *testing.T) {
	for name, want := range map[string]string{
		"UV1_Devil tail": "尾巴", "UV1_Devil tail - ribbon": "尾巴", "UV1_Devil wing": "翅膀", "UV2_Sandal jewel": "鞋子",
		"UV2_Loose socks": "袜子", "UV1_Devil horn - ribbon": "头饰", "UV1_Hair tie": "头饰", "UV1_Pin - L1": "头饰",
		"UV2_Bag - Beads charm": "包", "UV4_Cardigan": "外套", "UV2_Skirt": "裙子", "UV3_Slacks": "裤子", "UV1_Sailor": "上衣",
		"UV1_Choker": "饰品", "UV3_Belt charm": "饰品", "UV1_Face Heart": "", "Body": "", "Swimwear": "", "DevilTail_L": "尾巴",
		"Jacket_Arm": "外套", "outer.arm": "外套", "靴下": "袜子", "ブーツ": "鞋子", "Earrings.L.001": "饰品", "Topping": "", "Shoes": "鞋子",
	} {
		if got := partOf(name); got != want {
			t.Errorf("partOf(%q) = %q, want %q", name, got, want)
		}
	}
}

func testPrefabs() []prefabInfo {
	meshes := []string{"UV1_Choker", "UV1_Devil horn", "UV1_Devil tail", "UV1_Devil wing", "UV1_Sailor", "UV2_Loose socks", "UV2_Sandal", "UV2_Skirt", "UV4_Cardigan"}
	var out []prefabInfo
	for _, base := range []string{"Kaguya", "Manuka"} {
		for _, col := range []string{"Envy cat", "Heart Catcher ", "Melty Devil "} {
			out = append(out, prefabInfo{Path: "Assets/Shop/SailorSet/" + base + "/" + base + " prefab/" + col + ".prefab", Name: col, MAReady: true, Renderers: 9, MeshNames: meshes, HasArmature: true})
		}
	}
	out = append(out, prefabInfo{Path: "Assets/Shop/SailorSet/Kaguya/Kaguya_full.prefab", Name: "Kaguya_full", WholeAvatar: true, Renderers: 20},
		prefabInfo{Path: "Assets/Shop/SailorSet/Kaguya/acc.prefab", Name: "acc", Renderers: 2, MeshNames: []string{"Ring"}})
	return out
}

func TestPickOutfits(t *testing.T) {
	out, skipped := pickOutfits(testPrefabs(), []string{"Kaguya", "kaguya", "辉夜"})
	if len(out) != 1 || len(out[0].colours) != 2 || !strings.Contains(out[0].base.Path, "/Kaguya/") {
		t.Fatalf("outfits %+v", out)
	}
	if out[0].label != "SailorSet" {
		t.Errorf("label %q", out[0].label)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "完整模型") {
		t.Errorf("skipped %v", skipped)
	}
	// no base body known: both bodies' versions are outfits of their own
	if out, _ = pickOutfits(testPrefabs(), nil); len(out) != 2 {
		t.Errorf("without aliases: %d outfits", len(out))
	}
}

// sceneUnity: a small avatar scene that remembers what was asked of it.
type sceneUnity struct {
	mu      sync.Mutex
	calls   []string
	dressed []map[string]any
	menus   []map[string]any
	placed  []map[string]any
	refresh int
	empty   bool         // the scene has no avatar until one is placed
	extra   []prefabInfo // prefabs beyond testPrefabs()
}

func (u *sceneUnity) answer(cmd string, args map[string]any) (any, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, cmd)
	switch cmd {
	case "refresh":
		u.refresh++
		return map[string]any{"refreshed": true}, ""
	case "inspect":
		if u.empty && len(u.placed) == 0 {
			return map[string]any{"modularAvatar": "1.18.3", "vrcSdk": true, "avatars": []any{}}, ""
		}
		return map[string]any{"modularAvatar": "1.18.3", "vrcSdk": true, "avatars": []any{map[string]any{
			"name": "Kaguya_Test", "path": "Kaguya_Test", "active": true, "prefab": "Assets/IKUSIA/kaguya/kaguya.prefab",
			"children": []any{map[string]any{"name": "Body", "kind": "mesh", "active": true}, map[string]any{"name": "Hair", "kind": "mesh", "active": true, "prefab": "Assets/IKUSIA/kaguya/kaguya.prefab"}}, "maMenu": []any{}}}}, ""
	case "prefabs":
		all := append(testPrefabs(), u.extra...)
		return map[string]any{"prefabs": all, "total": len(all)}, ""
	case "place_avatar":
		u.placed = append(u.placed, args)
		return map[string]any{"scene": "Assets/P/P.unity", "avatar": "Kaguya_Test", "prefab": args["prefab"]}, ""
	case "dress":
		u.dressed = append(u.dressed, args)
		name := strings.TrimSpace(strings.TrimSuffix(filepath.Base(fmt.Sprint(args["prefab"])), ".prefab"))
		var meshes []any
		names := testPrefabs()[0].MeshNames
		for _, x := range u.extra {
			if strings.EqualFold(x.Path, fmt.Sprint(args["prefab"])) {
				names = x.MeshNames
			}
		}
		for _, m := range names {
			meshes = append(meshes, map[string]any{"path": name + "/" + m, "active": true, "height": []float64{0, 1}})
		}
		return map[string]any{"setUp": true, "warnings": []string{}, "outfit": map[string]any{"object": name, "meshes": meshes}}, ""
	case "inspect_object":
		return map[string]any{"object": args["path"], "meshes": []any{map[string]any{"path": fmt.Sprint(args["path"]) + "/UV2_Sandal", "height": []float64{0, 0.1}}}}, ""
	case "build_menu":
		items, _ := args["items"].([]any)
		for _, it := range items {
			o := it.(map[string]any)
			for _, p := range asStrings(o["objects"]) {
				if strings.Contains(p, "NOPE") {
					return nil, "头像下找不到「" + p + "」"
				}
			}
		}
		u.menus = append(u.menus, args)
		return map[string]any{"created": []string{"Avatar Menu"}, "warnings": []string{}, "icons": len(items), "root": "Avatar Menu", "parameter": "Clothtoggle",
			"menu": map[string]any{"big": strings.Repeat("x", 5000)}}, ""
	case "undo":
		return map[string]any{"undone": "MioVRCA 生成菜单"}, ""
	}
	return nil, "不认识的操作：" + cmd
}

func asStrings(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, e := range l {
			out = append(out, fmt.Sprint(e))
		}
	}
	return out
}

func waitRun(t *testing.T, s *aiSession) []AIStep {
	t.Helper()
	for i := 0; i < 600; i++ {
		busy, steps, _ := s.snapshot()
		if !busy {
			return steps
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the run did not end")
	return nil
}

func TestQuickDress(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Folders: []string{"Assets/Shop/SailorSet"}, Hierarchy: "主菜单 > 换装 > {衣服} > {开关}"}); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	last := steps[len(steps)-1]
	if last.Kind != "say" || !strings.Contains(last.Text, "已装配") {
		t.Fatalf("steps %+v", steps)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.refresh == 0 || len(u.dressed) != 1 || len(u.menus) != 1 {
		t.Fatalf("calls %v", u.calls)
	}
	if p := fmt.Sprint(u.dressed[0]["prefab"]); !strings.Contains(p, "/Kaguya/") || u.dressed[0]["active"] != true {
		t.Errorf("dressed %v", u.dressed[0])
	}
	items := u.menus[0]["items"].([]any)
	var kinds, labels []string
	for _, it := range items {
		o := it.(map[string]any)
		kinds = append(kinds, fmt.Sprint(o["kind"]))
		labels = append(labels, strings.Join(asStrings(o["path"]), "/")+":"+fmt.Sprint(o["label"]))
	}
	want := []string{"换装/SailorSet/配色:Envy cat", "换装/SailorSet/配色:Heart Catcher", "换装/SailorSet/配色:Melty Devil",
		"换装/SailorSet:外套", "换装/SailorSet:上衣", "换装/SailorSet:裙子", "换装/SailorSet:袜子", "换装/SailorSet:鞋子", "换装/SailorSet:尾巴", "换装/SailorSet:饰品"}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("menu items %v\n%v", labels, kinds)
	}
	// what was merged away is in 饰品: the choker, the horn and the wings
	if acc := asStrings(items[9].(map[string]any)["objects"]); len(acc) != 3 {
		t.Errorf("饰品 holds %v", acc)
	}
	first := items[0].(map[string]any)
	second := items[1].(map[string]any)
	if first["default"] != true || first["materialsFrom"] != nil || !strings.HasSuffix(fmt.Sprint(second["materialsFrom"]), "Heart Catcher .prefab") {
		t.Errorf("colours %v %v", first, second)
	}
	if u.menus[0]["root"] != nil {
		t.Errorf("root %v", u.menus[0]["root"])
	}
	// the hierarchy the player used is kept for next time
	if h := loadAIConfig().Hierarchy; !strings.Contains(h, "换装") {
		t.Errorf("hierarchy not kept: %q", h)
	}
}

// every kind at once, on a scene that has no avatar yet: the base body is placed first, outfits and hair are
// exclusive on parameters of their own, accessories and props are switches, and hair keeps the body's own hair choosable.
func TestQuickPipelineKinds(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{empty: true, extra: []prefabInfo{
		{Path: "Assets/IKUSIA/kaguya/kaguya.prefab", Name: "kaguya", WholeAvatar: true, Renderers: 12},
		{Path: "Assets/IKUSIA/kaguya/kaguya_Quest.prefab", Name: "kaguya_Quest", WholeAvatar: true, Renderers: 6},
		{Path: "Assets/_头发/樱发/Khaki.prefab", Name: "Khaki", Renderers: 3, MeshNames: []string{"Hair", "hairpin1", "hairpin2"}, MAReady: true},
		{Path: "Assets/_头发/耳发/Blue.prefab", Name: "Blue", Renderers: 1, MeshNames: []string{"Hair_ear"}, MAReady: true},
		{Path: "Assets/_道具/Bat/bat.prefab", Name: "bat", Renderers: 1, MeshNames: []string{"Bat"}},
		{Path: "Assets/_配饰/Glasses/glasses.prefab", Name: "glasses", Renderers: 1, MeshNames: []string{"Glasses"}},
	}}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	req := aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{
		{Folder: "Assets/_道具/Bat", Kind: "道具", Name: "球棒"},
		{Folder: "Assets/IKUSIA/kaguya", Kind: "素体", Name: "辉夜"},
		{Folder: "Assets/Shop/SailorSet", Kind: "衣服", Name: "水手服"},
		{Folder: "Assets/_头发/樱发", Kind: "头发", Name: "樱发"},
		{Folder: "Assets/_头发/耳发", Kind: "头发", Name: "蓝耳发"},
		{Folder: "Assets/_配饰/Glasses", Kind: "配饰", Name: "眼镜"},
	}}
	if err := startAIRun(st, req); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	last := steps[len(steps)-1]
	if last.Kind != "say" || !strings.Contains(last.Text, "已装配") {
		t.Fatalf("steps %+v", steps)
	}
	if !strings.Contains(steps[0].Text, "- 素体：Assets/IKUSIA/kaguya（辉夜）") || !strings.Contains(steps[0].Text, "- 道具：Assets/_道具/Bat（球棒）") {
		t.Errorf("task text %q", steps[0].Text)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.placed) != 1 || !strings.HasSuffix(fmt.Sprint(u.placed[0]["prefab"]), "/kaguya.prefab") {
		t.Fatalf("placed %v (calls %v)", u.placed, u.calls)
	}
	if u.calls[1] != "inspect" || u.calls[3] != "place_avatar" || u.calls[4] != "inspect" {
		t.Errorf("order %v", u.calls)
	}
	var dressed []string
	for _, d := range u.dressed {
		dressed = append(dressed, filepath.Base(fmt.Sprint(d["prefab"]))+":"+fmt.Sprint(d["active"]))
	}
	if want := []string{"Envy cat.prefab:true", "Khaki.prefab:true", "Blue.prefab:false", "glasses.prefab:true", "bat.prefab:false"}; !reflect.DeepEqual(dressed, want) {
		t.Errorf("dressed %v", dressed)
	}
	if len(u.menus) != 1 {
		t.Fatalf("menus %d", len(u.menus))
	}
	var rows []string
	for _, it := range u.menus[0]["items"].([]any) {
		o := it.(map[string]any)
		row := fmt.Sprint(o["kind"]) + " " + strings.Join(asStrings(o["path"]), "/") + ":" + fmt.Sprint(o["label"])
		if p, _ := o["parameter"].(string); p != "" {
			row += " @" + p
		}
		if d, _ := o["default"].(bool); d {
			row += " *"
		}
		rows = append(rows, row)
	}
	want := []string{
		"outfit 衣服/水手服/配色:Envy cat @Clothtoggle *", "outfit 衣服/水手服/配色:Heart Catcher @Clothtoggle", "outfit 衣服/水手服/配色:Melty Devil @Clothtoggle",
		"part 衣服/水手服:外套", "part 衣服/水手服:上衣", "part 衣服/水手服:裙子", "part 衣服/水手服:袜子", "part 衣服/水手服:鞋子", "part 衣服/水手服:尾巴", "part 衣服/水手服:饰品",
		"outfit 头发:原装头发 @Hair_Choose", "outfit 头发/樱发:戴上 @Hair_Choose *", "part 头发/樱发:头饰", "outfit 头发:蓝耳发 @Hair_Choose",
		"toggle 配饰:眼镜 *", "toggle 道具/球棒:显示",
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("menu rows:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
	if u.menus[0]["parameter"] != "Clothtoggle" {
		t.Errorf("parameter %v", u.menus[0]["parameter"])
	}
	if !strings.Contains(last.Text, "Assets/P/P.unity") {
		t.Errorf("summary lacks the new scene: %s", last.Text)
	}
}

// the same assets when the scene has an avatar already: the base body is left alone and the summary says so
func TestQuickPipelineBaseSkipped(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{extra: []prefabInfo{{Path: "Assets/IKUSIA/kaguya/kaguya.prefab", Name: "kaguya", WholeAvatar: true, Renderers: 12}}}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	req := aiRunReq{Project: proj, Mode: "dress", NoAI: true, Hierarchy: "主菜单 > 衣服 > {衣服} > {开关}", Assets: []aiAsset{
		{Folder: "Assets/IKUSIA/kaguya", Kind: "素体", Name: "辉夜"}, {Folder: "Assets/Shop/SailorSet", Kind: "衣服"}}}
	if err := startAIRun(st, req); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	last := steps[len(steps)-1]
	if last.Kind != "say" || !strings.Contains(last.Text, "未重新放置") {
		t.Fatalf("steps %+v", steps)
	}
	u.mu.Lock()
	if len(u.placed) != 0 || len(u.dressed) != 1 {
		t.Errorf("placed %v dressed %v", u.placed, u.dressed)
	}
	u.mu.Unlock()
	// a base body alone, nothing to wear: the line stops after the avatar
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{{Folder: "Assets/IKUSIA/kaguya", Kind: "素体"}}}); err != nil {
		t.Fatal(err)
	}
	steps = waitRun(t, aiSessionFor(proj))
	if last := steps[len(steps)-1]; last.Kind != "say" || !strings.Contains(last.Text, "流水线已结束") {
		t.Errorf("base only: %+v", last)
	}
}

func TestMenuPath(t *testing.T) {
	for _, c := range []struct {
		tpl, kind string
		base      []string
		per       bool
	}{
		{"主菜单 > {分类} > {素材} > {开关}", "衣服", []string{"衣服"}, true},
		{"主菜单 > {分类} > {素材} > {开关}", "头发", []string{"头发"}, true},
		{"主菜单 > {分类} > {素材} > {开关}", "道具", []string{"道具"}, true},
		{"主菜单 > 衣服 > {衣服} > {开关}", "头发", []string{"头发"}, true},
		{"主菜单 > 衣服 > {衣服} > {开关}", "衣服", []string{"衣服"}, true},
		{"主菜单->换装->服装->{衣服}->{开关}", "头发", []string{"换装", "头发"}, true},
		{"主菜单 > 衣柜 > {开关}", "配饰", []string{"衣柜"}, false},
		{"主菜单 > 外观 > {分类} > {开关}", "道具", []string{"外观", "道具"}, false},
		{"主菜单 > {类别} > {每个素材} > {开关}", "配饰", []string{"配饰"}, true},
	} {
		base, per := menuPath(parseHierarchy(c.tpl), c.kind)
		if !reflect.DeepEqual(base, c.base) || per != c.per {
			t.Errorf("%q for %s → %v %v, want %v %v", c.tpl, c.kind, base, per, c.base, c.per)
		}
	}
	for name, want := range map[string]string{"Hair": "", "Hair_front": "", "hairpin1": "头饰", "Hair ribbon": "头饰", "Khaki/flower": "头饰", "Hair_Ornament": "头饰", "Ear_L": "头饰", "Tail": "尾巴"} {
		if got := hairPartOf(name); got != want {
			t.Errorf("hairPartOf(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOutfitMenuReuse(t *testing.T) {
	av := avatarInfo{Name: "A", MaMenu: []menuNode{{Object: "Menu_Outfits", Type: "MenuInstaller", Children: []menuNode{
		{Object: "Menu_Outfits/原衣服", Type: "Toggle", Parameter: "cloth_choose", Default: true},
		{Object: "Menu_Outfits/a", Type: "Toggle", Parameter: "cloth_choose", Toggles: []string{"A=on", "B=off"}},
		{Object: "Menu_Outfits/sub", Type: "SubMenu", Children: []menuNode{{Object: "Menu_Outfits/sub/b", Type: "Toggle", Parameter: "cloth_choose", Toggles: []string{"B=on"}}}},
		{Object: "Menu_Outfits/hide", Type: "Toggle", Auto: true, Toggles: []string{"A/x=off"}},
	}}}}
	root, param := outfitMenu(av)
	if root != "Menu_Outfits" || param != "cloth_choose" || !wearsByDefault(av, "cloth_choose") || wearsByDefault(av, "Clothtoggle") {
		t.Errorf("outfitMenu = %q %q", root, param)
	}
	if r, p := outfitMenu(avatarInfo{}); r != "" || p != "" {
		t.Error("empty avatar has a menu")
	}
}

// mockLLM answers like an OpenAI-compatible or an Anthropic-compatible service, following a script of turns.
type mockLLM struct {
	mu     sync.Mutex
	wire   string
	script []mockTurn
	seen   []map[string]any
	auth   []string
}

type mockTurn struct {
	text  string
	calls []aiCall
}

func (m *mockLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" {
		_, _ = io.WriteString(w, `{"data":[{"id":"model-b"},{"id":"model-a"}]}`)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	m.seen = append(m.seen, body)
	m.auth = append(m.auth, r.Header.Get("Authorization")+"|"+r.Header.Get("x-api-key"))
	if body["model"] == "bad-model" {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"error":{"message":"The model bad-model does not exist"}}`)
		return
	}
	i := len(m.seen) - 1
	if i >= len(m.script) {
		i = len(m.script) - 1
	}
	turn := m.script[i]
	if m.wire == "claude" {
		if r.URL.Path != "/v1/messages" || r.Header.Get("anthropic-version") == "" {
			w.WriteHeader(404)
			return
		}
		blocks := []map[string]any{}
		if turn.text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": turn.text})
		}
		for _, c := range turn.calls {
			var in any
			_ = json.Unmarshal(c.Args, &in)
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": in})
		}
		stop := "end_turn"
		if len(turn.calls) > 0 {
			stop = "tool_use"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"role": "assistant", "content": blocks, "stop_reason": stop})
		return
	}
	if r.URL.Path != "/v1/chat/completions" {
		w.WriteHeader(404)
		return
	}
	msg := map[string]any{"role": "assistant", "content": turn.text}
	if len(turn.calls) > 0 {
		var cs []map[string]any
		for _, c := range turn.calls {
			cs = append(cs, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Args)}})
		}
		msg["tool_calls"] = cs
		msg["reasoning_content"] = "thinking…"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}})
}

func dressScript() []mockTurn {
	menu := `{"items":[
	 {"kind":"outfit","path":["衣服","水手服"],"label":"穿上","objects":["Envy cat"],"default":true},
	 {"kind":"outfit","path":["衣服","水手服"],"label":"融化恶魔","objects":["Envy cat"],"materialsFrom":"Assets/Shop/SailorSet/Kaguya/Kaguya prefab/Melty Devil .prefab"},
	 {"kind":"part","path":["衣服","水手服"],"label":"鞋子","objects":["Envy cat/NOPE"]}]}`
	fixed := strings.Replace(menu, "NOPE", "UV2_Sandal", 1)
	return []mockTurn{
		{text: "先看看头像。", calls: []aiCall{{ID: "c1", Name: "inspect_avatar", Args: json.RawMessage(`{}`)}}},
		{calls: []aiCall{{ID: "c2", Name: "list_prefabs", Args: json.RawMessage(`{"folders":["Assets/Shop/SailorSet"]}`)}}},
		{calls: []aiCall{{ID: "c3", Name: "dress", Args: json.RawMessage(`{"prefab":"Assets/Shop/SailorSet/Kaguya/Kaguya prefab/Envy cat.prefab"}`)},
			{ID: "c4", Name: "inspect_object", Args: json.RawMessage(`{"path":"Envy cat"}`)}}},
		{calls: []aiCall{{ID: "c5", Name: "build_menu", Args: json.RawMessage(menu)}}},  // a wrong path: refused whole
		{calls: []aiCall{{ID: "c6", Name: "build_menu", Args: json.RawMessage(fixed)}}}, // corrected
		{text: "穿好了：水手服，带一个换色项和鞋子开关。"},
	}
}

func testAIRun(t *testing.T, provider string) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	llm := &mockLLM{wire: aiProvider(provider).Wire, script: dressScript()}
	srv := httptest.NewServer(llm)
	defer srv.Close()
	key := "secret-key-12345678"
	if err := aiSave(provider, srv.URL, "test-model", &key, nil); err != nil {
		t.Fatal(err)
	}
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", Folders: []string{"Assets/Shop/SailorSet"}}); err != nil {
		t.Fatal(err)
	}
	s := aiSessionFor(proj)
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "hi"}); err == nil {
		t.Error("a second run started while the first was going")
	}
	steps := waitRun(t, s)
	var kinds []string
	for _, x := range steps {
		k := x.Kind
		if x.Kind == "tool" {
			k = x.Tool + map[bool]string{true: "+", false: "-"}[x.OK]
		}
		kinds = append(kinds, k)
	}
	want := "user refresh+ say inspect_avatar+ list_prefabs+ dress+ inspect_object+ build_menu- build_menu+ say"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("steps:\n got %s\nwant %s\n%+v", got, want, steps)
	}
	if !strings.Contains(steps[0].Text, "Assets/Shop/SailorSet") || !strings.Contains(steps[0].Text, defaultHierarchy) {
		t.Errorf("task text %q", steps[0].Text)
	}
	if !strings.Contains(steps[7].Out, "找不到") {
		t.Errorf("failed step says %q", steps[7].Out)
	}
	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.seen) != 6 {
		t.Fatalf("%d requests", len(llm.seen))
	}
	for _, a := range llm.auth {
		if !strings.Contains(a, key) {
			t.Errorf("auth %q", a)
		}
	}
	first, lastReq := llm.seen[0], llm.seen[5]
	b, _ := json.Marshal(lastReq)
	js := string(b)
	// the tools were offered, the system prompt went along, every call got its answer back, and the big
	// "menu" part of a build_menu answer was left out
	for _, w := range []string{"build_menu", "unity_skill", "头像下找不到", "Kaguya_Test", "c6"} {
		if !strings.Contains(js, w) {
			t.Errorf("last request lacks %q", w)
		}
	}
	if strings.Contains(js, "xxxxxxxxxx") {
		t.Error("the whole menu went back to the AI")
	}
	if provider == "claude" {
		if s, _ := first["system"].(string); !strings.Contains(s, "改模助手") || first["max_tokens"] == nil {
			t.Errorf("claude request %v", first)
		}
		if !strings.Contains(js, `"tool_result"`) || !strings.Contains(js, `"is_error":true`) || !strings.Contains(js, `"input_schema"`) {
			t.Errorf("claude wire: %s", js[:400])
		}
	} else {
		if !strings.Contains(js, `"role":"tool"`) || !strings.Contains(js, `"tool_call_id":"c5"`) || !strings.Contains(js, `"reasoning_content"`) || !strings.Contains(js, `"role":"system"`) {
			t.Errorf("openai wire: %s", js[:400])
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.dressed) != 1 || len(u.menus) != 1 {
		t.Errorf("unity: %v", u.calls)
	}
	if _, _, changes := s.snapshot(); changes != 2 {
		t.Errorf("changes %d", changes)
	}
	// a follow-up in the same conversation carries the history
	llm.script = []mockTurn{{text: "好的。"}}
	llm.seen = nil
	llm.mu.Unlock()
	u.mu.Unlock()
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "把鞋子开关改名叫凉鞋"}); err != nil {
		t.Fatal(err)
	}
	waitRun(t, s)
	llm.mu.Lock()
	u.mu.Lock()
	b, _ = json.Marshal(llm.seen[0])
	if !strings.Contains(string(b), "凉鞋") || !strings.Contains(string(b), "c6") {
		t.Error("follow-up without history")
	}
}

func TestAIRunOpenAI(t *testing.T)   { testAIRun(t, "openai") }
func TestAIRunDeepSeek(t *testing.T) { testAIRun(t, "deepseek") }
func TestAIRunClaude(t *testing.T)   { testAIRun(t, "claude") }

func TestAIServiceErrors(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	// nothing set up
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "hi"}); err == nil || !strings.Contains(err.Error(), "AI 服务") {
		t.Errorf("no service: %v", err)
	}
	llm := &mockLLM{wire: "openai", script: []mockTurn{{text: "收到"}}}
	srv := httptest.NewServer(llm)
	defer srv.Close()
	key := "k-1234567890"
	_ = aiSave("openai", srv.URL, "bad-model", &key, nil)
	c, err := newAIClient(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aiTest(context.Background(), c); err == nil || !strings.Contains(err.Error(), "模型名称不正确") || !strings.Contains(err.Error(), "bad-model") {
		t.Errorf("bad model: %v", err)
	}
	_ = aiSave("openai", srv.URL, "ok-model", nil, nil)
	c, _ = newAIClient(st)
	note, err := aiTest(context.Background(), c)
	if err != nil || !strings.Contains(note, "收到") {
		t.Errorf("test: %q %v", note, err)
	}
	ms, err := c.models(context.Background())
	if err != nil || !reflect.DeepEqual(ms, []string{"model-a", "model-b"}) {
		t.Errorf("models %v %v", ms, err)
	}
	// Unity not there: the dress run says so before the AI is asked anything
	llm.mu.Lock()
	llm.seen = nil
	llm.mu.Unlock()
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", Folders: []string{"Assets/X"}}); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	if last := steps[len(steps)-1]; last.Kind != "error" || !strings.Contains(last.Text, "AI 插件") {
		t.Errorf("without Unity: %+v", steps)
	}
	llm.mu.Lock()
	if len(llm.seen) != 0 {
		t.Error("the AI was asked although Unity is not there")
	}
	llm.mu.Unlock()
	// a service that is not there at all
	// another address: the saved key is not sent there, it has to be entered again
	if err := aiSave("openai", "http://127.0.0.1:9/v1", "m", nil, nil); err != errKeyForOtherHost {
		t.Errorf("new address with the old key: %v", err)
	}
	if got := loadAIConfig().profile("openai").BaseURL; got != srv.URL {
		t.Errorf("the refused address was saved: %s", got)
	}
	if err := aiSave("openai", "http://127.0.0.1:9/v1", "m", &key, nil); err != nil {
		t.Fatal(err)
	}
	c, _ = newAIClient(st)
	if _, err := aiTest(context.Background(), c); err == nil || !strings.Contains(err.Error(), "无法连接") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestAIRiskyConfirm(t *testing.T) {
	for name, want := range map[string]bool{"asset_delete": true, "scene_save": true, "scene_load": true, "editor_execute_menu": true, "script_create": true, "gameobject_delete": true,
		"gameobject_find": false, "component_get_properties": false, "material_set_color": false, "console_get_logs": false, "asset_find": false, "texture_set_platform_settings": false} {
		if riskySkill(name) != want {
			t.Errorf("riskySkill(%s) = %v", name, !want)
		}
	}
	st := testkit.NewStore(t)
	proj := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	var mu sync.Mutex
	var ran []string
	skills := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/skills" {
			_, _ = io.WriteString(w, `{"skills":[
			 {"name":"asset_delete","operation":["Delete"],"readOnly":false,"mutatesAssets":true,"riskLevel":"medium"},
			 {"name":"gameobject_find","operation":["Query"],"readOnly":true,"riskLevel":"low"},
			 {"name":"gameobject_set_active","operation":["Modify"],"readOnly":false,"mutatesScene":true,"riskLevel":"low"},
			 {"name":"prefab_apply","operation":["Modify"],"readOnly":false,"mutatesScene":true,"mutatesAssets":true,"riskLevel":"medium"},
			 {"name":"material_set_color","operation":["Modify"],"readOnly":false,"mutatesAssets":true,"riskLevel":"low"},
			 {"name":"editor_stop","operation":["Execute"],"readOnly":false,"riskLevel":"low"},
			 {"name":"test_run","operation":["Execute"],"readOnly":false,"riskLevel":"low"},
			 {"name":"scene_save","operation":["Execute"],"readOnly":false,"mutatesAssets":true,"riskLevel":"high"},
			 {"name":"old_modify","operation":["Modify"]},{"name":"old_get_info","operation":["Query"]},{"name":"old_delete_thing","operation":[]}]}`)
			return
		}
		if r.URL.Path != "/health" {
			mu.Lock()
			ran = append(ran, r.URL.Path)
			mu.Unlock()
		}
		_, _ = io.WriteString(w, `{"status":"success","result":{"deleted":"Assets/Old"}}`)
	}))
	defer skills.Close()
	addr := skills.Listener.Addr().String()
	_ = os.MkdirAll(filepath.Join(home, ".unity_skills"), 0755)
	_ = os.WriteFile(filepath.Join(home, ".unity_skills", "registry.json"), []byte(fmt.Sprintf(`{%q:{"path":%q,"port":%s,"last_active":%d}}`, proj, proj, addr[strings.LastIndex(addr, ":")+1:], time.Now().Unix())), 0644)
	llm := &mockLLM{wire: "openai", script: []mockTurn{
		{calls: []aiCall{{ID: "d1", Name: "unity_skill", Args: json.RawMessage(`{"name":"asset_delete","args":{"assetPath":"Assets/Old"}}`)}}},
		{text: "好的，没有删。"},
	}}
	srv := httptest.NewServer(llm)
	defer srv.Close()
	key := "k-1234567890"
	_ = aiSave("openai", srv.URL, "m", &key, nil)
	s := aiSessionFor(proj)
	// what is asked about, by what UnitySkills says of each skill
	man := skillsManifest(context.Background(), proj)
	for name, ask := range map[string]bool{"asset_delete": true, "prefab_apply": true, "material_set_color": true, "editor_stop": true, "test_run": true, "scene_save": true,
		"old_delete_thing": true, "gameobject_find": false, "gameobject_set_active": false, "old_modify": false, "old_get_info": false, "no_such_skill": false} {
		if why := consentReason(man, name); (why != "") != ask {
			t.Errorf("consentReason(%s) = %q, want asked=%v", name, why, ask)
		}
	}
	if consentReason(nil, "gameobject_find") == "" {
		t.Error("without the manifest nothing may run unasked")
	}
	run := func(allow bool) []AIStep {
		llm.mu.Lock()
		llm.seen = nil
		llm.mu.Unlock()
		if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "删掉 Assets/Old"}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 300; i++ {
			_, steps, _ := s.snapshot()
			if n := len(steps); n > 0 && steps[n-1].Kind == "ask" && steps[n-1].Busy {
				if !strings.Contains(steps[n-1].Text, "asset_delete") || !strings.Contains(steps[n-1].Text, "Assets/Old") {
					t.Errorf("question %q", steps[n-1].Text)
				}
				if !s.answer(allow, false) {
					t.Error("nobody was waiting for the answer")
				}
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		return waitRun(t, s)
	}
	run(false)
	mu.Lock()
	if len(ran) != 0 {
		t.Errorf("ran without consent: %v", ran)
	}
	mu.Unlock()
	llm.mu.Lock()
	b, _ := json.Marshal(llm.seen[len(llm.seen)-1])
	llm.mu.Unlock()
	if !strings.Contains(string(b), "没有同意") {
		t.Error("the AI was not told about the refusal")
	}
	s.reset()
	run(true)
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 1 || ran[0] != "/skill/asset_delete" {
		t.Errorf("after consent: %v", ran)
	}
	if s.answer(true, false) {
		t.Error("an answer without a question was taken")
	}
}
