//go:build !windows

package update

import (
	"syscall"
	"time"
)

func waitPidExit(pid int, timeout time.Duration) {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func setInstalledVersion(v string) {}
