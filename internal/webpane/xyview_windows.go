//go:build windows

package webpane

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"

	"vrclib/internal/core"
)

// WinXy is 闲鱼's own view inside the window (see xyview.go): a child window with a WebView2 of its own, on its
// own profile, that the program leaves alone. Everything that touches the view runs on the window's thread.
type WinXy struct {
	Wv webview2.WebView

	mu      sync.Mutex
	hwnd    uintptr        // the child window; set while the view is being created too
	ch      *edge.Chromium // set once the view is there
	err     error          // why the last try to create it failed
	holding bool           // it was sent to a page, and has not been told to let go of it
	last    XyState        // what the view said last
	asked   int            // questions about its state that are still on their way to the window's thread
	askedAt time.Time

	loading time.Time // window's thread only: when the page shown was sent for
	silent  int       // window's thread only: how often in a row the view had no address to give (its browser is gone)
}

var (
	theXy       *WinXy // for the window procedure and the move hook (there is only one)
	xyClassOnce sync.Once
	// a view that could not be finished, or whose browser died: the browser may still hold pointers into it, so
	// it is never given up to the garbage collector
	xyKept []*edge.Chromium
)

// The browser of this view is started with nothing but this: no DevTools port, and never through a proxy (not
// the one of the settings, not the system's) — 闲鱼 sees the connection this computer really has.
const xyBrowserArgs = "--no-proxy-server"

