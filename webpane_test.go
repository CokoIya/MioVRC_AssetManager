package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestKeptLogins(t *testing.T) {
	for d, want := range map[string]bool{".goofish.com": true, "www.goofish.com": true, ".taobao.com": true, "login.taobao.com": true,
		".booth.pm": true, "accounts.pixiv.net": true, ".baidu.com": true, "pan.baidu.com": true,
		"alipay.com": false, "notgoofish.com": false, "example.com": false} {
		if keepLoginSite(d) != want {
			t.Errorf("%s: kept %v, want %v", d, !want, want)
		}
	}
	// for the site and its subdomains: by domain; for one host: by address, so it stays a cookie of that host
	p := keptCookie{Name: "cookie2", Value: "v", Domain: ".goofish.com", Path: "/", Secure: true, HTTPOnly: true, Expires: 99, SameSite: "None"}.params()
	if p["domain"] != ".goofish.com" || p["url"] != nil || p["sameSite"] != "None" || p["expires"] != float64(99) || p["httpOnly"] != true {
		t.Errorf("domain cookie: %v", p)
	}
	p = keptCookie{Name: "sid", Value: "v", Domain: "www.goofish.com", Path: "/im", Secure: true}.params()
	if p["url"] != "https://www.goofish.com/im" || p["domain"] != nil {
		t.Errorf("host cookie: %v", p)
	}
	p = keptCookie{Name: "sid", Value: "v", Domain: "127.0.0.1", Path: "/"}.params()
	if p["url"] != "http://127.0.0.1/" {
		t.Errorf("plain host cookie: %v", p)
	}
	// the saved copy: written only when it changes, removed when nothing is left, pruned on logging out
	dataDir = t.TempDir()
	keptLast = ""
	until := float64(time.Now().Add(time.Hour).Unix())
	saveKeptLogins([]keptCookie{{Name: "cookie2", Value: "a", Domain: ".goofish.com", Path: "/", Expires: until},
		{Name: "BDUSS", Value: "b", Domain: ".baidu.com", Path: "/", Expires: until}})
	if got := loadKeptLogins(); len(got) != 2 || got[0].Name != "BDUSS" {
		t.Fatalf("saved %+v", got)
	}
	dropKeptLogins([]string{"baidu.com"})
	if got := loadKeptLogins(); len(got) != 1 || got[0].Name != "cookie2" {
		t.Errorf("after logging out of Baidu: %+v", got)
	}
	dropKeptLogins([]string{"goofish.com"})
	if statOK(filepath.Join(dataDir, "web-session.dat")) {
		t.Error("nothing left: no file")
	}
}
