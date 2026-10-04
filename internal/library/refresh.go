package library

import (
	"sync"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/translate"
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
			if (!again && !scan && !usage) || core.Quitting.Load() {
				break
			}
			RunPipeline(st, scan, usage || scan, again, false, nil)
		}
	}()
	return true
}

// RunPipeline does the steps asked for, saving after each. Once the program is quitting no further step is
// begun, and the one under way stops at its next folder or request without leaving half a result.
func RunPipeline(st *core.Store, doScan, doUsage, doBooth bool, forceBooth bool, boothOnly []string) {
	step := func(t *core.Task, f func()) {
		if core.Quitting.Load() {
			return
		}
		core.RunTask(t, f)
		_ = st.Save()
		core.BumpRev()
	}
	if doScan {
		step(core.TaskScan, func() { RunFolderScan(st, core.TaskScan) })
	}
	if doUsage {
		step(core.TaskUsage, func() { RunUsageScan(st, core.TaskUsage) })
	}
	if doBooth && len(boothOnly) == 0 {
		step(booth.TaskMatch, func() { RunAutoMatch(st, booth.TaskMatch) })
	}
	if doBooth {
		step(core.TaskBooth, func() { booth.RunBoothFetch(st, core.TaskBooth, forceBooth, boothOnly) })
	}
	if !core.Quitting.Load() {
		KickTranslate(st)
	}
	if doScan && !core.Quitting.Load() {
		KickPkgCovers(st) // last, and apart from the pipeline: the window is not kept "scanning" by it
	}
}

// BackgroundBusy: is any of the work a rescan sets going still under way — the pipeline itself, and the look
// into the packages and the translation of names that follow it? (Tests wait for it before their folders go.)
func BackgroundBusy() bool {
	return PipelineBusy() || PkgBatchSnapshot().Running || translate.TransBusy.Load()
}

func PipelineBusy() bool {
	pipeMu.Lock()
	defer pipeMu.Unlock()
	return pipeBusy
}
