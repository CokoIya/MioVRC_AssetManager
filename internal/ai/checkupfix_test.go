package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/testkit"
	"vrclib/internal/unity"
	"vrclib/internal/unity/unitytest"
)

// the AI's one-click fix: the player is asked first (and told what the fix reaches), a scene fix counts as one
// step for 「撤销上一步」, a texture fix as none
func TestCheckupFixTool(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	var kinds []string
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch cmd {
		case "checkup":
			return map[string]any{"avatar": "Kaguya", "prebuild": true, "sdkCalc": true, "ranks": map[string]any{"pc": "Good"}, "items": []any{
				map[string]any{"id": "textures", "group": "look", "label": "4096 像素以上的贴图", "level": "warn", "value": 5, "fixable": 3},
				map[string]any{"id": "fig.lights", "group": "figure", "label": "灯光", "level": "ok", "value": 0, "rating": "Excellent"}}}, ""
		case "fix":
			kinds = append(kinds, args["kind"].(string))
			if args["kind"] == "lights" {
				return map[string]any{"kind": "lights", "changed": 2, "items": []any{map[string]any{"t": "Head/Lamp", "n": "已移除"}, map[string]any{"t": "Hand/Torch", "n": "已移除"}}, "undo": "MioVRCA 移除灯光"}, ""
			}
			if args["kind"] == "missing" {
				return map[string]any{"kind": "missing", "changed": 1, "items": []any{map[string]any{"t": "Hips/Tail", "v": 1, "u": "个"}}, "undo": "MioVRCA 移除丢失的脚本"}, ""
			}
			if args["all"] == true {
				return map[string]any{"kind": "textures", "max": 2048, "changed": 0, "items": []any{}}, ""
			}
			if paths, _ := args["paths"].([]any); len(paths) != 1 || paths[0] != "Assets/Tex/Body.png" {
				t.Errorf("paths %v", args)
			}
			return map[string]any{"kind": "textures", "max": 2048, "changed": 1, "record": "UserSettings/MioVRCA/fixes/a.json", "items": []any{map[string]any{"path": "Assets/Tex/Body.png", "from": 4096, "to": 2048}}}, ""
		}
		return nil, "不认识的操作：" + cmd
	})
	_ = os.WriteFile(filepath.Join(proj, "Packages", "com.miovrc.pipeline", "package.json"), []byte(`{}`), 0644)
	has := false
	for _, tl := range aiTools {
		has = has || tl.Name == "checkup_fix"
	}
	if !has || toolTitle("checkup_fix", map[string]any{"kind": "lights"}) != "一键处理：移除灯光" {
		t.Error("the tool is not offered to the AI")
	}
	s := aiSessionFor(proj)
	// the call waits for the player's answer; what it asks names the fix and how it is taken back
	question := ""
	run := func(a map[string]any, allow bool) (string, string, bool) {
		type res struct {
			content, short string
			ok             bool
		}
		ch := make(chan res, 1)
		go func() { c, sh, ok := s.runTool(context.Background(), "checkup_fix", a); ch <- res{c, sh, ok} }()
		for i := 0; i < 300; i++ {
			_, steps, _ := s.snapshot()
			if n := len(steps); n > 0 && steps[n-1].Kind == "ask" && steps[n-1].Busy {
				if question = steps[n-1].Text; !strings.Contains(question, "一键处理「") || !strings.Contains(question, "Ctrl+Z") && !strings.Contains(question, "点击「恢复」") {
					t.Errorf("question %q", question)
				}
				if !s.answer(allow, false) {
					t.Error("nobody was waiting for the answer")
				}
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		select {
		case r := <-ch:
			return r.content, r.short, r.ok
		case <-time.After(20 * time.Second):
			t.Fatal("the tool did not return")
		}
		return "", "", false
	}
	if content, short, ok := run(map[string]any{"kind": "lights"}, false); ok || short != "未获允许，未执行" || !strings.Contains(content, "没有同意") || len(kinds) != 0 {
		t.Errorf("refused: %v %q %q %v", ok, short, content, kinds)
	}
	content, short, ok := run(map[string]any{"kind": "lights"}, true)
	if !ok || short != "已移除 2 个灯光组件。" || len(kinds) != 1 || !strings.Contains(content, "Hand/Torch") || !strings.Contains(content, "撤销上一步") || !strings.Contains(content, "重新体检：") {
		t.Errorf("lights: %v %q %q", ok, short, content)
	}
	if _, _, n := s.snapshot(); n != 1 {
		t.Errorf("%d changes counted", n)
	}
	content, short, ok = run(map[string]any{"kind": "textures", "paths": []any{"Assets/Tex/Body.png"}}, true)
	if !ok || !strings.HasPrefix(short, "已将 1 张贴图的 Max Size 降到 2048") || !strings.Contains(content, "恢复记录：UserSettings/MioVRCA/fixes/a.json") || strings.Contains(content, "fix_revert") {
		t.Errorf("textures: %v %q %q", ok, short, content)
	}
	if _, _, n := s.snapshot(); n != 1 {
		t.Errorf("a texture fix counted as an undo step: %d", n)
	}
	// the texture fix's question: whose textures and how many, that the setting is the project's, and the way back
	for _, want := range []string{"将把模型「Kaguya」的 1 张指定贴图的 Max Size 改为 2048 并重新导入。", "对整个工程生效", "其他模型和场景也会随之改变", "处理后可在体检面板中点击「恢复」改回原设置"} {
		if !strings.Contains(question, want) {
			t.Errorf("the question lacks %q: %q", want, question)
		}
	}
	// … for all of them: as many as the check-up says the fix can lower (3 of the 5)
	if _, _, ok = run(map[string]any{"kind": "textures"}, true); !ok || !strings.Contains(question, "将把模型「Kaguya」用到的 3 张 4096 像素贴图的 Max Size 改为 2048 并重新导入。") {
		t.Errorf("textures, all: %v %q", ok, question)
	}
	// missing scripts: the question says that a plugin that is not installed looks the same, what is lost, and
	// it is asked every time, 「本次对话始终允许」 or not
	s.mu.Lock()
	s.allowAll = true
	s.mu.Unlock()
	question = ""
	if _, _, ok = run(map[string]any{"kind": "missing"}, false); ok || kinds[len(kinds)-1] == "missing" {
		t.Errorf("missing scripts were removed without the player's yes: %v", kinds)
	}
	for _, want := range []string{"Modular Avatar、VRCFury", "没有安装或编译出错", "应先安装或修复该插件", "在场景保存后无法找回", "保存场景前可用 Ctrl+Z 撤销"} {
		if !strings.Contains(question, want) {
			t.Errorf("the question lacks %q: %q", want, question)
		}
	}
	question = ""
	if _, _, ok = run(map[string]any{"kind": "lights"}, true); !ok || question != "" {
		t.Errorf("lights with 「本次对话始终允许」: %v, asked %q", ok, question)
	}
	s.mu.Lock()
	s.allowAll = false
	s.mu.Unlock()
	if _, short, ok := s.runTool(context.Background(), "checkup_fix", map[string]any{"kind": "bounds"}); ok || !strings.Contains(short, "没有这项一键处理") {
		t.Errorf("bounds: %v %q", ok, short)
	}
}

// 「撤销上一步」 follows what the plugin says it took back. A one-click fix (the panel's or the AI's) is a step
// it can take back, and taking it back leaves what the line put on the avatar counted and noted for
// 「保存为方案」; a fix nothing counted (made before a restart) is taken back without the session forgetting a
// change of its own.
func TestUndoFollowsWhatWasUndone(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	var history []string // Unity's undo history
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch cmd {
		case "dress":
			history = append(history, "MioVRCA 装配 Dress")
			return map[string]any{"setUp": true, "changed": true, "outfit": map[string]any{"object": "Dress", "meshes": []any{}}}, ""
		case "checkup":
			return map[string]any{"avatar": "Kaguya", "prebuild": true, "items": []any{}}, ""
		case "fix":
			name := map[string]string{"lights": "MioVRCA 移除灯光", "missing": "MioVRCA 移除丢失的脚本"}[args["kind"].(string)]
			if args["remove"] == false {
				name = "MioVRCA 关闭灯光"
			}
			history = append(history, name)
			return map[string]any{"kind": args["kind"], "changed": 1, "items": []any{}, "undo": name}, ""
		case "undo":
			if len(history) == 0 {
				return nil, "Unity 的撤销记录中没有可撤销的 MioVRCA 操作，未执行撤销"
			}
			top := history[len(history)-1]
			history = history[:len(history)-1]
			return map[string]any{"undone": top}, ""
		}
		return nil, "不认识的操作：" + cmd
	})
	_ = os.WriteFile(filepath.Join(proj, "Packages", "com.miovrc.pipeline", "package.json"), []byte(`{}`), 0644)
	ctx := context.Background()
	s := aiSessionFor(proj)
	state := func() (changes, noted int) {
		_, _, changes = s.snapshot()
		s.mu.Lock()
		defer s.mu.Unlock()
		return changes, len(s.rec.steps)
	}
	dress := func() {
		t.Helper()
		if _, short, ok := s.runTool(ctx, "dress", map[string]any{"prefab": "Assets/Shop/Dress.prefab"}); !ok {
			t.Fatalf("dress: %s", short)
		}
	}
	// the panel's button, as /api/checkup/fix carries it out; counted: the server tells the session
	panel := func(kind string, args map[string]any, counted bool) {
		t.Helper()
		fix, _, _, err := unity.RunFix(ctx, proj, kind, args)
		if err != nil {
			t.Fatal(err)
		}
		if counted {
			NoteFix(proj, fix)
		}
	}
	undo := func(want string, changes, noted int) {
		t.Helper()
		what, err := s.undo(ctx, 0)
		if err != nil || what != want {
			t.Fatalf("undo took back %q (%v), want %q", what, err, want)
		}
		if c, n := state(); c != changes || n != noted {
			t.Fatalf("after taking back %q the session counts %d step(s) and notes %d for the recipe, want %d and %d", what, c, n, changes, noted)
		}
	}
	// a panel fix alone: 「撤销上一步」 is there for it
	panel("lights", map[string]any{"all": true}, true)
	if c, _ := state(); c != 1 {
		t.Fatalf("a panel fix is not a step 「撤销上一步」 is offered for: %d", c)
	}
	undo("MioVRCA 移除灯光", 0, 0)
	// the line dresses an outfit, then the player removes the lights from the panel: the fix goes first, the
	// dress stays counted and noted
	dress()
	panel("lights", map[string]any{"all": true}, true)
	if c, n := state(); c != 2 || n != 1 {
		t.Fatalf("a dress and a fix: %d %d", c, n)
	}
	undo("MioVRCA 移除灯光", 1, 1)
	undo("MioVRCA 装配 Dress", 0, 0)
	// the other way round: the dress goes first, the fix stays for the next click
	panel("missing", nil, true)
	dress()
	undo("MioVRCA 装配 Dress", 1, 0)
	undo("MioVRCA 移除丢失的脚本", 0, 0)
	// a fix nothing counted (the program was restarted since): taking it back does not cost the dress
	dress()
	panel("lights", map[string]any{"all": true, "remove": false}, false)
	undo("MioVRCA 关闭灯光", 1, 1)
	undo("MioVRCA 装配 Dress", 0, 0)
	// a texture fix is no undo step
	NoteFix(proj, &unity.FixResult{Kind: "textures", Changed: 2, Record: "UserSettings/MioVRCA/fixes/a.json"})
	NoteFix(proj, nil)
	if c, _ := state(); c != 0 {
		t.Fatalf("a texture fix is counted as an undo step: %d", c)
	}
	for name, want := range map[string]bool{"MioVRCA 移除灯光": true, "MioVRCA 关闭灯光": true, "MioVRCA 移除丢失的脚本": true, "MioVRCA 装配 移除灯光": false, "MioVRCA 生成菜单": false, "Skill: gameobject_set_active": false, "": false} {
		if unity.IsFixUndo(name) != want {
			t.Errorf("IsFixUndo(%q) = %v", name, !want)
		}
	}
}
