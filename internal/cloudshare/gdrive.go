package cloudshare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vrclib/internal/core"
)

// GDriveAPIKey: a Google API key restricted to the Drive API, set when the program is built:
//
//	go build -ldflags "-X vrclib/internal/cloudshare.GDriveAPIKey=AIza…"
//
// With it, shares are read through the Drive API (every file with its size, folders in full). Without it —
// the default — the share pages are read, which tells the names but not the sizes of a folder's files, and
// large files go through Google's "can't scan for viruses" page. A key in a public program is not a secret:
// Google permits keys that only identify the project, and the key is to be restricted to the Drive API
// with a quota of its own.
var GDriveAPIKey = ""

func gdriveKey() string {
	if v := os.Getenv("VRCLIB_GDRIVE_KEY"); v != "" {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(GDriveAPIKey)
}

func gdriveWeb() string {
	if v := os.Getenv("VRCLIB_GDRIVE_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://drive.google.com"
}

func gdriveAPI() string {
	if v := os.Getenv("VRCLIB_GDRIVE_API"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://www.googleapis.com"
}

// gdFolderMime: what the API calls a folder; the other google-apps types (Docs, Sheets, shortcuts) are not
// files that can be fetched.
const gdFolderMime = "application/vnd.google-apps.folder"

// gdGap: between two folder listings of one share (tests shorten it).
var gdGap = 250 * time.Millisecond

// gdFile: a file or folder as the API, or the share page, describes it.
type gdFile struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	MimeType string      `json:"mimeType"`
	Size     json.Number `json:"size"`
	RKey     string      `json:"resourceKey"` // its own resourcekey, when it has one (items shared before 2021)
}

func (f gdFile) folder() bool { return f.MimeType == gdFolderMime }

// ---------- the Drive API (with a key) ----------

// gdAPIGet asks the API; the answer's body, or the error it describes.
func gdAPIGet(ctx context.Context, c *http.Client, key, p string, q url.Values, rkeys string) ([]byte, error) {
	q.Set("key", key)
	q.Set("supportsAllDrives", "true")
	req := newRequest(ctx, gdriveAPI()+"/drive/v3/"+p+"?"+q.Encode())
	if rkeys != "" {
		req.Header.Set("X-Goog-Drive-Resource-Keys", rkeys)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, netErr(GDrive, err)
	}
	defer resp.Body.Close()
	body := readPage(resp)
	if resp.StatusCode != http.StatusOK {
		return nil, gdAPIErr(resp.StatusCode, body)
	}
	return []byte(body), nil
}

// gdAPIErr: what an API error means, by the reason Google gives
// ({"error":{"code":403,"errors":[{"reason":"downloadQuotaExceeded"}]}}).
func gdAPIErr(status int, body string) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &e)
	reason := ""
	if len(e.Error.Errors) > 0 {
		reason = e.Error.Errors[0].Reason
	}
	switch {
	case status == http.StatusNotFound || reason == "notFound":
		return errGDGone
	case reason == "downloadQuotaExceeded":
		return ErrGDQuota
	case reason == "cannotDownloadAbusiveFile":
		return errGDAbusive
	case reason == "rateLimitExceeded" || reason == "userRateLimitExceeded" || status == http.StatusTooManyRequests || status >= 500:
		return tempErr{fmt.Errorf("Google Drive 暂时无法响应（%s），请稍后重试", gdDetail(status, reason))}
	case status == http.StatusUnauthorized || status == http.StatusBadRequest ||
		reason == "keyInvalid" || reason == "keyExpired" || reason == "accessNotConfigured" || reason == "ipRefererBlocked" ||
		strings.HasPrefix(reason, "dailyLimitExceeded") || strings.Contains(e.Error.Message, "API key"):
		return fmt.Errorf("%w（%s）", errAPIKeyUnusable, gdDetail(status, reason))
	case status == http.StatusForbidden:
		return errGDDenied
	}
	return fmt.Errorf("Google Drive 返回错误（%s）", gdDetail(status, reason))
}

// gdDetail: "HTTP 403 downloadQuotaExceeded", for the player and the log.
func gdDetail(status int, reason string) string {
	if reason == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Sprintf("HTTP %d %s", status, reason)
}

