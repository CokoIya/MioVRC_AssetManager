package core

import (
	"net/http"
	"testing"
)

// Callers get clients of their own on shared connections, and Windows is not asked for its proxy each time.
func TestHTTPClientReuse(t *testing.T) {
	asked := 0
	old := readSystemProxy
	readSystemProxy = func() string { asked++; return "" }
	defer func() { readSystemProxy = old; ProxyChanged() }()
	ProxyChanged()
	st := &Store{}
	a, b := HTTPClient(st), HTTPClient(st)
	if a == b {
		t.Fatal("one client for two callers: a timeout set by one would change the other's")
	}
	tr, ok := a.Transport.(*http.Transport)
	if !ok || a.Transport != b.Transport {
		t.Fatal("the connections are not shared")
	}
	if asked != 1 {
		t.Errorf("the system proxy was asked %d times for two clients", asked)
	}
	if tr.TLSHandshakeTimeout == 0 || tr.ResponseHeaderTimeout == 0 || tr.IdleConnTimeout == 0 || tr.DialContext == nil {
		t.Errorf("a silent server can hold a caller: %+v", tr)
	}
	// after the settings were saved the system is asked again
	ProxyChanged()
	if HTTPClient(st).Transport != a.Transport || asked != 2 {
		t.Errorf("after a settings change: asked %d times", asked)
	}
	// a proxy typed in the settings: its own connections, the same ones every time
	st.Settings.Proxy = "127.0.0.1:7890"
	p1, p2 := HTTPClient(st), HTTPClient(st)
	if p1.Transport == a.Transport || p1.Transport != p2.Transport || asked != 2 {
		t.Fatalf("a proxy from the settings: shared with none %v, reused %v, asked %d", p1.Transport == a.Transport, p1.Transport == p2.Transport, asked)
	}
	req, _ := http.NewRequest("GET", "https://booth.pm/", nil)
	if u, err := p1.Transport.(*http.Transport).Proxy(req); err != nil || u == nil || u.Host != "127.0.0.1:7890" {
		t.Errorf("proxy %v %v", u, err)
	}
}
