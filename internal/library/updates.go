package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/netdisk"
	"vrclib/internal/update"
)

// UpdateNote: something newer than what an asset has on disk, and how to get it. It is made only of what
// the sources say: the download list of a purchase (a later version in a file name, a file that was added
// or put under another id), and the file list of a netdisk share.
type UpdateNote struct {
	Key      string    `json:"key"`
	Source   string    `json:"source"`             // "booth": a purchase's downloads (Gumroad's too); "pan": a netdisk share
	Version  string    `json:"version,omitempty"`  // booth: a later version, read from a download's file name
	Have     string    `json:"have,omitempty"`     // … and the one on disk
	Added    []string  `json:"added,omitempty"`    // files that are new
	Replaced []string  `json:"replaced,omitempty"` // files put under a name that was there before
	Changed  []string  `json:"changed,omitempty"`  // pan: new or replaced — the share's change notice does not tell which
	Removed  []string  `json:"removed,omitempty"`  // pan: files the seller took away
	At       int64     `json:"at,omitempty"`       // when it was noticed
	Item     string    `json:"item,omitempty"`     // booth: the purchase
	DLs      []string  `json:"dls,omitempty"`      // booth: its downloads to fetch
	Gumroad  bool      `json:"gumroad,omitempty"`
	PanKey   string    `json:"panKey,omitempty"` // pan: the card that is downloaded
	Paths    []string  `json:"paths,omitempty"`  // pan: the parts of its file list to fetch (none: all of it)
	CanDL    bool      `json:"canDl"`            // the program can fetch it itself
	Old      []OldCopy `json:"old,omitempty"`    // booth: the earlier version's folder or file on disk
}

// OldCopy: what an update leaves behind — kept, unless the player says it may go to the Recycle Bin.
type OldCopy struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Dir  bool   `json:"dir,omitempty"`
}

// updates.json: what the player had when an asset was last up to date, to tell what came later.
type updState struct {
	Base  map[string][]dlSeen `json:"base"`  // purchase → its downloads then
	Ver   map[string]string   `json:"ver"`   // asset → the later version that has been dealt with
	Pan   map[string]*panSeen `json:"pan"`   // netdisk card (or "link:<asset>|<share>") → its files then
	Since map[string]int64    `json:"since"` // asset → when its update was first noticed
}

type dlSeen struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type panSeen struct {
	At    int64            `json:"at"`
	Files map[string]int64 `json:"files"`
}

var upd struct {
	mu    sync.Mutex
	file  string
	s     *updState
	dirty bool
}

const panSnapMax = 5000 // files of one card remembered; a bigger share is compared by its change notice only

func init() {
	// updates.json was replaced under the running program (an import, or its undo): read it again
	OnDataImported(func() {
		upd.mu.Lock()
		upd.s = nil
		upd.mu.Unlock()
	})
}

func updFile() string { return filepath.Join(core.DataDir, "updates.json") }

// updLoadLocked: the state of this data folder (tests move the folder). Caller holds upd.mu.
func updLoadLocked() *updState {
	if upd.s != nil && upd.file == updFile() {
		return upd.s
	}
	s := &updState{}
	if b, err := os.ReadFile(updFile()); err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.Base == nil {
		s.Base = map[string][]dlSeen{}
	}
	if s.Ver == nil {
		s.Ver = map[string]string{}
	}
	if s.Pan == nil {
		s.Pan = map[string]*panSeen{}
	}
	if s.Since == nil {
		s.Since = map[string]int64{}
	}
	upd.s, upd.file, upd.dirty = s, updFile(), false
	return s
}

// updSaveLocked writes the state when it changed: a temporary file, then renamed over the old one.
func updSaveLocked() {
	if !upd.dirty || upd.s == nil {
		return
	}
	b, err := json.Marshal(upd.s)
	if err != nil {
		return
	}
	if err := writeFileAtomic(upd.file, b); err != nil {
		core.Logf("updates.json 保存失败: %v", err)
		return
	}
	upd.dirty = false
}

// writeFileAtomic: the file is whole at every moment (written next to it, then renamed over it).
func writeFileAtomic(file string, b []byte) error {
	tmp := file + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// a scanner holding the old file for a moment makes Windows refuse the swap: tried a few times
	for i := 0; ; i++ {
		if err = os.Rename(tmp, file); err == nil {
			return nil
		}
		if i == 3 {
			_ = os.Remove(tmp)
			return err
		}
		time.Sleep(time.Duration(i+1) * 50 * time.Millisecond)
	}
}

