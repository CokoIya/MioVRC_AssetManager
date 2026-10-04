package booth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// The price and the variations of the item JSON, in the shapes the live site gives (read 2026-10-03: one
// variation without a name, and several with names under a "¥ 1,060~" price).
func TestBoothItemPrices(t *testing.T) {
	one := `{"name":"Kaguya","price":"¥ 6,000","is_sold_out":false,"is_end_of_sale":false,
	"variations":[{"buyee_html":null,"downloadable":null,"id":8511311,"is_empty_stock":false,"name":null,"order_url":null,"price":6000,"status":"addable_to_cart","type":"digital"}]}`
	it, err := parseBoothItem([]byte(one))
	if err != nil || it.PriceNum != 6000 || len(it.Vars) != 1 || it.Vars[0].ID != "8511311" || it.Vars[0].Name != "" || it.Vars[0].Price != 6000 || it.SoldOut || it.EndOfSale {
		t.Fatalf("%v %+v", err, it)
	}
	several := `{"name":"Key holder","price":"¥ 1,060~","is_sold_out":true,
	"variations":[{"id":8395957,"name":"70 x 70 (mm)","price":1470,"is_empty_stock":true},{"id":8395936,"name":"100 x 100 (mm)","price":1060,"is_empty_stock":false},{"id":1,"name":"no price"}]}`
	it, err = parseBoothItem([]byte(several))
	if err != nil || it.PriceNum != 1060 || len(it.Vars) != 2 || !it.Vars[0].Empty || it.Vars[1].Name != "100 x 100 (mm)" || !it.SoldOut {
		t.Fatalf("%v %+v", err, it)
	}
	// an older shape without variations: the number in the price text
	it, _ = parseBoothItem([]byte(`{"name":"a","price":"¥ 1,500"}`))
	if it.PriceNum != 1500 || len(it.Vars) != 0 {
		t.Fatalf("%+v", it)
	}
	for s, want := range map[string]int64{"¥ 6,000": 6000, "¥ 1,060~": 1060, "¥ 0": 0, "1500": 1500, "": -1, "無料": -1, "¥ 12,345,678 円": 12345678} {
		if got := priceNumber(s); got != want {
			t.Errorf("priceNumber(%q) = %d, want %d", s, got, want)
		}
	}
	if s := PriceText("JPY", 1234567); s != "¥ 1,234,567" || PriceText("JPY", 0) != "¥ 0" || PriceText("JPY", 999) != "¥ 999" || PriceText("JPY", -1) != "" || PriceText("USD", 1500) != "1,500 USD" {
		t.Errorf("PriceText: %q", s)
	}
}

func TestWishID(t *testing.T) {
	t.Setenv("VRCLIB_BOOTH_WEB", "http://127.0.0.1:8123")
	for text, want := range map[string]string{
		"5058077": "5058077", " 5058077 ": "5058077", "https://booth.pm/ja/items/5058077": "5058077", "https://booth.pm/items/5058077?utm=x": "5058077",
		"https://paryi.booth.pm/items/8562330": "8562330", "booth.pm/zh-cn/items/123": "123", "https://booth.pm/en/items/123#x": "123", "http://127.0.0.1:8123/ja/items/77.json": "77",
		"https://booth.pm.evil.example/items/1": "", "https://jinxxy.com/aesu/items/1": "", "https://example.com/ja/items/123": "", "http://127.0.0.1:9999/ja/items/77": "",
		"abc": "", "": "", "12a": "", "https://booth.pm/ja/items/": "", "https://booth.pm/ja/items/1234567890123": "",
	} {
		if got := WishID(text); got != want {
			t.Errorf("WishID(%q) = %q, want %q", text, got, want)
		}
	}
}

func bi(price int64, vars ...boothVar) *boothItem {
	it := &boothItem{Name: "Dress", Shop: "Shop", PriceNum: price, Vars: vars}
	if len(vars) == 0 {
		it.Vars = []boothVar{{ID: "1", Price: price}}
	}
	return it
}

