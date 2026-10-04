package booth

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
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

// The wish list: Booth items the player means to buy. Their prices are looked at once a day (one request an
// item, a few items at a time, never while Booth is turning requests away), and what changed is kept with the
// item: a lower or higher price, no longer for sale, for sale again. Kept in wishlist.json in the data folder.

// WishPoint: a price at a time (written down when it changed, not at every look).
type WishPoint struct {
	At    int64  `json:"at"`
	Price int64  `json:"price"`
	State string `json:"state,omitempty"` // "" for sale, "off" not for sale, "gone" taken off Booth
}

// WishVar: one variation of an item with several.
type WishVar struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Price int64       `json:"price"`
	Off   bool        `json:"off,omitempty"` // out of stock
	Hist  []WishPoint `json:"hist,omitempty"`
}

// WishChange: what the last look found different.
type WishChange struct {
	Kind string `json:"kind"` // drop, free, rise, off, gone, back
	At   int64  `json:"at"`
	Old  int64  `json:"old"`
	New  int64  `json:"new"`
	Var  string `json:"var,omitempty"`  // the variation it is about, when the item has several
	Told bool   `json:"told,omitempty"` // the window has announced it
}

type WishItem struct {
	Site     string      `json:"site"` // "booth"
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Shop     string      `json:"shop,omitempty"`
	ShopURL  string      `json:"shopUrl,omitempty"`
	Thumb    string      `json:"thumb,omitempty"`
	Currency string      `json:"currency"`
	Price    int64       `json:"price"` // the lowest variation's; -1 while not known
	Vars     []WishVar   `json:"vars,omitempty"`
	State    string      `json:"state,omitempty"`
	Added    int64       `json:"added"`
	First    int64       `json:"first"` // the price when it was added (-1: not known then)
	Low      int64       `json:"low"`   // the lowest price seen
	LowAt    int64       `json:"lowAt,omitempty"`
	Note     string      `json:"note,omitempty"`
	Checked  int64       `json:"checked,omitempty"` // the last look that got an answer
	Tried    int64       `json:"tried,omitempty"`   // the last look
	Err      string      `json:"err,omitempty"`
	Hist     []WishPoint `json:"hist,omitempty"`
	Change   *WishChange `json:"change,omitempty"`
	Unseen   bool        `json:"unseen,omitempty"` // changed since the player last opened the list
	Bought   int64       `json:"bought,omitempty"` // found among the purchases: no longer watched
}

func (it *WishItem) Key() string { return it.Site + ":" + it.ID }

type wishFile struct {
	Ver     int         `json:"ver"`
	NoWatch bool        `json:"noWatch,omitempty"` // the player turned the daily look off
	Items   []*WishItem `json:"items"`
}

const (
	wishHistKeep    = 30
	wishVarHistKeep = 12
	wishMax         = 500
	wishNoteMax     = 500
)

// How often Booth is asked. Tests shorten the times.
var (
	wishFirst  = 2 * time.Minute  // after the start: the start-up refresh asks Booth first
	wishTick   = 10 * time.Minute // between two rounds
	wishEvery  = 24 * time.Hour   // one look an item
	wishRetry  = 3 * time.Hour    // after a look that got no answer
	wishGap    = 4 * time.Second  // between two items of a round
	wishBatch  = 12               // items a round
	wishByHand = time.Hour        // 「立即检查」 leaves out what was looked at within this
)

func init() {
	if os.Getenv("VRCLIB_WISH_FAST") == "1" { // tests only: a day in a few seconds
		wishFirst, wishTick, wishEvery, wishRetry, wishGap, wishByHand = 3*time.Second, 4*time.Second, 6*time.Second, 6*time.Second, 300*time.Millisecond, 0
	}
}

var (
	wishMu      sync.Mutex
	wishData    wishFile
	wishFrom    string // the file wishData was read from (the data folder can change between tests)
	wishRunMu   sync.Mutex
	wishRunning bool
	wishBrokeAt string // the item the last round was turned away at
)

