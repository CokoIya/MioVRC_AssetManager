package library

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

// Covers from the previews inside unitypackages. A unitypackage is a gzip-compressed tar with one folder per
// asset, named by its GUID, holding "asset", "asset.meta", "pathname" and, for what Unity could draw,
// "preview.png": a thumbnail of up to 128 px (prefabs and models rendered, textures as they are, materials as
// spheres). An asset with no picture of its own gets the best of those previews as its cover, without Unity.
// They are kept apart from the library (pkgcovers.json, covers/unitypackage) and come last: a Booth picture, a
// picture beside the files, one the player picks or one drawn in Unity shows instead as soon as there is one.
//
// How a preview is scored (scorePreview):
//   - what it shows: a prefab 140, a model (fbx, obj …) 70, a texture or sprite 40; materials, audio, animations,
//     fonts and the rest are not taken;
//   - its name against the asset's names: the same 60, one containing the other 35, each shared word 12 (up to 24);
//   - a texture named like a map (normal, mask, metallic, _N, _M …) −60, an icon or one in a folder of icons −50,
//     a "thumb / cover / preview / sample" +30; a texture that comes out of these with less than 12 over its 40
//     is not taken at all — it must be named like the asset or like a picture of it;
//   - a prefab named like a part or a helper (physbone, collider, menu, fx …) −50, as gencover does; these words
//     are whole words of the name (nameWords), not pieces of other words;
//   - under an Editor folder −50, and −2 per folder level beyond the second;
//   - the picture itself: one that is mostly one flat colour (few pixels differ from the edges of their row, little
//     spread of tones) is unusable; otherwise up to +25 for how much of it is not background, +15 for its spread of
//     tones and +10 for its size in bytes (detail).
//
// A prefab preview that is not flat and scores 120 or more is "strong": once one is found, a huge package is not
// read to its end. The tar is streamed once (gzip is sequential): previews are decoded as their path is known,
// only the best one is kept, and previews whose path has not come yet are held up to pkgMaxHeld.

const (
	pkgMaxHeld   = 64         // previews held while their pathname has not been read yet
	pkgMaxPNG    = 512 << 10  // a preview larger than this is not Unity's 128 px thumbnail
	pkgStopAfter = 256 << 20  // compressed bytes after which a strong candidate ends the read
	pkgMaxRead   = 1536 << 20 // compressed bytes read of one package at most
	pkgPerAsset  = 6          // packages looked into for one asset
	pkgZips      = 3          // …and zips, when it has no plain unitypackage
	pkgStrong    = 120
	pkgTexMin    = 12   // what a texture's name must earn it over the 40 for being one (a shared word is 12)
	pkgMinSide   = 16   // pixels a side of a preview, at least …
	pkgMaxSide   = 1024 // … and at most (Unity writes 128)
)

type PkgCover struct {
	File    string `json:"file"`    // in covers/unitypackage
	Package string `json:"package"` // the unitypackage it came from ("<zip>!<name>" for one inside a zip)
	Size    int64  `json:"size"`    // of that file (the zip for one inside a zip)
	MTime   int64  `json:"mtime"`
	Entry   string `json:"entry"` // the asset inside the package whose preview it is
	At      int64  `json:"at"`
}

// PkgFail: nothing usable was found in the asset's packages; not tried again until they change.
type PkgFail struct {
	Sig string `json:"sig"` // the packages that were looked into (pkgSig)
	Why string `json:"why"`
	At  int64  `json:"at"`
}

type pkgFile struct {
	Version int                  `json:"version"`
	Covers  map[string]*PkgCover `json:"covers"`
	Failed  map[string]*PkgFail  `json:"failed,omitempty"`
	Off     map[string]int64     `json:"off,omitempty"` // removed by the player: not extracted again by itself
}

// The list is kept in memory for one data folder at a time: the folder of the library it belongs to (dataDirOf),
// never the program-wide one, so a run that outlives a test does not reach into the next test's folder.
var (
	pkgMu  sync.RWMutex
	pkgDir string // the data folder the list was read from
	pkgAll *pkgFile
)

func pkgCoverDir(dir string) string  { return filepath.Join(dir, "covers", "unitypackage") }
func pkgCoverFile(dir string) string { return filepath.Join(dir, "pkgcovers.json") }

// dataDirOf: the data folder of a library — where its file is (core.DataDir, in the program).
func dataDirOf(st *core.Store) string {
	if st.Path != "" {
		return filepath.Dir(st.Path)
	}
	return core.DataDir
}

// pkgCovers: the list, read once per data folder. An entry whose picture is gone is dropped. For looking only
// (under pkgMu.RLock): a change goes through pkgChange, which takes the list in memory at that moment.
func pkgCovers(dir string) *pkgFile {
	pkgMu.RLock()
	f, ok := pkgAll, pkgAll != nil && pkgDir == dir
	pkgMu.RUnlock()
	if ok {
		return f
	}
	pkgMu.Lock()
	defer pkgMu.Unlock()
	return pkgLoadLocked(dir)
}

// pkgLoadLocked: the list of the data folder dir, read from its file when it is not the one in memory. Caller
// holds pkgMu for writing.
func pkgLoadLocked(dir string) *pkgFile {
	if pkgAll != nil && pkgDir == dir {
		return pkgAll
	}
	f := &pkgFile{}
	if b, err := os.ReadFile(pkgCoverFile(dir)); err == nil {
		_ = json.Unmarshal(b, f)
	}
	covers := map[string]*PkgCover{}
	for k, c := range f.Covers {
		if c != nil && c.File != "" && core.StatOK(filepath.Join(pkgCoverDir(dir), filepath.Base(c.File))) {
			covers[k] = c
		}
	}
	f.Covers = covers
	if f.Failed == nil {
		f.Failed = map[string]*PkgFail{}
	}
	if f.Off == nil {
		f.Off = map[string]int64{}
	}
	pkgAll, pkgDir = f, dir
	return f
}

