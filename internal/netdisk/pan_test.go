package netdisk

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"vrclib/internal/core"
)

func TestShareLinks(t *testing.T) {
	for _, c := range []struct{ link, surl, pwd string }{
		{"https://pan.baidu.com/s/1AbCd_Ef-Gh?pwd=ab12", "1AbCd_Ef-Gh", "ab12"},
		{"https://pan.baidu.com/s/1AbCdEfG-_", "1AbCdEfG-_", ""}, // an id may end in a dash
		{"https://pan.baidu.com/s/1AbCdEfG-_ 提取码 x9y8", "1AbCdEfG-_", "x9y8"},
		{"链接：https://pan.baidu.com/s/1AbCdEfGh 提取码：ab12", "1AbCdEfGh", "ab12"},
		{"链接: https://pan.baidu.com/s/1AbCdEfGh?pwd=zz99 提取码: ab12", "1AbCdEfGh", "zz99"}, // the link's own code first
		{"https://pan.baidu.com/s/1AbCd--来自百度网盘超级会员V5的分享", "1AbCd", ""},
		{"https://pan.baidu.com/s/1AbCdEfGh--来自百度网盘超级会员V5的分享", "1AbCdEfGh", ""},
		{"https://pan.baidu.com/s/1AbCdEfGh_提取码ab12", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/s/1AbCdEfGh 密码: ab12", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/s/1AbCdEfGh 提取碼:ab12。", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/share/init?surl=AbCdEfGh&pwd=ab12", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/wap/init?surl=AbCdEfGh", "1AbCdEfGh", ""},
		{"https://yun.baidu.com/s/1AbCdEfGh", "1AbCdEfGh", ""},
		{"https://yun.baidu.com/share/init?surl=AbCdEfGh", "1AbCdEfGh", ""},
		{"https://pan.baidu.com/share/link?shareid=123&uk=456", "", ""}, // the old kind of link: not read
		{"https://pan.baidu.com/s/1AbCdEfGh?pwd=ａｂ１２", "1AbCdEfGh", "ab12"},
		{"ｈｔｔｐｓ：／／ｐａｎ．ｂａｉｄｕ．ｃｏｍ／ｓ／１ＡｂＣｄＥｆＧｈ？ｐｗｄ＝ａｂ１２", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/s/1AbCdEfGh ｐｗｄ＝ａｂ１２", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/s/1AbCdEfGh?pwd=ab12。", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/s/1AbCdEfGh?pwd=ab12cd", "1AbCdEfGh", ""}, // a code has four characters
		{"https://pan.baidu.com/s/1AbCdEfGh#list/path=%2F&pwd=zz99", "1AbCdEfGh", "zz99"},
		{"http://pan.baidu.com/s/1AbCdEfGh?from=init&pwd=ab12", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/e/1AbCdEfGh", "", ""},
		{"https://pan.baidu.com/s/1AbCdEfGh?_at_=123&pwd=ab12", "1AbCdEfGh", "ab12"},
		{"https://pan.baidu.com/s/1qcodeAbCd-xyz", "1qcodeAbCd-xyz", ""}, // "code" inside an id is not a code
		{"https://example.com/s/1AbCdEfGh", "", ""},
	} {
		if surl, pwd := ShareSurl(c.link), SharePwdFromURL(c.link); surl != c.surl || pwd != c.pwd {
			t.Errorf("%s\n\tsurl %q pwd %q, want %q %q", c.link, surl, pwd, c.surl, c.pwd)
		}
	}
}

// A pasted text with links of both kinds: the first one says which share a new card is made of. A link field
// that holds both is kept as the Baidu share's (ShareID).
func TestCloudFirst(t *testing.T) {
	const gd, db, bd = "https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz012345/view", "https://www.dropbox.com/s/abc123xyz456789/a.zip?dl=0", "https://pan.baidu.com/s/1abcDEF_gh"
	both := "链接: " + bd + " 提取码: ab12  海外: " + gd
	for _, c := range []struct {
		text  string
		cloud bool
	}{
		{gd, true}, {db, true}, {bd, false}, {"", false}, {"no link here", false},
		{both, false},
		{"海外: " + gd + " 国内: " + bd + " 提取码: ab12", true},
		{db + " / " + bd, true},
		{"https://yun.baidu.com/share/init?surl=abcDEF_gh&pwd=ab12 或 " + db, false},
		{"ｈｔｔｐｓ：／／ｐａｎ．ｂａｉｄｕ．ｃｏｍ／ｓ／１ＡｂＣｄＥｆＧｈ　" + gd, false},
	} {
		if got := CloudFirst(c.text); got != c.cloud {
			t.Errorf("%s\n\tcloud first: %v, want %v", c.text, got, c.cloud)
		}
	}
	if ShareID(both) != "1abcDEF_gh" || ShareID("海外: "+gd+" 国内: "+bd) != "1abcDEF_gh" || ShareID(gd) != "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz012345" {
		t.Errorf("share ids: %q %q %q", ShareID(both), ShareID("海外: "+gd+" 国内: "+bd), ShareID(gd))
	}
}

func TestRedactDebug(t *testing.T) {
	in := `verify: {"errno":0,"randsk":"SECRET%2Bsk","request_id":123}
{"bdstoken":"TOK","username":"mio_account","uk":1234567,"share_uk":"39","shareid":46386138315,"file_list":[{"server_filename":"a.zip","size":5}]}`
	out := RedactDebug(in)
	for _, gone := range []string{"SECRET", "TOK", "mio_account", "1234567"} {
		if strings.Contains(out, gone) {
			t.Errorf("%s is still in the debug text", gone)
		}
	}
	for _, kept := range []string{`"errno":0`, `"share_uk":"39"`, `"shareid":46386138315`, `"server_filename":"a.zip"`, `"randsk":"…"`} {
		if !strings.Contains(out, kept) {
			t.Errorf("%s is gone from the debug text: %s", kept, out)
		}
	}
}

// Once Baidu asks for a captcha nothing more is asked of it: not the other way in, not the next folder.
func TestCaptchaEndsTheReading(t *testing.T) {
	core.DataDir = t.TempDir()
	var mu sync.Mutex
	hits := map[string]int{}
	captchaAt := "/share/verify"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		at := captchaAt
		mu.Unlock()
		switch {
		case r.URL.Path == at:
			fmt.Fprint(w, `{"errno":-62}`)
		case r.URL.Path == "/share/verify":
			fmt.Fprint(w, `{"errno":0,"randsk":"sk"}`)
		case r.URL.Path == "/s/1AbCdEfGh":
			fmt.Fprint(w, `<script id="locals-data" type="application/json">{"share_uk":"3","shareid":4,"file_list":[`+
				`{"fs_id":1,"server_filename":"A","path":"/A","isdir":1},{"fs_id":2,"server_filename":"B","path":"/B","isdir":1},`+
				`{"fs_id":3,"server_filename":"C","path":"/C","isdir":1}]}</script>`)
		default:
			fmt.Fprint(w, `{"errno":0,"list":[]}`)
		}
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_PAN_BASE", srv.URL)
	st := &core.Store{}
	for _, at := range []string{"/share/verify", "/share/list"} {
		mu.Lock()
		captchaAt, hits = at, map[string]int{}
		mu.Unlock()
		l, err := FetchPanListing(st, "https://pan.baidu.com/s/1AbCdEfGh", "ab12")
		if l != nil || !errors.Is(err, ErrPanCaptcha) {
			t.Fatalf("captcha at %s: listing %v, err %v", at, l, err)
		}
		mu.Lock()
		if hits["/share/wxlist"] != 0 || hits["/share/list"] > 1 {
			t.Errorf("captcha at %s: asked on: %v", at, hits)
		}
		mu.Unlock()
	}
	if !PanErrSettled(PanErrno(105)) || !PanErrSettled(PanErrno(-9)) || PanErrSettled(ErrPanCaptcha.Error()) || PanErrSettled("") {
		t.Error("which errors reading again does not change")
	}
}