func wishPath() string { return filepath.Join(core.DataDir, "wishlist.json") }

// wishLoad reads the file once. Caller holds wishMu.
func wishLoad() {
	p := wishPath()
	if wishFrom == p {
		return
	}
	wishFrom, wishData = p, wishFile{Ver: 1}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var f wishFile
	if err := json.Unmarshal(b, &f); err != nil {
		// not thrown away: the next save would write an empty list over it
		_ = os.Rename(p, p+".broken")
		core.Logf("wishlist.json 无法读取，已另存为 wishlist.json.broken: %v", err)
		return
	}
	f.Ver = 1
	keep := f.Items[:0]
	for _, it := range f.Items {
		if it != nil && it.ID != "" && it.Site != "" {
			keep = append(keep, it)
		}
	}
	f.Items = keep
	wishData = f
}

// wishSave writes the file whole: to a temporary file that then takes its place. Caller holds wishMu.
func wishSave() {
	b, err := json.MarshalIndent(&wishData, "", " ")
	if err != nil {
		return
	}
	p := wishPath()
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
		core.Logf("wishlist.json 保存失败: %v", err)
	}
}

func wishFind(key string) *WishItem {
	for _, it := range wishData.Items {
		if it.Key() == key {
			return it
		}
	}
	return nil
}

// PriceText: a price as the shop writes it. Booth's are yen; nothing is converted.
func PriceText(currency string, n int64) string {
	if n < 0 {
		return ""
	}
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if currency == "JPY" || currency == "" {
		return "¥ " + s
	}
	return s + " " + currency
}

var (
	reWishURL = regexp.MustCompile(`(?i)^(?:https?://)?([a-z0-9.-]+(?::\d+)?)/(?:[a-z]{2}(?:-[a-z]{2})?/)?items/(\d{1,12})(?:[/?#.]|$)`)
	reWishID  = regexp.MustCompile(`^\d{1,12}$`)
)

// WishID: the item a pasted address (booth.pm/ja/items/123, shop.booth.pm/items/123) or number stands for.
func WishID(text string) string {
	text = strings.TrimSpace(text)
	if reWishID.MatchString(text) {
		return text
	}
	m := reWishURL.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	host := strings.ToLower(m[1])
	if host == "booth.pm" || strings.HasSuffix(host, ".booth.pm") {
		return m[2]
	}
	if b, err := url.Parse(core.BoothWebBase()); err == nil && b.Host == host { // the stand-in the tests use
		return m[2]
	}
	return ""
}

// WishSeed: what the window already shows of an item (a search result, the details panel).
type WishSeed struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Shop  string `json:"shop"`
	Thumb string `json:"thumb"`
	Price string `json:"price"` // "¥ 6,000"
}

var errWishFull = errors.New("愿望单已满，请先移除不再需要的商品")

// WishAdd puts an item on the list. One that the window already shows (a search result, the details panel) is
// listed at once with what is known of it, and Booth is asked for its price and variations in the background;
// a pasted address or number has to be asked for first, to know what it is.
func WishAdd(st *core.Store, seed WishSeed) (*WishItem, error) {
	id := WishID(seed.ID)
	if id == "" {
		return nil, errors.New("请输入 Booth 商品页链接或商品编号")
	}
	wishMu.Lock()
	wishLoad()
	if it := wishFind("booth:" + id); it != nil {
		c := *it
		wishMu.Unlock()
		return &c, nil
	}
	full := len(wishData.Items) >= wishMax
	wishMu.Unlock()
	if full {
		return nil, errWishFull
	}
	now := time.Now().Unix()
	it := &WishItem{Site: "booth", ID: id, Title: strings.TrimSpace(seed.Title), Shop: strings.TrimSpace(seed.Shop), Thumb: seed.Thumb,
		Currency: "JPY", Price: priceNumber(seed.Price), Added: now, First: -1, Low: -1}
	if it.Title == "" {
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
		it.Tried = now
		it.apply(now, fresh, false)
	} else if it.Price >= 0 {
		it.First, it.Low, it.LowAt = it.Price, it.Price, now
		it.Hist = []WishPoint{{At: now, Price: it.Price}}
	}
	wishMu.Lock()
	if old := wishFind(it.Key()); old != nil { // added twice at once
		c := *old
		wishMu.Unlock()
		return &c, nil
	}
	wishData.Items = append(wishData.Items, it)
	wishSave()
	c := *it
	wishMu.Unlock()
	core.BumpRev()
	if it.Checked == 0 {
		go wishFirstLook(st, it.Key())
	}
	return &c, nil
}

