package core

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

// Relabel: another name for the work, for a task that does two kinds of it (called before it runs).
func (t *Task) Relabel(label string) {
	t.mu.Lock()
	t.Label = label
	t.mu.Unlock()
}

func (t *Task) Snapshot() Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Task{Name: t.Name, Label: t.Label, Running: t.Running, Done: t.Done, Total: t.Total, Msg: t.Msg, Started: t.Started, Ended: t.Ended}
}

var (
	TaskScan        = &Task{Name: "scan", Label: "扫描文件夹"}
	TaskUsage       = &Task{Name: "usage", Label: "统计工程使用情况"}
	TaskBooth       = &Task{Name: "booth", Label: "获取 Booth 信息"}
	revision  int64 = 1
	revMu     sync.Mutex
)

func BumpRev() {
	revMu.Lock()
	revision++
	revMu.Unlock()
}

func CurRev() int64 {
	revMu.Lock()
	defer revMu.Unlock()
	return revision
}

func RunTask(t *Task, f func()) {
	t.mu.Lock()
	t.Running, t.Done, t.Total, t.Msg, t.Started = true, 0, 0, "", time.Now().Unix()
	t.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			Logf("任务 %s 出错: %v", t.Name, r)
			t.Set(0, 0, "任务出错，详见 library.log")
		}
		t.mu.Lock()
		t.Running, t.Ended = false, time.Now().Unix()
		t.mu.Unlock()
		BumpRev()
	}()
	f()
}
