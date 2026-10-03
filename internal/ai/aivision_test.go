package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

// eyeLLM: a service that may or may not read pictures. One that does really reads the number off the test
// picture; one that does not refuses any request with a picture in it, the way DeepSeek does.
type eyeLLM struct {
	mu        sync.Mutex
	wire      string
	sees      bool
	eyeModel  string // set: only this model reads pictures (the others refuse them)
	script    []mockTurn
	seen      []map[string]any // the requests of a run (those that offer tools)
	probes    int
	describes []string // what it was asked when describing
	auth      []string
	pictures  []int // pictures in each request of a run
	refused   int
}

// readDigits: the number on a probeImage, read back from its pixels.
func readDigits(data []byte) string {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	const cell, pad = 14, 22
	out := ""
	for i := 0; pad+i*6*cell+5*cell <= img.Bounds().Dx(); i++ {
		var rows [7]uint8
		for y := 0; y < 7; y++ {
			for x := 0; x < 5; x++ {
				r, g, b, _ := img.At(pad+i*6*cell+x*cell+cell/2, pad+y*cell+cell/2).RGBA()
				if (r+g+b)/3 < 0x8000 {
					rows[y] |= 0x10 >> x
				}
			}
		}
		d := "?"
		for n, want := range digitRows {
			if want == rows {
				d = fmt.Sprint(n)
			}
		}
		out += d
	}
	return out
}

func (m *eyeLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	m.auth = append(m.auth, r.Header.Get("Authorization")+"|"+r.Header.Get("x-api-key"))
	// the pictures and the text of the last user message
	var pics [][]byte
	text := ""
	msgs, _ := body["messages"].([]any)
	var walk func(v any)
	walk = func(v any) {
		switch o := v.(type) {
		case []any:
			for _, e := range o {
				walk(e)
			}
		case map[string]any:
			switch o["type"] {
			case "image_url":
				u, _ := o["image_url"].(map[string]any)["url"].(string)
				if i := strings.Index(u, "base64,"); i > 0 && strings.HasPrefix(u, "data:image/jpeg;") {
					d, _ := base64.StdEncoding.DecodeString(u[i+7:])
					pics = append(pics, d)
				}
			case "image":
				src, _ := o["source"].(map[string]any)
				if src["type"] == "base64" && src["media_type"] == "image/jpeg" {
					d, _ := base64.StdEncoding.DecodeString(fmt.Sprint(src["data"]))
					pics = append(pics, d)
				}
			case "text":
				text, _ = o["text"].(string)
			}
			if c, ok := o["content"]; ok {
				if s, ok := c.(string); ok {
					if o["role"] == "user" {
						text = s
					}
				} else {
					walk(c)
				}
			}
		}
	}
	walk(msgs)
	sees := m.sees
	if m.eyeModel != "" {
		sees = body["model"] == m.eyeModel
	}
	if len(pics) > 0 && !sees {
		m.refused++
		w.WriteHeader(400)
		_, _ = io.WriteString(w, "{\"error\":{\"message\":\"Failed to deserialize the JSON body: unknown variant `image_url`, expected `text`\"}}")
		return
	}
	reply := func(text string, calls []aiCall) {
		if m.wire == "claude" {
			blocks := []map[string]any{}
			if text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
			for _, c := range calls {
				var in any
				_ = json.Unmarshal(c.Args, &in)
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": in})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"role": "assistant", "content": blocks, "stop_reason": "end_turn"})
			return
		}
		msg := map[string]any{"role": "assistant", "content": text}
		if len(calls) > 0 {
			var cs []map[string]any
			for _, c := range calls {
				cs = append(cs, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Args)}})
			}
			msg["tool_calls"] = cs
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}})
	}
	if body["tools"] == nil {
		switch {
		case strings.Contains(text, "三位数"):
			m.probes++
			if len(pics) == 1 {
				reply("图里的数字是 "+readDigits(pics[0])+"。", nil)
			} else {
				reply("看不到", nil)
			}
		case strings.Contains(text, "截图"):
			m.describes = append(m.describes, fmt.Sprintf("%d|%s", len(pics), text))
			reply("- 整体：完整的人形，A 字站姿。\n- 衣服：水手服在身上，左肩有一小块身体穿出来。\n- 材质：没有洋红色。", nil)
		default:
			reply("收到", nil)
		}
		return
	}
	m.seen = append(m.seen, body)
	m.pictures = append(m.pictures, len(pics))
	i := len(m.seen) - 1
	if i >= len(m.script) {
		i = len(m.script) - 1
	}
	reply(m.script[i].text, m.script[i].calls)
}