func gdAPIFile(ctx context.Context, c *http.Client, key, id, rkey string) (gdFile, error) {
	b, err := gdAPIGet(ctx, c, key, "files/"+url.PathEscape(id), url.Values{"fields": {"id,name,mimeType,size,resourceKey"}}, gdResourceKeys(id, rkey))
	if err != nil {
		return gdFile{}, err
	}
	var f gdFile
	if json.Unmarshal(b, &f) != nil || f.ID == "" {
		return gdFile{}, errors.New("Google Drive 响应异常")
	}
	return f, nil
}

// gdAPIList: what a folder holds, page by page.
func gdAPIList(ctx context.Context, c *http.Client, key, folder, rkey string) ([]gdFile, error) {
	var all []gdFile
	token := ""
	for page := 0; page < 20; page++ {
		q := url.Values{"q": {fmt.Sprintf("'%s' in parents and trashed = false", folder)}, "fields": {"nextPageToken,files(id,name,mimeType,size,resourceKey)"},
			"pageSize": {"1000"}, "includeItemsFromAllDrives": {"true"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		b, err := gdAPIGet(ctx, c, key, "files", q, gdResourceKeys(folder, rkey))
		if err != nil {
			return nil, err
		}
		var r struct {
			Files []gdFile `json:"files"`
			Next  string   `json:"nextPageToken"`
		}
		if json.Unmarshal(b, &r) != nil {
			return nil, errors.New("Google Drive 响应异常")
		}
		all = append(all, r.Files...)
		if r.Next == "" || len(all) >= 2000 {
			return all, nil
		}
		token = r.Next
	}
	return all, nil
}

// gdResourceKeys: the header some link-shared files need (the resourcekey of the link).
func gdResourceKeys(id, rkey string) string {
	if rkey == "" {
		return ""
	}
	return id + "/" + rkey
}

// gdPause: the gap between two folder listings; a cancelled read does not sit it out.
func gdPause(ctx context.Context) error {
	if gdGap <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(gdGap):
		return nil
	}
}

// gdKey: the resourcekey an entry of a folder is asked with — its own when it has one, else the share link's.
func gdKey(own, link string) string {
	if own != "" {
		return own
	}
	return link
}

func gdAPIListing(ctx context.Context, st *core.Store, l *Link, key string) (*core.PanListing, error) {
	c := Client(st)
	defer c.CloseIdleConnections()
	root, err := gdAPIFile(ctx, c, key, l.ID, l.RKey)
	if err != nil {
		return nil, err
	}
	out := &core.PanListing{Surl: l.Key(), Fetched: time.Now().Unix()}
	if !root.folder() {
		if !gdFetchable(root) {
			return nil, errors.New("该链接指向的是 Google 文档，不是文件")
		}
		out.Files = []*core.PanFile{gdPanFile(root)}
		out.Count, out.Size, out.Title = 1, out.Files[0].Size, core.StripArchiveExt(root.Name)
		return out, nil
	}
	kids, err := gdAPIList(ctx, c, key, root.ID, l.RKey)
	if err != nil {
		return nil, err
	}
	if out.Files, err = gdWalk(out, kids, func(k gdFile) ([]gdFile, error) {
		if err := gdPause(ctx); err != nil {
			return nil, err
		}
		return gdAPIList(ctx, c, key, k.ID, gdKey(k.RKey, l.RKey))
	}); err != nil {
		return nil, err
	}
	out.Title = root.Name
	return out, nil
}

// gdFetchable: a real file (Docs, Sheets, shortcuts and the like have no bytes to fetch).
func gdFetchable(f gdFile) bool {
	return !strings.HasPrefix(f.MimeType, "application/vnd.google-apps.")
}

func gdPanFile(f gdFile) *core.PanFile {
	size, _ := f.Size.Int64()
	return &core.PanFile{Name: f.Name, Size: size, Dir: f.folder(), Ref: f.ID, RKey: f.RKey}
}

// gdWalk lists a folder tree (a few levels deep, so many files) the way netdisk lists a share's. A folder
// beyond the limits (too deep, too many entries) is kept, marked as not fully listed. A sub-folder that could
// not be read ends the walk with its error: half a listing kept as the share's would read as files removed
// now and added again at the next read, and a download of it would call itself complete. Only a sub-folder
// that is itself gone or closed to the public stays, marked: reading again does not change that.
func gdWalk(out *core.PanListing, root []gdFile, list func(k gdFile) ([]gdFile, error)) ([]*core.PanFile, error) {
	budget := 1500
	var walk func(kids []gdFile, depth int) ([]*core.PanFile, bool, error)
	walk = func(kids []gdFile, depth int) ([]*core.PanFile, bool, error) {
		var files []*core.PanFile
		cut := false
		for _, k := range kids {
			if !k.folder() && !gdFetchable(k) {
				continue
			}
			if budget <= 0 {
				out.Truncated, cut = true, true // the folder these are the entries of is not listed in full
				break
			}
			budget--
			f := gdPanFile(k)
			if f.Dir {
				var sub []gdFile
				var err error
				if depth < 6 {
					sub, err = list(k)
				}
				switch {
				case depth >= 6 || errors.Is(err, errGDGone) || errors.Is(err, errGDDenied):
					f.Partial = true
				case err != nil:
					return nil, false, err
				default:
					if f.Children, f.Partial, err = walk(sub, depth+1); err != nil {
						return nil, false, err
					}
				}
				if f.Partial {
					out.Truncated = true
				}
			} else {
				out.Count++
				out.Size += f.Size
			}
			files = append(files, f)
		}
		sortFiles(files)
		return files, cut, nil
	}
	files, _, err := walk(root, 0)
	return files, err
}

// ---------- the share pages (no key) ----------

var (
	reGDTitle    = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	reGDAnchor   = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	reGDEntry    = regexp.MustCompile(`(?is)class="flip-entry-title"[^>]*>(.*?)<`)
	reGDHrefID   = regexp.MustCompile(`drive\.google\.com/file/d/([A-Za-z0-9_-]{10,})`)
	reGDHrefDir  = regexp.MustCompile(`drive\.google\.com/drive/folders/([A-Za-z0-9_-]{10,})`)
	reTags       = regexp.MustCompile(`(?s)<[^>]*>`)
	reGDForm     = regexp.MustCompile(`(?is)<form[^>]*id="download-form"[^>]*>(.*?)</form>`)
	reGDAction   = regexp.MustCompile(`(?i)action="([^"]+)"`)
	reGDInput    = regexp.MustCompile(`(?is)<input[^>]*>`)
	reAttrName   = regexp.MustCompile(`(?i)\bname="([^"]*)"`)
	reAttrValue  = regexp.MustCompile(`(?i)\bvalue="([^"]*)"`)
	reAttrType   = regexp.MustCompile(`(?i)\btype="([^"]*)"`)
	reGDHref     = regexp.MustCompile(`href="(/uc\?export=download[^"]+)"`)
	reGDDLURL    = regexp.MustCompile(`"downloadUrl":"([^"]+)"`)
	reGDErr      = regexp.MustCompile(`(?is)<p class="uc-error-subcaption">(.*?)</p>`)
	reGDNameSz   = regexp.MustCompile(`(?is)class="uc-name-size"[^>]*>\s*<a[^>]*>(.*?)</a>\s*\(([^)]*)\)`)
	reGDOGTitle  = regexp.MustCompile(`(?i)<meta\s+property="og:title"\s+content="([^"]*)"`)
	reDebugToken = regexp.MustCompile(`(?i)(uuid=|"uuid"\s*:\s*"|name="uuid"\s+value=")[A-Za-z0-9_-]+`)
)

func stripTags(s string) string {
	return strings.TrimSpace(html.UnescapeString(reTags.ReplaceAllString(s, "")))
}

// gdPageTitle: "<name> - Google Drive" → name.
func gdPageTitle(page string) string {
	m := reGDTitle.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	t := stripTags(m[1])
	return strings.TrimSpace(strings.TrimSuffix(t, "- Google Drive"))
}

// gdEmbeddedFolder reads a folder's entries from the embeddable folder view, which lists a shared folder
// without a login: the folder's name, and a link with the name of each file and sub-folder (no sizes).
func gdEmbeddedFolder(ctx context.Context, c *http.Client, id, rkey string) (string, []gdFile, error) {
	u := gdriveWeb() + "/embeddedfolderview?id=" + url.QueryEscape(id)
	if rkey != "" {
		u += "&resourcekey=" + url.QueryEscape(rkey)
	}
	resp, err := c.Do(newRequest(ctx, u))
	if err != nil {
		return "", nil, netErr(GDrive, err)
	}
	defer resp.Body.Close()
	page := readPage(resp)
	if err := gdPageStatus(resp); err != nil {
		return "", nil, err
	}
	var kids []gdFile
	for _, m := range reGDAnchor.FindAllStringSubmatch(page, -1) {
		href, body := html.UnescapeString(m[1]), m[2]
		name := ""
		if t := reGDEntry.FindStringSubmatch(body); t != nil {
			name = stripTags(t[1])
		} else {
			name = stripTags(body)
		}
		if name == "" {
			continue
		}
		own := "" // an entry shared before 2021 carries its own resourcekey in its link
		if k := reGDRKey.FindStringSubmatch(href); k != nil {
			own = k[1]
		}
		switch {
		case reGDHrefID.MatchString(href):
			kids = append(kids, gdFile{ID: reGDHrefID.FindStringSubmatch(href)[1], Name: name, RKey: own})
		case reGDHrefDir.MatchString(href):
			kids = append(kids, gdFile{ID: reGDHrefDir.FindStringSubmatch(href)[1], Name: name, MimeType: gdFolderMime, RKey: own})
		}
	}
	title := gdPageTitle(page)
	if len(kids) == 0 && gdNoAccess(title, page) {
		return "", nil, errGDDenied // a notice where the entries should be: not an empty folder of that name
	}
	if len(kids) == 0 && title == "" {
		if strings.Contains(resp.Request.URL.String(), "accounts.google.com") {
			return "", nil, errGDDenied
		}
		WriteDebug("embeddedfolderview "+id, page)
		return "", nil, errors.New("无法从 Google Drive 页面读取文件夹内容（分享可能未公开，或页面格式已变化）")
	}
	return title, kids, nil
}

// gdNoAccess: a page that says the folder is not open to everyone (answered with 200, in the place of the
// folder's entries): Google's sign-in page, or its "You need access" notice.
func gdNoAccess(title, page string) bool {
	t := strings.ToLower(title)
	if strings.Contains(t, "sign in") || strings.Contains(t, "sign-in") || t == "access denied" || strings.HasPrefix(t, "error 40") {
		return true
	}
	lower := strings.ToLower(page)
	for _, s := range []string{"you need access", "you need permission", "request access", "accounts.google.com/servicelogin", "accounts.google.com/v3/signin"} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// gdPageStatus: what an answer's status says.
func gdPageStatus(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errGDGone
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return errGDDenied
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return tempErr{fmt.Errorf("Google Drive 暂时无法响应（HTTP %d），请稍后重试", resp.StatusCode)}
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		return errRange
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent:
		return fmt.Errorf("Google Drive 返回错误（HTTP %d）", resp.StatusCode)
	case strings.Contains(resp.Request.URL.String(), "accounts.google.com"):
		return errGDDenied // sent to the sign-in page: the share is not public
	}
	return nil
}

// gdPage: what the download endpoint answered instead of the file.
type gdPage struct {
	Action string     // where the "Download anyway" form goes ("" when there is none)
	Params url.Values // its hidden fields
	Name   string     // the file, as the page names it
	Size   int64      // its size as the page gives it, rounded (0 = not said)
	Err    string     // the page's own error notice
}

// parseGDPage reads Google's "can't scan this file for viruses" page: a form (id download-form) whose
// hidden fields (id, export, confirm, uuid) make the download address; older layouts carry the address as a
// link or in a script. An error page says why in a uc-error-subcaption paragraph.
func parseGDPage(page string) gdPage {
	var p gdPage
	if m := reGDErr.FindStringSubmatch(page); m != nil {
		p.Err = stripTags(m[1])
	}
	if m := reGDNameSz.FindStringSubmatch(page); m != nil {
		p.Name = stripTags(m[1])
		p.Size = gdSizeText(stripTags(m[2]))
	}
	if m := reGDForm.FindStringSubmatch(page); m != nil {
		form := m[0]
		if a := reGDAction.FindStringSubmatch(form); a != nil {
			p.Action = html.UnescapeString(a[1])
		}
		p.Params = url.Values{}
		for _, in := range reGDInput.FindAllString(m[1], -1) {
			t := reAttrType.FindStringSubmatch(in)
			n, v := reAttrName.FindStringSubmatch(in), reAttrValue.FindStringSubmatch(in)
			if t != nil && strings.EqualFold(t[1], "hidden") && n != nil && v != nil {
				p.Params.Set(html.UnescapeString(n[1]), html.UnescapeString(v[1]))
			}
		}
		if p.Action != "" {
			return p
		}
	}
	if m := reGDHref.FindStringSubmatch(page); m != nil {
		p.Action = gdriveWeb() + html.UnescapeString(m[1])
		return p
	}
	if m := reGDDLURL.FindStringSubmatch(page); m != nil {
		p.Action = strings.NewReplacer(`=`, "=", `&`, "&", `\/`, "/").Replace(m[1])
	}
	return p
}

// gdSizeText: "1.2G" / "523M" / "900K" as the page rounds it.
func gdSizeText(s string) int64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}
	unit := int64(1)
	switch s[len(s)-1] {
	case 'K':
		unit = 1 << 10
	case 'M':
		unit = 1 << 20
	case 'G':
		unit = 1 << 30
	case 'T':
		unit = 1 << 40
	}
	if unit > 1 {
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || n <= 0 {
		return 0
	}
	return int64(n * float64(unit))
}

