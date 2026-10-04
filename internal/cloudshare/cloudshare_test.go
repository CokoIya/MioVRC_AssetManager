package cloudshare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
)

// mockDrive stands in for drive.google.com (share pages, the uc download endpoint, the confirm form's
// target) and for www.googleapis.com (the Drive API). Shapes follow Google's pages and API as of 2026-10.
type mockDrive struct {
	mu     sync.Mutex
	hits   map[string]int
	files  map[string]mockFile // id → file
	quota  map[string]bool     // ids Google throttles
	abuse  map[string]bool     // ids flagged as harmful
	keyBad bool                // the API key is turned away
	noAPI  bool                // the API is not reached at all (connection refused)
}

type mockFile struct {
	name    string
	body    string
	folder  bool
	kids    []string
	private bool
	big     bool // the uc endpoint shows the confirm page first
}

func newMockDrive() *mockDrive {
	return &mockDrive{hits: map[string]int{}, quota: map[string]bool{}, abuse: map[string]bool{}, files: map[string]mockFile{
		"1FileSmall000000000000000000": {name: "Outfit_v1.zip", body: strings.Repeat("Z", 3000)},
		"1FileBig00000000000000000000": {name: "Big_Outfit.zip", body: strings.Repeat("B", 5000), big: true},
		"1FilePrivate0000000000000000": {name: "secret.zip", body: "S", private: true},
		"1Folder000000000000000000000": {name: "Kaguya Outfits", folder: true, kids: []string{"1FileSmall000000000000000000", "1Sub00000000000000000000000", "1Doc000000000000000000000000"}},
		"1Sub00000000000000000000000":  {name: "Textures", folder: true, kids: []string{"1FileBig00000000000000000000"}},
		"1Doc000000000000000000000000": {name: "Manual", body: "doc"}, // a Google Doc: no bytes to fetch
	}}
}

func (m *mockDrive) hit(what string) {
	m.mu.Lock()
	m.hits[what]++
	m.mu.Unlock()
}

func (m *mockDrive) count(what string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits[what]
}

func (m *mockDrive) serveFile(w http.ResponseWriter, r *http.Request, f mockFile) {
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.name+`"; filename*=UTF-8''`+f.name)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("ETag", `"etag-`+f.name+`"`)
	w.Header().Set("Accept-Ranges", "bytes")
	from, to := 0, len(f.body)-1
	if rg := r.Header.Get("Range"); rg != "" {
		if n, _ := fmt.Sscanf(rg, "bytes=%d-%d", &from, &to); n == 1 {
			to = len(f.body) - 1
		}
		if from >= len(f.body) {
			w.WriteHeader(416)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, len(f.body)))
		w.Header().Set("Content-Length", fmt.Sprint(to-from+1))
		w.WriteHeader(206)
		_, _ = io.WriteString(w, f.body[from:to+1])
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(f.body)))
	_, _ = io.WriteString(w, f.body)
}

