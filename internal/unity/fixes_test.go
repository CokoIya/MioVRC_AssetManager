package unity_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
	"vrclib/internal/unity/unitytest"
)

// a fix is carried out on the avatar the check-up measured, and the check-up runs again after it
func TestRunFix(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	if _, _, _, err := unity.RunFix(context.Background(), proj, "textures", nil); err == nil || !strings.Contains(err.Error(), "尚未安装 Unity 插件") {
		t.Errorf("no plugin: %v", err)
	}
	if _, _, _, err := unity.RunFix(context.Background(), proj, "materials", nil); err == nil || !strings.Contains(err.Error(), "没有这项一键处理") {
		t.Errorf("a kind that is advice only: %v", err)
	}
	big, old, checks, lost := 2, false, 0, []any{"Assets/Tex/Hair.png"}
	var asked []map[string]any
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch {
		case old:
			return nil, "不认识的操作：" + cmd
		case cmd == "checkup":
			checks++
			return map[string]any{"avatar": "Kaguya", "path": "Kaguya", "prebuild": true, "sdkCalc": true, "items": []any{
				map[string]any{"id": "textures", "group": "look", "label": "4096 像素以上的贴图", "level": map[bool]string{true: "warn", false: "ok"}[big > 0], "value": big,
					"details": []any{map[string]any{"t": "Body", "n": "4096 × 4096　DXT5　21.3 MB", "w": big > 0, "p": "Assets/Tex/Body.png"}}}}}, ""
		case cmd == "fix":
			asked = append(asked, args)
			switch args["kind"] {
			case "textures":
				big = 0
				return map[string]any{"kind": "textures", "max": 2048, "changed": 2, "record": "UserSettings/MioVRCA/fixes/20261004_120000_000.json", "memoryBefore": 42.7, "memoryAfter": 10.7,
					"items":   []any{map[string]any{"path": "Assets/Tex/Body.png", "name": "Body", "from": 4096, "to": 2048, "platforms": []any{"Standalone"}}, map[string]any{"path": "Assets/Tex/Hair.png", "name": "Hair", "from": 8192, "to": 2048}},
					"skipped": []any{map[string]any{"t": "Assets/Tex/Face.png", "n": "Max Size 已不高于 2048"}}}, ""
			case "lights":
				return map[string]any{"kind": "lights", "changed": 1, "items": []any{map[string]any{"t": "Head/Lamp", "n": "已移除"}}, "undo": "MioVRCA 移除灯光"}, ""
			}
			return nil, "没有「" + args["kind"].(string) + "」这项一键处理"
		case cmd == "fix_revert":
			asked = append(asked, args)
			big = 2
			return map[string]any{"kind": "textures", "reverted": 2 - len(lost), "items": []any{map[string]any{"path": "Assets/Tex/Body.png", "to": 4096}}, "missing": lost}, ""
		}
		return nil, "不认识的操作：" + cmd
	})
	_ = os.WriteFile(filepath.Join(proj, "Packages", unity.PipePkg, "package.json"), []byte(`{}`), 0644)

	fix, rec, recheck, err := unity.RunFix(context.Background(), proj, "textures", map[string]any{"all": true, "avatar": "Kaguya"})
	if err != nil || recheck != nil {
		t.Fatalf("textures: %v / %v", err, recheck)
	}
	if a := asked[0]; a["kind"] != "textures" || a["all"] != true || a["avatar"] != "Kaguya" {
		t.Errorf("asked %v", a)
	}
	if fix.Changed != 2 || fix.Max != 2048 || fix.Record == "" || len(fix.Items) != 2 || fix.Items[0].From != 4096 || len(fix.Items[0].Platforms) != 1 || len(fix.Skipped) != 1 || fix.At == 0 {
		t.Errorf("fix %+v", fix)
	}
	// the check-up ran again and is the project's last one
	if checks != 1 || rec == nil || rec.Result.Items[0].Level != "ok" || unity.LastCheckup(proj) == nil || unity.LastCheckup(proj).At != rec.At {
		t.Errorf("recheck: %d %+v", checks, rec)
	}
	// the fix stays with the check-up, so that 「恢复」 is still there after a restart, and through later check-ups
	if rec.TexFix == nil || rec.TexFix.Record != fix.Record || unity.LastCheckup(proj).TexFix == nil {
		t.Errorf("the fix is not kept with the check-up: %+v", rec.TexFix)
	}
	if again, err := unity.RunCheckup(context.Background(), proj, "Kaguya"); err != nil || again.TexFix == nil || again.TexFix.Record != fix.Record {
		t.Errorf("a later check-up dropped the fix: %v %+v", err, again)
	}
	checks--
	text := fix.Text()
	// the figure is of the textures that were changed, not the avatar's whole; the way back is the panel's 「恢复」
	for _, want := range []string{"已将 2 张贴图的 Max Size 降到 2048，所处理贴图的显存估计由 42.7 MB 降到 10.7 MB", "Assets/Tex/Body.png：4096 → 2048", "未处理 Assets/Tex/Face.png：Max Size 已不高于 2048",
		"玩家可在体检面板中点击「恢复」改回原设置", "恢复记录：UserSettings/MioVRCA/fixes/20261004_120000_000.json"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "fix_revert") {
		t.Errorf("the AI is told of a tool it does not have:\n%s", text)
	}
	// the way back. A texture that is no longer at its path: the plugin keeps the record, and so 「恢复」 stays
	rev, rec, recheck, err := unity.RevertFix(context.Background(), proj, fix.Record, "Kaguya")
	if err != nil || recheck != nil {
		t.Fatalf("revert: %v / %v", err, recheck)
	}
	if asked[1]["record"] != fix.Record || rev.Reverted != 1 || len(rev.Missing) != 1 || checks != 2 || rec.Result.Items[0].Level != "warn" {
		t.Errorf("revert %+v (asked %v, %d checks)", rev, asked[1], checks)
	}
	if rec.TexFix == nil || rec.TexFix.Record != fix.Record {
		t.Errorf("a texture was not found, the record is kept, but 「恢复」 is no longer offered: %+v", rec.TexFix)
	}
	// the texture is back at its path: everything is put back, and the fix is no longer offered
	lost = []any{}
	rev, rec, _, err = unity.RevertFix(context.Background(), proj, fix.Record, "Kaguya")
	if err != nil || rev.Reverted != 2 || len(rev.Missing) != 0 {
		t.Fatalf("revert again: %v %+v", err, rev)
	}
	if rec.TexFix != nil || unity.LastCheckup(proj).TexFix != nil {
		t.Errorf("the fix is still offered after it was taken back: %+v", rec.TexFix)
	}
	// a scene fix: the undo step's name comes along
	fix, _, _, err = unity.RunFix(context.Background(), proj, "lights", map[string]any{"all": true})
	if err != nil || fix.Changed != 1 || fix.Undo != "MioVRCA 移除灯光" || !strings.Contains(fix.Text(), "已移除 1 个灯光组件") || !strings.Contains(fix.Text(), "Head/Lamp") || !strings.Contains(fix.Text(), "撤销上一步") {
		t.Errorf("lights: %v %+v", err, fix)
	}
	if _, _, _, err = unity.RunFix(context.Background(), proj, "missing", map[string]any{}); err == nil || !strings.Contains(err.Error(), "没有「missing」这项一键处理") {
		t.Errorf("the plugin's own refusal is passed on: %v", err)
	}
	// a plugin from before the fixes
	old = true
	if _, _, _, err = unity.RunFix(context.Background(), proj, "lights", nil); err == nil || !strings.Contains(err.Error(), "不支持一键处理") {
		t.Errorf("old plugin: %v", err)
	}
	if _, _, _, err = unity.RevertFix(context.Background(), proj, "x", ""); err == nil || !strings.Contains(err.Error(), "不支持一键处理") {
		t.Errorf("old plugin, revert: %v", err)
	}
}

