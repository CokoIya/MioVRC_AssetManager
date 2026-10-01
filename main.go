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
)

var appVersion = "1.6.1" // a var so test builds can set it with -ldflags -X

var updatedFrom string // the version this run was updated from (shown once in the window)

var logger *log.Logger

func logf(format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}

func main() {
	scanOnly := flag.Bool("scan-only", false, "扫描后写入 library.json 并退出")
	noBooth := flag.Bool("no-booth", false, "不抓取 Booth 信息")
	noWindow := flag.Bool("no-window", false, "不打开窗口（只启动服务）")
	port := flag.Int("port", 47821, "本地端口")
	data := flag.String("data", "", "数据文件夹（默认 exe 所在文件夹）")
	waitPid := flag.Int("wait-pid", 0, "更新后：等这个进程退出再启动")
	flag.StringVar(&updatedFrom, "updated-from", "", "更新后：之前的版本")
	flag.Parse()
	if *waitPid > 0 || updatedFrom != "" {
		afterUpdate(*waitPid)
	}

	exe, _ := os.Executable()
	if *data != "" {
		dataDir = *data
	} else {
		dataDir = resolveDataDir(filepath.Dir(exe))
	}
	_ = os.MkdirAll(dataDir, 0755)
	if lf, err := os.OpenFile(filepath.Join(dataDir, "library.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		logger = log.New(lf, "", log.LstdFlags)
	} else {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	logf("启动 v%s data=%s", appVersion, dataDir)
	st := LoadStore(filepath.Join(dataDir, "library.json"))
	if !st.Settings.SetupDone {
		st.NotesSeen = appVersion // a first install: nothing to tell about changes
	}

	if *scanOnly {
		RunPipeline(st, true, true, !*noBooth, false, nil)
		logf("scan-only 完成：%d 个素材", len(st.Assets))
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
			logf("无法监听端口: %v", err)
			return
		}
	}
	url := "http://" + ln.Addr().String() + "/"
	logf("服务地址 %s", url)
	srv := &http.Server{Handler: newMux(st)}
	go func() { _ = srv.Serve(ln) }()
	go autoCheckUpdate(st)
	go watchRoots(st)
	go syncLoop(st)
	// an update has started the new exe: hand over to it
	go func() {
		<-updateDoneCh
		_ = st.Save()
		logf("退出（更新）")
		os.Exit(0)
	}()

	// first run waits for the setup screen; afterwards refresh in the background on every start
	if st.Settings.SetupDone && (st.LastScan == 0 || !st.Settings.ManualRescan) {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			StartPipeline(st, true, true, st.Settings.AutoBooth, false, nil)
		}()
	}

	quit := make(chan struct{})
	var quitOnce sync.Once
	stop := func() { quitOnce.Do(func() { close(quit) }) }
	// heartbeat: exit when the page has been closed for a while
	go func() {
		for {
			time.Sleep(5 * time.Second)
			if everPing.Load() && time.Now().Unix()-lastPing.Load() > 75 && !pipelineBusy() && !purchaseBusy.Load() {
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
	for i := 0; i < 600 && pipelineBusy(); i++ {
		time.Sleep(time.Second)
	}
	_ = st.Save()
	logf("退出")
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
		b := defaultBrowserExe()
		if !isChromiumExe(b) {
			b = findEdge()
		}
		if b != "" {
			cmd := appWindow(b, url, filepath.Join(dataDir, "webview"))
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
	_ = openURL(url)
}