func (m *mockDrive) handler(base string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.hit(r.URL.Path)
		q := r.URL.Query()
		id := q.Get("id")
		switch {
		case strings.HasPrefix(r.URL.Path, "/drive/v3/files"):
			if m.noAPI {
				hj, _ := w.(http.Hijacker)
				c, _, _ := hj.Hijack()
				c.Close()
				return
			}
			if m.keyBad || q.Get("key") == "" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","errors":[{"reason":"badRequest"}]}}`)
				return
			}
			if r.URL.Path == "/drive/v3/files" {
				parent := strings.TrimSuffix(strings.TrimPrefix(q.Get("q"), "'"), "' in parents and trashed = false")
				f, ok := m.files[parent]
				if !ok || !f.folder {
					w.WriteHeader(404)
					fmt.Fprint(w, `{"error":{"code":404,"message":"File not found","errors":[{"reason":"notFound"}]}}`)
					return
				}
				var items []string
				for _, k := range f.kids {
					items = append(items, m.apiJSON(k))
				}
				fmt.Fprintf(w, `{"files":[%s]}`, strings.Join(items, ","))
				return
			}
			id = strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")
			f, ok := m.files[id]
			if !ok || f.private {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":{"code":404,"message":"File not found: `+id+`.","errors":[{"domain":"global","reason":"notFound","message":"File not found: `+id+`."}]}}`)
				return
			}
			if q.Get("alt") != "media" {
				fmt.Fprint(w, m.apiJSON(id))
				return
			}
			switch {
			case m.quota[id]:
				w.WriteHeader(403)
				fmt.Fprint(w, `{"error":{"code":403,"message":"The download quota for this file has been exceeded.","errors":[{"reason":"downloadQuotaExceeded"}]}}`)
			case m.abuse[id] && q.Get("acknowledgeAbuse") != "true":
				w.WriteHeader(403)
				fmt.Fprint(w, `{"error":{"code":403,"errors":[{"reason":"cannotDownloadAbusiveFile"}]}}`)
			default:
				m.serveFile(w, r, f)
			}
		case r.URL.Path == "/embeddedfolderview":
			f, ok := m.files[id]
			if !ok || !f.folder {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			var b strings.Builder
			fmt.Fprintf(&b, `<!DOCTYPE html><html><head><title>%s - Google Drive</title></head><body><div id="folder-view"><div class="flip-entries">`, f.name)
			for _, k := range f.kids {
				kf := m.files[k]
				href := "https://drive.google.com/file/d/" + k + "/view?usp=drive_web"
				if kf.folder {
					href = "https://drive.google.com/drive/folders/" + k + "?usp=drive_web"
				} else if kf.body == "doc" {
					href = "https://docs.google.com/document/d/" + k + "/edit?usp=drive_web"
				}
				fmt.Fprintf(&b, `<div class="flip-entry" id="entry-%s"><a href="%s" target="_blank"><div class="flip-entry-thumb"><img src="x"></div><div class="flip-entry-info"><div class="flip-entry-title">%s</div><div class="flip-entry-last-modified"><div>Oct 1, 2026</div></div></div></a></div>`, k, href, kf.name)
			}
			b.WriteString(`</div></div></body></html>`)
			fmt.Fprint(w, b.String())
		case r.URL.Path == "/uc":
			f, ok := m.files[id]
			switch {
			case !ok:
				w.WriteHeader(404)
				fmt.Fprint(w, "<html><body>Not Found</body></html>")
			case f.private:
				http.Redirect(w, r, base+"/accounts.google.com/ServiceLogin", 302)
			case m.quota[id]:
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, `<html><body><div class="uc-main"><p class="uc-error-caption">Sorry, you can't view or download this file at this time.</p><p class="uc-error-subcaption">Too many users have viewed or downloaded this file recently. Please try accessing the file again later. If the file you are trying to access is particularly large or is shared with many people, it may take up to 24 hours to be able to view or download the file. If you still can't access a file after 24 hours, contact your domain administrator.</p></div></body></html>`)
			case f.big:
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Google Drive - Virus scan warning</title></head><body><div class="uc-main"><p class="uc-warning-caption">Google Drive can't scan this file for viruses.</p><p class="uc-warning-subcaption"><span class="uc-name-size"><a href="/open?id=%s">%s</a> (4.9K)</span> is too large for Google to scan for viruses. Would you still like to download this file?</p><form id="download-form" action="%s/download" method="get"><input type="submit" id="uc-download-link" class="goog-inline-block jfk-button jfk-button-action" value="Download anyway"/><input type="hidden" name="id" value="%s"><input type="hidden" name="export" value="download"><input type="hidden" name="confirm" value="t"><input type="hidden" name="uuid" value="0c8a7b3e-1d2f-4a5b-9c6d-7e8f9a0b1c2d"></form></div></body></html>`, id, f.name, base, id)
			default:
				m.serveFile(w, r, f)
			}
		case r.URL.Path == "/download": // drive.usercontent.google.com/download: the confirm form's target
			f, ok := m.files[id]
			if !ok || q.Get("confirm") != "t" || q.Get("uuid") == "" {
				w.WriteHeader(400)
				return
			}
			m.serveFile(w, r, f)
		case strings.HasPrefix(r.URL.Path, "/accounts.google.com/"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<html><body>Sign in</body></html>")
		case strings.HasPrefix(r.URL.Path, "/file/d/"):
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/file/d/"), "/")[0]
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<html><head><meta property="og:title" content="%s"><title>%s - Google Drive</title></head></html>`, m.files[id].name, m.files[id].name)
		default:
			w.WriteHeader(404)
		}
	}
}

func (m *mockDrive) apiJSON(id string) string {
	f := m.files[id]
	switch {
	case f.folder:
		return fmt.Sprintf(`{"id":"%s","name":"%s","mimeType":"application/vnd.google-apps.folder"}`, id, f.name)
	case f.body == "doc":
		return fmt.Sprintf(`{"id":"%s","name":"%s","mimeType":"application/vnd.google-apps.document"}`, id, f.name)
	}
	return fmt.Sprintf(`{"id":"%s","name":"%s","mimeType":"application/zip","size":"%d"}`, id, f.name, len(f.body))
}

func startDrive(t *testing.T, m *mockDrive) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(nil)
	srv.Config.Handler = m.handler(srv.URL)
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_GDRIVE_BASE", srv.URL)
	t.Setenv("VRCLIB_GDRIVE_API", srv.URL)
	t.Setenv("VRCLIB_GDRIVE_KEY", "")
	core.DataDir = t.TempDir()
	gap := gdGap
	gdGap = 0
	t.Cleanup(func() { gdGap = gap })
	return srv
}

func names(fs []*core.PanFile, prefix string) []string {
	var out []string
	for _, f := range fs {
		p := prefix + "/" + f.Name
		if f.Dir {
			out = append(out, p+"/")
			out = append(out, names(f.Children, p)...)
		} else {
			out = append(out, fmt.Sprintf("%s(%d)", p, f.Size))
		}
	}
	return out
}

func TestDriveListingWithKey(t *testing.T) {
	m := newMockDrive()
	startDrive(t, m)
	t.Setenv("VRCLIB_GDRIVE_KEY", "AIzaTestKey")
	st := &core.Store{}
	l, err := FetchListing(st, "https://drive.google.com/drive/folders/1Folder000000000000000000000?usp=sharing")
	if err != nil {
		t.Fatal(err)
	}
	want := "/Textures/ /Textures/Big_Outfit.zip(5000) /Outfit_v1.zip(3000)"
	if got := strings.Join(names(l.Files, ""), " "); got != want || l.Title != "Kaguya Outfits" || l.Count != 2 || l.Size != 8000 || l.Surl != "gd:1Folder000000000000000000000" || l.Truncated {
		t.Errorf("listing %s (%s, %d files, %d bytes, truncated %v)", got, l.Title, l.Count, l.Size, l.Truncated)
	}
	if l.Files[0].Children[0].Ref != "1FileBig00000000000000000000" {
		t.Error("the file's id is not kept")
	}
	if m.count("/uc") != 0 || m.count("/embeddedfolderview") != 0 {
		t.Error("the share pages were read although the API answered")
	}
	// one file
	l, err = FetchListing(st, "https://drive.google.com/file/d/1FileSmall000000000000000000/view")
	if err != nil || len(l.Files) != 1 || l.Files[0].Name != "Outfit_v1.zip" || l.Files[0].Size != 3000 || l.Title != "Outfit_v1" {
		t.Errorf("one file: %+v %v", l, err)
	}
	// gone, a Google Doc
	if _, err := FetchListing(st, "https://drive.google.com/file/d/1Nope0000000000000000000000/view"); !errors.Is(err, errGDGone) || !ErrSettled(err.Error()) {
		t.Errorf("a file that is not there: %v", err)
	}
	if _, err := FetchListing(st, "https://drive.google.com/file/d/1Doc000000000000000000000000/view"); err == nil {
		t.Error("a Google Doc is not a file")
	}
	// a key Google turns away: the pages are read instead
	m.keyBad = true
	l, err = FetchListing(st, "https://drive.google.com/drive/folders/1Folder000000000000000000000")
	if err != nil || m.count("/embeddedfolderview") == 0 {
		t.Fatalf("no fallback to the share page: %v (%v)", err, m.hits)
	}
	if got := strings.Join(names(l.Files, ""), " "); got != "/Textures/ /Textures/Big_Outfit.zip(0) /Outfit_v1.zip(0)" || l.Title != "Kaguya Outfits" {
		t.Errorf("page listing %s (%s)", got, l.Title)
	}
}

func TestDriveListingFromPages(t *testing.T) {
	m := newMockDrive()
	startDrive(t, m)
	st := &core.Store{}
	// a small file: the download endpoint answers the first byte with the name and the size
	l, err := FetchListing(st, "https://drive.google.com/file/d/1FileSmall000000000000000000/view?usp=sharing")
	if err != nil || len(l.Files) != 1 || l.Files[0].Name != "Outfit_v1.zip" || l.Files[0].Size != 3000 || l.Files[0].Ref != "1FileSmall000000000000000000" {
		t.Errorf("small file: %+v %v", l, err)
	}
	// a large file: the confirm page names it and gives a rounded size; the download is not started
	l, err = FetchListing(st, "https://drive.google.com/open?id=1FileBig00000000000000000000")
	if err != nil || len(l.Files) != 1 || l.Files[0].Name != "Big_Outfit.zip" || l.Files[0].Size != 5017 {
		t.Errorf("large file: %+v %v", l, err)
	}
	if m.count("/download") != 0 {
		t.Error("the confirm form was submitted while listing")
	}
	// throttled, private, gone
	m.quota["1FileSmall000000000000000000"] = true
	if _, err := FetchListing(st, "https://drive.google.com/file/d/1FileSmall000000000000000000/view"); !errors.Is(err, ErrGDQuota) || ErrSettled(err.Error()) {
		t.Errorf("throttled: %v", err)
	}
	if _, err := FetchListing(st, "https://drive.google.com/file/d/1FilePrivate0000000000000000/view"); !errors.Is(err, errGDDenied) || !ErrSettled(err.Error()) {
		t.Errorf("private: %v", err)
	}
	if _, err := FetchListing(st, "https://drive.google.com/file/d/1Nope0000000000000000000000/view"); !errors.Is(err, errGDGone) {
		t.Errorf("gone: %v", err)
	}
	// a folder, through the embeddable folder view, sub-folders and all; Docs are left out
	l, err = FetchListing(st, "https://drive.google.com/drive/folders/1Folder000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(l.Files, ""), " "); got != "/Textures/ /Textures/Big_Outfit.zip(0) /Outfit_v1.zip(0)" || l.Title != "Kaguya Outfits" || l.Count != 2 {
		t.Errorf("folder: %s (%s, %d)", got, l.Title, l.Count)
	}
	if m.count("/drive/v3/files") != 0 {
		t.Error("the API was asked without a key")
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDriveOpen(t *testing.T) {
	m := newMockDrive()
	startDrive(t, m)
	st := &core.Store{}
	ctx := context.Background()
	c := Client(st)
	big := Source{Service: GDrive, ID: "1FileBig00000000000000000000"}
	// the confirm page is answered: the form's fields go back to its target
	resp, err := big.Open(ctx, c, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if body := readAll(t, resp); len(body) != 5000 || resp.StatusCode != 200 || FileName(resp) != "Big_Outfit.zip" || TotalSize(resp) != 5000 {
		t.Errorf("confirm flow: %d bytes, %d, %q, %d", len(body), resp.StatusCode, FileName(resp), TotalSize(resp))
	}
	// continued from byte 4000, through the confirm page again
	resp, err = big.Open(ctx, c, 4000, `"etag-Big_Outfit.zip"`)
	if err != nil {
		t.Fatal(err)
	}
	if body := readAll(t, resp); len(body) != 1000 || resp.StatusCode != 206 || TotalSize(resp) != 5000 {
		t.Errorf("resume: %d bytes, %d, total %d", len(body), resp.StatusCode, TotalSize(resp))
	}
	// a small file comes straight away
	resp, err = Source{Service: GDrive, ID: "1FileSmall000000000000000000"}.Open(ctx, c, 0, "")
	if err != nil || readAll(t, resp) != strings.Repeat("Z", 3000) {
		t.Errorf("small file: %v", err)
	}
	// what Google says instead of the file
	m.quota["1FileBig00000000000000000000"] = true
	if _, err := big.Open(ctx, c, 0, ""); !errors.Is(err, ErrGDQuota) {
		t.Errorf("throttled: %v", err)
	}
	if _, err := (Source{Service: GDrive, ID: "1FilePrivate0000000000000000"}).Open(ctx, c, 0, ""); !errors.Is(err, errGDDenied) {
		t.Errorf("private: %v", err)
	}
	if _, err := (Source{Service: GDrive, ID: "1Nope0000000000000000000000"}).Open(ctx, c, 0, ""); !errors.Is(err, errGDGone) {
		t.Errorf("gone: %v", err)
	}
	// with a key: the API; a file flagged as harmful is asked for with the risk acknowledged
	t.Setenv("VRCLIB_GDRIVE_KEY", "AIzaTestKey")
	m.abuse["1FileSmall000000000000000000"] = true
	resp, err = Source{Service: GDrive, ID: "1FileSmall000000000000000000"}.Open(ctx, c, 1000, "")
	if err != nil {
		t.Fatal(err)
	}
	if body := readAll(t, resp); len(body) != 2000 || resp.StatusCode != 206 {
		t.Errorf("API resume: %d bytes, %d", len(body), resp.StatusCode)
	}
	if _, err := (Source{Service: GDrive, ID: "1FileBig00000000000000000000"}).Open(ctx, c, 0, ""); !errors.Is(err, ErrGDQuota) {
		t.Errorf("API throttled: %v", err)
	}
	if _, err := (Source{Service: GDrive, ID: "1Nope0000000000000000000000"}).Open(ctx, c, 0, ""); !errors.Is(err, errGDGone) {
		t.Errorf("API gone: %v", err)
	}
	// the key turned away: the page flow takes over
	m.keyBad, m.quota["1FileBig00000000000000000000"] = true, false
	resp, err = big.Open(ctx, c, 0, "")
	if err != nil || len(readAll(t, resp)) != 5000 || m.count("/download") == 0 {
		t.Errorf("fallback: %v", err)
	}
}

func TestDriveConfirmPageShapes(t *testing.T) {
	// the form, with the attributes in any order and entities in values
	p := parseGDPage(`<form method="get" id="download-form" action="https://drive.usercontent.google.com/download?x=1&amp;y=2"><input value="t" name="confirm" type="hidden"><input type="hidden" name="id" value="1abc"><input type="hidden" value="download" name="export"><input type="hidden" name="uuid" value="u-1"><input type="submit" value="Download anyway"></form>`)
	if p.Action != "https://drive.usercontent.google.com/download?x=1&y=2" || p.Params.Get("confirm") != "t" || p.Params.Get("id") != "1abc" || p.Params.Get("uuid") != "u-1" || p.Params.Get("export") != "download" || len(p.Params) != 4 {
		t.Errorf("form: %+v", p)
	}
	// the older link
	p = parseGDPage(`<a id="uc-download-link" href="/uc?export=download&amp;confirm=abcd&amp;id=1abc">Download anyway</a>`)
	if !strings.HasSuffix(p.Action, "/uc?export=download&confirm=abcd&id=1abc") {
		t.Errorf("link: %+v", p)
	}
	// the script
	p = parseGDPage(`{"downloadUrl":"https://drive.usercontent.google.com/download?id=1abc&confirm=t"}`)
	if p.Action != "https://drive.usercontent.google.com/download?id=1abc&confirm=t" {
		t.Errorf("script: %+v", p)
	}
	// the error page; a confirm page never goes anywhere but Google
	p = parseGDPage(`<p class="uc-error-subcaption">Too many users have viewed or downloaded this file recently.</p>`)
	if p.Action != "" || !strings.HasPrefix(p.Err, "Too many users") {
		t.Errorf("error: %+v", p)
	}
	for _, c := range []struct {
		u  string
		ok bool
	}{{"https://drive.usercontent.google.com/download", true}, {"https://docs.google.com/uc", true}, {"https://evil.example.com/download", false}, {"https://google.com.example.com/x", false}} {
		if u, _ := parseURL(c.u); gdSameSite(u) != c.ok {
			t.Errorf("%s: same site %v", c.u, !c.ok)
		}
	}
	for s, n := range map[string]int64{"4.9K": 5017, "523M": 523 << 20, "1.2G": 1288490188, "12": 12, "": 0, "x": 0} {
		if gdSizeText(s) != n {
			t.Errorf("size %q: %d, want %d", s, gdSizeText(s), n)
		}
	}
}

func TestNetErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()
	t.Setenv("VRCLIB_GDRIVE_BASE", addr)
	t.Setenv("VRCLIB_GDRIVE_API", addr)
	t.Setenv("VRCLIB_DROPBOX_BASE", addr)
	t.Setenv("VRCLIB_GDRIVE_KEY", "")
	core.DataDir = t.TempDir()
	st := &core.Store{}
	for _, link := range []string{"https://drive.google.com/file/d/1FileSmall000000000000000000/view", "https://drive.google.com/drive/folders/1Folder000000000000000000000",
		"https://www.dropbox.com/s/abc123def456ghi/a.zip?dl=0"} {
		_, err := FetchListing(st, link)
		if err == nil || !Temporary(err) || !strings.Contains(err.Error(), "代理") || strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("%s: %v", link, err)
		}
	}
	// a stall: the server takes the connection and says nothing; the caller's deadline ends it
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(2 * time.Second) }))
	defer hang.Close()
	t.Setenv("VRCLIB_DROPBOX_BASE", hang.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := Source{Service: Dropbox, Link: "https://www.dropbox.com/s/abc123def456ghi/a.zip"}.Open(ctx, Client(st), 0, "")
	if err == nil || !Temporary(err) || !strings.Contains(err.Error(), "连接超时") {
		t.Errorf("stall: %v", err)
	}
}

// mockDropbox: www.dropbox.com and its download host as one server. dl=1 gives the file (or the zip of a
// shared folder); a password-protected link gives its page.
func mockDropbox(t *testing.T) (*httptest.Server, *sync.Map) {
	t.Helper()
	hits := &sync.Map{}
	body := strings.Repeat("D", 7000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := hits.LoadOrStore(r.URL.Path, new(int))
		*n.(*int)++
		q := r.URL.Query()
		if q.Get("dl") != "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<html><body>preview page</body></html>")
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/s/abc123def456ghi/"), strings.HasPrefix(r.URL.Path, "/scl/fi/abc123def456ghi/") && q.Get("rlkey") == "k1k2k3k4k5k6":
			w.Header().Set("Content-Disposition", `attachment; filename="Outfit v1.2.zip"; filename*=UTF-8''Outfit%20v1.2.zip`)
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("ETag", `"db-etag"`)
			from, to := 0, len(body)-1
			if rg := r.Header.Get("Range"); rg != "" {
				if n, _ := fmt.Sscanf(rg, "bytes=%d-%d", &from, &to); n == 1 {
					to = len(body) - 1
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, len(body)))
				w.WriteHeader(206)
				_, _ = io.WriteString(w, body[from:to+1])
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			_, _ = io.WriteString(w, body)
		case strings.HasPrefix(r.URL.Path, "/scl/fo/fo1234567890abc/"):
			// a folder: Dropbox builds the zip as it sends it (no length, no ranges)
			w.Header().Set("Content-Disposition", `attachment; filename="Kaguya Outfits.zip"`)
			w.Header().Set("Content-Type", "application/zip")
			w.WriteHeader(200)
			w.(http.Flusher).Flush() // chunked, as a zip made on the fly comes
			_, _ = io.WriteString(w, "PK\x03\x04folder zip")
		case strings.HasPrefix(r.URL.Path, "/scl/fi/pwd4567890abcde/"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<html><body><form>Enter the password</form></body></html>")
		case strings.HasPrefix(r.URL.Path, "/s/busy123456789ab/"):
			w.WriteHeader(429)
			fmt.Fprint(w, "<html><body>This account's links are generating too much traffic and have been temporarily disabled!</body></html>")
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, "<html><body>Error (404)</body></html>")
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_DROPBOX_BASE", srv.URL)
	core.DataDir = t.TempDir()
	return srv, hits
}

func TestDropbox(t *testing.T) {
	_, hits := mockDropbox(t)
	st := &core.Store{}
	for _, link := range []string{"https://www.dropbox.com/s/abc123def456ghi/Outfit_v1.2.zip?dl=0", "https://www.dropbox.com/scl/fi/abc123def456ghi/Outfit_v1.2.zip?rlkey=k1k2k3k4k5k6&st=x&dl=0"} {
		l, err := FetchListing(st, link)
		if err != nil || len(l.Files) != 1 || l.Files[0].Name != "Outfit v1.2.zip" || l.Files[0].Size != 7000 || l.Title != "Outfit v1.2" || l.Count != 1 {
			t.Errorf("%s: %+v %v", link, l, err)
		}
	}
	// a shared folder: one zip, size unknown until it comes
	l, err := FetchListing(st, "https://www.dropbox.com/scl/fo/fo1234567890abc/h?rlkey=zz&dl=0")
	if err != nil || len(l.Files) != 1 || l.Files[0].Name != "Kaguya Outfits.zip" || l.Files[0].Size != 0 || l.Title != "Kaguya Outfits" {
		t.Errorf("folder: %+v %v", l, err)
	}
	// what Dropbox says instead of the file
	if _, err := FetchListing(st, "https://www.dropbox.com/scl/fi/pwd4567890abcde/x.zip?rlkey=a&dl=0"); !errors.Is(err, errDBPage) || ErrSettled(err.Error()) {
		t.Errorf("password page: %v", err)
	}
	if _, err := FetchListing(st, "https://www.dropbox.com/s/busy123456789ab/x.zip"); !errors.Is(err, ErrDBTraffic) || !Temporary(err) {
		t.Errorf("traffic: %v", err)
	}
	if _, err := FetchListing(st, "https://www.dropbox.com/s/gone123456789ab/x.zip"); !errors.Is(err, errDBGone) || !ErrSettled(err.Error()) {
		t.Errorf("gone: %v", err)
	}
	// the download, continued
	src := SourceFor(Parse("https://www.dropbox.com/scl/fi/abc123def456ghi/Outfit_v1.2.zip?rlkey=k1k2k3k4k5k6&dl=0"), nil)
	resp, err := src.Open(context.Background(), Client(st), 5000, `"db-etag"`)
	if err != nil {
		t.Fatal(err)
	}
	if body := readAll(t, resp); len(body) != 2000 || resp.StatusCode != 206 || TotalSize(resp) != 7000 || FileName(resp) != "Outfit v1.2.zip" {
		t.Errorf("resume: %d bytes, %d, %d, %q", len(body), resp.StatusCode, TotalSize(resp), FileName(resp))
	}
	n, _ := hits.Load("/scl/fi/abc123def456ghi/Outfit_v1.2.zip")
	if n == nil || *n.(*int) != 2 {
		t.Errorf("requests to the file: %v", n)
	}
}

func TestFileNames(t *testing.T) {
	for cd, want := range map[string]string{
		`attachment; filename="a.zip"`:                                 "a.zip",
		`attachment; filename="a.zip"; filename*=UTF-8''%E8%A1%A3.zip`: "衣.zip",
		`attachment;filename*=UTF-8''Outfit%20v1.zip`:                  "Outfit v1.zip",
		`attachment; filename=plain.zip`:                               "plain.zip",
		`attachment; filename="..\\evil\\x.zip"`:                       "x.zip",
		`inline`:                                                       "",
		``:                                                             "",
		`attachment; filename="broken; filename*=UTF-8''fixed%20name.zip`: "fixed name.zip",
	} {
		resp := &http.Response{Header: http.Header{}}
		if cd != "" {
			resp.Header.Set("Content-Disposition", cd)
		}
		if got := FileName(resp); got != want {
			t.Errorf("%s: %q, want %q", cd, got, want)
		}
	}
}

