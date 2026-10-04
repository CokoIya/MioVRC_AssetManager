package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
	"vrclib/internal/unity/unitytest"
)

// the window's requests for a one-click fix, its way back, and the report card's file
func TestCheckupFixAPI(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	_ = os.MkdirAll(filepath.Join(proj, "ProjectSettings"), 0755)
	st.Projects = []core.ProjectInfo{{Name: filepath.Base(proj), Path: proj}}
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()

	if r := postJSON(t, srv, "/api/checkup/fix", map[string]any{"project": "/nowhere", "kind": "lights"}); r["ok"] != false || !strings.Contains(r["err"].(string), "不在工程列表中") {
		t.Errorf("unknown project: %v", r)
	}
	if r := postJSON(t, srv, "/api/checkup/fix", map[string]any{"project": proj, "kind": "lights"}); r["ok"] != false || !strings.Contains(r["err"].(string), "尚未安装 Unity 插件") {
		t.Errorf("no plugin: %v", r)
	}
	lights, checks := 1, 0
	var asked []map[string]any
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch cmd {
		case "checkup":
			checks++
			return map[string]any{"avatar": "Kaguya", "path": "Kaguya", "prebuild": true, "sdkCalc": true, "ranks": map[string]any{"pc": "Good"}, "items": []any{
				map[string]any{"id": "fig.lights", "group": "figure", "label": "灯光", "level": map[bool]string{true: "warn", false: "ok"}[lights > 0], "value": lights, "rating": "Poor"}}}, ""
		case "fix":
			asked = append(asked, args)
			if args["kind"] == "lights" {
				lights = 0
				return map[string]any{"kind": "lights", "changed": 1, "items": []any{map[string]any{"t": "Head/Lamp", "n": "已移除"}}, "undo": "MioVRCA 移除灯光"}, ""
			}
			return map[string]any{"kind": "textures", "max": args["max"], "changed": 1, "record": "UserSettings/MioVRCA/fixes/a.json", "items": []any{map[string]any{"path": args["paths"].([]any)[0], "from": 4096, "to": args["max"]}}}, ""
		case "fix_revert":
			asked = append(asked, args)
			return map[string]any{"kind": "textures", "reverted": 1, "items": []any{}}, ""
		}
		return nil, "不认识的操作：" + cmd
	})
	_ = os.WriteFile(filepath.Join(proj, "Packages", unity.PipePkg, "package.json"), []byte(`{}`), 0644)

	r := postJSON(t, srv, "/api/checkup/fix", map[string]any{"project": proj, "kind": "lights", "avatar": "Kaguya"})
	fix, _ := r["fix"].(map[string]any)
	rec, _ := r["record"].(map[string]any)
	if r["ok"] != true || fix == nil || fix["changed"] != float64(1) || fix["undo"] != "MioVRCA 移除灯光" || rec == nil || r["can"] != true || r["recheckErr"] != nil {
		t.Fatalf("lights: %v", r)
	}
	if a := asked[0]; a["kind"] != "lights" || a["all"] != true || a["avatar"] != "Kaguya" || a["remove"] != nil {
		t.Errorf("asked %v", a)
	}
	if items := rec["result"].(map[string]any)["items"].([]any); checks != 1 || items[0].(map[string]any)["level"] != "ok" {
		t.Errorf("the check-up after the fix: %d %v", checks, items)
	}
	// a scene fix from the panel is a step 「撤销上一步」 is offered for, as the panel's hint says
	if r := postJSON(t, srv, "/api/ai/session", map[string]any{"project": proj}); r["changes"] != float64(1) {
		t.Errorf("the panel's fix is not counted for 「撤销上一步」: %v", r["changes"])
	}
	// chosen textures, a size, the removal of lights
	r = postJSON(t, srv, "/api/checkup/fix", map[string]any{"project": proj, "kind": "textures", "paths": []string{"Assets/T/a.png"}, "max": 1024})
	if r["ok"] != true || asked[1]["all"] != nil || asked[1]["max"] != float64(1024) || r["fix"].(map[string]any)["record"] != "UserSettings/MioVRCA/fixes/a.json" {
		t.Errorf("textures: %v (asked %v)", r, asked[1])
	}
	if r := postJSON(t, srv, "/api/ai/session", map[string]any{"project": proj}); r["changes"] != float64(1) {
		t.Errorf("a texture fix is counted as an undo step: %v", r["changes"])
	}
	postJSON(t, srv, "/api/checkup/fix", map[string]any{"project": proj, "kind": "lights", "remove": true})
	if asked[2]["remove"] != true {
		t.Errorf("remove: %v", asked[2])
	}
	if r := postJSON(t, srv, "/api/checkup/fix", map[string]any{"project": proj, "kind": "params"}); r["ok"] != false || !strings.Contains(r["err"].(string), "没有这项一键处理") {
		t.Errorf("params: %v", r)
	}
	r = postJSON(t, srv, "/api/checkup/fix/revert", map[string]any{"project": proj, "record": "UserSettings/MioVRCA/fixes/a.json", "avatar": "Kaguya"})
	if r["ok"] != true || r["revert"].(map[string]any)["reverted"] != float64(1) || asked[3]["record"] != "UserSettings/MioVRCA/fixes/a.json" || r["record"] == nil || checks != 4 {
		t.Errorf("revert: %v (asked %v, %d checks)", r, asked[3], checks)
	}
	// a texture fix whose answer never arrived (stopped, or broken off half way) left its record in the
	// project: the page is offered 「恢复」 from it the next time it asks, without another check-up
	if rec, _ := postJSON(t, srv, "/api/checkup/last", map[string]any{"project": proj})["record"].(map[string]any); rec == nil || rec["texFix"] != nil {
		t.Fatalf("before: %v", rec)
	}
	_ = os.MkdirAll(filepath.Join(proj, "UserSettings", "MioVRCA", "fixes"), 0755)
	_ = os.WriteFile(filepath.Join(proj, "UserSettings", "MioVRCA", "fixes", "20261004_120000_000.json"), []byte(`{"kind":"textures","at":1791000000,"max":2048,"textures":[{"path":"Assets/T/a.png","max":4096}]}`), 0644)
	rec, _ = postJSON(t, srv, "/api/checkup/last", map[string]any{"project": proj})["record"].(map[string]any)
	if tf, _ := rec["texFix"].(map[string]any); tf == nil || tf["record"] != "UserSettings/MioVRCA/fixes/20261004_120000_000.json" || tf["adopted"] != true || tf["changed"] != float64(1) || checks != 4 {
		t.Errorf("the record in the project is not offered: %v (%d checks)", rec["texFix"], checks)
	}

	// the report card
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n...."))
	r = postJSON(t, srv, "/api/checkup/report/save", map[string]any{"avatar": "Kaguya", "png": "data:image/png;base64," + png})
	p, _ := r["path"].(string)
	if r["ok"] != true || !strings.HasPrefix(p, filepath.Join(core.DataDir, "reports")) || !core.StatOK(p) {
		t.Errorf("save: %v", r)
	}
	if r := postJSON(t, srv, "/api/checkup/report/save", map[string]any{"avatar": "Kaguya", "png": "bm90IGEgcG5n"}); r["ok"] != false || !strings.Contains(r["err"].(string), "无效") {
		t.Errorf("not a png: %v", r)
	}
}

