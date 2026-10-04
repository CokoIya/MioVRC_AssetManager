//go:build !windows

package core

import "os"

// PickFile: outside Windows (development and tests) the file comes from VRCLIB_PICK_FILE.
func PickFile(title, typeName, pattern string) (string, error) {
	if p := os.Getenv("VRCLIB_PICK_FILE"); p != "" {
		return p, nil
	}
	return "", ErrPickCancelled
}
