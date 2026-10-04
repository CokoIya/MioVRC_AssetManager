package library

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

// Avatar Explorer is another asset manager for VRChat avatars. Players who used it keep its folder inside
// their asset folders, and it holds more than assets:
//
//	V1 (in the program's own folder):
//	  Datas/ItemsData.json, Datas/CommonAvatar.json   its library
//	  Datas/Items/<item>/                              the assets it unpacked
//	  Datas/Thumbnail, Datas/AuthorImage, Datas/Temp   pictures and scratch space
//	  Backup/<time>.zip                                the whole of Datas, zipped — not assets
//	  Output/                                          exported lists
//	V2 (%APPDATA%/Avatar Explorer V2, or where the player moved it):
//	  database/items.json, database/commonAvatars.json
//	  items/<item>/                                    (settings/runtimeSettings.json may name another folder)
//	  images/item_thumbnails/, backups/<time>/, settings/, logs/
//
// In such a folder the scan leaves out what Avatar Explorer keeps for itself and takes each folder in its item
// folder for an asset (managerLayout, managerChild in konoasset.go); what else the player keeps there is
// scanned like anywhere. What Avatar Explorer knows about each item (title, Booth id, kind, the avatars it
// fits, tags, memo, picture) can be taken over.

// aeHome: dir has the library file of an Avatar Explorer folder (V1's program folder or its Datas, or V2's
// data folder; what is in the file is for ReadAE to say). items is the folder its assets are in ("" when
// there is none).
func aeHome(dir string) (ok bool, items string) {
	switch {
	case core.FileExists(filepath.Join(dir, "Datas", "ItemsData.json")):
		items = filepath.Join(dir, "Datas", "Items")
	case core.FileExists(filepath.Join(dir, "ItemsData.json")) && strings.EqualFold(filepath.Base(dir), "Datas"):
		items = filepath.Join(dir, "Items")
	case core.FileExists(filepath.Join(dir, "database", "items.json")):
		items = filepath.Join(dir, "items")
	default:
		return false, ""
	}
	if !core.IsDir(items) {
		items = ""
	}
	return true, items
}

var reAEStamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[- ]\d{2}-\d{2}-\d{2}(_\d+)?$`)

var aeDBFiles = map[string]bool{"itemsdata.json": true, "commonavatar.json": true, "customcategory.txt": true, "items.json": true,
	"commonavatars.json": true, "bulkimportpresets.json": true, "tempavatars.json": true, "variationhashes.json": true}

// aeBackup: a backup Avatar Explorer made, wherever it was put — a folder named after a time holding only
// its database files (V2), or a zip named after a time with ItemsData.json in it (V1).
func aeBackup(full string, isDir bool) bool {
	name := filepath.Base(full)
	if isDir {
		if !reAEStamp.MatchString(name) {
			// the folder the backups are kept in, when there is nothing else in it
			if l := strings.ToLower(name); !strings.Contains(l, "backup") && !strings.Contains(name, "备份") && !strings.Contains(name, "バックアップ") {
				return false
			}
			ents, err := os.ReadDir(full)
			if err != nil || len(ents) == 0 {
				return false
			}
			for _, e := range ents {
				if !reAEStamp.MatchString(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))) || !aeBackup(filepath.Join(full, e.Name()), e.IsDir()) {
					return false
				}
			}
			return true
		}
		ents, err := os.ReadDir(full)
		if err != nil || len(ents) == 0 {
			return false
		}
		for _, e := range ents {
			if e.IsDir() || !aeDBFiles[strings.ToLower(e.Name())] {
				return false
			}
		}
		return true
	}
	if core.LowerExt(name) != ".zip" || !reAEStamp.MatchString(strings.TrimSuffix(name, filepath.Ext(name))) {
		return false
	}
	zr, err := zip.OpenReader(full)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if n := strings.ReplaceAll(f.Name, "\\", "/"); n == "ItemsData.json" || strings.HasSuffix(n, "/ItemsData.json") {
			return true
		}
	}
	return false
}

// the Avatar Explorer folders the last scan came across, for the settings to offer
var (
	aeSeenMu sync.Mutex
	aeSeen   []string
)

func aeNote(dir string) {
	aeSeenMu.Lock()
	defer aeSeenMu.Unlock()
	for _, d := range aeSeen {
		if core.PathKey(d) == core.PathKey(dir) {
			return
		}
	}
	aeSeen = append(aeSeen, dir)
}

// ---------- reading its library ----------

type AEItem struct {
	Title     string
	Author    string
	Memo      string
	BoothID   int
	Paths     []string // the item's folders, absolute
	Thumb     string   // picture file, "" when it has none
	Kind      string   // avatar, clothing, texture, gimmick, accessory, hair, animation, tool, shader, custom, ""
	Custom    string   // the name of its custom category
	Supported []string // titles of the avatars it is for
	Tags      []string
	Hidden    bool
}

// AELibrary: what another asset manager knows — Avatar Explorer's, or KonoAsset's read into the same shape
// (konoasset.go), so that taking it over is one piece of code.
type AELibrary struct {
	Dir     string // the folder it was read from
	Version int    // 1 or 2 (0: not Avatar Explorer)
	Source  string // "" = Avatar Explorer, "konoasset"
	ItemDir string // where its assets are
	AddDir  string // the folder to add to the library for the scan to reach them (it knows what to take out of it)
	Items   []AEItem
}

var aeKindsV1 = []string{"avatar", "clothing", "texture", "gimmick", "accessory", "hair", "animation", "tool", "shader", "custom"}
var aeKindsV2 = append([]string{""}, aeKindsV1...)

// aeKind reads an item type written as a number or as its name.
func aeKind(raw json.RawMessage, kinds []string) string {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		if n >= 0 && n < len(kinds) {
			return kinds[n]
		}
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch s = strings.ToLower(s); s {
		case "hairstyle":
			return "hair"
		case "avatar", "clothing", "texture", "gimmick", "accessory", "animation", "tool", "shader", "custom":
			return s
		}
	}
	return ""
}

const aeMaxDB = 64 << 20

func aeReadFile(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if fi.Size() > aeMaxDB {
		return nil, errors.New("文件过大")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")), nil
}

// ReadAE reads the library of the Avatar Explorer folder at dir.
func ReadAE(dir string) (*AELibrary, error) {
	dir = filepath.Clean(dir)
	if strings.EqualFold(filepath.Base(dir), "Datas") && core.FileExists(filepath.Join(dir, "ItemsData.json")) {
		dir = filepath.Dir(dir)
	}
	switch {
	case core.FileExists(filepath.Join(dir, "Datas", "ItemsData.json")):
		return readAEv1(dir)
	case core.FileExists(filepath.Join(dir, "database", "items.json")):
		return readAEv2(dir)
	}
	return nil, errors.New("该文件夹不是 Avatar Explorer 的数据文件夹（未找到 Datas\\ItemsData.json 或 database\\items.json）")
}

// aePath: a path as Avatar Explorer wrote it (either kind of separator, maybe relative to its folder), "" for
// one that is not taken. A path on another computer (\\host\share, \\?\UNC\…) is not taken unless it lies
// in base, where the library itself was read from: only asking Windows whether such a path exists signs
// this user in to that host, and a library file can come with anything unpacked into the asset folders.
func aePath(p, base string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "//") {
		lb := strings.ToLower(strings.TrimRight(strings.ReplaceAll(base, "\\", "/"), "/"))
		if lp := strings.ToLower(p); !strings.HasPrefix(lb, "//") || !(lp == lb || strings.HasPrefix(lp, lb+"/")) {
			return ""
		}
		return filepath.Clean(filepath.FromSlash(p))
	}
	if win := len(p) > 2 && p[1] == ':'; !win && !strings.HasPrefix(p, "/") {
		p = filepath.ToSlash(base) + "/" + strings.TrimPrefix(p, "./")
	}
	return filepath.Clean(filepath.FromSlash(p))
}

// flexInt: a number as either manager may have written it — a JSON number, or a string holding one. Anything
// else is 0 and never an error.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(strings.Trim(strings.TrimSpace(string(b)), `"`))
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexInt(n)
	} else if x, err := strconv.ParseFloat(s, 64); err == nil && x > -1e15 && x < 1e15 {
		*f = flexInt(x)
	} else {
		*f = 0
	}
	return nil
}

