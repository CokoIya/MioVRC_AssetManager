package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// The wish list over the API: an item by its address, the list as the page gets it, a note, the daily look
// turned off, removal. Booth is asked once (for the item added), with nothing but the item's JSON.
func TestWishAPI(t *testing.T) {
	var asked atomic.Int32
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/ja/items/4242.json") {
			t.Errorf("asked for %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"name":"<b>Dress</b>","price":"¥ 1,500~","shop":{"name":"Luna","subdomain":"luna"},"variations":[{"id":1,"name":"Plum","price":1500,"is_empty_stock":false},{"id":2,"name":"Full","price":4000,"is_empty_stock":false}]}`)
	}))
	defer shop.Close()
	t.Setenv("VRCLIB_BOOTH_WEB", shop.URL)
	st := testkit.NewStore(t)
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()

	// without the page's token nothing is given or taken
	if resp, err := http.Post(srv.URL+"/api/stores/state", "application/json", strings.NewReader("{}")); err != nil || resp.StatusCode != 403 {
		t.Fatalf("no token: %v %v", err, resp)
	}
	if r := postJSON(t, srv, "/api/wish/add", map[string]any{"text": "https://example.com/items/1"}); r["ok"] != false || r["err"] == "" {
		t.Fatalf("another site's address: %v", r)
	}
	r := postJSON(t, srv, "/api/wish/add", map[string]any{"text": shop.URL + "/ja/items/4242"})
	if r["ok"] != true || r["key"] != "booth:4242" || asked.Load() != 1 {
		t.Fatalf("add: %v, asked %d", r, asked.Load())
	}
	state := postJSON(t, srv, "/api/stores/state", map[string]any{})
	wish := state["wish"].(map[string]any)
	items := wish["items"].([]any)
	it := items[0].(map[string]any)
	if len(items) != 1 || it["title"] != "<b>Dress</b>" || it["price"] != "¥ 1,500" || it["from"] != true || it["currency"] != "JPY" || it["shop"] != "Luna" ||
		len(it["vars"].([]any)) != 2 || wish["watch"] != true || wish["unseen"] != float64(0) {
		t.Fatalf("state: %v", state)
	}
	if jx := state["jinxxy"].(map[string]any); jx["base"] != "https://jinxxy.com" || !strings.HasPrefix(jx["search"].(string), "https://cn.bing.com/search?q=site%3Ajinxxy.com+") {
		t.Fatalf("jinxxy: %v", jx)
	}
	if r := postJSON(t, srv, "/api/wish/note", map[string]any{"key": "booth:4242", "note": "for Plum"}); r["ok"] != true {
		t.Fatalf("note: %v", r)
	}
	if r := postJSON(t, srv, "/api/wish/note", map[string]any{"key": "booth:1", "note": "x"}); r["ok"] != false {
		t.Fatalf("note on nothing: %v", r)
	}
	postJSON(t, srv, "/api/wish/watch", map[string]any{"on": false})
	state = postJSON(t, srv, "/api/stores/state", map[string]any{})
	wish = state["wish"].(map[string]any)
	if wish["watch"] != false || wish["items"].([]any)[0].(map[string]any)["note"] != "for Plum" {
		t.Fatalf("after the changes: %v", wish)
	}
	b, err := os.ReadFile(filepath.Join(core.DataDir, "wishlist.json"))
	if err != nil || !strings.Contains(string(b), `"noWatch": true`) || !strings.Contains(string(b), "for Plum") {
		t.Fatalf("wishlist.json: %v %s", err, b)
	}
	if lib, _ := os.ReadFile(filepath.Join(core.DataDir, "library.json")); strings.Contains(string(lib), "4242") {
		t.Error("the wish list went into library.json")
	}
	if r := postJSON(t, srv, "/api/wish/remove", map[string]any{"key": "booth:4242"}); r["ok"] != true {
		t.Fatalf("remove: %v", r)
	}
	if r := postJSON(t, srv, "/api/wish/check", map[string]any{}); r["ok"] != true || r["n"] != float64(0) || asked.Load() != 1 {
		t.Fatalf("nothing to look at: %v, asked %d", r, asked.Load())
	}
}

