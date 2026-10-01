package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Settings struct {
	Roots         []string `json:"roots"`
	ProjectRoots  []string `json:"projectRoots"`
	Bases         []string `json:"bases"`
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
	Status  string  `json:"status"` // used, partial
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
	mu        sync.RWMutex
	path      string
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

	Pan        map[string]*PanListing `json:"pan"`        // share id → what is inside
	BoothMatch map[string]*BoothMatch `json:"boothMatch"` // asset key → Booth search result
	Trans      map[string]string      `json:"trans"`      // text → simplified Chinese

	ScanStart     int64                `json:"scanStart,omitempty"` // first scan ever: what came later is "new"
	Update        *UpdateInfo          `json:"update,omitempty"`    // latest release seen on GitHub
	UpdateChecked int64                `json:"updateChecked,omitempty"`
	NotesSeen     string               `json:"notesSeen,omitempty"`  // the version whose "what changed" was last shown
	Downloaded    map[string]*DLRecord `json:"downloaded,omitempty"` // Booth files downloaded here, by downloadable id
	AutoDLDir     string               `json:"autoDlDir,omitempty"`  // the download folder picked automatically, once used
}

func defaultSettings() Settings {
	return Settings{
		Bases: []string{
			"Plum=plum|プラム", "Chocolat=chocolat|ショコラ", "Chiffon=chiffon|シフォン", "Lime=lime|ライム",
			"Kaguya=kaguya|辉夜|輝夜", "Manuka=manuka|マヌカ", "Karin=karin|カリン", "Shinano=shinano|しなの",
			"Rusk=rusk|ラスク", "Mamehinata=mamehinata|まめひなた", "Lasyusha=lasyusha|ラシューシャ",
			"Kikyo=kikyo|桔梗", "Selestia=selestia|セレスティア", "Airi=airi|愛莉", "Maya=maya|舞夜",
			"Uzuki=uzuki|卯月", "Rindo=rindo|竜胆", "Hakka=hakka|薄荷", "Grus=grus|グルス", "Milltina=milltina|ミルティナ",
			"Mizuki=mizuki|瑞希", "Sio=sio|しお", "Moe=moe|萌", "Sue=sue|スウ", "Shiratsume=shiratsume|白狼",
			"Rurune=rurune|ルルネ", "Chise=chise|チセ", "Kuuta=kuuta|空太", "Leefa=leefa|リーファ", "Mafuyu=mafuyu|真冬",
			"Lzebul=lzebul|ルゼブル", "Wolferia=wolferia|ウルフェリア", "Ririka=ririka|リリカ", "Imeris=imeris|イメリス",
		},
		AutoBooth: true,
		Styles:    append([]string{}, defaultStyles...),
	}
}

func LoadStore(path string) *Store {
	s := &Store{path: path, Version: 1, Settings: defaultSettings(), User: map[string]*UserData{},
		Booth: map[string]*BoothInfo{}, Overrides: map[string]string{}, FirstSeen: map[string]int64{},
		Purchases: map[string]*Purchase{}}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, s); err != nil {
			logf("library.json 解析失败，已备份: %v", err)
			_ = os.WriteFile(path+".broken-"+time.Now().Format("20060102-150405"), b, 0644)
		}
	}
	s.path = path
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
		if isPanShareKey(k) && s.FirstSeen[k] == 0 && u != nil {
			s.FirstSeen[k] = min(u.Updated, s.ScanStart)
		}
	}
	if s.Settings.Styles == nil {
		s.Settings.Styles = append([]string{}, defaultStyles...)
	}
	if len(s.Settings.Bases) == 0 {
		s.Settings.Bases = defaultSettings().Bases
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

// Save writes atomically. Caller must not hold the write lock (takes read lock).
func (s *Store) Save() error {
	s.mu.RLock()
	b, err := json.MarshalIndent(s, "", " ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	// keep one rolling backup
	if _, err := os.Stat(s.path); err == nil {
		_ = os.Remove(s.path + ".bak")
		_ = os.Rename(s.path, s.path+".bak")
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) userFor(a *Asset) *UserData {
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

func sortedKeys[M ~map[string]V, V any](m M) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

var dataDir string

func coversDir() string { return filepath.Join(dataDir, "covers") }
