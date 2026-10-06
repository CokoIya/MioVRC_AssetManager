package pandl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/cloudshare"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/naming"
	"vrclib/internal/netdisk"
	"vrclib/internal/purchases"
	"vrclib/internal/unity"
	"vrclib/internal/webpane"
)

const panSaveRoot = "/MioVRCA"

// the cookies the netdisk needs; the whole baidu.com jar makes requests too big for Baidu ("400
// Request Header Or Cookie Too Large")
var baiduCookieNames = []string{"BDUSS", "BDUSS_BFESS", "STOKEN", "PTOKEN", "BAIDUID", "BAIDUID_BFESS",
	"PSTM", "BIDUPSID", "H_PS_PSSID", "PSINO", "ndut_fmt", "PANWEB", "PANPSC"}

var ErrBaiduLogin = errors.New("百度网盘未登录或登录已失效")

func pcsBase() string {
	if v := os.Getenv("VRCLIB_PCS_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://d.pcs.baidu.com"
}

func BaiduLoginURL() string {
	if v := os.Getenv("VRCLIB_BAIDU_LOGIN"); v != "" { // tests only
		return v
	}
	return "https://passport.baidu.com/v2/?login&tpl=netdisk&u=" + url.QueryEscape("https://pan.baidu.com/disk/main")
}

// ---------- the saved login ----------

type baiduSession struct {
	Cookies []core.SavedCookie `json:"cookies"`
	Name    string             `json:"name"`
	VIP     int                `json:"vip"` // -1 unknown, 0 none, 1 会员, 2 超级会员
	At      int64              `json:"at"`
}

var (
	bdMu       sync.Mutex
	bdCache    *baiduSession
	bdLoaded   bool
	bdWatching atomic.Bool
)

func baiduSessionFile() string { return filepath.Join(core.DataDir, "baidu-session.dat") }

func SaveBaiduSession(s *baiduSession) error {
	b, _ := json.Marshal(s)
	enc, err := core.ProtectData(b)
	if err != nil {
		return err
	}
	bdMu.Lock()
	defer bdMu.Unlock()
	bdCache, bdLoaded = s, true
	return os.WriteFile(baiduSessionFile(), enc, 0600)
}

func LoadBaiduSession() *baiduSession {
	bdMu.Lock()
	defer bdMu.Unlock()
	if bdLoaded {
		return bdCache
	}
	bdLoaded = true
	b, err := os.ReadFile(baiduSessionFile())
	if err != nil {
		return nil
	}
	dec, err := core.UnprotectData(b)
	if err != nil {
		return nil
	}
	var s baiduSession
	if json.Unmarshal(dec, &s) != nil || !hasCookie(s.Cookies, "BDUSS") {
		return nil
	}
	bdCache = &s
	return bdCache
}

func ForgetBaiduSession() {
	bdMu.Lock()
	bdCache, bdLoaded = nil, true
	bdMu.Unlock()
	_ = os.Remove(baiduSessionFile())
}

func hasCookie(cs []core.SavedCookie, name string) bool {
	for _, c := range cs {
		if c.Name == name && c.Value != "" {
			return true
		}
	}
	return false
}

// pickBaiduCookies keeps the netdisk's cookies; of two with one name, the netdisk's own
// (pan.baidu.com) wins over the one for all of baidu.com.
func pickBaiduCookies(cs []core.SavedCookie) []core.SavedCookie {
	best := map[string]core.SavedCookie{}
	for _, c := range cs {
		if !core.ContainsStr(baiduCookieNames, c.Name) || c.Value == "" || !core.IsASCII(c.Value) {
			continue
		}
		if old, ok := best[c.Name]; !ok || len(strings.TrimPrefix(c.Domain, ".")) > len(strings.TrimPrefix(old.Domain, ".")) {
			best[c.Name] = c
		}
	}
	var out []core.SavedCookie
	for _, n := range baiduCookieNames {
		if c, ok := best[n]; ok {
			out = append(out, c)
		}
	}
	return out
}

type BaiduAccount struct {
	LoggedIn bool   `json:"loggedIn"`
	Name     string `json:"name,omitempty"`
	VIP      int    `json:"vip"`
	Waiting  bool   `json:"waiting,omitempty"` // the login page is open, waiting for the player
}

func CurrentBaiduAccount() BaiduAccount {
	a := BaiduAccount{Waiting: bdWatching.Load(), VIP: -1}
	if s := LoadBaiduSession(); s != nil {
		a.LoggedIn, a.Name, a.VIP = true, s.Name, s.VIP
	}
	return a
}

// WatchBaiduLogin: the login page is open in the built-in page; once Baidu has logged the player
// in, the login is kept (and downloads that waited for it go on).
func WatchBaiduLogin(st *core.Store) {
	if !bdWatching.CompareAndSwap(false, true) {
		return
	}
	core.BumpRev()
	go func() {
		defer func() {
			bdWatching.Store(false)
			core.BumpRev()
		}()
		end := time.Now().Add(20 * time.Minute)
		gap, next := 5*time.Second, time.Time{} // Baidu itself is asked less and less often while the answer stays "not yet"
		for time.Now().Before(end) {
			time.Sleep(2 * time.Second)
			webpane.Pane.Mu.Lock()
			running := webpane.Pane.Port > 0
			webpane.Pane.Mu.Unlock()
			if !running {
				return // the window was closed
			}
			s, asked, err := captureBaiduLoginIf(st, func() bool { return !time.Now().Before(next) })
			if asked {
				next = time.Now().Add(gap)
				gap = min(gap*2, time.Minute)
			}
			if err != nil {
				core.Logf("百度网盘登录检查：%v", err)
				continue
			}
			if s != nil {
				core.Logf("百度网盘已登录")
				ResumePanDownloads(st)
				return
			}
		}
	}()
}

// captureBaiduLogin takes the login from the built-in page (nil: not logged in there).
func captureBaiduLogin(st *core.Store) (*baiduSession, error) {
	s, _, err := captureBaiduLoginIf(st, func() bool { return true })
	return s, err
}

// captureBaiduLoginIf: the page's cookies are looked at every time; Baidu is asked whose they are (asked) only
// when they look like a login and ask allows it.
func captureBaiduLoginIf(st *core.Store, ask func() bool) (s *baiduSession, asked bool, err error) {
	cs, err := webpane.Pane.Cookies([]string{core.PanBase() + "/"})
	if err != nil {
		return nil, false, err
	}
	cs = pickBaiduCookies(cs)
	if !hasCookie(cs, "BDUSS") || !hasCookie(cs, "STOKEN") || !ask() {
		return nil, false, nil // not logged in, or the netdisk page has not opened after the login yet
	}
	s = &baiduSession{Cookies: cs, At: time.Now().Unix(), VIP: -1}
	bc := NewBDClient(st, s)
	defer bc.c.CloseIdleConnections()
	name, vip, err := bc.Whoami()
	if errors.Is(err, ErrBaiduLogin) {
		return nil, true, nil // a cookie from an older login: the page has not logged in yet
	}
	if err != nil {
		return nil, true, err
	}
	s.Name, s.VIP = name, vip
	return s, true, SaveBaiduSession(s)
}

// LogoutBaidu forgets the login, here and in the built-in page (so its login page asks again).
func LogoutBaidu() error {
	ForgetBaiduSession()
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
	sites := []string{"baidu.com"}
	if u, err := url.Parse(core.PanBase()); err == nil && !strings.HasSuffix(u.Hostname(), "baidu.com") {
		sites = append(sites, u.Hostname()) // tests: a local stand-in
	}
	return webpane.Pane.ForgetSites(sites)
}

// ---------- requests ----------

type bdClient struct {
	c        *http.Client
	dl       *http.Client
	ctx      context.Context // ends when the player cancels the job
	mu       sync.Mutex
	cookies  []core.SavedCookie
	bdstoken string
	dlUA     int
	ref      string // Referer: the share page while working on a share
}

// the download answers to the netdisk client; the second one when Baidu turns the first away
var pcsUAs = []string{"pan.baidu.com", "netdisk;7.42.0.5;PC;PC-Windows;10.0.22631;WindowsBaiduYunGuanJia"}

func NewBDClient(st *core.Store, s *baiduSession) *bdClient {
	tr := netdisk.NewPanClient(st).HTTP.Transport // Baidu is reached directly, like the share listings
	return &bdClient{c: &http.Client{Transport: tr, Timeout: 40 * time.Second}, dl: &http.Client{Transport: tr},
		ctx: context.Background(), cookies: append([]core.SavedCookie(nil), s.Cookies...)}
}

// sleep waits between requests; errPanCancelled when the job is cancelled meanwhile.
func (b *bdClient) sleep(d time.Duration) error {
	if !purchases.Sleep(b.ctx, d) {
		return errPanCancelled
	}
	return nil
}

func (b *bdClient) cookieHeader() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	parts := make([]string, 0, len(b.cookies))
	for _, c := range b.cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// keep what Baidu sets on the way (BDCLND after the share's code, renewed tokens)
func (b *bdClient) absorb(resp *http.Response) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, n := range resp.Cookies() {
		if n.Value == "" || (n.Name != "BDCLND" && !core.ContainsStr(baiduCookieNames, n.Name)) {
			continue
		}
		found := false
		for i := range b.cookies {
			if b.cookies[i].Name == n.Name {
				b.cookies[i].Value, found = n.Value, true
			}
		}
		if !found {
			b.cookies = append(b.cookies, core.SavedCookie{Name: n.Name, Value: n.Value})
		}
	}
}

