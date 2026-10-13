package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Settings struct {
	Roots         []string `json:"roots"`
	ProjectRoots  []string `json:"projectRoots"`
	Bases         []string `json:"bases"`
	BasesAdded    int      `json:"basesAdded,omitempty"` // how many of BaseAdditions the table has been given (it keeps what the player took out)
	Proxy         string   `json:"proxy"`
	AutoBooth     bool     `json:"autoBooth"`
	ManualRescan  bool     `json:"manualRescan"` // false: rescan in the background every time the app opens
	SetupDone     bool     `json:"setupDone"`
	WindowMode    string   `json:"windowMode"` // "" = the app's own window (WebView2), "tab" = a tab in the default browser
	HideZh        bool     `json:"hideZh"`     // hide the Chinese translation line
	NoAutoMatch   bool     `json:"noAutoMatch"`
	Styles        []string `json:"styles"`        // outfit style tags: "名称=Booth 标签或关键词|…"
	NoUpdateCheck bool     `json:"noUpdateCheck"` // do not look for new releases on start
	SkipVersion   string   `json:"skipVersion"`   // "remind me no more" for this release
	NoWatch       bool     `json:"noWatch"`       // do not rescan by itself when the asset folders change
	NoSync        bool     `json:"noSync"`        // do not re-read shares / Booth pages to spot updates
	DownloadDir   string   `json:"downloadDir"`   // where Booth purchases are downloaded ("" = first asset folder)
	NoExtract     bool     `json:"noExtract"`     // keep downloaded zips packed
	KeepZip       bool     `json:"keepZip"`       // keep the zip after unpacking it
	Lang          string   `json:"lang"`          // the interface's language: "" = Chinese, "en", "ja" (web/i18n.js)
	// 闲鱼's pages (webpane/xyview.go), and the links the player copies. Changed through /api/xy/prefs only.
	XyExternal bool `json:"xyExternal"` // open them in the default browser, not in the window
	XyNoticed  bool `json:"xyNoticed"`  // the note about how they open now has been read
	NoXyClip   bool `json:"noXyClip"`   // do not offer the 闲鱼, Booth and netdisk links the player copies (named when it was the 闲鱼 tab's alone)
}

type Location struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // dir, zip, rar, 7z, unitypackage, file
	Size int64  `json:"size"`
	Root string `json:"root"`
}

type Usage struct {
	Project string  `json:"project"`
	Matched int     `json:"matched"`
	Total   int     `json:"total"`
	Ratio   float64 `json:"ratio"`
	Status  string  `json:"status"`           // used, partial
	Folder  string  `json:"folder,omitempty"` // Assets/<shop>/<item> where most of its files are
}

type Asset struct {
	Key          string     `json:"key"`
	AltKey       string     `json:"altKey"`
	Name         string     `json:"name"`
	RawName      string     `json:"rawName"`
	Category     string     `json:"category"`
	Bases        []string   `json:"bases"`
	BoothID      string     `json:"boothId"`
	BoothFromURL bool       `json:"boothFromUrl,omitempty"`
	BoothFromLib bool       `json:"boothFromLib,omitempty"` // matched to a Booth purchase by download file name
	Hints        []string   `json:"hints"`
	Locations    []Location `json:"locations"`
	Size         int64      `json:"size"`
	Files        int        `json:"files"`
	Packages     []string   `json:"packages"`
	Archives     []string   `json:"archives"`
	MetaDirs     []string   `json:"metaDirs"`
	Covers       []string   `json:"covers"`
	MTime        int64      `json:"mtime"`
	FirstSeen    int64      `json:"firstSeen"`
	Usage        []Usage    `json:"usage"`
	GuidCount    int        `json:"guidCount"`
	HasDir       bool       `json:"hasDir"`
	// what is inside: texture sources (PSD / CLIP …) and Unity content, to tell source packs apart
	PSDs        []PSDFile `json:"psds,omitempty"`
	PSDCount    int       `json:"psdCount,omitempty"`
	PSDInZip    int       `json:"psdInZip,omitempty"`
	ModelFiles  int       `json:"modelFiles,omitempty"`
	ZipPackages int       `json:"zipPackages,omitempty"`
	// rar / 7z / split volumes (unpacked only for an import)
	OtherArchives int `json:"otherArchives,omitempty"`
	// a collection of products in one card (合集包): how many products, and whether some of them are still
	// inside archives. It is split into a card for each before anything is imported (library/bundle.go).
	Bundle       int  `json:"bundle,omitempty"`
	BundlePacked bool `json:"bundlePacked,omitempty"`
}

