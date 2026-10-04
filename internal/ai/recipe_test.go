package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

const (
	sailor = "Assets/Shop/SailorSet/Kaguya/Kaguya prefab/Envy cat.prefab"
	khaki  = "Assets/_头发/樱发/Khaki.prefab"
)

// recipeRig: a project with the sailor set and a hair style, a library that knows the sailor set, and a
// pipeline run (no AI) that put both on.
func recipeRig(t *testing.T) (*core.Store, string, *sceneUnity) {
	t.Helper()
	st := testkit.NewStore(t)
	proj := t.TempDir()
	touch(t, proj, "ProjectSettings/ProjectVersion.txt", khaki)
	for _, p := range testPrefabs() {
		touch(t, proj, p.Path)
	}
	st.Assets = []*core.Asset{{Key: "booth:1234567", AltKey: "path:Downloads/sailor", Name: "恶魔水手服", Category: "衣服", BoothID: "1234567", Bases: []string{"Kaguya", "Manuka"},
		Usage: []core.Usage{{Project: filepath.Base(proj), Status: "used", Folder: "Assets/Shop/SailorSet"}}}}
	st.Booth = map[string]*core.BoothInfo{"1234567": {ID: "1234567", Name: "Devil Sailor", URL: "https://booth.pm/ja/items/1234567"}}
	u := &sceneUnity{live: true, extra: []prefabInfo{{Path: khaki, Name: "Khaki", Renderers: 3, MeshNames: []string{"Hair", "hairpin1", "hairpin2"}, MAReady: true}}}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{
		{Folder: "Assets/Shop/SailorSet", Kind: "衣服", Name: "水手服"}, {Folder: "Assets/_头发/樱发", Kind: "头发", Name: "樱发"}}}); err != nil {
		t.Fatal(err)
	}
	if steps := waitRun(t, aiSessionFor(proj)); steps[len(steps)-1].Kind != "say" {
		t.Fatalf("the run failed: %+v", steps[len(steps)-1])
	}
	return st, proj, u
}

func itemRows(m RecipeMenu) []string {
	var rows []string
	for _, it := range m.Items {
		row := it.Kind + " " + strings.Join(it.Path, "/") + ":" + it.Label + " " + fmt.Sprint(it.Objects)
		if it.Parameter != "" {
			row += " @" + it.Parameter
		}
		if it.Default {
			row += " *"
		}
		if it.MaterialsFrom != "" {
			row += " <" + filepath.Base(it.MaterialsFrom)
		}
		rows = append(rows, row)
	}
	return rows
}

