package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func testWorn() ([]wornInfo, []planned) {
	meshes := []meshInfo{{Path: "Dress/Body"}, {Path: "Dress/Jacket"}, {Path: "Dress/Shoes"}, {Path: "Dress/Ribbon"}}
	w := wornInfo{kind: "衣服", label: "Dress", object: "Dress", meshes: meshes, base: []string{"衣服"}, param: "Clothtoggle", first: true,
		colours: []colourInfo{{Prefab: "Assets/D/Dress_Black.prefab", Label: "Black"}}}
	h := wornInfo{kind: "头发", label: "Twin", object: "Twin", meshes: []meshInfo{{Path: "Twin/Hair"}, {Path: "Twin/Clip"}}, base: []string{"头发"}, param: "Hair_Choose"}
	plan := []planned{
		{"Avatar Menu", map[string]any{"kind": "outfit", "path": []string{"衣服", "Dress"}, "label": "Dress", "objects": []string{"Dress"}, "default": true, "parameter": "Clothtoggle"}},
		{"Avatar Menu", map[string]any{"kind": "outfit", "path": []string{"衣服", "Dress"}, "label": "Black", "objects": []string{"Dress"}, "materialsFrom": "Assets/D/Dress_Black.prefab", "parameter": "Clothtoggle"}},
		{"Avatar Menu", map[string]any{"kind": "part", "path": []string{"衣服", "Dress"}, "label": "鞋子", "objects": []string{"Dress/Shoes"}}},
		{"Avatar Menu", map[string]any{"kind": "strip", "path": []string{"衣服"}, "label": "一键脱光", "parameter": "Clothtoggle"}},
		{"Avatar Menu", map[string]any{"kind": "outfit", "path": []string{"头发"}, "label": "原装头发", "objects": []string{"Hair_base"}, "parameter": "Hair_Choose"}},
		{"Avatar Menu", map[string]any{"kind": "outfit", "path": []string{"头发"}, "label": "Twin", "objects": []string{"Twin"}, "parameter": "Hair_Choose"}},
		{"Avatar Menu", map[string]any{"kind": "toggle", "path": []string{"配饰"}, "label": "Ring", "objects": []string{"Ring"}, "default": true}},
	}
	w.items = []int{0, 1, 2}
	h.items = []int{5}
	return []wornInfo{w, h}, plan
}

func items(js string) []map[string]any {
	var v []map[string]any
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		panic(err)
	}
	return v
}

