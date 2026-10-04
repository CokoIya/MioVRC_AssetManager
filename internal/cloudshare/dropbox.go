package cloudshare

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"vrclib/internal/core"
)

// Dropbox: a public link with dl=1 answers with the file itself (a shared folder: with one zip of the
// folder, which Dropbox makes for folders up to 20 GB and 10,000 files). What a shared folder holds cannot
// be listed without a Dropbox sign-in, so a folder is one entry here. Links that ask for a password are
// not read.

func dropboxBase() string {
	if v := os.Getenv("VRCLIB_DROPBOX_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://www.dropbox.com"
}

// dbDownloadURL: the link with dl=1 (and on the test's stand-in when there is one).
func dbDownloadURL(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	if b, err := url.Parse(dropboxBase()); err == nil {
		u.Scheme, u.Host = b.Scheme, b.Host
	}
	q := u.Query()
	q.Set("dl", "1")
	u.RawQuery = q.Encode()
	return u.String()
}

func dbStatus(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return errDBGone
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return errDBDenied
	case resp.StatusCode == http.StatusTooManyRequests:
		return tempErr{ErrDBTraffic}
	case resp.StatusCode >= 500:
		return tempErr{fmt.Errorf("Dropbox 暂时无法响应（HTTP %d），请稍后重试", resp.StatusCode)}
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		return errRange
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent:
		return fmt.Errorf("Dropbox 返回错误（HTTP %d）", resp.StatusCode)
	}
	return nil
}

// dbOpen asks for the file from `from` on. The response's body is the file.
func dbOpen(ctx context.Context, c *http.Client, link string, from int64, ifRange string) (*http.Response, error) {
	req := newRequest(ctx, dbDownloadURL(link))
	setRange(req, from, ifRange)
	resp, err := c.Do(req)
	if err != nil {
		return nil, netErr(Dropbox, err)
	}
	if err := dbStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	if isPage(resp) {
		page := readPage(resp)
		resp.Body.Close()
		return nil, dbPageErr(page)
	}
	return resp, nil
}

// dbPageErr: a page where the file should be — the password prompt, or the notice that the folder is too
// big to zip; a link suspended for its traffic says so.
func dbPageErr(page string) error {
	lower := strings.ToLower(page)
	if strings.Contains(lower, "too much traffic") || strings.Contains(lower, "temporarily disabled") {
		return tempErr{ErrDBTraffic}
	}
	return errDBPage
}

// dbProbe reads the name and size of what the link gives, from the answer to a request for its first byte.
func dbProbe(ctx context.Context, c *http.Client, l *Link) (name string, size int64, err error) {
	req := newRequest(ctx, dbDownloadURL(l.URL))
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, netErr(Dropbox, err)
	}
	defer resp.Body.Close()
	if err := dbStatus(resp); err != nil {
		return "", 0, err
	}
	if isPage(resp) {
		return "", 0, dbPageErr(readPage(resp))
	}
	size = TotalSize(resp)
	if size < 0 || resp.StatusCode == http.StatusPartialContent && resp.Header.Get("Content-Range") == "" {
		size = 0
	}
	return FileName(resp), size, nil
}

func dbListing(ctx context.Context, st *core.Store, l *Link) (*core.PanListing, error) {
	c := Client(st)
	defer c.CloseIdleConnections()
	name, size, err := dbProbe(ctx, c, l)
	if err != nil {
		return nil, err
	}
	if name == "" {
		if n, err := url.PathUnescape(l.Name); err == nil {
			name = n
		} else {
			name = l.Name
		}
		if l.Folder && name != "" && core.LowerExt(name) != ".zip" {
			name += ".zip"
		}
	}
	if name == "" {
		name = "Dropbox " + l.ID[:min(8, len(l.ID))]
		if l.Folder {
			name += ".zip"
		}
	}
	out := &core.PanListing{Surl: l.Key(), Fetched: time.Now().Unix(), Title: core.StripArchiveExt(name),
		Files: []*core.PanFile{{Name: name, Size: size}}, Count: 1, Size: size}
	return out, nil
}
