package pandl

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/testkit"
)

// Baidu refuses a folder name with emoji in it (errno -7): the copy in the netdisk goes into a folder without
// them, the folder on this disk keeps the whole name.
func TestPanFolderWithEmoji(t *testing.T) {
	f, st := newFakeBaidu(t)
	f.share["/sh/Root"] = &bdNode{fsid: 1, dir: true, mtime: 10}
	f.share["/sh/Root/a.txt"] = &bdNode{fsid: 2, data: []byte("hair"), mtime: 10}
	title := "🌊【VRC Hair】 ♦Neru Hair♦ + Cloth💨🌊"
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: title, Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	local := core.SafeName(naming.CleanName(title), 60)
	if !strings.Contains(local, "🌊") || j.Dir != filepath.Join(st.Settings.DownloadDir, local) {
		t.Errorf("on this disk: %s", j.Dir)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "a.txt" {
		t.Errorf("downloaded: %s", got)
	}
	if tr, _ := f.take(); tr != "[1] -> /MioVRCA/【VRC Hair】 ♦Neru Hair♦ + Cloth" || j.Saved != "/MioVRCA/【VRC Hair】 ♦Neru Hair♦ + Cloth" {
		t.Errorf("saved: %q into %q", tr, j.Saved)
	}
}

// A name Baidu refuses for another reason: the folder in the netdisk is named after the share's link.
func TestPanFolderNameRefused(t *testing.T) {
	f, st := newFakeBaidu(t)
	f.refuse = func(name string) bool { return strings.Contains(name, "♦") }
	f.share["/sh/Root"] = &bdNode{fsid: 1, dir: true, mtime: 10}
	f.share["/sh/Root/a.txt"] = &bdNode{fsid: 2, data: []byte("hair"), mtime: 10}
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "♦Neru Hair♦", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if tr, _ := f.take(); tr != "[1] -> /MioVRCA/share_1Share" || j.Saved != "/MioVRCA/share_1Share" {
		t.Errorf("saved: %q into %q", tr, j.Saved)
	}
	if j.Dir != filepath.Join(st.Settings.DownloadDir, "♦Neru Hair♦") {
		t.Errorf("on this disk: %s", j.Dir)
	}
	st.Mu.RLock()
	copyAt := st.User["pan:1Share"].PanCopy
	st.Mu.RUnlock()
	if copyAt != "/MioVRCA/share_1Share" {
		t.Errorf("recorded copy: %q", copyAt)
	}
	// refused even so: said plainly
	f.refuse = func(string) bool { return true }
	st.Mu.Lock()
	st.User["pan:1Share"].PanCopy = ""
	st.Mu.Unlock()
	j = &PanJob{ID: 2, Key: "pan:1Share", Title: "♦Neru Hair♦", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err == nil || err.Error() != "在网盘中创建文件夹失败：百度网盘不接受这个文件夹名（百度网盘错误 -7）" {
		t.Errorf("refused: %v", err)
	}
}

func TestBaiduName(t *testing.T) {
	for in, want := range map[string]string{
		"🌊【VRC Hair】 ♦Neru Hair♦ + Cloth💨🌊":          "【VRC Hair】 ♦Neru Hair♦ + Cloth",
		"♦️ Dress ❤️ v1.2":                           "♦ Dress ❤ v1.2",
		"a\u200db\ufeffc  d.":                        "abc d",
		"\U0001F469\u200d\U0001F469\u200d\U0001F467": "",
		"衣服 𠮷野家":                                     "衣服 野家",
	} {
		if got := baiduName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

type logSink struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The membership is asked for again when a download starts (the player may have changed it since logging in):
// kept with the login, and logged with the speed of the download.
func TestPanMembershipLogged(t *testing.T) {
	f, st := newFakeBaidu(t)
	sink := &logSink{}
	old := core.Logger
	core.Logger = log.New(sink, "", 0)
	t.Cleanup(func() { core.Logger = old })
	if s := LoadBaiduSession(); s == nil || s.VIP != 0 {
		t.Fatalf("session before: %+v", s)
	}
	f.share["/sh/Root"] = &bdNode{fsid: 1, dir: true, mtime: 10}
	f.share["/sh/Root/a.txt"] = &bdNode{fsid: 2, data: []byte("hair"), mtime: 10}
	if err := runPanJob(context.Background(), st, &PanJob{ID: 1, Key: "pan:1Share", Title: "Hair", Stage: "save"}); err != nil {
		t.Fatal(err)
	}
	if s := LoadBaiduSession(); s == nil || s.VIP != 2 || CurrentBaiduAccount().VIP != 2 {
		t.Errorf("session after: %+v", s)
	}
	if !strings.Contains(sink.String(), "网盘文件下载：4 B，") || !strings.Contains(sink.String(), "（百度网盘账号：超级会员）") {
		t.Errorf("log: %s", sink.String())
	}
}
