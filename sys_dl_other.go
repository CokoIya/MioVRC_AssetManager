//go:build !windows

package main

func decodeCP932(b []byte) (string, bool)    { return "", false }
func protectData(b []byte) ([]byte, error)   { return b, nil }
func unprotectData(b []byte) ([]byte, error) { return b, nil }

// fixedDrives: the hard disks other than the system's ("D:", "E:" …); none outside Windows.
func fixedDrives() []string { return nil }

// diskFree: free bytes on the disk holding p (0 when unknown).
func diskFree(p string) uint64 { return 0 }

func systemDrive() string { return "" }
