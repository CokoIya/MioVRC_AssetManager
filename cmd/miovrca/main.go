package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
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
	if lf, err := os.OpenFile(filepath.Join(core.DataDir, "library.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		core.Logger = log.New(lf, "", log.LstdFlags)
	} else {
		core.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	core.Logf("启动 v%s data=%s", core.AppVersion, core.DataDir)
	go refreshShellIcon() // after an update: the taskbar and the shortcuts show the new exe's icon
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	if !st.Settings.SetupDone {
		st.NotesSeen = core.AppVersion // a first install: nothing to tell about changes
	}

	if *scanOnly {
		library.RunPipeline(st, true, true, !*noBooth, false, nil)
		core.Logf("scan-only 完成：%d 个素材", len(st.Assets))
		fmt.Printf("done: %d assets\n", len(st.Assets))
		return
	}

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// already running? just bring up another window
		if c, e := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + addr + "/api/ping"); e == nil {
			c.Body.Close()
			if !*noWindow && !focusExistingWindow() {
				showUI("http://"+addr+"/", st.Settings.WindowMode == "tab", nil)
			}
			return
		}
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			core.Logf("无法监听端口: %v", err)
			return
		}
	}
	url := "http://" + ln.Addr().String() + "/"
	core.Logf("服务地址 %s", url)
	srv := &http.Server{Handler: server.NewMux(st)}
	go func() { _ = srv.Serve(ln) }()
	go update.AutoCheckUpdate(st)
	go library.WatchRoots(st)
	go library.SyncLoop(st)
	go webpane.KeepLoginsLoop()
	// an update has started the new exe: hand over to it
	go func() {
		<-update.UpdateDoneCh
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
	stop := func() { quitOnce.Do(func() { close(quit) }) }
	// heartbeat: exit when the page has been closed for a while
	go func() {
		for {
			time.Sleep(5 * time.Second)
			if server.EverPing.Load() && time.Now().Unix()-server.LastPing.Load() > 75 && !library.PipelineBusy() && !core.PurchaseBusy.Load() {
				select {
				case <-quit:
				default:
					close(quit)
				}
				return
			}
		}
	}()
	if !*noWindow {
		// the app's own window runs on this (main) thread and returns when it is closed
		showUI(url, st.Settings.WindowMode == "tab", stop)
	}
	<-quit
	// let a running pipeline finish its current save
	for i := 0; i < 600 && library.PipelineBusy(); i++ {
		time.Sleep(time.Second)
	}
	_ = st.Save()
	core.Logf("退出")
}

// showUI opens the interface. Preferred: the app's own window (WebView2), which blocks until closed and
// then calls done. Otherwise a Chromium "app" window (the default browser when it is Chromium-based,
// else Edge, which every Windows 10/11 has), and as a last resort a tab in the default browser —
// those two leave quitting to the heartbeat. Links inside the app always go to the default browser.
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
					// a quick return means the browser handed the window to a running instance: rely on the heartbeat
					if time.Since(start) > 8*time.Second && done != nil {
						done()
					}
				}()
				return
			}
		}
	}
	_ = core.OpenURL(url)
}
