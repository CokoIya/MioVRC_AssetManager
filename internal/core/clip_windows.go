//go:build windows

package core

import (
	"os"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The clipboard is looked at for one thing: a link the player copied — a netdisk share, a 闲鱼 or a Booth page —
// which the page then offers to add or to open, whatever tab it shows (the server decides what is one:
// NetdiskShareText here, clipLink there). Nothing is written to it, and what is read is not kept or logged.

var (
	procGetClipboardSequenceNumber = ModUser32.NewProc("GetClipboardSequenceNumber")
	procIsClipboardFormatAvailable = ModUser32.NewProc("IsClipboardFormatAvailable")
	procOpenClipboard              = ModUser32.NewProc("OpenClipboard")
	procCloseClipboard             = ModUser32.NewProc("CloseClipboard")
	procGetClipboardData           = ModUser32.NewProc("GetClipboardData")
	procGlobalLock                 = ModKernel32DL.NewProc("GlobalLock")
	procGlobalUnlock               = ModKernel32DL.NewProc("GlobalUnlock")
	procGlobalSize                 = ModKernel32DL.NewProc("GlobalSize")
	procRtlMoveMemory              = ModKernel32DL.NewProc("RtlMoveMemory")
	procRegisterClipboardFormatW   = ModUser32.NewProc("RegisterClipboardFormatW")
	procGetWindowThreadProcessId   = ModUser32.NewProc("GetWindowThreadProcessId")
)

// AppInFront: the window in front is one of this program's own (the interface in its own window, with the pages
// inside it). Not so when the player is in another program — or when the interface runs in a browser, which says
// for itself whether it has the focus.
func AppInFront() bool {
	h, _, _ := procGetForegroundWindow.Call()
	if h == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	return pid == uint32(os.Getpid())
}

// Elevated: the program runs as administrator.
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// clipPrivate: what is on the clipboard was marked by the program that put it there as not for programs that
// watch the clipboard (password managers do that).
func clipPrivate() bool {
	name, _ := syscall.UTF16PtrFromString("ExcludeClipboardContentFromMonitorProcessing")
	f, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	if f == 0 {
		return false
	}
	r, _, _ := procIsClipboardFormatAvailable.Call(f)
	return r != 0
}

// ClipboardSeq: a number that changes whenever what is on the clipboard changes (0: it cannot be told). Asking
// for it does not open the clipboard.
func ClipboardSeq() uint32 {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(r)
}

// ClipboardText: the text on the clipboard. ok is false when there is none, when it is longer than ClipMaxChars
// or when it is marked as not for programs that watch the clipboard; busy when another program is holding the
// clipboard at the moment (it can be asked for again).
func ClipboardText() (text string, ok, busy bool) {
	const cfUnicodeText = 13
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText); r == 0 || clipPrivate() {
		return "", false, false
	}
	runtime.LockOSThread() // the thread that opens the clipboard is the one that has to close it
	defer runtime.UnlockOSThread()
	opened := false
	for i := 0; i < 5 && !opened; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			opened = true
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !opened {
		return "", false, true
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText) // (the clipboard's own: not freed here)
	if h == 0 {
		return "", false, false
	}
	size, _, _ := procGlobalSize.Call(h)
	if size < 2 {
		return "", false, false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", false, false
	}
	defer procGlobalUnlock.Call(h)
	n := min(int(size/2), ClipMaxChars+1)
	buf := make([]uint16, n)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), p, uintptr(n)*2) // a copy: the block is the clipboard's
	for i, c := range buf {
		if c == 0 {
			return string(utf16.Decode(buf[:i])), true, false
		}
	}
	if n > ClipMaxChars {
		return "", false, false // longer than anything this is for
	}
	return string(utf16.Decode(buf)), true, false // (a block without the closing zero: all of it is text)
}
