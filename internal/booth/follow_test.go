package booth

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

func TestFollowSub(t *testing.T) {
	t.Setenv("VRCLIB_BOOTH_WEB", "http://127.0.0.1:8123")
	for text, want := range map[string]string{
		"komado": "komado", " Komado ": "komado", "https://komado.booth.pm/": "komado", "https://komado.booth.pm/items/7770415": "komado",
		"komado.booth.pm": "komado", "http://paryi.booth.pm/items?page=2": "paryi", "https://a-b1.booth.pm/item_lists/x": "a-b1",
		"http://127.0.0.1:8123/shop/komado/items": "komado", "http://127.0.0.1:8123/shop/komado/": "komado",
		"https://booth.pm/ja/items/7770415": "", "booth.pm/ja/items/123": "", "https://www.booth.pm/": "", "https://accounts.booth.pm/library": "",
		"https://komado.booth.pm.evil.example/": "", "https://example.com/shop/x/": "", "http://127.0.0.1:9999/shop/komado/": "",
		"ko mado": "", "-komado": "", "": "", "7770415": "",
	} {
		if got := FollowSub(text); got != want {
			t.Errorf("FollowSub(%q) = %q, want %q", text, got, want)
		}
	}
	if ShopListURL("komado") != "http://127.0.0.1:8123/shop/komado/items" {
		t.Error(ShopListURL("komado"))
	}
	t.Setenv("VRCLIB_BOOTH_WEB", "")
	if ShopURL("komado") != "https://komado.booth.pm/" || ShopListURL("komado") != "https://komado.booth.pm/items" {
		t.Error(ShopURL("komado"))
	}
}

