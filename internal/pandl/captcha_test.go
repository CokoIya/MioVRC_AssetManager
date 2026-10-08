package pandl

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// tinyPNG: a picture, as Baidu's captcha comes.
func tinyPNG() []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 2)))
	return b.Bytes()
}

// queued: the job is in the queue's list, as the window sees it (an answer to a captcha finds it there).
func queued(t *testing.T, j *PanJob) {
	t.Helper()
	panDLMu.Lock()
	panJobs = append(panJobs, j)
	panDLMu.Unlock()
	t.Cleanup(func() {
		panDLMu.Lock()
		keepPanLocked(func(o *PanJob) bool { return o == j })
		panDLMu.Unlock()
	})
}

// waitPan: the job as the window sees it, once ok says so.
func waitPan(t *testing.T, key, what string, ok func(j PanJob) bool) PanJob {
	t.Helper()
	for i := 0; i < 500; i++ {
		for _, j := range PanJobsSnapshot() {
			if j.Key == key && ok(j) {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s: never seen; jobs %+v", what, PanJobsSnapshot())
	return PanJob{}
}

func ended(t *testing.T, done chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the job did not end")
		return nil
	}
}

func shareWithCode(t *testing.T) (*fakeBaidu, *core.Store) {
	t.Helper()
	f, st := newFakeBaidu(t)
	f.pwd, f.captcha = "abcd", true
	st.User["pan:1Share"].SharePwd = "abcd"
	f.share["/sh/a.zip"] = &bdNode{fsid: 2, data: []byte("zip"), mtime: 10}
	return f, st
}

// A share whose code Baidu checks only after a captcha is read: the job shows the picture in the window and
// waits; a new picture comes on request, a misread one is followed by a new one marked as not taken, and the
// right reading lets the download go on. Downloading the same share again in this run checks no code.
func TestPanCaptcha(t *testing.T) {
	f, st := shareWithCode(t)
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save"}
	queued(t, j)
	done := make(chan error, 1)
	go func() { done <- runPanJob(context.Background(), st, j) }()

	w := waitPan(t, j.Key, "the first picture", func(j PanJob) bool { return j.Stage == "captcha" && j.CaptchaN == 1 })
	if !strings.HasPrefix(w.Captcha, "data:image/png;base64,") || w.CaptchaBad || w.Msg != "请输入百度网盘验证码" {
		t.Fatalf("the first picture: %.40q, bad %v, %q", w.Captcha, w.CaptchaBad, w.Msg)
	}
	if err := AnswerCaptcha(j.Key, "  ", false); err == nil || err.Error() != "请输入验证码" {
		t.Errorf("an empty answer: %v", err)
	}
	if err := AnswerCaptcha(j.Key, "", true); err != nil { // another picture
		t.Fatal(err)
	}
	waitPan(t, j.Key, "the second picture", func(j PanJob) bool { return j.Stage == "captcha" && j.CaptchaN == 2 && !j.CaptchaBad })
	if err := AnswerCaptcha(j.Key, "nope", false); err != nil {
		t.Fatal(err)
	}
	waitPan(t, j.Key, "a picture after a misread one", func(j PanJob) bool { return j.Stage == "captcha" && j.CaptchaN == 3 && j.CaptchaBad })
	f.mu.Lock()
	answer := f.answer
	f.mu.Unlock()
	if err := AnswerCaptcha(j.Key, " "+answer+" ", false); err != nil {
		t.Fatal(err)
	}
	if err := ended(t, done); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "a.zip" || j.Captcha != "" {
		t.Errorf("downloaded: %q; picture left: %v", got, j.Captcha != "")
	}
	f.mu.Lock()
	verifies, pictures := f.verifies, f.pictures
	f.mu.Unlock()
	if verifies != 3 || pictures != 3 { // asked, misread, read
		t.Errorf("%d checks of the code, %d pictures", verifies, pictures)
	}
	if err := AnswerCaptcha(j.Key, "c001", false); err == nil {
		t.Error("an answer was taken with no captcha shown")
	}

	j2 := &PanJob{ID: 2, Key: "pan:1Share", Title: "a", Stage: "save"}
	if err := runPanJob(context.Background(), st, j2); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	verifies = f.verifies
	f.mu.Unlock()
	if verifies != 3 {
		t.Errorf("the code was checked again: %d checks", verifies)
	}
}

// Cancel while the captcha waits: the job ends at once. Nobody answering: it ends after a while, with a word
// on what to do.
func TestPanCaptchaWaitEnds(t *testing.T) {
	_, st := shareWithCode(t)
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "a", Stage: "save"}
	queued(t, j)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runPanJob(ctx, st, j) }()
	waitPan(t, j.Key, "the picture", func(j PanJob) bool { return j.Stage == "captcha" })
	cancel()
	if err := ended(t, done); !errors.Is(err, errPanCancelled) {
		t.Errorf("cancelled: %v", err)
	}

	wait := panCaptchaWait
	panCaptchaWait = 50 * time.Millisecond
	t.Cleanup(func() { panCaptchaWait = wait })
	j2 := &PanJob{ID: 2, Key: "pan:1Share", Title: "a", Stage: "save"}
	if err := runPanJob(context.Background(), st, j2); err == nil || err.Error() != "等待输入验证码超时，请点击「重试」" {
		t.Errorf("no answer: %v", err)
	}
	if j2.Stage != "save" || j2.Captcha != "" {
		t.Errorf("after the wait: %s, picture left %v", j2.Stage, j2.Captcha != "")
	}
}

