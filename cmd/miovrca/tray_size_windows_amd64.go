//go:build windows && amd64

package main

import "unsafe"

// Windows reads these structures by its own layout: NOTIFYICONDATAW is 976 bytes on 64-bit Windows, FLASHWINFO
// 32. A field out of place does not compile.
var (
	_ [976 - unsafe.Sizeof(notifyIconData{})]byte
	_ [unsafe.Sizeof(notifyIconData{}) - 976]byte
	_ [32 - unsafe.Sizeof(flashInfo{})]byte
	_ [unsafe.Sizeof(flashInfo{}) - 32]byte
)
