package ai

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// noticesOf collects what core.Notify hands to the window while the test runs.
func noticesOf(t *testing.T) chan core.Notice {
	ch := make(chan core.Notice, 16)
	old := core.NoticeHook
	core.NoticeHook = func(n core.Notice) { ch <- n }
	t.Cleanup(func() { core.NoticeHook = old })
	return ch
}

func nextNotice(ch chan core.Notice) core.Notice {
	select {
	case n := <-ch:
		return n
	case <-time.After(3 * time.Second):
		return core.Notice{}
	}
}

// A question of the pipeline's is heard as it is put — the player may be looking at Unity —, and the end of a
// long run, well or not; not the end of a short one, nor of one the player stopped.
func TestPipelineNotices(t *testing.T) {
	testkit.NewStore(t)
	ch := noticesOf(t)
	proj := filepath.Join(t.TempDir(), "Manuka")
	s := aiSessionFor(proj)
	done := make(chan bool, 1)
	go func() { done <- s.confirm(context.Background(), "AI 请求删除 Assets/Old", "") }()
	if n := nextNotice(ch); n != (core.Notice{Kind: "ask", What: "pipe", Name: "Manuka"}) {
		t.Errorf("the question: %+v", n)
	}
	for i := 0; i < 200 && !s.answer(true, false); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !<-done {
		t.Error("the answer did not come through")
	}
	long := time.Now().Add(-core.NoticeLongEnough)
	runEnded(proj, nil, false, long)
	if n := nextNotice(ch); n != (core.Notice{Kind: "done", What: "pipe", Name: "Manuka"}) {
		t.Errorf("a long run's end: %+v", n)
	}
	runEnded(proj, errors.New("Unity 没有响应"), false, long)
	if n := nextNotice(ch); n != (core.Notice{Kind: "fail", What: "pipe", Name: "Manuka"}) {
		t.Errorf("a long run that failed: %+v", n)
	}
	runEnded(proj, nil, false, time.Now().Add(-3*time.Second))
	runEnded(proj, errors.New("已停止"), true, long)
	select {
	case n := <-ch:
		t.Errorf("told of a short run or a stopped one: %+v", n)
	case <-time.After(300 * time.Millisecond):
	}
}
