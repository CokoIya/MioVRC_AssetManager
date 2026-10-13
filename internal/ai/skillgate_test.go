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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity"
	"vrclib/internal/unity/unitytest"
)

// fakeSkills plays the project's UnitySkills server the way UnitySkills answers: a skill is found by its name in
// any case (SkillRouter keeps them in a dictionary that ignores case), any other name is "not found".
type fakeSkills struct {
	mu     sync.Mutex
	skills []string          // the manifest's entries (JSON)
	lists  int               // how often the manifest was read
	sent   []string          // the skills called, as the request named them
	ran    []string          // …and the ones that ran, by their own name
	bodies map[string]string // what each skill that ran was given
}

// skillsServer starts it and lists it in UnitySkills' registry under the project (HOME is the test's).
func skillsServer(t *testing.T, proj string, skills ...string) *fakeSkills {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	f := &fakeSkills{skills: skills, bodies: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	_ = os.MkdirAll(filepath.Join(home, ".unity_skills"), 0755)
	_ = os.WriteFile(filepath.Join(home, ".unity_skills", "registry.json"), []byte(fmt.Sprintf(`{%q:{"path":%q,"port":%s,"last_active":%d}}`, proj, proj, addr[strings.LastIndex(addr, ":")+1:], time.Now().Unix())), 0644)
	return f
}

func (f *fakeSkills) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/skills":
		f.lists++
		_, _ = io.WriteString(w, `{"skills":[`+strings.Join(f.skills, ",")+`]}`)
	case strings.HasPrefix(r.URL.Path, "/skill/"):
		asked := strings.TrimPrefix(r.URL.Path, "/skill/")
		f.sent = append(f.sent, asked)
		for _, s := range f.skills {
			var k skillInfo
			if json.Unmarshal([]byte(s), &k) == nil && strings.EqualFold(k.Name, asked) {
				b, _ := io.ReadAll(r.Body)
				f.ran = append(f.ran, k.Name)
				f.bodies[k.Name] = string(b)
				_, _ = io.WriteString(w, `{"status":"success","result":{}}`)
				return
			}
		}
		_, _ = io.WriteString(w, `{"status":"error","errorCode":"SKILL_NOT_FOUND","error":"Skill '`+asked+`' not found"}`)
	default:
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}
}

func (f *fakeSkills) add(skill string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skills = append(f.skills, skill)
}

func (f *fakeSkills) state() (lists int, sent, ran []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists, append([]string{}, f.sent...), append([]string{}, f.ran...)
}

// callAnswering carries one tool call out and answers the question it puts, if any (all: 「本次对话始终允许」).
// The question comes back (nil when none was put) with how the call ended.
func callAnswering(t *testing.T, s *aiSession, tool string, args map[string]any, allow, all bool) (*AIStep, bool) {
	t.Helper()
	ch := make(chan bool, 1)
	go func() { _, _, ok := s.runTool(context.Background(), tool, args); ch <- ok }()
	var asked *AIStep
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); {
		select {
		case ok := <-ch:
			return asked, ok
		case <-time.After(5 * time.Millisecond):
		}
		if _, steps, _ := s.snapshot(); asked == nil && len(steps) > 0 && steps[len(steps)-1].Kind == "ask" && steps[len(steps)-1].Busy {
			q := steps[len(steps)-1]
			asked = &q
			if !s.answer(allow, all) {
				t.Error("nobody was waiting for the answer")
			}
		}
	}
	t.Fatalf("%s did not return", tool)
	return nil, false
}

const (
	skDelete   = `{"name":"asset_delete","operation":["Delete"],"readOnly":false,"mutatesAssets":true,"riskLevel":"medium"}`
	skSave     = `{"name":"scene_save","operation":["Execute"],"readOnly":false,"mutatesAssets":true,"riskLevel":"high"}`
	skScript   = `{"name":"script_create","operation":["Create"],"readOnly":false,"mutatesAssets":true,"mayTriggerReload":true,"riskLevel":"medium"}`
	skFind     = `{"name":"gameobject_find","operation":["Query"],"readOnly":true,"riskLevel":"low"}`
	skColour   = `{"name":"material_set_color","operation":["Modify"],"readOnly":false,"mutatesAssets":true,"riskLevel":"low"}`
	skTexture  = `{"name":"texture_set_settings","operation":["Modify"],"readOnly":false,"mutatesAssets":true,"riskLevel":"low"}`
	skCamShot  = `{"name":"camera_screenshot","operation":["Execute"],"readOnly":false,"mutatesAssets":true,"riskLevel":"low"}`
	skNewPurge = `{"name":"asset_purge_unused","operation":["Delete"],"readOnly":false,"mutatesAssets":true,"riskLevel":"high"}`
)

