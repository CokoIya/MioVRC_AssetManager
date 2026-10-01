package main

// Booth search: lets every asset reach its Booth page, and fills in missing covers by matching
// asset names to Booth products (only when the match is unambiguous).

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
	"time"
	"unicode"
)

type BoothHit struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Shop     string  `json:"shop"`
	ShopSub  string  `json:"shopSub"`
	Thumb    string  `json:"thumb"`
	Price    string  `json:"price"`
	Category string  `json:"category"`
	Score    float64 `json:"score,omitempty"`
	Full     bool    `json:"full,omitempty"` // every distinctive word of the asset name is in it
}

type BoothMatch struct {
	ID    string     `json:"id,omitempty"` // picked automatically (unambiguous)
	Query string     `json:"query"`
	Hits  []BoothHit `json:"hits,omitempty"`
	Tried int64      `json:"tried"`
	Err   string     `json:"err,omitempty"`
}

func boothWebBase() string {
	if v := os.Getenv("VRCLIB_BOOTH_WEB"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://booth.pm"
}

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

func parseBoothSearch(page string) []BoothHit {
	var out []BoothHit
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
		h := BoothHit{ID: id, ShopSub: sub(reCardBrand, card), Thumb: sub(reCardThumb, card),
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
func SearchBooth(c *http.Client, q string) ([]BoothHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, errors.New("没有可以搜索的关键词")
	}
	urls := []string{
		boothWebBase() + "/ja/browse/3D%E3%83%A2%E3%83%87%E3%83%AB?q=" + url.QueryEscape(q),
		boothWebBase() + "/ja/search/" + url.PathEscape(q),
	}
	var lastErr error
	for _, u := range urls {
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
		resp, err := c.Do(req)
		if err != nil {
			lastErr = errors.New(friendlyNetErr(err))
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("Booth 返回 %d", resp.StatusCode)
			continue
		}
		if hits := parseBoothSearch(string(b)); len(hits) > 0 {
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

var reVersionTok = regexp.MustCompile(`^(v|ver|version|r)?\d+([._]\d+)*[a-z]?$`)

func isWordRune(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

// nameTokens splits a name into distinctive words; base names (Plum, Kaguya …) are returned apart.
func nameTokens(name string, bases []baseDef, keepBases bool) (toks, baseToks []string) {
	s := strings.ToLower(cleanName(name))
	s = reBracketNum.ReplaceAllString(s, " ")
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
		if seen[w] || stopWords[w] || reVersionTok.MatchString(w) {
			continue
		}
		seen[w] = true
		ascii := isASCII(w)
		if ascii && (len(w) < 3 || isMostlyDigits(w)) {
			continue
		}
		if !ascii && (len([]rune(w)) < 2 || isCategoryWordName(w)) {
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

func isBaseWord(w string, bases []baseDef) bool {
	for _, b := range bases {
		for _, re := range b.ascii {
			if re.MatchString(w) && len(w) <= len(b.name)+2 {
				return true
			}
		}
		for _, o := range b.others {
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
		if isASCII(t) {
			n += len(t)
		} else {
			n += 2 * len([]rune(t))
		}
	}
	return n
}

func boothQueryFor(name string, bases []baseDef, cat string) string {
	toks, baseToks := nameTokens(name, bases, cat == "素体")
	if len(toks) == 0 {
		toks = baseToks
	}
	if len(toks) > 5 {
		toks = toks[:5]
	}
	return strings.Join(toks, " ")
}

var reCompact = regexp.MustCompile(`[\s_\-\.\+&・·'"!！?？~～/\\:：,，、()（）\[\]［］【】「」『』《》|#*=⊹࣪˖♰✨🌸🐰💗🩷❤️]+`)

func boothCategory(c string) string {
	if v, ok := boothCatMap[c]; ok {
		return v
	}
	return ""
}

// scoreHits ranks search results against an asset name and decides whether one is a sure match.
func scoreHits(hits []BoothHit, name, cat string, bases []baseDef) (best string) {
	toks, baseToks := nameTokens(name, bases, cat == "素体")
	if len(toks) == 0 && cat == "素体" {
		toks = baseToks
	}
	var full []int
	for i := range hits {
		h := &hits[i]
		hn := strings.ToLower(h.Name)
		hc := reCompact.ReplaceAllString(hn, "")
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
	var fulls []BoothHit
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

var taskMatch = &Task{Name: "match", Label: "在 Booth 上找封面"}

type matchJob struct {
	key, name, cat string
}

func RunAutoMatch(st *Store, prog *Task) {
	st.mu.RLock()
	if st.Settings.NoAutoMatch {
		st.mu.RUnlock()
		return
	}
	bases := parseBases(st.Settings.Bases)
	now := time.Now().Unix()
	var jobs []matchJob
	consider := func(key string, a *Asset, name, cat string, hasCover bool) {
		u := st.User[key]
		if hasCover || (u != nil && (u.Cover != "" || u.NoBooth)) {
			return
		}
		if id, _ := assetBooth(st, key, a); id != "" {
			return
		}
		if m := st.BoothMatch[key]; m != nil && now-m.Tried < 7*86400 {
			return
		}
		if boothQueryFor(name, bases, cat) == "" {
			return
		}
		jobs = append(jobs, matchJob{key, name, cat})
	}
	for _, a := range st.Assets {
		hasCover := len(a.Covers) > 0
		if p := st.Purchases[a.BoothID]; p != nil && p.Cover != "" {
			hasCover = true
		}
		consider(a.Key, a, a.Name, a.Category, hasCover)
	}
	for _, key := range sortedKeys(st.User) {
		if strings.HasPrefix(key, "pan:") {
			if l := st.Pan[strings.TrimPrefix(key, "pan:")]; l == nil || l.Title == "" {
				continue // not read yet
			}
			name := panAssetName(st, key)
			consider(key, nil, name, classify(name), false)
		}
	}
	st.mu.RUnlock()
	if len(jobs) > 60 {
		jobs = jobs[:60]
	}
	if len(jobs) == 0 {
		return
	}
	c := httpClient(st)
	fails := 0
	for i, j := range jobs {
		prog.Set(i, len(jobs), j.name)
		q := boothQueryFor(j.name, bases, j.cat)
		hits, err := SearchBooth(c, q)
		m := &BoothMatch{Query: q, Tried: time.Now().Unix()}
		if err != nil {
			m.Err = err.Error()
			fails++
			if fails >= 3 && fails == i+1 {
				prog.Set(len(jobs), len(jobs), "连不上 Booth，请在设置里填代理")
				return
			}
		} else {
			m.ID = scoreHits(hits, j.name, j.cat, bases)
			if len(hits) > 8 {
				hits = hits[:8]
			}
			m.Hits = hits
		}
		st.mu.Lock()
		st.BoothMatch[j.key] = m
		st.mu.Unlock()
		if i%6 == 5 {
			bumpRev()
		}
		time.Sleep(1200 * time.Millisecond)
	}
	prog.Set(len(jobs), len(jobs), "完成")
}

// assetBooth: which Booth item an asset belongs to, and how we know ("user", "name", "url",
// "library", "auto"). key is the asset key; a may be nil for netdisk-only assets.
func assetBooth(st *Store, key string, a *Asset) (string, string) {
	u := st.User[key]
	if u != nil && u.BoothURL != "" {
		if m := reBoothURL.FindStringSubmatch(u.BoothURL); m != nil {
			return m[1], "user"
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

func remoteThumb(st *Store, u string) (string, error) {
	pu, err := url.Parse(u)
	if err != nil || pu.Scheme != "https" || !(pu.Host == "booth.pximg.net" || strings.HasSuffix(pu.Host, ".booth.pm") || pu.Host == "booth.pm") {
		if os.Getenv("VRCLIB_BOOTH_WEB") == "" || !strings.HasPrefix(u, os.Getenv("VRCLIB_BOOTH_WEB")) {
			return "", errors.New("host not allowed")
		}
	}
	h := sha1.Sum([]byte(u))
	name := hex.EncodeToString(h[:10])
	dir := filepath.Join(coversDir(), "remote")
	for _, ext := range []string{".jpg", ".png", ".webp", ".gif", ".jpeg"} {
		if p := filepath.Join(dir, name+ext); fileExists(p) {
			return p, nil
		}
	}
	_ = os.MkdirAll(dir, 0755)
	return downloadTo(httpClient(st), u, filepath.Join("remote", name))
}
