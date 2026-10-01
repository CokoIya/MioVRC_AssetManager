package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

//go:embed booth_scraper.js
var boothScraperJS string

type PurchaseOrder struct {
	ID   string `json:"id"`
	Date string `json:"date,omitempty"`
}

type Purchase struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Shop      string          `json:"shop,omitempty"`
	ShopURL   string          `json:"shopUrl,omitempty"`
	Thumb     string          `json:"thumb,omitempty"`
	Cover     string          `json:"cover,omitempty"` // local copy of the thumbnail
	Files     []string        `json:"files,omitempty"`
	Downloads []string        `json:"downloads,omitempty"`
	Gift      bool            `json:"gift,omitempty"`
	Orders    []PurchaseOrder `json:"orders,omitempty"`
	First     int64           `json:"first"`
}

func boothAccountsBase() string {
	if v := os.Getenv("VRCLIB_BOOTH_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://accounts.booth.pm"
}

func orderURL(id string) string { return boothAccountsBase() + "/orders/" + id }
func libraryURL(gift bool) string {
	if gift {
		return boothAccountsBase() + "/library/gifts"
	}
	return boothAccountsBase() + "/library"
}

// When the purchase was made (latest order), falling back to when it was first synced.
func (p *Purchase) when() int64 {
	for _, o := range p.Orders {
		if t, err := time.ParseInLocation("2006-01-02", o.Date, time.Local); err == nil {
			return t.Unix()
		}
	}
	return p.First
}

var (
	taskPurchase   = &Task{Name: "purchase", Label: "同步 Booth 已购"}
	purchaseBusy   atomic.Bool
	purchaseCancel atomic.Bool
)

// StartPurchaseSync opens the Booth window and syncs in the background.
func StartPurchaseSync(st *Store) bool {
	if !purchaseBusy.CompareAndSwap(false, true) {
		return false
	}
	purchaseCancel.Store(false)
	go func() {
		defer purchaseBusy.Store(false)
		var ok bool
		run(taskPurchase, func() { ok = RunPurchaseSync(st, taskPurchase) })
		if ok {
			st.mu.RLock()
			auto := st.Settings.AutoBooth
			st.mu.RUnlock()
			if auto {
				StartPipeline(st, false, false, true, false, nil)
			} else {
				KickTranslate(st)
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
func RunPurchaseSync(st *Store, prog *Task) bool {
	browser, note := syncBrowser()
	if browser == "" {
		prog.Set(0, 0, "没找到可用的浏览器（Chrome、Edge 或 Firefox）")
		return false
	}
	if note != "" {
		logf("%s", note)
	}
	base := boothAccountsBase()
	bu, _ := url.Parse(base)
	start := base + "/library"
	prog.Set(0, 0, "正在打开 "+browserLabel(browser)+" 窗口…")
	var d pageDriver
	var err error
	if isFirefoxExe(browser) {
		d, err = startFirefoxDriver(browser, filepath.Join(dataDir, "booth-profile-firefox"), start)
	} else {
		var extra []string
		if os.Getenv("VRCLIB_HEADLESS") == "1" { // tests only
			extra = append(extra, "--headless=new", "--no-sandbox")
		}
		d, err = startChromiumDriver(browser, boothProfileDir(), start, bu.Host, extra)
	}
	if err != nil {
		logf("Booth 窗口启动失败: %v", err)
		prog.Set(0, 0, "Booth 窗口打不开："+err.Error())
		return false
	}
	closeWin := true
	defer func() {
		d.Close()
		if closeWin {
			d.CloseBrowser()
		}
	}()

	deadline := time.Now().Add(20 * time.Minute)
	var lastNav time.Time
	waitingMsg := "请在弹出的 " + browserLabel(browser) + " 窗口里登录 Booth，登录后会自动开始读取"
	prog.Set(0, 0, waitingMsg)
	var res *scrapeResult
	for res == nil {
		if purchaseCancel.Load() {
			prog.Set(0, 0, "已取消")
			return false
		}
		if time.Now().After(deadline) {
			prog.Set(0, 0, "等待超时，已取消")
			return false
		}
		href, err := evalString(d, "location.href", 6*time.Second)
		if err != nil {
			if d.Dead() || errors.Is(err, errCDPClosed) {
				closeWin = false
				prog.Set(0, 0, "Booth 窗口被关掉了，同步已取消")
				return false
			}
			// the tab may have been replaced (login redirects can open a new one)
			if rerr := d.Reconnect(); rerr != nil && d.Dead() {
				closeWin = false
				prog.Set(0, 0, "Booth 窗口被关掉了，同步已取消")
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
		if !onAccounts || strings.Contains(u.Path, "sign_in") {
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
			logf("Booth 已购读取失败: %v", err)
			prog.Set(0, 0, "读取失败："+msg)
			return false
		}
		res = r
	}

	n := mergePurchases(st, res)
	if n == 0 {
		if res.Debug != "" {
			_ = os.WriteFile(filepath.Join(dataDir, "booth-debug.html"), []byte(res.Debug), 0644)
		}
		prog.Set(1, 1, "没有读到已购商品（如果买过，请把数据文件夹里的 booth-debug.html 发给作者）")
		return false
	}
	_ = os.Remove(filepath.Join(dataDir, "booth-debug.html"))
	_ = st.Save()
	bumpRev()
	downloadPurchaseThumbs(st, prog)
	_ = st.Save()
	bumpRev()
	msg := fmt.Sprintf("完成：%d 件已购", n)
	if res.OrderError != "" {
		msg += "（订单页没读全：" + res.OrderError + "）"
	}
	prog.Set(1, 1, msg)
	logf("Booth 已购同步完成：%d 件，%d 个订单", n, len(res.Orders))
	return true
}

func runScraper(d pageDriver, prog *Task) (*scrapeResult, error) {
	if _, err := d.Eval(boothScraperJS+"\n;window.__vrclibRun(); 'started'", 10*time.Second); err != nil {
		return nil, err
	}
	for {
		time.Sleep(900 * time.Millisecond)
		if purchaseCancel.Load() {
			return nil, errors.New("已取消")
		}
		raw, err := evalString(d, "JSON.stringify(window.__vrclib || null)", 8*time.Second)
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
			prog.Set(0, 0, fmt.Sprintf("读取已购 第 %d 页（%d 件）", p.Page, p.Items))
		case "gifts":
			prog.Set(0, 0, fmt.Sprintf("读取收到的礼物 第 %d 页（%d 件）", p.Page, p.Items))
		case "orders":
			prog.Set(0, 0, fmt.Sprintf("读取订单列表 第 %d 页（%d 个订单）", p.Page, p.Orders))
		case "orderDetail":
			prog.Set(p.OrderDone, p.OrdersTotal, "读取订单详情")
		}
		if !p.Done {
			continue
		}
		if p.Error != "" {
			return nil, errors.New(p.Error)
		}
		out, err := evalString(d, "window.__vrclibResult || ''", 60*time.Second)
		if err != nil {
			return nil, err
		}
		var r scrapeResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			return nil, fmt.Errorf("结果解析失败：%v", err)
		}
		return &r, nil
	}
}

func mergePurchases(st *Store, r *scrapeResult) int {
	now := time.Now().Unix()
	st.mu.Lock()
	defer st.mu.Unlock()
	old := st.Purchases
	next := map[string]*Purchase{}
	add := func(it scrapeItem) {
		if it.ID == "" {
			return
		}
		p := next[it.ID]
		if p == nil {
			p = &Purchase{ID: it.ID, Gift: it.Gift, First: now}
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
			if !containsStr(p.Downloads, d) {
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
		return 0
	}
	for _, o := range r.Orders {
		for _, id := range o.Items {
			if p := next[id]; p != nil && !containsOrder(p.Orders, o.ID) {
				p.Orders = append(p.Orders, PurchaseOrder{ID: o.ID, Date: o.Date})
			}
		}
	}
	for _, p := range next {
		sort.SliceStable(p.Orders, func(i, j int) bool { return p.Orders[i].Date > p.Orders[j].Date })
	}
	st.Purchases = next
	st.PurchaseSync = now
	applyPurchases(st)
	return len(next)
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func containsOrder(l []PurchaseOrder, id string) bool {
	for _, o := range l {
		if o.ID == id {
			return true
		}
	}
	return false
}

func downloadPurchaseThumbs(st *Store, prog *Task) {
	st.mu.RLock()
	var todo []*Purchase
	for _, p := range st.Purchases {
		if p.Thumb != "" && (p.Cover == "" || !fileExists(p.Cover)) {
			todo = append(todo, p)
		}
	}
	st.mu.RUnlock()
	if len(todo) == 0 {
		return
	}
	c := httpClient(st)
	fails := 0
	for i, p := range todo {
		prog.Set(i, len(todo), "下载已购商品缩略图")
		path, err := downloadTo(c, p.Thumb, "purchase_"+p.ID)
		if err != nil {
			fails++
			if fails >= 5 && fails == i+1 {
				break // network clearly unreachable; Booth covers come later via the normal fetch
			}
			continue
		}
		st.mu.Lock()
		p.Cover = path
		st.mu.Unlock()
		time.Sleep(120 * time.Millisecond)
	}
}

// ---------- matching purchases to local assets ----------

// fileMatchKey turns a download / folder / archive name into a comparable key ("" when too generic).
func fileMatchKey(name string) string {
	base := stripArchiveExt(filepath.Base(strings.TrimSpace(name)))
	if ext := filepath.Ext(base); ext != "" && len(ext) <= 6 && !strings.ContainsAny(ext[1:], "0123456789") {
		base = strings.TrimSuffix(base, ext)
	}
	k := normKey(base)
	if len([]rune(k)) < 5 || isGenericName(base) || isStructuralName(base, "") {
		return ""
	}
	return k
}

func purchaseFileIndex(ps map[string]*Purchase) map[string]string {
	idx := map[string]string{}
	dup := map[string]bool{}
	for id, p := range ps {
		for _, f := range p.Files {
			k := fileMatchKey(f)
			if k == "" {
				continue
			}
			if old, ok := idx[k]; ok && old != id {
				dup[k] = true
			}
			idx[k] = id
		}
	}
	for k := range dup {
		delete(idx, k)
	}
	return idx
}

// applyPurchases links local assets without a name-given Booth id to a purchase whose download
// file has the same name. Caller holds st.mu (write).
func applyPurchases(st *Store) {
	if len(st.Purchases) == 0 {
		return
	}
	idx := purchaseFileIndex(st.Purchases)
	for _, a := range st.Assets {
		if a.BoothID != "" && !a.BoothFromURL && !a.BoothFromLib {
			continue
		}
		if id := matchAssetPurchase(a, idx, st.Purchases); id != "" {
			a.BoothID, a.BoothFromURL, a.BoothFromLib = id, false, true
		}
	}
}

func matchAssetPurchase(a *Asset, idx map[string]string, ps map[string]*Purchase) string {
	for _, l := range a.Locations {
		if id, ok := idx[fileMatchKey(l.Path)]; ok {
			return id
		}
	}
	// packages inside the folder may be bundled dependencies; accept them only when the names agree
	for _, list := range [][]string{a.Packages, a.Archives} {
		for _, f := range list {
			id, ok := idx[fileMatchKey(f)]
			if !ok || depBoothIDs[id] {
				continue
			}
			if p := ps[id]; p != nil && namesRelated(a.Name+" "+a.RawName, p.Name+" "+filepath.Base(f)) {
				return id
			}
		}
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func boothProfileDir() string { return filepath.Join(dataDir, "booth-profile") }

// ForgetBoothLogin removes the dedicated browser profile (the Booth login lives only there).
func ForgetBoothLogin() error {
	if port, _ := readDevToolsPort(boothProfileDir()); port > 0 {
		closeDebugBrowser(port)
		time.Sleep(800 * time.Millisecond)
	}
	err := os.RemoveAll(boothProfileDir())
	if err2 := os.RemoveAll(filepath.Join(dataDir, "booth-profile-firefox")); err == nil {
		err = err2
	}
	return err
}
