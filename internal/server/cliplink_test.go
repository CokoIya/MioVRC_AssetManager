package server

import (
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/webpane"
)

// What among copied text is a 闲鱼 or a Booth page to offer, and what is not: a shopping link, a site that is
// merely named, an address inside another one. What is passed on is the address alone, as a browser takes it.
func TestClipLink(t *testing.T) {
	t.Setenv("VRCLIB_BOOTH_WEB", "")
	t.Setenv("VRCLIB_XY_BASE", "")
	item := "https://booth.pm/ja/items/1234567"
	for _, c := range []struct{ text, kind, url string }{
		// Booth: an item's page, with or without a language, a shop's name or "https://" before it
		{item, "booth", item},
		{"https://booth.pm/items/1234567", "booth", "https://booth.pm/items/1234567"},
		{"https://booth.pm/zh-cn/items/1234567?utm_source=x#top", "booth", "https://booth.pm/zh-cn/items/1234567?utm_source=x#top"},
		{"http://booth.pm/en/items/12", "booth", "http://booth.pm/en/items/12"},
		{"booth.pm/ja/items/1234567", "booth", item},
		{"HTTPS://BOOTH.PM/JA/ITEMS/1234567", "booth", "https://BOOTH.PM/JA/ITEMS/1234567"},
		{"https://komado.booth.pm/items/1234567", "booth", "https://komado.booth.pm/items/1234567"},
		{"komado.booth.pm/items/1234567", "booth", "https://komado.booth.pm/items/1234567"},
		// … a shop's page: any page of its own site
		{"https://komado.booth.pm/", "booth", "https://komado.booth.pm/"},
		{"https://komado.booth.pm", "booth", "https://komado.booth.pm"},
		{"komado.booth.pm", "booth", "https://komado.booth.pm"},
		{"https://12345.booth.pm/item_lists/8xYz", "booth", "https://12345.booth.pm/item_lists/8xYz"},
		{"https://Komado.Booth.pm/items?sort=new", "booth", "https://Komado.Booth.pm/items?sort=new"},
		// … with the words of a message around it, and with what a sentence puts after an address
		{"Kaguya 用的衣服 | KOMADO https://booth.pm/ja/items/1234567 #booth_pm", "booth", item},
		{"看看这个 " + item + "。", "booth", item},
		{"看看这个：" + item + "，很好看", "booth", item},
		{item + "这个不错", "booth", item},
		{"（" + item + "）", "booth", item},
		{"(" + item + ").", "booth", item},
		{"【Booth】" + item + "】点这里", "booth", item},
		{`"` + item + `"`, "booth", item},
		{"'" + item + "'", "booth", item},
		{"<" + item + ">", "booth", item},
		{item + "!?", "booth", item},
		{"「https://komado.booth.pm/」", "booth", "https://komado.booth.pm/"},
		{"店铺是komado.booth.pm。", "booth", "https://komado.booth.pm"},
		{"link:booth.pm/ja/items/1234567", "booth", item},
		{"第一行\r\n" + item + "\r\n第三行", "booth", item},
		// … the first one of several; a page that is none does not stand in the way of one that is
		{"https://aaa.booth.pm/ https://bbb.booth.pm/", "booth", "https://aaa.booth.pm/"},
		{"https://booth.pm/ja https://example.com/x " + item, "booth", item},
		// 闲鱼: its own sites
		{"https://www.goofish.com/item?id=123456&categoryId=50025386", "xianyu", "https://www.goofish.com/item?id=123456&categoryId=50025386"},
		{"https://goofish.com/", "xianyu", "https://goofish.com/"},
		{"https://h5.m.goofish.com/item?id=1", "xianyu", "https://h5.m.goofish.com/item?id=1"},
		{"https://www.xianyu.com/x", "xianyu", "https://www.xianyu.com/x"},
		{"https://2.taobao.com/item.htm?id=1", "xianyu", "https://2.taobao.com/item.htm?id=1"},
		{"http://s.2.taobao.com/list/?q=vrchat", "xianyu", "http://s.2.taobao.com/list/?q=vrchat"},
		{"卖家主页 https://www.goofish.com/personal?userId=1，在线", "xianyu", "https://www.goofish.com/personal?userId=1"},
		// … and one of 淘宝's short links when the text it came in says it is 闲鱼's (what the 闲鱼 app shares)
		{"【闲鱼】https://m.tb.cn/h.5abc?tk=AbCd123 CZ0001 「我在闲鱼发布了【VRChat 衣服】」\n点击链接直接打开", "xianyu", "https://m.tb.cn/h.5abc?tk=AbCd123"},
		{"闲鱼上看到的 https://m.tb.cn/h.5abc，你看看", "xianyu", "https://m.tb.cn/h.5abc"},
		// 闲鱼's before Booth's, wherever they stand
		{item + " 闲鱼有二手 https://www.goofish.com/item?id=2", "xianyu", "https://www.goofish.com/item?id=2"},
		{"https://www.goofish.com/item?id=2 原版 " + item, "xianyu", "https://www.goofish.com/item?id=2"},
	} {
		if kind, u := clipLink(c.text); kind != c.kind || u != c.url {
			t.Errorf("%q: %q %q, want %q %q", c.text, kind, u, c.kind, c.url)
		} else if p, err := webpane.PageURL(u); err != nil || p.String() != u {
			t.Errorf("%q: %q is not an address as the built-in browsers take one (%v)", c.text, u, err)
		} else if (kind == "xianyu") != webpane.XianyuURL(u) {
			t.Errorf("%q: %q — 闲鱼's: the page area says %v", c.text, u, webpane.XianyuURL(u))
		}
	}
	for _, text := range []string{
		"", "   ", "你好，在吗", "my password is hunter2", "1234567",
		// Booth's first page, a search, a category, a list: no item
		"https://booth.pm/", "https://booth.pm", "booth.pm", "https://booth.pm/ja", "https://booth.pm/ja/search/kaguya",
		"https://booth.pm/ja/browse/3D%E3%83%A2%E3%83%87%E3%83%AB", "https://booth.pm/ja/items", "https://booth.pm/ja/items/abc",
		"https://booth.pm/ja/items/1234567abc", "https://booth.pm/ja/itemsets/1234567",
		// the sites under booth.pm that are no shop's
		"https://accounts.booth.pm/library", "accounts.booth.pm", "https://manage.booth.pm/items/1234567", "https://www.booth.pm/ja/items/1234567",
		"https://api.booth.pm/x", "https://asset.booth.pm/x.png", "https://static.booth.pm/", "https://help.booth.pm/", "https://checkout.booth.pm/",
		"https://booth.booth.pm/", "https://x.y.booth.pm/", "x.y.booth.pm",
		// other sites, however they are named or whatever they carry
		"https://booth.pximg.net/c/72x72/users/1/icon.jpg", "https://notbooth.pm/items/1234567", "https://booth.pm.evil.example/ja/items/1234567",
		"komado.booth.pm.evil.example", "https://evil.example/booth.pm/ja/items/1234567", "https://example.com/?next=booth.pm/ja/items/1234567",
		"https://example.com/?u=" + item, "https://example.com/ja/items/1234567", "someone@komado.booth.pm", "support@booth.pm",
		"https://user@booth.pm/ja/items/1234567", "https://booth.pm:8443/ja/items/1234567", "https://komado.booth.pm:8443/", "//booth.pm/ja/items/1234567",
		"ftp://booth.pm/ja/items/1234567", "https://booth.pm\\ja/items/1234567",
		// a text that merely names a site
		"我在 booth.pm 上买的", "Booth 上的衣服，店名 komado", "去闲鱼看看", "闲鱼 goofish.com 上有", "www.goofish.com/item?id=1",
		// shopping links: 淘宝, 天猫, 支付宝 — also when the text speaks of 闲鱼; and 淘宝's own short links
		"https://item.taobao.com/item.htm?id=1", "https://detail.tmall.com/item.htm?id=1", "https://www.alipay.com/", "https://cashier.alipay.com/x?t=1",
		"https://login.taobao.com/member/login.jhtml", "https://12.taobao.com/x", "闲鱼和淘宝都有 https://item.taobao.com/item.htm?id=1",
		"https://m.tb.cn/h.5abc", "https://m.tb.cn/h.5abc?tk=AbCd123 CZ3457 「VRChat 衣服」",
		"【淘宝】https://m.tb.cn/h.5abc?tk=AbCd123 CZ3457 「VRChat 衣服」\n点击链接直接打开 或者 淘宝搜索直接打开",
		"【淘宝】https://m.tb.cn/h.5abc?tk=AbCd123 CZ3457 「闲鱼同款 VRChat 衣服」",
		"https://goofish.com.evil.example/", "https://notgoofish.com/", "https://www.goofish.com:8443/", "https://evil.example/?u=https://www.goofish.com/item?id=1",
		// a netdisk share is not this function's
		"链接: https://pan.baidu.com/s/1AbCdEf 提取码: x1y2", "https://drive.google.com/file/d/1AbCdEfGhIjKlMnOp/view",
		// the stand-ins of the tests are nothing outside them
		"http://127.0.0.1:8123/ja/items/1234567", "http://127.0.0.1:8123/xy/item?id=5",
	} {
		if kind, u := clipLink(text); kind != "" || u != "" {
			t.Errorf("taken for a link: %q → %q %q", text, kind, u)
		}
	}
	// a text as long as the clipboard is read at all, full of pages that are none: it is read to its end
	long := strings.Repeat("booth.pm https://booth.pm/ja ", core.ClipMaxChars/30)
	if kind, u := clipLink(long + item); kind != "booth" || u != item {
		t.Errorf("the item after many pages that are none: %q %q", kind, u)
	}

	// in the tests of the interface: the stand-ins for Booth and for 闲鱼
	t.Setenv("VRCLIB_BOOTH_WEB", "http://127.0.0.1:8123")
	t.Setenv("VRCLIB_XY_BASE", "http://127.0.0.1:8123/xy")
	for _, c := range []struct{ text, kind, url string }{
		{"http://127.0.0.1:8123/ja/items/1234567", "booth", "http://127.0.0.1:8123/ja/items/1234567"},
		{"http://127.0.0.1:8123/xy/item?id=5", "xianyu", "http://127.0.0.1:8123/xy/item?id=5"},
		{"http://127.0.0.1:8123/xy", "xianyu", "http://127.0.0.1:8123/xy"},
		{"【闲鱼】http://127.0.0.1:8123/xy/item?id=5 「我在闲鱼发布了【衣服】」", "xianyu", "http://127.0.0.1:8123/xy/item?id=5"},
		{"http://127.0.0.1:8123/ja", "", ""},
		{"http://127.0.0.1:8123/xyz/item?id=5", "", ""},
		{"http://127.0.0.1:9999/xy/item?id=5", "", ""},
		{"https://127.0.0.1:8123/xy/item?id=5", "", ""},
		{"https://item.taobao.com/item.htm?id=1", "", ""},
		{item, "booth", item},
		{"https://www.goofish.com/item?id=2", "xianyu", "https://www.goofish.com/item?id=2"},
	} {
		if kind, u := clipLink(c.text); kind != c.kind || u != c.url {
			t.Errorf("with the stand-ins, %q: %q %q, want %q %q", c.text, kind, u, c.kind, c.url)
		}
	}
}
