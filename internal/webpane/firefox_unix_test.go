//go:build !windows

package webpane

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A Firefox that starts but whose remote interface cannot be reached is ended, not left running.
func TestFirefoxEndedWhenSessionFails(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	exe := filepath.Join(dir, "firefox")
	// says it listens where nothing does, then stays around like a browser would
	script := "#!/bin/sh\necho $$ > " + pidFile + "\necho 'WebDriver BiDi listening on ws://127.0.0.1:1' >&2\nexec sleep 60\n"
	if err := os.WriteFile(exe, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := StartFirefoxDriver(exe, filepath.Join(dir, "profile"), "about:blank", nil); err == nil {
		t.Fatal("a session with nothing listening")
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	for i := 0; i < 200; i++ {
		if syscall.Kill(pid, 0) != nil {
			return // gone
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("the browser (pid %d) was left running", pid)
}
