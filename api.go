package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

//go:embed web/*
var webFS embed.FS

var (
	apiToken string
	lastPing atomic.Int64
	everPing atomic.Bool
)

func init() {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	apiToken = hex.EncodeToString(b)
}

type AssetView struct {
	Key          string        `json:"key"`
	Name         string        `json:"name"`
	AutoName     string        `json:"autoName"`
	RawName      string        `json:"rawName"`
	Category     string        `json:"category"`
	AutoCategory string        `json:"autoCategory"`
	Bases        []string      `json:"bases"`
	AutoBases    []string      `json:"autoBases"`
	Tags         []string      `json:"tags"`
	BoothID      string        `json:"boothId"`
	Booth        *BoothInfo    `json:"booth"`
	Locations    []Location    `json:"locations"`
	Size         int64         `json:"size"`
	Files        int           `json:"files"`
	MTime        int64         `json:"mtime"`
	FirstSeen    int64         `json:"firstSeen"`
	Usage        []Usage       `json:"usage"`
	GuidCount    int           `json:"guidCount"`
	Packages     int           `json:"packages"`
	Cover        string        `json:"cover"`
	CoverBig     string        `json:"coverBig"`
	LocalCovers  []string      `json:"localCovers"`
	User         UserData      `json:"user"`
	Hints        []string      `json:"hints"`
	Hidden       bool          `json:"hidden"`
	HasDir       bool          `json:"hasDir"`
	Virtual      bool          `json:"virtual,omitempty"` // bought on Booth, not found on disk
	Purchase     *PurchaseView `json:"purchase,omitempty"`
	PanOnly      bool          `json:"panOnly,omitempty"` // only in a Baidu Netdisk share
	Pan          *PanListing   `json:"pan,omitempty"`     // what is inside the share link
	NameZh       string        `json:"nameZh,omitempty"`
	BoothSrc     string        `json:"boothSrc,omitempty"` // user / name / url / library / auto
	BoothQuery   string        `json:"boothQuery,omitempty"`
	BoothHits    []BoothHit    `json:"boothHits,omitempty"`
}

type OrderView struct {
	ID   string `json:"id"`
	Date string `json:"date"`
	URL  string `json:"url"`
}

type PurchaseView struct {
	Name       string      `json:"name"`
	Shop       string      `json:"shop"`
	ShopURL    string      `json:"shopUrl"`
	Files      []string    `json:"files"`
	Gift       bool        `json:"gift"`
	Orders     []OrderView `json:"orders"`
	PageURL    string      `json:"pageUrl"` // where the downloads are: latest order, or the library
	LibraryURL string      `json:"libraryUrl"`
	Matched    bool        `json:"matched"` // linked by file name (not by an id in the folder name)
}

func purchaseView(p *Purchase, matched bool) *PurchaseView {
	v := &PurchaseView{Name: p.Name, Shop: p.Shop, ShopURL: p.ShopURL, Files: p.Files, Gift: p.Gift,
		LibraryURL: libraryURL(p.Gift), Matched: matched}
	for _, o := range p.Orders {
		v.Orders = append(v.Orders, OrderView{ID: o.ID, Date: o.Date, URL: orderURL(o.ID)})
	}
	v.PageURL = v.LibraryURL
	if len(v.Orders) > 0 {
		v.PageURL = v.Orders[0].URL
	}
	return v
}

func thumbURL(p string, w int) string {
	return "/thumb?w=" + strconv.Itoa(w) + "&p=" + url.QueryEscape(p)
}

func effectiveBoothID(a *Asset, u *UserData) string {
	if u != nil && u.BoothURL != "" {
		if m := reBoothURL.FindStringSubmatch(u.BoothURL); m != nil {
			return m[1]
		}
	}
	return a.BoothID
}

var reWordTok = regexp.MustCompile(`[a-z0-9]{4,}`)

// namesRelated: do two product names share a meaningful word (or two CJK characters in a row)?
func namesRelated(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	for _, t := range reWordTok.FindAllString(la, -1) {
		if !isMostlyDigits(t) && strings.Contains(lb, t) {
			return true
		}
	}
	ra := []rune(la)
	for i := 0; i+1 < len(ra); i++ {
		if ra[i] >= 0x3040 && ra[i+1] >= 0x3040 && strings.Contains(lb, string(ra[i:i+2])) {
			return true
		}
	}
	return false
}