// cameraUnity: sceneUnity with the plugin's snapshot command.
type cameraUnity struct {
	sceneUnity
	project string
	old     bool // a plugin from before 1.2.0
	shots   []map[string]any
}

func (u *cameraUnity) answer(cmd string, args map[string]any) (any, string) {
	if cmd != "snapshot" || u.old {
		return u.sceneUnity.answer(cmd, args)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, cmd)
	u.shots = append(u.shots, args)
	views := asStrings(args["views"])
	if len(views) == 0 {
		views = []string{"front"}
	}
	dir := filepath.Join(u.project, "UserSettings", "MioVRCA", "shots")
	_ = os.MkdirAll(dir, 0755)
	var shots []any
	for i, v := range views {
		f := filepath.Join(dir, fmt.Sprintf("t%d_%d_%s.jpg", len(u.shots), i, v))
		_ = os.WriteFile(f, testkit.JPEG(1400, 700, color.RGBA{200, uint8(40 * i), 90, 255}), 0644)
		shots = append(shots, map[string]any{"view": v, "file": filepath.ToSlash(f), "width": 1400, "height": 700})
	}
	return map[string]any{"avatar": "Kaguya_Test", "shots": shots, "visible": []string{"Body", "Envy cat"}, "hidden": []string{"Pants"}, "playing": false, "note": "编辑模式里的画面"}, ""
}

type visionRig struct {
	st    *core.Store
	proj  string
	unity *cameraUnity
	llm   *eyeLLM
	sess  *aiSession
}

func lookScript(n int) []mockTurn {
	var s []mockTurn
	for i := 0; i < n; i++ {
		s = append(s, mockTurn{calls: []aiCall{{ID: fmt.Sprintf("L%d", i+1), Name: "look", Args: json.RawMessage(`{"views":["front","back"],"hide":["Pants"],"question":"外套有没有穿模"}`)}}})
	}
	return append(s, mockTurn{text: "看过了。"})
}

func newVisionRig(t *testing.T, provider string, sees bool, script []mockTurn) *visionRig {
	t.Helper()
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &cameraUnity{project: proj}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	llm := &eyeLLM{wire: aiProvider(provider).Wire, sees: sees, script: script}
	srv := httptest.NewServer(llm)
	t.Cleanup(srv.Close)
	key := "main-key-12345678"
	if err := aiSave(provider, srv.URL, "main-model", &key, nil); err != nil {
		t.Fatal(err)
	}
	return &visionRig{st: st, proj: proj, unity: u, llm: llm, sess: aiSessionFor(proj)}
}

func (r *visionRig) run(t *testing.T, text string) []AIStep {
	t.Helper()
	if err := startAIRun(r.st, aiRunReq{Project: r.proj, Mode: "chat", Text: text}); err != nil {
		t.Fatal(err)
	}
	return waitRun(t, r.sess)
}

func lookStep(steps []AIStep) *AIStep {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Tool == "look" {
			return &steps[i]
		}
	}
	return nil
}