// gdNotice: what a page says about the file in words this program knows.
func gdNotice(p gdPage) error {
	lower := strings.ToLower(p.Err)
	switch {
	case strings.Contains(lower, "too many users") || strings.Contains(lower, "can't view or download this file at this time") ||
		strings.Contains(lower, "cannot view or download this file at this time"):
		return ErrGDQuota
	case strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist"):
		return errGDGone
	case strings.Contains(lower, "permission") || strings.Contains(lower, "need access"):
		return errGDDenied
	}
	return nil
}

// gdPageErr: the error a page carries, as the player is told.
func gdPageErr(p gdPage, resp *http.Response) error {
	switch err := gdNotice(p); {
	case err != nil:
		return err
	case p.Err != "":
		return errors.New("Google Drive：" + p.Err)
	case strings.Contains(resp.Request.URL.String(), "accounts.google.com"):
		return errGDDenied
	}
	return nil
}

// gdRefusal reads a page that came where the file was asked for: its own notice counts before its status
// (the "too many users" page is sent with 403 or 429 as well as with 200 — it is neither "no access" nor
// worth three tries), then the status, then whatever else the page says.
func gdRefusal(page string, resp *http.Response) (gdPage, error) {
	p := parseGDPage(page)
	if err := gdNotice(p); err != nil {
		return p, err
	}
	if err := gdPageStatus(resp); err != nil {
		return p, err
	}
	return p, gdPageErr(p, resp)
}

