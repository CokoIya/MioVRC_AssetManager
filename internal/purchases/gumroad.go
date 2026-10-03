package purchases

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/webpane"
)

func gumroadLoginURL() string { return core.GumroadBase() + "/login?next=%2Flibrary" }

var errGumLogin = errors.New("Gumroad 尚未登录或登录已失效")

// errGumBlocked: the site's protection (Cloudflare) answered the program with a "prove you are a browser" page.
var errGumBlocked = errors.New("Gumroad 的防护页（Cloudflare）拦截了请求，暂时无法获取已购，请稍后重试；如持续出现，可通过「反馈与建议」告知作者")

// gumChallenged: the answer is the protection's challenge page, not Gumroad's own.
func gumChallenged(resp *http.Response, body []byte) bool {
	if resp.StatusCode != 403 && resp.StatusCode != 503 && resp.StatusCode != 429 {
		return false
	}
	if resp.Header.Get("Cf-Mitigated") != "" {
		return true
	}
	b := string(body)
	return strings.Contains(b, "challenge-platform") || strings.Contains(b, "Just a moment") || strings.Contains(b, "cf-chl")
}

// ---------- the saved login ----------

type gumSession struct {
	Cookies []core.SavedCookie `json:"cookies"`
	At      int64              `json:"at"`
	Name    string             `json:"name,omitempty"`
}

var gumMu sync.Mutex

func gumSessionFile() string { return filepath.Join(core.DataDir, "gumroad-session.dat") }

func saveGumSession(s *gumSession) error {
	b, _ := json.Marshal(s)
	enc, err := core.ProtectData(b)
	if err != nil {
		return err
	}
	gumMu.Lock()
	defer gumMu.Unlock()
	return os.WriteFile(gumSessionFile(), enc, 0600)
}

func LoadGumSession() *gumSession {
	gumMu.Lock()
	b, err := os.ReadFile(gumSessionFile())
	gumMu.Unlock()
	if err != nil {
		return nil
	}
	dec, err := core.UnprotectData(b)
	if err != nil {
		return nil
	}
	var s gumSession
	if json.Unmarshal(dec, &s) != nil || len(s.Cookies) == 0 {
		return nil
	}
	return &s
}

func forgetGumSession() {
	gumMu.Lock()
	_ = os.Remove(gumSessionFile())
	gumMu.Unlock()
}

// ---------- talking to gumroad.com ----------

type gumClient struct {
	s  *gumSession
	hc *http.Client
}

func newGumClient(st *core.Store, s *gumSession) *gumClient {
	c := core.HTTPClient(st)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &gumClient{s: s, hc: c}
}

func (g *gumClient) cookieHeader() string {
	var parts []string
	for _, c := range g.s.Cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// absorb keeps a cookie the site renewed.
func (g *gumClient) absorb(resp *http.Response) {
	changed := false
	for _, n := range resp.Cookies() {
		for i := range g.s.Cookies {
			if g.s.Cookies[i].Name == n.Name && n.Value != "" && g.s.Cookies[i].Value != n.Value {
				g.s.Cookies[i].Value = n.Value
				if !n.Expires.IsZero() {
					g.s.Cookies[i].Expires = n.Expires.Unix()
				}
				changed = true
			}
		}
	}
	if changed {
		_ = saveGumSession(g.s)
	}
}

func (g *gumClient) get(u string, inertia bool) (*http.Response, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Accept-Language", "en,zh-CN;q=0.8")
	req.Header.Set("Cookie", g.cookieHeader())
	if inertia {
		req.Header.Set("X-Inertia", "true")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Accept", "text/html, application/xhtml+xml")
	}
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 Gumroad（%s）", core.FriendlyNetErr(err))
	}
	g.absorb(resp)
	return resp, nil
}

// sameSite: an address of the Gumroad the login is for (the key cookies never go anywhere else).
func gumSameSite(u *url.URL) bool {
	b, err := url.Parse(core.GumroadBase())
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, b.Host) {
		return true
	}
	h, bh := strings.ToLower(u.Hostname()), strings.ToLower(b.Hostname())
	return b.Port() == "" && u.Port() == "" && strings.HasSuffix(h, "."+bh) // app.gumroad.com
}

var (
	reDataPageAttr   = regexp.MustCompile(`data-page="([^"]*)"`)
	reDataPageScript = regexp.MustCompile(`(?s)<script[^>]*data-page[^>]*>(.*?)</script>`)
)

