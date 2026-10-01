package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func httpClient(st *Store) *http.Client {
	st.mu.RLock()
	px := strings.TrimSpace(st.Settings.Proxy)
	st.mu.RUnlock()
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 20 * time.Second}
	if px == "" {
		px = systemProxy()
	}
	if px != "" && !strings.EqualFold(px, "direct") {
		if !strings.Contains(px, "://") {
			px = "http://" + px
		}
		if u, err := url.Parse(px); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: tr, Timeout: 40 * time.Second}
}

const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"

// The live item JSON changes shape from time to time (tags became objects in 2025), so it is read
// loosely: a field of an unexpected type is skipped instead of failing the whole item.
type boothItem struct {
	Name, Desc, Price, URL, Category string
	Tags, Images                     []string
	Image                            string
	Shop, ShopURL                    string
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

const boothInfoVer = 3

func fetchBooth(c *http.Client, id string) (*BoothInfo, error) {
	bi := &BoothInfo{ID: id, URL: "https://booth.pm/ja/items/" + id, Fetched: time.Now().Unix(), Ver: boothInfoVer}
	req, _ := http.NewRequest("GET", boothWebBase()+"/ja/items/"+id+".json", nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
	resp, err := c.Do(req)
	if err != nil {
		return bi, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	if resp.StatusCode == 404 {
		bi.Gone = true
		return bi, fmt.Errorf("Booth 上找不到这个商品（可能已下架）")
	}
	if resp.StatusCode != 200 {
		return bi, fmt.Errorf("Booth 返回 %d", resp.StatusCode)
	}
	it, jerr := parseBoothItem(body)
	// the item page carries the long description blocks (and is the fallback when the JSON is unreadable)
	var page []byte
	req2, _ := http.NewRequest("GET", boothWebBase()+"/ja/items/"+id, nil)
	req2.Header.Set("User-Agent", ua)
	req2.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
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
		bi.Desc, bi.Tags, bi.Images, bi.ImageURL = it.Desc, it.Tags, it.Images, it.Image
	}
	if more := boothSections(page); more != "" {
		if bi.Desc != "" {
			bi.Desc += "\n\n"
		}
		bi.Desc += more
	}
	bi.Desc = clipRunes(bi.Desc, 8000)
	if bi.ImageURL != "" {
		if p, err := downloadCover(c, id, bi.ImageURL); err == nil {
			bi.Cover = p
		}
	}
	return bi, nil
}

func htmlUnescape(s string) string { return html.UnescapeString(s) }

func downloadCover(c *http.Client, id, u string) (string, error) {
	return downloadTo(c, u, "booth_"+id)
}

// downloadTo saves an image from Booth's CDN into covers/<name>.<ext>.
func downloadTo(c *http.Client, u, name string) (string, error) {
	_ = os.MkdirAll(coversDir(), 0755)
	ext := strings.ToLower(filepath.Ext(strings.Split(u, "?")[0]))
	if ext == "" || len(ext) > 5 {
		ext = ".jpg"
	}
	dst := filepath.Join(coversDir(), name+ext)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", ua)
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

// RunBoothFetch fetches Booth info for assets with an item id. force=true refetches everything.
func RunBoothFetch(st *Store, prog *Task, force bool, only []string) {
	st.mu.RLock()
	var ids []string
	seen := map[string]bool{}
	want := map[string]bool{}
	for _, k := range only {
		want[k] = true
	}
	consider := func(key, id string) {
		if id == "" || seen[id] {
			return
		}
		if len(want) > 0 && !want[key] {
			return
		}
		seen[id] = true
		if needsBooth(st.Booth[id], force || len(want) > 0) {
			ids = append(ids, id)
		}
	}
	for _, a := range st.Assets {
		id, _ := assetBooth(st, a.Key, a)
		consider(a.Key, id)
	}
	for _, key := range sortedKeys(st.User) {
		if strings.HasPrefix(key, "pan:") || strings.HasPrefix(key, "purchase:") {
			id, _ := assetBooth(st, key, nil)
			consider(key, id)
		}
	}
	// purchases that are not on disk yet: newest first
	if len(want) == 0 {
		var ps []*Purchase
		for id, p := range st.Purchases {
			if !seen[id] {
				ps = append(ps, p)
			}
		}
		sort.Slice(ps, func(i, j int) bool { return ps[i].when() > ps[j].when() })
		for _, p := range ps {
			seen[p.ID] = true
			if needsBooth(st.Booth[p.ID], force) {
				ids = append(ids, p.ID)
			}
		}
	}
	st.mu.RUnlock()
	if len(ids) == 0 {
		prog.Set(1, 1, "没有需要抓取的 Booth 商品")
		return
	}
	c := httpClient(st)
	fails := 0
	for i, id := range ids {
		prog.Set(i, len(ids), "Booth #"+id)
		bi, err := fetchBooth(c, id)
		if err != nil {
			bi.Err = friendlyNetErr(err)
			if !bi.Gone {
				fails++ // a delisted item is not a network problem
			}
		}
		st.mu.Lock()
		if old := st.Booth[id]; old != nil && err != nil && old.Name != "" {
			old.Err = bi.Err
			old.Fetched = bi.Fetched
			old.Gone = bi.Gone
		} else {
			st.Booth[id] = bi
		}
		st.mu.Unlock()
		if i%12 == 11 {
			bumpRev() // let the window pick up new covers while a long fetch is running
		}
		// give up early when the network is clearly unreachable
		if fails >= 4 && fails == i+1 {
			prog.Set(len(ids), len(ids), "连不上 Booth，请在设置里填代理")
			return
		}
		time.Sleep(700 * time.Millisecond)
	}
	prog.Set(len(ids), len(ids), "完成")
}

func needsBooth(b *BoothInfo, force bool) bool {
	switch {
	case force || b == nil:
		return true
	case b.Gone:
		return false
	case b.Ver < boothInfoVer && b.Err == "":
		return true // fetched by an older version: description / pictures missing
	case b.Err != "" && time.Now().Unix()-b.Fetched > 3600:
		return true
	}
	return false
}

func friendlyNetErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		return "连接 Booth 超时（可能需要代理）"
	case strings.Contains(s, "refused") || strings.Contains(s, "no such host") || strings.Contains(s, "reset"):
		return "连不上 Booth（可能需要代理）"
	}
	return s
}
