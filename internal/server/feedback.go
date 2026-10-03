package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/update"
	"vrclib/internal/webpane"
)

const feedbackMail = "Coko_Iya@163.com"

func feedbackEndpoint() string {
	if v := os.Getenv("VRCLIB_FEEDBACK_URL"); v != "" { // tests only
		return v
	}
	return "https://formsubmit.co/ajax/" + feedbackMail
}

var (
	feedbackMu   sync.Mutex
	lastFeedback time.Time
	reEmail      = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// feedbackInfo: what helps to understand a problem, without the library's contents.
func feedbackInfo(st *core.Store) string {
	st.Mu.RLock()
	nAssets, nShares, nPurch, nRoots, nProj := len(st.Assets), 0, len(st.Purchases), len(st.Settings.Roots), len(st.Settings.ProjectRoots)
	for k := range st.User {
		if core.IsPanShareKey(k) {
			nShares++
		}
	}
	st.Mu.RUnlock()
	var b strings.Builder
	fmt.Fprintf(&b, "版本：%s（%s/%s）\n", core.AppVersion, runtime.GOOS, runtime.GOARCH)
	if v := core.WebView2Version(); v != "" {
		fmt.Fprintf(&b, "WebView2：%s\n", v)
	}
	if br := webpane.BrowserLabel(core.DefaultBrowserExe()); br != "" {
		fmt.Fprintf(&b, "默认浏览器：%s\n", br)
	}
	fmt.Fprintf(&b, "素材 %d，网盘分享 %d，Booth 已购 %d，素材文件夹 %d，Unity 工程文件夹 %d\n", nAssets, nShares, nPurch, nRoots, nProj)
	// the last lines of the log (paths in it are local folder names)
	if f, err := os.Open(filepath.Join(core.DataDir, "library.log")); err == nil {
		var lines []string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
			if len(lines) > 60 {
				lines = lines[1:]
			}
		}
		f.Close()
		if len(lines) > 0 {
			b.WriteString("\n最近的日志：\n" + strings.Join(lines, "\n") + "\n")
		}
	}
	return b.String()
}

// fbInputError: a message the window can fix by itself (too short, sent too soon), as opposed
// to one that could not be delivered.
type fbInputError string

func (e fbInputError) Error() string { return string(e) }

// SendFeedback posts one message. It returns a note for the window ("" when all went well).
func SendFeedback(st *core.Store, kind, text, contact string, withInfo bool) (string, error) {
	text, contact = strings.TrimSpace(text), strings.TrimSpace(contact)
	if len([]rune(text)) < 4 {
		return "", fbInputError("内容太短")
	}
	if len([]rune(text)) > 5000 {
		return "", fbInputError("内容太长了（最多 5000 字）")
	}
	feedbackMu.Lock()
	if time.Since(lastFeedback) < 30*time.Second {
		feedbackMu.Unlock()
		return "", fbInputError("发送太频繁，半分钟后再试")
	}
	feedbackMu.Unlock()
	if kind == "" {
		kind = "反馈"
	}
	msg := text
	if contact != "" {
		msg += "\n\n联系方式：" + contact
	}
	if withInfo {
		msg += "\n\n---\n" + feedbackInfo(st)
	}
	form := map[string]string{
		"_subject":  fmt.Sprintf("[%s %s] %s", core.AppName, core.AppVersion, kind),
		"_template": "box",
		"_captcha":  "false",
		"类型":        kind,
		"name":      contact,
		"message":   msg,
	}
	if form["name"] == "" {
		form["name"] = "匿名玩家"
	}
	if reEmail.MatchString(contact) {
		form["_replyto"] = contact
	}
	body, _ := json.Marshal(form)
	req, _ := http.NewRequest("POST", feedbackEndpoint(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", core.AppID+"/"+core.AppVersion)
	// FormSubmit wants to know which page the form is on
	req.Header.Set("Origin", "https://github.com")
	req.Header.Set("Referer", "https://github.com/"+update.UpdateRepo)
	c := core.HTTPClient(st)
	c.Timeout = 30 * time.Second
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送失败（%s）", core.FriendlyNetErr(err))
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var r struct {
		Success any    `json:"success"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(rb, &r)
	ok := fmt.Sprint(r.Success) == "true"
	if resp.StatusCode != 200 || !ok {
		if strings.Contains(strings.ToLower(r.Message), "activat") {
			// the address has not confirmed FormSubmit yet; this message may not be passed on
			markFeedbackSent()
			core.Logf("反馈已提交，等待邮箱激活：%s", r.Message)
			return "已提交，但作者邮箱还没开通接收，可能收不到。", nil
		}
		core.Logf("反馈发送失败：%d %s", resp.StatusCode, core.Truncate(string(rb), 200))
		if r.Message != "" {
			return "", fmt.Errorf("发送失败（%s）", r.Message)
		}
		return "", fmt.Errorf("发送失败（%d）", resp.StatusCode)
	}
	markFeedbackSent()
	core.Logf("反馈已发送")
	return "", nil
}

func markFeedbackSent() {
	feedbackMu.Lock()
	lastFeedback = time.Now()
	feedbackMu.Unlock()
}