var keywordWins = map[string]bool{"插件": true, "音效": true, "面捕": true, "字体": true, "头发": true, "动作": true}

func buildView(st *Store, a *Asset) AssetView {
	u := st.User[a.Key]
	v := AssetView{Key: a.Key, AutoName: a.Name, RawName: a.RawName, AutoCategory: a.Category, AutoBases: a.Bases,
		Locations: a.Locations, Size: a.Size, Files: a.Files, MTime: a.MTime, FirstSeen: a.FirstSeen, Usage: a.Usage,
		GuidCount: a.GuidCount, Packages: len(a.Packages), LocalCovers: a.Covers, Hints: a.Hints, HasDir: a.HasDir}
	id, src := assetBooth(st, a.Key, a)
	if u == nil && id != "" {
		u = st.User["purchase:"+id] // notes typed while it was only a purchase follow it onto disk
	}
	if u != nil {
		v.User = *u
	}
	v.BoothID, v.BoothSrc = id, src
	if id != "" {
		if b := st.Booth[id]; b != nil {
			// an id that only came from a .url shortcut must look like the same product, or it is a dependency link
			if src != "url" || b.Name == "" || namesRelated(a.Name+" "+a.RawName, b.Name) {
				bc := *b
				v.Booth = &bc
			} else {
				v.BoothID, v.BoothSrc = "", ""
			}
		}
		if p := st.Purchases[v.BoothID]; p != nil {
			v.Purchase = purchaseView(p, a.BoothFromLib)
		}
	}
	// name
	v.Name = a.Name
	if v.Booth != nil && v.Booth.Name != "" && (isMostlyDigits(a.Name) || len([]rune(a.Name)) <= 2 || isGenericName(a.Name)) {
		v.Name = v.Booth.Name
	} else if v.Purchase != nil && v.Purchase.Name != "" && (isMostlyDigits(a.Name) || len([]rune(a.Name)) <= 2 || isGenericName(a.Name)) {
		v.Name = v.Purchase.Name
	}
	// category: booth (except keyword categories it lumps together) > auto
	v.Category = a.Category
	if v.Booth != nil && v.Booth.Category != "" {
		if bc, ok := boothCatMap[v.Booth.Category]; ok && !keywordWins[a.Category] && (bc != "其他" || a.Category == "其他") {
			v.Category = bc
		}
	}
	v.Bases = a.Bases
	extraText := ""
	if v.Booth != nil {
		extraText = v.Booth.Name
	} else if v.Purchase != nil {
		extraText = v.Purchase.Name
	}
	if extraText != "" {
		extra := detectBases(extraText, parseBases(st.Settings.Bases))
		v.Bases = uniqStrings(append(append([]string{}, a.Bases...), extra...))
	}
	// cover: user > booth > purchase thumbnail > local image
	cover := ""
	switch {
	case v.User.Cover != "":
		cover = v.User.Cover
	case v.Booth != nil && v.Booth.Cover != "":
		cover = v.Booth.Cover
	case v.Purchase != nil && st.Purchases[v.BoothID].Cover != "":
		cover = st.Purchases[v.BoothID].Cover
	case len(a.Covers) > 0:
		cover = a.Covers[0]
	}
	applyUserView(&v, cover)
	finishView(st, &v, a.Name)
	return v
}

// finishView fills what every kind of card shares: netdisk listing, Booth search words and
// suggestions, Chinese name.
func finishView(st *Store, v *AssetView, autoName string) {
	if surl := shareSurl(v.User.ShareURL); surl != "" {
		if l := st.Pan[surl]; l != nil {
			v.Pan = l
		}
	}
	bases := parseBases(st.Settings.Bases)
	v.BoothQuery = boothQueryFor(autoName, bases, v.Category)
	if v.PanOnly && (v.Pan == nil || v.Pan.Title == "") {
		v.BoothQuery = "" // the share has not been read yet: its placeholder name is not worth searching
	} else if v.BoothQuery == "" {
		v.BoothQuery = strings.TrimSpace(strings.NewReplacer("_", " ").Replace(cleanName(autoName)))
	}
	if v.BoothID == "" {
		if m := st.BoothMatch[v.Key]; m != nil {
			v.BoothHits = m.Hits
		}
	}
	if v.User.NameZh != "" {
		v.NameZh = v.User.NameZh
	} else if src := zhSourceOfView(v); src != "" {
		v.NameZh = zhGlossary.Replace(st.Trans[src])
	}
}