// A model that reads pictures gets them with the tool's answer, on either wire; the player sees them too.
func testLookMain(t *testing.T, provider string) {
	r := newVisionRig(t, provider, true, lookScript(1))
	steps := r.run(t, "看看穿得怎么样")
	ls := lookStep(steps)
	if ls == nil || !ls.OK || len(ls.Imgs) != 2 || !strings.Contains(ls.Out, "正面、背面") || !strings.Contains(ls.Out, "由当前 AI 模型直接识别") {
		t.Fatalf("look step %+v", ls)
	}
	// the window's pictures: saved, no larger than 1024, served by name only
	for _, n := range ls.Imgs {
		rec := httptest.NewRecorder()
		ServeShot(rec, httptest.NewRequest("GET", "/shot?f="+n, nil))
		img, err := jpeg.Decode(rec.Body)
		if rec.Code != 200 || err != nil || img.Bounds().Dx() != 1024 || img.Bounds().Dy() != 512 {
			t.Errorf("shot %s: %d %v", n, rec.Code, err)
		}
	}
	for _, bad := range []string{"../ai.json", "..%2Fai.json", "x.png", ""} {
		rec := httptest.NewRecorder()
		ServeShot(rec, httptest.NewRequest("GET", "/shot?f="+bad, nil))
		if rec.Code != 404 {
			t.Errorf("served %q", bad)
		}
	}
	// nothing is left in the project
	if left, _ := filepath.Glob(filepath.Join(r.proj, "UserSettings", "MioVRCA", "shots", "*.jpg")); len(left) != 0 {
		t.Errorf("left in the project: %v", left)
	}
	r.unity.mu.Lock()
	if len(r.unity.shots) != 1 || fmt.Sprint(r.unity.shots[0]["views"]) != "[front back]" || fmt.Sprint(r.unity.shots[0]["hide"]) != "[Pants]" {
		t.Errorf("snapshot args %v", r.unity.shots)
	}
	r.unity.mu.Unlock()
	r.llm.mu.Lock()
	defer r.llm.mu.Unlock()
	if r.llm.probes != 1 || len(r.llm.seen) != 2 || fmt.Sprint(r.llm.pictures) != "[0 2]" {
		t.Fatalf("probes %d, requests %d, pictures %v", r.llm.probes, len(r.llm.seen), r.llm.pictures)
	}
	b, _ := json.Marshal(r.llm.seen[1])
	js := string(b)
	if !strings.Contains(js, "截图附在后面") || !strings.Contains(js, `hidden\":[\"Pants\"]`) || strings.Contains(js, "截图已经显示给玩家") {
		t.Errorf("tool answer: %.600s", js)
	}
	if provider == "claude" {
		if !strings.Contains(js, `"tool_result"`) || !strings.Contains(js, `"media_type":"image/jpeg"`) {
			t.Errorf("claude wire: %.400s", js)
		}
	} else {
		// the tool's answer stays text; the pictures follow in a user message
		msgs := r.llm.seen[1]["messages"].([]any)
		last := msgs[len(msgs)-1].(map[string]any)
		tool := msgs[len(msgs)-2].(map[string]any)
		if _, ok := tool["content"].(string); !ok || tool["role"] != "tool" || last["role"] != "user" {
			t.Errorf("openai wire: %v / %v", tool["role"], last["role"])
		}
	}
	if loadAIConfig().Sees[seesKey(r.sess.client)] != "yes" || aiView().Looks != "main" {
		t.Errorf("not remembered: %v, looks %s", loadAIConfig().Sees, aiView().Looks)
	}
}

func TestLookMainOpenAI(t *testing.T) { testLookMain(t, "openai") }
func TestLookMainClaude(t *testing.T) { testLookMain(t, "claude") }

// The model is asked once whether it sees; older pictures leave the conversation.
func TestLookAgain(t *testing.T) {
	r := newVisionRig(t, "openai", true, lookScript(4))
	r.run(t, "多看几次")
	r.llm.mu.Lock()
	defer r.llm.mu.Unlock()
	if r.llm.probes != 1 {
		t.Errorf("%d probes", r.llm.probes)
	}
	// requests: before any look, then after 1, 2, 3, 4 looks — never more than the last two looks' pictures
	if fmt.Sprint(r.llm.pictures) != "[0 2 4 4 4]" {
		t.Errorf("pictures per request %v", r.llm.pictures)
	}
	b, _ := json.Marshal(r.llm.seen[4])
	if strings.Count(string(b), "已经从对话里撤下") != 2 {
		t.Error("the older looks do not say their pictures are gone")
	}
}

