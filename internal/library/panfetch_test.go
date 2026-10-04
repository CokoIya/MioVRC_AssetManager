package library

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/netdisk"
	"vrclib/internal/testkit"
)

func panStore(t *testing.T, surls ...string) *core.Store {
	t.Helper()
	st := testkit.NewStore(t)
	st.Settings.AutoBooth = false
	if st.Pan == nil {
		st.Pan = map[string]*core.PanListing{}
	}
	for _, s := range surls {
		st.User["pan:"+s] = &core.UserData{ShareURL: "https://pan.baidu.com/s/" + s}
	}
	gap, first := panFetchGap, panPauseFirst
	read := fetchPanListing
	panFetchGap = time.Millisecond
	t.Cleanup(func() {
		waitPanIdle(t)
		netdisk.PanMu.Lock()
		panFetchGap, panPauseFirst, panPauseStep, panPausedUntil = gap, first, 0, time.Time{}
		netdisk.PanMu.Unlock()
		fetchPanListing = read
	})
	return st
}

func waitPanIdle(t *testing.T) {
	t.Helper()
	for i := 0; i < 500; i++ {
		netdisk.PanMu.Lock()
		idle := !netdisk.PanActive && len(netdisk.PanQueue) == 0
		netdisk.PanMu.Unlock()
		if idle && !netdisk.TaskPan.Snapshot().Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the share reader did not finish")
}

// A captcha ends the round: the shares after it are not asked for, stay due, and nothing is read by itself
// until Baidu has had its rest.
func TestPanRoundStopsAtCaptcha(t *testing.T) {
	st := panStore(t, "1aaa", "1bbb", "1ccc")
	st.Pan["1bbb"] = &core.PanListing{Surl: "1bbb", Fetched: 100, Files: []*core.PanFile{{Name: "old.zip", Size: 1}}}
	var mu sync.Mutex
	var asked []string
	fetchPanListing = func(st *core.Store, link, pwd string) (*core.PanListing, error) {
		surl := netdisk.ShareSurl(link)
		mu.Lock()
		asked = append(asked, surl)
		mu.Unlock()
		if surl == "1bbb" {
			return nil, netdisk.ErrPanCaptcha
		}
		return &core.PanListing{Surl: surl, Fetched: time.Now().Unix(), Files: []*core.PanFile{{Name: "a.zip", Size: 5}}, Count: 1}, nil
	}
	if PanFetchPaused() {
		t.Fatal("paused before anything happened")
	}
	QueuePanFetch(st, "pan:1aaa", "pan:1bbb", "pan:1ccc")
	waitPanIdle(t)
	mu.Lock()
	got := len(asked)
	mu.Unlock()
	if got != 2 || !PanFetchPaused() {
		t.Fatalf("asked for %v, paused %v", asked, PanFetchPaused())
	}
	st.Mu.RLock()
	b := st.Pan["1bbb"]
	if st.Pan["1aaa"] == nil || st.Pan["1ccc"] != nil || b.Fetched != 100 || len(b.Files) != 1 || b.Err == "" {
		t.Errorf("listings: bbb %+v, ccc %v", b, st.Pan["1ccc"])
	}
	due := dueShares(st, time.Now())
	st.Mu.RUnlock()
	if len(due) != 2 || due[0] != "pan:1bbb" || due[1] != "pan:1ccc" {
		t.Errorf("still due: %v", due)
	}
}

// A share that is gone is looked at once a week, not every day; a failed connection is tried again the next day.
func TestDueShares(t *testing.T) {
	st := panStore(t, "1dead", "1fine", "1neterr", "1new", "1code")
	now := time.Now()
	ago := func(d time.Duration) int64 { return now.Add(-d).Unix() }
	st.Pan["1dead"] = &core.PanListing{Surl: "1dead", Fetched: ago(3 * 24 * time.Hour), Err: netdisk.PanErrno(105)}
	st.Pan["1code"] = &core.PanListing{Surl: "1code", Fetched: ago(2 * 24 * time.Hour), Err: netdisk.PanErrno(-9)}
	st.Pan["1fine"] = &core.PanListing{Surl: "1fine", Fetched: ago(25 * time.Hour)}
	st.Pan["1neterr"] = &core.PanListing{Surl: "1neterr", Fetched: ago(25 * time.Hour), Err: "无法连接百度网盘，请检查网络"}
	if due := dueShares(st, now); len(due) != 3 || due[0] != "pan:1fine" || due[1] != "pan:1neterr" || due[2] != "pan:1new" {
		t.Errorf("due %v", due)
	}
	st.Pan["1dead"].Fetched = ago(8 * 24 * time.Hour)
	if due := dueShares(st, now); len(due) != 4 || due[0] != "pan:1dead" {
		t.Errorf("after a week: %v", due)
	}
}

// One reader at a time, also when shares are added while a round is ending.
func TestPanFetchOneAtATime(t *testing.T) {
	st := panStore(t, "1aaa", "1bbb", "1ccc")
	var running, most atomic.Int32
	hold := make(chan struct{})
	fetchPanListing = func(st *core.Store, link, pwd string) (*core.PanListing, error) {
		n := running.Add(1)
		for {
			if m := most.Load(); n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		if netdisk.ShareSurl(link) != "1aaa" {
			<-hold
		}
		running.Add(-1)
		return nil, errors.New("无法连接百度网盘，请检查网络")
	}
	QueuePanFetch(st, "pan:1aaa")
	// catch the first reader between finding the queue empty and leaving: what it does next needs the store,
	// so holding the store keeps it there while the second reader is started
	for i := 0; ; i++ {
		st.Mu.Lock()
		netdisk.PanMu.Lock()
		active := netdisk.PanActive
		netdisk.PanMu.Unlock()
		if !active {
			break
		}
		st.Mu.Unlock()
		if i > 5_000_000 {
			t.Fatal("the first round never ended")
		}
		runtime.Gosched()
	}
	QueuePanFetch(st, "pan:1bbb") // a second reader starts
	st.Mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	QueuePanFetch(st, "pan:1ccc") // has to wait for it, not start a third
	time.Sleep(100 * time.Millisecond)
	close(hold)
	waitPanIdle(t)
	if most.Load() != 1 {
		t.Errorf("%d readers at once", most.Load())
	}
}
