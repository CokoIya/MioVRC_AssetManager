//go:build windows
// +build windows

package edge

import (
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// vrclib patch: ICoreWebView2EnvironmentOptions, for the arguments a view's browser is started with.
// WebView2 (runtime 150 and later) does not read WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS, nor the command line or
// the user's policies, in a program that runs as administrator; the arguments given through this interface are
// still used. Only what the interface itself asks for is answered: every other interface (the later versions
// of the options) is refused, and WebView2 takes its defaults for them.

var (
	iidIUnknown                        = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidICoreWebView2EnvironmentOptions = windows.GUID{Data1: 0x2fde08a8, Data2: 0x1e9a, Data3: 0x4766, Data4: [8]byte{0x8c, 0x05, 0x95, 0xa9, 0xce, 0xb9, 0xd1, 0xc5}}

	procCoTaskMemAlloc = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemAlloc")
)

const (
	hrOK          = 0
	hrNoInterface = 0x80004002
	hrPointer     = 0x80004003
	hrOutOfMemory = 0x8007000E
	hrInvalidArg  = 0x80070057

	// the oldest runtime the options are meant for: any WebView2 runtime is newer
	envTargetVersion = "86.0.616.0"
)

type envOptionsVtbl struct {
	_IUnknownVtbl
	GetAdditionalBrowserArguments             ComProc
	PutAdditionalBrowserArguments             ComProc
	GetLanguage                               ComProc
	PutLanguage                               ComProc
	GetTargetCompatibleBrowserVersion         ComProc
	PutTargetCompatibleBrowserVersion         ComProc
	GetAllowSingleSignOnUsingOSPrimaryAccount ComProc
	PutAllowSingleSignOnUsingOSPrimaryAccount ComProc
}

// envOptions is handed to WebView2 as a COM object: the vtable comes first. It lives as long as the Chromium
// that holds it (WebView2 may keep it), and is never freed by Release.
type envOptions struct {
	vtbl   *envOptionsVtbl
	refs   int32
	asked  int32 // how often a value was read (for the tests)
	args   string
	lang   string
	target string
	sso    bool
}

func newEnvOptions(args string) *envOptions {
	return &envOptions{vtbl: &envOptionsFn, refs: 1, args: args, target: envTargetVersion}
}

// coTaskString: s in memory the caller frees with CoTaskMemFree; nothing (0) for an empty string, as WebView2's
// own options give for a value that was not set.
func coTaskString(s string) (uintptr, uintptr) {
	if s == "" {
		return 0, hrOK
	}
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, hrInvalidArg
	}
	p, _, _ := procCoTaskMemAlloc.Call(uintptr(len(u) * 2))
	if p == 0 {
		return 0, hrOutOfMemory
	}
	copy((*[1 << 20]uint16)(unsafe.Pointer(p))[:len(u):len(u)], u) // (go 1.16: no unsafe.Slice)
	return p, hrOK
}

func envOptionsQueryInterface(this *envOptions, riid, out uintptr) uintptr {
	if out == 0 {
		return hrPointer
	}
	o := (*uintptr)(unsafe.Pointer(out))
	if riid != 0 {
		g := *(*windows.GUID)(unsafe.Pointer(riid))
		if g == iidIUnknown || g == iidICoreWebView2EnvironmentOptions {
			atomic.AddInt32(&this.refs, 1)
			*o = uintptr(unsafe.Pointer(this))
			return hrOK
		}
	}
	*o = 0
	return hrNoInterface
}

func envOptionsAddRef(this *envOptions) uintptr {
	return uintptr(atomic.AddInt32(&this.refs, 1))
}

func envOptionsRelease(this *envOptions) uintptr {
	n := atomic.AddInt32(&this.refs, -1)
	if n < 1 {
		return 1 // (Go owns the object: it stays for as long as its Chromium does)
	}
	return uintptr(n)
}

func envOptionsGetString(get func(*envOptions) string) func(this *envOptions, value uintptr) uintptr {
	return func(this *envOptions, value uintptr) uintptr {
		if value == 0 {
			return hrPointer
		}
		atomic.AddInt32(&this.asked, 1)
		p, hr := coTaskString(get(this))
		*(*uintptr)(unsafe.Pointer(value)) = p
		return hr
	}
}

func envOptionsPutString(put func(*envOptions, string)) func(this *envOptions, value uintptr) uintptr {
	return func(this *envOptions, value uintptr) uintptr {
		s := ""
		if value != 0 {
			s = windows.UTF16PtrToString((*uint16)(unsafe.Pointer(value)))
		}
		put(this, s)
		return hrOK
	}
}

func envOptionsGetSSO(this *envOptions, value uintptr) uintptr {
	if value == 0 {
		return hrPointer
	}
	atomic.AddInt32(&this.asked, 1)
	v := int32(0)
	if this.sso {
		v = 1
	}
	*(*int32)(unsafe.Pointer(value)) = v
	return hrOK
}

func envOptionsPutSSO(this *envOptions, value uintptr) uintptr {
	this.sso = int32(value) != 0
	return hrOK
}

var envOptionsFn = envOptionsVtbl{
	_IUnknownVtbl{
		NewComProc(envOptionsQueryInterface),
		NewComProc(envOptionsAddRef),
		NewComProc(envOptionsRelease),
	},
	NewComProc(envOptionsGetString(func(o *envOptions) string { return o.args })),
	NewComProc(envOptionsPutString(func(o *envOptions, s string) { o.args = s })),
	NewComProc(envOptionsGetString(func(o *envOptions) string { return o.lang })),
	NewComProc(envOptionsPutString(func(o *envOptions, s string) { o.lang = s })),
	NewComProc(envOptionsGetString(func(o *envOptions) string { return o.target })),
	NewComProc(envOptionsPutString(func(o *envOptions, s string) { o.target = s })),
	NewComProc(envOptionsGetSSO),
	NewComProc(envOptionsPutSSO),
}
