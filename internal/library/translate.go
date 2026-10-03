package library

import (
	"strings"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/translate"
)

// KickTranslate translates names that appeared since the last run, in the background.
// A request arriving while a run is in progress makes that run go round once more.
func KickTranslate(st *core.Store) {
	translate.TransAgain.Store(true)
	if !translate.TransBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer translate.TransBusy.Store(false)
		for translate.TransAgain.Swap(false) {
			core.RunTask(translate.TaskTrans, func() { RunTranslate(st, translate.TaskTrans, zhSources(st)) })
			_ = st.Save()
			core.BumpRev()
		}
	}()
}

// RunTranslate translates names that have no cached translation yet, in batches.
func RunTranslate(st *core.Store, prog *core.Task, sources []string) {
	st.Mu.RLock()
	if st.Settings.HideZh {
		st.Mu.RUnlock()
		return
	}
	var todo []string
	seen := map[string]bool{}
	for _, s := range sources {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		if _, ok := st.Trans[s]; !ok {
			todo = append(todo, s)
		}
	}
	st.Mu.RUnlock()
	if len(todo) == 0 {
		return
	}
	fails := 0
	for i := 0; i < len(todo); {
		// pack lines up to ~800 characters per request
		j, n := i, 0
		for j < len(todo) && (j == i || n+len([]rune(todo[j]))+1 <= 800) && j-i < 40 {
			n += len([]rune(todo[j])) + 1
			j++
		}
		batch := todo[i:j]
		prog.Set(i, len(todo), "正在翻译…")
		out, err := translate.TranslateText(st, strings.Join(batch, "\n"))
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if err == nil && len(lines) != len(batch) {
			// line structure got lost: translate one by one
			lines = lines[:0]
			for _, s := range batch {
				t, e := translate.TranslateText(st, s)
				if e != nil {
					err = e
					break
				}
				lines = append(lines, t)
				time.Sleep(300 * time.Millisecond)
			}
		}
		if err != nil || len(lines) != len(batch) {
			fails++
			core.Logf("翻译失败: %v", err)
			if fails >= 3 {
				prog.Set(len(todo), len(todo), "无法连接翻译服务，请稍后重试")
				return
			}
			time.Sleep(2 * time.Second)
			continue
		}
		st.Mu.Lock()
		for k, src := range batch {
			t := translate.ZHGlossary.Replace(strings.TrimSpace(lines[k]))
			if translate.NormCmp(t) == translate.NormCmp(src) {
				t = "" // nothing gained
			}
			st.Trans[src] = t
		}
		st.Mu.Unlock()
		core.BumpRev()
		i = j
		time.Sleep(600 * time.Millisecond)
	}
	prog.Set(len(todo), len(todo), "完成")
}
