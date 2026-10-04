package core

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrub(t *testing.T) {
	for in, want := range map[string]string{
		"Gumroad 已登录：mio.yukawa+vrc@example.co.jp":                                            "Gumroad 已登录：***@example.co.jp",
		`下载失败 123: Get "https://cdn.booth.pm/files/a.zip?X-Amz-Signature=abc&Expires=9": EOF`: `下载失败 123: Get "https://cdn.booth.pm/files/a.zip?…": EOF`,
		"页面打不开 https://accounts.booth.pm/users/sign_in#frag: timeout":                         "页面打不开 https://accounts.booth.pm/users/sign_in?… timeout",
		"Cookie: _plaza_session_nktz7u=abcd; other=1":                                         "Cookie: ***",
		"请求失败 BDUSS=AbCdEf123 stoken: xyz, access_token=tok123&x=1":                           "请求失败 BDUSS=*** stoken: ***, access_token=***&x=1",
		`{"password":"hunter2","name":"x"}`:                                                   `{"password":"***","name":"x"}`,
		"sid 0123456789abcdef0123456789abcdef 完成":                                             "sid *** 完成",
		`已下载 Kaguya_Summer_Dress_v1.07.zip → D:\VRChat素材\Kaguya_Summer_Dress_v1.07`:           `已下载 Kaguya_Summer_Dress_v1.07.zip → D:\VRChat素材\Kaguya_Summer_Dress_v1.07`,
		"服务地址 http://127.0.0.1:47821/":                                                        "服务地址 http://127.0.0.1:47821/",
		"检查更新：最新 1.7.6，当前 1.7.5":                                                              "检查更新：最新 1.7.6，当前 1.7.5",
	} {
		if got := Scrub(in); got != want {
			t.Errorf("Scrub(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// library.log starts anew when it is full; one earlier file is kept.
func TestLogRotation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "library.log")
	_ = os.WriteFile(p, []byte("from an earlier run\n"), 0644)
	w, err := OpenLog(p)
	if err != nil {
		t.Fatal(err)
	}
	w.(*logFile).max = 4000
	old := Logger
	Logger = log.New(w, "", 0)
	defer func() {
		Logger = old
		lf := w.(*logFile)
		lf.mu.Lock()
		if lf.f != nil {
			_ = lf.f.Close()
			lf.f = nil
		}
		lf.mu.Unlock()
	}()
	for i := 0; i < 300; i++ {
		Logf("line %d token=secret%d %s", i, i, strings.Repeat("x", 40))
	}
	cur, _ := os.ReadFile(p)
	prev, _ := os.ReadFile(p + ".1")
	if len(cur) == 0 || len(cur) > 4000 || len(prev) == 0 || len(prev) > 4000 {
		t.Fatalf("sizes: library.log %d, library.log.1 %d", len(cur), len(prev))
	}
	if !strings.HasSuffix(string(cur), "line 299 token=*** "+strings.Repeat("x", 40)+"\n") || strings.Contains(string(cur)+string(prev), "secret") {
		t.Errorf("last line %q", cur[len(cur)-70:])
	}
	if left, _ := filepath.Glob(p + "*"); len(left) != 2 {
		t.Errorf("files: %v", left)
	}
}