var wishLookMu sync.Mutex // first looks go one after the other, however fast items are added

// wishFirstLook asks Booth for an item that was listed with what the window showed of it.
func wishFirstLook(st *core.Store, key string) {
	wishLookMu.Lock()
	defer wishLookMu.Unlock()
	if boothPaused() || core.Quitting.Load() {
		return // the daily look fills it in
	}
	fresh, gone, err := wishFetch(core.HTTPClient(st), strings.TrimPrefix(key, "booth:"))
	now := time.Now().Unix()
	var busy busyErr
	if errors.As(err, &busy) {
		turnedAway()
		core.BumpRev()
		return
	}
	wishMu.Lock()
	wishLoad() // (the file may have been replaced meanwhile: WishReload)
	if it := wishFind(key); it != nil && it.Checked == 0 {
		it.Tried = now
		if err != nil {
			it.Err = core.FriendlyNetErr(err)
		} else {
			it.apply(now, fresh, gone)
		}
		wishSave()
	}
	wishMu.Unlock()
	core.BumpRev()
	time.Sleep(boothGap)
}

func WishRemove(key string) bool {
	wishMu.Lock()
	defer wishMu.Unlock()
	wishLoad()
	for i, it := range wishData.Items {
		if it.Key() == key {
			wishData.Items = append(wishData.Items[:i], wishData.Items[i+1:]...)
			wishSave()
			core.BumpRev()
			return true
		}
	}
	return false
}

func WishNote(key, note string) bool {
	note = strings.TrimSpace(note)
	if r := []rune(note); len(r) > wishNoteMax {
		note = string(r[:wishNoteMax])
	}
	wishMu.Lock()
	defer wishMu.Unlock()
	wishLoad()
	it := wishFind(key)
	if it == nil {
		return false
	}
	if it.Note != note {
		it.Note = note
		wishSave()
		core.BumpRev()
	}
	return true
}

// WishSeen: the player has the list open: the dot on its entry goes.
func WishSeen() {
	wishMu.Lock()
	defer wishMu.Unlock()
	wishLoad()
	n := 0
	for _, it := range wishData.Items {
		if it.Unseen {
			it.Unseen = false
			n++
		}
	}
	if n > 0 {
		wishSave()
		core.BumpRev()
	}
}

// WishTold: the window has announced these items' changes (as they were at the given times): never again.
func WishTold(at map[string]int64) {
	wishMu.Lock()
	defer wishMu.Unlock()
	wishLoad()
	n := 0
	for key, t := range at {
		if it := wishFind(key); it != nil && it.Change != nil && !it.Change.Told && it.Change.At == t {
			it.Change.Told = true
			n++
		}
	}
	if n > 0 {
		wishSave()
	}
}

func WishSetWatch(on bool) {
	wishMu.Lock()
	defer wishMu.Unlock()
	wishLoad()
	if wishData.NoWatch == on {
		wishData.NoWatch = !on
		wishSave()
		core.BumpRev()
	}
}

// ---------- what the window shows ----------

type WishVarView struct {
	Name  string          `json:"name"`
	Price string          `json:"price"`
	Off   bool            `json:"off,omitempty"`
	Hist  []WishPointView `json:"hist,omitempty"`
}

