package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// /rthumb is for the program's own pages: another site's page cannot have Booth's pictures fetched and kept
// through it. What it serves is a picture, says which kind, and cannot be read as a page — whatever the remote
// address ends in or the remote server says.
func TestRemoteThumbRoute(t *testing.T) {
	var pic bytes.Buffer
	_ = png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	var asked atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Header().Set("Content-Type", "image/png") // (counts for nothing)
		if r.URL.Path == "/i/cover.svg" {
			_, _ = w.Write(pic.Bytes()) // a PNG at an SVG's address
			return
		}
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`))
	}))
	defer remote.Close()
	t.Setenv("VRCLIB_BOOTH_WEB", remote.URL)
	_, srv := guarded(t)
	fetch := func(u, site string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/rthumb?u="+url.QueryEscape(u), nil)
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}
	cover := remote.URL + "/i/cover.svg"
	// asked for by another site's page: refused before anything is fetched
	for _, site := range []string{"cross-site", "same-site", "something-else"} {
		if resp, _ := fetch(cover, site); resp.StatusCode != 403 || asked.Load() != 0 {
			t.Errorf("Sec-Fetch-Site %s: %d (the remote was asked %d times)", site, resp.StatusCode, asked.Load())
		}
	}
	// the program's own page (an <img> of it), the address typed in, and a client that does not say
	for _, site := range []string{"same-origin", "none", ""} {
		resp, b := fetch(cover, site)
		h := resp.Header
		if resp.StatusCode != 200 || !bytes.Equal(b, pic.Bytes()) || h.Get("Content-Type") != "image/png" || h.Get("X-Content-Type-Options") != "nosniff" ||
			h.Get("Content-Security-Policy") != "default-src 'none'; sandbox" || !core.ContainsStr(h.Values("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("Sec-Fetch-Site %q: %d, %d bytes, headers %v", site, resp.StatusCode, len(b), h)
		}
	}
	if asked.Load() != 1 {
		t.Errorf("the remote was asked %d times for one picture", asked.Load())
	}
	// not a picture, whatever the address ends in: not served, not kept
	for _, p := range []string{"/i/a.jpg", "/i/b.svg", "/i/c.html", "/i/d.xml", "/i/e.png?x=.gif"} {
		if resp, b := fetch(remote.URL+p, "same-origin"); resp.StatusCode != 404 || bytes.Contains(b, []byte("<script>")) {
			t.Errorf("%s: %d %q (%s)", p, resp.StatusCode, b, resp.Header.Get("Content-Type"))
		}
	}
	if ents, _ := os.ReadDir(filepath.Join(core.CoversDir(), "remote")); len(ents) != 1 || filepath.Ext(ents[0].Name()) != ".png" {
		t.Errorf("kept: %v", ents)
	}
}

// The library's pictures (/thumb) and the AI's screenshots (/shot) are not guessed at by the browser either.
func TestPicturesNotSniffed(t *testing.T) {
	_, srv := guarded(t)
	var pic bytes.Buffer
	_ = png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	own := filepath.Join(core.CoversDir(), "own-x.png")
	shot := filepath.Join(core.DataDir, "shots", "step1.jpg")
	for p, b := range map[string][]byte{own: pic.Bytes(), shot: testkit.JPEG(8, 8, color.RGBA{R: 128, G: 128, B: 128, A: 255})} {
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/thumb?p=" + url.QueryEscape(own), "/thumb?w=64&p=" + url.QueryEscape(own), "/shot?f=step1.jpg"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Content-Type") != "image/jpeg" && resp.Header.Get("Content-Type") != "image/png" {
			t.Errorf("%s: %d, %v", path, resp.StatusCode, resp.Header)
		}
	}
}
