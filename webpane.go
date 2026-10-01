package main

// The web pane: Booth and 闲鱼 pages inside the program — logging in, buying, talking to sellers —
// on a profile of its own, so the logins are kept. In the app's own window it is a second WebView2
// laid over the page area (webpane_windows.go); otherwise a separate app-style Edge/Chrome window.
// Either way it is driven over the DevTools protocol on 127.0.0.1, like the old Booth sync window.

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// paneNativeAPI is the pane inside the app's own window; nativePane is nil when that window is not
// running (browser mode, other systems).
type paneNativeAPI interface {
	Ensure(proxy string) (int, error) // create it hidden (once) and return its DevTools port
	Place(x, y, w, h int, show bool)  // physical pixels in the window's client area
}

var nativePane paneNativeAPI

var errNoPane = errors.New("没有可用的内置浏览器（需要 WebView2、Edge 或 Chrome）")

// Links that would open a new window stay in the pane (popups with a size, such as payment windows,
// still open on their own).
const paneLinkScript = `(()=>{if(window.__mioPane)return;window.__mioPane=1;
document.addEventListener('click',e=>{const a=e.target&&e.target.closest&&e.target.closest('a[target]');
if(!a||!/^https?:/i.test(a.href||'')||e.ctrlKey||e.shiftKey||e.metaKey)return;
const t=(a.getAttribute('target')||'').toLowerCase();if(t&&t!=='_self'&&t!=='_top'&&t!=='_parent'){e.preventDefault();location.href=a.href}},true);
const o=window.open;window.open=function(u,n,f){try{if(u&&!f){const h=new URL(String(u),location.href).href;if(/^https?:/i.test(h)){location.href=h;return window}}}catch(e){}return o.apply(this,arguments)};
document.addEventListener('click',e=>{if(typeof window.mioDownload!=='function')return;
const el=e.target&&e.target.closest&&e.target.closest('[data-href*="/downloadables/"],a[href*="/downloadables/"]');if(!el)return;
const m=/\/downloadables\/(\d+)/.exec(el.getAttribute('data-href')||el.getAttribute('href')||'');if(!m)return;
e.preventDefault();e.stopPropagation();e.stopImmediatePropagation();
let box=el,item='',name='',file='';for(let i=0;i<10&&box;i++){box=box.parentElement;if(box&&box.querySelector('a[href*="/items/"]'))break}
if(box){const a=box.querySelector('a[href*="/items/"]'),im=/\/items\/(\d+)/.exec(a.href);if(im)item=im[1];const t=box.querySelector('[class*="font-bold"]');name=t?t.textContent.trim():''}
const row=el.parentElement,f=row&&row.querySelector('.break-all');file=f?f.textContent.trim():'';
window.mioDownload(JSON.stringify({id:m[1],item,name,file}));const sp=el.querySelector('span')||el;sp.textContent='已加入软件下载';el.style.opacity='.6'},true)})()`

type webPane struct {
	mu       sync.Mutex
	st       *Store
	mode     string // "native" | "window" once started
	port     int
	conn     *cdpConn
	target   string
	shown    bool   // on screen (native: placed and visible; window: a visible window)
	headless bool   // window mode: started without a window for a quiet login check
	kind     string // "booth" | "xianyu": which tab the page belongs to
	shownAt  time.Time
}

var pane = &webPane{}

func paneBrowser() string {
	if b, _ := syncBrowser(); isChromiumExe(b) {
		return b
	}
	if e := findEdge(); isChromiumExe(e) {
		return e
	}
	return ""
}

// paneMode: how pages can be shown: "native", "window", or "" (only the system browser).
func paneMode() string {
	if nativePane != nil {
		return "native"
	}
	if paneBrowser() != "" {
		return "window"
	}
	return ""
}

func (p *webPane) proxy() string {
	if p.st == nil {
		return ""
	}
	p.st.mu.RLock()
	px := strings.TrimSpace(p.st.Settings.Proxy)
	p.st.mu.RUnlock()
	if px == "" || strings.EqualFold(px, "direct") {
		return px
	}
	if !strings.Contains(px, "://") {
		px = "http://" + px
	}
	return px
}

