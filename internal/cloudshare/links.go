package cloudshare

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// the services, as the card keys name them ("gd:<id>", "db:<id>"; a product inside a share adds "#path", as
// netdisk's "pan:" keys do)
const (
	GDrive  = "gd"
	Dropbox = "db"
)

// Link: what a pasted Google Drive or Dropbox link points at.
type Link struct {
	Service string // GDrive or Dropbox
	ID      string // Drive: the file or folder id; Dropbox: the link's id
	Folder  bool   // Drive: a folder; Dropbox: a shared folder (fetched as one zip)
	Name    string // Dropbox: the file or folder name the link carries ("" when it has none)
	RKey    string // Drive: the resourcekey some links carry; Dropbox: the rlkey of /scl/ links
	URL     string // the link as it is kept: canonical, without dl= and tracking parameters
}

// Key: the card key of the whole share.
func (l *Link) Key() string { return l.Service + ":" + l.ID }

var (
	// a Workspace account's links carry its domain ("/a/example.com/file/d/…") or "corp" ("/corp/drive/folders/…")
	reGDFile   = regexp.MustCompile(`(?i)(?:drive|docs)\.google\.com/(?:a/[^/\s]+/)?file/(?:u/\d+/)?d/([A-Za-z0-9_-]{10,})`)
	reGDFolder = regexp.MustCompile(`(?i)drive\.google\.com/(?:a/[^/\s]+/)?(?:corp/)?drive/(?:u/\d+/)?(?:mobile/)?folders/([A-Za-z0-9_-]{10,})`)
	// uc?id=…, open?id=…, folderview?id=… and the download host's download?id=…
	reGDQuery  = regexp.MustCompile(`(?i)(?:(?:drive|docs)\.google\.com/(?:a/[^/\s]+/|u/\d+/)?(uc|open|folderview|embeddedfolderview)|drive\.usercontent\.google\.com/(?:u/\d+/)?(download|uc))\?([^\s"'<>]*)`)
	reGDID     = regexp.MustCompile(`(?:^|[?&])id=([A-Za-z0-9_-]{10,})`)
	reGDRKey   = regexp.MustCompile(`(?i)[?&]resourcekey=([A-Za-z0-9_-]+)`)
	reDBLegacy = regexp.MustCompile(`(?i)dropbox(?:usercontent)?\.com/s/([A-Za-z0-9]+)(?:/([^\s?#"'<>]+))?`)
	reDBScl    = regexp.MustCompile(`(?i)dropbox(?:usercontent)?\.com/scl/(fi|fo)/([A-Za-z0-9]+)(?:/([^\s?#"'<>]*))?([^\s"'<>]*)`)
	reDBSh     = regexp.MustCompile(`(?i)dropbox(?:usercontent)?\.com/sh/([A-Za-z0-9]+)/([A-Za-z0-9_-]+)((?:/[^\s?#"'<>]*)?)`)
	reDBRLKey  = regexp.MustCompile(`(?i)[?&]rlkey=([A-Za-z0-9]+)`)
)

// halfWidth: links are pasted out of chats, where letters, digits and punctuation often come full-width.
func halfWidth(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		case r == 0x3000:
			return ' '
		}
		return r
	}, s)
}

// what the chat glues onto the end of a link: sentence punctuation, closing brackets
const trailingPunct = ".,;:!?)]}>。，、；：！？）】」』》〉"

// the brackets among them, and what opens each
var closers = map[rune]string{')': "(", ']': "[", '}': "{", '）': "（", '】': "【", '」': "「", '』': "『", '》': "《", '〉': "〈"}

// cleanName takes the chat's punctuation off the end of a name that ends its link. A bracket the name itself
// opened ("Dress (v2)") belongs to the name.
func cleanName(n string) string {
	for n != "" {
		r, size := utf8.DecodeLastRuneInString(n)
		if !strings.ContainsRune(trailingPunct, r) {
			break
		}
		if open := closers[r]; open != "" && strings.Count(n, open) >= strings.Count(n, string(r)) {
			break
		}
		n = n[:len(n)-size]
	}
	return n
}

// pathName: a name in a Dropbox link's path. Followed by more of the link ("?dl=0") it is the name exactly as
// pasted; only a name that ends the link may have the chat's punctuation glued on.
func pathName(n, rest string) string {
	if strings.HasPrefix(rest, "?") || strings.HasPrefix(rest, "#") {
		return n
	}
	return cleanName(n)
}

