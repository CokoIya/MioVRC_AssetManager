package webpane

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
)

func TestXianyuURL(t *testing.T) {
	for u, want := range map[string]bool{
		"https://www.goofish.com/":                         true,
		"https://www.goofish.com/search?q=vrchat":          true,
		"https://h5.m.goofish.com/item?id=1":               true,
		"https://login.taobao.com/member/login.jhtml":      true,
		"https://TAOBAO.com":                               true,
		"https://detail.tmall.com/item.htm":                true,
		"https://cashier.alipay.com/pay?token=x":           true,
		"http://www.xianyu.com./":                          true,
		"https://m.tb.cn/h.abc123":                         true,
		"https://example-tb.cn/":                           false,
		"https://goofish.com.evil.example/":                false,
		"https://notgoofish.com/":                          false,
		"https://booth.pm/ja/items/1":                      false,
		"https://pan.baidu.com/s/1abc":                     false,
		"https://example.com/?next=https://www.taobao.com": false,
		"about:blank":                                      false,
		"":                                                 false,
		"goofish.com":                                      false, // (not an address)
	} {
		if XianyuURL(u) != want {
			t.Errorf("%q: 闲鱼's = %v, want %v", u, !want, want)
		}
	}
	// the stand-in the tests use is one path of a server that also plays the other sites
	t.Setenv("VRCLIB_XY_BASE", "http://127.0.0.1:8123/xy")
	for u, want := range map[string]bool{"http://127.0.0.1:8123/xy": true, "http://127.0.0.1:8123/xy/search?q=1": true,
		"http://127.0.0.1:8123/xyz": false, "http://127.0.0.1:8123/ja/items/1": false, "http://127.0.0.1:9999/xy/": false, "https://127.0.0.1:8123/xy/": false} {
		if XianyuURL(u) != want {
			t.Errorf("stand-in %q: 闲鱼's = %v, want %v", u, !want, want)
		}
	}
}

// Only an address that reads the same to this program and to a browser gets to a built-in browser, and in the
// form it was checked in.
func TestPageURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://booth.pm/ja/items/123":                         "https://booth.pm/ja/items/123",
		"  https://booth.pm/ja/search/衣装?q=a b#x  ":             "https://booth.pm/ja/search/%E8%A1%A3%E8%A3%85?q=a b#x",
		"http://127.0.0.1:8123/xy/im":                           "http://127.0.0.1:8123/xy/im",
		"https://pan.baidu.com/disk/main#/index?path=%2F":       "https://pan.baidu.com/disk/main#/index?path=%2F",
		"HTTPS://Booth.PM/x":                                    "https://Booth.PM/x",
		"https://www.goofish.com./":                             "https://www.goofish.com./",
		"https://drive.google.com/file/d/1AbC/view?usp=sharing": "https://drive.google.com/file/d/1AbC/view?usp=sharing",
	} {
		p, err := PageURL(raw)
		if err != nil || p.String() != want {
			t.Errorf("%q → %v %v, want %q", raw, p, err, want)
		}
	}
	// what a browser reads as another host than the one a check here would see, or as no address of a site at all
	for _, raw := range []string{
		"", "about:blank", "javascript:alert(1)", "file:///C:/Windows", "data:text/html,x", "ftp://booth.pm/", "//booth.pm/x", "booth.pm/x",
		"https:///www.goofish.com/",   // a browser drops the empty host and takes the next part for it
		"https://www.goofish.com\\im", // … and reads a backslash as a slash
		"https://booth.pm\\@www.goofish.com/",
		"https://booth.pm@login.taobao.com/x",   // a user part
		"https://booth.pm:x@login.taobao.com/x", //
		"https://a b@www.goofish.com/",
		"https://www.goofish%2ecom/",   // an escape in the host
		"https://www.goofish.com/%zz",  // a bad escape: unreadable here, read by a browser
		"https://www.goofish.com/a\tb", // a tab is dropped by a browser
		"https://www.goofish.com/\x01",
		"https://www.goofish.com/\x00",
		"https://www.goofish.com/\r\nHost: x",
		"https://www.ｇｏｏｆｉｓｈ.com/", // a host a browser folds to plain letters
		"https://www.goofish。com/",
		"https://xn--bcher-kva.example/..//", "https://[::1]/", "https://booth.pm:/x", "https://booth.pm:99999999/", "https://booth.pm:8a/",
		"https://-booth.pm/", "https://booth..pm/", "https://.booth.pm/", "https://booth_x.pm/",
	} {
		if raw == "https://xn--bcher-kva.example/..//" { // (punycode is plain letters: let through, and judged by its host)
			if _, err := PageURL(raw); err != nil {
				t.Errorf("%q: %v", raw, err)
			}
			continue
		}
		if p, err := PageURL(raw); err == nil {
			t.Errorf("%q got through as %q", raw, p.String())
		}
	}
}

