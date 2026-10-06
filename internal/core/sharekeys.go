package core

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// IsNetdiskKey: the card of a share — on Baidu Netdisk ("pan:<surl>"), Google Drive ("gd:<id>") or Dropbox
// ("db:<id>") — or of a product inside one ("…#/path"). IsPanShareKey is the whole share only.
func IsNetdiskKey(key string) bool {
	return strings.HasPrefix(key, "pan:") || strings.HasPrefix(key, "gd:") || strings.HasPrefix(key, "db:")
}

// ClipMaxChars: text on the clipboard longer than this is not read: a share a seller sends is a line or two.
const ClipMaxChars = 2000

// A link to a share on one of the netdisks sellers use, with or without "https://" before it. The shape of the
// link, not the name of the site alone: a text that only mentions one of them is not a share.
var reNetdiskShare = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9.-])(?:` +
	`(?:pan|yun)\.baidu\.com/(?:s/|share/init\?|wap/init\?)[A-Za-z0-9_-]` +
	`|pan\.quark\.cn/s/[A-Za-z0-9]` +
	`|(?:www\.)?(?:aliyundrive|alipan)\.com/s/[A-Za-z0-9]` +
	`|(?:www\.)?123(?:pan|684|865|912)\.(?:com|cn)/s/[A-Za-z0-9]` +
	`|(?:[a-z0-9-]+\.)?lanzou[a-z]?\.com/[A-Za-z0-9]` +
	`|(?:drive|docs)\.google\.com/(?:(?:a/[^/\s]+/)?file/(?:u/\d+/)?d/|(?:a/[^/\s]+/)?(?:corp/)?drive/(?:u/\d+/)?(?:mobile/)?folders/|(?:u/\d+/)?(?:open|uc|folderview|embeddedfolderview)\?)` +
	`|drive\.usercontent\.google\.com/(?:u/\d+/)?(?:download|uc)\?` +
	`|(?:www\.|dl\.)?dropbox(?:usercontent)?\.com/(?:s|sh|scl)/[A-Za-z0-9]` +
	`)`)

// NetdiskShareText: s when it is a netdisk share as a seller sends it — a link, with or without its extraction
// code and the words around them — and "" for anything else. The link is looked for the way the add dialog
// reads one (netdisk.ShareSurl, cloudshare.Parse): also in full-width characters, which is how a chat may send
// it ("ｐａｎ．ｂａｉｄｕ．ｃｏｍ／ｓ／…").
func NetdiskShareText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > ClipMaxChars*4 || utf8.RuneCountInString(s) > ClipMaxChars || !utf8.ValidString(s) {
		return ""
	}
	half := strings.Map(func(r rune) rune {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		case r == 0x3000:
			return ' '
		}
		return r
	}, s)
	if !reNetdiskShare.MatchString(half) {
		return ""
	}
	return s
}
