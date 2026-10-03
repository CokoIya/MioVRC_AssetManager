package core

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func HTTPClient(st *Store) *http.Client {
	st.Mu.RLock()
	px := strings.TrimSpace(st.Settings.Proxy)
	st.Mu.RUnlock()
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 20 * time.Second}
	if px == "" {
		px = systemProxy()
	}
	if px != "" && !strings.EqualFold(px, "direct") {
		if !strings.Contains(px, "://") {
			px = "http://" + px
		}
		if u, err := url.Parse(px); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: tr, Timeout: 40 * time.Second}
}

const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"

func FriendlyNetErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		return "连接 Booth 超时（可能需要代理）"
	case strings.Contains(s, "refused") || strings.Contains(s, "no such host") || strings.Contains(s, "reset"):
		return "连不上 Booth（可能需要代理）"
	}
	return s
}