// xyEnv: what WebView2 reads from the environment when a browser is started, set for this view's — the
// arguments above, and nothing of another view's (the pane's DevTools port) or of the player's own (another
// profile folder, a script debugger). The function returned puts everything back.
func xyEnv() func() {
	type was struct {
		v   string
		had bool
	}
	saved := map[string]was{}
	put := func(k, v string, set bool) {
		o, had := os.LookupEnv(k)
		saved[k] = was{o, had}
		if set {
			_ = os.Setenv(k, v)
		} else {
			_ = os.Unsetenv(k)
		}
	}
	put("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", xyBrowserArgs, true)
	for _, k := range []string{"WEBVIEW2_USER_DATA_FOLDER", "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER", "WEBVIEW2_PIPE_FOR_SCRIPT_DEBUGGER", "WEBVIEW2_WAIT_FOR_SCRIPT_DEBUGGER"} {
		put(k, "", false)
	}
	return func() {
		for k, o := range saved {
			if o.had {
				_ = os.Setenv(k, o.v)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}

func xyWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == 0x0005 && theXy != nil && theXy.ch != nil { // WM_SIZE
		theXy.ch.Resize()
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// ui runs f on the window's thread. A mistake in it must not end the program.
func (p *WinXy) ui(f func()) {
	p.Wv.Dispatch(func() {
		defer func() {
			if r := recover(); r != nil {
				core.Logf("闲鱼页面：内部错误：%v", r)
			}
		}()
		f()
	})
}

// wait: until done, for as long as given. The window's thread is woken again meanwhile: the message that tells
// it of something to run is lost while it is inside a loop of its own (a view being created, a window being
// dragged, a message box).
func (p *WinXy) wait(done <-chan struct{}, patience time.Duration) bool {
	end := time.After(patience)
	for {
		select {
		case <-done:
			return true
		case <-end:
			return false
		case <-time.After(400 * time.Millisecond):
			p.Wv.Dispatch(func() {})
		}
	}
}

func (p *WinXy) view() *edge.Chromium {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ch
}

func (p *WinXy) ensure() error {
	if p.view() != nil {
		return nil
	}
	done := make(chan struct{})
	p.ui(func() { defer close(done); p.create() })
	if !p.wait(done, 45*time.Second) {
		return errors.New("闲鱼页面启动超时")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch == nil {
		if p.err != nil {
			return p.err
		}
		return errors.New("闲鱼页面启动失败")
	}
	return nil
}

// create runs on the window's thread.
func (p *WinXy) create() {
	p.mu.Lock()
	if p.hwnd != 0 { // there already
		p.mu.Unlock()
		return
	}
	p.err = nil
	p.mu.Unlock()
	fail := func(err error) {
		p.mu.Lock()
		p.hwnd, p.ch, p.err, p.holding = 0, nil, err, false
		p.mu.Unlock()
		core.Logf("闲鱼页面：%v", err)
	}
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString("MioVRCXy")
	xyClassOnce.Do(func() {
		wc := wndClassExW{lpfnWndProc: syscall.NewCallback(xyWndProc), hInstance: hinst, lpszClassName: cls}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	})
	const style = 0x40000000 | 0x02000000 | 0x04000000 // WS_CHILD | WS_CLIPCHILDREN | WS_CLIPSIBLINGS
	h, _, cerr := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), 0, style, 0, 0, 200, 200,
		uintptr(p.Wv.Window()), 0, hinst, 0)
	if h == 0 {
		fail(fmt.Errorf("页面窗口创建失败：%v", cerr))
		return
	}
	p.mu.Lock()
	theXy, p.hwnd = p, h
	p.mu.Unlock()
	ch := edge.NewChromium()
	ch.Plain = true // nothing of the program's goes into its pages, and nothing of theirs is answered by it
	ch.DataPath = xyProfileDir()
	ch.Background = &edge.COREWEBVIEW2_COLOR{A: 255, R: 255, G: 255, B: 255}
	// what was sent to this thread while Embed ran its own message loop lost its wake-up message
	defer func() { go p.Wv.Dispatch(func() {}) }()
	ok := func() bool {
		defer xyEnv()() // (read by WebView2 when the browser is started; put back whatever happens in there)
		return ch.Embed(h)
	}()
	if !ok || !ch.Ready() {
		xyKept = append(xyKept, ch)
		procDestroyWindow.Call(h)
		fail(errors.New("内嵌浏览器启动失败"))
		return
	}
	if s, err := ch.GetSettings(); err == nil {
		_ = s.PutAreDevToolsEnabled(false)
	}
	ch.NavigationCompletedCallback = func(*edge.ICoreWebView2, *edge.ICoreWebView2NavigationCompletedEventArgs) {
		p.loading = time.Time{}
	}
	_ = ch.Hide()
	p.silent = 0
	p.mu.Lock()
	p.ch, p.last = ch, XyState{}
	p.mu.Unlock()
	core.Logf("闲鱼页面：独立的内嵌浏览器已启动（启动参数仅 %s；未开调试端口，不注入脚本）", xyBrowserArgs)
}

// gone: the view's browser is not there any more (it crashed or was ended): the child window goes, and the next
// page asked for starts a new view. Runs on the window's thread.
func (p *WinXy) gone() {
	p.mu.Lock()
	h, ch := p.hwnd, p.ch
	p.hwnd, p.ch, p.holding, p.last = 0, nil, false, XyState{}
	p.mu.Unlock()
	if ch != nil {
		xyKept = append(xyKept, ch)
	}
	if h != 0 {
		procDestroyWindow.Call(h)
	}
	p.loading, p.silent = time.Time{}, 0
	core.Logf("闲鱼页面：内嵌浏览器已退出，下次打开时重新启动")
}

func (p *WinXy) Open(u string) error {
	if strings.ContainsRune(u, 0) {
		return errBadURL
	}
	if err := p.ensure(); err != nil {
		return err
	}
	done := make(chan struct{})
	p.ui(func() {
		defer close(done)
		if ch := p.view(); ch != nil {
			p.loading = time.Now()
			ch.Navigate(u)
			p.mu.Lock()
			p.holding = true
			p.mu.Unlock()
		}
	})
	p.wait(done, 5*time.Second) // (the page itself is not waited for)
	return nil
}

// Holding: the view was sent to a page and still has it. Known without asking the view.
func (p *WinXy) Holding() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ch != nil && p.holding
}

func (p *WinXy) Place(x, y, w, h int, show bool) {
	p.ui(func() {
		p.mu.Lock()
		hwnd, ch := p.hwnd, p.ch
		p.mu.Unlock()
		if hwnd == 0 || ch == nil {
			return
		}
		const noActivate, showWin, hideWin, noMove, noSize = 0x0010, 0x0040, 0x0080, 0x0002, 0x0001
		if show && w > 0 && h > 0 {
			procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), noActivate|showWin) // HWND_TOP
			ch.Resize()
			_ = ch.Show()
		} else {
			_ = ch.Hide()
			procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, noActivate|hideWin|noMove|noSize)
		}
		_ = ch.NotifyParentWindowPositionChanged()
	})
}

