//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

// Non-Windows builds exist only for development and tests.

var errPickCancelled = errors.New("cancelled")

func openInExplorer(p string, isDir bool) error { logf("open %s (dir=%v)", p, isDir); return nil }
func openURL(u string) error                    { logf("open url %s", u); return nil }
func findEdge() string                          { return os.Getenv("VRCLIB_BROWSER") }
func defaultBrowserExe() string                 { return os.Getenv("VRCLIB_DEFAULT_BROWSER") }
func appWindow(browser, url, profile string) *exec.Cmd {
	return exec.Command(browser, "--app="+url)
}
func systemProxy() string  { return "" }
func driveRoots() []string { return nil }
func userDataBase() string {
	d, _ := os.UserConfigDir()
	return d
}
func isInstalledDir(d string) bool                       { return false }
func browserCmd(browser string, args []string) *exec.Cmd { return exec.Command(browser, args...) }
func pickFolder(title, initial string) (string, error) {
	if p := os.Getenv("VRCLIB_PICK"); p != "" {
		return p, nil
	}
	return "", errPickCancelled
}
func createDesktopShortcut() (string, error) { return "", errors.New("只支持 Windows") }