// zhSourceOfView: the most descriptive name to translate (the Booth title when there is one).
func zhSourceOfView(v *AssetView) string {
	if v.User.Name != "" {
		return zhSource(v.User.Name)
	}
	if v.Booth != nil && v.Booth.Name != "" {
		return zhSource(v.Booth.Name)
	}
	if v.Purchase != nil && v.Purchase.Name != "" {
		return zhSource(v.Purchase.Name)
	}
	return zhSource(v.Name)
}

// allViews: local assets, purchases that are not on disk, and netdisk-only assets. Caller holds st.mu.
func allViews(st *Store) []AssetView {
	var out []AssetView
	onDisk := map[string]bool{}
	for _, a := range st.Assets {
		v := buildView(st, a)
		if v.Purchase != nil {
			onDisk[v.BoothID] = true
		}
		out = append(out, v)
	}
	for _, key := range sortedKeys(st.User) {
		if strings.HasPrefix(key, "pan:") {
			v := panOnlyView(st, key)
			if v.Purchase != nil {
				onDisk[v.BoothID] = true
			}
			out = append(out, v)
		}
	}
	for _, id := range sortedKeys(st.Purchases) {
		if !onDisk[id] {
			out = append(out, purchaseOnlyView(st, st.Purchases[id]))
		}
	}
	return out
}