// A wrong code with no captcha asked for says so, as before.
func TestPanWrongCode(t *testing.T) {
	f, st := shareWithCode(t)
	f.captcha = false
	st.User["pan:1Share"].SharePwd = "zzzz"
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "a", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err == nil || err.Error() != "提取码错误" {
		t.Errorf("wrong code: %v", err)
	}
}

// Baidu makes the card's folder under another name than the one asked for: the files are saved where Baidu
// made it, and the card keeps that place for the next download.
func TestPanSaveFolderRenamed(t *testing.T) {
	f, st := newFakeBaidu(t)
	f.share["/sh/a.zip"] = &bdNode{fsid: 2, data: []byte("zip"), mtime: 10}
	f.renamed = map[string]string{"/MioVRCA/a": "/MioVRCA/a_1"}
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	st.Mu.RLock()
	kept := st.User["pan:1Share"].PanCopy
	st.Mu.RUnlock()
	if tr, _ := f.take(); tr != "[2] -> /MioVRCA/a_1" || j.Saved != "/MioVRCA/a_1" || kept != "/MioVRCA/a_1" {
		t.Errorf("saved: %q; the job says %q, the card %q", tr, j.Saved, kept)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "a.zip" {
		t.Errorf("downloaded: %q", got)
	}
}

// A folder that is not there after Baidu said it was made: the save fails with Baidu's 2 explained, and
// pan-debug.txt keeps what Baidu answered.
func TestPanSaveFolderMissing(t *testing.T) {
	f, st := newFakeBaidu(t)
	f.share["/sh/a.zip"] = &bdNode{fsid: 2, data: []byte("zip"), mtime: 10}
	f.lost = map[string]bool{"/MioVRCA/a": true}
	j := &PanJob{ID: 1, Key: "pan:1Share", Title: "网盘分享 1Share", Stage: "save"}
	err := runPanJob(context.Background(), st, j)
	if err == nil || err.Error() != "转存失败：网盘中没有目标文件夹「a」（百度网盘错误 2），请重试" {
		t.Fatalf("missing folder: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(core.DataDir, "pan-debug.txt"))
	if s := string(b); !strings.Contains(s, "dest: /MioVRCA/a\ndest there: false") || !strings.Contains(s, `"errno":2`) {
		t.Errorf("pan-debug.txt: %s", s)
	}
}
