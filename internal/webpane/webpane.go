package webpane

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
)

// paneNativeAPI is the pane inside the app's own window; nativePane is nil when that window is not
// running (browser mode, other systems).
type paneNativeAPI interface {
	Ensure(proxy string) (int, error) // create it hidden (once) and return its DevTools port
	Place(x, y, w, h int, show bool)  // physical pixels in the window's client area
}

var NativePane paneNativeAPI

var errNoPane = errors.New("本机缺少内置浏览器组件（需要 WebView2、Edge 或 Chrome）")

// Links that would open a new window stay in the pane (popups with a size, such as payment windows,
// still open on their own). On a Jinxxy page, a link to another site leaves for the default browser (see
// storepane.go; "__JX__" is replaced by the test for such a page) — but not one to a login or a payment page,
// which has to stay with the page it belongs to.
const paneLinkSource = `(()=>{if(window.__mioPane)return;window.__mioPane=1;
document.addEventListener('click',e=>{if(!(__JX__)||typeof window.mioExternal!=='function')return;
const a=e.target&&e.target.closest&&e.target.closest('a[href]');if(!a||!/^https?:/i.test(a.href||''))return;
let u;try{u=new URL(a.href)}catch(x){return}
if(u.origin===location.origin||/(^|\.)(jinxxy|jinxxy-cdn|paypal|stripe)\.com$/i.test(u.hostname)||(/(^|\.)discord\.com$/i.test(u.hostname)&&/^\/(api\/)?(oauth2|login)/.test(u.pathname)))return;
e.preventDefault();e.stopImmediatePropagation();window.mioExternal(u.href)},true);
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
window.mioDownload(JSON.stringify({id:m[1],item,name,file}));const sp=el.querySelector('span')||el;sp.textContent='已加入下载队列';el.style.opacity='.6'},true)})()`

var paneLinkScript = paneScriptFor(core.JinxxyBase())

type webPane struct {
	Mu       sync.Mutex
	St       *core.Store
	mode     string // "native" | "window" once started
	Port     int
	conn     *cdpConn
	target   string
	shown    bool        // on screen (native: placed and visible; window: a visible window)
	headless bool        // window mode: started without a window for a quiet login check
	kind     string      // "booth" | "pan" | "jinxxy" …: whose page it is
	restored int         // the browser (by its port) whose saved logins were put back
	moved    string      // the site of a page that was given to the default browser instead (handOver), for the UI to say
	movedURL string      // … its address and when,
	movedAt  time.Time   //
	handed   []time.Time // and when the last few were given: a page that keeps sending the browser there is not followed
	shownAt  time.Time
	origins  map[int]string // the page's script contexts (the page itself and its frames) → whose page each is
	frames   map[int]string // … and the frame each of them runs in (a download names the frame that began it)
	dlConn   *cdpConn       // the connection on which the browser was told to hand its downloads over (storepane.go)
	dlDir    string
}

var Pane = &webPane{}

// What the page asks of the downloads. The downloads set these (boothdl.go), so the page itself does not
// depend on them.
var (
	PaneDownloadsLeft   = func() int { return 0 }                              // files still downloading, shown in the bar
	PaneDownloadClicked = func(st *core.Store, id, item, name, file string) {} // a download button clicked on a Booth page
)

func paneBrowser() string {
	if b, _ := SyncBrowser(); IsChromiumExe(b) {
		return b
	}
	if e := core.FindEdge(); IsChromiumExe(e) {
		return e
	}
	return ""
}

// PaneMode: how pages can be shown: "native", "window", or "" (only the system browser).
func PaneMode() string {
	if NativePane != nil {
		return "native"
	}
	if paneBrowser() != "" {
		return "window"
	}
	return ""
}

func (p *webPane) proxy() string {
	if p.St == nil {
		return ""
	}
	p.St.Mu.RLock()
	px := strings.TrimSpace(p.St.Settings.Proxy)
	p.St.Mu.RUnlock()
	if px == "" || strings.EqualFold(px, "direct") {
		return px
	}
	if !strings.Contains(px, "://") {
		px = "http://" + px
	}
	return px
}

func XianyuBase() string {
	if v := os.Getenv("VRCLIB_XY_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://www.goofish.com"
}

func paneProfileDir() string { return filepath.Join(core.DataDir, "web-login") }

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
	switch PaneMode() {
	case "native":
		if p.Port == 0 {
			port, err := NativePane.Ensure(p.proxy())
			if err != nil {
				return false, err
			}
			p.Port, p.mode = port, "native"
		}
		return false, nil
	case "window":
		if p.Port > 0 {
			if _, err := cdpTargets(p.Port); err == nil {
				if !(visible && p.headless) {
					return false, nil
				}
				CloseDebugBrowser(p.Port) // a hidden check is still running: reopen with a window
				time.Sleep(600 * time.Millisecond)
			}
			p.Port, p.conn = 0, nil
		}
		if startURL == "" {
			startURL = "about:blank"
		}
		port, reused, err := launchPaneWindow(paneBrowser(), core.BoothProfileDir(), startURL, !visible, p.proxy())
		if err != nil {
			return false, err
		}
		p.Port, p.mode, p.headless, p.conn = port, "window", !visible && !reused, nil
		return !reused, nil
	}
	return false, errNoPane
}

