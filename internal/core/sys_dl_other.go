//go:build !windows

package core

func DecodeCP932(b []byte) (string, bool)    { return "", false }
func ProtectData(b []byte) ([]byte, error)   { return b, nil }
func UnprotectData(b []byte) ([]byte, error) { return b, nil }

// FixedDrives: the hard disks other than the system's ("D:", "E:" …); none outside Windows.
func FixedDrives() []string { return nil }

// DiskFree: free bytes on the disk holding p (0 when unknown).
func DiskFree(p string) uint64 { return 0 }

func SystemDrive() string { return "" }