func (b *bdClient) setCookie(name, value string) {
	b.absorb(&http.Response{Header: http.Header{"Set-Cookie": {name + "=" + value}}})
}

func (b *bdClient) do(method, u string, form url.Values) ([]byte, *http.Response, error) {
	var rd io.Reader
	if form != nil {
		rd = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequestWithContext(b.ctx, method, u, rd)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	ref := b.ref
	if ref == "" {
		ref = core.PanBase() + "/disk/home"
	}
	req.Header.Set("Referer", ref)
	req.Header.Set("Cookie", b.cookieHeader())
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	resp, err := b.c.Do(req)
	if err != nil {
		if b.ctx.Err() != nil {
			return nil, nil, errPanCancelled
		}
		return nil, nil, netdisk.FriendlyPanErr(err)
	}
	defer resp.Body.Close()
	b.absorb(resp)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return body, resp, err
}

// call: a netdisk API; out gets the answer. Returns Baidu's errno.
func (b *bdClient) call(method, p string, q, form url.Values, out any) (int, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("channel", "chunlei")
	q.Set("web", "1")
	q.Set("app_id", "250528")
	q.Set("clienttype", "0")
	if b.bdstoken != "" && q.Get("bdstoken") == "" {
		q.Set("bdstoken", b.bdstoken)
	}
	body, resp, err := b.do(method, core.PanBase()+p+"?"+q.Encode(), form)
	if err != nil {
		return 0, err
	}
	var e struct {
		Errno json.Number `json:"errno"`
	}
	if json.Unmarshal(body, &e) != nil {
		if resp.StatusCode == 400 && strings.Contains(strings.ToLower(string(body)), "too large") {
			return 0, errors.New("百度网盘拒绝请求（登录状态数据过长），请退出登录后重新登录")
		}
		return 0, fmt.Errorf("百度网盘响应异常（%d）", resp.StatusCode)
	}
	if out != nil {
		_ = json.Unmarshal(body, out)
	}
	n, _ := e.Errno.Int64()
	return int(n), nil
}

// Whoami: the account and the token the netdisk's write requests need.
func (b *bdClient) Whoami() (name string, vip int, err error) {
	var r struct {
		Result struct {
			Bdstoken   string      `json:"bdstoken"`
			Username   string      `json:"username"`
			Loginstate json.Number `json:"loginstate"`
		} `json:"result"`
	}
	errno, err := b.call("GET", "/api/gettemplatevariable", url.Values{"fields": {`["bdstoken","username","loginstate"]`}}, nil, &r)
	if err != nil {
		return "", 0, err
	}
	if errno != 0 || r.Result.Loginstate.String() != "1" || r.Result.Bdstoken == "" {
		return "", 0, ErrBaiduLogin
	}
	b.bdstoken = r.Result.Bdstoken
	// the membership, when Baidu says it (only shown to the player; downloads work either way)
	vip = -1
	var m struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if n, err := b.call("GET", "/api/gettemplatevariable", url.Values{"fields": {`["is_vip","is_svip"]`}}, nil, &m); err == nil && n == 0 {
		flag := func(k string) bool {
			s := strings.Trim(string(m.Result[k]), `"`)
			return s == "1" || s == "true"
		}
		_, hasV := m.Result["is_vip"]
		_, hasS := m.Result["is_svip"]
		switch {
		case flag("is_svip"):
			vip = 2
		case flag("is_vip"):
			vip = 1
		case hasV || hasS:
			vip = 0
		}
	}
	return r.Result.Username, vip, nil
}

// ---------- the share ----------

func (b *bdClient) openShare(surl, pwd string) (*netdisk.BDShare, error) {
	short := strings.TrimPrefix(surl, "1")
	defer func() { b.ref = core.PanBase() + "/s/" + surl }() // what follows is done "from" the share page
	if pwd != "" {
		b.ref = core.PanBase() + "/share/init?surl=" + short
		var v struct {
			Randsk string `json:"randsk"`
		}
		errno, err := b.call("POST", "/share/verify", url.Values{"surl": {short}, "t": {strconv.FormatInt(time.Now().UnixMilli(), 10)}},
			url.Values{"pwd": {pwd}, "vcode": {""}, "vcode_str": {""}}, &v)
		if err != nil {
			return nil, err
		}
		if errno != 0 {
			return nil, errors.New(netdisk.PanErrno(errno))
		}
		if v.Randsk != "" {
			b.setCookie("BDCLND", v.Randsk)
		}
	}
	b.ref = core.PanBase() + "/disk/home"
	page, resp, err := b.do("GET", core.PanBase()+"/s/"+surl, nil)
	if err != nil {
		return nil, err
	}
	if s := netdisk.ParseSharePage(page); s != nil {
		return s, nil
	}
	text := string(page)
	switch {
	case resp != nil && strings.Contains(resp.Request.URL.Path, "/share/init"):
		if pwd == "" {
			return nil, errors.New("该分享需要提取码，请在详情中填写后重新下载")
		}
		return nil, errors.New("提取码验证未通过")
	case strings.Contains(text, "wappass") || strings.Contains(text, "验证码"):
		return nil, errors.New("百度要求输入验证码，请点击「在软件中打开分享」手动输入后重新下载")
	case strings.Contains(text, "分享的文件已经被删除") || strings.Contains(text, "分享的文件已经被取消") ||
		strings.Contains(text, "链接已过期") || strings.Contains(text, "分享已过期") || strings.Contains(text, "platform-non-found"):
		return nil, errors.New("分享已失效或被取消")
	}
	netdisk.WriteDebug(core.Truncate(text, 200000))
	return nil, errors.New("无法读取分享内容（百度网盘页面可能已改版）")
}

func (b *bdClient) shareList(s *netdisk.BDShare, dir string) ([]netdisk.PanRaw, error) {
	var all []netdisk.PanRaw
	for page := 1; page <= 50; page++ {
		var r struct {
			List []netdisk.PanRaw `json:"list"`
		}
		errno, err := b.call("GET", "/share/list", url.Values{"uk": {s.ShareUK.String()}, "shareid": {s.ShareID.String()},
			"order": {"other"}, "desc": {"1"}, "showempty": {"0"}, "page": {strconv.Itoa(page)}, "num": {"100"}, "dir": {dir}}, nil, &r)
		if err != nil {
			return nil, err
		}
		if errno != 0 {
			return nil, errors.New(netdisk.PanErrno(errno))
		}
		all = append(all, r.List...)
		if len(r.List) < 100 {
			break
		}
		if err := b.sleep(200 * time.Millisecond); err != nil {
			return nil, err
		}
	}
	return all, nil
}

func rawName(r netdisk.PanRaw) string { return html.UnescapeString(r.Name) }

// shareLister finds things in a share by their path ("/folder/file.zip"), listing each folder once.
type shareLister struct {
	b     *bdClient
	s     *netdisk.BDShare
	cache map[string][]netdisk.PanRaw
}

func newShareLister(b *bdClient, s *netdisk.BDShare) *shareLister {
	return &shareLister{b: b, s: s, cache: map[string][]netdisk.PanRaw{}}
}

func (l *shareLister) list(dir string) ([]netdisk.PanRaw, error) {
	if kids, ok := l.cache[dir]; ok {
		return kids, nil
	}
	kids, err := l.b.shareList(l.s, dir)
	if err == nil {
		l.cache[dir] = kids
	}
	return kids, err
}

func (l *shareLister) find(p string) (netdisk.PanRaw, error) {
	cur := l.s.FileList
	segs := strings.Split(strings.Trim(p, "/"), "/")
	for i, seg := range segs {
		var hit *netdisk.PanRaw
		for k := range cur {
			if rawName(cur[k]) == seg {
				hit = &cur[k]
				break
			}
		}
		if hit == nil {
			return netdisk.PanRaw{}, errors.New("分享中未找到「" + seg + "」（分享内容可能已变更，请刷新后重试）")
		}
		if i == len(segs)-1 {
			return *hit, nil
		}
		kids, err := l.list(hit.Path)
		if err != nil {
			return netdisk.PanRaw{}, err
		}
		cur = kids
	}
	return netdisk.PanRaw{}, errors.New("分享中未找到该素材")
}

// ---------- the player's netdisk ----------

type bdEntry struct {
	Path  string      `json:"path"`
	Name  string      `json:"server_filename"`
	IsDir json.Number `json:"isdir"`
	Size  json.Number `json:"size"`
	Mtime json.Number `json:"server_mtime"`
}

func (b *bdClient) list(dir string) ([]bdEntry, error) {
	var all []bdEntry
	for start := 0; start < 20000; start += 100 {
		var r struct {
			List []bdEntry `json:"list"`
		}
		ref := b.ref
		b.ref = ""
		errno, err := b.call("GET", "/api/list", url.Values{"dir": {dir}, "order": {"name"}, "start": {strconv.Itoa(start)}, "num": {"100"}}, nil, &r)
		b.ref = ref
		if err != nil {
			return nil, err
		}
		if errno == -9 {
			return nil, nil // not there
		}
		if errno != 0 {
			return nil, bdWriteErr("读取网盘文件夹", errno)
		}
		all = append(all, r.List...)
		if len(r.List) < 100 {
			break
		}
	}
	return all, nil
}

func (b *bdClient) mkdir(p string) error {
	var r struct {
		Path string `json:"path"`
	}
	errno, err := b.call("POST", "/api/create", url.Values{"a": {"commit"}},
		url.Values{"path": {p}, "isdir": {"1"}, "size": {"0"}, "block_list": {"[]"}, "rtype": {"0"}}, &r)
	if err != nil {
		return err
	}
	if errno != 0 && errno != -8 { // -8: it is there already
		return bdWriteErr("在网盘中创建文件夹", errno)
	}
	return nil
}

// remove puts things of the player's netdisk into its recycle bin (Baidu keeps them there for ten days or
// more, so the player can still take them back). Only ever asked for what is inside the program's own folder.
func (b *bdClient) remove(paths []string) error {
	for _, p := range paths {
		if path.Clean(p) != p || !strings.HasPrefix(p, panSaveRoot+"/") || strings.Count(p, "/") < 3 {
			return errors.New("无法替换网盘中的旧文件（路径不在「" + panSaveRoot + "」内）")
		}
	}
	list, _ := json.Marshal(paths)
	var r struct {
		TaskID json.Number `json:"taskid"`
	}
	ref := b.ref
	b.ref = ""
	errno, err := b.call("POST", "/api/filemanager", url.Values{"opera": {"delete"}, "async": {"2"}, "onnest": {"fail"}},
		url.Values{"filelist": {string(list)}}, &r)
	b.ref = ref
	if err != nil {
		return err
	}
	if errno != 0 {
		return bdWriteErr("替换网盘中的旧文件", errno)
	}
	if id := r.TaskID.String(); id != "" && id != "0" {
		return b.waitTask(id, "替换网盘中的旧文件")
	}
	return nil
}

func bdWriteErr(what string, errno int) error {
	switch errno {
	case -6, -4, 9019, 4000023:
		return ErrBaiduLogin
	case -10, -32, 3:
		return errors.New("百度网盘空间不足，请清理文件后重试")
	case 111:
		return errors.New("百度网盘有其他转存任务正在进行，请稍后重试")
	case -62, -63:
		return errors.New("百度要求输入验证码，请点击「在软件中打开分享」手动输入后重新下载")
	case 105, 115, 116, 117, 145:
		return errors.New("分享已失效或被取消")
	}
	return fmt.Errorf("%s失败（百度网盘错误 %d）", what, errno)
}

// transfer saves share items into dest. Over the account's file limit for one save, the folders are
// saved piece by piece.
func (b *bdClient) transfer(s *netdisk.BDShare, items []netdisk.PanRaw, dest string, depth int) error {
	for i := 0; i < len(items); i += 100 {
		batch := items[i:min(i+100, len(items))]
		ids := make([]string, 0, len(batch))
		for _, it := range batch {
			ids = append(ids, it.FsID.String())
		}
		var r struct {
			TargetNums  int `json:"target_file_nums"`
			TargetLimit int `json:"target_file_nums_limit"`
			Info        []struct {
				Errno int `json:"errno"`
			} `json:"info"`
			TaskID json.Number `json:"task_id"`
		}
		errno, err := b.call("POST", "/share/transfer", url.Values{"shareid": {s.ShareID.String()}, "from": {s.ShareUK.String()},
			"ondup": {"newcopy"}, "async": {"1"}}, url.Values{"fsidlist": {"[" + strings.Join(ids, ",") + "]"}, "path": {dest}}, &r)
		if err != nil {
			return err
		}
		tooMany := (errno == 12 && r.TargetLimit > 0 && r.TargetNums > r.TargetLimit) || errno == 120 || errno == -33
		dup := errno == 4 || (errno == 12 && len(r.Info) > 0 && r.Info[0].Errno == -30)
		switch {
		case errno == 0 || dup:
			if id := r.TaskID.String(); id != "" && id != "0" {
				if err := b.waitTask(id, "转存到网盘"); err != nil {
					return err
				}
			}
		case tooMany && depth < 8:
			core.Logf("网盘转存：一次存不下（%d / %d 个文件），分开存", r.TargetNums, r.TargetLimit)
			for _, it := range batch {
				if it.IsDir.String() != "1" {
					if err := b.transfer(s, []netdisk.PanRaw{it}, dest, 99); err != nil {
						return err
					}
					continue
				}
				sub := path.Join(dest, rawName(it))
				if err := b.mkdir(sub); err != nil {
					return err
				}
				kids, err := b.shareList(s, it.Path)
				if err != nil {
					return err
				}
				if err := b.transfer(s, kids, sub, depth+1); err != nil {
					return err
				}
			}
		case tooMany:
			return fmt.Errorf("文件数量过多，百度网盘单次最多转存 %d 个，可在网盘中手动分批保存", r.TargetLimit)
		default:
			return bdWriteErr("转存到网盘", errno)
		}
	}
	return nil
}

// panPoll: between two looks at something Baidu does in the background (tests shorten it).
var panPoll = 2 * time.Second

// waitTask: a big save (or removal) runs in the background on Baidu's side.
func (b *bdClient) waitTask(id, what string) error {
	for i := 0; i < 300; i++ {
		var r struct {
			Status    string `json:"status"`
			TaskErrno int    `json:"task_errno"`
		}
		errno, err := b.call("GET", "/share/taskquery", url.Values{"taskid": {id}}, nil, &r)
		if err != nil {
			return err
		}
		switch {
		case errno != 0:
			return b.sleep(panPoll * 3 / 2) // no word on it: the listing afterwards shows what arrived
		case r.Status == "success":
			return nil
		case r.Status == "failed":
			return bdWriteErr(what, r.TaskErrno)
		}
		if err := b.sleep(panPoll); err != nil {
			return err
		}
	}
	return errors.New("网盘转存尚未结束，请稍后重新下载（已转存的文件不会重复转存）")
}

type bdFile struct {
	Path string
	Rel  string
	Size int64
}

// tree: every file under dir (paths relative to it).
func (b *bdClient) tree(dir string) ([]bdFile, error) {
	var out []bdFile
	var walk func(d, rel string, depth int) error
	walk = func(d, rel string, depth int) error {
		ents, err := b.list(d)
		if err != nil {
			return err
		}
		for _, e := range ents {
			r := path.Join(rel, e.Name)
			if e.IsDir.String() == "1" {
				if depth < 12 {
					if err := walk(e.Path, r, depth+1); err != nil {
						return err
					}
				}
				continue
			}
			sz, _ := e.Size.Int64()
			out = append(out, bdFile{Path: e.Path, Rel: r, Size: sz})
			if len(out) > 20000 {
				return errors.New("文件数量过多")
			}
		}
		return nil
	}
	return out, walk(dir, "", 0)
}

// ---------- downloading ----------

type pcsError struct {
	status int
	body   string
}

func (e pcsError) Error() string {
	return fmt.Sprintf("百度网盘拒绝下载（%d %s）", e.status, core.Truncate(e.body, 120))
}

func (e pcsError) auth() bool {
	if e.status != 401 && e.status != 403 {
		return false
	}
	for _, m := range []string{"31045", "31064", "31066", "user not exists", "auth fail"} {
		if strings.Contains(strings.ToLower(e.body), m) {
			return true
		}
	}
	return false
}

func pcsURL(remote string) string {
	return pcsBase() + "/rest/2.0/pcs/file?method=download&app_id=250528&path=" + strings.ReplaceAll(url.QueryEscape(remote), "+", "%20")
}

// fetchRange appends remote[from:] to part.
func (b *bdClient) fetchRange(remote, part string, from, size int64, prog func(n int64)) error {
	g := purchases.NewStallGuard(b.ctx)
	defer g.Stop()
	netErr := func(err error) error {
		switch {
		case b.ctx.Err() != nil:
			return errPanCancelled
		case g.Stalled():
			return fmt.Errorf("下载中断（%d 秒内未收到数据）", int(purchases.StallAfter.Seconds()))
		}
		return fmt.Errorf("下载中断（%s）", purchases.NetErrText(err))
	}
	req, _ := http.NewRequestWithContext(g.Ctx, "GET", pcsURL(remote), nil)
	b.mu.Lock()
	req.Header.Set("User-Agent", pcsUAs[b.dlUA])
	b.mu.Unlock()
	req.Header.Set("Cookie", b.cookieHeader())
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, size-1))
	resp, err := b.dl.Do(req)
	if err != nil {
		return netErr(err)
	}
	defer resp.Body.Close()
	g.Kick()
	if resp.StatusCode != 206 && !(resp.StatusCode == 200 && from == 0) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
		return pcsError{resp.StatusCode, string(body)}
	}
	if cr := resp.Header.Get("Content-Range"); resp.StatusCode == 206 && cr != "" {
		var a, z int64
		if _, err := fmt.Sscanf(strings.TrimPrefix(cr, "bytes "), "%d-%d", &a, &z); err != nil || a != from {
			return fmt.Errorf("百度网盘返回的数据范围有误（%s）", cr)
		}
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return purchases.WriteErr(part, err)
	}
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			g.Kick()
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return purchases.WriteErr(part, werr)
			}
			prog(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return netErr(rerr)
		}
	}
	if err := f.Close(); err != nil {
		return purchases.WriteErr(part, err)
	}
	return nil
}

