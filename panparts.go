package main

// What is inside a netdisk share, read from its file names: the product name, the Booth item id
// in a wrapping folder ("8099091/"), and one line per download ("AONAMI_manuka.zip" → Manuka,
// "AONAMI_PSD.zip" → PSD), so a share is shown the same way as a folder on disk.

import (
	"regexp"
	"strings"
)

// PanPart is one entry at the content level of a share, with what it is for.
type PanPart struct {
	Path  string   `json:"path"` // "/folder/name", as the details panel builds it
	Name  string   `json:"name"`
	Size  int64    `json:"size"`
	Dir   bool     `json:"dir,omitempty"`
	Kind  string   `json:"kind"` // variant, psd, material, doc, bonus
	Bases []string `json:"bases,omitempty"`
}

type panInfo struct {
	parts    []PanPart
	bases    []string
	wrappers []string // folders that only wrap the content ("8099091")
	name     string   // product name shared by the files ("AONAMI")
	allPSD   bool
}

var (
	rePanMat   = regexp.MustCompile(`(?i)texture|(^|[^a-z])tex([^a-z]|$)|material|matcap|贴图|材质|テクスチャ|マテリアル`)
	rePanDoc   = regexp.MustCompile(`(?i)readme|manual|説明|说明|使用方法|規約|利用規約|terms|license|ライセンス`)
	rePanBonus = regexp.MustCompile(`(?i)bonus|特典|おまけ|sample|サンプル`)
	reTokSplit = regexp.MustCompile(`[\s_\-.()（）\[\]【】+&]+`)
	reAlphaTok = regexp.MustCompile(`^[A-Za-z]{3,15}$`)
)

func panPartKind(name string) string {
	l := strings.ToLower(name)
	switch {
	case psdExt[lowerExt(name)] || rePSDName.MatchString(l):
		return "psd"
	case rePanMat.MatchString(l):
		return "material"
	case rePanDoc.MatchString(l) || lowerExt(name) == ".txt" || lowerExt(name) == ".pdf" || lowerExt(name) == ".url":
		return "doc"
	case rePanBonus.MatchString(l):
		return "bonus"
	}
	return "variant"
}

func analyzePan(l *PanListing, defs []baseDef) panInfo {
	var info panInfo
	if l == nil {
		return info
	}
	files, prefix := l.Files, ""
	for len(files) == 1 && files[0].Dir && len(files[0].Children) > 0 {
		info.wrappers = append(info.wrappers, files[0].Name)
		prefix += "/" + files[0].Name
		files = files[0].Children
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	info.name = commonChildPrefix(names)
	if info.name == "" && len(files) == 1 {
		info.name = cleanName(stripArchiveExt(files[0].Name))
	}
	// one line per entry; base bodies from the names
	var variants []int
	info.allPSD = len(files) > 0
	for _, f := range files {
		n := stripArchiveExt(f.Name)
		p := PanPart{Path: prefix + "/" + f.Name, Name: f.Name, Size: f.Size, Dir: f.Dir, Kind: panPartKind(f.Name)}
		if f.Dir {
			p.Size = panTreeSize(f)
		}
		if p.Kind != "psd" {
			info.allPSD = false
		}
		if p.Kind == "variant" {
			p.Bases = uniqStrings(append(detectBases(n, defs), forBases(n, defs)...))
			variants = append(variants, len(info.parts))
		}
		info.parts = append(info.parts, p)
	}
	// siblings named "<product>_<base>": when most of them name a known base body, the other
	// words in that position are base bodies too ("AONAMI_milfy_eku.zip" next to "AONAMI_manuka.zip")
	if len(variants) >= 2 {
		known := 0
		for _, i := range variants {
			if len(info.parts[i].Bases) > 0 {
				known++
			}
		}
		if known*2 >= len(variants) {
			for _, i := range variants {
				p := &info.parts[i]
				for _, tok := range reTokSplit.Split(strings.TrimSpace(restAfter(stripArchiveExt(p.Name), info.name)), -1) {
					lt := strings.ToLower(tok)
					if !reAlphaTok.MatchString(tok) || forStop[lt] || reGenericKey.MatchString(lt) || reStructural.MatchString(lt) ||
						panPartKind(tok) != "variant" || len(detectBases(tok, defs)) > 0 {
						continue
					}
					p.Bases = uniqStrings(append(p.Bases, canonBase(tok, defs)))
				}
			}
		}
	}
	for _, p := range info.parts {
		info.bases = append(info.bases, p.Bases...)
	}
	info.bases = uniqStrings(info.bases)
	return info
}

// restAfter: name without the product name in front ("AONAMI_milfy_eku" → "_milfy_eku").
func restAfter(name, product string) string {
	if product != "" && len(name) >= len(product) && strings.EqualFold(name[:len(product)], product) {
		return name[len(product):]
	}
	return name
}

// panBoothID: a Booth item number in the share's title, a wrapping folder or a file name.
func panBoothID(l *PanListing, info panInfo) string {
	if l == nil {
		return ""
	}
	cands := append([]string{l.Title}, info.wrappers...)
	for _, p := range info.parts {
		cands = append(cands, stripArchiveExt(p.Name))
	}
	for _, n := range cands {
		if id := boothIDFromName(n); id != "" {
			return id
		}
	}
	return ""
}

// panNames: every name in the share (bounded), for classification and base detection.
func panNames(l *PanListing, limit int) []string {
	var out []string
	var walk func(fs []*PanFile)
	walk = func(fs []*PanFile) {
		for _, f := range fs {
			if len(out) >= limit {
				return
			}
			out = append(out, f.Name)
			walk(f.Children)
		}
	}
	if l != nil {
		walk(l.Files)
	}
	return out
}
