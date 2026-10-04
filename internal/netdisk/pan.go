package netdisk

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/cloudshare"
	"vrclib/internal/core"
)

var (
	reShareS    = regexp.MustCompile(`(?i)(?:pan|yun)\.baidu\.com/s/([A-Za-z0-9_\-]+)`)
	reShareInit = regexp.MustCompile(`(?i)(?:pan|yun)\.baidu\.com/(?:share/init|wap/init)\?(?:[^#\s]*&)?surl=([A-Za-z0-9_\-]+)`)
	reSharePwd  = regexp.MustCompile(`(?i)[?&]pwd=([A-Za-z0-9]{4})(?:[^A-Za-z0-9]|$)`)
	reShareCode = regexp.MustCompile(`(?:提取码|提取碼|访问码|訪問碼|密码|密碼)\s*[:=]?\s*([A-Za-z0-9]{4})(?:[^A-Za-z0-9]|$)`)
	reShareWord = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(?:pwd|code)\s*[:=]\s*([A-Za-z0-9]{4})(?:[^A-Za-z0-9]|$)`)
	reLocals    = regexp.MustCompile(`(?s)<script id="locals-data" type="application/json">\s*(.*?)\s*</script>`)
)

// halfWidth: links are pasted out of chats, where letters, digits and punctuation often come full-width
// ("ｐｗｄ＝ａｂ１２", "提取码：ab12").
func halfWidth(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		case r == 0x3000:
			return ' '
		}
		return r
	}, s)
}

// ShareSurl returns the id of a share link in its "1xxxx" form ("" when it is not a Baidu share).
func ShareSurl(link string) string {
	link = halfWidth(link)
	if m := reShareS.FindStringSubmatchIndex(link); m != nil {
		surl := link[m[2]:m[3]]
		// "…/s/1AbCd--来自百度网盘超级会员的分享": the dashes belong to the text glued on, not to the id (which may
		// well end in one when nothing of the kind follows)
		if rest := link[m[3]:]; rest != "" && rest[0] >= 0x80 {
			surl = strings.TrimRight(surl, "-_")
		}
		return surl
	}
	if m := reShareInit.FindStringSubmatch(link); m != nil {
		return "1" + m[1]
	}
	return ""
}

// SharePwdFromURL: the share's code, from the link itself ("?pwd=ab12") or from what was pasted along with
// it ("提取码：ab12").
func SharePwdFromURL(link string) string {
	link = halfWidth(link)
	if m := reSharePwd.FindStringSubmatch(link); m != nil {
		return m[1]
	}
	for _, re := range []*regexp.Regexp{reShareCode, reShareWord} {
		if m := re.FindStringSubmatch(link); m != nil {
			return m[1]
		}
	}
	return ""
}

type panClient struct {
	HTTP    *http.Client
	ref     string
	captcha bool // Baidu asked for a captcha: nothing more is asked
}

func NewPanClient(st *core.Store) *panClient {
	jar, _ := cookiejar.New(nil)
	// Baidu is reached directly (it is in China); only a proxy written in the settings is honoured
	tr := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 20 * time.Second, TLSHandshakeTimeout: 15 * time.Second,
		DialContext: (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext, IdleConnTimeout: 60 * time.Second}
	st.Mu.RLock()
	px := strings.TrimSpace(st.Settings.Proxy)
	st.Mu.RUnlock()
	if os.Getenv("VRCLIB_PAN_PROXY") == "1" && px != "" {
		if !strings.Contains(px, "://") {
			px = "http://" + px
		}
		if u, err := url.Parse(px); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &panClient{HTTP: &http.Client{Jar: jar, Transport: tr, Timeout: 30 * time.Second}}
}

func (p *panClient) do(method, u string, body url.Values) ([]byte, *http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(body.Encode())
	}
	req, _ := http.NewRequest(method, u, rd)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if body == nil {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	}
	if p.ref != "" {
		req.Header.Set("Referer", p.ref)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return b, resp, err
}

type PanRaw struct {
	FsID  json.Number `json:"fs_id"`
	Name  string      `json:"server_filename"`
	Path  string      `json:"path"`
	IsDir json.Number `json:"isdir"`
	Size  json.Number `json:"size"`
	Mtime json.Number `json:"server_mtime"`
}

// ErrPanCaptcha: Baidu wants a captcha solved before it answers more. Asking on only makes that last longer.
var ErrPanCaptcha = errors.New("百度要求输入验证码，请稍后重试")

const (
	panDeadMsg    = "分享已失效或被取消"
	panBadCodeMsg = "提取码错误"
	panNoCodeMsg  = "该分享需要提取码"
)

func PanErrno(n int) string {
	switch n {
	case -9, -12:
		return panBadCodeMsg
	case -62, -63:
		return ErrPanCaptcha.Error()
	case 105, -7, 2, 115, 116, 117, 145:
		return panDeadMsg
	}
	return fmt.Sprintf("百度网盘返回错误 %d", n)
}

func panErrnoErr(n int) error {
	if n == -62 || n == -63 {
		return ErrPanCaptcha
	}
	return errors.New(PanErrno(n))
}

// PanErrSettled: an error of a listing that reading the share again will not change — it is gone, or its code
// is missing or wrong (changing the code reads it again by itself).
func PanErrSettled(msg string) bool {
	return msg == panDeadMsg || msg == panBadCodeMsg || msg == panNoCodeMsg || cloudshare.ErrSettled(msg)
}

var reDebugSecret = regexp.MustCompile(`(?i)"([a-z_]*(?:randsk|sekey|token|bduss|sign|username|photo)[a-z_]*|uk)"\s*:\s*(?:"[^"]*"|-?[0-9]+)`)

// RedactDebug: what pan-debug.txt keeps of Baidu's answers, without the share's access key, the account's
// tokens and its name.
func RedactDebug(s string) string { return reDebugSecret.ReplaceAllString(s, `"$1":"…"`) }

// WriteDebug keeps Baidu's unexpected answer for the author to look at (pan-debug.txt in the data folder).
func WriteDebug(text string) {
	_ = os.WriteFile(filepath.Join(core.DataDir, "pan-debug.txt"), []byte(RedactDebug(text)), 0600)
}

// FetchPanListing reads the whole share (folders up to a few levels deep). It tries the share web
// page first, then the API the WeChat mini program uses.
func FetchPanListing(st *core.Store, link, pwd string) (*core.PanListing, error) {
	surl := ShareSurl(link)
	if surl == "" {
		return nil, errors.New("不是百度网盘分享链接")
	}
	if pwd == "" {
		pwd = SharePwdFromURL(link)
	}
	pwd = strings.TrimSpace(pwd)
	var debug strings.Builder
	l, err := fetchPanWeb(st, surl, pwd, &debug)
	if err == nil {
		return l, nil
	}
	if errors.Is(err, ErrPanCaptcha) {
		return nil, err // the other way in would only be one more request Baidu counts
	}
	fmt.Fprintf(&debug, "\n== web: %v\n", err)
	l2, err2 := fetchPanWx(st, surl, pwd, &debug)
	if err2 == nil {
		return l2, nil
	}
	if errors.Is(err2, ErrPanCaptcha) {
		return nil, err2
	}
	fmt.Fprintf(&debug, "\n== wx: %v\n", err2)
	WriteDebug(debug.String())
	if errors.Is(err, errPanDefinite) {
		return nil, errors.Unwrap(err)
	}
	return nil, err
}

var errPanDefinite = errors.New("definite")

type definiteErr struct{ msg string }

func (e definiteErr) Error() string   { return e.msg }
func (e definiteErr) Is(t error) bool { return t == errPanDefinite }
func (e definiteErr) Unwrap() error   { return errors.New(e.msg) }
func panDefinite(msg string) error    { return definiteErr{msg} }

func fetchPanWeb(st *core.Store, surl, pwd string, debug *strings.Builder) (*core.PanListing, error) {
	base := core.PanBase()
	p := NewPanClient(st)
	shareURL := base + "/s/" + surl
	short := strings.TrimPrefix(surl, "1")
	if _, _, err := p.do("GET", shareURL, nil); err != nil {
		return nil, FriendlyPanErr(err)
	}
	if pwd != "" {
		p.ref = base + "/share/init?surl=" + short
		b, _, err := p.do("POST", fmt.Sprintf("%s/share/verify?surl=%s&t=%d&channel=chunlei&web=1&app_id=250528&bdstoken=&clienttype=0",
			base, short, time.Now().UnixMilli()), url.Values{"pwd": {pwd}, "vcode": {""}, "vcode_str": {""}})
		if err != nil {
			return nil, FriendlyPanErr(err)
		}
		fmt.Fprintf(debug, "verify: %s\n", core.Truncate(string(b), 400))
		var v struct {
			Errno  int    `json:"errno"`
			Randsk string `json:"randsk"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(string(b))), &v) != nil {
			return nil, errors.New("百度网盘响应异常，请稍后重试")
		}
		if v.Errno != 0 {
			if v.Errno == -9 || v.Errno == -12 {
				return nil, panDefinite(PanErrno(v.Errno))
			}
			return nil, panErrnoErr(v.Errno)
		}
		if u, err := url.Parse(base + "/"); err == nil && v.Randsk != "" {
			p.HTTP.Jar.SetCookies(u, []*http.Cookie{{Name: "BDCLND", Value: v.Randsk, Path: "/", Domain: u.Hostname()}})
		}
	}
	p.ref = ""
	b, resp, err := p.do("GET", shareURL, nil)
	if err != nil {
		return nil, FriendlyPanErr(err)
	}
	if resp != nil {
		fmt.Fprintf(debug, "page: %d %s (%d bytes)\n", resp.StatusCode, resp.Request.URL, len(b))
	}
	info := ParseSharePage(b)
	if info == nil {
		info = &BDShare{}
	}
	if len(info.FileList) == 0 {
		page := string(b)
		debug.WriteString(core.Truncate(page, 200000))
		switch {
		case resp != nil && strings.Contains(resp.Request.URL.Path, "/share/init"):
			if pwd == "" {
				return nil, panDefinite(panNoCodeMsg)
			}
			return nil, errors.New("提取码验证未通过")
		case strings.Contains(page, "分享的文件已经被删除") || strings.Contains(page, "分享的文件已经被取消") ||
			strings.Contains(page, "链接已过期") || strings.Contains(page, "分享已过期") || strings.Contains(page, "此链接分享内容可能因为"):
			return nil, panDefinite(panDeadMsg)
		}
		return nil, errors.New("无法从网页读取分享内容")
	}
	out := &core.PanListing{Surl: surl, Fetched: time.Now().Unix()}
	p.ref = shareURL
	out.Files = walkPan(out, info.FileList, func(dir string, budget *int) ([]PanRaw, bool) {
		return p.listDir(base, info.ShareUK.String(), info.ShareID.String(), dir, budget)
	})
	if p.captcha {
		return nil, ErrPanCaptcha // half a listing would read as files taken out of the share
	}
	out.Title = strings.TrimSpace(info.Title)
	if out.Title == "" {
		out.Title = panTitle(out.Files)
	}
	return out, nil
}

// fetchPanWx: the API used by Baidu's WeChat mini program (no web page, no cookies needed).
func fetchPanWx(st *core.Store, surl, pwd string, debug *strings.Builder) (*core.PanListing, error) {
	base := core.PanBase()
	p := NewPanClient(st)
	call := func(short string, root bool, dir string, page int) (list []PanRaw, more bool, errno int, err error) {
		form := url.Values{"shorturl": {short}, "pwd": {pwd}, "dir": {dir}, "page": {fmt.Sprint(page)}, "num": {"1000"}, "order": {"time"}}
		if root {
			form.Set("root", "1")
		} else {
			form.Set("root", "0")
		}
		req, _ := http.NewRequest("POST", base+"/share/wxlist?channel=weixin&version=2.2.2&clienttype=25&web=1", strings.NewReader(form.Encode()))
		req.Header.Set("User-Agent", "netdisk")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "ndut_fmt", Value: ""})
		resp, err := p.HTTP.Do(req)
		if err != nil {
			return nil, false, 0, FriendlyPanErr(err)
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		var r struct {
			Errno int `json:"errno"`
			Data  struct {
				List    []PanRaw `json:"list"`
				HasMore bool     `json:"has_more"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &r) != nil {
			fmt.Fprintf(debug, "wxlist(%s): %s\n", short, core.Truncate(string(b), 300))
			return nil, false, 0, errors.New("百度网盘响应异常")
		}
		if r.Errno != 0 {
			fmt.Fprintf(debug, "wxlist(%s): %s\n", short, core.Truncate(string(b), 300))
		}
		return r.Data.List, r.Data.HasMore, r.Errno, nil
	}
	short := surl
	list, _, errno, err := call(short, true, "", 1)
	if err == nil && errno != 0 && errno != -62 && errno != -63 && strings.HasPrefix(surl, "1") {
		short = strings.TrimPrefix(surl, "1")
		list, _, errno, err = call(short, true, "", 1)
	}
	if err != nil {
		return nil, err
	}
	if errno != 0 {
		return nil, panErrnoErr(errno)
	}
	out := &core.PanListing{Surl: surl, Fetched: time.Now().Unix()}
	out.Files = walkPan(out, list, func(dir string, budget *int) ([]PanRaw, bool) {
		var all []PanRaw
		for page := 1; page <= 10 && !p.captcha; page++ {
			time.Sleep(250 * time.Millisecond)
			l, more, errno, err := call(short, false, dir, page)
			if err != nil || errno != 0 {
				p.captcha = p.captcha || errno == -62 || errno == -63
				return all, true
			}
			all = append(all, l...)
			if !more || len(all) >= *budget {
				return all, more
			}
		}
		return all, true
	})
	if p.captcha {
		return nil, ErrPanCaptcha
	}
	out.Title = panTitle(out.Files)
	return out, nil
}

func walkPan(out *core.PanListing, root []PanRaw, list func(dir string, budget *int) ([]PanRaw, bool)) []*core.PanFile {
	budget := 1500
	var walk func(items []PanRaw, depth int) []*core.PanFile
	walk = func(items []PanRaw, depth int) []*core.PanFile {
		var files []*core.PanFile
		for _, it := range items {
			if budget <= 0 {
				out.Truncated = true
				break
			}
			budget--
			size, _ := it.Size.Int64()
			f := &core.PanFile{Name: html.UnescapeString(it.Name), Size: size, Dir: it.IsDir.String() == "1"}
			if f.Dir {
				if depth >= 6 {
					f.Partial = true
				} else {
					kids, partial := list(it.Path, &budget)
					f.Children = walk(kids, depth+1)
					f.Partial = partial
					if partial {
						out.Truncated = true
					}
				}
			} else {
				out.Count++
				out.Size += size
			}
			files = append(files, f)
		}
		sortPan(files)
		return files
	}
	return walk(root, 0)
}

func (p *panClient) listDir(base, uk, shareid, dir string, budget *int) ([]PanRaw, bool) {
	var all []PanRaw
	for page := 1; page <= 20 && !p.captcha; page++ {
		time.Sleep(250 * time.Millisecond)
		u := fmt.Sprintf("%s/share/list?uk=%s&shareid=%s&order=other&desc=1&showempty=0&web=1&page=%d&num=100&dir=%s&channel=chunlei&app_id=250528&clienttype=0",
			base, uk, shareid, page, url.QueryEscape(dir))
		b, _, err := p.do("GET", u, nil)
		if err != nil {
			return all, true
		}
		var r struct {
			Errno int      `json:"errno"`
			List  []PanRaw `json:"list"`
		}
		if json.Unmarshal(b, &r) != nil || r.Errno != 0 {
			p.captcha = p.captcha || r.Errno == -62 || r.Errno == -63
			return all, true
		}
		all = append(all, r.List...)
		if len(r.List) < 100 || len(all) >= *budget {
			return all, len(r.List) == 100
		}
	}
	return all, true
}

var rePanAux = regexp.MustCompile(`(?i)texture|tex([^a-z]|$)|psd|material|manual|readme|説明|说明|bonus|特典|sample|贴图|材质`)

// panTitle names a share after its only item, or its biggest main item (textures, PSDs and
// readmes are only used when there is nothing else).
func panTitle(fs []*core.PanFile) string {
	var main []*core.PanFile
	for _, f := range fs {
		if !rePanAux.MatchString(f.Name) {
			main = append(main, f)
		}
	}
	if len(main) == 0 {
		main = fs
	}
	var best *core.PanFile
	var bestSize int64 = -1
	for _, f := range main {
		sz := f.Size
		if f.Dir {
			sz = panTreeSize(f) + 1
		}
		if sz > bestSize {
			best, bestSize = f, sz
		}
	}
	if best == nil {
		return ""
	}
	if best.Dir {
		return best.Name
	}
	return core.StripArchiveExt(best.Name)
}

func panTreeSize(f *core.PanFile) int64 {
	n := f.Size
	for _, c := range f.Children {
		n += panTreeSize(c)
	}
	return n
}

func sortPan(fs []*core.PanFile) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Dir != fs[j].Dir {
			return fs[i].Dir
		}
		return strings.ToLower(fs[i].Name) < strings.ToLower(fs[j].Name)
	})
}

func FriendlyPanErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err // without the address
	}
	s := err.Error()
	if strings.Contains(s, "timeout") || strings.Contains(s, "deadline") || strings.Contains(s, "refused") || strings.Contains(s, "no such host") {
		return errors.New("无法连接百度网盘，请检查网络")
	}
	return err
}

// ---------- background refresh ----------

var (
	TaskPan   = &core.Task{Name: "pan", Label: "获取网盘分享"}
	PanMu     sync.Mutex
	PanQueue  []string // asset keys
	PanActive bool
)
