package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
)

func directStore(t *testing.T) *core.Store {
	t.Helper()
	core.DataDir = t.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	st.Settings.Proxy = "direct"
	return st
}

// shakyServer serves file, at most chunk bytes per connection: then it drops the connection (drop), or
// goes silent while keeping it open. Requests for the rest (Range) are answered when ranges is true.
func shakyServer(t *testing.T, file []byte, chunk int, drop, ranges bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	quiet := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		from := 0
		if rg := r.Header.Get("Range"); rg != "" && ranges {
			from, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(file)-1, len(file)))
			w.Header().Set("Content-Length", strconv.Itoa(len(file)-from))
			w.WriteHeader(206)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(file)))
		}
		end := min(from+chunk, len(file))
		_, _ = w.Write(file[from:end])
		w.(http.Flusher).Flush()
		if end == len(file) {
			return
		}
		if !drop {
			select { // says nothing more, and does not hang up either
			case <-quiet:
			case <-r.Context().Done():
			}
		}
		panic(http.ErrAbortHandler) // the connection is cut in the middle of the file
	}))
	t.Cleanup(func() { close(quiet); srv.Close() })
	return srv, &requests
}

func shortWaits(t *testing.T, stall time.Duration) {
	t.Helper()
	oldStall, oldWait := dlStall, dlRetryWait
	dlStall, dlRetryWait = stall, time.Millisecond
	t.Cleanup(func() { dlStall, dlRetryWait = oldStall, oldWait })
}

// A connection that breaks or goes silent in the middle of the download: the download goes on from where it
// stopped instead of failing, however long the whole of it takes.
func TestDownloadResumes(t *testing.T) {
	file := bytes.Repeat([]byte("MioVRCA installer "), 20000) // 360 KB
	sum := sha256.Sum256(file)
	for _, c := range []struct {
		what         string
		drop, ranges bool
		chunk        int
	}{
		{"connections that break", true, true, 100 << 10},
		{"connections that go silent", false, true, 150 << 10},
	} {
		st := directStore(t)
		shortWaits(t, 300*time.Millisecond)
		srv, requests := shakyServer(t, file, c.chunk, c.drop, c.ranges)
		dst := filepath.Join(t.TempDir(), "setup.exe")
		a := &core.UpdateAsset{Name: "setup.exe", URL: srv.URL + "/setup.exe", Size: int64(len(file)), Digest: "sha256:" + hex.EncodeToString(sum[:])}
		if err := download(st, a, dst, &core.Task{}); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if got, _ := os.ReadFile(dst); !bytes.Equal(got, file) {
			t.Errorf("%s: the file is not what the server has (%d bytes)", c.what, len(got))
		}
		if n := int(requests.Load()); n != (len(file)+c.chunk-1)/c.chunk {
			t.Errorf("%s: %d requests", c.what, n)
		}
	}
}

// A server that cannot continue a download starts over each time and never gets further: that ends with an
// error (not with half a file taken for the whole), after a few tries.
func TestDownloadGivesUp(t *testing.T) {
	file := bytes.Repeat([]byte("x"), 300<<10)
	st := directStore(t)
	shortWaits(t, 300*time.Millisecond)
	srv, requests := shakyServer(t, file, 100<<10, true, false)
	dst := filepath.Join(t.TempDir(), "setup.exe")
	err := download(st, &core.UpdateAsset{Name: "setup.exe", URL: srv.URL + "/setup.exe", Size: int64(len(file))}, dst, &core.Task{})
	if err == nil || !strings.Contains(err.Error(), "下载中断") {
		t.Fatalf("err %v", err)
	}
	if n := requests.Load(); n < dlTries || n > dlTries+1 {
		t.Errorf("%d requests", n)
	}
	// a file that is not there is not asked for again and again
	requests.Store(0)
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(404) }))
	defer gone.Close()
	if err := download(st, &core.UpdateAsset{URL: gone.URL + "/setup.exe"}, dst, &core.Task{}); err == nil || requests.Load() != 1 {
		t.Errorf("404: err %v after %d requests", err, requests.Load())
	}
}

// The release page gave the version but the question "is the installer there?" got no answer: that is not
// kept as "this version has no installer", and the next check asks again.
func TestUnansweredProbeIsNotKept(t *testing.T) {
	st := directStore(t)
	down := true
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/repos/"):
			w.WriteHeader(403)
		case strings.HasSuffix(p, "/releases/latest"):
			http.Redirect(w, r, srv.URL+"/"+UpdateRepo+"/releases/tag/v9.9.9", 302)
		case strings.HasSuffix(p, "-setup-9.9.9.exe") && down:
			w.WriteHeader(503)
		case strings.HasSuffix(p, "-setup-9.9.9.exe"):
			w.Header().Set("Content-Length", "777")
		default:
			w.WriteHeader(404) // no portable zip in this release: said by the site, so known
		}
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_GITHUB_API", srv.URL)
	t.Setenv("VRCLIB_GITHUB_SITE", srv.URL)
	info, err := CheckUpdate(st, false)
	if err != nil || info.Version != "9.9.9" || info.Zip != nil {
		t.Fatalf("%+v %v", info, err)
	}
	if info.Setup == nil || st.UpdateChecked != 0 {
		t.Fatalf("no answer about the installer: setup %+v, remembered as checked: %v", info.Setup, st.UpdateChecked != 0)
	}
	down = false
	if info, err = CheckUpdate(st, false); err != nil || info.Setup == nil || info.Setup.Size != 777 || st.UpdateChecked == 0 {
		t.Errorf("once the site answers: %+v %v", info.Setup, err)
	}
}

// What earlier updates downloaded is removed at the next start; other files in the folder are not touched.
func TestCleanDownloads(t *testing.T) {
	core.DataDir = t.TempDir()
	dir := filepath.Join(core.DataDir, "update")
	_ = os.MkdirAll(dir, 0755)
	for _, n := range []string{core.AppID + "-setup-1.7.4.exe", core.AppID + "-1.7.3.zip", "notes.txt"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0644)
	}
	CleanDownloads()
	left, _ := os.ReadDir(dir)
	if len(left) != 1 || left[0].Name() != "notes.txt" {
		t.Errorf("left: %v", left)
	}
}