// panRetryUnit: the pauses between tries are counted in this (tests shorten it).
var panRetryUnit = time.Second

// download one file with resume: what is in local+".part" is kept between tries (and runs). What the disk
// refuses is not tried again: only the connection is.
func (b *bdClient) download(f bdFile, local string, prog func(n int64)) error {
	if fi, err := os.Stat(local); err == nil && fi.Size() == f.Size {
		prog(f.Size)
		return nil // downloaded before
	}
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return purchases.WriteErr(filepath.Dir(local), err)
	}
	if f.Size == 0 {
		if err := os.WriteFile(local, nil, 0644); err != nil {
			return purchases.WriteErr(local, err)
		}
		return nil
	}
	part := local + ".part"
	var had int64
	if fi, err := os.Stat(part); err == nil {
		if fi.Size() > f.Size {
			_ = os.Remove(part)
		} else {
			had = fi.Size()
			prog(had)
		}
	}
	if err := purchases.CheckSpace(filepath.Dir(local), f.Size-had); err != nil {
		return err
	}
	var lastErr error
	stuck := 0
	for attempt := 0; attempt < 40; attempt++ {
		if b.ctx.Err() != nil {
			return errPanCancelled
		}
		var have int64
		if fi, err := os.Stat(part); err == nil {
			have = fi.Size()
		}
		if have == f.Size {
			_ = os.Remove(local)
			if err := os.Rename(part, local); err != nil {
				return purchases.WriteErr(local, err)
			}
			return nil
		}
		err := b.fetchRange(f.Path, part, have, f.Size, prog)
		if err == nil {
			if fi, e := os.Stat(part); e == nil && fi.Size() > have {
				continue
			}
			if stuck++; stuck >= 3 {
				return errors.New("百度网盘返回的文件不完整（" + path.Base(f.Path) + "），请稍后重新下载")
			}
			err = errors.New("百度网盘返回的文件不完整") // nothing more came: try again a little later
		}
		if errors.Is(err, errPanCancelled) || purchases.IsLocalErr(err) {
			return err
		}
		var pe pcsError
		if errors.As(err, &pe) {
			if pe.auth() {
				return ErrBaiduLogin
			}
			b.mu.Lock()
			switched := b.dlUA+1 < len(pcsUAs)
			if switched {
				b.dlUA++
			}
			b.mu.Unlock()
			if !switched && (pe.status == 403 || pe.status == 404) {
				return err
			}
		}
		lastErr = err
		if err := b.sleep(time.Duration(min(2+attempt*2, 20)) * panRetryUnit); err != nil {
			return err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("下载失败，请重试")
	}
	return lastErr
}

