package main

import (
	"sync"
	"time"
)

type Task struct {
	mu      sync.Mutex
	Name    string `json:"name"`
	Label   string `json:"label"`
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Msg     string `json:"msg"`
	Started int64  `json:"started"`
	Ended   int64  `json:"ended"`
}

func (t *Task) Set(done, total int, msg string) {
	t.mu.Lock()
	t.Done, t.Total, t.Msg = done, total, msg
	t.mu.Unlock()
}

func (t *Task) snapshot() Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Task{Name: t.Name, Label: t.Label, Running: t.Running, Done: t.Done, Total: t.Total, Msg: t.Msg, Started: t.Started, Ended: t.Ended}
}

var (
	taskScan   = &Task{Name: "scan", Label: "扫描文件夹"}
	taskUsage  = &Task{Name: "usage", Label: "分析工程使用情况"}
	taskBooth  = &Task{Name: "booth", Label: "抓取 Booth 信息"}
	pipeMu     sync.Mutex
	pipeBusy   bool
	boothAgain bool
	scanAgain  bool
	usageAgain bool
	revision   int64 = 1
	revMu      sync.Mutex
)

func bumpRev() {
	revMu.Lock()
	revision++
	revMu.Unlock()
}

func curRev() int64 {
	revMu.Lock()
	defer revMu.Unlock()
	return revision
}

func run(t *Task, f func()) {
	t.mu.Lock()
	t.Running, t.Done, t.Total, t.Msg, t.Started = true, 0, 0, "", time.Now().Unix()
	t.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			logf("任务 %s 出错: %v", t.Name, r)
			t.Set(0, 0, "出错了，详见 library.log")
		}
		t.mu.Lock()
		t.Running, t.Ended = false, time.Now().Unix()
		t.mu.Unlock()
		bumpRev()
	}()
	f()
}

// StartPipeline runs scan → usage → booth in the background (only one at a time).
func StartPipeline(st *Store, doScan, doUsage, doBooth bool, forceBooth bool, boothOnly []string) bool {
	pipeMu.Lock()
	if pipeBusy {
		if doBooth && len(boothOnly) == 0 {
			boothAgain = true // Booth matching / fetching runs again when the current pipeline ends
		}
		scanAgain = scanAgain || doScan // a download or an import finished meanwhile: look again after
		usageAgain = usageAgain || doUsage
		pipeMu.Unlock()
		return false
	}
	pipeBusy = true
	pipeMu.Unlock()
	go func() {
		defer func() {
			pipeMu.Lock()
			pipeBusy = false
			pipeMu.Unlock()
		}()
		RunPipeline(st, doScan, doUsage, doBooth, forceBooth, boothOnly)
		for {
			pipeMu.Lock()
			again, scan, usage := boothAgain, scanAgain, usageAgain
			boothAgain, scanAgain, usageAgain = false, false, false
			pipeMu.Unlock()
			if !again && !scan && !usage {
				break
			}
			RunPipeline(st, scan, usage || scan, again, false, nil)
		}
	}()
	return true
}

func RunPipeline(st *Store, doScan, doUsage, doBooth bool, forceBooth bool, boothOnly []string) {
	if doScan {
		run(taskScan, func() { RunFolderScan(st, taskScan) })
		_ = st.Save()
		bumpRev()
	}
	if doUsage {
		run(taskUsage, func() { RunUsageScan(st, taskUsage) })
		_ = st.Save()
		bumpRev()
	}
	if doBooth && len(boothOnly) == 0 {
		run(taskMatch, func() { RunAutoMatch(st, taskMatch) })
		_ = st.Save()
		bumpRev()
	}
	if doBooth {
		run(taskBooth, func() { RunBoothFetch(st, taskBooth, forceBooth, boothOnly) })
		_ = st.Save()
		bumpRev()
	}
	KickTranslate(st)
}

func pipelineBusy() bool {
	pipeMu.Lock()
	defer pipeMu.Unlock()
	return pipeBusy
}
