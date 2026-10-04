package ai

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

func TestPickCoverPrefabs(t *testing.T) {
	list := []prefabInfo{
		{Path: "Assets/Shop/Sailor/Kaguya/Prefab/Sailor_White.prefab", Name: "Sailor_White", Renderers: 9},
		{Path: "Assets/Shop/Sailor/Kaguya/Prefab/Sailor_Black.prefab", Name: "Sailor_Black", Renderers: 9},
		{Path: "Assets/Shop/Sailor/Kaguya/Prefab/Sailor_Kaguya_Full.prefab", Name: "Sailor_Kaguya_Full", Renderers: 20, WholeAvatar: true},
		{Path: "Assets/Shop/Sailor/Kaguya/Prefab/Sailor_PhysBone.prefab", Name: "Sailor_PhysBone", Renderers: 1},
		{Path: "Assets/Shop/Sailor/Kaguya/Prefab/Empty.prefab", Name: "Empty"},
	}
	got := pickCoverPrefabs("Sailor", "衣服", list)
	want := []string{"Assets/Shop/Sailor/Kaguya/Prefab/Sailor_Black.prefab", "Assets/Shop/Sailor/Kaguya/Prefab/Sailor_White.prefab",
		"Assets/Shop/Sailor/Kaguya/Prefab/Sailor_Kaguya_Full.prefab", "Assets/Shop/Sailor/Kaguya/Prefab/Sailor_PhysBone.prefab"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("colours of one outfit: the first colour, then the rest; the whole avatar and the helper last\n got %v", got)
	}
	// the prefab named after the asset wins over a bigger one
	list = []prefabInfo{
		{Path: "Assets/Shop/Cat Ears/Parts/Ribbon Set.prefab", Name: "Ribbon Set", Renderers: 6},
		{Path: "Assets/Shop/Cat Ears/Cat Ears.prefab", Name: "Cat Ears", Renderers: 2},
	}
	if got := pickCoverPrefabs("Cat-Ears", "配饰", list); len(got) != 2 || got[0] != "Assets/Shop/Cat Ears/Cat Ears.prefab" {
		t.Errorf("named after the asset: %v", got)
	}
	// a base body is shown whole
	list = []prefabInfo{
		{Path: "Assets/IKUSIA/kaguya/Parts/kaguya_hair.prefab", Name: "kaguya_hair", Renderers: 3},
		{Path: "Assets/IKUSIA/kaguya/kaguya.prefab", Name: "kaguya", Renderers: 12, WholeAvatar: true},
	}
	if got := pickCoverPrefabs("Kaguya_v1.06", "素体", list); got[0] != "Assets/IKUSIA/kaguya/kaguya.prefab" {
		t.Errorf("base body: %v", got)
	}
	if got := pickCoverPrefabs("x", "衣服", []prefabInfo{{Path: "Assets/a.prefab", Name: "a"}}); len(got) != 0 {
		t.Errorf("a prefab without meshes: %v", got)
	}
}

// a project with two library assets in it, and a Unity that draws prefabs
func coverProject(t *testing.T) (st *core.Store, proj string, calls *[]string, fail *string) {
	t.Helper()
	st = testkit.NewStore(t)
	proj = t.TempDir()
	for _, f := range []string{"Assets/Shop/Sailor/Sailor_Black.prefab", "Assets/Shop/Sailor/Sailor_White.prefab", "Assets/Shop/Bat/Bat.prefab", "ProjectSettings/ProjectVersion.txt"} {
		p := filepath.Join(proj, filepath.FromSlash(f))
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("x"), 0644)
	}
	st.Projects = []core.ProjectInfo{{Name: filepath.Base(proj), Path: proj}}
	st.Assets = []*core.Asset{
		{Key: "name:sailor", Name: "Sailor", Category: "衣服", Usage: []core.Usage{{Project: filepath.Base(proj), Status: "used", Folder: "Assets/Shop/Sailor"}}},
		{Key: "name:bat", Name: "Bat", Category: "道具", Covers: []string{filepath.Join(core.DataDir, "bat.png")}, Usage: []core.Usage{{Project: filepath.Base(proj), Status: "used", Folder: "Assets/Shop/Bat"}}},
		{Key: "name:hat", Name: "Hat", Category: "配饰"},
	}
	var mu sync.Mutex
	calls, fail = &[]string{}, new(string)
	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 64, 64)))
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		mu.Lock()
		defer mu.Unlock()
		switch cmd {
		case "prefabs":
			var out []map[string]any
			for _, f := range args["folders"].([]any) {
				switch f {
				case "Assets/Shop/Sailor":
					out = append(out, map[string]any{"path": "Assets/Shop/Sailor/Sailor_White.prefab", "name": "Sailor_White", "renderers": 9},
						map[string]any{"path": "Assets/Shop/Sailor/Sailor_Black.prefab", "name": "Sailor_Black", "renderers": 9})
				case "Assets/Shop/Bat":
					out = append(out, map[string]any{"path": "Assets/Shop/Bat/Bat.prefab", "name": "Bat", "renderers": 1})
				}
			}
			return map[string]any{"prefabs": out}, ""
		case "prefab_shot":
			if *fail == "old" {
				return nil, "不认识的操作：prefab_shot"
			}
			prefab := args["prefabs"].([]any)[0].(string)
			*calls = append(*calls, prefab)
			if *fail != "" && strings.Contains(prefab, *fail) {
				return map[string]any{"shots": []any{map[string]any{"prefab": prefab, "error": "该 prefab 的材质缺少着色器（显示为粉色），请先安装对应的着色器"}}}, ""
			}
			file := filepath.Join(proj, "Temp", "MioVRCA", "prefabshots", filepath.Base(prefab)+".png")
			_ = os.MkdirAll(filepath.Dir(file), 0755)
			_ = os.WriteFile(file, img.Bytes(), 0644)
			return map[string]any{"shots": []any{map[string]any{"prefab": prefab, "file": filepath.ToSlash(file), "width": 64, "height": 64}}}, ""
		}
		return nil, "不认识的操作：" + cmd
	})
	return
}