// The check-up panel's side of the fixes, played outside a browser (needs node; skipped without it): what the
// three questions say, when the button is there, and that 「恢复」 follows what the server holds.
func TestCheckupPanelFixes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	raw, err := exec.Command(node, "testdata/checkup_harness.js", filepath.Join("..", "..", "web", "checkup.js")).Output()
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	type note struct {
		Revert bool
		Text   string
	}
	var got struct {
		Error                                                                                     string
		AskTextures                                                                               string `json:"ask_textures"`
		AskLights                                                                                 string `json:"ask_lights"`
		AskMissing                                                                                string `json:"ask_missing"`
		BtnTextures, BtnNothingToLower, BtnOlderPlugin, BtnMissing                                string
		First, Second, AfterPoll, Third, AfterRevert2, AfterMissing, AfterMissingPoll             note
		AfterRevert1, Adopted                                                                     note
		FirstToast, SecondToast, Revert2Asked, Revert2Toast, MissingAsked, LightsNote, RevertText string
		LightsPolled                                                                              int
	}
	if err := json.Unmarshal(raw, &got); err != nil || got.Error != "" {
		t.Fatalf("harness output %q: %v %s", raw, err, got.Error)
	}
	has := func(what, text string, wants ...string) {
		t.Helper()
		for _, w := range wants {
			if !strings.Contains(text, w) {
				t.Errorf("%s lacks %q: %q", what, w, text)
			}
		}
	}
	// the questions: only what the fix will change is counted, and what it reaches beyond the avatar is said
	has("the textures question", got.AskTextures, "将把模型用到的 2 张 4096 像素贴图的 Max Size 改为 2048", "另有 1 张不在 Assets 文件夹内或没有可调整的导入设置，不会改动",
		"贴图的导入设置对整个工程生效，使用这些贴图的其他模型和场景也会随之改变", "处理后可点击「恢复」改回原设置")
	has("the lights question", got.AskLights, "将移除模型下的 2 个灯光组件", "Ctrl+Z 或点击「撤销上一步」撤销")
	has("the missing scripts question", got.AskMissing, "将移除模型下 4 个丢失脚本的组件", "插件（Modular Avatar、VRCFury 等）没有安装或编译出错，请不要移除，应先安装或修复该插件",
		"在场景保存后无法找回，重新安装插件也不会恢复", "保存场景前可在 Unity 中按 Ctrl+Z 或点击「撤销上一步」撤销")
	// the button: there for what it can change, not for textures it cannot lower; the hint under 丢失的脚本 does
	// not put removal forward as the answer
	if !strings.Contains(got.BtnTextures, "data-ckfix") || !strings.Contains(got.BtnOlderPlugin, "data-ckfix") || strings.Contains(got.BtnNothingToLower, "data-ckfix") || !strings.Contains(got.BtnNothingToLower, "无法一键处理") {
		t.Errorf("the textures button: %q / %q / %q", got.BtnTextures, got.BtnNothingToLower, got.BtnOlderPlugin)
	}
	has("the hint under 丢失的脚本", got.BtnMissing, "仅在确认这些组件已不再需要时使用", "请先安装插件")
	if strings.Contains(got.BtnMissing, "撤销") {
		t.Errorf("the hint under 丢失的脚本 offers removal as something easily taken back: %q", got.BtnMissing)
	}
	// a second click that changes nothing: the first fix's note and its 「恢复」 stay
	has("the note after the fix", got.First.Text, "已将 2 张贴图的 Max Size 降到 2048，所处理贴图的显存估计由 42.7 MB 降到 10.7 MB")
	if !got.First.Revert || got.Second != got.First || got.AfterPoll != got.First || got.SecondToast != "没有改动任何内容" {
		t.Errorf("after a click that changed nothing: %+v (toast %q), then %+v; was %+v", got.Second, got.SecondToast, got.AfterPoll, got.First)
	}
	// the newest fix is taken back first, then the one before it is offered
	if !got.Third.Revert || !strings.HasSuffix(got.Revert2Asked, "/R2.json") || got.AfterRevert2 != got.First || got.Revert2Toast != "已恢复 1 张贴图的导入设置" {
		t.Errorf("two fixes: %+v, asked %q, then %+v (toast %q)", got.Third, got.Revert2Asked, got.AfterRevert2, got.Revert2Toast)
	}
	// a texture that was not found: said, listed, and 「恢复」 stays (the record is kept)
	has("the note after a revert that missed a texture", got.AfterMissing.Text, "部分已恢复", "已恢复 1 张贴图的导入设置，1 张未找到（可能已移动或改名），恢复记录已保留", "Assets/moved.png 未找到")
	if !strings.HasSuffix(got.MissingAsked, "/R1.json") || !got.AfterMissing.Revert || got.AfterMissingPoll != got.AfterMissing {
		t.Errorf("a texture not found: asked %q, %+v, then %+v", got.MissingAsked, got.AfterMissing, got.AfterMissingPoll)
	}
	if got.AfterRevert1.Revert || !strings.Contains(got.AfterRevert1.Text, "已恢复原设置") || got.RevertText != "已恢复 3 张贴图的导入设置" {
		t.Errorf("everything put back: %+v", got.AfterRevert1)
	}
	// a fix whose answer never arrived
	has("the note for a record found in the project", got.Adopted.Text, "工程中留有一次贴图处理的恢复记录（3 张贴图），可点击「恢复」改回原设置", "Assets/a.png")
	if !got.Adopted.Revert {
		t.Errorf("a record found in the project is not offered for 「恢复」: %+v", got.Adopted)
	}
	// a scene fix: the pipeline page asks for the session at once, so 「撤销上一步」 is there
	if got.LightsPolled != 1 || !strings.Contains(got.LightsNote, "可用 Ctrl+Z 或「撤销上一步」撤销") {
		t.Errorf("lights: polled %d, %q", got.LightsPolled, got.LightsNote)
	}
}
