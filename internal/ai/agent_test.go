package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// agentRig: a project with a fake Unity and a scripted AI service.
type agentRig struct {
	st   *core.Store
	proj string
	u    *sceneUnity
	llm  *mockLLM
	s    *aiSession
}

func newAgentRig(t *testing.T, provider string, u *sceneUnity, script []mockTurn) *agentRig {
	t.Helper()
	st := testkit.NewStore(t)
	proj := t.TempDir()
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	llm := &mockLLM{wire: aiProvider(provider).Wire, script: script}
	srv := httptest.NewServer(llm)
	t.Cleanup(srv.Close)
	key := "secret-key-12345678"
	if err := aiSave(provider, srv.URL, "test-model", &key, nil); err != nil {
		t.Fatal(err)
	}
	return &agentRig{st: st, proj: proj, u: u, llm: llm, s: aiSessionFor(proj)}
}

func (r *agentRig) chat(t *testing.T, text string) []AIStep {
	t.Helper()
	if err := startAIRun(r.st, aiRunReq{Project: r.proj, Mode: "chat", Text: text}); err != nil {
		t.Fatal(err)
	}
	return waitRun(t, r.s)
}

func (r *agentRig) requests() []string {
	r.llm.mu.Lock()
	defer r.llm.mu.Unlock()
	var out []string
	for _, b := range r.llm.seen {
		j, _ := json.Marshal(b)
		out = append(out, string(j))
	}
	return out
}

func quickRetries(t *testing.T) {
	old := aiRetryWait
	aiRetryWait = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	t.Cleanup(func() { aiRetryWait = old })
}

const cutArgs = `{"items":[{"kind":"outfit","path":["衣服"],"label":"A","obj`

// A tool call the output limit cut off is not carried out, its broken arguments are not sent back, and the
// second one in a row ends the run instead of going on for forty rounds.
func TestTruncatedToolCall(t *testing.T) {
	for _, provider := range []string{"kimi", "openai", "claude"} {
		args := json.RawMessage(cutArgs)
		if provider == "claude" { // that wire hands the arguments over parsed: what was written before the cut is lost
			args = json.RawMessage(`{}`)
		}
		cut := mockTurn{calls: []aiCall{{ID: "c", Name: "build_menu", Args: args}}, cut: true}
		r := newAgentRig(t, provider, &sceneUnity{}, []mockTurn{cut, cut, cut, {text: "不该走到这里"}})
		steps := r.chat(t, "做菜单")
		reqs := r.requests()
		if len(reqs) != 2 {
			t.Fatalf("%s: %d requests, want 2", provider, len(reqs))
		}
		last := steps[len(steps)-1]
		if last.Kind != "error" || !strings.Contains(last.Text, "连续两次") || !strings.Contains(last.Text, "截断") {
			t.Errorf("%s: last step %+v", provider, last)
		}
		cutSteps := 0
		for _, x := range steps {
			if x.Tool == "build_menu" && !x.OK && strings.Contains(x.Out, "截断") {
				cutSteps++
			}
		}
		if cutSteps != 2 {
			t.Errorf("%s: steps %+v", provider, steps)
		}
		if strings.Contains(reqs[1], `\"obj"`) || !strings.Contains(reqs[1], "输出上限处被截断") {
			t.Errorf("%s: the cut arguments went back, or the call got no answer", provider)
		}
		if provider != "claude" && !strings.Contains(reqs[1], `"arguments":"{}"`) {
			t.Errorf("%s: the cut arguments were not replaced", provider)
		}
		// every service is given an output limit
		r.llm.mu.Lock()
		if got := r.llm.seen[0]["max_tokens"]; got != float64(aiOutputMax) {
			t.Errorf("%s: max_tokens %v", provider, got)
		}
		r.llm.mu.Unlock()
		r.u.mu.Lock()
		if len(r.u.menus) != 0 {
			t.Errorf("%s: a cut call was carried out", provider)
		}
		r.u.mu.Unlock()
	}
}

