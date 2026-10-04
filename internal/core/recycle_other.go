//go:build !windows

package core

import "os"

// RecycleOnly: outside Windows (development and tests) there is no Recycle Bin; files and folders are removed.
func RecycleOnly(paths []string) error {
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}
