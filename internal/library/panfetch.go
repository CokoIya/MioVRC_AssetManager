package library

import (
	"time"

	"vrclib/internal/core"
	"vrclib/internal/netdisk"
)

// QueuePanFetch reads the share links of the given assets in the background (one at a time).
func QueuePanFetch(st *core.Store, keys ...string) {
	netdisk.PanMu.Lock()
	for _, k := range keys {
		if !core.ContainsStr(netdisk.PanQueue, k) {
			netdisk.PanQueue = append(netdisk.PanQueue, k)
		}
	}
	if netdisk.PanActive {
		netdisk.PanMu.Unlock()
		return
	}
	netdisk.PanActive = true
	netdisk.PanMu.Unlock()
	go func() {
		defer func() {
			netdisk.PanMu.Lock()
			netdisk.PanActive = false
			netdisk.PanMu.Unlock()
		}()
		core.RunTask(netdisk.TaskPan, func() {
			done := 0
			for {
				netdisk.PanMu.Lock()
				if len(netdisk.PanQueue) == 0 {
					netdisk.PanActive = false
					netdisk.PanMu.Unlock()
					// new share titles: look them up on Booth (covers) and translate them
					st.Mu.RLock()
					auto := st.Settings.AutoBooth
					st.Mu.RUnlock()
					if !auto || !StartPipeline(st, false, false, true, false, nil) {
						KickTranslate(st)
					}
					return
				}
				k := netdisk.PanQueue[0]
				netdisk.PanQueue = netdisk.PanQueue[1:]
				left := len(netdisk.PanQueue)
				netdisk.PanMu.Unlock()
				st.Mu.RLock()
				u := st.User[k]
				var link, pwd string
				if u != nil {
					link, pwd = u.ShareURL, u.SharePwd
				}
				st.Mu.RUnlock()
				surl := netdisk.ShareSurl(link)
				if surl == "" {
					continue
				}
				netdisk.TaskPan.Set(done, done+left+1, "读取网盘分享")
				l, err := netdisk.FetchPanListing(st, link, pwd)
				st.Mu.Lock()
				if err != nil {
					old := st.Pan[surl]
					if old == nil {
						old = &core.PanListing{Surl: surl}
						st.Pan[surl] = old
					}
					old.Err, old.Fetched = err.Error(), time.Now().Unix()
				} else {
					prev := st.Pan[surl]
					netdisk.DiffPan(prev, l)
					st.Pan[surl] = l
					// products of a collection seen for the first time; ones that were already in
					// the share (read by an older version) are as old as the share
					now, had := time.Now().Unix(), netdisk.PanFileMap(prev)
					for _, it := range netdisk.SplitPan(l) {
						ik := netdisk.PanItemKey(surl, it.Path)
						if st.FirstSeen[ik] != 0 {
							continue
						}
						st.FirstSeen[ik] = now
						if netdisk.PanHasPath(had, it.Path) {
							st.FirstSeen[ik] = max(st.FirstSeen["pan:"+surl], 1)
						}
					}
				}
				st.Mu.Unlock()
				done++
				_ = st.Save()
				core.BumpRev()
			}
		})
	}()
}