// pkgChange changes the list and saves it as one step under the lock. The list is fetched inside the lock: a
// library import (or its undo) may have dropped the one a caller looked at a moment ago, and a change made
// to that one would be lost — or, saved, would put the old list back over the imported file. change says
// whether there is anything to save.
func pkgChange(dir string, change func(f *pkgFile) bool) error {
	pkgMu.Lock()
	defer pkgMu.Unlock()
	f := pkgLoadLocked(dir)
	if !change(f) {
		return nil
	}
	return savePkgCoversLocked(dir, f)
}

func savePkgCoversLocked(dir string, f *pkgFile) error {
	f.Version = 1
	b, _ := json.MarshalIndent(f, "", " ")
	tmp := pkgCoverFile(dir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, pkgCoverFile(dir)) // whole at every moment
}

// PackageCover: the picture taken from the asset's unitypackage, "" when it has none.
func PackageCover(st *core.Store, key string) string {
	dir := dataDirOf(st)
	f := pkgCovers(dir)
	pkgMu.RLock()
	defer pkgMu.RUnlock()
	if c := f.Covers[key]; c != nil {
		return filepath.Join(pkgCoverDir(dir), filepath.Base(c.File))
	}
	return ""
}

// PackageCoverInfo: where the asset's unitypackage cover came from, nil when it has none.
func PackageCoverInfo(st *core.Store, key string) *PkgCover {
	f := pkgCovers(dataDirOf(st))
	pkgMu.RLock()
	defer pkgMu.RUnlock()
	if c := f.Covers[key]; c != nil {
		cp := *c
		return &cp
	}
	return nil
}

// setPkgCover keeps a preview as the asset's cover. The file is named after what it came from, so another
// package or entry gives another path (the window caches a picture by its path).
func setPkgCover(dir, key string, pk *pkgPick, src pkgSource) (*PkgCover, error) {
	if err := os.MkdirAll(pkgCoverDir(dir), 0755); err != nil {
		return nil, err
	}
	h := sha1.Sum([]byte(key + "|" + src.name + "|" + strconv.FormatInt(src.size, 10) + "|" + strconv.FormatInt(src.mtime, 10) + "|" + pk.entry))
	name := hex.EncodeToString(h[:10]) + ".png"
	dst := filepath.Join(pkgCoverDir(dir), name)
	if err := os.WriteFile(dst+".part", pk.png, 0644); err != nil {
		return nil, err
	}
	if err := os.Rename(dst+".part", dst); err != nil {
		_ = os.Remove(dst + ".part")
		return nil, err
	}
	c := &PkgCover{File: name, Package: src.name, Size: src.size, MTime: src.mtime, Entry: pk.entry, At: time.Now().Unix()}
	var old *PkgCover
	err := pkgChange(dir, func(f *pkgFile) bool {
		old = f.Covers[key]
		f.Covers[key] = c
		delete(f.Failed, key)
		delete(f.Off, key)
		return true
	})
	if old != nil && old.File != name {
		_ = os.Remove(filepath.Join(pkgCoverDir(dir), filepath.Base(old.File)))
	}
	return c, err
}

func setPkgFail(dir, key, sig, why string) {
	swapPkgFail(dir, key, &PkgFail{Sig: sig, Why: why, At: time.Now().Unix()})
}

// swapPkgFail puts fail in the place of what is remembered of the asset's last failed read (nil: nothing is
// remembered any more) and returns what was there.
func swapPkgFail(dir, key string, fail *PkgFail) (prev *PkgFail) {
	err := pkgChange(dir, func(f *pkgFile) bool {
		prev = f.Failed[key]
		if fail == nil {
			delete(f.Failed, key)
			return prev != nil
		}
		f.Failed[key] = fail
		return true
	})
	if err != nil {
		core.Logf("pkgcovers.json 保存失败: %v", err)
	}
	return prev
}

// RemovePkgCover takes the asset's unitypackage cover away again; it is not extracted again by itself (the
// button in the details panel does).
func RemovePkgCover(st *core.Store, key string) bool {
	dir := dataDirOf(st)
	var old *PkgCover
	err := pkgChange(dir, func(f *pkgFile) bool {
		if old = f.Covers[key]; old == nil {
			return false
		}
		delete(f.Covers, key)
		f.Off[key] = time.Now().Unix()
		return true
	})
	if err != nil {
		core.Logf("pkgcovers.json 保存失败: %v", err)
	}
	if old == nil {
		return false
	}
	_ = os.Remove(filepath.Join(pkgCoverDir(dir), filepath.Base(old.File)))
	core.BumpRev()
	return true
}

// pkgcovers.json replaced by a library import or its undo: read again at the next use
func init() {
	OnDataImported(func() {
		pkgMu.Lock()
		pkgAll = nil
		pkgMu.Unlock()
	})
}

// ---------- reading one package ----------

type pkgPick struct {
	entry  string
	png    []byte
	score  int
	strong bool
}

// pkgSource: a package to read: a file, or an entry of a zip.
type pkgSource struct {
	name  string // the path, "<zip>!<entry>" inside a zip
	size  int64
	mtime int64
	open  func() (io.ReadCloser, error)
}

