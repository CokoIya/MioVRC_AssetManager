//go:build !windows

package webpane

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"

	"vrclib/internal/core"
)

// Outside Windows (development and tests) the program has no window of its own, so the native side of the page
// area cannot run. With VRCLIB_FAKE_NATIVE=1, stand-ins take its place, so that everything around it — which
// view the page area shows, where it is put, what the toolbar is told — can be tried: the pane's browser is a
// headless one, started like the window-mode pane's, and 闲鱼's view is a list of addresses that says in the log
// what was done with it.

// FakeNative puts the stand-ins in place when the variable asks for them.
func FakeNative() bool {
	if os.Getenv("VRCLIB_FAKE_NATIVE") != "1" {
		return false
	}
	NativePane, NativeXy = &fakePane{}, &fakeXy{}
	return true
}

type fakePane struct {
	mu   sync.Mutex
	port int
	last string
}

func (f *fakePane) Ensure(proxy string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.port > 0 {
		if _, err := cdpTargets(f.port); err == nil {
			return f.port, nil
		}
	}
	b := core.FindEdge()
	if b == "" {
		return 0, errors.New("no browser for the stand-in pane (VRCLIB_BROWSER)")
	}
	port, _, err := launchPaneWindow(b, paneProfileDir(), "about:blank", true, proxy)
	if err != nil {
		return 0, err
	}
	f.port = port
	return port, nil
}

func (f *fakePane) Place(x, y, w, h int, show bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := fmt.Sprintf("%d,%d %dx%d show=%v", x, y, w, h, show); s != f.last {
		f.last = s
		core.Logf("fake pane place %s", s)
	}
}

type fakeXy struct {
	mu   sync.Mutex
	hist []string
	at   int
	last string
}

func (f *fakeXy) Open(u string) error {
	if os.Getenv("VRCLIB_FAKE_XY_FAIL") == "1" {
		return errors.New("内嵌浏览器启动失败")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hist) > 0 {
		f.hist = f.hist[:f.at+1]
	}
	f.hist = append(f.hist, u)
	f.at = len(f.hist) - 1
	core.Logf("fake xy open %s", u)
	return nil
}

func (f *fakeXy) Holding() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hist) > 0 && f.hist[f.at] != "about:blank"
}

func (f *fakeXy) Place(x, y, w, h int, show bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := fmt.Sprintf("%d,%d %dx%d show=%v", x, y, w, h, show); s != f.last {
		f.last = s
		core.Logf("fake xy place %s", s)
	}
}

func (f *fakeXy) State() (XyState, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hist) == 0 {
		return XyState{}, false
	}
	u := f.hist[f.at]
	s := XyState{URL: u, Title: u, Back: f.at > 0, Fwd: f.at < len(f.hist)-1}
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		s.Title = "闲鱼 " + p.Path
	}
	return s, true
}

func (f *fakeXy) Act(act string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case act == "back" && f.at > 0:
		f.at--
	case act == "forward" && f.at < len(f.hist)-1:
		f.at++
	}
	core.Logf("fake xy act %s", act)
}

func (f *fakeXy) Blank() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hist) > 0 {
		f.hist = append(f.hist[:f.at+1], "about:blank")
		f.at = len(f.hist) - 1
	}
	f.last = ""
	core.Logf("fake xy blank")
}
