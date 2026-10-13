package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
)

// guarded: the server as main() runs it, with the Host check in front of every route.
func guarded(t *testing.T) (*core.Store, *httptest.Server) {
	t.Helper()
	core.DataDir = t.TempDir()
	t.Cleanup(settle)
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = LocalOnly(srv.Listener.Addr(), NewMux(st))
	srv.Start()
	t.Cleanup(srv.Close)
	return st, srv
}

func get(t *testing.T, srv *httptest.Server, path, host string, header ...string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if host != "" {
		req.Host = host
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// A page of another site whose name was re-pointed at 127.0.0.1 sends that site's Host: it gets neither the
// page with the token nor anything else, whatever the route.
func TestForeignHostRefused(t *testing.T) {
	apiToken = "t0ken"
	st, srv := guarded(t)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	for _, host := range []string{"evil.example:" + port, "evil.example", "127.0.0.1", "127.0.0.1:1", "localhost.evil.example:" + port, "[::1]:" + port, "127.0.0.1." + port} {
		for _, path := range []string{"/", "/index.html", "/app.js", "/api/state", "/api/ping", "/api/detect", "/thumb?p=x.png", "/no/such/route"} {
			if code, body := get(t, srv, path, host, "Origin", "http://"+host); code != 403 || strings.Contains(body, apiToken) {
				t.Errorf("Host %s, GET %s: %d", host, path, code)
			}
		}
		req, _ := http.NewRequest("POST", srv.URL+"/api/settings", strings.NewReader(`{"settings":{"roots":["/"],"proxy":"evil.example:8080"},"noRescan":true}`))
		req.Host = host
		req.Header.Set("X-Token", apiToken)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		st.Mu.RLock()
		if resp.StatusCode != 403 || st.Settings.Proxy != "" || len(st.Settings.Roots) != 0 {
			t.Errorf("Host %s, POST /api/settings: %d, settings %+v", host, resp.StatusCode, st.Settings)
		}
		st.Mu.RUnlock()
	}
	// the program's own window and a browser tab: both names of this computer, any letter case
	for _, host := range []string{"", "127.0.0.1:" + port, "localhost:" + port, "LocalHost:" + port} {
		if code, body := get(t, srv, "/", host); code != 200 || !strings.Contains(body, apiToken) {
			t.Errorf("Host %q: %d", host, code)
		}
		if code, _ := get(t, srv, "/api/ping", host); code != 200 {
			t.Errorf("Host %q, ping: %d", host, code)
		}
	}
}

// No answer of the program may be shown inside a frame of another site's page — the page with the token above all
// (a site could lay it under its own and have the player click it) — whatever the route, and also when refused.
func TestNoAnswerInAFrame(t *testing.T) {
	apiToken = "t0ken"
	_, srv := guarded(t)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	ask := func(method, path, host string, header ...string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader("{}"))
		req.Host = host
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if xfo, csp := resp.Header.Get("X-Frame-Options"), resp.Header.Values("Content-Security-Policy"); xfo != "DENY" || !core.ContainsStr(csp, "frame-ancestors 'none'") {
			t.Errorf("%s %s (Host %s): X-Frame-Options %q, Content-Security-Policy %q", method, path, host, xfo, csp)
		}
		return resp.StatusCode, string(b)
	}
	own := "127.0.0.1:" + port
	// as a frame of another site would ask for it
	if code, body := ask("GET", "/", own, "Sec-Fetch-Dest", "iframe", "Sec-Fetch-Site", "cross-site"); code != 200 || !strings.Contains(body, apiToken) {
		t.Errorf("the page: %d", code)
	}
	for _, path := range []string{"/index.html", "/app.js", "/app.css", "/api/state", "/api/ping", "/thumb?p=x.png", "/rthumb?u=x", "/shot?f=x.jpg", "/no/such/route"} {
		ask("GET", path, own)
	}
	ask("POST", "/api/feedback/info", own, "X-Token", apiToken)
	ask("POST", "/api/feedback/info", own) // (refused: no token)
	ask("GET", "/", "evil.example:"+port)  // (refused: another site's name)
}

// /api/state while the overrides and the cards are being changed: the answer is written out while the store
// is held (run with -race; before, the map was read after the lock was let go, which can stop the program).
func TestStateWhileStoreChanges(t *testing.T) {
	st, srv := guarded(t)
	for i := 0; i < 2000; i++ {
		st.Overrides[`c:\x\`+core.Itoa(i)] = "ignore"
	}
	for i := 0; i < 50; i++ {
		st.Assets = append(st.Assets, &core.Asset{Key: "name:outfit" + core.Itoa(i), Name: "Outfit " + core.Itoa(i), RawName: "Outfit " + core.Itoa(i), Category: "衣服",
			Locations: []core.Location{{Path: "/assets/Outfit " + core.Itoa(i), Kind: "dir"}}})
	}
	st.Warnings = []string{"未找到素材文件夹：D:\\gone"}
	stop := time.Now().Add(700 * time.Millisecond)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; time.Now().Before(stop); i++ {
			st.Mu.Lock() // what POST /api/override, a scan and the usage count do
			st.Overrides[`c:\y\`+core.Itoa(i%500)] = "split"
			a := st.Assets[i%len(st.Assets)]
			a.Usage = append(a.Usage, core.Usage{Project: "P" + core.Itoa(i)})
			a.Locations = append(a.Locations[:0:0], core.Location{Path: "/assets/moved " + core.Itoa(i), Kind: "dir"})
			st.Warnings = append(st.Warnings[:0:0], "未找到素材文件夹："+core.Itoa(i))
			st.Mu.Unlock()
		}
	}()
	var state struct {
		Assets    []map[string]any  `json:"assets"`
		Overrides map[string]string `json:"overrides"`
		Warnings  []string          `json:"warnings"`
	}
	go func() {
		defer wg.Done()
		for time.Now().Before(stop) {
			code, body := get(t, srv, "/api/state", "")
			if code != 200 || json.Unmarshal([]byte(body), &state) != nil {
				t.Errorf("state: %d", code)
				return
			}
		}
	}()
	wg.Wait()
	if len(state.Assets) != 50 || len(state.Overrides) < 2000 || len(state.Warnings) != 1 {
		t.Errorf("state: %d cards, %d overrides, warnings %q", len(state.Assets), len(state.Overrides), state.Warnings)
	}
}

// What the window is told is in Chinese, without Go's own wording; and a starting copy can see that this
// one is on its way out.
func TestErrorsAndPing(t *testing.T) {
	apiToken = "t"
	_, srv := guarded(t)
	for path, body := range map[string]any{
		"/api/user":     map[string]any{"key": "name:x", "user": "not an object"},
		"/api/settings": map[string]any{"settings": []int{1}},
		"/api/styles":   map[string]any{"styles": "JK"},
	} {
		r := postJSON(t, srv, path, body)
		if msg, _ := r["err"].(string); r["ok"] != false || !strings.Contains(msg, "数据格式有误") || strings.Contains(msg, "json") || strings.Contains(msg, "unmarshal") {
			t.Errorf("%s: %v", path, r)
		}
	}
	if _, body := get(t, srv, "/api/ping", ""); !strings.Contains(body, `"app":"vrclib"`) || strings.Contains(body, "quitting") {
		t.Errorf("ping: %s", body)
	}
	core.Quitting.Store(true)
	defer core.Quitting.Store(false)
	if _, body := get(t, srv, "/api/ping", ""); !strings.Contains(body, `"quitting":true`) {
		t.Errorf("ping while quitting: %s", body)
	}
}
