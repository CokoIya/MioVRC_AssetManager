package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/pandl"
	"vrclib/internal/server"
	"vrclib/internal/update"
	"vrclib/internal/webpane"
)

func main() {
	scanOnly := flag.Bool("scan-only", false, "扫描后写入 library.json 并退出")
	noBooth := flag.Bool("no-booth", false, "不抓取 Booth 信息")
	noWindow := flag.Bool("no-window", false, "不打开窗口（只启动服务）")
	port := flag.Int("port", 47821, "本地端口")
	data := flag.String("data", "", "数据文件夹（默认 exe 所在文件夹）")
	waitPid := flag.Int("wait-pid", 0, "更新后：等这个进程退出再启动")
	flag.StringVar(&core.UpdatedFrom, "updated-from", "", "更新后：之前的版本")
	flag.Parse()
	if *waitPid > 0 || core.UpdatedFrom != "" {
		update.AfterUpdate(*waitPid)
	}

	exe, _ := os.Executable()
	if *data != "" {
		core.DataDir = *data
	} else {
		core.DataDir = core.ResolveDataDir(filepath.Dir(exe))
	}
	_ = os.MkdirAll(core.DataDir, 0755)
	if lf, err := core.OpenLog(filepath.Join(core.DataDir, "library.log")); err == nil {
		core.Logger = log.New(lf, "", log.LstdFlags)
	} else {
		core.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	core.Logf("启动 v%s data=%s", core.AppVersion, core.DataDir)
	go refreshShellIcon()      // after an update: the taskbar and the shortcuts show the new exe's icon
	go update.CleanDownloads() // installers and update packages of earlier updates
	load := func() *core.Store {
		st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
		if !st.Settings.SetupDone {
			st.NotesSeen = core.AppVersion // a first install: nothing to tell about changes
		}
		return st
	}
	st := load()
	uiLang = func() string { st.Mu.RLock(); defer st.Mu.RUnlock(); return st.Settings.Lang }

	if *scanOnly {
		library.RunPipeline(st, true, true, !*noBooth, false, nil)
		core.Logf("scan-only 完成：%d 个素材", len(st.Assets))
		fmt.Printf("done: %d assets\n", len(st.Assets))
		return
	}

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		ours, quitting := otherCopy(addr)
		if ours && !quitting {
			// already running: just bring up another window
			if !*noWindow && !focusExistingWindow() {
				showUI("http://"+addr+"/", st.Settings.WindowMode == "tab", nil)
			}
			return
		}
		if quitting {
			// the copy that holds the port has closed its window and is on its way out: a window opened on its
			// server would go blank in a moment. Wait for it to be gone, then start as usual.
			core.Logf("上一个实例正在退出，等待其结束")
			if ln = waitForPort(addr, 20*time.Second); ln != nil {
				st = load() // with what it saved on its way out
			}
		}
		if ln == nil { // the port belongs to another program (or the old copy will not go): any free port
			if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
				core.Logf("无法监听端口: %v", err)
				return
			}
		}
	}
	url := "http://" + ln.Addr().String() + "/"
	core.Logf("服务地址 %s", url)
	srv := &http.Server{Handler: server.LocalOnly(ln.Addr(), server.NewMux(st))}
	go func() { _ = srv.Serve(ln) }()
	go update.AutoCheckUpdate(st)
	go library.WatchRoots(st)
	go library.SyncLoop(st)
	webpane.DropXianyuLogins() // 闲鱼's login is not the program's to keep any more (webpane/xyview.go)
	go webpane.KeepLoginsLoop()
	// an update has started the new exe: hand over to it
	go func() {
		<-update.UpdateDoneCh
		core.Quitting.Store(true)
		webpane.Pane.KeepLogins()
		_ = st.Save()
		core.Logf("退出（更新）")
		os.Exit(0)
	}()

	// first run waits for the setup screen; afterwards refresh in the background on every start
	if st.Settings.SetupDone && (st.LastScan == 0 || !st.Settings.ManualRescan) {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			library.StartPipeline(st, true, true, st.Settings.AutoBooth, false, nil)
		}()
	}

	quit := make(chan struct{})
	var quitOnce sync.Once
	// stop: the one way out, whoever asks (the window, the browser window, the heartbeat)
	stop := func() {
		quitOnce.Do(func() {
			core.Quitting.Store(true)
			close(quit)
		})
	}
	// heartbeat: without a window of its own, the program exits when the page has been closed for a while
	go func() {
		last := time.Now().Unix()
		for {
			time.Sleep(5 * time.Second)
			now := time.Now().Unix()
			if now-last > 30 {
				server.LastPing.Store(now) // the computer was asleep: the page gets its time to answer again
			}
			last = now
			if pageGone(now) {
				stop()
				return
			}
		}
	}()
	if !*noWindow {
		// the app's own window runs on this (main) thread and returns when it is closed
		showUI(url, st.Settings.WindowMode == "tab", stop)
	}
	<-quit
	// downloads end here (the window asked first): a moment for them to let go of their files
	pandl.CancelAll()
	for i := 0; i < 30 && core.Downloading.Load() > 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	// the background refresh stops at its next step (core.Quitting): a moment for it to get there, then the
	// save. What it has not got to is done at the next start.
	for i := 0; i < 80 && library.PipelineBusy(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	_ = st.Save()
	core.Logf("退出")
}