// PSDFile is one texture source file (PSD, PSB, CLIP, SAI …) found in an asset folder.
type PSDFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	W    int    `json:"w,omitempty"`
	H    int    `json:"h,omitempty"`
}

type UserData struct {
	Name     string   `json:"name,omitempty"`
	Category string   `json:"category,omitempty"`
	Bases    []string `json:"bases,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Notes    string   `json:"notes,omitempty"`
	ShareURL string   `json:"shareUrl,omitempty"`
	SharePwd string   `json:"sharePwd,omitempty"`
	PanPath  string   `json:"panPath,omitempty"`
	BoothURL string   `json:"boothUrl,omitempty"`
	NoBooth  bool     `json:"noBooth,omitempty"` // "not a Booth product": no automatic matching
	NameZh   string   `json:"nameZh,omitempty"`  // hand-written Chinese name
	Cover    string   `json:"cover,omitempty"`
	Hidden   bool     `json:"hidden,omitempty"`
	Fav      bool     `json:"fav,omitempty"`
	Updated  int64    `json:"updated,omitempty"`
	// style tags picked by hand (StylesSet); otherwise they come from the Booth tags
	Styles    []string `json:"styles,omitempty"`
	StylesSet bool     `json:"stylesSet,omitempty"`
	NoGroup   bool     `json:"noGroup,omitempty"`   // keep it out of "same product" cards
	NoSplit   bool     `json:"noSplit,omitempty"`   // a netdisk collection shown as one card
	PanSeen   int64    `json:"panSeen,omitempty"`   // share changes up to then have been looked at
	BoothSeen int64    `json:"boothSeen,omitempty"` // Booth page changes up to then have been looked at
	// a netdisk asset downloaded by the program: its folder (the card gives way to the folder's)
	Downloaded  string `json:"downloaded,omitempty"`
	DownloadDir string `json:"downloadDir,omitempty"` // a download under way (or stopped) goes on here
	// parts of the card's file list ("/folder/file.zip"; "/" = all of it) saved completely into the copy in the
	// player's netdisk (PanCopy), and downloaded from there
	PanCopy  string   `json:"panCopy,omitempty"`
	PanSaved []string `json:"panSaved,omitempty"`
	PanGot   []string `json:"panGot,omitempty"`
}

type BoothInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Shop     string   `json:"shop"`
	ShopURL  string   `json:"shopUrl"`
	Price    string   `json:"price"`
	Category string   `json:"category"`
	URL      string   `json:"url"`
	ImageURL string   `json:"imageUrl"`
	Cover    string   `json:"cover"` // local file path
	Fetched  int64    `json:"fetched"`
	Err      string   `json:"err,omitempty"`
	Gone     bool     `json:"gone,omitempty"` // 404 on Booth: do not keep retrying
	Desc     string   `json:"desc,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Images   []string `json:"images,omitempty"`
	Ver      int      `json:"ver,omitempty"` // see boothInfoVer
	Adult    bool     `json:"adult,omitempty"`
	// what the shop changed, noticed at a later fetch
	Changed    int64  `json:"changed,omitempty"`
	ChangeNote string `json:"changeNote,omitempty"`
}

type ProjectInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Guids  int    `json:"guids"`
	Assets int    `json:"assets"`
}

