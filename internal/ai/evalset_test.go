package ai

import (
	"net/http/httptest"
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
