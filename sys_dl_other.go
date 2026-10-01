//go:build !windows

package main

func decodeCP932(b []byte) (string, bool)    { return "", false }
func protectData(b []byte) ([]byte, error)   { return b, nil }
func unprotectData(b []byte) ([]byte, error) { return b, nil }
