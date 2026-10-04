package purchases

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// quickFetch: no real waiting between tries, and a connection counts as silent after a moment.
func quickFetch(t *testing.T) {
	t.Helper()
	pause, stall := fetchPause, StallAfter
	fetchPause, StallAfter = time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { fetchPause, StallAfter = pause, stall })
}

func fetchTo(t *testing.T, u, name string) (dst string, err error) {
	t.Helper()
	st := testkit.NewStore(t)
	dst = filepath.Join(t.TempDir(), name)
	return dst, fetchFile(context.Background(), st, u, dst, &DLJob{ID: "1", Name: name})
}

// A page answered where the file should be is not kept as the file, and not asked for three times.
func TestFetchRejectsPage(t *testing.T) {
	quickFetch(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/sniff" {
			w.Header().Set("Content-Type", "application/octet-stream")
			fmt.Fprint(w, "\n <!DOCTYPE html><html><body>maintenance</body></html>")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body>Access denied / maintenance</body></html>")
	}))
	defer srv.Close()
	for _, p := range []string{"/x", "/sniff", "/users/sign_in"} {
		hits.Store(0)
		dst, err := fetchTo(t, srv.URL+p, "Avatar.zip")
		if !errors.Is(err, errNotFile) || !strings.Contains(err.Error(), "网页") {
			t.Errorf("%s: err %v", p, err)
		}
		if onLoginPage(err) != (p == "/users/sign_in") {
			t.Errorf("%s: taken for the login page: %v", p, onLoginPage(err))
		}
		if core.StatOK(dst) || core.StatOK(dst+".part") || hits.Load() != 1 {
			t.Errorf("%s: the page was kept, or asked for %d times", p, hits.Load())
		}
	}
	// a page that is what was asked for stays a page
	if dst, err := fetchTo(t, srv.URL+"/x", "readme.html"); err != nil || !core.StatOK(dst) {
		t.Errorf("an .html download: %v", err)
	}
}

// A connection that ends before the file does: the next try asks for the rest, and only for the same file.
func TestFetchResumes(t *testing.T) {
	quickFetch(t)
	data := bytes.Repeat([]byte("0123456789abcdef"), 20000)
	var mu sync.Mutex
	var ranges, ifRanges []string
	cut := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges, ifRanges = append(ranges, r.Header.Get("Range")), append(ifRanges, r.Header.Get("If-Range"))
		first := cut
		cut = false
		mu.Unlock()
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "application/zip")
		if first {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			_, _ = w.Write(data[:len(data)/3])
			c, _, _ := w.(http.Hijacker).Hijack()
			c.Close()
			return
		}
		var a int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &a); err != nil || r.URL.Path == "/norange" {
			_, _ = w.Write(data) // a server that does not do ranges sends all of it again
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[a:])
	}))
	defer srv.Close()
	for _, p := range []string{"/file", "/norange"} {
		mu.Lock()
		cut, ranges, ifRanges = true, nil, nil
		mu.Unlock()
		dst, err := fetchTo(t, srv.URL+p, "Big.zip")
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		got, _ := os.ReadFile(dst)
		if !bytes.Equal(got, data) || core.StatOK(dst+".part") {
			t.Fatalf("%s: file of %d bytes, want %d", p, len(got), len(data))
		}
		if len(ranges) != 2 || ranges[0] != "" || !strings.HasPrefix(ranges[1], "bytes=") || ranges[1] == "bytes=0-" || ifRanges[1] != `"v1"` {
			t.Errorf("%s: ranges %q if-range %q", p, ranges, ifRanges)
		}
	}
}

// Without a length the only sign of a connection that ended early is the file itself.
func TestFetchCutShortWithoutLength(t *testing.T) {
	quickFetch(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = c.Read(make([]byte, 4096))
				_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: application/zip\r\n\r\n" + strings.Repeat("A", 1000)))
				c.Close()
			}()
		}
	}()
	dst, err := fetchTo(t, "http://"+ln.Addr().String()+"/x", "Big.zip")
	if err == nil || !strings.Contains(err.Error(), "不完整") || core.StatOK(dst) {
		t.Errorf("a zip cut short was kept: %v", err)
	}
}

