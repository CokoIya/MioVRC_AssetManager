package pandl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/purchases"
	"vrclib/internal/testkit"
)

func quickRetries(t *testing.T) {
	t.Helper()
	unit, stall := panRetryUnit, purchases.StallAfter
	panRetryUnit, purchases.StallAfter = time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { panRetryUnit, purchases.StallAfter = unit, stall })
}

// Cancel pressed while the server cannot be reached: the download gives up at once, not after forty tries.
func TestPanDownloadCancelled(t *testing.T) {
	core.DataDir = t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close() // nothing listens any more: connection refused
	t.Setenv("VRCLIB_PCS_BASE", addr)
	b := NewBDClient(&core.Store{}, &baiduSession{Cookies: []core.SavedCookie{{Name: "BDUSS", Value: "ok"}}})
	ctx, cancel := context.WithCancel(context.Background())
	b.ctx = ctx
	done := make(chan error, 1)
	go func() {
		done <- b.download(bdFile{Path: "/a.zip", Rel: "a.zip", Size: 100}, filepath.Join(t.TempDir(), "a.zip"), func(int64) {})
	}()
	time.Sleep(50 * time.Millisecond) // it is waiting between two tries by now
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errPanCancelled) {
			t.Errorf("cancelled: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("still trying after cancel")
	}
	// the waits of a save in the netdisk end the same way
	if err := b.waitTask("1", "转存到网盘"); !errors.Is(err, errPanCancelled) {
		t.Errorf("waiting for a save: %v", err)
	}
}

// What the disk refuses ends the job with a word about it, after one request instead of forty.
func TestPanDownloadLocalErrors(t *testing.T) {
	quickRetries(t)
	core.DataDir = t.TempDir()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var a, z int
		fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &z)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, z, 1<<20))
		w.WriteHeader(206)
		_, _ = w.Write([]byte(strings.Repeat("A", z-a+1)))
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_PCS_BASE", srv.URL)
	b := NewBDClient(&core.Store{}, &baiduSession{Cookies: []core.SavedCookie{{Name: "BDUSS", Value: "ok"}}})
	local := filepath.Join(t.TempDir(), "a.zip")
	_ = os.MkdirAll(filepath.Join(local+".part", "x"), 0755) // the part file cannot be opened for writing
	err := b.download(bdFile{Path: "/a.zip", Rel: "a.zip", Size: 1 << 20}, local, func(int64) {})
	if !purchases.IsLocalErr(err) || !strings.Contains(err.Error(), "无法写入「") || hits.Load() != 1 {
		t.Errorf("%v after %d requests", err, hits.Load())
	}
	free := purchases.DiskFree
	purchases.DiskFree = func(string) uint64 { return 10 }
	defer func() { purchases.DiskFree = free }()
	hits.Store(0)
	err = b.download(bdFile{Path: "/b.zip", Rel: "b.zip", Size: 100}, filepath.Join(t.TempDir(), "b.zip"), func(int64) {})
	if !purchases.IsLocalErr(err) || !strings.Contains(err.Error(), "磁盘空间不足") || hits.Load() != 0 {
		t.Errorf("a full disk: %v after %d requests", err, hits.Load())
	}
}

