package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/netdisk"
	"vrclib/internal/testkit"
)

func postJSON(t *testing.T, srv *httptest.Server, path string, body any) map[string]any {
	t.Helper()
	j, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(j))
	req.Header.Set("X-Token", apiToken)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// settle waits for the rescan a request started to end, and for what follows a scan: the look into unitypackages
// (it writes pkgcovers.json into the data folder) and the translation of names (it reads the data folder).
func settle() {
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && library.BackgroundBusy(); {
		time.Sleep(5 * time.Millisecond)
	}
}

// scannedLibrary: a library with this folder scanned, and its assets by name.
func scannedLibrary(t *testing.T, root string) (*core.Store, map[string]*core.Asset) {
	t.Helper()
	core.DataDir = t.TempDir()
	t.Cleanup(settle) // before the folder is removed: a rescan started by the test still saves into it
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	st.Settings.Roots = []string{root}
	library.RunFolderScan(st, &core.Task{})
	byName := map[string]*core.Asset{}
	for _, a := range st.Assets {
		byName[a.Name] = a
	}
	return st, byName
}

// Several cards at once: a category, hidden or not, and "do not list".
func TestBulkEdit(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"Alpha Coat", "Beta Hair", "Gamma Prop"} {
		testkit.WriteFile(t, filepath.Join(root, n, n+".unitypackage"), 100)
	}
	st, as := scannedLibrary(t, root)
	st.User[as["Alpha Coat"].Key] = &core.UserData{Notes: "kept", Tags: []string{"winter"}}
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	keys := []string{as["Alpha Coat"].Key, as["Beta Hair"].Key, "name:nosuchthing"}
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": keys, "category": "配饰"}); r["ok"] != true || r["n"] != float64(2) {
		t.Fatalf("category: %v", r)
	}
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": keys, "hidden": true}); r["ok"] != true {
		t.Fatalf("hide: %v", r)
	}
	st.Mu.RLock()
	a, b := st.User[as["Alpha Coat"].Key], st.User[as["Beta Hair"].Key]
	if a.Category != "配饰" || !a.Hidden || a.Notes != "kept" || len(a.Tags) != 1 || b == nil || b.Category != "配饰" || !b.Hidden || st.User["name:nosuchthing"] != nil {
		t.Errorf("after bulk: %+v %+v", a, b)
	}
	if u := st.User[as["Gamma Prop"].Key]; u != nil && (u.Hidden || u.Category != "") {
		t.Errorf("an unpicked card changed: %+v", u)
	}
	st.Mu.RUnlock()
	// back to the automatic category, shown again
	postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": keys[:1], "category": "", "hidden": false})
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": keys, "category": "没有的分类"}); r["ok"] != false {
		t.Errorf("an unknown category was taken: %v", r)
	}
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": []string{}}); r["ok"] != false {
		t.Errorf("nothing picked: %v", r)
	}
	st.Mu.RLock()
	if a := st.User[as["Alpha Coat"].Key]; a.Category != "" || a.Hidden {
		t.Errorf("not reset: %+v", a)
	}
	st.Mu.RUnlock()
	// "do not list": the folder is skipped by the next scan
	if r := postJSON(t, srv, "/api/user/bulk", map[string]any{"keys": []string{as["Gamma Prop"].Key}, "ignore": true}); r["ignored"] != float64(1) {
		t.Fatalf("ignore: %v", r)
	}
	st.Mu.RLock()
	mode := st.Overrides[core.PathKey(filepath.Join(root, "Gamma Prop"))]
	st.Mu.RUnlock()
	if mode != "ignore" {
		t.Errorf("override %q", mode)
	}
}

