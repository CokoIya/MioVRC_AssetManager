package purchases

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/webpane"
)

//go:embed booth_scraper.js
var boothScraperJS string

var (
	TaskPurchase   = &core.Task{Name: "purchase", Label: "同步 Booth 已购"}
	PurchaseCancel atomic.Bool
)

// StartPurchaseSync opens the Booth window and syncs in the background.
func StartPurchaseSync(st *core.Store) bool {
	if !core.PurchaseBusy.CompareAndSwap(false, true) {
		return false
	}
	PurchaseCancel.Store(false)
	go func() {
		defer core.PurchaseBusy.Store(false)
		var ok bool
		core.RunTask(TaskPurchase, func() { ok = RunPurchaseSync(st, TaskPurchase) })
		if ok || len(LoadBoothSession()) > 0 {
			ResumeDownloads(st) // downloads that waited for the login
		}
		if ok {
			st.Mu.RLock()
			auto := st.Settings.AutoBooth
			st.Mu.RUnlock()
			if auto {
				library.StartPipeline(st, false, false, true, false, nil)
			} else {
				library.KickTranslate(st)
			}
		}
	}()
	return true
}

type scrapeItem struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Shop      string   `json:"shop"`
	ShopURL   string   `json:"shopUrl"`
	Thumb     string   `json:"thumb"`
	Files     []string `json:"files"`
	Downloads []string `json:"downloads"`
	Gift      bool     `json:"gift"`
}

type scrapeOrder struct {
	ID    string   `json:"id"`
	Date  string   `json:"date"`
	Items []string `json:"items"`
}

type scrapeResult struct {
	Library    []scrapeItem  `json:"library"`
	Gifts      []scrapeItem  `json:"gifts"`
	Orders     []scrapeOrder `json:"orders"`
	OrderError string        `json:"orderError"`
	Incomplete string        `json:"incomplete"` // why the lists may not be whole (a page that did not load, fewer pages than Booth shows)
	Debug      string        `json:"debug"`
}

type scrapeProgress struct {
	Stage       string `json:"stage"`
	Page        int    `json:"page"`
	Items       int    `json:"items"`
	Orders      int    `json:"orders"`
	OrdersTotal int    `json:"ordersTotal"`
	OrderDone   int    `json:"orderDone"`
	Done        bool   `json:"done"`
	Error       string `json:"error"`
}

// RunPurchaseSync: open a browser window on a dedicated profile, wait for the user to be logged in
// to Booth, then read their library from inside that page. Returns true when purchases were saved.
func RunPurchaseSync(st *core.Store, prog *core.Task) bool { return runBoothWindow(st, prog, false) }

// EnsureBoothLogin refreshes the saved Booth login without showing a window: the browser profile
// is still logged in most of the time. False when the player has to log in again.
func EnsureBoothLogin(st *core.Store) bool {
	if !core.PurchaseBusy.CompareAndSwap(false, true) {
		return false
	}
	defer core.PurchaseBusy.Store(false)
	return runBoothWindow(st, &core.Task{Name: "login"}, true)
}

// saveSessionFrom keeps the Booth login of the window, for downloads.
func saveSessionFrom(d webpane.PageDriver) {
	cs, err := d.Cookies(webpane.BoothCookieURLs())
	if err != nil || len(cs) == 0 {
		core.Logf("没读到 Booth 登录信息：%v", err)
		return
	}
	if err := saveBoothSession(cs); err != nil {
		core.Logf("Booth 登录信息保存失败：%v", err)
	}
}

// runBoothWindow: quiet = only take the login, in a hidden window, and give up when it is not logged in.
func runBoothWindow(st *core.Store, prog *core.Task, quiet bool) bool {
	if webpane.PaneMode() != "" {
		return runBoothPane(st, prog, quiet) // inside the program
	}
	browser, note := webpane.SyncBrowser()
	if browser == "" {
		prog.Set(0, 0, "未找到可用的浏览器（Chrome、Edge 或 Firefox）")
		return false
	}
	if note != "" {
		core.Logf("%s", note)
	}
	base := core.BoothAccountsBase()
	bu, _ := url.Parse(base)
	start := base + "/library"
	if !quiet {
		prog.Set(0, 0, "正在打开 "+webpane.BrowserLabel(browser)+" 窗口…")
	}
	var d webpane.PageDriver
	var err error
	if webpane.IsFirefoxExe(browser) {
		var extra []string
		if quiet {
			extra = append(extra, "--headless")
		}
		d, err = webpane.StartFirefoxDriver(browser, filepath.Join(core.DataDir, "booth-profile-firefox"), start, extra)
	} else {
		var extra []string
		if quiet || os.Getenv("VRCLIB_HEADLESS") == "1" { // (the variable: tests only)
			extra = append(extra, "--headless=new")
		}
		if os.Getenv("VRCLIB_HEADLESS") == "1" {
			extra = append(extra, "--no-sandbox")
		}
		d, err = webpane.StartChromiumDriver(browser, core.BoothProfileDir(), start, bu.Host, extra)
	}
	if err != nil {
		core.Logf("Booth 窗口启动失败: %v", err)
		prog.Set(0, 0, "无法打开 Booth 窗口："+err.Error())
		return false
	}
	closeWin := true
	defer func() {
		d.Close()
		if closeWin {
			d.CloseBrowser()
		}
	}()
	return boothLoop(st, prog, d, quiet, "请在弹出的 "+webpane.BrowserLabel(browser)+" 窗口中登录 Booth，登录后自动同步已购", &closeWin)
}