// DeepSeek and the like: no picture is sent after the first refusal, and the AI is told it saw nothing.
func TestLookBlind(t *testing.T) {
	r := newVisionRig(t, "openai", false, lookScript(2))
	if aiView().Looks != "unknown" {
		t.Errorf("looks %s before anything was asked", aiView().Looks)
	}
	steps := r.run(t, "看看")
	ls := lookStep(steps)
	if ls == nil || !ls.OK || len(ls.Imgs) != 2 || !strings.Contains(ls.Out, "仅显示在记录中") {
		t.Fatalf("look step %+v", ls)
	}
	r.llm.mu.Lock()
	if r.llm.refused != 1 || r.llm.probes != 0 || fmt.Sprint(r.llm.pictures) != "[0 0 0]" {
		t.Errorf("refused %d, probes %d, pictures %v", r.llm.refused, r.llm.probes, r.llm.pictures)
	}
	b, _ := json.Marshal(r.llm.seen[2])
	r.llm.mu.Unlock()
	if !strings.Contains(string(b), "看不了图") || !strings.Contains(string(b), "不要描述画面") || !strings.Contains(string(b), "视觉模型") {
		t.Errorf("the AI was not told: %.500s", b)
	}
	if loadAIConfig().Sees[seesKey(r.sess.client)] != "no" || aiView().Looks != "none" {
		t.Errorf("sees %v, looks %s", loadAIConfig().Sees, aiView().Looks)
	}
}

// A provider's own vision model is the eye of its model that cannot see: same address, same key, nothing to
// set up. (DeepSeek: deepseek-v4-pro works, deepseek-flash looks.)
func TestLookOwnEye(t *testing.T) {
	r := newVisionRig(t, "deepseek", false, lookScript(1))
	r.llm.eyeModel = "deepseek-flash"
	if v := aiView(); v.Looks != "unknown" || v.EyeModel != "deepseek-flash" || !v.EyeOwn {
		t.Errorf("view before: looks %s eye %q own %v", v.Looks, v.EyeModel, v.EyeOwn)
	}
	ls := lookStep(r.run(t, "看看"))
	if ls == nil || !ls.OK || len(ls.Imgs) != 2 || !strings.Contains(ls.Out, "由视觉模型（deepseek-flash）代为描述") {
		t.Fatalf("look step %+v", ls)
	}
	r.llm.mu.Lock()
	if len(r.llm.describes) != 1 || !strings.HasPrefix(r.llm.describes[0], "2|") || r.llm.refused != 1 || fmt.Sprint(r.llm.pictures) != "[0 0]" {
		t.Errorf("describes %v, refused %d, pictures %v", r.llm.describes, r.llm.refused, r.llm.pictures)
	}
	for _, a := range r.llm.auth {
		if !strings.Contains(a, "main-key") {
			t.Errorf("auth %q", a)
		}
	}
	b, _ := json.Marshal(r.llm.seen[1])
	r.llm.mu.Unlock()
	if !strings.Contains(string(b), "左肩有一小块身体穿出来") {
		t.Errorf("no description in the answer: %.400s", b)
	}
	if aiView().Looks != "other" {
		t.Errorf("looks %s", aiView().Looks)
	}
	// the eye model doing the work itself: it looks directly, nobody describes
	key := "main-key-12345678"
	_ = aiSave("deepseek", loadAIConfig().profile("deepseek").BaseURL, "deepseek-flash", &key, nil)
	r.sess.reset()
	r.llm.mu.Lock()
	r.llm.seen, r.llm.pictures, r.llm.describes = nil, nil, nil
	r.llm.mu.Unlock()
	if ls = lookStep(r.run(t, "再看看")); ls == nil || !strings.Contains(ls.Out, "由当前 AI 模型直接识别") {
		t.Fatalf("look step %+v", ls)
	}
	// a provider without a vision model of its own, or without a key: no eye
	c := loadAIConfig()
	c.Provider = "openai"
	if _, _, _, ok := eyeFor(c, map[string]string{"openai": "k"}); ok {
		t.Error("an eye for a service that has none")
	}
	c.Provider = "glm"
	if _, _, _, ok := eyeFor(c, map[string]string{}); ok {
		t.Error("an eye without a key")
	}
	if eye, key, own, ok := eyeFor(c, map[string]string{"glm": "gk"}); !ok || !own || key != "gk" || eye.Model != "glm-4.6v-flash" || eye.BaseURL != "https://open.bigmodel.cn/api/paas/v4" {
		t.Errorf("glm eye %+v %q %v %v", eye, key, own, ok)
	}
}