// ---------- jobs ----------

type PanJob struct {
	ID      int64    `json:"id"`
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Stage   string   `json:"stage"` // queued, login, save, download, unpack, done, failed
	Msg     string   `json:"msg"`
	Done    int64    `json:"done"`
	Total   int64    `json:"total"`
	ByCount bool     `json:"byCount,omitempty"` // the sizes are not known beforehand: the progress is Pct, by files
	Pct     int      `json:"pct,omitempty"`
	File    string   `json:"file,omitempty"`
	Files   int      `json:"files"`
	FileN   int      `json:"fileN"`
	Speed   int64    `json:"speed"`
	Slow    bool     `json:"slow,omitempty"` // much slower than the line: Baidu's limit for accounts without 超级会员
	Dir     string   `json:"dir,omitempty"`
	Saved   string   `json:"saved,omitempty"` // where in the netdisk
	Err     string   `json:"err,omitempty"`
	Failed  []string `json:"failed,omitempty"` // archives that did not unpack
	Project string   `json:"project,omitempty"`
	Paths   []string `json:"paths,omitempty"` // only these parts of the card's file list

	imp     *unity.ImportReq
	counted bool // in core.Downloading
}

var (
	panDLMu      sync.Mutex
	panJobs      []*PanJob
	panDLRunning bool
	panCtx       context.Context // ends when the player cancels; what is queued after that gets a new one
	panStop      context.CancelFunc
	TaskPanDL    = &core.Task{Name: "pandl", Label: "下载网盘分享"}

	errPanCancelled = purchases.ErrCancelled
)