// Parse finds the first Google Drive or Dropbox share link in a text (nil when there is none). Google Docs,
// Sheets and Slides links are not files and are left alone.
func Parse(text string) *Link {
	// the text as pasted (its wide spaces made plain ones, so that they end a link) and in half-width
	// characters, rune for rune: a link is looked for in the second — chats send them full-width — but the
	// file name in a Dropbox link typed in plain characters is read from the first ("髪型（新）.zip" is the
	// file's name, and the link stops working when it is rewritten)
	pasted := strings.Map(func(r rune) rune {
		if r == 0x3000 {
			return ' '
		}
		return r
	}, strings.ReplaceAll(text, "&amp;", "&"))
	text = halfWidth(pasted)
	var runes []rune
	// dropbox finds a Dropbox link: where it starts in text, and the text its parts are read from with their
	// positions in it
	dropbox := func(re *regexp.Regexp) (int, string, []int) {
		m := re.FindStringSubmatchIndex(text)
		if m == nil {
			return 0, text, nil
		}
		if pasted == text {
			return m[0], text, m
		}
		if runes == nil {
			runes = []rune(pasted)
		}
		if tail := string(runes[utf8.RuneCountInString(text[:m[0]]):]); tail != text[m[0]:] {
			if pm := re.FindStringSubmatchIndex(tail); pm != nil && pm[0] == 0 { // typed in plain characters
				return m[0], tail, pm
			}
		}
		return m[0], text, m
	}
	var best *Link
	at := len(text) + 1
	consider := func(pos int, l *Link) {
		if l != nil && pos < at {
			best, at = l, pos
		}
	}
	if m := reGDFile.FindStringSubmatchIndex(text); m != nil {
		consider(m[0], gdLink(text[m[2]:m[3]], false, text[m[0]:]))
	}
	if m := reGDFolder.FindStringSubmatchIndex(text); m != nil {
		consider(m[0], gdLink(text[m[2]:m[3]], true, text[m[0]:]))
	}
	if m := reGDQuery.FindStringSubmatchIndex(text); m != nil {
		kind := strings.ToLower(group(text, m, 1) + group(text, m, 2))
		if id := reGDID.FindStringSubmatch(group(text, m, 3)); id != nil {
			l := gdLink(id[1], strings.Contains(kind, "folderview"), text[m[0]:])
			if kind == "open" {
				// "open?id=…" may be a file's or a folder's (the share's reader finds out which): the link is
				// kept in that form, which the browser opens either way
				l.URL = "https://drive.google.com/open?id=" + l.ID
				if l.RKey != "" {
					l.URL += "&resourcekey=" + l.RKey
				}
			}
			consider(m[0], l)
		}
	}
	if pos, src, m := dropbox(reDBScl); m != nil {
		kind, id, rest := strings.ToLower(group(src, m, 1)), group(src, m, 2), group(src, m, 4)
		name := pathName(group(src, m, 3), rest)
		rk := ""
		if k := reDBRLKey.FindStringSubmatch(rest); k != nil {
			rk = k[1]
		}
		u := "https://www.dropbox.com/scl/" + kind + "/" + id
		if name != "" {
			u += "/" + name
		}
		if rk != "" {
			u += "?rlkey=" + rk
		}
		shown := name[strings.LastIndex(name, "/")+1:]
		if kind == "fo" && (shown == "h" || strings.HasPrefix(name, "h/")) {
			shown = "" // "/scl/fo/<id>/h": the folder's root, without its name
		}
		consider(pos, &Link{Service: Dropbox, ID: id, Folder: kind == "fo", Name: shown, RKey: rk, URL: u})
	}
	if pos, src, m := dropbox(reDBLegacy); m != nil {
		id, name := group(src, m, 1), pathName(group(src, m, 2), src[m[1]:])
		u := "https://www.dropbox.com/s/" + id
		if name != "" {
			u += "/" + name
		}
		consider(pos, &Link{Service: Dropbox, ID: id, Name: name, URL: u})
	}
	if pos, src, m := dropbox(reDBSh); m != nil {
		id, secret := group(src, m, 1), group(src, m, 2)
		sub := strings.TrimRight(pathName(group(src, m, 3), src[m[1]:]), "/") // a folder inside the share
		consider(pos, &Link{Service: Dropbox, ID: id, Folder: true, RKey: secret, URL: "https://www.dropbox.com/sh/" + id + "/" + secret + sub})
	}
	return best
}

// group: submatch n of a FindStringSubmatchIndex result ("" when it did not take part).
func group(text string, m []int, n int) string {
	if m[2*n] < 0 {
		return ""
	}
	return text[m[2*n]:m[2*n+1]]
}

func gdLink(id string, folder bool, rest string) *Link {
	l := &Link{Service: GDrive, ID: id, Folder: folder}
	// the resourcekey of this link, not of a later one in the text
	head := rest
	if i := strings.IndexAny(head, " \t\r\n\"'<>"); i >= 0 {
		head = head[:i]
	}
	if k := reGDRKey.FindStringSubmatch(head); k != nil {
		l.RKey = k[1]
	}
	if folder {
		l.URL = "https://drive.google.com/drive/folders/" + id
	} else {
		l.URL = "https://drive.google.com/file/d/" + id + "/view"
	}
	if l.RKey != "" {
		l.URL += "?resourcekey=" + l.RKey
	}
	return l
}

// KeyOf: the card key of the share a text links to ("" when it holds no such link).
func KeyOf(text string) string {
	if l := Parse(text); l != nil {
		return l.Key()
	}
	return ""
}

// IsCloudKey: a card of a Google Drive or Dropbox share (or of a product inside one).
func IsCloudKey(key string) bool {
	return strings.HasPrefix(key, GDrive+":") || strings.HasPrefix(key, Dropbox+":")
}

// ServiceOf: the service of a card key ("" for other cards).
func ServiceOf(key string) string {
	if i := strings.Index(key, ":"); i > 0 && IsCloudKey(key) {
		return key[:i]
	}
	return ""
}

// Label: the service's name as the page shows it.
func Label(service string) string {
	switch service {
	case GDrive:
		return "Google Drive"
	case Dropbox:
		return "Dropbox"
	}
	return ""
}
