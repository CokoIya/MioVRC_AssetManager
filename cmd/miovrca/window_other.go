//go:build !windows

package main

// Outside Windows (tests) there is no native window: the UI opens in a browser.
func focusExistingWindow() bool       { return false }
func runNativeWindow(url string) bool { return false }

func refreshShellIcon() {}