// what a view inside the window is asked to do
type xyRecorder struct {
	mu    sync.Mutex
	calls []string
	url   string
	fail  error
	slow  func() // what happens while the view is starting
}

func (r *xyRecorder) add(s string) {
	r.mu.Lock()
	r.calls = append(r.calls, s)
	r.mu.Unlock()
}
func (r *xyRecorder) take() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := strings.Join(r.calls, " | ")
	r.calls = nil
	return s
}
func (r *xyRecorder) Open(u string) error {
	if r.fail != nil {
		return r.fail
	}
	if r.slow != nil {
		r.slow()
	}
	r.mu.Lock()
	r.url = u
	r.mu.Unlock()
	r.add("open " + u)
	return nil
}
func (r *xyRecorder) Holding() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.url != "" && r.url != "about:blank"
}
func (r *xyRecorder) Place(x, y, w, h int, show bool) {
	if show {
		r.add("show " + strings.Join([]string{itoa(x), itoa(y), itoa(w), itoa(h)}, ","))
	} else {
		r.add("hide")
	}
}
func (r *xyRecorder) State() (XyState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return XyState{URL: r.url, Title: "t", Back: true}, r.url != ""
}
func (r *xyRecorder) Act(act string) { r.add("act " + act) }
func (r *xyRecorder) Blank() {
	r.mu.Lock()
	r.url = "about:blank"
	r.mu.Unlock()
	r.add("blank")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

// 闲鱼 opens in its own view, or in the default browser — never in the pane.
func TestXianyuRouting(t *testing.T) {
	core.DataDir = t.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	var opened []string
	ext := PaneOpenExternal
	PaneOpenExternal = func(u string) error { opened = append(opened, u); return nil }
	defer func() {
		PaneOpenExternal, NativeXy = ext, nil
		PaneTakesOver()
	}()
	home := "https://www.goofish.com/"

	// no view inside the window (no WebView2, or the interface in a browser tab): the default browser
	NativeXy = nil
	if XyCanEmbed() || XyMode(st) != "external" {
		t.Fatal("embedded without a view")
	}
	if external, err := OpenXianyu(st, home, false); !external || err != nil || len(opened) != 1 || opened[0] != home {
		t.Fatalf("no view: external %v, err %v, opened %q", external, err, opened)
	}
	if external, err := OpenXianyu(st, home, true); !external || err != nil || len(opened) != 1 {
		t.Errorf("the tab coming into view opened a browser window: %q", opened)
	}
	for _, bad := range []string{"", "javascript:alert(1)", "file:///C:/x", "https://www.goofish.com/\x00", "https://www.goofish.com/\r\nX: y", "https://booth.pm@www.goofish.com/", "https://www.goofish.com\\im"} {
		if _, err := OpenXianyu(st, bad, false); !errors.Is(err, errBadURL) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if len(opened) != 1 {
		t.Errorf("a bad address reached the browser: %q", opened)
	}
	if s := PageState(); s.Kind == "xianyu" {
		t.Errorf("the page area is 闲鱼's without a view: %+v", s)
	}

	// a view inside the window
	rec := &xyRecorder{}
	NativeXy = rec
	if !XyCanEmbed() || XyMode(st) != "embed" {
		t.Fatal("not embedded with a view")
	}
	if external, err := OpenXianyu(st, home, false); external || err != nil || len(opened) != 1 {
		t.Fatalf("view: external %v, err %v, opened %q", external, err, opened)
	}
	if got := rec.take(); got != "open "+home {
		t.Errorf("view asked: %s", got)
	}
	s := PageState()
	if s.Kind != "xianyu" || !s.Open || s.URL != home || !s.Back || s.Shown {
		t.Errorf("state %+v", s)
	}
	// where the page area is: only when the page means 闲鱼
	PlacePage(10, 20, 300, 200, 1.5, true, true)
	if got := rec.take(); got != "show 15,30,450,300" {
		t.Errorf("placed: %s", got)
	}
	if !PageState().Shown {
		t.Error("not shown after being placed")
	}
	PlacePage(10, 20, 300, 200, 1.5, true, false) // the tab has changed, its page is still on its way
	if got := rec.take(); got != "hide" {
		t.Errorf("shown for another tab: %s", got)
	}
	PlacePage(0, 0, 0, 0, 1, false, true)
	rec.take()
	// the toolbar
	for _, a := range []string{"back", "forward", "reload", "stop"} {
		if _, err := PageAct(a, true); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
	if got := rec.take(); got != "act back | act forward | act reload | act stop" {
		t.Errorf("toolbar: %s", got)
	}
	if u, err := PageAct("url", true); u != home || err != nil {
		t.Errorf("url %q %v", u, err)
	}
	if text, _ := PageAct("selection", true); text != "" || rec.take() != "" {
		t.Errorf("something was read from a 闲鱼 page: %q", text)
	}
	// a button of the other tab's toolbar, pressed while the page area is still 闲鱼's, does nothing here
	for _, a := range []string{"back", "reload", "url", "close"} {
		if text, err := PageAct(a, false); text != "" || err != nil || rec.take() != "" {
			t.Errorf("%s meant for the pane reached the view: %q %v", a, text, err)
		}
	}
	// coming back to the tab: the view stays on the page it has
	rec.url = "https://www.goofish.com/im"
	if _, err := OpenXianyu(st, home, true); err != nil || rec.take() != "" || PageState().URL != "https://www.goofish.com/im" {
		t.Errorf("the page was replaced on coming back: %v", err)
	}
	if _, err := PageAct("back", true); err != nil || rec.take() != "act back" { // (its toolbar again)
		t.Error("the toolbar after coming back")
	}
	// a page opened in the pane for the player to see: the page area is the pane's again
	PaneTakesOver()
	if got := rec.take(); got != "hide" {
		t.Errorf("taken over: %s", got)
	}
	if PageState().Kind == "xianyu" {
		t.Error("still 闲鱼's after the pane took over")
	}
	PlacePage(10, 20, 300, 200, 1, true, true) // (the page still means 闲鱼: nothing of the pane's is shown for it)
	if got := rec.take(); got != "" {
		t.Errorf("the view was placed while the pane has the page area: %s", got)
	}
	// while the view was starting (seconds, the first time) the player went on to a page of the pane: the page area
	// is not taken from under that page when the view is there at last
	rec.url = ""
	rec.slow = func() { PaneTakesOver() }
	if external, err := OpenXianyu(st, home, false); external || err != nil {
		t.Fatal(external, err)
	}
	rec.slow = nil
	if got := rec.take(); got != "open "+home || PageState().Kind == "xianyu" {
		t.Errorf("the page area was taken back from the pane: %s, %+v", got, PageState())
	}
	// … or chose the default browser: the page that arrives too late is let go of
	rec.slow = func() {
		st.Mu.Lock()
		st.Settings.XyExternal = true
		st.Mu.Unlock()
		XyLeave()
	}
	if external, err := OpenXianyu(st, home, false); !external || err != nil || len(opened) != 1 {
		t.Errorf("chosen meanwhile: external %v err %v opened %q", external, err, opened)
	}
	rec.slow = nil
	if got := rec.take(); got != "blank | open "+home+" | blank" || rec.Holding() || PageState().Kind == "xianyu" {
		t.Errorf("a page was kept in the view after the default browser was chosen: %s", got)
	}
	st.Settings.XyExternal = false
	// the view cannot be started: said, not swallowed, and nothing in the default browser behind the player's back
	rec.fail = errors.New("内嵌浏览器启动失败")
	if external, err := OpenXianyu(st, home, false); external || err == nil || len(opened) != 1 {
		t.Errorf("failing view: external %v err %v opened %q", external, err, opened)
	}
	rec.fail = nil
	// the player would rather have the default browser
	if _, err := OpenXianyu(st, home, false); err != nil {
		t.Fatal(err)
	}
	rec.take()
	st.Settings.XyExternal = true
	XyLeave()
	if got := rec.take(); got != "hide | blank" {
		t.Errorf("leaving: %s", got)
	}
	if XyMode(st) != "external" || PageState().Kind == "xianyu" {
		t.Error("still embedded after choosing the browser")
	}
	if external, err := OpenXianyu(st, home+"im", false); !external || err != nil || len(opened) != 2 || opened[1] != home+"im" || rec.take() != "" {
		t.Errorf("external by choice: %v %v %q", external, err, opened)
	}
}

// The pane the program drives does not open 闲鱼, 淘宝 or 支付宝, whoever asks.
func TestPaneRefusesXianyu(t *testing.T) {
	t.Setenv("VRCLIB_BROWSER", "") // (nothing to start: a refusal comes before any browser)
	for _, u := range []string{"https://www.goofish.com/", "https://login.taobao.com/", "https://cashier.alipay.com/x", "https://m.tb.cn/h.x", "HTTPS://WWW.GOOFISH.COM./"} {
		for _, show := range []bool{true, false} {
			if err := Pane.Open(u, "booth", show); !errors.Is(err, errXyInPane) {
				t.Errorf("%s (show %v): %v", u, show, err)
			}
		}
	}
	// … nor anything a browser would read as one of them although it does not look like it here
	for _, u := range []string{"https://booth.pm@login.taobao.com\\x", "https://pan.baidu.com@cashier.alipay.com/%zz", "https:///www.goofish.com/", "https://www.goofish.com\\im", "https://www.goofish%2ecom/", "javascript:alert(1)"} {
		if err := Pane.Open(u, "booth", false); !errors.Is(err, errBadURL) {
			t.Errorf("%q: %v", u, err)
		}
	}
	for _, d := range []string{".goofish.com", "www.goofish.com", ".taobao.com", "login.taobao.com", ".alipay.com", ".tmall.com"} {
		if keepLoginSite(d) {
			t.Errorf("%s: its login would be written back and put back", d)
		}
	}
}

// What versions up to 1.7.6 saved of 闲鱼's login goes at the start; the other sites' stays.
func TestDropXianyuLogins(t *testing.T) {
	core.DataDir = t.TempDir()
	keptLast = ""
	file := filepath.Join(core.DataDir, "web-session.dat")
	until := float64(time.Now().Add(time.Hour).Unix())
	DropXianyuLogins() // nothing saved: nothing written
	if core.StatOK(file) {
		t.Fatal("a file out of nothing")
	}
	saveKeptLogins([]keptCookie{{Name: "cookie2", Value: "a", Domain: ".goofish.com", Path: "/", Expires: until},
		{Name: "unb", Value: "u", Domain: ".taobao.com", Path: "/", Expires: until},
		{Name: "sgcookie", Value: "s", Domain: "login.taobao.com", Path: "/", Expires: until},
		{Name: "BDUSS", Value: "b", Domain: ".baidu.com", Path: "/", Expires: until},
		{Name: "_plaza_session", Value: "p", Domain: ".booth.pm", Path: "/", Expires: until}})
	DropXianyuLogins()
	got := loadKeptLogins()
	if len(got) != 2 || got[0].Name != "BDUSS" || got[1].Name != "_plaza_session" {
		t.Fatalf("left: %+v", got)
	}
	before, _ := os.ReadFile(file)
	DropXianyuLogins() // nothing of theirs left: the file is not written again
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Error("written again with nothing to remove")
	}
	// only theirs in it: no file left
	keptLast = ""
	saveKeptLogins([]keptCookie{{Name: "cookie2", Value: "a", Domain: ".goofish.com", Path: "/", Expires: until}})
	DropXianyuLogins()
	if core.StatOK(file) {
		t.Error("nothing left: no file")
	}
}

// With a real browser (VRCLIB_TEST_BROWSER names a Chromium; skipped without one): what is left of 闲鱼 in the
// pane's profile goes when its browser is first reached; a page of the pane that goes to 闲鱼 after all is taken
// back out of it, the address handed to the default browser; so is a window a page of the pane opens.
func TestPaneLeavesXianyu(t *testing.T) {
	browser := os.Getenv("VRCLIB_TEST_BROWSER")
	if browser == "" {
		t.Skip("VRCLIB_TEST_BROWSER not set")
	}
	page := func(title, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(`<html><head><title>` + title + `</title></head><body>` + body + `</body></html>`))
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/shop", page("shop", `<a id="x" href="/xy/item?id=1">on 闲鱼</a>
		<button id="pop" onclick="window.open('/xy/pop?t=1','_blank','width=300,height=200')">a pop-up</button>
		<button id="tab" onclick="window.open('/other','_blank','noopener,width=300,height=200')">another window</button>`))
	mux.HandleFunc("/other", page("other", `not theirs`))
	mux.HandleFunc("/hop", page("hop", `<script>location.href="/xy/pay"</script>`))                                                            // sends on to 闲鱼 while it loads
	mux.HandleFunc("/loop", page("loop", `<script>addEventListener("pageshow",()=>setTimeout(()=>{location.href="/xy/again"},400))</script>`)) // … and this one every time it is shown
	mux.HandleFunc("/xy/dead", func(w http.ResponseWriter, r *http.Request) {                                                                  // a page of theirs that cannot be loaded
		if hj, ok := w.(http.Hijacker); ok {
			if c, _, err := hj.Hijack(); err == nil {
				c.Close()
			}
		}
	})
	mux.HandleFunc("/xy/", page("xy", `闲鱼`))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	core.DataDir = t.TempDir()
	t.Setenv("VRCLIB_BROWSER", browser)
	t.Setenv("VRCLIB_HEADLESS", "1")
	t.Setenv("VRCLIB_XY_BASE", srv.URL+"/xy")
	var mu sync.Mutex
	var opened []string
	ext := PaneOpenExternal
	PaneOpenExternal = func(u string) error { mu.Lock(); opened = append(opened, u); mu.Unlock(); return nil }
	p := &webPane{St: &core.Store{}}
	old := Pane
	Pane = p
	defer func() {
		PaneOpenExternal, Pane = ext, old
		p.Mu.Lock()
		port := p.Port
		p.Mu.Unlock()
		if port > 0 {
			CloseDebugBrowser(port)
		}
	}()
	seen := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string{}, opened...) }
	at := func() string { return p.currentURL() }
	fresh := func() { // (the limit on how many addresses are handed over a minute is not what is tried next)
		p.Mu.Lock()
		p.handed, p.movedURL = nil, ""
		p.Mu.Unlock()
		mu.Lock()
		opened = nil
		mu.Unlock()
	}
	pages := func() []string { // the windows of the pane's browser, by address
		p.Mu.Lock()
		port := p.Port
		p.Mu.Unlock()
		ts, _ := cdpTargets(port)
		var out []string
		for _, x := range ts {
			if x.Type == "page" {
				out = append(out, x.URL)
			}
		}
		return out
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("%s: the pane is on %q, its browser's windows %q, the default browser was given %q", what, at(), pages(), seen())
	}
	click := func(c *cdpConn, id string) {
		t.Helper()
		if _, err := c.call("Runtime.evaluate", map[string]any{"expression": `document.getElementById("` + id + `").click()`, "userGesture": true}, 3*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	shop := srv.URL + "/shop"

	if err := p.Open(shop, "booth", false); err != nil {
		t.Fatal(err)
	}
	waitFor("the shop page", func() bool { return at() == shop })

	// cookies as an older version left them, next to another site's
	p.Mu.Lock()
	c, err := p.page()
	p.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	until := float64(time.Now().Add(time.Hour).Unix())
	for _, k := range []keptCookie{{Name: "cookie2", Value: "a", Domain: ".goofish.com", Path: "/", Expires: until, Secure: true},
		{Name: "unb", Value: "u", Domain: ".taobao.com", Path: "/", Expires: until, Secure: true},
		{Name: "tk", Value: "t", Domain: "login.taobao.com", Path: "/", Expires: until, Secure: true},
		{Name: "ALIPAYJSESSIONID", Value: "j", Domain: ".alipay.com", Path: "/", Expires: until, Secure: true},
		{Name: "_plaza_session", Value: "p", Domain: ".booth.pm", Path: "/", Expires: until, Secure: true},
		{Name: "BDUSS", Value: "b", Domain: ".baidu.com", Path: "/", Expires: until, Secure: true}} {
		if _, err := c.call("Network.setCookie", k.params(), 3*time.Second); err != nil {
			t.Fatalf("cookie %s: %v", k.Name, err)
		}
	}
	if n := forgetXianyu(c); n != 4 {
		t.Errorf("cookies removed: %d, want 4", n)
	}
	left, err := browserCookies(c)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, k := range left {
		names[k.Name] = true
		if xyHost(k.Domain) {
			t.Errorf("left in the pane's profile: %s of %s", k.Name, k.Domain)
		}
	}
	if !names["_plaza_session"] || !names["BDUSS"] {
		t.Errorf("another site's login went with them: %v", names)
	}

	// a link on a page of the pane leads to 闲鱼
	click(c, "x")
	waitFor("after a link to 闲鱼", func() bool { return len(seen()) == 1 && at() == shop })
	if got := seen()[0]; got != srv.URL+"/xy/item?id=1" {
		t.Errorf("handed to the default browser: %q", got)
	}
	st := p.State()
	if st.Moved != "127.0.0.1" || st.MovedAt == 0 || p.State().MovedAt != st.MovedAt {
		t.Errorf("told the page: %q at %d", st.Moved, st.MovedAt)
	}
	// 前进 does not lead there again: the page it was taken out of is passed over (and is still in the list)
	steps := func() (back, fwd bool, list []string) {
		t.Helper()
		h, err := p.history()
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range h.Entries {
			list = append(list, e.URL)
		}
		return h.step(-1) >= 0, h.step(1) >= 0, list
	}
	if _, fwd, list := steps(); fwd || len(list) < 2 || !XianyuURL(list[len(list)-1]) {
		t.Errorf("前进 offered towards 闲鱼, or the list is not what this is about: %q", list)
	}
	if _, err := p.Act("forward"); err != nil {
		t.Error(err)
	}
	time.Sleep(700 * time.Millisecond)
	if at() != shop || len(seen()) != 1 {
		t.Errorf("前进 went to %q, handed over %q", at(), seen())
	}

	// a pop-up a page of the pane opens on 闲鱼: closed, and handed over; another window is left alone
	fresh()
	click(c, "tab")
	waitFor("another window", func() bool { return len(pages()) == 2 })
	click(c, "pop")
	waitFor("after a pop-up on 闲鱼", func() bool {
		ps := pages()
		return len(seen()) == 1 && len(ps) == 2 && !XianyuURL(ps[0]) && !XianyuURL(ps[1])
	})
	if got := seen(); got[0] != srv.URL+"/xy/pop?t=1" || at() != shop {
		t.Errorf("after the pop-up: pane on %q, handed over %q", at(), got)
	}

	// a page of theirs that cannot be loaded: its address is not on the error page, but it is seen all the same
	fresh()
	if _, err := c.call("Page.navigate", map[string]any{"url": srv.URL + "/xy/dead"}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	waitFor("after a page of theirs that fails to load", func() bool { return len(seen()) == 1 && at() == shop })
	if got := seen(); got[0] != srv.URL+"/xy/dead" {
		t.Errorf("after the failed page: %q", got)
	}
	if _, fwd, list := steps(); fwd || XianyuURL(at()) {
		t.Errorf("after the failed page: on %q, 前进 offered, list %q", at(), list)
	}

	// a page that sends on there while it loads
	fresh()
	if err := p.Open(srv.URL+"/hop", "booth", false); err != nil {
		t.Fatal(err)
	}
	waitFor("after a page that redirects to 闲鱼", func() bool { return len(seen()) == 1 && at() == shop })
	if got := seen(); got[0] != srv.URL+"/xy/pay" {
		t.Errorf("after the redirect: opened %q", got)
	}
	// a page that sends there every time it is shown: the pane does not go back to it for ever, and the default
	// browser is not given the address again and again
	fresh()
	if err := p.Open(srv.URL+"/loop", "booth", false); err != nil {
		t.Fatal(err)
	}
	waitFor("after a page that keeps redirecting", func() bool { return len(seen()) >= 1 && at() == "about:blank" })
	time.Sleep(2 * time.Second)
	if got := seen(); len(got) != 1 || got[0] != srv.URL+"/xy/again" || at() != "about:blank" {
		t.Errorf("after the loop: on %q, opened %q", at(), got)
	}
	// 后退 from the empty page goes past their page too
	if back, _, list := steps(); !back {
		t.Errorf("no way back from the empty page: %q", list)
	}
	if _, err := p.Act("back"); err != nil {
		t.Error(err)
	}
	waitFor("后退 from the empty page", func() bool { return at() != "about:blank" })
	if XianyuURL(at()) || len(seen()) != 1 {
		t.Errorf("后退 led to %q, handed over %q", at(), seen())
	}
	// not more than three a minute, whatever the addresses
	if err := p.Open(shop, "booth", false); err != nil {
		t.Fatal(err)
	}
	waitFor("the shop page again", func() bool { return at() == shop })
	fresh()
	for i := 0; i < 5; i++ {
		p.handOver(srv.URL + "/xy/n" + itoa(i+1))
	}
	if got := seen(); len(got) != 3 {
		t.Errorf("handed over in a row: %q", got)
	}
	if p.handOver("tbopen://www.taobao.com/x") || p.handOver("not an address") {
		t.Error("something that is no web address was handed to the system")
	}
	// and it cannot be sent there directly
	if err := p.Open(srv.URL+"/xy/home", "booth", false); !errors.Is(err, errXyInPane) {
		t.Errorf("opened in the pane: %v", err)
	}
}
