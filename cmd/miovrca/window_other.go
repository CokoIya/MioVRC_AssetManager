//go:build !windows

package main

import "vrclib/internal/webpane"

// Outside Windows (tests) there is no native window: the UI opens in a browser.
func focusExistingWindow() bool       { return false }
func runNativeWindow(url string) bool { return false }

// (tests can ask for stand-ins of the views a native window would hold)
func init() { webpane.FakeNative() }

func refreshShellIcon() {}