// jsonLenient: json.Unmarshal for a library another program wrote. A value of a type that was not expected
// (a number written as text, a null list, a record that is not a record) costs that one field: the decoder
// fills in everything else and reports the first such value, which is not passed on here — one odd record
// must not make the whole library unreadable. What is not JSON at all is still an error.
func jsonLenient(b []byte, v any) error {
	err := json.Unmarshal(b, v)
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return nil
	}
	return err
}

// jsonIsList: the file is a list at its top (not an object).
func jsonIsList(b []byte) bool {
	return strings.HasPrefix(strings.TrimSpace(string(b[:min(len(b), 64)])), "[")
}

func readAEv1(dir string) (*AELibrary, error) {
	b, err := aeReadFile(filepath.Join(dir, "Datas", "ItemsData.json"))
	if err != nil {
		return nil, fmt.Errorf("无法读取 ItemsData.json（%s）", core.TrimErr(err))
	}
	var raw []struct {
		Title, AuthorName, ItemMemo, ItemPath, ImagePath, CustomCategory string
		BoothId                                                          flexInt
		Type                                                             json.RawMessage
		SupportedAvatar, Tags                                            []string
	}
	if !jsonIsList(b) || jsonLenient(b, &raw) != nil {
		return nil, errors.New("ItemsData.json 的格式无法识别")
	}
	// (the program folder is the one to add: the scan knows to take only Datas/Items out of it)
	lib := &AELibrary{Dir: dir, Version: 1, ItemDir: filepath.Join(dir, "Datas", "Items"), AddDir: dir}
	titleOf := map[string]string{} // an avatar's folder → its title
	for _, r := range raw {
		it := AEItem{Title: strings.TrimSpace(r.Title), Author: strings.TrimSpace(r.AuthorName), Memo: strings.TrimSpace(r.ItemMemo),
			BoothID: int(r.BoothId), Kind: aeKind(r.Type, aeKindsV1), Custom: strings.TrimSpace(r.CustomCategory), Tags: r.Tags}
		if p := aePath(r.ItemPath, dir); p != "" {
			it.Paths = []string{p}
			titleOf[core.PathKey(p)] = it.Title
		}
		if it.Title == "" && len(it.Paths) == 0 {
			continue // not a record
		}
		// (the picture may be any file the player once chose: only a picture is taken, and only from this computer)
		if p := aePath(r.ImagePath, dir); p != "" && ImageExt[core.LowerExt(p)] && core.FileExists(p) {
			it.Thumb = p
		}
		it.Supported = r.SupportedAvatar // folders for now
		lib.Items = append(lib.Items, it)
	}
	for i := range lib.Items {
		var names []string
		for _, p := range lib.Items[i].Supported {
			if t := titleOf[core.PathKey(aePath(p, dir))]; t != "" {
				names = append(names, t)
			}
		}
		lib.Items[i].Supported = names
	}
	return lib, nil
}