func TestGenerateCover(t *testing.T) {
	st, proj, calls, fail := coverProject(t)
	ctx := context.Background()

	if s := CoverStatusOf(st, "name:sailor"); !s.Can || s.Real || s.Generated || s.Project != filepath.Base(proj) {
		t.Errorf("status before: %+v", s)
	}
	if s := CoverStatusOf(st, "name:bat"); !s.Real {
		t.Errorf("an asset with a picture of its own: %+v", s)
	}
	if s := CoverStatusOf(st, "name:hat"); s.Can || !strings.Contains(s.Why, "尚未导入任何工程") {
		t.Errorf("an asset in no project: %+v", s)
	}
	if s := CoverStatusOf(st, "name:nope"); s.Can || s.Why == "" {
		t.Errorf("no such asset: %+v", s)
	}
	if _, err := GenerateCover(ctx, st, "name:hat", ""); err == nil || !strings.Contains(err.Error(), "尚未导入任何工程") {
		t.Errorf("an asset in no project: %v", err)
	}

	// the first colour cannot be drawn: the next one is
	*fail = "Black"
	prefab, err := GenerateCover(ctx, st, "name:sailor", "")
	if err != nil || prefab != "Assets/Shop/Sailor/Sailor_White.prefab" {
		t.Fatalf("generate: %q %v", prefab, err)
	}
	if want := []string{"Assets/Shop/Sailor/Sailor_Black.prefab", "Assets/Shop/Sailor/Sailor_White.prefab"}; !reflect.DeepEqual(*calls, want) {
		t.Errorf("prefabs tried: %v", *calls)
	}
	cover := library.GeneratedCover("name:sailor")
	if cover == "" || !core.StatOK(cover) {
		t.Fatalf("no cover kept: %q", cover)
	}
	if left, _ := filepath.Glob(filepath.Join(proj, "Temp", "MioVRCA", "prefabshots", "*.png")); len(left) != 0 {
		t.Errorf("pictures left in the project: %v", left)
	}
	st.Mu.RLock()
	v := library.BuildView(st, st.Assets[0])
	st.Mu.RUnlock()
	if !strings.Contains(v.Cover, "generated") {
		t.Errorf("the card's cover: %q", v.Cover)
	}
	if s := CoverStatusOf(st, "name:sailor"); !s.Generated || s.Real || s.Prefab != prefab || !s.Can {
		t.Errorf("status after: %+v", s)
	}

	// none of its prefabs can be drawn: the reason is the plugin's
	*fail = "Sailor"
	if _, err := GenerateCover(ctx, st, "name:sailor", ""); err == nil || !strings.Contains(err.Error(), "缺少着色器") || !strings.Contains(err.Error(), "「Sailor_") {
		t.Errorf("nothing to draw: %v", err)
	}
	if library.GeneratedCover("name:sailor") != cover {
		t.Error("a failed attempt took the cover away")
	}
	// a plugin from before 1.7.6
	*fail = "old"
	if _, err := GenerateCover(ctx, st, "name:sailor", ""); err == nil || !strings.Contains(err.Error(), "点击「更新」") {
		t.Errorf("old plugin: %v", err)
	}
	// only in the project that was named
	*fail = ""
	if _, err := GenerateCover(ctx, st, "name:sailor", filepath.Join(proj, "other")); err == nil || !strings.Contains(err.Error(), "打开导入了该素材的工程") {
		t.Errorf("another project: %v", err)
	}
}

