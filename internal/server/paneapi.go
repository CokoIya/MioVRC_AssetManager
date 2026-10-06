package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync/atomic"

	"vrclib/internal/cloudshare"
	"vrclib/internal/core"
	"vrclib/internal/netdisk"
	"vrclib/internal/pandl"
	"vrclib/internal/purchases"
	"vrclib/internal/webpane"
)

// registerPane: the page area — Booth, the netdisk, Gumroad and Jinxxy pages in the pane the program drives,
// and 闲鱼 in a view of its own or in the default browser (webpane/xyview.go).
func registerPane(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	boolean := func(b map[string]json.RawMessage, k string) bool {
		var v bool
		_ = json.Unmarshal(b[k], &v)
		return v
	}
	webpane.Pane.St = st
	post("/api/pane/open", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		kind := str(b, "kind")
		// only an address that reads the same here and in a browser (webpane.PageURL), and in the form it was
		// checked in: nothing a browser would take to another site than the one decided on below
		pu, err := webpane.PageURL(str(b, "url"))
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "网址无效"})
			return
		}
		u := pu.String()
		// 闲鱼 and the sites its pages lead to, whatever tab asks: never in the pane the program drives
		if kind == "xianyu" || webpane.XianyuURL(u) {
			resume := boolean(b, "resume")
			st.Mu.RLock()
			noticed := st.Settings.XyNoticed
			st.Mu.RUnlock()
			if !noticed && webpane.XyEmbedded(st) { // not before the player has read how these pages open now
				core.WriteJSON(w, map[string]any{"ok": false, "notice": true})
				return
			}
			external, err := webpane.OpenXianyu(st, u, resume)
			if err != nil {
				core.Logf("闲鱼页面打不开 %s: %v", hostOf(u), err)
				core.WriteJSON(w, map[string]any{"ok": false, "err": "页面无法打开：" + err.Error()})
				return
			}
			core.WriteJSON(w, map[string]any{"ok": true, "external": external, "xy": true, "mode": webpane.PaneMode()})
			return
		}
		if webpane.PaneMode() == "" {
			core.WriteJSON(w, map[string]any{"ok": core.OpenURL(u) == nil, "external": true})
			return
		}
		if err := webpane.Pane.Open(u, kind, true); err != nil {
			core.Logf("页面打不开 %s: %v", u, err)
			core.WriteJSON(w, map[string]any{"ok": false, "err": "页面无法打开：" + err.Error()})
			return
		}
		if kind == "pan" && !pandl.CurrentBaiduAccount().LoggedIn {
			pandl.WatchBaiduLogin(st) // a netdisk page: once the player logs in there, downloads can use it
		}
		if kind == "gumroad" && purchases.LoadGumSession() == nil {
			purchases.WatchGumroadLogin(st) // the Gumroad login page: once the player is in, the purchases are read
		}
		core.WriteJSON(w, map[string]any{"ok": true, "mode": webpane.PaneMode()})
	})
	post("/api/pane/place", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var r struct {
			X, Y, W, H, DPR float64
			Show, Xy        bool
		}
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &r)
		webpane.PlacePage(r.X, r.Y, r.W, r.H, r.DPR, r.Show, r.Xy)
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/pane/state", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, webpane.PageState())
	})
	// xy: the page means 闲鱼's own view (as with place): a button pressed while the other view still has the page
	// area does nothing
	post("/api/pane/act", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		act, xy := str(b, "act"), boolean(b, "xy")
		if act == "external" {
			u, err := webpane.PageAct("url", xy)
			if err != nil || u == "" {
				core.WriteJSON(w, map[string]any{"ok": false, "err": "当前未打开任何页面"})
				return
			}
			core.WriteJSON(w, map[string]any{"ok": core.OpenURL(u) == nil})
			return
		}
		text, err := webpane.PageAct(act, xy)
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "text": text})
	})

	// ---------- 闲鱼: how its pages open; and the links the player copies ----------
	// external: in the default browser; noticed: the note about these pages has been read; clip: offer the 闲鱼,
	// Booth and netdisk links the player copies (on every tab). Only what is given is changed.
	post("/api/xy/prefs", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		_, ext := b["external"]
		_, seen := b["noticed"]
		_, clip := b["clip"]
		st.Mu.Lock()
		was := st.Settings.XyExternal
		if ext {
			st.Settings.XyExternal = boolean(b, "external")
		}
		if seen {
			st.Settings.XyNoticed = boolean(b, "noticed")
		}
		if clip {
			st.Settings.NoXyClip = !boolean(b, "clip")
		}
		now := st.Settings.XyExternal
		st.Mu.Unlock()
		if now && !was {
			webpane.XyLeave() // the view inside the window lets go of the page it had
			core.Logf("闲鱼页面：改为在默认浏览器中打开")
		} else if was && !now {
			core.Logf("闲鱼页面：改为在软件内打开")
		}
		_ = st.Save()
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true, "xyMode": webpane.XyMode(st)})
	})
	// The clipboard, for one thing: a link the player copied, for the page to offer on whatever tab it shows — a
	// netdisk share a card can be made of (shareAddable), to add; a 闲鱼 or Booth page (clipLink), to open. seq is
	// the clipboard's number the page saw last; a link comes only when the number has moved on since. A share is
	// passed on as the text it came in (kind "share", text), a page as its address alone (kind "xianyu" or
	// "booth", url). Anything else on the clipboard is not passed on, and nothing of it is kept or logged.
	// The clipboard is looked at only while the program is the one in front (focus: the page says it has the focus
	// — the interface in a browser), so what the player copies in other programs meanwhile is not read as it comes;
	// and not at all when the player has turned the offer off. front: this look counted as one in front.
	// A 闲鱼 or Booth page is offered only when the player comes back with it from another program — the clipboard
	// has changed while this one was not in front (clipAway; away: the page had not been asking for a while, as a
	// minimised window does not, so that leaving was not seen here). One copied in the program's own pages is
	// where the player already is. And only where the program has a page of its own to open it in (闲鱼's view,
	// the pane): a link that would go to the default browser is nothing to offer.
	// read: the player pressed 「收录网盘链接」 — what is there now, whenever it was copied: a share, as before. A
	// share on a netdisk the program cannot add is not passed on then either; the page is told that there is one
	// (other), to say so.
	post("/api/clip/share", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var last uint32
		_ = json.Unmarshal(b["seq"], &last)
		read, back := boolean(b, "read"), false
		if !read {
			st.Mu.RLock()
			off := st.Settings.NoXyClip
			st.Mu.RUnlock()
			if off {
				clipAway.Store(false)
				core.WriteJSON(w, map[string]any{"ok": true, "seq": 0, "front": false})
				return
			}
			if !core.AppInFront() && !boolean(b, "focus") { // asked again, with the same number, when the player is back
				clipAway.Store(true)
				core.WriteJSON(w, map[string]any{"ok": true, "seq": last, "front": false})
				return
			}
			back = clipAway.Swap(false) || boolean(b, "away")
		}
		seq := core.ClipboardSeq()
		res := map[string]any{"ok": true, "seq": seq}
		if !read {
			res["front"] = true
		}
		if read || (last != 0 && seq != 0 && seq != last) {
			t, ok, busy := core.ClipboardText()
			if busy && !read {
				res["seq"] = last // another program is holding the clipboard: looked at again in a moment
				if back {
					clipAway.Store(true) // … as the look after coming back that this one was
				}
			}
			if ok {
				switch s := core.NetdiskShareText(t); {
				case s != "" && shareAddable(s):
					res["text"] = s
					if !read {
						res["kind"] = "share"
					}
				case s != "" && read:
					res["other"] = true
				case back:
					if kind, u := clipLink(t); (kind == "xianyu" && webpane.XyEmbedded(st)) || (kind == "booth" && webpane.PaneMode() != "") {
						res["kind"], res["url"] = kind, u
					}
				}
			}
		}
		core.WriteJSON(w, res)
	})
}

// clipAway: the page has asked about the clipboard while the program was not the one in front, and has not
// asked from the front since: the player is away, or just back.
var clipAway atomic.Bool

// shareAddable: a card can be made of this text — it holds a share link the way 「添加网盘素材」 takes one: of
// Baidu Netdisk (/api/pan/add), or of Google Drive or Dropbox (/api/cloud/add). A share on another netdisk
// (夸克, 阿里云盘, 123 云盘, 蓝奏云) is one the program has no card for: offering it would lead to a refusal.
func shareAddable(text string) bool {
	return netdisk.ShareSurl(text) != "" || cloudshare.Parse(text) != nil
}

// hostOf: the site of an address, for the log (an address can carry a search or a token).
func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return "?"
}
