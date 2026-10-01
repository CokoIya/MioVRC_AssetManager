//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"time"
)

var (
	modKernel32UP          = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcessUP      = modKernel32UP.NewProc("OpenProcess")
	procWaitForSingleObjUP = modKernel32UP.NewProc("WaitForSingleObject")
)

// waitPidExit blocks until the process has ended (or the timeout passes).
func waitPidExit(pid int, timeout time.Duration) {
	const synchronize = 0x00100000
	h, _, _ := procOpenProcessUP.Call(synchronize, 0, uintptr(pid))
	if h == 0 {
		return // already gone
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	procWaitForSingleObjUP.Call(h, uintptr(timeout.Milliseconds()))
}

// setInstalledVersion keeps "Apps & features" showing the right name and version after an in-place
// update (only for a copy put there by the installer: the key exists).
func setInstalledVersion(v string) {
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\VRCAssetLibrary`
	if hidden(exec.Command("reg", "query", key, "/v", "DisplayName")).Run() != nil {
		return
	}
	_ = hidden(exec.Command("reg", "add", key, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", v, "/f")).Run()
	_ = hidden(exec.Command("reg", "add", key, "/v", "DisplayName", "/t", "REG_SZ", "/d", appName, "/f")).Run()
}
