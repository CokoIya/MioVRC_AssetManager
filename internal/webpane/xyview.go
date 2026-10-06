package webpane

import (
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"vrclib/internal/core"
)

// 闲鱼's pages.
//
// The other built-in pages are driven by the program (the pane, webpane.go): a DevTools port, a script in
// every page, login cookies written back and put back, the proxy of the settings. To 闲鱼's risk control that is
// a login from an automation tool, possibly from abroad — a player's account was logged out everywhere, asked
// for face verification and restricted. So 闲鱼, and the sites its pages lead to (淘宝's login, 支付宝's
// payment), never open in that pane. They open in one of two places:
//
//   - a view of their own inside the window (xyview_windows.go): a second WebView2 on a profile of its own, in
//     which the program does nothing. No DevTools port, nothing put into its pages, no script run in them, no
//     cookie read or written, and never the proxy of the settings. How long a login lasts is 闲鱼's business:
//     its login cookies end with the browser, so the player logs in again after the program was closed. The
//     toolbar is served from the host side (where the view is, its title, its history). A window a page wants
//     to open (an item in a new tab, a login or payment window) is the browser's own, with the request the page
//     made: the program does not stand in for it.
//   - the default browser, where there is no such view (no WebView2 on this computer, the interface in a browser
//     tab, the program run as administrator — WebView2 then ignores the arguments the view is started with) or
//     the player would rather not have it.
//
// A netdisk share a seller sent is taken from the clipboard (core.ClipboardText), not from the page — the same
// look at the clipboard that, on every tab, offers the links the player copies (server/paneapi.go).

// XyState: what the toolbar shows of the view.
type XyState struct {
	URL, Title         string
	Back, Fwd, Loading bool
}

// xyNativeAPI is the view inside the app's own window; NativeXy is nil when that window is not running.
type xyNativeAPI interface {
	Open(u string) error             // creates the view when there is none, and goes to u
	Holding() bool                   // it was sent to a page and has not let go of it (known without asking the view)
	Place(x, y, w, h int, show bool) // physical pixels in the window's client area
	State() (XyState, bool)          // false: there is no view (yet, or any more)
	Act(act string)                  // back, forward, reload, stop
	Blank()                          // lets go of the page shown (hidden, on an empty page)
}

var NativeXy xyNativeAPI

// the sites a 闲鱼 visit leads to: itself, the login, the payment, and 淘宝's short links (what 闲鱼 shares)
var xyHosts = []string{"goofish.com", "taobao.com", "tmall.com", "alipay.com", "xianyu.com", "tb.cn"}

