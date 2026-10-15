//go:build windows

package main

import (
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/jchv/go-webview2"

	"vrclib/internal/core"
)

// The tray. The window's close button puts the window there — unless the settings say 「关闭窗口时：退出程序」 —
// and the program goes on: an import, a download, the pipeline. The icon is there while the window is: a click
// on it opens the window again, its menu ends the program. What the player should hear of (core.Notify) is said
// there while the window is hidden; while the window is open but not in front, its taskbar button flashes. The
// sounds play in any case, with the window or without (a browser tab).
//
// Only the close button and Alt+F4 (WM_SYSCOMMAND SC_CLOSE) go to the tray: a WM_CLOSE sent to the window — the
// installer's, which closes the program before it writes over it — ends the program as it always has.

var (
	procShellNotifyIconW       = core.ModShell32.NewProc("Shell_NotifyIconW")
	procPlaySoundW             = syscall.NewLazyDLL("winmm.dll").NewProc("PlaySoundW")
	procGetModuleHandleW       = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procFlashWindowEx          = core.ModUser32.NewProc("FlashWindowEx")
	procIsWindowVisible        = core.ModUser32.NewProc("IsWindowVisible")
	procGetForegroundWindow    = core.ModUser32.NewProc("GetForegroundWindow")
	procRegisterWindowMessageW = core.ModUser32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu        = core.ModUser32.NewProc("CreatePopupMenu")
	procAppendMenuW            = core.ModUser32.NewProc("AppendMenuW")
	procSetMenuDefaultItem     = core.ModUser32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenu         = core.ModUser32.NewProc("TrackPopupMenu")
	procDestroyMenu            = core.ModUser32.NewProc("DestroyMenu")
	procGetCursorPos           = core.ModUser32.NewProc("GetCursorPos")
	procPostMessageW           = core.ModUser32.NewProc("PostMessageW")
	procSendMessageTimeoutW    = core.ModUser32.NewProc("SendMessageTimeoutW")
	procLoadImageW             = core.ModUser32.NewProc("LoadImageW")
	procGetSystemMetrics       = core.ModUser32.NewProc("GetSystemMetrics")

	procChangeWindowMessageFilterEx = core.ModUser32.NewProc("ChangeWindowMessageFilterEx")
)

const (
	wmNull         = 0x0000
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmSysCommand   = 0x0112
	scClose        = 0xF060
	wmTray         = 0x8001 // WM_APP+1: the icon's messages (WM_APP itself runs the window's dispatch queue)
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	ninBalloonUser = 0x0405 // NIN_BALLOONUSERCLICK: the notification was clicked

	nimAdd, nimModify, nimDelete         = 0, 1, 2
	nifMessage, nifIcon, nifTip, nifInfo = 0x01, 0x02, 0x04, 0x10
	niifInfo, niifNoSound                = 0x01, 0x10 // (the program plays its own sound)

	swHide, swShow, swMinimize, swRestore  = 0, 5, 6, 9
	sndAsync, sndNoDefault, sndMemory      = 0x1, 0x2, 0x4
	tpmRightButton, tpmNoNotify, tpmRetCmd = 0x2, 0x80, 0x100
	mfSeparator                            = 0x800
	flashwTray, flashwTimerNoFG            = 0x2, 0xC

	trayID           = 1
	cmdOpen, cmdQuit = 1, 2
)

// NOTIFYICONDATAW, as Windows Vista and later have it (976 bytes on 64-bit Windows)
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type flashInfo struct {
	CbSize    uint32
	Hwnd      uintptr
	DwFlags   uint32
	UCount    uint32
	DwTimeout uint32
}

type point struct{ X, Y int32 }

type trayIcon struct {
	hwnd    uintptr
	wv      webview2.WebView
	icon    uintptr
	created uintptr     // "TaskbarCreated": the taskbar has started again (Explorer restarted), the icon is gone
	in      atomic.Bool // the icon is in the tray: the window is hidden
}

// theTray: the program's window's, once it is open (nil: no window of its own — a browser tab)
var theTray atomic.Pointer[trayIcon]

