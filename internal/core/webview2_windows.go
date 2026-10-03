//go:build windows

package core

import (
	"github.com/jchv/go-webview2/webviewloader"
)

// WebView2Version is the installed WebView2 runtime, "" when there is none.
func WebView2Version() string {
	v, err := webviewloader.GetInstalledVersion()
	if err != nil {
		return ""
	}
	return v
}