type Store struct {
	Mu        sync.RWMutex          `json:"-"`
	Path      string                `json:"-"`
	Version   int                   `json:"version"`
	Settings  Settings              `json:"settings"`
	Assets    []*Asset              `json:"assets"`
	User      map[string]*UserData  `json:"user"`
	Booth     map[string]*BoothInfo `json:"booth"`
	Overrides map[string]string     `json:"overrides"`
	Projects  []ProjectInfo         `json:"projects"`
	FirstSeen map[string]int64      `json:"firstSeen"`
	LastScan  int64                 `json:"lastScan"`
	LastUsage int64                 `json:"lastUsage"`
	Warnings  []string              `json:"warnings"`

	Purchases    map[string]*Purchase `json:"purchases"`
	PurchaseSync int64                `json:"purchaseSync"`
	GumroadSync  int64                `json:"gumroadSync,omitempty"` // Gumroad purchases live in Purchases too (ids "gr_…")

	Pan        map[string]*PanListing `json:"pan"`        // share id → what is inside
	BoothMatch map[string]*BoothMatch `json:"boothMatch"` // asset key → Booth search result
	Trans      map[string]string      `json:"trans"`      // text → simplified Chinese

	ScanStart     int64                `json:"scanStart,omitempty"` // first scan ever: what came later is "new"
	Update        *UpdateInfo          `json:"update,omitempty"`    // latest release seen on GitHub
	UpdateChecked int64                `json:"updateChecked,omitempty"`
	NotesSeen     string               `json:"notesSeen,omitempty"`  // the version whose "what changed" was last shown
	Downloaded    map[string]*DLRecord `json:"downloaded,omitempty"` // Booth files downloaded here, by downloadable id
	AutoDLDir     string               `json:"autoDlDir,omitempty"`  // the download folder picked automatically, once used

	// saving: one writer at a time, and what is known about the files on disk
	saveMu    sync.Mutex
	saveAsked atomic.Int64 // Save calls so far
	saveDone  int64        // the last of them the file on disk covers
	saveErr   error
	mainGood  bool          // library.json on disk is a whole library: read at start, or written by this run
	bakAt     time.Time     // when library.json.bak was last refreshed by this run
	bakWait   time.Duration // and how long until it may be again
	// library files that were there at the start but could not be read (so not known to be damaged): each is
	// renamed to this name before anything is written in its place — library.json before a save, and nothing
	// is saved while that fails; library.json.bak before the backup is refreshed. "" once out of the way.
	mainAside string
	bakAside  string
	noteMu    sync.Mutex
	notices   []string // shown with the scan's warnings, not saved: the library came from the backup …
	unread    string   // the notice of a library.json that could not be read, up to where the file is
	unreadAs  string   // … which is this name, once it has been renamed
	saveNote  string   // the notice of a save that is failing
}

func DefaultSettings() Settings {
	bases := []string{
		"Plum=plum|プラム", "Chocolat=chocolat|ショコラ", "Chiffon=chiffon|シフォン", "Lime=lime|ライム",
		"Kaguya=kaguya|辉夜|輝夜", "Manuka=manuka|マヌカ", "Karin=karin|カリン", "Shinano=shinano|しなの",
		"Rusk=rusk|ラスク", "Mamehinata=mamehinata|まめひなた", "Lasyusha=lasyusha|ラシューシャ",
		"Kikyo=kikyo|桔梗", "Selestia=selestia|セレスティア", "Airi=airi|愛莉", "Maya=maya|舞夜",
		"Uzuki=uzuki|卯月", "Rindo=rindo|竜胆", "Hakka=hakka|薄荷", "Grus=grus|グルス", "Milltina=milltina|ミルティナ",
		"Mizuki=mizuki|瑞希", "Sio=sio|しお", "Moe=moe|萌", "Sue=sue|スウ", "Shiratsume=shiratsume|白狼",
		"Rurune=rurune|ルルネ", "Chise=chise|チセ", "Kuuta=kuuta|空太", "Leefa=leefa|リーファ", "Mafuyu=mafuyu|真冬",
		"Lzebul=lzebul|ルゼブル", "Wolferia=wolferia|ウルフェリア", "Ririka=ririka|リリカ", "Imeris=imeris|イメリス",
	}
	for _, add := range BaseAdditions {
		bases = append(bases, add...)
	}
	return Settings{
		Bases:      bases,
		BasesAdded: len(BaseAdditions),
		AutoBooth:  true,
		Styles:     append([]string{}, DefaultStyles...),
	}
}