func zhSources(st *Store) []string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	var out []string
	for _, v := range allViews(st) {
		if v.User.NameZh == "" {
			if s := zhSourceOfView(&v); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func panAssetName(st *Store, key string) string {
	surl := strings.TrimPrefix(key, "pan:")
	if l := st.Pan[surl]; l != nil && l.Title != "" {
		return l.Title
	}
	return "网盘分享 " + surl
}

// panOnlyView: an asset that only lives in a Baidu Netdisk share.
func panOnlyView(st *Store, key string) AssetView {
	u := st.User[key]
	name := panAssetName(st, key)
	v := AssetView{Key: key, Name: name, AutoName: name, RawName: name, PanOnly: true}
	if u != nil {
		v.User = *u
		v.FirstSeen = u.Updated
	}
	text := name
	if l := st.Pan[strings.TrimPrefix(key, "pan:")]; l != nil {
		v.Size, v.Files = l.Size, l.Count
		for i, f := range l.Files {
			if i >= 30 {
				break
			}
			text += " " + f.Name
		}
	}
	cat := bracketCategory(name)
	if cat == "" {
		cat = classify(name)
	}
	if cat == "其他" {
		cat = classify(text)
	}
	id, src := assetBooth(st, key, nil)
	v.BoothID, v.BoothSrc = id, src
	if b := st.Booth[id]; id != "" && b != nil {
		bc := *b
		v.Booth = &bc
		if bcat, ok := boothCatMap[b.Category]; ok && !keywordWins[cat] && (bcat != "其他" || cat == "其他") {
			cat = bcat
		}
	}
	if p := st.Purchases[id]; id != "" && p != nil {
		v.Purchase = purchaseView(p, false)
	}
	v.Category, v.AutoCategory = cat, cat
	v.Bases = detectBases(text, parseBases(st.Settings.Bases))
	v.AutoBases = v.Bases
	cover := ""
	switch {
	case v.User.Cover != "":
		cover = v.User.Cover
	case v.Booth != nil && v.Booth.Cover != "":
		cover = v.Booth.Cover
	}
	applyUserView(&v, cover)
	finishView(st, &v, name)
	return v
}

// purchaseOnlyView: something bought on Booth that is not in any scanned folder.
func purchaseOnlyView(st *Store, p *Purchase) AssetView {
	key := "purchase:" + p.ID
	v := AssetView{Key: key, Name: p.Name, AutoName: p.Name, RawName: p.Name, BoothID: p.ID, BoothSrc: "library", Virtual: true,
		FirstSeen: p.when(), Purchase: purchaseView(p, false)}
	if u := st.User[key]; u != nil {
		v.User = *u
	}
	if b := st.Booth[p.ID]; b != nil {
		bc := *b
		v.Booth = &bc
		if v.Name == "" {
			v.Name = b.Name
		}
	}
	text := p.Name + " " + strings.Join(p.Files, " ")
	cat := bracketCategory(p.Name)
	if cat == "" {
		cat = classify(p.Name)
	}
	if v.Booth != nil {
		if bc, ok := boothCatMap[v.Booth.Category]; ok && !keywordWins[cat] && (bc != "其他" || cat == "其他") {
			cat = bc
		}
	}
	v.Category, v.AutoCategory = cat, cat
	v.Bases = detectBases(text, parseBases(st.Settings.Bases))
	v.AutoBases = v.Bases
	cover := ""
	switch {
	case v.User.Cover != "":
		cover = v.User.Cover
	case v.Booth != nil && v.Booth.Cover != "":
		cover = v.Booth.Cover
	case p.Cover != "":
		cover = p.Cover
	}
	applyUserView(&v, cover)
	finishView(st, &v, p.Name)
	return v
}

func applyUserView(v *AssetView, cover string) {
	if v.User.Name != "" {
		v.Name = v.User.Name
	}
	if v.User.Category != "" {
		v.Category = v.User.Category
	}
	if v.User.Bases != nil {
		v.Bases = v.User.Bases
	}
	v.Tags = v.User.Tags
	v.Hidden = v.User.Hidden
	if cover != "" {
		v.Cover = thumbURL(cover, 420)
		v.CoverBig = thumbURL(cover, 900)
	}
}

type stateResp struct {
	Rev        int64             `json:"rev"`
	Version    string            `json:"version"`
	Assets     []AssetView       `json:"assets"`
	Settings   Settings          `json:"settings"`
	Projects   []ProjectInfo     `json:"projects"`
	Warnings   []string          `json:"warnings"`
	LastScan   int64             `json:"lastScan"`
	LastUsage  int64             `json:"lastUsage"`
	Tasks      []Task            `json:"tasks"`
	Busy       bool              `json:"busy"`
	Categories []string          `json:"categories"`
	Overrides  map[string]string `json:"overrides"`
	DataDir    string            `json:"dataDir"`

	SetupNeeded    bool   `json:"setupNeeded"`
	PurchaseSync   int64  `json:"purchaseSync"`
	PurchaseCount  int    `json:"purchaseCount"`
	PurchaseBusy   bool   `json:"purchaseBusy"`
	CanShortcut    bool   `json:"canShortcut"`
	DefaultBrowser string `json:"defaultBrowser"`
}

func tasksSnapshot() []Task {
	return []Task{taskScan.snapshot(), taskUsage.snapshot(), taskMatch.snapshot(), taskBooth.snapshot(), taskTrans.snapshot(),
		taskPurchase.snapshot(), taskPan.snapshot()}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func newMux(st *Store) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		b, err := webFS.ReadFile("web/" + name)
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
		lastPing.Store(time.Now().Unix())
		everPing.Store(true)
		writeJSON(w, map[string]any{"app": "vrclib", "rev": curRev()})
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		st.mu.RLock()
		resp := stateResp{Rev: curRev(), Version: appVersion, Settings: st.Settings, Projects: st.Projects,
			Warnings: st.Warnings, LastScan: st.LastScan, LastUsage: st.LastUsage, Categories: Categories,
			Overrides: st.Overrides, DataDir: dataDir, SetupNeeded: !st.Settings.SetupDone,
			PurchaseSync: st.PurchaseSync, PurchaseCount: len(st.Purchases), PurchaseBusy: purchaseBusy.Load(),
			CanShortcut: runtime.GOOS == "windows"}
		resp.Assets = allViews(st)
		st.mu.RUnlock()
		resp.DefaultBrowser = browserLabel(defaultBrowserExe())
		resp.Tasks = tasksSnapshot()
		resp.Busy = pipelineBusy()
		writeJSON(w, resp)
	})
	mux.HandleFunc("/api/progress", func(w http.ResponseWriter, r *http.Request) {
		lastPing.Store(time.Now().Unix())
		writeJSON(w, map[string]any{"rev": curRev(), "tasks": tasksSnapshot(), "busy": pipelineBusy(), "purchaseBusy": purchaseBusy.Load()})
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
		if t := thumbnail(p, wd); t != "" {
			http.ServeFile(w, r, t)
			return
		}
		http.ServeFile(w, r, p) // webp etc.: let the browser decode the original
	})

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
			writeJSON(w, map[string]any{"ok": false, "err": "路径不存在：" + p})
			return
		}
		err = openInExplorer(filepath.Clean(p), fi.IsDir())
		writeJSON(w, map[string]any{"ok": err == nil})
	})
	post("/api/openurl", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		u := str(b, "url")
		if !(strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) {
			writeJSON(w, map[string]any{"ok": false, "err": "不是网址"})
			return
		}
		writeJSON(w, map[string]any{"ok": openURL(u) == nil})
	})
	post("/api/user", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		key := str(b, "key")
		if boolean(b, "delete") && strings.HasPrefix(key, "pan:") {
			st.mu.Lock()
			delete(st.User, key)
			delete(st.BoothMatch, key)
			st.mu.Unlock()
			_ = st.Save()
			bumpRev()
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		var u UserData
		if err := json.Unmarshal(b["user"], &u); err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		u.Updated = time.Now().Unix()
		u.Tags = cleanList(u.Tags)
		if u.Bases != nil {
			u.Bases = cleanList(u.Bases)
		}
		u.ShareURL, u.SharePwd = strings.TrimSpace(u.ShareURL), strings.TrimSpace(u.SharePwd)
		st.mu.Lock()
		old := st.User[key]
		st.User[key] = &u
		panChanged := shareSurl(u.ShareURL) != "" && (old == nil || old.ShareURL != u.ShareURL || old.SharePwd != u.SharePwd ||
			st.Pan[shareSurl(u.ShareURL)] == nil)
		st.mu.Unlock()
		_ = st.Save()
		bumpRev()
		writeJSON(w, map[string]any{"ok": true})
		if panChanged {
			QueuePanFetch(st, key)
		} else if u.Name != "" && (old == nil || old.Name != u.Name) {
			KickTranslate(st)
		}
		// a newly entered booth link → fetch its info
		if u.BoothURL != "" && reBoothURL.MatchString(u.BoothURL) {
			StartPipeline(st, false, false, true, false, []string{key})
		}
	})
	post("/api/scan", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		ok := StartPipeline(st, boolean(b, "scan"), boolean(b, "usage"), boolean(b, "booth"), boolean(b, "force"), nil)
		writeJSON(w, map[string]any{"ok": ok, "busy": !ok})
	})
	post("/api/booth", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var keys []string
		_ = json.Unmarshal(b["keys"], &keys)
		ok := StartPipeline(st, false, false, true, boolean(b, "force"), keys)
		writeJSON(w, map[string]any{"ok": ok, "busy": !ok})
	})
	post("/api/settings", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var s Settings
		if err := json.Unmarshal(b["settings"], &s); err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		s.Roots, s.ProjectRoots, s.Bases = cleanPaths(s.Roots), cleanPaths(s.ProjectRoots), cleanList(s.Bases)
		if len(s.Bases) == 0 {
			s.Bases = defaultSettings().Bases
		}
		s.SetupDone = true
		st.mu.Lock()
		st.Settings = s
		st.mu.Unlock()
		_ = st.Save()
		bumpRev()
		ok := StartPipeline(st, true, true, s.AutoBooth, false, nil)
		writeJSON(w, map[string]any{"ok": true, "started": ok})
	})
	post("/api/override", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, mode := str(b, "path"), str(b, "mode")
		st.mu.Lock()
		if mode == "" {
			delete(st.Overrides, pathKey(p))
		} else {
			st.Overrides[pathKey(p)] = mode
		}
		st.mu.Unlock()
		_ = st.Save()
		ok := StartPipeline(st, true, true, false, false, nil)
		writeJSON(w, map[string]any{"ok": true, "started": ok})
	})
	mux.HandleFunc("/rthumb", func(w http.ResponseWriter, r *http.Request) {
		p, err := remoteThumb(st, r.URL.Query().Get("u"))
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Cache-Control", "max-age=604800")
		http.ServeFile(w, r, p)
	})
	post("/api/boothsearch", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		hits, err := SearchBooth(httpClient(st), str(b, "q"))
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		if name := str(b, "name"); name != "" {
			st.mu.RLock()
			bases := parseBases(st.Settings.Bases)
			st.mu.RUnlock()
			scoreHits(hits, name, str(b, "cat"), bases)
		}
		if len(hits) > 24 {
			hits = hits[:24]
		}
		writeJSON(w, map[string]any{"ok": true, "hits": hits})
	})
	post("/api/translate", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		t, err := translateLong(st, str(b, "text"))
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": "翻译失败：" + err.Error()})
			return
		}
		_ = st.Save()
		writeJSON(w, map[string]any{"ok": true, "text": t})
	})
	post("/api/pan/add", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		link, pwd := strings.TrimSpace(str(b, "url")), strings.TrimSpace(str(b, "pwd"))
		surl := shareSurl(link)
		if surl == "" {
			writeJSON(w, map[string]any{"ok": false, "err": "没认出百度网盘分享链接"})
			return
		}
		if pwd == "" {
			pwd = sharePwdFromURL(link)
		}
		key := "pan:" + surl
		st.mu.Lock()
		if st.User[key] == nil {
			st.User[key] = &UserData{ShareURL: "https://pan.baidu.com/s/" + surl, SharePwd: pwd, Updated: time.Now().Unix(),
				Name: strings.TrimSpace(str(b, "name"))}
		}
		st.mu.Unlock()
		_ = st.Save()
		bumpRev()
		QueuePanFetch(st, key)
		writeJSON(w, map[string]any{"ok": true, "key": key})
	})
	post("/api/pan/refresh", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		QueuePanFetch(st, str(b, "key"))
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/detect", func(w http.ResponseWriter, r *http.Request) {
		roots, projects := detectCandidates()
		writeJSON(w, map[string]any{"roots": roots, "projects": projects})
	})
	post("/api/pickfolder", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, err := pickFolder(str(b, "title"), str(b, "initial"))
		switch {
		case errors.Is(err, errPickCancelled):
			writeJSON(w, map[string]any{"ok": false, "cancelled": true})
		case err != nil:
			writeJSON(w, map[string]any{"ok": false, "err": "打不开选择窗口，请直接粘贴路径：" + err.Error()})
		default:
			writeJSON(w, map[string]any{"ok": true, "path": p})
		}
	})
	post("/api/shortcut", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, err := createDesktopShortcut()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "path": p})
	})
	post("/api/purchases/sync", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		ok := StartPurchaseSync(st)
		writeJSON(w, map[string]any{"ok": ok, "busy": !ok})
	})
	post("/api/purchases/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		purchaseCancel.Store(true)
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/purchases/forget", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if purchaseBusy.Load() {
			writeJSON(w, map[string]any{"ok": false, "err": "正在同步，稍后再试"})
			return
		}
		err := ForgetBoothLogin()
		if boolean(b, "clear") {
			st.mu.Lock()
			st.Purchases = map[string]*Purchase{}
			st.PurchaseSync = 0
			for _, a := range st.Assets {
				if a.BoothFromLib {
					a.BoothID, a.BoothFromLib = "", false
				}
			}
			st.mu.Unlock()
			_ = st.Save()
			bumpRev()
		}
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	return mux
}

func cleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out
}

func cleanPaths(in []string) []string {
	var out []string
	for _, s := range cleanList(in) {
		s = strings.Trim(s, `"`)
		out = append(out, filepath.Clean(s))
	}
	return out
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

func imageAllowed(st *Store, p string) bool {
	if p == "" || !imageExt[lowerExt(p)] {
		return false
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	dirs := append(append([]string{dataDir}, st.Settings.Roots...), st.Settings.ProjectRoots...)
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