func xianyuBase() string {
	if v := os.Getenv("VRCLIB_XY_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://www.goofish.com"
}

func paneProfileDir() string { return filepath.Join(dataDir, "web-login") }

func (c *cdpConn) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// ensure starts the pane when it is not running; a new window starts on startURL. Caller holds p.mu.
// fresh: the window was just started on startURL.
func (p *webPane) ensure(visible bool, startURL string) (fresh bool, err error) {
	switch paneMode() {
	case "native":
		if p.port == 0 {
			port, err := nativePane.Ensure(p.proxy())
			if err != nil {
				return false, err
			}
			p.port, p.mode = port, "native"
		}
		return false, nil
	case "window":
		if p.port > 0 {
			if _, err := cdpTargets(p.port); err == nil {
				if !(visible && p.headless) {
					return false, nil
				}
				closeDebugBrowser(p.port) // a hidden check is still running: reopen with a window
				time.Sleep(600 * time.Millisecond)
			}
			p.port, p.conn = 0, nil
		}
		if startURL == "" {
			startURL = "about:blank"
		}
		port, reused, err := launchPaneWindow(paneBrowser(), boothProfileDir(), startURL, !visible, p.proxy())
		if err != nil {
			return false, err
		}
		p.port, p.mode, p.headless, p.conn = port, "window", !visible && !reused, nil
		return !reused, nil
	}
	return false, errNoPane
}

// launchPaneWindow: an app-style window (no tabs, no address bar) on its own profile with a DevTools
// port on 127.0.0.1. The window profile is the one the Booth sync always used, so its login stays.
func launchPaneWindow(browser, profile, startURL string, headless bool, proxy string) (port int, reused bool, err error) {
	_ = os.MkdirAll(profile, 0755)
	if port, _ := readDevToolsPort(profile); port > 0 {
		if _, err := cdpTargets(port); err == nil {
			return port, true, nil // still open (an older sync window, say)
		}
	}
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	args := []string{"--user-data-dir=" + profile, "--remote-debugging-port=0", "--remote-debugging-address=127.0.0.1",
		"--no-first-run", "--no-default-browser-check", "--disable-features=Translate", "--lang=zh-CN", "--window-size=1280,900"}
	switch {
	case strings.EqualFold(proxy, "direct"):
		args = append(args, "--no-proxy-server")
	case proxy != "":
		args = append(args, "--proxy-server="+proxy)
	}
	if headless || os.Getenv("VRCLIB_HEADLESS") == "1" { // (the variable: tests only)
		args = append(args, "--headless=new")
	}
	if os.Getenv("VRCLIB_HEADLESS") == "1" {
		args = append(args, "--no-sandbox")
	}
	args = append(args, "--app="+startURL)
	cmd := browserCmd(browser, args)
	if err := cmd.Start(); err != nil {
		return 0, false, err
	}
	go func(c *exec.Cmd) { _ = c.Wait() }(cmd)
	for i := 0; i < 150; i++ {
		time.Sleep(150 * time.Millisecond)
		if port, _ := readDevToolsPort(profile); port > 0 {
			if _, err := cdpTargets(port); err == nil {
				return port, false, nil
			}
		}
	}
	return 0, false, errors.New("浏览器没有打开调试端口")
}

// page: the DevTools connection to the pane's page. Caller holds p.mu.
func (p *webPane) page() (*cdpConn, error) {
	if p.conn != nil && p.conn.alive() {
		return p.conn, nil
	}
	p.conn = nil
	if p.port == 0 {
		return nil, errCDPClosed
	}
	var pick *cdpTarget
	for i := 0; i < 20 && pick == nil; i++ {
		ts, err := cdpTargets(p.port)
		if err != nil {
			p.port = 0 // the window was closed
			return nil, errCDPClosed
		}
		for j := range ts {
			t := &ts[j]
			if (t.Type != "page" && t.Type != "webview") || t.WSURL == "" || strings.HasPrefix(t.URL, "devtools:") {
				continue
			}
			if pick == nil || t.ID == p.target {
				pick = t
			}
		}
		if pick == nil && len(ts) > 0 {
			logf("内置浏览器的页面：%+v", ts) // an unexpected kind of target
		}
		if pick == nil {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if pick == nil {
		return nil, errors.New("页面还没准备好")
	}
	c, err := cdpDial(pick.WSURL, 6*time.Second)
	if err != nil {
		return nil, err
	}
	p.conn, p.target = c, pick.ID
	c.onEvent = p.event
	_, _ = c.call("Page.enable", nil, 5*time.Second)
	_, _ = c.call("Runtime.enable", nil, 5*time.Second)
	_, _ = c.call("Runtime.addBinding", map[string]any{"name": "mioDownload"}, 5*time.Second)
	_, _ = c.call("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": paneLinkScript}, 5*time.Second)
	_, _ = c.eval(paneLinkScript, false, 3*time.Second)
	return c, nil
}

func (p *webPane) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	p.mu.Lock()
	c, err := p.page()
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return c.call(method, params, timeout)
}

// Open shows u in the pane (show=false: load it without putting it on screen).
func (p *webPane) Open(u, kind string, show bool) error {
	p.mu.Lock()
	fresh, err := p.ensure(show, u)
	if err != nil {
		p.mu.Unlock()
		return err
	}
	c, err := p.page()
	if err != nil {
		p.mu.Unlock()
		return err
	}
	if kind != "" {
		p.kind = kind
	}
	if show && p.mode == "window" {
		p.shown, p.shownAt = true, time.Now()
	}
	p.mu.Unlock()
	if !fresh {
		if _, err := c.call("Page.navigate", map[string]any{"url": u}, 15*time.Second); err != nil {
			return err
		}
	}
	if show && p.mode == "window" {
		_, _ = c.call("Page.bringToFront", nil, 3*time.Second)
	}
	return nil
}

// Place puts the native pane on screen (or hides it). The UI reports where its page area is.
func (p *webPane) Place(x, y, w, h float64, dpr float64, show bool) {
	if nativePane == nil {
		return
	}
	if dpr <= 0 {
		dpr = 1
	}
	p.mu.Lock()
	if show && p.port == 0 {
		p.mu.Unlock()
		return // nothing opened yet
	}
	if show && !p.shown {
		p.shownAt = time.Now()
	}
	p.shown = show
	p.mu.Unlock()
	r := func(v float64) int { return int(v*dpr + 0.5) }
	nativePane.Place(r(x), r(y), r(x+w)-r(x), r(y+h)-r(y), show)
}

// Shown: the player can see the pane (and so may be using it).
func (p *webPane) Shown() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode == "window" {
		if p.port == 0 || p.headless {
			return false
		}
		if _, err := cdpTargets(p.port); err != nil {
			p.port, p.conn, p.shown = 0, nil, false
			return false
		}
		return true
	}
	return p.shown
}

