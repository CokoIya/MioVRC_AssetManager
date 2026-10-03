package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"vrclib"
	"vrclib/internal/ai"
	"vrclib/internal/archive"
	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/naming"
	"vrclib/internal/netdisk"
	"vrclib/internal/pandl"
	"vrclib/internal/purchases"
	"vrclib/internal/translate"
	"vrclib/internal/unity"
	"vrclib/internal/update"
	"vrclib/internal/webpane"
)

var (
	apiToken string
	LastPing atomic.Int64
	EverPing atomic.Bool
)

func init() {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	apiToken = hex.EncodeToString(b)
}

func effectiveBoothID(a *core.Asset, u *core.UserData) string {
	if u != nil && u.BoothURL != "" {
		if m := naming.ReBoothURL.FindStringSubmatch(u.BoothURL); m != nil {
			return m[1]
		}
	}
	return a.BoothID
}

type stateResp struct {
	Rev        int64               `json:"rev"`
	Version    string              `json:"version"`
	Assets     []library.AssetView `json:"assets"`
	Settings   core.Settings       `json:"settings"`
	Projects   []core.ProjectInfo  `json:"projects"`
	Warnings   []string            `json:"warnings"`
	LastScan   int64               `json:"lastScan"`
	LastUsage  int64               `json:"lastUsage"`
	Tasks      []core.Task         `json:"tasks"`
	Busy       bool                `json:"busy"`
	Categories []string            `json:"categories"`
	Overrides  map[string]string   `json:"overrides"`
	DataDir    string              `json:"dataDir"`

	SetupNeeded    bool     `json:"setupNeeded"`
	PurchaseSync   int64    `json:"purchaseSync"`
	PurchaseCount  int      `json:"purchaseCount"`
	PurchaseBusy   bool     `json:"purchaseBusy"`
	CanShortcut    bool     `json:"canShortcut"`
	DefaultBrowser string   `json:"defaultBrowser"`
	StyleNames     []string `json:"styleNames"`

	Update      *core.UpdateInfo `json:"update,omitempty"`
	UpdateNewer bool             `json:"updateNewer"`
	UpdatedFrom string           `json:"updatedFrom,omitempty"`
	Releases    string           `json:"releases"`
	AppName     string           `json:"appName"`

	Changelog []update.ChangeEntry `json:"changelog"`

	ShopCats    []string                 `json:"shopCats"`
	Downloads   []purchases.DLJob        `json:"downloads"`
	DLNeedLogin bool                     `json:"dlNeedLogin"`
	DLDir       string                   `json:"dlDir"`
	BoothLogin  bool                     `json:"boothLogin"`         // a saved Booth login exists
	WhatsNew    []update.ChangeEntry     `json:"whatsNew,omitempty"` // shown once after an update
	PaneMode    string                   `json:"paneMode"`           // how Booth / 闲鱼 pages open: "native", "window" or "" (system browser)
	BoothWeb    string                   `json:"boothWeb"`           // https://booth.pm (tests: a local server)
	BoothAcc    string                   `json:"boothAccounts"`
	XYBase      string                   `json:"xyBase"` // https://www.goofish.com
	Import      *unity.ImportJob         `json:"importJob,omitempty"`
	NewProject  *unity.NewProjectJob     `json:"newProject,omitempty"`
	ArcTools    []string                 `json:"arcTools"` // archive programs found (the player's default first)
	PanJobs     []pandl.PanJob           `json:"panJobs"`
	Baidu       pandl.BaiduAccount       `json:"baidu"`
	Gumroad     purchases.GumroadAccount `json:"gumroad"`
	BaiduLogin  string                   `json:"baiduLogin"` // the login page
	PanWeb      string                   `json:"panWeb"`     // https://pan.baidu.com (tests: a local server)
}

func tasksSnapshot() []core.Task {
	return []core.Task{core.TaskScan.Snapshot(), core.TaskUsage.Snapshot(), booth.TaskMatch.Snapshot(), core.TaskBooth.Snapshot(), translate.TaskTrans.Snapshot(),
		purchases.TaskPurchase.Snapshot(), purchases.TaskGumroad.Snapshot(), netdisk.TaskPan.Snapshot(), update.TaskUpdate.Snapshot(), purchases.TaskDownload.Snapshot(), pandl.TaskPanDL.Snapshot(), unity.TaskImport.Snapshot(), unity.TaskNP.Snapshot()}
}

