package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vrclib/internal/webpane"
)

// a view inside the window, as the routes see it
type xyStub struct {
	mu    sync.Mutex
	calls []string
	url   string
}

func (x *xyStub) add(s string) {
	x.mu.Lock()
	x.calls = append(x.calls, s)
	x.mu.Unlock()
}
func (x *xyStub) take() string {
	x.mu.Lock()
	defer x.mu.Unlock()
	s := strings.Join(x.calls, " | ")
	x.calls = nil
	return s
}
func (x *xyStub) Open(u string) error {
	x.mu.Lock()
	x.url = u
	x.mu.Unlock()
	x.add("open " + u)
	return nil
}
func (x *xyStub) Holding() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.url != "" && x.url != "about:blank"
}
func (x *xyStub) Place(_, _, w, h int, show bool) {
	if show && w > 0 && h > 0 {
		x.add("show")
	} else {
		x.add("hide")
	}
}
func (x *xyStub) State() (webpane.XyState, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return webpane.XyState{URL: x.url, Title: "闲鱼"}, x.url != ""
}
func (x *xyStub) Act(act string) { x.add("act " + act) }
func (x *xyStub) Blank() {
	x.mu.Lock()
	x.url = "about:blank"
	x.mu.Unlock()
	x.add("blank")
}