// The names DeepSeek retired in July 2026 are read as the model that answers now.
func TestRetiredModelNames(t *testing.T) {
	testkit.NewStore(t)
	key := "k-12345678"
	if err := aiSave("deepseek", "", "deepseek-chat", &key, nil); err != nil {
		t.Fatal(err)
	}
	if m := loadAIConfig().profile("deepseek").Model; m != "deepseek-flash" {
		t.Errorf("deepseek-chat read as %q", m)
	}
	// a relay at another address keeps whatever name it wants
	if err := aiSave("deepseek", "https://relay.example/v1", "deepseek-chat", &key, nil); err != nil {
		t.Fatal(err)
	}
	if m := loadAIConfig().profile("deepseek").Model; m != "deepseek-chat" {
		t.Errorf("a relay's model name was changed to %q", m)
	}
}

// A second service describes the pictures for a model that cannot see; each key goes to its own address.
func TestLookDescribed(t *testing.T) {
	r := newVisionRig(t, "deepseek", false, lookScript(1))
	eye := &eyeLLM{wire: "openai", sees: true}
	srv := httptest.NewServer(eye)
	defer srv.Close()
	vkey := "vision-key-87654321"
	if err := aiSaveVision(AIVision{BaseURL: srv.URL, Model: "glm-4v-flash"}, &vkey); err != nil {
		t.Fatal(err)
	}
	if v := aiView(); v.VisionKey != "…4321" || v.Vision.Wire != "openai" || v.Looks != "unknown" {
		t.Errorf("view %+v", v)
	}
	steps := r.run(t, "看看")
	ls := lookStep(steps)
	if ls == nil || !ls.OK || !strings.Contains(ls.Out, "由视觉模型（glm-4v-flash）代为描述") {
		t.Fatalf("look step %+v", ls)
	}
	eye.mu.Lock()
	if len(eye.describes) != 1 || !strings.HasPrefix(eye.describes[0], "2|") || !strings.Contains(eye.describes[0], "外套有没有穿模") || !strings.Contains(eye.describes[0], "正面、背面") {
		t.Errorf("describe requests %v", eye.describes)
	}
	for _, a := range eye.auth {
		if !strings.Contains(a, vkey) || strings.Contains(a, "main-key") {
			t.Errorf("vision auth %q", a)
		}
	}
	eye.mu.Unlock()
	r.llm.mu.Lock()
	for _, a := range r.llm.auth {
		if strings.Contains(a, vkey) {
			t.Errorf("the vision key went to the main service: %q", a)
		}
	}
	b, _ := json.Marshal(r.llm.seen[1])
	if fmt.Sprint(r.llm.pictures) != "[0 0]" || !strings.Contains(string(b), "左肩有一小块身体穿出来") || !strings.Contains(string(b), "视觉模型（glm-4v-flash）") {
		t.Errorf("pictures %v; answer %.500s", r.llm.pictures, b)
	}
	r.llm.mu.Unlock()
	if aiView().Looks != "other" {
		t.Errorf("looks %s", aiView().Looks)
	}
	// a saved key stays with its address
	if err := aiSaveVision(AIVision{BaseURL: "https://elsewhere.example/v1", Model: "m"}, nil); err == nil {
		t.Error("the vision key would have gone to another address")
	}
	// the vision service is down: the AI is told it saw nothing
	srv.Close()
	r.sess.reset()
	r.llm.mu.Lock()
	r.llm.seen, r.llm.pictures = nil, nil
	r.llm.mu.Unlock()
	steps = r.run(t, "再看看")
	if ls = lookStep(steps); ls == nil || !strings.Contains(ls.Out, "视觉模型未返回结果") {
		t.Fatalf("look step %+v", ls)
	}
	// off: nothing is sent anywhere
	if err := aiSaveVision(AIVision{Mode: "off", BaseURL: srv.URL, Model: "glm-4v-flash"}, nil); err != nil {
		t.Fatal(err)
	}
	r.sess.reset()
	r.llm.mu.Lock()
	r.llm.seen, r.llm.pictures = nil, nil
	r.llm.mu.Unlock()
	if ls = lookStep(r.run(t, "再看看")); ls == nil || !strings.Contains(ls.Out, "识图已关闭") || len(ls.Imgs) != 2 || aiView().Looks != "off" {
		t.Fatalf("look step %+v", ls)
	}
}