const panKeep = 50 // finished jobs the window still lists

func panOver(stage string) bool { return stage == "done" || stage == "failed" }

// recountPanLocked keeps core.Downloading in step with the jobs: a job counts from being queued until it is
// over for whatever reason (one waiting for a login does not: nothing is running for it). Caller holds panDLMu.
func recountPanLocked() {
	for _, j := range panJobs {
		if a := !panOver(j.Stage) && j.Stage != "login"; a != j.counted {
			j.counted = a
			if a {
				core.Downloading.Add(1)
			} else {
				core.Downloading.Add(-1)
			}
		}
	}
}

// keepPanLocked: the list without the jobs drop picks; one that still counts is uncounted on its way out.
// Caller holds panDLMu.
func keepPanLocked(drop func(j *PanJob) bool) {
	kept := panJobs[:0]
	for _, j := range panJobs {
		if !drop(j) {
			kept = append(kept, j)
			continue
		}
		if j.counted {
			j.counted = false
			core.Downloading.Add(-1)
		}
	}
	clear(panJobs[len(kept):])
	panJobs = kept
}

func PanJobsSnapshot() []PanJob {
	panDLMu.Lock()
	defer panDLMu.Unlock()
	out := make([]PanJob, 0, len(panJobs))
	for _, j := range panJobs {
		out = append(out, *j)
	}
	return out
}

// setPan changes a job; the window reloads only when its stage changes.
func setPan(j *PanJob, f func(j *PanJob)) {
	panDLMu.Lock()
	stage := j.Stage
	f(j)
	changed := j.Stage != stage
	if changed {
		recountPanLocked()
	}
	panDLMu.Unlock()
	if changed {
		core.BumpRev()
	}
}

// QueuePanDownload downloads a netdisk asset (a share, or one product inside it), or only the picked parts of
// its file list ("/folder/file.zip"); with imp it is imported into a Unity project afterwards.
func QueuePanDownload(st *core.Store, key string, paths []string, imp *unity.ImportReq) error {
	surl, _ := netdisk.SplitPanKey(key)
	st.Mu.RLock()
	u := st.User[netdisk.ShareKey(surl)]
	link := ""
	if u != nil {
		link = u.ShareURL
	}
	title := ""
	for _, v := range library.AllViews(st) {
		if v.Key == key || (v.FromPan == key && title == "") { // downloaded before: the folder's card
			title = v.Name
		}
	}
	st.Mu.RUnlock()
	if netdisk.ShareID(link) == "" {
		return errors.New("该素材没有百度网盘分享链接")
	}
	if title == "" {
		title = netdisk.SharePlaceholder(surl)
	}
	panDLMu.Lock()
	for _, o := range panJobs {
		if o.Key == key && !panOver(o.Stage) {
			panDLMu.Unlock()
			return errors.New("已在下载队列中")
		}
	}
	keepPanLocked(func(o *PanJob) bool { return o.Key == key })
	j := &PanJob{ID: time.Now().UnixNano(), Key: key, Title: title, Stage: "queued", Msg: "排队中", Paths: normPanPaths(paths), imp: imp}
	if imp != nil {
		j.Project = imp.Project
	}
	panJobs = append(panJobs, j)
	// over the limit the oldest finished ones go; a job that waits or runs is never dropped
	over := len(panJobs) - panKeep
	keepPanLocked(func(o *PanJob) bool {
		if over > 0 && panOver(o.Stage) {
			over--
			return true
		}
		return false
	})
	recountPanLocked()
	start := !panDLRunning
	panDLRunning = true
	panDLMu.Unlock()
	core.BumpRev()
	if start {
		go panDLWorker(st)
	}
	return nil
}

// ResumePanDownloads: after logging in, the downloads that waited for it go on.
func ResumePanDownloads(st *core.Store) {
	panDLMu.Lock()
	n := 0
	for _, j := range panJobs {
		if j.Stage == "login" {
			j.Stage, j.Msg, j.Err = "queued", "排队中", ""
			n++
		}
	}
	recountPanLocked()
	start := n > 0 && !panDLRunning
	if start {
		panDLRunning = true
	}
	panDLMu.Unlock()
	core.BumpRev()
	if start {
		go panDLWorker(st)
	}
}

// CancelPanDownloads stops the job that is running (its requests are ended, its waits cut short) and takes
// the waiting ones off the queue.
func CancelPanDownloads() {
	panDLMu.Lock()
	if panStop != nil {
		panStop()
	}
	for _, j := range panJobs {
		if j.Stage == "queued" || j.Stage == "login" {
			j.Stage, j.Msg, j.Err = "failed", "已取消", "已取消"
		}
	}
	recountPanLocked()
	panDLMu.Unlock()
	core.BumpRev()
}

// CancelAll: the program is closing — every download stops, the netdisk's and the Booth / Gumroad ones.
// core.Downloading is back at zero once the running ones have let go.
func CancelAll() {
	CancelPanDownloads()
	purchases.CancelAllDownloads()
}

// DismissPanJob: the player closed a finished job's note.
func DismissPanJob(key string) {
	panDLMu.Lock()
	keepPanLocked(func(j *PanJob) bool { return j.Key == key && panOver(j.Stage) })
	panDLMu.Unlock()
	core.BumpRev()
}