// Followed shops over the API: a shop by its address and by an item's, the list as the page gets it, a note,
// the daily look turned off for one shop and for all, 「全部标为已读」, unfollowing. The shop's list is read
// once at the follow, and nothing but that list and the pasted item's JSON is asked for.
func TestFollowAPI(t *testing.T) {
	var asked atomic.Int32
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		switch r.URL.Path {
		case "/shop/luna/items":
			fmt.Fprint(w, `<span class="shop-name-label display_title">Luna &lt;Works&gt;</span><div class="avatar-image" style="background-image: url(https://booth.pximg.net/c/48x48/users/1/icon_image/a.jpg)"></div>
			<li data-item="{&quot;id&quot;:&quot;4242&quot;,&quot;name&quot;:&quot;Dress&quot;,&quot;price&quot;:&quot;¥ 1,500&quot;,&quot;thumbnail_image_urls&quot;:[&quot;https://booth.pximg.net/c/72x72_a2_g5/u/i/4242/t.jpg&quot;]}"></li>`)
		case "/ja/items/4242.json":
			fmt.Fprint(w, `{"name":"Dress","price":"¥ 1,500","shop":{"name":"Luna","subdomain":"luna","url":"https://luna.booth.pm/"},"variations":[{"id":1,"name":null,"price":1500}]}`)
		default:
			t.Errorf("asked for %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer shop.Close()
	t.Setenv("VRCLIB_BOOTH_WEB", shop.URL)
	st := testkit.NewStore(t)
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()

	if r := postJSON(t, srv, "/api/follow/add", map[string]any{"text": "https://example.com/"}); r["ok"] != false || r["err"] == "" {
		t.Fatalf("another site's address: %v", r)
	}
	r := postJSON(t, srv, "/api/follow/add", map[string]any{"text": shop.URL + "/ja/items/4242"})
	if r["ok"] != true || r["sub"] != "luna" || r["name"] != "Luna" {
		t.Fatalf("add by an item: %v", r)
	}
	var state, follow map[string]any
	for i := 0; i < 200; i++ { // the first look goes on in the background
		state = postJSON(t, srv, "/api/stores/state", map[string]any{})
		follow = state["follow"].(map[string]any)
		if s := follow["shops"].([]any)[0].(map[string]any); s["checked"].(float64) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	shops := follow["shops"].([]any)
	s := shops[0].(map[string]any)
	if len(shops) != 1 || s["sub"] != "luna" || s["name"] != "Luna <Works>" || s["url"] != shop.URL+"/shop/luna/" || s["items"] != float64(1) || s["unread"] != float64(0) ||
		len(s["news"].([]any)) != 0 || s["watch"] != true || follow["watch"] != true || follow["unseen"] != float64(0) || asked.Load() != 2 {
		t.Fatalf("state: %v, asked %d", follow, asked.Load())
	}
	if r := postJSON(t, srv, "/api/follow/add", map[string]any{"sub": "https://luna.booth.pm/", "name": "Luna"}); r["ok"] != true || r["sub"] != "luna" || asked.Load() != 2 {
		t.Fatalf("followed twice: %v", r)
	}
	if r := postJSON(t, srv, "/api/follow/note", map[string]any{"sub": "luna", "note": "for Plum"}); r["ok"] != true {
		t.Fatalf("note: %v", r)
	}
	if r := postJSON(t, srv, "/api/follow/note", map[string]any{"sub": "nobody", "note": "x"}); r["ok"] != false {
		t.Fatalf("note on nothing: %v", r)
	}
	postJSON(t, srv, "/api/follow/watch", map[string]any{"sub": "luna", "on": false})
	postJSON(t, srv, "/api/follow/watch", map[string]any{"on": false})
	if r := postJSON(t, srv, "/api/follow/read", map[string]any{"sub": "luna"}); r["ok"] != true {
		t.Fatalf("read: %v", r)
	}
	postJSON(t, srv, "/api/follow/seen", map[string]any{})
	postJSON(t, srv, "/api/follow/told", map[string]any{"keys": []string{"luna:1"}})
	follow = postJSON(t, srv, "/api/stores/state", map[string]any{})["follow"].(map[string]any)
	s = follow["shops"].([]any)[0].(map[string]any)
	if follow["watch"] != false || s["watch"] != false || s["note"] != "for Plum" {
		t.Fatalf("after the changes: %v", follow)
	}
	b, err := os.ReadFile(filepath.Join(core.DataDir, "follows.json"))
	if err != nil || !strings.Contains(string(b), `"noWatch": true`) || !strings.Contains(string(b), "for Plum") || !strings.Contains(string(b), `"4242"`) {
		t.Fatalf("follows.json: %v %s", err, b)
	}
	if lib, _ := os.ReadFile(filepath.Join(core.DataDir, "library.json")); strings.Contains(string(lib), "luna") {
		t.Error("the followed shops went into library.json")
	}
	if r := postJSON(t, srv, "/api/follow/check", map[string]any{}); r["ok"] != true || r["n"] != float64(0) {
		t.Fatalf("nothing to look at: %v", r)
	}
	if r := postJSON(t, srv, "/api/follow/remove", map[string]any{"sub": "luna"}); r["ok"] != true || asked.Load() != 2 {
		t.Fatalf("remove: %v", r)
	}
}
