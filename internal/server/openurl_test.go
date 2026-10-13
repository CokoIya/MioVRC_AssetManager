package server

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/webpane"
)

// What is handed to Windows to open: a web page, a mail to the feedback address, Unity Hub's install link — in
// the form they were checked in, and nothing that could end the address on the command line it is put into.
func TestShellURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://booth.pm/ja/items/123":                                       "https://booth.pm/ja/items/123",
		"  https://miovrc.com/vrca/  ":                                        "https://miovrc.com/vrca/",
		"http://127.0.0.1:47821/":                                             "http://127.0.0.1:47821/",
		"HTTPS://Booth.PM/x":                                                  "https://Booth.PM/x",
		"https://booth.pm/ja/search/衣装":                                       "https://booth.pm/ja/search/%E8%A1%A3%E8%A3%85",
		"https://booth.pm/ja/search/Rusk's%20Dress":                           "https://booth.pm/ja/search/Rusk%27s%20Dress",
		"https://pan.baidu.com/disk/main#/index?path=%2FA'(1)":                "https://pan.baidu.com/disk/main#/index?path=%2FA%27(1)", // a folder's name (encodeURIComponent leaves ' as it is)
		"https://github.com/CokoIya/MioVRC_AssetManager/releases/tag/v1.7.13": "https://github.com/CokoIya/MioVRC_AssetManager/releases/tag/v1.7.13",
		// the feedback mail, as the page makes it
		"mailto:Coko_Iya@163.com?subject=%5BMioVRCA%201.7.13%5D%20bug&body=it's%20(still)%20broken%0A%E8%B0%A2%E8%B0%A2": "mailto:Coko_Iya@163.com?subject=%5BMioVRCA%201.7.13%5D%20bug&body=it%27s%20(still)%20broken%0A%E8%B0%A2%E8%B0%A2",
		"mailto:coko_iya@163.com":               "mailto:coko_iya@163.com",
		"unityhub://2022.3.22f1/887be4894c44":   "unityhub://2022.3.22f1/887be4894c44",
		"unityhub://2022.3.22f1c1/887be4894c44": "unityhub://2022.3.22f1c1/887be4894c44",
	} {
		if got, ok := shellURL(raw); !ok || got != want {
			t.Errorf("%q → %q %v, want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"", "about:blank", "javascript:alert(1)", "file:///C:/Windows/System32/calc.exe", "data:text/html,x", "ms-settings:display", "vcc://vpm/addRepo",
		`\\host\share\x.exe`, "//booth.pm/x", "booth.pm/x",
		// what would end the address on a command line and add an argument of its own
		`https://booth.pm/" --new-window "https://evil.example/`, `https://booth.pm/x"`, "https://booth.pm/a b", "https://booth.pm/?q=a b",
		"https://booth.pm/\tx", "https://booth.pm/\r\n--x", "https://booth.pm/\x00", "https://booth.pm/\x7f", "https://booth.pm/\u0085x", "https://booth.pm/\u00a0x",
		"https://booth.pm/\u3000x", `https://booth.pm\@evil.example/`, `https://booth.pm/x\`,
		// not a web page with a host
		"https:///booth.pm/", "https:booth.pm", "https://user:pw@booth.pm/", "https://booth.pm@evil.example/", "https://booth.pm/%zz", "https://booth.pm:8a/",
		// another mail address, other fields than a subject and a text
		"mailto:", "mailto:someone@else.example", "mailto:Coko_Iya@163.com.evil.example", "mailto:x@evil.example,Coko_Iya@163.com",
		"mailto:Coko_Iya@163.com?cc=x@evil.example", "mailto:Coko_Iya@163.com?attach=C:%5Cx", "mailto:Coko_Iya@163.com?Subject=x",
		"mailto:Coko_Iya@163.com?body=%zz", `mailto:Coko_Iya@163.com?body="x"`, "mailto:Coko_Iya@163.com?body=a b", "mailto:Coko_Iya@163.com#x",
		// anything but the install link
		"unityhub://", "unityhub://x", "unityhub://2022.3.22f1", `unityhub://2022.3.22f1/887be4894c44" --x`, "unityhub://2022.3.22f1/887be4894c44/../x",
		"unityhub://2022.3.22f1/887be4894c44?x=1", "UNITYHUB://2022.3.22f1/887be4894c44x",
	} {
		if got, ok := shellURL(raw); ok {
			t.Errorf("%q got through as %q", raw, got)
		}
	}
	// the page area's button takes web pages only
	for raw, want := range map[string]bool{"https://www.goofish.com/item?id=1": true, "http://127.0.0.1:8123/x": true,
		"mailto:Coko_Iya@163.com": false, "unityhub://2022.3.22f1/887be4894c44": false, "about:blank": false, `https://www.goofish.com/"x`: false} {
		if _, ok := webLink(raw); ok != want {
			t.Errorf("web page %q: %v, want %v", raw, ok, want)
		}
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// The two routes that hand an address to Windows: /api/openurl and the page area's 「在默认浏览器中打开」. What is
// refused never reaches Windows (here: the line core.OpenURL logs).
func TestOpenURLRoutes(t *testing.T) {
	apiToken = "t"
	_, srv := guarded(t)
	var mu sync.Mutex
	var opened []string
	old := core.Logger
	core.Logger = log.New(writerFunc(func(p []byte) (int, error) {
		if s := string(p); strings.HasPrefix(s, "open url ") {
			mu.Lock()
			opened = append(opened, strings.TrimSpace(strings.TrimPrefix(s, "open url ")))
			mu.Unlock()
		}
		return len(p), nil
	}), "", 0)
	t.Cleanup(func() { core.Logger = old })
	took := func() string {
		mu.Lock()
		defer mu.Unlock()
		s := strings.Join(opened, " | ")
		opened = nil
		return s
	}
	post := func(path string, body map[string]any) map[string]any { t.Helper(); return postJSON(t, srv, path, body) }

	for _, u := range []string{`https://booth.pm/" --new-window "https://evil.example/`, "https://booth.pm/a b", "file:///C:/x.exe", "mailto:x@evil.example", "unityhub://x"} {
		if r, got := post("/api/openurl", map[string]any{"url": u}), took(); r["ok"] != false || r["err"] != "网址无效" || got != "" {
			t.Errorf("%q: %v, opened %q", u, r, got)
		}
	}
	if r, got := post("/api/openurl", map[string]any{"url": "https://booth.pm/ja/search/Rusk's"}), took(); r["ok"] != true || got != "https://booth.pm/ja/search/Rusk%27s" {
		t.Errorf("a page: %v, opened %q", r, got)
	}
	// the install link needs Unity Hub on this computer
	t.Setenv("VRCLIB_UNITYHUB", "")
	if r, got := post("/api/openurl", map[string]any{"url": "unityhub://2022.3.22f1/887be4894c44"}), took(); r["ok"] != false || !strings.Contains(fmt.Sprint(r["err"]), "Unity Hub") || got != "" {
		t.Errorf("no Unity Hub: %v, opened %q", r, got)
	}
	t.Setenv("VRCLIB_UNITYHUB", "/opt/unityhub")
	if r, got := post("/api/openurl", map[string]any{"url": "unityhub://2022.3.22f1/887be4894c44"}), took(); r["ok"] != true || got != "unityhub://2022.3.22f1/887be4894c44" {
		t.Errorf("Unity Hub: %v, opened %q", r, got)
	}

	// a page the page area gives to the default browser (闲鱼's without a view of its own, any without a built-in
	// browser): checked as a page's address (webpane.PageURL), which keeps the query as it is — core.OpenURL refuses
	// what would end it on the command line
	webpane.NativeXy = nil
	t.Setenv("VRCLIB_BROWSER", "")
	t.Setenv("VRCLIB_DEFAULT_BROWSER", "")
	for _, c := range [][2]string{{`https://www.goofish.com/search?q=" --new-window "https://evil.example/`, "xianyu"}, {"https://www.goofish.com/search?q=a b", "xianyu"},
		{`https://booth.pm/ja/search/x?q=" --x`, "booth"}} {
		if r, got := post("/api/pane/open", map[string]any{"url": c[0], "kind": c[1]}), took(); r["ok"] != false || got != "" {
			t.Errorf("%q as %s: %v, opened %q", c[0], c[1], r, got)
		}
	}
	if r, got := post("/api/pane/open", map[string]any{"url": "https://www.goofish.com/item/it's", "kind": "xianyu"}), took(); r["ok"] != true || got != "https://www.goofish.com/item/it%27s" {
		t.Errorf("a 闲鱼 page in the default browser: %v, opened %q", r, got)
	}

	// 「在默认浏览器中打开」 on 闲鱼's view: the address the view is at
	stub := &xyStub{}
	webpane.NativeXy = stub
	defer func() {
		webpane.NativeXy = nil
		webpane.PaneTakesOver()
	}()
	post("/api/xy/prefs", map[string]any{"noticed": true})
	if r := post("/api/pane/open", map[string]any{"url": "https://www.goofish.com/", "kind": "xianyu"}); r["ok"] != true || r["xy"] != true {
		t.Fatalf("open: %v", r)
	}
	at := func(u string) map[string]any {
		t.Helper()
		stub.mu.Lock()
		stub.url = u
		stub.mu.Unlock()
		return post("/api/pane/act", map[string]any{"act": "external", "xy": true})
	}
	for _, u := range []string{`https://www.goofish.com/"--x`, "https://www.goofish.com/a b", "javascript:alert(1)", "file:///C:/x.exe"} {
		if r, got := at(u), took(); r["ok"] != false || r["err"] != "网址无效" || got != "" {
			t.Errorf("view at %q: %v, opened %q", u, r, got)
		}
	}
	if r, got := at("about:blank"), took(); r["ok"] != false || r["err"] != "当前未打开任何页面" || got != "" {
		t.Errorf("an empty view: %v, opened %q", r, got)
	}
	if r, got := at("https://www.goofish.com/item/1"), took(); r["ok"] != true || got != "https://www.goofish.com/item/1" {
		t.Errorf("view at a page: %v, opened %q", r, got)
	}
}
