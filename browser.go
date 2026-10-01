package main

import (
	"path/filepath"
	"strings"
)

// Chromium builds that understand --app and --remote-debugging-port the same way Chrome does.
var chromiumExes = map[string]string{
	"chrome.exe": "Chrome", "msedge.exe": "Edge", "brave.exe": "Brave", "vivaldi.exe": "Vivaldi",
	"chromium.exe": "Chromium", "thorium.exe": "Thorium", "supermium.exe": "Supermium", "chrome": "Chrome",
}

func browserLabel(exe string) string {
	if exe == "" {
		return ""
	}
	b := strings.ToLower(filepath.Base(exe))
	if l, ok := chromiumExes[b]; ok {
		return l
	}
	if b == "firefox.exe" || b == "firefox" {
		return "Firefox"
	}
	return strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
}

func isChromiumExe(exe string) bool {
	if exe == "" || !fileExists(exe) {
		return false
	}
	_, ok := chromiumExes[strings.ToLower(filepath.Base(exe))]
	return ok
}

// syncBrowser: the default browser when it can be driven for the Booth window (Chromium-based or
// Firefox), otherwise Edge/Chrome.
func syncBrowser() (exe, note string) {
	def := defaultBrowserExe()
	if isChromiumExe(def) || (isFirefoxExe(def) && fileExists(def)) {
		return def, ""
	}
	exe = findEdge()
	if def != "" && exe != "" {
		note = "默认浏览器（" + browserLabel(def) + "）不支持，Booth 登录窗口改用 " + browserLabel(exe)
	}
	return exe, note
}
