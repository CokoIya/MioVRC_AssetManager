package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/server"
)

// The heartbeat ends the program only for a page in a browser that has stopped answering, never for the
// program's own window, and not while something is being downloaded.
func TestPageGone(t *testing.T) {
	now := time.Now().Unix()
	defer func() {
		server.EverPing.Store(false)
		nativeUI.Store(false)
		core.Downloading.Store(0)
		core.PurchaseBusy.Store(false)
	}()
	if pageGone(now) {
		t.Error("no page has been open yet")
	}
	server.EverPing.Store(true)
	server.LastPing.Store(now - 60)
	if pageGone(now) {
		t.Error("60 seconds without a ping is not gone")
	}
	server.LastPing.Store(now - 76)
	if !pageGone(now) {
		t.Error("the page is gone")
	}
	core.Downloading.Store(2)
	if pageGone(now) {
		t.Error("gone while 2 downloads are running")
	}
	core.Downloading.Store(0)
	core.PurchaseBusy.Store(true)
	if pageGone(now) {
		t.Error("gone while the purchases are being read")
	}
	core.PurchaseBusy.Store(false)
	nativeUI.Store(true) // after the computer slept the window's page is late with its ping: the window stays
	if pageGone(now + 3600) {
		t.Error("the program's own window was given up")
	}
}

// The window cannot be made narrower than the layout needs, nor is its minimum larger than the window.
func TestMinWindowSize(t *testing.T) {
	for _, c := range []struct{ w, h, dpi, minW, minH int }{
		{1440, 900, 96, 800, 560},    // 55 % would be 792 × 540
		{2160, 1350, 144, 1200, 840}, // 150 % scaling
		{2880, 1800, 96, 1584, 1080}, // a huge window: the share of it
		{1257, 706, 96, 800, 560},    // 1366 × 768 screen
		{736, 480, 96, 736, 480},     // a screen smaller than the layout: the window as it is
		{1177, 662, 144, 1177, 662},  // 150 % on a small screen
	} {
		if w, h := minWindowSize(c.w, c.h, c.dpi); w != c.minW || h != c.minH {
			t.Errorf("window %d × %d at %d dpi: minimum %d × %d, want %d × %d", c.w, c.h, c.dpi, w, h, c.minW, c.minH)
		}
	}
}

// Who holds the port: this program, this program on its way out, or something else altogether.
func TestOtherCopy(t *testing.T) {
	answer := `{"app":"vrclib","rev":3}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(answer)) }))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	if ours, quitting := otherCopy(addr); !ours || quitting {
		t.Errorf("a running copy: ours %v quitting %v", ours, quitting)
	}
	answer = `{"app":"vrclib","rev":3,"quitting":true}`
	if ours, quitting := otherCopy(addr); !ours || !quitting {
		t.Errorf("a copy on its way out: ours %v quitting %v", ours, quitting)
	}
	for _, other := range []string{`<html>some other program</html>`, `{"app":"other"}`, `{}`} {
		answer = other
		if ours, _ := otherCopy(addr); ours {
			t.Errorf("%s was taken for this program", other)
		}
	}
	// the real server's answer, through its Host check
	core.DataDir = t.TempDir()
	real := httptest.NewUnstartedServer(nil)
	real.Config.Handler = server.LocalOnly(real.Listener.Addr(), server.NewMux(core.LoadStore(core.DataDir+"/library.json")))
	real.Start()
	defer real.Close()
	core.Quitting.Store(true)
	ours, quitting := otherCopy(real.Listener.Addr().String())
	core.Quitting.Store(false)
	if !ours || !quitting {
		t.Errorf("the real server while quitting: ours %v quitting %v", ours, quitting)
	}
	server.EverPing.Store(false)
}

// A copy started while the old one is going away takes the usual port as soon as it is free.
func TestWaitForPort(t *testing.T) {
	old, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := old.Addr().String()
	if ln := waitForPort(addr, 300*time.Millisecond); ln != nil {
		ln.Close()
		t.Fatal("got a port that is held")
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		old.Close()
	}()
	start := time.Now()
	ln := waitForPort(addr, 10*time.Second)
	if ln == nil {
		t.Fatal("the port was not taken once it was free")
	}
	ln.Close()
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
}