// What 「恢复」 needs is in the project (the plugin writes the record before it reimports), so a texture fix
// whose answer never arrives is still offered for taking back: the run was stopped while Unity reimported, or
// the record only appeared after the program gave up waiting.
func TestTexFixAdoptedFromTheProject(t *testing.T) {
	for _, late := range []bool{false, true} {
		testkit.NewStore(t)
		proj := t.TempDir()
		big := 2
		rec := "UserSettings/MioVRCA/fixes/20261004_120000_000.json"
		reached := make(chan struct{}) // the fix has got as far as the player's 「停止」 finds it
		write := func(name, body string) {
			_ = os.MkdirAll(filepath.Join(proj, "UserSettings", "MioVRCA", "fixes"), 0755)
			if err := os.WriteFile(filepath.Join(proj, "UserSettings", "MioVRCA", "fixes", name), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
		}
		unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
			switch cmd {
			case "checkup":
				return map[string]any{"avatar": "Kaguya", "path": "Kaguya", "prebuild": true, "items": []any{
					map[string]any{"id": "textures", "group": "look", "label": "4096 像素以上的贴图", "level": "warn", "value": big, "fixable": big}}}, ""
			case "fix":
				// as Fixes.cs: the record first, then the reimport (slow), then the answer
				if late { // stopped while Unity was still busy with something else: the record comes afterwards
					close(reached)
					time.Sleep(400 * time.Millisecond)
				}
				write(filepath.Base(rec), `{"kind":"textures","at":1791000000,"max":2048,"avatar":"Kaguya","textures":[{"path":"Assets/a.png","max":4096,"platforms":{"Standalone":{"overridden":false,"max":2048}}},{"path":"Assets/b.png","max":2048,"platforms":{"Android":{"overridden":true,"max":4096}}}]}`)
				if !late {
					close(reached)
				}
				time.Sleep(400 * time.Millisecond)
				big = 0
				return map[string]any{"kind": "textures", "max": 2048, "changed": 2, "record": rec, "items": []any{}}, ""
			}
			return nil, "不认识的操作：" + cmd
		})
		_ = os.WriteFile(filepath.Join(proj, "Packages", unity.PipePkg, "package.json"), []byte(`{}`), 0644)
		// records that are not a texture fix's, and an older one, lie in the folder too
		write("20261003_090000_000.json", `{"kind":"textures","at":1790900000,"max":2048,"textures":[{"path":"Assets/old.png","max":4096}]}`)
		write("20261005_000000_000.json", `{"kind":"other"}`)
		write("20261006_000000_000.json", `not json`)
		if _, err := unity.RunCheckup(context.Background(), proj, ""); err != nil {
			t.Fatal(err)
		}
		// (the older record nothing knew of is offered: nothing is left without a way back)
		if tf := unity.LastCheckup(proj).TexFix; tf == nil || !strings.HasSuffix(tf.Record, "/20261003_090000_000.json") || !tf.Adopted {
			t.Fatalf("late=%v: the record in the project is not offered: %+v", late, tf)
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { <-reached; cancel() }() // 「停止」 while Unity is at it
		_, _, _, err := unity.RunFix(ctx, proj, "textures", map[string]any{"all": true})
		cancel()
		if err == nil {
			t.Fatal("the fix was not abandoned")
		}
		if tf := unity.LastCheckup(proj).TexFix; !late && (tf == nil || tf.Record != rec) {
			t.Errorf("the record was in the project when the fix was abandoned, and is not offered: %+v", tf)
		}
		r, err := unity.RunCheckup(context.Background(), proj, "") // (answered once Unity is through with the fix)
		if err != nil {
			t.Fatal(err)
		}
		tf := r.TexFix
		if tf == nil || tf.Record != rec || !tf.Adopted || tf.Kind != "textures" || tf.Changed != 2 || tf.Max != 2048 || tf.At != 1791000000 {
			t.Fatalf("late=%v: the textures were lowered, the record is in the project, and 「恢复」 is not offered: %+v", late, tf)
		}
		if len(tf.Items) != 2 || tf.Items[0].Path != "Assets/a.png" || tf.Items[0].From != 4096 || tf.Items[0].To != 2048 || tf.Items[1].Path != "Assets/b.png" || tf.Items[1].From != 0 {
			t.Errorf("what the record names: %+v", tf.Items)
		}
		if len(r.TexFixOlder) != 1 || !strings.HasSuffix(r.TexFixOlder[0].Record, "/20261003_090000_000.json") {
			t.Errorf("the record before it: %+v", r.TexFixOlder)
		}
		// once only, however many check-ups follow
		if r, _ = unity.RunCheckup(context.Background(), proj, ""); r.TexFix == nil || r.TexFix.Record != rec || len(r.TexFixOlder) != 1 {
			t.Errorf("a later check-up: %+v %+v", r.TexFix, r.TexFixOlder)
		}
	}
}

