//go:build windows
// +build windows

package edge

// vrclib patch. What the host needs of a view that only shows a web site (Chromium.Plain): where it is, its
// title and its history, asked for from the host side. Nothing is put into the page, no script is run in it, and
// nothing of the page's is answered for it: a window a page wants to open is opened by the browser itself, as its
// own window, with the request the page made. Every call here has to be made on the thread that created the view
// (the window's). The methods are those of ICoreWebView2 in WebView2.h, in the order of the table in
// corewebview2.go.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func failedHR(hr uintptr) bool { return int32(uint32(hr)) < 0 } // an HRESULT is 32 bits wide: the rest of the register is not looked at

// ---------- where the view is ----------

// wstrOf: a property that hands out a string the caller has to free.
func (e *Chromium) wstrOf(get func(*iCoreWebView2Vtbl) ComProc) string {
	if e == nil || e.webview == nil {
		return ""
	}
	var p *uint16
	hr, _, _ := get(e.webview.vtbl).Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(&p)))
	if p == nil {
		return ""
	}
	s := ""
	if !failedHR(hr) {
		s = windows.UTF16PtrToString(p)
	}
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

func (e *Chromium) boolOf(get func(*iCoreWebView2Vtbl) ComProc) bool {
	if e == nil || e.webview == nil {
		return false
	}
	var b int32
	hr, _, _ := get(e.webview.vtbl).Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(&b)))
	return !failedHR(hr) && b != 0
}

func (e *Chromium) do(get func(*iCoreWebView2Vtbl) ComProc) {
	if e == nil || e.webview == nil {
		return
	}
	_, _, _ = get(e.webview.vtbl).Call(uintptr(unsafe.Pointer(e.webview)))
}

// Source is the address of the page shown.
func (e *Chromium) Source() string {
	return e.wstrOf(func(v *iCoreWebView2Vtbl) ComProc { return v.GetSource })
}

// DocumentTitle is the title of the page shown.
func (e *Chromium) DocumentTitle() string {
	return e.wstrOf(func(v *iCoreWebView2Vtbl) ComProc { return v.GetDocumentTitle })
}

func (e *Chromium) CanGoBack() bool {
	return e.boolOf(func(v *iCoreWebView2Vtbl) ComProc { return v.GetCanGoBack })
}

func (e *Chromium) CanGoForward() bool {
	return e.boolOf(func(v *iCoreWebView2Vtbl) ComProc { return v.GetCanGoForward })
}

func (e *Chromium) GoBack()    { e.do(func(v *iCoreWebView2Vtbl) ComProc { return v.GoBack }) }
func (e *Chromium) GoForward() { e.do(func(v *iCoreWebView2Vtbl) ComProc { return v.GoForward }) }
func (e *Chromium) Reload()    { e.do(func(v *iCoreWebView2Vtbl) ComProc { return v.Reload }) }
func (e *Chromium) Stop()      { e.do(func(v *iCoreWebView2Vtbl) ComProc { return v.Stop }) }

// Ready: the view was created.
func (e *Chromium) Ready() bool { return e != nil && e.webview != nil }