// UnitySkills runs a skill named in any case: the gate looks names up the same way, sends the manifest's own
// spelling, reads the manifest anew for a name it does not list, and asks about an unknown name that looks risky.
func TestSkillNamesInAnyCase(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	f := skillsServer(t, proj, skDelete, skSave, skScript, skFind, skCamShot)
	man := skillsManifest(context.Background(), proj)
	for name, ask := range map[string]bool{"Asset_Delete": true, "ASSET_DELETE": true, "Scene_Save": true, "Script_Create": true, "GameObject_Find": false, "gameobject_find": false} {
		if why := consentReason(man, name); (why != "") != ask {
			t.Errorf("consentReason(%s) = %q, want asked=%v", name, why, ask)
		}
	}
	s := aiSessionFor(proj)
	call := func(name string, args map[string]any, allow bool) (*AIStep, bool) {
		t.Helper()
		return callAnswering(t, s, "unity_skill", map[string]any{"name": name, "args": args}, allow, false)
	}
	// refused: nothing is sent; allowed: what runs is what the question named, in the manifest's spelling
	for _, name := range []string{"Asset_Delete", "Scene_Save", "Script_Create"} {
		q, ok := call(name, map[string]any{"assetPath": "Assets/Old"}, false)
		if want := strings.ToLower(name); q == nil || ok || !strings.Contains(q.Text, "「"+want+"」") || !strings.Contains(q.Text, "Assets/Old") {
			t.Errorf("%s ran unasked, or the question did not name %s: %+v", name, want, q)
		}
	}
	if _, sent, _ := f.state(); len(sent) != 0 {
		t.Fatalf("sent without the player's yes: %v", sent)
	}
	if q, ok := call("Asset_Delete", map[string]any{"assetPath": "Assets/Old"}, true); q == nil || !ok {
		t.Errorf("allowed: %+v %v", q, ok)
	}
	if _, sent, ran := f.state(); strings.Join(sent, " ") != "asset_delete" || strings.Join(ran, " ") != "asset_delete" {
		t.Errorf("sent %v, ran %v", sent, ran)
	}
	// a screenshot by another spelling is still a screenshot: its picture goes where the others go
	if _, ok := call("Camera_Screenshot", map[string]any{"savePath": "Assets/Avatar/Body.png"}, false); !ok {
		t.Error("camera screenshot")
	}
	f.mu.Lock()
	if b := f.bodies["camera_screenshot"]; !strings.Contains(b, `"savePath":"Assets/Screenshots/Body.png"`) || !strings.Contains(b, `"returnImage":true`) {
		t.Errorf("camera screenshot given %s", b)
	}
	f.mu.Unlock()
	// a skill UnitySkills has gained since the manifest was read (an update): read anew, and asked about
	f.add(skNewPurge)
	lists, _, _ := f.state()
	if q, ok := call("Asset_Purge_Unused", nil, false); q == nil || ok || !strings.Contains(q.Text, "会删除内容") {
		t.Errorf("a skill added since: %+v %v", q, ok)
	}
	if n, _, _ := f.state(); n != lists+1 {
		t.Errorf("the manifest was read %d more times, want once", n-lists)
	}
	if q, ok := call("asset_purge_unused", nil, false); q == nil || ok {
		t.Errorf("asked once, and not again: %+v %v", q, ok)
	}
	// a name UnitySkills does not know (read anew as well): one that looks risky is asked about, every time;
	// another goes through and UnitySkills says it has none
	q, ok := call("Foo_Delete_All", nil, false)
	if q == nil || ok || q.Opts != "once" || !strings.Contains(q.Text, "操作清单中没有该操作") {
		t.Errorf("an unknown risky name: %+v %v", q, ok)
	}
	if q, ok = call("no_such_skill", nil, false); q != nil || ok {
		t.Errorf("an unknown name: %+v %v", q, ok)
	}
	if _, sent, ran := f.state(); strings.Join(sent, " ") != "asset_delete camera_screenshot no_such_skill" || len(ran) != 2 {
		t.Errorf("sent %v, ran %v", sent, ran)
	}
}

