package pandl

import (
	"bytes"
	"context"
	"fmt"
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
)

// rangeServer: a netdisk that answers pieces of one file, counting the connections at once.
type rangeServer struct {
	data    []byte
	mu      sync.Mutex
	ranges  []string
	now     atomic.Int32 // requests being answered
	most    atomic.Int32 // … at most
	cut     atomic.Int32 // the next this many answers break off half way
	refuse  func(now int32) bool
	delay   time.Duration
	uas     []string
	stopped chan struct{} // closed once a request comes after the piece at offset stopAt began (cancel tests)
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/rest/2.0/pcs/file" {
		http.Redirect(w, r, "/file?p="+r.URL.Query().Get("path"), http.StatusFound)
		return
	}
	n := s.now.Add(1)
	defer s.now.Add(-1)
	for m := s.most.Load(); n > m && !s.most.CompareAndSwap(m, n); m = s.most.Load() {
	}
	s.mu.Lock()
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.uas = append(s.uas, r.UserAgent())
	s.mu.Unlock()
	if s.refuse != nil && s.refuse(n) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error_code":31326,"error_msg":"anti hotlinking"}`)
		return
	}
	var a, z int
	fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &z)
	chunk := s.data[a : z+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, z, len(s.data)))
	w.Header().Set("Content-Length", fmt.Sprint(len(chunk)))
	w.WriteHeader(206)
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.cut.Add(-1) >= 0 {
		_, _ = w.Write(chunk[:len(chunk)/3])
		if hj, ok := w.(http.Hijacker); ok {
			c, _, _ := hj.Hijack()
			c.Close()
		}
		return
	}
	_, _ = w.Write(chunk)
}

func smallPieces(t *testing.T) {
	t.Helper()
	w, minSize, piece := rangeWorkers, rangeMinSize, rangePiece
	rangeWorkers, rangeMinSize, rangePiece = 4, 64<<10, 32<<10
	t.Cleanup(func() { rangeWorkers, rangeMinSize, rangePiece = w, minSize, piece })
}

func newRangeRig(t *testing.T, size int) (*rangeServer, *bdClient, bdFile) {
	t.Helper()
	quickRetries(t)
	smallPieces(t)
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*7 + i/251)
	}
	s := &rangeServer{data: data}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_PCS_BASE", srv.URL)
	core.DataDir = t.TempDir()
	b := NewBDClient(&core.Store{}, &baiduSession{Cookies: []core.SavedCookie{{Name: "BDUSS", Value: "ok"}}})
	return s, b, bdFile{Path: "/MioVRCA/Big/Big.zip", Size: int64(size)}
}