// What a look finds different: a lower price, a free item, a higher price, no longer for sale, gone, back.
func TestWishApplyChanges(t *testing.T) {
	it := &WishItem{Site: "booth", ID: "1", Currency: "JPY", Price: -1, First: -1, Low: -1}
	if ch := it.apply(100, bi(3000), false); ch != nil || it.Price != 3000 || it.First != 3000 || it.Low != 3000 || it.Title != "Dress" || len(it.Hist) != 1 {
		t.Fatalf("first look: %+v %+v", ch, it)
	}
	if ch := it.apply(200, bi(3000), false); ch != nil || len(it.Hist) != 1 || it.Unseen {
		t.Fatalf("nothing changed: %+v", ch)
	}
	ch := it.apply(300, bi(2400), false)
	if ch == nil || ch.Kind != "drop" || ch.Old != 3000 || ch.New != 2400 || !it.Unseen || it.Low != 2400 || it.LowAt != 300 || len(it.Hist) != 2 {
		t.Fatalf("drop: %+v %+v", ch, it)
	}
	if ch := it.apply(350, bi(2400), false); ch != nil || it.Change == nil || it.Change.At != 300 {
		t.Fatalf("the same price again is not a second change: %+v", ch)
	}
	if ch := it.apply(400, bi(3200), false); ch == nil || ch.Kind != "rise" || ch.Old != 2400 || ch.New != 3200 || it.Low != 2400 {
		t.Fatalf("rise: %+v", ch)
	}
	sold := bi(3200)
	sold.SoldOut = true
	if ch := it.apply(500, sold, false); ch == nil || ch.Kind != "off" || it.State != "off" || len(it.Hist) != 4 {
		t.Fatalf("sold out: %+v %+v", ch, it)
	}
	if ch := it.apply(600, nil, true); ch == nil || ch.Kind != "gone" || it.State != "gone" || it.Price != 3200 {
		t.Fatalf("gone: %+v", ch)
	}
	if ch := it.apply(650, nil, true); ch != nil {
		t.Fatalf("still gone: %+v", ch)
	}
	if ch := it.apply(700, bi(3200), false); ch == nil || ch.Kind != "back" || it.State != "" {
		t.Fatalf("back: %+v", ch)
	}
	if ch := it.apply(800, bi(0), false); ch == nil || ch.Kind != "free" || ch.Old != 3200 || ch.New != 0 || it.Low != 0 {
		t.Fatalf("free: %+v", ch)
	}
	// back and cheaper at once: the lower price is what is told
	end := bi(500)
	end.EndOfSale = true
	it.apply(900, end, false)
	if ch := it.apply(1000, bi(300), false); ch == nil || ch.Kind != "drop" || ch.Old != 500 || ch.New != 300 {
		t.Fatalf("back and cheaper: %+v", ch)
	}
	// a lower price of an item that cannot be bought is not told as one
	it.apply(1100, end, false)
	end2 := bi(100)
	end2.EndOfSale = true
	if ch := it.apply(1200, end2, false); ch != nil {
		t.Fatalf("cheaper but not for sale: %+v", ch)
	}
	for i := 0; i < 80; i++ {
		it.apply(int64(2000+i), bi(int64(1000+i)), false)
	}
	if len(it.Hist) != wishHistKeep || it.Hist[len(it.Hist)-1].Price != 1079 {
		t.Fatalf("history kept: %d", len(it.Hist))
	}
}