// one cut call, then a shorter one: the run goes on; arguments that are not JSON without a cut are asked for again
func TestTruncatedThenShorter(t *testing.T) {
	menu := `{"items":[{"kind":"outfit","path":["衣服"],"label":"A","objects":["Envy cat"]}]}`
	r := newAgentRig(t, "openai", &sceneUnity{}, []mockTurn{
		{calls: []aiCall{{ID: "a", Name: "inspect_avatar", Args: json.RawMessage(`{}`)}, {ID: "b", Name: "build_menu", Args: json.RawMessage(cutArgs)}}, cut: true},
		{calls: []aiCall{{ID: "c", Name: "build_menu", Args: json.RawMessage(`{"items":[`)}}},
		{calls: []aiCall{{ID: "d", Name: "build_menu", Args: json.RawMessage(menu)}}},
		{text: "做好了。"},
	})
	steps := r.chat(t, "做菜单")
	var kinds []string
	for _, x := range steps {
		if x.Kind == "tool" {
			kinds = append(kinds, x.Tool+map[bool]string{true: "+", false: "-"}[x.OK])
		}
	}
	if got := strings.Join(kinds, " "); got != "inspect_avatar+ build_menu- build_menu- build_menu+" || steps[len(steps)-1].Kind != "say" {
		t.Fatalf("steps %s: %+v", got, steps)
	}
	reqs := r.requests()
	if len(reqs) != 4 || strings.Contains(reqs[3], `\"obj"`) || strings.Contains(reqs[3], `{\"items\":[","name"`) || strings.Count(reqs[3], `"arguments":"{}"`) != 3 || !strings.Contains(reqs[3], "不是合法的 JSON") {
		t.Errorf("%d requests; arguments that could not be read went back as they came", len(reqs))
	}
	// the call before the cut one was whole: it ran, and its answer went back
	if !strings.Contains(reqs[1], "Kaguya_Test") {
		t.Error("the complete call of a cut turn was not carried out")
	}
}

// a service that wants the output limit under another name, smaller, or not at all gets it that way
func TestOutputLimitAdapts(t *testing.T) {
	st := testkit.NewStore(t)
	var mu sync.Mutex
	var seen []map[string]any
	mode := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, body)
		refuse := func(msg string) {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg}})
		}
		switch {
		case mode == "renamed" && body["max_tokens"] != nil:
			refuse("Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.")
		case mode == "small" && body["max_tokens"] != nil && body["max_tokens"].(float64) > 4096:
			refuse("Range of max_tokens should be [1, 4096]")
		case mode == "none" && (body["max_tokens"] != nil || body["max_completion_tokens"] != nil):
			refuse("max_tokens is not allowed for this model")
		default:
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"收到"},"finish_reason":"stop"}]}`)
		}
	}))
	defer srv.Close()
	for m, want := range map[string]string{"renamed": "max_completion_tokens=8192", "small": "max_tokens=4096", "none": "", "": "max_tokens=8192"} {
		mu.Lock()
		mode, seen = m, nil
		mu.Unlock()
		c, err := aiClientWith(st, *aiProvider("openai"), srv.URL, "m", "k-1234567890")
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ { // what worked is kept: the second request goes through at once
			if _, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil); err != nil {
				t.Fatalf("%s: %v", m, err)
			}
		}
		mu.Lock()
		last := seen[len(seen)-1]
		got := ""
		for _, k := range []string{"max_tokens", "max_completion_tokens"} {
			if v, ok := last[k]; ok {
				got += fmt.Sprint(k, "=", v)
			}
		}
		if n := len(seen); got != want || (m == "" && n != 2) || (m == "renamed" && n != 3) || (m == "small" && n != 3) || (m == "none" && n != 4) {
			t.Errorf("%s: %d requests, the last with %q, want %q", m, n, got, want)
		}
		mu.Unlock()
	}
}

func TestContextOverflowText(t *testing.T) {
	for _, body := range []string{
		`{"error":{"message":"This model's maximum context length is 131072 tokens. However, you requested 140211 tokens (132019 in the messages, 8192 in the completion). Please reduce the length of the messages or completion.","type":"invalid_request_error"}}`,
		`{"error":{"message":"<400> InternalError.Algo.InvalidParameter: Range of input length should be [1, 129024]","code":"invalid_parameter_error"}}`,
		`{"error":{"code":"context_length_exceeded","message":"context_length_exceeded"}}`,
		`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 210000 tokens > 200000 maximum"}}`,
		`{"error":{"message":"Invalid request: Your request exceeded model token limit: 131072"}}`,
	} {
		got := aiErrText("X", 400, []byte(body))
		if !strings.Contains(got, "已超出模型的上下文长度") || strings.Contains(got, "模型名称不正确") {
			t.Errorf("%s\n→ %s", body, got)
		}
		he := &aiHTTPError{Status: 400, Said: strings.ToLower(aiSaid([]byte(body)))}
		if !he.overflow() || he.noPictures() || he.busy() {
			t.Errorf("kinds of %s: %v %v %v", body, he.overflow(), he.noPictures(), he.busy())
		}
	}
	// other refusals keep their words
	if got := aiErrText("X", 400, []byte(`{"error":{"message":"tools is not supported"}}`)); !strings.Contains(got, "不支持工具调用") {
		t.Errorf("plain 400: %s", got)
	}
	for said, want := range map[string]bool{"unknown variant `image_url`, expected `text`": true, "This model does not support image input": true, "messages[1].content: invalid type: sequence, expected a string": true,
		"该模型不支持图片输入": true, "tools is not supported": false, "invalid temperature": false} {
		if got := (&aiHTTPError{Status: 400, Said: strings.ToLower(said)}).noPictures(); got != want {
			t.Errorf("noPictures(%q) = %v", said, got)
		}
	}
	for status, want := range map[int]bool{429: true, 500: true, 502: true, 503: true, 529: true, 408: true, 400: false, 401: false, 404: false} {
		if got := (&aiHTTPError{Status: status}).busy(); got != want {
			t.Errorf("busy(%d) = %v", status, got)
		}
	}
	if (&aiHTTPError{Status: 429, Said: "insufficient balance"}).busy() {
		t.Error("a 429 about money is asked again")
	}
}