// What the disk refuses is said once, in words, and not asked of the server again.
func TestFetchLocalErrors(t *testing.T) {
	quickFetch(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(1<<20))
		_, _ = w.Write(bytes.Repeat([]byte("A"), 1<<20))
	}))
	defer srv.Close()
	st := testkit.NewStore(t)
	dst := filepath.Join(t.TempDir(), "missing-dir", "Big.zip")
	err := fetchFile(context.Background(), st, srv.URL+"/x", dst, &DLJob{ID: "1", Name: "Big.zip"})
	if !IsLocalErr(err) || !strings.Contains(err.Error(), "无法写入「") || hits.Load() != 1 {
		t.Errorf("a folder that is not there: %v after %d requests", err, hits.Load())
	}
	// a disk with less room than the file needs: nothing is written at all
	free := DiskFree
	DiskFree = func(string) uint64 { return 4096 }
	defer func() { DiskFree = free }()
	hits.Store(0)
	dst = filepath.Join(t.TempDir(), "Big.zip")
	err = fetchFile(context.Background(), st, srv.URL+"/x", dst, &DLJob{ID: "1", Name: "Big.zip"})
	if !IsLocalErr(err) || !strings.Contains(err.Error(), "磁盘空间不足") || hits.Load() != 1 || core.StatOK(dst+".part") {
		t.Errorf("a full disk: %v after %d requests", err, hits.Load())
	}
	if e := WriteErr("D:\\x.zip", &os.PathError{Op: "write", Path: "x", Err: os.ErrPermission}); !strings.Contains(e.Error(), "没有写入权限") {
		t.Errorf("no permission: %v", e)
	}
}

// silentServer accepts connections and never says a word.
func silentServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	return ln.Addr().String()
}

// A server that accepts the connection and then stays silent does not hold the queue for ever, and cancel
// ends the request instead of waiting for it.
func TestFetchSilentServer(t *testing.T) {
	quickFetch(t)
	addr := silentServer(t)
	st := testkit.NewStore(t)
	for _, scheme := range []string{"http://", "https://"} { // silent before the answer, and during the TLS handshake
		start := time.Now()
		err := fetchFile(context.Background(), st, scheme+addr+"/x.zip", filepath.Join(t.TempDir(), "x.zip"), &DLJob{ID: "1", Name: "x.zip"})
		if err == nil || !strings.Contains(err.Error(), "未收到数据") || time.Since(start) > 10*time.Second {
			t.Errorf("%s: %v after %v", scheme, err, time.Since(start))
		}
	}
	StallAfter = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- fetchFile(ctx, st, "https://"+addr+"/x.zip", filepath.Join(t.TempDir(), "x.zip"), &DLJob{ID: "1", Name: "x.zip"})
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrCancelled) {
			t.Errorf("cancelled: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not end the request")
	}
}

// The file's address is signed and only good for a while: when it is refused, a new one is asked for and the
// download goes on. Nothing of the address reaches the error text.
func TestDownloadAsksAddressAgain(t *testing.T) {
	quickFetch(t)
	var resolves atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/downloadables/"):
			http.Redirect(w, r, fmt.Sprintf("/cdn/Coat.unitypackage?sig=secret%d", resolves.Add(1)), http.StatusFound)
		case r.URL.Query().Get("sig") == "secret1":
			w.WriteHeader(http.StatusForbidden)
		default:
			_, _ = w.Write([]byte("package"))
		}
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_BOOTH_DL", srv.URL)
	st := testkit.NewStore(t)
	st.Settings.DownloadDir = t.TempDir()
	_ = saveBoothSession([]core.SavedCookie{{Name: boothSessionCookie, Value: "good", Expires: time.Now().Add(time.Hour).Unix()}})
	j := &DLJob{ID: "77", Item: "7000001", Name: "Coat.unitypackage", Status: "running"}
	if err := downloadJob(context.Background(), st, j); err != nil || resolves.Load() != 2 || j.Status != "done" {
		t.Fatalf("err %v after %d addresses, job %+v", err, resolves.Load(), j)
	}
	srv.Close()
	err := fetchFile(context.Background(), st, srv.URL+"/cdn/x.zip?sig=secret9", filepath.Join(t.TempDir(), "x.zip"), &DLJob{ID: "1"})
	if err == nil || strings.Contains(err.Error(), "secret9") || strings.Contains(err.Error(), "cdn") {
		t.Errorf("the address is in the error: %v", err)
	}
	if got := LogURL("https://cdn.example.com/a/b.zip?X-Amz-Signature=abc&Expires=1"); got != "cdn.example.com/a/b.zip" {
		t.Errorf("log address %q", got)
	}
}

// An id that is not a number never becomes part of a booth.pm address (the session cookie goes with it).
func TestDownloadIDIsANumber(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	t.Setenv("VRCLIB_BOOTH_DL", srv.URL)
	st := testkit.NewStore(t)
	_ = saveBoothSession([]core.SavedCookie{{Name: boothSessionCookie, Value: "SECRET", Expires: time.Now().Add(time.Hour).Unix()}})
	for _, id := range []string{"../users/edit?x=1#", "12/../3", "12?x", " 12", "", "１２"} {
		if _, _, err := resolveDownload(context.Background(), st, id); err == nil || errors.Is(err, errNeedLogin) {
			t.Errorf("%q: %v", id, err)
		}
		if n := QueueDownloadFromPage(st, id, "", "", "x.zip"); n != 0 {
			t.Errorf("%q was queued", id)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests went out", hits.Load())
	}
}