// countReader counts the compressed bytes that went by, and keeps the error the disk gave, if any.
type countReader struct {
	r   io.Reader
	n   int64
	err error
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if isDiskError(err) {
		c.err = err
	}
	return n, err
}

// isDiskError: the system refused to open or read the file (in use by another program, no permission, the
// disk or the network) — not: the file ends early, or what is in it is damaged.
func isDiskError(err error) bool {
	var pe *fs.PathError
	var se *os.SyscallError
	var no syscall.Errno
	return errors.As(err, &pe) || errors.As(err, &se) || errors.As(err, &no)
}

var (
	errPkgNone      = errors.New("unitypackage 中没有可用的预览图")
	errPkgCancelled = errors.New("已取消")
)

// pkgIOError: the package could not be opened or read (in use by another program, the disk, the network) —
// which says nothing about what is in it, so it is not remembered as a failure: the next run tries again.
type pkgIOError struct{ err error }

func (e *pkgIOError) Error() string {
	return "无法读取 unitypackage（" + core.TrimErr(e.err) + "）"
}
func (e *pkgIOError) Unwrap() error { return e.err }

func isPkgIOError(err error) bool {
	var e *pkgIOError
	return errors.As(err, &e)
}

// pickPreview streams one package and returns its best preview for an asset known by names, or errPkgNone.
// cancel: stop early (what was found so far is returned, as if the package ended there).
func pickPreview(r io.Reader, names []string, cancel func() bool) (*pkgPick, error) {
	cr := &countReader{r: r}
	gz, err := gzip.NewReader(cr)
	if err != nil {
		if cr.err != nil {
			return nil, &pkgIOError{cr.err}
		}
		return nil, errors.New("不是有效的 unitypackage")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	keys := nameKeys(names)
	type ent struct {
		path string
		png  []byte
		seq  int
	}
	ents := map[string]*ent{}
	held, seq := 0, 0 // previews whose path is not known yet
	var best *pkgPick
	consider := func(e *ent) {
		if e.png == nil || e.path == "" {
			return
		}
		png := e.png
		e.png = nil
		held--
		if pk := scorePreview(e.path, png, keys, best); pk != nil {
			best = pk
		}
	}
	for {
		if cancel != nil && cancel() {
			break
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if cr.err != nil {
				return nil, &pkgIOError{cr.err} // the file could not be read: whatever was found is not its best
			}
			if best != nil {
				break // a damaged tail: what came before it is still good
			}
			return nil, errors.New("unitypackage 读取不完整（文件可能已损坏）")
		}
		if cr.n > pkgMaxRead || (cr.n > pkgStopAfter && best != nil && best.strong) {
			break
		}
		guid, part := pkgEntryName(h.Name)
		if guid == "" {
			continue
		}
		switch part {
		case "pathname":
			b, _ := io.ReadAll(io.LimitReader(tr, 4096))
			line := strings.SplitN(strings.ReplaceAll(string(b), "\r", ""), "\n", 2)[0]
			e := ents[guid]
			if e == nil {
				e = &ent{}
				ents[guid] = e
			}
			e.path = strings.TrimSpace(line)
			consider(e)
		case "preview.png":
			if h.Size <= 0 || h.Size > pkgMaxPNG {
				continue
			}
			b, err := io.ReadAll(io.LimitReader(tr, pkgMaxPNG))
			if err != nil || len(b) == 0 {
				continue
			}
			e := ents[guid]
			if e == nil {
				e = &ent{}
				ents[guid] = e
			}
			seq++
			e.png, e.seq = b, seq
			held++
			consider(e)
			if held > pkgMaxHeld { // the path of these is still to come: the oldest one is let go
				oldest, at := "", 0
				for g, x := range ents {
					if x.png != nil && (oldest == "" || x.seq < at) {
						oldest, at = g, x.seq
					}
				}
				if oldest != "" {
					ents[oldest].png = nil
					held--
				}
			}
		}
	}
	if best == nil {
		return nil, errPkgNone
	}
	return best, nil
}

// pkgEntryName: "<guid>/<part>" of a tar entry (Unity writes "./<guid>/<part>").
func pkgEntryName(name string) (guid, part string) {
	name = strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "./")
	i := strings.IndexByte(name, '/')
	if i <= 0 {
		return "", ""
	}
	g := strings.ToLower(name[:i])
	if !IsHex32(g) {
		return "", ""
	}
	return g, name[i+1:]
}

// What a name says is looked for in its words (nameWords: "SailorDress_PhysBone" is "sailor dress phys bone"),
// a whole word at a time — "anim" is not in Animal or Anime, "icon" not in Silicone, "test" not in Latest,
// "bone" not in Trombone, "main" not in Remain. Japanese and Chinese are written without spaces: those words
// are looked for anywhere.
var (
	reMapTex   = regexp.MustCompile(`(?:^| )(?:normal|nrm|nml|mask|metal|metallic|rough|roughness|smooth|smoothness|ao|occlusion|emis|emission|emissive|matcap|height|bump|spec|specular|gloss|alpha|opacity|ramp|gradient|lut|noise|shadow|rim|outline|uv|uvmap|id|n|m|r|e|h|mt)(?: |$)`)
	reThumbTex = regexp.MustCompile(`(?:^| )(?:thumb(?:nail)?s?|covers?|previews?|samples?|main)(?: |$)|サムネ|封面|预览|メイン|商品`)
	reIconTex  = regexp.MustCompile(`(?:^| )icons?(?: |$)|アイコン|图标`)
	// parts of an outfit that come as prefabs of their own, and helpers nobody wants as a cover
	rePartPrefab = regexp.MustCompile(`(?:^| )(?:phys ?bones?|colliders?|armatures?|bones?|icons?|menus?|param(?:s|eters?)?|toggles?|fx|anim(?:s|ations?|ators?)?|particles?|shaders?|materials?|samples?|tests?|modular|vrc ?fury|ndmf|lil ?toon|poiyomi|gestures?)(?: |$)`)
	reWordTok3   = regexp.MustCompile(`[a-z0-9]{3,}`)
)

