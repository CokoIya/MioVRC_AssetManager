package main

// Simplified-Chinese names for assets. Uses Bing's public web translator (cn.bing.com works in
// mainland China without a proxy), falling back to Google's. Results are cached in library.json.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

type bingSession struct {
	mu      sync.Mutex
	c       *http.Client
	ig, iid string
	key     string
	token   string
	until   time.Time
	n       int
}

var bing = &bingSession{}

var (
	reBingIG    = regexp.MustCompile(`IG:"([0-9A-Fa-f]+)"`)
	reBingIID   = regexp.MustCompile(`data-iid="(translator\.\d+)"`)
	reBingToken = regexp.MustCompile(`params_AbusePreventionHelper\s*=\s*\[(\d+),"([^"]+)",(\d+)\]`)
)

func bingBase() string {
	if v := os.Getenv("VRCLIB_BING_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://cn.bing.com"
}

func (b *bingSession) init() error {
	if b.c == nil {
		jar, _ := cookiejar.New(nil)
		b.c = &http.Client{Jar: jar, Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	}
	if b.ig != "" && time.Now().Before(b.until) {
		return nil
	}
	req, _ := http.NewRequest("GET", bingBase()+"/translator?from=ja&to=zh-Hans", nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	resp, err := b.c.Do(req)
	if err != nil {
		return err
	}
	page, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	ig, iid, tk := reBingIG.FindSubmatch(page), reBingIID.FindSubmatch(page), reBingToken.FindSubmatch(page)
	if ig == nil || iid == nil || tk == nil {
		return errors.New("Bing 翻译页面变了")
	}
	b.ig, b.iid, b.key, b.token = string(ig[1]), string(iid[1]), string(tk[1]), string(tk[2])
	b.until = time.Now().Add(25 * time.Minute)
	b.n = 0
	return nil
}

func (b *bingSession) translate(text string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.init(); err != nil {
		return "", err
	}
	b.n++
	u := fmt.Sprintf("%s/ttranslatev3?isVertical=1&&IG=%s&IID=%s.%d", bingBase(), b.ig, b.iid, b.n)
	form := url.Values{"fromLang": {"auto-detect"}, "to": {"zh-Hans"}, "text": {text}, "token": {b.token}, "key": {b.key},
		"tryFetchingGenderDebiasedTranslations": {"true"}}
	req, _ := http.NewRequest("POST", u, strings.NewReader(form.Encode()))
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", bingBase()+"/translator")
	resp, err := b.c.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	var r []struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if json.Unmarshal(body, &r) != nil || len(r) == 0 || len(r[0].Translations) == 0 {
		b.ig = "" // token may have expired: fetch a new one next time
		return "", fmt.Errorf("Bing 翻译没有返回结果（%d）", resp.StatusCode)
	}
	return r[0].Translations[0].Text, nil
}

func googleTranslate(c *http.Client, text string) (string, error) {
	u := "https://translate.googleapis.com/translate_a/single?client=gtx&sl=auto&tl=zh-CN&dt=t&q=" + url.QueryEscape(text)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", ua)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	var r []any
	if json.Unmarshal(body, &r) != nil || len(r) == 0 {
		return "", fmt.Errorf("Google 翻译没有返回结果（%d）", resp.StatusCode)
	}
	segs, _ := r[0].([]any)
	var sb strings.Builder
	for _, s := range segs {
		if p, ok := s.([]any); ok && len(p) > 0 {
			if t, ok := p[0].(string); ok {
				sb.WriteString(t)
			}
		}
	}
	return sb.String(), nil
}

// translateText translates one block of text (≤ ~900 characters).
func translateText(st *Store, text string) (string, error) {
	out, err := bing.translate(text)
	if err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}
	out2, err2 := googleTranslate(httpClient(st), text)
	if err2 == nil && strings.TrimSpace(out2) != "" {
		return out2, nil
	}
	if err == nil {
		err = err2
	}
	return "", err
}

// ---------- names ----------

var reDecor = regexp.MustCompile(`[\p{So}\p{Sk}\p{Mn}\x{FE0F}\x{200D}\x{2200}-\x{22FF}\x{2900}-\x{2BFF}]+`)

// VRChat wording the machine translators get wrong ("avatar" is not a profile picture here).
var zhGlossary = strings.NewReplacer("头像", "模型", "化身", "模型", "阿凡达", "模型", "虚拟形象", "模型")

// zhSource cleans a name for translation; "" when it needs no translation (already Chinese, no words).
func zhSource(name string) string {
	s := reDecor.ReplaceAllString(name, " ")
	s = strings.NewReplacer("_", " ", "＿", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	kana, han, latin := 0, 0, 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana++
		case unicode.Is(unicode.Han, r):
			han++
		case r < 128 && unicode.IsLetter(r):
			latin++
		}
	}
	if kana == 0 && latin < 3 {
		return "" // Chinese already, or nothing to translate
	}
	if kana == 0 && (han >= 4 || (han > 0 && latin < 8)) {
		return "" // Chinese already (product names in English are kept as they are)
	}
	if kana == 0 && reVersionTok.MatchString(strings.ToLower(strings.ReplaceAll(s, " ", ""))) {
		return ""
	}
	return s
}

func normCmp(s string) string { return strings.ToLower(reCompact.ReplaceAllString(s, "")) }

var (
	taskTrans  = &Task{Name: "trans", Label: "翻译名称"}
	transBusy  atomic.Bool
	transAgain atomic.Bool
)

// KickTranslate translates names that appeared since the last run, in the background.
// A request arriving while a run is in progress makes that run go round once more.
func KickTranslate(st *Store) {
	transAgain.Store(true)
	if !transBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer transBusy.Store(false)
		for transAgain.Swap(false) {
			run(taskTrans, func() { RunTranslate(st, taskTrans, zhSources(st)) })
			_ = st.Save()
			bumpRev()
		}
	}()
}