type WishChangeView struct {
	Kind string `json:"kind"`
	At   int64  `json:"at"`
	Old  string `json:"old"`
	New  string `json:"new"`
	Var  string `json:"var,omitempty"`
	Told bool   `json:"told,omitempty"`
}

type WishPointView struct {
	At    int64  `json:"at"`
	Price string `json:"price"`
	State string `json:"state,omitempty"`
}

type WishView struct {
	Key      string          `json:"key"`
	Site     string          `json:"site"`
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Shop     string          `json:"shop"`
	Thumb    string          `json:"thumb"`
	Currency string          `json:"currency"`
	Price    string          `json:"price"`          // "¥ 6,000"; "" while not known
	From     bool            `json:"from,omitempty"` // variations at different prices: this is the lowest
	PriceNum int64           `json:"priceNum"`
	First    string          `json:"first"`
	Diff     string          `json:"diff"`    // against the price when added: "¥ 500", without a sign
	DiffDir  int             `json:"diffDir"` // -1 cheaper now, 1 dearer, 0 the same or not known
	Pct      float64         `json:"pct"`     // the same in percent of the first price (negative: cheaper)
	Low      string          `json:"low"`
	LowAt    int64           `json:"lowAt,omitempty"`
	State    string          `json:"state"`
	Bought   bool            `json:"bought,omitempty"`
	Note     string          `json:"note"`
	Added    int64           `json:"added"`
	Checked  int64           `json:"checked"`
	Err      string          `json:"err,omitempty"`
	Vars     []WishVarView   `json:"vars,omitempty"`
	Hist     []WishPointView `json:"hist,omitempty"`
	Change   *WishChangeView `json:"change,omitempty"`
	Unseen   bool            `json:"unseen,omitempty"`
}

type WishState struct {
	Items  []WishView `json:"items"`
	Unseen int        `json:"unseen"`
	Watch  bool       `json:"watch"`
	Paused bool       `json:"paused"` // Booth is turning requests away: looks wait
	Busy   bool       `json:"busy"`   // a round is under way
}

func (it *WishItem) view() WishView {
	v := WishView{Key: it.Key(), Site: it.Site, ID: it.ID, Title: it.Title, Shop: it.Shop, Thumb: it.Thumb, Currency: it.Currency,
		Price: PriceText(it.Currency, it.Price), PriceNum: it.Price, First: PriceText(it.Currency, it.First), Low: PriceText(it.Currency, it.Low), LowAt: it.LowAt,
		State: it.State, Bought: it.Bought > 0, Note: it.Note, Added: it.Added, Checked: it.Checked, Err: it.Err, Unseen: it.Unseen}
	if it.Price >= 0 && it.First >= 0 && it.Price != it.First {
		d := it.Price - it.First
		v.DiffDir = 1
		if d < 0 {
			d, v.DiffDir = -d, -1
		}
		v.Diff = PriceText(it.Currency, d)
		if it.First > 0 {
			v.Pct = float64(it.Price-it.First) * 100 / float64(it.First)
		}
	}
	if len(it.Vars) > 1 {
		for _, x := range it.Vars {
			vv := WishVarView{Name: x.Name, Price: PriceText(it.Currency, x.Price), Off: x.Off}
			for _, h := range x.Hist {
				vv.Hist = append(vv.Hist, WishPointView{At: h.At, Price: PriceText(it.Currency, h.Price)})
			}
			v.Vars = append(v.Vars, vv)
			v.From = v.From || x.Price != it.Price
		}
	}
	for _, h := range it.Hist {
		v.Hist = append(v.Hist, WishPointView{At: h.At, Price: PriceText(it.Currency, h.Price), State: h.State})
	}
	if c := it.Change; c != nil {
		v.Change = &WishChangeView{Kind: c.Kind, At: c.At, Old: PriceText(it.Currency, c.Old), New: PriceText(it.Currency, c.New), Var: c.Var, Told: c.Told}
	}
	return v
}