type PaneState struct {
	Mode    string `json:"mode"`
	Open    bool   `json:"open"`
	Shown   bool   `json:"shown"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Back    bool   `json:"back"`
	Fwd     bool   `json:"fwd"`
	Loading bool   `json:"loading"`
	Kind    string `json:"kind"`
	DL      int    `json:"dl"` // downloads waiting or running (a click on the page may have added one)
}

type navHistory struct {
	CurrentIndex int `json:"currentIndex"`
	Entries      []struct {
		ID    int    `json:"id"`
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"entries"`
}

func (p *webPane) history() (*navHistory, error) {
	raw, err := p.call("Page.getNavigationHistory", nil, 3*time.Second)
	if err != nil {
		return nil, err
	}
	var h navHistory
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func (p *webPane) State() PaneState {
	s := PaneState{Mode: paneMode(), DL: dlLeft()}
	p.mu.Lock()
	running := p.port > 0 && !p.headless
	s.Kind = p.kind
	p.mu.Unlock()
	if !running {
		return s
	}
	s.Shown = p.Shown()
	h, err := p.history()
	if err != nil {
		return s
	}
	s.Open = true
	if h.CurrentIndex >= 0 && h.CurrentIndex < len(h.Entries) {
		e := h.Entries[h.CurrentIndex]
		s.URL, s.Title = e.URL, e.Title
		s.Back = h.CurrentIndex > 0
		s.Fwd = h.CurrentIndex < len(h.Entries)-1
	}
	if raw, err := p.call("Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true}, 2*time.Second); err == nil {
		s.Loading = !strings.Contains(string(raw), `"complete"`)
	}
	if s.URL == "about:blank" {
		s.URL = ""
	}
	return s
}

// Act: the toolbar buttons. Returns text for "selection".
func (p *webPane) Act(act string) (string, error) {
	switch act {
	case "back", "forward":
		h, err := p.history()
		if err != nil {
			return "", err
		}
		i := h.CurrentIndex - 1
		if act == "forward" {
			i = h.CurrentIndex + 1
		}
		if i < 0 || i >= len(h.Entries) {
			return "", nil
		}
		_, err = p.call("Page.navigateToHistoryEntry", map[string]any{"entryId": h.Entries[i].ID}, 5*time.Second)
		return "", err
	case "reload":
		_, err := p.call("Page.reload", nil, 5*time.Second)
		return "", err
	case "stop":
		_, err := p.call("Page.stopLoading", nil, 5*time.Second)
		return "", err
	case "front":
		_, err := p.call("Page.bringToFront", nil, 3*time.Second)
		return "", err
	case "url":
		h, err := p.history()
		if err != nil || h.CurrentIndex < 0 || h.CurrentIndex >= len(h.Entries) {
			return "", err
		}
		return h.Entries[h.CurrentIndex].URL, nil
	case "selection":
		p.mu.Lock()
		c, err := p.page()
		p.mu.Unlock()
		if err != nil {
			return "", err
		}
		return c.evalString("String(window.getSelection ? getSelection() : '')", 3*time.Second)
	case "close":
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.mode == "window" && p.port > 0 && !purchaseBusy.Load() {
			if p.conn != nil {
				p.conn.Close()
			}
			closeDebugBrowser(p.port)
			p.port, p.conn = 0, nil
		}
		p.shown = false
		return "", nil
	}
	return "", errors.New("未知操作")
}

// event: DevTools events of the pane's page. A download button clicked on a Booth page goes into the
// program's own downloads (unpacked into the library) instead of the browser's.
func (p *webPane) event(method string, params json.RawMessage) {
	if method != "Runtime.bindingCalled" || p.st == nil {
		return
	}
	var b struct{ Name, Payload string }
	if json.Unmarshal(params, &b) != nil || b.Name != "mioDownload" {
		return
	}
	var d struct{ ID, Item, Name, File string }
	if json.Unmarshal([]byte(b.Payload), &d) != nil || d.ID == "" {
		return
	}
	if cs, err := p.Cookies(boothCookieURLs()); err == nil && hasBoothSession(cs) {
		_ = saveBoothSession(cs) // the page is logged in: so are the downloads
	}
	n := QueueDownloadFromPage(p.st, d.ID, d.Item, d.Name, d.File)
	logf("Booth 页面里点了下载 %s（%s）：加入 %d 个", d.ID, d.File, n)
}

// Cookies of the pane's profile for these sites.
func (p *webPane) Cookies(urls []string) ([]savedCookie, error) {
	d := &cdpDriver{}
	p.mu.Lock()
	c, err := p.page()
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	d.c = c
	return d.Cookies(urls)
}

// forgetSites removes the cookies of these sites (by domain suffix) from the pane's profile.
func (p *webPane) forgetSites(suffixes []string) error {
	raw, err := p.call("Network.getAllCookies", nil, 6*time.Second)
	if err != nil {
		return err
	}
	var r struct {
		Cookies []struct {
			Name   string `json:"name"`
			Domain string `json:"domain"`
			Path   string `json:"path"`
		} `json:"cookies"`
	}
	_ = json.Unmarshal(raw, &r)
	for _, c := range r.Cookies {
		d := strings.TrimPrefix(c.Domain, ".")
		for _, s := range suffixes {
			if d == s || strings.HasSuffix(d, "."+s) {
				_, _ = p.call("Network.deleteCookies", map[string]any{"name": c.Name, "domain": c.Domain, "path": c.Path}, 4*time.Second)
				break
			}
		}
	}
	return nil
}

// closeHidden ends a window-mode pane that was only started for a quiet check.
func (p *webPane) closeHidden() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode == "window" && p.headless && p.port > 0 {
		if p.conn != nil {
			p.conn.Close()
		}
		closeDebugBrowser(p.port)
		p.port, p.conn, p.headless = 0, nil, false
	}
}