func dlList(p *core.Purchase) []dlSeen {
	out := make([]dlSeen, 0, len(p.Downloads))
	for i, d := range p.Downloads {
		n := ""
		if i < len(p.Files) {
			n = p.Files[i]
		}
		out = append(out, dlSeen{d, n})
	}
	return out
}

func sameDLs(a, b []dlSeen) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// assetPurchase: the purchase an asset on disk belongs to, as its card shows it. Caller holds st.Mu.
func assetPurchase(st *core.Store, a *core.Asset) *core.Purchase {
	id, src := booth.AssetBooth(st, a.Key, a)
	if id == "" {
		return nil
	}
	// an id that only came from a .url shortcut must look like the same product, or it is a dependency link
	if b := st.Booth[id]; b != nil && src == "url" && b.Name != "" && !namesRelated(a.Name+" "+a.RawName, b.Name) {
		return nil
	}
	return st.Purchases[id]
}

// panCardOf: the netdisk card an asset on disk came from (downloaded by the program: fromPan), or the share
// linked to it by hand. Caller holds st.Mu.
func panCardOf(st *core.Store, a *core.Asset, fromPan map[string]string) (key string, byProgram bool) {
	for _, l := range a.Locations {
		for p, up := core.PathKey(l.Path), 0; up < 6; up++ {
			if k, ok := fromPan[p]; ok {
				return k, true
			}
			q := filepath.Dir(p)
			if q == p {
				break
			}
			p = q
		}
	}
	if u := st.User[a.Key]; u != nil {
		if surl := netdisk.ShareID(u.ShareURL); surl != "" {
			return netdisk.ShareKey(surl), false
		}
	}
	return "", false
}

// panCovers: does a changed file of a card concern what the player downloaded of it — the file itself, or
// one inside a folder that was downloaded? Nothing recorded means all of the card.
func panCovers(p string, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	for _, s := range parts {
		if s == "/" || p == s || strings.HasPrefix(p, s+"/") {
			return true
		}
	}
	return false
}

// UpdateNotes: what is newer for the assets on disk, by asset key. The store is read, not held while
// updates.json is written. Caller must not hold st.Mu.
func UpdateNotes(st *core.Store) map[string]*UpdateNote {
	upd.mu.Lock()
	defer upd.mu.Unlock()
	s := updLoadLocked()
	now := time.Now().Unix()
	out := map[string]*UpdateNote{}

	st.Mu.RLock()
	fromPan := map[string]string{}
	for k, u := range st.User {
		if core.IsNetdiskKey(k) && u != nil && u.Downloaded != "" && isDirCached(u.Downloaded, now) {
			fromPan[core.PathKey(u.Downloaded)] = k
		}
	}
	local := map[string]bool{} // purchases with something on disk
	for _, a := range st.Assets {
		var n *UpdateNote
		if p := assetPurchase(st, a); p != nil {
			local[p.ID] = true
			n = boothNote(st, s, a, p)
		}
		if n == nil {
			n = panNote(st, s, a, fromPan, now)
		}
		if n == nil {
			continue
		}
		n.Key = a.Key
		if n.At == 0 {
			if s.Since[a.Key] == 0 {
				s.Since[a.Key] = now
				upd.dirty = true
			}
			n.At = s.Since[a.Key]
		}
		out[a.Key] = n
	}
	// a purchase seen for the first time, or with nothing on disk yet: what it offers now is where it starts
	for id, p := range st.Purchases {
		if cur := dlList(p); s.Base[id] == nil || (!local[id] && !sameDLs(s.Base[id], cur)) {
			s.Base[id] = cur
			upd.dirty = true
		}
	}
	for id := range s.Base {
		if st.Purchases[id] == nil {
			delete(s.Base, id)
			upd.dirty = true
		}
	}
	st.Mu.RUnlock()
	for k := range s.Since {
		if out[k] == nil {
			delete(s.Since, k)
			upd.dirty = true
		}
	}
	updSaveLocked()
	return out
}

