package booth

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/netdisk"
	"vrclib/internal/translate"
)

var (
	reCardSplit   = regexp.MustCompile(`<li class="item-card`)
	reCardID      = regexp.MustCompile(`data-product-id="(\d+)"`)
	reCardBrand   = regexp.MustCompile(`data-product-brand="([^"]*)"`)
	reCardPrice   = regexp.MustCompile(`data-product-price="(\d*)"`)
	reCardTitle   = regexp.MustCompile(`item-card__title-anchor[^>]*>([^<]*)</a>`)
	reCardThumb   = regexp.MustCompile(`data-original="([^"]+)"`)
	reCardCat     = regexp.MustCompile(`item-card__category-anchor[^>]*>([^<]*)</a>`)
	reCardShop    = regexp.MustCompile(`<img alt="([^"]*)" class="user-avatar`)
	reCardNameAlt = regexp.MustCompile(`data-product-name="([^"]*)"`)
)

func ParseBoothSearch(page string) []core.BoothHit {
	var out []core.BoothHit
	idx := reCardSplit.FindAllStringIndex(page, -1)
	for i, m := range idx {
		end := len(page)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		card := page[m[0]:end]
		id := sub(reCardID, card)
		if id == "" {
			continue
		}
		h := core.BoothHit{ID: id, ShopSub: sub(reCardBrand, card), Thumb: sub(reCardThumb, card),
			Category: html.UnescapeString(sub(reCardCat, card)), Shop: html.UnescapeString(sub(reCardShop, card))}
		h.Name = html.UnescapeString(strings.TrimSpace(sub(reCardTitle, card)))
		if h.Name == "" {
			h.Name = html.UnescapeString(sub(reCardNameAlt, card))
		}
		if p := sub(reCardPrice, card); p != "" {
			h.Price = "¥ " + p
		}
		out = append(out, h)
	}
	return out
}

func sub(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// SearchBooth looks among 3D models first (VRChat things live there), then all of Booth.
func SearchBooth(c *http.Client, q string) ([]core.BoothHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, errors.New("没有可以搜索的关键词")
	}
	urls := []string{
		core.BoothWebBase() + "/ja/browse/3D%E3%83%A2%E3%83%87%E3%83%AB?q=" + url.QueryEscape(q),
		core.BoothWebBase() + "/ja/search/" + url.PathEscape(q),
	}
	var lastErr error
	for _, u := range urls {
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", core.UA)
		req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
		resp, err := c.Do(req)
		if err != nil {
			lastErr = errors.New(core.FriendlyNetErr(err))
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("Booth 返回 %d", resp.StatusCode)
			continue
		}
		if hits := ParseBoothSearch(string(b)); len(hits) > 0 {
			return hits, nil
		}
		lastErr = nil
	}
	return nil, lastErr
}

// ---------- turning an asset name into search words ----------

var stopWords = map[string]bool{
	"for": true, "the": true, "and": true, "ver": true, "version": true, "full": true, "fullset": true, "set": true,
	"pack": true, "psd": true, "fbx": true, "blend": true, "texture": true, "textures": true, "material": true,
	"materials": true, "unity": true, "unitypackage": true, "vrchat": true, "vrc": true, "avatar": true,
	"avatars": true, "prefab": true, "data": true, "new": true, "update": true, "updated": true, "free": true,
	"booth": true, "dlc": true, "bonus": true, "sale": true, "modular": true, "with": true, "only": true,
	"zip": true, "rar": true, "file": true, "files": true, "edit": true, "fix": true, "fixed": true, "test": true,
	"対応": true, "アバター": true, "無料": true, "素材": true, "模型": true, "改変": true, "用": true, "特典": true,
	"含特典": true, "导入": true, "先导入": true, "复制": true, "整合包": true, "合集": true, "自制": true,
}

func isWordRune(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

// nameTokens splits a name into distinctive words; base names (Plum, Kaguya …) are returned apart.
func nameTokens(name string, bases []naming.BaseDef, keepBases bool) (toks, baseToks []string) {
	s := strings.ToLower(naming.CleanName(name))
	s = naming.ReBracketNum.ReplaceAllString(s, " ")
	var words []string
	var cur []rune
	kind := 0 // 1 ascii, 2 other
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range s {
		k := 0
		switch {
		case isWordRune(r):
			k = 1
		case unicode.IsLetter(r) || unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana):
			k = 2
		}
		if k == 0 || (kind != 0 && k != kind) {
			flush()
		}
		if k != 0 {
			cur = append(cur, r)
		}
		kind = k
	}
	flush()
	seen := map[string]bool{}
	for _, w := range words {
		if seen[w] || stopWords[w] || translate.ReVersionTok.MatchString(w) {
			continue
		}
		seen[w] = true
		ascii := core.IsASCII(w)
		if ascii && (len(w) < 3 || naming.IsMostlyDigits(w)) {
			continue
		}
		if !ascii && (len([]rune(w)) < 2 || naming.IsCategoryWordName(w)) {
			continue
		}
		if isBaseWord(w, bases) {
			baseToks = append(baseToks, w)
			if !keepBases {
				continue
			}
		}
		toks = append(toks, w)
	}
	return toks, baseToks
}

