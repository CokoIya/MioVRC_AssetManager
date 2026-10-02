//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// On other systems (tests) the tools are wherever the environment says.
func findAlcom() string    { return os.Getenv("VRCLIB_ALCOM") }
func findVCC() string      { return os.Getenv("VRCLIB_VCC") }
func findUnityHub() string { return os.Getenv("VRCLIB_UNITYHUB") }
func alcomConfigured() bool {
	return os.Getenv("VRCLIB_ALCOM") != "" || statOK(filepath.Join(vccDir(), "vrc-get", "gui-config.json"))
}

func launchApp(exe string, args ...string) error {
	logf("launch %s %v", exe, args)
	if os.Getenv("VRCLIB_LAUNCH_REAL") == "" {
		return nil
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