// WishSnapshot: the list for the window. An item found among the synced purchases is marked as bought here,
// and is not looked at any more.
func WishSnapshot(st *core.Store) WishState {
	bought := map[string]bool{}
	st.Mu.RLock()
	for id := range st.Purchases {
		bought[id] = true
	}
	st.Mu.RUnlock()
	wishRunMu.Lock()
	busy := wishRunning
	wishRunMu.Unlock()
	wishMu.Lock()
	defer wishMu.Unlock()
	wishLoad()
	s := WishState{Items: make([]WishView, 0, len(wishData.Items)), Watch: !wishData.NoWatch, Paused: boothPaused(), Busy: busy}
	changed := false
	for _, it := range wishData.Items {
		if it.Site == "booth" && it.Bought == 0 && bought[it.ID] {
			it.Bought, changed = time.Now().Unix(), true
		}
		if it.Unseen {
			s.Unseen++
		}
		s.Items = append(s.Items, it.view())
	}
	if changed {
		wishSave()
	}
	return s
}

// ---------- looking at the prices ----------

var errWishShape = errors.New("无法解析 Booth 返回的商品信息")

// wishFetch asks Booth for the item: one request. gone: Booth has no such item (any more).
func wishFetch(c *http.Client, id string) (it *boothItem, gone bool, err error) {
	body, status, err := boothItemJSON(c, id)
	switch {
	case err != nil:
		return nil, false, err
	case status == 404:
		return nil, true, nil
	case status == 429 || status == 403:
		return nil, false, busyErr{status}
	case status != 200:
		return nil, false, fmt.Errorf("Booth 返回错误（HTTP %d）", status)
	}
	it, err = parseBoothItem(body)
	if err != nil {
		return nil, false, errWishShape
	}
	return it, false, nil
}

func keepLast(h []WishPoint, n int) []WishPoint {
	if len(h) > n {
		h = append(h[:0], h[len(h)-n:]...)
	}
	return h
}

