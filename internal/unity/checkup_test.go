package unity_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
	"vrclib/internal/unity/unitytest"
)

// what Checkup.cs answers for an avatar with one blocker, one thing to improve and two figures
const checkupAnswer = `{"prebuild":true,"target":"pc","avatar":"Kaguya","path":"Kaguya","sdkCalc":true,"ranks":{"pc":"VeryPoor","quest":"VeryPoor"},
 "tools":{"modularAvatar":"1.18.3","avatarOptimizer":false,"vrcfury":true},"ms":120,"items":[
 {"id":"params","group":"limit","label":"同步参数","level":"fail","value":262,"limit":256,"unit":"位","atLeast":true,
  "details":[{"t":"参数资产中已有","v":230,"u":"位"},{"t":"VRCFury 在构建时生成","n":"无法预估"}],"advice":"同步参数超过 256 位，上传会被拒绝。"},
 {"id":"physbones","group":"limit","label":"PhysBone 组件","level":"ok","value":88,"limit":256,"rating":"VeryPoor","tiers":[4,8,16,32]},
 {"id":"materials","group":"look","label":"粉色或缺失的材质","level":"fail","value":2,
  "details":[{"t":"Dress/Skirt","n":"着色器未安装","m":"Skirt_A","f":"lilToon"},{"t":"Dress/Ribbon","n":"材质槽为空"}],"advice":"这些材质在游戏中会显示为粉色。"},
 {"id":"fig.triangles","group":"figure","label":"三角面","level":"warn","value":96000,"rating":"VeryPoor","ratingQuest":"VeryPoor","tiers":[32000,70000,70000,70000],"advice":"三角面数超出 Poor 档的上限。"},
 {"id":"fig.lights","group":"figure","label":"灯光","level":"ok","value":0,"rating":"Excellent"},
 {"id":"fig.textureMemory","group":"figure","label":"贴图显存","level":"ok","value":88.5,"unit":"MB","rating":"Medium"},
 {"id":"fig.bounds","group":"figure","label":"包围盒","level":"ok","text":"0.8 × 1.5 × 0.5","unit":"米","rating":"Excellent"}]}`

