package main

import "vrclib/internal/core"

// appStore: the program's library and settings, for what the window does outside the page (the tray, the notices)
var appStore *core.Store

// What the tray says, in the interface's language ("" Chinese, "en", "ja"): it is not the page's, so its other
// languages stand here (as confirmClose's do).

// trayNote: the first time the window goes to the tray, where it went and how it comes back
func trayNote(lang string) (title, text string) {
	switch lang {
	case "en":
		return core.AppName + " is still running", "The window is in the notification area; imports, downloads and the pipeline go on. " +
			"Click the icon to open it again, right-click it to quit. In Settings, the close button can be made to quit instead."
	case "ja":
		return core.AppName + " は実行中です", "ウィンドウは通知領域に格納されました。インポート、ダウンロード、パイプラインは続行されます。" +
			"アイコンをクリックすると再び開き、右クリックで終了できます。設定で、閉じるボタンで終了するようにも変更できます。"
	}
	return core.AppName + " 仍在运行", "窗口已收到通知区域（任务栏右下角），导入、下载和流水线会继续进行。单击图标可重新打开，右键可退出。可在设置中改为关闭窗口时退出程序。"
}

// trayMenuText: the icon's menu
func trayMenuText(lang string) (open, quit string) {
	switch lang {
	case "en":
		return "Open " + core.AppName, "Quit"
	case "ja":
		return core.AppName + " を開く", "終了"
	}
	return "打开 " + core.AppName, "退出"
}
