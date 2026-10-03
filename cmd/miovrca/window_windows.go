//go:build windows

package main

import (
	"github.com/jchv/go-webview2"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"vrclib/internal/core"
	"vrclib/internal/webpane"
)

const windowTitle = core.AppName

// The window's message loop has to stay on the thread that created it: keep main() on one thread.
func init() { runtime.LockOSThread() }

var (
	procFindWindowW         = core.ModUser32.NewProc("FindWindowW")
	procShowWindow          = core.ModUser32.NewProc("ShowWindow")
	procIsIconic            = core.ModUser32.NewProc("IsIconic")
	procSetForegroundWindow = core.ModUser32.NewProc("SetForegroundWindow")
	procGetDpiForSystem     = core.ModUser32.NewProc("GetDpiForSystem")
	procSystemParamsInfo    = core.ModUser32.NewProc("SystemParametersInfoW")
)

// focusExistingWindow brings an already open library window to the front.
func focusExistingWindow() bool {
	cls, _ := syscall.UTF16PtrFromString("webview")
	var h uintptr
	for _, t := range append([]string{windowTitle}, core.FormerNames...) { // a window of a version from before the renames too
		title, _ := syscall.UTF16PtrFromString(t)
		if h, _, _ = procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title))); h != 0 {
			break
		}
	}
	if h == 0 {
		return false
	}
	if min, _, _ := procIsIconic.Call(h); min != 0 {
		procShowWindow.Call(h, 9) // SW_RESTORE
	}
	procSetForegroundWindow.Call(h)
	return true
}

type rect struct{ Left, Top, Right, Bottom int32 }

// windowSize picks a comfortable first size: 1440×900 at 100 % scaling, never more than 92 % of the
// screen's free area.
func windowSize() (uint, uint) {
	dpi := uintptr(96)
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d >= 96 {
			dpi = d
		}
	}
	w, h := 1440*int(dpi)/96, 900*int(dpi)/96
	var wa rect
	if r, _, _ := procSystemParamsInfo.Call(0x30, 0, uintptr(unsafe.Pointer(&wa)), 0); r != 0 { // SPI_GETWORKAREA
		if mw := int(wa.Right-wa.Left) * 92 / 100; mw > 0 && w > mw {
			w = mw
		}
		if mh := int(wa.Bottom-wa.Top) * 92 / 100; mh > 0 && h > mh {
			h = mh
		}
	}
	return uint(w), uint(h)
}

// runNativeWindow shows the UI in the app's own window and returns when the user closes it.
// It returns false straight away when WebView2 is missing or fails to start.
func runNativeWindow(url string) bool {
	ver := core.WebView2Version()
	if ver == "" {
		core.Logf("没有 WebView2 运行库，改用浏览器窗口")
		return false
	}
	base, _ := os.UserCacheDir() // %LOCALAPPDATA%: the engine's cache is not library data
	if base == "" {
		base = core.DataDir
	}
	w, h := windowSize()
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(core.AppDataFolder(base), "WebView2"),
		WindowOptions: webview2.WindowOptions{
			Title: windowTitle, Width: w, Height: h, IconId: 1, Center: true,
			Background: 0x14151f, // matches --bg in app.css
		},
	})
	if wv == nil {
		core.Logf("WebView2 %s 启动失败，改用浏览器窗口", ver)
		return false
	}
	core.Logf("窗口：WebView2 %s", ver)
	if !webview2.IconLoaded {
		core.Logf("窗口图标没有加载到（exe 里的图标资源读不出来）")
	}
	webpane.NativePane = &webpane.WinPane{Wv: wv} // Booth / 闲鱼 pages open inside this window
	webview2.MoveHook = webpane.PaneParentMoved
	wv.SetSize(int(w)*55/100, int(h)*60/100, webview2.HintMin)
	wv.Navigate(url)
	wv.Run()
	return true
}
