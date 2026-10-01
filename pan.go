package main

// Reads what is inside a Baidu Netdisk share link (the same requests the share web page makes;
// no login needed). Listings are cached in library.json.

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
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
)

type PanFile struct {
	Name     string     `json:"n"`
	Size     int64      `json:"s,omitempty"`
	Dir      bool       `json:"d,omitempty"`
	Children []*PanFile `json:"c,omitempty"`
	Partial  bool       `json:"p,omitempty"` // folder not fully listed (limits)
}

type PanListing struct {
	Surl      string     `json:"surl"`
	Title     string     `json:"title"`
	Files     []*PanFile `json:"files"`
	Count     int        `json:"count"`
	Size      int64      `json:"size"`
	Fetched   int64      `json:"fetched"`
	Err       string     `json:"err,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
	// what changed at the last re-read that found a difference
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed int64    `json:"changed,omitempty"`
}

var (
	reShareS    = regexp.MustCompile(`pan\.baidu\.com/s/([A-Za-z0-9_\-]+)`)
	reShareInit = regexp.MustCompile(`pan\.baidu\.com/(?:share/init|wap/init)\?(?:[^#\s]*&)?surl=([A-Za-z0-9_\-]+)`)
	reSharePwd  = regexp.MustCompile(`[?&]pwd=([A-Za-z0-9]{4})`)
	reLocals    = regexp.MustCompile(`(?s)<script id="locals-data" type="application/json">\s*(.*?)\s*</script>`)
)

// shareSurl returns the id of a share link in its "1xxxx" form ("" when it is not a Baidu share).
func shareSurl(link string) string {
	if m := reShareS.FindStringSubmatch(link); m != nil {
		return m[1]
	}
	if m := reShareInit.FindStringSubmatch(link); m != nil {
		return "1" + m[1]
	}
	return ""
}

func sharePwdFromURL(link string) string {
	if m := reSharePwd.FindStringSubmatch(link); m != nil {
		return m[1]
	}
	return ""
}

type panClient struct {
	c   *http.Client
	ref string
}

func newPanClient(st *Store) *panClient {
	jar, _ := cookiejar.New(nil)
	// Baidu is reached directly (it is in China); only a proxy written in the settings is honoured
	tr := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 20 * time.Second}
	st.mu.RLock()
	px := strings.TrimSpace(st.Settings.Proxy)
	st.mu.RUnlock()
	if os.Getenv("VRCLIB_PAN_PROXY") == "1" && px != "" {
		if !strings.Contains(px, "://") {
			px = "http://" + px
		}
		if u, err := url.Parse(px); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &panClient{c: &http.Client{Jar: jar, Transport: tr, Timeout: 30 * time.Second}}
}

func panBase() string {
	if v := os.Getenv("VRCLIB_PAN_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://pan.baidu.com"
}

func (p *panClient) do(method, u string, body url.Values) ([]byte, *http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(body.Encode())
	}
	req, _ := http.NewRequest(method, u, rd)
	req.Header.Set("User-Agent", ua)
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
	resp, err := p.c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return b, resp, err
}

type panRaw struct {
	Name  string      `json:"server_filename"`
	Path  string      `json:"path"`
	IsDir json.Number `json:"isdir"`
	Size  json.Number `json:"size"`
}

func panErrno(n int) string {
	switch n {
	case -9, -12:
		return "提取码不对"
	case -62, -63:
		return "百度要求输入验证码，过一会儿再试"
	case 105, -7, 2, 115, 116, 117, 145:
		return "分享已失效或被取消"
	}
	return fmt.Sprintf("百度网盘返回错误 %d", n)
}

// FetchPanListing reads the whole share (folders up to a few levels deep). It tries the share web
// page first, then the API the WeChat mini program uses.
func FetchPanListing(st *Store, link, pwd string) (*PanListing, error) {
	surl := shareSurl(link)
	if surl == "" {
		return nil, errors.New("不是百度网盘分享链接")
	}
	if pwd == "" {
		pwd = sharePwdFromURL(link)
	}
	pwd = strings.TrimSpace(pwd)
	var debug strings.Builder
	l, err := fetchPanWeb(st, surl, pwd, &debug)
	if err == nil {
		return l, nil
	}
	fmt.Fprintf(&debug, "\n== web: %v\n", err)
	l2, err2 := fetchPanWx(st, surl, pwd, &debug)
	if err2 == nil {
		return l2, nil
	}
	fmt.Fprintf(&debug, "\n== wx: %v\n", err2)
	_ = os.WriteFile(filepath.Join(dataDir, "pan-debug.txt"), []byte(debug.String()), 0644)
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

func fetchPanWeb(st *Store, surl, pwd string, debug *strings.Builder) (*PanListing, error) {
	base := panBase()
	p := newPanClient(st)
	shareURL := base + "/s/" + surl
	short := strings.TrimPrefix(surl, "1")
	if _, _, err := p.do("GET", shareURL, nil); err != nil {
		return nil, friendlyPanErr(err)
	}
	if pwd != "" {
		p.ref = base + "/share/init?surl=" + short
		b, _, err := p.do("POST", fmt.Sprintf("%s/share/verify?surl=%s&t=%d&channel=chunlei&web=1&app_id=250528&bdstoken=&clienttype=0",
			base, short, time.Now().UnixMilli()), url.Values{"pwd": {pwd}, "vcode": {""}, "vcode_str": {""}})
		if err != nil {
			return nil, friendlyPanErr(err)
		}
		fmt.Fprintf(debug, "verify: %s\n", truncate(string(b), 400))
		var v struct {
			Errno  int    `json:"errno"`
			Randsk string `json:"randsk"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(string(b))), &v) != nil {
			return nil, errors.New("百度网盘没有正常响应，稍后再试")
		}
		if v.Errno != 0 {
			if v.Errno == -9 || v.Errno == -12 {
				return nil, panDefinite(panErrno(v.Errno))
			}
			return nil, errors.New(panErrno(v.Errno))
		}
		if u, err := url.Parse(base + "/"); err == nil && v.Randsk != "" {
			p.c.Jar.SetCookies(u, []*http.Cookie{{Name: "BDCLND", Value: v.Randsk, Path: "/", Domain: u.Hostname()}})
		}
	}
	p.ref = ""
	b, resp, err := p.do("GET", shareURL, nil)
	if err != nil {
		return nil, friendlyPanErr(err)
	}
	if resp != nil {
		fmt.Fprintf(debug, "page: %d %s (%d bytes)\n", resp.StatusCode, resp.Request.URL, len(b))
	}
	m := reLocals.FindSubmatch(b)
	var info struct {
		ShareUK  json.Number `json:"share_uk"`
		ShareID  json.Number `json:"shareid"`
		FileList []panRaw    `json:"file_list"`
		Title    string      `json:"title"`
	}
	if m != nil {
		_ = json.Unmarshal(m[1], &info)
	}
	if len(info.FileList) == 0 {
		page := string(b)
		debug.WriteString(truncate(page, 200000))
		switch {
		case resp != nil && strings.Contains(resp.Request.URL.Path, "/share/init"):
			if pwd == "" {
				return nil, panDefinite("这个分享需要提取码")
			}
			return nil, errors.New("提取码验证没通过")
		case strings.Contains(page, "分享的文件已经被删除") || strings.Contains(page, "分享的文件已经被取消") ||
			strings.Contains(page, "链接已过期") || strings.Contains(page, "分享已过期") || strings.Contains(page, "此链接分享内容可能因为"):
			return nil, panDefinite("分享已失效或被取消")
		}
		return nil, errors.New("网页里没读到分享内容")
	}
	out := &PanListing{Surl: surl, Fetched: time.Now().Unix()}
	p.ref = shareURL
	out.Files = walkPan(out, info.FileList, func(dir string, budget *int) ([]panRaw, bool) {
		return p.listDir(base, info.ShareUK.String(), info.ShareID.String(), dir, budget)
	})
	out.Title = strings.TrimSpace(info.Title)
	if out.Title == "" {
		out.Title = panTitle(out.Files)
	}
	return out, nil
}

// fetchPanWx: the API used by Baidu's WeChat mini program (no web page, no cookies needed).
func fetchPanWx(st *Store, surl, pwd string, debug *strings.Builder) (*PanListing, error) {
	base := panBase()
	p := newPanClient(st)
	call := func(short string, root bool, dir string, page int) (list []panRaw, more bool, errno int, err error) {
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
		resp, err := p.c.Do(req)
		if err != nil {
			return nil, false, 0, friendlyPanErr(err)
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		var r struct {
			Errno int `json:"errno"`
			Data  struct {
				List    []panRaw `json:"list"`
				HasMore bool     `json:"has_more"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &r) != nil {
			fmt.Fprintf(debug, "wxlist(%s): %s\n", short, truncate(string(b), 300))
			return nil, false, 0, errors.New("百度网盘没有正常响应")
		}
		if r.Errno != 0 {
			fmt.Fprintf(debug, "wxlist(%s): %s\n", short, truncate(string(b), 300))
		}
		return r.Data.List, r.Data.HasMore, r.Errno, nil
	}
	short := surl
	list, _, errno, err := call(short, true, "", 1)
	if err == nil && errno != 0 && strings.HasPrefix(surl, "1") {
		short = strings.TrimPrefix(surl, "1")
		list, _, errno, err = call(short, true, "", 1)
	}
	if err != nil {
		return nil, err
	}
	if errno != 0 {
		return nil, errors.New(panErrno(errno))
	}
	out := &PanListing{Surl: surl, Fetched: time.Now().Unix()}
	out.Files = walkPan(out, list, func(dir string, budget *int) ([]panRaw, bool) {
		var all []panRaw
		for page := 1; page <= 10; page++ {
			time.Sleep(250 * time.Millisecond)
			l, more, errno, err := call(short, false, dir, page)
			if err != nil || errno != 0 {
				return all, true
			}
			all = append(all, l...)
			if !more || len(all) >= *budget {
				return all, more
			}
		}
		return all, true
	})
	out.Title = panTitle(out.Files)
	return out, nil
}

func walkPan(out *PanListing, root []panRaw, list func(dir string, budget *int) ([]panRaw, bool)) []*PanFile {
	budget := 1500
	var walk func(items []panRaw, depth int) []*PanFile
	walk = func(items []panRaw, depth int) []*PanFile {
		var files []*PanFile
		for _, it := range items {
			if budget <= 0 {
				out.Truncated = true
				break
			}
			budget--
			size, _ := it.Size.Int64()
			f := &PanFile{Name: html.UnescapeString(it.Name), Size: size, Dir: it.IsDir.String() == "1"}
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

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (p *panClient) listDir(base, uk, shareid, dir string, budget *int) ([]panRaw, bool) {
	var all []panRaw
	for page := 1; page <= 20; page++ {
		time.Sleep(250 * time.Millisecond)
		u := fmt.Sprintf("%s/share/list?uk=%s&shareid=%s&order=other&desc=1&showempty=0&web=1&page=%d&num=100&dir=%s&channel=chunlei&app_id=250528&clienttype=0",
			base, uk, shareid, page, url.QueryEscape(dir))
		b, _, err := p.do("GET", u, nil)
		if err != nil {
			return all, true
		}
		var r struct {
			Errno int      `json:"errno"`
			List  []panRaw `json:"list"`
		}
		if json.Unmarshal(b, &r) != nil || r.Errno != 0 {
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
func panTitle(fs []*PanFile) string {
	var main []*PanFile
	for _, f := range fs {
		if !rePanAux.MatchString(f.Name) {
			main = append(main, f)
		}
	}
	if len(main) == 0 {
		main = fs
	}
	var best *PanFile
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
	return stripArchiveExt(best.Name)
}

func panTreeSize(f *PanFile) int64 {
	n := f.Size
	for _, c := range f.Children {
		n += panTreeSize(c)
	}
	return n
}

func sortPan(fs []*PanFile) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Dir != fs[j].Dir {
			return fs[i].Dir
		}
		return strings.ToLower(fs[i].Name) < strings.ToLower(fs[j].Name)
	})
}

func friendlyPanErr(err error) error {
	s := err.Error()
	if strings.Contains(s, "timeout") || strings.Contains(s, "deadline") || strings.Contains(s, "refused") || strings.Contains(s, "no such host") {
		return errors.New("连不上百度网盘，检查一下网络")
	}
	return err
}

// ---------- background refresh ----------

var (
	taskPan   = &Task{Name: "pan", Label: "读取网盘分享"}
	panMu     sync.Mutex
	panQueue  []string // asset keys
	panActive bool
)

// QueuePanFetch reads the share links of the given assets in the background (one at a time).
func QueuePanFetch(st *Store, keys ...string) {
	panMu.Lock()
	for _, k := range keys {
		if !containsStr(panQueue, k) {
			panQueue = append(panQueue, k)
		}
	}
	if panActive {
		panMu.Unlock()
		return
	}
	panActive = true
	panMu.Unlock()
	go func() {
		defer func() {
			panMu.Lock()
			panActive = false
			panMu.Unlock()
		}()
		run(taskPan, func() {
			done := 0
			for {
				panMu.Lock()
				if len(panQueue) == 0 {
					panActive = false
					panMu.Unlock()
					// new share titles: look them up on Booth (covers) and translate them
					st.mu.RLock()
					auto := st.Settings.AutoBooth
					st.mu.RUnlock()
					if !auto || !StartPipeline(st, false, false, true, false, nil) {
						KickTranslate(st)
					}
					return
				}
				k := panQueue[0]
				panQueue = panQueue[1:]
				left := len(panQueue)
				panMu.Unlock()
				st.mu.RLock()
				u := st.User[k]
				var link, pwd string
				if u != nil {
					link, pwd = u.ShareURL, u.SharePwd
				}
				st.mu.RUnlock()
				surl := shareSurl(link)
				if surl == "" {
					continue
				}
				taskPan.Set(done, done+left+1, "读取网盘分享")
				l, err := FetchPanListing(st, link, pwd)
				st.mu.Lock()
				if err != nil {
					old := st.Pan[surl]
					if old == nil {
						old = &PanListing{Surl: surl}
						st.Pan[surl] = old
					}
					old.Err, old.Fetched = err.Error(), time.Now().Unix()
				} else {
					prev := st.Pan[surl]
					diffPan(prev, l)
					st.Pan[surl] = l
					// products of a collection seen for the first time; ones that were already in
					// the share (read by an older version) are as old as the share
					now, had := time.Now().Unix(), panFileMap(prev)
					for _, it := range splitPan(l) {
						ik := panItemKey(surl, it.Path)
						if st.FirstSeen[ik] != 0 {
							continue
						}
						st.FirstSeen[ik] = now
						if panHasPath(had, it.Path) {
							st.FirstSeen[ik] = max(st.FirstSeen["pan:"+surl], 1)
						}
					}
				}
				st.mu.Unlock()
				done++
				_ = st.Save()
				bumpRev()
			}
		})
	}()
}
