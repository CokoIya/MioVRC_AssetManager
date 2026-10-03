package webpane

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"vrclib/internal/core"
)

type PageDriver interface {
	Eval(expr string, timeout time.Duration) (json.RawMessage, error)
	Navigate(u string) error
	Reconnect() error // pick the Booth tab again (after redirects / new tabs)
	Dead() bool
	Close()
	CloseBrowser()
	Cookies(urls []string) ([]core.SavedCookie, error) // the browser's cookies for these sites
}

// BoothCookieURLs: the Booth sites whose cookies make up the login.
func BoothCookieURLs() []string {
	var out []string
	for _, u := range []string{core.BoothAccountsBase(), core.BoothDLBase(), core.BoothWebBase()} {
		if u = u + "/"; !core.ContainsStr(out, u) {
			out = append(out, u)
		}
	}
	return out
}

func EvalString(d PageDriver, expr string, timeout time.Duration) (string, error) {
	v, err := d.Eval(expr, timeout)
	if err != nil {
		return "", err
	}
	var s string
	_ = json.Unmarshal(v, &s)
	return s, nil
}

func IsFirefoxExe(exe string) bool {
	b := strings.ToLower(filepath.Base(exe))
	return exe != "" && (b == "firefox.exe" || b == "firefox")
}

// ---------- Chromium (CDP) ----------

type cdpDriver struct {
	port int
	host string
	c    *cdpConn
}

func StartChromiumDriver(browser, profile, startURL, host string, extra []string) (*cdpDriver, error) {
	port, err := launchDebugBrowser(browser, profile, startURL, extra)
	if err != nil {
		return nil, err
	}
	d := &cdpDriver{port: port, host: host}
	if err := d.Reconnect(); err != nil {
		CloseDebugBrowser(port)
		return nil, err
	}
	return d, nil
}