// a long session keeps working: older tool answers give way before the model's context is full, and a refusal
// for length is answered by leaving out more and asking once again
func TestHistoryShrinks(t *testing.T) {
	oldSoft := aiHistorySoft
	t.Cleanup(func() { aiHistorySoft = oldSoft })
	var script []mockTurn
	for i := 0; i < 6; i++ {
		script = append(script, mockTurn{calls: []aiCall{{ID: fmt.Sprint("p", i), Name: "list_prefabs", Args: json.RawMessage(`{"folders":["Assets/Shop/SailorSet"]}`)}}})
	}
	script = append(script, mockTurn{text: "看完了。"})
	// 1. before it gets too long: with a low mark, only the newest three answers stay whole
	aiHistorySoft = 8000
	r := newAgentRig(t, "openai", &sceneUnity{}, script)
	steps := r.chat(t, "多看几遍")
	if last := steps[len(steps)-1]; last.Kind != "say" {
		t.Fatalf("steps %+v", steps)
	}
	reqs := r.requests()
	final := reqs[len(reqs)-1]
	if n := strings.Count(final, agedMark); n != 3 {
		t.Errorf("%d answers left out, want 3", n)
	}
	if n := strings.Count(final, "Kaguya_full.prefab"); n != 3 {
		t.Errorf("%d answers whole, want 3", n)
	}
	if !strings.Contains(final, "p0") || !strings.Contains(final, "p5") {
		t.Error("a call lost its answer")
	}
	// 2. the service refuses for length: more is left out and the request goes through
	aiHistorySoft = oldSoft
	r = newAgentRig(t, "openai", &sceneUnity{}, script)
	turn := len(reqs[1]) - len(reqs[0]) // what one call and its answer add to a request
	r.llm.mu.Lock()
	r.llm.limit = len(reqs[0]) + turn*4 - 100 // room for three answers, not for four
	r.llm.mu.Unlock()
	steps = r.chat(t, "多看几遍")
	if last := steps[len(steps)-1]; last.Kind != "say" {
		t.Fatalf("after a refusal for length: %s", last.Text)
	}
	r.llm.mu.Lock()
	if r.llm.refused != 1 || len(r.llm.seen) != 7 {
		t.Errorf("refused %d, answered %d", r.llm.refused, len(r.llm.seen))
	}
	r.llm.mu.Unlock()
	// 3. nothing more to leave out: said as what it is
	r = newAgentRig(t, "openai", &sceneUnity{}, script)
	r.llm.mu.Lock()
	r.llm.limit = 2000
	r.llm.mu.Unlock()
	steps = r.chat(t, "多看几遍")
	last := steps[len(steps)-1]
	if last.Kind != "error" || !strings.Contains(last.Text, "上下文长度") || !strings.Contains(last.Text, "新对话") || strings.Contains(last.Text, "模型名称不正确") {
		t.Errorf("overflow that cannot be helped: %+v", last)
	}
}

func TestAgeResults(t *testing.T) {
	long := strings.Repeat("x", 5000)
	s := &aiSession{}
	for i := 0; i < 5; i++ {
		s.turns = append(s.turns, aiTurn{Role: "assistant", Calls: []aiCall{{ID: fmt.Sprint(i), Name: "inspect_avatar"}}},
			aiTurn{Role: "tool", Results: []aiResult{{ID: fmt.Sprint(i), Name: "inspect_avatar", Content: long, Images: []aiImage{{Mime: "image/jpeg"}}}, {ID: "e", Name: "dress", Content: "错误：短的留着", IsErr: true}}})
	}
	if s.ageResults(false) {
		t.Error("a short conversation was touched")
	}
	old := aiHistorySoft
	aiHistorySoft = 16000
	defer func() { aiHistorySoft = old }()
	if !s.ageResults(false) || s.ageResults(false) {
		t.Error("aging is not done in one pass")
	}
	for i, tn := range s.turns {
		if tn.Role != "tool" {
			continue
		}
		r := tn.Results[0]
		if aged := strings.HasPrefix(r.Content, agedMark); aged != (i < 4) || (aged && (len(r.Images) != 0 || !strings.Contains(r.Content, "inspect_avatar"))) {
			t.Errorf("turn %d: %q (%d pictures)", i, r.Content[:20], len(r.Images))
		}
		if tn.Results[1].Content != "错误：短的留着" {
			t.Errorf("turn %d: a short answer was replaced", i)
		}
	}
	if !s.ageResults(true) {
		t.Error("nothing more left out when the service refused")
	}
	n := 0
	for _, tn := range s.turns {
		if tn.Role == "tool" && !strings.HasPrefix(tn.Results[0].Content, agedMark) {
			n++
		}
	}
	if n != 1 || s.soft == 0 {
		t.Errorf("%d answers whole after a refusal, want 1 (mark %d)", n, s.soft)
	}
	// three whole answers are still more than the mark: fewer stay
	s = &aiSession{}
	for i := 0; i < 5; i++ {
		s.turns = append(s.turns, aiTurn{Role: "tool", Results: []aiResult{{ID: fmt.Sprint(i), Name: "inspect_avatar", Content: long}}})
	}
	aiHistorySoft = 11000
	s.ageResults(false)
	if n = strings.Count(fmt.Sprint(s.turns), long); n != 2 {
		t.Errorf("%d answers whole under a low mark, want 2", n)
	}
}

