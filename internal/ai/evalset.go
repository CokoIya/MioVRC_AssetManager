package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
)

// The evaluation set (1.7.12): every request for a menu plan a run of the line makes is kept as a sample —
// the outfits and hair it put on, their meshes, the draft and the menu template; no pictures, no keys — and
// the samples can be asked again of the model set up now, to see how often its plan holds as it comes, after
// one correction, or not at all, and what that takes in requests, tokens, money and time. A model is compared
// with another on the player's own outfits, with the player's own key; nothing is changed in any project.

const evalKeep = 60 // samples kept; the oldest go

func evalDir() string { return filepath.Join(core.DataDir, "ai-samples") }

// evalSample: one request for a menu plan, as JSON (wornInfo and planned keep their fields to themselves).
type evalSample struct {
	At        int64        `json:"at"`
	Project   string       `json:"project"` // the folder's name only
	Hierarchy string       `json:"hierarchy"`
	Worn      []sampleWorn `json:"worn"`
	Plan      []samplePlan `json:"plan"`
}

type sampleWorn struct {
	Kind    string       `json:"kind"`
	Label   string       `json:"label"`
	Object  string       `json:"object"`
	Meshes  []meshInfo   `json:"meshes"`
	Colours []colourInfo `json:"colours,omitempty"`
	Base    []string     `json:"base"`
	Param   string       `json:"param"`
	First   bool         `json:"first,omitempty"`
	Items   []int        `json:"items"`
}

type samplePlan struct {
	Root string         `json:"root"`
	Item map[string]any `json:"item"`
}

func (x evalSample) inputs() ([]wornInfo, []planned) {
	var worn []wornInfo
	for _, w := range x.Worn {
		worn = append(worn, wornInfo{kind: w.Kind, label: w.Label, object: w.Object, meshes: w.Meshes, colours: w.Colours, base: w.Base, param: w.Param, first: w.First, items: w.Items})
	}
	var plan []planned
	for _, p := range x.Plan {
		it := map[string]any{}
		for k, v := range p.Item { // (back from JSON: lists of strings as the plan has them)
			if l, ok := v.([]any); ok {
				v = anyStrings(l)
			}
			it[k] = v
		}
		plan = append(plan, planned{p.Root, it})
	}
	return worn, plan
}

func (x evalSample) name() string {
	var names []string
	for _, w := range x.Worn {
		names = append(names, w.Label)
	}
	return strings.Join(names, "、")
}