// 闲鱼's pages: in the default browser, or — once the player has read the note — in a view of their own; never
// through the pane, whatever the page says the address is for.
func TestXianyuRoutes(t *testing.T) {
	apiToken = "t"
	st, srv := guarded(t)
	var mu sync.Mutex
	var opened []string
	ext := webpane.PaneOpenExternal
	webpane.PaneOpenExternal = func(u string) error { mu.Lock(); opened = append(opened, u); mu.Unlock(); return nil }
	webpane.NativeXy = nil
	defer func() {
		webpane.PaneOpenExternal, webpane.NativeXy = ext, nil
		webpane.PaneTakesOver()
	}()
	post := func(path string, body any) map[string]any { t.Helper(); return postJSON(t, srv, path, body) }
	state := func() map[string]any {
		t.Helper()
		_, body := get(t, srv, "/api/state", "")
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	home := "https://www.goofish.com/"

	// no view inside the window
	if s := state(); s["xyMode"] != "external" || s["xyEmbed"] != false {
		t.Fatalf("state without a view: %v %v", s["xyMode"], s["xyEmbed"])
	}
	if r := post("/api/pane/open", map[string]any{"url": home, "kind": "xianyu"}); r["ok"] != true || r["external"] != true || len(opened) != 1 || opened[0] != home {
		t.Fatalf("open: %v, default browser %q", r, opened)
	}
	if r := post("/api/pane/open", map[string]any{"url": home, "kind": "xianyu", "resume": true}); r["ok"] != true || len(opened) != 1 {
		t.Errorf("the tab coming into view opened the browser: %v %q", r, opened)
	}
	// an address of theirs under another tab's name does not get into the pane either
	for i, u := range []string{"https://login.taobao.com/member/login.jhtml", "https://cashier.alipay.com/x?t=1", "https://m.tb.cn/h.abc"} {
		if r := post("/api/pane/open", map[string]any{"url": u, "kind": "booth"}); r["ok"] != true || r["external"] != true || r["xy"] != true || len(opened) != 2+i || opened[1+i] != u {
			t.Errorf("%s as a Booth page: %v, default browser %q", u, r, opened)
		}
	}
	// an address a browser would read differently from the way it is checked goes nowhere: not into a built-in
	// browser under any name, and not to the default browser
	for _, u := range []string{"", "javascript:alert(1)", "file:///C:/Windows", home + "\x00", home + "\r\nHost: x",
		"https://booth.pm@login.taobao.com\\x", "https://pan.baidu.com@cashier.alipay.com/%zz", "https:///www.goofish.com/", "https://www.goofish.com\\im", "https://www.goofish%2ecom/"} {
		for _, kind := range []string{"xianyu", "booth", "pan", "jinxxy", ""} {
			if r := post("/api/pane/open", map[string]any{"url": u, "kind": kind}); r["ok"] != false || r["err"] != "网址无效" || len(opened) != 4 {
				t.Errorf("bad address %q as %q: %v, default browser %q", u, kind, r, opened)
			}
		}
	}

	// a view inside the window: not before the note has been read
	stub := &xyStub{}
	webpane.NativeXy = stub
	if s := state(); s["xyMode"] != "embed" || s["xyEmbed"] != true {
		t.Fatalf("state with a view: %v %v", s["xyMode"], s["xyEmbed"])
	}
	for _, resume := range []bool{false, true} {
		if r := post("/api/pane/open", map[string]any{"url": home, "kind": "xianyu", "resume": resume}); r["ok"] != false || r["notice"] != true || stub.take() != "" || len(opened) != 4 {
			t.Fatalf("before the note (resume %v): %v", resume, r)
		}
	}
	if r := post("/api/xy/prefs", map[string]any{"noticed": true}); r["ok"] != true || r["xyMode"] != "embed" || !st.Settings.XyNoticed || st.Settings.XyExternal {
		t.Fatalf("noticed: %v", r)
	}
	if r := post("/api/pane/open", map[string]any{"url": home, "kind": "xianyu"}); r["ok"] != true || r["external"] != false || r["xy"] != true || stub.take() != "open "+home || len(opened) != 4 {
		t.Fatalf("open in the view: %v", r)
	}
	if r := post("/api/pane/state", map[string]any{}); r["kind"] != "xianyu" || r["open"] != true || r["url"] != home {
		t.Errorf("state: %v", r)
	}
	post("/api/pane/place", map[string]any{"x": 1, "y": 2, "w": 300, "h": 200, "dpr": 1, "show": true, "xy": true})
	post("/api/pane/place", map[string]any{"x": 1, "y": 2, "w": 300, "h": 200, "dpr": 1, "show": true}) // (another tab's page area)
	if got := stub.take(); got != "show | hide" {
		t.Errorf("placed: %s", got)
	}
	for _, a := range []string{"back", "reload"} {
		if r := post("/api/pane/act", map[string]any{"act": a, "xy": true}); r["ok"] != true {
			t.Errorf("%s: %v", a, r)
		}
	}
	if got := stub.take(); got != "act back | act reload" {
		t.Errorf("toolbar: %s", got)
	}
	if r := post("/api/pane/act", map[string]any{"act": "selection", "xy": true}); r["text"] != nil && r["text"] != "" {
		t.Errorf("selection of a 闲鱼 page: %v", r)
	}
	if r := post("/api/pane/act", map[string]any{"act": "back"}); r["ok"] != true || stub.take() != "" { // (another tab's toolbar)
		t.Errorf("a button meant for the pane reached the view: %v", r)
	}
	if r := post("/api/pane/act", map[string]any{"act": "external", "xy": true}); r["ok"] != true {
		t.Errorf("the page's address for the default browser: %v", r)
	}

	// saving the settings dialog with an old copy of the settings does not undo these
	if r := post("/api/settings", map[string]any{"settings": map[string]any{"roots": []string{}, "xyNoticed": false, "xyExternal": true, "noXyClip": true}, "noRescan": true}); r["ok"] != true {
		t.Fatalf("settings: %v", r)
	}
	if !st.Settings.XyNoticed || st.Settings.XyExternal || st.Settings.NoXyClip {
		t.Errorf("settings overwrote 闲鱼's: %+v", st.Settings)
	}

	// the copied share: offered or not
	if r := post("/api/xy/prefs", map[string]any{"clip": false}); r["ok"] != true || !st.Settings.NoXyClip || !st.Settings.XyNoticed {
		t.Errorf("clip off: %v", r)
	}
	if r := post("/api/xy/prefs", map[string]any{"clip": true}); r["ok"] != true || st.Settings.NoXyClip {
		t.Errorf("clip on: %v", r)
	}

	// the default browser by choice: the view lets go of its page
	if r := post("/api/xy/prefs", map[string]any{"external": true}); r["ok"] != true || r["xyMode"] != "external" || !st.Settings.XyExternal {
		t.Fatalf("external: %v", r)
	}
	if got := stub.take(); got != "hide | blank" {
		t.Errorf("leaving the view: %s", got)
	}
	if r := post("/api/pane/state", map[string]any{}); r["kind"] == "xianyu" {
		t.Errorf("the page area is still 闲鱼's: %v", r)
	}
	if r := post("/api/pane/open", map[string]any{"url": home + "im", "kind": "xianyu"}); r["external"] != true || len(opened) != 5 || stub.take() != "" {
		t.Errorf("external by choice: %v %q", r, opened)
	}
	if s := state(); s["xyMode"] != "external" || s["xyEmbed"] != true {
		t.Errorf("state after choosing the browser: %v %v", s["xyMode"], s["xyEmbed"])
	}
	if r := post("/api/xy/prefs", map[string]any{"external": false}); r["xyMode"] != "embed" || stub.take() != "" {
		t.Errorf("back inside: %v", r)
	}
}

// The clipboard is passed on for one thing only: a link that arrived on it after the page first asked — here a
// netdisk share (TestClipLinkOffer: a 闲鱼 or Booth page) — and it is looked at only while the program is the one
// in front, and the player has not turned the offer off.
func TestClipShare(t *testing.T) {
	apiToken = "t"
	st, srv := guarded(t)
	clip := filepath.Join(t.TempDir(), "clip.txt")
	t.Setenv("VRCLIB_CLIP_FILE", clip)
	clipAway.Store(false)
	put := func(s string) {
		t.Helper()
		if err := os.WriteFile(clip, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	flag := func(ext string, on bool) {
		t.Helper()
		if on {
			_ = os.WriteFile(clip+ext, nil, 0644)
		} else {
			_ = os.Remove(clip + ext)
		}
	}
	ask := func(body map[string]any) (float64, string) {
		t.Helper()
		r := postJSON(t, srv, "/api/clip/share", body)
		if r["ok"] != true {
			t.Fatalf("clip: %v", r)
		}
		seq, _ := r["seq"].(float64)
		text, _ := r["text"].(string)
		return seq, text
	}
	share := "链接: https://pan.baidu.com/s/1AbCdEf 提取码: x1y2"
	// what was there before the page first asked is not offered, share or not
	put(share)
	seq, text := ask(map[string]any{})
	if seq == 0 || text != "" {
		t.Fatalf("first look: %v %q", seq, text)
	}
	if _, text := ask(map[string]any{"seq": seq}); text != "" {
		t.Errorf("offered again without a change: %q", text)
	}
	// something else is copied: its number moves, nothing of it is passed on — not even when it names a netdisk
	for _, other := range []string{"银行卡密码 123456", "my bank password is hunter2 — see also dropbox.com for the rest", "Mr. Alipanah wrote", `<div class="col-123panel">`, "https://example.com/?next=pan.baidu.com"} {
		put(other)
		seq2, text := ask(map[string]any{"seq": seq})
		if seq2 == seq || text != "" {
			t.Errorf("other text %q: %v → %v, %q", other, seq, seq2, text)
		}
		if _, text := ask(map[string]any{"seq": seq2, "read": true}); text != "" {
			t.Errorf("other text %q on the button: %q", other, text)
		}
		seq = seq2
	}
	// a share is copied: the page is told what it is, and that this look was one from the front
	put("  " + share + "\n")
	r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": seq})
	seq3, _ := r["seq"].(float64)
	if seq3 == seq || r["text"] != share || r["kind"] != "share" || r["front"] != true || r["url"] != nil {
		t.Errorf("share: %v → %v", seq, r)
	}
	if _, text := ask(map[string]any{"seq": seq3}); text != "" {
		t.Errorf("offered twice: %q", text)
	}
	// the button reads what is there now, whenever it was copied (its answer is what it always was)
	if r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": seq3, "read": true}); r["text"] != share || len(r) != 3 {
		t.Errorf("the button: %v", r)
	}
	// a share on a netdisk no card can be made of: not offered (the offer would end in a refusal). On the button
	// the page learns that there is one — to say so — and nothing of it
	for _, o := range []string{
		"我用夸克网盘分享了「衣服」，点击链接即可保存。链接：https://pan.quark.cn/s/1a2b3c4d5e6f 提取码：abcd",
		"https://www.alipan.com/s/AbCdEfGh123",
		"https://www.aliyundrive.com/s/AbCdEfGh123",
		"https://www.123pan.com/s/abcd-efgh 提取码:1234",
		"https://wwx.lanzoui.com/iAbCd12345 密码:1234",
	} {
		put(o)
		r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": seq3})
		s, _ := r["seq"].(float64)
		if r["ok"] != true || s == seq3 || r["text"] != nil || r["other"] != nil {
			t.Errorf("a share that cannot be added, %q: %v", o, r)
		}
		r = postJSON(t, srv, "/api/clip/share", map[string]any{"seq": s, "read": true})
		if r["ok"] != true || r["text"] != nil || r["other"] != true {
			t.Errorf("a share that cannot be added, on the button, %q: %v", o, r)
		}
		seq3 = s
	}
	// the shares a card can be made of, as sellers send them: with the words of the netdisk around them, without
	// "https://", in full-width characters, by the other form of the link, on Google Drive and on Dropbox
	for _, o := range []string{
		"通过百度网盘分享的文件：衣服.zip\n链接：https://pan.baidu.com/s/1AbCdEf_g?pwd=x1y2 \n提取码：x1y2 \n--来自百度网盘超级会员V5的分享",
		"链接：pan.baidu.com/s/1AbCdEf_g 提取码：x1y2",
		"ｐａｎ．ｂａｉｄｕ．ｃｏｍ／ｓ／１ＡｂＣｄＥｆ 提取码 x1y2",
		"https://pan.baidu.com/share/init?surl=AbCdEf_g",
		"https://yun.baidu.com/s/1AbCdEf_g",
		"drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz012345/view?usp=sharing",
		"海外: https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz012345",
		"https://www.dropbox.com/scl/fi/abc123xyz456789/Dress.zip?rlkey=k1&dl=0",
	} {
		put(o)
		s, text := ask(map[string]any{"seq": seq3})
		if s == seq3 || text != o {
			t.Errorf("a share that can be added, %q: %v → %v, %q", o, seq3, s, text)
		}
		if r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": s, "read": true}); r["text"] != o || r["other"] != nil {
			t.Errorf("a share that can be added, on the button, %q: %v", o, r)
		}
		seq3 = s
	}
	// the player is in another program: what is copied there is not read as it comes. Back in the program it is
	// looked at once, and a share among it is offered
	flag(".back", true)
	put("写给别人的一段话")
	if s, text := ask(map[string]any{"seq": seq3}); s != seq3 || text != "" {
		t.Errorf("read while in another program: %v %q", s, text)
	}
	other := "链接：https://pan.baidu.com/s/1ZzYyXx?pwd=ab12"
	put(other)
	if r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": seq3}); r["seq"] != seq3 || r["front"] != false || len(r) != 3 {
		t.Errorf("read while in another program: %v", r)
	}
	if s, text := ask(map[string]any{"seq": seq3, "focus": true}); s == seq3 || text != other { // (the interface in a browser, which has the focus)
		t.Errorf("the page has the focus: %v %q", s, text)
	}
	flag(".back", false)
	seq4, text := ask(map[string]any{"seq": seq3})
	if seq4 == seq3 || text != other {
		t.Errorf("back in the program: %v → %v, %q", seq3, seq4, text)
	}
	// another program is holding the clipboard at that moment: asked again with the same number, not skipped
	put(share)
	flag(".busy", true)
	if s, text := ask(map[string]any{"seq": seq4}); s != seq4 || text != "" {
		t.Errorf("busy clipboard: %v %q", s, text)
	}
	flag(".busy", false)
	seq5, text := ask(map[string]any{"seq": seq4})
	if seq5 == seq4 || text != share {
		t.Errorf("after the busy moment: %v %q", seq5, text)
	}
	// the offer turned off: the clipboard is not looked at for it, whatever the page asks; the button still works
	st.Mu.Lock()
	st.Settings.NoXyClip = true
	st.Mu.Unlock()
	put(other)
	if r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": seq5, "away": true}); r["seq"] != 0.0 || r["front"] != false || len(r) != 3 {
		t.Errorf("offer off: %v", r)
	}
	if _, text := ask(map[string]any{"read": true}); text != other {
		t.Errorf("the button with the offer off: %q", text)
	}
	st.Mu.Lock()
	st.Settings.NoXyClip = false
	st.Mu.Unlock()
	// no clipboard to be had: nothing, and no error
	t.Setenv("VRCLIB_CLIP_FILE", filepath.Join(t.TempDir(), "none"))
	if seq, text := ask(map[string]any{"seq": seq5, "read": true}); seq != 0 || text != "" {
		t.Errorf("no clipboard: %v %q", seq, text)
	}
}

