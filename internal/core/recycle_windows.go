//go:build windows

package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

// RecycleOnly moves files or folders to the Recycle Bin and never past it. Windows deletes for good what it
// cannot recycle, so a drive without a Recycle Bin is refused beforehand (ErrNoRecycleBin), and for a file
// too big for the bin Windows asks the player itself: a "no" leaves the file where it is.
func RecycleOnly(paths []string) error {
	var buf []uint16
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		root, err := syscall.UTF16PtrFromString(filepath.VolumeName(abs) + `\`)
		if err != nil {
			return err
		}
		if t, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(root))); t != 3 { // only DRIVE_FIXED has a Recycle Bin
			return ErrNoRecycleBin
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
	const foDelete, fofSilent, fofNoConfirmation, fofAllowUndo, fofNoErrorUI, fofWantNukeWarning = 3, 0x4, 0x10, 0x40, 0x400, 0x4000
	op := shFileOpStruct{wFunc: foDelete, pFrom: &buf[0], fFlags: fofSilent | fofNoConfirmation | fofAllowUndo | fofNoErrorUI | fofWantNukeWarning}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return fmt.Errorf("SHFileOperation %d", r)
	}
	if op.fAnyOperationsAborted != 0 {
		return errors.New("取消了")
	}
	return nil
}
