package booth

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
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
	"vrclib/internal/netdisk"
)

// The live item JSON changes shape from time to time (tags became objects in 2025), so it is read
// loosely: a field of an unexpected type is skipped instead of failing the whole item.
type boothItem struct {
	Name, Desc, Price, URL, Category string
	Tags, Images                     []string
	Image                            string
	Shop, ShopURL                    string
	Adult                            bool
	// what it costs, for the wish list: the lowest variation's price in yen (-1: not told), every variation,
	// and whether the shop still sells it
	PriceNum           int64
	Vars               []boothVar
	SoldOut, EndOfSale bool
}

// boothVar: one variation of an item ("フルセット", a size …). An item with one price has one, without a name.
type boothVar struct {
	ID    string
	Name  string
	Price int64
	Empty bool // out of stock
}

func jStr(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func jObj(m map[string]any, k string) map[string]any {
	o, _ := m[k].(map[string]any)
	if o == nil {
		o = map[string]any{}
	}
	return o
}

func parseBoothItem(body []byte) (*boothItem, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	it := &boothItem{Name: strings.TrimSpace(jStr(m, "name")), Desc: strings.TrimSpace(jStr(m, "description")),
		Price: jStr(m, "price"), URL: jStr(m, "url")}
	if it.Name == "" {
		return nil, fmt.Errorf("no name")
	}
	it.Adult, _ = m["is_adult"].(bool)
	it.SoldOut, _ = m["is_sold_out"].(bool)
	it.EndOfSale, _ = m["is_end_of_sale"].(bool)
	it.PriceNum = priceNumber(it.Price)
	vars, _ := m["variations"].([]any)
	for _, x := range vars {
		o, _ := x.(map[string]any)
		n, ok := o["price"].(float64)
		if !ok || n < 0 {
			continue // an older shape, or not a variation with a price
		}
		v := boothVar{ID: jStr(o, "id"), Name: strings.TrimSpace(jStr(o, "name")), Price: int64(n)}
		v.Empty, _ = o["is_empty_stock"].(bool)
		it.Vars = append(it.Vars, v)
		if len(it.Vars) == 1 || v.Price < it.PriceNum {
			it.PriceNum = v.Price
		}
	}
	cat := jObj(m, "category")
	it.Category = jStr(cat, "name")
	shop := jObj(m, "shop")
	it.Shop, it.ShopURL = jStr(shop, "name"), jStr(shop, "url")
	if it.ShopURL == "" && jStr(shop, "subdomain") != "" {
		it.ShopURL = "https://" + jStr(shop, "subdomain") + ".booth.pm/"
	}
	tags, _ := m["tags"].([]any)
	for _, t := range tags {
		switch v := t.(type) {
		case string:
			it.Tags = append(it.Tags, v)
		case map[string]any:
			if n := jStr(v, "name"); n != "" {
				it.Tags = append(it.Tags, n)
			}
		}
	}
	imgs, _ := m["images"].([]any)
	for _, x := range imgs {
		o, _ := x.(map[string]any)
		if o == nil {
			if s, ok := x.(string); ok && s != "" {
				it.Images = append(it.Images, s)
			}
			continue
		}
		orig, small := jStr(o, "original"), jStr(o, "resized")
		if it.Image == "" {
			it.Image = orig
			if it.Image == "" {
				it.Image = small
			}
		}
		if small == "" {
			small = orig
		}
		small = boothThumb300(small)
		if small != "" && len(it.Images) < 8 {
			it.Images = append(it.Images, small)
		}
	}
	return it, nil
}

// priceNumber: "¥ 6,000" or "¥ 1,060~" → 6000, 1060; -1 when there is no number in it.
func priceNumber(s string) int64 {
	n, any := int64(0), false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			if n > 1e12 {
				return -1
			}
			n, any = n*10+int64(r-'0'), true
		case r == ',' || r == ' ' || r == '¥' || r == '\u00a0' || r == '￥':
		default:
			if any {
				return n // "~", "円" … after the number
			}
		}
	}
	if !any {
		return -1
	}
	return n
}

