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

func hidden(cmd *exec.Cmd) *exec.Cmd { return cmd }

// recycleFiles: outside Windows there is no Recycle Bin to use; the files are removed.
func recycleFiles(paths []string) error {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// defaultArcExe: the program Windows opens this kind of file with (none elsewhere).
func defaultArcExe(ext string) string { return "" }
