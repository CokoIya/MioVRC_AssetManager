//go:build windows
// +build windows

package edge

import (
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The options are called as WebView2 calls them: through their vtable, with the values in memory of the COM
// allocator.
func TestEnvOptions(t *testing.T) {
	o := newEnvOptions("--remote-debugging-port=1234 --proxy-server=http://127.0.0.1:7890")
	this := uintptr(unsafe.Pointer(o))
	str := func(p ComProc) (string, uintptr) {
		var out uintptr = 1
		hr, _, _ := p.Call(this, uintptr(unsafe.Pointer(&out)))
		if out == 0 {
			return "<nil>", hr
		}
		s := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(out)))
		windows.CoTaskMemFree(unsafe.Pointer(out))
		return s, hr
	}
	if s, hr := str(o.vtbl.GetAdditionalBrowserArguments); hr != 0 || s != "--remote-debugging-port=1234 --proxy-server=http://127.0.0.1:7890" {
		t.Errorf("arguments: %q %x", s, hr)
	}
	if s, hr := str(o.vtbl.GetLanguage); hr != 0 || s != "<nil>" {
		t.Errorf("language: %q %x", s, hr)
	}
	if s, hr := str(o.vtbl.GetTargetCompatibleBrowserVersion); hr != 0 || s != envTargetVersion {
		t.Errorf("version: %q %x", s, hr)
	}
	ja, _ := windows.UTF16PtrFromString("ja-JP")
	if hr, _, _ := o.vtbl.PutLanguage.Call(this, uintptr(unsafe.Pointer(ja))); hr != 0 {
		t.Errorf("put language: %x", hr)
	}
	if s, _ := str(o.vtbl.GetLanguage); s != "ja-JP" {
		t.Errorf("language after put: %q", s)
	}
	sso := int32(7)
	if hr, _, _ := o.vtbl.GetAllowSingleSignOnUsingOSPrimaryAccount.Call(this, uintptr(unsafe.Pointer(&sso))); hr != 0 || sso != 0 {
		t.Errorf("sso: %d %x", sso, hr)
	}
	if hr, _, _ := o.vtbl.GetAdditionalBrowserArguments.Call(this, 0); uint32(hr) != hrPointer {
		t.Errorf("no place for the value: %x", hr)
	}
	// interfaces: IUnknown and the options themselves; a later version of the options is refused
	qi := func(g windows.GUID) (uintptr, uintptr) {
		var out uintptr = 1
		hr, _, _ := o.vtbl.QueryInterface.Call(this, uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&out)))
		return out, hr
	}
	if out, hr := qi(iidIUnknown); hr != 0 || out != this {
		t.Errorf("IUnknown: %x %x", out, hr)
	}
	if out, hr := qi(iidICoreWebView2EnvironmentOptions); hr != 0 || out != this {
		t.Errorf("options: %x %x", out, hr)
	}
	options2 := windows.GUID{Data1: 0xff85c98a, Data2: 0x1ba7, Data3: 0x4a6b, Data4: [8]byte{0x90, 0xc8, 0x2b, 0x75, 0x2c, 0x89, 0xe9, 0xe2}}
	if out, hr := qi(options2); uint32(hr) != hrNoInterface || out != 0 {
		t.Errorf("options 2: %x %x", out, hr)
	}
	if r, _, _ := o.vtbl.AddRef.Call(this); r != 4 { // 1, and the two interfaces given out
		t.Errorf("AddRef: %d", r)
	}
	for i := 0; i < 6; i++ {
		o.vtbl.Release.Call(this)
	}
	if r, _, _ := o.vtbl.AddRef.Call(this); r < 1 {
		t.Errorf("after releases: %d", r)
	}
}

// The loader reads the options: here (no WebView2 runtime under wine, say) environment creation fails, and the
// program goes on. VRCLIB_WEBVIEW2_RUNTIME=1 where a runtime is installed: the environment is created.
func TestEnvOptionsWithLoader(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_ = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	e := NewChromium()
	e.DataPath = t.TempDir()
	e.AdditionalBrowserArgs = "--remote-debugging-port=0"
	ok := e.Embed(0)
	t.Logf("embed: %v; values read: %d", ok, e.envOptions.asked)
	if os.Getenv("VRCLIB_WEBVIEW2_RUNTIME") == "1" && e.envOptions.asked == 0 {
		t.Error("the options were not read")
	}
}