var (
	reOgImage   = regexp.MustCompile(`<meta[^>]+property="og:image"[^>]+content="([^"]+)"`)
	reOgTitle   = regexp.MustCompile(`<meta[^>]+property="og:title"[^>]+content="([^"]+)"`)
	reShopSect  = regexp.MustCompile(`(?s)<section class="shop__text">\s*<h2[^>]*>(.*?)</h2>(.*?)</section>`)
	reHTMLBreak = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</li>|</h[1-6]>`)
	reHTMLTag   = regexp.MustCompile(`(?s)<[^>]+>`)
	reBlankRuns = regexp.MustCompile(`\n{3,}`)
)

func htmlText(s string) string {
	s = reHTMLBreak.ReplaceAllString(s, "\n")
	s = html.UnescapeString(reHTMLTag.ReplaceAllString(s, ""))
	return strings.TrimSpace(reBlankRuns.ReplaceAllString(s, "\n\n"))
}

// boothSections reads the titled description blocks of an item page (商品説明, 導入方法, 利用規約...),
// which the JSON does not include.
func boothSections(page []byte) string {
	var sb strings.Builder
	for _, m := range reShopSect.FindAllSubmatch(page, 30) {
		title, body := htmlText(string(m[1])), htmlText(string(m[2]))
		if body == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		if title != "" {
			sb.WriteString("【" + title + "】\n")
		}
		sb.WriteString(body)
	}
	return sb.String()
}

var reBoothCrop = regexp.MustCompile(`^(https://booth\.pximg\.net)/c/[0-9a-z_]+/`)

// boothThumb300 swaps Booth's 72px "resized" crop for the 300px one used on item cards.
func boothThumb300(u string) string {
	return reBoothCrop.ReplaceAllString(u, "$1/c/300x300_a2_g5/")
}

// trimOgTitle turns "Item - Shop - BOOTH" into "Item".
func trimOgTitle(s string) string {
	if t, ok := strings.CutSuffix(s, " - BOOTH"); ok {
		if i := strings.LastIndex(t, " - "); i > 0 {
			t = t[:i]
		}
		return strings.TrimSpace(t)
	}
	return s
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "\n…"
	}
	return s
}

const BoothInfoVer = 4 // 2: description, tags, pictures; 3: full description; 4: adult flag

func fetchBooth(c *http.Client, id string) (*core.BoothInfo, error) {
	return FetchBoothInfo(c, id, true)
}

// FetchBoothInfo reads an item; withCover also saves its picture as a cover.
func FetchBoothInfo(c *http.Client, id string, withCover bool) (*core.BoothInfo, error) {
	bi := &core.BoothInfo{ID: id, URL: "https://booth.pm/ja/items/" + id, Fetched: time.Now().Unix(), Ver: BoothInfoVer}
	body, status, err := boothItemJSON(c, id)
	if err != nil {
		return bi, err
	}
	if status == 404 {
		bi.Gone = true
		return bi, fmt.Errorf("Booth 上未找到该商品（可能已下架）")
	}
	if status == 429 || status == 403 {
		return bi, busyErr{status}
	}
	if status != 200 {
		return bi, fmt.Errorf("Booth 返回错误（HTTP %d）", status)
	}
	it, jerr := parseBoothItem(body)
	// the item page carries the long description blocks (and is the fallback when the JSON is unreadable)
	var page []byte
	req2, _ := http.NewRequest("GET", core.BoothWebBase()+"/ja/items/"+id, nil)
	req2.Header.Set("User-Agent", core.UA)
	req2.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
	req2.Header.Set("Cookie", "adult=t")
	if r2, err2 := c.Do(req2); err2 == nil {
		if r2.StatusCode == 200 {
			page, _ = io.ReadAll(io.LimitReader(r2.Body, 4<<20))
		}
		r2.Body.Close()
	}
	if jerr != nil {
		if m := reOgTitle.FindSubmatch(page); m != nil {
			bi.Name = trimOgTitle(html.UnescapeString(string(m[1])))
		}
		if m := reOgImage.FindSubmatch(page); m != nil {
			bi.ImageURL = html.UnescapeString(string(m[1]))
		}
		if bi.Name == "" {
			return bi, fmt.Errorf("无法解析 Booth 页面")
		}
	} else {
		bi.Name, bi.Price, bi.Shop, bi.ShopURL, bi.Category = it.Name, it.Price, it.Shop, it.ShopURL, it.Category
		if it.URL != "" {
			bi.URL = it.URL
		}
		bi.Desc, bi.Tags, bi.Images, bi.ImageURL, bi.Adult = it.Desc, it.Tags, it.Images, it.Image, it.Adult
	}
	if more := boothSections(page); more != "" {
		if bi.Desc != "" {
			bi.Desc += "\n\n"
		}
		bi.Desc += more
	}
	bi.Desc = clipRunes(bi.Desc, 8000)
	if bi.ImageURL != "" && withCover {
		if p, err := downloadCover(c, id, bi.ImageURL); err == nil {
			bi.Cover = p
		}
	}
	return bi, nil
}

// boothItemJSON asks Booth for an item's JSON: the one request an item's name, price and variations take.
func boothItemJSON(c *http.Client, id string) ([]byte, int, error) {
	req, _ := http.NewRequest("GET", core.BoothWebBase()+"/ja/items/"+id+".json", nil)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
	req.Header.Set("Cookie", "adult=t")
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	return body, resp.StatusCode, nil
}

func htmlUnescape(s string) string { return html.UnescapeString(s) }

var (
	reCompatLine = regexp.MustCompile(`(?i)対応|对应|對應|適用|适用|supported|compatible|アバター|avatar|素体|for\s`)
	reNotCompat  = regexp.MustCompile(`(?i)非対応|未対応|不対応|不支持|未对应|not\s+(supported|compatible)|unsupported`)
)

// BoothBaseText: where a Booth product names the base bodies it is made for — its title, its tags, and the
// part of its description that lists them ("対応アバター" and the lines under it). The rest of the description
// is left out: it mentions other products too.
func BoothBaseText(b *core.BoothInfo) string {
	if b == nil {
		return ""
	}
	var out strings.Builder
	out.WriteString(b.Name)
	for _, t := range b.Tags {
		out.WriteString(" | " + t)
	}
	lines := strings.Split(strings.ReplaceAll(b.Desc, "\r", ""), "\n")
	follow := 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			follow = 0
		case reNotCompat.MatchString(t):
			follow = 0
		case reCompatLine.MatchString(t) && len([]rune(t)) <= 200:
			out.WriteString(" | " + t)
			follow = 20 // the list under the heading
		case follow > 0 && len([]rune(t)) <= 60:
			out.WriteString(" | " + t)
			follow--
		default:
			follow = 0
		}
	}
	return out.String()
}

