package booth

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
)

// Followed shops: Booth shops whose new items the player wants to hear of. A shop's item list
// (<shop>.booth.pm/items, its first page) is read once a day, one shop at a time, a few shops a round, never
// while Booth is turning requests away (the same rest as the wish list's); an item id above every id the
// list has shown before is a new arrival (Booth's ids ascend; an older item that moves onto the first page
// when another is taken off sale is not one). The first read only writes down what the shop has. Kept in
// follows.json.

// FollowNew: one new arrival, as the shop page showed it.
type FollowNew struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Price  string `json:"price,omitempty"` // as the shop writes it: "¥ 6,000"
	Thumb  string `json:"thumb,omitempty"`
	URL    string `json:"url,omitempty"`
	Adult  bool   `json:"adult,omitempty"`
	At     int64  `json:"at"`               // first seen
	Told   bool   `json:"told,omitempty"`   // the window has announced it
	Read   bool   `json:"read,omitempty"`   // 「全部标为已读」
	Unseen bool   `json:"unseen,omitempty"` // arrived since the player last opened the list of shops
}

type FollowShop struct {
	Sub     string       `json:"sub"` // the shop's subdomain: what names it
	Name    string       `json:"name"`
	Icon    string       `json:"icon,omitempty"`
	Added   int64        `json:"added"`
	Note    string       `json:"note,omitempty"`
	NoWatch bool         `json:"noWatch,omitempty"` // the player turned this shop's daily look off
	Seen    []string     `json:"seen"`              // every item id the list has shown so far
	Top     int64        `json:"top,omitempty"`     // the highest of them: only an id above it is a new arrival
	Listed  bool         `json:"listed,omitempty"`  // the list has been read once: what it shows from then on can be new
	News    []*FollowNew `json:"news,omitempty"`    // the newest first
	Checked int64        `json:"checked,omitempty"` // the last look that got an answer
	Tried   int64        `json:"tried,omitempty"`   // the last look
	Err     string       `json:"err,omitempty"`
}

type followFile struct {
	Ver     int           `json:"ver"`
	NoWatch bool          `json:"noWatch,omitempty"` // the player turned the daily look off for every shop
	Shops   []*FollowShop `json:"shops"`
}

const (
	followMax     = 200
	followNewsMax = 60   // new arrivals kept a shop
	followSeenMax = 4000 // item ids kept a shop (a list page shows a few dozen: the oldest go first)
	followNoteMax = 500
)

// How often Booth is asked. Tests shorten the times (VRCLIB_WISH_FAST=1, as for the wish list).
var (
	followFirst  = 5 * time.Minute  // after the start (the wish list goes first)
	followTick   = 10 * time.Minute // between two rounds
	followEvery  = 24 * time.Hour   // one look a shop
	followRetry  = 3 * time.Hour    // after a look that got no answer
	followGap    = 8 * time.Second  // between two shops of a round (a list page is bigger than an item's JSON)
	followBatch  = 6                // shops a round: a hundred shops are spread over the day
	followByHand = time.Hour        // 「立即检查」 leaves out what was looked at within this
)

func init() {
	if os.Getenv("VRCLIB_WISH_FAST") == "1" { // tests only: a day in a few seconds
		followFirst, followTick, followEvery, followRetry, followGap, followByHand = 4*time.Second, 4*time.Second, 6*time.Second, 6*time.Second, 300*time.Millisecond, 0
	}
}

var (
	followMu      sync.Mutex
	followData    followFile
	followFrom    string // the file followData was read from
	followRunMu   sync.Mutex
	followRunning bool
	followBrokeAt string // the shop the last round was turned away at
)

func followPath() string { return filepath.Join(core.DataDir, "follows.json") }

