package ai

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

func waitEval(t *testing.T) evalRun {
	t.Helper()
	for i := 0; i < 500; i++ {
		v := evalView()
		if runs := v["runs"].([]evalRun); len(runs) > 0 && !runs[0].Running {
			return runs[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the evaluation did not end")
	return evalRun{}
}

// A run of the line keeps its request for a plan as a sample; the samples are asked again of the model set up
// now, and what came of each is told: held as it came, after a correction, not at all.
func TestEvalSet(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	evalMu.Lock()
	evalRuns, evalLoaded = nil, false
	evalMu.Unlock()
	if err := startEval(st); err == nil || !strings.Contains(err.Error(), "还没有评测样本") {
		t.Errorf("no samples: %v", err)
	}
	llm := &mockLLM{wire: "openai", script: dressScript()}
	srv := httptest.NewServer(llm)
	defer srv.Close()
	key := "secret-key-12345678"
	if err := aiSave("deepseek", srv.URL, "deepseek-flash", &key, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ { // three runs: three samples
		llm.mu.Lock()
		llm.script, llm.seen = dressScript(), nil
		llm.mu.Unlock()
		aiSessionFor(proj).reset()
		if err := startAIRun(st, aiRunReq{Project: proj, Mode: "dress", Folders: []string{"Assets/Shop/SailorSet"}}); err != nil {
			t.Fatal(err)
		}
		waitRun(t, aiSessionFor(proj))
	}
	if n := len(evalFiles()); n != 3 {
		t.Fatalf("%d samples", n)
	}
	// the first sample's plan holds at once, the second's after a correction, the third's never
	u1 := map[string]any{"prompt_tokens": 3000, "completion_tokens": 500}
	llm.mu.Lock()
	llm.script = []mockTurn{{text: planItems(false), usage: u1}, {text: planItems(true), usage: u1}, {text: planItems(false), usage: u1}, {text: "不知道", usage: u1}, {text: "还是不知道", usage: u1}}
	llm.seen = nil
	llm.mu.Unlock()
	if err := startEval(st); err != nil {
		t.Fatal(err)
	}
	if err := startEval(st); err == nil { // (one at a time)
		t.Error("a second evaluation started")
	}
	run := waitEval(t)
	if run.Samples != 3 || run.Done != 3 || run.First != 1 || run.Fixed != 1 || run.Failed != 1 || run.Errors != 0 || run.Model != "deepseek-flash" {
		t.Fatalf("run %+v", run)
	}
	if r := run.Results[0]; r.Outcome != planFirst || r.Requests != 1 || r.Tokens != 3500 || r.Items != 7 || r.Parts != 4 || !strings.Contains(r.Name, "SailorSet") {
		t.Errorf("first %+v", r)
	}
	if r := run.Results[1]; r.Outcome != planFixed || r.Requests != 2 || r.Tokens != 7000 {
		t.Errorf("second %+v", r)
	}
	if r := run.Results[2]; r.Outcome != planFailed || r.Requests != 2 || r.Problems != 1 || r.Items != 0 {
		t.Errorf("third %+v", r)
	}
	if run.Tokens != 3500*5 || run.Cost["CNY"] <= 0 {
		t.Errorf("tokens %d, cost %v", run.Tokens, run.Cost)
	}
	// nothing was asked of Unity, and the results stay
	evalMu.Lock()
	evalRuns, evalLoaded = nil, false
	evalMu.Unlock()
	if runs := evalView()["runs"].([]evalRun); len(runs) != 1 || runs[0].First != 1 {
		t.Errorf("kept runs %+v", runs)
	}
	// stopped half way
	llm.mu.Lock()
	llm.script = []mockTurn{{text: planItems(false), status: 0}}
	llm.mu.Unlock()
	if err := startEval(st); err != nil {
		t.Fatal(err)
	}
	stopEval()
	if run = waitEval(t); !run.Stopped || run.Done > 3 {
		t.Errorf("stopped run %+v", run)
	}
	if err := clearEval(); err != nil || len(evalFiles()) != 0 || len(evalView()["runs"].([]evalRun)) != 2 {
		t.Errorf("cleared: %v, %d samples", err, len(evalFiles()))
	}
}

// evalSetUp: no runs yet, and the model the samples are asked of answers as script says.
func evalSetUp(t *testing.T, script []mockTurn) (*core.Store, *mockLLM) {
	t.Helper()
	st := testkit.NewStore(t)
	evalMu.Lock()
	evalRuns, evalLoaded = nil, false
	evalMu.Unlock()
	llm := &mockLLM{wire: "openai", script: script}
	srv := httptest.NewServer(llm)
	t.Cleanup(srv.Close)
	key := "secret-key-12345678"
	if err := aiSave("deepseek", srv.URL, "deepseek-flash", &key, nil); err != nil {
		t.Fatal(err)
	}
	return st, llm
}

// writeSample: a sample of one outfit whose draft items are those of the plan named by items (a list in JSON).
func writeSample(t *testing.T, name, items string) {
	t.Helper()
	x := `{"at":1,"project":"P","hierarchy":"` + defaultHierarchy + `","worn":[{"kind":"衣服","label":"水手服","object":"Envy cat",` +
		`"meshes":[{"path":"Envy cat/Body","active":true},{"path":"Envy cat/UV4_Cardigan","active":true}],"base":["衣服"],"param":"cloth","first":true,"items":` + items + `}],` +
		`"plan":[{"root":"Menu_Outfits","item":{"kind":"outfit","path":["衣服","水手服"],"label":"穿上","objects":["Envy cat"]}}]}`
	if err := os.MkdirAll(evalDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evalDir(), name), []byte(x), 0644); err != nil {
		t.Fatal(err)
	}
}

// The window asks for the evaluation while it runs and writes the answer after evalMu is let go: what it is
// handed is not what the evaluation goes on adding to (go test -race; it was "concurrent map iteration and map
// write" over the run's cost).
func TestEvalViewWhileRunning(t *testing.T) {
	st, _ := evalSetUp(t, []mockTurn{{text: "不知道", usage: map[string]any{"prompt_tokens": 3000, "completion_tokens": 500}}})
	for i := 0; i < 12; i++ {
		writeSample(t, "20260101-000000-"+string(rune('a'+i))+".json", "[0]")
	}
	if err := startEval(st); err != nil {
		t.Fatal(err)
	}
	var run evalRun
	for end := time.Now().Add(20 * time.Second); ; time.Sleep(time.Millisecond) {
		v := evalView()
		if _, err := json.Marshal(v); err != nil {
			t.Fatal(err)
		}
		if runs := v["runs"].([]evalRun); len(runs) > 0 && !runs[0].Running {
			run = runs[0]
			break
		}
		if time.Now().After(end) {
			t.Fatal("the evaluation did not end")
		}
	}
	if run.Done != 12 || run.Failed != 12 || run.Tokens != 12*2*3500 || run.Cost["CNY"] <= 0 || len(run.Results) != 12 {
		t.Errorf("run %+v", run)
	}
}

// A sample whose outfit names draft items its plan does not have (a file edited by hand, or cut short) is an
// error of that sample, not asked of the model; the evaluation goes on with the next.
func TestEvalBadSample(t *testing.T) {
	st, llm := evalSetUp(t, []mockTurn{{text: "{}"}})
	writeSample(t, "20260101-000000-000.json", "[3]")
	writeSample(t, "20260101-000000-001.json", "[-1]")
	writeSample(t, "20260101-000000-002.json", "[0]")
	if err := startEval(st); err != nil {
		t.Fatal(err)
	}
	run := waitEval(t)
	if run.Done != 3 || run.Errors != 2 || run.Failed != 1 || len(run.Results) != 3 {
		t.Fatalf("run %+v", run)
	}
	for _, r := range run.Results[:2] {
		if r.Outcome != planError || r.Err != "样本无法读取" || r.Requests != 0 {
			t.Errorf("bad sample %+v", r)
		}
	}
	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.seen) != 2 { // the good one, asked again once: the answer was no plan
		t.Errorf("%d requests", len(llm.seen))
	}
}

// A fault while a sample is asked ends the evaluation — as that sample's error, saved and logged — not the
// program.
func TestEvalFault(t *testing.T) {
	st, _ := evalSetUp(t, []mockTurn{{text: "{}"}})
	writeSample(t, "20260101-000000-000.json", "[0]")
	writeSample(t, "20260101-000000-001.json", "[0]")
	logs := captureLog(t)
	orig := menuLang
	defer func() { menuLang = orig }()
	menuLang = func() string { panic("boom") } // (asked as the request for a plan is made)
	if err := startEval(st); err != nil {
		t.Fatal(err)
	}
	run := waitEval(t)
	if run.Done != 1 || run.Errors != 1 || len(run.Results) != 1 || run.Results[0].File != "20260101-000000-000.json" || run.Results[0].Err != "程序内部错误：boom" {
		t.Errorf("run %+v", run)
	}
	evalMu.Lock()
	evalRuns, evalLoaded = nil, false
	evalMu.Unlock()
	if runs := evalView()["runs"].([]evalRun); len(runs) != 1 || runs[0].Running || runs[0].Errors != 1 {
		t.Errorf("saved %+v", runs)
	}
	if !strings.Contains(logs(), "AI 评测出错（样本 20260101-000000-000.json），已中止：boom") {
		t.Errorf("log:\n%s", logs())
	}
	if err := startEval(st); err != nil { // the next one starts
		t.Error(err)
	}
	waitEval(t)
}