// gdSameSite: the confirm form goes back to Google (or to the test's stand-in), nowhere else.
func gdSameSite(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	if h == "google.com" || strings.HasSuffix(h, ".google.com") {
		return true
	}
	if w, err := url.Parse(gdriveWeb()); err == nil && strings.EqualFold(w.Host, u.Host) {
		return true
	}
	return false
}

func gdUCURL(id, rkey string) string {
	u := gdriveWeb() + "/uc?export=download&id=" + url.QueryEscape(id)
	if rkey != "" {
		u += "&resourcekey=" + url.QueryEscape(rkey)
	}
	return u
}

// gdOpenWeb asks the download endpoint for the file from `from` on; a confirm page in between is answered.
// The response's body is the file.
func gdOpenWeb(ctx context.Context, c *http.Client, id, rkey string, from int64, ifRange string) (*http.Response, error) {
	req := newRequest(ctx, gdUCURL(id, rkey))
	setRange(req, from, ifRange)
	resp, err := c.Do(req)
	if err != nil {
		return nil, netErr(GDrive, err)
	}
	if !isPage(resp) {
		if err := gdPageStatus(resp); err != nil {
			resp.Body.Close()
			return nil, err
		}
		return resp, nil
	}
	page := readPage(resp)
	resp.Body.Close()
	p, err := gdRefusal(page, resp)
	if err != nil {
		return nil, err
	}
	if p.Action == "" {
		WriteDebug("uc "+id, page)
		return nil, errGDPage
	}
	a, err := url.Parse(p.Action)
	if err != nil || !gdSameSite(a) {
		return nil, errGDPage
	}
	q := a.Query()
	for k, vs := range p.Params {
		q[k] = vs
	}
	a.RawQuery = q.Encode()
	req = newRequest(ctx, a.String())
	setRange(req, from, ifRange)
	if resp, err = c.Do(req); err != nil {
		return nil, netErr(GDrive, err)
	}
	if isPage(resp) {
		page := readPage(resp)
		resp.Body.Close()
		if _, err := gdRefusal(page, resp); err != nil {
			return nil, err
		}
		WriteDebug("confirm "+id, page)
		return nil, errGDPage
	}
	if err := gdPageStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

// gdOpenAPI asks the API for the file's bytes; a file Google flags as possibly harmful is asked for again
// with the risk acknowledged (the player chose to download it).
func gdOpenAPI(ctx context.Context, c *http.Client, key, id, rkey string, from int64, ifRange string) (*http.Response, error) {
	for _, ack := range []bool{false, true} {
		q := url.Values{"alt": {"media"}, "key": {key}, "supportsAllDrives": {"true"}}
		if ack {
			q.Set("acknowledgeAbuse", "true")
		}
		req := newRequest(ctx, gdriveAPI()+"/drive/v3/files/"+url.PathEscape(id)+"?"+q.Encode())
		if rk := gdResourceKeys(id, rkey); rk != "" {
			req.Header.Set("X-Goog-Drive-Resource-Keys", rk)
		}
		setRange(req, from, ifRange)
		resp, err := c.Do(req)
		if err != nil {
			return nil, netErr(GDrive, err)
		}
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
			return resp, nil
		}
		body := readPage(resp)
		resp.Body.Close()
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			return nil, errRange
		}
		err = gdAPIErr(resp.StatusCode, body)
		if errors.Is(err, errGDAbusive) && !ack {
			continue
		}
		return nil, err
	}
	return nil, errGDAbusive
}

