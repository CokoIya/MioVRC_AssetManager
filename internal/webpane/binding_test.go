package webpane

import (
	"encoding/json"
	"testing"
	"time"

	"vrclib/internal/core"
)

func TestBoothOrigin(t *testing.T) {
	for o, want := range map[string]bool{
		"https://booth.pm": true, "https://accounts.booth.pm": true, "https://some-shop.booth.pm/items/1": true,
		"http://booth.pm": false, "https://booth.pm.evil.example": false, "https://notbooth.pm": false,
		"https://www.goofish.com": false, "https://pan.baidu.com": false, "": false, "null": false, "about:blank": false,
	} {
		if boothOrigin(o) != want {
			t.Errorf("%q: booth page = %v, want %v", o, !want, want)
		}
	}
	t.Setenv("VRCLIB_BOOTH_WEB", "http://127.0.0.1:8123") // the stand-in the tests use
	if !boothOrigin("http://127.0.0.1:8123") || boothOrigin("http://127.0.0.1:9999") {
		t.Error("the test stand-in for booth.pm")
	}
}

// The page's "mioDownload" call counts only when a Booth page made it, and only with a number for an id.
func TestDownloadBindingChecksCaller(t *testing.T) {
	var got []string
	clicked := PaneDownloadClicked
	PaneDownloadClicked = func(st *core.Store, id, item, name, file string) { got = append(got, id+"/"+item) }
	defer func() { PaneDownloadClicked = clicked }()
	p := &webPane{St: &core.Store{}, origins: map[int]string{}}
	ctx := func(id int, origin string) {
		b, _ := json.Marshal(map[string]any{"context": map[string]any{"id": id, "origin": origin}})
		p.event("Runtime.executionContextCreated", b)
	}
	call := func(ctxID int, payload string) {
		b, _ := json.Marshal(map[string]any{"name": "mioDownload", "payload": payload, "executionContextId": ctxID})
		p.event("Runtime.bindingCalled", b)
	}
	ctx(1, "https://accounts.booth.pm")
	ctx(2, "https://www.goofish.com")
	ctx(3, "https://ads.example.com") // a frame inside the Booth page
	call(1, `{"id":"4567","item":"123","name":"Dress","file":"Dress.zip"}`)
	call(2, `{"id":"4568","item":"123"}`)
	call(3, `{"id":"4569","item":"123"}`)
	call(9, `{"id":"4570","item":"123"}`) // a context never heard of, and no page to ask where it is
	call(1, `{"id":"../users/edit?x=1#","item":"123"}`)
	call(1, `{"id":"4571","item":"../../x"}`)
	if len(got) != 2 || got[0] != "4567/123" || got[1] != "4571/" {
		t.Errorf("downloads taken: %q", got)
	}
	// after the page went somewhere else its old contexts say nothing
	p.event("Runtime.executionContextsCleared", []byte(`{}`))
	call(1, `{"id":"4572","item":"123"}`)
	if len(got) != 2 {
		t.Errorf("a call from a context that is gone was taken: %q", got)
	}
}

// The wait for a login ends when the page area goes over to another tab, or stays off screen; a dialog over
// it for a moment does not end it.
func TestPaneLeft(t *testing.T) {
	away := paneAway
	paneAway = 60 * time.Millisecond
	set := func(kind string, shown bool) {
		Pane.Mu.Lock()
		Pane.kind, Pane.shown = kind, shown
		Pane.Mu.Unlock()
	}
	defer func() {
		paneAway = away
		set("", false)
	}()
	set("booth", true)
	d := &PaneDriver{Start: time.Now()}
	if d.UserLeft() {
		t.Fatal("left while the page is on screen")
	}
	set("booth", false) // a dialog opens over the page
	if d.UserLeft() {
		t.Error("left the moment the page was covered")
	}
	set("booth", true)
	time.Sleep(80 * time.Millisecond)
	if d.UserLeft() {
		t.Error("left although the page came back")
	}
	set("booth", false)
	d.UserLeft()
	time.Sleep(80 * time.Millisecond)
	if !d.UserLeft() {
		t.Error("still waiting with the page off screen for good")
	}
	set("xianyu", true)
	w := &PaneWatch{Kind: "gumroad", Start: time.Now()}
	if !w.Left() || !(&PaneDriver{Start: time.Now()}).UserLeft() {
		t.Error("still waiting after the page area went over to another tab")
	}
	set("gumroad", false) // opened, but never shown: only the start counts
	if w.Left() || !(&PaneWatch{Kind: "gumroad", Start: time.Now().Add(-time.Minute)}).Left() {
		t.Error("a page that never came on screen")
	}
}