func NewMux(st *core.Store) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		b, err := vrclib.WebFS.ReadFile("web/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.HasSuffix(name, ".html"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			b = []byte(strings.ReplaceAll(string(b), "__TOKEN__", apiToken))
		case strings.HasSuffix(name, ".js"):
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case strings.HasSuffix(name, ".css"):
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case strings.HasSuffix(name, ".svg"):
			w.Header().Set("Content-Type", "image/svg+xml")
		}
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		LastPing.Store(time.Now().Unix())
		EverPing.Store(true)
		core.WriteJSON(w, map[string]any{"app": "vrclib", "rev": core.CurRev()})
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		st.Mu.RLock()
		resp := stateResp{Rev: core.CurRev(), Version: core.AppVersion, Settings: st.Settings, Projects: st.Projects,
			Warnings: st.Warnings, LastScan: st.LastScan, LastUsage: st.LastUsage, Categories: library.Categories,
			Overrides: st.Overrides, DataDir: core.DataDir, SetupNeeded: !st.Settings.SetupDone,
			PurchaseSync: st.PurchaseSync, PurchaseBusy: core.PurchaseBusy.Load(),
			CanShortcut: runtime.GOOS == "windows"}
		resp.Gumroad = purchases.CurrentGumroadAccount(st)
		resp.PurchaseCount = len(st.Purchases) - resp.Gumroad.Count
		resp.Assets = library.AllViews(st)
		resp.StyleNames = library.StyleNames(st.Settings.Styles)
		resp.Update = st.Update
		resp.UpdateNewer = st.Update != nil && update.VersionNewer(st.Update.Version, core.AppVersion)
		resp.UpdatedFrom, resp.Releases, resp.AppName = core.UpdatedFrom, update.ReleasesPage(), core.AppName
		resp.Changelog = update.Changelog()
		resp.DLDir = purchases.DownloadDir(st)
		for _, c := range library.ShopCats {
			resp.ShopCats = append(resp.ShopCats, c.Label)
		}
		if st.Settings.SetupDone {
			resp.WhatsNew = update.WhatsNew(st)
		}
		st.Mu.RUnlock()
		resp.DefaultBrowser = webpane.BrowserLabel(core.DefaultBrowserExe())
		resp.Downloads, resp.DLNeedLogin, resp.BoothLogin = purchases.DLSnapshot(), purchases.DLNeedLogin(), len(purchases.LoadBoothSession()) > 0
		resp.PaneMode, resp.BoothWeb, resp.BoothAcc, resp.XYBase = webpane.PaneMode(), core.BoothWebBase(), core.BoothAccountsBase(), webpane.XianyuBase()
		resp.Import = unity.ImportSnapshot()
		resp.NewProject = unity.NewProjectSnapshot()
		resp.PanJobs, resp.Baidu, resp.BaiduLogin, resp.PanWeb = pandl.PanJobsSnapshot(), pandl.CurrentBaiduAccount(), pandl.BaiduLoginURL(), core.PanBase()
		for _, t := range archive.ArchiveTools() {
			if t.Kind != "tar" && !core.ContainsStr(resp.ArcTools, t.Name) {
				resp.ArcTools = append(resp.ArcTools, t.Name)
			}
		}
		resp.Tasks = tasksSnapshot()
		resp.Busy = library.PipelineBusy()
		core.WriteJSON(w, resp)
	})
	mux.HandleFunc("/api/progress", func(w http.ResponseWriter, r *http.Request) {
		LastPing.Store(time.Now().Unix())
		core.WriteJSON(w, map[string]any{"rev": core.CurRev(), "tasks": tasksSnapshot(), "busy": library.PipelineBusy(), "purchaseBusy": core.PurchaseBusy.Load(),
			"downloads": purchases.DLSnapshot(), "dlNeedLogin": purchases.DLNeedLogin(), "importJob": unity.ImportSnapshot(), "newProject": unity.NewProjectSnapshot(), "panJobs": pandl.PanJobsSnapshot()})
	})
	mux.HandleFunc("/thumb", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("p")
		wd, _ := strconv.Atoi(r.URL.Query().Get("w"))
		if wd <= 0 || wd > 1600 {
			wd = 420
		}
		if !imageAllowed(st, p) {
			http.Error(w, "forbidden", 403)
			return
		}
		w.Header().Set("Cache-Control", "max-age=86400")
		if t := library.Thumbnail(p, wd); t != "" {
			http.ServeFile(w, r, t)
			return
		}
		http.ServeFile(w, r, p) // webp etc.: let the browser decode the original
	})

	mux.HandleFunc("/shot", ai.ServeShot) // the screenshots of the AI's steps

	post := func(path string, h func(w http.ResponseWriter, body map[string]json.RawMessage)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.Header.Get("X-Token") != apiToken {
				http.Error(w, "forbidden", 403)
				return
			}
			body := map[string]json.RawMessage{}
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body)
			h(w, body)
		})
	}
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

	post("/api/open", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p := str(b, "path")
		fi, err := os.Stat(p)
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "路径不存在：" + p})
			return
		}
		err = core.OpenInExplorer(filepath.Clean(p), fi.IsDir())
		core.WriteJSON(w, map[string]any{"ok": err == nil})
	})
	post("/api/openurl", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		u := str(b, "url")
		mail := strings.HasPrefix(strings.ToLower(u), "mailto:"+strings.ToLower(feedbackMail))
		// a unityhub:// link installs the Unity version VRChat wants; it needs Unity Hub on this computer
		hub := strings.HasPrefix(u, "unityhub://")
		if hub && unity.FindUnityHub() == "" {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "这台电脑上没找到 Unity Hub：先装 Unity Hub（unity.com/download），再来点这里装 Unity"})
			return
		}
		if !(strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") || mail || hub) {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "不是网址"})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": core.OpenURL(u) == nil})
	})
	post("/api/user", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		key := str(b, "key")
		if boolean(b, "delete") && strings.HasPrefix(key, "pan:") {
			st.Mu.Lock()
			delete(st.User, key)
			delete(st.BoothMatch, key)
			st.Mu.Unlock()
			_ = st.Save()
			core.BumpRev()
			core.WriteJSON(w, map[string]any{"ok": true})
			return
		}
		var u core.UserData
		if err := json.Unmarshal(b["user"], &u); err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		u.Updated = time.Now().Unix()
		u.Tags = core.CleanList(u.Tags)
		if u.Bases != nil {
			u.Bases = core.CleanList(u.Bases)
		}
		u.Styles = core.CleanList(u.Styles)
		if !u.StylesSet {
			u.Styles = nil
		}
		u.ShareURL, u.SharePwd = strings.TrimSpace(u.ShareURL), strings.TrimSpace(u.SharePwd)
		if strings.HasPrefix(key, "pan:") && strings.Contains(key, "#") {
			u.ShareURL, u.SharePwd, u.PanPath = "", "", "" // a product inside a share: the link stays with the share
		}
		st.Mu.Lock()
		old := st.User[key]
		if old != nil { // marks kept by the server, whatever an older copy in the window says
			u.PanSeen, u.BoothSeen = max(u.PanSeen, old.PanSeen), max(u.BoothSeen, old.BoothSeen)
			u.Downloaded, u.DownloadDir = old.Downloaded, old.DownloadDir
			u.PanCopy, u.PanSaved, u.PanGot = old.PanCopy, old.PanSaved, old.PanGot
		}
		st.User[key] = &u
		panChanged := netdisk.ShareSurl(u.ShareURL) != "" && (old == nil || old.ShareURL != u.ShareURL || old.SharePwd != u.SharePwd ||
			st.Pan[netdisk.ShareSurl(u.ShareURL)] == nil)
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true})
		if panChanged {
			library.QueuePanFetch(st, key)
		} else if u.Name != "" && (old == nil || old.Name != u.Name) {
			library.KickTranslate(st)
		}
		// a newly entered booth link → fetch its info
		if u.BoothURL != "" && naming.ReBoothURL.MatchString(u.BoothURL) && (old == nil || old.BoothURL != u.BoothURL) {
			library.StartPipeline(st, false, false, true, false, []string{key})
		}
	})
	// several cards at once: move them to a category, hide or show them, or stop listing them
	post("/api/user/bulk", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var keys []string
		_ = json.Unmarshal(b["keys"], &keys)
		if len(keys) == 0 {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "还没有选素材"})
			return
		}
		var category *string
		var hidden *bool
		if raw, ok := b["category"]; ok {
			var c string
			if json.Unmarshal(raw, &c) == nil {
				category = &c
			}
		}
		if raw, ok := b["hidden"]; ok {
			var h bool
			if json.Unmarshal(raw, &h) == nil {
				hidden = &h
			}
		}
		ignore := boolean(b, "ignore")
		st.Mu.Lock()
		if category != nil && *category != "" && !core.ContainsStr(library.Categories, *category) {
			st.Mu.Unlock()
			core.WriteJSON(w, map[string]any{"ok": false, "err": "没有这个分类：" + *category})
			return
		}
		byKey := map[string]*core.Asset{}
		for _, a := range st.Assets {
			byKey[a.Key] = a
		}
		n, ignored := 0, 0
		now := time.Now().Unix()
		for _, key := range keys {
			a := byKey[key]
			if ignore {
				if a == nil || len(a.Locations) == 0 {
					continue // not a folder on disk: nothing for the scan to skip
				}
				p := a.Locations[0].Path
				for _, l := range a.Locations {
					if l.Kind == "dir" {
						p = l.Path
						break
					}
				}
				st.Overrides[core.PathKey(p)] = "ignore"
				ignored++
				continue
			}
			u := st.User[key]
			if u == nil {
				if a == nil && !strings.HasPrefix(key, "pan:") && !strings.HasPrefix(key, "purchase:") {
					continue
				}
				u = &core.UserData{}
				st.User[key] = u
			}
			if category != nil {
				u.Category = *category
			}
			if hidden != nil {
				u.Hidden = *hidden
			}
			u.Updated = now
			n++
		}
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		if ignored > 0 {
			library.StartPipeline(st, true, true, false, false, nil)
		}
		core.WriteJSON(w, map[string]any{"ok": true, "n": n, "ignored": ignored})
	})
	post("/api/scan", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		ok := library.StartPipeline(st, boolean(b, "scan"), boolean(b, "usage"), boolean(b, "booth"), boolean(b, "force"), nil)
		core.WriteJSON(w, map[string]any{"ok": ok, "busy": !ok})
	})
	post("/api/booth", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var keys []string
		_ = json.Unmarshal(b["keys"], &keys)
		ok := library.StartPipeline(st, false, false, true, boolean(b, "force"), keys)
		core.WriteJSON(w, map[string]any{"ok": ok, "busy": !ok})
	})
	post("/api/settings", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var s core.Settings
		if err := json.Unmarshal(b["settings"], &s); err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		s.Roots, s.ProjectRoots, s.Bases = core.CleanPaths(s.Roots), core.CleanPaths(s.ProjectRoots), core.CleanList(s.Bases)
		if len(s.Bases) == 0 {
			s.Bases = core.DefaultSettings().Bases
		}
		s.Styles = core.CleanList(s.Styles)
		if s.Styles == nil {
			s.Styles = []string{}
		}
		s.SetupDone = true
		st.Mu.Lock()
		st.Settings = s
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		if boolean(b, "noRescan") { // only display settings or style tags changed
			library.KickTranslate(st)
			core.WriteJSON(w, map[string]any{"ok": true, "started": false})
			return
		}
		ok := library.StartPipeline(st, true, true, s.AutoBooth, false, nil)
		core.WriteJSON(w, map[string]any{"ok": true, "started": ok})
	})
	// ---------- feedback ----------
	post("/api/feedback/info", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "info": feedbackInfo(st), "mail": feedbackMail})
	})
	post("/api/feedback", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		note, err := SendFeedback(st, str(b, "kind"), str(b, "text"), str(b, "contact"), boolean(b, "withInfo"))
		if err != nil {
			var in fbInputError
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error(), "mail": feedbackMail, "input": errors.As(err, &in)})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "note": note})
	})
	// the "what changed" window has been shown for this version
	post("/api/whatsnew/seen", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		st.Mu.Lock()
		if st.NotesSeen == "" || update.VersionNewer(core.AppVersion, st.NotesSeen) {
			st.NotesSeen = core.AppVersion
		}
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// "知道了" on a change notice
	post("/api/seen", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		key, what := str(b, "key"), str(b, "what")
		st.Mu.Lock()
		u := st.User[key]
		if u == nil {
			u = &core.UserData{}
			st.User[key] = u
		}
		now := time.Now().Unix()
		switch what {
		case "pan":
			u.PanSeen = now
		case "booth":
			u.BoothSeen = now
		}
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// ---------- updates ----------
	post("/api/update/check", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		info, err := update.CheckUpdate(st, boolean(b, "force"))
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error(), "current": core.AppVersion})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "current": core.AppVersion, "update": info, "newer": update.VersionNewer(info.Version, core.AppVersion)})
	})
	post("/api/update/apply", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if err := update.ApplyUpdate(st); err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/update/skip", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		st.Mu.Lock()
		st.Settings.SkipVersion = str(b, "version")
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// the outfit style table on its own (adding a tag from the details panel): no rescan needed
	post("/api/styles", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var list []string
		if err := json.Unmarshal(b["styles"], &list); err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		list = core.CleanList(list)
		if list == nil {
			list = []string{}
		}
		st.Mu.Lock()
		st.Settings.Styles = list
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/override", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, mode := str(b, "path"), str(b, "mode")
		st.Mu.Lock()
		if mode == "" {
			delete(st.Overrides, core.PathKey(p))
		} else {
			st.Overrides[core.PathKey(p)] = mode
		}
		st.Mu.Unlock()
		_ = st.Save()
		ok := library.StartPipeline(st, true, true, false, false, nil)
		core.WriteJSON(w, map[string]any{"ok": true, "started": ok})
	})
	mux.HandleFunc("/rthumb", func(w http.ResponseWriter, r *http.Request) {
		p, err := booth.RemoteThumb(st, r.URL.Query().Get("u"))
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Cache-Control", "max-age=604800")
		http.ServeFile(w, r, p)
	})
	post("/api/boothsearch", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		hits, err := booth.SearchBooth(core.HTTPClient(st), str(b, "q"))
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		if name := str(b, "name"); name != "" {
			st.Mu.RLock()
			bases := naming.ParseBases(st.Settings.Bases)
			st.Mu.RUnlock()
			booth.ScoreHits(hits, name, str(b, "cat"), bases)
		}
		if len(hits) > 24 {
			hits = hits[:24]
		}
		core.WriteJSON(w, map[string]any{"ok": true, "hits": hits})
	})
	post("/api/translate", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		t, err := translate.TranslateLong(st, str(b, "text"))
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "翻译失败：" + err.Error()})
			return
		}
		_ = st.Save()
		core.WriteJSON(w, map[string]any{"ok": true, "text": t})
	})
	post("/api/pan/add", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		link, pwd := strings.TrimSpace(str(b, "url")), strings.TrimSpace(str(b, "pwd"))
		surl := netdisk.ShareSurl(link)
		if surl == "" {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "没认出百度网盘分享链接"})
			return
		}
		if pwd == "" {
			pwd = netdisk.SharePwdFromURL(link)
		}
		key := "pan:" + surl
		st.Mu.Lock()
		if st.User[key] == nil {
			st.User[key] = &core.UserData{ShareURL: "https://pan.baidu.com/s/" + surl, SharePwd: pwd, Updated: time.Now().Unix(),
				Name: strings.TrimSpace(str(b, "name"))}
			if st.FirstSeen[key] == 0 {
				st.FirstSeen[key] = time.Now().Unix()
			}
		}
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		library.QueuePanFetch(st, key)
		core.WriteJSON(w, map[string]any{"ok": true, "key": key})
	})
	post("/api/pan/refresh", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		library.QueuePanFetch(st, str(b, "key"))
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/detect", func(w http.ResponseWriter, r *http.Request) {
		roots, projects := core.DetectCandidates()
		core.WriteJSON(w, map[string]any{"roots": roots, "projects": projects})
	})
	post("/api/pickfolder", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, err := core.PickFolder(str(b, "title"), str(b, "initial"))
		switch {
		case errors.Is(err, core.ErrPickCancelled):
			core.WriteJSON(w, map[string]any{"ok": false, "cancelled": true})
		case err != nil:
			core.WriteJSON(w, map[string]any{"ok": false, "err": "打不开选择窗口，请直接粘贴路径：" + err.Error()})
		default:
			core.WriteJSON(w, map[string]any{"ok": true, "path": p})
		}
	})
	post("/api/shortcut", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, err := core.CreateDesktopShortcut()
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "path": p})
	})
	// ---------- Gumroad purchases ----------
	post("/api/gumroad/sync", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if purchases.LoadGumSession() == nil {
			core.WriteJSON(w, map[string]any{"ok": false, "login": true, "err": "还没有登录 Gumroad"})
			return
		}
		ok := purchases.StartGumroadSync(st)
		core.WriteJSON(w, map[string]any{"ok": ok, "busy": !ok})
	})
	post("/api/gumroad/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		purchases.GumCancel.Store(true)
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/gumroad/logout", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if purchases.GumBusy.Load() {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "正在同步，稍后再试"})
			return
		}
		err := purchases.LogoutGumroad()
		if boolean(b, "clear") { // the purchases read so far go too; what is on disk stays
			st.Mu.Lock()
			for id := range st.Purchases {
				if core.IsGumID(id) {
					delete(st.Purchases, id)
					delete(st.Booth, id)
				}
			}
			st.GumroadSync = 0
			for _, a := range st.Assets {
				if a.BoothFromLib && core.IsGumID(a.BoothID) {
					a.BoothID, a.BoothFromLib = "", false
				}
			}
			st.Mu.Unlock()
			_ = st.Save()
		}
		core.BumpRev()
		if err != nil {
			core.Logf("退出 Gumroad：%v", err)
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/purchases/sync", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		ok := purchases.StartPurchaseSync(st)
		core.WriteJSON(w, map[string]any{"ok": ok, "busy": !ok})
	})
	// ---------- Booth: downloads and browsing ----------
	post("/api/booth/download", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var ids []string
		_ = json.Unmarshal(b["ids"], &ids)
		if p := strings.TrimSpace(str(b, "importTo")); p != "" { // "下载并导入"
			p = filepath.Clean(strings.Trim(p, `"`))
			if !core.IsUnityProject(p) {
				core.WriteJSON(w, map[string]any{"ok": false, "err": "这不是 Unity 工程（找不到 Assets 和 ProjectSettings 文件夹）"})
				return
			}
			purchases.SetPendingImport(str(b, "item"), unity.ImportReq{Project: p, Pwd: str(b, "pwd"), Recycle: true})
		}
		n, err := purchases.QueueDownloads(st, str(b, "item"), ids)
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "n": n})
	})
	// every purchase that is not in the library yet
	post("/api/booth/download/missing", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var only []string // the cards on screen; empty = every purchase not downloaded yet
		_ = json.Unmarshal(b["items"], &only)
		owned := library.OwnedBoothIDs(st)
		st.Mu.RLock()
		var items []string
		for _, id := range core.SortedKeys(st.Purchases) {
			if len(only) > 0 && !core.ContainsStr(only, id) {
				continue
			}
			if _, ok := owned[id]; !ok && len(st.Purchases[id].Downloads) > 0 {
				items = append(items, id)
			}
		}
		st.Mu.RUnlock()
		total := 0
		for _, id := range items {
			n, _ := purchases.QueueDownloads(st, id, nil)
			total += n
		}
		core.WriteJSON(w, map[string]any{"ok": true, "n": total, "items": len(items)})
	})
	post("/api/booth/download/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		purchases.CancelDownloads()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/shop/search", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var q library.ShopQuery
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &q)
		items, more, err := library.SearchShop(st, q)
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "items": items, "more": more})
	})
	post("/api/shop/item", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		d, err := library.ShopItemDetail(st, str(b, "id"))
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "item": d})
	})
	// ---------- Booth / 闲鱼 pages inside the program ----------
	webpane.Pane.St = st
	post("/api/pane/open", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		u := str(b, "url")
		if !(strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "不是网址"})
			return
		}
		if webpane.PaneMode() == "" {
			core.WriteJSON(w, map[string]any{"ok": core.OpenURL(u) == nil, "external": true})
			return
		}
		if err := webpane.Pane.Open(u, str(b, "kind"), true); err != nil {
			core.Logf("页面打不开 %s: %v", u, err)
			core.WriteJSON(w, map[string]any{"ok": false, "err": "页面打不开：" + err.Error()})
			return
		}
		if str(b, "kind") == "pan" && !pandl.CurrentBaiduAccount().LoggedIn {
			pandl.WatchBaiduLogin(st) // a netdisk page: once the player logs in there, downloads can use it
		}
		if str(b, "kind") == "gumroad" && purchases.LoadGumSession() == nil {
			purchases.WatchGumroadLogin(st) // the Gumroad login page: once the player is in, the purchases are read
		}
		core.WriteJSON(w, map[string]any{"ok": true, "mode": webpane.PaneMode()})
	})
	post("/api/pane/place", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var r struct {
			X, Y, W, H, DPR float64
			Show            bool
		}
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &r)
		webpane.Pane.Place(r.X, r.Y, r.W, r.H, r.DPR, r.Show)
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/pane/state", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, webpane.Pane.State())
	})
	post("/api/pane/act", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		act := str(b, "act")
		if act == "external" {
			u, err := webpane.Pane.Act("url")
			if err != nil || u == "" {
				core.WriteJSON(w, map[string]any{"ok": false, "err": "没有打开的页面"})
				return
			}
			core.WriteJSON(w, map[string]any{"ok": core.OpenURL(u) == nil})
			return
		}
		text, err := webpane.Pane.Act(act)
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "text": text})
	})
	// ---------- Baidu Netdisk: the account and downloads ----------
	post("/api/baidu/check", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		s := pandl.LoadBaiduSession()
		if s == nil {
			core.WriteJSON(w, map[string]any{"ok": true, "baidu": pandl.CurrentBaiduAccount()})
			return
		}
		name, vip, err := pandl.NewBDClient(st, s).Whoami()
		switch {
		case errors.Is(err, pandl.ErrBaiduLogin):
			pandl.ForgetBaiduSession()
			core.BumpRev()
		case err == nil && (name != s.Name || vip != s.VIP):
			c := *s
			c.Name, c.VIP = name, vip
			_ = pandl.SaveBaiduSession(&c)
			core.BumpRev()
		}
		res := map[string]any{"ok": err == nil || errors.Is(err, pandl.ErrBaiduLogin), "baidu": pandl.CurrentBaiduAccount()}
		if err != nil && !errors.Is(err, pandl.ErrBaiduLogin) {
			res["err"] = err.Error()
		}
		core.WriteJSON(w, res)
	})
	post("/api/baidu/logout", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		err := pandl.LogoutBaidu()
		core.BumpRev()
		if err != nil {
			core.Logf("退出百度网盘：%v", err)
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/pan/download", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var imp *unity.ImportReq
		if p := strings.TrimSpace(str(b, "importTo")); p != "" {
			p = filepath.Clean(strings.Trim(p, `"`))
			if !core.IsUnityProject(p) {
				core.WriteJSON(w, map[string]any{"ok": false, "err": "这不是 Unity 工程（找不到 Assets 和 ProjectSettings 文件夹）"})
				return
			}
			imp = &unity.ImportReq{Project: p, Pwd: str(b, "pwd"), Recycle: true}
		}
		var paths []string
		_ = json.Unmarshal(b["paths"], &paths)
		if err := pandl.QueuePanDownload(st, str(b, "key"), paths, imp); err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "login": !pandl.CurrentBaiduAccount().LoggedIn})
	})
	post("/api/pan/download/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		pandl.CancelPanDownloads()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/pan/download/dismiss", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		pandl.DismissPanJob(str(b, "key"))
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// ---------- one-click import into a Unity project ----------
	post("/api/import/start", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var req unity.ImportReq
		raw, _ := json.Marshal(b)
		_ = json.Unmarshal(raw, &req)
		req.Project = filepath.Clean(strings.Trim(strings.TrimSpace(req.Project), `"`))
		if err := unity.StartImport(st, req); err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/import/choose", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var paths []string
		_ = json.Unmarshal(b["paths"], &paths)
		core.WriteJSON(w, map[string]any{"ok": unity.ChooseImport(paths)})
	})
	post("/api/import/dismiss", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		unity.DismissImport()
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	// ---------- the 工程 page ----------
	ai.RegisterAI(st, post)
	registerPipe(st, post)
	post("/api/projects", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "projects": unity.ProjectCards(st)})
	})
	post("/api/project/open", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := unity.KnownProject(st, str(b, "path"))
		if !ok {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "不在工程列表里"})
			return
		}
		if err := unity.OpenInUnity(p); err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/project/cover", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := unity.KnownProject(st, str(b, "path"))
		if !ok {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "不在工程列表里"})
			return
		}
		var on bool
		_ = json.Unmarshal(b["on"], &on)
		if err := unity.SetCoverHelper(p, on); err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "running": unity.ProjectRunning(p)})
	})
	// a project picked for the import that the library does not know yet
	post("/api/import/project", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p := filepath.Clean(strings.Trim(strings.TrimSpace(str(b, "path")), `"`))
		if !core.IsUnityProject(p) {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "这不是 Unity 工程（找不到 Assets 和 ProjectSettings 文件夹）"})
			return
		}
		st.Mu.Lock()
		known := false
		for _, pr := range st.Projects {
			if core.PathKey(pr.Path) == core.PathKey(p) {
				known = true
			}
		}
		if !known {
			st.Settings.ProjectRoots = core.CleanPaths(append(st.Settings.ProjectRoots, p))
			st.Projects = append(st.Projects, core.ProjectInfo{Name: filepath.Base(p), Path: p})
			sort.Slice(st.Projects, func(i, j int) bool { return st.Projects[i].Name < st.Projects[j].Name })
		}
		st.Mu.Unlock()
		if !known {
			_ = st.Save()
			core.BumpRev()
			library.StartPipeline(st, false, true, false, false, nil)
		}
		core.WriteJSON(w, map[string]any{"ok": true, "path": p, "name": filepath.Base(p)})
	})
	post("/api/purchases/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		purchases.PurchaseCancel.Store(true)
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/purchases/forget", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if core.PurchaseBusy.Load() {
			core.WriteJSON(w, map[string]any{"ok": false, "err": "正在同步，稍后再试"})
			return
		}
		err := purchases.ForgetBoothLogin()
		if boolean(b, "clear") {
			st.Mu.Lock()
			for id := range st.Purchases {
				if !core.IsGumID(id) { // Gumroad's stay: they have their own "退出"
					delete(st.Purchases, id)
				}
			}
			st.PurchaseSync = 0
			for _, a := range st.Assets {
				if a.BoothFromLib && !core.IsGumID(a.BoothID) {
					a.BoothID, a.BoothFromLib = "", false
				}
			}
			st.Mu.Unlock()
			_ = st.Save()
			core.BumpRev()
		}
		if err != nil {
			core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	return mux
}

func underAny(p string, dirs []string) bool {
	lp := strings.ToLower(filepath.Clean(p))
	for _, d := range dirs {
		ld := strings.ToLower(filepath.Clean(d))
		if ld != "" && (lp == ld || strings.HasPrefix(lp, ld+string(os.PathSeparator))) {
			return true
		}
	}
	return false
}

func imageAllowed(st *core.Store, p string) bool {
	if p == "" || !library.ImageExt[core.LowerExt(p)] {
		return false
	}
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	dirs := append(append([]string{core.DataDir}, st.Settings.Roots...), st.Settings.ProjectRoots...)
	if underAny(p, dirs) {
		return true
	}
	for _, u := range st.User {
		if u.Cover != "" && strings.EqualFold(filepath.Clean(u.Cover), filepath.Clean(p)) {
			return true
		}
	}
	return false
}

// keep sort imported for future use
var _ = sort.Strings