// BaseAdditions: the base bodies added to the table since it was first written, a list for each version that
// added some. A library made before has them added once (MergeBaseAdditions); one the player took out again is
// not put back. (Short names in kana that are part of common words — エク, ミヨ, ユギ, ミルフィ, ルミナ — are
// left to their Latin spelling.)
var BaseAdditions = [][]string{
	{ // 1.7.13
		"Kipfel=kipfel|キプフェル", "Anomea=anomea|あのめあ", "Nemesis=nemesis|ネメシス", "ELusion=elusion|エルシオン",
		"Hakslea=hakslea|ハクスレア", "Milfy=milfy", "Lumina=lumina", "Alue=alue|alué|アルエ", "Yugi=yugi", "Miyo=miyo", "Eku=eku",
	},
}

// MergeBaseAdditions adds to a table the base bodies added since it was given the last of them (from), unless it
// has them already, under their name or one of their aliases.
func MergeBaseAdditions(bases []string, from int) []string {
	have := map[string]bool{}
	words := func(row string) []string {
		name, rest, _ := strings.Cut(row, "=")
		out := []string{strings.ToLower(strings.TrimSpace(name))}
		for _, a := range strings.Split(rest, "|") {
			if a = strings.ToLower(strings.TrimSpace(a)); a != "" {
				out = append(out, a)
			}
		}
		return out
	}
	for _, row := range bases {
		for _, w := range words(row) {
			have[w] = true
		}
	}
	out := append([]string{}, bases...)
	for _, list := range BaseAdditions[min(max(from, 0), len(BaseAdditions)):] {
		for _, row := range list {
			known := false
			for _, w := range words(row) {
				known = known || have[w]
			}
			if !known {
				out = append(out, row)
			}
		}
	}
	return out
}

func newStore(path string) *Store {
	return &Store{Path: path, Version: 1, Settings: DefaultSettings(), User: map[string]*UserData{},
		Booth: map[string]*BoothInfo{}, Overrides: map[string]string{}, FirstSeen: map[string]int64{},
		Purchases: map[string]*Purchase{}}
}

// readPatience: how long a library file that is there but cannot be read (held by a virus scanner or a sync
// client, a file in the cloud being fetched) is tried again before this start does without it.
var readPatience = 3 * time.Second

// readStore reads one library file. What was read comes back too, for a copy of a file that is damaged; nil
// when nothing could be read (then the file is not known to be damaged).
func readStore(path, file string) (*Store, []byte, error) {
	b, err := readPatiently(file, readPatience)
	if err != nil {
		return nil, nil, err
	}
	if b == nil {
		b = []byte{} // (read, and empty)
	}
	s := newStore(path)       // a fresh one each time: a file that fails half way leaves nothing behind
	s.Settings.BasesAdded = 0 // (a file that does not say it has been given none: its table is as it was made)
	if !bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		return nil, b, errors.New("not a library file")
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, b, err
	}
	return s, b, nil
}