// boothNote: a later version among the purchase's downloads, or downloads that were not there when the
// asset was last up to date. Caller holds st.Mu and upd.mu.
func boothNote(st *core.Store, s *updState, a *core.Asset, p *core.Purchase) *UpdateNote {
	n := &UpdateNote{Source: "booth", Item: p.ID, CanDL: true, Gumroad: core.IsGumID(p.ID)}
	got := func(dl string) bool { return st.Downloaded[dl] != nil } // fetched by the program already
	if ver, have, dl := purchaseNewerDL(a, p); ver != "" && dl != "" && s.Ver[a.Key] != ver && !got(dl) {
		n.Version, n.Have, n.DLs = ver, have, []string{dl}
		n.Old = oldCopies(a, p, ver)
	}
	if base, ok := s.Base[p.ID]; ok {
		had, names := map[string]bool{}, map[string]string{}
		for _, b := range base {
			had[b.ID] = true
			names[strings.ToLower(b.Name)] = b.ID
		}
		still := map[string]bool{}
		for _, d := range p.Downloads {
			still[d] = true
		}
		fresh := false
		for _, d := range dlList(p) {
			if had[d.ID] || got(d.ID) {
				continue
			}
			fresh = true
			if old, ok := names[strings.ToLower(d.Name)]; ok && d.Name != "" && !still[old] {
				n.Replaced = append(n.Replaced, d.Name)
			} else {
				n.Added = append(n.Added, d.Name)
			}
			if !core.ContainsStr(n.DLs, d.ID) {
				n.DLs = append(n.DLs, d.ID)
			}
		}
		if !fresh && !sameDLs(base, dlList(p)) { // only files taken away, or all of the new ones fetched: up to date
			s.Base[p.ID] = dlList(p)
			upd.dirty = true
		}
	}
	if len(n.DLs) == 0 {
		return nil
	}
	return n
}

// oldCopies: the places of the asset that hold the earlier version of the file that has a later one
// ("Kaguya_v1.06" next to the new "Kaguya_v1.07.zip").
func oldCopies(a *core.Asset, p *core.Purchase, ver string) []OldCopy {
	stems := map[string]bool{}
	for _, f := range p.Files {
		if s, v := splitVer(f); s != "" && v == ver {
			stems[s] = true
		}
	}
	var out []OldCopy
	for _, l := range a.Locations {
		if s, v := splitVer(filepathBase(l.Path)); s != "" && stems[s] && update.VersionNewer(ver, v) {
			out = append(out, OldCopy{Path: l.Path, Size: l.Size, Dir: l.Kind == "dir"})
		}
	}
	return out
}

// panNote: what the share holds now against what the player had. Caller holds st.Mu and upd.mu.
func panNote(st *core.Store, s *updState, a *core.Asset, fromPan map[string]string, now int64) *UpdateNote {
	key, byProgram := panCardOf(st, a, fromPan)
	if key == "" {
		return nil
	}
	surl, sub := netdisk.SplitPanKey(key)
	whole := st.Pan[surl]
	l, _ := netdisk.PanSub(st, key)
	if whole == nil || l == nil || l.Err != "" || l.Truncated || whole.Truncated {
		return nil // not read, or not read in full: nothing to compare
	}
	cur := netdisk.PanFileMap(l)
	if len(cur) == 0 {
		return nil
	}
	sk := key
	var got []string
	if byProgram {
		if u := st.User[key]; u != nil {
			got = u.PanGot
		}
	} else {
		sk = "link:" + a.Key + "|" + surl
	}
	n := &UpdateNote{Source: "pan", PanKey: key, CanDL: byProgram}
	snap := s.Pan[sk]
	keep := func() { // what is there now is what the player has
		if len(cur) <= panSnapMax {
			s.Pan[sk] = &panSeen{At: now, Files: cur}
		} else {
			s.Pan[sk] = &panSeen{At: now}
		}
		upd.dirty = true
	}
	relevant := func(list []string) []string {
		var out []string
		for _, p := range list {
			if panCovers(p, got) {
				out = append(out, p)
			}
		}
		return out
	}
	if snap == nil || snap.Files == nil {
		// no list to compare with: the share's own change notice, when it is later than anything the player did
		seen := a.FirstSeen
		if snap != nil {
			seen = max(seen, snap.At)
		}
		for _, k := range []string{a.Key, key} {
			if u := st.User[k]; u != nil {
				seen = max(seen, u.PanSeen)
			}
		}
		added, removed := shareNews(whole, sub)
		rel := relevant(added)
		if whole.Changed <= seen || len(rel) == 0 {
			if snap == nil {
				keep()
			}
			return nil
		}
		n.Changed, n.Removed, n.At = rel, removed, whole.Changed
	} else {
		for p, sz := range cur {
			if old, ok := snap.Files[p]; !ok {
				n.Added = append(n.Added, p)
			} else if old != sz {
				n.Replaced = append(n.Replaced, p)
			}
		}
		for p := range snap.Files {
			if _, ok := cur[p]; !ok {
				n.Removed = append(n.Removed, p)
			}
		}
		n.Added, n.Replaced = relevant(n.Added), relevant(n.Replaced)
		if len(n.Added)+len(n.Replaced) == 0 {
			if len(cur) != len(snap.Files) || len(n.Removed) > 0 || changedSizes(cur, snap.Files) {
				keep() // changes in parts the player never downloaded, or files taken away
			}
			return nil
		}
		sort.Strings(n.Added)
		sort.Strings(n.Replaced)
		sort.Strings(n.Removed)
		n.At = whole.Changed
	}
	if paths := append(append(append([]string{}, n.Added...), n.Replaced...), n.Changed...); len(paths) <= 200 {
		n.Paths = paths
	}
	return n
}