// showWindowMsg: what another copy of the program, started while this one is in the tray, sends to its window to
// bring it back (focusExistingWindow)
func showWindowMsg() uintptr {
	name, _ := syscall.UTF16PtrFromString("MioVRCA.ShowWindow")
	m, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	return m
}

func startTray(wv webview2.WebView, hwnd uintptr) {
	t := &trayIcon{hwnd: hwnd, wv: wv}
	if h, _, _ := procGetModuleHandleW.Call(0); h != 0 {
		cx, _, _ := procGetSystemMetrics.Call(49) // SM_CXSMICON
		cy, _, _ := procGetSystemMetrics.Call(50)
		t.icon, _, _ = procLoadImageW.Call(h, 1, 1, cx, cy, 0x8000) // the exe's icon (IMAGE_ICON, LR_SHARED)
	}
	name, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	t.created, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	show := showWindowMsg()
	// run as administrator, the window would not hear the taskbar (the icon's clicks, its coming back) nor a copy of
	// the program started without: Windows keeps such messages from a lesser program unless they are let through
	if procChangeWindowMessageFilterEx.Find() == nil {
		for _, m := range []uintptr{wmTray, t.created, show} {
			if m != 0 {
				procChangeWindowMessageFilterEx.Call(hwnd, m, 1, 0) // MSGFLT_ALLOW
			}
		}
	}
	webview2.MsgHook = func(hwnd, msg, wp, lp uintptr) (uintptr, bool) {
		switch {
		case msg == wmSysCommand && wp&0xFFF0 == scClose && !closeQuits():
			t.hide()
			return 0, true
		case msg == wmTray:
			switch lp & 0xFFFF {
			case wmLButtonUp, ninBalloonUser:
				t.show()
			case wmRButtonUp:
				t.menu()
			}
			return 0, true
		case show != 0 && msg == show:
			t.show()
			return 1, true
		case t.created != 0 && msg == t.created:
			if t.in.Load() {
				t.in.Store(false)
				t.add()
			}
			return 0, true
		case msg == wmDestroy:
			t.remove() // (and on to the window's own handling: the program ends)
		}
		return 0, false
	}
	theTray.Store(t)
}

// closeQuits: the settings say the close button ends the program
func closeQuits() bool {
	if appStore == nil {
		return true
	}
	appStore.Mu.RLock()
	defer appStore.Mu.RUnlock()
	return appStore.Settings.CloseQuit
}

func (t *trayIcon) data(flags uint32) *notifyIconData {
	d := &notifyIconData{HWnd: t.hwnd, UID: trayID, UFlags: flags, UCallbackMessage: wmTray, HIcon: t.icon}
	d.CbSize = uint32(unsafe.Sizeof(*d))
	return d
}

func shellNotify(op uintptr, d *notifyIconData) bool {
	r, _, _ := procShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(d)))
	return r != 0
}

// putText writes s into a fixed field of the structure, cut to fit, ending with a zero
func putText(dst []uint16, s string) {
	u := utf16.Encode([]rune(s))
	if len(u) > len(dst)-1 {
		u = u[:len(dst)-1]
	}
	copy(dst, u)
	dst[len(u)] = 0
}

// add puts the icon in the tray; false when there is no tray to put it in (no taskbar running)
func (t *trayIcon) add() bool {
	d := t.data(nifMessage | nifIcon | nifTip)
	putText(d.SzTip[:], core.AppName)
	op := uintptr(nimAdd)
	if t.in.Load() {
		op = nimModify
	}
	ok := shellNotify(op, d)
	t.in.Store(ok || t.in.Load())
	return ok
}

func (t *trayIcon) remove() {
	if t.in.Swap(false) {
		shellNotify(nimDelete, t.data(0))
	}
}

// hide: the close button — the window goes to the tray. The first time the player is told where it went.
func (t *trayIcon) hide() {
	if !t.add() {
		procShowWindow.Call(t.hwnd, swMinimize) // no tray to go to: minimized, never out of reach
		return
	}
	procShowWindow.Call(t.hwnd, swHide)
	core.Logf("窗口已收到托盘")
	if firstTimeInTray() {
		title, text := trayNote(uiLang())
		t.balloon(title, text)
	}
}

