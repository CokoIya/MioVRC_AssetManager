package library

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

// KonoAsset is another asset manager for VRChat avatars, popular with Japanese players. Its data folder
// (Documents\KonoAsset unless the player moved it; preference.json in %LOCALAPPDATA%\dev.konoasset.app
// names it as dataDirPath) holds:
//
//	metadata/avatars.json, avatarWearables.json, worldObjects.json, otherAssets.json   its library
//	data/<id>/                                                                       the assets, one folder each, named by id
//	images/                                                                          their pictures
//
// Each record: {id, description: {name, creator, imageFilename, tags, memo, boothItemId, dependencies,
// createdAt, publishedAt}, category (free text), supportedAvatars (names)}. In such a folder the scan leaves
// metadata and images out and takes each folder under data for an item (managerChild); what else the player
// keeps there is scanned like anywhere. What KonoAsset knows about each item can be taken over the way
// Avatar Explorer's library is (avatarexplorer.go: it is read into the same shape).

var kaFiles = []struct{ file, group string }{{"avatars.json", "avatar"}, {"avatarWearables.json", "wearable"},
	{"worldObjects.json", "world"}, {"otherAssets.json", "other"}}

// kaHome: dir has the files of a KonoAsset data folder (what is in them is for ReadKA to say). items is the
// folder its assets are in ("" when there is none). The scan asks more before it treats a folder as one
// (managerLayout).
func kaHome(dir string) (ok bool, items string) {
	for _, f := range kaFiles {
		if core.FileExists(filepath.Join(dir, "metadata", f.file)) {
			ok = true
			break
		}
	}
	if !ok {
		return false, ""
	}
	if items = filepath.Join(dir, "data"); !core.IsDir(items) {
		items = ""
	}
	return true, items
}

// the KonoAsset folders the last scan came across, for the settings to offer
var (
	kaSeenMu sync.Mutex
	kaSeen   []string
)

func kaNote(dir string) {
	kaSeenMu.Lock()
	defer kaSeenMu.Unlock()
	for _, d := range kaSeen {
		if core.PathKey(d) == core.PathKey(dir) {
			return
		}
	}
	kaSeen = append(kaSeen, dir)
}

// What the scan takes a folder for.
const (
	homeNone     = iota // an ordinary folder
	homeKA              // a KonoAsset data folder
	homeAE1             // Avatar Explorer V1's program folder
	homeAE1Datas        // … its Datas folder, added by itself
	homeAE2             // Avatar Explorer V2's data folder
)

// managerLayout: which asset manager's folder dir is. A folder is one when the manager's library file is in
// its place AND reads as that manager's library: a list of records with its field names — not when a file of
// that name merely exists (a product may ship a "database/items.json" of its own, and a folder taken for a
// manager's is scanned differently). A list with no record in it says too little by itself: it counts when
// another of the manager's files lies beside it.
//
//	Avatar Explorer V1  Datas/ItemsData.json     a list; records with Title and ItemPath
//	                                             (empty: Datas/CommonAvatar.json is there too)
//	Avatar Explorer V2  database/items.json      {"Items": [...]} or a list; records with Title and ItemPath
//	                                             or ItemPaths (empty: database/commonAvatars.json is there too)
//	KonoAsset           metadata/avatars.json, avatarWearables.json, worldObjects.json, otherAssets.json
//	                                             {"data": [...]} or a list; records with id and description in
//	                                             one of them (all empty: at least two of the four are there)
func managerLayout(dir string) int {
	aeV1 := func(datas string) bool {
		found, empty, f := dbList(filepath.Join(datas, "ItemsData.json"), "")
		if empty {
			return core.FileExists(filepath.Join(datas, "CommonAvatar.json"))
		}
		return found && f["title"] && f["itempath"]
	}
	if aeV1(filepath.Join(dir, "Datas")) {
		return homeAE1
	}
	if strings.EqualFold(filepath.Base(dir), "Datas") && aeV1(dir) {
		return homeAE1Datas
	}
	if found, empty, f := dbList(filepath.Join(dir, "database", "items.json"), "items"); found {
		if empty && core.FileExists(filepath.Join(dir, "database", "commonAvatars.json")) || !empty && f["title"] && (f["itempath"] || f["itempaths"]) {
			return homeAE2
		}
	}
	empties := 0
	for _, kf := range kaFiles {
		found, empty, f := dbList(filepath.Join(dir, "metadata", kf.file), "data")
		if empty {
			empties++
		} else if found && f["id"] && f["description"] {
			return homeKA
		}
	}
	if empties >= 2 {
		return homeKA
	}
	return homeNone
}

