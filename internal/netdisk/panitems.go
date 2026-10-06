package netdisk

import (
	"strings"
	"sync"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

type panItem struct {
	Path  string        // "/folder/sub/name", as the details panel builds tree paths
	Node  *core.PanFile // the folder or archive
	Hints []string      // folders above it that it was sorted under ("衣服"), outermost first
}

func PanItemKey(surl, path string) string { return ShareKey(surl) + "#" + path }

// SplitPanKey: "pan:1abc#/x/y" → ("1abc", "/x/y"); a whole share has no path.
func SplitPanKey(key string) (string, string) {
	k := strings.TrimPrefix(key, "pan:")
	if i := strings.Index(k, "#"); i >= 0 {
		return k[:i], k[i+1:]
	}
	return k, ""
}

// panIsContainer: does this folder hold several products (a category / collection folder), rather
// than being one product?
func panIsContainer(f *core.PanFile) bool {
	if !f.Dir || len(f.Children) == 0 {
		return false
	}
	n := naming.CleanName(f.Name)
	if f.Name != "" {
		if naming.BoothIDFromName(f.Name) != "" {
			return false // "8562330 辉夜 Kaguya模型": one product, named by its item number
		}
		if naming.IsCategoryWordName(n) || naming.IsGenericName(n) || naming.ReCollection.MatchString(strings.ToLower(n)) {
			return true // 衣服 / 头发 / 更新归总 / …合集
		}
	}
	var entries []string // sub-folders and archives that look like products of their own
	for _, c := range f.Children {
		ext := core.LowerExt(c.Name)
		if !c.Dir && naming.StrongFileExt[ext] && ext != ".unitypackage" {
			return false // Unity / model files right here: this is the product itself
		}
		if !c.Dir && !naming.LooseAssetExt[ext] {
			continue // pictures, readmes, links
		}
		cn := naming.CleanName(core.StripArchiveExt(c.Name))
		if naming.IsGenericName(cn) || naming.IsStructuralName(cn, "") || PanPartKind(c.Name) != "variant" {
			continue // Texture / PSD / 说明 belong to a product
		}
		entries = append(entries, c.Name)
	}
	if len(entries) < 2 {
		return false
	}
	// "AONAMI_manuka.zip", "AONAMI_sio.zip": downloads of one product
	return naming.CommonChildPrefix(entries) == ""
}

// SplitPan lists the products in a share; fewer than two means it is one product.
func SplitPan(l *core.PanListing) []panItem {
	if l == nil || len(l.Files) == 0 {
		return nil
	}
	// the share's own top level: one product with loose downloads stays whole
	root := &core.PanFile{Dir: true, Children: l.Files}
	if !(len(l.Files) == 1 && l.Files[0].Dir) && !panIsContainer(root) {
		return nil
	}
	var items []panItem
	var walk func(fs []*core.PanFile, prefix string, hints []string, depth int)
	walk = func(fs []*core.PanFile, prefix string, hints []string, depth int) {
		for _, f := range fs {
			p := prefix + "/" + f.Name
			if f.Dir {
				// a lone wrapper folder at the top ("辉夜合集-…") is looked into like a container
				wrapper := depth == 0 && len(fs) == 1 && len(f.Children) > 0
				if (panIsContainer(f) || (wrapper && panIsContainer(&core.PanFile{Dir: true, Children: f.Children}))) && depth < 6 {
					h := append(append([]string{}, hints...), f.Name)
					walk(f.Children, p, h, depth+1)
				} else if len(f.Children) > 0 || f.Partial {
					items = append(items, panItem{Path: p, Node: f, Hints: hints})
				}
				continue
			}
			if naming.LooseAssetExt[core.LowerExt(f.Name)] && PanPartKind(f.Name) != "doc" {
				items = append(items, panItem{Path: p, Node: f, Hints: hints})
			}
		}
	}
	walk(l.Files, "", nil, 0)
	return items
}

var splitCache sync.Map // surl → cachedSplit

type cachedSplit struct {
	fetched int64
	n       int
	items   []panItem
}

func SplitPanCached(l *core.PanListing) []panItem {
	if l == nil {
		return nil
	}
	if v, ok := splitCache.Load(l.Surl); ok {
		c := v.(cachedSplit)
		if c.fetched == l.Fetched && c.n == l.Count {
			return c.items
		}
	}
	items := SplitPan(l)
	splitCache.Store(l.Surl, cachedSplit{l.Fetched, l.Count, items})
	return items
}

// PanItemsFor: the products a share is shown as (nil when it is shown as one card). Caller holds st.mu.
func PanItemsFor(st *core.Store, key string) []panItem {
	surl, path := SplitPanKey(key)
	if path != "" {
		return nil
	}
	if u := st.User[key]; u != nil && u.NoSplit {
		return nil
	}
	items := SplitPanCached(st.Pan[surl])
	if len(items) < 2 {
		return nil
	}
	return items
}

// PanCardKeys: every netdisk card (whole shares and products inside split shares). Caller holds st.mu.
func PanCardKeys(st *core.Store) []string {
	var out []string
	for _, key := range core.SortedKeys(st.User) {
		if !core.IsPanShareKey(key) {
			continue
		}
		surl, _ := SplitPanKey(key)
		if items := PanItemsFor(st, key); items != nil {
			for _, it := range items {
				out = append(out, PanItemKey(surl, it.Path))
			}
			continue
		}
		out = append(out, key)
	}
	return out
}

// PanSub: the part of a share that one card stands for, as a listing of its own.
func PanSub(st *core.Store, key string) (*core.PanListing, *panItem) {
	surl, path := SplitPanKey(key)
	l := st.Pan[surl]
	if l == nil || path == "" {
		return l, nil
	}
	for _, it := range SplitPanCached(l) {
		if it.Path != path {
			continue
		}
		it := it
		sub := &core.PanListing{Surl: surl, Title: it.Node.Name, Fetched: l.Fetched, Err: l.Err}
		if it.Node.Dir {
			sub.Files = it.Node.Children
			sub.Truncated = it.Node.Partial
		} else {
			sub.Files = []*core.PanFile{it.Node}
		}
		var count func(fs []*core.PanFile)
		count = func(fs []*core.PanFile) {
			for _, f := range fs {
				if f.Dir {
					count(f.Children)
				} else {
					sub.Count++
					sub.Size += f.Size
				}
			}
		}
		count(sub.Files)
		return sub, &it
	}
	return nil, nil
}

// PanItemCategory: the category folder a product sits in ("衣服", "头发", "妆容" …), innermost first.
func PanItemCategory(hints []string) string {
	for i := len(hints) - 1; i >= 0; i-- {
		if c := naming.BracketCategory(hints[i]); c != "" {
			return c
		}
		if naming.IsCategoryWordName(hints[i]) {
			if c := naming.Classify(hints[i]); c != "其他" {
				return c
			}
		}
	}
	return ""
}
