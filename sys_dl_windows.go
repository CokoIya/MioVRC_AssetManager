//go:build windows

package main

import (
	"os"
	"strings"
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

var (
	procGetLogicalDrives    = modKernel32DL.NewProc("GetLogicalDrives")
	procGetDriveTypeW       = modKernel32DL.NewProc("GetDriveTypeW")
	procGetDiskFreeSpaceExW = modKernel32DL.NewProc("GetDiskFreeSpaceExW")
)

func systemDrive() string {
	if d := os.Getenv("SystemDrive"); d != "" {
		return strings.ToUpper(d)
	}
	return "C:"
}

// fixedDrives: the hard disks other than the system's ("D:", "E:" …).
func fixedDrives() []string {
	mask, _, _ := procGetLogicalDrives.Call()
	var out []string
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		d := string(rune('A'+i)) + ":"
		if strings.EqualFold(d, systemDrive()) {
			continue
		}
		root, _ := syscall.UTF16PtrFromString(d + `\`)
		if t, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(root))); t == 3 { // DRIVE_FIXED
			out = append(out, d)
		}
	}
	return out
}

// diskFree: free bytes on the disk holding p (0 when unknown).
func diskFree(p string) uint64 {
	root, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return 0
	}
	var free, total, totalFree uint64
	r, _, _ := procGetDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
	if r == 0 {
		return 0
	}
	return free
}