// dbList looks into the JSON file at p for a list of records: the file itself, or what its top object holds
// under listKey (any case; "": only the file itself). found: there is such a list; empty: it has no record;
// fields: the names in its first record, in small letters. Only the start of the file is read.
func dbList(p, listKey string) (found, empty bool, fields map[string]bool) {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() || fi.Size() > aeMaxDB {
		return false, false, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return false, false, nil
	}
	defer f.Close()
	br := bufio.NewReader(f)
	if b, _ := br.Peek(3); string(b) == "\xef\xbb\xbf" {
		_, _ = br.Discard(3)
	}
	dec := json.NewDecoder(br)
	first := func() (bool, bool, map[string]bool) { // (after the "[" of the list)
		if !dec.More() {
			_, err := dec.Token()
			return err == nil, err == nil, nil // "]": a list with nothing in it
		}
		var rec map[string]json.RawMessage
		if dec.Decode(&rec) != nil || rec == nil {
			return false, false, nil
		}
		fields := map[string]bool{}
		for k := range rec {
			fields[strings.ToLower(k)] = true
		}
		return true, false, fields
	}
	tok, err := dec.Token()
	if err != nil {
		return false, false, nil
	}
	switch tok {
	case json.Delim('['):
		return first()
	case json.Delim('{'):
		for listKey != "" && dec.More() {
			k, err := dec.Token()
			if err != nil {
				return false, false, nil
			}
			if key, _ := k.(string); strings.EqualFold(key, listKey) {
				if t, err := dec.Token(); err != nil || t != json.Delim('[') {
					return false, false, nil
				}
				return first()
			}
			var skip json.RawMessage
			if dec.Decode(&skip) != nil {
				return false, false, nil
			}
		}
	}
	return false, false, nil
}

// holdsNoAssets: nothing under dir is a file the library would show something of (packages, archives, models,
// texture sources, pictures) — lists, logs and settings at most, and Avatar Explorer's own backups.
func holdsNoAssets(dir string) bool {
	n, none := 0, true
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := core.LowerExt(d.Name())
		asset := naming.LooseAssetExt[ext] || naming.StrongFileExt[ext] || naming.PSDExt[ext] || archiveExt[ext] || modelExt[ext] || ImageExt[ext] ||
			strings.HasSuffix(strings.ToLower(d.Name()), ".tar.gz")
		if n++; n > 5000 || asset && !aeBackup(p, false) {
			none = false
			return filepath.SkipAll
		}
		return nil
	})
	return none
}

// What the scan does with a folder directly inside another manager's folder.
const (
	childScan  = iota // like any other folder
	childSkip         // the manager's own bookkeeping: not looked into
	childItems        // where the manager keeps its assets: each thing in it is looked at by itself
)

// managerChild: what the folder name (at full) is inside a folder that is the manager's home. Only what the
// manager itself keeps there is left out; everything else the player has in the folder is scanned like
// anywhere else — a manager's data folder may be the player's asset folder.
//
//	KonoAsset           metadata, images: left out; data: its assets
//	Avatar Explorer V2  database, images, backups, settings, logs: left out; items: its assets
//	Avatar Explorer V1  Datas: left out but for Datas/Items, its assets; Backup, Output: left out when they
//	                    hold nothing but its backups and lists
func managerChild(home int, name, full string) (what int, items string) {
	l := strings.ToLower(name)
	switch home {
	case homeKA:
		switch l {
		case "metadata", "images":
			return childSkip, ""
		case "data":
			return childItems, full
		}
	case homeAE2:
		switch l {
		case "database", "images", "backups", "settings", "logs":
			return childSkip, ""
		case "items":
			return childItems, full
		}
	case homeAE1:
		switch l {
		case "datas":
			if it := filepath.Join(full, "Items"); core.IsDir(it) {
				return childItems, it
			}
			return childSkip, ""
		case "backup", "output": // (a Backup of nothing but its backups is left out wherever it is: aeBackup)
			if holdsNoAssets(full) {
				return childSkip, ""
			}
		}
	}
	return childScan, ""
}

// managerHome: which asset manager's folder dir is (homeNone: none), asked once per folder and scan. One that
// is found is noted for the settings to offer.
func (c *scanCtx) managerHome(dir string) int {
	k := core.PathKey(dir)
	if h, ok := c.homes[k]; ok {
		return h
	}
	h := managerLayout(dir)
	c.homes[k] = h
	switch h {
	case homeKA:
		kaNote(dir)
	case homeAE1, homeAE1Datas, homeAE2:
		aeNote(dir)
	}
	return h
}

// forgetManagers: a scan starts: the folders it comes across are noted afresh.
func forgetManagers() {
	aeSeenMu.Lock()
	aeSeen = nil
	aeSeenMu.Unlock()
	kaSeenMu.Lock()
	kaSeen = nil
	kaSeenMu.Unlock()
}

var reUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isUUIDName: a folder named by an id (KonoAsset's item folders) says nothing about what is in it.
func isUUIDName(n string) bool { return reUUID.MatchString(strings.TrimSpace(n)) }

// ---------- reading its library ----------

type kaDesc struct {
	Name, Creator string
	ImageFilename *string
	Tags          []string
	Memo          *string
	BoothItemId   flexInt // (null, a number, or a number written as text)
	Dependencies  []string
}

type kaItem struct {
	Id               string
	Description      kaDesc
	Category         string
	SupportedAvatars []string
}

// kaRead reads one of the metadata files: {"version": 3, "data": [...]} (an early version wrote the list alone).
func kaRead(file string) ([]kaItem, error) {
	b, err := aeReadFile(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取 %s（%s）", filepath.Base(file), core.TrimErr(err))
	}
	// (read leniently: one record with a field of another type than expected does not cost the library)
	var box struct{ Data []kaItem }
	if jsonIsList(b) {
		err = jsonLenient(b, &box.Data)
	} else if err = jsonLenient(b, &box); err == nil && box.Data == nil {
		err = errors.New("no data")
	}
	if err != nil {
		return nil, fmt.Errorf("KonoAsset 的数据文件格式无法识别（%s）", filepath.Base(file))
	}
	return box.Data, nil
}

// KonoAsset's categories are free text. The usual words, by group, to the kinds Avatar Explorer uses
// (aeCategory maps those to the library's); the library's own classifier after that.
var kaKinds = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"accessory", regexp.MustCompile(`アクセ|accessor|飾り|飾品|配饰|饰品|ピアス|イヤリング|ネックレス|チョーカー|メガネ|眼鏡|帽子|hat|glasses|尻尾|しっぽ|ケモミミ|耳|tail|wing|翼|羽`)},
	{"hair", regexp.MustCompile(`髪|ヘア|hair|头发|发型`)},
	{"texture", regexp.MustCompile(`テクスチャ|texture|マテリアル|material|材质|skin|スキン|肌|メイク|makeup|妆|ネイル|nail|瞳|eye`)},
	{"shader", regexp.MustCompile(`シェーダ|shader`)},
	{"animation", regexp.MustCompile(`アニメ|animation|モーション|motion|ポーズ|pose|エモート|emote|动作|表情|expression|フェイス|face`)},
	{"gimmick", regexp.MustCompile(`ギミック|gimmick|小物|props?\b|道具|武器|weapon|ペット|pet`)},
	{"tool", regexp.MustCompile(`ツール|tool|システム|system|スクリプト|script|プラグイン|plugin|插件|工具|パーティクル|particle|エフェクト|effect|音|sound|voice|ボイス|フォント|font`)},
	{"avatar", regexp.MustCompile(`アバター|avatar|素体|モデル|model`)},
	{"clothing", regexp.MustCompile(`衣装|衣服|服|cloth|outfit|dress|wear|costume|コスチューム|靴|シューズ|shoes|boots|ブーツ|下着|水着|swim|ソックス|socks|タイツ|手袋|glove`)},
}

var kaCatKind = map[string]string{"素体": "avatar", "衣服": "clothing", "头发": "hair", "配饰": "accessory", "道具": "gimmick",
	"材质": "texture", "插件": "tool", "动作": "animation"}

// kaKind: the kind an item is, from its group and its category text. known: the text was understood (when it
// was not, it is kept as a tag so nothing is lost).
func kaKind(group, category string) (kind string, known bool) {
	if group == "avatar" {
		return "avatar", true
	}
	c := strings.ToLower(strings.TrimSpace(category))
	if c != "" {
		for _, k := range kaKinds {
			if k.re.MatchString(c) {
				return k.kind, true
			}
		}
		if k := kaCatKind[naming.Classify(c)]; k != "" {
			return k, true
		}
	}
	if group == "wearable" {
		return "clothing", c == ""
	}
	return "", c == ""
}

