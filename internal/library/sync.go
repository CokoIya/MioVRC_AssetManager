package library

import (
	"regexp"
	"strings"
	"time"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/netdisk"
	"vrclib/internal/update"
)

const (
	shareRecheck = 24 * time.Hour
	// a share that is gone (or whose code is wrong) does not come back by being asked for every day
	deadShareRecheck = 7 * 24 * time.Hour
)

// dueShares: the shares to read again — not read for a day; for a week when the last answer was that the
// share is gone or its code is wrong. Caller holds st.mu.
func dueShares(st *core.Store, now time.Time) []string {
	var due []string
	for _, key := range core.SortedKeys(st.User) {
		u := st.User[key]
		if u == nil || u.ShareURL == "" || strings.Contains(key, "#") {
			continue
		}
		surl := netdisk.ShareID(u.ShareURL)
		if surl == "" {
			continue
		}
		l := st.Pan[surl]
		every := shareRecheck
		if l != nil && netdisk.PanErrSettled(l.Err) {
			every = deadShareRecheck
		}
		if l == nil || now.Sub(time.Unix(l.Fetched, 0)) > every {
			due = append(due, key)
		}
	}
	return due
}

// SyncLoop queues shares that have not been read for a day, and once a day lets the Booth step
// look for pages due for their weekly re-fetch (needsBooth), for windows left open for days.
func SyncLoop(st *core.Store) {
	time.Sleep(40 * time.Second)
	lastBooth := time.Now() // the start-up scan has just done it
	for {
		st.Mu.RLock()
		off, auto := st.Settings.NoSync, st.Settings.AutoBooth
		due := dueShares(st, time.Now())
		st.Mu.RUnlock()
		if !off && len(due) > 0 && !PanFetchPaused() {
			core.Logf("重新读取 %d 个网盘分享", len(due))
			QueuePanFetch(st, due...)
		}
		// daily, and sooner when a batch was broken off because Booth turned requests away and has had its rest
		if !off && auto && (time.Since(lastBooth) > shareRecheck || booth.ResumeDue()) && StartPipeline(st, false, false, true, false, nil) {
			lastBooth = time.Now()
		}
		time.Sleep(30 * time.Minute)
	}
}

// ---------- shares ----------

// shareNews: what changed in the part of a share a card stands for ("" path = the whole share).
func shareNews(l *core.PanListing, path string) (added, removed []string) {
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

// ---------- Booth downloads newer than the files on disk ----------

var reVerTok = regexp.MustCompile(`(?i)(^|[^a-z0-9])(v(?:er)?\.?\s*)?([0-9]+(?:[._][0-9]+)+)`)

// splitVer: "Kaguya_v1.07.zip" → ("kaguya", "1.07"); names without a version give "".
func splitVer(name string) (stem, ver string) {
	n := core.StripArchiveExt(name)
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
	stem = naming.NormKey(n[:pick[2]] + " " + n[pick[1]:])
	if len([]rune(stem)) < 3 {
		return "", ""
	}
	return stem, ver
}

// purchaseNewer: "v1.07" when a Booth download of this purchase is a later version of a file
// on disk ("Kaguya_v1.07.zip" on Booth, "Kaguya_v1.06" here); also returns the local version.
func purchaseNewer(a *core.Asset, p *core.Purchase) (string, string) {
	best, have, _ := purchaseNewerDL(a, p)
	return best, have
}

// purchaseNewerDL also says which download has the newer version (its downloadable id).
func purchaseNewerDL(a *core.Asset, p *core.Purchase) (string, string, string) {
	if a == nil || p == nil || len(p.Files) == 0 {
		return "", "", ""
	}
	local := map[string]string{}
	add := func(n string) {
		if s, v := splitVer(n); s != "" {
			if cur, ok := local[s]; !ok || update.VersionNewer(v, cur) {
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
		if lv, ok := local[s]; ok && s != "" && update.VersionNewer(v, lv) && (best == "" || update.VersionNewer(v, best)) {
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