// a pane inside the window, as far as the routes ask about one
type paneStub struct{}

func (paneStub) Ensure(string) (int, error)   { return 0, errors.New("no browser in this test") }
func (paneStub) Place(_, _, _, _ int, _ bool) {}

// A 闲鱼 or Booth link is offered when the player comes back to the program with it — the clipboard has changed
// while another program was in front — and only where the program has a page of its own to open it in. One that
// was copied in the program itself is not. What is passed on is the address, without the words it came in.
func TestClipLinkOffer(t *testing.T) {
	apiToken = "t"
	st, srv := guarded(t)
	clip := filepath.Join(t.TempDir(), "clip.txt")
	t.Setenv("VRCLIB_CLIP_FILE", clip)
	for _, k := range []string{"VRCLIB_BROWSER", "VRCLIB_DEFAULT_BROWSER", "VRCLIB_BOOTH_WEB", "VRCLIB_XY_BASE"} {
		t.Setenv(k, "")
	}
	pane, view := webpane.NativePane, webpane.NativeXy
	webpane.NativePane, webpane.NativeXy = paneStub{}, &xyStub{}
	clipAway.Store(false)
	defer func() {
		webpane.NativePane, webpane.NativeXy = pane, view
		clipAway.Store(false)
	}()
	put := func(s string) {
		t.Helper()
		if err := os.WriteFile(clip, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	flag := func(ext string, on bool) {
		t.Helper()
		if on {
			_ = os.WriteFile(clip+ext, nil, 0644)
		} else {
			_ = os.Remove(clip + ext)
		}
	}
	// the page's look, once a second: it sends the number it was given last. What the answer offers comes back
	// as "kind address" (or "share text"), and "" for an answer that holds nothing beyond ok, seq and front
	seq := 0.0
	look := func(front bool, extra ...any) string {
		t.Helper()
		body := map[string]any{"seq": seq}
		for i := 0; i+1 < len(extra); i += 2 {
			body[extra[i].(string)] = extra[i+1]
		}
		r := postJSON(t, srv, "/api/clip/share", body)
		if r["ok"] != true || r["front"] != front {
			t.Fatalf("clip (in front: %v): %v", front, r)
		}
		seq, _ = r["seq"].(float64)
		kind, _ := r["kind"].(string)
		u, _ := r["url"].(string)
		text, _ := r["text"].(string)
		link := u != "" && text == "" && len(r) == 5
		if !map[string]bool{"": len(r) == 3, "share": text != "" && u == "" && len(r) == 5, "xianyu": link, "booth": link}[kind] {
			t.Fatalf("an answer of no known shape: %v", r)
		}
		if kind == "" {
			return ""
		}
		return kind + " " + u + text
	}
	// the player goes to another program, copies there, and comes back
	elsewhere := func(s string) string {
		t.Helper()
		flag(".back", true)
		if got := look(false); got != "" {
			t.Fatalf("offered while in another program: %q", got)
		}
		was := seq
		put(s)
		if got := look(false); got != "" || seq != was {
			t.Fatalf("read while in another program: %q, %v → %v", got, was, seq)
		}
		flag(".back", false)
		got := look(true)
		if seq == was {
			t.Fatalf("back in the program, the clipboard's number did not move: %v", seq)
		}
		if again := look(true); again != "" {
			t.Errorf("offered twice: %q", again)
		}
		return got
	}
	item, xy := "https://booth.pm/ja/items/1234567", "https://www.goofish.com/item?id=987654"
	share := "链接: https://pan.baidu.com/s/1AbCdEf 提取码: x1y2"

	// what was there before the page first asked is not offered, wherever the player was
	put(item)
	if got := look(true, "away", true); got != "" || seq == 0 {
		t.Fatalf("first look: %q %v", got, seq)
	}
	// copied while the program is in front (in its Booth page, in 闲鱼's view, with one of its buttons): not offered
	for _, s := range []string{"https://komado.booth.pm/items/7654321", xy, "【闲鱼】https://m.tb.cn/h.5abc?tk=AbCd 「我在闲鱼发布了【衣服】」"} {
		was := seq
		put(s)
		if got := look(true); got != "" || seq == was {
			t.Errorf("copied in the program, %q: offered %q (%v → %v)", s, got, was, seq)
		}
	}
	// … a netdisk share is, there too
	put(share)
	if got := look(true); got != "share "+share {
		t.Errorf("a share copied in the program: %q", got)
	}
	// copied in another program: offered on coming back, the address alone
	for _, c := range [][2]string{
		{item, "booth " + item},
		{"Kaguya 用的衣服 | KOMADO " + item + "。#booth_pm", "booth " + item},
		{"komado.booth.pm", "booth https://komado.booth.pm"},
		{xy, "xianyu " + xy},
		{"卖家发的：" + xy + "，你看看", "xianyu " + xy},
		{"【闲鱼】https://m.tb.cn/h.5abc?tk=AbCd CZ0001 「我在闲鱼发布了【衣服】」点击链接直接打开", "xianyu https://m.tb.cn/h.5abc?tk=AbCd"},
		{share, "share " + share},
		// of several in one text: the share, then 闲鱼's, then Booth's
		{item + " " + xy + " " + share, "share " + item + " " + xy + " " + share},
		{item + " 二手 " + xy, "xianyu " + xy},
		// a share on a netdisk no card can be made of is none: the page in the same text is offered
		{"夸克：https://pan.quark.cn/s/1a2b3c4d5e6f 原版：" + item, "booth " + item},
		// nothing of these: a shopping link, 淘宝's own short link, Booth's first page, its account pages, other text
		{"https://item.taobao.com/item.htm?id=1", ""},
		{"https://detail.tmall.com/item.htm?id=1", ""},
		{"【淘宝】https://m.tb.cn/h.5abc?tk=AbCd CZ3457 「VRChat 衣服」点击链接直接打开 或者 淘宝搜索直接打开", ""},
		{"https://booth.pm/ja", ""},
		{"https://accounts.booth.pm/library", ""},
		{"我的密码是 hunter2，别告诉别人", ""},
		{"https://pan.quark.cn/s/1a2b3c4d5e6f 提取码：abcd", ""},
	} {
		if got := elsewhere(c[0]); got != c[1] {
			t.Errorf("copied elsewhere, %q: %q, want %q", c[0], got, c[1])
		}
	}
	// away and back without copying anything: what is copied in the program after that is not offered
	flag(".back", true)
	look(false)
	flag(".back", false)
	look(true)
	put("https://aaa.booth.pm/")
	if got := look(true); got != "" {
		t.Errorf("copied in the program after having been away: %q", got)
	}
	// the page was not asking while the player was away (a minimised window does not): it says so
	put("https://bbb.booth.pm/")
	if got := look(true, "away", true); got != "booth https://bbb.booth.pm/" {
		t.Errorf("back from a time without looks: %q", got)
	}
	put("https://ccc.booth.pm/")
	if got := look(true); got != "" {
		t.Errorf("the page's word that it had been away outlived its look: %q", got)
	}
	// the interface in a browser: it is in front when the page has the focus, whatever window the program sees
	flag(".back", true)
	look(false, "focus", false)
	put(xy + "&from=tab")
	if got := look(true, "focus", true); got != "xianyu "+xy+"&from=tab" {
		t.Errorf("the page has the focus again: %q", got)
	}
	flag(".back", false)
	look(true)
	// another program is holding the clipboard at the moment of coming back: asked again, and still as that
	flag(".back", true)
	look(false)
	put("https://ddd.booth.pm/")
	flag(".back", false)
	flag(".busy", true)
	was := seq
	if got := look(true); got != "" || seq != was {
		t.Errorf("busy clipboard: %q %v → %v", got, was, seq)
	}
	flag(".busy", false)
	if got := look(true); got != "booth https://ddd.booth.pm/" {
		t.Errorf("after the busy moment: %q", got)
	}
	// … which a busy moment while the program is in front does not make of a look
	put("https://eee.booth.pm/")
	flag(".busy", true)
	look(true)
	flag(".busy", false)
	if got := look(true); got != "" {
		t.Errorf("copied in the program, read after a busy moment: %q", got)
	}
	// 「收录网盘链接」 is for shares as it was, and is no look: it does not count as having been away
	flag(".back", true)
	put(item + "?read=1")
	if r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": seq, "read": true}); r["ok"] != true || len(r) != 2 {
		t.Errorf("the button, a Booth link on the clipboard: %v", r)
	}
	flag(".back", false)
	if got := look(true); got != "" {
		t.Errorf("after the button was pressed while away: %q", got)
	}

	// 闲鱼 in the default browser (by choice, or with no view inside the window): its links are not offered;
	// Booth's are. A text that holds one of each is taken for 闲鱼's (which comes first), so it is not offered either
	st.Mu.Lock()
	st.Settings.XyExternal = true
	st.Mu.Unlock()
	if got := elsewhere(xy + "&x=1"); got != "" {
		t.Errorf("闲鱼 in the default browser by choice: %q", got)
	}
	if got := elsewhere(item + "?x=1"); got != "booth "+item+"?x=1" {
		t.Errorf("Booth while 闲鱼 is in the default browser: %q", got)
	}
	if got := elsewhere(item + "?x=2 " + xy + "&x=2"); got != "" {
		t.Errorf("a 闲鱼 and a Booth link, 闲鱼 in the default browser: %q", got)
	}
	st.Mu.Lock()
	st.Settings.XyExternal = false
	st.Mu.Unlock()
	webpane.NativeXy = nil
	if got := elsewhere(xy + "&x=3"); got != "" {
		t.Errorf("闲鱼 with no view inside the window: %q", got)
	}
	webpane.NativeXy = &xyStub{}
	if got := elsewhere(xy + "&x=4"); got != "xianyu "+xy+"&x=4" {
		t.Errorf("闲鱼 inside the window again: %q", got)
	}
	// no built-in page at all (links go to the default browser): Booth's are not offered. A pane in a window of
	// its own is one
	webpane.NativePane = nil
	if got := elsewhere(item + "?x=5"); got != "" {
		t.Errorf("Booth without a built-in page: %q", got)
	}
	if got := elsewhere(share + " 5"); got != "share "+share+" 5" {
		t.Errorf("a share without a built-in page: %q", got)
	}
	chrome := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(chrome, nil, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VRCLIB_BROWSER", chrome)
	if got := elsewhere(item + "?x=6"); got != "booth "+item+"?x=6" {
		t.Errorf("Booth with the pane in a window of its own (%q): %q", webpane.PaneMode(), got)
	}
	webpane.NativePane = paneStub{}

	// the offer turned off: nothing is looked at, and nothing is offered when it is turned on again
	st.Mu.Lock()
	st.Settings.NoXyClip = true
	st.Mu.Unlock()
	flag(".back", true)
	put(item + "?x=7")
	if r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": seq}); r["seq"] != 0.0 || r["front"] != false || len(r) != 3 {
		t.Errorf("offer off, away: %v", r)
	}
	flag(".back", false)
	if r := postJSON(t, srv, "/api/clip/share", map[string]any{"seq": seq, "away": true}); r["seq"] != 0.0 || r["front"] != false || len(r) != 3 {
		t.Errorf("offer off, back: %v", r)
	}
	st.Mu.Lock()
	st.Settings.NoXyClip = false
	st.Mu.Unlock()
	seq = 0 // (the page forgets its number while the offer is off)
	if got := look(true, "away", true); got != "" {
		t.Errorf("offer on again: %q", got)
	}
	if got := elsewhere(item + "?x=8"); got != "booth "+item+"?x=8" {
		t.Errorf("after the offer was turned on again: %q", got)
	}
}