// nextPanJob: the next waiting job and the context it runs under.
func nextPanJob() (*PanJob, context.Context) {
	panDLMu.Lock()
	defer panDLMu.Unlock()
	for _, j := range panJobs {
		if j.Stage == "queued" {
			j.Stage = "save"
			if panCtx == nil || panCtx.Err() != nil { // cancelled before: what was queued since then runs
				panCtx, panStop = context.WithCancel(context.Background())
			}
			return j, panCtx
		}
	}
	panDLRunning = false
	return nil, nil
}

// runPan: one job (tests put their own here).
var runPan = runPanJob

func panDLWorker(st *core.Store) {
	got := 0
	again, login := false, false
	core.RunTask(TaskPanDL, func() {
		var cur *PanJob
		clean := false
		defer func() {
			if clean {
				return
			}
			// a panic (RunTask reports it): that job is over, and the queue is not left stuck behind it
			panDLMu.Lock()
			if cur != nil && !panOver(cur.Stage) {
				cur.Stage, cur.Err, cur.Msg = "failed", "下载出错，详见 library.log", "下载出错，详见 library.log"
				for _, o := range panJobs {
					again = again || o.Stage == "queued"
				}
			}
			panDLRunning = again
			recountPanLocked()
			panDLMu.Unlock()
		}()
		for {
			j, ctx := nextPanJob()
			if j == nil {
				break
			}
			cur = j
			core.BumpRev()
			err := runPan(ctx, st, j)
			switch {
			case err == nil:
				got++
			case ctx.Err() != nil:
				setPan(j, func(j *PanJob) { j.Stage, j.Err, j.Msg = "failed", "已取消", "已取消" })
			case errors.Is(err, ErrBaiduLogin):
				ForgetBaiduSession() // Baidu turned the saved login away: the login page asks again
				// the Baidu jobs wait for the login; a Google Drive / Dropbox one needs none and goes on
				panDLMu.Lock()
				for _, o := range panJobs {
					if o == j || (o.Stage == "queued" && !cloudshare.IsCloudKey(o.Key)) {
						o.Stage, o.Msg, o.Err = "login", "需要登录百度网盘", ""
					}
				}
				recountPanLocked()
				panDLMu.Unlock()
				login = true
				TaskPanDL.Set(0, 0, "需要登录百度网盘")
				core.BumpRev()
			default:
				core.Logf("网盘下载失败 %s: %v", j.Key, err)
				setPan(j, func(j *PanJob) { j.Stage, j.Err, j.Msg = "failed", err.Error(), err.Error() })
			}
		}
		clean = true
		switch {
		case got > 0:
			TaskPanDL.Set(1, 1, fmt.Sprintf("完成：已下载 %d 个网盘分享", got))
		case login:
			TaskPanDL.Set(0, 0, "需要登录百度网盘")
		default:
			TaskPanDL.Set(0, 0, "")
		}
	})
	if again {
		go panDLWorker(st) // the jobs behind the one that crashed
	}
}