// State: where the view is. Asked of the view on the window's thread, from the host side — the page is not
// asked anything. When that thread is busy, what was true a moment ago is the answer.
func (p *WinXy) State() (XyState, bool) {
	p.mu.Lock()
	if p.ch == nil {
		p.mu.Unlock()
		return XyState{}, false
	}
	if p.asked > 0 && time.Since(p.askedAt) < 3*time.Second {
		s := p.last
		p.mu.Unlock()
		return s, true
	}
	p.asked++
	p.askedAt = time.Now()
	p.mu.Unlock()
	type answer struct {
		s  XyState
		ok bool
	}
	got := make(chan answer, 1)
	p.Wv.Dispatch(func() {
		a := answer{}
		func() {
			defer func() { _ = recover() }()
			ch := p.view()
			if ch == nil {
				return
			}
			s := XyState{URL: ch.Source(), Title: ch.DocumentTitle(), Back: ch.CanGoBack(), Fwd: ch.CanGoForward()}
			s.Loading = !p.loading.IsZero() && time.Since(p.loading) < 30*time.Second
			if s.URL == "" { // a view always has an address ("about:blank" at least): its browser does not answer
				if p.silent++; p.silent >= 3 {
					p.gone()
					return
				}
			} else {
				p.silent = 0
			}
			a = answer{s, true}
		}()
		p.mu.Lock()
		p.asked--
		if a.ok {
			p.last = a.s
		}
		p.mu.Unlock()
		got <- a
	})
	select {
	case a := <-got:
		return a.s, a.ok
	case <-time.After(1500 * time.Millisecond):
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.last, p.ch != nil
	}
}

func (p *WinXy) Act(act string) {
	p.ui(func() {
		ch := p.view()
		if ch == nil {
			return
		}
		switch act {
		case "back":
			if ch.CanGoBack() {
				p.loading = time.Now()
				ch.GoBack()
			}
		case "forward":
			if ch.CanGoForward() {
				p.loading = time.Now()
				ch.GoForward()
			}
		case "reload":
			p.loading = time.Now()
			ch.Reload()
		case "stop":
			p.loading = time.Time{}
			ch.Stop()
		}
	})
}

// Blank: the page shown is let go of — off screen and on an empty page, so that nothing of it goes on running.
func (p *WinXy) Blank() {
	p.mu.Lock()
	p.holding = false
	p.mu.Unlock()
	p.ui(func() {
		p.mu.Lock()
		hwnd, ch := p.hwnd, p.ch
		p.last = XyState{}
		p.mu.Unlock()
		if hwnd == 0 || ch == nil {
			return
		}
		const noActivate, hideWin, noMove, noSize = 0x0010, 0x0080, 0x0002, 0x0001
		_ = ch.Hide()
		procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, noActivate|hideWin|noMove|noSize)
		p.loading = time.Time{}
		ch.Navigate("about:blank")
	})
}

// xyParentMoved keeps the view's popups (menus, date pickers) in place when the window moves.
func xyParentMoved() {
	if theXy != nil && theXy.ch != nil {
		_ = theXy.ch.NotifyParentWindowPositionChanged()
	}
}