// followLoad reads the file once. Caller holds followMu.
func followLoad() {
	p := followPath()
	if followFrom == p {
		return
	}
	followFrom, followData = p, followFile{Ver: 1}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var f followFile
	if err := json.Unmarshal(b, &f); err != nil {
		// not thrown away: the next save would write an empty list over it
		_ = os.Rename(p, p+".broken")
		core.Logf("follows.json 无法读取，已另存为 follows.json.broken: %v", err)
		return
	}
	f.Ver = 1
	keep := f.Shops[:0]
	for _, s := range f.Shops {
		if s != nil && s.Sub != "" && s.Sub == strings.ToLower(s.Sub) && followSubOK(s.Sub) {
			if s.Seen == nil {
				s.Seen = []string{}
			}
			news := s.News[:0]
			for _, x := range s.News { // a null among them (a hand-edited file) would take the window down
				if x != nil {
					news = append(news, x)
				}
			}
			s.News = news
			if len(s.Seen) > 0 { // a file from before Top and Listed were kept
				s.Listed = true
				for _, id := range s.Seen {
					s.Top = max(s.Top, followID(id))
				}
			}
			keep = append(keep, s)
		}
	}
	f.Shops = keep
	followData = f
}

// followSave writes the file whole: to a temporary file that then takes its place. Caller holds followMu.
func followSave() {
	b, err := json.MarshalIndent(&followData, "", " ")
	if err != nil {
		return
	}
	p := followPath()
	tmp := p + ".tmp"
	err = func() error {
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		if _, err := f.Write(b); err != nil {
			f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}()
	if err == nil {
		for i := 0; ; i++ { // (Windows refuses for a moment while a scanner holds the old file open)
			if err = os.Rename(tmp, p); err == nil || i == 3 {
				break
			}
			time.Sleep(time.Duration(i+1) * 50 * time.Millisecond)
		}
	}
	if err != nil {
		_ = os.Remove(tmp)
		core.Logf("follows.json 保存失败: %v", err)
	}
}

func followFind(sub string) *FollowShop {
	for _, s := range followData.Shops {
		if s.Sub == sub {
			return s
		}
	}
	return nil
}

// FollowReload: follows.json was replaced from outside; read it again at the next use.
func FollowReload() {
	followMu.Lock()
	followFrom = ""
	followMu.Unlock()
}

// ---------- which shop ----------

var (
	reFollowSub  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	reFollowHost = regexp.MustCompile(`(?i)^(?:https?://)?([a-z0-9][a-z0-9-]*)\.booth\.pm(?:[/?#:]|$)`)
	reFollowTest = regexp.MustCompile(`(?i)^https?://[^/]+/shop/([a-z0-9-]+)(?:[/?#]|$)`)
	followNotSub = map[string]bool{"www": true, "accounts": true, "manage": true, "api": true, "asset": true, "booth": true, "static": true, "help": true, "checkout": true}
)

// FollowSub: the shop a pasted address or name stands for: "komado", "https://komado.booth.pm/items/123",
// "komado.booth.pm". "" when it is none.
func FollowSub(text string) string {
	text = strings.TrimSpace(text)
	addr := true
	if m := reFollowHost.FindStringSubmatch(text); m != nil {
		text = m[1]
	} else if m := reFollowTest.FindStringSubmatch(text); m != nil && strings.HasPrefix(strings.ToLower(text), strings.ToLower(core.BoothWebBase())+"/") && os.Getenv("VRCLIB_BOOTH_WEB") != "" {
		text = m[1] // the stand-in the tests use
	} else if strings.ContainsAny(text, "/.:") {
		return ""
	} else {
		addr = false
	}
	text = strings.ToLower(text)
	if !followSubOK(text) || (!addr && reWishID.MatchString(text)) { // a number alone is an item's; in an address it is the shop's name
		return ""
	}
	return text
}

// followSubOK: could be a shop's name in its address.
func followSubOK(sub string) bool { return reFollowSub.MatchString(sub) && !followNotSub[sub] }

// ShopURL: a shop's page, and ShopListURL its item list (the first page, the newest first).
func ShopURL(sub string) string {
	if os.Getenv("VRCLIB_BOOTH_WEB") != "" { // tests only: the stand-in has no subdomains
		return core.BoothWebBase() + "/shop/" + sub + "/"
	}
	return "https://" + sub + ".booth.pm/"
}
func ShopListURL(sub string) string { return ShopURL(sub) + "items" }

// ---------- reading a shop's list ----------

// followPage: what one read of a shop's item list gives.
type followPage struct {
	Name, Icon string
	Items      []followHit
}

type followHit struct {
	ID, Title, Price, Thumb, URL string
	Adult                        bool
}

var (
	reFollowItem  = regexp.MustCompile(`data-item="([^"]*)"|data-item='([^']*)'`)
	reFollowName  = regexp.MustCompile(`(?s)<[^>]+class="[^"]*\bshop-name-label\b[^"]*"[^>]*>(.*?)</`)
	reFollowIcon  = regexp.MustCompile(`class="[^"]*\bavatar-image\b[^"]*"[^>]*url\(\s*['"]?([^'")\s]+)`)
	reFollowIcon2 = regexp.MustCompile(`url\(\s*['"]?([^'")\s]+)[^>]*class="[^"]*\bavatar-image\b`)
	reFollowTitle = regexp.MustCompile(`<title>([^<]*)</title>`)
)

var errFollowShape = errors.New("无法解析店铺页面")

// parseShopPage reads a shop's list page: every item card carries its item as JSON (data-item), the header
// the shop's name and icon. Markup as the shop pages have it (checked against Booth2RSS, 2026-09).
func parseShopPage(page []byte) (*followPage, error) {
	s := string(page)
	fp := &followPage{}
	header := false
	if m := reFollowName.FindStringSubmatch(s); m != nil {
		fp.Name, header = htmlText(m[1]), true
	}
	if fp.Name == "" {
		if m := reFollowTitle.FindStringSubmatch(s); m != nil {
			fp.Name = strings.TrimSpace(strings.TrimSuffix(html.UnescapeString(m[1]), " - BOOTH"))
		}
	}
	if m := reFollowIcon.FindStringSubmatch(s); m != nil {
		fp.Icon = html.UnescapeString(m[1])
	} else if m := reFollowIcon2.FindStringSubmatch(s); m != nil {
		fp.Icon = html.UnescapeString(m[1])
	}
	seen := map[string]bool{}
	for _, m := range reFollowItem.FindAllStringSubmatch(s, -1) {
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}
		var o map[string]any
		if json.Unmarshal([]byte(html.UnescapeString(raw)), &o) != nil {
			continue
		}
		h := followHit{ID: jStr(o, "id"), Title: strings.TrimSpace(jStr(o, "name")), Price: strings.TrimSpace(jStr(o, "price")), URL: jStr(o, "url")}
		if h.ID == "" || !reWishID.MatchString(h.ID) || seen[h.ID] {
			continue
		}
		seen[h.ID] = true
		h.Adult, _ = o["is_adult"].(bool)
		if thumbs, _ := o["thumbnail_image_urls"].([]any); len(thumbs) > 0 {
			if t, _ := thumbs[0].(string); t != "" {
				h.Thumb = boothThumb300(t)
			}
		}
		fp.Items = append(fp.Items, h)
	}
	if len(fp.Items) == 0 && !header && !strings.Contains(s, "data-item") { // (a shop with nothing for sale still has its header)
		return nil, errFollowShape
	}
	return fp, nil
}