func downloadCover(c *http.Client, id, u string) (string, error) {
	return DownloadTo(c, u, "booth_"+id)
}

// DownloadTo saves an image from Booth's CDN into covers/<name>.<ext>.
func DownloadTo(c *http.Client, u, name string) (string, error) {
	_ = os.MkdirAll(core.CoversDir(), 0755)
	ext := strings.ToLower(filepath.Ext(strings.Split(u, "?")[0]))
	if ext == "" || len(ext) > 5 {
		ext = ".jpg"
	}
	dst := filepath.Join(core.CoversDir(), name+ext)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Referer", "https://booth.pm/")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("image %d", resp.StatusCode)
	}
	f, err := os.Create(dst + ".part")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 30<<20))
	f.Close()
	if err != nil {
		return "", err
	}
	return dst, os.Rename(dst+".part", dst)
}

// busyErr: Booth turns requests away — too many of them (429), or its protection stepped in (403).
type busyErr struct{ status int }

func (e busyErr) Error() string {
	return fmt.Sprintf("Booth 暂时限制了访问（HTTP %d），请稍后重试", e.status)
}

// A batch that Booth turned away stops there: the items not asked for yet stay due, and nothing is fetched by
// itself until the pause is over (twice as long every time it happens again). Tests shorten the times.
var (
	boothMu         sync.Mutex
	boothPauseFirst = 15 * time.Minute
	boothPauseMax   = 6 * time.Hour
	boothGap        = 700 * time.Millisecond // between two items
	boothPauseStep  time.Duration
	boothPausedTill time.Time
	boothBrokenOff  bool
	boothBrokeAt    string // the item the last batch stopped at
)

func boothPaused() bool {
	boothMu.Lock()
	defer boothMu.Unlock()
	return time.Now().Before(boothPausedTill)
}

// turnedAway: Booth refused a request made outside a batch (a wish list price check). The same rest as for a
// batch, so that nothing asks again before it is over.
func turnedAway() {
	boothMu.Lock()
	boothPauseStep = min(max(boothPauseStep*2, boothPauseFirst), boothPauseMax)
	boothPausedTill = time.Now().Add(boothPauseStep)
	boothMu.Unlock()
}

// answeredAgain: Booth answers again and no batch is waiting to go on: the next rest starts short again.
func answeredAgain() {
	boothMu.Lock()
	if !boothBrokenOff {
		boothPauseStep = 0
	}
	boothMu.Unlock()
}

// ResumeDue: a batch was broken off, and Booth has had its rest: the remaining items can be asked for.
func ResumeDue() bool {
	boothMu.Lock()
	defer boothMu.Unlock()
	return boothBrokenOff && !time.Now().Before(boothPausedTill)
}