// Variations are compared one by one: the one that got cheaper is named, also when it is not the cheapest.
func TestWishApplyVariations(t *testing.T) {
	it := &WishItem{Site: "booth", ID: "1", Currency: "JPY", Price: -1, First: -1, Low: -1}
	it.apply(100, bi(1000, boothVar{ID: "a", Name: "Plum", Price: 1000}, boothVar{ID: "b", Name: "Full set", Price: 3000}), false)
	if len(it.Vars) != 2 || len(it.Vars[1].Hist) != 1 || it.Price != 1000 {
		t.Fatalf("%+v", it)
	}
	ch := it.apply(200, bi(1000, boothVar{ID: "a", Name: "Plum", Price: 1000}, boothVar{ID: "b", Name: "Full set", Price: 2400}), false)
	if ch == nil || ch.Kind != "drop" || ch.Var != "Full set" || ch.Old != 3000 || ch.New != 2400 || it.Price != 1000 || len(it.Vars[1].Hist) != 2 || len(it.Vars[0].Hist) != 1 {
		t.Fatalf("a variation got cheaper: %+v %+v", ch, it.Vars)
	}
	// one cheaper, one dearer: the lower price wins
	ch = it.apply(300, bi(1200, boothVar{ID: "a", Name: "Plum", Price: 1200}, boothVar{ID: "b", Name: "Full set", Price: 2000}), false)
	if ch == nil || ch.Kind != "drop" || ch.Var != "Full set" || it.Price != 1200 {
		t.Fatalf("mixed: %+v", ch)
	}
	// a new, cheaper variation moves the item's price
	ch = it.apply(400, bi(500, boothVar{ID: "a", Name: "Plum", Price: 1200}, boothVar{ID: "b", Name: "Full set", Price: 2000}, boothVar{ID: "c", Name: "Texture only", Price: 500}), false)
	if ch == nil || ch.Kind != "drop" || ch.Var != "Texture only" || ch.Old != 1200 || ch.New != 500 {
		t.Fatalf("new variation: %+v", ch)
	}
	ch = it.apply(500, bi(1500, boothVar{ID: "a", Name: "Plum", Price: 1500}, boothVar{ID: "b", Name: "Full set", Price: 2000}), false)
	if ch == nil || ch.Kind != "rise" || it.Price != 1500 {
		t.Fatalf("rise: %+v", ch)
	}
	// every variation out of stock: not for sale
	ch = it.apply(600, bi(1500, boothVar{ID: "a", Name: "Plum", Price: 1500, Empty: true}, boothVar{ID: "b", Name: "Full set", Price: 2000, Empty: true}), false)
	if ch == nil || ch.Kind != "off" || !it.Vars[0].Off {
		t.Fatalf("out of stock: %+v", ch)
	}
	v := it.view()
	if !v.From || len(v.Vars) != 2 || v.Vars[0].Price != "¥ 1,500" || v.Price != "¥ 1,500" || v.First != "¥ 1,000" || v.DiffDir != 1 || v.Diff != "¥ 500" || v.Pct != 50 || v.Low != "¥ 500" {
		t.Fatalf("view: %+v", v)
	}
}

type wishShop struct {
	mu    sync.Mutex
	asked []string
	price map[string]int64
	code  map[string]int
	hook  func(id string) // called while an item is being asked for
}

func (s *wishShop) serve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/ja/items/"), ".json")
		s.mu.Lock()
		s.asked = append(s.asked, id)
		code, price, hook := s.code[id], s.price[id], s.hook
		s.mu.Unlock()
		if hook != nil {
			hook(id)
		}
		if !strings.HasSuffix(r.URL.Path, ".json") {
			t.Errorf("a price check asked for more than the item's JSON: %s", r.URL.Path)
		}
		if c := r.Header.Get("Cookie"); c != "adult=t" {
			t.Errorf("cookie sent to the shop: %q", c)
		}
		if code != 0 {
			w.WriteHeader(code)
			return
		}
		fmt.Fprintf(w, `{"name":"Item %s","price":"¥ %d","shop":{"name":"Shop","subdomain":"shop"},"images":[{"original":"o","resized":"r"}],"variations":[{"id":%s0,"name":null,"price":%d,"is_empty_stock":false}]}`, id, price, id, price)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_BOOTH_WEB", srv.URL)
}