func TestRunCheckup(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	if _, err := unity.RunCheckup(context.Background(), proj, ""); err == nil || !strings.Contains(err.Error(), "尚未安装 Unity 插件") {
		t.Errorf("no plugin: %v", err)
	}
	asked := map[string]any{}
	old := false
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		if cmd != "checkup" || old {
			return nil, "不认识的操作：" + cmd
		}
		asked = args
		return json.RawMessage(checkupAnswer), ""
	})
	_ = os.WriteFile(filepath.Join(proj, "Packages", unity.PipePkg, "package.json"), []byte(`{"name":"com.miovrc.pipeline"}`), 0644)
	if unity.LastCheckup(proj) != nil {
		t.Error("a check-up before any was run")
	}
	rec, err := unity.RunCheckup(context.Background(), proj, "Kaguya")
	if err != nil {
		t.Fatal(err)
	}
	if asked["avatar"] != "Kaguya" {
		t.Errorf("asked %v", asked)
	}
	fail, warn := rec.Result.Counts()
	if rec.Avatar != "Kaguya" || rec.At == 0 || rec.Plugin != unity.EmbeddedPipelineVersion() || fail != 2 || warn != 1 || len(rec.Result.Items) != 7 {
		t.Errorf("record %+v (%d fail, %d warn)", rec, fail, warn)
	}
	if it := rec.Result.Items[0]; it.Value == nil || *it.Value != 262 || it.Limit != 256 || !it.AtLeast || len(it.Details) != 2 || it.Details[0].V == nil || *it.Details[0].V != 230 {
		t.Errorf("first item %+v", it)
	}
	// kept: the card reads it after a restart
	b, err := os.ReadFile(filepath.Join(core.DataDir, "checkups.json"))
	if err != nil || !strings.Contains(string(b), `"fig.triangles"`) {
		t.Fatalf("checkups.json: %v %s", err, b)
	}
	if core.StatOK(filepath.Join(core.DataDir, "checkups.json.tmp")) {
		t.Error("the temporary file was left behind")
	}
	last := unity.LastCheckup(proj)
	if last == nil || last.At != rec.At {
		t.Fatalf("last %+v", last)
	}
	if br := last.Brief(); br.PC != "VeryPoor" || br.Quest != "VeryPoor" || br.Fail != 2 || br.Warn != 1 || br.Avatar != "Kaguya" {
		t.Errorf("brief %+v", br)
	}
	if all := unity.AllCheckups(); len(all) != 1 || all[proj] == nil {
		t.Errorf("all %v", all)
	}
	// another data folder has its own records, and this one's are read again from the file
	dir := core.DataDir
	core.DataDir = t.TempDir()
	if unity.LastCheckup(proj) != nil {
		t.Error("a record from another data folder")
	}
	core.DataDir = dir
	if l := unity.LastCheckup(proj); l == nil || len(l.Result.Items) != 7 || l.Result.Ranks["pc"] != "VeryPoor" {
		t.Errorf("read again: %+v", l)
	}

	// in words, for the AI: what needs handling in full, with the advice; the figures in a line
	text := rec.TextForAI(6000)
	for _, want := range []string{"2 项需处理，1 项建议优化", "PC 极差（Very Poor）", "同步参数：至少 262 / 256 位", "Dress/Skirt（材质 Skirt_A）：着色器未安装，可能是 lilToon",
		"建议：同步参数超过 256 位", "【建议优化】", "三角面：96000，极差（Very Poor）", "构建前的数值", "贴图显存 88.5 MB（Medium）", "PhysBone 组件 88 / 256（VeryPoor）"} {
		if !strings.Contains(text, want) {
			t.Errorf("the AI's text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "灯光") {
		t.Errorf("a figure of zero is said:\n%s", text)
	}
	if short := rec.TextForAI(700); len(short) > 800 || !strings.Contains(short, "【需处理】") || strings.Contains(short, "【其余数值】") {
		t.Errorf("cut short (%d bytes):\n%s", len(short), short)
	}

	// a plugin from before the check-up: the player is told to update it
	old = true
	if _, err := unity.RunCheckup(context.Background(), proj, ""); err == nil || !strings.Contains(err.Error(), "点击「更新」") {
		t.Errorf("old plugin: %v", err)
	}
	if l := unity.LastCheckup(proj); l == nil || l.At != rec.At {
		t.Error("a failed check-up replaced the last one")
	}
}

// what stands in the way is said before Unity is asked
func TestCheckupReady(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	pkg := filepath.Join(proj, "Packages", unity.PipePkg)
	_ = os.MkdirAll(pkg, 0755)
	_ = os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{}`), 0644)
	if err := unity.CheckupReady(proj); err == nil || !strings.Contains(err.Error(), "尚未在 Unity 中打开") {
		t.Errorf("closed: %v", err)
	}
	alive := func(s string) {
		_ = os.MkdirAll(unity.BridgeDir(proj), 0755)
		_ = os.WriteFile(filepath.Join(unity.BridgeDir(proj), "alive.json"), []byte(s), 0644)
	}
	alive(`{"bridge":"1.2.3","playing":true}`)
	if err := unity.CheckupReady(proj); err == nil || !strings.Contains(err.Error(), "Play 模式") {
		t.Errorf("playing: %v", err)
	}
	if _, err := unity.RunCheckup(context.Background(), proj, ""); err == nil || !strings.Contains(err.Error(), "Play 模式") {
		t.Errorf("run while playing: %v", err)
	}
	alive(`{"bridge":"1.2.3","compiling":true}`)
	if err := unity.CheckupReady(proj); err == nil || !strings.Contains(err.Error(), "正在编译") {
		t.Errorf("compiling: %v", err)
	}
	alive(`{"bridge":"1.2.3"}`)
	if err := unity.CheckupReady(proj); err != nil {
		t.Errorf("ready: %v", err)
	}
}
