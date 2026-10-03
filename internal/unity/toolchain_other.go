//go:build !windows

package unity

import (
	"os"
	"os/exec"
	"path/filepath"

	"vrclib/internal/core"
)

// On other systems (tests) the tools are wherever the environment says.
func FindAlcom() string    { return os.Getenv("VRCLIB_ALCOM") }
func FindVCC() string      { return os.Getenv("VRCLIB_VCC") }
func FindUnityHub() string { return os.Getenv("VRCLIB_UNITYHUB") }
func alcomConfigured() bool {
	return os.Getenv("VRCLIB_ALCOM") != "" || core.StatOK(filepath.Join(vccDir(), "vrc-get", "gui-config.json"))
}

func LaunchApp(exe string, args ...string) error {
	core.Logf("launch %s %v", exe, args)
	if os.Getenv("VRCLIB_LAUNCH_REAL") == "" {
		return nil
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
