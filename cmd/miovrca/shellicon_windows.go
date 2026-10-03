package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"vrclib/internal/core"
)

var procSHChangeNotify = core.ModShell32.NewProc("SHChangeNotify")

func refreshShellIcon() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return
	}
	stamp := fmt.Sprintf("%s|%d|%d", exe, fi.Size(), fi.ModTime().UnixNano())
	mark := filepath.Join(core.DataDir, "exe.stamp")
	if b, err := os.ReadFile(mark); err == nil && string(b) == stamp {
		return
	}
	const (
		shcneUpdateItem   = 0x00002000
		shcneAssocChanged = 0x08000000
		shcnfIDList       = 0x0000
		shcnfPathW        = 0x0005
		shcnfFlushNoWait  = 0x3000
	)
	if p, err := syscall.UTF16PtrFromString(exe); err == nil {
		procSHChangeNotify.Call(shcneUpdateItem, shcnfPathW|shcnfFlushNoWait, uintptr(unsafe.Pointer(p)), 0)
	}
	// every cached icon is read again (the shortcuts to the exe and a pinned taskbar button among them)
	procSHChangeNotify.Call(shcneAssocChanged, shcnfIDList|shcnfFlushNoWait, 0, 0)
	_ = os.WriteFile(mark, []byte(stamp), 0644)
	core.Logf("exe 换了新的，已让系统重新读取图标")
}
