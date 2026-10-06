package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
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
	noteMu    sync.Mutex
	notices   []string // shown with the scan's warnings, not saved: the library came from the backup …
	saveNote  string   // the notice of a save that is failing
}

func DefaultSettings() Settings {
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
		Styles:    append([]string{}, DefaultStyles...),
	}
}

func newStore(path string) *Store {
	return &Store{Path: path, Version: 1, Settings: DefaultSettings(), User: map[string]*UserData{},
		Booth: map[string]*BoothInfo{}, Overrides: map[string]string{}, FirstSeen: map[string]int64{},
		Purchases: map[string]*Purchase{}}
}

// readStore reads one library file. What was read comes back too, for a copy of a file that is damaged.
func readStore(path, file string) (*Store, []byte, error) {
	b, err := os.ReadFile(file)
	for i := 0; i < 5 && err != nil && !os.IsNotExist(err); i++ {
		time.Sleep(200 * time.Millisecond) // held by a virus scanner for a moment: not a damaged file
		b, err = os.ReadFile(file)
	}
	if err != nil {
		return nil, nil, err
	}
	s := newStore(path) // a fresh one each time: a file that fails half way leaves nothing behind
	if !bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		return nil, b, errors.New("not a library file")
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, b, err
	}
	return s, b, nil
}

// LoadStore reads library.json. When it is missing or damaged the rolling backup (library.json.bak) is
// used instead, and the window is told. A damaged file is kept as ".broken-<time>" and never deleted.
func LoadStore(path string) *Store {
	stamp := time.Now().Format("20060102-150405")
	keep := func(file string, b []byte) {
		if len(b) > 0 {
			_ = os.WriteFile(file+".broken-"+stamp, b, 0644)
		}
	}
	s, raw, err := readStore(path, path)
	switch {
	case err == nil:
		s.mainGood = true
	case os.IsNotExist(err) && !StatOK(path+".bak"):
		s = newStore(path) // a first start
	default:
		damaged := !os.IsNotExist(err)
		if damaged {
			Logf("library.json 无法读取（%v），原文件另存为 library.json.broken-%s", err, stamp)
			keep(path, raw)
		}
		bak, bakRaw, bakErr := readStore(path, path+".bak")
		if bakErr != nil {
			if !os.IsNotExist(bakErr) {
				Logf("备份 library.json.bak 也无法读取（%v），原文件另存为 library.json.bak.broken-%s", bakErr, stamp)
				keep(path+".bak", bakRaw)
			}
			s = newStore(path)
			s.notices = append(s.notices, "素材库文件已损坏，且没有可用的备份，已按全新素材库启动；损坏的文件保留在数据文件夹中（文件名以 .broken-"+stamp+" 结尾）")
			break
		}
		s = bak
		when := ""
		if fi, e := os.Stat(path + ".bak"); e == nil {
			when = fi.ModTime().Format("2006-01-02 15:04")
		}
		what := "素材库文件缺失"
		if damaged {
			what = "素材库文件已损坏"
		}
		Logf("%s，已从备份 library.json.bak 恢复（备份时间 %s）", what, when)
		s.notices = append(s.notices, what+"，已从备份恢复（备份时间 "+when+"）；该时间之后的改动未能保留")
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
		s.Settings.Bases = DefaultSettings().Bases
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
	// the rolling backup: a copy of the library.json about to be replaced, and only of one known to be whole
	if s.mainGood && time.Since(s.bakAt) >= s.bakWait {
		if err := copySynced(s.Path, s.Path+".bak"); err != nil {
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