// 「主模型自己看」 with a model that cannot: the service refuses, and the conversation goes on without pictures.
func TestLookForcedRefused(t *testing.T) {
	r := newVisionRig(t, "openai", false, lookScript(1))
	if err := aiSaveVision(AIVision{Mode: "main"}, nil); err != nil {
		t.Fatal(err)
	}
	steps := r.run(t, "看看")
	last := steps[len(steps)-1]
	var told bool
	for _, s := range steps {
		told = told || (s.Kind == "error" && strings.Contains(s.Text, "不接受图片"))
	}
	if last.Kind != "say" || last.Text != "看过了。" || !told {
		t.Fatalf("steps %+v", steps)
	}
	r.llm.mu.Lock()
	defer r.llm.mu.Unlock()
	b, _ := json.Marshal(r.llm.seen[len(r.llm.seen)-1])
	if r.llm.refused != 1 || !strings.Contains(string(b), "截图没有送到") {
		t.Errorf("refused %d; %.400s", r.llm.refused, b)
	}
}

func TestLookOldPlugin(t *testing.T) {
	r := newVisionRig(t, "openai", true, lookScript(1))
	r.unity.old = true
	ls := lookStep(r.run(t, "看看"))
	if ls == nil || ls.OK || !strings.Contains(ls.Out, "旧版") || !strings.Contains(ls.Out, "更新") {
		t.Fatalf("look step %+v", ls)
	}
}

// The pipeline without an AI ends with pictures for the player.
func TestQuickPipelineShots(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &cameraUnity{project: proj}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Folders: []string{"Assets/Shop/SailorSet"}}); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	ls := lookStep(steps)
	if ls == nil || !ls.OK || len(ls.Imgs) != 2 || !strings.Contains(steps[len(steps)-1].Text, "截图") {
		t.Fatalf("steps %+v", steps)
	}
}

