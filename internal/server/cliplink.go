package server

import (
	"net/url"
	"regexp"
	"strings"

	"vrclib/internal/booth"
	"vrclib/internal/webpane"
)

// The links among what the player copies that the program offers to open: a 闲鱼 page, a Booth page. (The third
// thing looked for on the clipboard, a netdisk share, is told by core.NetdiskShareText.) Of a text that holds
// one, only the address is passed on, never the words around it.

// What an address is written with. A blank, a quote or an angle bracket ends one, and so does any character
// outside ASCII: the words glued to a link in a chat are Chinese, and an address as a browser or an app gives
// it has none.
const clipAddrChars = `[!#-;=?-\[\]_a-z~]`

// An address in a copied text: with its scheme (1), or — Booth's alone, as people write them — without (2). That
// one only at the start, or after a blank, a character outside ASCII or punctuation that sets it apart: a
// "booth.pm/…" inside another address, or after the "@" of a mail address, is none.
var reClipAddr = regexp.MustCompile(`(?i)(https?://` + clipAddrChars + `+)|(?:^|[^!-~]|["'(\[{<:,;|])((?:[a-z0-9-]+\.)?booth\.pm` + clipAddrChars + `*)`)

// what may follow an address in a sentence and is not part of it
const clipTail = ".,;:!?'()[]*~"

// clipLink: the 闲鱼 or Booth page a copied text holds a link to: whose it is ("xianyu", "booth") and its
// address, as the built-in browsers take one (webpane.PageURL), with "https://" before one that came without.
// "" when there is none. Of several, 闲鱼's comes first.
func clipLink(text string) (kind, u string) {
	// 淘宝's short links are what the 闲鱼 app shares ("【闲鱼】https://m.tb.cn/h.… 「我在闲鱼发布了…」") — and what
	// 淘宝 itself shares ("【淘宝】https://m.tb.cn/h.… 「…」"). One is 闲鱼's when the text it came in says 闲鱼 and
	// is not marked as 淘宝's own, whatever the item in it is called
	says := strings.Contains(text, "闲鱼") && !strings.Contains(text, "【淘宝】")
	shop := ""
	for _, m := range reClipAddr.FindAllStringSubmatch(text, -1) {
		raw := m[1]
		if raw == "" {
			raw = "https://" + m[2]
		}
		p, err := webpane.PageURL(strings.TrimRight(raw, clipTail))
		if err != nil {
			continue
		}
		if m[1] != "" && clipXianyu(p, says) {
			return "xianyu", p.String()
		}
		if shop == "" && clipBooth(p) {
			shop = p.String()
		}
	}
	if shop != "" {
		return "booth", shop
	}
	return "", ""
}

// clipXianyu: an address of 闲鱼 itself (short: one of 淘宝's short links counts, the text says it is 闲鱼's).
// Not of the other sites its pages lead to — 淘宝, 天猫, 支付宝: a player who copies a shopping link is not
// asked about it.
func clipXianyu(p *url.URL, short bool) bool {
	if h := strings.ToLower(p.Hostname()); p.Port() == "" {
		for _, s := range []string{"goofish.com", "xianyu.com", "2.taobao.com"} {
			if h == s || strings.HasSuffix(h, "."+s) {
				return true
			}
		}
		if short && (h == "tb.cn" || strings.HasSuffix(h, ".tb.cn")) {
			return true
		}
	}
	b, err := url.Parse(webpane.XianyuBase()) // (in tests: the stand-in for 闲鱼)
	return err == nil && b.Host == p.Host && webpane.XianyuURL(p.String())
}

// clipBooth: a page of a shop on Booth — any page of the shop's own site — or an item's page on booth.pm. Not
// booth.pm's first page, a search or a category there, and not the sites under it that are no shop's
// (accounts.booth.pm, manage.booth.pm …: booth.FollowSub knows them).
func clipBooth(p *url.URL) bool {
	if h := strings.ToLower(p.Hostname()); strings.HasSuffix(h, ".booth.pm") {
		return p.Port() == "" && booth.FollowSub(h) != ""
	}
	return booth.WishID(p.String()) != "" // (in tests the stand-in for booth.pm counts too)
}