func (d *cdpDriver) Reconnect() error {
	if d.c != nil {
		d.c.Close()
		d.c = nil
	}
	for i := 0; i < 40; i++ {
		ts, err := cdpTargets(d.port)
		if err != nil {
			return ErrCDPClosed
		}
		var pick *cdpTarget
		for j := range ts {
			t := &ts[j]
			if t.Type != "page" || t.WSURL == "" {
				continue
			}
			if strings.Contains(t.URL, d.host) {
				pick = t
				break
			}
			if pick == nil && (strings.Contains(t.URL, "booth") || strings.Contains(t.URL, "pixiv")) {
				pick = t
			}
		}
		if pick != nil {
			if c, err := cdpDial(pick.WSURL, 6*time.Second); err == nil {
				d.c = c
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("找不到 Booth 页面")
}

func (d *cdpDriver) Eval(expr string, timeout time.Duration) (json.RawMessage, error) {
	if d.c == nil {
		return nil, ErrCDPClosed
	}
	return d.c.eval(expr, false, timeout)
}

func (d *cdpDriver) Navigate(u string) error {
	if d.c == nil {
		return ErrCDPClosed
	}
	_, err := d.c.call("Page.navigate", map[string]any{"url": u}, 10*time.Second)
	return err
}

func (d *cdpDriver) Dead() bool { return d.c == nil || d.c.err != nil }

func (d *cdpDriver) Cookies(urls []string) ([]core.SavedCookie, error) {
	if d.c == nil {
		return nil, ErrCDPClosed
	}
	res, err := d.c.call("Network.getCookies", map[string]any{"urls": urls}, 8*time.Second)
	if err != nil {
		return nil, err
	}
	var r struct {
		Cookies []struct {
			Name    string  `json:"name"`
			Value   string  `json:"value"`
			Domain  string  `json:"domain"`
			Expires float64 `json:"expires"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	var out []core.SavedCookie
	for _, c := range r.Cookies {
		exp := int64(c.Expires)
		if exp < 0 {
			exp = 0
		}
		out = append(out, core.SavedCookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Expires: exp})
	}
	return out, nil
}
func (d *cdpDriver) Close() {
	if d.c != nil {
		d.c.Close()
	}
}
func (d *cdpDriver) CloseBrowser() { CloseDebugBrowser(d.port) }

// ---------- Firefox (WebDriver BiDi) ----------

type bidiDriver struct {
	c   *cdpConn
	ctx string
	cmd *exec.Cmd
}

var reBidiListen = regexp.MustCompile(`WebDriver BiDi listening on (ws://[^\s]+)`)

func StartFirefoxDriver(exe, profile, startURL string, extra []string) (*bidiDriver, error) {
	_ = os.MkdirAll(profile, 0755)
	userJS := filepath.Join(profile, "user.js")
	if !core.FileExists(userJS) {
		prefs := []string{
			`user_pref("browser.shell.checkDefaultBrowser", false);`,
			`user_pref("browser.aboutwelcome.enabled", false);`,
			`user_pref("browser.startup.homepage_override.mstone", "ignore");`,
			`user_pref("datareporting.policy.dataSubmissionPolicyBypassNotification", true);`,
			`user_pref("toolkit.telemetry.reportingpolicy.firstRun", false);`,
			`user_pref("browser.tabs.warnOnClose", false);`,
			`user_pref("intl.locale.requested", "zh-CN");`,
		}
		_ = os.WriteFile(userJS, []byte(strings.Join(prefs, "\n")+"\n"), 0644)
	}
	_ = os.Remove(filepath.Join(profile, "WebDriverBiDiServer.json"))
	args := append([]string{"--no-remote", "--profile", profile, "--remote-debugging-port=0", "--wait-for-browser"}, extra...)
	cmd := exec.Command(exe, append(args, "--new-window", startURL)...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := reBidiListen.FindStringSubmatch(sc.Text()); m != nil {
				select {
				case found <- m[1]:
				default:
				}
			}
		}
	}()
	go func() { _ = cmd.Wait() }()
	var ws string
	deadline := time.Now().Add(40 * time.Second)
	for ws == "" && time.Now().Before(deadline) {
		select {
		case ws = <-found:
		case <-time.After(300 * time.Millisecond):
			// newer Firefox also writes the address into the profile
			if b, err := os.ReadFile(filepath.Join(profile, "WebDriverBiDiServer.json")); err == nil {
				var v struct {
					Host string `json:"ws_host"`
					Port int    `json:"ws_port"`
				}
				if json.Unmarshal(b, &v) == nil && v.Port > 0 {
					if v.Host == "" {
						v.Host = "127.0.0.1"
					}
					ws = fmt.Sprintf("ws://%s:%d", v.Host, v.Port)
				}
			}
		}
	}
	if ws == "" {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, errors.New("Firefox 没有打开远程接口（可能已经有一个同样配置的 Firefox 窗口开着）")
	}
	c, err := cdpDial(strings.TrimRight(ws, "/")+"/session", 10*time.Second)
	if err != nil {
		return nil, err
	}
	if _, err := c.call("session.new", map[string]any{"capabilities": map[string]any{}}, 15*time.Second); err != nil {
		c.Close()
		return nil, fmt.Errorf("Firefox 会话没建立：%v", err)
	}
	d := &bidiDriver{c: c, cmd: cmd}
	if err := d.Reconnect(); err != nil {
		d.CloseBrowser()
		return nil, err
	}
	return d, nil
}

func (d *bidiDriver) Reconnect() error {
	for i := 0; i < 30; i++ {
		res, err := d.c.call("browsingContext.getTree", map[string]any{}, 8*time.Second)
		if err != nil {
			return err
		}
		var tree struct {
			Contexts []struct {
				Context string `json:"context"`
				URL     string `json:"url"`
			} `json:"contexts"`
		}
		_ = json.Unmarshal(res, &tree)
		pick := ""
		for _, c := range tree.Contexts {
			if strings.Contains(c.URL, "booth") || strings.Contains(c.URL, "pixiv") {
				pick = c.Context
				break
			}
		}
		if pick == "" && len(tree.Contexts) > 0 {
			pick = tree.Contexts[0].Context
		}
		if pick != "" {
			d.ctx = pick
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("找不到 Booth 页面")
}

func (d *bidiDriver) Eval(expr string, timeout time.Duration) (json.RawMessage, error) {
	res, err := d.c.call("script.evaluate", map[string]any{
		"expression": expr, "target": map[string]any{"context": d.ctx}, "awaitPromise": false, "resultOwnership": "none",
	}, timeout)
	if err != nil {
		return nil, err
	}
	var r struct {
		Type   string `json:"type"`
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if r.Type == "exception" {
		return nil, errors.New("页面脚本出错：" + firstLine(r.ExceptionDetails.Text))
	}
	switch r.Result.Type {
	case "string", "number", "boolean":
		return r.Result.Value, nil
	}
	return json.RawMessage("null"), nil
}

func (d *bidiDriver) Navigate(u string) error {
	_, err := d.c.call("browsingContext.navigate", map[string]any{"context": d.ctx, "url": u, "wait": "none"}, 10*time.Second)
	return err
}

func (d *bidiDriver) Dead() bool { return d.c == nil || d.c.err != nil }

func (d *bidiDriver) Cookies(urls []string) ([]core.SavedCookie, error) {
	res, err := d.c.call("storage.getCookies", map[string]any{}, 8*time.Second)
	if err != nil {
		return nil, err
	}
	var r struct {
		Cookies []struct {
			Name  string `json:"name"`
			Value struct {
				Value string `json:"value"`
			} `json:"value"`
			Domain string `json:"domain"`
			Expiry int64  `json:"expiry"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	var out []core.SavedCookie
	for _, c := range r.Cookies {
		dom := strings.TrimPrefix(c.Domain, ".")
		keep := false
		for _, u := range urls {
			if pu, err := url.Parse(u); err == nil && (pu.Hostname() == dom || strings.HasSuffix(pu.Hostname(), "."+dom)) {
				keep = true
			}
		}
		if keep {
			out = append(out, core.SavedCookie{Name: c.Name, Value: c.Value.Value, Domain: c.Domain, Expires: c.Expiry})
		}
	}
	return out, nil
}
func (d *bidiDriver) Close() {}
func (d *bidiDriver) CloseBrowser() {
	if d.c != nil && d.c.err == nil {
		_, _ = d.c.call("browser.close", map[string]any{}, 5*time.Second)
		d.c.Close()
	}
	if d.cmd != nil && d.cmd.Process != nil {
		time.Sleep(500 * time.Millisecond)
		_ = d.cmd.Process.Kill()
	}
}