// A server that accepts the connection and says nothing: the try is given up and counted, the job goes on.
func TestPanDownloadSilentServer(t *testing.T) {
	quickRetries(t)
	core.DataDir = t.TempDir()
	var hits atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			select { // the first connection never answers
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("Content-Range", "bytes 0-4/5")
		w.WriteHeader(206)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()
	defer close(release)
	t.Setenv("VRCLIB_PCS_BASE", srv.URL)
	b := NewBDClient(&core.Store{}, &baiduSession{Cookies: []core.SavedCookie{{Name: "BDUSS", Value: "ok"}}})
	local := filepath.Join(t.TempDir(), "a.txt")
	if err := b.download(bdFile{Path: "/a.txt", Rel: "a.txt", Size: 5}, local, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(local); string(got) != "hello" || hits.Load() != 2 {
		t.Errorf("file %q after %d requests", got, hits.Load())
	}
}

func emptyPanQueue(t *testing.T) int32 {
	t.Helper()
	idle := func() bool { panDLMu.Lock(); defer panDLMu.Unlock(); return !panDLRunning }
	clear := func() {
		panDLMu.Lock()
		keepPanLocked(func(*PanJob) bool { return true })
		panDLRunning = false
		panDLMu.Unlock()
	}
	clear()
	run := runPan
	t.Cleanup(func() {
		CancelAll()
		waitUntil(t, "the worker to stop", idle)
		runPan = run
		clear()
	})
	return core.Downloading.Load()
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// More shares queued than the list keeps: every one of them waits its turn; core.Downloading counts each
// from being queued until it is over — downloaded, failed, cancelled or crashed — and ends at zero.
func TestPanQueueAndDownloadingCount(t *testing.T) {
	base := emptyPanQueue(t)
	st := testkit.NewStore(t)
	const n = 70
	st.User["pan:1share"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1share"}
	key := func(i int) string { return fmt.Sprintf("pan:1share#/product %02d", i) } // the products of one collection
	var ran atomic.Int32
	gate := make(chan struct{})
	runPan = func(ctx context.Context, st *core.Store, j *PanJob) error {
		k := ran.Add(1)
		switch {
		case k == 1:
			<-gate // busy with the first one while the others are queued
			setPan(j, func(j *PanJob) { j.Stage = "done" })
			return nil
		case k == 2:
			return errors.New("分享已失效或被取消")
		case k == 3:
			panic("a bug in the middle of a job")
		case k == 6:
			<-ctx.Done()
			return errPanCancelled
		}
		setPan(j, func(j *PanJob) { j.Stage = "download" })
		setPan(j, func(j *PanJob) { j.Stage = "unpack" })
		setPan(j, func(j *PanJob) { j.Stage = "done" })
		return nil
	}
	for i := 0; i < n; i++ {
		if err := QueuePanDownload(st, key(i), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := QueuePanDownload(st, key(5), nil, nil); err == nil {
		t.Error("queued twice")
	}
	if got := len(PanJobsSnapshot()); got != n {
		t.Fatalf("%d of %d jobs are in the queue", got, n)
	}
	if c := core.Downloading.Load() - base; c != n {
		t.Errorf("%d downloads counted, want %d", c, n)
	}
	close(gate)
	waitUntil(t, "job 6 to run", func() bool { return ran.Load() >= 6 })
	CancelPanDownloads() // job 6 is running, the rest wait
	waitUntil(t, "the worker to stop", func() bool { panDLMu.Lock(); defer panDLMu.Unlock(); return !panDLRunning })
	stages := map[string]int{}
	for _, j := range PanJobsSnapshot() {
		stages[j.Stage+" "+j.Err]++
	}
	if stages["done "] != 3 || stages["failed 分享已失效或被取消"] != 1 || stages["failed 下载出错，详见 library.log"] != 1 || stages["failed 已取消"] != n-5 {
		t.Errorf("jobs: %v", stages)
	}
	if c := core.Downloading.Load() - base; c != 0 {
		t.Errorf("%d downloads still counted", c)
	}
	// a job queued after the cancel runs, and finished ones make room in the list
	if err := QueuePanDownload(st, key(0), nil, nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the new job", func() bool { panDLMu.Lock(); defer panDLMu.Unlock(); return !panDLRunning })
	jobs := PanJobsSnapshot()
	if len(jobs) != panKeep || jobs[len(jobs)-1].Key != key(0) || jobs[len(jobs)-1].Stage != "done" || core.Downloading.Load() != base {
		t.Errorf("%d jobs listed, last %+v, %d counted", len(jobs), jobs[len(jobs)-1], core.Downloading.Load()-base)
	}
}
