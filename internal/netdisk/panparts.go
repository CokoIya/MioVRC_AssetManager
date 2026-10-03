package netdisk

import (
	"regexp"
	"strings"

	"vrclib/internal/core"
	"vrclib/internal/naming"
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

type PanInfo struct {
	Parts    []PanPart
	Bases    []string
	Wrappers []string // folders that only wrap the content ("8099091")
	Name     string   // product name shared by the files ("AONAMI")
	AllPSD   bool
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
	case naming.PSDExt[core.LowerExt(name)] || naming.RePSDName.MatchString(l):
		return "psd"
	case rePanMat.MatchString(l):
		return "material"
	case rePanDoc.MatchString(l) || core.LowerExt(name) == ".txt" || core.LowerExt(name) == ".pdf" || core.LowerExt(name) == ".url":
		return "doc"
	case rePanBonus.MatchString(l):
		return "bonus"
	}
	return "variant"
}

func AnalyzePan(l *core.PanListing, defs []naming.BaseDef) PanInfo {
	var info PanInfo
	if l == nil {
		return info
	}
	files, prefix := l.Files, ""
	for len(files) == 1 && files[0].Dir && len(files[0].Children) > 0 {
		info.Wrappers = append(info.Wrappers, files[0].Name)
		prefix += "/" + files[0].Name
		files = files[0].Children
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	info.Name = naming.CommonChildPrefix(names)
	if info.Name == "" && len(files) == 1 {
		info.Name = naming.CleanName(core.StripArchiveExt(files[0].Name))
	}
	// one line per entry; base bodies from the names
	var variants []int
	info.AllPSD = len(files) > 0
	for _, f := range files {
		n := core.StripArchiveExt(f.Name)
		p := PanPart{Path: prefix + "/" + f.Name, Name: f.Name, Size: f.Size, Dir: f.Dir, Kind: panPartKind(f.Name)}
		if f.Dir {
			p.Size = panTreeSize(f)
		}
		if p.Kind != "psd" {
			info.AllPSD = false
		}
		if p.Kind == "variant" {
			p.Bases = core.UniqStrings(append(naming.DetectBases(n, defs), naming.ForBases(n, defs)...))
			variants = append(variants, len(info.Parts))
		}
		info.Parts = append(info.Parts, p)
	}
	// siblings named "<product>_<base>": when most of them name a known base body, the other
	// words in that position are base bodies too ("AONAMI_milfy_eku.zip" next to "AONAMI_manuka.zip")
	if len(variants) >= 2 {
		known := 0
		for _, i := range variants {
			if len(info.Parts[i].Bases) > 0 {
				known++
			}
		}
		if known*2 >= len(variants) {
			for _, i := range variants {
				p := &info.Parts[i]
				for _, tok := range reTokSplit.Split(strings.TrimSpace(restAfter(core.StripArchiveExt(p.Name), info.Name)), -1) {
					lt := strings.ToLower(tok)
					if !reAlphaTok.MatchString(tok) || naming.ForStop[lt] || naming.ReGenericKey.MatchString(lt) || naming.ReStructural.MatchString(lt) ||
						panPartKind(tok) != "variant" || len(naming.DetectBases(tok, defs)) > 0 {
						continue
					}
					p.Bases = core.UniqStrings(append(p.Bases, naming.CanonBase(tok, defs)))
				}
			}
		}
	}
	for _, p := range info.Parts {
		info.Bases = append(info.Bases, p.Bases...)
	}
	info.Bases = core.UniqStrings(info.Bases)
	return info
}

// restAfter: name without the product name in front ("AONAMI_milfy_eku" → "_milfy_eku").
func restAfter(name, product string) string {
	if product != "" && len(name) >= len(product) && strings.EqualFold(name[:len(product)], product) {
		return name[len(product):]
	}
	return name
}

// PanBoothID: a Booth item number in the share's title, a wrapping folder or a file name.
func PanBoothID(l *core.PanListing, info PanInfo) string {
	if l == nil {
		return ""
	}
	cands := append([]string{l.Title}, info.Wrappers...)
	for _, p := range info.Parts {
		cands = append(cands, core.StripArchiveExt(p.Name))
	}
	for _, n := range cands {
		if id := naming.BoothIDFromName(n); id != "" {
			return id
		}
	}
	return ""
}

// PanNames: every name in the share (bounded), for classification and base detection.
func PanNames(l *core.PanListing, limit int) []string {
	var out []string
	var walk func(fs []*core.PanFile)
	walk = func(fs []*core.PanFile) {
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