func changedSizes(a, b map[string]int64) bool {
	for p, sz := range a {
		if b[p] != sz {
			return true
		}
	}
	return false
}

// AckUpdate: the asset is up to date now (the new files were fetched, or the player said so) and stays so
// until something newer turns up. paths: only these parts of a netdisk card were fetched (none: all of it).
func AckUpdate(st *core.Store, key string, paths []string) {
	upd.mu.Lock()
	s := updLoadLocked()
	now := time.Now().Unix()
	st.Mu.Lock()
	var a *core.Asset
	for _, x := range st.Assets {
		if x.Key == key {
			a = x
		}
	}
	saved := false
	if a != nil {
		if p := assetPurchase(st, a); p != nil {
			if ver, _, _ := purchaseNewerDL(a, p); ver != "" {
				s.Ver[key] = ver
			}
			s.Base[p.ID] = dlList(p)
			upd.dirty = true
		}
		fromPan := map[string]string{}
		for k, u := range st.User {
			if core.IsNetdiskKey(k) && u != nil && u.Downloaded != "" {
				fromPan[core.PathKey(u.Downloaded)] = k
			}
		}
		if pk, byProgram := panCardOf(st, a, fromPan); pk != "" {
			sk := pk
			if !byProgram {
				surl, _ := netdisk.SplitPanKey(pk)
				sk = "link:" + key + "|" + surl
			}
			ackPanLocked(st, s, pk, sk, paths, now)
			u := st.User[key]
			if u == nil {
				u = &core.UserData{}
				st.User[key] = u
			}
			u.PanSeen, saved = now, true
		}
	}
	st.Mu.Unlock()
	delete(s.Since, key)
	upd.dirty = true
	updSaveLocked()
	upd.mu.Unlock()
	if saved {
		_ = st.Save()
	}
	core.BumpRev()
}

// PanFetched: a netdisk card was downloaded by the program just now (all of it, or the parts given): the
// assets that came from it have what the share holds.
func PanFetched(st *core.Store, panKey string, paths []string) {
	upd.mu.Lock()
	defer upd.mu.Unlock()
	s := updLoadLocked()
	st.Mu.RLock()
	ackPanLocked(st, s, panKey, panKey, paths, time.Now().Unix())
	st.Mu.RUnlock()
	updSaveLocked()
}

// ackPanLocked: the card's files as they are now are what the player has — all of them, or only the
// parts under paths. Caller holds st.Mu and upd.mu.
func ackPanLocked(st *core.Store, s *updState, panKey, stateKey string, paths []string, now int64) {
	l, _ := netdisk.PanSub(st, panKey)
	if l == nil {
		return
	}
	cur := netdisk.PanFileMap(l)
	snap := s.Pan[stateKey]
	switch {
	case len(cur) > panSnapMax:
		s.Pan[stateKey] = &panSeen{At: now}
	case len(paths) == 0 || snap == nil || snap.Files == nil:
		s.Pan[stateKey] = &panSeen{At: now, Files: cur}
	default:
		for p, sz := range cur {
			for _, part := range paths {
				if p == part || strings.HasPrefix(p, part+"/") {
					snap.Files[p] = sz
				}
			}
		}
		snap.At = now
	}
	upd.dirty = true
}
