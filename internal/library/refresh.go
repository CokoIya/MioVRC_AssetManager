package library

import (
	"sync"

	"vrclib/internal/booth"
	"vrclib/internal/core"
)

var (
	pipeMu     sync.Mutex
	pipeBusy   bool
	boothAgain bool
	scanAgain  bool
	usageAgain bool
)

// StartPipeline runs scan → usage → booth in the background (only one at a time).
func StartPipeline(st *core.Store, doScan, doUsage, doBooth bool, forceBooth bool, boothOnly []string) bool {
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

func RunPipeline(st *core.Store, doScan, doUsage, doBooth bool, forceBooth bool, boothOnly []string) {
	if doScan {
		core.RunTask(core.TaskScan, func() { RunFolderScan(st, core.TaskScan) })
		_ = st.Save()
		core.BumpRev()
	}
	if doUsage {
		core.RunTask(core.TaskUsage, func() { RunUsageScan(st, core.TaskUsage) })
		_ = st.Save()
		core.BumpRev()
	}
	if doBooth && len(boothOnly) == 0 {
		core.RunTask(booth.TaskMatch, func() { RunAutoMatch(st, booth.TaskMatch) })
		_ = st.Save()
		core.BumpRev()
	}
	if doBooth {
		core.RunTask(core.TaskBooth, func() { booth.RunBoothFetch(st, core.TaskBooth, forceBooth, boothOnly) })
		_ = st.Save()
		core.BumpRev()
	}
	KickTranslate(st)
}

func PipelineBusy() bool {
	pipeMu.Lock()
	defer pipeMu.Unlock()
	return pipeBusy
}