// boothLoop waits until the page is a logged-in Booth library page, then reads the purchases (quiet:
// only keeps the login). closeWin is cleared when the player closed the window.
func boothLoop(st *core.Store, prog *core.Task, d webpane.PageDriver, quiet bool, waitingMsg string, closeWin *bool) bool {
	base := core.BoothAccountsBase()
	bu, _ := url.Parse(base)
	start := base + "/library"
	deadline := time.Now().Add(20 * time.Minute)
	if quiet {
		deadline = time.Now().Add(25 * time.Second)
	}
	var lastNav, signInSince time.Time
	left, _ := d.(interface{ UserLeft() bool })
	prog.Set(0, 0, waitingMsg)
	var res *scrapeResult
	for res == nil {
		if PurchaseCancel.Load() {
			prog.Set(0, 0, "已取消")
			return false
		}
		if time.Now().After(deadline) {
			prog.Set(0, 0, "等待超时，同步已取消")
			return false
		}
		href, err := webpane.EvalString(d, "location.href", 6*time.Second)
		if err != nil {
			if d.Dead() || errors.Is(err, webpane.ErrCDPClosed) {
				*closeWin = false
				prog.Set(0, 0, "Booth 窗口已关闭，同步已取消")
				return false
			}
			// the tab may have been replaced (login redirects can open a new one)
			if rerr := d.Reconnect(); rerr != nil && d.Dead() {
				*closeWin = false
				prog.Set(0, 0, "Booth 窗口已关闭，同步已取消")
				return false
			}
			time.Sleep(1500 * time.Millisecond)
			continue
		}
		u, _ := url.Parse(href)
		if u == nil {
			time.Sleep(time.Second)
			continue
		}
		onAccounts := u.Host == bu.Host
		if quiet && strings.Contains(u.Path, "sign_in") {
			if signInSince.IsZero() {
				signInSince = time.Now()
			} else if time.Since(signInSince) > 6*time.Second {
				return false // not logged in: the player has to do it in a visible window
			}
		}
		if !onAccounts || strings.Contains(u.Path, "sign_in") {
			if !quiet && left != nil && left.UserLeft() {
				prog.Set(0, 0, "未登录，同步已取消")
				return false
			}
			// back on booth.pm after logging in → go to the library page ourselves
			if strings.HasSuffix(u.Host, "booth.pm") && !onAccounts && time.Since(lastNav) > 10*time.Second {
				lastNav = time.Now()
				_ = d.Navigate(start)
			}
			prog.Set(0, 0, waitingMsg)
			time.Sleep(1500 * time.Millisecond)
			continue
		}
		// logged-in accounts page: run the reader
		if quiet {
			saveSessionFrom(d)
			core.Logf("已确认 Booth 登录")
			return true
		}
		r, err := runScraper(d, prog)
		if err != nil {
			if err.Error() == "LOGIN" {
				prog.Set(0, 0, waitingMsg)
				_ = d.Navigate(start)
				time.Sleep(2 * time.Second)
				continue
			}
			if err.Error() == "已取消" {
				prog.Set(0, 0, "已取消")
				return false
			}
			msg := err.Error()
			if d.Dead() || strings.Contains(msg, "context") || strings.Contains(msg, "frame") || strings.Contains(msg, "navigat") || strings.Contains(msg, "页面已刷新") {
				time.Sleep(1500 * time.Millisecond)
				continue // page navigated while reading; start over
			}
			core.Logf("Booth 已购读取失败: %v", err)
			prog.Set(0, 0, "同步失败："+msg)
			return false
		}
		res = r
	}
	saveSessionFrom(d)

	n, partial := mergePurchases(st, res)
	if n == 0 {
		if res.Debug != "" {
			_ = os.WriteFile(filepath.Join(core.DataDir, "booth-debug.html"), []byte(redactPage(res.Debug)), 0600)
		}
		prog.Set(1, 1, "未获取到已购商品（如确有已购，请将数据文件夹中的 booth-debug.html 发送给作者）")
		return false
	}
	_ = os.Remove(filepath.Join(core.DataDir, "booth-debug.html"))
	_ = st.Save()
	core.BumpRev()
	downloadPurchaseThumbs(st, prog)
	_ = st.Save()
	core.BumpRev()
	msg := fmt.Sprintf("完成：已同步 %d 件 Booth 已购", n)
	if partial {
		msg += "（" + syncPartialNote + "）"
	}
	if res.OrderError != "" {
		msg += "（订单页未完整获取：" + res.OrderError + "）"
	}
	prog.Set(1, 1, msg)
	core.Logf("Booth 已购同步完成：%d 件，%d 个订单，未读全：%v %s", n, len(res.Orders), partial, res.Incomplete)
	return true
}