// RunTranslate translates names that have no cached translation yet, in batches.
func RunTranslate(st *Store, prog *Task, sources []string) {
	st.mu.RLock()
	if st.Settings.HideZh {
		st.mu.RUnlock()
		return
	}
	var todo []string
	seen := map[string]bool{}
	for _, s := range sources {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		if _, ok := st.Trans[s]; !ok {
			todo = append(todo, s)
		}
	}
	st.mu.RUnlock()
	if len(todo) == 0 {
		return
	}
	fails := 0
	for i := 0; i < len(todo); {
		// pack lines up to ~800 characters per request
		j, n := i, 0
		for j < len(todo) && (j == i || n+len([]rune(todo[j]))+1 <= 800) && j-i < 40 {
			n += len([]rune(todo[j])) + 1
			j++
		}
		batch := todo[i:j]
		prog.Set(i, len(todo), "翻译名称")
		out, err := translateText(st, strings.Join(batch, "\n"))
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if err == nil && len(lines) != len(batch) {
			// line structure got lost: translate one by one
			lines = lines[:0]
			for _, s := range batch {
				t, e := translateText(st, s)
				if e != nil {
					err = e
					break
				}
				lines = append(lines, t)
				time.Sleep(300 * time.Millisecond)
			}
		}
		if err != nil || len(lines) != len(batch) {
			fails++
			logf("翻译失败: %v", err)
			if fails >= 3 {
				prog.Set(len(todo), len(todo), "翻译服务连不上，稍后再试")
				return
			}
			time.Sleep(2 * time.Second)
			continue
		}
		st.mu.Lock()
		for k, src := range batch {
			t := zhGlossary.Replace(strings.TrimSpace(lines[k]))
			if normCmp(t) == normCmp(src) {
				t = "" // nothing gained
			}
			st.Trans[src] = t
		}
		st.mu.Unlock()
		bumpRev()
		i = j
		time.Sleep(600 * time.Millisecond)
	}
	prog.Set(len(todo), len(todo), "完成")
}

// translateLong translates a description, paragraph by paragraph (cached).
func translateLong(st *Store, text string) (string, error) {
	h := sha1.Sum([]byte(text))
	key := "h:" + hex.EncodeToString(h[:12])
	st.mu.RLock()
	if v, ok := st.Trans[key]; ok && v != "" {
		st.mu.RUnlock()
		return v, nil
	}
	st.mu.RUnlock()
	var chunks []string
	var cur strings.Builder
	const limit = 850 // characters per request (Bing's web translator takes up to 1000)
	for _, para := range strings.Split(text, "\n") {
		if len([]rune(cur.String()))+len([]rune(para)) > limit && cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
		for r := []rune(para); len(r) > limit; r = []rune(para) { // a very long line
			chunks = append(chunks, string(r[:limit]))
			para = string(r[limit:])
		}
		if cur.Len() > 0 {
			cur.WriteString("\n")
		}
		cur.WriteString(para)
	}
	if cur.Len() > 0 {
		chunks = append(chunks, cur.String())
	}
	var out []string
	for i, c := range chunks {
		if strings.TrimSpace(c) == "" {
			out = append(out, c)
			continue
		}
		t, err := translateText(st, c)
		if err != nil {
			return "", err
		}
		out = append(out, t)
		if i < len(chunks)-1 {
			time.Sleep(400 * time.Millisecond)
		}
	}
	res := zhGlossary.Replace(strings.Join(out, "\n"))
	st.mu.Lock()
	st.Trans[key] = res
	st.mu.Unlock()
	return res, nil
}
