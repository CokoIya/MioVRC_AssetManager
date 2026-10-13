package booth

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"vrclib/internal/testkit"
)

// The items the player marked スキ！ on Booth go on the list in Booth's order: not those on it already, bought or
// in the library (skip), nor those the player took off it — until the player puts one back by hand; up to the
// list's size. Booth is not asked about them here: the rounds look at them as at any item not looked at yet.
func TestWishFromLikes(t *testing.T) {
	shop := &wishShop{price: map[string]int64{"105": 700}, code: map[string]int{}}
	shop.serve(t)
	st := testkit.NewStore(t)
	WishReload()
	t.Cleanup(WishReload)
	if err := os.WriteFile(wishPath(), []byte(`{"ver":1,"items":[
		{"site":"booth","id":"100","title":"Mine","currency":"JPY","price":300,"added":1,"first":300,"low":300,"checked":1},
		{"site":"booth","id":"105","title":"Not any more","currency":"JPY","price":700,"added":2,"first":700,"low":700,"checked":2}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if WishLikesOn() || WishSnapshot(st).Likes {
		t.Fatal("on by itself")
	}
	WishSetLikes(true)
	if !WishLikesOn() || !WishSnapshot(st).Likes {
		t.Fatal("not on")
	}
	if !WishRemove("booth:105") {
		t.Fatal("not removed")
	}
	likes := []LikeItem{
		{ID: "100", Name: "Mine, liked too"},
		{ID: "101", Name: "Dress", Price: "¥ 1,500~", Shop: "Luna", ShopURL: "https://luna.booth.pm/", Thumb: "https://booth.pximg.net/c/101.jpg"},
		{ID: "102", Name: "Bought"},
		{ID: "103"},
		{ID: "x104", Name: "not an item"},
		{ID: "105", Name: "Taken off the list before"},
		{ID: "106", Name: "Odd links", ShopURL: "javascript:alert(1)", Thumb: "http://elsewhere.example/106.jpg"},
	}
	added, full := WishFromLikes(likes, 40, func(id string) bool { return id == "102" })
	s := WishSnapshot(st)
	byID := map[string]WishView{}
	for _, v := range s.Items {
		byID[v.ID] = v
	}
	if added != 3 || full != 0 || len(s.Items) != 4 {
		t.Fatalf("added %d, full %d, list %d", added, full, len(s.Items))
	}
	if v := byID["100"]; v.Title != "Mine" || v.Like {
		t.Errorf("an item on the list already changed: %+v", v)
	}
	if v := byID["101"]; !v.Like || v.Title != "Dress" || v.Price != "¥ 1,500" || v.First != "¥ 1,500" || v.Low != "¥ 1,500" || len(v.Hist) != 1 ||
		v.Shop != "Luna" || v.Thumb != "https://booth.pximg.net/c/101.jpg" || v.Checked != 0 {
		t.Errorf("a like: %+v", v)
	}
	if v := byID["103"]; !v.Like || v.Title != "Booth #103" || v.Price != "" || v.First != "" {
		t.Errorf("a like known by its number only: %+v", v)
	}
	if v := byID["106"]; v.Thumb != "" {
		t.Errorf("a picture from elsewhere: %+v", v)
	}
	if byID["101"].Added <= byID["103"].Added || byID["103"].Added <= byID["106"].Added {
		t.Error("Booth's order is not kept for 「加入时间」")
	}
	if _, ok := byID["105"]; ok {
		t.Error("an item the player took off the list came back")
	}
	if s.LikesN != 40 || s.LikesAdded != 3 || s.LikesFull != 0 || s.LikesAt == 0 || s.LikesErr != "" {
		t.Errorf("what the window is told: %+v", s)
	}
	if got := shop.took(); got != "" {
		t.Errorf("Booth was asked about %q", got)
	}
	// the next sync: nothing new
	if added, _ := WishFromLikes(likes, 40, func(id string) bool { return id == "102" }); added != 0 {
		t.Errorf("added again: %d", added)
	}
	// a list that could not be read
	WishLikesFailed("返回的不是 JSON")
	if s := WishSnapshot(st); s.LikesErr != "返回的不是 JSON" || s.LikesAdded != 0 || len(s.Items) != 4 {
		t.Errorf("failed: %+v", s)
	}
	// put back by hand: off the list again later, and the likes do not bring it back; on it, they leave it
	if _, err := WishAdd(st, WishSeed{ID: "105", Title: "Again", Price: "¥ 700"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200 && shop.took() == ""; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if b, _ := os.ReadFile(wishPath()); strings.Contains(string(b), `"105": `) {
		t.Errorf("still remembered as taken off: %s", b)
	}
	WishSetLikes(false)
	if WishLikesOn() {
		t.Error("still on")
	}
}

// The list's size holds: the likes that do not fit are counted, not added.
func TestWishFromLikesFull(t *testing.T) {
	st := testkit.NewStore(t)
	WishReload()
	t.Cleanup(WishReload)
	f := wishFile{Ver: 1}
	for i := 0; i < wishMax-1; i++ {
		f.Items = append(f.Items, &WishItem{Site: "booth", ID: fmt.Sprint(1000 + i), Title: "x", Currency: "JPY", Price: 100, Added: 1, First: 100, Low: 100, Checked: 1})
	}
	b, _ := json.Marshal(f)
	if err := os.WriteFile(wishPath(), b, 0644); err != nil {
		t.Fatal(err)
	}
	added, full := WishFromLikes([]LikeItem{{ID: "1"}, {ID: "2"}, {ID: "3"}}, 3, nil)
	if s := WishSnapshot(st); added != 1 || full != 2 || len(s.Items) != wishMax || s.LikesFull != 2 || s.Items[len(s.Items)-1].ID != "1" {
		t.Errorf("added %d, full %d, list %d, told %d", added, full, len(s.Items), s.LikesFull)
	}
}

// Many items taken off the list: the oldest are forgotten, so the file does not grow without end.
func TestWishOffKept(t *testing.T) {
	testkit.NewStore(t)
	WishReload()
	t.Cleanup(WishReload)
	wishMu.Lock()
	defer wishMu.Unlock()
	if err := wishLoad(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < wishOffKeep+5; i++ {
		wishOff(fmt.Sprint(i), int64(1000+i))
	}
	if len(wishData.Off) != wishOffKeep || wishData.Off["4"] != 0 || wishData.Off["5"] == 0 || wishData.Off[fmt.Sprint(wishOffKeep+4)] == 0 {
		t.Errorf("kept %d (0-4 gone: %v)", len(wishData.Off), wishData.Off["4"] == 0)
	}
}