// 「本次对话始终允许」 covers the one operation it was given to, for the rest of the conversation. What deletes,
// saves, writes scripts or runs commands is asked about every time: the question does not offer it, and the
// program does not take it when it is sent all the same.
func TestAlwaysAllowCoversOneOperation(t *testing.T) {
	testkit.NewStore(t)
	proj := t.TempDir()
	f := skillsServer(t, proj, skDelete, skSave, skScript, skColour, skTexture)
	s := aiSessionFor(proj)
	call := func(name string, allow, all bool) (*AIStep, bool) {
		t.Helper()
		return callAnswering(t, s, "unity_skill", map[string]any{"name": name, "args": map[string]any{"path": "Assets/M.mat"}}, allow, all)
	}
	if q, ok := call("material_set_color", true, true); q == nil || q.Opts != "" || !ok {
		t.Fatalf("the colour change: %+v %v", q, ok)
	}
	for _, name := range []string{"material_set_color", "Material_Set_Color"} {
		if q, ok := call(name, false, false); q != nil || !ok {
			t.Errorf("%s asked again after 「本次对话始终允许」: %+v %v", name, q, ok)
		}
	}
	if q, ok := call("texture_set_settings", false, false); q == nil || q.Opts != "" || ok {
		t.Errorf("another operation ran under the colour change's 「本次对话始终允许」: %+v %v", q, ok)
	}
	for _, name := range []string{"asset_delete", "scene_save", "script_create"} {
		q, ok := call(name, true, true) // an "always" sent although the question did not offer it
		if q == nil || q.Opts != "once" || !ok {
			t.Errorf("%s: %+v %v", name, q, ok)
		}
		if q, ok = call(name, false, false); q == nil || ok {
			t.Errorf("%s ran unasked the second time: %+v %v", name, q, ok)
		}
	}
	if _, _, ran := f.state(); strings.Join(ran, " ") != "material_set_color material_set_color material_set_color asset_delete scene_save script_create" {
		t.Errorf("ran %v", ran)
	}
	// a new conversation asks again
	s.reset()
	if q, ok := call("material_set_color", false, false); q == nil || ok {
		t.Errorf("after 「新对话」: %+v %v", q, ok)
	}

	for _, c := range []struct {
		k     skillInfo
		name  string
		every bool
	}{
		{skillInfo{Operation: []string{"Modify"}, MutatesAssets: true, Risk: "low"}, "material_set_color", false},
		{skillInfo{Operation: []string{"Modify"}, MutatesScene: true, MutatesAssets: true, Risk: "medium"}, "prefab_apply", false},
		{skillInfo{Operation: []string{"Create"}, MutatesAssets: true}, "scriptableobject_create", false},
		{skillInfo{Operation: []string{"Modify"}, MutatesAssets: true}, "graphics_remove_always_included_shader", true},
		{skillInfo{Operation: []string{"Delete"}}, "component_strip", true},
		{skillInfo{Operation: []string{"Analyze", "Execute"}}, "scene_export_report", true},
		{skillInfo{Operation: []string{"Modify"}, MayReload: true}, "debug_set_defines", true},
		{skillInfo{Operation: []string{"Modify"}, MayPlay: true}, "editor_toggle", true},
		// an older UnitySkills without notes: by the name
		{skillInfo{}, "gameobject_destroy", true}, {skillInfo{}, "prefab_save", true}, {skillInfo{}, "script_edit", true},
		{skillInfo{}, "dotween_generate_tween_script", true}, {skillInfo{}, "qframework_generate_ui_panel_code", true},
		{skillInfo{}, "editor_execute_menu", true}, {skillInfo{}, "event_invoke", true}, {skillInfo{}, "scene_load", true},
		{skillInfo{}, "scene_new", true}, {skillInfo{}, "scene_unload", true}, {skillInfo{}, "editor_play", true},
		{skillInfo{}, "gameobject_rename", false}, {skillInfo{}, "texture_set_settings", false},
	} {
		if askedEveryTime(c.k, c.name) != c.every {
			t.Errorf("askedEveryTime(%s %+v) = %v", c.name, c.k, !c.every)
		}
	}
}

