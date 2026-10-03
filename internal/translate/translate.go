package translate

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

	"vrclib/internal/core"
)

var ReVersionTok = regexp.MustCompile(`^(v|ver|version|r)?\d+([._]\d+)*[a-z]?$`)

var ReCompact = regexp.MustCompile(`[\s_\-\.\+&・·'"!！?？~～/\\:：,，、()（）\[\]［］【】「」『』《》|#*=⊹࣪˖♰✨🌸🐰💗🩷❤️]+`)

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
	req.Header.Set("User-Agent", core.UA)
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
	req.Header.Set("User-Agent", core.UA)
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
	req.Header.Set("User-Agent", core.UA)
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

// TranslateText translates one block of text (≤ ~900 characters).
func TranslateText(st *core.Store, text string) (string, error) {
	out, err := bing.translate(text)
	if err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}
	out2, err2 := googleTranslate(core.HTTPClient(st), text)
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
var ZHGlossary = strings.NewReplacer("头像", "模型", "化身", "模型", "阿凡达", "模型", "虚拟形象", "模型")

// ZHSource cleans a name for translation; "" when it needs no translation (already Chinese, no words).
func ZHSource(name string) string {
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
	if kana == 0 && ReVersionTok.MatchString(strings.ToLower(strings.ReplaceAll(s, " ", ""))) {
		return ""
	}
	return s
}

func NormCmp(s string) string { return strings.ToLower(ReCompact.ReplaceAllString(s, "")) }

var (
	TaskTrans  = &core.Task{Name: "trans", Label: "翻译名称"}
	TransBusy  atomic.Bool
	TransAgain atomic.Bool
)

// TranslateLong translates a description, paragraph by paragraph (cached).
func TranslateLong(st *core.Store, text string) (string, error) {
	h := sha1.Sum([]byte(text))
	key := "h:" + hex.EncodeToString(h[:12])
	st.Mu.RLock()
	if v, ok := st.Trans[key]; ok && v != "" {
		st.Mu.RUnlock()
		return v, nil
	}
	st.Mu.RUnlock()
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
		t, err := TranslateText(st, c)
		if err != nil {
			return "", err
		}
		out = append(out, t)
		if i < len(chunks)-1 {
			time.Sleep(400 * time.Millisecond)
		}
	}
	res := ZHGlossary.Replace(strings.Join(out, "\n"))
	st.Mu.Lock()
	st.Trans[key] = res
	st.Mu.Unlock()
	return res, nil
}
