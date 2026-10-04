//go:build windows

package core

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// comdlgFilterSpec: one line of the "file type" list (COMDLG_FILTERSPEC).
type comdlgFilterSpec struct {
	name, spec *uint16
}

// pickFileCOM: the system's "open file" window (IFileOpenDialog), showing only files that match pattern
// ("*.zip").
func pickFileCOM(title, typeName, pattern string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := procCoInitializeEx.Call(0, 0x2|0x4) // COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE
	if int32(hr) >= 0 {
		defer procCoUninitialize.Call()
	}
	var dlg uintptr
	hr, _, _ = procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, 1,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if int32(hr) < 0 || dlg == 0 {
		return "", fmt.Errorf("CoCreateInstance 0x%x", uint32(hr))
	}
	defer comCall(dlg, 2) // Release
	var opts uint32
	comCall(dlg, 10, uintptr(unsafe.Pointer(&opts))) // GetOptions
	comCall(dlg, 9, uintptr(opts|0x40|0x800|0x1000)) // SetOptions: FORCEFILESYSTEM|PATHMUSTEXIST|FILEMUSTEXIST
	if pattern != "" {
		n, _ := syscall.UTF16PtrFromString(typeName)
		s, _ := syscall.UTF16PtrFromString(pattern)
		spec := [1]comdlgFilterSpec{{n, s}}
		comCall(dlg, 4, 1, uintptr(unsafe.Pointer(&spec[0]))) // SetFileTypes
		runtime.KeepAlive(spec)
	}
	if title != "" {
		t, _ := syscall.UTF16PtrFromString(title)
		comCall(dlg, 17, uintptr(unsafe.Pointer(t))) // SetTitle
	}
	owner, _, _ := procGetForegroundWindow.Call()
	hr = comCall(dlg, 3, owner) // Show
	if uint32(hr) == 0x800704C7 {
		return "", ErrPickCancelled
	}
	if int32(hr) < 0 {
		return "", fmt.Errorf("Show 0x%x", uint32(hr))
	}
	var item uintptr
	hr = comCall(dlg, 20, uintptr(unsafe.Pointer(&item))) // GetResult
	if int32(hr) < 0 || item == 0 {
		return "", fmt.Errorf("GetResult 0x%x", uint32(hr))
	}
	defer comCall(item, 2)
	var psz uintptr
	hr = comCall(item, 5, 0x80058000, uintptr(unsafe.Pointer(&psz))) // GetDisplayName(SIGDN_FILESYSPATH)
	if int32(hr) < 0 || psz == 0 {
		return "", fmt.Errorf("GetDisplayName 0x%x", uint32(hr))
	}
	defer procCoTaskMemFree.Call(psz)
	return utf16PtrString(psz), nil
}

// PickFile asks the player for one file. pattern: which files the window shows ("*.zip"); typeName: what
// that kind is called in the window's list.
func PickFile(title, typeName, pattern string) (string, error) {
	p, err := pickFileCOM(title, typeName, pattern)
	if err == nil || errors.Is(err, ErrPickCancelled) {
		return p, err
	}
	Logf("系统选文件窗口失败，改用备用方式: %v", err)
	out, err2 := powershell(`[Console]::OutputEncoding=[Text.Encoding]::UTF8; Add-Type -AssemblyName System.Windows.Forms;` +
		`$d=New-Object System.Windows.Forms.OpenFileDialog; $d.Title=` + psQuote(title) + `; $d.Filter=` + psQuote(typeName+"|"+pattern) + `;` +
		`if($d.ShowDialog() -eq 'OK'){ $d.FileName }`)
	if err2 != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", ErrPickCancelled
	}
	return s, nil
}