func parseURL(s string) (*url.URL, error) { return url.Parse(s) }

// pageDrive: drive.google.com as a handler of the test's own.
func pageDrive(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_GDRIVE_BASE", srv.URL)
	t.Setenv("VRCLIB_GDRIVE_API", srv.URL)
	t.Setenv("VRCLIB_GDRIVE_KEY", "")
	core.DataDir = t.TempDir()
	gap := gdGap
	gdGap = 0
	t.Cleanup(func() { gdGap = gap })
}

func folderPage(w http.ResponseWriter, title string, entries ...string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<html><head><title>%s - Google Drive</title></head><body><div class="flip-entries">`, title)
	for i := 0; i+1 < len(entries); i += 2 {
		fmt.Fprintf(w, `<div class="flip-entry"><a href="%s"><div class="flip-entry-title">%s</div></a></div>`, entries[i], entries[i+1])
	}
	fmt.Fprint(w, `</div></body></html>`)
}

// A sub-folder that cannot be read just now fails the whole read — half a listing is not kept as the share's;
// one that is gone or closed, or beyond the limits, is kept and marked.
func TestDriveSubFolderUnread(t *testing.T) {
	var sub atomic.Int32 // what the sub-folder answers with
	pageDrive(t, func(w http.ResponseWriter, r *http.Request) {
		switch id := r.URL.Query().Get("id"); {
		case r.URL.Path == "/drive/v3/files" && strings.Contains(r.URL.Query().Get("q"), "1SubFolder000000000000000000"), id == "1SubFolder000000000000000000":
			if s := int(sub.Load()); s != 200 {
				w.WriteHeader(s)
				return
			}
			if r.URL.Path == "/drive/v3/files" {
				fmt.Fprint(w, `{"files":[{"id":"1FileT0000000000000000000000","name":"T.png","mimeType":"image/png","size":"5"}]}`)
				return
			}
			folderPage(w, "Textures", "https://drive.google.com/file/d/1FileT0000000000000000000000/view", "T.png")
		case r.URL.Path == "/drive/v3/files/1Folder000000000000000000000":
			fmt.Fprint(w, `{"id":"1Folder000000000000000000000","name":"Pack","mimeType":"application/vnd.google-apps.folder"}`)
		case r.URL.Path == "/drive/v3/files":
			fmt.Fprint(w, `{"files":[{"id":"1SubFolder000000000000000000","name":"Textures","mimeType":"application/vnd.google-apps.folder"},{"id":"1FileA0000000000000000000000","name":"A.zip","mimeType":"application/zip","size":"7"}]}`)
		case r.URL.Path == "/embeddedfolderview":
			folderPage(w, "Pack", "https://drive.google.com/drive/folders/1SubFolder000000000000000000", "Textures", "https://drive.google.com/file/d/1FileA0000000000000000000000/view", "A.zip")
		default:
			w.WriteHeader(404)
		}
	})
	st := &core.Store{}
	link := "https://drive.google.com/drive/folders/1Folder000000000000000000000"
	for _, key := range []string{"", "AIzaTestKey"} {
		t.Setenv("VRCLIB_GDRIVE_KEY", key)
		for _, status := range []int32{429, 500} {
			sub.Store(status)
			if l, err := FetchListing(st, link); err == nil || !Temporary(err) || l != nil {
				t.Errorf("key %q, sub-folder answers %d: listing %+v, err %v", key, status, l, err)
			}
		}
		sub.Store(404) // gone for good: the rest of the share is still its listing, marked as not all of it
		l, err := FetchListing(st, link)
		if err != nil || !l.Truncated || l.Count != 1 || len(l.Files) != 2 || !l.Files[0].Dir || !l.Files[0].Partial || len(l.Files[0].Children) != 0 {
			t.Fatalf("key %q, sub-folder gone: %+v %v", key, l, err)
		}
		sub.Store(200)
		l, err = FetchListing(st, link)
		if err != nil || l.Truncated || l.Count != 2 || l.Files[0].Partial || len(l.Files[0].Children) != 1 {
			t.Fatalf("key %q, all readable: %+v %v", key, l, err)
		}
	}
	// the limits: seven levels of folders, 1500 entries — kept, and the folder that was cut is marked
	out := &core.PanListing{}
	deep := func(k gdFile) ([]gdFile, error) {
		return []gdFile{{ID: k.ID + "x", Name: "deeper", MimeType: gdFolderMime}, {ID: k.ID + "f", Name: "f.png"}}, nil
	}
	files, err := gdWalk(out, []gdFile{{ID: "d", Name: "top", MimeType: gdFolderMime}}, deep)
	if err != nil || !out.Truncated || out.Count != 6 {
		t.Fatalf("deep tree: %v, truncated %v, %d files", err, out.Truncated, out.Count)
	}
	f := files[0]
	for depth := 0; depth < 6; depth++ {
		if f.Partial {
			t.Errorf("level %d is marked as cut", depth)
		}
		f = f.Children[0]
	}
	if !f.Partial || len(f.Children) != 0 {
		t.Errorf("the seventh level: %+v", f)
	}
	out = &core.PanListing{}
	var many []gdFile
	for i := 0; i < 1600; i++ {
		many = append(many, gdFile{ID: fmt.Sprint("f", i), Name: fmt.Sprintf("f%04d.png", i)})
	}
	files, err = gdWalk(out, []gdFile{{ID: "big", Name: "big", MimeType: gdFolderMime}, {ID: "z", Name: "z.png"}}, func(gdFile) ([]gdFile, error) { return many, nil })
	if err != nil || !out.Truncated || len(files) != 1 || !files[0].Partial || len(files[0].Children) != 1499 {
		t.Errorf("many entries: %v, truncated %v, %d at the top", err, out.Truncated, len(files))
	}
	// a read that is cancelled ends between two folders
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t.Setenv("VRCLIB_GDRIVE_KEY", "")
	if _, err := FetchListingCtx(ctx, st, link); err == nil {
		t.Error("a cancelled read went through")
	}
}

// A file that is a web page itself (README.html) is a file: it comes as an attachment. The pages Google and
// Dropbox show instead of a file do not.
func TestHTMLFileIsAFile(t *testing.T) {
	const body = "<!DOCTYPE html><html><body>terms of use</body></html>"
	pageDrive(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="README.html"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		fmt.Fprint(w, body)
	})
	t.Setenv("VRCLIB_DROPBOX_BASE", gdriveWeb())
	st := &core.Store{}
	for _, link := range []string{"https://drive.google.com/file/d/1FileR0000000000000000000000/view", "https://www.dropbox.com/s/abc123def456ghi/README.html?dl=0"} {
		l, err := FetchListing(st, link)
		if err != nil || len(l.Files) != 1 || l.Files[0].Name != "README.html" || l.Files[0].Size != int64(len(body)) {
			t.Fatalf("%s: %+v %v", link, l, err)
		}
		resp, err := SourceFor(Parse(link), l.Files[0]).Open(context.Background(), Client(st), 0, "")
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		if got := readAll(t, resp); got != body {
			t.Errorf("%s: %q", link, got)
		}
	}
}

// Google's "too many users" page says what it says whatever status it is sent with; a page that says nothing
// this program knows is read by its status.
func TestDriveNoticeBeforeStatus(t *testing.T) {
	const quota = `<html><body><p class="uc-error-caption">Sorry, you can't view or download this file at this time.</p><p class="uc-error-subcaption">Too many users have viewed or downloaded this file recently. Please try accessing the file again later.</p></body></html>`
	var status, hits atomic.Int32
	var page atomic.Value
	page.Store(quota)
	pageDrive(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(int(status.Load()))
		fmt.Fprint(w, page.Load())
	})
	st := &core.Store{}
	src := Source{Service: GDrive, ID: "1FileA0000000000000000000000"}
	for _, s := range []int32{200, 403, 429} {
		status.Store(s)
		hits.Store(0)
		if _, err := src.Open(context.Background(), Client(st), 0, ""); !errors.Is(err, ErrGDQuota) || Temporary(err) || hits.Load() != 1 {
			t.Errorf("the quota page with HTTP %d, download: %v after %d requests", s, err, hits.Load())
		}
		if _, err := FetchListing(st, "https://drive.google.com/uc?id=1FileA0000000000000000000000"); !errors.Is(err, ErrGDQuota) {
			t.Errorf("the quota page with HTTP %d, listing: %v", s, err)
		}
	}
	page.Store("<html><body>Error</body></html>")
	for s, want := range map[int32]func(error) bool{403: func(err error) bool { return errors.Is(err, errGDDenied) }, 404: Gone, 429: Temporary, 503: Temporary, 416: ErrRange} {
		status.Store(s)
		if _, err := src.Open(context.Background(), Client(st), 0, ""); !want(err) {
			t.Errorf("a page with HTTP %d: %v", s, err)
		}
	}
	// a notice of Google's own that is not one of the known ones: said as it is when the status is fine,
	// tried again when the status says "later"
	page.Store(`<html><body><p class="uc-error-subcaption">Something else went wrong.</p></body></html>`)
	status.Store(200)
	if _, err := src.Open(context.Background(), Client(st), 0, ""); err == nil || !strings.Contains(err.Error(), "Something else went wrong") {
		t.Errorf("an unknown notice: %v", err)
	}
	status.Store(503)
	if _, err := src.Open(context.Background(), Client(st), 0, ""); !Temporary(err) {
		t.Errorf("an unknown notice with HTTP 503: %v", err)
	}
}