// 429 and 5xx are asked again after a pause, with a line in the step list; a service that stays down ends the run
func TestRetryBusyService(t *testing.T) {
	quickRetries(t)
	r := newAgentRig(t, "openai", &sceneUnity{}, []mockTurn{
		{status: 429, said: "Rate limit reached", after: "0"},
		{status: 503, said: "overloaded"},
		{text: "好的。"},
	})
	steps := r.chat(t, "你好")
	var lines []string
	for _, x := range steps {
		if x.Tool == "retry" {
			lines = append(lines, x.Text+"|"+x.Out)
		}
	}
	if steps[len(steps)-1].Kind != "say" || len(r.requests()) != 3 || strings.Join(lines, " ") != "服务繁忙，正在重试（2/3）|仍未成功 服务繁忙，正在重试（3/3）|已恢复" {
		t.Errorf("retried run: %v %+v", lines, steps)
	}
	// down for good: three tries, then the service's words
	r = newAgentRig(t, "claude", &sceneUnity{}, []mockTurn{{status: 529, said: "Overloaded"}})
	steps = r.chat(t, "你好")
	if last := steps[len(steps)-1]; last.Kind != "error" || !strings.Contains(last.Text, "Overloaded") || len(r.requests()) != 3 {
		t.Errorf("down for good: %d requests, %+v", len(r.requests()), last)
	}
	// not worth another try: no money, a wrong key
	for _, turn := range []mockTurn{{status: 429, said: "Insufficient Balance"}, {status: 401, said: "bad key"}, {status: 400, said: "tools is not supported"}} {
		r = newAgentRig(t, "openai", &sceneUnity{}, []mockTurn{turn})
		steps = r.chat(t, "你好")
		if last := steps[len(steps)-1]; last.Kind != "error" || len(r.requests()) != 1 {
			t.Errorf("%d: %d requests, %+v", turn.status, len(r.requests()), last)
		}
	}
	// a test of the connection is sent once: the player is waiting for the answer
	r = newAgentRig(t, "openai", &sceneUnity{}, []mockTurn{{status: 500, said: "boom"}})
	c, _ := newAIClient(r.st)
	if _, err := aiTest(context.Background(), c); err == nil || len(r.requests()) != 1 {
		t.Errorf("connection test: %v, %d requests", err, len(r.requests()))
	}
	if d := retryAfter("7"); d != 7*time.Second || retryAfter("") != -1 || retryAfter("soon") != -1 {
		t.Errorf("retryAfter %v", d)
	}
	if d := retryAfter(time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)); d < 80*time.Second || d > 91*time.Second {
		t.Errorf("retryAfter of a date: %v", d)
	}
}

