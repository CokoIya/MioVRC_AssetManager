package webpane

import (
	"encoding/json"
	"errors"
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
	Mu       sync.Mutex
	St       *core.Store
	mode     string // "native" | "window" once started
	Port     int
	conn     *cdpConn
	target   string
	shown    bool   // on screen (native: placed and visible; window: a visible window)
	headless bool   // window mode: started without a window for a quiet login check
	kind     string // "booth" | "xianyu": which tab the page belongs to
	restored int    // the browser (by its port) whose saved logins were put back
	shownAt  time.Time
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
	return 0, false, errors.New("浏览器没有打开调试端口")
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
		return nil, errors.New("页面还没准备好")
	}
	c, err := cdpDial(pick.WSURL, 6*time.Second)
	if err != nil {
		return nil, err
	}
	p.conn, p.target = c, pick.ID
	c.onEvent = p.event
	if p.restored != p.Port { // this browser was just started
		p.restored = p.Port
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
	_, _ = c.call("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": paneLinkScript}, 5*time.Second)
	_, _ = c.eval(paneLinkScript, false, 3*time.Second)
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

// Open shows u in the pane (show=false: load it without putting it on screen).
func (p *webPane) Open(u, kind string, show bool) error {
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
	s := PaneState{Mode: PaneMode(), DL: PaneDownloadsLeft()}
	p.Mu.Lock()
	running := p.Port > 0 && !p.headless
	s.Kind = p.kind
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
		p.Mu.Lock()
		c, err := p.page()
		p.Mu.Unlock()
		if err != nil {
			return "", err
		}
		return c.evalString("String(window.getSelection ? getSelection() : '')", 3*time.Second)
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

// event: DevTools events of the pane's page. A download button clicked on a Booth page goes into the
// program's own downloads (unpacked into the library) instead of the browser's.
func (p *webPane) event(method string, params json.RawMessage) {
	if method != "Runtime.bindingCalled" || p.St == nil {
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
	PaneDownloadClicked(p.St, d.ID, d.Item, d.Name, d.File)
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

// ---------- keeping logins across restarts ----------
//
// Some sites give their login cookies no expiry date ("until the browser closes"). 闲鱼 / 淘宝 do, so that login
// was gone every time the program was started again (an update restarts it). While the pane runs, such cookies of
// the sites the program is for are
//   - written back with an expiry date, so the browser keeps them in its profile like any other cookie, and
//   - saved (encrypted, like the download logins) in web-session.dat, from where the missing ones are put back
//     when the pane starts: the browser writes its cookies to disk only every half minute, and not at all when
//     it is ended abruptly.
// How long a login is good for is still up to the site.

var keepLoginSites = []string{"goofish.com", "taobao.com", "booth.pm", "pixiv.net", "baidu.com", "gumroad.com"}

const keepLoginFor = 180 * 24 * time.Hour

func keepLoginSite(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(domain, "."))
	for _, s := range keepLoginSites {
		if d == s || strings.HasSuffix(d, "."+s) {
			return true
		}
	}
	for _, base := range []string{XianyuBase(), core.BoothWebBase(), core.PanBase(), core.GumroadBase()} { // other hosts only in tests
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
	Start time.Time
	seen  bool // the player had the pane on screen during this sync
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

// userLeft: the player closed the pane while the sync was still waiting for the login.
func (d *PaneDriver) userLeft() bool {
	Pane.Mu.Lock()
	other := Pane.kind == "xianyu" || Pane.kind == "pan" // the page area went over to 闲鱼 or the netdisk
	Pane.Mu.Unlock()
	if other {
		return true
	}
	if Pane.Shown() {
		d.seen = true
		return false
	}
	return d.seen || time.Since(d.Start) > 15*time.Second
}
