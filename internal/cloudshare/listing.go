package cloudshare

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"vrclib/internal/core"
)

// FetchListing reads what a Google Drive or Dropbox share holds, as a listing like a Baidu share's (its
// Surl is the card key, "gd:…" / "db:…"). A Drive folder is listed in full (with sizes when the Drive API
// can be asked, by name alone from the share page otherwise); a Dropbox share is one file, or one zip for a
// folder.
func FetchListing(st *core.Store, link string) (*core.PanListing, error) {
	return FetchListingCtx(context.Background(), st, link)
}

// FetchListingCtx: FetchListing that ends when ctx does (a download that reads the share on its way, cancelled).
func FetchListingCtx(ctx context.Context, st *core.Store, link string) (*core.PanListing, error) {
	l := Parse(link)
	if l == nil {
		return nil, errors.New("不是 Google Drive 或 Dropbox 分享链接")
	}
	switch l.Service {
	case GDrive:
		if key := gdriveKey(); key != "" {
			out, err := gdAPIListing(ctx, st, l, key)
			if err == nil || !errors.Is(err, errAPIKeyUnusable) {
				return out, err
			}
			core.Logf("Google Drive API 不可用（%v），改为读取分享页面", err)
		}
		return gdWebListing(ctx, st, l)
	case Dropbox:
		return dbListing(ctx, st, l)
	}
	return nil, errors.New("不是 Google Drive 或 Dropbox 分享链接")
}

func sortFiles(fs []*core.PanFile) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Dir != fs[j].Dir {
			return fs[i].Dir
		}
		return strings.ToLower(fs[i].Name) < strings.ToLower(fs[j].Name)
	})
}

// Source: one file of a share, as the download asks for it.
type Source struct {
	Service string
	ID      string // Drive: the file's id (a listing entry's Ref)
	RKey    string // Drive: the file's own resourcekey, else the share link's
	Link    string // Dropbox: the share link
}

// SourceFor: where a file of a share's listing is fetched from.
func SourceFor(link *Link, f *core.PanFile) Source {
	s := Source{Service: link.Service, RKey: link.RKey, Link: link.URL}
	if f != nil {
		s.ID = f.Ref
		if f.RKey != "" {
			s.RKey = f.RKey
		}
	}
	if s.ID == "" {
		s.ID = link.ID
	}
	return s
}

// Open asks the service for the file from byte `from` on (ifRange: what the server said about the file last
// time, so that only the same file is continued). The response's body is the file — 200 from the start, 206
// from `from`; anything else is an error, a typed one when the service said why. Google's confirm page for
// large files is answered on the way.
func (s Source) Open(ctx context.Context, c *http.Client, from int64, ifRange string) (*http.Response, error) {
	switch s.Service {
	case GDrive:
		if key := gdriveKey(); key != "" {
			resp, err := gdOpenAPI(ctx, c, key, s.ID, s.RKey, from, ifRange)
			if err == nil || !errors.Is(err, errAPIKeyUnusable) {
				return resp, err
			}
			core.Logf("Google Drive API 不可用（%v），改为从分享页面下载", err)
		}
		return gdOpenWeb(ctx, c, s.ID, s.RKey, from, ifRange)
	case Dropbox:
		return dbOpen(ctx, c, s.Link, from, ifRange)
	}
	return nil, errors.New("不是 Google Drive 或 Dropbox 分享")
}