// syncPartialNote: said in the sync's status when what was read cannot be all of it.
const syncPartialNote = "同步未完整，已保留原有记录"

var (
	rePageToken = regexp.MustCompile(`(?i)((?:csrf-token|csrf-param|authenticity_token)"[^>]{0,80}?(?:content|value)=")[^"]*`)
	rePageMail  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// redactPage: the library page kept for the author to look at, without the form tokens and e-mail addresses
// in it.
func redactPage(html string) string {
	html = rePageToken.ReplaceAllString(html, "${1}…")
	return rePageMail.ReplaceAllString(html, "…@…")
}

func runScraper(d webpane.PageDriver, prog *core.Task) (*scrapeResult, error) {
	if _, err := d.Eval(boothScraperJS+"\n;window.__vrclibRun(); 'started'", 10*time.Second); err != nil {
		return nil, err
	}
	for {
		time.Sleep(900 * time.Millisecond)
		if PurchaseCancel.Load() {
			return nil, errors.New("已取消")
		}
		raw, err := webpane.EvalString(d, "JSON.stringify(window.__vrclib || null)", 8*time.Second)
		if err != nil {
			return nil, err
		}
		if raw == "" || raw == "null" {
			return nil, errors.New("页面已刷新")
		}
		var p scrapeProgress
		_ = json.Unmarshal([]byte(raw), &p)
		switch p.Stage {
		case "library":
			prog.Set(0, 0, fmt.Sprintf("正在获取已购列表，第 %d 页（%d 件）", p.Page, p.Items))
		case "gifts":
			prog.Set(0, 0, fmt.Sprintf("正在获取礼物列表，第 %d 页（%d 件）", p.Page, p.Items))
		case "orders":
			prog.Set(0, 0, fmt.Sprintf("正在获取订单列表，第 %d 页（%d 个订单）", p.Page, p.Orders))
		case "orderDetail":
			prog.Set(p.OrderDone, p.OrdersTotal, "正在获取订单详情")
		}
		if !p.Done {
			continue
		}
		if p.Error != "" {
			return nil, errors.New(p.Error)
		}
		out, err := webpane.EvalString(d, "window.__vrclibResult || ''", 60*time.Second)
		if err != nil {
			return nil, err
		}
		var r scrapeResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			return nil, fmt.Errorf("无法解析同步结果：%v", err)
		}
		return &r, nil
	}
}

// lostTooMany: a Booth library does not shrink by itself, so a list that is much shorter than the one kept is
// taken for a sync that did not read everything.
func lostTooMany(lost, had int) bool { return lost >= 3 && lost*5 > had }

