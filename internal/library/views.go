package library

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/netdisk"
	"vrclib/internal/translate"
)

type AssetView struct {
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	AutoName     string            `json:"autoName"`
	RawName      string            `json:"rawName"`
	Category     string            `json:"category"`
	AutoCategory string            `json:"autoCategory"`
	Bases        []string          `json:"bases"`
	AutoBases    []string          `json:"autoBases"`
	Tags         []string          `json:"tags"`
	BoothID      string            `json:"boothId"`
	Booth        *core.BoothInfo   `json:"booth"`
	Locations    []core.Location   `json:"locations"`
	Size         int64             `json:"size"`
	Files        int               `json:"files"`
	MTime        int64             `json:"mtime"`
	FirstSeen    int64             `json:"firstSeen"`
	Usage        []core.Usage      `json:"usage"`
	GuidCount    int               `json:"guidCount"`
	Packages     int               `json:"packages"`
	Archives     int               `json:"archives,omitempty"` // archives (and unitypackages inside zips) to unpack for an import
	Cover        string            `json:"cover"`
	CoverBig     string            `json:"coverBig"`
	LocalCovers  []string          `json:"localCovers"`
	User         core.UserData     `json:"user"`
	Hints        []string          `json:"hints"`
	Hidden       bool              `json:"hidden"`
	HasDir       bool              `json:"hasDir"`
	Virtual      bool              `json:"virtual,omitempty"` // bought on Booth, not found on disk
	Purchase     *PurchaseView     `json:"purchase,omitempty"`
	PanOnly      bool              `json:"panOnly,omitempty"` // only in a Baidu Netdisk share
	Pan          *core.PanListing  `json:"pan,omitempty"`     // what is inside the share link
	NameZh       string            `json:"nameZh,omitempty"`
	BoothSrc     string            `json:"boothSrc,omitempty"` // user / name / url / library / auto
	BoothQuery   string            `json:"boothQuery,omitempty"`
	BoothHits    []core.BoothHit   `json:"boothHits,omitempty"`
	Styles       []string          `json:"styles"`
	AutoStyles   []string          `json:"autoStyles"`
	PSD          bool              `json:"psd,omitempty"` // only texture sources (PSD …), no Unity content
	PSDs         []core.PSDFile    `json:"psds,omitempty"`
	PSDCount     int               `json:"psdCount,omitempty"`
	PSDInZip     int               `json:"psdInZip,omitempty"`
	Group        string            `json:"group,omitempty"` // downloads of one product share it
	GroupName    string            `json:"groupName,omitempty"`
	Variant      string            `json:"variant,omitempty"` // what this download is for: base bodies, PSD …
	PanParts     []netdisk.PanPart `json:"panParts,omitempty"`
	New          bool              `json:"new,omitempty"`       // turned up in the last days, after the first scan
	PanNews      *PanNews          `json:"panNews,omitempty"`   // the share changed since it was last looked at
	ShareErr     string            `json:"shareErr,omitempty"`  // the share could not be read again (expired?)
	BoothNews    string            `json:"boothNews,omitempty"` // the shop changed its Booth page
	BoothNewsAt  int64             `json:"boothNewsAt,omitempty"`
	NewerOnBooth string            `json:"newerOnBooth,omitempty"` // a later version among the Booth downloads
	NewerDL      string            `json:"newerDl,omitempty"`      // its downloadable id
	LocalVer     string            `json:"localVer,omitempty"`
	PanParent    string            `json:"panParent,omitempty"` // a product inside a split share: the share's key
	PanPath      string            `json:"panPath,omitempty"`   // and where it is in the share
	SplitInto    int               `json:"splitInto,omitempty"` // a share shown as this many product cards
	CanSplit     int               `json:"canSplit,omitempty"`  // kept whole by the player, but holds this many products
	FromPan      string            `json:"fromPan,omitempty"`   // downloaded by the program from this netdisk card
	PanGot       []string          `json:"panGot,omitempty"`    // which parts of that card's file list ("/" = all)
	CoverPkg     bool              `json:"coverPkg,omitempty"`  // the cover is a preview from its unitypackage (pkgcover.go)

	coverGen bool // the cover is one made from a prefab in Unity (gencover.go)
}

