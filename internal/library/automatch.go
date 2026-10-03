package library

import (
	"time"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/netdisk"
)

func RunAutoMatch(st *core.Store, prog *core.Task) {
	st.Mu.RLock()
	if st.Settings.NoAutoMatch {
		st.Mu.RUnlock()
		return
	}
	bases := naming.ParseBases(st.Settings.Bases)
	now := time.Now().Unix()
	var jobs []booth.MatchJob
	consider := func(key string, a *core.Asset, name, cat string, hasCover bool) {
		u := st.User[key]
		if hasCover || (u != nil && (u.Cover != "" || u.NoBooth)) {
			return
		}
		if id, _ := booth.AssetBooth(st, key, a); id != "" {
			return
		}
		if m := st.BoothMatch[key]; m != nil && now-m.Tried < 7*86400 {
			return
		}
		if booth.BoothQueryFor(name, bases, cat) == "" {
			return
		}
		jobs = append(jobs, booth.MatchJob{Key: key, Name: name, Cat: cat})
	}
	for _, a := range st.Assets {
		hasCover := len(a.Covers) > 0
		if p := st.Purchases[a.BoothID]; p != nil && p.Cover != "" {
			hasCover = true
		}
		consider(a.Key, a, a.Name, a.Category, hasCover)
	}
	for _, key := range netdisk.PanCardKeys(st) {
		if l, _ := netdisk.PanSub(st, key); l == nil || len(l.Files) == 0 {
			continue // not read yet
		}
		v := panOnlyView(st, key)
		consider(key, nil, v.AutoName, v.Category, false)
	}
	st.Mu.RUnlock()
	if len(jobs) > 60 {
		jobs = jobs[:60]
	}
	if len(jobs) == 0 {
		return
	}
	c := core.HTTPClient(st)
	fails := 0
	for i, j := range jobs {
		prog.Set(i, len(jobs), j.Name)
		q := booth.BoothQueryFor(j.Name, bases, j.Cat)
		hits, err := booth.SearchBooth(c, q)
		m := &core.BoothMatch{Query: q, Tried: time.Now().Unix()}
		if err != nil {
			m.Err = err.Error()
			fails++
			if fails >= 3 && fails == i+1 {
				prog.Set(len(jobs), len(jobs), "无法连接 Booth，请在设置中配置代理")
				return
			}
		} else {
			m.ID = booth.ScoreHits(hits, j.Name, j.Cat, bases)
			if len(hits) > 8 {
				hits = hits[:8]
			}
			m.Hits = hits
		}
		st.Mu.Lock()
		st.BoothMatch[j.Key] = m
		st.Mu.Unlock()
		if i%6 == 5 {
			core.BumpRev()
		}
		time.Sleep(1200 * time.Millisecond)
	}
	prog.Set(len(jobs), len(jobs), "完成")
}