// mergePurchases puts what a sync read into the store. partial: the lists were not read whole (or shrank more
// than a library does), so the purchases kept before were not dropped.
func mergePurchases(st *core.Store, r *scrapeResult) (n int, partial bool) {
	now := time.Now().Unix()
	st.Mu.Lock()
	defer st.Mu.Unlock()
	old := st.Purchases
	next := map[string]*core.Purchase{}
	add := func(it scrapeItem) {
		if it.ID == "" {
			return
		}
		p := next[it.ID]
		if p == nil {
			p = &core.Purchase{ID: it.ID, Gift: it.Gift, First: now}
			if o := old[it.ID]; o != nil {
				p.First, p.Cover = o.First, o.Cover
			}
			next[it.ID] = p
		}
		if !it.Gift {
			p.Gift = false
		}
		if p.Name == "" {
			p.Name = it.Name
		}
		if p.Shop == "" {
			p.Shop, p.ShopURL = it.Shop, it.ShopURL
		}
		if p.Thumb == "" {
			p.Thumb = it.Thumb
		}
		for i, d := range it.Downloads {
			if !core.ContainsStr(p.Downloads, d) {
				p.Downloads = append(p.Downloads, d)
				f := ""
				if i < len(it.Files) {
					f = it.Files[i]
				}
				p.Files = append(p.Files, f)
			}
		}
	}
	for _, it := range r.Library {
		add(it)
	}
	for _, it := range r.Gifts {
		add(it)
	}
	if len(next) == 0 {
		return 0, false
	}
	for _, o := range r.Orders {
		for _, id := range o.Items {
			if p := next[id]; p != nil && !containsOrder(p.Orders, o.ID) {
				p.Orders = append(p.Orders, core.PurchaseOrder{ID: o.ID, Date: o.Date})
			}
		}
	}
	// orders known before stay when this sync did not read them (the order pages failed, or were not reached)
	for id, p := range next {
		if o := old[id]; o != nil {
			for _, po := range o.Orders {
				if !containsOrder(p.Orders, po.ID) {
					p.Orders = append(p.Orders, po)
				}
			}
		}
		sort.SliceStable(p.Orders, func(i, j int) bool { return p.Orders[i].Date > p.Orders[j].Date })
	}
	n = len(next)
	had, lost := 0, 0
	for id := range old {
		if !core.IsGumID(id) {
			had++
			if next[id] == nil {
				lost++
			}
		}
	}
	partial = r.Incomplete != "" || lostTooMany(lost, had)
	for id, p := range old {
		if core.IsGumID(id) { // what was bought on Gumroad is not Booth's to forget
			next[id] = p
		} else if partial && next[id] == nil {
			next[id] = p
			n++
		}
	}
	st.Purchases = next
	st.PurchaseSync = now
	library.ApplyPurchases(st)
	return n, partial
}

func containsOrder(l []core.PurchaseOrder, id string) bool {
	for _, o := range l {
		if o.ID == id {
			return true
		}
	}
	return false
}

func downloadPurchaseThumbs(st *core.Store, prog *core.Task) {
	st.Mu.RLock()
	var todo []*core.Purchase
	for _, p := range st.Purchases {
		if p.Thumb != "" && (p.Cover == "" || !core.FileExists(p.Cover)) {
			todo = append(todo, p)
		}
	}
	st.Mu.RUnlock()
	if len(todo) == 0 {
		return
	}
	c := core.HTTPClient(st)
	fails := 0
	for i, p := range todo {
		prog.Set(i, len(todo), "正在下载已购商品缩略图")
		path, err := booth.DownloadTo(c, p.Thumb, "purchase_"+p.ID)
		if err != nil {
			fails++
			if fails >= 5 && fails == i+1 {
				break // network clearly unreachable; Booth covers come later via the normal fetch
			}
			continue
		}
		st.Mu.Lock()
		p.Cover = path
		st.Mu.Unlock()
		time.Sleep(120 * time.Millisecond)
	}
}

// ---------- matching purchases to local assets ----------

// ForgetBoothLogin logs out of Booth: the saved login, and the Booth cookies of the pane's profile
// (or the whole old browser profile when there is no pane running).
func ForgetBoothLogin() error {
	if webpane.PaneMode() != "" {
		// the pane also holds the 闲鱼 login: only Booth's (and pixiv's, which logs Booth back in) go
		forgetBoothSession()
		webpane.Pane.Mu.Lock()
		running := webpane.Pane.Port > 0
		webpane.Pane.Mu.Unlock()
		if !running && webpane.PaneMode() == "native" {
			if err := webpane.Pane.Open("about:blank", "", false); err != nil {
				return err
			}
			running = true
		}
		if running {
			_ = os.RemoveAll(filepath.Join(core.DataDir, "booth-profile-firefox"))
			return webpane.Pane.ForgetSites([]string{"booth.pm", "pixiv.net"})
		}
	}
	if port, _ := webpane.ReadDevToolsPort(core.BoothProfileDir()); port > 0 {
		webpane.CloseDebugBrowser(port)
		time.Sleep(800 * time.Millisecond)
	}
	forgetBoothSession()
	err := os.RemoveAll(core.BoothProfileDir())
	if err2 := os.RemoveAll(filepath.Join(core.DataDir, "booth-profile-firefox")); err == nil {
		err = err2
	}
	return err
}
