package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"vrclib/internal/cloudshare"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/netdisk"
)

// registerCloudAPI: Google Drive and Dropbox shares. A card of one is a netdisk card with another key
// ("gd:…", "db:…"): its listing, downloads and change notices go through the /api/pan/* routes.
func registerCloudAPI(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	// the share link (or the chat text around it) makes a card, and the share is read in the background
	post("/api/cloud/add", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		l := cloudshare.Parse(str(b, "url"))
		if l == nil || !netdisk.CloudFirst(str(b, "url")) { // a Baidu link before it: the text is that share's
			core.WriteJSON(w, map[string]any{"ok": false, "err": "未识别到 Google Drive 或 Dropbox 分享链接"})
			return
		}
		key := l.Key()
		st.Mu.Lock()
		if u := st.User[key]; u == nil {
			st.User[key] = &core.UserData{ShareURL: l.URL, Updated: time.Now().Unix(), Name: strings.TrimSpace(str(b, "name"))}
			if st.FirstSeen[key] == 0 {
				st.FirstSeen[key] = time.Now().Unix()
			}
		} else if old := cloudshare.Parse(u.ShareURL); l.RKey != "" && (old == nil || old.RKey != l.RKey) {
			// the card is there, made of a link without its key (Dropbox's rlkey, Drive's resourcekey) or with
			// another: the link pasted now is the one that opens the share — a cloud card has no link field to
			// mend it in
			u.ShareURL, u.Updated = l.URL, time.Now().Unix()
		}
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		library.QueuePanFetch(st, key)
		core.WriteJSON(w, map[string]any{"ok": true, "key": key, "service": l.Service})
	})
}