// 「添加网盘素材」 with a Google Drive / Dropbox link: the first link of the pasted text says what the card is
// made of, and a link pasted again with the key the card's link lacks mends the card.
func TestCloudAdd(t *testing.T) {
	core.DataDir = t.TempDir()
	// nothing answers at the services' addresses: the read that follows an add fails at once
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := dead.URL
	dead.Close()
	for _, k := range []string{"VRCLIB_GDRIVE_BASE", "VRCLIB_GDRIVE_API", "VRCLIB_DROPBOX_BASE"} {
		t.Setenv(k, addr)
	}
	t.Setenv("VRCLIB_GDRIVE_KEY", "")
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	idle := func() {
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
			netdisk.PanMu.Lock()
			busy := netdisk.PanActive || len(netdisk.PanQueue) > 0
			netdisk.PanMu.Unlock()
			if !busy && !library.PipelineBusy() {
				return
			}
		}
	}
	t.Cleanup(idle)
	add := func(text, name string) map[string]any { // one share a round: no pause between two of them
		defer idle()
		return postJSON(t, srv, "/api/cloud/add", map[string]any{"url": text, "name": name})
	}
	link := func(key string) string {
		st.Mu.RLock()
		defer st.Mu.RUnlock()
		if u := st.User[key]; u != nil {
			return u.ShareURL
		}
		return ""
	}
	const gd = "https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz012345"
	// a Baidu link first: not a Drive card
	r := add("链接: https://pan.baidu.com/s/1abcDEF_gh 提取码: ab12  海外: "+gd, "")
	if r["ok"] != false || link("gd:1AbCdEfGhIjKlMnOpQrStUvWxYz012345") != "" {
		t.Fatalf("a Baidu share's text made a Drive card: %v", r)
	}
	r = add("海外: "+gd+"?usp=sharing 国内: https://pan.baidu.com/s/1abcDEF_gh 提取码: ab12", "")
	if r["ok"] != true || r["key"] != "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz012345" || link("gd:1AbCdEfGhIjKlMnOpQrStUvWxYz012345") != gd {
		t.Fatalf("a Drive link first: %v, kept as %q", r, link("gd:1AbCdEfGhIjKlMnOpQrStUvWxYz012345"))
	}
	// the same share again with its resourcekey: the card's link takes it; without one, the link stays
	r = add(gd+"?resourcekey=0-Key_1&usp=sharing", "")
	if r["ok"] != true || link("gd:1AbCdEfGhIjKlMnOpQrStUvWxYz012345") != gd+"?resourcekey=0-Key_1" {
		t.Errorf("the key was not taken: %v, %q", r, link("gd:1AbCdEfGhIjKlMnOpQrStUvWxYz012345"))
	}
	add(gd, "")
	if got := link("gd:1AbCdEfGhIjKlMnOpQrStUvWxYz012345"); got != gd+"?resourcekey=0-Key_1" {
		t.Errorf("a link without the key replaced the one with it: %q", got)
	}
	// Dropbox: first without its rlkey, then with it, then with another
	const db = "https://www.dropbox.com/scl/fi/abc123xyz456789/Dress.zip"
	for _, c := range []struct{ paste, kept string }{
		{db + "?dl=0", db},
		{db + "?rlkey=k1k1k1&st=x&dl=0", db + "?rlkey=k1k1k1"},
		{db + "?dl=0", db + "?rlkey=k1k1k1"},
		{db + "?rlkey=k2k2k2&dl=0", db + "?rlkey=k2k2k2"},
	} {
		r := add(c.paste, "Dress")
		if got := link("db:abc123xyz456789"); r["ok"] != true || got != c.kept {
			t.Errorf("after %s: %v, kept as %q, want %q", c.paste, r, got, c.kept)
		}
	}
	st.Mu.RLock()
	name := st.User["db:abc123xyz456789"].Name
	st.Mu.RUnlock()
	if name != "Dress" {
		t.Errorf("the card's name: %q", name)
	}
}

// The page's language goes into a script of the page: only the values the page knows are put there, whatever a
// library.json (an imported one, a hand-edited one) says.
func TestPageLangIsOneOfOurs(t *testing.T) {
	core.DataDir = t.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	page := func() string {
		resp, err := srv.Client().Get(srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	for lang, want := range map[string]string{"en": `window.LANG = "en"`, "ja": `window.LANG = "ja"`, "": `window.LANG = ""`,
		`en";window.__x=1;//`: `window.LANG = ""`, `zz"></script><b id=M>x</b><script>`: `window.LANG = ""`} {
		st.Mu.Lock()
		st.Settings.Lang, st.Settings.SetupDone = lang, true
		st.Mu.Unlock()
		if p := page(); !strings.Contains(p, want) || strings.Contains(p, "__x") || strings.Contains(p, "<b id=M>") {
			t.Errorf("lang %q: the page does not start with %s", lang, want)
		}
	}
	st.Mu.Lock()
	st.Settings.Lang, st.Settings.SetupDone = "", false
	st.Mu.Unlock()
	if p := page(); !strings.Contains(p, `window.LANG = "?"`) {
		t.Error("a fresh install does not ask the page to go by the system's language")
	}
}
