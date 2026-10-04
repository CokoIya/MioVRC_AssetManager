package netdisk

import (
	"regexp"
	"strings"

	"vrclib/internal/cloudshare"
)

// A Google Drive or Dropbox share is kept like a Baidu share: its listing under st.Pan, keyed by its card
// key ("gd:<id>", "db:<id>"), and the card key itself is that key (a Baidu share's is "pan:<surl>"). The
// two functions here are where the difference is made; everything keyed by surl works for both.

// ShareID: the id a share link is kept under — a Baidu surl ("1xxxx"), or the key of a Google Drive /
// Dropbox share ("gd:…", "db:…"); "" when the text holds no share link.
func ShareID(link string) string {
	if s := ShareSurl(link); s != "" {
		return s
	}
	return cloudshare.KeyOf(link)
}

// CloudFirst: is the first share link of a pasted text a Google Drive or Dropbox one? (A seller's line may
// give both — "百度 … 提取码 … / Drive …": the link that comes first is the one a new card is made of.)
func CloudFirst(text string) bool {
	t := halfWidth(text)
	for _, re := range []*regexp.Regexp{reShareS, reShareInit} {
		if m := re.FindStringIndex(t); m != nil {
			t = t[:m[0]] // what stands before the Baidu link
		}
	}
	return cloudshare.Parse(t) != nil
}

// SharePlaceholder: what a share's card is called before its listing is read.
func SharePlaceholder(id string) string {
	if svc := cloudshare.ServiceOf(id); svc != "" {
		return cloudshare.Label(svc) + " 分享 " + strings.TrimPrefix(id, svc+":")[:min(8, len(id)-3)]
	}
	return "网盘分享 " + id
}

// ShareKey: the card key of a whole share, from its id.
func ShareKey(id string) string {
	if cloudshare.IsCloudKey(id) {
		return id
	}
	return "pan:" + id
}