func readAEv2(dir string) (*AELibrary, error) {
	b, err := aeReadFile(filepath.Join(dir, "database", "items.json"))
	if err != nil {
		return nil, fmt.Errorf("无法读取 items.json（%s）", core.TrimErr(err))
	}
	type item struct {
		Id, Title, Author, ItemMemo, ItemPath, ThumbnailFileName string
		BoothId                                                  flexInt
		ItemPaths, SupportedAvatars, Tags                        []string
		IsHidden                                                 bool
		Category                                                 struct {
			Type           json.RawMessage
			CustomCategory string
		}
	}
	var box struct{ Items []item }
	if jsonIsList(b) { // an early V2 wrote the list alone
		err = jsonLenient(b, &box.Items)
	} else {
		err = jsonLenient(b, &box)
	}
	if err != nil {
		return nil, errors.New("items.json 的格式无法识别")
	}
	itemDir := filepath.Join(dir, "items")
	if sb, err := aeReadFile(filepath.Join(dir, "settings", "runtimeSettings.json")); err == nil {
		var s struct{ DataRootDirectory string }
		if json.Unmarshal(sb, &s) == nil && strings.TrimSpace(s.DataRootDirectory) != "" {
			if d := aePath(s.DataRootDirectory, dir); d != "" {
				itemDir = d
			}
		}
	}
	lib := &AELibrary{Dir: dir, Version: 2, ItemDir: itemDir, AddDir: itemDir}
	titleOf := map[string]string{}
	const rootMark = "<root>"
	full := func(p string) string {
		if strings.HasPrefix(p, rootMark) {
			return filepath.Join(itemDir, filepath.FromSlash(strings.TrimLeft(strings.ReplaceAll(p[len(rootMark):], "\\", "/"), "/")))
		}
		return aePath(p, itemDir)
	}
	for _, r := range box.Items {
		it := AEItem{Title: strings.TrimSpace(r.Title), Author: strings.TrimSpace(r.Author), Memo: strings.TrimSpace(r.ItemMemo), BoothID: int(r.BoothId),
			Kind: aeKind(r.Category.Type, aeKindsV2), Custom: strings.TrimSpace(r.Category.CustomCategory), Tags: r.Tags, Hidden: r.IsHidden,
			Supported: r.SupportedAvatars}
		for _, p := range append([]string{r.ItemPath}, r.ItemPaths...) {
			if p = full(p); p != "" && !core.ContainsStr(it.Paths, p) {
				it.Paths = append(it.Paths, p)
			}
		}
		if n := filepath.Base(strings.ReplaceAll(r.ThumbnailFileName, "\\", "/")); r.ThumbnailFileName != "" && n != "." {
			if p := filepath.Join(dir, "images", "item_thumbnails", n); ImageExt[core.LowerExt(p)] && core.FileExists(p) {
				it.Thumb = p
			}
		}
		if it.Title == "" && len(it.Paths) == 0 {
			continue // not a record
		}
		titleOf["item:"+r.Id] = it.Title
		lib.Items = append(lib.Items, it)
	}
	for i := range lib.Items {
		var names []string
		for _, id := range lib.Items[i].Supported {
			if t := titleOf[id]; t != "" {
				names = append(names, t)
			}
		}
		lib.Items[i].Supported = names
	}
	return lib, nil
}

// ---------- taking its information over ----------

// aeCategory: the library's category for one of Avatar Explorer's kinds ("" = leave it as the scan found it).
func aeCategory(kind string) string {
	switch kind {
	case "avatar":
		return "素体"
	case "clothing":
		return "衣服"
	case "texture":
		return "材质"
	case "gimmick":
		return "道具"
	case "accessory":
		return "配饰"
	case "hair":
		return "头发"
	case "animation":
		return "动作"
	case "tool", "shader":
		return "插件"
	}
	return ""
}

var reAEBracket = regexp.MustCompile(`[【\[（(][^】\]）)]*[】\]）)]`)

// aeBases: the library's base-body names for the avatars an item is made for. An avatar the settings do not
// list goes by its own (shortened) title.
func aeBases(titles []string, defs []naming.BaseDef) []string {
	var out []string
	for _, t := range titles {
		got := naming.DetectBases(t, defs)
		if len(got) == 0 {
			s := strings.TrimSpace(reAEBracket.ReplaceAllString(t, " "))
			if f := strings.Fields(s); len(f) > 0 {
				s = f[0]
			}
			if r := []rune(s); len(r) > 24 {
				s = string(r[:24])
			}
			if s != "" {
				got = []string{s}
			}
		}
		for _, g := range got {
			if !core.ContainsStr(out, g) {
				out = append(out, g)
			}
		}
	}
	return out
}