// errRange: the server does not take the part file's offset (the file changed): start over.
var errRange = errors.New("服务器不接受续传")

// ErrRange: is the error that the server refused to continue from the part file?
func ErrRange(err error) bool { return errors.Is(err, errRange) }

// gdProbe reads a file's name and size without fetching it: the download endpoint's answer to a request
// for its first byte, or the confirm page it shows for a large file.
func gdProbe(ctx context.Context, c *http.Client, id, rkey string) (name string, size int64, err error) {
	req := newRequest(ctx, gdUCURL(id, rkey))
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, netErr(GDrive, err)
	}
	defer resp.Body.Close()
	if !isPage(resp) {
		if err := gdPageStatus(resp); err != nil {
			return "", 0, err
		}
		size = TotalSize(resp)
		if size < 0 {
			size = 0
		}
		return FileName(resp), size, nil
	}
	page := readPage(resp)
	p, err := gdRefusal(page, resp)
	if err != nil {
		return "", 0, err
	}
	if p.Action == "" && p.Name == "" {
		WriteDebug("uc "+id, page)
		return "", 0, errGDPage
	}
	return p.Name, p.Size, nil
}

// gdViewName: the file's name from its preview page, when the download endpoint did not say it.
func gdViewName(ctx context.Context, c *http.Client, id, rkey string) string {
	u := gdriveWeb() + "/file/d/" + url.PathEscape(id) + "/view"
	if rkey != "" {
		u += "?resourcekey=" + url.QueryEscape(rkey)
	}
	resp, err := c.Do(newRequest(ctx, u))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	page := readPage(resp)
	if m := reGDOGTitle.FindStringSubmatch(page); m != nil {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	return gdPageTitle(page)
}

func gdWebListing(ctx context.Context, st *core.Store, l *Link) (*core.PanListing, error) {
	c := Client(st)
	defer c.CloseIdleConnections()
	out := &core.PanListing{Surl: l.Key(), Fetched: time.Now().Unix()}
	var title string
	var kids []gdFile
	if !l.Folder {
		name, size, err := gdProbe(ctx, c, l.ID, l.RKey)
		if err == nil {
			if name == "" {
				name = gdViewName(ctx, c, l.ID, l.RKey)
			}
			if name == "" {
				name = "Google Drive " + l.ID[:min(8, len(l.ID))]
			}
			out.Files = []*core.PanFile{{Name: name, Size: size, Ref: l.ID}}
			out.Count, out.Size, out.Title = 1, size, core.StripArchiveExt(name)
			return out, nil
		}
		if Temporary(err) || errors.Is(err, ErrGDQuota) || ctx.Err() != nil {
			return nil, err
		}
		// "open?id=…" does not say whether the id is a file's or a folder's: it is tried as a folder once, and
		// taken as one when the folder view shows entries
		var ferr error
		if title, kids, ferr = gdEmbeddedFolder(ctx, c, l.ID, l.RKey); ferr != nil || len(kids) == 0 {
			return nil, err
		}
	} else {
		var err error
		if title, kids, err = gdEmbeddedFolder(ctx, c, l.ID, l.RKey); err != nil {
			return nil, err
		}
	}
	files, err := gdWalk(out, kids, func(k gdFile) ([]gdFile, error) {
		if err := gdPause(ctx); err != nil {
			return nil, err
		}
		_, sub, err := gdEmbeddedFolder(ctx, c, k.ID, gdKey(k.RKey, l.RKey))
		return sub, err
	})
	if err != nil {
		return nil, err
	}
	out.Files, out.Title = files, title
	return out, nil
}