// RunBoothFetch fetches Booth info for assets with an item id. force=true refetches everything.
func RunBoothFetch(st *core.Store, prog *core.Task, force bool, only []string) {
	if !force && len(only) == 0 && boothPaused() { // what the player asks for by hand is still tried
		prog.Set(1, 1, "Booth 暂时限制了访问，稍后将自动继续获取")
		return
	}
	st.Mu.RLock()
	weekly := !st.Settings.NoSync
	var ids []string
	seen := map[string]bool{}
	want := map[string]bool{}
	for _, k := range only {
		want[k] = true
	}
	consider := func(key, id string) {
		if id == "" || seen[id] || core.IsGumID(id) { // a Gumroad purchase: nothing to ask Booth
			return
		}
		if len(want) > 0 && !want[key] {
			return
		}
		seen[id] = true
		if needsBooth(st.Booth[id], force || len(want) > 0, weekly) {
			ids = append(ids, id)
		}
	}
	for _, a := range st.Assets {
		id, _ := AssetBooth(st, a.Key, a)
		consider(a.Key, id)
	}
	for _, key := range netdisk.PanCardKeys(st) {
		id, _ := AssetBooth(st, key, nil)
		consider(key, id)
	}
	for _, key := range core.SortedKeys(st.User) {
		if strings.HasPrefix(key, "purchase:") {
			id, _ := AssetBooth(st, key, nil)
			consider(key, id)
		}
	}
	// purchases that are not on disk yet: newest first
	if len(want) == 0 {
		var ps []*core.Purchase
		for id, p := range st.Purchases {
			if !seen[id] && !core.IsGumID(id) {
				ps = append(ps, p)
			}
		}
		sort.Slice(ps, func(i, j int) bool { return ps[i].When() > ps[j].When() })
		for _, p := range ps {
			seen[p.ID] = true
			if needsBooth(st.Booth[p.ID], force, weekly) {
				ids = append(ids, p.ID)
			}
		}
	}
	st.Mu.RUnlock()
	if len(ids) == 0 {
		prog.Set(1, 1, "暂无需要获取的 Booth 商品信息")
		return
	}
	c := core.HTTPClient(st)
	fails := 0
	for i, id := range ids {
		if core.Quitting.Load() {
			return // what is left is asked for at the next start
		}
		prog.Set(i, len(ids), "Booth #"+id)
		bi, err := fetchBooth(c, id)
		var busy busyErr
		if errors.As(err, &busy) {
			boothMu.Lock()
			// refused again as the first one asked for after the rest, and not for asking too often: it is this
			// item Booth does not show — it gets the error like any other, and the batch goes on
			own := busy.status == 403 && i == 0 && id == boothBrokeAt
			if !own {
				boothPauseStep = min(max(boothPauseStep*2, boothPauseFirst), boothPauseMax)
				boothPausedTill, boothBrokenOff, boothBrokeAt = time.Now().Add(boothPauseStep), true, id
			}
			boothMu.Unlock()
			if !own {
				// not written down as this item's error: it stays due, like the ones after it
				core.Logf("Booth 限制了访问（HTTP %d）：本次还有 %d 件商品未获取，稍后继续", busy.status, len(ids)-i)
				prog.Set(i, len(ids), fmt.Sprintf("Booth 暂时限制了访问（HTTP %d），已暂停获取，稍后将自动继续", busy.status))
				core.BumpRev()
				return
			}
		}
		if err != nil {
			bi.Err = core.FriendlyNetErr(err)
			if !bi.Gone {
				fails++ // a delisted item is not a network problem
			}
		}
		st.Mu.Lock()
		if old := st.Booth[id]; old != nil && err != nil && old.Name != "" {
			old.Err = bi.Err
			old.Fetched = bi.Fetched
			old.Gone = bi.Gone
		} else {
			if err == nil {
				boothChanges(st.Booth[id], bi)
			}
			st.Booth[id] = bi
		}
		st.Mu.Unlock()
		if i%12 == 11 {
			core.BumpRev() // let the window pick up new covers while a long fetch is running
		}
		// give up early when the network is clearly unreachable
		if fails >= 4 && fails == i+1 {
			prog.Set(len(ids), len(ids), "无法连接 Booth，请在设置中配置代理")
			return
		}
		time.Sleep(boothGap)
	}
	boothMu.Lock()
	boothPauseStep, boothBrokenOff, boothBrokeAt = 0, false, ""
	boothMu.Unlock()
	prog.Set(len(ids), len(ids), "完成")
}

func needsBooth(b *core.BoothInfo, force, weekly bool) bool {
	switch {
	case force || b == nil:
		return true
	case b.Gone:
		return false
	case weekly && b.Err == "" && time.Now().Unix()-b.Fetched > 7*86400:
		return true // look again for changes made by the shop
	case b.Ver < BoothInfoVer && b.Err == "":
		return true // fetched by an older version: description / pictures missing
	case b.Err != "" && time.Now().Unix()-b.Fetched > 3600:
		return true
	}
	return false
}
