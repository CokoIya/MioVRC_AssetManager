package naming

import (
	"regexp"
	"strings"
	"sync"
	"unicode"

	"vrclib/internal/core"
)

var (
	reForBase   = regexp.MustCompile(`(?i)(?:^|[^a-z])for[ _\-]+([a-z]{3,15})(?:[^a-z]|$)`)
	reForBaseJa = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Za-z]{3,15})[ _]?対応`)
	// words that follow "for" without being a base body
	ForStop = map[string]bool{}
)

func init() {
	for _, w := range strings.Fields(`vrchat vrc unity quest android ios pc mac liltoon poiyomi modular modularavatar ma avatar avatars
		all any common free blender mobile sdk vcc alcom test beta mmd vroid psd texture textures preview booth the your you
		men women girls boys girl boy male female use sale each every multi several various standard basic unitypackage fbx vrm
		pcss face tracking facetracking body hair windows version ver update new mesh mmd4 cluster resonite neos chilloutvr cvr
		lite light dark black white red blue pink green version2 more other others`) {
		ForStop[w] = true
	}
}

// CanonBase turns an extracted word into a base name: the table's name when it is a known base,
// otherwise the word with ordinary capitalisation ("LUMINA" → "Lumina").
func CanonBase(w string, defs []BaseDef) string {
	if hit := DetectBases(w, defs); len(hit) > 0 {
		return hit[0]
	}
	if strings.ToUpper(w) == w || strings.ToLower(w) == w {
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		return string(r)
	}
	return w
}

// ForBases finds base bodies written as "For_Milfy" / "Milfy対応" that the base table does not know.
func ForBases(text string, defs []BaseDef) []string {
	var out []string
	add := func(w string) {
		if ForStop[strings.ToLower(w)] || IsMostlyDigits(w) {
			return
		}
		out = append(out, CanonBase(w, defs))
	}
	for _, m := range reForBase.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range reForBaseJa.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	return core.UniqStrings(out)
}

type BaseDef struct {
	Name   string
	ASCII  []*regexp.Regexp
	Words  []string // ascii[i] matches words[i] as a whole word; checked with Contains first
	Others []string
}

// parsed tables are cached: they are read for every card on every refresh
var DefsCache sync.Map

func ParseBases(list []string) []BaseDef {
	key := "b\x00" + strings.Join(list, "\n")
	if v, ok := DefsCache.Load(key); ok {
		return v.([]BaseDef)
	}
	out := parseBasesNow(list)
	DefsCache.Store(key, out)
	return out
}

func parseBasesNow(list []string) []BaseDef {
	var out []BaseDef
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, aliases := item, item
		if i := strings.Index(item, "="); i > 0 {
			name, aliases = strings.TrimSpace(item[:i]), item[i+1:]
		}
		bd := BaseDef{Name: name}
		for _, al := range strings.Split(aliases+"|"+name, "|") {
			al = strings.TrimSpace(strings.ToLower(al))
			if al == "" {
				continue
			}
			if core.IsASCII(al) {
				bd.Words = append(bd.Words, al)
				bd.ASCII = append(bd.ASCII, regexp.MustCompile(`(^|[^a-z])`+regexp.QuoteMeta(al)+`([^a-z]|$)`))
			} else {
				bd.Others = append(bd.Others, al)
			}
		}
		out = append(out, bd)
	}
	return out
}

func DetectBases(text string, defs []BaseDef) []string {
	t := strings.ToLower(text)
	var out []string
	for _, d := range defs {
		hit := false
		for i, re := range d.ASCII {
			if strings.Contains(t, d.Words[i]) && re.MatchString(t) {
				hit = true
				break
			}
		}
		if !hit {
			for _, o := range d.Others {
				if strings.Contains(t, o) {
					hit = true
					break
				}
			}
		}
		if hit {
			out = append(out, d.Name)
		}
	}
	return out
}