// UnitySkills' own screenshot skills: the picture is taken out of the answer and handed over as a picture.
func TestSkillScreenshot(t *testing.T) {
	r := newVisionRig(t, "openai", true, []mockTurn{
		{calls: []aiCall{{ID: "s1", Name: "unity_skill", Args: json.RawMessage(`{"name":"camera_sceneview_screenshot","args":{"filename":"x"}}`)}}},
		{calls: []aiCall{{ID: "s2", Name: "unity_skill", Args: json.RawMessage(`{"name":"camera_screenshot","args":{"savePath":"Assets/IKUSIA/kaguya/tex/body.png"}}`)}}},
		{calls: []aiCall{{ID: "s3", Name: "unity_skill", Args: json.RawMessage(`{"name":"gameobject_find","args":{"name":"Body"}}`)}}},
		{text: "看过了。"},
	})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var pngBuf bytes.Buffer
	big := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	rnd := rand.New(rand.NewSource(7))
	for i := range big.Pix {
		big.Pix[i] = uint8(rnd.Intn(256)) | 0x80
	}
	_ = png.Encode(&pngBuf, big)
	b64 := base64.StdEncoding.EncodeToString(pngBuf.Bytes())
	if len(b64) < 100000 {
		t.Fatalf("test picture too small: %d", len(b64))
	}
	var mu sync.Mutex
	got := map[string]map[string]any{}
	skills := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/skills" {
			_, _ = io.WriteString(w, `{"skills":[
			 {"name":"camera_sceneview_screenshot","operation":["Execute"],"readOnly":false,"mutatesAssets":true,"riskLevel":"low"},
			 {"name":"camera_screenshot","operation":["Execute"],"readOnly":false,"mutatesAssets":true,"riskLevel":"low"},
			 {"name":"gameobject_find","operation":["Query"],"readOnly":true,"riskLevel":"low"}]}`)
			return
		}
		var args map[string]any
		_ = json.NewDecoder(q.Body).Decode(&args)
		mu.Lock()
		got[strings.TrimPrefix(q.URL.Path, "/skill/")] = args
		mu.Unlock()
		switch q.URL.Path {
		case "/skill/gameobject_find":
			// a long, indented answer: compacted before it is cut
			var rows []string
			for i := 0; i < 700; i++ {
				rows = append(rows, fmt.Sprintf("    {\n        \"name\":   \"Bone_%03d\",\n        \"id\":     %d\n    }", i, i))
			}
			_, _ = io.WriteString(w, "{\n  \"status\": \"success\",\n  \"result\": [\n"+strings.Join(rows, ",\n")+"\n  ]\n}")
		default:
			_, _ = io.WriteString(w, `{"status":"success","result":{"path":"Assets/Screenshots/x.png","imageWidth":1600,"imageHeight":900,"imageBytes":`+fmt.Sprint(pngBuf.Len())+`,"imageBase64":"`+b64+`"}}`)
		}
	}))
	defer skills.Close()
	addr := skills.Listener.Addr().String()
	_ = os.MkdirAll(filepath.Join(home, ".unity_skills"), 0755)
	_ = os.WriteFile(filepath.Join(home, ".unity_skills", "registry.json"), []byte(fmt.Sprintf(`{%q:{"path":%q,"port":%s,"last_active":%d}}`, r.proj, r.proj, addr[strings.LastIndex(addr, ":")+1:], time.Now().Unix())), 0644)
	steps := r.run(t, "看看 Scene 视图")
	var shots int
	for _, s := range steps {
		if s.Kind == "ask" {
			t.Errorf("asked before a screenshot: %s", s.Text)
		}
		if s.Tool == "unity_skill" && len(s.Imgs) == 1 && s.OK && strings.Contains(s.Out, "由当前 AI 模型直接识别") {
			shots++
		}
	}
	if shots != 2 {
		t.Fatalf("steps %+v", steps)
	}
	mu.Lock()
	if got["camera_sceneview_screenshot"]["returnImage"] != true || got["camera_sceneview_screenshot"]["filename"] != "x" || got["camera_sceneview_screenshot"]["maxDimension"] == nil {
		t.Errorf("args %v", got["camera_sceneview_screenshot"])
	}
	if got["camera_screenshot"]["savePath"] != "Assets/Screenshots/body.png" { // never over a file of the project
		t.Errorf("camera args %v", got["camera_screenshot"])
	}
	mu.Unlock()
	r.llm.mu.Lock()
	defer r.llm.mu.Unlock()
	if fmt.Sprint(r.llm.pictures) != "[0 1 2 2]" {
		t.Errorf("pictures %v", r.llm.pictures)
	}
	b, _ := json.Marshal(r.llm.seen[3])
	js := string(b)
	if strings.Contains(js, b64[:200]) || !strings.Contains(js, "已从结果里取出") || !strings.Contains(js, `\"imageWidth\":1600`) {
		t.Errorf("the picture stayed in the text, or the rest of the answer was lost")
	}
	if !strings.Contains(js, "Bone_699") || strings.Contains(js, "太长") {
		t.Errorf("a 700-row answer did not fit after compacting")
	}
}