// launchPaneWindow: an app-style window (no tabs, no address bar) on its own profile with a DevTools
// port on 127.0.0.1. The window profile is the one the Booth sync always used, so its login stays.
func launchPaneWindow(browser, profile, startURL string, headless bool, proxy string) (port int, reused bool, err error) {
	_ = os.MkdirAll(profile, 0755)
	if port, _ := ReadDevToolsPort(profile); port > 0 {
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
	cmd := core.BrowserCmd(browser, args)
	if err := cmd.Start(); err != nil {
		return 0, false, err
	}
	go func(c *exec.Cmd) { _ = c.Wait() }(cmd)
	for i := 0; i < 150; i++ {
		time.Sleep(150 * time.Millisecond)
		if port, _ := ReadDevToolsPort(profile); port > 0 {
			if _, err := cdpTargets(port); err == nil {
				return port, false, nil
			}
		}
	}
	return 0, false, errors.New("浏览器未开启调试端口")
}

// page: the DevTools connection to the pane's page. Caller holds p.mu.
func (p *webPane) page() (*cdpConn, error) {
	if p.conn != nil && p.conn.alive() {
		return p.conn, nil
	}
	p.conn = nil
	if p.Port == 0 {
		return nil, ErrCDPClosed
	}
	var pick *cdpTarget
	for i := 0; i < 20 && pick == nil; i++ {
		ts, err := cdpTargets(p.Port)
		if err != nil {
			p.Port = 0 // the window was closed
			return nil, ErrCDPClosed
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
			core.Logf("内置浏览器的页面：%+v", ts) // an unexpected kind of target
		}
		if pick == nil {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if pick == nil {
		return nil, errors.New("页面尚未就绪")
	}
	c, err := cdpDial(pick.WSURL, 6*time.Second)
	if err != nil {
		return nil, err
	}
	p.conn, p.target, p.origins, p.frames = c, pick.ID, map[int]string{}, map[int]string{}
	c.onEvent = p.event
	if p.restored != p.Port { // this browser was just started
		p.restored = p.Port
		if n := forgetXianyu(c); n > 0 {
			core.Logf("内置浏览器：已清除 %d 个闲鱼 / 淘宝 / 支付宝的 Cookie（这些网站不再在此浏览器中打开）", n)
		}
		if XianyuURL(pick.URL) { // (a window an older version left open on one of their pages)
			_, _ = c.call("Page.navigate", map[string]any{"url": "about:blank"}, 5*time.Second)
		}
		go p.watchWindows(p.Port)
		if n := restoreLogins(c); n > 0 {
			core.Logf("内置浏览器：放回了 %d 个保存的登录信息", n)
			if !strings.HasPrefix(pick.URL, "about:") {
				_, _ = c.call("Page.reload", nil, 5*time.Second)
			}
		}
	}
	_, _ = c.call("Page.enable", nil, 5*time.Second)
	_, _ = c.call("Runtime.enable", nil, 5*time.Second)
	_, _ = c.call("Runtime.addBinding", map[string]any{"name": "mioDownload"}, 5*time.Second)
	_, _ = c.call("Runtime.addBinding", map[string]any{"name": "mioExternal"}, 5*time.Second)
	_, _ = c.call("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": paneLinkScript}, 5*time.Second)
	_, _ = c.eval(paneLinkScript, false, 3*time.Second)
	p.downloadMode(c)
	return c, nil
}

func (p *webPane) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	p.Mu.Lock()
	c, err := p.page()
	p.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	return c.call(method, params, timeout)
}

// errXyInPane: 闲鱼 and the sites its pages lead to are not for this browser (xyview.go).
var errXyInPane = errors.New("闲鱼、淘宝和支付宝的页面不在此内置浏览器中打开")

// Open shows u in the pane (show=false: load it without putting it on screen).
func (p *webPane) Open(u, kind string, show bool) error {
	if u != "about:blank" {
		pu, err := PageURL(u)
		if err != nil {
			return err
		}
		if u = pu.String(); XianyuURL(u) {
			return errXyInPane
		}
	}
	if show {
		PaneTakesOver() // the page area may have been 闲鱼's
	}
	p.Mu.Lock()
	fresh, err := p.ensure(show, u)
	if err != nil {
		p.Mu.Unlock()
		return err
	}
	c, err := p.page()
	if err != nil {
		p.Mu.Unlock()
		return err
	}
	if kind != "" {
		p.kind = kind
	}
	p.downloadMode(c)
	if show && p.mode == "window" {
		p.shown, p.shownAt = true, time.Now()
	}
	p.Mu.Unlock()
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
	if NativePane == nil {
		return
	}
	if dpr <= 0 {
		dpr = 1
	}
	p.Mu.Lock()
	if show && p.Port == 0 {
		p.Mu.Unlock()
		return // nothing opened yet
	}
	if show && !p.shown {
		p.shownAt = time.Now()
	}
	p.shown = show
	p.Mu.Unlock()
	r := func(v float64) int { return int(v*dpr + 0.5) }
	NativePane.Place(r(x), r(y), r(x+w)-r(x), r(y+h)-r(y), show)
}

// Shown: the player can see the pane (and so may be using it).
func (p *webPane) Shown() bool {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	if p.mode == "window" {
		if p.Port == 0 || p.headless {
			return false
		}
		if _, err := cdpTargets(p.Port); err != nil {
			p.Port, p.conn, p.shown = 0, nil, false
			return false
		}
		return true
	}
	return p.shown
}

type PaneState struct {
	Mode    string     `json:"mode"`
	Open    bool       `json:"open"`
	Shown   bool       `json:"shown"`
	URL     string     `json:"url"`
	Title   string     `json:"title"`
	Back    bool       `json:"back"`
	Fwd     bool       `json:"fwd"`
	Loading bool       `json:"loading"`
	Kind    string     `json:"kind"`
	DL      int        `json:"dl"`                // downloads waiting or running (a click on the page may have added one)
	Files   []PaneFile `json:"files,omitempty"`   // files the pane's browser downloaded for the library (storepane.go)
	Moved   string     `json:"moved,omitempty"`   // the site of a page that was opened in the default browser instead, for a few seconds
	MovedAt int64      `json:"movedAt,omitempty"` // … and when (the page says each one once)
}

type navHistory struct {
	CurrentIndex int `json:"currentIndex"`
	Entries      []struct {
		ID    int    `json:"id"`
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"entries"`
}

// step: the id of the page one step back (-1) or forward (+1) in the list — past the pages of 闲鱼 and the sites
// its pages lead to, which the pane was taken out of (leaveXianyu) and does not go back to. -1: there is none.
func (h *navHistory) step(dir int) int {
	for i := h.CurrentIndex + dir; i >= 0 && i < len(h.Entries); i += dir {
		if !XianyuURL(h.Entries[i].URL) {
			return h.Entries[i].ID
		}
	}
	return -1
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
	s := PaneState{Mode: PaneMode(), DL: PaneDownloadsLeft()}
	s.Files = PaneFiles()
	for _, f := range s.Files {
		if f.Status == "running" || f.Status == "saving" {
			s.DL++
		}
	}
	p.Mu.Lock()
	running := p.Port > 0 && !p.headless
	s.Kind = p.kind
	if p.moved != "" && time.Since(p.movedAt) < 8*time.Second {
		s.Moved, s.MovedAt = p.moved, p.movedAt.UnixMilli()
	}
	p.Mu.Unlock()
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
		s.Back, s.Fwd = h.step(-1) >= 0, h.step(1) >= 0
	}
	if raw, err := p.call("Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true}, 2*time.Second); err == nil {
		s.Loading = !strings.Contains(string(raw), `"complete"`)
	}
	if s.URL == "about:blank" {
		s.URL = ""
	}
	return s
}

// Act: the toolbar buttons. Returns text for "url".
func (p *webPane) Act(act string) (string, error) {
	switch act {
	case "back", "forward":
		h, err := p.history()
		if err != nil {
			return "", err
		}
		dir := -1
		if act == "forward" {
			dir = 1
		}
		id := h.step(dir)
		if id < 0 {
			return "", nil
		}
		_, err = p.call("Page.navigateToHistoryEntry", map[string]any{"entryId": id}, 5*time.Second)
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
	case "close":
		p.Mu.Lock()
		defer p.Mu.Unlock()
		if p.mode == "window" && p.Port > 0 && !core.PurchaseBusy.Load() {
			if p.conn != nil {
				p.conn.Close()
			}
			CloseDebugBrowser(p.Port)
			p.Port, p.conn = 0, nil
		}
		p.shown = false
		return "", nil
	}
	return "", errors.New("未知操作")
}

// boothOrigin: a page of booth.pm or one of its shops (in tests: of the stand-in for it).
func boothOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if u.Scheme == "https" && (h == "booth.pm" || strings.HasSuffix(h, ".booth.pm")) {
		return true
	}
	for _, base := range []string{core.BoothWebBase(), core.BoothAccountsBase(), core.BoothDLBase()} { // other hosts only in tests
		if b, err := url.Parse(base); err == nil && !strings.HasSuffix(b.Hostname(), "booth.pm") && b.Scheme == u.Scheme && b.Host == u.Host {
			return true
		}
	}
	return false
}

// downloadID: Booth's ids are numbers.
func downloadID(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != "" && len(s) <= 20
}

// event: DevTools events of the pane's page. A download button clicked on a Booth page goes into the
// program's own downloads (unpacked into the library) instead of the browser's.
func (p *webPane) event(method string, params json.RawMessage) {
	switch method {
	case "Runtime.executionContextCreated":
		var e struct {
			Context struct {
				ID     int    `json:"id"`
				Origin string `json:"origin"`
				Aux    struct {
					Frame string `json:"frameId"`
				} `json:"auxData"`
			} `json:"context"`
		}
		if json.Unmarshal(params, &e) == nil && e.Context.ID != 0 {
			p.Mu.Lock()
			if p.origins != nil {
				p.origins[e.Context.ID] = e.Context.Origin
			}
			if p.frames != nil && e.Context.Aux.Frame != "" {
				p.frames[e.Context.ID] = e.Context.Aux.Frame
			}
			p.Mu.Unlock()
		}
		return
	case "Runtime.executionContextDestroyed":
		var e struct {
			ID int `json:"executionContextId"`
		}
		if json.Unmarshal(params, &e) == nil {
			p.Mu.Lock()
			delete(p.origins, e.ID)
			delete(p.frames, e.ID)
			p.Mu.Unlock()
		}
		return
	case "Runtime.executionContextsCleared":
		p.Mu.Lock()
		if p.origins != nil {
			p.origins = map[int]string{}
		}
		if p.frames != nil {
			p.frames = map[int]string{}
		}
		p.Mu.Unlock()
		return
	}
	if method == "Browser.downloadWillBegin" || method == "Browser.downloadProgress" {
		p.downloadEvent(method, params)
		return
	}
	if method == "Page.frameNavigated" {
		var e struct {
			Frame struct {
				Parent      string `json:"parentId"`
				URL         string `json:"url"`
				Unreachable string `json:"unreachableUrl"` // the address of a page that could not be loaded (url is the error page's)
			} `json:"frame"`
		}
		if json.Unmarshal(params, &e) != nil || e.Frame.Parent != "" {
			return
		}
		for _, u := range []string{e.Frame.Unreachable, e.Frame.URL} {
			if XianyuURL(u) {
				p.leaveXianyu(u)
				break
			}
		}
		return
	}
	if method != "Runtime.bindingCalled" || p.St == nil {
		return
	}
	var b struct {
		Name, Payload string
		Ctx           int `json:"executionContextId"`
	}
	if json.Unmarshal(params, &b) == nil && b.Name == "mioExternal" {
		p.externalClicked(b.Ctx, b.Payload)
		return
	}
	if json.Unmarshal(params, &b) != nil || b.Name != "mioDownload" {
		return
	}
	var d struct{ ID, Item, Name, File string }
	if json.Unmarshal([]byte(b.Payload), &d) != nil || !downloadID(d.ID) {
		return
	}
	if d.Item != "" && !downloadID(d.Item) {
		d.Item = ""
	}
	// every page in the pane can call the binding (闲鱼, the netdisk, a frame inside a Booth page): only a
	// Booth page's call counts. Asked of the page by its script context; of the pane's address when that
	// context is not known.
	p.Mu.Lock()
	origin, known := p.origins[b.Ctx]
	p.Mu.Unlock()
	if !known || origin == "" {
		origin = p.currentURL()
	}
	if !boothOrigin(origin) {
		core.Logf("内置页面：已忽略非 Booth 页面发起的下载请求")
		return
	}
	PaneDownloadClicked(p.St, d.ID, d.Item, d.Name, d.File)
}

// currentURL: where the pane's page is ("" when it cannot be told).
func (p *webPane) currentURL() string {
	h, err := p.history()
	if err != nil || h.CurrentIndex < 0 || h.CurrentIndex >= len(h.Entries) {
		return ""
	}
	return h.Entries[h.CurrentIndex].URL
}

// Cookies of the pane's profile for these sites.
func (p *webPane) Cookies(urls []string) ([]core.SavedCookie, error) {
	d := &cdpDriver{}
	p.Mu.Lock()
	c, err := p.page()
	p.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	d.c = c
	return d.Cookies(urls)
}

// ForgetSites removes the cookies of these sites (by domain suffix) from the pane's profile.
func (p *webPane) ForgetSites(suffixes []string) error {
	dropKeptLogins(suffixes)
	p.Mu.Lock()
	c, err := p.page()
	p.Mu.Unlock()
	if err != nil {
		return err
	}
	_, err = forgetCookies(c, suffixes)
	return err
}

// forgetCookies removes the cookies of these sites (by domain suffix) from the browser behind c; how many went.
func forgetCookies(c *cdpConn, suffixes []string) (int, error) {
	raw, err := c.call("Network.getAllCookies", nil, 6*time.Second)
	if err != nil {
		return 0, err
	}
	var r struct {
		Cookies []struct {
			Name      string          `json:"name"`
			Domain    string          `json:"domain"`
			Path      string          `json:"path"`
			Partition json.RawMessage `json:"partitionKey"`
		} `json:"cookies"`
	}
	_ = json.Unmarshal(raw, &r)
	n := 0
	for _, k := range r.Cookies {
		d := strings.ToLower(strings.TrimPrefix(k.Domain, "."))
		for _, s := range suffixes {
			if d == s || strings.HasSuffix(d, "."+s) {
				arg := map[string]any{"name": k.Name, "domain": k.Domain, "path": k.Path}
				if len(k.Partition) > 0 && string(k.Partition) != "null" {
					arg["partitionKey"] = k.Partition
				}
				if _, err := c.call("Network.deleteCookies", arg, 4*time.Second); err == nil {
					n++
				}
				break
			}
		}
	}
	return n, nil
}

// ---------- 闲鱼 is not for this browser ----------
//
// Versions up to 1.7.6 opened 闲鱼 here, kept its login cookies in the profile and a copy of them in
// web-session.dat. Both go: the cookies when the browser is first reached (forgetXianyu), the copy at the start
// of the program (DropXianyuLogins). And a page of the pane that goes to one of those sites after all (a link,
// a payment that leads to 支付宝) is taken out of it (leaveXianyu).

// the pages 闲鱼 and 淘宝 keep what they know of a browser on, besides cookies
var xyOrigins = []string{"https://www.goofish.com", "https://h5.m.goofish.com", "https://passport.goofish.com",
	"https://www.taobao.com", "https://login.taobao.com", "https://main.m.taobao.com", "https://www.alipay.com"}

// forgetXianyu: nothing of 闲鱼, 淘宝 or 支付宝 stays in the browser behind c. How many cookies went.
func forgetXianyu(c *cdpConn) int {
	n, _ := forgetCookies(c, xyHosts)
	if n > 0 { // it was used for them: what their pages stored goes too
		for _, o := range xyOrigins {
			_, _ = c.call("Storage.clearDataForOrigin", map[string]any{"origin": o, "storageTypes": "local_storage,indexeddb,service_workers,cache_storage"}, 4*time.Second)
		}
	}
	return n
}

// DropXianyuLogins takes 闲鱼's and 淘宝's login cookies out of web-session.dat. Called when the program starts.
func DropXianyuLogins() {
	keptMu.Lock()
	defer keptMu.Unlock()
	saved := loadKeptLogins()
	var keep []keptCookie
	for _, k := range saved {
		if !xyHost(k.Domain) {
			keep = append(keep, k)
		}
	}
	if len(keep) == len(saved) {
		return
	}
	keptLast = "?"
	saveKeptLogins(keep)
	if keptLast == "?" {
		core.Logf("旧版本保存的闲鱼 / 淘宝登录 Cookie 未能清除（web-session.dat 无法写入）；它们不会再被放回浏览器")
		return
	}
	core.Logf("已清除旧版本保存的 %d 个闲鱼 / 淘宝登录 Cookie", len(saved)-len(keep))
}

// handOver: an address of theirs that turned up in the pane's browser is given to the default browser, and the
// UI is told. Not the same one again within a few seconds, and not more than three a minute: a page that keeps
// sending the browser there is not followed (false).
func (p *webPane) handOver(u string) bool {
	pu, err := url.Parse(u)
	if err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
		return false
	}
	now := time.Now()
	p.Mu.Lock()
	recent := p.handed[:0]
	for _, t := range p.handed {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	ok := !(u == p.movedURL && now.Sub(p.movedAt) < 4*time.Second) && len(recent) < 3
	if ok {
		recent = append(recent, now)
	}
	p.handed, p.movedURL, p.movedAt, p.moved = recent, u, now, pu.Hostname()
	p.Mu.Unlock()
	if ok {
		core.Logf("内置页面：%s 的页面不在内置浏览器中打开，已改用默认浏览器", pu.Hostname()) // (the host only: an address can carry a token)
		_ = PaneOpenExternal(u)
	}
	return ok
}

// leaveXianyu: the pane's page has gone to 闲鱼 or to one of the sites its pages lead to. It is not left there:
// the address opens in the default browser, the pane goes back to the page it came from — to an empty one when
// there is none, or when that page keeps sending it there — and what the site left in the pane's profile goes.
func (p *webPane) leaveXianyu(u string) {
	back := -1
	if p.handOver(u) {
		// the nearest page before this one that is not one of theirs. The list of pages lags a moment behind the
		// event: it is asked for until it shows the page that was just gone to
		var h *navHistory
		for i := 0; i < 20; i++ {
			if hh, err := p.history(); err == nil && hh.CurrentIndex >= 0 && hh.CurrentIndex < len(hh.Entries) {
				if h = hh; XianyuURL(h.Entries[h.CurrentIndex].URL) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		if h != nil {
			if !XianyuURL(h.Entries[h.CurrentIndex].URL) {
				back = -2 // the pane is not on one of their pages (any more): it stays where it is
			} else {
				back = h.step(-1)
			}
		}
	}
	switch {
	case back >= 0:
		_, _ = p.call("Page.navigateToHistoryEntry", map[string]any{"entryId": back}, 5*time.Second)
	case back == -1:
		_, _ = p.call("Page.navigate", map[string]any{"url": "about:blank"}, 5*time.Second)
	}
	// wherever that led: the pane does not stay on one of their pages
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if cur := p.currentURL(); cur == "" || !XianyuURL(cur) {
			if cur != "" || i >= 5 {
				break
			}
		} else if i == 10 || i == 20 {
			_, _ = p.call("Page.navigate", map[string]any{"url": "about:blank"}, 5*time.Second)
		}
	}
	p.Mu.Lock()
	c := p.conn
	p.Mu.Unlock()
	if c != nil && c.alive() {
		_, _ = forgetCookies(c, xyHosts)
	}
}

// watchWindows looks after the other pages of the pane's browser: a window a page of the pane opened (a pop-up
// with a size, a link opened with Ctrl or the middle button). The pane's own page is seen to through its own
// connection (event); those windows are not connected to at all, so they are watched from the browser's side,
// for as long as it runs: one that goes to 闲鱼 or to one of the sites its pages lead to is closed, and the
// address opens in the default browser.
func (p *webPane) watchWindows(port int) {
	resp, err := LocalHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return
	}
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&v)
	resp.Body.Close()
	if v.WS == "" {
		return
	}
	c, err := cdpDial(v.WS, 4*time.Second)
	if err != nil {
		return
	}
	c.onEvent = func(method string, params json.RawMessage) {
		if method != "Target.targetCreated" && method != "Target.targetInfoChanged" {
			return
		}
		var e struct {
			Info struct {
				ID   string `json:"targetId"`
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(params, &e) != nil || (e.Info.Type != "page" && e.Info.Type != "webview") || !XianyuURL(e.Info.URL) {
			return
		}
		p.Mu.Lock()
		own := e.Info.ID == p.target
		p.Mu.Unlock()
		if own {
			return
		}
		_, _ = c.call("Target.closeTarget", map[string]any{"targetId": e.Info.ID}, 4*time.Second)
		p.handOver(e.Info.URL)
	}
	if _, err := c.call("Target.setDiscoverTargets", map[string]any{"discover": true}, 5*time.Second); err != nil {
		core.Logf("内置浏览器：无法监视它打开的其他窗口: %v", err)
		c.Close()
	}
}

// ---------- keeping logins across restarts ----------
//
// Some sites give their login cookies no expiry date ("until the browser closes"), so that login was gone every
// time the program was started again (an update restarts it). While the pane runs, such cookies of the sites the
// program is for are
//   - written back with an expiry date, so the browser keeps them in its profile like any other cookie, and
//   - saved (encrypted, like the download logins) in web-session.dat, from where the missing ones are put back
//     when the pane starts: the browser writes its cookies to disk only every half minute, and not at all when
//     it is ended abruptly.
// How long a login is good for is still up to the site.
//
// 闲鱼 and 淘宝 were among them up to 1.7.6. They are not: their pages do not open in the pane any more, and a
// login that is written back and put back is one of the things their risk control takes for a tool's (xyview.go).

var keepLoginSites = []string{"booth.pm", "pixiv.net", "baidu.com", "gumroad.com", "jinxxy.com"}

const keepLoginFor = 180 * 24 * time.Hour

func keepLoginSite(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(domain, "."))
	for _, s := range keepLoginSites {
		if d == s || strings.HasSuffix(d, "."+s) {
			return true
		}
	}
	for _, base := range []string{core.BoothWebBase(), core.PanBase(), core.GumroadBase(), core.JinxxyBase()} { // other hosts only in tests
		if u, err := url.Parse(base); err == nil && u.Hostname() == d {
			return true
		}
	}
	return false
}

// keptCookie: a browser cookie as DevTools gives and takes it.
type keptCookie struct {
	Name         string          `json:"name"`
	Value        string          `json:"value"`
	Domain       string          `json:"domain"` // ".site.com" for the site and its subdomains, "www.site.com" for that host only
	Path         string          `json:"path"`
	Expires      float64         `json:"expires"`
	HTTPOnly     bool            `json:"httpOnly,omitempty"`
	Secure       bool            `json:"secure,omitempty"`
	Session      bool            `json:"session,omitempty"`
	SameSite     string          `json:"sameSite,omitempty"`
	Priority     string          `json:"priority,omitempty"`
	SourceScheme string          `json:"sourceScheme,omitempty"`
	SourcePort   int             `json:"sourcePort,omitempty"`
	PartitionKey json.RawMessage `json:"partitionKey,omitempty"`
}

func (k keptCookie) key() string { return k.Domain + "|" + k.Path + "|" + k.Name }

func (k keptCookie) params() map[string]any {
	m := map[string]any{"name": k.Name, "value": k.Value, "path": k.Path, "secure": k.Secure, "httpOnly": k.HTTPOnly, "expires": k.Expires}
	if strings.HasPrefix(k.Domain, ".") {
		m["domain"] = k.Domain
	} else { // for that host only: it stays that way when given as an address
		scheme := "http"
		if k.Secure || k.SourceScheme == "Secure" {
			scheme = "https"
		}
		m["url"] = scheme + "://" + k.Domain + k.Path
	}
	if k.SameSite != "" {
		m["sameSite"] = k.SameSite
	}
	if k.Priority != "" {
		m["priority"] = k.Priority
	}
	if k.SourceScheme != "" && k.SourceScheme != "Unset" {
		m["sourceScheme"] = k.SourceScheme
		if k.SourcePort > 0 {
			m["sourcePort"] = k.SourcePort
		}
	}
	return m
}

var (
	keptMu   sync.Mutex
	keptLast string // what the file holds, to write it only when something changed
)

func keptLoginsFile() string { return filepath.Join(core.DataDir, "web-session.dat") }

func loadKeptLogins() []keptCookie {
	b, err := os.ReadFile(keptLoginsFile())
	if err != nil {
		return nil
	}
	dec, err := core.UnprotectData(b)
	if err != nil {
		return nil
	}
	var cs []keptCookie
	_ = json.Unmarshal(dec, &cs)
	return cs
}

func saveKeptLogins(cs []keptCookie) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].key() < cs[j].key() })
	b, _ := json.Marshal(cs)
	if string(b) == keptLast {
		return
	}
	if len(cs) == 0 {
		_ = os.Remove(keptLoginsFile())
		keptLast = string(b)
		return
	}
	if enc, err := core.ProtectData(b); err == nil && os.WriteFile(keptLoginsFile(), enc, 0600) == nil {
		keptLast = string(b)
	}
}

func browserCookies(c *cdpConn) ([]keptCookie, error) {
	raw, err := c.call("Network.getAllCookies", nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var r struct {
		Cookies []keptCookie `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return r.Cookies, nil
}

// KeepLogins looks after the login cookies of those sites (see above). It returns how many it gave an expiry date.
func (p *webPane) KeepLogins() int {
	p.Mu.Lock()
	c := p.conn
	ok := p.Port > 0 && c != nil && c.alive() // not worth starting the browser for
	p.Mu.Unlock()
	if !ok {
		return 0
	}
	keptMu.Lock()
	defer keptMu.Unlock()
	now := time.Now()
	cs, err := browserCookies(c)
	if err != nil {
		return 0
	}
	ours := map[string]bool{} // the ones this has given their date before
	for _, k := range loadKeptLogins() {
		ours[k.key()] = true
	}
	n := 0
	until := float64(now.Add(keepLoginFor).Unix())
	var keep []keptCookie
	for _, k := range cs {
		if !keepLoginSite(k.Domain) || (len(k.PartitionKey) > 0 && string(k.PartitionKey) != "null") {
			continue
		}
		stale := ours[k.key()] && k.Expires < float64(now.Add(keepLoginFor-7*24*time.Hour).Unix())
		if !k.Session && !stale {
			if ours[k.key()] {
				keep = append(keep, k)
			}
			continue
		}
		k.Session, k.Expires, k.PartitionKey = false, until, nil
		if _, err := c.call("Network.setCookie", k.params(), 3*time.Second); err == nil {
			if !stale {
				n++
			}
			keep = append(keep, k)
		}
	}
	saveKeptLogins(keep)
	return n
}

// restoreLogins puts back the saved login cookies the browser does not have (any more). Called once when the pane's
// page is first reached; a page already loaded is loaded again so it sees them.
func restoreLogins(c *cdpConn) int {
	keptMu.Lock()
	defer keptMu.Unlock()
	saved := loadKeptLogins()
	if len(saved) == 0 {
		return 0
	}
	cs, err := browserCookies(c)
	if err != nil {
		return 0
	}
	have := map[string]bool{}
	for _, k := range cs {
		have[k.key()] = true
	}
	n, now := 0, float64(time.Now().Unix())
	for _, k := range saved {
		if have[k.key()] || k.Expires <= now || !keepLoginSite(k.Domain) {
			continue
		}
		if _, err := c.call("Network.setCookie", k.params(), 3*time.Second); err == nil {
			n++
		}
	}
	return n
}

// dropKeptLogins forgets the saved login cookies of these sites (logging out).
func dropKeptLogins(suffixes []string) {
	keptMu.Lock()
	defer keptMu.Unlock()
	var keep []keptCookie
	for _, k := range loadKeptLogins() {
		d, gone := strings.TrimPrefix(k.Domain, "."), false
		for _, s := range suffixes {
			gone = gone || d == s || strings.HasSuffix(d, "."+s)
		}
		if !gone {
			keep = append(keep, k)
		}
	}
	keptLast = "?"
	saveKeptLogins(keep)
}

// KeepLoginsLoop runs for as long as the program does.
func KeepLoginsLoop() {
	for {
		time.Sleep(10 * time.Second)
		if n := Pane.KeepLogins(); n > 0 {
			core.Logf("内置浏览器：%d 个登录信息改为长期保存", n)
		}
	}
}

// CloseHidden ends a window-mode pane that was only started for a quiet check.
func (p *webPane) CloseHidden() {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	if p.mode == "window" && p.headless && p.Port > 0 {
		if p.conn != nil {
			p.conn.Close()
		}
		CloseDebugBrowser(p.Port)
		p.Port, p.conn, p.headless = 0, nil, false
	}
}

// ---------- the pane as the Booth sync window ----------

type PaneDriver struct {
	Start  time.Time
	seen   bool      // the player had the pane on screen during this sync
	hidden time.Time // since when it is off screen
}

func (d *PaneDriver) conn() (*cdpConn, error) {
	Pane.Mu.Lock()
	defer Pane.Mu.Unlock()
	return Pane.page()
}

func (d *PaneDriver) Eval(expr string, timeout time.Duration) (json.RawMessage, error) {
	c, err := d.conn()
	if err != nil {
		return nil, err
	}
	return c.eval(expr, false, timeout)
}

func (d *PaneDriver) Navigate(u string) error {
	_, err := Pane.call("Page.navigate", map[string]any{"url": u}, 15*time.Second)
	return err
}

func (d *PaneDriver) Reconnect() error {
	Pane.Mu.Lock()
	if Pane.conn != nil {
		Pane.conn.Close()
		Pane.conn = nil
	}
	_, err := Pane.page()
	Pane.Mu.Unlock()
	return err
}

func (d *PaneDriver) Dead() bool {
	Pane.Mu.Lock()
	defer Pane.Mu.Unlock()
	return Pane.Port == 0
}

func (d *PaneDriver) Close()        {}
func (d *PaneDriver) CloseBrowser() {}

func (d *PaneDriver) Cookies(urls []string) ([]core.SavedCookie, error) { return Pane.Cookies(urls) }

// UserLeft: the player closed the pane while the sync was still waiting for the login.
func (d *PaneDriver) UserLeft() bool { return paneLeft("booth", d.Start, &d.seen, &d.hidden) }

// PaneWatch: is the player still on the page a login is waited for on?
type PaneWatch struct {
	Kind   string // the tab the page was opened for
	Start  time.Time
	seen   bool
	hidden time.Time
}

// Left: the page area went over to another tab, or the player closed the pane (or never opened it).
func (w *PaneWatch) Left() bool { return paneLeft(w.Kind, w.Start, &w.seen, &w.hidden) }

// paneAway: a pane off screen for this long was closed; for less, a dialog or the details panel was over it.
var paneAway = 8 * time.Second

func paneLeft(kind string, start time.Time, seen *bool, hidden *time.Time) bool {
	Pane.Mu.Lock()
	other := Pane.kind != "" && Pane.kind != kind // the page area went over to Jinxxy, the netdisk …
	Pane.Mu.Unlock()
	if other {
		return true
	}
	if Pane.Shown() {
		*seen, *hidden = true, time.Time{}
		return false
	}
	if !*seen {
		return time.Since(start) > 15*time.Second
	}
	if hidden.IsZero() {
		*hidden = time.Now()
	}
	return time.Since(*hidden) > paneAway
}