// apply takes in what Booth says now and returns what is different from the last look (nil: nothing, or
// nothing to compare with). Caller holds wishMu, or owns the item.
func (it *WishItem) apply(now int64, fresh *boothItem, gone bool) *WishChange {
	wasState, wasPrice, first := it.State, it.Price, it.Checked == 0
	it.Checked, it.Err = now, ""
	if gone {
		it.State = "gone"
	} else {
		it.State = ""
		allOut := len(fresh.Vars) > 0
		for _, v := range fresh.Vars {
			allOut = allOut && v.Empty
		}
		if fresh.SoldOut || fresh.EndOfSale || allOut {
			it.State = "off"
		}
		if fresh.Name != "" {
			it.Title = fresh.Name
		}
		if fresh.Shop != "" {
			it.Shop, it.ShopURL = fresh.Shop, fresh.ShopURL
		}
		if it.Thumb == "" && len(fresh.Images) > 0 {
			it.Thumb = fresh.Images[0]
		}
	}
	// the variations, each against what it cost at the last look
	var drop, rise *WishChange
	rate := func(c *WishChange) float64 {
		if c.Old <= 0 {
			return 0
		}
		return float64(c.Old-c.New) / float64(c.Old)
	}
	if !gone {
		old := map[string]WishVar{}
		for _, v := range it.Vars {
			old[v.ID] = v
		}
		several := len(fresh.Vars) > 1
		vars := make([]WishVar, 0, len(fresh.Vars))
		for _, f := range fresh.Vars {
			v := WishVar{ID: f.ID, Name: f.Name, Price: f.Price, Off: f.Empty}
			o, known := old[f.ID]
			if known {
				v.Hist = o.Hist
			}
			if several && (!known || o.Price != f.Price || len(v.Hist) == 0) {
				v.Hist = keepLast(append(v.Hist, WishPoint{At: now, Price: f.Price}), wishVarHistKeep)
			}
			if known && !first && several && o.Price != f.Price {
				c := &WishChange{At: now, Old: o.Price, New: f.Price, Var: f.Name}
				if f.Price < o.Price && (drop == nil || rate(c) > rate(drop)) {
					drop = c
				}
				if f.Price > o.Price && (rise == nil || rate(c) < rate(rise)) {
					rise = c
				}
			}
			vars = append(vars, v)
		}
		it.Vars = vars
		if fresh.PriceNum >= 0 {
			it.Price = fresh.PriceNum
		}
		// the item's own price (the lowest variation's): also moves when a cheaper variation is added
		if !first && wasPrice >= 0 && it.Price >= 0 && it.Price != wasPrice {
			c := &WishChange{At: now, Old: wasPrice, New: it.Price}
			if several {
				for _, f := range fresh.Vars {
					if f.Price == it.Price {
						c.Var = f.Name
						break
					}
				}
			}
			if it.Price < wasPrice && drop == nil {
				drop = c
			}
			if it.Price > wasPrice && rise == nil {
				rise = c
			}
		}
		if first && it.Price >= 0 {
			// what Booth says it costs is what it cost when it was added (a search result gives "¥ 1,000~" at best)
			it.First, it.Low, it.LowAt, it.Hist = it.Price, it.Price, now, nil
		}
		if it.Price >= 0 && it.State == "" && (it.Low < 0 || it.Price < it.Low) {
			it.Low, it.LowAt = it.Price, now
		}
	}
	if n := len(it.Hist); it.Price >= 0 && (n == 0 || it.Hist[n-1].Price != it.Price || it.Hist[n-1].State != it.State) {
		it.Hist = keepLast(append(it.Hist, WishPoint{At: now, Price: it.Price, State: it.State}), wishHistKeep)
	}
	if first {
		return nil
	}
	var ch *WishChange
	switch {
	case it.State == "gone" && wasState != "gone":
		ch = &WishChange{Kind: "gone", At: now, Old: wasPrice, New: wasPrice}
	case it.State == "off" && wasState == "":
		ch = &WishChange{Kind: "off", At: now, Old: wasPrice, New: it.Price}
	case drop != nil && it.State == "":
		ch = drop
		ch.Kind = "drop"
		if ch.New == 0 {
			ch.Kind = "free"
		}
	case it.State == "" && wasState != "":
		ch = &WishChange{Kind: "back", At: now, Old: wasPrice, New: it.Price}
	case rise != nil && it.State == "":
		ch = rise
		ch.Kind = "rise"
	}
	if ch != nil {
		it.Change, it.Unseen = ch, true
	}
	return ch
}

// wishDue: the items a round looks at, the longest-waiting first. Caller holds wishMu.
func wishDue(now time.Time, byHand bool) []string {
	type due struct {
		key  string
		last int64
	}
	var list []due
	for _, it := range wishData.Items {
		if it.Site != "booth" || it.Bought > 0 {
			continue
		}
		wait := wishEvery
		switch {
		case byHand:
			wait = wishByHand
		case it.Tried > it.Checked:
			wait = wishRetry
		}
		last := max(it.Tried, it.Checked)
		if now.Sub(time.Unix(last, 0)) >= wait {
			list = append(list, due{it.Key(), last})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].last < list[j].last })
	keys := make([]string, 0, len(list))
	for _, d := range list {
		keys = append(keys, d.key)
	}
	return keys
}