func TestTakeImages(t *testing.T) {
	proj := t.TempDir()
	// no picture in the answer: the file it names is read, once it is there
	_ = os.MkdirAll(filepath.Join(proj, "Assets", "Screenshots"), 0755)
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 64, 32)))
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(proj, "Assets", "Screenshots", "late.png"), pngBuf.Bytes(), 0644)
	}()
	raw, imgs := takeImages(proj, []byte(`{"status":"success","result":{"path":"Assets/Screenshots/late.png"}}`))
	if len(imgs) != 1 || imgs[0].Mime != "image/jpeg" || !strings.Contains(string(raw), "late.png") {
		t.Errorf("file: %d images", len(imgs))
	}
	// a path outside the project is not read
	out := filepath.Join(t.TempDir(), "secret.png")
	_ = os.WriteFile(out, pngBuf.Bytes(), 0644)
	for _, p := range []string{out, "../../" + filepath.Base(filepath.Dir(out)) + "/secret.png"} {
		j, _ := json.Marshal(map[string]any{"result": map[string]any{"path": p}})
		if _, imgs := takeImages(proj, j); len(imgs) != 0 {
			t.Errorf("read %s", p)
		}
	}
	if _, imgs := takeImages(proj, []byte(`not json`)); imgs != nil {
		t.Error("pictures out of nothing")
	}
	// a transparent PNG lands on the dark ground, not on black or white
	im, err := normImage(pngBuf.Bytes(), 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	j, _ := jpeg.Decode(bytes.NewReader(im.Data))
	if r, g, b, _ := j.At(5, 5).RGBA(); r>>8 < 0x2c || r>>8 > 0x3a || g>>8 < 0x30 || b>>8 < 0x3a {
		t.Errorf("ground %x %x %x", r>>8, g>>8, b>>8)
	}
	if _, err := normImage([]byte("nope"), 1024, ""); err == nil {
		t.Error("garbage read as a picture")
	}
}

func TestProbeImage(t *testing.T) {
	for i := 0; i < 20; i++ {
		im, digits := probeImage()
		if len(digits) != 3 || readDigits(im.Data) != digits {
			t.Fatalf("probe %q read back as %q", digits, readDigits(im.Data))
		}
	}
	// the answer counts only when the number is in it
	c := &aiClient{info: aiProviderInfo{Wire: "openai", Label: "x"}, model: "m"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"我看到一张图片，但无法识别其中的数字。"}}]}`)
	}))
	defer srv.Close()
	c.base, c.hc = srv.URL+"/v1", srv.Client()
	if sees, _, refused, err := aiSeesTest(context.Background(), c); sees || refused || err != nil {
		t.Errorf("a model that only says it sees: %v %v %v", sees, refused, err)
	}
}

// The vision model's key: its own, or the key of a provider at the same address — never another host's.
func TestVisionKeyReuse(t *testing.T) {
	testkit.NewStore(t)
	k := "glm-key-12345678"
	if err := aiSave("glm", "", "", &k, nil); err != nil {
		t.Fatal(err)
	}
	d := "deepseek-key-12345678"
	if err := aiSave("deepseek", "", "", &d, nil); err != nil {
		t.Fatal(err)
	}
	if err := aiSaveVision(AIVision{BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4.6v-flash"}, nil); err != nil {
		t.Fatal(err)
	}
	c, keys := loadAIConfig(), map[string]string{"glm": k, "deepseek": d}
	if got := visionKey(c, keys); got != k {
		t.Errorf("same address: %q", got)
	}
	if !c.Vision.otherReady(c, keys) {
		t.Error("not ready with the provider's key")
	}
	c.Vision.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	if got := visionKey(c, keys); got != "" {
		t.Errorf("a key went to another address: %q", got)
	}
	keys[visionKeyID] = "own"
	if got := visionKey(c, keys); got != "own" {
		t.Errorf("own key: %q", got)
	}
	// the new providers and their addresses
	for id, want := range map[string]string{"qwen": "https://dashscope.aliyuncs.com/compatible-mode/v1", "glm": "https://open.bigmodel.cn/api/paas/v4", "kimi": "https://api.moonshot.cn/v1"} {
		p := aiProvider(id)
		if p == nil || p.Wire != "openai" || normBase(p.Wire, p.Base) != want || p.Model == "" {
			t.Errorf("provider %s: %+v", id, p)
		}
	}
	if v := aiView(); len(v.Presets) != 4 || len(v.Providers) != 6 {
		t.Errorf("view: %d presets, %d providers", len(v.Presets), len(v.Providers))
	}
}
