package main

// Looking for new things on Booth from inside the program. The player picks tags (what kind of
// item, which base bodies, which style) instead of typing Japanese search words; each tag becomes a
// Booth category or search word. Results say which items were bought already and which are in the
// library.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// shopCat: a kind of item, as a Booth category under 3Dモデル plus an optional search word.
type shopCat struct {
	Label string `json:"label"`
	path  string
	word  string
}

var shopCats = []shopCat{
	{"素体", "3Dキャラクター", ""},
	{"衣服", "3D衣装", ""},
	{"头发", "3Dモデル", "髪型"},
	{"配饰", "3D装飾品", ""},
	{"鞋子", "3Dモデル", "靴"},
	{"皮肤", "3Dテクスチャ", "肌"},
	{"妆容", "3Dテクスチャ", "メイク"},
	{"手势", "3Dモーション・アニメーション", "ジェスチャー"},
	{"动作", "3Dモーション・アニメーション", ""},
	{"道具", "3D小道具", ""},
	{"插件", "3Dツール・システム", ""},
	{"面捕", "3Dモデル", "フェイストラッキング"},
	{"世界", "3D環境・ワールド", ""},
}

var shopSorts = map[string]string{"popular": "wish_lists", "new": "new", "cheap": "price_asc", "pricey": "price_desc"}

type ShopQuery struct {
	Cats   []string `json:"cats"`
	Bases  []string `json:"bases"`
	Styles []string `json:"styles"`
	Q      string   `json:"q"`
	Sort   string   `json:"sort"`
	Page   int      `json:"page"`
	Adult  bool     `json:"adult"`
}

type ShopItem struct {
	BoothHit
	Bought   bool   `json:"bought,omitempty"`
	Owned    bool   `json:"owned,omitempty"`
	OwnedKey string `json:"ownedKey,omitempty"` // the library card to open
}

// searchWord: the word Booth knows a base body or style by — the first Japanese alias
// ("プラム" for Plum), else the name itself.
func searchWord(name string, table []string) string {
	for _, line := range table {
		n, aliases, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(n), name) {
			continue
		}
		var first string
		for _, a := range strings.Split(aliases, "|") {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			if first == "" {
				first = a
			}
			if !isASCII(a) {
				return a
			}
		}
		if first != "" {
			return first
		}
	}
	return name
}

// shopURLs: one Booth address per picked category (categories add up; base bodies, styles and
// the typed words all have to match).
func shopURLs(q ShopQuery, bases, styles []string) []string {
	var words []string
	adult := q.Adult
	for _, b := range q.Bases {
		words = append(words, searchWord(b, bases))
	}
	for _, s := range q.Styles {
		if s == adultStyle {
			adult = true // "H": adult items, not a word
			continue
		}
		words = append(words, searchWord(s, styles))
	}
	if t := strings.TrimSpace(q.Q); t != "" {
		words = append(words, t)
	}
	cats := q.Cats
	if len(cats) == 0 {
		cats = []string{""}
	}
	var out []string
	for _, label := range cats {
		path, extra := "3Dモデル", ""
		for _, c := range shopCats {
			if c.Label == label {
				path, extra = c.path, c.word
			}
		}
		ws := append([]string{}, words...)
		if extra != "" {
			ws = append(ws, extra)
		}
		v := url.Values{}
		if len(ws) > 0 {
			v.Set("q", strings.Join(ws, " "))
		}
		if s := shopSorts[q.Sort]; s != "" {
			v.Set("sort", s)
		} else {
			v.Set("sort", "wish_lists")
		}
		if q.Page > 1 {
			v.Set("page", fmt.Sprint(q.Page))
		}
		if adult {
			v.Set("adult", "include")
		}
		out = append(out, boothWebBase()+"/ja/browse/"+url.PathEscape(path)+"?"+v.Encode())
	}
	return out
}

var shopCache sync.Map // url → cachedPage

type cachedPage struct {
	at   time.Time
	hits []BoothHit
}

