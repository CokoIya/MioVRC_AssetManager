package ai

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

func TestProjectAssets(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(proj, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("x"), 0644)
	}
	for _, f := range []string{"Assets/AddRecollection/38KoakumaSailor/Kaguya/Kaguya prefab/Envy cat.prefab", "Assets/AddRecollection/38KoakumaSailor/Kaguya/Kaguya prefab/Melty Devil .prefab",
		"Assets/IKUSIA/kaguya/kaguya.prefab", "Assets/KDress/Kaguya/KDress.prefab", "Assets/KDress/Plum/KDress.prefab", "Assets/头发_樱发/hair.prefab",
		"Assets/VRCSDK/x.prefab", "Assets/_Backup/y.prefab", "Assets/Editor/z.prefab", "Assets/Guns/Sniper/gun.prefab", "ProjectSettings/ProjectVersion.txt"} {
		mk(f)
	}
	st.Assets = []*core.Asset{
		{Key: "a1", Name: "嫉妒猫水手服", Category: "衣服", Usage: []core.Usage{{Project: filepath.Base(proj), Status: "used", Folder: "Assets/AddRecollection/38KoakumaSailor"}}},
		{Key: "a2", Name: "辉夜 Kaguya", Category: "素体", Usage: []core.Usage{{Project: filepath.Base(proj), Status: "used", Folder: "Assets/IKUSIA/kaguya"}}},
		{Key: "a3", Name: "Kaguya Dress 辉夜连衣裙", Category: "衣服"},
		{Key: "a4", Name: "lilToon", Category: "插件", Usage: []core.Usage{{Project: filepath.Base(proj), Status: "used", Folder: "Assets/lilToon"}}},
	}
	aiRemember(proj, "a3", []string{"Assets/KDress"})
	list := projectAssets(st, proj)
	got := map[string]ProjAsset{}
	for _, a := range list {
		got[a.Folder] = a
	}
	if len(list) != 5 {
		t.Errorf("%d assets: %+v", len(list), list)
	}
	if a := got["Assets/AddRecollection/38KoakumaSailor"]; a.Key != "a1" || a.Kind != "衣服" || a.Prefabs != 2 || a.Source != "匹配" {
		t.Errorf("sailor %+v", a)
	}
	if a := got["Assets/IKUSIA/kaguya"]; a.Key != "a2" || a.Kind != "素体" {
		t.Errorf("kaguya %+v", a)
	}
	if a := got["Assets/KDress"]; a.Key != "a3" || a.Kind != "衣服" || a.Prefabs != 2 || a.Source != "导入" || a.Name != "Kaguya Dress 辉夜连衣裙" {
		t.Errorf("kdress %+v", a)
	}
	if a := got["Assets/头发_樱发"]; a.Key != "" || a.Kind != "头发" {
		t.Errorf("hair by name %+v", a)
	}
	if a := got["Assets/头发_樱发"]; a.Name != "头发_樱发" {
		t.Errorf("hair row named %q", a.Name)
	}
	if a := got["Assets/Guns/Sniper"]; a.Kind != "道具" && a.Kind != "其他" {
		t.Errorf("gun %+v", a)
	}
	if list[0].Kind != "素体" {
		t.Errorf("order %v", list)
	}
	for _, bad := range []string{"Assets/VRCSDK", "Assets/_Backup", "Assets/Editor", "Assets/lilToon"} {
		if _, ok := got[bad]; ok {
			t.Errorf("%s listed", bad)
		}
	}
	// Unity adds: whole avatar, what is worn
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch cmd {
		case "prefabs":
			return map[string]any{"prefabs": []any{
				map[string]any{"path": "Assets/IKUSIA/kaguya/kaguya.prefab", "wholeAvatar": true, "renderers": 5},
				map[string]any{"path": "Assets/AddRecollection/38KoakumaSailor/Kaguya/Kaguya prefab/Envy cat.prefab", "modularAvatarReady": true, "renderers": 9},
				map[string]any{"path": "Assets/Guns/Sniper/gun.prefab", "renderers": 1}}}, ""
		case "inspect":
			return map[string]any{"avatars": []any{map[string]any{"name": "K", "prefab": "Assets/IKUSIA/kaguya/kaguya.prefab",
				"children": []any{map[string]any{"name": "Sailor", "kind": "outfit", "prefab": "Assets/AddRecollection/38KoakumaSailor/Kaguya/Kaguya prefab/Envy cat.prefab"}}}}}, ""
		}
		return nil, "?"
	})
	list = projectAssetsLive(st, proj, false)
	for _, a := range list {
		got[a.Folder] = a
	}
	if a := got["Assets/IKUSIA/kaguya"]; !a.Avatar || !a.Worn || a.Prefabs != 1 {
		t.Errorf("live kaguya %+v", a)
	}
	if a := got["Assets/AddRecollection/38KoakumaSailor"]; !a.MAReady || !a.Worn || a.Prefabs != 1 {
		t.Errorf("live sailor %+v", a)
	}
	if a := got["Assets/KDress"]; a.Worn || a.Prefabs != 2 {
		t.Errorf("live kdress %+v", a)
	}
}

// The pipeline page asks for the AI settings and for the project's assets at once, while the library is being
// written: aiView may not hold aiMu while it waits for st.Mu, nor projectAssets st.Mu while it waits for aiMu
// (behind the writer, the three waited on each other for good).
func TestProjectAssetsBesideAIView(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	for _, f := range []string{"Assets/Shop/Item/a.prefab", "ProjectSettings/ProjectVersion.txt"} {
		testkit.WriteFile(t, filepath.Join(proj, filepath.FromSlash(f)), 1)
	}
	aiRemember(proj, "a1", []string{"Assets/Shop/Item"})
	// aiView is held where it asks for the interface's language until the other two are under way
	orig := menuLang
	defer func() { menuLang = orig }()
	gate, inView := make(chan struct{}), make(chan struct{}, 1)
	menuLang = func() string {
		select {
		case inView <- struct{}{}:
		default:
		}
		<-gate
		return storeLang(st)
	}
	view, list, write := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { aiView(); close(view) }() // /api/ai/config
	<-inView
	go func() { projectAssets(st, proj); close(list) }() // /api/pipe/assets
	time.Sleep(100 * time.Millisecond)
	go func() { st.Mu.Lock(); st.Mu.Unlock(); close(write) }() // a scan, an edit
	time.Sleep(100 * time.Millisecond)
	close(gate)
	for _, done := range []chan struct{}{view, list, write} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("aiView, projectAssets and a writer of the library wait on each other")
		}
	}
}
