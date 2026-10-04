package webpane

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
)

func TestJinxxyOrigin(t *testing.T) {
	for o, want := range map[string]bool{
		"https://jinxxy.com": true, "https://www.jinxxy.com/market/browse": true, "https://dashboard.jinxxy.com": true,
		"http://jinxxy.com": false, "https://jinxxy.com.evil.example": false, "https://notjinxxy.com": false, "https://jinxxy-cdn.com": false,
		"https://payhip.com": false, "https://booth.pm": false, "": false, "null": false, "about:blank": false,
	} {
		if jinxxyOrigin(o) != want {
			t.Errorf("%q: Jinxxy page = %v, want %v", o, !want, want)
		}
	}
	t.Setenv("VRCLIB_JINXXY_BASE", "http://127.0.0.1:8124") // the stand-in the tests use
	if !jinxxyOrigin("http://127.0.0.1:8124") || jinxxyOrigin("http://127.0.0.1:9999") || jinxxyOrigin("http://localhost:8124") {
		t.Error("the test stand-in for jinxxy.com")
	}
	if !keepLoginSite(".jinxxy.com") || !keepLoginSite("jinxxy.com") || !keepLoginSite("127.0.0.1") || keepLoginSite("payhip.com") {
		t.Error("whose logins are kept")
	}
	// the script knows a Jinxxy page by its host; the stand-in by its whole origin
	if s := paneScriptFor("https://jinxxy.com"); strings.Contains(s, "__JX__") || strings.Contains(s, "location.origin===\"") || !strings.Contains(s, `jinxxy\.com$/i.test(location.hostname)`) {
		t.Error("the script for the live site")
	}
	if s := paneScriptFor("http://127.0.0.1:8124"); !strings.Contains(s, `||location.origin==="http://127.0.0.1:8124"`) {
		t.Error("the script for the stand-in")
	}
}

// A link handed over by the page is opened in the default browser only when a Jinxxy page handed it over, and
// only when it leads somewhere else by http(s).
func TestExternalLinkChecksCaller(t *testing.T) {
	var got []string
	open := PaneOpenExternal
	PaneOpenExternal = func(u string) error { got = append(got, u); return nil }
	defer func() { PaneOpenExternal = open }()
	p := &webPane{St: &core.Store{}, origins: map[int]string{}}
	ctx := func(id int, origin string) {
		b, _ := json.Marshal(map[string]any{"context": map[string]any{"id": id, "origin": origin}})
		p.event("Runtime.executionContextCreated", b)
	}
	call := func(ctxID int, link string) {
		b, _ := json.Marshal(map[string]any{"name": "mioExternal", "payload": link, "executionContextId": ctxID})
		p.event("Runtime.bindingCalled", b)
	}
	ctx(1, "https://jinxxy.com")
	ctx(2, "https://www.goofish.com")
	ctx(3, "https://ads.example.com") // a frame inside the Jinxxy page
	call(1, "https://payhip.com/b/QvEtC?ref=jinxxy")
	call(2, "https://payhip.com/b/2")
	call(3, "https://payhip.com/b/3")
	call(9, "https://payhip.com/b/9") // a context never heard of, and no page to ask where it is
	call(1, "https://jinxxy.com/aesu/studded_shorts")
	call(1, "javascript:alert(1)")
	call(1, "file:///C:/Windows/system32/calc.exe")
	call(1, "ms-settings:display")
	call(1, "not an address")
	if len(got) != 1 || got[0] != "https://payhip.com/b/QvEtC?ref=jinxxy" {
		t.Errorf("opened outside: %q", got)
	}
}

