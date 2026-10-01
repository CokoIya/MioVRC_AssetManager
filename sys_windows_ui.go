//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var errPickCancelled = errors.New("cancelled")

func driveRoots() []string {
	var out []string
	for c := 'C'; c <= 'Z'; c++ {
		d := string(c) + `:\`
		if isDir(d) {
			out = append(out, d)
		}
	}
	return out
}

func userDataBase() string { return os.Getenv("LOCALAPPDATA") }

func isInstalledDir(d string) bool {
	if fileExists(filepath.Join(d, "卸载.exe")) { // put there by the installer, wherever the player chose
		return true
	}
	for _, base := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
		os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramW6432"),
	} {
		if base != "" && underDir(d, base) {
			return true
		}
	}
	return false
}

func browserCmd(browser string, args []string) *exec.Cmd { return exec.Command(browser, args...) }

// ---------- native folder picker (IFileOpenDialog with FOS_PICKFOLDERS) ----------

var (
	modOle32                        = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx              = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize              = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance            = modOle32.NewProc("CoCreateInstance")
	procCoTaskMemFree               = modOle32.NewProc("CoTaskMemFree")
	modUser32                       = syscall.NewLazyDLL("user32.dll")
	procGetForegroundWindow         = modUser32.NewProc("GetForegroundWindow")
	modShell32                      = syscall.NewLazyDLL("shell32.dll")
	procSHCreateItemFromParsingName = modShell32.NewProc("SHCreateItemFromParsingName")
	procShellExecuteW               = modShell32.NewProc("ShellExecuteW")
)

var (
	modShlwapi           = syscall.NewLazyDLL("shlwapi.dll")
	procAssocQueryString = modShlwapi.NewProc("AssocQueryStringW")
)

// assocExe asks Windows which program handles a URL protocol (honours the user's default-app choice).
func assocExe(proto string) string {
	var buf [1024]uint16
	n := uint32(len(buf))
	p, _ := syscall.UTF16PtrFromString(proto)
	verb, _ := syscall.UTF16PtrFromString("open")
	hr, _, _ := procAssocQueryString.Call(0x1000, 2, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if hr != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

func shellOpen(target string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := procCoInitializeEx.Call(0, 0x2|0x4); int32(hr) >= 0 {
		defer procCoUninitialize.Call()
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	t, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(t)), 0, 0, 1)
	if r <= 32 {
		return fmt.Errorf("ShellExecute %d", r)
	}
	return nil
}

type comGUID struct {
	D1     uint32
	D2, D3 uint16
	D4     [8]byte
}

var (
	clsidFileOpenDialog = comGUID{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = comGUID{0xd57c7288, 0xd4ad, 0x4768, [8]byte{0xbe, 0x02, 0x9d, 0x96, 0x95, 0x32, 0xd9, 0x60}}
	iidIShellItem       = comGUID{0x43826d1e, 0xe718, 0x42ee, [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
)

func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func utf16PtrString(p uintptr) string {
	var buf []uint16
	for i := uintptr(0); i < 32768; i++ {
		c := *(*uint16)(unsafe.Pointer(p + i*2))
		if c == 0 {
			break
		}
		buf = append(buf, c)
	}
	return string(utf16.Decode(buf))
}

func pickFolderCOM(title, initial string) (string, error) {
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
	comCall(dlg, 9, uintptr(opts|0x20|0x40|0x800))   // SetOptions: PICKFOLDERS|FORCEFILESYSTEM|PATHMUSTEXIST
	if title != "" {
		t, _ := syscall.UTF16PtrFromString(title)
		comCall(dlg, 17, uintptr(unsafe.Pointer(t))) // SetTitle
	}
	if initial != "" && isDir(initial) {
		p, _ := syscall.UTF16PtrFromString(initial)
		var item uintptr
		r, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0,
			uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item)))
		if int32(r) >= 0 && item != 0 {
			comCall(dlg, 12, item) // SetFolder
			comCall(item, 2)
		}
	}
	owner, _, _ := procGetForegroundWindow.Call()
	hr = comCall(dlg, 3, owner) // Show
	if uint32(hr) == 0x800704C7 {
		return "", errPickCancelled
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

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func powershell(script string) ([]byte, error) {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[i*2], b[i*2+1] = byte(c), byte(c>>8)
	}
	enc := base64.StdEncoding.EncodeToString(b)
	return hidden(exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", enc)).Output()
}

func pickFolder(title, initial string) (string, error) {
	p, err := pickFolderCOM(title, initial)
	if err == nil || errors.Is(err, errPickCancelled) {
		return p, err
	}
	logf("系统选文件夹窗口失败，改用备用方式: %v", err)
	out, err2 := powershell(`[Console]::OutputEncoding=[Text.Encoding]::UTF8; Add-Type -AssemblyName System.Windows.Forms;` +
		`$d=New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description=` + psQuote(title) + `; $d.ShowNewFolderButton=$false;` +
		`if($d.ShowDialog() -eq 'OK'){ $d.SelectedPath }`)
	if err2 != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", errPickCancelled
	}
	return s, nil
}

func createDesktopShortcut() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	out, err := powershell(`[Console]::OutputEncoding=[Text.Encoding]::UTF8;` +
		`$p=Join-Path ([Environment]::GetFolderPath('Desktop')) ` + psQuote(appName+".lnk") + `;` +
		`$s=(New-Object -ComObject WScript.Shell).CreateShortcut($p);` +
		`$s.TargetPath=` + psQuote(exe) + `; $s.WorkingDirectory=` + psQuote(filepath.Dir(exe)) + `;` +
		`$s.IconLocation=` + psQuote(exe+",0") + `; $s.Save(); $p`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ---------- Recycle Bin ----------

var procSHFileOperationW = modShell32.NewProc("SHFileOperationW")

type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

// recycleFiles moves files to the Recycle Bin (so an unpacked archive can still be brought back).
func recycleFiles(paths []string) error {
	var buf []uint16
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		u, err := syscall.UTF16FromString(abs)
		if err != nil {
			return err
		}
		buf = append(buf, u...) // each path ends with its NUL
	}
	if len(buf) == 0 {
		return nil
	}
	buf = append(buf, 0) // the list ends with a second NUL
	const foDelete, fofSilent, fofNoConfirmation, fofAllowUndo, fofNoErrorUI = 3, 0x4, 0x10, 0x40, 0x400
	op := shFileOpStruct{wFunc: foDelete, pFrom: &buf[0], fFlags: fofSilent | fofNoConfirmation | fofAllowUndo | fofNoErrorUI}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return fmt.Errorf("SHFileOperation %d", r)
	}
	if op.fAnyOperationsAborted != 0 {
		return errors.New("取消了")
	}
	return nil
}

// defaultArcExe: the program Windows opens files with this extension with ("" when none is chosen).
func defaultArcExe(ext string) string {
	var buf [1024]uint16
	n := uint32(len(buf))
	p, _ := syscall.UTF16PtrFromString(ext)
	verb, _ := syscall.UTF16PtrFromString("open")
	hr, _, _ := procAssocQueryString.Call(0, 2, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if hr != 0 {
		return ""
	}
	exe := syscall.UTF16ToString(buf[:])
	if strings.EqualFold(filepath.Base(exe), "explorer.exe") || strings.EqualFold(filepath.Base(exe), "rundll32.exe") {
		return "" // Windows' own zip folders: no command line
	}
	return exe
}
