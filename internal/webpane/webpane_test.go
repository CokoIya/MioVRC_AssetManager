package webpane

import (
	"path/filepath"
	"testing"
	"time"

	"vrclib/internal/core"
)

func TestKeptLogins(t *testing.T) {
	// (闲鱼 and 淘宝 are not among them any more: xyview_test.go)
	for d, want := range map[string]bool{".goofish.com": false, "www.goofish.com": false, ".taobao.com": false, "login.taobao.com": false,
		".booth.pm": true, "accounts.pixiv.net": true, ".baidu.com": true, "pan.baidu.com": true, ".gumroad.com": true, "jinxxy.com": true,
		"alipay.com": false, "notbooth.pm": false, "example.com": false} {
		if keepLoginSite(d) != want {
			t.Errorf("%s: kept %v, want %v", d, !want, want)
		}
	}
	// for the site and its subdomains: by domain; for one host: by address, so it stays a cookie of that host
	p := keptCookie{Name: "cookie2", Value: "v", Domain: ".booth.pm", Path: "/", Secure: true, HTTPOnly: true, Expires: 99, SameSite: "None"}.params()
	if p["domain"] != ".booth.pm" || p["url"] != nil || p["sameSite"] != "None" || p["expires"] != float64(99) || p["httpOnly"] != true {
		t.Errorf("domain cookie: %v", p)
	}
	p = keptCookie{Name: "sid", Value: "v", Domain: "accounts.booth.pm", Path: "/library", Secure: true}.params()
	if p["url"] != "https://accounts.booth.pm/library" || p["domain"] != nil {
		t.Errorf("host cookie: %v", p)
	}
	p = keptCookie{Name: "sid", Value: "v", Domain: "127.0.0.1", Path: "/"}.params()
	if p["url"] != "http://127.0.0.1/" {
		t.Errorf("plain host cookie: %v", p)
	}
	// the saved copy: written only when it changes, removed when nothing is left, pruned on logging out
	core.DataDir = t.TempDir()
	keptLast = ""
	until := float64(time.Now().Add(time.Hour).Unix())
	saveKeptLogins([]keptCookie{{Name: "cookie2", Value: "a", Domain: ".booth.pm", Path: "/", Expires: until},
		{Name: "BDUSS", Value: "b", Domain: ".baidu.com", Path: "/", Expires: until}})
	if got := loadKeptLogins(); len(got) != 2 || got[0].Name != "BDUSS" {
		t.Fatalf("saved %+v", got)
	}
	dropKeptLogins([]string{"baidu.com"})
	if got := loadKeptLogins(); len(got) != 1 || got[0].Name != "cookie2" {
		t.Errorf("after logging out of Baidu: %+v", got)
	}
	dropKeptLogins([]string{"booth.pm"})
	if core.StatOK(filepath.Join(core.DataDir, "web-session.dat")) {
		t.Error("nothing left: no file")
	}
}
