package netdisk

import (
	"sort"
	"strings"
	"time"

	"vrclib/internal/core"
)

func PanFileMap(l *core.PanListing) map[string]int64 {
	out := map[string]int64{}
	if l == nil {
		return out
	}
	var walk func(fs []*core.PanFile, prefix string)
	walk = func(fs []*core.PanFile, prefix string) {
		for _, f := range fs {
			p := prefix + "/" + f.Name
			if f.Dir {
				walk(f.Children, p)
			} else {
				out[p] = f.Size
			}
		}
	}
	walk(l.Files, "")
	return out
}

// PanHasPath: is there a file at or under path ("/a/b") in a file map?
func PanHasPath(files map[string]int64, path string) bool {
	if _, ok := files[path]; ok {
		return true
	}
	for p := range files {
		if strings.HasPrefix(p, path+"/") {
			return true
		}
	}
	return false
}

// DiffPan records what changed since the previous listing (kept until the next change).
func DiffPan(old, l *core.PanListing) {
	if old == nil || len(old.Files) == 0 {
		return
	}
	a, b := PanFileMap(old), PanFileMap(l)
	var added, removed []string
	for p, sz := range b {
		if osz, ok := a[p]; !ok || osz != sz {
			added = append(added, p)
		}
	}
	for p := range a {
		if _, ok := b[p]; !ok {
			removed = append(removed, p)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		l.Added, l.Removed, l.Changed = old.Added, old.Removed, old.Changed
		return
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) > 200 {
		added = added[:200]
	}
	if len(removed) > 200 {
		removed = removed[:200]
	}
	l.Added, l.Removed, l.Changed = added, removed, time.Now().Unix()
	core.Logf("网盘分享 %s 有变化：新增 %d，删除 %d", l.Surl, len(added), len(removed))
}