// LoadStore reads library.json. When it is missing or damaged the rolling backup (library.json.bak) is
// used instead, and the window is told. A damaged file is kept as ".broken-<time>" and never deleted. One that
// is there but cannot be read (held by another program, in the cloud and not to be fetched, no access) is not
// known to be damaged: it is left as it is, and renamed to ".unread-<time>" before anything is saved in its
// place — nothing is saved while that cannot be done.
func LoadStore(path string) *Store {
	stamp := time.Now().Format("20060102-150405")
	keep := func(file string, b []byte) bool { // a copy of a damaged file: was one written?
		return len(b) > 0 && os.WriteFile(file+".broken-"+stamp, b, 0644) == nil
	}
	s, raw, err := readStore(path, path)
	switch {
	case err == nil:
		s.mainGood = true
	case os.IsNotExist(err) && !StatOK(path+".bak"):
		s = newStore(path) // a first start
	default:
		damaged, unread := raw != nil, raw == nil && !os.IsNotExist(err)
		kept, keptMain := false, false // a copy of a damaged file was written; of library.json
		what := "素材库文件缺失"
		switch {
		case damaged:
			what = "素材库文件已损坏"
			if kept = keep(path, raw); kept {
				keptMain = true
				Logf("library.json 无法读取（%v），原文件另存为 library.json.broken-%s", err, stamp)
			} else {
				Logf("library.json 无法读取（%v），未能另存副本", err)
			}
		case unread:
			what = "素材库文件暂时无法读取（" + TrimErr(err) + "）"
			Logf("library.json 无法读取（%v），原文件保持原样，保存前先改名为 library.json.unread-%s", err, stamp)
		}
		note := ""
		if bak, bakRaw, bakErr := readStore(path, path+".bak"); bakErr == nil {
			s = bak
			when := ""
			if fi, e := os.Stat(path + ".bak"); e == nil {
				when = fi.ModTime().Format("2006-01-02 15:04")
			}
			Logf("%s，已从备份 library.json.bak 恢复（备份时间 %s）", what, when)
			note = what + "，已从备份恢复（备份时间 " + when + "）；该时间之后的改动未能保留"
			if unread { // (what came after it is in the file that could not be read)
				note = what + "，已用 " + when + " 的备份启动"
			}
		} else {
			s = newStore(path)
			switch {
			case os.IsNotExist(bakErr):
			case bakRaw == nil: // there, but not readable either: not written over when the backup is refreshed
				s.bakAside = path + ".bak.unread-" + stamp
				Logf("备份 library.json.bak 也无法读取（%v），保持原样", bakErr)
			case keep(path+".bak", bakRaw):
				kept = true
				Logf("备份 library.json.bak 也无法读取（%v），原文件另存为 library.json.bak.broken-%s", bakErr, stamp)
			default:
				Logf("备份 library.json.bak 也无法读取（%v），未能另存副本", bakErr)
			}
			note = what + "，且没有可用的备份，已按全新素材库启动"
			if kept {
				note += "；损坏的文件保留在数据文件夹中（文件名以 .broken-" + stamp + " 结尾）"
			}
		}
		switch { // (the notice says where the file is: Notices)
		case unread:
			s.mainAside, s.unread = path+".unread-"+stamp, note
		case damaged && !keptMain: // no copy of it could be written: it is not written over either, but set aside
			s.mainAside, s.unread = path+".broken-"+stamp, note
		default:
			s.notices = append(s.notices, note)
		}
	}
	s.Path = path
	if s.User == nil {
		s.User = map[string]*UserData{}
	}
	if s.Booth == nil {
		s.Booth = map[string]*BoothInfo{}
	}
	if s.Overrides == nil {
		s.Overrides = map[string]string{}
	}
	if s.FirstSeen == nil {
		s.FirstSeen = map[string]int64{}
	}
	if s.ScanStart == 0 && s.LastScan > 0 {
		s.ScanStart = s.LastScan // libraries from before 1.5: nothing counts as new yet
	}
	for k, u := range s.User { // shares added before 1.5: when they were added is not known any more
		if IsPanShareKey(k) && s.FirstSeen[k] == 0 && u != nil {
			s.FirstSeen[k] = min(u.Updated, s.ScanStart)
		}
	}
	if s.Settings.Styles == nil {
		s.Settings.Styles = append([]string{}, DefaultStyles...)
	}
	if len(s.Settings.Bases) == 0 {
		s.Settings.Bases, s.Settings.BasesAdded = DefaultSettings().Bases, len(BaseAdditions)
	}
	if s.Settings.BasesAdded < len(BaseAdditions) { // base bodies added to the table since this library was made
		s.Settings.Bases = MergeBaseAdditions(s.Settings.Bases, s.Settings.BasesAdded)
		s.Settings.BasesAdded = len(BaseAdditions)
	}
	if s.Purchases == nil {
		s.Purchases = map[string]*Purchase{}
	}
	if s.Pan == nil {
		s.Pan = map[string]*PanListing{}
	}
	if s.BoothMatch == nil {
		s.BoothMatch = map[string]*BoothMatch{}
	}
	if s.Trans == nil {
		s.Trans = map[string]string{}
	}
	// libraries created before the setup screen existed are already set up
	if !s.Settings.SetupDone && len(s.Settings.Roots) > 0 {
		s.Settings.SetupDone = true
	}
	return s
}

