package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseSharePage(t *testing.T) {
	fresh := `<html><script id="locals-data" type="application/json">
 {"share_uk":"3967098798","shareid":46386138315,"file_list":[{"fs_id":123,"server_filename":"A &amp; B","path":"/s/A","isdir":1,"size":0}]}
</script></html>`
	old := `<script>locals.mset({"bdstoken":"t","shareid":"46386138315","share_uk":"39","file_list":{"errno":0,"list":[{"fs_id":"9","server_filename":"x.zip","path":"/x.zip","isdir":0,"size":5}]}});</script>`
	s := parseSharePage([]byte(fresh))
	if s == nil || s.ShareID.String() != "46386138315" || len(s.FileList) != 1 || s.FileList[0].FsID.String() != "123" || rawName(s.FileList[0]) != "A & B" {
		t.Fatalf("locals-data: %+v", s)
	}
	s = parseSharePage([]byte(old))
	if s == nil || s.ShareUK.String() != "39" || len(s.FileList) != 1 || s.FileList[0].FsID.String() != "9" {
		t.Fatalf("locals.mset: %+v", s)
	}
	if parseSharePage([]byte("<html>请输入提取码</html>")) != nil {
		t.Error("no data on a code page")
	}
}

func TestPickBaiduCookies(t *testing.T) {
	cs := []savedCookie{{Name: "BDUSS", Value: "u", Domain: ".baidu.com"}, {Name: "STOKEN", Value: "passport", Domain: ".baidu.com"},
		{Name: "STOKEN", Value: "pan", Domain: "pan.baidu.com"}, {Name: "H_WISE_SIDS", Value: "x", Domain: ".baidu.com"},
		{Name: "BAIDUID", Value: "中文", Domain: ".baidu.com"}}
	got := pickBaiduCookies(cs)
	var names []string
	for _, c := range got {
		names = append(names, c.Name+"="+c.Value)
	}
	if strings.Join(names, ";") != "BDUSS=u;STOKEN=pan" {
		t.Errorf("got %v", names)
	}
}

func TestBaiduSessionFile(t *testing.T) {
	dataDir = t.TempDir()
	bdLoaded, bdCache = false, nil
	if loadBaiduSession() != nil {
		t.Fatal("nothing saved yet")
	}
	_ = saveBaiduSession(&baiduSession{Cookies: []savedCookie{{Name: "BDUSS", Value: "abc"}}, Name: "mio"})
	bdLoaded, bdCache = false, nil // as after a restart
	s := loadBaiduSession()
	if s == nil || s.Name != "mio" || !baiduAccount().LoggedIn {
		t.Fatalf("loaded %+v", s)
	}
	forgetBaiduSession()
	if _, err := os.Stat(baiduSessionFile()); err == nil || baiduAccount().LoggedIn {
		t.Error("still logged in after logging out")
	}
}

// The download goes on where a broken connection left it, and only with the netdisk client's name.
func TestPCSDownloadResume(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 40000)
	var cut atomic.Bool
	cut.Store(true)
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/2.0/pcs/file" {
			if r.UserAgent() != "pan.baidu.com" || !strings.Contains(r.Header.Get("Cookie"), "BDUSS=ok") {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"error_code":31045}`)
				return
			}
			http.Redirect(w, r, "/file?p="+r.URL.Query().Get("path"), http.StatusFound)
			return
		}
		var a, z int
		fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &z)
		ranges = append(ranges, r.Header.Get("Range"))
		chunk := data[a : z+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, z, len(data)))
		w.Header().Set("Content-Length", fmt.Sprint(len(chunk)))
		w.WriteHeader(206)
		if cut.Swap(false) {
			_, _ = w.Write(chunk[:len(chunk)/3])
			if hj, ok := w.(http.Hijacker); ok {
				c, _, _ := hj.Hijack()
				c.Close()
			}
			return
		}
		_, _ = w.Write(chunk)
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_PCS_BASE", srv.URL)
	dataDir = t.TempDir()
	st := &Store{}
	b := newBdClient(st, &baiduSession{Cookies: []savedCookie{{Name: "BDUSS", Value: "ok"}}})
	out := filepath.Join(t.TempDir(), "sub", "Dress.zip")
	var got int64
	if err := b.download(bdFile{Path: "/MioVRCA/Dress/Dress.zip", Size: int64(len(data))}, out, func(n int64) { got += n }); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(out)
	if !bytes.Equal(b2, data) || got != int64(len(data)) {
		t.Fatalf("file %d bytes, progress %d, ranges %v", len(b2), got, ranges)
	}
	if len(ranges) != 2 || ranges[0] != fmt.Sprintf("bytes=0-%d", len(data)-1) || !strings.HasPrefix(ranges[1], "bytes=") || ranges[1] == ranges[0] {
		t.Errorf("ranges %v", ranges)
	}
	// a login Baidu no longer takes
	b3 := newBdClient(st, &baiduSession{Cookies: []savedCookie{{Name: "BDUSS", Value: "old"}}})
	err := b3.download(bdFile{Path: "/x.zip", Size: 10}, filepath.Join(t.TempDir(), "x.zip"), func(int64) {})
	if err != errBaiduLogin {
		t.Errorf("expired login: %v", err)
	}
}

func TestPanPicks(t *testing.T) {
	got := normPanPaths([]string{"/Set/PSD/a.psd", "/Set/PSD", " ", "Set/Dress A.zip", "/Set/PSD b", "/Set/../Set/x.zip"})
	if strings.Join(got, "|") != "/Set/Dress A.zip|/Set/PSD|/Set/PSD b|/Set/x.zip" {
		t.Errorf("normalized %q", got)
	}
	if normPanPaths([]string{"/a", "/"}) != nil {
		t.Error("the whole card picked")
	}
	if !panCovered("/Set/PSD/a.psd", []string{"/Set/PSD"}) || panCovered("/Set/PSD b", []string{"/Set/PSD"}) || !panCovered("/x", []string{"/"}) {
		t.Error("covered")
	}
	if m := mergePanParts([]string{"/Set/PSD/a.psd"}, []string{"/Set/PSD", "/Set/b.zip"}); strings.Join(m, "|") != "/Set/PSD|/Set/b.zip" {
		t.Errorf("merged %q", m)
	}
	if m := mergePanParts([]string{"/Set/PSD"}, []string{"/"}); strings.Join(m, "|") != "/" {
		t.Errorf("merged with all %q", m)
	}
}