// page reads an Inertia page of the site: its component name and its data.
func (g *gumClient) page(u string) (string, json.RawMessage, error) {
	for hop := 0; hop < 6; hop++ {
		pu, err := url.Parse(u)
		if err != nil || !gumSameSite(pu) {
			return "", nil, errors.New("Gumroad 页面被重定向到其他网站：" + u)
		}
		resp, err := g.get(u, true)
		if err != nil {
			return "", nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		next := ""
		switch {
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			if loc, err := resp.Location(); err == nil {
				next = loc.String()
			}
		case resp.StatusCode == 409 && resp.Header.Get("X-Inertia-Location") != "":
			next = resp.Header.Get("X-Inertia-Location")
			if nu, err := pu.Parse(next); err == nil {
				next = nu.String()
			}
		case gumChallenged(resp, body):
			return "", nil, errGumBlocked
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			return "", nil, errGumLogin
		case resp.StatusCode == 404:
			return "", nil, errors.New("Gumroad 上未找到该页面（404）")
		case resp.StatusCode == 429:
			return "", nil, errors.New("Gumroad 请求过于频繁，请稍后重试")
		case resp.StatusCode != 200:
			return "", nil, fmt.Errorf("Gumroad 返回错误（HTTP %d）", resp.StatusCode)
		}
		if next != "" {
			if nu, err := url.Parse(next); err == nil && strings.Contains(nu.Path, "/login") {
				return "", nil, errGumLogin
			}
			u = next
			continue
		}
		var p struct {
			Component string          `json:"component"`
			Props     json.RawMessage `json:"props"`
		}
		raw := body
		if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
			if m := reDataPageAttr.FindSubmatch(body); m != nil {
				raw = []byte(html.UnescapeString(string(m[1])))
			} else if m := reDataPageScript.FindSubmatch(body); m != nil {
				raw = m[1]
			} else {
				return "", nil, errors.New("无法解析 Gumroad 页面（网站可能已改版）")
			}
		}
		if json.Unmarshal(raw, &p) != nil || p.Component == "" {
			return "", nil, errors.New("无法解析 Gumroad 页面（网站可能已改版）")
		}
		if strings.HasPrefix(p.Component, "Logins/") || strings.HasPrefix(p.Component, "TwoFactorAuthentication/") {
			return "", nil, errGumLogin
		}
		return p.Component, p.Props, nil
	}
	return "", nil, errors.New("Gumroad 页面重定向次数过多")
}

type gumCard struct {
	Product struct {
		Name    string `json:"name"`
		Creator *struct {
			Name       string `json:"name"`
			ProfileURL string `json:"profile_url"`
		} `json:"creator"`
		ThumbnailURL string `json:"thumbnail_url"`
	} `json:"product"`
	Purchase struct {
		ID          string `json:"id"`
		IsArchived  bool   `json:"is_archived"`
		DownloadURL string `json:"download_url"`
		Variants    string `json:"variants"`
	} `json:"purchase"`
}

type gumLibraryProps struct {
	Results    []gumCard `json:"results"`
	Pagination struct {
		Page  int `json:"page"`
		Pages int `json:"pages"`
		Count int `json:"count"`
	} `json:"pagination"`
	LoggedInUser map[string]any `json:"logged_in_user"`
}

type gumItem struct {
	Type            string    `json:"type"`
	Name            string    `json:"name"` // a folder's
	FileName        string    `json:"file_name"`
	Extension       string    `json:"extension"`
	ID              string    `json:"id"`
	FileSize        int64     `json:"file_size"`
	DownloadURL     string    `json:"download_url"`
	ExternalLinkURL string    `json:"external_link_url"`
	Children        []gumItem `json:"children"`
}

type gumDownloadProps struct {
	Content struct {
		ContentItems []gumItem `json:"content_items"`
	} `json:"content"`
	Purchase *struct {
		ProductPermalink string `json:"product_permalink"`
		ProductName      string `json:"product_name"`
		ProductLongURL   string `json:"product_long_url"`
		CreatedAt        string `json:"created_at"`
	} `json:"purchase"`
}

// library reads one page of the library (15 purchases a page, newest purchase first).
func (g *gumClient) library(page int, archived bool) (*gumLibraryProps, error) {
	u := fmt.Sprintf("%s/library?sort=purchase_date&page=%d", core.GumroadBase(), page)
	if archived {
		u += "&show_archived_only=true"
	}
	comp, props, err := g.page(u)
	if err != nil {
		return nil, err
	}
	if comp != "Library/Index" {
		return nil, errors.New("Gumroad 返回的不是已购页面（" + comp + "）")
	}
	var p gumLibraryProps
	if err := json.Unmarshal(props, &p); err != nil {
		return nil, errors.New("无法解析 Gumroad 已购页面（网站可能已改版）")
	}
	return &p, nil
}