// nameWords: a file or folder name as its words, in small letters with one space between them. A word ends
// at anything that is not a letter or a digit (space, "_", "-", "." …), where small letters turn into a
// capital ("sailorDress"), before the last capital of a run that goes on in small letters ("FXLayer",
// "VRCFury"), and between letters and digits ("Test01"); text outside ASCII is a word of its own.
func nameWords(name string) string {
	const (
		none = iota
		lower
		upper
		digit
		other // not ASCII
	)
	kind := func(r rune) int {
		switch {
		case r >= 'a' && r <= 'z':
			return lower
		case r >= 'A' && r <= 'Z':
			return upper
		case r >= '0' && r <= '9':
			return digit
		case r > 127 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			return other
		}
		return none
	}
	rs := []rune(name)
	var b strings.Builder
	prev := none
	for i, r := range rs {
		k := kind(r)
		if k == none {
			prev = none
			continue
		}
		cut := prev == none || k != prev && !(prev == upper && k == lower)
		if prev == upper && k == upper && i+1 < len(rs) && kind(rs[i+1]) == lower {
			cut = true
		}
		if cut && b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(unicode.ToLower(r))
		prev = k
	}
	return b.String()
}

// isPartName: the name (of a prefab, of a package) is that of a part or a helper.
func isPartName(name string) bool { return rePartPrefab.MatchString(nameWords(name)) }