func (t *trayIcon) show() {
	procShowWindow.Call(t.hwnd, swShow)
	if min, _, _ := procIsIconic.Call(t.hwnd); min != 0 {
		procShowWindow.Call(t.hwnd, swRestore)
	}
	procSetForegroundWindow.Call(t.hwnd)
	t.remove()
}

func (t *trayIcon) balloon(title, text string) {
	if !t.in.Load() {
		return
	}
	d := t.data(nifInfo)
	putText(d.SzInfoTitle[:], title)
	putText(d.SzInfo[:], text)
	d.DwInfoFlags = niifInfo | niifNoSound
	shellNotify(nimModify, d)
}

// menu: the icon's right-click menu — open the window, or end the program
func (t *trayIcon) menu() {
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)
	open, quit := trayMenuText(uiLang())
	item := func(id uintptr, s string) {
		p, _ := syscall.UTF16PtrFromString(s)
		procAppendMenuW.Call(m, 0, id, uintptr(unsafe.Pointer(p)))
	}
	item(cmdOpen, open)
	procAppendMenuW.Call(m, mfSeparator, 0, 0)
	item(cmdQuit, quit)
	procSetMenuDefaultItem.Call(m, cmdOpen, 0) // in bold: what a click on the icon does
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	procSetForegroundWindow.Call(t.hwnd) // (or the menu stays open when the player clicks elsewhere)
	cmd, _, _ := procTrackPopupMenu.Call(m, tpmRightButton|tpmNoNotify|tpmRetCmd, uintptr(p.X), uintptr(p.Y), 0, t.hwnd, 0)
	procPostMessageW.Call(t.hwnd, wmNull, 0, 0)
	switch cmd {
	case cmdOpen:
		t.show()
	case cmdQuit:
		if core.Downloading.Load() > 0 {
			t.show() // the question about the downloads under way is asked over the window
		}
		procPostMessageW.Call(t.hwnd, wmClose, 0, 0) // as the close button did before: confirmClose, then the end
	}
}

// notice (on the window's thread): said in the tray while the window is hidden; the taskbar button flashes while
// it is open but not in front, until the player comes to it
func (t *trayIcon) notice(n core.Notice) {
	if t.in.Load() {
		title, text := core.NoticeText(n, uiLang())
		t.balloon(title, text)
		return
	}
	if fg, _, _ := procGetForegroundWindow.Call(); fg != t.hwnd {
		fi := flashInfo{Hwnd: t.hwnd, DwFlags: flashwTray | flashwTimerNoFG}
		fi.CbSize = uint32(unsafe.Sizeof(fi))
		procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
	}
}

// startNotices: what the program does with core.Notify — the sound, unless the settings turn it off (「试听」 plays
// all three whatever they say), and the window's part when there is a window
func startNotices(st *core.Store) {
	core.NoticeHook = func(n core.Notice) {
		if n.What == "test" {
			for i, k := range []string{"done", "ask", "fail"} {
				if i > 0 {
					time.Sleep(1400 * time.Millisecond)
				}
				playSound(k)
			}
			return
		}
		st.Mu.RLock()
		mute := st.Settings.NoSound
		st.Mu.RUnlock()
		if !mute {
			playSound(n.Kind)
		}
		if t := theTray.Load(); t != nil {
			t.wv.Dispatch(func() { t.notice(n) })
		}
	}
}

// playSound: the sound is kept by core for as long as the program runs, as PlaySound wants of one it plays
// while the call has returned
func playSound(kind string) {
	if b := core.NoticeSound(kind); len(b) > 0 {
		procPlaySoundW.Call(uintptr(unsafe.Pointer(&b[0])), 0, sndMemory|sndAsync|sndNoDefault)
	}
}

// trayGone: the icon goes before the program does (an update ends it with os.Exit)
func trayGone() {
	if t := theTray.Load(); t != nil {
		t.remove()
	}
}

// firstTimeInTray: the window has not been put in the tray before (it is marked now)
func firstTimeInTray() bool {
	if appStore == nil {
		return false
	}
	appStore.Mu.Lock()
	first := !appStore.Settings.TrayNoticed
	appStore.Settings.TrayNoticed = true
	appStore.Mu.Unlock()
	if first {
		go func() { _ = appStore.Save() }()
	}
	return first
}