// a run saved as a recipe: the assets with what the library knows, the plan that was built, nothing of this computer
func TestRecipeSave(t *testing.T) {
	st, proj, _ := recipeRig(t)
	if !RecipeReady(proj) {
		t.Fatal("nothing to save after a run")
	}
	if _, err := SaveRecipe(st, proj, "  ", ""); err == nil || !strings.Contains(err.Error(), "名称") {
		t.Errorf("a recipe without a name: %v", err)
	}
	r, err := SaveRecipe(st, proj, " 水手服  与樱发 ", "第一套\r\n日常用")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "水手服 与樱发" || r.Note != "第一套\n日常用" || r.Format != recipeFormat || r.Schema != recipeSchema || r.App != core.AppVersion || r.Plugin == "" || r.Hierarchy != defaultHierarchy {
		t.Errorf("head %+v", r)
	}
	if r.Base.Prefab != "Assets/IKUSIA/kaguya/kaguya.prefab" || r.Base.Name != "Kaguya" {
		t.Errorf("base %+v", r.Base)
	}
	if len(r.Assets) != 2 {
		t.Fatalf("assets %+v", r.Assets)
	}
	on := true
	want := RecipeAsset{Key: "booth:1234567", Name: "恶魔水手服", BoothID: "1234567", URL: "https://booth.pm/ja/items/1234567", Prefab: sailor, Kind: "衣服", Version: "Kaguya", Object: "Envy cat", Active: &on}
	if !reflect.DeepEqual(r.Assets[0], want) {
		t.Errorf("sailor %+v", r.Assets[0])
	}
	if a := r.Assets[1]; a.Key != "" || a.Name != "樱发" || a.Prefab != khaki || a.Kind != "头发" || a.Object != "Khaki" || a.URL != "" {
		t.Errorf("hair %+v", a)
	}
	if len(r.Menus) != 1 || r.Menus[0].Parameter != "Clothtoggle" || r.Menus[0].Root != "" {
		t.Fatalf("menus %+v", r.Menus)
	}
	rows := itemRows(r.Menus[0])
	for _, w := range []string{"outfit 衣服/水手服/配色:Envy cat [Envy cat] @Clothtoggle *", "outfit 衣服/水手服/配色:Heart Catcher [Envy cat] @Clothtoggle <Heart Catcher .prefab",
		"part 衣服/水手服:外套 [Envy cat/UV4_Cardigan]", "strip 衣服:一键脱光 [] @Clothtoggle", "outfit 头发:原装头发 [Hair] @Hair_Choose", "outfit 头发/樱发:戴上 [Khaki] @Hair_Choose *", "part 头发/樱发:头饰 [Khaki/hairpin1 Khaki/hairpin2]"} {
		if !core.ContainsStr(rows, w) {
			t.Errorf("the plan lacks %q:\n%s", w, strings.Join(rows, "\n"))
		}
	}
	// the file: there, whole, and nothing in it says where this computer keeps things
	b, err := os.ReadFile(filepath.Join(recipesDir(), r.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{proj, filepath.ToSlash(proj), core.DataDir, "path:Downloads"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("the file names %q", bad)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(recipesDir(), "*.tmp")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
	if l := ListRecipes(); len(l) != 1 || l[0].Name != r.Name || l[0].Assets != 2 || l[0].Items != len(rows) || l[0].Base != "Kaguya" {
		t.Errorf("list %+v", l)
	}
	if r2, err := RenameRecipe(r.ID, "新名字", ""); err != nil || r2.Name != "新名字" || ListRecipes()[0].Name != "新名字" || ListRecipes()[0].Note != "" {
		t.Errorf("rename: %v %+v", err, r2)
	}
	if _, err := RenameRecipe(r.ID, strings.Repeat("长", 61), ""); err == nil {
		t.Error("a name of 61 characters was taken")
	}
	for _, id := range []string{"../library", "..", "", "nope-nope", r.ID + "/../x"} {
		if _, err := LoadRecipe(id); err == nil {
			t.Errorf("LoadRecipe(%q) found something", id)
		}
		if DeleteRecipe(id) == nil {
			t.Errorf("DeleteRecipe(%q) deleted something", id)
		}
	}
	if err := DeleteRecipe(r.ID); err != nil || len(ListRecipes()) != 0 {
		t.Errorf("delete: %v", err)
	}
}

// what the player undid is not in the recipe; neither is a session without a run
func TestRecipeFollowsUndo(t *testing.T) {
	st, proj, _ := recipeRig(t)
	s := aiSessionFor(proj)
	if _, err := s.undo(t.Context(), 0); err != nil { // the menu
		t.Fatal(err)
	}
	r, err := SaveRecipe(st, proj, "半套", "")
	if err != nil || len(r.Assets) != 2 || len(r.Menus) != 0 {
		t.Fatalf("after one undo: %v %+v", err, r)
	}
	if _, err := s.undo(t.Context(), 0); err != nil { // the hair
		t.Fatal(err)
	}
	if r, _ = SaveRecipe(st, proj, "半套", ""); r == nil || len(r.Assets) != 1 || r.Assets[0].Prefab != sailor {
		t.Errorf("after two: %+v", r)
	}
	s.reset()
	if _, err := SaveRecipe(st, proj, "空", ""); err == nil || RecipeReady(proj) {
		t.Errorf("a new conversation still has a run to save: %v", err)
	}
}

// what the AI built is saved as it was sent: the build_menu arguments of its calls, in order
func TestRecipeFromAIRun(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	touch(t, proj, "ProjectSettings/ProjectVersion.txt", sailor)
	u := &sceneUnity{live: true}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	s := aiSessionFor(proj)
	call := func(name, args string) {
		t.Helper()
		var a map[string]any
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			t.Fatal(err)
		}
		if _, short, ok := s.runTool(t.Context(), name, a); !ok {
			t.Fatalf("%s: %s", name, short)
		}
	}
	call("inspect_avatar", `{}`)
	call("dress", `{"prefab":"`+sailor+`","active":false}`)
	call("build_menu", `{"parameter":"Outfit","items":[{"kind":"outfit","path":["主菜单","衣柜"],"label":"水手服","objects":["Envy cat"],"default":true},
	 {"kind":"outfit","path":"衣柜","label":"融化恶魔","objects":["Envy cat"],"materialsFrom":"Assets\\Shop\\SailorSet\\Kaguya\\Kaguya prefab\\Melty Devil .prefab","junk":1},
	 {"kind":"skin","path":["皮肤"],"label":"小麦色","materials":[{"object":"Body","slot":1,"material":"Assets/Skin/Tan.mat"}]}]}`)
	if _, _, ok := s.runTool(t.Context(), "build_menu", map[string]any{"items": []any{map[string]any{"kind": "part", "path": []any{"衣柜"}, "label": "鞋子", "objects": []any{"Envy cat/NOPE"}}}}); ok {
		t.Fatal("a plan that failed was carried out")
	}
	r, err := SaveRecipe(st, proj, "AI 做的", "")
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if len(r.Assets) != 1 || r.Assets[0].Prefab != sailor || !reflect.DeepEqual(r.Assets[0].Active, &off) || r.Assets[0].Kind != "衣服" || r.Base.Name != "Kaguya" {
		t.Errorf("assets %+v base %+v", r.Assets, r.Base)
	}
	if len(r.Menus) != 1 || r.Menus[0].Parameter != "Outfit" {
		t.Fatalf("menus %+v", r.Menus)
	}
	want := []string{"outfit 衣柜:水手服 [Envy cat] *", "outfit 衣柜:融化恶魔 [Envy cat] <Melty Devil .prefab", "skin 皮肤:小麦色 []"}
	if rows := itemRows(r.Menus[0]); !reflect.DeepEqual(rows, want) {
		t.Errorf("plan:\n%s", strings.Join(rows, "\n"))
	}
	if it := r.Menus[0].Items[1]; it.MaterialsFrom != "Assets/Shop/SailorSet/Kaguya/Kaguya prefab/Melty Devil .prefab" {
		t.Errorf("materialsFrom %q", it.MaterialsFrom)
	}
	if it := r.Menus[0].Items[2]; !reflect.DeepEqual(it.Materials, []RecipeMaterial{{Object: "Body", Slot: 1, Material: "Assets/Skin/Tan.mat"}}) {
		t.Errorf("materials %+v", it.Materials)
	}
}

// export and import: one file, the same recipe; a file that is not what a recipe is, is refused
func TestRecipeExportImport(t *testing.T) {
	st, proj, _ := recipeRig(t)
	r, err := SaveRecipe(st, proj, `我的/方案:1`, "给朋友")
	if err != nil {
		t.Fatal(err)
	}
	// a record named after a folder of this computer's library does not travel
	r.Assets[1].Key = "path:Downloads/樱发"
	if err := writeRecipe(r); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file, err := ExportRecipe(r.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(file) != dir || !strings.HasSuffix(file, RecipeExt) || strings.ContainsAny(filepath.Base(file), `/\:`) {
		t.Errorf("exported to %q", file)
	}
	if again, _ := ExportRecipe(r.ID, dir); again == file || !strings.Contains(again, "(2)") {
		t.Errorf("a second export overwrote the first: %q", again)
	}
	if _, err := ExportRecipe(r.ID, filepath.Join(dir, "nope")); err == nil {
		t.Error("exported into a folder that is not there")
	}
	text, _ := os.ReadFile(file)
	for _, bad := range []string{`"id"`, `"imported"`, "path:Downloads", proj} {
		if strings.Contains(string(text), bad) {
			t.Errorf("the exported file has %s", bad)
		}
	}
	got, err := ImportRecipe(text, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "" || len(ListRecipes()) != 1 {
		t.Error("looking at a file saved it")
	}
	if got.Name != r.Name || got.Note != r.Note || !reflect.DeepEqual(got.Menus, r.Menus) || got.Assets[0] != r.Assets[0] && !reflect.DeepEqual(got.Assets[0], r.Assets[0]) || !got.Imported {
		t.Errorf("read back %+v", got)
	}
	needs := RecipeNeeds(st, got)
	if len(needs) != 2 || needs[0].Link != "https://booth.pm/ja/items/1234567" || needs[0].LibKey != "booth:1234567" || needs[1].Link != "" || needs[1].LibKey != "" {
		t.Errorf("needs %+v", needs)
	}
	saved, err := ImportRecipe(append([]byte("\xef\xbb\xbf"), text...), true)
	if err != nil || saved.ID == "" || saved.ID == r.ID || len(ListRecipes()) != 2 {
		t.Fatalf("import: %v %+v", err, saved)
	}
	if again, _ := LoadRecipe(saved.ID); again == nil || !again.Imported || again.Name != r.Name {
		t.Errorf("the imported recipe on disk: %+v", again)
	}

	// ---- files that are not what a recipe is ----
	var good map[string]any
	_ = json.Unmarshal(text, &good)
	with := func(change func(m map[string]any)) []byte {
		var m map[string]any
		_ = json.Unmarshal(text, &m)
		change(m)
		b, _ := json.Marshal(m)
		return b
	}
	asset := func(m map[string]any, i int) map[string]any { return m["assets"].([]any)[i].(map[string]any) }
	item := func(m map[string]any, i int) map[string]any {
		return m["menus"].([]any)[0].(map[string]any)["items"].([]any)[i].(map[string]any)
	}
	for name, c := range map[string]struct {
		file []byte
		says string
	}{
		"not json":             {[]byte("MZ\x90\x00"), "JSON"},
		"another format":       {with(func(m map[string]any) { m["format"] = "something" }), "不是 MioVRCA 装配方案"},
		"a newer schema":       {with(func(m map[string]any) { m["schema"] = 2 }), "更新版本"},
		"schema 0":             {with(func(m map[string]any) { m["schema"] = 0 }), "版本标记"},
		"unknown field":        {with(func(m map[string]any) { m["exec"] = "calc.exe" }), "无法识别"},
		"unknown item field":   {with(func(m map[string]any) { item(m, 0)["script"] = "x" }), "无法识别"},
		"unknown asset field":  {with(func(m map[string]any) { asset(m, 0)["localPath"] = `C:\Users\me` }), "无法识别"},
		"trailing content":     {append(append([]byte{}, text...), []byte(`{"format":"x"}`)...), "多余"},
		"too big":              {append(append([]byte{}, text...), []byte(strings.Repeat(" ", RecipeMaxFile))...), "过大"},
		"no name":              {with(func(m map[string]any) { m["name"] = "  " }), "缺少名称"},
		"a long name":          {with(func(m map[string]any) { m["name"] = strings.Repeat("名", 61) }), "过长"},
		"a long note":          {with(func(m map[string]any) { m["note"] = strings.Repeat("注", 501) }), "过长"},
		"path traversal":       {with(func(m map[string]any) { asset(m, 0)["prefab"] = "Assets/../../Windows/x.prefab" }), "不是有效的工程内路径"},
		"absolute path":        {with(func(m map[string]any) { asset(m, 0)["prefab"] = "C:/Users/me/x.prefab" }), "Assets/"},
		"rooted path":          {with(func(m map[string]any) { asset(m, 0)["prefab"] = "/Assets/x.prefab" }), "Assets/"},
		"backslashes":          {with(func(m map[string]any) { asset(m, 0)["prefab"] = `Assets\Shop\x.prefab` }), "Assets/"},
		"not a prefab":         {with(func(m map[string]any) { asset(m, 0)["prefab"] = "Assets/Shop/x.exe" }), ".prefab"},
		"under Packages":       {with(func(m map[string]any) { asset(m, 0)["prefab"] = "Packages/x/y.prefab" }), "Assets/"},
		"base outside":         {with(func(m map[string]any) { m["base"].(map[string]any)["prefab"] = "Assets/a/../../b.prefab" }), "不是有效的工程内路径"},
		"colour outside":       {with(func(m map[string]any) { item(m, 1)["materialsFrom"] = "../x.prefab" }), "Assets/"},
		"object path up":       {with(func(m map[string]any) { item(m, 0)["objects"] = []string{"../Other/Body"} }), "物体路径"},
		"object path rooted":   {with(func(m map[string]any) { item(m, 0)["objects"] = []string{"/Body"} }), "物体路径"},
		"unknown kind":         {with(func(m map[string]any) { item(m, 0)["kind"] = "delete" }), "类型无效"},
		"unknown asset kind":   {with(func(m map[string]any) { asset(m, 0)["kind"] = "病毒" }), "类别无效"},
		"a foreign link":       {with(func(m map[string]any) { asset(m, 0)["url"] = "https://evil.example/booth.pm/items/1" }), "链接"},
		"a script link":        {with(func(m map[string]any) { asset(m, 0)["url"] = "javascript:alert(1)" }), "链接"},
		"a plain http link":    {with(func(m map[string]any) { asset(m, 0)["url"] = "http://booth.pm/ja/items/1" }), "链接"},
		"a bad item number":    {with(func(m map[string]any) { asset(m, 0)["boothId"] = "12 34<script>" }), "商品编号"},
		"a parameter":          {with(func(m map[string]any) { item(m, 0)["parameter"] = "a b" }), "参数名"},
		"a control character":  {with(func(m map[string]any) { item(m, 0)["label"] = "a\x00b" }), "无法显示"},
		"a deep menu":          {with(func(m map[string]any) { item(m, 0)["path"] = []string{"1", "2", "3", "4", "5", "6", "7"} }), "过深"},
		"an empty plan":        {with(func(m map[string]any) { m["menus"] = []any{map[string]any{"items": []any{}}} }), "空的菜单计划"},
		"nothing in it":        {with(func(m map[string]any) { m["menus"], m["assets"] = []any{}, []any{} }), "没有素材"},
		"the same asset twice": {with(func(m map[string]any) { m["assets"] = append(m["assets"].([]any), asset(m, 0)) }), "重复"},
		"too many assets": {with(func(m map[string]any) {
			var l []any
			for i := 0; i < recipeAssetsMax+1; i++ {
				l = append(l, map[string]any{"name": "a", "prefab": fmt.Sprintf("Assets/a/%d.prefab", i), "kind": "衣服", "object": "a"})
			}
			m["assets"] = l
		}), "过多"},
		"a wrong type": {with(func(m map[string]any) { m["assets"] = "all of them" }), "JSON"},
	} {
		if _, err := ImportRecipe(c.file, true); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v (should say %q)", name, err, c.says)
		}
	}
	if len(ListRecipes()) != 2 {
		t.Errorf("a refused file was saved: %d recipes", len(ListRecipes()))
	}
	// a record of this computer's kind in somebody's file is dropped, not believed
	odd, err := ImportRecipe(with(func(m map[string]any) { asset(m, 0)["key"] = "path:C:/Users/someone/x" }), false)
	if err != nil || odd.Assets[0].Key != "" {
		t.Errorf("a path key: %v %+v", err, odd)
	}
}

// A recipe file that got into the folder some other way (a library import writes the recipes of another
// computer there) is held to the same rules as one that is imported by hand: what is not a valid recipe is
// neither listed nor loaded.
func TestLoadRecipeValidates(t *testing.T) {
	st, proj, _ := recipeRig(t)
	r, err := SaveRecipe(st, proj, "方案", "")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(filepath.Join(recipesDir(), r.ID+".json"))
	put := func(id string, change func(m map[string]any)) {
		var m map[string]any
		_ = json.Unmarshal(text, &m)
		change(m)
		b, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(recipesDir(), id+".json"), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	asset := func(m map[string]any, i int) map[string]any { return m["assets"].([]any)[i].(map[string]any) }
	put("zz-good-copy", func(m map[string]any) { m["name"] = "别人的方案" })
	put("zz-outside-prefab", func(m map[string]any) { asset(m, 0)["prefab"] = "../../outside.prefab" })
	put("zz-base-outside", func(m map[string]any) { m["base"].(map[string]any)["prefab"] = "Packages/../../x.prefab" })
	put("zz-script-link", func(m map[string]any) { asset(m, 0)["url"] = "javascript:alert(1)" })
	put("zz-newer-schema", func(m map[string]any) { m["schema"] = 99 })
	_ = os.WriteFile(filepath.Join(recipesDir(), "zz-not-json.json"), []byte("{"), 0644)
	var names []string
	for _, x := range ListRecipes() {
		names = append(names, x.ID)
	}
	sort.Strings(names)
	if want := []string{r.ID, "zz-good-copy"}; !reflect.DeepEqual(names, want) {
		t.Errorf("listed: %v, want %v", names, want)
	}
	for _, id := range []string{"zz-outside-prefab", "zz-base-outside", "zz-script-link", "zz-newer-schema", "zz-not-json"} {
		for i := 0; i < 2; i++ { // (the second time it is known to be bad without being read)
			if x, err := LoadRecipe(id); err == nil {
				t.Errorf("%s was loaded: %+v", id, x)
			}
		}
		if _, err := PreviewRecipe(st, id, proj); err == nil {
			t.Errorf("%s can be previewed", id)
		}
	}
	if x, err := LoadRecipe("zz-good-copy"); err != nil || x.Name != "别人的方案" || x.ID != "zz-good-copy" {
		t.Errorf("the valid one: %+v %v", x, err)
	}
	// put right (and written again): it is read anew
	time.Sleep(15 * time.Millisecond)
	put("zz-script-link", func(m map[string]any) {})
	now := time.Now().Add(time.Second)
	_ = os.Chtimes(filepath.Join(recipesDir(), "zz-script-link.json"), now, now)
	if _, err := LoadRecipe("zz-script-link"); err != nil {
		t.Errorf("a file that was put right: %v", err)
	}
}

func TestShopURL(t *testing.T) {
	for u, want := range map[string]bool{"https://booth.pm/ja/items/123": true, "https://shop.booth.pm/items/1": true, "https://x.gumroad.com/l/abc": true,
		"https://booth.pm.evil.example/": false, "https://evilbooth.pm/": false, "http://booth.pm/": false, "https://user:pw@booth.pm/": false, "https://booth.pm:8443/": false, "booth.pm/items/1": false, "": false} {
		if shopURL(u) != want {
			t.Errorf("shopURL(%q) = %v", u, !want)
		}
	}
}

// a recipe in another project: same path, through the library's record, missing; and on another base body
func TestRecipeResolve(t *testing.T) {
	st, _, _ := recipeRig(t)
	on := true
	r := &Recipe{Format: recipeFormat, Schema: 1, Name: "r", Base: RecipeBase{Name: "Manuka"}, Assets: []RecipeAsset{
		{Key: "booth:1234567", Name: "恶魔水手服", Prefab: "Assets/Shop/SailorSet/Manuka/Manuka prefab/Envy cat.prefab", Kind: "衣服", Version: "Manuka", Object: "Envy cat", Active: &on},
		{Name: "樱发", Prefab: khaki, Kind: "头发", Object: "Khaki"},
		{BoothID: "7654321", Name: "球棒", Prefab: "Assets/_道具/Bat/bat.prefab", Kind: "道具", Object: "bat"},
	}}
	// (1) the same layout; the avatar is a Kaguya, the recipe has the Manuka version: the package's Kaguya version is taken
	p1 := t.TempDir()
	touch(t, p1, "ProjectSettings/ProjectVersion.txt", sailor, "Assets/Shop/SailorSet/Manuka/Manuka prefab/Envy cat.prefab", khaki)
	av := &avatarInfo{Name: "Kaguya_Test", Prefab: "Assets/IKUSIA/kaguya/kaguya.prefab"}
	av.Children = append(av.Children, avatarChild{Name: "Khaki", Kind: "outfit", Prefab: khaki})
	rows := resolveRecipe(st, p1, r, av)
	if rows[0].Status != "dress" || rows[0].Resolved != sailor || rows[0].Via != "version" || rows[0].OtherBase != "" {
		t.Errorf("version for this body: %+v", rows[0])
	}
	if rows[1].Status != "worn" || rows[1].Via != "path" {
		t.Errorf("hair on the avatar: %+v", rows[1])
	}
	if rows[2].Status != "missing" || rows[2].LibKey != "" || rows[2].Link != "https://booth.pm/ja/items/7654321" {
		t.Errorf("missing bat: %+v", rows[2])
	}
	// (2) only the Manuka version is there: used, and said to be for another body
	p2 := t.TempDir()
	touch(t, p2, "ProjectSettings/ProjectVersion.txt", "Assets/Shop/SailorSet/Manuka/Manuka prefab/Envy cat.prefab")
	rows = resolveRecipe(st, p2, r, av)
	if rows[0].Status != "dress" || rows[0].Via != "path" || rows[0].OtherBase != "Manuka" || rows[1].Status != "missing" {
		t.Errorf("another body's version: %+v", rows[:2])
	}
	// (3) the player keeps outfits elsewhere: found through the library's record of the folder
	p3 := t.TempDir()
	touch(t, p3, "ProjectSettings/ProjectVersion.txt", "Assets/_衣服/水手/Kaguya/Kaguya prefab/Envy cat.prefab", "Assets/_衣服/水手/Manuka/Manuka prefab/Envy cat.prefab")
	st.Mu.Lock()
	st.Assets[0].Usage = append(st.Assets[0].Usage, core.Usage{Project: filepath.Base(p3), Status: "used", Folder: "Assets/_衣服/水手"})
	st.Mu.Unlock()
	rows = resolveRecipe(st, p3, r, av)
	if rows[0].Status != "dress" || rows[0].Via != "library" || rows[0].Resolved != "Assets/_衣服/水手/Kaguya/Kaguya prefab/Envy cat.prefab" || rows[0].LibKey != "booth:1234567" {
		t.Errorf("through the library: %+v", rows[0])
	}
	// … and without an avatar to go by, the version the recipe has
	if rows = resolveRecipe(st, p3, r, nil); rows[0].Resolved != "Assets/_衣服/水手/Manuka/Manuka prefab/Envy cat.prefab" {
		t.Errorf("no avatar: %+v", rows[0])
	}
}

// applying a recipe: dress and build_menu with the saved plan, the objects called what dress calls them
// here, what is missing left out and said, each step one to undo
func TestRecipeApply(t *testing.T) {
	st, proj, _ := recipeRig(t)
	r, err := SaveRecipe(st, proj, "水手服与樱发", "")
	if err != nil {
		t.Fatal(err)
	}
	// another project: the sailor set is there (its object will get another name), the hair is not
	p2 := t.TempDir()
	touch(t, p2, "ProjectSettings/ProjectVersion.txt")
	for _, p := range testPrefabs() {
		touch(t, p2, p.Path)
	}
	u2 := &sceneUnity{live: true, bones: 300, rename: map[string]string{sailor: "Envy cat (2)"}}
	unitytest.FakeUnity(t, p2, u2.answer)
	st.Projects = append(st.Projects, core.ProjectInfo{Name: "P2", Path: p2})

	pv, err := PreviewRecipe(st, r.ID, p2)
	if err != nil {
		t.Fatal(err)
	}
	if !pv.Alive || !pv.CanApply || pv.Avatar != "Kaguya_Test" || len(pv.Rows) != 2 || pv.Rows[0].Status != "dress" || pv.Rows[1].Status != "missing" || pv.Dropped != 2 || pv.Items != len(r.Menus[0].Items) {
		t.Errorf("preview %+v", pv)
	}
	u2.mu.Lock()
	if len(u2.dressed)+len(u2.menus) != 0 {
		t.Error("the preview changed the scene")
	}
	u2.mu.Unlock()

	if err := StartRecipeRun(st, p2, "nope"); err == nil {
		t.Error("a recipe that is not there was applied")
	}
	if err := StartRecipeRun(st, p2, r.ID); err != nil {
		t.Fatal(err)
	}
	s2 := aiSessionFor(p2)
	steps := waitRun(t, s2)
	last := steps[len(steps)-1]
	if last.Kind != "say" || steps[0].Text != "应用装配方案「水手服与樱发」" {
		t.Fatalf("steps %+v", steps)
	}
	u2.mu.Lock()
	if len(u2.dressed) != 1 || u2.dressed[0]["prefab"] != sailor || u2.dressed[0]["active"] != true || len(u2.menus) != 1 {
		t.Fatalf("dressed %v, %d menus", u2.dressed, len(u2.menus))
	}
	var rows []string
	for _, it := range u2.menus[0]["items"].([]any) {
		o := it.(map[string]any)
		rows = append(rows, fmt.Sprint(o["kind"], " ", strings.Join(asStrings(o["path"]), "/"), ":", o["label"], " ", asStrings(o["objects"]), " ", o["parameter"], " ", o["default"]))
	}
	for _, w := range []string{"outfit 衣服/水手服/配色:Envy cat [Envy cat (2)] <nil> true", "part 衣服/水手服:外套 [Envy cat (2)/UV4_Cardigan] <nil> <nil>",
		"strip 衣服:一键脱光 [] <nil> <nil>", "outfit 头发:原装头发 [Hair] Hair_Choose <nil>"} {
		if !core.ContainsStr(rows, w) {
			t.Errorf("the plan lacks %q:\n%s", w, strings.Join(rows, "\n"))
		}
	}
	for _, row := range rows {
		if strings.Contains(row, "Khaki") || strings.Contains(row, "[Envy cat]") || strings.Contains(row, "[Envy cat/") {
			t.Errorf("a row for what is not there: %s", row)
		}
	}
	if u2.menus[0]["root"] != nil || u2.menus[0]["parameter"] != "Clothtoggle" {
		t.Errorf("root %v parameter %v", u2.menus[0]["root"], u2.menus[0]["parameter"])
	}
	u2.mu.Unlock()
	for _, w := range []string{"已应用方案「水手服与樱发」", "新装配 1 项：恶魔水手服", "未导入、未装配 1 项：樱发", "有 2 个菜单项因所需素材缺失而未生成", "共有 300 个 PhysBone", "撤销上一步"} {
		if !strings.Contains(last.Text, w) {
			t.Errorf("summary lacks %q: %s", w, last.Text)
		}
	}
	// the same undo as after a run of the line: two changes, and the run can be saved as a recipe again
	if _, _, changes := s2.snapshot(); changes != 2 {
		t.Errorf("%d changes", changes)
	}
	again, err := SaveRecipe(st, p2, "再存一次", "")
	if err != nil || len(again.Assets) != 1 || again.Assets[0].Object != "Envy cat (2)" || again.Assets[0].Key != "" && again.Assets[0].Key != "booth:1234567" {
		t.Errorf("saved again: %v %+v", err, again)
	}

	// applied a second time: everything is in place, nothing new to undo; and the avatar's default is left alone
	if err := StartRecipeRun(st, p2, r.ID); err != nil {
		t.Fatal(err)
	}
	steps = waitRun(t, s2)
	if last := steps[len(steps)-1]; !strings.Contains(last.Text, "已在模型上 1 项") {
		t.Errorf("second time: %s", last.Text)
	}
	if _, _, changes := s2.snapshot(); changes != 2 {
		t.Errorf("second time: %d changes", changes)
	}
	u2.mu.Lock()
	second := u2.menus[len(u2.menus)-1]["items"].([]any)[0].(map[string]any)
	if _, said := u2.dressed[len(u2.dressed)-1]["active"]; said || second["default"] == true {
		t.Errorf("second time: active said %v, default %v", said, second["default"])
	}
	u2.mu.Unlock()
	if pv, _ = PreviewRecipe(st, r.ID, p2); pv == nil || pv.Rows[0].Status != "worn" {
		t.Errorf("preview after: %+v", pv)
	}

	// nothing of it in the project: said before anything is tried
	p3 := t.TempDir()
	touch(t, p3, "ProjectSettings/ProjectVersion.txt")
	unitytest.FakeUnity(t, p3, (&sceneUnity{live: true}).answer)
	if pv, _ = PreviewRecipe(st, r.ID, p3); pv == nil || pv.CanApply || !strings.Contains(pv.Why, "均未导入") {
		t.Errorf("empty project: %+v", pv)
	}
	// Unity not open: the files still say what is imported
	p4 := t.TempDir()
	touch(t, p4, "ProjectSettings/ProjectVersion.txt", sailor)
	if pv, _ = PreviewRecipe(st, r.ID, p4); pv == nil || pv.Alive || pv.CanApply || pv.Rows[0].Status != "dress" || !strings.Contains(pv.Why, "Unity 尚未连接") {
		t.Errorf("no Unity: %+v", pv)
	}
}

// an avatar that has an outfit menu of its own: the recipe's outfits join it, on its parameter
func TestRecipeJoinsExistingMenu(t *testing.T) {
	st, proj, _ := recipeRig(t)
	r, err := SaveRecipe(st, proj, "方案", "")
	if err != nil {
		t.Fatal(err)
	}
	p2 := t.TempDir()
	touch(t, p2, "ProjectSettings/ProjectVersion.txt", sailor)
	u2 := &sceneUnity{live: true}
	menu := []any{map[string]any{"object": "Menu_Outfits", "label": "衣柜", "type": "SubMenu", "children": []any{
		map[string]any{"object": "Menu_Outfits/a", "label": "a", "type": "Toggle", "parameter": "cloth_choose", "value": 1, "default": true, "toggles": []any{"A=on", "B=off"}},
		map[string]any{"object": "Menu_Outfits/b", "label": "b", "type": "Toggle", "parameter": "cloth_choose", "value": 2, "toggles": []any{"B=on", "A=off"}}}}}
	unitytest.FakeUnity(t, p2, func(cmd string, args map[string]any) (any, string) {
		res, e := u2.answer(cmd, args)
		if m, ok := res.(map[string]any); ok && cmd == "inspect" {
			m["avatars"].([]any)[0].(map[string]any)["maMenu"] = menu
		}
		return res, e
	})
	st.Projects = append(st.Projects, core.ProjectInfo{Name: "P2", Path: p2})
	pv, _ := PreviewRecipe(st, r.ID, p2)
	if pv == nil || !strings.Contains(strings.Join(pv.Notes, "\n"), "模型已有衣服菜单「Menu_Outfits」（参数 cloth_choose）") {
		t.Errorf("preview notes %+v", pv)
	}
	if err := StartRecipeRun(st, p2, r.ID); err != nil {
		t.Fatal(err)
	}
	if steps := waitRun(t, aiSessionFor(p2)); steps[len(steps)-1].Kind != "say" {
		t.Fatalf("steps %+v", steps)
	}
	u2.mu.Lock()
	defer u2.mu.Unlock()
	if len(u2.menus) != 1 || u2.menus[0]["root"] != "Menu_Outfits" || u2.menus[0]["parameter"] != "cloth_choose" {
		t.Fatalf("menus %+v", u2.menus)
	}
	first := u2.menus[0]["items"].([]any)[0].(map[string]any)
	if first["default"] == true || first["parameter"] != nil || u2.dressed[0]["active"] != false {
		t.Errorf("the avatar's own default was taken: item %v, dress %v", first, u2.dressed[0])
	}
}
