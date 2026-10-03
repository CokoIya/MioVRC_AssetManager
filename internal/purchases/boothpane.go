package purchases

import (
	"time"

	"vrclib/internal/core"
	"vrclib/internal/webpane"
)

func hasBoothSession(cs []core.SavedCookie) bool {
	for _, c := range cs {
		if c.Name == boothSessionCookie && c.Value != "" {
			return true
		}
	}
	return false
}

// runBoothPane: the Booth sync (or, quiet, only a login check) inside the pane.
func runBoothPane(st *core.Store, prog *core.Task, quiet bool) bool {
	start := core.BoothAccountsBase() + "/library"
	if quiet {
		inUse := webpane.Pane.Shown()
		webpane.Pane.Mu.Lock()
		running := webpane.Pane.Port > 0
		webpane.Pane.Mu.Unlock()
		if running {
			if cs, err := webpane.Pane.Cookies(webpane.BoothCookieURLs()); err == nil && hasBoothSession(cs) {
				_ = saveBoothSession(cs)
				if inUse {
					return true
				}
			}
		}
		if inUse {
			return false // the player is using the page: do not take it away
		}
		if err := webpane.Pane.Open(start, "booth", false); err != nil {
			core.Logf("Booth 登录检查：%v", err)
			return false
		}
		defer webpane.Pane.CloseHidden()
	} else {
		prog.Set(0, 0, "正在打开 Booth 页面…")
		if err := webpane.Pane.Open(start, "booth", true); err != nil {
			core.Logf("Booth 页面打不开: %v", err)
			prog.Set(0, 0, "无法打开 Booth 页面："+err.Error())
			return false
		}
	}
	closeWin := false
	msg := "请在软件内的 Booth 页面登录，登录后自动同步已购"
	if webpane.PaneMode() == "window" {
		msg = "请在打开的 Booth 窗口中登录，登录后自动同步已购"
	}
	return boothLoop(st, prog, &webpane.PaneDriver{Start: time.Now()}, quiet, msg, &closeWin)
}