type AEPreview struct {
	Dir      string `json:"dir"`
	Version  int    `json:"version"`
	Items    int    `json:"items"`    // in its library
	Matched  int    `json:"matched"`  // of them, in this library
	Outside  int    `json:"outside"`  // their folder is in none of the asset folders
	Missing  int    `json:"missing"`  // their folder is not there any more
	ItemDir  string `json:"itemDir"`  // its own asset folder …
	AddRoot  string `json:"addRoot"`  // … and the folder to add so that the scan reaches it ("" = reached already)
	InFolder int    `json:"inFolder"` // items the scan would reach once AddRoot is added
}

// aeIndex: every asset of the library by the folders and files it is made of.
func aeIndex(st *core.Store) map[string]string {
	idx := map[string]string{}
	for _, a := range st.Assets {
		for _, l := range a.Locations {
			idx[core.PathKey(l.Path)] = a.Key
		}
	}
	return idx
}

// aeAbove: the asset the folder p lies inside ("" when it lies in none).
func aeAbove(idx map[string]string, p string) string {
	for k := core.PathKey(p); ; {
		up := filepath.Dir(k)
		if up == k || len(up) < 4 {
			return ""
		}
		if key := idx[up]; key != "" {
			return key
		}
		k = up
	}
}

// libMatch: what one item of another manager's library is in this one.
type libMatch struct {
	there bool     // one of its folders exists
	in    bool     // it is in the library: an asset is at its folder, inside it or around it
	keys  []string // the assets its information goes to (none when another item stands for its asset already)
}

// matchLib finds, for every item of another manager's library, the assets of this one it stands for — the
// same for the preview and for taking it over, so that the two count alike. Only folders that exist are
// looked at. In this order, an asset going to the first item that reaches it:
//  1. the asset that is at the item's folder;
//  2. the assets that lie inside it, when none is at it (an item folder the scan took apart: "<id>/Dress_A",
//     "<id>/Dress_B") — the item's information goes to each;
//  3. the asset the folder lies inside, when no item is at that asset itself (a dress kept in its avatar's
//     folder does not give the avatar its Booth page while the avatar has an item of its own).
//
// roots: the library's asset folders. Caller must not hold st.Mu (folders are asked for here).
func matchLib(st *core.Store, lib *AELibrary) (ms []libMatch, roots []string) {
	st.Mu.RLock()
	idx := aeIndex(st)
	roots = append(roots, st.Settings.Roots...)
	st.Mu.RUnlock()
	ms = make([]libMatch, len(lib.Items))
	paths := make([][]string, len(lib.Items)) // the folders that exist
	taken := map[string]bool{}
	for i, it := range lib.Items {
		for _, p := range it.Paths {
			if core.StatOK(p) {
				paths[i] = append(paths[i], p)
			}
		}
		ms[i].there = len(paths[i]) > 0
		for _, p := range paths[i] {
			if key := idx[core.PathKey(p)]; key != "" {
				ms[i].in = true
				if !taken[key] {
					ms[i].keys, taken[key] = []string{key}, true
					break
				}
			}
		}
	}
	// the assets inside an item's folder
	owner := map[string]int{}
	for i := range lib.Items {
		if !ms[i].in {
			for _, p := range paths[i] {
				if _, dup := owner[core.PathKey(p)]; !dup {
					owner[core.PathKey(p)] = i
				}
			}
		}
	}
	if len(owner) > 0 {
		locs := make([]string, 0, len(idx))
		for l := range idx {
			locs = append(locs, l)
		}
		sort.Strings(locs) // (the same order every time)
		for _, l := range locs {
			for k := l; ; {
				up := filepath.Dir(k)
				if up == k || len(up) < 4 {
					break
				}
				if i, ok := owner[up]; ok {
					ms[i].in = true
					if key := idx[l]; !taken[key] {
						ms[i].keys, taken[key] = append(ms[i].keys, key), true
					}
					break
				}
				k = up
			}
		}
	}
	for i := range lib.Items {
		if ms[i].in {
			continue
		}
		for _, p := range paths[i] {
			if key := aeAbove(idx, p); key != "" {
				ms[i].in = true
				if !taken[key] {
					ms[i].keys, taken[key] = []string{key}, true
					break
				}
			}
		}
	}
	return ms, roots
}