func TestCoverBatch(t *testing.T) {
	st, proj, calls, _ := coverProject(t)
	// a second cover-less asset, tied to its folder by an import of this program
	st.Assets = append(st.Assets, &core.Asset{Key: "name:hat2", Name: "Hat", Category: "配饰"})
	p := filepath.Join(proj, "Assets", "Shop", "Hat", "Hat.prefab")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	_ = os.WriteFile(p, []byte("x"), 0644)
	aiRemember(proj, "name:hat2", []string{"Assets/Shop/Hat"})

	keys, names := coverless(st, proj)
	if !reflect.DeepEqual(keys, []string{"name:hat2", "name:sailor"}) && !reflect.DeepEqual(keys, []string{"name:sailor", "name:hat2"}) {
		t.Fatalf("cover-less assets: %v %v", keys, names)
	}
	if err := StartCoverBatch(st, proj); err != nil {
		t.Fatal(err)
	}
	if err := StartCoverBatch(st, proj); err == nil {
		t.Error("a second batch started beside the first")
	}
	var b CoverBatch
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if b = CoverBatchSnapshot(); !b.Running {
			break
		}
	}
	// the hat's folder holds no prefab Unity knows: it fails, the batch goes on
	if b.Running || b.Total != 2 || b.Done != 2 || b.Made != 1 || b.Failed != 1 || b.Err != "" || !strings.Contains(b.Last, "「Hat」") {
		t.Errorf("batch %+v", b)
	}
	if library.GeneratedCover("name:sailor") == "" || library.GeneratedCover("name:bat") != "" || len(*calls) != 1 {
		t.Errorf("covers made: sailor %q bat %q, calls %v", library.GeneratedCover("name:sailor"), library.GeneratedCover("name:bat"), *calls)
	}
	// nothing left to do; and a batch that is cancelled stops
	if err := StartCoverBatch(st, filepath.Join(proj, "nope")); err == nil {
		t.Error("a batch in a project that is not open")
	}
	library.RemoveGeneratedCover("name:sailor")
	if err := StartCoverBatch(st, proj); err != nil {
		t.Fatal(err)
	}
	CancelCoverBatch()
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if b = CoverBatchSnapshot(); !b.Running {
			break
		}
	}
	if b.Running || b.Err != "已取消" || b.Done == b.Total && b.Made == 2 {
		t.Errorf("cancelled batch %+v", b)
	}
}

// the AI's check-up tool: the answer in words, the count for the player, and no scene change counted
func TestCheckupTool(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		if cmd != "checkup" {
			return nil, "不认识的操作：" + cmd
		}
		return map[string]any{"avatar": "Kaguya", "prebuild": true, "sdkCalc": true, "ranks": map[string]any{"pc": "Poor"}, "items": []any{
			map[string]any{"id": "missing", "group": "limit", "label": "丢失的脚本", "level": "fail", "value": 2, "advice": "请安装对应的插件。",
				"details": []any{map[string]any{"t": "Dress/Tail", "v": 2, "u": "个"}}},
			map[string]any{"id": "fig.bones", "group": "figure", "label": "骨骼", "level": "ok", "value": 320, "rating": "Poor"}}}, ""
	})
	_ = os.WriteFile(filepath.Join(proj, "Packages", "com.miovrc.pipeline", "package.json"), []byte(`{}`), 0644)
	has := false
	for _, tl := range aiTools {
		has = has || tl.Name == "checkup"
	}
	if !has || !strings.Contains(aiPromptText, "checkup") || toolTitle("checkup", nil) != "上传前体检" {
		t.Error("the tool is not offered to the AI")
	}
	s := aiSessionFor(proj)
	content, short, ok := s.runTool(context.Background(), "checkup", map[string]any{})
	if !ok || short != "1 项需处理，0 项建议优化" {
		t.Fatalf("checkup: %v %q", ok, short)
	}
	for _, want := range []string{"丢失的脚本：2", "Dress/Tail：2 个", "建议：请安装对应的插件。", "PC 较差（Poor）", "骨骼 320（Poor）"} {
		if !strings.Contains(content, want) {
			t.Errorf("the AI's text lacks %q:\n%s", want, content)
		}
	}
	if _, _, n := s.snapshot(); n != 0 {
		t.Errorf("a check-up counted as %d scene changes", n)
	}
}
