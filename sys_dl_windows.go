//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	modKernel32DL           = syscall.NewLazyDLL("kernel32.dll")
	procMultiByteToWideChar = modKernel32DL.NewProc("MultiByteToWideChar")
	modCrypt32              = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData    = modCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData  = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree           = modKernel32DL.NewProc("LocalFree")
)

// decodeCP932 reads a Japanese (Shift-JIS) file name, as found in zips made on Japanese Windows.
func decodeCP932(b []byte) (string, bool) {
	if len(b) == 0 {
		return "", true
	}
	const cp932, errInvalid = 932, 0x8 // MB_ERR_INVALID_CHARS
	n, _, _ := procMultiByteToWideChar.Call(cp932, errInvalid, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return "", false
	}
	buf := make([]uint16, n)
	n, _, _ = procMultiByteToWideChar.Call(cp932, errInvalid, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&buf[0])), n)
	if n == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf[:n]), true
}

type dataBlob struct {
	n uint32
	p *byte
}

func blobOf(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{n: uint32(len(b)), p: &b[0]}
}

func (b *dataBlob) bytes() []byte {
	out := make([]byte, b.n)
	copy(out, unsafe.Slice(b.p, b.n))
	return out
}

// protectData / unprotectData: Windows DPAPI, tied to this Windows user, so a copied data folder
// does not carry a usable Booth login.
func protectData(b []byte) ([]byte, error) {
	var out dataBlob
	r, _, err := procCryptProtectData.Call(uintptr(unsafe.Pointer(blobOf(b))), 0, 0, 0, 0, 0x1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.p)))
	return out.bytes(), nil
}

func unprotectData(b []byte) ([]byte, error) {
	var out dataBlob
	r, _, err := procCryptUnprotectData.Call(uintptr(unsafe.Pointer(blobOf(b))), 0, 0, 0, 0, 0x1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.p)))
	return out.bytes(), nil
}