// ReadKA reads the library of the KonoAsset data folder at dir. lang: the window's language, for the one line
// written into a memo.
func ReadKA(dir, lang string) (*AELibrary, error) {
	dir = filepath.Clean(dir)
	if ok, _ := kaHome(dir); !ok {
		return nil, errors.New("该文件夹不是 KonoAsset 的数据文件夹（未找到 metadata\\avatars.json 等文件）")
	}
	lib := &AELibrary{Dir: dir, Source: "konoasset", ItemDir: filepath.Join(dir, "data"), AddDir: dir}
	nameOf := map[string]string{} // id → name, for the dependencies
	type dep struct {
		at  int
		ids []string
	}
	var deps []dep
	for _, f := range kaFiles {
		items, err := kaRead(filepath.Join(dir, "metadata", f.file))
		if err != nil {
			return nil, err
		}
		for _, r := range items {
			if strings.TrimSpace(r.Id) == "" {
				continue
			}
			d := r.Description
			it := AEItem{Title: strings.TrimSpace(d.Name), Author: strings.TrimSpace(d.Creator), Tags: append([]string{}, d.Tags...),
				Paths: []string{filepath.Join(lib.ItemDir, r.Id)}, Supported: r.SupportedAvatars}
			if d.Memo != nil {
				it.Memo = strings.TrimSpace(*d.Memo)
			}
			if d.BoothItemId > 0 {
				it.BoothID = int(d.BoothItemId)
			}
			if d.ImageFilename != nil {
				if n := filepath.Base(strings.ReplaceAll(*d.ImageFilename, "\\", "/")); n != "." && n != "" {
					if p := filepath.Join(dir, "images", n); ImageExt[core.LowerExt(p)] && core.FileExists(p) {
						it.Thumb = p
					}
				}
			}
			kind, known := kaKind(f.group, r.Category)
			it.Kind = kind
			if !known {
				it.Tags = append(it.Tags, strings.TrimSpace(r.Category))
			}
			nameOf[r.Id] = it.Title
			if len(d.Dependencies) > 0 {
				deps = append(deps, dep{len(lib.Items), d.Dependencies})
			}
			lib.Items = append(lib.Items, it)
		}
	}
	// what an item depends on, by name, into its memo
	for _, dp := range deps {
		var names []string
		for _, id := range dp.ids {
			if n := nameOf[id]; n != "" && !core.ContainsStr(names, n) {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			it := &lib.Items[dp.at]
			if it.Memo != "" {
				it.Memo += "\n"
			}
			it.Memo += kaDepLabel(lang) + strings.Join(names, "、")
		}
	}
	return lib, nil
}

// kaDepLabel: the line in the memo that lists the dependencies, in the window's language (a memo is the
// player's own text: it is not translated on the page).
func kaDepLabel(lang string) string {
	switch lang {
	case "en":
		return "Depends on: "
	case "ja":
		return "依存アセット："
	}
	return "依赖素材："
}

// PreviewKA says what taking over the KonoAsset library at dir would do.
func PreviewKA(st *core.Store, dir string) (*AEPreview, error) {
	lib, err := readKA(st, dir)
	if err != nil {
		return nil, err
	}
	return previewLib(st, lib), nil
}

// ApplyKA takes over what KonoAsset knows about the assets that are in the library, like ApplyAE.
func ApplyKA(st *core.Store, dir string, addRoot bool) (*AEResult, error) {
	lib, err := readKA(st, dir)
	if err != nil {
		return nil, err
	}
	return applyLib(st, lib, addRoot), nil
}

func readKA(st *core.Store, dir string) (*AELibrary, error) {
	st.Mu.RLock()
	lang := st.Settings.Lang
	st.Mu.RUnlock()
	return ReadKA(dir, lang)
}

// kaPreferredDir: the data folder named in a KonoAsset preference.json ({"version": 6, "data": {"dataDirPath":
// …}}; the first versions wrote dataDirPath at the top), "" when the file does not say.
func kaPreferredDir(file string) string {
	b, err := aeReadFile(file)
	if err != nil {
		return ""
	}
	var pref struct {
		Data        json.RawMessage
		DataDirPath string
	}
	if json.Unmarshal(b, &pref) != nil {
		return ""
	}
	if pref.DataDirPath == "" && len(pref.Data) > 0 {
		var inner struct{ DataDirPath string }
		_ = json.Unmarshal(pref.Data, &inner)
		pref.DataDirPath = inner.DataDirPath
	}
	return strings.TrimSpace(pref.DataDirPath)
}

// KAFolders: the KonoAsset folders worth offering — those the last scan came across, the one its preferences
// name, and its usual places on this computer.
func KAFolders() []string {
	kaSeenMu.Lock()
	out := append([]string{}, kaSeen...)
	kaSeenMu.Unlock()
	var cands []string
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		for _, app := range []string{"dev.konoasset.app", "KonoAsset"} {
			if d := kaPreferredDir(filepath.Join(la, app, "preference.json")); d != "" {
				cands = append(cands, d)
			}
			cands = append(cands, filepath.Join(la, app, "KonoAsset"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(home, "Documents", "KonoAsset"))
	}
	if up := os.Getenv("USERPROFILE"); up != "" {
		cands = append(cands, filepath.Join(up, "Documents", "KonoAsset"))
	}
	for _, d := range cands {
		if ok, _ := kaHome(d); ok {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return core.UniqStrings(out)
}
