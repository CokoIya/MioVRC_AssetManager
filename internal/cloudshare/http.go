package cloudshare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"vrclib/internal/core"
)

// ---------- what the services answer, as the player is told ----------

var (
	errGDGone   = errors.New("文件不存在或分享已被取消")
	errGDDenied = errors.New("没有访问权限：该分享未设为「知道链接的任何人」可查看")
	// ErrGDQuota: Google throttles a file that many people fetch ("Too many users have viewed or downloaded
	// this file recently"); it passes within a day or so.
	ErrGDQuota   = errors.New("该文件近期下载人数过多，Google Drive 暂时限制了下载，请稍后再试")
	errGDPage    = errors.New("无法从 Google Drive 读取下载地址（页面格式可能已变化）")
	errGDAbusive = errors.New("Google Drive 将该文件标记为可能有害")
	errDBGone    = errors.New("Dropbox 链接已失效或文件已被删除")
	errDBDenied  = errors.New("该 Dropbox 链接需要密码或登录后才能访问")
	// ErrDBTraffic: Dropbox suspends a link whose traffic is too high ("This account's links are generating too
	// much traffic"); it comes back by itself.
	ErrDBTraffic = errors.New("该 Dropbox 链接近期流量过大，Dropbox 暂时限制了下载，请稍后再试")
	errDBPage    = errors.New("Dropbox 返回的是网页而不是文件：链接可能需要密码，或文件夹过大无法打包下载（超过 20 GB 或 1 万个文件）")
	// errAPIKeyUnusable: Google turned the API key away (invalid, the Drive API not enabled for it, its quota
	// used up): the share pages are read instead.
	errAPIKeyUnusable = errors.New("Google Drive API 密钥不可用")
)

// ErrSettled: an error that reading the share again will not change — it is gone, or it needs an access the
// program does not have.
func ErrSettled(msg string) bool {
	for _, e := range []error{errGDGone, errGDDenied, errDBGone, errDBDenied} {
		if msg == e.Error() {
			return true
		}
	}
	return false
}

// tempErr: the service or the connection failed in a way that passes: the request is worth another try.
type tempErr struct{ error }

func (e tempErr) Unwrap() error { return e.error }

// Temporary: is the error one to try again after a pause (the connection broke, the service is busy)?
func Temporary(err error) bool {
	var te tempErr
	return errors.As(err, &te)
}

// Temp marks an error as one to try again after a pause.
func Temp(err error) error { return tempErr{err} }

// netErr: why a request failed, without its address (a file's address carries the key that opens it). The
// services sit behind the Great Firewall: a connection that times out or is refused points at the proxy
// setting.
func netErr(service string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		s = "连接超时"
	case strings.Contains(s, "refused") || strings.Contains(s, "no such host") || strings.Contains(s, "reset") || strings.Contains(s, "EOF") || strings.Contains(s, "unreachable"):
		s = "连接失败"
	default:
		return tempErr{fmt.Errorf("无法连接 %s（%s）", Label(service), s)}
	}
	return tempErr{fmt.Errorf("无法连接 %s（%s），请检查网络或在「设置」中填写代理", Label(service), s)}
}

// ---------- requests ----------

// Client: the shares' requests, through the proxy of the settings. The jar keeps what Google sets on the way
// to a download; nothing of it outlives the client.
func Client(st *core.Store) *http.Client {
	c := core.HTTPClient(st)
	c.Jar, _ = cookiejar.New(nil)
	return c
}

func newRequest(ctx context.Context, u string) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", core.UA)
	// the notices looked for in Google's pages are their English ones
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	req.Header.Set("Accept", "*/*")
	return req
}

func setRange(req *http.Request, from int64, ifRange string) {
	if from > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
		if ifRange != "" {
			req.Header.Set("If-Range", ifRange)
		}
	}
}

const pageMax = 4 << 20 // a share page, a confirm page, an error page: never a file

func readPage(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, pageMax))
	return string(b)
}

// isHTML: a web page, by what the server calls it.
func isHTML(resp *http.Response) bool {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.HasPrefix(ct, "text/html") || strings.HasPrefix(ct, "application/xhtml")
}

// isPage: a web page where a file was asked for. A file that is itself a page (README.html, 利用規約.html)
// comes as what every file comes as: an attachment.
func isPage(resp *http.Response) bool {
	return isHTML(resp) && resp.Header.Get("Content-Disposition") == ""
}

// Gone: the service says the file is not there (any more).
func Gone(err error) bool { return errors.Is(err, errGDGone) || errors.Is(err, errDBGone) }

// FileName: the name the server gives the file (Content-Disposition), "" when it gives none.
func FileName(resp *http.Response) string {
	cd := resp.Header.Get("Content-Disposition")
	if cd == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(cd); err == nil {
		if n := params["filename"]; n != "" {
			return filepath.Base(strings.ReplaceAll(n, "\\", "/"))
		}
	}
	// a name in a form ParseMediaType refuses (a stray quote, a bare UTF-8 name)
	if i := strings.Index(cd, "filename*=UTF-8''"); i >= 0 {
		n := cd[i+len("filename*=UTF-8''"):]
		if j := strings.IndexAny(n, ";"); j >= 0 {
			n = n[:j]
		}
		if d, err := url.PathUnescape(strings.TrimSpace(n)); err == nil {
			return filepath.Base(d)
		}
	}
	if i := strings.Index(cd, "filename="); i >= 0 {
		n := strings.Trim(strings.TrimSpace(cd[i+len("filename="):]), `"`)
		if j := strings.IndexAny(n, `";`); j >= 0 {
			n = n[:j]
		}
		return filepath.Base(n)
	}
	return ""
}

// TotalSize: the file's whole size by the answer's headers (-1 when the server does not say): the total of a
// Content-Range, else the Content-Length of an answer that holds the whole file.
func TotalSize(resp *http.Response) int64 {
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if _, tot, ok := strings.Cut(cr, "/"); ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(tot), 10, 64); err == nil {
				return n
			}
		}
		return -1
	}
	if resp.StatusCode == http.StatusOK && resp.ContentLength >= 0 {
		return resp.ContentLength
	}
	return -1
}

// WriteDebug keeps a page that was not understood for the author to look at (cloud-debug.txt in the data
// folder). A confirm page's one-time token is of no use to anyone afterwards; still, it is not kept.
func WriteDebug(what, page string) {
	page = reDebugToken.ReplaceAllString(page, `$1…`)
	_ = os.WriteFile(filepath.Join(core.DataDir, "cloud-debug.txt"), []byte(what+"\n"+core.Truncate(page, 200000)), 0600)
}