func (s *wishShop) took() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	got := strings.Join(s.asked, " ")
	s.asked = nil
	return got
}

func (s *wishShop) set(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
}

// wishTimes makes a day short for one test and puts everything back after it.
func wishTimes(t *testing.T, every, retry time.Duration) {
	f, e, r, g, h, pf := wishFirst, wishEvery, wishRetry, wishGap, wishByHand, boothPauseFirst
	wishEvery, wishRetry, wishGap, wishByHand, boothPauseFirst = every, retry, time.Millisecond, time.Hour, 150*time.Millisecond
	t.Cleanup(func() {
		wishFirst, wishEvery, wishRetry, wishGap, wishByHand = f, e, r, g, h
		boothMu.Lock()
		boothPauseFirst, boothPauseStep, boothPausedTill, boothBrokenOff, boothBrokeAt = pf, 0, time.Time{}, false, ""
		boothMu.Unlock()
		wishBrokeAt = ""
	})
}

// An item is looked at once a day and no more, a lower price is noticed once, the list is in wishlist.json
// (whole at every moment), and an item that was bought is left alone.
func TestWishRoundOnceADay(t *testing.T) {
	wishTimes(t, time.Hour, time.Hour)
	shop := &wishShop{price: map[string]int64{"101": 3000, "102": 500, "103": 800}, code: map[string]int{}}
	shop.serve(t)
	st := testkit.NewStore(t)
	for _, id := range []string{"101", "102"} {
		if _, err := WishAdd(st, WishSeed{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	// from a search result: listed at once with what the card showed, then Booth's own word for it
	if it, err := WishAdd(st, WishSeed{ID: core.BoothWebBase() + "/ja/items/103", Title: "from the card", Price: "¥ 900~"}); err != nil || it.Title != "from the card" || it.Price != 900 || it.First != 900 {
		t.Fatalf("add from a card: %v %+v", err, it)
	}
	for i := 0; i < 200 && WishSnapshot(st).Items[2].Checked == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if v := WishSnapshot(st).Items[2]; v.Title != "Item 103" || v.Price != "¥ 800" || v.First != "¥ 800" || v.Low != "¥ 800" || v.Change != nil || len(v.Hist) != 1 || v.Shop != "Shop" || v.Thumb == "" {
		t.Fatalf("after the first look: %+v", v)
	}
	if it, err := WishAdd(st, WishSeed{ID: "101"}); err != nil || it.ID != "101" || len(WishSnapshot(st).Items) != 3 {
		t.Fatalf("added twice: %v", err)
	}
	if _, err := WishAdd(st, WishSeed{ID: "not an item"}); err == nil {
		t.Fatal("anything is taken for an item")
	}
	if got := shop.took(); got != "101 102 103" {
		t.Fatalf("adding asked for %q", got)
	}
	// just added: nothing is due
	if n, err := WishRound(st, false); n != 0 || err != nil || shop.took() != "" {
		t.Fatalf("looked again at once: %d %v", n, err)
	}
	// a day later, with one of them cheaper and one bought meanwhile
	back := func(d time.Duration) {
		wishMu.Lock()
		for _, it := range wishData.Items {
			it.Checked -= int64(d / time.Second)
			it.Tried -= int64(d / time.Second)
		}
		wishMu.Unlock()
	}
	back(2 * time.Hour)
	shop.set(func() { shop.price["101"] = 2400 })
	st.Mu.Lock()
	st.Purchases["102"] = &core.Purchase{ID: "102", Name: "bought"}
	st.Mu.Unlock()
	rev := core.CurRev()
	if n, err := WishRound(st, false); n != 2 || err != nil {
		t.Fatalf("round: %d %v", n, err)
	}
	if got := shop.took(); got != "101 103" {
		t.Fatalf("the round asked for %q", got)
	}
	if core.CurRev() == rev {
		t.Error("the window is not told")
	}
	s := WishSnapshot(st)
	byID := map[string]WishView{}
	for _, v := range s.Items {
		byID[v.ID] = v
	}
	a := byID["101"]
	if s.Unseen != 1 || a.Change == nil || a.Change.Kind != "drop" || a.Change.Old != "¥ 3,000" || a.Change.New != "¥ 2,400" || a.Change.Told || !a.Unseen ||
		a.Price != "¥ 2,400" || a.First != "¥ 3,000" || a.DiffDir != -1 || a.Diff != "¥ 600" || a.Pct != -20 || a.Low != "¥ 2,400" || len(a.Hist) != 2 || a.Currency != "JPY" {
		t.Fatalf("the cheaper one: %+v %+v", a, a.Change)
	}
	if !byID["102"].Bought || byID["102"].Change != nil || byID["103"].Change != nil {
		t.Fatalf("the others: %+v %+v", byID["102"], byID["103"])
	}
	// a second round the same day asks for nothing; the change is told once
	if n, _ := WishRound(st, false); n != 0 || shop.took() != "" {
		t.Fatal("a second look the same day")
	}
	WishTold(map[string]int64{"booth:101": a.Change.At + 1}) // an older notice does not cover a newer change
	if WishSnapshot(st).Items[0].Change.Told {
		t.Fatal("told for another change")
	}
	WishTold(map[string]int64{"booth:101": a.Change.At})
	WishSeen()
	s = WishSnapshot(st)
	if !s.Items[0].Change.Told || s.Unseen != 0 || s.Items[0].Unseen {
		t.Fatalf("told and seen: %+v", s.Items[0])
	}
	// the file: whole, nothing left over, and read again as it was written
	b, err := os.ReadFile(filepath.Join(core.DataDir, "wishlist.json"))
	var f wishFile
	if err != nil || json.Unmarshal(b, &f) != nil || len(f.Items) != 3 || f.Items[0].Change == nil || !f.Items[0].Change.Told || f.Items[1].Bought == 0 {
		t.Fatalf("wishlist.json: %v %s", err, b)
	}
	if _, err := os.Stat(filepath.Join(core.DataDir, "wishlist.json.tmp")); err == nil {
		t.Error("the temporary file is left behind")
	}
	wishMu.Lock()
	wishFrom = "" // as after a restart
	wishMu.Unlock()
	if s := WishSnapshot(st); len(s.Items) != 3 || s.Items[0].Price != "¥ 2,400" || !s.Items[0].Change.Told {
		t.Fatalf("read again: %+v", s.Items)
	}
	// turned off: no looks, however long it has been; by hand it still is looked at
	WishSetWatch(false)
	back(48 * time.Hour)
	if n, _ := WishRound(st, false); n != 0 || shop.took() != "" || WishSnapshot(st).Watch {
		t.Fatal("looked while turned off")
	}
	if n, err := WishRound(st, true); n != 2 || err != nil || shop.took() != "101 103" {
		t.Fatalf("by hand: %d %v", n, err)
	}
	if n, _ := WishRound(st, true); n != 0 || shop.took() != "" {
		t.Fatal("by hand again within the hour")
	}
	// note and removal
	if !WishNote("booth:103", "  wait for the sale  ") || WishNote("booth:999", "x") || WishSnapshot(st).Items[2].Note != "wait for the sale" {
		t.Fatal("note")
	}
	if !WishRemove("booth:103") || WishRemove("booth:103") || len(WishSnapshot(st).Items) != 2 {
		t.Fatal("remove")
	}
}

// Booth answering 429 ends the round there and nothing is asked until the pause is over; an item that is gone
// is marked, not asked for again the same day; a look that fails for the network is tried again sooner.
func TestWishRoundBacksOff(t *testing.T) {
	wishTimes(t, 0, 0)
	shop := &wishShop{price: map[string]int64{"201": 100, "202": 200, "203": 300, "204": 400}, code: map[string]int{}}
	shop.serve(t)
	st := testkit.NewStore(t)
	for _, id := range []string{"201", "202", "203", "204"} {
		if _, err := WishAdd(st, WishSeed{ID: id}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1100 * time.Millisecond / 4) // (the order of a round is by the time of the last look, in seconds)
	}
	wishMu.Lock()
	for i, it := range wishData.Items {
		it.Checked, it.Tried = int64(1000+i), int64(1000+i)
	}
	wishMu.Unlock()
	shop.took()
	shop.set(func() { shop.code["202"] = http.StatusTooManyRequests; shop.code["204"] = http.StatusNotFound })
	if n, _ := WishRound(st, false); n != 1 {
		t.Fatalf("round: %d", n)
	}
	if got := shop.took(); got != "201 202" {
		t.Fatalf("asked for %q after being turned away", got)
	}
	s := WishSnapshot(st)
	if !s.Paused || s.Items[1].Err != "" || s.Items[1].Checked != 1001 {
		t.Fatalf("the refused one is written down as looked at: %+v", s.Items[1])
	}
	// during the pause: no round, no request when adding from a card, and an address alone is not enough
	if n, err := WishRound(st, false); n != 0 || err == nil {
		t.Fatal("a round during the pause")
	}
	if _, err := WishStartRound(st); err == nil {
		t.Fatal("by hand during the pause")
	}
	if it, err := WishAdd(st, WishSeed{ID: "205", Title: "Seen on a card", Shop: "S", Price: "¥ 1,200~"}); err != nil || it.Price != 1200 || it.First != 1200 || it.Checked != 0 {
		t.Fatalf("from a card during the pause: %v %+v", err, it)
	}
	time.Sleep(50 * time.Millisecond) // (its first look finds the pause and leaves it to the round)
	if _, err := WishAdd(st, WishSeed{ID: "206"}); err == nil {
		t.Fatal("added without knowing what it is")
	}
	if got := shop.took(); got != "" {
		t.Fatalf("asked during the pause: %q", got)
	}
	time.Sleep(200 * time.Millisecond)
	shop.set(func() { shop.code["202"] = 0; shop.price["205"] = 1000 })
	wishMu.Lock()
	wishFind("booth:201").Checked, wishFind("booth:201").Tried = time.Now().Unix()+3600, time.Now().Unix()+3600 // looked at: not due
	wishMu.Unlock()
	wishEvery = time.Hour
	if n, _ := WishRound(st, false); n != 4 {
		t.Fatalf("after the pause: %d", n)
	}
	if got := shop.took(); got != "205 202 203 204" {
		t.Fatalf("after the pause asked for %q", got)
	}
	s = WishSnapshot(st)
	if s.Paused || s.Items[3].State != "gone" || s.Items[3].Change == nil || s.Items[3].Change.Kind != "gone" || s.Items[4].Price != "¥ 1,000" || s.Items[4].Change != nil || s.Items[4].First != "¥ 1,000" {
		t.Fatalf("after the pause: %+v %+v", s.Items[3], s.Items[4])
	}
	boothMu.Lock()
	step := boothPauseStep
	boothMu.Unlock()
	if step != 0 {
		t.Errorf("the next pause does not start short again: %v", step)
	}
	// the shop does not answer at all: the round gives up after two, and those are due again after wishRetry
	wishMu.Lock()
	for _, it := range wishData.Items {
		it.Checked, it.Tried = 1000, 1000
	}
	wishMu.Unlock()
	t.Setenv("VRCLIB_BOOTH_WEB", "http://127.0.0.1:1")
	if n, _ := WishRound(st, false); n != 0 {
		t.Fatalf("looked without a shop: %d", n)
	}
	s = WishSnapshot(st)
	failed := 0
	for _, v := range s.Items {
		if v.Err != "" {
			failed++
		}
	}
	if failed != 2 {
		t.Fatalf("%d looks failed before the round gave up", failed)
	}
}

// An item Booth refuses by itself (403 again, first thing after the pause) gets the error and does not hold
// up the others for ever.
func TestWishRoundItemRefused(t *testing.T) {
	wishTimes(t, 0, 0)
	shop := &wishShop{price: map[string]int64{"301": 100, "302": 200}, code: map[string]int{}}
	shop.serve(t)
	st := testkit.NewStore(t)
	for _, id := range []string{"301", "302"} {
		if _, err := WishAdd(st, WishSeed{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	wishMu.Lock()
	wishData.Items[0].Checked, wishData.Items[0].Tried = 1000, 1000
	wishData.Items[1].Checked, wishData.Items[1].Tried = 1001, 1001
	wishMu.Unlock()
	shop.took()
	shop.set(func() { shop.code["301"] = http.StatusForbidden })
	if n, _ := WishRound(st, false); n != 0 || shop.took() != "301" || !WishSnapshot(st).Paused {
		t.Fatal("not stopped by the refusal")
	}
	time.Sleep(200 * time.Millisecond)
	if n, _ := WishRound(st, false); n != 1 {
		t.Fatalf("after the pause: %d", n)
	}
	if got := shop.took(); got != "301 302" {
		t.Fatalf("after the pause asked for %q", got)
	}
	s := WishSnapshot(st)
	if s.Paused || s.Items[0].Err == "" || s.Items[1].Err != "" {
		t.Fatalf("%+v", s.Items)
	}
}

// wishlist.json is replaced by a library import while a round, or a first look, is asking Booth: what they
// write down afterwards goes to the imported list, never the old list over the imported file.
func TestWishImportWhileAsking(t *testing.T) {
	wishTimes(t, time.Hour, time.Hour)
	t.Cleanup(WishReload)
	imported := `{"ver":1,"items":[{"site":"booth","id":"900","title":"Imported","currency":"JPY","price":100,"added":1,"first":100,"low":100,"checked":1,"tried":1}]}`
	var importAt string
	shop := &wishShop{price: map[string]int64{"101": 3000, "102": 500, "103": 800}, code: map[string]int{}}
	asked := make(chan string, 8)
	shop.hook = func(id string) {
		shop.mu.Lock()
		now := id == importAt
		if now {
			importAt = ""
		}
		shop.mu.Unlock()
		if !now {
			return
		}
		if err := os.WriteFile(wishPath(), []byte(imported), 0644); err != nil {
			t.Error(err)
		}
		WishReload()
		asked <- id
	}
	shop.serve(t)
	st := testkit.NewStore(t)
	for _, id := range []string{"101", "102"} {
		if _, err := WishAdd(st, WishSeed{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(when string) {
		t.Helper()
		b, _ := os.ReadFile(wishPath())
		var f wishFile
		if json.Unmarshal(b, &f) != nil || len(f.Items) != 1 || f.Items[0].ID != "900" {
			t.Fatalf("%s: wishlist.json holds %s", when, b)
		}
		if s := WishSnapshot(st); len(s.Items) != 1 || s.Items[0].ID != "900" {
			t.Fatalf("%s: the window shows %+v", when, s.Items)
		}
	}
	// during a round
	wishMu.Lock()
	for _, it := range wishData.Items {
		it.Checked -= 7200
		it.Tried -= 7200
	}
	first := strings.TrimPrefix(wishDue(time.Now(), false)[0], "booth:") // the first item the round asks for
	wishMu.Unlock()
	shop.set(func() { importAt = first })
	if _, err := WishRound(st, false); err != nil {
		t.Fatal(err)
	}
	<-asked
	check("after the round")
	// during a first look: an item listed with what a card showed is asked for in the background
	shop.set(func() { importAt = "103" })
	if _, err := WishAdd(st, WishSeed{ID: "103", Title: "from the card", Price: "¥ 900"}); err != nil {
		t.Fatal(err)
	}
	<-asked
	wishLookMu.Lock() // the first look is over
	wishLookMu.Unlock()
	check("after the first look")
}
