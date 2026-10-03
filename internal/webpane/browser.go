package webpane

import (
	"path/filepath"
	"strings"

	"vrclib/internal/core"
)

// Chromium builds that understand --app and --remote-debugging-port the same way Chrome does.
var chromiumExes = map[string]string{
	"chrome.exe": "Chrome", "msedge.exe": "Edge", "brave.exe": "Brave", "vivaldi.exe": "Vivaldi",
	"chromium.exe": "Chromium", "thorium.exe": "Thorium", "supermium.exe": "Supermium", "chrome": "Chrome",
}

func BrowserLabel(exe string) string {
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

func IsChromiumExe(exe string) bool {
	if exe == "" || !core.FileExists(exe) {
		return false
	}
	_, ok := chromiumExes[strings.ToLower(filepath.Base(exe))]
	return ok
}

// SyncBrowser: the default browser when it can be driven for the Booth window (Chromium-based or
// Firefox), otherwise Edge/Chrome.
func SyncBrowser() (exe, note string) {
	def := core.DefaultBrowserExe()
	if IsChromiumExe(def) || (IsFirefoxExe(def) && core.FileExists(def)) {
		return def, ""
	}
	exe = core.FindEdge()
	if def != "" && exe != "" {
		note = "默认浏览器（" + BrowserLabel(def) + "）不支持，Booth 登录窗口改用 " + BrowserLabel(exe)
	}
	return exe, note
}
