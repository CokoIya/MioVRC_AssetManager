package webpane

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
)

// devToolsStandIn: a browser's debugging port as the program meets it — /json/list and /json/version name the
// WebSocket address ws, and a WebSocket handshake there is taken. came counts who connected.
func devToolsStandIn(t *testing.T, ws string) (port int, came *atomic.Int32) {
	t.Helper()
	came = &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/list":
			fmt.Fprintf(w, `[{"id":"P1","type":"page","url":"https://booth.pm/","webSocketDebuggerUrl":%q}]`, ws)
		case "/json/version":
			fmt.Fprintf(w, `{"webSocketDebuggerUrl":%q}`, ws)
		default:
			came.Add(1)
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			_ = buf.Flush()
			time.Sleep(50 * time.Millisecond)
			conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().(*net.TCPAddr).Port, came
}

// listener: something else on this computer that would take a connection, and counts them.
func listener(t *testing.T) (int, *atomic.Int32) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	n := &atomic.Int32{}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			n.Add(1)
			c.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, n
}

// The DevTools connection goes to the debugging port of the browser the program started, on this computer, and
// nowhere else — whatever address the browser's answers name (the saved logins are sent over it).
func TestDevToolsAddressIsLocal(t *testing.T) {
	port, came := devToolsStandIn(t, "")
	p := strconv.Itoa(port)
	for _, ws := range []string{"ws://127.0.0.1:" + p + "/devtools/page/P1", "ws://localhost:" + p + "/devtools/browser/B", "ws://LOCALHOST:" + p + "/session"} {
		c, err := cdpDial(ws, port, 2*time.Second)
		if err != nil {
			t.Errorf("%s: %v", ws, err)
			continue
		}
		c.Close()
	}
	if came.Load() != 3 {
		t.Fatalf("%d connections", came.Load())
	}
	for _, ws := range []string{
		"", "ws://127.0.0.1:" + p, // (the port alone: right)
		"ws://evil.example:" + p + "/devtools/page/P1", "ws://127.0.0.1.evil.example:" + p + "/x", "ws://192.0.2.1:" + p + "/x", "ws://127.0.0.2:" + p + "/x",
		"ws://[::1]:" + p + "/x", "ws://0.0.0.0:" + p + "/x", "ws://user@127.0.0.1:" + p + "/x", "wss://127.0.0.1:" + p + "/x", "http://127.0.0.1:" + p + "/x",
		"ws:127.0.0.1:" + p, "ws://127.0.0.1/x", "ws://127.0.0.1:0" + p + "/x", "ws://127.0.0.1:" + strconv.Itoa(port+1) + "/x", "ws://127.0.0.1:" + p + "\r\nX: y",
	} {
		if ws == "ws://127.0.0.1:"+p {
			if _, err := devToolsAddr(ws, port); err != nil {
				t.Errorf("%s: %v", ws, err)
			}
			continue
		}
		if c, err := cdpDial(ws, port, 2*time.Second); !errors.Is(err, errDevToolsAddr) {
			if c != nil {
				c.Close()
			}
			t.Errorf("%s: %v", ws, err)
		}
	}
	if _, err := cdpDial("ws://127.0.0.1:"+p+"/x", 0, time.Second); !errors.Is(err, errDevToolsAddr) {
		t.Errorf("no port to expect: %v", err)
	}
	if came.Load() != 3 {
		t.Errorf("%d connections after the refused ones", came.Load())
	}
}

// A browser whose answers name another address than its own port: the pane does not connect there (so no saved
// login is put back over that connection), nor does closing the browser or watching its windows.
func TestPaneConnectsOnlyToItsPort(t *testing.T) {
	core.DataDir = t.TempDir()
	other, reached := listener(t)
	port, came := devToolsStandIn(t, "ws://127.0.0.1:"+strconv.Itoa(other)+"/devtools/page/P1")
	p := &webPane{Port: port, mode: "window"}
	p.Mu.Lock()
	c, err := p.page()
	p.Mu.Unlock()
	if !errors.Is(err, errDevToolsAddr) || c != nil {
		t.Errorf("the pane's page: %v", err)
	}
	CloseDebugBrowser(port)
	p.watchWindows(port)
	time.Sleep(50 * time.Millisecond)
	if reached.Load() != 0 || came.Load() != 0 {
		t.Errorf("connected to the address named (%d) or to the port (%d)", reached.Load(), came.Load())
	}
}