// pictures stay in the conversation when a request is refused for another reason
func TestPicturesKeptOnOtherRefusal(t *testing.T) {
	r := newAgentRig(t, "openai", &sceneUnity{}, []mockTurn{{status: 400, said: "invalid temperature"}})
	r.s.mu.Lock()
	r.s.turns = []aiTurn{{Role: "user", Text: "看看"}, {Role: "assistant", Calls: []aiCall{{ID: "l", Name: "look", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", Results: []aiResult{{ID: "l", Name: "look", Content: "截图附在后面", Images: []aiImage{{Mime: "image/jpeg", Data: []byte("jpg")}}}}}, {Role: "assistant", Text: "看过了。"}}
	r.s.mu.Unlock()
	steps := r.chat(t, "再说一遍")
	if last := steps[len(steps)-1]; last.Kind != "error" || !strings.Contains(last.Text, "invalid temperature") || len(r.requests()) != 1 {
		t.Errorf("%d requests, %+v", len(r.requests()), last)
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if len(r.s.turns) != 4 || len(r.s.turns[2].Results[0].Images) != 1 {
		t.Errorf("the pictures were taken out (%d turns)", len(r.s.turns))
	}
}

// the pipeline without an AI, run twice on the same folder: the outfit that is on the avatar is not told to hide
func TestPipelineRerun(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{live: true}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	s := aiSessionFor(proj)
	for run := 1; run <= 2; run++ {
		if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Folders: []string{"Assets/Shop/SailorSet"}}); err != nil {
			t.Fatal(err)
		}
		steps := waitRun(t, s)
		if last := steps[len(steps)-1]; last.Kind != "say" || !strings.Contains(last.Text, "已装配") {
			t.Fatalf("run %d: %+v", run, last)
		}
		u.mu.Lock()
		d, m := u.dressed[len(u.dressed)-1], u.menus[len(u.menus)-1]
		first := m["items"].([]any)[0].(map[string]any)
		active, said := d["active"]
		if run == 1 && (active != true || first["default"] != true) {
			t.Errorf("first run: active=%v default=%v", active, first["default"])
		}
		if run == 2 && (said || first["default"] == true) {
			t.Errorf("second run: active=%v (sent: %v) default=%v", active, said, first["default"])
		}
		u.mu.Unlock()
		// the first run changed the scene twice (the outfit, the menu); the second found everything in place
		if _, _, changes := s.snapshot(); changes != 2 {
			t.Errorf("run %d: %d changes counted", run, changes)
		}
		if run == 2 {
			var outs []string
			for _, x := range steps[len(steps)-6:] {
				outs = append(outs, x.Out)
			}
			if all := strings.Join(outs, "|"); !strings.Contains(all, "已在模型上") || !strings.Contains(all, "未作改动") {
				t.Errorf("second run's steps: %s", all)
			}
			if txt := steps[len(steps)-1].Text; !strings.Contains(txt, "模型已有衣服菜单") || strings.Contains(txt, "头发已加入") {
				t.Errorf("second run's summary: %s", txt)
			}
		}
	}
}

// two prefabs with one file name (an outfit in a folder per base body)
func TestPipelineSameNamePrefabs(t *testing.T) {
	meshes := []string{"Dress", "Skirt", "Shoes"}
	twins := []prefabInfo{
		{Path: "Assets/Shop/Twin/BodyA/Dress.prefab", Name: "Dress", MAReady: true, Renderers: 3, MeshNames: meshes, HasArmature: true},
		{Path: "Assets/Shop/Twin/BodyB/Dress.prefab", Name: "Dress", MAReady: true, Renderers: 3, MeshNames: meshes, HasArmature: true},
	}
	run := func(known bool) (*sceneUnity, string) {
		st := testkit.NewStore(t)
		if !known {
			st.Settings.Bases = []string{"Other=other"}
		}
		proj := t.TempDir()
		u := &sceneUnity{live: true, extra: twins}
		unitytest.FakeUnity(t, proj, u.answer)
		st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
		if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Folders: []string{"Assets/Shop/Twin"}}); err != nil {
			t.Fatal(err)
		}
		steps := waitRun(t, aiSessionFor(proj))
		return u, steps[len(steps)-1].Text
	}
	rows := func(u *sceneUnity) []string {
		var out []string
		for _, m := range u.menus {
			for _, it := range m["items"].([]any) {
				o := it.(map[string]any)
				out = append(out, fmt.Sprint(o["kind"], " ", strings.Join(asStrings(o["path"]), "/"), ":", o["label"], " ", asStrings(o["objects"])))
			}
		}
		return out
	}
	// the base body is known and neither folder is named after it: one version is put on, and the text says which
	u, text := run(true)
	u.mu.Lock()
	if len(u.dressed) != 1 || !strings.HasSuffix(fmt.Sprint(u.dressed[0]["prefab"]), "BodyA/Dress.prefab") {
		t.Errorf("dressed %v", u.dressed)
	}
	u.mu.Unlock()
	for _, w := range []string{"多个素体版本", "BodyA、BodyB", "已装配 BodyA 版本", "Dress（BodyB 版本）"} {
		if !strings.Contains(text, w) {
			t.Errorf("summary lacks %q: %s", w, text)
		}
	}
	// the base body is not known: both are put on, each under a name and with menu items of its own
	u, text = run(false)
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.dressed) != 2 {
		t.Fatalf("dressed %v", u.dressed)
	}
	got := strings.Join(rows(u), "\n")
	want := "outfit 衣服/Dress:穿上 [Dress]\npart 衣服/Dress:裙子 [Dress/Skirt]\npart 衣服/Dress:鞋子 [Dress/Shoes]\n" +
		"outfit 衣服/Dress_BodyB:穿上 [Dress_BodyB]\npart 衣服/Dress_BodyB:裙子 [Dress_BodyB/Skirt]\npart 衣服/Dress_BodyB:鞋子 [Dress_BodyB/Shoes]\nstrip 衣服:一键脱光 []"
	if got != want {
		t.Errorf("menu rows:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(text, "已命名为「Dress_BodyB」") || !strings.Contains(text, "已装配：Dress、Dress_BodyB") {
		t.Errorf("summary: %s", text)
	}
}

func TestVersionFolder(t *testing.T) {
	for _, c := range [][3]string{
		{"Assets/Shop/BodyB/Dress.prefab", "Assets/Shop/BodyA/Dress.prefab", "BodyB"},
		{"Assets/Shop/BodyB/Prefab/Dress.prefab", "Assets/Shop/BodyA/Prefab/Dress.prefab", "BodyB"},
		{"Assets/A/Dress.prefab", "Assets/A/Dress.prefab", "A"},
	} {
		if got := versionFolder(c[0], c[1]); got != c[2] {
			t.Errorf("versionFolder(%s, %s) = %q", c[0], c[1], got)
		}
	}
}

// hair goes under the avatar's hair menu and outfits under its outfit menu, when it has one of each
func TestPipelineMenuRoots(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	base := &sceneUnity{extra: []prefabInfo{{Path: "Assets/_头发/耳发/Blue.prefab", Name: "Blue", Renderers: 1, MeshNames: []string{"Hair_ear"}, MAReady: true},
		{Path: "Assets/_配饰/Glasses/glasses.prefab", Name: "glasses", Renderers: 1, MeshNames: []string{"Glasses"}}}, bits: 300, bones: 280}
	tg := func(root, label, param string, def bool) map[string]any {
		return map[string]any{"object": root + "/" + label, "label": label, "type": "Toggle", "parameter": param, "default": def, "toggles": []any{label + "=on"}}
	}
	answer := func(cmd string, args map[string]any) (any, string) {
		if cmd == "inspect" {
			return map[string]any{"avatars": []any{map[string]any{"name": "Kaguya_Test", "path": "Kaguya_Test", "active": true, "prefab": "Assets/IKUSIA/kaguya/kaguya.prefab",
				"children": []any{map[string]any{"name": "Body", "kind": "mesh", "active": true}},
				"maMenu": []any{
					map[string]any{"object": "Menu_Outfits", "type": "MenuInstaller", "children": []any{tg("Menu_Outfits", "A", "cloth_choose", true), tg("Menu_Outfits", "B", "cloth_choose", false)}},
					map[string]any{"object": "Menu_Hair", "type": "MenuInstaller", "children": []any{tg("Menu_Hair", "H1", "Hair_Choose", true), tg("Menu_Hair", "H2", "Hair_Choose", false)}},
				}}}}, ""
		}
		res, e := base.answer(cmd, args)
		if m, ok := res.(map[string]any); ok && cmd == "build_menu" { // a plugin that reports the numbers but no warning
			m["parameterBits"] = 300
		}
		if m, ok := res.(map[string]any); ok && cmd == "dress" {
			m["physBones"] = 280
		}
		return res, e
	}
	unitytest.FakeUnity(t, proj, answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	req := aiRunReq{Project: proj, Mode: "dress", NoAI: true, Assets: []aiAsset{
		{Folder: "Assets/Shop/SailorSet", Kind: "衣服", Name: "水手服"}, {Folder: "Assets/_头发/耳发", Kind: "头发", Name: "蓝耳发"}, {Folder: "Assets/_配饰/Glasses", Kind: "配饰", Name: "眼镜"}}}
	if err := startAIRun(st, req); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	last := steps[len(steps)-1]
	if last.Kind != "say" {
		t.Fatalf("steps %+v", steps)
	}
	base.mu.Lock()
	defer base.mu.Unlock()
	if len(base.menus) != 2 {
		t.Fatalf("%d plans", len(base.menus))
	}
	for i, want := range []struct{ root, kinds string }{{"Menu_Outfits", "outfit outfit outfit part part part part part part part strip toggle"}, {"Menu_Hair", "outfit"}} {
		var kinds []string
		for _, it := range base.menus[i]["items"].([]any) {
			kinds = append(kinds, fmt.Sprint(it.(map[string]any)["kind"]))
		}
		if base.menus[i]["root"] != want.root || strings.Join(kinds, " ") != want.kinds {
			t.Errorf("plan %d: root %v, %v", i, base.menus[i]["root"], kinds)
		}
	}
	if hair := base.menus[1]["items"].([]any)[0].(map[string]any); hair["parameter"] != "Hair_Choose" || hair["default"] == true {
		t.Errorf("hair item %v", hair)
	}
	if first := base.menus[0]["items"].([]any)[0].(map[string]any); first["parameter"] != "cloth_choose" || first["default"] == true || base.menus[0]["parameter"] != "cloth_choose" {
		t.Errorf("outfit item %v", first)
	}
	for _, w := range []string{"「Menu_Outfits」「Menu_Hair」", "模型已有衣服菜单", "头发已加入原有的头发菜单", "同步参数预计 300 / 256 位", "共有 280 个 PhysBone"} {
		if !strings.Contains(last.Text, w) {
			t.Errorf("summary lacks %q: %s", w, last.Text)
		}
	}
}

// a run that ends half way says what is on the avatar already and how to take it back
func TestAbortSaysWhatIsDone(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{live: true, menuErr: "「衣服」已是开关而非子菜单，无法在其中添加菜单项"}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Folders: []string{"Assets/Shop/SailorSet"}}); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	last := steps[len(steps)-1]
	for _, w := range []string{"已装配 1 件（SailorSet）", "菜单尚未生成", "「撤销上一步」", "Ctrl+Z", "中断原因：「衣服」已是开关而非子菜单"} {
		if last.Kind != "error" || !strings.Contains(last.Text, w) {
			t.Errorf("abort text lacks %q: %+v", w, last)
		}
	}
	// nothing was done yet: the reason alone
	u.mu.Lock()
	u.dressErr = map[string]string{"Assets/Shop/SailorSet/Kaguya/Kaguya prefab/Envy cat.prefab": "工程中未找到该 prefab"}
	u.objects = nil
	u.mu.Unlock()
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", NoAI: true, Folders: []string{"Assets/Shop/SailorSet"}}); err != nil {
		t.Fatal(err)
	}
	steps = waitRun(t, aiSessionFor(proj))
	if last := steps[len(steps)-1]; last.Kind != "error" || last.Text != "工程中未找到该 prefab" {
		t.Errorf("nothing done: %+v", last)
	}
	// the AI's run, stopped by a tool call cut off twice after it dressed something
	cut := mockTurn{calls: []aiCall{{ID: "c", Name: "build_menu", Args: json.RawMessage(cutArgs)}}, cut: true}
	r := newAgentRig(t, "openai", &sceneUnity{live: true}, []mockTurn{
		{calls: []aiCall{{ID: "d", Name: "dress", Args: json.RawMessage(`{"prefab":"Assets/Shop/SailorSet/Kaguya/Kaguya prefab/Envy cat.prefab"}`)}}}, cut, cut})
	steps = r.chat(t, "穿上并做菜单")
	if last := steps[len(steps)-1]; last.Kind != "error" || !strings.Contains(last.Text, "已装配 1 件（Envy cat）") || !strings.Contains(last.Text, "中断原因：AI 的工具调用连续两次") {
		t.Errorf("AI run: %+v", last)
	}
}

