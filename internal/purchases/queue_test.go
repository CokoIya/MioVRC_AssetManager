package purchases

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
)

// emptyQueue: a queue of its own for a test, and how many downloads the others left counted.
func emptyQueue(t *testing.T) int32 {
	t.Helper()
	clear := func() {
		dlMu.Lock()
		for len(dlJobs) > 0 {
			forgetLocked(0)
		}
		dlRunning = false
		dlMu.Unlock()
	}
	clear()
	run := runDownload
	t.Cleanup(func() {
		CancelDownloads()
		// (a worker that found the queue empty still saves the library and starts the rescan: all of that is
		// over before the test's folders go and the next test begins)
		waitFor(t, "the worker to stop", func() bool { dlMu.Lock(); defer dlMu.Unlock(); return !dlRunning && dlWorkers.Load() == 0 })
		settleBackground()
		runDownload = run
		clear()
	})
	return core.Downloading.Load()
}

// settleBackground waits for the download worker to end (after its last job it still saves the library and
// starts the rescan), for that rescan — or the one a taken-in file started — and for what follows it.
func settleBackground() {
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && (dlWorkers.Load() > 0 || library.BackgroundBusy()); {
		time.Sleep(5 * time.Millisecond)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// "下载全部未下载的已购" with more files than the list keeps: every one of them waits its turn, and the number
// given back is what was queued. Only finished jobs make room.
func TestQueueKeepsWaitingJobs(t *testing.T) {
	base := emptyQueue(t)
	st := testkit.NewStore(t)
	for i := 0; i < 130; i++ {
		id := fmt.Sprintf("%07d", 1000000+i)
		st.Purchases[id] = &core.Purchase{ID: id, Name: "item " + id, Files: []string{"a.zip", "b.zip"}, Downloads: []string{id + "0", id + "1"}}
	}
	dlMu.Lock()
	for i := 0; i < 150; i++ { // what was downloaded earlier today
		dlJobs = append(dlJobs, &DLJob{ID: fmt.Sprintf("old%d", i), Status: "done"})
	}
	dlRunning = true // a worker is busy with something: nothing is taken off the queue meanwhile
	dlMu.Unlock()
	total := 0
	for _, id := range core.SortedKeys(st.Purchases) {
		n, _ := QueueDownloads(st, id, nil)
		total += n
	}
	queued, done := 0, 0
	snap := DLSnapshot()
	for _, j := range snap {
		switch j.Status {
		case "queued":
			queued++
		case "done":
			done++
		}
	}
	if total != 260 || queued != 260 || done != 0 || snap[0].Item != "1000000" {
		t.Errorf("reported %d, waiting %d, finished kept %d, first %s", total, queued, done, snap[0].Item)
	}
	if n := core.Downloading.Load() - base; n != 260 {
		t.Errorf("%d downloads counted", n)
	}
	// asking for the same files again adds nothing
	if n, _ := QueueDownloads(st, "1000000", nil); n != 0 {
		t.Errorf("queued twice: %d", n)
	}
	dlMu.Lock()
	dlRunning = false
	dlMu.Unlock()
	CancelDownloads()
	if n := core.Downloading.Load() - base; n != 0 {
		t.Errorf("%d downloads still counted after cancelling", n)
	}
}

// core.Downloading goes up for every job queued and comes down once however the job ends — downloaded,
// failed, cancelled, waiting for a login, or a crash in the middle — and the queue is not left stuck.
func TestDownloadingReturnsToZero(t *testing.T) {
	base := emptyQueue(t)
	st := testkit.NewStore(t)
	st.Settings.HideZh = true // (no translation of names behind the test: it would go out to the net)
	started := make(chan string, 8)
	gate := make(chan struct{}) // (the worker does not begin before the count of what was queued has been read)
	runDownload = func(ctx context.Context, st *core.Store, j *DLJob) error {
		<-gate
		started <- j.ID
		switch j.ID {
		case "1":
			setJob(j, func(j *DLJob) { j.Status = "unpacking" })
			setJob(j, func(j *DLJob) { j.Status = "done" })
			return nil
		case "2":
			return errors.New("Booth 上未找到该文件（可能已删除）")
		case "3":
			panic("a bug in the middle of a download")
		case "4":
			<-ctx.Done()
			return ErrCancelled
		case "6":
			return errGumLogin
		}
		setJob(j, func(j *DLJob) { j.Status = "done" })
		return nil
	}
	var add []*DLJob
	for i := 1; i <= 5; i++ {
		add = append(add, &DLJob{ID: fmt.Sprint(i), Item: "9", Name: fmt.Sprintf("f%d.zip", i), Status: "queued"})
	}
	n, counted := enqueueDownloads(st, add), core.Downloading.Load()-base
	close(gate)
	if n != 5 || counted != 5 {
		t.Fatalf("queued %d, counted %d", n, counted)
	}
	for _, want := range []string{"1", "2", "3", "4"} { // the crash in 3 did not stop the queue
		select {
		case id := <-started:
			if id != want {
				t.Fatalf("job %s started, want %s", id, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("job %s never started", want)
		}
	}
	CancelDownloads() // 4 is running, 5 waits
	waitFor(t, "the worker to stop", func() bool { dlMu.Lock(); defer dlMu.Unlock(); return !dlRunning })
	status := map[string]string{}
	for _, j := range DLSnapshot() {
		status[j.ID] = j.Status + " " + j.Err
	}
	if status["1"] != "done " || status["2"] != "failed Booth 上未找到该文件（可能已删除）" || status["3"] != "failed 下载出错，详见 library.log" ||
		status["4"] != "failed 已取消" || status["5"] != "failed 已取消" {
		t.Errorf("jobs %v", status)
	}
	if n := core.Downloading.Load() - base; n != 0 {
		t.Errorf("%d downloads still counted", n)
	}
	// what is queued after a cancel runs; a login that is gone parks the queue without counting it as running
	if enqueueDownloads(st, []*DLJob{{ID: "7", Status: "queued"}, {ID: "6", Status: "queued"}, {ID: "8", Status: "queued"}}) != 3 {
		t.Fatal("not queued after a cancel")
	}
	waitFor(t, "the queue to run again", func() bool { dlMu.Lock(); defer dlMu.Unlock(); return !dlRunning })
	for _, j := range DLSnapshot() {
		status[j.ID] = j.Status
	}
	if status["7"] != "done" || status["6"] != "failed" || status["8"] != "done" || core.Downloading.Load() != base {
		t.Errorf("after the cancel: %v, %d counted", status, core.Downloading.Load()-base)
	}
	// the scan that follows a download, and what follows the scan: over before the data folder goes
	for i, idle := 0, 0; i < 400 && idle < 3; i++ {
		if library.BackgroundBusy() {
			idle = 0
		} else {
			idle++
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Jobs that wait for the player to log in are not downloads in progress: the program may close.
func TestLoginWaitIsNotCounted(t *testing.T) {
	base := emptyQueue(t)
	st := testkit.NewStore(t)
	t.Setenv("VRCLIB_DEFAULT_BROWSER", "") // no browser to check the login with
	t.Setenv("VRCLIB_BROWSER", "")
	var gone atomic.Bool
	gone.Store(true)
	runDownload = func(ctx context.Context, st *core.Store, j *DLJob) error {
		if gone.Load() {
			return errNeedLogin
		}
		setJob(j, func(j *DLJob) { j.Status = "done" })
		return nil
	}
	enqueueDownloads(st, []*DLJob{{ID: "1", Status: "queued"}, {ID: "2", Status: "queued"}})
	waitFor(t, "the queue to park", func() bool { return DLNeedLogin() })
	waitFor(t, "the worker to stop", func() bool { dlMu.Lock(); defer dlMu.Unlock(); return !dlRunning })
	if n := core.Downloading.Load() - base; n != 0 {
		t.Errorf("%d counted while waiting for the login", n)
	}
	gone.Store(false)
	ResumeDownloads(st)
	waitFor(t, "the downloads to go on", func() bool {
		n := 0
		for _, j := range DLSnapshot() {
			if j.Status == "done" {
				n++
			}
		}
		return n == 2
	})
	waitFor(t, "the worker to stop", func() bool { dlMu.Lock(); defer dlMu.Unlock(); return !dlRunning })
	if n := core.Downloading.Load() - base; n != 0 {
		t.Errorf("%d still counted", n)
	}
	for i := 0; i < 400 && library.PipelineBusy(); i++ {
		time.Sleep(25 * time.Millisecond)
	}
}