type gumFile struct {
	ID   string
	Name string
	Size int64
	Path string // "/r/<token>/product_files?product_file_ids[]=<id>"
}

// files reads a purchase's download page: what can be downloaded, and the product it is.
func (g *gumClient) files(downloadURL string) (files []gumFile, d *gumDownloadProps, note string, err error) {
	comp, props, err := g.page(downloadURL)
	if err != nil {
		return nil, nil, "", err
	}
	switch comp {
	case "UrlRedirects/DownloadPage":
	case "UrlRedirects/ConfirmPage":
		return nil, nil, "Gumroad 要求先确认购买邮箱，请在 Gumroad 已购页面中打开一次该商品", nil
	case "UrlRedirects/Expired", "UrlRedirects/RentalExpired":
		return nil, nil, "访问已过期", nil
	case "UrlRedirects/MembershipInactive":
		return nil, nil, "会员订阅已停止，无法下载", nil
	default:
		return nil, nil, "无法读取下载页（" + comp + "）", nil
	}
	d = &gumDownloadProps{}
	if err := json.Unmarshal(props, d); err != nil {
		return nil, nil, "", errors.New("无法解析 Gumroad 下载页面（网站可能已改版）")
	}
	seen := map[string]int{}
	var walk func(items []gumItem)
	walk = func(items []gumItem) {
		for _, it := range items {
			if it.Type == "folder" {
				walk(it.Children)
				continue
			}
			if it.ID == "" || it.DownloadURL == "" { // streamed only, or a link to another site
				continue
			}
			name := strings.TrimSpace(it.FileName)
			if ext := strings.ToLower(strings.TrimSpace(it.Extension)); ext != "" && !strings.HasSuffix(strings.ToLower(name), "."+ext) {
				name += "." + ext
			}
			if name == "" {
				name = it.ID
			}
			key := strings.ToLower(name)
			if n := seen[key]; n > 0 { // two files of one name in different folders
				e := filepath.Ext(name)
				name = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, e), n+1, e)
			}
			seen[key]++
			files = append(files, gumFile{ID: it.ID, Name: name, Size: it.FileSize, Path: it.DownloadURL})
		}
	}
	walk(d.Content.ContentItems)
	return files, d, "", nil
}

// ---------- logging in, in the built-in page ----------

type GumroadAccount struct {
	LoggedIn   bool   `json:"loggedIn"`
	Name       string `json:"name,omitempty"`
	Waiting    bool   `json:"waiting,omitempty"` // the login page is open, waiting for the player
	Count      int    `json:"count"`
	LastSync   int64  `json:"lastSync,omitempty"`
	Busy       bool   `json:"busy,omitempty"`
	LoginURL   string `json:"loginUrl"`
	LibraryURL string `json:"libraryUrl"`
}

var (
	gumWatching atomic.Bool
	GumBusy     atomic.Bool
	GumCancel   atomic.Bool
	TaskGumroad = &core.Task{Name: "gumroad", Label: "同步 Gumroad 已购"}
)

// CurrentGumroadAccount: caller holds st.mu (read).
func CurrentGumroadAccount(st *core.Store) GumroadAccount {
	a := GumroadAccount{Waiting: gumWatching.Load(), Busy: GumBusy.Load(), LoginURL: gumroadLoginURL(), LibraryURL: core.GumroadLibraryURL(), LastSync: st.GumroadSync}
	if s := LoadGumSession(); s != nil {
		a.LoggedIn, a.Name = true, s.Name
	}
	for id := range st.Purchases {
		if core.IsGumID(id) {
			a.Count++
		}
	}
	return a
}