// ---------- the pane as the Booth sync window ----------

type paneDriver struct {
	start time.Time
	seen  bool // the player had the pane on screen during this sync
}

func (d *paneDriver) conn() (*cdpConn, error) {
	pane.mu.Lock()
	defer pane.mu.Unlock()
	return pane.page()
}

func (d *paneDriver) Eval(expr string, timeout time.Duration) (json.RawMessage, error) {
	c, err := d.conn()
	if err != nil {
		return nil, err
	}
	return c.eval(expr, false, timeout)
}

func (d *paneDriver) Navigate(u string) error {
	_, err := pane.call("Page.navigate", map[string]any{"url": u}, 15*time.Second)
	return err
}

func (d *paneDriver) Reconnect() error {
	pane.mu.Lock()
	if pane.conn != nil {
		pane.conn.Close()
		pane.conn = nil
	}
	_, err := pane.page()
	pane.mu.Unlock()
	return err
}

func (d *paneDriver) Dead() bool {
	pane.mu.Lock()
	defer pane.mu.Unlock()
	return pane.port == 0
}

func (d *paneDriver) Close()        {}
func (d *paneDriver) CloseBrowser() {}

func (d *paneDriver) Cookies(urls []string) ([]savedCookie, error) { return pane.Cookies(urls) }