// 「撤销上一步」 counts only steps that changed the scene, and tells the plugin whose step is on top
func TestUndoCountsRealChanges(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	u := &sceneUnity{live: true}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	skills := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/skills" {
			_, _ = io.WriteString(w, `{"skills":[
			 {"name":"gameobject_find","operation":["Query"],"readOnly":true,"riskLevel":"low"},
			 {"name":"gameobject_set_active","operation":["Modify"],"readOnly":false,"mutatesScene":true,"riskLevel":"low"},
			 {"name":"texture_set_size","operation":["Modify"],"readOnly":false,"mutatesAssets":true,"riskLevel":"low"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"success","result":{}}`)
	}))
	defer skills.Close()
	addr := skills.Listener.Addr().String()
	_ = os.MkdirAll(filepath.Join(home, ".unity_skills"), 0755)
	_ = os.WriteFile(filepath.Join(home, ".unity_skills", "registry.json"), []byte(fmt.Sprintf(`{%q:{"path":%q,"port":%s,"last_active":%d}}`, proj, proj, addr[strings.LastIndex(addr, ":")+1:], time.Now().Unix())), 0644)
	s := aiSessionFor(proj)
	s.mu.Lock()
	s.allowAll = true
	s.mu.Unlock()
	ctx := context.Background()
	changes := func() int { _, _, n := s.snapshot(); return n }
	call := func(name, args string, want int) {
		t.Helper()
		var a map[string]any
		_ = json.Unmarshal([]byte(args), &a)
		if _, short, ok := s.runTool(ctx, name, a); !ok {
			t.Fatalf("%s: %s", name, short)
		}
		if got := changes(); got != want {
			t.Errorf("after %s %s: %d changes, want %d", name, args, got, want)
		}
	}
	dress := `{"prefab":"Assets/Shop/SailorSet/Kaguya/Kaguya prefab/Envy cat.prefab"}`
	menu := `{"items":[{"kind":"outfit","path":["衣服"],"label":"A","objects":["Envy cat"]}]}`
	call("inspect_avatar", `{}`, 0)
	call("dress", dress, 1)
	call("dress", dress, 1) // on the avatar already
	call("build_menu", menu, 2)
	call("build_menu", menu, 2) // nothing to change
	call("unity_skill", `{"name":"gameobject_find","args":{"name":"Body"}}`, 2)
	call("unity_skill", `{"name":"texture_set_size","args":{}}`, 2) // a file of the project: not something Ctrl+Z takes back
	call("unity_skill", `{"name":"gameobject_set_active","args":{"name":"Body","active":false}}`, 3)
	// the last change was UnitySkills': the plugin is told its steps count; then the plugin's own
	call("undo", `{}`, 2)
	call("undo", `{}`, 1)
	u.mu.Lock()
	if len(u.undos) != 2 || u.undos[0]["skills"] != true || u.undos[1]["skills"] != false {
		t.Errorf("undo requests %v", u.undos)
	}
	// something else is on top of Unity's history: said, and the count stays
	u.undoErr = "Unity 撤销记录中最近的一步是「Move Body」，不是 MioVRCA 的操作。为避免撤销您自己的改动，未执行撤销"
	u.mu.Unlock()
	if _, short, ok := s.runTool(ctx, "undo", map[string]any{}); ok || !strings.Contains(short, "Move Body") || changes() != 1 {
		t.Errorf("refused undo: %v %q, %d changes", ok, short, changes())
	}
}