// PreviewAE says what taking over the Avatar Explorer library at dir would do.
func PreviewAE(st *core.Store, dir string) (*AEPreview, error) {
	lib, err := ReadAE(dir)
	if err != nil {
		return nil, err
	}
	return previewLib(st, lib), nil
}

// previewLib: what taking over another manager's library would do (PreviewAE, PreviewKA).
func previewLib(st *core.Store, lib *AELibrary) *AEPreview {
	ms, roots := matchLib(st, lib)
	pv := &AEPreview{Dir: lib.Dir, Version: lib.Version, Items: len(lib.Items), ItemDir: lib.ItemDir}
	under := func(p string) bool {
		for _, r := range roots {
			if core.UnderDir(p, r) || core.PathKey(p) == core.PathKey(r) {
				return true
			}
		}
		return false
	}
	for i, it := range lib.Items {
		inside, own := false, false
		if ms[i].there && !ms[i].in {
			for _, p := range it.Paths {
				if core.StatOK(p) {
					inside = inside || under(p)
					own = own || core.UnderDir(p, lib.ItemDir)
				}
			}
		}
		switch {
		case ms[i].in:
			pv.Matched++
		case !ms[i].there:
			pv.Missing++
		case !inside:
			pv.Outside++
			if own {
				pv.InFolder++
			}
		}
	}
	if pv.InFolder > 0 && core.IsDir(lib.ItemDir) && !under(lib.ItemDir) {
		pv.AddRoot = lib.AddDir
	}
	return pv
}

type AEResult struct {
	Matched  int    `json:"matched"`  // assets that got something
	Booth    int    `json:"booth"`    // linked to their Booth page
	Named    int    `json:"named"`    // names
	Category int    `json:"category"` // categories
	Bases    int    `json:"bases"`    // base bodies
	Tags     int    `json:"tags"`
	Notes    int    `json:"notes"`
	Covers   int    `json:"covers"`
	Skipped  int    `json:"skipped"` // items with no asset in the library (yet)
	AddRoot  string `json:"addRoot"` // the folder that was added
}

// ApplyAE takes over what Avatar Explorer knows about the assets that are in the library. Only what the
// player has not set here is filled in; nothing of Avatar Explorer's is changed. addRoot: its asset folder
// becomes one of the library's when the scan does not reach it yet (the items in it are taken over at the
// next call, once they have been scanned).
func ApplyAE(st *core.Store, dir string, addRoot bool) (*AEResult, error) {
	lib, err := ReadAE(dir)
	if err != nil {
		return nil, err
	}
	return applyLib(st, lib, addRoot), nil
}

