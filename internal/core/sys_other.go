//go:build !windows

package core

import (
	"errors"
	"os"
	"os/exec"
)

// Non-Windows builds exist only for development and tests.

var ErrPickCancelled = errors.New("cancelled")

func OpenInExplorer(p string, isDir bool) error { Logf("open %s (dir=%v)", p, isDir); return nil }
func OpenURL(u string) error                    { Logf("open url %s", u); return nil }
func FindEdge() string                          { return os.Getenv("VRCLIB_BROWSER") }
func DefaultBrowserExe() string                 { return os.Getenv("VRCLIB_DEFAULT_BROWSER") }
func AppWindow(browser, url, profile string) *exec.Cmd {
	return exec.Command(browser, "--app="+url)
}
func systemProxy() string  { return "" }
func driveRoots() []string { return nil }
func userDataBase() string {
	d, _ := os.UserConfigDir()
	return d
}
func IsInstalledDir(d string) bool                       { return false }
func BrowserCmd(browser string, args []string) *exec.Cmd { return exec.Command(browser, args...) }
func PickFolder(title, initial string) (string, error) {
	if p := os.Getenv("VRCLIB_PICK"); p != "" {
		return p, nil
	}
	return "", ErrPickCancelled
}
func CreateDesktopShortcut() (string, error) { return "", errors.New("只支持 Windows") }

func Hidden(cmd *exec.Cmd) *exec.Cmd { return cmd }

// RecycleFiles: outside Windows there is no Recycle Bin to use; the files are removed.
func RecycleFiles(paths []string) error {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// DefaultArcExe: the program Windows opens this kind of file with (none elsewhere).
func DefaultArcExe(ext string) string { return "" }
