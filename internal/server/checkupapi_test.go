package server

import (
	"bytes"
	"image"
	"image/png"
	"net/http/httptest"
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

// the window's requests: a check-up, its last result, a cover from Unity
func TestCheckupAndCoverAPI(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	for _, f := range []string{"Assets/Shop/Dress/Dress.prefab", "ProjectSettings/ProjectVersion.txt"} {
		p := filepath.Join(proj, filepath.FromSlash(f))
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("x"), 0644)
	}
	st.Projects = []core.ProjectInfo{{Name: filepath.Base(proj), Path: proj}}
	st.Assets = []*core.Asset{{Key: "name:dress", Name: "Dress", Category: "衣服", Usage: []core.Usage{{Project: filepath.Base(proj), Status: "used", Folder: "Assets/Shop/Dress"}}}}
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()

	if r := postJSON(t, srv, "/api/checkup/run", map[string]any{"project": "/nowhere"}); r["ok"] != false || !strings.Contains(r["err"].(string), "不在工程列表中") {
		t.Errorf("unknown project: %v", r)
	}
	if r := postJSON(t, srv, "/api/checkup/last", map[string]any{"project": proj}); r["ok"] != true || r["record"] != nil || r["can"] != false || !strings.Contains(r["why"].(string), "尚未安装 Unity 插件") {
		t.Errorf("before anything: %v", r)
	}
	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 64, 64)))
	unitytest.FakeUnity(t, proj, func(cmd string, args map[string]any) (any, string) {
		switch cmd {
		case "checkup":
			return map[string]any{"avatar": "Kaguya", "prebuild": true, "sdkCalc": true, "ranks": map[string]any{"pc": "Good", "quest": "VeryPoor"}, "items": []any{
				map[string]any{"id": "params", "group": "limit", "label": "同步参数", "level": "ok", "value": 120, "limit": 256, "unit": "位"},
				map[string]any{"id": "fig.triangles", "group": "figure", "label": "三角面", "level": "warn", "value": 90000, "rating": "VeryPoor"}}}, ""
		case "prefabs":
			return map[string]any{"prefabs": []any{map[string]any{"path": "Assets/Shop/Dress/Dress.prefab", "name": "Dress", "renderers": 4}}}, ""
		case "prefab_shot":
			file := filepath.Join(proj, "Temp", "MioVRCA", "prefabshots", "dress.png")
			_ = os.MkdirAll(filepath.Dir(file), 0755)
			_ = os.WriteFile(file, img.Bytes(), 0644)
			return map[string]any{"shots": []any{map[string]any{"prefab": "Assets/Shop/Dress/Dress.prefab", "file": file}}}, ""
		}
		return nil, "不认识的操作：" + cmd
	})
	_ = os.WriteFile(filepath.Join(proj, "Packages", unity.PipePkg, "package.json"), []byte(`{}`), 0644)

	r := postJSON(t, srv, "/api/checkup/run", map[string]any{"project": proj})
	rec, _ := r["record"].(map[string]any)
	if r["ok"] != true || rec == nil || rec["avatar"] != "Kaguya" || r["can"] != true {
		t.Fatalf("run: %v", r)
	}
	if items := rec["result"].(map[string]any)["items"].([]any); len(items) != 2 {
		t.Errorf("items: %v", items)
	}
	if r := postJSON(t, srv, "/api/checkup/last", map[string]any{"project": proj}); r["record"] == nil || r["can"] != true {
		t.Errorf("last: %v", r)
	}
	if r := postJSON(t, srv, "/api/checkup/all", map[string]any{}); len(r["records"].(map[string]any)) != 1 {
		t.Errorf("all: %v", r)
	}
	// the project's card carries it in short
	cards := postJSON(t, srv, "/api/projects", map[string]any{})["projects"].([]any)
	if c, _ := cards[0].(map[string]any)["checkup"].(map[string]any); c == nil || c["pc"] != "Good" || c["warn"] != float64(1) || c["fail"] != float64(0) {
		t.Errorf("card: %v", cards[0])
	}

	// a cover for the asset that has none
	if s := postJSON(t, srv, "/api/cover/status", map[string]any{"key": "name:dress"})["status"].(map[string]any); s["can"] != true || s["real"] != false || s["generated"] != false {
		t.Errorf("status: %v", s)
	}
	rev := core.CurRev()
	g := postJSON(t, srv, "/api/cover/generate", map[string]any{"key": "name:dress"})
	if g["ok"] != true || g["prefab"] != "Assets/Shop/Dress/Dress.prefab" || g["status"].(map[string]any)["generated"] != true || core.CurRev() == rev {
		t.Fatalf("generate: %v", g)
	}
	// …which the state shows, and /thumb serves
	state := func() string {
		resp, err := srv.Client().Get(srv.URL + "/api/state")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return b.String()
	}
	if s := state(); !strings.Contains(s, "generated") || strings.Contains(s, "coverGen") {
		t.Errorf("the state does not show the cover (or carries a key of ours)")
	}
	if !imageAllowed(st, filepath.Join(core.DataDir, "covers", "generated", "x.png")) {
		t.Error("/thumb would refuse a generated cover")
	}
	if r := postJSON(t, srv, "/api/cover/remove", map[string]any{"key": "name:dress"}); r["status"].(map[string]any)["generated"] != false || strings.Contains(state(), "generated") {
		t.Errorf("remove: %v", r)
	}
	// every cover-less asset of the project, as a task
	if r := postJSON(t, srv, "/api/cover/batch", map[string]any{"project": proj}); r["ok"] != true {
		t.Fatalf("batch: %v", r)
	}
	var b map[string]any
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if b = postJSON(t, srv, "/api/cover/batch/status", map[string]any{})["batch"].(map[string]any); b["running"] != true {
			break
		}
	}
	if b["running"] != false || b["made"] != float64(1) || b["total"] != float64(1) {
		t.Errorf("batch: %v", b)
	}
	if r := postJSON(t, srv, "/api/cover/batch", map[string]any{"project": proj}); r["ok"] != false || !strings.Contains(r["err"].(string), "都已有封面") {
		t.Errorf("nothing left: %v", r)
	}
	postJSON(t, srv, "/api/cover/batch/cancel", map[string]any{})
}