var (
	errBadURL   = errors.New("网址无效")
	rePlainHost = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*\.?$`)
)

// PageURL: an address as the built-in browsers take it, in the form they are given it. Only what reads the same
// to this program and to a browser gets through: http or https, a plain host (letters, digits, hyphens and
// dots — every site the program opens has one), no user part, no backslash, control character or bad escape. An
// address that a browser would take to another host than the one checked here must not reach one.
func PageURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return nil, errBadURL
		}
	}
	p, err := url.Parse(raw)
	if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Opaque != "" || p.User != nil || !rePlainHost.MatchString(p.Hostname()) {
		return nil, errBadURL
	}
	if port := p.Port(); strings.Contains(p.Host, ":") && (port == "" || len(port) > 5 || strings.Trim(port, "0123456789") != "") {
		return nil, errBadURL
	}
	return p, nil
}

// XianyuURL: an address of 闲鱼 or of one of the sites its pages lead to (in tests: of the stand-in for it).
func XianyuURL(u string) bool {
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil || p.Host == "" {
		return false
	}
	if xyHost(p.Hostname()) {
		return true
	}
	if b, err := url.Parse(XianyuBase()); err == nil && !xyHost(b.Hostname()) && b.Scheme == p.Scheme && b.Host == p.Host { // another host only in tests
		return b.Path == "" || p.Path == b.Path || strings.HasPrefix(p.Path, strings.TrimRight(b.Path, "/")+"/")
	}
	return false
}

func xyHost(h string) bool {
	h = strings.ToLower(strings.Trim(h, "."))
	for _, s := range xyHosts {
		if h == s || strings.HasSuffix(h, "."+s) {
			return true
		}
	}
	return false
}

func xyProfileDir() string { return filepath.Join(core.DataDir, "web-xianyu") }

// XyCanEmbed: this computer can show 闲鱼 inside the window.
func XyCanEmbed() bool { return NativeXy != nil }

// XyEmbedded: 闲鱼 opens inside the window — the view is there and the player has not chosen the default browser.
func XyEmbedded(st *core.Store) bool {
	if NativeXy == nil || st == nil {
		return false
	}
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	return !st.Settings.XyExternal
}

// XyMode: where 闲鱼 opens, for the page: "embed" or "external".
func XyMode(st *core.Store) string {
	if XyEmbedded(st) {
		return "embed"
	}
	return "external"
}

// which of the two views the page area shows
var (
	viewMu   sync.Mutex
	xyActive bool
	xyShown  bool
	viewGen  int // moves on whenever the page area is given to the pane, or 闲鱼 to the default browser
)

func xyShowing() bool {
	viewMu.Lock()
	defer viewMu.Unlock()
	return xyActive && NativeXy != nil
}

// OpenXianyu shows a 闲鱼 page: in its own view, or (external) in the default browser. resume: the page area is
// only given back to the view, which stays on the page it has — u is where it goes when it has none; in the
// default browser nothing is opened for that (the tab coming into view must not open a browser window).
func OpenXianyu(st *core.Store, u string, resume bool) (external bool, err error) {
	p, err := PageURL(u)
	if err != nil {
		return false, err
	}
	u = p.String()
	if !XyEmbedded(st) {
		if resume {
			return true, nil
		}
		return true, PaneOpenExternal(u)
	}
	viewMu.Lock()
	gen := viewGen
	viewMu.Unlock()
	if !(resume && NativeXy.Holding()) {
		if err := NativeXy.Open(u); err != nil { // (the first one starts the view's browser: seconds)
			return false, err
		}
	}
	viewMu.Lock()
	if viewGen != gen {
		// meanwhile the player went to a page of the pane, or chose the default browser: the page area is not
		// taken from under that
		viewMu.Unlock()
		if !XyEmbedded(st) {
			NativeXy.Blank()
			return true, nil
		}
		return false, nil
	}
	was := xyActive
	xyActive = true
	viewMu.Unlock()
	if !was {
		Pane.Place(0, 0, 0, 0, 1, false) // the page area is 闲鱼's now
	}
	return false, nil
}

// PaneTakesOver: a page was opened in the pane for the player to see: the page area is the pane's again.
func PaneTakesOver() {
	viewMu.Lock()
	was := xyActive
	xyActive, xyShown = false, false
	viewGen++
	viewMu.Unlock()
	if was && NativeXy != nil {
		NativeXy.Place(0, 0, 0, 0, false)
	}
}

// XyLeave: the player chose the default browser: the view inside the window lets go of its page.
func XyLeave() {
	PaneTakesOver()
	if NativeXy != nil {
		NativeXy.Blank()
	}
}

// PlacePage puts the view the page area shows on screen (or hides it). The UI reports where its page area is,
// and whose it is at the moment (forXy: 闲鱼's): a view that is not the one the UI means stays hidden — the tab
// has changed and the page for it is still on its way.
func PlacePage(x, y, w, h, dpr float64, show, forXy bool) {
	if !xyShowing() {
		Pane.Place(x, y, w, h, dpr, show && !forXy)
		return
	}
	show = show && forXy
	if dpr <= 0 {
		dpr = 1
	}
	r := func(v float64) int { return int(v*dpr + 0.5) }
	viewMu.Lock()
	xyShown = show
	viewMu.Unlock()
	NativeXy.Place(r(x), r(y), r(x+w)-r(x), r(y+h)-r(y), show)
}

// PageState: the state of the view the page area shows.
func PageState() PaneState {
	if !xyShowing() {
		return Pane.State()
	}
	s := PaneState{Mode: PaneMode(), DL: PaneDownloadsLeft(), Kind: "xianyu"}
	s.Files = PaneFiles()
	for _, f := range s.Files {
		if f.Status == "running" || f.Status == "saving" {
			s.DL++
		}
	}
	x, ok := NativeXy.State()
	if !ok {
		return s
	}
	viewMu.Lock()
	s.Shown = xyShown
	viewMu.Unlock()
	s.Open, s.URL, s.Title, s.Back, s.Fwd, s.Loading = true, x.URL, x.Title, x.Back, x.Fwd, x.Loading
	if s.URL == "about:blank" {
		s.URL, s.Title = "", ""
	}
	return s
}

// PageAct: a toolbar button, for the view the page area shows. Returns text for "url". forXy: the view the page
// means (as with PlacePage): a button pressed while the other one still has the page area does nothing.
func PageAct(act string, forXy bool) (string, error) {
	if forXy != xyShowing() {
		return "", nil
	}
	if !forXy {
		return Pane.Act(act)
	}
	switch act {
	case "back", "forward", "reload", "stop":
		NativeXy.Act(act)
		return "", nil
	case "url":
		x, _ := NativeXy.State()
		if x.URL == "about:blank" {
			return "", nil
		}
		return x.URL, nil
	case "front":
		return "", nil
	case "close":
		PlacePage(0, 0, 0, 0, 1, false, true)
		return "", nil
	}
	return "", errors.New("未知操作")
}