func runPanJob(ctx context.Context, st *core.Store, j *PanJob) error {
	if cloudshare.IsCloudKey(j.Key) {
		return runCloudJob(ctx, st, j)
	}
	s := LoadBaiduSession()
	if webpane.PaneMode() != "" {
		webpane.Pane.Mu.Lock()
		running := webpane.Pane.Port > 0
		webpane.Pane.Mu.Unlock()
		if running { // the built-in page keeps its login fresh: take it over (or the first login, not noticed yet)
			if fresh, err := captureBaiduLogin(st); err == nil && fresh != nil {
				s = fresh
			}
		}
	}
	if s == nil {
		return ErrBaiduLogin
	}
	b := NewBDClient(st, s)
	b.ctx = ctx
	defer b.c.CloseIdleConnections()
	msg := func(m string) {
		setPan(j, func(j *PanJob) { j.Msg = m })
		TaskPanDL.Set(0, 0, j.Title+"："+m)
	}
	msg("正在检查百度网盘登录状态")
	if _, _, err := b.Whoami(); err != nil {
		return err
	}
	surl, sub := netdisk.SplitPanKey(j.Key)
	st.Mu.RLock()
	var link, pwd string
	if u := st.User["pan:"+surl]; u != nil {
		link, pwd = u.ShareURL, u.SharePwd
	}
	extract, keep := !st.Settings.NoExtract, st.Settings.KeepZip
	dlRoot := purchases.DownloadDir(st)
	prev, got := "", []string(nil)
	if u := st.User[j.Key]; u != nil {
		prev = u.DownloadDir // a download that stopped goes on where it was
		if prev == "" {
			prev = u.Downloaded
		}
		got = u.PanGot
	}
	st.Mu.RUnlock()
	if pwd == "" {
		pwd = netdisk.SharePwdFromURL(link)
	}
	msg("正在打开分享")
	share, err := b.openShare(surl, strings.TrimSpace(pwd))
	if err != nil {
		return err
	}
	ls := newShareLister(b, share)
	// the card's content: the whole share, or one product in it
	items := share.FileList
	if sub != "" {
		it, err := ls.find(sub)
		if err != nil {
			return err
		}
		items = []netdisk.PanRaw{it}
	}
	if len(items) == 0 {
		return errors.New("分享内容为空")
	}
	if strings.HasPrefix(j.Title, "网盘分享 ") { // added a moment ago, before its listing was read
		t := strings.TrimSpace(share.Title)
		if len(items) == 1 {
			t = core.StripArchiveExt(rawName(items[0]))
		}
		if t = naming.CleanName(t); t != "" {
			setPan(j, func(j *PanJob) { j.Title = t })
		}
	}
	oneDir := len(items) == 1 && items[0].IsDir.String() == "1"
	// what to save: all of it, or the parts picked in the card's file list (which starts inside a product's folder)
	var picks []panPick
	whole := len(j.Paths) == 0 || (sub != "" && !oneDir)
	if whole {
		for _, it := range items {
			t := "/"
			if sub == "" {
				t = "/" + rawName(it)
			}
			picks = append(picks, panPick{tree: t, raw: it})
		}
	} else {
		top, copyTop := "", ""
		if sub != "" {
			top, copyTop = sub, "/"+rawName(items[0])
		}
		for _, p := range j.Paths {
			it, err := ls.find(top + p)
			if err != nil {
				return err
			}
			d := path.Dir(p)
			if d == "/" {
				d = ""
			}
			picks = append(picks, panPick{tree: p, raw: it, dir: copyTop + d})
		}
	}
	// 1. save into the player's netdisk: /MioVRCA/<name>
	name := core.SafeName(naming.CleanName(j.Title), 60)
	if name == "" || name == "_" {
		name = "网盘分享 " + surl
	}
	dest := panSaveRoot + "/" + name
	st.Mu.RLock()
	if u := st.User[j.Key]; u != nil && strings.HasPrefix(u.PanCopy, panSaveRoot+"/") { // where an earlier download saved it
		dest = u.PanCopy
	}
	st.Mu.RUnlock()
	// where it goes on this disk: the folder of an earlier download, else a new one
	local := prev
	if local == "" || !core.IsDir(local) {
		local, got = archive.UniquePath(filepath.Join(dlRoot, name)), nil
	}
	strip := ""
	if oneDir {
		strip = rawName(items[0]) + "/" // one folder: its contents go straight into the asset's folder
	}
	localOf := func(rel string) string {
		if rel+"/" == strip {
			return local
		}
		segs := strings.Split(strings.TrimPrefix(rel, strip), "/")
		for k := range segs {
			segs[k] = core.SafeName(segs[k], 200)
		}
		return filepath.Join(append([]string{local}, segs...)...)
	}
	setPan(j, func(j *PanJob) { j.Stage, j.Saved = "save", dest })
	msg("正在转存到网盘")
	if err := b.mkdir(panSaveRoot); err != nil {
		return err
	}
	if err := b.mkdir(dest); err != nil {
		return err
	}
	byDir := map[string][]panPick{}
	var dirs []string
	for _, pk := range picks {
		if _, ok := byDir[pk.dir]; !ok {
			dirs = append(dirs, pk.dir)
		}
		byDir[pk.dir] = append(byDir[pk.dir], pk)
	}
	for _, d := range dirs {
		at := dest
		for _, seg := range strings.Split(strings.Trim(d, "/"), "/") {
			if seg != "" { // the folders above a picked file, one level at a time
				at += "/" + seg
				if err := b.mkdir(at); err != nil {
					return err
				}
			}
		}
		replaced, err := b.saveInto(ls, byDir[d], at, 0, msg)
		// what the seller replaced under the same name: the file on this disk is the old one too, whatever its
		// size. It goes at once, so that a job that stops after this does not leave it to pass for downloaded
		for _, p := range replaced {
			old := localOf(strings.TrimPrefix(p, dest+"/"))
			_ = os.Remove(old)
			_ = os.Remove(old + ".part")
		}
		if err != nil {
			return err
		}
	}
	st.Mu.Lock()
	su := st.User[j.Key]
	if su == nil {
		su = &core.UserData{}
		st.User[j.Key] = su
	}
	su.PanCopy, su.PanSaved = dest, mergePanParts(su.PanSaved, pickTrees(picks, whole))
	st.Mu.Unlock()
	_ = st.Save()
	// 2. download the picked parts of the copy
	msg("正在获取网盘文件列表")
	// a picked folder with parts downloaded before (their archives unpacked and gone): those parts are left out;
	// picking a downloaded part itself downloads it again
	again := func(pk panPick, tree string) bool { return !panCovered(tree, got) || panCovered(pk.tree, got) }
	var files []bdFile
	skipped := 0
	listed := map[string][]bdEntry{}
	for _, pk := range picks {
		parent := dest + pk.dir
		ents, ok := listed[parent]
		if !ok {
			if ents, err = b.list(parent); err != nil {
				return err
			}
			listed[parent] = ents
		}
		var e *bdEntry
		for k := range ents {
			if ents[k].Name == rawName(pk.raw) {
				e = &ents[k]
				break
			}
		}
		if e == nil {
			return errors.New("网盘中未找到「" + rawName(pk.raw) + "」（可能已被删除），请重新下载")
		}
		rel := strings.TrimPrefix(pk.dir+"/"+e.Name, "/")
		if e.IsDir.String() != "1" {
			sz, _ := e.Size.Int64()
			files = append(files, bdFile{Path: e.Path, Rel: rel, Size: sz})
			continue
		}
		fs, err := b.tree(e.Path)
		if err != nil {
			return err
		}
		for _, f := range fs {
			if !again(pk, path.Join(pk.tree, f.Rel)) {
				skipped++
				continue
			}
			f.Rel = rel + "/" + f.Rel
			files = append(files, f)
		}
	}
	if len(files) == 0 && skipped == 0 {
		return errors.New("网盘中的文件夹为空")
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	setPan(j, func(j *PanJob) {
		j.Stage, j.Dir, j.Total, j.Done, j.FileN, j.Files = "download", local, total, 0, len(files), 0
	})
	// what is in the folder already counts as done; remember the folder so a retry continues there
	st.Mu.Lock()
	u := st.User[j.Key]
	if u == nil {
		u = &core.UserData{}
		st.User[j.Key] = u
	}
	if u.Downloaded != local { // more of it into the folder downloaded before: that folder's card stays
		u.Downloaded = ""
	}
	if local != prev {
		u.PanGot = nil // a new folder: nothing downloaded into it yet
	}
	u.DownloadDir = local
	st.Mu.Unlock()
	_ = st.Save()
	if err := os.MkdirAll(local, 0755); err != nil {
		return purchases.WriteErr(local, err)
	}
	var need int64
	for _, f := range files {
		if fi, err := os.Stat(localOf(f.Rel)); err != nil || fi.Size() != f.Size {
			need += f.Size
		}
	}
	if err := purchases.CheckSpace(local, need); err != nil {
		return err
	}
	purchases.PinDownloadDir(st, dlRoot)
	var done int64
	winStart, winBytes := time.Now(), int64(0)
	started := time.Now()
	lastShow := time.Time{}
	slowHint := s.VIP != 2
	for i, f := range files {
		dst := localOf(f.Rel)
		setPan(j, func(j *PanJob) { j.File, j.Files = path.Base(f.Rel), i })
		err := b.download(f, dst, func(n int64) {
			done += n
			winBytes += n
			now := time.Now()
			if now.Sub(winStart) >= 2*time.Second {
				speed := int64(float64(winBytes) / now.Sub(winStart).Seconds())
				winStart, winBytes = now, 0
				slow := slowHint && now.Sub(started) > 15*time.Second && speed < 300<<10
				setPan(j, func(j *PanJob) { j.Speed, j.Slow = speed, slow })
			}
			if now.Sub(lastShow) > 500*time.Millisecond {
				lastShow = now
				setPan(j, func(j *PanJob) { j.Done = done })
				if total > 0 {
					TaskPanDL.Set(int(done>>10), int(total>>10), fmt.Sprintf("%s  %s / %s", j.Title, fmtBytes(done), fmtBytes(total)))
				}
			}
		})
		if err != nil {
			return err
		}
	}
	setPan(j, func(j *PanJob) { j.Done, j.Files, j.Speed = total, len(files), 0 })
	// 3. unpack (archives inside archives too); the downloaded archives can go, they are in the netdisk
	var failed, unpacked, from []string
	if extract {
		setPan(j, func(j *PanJob) { j.Stage, j.Msg = "unpack", "正在解压" })
		var remove func([]string) error
		if !keep {
			remove = archive.RemoveFiles
		}
		// only the files this job downloaded (those of an earlier try that stopped are among them): the folder
		// may be the asset's own, with archives the player keeps packed
		var mine []string
		for _, f := range files {
			mine = append(mine, localOf(f.Rel))
		}
		res := archive.UnpackFiles(mine, "", remove, func(n string, i, k int) {
			setPan(j, func(j *PanJob) { j.Msg = "正在解压 " + n })
			TaskPanDL.Set(i, k, j.Title+"：正在解压 "+n)
		})
		for a, e := range res.Failed {
			failed = append(failed, filepath.Base(a)+"："+e)
		}
		unpacked, from = res.Done, res.From
	}
	// a share that is one archive of many products (合集包): the levels of its folder are marked, so that the
	// scan below makes a card for each product instead of one for the lot
	bundle := library.MarkBundles(st, local, library.Unpacked(unpacked, from))
	// 4. into the library: the folder replaces the netdisk card
	st.Mu.Lock()
	if u := st.User[j.Key]; u != nil {
		u.Downloaded, u.DownloadDir = local, ""
		u.PanGot = mergePanParts(u.PanGot, pickTrees(picks, whole))
	}
	inRoots := false
	for _, r := range st.Settings.Roots {
		if core.UnderDir(local, r) {
			inRoots = true
		}
	}
	if !inRoots {
		st.Settings.Roots = append(st.Settings.Roots, filepath.Dir(local))
	}
	auto := st.Settings.AutoBooth
	st.Mu.Unlock()
	_ = st.Save()
	note := fmt.Sprintf("已下载 %d 个文件（%s）", len(files), fmtBytes(total))
	if !whole {
		note = fmt.Sprintf("已下载所选的 %d 项，共 %d 个文件（%s）", len(j.Paths), len(files), fmtBytes(total))
	}
	if skipped > 0 {
		note += fmt.Sprintf("；已跳过此前下载过的 %d 个文件", skipped)
		if len(files) == 0 {
			note = "所选内容此前均已下载"
		}
	}
	if len(failed) > 0 {
		note += "；部分压缩包解压失败"
	}
	said, noImport := bundleEnd(bundle, j.imp != nil)
	note += said
	setPan(j, func(j *PanJob) { j.Stage, j.Msg, j.Failed, j.File = "done", note, failed, "" })
	core.Logf("网盘下载完成 %s → %s", j.Key, local)
	library.StartPipeline(st, true, true, auto, false, nil)
	if j.imp != nil && !noImport {
		req := *j.imp
		req.Key, req.Paths = j.Key, []string{local}
		if !whole { // only what was picked (an archive in it is a folder now)
			req.Paths = nil
			for _, pk := range picks {
				if p := localOf(strings.TrimPrefix(pk.dir+"/"+rawName(pk.raw), "/")); core.StatOK(p) {
					req.Paths = append(req.Paths, p)
				}
			}
			req.Paths = core.UniqStrings(append(req.Paths, unpacked...))
		}
		unity.StartImportWhenFree(st, req)
	}
	return nil
}

// bundleEnd: what a job says at its end when what it downloaded is a collection of products (合集包), and
// whether the import that was to follow is off — a collection is split into cards, never imported whole.
func bundleEnd(b library.BundleInfo, importing bool) (note string, noImport bool) {
	switch {
	case b.Products < 2:
		return "", false
	case len(b.Containers) == 0: // all of it is still packed (automatic unpacking is off, or did not work)
		return "；合集包，拆分后可分别导入", true
	}
	note = fmt.Sprintf("；合集包，已拆分为 %d 个素材", b.Products)
	if importing {
		note += "；合集包不会整包导入，请在各素材卡片上分别导入"
	}
	return note, true
}

// panPick: one part of a netdisk card to save and download.
type panPick struct {
	tree string // where the card's file list shows it ("/" = all of the card)
	raw  netdisk.PanRaw
	dir  string // its folder in the netdisk copy, below the copy's top ("" = the top)
}

func pickTrees(ps []panPick, whole bool) []string {
	if whole {
		return []string{"/"}
	}
	var out []string
	for _, p := range ps {
		out = append(out, p.tree)
	}
	return out
}

// normPanPaths: picked parts of a card's file list, without those inside another picked folder; nil = all of it.
func normPanPaths(ps []string) []string {
	var clean []string
	for _, p := range ps {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		p = path.Clean("/" + p)
		if p == "/" {
			return nil
		}
		clean = append(clean, p)
	}
	sort.Strings(clean)
	var out []string
	for _, p := range clean {
		if !panCovered(p, out) { // a folder sorts before what is in it
			out = append(out, p)
		}
	}
	return out
}

// panCovered: is p (in a card's file list) one of parts, or inside one of them?
func panCovered(p string, parts []string) bool {
	for _, s := range parts {
		if s == "/" || p == s || strings.HasPrefix(p, s+"/") {
			return true
		}
	}
	return false
}

func mergePanParts(a, b []string) []string {
	all := append(append([]string{}, a...), b...)
	for _, p := range all {
		if p == "/" {
			return []string{"/"}
		}
	}
	return normPanPaths(all)
}

// panStale: the copy in the netdisk is not what the share holds under that name now — the seller put another
// file there (another size, or changed after the copy was made), or a folder where a file was.
func panStale(it netdisk.PanRaw, e bdEntry) bool {
	dir := it.IsDir.String() == "1"
	if dir != (e.IsDir.String() == "1") {
		return true
	}
	if dir {
		return false
	}
	a, _ := it.Size.Int64()
	z, _ := e.Size.Int64()
	if a != z {
		return true
	}
	ta, _ := it.Mtime.Int64()
	tz, _ := e.Mtime.Int64()
	return ta > 0 && tz > 0 && ta > tz
}

// saveInto copies share items into the netdisk folder dir, so that the copy ends up as the share is now: what
// is missing is saved; a file that is there under the same name but is not the share's file any more is put
// into the netdisk's recycle bin and saved again (replaced: those paths); a folder that is there is looked
// into, every time — the share may have got more in it since, and an earlier save may have been cut short.
func (b *bdClient) saveInto(ls *shareLister, items []panPick, dir string, depth int, msg func(string)) (replaced []string, err error) {
	have, err := b.list(dir)
	if err != nil {
		return nil, err
	}
	there := map[string]bdEntry{}
	for _, e := range have {
		there[e.Name] = e
	}
	var todo []netdisk.PanRaw
	var old []string
	for _, it := range items {
		n := rawName(it.raw)
		e, ok := there[n]
		switch {
		case !ok:
			todo = append(todo, it.raw)
		case panStale(it.raw, e):
			old = append(old, dir+"/"+n)
			todo = append(todo, it.raw)
		case it.raw.IsDir.String() == "1" && depth < 12:
			if err := b.sleep(150 * time.Millisecond); err != nil { // many folders: not all at once
				return replaced, err
			}
			kids, err := ls.list(it.raw.Path)
			if err != nil {
				return replaced, err
			}
			sub := make([]panPick, 0, len(kids))
			for _, k := range kids {
				sub = append(sub, panPick{tree: path.Join(it.tree, rawName(k)), raw: k})
			}
			r, err := b.saveInto(ls, sub, dir+"/"+n, depth+1, msg)
			replaced = append(replaced, r...)
			if err != nil {
				return replaced, err
			}
		}
	}
	if len(todo) == 0 {
		return replaced, nil
	}
	if len(old) > 0 {
		// Baidu saves next to a file of the same name ("x(1).zip") instead of over it: the old one goes first
		msg(fmt.Sprintf("正在替换网盘中已变更的 %d 项", len(old)))
		replaced = append(replaced, old...) // from here on they may be gone, in part too
		if err := b.remove(old); err != nil {
			return replaced, err
		}
		for i := 0; ; i++ {
			if have, err = b.list(dir); err != nil {
				return replaced, err
			}
			left := 0
			for _, e := range have {
				if core.ContainsStr(old, dir+"/"+e.Name) {
					left++
				}
			}
			if left == 0 {
				break
			}
			if i >= 29 {
				return replaced, errors.New("无法替换网盘中的旧文件，请稍后重新下载")
			}
			if err := b.sleep(panPoll); err != nil {
				return replaced, err
			}
		}
	}
	if err := b.transfer(ls.s, todo, dir, 0); err != nil {
		return replaced, err
	}
	// Baidu may finish the save in the background
	for i := 0; ; i++ {
		if have, err = b.list(dir); err != nil {
			return replaced, err
		}
		there = map[string]bdEntry{}
		for _, e := range have {
			there[e.Name] = e
		}
		missing := 0
		for _, it := range todo {
			if _, ok := there[rawName(it)]; !ok {
				missing++
			}
		}
		if missing == 0 {
			return replaced, nil
		}
		if i >= 149 {
			return replaced, errors.New("网盘转存尚未结束，请稍后重新下载（已转存的文件不会重复转存）")
		}
		msg(fmt.Sprintf("正在转存到网盘（剩余 %d 项）", missing))
		if err := b.sleep(panPoll); err != nil {
			return replaced, err
		}
	}
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