// WishRound looks at the items that are due, a few at a time and one after the other. byHand: the player
// asked (every item not looked at within the hour, in one go). It returns how many were looked at.
func WishRound(st *core.Store, byHand bool) (int, error) {
	wishRunMu.Lock()
	if wishRunning {
		wishRunMu.Unlock()
		return 0, errors.New("正在检查价格，请稍候")
	}
	wishRunning = true
	wishRunMu.Unlock()
	defer func() {
		wishRunMu.Lock()
		wishRunning = false
		wishRunMu.Unlock()
		core.BumpRev()
	}()
	if boothPaused() {
		return 0, errors.New("Booth 暂时限制了访问，请稍后重试")
	}
	// bought items leave the round
	bought := map[string]bool{}
	st.Mu.RLock()
	for id := range st.Purchases {
		bought[id] = true
	}
	st.Mu.RUnlock()
	wishMu.Lock()
	wishLoad()
	if wishData.NoWatch && !byHand {
		wishMu.Unlock()
		return 0, nil
	}
	for _, it := range wishData.Items {
		if it.Site == "booth" && it.Bought == 0 && bought[it.ID] {
			it.Bought = time.Now().Unix()
		}
	}
	keys := wishDue(time.Now(), byHand)
	wishMu.Unlock()
	if !byHand && len(keys) > wishBatch {
		keys = keys[:wishBatch]
	}
	if len(keys) == 0 {
		return 0, nil
	}
	if byHand {
		core.BumpRev() // the window shows that a round is under way
	}
	c := core.HTTPClient(st)
	done, fails := 0, 0
	for i, key := range keys {
		if core.Quitting.Load() {
			break
		}
		if i > 0 {
			time.Sleep(wishGap + time.Duration(rand.Int63n(int64(wishGap)/2+1)))
			if boothPaused() || core.Quitting.Load() {
				break
			}
		}
		id := strings.TrimPrefix(key, "booth:")
		fresh, gone, err := wishFetch(c, id)
		now := time.Now().Unix()
		var busy busyErr
		if errors.As(err, &busy) {
			// refused again as the first one asked for after the rest, and not for asking too often: it is this
			// item Booth does not show. It gets the error like any other, and the round goes on.
			own := busy.status == 403 && i == 0 && key == wishBrokeAt
			if !own {
				wishBrokeAt = key
				turnedAway()
				core.Logf("Booth 限制了访问（HTTP %d）：愿望单价格检查已暂停，稍后继续", busy.status)
				break // not written down as this item's look: it stays due, like the ones after it
			}
		}
		wishMu.Lock()
		wishLoad() // the file may have been replaced since the round began (a library import): never the old list over it
		it := wishFind(key)
		if it == nil { // removed meanwhile
			wishMu.Unlock()
			continue
		}
		it.Tried = now
		if err != nil {
			it.Err = core.FriendlyNetErr(err)
			fails++
		} else {
			fails = 0
			if ch := it.apply(now, fresh, gone); ch != nil {
				core.Logf("愿望单：Booth #%s %s（%d → %d）", it.ID, ch.Kind, ch.Old, ch.New)
			}
			done++
		}
		wishSave()
		wishMu.Unlock()
		if fails >= 2 { // the network is not there: the rest waits for the next round
			break
		}
	}
	if done > 0 {
		wishBrokeAt = ""
		answeredAgain()
	}
	return done, nil
}

// WishStartRound: 「立即检查」. It returns how many items are about to be looked at; the looking goes on in the
// background.
func WishStartRound(st *core.Store) (int, error) {
	wishRunMu.Lock()
	running := wishRunning
	wishRunMu.Unlock()
	if running {
		return 0, errors.New("正在检查价格，请稍候")
	}
	if boothPaused() {
		return 0, errors.New("Booth 暂时限制了访问，请稍后重试")
	}
	wishMu.Lock()
	wishLoad()
	n := len(wishDue(time.Now(), true))
	wishMu.Unlock()
	if n > 0 {
		go func() { _, _ = WishRound(st, true) }()
	}
	return n, nil
}

// WishLoop runs for as long as the program does.
func WishLoop(st *core.Store) {
	time.Sleep(wishFirst)
	for {
		// one thing asking Booth at a time: not while the library's own Booth step or the followed shops' look is
		followRunMu.Lock()
		followBusy := followRunning
		followRunMu.Unlock()
		if !core.Quitting.Load() && !core.TaskBooth.Snapshot().Running && !followBusy {
			_, _ = WishRound(st, false)
		}
		time.Sleep(wishTick)
	}
}

// WishReload: wishlist.json was replaced from outside (a library import or its undo); read it again at the next use.
func WishReload() {
	wishMu.Lock()
	wishFrom = ""
	wishMu.Unlock()
}