func isBaseWord(w string, bases []naming.BaseDef) bool {
	for _, b := range bases {
		for _, re := range b.ASCII {
			if re.MatchString(w) && len(w) <= len(b.Name)+2 {
				return true
			}
		}
		for _, o := range b.Others {
			if w == o {
				return true
			}
		}
	}
	return false
}

func tokenWeight(toks []string) int {
	n := 0
	for _, t := range toks {
		if core.IsASCII(t) {
			n += len(t)
		} else {
			n += 2 * len([]rune(t))
		}
	}
	return n
}

func BoothQueryFor(name string, bases []naming.BaseDef, cat string) string {
	toks, baseToks := nameTokens(name, bases, cat == "素体")
	if len(toks) == 0 {
		toks = baseToks
	}
	if len(toks) > 5 {
		toks = toks[:5]
	}
	return strings.Join(toks, " ")
}

func boothCategory(c string) string {
	if v, ok := naming.BoothCatMap[c]; ok {
		return v
	}
	return ""
}

// ScoreHits ranks search results against an asset name and decides whether one is a sure match.
func ScoreHits(hits []core.BoothHit, name, cat string, bases []naming.BaseDef) (best string) {
	toks, baseToks := nameTokens(name, bases, cat == "素体")
	if len(toks) == 0 && cat == "素体" {
		toks = baseToks
	}
	var full []int
	for i := range hits {
		h := &hits[i]
		hn := strings.ToLower(h.Name)
		hc := translate.ReCompact.ReplaceAllString(hn, "")
		matched := 0
		for _, t := range toks {
			if strings.Contains(hn, t) || strings.Contains(hc, t) {
				matched++
			}
		}
		if len(toks) > 0 {
			h.Score = float64(matched) / float64(len(toks))
		}
		bc := boothCategory(h.Category)
		catAdj := 0.0
		switch {
		case cat == "" || cat == "其他" || bc == "" || bc == "其他":
		case bc == cat:
			catAdj = 0.15
		default:
			catAdj = -0.5
		}
		for _, b := range baseToks {
			if strings.Contains(hn, b) {
				h.Score += 0.05
				break
			}
		}
		h.Score += catAdj
		h.Full = len(toks) > 0 && matched == len(toks) && catAdj >= 0
		if h.Full {
			full = append(full, i)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if tokenWeight(toks) < 5 || len(full) == 0 {
		return ""
	}
	var fulls []core.BoothHit
	for _, h := range hits {
		if h.Full {
			fulls = append(fulls, h)
		}
	}
	if len(fulls) == 1 || fulls[0].Score-fulls[1].Score >= 0.15 {
		return fulls[0].ID
	}
	return "" // several different products fit equally well: let the user choose
}

// ---------- background matching for assets without any picture ----------

var TaskMatch = &core.Task{Name: "match", Label: "在 Booth 上找封面"}

type MatchJob struct {
	Key, Name, Cat string
}

// AssetBooth: which Booth item an asset belongs to, and how we know ("user", "name", "url",
// "library", "auto"). key is the asset key; a may be nil for netdisk-only assets.
func AssetBooth(st *core.Store, key string, a *core.Asset) (string, string) {
	u := st.User[key]
	if u != nil && u.BoothURL != "" {
		if m := naming.ReBoothURL.FindStringSubmatch(u.BoothURL); m != nil {
			return m[1], "user"
		}
	}
	// a share whose folder or files carry the item number ("8099091/AONAMI_….zip")
	if a == nil && strings.HasPrefix(key, "pan:") {
		if l, it := netdisk.PanSub(st, key); l != nil {
			info := netdisk.AnalyzePan(l, naming.ParseBases(st.Settings.Bases))
			if it == nil && len(netdisk.SplitPanCached(l)) >= 2 {
				info.Parts = nil // a collection: an item number inside it belongs to one of its products
			}
			if id := netdisk.PanBoothID(l, info); id != "" {
				return id, "name"
			}
		}
	}
	if a != nil && a.BoothID != "" {
		switch {
		case a.BoothFromLib:
			return a.BoothID, "library"
		case a.BoothFromURL:
			return a.BoothID, "url"
		}
		return a.BoothID, "name"
	}
	if u != nil && u.NoBooth {
		return "", ""
	}
	if m := st.BoothMatch[key]; m != nil && m.ID != "" {
		return m.ID, "auto"
	}
	return "", ""
}

// ---------- remote thumbnails (Booth search results) ----------

func RemoteThumb(st *core.Store, u string) (string, error) {
	pu, err := url.Parse(u)
	if err != nil || pu.Scheme != "https" || !(pu.Host == "booth.pximg.net" || strings.HasSuffix(pu.Host, ".booth.pm") || pu.Host == "booth.pm") {
		if os.Getenv("VRCLIB_BOOTH_WEB") == "" || !strings.HasPrefix(u, os.Getenv("VRCLIB_BOOTH_WEB")) {
			return "", errors.New("host not allowed")
		}
	}
	h := sha1.Sum([]byte(u))
	name := hex.EncodeToString(h[:10])
	dir := filepath.Join(core.CoversDir(), "remote")
	for _, ext := range []string{".jpg", ".png", ".webp", ".gif", ".jpeg"} {
		if p := filepath.Join(dir, name+ext); core.FileExists(p) {
			return p, nil
		}
	}
	_ = os.MkdirAll(dir, 0755)
	return DownloadTo(core.HTTPClient(st), u, filepath.Join("remote", name))
}