// 超级会员: a large file comes over several connections at once, in pieces, and whole.
func TestPCSDownloadRanges(t *testing.T) {
	s, b, f := newRangeRig(t, 1<<20+12345)
	s.delay = 20 * time.Millisecond // (so that the pieces overlap)
	s.cut.Store(3)                  // three pieces break off half way: asked for again from where they stopped
	out := filepath.Join(t.TempDir(), "Big.zip")
	var mu sync.Mutex
	var got int64
	if err := b.download(f, out, rangeWorkers, func(n int64) { mu.Lock(); got += n; mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(out)
	if !bytes.Equal(b2, s.data) || got != f.Size {
		t.Fatalf("file %d bytes (same: %v), progress %d of %d", len(b2), bytes.Equal(b2, s.data), got, f.Size)
	}
	if m := s.most.Load(); m < 2 || m > int32(rangeWorkers) {
		t.Errorf("%d connections at once", m)
	}
	if core.FileExists(out+".part") || core.FileExists(out+".part.json") {
		t.Error("left behind: the part or its state")
	}
	// an account without 超级会员: one connection, the file in one request
	s2, b3, f2 := newRangeRig(t, 300<<10)
	out2 := filepath.Join(t.TempDir(), "One.zip")
	if err := b3.download(f2, out2, 1, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if b4, _ := os.ReadFile(out2); !bytes.Equal(b4, s2.data) || len(s2.ranges) != 1 || s2.most.Load() != 1 {
		t.Errorf("one connection: ranges %v, at once %d", s2.ranges, s2.most.Load())
	}
}

// A download in pieces that stops goes on where it was, in the next run: what is on the disk is not asked
// for again. One that went over one connection so far keeps what it has too.
func TestPCSDownloadRangesResume(t *testing.T) {
	s, b, f := newRangeRig(t, 800<<10)
	out := filepath.Join(t.TempDir(), "Big.zip")
	ctx, cancel := context.WithCancel(context.Background())
	b.ctx = ctx
	var got atomic.Int64
	err := b.download(f, out, rangeWorkers, func(n int64) {
		if got.Add(n) > f.Size/2 {
			cancel()
		}
	})
	if err != errPanCancelled {
		t.Fatalf("cancelled: %v", err)
	}
	st := readRangeState(out+".part", f)
	if st == nil || len(st.Pending) == 0 {
		t.Fatalf("no state kept: %+v", st)
	}
	var left int64
	for _, p := range st.Pending {
		left += p[1] - p[0]
	}
	if left >= f.Size || left <= 0 {
		t.Errorf("%d of %d left", left, f.Size)
	}
	// the next run, over one connection now (the account's membership ended): the state is used all the same
	s.mu.Lock()
	s.ranges = nil
	s.mu.Unlock()
	b2 := NewBDClient(&core.Store{}, &baiduSession{Cookies: []core.SavedCookie{{Name: "BDUSS", Value: "ok"}}})
	var again int64
	if err := b2.download(f, out, 1, func(n int64) { again += n }); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(out); !bytes.Equal(data, s.data) {
		t.Fatal("resumed file differs")
	}
	if again != f.Size {
		t.Errorf("progress %d of %d (what was there counts too)", again, f.Size)
	}
	for _, r := range s.ranges {
		var a, z int64
		fmt.Sscanf(r, "bytes=%d-%d", &a, &z)
		inside := false
		for _, p := range st.Pending {
			inside = inside || (a >= p[0] && z < p[1])
		}
		if !inside {
			t.Errorf("asked again for %s, which was on the disk (pending %v)", r, st.Pending)
		}
	}
	// a part begun over one connection: its bytes are kept, the rest comes in pieces
	s3, b3, f3 := newRangeRig(t, 500<<10)
	out3 := filepath.Join(t.TempDir(), "Half.zip")
	_ = os.WriteFile(out3+".part", s3.data[:200<<10], 0644)
	if err := b3.download(f3, out3, rangeWorkers, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(out3); !bytes.Equal(data, s3.data) {
		t.Fatal("file from a part differs")
	}
	for _, r := range s3.ranges {
		var a int64
		fmt.Sscanf(r, "bytes=%d-", &a)
		if a < 200<<10 {
			t.Errorf("asked for %s, which was in the part", r)
		}
	}
	// a state for another version of the file: begun anew
	s4, b4, f4 := newRangeRig(t, 300<<10)
	out4 := filepath.Join(t.TempDir(), "Other.zip")
	_ = os.WriteFile(out4+".part", bytes.Repeat([]byte("x"), 400<<10), 0644)
	_ = writeRangeState(out4+".part", &rangeState{Size: 400 << 10, Path: f4.Path, Pending: [][2]int64{{0, 1}}})
	if err := b4.download(f4, out4, rangeWorkers, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(out4); !bytes.Equal(data, s4.data) {
		t.Fatal("file after another version's state differs")
	}
}

// Baidu turns several connections away: the download goes on over one, as it always did.
func TestPCSDownloadRangesRefused(t *testing.T) {
	s, b, f := newRangeRig(t, 600<<10)
	s.delay = 15 * time.Millisecond
	s.refuse = func(now int32) bool { return now > 1 }
	out := filepath.Join(t.TempDir(), "Big.zip")
	if err := b.download(f, out, rangeWorkers, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(out); !bytes.Equal(data, s.data) {
		t.Fatal("file differs")
	}
	// and a login Baidu no longer takes ends it at once
	s2, b2, f2 := newRangeRig(t, 600<<10)
	s2.refuse = nil
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error_code":31045,"error_msg":"user not exists"}`)
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_PCS_BASE", srv.URL)
	if err := b2.download(f2, filepath.Join(t.TempDir(), "x.zip"), rangeWorkers, func(int64) {}); err != ErrBaiduLogin {
		t.Errorf("expired login: %v", err)
	}
	if n.Load() > int32(rangeWorkers) {
		t.Errorf("%d requests after the login was refused", n.Load())
	}
	_ = s2
	if !strings.Contains(fmt.Sprint(s.uas), "pan.baidu.com") {
		t.Errorf("user agents %v", s.uas)
	}
}
