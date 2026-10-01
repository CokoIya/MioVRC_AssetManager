//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// On Windows (or Wine): the drive list, free space and the default archive program come from the system.
func TestWinDrives(t *testing.T) {
	sys := systemDrive()
	ds := fixedDrives()
	t.Logf("system %q, fixed %v", sys, ds)
	if sys == "" || len(ds) == 0 {
		t.Fatalf("no drives: %q %v", sys, ds)
	}
	for _, d := range ds {
		t.Logf("%s free %d", d, diskFree(d))
	}
	t.Logf("default for .zip %q, .7z %q, .rar %q", defaultArcExe(".zip"), defaultArcExe(".7z"), defaultArcExe(".rar"))
	tmp := t.TempDir()
	dir := autoDownloadDir([]string{filepath.Join(tmp, "assets")})
	t.Logf("auto download dir with a root on %s: %q", tmp[:2], dir)
	if dir == "" {
		t.Error("no download dir")
	}
	p := filepath.Join(tmp, "Proj")
	_ = os.MkdirAll(filepath.Join(p, "Temp"), 0755)
	lock := filepath.Join(p, "Temp", "UnityLockfile")
	_ = os.WriteFile(lock, nil, 0644)
	if projectRunning(p) {
		t.Error("a lock file nobody holds: not open")
	}
}
