package library

import (
	"errors"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/netdisk"
)

// between two shares of one round, and how long nothing is read by itself after Baidu asked for a captcha
// (twice as long every time it does so again, up to panPauseMax); tests shorten them. Under netdisk.PanMu.
var (
	panFetchGap    = 2 * time.Second
	panPauseFirst  = 30 * time.Minute
	panPauseMax    = 8 * time.Hour
	panPauseStep   time.Duration
	panPausedUntil time.Time
)

// PanFetchPaused: Baidu asked for a captcha a while ago; shares are not read again by themselves until it has
// had its rest (one the player asks for is still read).
func PanFetchPaused() bool {
	netdisk.PanMu.Lock()
	defer netdisk.PanMu.Unlock()
	return time.Now().Before(panPausedUntil)
}

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
		// PanActive is cleared where the queue is found empty, under the same lock: clearing it again here
		// would take it from a worker started since. Only a round that broke off clears it on the way out.
		ended := false
		defer func() {
			if !ended {
				netdisk.PanMu.Lock()
				netdisk.PanActive = false
				netdisk.PanMu.Unlock()
			}
		}()
		core.RunTask(netdisk.TaskPan, func() {
			done := 0
			for {
				netdisk.PanMu.Lock()
				if len(netdisk.PanQueue) == 0 {
					netdisk.PanActive = false
					ended = true
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
				surl := netdisk.ShareID(link)
				if surl == "" {
					continue
				}
				if done > 0 {
					time.Sleep(panFetchGap) // one share after the other, not all at once
				}
				netdisk.TaskPan.Set(done, done+left+1, "正在获取网盘分享")
				l, err := fetchListing(st, link, pwd)
				captcha := errors.Is(err, netdisk.ErrPanCaptcha)
				st.Mu.Lock()
				if err != nil {
					old := st.Pan[surl]
					if old == nil {
						old = &core.PanListing{Surl: surl}
						st.Pan[surl] = old
					}
					old.Err = err.Error()
					if !captcha { // after a captcha it stays due, and is read once Baidu has had its rest
						old.Fetched = time.Now().Unix()
					}
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
							st.FirstSeen[ik] = max(st.FirstSeen[netdisk.ShareKey(surl)], 1)
						}
					}
				}
				st.Mu.Unlock()
				done++
				_ = st.Save()
				core.BumpRev()
				netdisk.PanMu.Lock()
				if captcha {
					// the round ends here: every further share would be one more request Baidu counts
					panPauseStep = min(max(panPauseStep*2, panPauseFirst), panPauseMax)
					panPausedUntil = time.Now().Add(panPauseStep)
					core.Logf("百度要求输入验证码：本轮剩余的 %d 个网盘分享稍后再读取", len(netdisk.PanQueue))
					netdisk.PanQueue = nil
				} else if err == nil {
					panPauseStep = 0
				}
				netdisk.PanMu.Unlock()
				if captcha {
					netdisk.TaskPan.Set(done, done, "百度要求输入验证码，网盘分享将在稍后重新读取")
				}
			}
		})
	}()
}

// fetchPanListing: tests put their own reader here.
var fetchPanListing = netdisk.FetchPanListing