// nativeUI: the interface is in the program's own window, which ends the program when it is closed.
var nativeUI atomic.Bool

// uiLang: the interface's language ("" Chinese, "en", "ja"), for what is shown outside the page.
var uiLang = func() string { return "" }

// pageGone: has the page in the browser been closed for good? Never said of the program's own window (it
// decides by itself), nor while something is still being downloaded or synchronised.
func pageGone(now int64) bool {
	if nativeUI.Load() || !server.EverPing.Load() || now-server.LastPing.Load() <= 75 {
		return false
	}
	return !library.PipelineBusy() && !core.PurchaseBusy.Load() && core.Downloading.Load() <= 0
}

// otherCopy asks whoever holds the port: is it this program, and is it shutting down?
func otherCopy(addr string) (ours, quitting bool) {
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + addr + "/api/ping")
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	var p struct {
		App      string `json:"app"`
		Quitting bool   `json:"quitting"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&p) != nil || p.App != "vrclib" {
		return false, false // some other program's server
	}
	return true, p.Quitting
}

// waitForPort listens on addr as soon as its holder has gone; nil when it has not within the time given.
func waitForPort(addr string, patience time.Duration) net.Listener {
	for end := time.Now().Add(patience); time.Now().Before(end); time.Sleep(250 * time.Millisecond) {
		if ln, err := net.Listen("tcp", addr); err == nil {
			return ln
		}
	}
	return nil
}

// minWindowSize: the smallest the window can be dragged to — 55 % × 60 % of its first size, and not below
// the 800 × 560 (at 100 % scaling) the layout needs, but never more than that first size itself.
func minWindowSize(w, h, dpi int) (int, int) {
	return min(w, max(w*55/100, 800*dpi/96)), min(h, max(h*60/100, 560*dpi/96))
}

// showUI opens the interface. Preferred: the app's own window (WebView2), which blocks until closed and
// then calls done. Otherwise a Chromium "app" window (the default browser when it is Chromium-based,
// else Edge, which every Windows 10/11 has), and as a last resort a tab in the default browser —
// those two leave quitting to the heartbeat (pageGone). Links inside the app always go to the default browser.
func showUI(url string, tabOnly bool, done func()) {
	if !tabOnly {
		if runNativeWindow(url) {
			if done != nil {
				done()
			}
			return
		}
		b := core.DefaultBrowserExe()
		if !webpane.IsChromiumExe(b) {
			b = core.FindEdge()
		}
		if b != "" {
			cmd := core.AppWindow(b, url, filepath.Join(core.DataDir, "webview"))
			if err := cmd.Start(); err == nil {
				go func() {
					start := time.Now()
					_ = cmd.Wait()
					// a quick return means the browser handed the window to a running instance: rely on the heartbeat.
					// So do downloads under way: they go on, and the heartbeat ends the program after them
					if time.Since(start) > 8*time.Second && done != nil && core.Downloading.Load() <= 0 {
						done()
					}
				}()
				return
			}
		}
	}
	_ = core.OpenURL(url)
}