// userLeft: the player closed the pane while the sync was still waiting for the login.
func (d *paneDriver) userLeft() bool {
	pane.mu.Lock()
	other := pane.kind == "xianyu" // the page area went over to 闲鱼
	pane.mu.Unlock()
	if other {
		return true
	}
	if pane.Shown() {
		d.seen = true
		return false
	}
	return d.seen || time.Since(d.start) > 15*time.Second
}

func hasBoothSession(cs []savedCookie) bool {
	for _, c := range cs {
		if c.Name == boothSessionCookie && c.Value != "" {
			return true
		}
	}
	return false
}

// runBoothPane: the Booth sync (or, quiet, only a login check) inside the pane.
func runBoothPane(st *Store, prog *Task, quiet bool) bool {
	start := boothAccountsBase() + "/library"
	if quiet {
		inUse := pane.Shown()
		pane.mu.Lock()
		running := pane.port > 0
		pane.mu.Unlock()
		if running {
			if cs, err := pane.Cookies(boothCookieURLs()); err == nil && hasBoothSession(cs) {
				_ = saveBoothSession(cs)
				if inUse {
					return true
				}
			}
		}
		if inUse {
			return false // the player is using the page: do not take it away
		}
		if err := pane.Open(start, "booth", false); err != nil {
			logf("Booth 登录检查：%v", err)
			return false
		}
		defer pane.closeHidden()
	} else {
		prog.Set(0, 0, "正在打开 Booth 页面…")
		if err := pane.Open(start, "booth", true); err != nil {
			logf("Booth 页面打不开: %v", err)
			prog.Set(0, 0, "Booth 页面打不开："+err.Error())
			return false
		}
	}
	closeWin := false
	msg := "请在软件里的 Booth 页面登录，登录后会自动开始读取"
	if paneMode() == "window" {
		msg = "请在打开的 Booth 窗口里登录，登录后会自动开始读取"
	}
	return boothLoop(st, prog, &paneDriver{start: time.Now()}, quiet, msg, &closeWin)
}