// Notices: what the window shows above the scan's warnings (the library came from the backup, saving fails).
func (s *Store) Notices() []string {
	s.noteMu.Lock()
	defer s.noteMu.Unlock()
	out := append([]string{}, s.notices...)
	if s.unread != "" { // and where the library.json that could not be read is now
		if s.unreadAs != "" {
			as := "；原文件已改名保留为 " + filepath.Base(s.unreadAs)
			if strings.Contains(filepath.Base(s.unreadAs), ".unread-") { // (what it holds may be newer than the backup)
				as += "，如需找回其中的内容，请先关闭本程序，再将它改名为 library.json"
			}
			out = append(out, s.unread+as)
		} else {
			out = append(out, s.unread+"；原文件保持原样，不会被覆盖")
		}
	}
	if s.saveNote != "" {
		out = append(out, s.saveNote)
	}
	return out
}

// library.json.bak is refreshed by the first save of a run (with the library as it was at the start), then
// after bakFirst, twice that, … up to every bakEvery: a library that is being set up is not left with a
// backup from before anything was in it, and a large one is not copied at every save.
const (
	bakFirst = 15 * time.Second
	bakEvery = 5 * time.Minute
)

// Save writes the library to disk: one save at a time, to a temporary file that then replaces
// library.json in one step, so the file is whole at every moment. Caller must not hold the write lock
// (takes the read lock). A save that fails is logged here and shown in the window; callers need not.
func (s *Store) Save() error {
	ticket := s.saveAsked.Add(1)
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.saveDone >= ticket {
		return s.saveErr // a save that started after this call was made has written the same state
	}
	upTo := s.saveAsked.Load()
	err := s.write()
	s.saveDone, s.saveErr = upTo, err
	note := ""
	if err != nil {
		note = "素材库保存失败，最近的改动尚未写入磁盘（" + TrimErr(err) + "）"
	}
	s.noteMu.Lock()
	changed := note != s.saveNote
	s.saveNote = note
	s.noteMu.Unlock()
	if changed { // once per problem, not once per save
		if err != nil {
			Logf("library.json 保存失败: %v", err)
		} else {
			Logf("library.json 已恢复保存")
		}
		BumpRev()
	}
	return err
}