// shopPage: a shop's list page in the markup the live pages have (the cards carry their item as JSON in
// data-item, the header the name and the icon).
func shopPage(name, icon string, items [][3]string) string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><head><title>` + html.EscapeString(name) + ` - BOOTH</title></head><body><header><a class="nav" title="Home" href="/">` + html.EscapeString(name) + `</a>
<div class="avatar-image" style="background-image: url(` + icon + `)"></div><span class="shop-name-label display_title">` + html.EscapeString(name) + `</span></header><ul>`)
	for _, it := range items {
		j, _ := json.Marshal(map[string]any{"id": it[0], "name": it[1], "price": it[2], "url": "https://shop.booth.pm/items/" + it[0], "is_adult": it[0] == "9",
			"is_sold_out": false, "is_end_of_sale": false, "is_vrchat": true, "category": map[string]any{"name": map[string]string{"ja": "3D衣装", "en": "3D Clothing"}, "url": "x"},
			"thumbnail_image_urls": []string{"https://booth.pximg.net/c/72x72_a2_g5/u/i/" + it[0] + "/t.jpg"}})
		sb.WriteString(`<li class="item-card l-card js-mount-point-shop-item-card" data-item="` + html.EscapeString(string(j)) + `"></li>`)
	}
	sb.WriteString(`</ul><nav><a class="nav-item" href="/items?page=2">2</a><a class="nav-item last-page" href="/items?page=3"></a></nav></body></html>`)
	return sb.String()
}

func TestParseShopPage(t *testing.T) {
	page := shopPage("Luna <Works>", "https://booth.pximg.net/c/48x48/users/1/icon_image/a_base_resized.jpg",
		[][3]string{{"101", "Moon \"Dress\" <b>", "¥ 1,500"}, {"102", "Ribbon", "¥ 300~"}, {"102", "twice", "¥ 1"}, {"abc", "no id", "¥ 1"}, {"9", "adult", "¥ 2,000"}})
	fp, err := parseShopPage([]byte(page))
	if err != nil || fp.Name != "Luna <Works>" || fp.Icon != "https://booth.pximg.net/c/48x48/users/1/icon_image/a_base_resized.jpg" || len(fp.Items) != 3 {
		t.Fatalf("%v %+v", err, fp)
	}
	if a := fp.Items[0]; a.ID != "101" || a.Title != `Moon "Dress" <b>` || a.Price != "¥ 1,500" || a.URL != "https://shop.booth.pm/items/101" || a.Thumb != "https://booth.pximg.net/c/300x300_a2_g5/u/i/101/t.jpg" || a.Adult {
		t.Fatalf("%+v", a)
	}
	if fp.Items[1].Price != "¥ 300~" || !fp.Items[2].Adult {
		t.Fatalf("%+v", fp.Items)
	}
	// a single-quoted attribute and a number for an id; the name from the title when the header has none
	fp, err = parseShopPage([]byte(`<title>Shop X - BOOTH</title><div data-item='{"id":55,"name":"A","price":"¥ 100"}'></div>`))
	if err != nil || fp.Name != "Shop X" || len(fp.Items) != 1 || fp.Items[0].ID != "55" {
		t.Fatalf("%v %+v", err, fp)
	}
	if _, err := parseShopPage([]byte(`<html><body>nothing here</body></html>`)); err == nil {
		t.Fatal("a page without cards is read as a shop")
	}
	if fp, err := parseShopPage([]byte(shopPage("Empty", "", nil))); err != nil || fp.Name != "Empty" || len(fp.Items) != 0 {
		t.Fatalf("a shop with nothing for sale: %v %+v", err, fp)
	}
}

// followShop stands in for Booth: shops' list pages and items' JSON.
type followShop struct {
	mu    sync.Mutex
	asked []string
	items map[string][][3]string // sub → items, the newest first
	code  map[string]int
	hook  func(sub string) // called while a shop's list is being asked for
}

func (s *followShop) serve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := r.Header.Get("Cookie"); c != "adult=t" {
			t.Errorf("cookie sent to the shop: %q", c)
		}
		if strings.HasPrefix(r.URL.Path, "/ja/items/") { // an item's JSON: whose it is
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/ja/items/"), ".json")
			s.mu.Lock()
			s.asked = append(s.asked, "item:"+id)
			s.mu.Unlock()
			fmt.Fprintf(w, `{"name":"Item %s","price":"¥ 100","shop":{"name":"Luna Works","subdomain":"luna","url":"https://luna.booth.pm/"},"variations":[{"id":1,"name":null,"price":100}]}`, id)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "shop" || parts[2] != "items" {
			t.Errorf("a shop look asked for more than the list: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		sub := parts[1]
		s.mu.Lock()
		s.asked = append(s.asked, sub)
		code, items, hook := s.code[sub], s.items[sub], s.hook
		s.mu.Unlock()
		if hook != nil {
			hook(sub)
		}
		if code != 0 {
			w.WriteHeader(code)
			return
		}
		fmt.Fprint(w, shopPage("Shop "+sub, "https://booth.pximg.net/c/48x48/users/1/icon_image/"+sub+".jpg", items))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_BOOTH_WEB", srv.URL)
}

func (s *followShop) took() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	got := strings.Join(s.asked, " ")
	s.asked = nil
	return got
}

// sorted: what was asked for, in alphabetical order (first looks and a pasted item's request race)
func (s *followShop) sorted() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	got := append([]string{}, s.asked...)
	sort.Strings(got)
	s.asked = nil
	return strings.Join(got, " ")
}

func (s *followShop) set(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
}

// followTimes makes a day short for one test and puts everything back after it.
func followTimes(t *testing.T, every, retry time.Duration) {
	f, e, r, g, h, pf := followFirst, followEvery, followRetry, followGap, followByHand, boothPauseFirst
	followEvery, followRetry, followGap, followByHand, boothPauseFirst = every, retry, time.Millisecond, time.Hour, 150*time.Millisecond
	t.Cleanup(func() {
		followFirst, followEvery, followRetry, followGap, followByHand = f, e, r, g, h
		boothMu.Lock()
		boothPauseFirst, boothPauseStep, boothPausedTill, boothBrokenOff, boothBrokeAt = pf, 0, time.Time{}, false, ""
		boothMu.Unlock()
		followBrokeAt = ""
		followMu.Lock()
		followFrom = ""
		followMu.Unlock()
	})
}

func waitSeeded(t *testing.T, sub string) FollowShopView {
	t.Helper()
	for i := 0; i < 300; i++ {
		for _, v := range FollowSnapshot().Shops {
			if v.Sub == sub && (v.Checked > 0 || v.Err != "") {
				return v
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was not looked at", sub)
	return FollowShopView{}
}

// A shop is followed from the panel, by its address, and by an item's address; its first read only writes
// down what it has; a day later a new item is told once; the record is in follows.json; the daily look can be
// turned off for one shop and for all.
func TestFollowRoundOnceADay(t *testing.T) {
	followTimes(t, time.Hour, time.Hour)
	shop := &followShop{items: map[string][][3]string{
		"luna":   {{"103", "Moon Dress", "¥ 1,500"}, {"102", "Ribbon", "¥ 300"}, {"101", "Old hat", "¥ 100"}},
		"komado": {{"7770415", "Plum", "¥ 5,500"}},
		"tail":   {{"201", "Tail", "¥ 0"}},
	}, code: map[string]int{}}
	shop.serve(t)
	st := testkit.NewStore(t)
	if s, err := FollowAdd(st, FollowSeed{Sub: "https://luna.booth.pm/", Name: "Luna Works"}); err != nil || s.Sub != "luna" || s.Name != "Luna Works" {
		t.Fatalf("from the panel: %v %+v", err, s)
	}
	if s, err := FollowAdd(st, FollowSeed{Text: "komado.booth.pm"}); err != nil || s.Sub != "komado" || s.Name != "komado" {
		t.Fatalf("by address: %v %+v", err, s)
	}
	if s, err := FollowAdd(st, FollowSeed{Text: "https://booth.pm/ja/items/201"}); err != nil || s.Sub != "luna" {
		t.Fatalf("by an item's address: %v %+v", err, s) // (the stand-in says every item is Luna's)
	}
	for _, text := range []string{"not a shop", "luna", "https://example.com/shop/x/", "https://booth.pm/ja/items/"} {
		if _, err := FollowAdd(st, FollowSeed{Text: text}); err == nil {
			t.Fatalf("%q is taken for a shop", text)
		}
	}
	luna, komado := waitSeeded(t, "luna"), waitSeeded(t, "komado")
	if luna.Items != 3 || len(luna.News) != 0 || luna.Unread != 0 || luna.Name != "Shop luna" || !strings.HasSuffix(luna.Icon, "/luna.jpg") || luna.Err != "" || !luna.Watch {
		t.Fatalf("after the first look: %+v", luna)
	}
	if komado.Items != 1 || komado.Name != "Shop komado" {
		t.Fatalf("after the first look: %+v", komado)
	}
	if got := shop.sorted(); got != "item:201 komado luna" {
		t.Fatalf("following asked for %q", got)
	}
	s := FollowSnapshot()
	if len(s.Shops) != 2 || s.Unseen != 0 || !s.Watch || s.Paused || s.Busy {
		t.Fatalf("%+v", s)
	}
	// just followed: nothing is due
	if n, err := FollowRound(st, false); n != 0 || err != nil || shop.took() != "" {
		t.Fatalf("looked again at once: %d %v", n, err)
	}
	// a day later: a new item at Luna's, one gone at Komado's (nothing to tell)
	back := func(d time.Duration) {
		followMu.Lock()
		for _, s := range followData.Shops {
			s.Checked -= int64(d / time.Second)
			s.Tried -= int64(d / time.Second)
		}
		followMu.Unlock()
	}
	back(2 * time.Hour)
	shop.set(func() {
		shop.items["luna"] = append([][3]string{{"104", "New <Cape>", "¥ 2,000"}}, shop.items["luna"]...)
		shop.items["komado"] = nil
	})
	if n, err := FollowRound(st, false); n != 2 || err != nil {
		t.Fatalf("round: %d %v", n, err)
	}
	if got := shop.took(); got != "luna komado" {
		t.Fatalf("the round asked for %q", got)
	}
	s = FollowSnapshot()
	luna = s.Shops[0] // the one with news first
	if s.Unseen != 1 || luna.Sub != "luna" || luna.Unread != 1 || luna.Items != 4 || len(luna.News) != 1 {
		t.Fatalf("the new item: %+v", s)
	}
	if x := luna.News[0]; x.Key != "luna:104" || x.Title != "New <Cape>" || x.Price != "¥ 2,000" || x.Thumb != "https://booth.pximg.net/c/300x300_a2_g5/u/i/104/t.jpg" || x.URL != "https://shop.booth.pm/items/104" || x.At == 0 || x.Told || x.Read || !x.Unseen {
		t.Fatalf("the new item: %+v", x)
	}
	if k := s.Shops[1]; k.Sub != "komado" || k.Items != 1 || len(k.News) != 0 || k.Err != "" {
		t.Fatalf("an item that went: %+v", k)
	}
	// a second round the same day asks for nothing; told once, seen, read
	if n, _ := FollowRound(st, false); n != 0 || shop.took() != "" {
		t.Fatal("a second look the same day")
	}
	FollowTold([]string{"luna:999"})
	if FollowSnapshot().Shops[0].News[0].Told {
		t.Fatal("told for another item")
	}
	FollowTold([]string{"luna:104"})
	FollowSeen()
	s = FollowSnapshot()
	if x := s.Shops[0].News[0]; !x.Told || x.Unseen || x.Read || s.Unseen != 0 || s.Shops[0].Unread != 1 {
		t.Fatalf("told and seen: %+v", x)
	}
	if !FollowRead("luna") || FollowRead("nobody") || FollowSnapshot().Shops[0].Unread != 0 {
		t.Fatal("read")
	}
	// the file: whole, nothing left over, and read again as it was written
	b, err := os.ReadFile(filepath.Join(core.DataDir, "follows.json"))
	var f followFile
	if err != nil || json.Unmarshal(b, &f) != nil || len(f.Shops) != 2 || len(f.Shops[0].News) != 1 || !f.Shops[0].News[0].Read || len(f.Shops[0].Seen) != 4 {
		t.Fatalf("follows.json: %v %s", err, b)
	}
	if _, err := os.Stat(filepath.Join(core.DataDir, "follows.json.tmp")); err == nil {
		t.Error("the temporary file is left behind")
	}
	followMu.Lock()
	followFrom = "" // as after a restart
	followMu.Unlock()
	if s := FollowSnapshot(); len(s.Shops) != 2 || s.Shops[0].News[0].Title != "New <Cape>" {
		t.Fatalf("read again: %+v", s.Shops)
	}
	// an item that was seen before is not told again when it comes back
	back(2 * time.Hour)
	shop.set(func() { shop.items["komado"] = [][3]string{{"7770415", "Plum", "¥ 5,500"}} })
	if n, _ := FollowRound(st, false); n != 2 || len(FollowSnapshot().Shops[1].News) != 0 {
		t.Fatalf("an item that came back is told as new: %d", n)
	}
	shop.took()
	// one shop turned off: left out; by hand it is still looked at
	if !FollowSetWatch("komado", false) || FollowSetWatch("nobody", false) {
		t.Fatal("watch")
	}
	back(2 * time.Hour)
	if n, _ := FollowRound(st, false); n != 1 || shop.took() != "luna" {
		t.Fatal("a shop turned off was looked at")
	}
	// all turned off: no looks, however long it has been; by hand still
	FollowSetWatch("", false)
	back(48 * time.Hour)
	if n, _ := FollowRound(st, false); n != 0 || shop.took() != "" || FollowSnapshot().Watch {
		t.Fatal("looked while turned off")
	}
	if n, err := FollowRound(st, true); n != 2 || err != nil || shop.sorted() != "komado luna" {
		t.Fatalf("by hand: %d %v", n, err)
	}
	if n, _ := FollowRound(st, true); n != 0 || shop.took() != "" {
		t.Fatal("by hand again within the hour")
	}
	// note and unfollowing
	if !FollowNote("luna", "  for Plum  ") || FollowNote("nobody", "x") || FollowSnapshot().Shops[0].Note != "for Plum" {
		t.Fatal("note")
	}
	if !FollowRemove("komado") || FollowRemove("komado") || len(FollowSnapshot().Shops) != 1 {
		t.Fatal("remove")
	}
	if s, err := FollowAdd(st, FollowSeed{Sub: "luna"}); err != nil || s.Note != "for Plum" {
		t.Fatalf("followed twice: %v", err)
	}
}

// Booth answering 429 ends the round there and nothing is asked until the pause is over; a shop that is gone
// gets the error and is not asked again the same day; a look that fails for the network is tried again sooner.
func TestFollowRoundBacksOff(t *testing.T) {
	followTimes(t, 0, 0)
	shop := &followShop{items: map[string][][3]string{"a": {{"1", "A", "¥ 1"}}, "b": {{"2", "B", "¥ 2"}}, "c": {{"3", "C", "¥ 3"}}, "d": {{"4", "D", "¥ 4"}}}, code: map[string]int{}}
	shop.serve(t)
	st := testkit.NewStore(t)
	for _, sub := range []string{"a", "b", "c", "d"} {
		if _, err := FollowAdd(st, FollowSeed{Sub: sub}); err != nil {
			t.Fatal(err)
		}
		waitSeeded(t, sub)
	}
	followMu.Lock()
	for i, s := range followData.Shops {
		s.Checked, s.Tried = int64(1000+i), int64(1000+i)
	}
	followMu.Unlock()
	shop.took()
	shop.set(func() { shop.code["b"] = http.StatusTooManyRequests; shop.code["d"] = http.StatusNotFound })
	if n, _ := FollowRound(st, false); n != 1 {
		t.Fatalf("round: %d", n)
	}
	if got := shop.took(); got != "a b" {
		t.Fatalf("asked for %q after being turned away", got)
	}
	s := FollowSnapshot()
	var bv FollowShopView
	for _, v := range s.Shops {
		if v.Sub == "b" {
			bv = v
		}
	}
	if !s.Paused || bv.Err != "" || bv.Checked != 1001 {
		t.Fatalf("the refused one is written down as looked at: %+v", bv)
	}
	// during the pause: no round, and a new shop is listed without asking
	if n, err := FollowRound(st, false); n != 0 || err == nil {
		t.Fatal("a round during the pause")
	}
	if _, err := FollowStartRound(st); err == nil {
		t.Fatal("by hand during the pause")
	}
	if v, err := FollowAdd(st, FollowSeed{Sub: "e", Name: "E"}); err != nil || v.Name != "E" {
		t.Fatalf("during the pause: %v", err)
	}
	if _, err := FollowAdd(st, FollowSeed{Text: "https://booth.pm/ja/items/5"}); err == nil {
		t.Fatal("asked for an item during the pause")
	}
	time.Sleep(50 * time.Millisecond)
	if got := shop.took(); got != "" {
		t.Fatalf("asked during the pause: %q", got)
	}
	time.Sleep(200 * time.Millisecond)
	shop.set(func() { shop.code["b"] = 0; shop.items["e"] = [][3]string{{"5", "E", "¥ 5"}} })
	followMu.Lock()
	followFind("a").Checked, followFind("a").Tried = time.Now().Unix()+3600, time.Now().Unix()+3600 // looked at: not due
	followMu.Unlock()
	followEvery = time.Hour
	if n, _ := FollowRound(st, false); n != 4 {
		t.Fatalf("after the pause: %d", n)
	}
	if got := shop.took(); got != "e b c d" {
		t.Fatalf("after the pause asked for %q", got)
	}
	bys := map[string]FollowShopView{}
	for _, v := range FollowSnapshot().Shops {
		bys[v.Sub] = v
	}
	if FollowSnapshot().Paused || bys["d"].Err == "" || bys["d"].Checked == 0 || bys["e"].Items != 1 || len(bys["e"].News) != 0 || bys["b"].Err != "" {
		t.Fatalf("after the pause: %+v %+v", bys["d"], bys["e"])
	}
	boothMu.Lock()
	step := boothPauseStep
	boothMu.Unlock()
	if step != 0 {
		t.Errorf("the next pause does not start short again: %v", step)
	}
	// the shop does not answer at all: the round gives up after two, and those are due again after followRetry
	followMu.Lock()
	for _, s := range followData.Shops {
		s.Checked, s.Tried, s.Err = 1000, 1000, ""
	}
	followMu.Unlock()
	t.Setenv("VRCLIB_BOOTH_WEB", "http://127.0.0.1:1")
	if n, _ := FollowRound(st, false); n != 0 {
		t.Fatalf("looked without a shop: %d", n)
	}
	failed := 0
	for _, v := range FollowSnapshot().Shops {
		if v.Err != "" {
			failed++
		}
	}
	if failed != 2 {
		t.Fatalf("%d looks failed before the round gave up", failed)
	}
}

// A shop Booth refuses by itself (403 again, first thing after the pause) gets the error and does not hold
// up the others for ever.
func TestFollowRoundShopRefused(t *testing.T) {
	followTimes(t, 0, 0)
	shop := &followShop{items: map[string][][3]string{"a": {{"1", "A", "¥ 1"}}, "b": {{"2", "B", "¥ 2"}}}, code: map[string]int{}}
	shop.serve(t)
	st := testkit.NewStore(t)
	for _, sub := range []string{"a", "b"} {
		if _, err := FollowAdd(st, FollowSeed{Sub: sub}); err != nil {
			t.Fatal(err)
		}
		waitSeeded(t, sub)
	}
	followMu.Lock()
	followData.Shops[0].Checked, followData.Shops[0].Tried = 1000, 1000
	followData.Shops[1].Checked, followData.Shops[1].Tried = 1001, 1001
	followMu.Unlock()
	shop.took()
	shop.set(func() { shop.code["a"] = http.StatusForbidden })
	if n, _ := FollowRound(st, false); n != 0 || shop.took() != "a" || !FollowSnapshot().Paused {
		t.Fatal("not stopped by the refusal")
	}
	time.Sleep(200 * time.Millisecond)
	if n, _ := FollowRound(st, false); n != 1 {
		t.Fatalf("after the pause: %d", n)
	}
	if got := shop.took(); got != "a b" {
		t.Fatalf("after the pause asked for %q", got)
	}
	bys := map[string]FollowShopView{}
	for _, v := range FollowSnapshot().Shops {
		bys[v.Sub] = v
	}
	if FollowSnapshot().Paused || bys["a"].Err == "" || bys["b"].Err != "" {
		t.Fatalf("%+v", bys)
	}
}

// daysPass moves every shop's last look back, as if that long had passed.
func daysPass(d time.Duration) {
	followMu.Lock()
	for _, s := range followData.Shops {
		if s.Checked > 0 {
			s.Checked -= int64(d / time.Second)
		}
		if s.Tried > 0 {
			s.Tried -= int64(d / time.Second)
		}
	}
	followMu.Unlock()
}

// Only the first page of a shop's list is read. When an item of it is taken off sale (or the owner reorders),
// an old item moves up from the second page: it is written down, not announced. An id above every id seen so
// far still is.
func TestFollowOldItemMovesOntoFirstPage(t *testing.T) {
	followTimes(t, time.Hour, time.Hour)
	// 30 items; a list page shows 24 (ids 130…107 on the first page, 106…101 on the second)
	var all [][3]string
	for id := 130; id > 100; id-- {
		all = append(all, [3]string{fmt.Sprint(id), fmt.Sprintf("Item %d", id), "¥ 100"})
	}
	shop := &followShop{items: map[string][][3]string{"luna": all[:24]}, code: map[string]int{}}
	shop.serve(t)
	st := testkit.NewStore(t)
	if _, err := FollowAdd(st, FollowSeed{Sub: "luna"}); err != nil {
		t.Fatal(err)
	}
	waitSeeded(t, "luna")
	daysPass(2 * time.Hour)
	// item 120 is taken off sale: the first page is now 130…121, 119…106
	page := func(first ...[3]string) [][3]string {
		p := first
		for _, it := range all {
			if it[0] != "120" && len(p) < 24 {
				p = append(p, it)
			}
		}
		return p
	}
	shop.set(func() { shop.items["luna"] = page() })
	if n, err := FollowRound(st, false); n != 1 || err != nil {
		t.Fatalf("round: %d %v", n, err)
	}
	v := FollowSnapshot().Shops[0]
	if len(v.News) != 0 || v.Unread != 0 || v.Items != 25 {
		t.Fatalf("an old item that moved up is announced: %+v", v.News)
	}
	// a day later the shop puts a new item on sale: that one is told, and nothing else
	daysPass(2 * time.Hour)
	shop.set(func() { shop.items["luna"] = page([3]string{"131", "New", "¥ 500"}) })
	if n, _ := FollowRound(st, false); n != 1 {
		t.Fatalf("round: %d", n)
	}
	if v = FollowSnapshot().Shops[0]; len(v.News) != 1 || v.News[0].ID != "131" {
		t.Fatalf("the new item: %+v", v.News)
	}
	followMu.Lock()
	top, listed := followData.Shops[0].Top, followData.Shops[0].Listed
	followFrom = "" // as after a restart: the highest id is in the file
	followMu.Unlock()
	if top != 131 || !listed || len(FollowSnapshot().Shops) != 1 {
		t.Fatalf("top %d, listed %v", top, listed)
	}
	daysPass(2 * time.Hour)
	shop.set(func() { shop.items["luna"] = all[6:] }) // the six newest are gone: 106…101 are all on the page now
	if n, _ := FollowRound(st, false); n != 1 || len(FollowSnapshot().Shops[0].News) != 1 {
		t.Fatalf("after a restart old items are announced: %+v", FollowSnapshot().Shops[0].News)
	}
}

// A follows.json from before the highest id was kept: it is what the seen ids say.
func TestFollowLoadOlderFile(t *testing.T) {
	testkit.NewStore(t)
	FollowReload()
	t.Cleanup(FollowReload)
	if err := os.WriteFile(followPath(), []byte(`{"ver":1,"shops":[{"sub":"luna","name":"L","added":1,"seen":["101","130","9"],"checked":5}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	FollowSnapshot()
	followMu.Lock()
	s := followData.Shops[0]
	followMu.Unlock()
	if s.Top != 130 || !s.Listed {
		t.Fatalf("top %d, listed %v", s.Top, s.Listed)
	}
	if news := s.apply(10, &followPage{Items: []followHit{{ID: "131"}, {ID: "120"}}}); len(news) != 1 || news[0].ID != "131" {
		t.Fatalf("%+v", news)
	}
}

// A shop that was not there at its first looks (404) and appears later: the first list it ever shows is
// written down, not announced whole.
func TestFollowShopAppearsLater(t *testing.T) {
	followTimes(t, time.Hour, time.Hour)
	shop := &followShop{items: map[string][][3]string{"luna": {{"103", "C", "¥ 1"}, {"102", "B", "¥ 1"}, {"101", "A", "¥ 1"}}}, code: map[string]int{"luna": 404}}
	shop.serve(t)
	st := testkit.NewStore(t)
	if _, err := FollowAdd(st, FollowSeed{Sub: "luna"}); err != nil {
		t.Fatal(err)
	}
	if v := waitSeeded(t, "luna"); v.Err == "" { // the first look: 404
		t.Fatalf("%+v", v)
	}
	daysPass(4 * time.Hour)
	if n, _ := FollowRound(st, false); n != 1 { // the daily look: 404 again
		t.Fatalf("round %d", n)
	}
	daysPass(2 * time.Hour)
	shop.set(func() { shop.code["luna"] = 0 }) // the shop is public
	if n, _ := FollowRound(st, false); n != 1 {
		t.Fatalf("round %d", n)
	}
	if v := FollowSnapshot().Shops[0]; len(v.News) != 0 || v.Items != 3 || v.Err != "" {
		t.Fatalf("the first list the shop ever showed is announced: %+v", v)
	}
	daysPass(2 * time.Hour)
	shop.set(func() { shop.items["luna"] = append([][3]string{{"104", "D", "¥ 1"}}, shop.items["luna"]...) })
	if n, _ := FollowRound(st, false); n != 1 || len(FollowSnapshot().Shops[0].News) != 1 {
		t.Fatalf("a new item after it: %+v", FollowSnapshot().Shops[0].News)
	}
}

// A shop with nothing for sale at its first look: what it puts on sale later is new.
func TestFollowEmptyShopThenItems(t *testing.T) {
	s := &FollowShop{Sub: "luna", Seen: []string{}}
	if news := s.apply(1, &followPage{Name: "L"}); len(news) != 0 || !s.Listed || s.Top != 0 {
		t.Fatalf("%+v %+v", news, s)
	}
	if news := s.apply(2, &followPage{Items: []followHit{{ID: "12"}, {ID: "11"}}}); len(news) != 2 || s.Top != 12 {
		t.Fatalf("%+v", news)
	}
}

// follows.json is replaced by a library import while a round, or a first look, is asking Booth: what they
// write down afterwards goes to the imported list, never the old list over the imported file.
func TestFollowImportWhileAsking(t *testing.T) {
	followTimes(t, time.Hour, time.Hour)
	imported := `{"ver":1,"shops":[{"sub":"imported","name":"Imported shop","added":1,"seen":["1"],"checked":1,"tried":1}]}`
	var importAt string
	shop := &followShop{items: map[string][][3]string{"luna": {{"101", "A", "¥ 1"}}, "tail": {{"201", "T", "¥ 1"}}, "late": {{"301", "L", "¥ 1"}}}, code: map[string]int{}}
	asked := make(chan string, 8)
	shop.hook = func(sub string) {
		shop.mu.Lock()
		now := sub == importAt
		if now {
			importAt = ""
		}
		shop.mu.Unlock()
		if !now {
			return
		}
		// as library.OnDataImported does: another file takes the place, and the program is told
		if err := os.WriteFile(followPath(), []byte(imported), 0644); err != nil {
			t.Error(err)
		}
		FollowReload()
		asked <- sub
	}
	shop.serve(t)
	st := testkit.NewStore(t)
	for _, sub := range []string{"luna", "tail"} {
		if _, err := FollowAdd(st, FollowSeed{Sub: sub}); err != nil {
			t.Fatal(err)
		}
		waitSeeded(t, sub)
	}
	check := func(when string) {
		t.Helper()
		b, _ := os.ReadFile(followPath())
		var f followFile
		if json.Unmarshal(b, &f) != nil || len(f.Shops) != 1 || f.Shops[0].Sub != "imported" {
			t.Fatalf("%s: follows.json holds %s", when, b)
		}
		if s := FollowSnapshot(); len(s.Shops) != 1 || s.Shops[0].Sub != "imported" {
			t.Fatalf("%s: the window shows %+v", when, s.Shops)
		}
	}
	// during a round
	daysPass(2 * time.Hour)
	followMu.Lock()
	first := followDue(time.Now(), false)[0] // the first shop the round asks for
	followMu.Unlock()
	shop.set(func() { importAt = first })
	if _, err := FollowRound(st, false); err != nil {
		t.Fatal(err)
	}
	<-asked
	check("after the round")
	// during a first look
	shop.set(func() { importAt = "late" })
	if _, err := FollowAdd(st, FollowSeed{Sub: "late"}); err != nil {
		t.Fatal(err)
	}
	<-asked
	followLookMu.Lock() // the first look is over
	followLookMu.Unlock()
	check("after the first look")
}

// A null among a shop's news (a hand-edited file, or one from another program) is left out when the file is read.
func TestFollowLoadNullNews(t *testing.T) {
	testkit.NewStore(t)
	FollowReload()
	t.Cleanup(FollowReload)
	if err := os.WriteFile(followPath(), []byte(`{"ver":1,"shops":[null,{"sub":"luna","name":"L","added":1,"seen":["5"],"news":[null,{"id":"5","title":"T","at":3},null]}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := FollowSnapshot()
	if len(s.Shops) != 1 || len(s.Shops[0].News) != 1 || s.Shops[0].News[0].ID != "5" || s.Shops[0].Unread != 1 {
		t.Fatalf("%+v", s.Shops)
	}
	FollowSeen()
	FollowRead("")
	FollowTold([]string{"luna:5"})
}

// a shop whose address is all digits is a shop when it comes as an address; a number alone stays an item's
func TestFollowSubOfDigits(t *testing.T) {
	for in, want := range map[string]string{"https://12345.booth.pm/": "12345", "12345.booth.pm": "12345", "12345": "", "komado": "komado", "https://www.booth.pm/": ""} {
		if got := FollowSub(in); got != want {
			t.Errorf("FollowSub(%q) = %q, want %q", in, got, want)
		}
	}
}