type PanNews struct {
	At      int64    `json:"at"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
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
	ID         string      `json:"id"`
	DLs        []string    `json:"dls"`                  // downloadable id of each file
	Got        []string    `json:"got"`                  // where each file was downloaded to ("" = not by this program)
	Source     string      `json:"source,omitempty"`     // "gumroad" for a Gumroad purchase
	ProductURL string      `json:"productUrl,omitempty"` // Gumroad: the product's page
	Variant    string      `json:"variant,omitempty"`
	Note       string      `json:"note,omitempty"` // why there is nothing to download
}

// purchaseView: caller holds st.mu.
func purchaseView(st *core.Store, p *core.Purchase, matched bool) *PurchaseView {
	v := &PurchaseView{Name: p.Name, Shop: p.Shop, ShopURL: p.ShopURL, Files: p.Files, Gift: p.Gift,
		LibraryURL: core.LibraryURL(p.Gift), Matched: matched, ID: p.ID, DLs: p.Downloads}
	for _, d := range p.Downloads {
		got := ""
		if r := st.Downloaded[d]; r != nil {
			got = r.Path
		}
		v.Got = append(v.Got, got)
	}
	if p.Source == "gumroad" {
		v.Source, v.ProductURL, v.Variant, v.Note = p.Source, p.PageURL, p.Variant, p.Note
		v.LibraryURL, v.PageURL = core.GumroadLibraryURL(), p.DLPage
		if v.PageURL == "" {
			v.PageURL = v.LibraryURL
		}
		for _, o := range p.Orders {
			v.Orders = append(v.Orders, OrderView{Date: o.Date, URL: v.PageURL})
		}
		return v
	}
	for _, o := range p.Orders {
		v.Orders = append(v.Orders, OrderView{ID: o.ID, Date: o.Date, URL: core.OrderURL(o.ID)})
	}
	v.PageURL = v.LibraryURL
	if len(v.Orders) > 0 {
		v.PageURL = v.Orders[0].URL
	}
	return v
}

var reWordTok = regexp.MustCompile(`[a-z0-9]{4,}`)

// namesRelated: do two product names share a meaningful word (or two CJK characters in a row)?
func namesRelated(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	for _, t := range reWordTok.FindAllString(la, -1) {
		if !naming.IsMostlyDigits(t) && strings.Contains(lb, t) {
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

func BuildView(st *core.Store, a *core.Asset) AssetView {
	u := st.User[a.Key]
	v := AssetView{Key: a.Key, AutoName: a.Name, RawName: a.RawName, AutoCategory: a.Category, AutoBases: a.Bases,
		Locations: a.Locations, Size: a.Size, Files: a.Files, MTime: a.MTime, FirstSeen: a.FirstSeen, Usage: a.Usage,
		GuidCount: a.GuidCount, Packages: len(a.Packages), Archives: len(a.Archives) + a.ZipPackages + a.OtherArchives, LocalCovers: a.Covers, Hints: a.Hints, HasDir: a.HasDir,
		PSD: isPSDOnly(a), PSDs: a.PSDs, PSDCount: a.PSDCount, PSDInZip: a.PSDInZip}
	id, src := booth.AssetBooth(st, a.Key, a)
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
			v.Purchase = purchaseView(st, p, a.BoothFromLib)
			v.NewerOnBooth, v.LocalVer, v.NewerDL = purchaseNewerDL(a, p)
		}
	}
	// name
	v.Name = a.Name
	if v.Booth != nil && v.Booth.Name != "" && saysNothing(a.Name) {
		v.Name = v.Booth.Name
	} else if v.Purchase != nil && v.Purchase.Name != "" && saysNothing(a.Name) {
		v.Name = v.Purchase.Name
	}
	// category: booth (except keyword categories it lumps together) > auto
	v.Category = a.Category
	if v.Booth != nil && v.Booth.Category != "" {
		if bc, ok := naming.BoothCatMap[v.Booth.Category]; ok && !keywordWins[a.Category] && (bc != "其他" || a.Category == "其他") {
			v.Category = bc
		}
	}
	v.Bases = a.Bases
	extraText := ""
	if v.Booth != nil {
		extraText = booth.BoothBaseText(v.Booth)
	} else if v.Purchase != nil {
		extraText = v.Purchase.Name
	}
	defs := naming.ParseBases(st.Settings.Bases)
	if extraText != "" {
		extra := naming.DetectBases(extraText, defs)
		v.Bases = core.UniqStrings(append(append([]string{}, a.Bases...), extra...))
	}
	// base bodies only named as "For_X" in the file name
	v.Bases = core.UniqStrings(append(append([]string{}, v.Bases...), forBases(a.Name+" "+a.RawName, defs)...))
	v.AutoBases = v.Bases
	// cover: user > booth > purchase thumbnail > local image > made in Unity > unitypackage preview
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
	default: // no picture anywhere: the one made from a prefab in Unity, when there is one
		cover = GeneratedCover(a.Key)
		v.coverGen = cover != ""
		if cover == "" { // …or the preview found inside its unitypackage
			cover = PackageCover(st, a.Key)
			v.CoverPkg = cover != ""
		}
	}
	applyUserView(&v, cover)
	finishView(st, &v, a.Name)
	return v
}

// finishView fills what every kind of card shares: netdisk listing, Booth search words and
// suggestions, Chinese name.
func finishView(st *core.Store, v *AssetView, autoName string) {
	if surl := netdisk.ShareID(v.User.ShareURL); surl != "" && v.PanPath == "" {
		if l := st.Pan[surl]; l != nil {
			v.Pan = l
			v.PanParts = netdisk.AnalyzePan(l, naming.ParseBases(st.Settings.Bases)).Parts
		}
	}
	bases := naming.ParseBases(st.Settings.Bases)
	if v.PanOnly && (v.Pan == nil || v.Pan.Title == "") {
		v.BoothQuery = "" // the share has not been read yet: its placeholder name is not worth searching
	} else {
		v.BoothQuery = queryMemo.get(memoKey{defsID(bases), v.Category == "素体", autoName}, func() string {
			if q := booth.BoothQueryFor(autoName, bases, v.Category); q != "" {
				return q
			}
			return strings.TrimSpace(strings.NewReplacer("_", " ").Replace(naming.CleanName(autoName)))
		})
	}
	if v.BoothID == "" {
		if m := st.BoothMatch[v.Key]; m != nil {
			v.BoothHits = m.Hits
		}
	}
	v.AutoStyles = autoStyles(v, styleDefs(st.Settings.Styles))
	if v.User.StylesSet {
		v.Styles = v.User.Styles
	} else {
		v.Styles = v.AutoStyles
	}
	// news: new arrival, changes in its share or on its Booth page
	if st.ScanStart > 0 && v.FirstSeen > st.ScanStart+5 && time.Now().Unix()-v.FirstSeen < 3*86400 && !v.Virtual {
		v.New = true
	}
	if l := v.Pan; l != nil {
		if l.Err != "" && len(l.Files) > 0 {
			v.ShareErr = l.Err
		}
		if l.Changed > v.User.PanSeen {
			if added, removed := shareNews(l, ""); len(added)+len(removed) > 0 {
				v.PanNews = &PanNews{At: l.Changed, Added: added, Removed: removed}
			}
		}
	}
	if b := v.Booth; b != nil && b.Changed > 0 && b.Changed > v.User.BoothSeen {
		v.BoothNews, v.BoothNewsAt = b.ChangeNote, b.Changed
	}
	if v.User.NameZh != "" {
		v.NameZh = v.User.NameZh
	} else if src := zhSourceOfView(v); src != "" {
		v.NameZh = translate.ZHGlossary.Replace(st.Trans[src])
	}
}

var (
	queryMemo nameMemo[string]   // the Booth search words for a name
	zhMemo    nameMemo[string]   // translate.ZHSource of a name
	forMemo   nameMemo[[]string] // naming.ForBases of a name (read only)
	stemMemo  nameMemo[string]   // stemOf a name
	groupMemo nameMemo[string]   // groupName of the names in a group
	restMemo  nameMemo[string]   // what variantLabel leaves of a name
	styleMemo nameMemo[[]string] // autoStyles of a card's text (read only)
	plainMemo nameMemo[bool]     // saysNothing
)

// saysNothing: a folder name that gives way to the product's title (an item number, "衣服", two letters, an id).
func saysNothing(name string) bool {
	return plainMemo.get(memoKey{name: name}, func() bool {
		return naming.IsMostlyDigits(name) || len([]rune(name)) <= 2 || naming.IsGenericName(name) || isUUIDName(name)
	})
}

func zhSource(name string) string {
	return zhMemo.get(memoKey{name: name}, func() string { return translate.ZHSource(name) })
}

// forBases: naming.ForBases, remembered. The slice is shared: callers copy from it.
func forBases(text string, defs []naming.BaseDef) []string {
	return forMemo.get(memoKey{defs: defsID(defs), name: text}, func() []string { return naming.ForBases(text, defs) })
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

// AllViews: local assets, purchases that are not on disk, and netdisk-only assets. Caller holds st.mu.
func AllViews(st *core.Store) []AssetView {
	out := make([]AssetView, 0, len(st.Assets)+len(st.Purchases)+16)
	onDisk := map[string]bool{}
	// netdisk assets the program downloaded: the folder's card takes their place, with the share link
	fromPan := map[string]string{}
	now := time.Now().Unix()
	for k, u := range st.User {
		if core.IsNetdiskKey(k) && u.Downloaded != "" && isDirCached(u.Downloaded, now) {
			fromPan[core.PathKey(u.Downloaded)] = k
		}
	}
	claimed := map[string]bool{}
	for _, a := range st.Assets {
		v := BuildView(st, a)
		if v.Purchase != nil {
			onDisk[v.BoothID] = true
		}
		for _, l := range v.Locations {
			// the folder itself, or (holding several products, it is scanned as several cards) a folder inside it
			k, ok := "", false
			for p, up := core.PathKey(l.Path), 0; up < 6 && !ok; up++ {
				if k, ok = fromPan[p]; !ok {
					q := filepath.Dir(p)
					if q == p {
						break
					}
					p = q
				}
			}
			if ok && v.FromPan == "" {
				v.FromPan, claimed[k] = k, true
				v.PanGot = st.User[k].PanGot
				surl, _ := netdisk.SplitPanKey(k)
				if pu := st.User[netdisk.ShareKey(surl)]; pu != nil && v.User.ShareURL == "" {
					v.User.ShareURL, v.User.SharePwd = pu.ShareURL, pu.SharePwd
					// the share's file list stays, to download more of it later
					if pl, _ := netdisk.PanSub(st, k); pl != nil {
						v.Pan = pl
						v.PanParts = netdisk.AnalyzePan(pl, naming.ParseBases(st.Settings.Bases)).Parts
					}
				}
			}
		}
		out = append(out, v)
	}
	for _, key := range core.SortedKeys(st.User) {
		if !core.IsPanShareKey(key) || claimed[key] {
			continue
		}
		v := panOnlyView(st, key)
		// a collection: one card per product inside, the share itself only for its details panel
		if items := netdisk.PanItemsFor(st, key); items != nil {
			v.SplitInto = len(items)
			out = append(out, v)
			surl, _ := netdisk.SplitPanKey(key)
			for _, it := range items {
				if claimed[netdisk.PanItemKey(surl, it.Path)] {
					continue
				}
				iv := panOnlyView(st, netdisk.PanItemKey(surl, it.Path))
				if iv.Purchase != nil {
					onDisk[iv.BoothID] = true
				}
				out = append(out, iv)
			}
			continue
		}
		if u := st.User[key]; u != nil && u.NoSplit {
			surl, _ := netdisk.SplitPanKey(key)
			if n := len(netdisk.SplitPanCached(st.Pan[surl])); n >= 2 {
				v.CanSplit = n
			}
		}
		if v.Purchase != nil {
			onDisk[v.BoothID] = true
		}
		out = append(out, v)
	}
	for _, id := range core.SortedKeys(st.Purchases) {
		if !onDisk[id] {
			out = append(out, PurchaseOnlyView(st, st.Purchases[id]))
		}
	}
	groupViews(st, out)
	return out
}

func zhSources(st *core.Store) []string {
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var out []string
	for _, v := range AllViews(st) {
		if v.User.NameZh == "" {
			if s := zhSourceOfView(&v); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func panAssetName(st *core.Store, key string) string {
	surl, _ := netdisk.SplitPanKey(key)
	if l := st.Pan[surl]; l != nil && l.Title != "" {
		// "8099091" (an item number) or "新建文件夹" says nothing: use the name the files share
		if t := naming.CleanName(l.Title); naming.IsMostlyDigits(t) || naming.IsGenericName(t) || len([]rune(t)) <= 2 {
			if n := netdisk.AnalyzePan(l, naming.ParseBases(st.Settings.Bases)).Name; n != "" && !naming.IsMostlyDigits(n) {
				return n
			}
		}
		return l.Title
	}
	return netdisk.SharePlaceholder(surl)
}

// panOnlyView: an asset that only lives in a Baidu Netdisk share.
func panOnlyView(st *core.Store, key string) AssetView {
	surl, path := netdisk.SplitPanKey(key)
	parentKey := netdisk.ShareKey(surl)
	u, pu := st.User[key], st.User[parentKey]
	l, it := netdisk.PanSub(st, key)
	name, raw := "", ""
	if it != nil {
		raw = it.Node.Name
		name = naming.CleanName(core.StripArchiveExt(raw))
	} else {
		name = panAssetName(st, key)
		raw = name
	}
	defs := naming.ParseBases(st.Settings.Bases)
	var info netdisk.PanInfo
	if l != nil {
		info = netdisk.AnalyzePan(l, defs)
	}
	if it != nil && (naming.IsMostlyDigits(name) || naming.IsGenericName(name)) && info.Name != "" && !naming.IsMostlyDigits(info.Name) {
		name = info.Name
	}
	v := AssetView{Key: key, Name: name, AutoName: name, RawName: raw, PanOnly: true}
	if u != nil {
		v.User = *u
	}
	if it != nil {
		v.PanParent, v.PanPath, v.Hints = parentKey, path, it.Hints
		if pu != nil { // the link lives with the share
			v.User.ShareURL, v.User.SharePwd = pu.ShareURL, pu.SharePwd
		}
	}
	// when it was added (not when it was last edited)
	v.FirstSeen = st.FirstSeen[key]
	if v.FirstSeen == 0 && it != nil {
		v.FirstSeen = st.FirstSeen[parentKey]
	}
	text := name
	if l != nil {
		v.Size, v.Files = l.Size, l.Count
		text += " " + strings.Join(netdisk.PanNames(l, 300), " ")
		v.PSD = info.AllPSD
	}
	cat := naming.BracketCategory(name)
	if cat == "" && it != nil {
		cat = netdisk.PanItemCategory(it.Hints) // the 衣服 / 头发 / 妆容 folder it was found in
	}
	if cat == "" {
		cat = naming.Classify(name)
	}
	if cat == "其他" {
		// the downloads themselves, not the PSD / material packs next to them
		ct := strings.Join(info.Wrappers, " ")
		for _, p := range info.Parts {
			if p.Kind == "variant" {
				ct += " " + p.Name
			}
		}
		if cat = naming.Classify(ct); cat == "其他" {
			cat = naming.Classify(text)
		}
	}
	id, src := booth.AssetBooth(st, key, nil)
	v.BoothID, v.BoothSrc = id, src
	if b := st.Booth[id]; id != "" && b != nil {
		bc := *b
		v.Booth = &bc
		if bcat, ok := naming.BoothCatMap[b.Category]; ok && !keywordWins[cat] && (bcat != "其他" || cat == "其他") {
			cat = bcat
		}
	}
	if p := st.Purchases[id]; id != "" && p != nil {
		v.Purchase = purchaseView(st, p, false)
	}
	// like a folder on disk: an item number or a generic name gives way to the Booth title
	if t := naming.CleanName(name); naming.IsMostlyDigits(t) || naming.IsGenericName(t) || len([]rune(t)) <= 2 {
		if v.Booth != nil && v.Booth.Name != "" {
			v.Name = v.Booth.Name
		} else if v.Purchase != nil && v.Purchase.Name != "" {
			v.Name = v.Purchase.Name
		}
	}
	v.Category, v.AutoCategory = cat, cat
	v.Bases = core.UniqStrings(append(append(naming.DetectBases(text+" "+booth.BoothBaseText(v.Booth), defs), forBases(v.AutoName, defs)...), info.Bases...))
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
	if it != nil { // its own part of the share, not the whole share
		v.Pan, v.PanParts = l, info.Parts
		v.PanNews = nil
		if whole := st.Pan[surl]; whole != nil && whole.Changed > v.User.PanSeen {
			if added, removed := shareNews(whole, path); len(added)+len(removed) > 0 {
				v.PanNews = &PanNews{At: whole.Changed, Added: added, Removed: removed}
			}
		}
	}
	return v
}

// PurchaseOnlyView: something bought on Booth that is not in any scanned folder.
func PurchaseOnlyView(st *core.Store, p *core.Purchase) AssetView {
	key := "purchase:" + p.ID
	v := AssetView{Key: key, Name: p.Name, AutoName: p.Name, RawName: p.Name, BoothID: p.ID, BoothSrc: "library", Virtual: true,
		FirstSeen: p.When(), Purchase: purchaseView(st, p, false)}
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
	cat := naming.BracketCategory(p.Name)
	if cat == "" {
		cat = naming.Classify(p.Name)
	}
	if v.Booth != nil {
		if bc, ok := naming.BoothCatMap[v.Booth.Category]; ok && !keywordWins[cat] && (bc != "其他" || cat == "其他") {
			cat = bc
		}
	}
	v.Category, v.AutoCategory = cat, cat
	defs := naming.ParseBases(st.Settings.Bases)
	v.Bases = core.UniqStrings(append(naming.DetectBases(text+" "+booth.BoothBaseText(v.Booth), defs), forBases(v.AutoName, defs)...))
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
		q := url.QueryEscape(cover) // once for both sizes
		v.Cover = "/thumb?w=420&p=" + q
		v.CoverBig = "/thumb?w=900&p=" + q
	}
}