// A file the pane's browser downloads is handed on once it is whole, whatever order its reports come in.
func TestPaneDownloadEvents(t *testing.T) {
	dir := t.TempDir()
	type saved struct{ path, name string }
	done := make(chan saved, 4)
	keep, wait := PaneFileSaved, paneBeginWait
	PaneFileSaved = func(st *core.Store, path, name string) (string, error) {
		done <- saved{path, name}
		return filepath.Join(dir, "lib", name), nil
	}
	paneBeginWait = 400 * time.Millisecond
	defer func() {
		PaneFileSaved, paneBeginWait = keep, wait
		paneFileMu.Lock()
		paneFiles = nil
		paneFileMu.Unlock()
	}()
	p := &webPane{St: &core.Store{}, dlDir: dir, kind: "jinxxy", origins: map[int]string{}, frames: map[int]string{}}
	ev := func(method string, m map[string]any) {
		b, _ := json.Marshal(m)
		p.event(method, b)
	}
	ctx := func(id int, origin, frame string) {
		ev("Runtime.executionContextCreated", map[string]any{"context": map[string]any{"id": id, "origin": origin, "auxData": map[string]any{"frameId": frame, "isDefault": true}}})
	}
	ctx(1, "https://jinxxy.com", "MAIN") // the pane's page is a Jinxxy page
	was := core.Downloading.Load()
	g1 := "527a8247-3c3c-472e-8a03-f074dbb4055a"
	// (the file itself comes from wherever the shop keeps its files: what counts is the page that began it)
	ev("Browser.downloadWillBegin", map[string]any{"guid": g1, "frameId": "MAIN", "url": "https://cdn.example/f?sig=secret", "suggestedFilename": `..\..\Studded: Shorts?.zip`})
	ev("Browser.downloadProgress", map[string]any{"guid": g1, "totalBytes": 100, "receivedBytes": 40, "state": "inProgress"})
	if fs := PaneFiles(); len(fs) != 1 || fs[0].Status != "running" || fs[0].Done != 40 || fs[0].Total != 100 || strings.ContainsAny(fs[0].Name, `\/:?`) || !strings.HasSuffix(fs[0].Name, ".zip") {
		t.Fatalf("in progress: %+v", fs)
	}
	if core.Downloading.Load() != was+1 {
		t.Error("not counted as a download under way")
	}
	ev("Browser.downloadProgress", map[string]any{"guid": g1, "totalBytes": 100, "receivedBytes": 100, "state": "completed"})
	select {
	case s := <-done:
		if s.path != filepath.Join(dir, g1) || s.name != PaneFiles()[0].Name {
			t.Fatalf("handed on: %+v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not handed on")
	}
	if fs := PaneFiles(); fs[0].Status != "done" || fs[0].Path == "" || core.Downloading.Load() != was {
		t.Fatalf("done: %+v", fs)
	}
	// finished before it is reported as begun (a small file): its name is waited for
	g2 := "93389612-bf49-4f8e-aa3b-16ffec4cb4d1"
	go ev("Browser.downloadProgress", map[string]any{"guid": g2, "totalBytes": 22, "receivedBytes": 22, "state": "completed"})
	time.Sleep(150 * time.Millisecond)
	ev("Browser.downloadWillBegin", map[string]any{"guid": g2, "frameId": "MAIN", "suggestedFilename": "Late Name.unitypackage"})
	select {
	case s := <-done:
		if s.name != "Late Name.unitypackage" {
			t.Fatalf("name: %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not handed on")
	}
	for i := 0; i < 100 && PaneFiles()[1].Status != "done"; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	// broken off: what there is of the file goes, and nothing is handed on
	g3 := "11111111-2222-3333-4444-555555555555"
	_ = os.WriteFile(filepath.Join(dir, g3), []byte("part"), 0644)
	ev("Browser.downloadWillBegin", map[string]any{"guid": g3, "frameId": "MAIN", "suggestedFilename": "x.zip"})
	ev("Browser.downloadProgress", map[string]any{"guid": g3, "state": "canceled"})
	if _, err := os.Stat(filepath.Join(dir, g3)); err == nil || PaneFiles()[2].Status != "failed" || core.Downloading.Load() != was {
		t.Fatalf("cancelled: %+v", PaneFiles()[2])
	}
	// a name that is not one of the browser's is never put into a path
	ev("Browser.downloadWillBegin", map[string]any{"guid": "../../etc/passwd", "frameId": "MAIN", "suggestedFilename": "x.zip"})
	if len(PaneFiles()) != 3 || len(done) != 0 {
		t.Fatal("a made-up id was taken")
	}
	// no folder was handed to the browser (the pane is not on Jinxxy): its downloads are none of the program's
	p2 := &webPane{St: &core.Store{}}
	b, _ := json.Marshal(map[string]any{"guid": g1[:35] + "b", "suggestedFilename": "y.zip"})
	p2.event("Browser.downloadWillBegin", b)
	if len(PaneFiles()) != 3 {
		t.Fatal("a download of another page was taken")
	}
}

// The pane being Jinxxy's tab is not enough: its search is a search engine's page, and a link there leads
// anywhere. A download is taken in only when the page (or frame) that began it is Jinxxy's; any other is
// stopped, what it wrote is removed, and the list says why.
func TestPaneDownloadFromAnotherSite(t *testing.T) {
	dir := t.TempDir()
	savedN := 0
	keep, wait := PaneFileSaved, paneBeginWait
	PaneFileSaved = func(st *core.Store, path, name string) (string, error) { savedN++; return path, nil }
	paneBeginWait = 300 * time.Millisecond
	defer func() {
		PaneFileSaved, paneBeginWait = keep, wait
		paneFileMu.Lock()
		paneFiles = nil
		paneFileMu.Unlock()
	}()
	p := &webPane{St: &core.Store{}, dlDir: dir, kind: "jinxxy", origins: map[int]string{}, frames: map[int]string{}}
	ev := func(method string, m map[string]any) {
		b, _ := json.Marshal(m)
		p.event(method, b)
	}
	ctx := func(id int, origin, frame string) {
		ev("Runtime.executionContextCreated", map[string]any{"context": map[string]any{"id": id, "origin": origin, "auxData": map[string]any{"frameId": frame}}})
	}
	was := core.Downloading.Load()
	n := 0
	refused := func(what, guid string) {
		t.Helper()
		n++
		fs := PaneFiles()
		if len(fs) != n {
			t.Fatalf("%s: %d files listed", what, len(fs))
		}
		f := fs[n-1]
		if f.ID != guid || f.Status != "failed" || !f.Foreign || f.Err != errForeignFile || f.Path != "" || savedN != 0 || core.Downloading.Load() != was {
			t.Fatalf("%s: %+v, %d handed on, %d counted", what, f, savedN, core.Downloading.Load()-was)
		}
		if _, err := os.Stat(filepath.Join(dir, guid)); err == nil {
			t.Fatalf("%s: what the browser wrote is still there", what)
		}
	}
	// 1. the pane's page is the search engine's (the Jinxxy tab's search), or a page a result led to
	ctx(1, "https://jinxxy.com", "MAIN")
	ev("Runtime.executionContextsCleared", nil)
	ctx(2, "https://www.bing.com", "MAIN")
	g := "aaaaaaaa-0000-0000-0000-000000000001"
	_ = os.WriteFile(filepath.Join(dir, g), []byte("part"), 0644)
	ev("Browser.downloadWillBegin", map[string]any{"guid": g, "frameId": "MAIN", "url": "https://evil.example/setup.zip", "suggestedFilename": "Moon Dress.zip"})
	ev("Browser.downloadProgress", map[string]any{"guid": g, "totalBytes": 4, "receivedBytes": 4, "state": "completed"}) // (the browser was faster than the cancel)
	refused("begun by a search page", g)
	// 2. a frame of another site inside a Jinxxy page
	ev("Runtime.executionContextsCleared", nil)
	ctx(3, "https://jinxxy.com", "MAIN")
	ctx(4, "https://ads.example.com", "AD")
	g = "aaaaaaaa-0000-0000-0000-000000000002"
	ev("Browser.downloadWillBegin", map[string]any{"guid": g, "frameId": "AD", "suggestedFilename": "x.zip"})
	refused("begun by a frame of another site", g)
	// 3. whole before it is reported as begun, and then begun by another site's page
	g = "aaaaaaaa-0000-0000-0000-000000000003"
	_ = os.WriteFile(filepath.Join(dir, g), []byte("all of it"), 0644)
	over := make(chan bool)
	go func() {
		ev("Browser.downloadProgress", map[string]any{"guid": g, "totalBytes": 9, "receivedBytes": 9, "state": "completed"})
		close(over)
	}()
	time.Sleep(100 * time.Millisecond)
	ev("Browser.downloadWillBegin", map[string]any{"guid": g, "frameId": "AD", "suggestedFilename": "y.zip"})
	<-over
	refused("whole before it began", g)
	// 4. never reported as begun: nothing says whose page it was
	g = "aaaaaaaa-0000-0000-0000-000000000004"
	_ = os.WriteFile(filepath.Join(dir, g), []byte("all of it"), 0644)
	ev("Browser.downloadProgress", map[string]any{"guid": g, "totalBytes": 9, "receivedBytes": 9, "state": "completed"})
	refused("never reported as begun", g)
	// 5. a frame not heard of, and no page to ask where the pane is
	g = "aaaaaaaa-0000-0000-0000-000000000005"
	ev("Browser.downloadWillBegin", map[string]any{"guid": g, "frameId": "UNKNOWN", "suggestedFilename": "z.zip"})
	refused("an unknown frame", g)
	// 6. a frame with two contexts, one of them another site's (the page is on its way somewhere else)
	ctx(5, "https://jinxxy.com", "MIXED")
	ctx(6, "https://evil.example", "MIXED")
	g = "aaaaaaaa-0000-0000-0000-000000000007"
	ev("Browser.downloadWillBegin", map[string]any{"guid": g, "frameId": "MIXED", "suggestedFilename": "m.zip"})
	refused("a frame that is not Jinxxy's alone", g)
	// … while the Jinxxy page's own download, in the same pane, is taken
	g = "aaaaaaaa-0000-0000-0000-000000000006"
	ev("Browser.downloadWillBegin", map[string]any{"guid": g, "frameId": "MAIN", "suggestedFilename": "mine.zip"})
	ev("Browser.downloadProgress", map[string]any{"guid": g, "totalBytes": 9, "receivedBytes": 9, "state": "completed"})
	if fs := PaneFiles(); savedN != 1 || fs[len(fs)-1].Status != "done" || fs[len(fs)-1].Foreign {
		t.Errorf("the Jinxxy page's download: %+v, %d handed on", fs[len(fs)-1], savedN)
	}
}
