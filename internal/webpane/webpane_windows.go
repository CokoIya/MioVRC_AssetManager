//go:build windows

package webpane

import (
	"errors"
	"fmt"
	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"vrclib/internal/core"
)

var (
	procRegisterClassExW = core.ModUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = core.ModUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = core.ModUser32.NewProc("DefWindowProcW")
	procDestroyWindow    = core.ModUser32.NewProc("DestroyWindow")
	procSetWindowPos     = core.ModUser32.NewProc("SetWindowPos")
	procGetModuleHandleW = core.ModKernel32DL.NewProc("GetModuleHandleW")
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type WinPane struct {
	Wv   webview2.WebView
	mu   sync.Mutex
	hwnd uintptr
	ch   *edge.Chromium
	port int
	err  error
}

var thePane *WinPane // for the window procedure (there is only one)

func paneWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == 0x0005 && thePane != nil && thePane.ch != nil { // WM_SIZE
		thePane.ch.Resize()
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (p *WinPane) Ensure(proxy string) (int, error) {
	p.mu.Lock()
	port := p.port
	p.mu.Unlock()
	if port > 0 {
		return port, nil
	}
	done := make(chan struct{})
	p.Wv.Dispatch(func() { p.create(proxy); close(done) })
	select {
	case <-done:
	case <-time.After(45 * time.Second):
		return 0, errors.New("内置浏览器启动超时")
	}
	p.mu.Lock()
	port, err := p.port, p.err
	p.mu.Unlock()
	if port == 0 {
		if err == nil {
			err = errors.New("内置浏览器启动失败")
		}
		return 0, err
	}
	for i := 0; i < 60; i++ { // the DevTools port opens a moment after the page
		if _, err := cdpTargets(port); err == nil {
			return port, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0, errors.New("内置浏览器没有打开调试端口")
}

// create runs on the window's thread.
func (p *WinPane) create(proxy string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hwnd != 0 {
		return
	}
	p.err = nil
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString("MioVRCPane")
	wc := wndClassExW{lpfnWndProc: syscall.NewCallback(paneWndProc), hInstance: hinst, lpszClassName: cls}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))) // fails harmlessly when already registered
	const style = 0x40000000 | 0x02000000 | 0x04000000      // WS_CHILD | WS_CLIPCHILDREN | WS_CLIPSIBLINGS
	h, _, cerr := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), 0, style, 0, 0, 200, 200,
		uintptr(p.Wv.Window()), 0, hinst, 0)
	if h == 0 {
		p.err = fmt.Errorf("建不了页面窗口：%v", cerr)
		return
	}
	port, err := freePort()
	if err != nil {
		procDestroyWindow.Call(h)
		p.err = err
		return
	}
	args := fmt.Sprintf("--remote-debugging-port=%d", port)
	switch {
	case strings.EqualFold(proxy, "direct"):
		args += " --no-proxy-server"
	case proxy != "":
		args += " --proxy-server=" + proxy
	}
	// read by WebView2 when the environment is created; the app's own page was created without it
	old, had := os.LookupEnv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
	_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", args)
	ch := edge.NewChromium()
	ch.DataPath = paneProfileDir()
	ch.Background = &edge.COREWEBVIEW2_COLOR{A: 255, R: 255, G: 255, B: 255}
	thePane, p.hwnd, p.ch = p, h, ch
	ok := ch.Embed(h)
	if had {
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", old)
	} else {
		_ = os.Unsetenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
	}
	if !ok {
		procDestroyWindow.Call(h)
		p.hwnd, p.ch = 0, nil
		p.err = errors.New("内置浏览器启动失败")
		return
	}
	if s, err := ch.GetSettings(); err == nil {
		_ = s.PutAreDevToolsEnabled(false)
	}
	ch.Init(paneLinkScript)
	_ = ch.Hide()
	p.port = port
	core.Logf("内置浏览器已启动（调试端口 %d）", port)
	// a Dispatch posted while Embed ran its own message loop lost its wake-up message
	go p.Wv.Dispatch(func() {})
}

func (p *WinPane) Place(x, y, w, h int, show bool) {
	p.Wv.Dispatch(func() {
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

// PaneParentMoved keeps the pane's popups (menus, date pickers) in place when the window moves.
func PaneParentMoved() {
	if thePane != nil && thePane.ch != nil {
		_ = thePane.ch.NotifyParentWindowPositionChanged()
	}
}
