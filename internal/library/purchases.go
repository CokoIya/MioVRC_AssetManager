package library

import (
	"os"
	"path/filepath"
	"strings"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

// gumFolders: the folders Gumroad purchases were downloaded into → the purchase. Caller holds st.mu.
func gumFolders(st *core.Store) map[string]string {
	out := map[string]string{}
	for dl, r := range st.Downloaded {
		if !core.IsGumID(dl) || r == nil || r.Path == "" || st.Purchases[r.Item] == nil {
			continue
		}
		p := r.Path
		if fi, err := os.Stat(p); err != nil {
			continue
		} else if !fi.IsDir() {
			p = filepath.Dir(p)
		}
		out[core.PathKey(p)] = r.Item
	}
	return out
}

// fileMatchKey turns a download / folder / archive name into a comparable key ("" when too generic).
func fileMatchKey(name string) string {
	base := core.StripArchiveExt(filepath.Base(strings.TrimSpace(name)))
	if ext := filepath.Ext(base); ext != "" && len(ext) <= 6 && !strings.ContainsAny(ext[1:], "0123456789") {
		base = strings.TrimSuffix(base, ext)
	}
	k := naming.NormKey(base)
	if len([]rune(k)) < 5 || naming.IsGenericName(base) || naming.IsStructuralName(base, "") {
		return ""
	}
	return k
}

func purchaseFileIndex(ps map[string]*core.Purchase) map[string]string {
	idx := map[string]string{}
	dup := map[string]bool{}
	for id, p := range ps {
		for _, f := range p.Files {
			k := fileMatchKey(f)
			if k == "" {
				continue
			}
			if old, ok := idx[k]; ok && old != id {
				dup[k] = true
			}
			idx[k] = id
		}
	}
	for k := range dup {
		delete(idx, k)
	}
	return idx
}

// ApplyPurchases links local assets without a name-given Booth id to a purchase whose download
// file has the same name. Caller holds st.mu (write).
func ApplyPurchases(st *core.Store) {
	if len(st.Purchases) == 0 {
		return
	}
	idx := purchaseFileIndex(st.Purchases)
	gum := gumFolders(st)
	for _, a := range st.Assets {
		if a.BoothID != "" && !a.BoothFromURL && !a.BoothFromLib {
			continue
		}
		// a folder this program downloaded a Gumroad purchase into
		for _, l := range a.Locations {
			if id := gum[core.PathKey(l.Path)]; id != "" && l.Kind == "dir" {
				a.BoothID, a.BoothFromURL, a.BoothFromLib = id, false, true
			}
		}
		if core.IsGumID(a.BoothID) {
			continue
		}
		if id := matchAssetPurchase(a, idx, st.Purchases); id != "" {
			a.BoothID, a.BoothFromURL, a.BoothFromLib = id, false, true
		}
	}
}

func matchAssetPurchase(a *core.Asset, idx map[string]string, ps map[string]*core.Purchase) string {
	for _, l := range a.Locations {
		if id, ok := idx[fileMatchKey(l.Path)]; ok {
			return id
		}
	}
	// packages inside the folder may be bundled dependencies; accept them only when the names agree
	for _, list := range [][]string{a.Packages, a.Archives} {
		for _, f := range list {
			id, ok := idx[fileMatchKey(f)]
			if !ok || depBoothIDs[id] {
				continue
			}
			if p := ps[id]; p != nil && namesRelated(a.Name+" "+a.RawName, p.Name+" "+filepath.Base(f)) {
				return id
			}
		}
	}
	return ""
}