func userName(m map[string]any) string {
	for _, k := range []string{"name", "username", "email"} {
		if s, _ := m[k].(string); strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// captureGumroadLogin takes the login from the built-in page (nil: not logged in there).
func captureGumroadLogin(st *core.Store) (*gumSession, error) {
	cs, err := webpane.Pane.Cookies([]string{core.GumroadBase() + "/"})
	if err != nil {
		return nil, err
	}
	var keep []core.SavedCookie
	session := false
	for _, c := range cs {
		if c.Value == "" {
			continue
		}
		keep = append(keep, c)
		if strings.HasPrefix(c.Name, "_gumroad_app_session") {
			session = true
		}
	}
	if !session {
		return nil, nil
	}
	s := &gumSession{Cookies: keep, At: time.Now().Unix()}
	lib, err := newGumClient(st, s).library(1, false)
	if errors.Is(err, errGumLogin) {
		return nil, nil // the cookie of a visitor: the player has not logged in yet
	}
	if err != nil {
		return nil, err
	}
	s.Name = userName(lib.LoggedInUser)
	return s, saveGumSession(s)
}

// WatchGumroadLogin: the login page is open in the built-in page; once the player is logged in the login is
// kept and the purchases are read.
func WatchGumroadLogin(st *core.Store) {
	if !gumWatching.CompareAndSwap(false, true) {
		return
	}
	core.BumpRev()
	go func() {
		defer func() {
			gumWatching.Store(false)
			core.BumpRev()
		}()
		end := time.Now().Add(20 * time.Minute)
		for time.Now().Before(end) {
			time.Sleep(2 * time.Second)
			webpane.Pane.Mu.Lock()
			running := webpane.Pane.Port > 0
			webpane.Pane.Mu.Unlock()
			if !running {
				return // the window was closed
			}
			s, err := captureGumroadLogin(st)
			if errors.Is(err, errGumBlocked) {
				core.Logf("Gumroad 登录检查：%v", err)
				msg := err.Error() // say it in the status bar, instead of waiting in silence
				core.RunTask(TaskGumroad, func() { TaskGumroad.Set(0, 0, msg) })
				return
			}
			if err != nil {
				core.Logf("Gumroad 登录检查：%v", err)
				continue
			}
			if s != nil {
				core.Logf("Gumroad 已登录：%s", s.Name)
				gumWatching.Store(false)
				StartGumroadSync(st)
				return
			}
		}
	}()
}

// LogoutGumroad forgets the login, here and in the built-in page. The purchases already read stay.
func LogoutGumroad() error {
	forgetGumSession()
	if webpane.PaneMode() == "" {
		return nil
	}
	webpane.Pane.Mu.Lock()
	running := webpane.Pane.Port > 0
	webpane.Pane.Mu.Unlock()
	if !running {
		if err := webpane.Pane.Open("about:blank", "", false); err != nil {
			return err
		}
		defer webpane.Pane.CloseHidden()
	}
	sites := []string{"gumroad.com"}
	if u, err := url.Parse(core.GumroadBase()); err == nil && !strings.HasSuffix(u.Hostname(), "gumroad.com") {
		sites = append(sites, u.Hostname()) // tests: a local stand-in
	}
	return webpane.Pane.ForgetSites(sites)
}

// ---------- reading the library ----------

// StartGumroadSync reads the purchases in the background. False when one is running or nobody is logged in.
func StartGumroadSync(st *core.Store) bool {
	if LoadGumSession() == nil || !GumBusy.CompareAndSwap(false, true) {
		return false
	}
	GumCancel.Store(false)
	go func() {
		defer GumBusy.Store(false)
		ok := false
		core.RunTask(TaskGumroad, func() { ok = RunGumroadSync(st, TaskGumroad) })
		if ok {
			library.KickTranslate(st)
		}
	}()
	return true
}

func gumID(purchaseID string) string {
	return core.GumPrefix + strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, purchaseID)
}

// RunGumroadSync reads every page of the library, and the download page of each purchase it has not read
// before. True when the purchases were saved.
func RunGumroadSync(st *core.Store, prog *core.Task) bool {
	s := LoadGumSession()
	if s == nil {
		prog.Set(0, 0, "尚未登录 Gumroad")
		return false
	}
	g := newGumClient(st, s)
	prog.Set(0, 0, "正在获取 Gumroad 已购…")
	var cards []gumCard
	for _, archived := range []bool{false, true} {
		for page := 1; page <= 400; page++ {
			if GumCancel.Load() {
				prog.Set(0, 0, "已取消")
				return false
			}
			lib, err := g.library(page, archived)
			if errors.Is(err, errGumLogin) {
				forgetGumSession()
				core.BumpRev()
				prog.Set(0, 0, "Gumroad 登录已失效，请重新登录")
				return false
			}
			if err != nil {
				core.Logf("Gumroad 已购读取失败：%v", err)
				prog.Set(0, 0, "同步失败："+err.Error())
				return false
			}
			if page == 1 && !archived {
				if n := userName(lib.LoggedInUser); n != "" && n != s.Name {
					s.Name = n
					_ = saveGumSession(s)
				}
			}
			cards = append(cards, lib.Results...)
			prog.Set(0, 0, fmt.Sprintf("正在获取 Gumroad 已购，第 %d 页（%d 件）", page, len(cards)))
			if len(lib.Results) == 0 || page >= lib.Pagination.Pages {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	// what was read before stays as it is; a purchase seen for the first time gets its download page read
	st.Mu.RLock()
	known := map[string]*core.Purchase{}
	for id, p := range st.Purchases {
		if core.IsGumID(id) {
			known[id] = p
		}
	}
	st.Mu.RUnlock()
	now := time.Now().Unix()
	next := map[string]*core.Purchase{}
	urls := map[string]string{}
	var fresh []string
	for _, c := range cards {
		if c.Purchase.ID == "" {
			continue
		}
		id := gumID(c.Purchase.ID)
		if next[id] != nil {
			continue
		}
		p := &core.Purchase{ID: id, Source: "gumroad", Name: strings.TrimSpace(c.Product.Name), Thumb: c.Product.ThumbnailURL, DLPage: c.Purchase.DownloadURL, First: now}
		if v := strings.TrimSpace(c.Purchase.Variants); v != "" {
			p.Variant = v
		}
		if c.Product.Creator != nil {
			p.Shop, p.ShopURL = c.Product.Creator.Name, c.Product.Creator.ProfileURL
		}
		if o := known[id]; o != nil {
			p.First, p.Cover, p.Orders, p.PageURL = o.First, o.Cover, o.Orders, o.PageURL
			p.Files, p.Downloads, p.DLPaths, p.Note = o.Files, o.Downloads, o.DLPaths, o.Note
		}
		next[id] = p
		urls[id] = c.Purchase.DownloadURL
		if c.Purchase.DownloadURL != "" && (known[id] == nil || len(p.Downloads) == 0 || p.DLPage != known[id].DLPage) {
			fresh = append(fresh, id)
		}
	}
	for i, id := range fresh {
		if GumCancel.Load() {
			break
		}
		p := next[id]
		prog.Set(i, len(fresh), "正在获取下载页："+p.Name)
		files, d, note, err := g.files(urls[id])
		if errors.Is(err, errGumLogin) {
			break
		}
		if err != nil {
			core.Logf("Gumroad 下载页读取失败 %s：%v", p.Name, err)
			p.Note = err.Error()
			continue
		}
		p.Note = note
		p.Files, p.Downloads, p.DLPaths = nil, nil, nil
		for _, f := range files {
			p.Files = append(p.Files, f.Name)
			p.Downloads = append(p.Downloads, core.GumPrefix+f.ID)
			p.DLPaths = append(p.DLPaths, f.Path)
		}
		if d != nil && d.Purchase != nil {
			p.PageURL = d.Purchase.ProductLongURL
			if t, err := time.Parse(time.RFC3339, d.Purchase.CreatedAt); err == nil {
				p.Orders = []core.PurchaseOrder{{Date: t.Local().Format("2006-01-02")}}
			}
			if p.Name == "" {
				p.Name = d.Purchase.ProductName
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	st.Mu.Lock()
	for id := range st.Purchases {
		if core.IsGumID(id) {
			delete(st.Purchases, id)
			if next[id] == nil {
				delete(st.Booth, id) // refunded, or removed from the library
			}
		}
	}
	for id, p := range next {
		st.Purchases[id] = p
		st.Booth[id] = gumInfo(p, st.Booth[id])
	}
	st.GumroadSync = now
	library.ApplyPurchases(st)
	st.Mu.Unlock()
	_ = st.Save()
	core.BumpRev()
	downloadPurchaseThumbs(st, prog)
	st.Mu.Lock()
	for id, p := range st.Purchases {
		if core.IsGumID(id) && p.Cover != "" && st.Booth[id] != nil {
			st.Booth[id].Cover = p.Cover
		}
	}
	st.Mu.Unlock()
	_ = st.Save()
	core.BumpRev()
	prog.Set(1, 1, fmt.Sprintf("完成：已同步 %d 件 Gumroad 已购", len(next)))
	core.Logf("Gumroad 已购同步完成：%d 件，其中 %d 件是新读的", len(next), len(fresh))
	return true
}

// gumInfo: the product as the card shows it (the part Booth fills in for a Booth product).
func gumInfo(p *core.Purchase, old *core.BoothInfo) *core.BoothInfo {
	bi := &core.BoothInfo{ID: p.ID, Name: p.Name, Shop: p.Shop, ShopURL: p.ShopURL, URL: p.PageURL, ImageURL: p.Thumb, Cover: p.Cover,
		Fetched: time.Now().Unix(), Ver: booth.BoothInfoVer}
	if bi.URL == "" {
		bi.URL = p.DLPage
	}
	if old != nil && bi.Cover == "" {
		bi.Cover = old.Cover
	}
	return bi
}

// ---------- downloading ----------

// gumDownloadPath: where a file of a Gumroad purchase is asked for. Caller holds st.mu (read).
func gumDownloadPath(st *core.Store, id string) string {
	for pid, p := range st.Purchases {
		if !core.IsGumID(pid) {
			continue
		}
		for i, d := range p.Downloads {
			if d == id && i < len(p.DLPaths) {
				return p.DLPaths[i]
			}
		}
	}
	return ""
}

// resolveGumDownload asks Gumroad where the file is: its answer is a redirect to the file's (signed) address.
func resolveGumDownload(st *core.Store, id string) (string, string, error) {
	s := LoadGumSession()
	if s == nil {
		return "", "", errGumLogin
	}
	url1, name, err := resolveGumWith(st, s, id)
	if errors.Is(err, errGumLogin) {
		forgetGumSession() // the site no longer takes this login: the window offers to log in again
		core.BumpRev()
	}
	return url1, name, err
}

func resolveGumWith(st *core.Store, s *gumSession, id string) (string, string, error) {
	st.Mu.RLock()
	p := gumDownloadPath(st, id)
	st.Mu.RUnlock()
	if p == "" {
		return "", "", errors.New("未找到该文件的下载地址，请重新同步 Gumroad 已购")
	}
	base, _ := url.Parse(core.GumroadBase())
	u, err := base.Parse(p)
	if err != nil || !gumSameSite(u) {
		return "", "", errors.New("下载地址无效")
	}
	g := newGumClient(st, s)
	for hop := 0; hop < 4; hop++ {
		resp, err := g.get(u.String(), false)
		if err != nil {
			return "", "", err
		}
		var head []byte
		if resp.StatusCode >= 400 {
			head, _ = io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		}
		resp.Body.Close()
		switch {
		case gumChallenged(resp, head):
			return "", "", errGumBlocked
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			loc, err := resp.Location()
			if err != nil {
				return "", "", errors.New("Gumroad 未返回下载地址")
			}
			if !gumSameSite(loc) { // the file itself, on the storage
				name, _ := url.PathUnescape(filepath.Base(loc.Path))
				if n := dispositionName(loc.RawQuery); n != "" {
					name = n
				}
				return loc.String(), name, nil
			}
			lp := strings.ToLower(loc.Path)
			switch {
			case strings.Contains(lp, "/login"):
				return "", "", errGumLogin
			case strings.HasPrefix(lp, "/d/"), strings.Contains(lp, "/confirm"), strings.Contains(lp, "/expired"), strings.Contains(lp, "check_purchaser"):
				return "", "", errors.New("该文件暂时无法从 Gumroad 下载（正在准备、已过期或需要确认邮箱），请前往 Gumroad 已购页面查看")
			}
			u = loc
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			return "", "", errGumLogin
		case resp.StatusCode == 404:
			return "", "", errors.New("Gumroad 上未找到该文件（可能已删除）")
		default:
			return "", "", fmt.Errorf("Gumroad 返回错误（HTTP %d）", resp.StatusCode)
		}
	}
	return "", "", errors.New("Gumroad 下载地址重定向次数过多")
}

var reDispositionName = regexp.MustCompile(`filename\*?=(?:UTF-8'')?"?([^";]+)`)

// dispositionName: the file name a signed storage address asks the browser to save under
// (response-content-disposition=attachment; filename="X.zip").
func dispositionName(rawQuery string) string {
	for _, kv := range strings.Split(rawQuery, "&") {
		v, ok := strings.CutPrefix(kv, "response-content-disposition=")
		if !ok {
			continue
		}
		cd, err := url.QueryUnescape(v)
		if err != nil {
			return ""
		}
		if m := reDispositionName.FindStringSubmatch(cd); m != nil {
			if n, err := url.QueryUnescape(m[1]); err == nil {
				return strings.TrimSpace(n)
			}
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}
