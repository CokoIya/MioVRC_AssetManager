package core

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

var (
	httpMu     sync.Mutex
	transports = map[string]*http.Transport{} // by proxy
	sysProxy   string
	sysProxyAt time.Time
	// asking Windows for its proxy starts reg.exe: not for every request
	readSystemProxy = systemProxy
)

const sysProxyKeep = 30 * time.Second

// ProxyChanged: the settings were saved, the system's proxy is asked again at the next request.
func ProxyChanged() {
	httpMu.Lock()
	sysProxyAt = time.Time{}
	httpMu.Unlock()
}

// transportFor: the connections to use with this proxy ("" = none set). One transport per proxy, kept for
// the whole run, so connections are reused instead of left behind by every request.
func transportFor(px string) *http.Transport {
	httpMu.Lock()
	defer httpMu.Unlock()
	if px == "" {
		if sysProxyAt.IsZero() || time.Since(sysProxyAt) > sysProxyKeep {
			sysProxy, sysProxyAt = readSystemProxy(), time.Now()
		}
		px = sysProxy
	}
	if tr := transports[px]; tr != nil {
		return tr
	}
	for k, old := range transports { // the proxy changed: requests under way finish, idle connections go
		old.CloseIdleConnections()
		delete(transports, k)
	}
	// a server that accepts and then says nothing must not hold a caller forever
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
	}
	transports[px] = tr
	if px != "" && !strings.EqualFold(px, "direct") {
		if !strings.Contains(px, "://") {
			px = "http://" + px
		}
		if u, err := url.Parse(px); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return tr
}

// HTTPClient: a client that goes through the proxy of the settings (or the system's). Every caller gets a
// client of its own — they set its timeout and redirect rule — on connections shared by all.
func HTTPClient(st *Store) *http.Client {
	st.Mu.RLock()
	px := strings.TrimSpace(st.Settings.Proxy)
	st.Mu.RUnlock()
	return &http.Client{Transport: transportFor(px), Timeout: 40 * time.Second}
}

const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"

func FriendlyNetErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		return "连接超时，可能需要代理"
	case strings.Contains(s, "refused") || strings.Contains(s, "no such host") || strings.Contains(s, "reset"):
		return "连接失败，可能需要代理"
	}
	return s
}