// nameKeys: the asset's names as NormKey, the ones worth comparing.
func nameKeys(names []string) []string {
	var out []string
	for _, n := range names {
		if k := naming.NormKey(naming.CleanName(core.StripArchiveExt(n))); len([]rune(k)) >= 3 && !core.ContainsStr(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// nameScore: how the file name of an entry matches the asset's names.
func nameScore(base string, keys []string) int {
	nk := naming.NormKey(base)
	if len([]rune(nk)) < 3 {
		return 0
	}
	best := 0
	for _, k := range keys {
		s := 0
		switch {
		case nk == k:
			s = 60
		case len([]rune(k)) >= 4 && strings.Contains(nk, k), len([]rune(nk)) >= 4 && strings.Contains(k, nk):
			s = 35
		default:
			kw := reWordTok3.FindAllString(k, -1)
			for _, w := range reWordTok3.FindAllString(nk, -1) {
				if !naming.IsMostlyDigits(w) && core.ContainsStr(kw, w) {
					s += 12
				}
			}
			s = min(s, 24)
		}
		best = max(best, s)
	}
	return best
}

// previewKind: the score an entry starts with for what it is (0: not taken), and whether it is a texture.
func previewKind(ext string) (base int, texture bool) {
	switch ext {
	case ".prefab":
		return 140, false
	case ".fbx", ".obj", ".blend", ".dae", ".3ds", ".vrm", ".glb", ".gltf":
		return 70, false
	case ".png", ".jpg", ".jpeg", ".tga", ".psd", ".psb", ".tif", ".tiff", ".bmp", ".exr", ".webp", ".gif", ".dds", ".hdr":
		return 40, true
	}
	return 0, false
}

// inIconFolder: one of the folders the entry lies in is named for icons ("Icons", "icon", "MenuIcons").
func inIconFolder(p string) bool {
	dirs := strings.Split(p, "/")
	for _, d := range dirs[:len(dirs)-1] {
		if reIconTex.MatchString(nameWords(d)) {
			return true
		}
	}
	return false
}

// scorePreview: the entry as a candidate, or nil when it is not worth taking or no better than best.
func scorePreview(p string, pngData []byte, keys []string, best *pkgPick) *pkgPick {
	p = strings.ReplaceAll(p, "\\", "/")
	lp := strings.ToLower(p)
	ext := path.Ext(lp)
	base, texture := previewKind(ext)
	if base == 0 {
		return nil
	}
	fname := strings.TrimSuffix(path.Base(p), path.Ext(p))
	words := nameWords(fname)
	s := base + nameScore(fname, keys)
	if texture {
		if reMapTex.MatchString(words) {
			s -= 60
		}
		if reIconTex.MatchString(words) || inIconFolder(p) {
			s -= 50
		}
		if reThumbTex.MatchString(words) {
			s += 30
		}
		// a texture is only taken for what its name says: any texture has a picture to show, and without this
		// a menu glyph or a matcap would stand for the asset whenever nothing better is in the package
		if s < base+pkgTexMin {
			return nil
		}
	} else if rePartPrefab.MatchString(words) {
		s -= 50
	}
	if strings.Contains(lp, "/editor/") {
		s -= 50
	}
	s -= 2 * max(0, strings.Count(p, "/")-2)
	// the picture is only decoded when it could still win
	if best != nil && s+50 <= best.score {
		return nil
	}
	// its size first, from the header alone: decoding allocates what the header declares, and a few hundred
	// bytes can declare gigabytes
	cfg, err := png.DecodeConfig(bytes.NewReader(pngData))
	if err != nil || cfg.Width < pkgMinSide || cfg.Height < pkgMinSide || cfg.Width > pkgMaxSide || cfg.Height > pkgMaxSide {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil
	}
	ratio, spread := imageStats(img)
	if ratio < 0.02 || spread < 4 {
		return nil // one flat colour: nothing to see
	}
	s += min(int(ratio*50), 25) + min(int(spread/3), 15) + min(len(pngData)/2048, 10)
	if best != nil && s <= best.score {
		return nil
	}
	if !texture {
		pngData = framePreview(img, pngData)
	}
	return &pkgPick{entry: p, png: pngData, score: s, strong: ext == ".prefab" && s >= pkgStrong}
}

// framePreview: Unity frames a prefab by its bounds, and an avatar with wide bounds ends up a speck in the
// middle of its preview. When what is drawn fills less than 0.7 of the frame, the picture is cut to a square
// around it (an eighth of margin, kept inside the frame) and enlarged, to 256 px at most and 4 times at most.
// Anything else comes back as it was.
func framePreview(img image.Image, pngData []byte) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	x0, y0, x1, y1 := w, h, -1, -1
	for y := 0; y < h; y++ {
		lr, lg, lb, _ := img.At(b.Min.X, b.Min.Y+y).RGBA()
		rr, rg, rb, _ := img.At(b.Min.X+w-1, b.Min.Y+y).RGBA()
		br, bg, bb := int(lr>>8+rr>>8)/2, int(lg>>8+rg>>8)/2, int(lb>>8+rb>>8)/2
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a>>8 < 32 || abs(int(r>>8)-br)+abs(int(g>>8)-bg)+abs(int(bl>>8)-bb) <= 48 {
				continue
			}
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
		}
	}
	if x1 < 0 {
		return pngData
	}
	frame := min(w, h)
	side := max(x1-x0+1, y1-y0+1)
	side += max(side/4, 4)
	if side < 12 || side*10 >= frame*7 {
		return pngData
	}
	cx, cy := (x0+x1+1)/2, (y0+y1+1)/2
	sx := min(max(cx-side/2, 0), w-side)
	sy := min(max(cy-side/2, 0), h-side)
	out := min(256, side*4)
	dst := image.NewNRGBA(image.Rect(0, 0, out, out))
	at := func(x, y int) [4]float64 {
		c := color.NRGBAModel.Convert(img.At(b.Min.X+sx+min(max(x, 0), side-1), b.Min.Y+sy+min(max(y, 0), side-1))).(color.NRGBA)
		return [4]float64{float64(c.R), float64(c.G), float64(c.B), float64(c.A)}
	}
	for y := 0; y < out; y++ {
		fy := (float64(y)+0.5)*float64(side)/float64(out) - 0.5
		iy := int(math.Floor(fy))
		ty := fy - float64(iy)
		for x := 0; x < out; x++ {
			fx := (float64(x)+0.5)*float64(side)/float64(out) - 0.5
			ix := int(math.Floor(fx))
			tx := fx - float64(ix)
			p00, p10, p01, p11 := at(ix, iy), at(ix+1, iy), at(ix, iy+1), at(ix+1, iy+1)
			var v [4]uint8
			for i := range v {
				v[i] = uint8(math.Round((p00[i]*(1-tx)+p10[i]*tx)*(1-ty) + (p01[i]*(1-tx)+p11[i]*tx)*ty))
			}
			dst.SetNRGBA(x, y, color.NRGBA{v[0], v[1], v[2], v[3]})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return pngData
	}
	return buf.Bytes()
}

// imageStats: how much of a preview is not its background — the colour at the two ends of each row, which
// follows the vertical gradient Unity draws prefabs on — and how spread its tones are (the standard deviation
// of the luminance of its opaque pixels). Both are 0 for a picture of one flat colour.
func imageStats(img image.Image) (ratio, spread float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return 0, 0
	}
	rgb := func(x, y int) (int, int, int, int) {
		r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return int(r >> 8), int(g >> 8), int(bl >> 8), int(a >> 8)
	}
	var sum, sum2 float64
	n, diff := 0, 0
	for y := 0; y < h; y++ {
		lr, lg, lb, _ := rgb(0, y)
		rr, rg, rb, _ := rgb(w-1, y)
		br, bg, bb := (lr+rr)/2, (lg+rg)/2, (lb+rb)/2
		for x := 0; x < w; x++ {
			r, g, bl, a := rgb(x, y)
			if a < 32 {
				continue // transparent: background
			}
			n++
			l := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)
			sum += l
			sum2 += l * l
			if abs(r-br)+abs(g-bg)+abs(bl-bb) > 48 {
				diff++
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	mean := sum / float64(n)
	return float64(diff) / float64(w*h), math.Sqrt(max(sum2/float64(n)-mean*mean, 0))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---------- the packages of one asset ----------

// pkgSources: the packages worth reading for an asset, the most promising first: those named after it, then
// the larger ones (the product itself rather than a bundled dependency or a small add-on). Without a plain
// unitypackage, the ones inside its zips (not encrypted) are read in the same way.
func pkgSources(a *core.Asset, names []string) []pkgSource {
	keys := nameKeys(names)
	type cand struct {
		src   pkgSource
		score int
	}
	rank := func(name string, size int64) int {
		s := nameScore(core.StripArchiveExt(filepath.Base(name)), keys) * 10
		if isPartName(core.StripArchiveExt(filepath.Base(name))) {
			s -= 300
		}
		return s + min(int(size>>20), 200) // a bigger package is more likely the product
	}
	var cs []cand
	for _, p := range a.Packages {
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		p := p
		cs = append(cs, cand{pkgSource{name: p, size: fi.Size(), mtime: fi.ModTime().Unix(), open: func() (io.ReadCloser, error) { return pkgOpenFile(p) }}, rank(p, fi.Size())})
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].score > cs[j].score })
	if len(cs) > pkgPerAsset {
		cs = cs[:pkgPerAsset]
	}
	var out []pkgSource
	for _, c := range cs {
		out = append(out, c.src)
	}
	if len(out) > 0 {
		return out
	}
	// inside its zips: a second level, only when there is no plain package to read
	zips := 0
	for _, z := range a.Archives {
		if zips >= pkgZips {
			break
		}
		fi, err := os.Stat(z)
		if err != nil || core.LowerExt(z) != ".zip" {
			continue
		}
		zr, err := zip.OpenReader(z)
		if err != nil {
			continue
		}
		var zc []cand
		for _, f := range zr.File {
			if f.Flags&1 != 0 || core.LowerExt(f.Name) != ".unitypackage" || f.FileInfo().IsDir() || f.UncompressedSize64 == 0 {
				continue
			}
			zpath, f := z, f
			zc = append(zc, cand{pkgSource{name: zpath + "!" + f.Name, size: fi.Size(), mtime: fi.ModTime().Unix(), open: func() (io.ReadCloser, error) {
				zr, err := zip.OpenReader(zpath)
				if err != nil {
					return nil, err
				}
				for _, g := range zr.File {
					if g.Name == f.Name {
						rc, err := g.Open()
						if err != nil {
							zr.Close()
							return nil, err
						}
						return &zipEntryReader{rc, zr}, nil
					}
				}
				zr.Close()
				return nil, errors.New("zip 中没有该文件")
			}}, rank(f.Name, int64(f.UncompressedSize64))})
		}
		zr.Close()
		if len(zc) == 0 {
			continue
		}
		zips++
		sort.SliceStable(zc, func(i, j int) bool { return zc[i].score > zc[j].score })
		for i, c := range zc {
			if i >= pkgZips {
				break
			}
			out = append(out, c.src)
		}
	}
	return out
}

// pkgOpenFile opens a package on the disk (its own variable for the tests: a file another program holds).
var pkgOpenFile = func(p string) (io.ReadCloser, error) { return os.Open(p) }

type zipEntryReader struct {
	io.ReadCloser
	zr *zip.ReadCloser
}

func (z *zipEntryReader) Close() error {
	err := z.ReadCloser.Close()
	z.zr.Close()
	return err
}

// pkgSig: the asset's packages and zips with their size and time: a failure is not tried again until one of
// them changes.
func pkgSig(a *core.Asset) string {
	var lines []string
	for _, p := range append(append([]string{}, a.Packages...), a.Archives...) {
		if fi, err := os.Stat(p); err == nil {
			lines = append(lines, core.PathKey(p)+"|"+strconv.FormatInt(fi.Size(), 10)+"|"+strconv.FormatInt(fi.ModTime().Unix(), 10))
		}
	}
	sort.Strings(lines)
	h := sha1.Sum([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h[:8])
}

// pkgWorkMu: one package is read at a time, whoever asks.
var pkgWorkMu sync.Mutex

// extractPkgCover reads the asset's packages, the most promising first, until a strong preview turns up or they
// are all read, and keeps the best one as its cover in the data folder dir. Caller must not hold st.Mu.
// Nothing is kept of a read that was cancelled (errPkgCancelled): the best preview of half a package would
// stand as the cover of the whole, with the file's size and time, and never be looked at again. A package that
// could not be opened or read gives a pkgIOError when nothing better came of the others.
func extractPkgCover(dir, key string, a *core.Asset, names []string, cancel func() bool) (*PkgCover, error) {
	srcs := pkgSources(a, names)
	if len(srcs) == 0 {
		return nil, errors.New("该素材没有可读取的 unitypackage")
	}
	pkgWorkMu.Lock()
	defer pkgWorkMu.Unlock()
	var best *pkgPick
	var bestSrc pkgSource
	var lastErr, ioErr error
	for _, src := range srcs {
		if cancel != nil && cancel() {
			break
		}
		rc, err := src.open()
		if err != nil {
			if isDiskError(err) {
				ioErr = &pkgIOError{err}
			} else {
				lastErr = err
			}
			continue
		}
		pk, err := pickPreview(rc, names, cancel)
		rc.Close()
		if err != nil {
			if isPkgIOError(err) {
				ioErr = err
			} else {
				lastErr = err
			}
			continue
		}
		if best == nil || pk.score > best.score {
			best, bestSrc = pk, src
		}
		if best.strong {
			break
		}
	}
	if cancel != nil && cancel() {
		return nil, errPkgCancelled
	}
	if best == nil {
		switch {
		case ioErr != nil:
			return nil, ioErr
		case lastErr == nil:
			lastErr = errPkgNone
		}
		return nil, lastErr
	}
	return setPkgCover(dir, key, best, bestSrc)
}

// ---------- what the window asks ----------

type PkgCoverStatus struct {
	Has     bool      `json:"has"`              // it has a unitypackage (or a zip that may hold one) to read
	Cover   *PkgCover `json:"cover,omitempty"`  // the cover it shows came from a package
	Failed  string    `json:"failed,omitempty"` // the last try found nothing: why
	Off     bool      `json:"off,omitempty"`    // the player removed its cover: not extracted again by itself
	Busy    bool      `json:"busy"`             // being read right now
	Running bool      `json:"running"`          // a run over the library is under way
}

func PkgCoverStatusOf(st *core.Store, key string) PkgCoverStatus {
	var s PkgCoverStatus
	st.Mu.RLock()
	for _, a := range st.Assets {
		if a.Key == key {
			s.Has = len(a.Packages) > 0 || len(a.Archives) > 0 || a.ZipPackages > 0
			break
		}
	}
	st.Mu.RUnlock()
	f := pkgCovers(dataDirOf(st))
	pkgMu.RLock()
	if c := f.Covers[key]; c != nil {
		cp := *c
		s.Cover = &cp
	}
	if x := f.Failed[key]; x != nil {
		s.Failed = x.Why
	}
	_, s.Off = f.Off[key]
	pkgMu.RUnlock()
	pkgJobMu.Lock()
	s.Busy, s.Running = pkgJob.nowKey == key, pkgJob.Running
	pkgJobMu.Unlock()
	return s
}

// pkgAssetOf: the asset and the names it goes by. Caller must not hold st.Mu.
func pkgAssetOf(st *core.Store, key string) (*core.Asset, []string) {
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	for _, a := range st.Assets {
		if a.Key == key || (a.AltKey != "" && a.AltKey == key) {
			v := BuildView(st, a)
			names := []string{v.Name, a.Name, a.RawName}
			for _, p := range a.Packages {
				names = append(names, filepath.Base(p))
			}
			cp := *a
			return &cp, names
		}
	}
	return nil, nil
}

// ExtractPkgCover: the button in the details panel: the asset's packages are read now, whatever was found or
// decided before.
func ExtractPkgCover(st *core.Store, key string) (*PkgCover, error) {
	a, names := pkgAssetOf(st, key)
	if a == nil {
		return nil, errors.New("该素材不在本地素材库中")
	}
	c, err := extractPkgCover(dataDirOf(st), key, a, names, func() bool { return core.Quitting.Load() })
	if err != nil {
		return nil, err
	}
	core.BumpRev()
	return c, nil
}

// ---------- the run over the library ----------

var TaskPkgCover = &core.Task{Name: "pkgcover", Label: "提取 unitypackage 封面"}

// pkgShowEvery: how often, at most, the window is told of the covers a run has made so far.
var pkgShowEvery = 5 * time.Second

// pkgWhyCrashed: what stays written of a package the program did not come back from reading.
const pkgWhyCrashed = "读取时程序意外退出，已不再自动读取"

type PkgBatch struct {
	Running bool   `json:"running"`
	Manual  bool   `json:"manual,omitempty"` // started from the window (not after a scan)
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Made    int    `json:"made"`
	Failed  int    `json:"failed"`
	Now     string `json:"now,omitempty"`  // the asset being read
	Last    string `json:"last,omitempty"` // why the last asset that failed did
	Err     string `json:"err,omitempty"`  // why it stopped early
	Ended   int64  `json:"ended,omitempty"`
	nowKey  string
	cancel  bool
	again   bool // asked for again while running
}

var (
	pkgJobMu sync.Mutex
	pkgJob   PkgBatch
)

func PkgBatchSnapshot() PkgBatch {
	pkgJobMu.Lock()
	defer pkgJobMu.Unlock()
	return pkgJob
}

func CancelPkgBatch() {
	pkgJobMu.Lock()
	pkgJob.cancel, pkgJob.again = true, false
	pkgJobMu.Unlock()
}

// StopPkgBatch cancels the run and waits for it to end, for wait at most. false: it is still running. A library
// import and its undo replace pkgcovers.json under the program and call this first (the scan that follows them
// starts the run again).
func StopPkgBatch(wait time.Duration) bool {
	for end := time.Now().Add(wait); ; time.Sleep(20 * time.Millisecond) {
		pkgJobMu.Lock()
		running := pkgJob.Running
		if running {
			pkgJob.cancel, pkgJob.again = true, false
		}
		pkgJobMu.Unlock()
		if !running {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}

// importRunning: a library import or its undo is replacing the data files.
func importRunning() bool {
	r := TransferStatus()
	return r.Running && r.What != "export"
}

type pkgTodo struct {
	key   string
	asset *core.Asset
	names []string
	name  string
}

// pkgTodos: the assets to look into: those showing no picture at all, with something to read. manual: the
// ones that failed before are tried again; otherwise only when their packages changed. What the player
// removed by hand is left alone either way. Caller must not hold st.Mu.
func pkgTodos(st *core.Store, manual bool) []pkgTodo {
	var todo []pkgTodo
	st.Mu.RLock()
	for _, a := range st.Assets {
		if len(a.Packages) == 0 && len(a.Archives) == 0 {
			continue
		}
		if u := st.User[a.Key]; u != nil && u.Hidden {
			continue
		}
		if isPSDOnly(a) {
			continue
		}
		v := BuildView(st, a)
		if v.Cover != "" && !v.CoverPkg {
			continue
		}
		names := []string{v.Name, a.Name, a.RawName}
		for _, p := range a.Packages {
			names = append(names, filepath.Base(p))
		}
		cp := *a
		todo = append(todo, pkgTodo{key: a.Key, asset: &cp, names: names, name: v.Name})
	}
	st.Mu.RUnlock()
	f := pkgCovers(dataDirOf(st))
	var out []pkgTodo
	for _, t := range todo {
		pkgMu.RLock()
		c, fail := f.Covers[t.key], f.Failed[t.key]
		_, off := f.Off[t.key]
		pkgMu.RUnlock()
		if off || (fail != nil && !manual && fail.Sig == pkgSig(t.asset)) {
			continue
		}
		if c != nil { // read again only when the package it came from changed or went
			if fi, err := os.Stat(pkgCoverSource(c.Package)); err == nil && fi.Size() == c.Size && fi.ModTime().Unix() == c.MTime {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// pkgCoverSource: the file a cover's package is: the package itself, or the zip of "<zip>!<entry>". A "!" is
// an ordinary character of a path ("D:\!VRC", "NEW! Dress"): the whole is tried first, and only a ".zip!"
// (in any case) divides — the first one that leaves a file there is.
func pkgCoverSource(pkg string) string {
	if fi, err := os.Stat(pkg); err == nil && !fi.IsDir() {
		return pkg
	}
	const mark = ".zip!"
	for i := 0; i+len(mark) <= len(pkg); i++ {
		if strings.EqualFold(pkg[i:i+len(mark)], mark) {
			z := pkg[:i+len(mark)-1]
			if fi, err := os.Stat(z); err == nil && !fi.IsDir() {
				return z
			}
		}
	}
	return pkg
}

// KickPkgCovers looks into the packages of the assets that have no picture, in the background, after a scan.
// A call while a run is under way makes it go round once more.
func KickPkgCovers(st *core.Store) {
	if pkgAuto {
		startPkgBatch(st, false)
	}
}

// pkgAuto: the run after a scan (off in the package's tests, which swap data folders under a run that outlives
// them; they start the run themselves where it is looked at)
var pkgAuto = true

// StartPkgBatch: 「为没有封面的素材提取封面」, from the window.
func StartPkgBatch(st *core.Store) error {
	pkgJobMu.Lock()
	running := pkgJob.Running
	pkgJobMu.Unlock()
	if running {
		return errors.New("正在提取封面，请等待完成或先取消")
	}
	if importRunning() {
		return errors.New("另一项导出或导入正在进行")
	}
	if len(pkgTodos(st, true)) == 0 {
		return errors.New("没有封面的素材中，没有可读取的 unitypackage")
	}
	startPkgBatch(st, true)
	return nil
}

func startPkgBatch(st *core.Store, manual bool) {
	pkgJobMu.Lock()
	if pkgJob.Running {
		pkgJob.again = true
		pkgJobMu.Unlock()
		return
	}
	if importRunning() { // (asked under the lock StopPkgBatch takes: a run does not slip in behind it)
		pkgJobMu.Unlock()
		return
	}
	pkgJob = PkgBatch{Running: true, Manual: manual}
	pkgJobMu.Unlock()
	go func() {
		for {
			core.RunTask(TaskPkgCover, func() { runPkgBatch(st, manual) })
			pkgJobMu.Lock()
			again := pkgJob.again && !pkgJob.cancel && !core.Quitting.Load()
			if !again {
				pkgJob.Running = false
				pkgJobMu.Unlock()
				return
			}
			pkgJob = PkgBatch{Running: true}
			manual = false
			pkgJobMu.Unlock()
		}
	}()
}

func runPkgBatch(st *core.Store, manual bool) {
	stopped := func() bool {
		pkgJobMu.Lock()
		defer pkgJobMu.Unlock()
		return pkgJob.cancel || core.Quitting.Load()
	}
	todos := pkgTodos(st, manual)
	dir := dataDirOf(st)
	pkgJobMu.Lock()
	pkgJob.Total = len(todos)
	pkgJobMu.Unlock()
	shown := time.Now()
	made, bumped := 0, 0 // covers made, and how many of them the window has been told of
	for i, t := range todos {
		if stopped() {
			break
		}
		// downloads and imports come first: this can wait
		for w := 0; w < 300 && core.Downloading.Load() > 0 && !stopped(); w++ {
			time.Sleep(2 * time.Second)
		}
		pkgJobMu.Lock()
		pkgJob.Now, pkgJob.nowKey = t.name, t.key
		pkgJobMu.Unlock()
		TaskPkgCover.Set(i, len(todos), t.name)
		// what is being read is written down as a failure before it is read: a package that brings the program
		// down (it runs out of memory, say) is then not read again at every start, since this run follows the
		// scan a start makes
		sig := pkgSig(t.asset)
		prev := swapPkgFail(dir, t.key, &PkgFail{Sig: sig, Why: pkgWhyCrashed, At: time.Now().Unix()})
		_, err := extractPkgCover(dir, t.key, t.asset, t.names, stopped)
		over := stopped() || err == errPkgCancelled
		pkgJobMu.Lock()
		pkgJob.Done = i + 1
		if err == nil {
			pkgJob.Made++
			made++
		} else if !over {
			pkgJob.Failed++
			pkgJob.Last = fmt.Sprintf("「%s」：%s", t.name, err.Error())
		}
		pkgJobMu.Unlock()
		switch {
		case err == nil: // (the cover took the note away)
		case over || isPkgIOError(err):
			swapPkgFail(dir, t.key, prev) // nothing was learned about the package: as it was before
		default:
			setPkgFail(dir, t.key, sig, err.Error())
		}
		if made > bumped && time.Since(shown) >= pkgShowEvery { // the cards show what is there so far
			core.BumpRev()
			shown, bumped = time.Now(), made
		}
	}
	pkgJobMu.Lock()
	if pkgJob.cancel {
		pkgJob.Err = "已取消"
	}
	pkgJob.Now, pkgJob.nowKey, pkgJob.Ended = "", "", time.Now().Unix()
	pkgJobMu.Unlock()
}
