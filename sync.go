package main

// Keeping up with changes made elsewhere: netdisk shares are read again once a day and compared
// with the last listing; Booth pages are fetched again once a week (see needsBooth) and compared;
// a Booth purchase whose download carries a later version than the files on disk is pointed out.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const shareRecheck = 24 * time.Hour

// syncLoop queues shares that have not been read for a day, and once a day lets the Booth step
// look for pages due for their weekly re-fetch (needsBooth), for windows left open for days.
func syncLoop(st *Store) {
	time.Sleep(40 * time.Second)
	lastBooth := time.Now() // the start-up scan has just done it
	for {
		st.mu.RLock()
		off, auto := st.Settings.NoSync, st.Settings.AutoBooth
		var due []string
		now := time.Now()
		for _, key := range sortedKeys(st.User) {
			u := st.User[key]
			if u == nil || u.ShareURL == "" || strings.Contains(key, "#") {
				continue
			}
			surl := shareSurl(u.ShareURL)
			l := st.Pan[surl]
			if surl != "" && (l == nil || now.Sub(time.Unix(l.Fetched, 0)) > shareRecheck) {
				due = append(due, key)
			}
		}
		st.mu.RUnlock()
		if !off && len(due) > 0 {
			logf("重新读取 %d 个网盘分享", len(due))
			QueuePanFetch(st, due...)
		}
		if !off && auto && time.Since(lastBooth) > shareRecheck && StartPipeline(st, false, false, true, false, nil) {
			lastBooth = time.Now()
		}
		time.Sleep(30 * time.Minute)
	}
}

// ---------- shares ----------

func panFileMap(l *PanListing) map[string]int64 {
	out := map[string]int64{}
	if l == nil {
		return out
	}
	var walk func(fs []*PanFile, prefix string)
	walk = func(fs []*PanFile, prefix string) {
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

// panHasPath: is there a file at or under path ("/a/b") in a file map?
func panHasPath(files map[string]int64, path string) bool {
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

// diffPan records what changed since the previous listing (kept until the next change).
func diffPan(old, l *PanListing) {
	if old == nil || len(old.Files) == 0 {
		return
	}
	a, b := panFileMap(old), panFileMap(l)
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
	logf("网盘分享 %s 有变化：新增 %d，删除 %d", l.Surl, len(added), len(removed))
}

// shareNews: what changed in the part of a share a card stands for ("" path = the whole share).
func shareNews(l *PanListing, path string) (added, removed []string) {
	if l == nil || l.Changed == 0 {
		return nil, nil
	}
	in := func(p string) bool { return path == "" || p == path || strings.HasPrefix(p, path+"/") }
	for _, p := range l.Added {
		if in(p) {
			added = append(added, strings.TrimPrefix(p, path))
		}
	}
	for _, p := range l.Removed {
		if in(p) {
			removed = append(removed, strings.TrimPrefix(p, path))
		}
	}
	return
}

// ---------- Booth pages ----------

func descHash(s string) string {
	h := sha1.Sum([]byte(strings.Join(strings.Fields(s), " ")))
	return hex.EncodeToString(h[:8])
}

// boothChanges compares a fresh fetch with the stored one and notes what the shop changed.
func boothChanges(old, bi *BoothInfo) {
	if old == nil {
		return
	}
	bi.Changed, bi.ChangeNote = old.Changed, old.ChangeNote // keep an earlier, unseen notice
	if old.Name == "" || old.Ver != bi.Ver || bi.Name == "" {
		return // fetched differently: nothing to compare
	}
	var notes []string
	if old.Name != bi.Name {
		notes = append(notes, "商品名改了")
	}
	if old.Price != bi.Price && old.Price != "" && bi.Price != "" {
		notes = append(notes, fmt.Sprintf("价格 %s → %s", old.Price, bi.Price))
	}
	if descHash(old.Desc) != descHash(bi.Desc) {
		notes = append(notes, "商品说明改了")
	}
	if len(old.Images) != len(bi.Images) {
		notes = append(notes, "商品图片改了")
	}
	if len(notes) > 0 {
		bi.Changed, bi.ChangeNote = time.Now().Unix(), strings.Join(notes, "，")
	}
}

// ---------- Booth downloads newer than the files on disk ----------

var reVerTok = regexp.MustCompile(`(?i)(^|[^a-z0-9])(v(?:er)?\.?\s*)?([0-9]+(?:[._][0-9]+)+)`)

// splitVer: "Kaguya_v1.07.zip" → ("kaguya", "1.07"); names without a version give "".
func splitVer(name string) (stem, ver string) {
	n := stripArchiveExt(name)
	ms := reVerTok.FindAllStringSubmatchIndex(n, -1)
	if len(ms) == 0 {
		return "", ""
	}
	pick := ms[0]
	for _, m := range ms {
		if m[4] >= 0 { // "v1.07" wins over other numbers
			pick = m
			break
		}
	}
	ver = strings.ReplaceAll(n[pick[6]:pick[7]], "_", ".")
	stem = normKey(n[:pick[2]] + " " + n[pick[1]:])
	if len([]rune(stem)) < 3 {
		return "", ""
	}
	return stem, ver
}

// purchaseNewer: "v1.07" when a Booth download of this purchase is a later version of a file
// on disk ("Kaguya_v1.07.zip" on Booth, "Kaguya_v1.06" here); also returns the local version.
func purchaseNewer(a *Asset, p *Purchase) (string, string) {
	best, have, _ := purchaseNewerDL(a, p)
	return best, have
}

// purchaseNewerDL also says which download has the newer version (its downloadable id).
func purchaseNewerDL(a *Asset, p *Purchase) (string, string, string) {
	if a == nil || p == nil || len(p.Files) == 0 {
		return "", "", ""
	}
	local := map[string]string{}
	add := func(n string) {
		if s, v := splitVer(n); s != "" {
			if cur, ok := local[s]; !ok || versionNewer(v, cur) {
				local[s] = v
			}
		}
	}
	add(a.RawName)
	add(a.Name)
	for _, l := range a.Locations {
		add(filepathBase(l.Path))
	}
	for _, pk := range a.Packages {
		add(filepathBase(pk))
	}
	best, have, dl := "", "", ""
	for i, f := range p.Files {
		s, v := splitVer(f)
		if lv, ok := local[s]; ok && s != "" && versionNewer(v, lv) && (best == "" || versionNewer(v, best)) {
			best, have = v, lv
			if i < len(p.Downloads) {
				dl = p.Downloads[i]
			}
		}
	}
	return best, have, dl
}

func filepathBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