// The case of TestAlwaysAllowCoversOneOperation in a run: 「本次对话始终允许」 to a colour change does not let a
// delete or a save of the scene run unasked.
func TestAlwaysAllowInARun(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	st.Projects = []core.ProjectInfo{{Name: filepath.Base(proj), Path: proj}} // (named as the usage scan names it)
	f := skillsServer(t, proj, skColour, skSave, skDelete)
	llm := &mockLLM{wire: "openai", script: []mockTurn{
		{calls: []aiCall{{ID: "a1", Name: "unity_skill", Args: json.RawMessage(`{"name":"material_set_color","args":{"path":"Assets/M.mat"}}`)}}},
		{calls: []aiCall{{ID: "a2", Name: "unity_skill", Args: json.RawMessage(`{"name":"Asset_Delete","args":{"assetPath":"Assets/Vendor"}}`)}}},
		{calls: []aiCall{{ID: "a3", Name: "unity_skill", Args: json.RawMessage(`{"name":"scene_save","args":{}}`)}}},
		{calls: []aiCall{{ID: "a4", Name: "unity_skill", Args: json.RawMessage(`{"name":"material_set_color","args":{"path":"Assets/N.mat"}}`)}}},
		{text: "done"},
	}}
	srv := httptest.NewServer(llm)
	defer srv.Close()
	key := "k-1234567890"
	_ = aiSave("openai", srv.URL, "m", &key, nil)
	s := aiSessionFor(proj)
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "把外套改成红色"}); err != nil {
		t.Fatal(err)
	}
	var asked []string
	for i, seen := 0, 0; i < 1000; i++ {
		busy, steps, _ := s.snapshot()
		if n := len(steps); n > seen && steps[n-1].Kind == "ask" && steps[n-1].Busy {
			seen = n
			asked = append(asked, steps[n-1].Opts)
			s.answer(true, true) // 「本次对话始终允许」 every time, whether offered or not
		}
		if !busy && i > 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, _, ran := f.state(); strings.Join(asked, ",") != ",once,once" || len(ran) != 4 {
		t.Errorf("questions %q, ran %v", asked, ran)
	}
}

// wantedAt: the time in the project's want_skills (0 when there is none).
func wantedAt(proj string) int64 {
	b, _ := os.ReadFile(filepath.Join(unity.BridgeDir(proj), "want_skills"))
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return n
}

// A conversation asks for UnitySkills' server as it begins, so that it starts while the AI thinks, and leaves it
// for a while when it ends. A project without UnitySkills is not asked to start one.
func TestRunWantsSkillsServer(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	unitytest.FakeUnity(t, proj, (&sceneUnity{}).answer)
	st.Projects = []core.ProjectInfo{{Name: filepath.Base(proj), Path: proj}} // (named as the usage scan names it)
	llm := &mockLLM{wire: "openai", script: []mockTurn{{text: "好的"}}}
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate // the AI thinks
		llm.ServeHTTP(w, r)
	}))
	defer srv.Close()
	key := "k-1234567890"
	_ = aiSave("openai", srv.URL, "m", &key, nil)
	s := aiSessionFor(proj)
	close(gate)
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "你好"}); err != nil {
		t.Fatal(err)
	}
	waitRun(t, s)
	if core.StatOK(filepath.Join(unity.BridgeDir(proj), "want_skills")) {
		t.Error("UnitySkills' server asked for in a project without it")
	}

	_ = os.MkdirAll(filepath.Join(proj, "Packages", unity.SkillsPkg), 0755)
	_ = os.WriteFile(filepath.Join(proj, "Packages", unity.SkillsPkg, "package.json"), []byte(`{"name":"com.besty.unity-skills","version":"2.8.4"}`), 0644)
	gate = make(chan struct{})
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "你好"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200 && wantedAt(proj) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	// before the AI has answered anything
	if now := time.Now().Unix(); wantedAt(proj) < now+170 || wantedAt(proj) > now+181 {
		t.Errorf("wanted until %d, %d s from now", wantedAt(proj), wantedAt(proj)-now)
	}
	close(gate)
	waitRun(t, s)
	if now := time.Now().Unix(); wantedAt(proj) < now+110 || wantedAt(proj) > now+121 {
		t.Errorf("after the run: wanted until %d, %d s from now", wantedAt(proj), wantedAt(proj)-now)
	}
}

// While a run goes on the server stays wanted (every third of the lease); once it is done, for the grace only.
func TestKeepSkillsRenews(t *testing.T) {
	proj := t.TempDir()
	_ = os.MkdirAll(unity.BridgeDir(proj), 0755)
	oldLease, oldGrace := skillsLease, skillsGrace
	skillsLease, skillsGrace = 3*time.Second, time.Second
	t.Cleanup(func() { skillsLease, skillsGrace = oldLease, oldGrace })
	ctx, cancel := context.WithCancel(context.Background())
	leave := keepSkills(ctx, proj)
	first := wantedAt(proj)
	if now := time.Now().Unix(); first < now+2 || first > now+3 {
		t.Errorf("wanted until %d, %d s from now", first, first-now)
	}
	for until := time.Now().Add(5 * time.Second); wantedAt(proj) <= first && time.Now().Before(until); {
		time.Sleep(50 * time.Millisecond)
	}
	if wantedAt(proj) <= first {
		t.Error("not renewed")
	}
	cancel()
	leave()
	if now := time.Now().Unix(); wantedAt(proj) > now+1 {
		t.Errorf("after the run: wanted until %d, %d s from now", wantedAt(proj), wantedAt(proj)-now)
	}
}