// applyLib takes over another manager's library (ApplyAE, ApplyKA). Caller must not hold st.Mu.
func applyLib(st *core.Store, lib *AELibrary, addRoot bool) *AEResult {
	res := &AEResult{}
	if addRoot {
		if pv := previewLib(st, lib); pv.AddRoot != "" {
			st.Mu.Lock()
			st.Settings.Roots = append(st.Settings.Roots, pv.AddRoot)
			st.Mu.Unlock()
			res.AddRoot = pv.AddRoot
		}
	}
	coverDir := filepath.Join(core.DataDir, "covers", "avatarexplorer")
	if lib.Source != "" {
		coverDir = filepath.Join(core.DataDir, "covers", lib.Source)
	}
	ms, _ := matchLib(st, lib) // (the same items the preview counted as in the library)
	st.Mu.Lock()
	byKey := map[string]*core.Asset{}
	for _, a := range st.Assets {
		byKey[a.Key] = a
	}
	defs := naming.ParseBases(st.Settings.Bases)
	type target struct {
		it    *AEItem
		key   string
		parts bool // one of several assets the item's folder was taken apart into
	}
	var targets []target
	for i := range lib.Items {
		if !ms[i].in {
			res.Skipped++
		}
		for _, key := range ms[i].keys {
			targets = append(targets, target{&lib.Items[i], key, len(ms[i].keys) > 1})
		}
	}
	for _, tg := range targets {
		it, key := tg.it, tg.key
		a := byKey[key]
		if a == nil {
			continue
		}
		u := st.User[key]
		if u == nil {
			u = &core.UserData{}
		}
		got := false
		if it.BoothID > 0 && a.BoothID == "" && u.BoothURL == "" && !u.NoBooth {
			u.BoothURL = "https://booth.pm/ja/items/" + strconv.Itoa(it.BoothID)
			res.Booth++
			got = true
		} else if it.BoothID <= 0 && a.BoothID == "" && u.BoothURL == "" && u.Name == "" && it.Title != "" && it.Title != a.Name && !tg.parts {
			u.Name = it.Title // with a Booth page the name comes from there
			res.Named++
			got = true
		}
		if u.Name == "" && it.Title != "" && isUUIDName(a.RawName) && !tg.parts {
			u.Name = it.Title // KonoAsset names its item folders by id: the name is always worth taking
			res.Named++
			got = true
		}
		if c := aeCategory(it.Kind); c != "" && u.Category == "" && c != a.Category {
			u.Category = c
			res.Category++
			got = true
		}
		if u.Bases == nil {
			if bs := aeBases(it.Supported, defs); len(bs) > 0 {
				all := append([]string{}, a.Bases...)
				n := len(all)
				for _, b := range bs {
					if !core.ContainsStr(all, b) {
						all = append(all, b)
					}
				}
				if len(all) > n {
					u.Bases = all
					res.Bases++
					got = true
				}
			}
		}
		tags := append([]string{}, it.Tags...)
		if it.Kind == "custom" && it.Custom != "" {
			tags = append(tags, it.Custom)
		}
		added := false
		for _, t := range core.CleanList(tags) {
			if !core.ContainsStr(u.Tags, t) {
				u.Tags = append(u.Tags, t)
				added = true
			}
		}
		if added {
			res.Tags++
			got = true
		}
		if u.Notes == "" && it.Memo != "" {
			u.Notes = it.Memo
			res.Notes++
			got = true
		}
		if u.Cover == "" && it.Thumb != "" && len(a.Covers) == 0 && a.BoothID == "" && u.BoothURL == "" {
			// a copy of its own: the picture stays when Avatar Explorer is removed
			if dst := aeCopyThumb(it.Thumb, coverDir); dst != "" {
				u.Cover = dst
				res.Covers++
				got = true
			}
		}
		if got {
			st.User[key] = u
			res.Matched++
		}
	}
	st.Mu.Unlock()
	if res.Matched > 0 || res.AddRoot != "" {
		_ = st.Save()
		core.BumpRev()
	}
	return res
}

// aeCopyThumb copies an item's picture into coverDir and returns the copy, "" when it is not taken: only a
// picture by its extension (the path comes out of another program's file, and what is in covers/ travels with
// a library export), and only one of a size worth reading — asked of the file system, not by reading it.
func aeCopyThumb(src, coverDir string) string {
	ext := core.LowerExt(src)
	if !ImageExt[ext] {
		return ""
	}
	if fi, err := os.Stat(src); err != nil || fi.IsDir() || fi.Size() == 0 || fi.Size() >= aeMaxThumb {
		return ""
	}
	b, err := os.ReadFile(src)
	if err != nil || len(b) >= aeMaxThumb {
		return ""
	}
	sum := sha1.Sum([]byte(core.PathKey(src)))
	dst := filepath.Join(coverDir, hex.EncodeToString(sum[:8])+ext)
	if os.MkdirAll(coverDir, 0755) != nil || os.WriteFile(dst, b, 0644) != nil {
		return ""
	}
	return dst
}

const aeMaxThumb = 20 << 20

// AEFolders: the Avatar Explorer folders worth offering — those the last scan came across, and V2's usual
// place on this computer.
func AEFolders() []string {
	aeSeenMu.Lock()
	out := append([]string{}, aeSeen...)
	aeSeenMu.Unlock()
	if ad := os.Getenv("APPDATA"); ad != "" {
		for _, d := range []string{filepath.Join(ad, "Avatar Explorer V2")} {
			if ok, _ := aeHome(d); ok {
				out = append(out, d)
			}
		}
	}
	sort.Strings(out)
	return core.UniqStrings(out)
}