// the overview goes to the AI with what matters first, and is cut by leaving out entries: always valid JSON
func TestInspectForAI(t *testing.T) {
	var kids, menu []any
	for i := 0; i < 400; i++ {
		kids = append(kids, map[string]any{"name": fmt.Sprintf("Object_%03d", i), "kind": "outfit", "active": true, "components": []string{"MA Merge Armature", "MA Mesh Settings"}, "prefab": "Assets/Shop/Some/Long/Path/Object.prefab"})
	}
	for i := 0; i < 30; i++ {
		menu = append(menu, map[string]any{"object": fmt.Sprint("Avatar Menu/Item", i), "label": fmt.Sprint("Item", i), "type": "Toggle", "toggles": []string{"A=on", "B=off"}})
	}
	raw, _ := json.Marshal(map[string]any{"scenes": []any{map[string]any{"name": "S"}}, "modularAvatar": "1.13.0", "vrcSdk": true, "avatars": []any{map[string]any{
		"children": kids, "name": "Kaguya", "path": "Kaguya", "descriptorMenu": []any{map[string]any{"name": "Emotes"}}, "maMenu": menu,
		"physBones": 120, "parameterBits": 201, "parameterBitsAsset": 180, "someNewField": 1}}})
	if len(raw) < 60000 {
		t.Fatalf("test overview too small: %d", len(raw))
	}
	out := inspectForAI(raw, 40000)
	var back struct {
		Avatars []struct {
			Name     string `json:"name"`
			Bits     int    `json:"parameterBits"`
			Asset    int    `json:"parameterBitsAsset"`
			Bones    int    `json:"physBones"`
			MaMenu   []any  `json:"maMenu"`
			Children []any  `json:"children"`
			Omitted  struct {
				Total  int `json:"total"`
				Listed int `json:"listed"`
			} `json:"childrenOmitted"`
			New int `json:"someNewField"`
		} `json:"avatars"`
	}
	if len(out) > 40000 || json.Unmarshal([]byte(out), &back) != nil || len(back.Avatars) != 1 {
		t.Fatalf("cut overview is not JSON (%d bytes)", len(out))
	}
	a := back.Avatars[0]
	if a.Name != "Kaguya" || a.Bits != 201 || a.Asset != 180 || a.Bones != 120 || len(a.MaMenu) != 30 || a.New != 1 {
		t.Errorf("what matters was lost: %+v", a)
	}
	if len(a.Children) == 0 || len(a.Children) >= 400 || a.Omitted.Total != 400 || a.Omitted.Listed != len(a.Children) {
		t.Errorf("children %d, omitted %+v", len(a.Children), a.Omitted)
	}
	order := []string{`"name"`, `"parameterBits"`, `"physBones"`, `"maMenu"`, `"descriptorMenu"`, `"children"`}
	for i := 1; i < len(order); i++ {
		if strings.Index(out, order[i-1]) > strings.Index(out, order[i]) {
			t.Errorf("%s comes after %s", order[i-1], order[i])
		}
	}
	// a small one goes through whole; something that is not an overview is cut the old way
	small := `{"avatars":[{"children":[{"name":"Body"}],"name":"A","maMenu":[]}],"vrcSdk":true}`
	if got := inspectForAI(json.RawMessage(small), 40000); got != `{"vrcSdk":true,"avatars":[{"name":"A","maMenu":[],"children":[{"name":"Body"}]}]}` {
		t.Errorf("small overview: %s", got)
	}
	if got := inspectForAI(json.RawMessage(`[1,2,3]`), 40000); got != "[1,2,3]" {
		t.Errorf("not an overview: %s", got)
	}
	// a menu too long for the limit is shortened as well, and the result still reads
	var big []any
	for i := 0; i < 2000; i++ {
		big = append(big, map[string]any{"object": fmt.Sprint("Avatar Menu/Item", i), "label": fmt.Sprint("Item", i), "type": "Toggle"})
	}
	raw, _ = json.Marshal(map[string]any{"avatars": []any{map[string]any{"name": "K", "maMenu": big, "children": kids}}})
	out = inspectForAI(raw, 40000)
	var any map[string]any
	if len(out) > 40000 || json.Unmarshal([]byte(out), &any) != nil || !strings.Contains(out, "maMenuOmitted") {
		t.Errorf("huge menu: %d bytes", len(out))
	}
}