// A second texture fix does not orphan the first one's record: after the newest is taken back the one before
// it is offered. texFix stays the newest in checkups.json; a record that is gone gives way to the next.
func TestTexFixesAreAStack(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	n, gone := 0, ""
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch cmd {
		case "checkup":
			return map[string]any{"avatar": "Kaguya", "path": "Kaguya", "prebuild": true, "items": []any{}}, ""
		case "fix":
			n++
			return map[string]any{"kind": "textures", "max": 2048, "changed": n, "record": fmt.Sprintf("UserSettings/MioVRCA/fixes/R%d.json", n), "items": []any{}}, ""
		case "fix_revert":
			if args["record"] == gone {
				return nil, "恢复记录已不存在，无法恢复：" + gone
			}
			return map[string]any{"kind": "textures", "reverted": 1, "items": []any{}}, ""
		}
		return nil, "不认识的操作：" + cmd
	})
	_ = os.WriteFile(filepath.Join(proj, "Packages", unity.PipePkg, "package.json"), []byte(`{}`), 0644)
	ctx := context.Background()
	if _, err := unity.RunCheckup(ctx, proj, ""); err != nil {
		t.Fatal(err)
	}
	rec := func(i int) string { return fmt.Sprintf("UserSettings/MioVRCA/fixes/R%d.json", i) }
	stack := func(r *unity.CheckupRecord) string {
		var got []string
		if r.TexFix != nil {
			got = append(got, strings.TrimSuffix(filepath.Base(r.TexFix.Record), ".json"))
		}
		for _, f := range r.TexFixOlder {
			got = append(got, strings.TrimSuffix(filepath.Base(f.Record), ".json"))
		}
		return strings.Join(got, " ")
	}
	var r *unity.CheckupRecord
	for i := 1; i <= 3; i++ {
		var err error
		if _, r, _, err = unity.RunFix(ctx, proj, "textures", map[string]any{"all": true}); err != nil {
			t.Fatal(err)
		}
	}
	if stack(r) != "R3 R2 R1" || r.TexFix.Changed != 3 {
		t.Fatalf("after three fixes: %s", stack(r))
	}
	// the file: texFix is the newest (what an older program reads), the others behind it; read again as written
	b, _ := os.ReadFile(filepath.Join(core.DataDir, "checkups.json"))
	var f struct {
		Projects map[string]struct {
			TexFix      *unity.FixResult   `json:"texFix"`
			TexFixOlder []*unity.FixResult `json:"texFixOlder"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(b, &f); err != nil || len(f.Projects) != 1 {
		t.Fatalf("checkups.json: %v %s", err, b)
	}
	for _, p := range f.Projects {
		if p.TexFix == nil || p.TexFix.Record != rec(3) || len(p.TexFixOlder) != 2 || p.TexFixOlder[0].Record != rec(2) {
			t.Errorf("checkups.json: %s", b)
		}
	}
	unity.CheckupsReload()
	if got := stack(unity.LastCheckup(proj)); got != "R3 R2 R1" {
		t.Errorf("read again: %s", got)
	}
	// a later check-up keeps them all
	if r, _ = unity.RunCheckup(ctx, proj, ""); stack(r) != "R3 R2 R1" {
		t.Errorf("after a check-up: %s", stack(r))
	}
	// the newest taken back: the one before it is offered
	if _, r, _, _ = unity.RevertFix(ctx, proj, rec(3), ""); stack(r) != "R2 R1" {
		t.Fatalf("after taking R3 back: %s", stack(r))
	}
	// a record that is gone from the project is dropped, and the next one offered
	gone = rec(2)
	if _, _, _, err := unity.RevertFix(ctx, proj, rec(2), ""); err == nil {
		t.Fatal("a record that is gone was taken back")
	}
	if got := stack(unity.LastCheckup(proj)); got != "R1" {
		t.Fatalf("after R2 was found gone: %s", got)
	}
	if _, r, _, _ = unity.RevertFix(ctx, proj, rec(1), ""); stack(r) != "" || r.TexFixOlder != nil {
		t.Fatalf("after taking R1 back: %s", stack(r))
	}
}

// the report card is kept under reports/ with a safe name; what is not a PNG, or too big, is refused
func TestSaveReport(t *testing.T) {
	testkit.NewStore(t)
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 100)
	p, err := unity.SaveReport("Kaguya/Test: v2?", "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte(png)))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != filepath.Join(core.DataDir, "reports") || !strings.HasPrefix(filepath.Base(p), "checkup_Kaguya／Test： v2？_") || !strings.HasSuffix(p, ".png") {
		t.Errorf("path %s", p)
	}
	if b, _ := os.ReadFile(p); string(b) != png {
		t.Error("the file is not the picture")
	}
	// a second one in the same minute does not replace the first
	p2, err := unity.SaveReport("Kaguya/Test: v2?", base64.StdEncoding.EncodeToString([]byte(png)))
	if err != nil || p2 == p || !core.StatOK(p) {
		t.Errorf("second: %v %s", err, p2)
	}
	if p3, err := unity.SaveReport("", base64.StdEncoding.EncodeToString([]byte(png))); err != nil || !strings.HasPrefix(filepath.Base(p3), "checkup_avatar_") {
		t.Errorf("no avatar: %v %s", err, p3)
	}
	if _, err := unity.SaveReport("A", base64.StdEncoding.EncodeToString([]byte("GIF89a"))); err == nil || !strings.Contains(err.Error(), "无效") {
		t.Errorf("not a png: %v", err)
	}
	if _, err := unity.SaveReport("A", "%%%"); err == nil {
		t.Error("bad base64 was taken")
	}
	if _, err := unity.SaveReport("A", strings.Repeat("A", 5<<20)); err == nil || !strings.Contains(err.Error(), "过大") {
		t.Errorf("too big: %v", err)
	}
	if j, _ := json.Marshal(unity.FixKinds); !strings.Contains(string(j), "textures") {
		t.Error("the kinds")
	}
}
