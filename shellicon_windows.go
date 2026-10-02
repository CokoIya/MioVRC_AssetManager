package main

// The icon Windows shows for this exe (taskbar button of a pinned program, shortcuts, Explorer) comes from the
// shell's icon cache. When the exe is replaced by an update, the shell may read the icon while the file is still
// being written and keep a blank page for it. So the first time a changed exe runs, the shell is told to read
// the icon again.

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var procSHChangeNotify = modShell32.NewProc("SHChangeNotify")

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
	mark := filepath.Join(dataDir, "exe.stamp")
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
	logf("exe 换了新的，已让系统重新读取图标")
}