func (s *Store) write() error {
	// a library.json that could not be read at the start goes out of the way first; while it cannot, nothing
	// is written (it may hold more than what this run started from)
	if s.mainAside != "" {
		kept, err := putAside(s.Path, s.mainAside)
		if err != nil {
			return errors.New("原素材库文件无法改名保留，为免覆盖暂不保存：" + TrimErr(err))
		}
		s.noteMu.Lock()
		s.mainAside, s.unreadAs = "", kept
		s.noteMu.Unlock()
		if kept != "" {
			Logf("无法读取的 library.json 已改名保留为 %s", filepath.Base(kept))
			BumpRev() // (the notice says where it is now)
		}
	}
	s.Mu.RLock()
	b, err := json.MarshalIndent(s, "", " ")
	s.Mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := writeSynced(tmp, bytes.NewReader(b)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// the rolling backup: a copy of the library.json about to be replaced, and only of one known to be whole —
	// never over a library.json.bak that could not be read at the start, which goes out of the way first
	if s.mainGood && time.Since(s.bakAt) >= s.bakWait {
		var err error
		if s.bakAside != "" {
			var kept string
			if kept, err = putAside(s.Path+".bak", s.bakAside); err == nil {
				s.bakAside = ""
				if kept != "" {
					Logf("无法读取的 library.json.bak 已改名保留为 %s", filepath.Base(kept))
				}
			}
		}
		if err == nil {
			err = copySynced(s.Path, s.Path+".bak")
		}
		if err != nil {
			Logf("library.json.bak 没能更新: %v", err)
		}
		s.bakAt = time.Now() // (not tried again at every save when it fails)
		s.bakWait = min(bakEvery, max(bakFirst, 2*s.bakWait))
	}
	if err := replaceFile(tmp, s.Path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.mainGood = true
	return nil
}

// writeSynced writes a file and has it reach the disk before returning.
func writeSynced(file string, r io.Reader) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// copySynced puts a copy of src at dst, which is whole at every moment too.
func copySynced(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := writeSynced(dst+".tmp", in); err != nil {
		_ = os.Remove(dst + ".tmp")
		return err
	}
	return replaceFile(dst+".tmp", dst)
}

// replaceFile renames src onto dst in one step (on Windows too: os.Rename replaces). A virus scanner or
// the search indexer holding dst open for a moment makes Windows refuse, so it is tried a few times.
func replaceFile(src, dst string) (err error) {
	for i := 0; ; i++ {
		if err = os.Rename(src, dst); err == nil || i == 3 {
			return err
		}
		time.Sleep(time.Duration(i+1) * 50 * time.Millisecond)
	}
}

// putAside renames a file that could not be read to the name given (or to one next to it, when that is taken),
// so that nothing is written over it: a rename can work where a read does not. It returns the name the file
// has now; "" when it is not there any more.
func putAside(file, to string) (string, error) {
	if _, err := os.Lstat(file); os.IsNotExist(err) {
		return "", nil
	}
	to = freeName(to)
	if err := replaceFile(file, to); err != nil {
		return "", err
	}
	return to, nil
}

// freeName: name, or the first of name-2, name-3 … that nothing has.
func freeName(name string) string {
	out := name
	for i := 2; i < 100; i++ {
		if _, err := os.Lstat(out); os.IsNotExist(err) {
			break
		}
		out = name + "-" + Itoa(i)
	}
	return out
}

// readPatiently reads a file, and tries again for up to patience while it is there but cannot be read.
func readPatiently(file string, patience time.Duration) ([]byte, error) {
	b, err := os.ReadFile(file)
	for end := time.Now().Add(patience); err != nil && !os.IsNotExist(err) && time.Now().Before(end); {
		time.Sleep(100 * time.Millisecond) // held by a virus scanner for a moment: not a damaged file
		b, err = os.ReadFile(file)
	}
	return b, err
}

// DataPatience: how long ReadDataFile tries again a file that is there but cannot be read.
var DataPatience = time.Second

// ReadDataFile reads a file of the data folder. One that is there but cannot be read for the moment (held by a
// virus scanner or a sync client, a file in the cloud being fetched) is tried again for DataPatience. An error
// that os.IsNotExist does not match means the file is there: nothing may be written over it.
func ReadDataFile(file string) ([]byte, error) { return readPatiently(file, DataPatience) }

// ReadDataFileAgain: ReadDataFile, for a file that is read at every use. One that could not be read the last
// time (failed) is tried once, without waiting: every use would wait for it otherwise, while it stays so.
func ReadDataFileAgain(file string, failed bool) ([]byte, error) {
	if failed {
		return os.ReadFile(file)
	}
	return ReadDataFile(file)
}

// KeepBroken moves a data file that was read but cannot be parsed out of the way, as "<file>.broken-<time>",
// before anything is written in its place; when it cannot be moved, what was read of it (b) is written there
// instead. It returns the name it is kept under ("" for an empty file: there is nothing to keep).
func KeepBroken(file string, b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	dst := freeName(file + ".broken-" + time.Now().Format("20060102-150405"))
	if os.Rename(file, dst) == nil {
		return dst, nil
	}
	if err := os.WriteFile(dst, b, 0644); err != nil {
		return "", err
	}
	return dst, nil
}

func (s *Store) UserFor(a *Asset) *UserData {
	if u, ok := s.User[a.Key]; ok {
		return u
	}
	if a.AltKey != "" {
		if u, ok := s.User[a.AltKey]; ok {
			// migrate to the primary key
			s.User[a.Key] = u
			delete(s.User, a.AltKey)
			return u
		}
	}
	return nil
}

func SortedKeys[M ~map[string]V, V any](m M) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

var DataDir string

func CoversDir() string { return filepath.Join(DataDir, "covers") }