func fetchShopPage(c *http.Client, u string) ([]BoothHit, error) {
	if v, ok := shopCache.Load(u); ok {
		if cp := v.(cachedPage); time.Since(cp.at) < 10*time.Minute {
			return cp.hits, nil
		}
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
	req.Header.Set("Cookie", "adult=t") // the age check is answered; adult items still only come with adult=include
	resp, err := c.Do(req)
	if err != nil {
		return nil, errors.New(friendlyNetErr(err))
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Booth 返回 %d", resp.StatusCode)
	}
	hits := parseBoothSearch(string(b))
	shopCache.Store(u, cachedPage{time.Now(), hits})
	return hits, nil
}

// SearchShop runs the query (one page) and marks what the player has.
func SearchShop(st *Store, q ShopQuery) ([]ShopItem, bool, error) {
	st.mu.RLock()
	bases, styles := st.Settings.Bases, st.Settings.Styles
	st.mu.RUnlock()
	urls := shopURLs(q, bases, styles)
	c := httpClient(st)
	results := make([][]BoothHit, len(urls))
	errs := make([]error, len(urls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], errs[i] = fetchShopPage(c, u)
		}(i, u)
	}
	wg.Wait()
	// categories are interleaved so every kind shows up near the top
	more := false
	var hits []BoothHit
	seen := map[string]bool{}
	for k := 0; ; k++ {
		any := false
		for _, r := range results {
			if k < len(r) {
				any = true
				if !seen[r[k].ID] {
					seen[r[k].ID] = true
					hits = append(hits, r[k])
				}
			}
		}
		if !any {
			break
		}
	}
	var firstErr error
	for i, r := range results {
		if len(r) >= 48 { // a full page: there is a next one
			more = true
		}
		if errs[i] != nil && firstErr == nil {
			firstErr = errs[i]
		}
	}
	if len(hits) == 0 && firstErr != nil {
		return nil, false, firstErr
	}
	owned := ownedBoothIDs(st)
	st.mu.RLock()
	out := make([]ShopItem, 0, len(hits))
	for _, h := range hits {
		it := ShopItem{BoothHit: h}
		it.Bought = st.Purchases[h.ID] != nil
		if k, ok := owned[h.ID]; ok {
			it.Owned, it.OwnedKey = true, k
		}
		out = append(out, it)
	}
	st.mu.RUnlock()
	return out, more, nil
}

// ownedBoothIDs: Booth items that are in the library (on disk or in a netdisk share) → a card key.
func ownedBoothIDs(st *Store) map[string]string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := map[string]string{}
	for _, v := range allViews(st) {
		if v.BoothID == "" || v.Virtual || v.SplitInto > 0 {
			continue
		}
		if _, ok := out[v.BoothID]; !ok {
			out[v.BoothID] = v.Key
		}
	}
	return out
}

// ---------- one item, opened inside the program ----------

var shopItems sync.Map // id → *BoothInfo (fetched while browsing; not kept)

type ShopDetail struct {
	*BoothInfo
	Bought    bool       `json:"bought,omitempty"`
	Owned     bool       `json:"owned,omitempty"`
	OwnedKey  string     `json:"ownedKey,omitempty"`
	Files     []ShopFile `json:"files,omitempty"`
	BoughtKey string     `json:"boughtKey,omitempty"` // the purchase card when it is not downloaded yet
}

type ShopFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Got  string `json:"got,omitempty"` // where it was downloaded
}

func ShopItemDetail(st *Store, id string) (*ShopDetail, error) {
	var bi *BoothInfo
	st.mu.RLock()
	if b := st.Booth[id]; b != nil && b.Name != "" && b.Ver == boothInfoVer {
		c := *b
		bi = &c
	}
	st.mu.RUnlock()
	if bi == nil {
		if v, ok := shopItems.Load(id); ok {
			bi = v.(*BoothInfo)
		}
	}
	if bi == nil {
		b, err := fetchBoothInfo(httpClient(st), id, false)
		if err != nil {
			return nil, err
		}
		shopItems.Store(id, b)
		bi = b
	}
	d := &ShopDetail{BoothInfo: bi}
	if k, ok := ownedBoothIDs(st)[id]; ok {
		d.Owned, d.OwnedKey = true, k
	}
	st.mu.RLock()
	if p := st.Purchases[id]; p != nil {
		d.Bought, d.BoughtKey = true, "purchase:"+id
		for i, dl := range p.Downloads {
			f := ShopFile{ID: dl}
			if i < len(p.Files) {
				f.Name = p.Files[i]
			}
			if r := st.Downloaded[dl]; r != nil {
				f.Got = r.Path
			}
			d.Files = append(d.Files, f)
		}
	}
	st.mu.RUnlock()
	return d, nil
}