// The AI's menu goes in where the draft's was, with the draft's parameters and default; everything else of the
// plan stays as and where it was. What does not hold is said, item by item.
func TestMergeMenuPlan(t *testing.T) {
	worn, plan := testWorn()
	good := `[
	 {"kind":"part","path":["衣服","连衣裙"],"label":"外套","objects":["Dress/Jacket"]},
	 {"kind":"outfit","path":["衣服","连衣裙"],"label":"原色","objects":["Dress"],"default":false,"parameter":"X"},
	 {"kind":"outfit","path":["衣服","连衣裙"],"label":"黑色","objects":["Dress"],"materialsFrom":"Assets/D/Dress_Black.prefab"},
	 {"kind":"part","path":["衣服","连衣裙"],"label":"鞋子","objects":["Dress/Shoes"]},
	 {"kind":"outfit","path":["头发","双马尾"],"label":"戴上","objects":["Twin"]},
	 {"kind":"part","path":["头发","双马尾"],"label":"发夹","objects":["Twin/Clip"]}]`
	merged, n, errs := mergeMenuPlan(items(good), worn, plan)
	if len(errs) > 0 {
		t.Fatalf("errors %v", errs)
	}
	var got []string
	for _, p := range merged {
		got = append(got, fmt.Sprintf("%v:%v:%v:%v", p.item["kind"], p.item["label"], p.item["default"], p.item["parameter"]))
	}
	want := "outfit:原色:true:Clothtoggle outfit:黑色:<nil>:Clothtoggle part:外套:<nil>:<nil> part:鞋子:<nil>:<nil> strip:一键脱光:<nil>:Clothtoggle " +
		"outfit:原装头发:<nil>:Hair_Choose outfit:戴上:<nil>:Hair_Choose part:发夹:<nil>:<nil> toggle:Ring:true:<nil>"
	if strings.Join(got, " ") != want || n != 6 {
		t.Errorf("merged (%d):\n got %s\nwant %s", n, strings.Join(got, " "), want)
	}
	for _, c := range []struct{ plan, says string }{
		{`[{"kind":"outfit","path":["衣服","连衣裙"],"label":"原色","objects":["Dress"]},{"kind":"outfit","path":["头发"],"label":"戴上","objects":["Twin"]}]`, "缺少配色「Black」"},
		{`[{"kind":"outfit","path":["配饰"],"label":"原色","objects":["Dress"]}]`, "path 要以 [衣服] 开头"},
		{`[{"kind":"part","path":["衣服"],"label":"全部","objects":["Dress/Body","Dress/Jacket","Dress/Shoes","Dress/Ribbon"]}]`, "包含了「Dress」的全部网格"},
		{`[{"kind":"part","path":["衣服"],"label":"A","objects":["Dress/Shoes"]},{"kind":"part","path":["衣服"],"label":"B","objects":["Dress/Shoes"]}]`, "已经放在部件「A」里"},
		{`[{"kind":"part","path":["衣服"],"label":"A","objects":["Dress/Shoes","Twin/Clip"]}]`, "分属几件衣服"},
		{`[{"kind":"part","path":["衣服"],"label":"A","objects":["Dress/Wings"]}]`, "「Dress/Wings」不是列出的网格"},
		{`[{"kind":"toggle","path":["衣服"],"label":"A","objects":["Dress"]}]`, "kind 只能是 outfit 或 part"},
		{`[{"kind":"outfit","path":["衣服"],"label":"原色","objects":["Dress","Twin"]}]`, "只能写一件衣服或头发的根物体"},
		{`[{"kind":"outfit","path":["衣服"],"label":"黑","objects":["Dress"],"materialsFrom":"Assets/D/Other.prefab"}]`, "不是「Dress」的配色"},
		{`[{"kind":"outfit","path":["衣服"],"label":"一二三四五六七八九十一二三四五六七","objects":["Dress"]}]`, "label 太长"},
	} {
		if _, _, errs := mergeMenuPlan(items(c.plan), worn, plan); !strings.Contains(strings.Join(errs, "\n"), c.says) {
			t.Errorf("%s: %v", c.says, errs)
		}
	}
	// a page takes eight, and each name once
	var many []string
	many = append(many, `{"kind":"outfit","path":["衣服","连衣裙"],"label":"原色","objects":["Dress"]}`, `{"kind":"outfit","path":["衣服","连衣裙"],"label":"黑色","objects":["Dress"],"materialsFrom":"Assets/D/Dress_Black.prefab"}`)
	for i, m := range []string{"Body", "Jacket", "Shoes"} {
		many = append(many, fmt.Sprintf(`{"kind":"part","path":["衣服","连衣裙"],"label":"P%d","objects":["Dress/%s"]}`, i, m))
	}
	hair := `{"kind":"outfit","path":["头发"],"label":"戴上","objects":["Twin"]}`
	if _, _, errs := mergeMenuPlan(items("["+strings.Join(append(many, hair), ",")+"]"), worn, plan); len(errs) > 0 {
		t.Errorf("five items: %v", errs)
	}
	dup := strings.Replace(many[3], `"P1"`, `"P0"`, 1)
	if _, _, errs := mergeMenuPlan(items("["+strings.Join([]string{many[0], many[1], many[2], dup, hair}, ",")+"]"), worn, plan); !strings.Contains(strings.Join(errs, ""), "两项都叫「P0」") {
		t.Errorf("same name twice: %v", errs)
	}
	worn[0].meshes = nil
	for i := 0; i < 9; i++ {
		worn[0].meshes = append(worn[0].meshes, meshInfo{Path: fmt.Sprintf("Dress/M%d", i)})
	}
	many = many[:2]
	for i := 0; i < 7; i++ {
		many = append(many, fmt.Sprintf(`{"kind":"part","path":["衣服","连衣裙"],"label":"P%d","objects":["Dress/M%d"]}`, i, i))
	}
	if _, _, errs := mergeMenuPlan(items("["+strings.Join(append(many, hair), ",")+"]"), worn, plan); !strings.Contains(strings.Join(errs, ""), "有 9 项，超过 8 项") {
		t.Errorf("nine on a page: %v", errs)
	}
}

func TestParsePlanItems(t *testing.T) {
	for in, n := range map[string]int{
		"```json\n{\"items\":[{\"kind\":\"part\"}]}\n```":                     1,
		"好的，方案如下：{\"items\":[{\"kind\":\"part\"},{\"kind\":\"outfit\"}]} 以上。": 2,
		"[{\"kind\":\"part\"}]": 1,
		"我觉得应该这样分……":            0,
		"{\"items\":[]}":        0,
	} {
		got, err := parsePlanItems(in)
		if len(got) != n || (n == 0) != (err != nil) {
			t.Errorf("%q: %d items, %v", in, len(got), err)
		}
	}
}