// "open?id=…" may be a folder's link: when the id does not answer as a file, it is tried as a folder. Entries
// with a resourcekey of their own are asked for with it. A notice where the entries should be is no listing.
func TestDriveFolderPages(t *testing.T) {
	var mu sync.Mutex
	asked := map[string]string{} // id → the resourcekey it was asked with
	notice := ""
	keyOf := func(what string) string {
		mu.Lock()
		defer mu.Unlock()
		return asked[what]
	}
	pageDrive(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		asked[r.URL.Path+" "+q.Get("id")] = q.Get("resourcekey")
		notice := notice
		mu.Unlock()
		switch {
		case r.URL.Path == "/embeddedfolderview" && notice != "":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, notice)
		case r.URL.Path == "/embeddedfolderview" && q.Get("id") == "1Folder000000000000000000000":
			folderPage(w, "Pack", "https://drive.google.com/drive/folders/1SubFolder000000000000000000?resourcekey=0-SubKey_1&amp;usp=drive_web", "Old",
				"https://drive.google.com/file/d/1FileA0000000000000000000000/view?usp=drive_web&amp;resourcekey=0-FileKey_2", "A.zip",
				"https://drive.google.com/file/d/1FileB0000000000000000000000/view?usp=drive_web", "B.zip")
		case r.URL.Path == "/embeddedfolderview" && q.Get("id") == "1SubFolder000000000000000000":
			folderPage(w, "Old", "https://drive.google.com/file/d/1FileT0000000000000000000000/view", "T.png")
		case r.URL.Path == "/embeddedfolderview" && q.Get("id") == "1Empty0000000000000000000000":
			folderPage(w, "Empty")
		case r.URL.Path == "/uc" && strings.HasPrefix(q.Get("id"), "1File"):
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="x.zip"`)
			fmt.Fprint(w, "PK")
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, "<html><body>Not Found</body></html>")
		}
	})
	st := &core.Store{}
	l, err := FetchListing(st, "https://drive.google.com/open?id=1Folder000000000000000000000&resourcekey=0-LinkKey_0")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(l.Files, ""), " "); got != "/Old/ /Old/T.png(0) /A.zip(0) /B.zip(0)" || l.Title != "Pack" || l.Count != 3 || l.Surl != "gd:1Folder000000000000000000000" {
		t.Errorf("a folder behind open?id=: %s (%s, %d)", got, l.Title, l.Count)
	}
	if keyOf("/embeddedfolderview 1Folder000000000000000000000") != "0-LinkKey_0" || keyOf("/embeddedfolderview 1SubFolder000000000000000000") != "0-SubKey_1" {
		t.Errorf("folders asked with %q and %q", keyOf("/embeddedfolderview 1Folder000000000000000000000"), keyOf("/embeddedfolderview 1SubFolder000000000000000000"))
	}
	link := Parse("https://drive.google.com/open?id=1Folder000000000000000000000&resourcekey=0-LinkKey_0")
	for _, c := range []struct {
		f   *core.PanFile
		key string
	}{{l.Files[1], "0-FileKey_2"}, {l.Files[2], "0-LinkKey_0"}} {
		src := SourceFor(link, c.f)
		resp, err := src.Open(context.Background(), Client(st), 0, "")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if src.RKey != c.key || keyOf("/uc "+c.f.Ref) != c.key {
			t.Errorf("%s asked with %q (source %q), want %q", c.f.Name, keyOf("/uc "+c.f.Ref), src.RKey, c.key)
		}
	}
	// an id that is neither: the file's answer stands; an empty folder is no reason to call a dead file link one
	if _, err := FetchListing(st, "https://drive.google.com/open?id=1Nope0000000000000000000000"); !errors.Is(err, errGDGone) {
		t.Errorf("neither a file nor a folder: %v", err)
	}
	if _, err := FetchListing(st, "https://drive.google.com/open?id=1Empty0000000000000000000000"); !errors.Is(err, errGDGone) {
		t.Errorf("an empty folder behind open?id=: %v", err)
	}
	if l, err := FetchListing(st, "https://drive.google.com/drive/folders/1Empty0000000000000000000000"); err != nil || len(l.Files) != 0 || l.Title != "Empty" {
		t.Errorf("an empty folder: %+v %v", l, err)
	}
	// a folder that is not public, answered with a page instead of a redirect to the sign-in
	for _, n := range []string{
		`<html><head><title>Google Drive: Sign-in</title></head><body><a href="https://accounts.google.com/ServiceLogin?continue=x">Sign in</a></body></html>`,
		`<html><head><title>Access denied - Google Drive</title></head><body><p>You need access</p><button>Request access</button></body></html>`,
	} {
		mu.Lock()
		notice = n
		mu.Unlock()
		if l, err := FetchListing(st, "https://drive.google.com/drive/folders/1Folder000000000000000000000"); !errors.Is(err, errGDDenied) || l != nil {
			t.Errorf("a notice instead of the entries: %+v %v", l, err)
		}
	}
}

// The link a download is asked with keeps a name's characters (the path is escaped, not rewritten).
func TestDropboxDownloadURL(t *testing.T) {
	t.Setenv("VRCLIB_DROPBOX_BASE", "")
	l := Parse("https://www.dropbox.com/scl/fi/abc123def456ghi/髪型（新）.zip?rlkey=k1k2k3k4k5k6&dl=0")
	u, err := url.Parse(dbDownloadURL(l.URL))
	if err != nil || u.Path != "/scl/fi/abc123def456ghi/髪型（新）.zip" || u.Query().Get("rlkey") != "k1k2k3k4k5k6" || u.Query().Get("dl") != "1" {
		t.Errorf("%v %v", u, err)
	}
	l = Parse("https://www.dropbox.com/s/abc123def456ghi/Dress%20(v2)?dl=0")
	if u, _ := url.Parse(dbDownloadURL(l.URL)); u == nil || u.Path != "/s/abc123def456ghi/Dress (v2)" {
		t.Errorf("%v", u)
	}
}
