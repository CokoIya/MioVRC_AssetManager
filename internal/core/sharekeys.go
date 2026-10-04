package core

import "strings"

// IsNetdiskKey: the card of a share — on Baidu Netdisk ("pan:<surl>"), Google Drive ("gd:<id>") or Dropbox
// ("db:<id>") — or of a product inside one ("…#/path"). IsPanShareKey is the whole share only.
func IsNetdiskKey(key string) bool {
	return strings.HasPrefix(key, "pan:") || strings.HasPrefix(key, "gd:") || strings.HasPrefix(key, "db:")
}