// saveEvalSample keeps a run's request for a plan. (Best effort: a sample that cannot be written is none.)
func saveEvalSample(project string, worn []wornInfo, plan []planned, hierarchy string) {
	x := evalSample{At: time.Now().Unix(), Project: filepath.Base(strings.ReplaceAll(project, `\`, "/")), Hierarchy: hierarchy}
	for _, w := range worn {
		x.Worn = append(x.Worn, sampleWorn{Kind: w.kind, Label: w.label, Object: w.object, Meshes: w.meshes, Colours: w.colours, Base: w.base, Param: w.param, First: w.first, Items: w.items})
	}
	for _, p := range plan {
		x.Plan = append(x.Plan, samplePlan{Root: p.root, Item: p.item})
	}
	b, err := json.MarshalIndent(x, "", " ")
	if err != nil || os.MkdirAll(evalDir(), 0755) != nil {
		return
	}
	name := strings.ReplaceAll(time.Now().Format("20060102-150405.000"), ".", "-") + ".json"
	if os.WriteFile(filepath.Join(evalDir(), name), b, 0644) != nil {
		return
	}
	files := evalFiles()
	for len(files) > evalKeep {
		_ = os.Remove(filepath.Join(evalDir(), files[0]))
		files = files[1:]
	}
}

// evalFiles: the samples, oldest first.
func evalFiles() []string {
	ents, _ := os.ReadDir(evalDir())
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// evalResult: one sample asked again.
type evalResult struct {
	File     string             `json:"file"`
	Name     string             `json:"name"`
	Outcome  planOutcome        `json:"outcome"`
	Requests int                `json:"requests"`
	Tokens   int64              `json:"tokens"`
	Cost     map[string]float64 `json:"cost,omitempty"`
	Seconds  float64            `json:"seconds"`
	Items    int                `json:"items"` // the AI's items in the plan that was built
	Parts    int                `json:"parts"` // … of which part switches
	Problems int                `json:"problems,omitempty"`
	Err      string             `json:"err,omitempty"`
}

// evalRun: the samples asked of one model.
type evalRun struct {
	At       int64              `json:"at"`
	Provider string             `json:"provider"`
	Label    string             `json:"label"`
	Model    string             `json:"model"`
	Samples  int                `json:"samples"`
	Done     int                `json:"done"`
	Running  bool               `json:"running,omitempty"`
	Stopped  bool               `json:"stopped,omitempty"`
	First    int                `json:"first"`
	Fixed    int                `json:"fixed"`
	Failed   int                `json:"failed"`
	Errors   int                `json:"errors"`
	Tokens   int64              `json:"tokens"`
	Cost     map[string]float64 `json:"cost,omitempty"`
	Seconds  float64            `json:"seconds"`
	Results  []evalResult       `json:"results"`
}

var (
	evalMu     sync.Mutex
	evalRuns   []*evalRun // the runs of this session of the program, newest last (and evalRunsFile)
	evalCancel context.CancelFunc
	evalLoaded bool
)

func evalRunsFile() string { return filepath.Join(core.DataDir, "ai-eval.json") }

// evalRunsLocked: the runs, read once. Caller holds evalMu.
func evalRunsLocked() []*evalRun {
	if !evalLoaded {
		evalLoaded = true
		if b, err := os.ReadFile(evalRunsFile()); err == nil {
			_ = json.Unmarshal(b, &evalRuns)
		}
	}
	return evalRuns
}

func saveEvalRunsLocked() {
	keep := evalRuns
	if len(keep) > 20 {
		keep = keep[len(keep)-20:]
	}
	if b, err := json.Marshal(keep); err == nil {
		_ = os.WriteFile(evalRunsFile(), b, 0644)
	}
}

// evalView: what the window shows.
func evalView() map[string]any {
	evalMu.Lock()
	defer evalMu.Unlock()
	runs := evalRunsLocked()
	out := make([]evalRun, 0, len(runs))
	for i := len(runs) - 1; i >= 0 && len(out) < 8; i-- { // newest first
		out = append(out, *runs[i])
	}
	return map[string]any{"samples": len(evalFiles()), "dir": evalDir(), "runs": out}
}

// startEval asks every sample of the model set up now, one after another.
func startEval(st *core.Store) error {
	files := evalFiles()
	if len(files) == 0 {
		return errors.New("还没有评测样本：用 AI 运行一次流水线后会自动保存")
	}
	c, err := newAIClient(st)
	if err != nil {
		return err
	}
	evalMu.Lock()
	runs := evalRunsLocked()
	if len(runs) > 0 && runs[len(runs)-1].Running {
		evalMu.Unlock()
		return errors.New("评测正在进行")
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &evalRun{At: time.Now().Unix(), Provider: c.info.ID, Label: c.info.Label, Model: c.model, Samples: len(files), Running: true}
	evalRuns, evalCancel = append(runs, run), cancel
	evalMu.Unlock()
	go func() {
		defer cancel()
		for _, f := range files {
			if ctx.Err() != nil {
				break
			}
			r := evalOne(ctx, c, f)
			evalMu.Lock()
			run.Results = append(run.Results, r)
			run.Done++
			switch r.Outcome {
			case planFirst:
				run.First++
			case planFixed:
				run.Fixed++
			case planFailed:
				run.Failed++
			default:
				run.Errors++
			}
			run.Tokens += r.Tokens
			run.Seconds += r.Seconds
			for k, v := range r.Cost {
				if run.Cost == nil {
					run.Cost = map[string]float64{}
				}
				run.Cost[k] += v
			}
			evalMu.Unlock()
		}
		evalMu.Lock()
		run.Running, run.Stopped = false, ctx.Err() != nil
		saveEvalRunsLocked()
		evalMu.Unlock()
		core.Logf("AI 评测：%s %s，%d 个样本，首次通过 %d，修改后通过 %d，未通过 %d，出错 %d，%s tokens", run.Label, run.Model, run.Done, run.First, run.Fixed, run.Failed, run.Errors, tokText(run.Tokens))
	}()
	return nil
}

func stopEval() {
	evalMu.Lock()
	defer evalMu.Unlock()
	if evalCancel != nil {
		evalCancel()
	}
}

// evalOne asks one sample.
func evalOne(ctx context.Context, c *aiClient, file string) evalResult {
	r := evalResult{File: file}
	b, err := os.ReadFile(filepath.Join(evalDir(), file))
	var x evalSample
	if err == nil {
		err = json.Unmarshal(b, &x)
	}
	if err != nil || len(x.Worn) == 0 {
		r.Outcome, r.Err = planError, "样本无法读取"
		return r
	}
	r.Name = x.name()
	worn, plan := x.inputs()
	var used usageSum
	start := time.Now()
	merged, n, how, errs := askMenuPlan(withUsageSink(ctx, &used), c, worn, plan, x.Hierarchy)
	r.Seconds = time.Since(start).Seconds()
	v := used.view()
	r.Requests, r.Tokens, r.Cost, r.Outcome, r.Items = v.Calls, v.Tokens, v.Cost, how, n
	if how == planError {
		r.Err = errs[0]
	} else if how == planFailed {
		r.Problems = len(errs)
	}
	for _, p := range merged {
		if n > 0 && p.item["kind"] == "part" {
			r.Parts++
		}
	}
	return r
}

// openEvalDir: the samples' folder in Explorer, made when there is none.
func openEvalDir() error {
	if err := os.MkdirAll(evalDir(), 0755); err != nil {
		return fmt.Errorf("无法创建样本文件夹：%v", err)
	}
	return core.OpenInExplorer(evalDir(), true)
}

// clearEval removes the samples (the runs' results stay).
func clearEval() error {
	evalMu.Lock()
	defer evalMu.Unlock()
	if n := len(evalRuns); n > 0 && evalRuns[n-1].Running {
		return errors.New("评测正在进行")
	}
	for _, f := range evalFiles() {
		_ = os.Remove(filepath.Join(evalDir(), f))
	}
	return nil
}