// followFetch reads a shop's list: one request. gone: Booth has no such shop.
func followFetch(c *http.Client, sub string) (fp *followPage, gone bool, err error) {
	req, _ := http.NewRequest("GET", ShopListURL(sub), nil)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
	req.Header.Set("Cookie", "adult=t")
	resp, err := c.Do(req)
	if err != nil {
		return nil, false, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	resp.Body.Close()
	switch {
	case resp.StatusCode == 404:
		return nil, true, nil
	case resp.StatusCode == 429 || resp.StatusCode == 403:
		return nil, false, busyErr{resp.StatusCode}
	case resp.StatusCode != 200:
		return nil, false, fmt.Errorf("Booth 返回错误（HTTP %d）", resp.StatusCode)
	}
	fp, err = parseShopPage(body)
	return fp, false, err
}

// followID: an item id as a number (0 when it is none).
func followID(id string) int64 {
	n, _ := strconv.ParseInt(id, 10, 64)
	return n
}

// apply takes in what the list shows now and returns the new arrivals: the ids above every id the list has
// shown before. None at the first list the shop ever shows (a shop that was not there at its first looks
// included), which only writes down what the shop has. Caller holds followMu.
func (s *FollowShop) apply(now int64, fp *followPage) []*FollowNew {
	first, top := !s.Listed, s.Top
	s.Listed, s.Checked, s.Err = true, now, ""
	if fp.Name != "" {
		s.Name = fp.Name
	}
	if fp.Icon != "" {
		s.Icon = fp.Icon
	}
	known := make(map[string]bool, len(s.Seen))
	for _, id := range s.Seen {
		known[id] = true
	}
	var news []*FollowNew
	for _, h := range fp.Items {
		if known[h.ID] {
			continue
		}
		known[h.ID] = true
		s.Seen = append(s.Seen, h.ID)
		id := followID(h.ID)
		s.Top = max(s.Top, id)
		if first || id <= top { // an older item that moved up the list: only the first page is read
			continue
		}
		news = append(news, &FollowNew{ID: h.ID, Title: h.Title, Price: h.Price, Thumb: h.Thumb, URL: h.URL, Adult: h.Adult, At: now, Unseen: true})
	}
	if n := len(s.Seen); n > followSeenMax {
		s.Seen = append(s.Seen[:0], s.Seen[n-followSeenMax:]...)
	}
	if len(news) > 0 {
		// the list shows the newest first: so does the record
		s.News = append(news, s.News...)
		if len(s.News) > followNewsMax {
			s.News = s.News[:followNewsMax]
		}
	}
	return news
}

// ---------- following ----------

// FollowSeed: what the window knows of a shop (the details panel), or a pasted address.
type FollowSeed struct {
	Text string `json:"text"` // an address or a name, when not from the panel
	Sub  string `json:"sub"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

var errFollowFull = errors.New("关注的店铺已达上限，请先取消关注不再需要的店铺")

// FollowAdd follows a shop. What is pasted has to be an address (a word alone would make a shop of any typing
// slip): a shop's, or an item's, which is asked for first (one request) to know whose it is. The shop's list is
// read in the background right away, so that what it has now does not count as new.
func FollowAdd(st *core.Store, seed FollowSeed) (*FollowShop, error) {
	sub := strings.ToLower(strings.TrimSpace(seed.Sub)) // from the panel it is the shop's name in its address, whatever it looks like
	if !followSubOK(sub) {
		sub = FollowSub(seed.Sub)
	}
	name, icon := strings.TrimSpace(seed.Name), seed.Icon
	if text := strings.TrimSpace(seed.Text); sub == "" && text != "" {
		if strings.ContainsAny(text, "./") {
			sub = FollowSub(text)
		}
		if sub == "" {
			if id := WishID(text); id != "" {
				if boothPaused() {
					return nil, errors.New("Booth 暂时限制了访问，请稍后重试")
				}
				fresh, gone, err := wishFetch(core.HTTPClient(st), id)
				var busy busyErr
				switch {
				case gone:
					return nil, errors.New("Booth 上未找到该商品（可能已下架）")
				case errors.As(err, &busy):
					turnedAway()
					return nil, err
				case err != nil:
					return nil, errors.New("无法获取商品信息：" + core.FriendlyNetErr(err))
				}
				sub, name = FollowSub(fresh.ShopURL), fresh.Shop
			}
		}
	}
	if sub == "" {
		return nil, errors.New("请输入 Booth 店铺链接（如 https://example.booth.pm/）或商品链接")
	}
	followMu.Lock()
	followLoad()
	if s := followFind(sub); s != nil {
		c := *s
		followMu.Unlock()
		return &c, nil
	}
	if len(followData.Shops) >= followMax {
		followMu.Unlock()
		return nil, errFollowFull
	}
	if name == "" {
		name = sub
	}
	s := &FollowShop{Sub: sub, Name: name, Icon: icon, Added: time.Now().Unix(), Seen: []string{}}
	followData.Shops = append(followData.Shops, s)
	followSave()
	c := *s
	followMu.Unlock()
	core.BumpRev()
	go followFirstLook(st, sub)
	return &c, nil
}

var followLookMu sync.Mutex // first looks go one after the other, however fast shops are followed

// followFirstLook reads a just-followed shop's list: what it has now is written down, nothing is announced.
func followFirstLook(st *core.Store, sub string) {
	followLookMu.Lock()
	defer followLookMu.Unlock()
	if boothPaused() || core.Quitting.Load() {
		return // the daily look fills it in
	}
	fp, gone, err := followFetch(core.HTTPClient(st), sub)
	now := time.Now().Unix()
	var busy busyErr
	if errors.As(err, &busy) {
		turnedAway()
		core.BumpRev()
		return
	}
	followMu.Lock()
	followLoad() // (the file may have been replaced meanwhile: FollowReload)
	if s := followFind(sub); s != nil && s.Checked == 0 {
		s.Tried = now
		switch {
		case gone:
			s.Err = "Booth 上未找到该店铺"
		case err != nil:
			s.Err = core.FriendlyNetErr(err)
		default:
			s.apply(now, fp)
		}
		followSave()
	}
	followMu.Unlock()
	core.BumpRev()
	time.Sleep(boothGap)
}

func FollowRemove(sub string) bool {
	followMu.Lock()
	defer followMu.Unlock()
	followLoad()
	for i, s := range followData.Shops {
		if s.Sub == sub {
			followData.Shops = append(followData.Shops[:i], followData.Shops[i+1:]...)
			followSave()
			core.BumpRev()
			return true
		}
	}
	return false
}

func FollowNote(sub, note string) bool {
	note = strings.TrimSpace(note)
	if r := []rune(note); len(r) > followNoteMax {
		note = string(r[:followNoteMax])
	}
	followMu.Lock()
	defer followMu.Unlock()
	followLoad()
	s := followFind(sub)
	if s == nil {
		return false
	}
	if s.Note != note {
		s.Note = note
		followSave()
		core.BumpRev()
	}
	return true
}

// FollowSeen: the player has the list of shops open: the dot on its entry goes.
func FollowSeen() {
	followMu.Lock()
	defer followMu.Unlock()
	followLoad()
	n := 0
	for _, s := range followData.Shops {
		for _, x := range s.News {
			if x.Unseen {
				x.Unseen = false
				n++
			}
		}
	}
	if n > 0 {
		followSave()
		core.BumpRev()
	}
}

// FollowRead: 「全部标为已读」 for one shop, or for every shop when sub is "".
func FollowRead(sub string) bool {
	followMu.Lock()
	defer followMu.Unlock()
	followLoad()
	n, found := 0, sub == ""
	for _, s := range followData.Shops {
		if sub != "" && s.Sub != sub {
			continue
		}
		found = true
		for _, x := range s.News {
			if !x.Read || x.Unseen {
				x.Read, x.Unseen = true, false
				n++
			}
		}
	}
	if n > 0 {
		followSave()
		core.BumpRev()
	}
	return found
}

// FollowTold: the window has announced these arrivals ("sub:id"): never again.
func FollowTold(keys []string) {
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	followMu.Lock()
	defer followMu.Unlock()
	followLoad()
	n := 0
	for _, s := range followData.Shops {
		for _, x := range s.News {
			if !x.Told && want[s.Sub+":"+x.ID] {
				x.Told = true
				n++
			}
		}
	}
	if n > 0 {
		followSave()
	}
}

// FollowSetWatch turns the daily look on or off: for one shop, or for all of them when sub is "".
func FollowSetWatch(sub string, on bool) bool {
	followMu.Lock()
	defer followMu.Unlock()
	followLoad()
	if sub == "" {
		if followData.NoWatch == on {
			followData.NoWatch = !on
			followSave()
			core.BumpRev()
		}
		return true
	}
	s := followFind(sub)
	if s == nil {
		return false
	}
	if s.NoWatch == on {
		s.NoWatch = !on
		followSave()
		core.BumpRev()
	}
	return true
}

// ---------- what the window shows ----------

type FollowNewView struct {
	Key    string `json:"key"` // "sub:id"
	ID     string `json:"id"`
	Title  string `json:"title"`
	Price  string `json:"price"`
	Thumb  string `json:"thumb"`
	URL    string `json:"url"`
	Adult  bool   `json:"adult,omitempty"`
	At     int64  `json:"at"`
	Told   bool   `json:"told,omitempty"`
	Read   bool   `json:"read,omitempty"`
	Unseen bool   `json:"unseen,omitempty"`
}

type FollowShopView struct {
	Sub     string          `json:"sub"`
	Name    string          `json:"name"`
	Icon    string          `json:"icon"`
	URL     string          `json:"url"`
	Added   int64           `json:"added"`
	Checked int64           `json:"checked"`
	Err     string          `json:"err,omitempty"`
	Note    string          `json:"note"`
	Watch   bool            `json:"watch"`
	Items   int             `json:"items"` // item ids seen so far (of a large shop: what its first page has shown), not what the shop has
	Unread  int             `json:"unread"`
	News    []FollowNewView `json:"news"`
}

type FollowState struct {
	Shops  []FollowShopView `json:"shops"`
	Unseen int              `json:"unseen"`
	Watch  bool             `json:"watch"`
	Paused bool             `json:"paused"` // Booth is turning requests away: looks wait
	Busy   bool             `json:"busy"`   // a round is under way
}

func (s *FollowShop) view() FollowShopView {
	v := FollowShopView{Sub: s.Sub, Name: s.Name, Icon: s.Icon, URL: ShopURL(s.Sub), Added: s.Added, Checked: s.Checked, Err: s.Err, Note: s.Note,
		Watch: !s.NoWatch, Items: len(s.Seen), News: make([]FollowNewView, 0, len(s.News))}
	for _, x := range s.News {
		if !x.Read {
			v.Unread++
		}
		u := x.URL
		if u == "" {
			u = ShopURL(s.Sub) + "items/" + x.ID
		}
		v.News = append(v.News, FollowNewView{Key: s.Sub + ":" + x.ID, ID: x.ID, Title: x.Title, Price: x.Price, Thumb: x.Thumb, URL: u, Adult: x.Adult, At: x.At, Told: x.Told, Read: x.Read, Unseen: x.Unseen})
	}
	return v
}

// FollowSnapshot: the shops for the window, the newest arrivals' shops first.
func FollowSnapshot() FollowState {
	followRunMu.Lock()
	busy := followRunning
	followRunMu.Unlock()
	followMu.Lock()
	defer followMu.Unlock()
	followLoad()
	st := FollowState{Shops: make([]FollowShopView, 0, len(followData.Shops)), Watch: !followData.NoWatch, Paused: boothPaused(), Busy: busy}
	for _, s := range followData.Shops {
		for _, x := range s.News {
			if x.Unseen {
				st.Unseen++
			}
		}
		st.Shops = append(st.Shops, s.view())
	}
	last := func(v FollowShopView) int64 {
		if len(v.News) > 0 {
			return v.News[0].At
		}
		return 0
	}
	sort.SliceStable(st.Shops, func(i, j int) bool {
		a, b := st.Shops[i], st.Shops[j]
		if (a.Unread > 0) != (b.Unread > 0) {
			return a.Unread > 0
		}
		if last(a) != last(b) {
			return last(a) > last(b)
		}
		return a.Added > b.Added
	})
	return st
}

// ---------- the daily look ----------

// followDue: the shops a round looks at, the longest-waiting first. Caller holds followMu.
func followDue(now time.Time, byHand bool) []string {
	type due struct {
		sub  string
		last int64
	}
	var list []due
	for _, s := range followData.Shops {
		if s.NoWatch && !byHand {
			continue
		}
		wait := followEvery
		switch {
		case byHand:
			wait = followByHand
		case s.Tried > s.Checked:
			wait = followRetry
		}
		last := max(s.Tried, s.Checked)
		if now.Sub(time.Unix(last, 0)) >= wait {
			list = append(list, due{s.Sub, last})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].last < list[j].last })
	subs := make([]string, 0, len(list))
	for _, d := range list {
		subs = append(subs, d.sub)
	}
	return subs
}

// FollowRound reads the lists of the shops that are due, a few at a time and one after the other. byHand: the
// player asked (every shop not looked at within the hour, in one go). It returns how many were read.
func FollowRound(st *core.Store, byHand bool) (int, error) {
	followRunMu.Lock()
	if followRunning {
		followRunMu.Unlock()
		return 0, errors.New("正在检查店铺上新，请稍候")
	}
	followRunning = true
	followRunMu.Unlock()
	defer func() {
		followRunMu.Lock()
		followRunning = false
		followRunMu.Unlock()
		core.BumpRev()
	}()
	if boothPaused() {
		return 0, errors.New("Booth 暂时限制了访问，请稍后重试")
	}
	followMu.Lock()
	followLoad()
	if followData.NoWatch && !byHand {
		followMu.Unlock()
		return 0, nil
	}
	subs := followDue(time.Now(), byHand)
	followMu.Unlock()
	if !byHand && len(subs) > followBatch {
		subs = subs[:followBatch]
	}
	if len(subs) == 0 {
		return 0, nil
	}
	if byHand {
		core.BumpRev() // the window shows that a round is under way
	}
	c := core.HTTPClient(st)
	done, fails, refused := 0, 0, false
	for i, sub := range subs {
		if core.Quitting.Load() {
			break
		}
		if i > 0 {
			time.Sleep(followGap + time.Duration(rand.Int63n(int64(followGap)/2+1)))
			if boothPaused() || core.Quitting.Load() {
				break
			}
		}
		// turned off meanwhile (for every shop, or for this one): the round stops at its next step
		followMu.Lock()
		followLoad() // the file may have been replaced since the round began (a library import): never the old list over it
		s := followFind(sub)
		off, allOff := s == nil || (!byHand && s.NoWatch), !byHand && followData.NoWatch
		followMu.Unlock()
		if allOff {
			break
		}
		if off {
			continue
		}
		fp, gone, err := followFetch(c, sub)
		now := time.Now().Unix()
		var busy busyErr
		if errors.As(err, &busy) {
			// refused again as the first one asked for after the rest, and not for asking too often: it is this
			// shop Booth does not show. It gets the error like any other, and the round goes on.
			own := busy.status == 403 && i == 0 && sub == followBrokeAt
			if !own {
				followBrokeAt, refused = sub, true
				turnedAway()
				core.Logf("Booth 限制了访问（HTTP %d）：店铺上新检查已暂停，稍后继续", busy.status)
				break // not written down as this shop's look: it stays due, like the ones after it
			}
		}
		followMu.Lock()
		followLoad()
		if s = followFind(sub); s == nil { // unfollowed meanwhile
			followMu.Unlock()
			continue
		}
		s.Tried = now
		switch {
		case gone:
			s.Checked, s.Err = now, "Booth 上未找到该店铺"
			done++
		case err != nil:
			s.Err = core.FriendlyNetErr(err)
			fails++
		default:
			fails = 0
			if news := s.apply(now, fp); len(news) > 0 {
				core.Logf("关注的店铺：%s 有 %d 件新商品", s.Sub, len(news))
			}
			done++
		}
		followSave()
		followMu.Unlock()
		if fails >= 2 { // the network is not there: the rest waits for the next round
			break
		}
	}
	if done > 0 && !refused {
		followBrokeAt = ""
		answeredAgain()
	}
	return done, nil
}

// FollowStartRound: 「立即检查」. It returns how many shops are about to be read; the reading goes on in the
// background.
func FollowStartRound(st *core.Store) (int, error) {
	followRunMu.Lock()
	running := followRunning
	followRunMu.Unlock()
	if running {
		return 0, errors.New("正在检查店铺上新，请稍候")
	}
	if boothPaused() {
		return 0, errors.New("Booth 暂时限制了访问，请稍后重试")
	}
	followMu.Lock()
	followLoad()
	n := len(followDue(time.Now(), true))
	followMu.Unlock()
	if n > 0 {
		go func() { _, _ = FollowRound(st, true) }()
	}
	return n, nil
}

// FollowLoop runs for as long as the program does.
func FollowLoop(st *core.Store) {
	time.Sleep(followFirst)
	for {
		// one thing asking Booth at a time: not while the library's own Booth step or the wish list is
		wishRunMu.Lock()
		wishBusy := wishRunning
		wishRunMu.Unlock()
		if !core.Quitting.Load() && !core.TaskBooth.Snapshot().Running && !wishBusy {
			_, _ = FollowRound(st, false)
		}
		time.Sleep(followTick)
	}
}
